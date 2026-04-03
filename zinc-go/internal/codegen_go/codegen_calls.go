package codegen_go

// Call expression formatting: subpackage calls, string method rewrites,
// collection methods, stream dispatch, builtins, constructors, pointer
// inference, callback adaptation, and default argument filling.

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
)

// streamMethods is the set of methods that trigger stream/inline-loop codegen.
var streamMethods = map[string]bool{
	"filter": true, "map": true, "sum": true,
	"anyMatch": true, "allMatch": true, "noneMatch": true,
	"findFirst": true, "skip": true, "limit": true,
	"distinct": true, "reduce": true, "forEach": true,
	"sortBy": true, "groupBy": true,
}

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
					goTA = append(goTA, g.resolveTypeArg(ta))
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
			if !g.isStructVar(sel.Object) {
				return obj + ".Key"
			}
		case "getValue":
			if !g.isStructVar(sel.Object) {
				return obj + ".Value"
			}
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
			if len(c.Args) == 1 && !g.isStructVar(sel.Object) {
				return fmt.Sprintf("func() bool { _, _ok := %s[%s]; return _ok }()", obj, g.formatExpr(c.Args[0]))
			}
		case "remove", "delete":
			if len(c.Args) == 1 && !g.isStructVar(sel.Object) {
				return fmt.Sprintf("delete(%s, %s)", obj, g.formatExpr(c.Args[0]))
			}
		case "sort":
			if !g.isStructVar(sel.Object) {
				g.needImport("sort")
				return fmt.Sprintf("func() { sort.Slice(%s, func(i, j int) bool { return %s[i] < %s[j] }) }()", obj, obj, obj)
			}
		case "reverse":
			if !g.isStructVar(sel.Object) {
				return fmt.Sprintf("func() { for _i, _j := 0, len(%s)-1; _i < _j; _i, _j = _i+1, _j-1 { %s[_i], %s[_j] = %s[_j], %s[_i] } }()", obj, obj, obj, obj, obj)
			}
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
			// Method calls on self — look up pub status from class
			methodGoName := exportName(ident.Name) // default
			if g.isSubpackage() && g.currentClass != "" {
				methodGoName = goName(ident.Name, g.isPubMember(g.currentClass, ident.Name))
			}
			callee = "s." + methodGoName
		}
		if strings.HasPrefix(ident.Name, "get") && len(ident.Name) > 3 {
			fieldName := strings.ToLower(ident.Name[3:4]) + ident.Name[4:]
			if g.currentFields != nil && g.currentFields[fieldName] {
				if goField, ok := g.currentFieldGoName[fieldName]; ok {
					return "s." + goField
				}
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
	case "long":
		if len(c.Args) == 1 {
			argType := g.inferExprType(c.Args[0], g.varTypes)
			if argType == "string" {
				g.needImport("strconv")
				return fmt.Sprintf("strconv.ParseInt(%s, 10, 64)", args)
			}
		}
		return fmt.Sprintf("int64(%s)", args)
	case "double":
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
			chanType = g.resolveTypeArg(c.TypeArgs[0])
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
			goTA = append(goTA, g.resolveTypeArg(ta))
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
		// Unqualified import: Item(...) → lib.NewItem(...), formatItem(...) → lib.FormatItem(...)
		if entry, ok := g.unqualifiedNames[ident.Name]; ok {
			if goPath, ok := g.importMap[entry.pkg]; ok {
				g.needImport(goPath)
			}
			switch entry.kind {
			case "data", "class":
				ctorName := entry.pkg + ".New" + exportName(entry.name) + goTypeArgStr
				return fmt.Sprintf("%s(%s)", ctorName, args)
			case "func":
				return fmt.Sprintf("%s.%s%s(%s)", entry.pkg, exportName(entry.name), goTypeArgStr, args)
			}
		}
	}

	// In subpackages, apply pub visibility to plain function calls (same-package)
	if g.isSubpackage() {
		if ident, ok := c.Callee.(*parser.Ident); ok {
			if _, ok := g.funcSigs[ident.Name]; ok {
				callee = g.exportIfSubpackage(ident.Name)
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
