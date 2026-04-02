package codegen_go

// Statement emission: var, assign, return, if, for, while, match,
// parallel for, concurrent, spawn, defer, assert, etc.

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
)

// emitStmt dispatches a statement to the appropriate emitter.
func (g *Generator) emitStmt(s parser.Stmt) {
	switch stmt := s.(type) {
	case *parser.VarStmt:
		g.emitVarStmt(stmt)
	case *parser.AssignStmt:
		g.emitAssignStmt(stmt)
	case *parser.ReturnStmt:
		g.emitReturnStmt(stmt)
	case *parser.IfStmt:
		g.emitIfStmt(stmt)
	case *parser.ForStmt:
		g.emitForStmt(stmt)
	case *parser.WhileStmt:
		g.writeln("for %s {", g.formatExpr(stmt.Cond))
		g.indent++
		g.emitBlock(stmt.Body)
		g.indent--
		g.writeln("}")
	case *parser.MatchStmt:
		g.emitMatchStmt(stmt)
	case *parser.ExprStmt:
		g.emitExprStmt(stmt)
	case *parser.PrintStmt:
		g.needImport("fmt")
		// Unwrap: print("msg {x}") → fmt.Printf("msg %v\n", x)
		if interp, ok := stmt.Value.(*parser.StringInterpLit); ok {
			fmtStr, args := g.formatPrintf(interp)
			if len(args) > 0 {
				g.writeln("fmt.Printf(%q, %s)", fmtStr+"\n", strings.Join(args, ", "))
			} else {
				g.writeln("fmt.Println(%q)", fmtStr)
			}
		} else {
			g.writeln("fmt.Println(%s)", g.formatExpr(stmt.Value))
		}
	case *parser.BreakStmt:
		g.writeln("break")
	case *parser.ContinueStmt:
		g.writeln("continue")
	case *parser.BlockStmt:
		g.emitBlock(stmt)
	case *parser.FnDecl:
		g.emitFnDecl(stmt)
	case *parser.TupleVarStmt:
		g.emitTupleVarStmt(stmt)
	case *parser.GoStmt:
		g.writeln("go func() {")
		g.indent++
		g.emitBlock(stmt.Body)
		g.indent--
		g.writeln("}()")
	case *parser.ParallelForStmt:
		g.emitParallelForStmt(stmt)
	case *parser.ConcurrentStmt:
		g.emitConcurrentStmt(stmt)
	case *parser.WithStmt:
		g.emitWithStmt(stmt)
	case *parser.DeferStmt:
		g.writeln("defer %s", g.formatExpr(stmt.Expr))
	case *parser.AssertStmt:
		g.emitAssertStmt(stmt)
	case *parser.TryStmt:
		g.writeln("// try/catch not directly supported in Go")
		g.emitBlock(stmt.Body)
	case *parser.RaiseStmt:
		g.writeln("panic(%s)", g.formatExpr(stmt.Value))
	}
}

// --- Type inference helpers --------------------------------------------------

// inferListLitElemType infers the Go element type from a list literal's elements.
func inferListLitElemType(elements []parser.Expr) string {
	if len(elements) == 0 {
		return "interface{}"
	}
	allInt := true
	allFloat := true
	allString := true
	allBool := true
	for _, e := range elements {
		switch e.(type) {
		case *parser.IntLit:
			allFloat = false
			allString = false
			allBool = false
		case *parser.FloatLit:
			allInt = false
			allString = false
			allBool = false
		case *parser.StringLit, *parser.StringInterpLit:
			allInt = false
			allFloat = false
			allBool = false
		case *parser.BoolLit:
			allInt = false
			allFloat = false
			allString = false
		default:
			return "interface{}"
		}
	}
	if allInt {
		return "int"
	}
	if allFloat {
		return "float64"
	}
	if allString {
		return "string"
	}
	if allBool {
		return "bool"
	}
	return "interface{}"
}

// inferMapLitType infers the Go key and value types from a map literal.
func inferMapLitType(keys []parser.Expr, values []parser.Expr) (string, string) {
	keyType := "string"
	for _, k := range keys {
		if _, ok := k.(*parser.StringLit); !ok {
			keyType = "interface{}"
			break
		}
	}
	valType := inferListLitElemType(values)
	return keyType, valType
}

