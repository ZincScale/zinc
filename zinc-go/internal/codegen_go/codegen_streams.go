package codegen_go

// Stream operations: filter, map, reduce, sum, distinct, sortBy, groupBy,
// anyMatch, allMatch, noneMatch, findFirst, skip, limit.
// Includes loop fusion for chained operations.

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
)

type streamOp = struct {
	method string
	args   []parser.Expr
}

// formatStreamExpr handles stream method calls, including chains.
// It unwraps chained calls, generates each step as a separate variable,
// and returns the final value.
func (g *Generator) formatStreamExpr(sel *parser.SelectorExpr, args []parser.Expr) string {
	// Collect the chain of stream operations from innermost to outermost
	var chain []streamOp
	chain = append(chain, streamOp{method: sel.Field, args: args})

	obj := sel.Object
	for {
		if call, ok := obj.(*parser.CallExpr); ok {
			if innerSel, ok := call.Callee.(*parser.SelectorExpr); ok && streamMethods[innerSel.Field] {
				chain = append(chain, streamOp{method: innerSel.Field, args: call.Args})
				obj = innerSel.Object
				continue
			}
		}
		break
	}

	// Reverse chain so it goes from source → terminal
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	sourceExpr := g.formatExpr(obj)

	// Single operation → use IIFE
	if len(chain) == 1 {
		return g.formatSingleStreamOp(sourceExpr, obj, chain[0].method, chain[0].args)
	}

	// Chained operations → try loop fusion first
	elemType := g.inferSliceElemType(obj)
	lastOp := chain[len(chain)-1].method

	if fused := g.tryLoopFusion(sourceExpr, chain, elemType); fused != "" {
		return fused
	}

	// Fallback: intermediate variables
	retType := "interface{}"
	switch lastOp {
	case "sum":
		if elemType == "int" || elemType == "float64" {
			retType = elemType
		} else {
			retType = "int"
		}
	case "anyMatch", "allMatch", "noneMatch":
		retType = "bool"
	case "reduce":
		retType = elemType
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("func() %s {\n", retType))
	currentVar := sourceExpr
	currentElemType := elemType
	for i, op := range chain {
		varName := fmt.Sprintf("_chain%d", i)
		innerCode := g.formatSingleStreamAssignTyped(varName, currentVar, op.method, op.args, currentElemType)
		sb.WriteString(innerCode)
		currentVar = varName
	}
	sb.WriteString(fmt.Sprintf("return %s\n", currentVar))
	sb.WriteString("}()")
	return sb.String()
}

