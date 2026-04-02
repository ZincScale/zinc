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
			return "s." + exportName(expr.Name)
		}
		if g.renamedVars != nil {
			if renamed, ok := g.renamedVars[expr.Name]; ok {
				return renamed
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
			// Auto-import: if the identifier is a known imported package, add the import
			if goPath, ok := g.importMap[ident.Name]; ok {
				g.needImport(goPath)
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

func (g *Generator) formatBinaryExpr(b *parser.BinaryExpr) string {
	left := g.formatExpr(b.Left)
	right := g.formatExpr(b.Right)

	switch b.Op {
	case "and", "&&":
		return fmt.Sprintf("%s && %s", left, right)
	case "or", "||":
		return fmt.Sprintf("%s || %s", left, right)
	case "not":
		return fmt.Sprintf("!%s", right)
	case "**":
		g.needImport("math")
		return fmt.Sprintf("math.Pow(float64(%s), float64(%s))", left, right)
	case "==":
		return fmt.Sprintf("%s == %s", left, right)
	case "!=":
		return fmt.Sprintf("%s != %s", left, right)
	case "===":
		return fmt.Sprintf("%s == %s", left, right)
	case "!==":
		return fmt.Sprintf("%s != %s", left, right)
	case "in":
		return g.formatInExpr(b.Left, b.Right, left, right)
	case "not in":
		return "!" + g.formatInExpr(b.Left, b.Right, left, right)
	case "is":
		goType := g.formatType(&parser.SimpleType{Name: right})
		knownType := g.inferExprType(b.Left, g.varTypes)
		if knownType != "" && knownType != "interface{}" && knownType == goType {
			return fmt.Sprintf("func() bool { _ = %s; return true }()", left)
		}
		g.needImport("reflect")
		return fmt.Sprintf("(reflect.TypeOf(%s).String() == \"%s\" || reflect.TypeOf(%s).Kind().String() == \"%s\")", left, goType, left, goType)
	case "is not":
		goType := g.formatType(&parser.SimpleType{Name: right})
		knownType := g.inferExprType(b.Left, g.varTypes)
		if knownType != "" && knownType != "interface{}" && knownType == goType {
			return fmt.Sprintf("func() bool { _ = %s; return false }()", left)
		}
		g.needImport("reflect")
		return fmt.Sprintf("!(reflect.TypeOf(%s).String() == \"%s\" || reflect.TypeOf(%s).Kind().String() == \"%s\")", left, goType, left, goType)
	default:
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

// streamMethods is the set of methods that trigger stream/inline-loop codegen.
var streamMethods = map[string]bool{
	"filter": true, "map": true, "sum": true,
	"anyMatch": true, "allMatch": true, "noneMatch": true,
	"findFirst": true, "skip": true, "limit": true,
	"distinct": true, "reduce": true, "forEach": true,
	"sortBy": true, "groupBy": true,
}

// --- Call expressions --------------------------------------------------------

// callReturnsPointer checks if a call expression returns a pointer type,
// using the GoTypeResolver to inspect the function/method signature.
func (g *Generator) callReturnsPointer(c *parser.CallExpr) bool {
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if ident, ok := sel.Object.(*parser.Ident); ok {
			// Package function: pkg.Func()
			if pkgPath, ok := g.importMap[ident.Name]; ok {
				return g.goResolver.FuncReturnsPointer(pkgPath, sel.Field)
			}
			// Method on tracked variable: obj.Method()
			if goType, ok := g.varGoTypes[ident.Name]; ok {
				return g.goResolver.ExprReturnsPointer("", sel.Field, goType)
			}
		}
	}
	// Zinc constructors (NewType) return pointers for classes
	if ident, ok := c.Callee.(*parser.Ident); ok {
		if _, isStruct := g.structs[ident.Name]; isStruct {
			if !g.dataClasses[ident.Name] {
				return true // zinc class constructors return *Type
			}
		}
	}
	return false
}

func (g *Generator) formatCallExpr(c *parser.CallExpr) string {
	// Zinc subpackage or import alias qualified calls:
	//   core.FlowFile(...) → core.NewFlowFile(...)
	//   logging.Logger(...) → logging.NewLogger(...)  (from import alias)
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if ident, ok := sel.Object.(*parser.Ident); ok && (g.isZincSubpackage(ident.Name) || g.isImportAlias(ident.Name)) {
			pkg := ident.Name
			name := sel.Field
			if goPath, ok := g.importMap[pkg]; ok {
				g.needImport(goPath)
			}
			args := g.formatExprList(c.Args)

			// Check what kind of export this is
			kind := ""
			if exports, ok := g.subpkgExports[pkg]; ok {
				kind = exports[name]
			}

			// For import aliases (external deps), check Go type info.
			// IsStruct and HasFunc fall back to AST parsing when type
			// resolution fails (e.g., transitive deps not available).
			if kind == "" && g.isImportAlias(ident.Name) {
				if goPath, ok := g.importMap[pkg]; ok {
					if g.goResolver.IsStruct(goPath, name) {
						kind = "class"
					} else if g.goResolver.HasFunc(goPath, "New"+name) {
						kind = "class"
					}
				}
			}

			// Format Go type args if present
			goTypeArgStr := ""
			if len(c.TypeArgs) > 0 {
				var goTA []string
				for _, ta := range c.TypeArgs {
					if mapped, ok := zincToGoType[ta]; ok {
						goTA = append(goTA, mapped)
					} else {
						goTA = append(goTA, ta)
					}
				}
				goTypeArgStr = "[" + strings.Join(goTA, ", ") + "]"
			}

			switch kind {
			case "data", "class":
				// Constructor: core.FlowFile(...) → core.NewFlowFile(...)
				return fmt.Sprintf("%s.New%s%s(%s)", pkg, name, goTypeArgStr, args)
			default:
				// Function or unknown: core.greet(...) → core.Greet(...)
				return fmt.Sprintf("%s.%s%s(%s)", pkg, exportName(name), goTypeArgStr, args)
			}
		}
	}

	// String method rewrites
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if goFunc, ok := stringMethodMapping[sel.Field]; ok {
			g.needImport("strings")
			obj := g.formatExpr(sel.Object)
			args := g.formatExprList(c.Args)
			if args != "" {
				return fmt.Sprintf("%s(%s, %s)", goFunc, obj, args)
			}
			return fmt.Sprintf("%s(%s)", goFunc, obj)
		}

		// Stream operations
		if streamMethods[sel.Field] {
			return g.formatStreamExpr(sel, c.Args)
		}

		// Collection methods
		obj := g.formatExpr(sel.Object)
		switch sel.Field {
		case "add":
			if !g.isStructVar(sel.Object) {
				args := g.formatExprList(c.Args)
				return fmt.Sprintf("append(%s, %s)", obj, args)
			}
		case "put":
			if len(c.Args) == 2 && !g.isStructVar(sel.Object) {
				return fmt.Sprintf("func() { %s[%s] = %s }()", obj, g.formatExpr(c.Args[0]), g.formatExpr(c.Args[1]))
			}
		case "send":
			if len(c.Args) == 1 && !g.isStructVar(sel.Object) {
				return fmt.Sprintf("func() { %s <- %s }()", obj, g.formatExpr(c.Args[0]))
			}
		case "recv":
			if !g.isStructVar(sel.Object) {
				// Add type assertion only for untyped channels (chan interface{}).
				// Typed channels (chan Job, chan FlowFile) already return the correct type.
				if g.currentMethodRetType != "" && g.currentMethodRetType != "interface{}" {
					// Check if the channel variable has a known element type
					chanElemType := g.varTypes[obj]
					if chanElemType == "" || chanElemType == "interface{}" {
						return fmt.Sprintf("(<-%s).(%s)", obj, g.currentMethodRetType)
					}
				}
				return fmt.Sprintf("<-%s", obj)
			}
		case "close":
			if !g.isStructVar(sel.Object) {
				return fmt.Sprintf("close(%s)", obj)
			}
		case "size":
			if g.isStructVar(sel.Object) {
				args := g.formatExprList(c.Args)
				return fmt.Sprintf("%s.%s(%s)", obj, exportName(sel.Field), args)
			}
			return fmt.Sprintf("len(%s)", obj)
		case "isEmpty":
			if g.isStructVar(sel.Object) {
				args := g.formatExprList(c.Args)
				return fmt.Sprintf("%s.%s(%s)", obj, exportName(sel.Field), args)
			}
			return fmt.Sprintf("len(%s) == 0", obj)
		case "length":
			if g.isStructVar(sel.Object) {
				args := g.formatExprList(c.Args)
				return fmt.Sprintf("%s.%s(%s)", obj, exportName(sel.Field), args)
			}
			return fmt.Sprintf("len(%s)", obj)
		case "toBytes":
			return fmt.Sprintf("[]byte(%s)", obj)
		case "charAt":
			return fmt.Sprintf("string(%s[%s])", obj, g.formatExprList(c.Args))
		case "substring":
			args := c.Args
			if len(args) == 2 {
				return fmt.Sprintf("%s[%s:%s]", obj, g.formatExpr(args[0]), g.formatExpr(args[1]))
			}
			return fmt.Sprintf("%s[%s:]", obj, g.formatExpr(args[0]))
		case "replace":
			g.needImport("strings")
			if len(c.Args) == 2 {
				return fmt.Sprintf("strings.ReplaceAll(%s, %s, %s)", obj, g.formatExpr(c.Args[0]), g.formatExpr(c.Args[1]))
			}
		case "trimStart":
			g.needImport("strings")
			return fmt.Sprintf("strings.TrimLeft(%s, \" \\t\\n\\r\")", obj)
		case "trimEnd":
			g.needImport("strings")
			return fmt.Sprintf("strings.TrimRight(%s, \" \\t\\n\\r\")", obj)
		case "upper":
			g.needImport("strings")
			return fmt.Sprintf("strings.ToUpper(%s)", obj)
		case "lower":
			g.needImport("strings")
			return fmt.Sprintf("strings.ToLower(%s)", obj)
		case "entrySet":
			return obj
		case "getKey":
			return obj + ".Key"
		case "getValue":
			return obj + ".Value"
		case "join":
			g.needImport("strings")
			if len(c.Args) == 1 {
				return fmt.Sprintf("strings.Join(%s, %s)", obj, g.formatExpr(c.Args[0]))
			}
			return fmt.Sprintf("strings.Join(%s, \"\")", obj)
		case "keys":
			keyType := "interface{}"
			if te, ok := g.varTypeExprs[obj]; ok {
				if gt, ok := te.(*parser.GenericType); ok && gt.Name == "Map" && len(gt.TypeArgs) >= 1 {
					keyType = g.formatType(gt.TypeArgs[0])
				}
			}
			return fmt.Sprintf("func() []%s { _keys := make([]%s, 0, len(%s)); for _k := range %s { _keys = append(_keys, _k) }; return _keys }()", keyType, keyType, obj, obj)
		case "values":
			valType := "interface{}"
			if te, ok := g.varTypeExprs[obj]; ok {
				if gt, ok := te.(*parser.GenericType); ok && gt.Name == "Map" && len(gt.TypeArgs) >= 2 {
					valType = g.formatType(gt.TypeArgs[1])
				}
			}
			return fmt.Sprintf("func() []%s { _vals := make([]%s, 0, len(%s)); for _, _v := range %s { _vals = append(_vals, _v) }; return _vals }()", valType, valType, obj, obj)
		case "containsKey":
			if len(c.Args) == 1 {
				return fmt.Sprintf("func() bool { _, _ok := %s[%s]; return _ok }()", obj, g.formatExpr(c.Args[0]))
			}
		case "remove":
			if len(c.Args) == 1 {
				return fmt.Sprintf("delete(%s, %s)", obj, g.formatExpr(c.Args[0]))
			}
		case "sort":
			g.needImport("sort")
			return fmt.Sprintf("func() { sort.Slice(%s, func(i, j int) bool { return %s[i] < %s[j] }) }()", obj, obj, obj)
		case "reverse":
			return fmt.Sprintf("func() { for _i, _j := 0, len(%s)-1; _i < _j; _i, _j = _i+1, _j-1 { %s[_i], %s[_j] = %s[_j], %s[_i] } }()", obj, obj, obj, obj, obj)
		default:
			// Getter pattern: obj.getHost() → obj.Host
			if strings.HasPrefix(sel.Field, "get") && len(sel.Field) > 3 && len(c.Args) == 0 {
				fieldName := strings.ToLower(sel.Field[3:4]) + sel.Field[4:]
				if ident, ok := sel.Object.(*parser.Ident); ok {
					if structName, ok := g.varStructTypes[ident.Name]; ok {
						if cls, ok := g.structs[structName]; ok {
							for _, f := range cls.Fields {
								if f.Name == fieldName {
									return fmt.Sprintf("%s.%s", obj, exportName(fieldName))
								}
							}
						}
					}
				}
			}
		}
	}

	// Go struct literal: pkg.Type(Field=value) → pkg.Type{Field: value}
	// Detected when callee is pkg.Name, Name starts uppercase, has named args,
	// and Name is a struct (not a function) in the Go package.
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok && len(c.NamedArgs) > 0 {
		if ident, ok := sel.Object.(*parser.Ident); ok {
			if goPath, ok := g.importMap[ident.Name]; ok {
				if g.goResolver.IsStruct(goPath, sel.Field) {
					g.needImport(goPath)
					var fields []string
					for _, na := range c.NamedArgs {
						fields = append(fields, fmt.Sprintf("%s: %s", exportName(na.Name), g.formatExpr(na.Value)))
					}
					return fmt.Sprintf("%s.%s{%s}", ident.Name, sel.Field, strings.Join(fields, ", "))
				}
			}
		}
	}

	callee := g.formatExpr(c.Callee)

	// Set.of(...)
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if ident, ok := sel.Object.(*parser.Ident); ok && ident.Name == "Set" && sel.Field == "of" {
			var elems []string
			for _, a := range c.Args {
				elems = append(elems, fmt.Sprintf("%s: {}", g.formatExpr(a)))
			}
			return fmt.Sprintf("map[interface{}]struct{}{%s}", strings.Join(elems, ", "))
		}
	}

	// Implicit self method calls
	if ident, ok := c.Callee.(*parser.Ident); ok && g.currentMethods != nil {
		if g.currentMethods[ident.Name] {
			callee = "s." + exportName(ident.Name)
		}
		if strings.HasPrefix(ident.Name, "get") && len(ident.Name) > 3 {
			fieldName := strings.ToLower(ident.Name[3:4]) + ident.Name[4:]
			if g.currentFields != nil && g.currentFields[fieldName] {
				return "s." + exportName(fieldName)
			}
		}
	}

	// Resolve Go function's expected param types for callback adaptation and pointer inference
	var goExpectedParams [][]string
	var goPkgPath string
	var goFuncName string
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if ident, ok := sel.Object.(*parser.Ident); ok {
			if pkgPath, ok := g.importMap[ident.Name]; ok {
				goPkgPath = pkgPath
				goFuncName = sel.Field
				goExpectedParams = make([][]string, len(c.Args))
				for i := range c.Args {
					goExpectedParams[i] = g.goResolver.FuncParamCallbackSignature(pkgPath, sel.Field, i)
				}
			}
		}
	}

	// Rewrite `it` keyword in args + adapt callback signatures + auto-insert & for pointer params
	var argStrs []string
	for i, arg := range c.Args {
		if containsIt(arg) {
			argStrs = append(argStrs, g.formatExprIt(arg))
		} else if ident, ok := arg.(*parser.Ident); ok && goExpectedParams != nil && goExpectedParams[i] != nil {
			argStrs = append(argStrs, g.adaptCallback(ident.Name, goExpectedParams[i]))
		} else {
			formatted := g.formatExpr(arg)
			// Auto-insert & when Go function expects a pointer parameter
			// (explicit *T in signature or implicit via known table)
			if goPkgPath != "" && g.goResolver.NeedsPointerArg(goPkgPath, goFuncName, i) {
				// Don't add & if the argument already produces a pointer:
				// - nil is already a valid nil pointer
				// - function calls that return pointers (e.g. slog.New() returns *Logger)
				alreadyPointer := formatted == "nil"
				if !alreadyPointer {
					if callArg, ok := arg.(*parser.CallExpr); ok {
						alreadyPointer = g.callReturnsPointer(callArg)
					}
				}
				if !alreadyPointer {
					formatted = "&" + formatted
				}
			}
			argStrs = append(argStrs, formatted)
		}
	}
	for _, na := range c.NamedArgs {
		argStrs = append(argStrs, g.formatExpr(na.Value))
	}
	args := strings.Join(argStrs, ", ")

	// Default parameters
	if ident, ok := c.Callee.(*parser.Ident); ok {
		args = g.fillDefaultArgs(ident.Name, c.Args, c.NamedArgs, args)
	}

	// Builtin rewrites
	switch callee {
	case "print":
		g.needImport("fmt")
		return fmt.Sprintf("fmt.Println(%s)", args)
	case "len":
		return fmt.Sprintf("len(%s)", args)
	case "str":
		g.needImport("fmt")
		return fmt.Sprintf("fmt.Sprint(%s)", args)
	case "int":
		// If the argument is a string literal or known string type, use strconv.Atoi.
		// Otherwise, emit a Go type conversion: int(expr).
		if len(c.Args) == 1 {
			argType := g.inferExprType(c.Args[0], g.varTypes)
			if argType == "string" {
				g.needImport("strconv")
				return fmt.Sprintf("strconv.Atoi(%s)", args)
			}
		}
		return fmt.Sprintf("int(%s)", args)
	case "float":
		if len(c.Args) == 1 {
			argType := g.inferExprType(c.Args[0], g.varTypes)
			if argType == "string" {
				g.needImport("strconv")
				return fmt.Sprintf("strconv.ParseFloat(%s, 64)", args)
			}
		}
		return fmt.Sprintf("float64(%s)", args)
	case "input":
		g.needImport("fmt")
		return fmt.Sprintf("func() string { var s string; fmt.Scanln(&s); return s }()")
	case "make":
		return fmt.Sprintf("make(%s)", args)
	case "delete":
		return fmt.Sprintf("delete(%s)", args)
	case "append":
		return fmt.Sprintf("append(%s)", args)
	case "close":
		return fmt.Sprintf("close(%s)", args)
	}

	// Channel constructor
	if callee == "Channel" || callee == "channel" || callee == "Chan" {
		chanType := "interface{}"
		if len(c.TypeArgs) > 0 {
			if mapped, ok := zincToGoType[c.TypeArgs[0]]; ok {
				chanType = mapped
			} else {
				chanType = c.TypeArgs[0]
			}
		}
		if args != "" {
			return fmt.Sprintf("make(chan %s, %s)", chanType, args)
		}
		return fmt.Sprintf("make(chan %s)", chanType)
	}

	// Format Go type arguments from Zinc type args (e.g. <String, int> → [string, int])
	goTypeArgStr := ""
	if len(c.TypeArgs) > 0 {
		var goTA []string
		for _, ta := range c.TypeArgs {
			if mapped, ok := zincToGoType[ta]; ok {
				goTA = append(goTA, mapped)
			} else {
				goTA = append(goTA, ta)
			}
		}
		goTypeArgStr = "[" + strings.Join(goTA, ", ") + "]"
	}

	// Constructor calls: new Type() → NewType()
	// Regular class constructors return *Type, so dereference for value semantics.
	// Data class constructors return Type (by value), no dereference needed.
	if c.IsNew {
		ctorName := "New" + callee + goTypeArgStr
		args = g.fillDefaultArgs("New"+callee, c.Args, c.NamedArgs, args)
		return fmt.Sprintf("%s(%s)", ctorName, args)
	}

	// Implicit constructor: Type(args) → NewType(args) when Type is known
	if ident, ok := c.Callee.(*parser.Ident); ok {
		if _, isStruct := g.structs[ident.Name]; isStruct {
			ctorName := "New" + ident.Name + goTypeArgStr
			args = g.fillDefaultArgs("New"+ident.Name, c.Args, c.NamedArgs, args)
			return fmt.Sprintf("%s(%s)", ctorName, args)
		}
		if g.dataClasses[ident.Name] {
			ctorName := "New" + ident.Name + goTypeArgStr
			args = g.fillDefaultArgs("New"+ident.Name, c.Args, c.NamedArgs, args)
			return fmt.Sprintf("%s(%s)", ctorName, args)
		}
	}

	// In subpackages, export plain function calls (same-package)
	if g.isSubpackage() {
		if ident, ok := c.Callee.(*parser.Ident); ok {
			if _, ok := g.funcSigs[ident.Name]; ok {
				callee = exportName(ident.Name)
			}
		}
	}

	return fmt.Sprintf("%s%s(%s)", callee, goTypeArgStr, args)
}

