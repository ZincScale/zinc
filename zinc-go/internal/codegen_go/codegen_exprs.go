package codegen_go

// Expression formatting: literals, identifiers, binary/unary ops, calls,
// selectors, lambdas, string interpolation, type assertions, safe navigation,
// match expressions, and the `it` keyword.

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
)

// formatExpr converts a Zinc AST expression to its Go source representation.
func (g *Generator) formatExpr(e parser.Expr) string {
	switch expr := e.(type) {
	case *parser.Ident:
		if expr.Name == "this" {
			return "s"
		}
		if expr.Name == "err" && g.currentErrVar != "" {
			return g.currentErrVar
		}
		// Implicit self: bare field name → s.Field in method/ctor context
		if g.currentFields != nil && g.currentFields[expr.Name] && !g.currentParams[expr.Name] {
			if goField, ok := g.currentFieldGoName[expr.Name]; ok {
				return "s." + goField
			}
			return "s." + exportName(expr.Name)
		}
		if g.renamedVars != nil {
			if renamed, ok := g.renamedVars[expr.Name]; ok {
				return renamed
			}
		}
		// Function references as values (e.g. passing addAttributeFactory to register)
		if g.isSubpackage() {
			if _, ok := g.funcSigs[expr.Name]; ok {
				return g.exportIfSubpackage(expr.Name)
			}
		}
		// Unqualified import: bare name like EQ → router.EQ, formatItem → lib.FormatItem
		if !g.isLocalVar(expr.Name) {
			if resolved, ok := g.resolveUnqualifiedExpr(expr.Name); ok {
				return resolved
			}
		}
		return expr.Name
	case *parser.IntLit:
		return expr.Value
	case *parser.FloatLit:
		return expr.Value
	case *parser.StringLit:
		if strings.Contains(expr.Value, "\n") {
			return fmt.Sprintf("`%s`", expr.Value)
		}
		return fmt.Sprintf("%q", expr.Value)
	case *parser.StringInterpLit:
		return g.formatStringInterp(expr)
	case *parser.BoolLit:
		if expr.Value {
			return "true"
		}
		return "false"
	case *parser.NullLit:
		return "nil"
	case *parser.SizedArrayExpr:
		// byte[4] → make([]byte, 4), int[10] → make([]int, 10)
		goType := expr.ElementType
		if mapped, ok := zincToGoType[goType]; ok {
			goType = mapped
		}
		return fmt.Sprintf("make([]%s, %s)", goType, g.formatExpr(expr.Size))
	case *parser.CapacityExpr:
		// List<T>(cap) → make([]T, 0, cap), Map<K,V>(cap) → make(map[K]V, cap)
		goType := g.formatType(expr.CollectionType)
		cap := g.formatExpr(expr.Capacity)
		if strings.HasPrefix(goType, "[]") {
			// List: make([]T, 0, cap) — length 0, capacity cap
			return fmt.Sprintf("make(%s, 0, %s)", goType, cap)
		}
		// Map: make(map[K]V, cap)
		return fmt.Sprintf("make(%s, %s)", goType, cap)
	case *parser.BinaryExpr:
		return g.formatBinaryExpr(expr)
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", expr.Op, g.formatExpr(expr.Operand))
	case *parser.CallExpr:
		return g.formatCallExpr(expr)
	case *parser.SelectorExpr:
		if (expr.Field == "length" || expr.Field == "size") && !g.isStructVar(expr.Object) {
			return fmt.Sprintf("len(%s)", g.formatExpr(expr.Object))
		}
		// Const field access: Config.VERSION → Config_VERSION
		if ident, ok := expr.Object.(*parser.Ident); ok {
			if cls, ok := g.structs[ident.Name]; ok {
				for _, f := range cls.Fields {
					if f.IsConst && f.Name == expr.Field {
						return fmt.Sprintf("%s_%s", ident.Name, exportName(expr.Field))
					}
				}
			}
			// Auto-import: if the identifier is a known imported package, add the import.
			// Skip when the name is shadowed by a user-scope field/param/local
			// (ZCA-10) — otherwise we'd emit a spurious import for code that
			// actually references a field/variable.
			if !g.isUserScopeShadow(ident.Name) {
				if goPath, ok := g.importMap[ident.Name]; ok {
					g.needImport(goPath)
				}
			}
		}
		return fmt.Sprintf("%s.%s", g.formatExpr(expr.Object), exportName(expr.Field))
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", g.formatExpr(expr.Object), g.formatExpr(expr.Index))
	case *parser.SliceExpr:
		low := ""
		high := ""
		if expr.Low != nil {
			low = g.formatExpr(expr.Low)
		}
		if expr.High != nil {
			high = g.formatExpr(expr.High)
		}
		return fmt.Sprintf("%s[%s:%s]", g.formatExpr(expr.Object), low, high)
	case *parser.ListLit:
		if expr.ExplicitType != nil {
			goType := g.formatType(expr.ExplicitType)
			elems := g.formatExprList(expr.Elements)
			return fmt.Sprintf("%s{%s}", goType, elems)
		}
		if len(expr.Elements) == 0 {
			return "[]interface{}{}"
		}
		elems := g.formatExprList(expr.Elements)
		elemType := inferListLitElemType(expr.Elements)
		return fmt.Sprintf("[]%s{%s}", elemType, elems)
	case *parser.MapLit:
		if expr.ExplicitType != nil {
			goType := g.formatType(expr.ExplicitType)
			if len(expr.Keys) == 0 {
				return goType + "{}"
			}
			var pairs []string
			for i := range expr.Keys {
				pairs = append(pairs, fmt.Sprintf("%s: %s", g.formatExpr(expr.Keys[i]), g.formatExpr(expr.Values[i])))
			}
			return fmt.Sprintf("%s{%s}", goType, strings.Join(pairs, ", "))
		}
		if len(expr.Keys) == 0 {
			return "map[string]interface{}{}"
		}
		var pairs []string
		for i := range expr.Keys {
			pairs = append(pairs, fmt.Sprintf("%s: %s", g.formatExpr(expr.Keys[i]), g.formatExpr(expr.Values[i])))
		}
		keyType, valType := inferMapLitType(expr.Keys, expr.Values)
		return fmt.Sprintf("map[%s]%s{%s}", keyType, valType, strings.Join(pairs, ", "))
	case *parser.LambdaExpr:
		return g.formatLambdaExpr(expr)
	case *parser.ThisExpr:
		return "s"
	case *parser.SuperCallExpr:
		return fmt.Sprintf("/* super(%s) */", g.formatExprList(expr.Args))
	case *parser.TypeAssertExpr:
		goType := g.formatType(&parser.SimpleType{Name: expr.TypeName})
		if expr.IsCheck {
			obj := g.formatExpr(expr.Object)
			g.needImport("reflect")
			return fmt.Sprintf("(reflect.TypeOf(%s).String() == \"%s\" || reflect.TypeOf(%s).Kind().String() == \"%s\")", obj, goType, obj, goType)
		}
		return fmt.Sprintf("%s.(%s)", g.formatExpr(expr.Object), goType)
	case *parser.SafeNavExpr:
		obj := g.formatExpr(expr.Object)
		deref := "*" + obj
		if expr.Call != nil {
			args := g.formatExprList(expr.Call.Args)
			field := expr.Field
			if field == "length" {
				return fmt.Sprintf("func() *int { if %s == nil { return nil }; _v := len(%s); return new(_v) }()", obj, deref)
			}
			if goFunc, ok := stringMethodMapping[field]; ok {
				g.needImport("strings")
				if args != "" {
					return fmt.Sprintf("func() *string { if %s == nil { return nil }; _v := %s(%s, %s); return new(_v) }()", obj, goFunc, deref, args)
				}
				return fmt.Sprintf("func() *string { if %s == nil { return nil }; _v := %s(%s); return new(_v) }()", obj, goFunc, deref)
			}
			return fmt.Sprintf("func() interface{} { if %s != nil { return %s.%s(%s) }; return nil }()", obj, deref, exportName(field), args)
		}
		if expr.Field == "length" {
			return fmt.Sprintf("func() *int { if %s == nil { return nil }; _v := len(%s); return new(_v) }()", obj, deref)
		}
		return fmt.Sprintf("func() interface{} { if %s != nil { return %s.%s }; return nil }()", obj, deref, exportName(expr.Field))
	case *parser.TupleLit:
		return fmt.Sprintf("[]interface{}{%s}", g.formatExprList(expr.Elements))
	case *parser.SpawnExpr:
		return "/* spawn */"
	case *parser.IfExpr:
		retType := "interface{}"
		thenType := g.inferExprType(expr.Then, g.varTypes)
		elseType := g.inferExprType(expr.Else, g.varTypes)
		if thenType != "" && thenType == elseType {
			retType = thenType
		} else if thenType != "" && thenType != "interface{}" {
			retType = thenType
		} else if elseType != "" && elseType != "interface{}" {
			retType = elseType
		}
		return fmt.Sprintf("func() %s { if %s { return %s }; return %s }()",
			retType, g.formatExpr(expr.Cond), g.formatExpr(expr.Then), g.formatExpr(expr.Else))
	case *parser.MatchExpr:
		return g.formatMatchExpr(expr)
	case *parser.RangeExpr:
		return fmt.Sprintf("/* range %s..%s */", g.formatExpr(expr.Start), g.formatExpr(expr.End))
	case *parser.RawStringLit:
		return fmt.Sprintf("`%s`", expr.Value)
	case *parser.SpreadExpr:
		return g.formatExpr(expr.Expr) + "..."
	default:
		return "/* unknown expr */"
	}
}