// tryLoopFusion attempts to fuse a chain of stream operations into a single loop.
// Fusible patterns: [filter]* → [map]? → terminal
func (g *Generator) tryLoopFusion(source string, chain []streamOp, elemType string) string {
	if len(chain) < 2 {
		return ""
	}

	var filters []string
	var mapExpr string
	var terminal streamOp
	terminalIdx := -1

	terminals := map[string]bool{
		"sum": true, "reduce": true, "forEach": true, "count": true,
		"anyMatch": true, "allMatch": true, "noneMatch": true, "findFirst": true,
	}
	intermediates := map[string]bool{
		"filter": true, "map": true,
	}

	for i, op := range chain {
		if terminals[op.method] {
			terminal = op
			terminalIdx = i
			break
		}
		if !intermediates[op.method] {
			return ""
		}
	}

	if terminalIdx < 0 {
		return ""
	}

	for i := 0; i < terminalIdx; i++ {
		op := chain[i]
		switch op.method {
		case "filter":
			filters = append(filters, g.streamLambdaBody(op.args))
		case "map":
			mapExpr = g.streamLambdaBody(op.args)
		default:
			return ""
		}
	}

	// Build the fused loop
	var sb strings.Builder
	iterVar := "_it"
	mappedVar := "_it"

	switch terminal.method {
	case "sum":
		retType := elemType
		if retType == "" || retType == "interface{}" {
			retType = "int"
		}
		sb.WriteString(fmt.Sprintf("func() %s { _acc := %s; for _, %s := range %s {",
			retType, zeroValueFor(retType), iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		if mapExpr != "" {
			sb.WriteString(fmt.Sprintf("; _v := %s; _acc += _v", mapExpr))
		} else {
			sb.WriteString("; _acc += _it")
		}
		sb.WriteString(" }; return _acc }()")

	case "count":
		sb.WriteString(fmt.Sprintf("func() int { _acc := 0; for _, %s := range %s {", iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		sb.WriteString("; _acc++")
		sb.WriteString(" }; return _acc }()")

	case "anyMatch":
		pred := g.streamLambdaBody(terminal.args)
		sb.WriteString(fmt.Sprintf("func() bool { for _, %s := range %s {", iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		if mapExpr != "" {
			mappedVar = "_v"
			sb.WriteString(fmt.Sprintf("; %s := %s", mappedVar, mapExpr))
		}
		finalPred := pred
		if mapExpr != "" {
			finalPred = strings.ReplaceAll(pred, "_it", mappedVar)
		}
		sb.WriteString(fmt.Sprintf("; if %s { return true }", finalPred))
		sb.WriteString(" }; return false }()")

	case "allMatch":
		pred := g.streamLambdaBody(terminal.args)
		sb.WriteString(fmt.Sprintf("func() bool { for _, %s := range %s {", iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		finalPred := pred
		if mapExpr != "" {
			mappedVar = "_v"
			sb.WriteString(fmt.Sprintf("; %s := %s", mappedVar, mapExpr))
			finalPred = strings.ReplaceAll(pred, "_it", mappedVar)
		}
		sb.WriteString(fmt.Sprintf("; if !(%s) { return false }", finalPred))
		sb.WriteString(" }; return true }()")

	case "noneMatch":
		pred := g.streamLambdaBody(terminal.args)
		sb.WriteString(fmt.Sprintf("func() bool { for _, %s := range %s {", iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		finalPred := pred
		if mapExpr != "" {
			mappedVar = "_v"
			sb.WriteString(fmt.Sprintf("; %s := %s", mappedVar, mapExpr))
			finalPred = strings.ReplaceAll(pred, "_it", mappedVar)
		}
		sb.WriteString(fmt.Sprintf("; if %s { return false }", finalPred))
		sb.WriteString(" }; return true }()")

	case "findFirst":
		pred := g.streamLambdaBody(terminal.args)
		sb.WriteString(fmt.Sprintf("func() %s { for _, %s := range %s {", elemType, iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		finalPred := pred
		if mapExpr != "" {
			mappedVar = "_v"
			sb.WriteString(fmt.Sprintf("; %s := %s", mappedVar, mapExpr))
			finalPred = strings.ReplaceAll(pred, "_it", mappedVar)
		}
		sb.WriteString(fmt.Sprintf("; if %s { return _it }", finalPred))
		sb.WriteString(fmt.Sprintf(" }; var _zero %s; return _zero }()", elemType))

	case "forEach":
		body := g.streamLambdaBody(terminal.args)
		sb.WriteString(fmt.Sprintf("func() { for _, %s := range %s {", iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		if mapExpr != "" {
			sb.WriteString(fmt.Sprintf("; _it = %s", mapExpr))
		}
		sb.WriteString(fmt.Sprintf("; %s", body))
		sb.WriteString(" } }()")

	case "reduce":
		if len(terminal.args) < 2 {
			return ""
		}
		initVal := g.formatExpr(terminal.args[0])
		reduceBody := g.streamReduceBody(terminal.args[1])
		sb.WriteString(fmt.Sprintf("func() %s { _acc := %s; for _, %s := range %s {",
			elemType, initVal, iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		if mapExpr != "" {
			sb.WriteString(fmt.Sprintf("; _it = %s", mapExpr))
		}
		sb.WriteString(fmt.Sprintf("; _acc = %s", reduceBody))
		sb.WriteString(" }; return _acc }()")

	default:
		return ""
	}

	return sb.String()
}

// --- Single stream operations ------------------------------------------------

// formatSingleStreamOp generates an IIFE for a single stream operation.
func (g *Generator) formatSingleStreamOp(source string, sourceExpr parser.Expr, method string, args []parser.Expr) string {
	elemType := g.inferSliceElemType(sourceExpr)
	sliceType := "[]" + elemType
	switch method {
	case "filter":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("func() %s { var _r %s; for _, _it := range %s { if %s { _r = append(_r, _it) } }; return _r }()", sliceType, sliceType, source, pred)
	case "map":
		transform := g.streamLambdaBody(args)
		return fmt.Sprintf("func() %s { _r := make(%s, len(%s)); for _i, _it := range %s { _r[_i] = %s }; return _r }()", sliceType, sliceType, source, source, transform)
	case "sum":
		if elemType == "int" {
			return fmt.Sprintf("func() int { _s := 0; for _, _it := range %s { _s += _it }; return _s }()", source)
		}
		if elemType == "float64" {
			return fmt.Sprintf("func() float64 { _s := 0.0; for _, _it := range %s { _s += _it }; return _s }()", source)
		}
		return fmt.Sprintf("func() int { _s := 0; for _, _it := range %s { _s += _it.(int) }; return _s }()", source)
	case "anyMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("func() bool { for _, _it := range %s { if %s { return true } }; return false }()", source, pred)
	case "allMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("func() bool { for _, _it := range %s { if !(%s) { return false } }; return true }()", source, pred)
	case "noneMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("func() bool { for _, _it := range %s { if %s { return false } }; return true }()", source, pred)
	case "findFirst":
		pred := g.streamLambdaBody(args)
		if elemType != "interface{}" {
			return fmt.Sprintf("func() %s { for _, _it := range %s { if %s { return _it } }; var _zero %s; return _zero }()", elemType, source, pred, elemType)
		}
		return fmt.Sprintf("func() interface{} { for _, _it := range %s { if %s { return _it } }; return nil }()", source, pred)
	case "skip":
		if len(args) > 0 {
			n := g.formatExpr(args[0])
			return fmt.Sprintf("%s[%s:]", source, n)
		}
		return source
	case "limit":
		if len(args) > 0 {
			n := g.formatExpr(args[0])
			return fmt.Sprintf("%s[:%s]", source, n)
		}
		return source
	case "distinct":
		return fmt.Sprintf("func() %s { _seen := map[%s]bool{}; var _r %s; for _, _it := range %s { if !_seen[_it] { _seen[_it] = true; _r = append(_r, _it) } }; return _r }()", sliceType, elemType, sliceType, source)
	case "reduce":
		if len(args) >= 2 {
			init := g.formatExpr(args[0])
			fn := g.streamReduceBody(args[1])
			if elemType != "interface{}" {
				return fmt.Sprintf("func() %s { _acc := %s; for _, _it := range %s { _acc = %s }; return _acc }()", elemType, init, source, fn)
			}
			return fmt.Sprintf("func() interface{} { _acc := %s; for _, _it := range %s { _acc = %s }; return _acc }()", init, source, fn)
		}
		return source
	case "forEach":
		if len(args) > 0 {
			body := g.streamLambdaBody(args)
			return fmt.Sprintf("func() { for _, _it := range %s { _ = %s } }()", source, body)
		}
		return source
	case "sortBy":
		if len(args) > 0 {
			g.needImport("sort")
			key := g.streamLambdaBody(args)
			cmp := g.sortByComparison(key, elemType)
			return fmt.Sprintf("func() %s { _r := make(%s, len(%s)); copy(_r, %s); sort.Slice(_r, func(_i, _j int) bool { _it := _r[_i]; _ = _it; _a := %s; _it = _r[_j]; _b := %s; return %s }); return _r }()", sliceType, sliceType, source, source, key, key, cmp)
		}
		return source
	case "groupBy":
		if len(args) > 0 {
			key := g.streamLambdaBody(args)
			keyType := g.inferGroupByKeyType(key, args)
			return fmt.Sprintf("func() map[%s]%s { _r := map[%s]%s{}; for _, _it := range %s { _k := %s; _r[_k] = append(_r[_k], _it) }; return _r }()", keyType, sliceType, keyType, sliceType, source, key)
		}
		return source
	default:
		return fmt.Sprintf("%s.%s()", source, method)
	}
}

// formatSingleStreamAssignTyped generates code that assigns the result of a stream op to a variable.
func (g *Generator) formatSingleStreamAssignTyped(varName, source, method string, args []parser.Expr, elemType string) string {
	sliceType := "[]" + elemType
	switch method {
	case "filter":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("var %s %s\nfor _, _it := range %s { if %s { %s = append(%s, _it) } }\n", varName, sliceType, source, pred, varName, varName)
	case "map":
		transform := g.streamLambdaBody(args)
		return fmt.Sprintf("%s := make(%s, len(%s))\nfor _i, _it := range %s { %s[_i] = %s }\n", varName, sliceType, source, source, varName, transform)
	case "sum":
		if elemType == "int" || elemType == "float64" {
			return fmt.Sprintf("%s := 0\nfor _, _it := range %s { %s += _it }\n", varName, source, varName)
		}
		return fmt.Sprintf("%s := 0\nfor _, _it := range %s { _s, _ok := _it.(int); if _ok { %s += _s } }\n", varName, source, varName)
	case "anyMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("%s := false\nfor _, _it := range %s { if %s { %s = true; break } }\n", varName, source, pred, varName)
	case "allMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("%s := true\nfor _, _it := range %s { if !(%s) { %s = false; break } }\n", varName, source, pred, varName)
	case "noneMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("%s := true\nfor _, _it := range %s { if %s { %s = false; break } }\n", varName, source, pred, varName)
	case "findFirst":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("var %s %s\nfor _, _it := range %s { if %s { %s = _it; break } }\n", varName, elemType, source, pred, varName)
	case "skip":
		if len(args) > 0 {
			n := g.formatExpr(args[0])
			return fmt.Sprintf("%s := %s[%s:]\n", varName, source, n)
		}
		return fmt.Sprintf("%s := %s\n", varName, source)
	case "limit":
		if len(args) > 0 {
			n := g.formatExpr(args[0])
			return fmt.Sprintf("%s := %s[:%s]\n", varName, source, n)
		}
		return fmt.Sprintf("%s := %s\n", varName, source)
	case "distinct":
		return fmt.Sprintf("_seen_%s := map[%s]bool{}\nvar %s %s\nfor _, _it := range %s { if !_seen_%s[_it] { _seen_%s[_it] = true; %s = append(%s, _it) } }\n",
			varName, elemType, varName, sliceType, source, varName, varName, varName, varName)
	case "reduce":
		if len(args) >= 2 {
			init := g.formatExpr(args[0])
			fn := g.streamReduceBody(args[1])
			return fmt.Sprintf("%s := %s\nfor _, _it := range %s { %s = %s }\n", varName, init, source, varName, fn)
		}
		return fmt.Sprintf("%s := %s\n", varName, source)
	case "forEach":
		if len(args) > 0 {
			body := g.streamLambdaBody(args)
			return fmt.Sprintf("for _, _it := range %s { _ = %s }\n%s := 0\n_ = %s\n", source, body, varName, varName)
		}
		return fmt.Sprintf("%s := 0\n_ = %s\n", varName, varName)
	case "sortBy":
		if len(args) > 0 {
			g.needImport("sort")
			key := g.streamLambdaBody(args)
			cmp := g.sortByComparison(key, elemType)
			return fmt.Sprintf("%s := make(%s, len(%s))\ncopy(%s, %s)\nsort.Slice(%s, func(_i, _j int) bool { _it := %s[_i]; _ = _it; _a := %s; _it = %s[_j]; _b := %s; return %s })\n",
				varName, sliceType, source, varName, source, varName, varName, key, varName, key, cmp)
		}
		return fmt.Sprintf("%s := %s\n", varName, source)
	case "groupBy":
		if len(args) > 0 {
			key := g.streamLambdaBody(args)
			keyType := g.inferGroupByKeyType(key, args)
			return fmt.Sprintf("%s := map[%s]%s{}\nfor _, _it := range %s { _k := %s; %s[_k] = append(%s[_k], _it) }\n",
				varName, keyType, sliceType, source, key, varName, varName)
		}
		return fmt.Sprintf("%s := %s\n", varName, source)
	default:
		return fmt.Sprintf("%s := %s\n", varName, source)
	}
}

// --- Stream helpers ----------------------------------------------------------

// sortByComparison returns the comparison expression for sortBy.
func (g *Generator) sortByComparison(keyExpr string, elemType string) string {
	keyType := ""
	switch {
	case elemType == "int" || elemType == "float64" || elemType == "string":
		keyType = elemType
	case strings.HasPrefix(keyExpr, "string("):
		keyType = "string"
	case strings.HasPrefix(keyExpr, "int(") || strings.HasPrefix(keyExpr, "len("):
		keyType = "int"
	case strings.HasPrefix(keyExpr, "float64("):
		keyType = "float64"
	}
	if keyType == "int" || keyType == "float64" || keyType == "string" {
		return "_a < _b"
	}
	g.needImport("fmt")
	return "fmt.Sprint(_a) < fmt.Sprint(_b)"
}

// inferGroupByKeyType infers the key type for groupBy from the lambda expression.
func (g *Generator) inferGroupByKeyType(keyExpr string, args []parser.Expr) string {
	switch {
	case strings.HasPrefix(keyExpr, "string("):
		return "string"
	case strings.HasPrefix(keyExpr, "int(") || strings.HasPrefix(keyExpr, "len("):
		return "int"
	case strings.HasPrefix(keyExpr, "float64("):
		return "float64"
	}
	if len(args) > 0 {
		if lambda, ok := args[0].(*parser.LambdaExpr); ok && lambda.Expr != nil {
			t := g.inferExprType(lambda.Expr, g.varTypes)
			if t != "" && t != "interface{}" {
				return t
			}
		}
	}
	return "string"
}

// streamLambdaBody extracts the body expression from a lambda or `it`-expression arg.
func (g *Generator) streamLambdaBody(args []parser.Expr) string {
	if len(args) == 0 {
		return "true"
	}
	arg := args[0]
	if lambda, ok := arg.(*parser.LambdaExpr); ok {
		if lambda.Expr != nil {
			if len(lambda.Params) == 1 {
				return g.replaceIdent(lambda.Expr, lambda.Params[0].Name, "_it")
			}
			return g.formatExpr(lambda.Expr)
		}
	}
	if containsIt(arg) {
		return g.formatExprIt(arg)
	}
	return g.formatExpr(arg) + "(_it)"
}

// streamReduceBody extracts the body for a reduce operation.
func (g *Generator) streamReduceBody(arg parser.Expr) string {
	if lambda, ok := arg.(*parser.LambdaExpr); ok {
		if lambda.Expr != nil && len(lambda.Params) == 2 {
			replaced := g.replaceIdent(lambda.Expr, lambda.Params[0].Name, "_acc")
			replaced = strings.ReplaceAll(replaced, lambda.Params[1].Name, "_it")
			return replaced
		}
	}
	return g.formatExpr(arg) + "(_acc, _it)"
}

// replaceIdent formats an expression, replacing occurrences of oldName with newName.
func (g *Generator) replaceIdent(e parser.Expr, oldName, newName string) string {
	switch expr := e.(type) {
	case *parser.Ident:
		if expr.Name == oldName {
			return newName
		}
		return g.formatExpr(e)
	case *parser.BinaryExpr:
		left := g.replaceIdent(expr.Left, oldName, newName)
		right := g.replaceIdent(expr.Right, oldName, newName)
		op := expr.Op
		switch op {
		case "and":
			op = "&&"
		case "or":
			op = "||"
		}
		return fmt.Sprintf("%s %s %s", left, op, right)
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", expr.Op, g.replaceIdent(expr.Operand, oldName, newName))
	case *parser.SelectorExpr:
		return fmt.Sprintf("%s.%s", g.replaceIdent(expr.Object, oldName, newName), exportName(expr.Field))
	case *parser.CallExpr:
		if sel, ok := expr.Callee.(*parser.SelectorExpr); ok {
			obj := g.replaceIdent(sel.Object, oldName, newName)
			var replArgs []string
			for _, a := range expr.Args {
				replArgs = append(replArgs, g.replaceIdent(a, oldName, newName))
			}
			switch sel.Field {
			case "length", "size":
				return fmt.Sprintf("len(%s)", obj)
			case "charAt":
				if len(replArgs) > 0 {
					return fmt.Sprintf("string(%s[%s])", obj, replArgs[0])
				}
			case "substring":
				if len(replArgs) == 2 {
					return fmt.Sprintf("%s[%s:%s]", obj, replArgs[0], replArgs[1])
				}
				if len(replArgs) == 1 {
					return fmt.Sprintf("%s[%s:]", obj, replArgs[0])
				}
			case "upper":
				g.needImport("strings")
				return fmt.Sprintf("strings.ToUpper(%s)", obj)
			case "lower":
				g.needImport("strings")
				return fmt.Sprintf("strings.ToLower(%s)", obj)
			case "contains":
				g.needImport("strings")
				if len(replArgs) > 0 {
					return fmt.Sprintf("strings.Contains(%s, %s)", obj, replArgs[0])
				}
			case "replace":
				g.needImport("strings")
				if len(replArgs) == 2 {
					return fmt.Sprintf("strings.ReplaceAll(%s, %s, %s)", obj, replArgs[0], replArgs[1])
				}
			}
			if goFunc, ok := stringMethodMapping[sel.Field]; ok {
				g.needImport("strings")
				if len(replArgs) > 0 {
					return fmt.Sprintf("%s(%s, %s)", goFunc, obj, strings.Join(replArgs, ", "))
				}
				return fmt.Sprintf("%s(%s)", goFunc, obj)
			}
			return fmt.Sprintf("%s.%s(%s)", obj, exportName(sel.Field), strings.Join(replArgs, ", "))
		}
		callee := g.replaceIdent(expr.Callee, oldName, newName)
		var args []string
		for _, a := range expr.Args {
			args = append(args, g.replaceIdent(a, oldName, newName))
		}
		if ident, ok := expr.Callee.(*parser.Ident); ok {
			switch ident.Name {
			case "print":
				g.needImport("fmt")
				return fmt.Sprintf("fmt.Println(%s)", strings.Join(args, ", "))
			case "len":
				return fmt.Sprintf("len(%s)", strings.Join(args, ", "))
			case "str":
				g.needImport("fmt")
				return fmt.Sprintf("fmt.Sprint(%s)", strings.Join(args, ", "))
			}
		}
		return fmt.Sprintf("%s(%s)", callee, strings.Join(args, ", "))
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", g.replaceIdent(expr.Object, oldName, newName), g.replaceIdent(expr.Index, oldName, newName))
	default:
		return g.formatExpr(e)
	}
}