// inferSliceElemType looks up the element type for a slice expression.
func (g *Generator) inferSliceElemType(expr parser.Expr) string {
	switch e := expr.(type) {
	case *parser.Ident:
		if t, ok := g.varTypes[e.Name]; ok {
			return t
		}
	case *parser.ListLit:
		return inferListLitElemType(e.Elements)
	}
	return "interface{}"
}

// --- Variable declarations ---------------------------------------------------

func (g *Generator) emitVarStmt(v *parser.VarStmt) {
	if v.OrHandler != nil && v.Value != nil {
		// var x = call() or default → error handling
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				if call.IsNew {
					if _, exists := g.structs[ident.Name]; exists {
						g.varStructTypes[v.Name] = ident.Name
					}
				} else if strings.HasPrefix(ident.Name, "New") {
					structName := ident.Name[3:]
					if _, exists := g.structs[structName]; exists {
						g.varStructTypes[v.Name] = structName
					}
				}
			}
		}
		g.emitOrAssignment(v.Name, v.Value, v.OrHandler)
		return
	}

	if v.Value != nil {
		// Typed array/slice
		if arrType, ok := v.Type.(*parser.ArrayType); ok {
			if listLit, ok := v.Value.(*parser.ListLit); ok {
				elemType := g.formatType(arrType.ElementType)
				elems := g.formatExprList(listLit.Elements)
				g.varTypes[v.Name] = elemType
				g.writeln("%s := []%s{%s}", v.Name, elemType, elems)
				return
			}
		}
		// Typed generic: List<int>, Map<K,V>
		if genType, ok := v.Type.(*parser.GenericType); ok {
			g.varTypeExprs[v.Name] = genType
			if listLit, ok := v.Value.(*parser.ListLit); ok {
				goType := g.formatType(genType)
				if strings.HasPrefix(goType, "[]") {
					g.varTypes[v.Name] = goType[2:]
				}
				elems := g.formatExprList(listLit.Elements)
				g.writeln("%s := %s{%s}", v.Name, goType, elems)
				return
			}
			if mapLit, ok := v.Value.(*parser.MapLit); ok {
				goType := g.formatType(genType)
				g.varTypes[v.Name] = goType
				var pairs []string
				for i := range mapLit.Keys {
					pairs = append(pairs, fmt.Sprintf("%s: %s", g.formatExpr(mapLit.Keys[i]), g.formatExpr(mapLit.Values[i])))
				}
				g.writeln("%s := %s{%s}", v.Name, goType, strings.Join(pairs, ", "))
				return
			}
		}

		// Track struct types from constructor calls
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				if call.IsNew {
					if _, exists := g.structs[ident.Name]; exists {
						g.varStructTypes[v.Name] = ident.Name
					}
				} else if strings.HasPrefix(ident.Name, "New") {
					structName := ident.Name[3:]
					if _, exists := g.structs[structName]; exists {
						g.varStructTypes[v.Name] = structName
					}
				} else if _, exists := g.structs[ident.Name]; exists {
					g.varStructTypes[v.Name] = ident.Name
				}
			}
		}

		// Infer type from list literal
		if listLit, ok := v.Value.(*parser.ListLit); ok && v.Type == nil {
			elemType := inferListLitElemType(listLit.Elements)
			if elemType != "interface{}" {
				g.varTypes[v.Name] = elemType
			}
		}

		// Track pointer vars from optional-returning functions
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				if g.funcReturnsOptional[ident.Name] {
					g.ptrVars[v.Name] = true
				}
			}
		}
		if _, ok := v.Value.(*parser.SafeNavExpr); ok {
			g.ptrVars[v.Name] = true
		}

		// Track scalar variable types
		if scalarType := g.inferExprType(v.Value, g.varTypes); scalarType != "" && scalarType != "interface{}" {
			g.varTypes[v.Name] = scalarType
		}

		varName := v.Name
		if goBuiltins[varName] {
			safe := "_" + varName
			g.renamedVars[varName] = safe
			varName = safe
		}
		g.writeln("%s := %s", varName, g.formatExpr(v.Value))
	} else {
		typeName := "interface{}"
		if v.Type != nil {
			typeName = g.formatType(v.Type)
		}
		varName := v.Name
		if goBuiltins[varName] {
			safe := "_" + varName
			g.renamedVars[varName] = safe
			varName = safe
		}
		g.writeln("var %s %s", varName, typeName)
	}
}

// --- Assignment, return, control flow ----------------------------------------