// --- Binary expressions ------------------------------------------------------

// goPrecedence returns the Go operator precedence for a binary operator.
// Higher number = binds tighter. Returns 0 for unknown.
func goPrecedence(op string) int {
	switch op {
	case "or", "||":
		return 1
	case "and", "&&":
		return 2
	case "==", "!=", "<", "<=", ">", ">=", "===", "!==":
		return 3
	case "+", "-":
		return 4
	case "*", "/", "%":
		return 5
	default:
		return 0
	}
}

// wrapIfLowerPrec wraps a child expression in parentheses if it is a BinaryExpr
// with lower precedence than the parent operator.
func (g *Generator) wrapIfLowerPrec(child parser.Expr, parentOp string) string {
	formatted := g.formatExpr(child)
	if bin, ok := child.(*parser.BinaryExpr); ok {
		childPrec := goPrecedence(bin.Op)
		parentPrec := goPrecedence(parentOp)
		if childPrec > 0 && parentPrec > 0 && childPrec < parentPrec {
			return "(" + formatted + ")"
		}
	}
	return formatted
}

func (g *Generator) formatBinaryExpr(b *parser.BinaryExpr) string {
	switch b.Op {
	case "and", "&&":
		left := g.wrapIfLowerPrec(b.Left, b.Op)
		right := g.wrapIfLowerPrec(b.Right, b.Op)
		return fmt.Sprintf("%s && %s", left, right)
	case "or", "||":
		left := g.wrapIfLowerPrec(b.Left, b.Op)
		right := g.wrapIfLowerPrec(b.Right, b.Op)
		return fmt.Sprintf("%s || %s", left, right)
	case "not":
		right := g.formatExpr(b.Right)
		return fmt.Sprintf("!%s", right)
	case "**":
		left := g.formatExpr(b.Left)
		right := g.formatExpr(b.Right)
		g.needImport("math")
		return fmt.Sprintf("math.Pow(float64(%s), float64(%s))", left, right)
	case "==":
		left := g.formatExpr(b.Left)
		right := g.formatExpr(b.Right)
		return fmt.Sprintf("%s == %s", left, right)
	case "!=":
		left := g.formatExpr(b.Left)
		right := g.formatExpr(b.Right)
		return fmt.Sprintf("%s != %s", left, right)
	case "===":
		left := g.formatExpr(b.Left)
		right := g.formatExpr(b.Right)
		return fmt.Sprintf("%s == %s", left, right)
	case "!==":
		left := g.formatExpr(b.Left)
		right := g.formatExpr(b.Right)
		return fmt.Sprintf("%s != %s", left, right)
	case "in":
		left := g.formatExpr(b.Left)
		right := g.formatExpr(b.Right)
		return g.formatInExpr(b.Left, b.Right, left, right)
	case "not in":
		left := g.formatExpr(b.Left)
		right := g.formatExpr(b.Right)
		return "!" + g.formatInExpr(b.Left, b.Right, left, right)
	case "is":
		left := g.formatExpr(b.Left)
		right := g.formatExpr(b.Right)
		goType := g.formatType(&parser.SimpleType{Name: right})
		knownType := g.inferExprType(b.Left, g.varTypes)
		if knownType != "" && knownType != "interface{}" && knownType == goType {
			return fmt.Sprintf("func() bool { _ = %s; return true }()", left)
		}
		g.needImport("reflect")
		return fmt.Sprintf("(reflect.TypeOf(%s).String() == \"%s\" || reflect.TypeOf(%s).Kind().String() == \"%s\")", left, goType, left, goType)
	case "is not":
		left := g.formatExpr(b.Left)
		right := g.formatExpr(b.Right)
		goType := g.formatType(&parser.SimpleType{Name: right})
		knownType := g.inferExprType(b.Left, g.varTypes)
		if knownType != "" && knownType != "interface{}" && knownType == goType {
			return fmt.Sprintf("func() bool { _ = %s; return false }()", left)
		}
		g.needImport("reflect")
		return fmt.Sprintf("!(reflect.TypeOf(%s).String() == \"%s\" || reflect.TypeOf(%s).Kind().String() == \"%s\")", left, goType, left, goType)
	default:
		left := g.formatExpr(b.Left)
		right := g.formatExpr(b.Right)
		return fmt.Sprintf("%s %s %s", left, b.Op, right)
	}
}

