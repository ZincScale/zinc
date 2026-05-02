package codegen_go

// Call expression formatting: subpackage calls, string method rewrites,
// collection methods, stream dispatch, builtins, constructors, pointer
// inference, callback adaptation, and default argument filling.

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
	"zinc-go/internal/typechecker"
)

// formatCallArgsWithPointerWrap formats a positional argument list,
// prepending `&` to any arg whose target Go param has an explicit `*T`
// in its signature (resolver says ParamIsPointer). Skips the wrap when
// the arg is already a pointer-shaped expression (`nil`, a call that
// returns a pointer, or an explicit `&x` written by the user).
//
// For Go funcs whose static signature is `any` but whose runtime
// contract requires a pointer (e.g. avro.Unmarshal), no auto-wrap
// happens here — the user must write `&x` at the call site.
//
// Used by the import-alias / zinc-subpackage call fast-path. The general
// call path (formatCallExpr's later branches) does the same wrap inline
// — keep the two in sync.
func (g *Generator) formatCallArgsWithPointerWrap(pkgPath, funcName string, args []parser.Expr, isFFI bool) string {
	if pkgPath == "" {
		return g.formatExprList(args)
	}
	out := make([]string, 0, len(args))
	for i, arg := range args {
		// Allow `&x` only when we're formatting the top-level expression
		// of a Go-library (FFI) call argument. Zinc-subpackage call args
		// pass isFFI=false, which keeps the validator strict for those.
		if isFFI {
			g.addrOfAllowed = true
		}
		formatted := g.formatExpr(arg)
		g.addrOfAllowed = false
		if g.goResolver.NeedsPointerArg(pkgPath, funcName, i) {
			alreadyPointer := formatted == "nil"
			if !alreadyPointer {
				if callArg, ok := arg.(*parser.CallExpr); ok {
					alreadyPointer = g.callReturnsPointer(callArg)
				}
			}
			if !alreadyPointer {
				if u, ok := arg.(*parser.UnaryExpr); ok && u.Op == "&" {
					alreadyPointer = true
				}
			}
			if !alreadyPointer {
				formatted = "&" + formatted
			}
		}
		out = append(out, formatted)
	}
	return strings.Join(out, ", ")
}