func (g *Generator) emitAssignStmt(a *parser.AssignStmt) {
	if a.OrHandler != nil {
		targetStr := g.formatExpr(a.Target)
		g.emitOrAssignment(targetStr, a.Value, a.OrHandler)
		return
	}
	g.writeln("%s %s %s", g.formatExpr(a.Target), a.Op, g.formatExpr(a.Value))
}

func (g *Generator) emitReturnStmt(r *parser.ReturnStmt) {
	if r.Value == nil {
		if g.currentReturnType != "" {
			zv := zeroValueFor(g.currentReturnType)
			g.writeln("return %s, nil", zv)
		} else if g.currentReturnType == "" && g.errorFuncs != nil {
			g.writeln("return")
		} else {
			g.writeln("return")
		}
		return
	}

	// return Error(...) → return zero, fmt.Errorf(...)
	if call, ok := r.Value.(*parser.CallExpr); ok {
		if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "Error" {
			zv := zeroValueFor(g.currentReturnType)
			if len(call.Args) == 1 {
				arg := call.Args[0]
				if innerCall, ok := arg.(*parser.CallExpr); ok {
					if _, ok := innerCall.Callee.(*parser.Ident); ok {
						g.needImport("fmt")
						if len(innerCall.Args) > 0 {
							msg := g.formatExpr(innerCall.Args[0])
							if zv != "" {
								g.writeln("return %s, fmt.Errorf(%s)", zv, msg)
							} else {
								g.writeln("return fmt.Errorf(%s)", msg)
							}
						} else {
							if zv != "" {
								g.writeln("return %s, fmt.Errorf(\"error\")", zv)
							} else {
								g.writeln("return fmt.Errorf(\"error\")")
							}
						}
						return
					}
				}
				if id, ok := arg.(*parser.Ident); ok {
					if zv != "" {
						g.writeln("return %s, %s", zv, id.Name)
					} else {
						g.writeln("return %s", id.Name)
					}
					return
				}
				g.needImport("fmt")
				if zv != "" {
					g.writeln("return %s, fmt.Errorf(%s)", zv, g.formatExpr(arg))
				} else {
					g.writeln("return fmt.Errorf(%s)", g.formatExpr(arg))
				}
				return
			}
			g.needImport("fmt")
			if zv != "" {
				g.writeln("return %s, fmt.Errorf(\"error\")", zv)
			} else {
				g.writeln("return fmt.Errorf(\"error\")")
			}
			return
		}
	}

	// Optional return: wrap value with new() for pointer type
	if g.currentReturnOptional {
		if _, ok := r.Value.(*parser.NullLit); ok {
			g.writeln("return nil")
			return
		}
		val := g.formatExpr(r.Value)
		g.writeln("return new(%s)", val)
		return
	}

	// Normal return in error-returning function → return val, nil
	if g.currentReturnType != "" {
		g.writeln("return %s, nil", g.formatExpr(r.Value))
		return
	}

	g.writeln("return %s", g.formatExpr(r.Value))
}

func (g *Generator) emitIfStmt(s *parser.IfStmt) {
	g.writeln("if %s {", g.formatExpr(s.Cond))
	g.indent++
	g.emitBlock(s.Then)
	g.indent--
	if s.ElseStmt != nil {
		switch e := s.ElseStmt.(type) {
		case *parser.IfStmt:
			g.write("} else ")
			g.emitIfStmt(e)
			return
		case *parser.BlockStmt:
			g.writeln("} else {")
			g.indent++
			g.emitBlock(e)
			g.indent--
		}
	}
	g.writeln("}")
}

func (g *Generator) emitForStmt(f *parser.ForStmt) {
	if f.IsRange {
		if rangeExpr, ok := f.Range.(*parser.RangeExpr); ok {
			start := g.formatExpr(rangeExpr.Start)
			end := g.formatExpr(rangeExpr.End)
			op := "<"
			if rangeExpr.Inclusive {
				op = "<="
			}
			g.writeln("for %s := %s; %s %s %s; %s++ {", f.Item, start, f.Item, op, end, f.Item)
		} else if f.IndexVar != "" {
			rangeExpr := g.stripEntrySet(f.Range)
			g.writeln("for %s, %s := range %s {", f.IndexVar, f.Item, rangeExpr)
		} else {
			if g.isEntrySetCall(f.Range) {
				mapExpr := g.stripEntrySet(f.Range)
				g.writeln("for %s := range %s {", f.Item, mapExpr)
			} else if g.isMapVar(f.Range) {
				g.writeln("for %s := range %s {", f.Item, g.formatExpr(f.Range))
			} else {
				g.writeln("for _, %s := range %s {", f.Item, g.formatExpr(f.Range))
			}
		}
	} else {
		init := ""
		if f.Init != nil {
			init = g.formatStmtInline(f.Init)
		}
		cond := ""
		if f.Cond != nil {
			cond = g.formatExpr(f.Cond)
		}
		post := ""
		if f.Post != nil {
			post = g.formatStmtInline(f.Post)
		}
		g.writeln("for %s; %s; %s {", init, cond, post)
	}
	g.indent++
	g.emitBlock(f.Body)
	g.indent--
	g.writeln("}")
}