// formatInExpr handles the `in` operator for strings, maps, and slices.
func (g *Generator) formatInExpr(leftExpr, rightExpr parser.Expr, left, right string) string {
	if _, ok := leftExpr.(*parser.StringLit); ok {
		g.needImport("strings")
		return fmt.Sprintf("strings.Contains(%s, %s)", right, left)
	}
	return fmt.Sprintf("func() bool { for _, _v := range %s { if _v == %s { return true } }; return false }()", right, left)
}

// --- String method and stream method tables ----------------------------------

// stringMethodMapping maps Zinc string methods to Go equivalents.
var stringMethodMapping = map[string]string{
	"upper":      "strings.ToUpper",
	"lower":      "strings.ToLower",
	"trim":       "strings.TrimSpace",
	"contains":   "strings.Contains",
	"startsWith": "strings.HasPrefix",
	"endsWith":   "strings.HasSuffix",
	"split":      "strings.Split",
	"repeat":     "strings.Repeat",
	"indexOf":    "strings.Index",
}

// --- Call expressions are in codegen_calls.go --------------------------------

// --- Lambda expressions ------------------------------------------------------

func (g *Generator) formatLambdaExpr(l *parser.LambdaExpr) string {
	var params []string
	var firstParamType string
	allTyped := true
	for _, p := range l.Params {
		typeName := "interface{}"
		if p.Type != nil {
			typeName = g.formatType(p.Type)
			if firstParamType == "" {
				firstParamType = typeName
			}
		} else {
			allTyped = false
		}
		params = append(params, p.Name+" "+typeName)
	}
	paramStr := strings.Join(params, ", ")

	if l.Expr != nil {
		retType := "interface{}"
		if l.ReturnType != nil {
			retType = g.formatType(l.ReturnType)
		} else if allTyped && firstParamType != "" {
			retType = g.inferLambdaReturnType(l.Expr, l.Params)
		}
		if g.isVoidExpr(l.Expr) {
			return fmt.Sprintf("func(%s) { %s }", paramStr, g.formatExpr(l.Expr))
		}
		return fmt.Sprintf("func(%s) %s { return %s }", paramStr, retType, g.formatExpr(l.Expr))
	}
	// Block lambda
	if l.Body != nil && len(l.Body.Stmts) > 0 {
		paramTypes := map[string]string{}
		for _, p := range g.currentFuncParams {
			if p.Type != nil {
				paramTypes[p.Name] = g.formatType(p.Type)
			}
		}
		for _, p := range l.Params {
			if p.Type != nil {
				paramTypes[p.Name] = g.formatType(p.Type)
			}
		}

		blockRetType := ""
		for _, s := range l.Body.Stmts {
			if ret, ok := s.(*parser.ReturnStmt); ok && ret.Value != nil {
				blockRetType = g.inferExprType(ret.Value, paramTypes)
				break
			}
		}

		var stmts []string
		for _, s := range l.Body.Stmts {
			stmts = append(stmts, g.formatStmtInline(s))
		}

		if blockRetType != "" && blockRetType != "interface{}" {
			return fmt.Sprintf("func(%s) %s { %s }", paramStr, blockRetType, strings.Join(stmts, "; "))
		}
		return fmt.Sprintf("func(%s) { %s }", paramStr, strings.Join(stmts, "; "))
	}
	return fmt.Sprintf("func(%s) {}", paramStr)
}