// --- Callback adaptation -----------------------------------------------------

// adaptCallback generates an adapter for a Zinc function being passed to a Go
// function that expects a specific callback signature.
func (g *Generator) adaptCallback(funcName string, goExpectedTypes []string) string {
	zincParams, ok := g.funcSigs[funcName]
	if !ok || len(zincParams) != len(goExpectedTypes) {
		return funcName
	}

	needsAdapter := false
	for i, expected := range goExpectedTypes {
		zincType := "interface{}"
		if zincParams[i].Type != nil {
			zincType = g.formatType(zincParams[i].Type)
		}
		normalizedExpected := expected
		if idx := strings.LastIndex(expected, "/"); idx >= 0 {
			prefix := ""
			if strings.HasPrefix(expected, "*") {
				prefix = "*"
				expected = expected[1:]
			}
			if idx := strings.LastIndex(expected, "/"); idx >= 0 {
				normalizedExpected = prefix + expected[idx+1:]
			}
		}
		if normalizedExpected != zincType {
			needsAdapter = true
			break
		}
	}

	if !needsAdapter {
		return funcName
	}

	var adapterParams []string
	var callArgs []string
	for i, expected := range goExpectedTypes {
		paramName := fmt.Sprintf("_p%d", i)
		goType := expected
		isPointer := strings.HasPrefix(goType, "*")
		if idx := strings.LastIndex(goType, "/"); idx >= 0 {
			prefix := ""
			rest := goType
			if strings.HasPrefix(goType, "*") {
				prefix = "*"
				rest = goType[1:]
			}
			if idx := strings.LastIndex(rest, "/"); idx >= 0 {
				goType = prefix + rest[idx+1:]
			}
		}
		adapterParams = append(adapterParams, paramName+" "+goType)

		zincType := "interface{}"
		if i < len(zincParams) && zincParams[i].Type != nil {
			zincType = g.formatType(zincParams[i].Type)
		}
		zincIsPointer := strings.HasPrefix(zincType, "*")
		if isPointer && !zincIsPointer {
			callArgs = append(callArgs, "*"+paramName)
		} else if !isPointer && zincIsPointer {
			callArgs = append(callArgs, "&"+paramName)
		} else {
			callArgs = append(callArgs, paramName)
		}
	}

	return fmt.Sprintf("func(%s) { %s(%s) }",
		strings.Join(adapterParams, ", "),
		funcName,
		strings.Join(callArgs, ", "))
}

// fillDefaultArgs fills in missing positional args with defaults from funcSigs.
func (g *Generator) fillDefaultArgs(funcName string, posArgs []parser.Expr, namedArgs []parser.NamedArg, currentArgs string) string {
	sig, ok := g.funcSigs[funcName]
	if !ok {
		return currentArgs
	}
	totalProvided := len(posArgs) + len(namedArgs)
	if totalProvided >= len(sig) {
		return currentArgs
	}
	var parts []string
	for _, a := range posArgs {
		parts = append(parts, g.formatExpr(a))
	}
	for _, na := range namedArgs {
		parts = append(parts, g.formatExpr(na.Value))
	}
	for i := totalProvided; i < len(sig); i++ {
		if sig[i].Default != nil {
			parts = append(parts, g.formatExpr(sig[i].Default))
		}
	}
	return strings.Join(parts, ", ")
}

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
			isStructVar := false
			if ident, ok := sel.Object.(*parser.Ident); ok {
				if st, ok := g.varStructTypes[ident.Name]; ok {
					_ = st
					isStructVar = true
				}
			}
			switch sel.Field {
			case "length", "size":
				if isStructVar {
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