// isEntrySetCall checks if an expression is a .entrySet() call.
func (g *Generator) isEntrySetCall(e parser.Expr) bool {
	if call, ok := e.(*parser.CallExpr); ok {
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "entrySet" {
			return true
		}
	}
	return false
}

// isMapVar checks if the range expression is a variable declared as a Map type.
func (g *Generator) isMapVar(e parser.Expr) bool {
	if ident, ok := e.(*parser.Ident); ok {
		if t, ok := g.varTypes[ident.Name]; ok && strings.HasPrefix(t, "map[") {
			return true
		}
		if t, ok := g.varTypes[ident.Name]; ok && t == "map" {
			return true
		}
	}
	return false
}

// stripEntrySet removes .entrySet() from a range expression.
func (g *Generator) stripEntrySet(e parser.Expr) string {
	if call, ok := e.(*parser.CallExpr); ok {
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "entrySet" {
			return g.formatExpr(sel.Object)
		}
	}
	return g.formatExpr(e)
}

func (g *Generator) emitMatchStmt(m *parser.MatchStmt) {
	// Detect type-switch match: if any case pattern is a data class constructor call,
	// emit a Go type switch instead of a value switch.
	if g.isTypeSwitchMatch(m) {
		g.emitTypeSwitchMatch(m)
		return
	}

	g.writeln("switch %s {", g.formatExpr(m.Subject))
	for _, c := range m.Cases {
		if c.Pattern == nil {
			g.writeln("default:")
		} else {
			g.writeln("case %s:", g.formatExpr(c.Pattern))
		}
		g.indent++
		g.emitBlock(c.Body)
		g.indent--
	}
	g.writeln("}")
}