// inferLambdaReturnType infers the return type of a lambda.
func (g *Generator) inferLambdaReturnType(expr parser.Expr, params []*parser.ParamDecl) string {
	paramTypes := map[string]string{}
	for _, p := range g.currentFuncParams {
		if p.Type != nil {
			paramTypes[p.Name] = g.formatType(p.Type)
		}
	}
	for _, p := range params {
		if p.Type != nil {
			paramTypes[p.Name] = g.formatType(p.Type)
		}
	}
	return g.inferExprType(expr, paramTypes)
}

// inferExprType infers the Go type of an expression given known variable types.
func (g *Generator) inferExprType(expr parser.Expr, known map[string]string) string {
	switch e := expr.(type) {
	case *parser.IntLit:
		return "int"
	case *parser.FloatLit:
		return "float64"
	case *parser.StringLit, *parser.StringInterpLit:
		return "string"
	case *parser.BoolLit:
		return "bool"
	case *parser.Ident:
		if t, ok := known[e.Name]; ok {
			return t
		}
	case *parser.BinaryExpr:
		lt := g.inferExprType(e.Left, known)
		rt := g.inferExprType(e.Right, known)
		if lt == rt && lt != "" {
			switch e.Op {
			case "+", "-", "*", "/", "%":
				return lt
			case ">", "<", ">=", "<=", "==", "!=":
				return "bool"
			}
		}
		if lt == "int" || rt == "int" {
			switch e.Op {
			case "+", "-", "*", "/", "%":
				return "int"
			}
		}
		if lt == "string" || rt == "string" {
			if e.Op == "+" {
				return "string"
			}
		}
	case *parser.CallExpr:
		if ident, ok := e.Callee.(*parser.Ident); ok {
			if rt, ok := g.funcReturnTypes[ident.Name]; ok {
				return rt
			}
			if t, ok := known[ident.Name]; ok {
				resolved := t
				if alias, ok := g.typeAliases[t]; ok {
					resolved = g.formatType(alias)
				}
				if strings.HasPrefix(resolved, "func(") {
					if idx := strings.LastIndex(resolved, ") "); idx >= 0 {
						return strings.TrimSpace(resolved[idx+2:])
					}
				}
			}
			switch ident.Name {
			case "len":
				return "int"
			case "str":
				return "string"
			}
		}
	}
	return "interface{}"
}