// callReturnsPointer checks if a call expression returns a pointer type,
// using the GoTypeResolver to inspect the function/method signature.
func (g *Generator) callReturnsPointer(c *parser.CallExpr) bool {
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if ident, ok := sel.Object.(*parser.Ident); ok {
			// Package function: pkg.Func() — guarded so a user-space
			// field/param/local with the same name as a package doesn't
			// fall into the importMap lookup (see ZCA-10).
			if !g.isUserScopeShadowIdent(ident) {
				if pkgPath, ok := g.importMap[ident.Name]; ok {
					return g.goResolver.FuncReturnsPointer(pkgPath, sel.Field)
				}
			}
			// Method on tracked variable: obj.Method().
			// Side-map first (Phase 3.4 type-tracking migration) — falls
			// back to ad-hoc varGoTypes when bind isn't on.
			if g.bound != nil {
				if t, ok := g.bound.NodeTypes[ident]; ok && t.GoType != nil {
					return g.goResolver.ExprReturnsPointer("", sel.Field, t.GoType)
				}
			}
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
	//
	// User scope (field / param / local) shadows same-named packages —
	// see isUserScopeShadow. Without this, `class Fabric { var processors = ... }`
	// in a project with a `src/processors/` sibling would misread
	// `processors.containsKey(...)` as a package call (ZCA-10).
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if ident, ok := sel.Object.(*parser.Ident); ok &&
			!g.isUserScopeShadowIdent(ident) &&
			(g.isZincSubpackage(ident.Name) || g.isImportAlias(ident.Name)) {
			pkg := ident.Name
			name := sel.Field
			goPath := ""
			if gp, ok := g.importMap[pkg]; ok {
				g.needImport(gp)
				goPath = gp
			}
			// Auto-pointer-wrap for explicit `*T` Go params. `any`-shape
			// sinks like avro.Unmarshal require the user to write `&x`
			// explicitly. isFFI is true only for import-alias calls
			// (third-party Go modules) — zinc subpackage calls don't
			// permit `&` because they aren't going across the FFI boundary.
			isFFI := g.isImportAlias(ident.Name) && !g.isZincSubpackage(ident.Name)
			args := g.formatCallArgsWithPointerWrap(goPath, name, c.Args, isFFI)

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

	// String method rewrites — guarded so a user-defined method on a class
	// with the same name (e.g., `class Text { fn upper() { ... } }`) doesn't
	// get redirected to `strings.ToUpper(obj)`.
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if goFunc, ok := stringMethodMapping[sel.Field]; ok && !g.isStructVar(sel.Object) {
			g.needImport("strings")
			obj := g.formatExpr(sel.Object)
			args := g.formatExprList(c.Args)
			if args != "" {
				return fmt.Sprintf("%s(%s, %s)", goFunc, obj, args)
			}
			return fmt.Sprintf("%s(%s)", goFunc, obj)
		}

		// Collection methods — apply builtins unless the receiver is a
		// known struct/class variable, OR a class/enum *name* (which
		// otherwise misroutes: `Math.add(2,3)` used to lower to
		// `append(Math, 2, 3)` because `Math` is a class name, not a
		// struct variable).
		recvIsClassName := false
		if id, ok := sel.Object.(*parser.Ident); ok {
			if g.isClassType(id.Name) || g.interfaces[id.Name] {
				recvIsClassName = true
			}
		}
		shouldApplyBuiltin := !g.isStructVar(sel.Object) && !recvIsClassName
		obj := g.formatExpr(sel.Object)
		switch sel.Field {
		case "add":
			if shouldApplyBuiltin {
				args := g.formatExprList(c.Args)
				return fmt.Sprintf("append(%s, %s)", obj, args)
			}
		case "put":
			if len(c.Args) == 2 && shouldApplyBuiltin {
				return fmt.Sprintf("func() { %s[%s] = %s }()", obj, g.formatExpr(c.Args[0]), g.formatExpr(c.Args[1]))
			}
		case "send":
			if len(c.Args) == 1 && shouldApplyBuiltin {
				return fmt.Sprintf("func() { %s <- %s }()", obj, g.formatExpr(c.Args[0]))
			}
		case "recv":
			if shouldApplyBuiltin {
				// Add type assertion only for untyped channels (`chan interface{}`).
				// Typed `Channel<T>(n)` already yields T on recv, so the cast
				// would be a compile error. Detect typedness by walking the
				// receiver AST (ident → local var / class field), not by
				// looking up `obj` string in varTypes (that misses fields
				// and nested expressions — ZCA-11 territory).
				isTypedChannel := false
				if gt := g.resolveReceiverGenericType(sel.Object); gt != nil &&
					(gt.Name == "Channel" || gt.Name == "Chan") && len(gt.TypeArgs) >= 1 {
					isTypedChannel = true
				}
				// Fallback: scalar-type tracking for simple local vars.
				// Side-map first; ad-hoc varTypes when bind isn't on.
				if !isTypedChannel {
					if ident, ok := sel.Object.(*parser.Ident); ok {
						if g.bound != nil {
							if t, ok := g.bound.NodeTypes[ident]; ok && t.Name != "" && t.Name != "any" {
								isTypedChannel = true
							}
						}
						if !isTypedChannel {
							elt := g.varTypes[ident.Name]
							if elt != "" && elt != "interface{}" {
								isTypedChannel = true
							}
						}
					}
				}
				if g.currentMethodRetType != "" && g.currentMethodRetType != "interface{}" && !isTypedChannel {
					return fmt.Sprintf("(<-%s).(%s)", obj, g.currentMethodRetType)
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
			if !g.isStructVar(sel.Object) {
				return fmt.Sprintf("[]byte(%s)", obj)
			}
		case "charAt":
			if !g.isStructVar(sel.Object) {
				return fmt.Sprintf("string(%s[%s])", obj, g.formatExprList(c.Args))
			}
		case "substring":
			if !g.isStructVar(sel.Object) {
				args := c.Args
				if len(args) == 2 {
					return fmt.Sprintf("%s[%s:%s]", obj, g.formatExpr(args[0]), g.formatExpr(args[1]))
				}
				return fmt.Sprintf("%s[%s:]", obj, g.formatExpr(args[0]))
			}
		case "replace":
			if !g.isStructVar(sel.Object) {
				g.needImport("strings")
				if len(c.Args) == 2 {
					return fmt.Sprintf("strings.ReplaceAll(%s, %s, %s)", obj, g.formatExpr(c.Args[0]), g.formatExpr(c.Args[1]))
				}
			}
		case "trimStart":
			if !g.isStructVar(sel.Object) {
				g.needImport("strings")
				return fmt.Sprintf("strings.TrimLeft(%s, \" \\t\\n\\r\")", obj)
			}
		case "trimEnd":
			if !g.isStructVar(sel.Object) {
				g.needImport("strings")
				return fmt.Sprintf("strings.TrimRight(%s, \" \\t\\n\\r\")", obj)
			}
		case "upper":
			if !g.isStructVar(sel.Object) {
				g.needImport("strings")
				return fmt.Sprintf("strings.ToUpper(%s)", obj)
			}
		case "lower":
			if !g.isStructVar(sel.Object) {
				g.needImport("strings")
				return fmt.Sprintf("strings.ToLower(%s)", obj)
			}
		case "entrySet":
			if !g.isStructVar(sel.Object) {
				return obj
			}
		case "getKey":
			if !g.isStructVar(sel.Object) {
				return obj + ".Key"
			}
		case "getValue":
			if !g.isStructVar(sel.Object) {
				return obj + ".Value"
			}
		case "join":
			if !g.isStructVar(sel.Object) {
				g.needImport("strings")
				if len(c.Args) == 1 {
					return fmt.Sprintf("strings.Join(%s, %s)", obj, g.formatExpr(c.Args[0]))
				}
				return fmt.Sprintf("strings.Join(%s, \"\")", obj)
			}
		case "keys":
			g.needImport("maps")
			g.needImport("slices")
			return fmt.Sprintf("slices.Collect(maps.Keys(%s))", obj)
		case "values":
			g.needImport("maps")
			g.needImport("slices")
			return fmt.Sprintf("slices.Collect(maps.Values(%s))", obj)
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
				g.needImport("slices")
				return fmt.Sprintf("slices.Sort(%s)", obj)
			}
		case "reverse":
			if !g.isStructVar(sel.Object) {
				g.needImport("slices")
				return fmt.Sprintf("slices.Reverse(%s)", obj)
			}
		default:
			// Getter pattern: obj.getHost() → obj.Host. Sugar for the
			// no-arg accessor where the method body is just `return host`.
			// Only fires when no real method with that name exists on the
			// class — otherwise a user-written `int getHost()` (which may
			// transform the field, unwrap a wrapper type like atomic.Int64,
			// or compute something derived) would be silently bypassed in
			// favor of a field access of the wrong type.
			if strings.HasPrefix(sel.Field, "get") && len(sel.Field) > 3 && len(c.Args) == 0 {
				fieldName := strings.ToLower(sel.Field[3:4]) + sel.Field[4:]
				if ident, ok := sel.Object.(*parser.Ident); ok {
					structName := ""
					// Side-map first (Phase 3.4 type-tracking migration).
					if g.bound != nil {
						if t, ok := g.bound.NodeTypes[ident]; ok && t.Name != "" && t.Name != "any" {
							structName = t.Name
						}
					}
					if structName == "" {
						structName = g.varStructTypes[ident.Name]
					}
					if cls, ok := g.structs[structName]; structName != "" && ok {
						hasMethod := false
						for _, m := range cls.Methods {
							if m.Name == sel.Field {
								hasMethod = true
								break
							}
						}
						if !hasMethod {
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
	// and Name is a struct (not a function) in the Go package. Guarded so a
	// user-space shadow of the package name doesn't trip the dispatch (ZCA-10).
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok && len(c.NamedArgs) > 0 {
		if ident, ok := sel.Object.(*parser.Ident); ok && !g.isUserScopeShadowIdent(ident) {
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
			callee = recvName + "." + methodGoName
		}
		if strings.HasPrefix(ident.Name, "get") && len(ident.Name) > 3 {
			fieldName := strings.ToLower(ident.Name[3:4]) + ident.Name[4:]
			if g.currentFields != nil && g.currentFields[fieldName] {
				if goField, ok := g.currentFieldGoName[fieldName]; ok {
					return recvName + "." + goField
				}
				return recvName + "." + exportName(fieldName)
			}
		}
	}

	// Resolve Go function's expected param types for callback adaptation and pointer inference
	var goExpectedParams [][]string
	var goPkgPath string
	var goFuncName string
	// goMethodOnFFIVar: callee is a method call on a variable whose type
	// was returned by a Go function (e.g. `dec.Decode(&raw)` where `dec`
	// came from `hambaOcf.NewDecoder(...)`). That's still an FFI seam, so
	// explicit `&x` at the top of an arg expression must be allowed —
	// even though we don't have package-level `goPkgPath` to drive auto-
	// pointer-wrap. The user is writing `&` deliberately; just don't
	// reject it.
	//
	// The varGoTypes check has to bypass `isUserScopeShadow` because the
	// shadow check itself includes varGoTypes membership (a tracked var
	// counts as user scope, which is correct for the package-call branch
	// below — `dec` is not a package). So check varGoTypes first; only
	// fall to the package branch when the receiver is unambiguously a
	// package alias.
	goMethodOnFFIVar := false
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if ident, ok := sel.Object.(*parser.Ident); ok {
			// Side-map fast path — when bound + typecheck supplied a GoType
			// for this ident's binding, the receiver is Go-typed and this
			// is an FFI method-call seam. Phase 3.5+ produces this for
			// `var dec, _ = pkg.NewDecoder(...); dec.Decode(...)` shapes.
			if g.bound != nil {
				if t, ok := g.bound.NodeTypes[ident]; ok && t.GoType != nil {
					goMethodOnFFIVar = true
				}
			}
			if !goMethodOnFFIVar {
				if _, ok := g.varGoTypes[ident.Name]; ok {
					goMethodOnFFIVar = true
				} else if !g.isUserScopeShadowIdent(ident) {
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
		}
	}

	// Resolve the called Zinc function's param types (if any) so we can
	// auto-wrap plain values as pointers when the param is `T?`. Same
	// shape as the goResolver path above but for user-declared funcs.
	var zincExpectedParams []*parser.ParamDecl
	if ident, ok := c.Callee.(*parser.Ident); ok {
		if sig, ok := g.funcSigs[ident.Name]; ok {
			zincExpectedParams = sig
		}
	}
	// Method call `obj.method(args)` — walk the receiver's class for the
	// matching method declaration and use its params. Without this, a
	// lambda passed to a method whose param is `Fn<..., (T, error)>`
	// wouldn't pick up the target hint, and the lambda body's
	// `return v, e` form wouldn't lower correctly.
	if zincExpectedParams == nil {
		if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
			if recv := g.resolveReceiverClassName(sel.Object); recv != "" {
				if cls := g.lookupClassDecl(recv); cls != nil {
					for _, m := range cls.Methods {
						if m.Name == sel.Field {
							zincExpectedParams = m.Params
							break
						}
					}
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
			// Lambda arg into a Fn-typed param slot: publish the param's
			// declared Fn type as the lambda hint so the lambda body's
			// return shape (multi-value or thrower-mode) is driven by
			// the target slot, not by inferLambdaReturnType.
			var prevTarget *parser.FuncTypeExpr
			restoreTarget := false
			if _, isLambda := arg.(*parser.LambdaExpr); isLambda &&
				zincExpectedParams != nil && i < len(zincExpectedParams) {
				if ft := g.resolveFuncTypeExpr(zincExpectedParams[i].Type); ft != nil {
					prevTarget = g.pendingLambdaTarget
					g.pendingLambdaTarget = ft
					restoreTarget = true
				}
			}
			// Permit `&x` at the top of an arg expression only when the
			// call is across a Go FFI seam — either a package-level call
			// (goPkgPath set via importMap) or a method call on a variable
			// whose type came from a Go function (goMethodOnFFIVar).
			if goPkgPath != "" || goMethodOnFFIVar {
				g.addrOfAllowed = true
			}
			formatted := g.formatExpr(arg)
			g.addrOfAllowed = false
			if restoreTarget {
				g.pendingLambdaTarget = prevTarget
			}
			// Auto-insert & when Go function expects a pointer parameter
			// (explicit *T in signature). Note: implicitPointerParams
			// has been removed — `any`-typed params now require the
			// user to write `&x` at the call site (FFI escape hatch).
			if goPkgPath != "" && g.goResolver.NeedsPointerArg(goPkgPath, goFuncName, i) {
				// Don't add & if the argument already produces a pointer:
				// - nil is already a valid nil pointer
				// - function calls that return pointers (e.g. slog.New() returns *Logger)
				// - the user already wrote an explicit `&x`
				alreadyPointer := formatted == "nil"
				if !alreadyPointer {
					if callArg, ok := arg.(*parser.CallExpr); ok {
						alreadyPointer = g.callReturnsPointer(callArg)
					}
				}
				if !alreadyPointer {
					if u, ok := arg.(*parser.UnaryExpr); ok && u.Op == "&" {
						alreadyPointer = true
					}
				}
				if !alreadyPointer {
					formatted = "&" + formatted
				}
			}
			// Auto-wrap when a Zinc function's param is `T?` and the
			// arg is a plain `T`. `&literal` is illegal in Go, so route
			// through the _zincPtr helper which uses a generic temp.
			if zincExpectedParams != nil && i < len(zincExpectedParams) &&
				isPointerOptional(zincExpectedParams[i].Type) &&
				!g.valueIsAlreadyPointer(arg) {
				if _, isNull := arg.(*parser.NullLit); !isNull {
					formatted = g.wrapAsPointer(formatted)
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
		} else {
			// Untyped Channel(N) lowers to `chan interface{}`. That's legal
			// but loses static typing — sends accept anything, recvs require
			// a type assertion. Warn the user to write Channel<T>(N).
			g.compileWarning(0, "Channel(%s) without a type argument lowers to chan interface{}; prefer Channel<T>(%s) for typed channels", args, args)
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

	// Constructor calls. Two shapes:
	//
	// 1. Zinc class — `new Stats()` → `NewStats()`. The Zinc compiler
	//    generates a NewT() factory for every class/data declaration.
	//    Same-package detection: callee has no dot AND name is in
	//    g.structs / g.dataClasses / g.unqualifiedNames.
	//
	// 2. Foreign Go-stdlib type — `new bytes.Buffer` → `&bytes.Buffer{}`.
	//    Detected by the dotted callee (`pkg.Type`). Always pointerized
	//    on the assumption that's why the user reached for `new` — Go-
	//    stdlib types with pointer-receiver methods can't be used as
	//    values (sync.Mutex, bytes.Buffer, strings.Builder, atomic.Int64).
	//    No args supported here — for args, use the foreign struct-
	//    literal form `pkg.Type(field=value, ...)` (handled elsewhere)
	//    or the package's New* factory function.
	if c.IsNew {
		if strings.Contains(callee, ".") {
			if len(c.Args) > 0 || len(c.NamedArgs) > 0 {
				// Positional args on `new pkg.Type(...)` aren't safe to
				// emit as a Go positional struct literal — Go struct
				// field order can change without source-level breakage.
				// Force the user to either use named args (handled by
				// the foreign-struct-literal codegen path) or call the
				// package's factory function explicitly.
				return fmt.Sprintf("/* new %s with args: use named-arg form pkg.Type(field=value) or pkg.NewType(args) */", callee)
			}
			// Auto-import the package if needed
			if dot := strings.Index(callee, "."); dot >= 0 {
				if goPath, ok := g.importMap[callee[:dot]]; ok {
					g.needImport(goPath)
				}
			}
			return fmt.Sprintf("&%s{}", callee+goTypeArgStr)
		}
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
		// Side-map first (Phase 3.4 resolution migration) — the bind phase
		// already resolved this ident to a definite Symbol with Pkg set
		// when it's a cross-pkg reference. Falls back to the ad-hoc
		// unqualifiedNames table when bound is unset.
		if g.bound != nil {
			if sym, ok := g.bound.Bindings[ident]; ok && sym.Pkg != "" {
				if goPath, ok := g.importMap[sym.Pkg]; ok {
					g.needImport(goPath)
				}
				switch sym.Kind {
				case typechecker.SymType, typechecker.SymClass, typechecker.SymDataClass:
					// Constructor call: pkg.NewType(args). Both classes and
					// data classes have NewType factories in Go.
					ctorName := sym.Pkg + ".New" + exportName(ident.Name) + goTypeArgStr
					return fmt.Sprintf("%s(%s)", ctorName, args)
				case typechecker.SymFn:
					return fmt.Sprintf("%s.%s%s(%s)", sym.Pkg, exportName(ident.Name), goTypeArgStr, args)
				}
			}
		}
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