// isTypeSwitchMatch checks if a match statement should use a Go type switch.
// Returns true if any non-wildcard case pattern is a data class constructor call.
func (g *Generator) isTypeSwitchMatch(m *parser.MatchStmt) bool {
	for _, c := range m.Cases {
		if c.Pattern == nil {
			continue
		}
		if call, ok := c.Pattern.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				if g.dataClasses[ident.Name] {
					return true
				}
			}
			// Qualified: core.Single(ff)
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				if pkg, ok := sel.Object.(*parser.Ident); ok {
					if exports, ok := g.subpkgExports[pkg.Name]; ok {
						if exports[sel.Field] == "data" {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// emitTypeSwitchMatch generates a Go type switch for sealed class / data class matching.
// match result {
//     case Single(ff) { ... }
//     case Multiple(ffs) { ... }
// }
// →
// switch _v := result.(type) {
// case Single:
//     ff := _v.Ff
//     ...
// case Multiple:
//     ffs := _v.Ffs
//     ...
// }
func (g *Generator) emitTypeSwitchMatch(m *parser.MatchStmt) {
	g.writeln("switch _v := %s.(type) {", g.formatExpr(m.Subject))
	for _, c := range m.Cases {
		if c.Pattern == nil {
			g.writeln("default:")
			g.indent++
			g.emitBlock(c.Body)
			g.indent--
			continue
		}

		call, ok := c.Pattern.(*parser.CallExpr)
		if !ok {
			// Plain identifier pattern (e.g. case Drop without args)
			g.writeln("case %s:", g.formatExpr(c.Pattern))
			g.indent++
			g.emitBlock(c.Body)
			g.indent--
			continue
		}

		// Get the type name (local or qualified)
		typeName := ""
		var dataDecl *parser.DataClassDecl
		if ident, ok := call.Callee.(*parser.Ident); ok {
			typeName = ident.Name
			// Look up the data class declaration to get field names
			for _, d := range g.currentSealedVariants(ident.Name) {
				if d.Name == ident.Name {
					dataDecl = d
					break
				}
			}
		} else if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
			if pkg, ok := sel.Object.(*parser.Ident); ok {
				typeName = pkg.Name + "." + sel.Field
			}
		}

		g.writeln("case %s:", typeName)
		g.indent++

		// Bind destructured variables: case Single(ff) → ff := _v.Ff
		if dataDecl != nil && len(call.Args) > 0 {
			for i, arg := range call.Args {
				if ident, ok := arg.(*parser.Ident); ok && ident.Name != "_" {
					if i < len(dataDecl.Params) {
						fieldName := exportName(dataDecl.Params[i].Name)
						g.writeln("%s := _v.%s", ident.Name, fieldName)
					}
				}
			}
		} else if len(call.Args) > 0 {
			// No data decl found — try positional binding with generic field names
			for i, arg := range call.Args {
				if ident, ok := arg.(*parser.Ident); ok && ident.Name != "_" {
					// Use the arg name as a field guess (capitalize first letter)
					fieldName := exportName(ident.Name)
					g.writeln("%s := _v.%s", ident.Name, fieldName)
				}
				_ = i
			}
		}

		g.emitBlock(c.Body)
		g.indent--
	}
	g.writeln("}")
}

// currentSealedVariants returns the variants of the sealed class that contains the given variant name.
func (g *Generator) currentSealedVariants(variantName string) []*parser.DataClassDecl {
	for _, cls := range g.structs {
		if cls.IsSealed {
			for _, v := range cls.Variants {
				if v.Name == variantName {
					return cls.Variants
				}
			}
		}
	}
	return nil
}

// --- Expression statements (spawn, print, collection methods, forEach) -------

func (g *Generator) emitExprStmt(es *parser.ExprStmt) {
	if es.OrHandler != nil {
		g.emitOrAssignment("_", es.Expr, es.OrHandler)
		return
	}
	// spawn { body } → go func() { body }()
	if spawn, ok := es.Expr.(*parser.SpawnExpr); ok {
		g.writeln("go func() {")
		g.indent++
		g.emitBlock(spawn.Body)
		g.indent--
		g.writeln("}()")
		return
	}
	// print("msg {x}") → fmt.Printf("msg %v\n", x)
	if call, ok := es.Expr.(*parser.CallExpr); ok {
		if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "print" && len(call.Args) == 1 {
			if interp, ok := call.Args[0].(*parser.StringInterpLit); ok {
				g.needImport("fmt")
				fmtStr, args := g.formatPrintf(interp)
				if len(args) > 0 {
					g.writeln("fmt.Printf(%q, %s)", fmtStr+"\n", strings.Join(args, ", "))
				} else {
					g.writeln("fmt.Println(%q)", fmtStr)
				}
				return
			}
		}
	}
	// Collection method rewrites as statements
	if call, ok := es.Expr.(*parser.CallExpr); ok {
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "add" && !g.isStructVar(sel.Object) {
			obj := g.formatExpr(sel.Object)
			args := g.formatExprList(call.Args)
			g.writeln("%s = append(%s, %s)", obj, obj, args)
			return
		}
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "send" && len(call.Args) == 1 {
			if !g.isStructVar(sel.Object) {
				obj := g.formatExpr(sel.Object)
				g.writeln("%s <- %s", obj, g.formatExpr(call.Args[0]))
				return
			}
		}
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "close" && len(call.Args) == 0 {
			if !g.isStructVar(sel.Object) {
				obj := g.formatExpr(sel.Object)
				g.writeln("close(%s)", obj)
				return
			}
		}
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "put" && len(call.Args) == 2 {
			obj := g.formatExpr(sel.Object)
			g.writeln("%s[%s] = %s", obj, g.formatExpr(call.Args[0]), g.formatExpr(call.Args[1]))
			return
		}
		// .forEach() as statement — try loop fusion for chains
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "forEach" && len(call.Args) == 1 {
			if innerCall, ok := sel.Object.(*parser.CallExpr); ok {
				if innerSel, ok := innerCall.Callee.(*parser.SelectorExpr); ok && streamMethods[innerSel.Field] {
					g.emitFusedForEachChain(sel.Object, call.Args[0])
					return
				}
			}
			obj := g.formatExpr(sel.Object)
			g.emitForEachStmt(obj, call.Args[0])
			return
		}
	}
	g.writeln("%s", g.formatExpr(es.Expr))
}

// isStructVar checks if an expression refers to a known struct instance variable.
// Used to distinguish channel ops (send/recv/close) from method calls on structs.
func (g *Generator) isStructVar(e parser.Expr) bool {
	if ident, ok := e.(*parser.Ident); ok {
		if _, ok := g.varStructTypes[ident.Name]; ok {
			return true
		}
	}
	return false
}

// emitForEachStmt emits a for-range loop for .forEach().
func (g *Generator) emitForEachStmt(obj string, fn parser.Expr) {
	if lambda, ok := fn.(*parser.LambdaExpr); ok {
		if len(lambda.Params) == 1 {
			paramName := lambda.Params[0].Name
			g.writeln("for _, %s := range %s {", paramName, obj)
			g.indent++
			if lambda.Expr != nil {
				g.writeln("%s", g.formatExpr(lambda.Expr))
			} else if lambda.Body != nil {
				g.emitBlock(lambda.Body)
			}
			g.indent--
			g.writeln("}")
			return
		}
	}
	if containsIt(fn) {
		g.writeln("for _, _it := range %s {", obj)
		g.indent++
		g.writeln("%s", g.formatExprIt(fn))
		g.indent--
		g.writeln("}")
		return
	}
	fnStr := g.formatExpr(fn)
	g.writeln("for _, _v := range %s {", obj)
	g.indent++
	g.writeln("%s(_v)", fnStr)
	g.indent--
	g.writeln("}")
}

// emitFusedForEachChain fuses a stream chain ending in forEach into a single loop.
func (g *Generator) emitFusedForEachChain(chainExpr parser.Expr, forEachFn parser.Expr) {
	type chainOp struct {
		method string
		args   []parser.Expr
	}
	var chain []chainOp
	obj := chainExpr
	for {
		if call, ok := obj.(*parser.CallExpr); ok {
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok && streamMethods[sel.Field] {
				chain = append(chain, chainOp{method: sel.Field, args: call.Args})
				obj = sel.Object
				continue
			}
		}
		break
	}
	// Reverse so chain goes from source to terminal
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	// Only fuse filter/map chains
	fusible := true
	for _, op := range chain {
		if op.method != "filter" && op.method != "map" {
			fusible = false
			break
		}
	}
	if !fusible {
		source := g.formatExpr(chainExpr)
		g.emitForEachStmt(source, forEachFn)
		return
	}

	source := g.formatExpr(obj)

	// Build forEach body
	var forEachBody func(iterVar string)
	if lambda, ok := forEachFn.(*parser.LambdaExpr); ok && len(lambda.Params) == 1 {
		paramName := lambda.Params[0].Name
		forEachBody = func(iterVar string) {
			if paramName != iterVar {
				g.writeln("%s := %s", paramName, iterVar)
			}
			if lambda.Expr != nil {
				g.writeln("%s", g.formatExpr(lambda.Expr))
			} else if lambda.Body != nil {
				g.emitBlock(lambda.Body)
			}
		}
	} else if containsIt(forEachFn) {
		forEachBody = func(_ string) {
			g.writeln("%s", g.formatExprIt(forEachFn))
		}
	} else {
		fnStr := g.formatExpr(forEachFn)
		forEachBody = func(iterVar string) {
			g.writeln("%s(%s)", fnStr, iterVar)
		}
	}

	// Emit fused loop
	g.writeln("for _, _it := range %s {", source)
	g.indent++
	iterVar := "_it"
	for _, op := range chain {
		switch op.method {
		case "filter":
			pred := g.streamLambdaBody(op.args)
			g.writeln("if !(%s) { continue }", pred)
		case "map":
			transform := g.streamLambdaBody(op.args)
			g.writeln("_it = %s", transform)
		}
	}
	forEachBody(iterVar)
	g.indent--
	g.writeln("}")
}

// --- Error handling (or expressions) -----------------------------------------

// emitOrAssignment handles: target = call() or default / or { block }
func (g *Generator) emitOrAssignment(target string, value parser.Expr, handler *parser.OrHandler) {
	callExpr := g.formatExpr(value)

	errVar := "_err"
	if g.errVarCount > 0 {
		errVar = fmt.Sprintf("_err%d", g.errVarCount)
	}
	g.errVarCount++
	savedErrVar := g.currentErrVar
	g.currentErrVar = errVar

	if handler.Body != nil && len(handler.Body.Stmts) == 1 {
		if es, ok := handler.Body.Stmts[0].(*parser.ExprStmt); ok && target != "_" {
			g.writeln("%s, %s := %s", target, errVar, callExpr)
			g.writeln("if %s != nil {", errVar)
			g.indent++
			g.writeln("%s = %s", target, g.formatExpr(es.Expr))
			g.indent--
			g.writeln("}")
			g.currentErrVar = savedErrVar
			return
		}
	}

	g.writeln("%s, %s := %s", target, errVar, callExpr)
	g.writeln("if %s != nil {", errVar)
	g.indent++
	if handler.Body != nil {
		g.emitOrBlock(handler.Body)
	}
	g.indent--
	g.writeln("}")
	g.currentErrVar = savedErrVar
}

// emitOrBlock emits a block inside an or-handler, mapping `err` to the current error variable.
func (g *Generator) emitOrBlock(block *parser.BlockStmt) {
	for _, s := range block.Stmts {
		if ret, ok := s.(*parser.ReturnStmt); ok && ret.Value != nil {
			if call, ok := ret.Value.(*parser.CallExpr); ok {
				if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "Error" {
					if len(call.Args) == 1 {
						if argId, ok := call.Args[0].(*parser.Ident); ok && argId.Name == "err" {
							zv := zeroValueFor(g.currentReturnType)
							if zv != "" {
								g.writeln("return %s, %s", zv, g.currentErrVar)
							} else {
								g.writeln("return %s", g.currentErrVar)
							}
							continue
						}
					}
				}
			}
		}
		g.emitStmt(s)
	}
}

// --- Concurrency statements --------------------------------------------------

func (g *Generator) emitParallelForStmt(p *parser.ParallelForStmt) {
	g.needImport("sync")
	g.writeln("var _wg sync.WaitGroup")
	if p.Max > 0 {
		g.writeln("_sem := make(chan struct{}, %d)", p.Max)
	}
	g.writeln("for _, %s := range %s {", p.Item, g.formatExpr(p.Range))
	g.indent++
	g.writeln("%s := %s // capture", p.Item, p.Item)
	g.writeln("_wg.Add(1)")
	if p.Max > 0 {
		g.writeln("_sem <- struct{}{}")
	}
	g.writeln("go func() {")
	g.indent++
	g.writeln("defer _wg.Done()")
	if p.Max > 0 {
		g.writeln("defer func() { <-_sem }()")
	}
	g.emitBlock(p.Body)
	g.indent--
	g.writeln("}()")
	g.indent--
	g.writeln("}")
	g.writeln("_wg.Wait()")
}

func (g *Generator) emitConcurrentStmt(c *parser.ConcurrentStmt) {
	g.needImport("sync")
	g.writeln("var _wg sync.WaitGroup")
	for _, task := range c.Tasks {
		g.writeln("_wg.Add(1)")
		g.writeln("go func() {")
		g.indent++
		g.writeln("defer _wg.Done()")
		g.writeln("%s", g.formatExpr(task))
		g.indent--
		g.writeln("}()")
	}
	g.writeln("_wg.Wait()")
}

func (g *Generator) emitWithStmt(w *parser.WithStmt) {
	if len(w.Resources) == 1 && w.Resources[0].Name == "_lock" {
		lockExpr := g.formatExpr(w.Resources[0].Value)
		g.writeln("%s.Lock()", lockExpr)
		g.writeln("defer %s.Unlock()", lockExpr)
		g.emitBlock(w.Body)
		return
	}
	for _, r := range w.Resources {
		g.writeln("%s := %s", r.Name, g.formatExpr(r.Value))
		g.writeln("defer %s.Close()", r.Name)
	}
	g.emitBlock(w.Body)
}

func (g *Generator) emitTupleVarStmt(t *parser.TupleVarStmt) {
	names := strings.Join(t.Names, ", ")
	g.writeln("%s := %s", names, g.formatExpr(t.Value))
}

func (g *Generator) emitAssertStmt(a *parser.AssertStmt) {
	if a.Message != nil {
		g.writeln("if !(%s) { panic(%s) }", g.formatExpr(a.Cond), g.formatExpr(a.Message))
	} else {
		g.writeln("if !(%s) { panic(\"assertion failed\") }", g.formatExpr(a.Cond))
	}
}

func (g *Generator) emitBlock(block *parser.BlockStmt) {
	if block == nil {
		return
	}
	for _, s := range block.Stmts {
		g.emitStmt(s)
	}
}