// isVoidExpr checks if an expression is a void call (print, etc.)
func (g *Generator) isVoidExpr(expr parser.Expr) bool {
	if call, ok := expr.(*parser.CallExpr); ok {
		if ident, ok := call.Callee.(*parser.Ident); ok {
			switch ident.Name {
			case "print", "println":
				return true
			}
		}
	}
	return false
}

// --- String interpolation ----------------------------------------------------

func (g *Generator) formatStringInterp(s *parser.StringInterpLit) string {
	g.needImport("fmt")
	var fmtStr strings.Builder
	var args []string
	for _, p := range s.Parts {
		switch part := p.(type) {
		case *parser.StringLit:
			escaped := strings.ReplaceAll(part.Value, "%", "%%")
			fmtStr.WriteString(escaped)
		default:
			fmtStr.WriteString("%v")
			expr := g.formatExpr(part)
			isPtr := false
			if ident, ok := part.(*parser.Ident); ok {
				isPtr = g.ptrVars[ident.Name]
			}
			if isPtr {
				expr = fmt.Sprintf("func() interface{} { if %s != nil { return *%s }; return \"null\" }()", expr, expr)
			}
			args = append(args, expr)
		}
	}
	if len(args) == 0 {
		return fmt.Sprintf("%q", fmtStr.String())
	}
	return fmt.Sprintf("fmt.Sprintf(%q, %s)", fmtStr.String(), strings.Join(args, ", "))
}

// formatPrintf returns the format string and args separately for fmt.Printf.
func (g *Generator) formatPrintf(s *parser.StringInterpLit) (string, []string) {
	var fmtStr strings.Builder
	var args []string
	for _, p := range s.Parts {
		switch part := p.(type) {
		case *parser.StringLit:
			escaped := strings.ReplaceAll(part.Value, "%", "%%")
			fmtStr.WriteString(escaped)
		default:
			fmtStr.WriteString("%v")
			expr := g.formatExpr(part)
			isPtr := false
			if ident, ok := part.(*parser.Ident); ok {
				isPtr = g.ptrVars[ident.Name]
			}
			if isPtr {
				expr = fmt.Sprintf("func() interface{} { if %s != nil { return *%s }; return \"null\" }()", expr, expr)
			}
			args = append(args, expr)
		}
	}
	return fmtStr.String(), args
}

// formatMatchExpr generates a Go switch expression via IIFE.
func (g *Generator) formatMatchExpr(m *parser.MatchExpr) string {
	var sb strings.Builder
	sb.WriteString("func() interface{} { switch ")
	sb.WriteString(g.formatExpr(m.Subject))
	sb.WriteString(" { ")
	for _, c := range m.Cases {
		if c.Pattern == nil {
			sb.WriteString(fmt.Sprintf("default: return %s; ", g.formatExpr(c.Value)))
		} else {
			sb.WriteString(fmt.Sprintf("case %s: return %s; ", g.formatExpr(c.Pattern), g.formatExpr(c.Value)))
		}
	}
	sb.WriteString("}; return nil }()")
	return sb.String()
}

// --- Inline statement formatting ---------------------------------------------

func (g *Generator) formatStmtInline(s parser.Stmt) string {
	switch stmt := s.(type) {
	case *parser.VarStmt:
		if stmt.Value != nil {
			return fmt.Sprintf("%s := %s", stmt.Name, g.formatExpr(stmt.Value))
		}
		return fmt.Sprintf("var %s interface{}", stmt.Name)
	case *parser.AssignStmt:
		return fmt.Sprintf("%s %s %s", g.formatExpr(stmt.Target), stmt.Op, g.formatExpr(stmt.Value))
	case *parser.ExprStmt:
		if call, ok := stmt.Expr.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "print" && len(call.Args) == 1 {
				if interp, ok := call.Args[0].(*parser.StringInterpLit); ok {
					g.needImport("fmt")
					fmtStr, args := g.formatPrintf(interp)
					if len(args) > 0 {
						return fmt.Sprintf("fmt.Printf(%q, %s)", fmtStr+"\n", strings.Join(args, ", "))
					}
					return fmt.Sprintf("fmt.Println(%q)", fmtStr)
				}
			}
		}
		return g.formatExpr(stmt.Expr)
	case *parser.ReturnStmt:
		if stmt.Value != nil {
			return "return " + g.formatExpr(stmt.Value)
		}
		return "return"
	default:
		return "/* inline stmt */"
	}
}

// --- `it` keyword helpers ----------------------------------------------------

// containsIt checks if an expression uses the implicit `it` parameter.
func containsIt(e parser.Expr) bool {
	switch expr := e.(type) {
	case *parser.Ident:
		return expr.Name == "it"
	case *parser.BinaryExpr:
		return containsIt(expr.Left) || containsIt(expr.Right)
	case *parser.UnaryExpr:
		return containsIt(expr.Operand)
	case *parser.SelectorExpr:
		return containsIt(expr.Object)
	case *parser.CallExpr:
		if containsIt(expr.Callee) {
			return true
		}
		for _, a := range expr.Args {
			if containsIt(a) {
				return true
			}
		}
		return false
	case *parser.IndexExpr:
		return containsIt(expr.Object) || containsIt(expr.Index)
	default:
		return false
	}
}

// formatExprIt formats an expression, replacing `it` with `_it`.
func (g *Generator) formatExprIt(e parser.Expr) string {
	switch expr := e.(type) {
	case *parser.Ident:
		if expr.Name == "it" {
			return "_it"
		}
		return expr.Name
	case *parser.BinaryExpr:
		left := g.formatExprIt(expr.Left)
		right := g.formatExprIt(expr.Right)
		switch expr.Op {
		case "and":
			return fmt.Sprintf("%s && %s", left, right)
		case "or":
			return fmt.Sprintf("%s || %s", left, right)
		default:
			return fmt.Sprintf("%s %s %s", left, expr.Op, right)
		}
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", expr.Op, g.formatExprIt(expr.Operand))
	case *parser.SelectorExpr:
		obj := g.formatExprIt(expr.Object)
		if expr.Field == "length" || expr.Field == "size" {
			return fmt.Sprintf("len(%s)", obj)
		}
		return fmt.Sprintf("%s.%s", obj, expr.Field)
	case *parser.CallExpr:
		if sel, ok := expr.Callee.(*parser.SelectorExpr); ok {
			obj := g.formatExprIt(sel.Object)
			var itArgs []string
			for _, a := range expr.Args {
				itArgs = append(itArgs, g.formatExprIt(a))
			}
			// Check if the object is a known struct variable — if so, call its method
			isStruct := g.isStructVar(sel.Object)
			switch sel.Field {
			case "length", "size":
				if isStruct {
					return fmt.Sprintf("%s.%s()", obj, exportName(sel.Field))
				}
				return fmt.Sprintf("len(%s)", obj)
			case "charAt":
				if len(itArgs) > 0 {
					return fmt.Sprintf("string(%s[%s])", obj, itArgs[0])
				}
			case "substring":
				if len(itArgs) == 2 {
					return fmt.Sprintf("%s[%s:%s]", obj, itArgs[0], itArgs[1])
				}
				if len(itArgs) == 1 {
					return fmt.Sprintf("%s[%s:]", obj, itArgs[0])
				}
			case "upper":
				g.needImport("strings")
				return fmt.Sprintf("strings.ToUpper(%s)", obj)
			case "lower":
				g.needImport("strings")
				return fmt.Sprintf("strings.ToLower(%s)", obj)
			}
			if goFunc, ok := stringMethodMapping[sel.Field]; ok {
				g.needImport("strings")
				if len(itArgs) > 0 {
					return fmt.Sprintf("%s(%s, %s)", goFunc, obj, strings.Join(itArgs, ", "))
				}
				return fmt.Sprintf("%s(%s)", goFunc, obj)
			}
			return fmt.Sprintf("%s.%s(%s)", obj, exportName(sel.Field), strings.Join(itArgs, ", "))
		}
		callee := g.formatExprIt(expr.Callee)
		var args []string
		for _, a := range expr.Args {
			args = append(args, g.formatExprIt(a))
		}
		if ident, ok := expr.Callee.(*parser.Ident); ok {
			switch ident.Name {
			case "print":
				g.needImport("fmt")
				return fmt.Sprintf("fmt.Println(%s)", strings.Join(args, ", "))
			case "len":
				return fmt.Sprintf("len(%s)", strings.Join(args, ", "))
			}
		}
		return fmt.Sprintf("%s(%s)", callee, strings.Join(args, ", "))
	default:
		return g.formatExpr(e)
	}
}
