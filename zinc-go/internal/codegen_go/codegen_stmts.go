package codegen_go

// Statement emission: var, assign, return, if, for, while, match,
// parallel for, concurrent, spawn, defer, assert, etc.

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
)

// stmtLine returns the source line of a statement, or 0 if unknown.
// Used to emit `//line file.zn:N` directives so Go compiler errors
// report positions in the .zn source rather than the generated .go.
func stmtLine(s parser.Stmt) int {
	switch stmt := s.(type) {
	case *parser.VarStmt:
		return stmt.Line
	case *parser.AssignStmt:
		return stmt.Line
	case *parser.ReturnStmt:
		return stmt.Line
	case *parser.IfStmt:
		return stmt.Line
	case *parser.ForStmt:
		return stmt.Line
	case *parser.WhileStmt:
		return stmt.Line
	case *parser.MatchStmt:
		return stmt.Line
	case *parser.ExprStmt:
		return stmt.Line
	case *parser.PrintStmt:
		return stmt.Line
	case *parser.FnDecl:
		return stmt.Line
	case *parser.TupleVarStmt:
		return stmt.Line
	case *parser.GoStmt:
		return stmt.Line
	case *parser.ParallelForStmt:
		return stmt.Line
	case *parser.WithStmt:
		return stmt.Line
	case *parser.AssertStmt:
		return stmt.Line
	}
	return 0
}

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
		g.emitGoroutineBody(stmt.Body)
		g.indent--
		g.writeln("}()")
	case *parser.ParallelForStmt:
		g.emitParallelForStmt(stmt)
	case *parser.WithStmt:
		g.emitWithStmt(stmt)
	case *parser.DeferStmt:
		g.writeln("defer %s", g.formatExpr(stmt.Expr))
	case *parser.AssertStmt:
		g.emitAssertStmt(stmt)
	case *parser.SelectStmt:
		g.emitSelectStmt(stmt)
	case *parser.TimeoutStmt:
		g.emitTimeoutStmt(stmt)
	}
}

// emitTimeoutStmt: timeout(d) { body } [or { fallback }] →
//
//   _done := make(chan struct{})
//   go func() { defer close(_done); /* body */ }()
//   select {
//   case <-_done:
//       // body finished in time
//   case <-time.After(d):
//       /* fallback (or panic if no handler) */
//   }
//
// Body runs in a goroutine because select needs a channel signal to know
// it's complete. The handler is the user's "what to do on timeout" hook;
// without one, an uncaught timeout panics (same contract as spawn).
func (g *Generator) emitTimeoutStmt(t *parser.TimeoutStmt) {
	g.needImport("time")
	doneVar := fmt.Sprintf("_done%d", g.errVarCount)
	g.errVarCount++
	g.writeln("%s := make(chan struct{})", doneVar)
	g.writeln("go func() {")
	g.indent++
	g.writeln("defer close(%s)", doneVar)
	g.emitBlock(t.Body)
	g.indent--
	g.writeln("}()")
	g.writeln("select {")
	g.writeln("case <-%s:", doneVar)
	g.writeln("case <-time.After(%s):", g.formatExpr(t.Duration))
	g.indent++
	if t.OrHandler != nil && t.OrHandler.Body != nil {
		g.emitOrBlock(t.OrHandler.Body)
	} else {
		g.writeln("panic(\"timeout exceeded\")")
	}
	g.indent--
	g.writeln("}")
}

// emitSelectStmt translates zinc's `select { case ch.recv(): ... }` to Go's
// `select { case <-ch: ... }`. Each case maps 1:1:
//
//   case <chan>.recv():        →  case <-ch:
//   case <var> = <chan>.recv(): →  case <var> := <-ch:
//   case <chan>.send(<expr>):  →  case ch <- <expr>:
//   case _:                    →  default:
func (g *Generator) emitSelectStmt(s *parser.SelectStmt) {
	g.writeln("select {")
	for _, c := range s.Cases {
		ch := g.formatExpr(c.Channel)
		switch c.Kind {
		case "recv":
			if c.Binding != "" {
				g.writeln("case %s := <-%s:", c.Binding, ch)
			} else {
				g.writeln("case <-%s:", ch)
			}
		case "send":
			g.writeln("case %s <- %s:", ch, g.formatExpr(c.SendValue))
		}
		g.indent++
		g.emitBlock(c.Body)
		g.indent--
	}
	if s.Default != nil {
		g.writeln("default:")
		g.indent++
		g.emitBlock(s.Default)
		g.indent--
	}
	g.writeln("}")
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
	// Register the local name for the implicit-self rewrite, but only
	// AFTER the value expression has been formatted — so `var n = n + 1`
	// resolves the RHS `n` to the outer field (as Go's `n := n + 1` does)
	// before the local shadows it for subsequent statements.
	defer g.declareLocal(v.Name)
	// Direct `as` cast: `var x = expr as T` (with optional `or { }` handler).
	// Emit comma-ok directly into the var, skipping the hoist temp for a
	// cleaner output. On mismatch, run the handler if present, otherwise
	// auto-propagate via emitErrReturn (fixed-point pass marks the
	// enclosing function as a thrower because exprContainsPropagate is
	// true for `as`).
	if ta, ok := v.Value.(*parser.TypeAssertExpr); ok && !ta.IsCheck {
		g.emitTypeAssertVar(v, ta)
		return
	}
	// Explicit error propagation: `var x = call()?`.
	// Strip the PropagateExpr wrapper and fall through to the same
	// propagation codegen the implicit path uses.
	if prop, ok := v.Value.(*parser.PropagateExpr); ok {
		stripped := *v
		stripped.Value = prop.Inner
		g.emitErrorPropagatingVar(&stripped)
		return
	}
	// Nested propagation: `var x = foo(bar()?) + 1`. Hoist each `?` (and
	// any nested `as`) to a temp above this statement, then emit the var
	// with the rewritten RHS.
	if v.Value != nil && (exprContainsPropagate(v.Value) || exprContainsAsCast(v.Value)) {
		rewritten := *v
		rewritten.Value = g.hoistPropagates(v.Value)
		g.emitVarStmt(&rewritten)
		return
	}

	// Exception pivot: `var x = call(...)` where call() is a Zinc function
	// that throws (or transitively calls a thrower) auto-propagates the
	// error. Inside a try body this returns from the IIFE; otherwise it
	// returns from the enclosing thrower function.
	if v.OrHandler == nil && v.Value != nil && g.callReturnsError(v.Value) {
		g.emitErrorPropagatingVar(v)
		return
	}

	if v.OrHandler != nil && v.Value != nil {
		// var x = call() or default → error handling
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				if call.IsNew {
					if g.isClassType(ident.Name) {
						g.varStructTypes[v.Name] = ident.Name
					}
				} else if strings.HasPrefix(ident.Name, "New") {
					structName := ident.Name[3:]
					if g.isClassType(structName) {
						g.varStructTypes[v.Name] = structName
					}
				}
			}
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				if pkg, ok := sel.Object.(*parser.Ident); ok {
					if exports, ok := g.subpkgExports[pkg.Name]; ok {
						if kind := exports[sel.Field]; kind == "class" || kind == "data" {
							g.varStructTypes[v.Name] = sel.Field
						}
					}
				}
				// Track method call return type
				retType := g.resolveMethodReturnType(sel)
				if retType != "" && g.isClassType(retType) {
					g.varStructTypes[v.Name] = retType
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

		// Track class types from constructor calls
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				if call.IsNew {
					if g.isClassType(ident.Name) {
						g.varStructTypes[v.Name] = ident.Name
					}
				} else if strings.HasPrefix(ident.Name, "New") {
					structName := ident.Name[3:]
					if g.isClassType(structName) {
						g.varStructTypes[v.Name] = structName
					}
				} else if g.isClassType(ident.Name) {
					g.varStructTypes[v.Name] = ident.Name
				} else if g.currentClass != "" {
					// Bare call inside a class method: may be a self-method
					// call like `var g = snapshotGraph()`. If the current
					// class declares `snapshotGraph` with a class/generic
					// return type, propagate it.
					if cls, ok := g.structs[g.currentClass]; ok {
						for _, m := range cls.Methods {
							if m.Name != ident.Name || m.ReturnType == nil {
								continue
							}
							if st, ok := m.ReturnType.(*parser.SimpleType); ok && g.isClassType(st.Name) {
								g.varStructTypes[v.Name] = st.Name
							}
							if gt, ok := m.ReturnType.(*parser.GenericType); ok {
								g.varTypeExprs[v.Name] = gt
							}
						}
					}
				}
			}
			// Qualified constructor: core.MemoryContentStore() → SelectorExpr
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				if pkg, ok := sel.Object.(*parser.Ident); ok {
					if exports, ok := g.subpkgExports[pkg.Name]; ok {
						kind := exports[sel.Field]
						if kind == "class" || kind == "data" {
							g.varStructTypes[v.Name] = sel.Field
						}
					}
				}
			}
		}

		// Track struct type from method call return on known struct instances
		// e.g., var dlq = fab.getDLQ() where fab:Fabric and getDLQ returns DLQ
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				retType := g.resolveMethodReturnType(sel)
				if retType != "" && g.isClassType(retType) {
					g.varStructTypes[v.Name] = retType
				}
			}
		}

		// Track generic type from method-call return:
		// `var conns = fab.getConnections()` where getConnections is declared
		// to return `Map<String, Map<String, List<String>>>`. Without this,
		// subsequent `conns.keys()` / `conns[k]` lose their type and fall
		// back to []interface{} (ZCA-11c — sibling of the receiver-shape fix,
		// for the store-then-use shape).
		if call, ok := v.Value.(*parser.CallExpr); ok && v.Type == nil {
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				if outerClass := g.resolveReceiverClassName(sel.Object); outerClass != "" {
					if cls := g.lookupClassDecl(outerClass); cls != nil {
						for _, m := range cls.Methods {
							if m.Name == sel.Field && m.ReturnType != nil {
								if gt, ok := m.ReturnType.(*parser.GenericType); ok {
									g.varTypeExprs[v.Name] = gt
								}
							}
						}
					}
				}
			}
		}

		// Infer type from list literal
		if listLit, ok := v.Value.(*parser.ListLit); ok && v.Type == nil {
			elemType := inferListLitElemType(listLit.Elements)
			if elemType != "interface{}" {
				g.varTypes[v.Name] = elemType
			}
			// Track explicit generic type from typed literal: var x = List<int>[]
			if listLit.ExplicitType != nil {
				g.varTypeExprs[v.Name] = listLit.ExplicitType
			}
		}

		// Track explicit generic type from typed map literal: var x = Map<String, String>{}
		if mapLit, ok := v.Value.(*parser.MapLit); ok && v.Type == nil {
			if mapLit.ExplicitType != nil {
				g.varTypeExprs[v.Name] = mapLit.ExplicitType
			}
		}

		// Track type from a map-index expression: `var x = m[k]` where m is
		// Map<K,V> → x has type V. Without this propagation, `x.keys()` on a
		// nested-map value (where x itself is a Map<K2,V2>) falls back to
		// []interface{} (ZCA-11b). Covers both local-var and class-field m.
		if idx, ok := v.Value.(*parser.IndexExpr); ok && v.Type == nil {
			if gt := g.resolveReceiverGenericType(idx.Object); gt != nil && gt.Name == "Map" && len(gt.TypeArgs) >= 2 {
				if gt2, ok := gt.TypeArgs[1].(*parser.GenericType); ok {
					g.varTypeExprs[v.Name] = gt2
				}
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

		// Track Channel-typed locals from `var ch = Channel<T>(N)` so that
		// downstream codegen (for-range, isChannelVar, isCollectionVar) can
		// identify them. Without this, only explicitly-typed declarations
		// (`Channel<T> ch = …`) were recognized; the bare-init form is the
		// idiomatic one used throughout zinc-flow-go.
		// (Field-level synthesis already exists in fieldGenericType.)
		if call, ok := v.Value.(*parser.CallExpr); ok && v.Type == nil {
			if ident, ok := call.Callee.(*parser.Ident); ok &&
				(ident.Name == "Channel" || ident.Name == "Chan") &&
				len(call.TypeArgs) >= 1 {
				elemType := &parser.SimpleType{Name: call.TypeArgs[0]}
				g.varTypeExprs[v.Name] = &parser.GenericType{
					Name:     "Channel",
					TypeArgs: []parser.TypeExpr{elemType},
				}
				g.varTypes[v.Name] = "chan " + g.formatType(elemType)
			}
		}
		if _, ok := v.Value.(*parser.SafeNavExpr); ok {
			g.ptrVars[v.Name] = true
		}

		// Track Go types from stdlib function calls (e.g. exec.Command → *exec.Cmd).
		// Guard against user-scope shadow: a field/param/local named the same
		// as an import must not be treated as a package here (ZCA-10).
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				if ident, ok := sel.Object.(*parser.Ident); ok && !g.isUserScopeShadow(ident.Name) {
					if pkgPath, ok := g.importMap[ident.Name]; ok {
						if retType := g.goResolver.FuncReturnType(pkgPath, sel.Field); retType != nil {
							g.varGoTypes[v.Name] = retType
						}
					}
				}
			}
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
		// Use explicit type only for interfaces/sealed types where Go
		// can't infer the interface from a concrete constructor.
		// For regular classes, let Go infer (avoids *Child vs *Parent issues).
		useExplicitType := false
		if v.Type != nil {
			if st, ok := v.Type.(*parser.SimpleType); ok {
				if g.interfaces[st.Name] {
					useExplicitType = true
				}
				// Check unqualified names for cross-package interfaces
				if entry, ok := g.unqualifiedNames[st.Name]; ok {
					if entry.kind == "interface" {
						useExplicitType = true
					}
				}
			}
		}
		if useExplicitType {
			typeName := g.formatType(v.Type)
			g.writeln("var %s %s = %s", varName, typeName, g.formatExpr(v.Value))
		} else {
			g.writeln("%s := %s", varName, g.formatExpr(v.Value))
		}
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
		// Track ptr-typed locals so subsequent assignments and reads
		// can apply auto-address-take / auto-deref. Only for shapes that
		// actually lower to *T — collection nullables (List<T>?, Map?,
		// Channel?) drop the pointer under Strategy B and don't need
		// this.
		if isPointerOptional(v.Type) {
			g.ptrVars[v.Name] = true
		}
	}
}

// isPointerOptional reports whether the given TypeExpr lowers to a Go
// pointer type. Only `T?` where T is a value type or class — collection
// optionals (`List<T>?`, `Map<K,V>?`, `Channel<T>?`, `T[]?`) lower to
// the bare collection (Strategy B) and use Go's nil zero-value as the
// "absent" sentinel.
func isPointerOptional(t parser.TypeExpr) bool {
	opt, ok := t.(*parser.OptionalType)
	if !ok {
		return false
	}
	if gt, ok := opt.Inner.(*parser.GenericType); ok {
		switch gt.Name {
		case "List", "Map", "Channel", "Set":
			return false
		}
	}
	if _, ok := opt.Inner.(*parser.ArrayType); ok {
		return false
	}
	return true
}

// --- Assignment, return, control flow ----------------------------------------

func (g *Generator) emitAssignStmt(a *parser.AssignStmt) {
	if a.OrHandler != nil {
		targetStr := g.formatExpr(a.Target)
		g.emitOrAssignment(targetStr, a.Value, a.OrHandler)
		return
	}
	// Explicit error propagation: `x = call()?`.
	// Strip the wrapper and fall through to the existing propagation path.
	if prop, ok := a.Value.(*parser.PropagateExpr); ok && a.Op == "=" {
		stripped := &parser.AssignStmt{Line: a.Line, Target: a.Target, Op: a.Op, Value: prop.Inner}
		g.emitAssignStmt(stripped)
		return
	}
	// Nested propagation on RHS: `x = foo(bar()?) + 1`. Hoist any `?`
	// or `as` to temps, then emit the rewritten assign.
	if a.Value != nil && (exprContainsPropagate(a.Value) || exprContainsAsCast(a.Value)) {
		rewritten := &parser.AssignStmt{Line: a.Line, Target: a.Target, Op: a.Op, Value: g.hoistPropagates(a.Value)}
		g.emitAssignStmt(rewritten)
		return
	}
	// Auto-propagate error from `x = call()` where call() throws. Only
	// simple `=` assignment with a (T, error)-returning call; compound
	// ops (`+=` etc.) don't apply to thrower calls in practice. Use a
	// temp so the LHS can be an index/selector (Go's `:=` requires a
	// bare name).
	if a.Op == "=" && g.callReturnsError(a.Value) && !g.callIsVoidThrower(a.Value) {
		errName := g.nextErrName()
		tmpName := fmt.Sprintf("_tmp%d", g.errVarCount)
		g.errVarCount++
		g.writeln("%s, %s := %s", tmpName, errName, g.formatExpr(a.Value))
		g.writeln("if %s != nil {", errName)
		g.indent++
		g.emitErrReturn(errName)
		g.indent--
		g.writeln("}")
		g.writeln("%s = %s", g.formatExpr(a.Target), tmpName)
		return
	}
	// Auto-address-take: if the LHS is `*T` (a nullable value/class) and
	// the RHS is a plain `T`, wrap with _zincPtr() so the assignment
	// type-checks. Without this, `box.name = "alice"` (where name is
	// `String?`) fails with "cannot use string as *string". Skip for
	// `null` (nil is the natural absent value) and for already-pointer
	// RHS values.
	if a.Op == "=" && g.targetIsPointerOptional(a.Target) && !g.valueIsAlreadyPointer(a.Value) {
		if _, isNull := a.Value.(*parser.NullLit); !isNull {
			g.writeln("%s = %s", g.formatExpr(a.Target), g.wrapAsPointer(g.formatExpr(a.Value)))
			return
		}
	}
	g.writeln("%s %s %s", g.formatExpr(a.Target), a.Op, g.formatExpr(a.Value))
}

// targetIsPointerOptional reports whether an assignment LHS expression
// resolves to a `*T` type — a nullable value/class field or local.
// Used to drive auto-address-take on the RHS.
func (g *Generator) targetIsPointerOptional(target parser.Expr) bool {
	switch t := target.(type) {
	case *parser.Ident:
		return g.ptrVars[t.Name]
	case *parser.SelectorExpr:
		// `box.name` — look up the field's type on the receiver class.
		if recv := g.resolveReceiverClassName(t.Object); recv != "" {
			if cls, ok := g.structs[recv]; ok {
				for _, f := range cls.Fields {
					if f.Name == t.Field {
						return isPointerOptional(f.Type)
					}
				}
			}
		}
	}
	return false
}

// valueIsAlreadyPointer reports whether the RHS expression already
// produces a `*T` value — in which case auto-address-take would
// double-wrap. Conservative: false-negatives just skip the rewrite
// (and the user gets a Go type error pointing at the actual issue).
func (g *Generator) valueIsAlreadyPointer(value parser.Expr) bool {
	if value == nil {
		return false
	}
	switch v := value.(type) {
	case *parser.Ident:
		if g.ptrVars[v.Name] {
			return true
		}
	case *parser.CallExpr:
		// Constructor call: NewFoo() returns *Foo.
		if ident, ok := v.Callee.(*parser.Ident); ok {
			if g.isClassType(ident.Name) {
				return true
			}
			if g.funcReturnsOptional[ident.Name] {
				return true
			}
		}
		// `new T` and `new T()` lower to &T{} — pointer.
		if v.IsNew {
			return true
		}
	case *parser.UnaryExpr:
		if v.Op == "&" {
			return true
		}
	case *parser.SelectorExpr:
		// Reading `box.name` where name is *string.
		if recv := g.resolveReceiverClassName(v.Object); recv != "" {
			if cls, ok := g.structs[recv]; ok {
				for _, f := range cls.Fields {
					if f.Name == v.Field {
						return isPointerOptional(f.Type)
					}
				}
			}
		}
	}
	return false
}

func (g *Generator) emitReturnStmt(r *parser.ReturnStmt) {
	// Explicit error propagation: any `?` in the returned expression.
	// hoistPropagates emits `_pN, errM := inner; if errM != nil { return zero, errM }`
	// for each `?`, then returns a rewritten value expression that
	// references the temps. Top-level `return call()?` and nested
	// `return call()? * 3` (or any `as` cast inside the return value)
	// both go through this path.
	if r.Value != nil && (exprContainsPropagate(r.Value) || exprContainsAsCast(r.Value)) {
		rewritten := &parser.ReturnStmt{Line: r.Line, Value: g.hoistPropagates(r.Value)}
		g.emitReturnStmt(rewritten)
		return
	}

	if r.Value == nil {
		if g.currentReturnType != "" {
			zv := g.zeroValueFor(g.currentReturnType)
			g.writeln("return %s, nil", zv)
		} else if g.currentReturnType == "" && g.errorFuncs != nil {
			g.writeln("return")
		} else {
			g.writeln("return")
		}
		return
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
		// Returning an error-class value: emit the error slot, not
		// the value slot. `return zeroVal, errVal`.
		if g.currentFuncIsThrower && g.exprIsErrorCtor(r.Value) {
			zv := g.zeroValueFor(g.currentReturnType)
			g.writeln("return %s, %s", zv, g.formatExpr(r.Value))
			return
		}
		// `return thrower()` — the call already returns (T, error)
		// matching this function's widened signature. Pass through
		// the tuple directly instead of appending `, nil` (which
		// would be a Go type error).
		if g.currentFuncIsThrower && g.callReturnsError(r.Value) {
			g.writeln("return %s", g.formatExpr(r.Value))
			return
		}
		g.writeln("return %s, nil", g.formatExpr(r.Value))
		return
	}

	// Void widened function returning an error-class value.
	if g.currentFuncIsThrower && g.exprIsErrorCtor(r.Value) {
		g.writeln("return %s", g.formatExpr(r.Value))
		return
	}

	g.writeln("return %s", g.formatExpr(r.Value))
}

// tryContainsKeyIfHeader recognizes `m.containsKey(k)` or `!m.containsKey(k)`
// in if-cond position and returns the (init, cond) parts of an `if init; cond {`
// header. Returns ok=false for any other shape.
func (g *Generator) tryContainsKeyIfHeader(e parser.Expr) (string, string, bool) {
	negated := false
	if u, ok := e.(*parser.UnaryExpr); ok && u.Op == "!" {
		negated = true
		e = u.Operand
	}
	call, ok := e.(*parser.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", "", false
	}
	sel, ok := call.Callee.(*parser.SelectorExpr)
	if !ok || sel.Field != "containsKey" {
		return "", "", false
	}
	if g.isStructVar(sel.Object) {
		return "", "", false
	}
	obj := g.formatExpr(sel.Object)
	key := g.formatExpr(call.Args[0])
	cond := "_ok"
	if negated {
		cond = "!_ok"
	}
	return fmt.Sprintf("_, _ok := %s[%s]", obj, key), cond, true
}

func (g *Generator) emitIfStmt(s *parser.IfStmt) {
	// Map containsKey check in cond position emits the natural Go form
	// `if _, _ok := m[k]; _ok { ... }` instead of an IIFE wrapper. Pure
	// readability win in the generated Go (Zinc's value-prop is that
	// the output is editable). Negation `!m.containsKey(k)` lowers to
	// `; !_ok`. Only fires when the call is in plain or single-negated
	// position; compound boolean exprs fall back to the IIFE form.
	if init, cond, ok := g.tryContainsKeyIfHeader(s.Cond); ok {
		g.writeln("if %s; %s {", init, cond)
	} else {
		g.writeln("if %s {", g.formatExpr(s.Cond))
	}
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
			} else if g.isChannelVar(f.Range) {
				// Go channel range yields one value per iteration, not (i, v).
				// Loop terminates when the channel is closed.
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
	g.withLocalScope(func() {
		// Loop-bound names shadow fields inside the body.
		if f.IsRange {
			g.declareLocal(f.Item)
			g.declareLocal(f.IndexVar)
		} else if init, ok := f.Init.(*parser.VarStmt); ok {
			g.declareLocal(init.Name)
		}
		g.emitBlock(f.Body)
	})
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

// isMapVar checks if the expression is a variable declared as a Map type.
func (g *Generator) isMapVar(e parser.Expr) bool {
	if ident, ok := e.(*parser.Ident); ok {
		if te, ok := g.varTypeExprs[ident.Name]; ok {
			if gt, ok := te.(*parser.GenericType); ok && gt.Name == "Map" {
				return true
			}
		}
		if t, ok := g.varTypes[ident.Name]; ok && strings.HasPrefix(t, "map[") {
			return true
		}
		if t, ok := g.varTypes[ident.Name]; ok && t == "map" {
			return true
		}
	}
	return false
}

// isListVar checks if the expression is a variable declared as a List/slice type.
func (g *Generator) isListVar(e parser.Expr) bool {
	if ident, ok := e.(*parser.Ident); ok {
		if te, ok := g.varTypeExprs[ident.Name]; ok {
			if gt, ok := te.(*parser.GenericType); ok && gt.Name == "List" {
				return true
			}
		}
		if t, ok := g.varTypes[ident.Name]; ok && strings.HasPrefix(t, "[]") {
			return true
		}
	}
	return false
}

// isChannelVar checks if the expression is a variable declared as a Channel type.
func (g *Generator) isChannelVar(e parser.Expr) bool {
	if ident, ok := e.(*parser.Ident); ok {
		if te, ok := g.varTypeExprs[ident.Name]; ok {
			if gt, ok := te.(*parser.GenericType); ok && gt.Name == "Channel" {
				return true
			}
		}
		if t, ok := g.varTypes[ident.Name]; ok && strings.HasPrefix(t, "chan ") {
			return true
		}
	}
	return false
}

// isCollectionVar checks if the expression is a built-in Go collection type (map, slice, or channel).
// Also checks class fields for collection types.
func (g *Generator) isCollectionVar(e parser.Expr) bool {
	if g.isMapVar(e) || g.isListVar(e) || g.isChannelVar(e) {
		return true
	}
	// Check class fields for collection types
	if ident, ok := e.(*parser.Ident); ok {
		if g.currentClass != "" && g.currentFields[ident.Name] {
			if cls, ok := g.structs[g.currentClass]; ok {
				for _, f := range cls.Fields {
					if f.Name == ident.Name {
						// Check explicit type annotation
						if f.Type != nil {
							if gt, ok := f.Type.(*parser.GenericType); ok {
								switch gt.Name {
								case "List", "Map", "Channel", "Set":
									return true
								}
							}
							if _, ok := f.Type.(*parser.ArrayType); ok {
								return true
							}
						}
						// Check inferred type from default value (var x = Map<K,V>{} or List<T>[])
						if f.Default != nil {
							if ml, ok := f.Default.(*parser.MapLit); ok && ml.ExplicitType != nil {
								if gt, ok := ml.ExplicitType.(*parser.GenericType); ok && gt.Name == "Map" {
									return true
								}
							}
							if ll, ok := f.Default.(*parser.ListLit); ok && ll.ExplicitType != nil {
								if gt, ok := ll.ExplicitType.(*parser.GenericType); ok && gt.Name == "List" {
									return true
								}
							}
						}
					}
				}
			}
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
// Returns true if any non-wildcard case pattern is a type name (data class,
// class, interface, or primitive type) with a binding argument.
func (g *Generator) isTypeSwitchMatch(m *parser.MatchStmt) bool {
	for _, c := range m.Cases {
		if c.Pattern == nil {
			continue
		}
		if call, ok := c.Pattern.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				// Data classes (sealed variants)
				if g.dataClasses[ident.Name] {
					return true
				}
				// Cross-package data classes
				if entry, ok := g.unqualifiedNames[ident.Name]; ok && entry.kind == "data" {
					return true
				}
				// Any known type: class, interface, primitive
				if _, ok := g.structs[ident.Name]; ok {
					return true
				}
				if entry, ok := g.unqualifiedNames[ident.Name]; ok && (entry.kind == "class" || entry.kind == "interface") {
					return true
				}
				if _, ok := zincToGoType[ident.Name]; ok {
					return true
				}
			}
			// Qualified: core.Single(ff) or core.MyClass(obj)
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				if pkg, ok := sel.Object.(*parser.Ident); ok {
					if exports, ok := g.subpkgExports[pkg.Name]; ok {
						kind := exports[sel.Field]
						if kind == "data" || kind == "class" || kind == "interface" {
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
	g.checkMatchExhaustiveness(m)
	g.writeln("switch _v := %s.(type) {", g.formatExpr(m.Subject))
	for _, c := range m.Cases {
		if c.Pattern == nil {
			g.writeln("default:")
			g.indent++
			g.writeln("_ = _v")
			g.emitBlock(c.Body)
			g.indent--
			continue
		}

		call, ok := c.Pattern.(*parser.CallExpr)
		if !ok {
			// Plain identifier pattern (e.g. case Drop without args)
			g.writeln("case %s:", g.formatExpr(c.Pattern))
			g.indent++
			g.writeln("_ = _v")
			g.emitBlock(c.Body)
			g.indent--
			continue
		}

		// Get the type name (local or qualified)
		typeName := ""
		isDataClass := false
		var dataDecl *parser.DataClassDecl
		if ident, ok := call.Callee.(*parser.Ident); ok {
			typeName = ident.Name
			// Primitive types: map Zinc name → Go name
			if goType, ok := zincToGoType[ident.Name]; ok {
				typeName = goType
			} else if entry, ok := g.unqualifiedNames[ident.Name]; ok {
				// Cross-package: resolve to Go-qualified form
				typeName = entry.pkg + "." + exportName(ident.Name)
				if entry.kind == "class" || entry.kind == "interface" {
					typeName = "*" + typeName
				}
				isDataClass = entry.kind == "data"
			} else if _, ok := g.structs[ident.Name]; ok {
				// Local class
				typeName = "*" + exportName(ident.Name)
			}
			if g.dataClasses[ident.Name] {
				isDataClass = true
				typeName = exportName(ident.Name)
			}
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
				if exports, ok := g.subpkgExports[pkg.Name]; ok {
					kind := exports[sel.Field]
					if kind == "class" || kind == "interface" {
						typeName = "*" + typeName
					}
					isDataClass = kind == "data"
				}
			}
		}

		g.writeln("case %s:", typeName)
		g.indent++

		// Track whether this arm actually binds _v so we know whether to
		// emit `_ = _v` afterwards. Without this, Go rejects the file with
		// "_v declared and not used" when every case discards (`case _`,
		// `case Foo(_)`, `case BareType`).
		bound := false
		if isDataClass {
			// Data class: destructure fields — case Single(ff) → ff := _v.Ff
			var fieldParams []*parser.FieldDecl
			if dataDecl != nil {
				fieldParams = dataDecl.Params
			} else if callerIdent, ok := call.Callee.(*parser.Ident); ok {
				if entry, ok := g.unqualifiedNames[callerIdent.Name]; ok {
					if pkgFields, ok := g.subpkgDataFields[entry.pkg]; ok {
						fieldParams = pkgFields[callerIdent.Name]
					}
				}
			}
			if fieldParams != nil && len(call.Args) > 0 {
				for i, arg := range call.Args {
					if ident, ok := arg.(*parser.Ident); ok && ident.Name != "_" {
						if i < len(fieldParams) {
							fieldName := exportName(fieldParams[i].Name)
							g.writeln("%s := _v.%s", ident.Name, fieldName)
							bound = true
						}
					}
				}
			} else if len(call.Args) > 0 {
				for _, arg := range call.Args {
					if ident, ok := arg.(*parser.Ident); ok && ident.Name != "_" {
						fieldName := exportName(ident.Name)
						g.writeln("%s := _v.%s", ident.Name, fieldName)
						bound = true
					}
				}
			}
		} else if len(call.Args) > 0 {
			// Non-data type: bind the whole typed value — case String(s) → s := _v
			if len(call.Args) == 1 {
				if ident, ok := call.Args[0].(*parser.Ident); ok && ident.Name != "_" {
					g.writeln("%s := _v", ident.Name)
					bound = true
				}
			}
		}
		if !bound {
			g.writeln("_ = _v")
		}

		g.emitBlock(c.Body)
		g.indent--
	}

	// If no explicit default arm was written, add one with panic("unreachable")
	// to satisfy Go's exhaustiveness requirement on type switches.
	hasDefault := false
	for _, c := range m.Cases {
		if c.Pattern == nil {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		g.writeln("default:")
		g.indent++
		g.writeln("_ = _v")
		g.writeln("panic(\"unreachable\")")
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

// sealedClassOfVariant returns the sealed class whose Variants contain the
// given data-class name, or nil if the name isn't a sealed variant in the
// current package or any imported subpackage.
func (g *Generator) sealedClassOfVariant(variantName string) *parser.ClassDecl {
	for _, cls := range g.structs {
		if cls.IsSealed {
			for _, v := range cls.Variants {
				if v.Name == variantName {
					return cls
				}
			}
		}
	}
	for _, pkgStructs := range g.subpkgStructs {
		for _, cls := range pkgStructs {
			if cls.IsSealed {
				for _, v := range cls.Variants {
					if v.Name == variantName {
						return cls
					}
				}
			}
		}
	}
	return nil
}

// checkMatchExhaustiveness records a compile error when a match on a sealed
// class omits variants without providing an explicit wildcard/else case.
// Non-sealed matches (type-switching on Object, enum matches, etc.) are
// skipped. Errors accumulate on the Generator; compileMultiFile fails the
// build when any are present.
func (g *Generator) checkMatchExhaustiveness(m *parser.MatchStmt) {
	// Identify the sealed class from any CallExpr pattern. Match statements
	// in Zinc mix case types (e.g. type_match.zn matches String/int/Animal
	// against Object) — those aren't sealed, so skip when the first variant
	// lookup fails to find a sealed owner.
	var sealed *parser.ClassDecl
	for _, c := range m.Cases {
		if c.Pattern == nil {
			continue
		}
		if call, ok := c.Pattern.(*parser.CallExpr); ok {
			if id, ok := call.Callee.(*parser.Ident); ok {
				if s := g.sealedClassOfVariant(id.Name); s != nil {
					sealed = s
					break
				}
			}
		}
	}
	if sealed == nil {
		return
	}

	covered := make(map[string]bool)
	for _, c := range m.Cases {
		if c.Pattern == nil {
			return // wildcard covers everything remaining
		}
		if call, ok := c.Pattern.(*parser.CallExpr); ok {
			if id, ok := call.Callee.(*parser.Ident); ok {
				covered[id.Name] = true
				continue
			}
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				covered[sel.Field] = true
				continue
			}
		}
		if id, ok := c.Pattern.(*parser.Ident); ok {
			covered[id.Name] = true
		}
	}

	var missing []string
	for _, v := range sealed.Variants {
		if !covered[v.Name] {
			missing = append(missing, v.Name)
		}
	}
	if len(missing) > 0 {
		g.compileError(m.Line,
			"non-exhaustive match on sealed type %q: missing variant(s) %s (add case(s) or an else branch)",
			sealed.Name, strings.Join(missing, ", "))
	}
}

// --- Expression statements (spawn, print, collection methods, forEach) -------

func (g *Generator) emitExprStmt(es *parser.ExprStmt) {
	if es.OrHandler != nil {
		g.emitOrAssignment("_", es.Expr, es.OrHandler)
		return
	}
	// Explicit error propagation: `call()?` as a bare statement.
	// Strip the wrapper and reuse the existing propagation path.
	if prop, ok := es.Expr.(*parser.PropagateExpr); ok {
		stripped := &parser.ExprStmt{Line: es.Line, Expr: prop.Inner}
		g.emitExprStmt(stripped)
		return
	}
	// Nested `?` (or `as`) inside an expression statement: e.g.
	// `foo(bar()?)` as a bare statement. Hoist, then emit the rewritten
	// statement.
	if exprContainsPropagate(es.Expr) || exprContainsAsCast(es.Expr) {
		rewritten := &parser.ExprStmt{Line: es.Line, Expr: g.hoistPropagates(es.Expr)}
		g.emitExprStmt(rewritten)
		return
	}
	// Auto-propagate error from a bare `call()` statement where call()
	// throws. Shape depends on return arity: void thrower → `err := call()`,
	// (T, error) thrower → `_, err := call()`.
	if g.callReturnsError(es.Expr) {
		errName := g.nextErrName()
		callExpr := g.formatExpr(es.Expr)
		if g.callIsVoidThrower(es.Expr) {
			g.writeln("%s := %s", errName, callExpr)
		} else {
			g.writeln("_, %s := %s", errName, callExpr)
		}
		g.writeln("if %s != nil {", errName)
		g.indent++
		g.emitErrReturn(errName)
		g.indent--
		g.writeln("}")
		return
	}
	// spawn { body } → go func() { body }()
	if spawn, ok := es.Expr.(*parser.SpawnExpr); ok {
		g.writeln("go func() {")
		g.indent++
		g.emitGoroutineBody(spawn.Body)
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
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "put" && len(call.Args) == 2 && !g.isStructVar(sel.Object) {
			obj := g.formatExpr(sel.Object)
			g.writeln("%s[%s] = %s", obj, g.formatExpr(call.Args[0]), g.formatExpr(call.Args[1]))
			return
		}
	}
	g.writeln("%s", g.formatExpr(es.Expr))
}

// isClassType returns true if the given name is a known class type,
// checking both local structs and cross-package unqualified names.
func (g *Generator) isClassType(name string) bool {
	if _, exists := g.structs[name]; exists {
		return true
	}
	// Local data classes — `data FlowLike(...)` in the same file.
	if g.dataClasses[name] {
		return true
	}
	if entry, ok := g.unqualifiedNames[name]; ok {
		return entry.kind == "class" || entry.kind == "data"
	}
	// Fallback — a `data FlowFile` imported from another package that
	// wasn't re-exported via unqualifiedNames still needs to be recognized
	// as a class type (see ZCA-11d: param-type tracking for cross-package
	// data classes).
	for _, pkgClasses := range g.subpkgStructs {
		if _, ok := pkgClasses[name]; ok {
			return true
		}
	}
	return false
}

// resolveMethodReturnType determines the return type of a method call on a known struct.
// For example, fab.getDLQ() where fab is Fabric and getDLQ() returns DLQ → returns "DLQ".
func (g *Generator) resolveMethodReturnType(sel *parser.SelectorExpr) string {
	receiverType := ""
	if ident, ok := sel.Object.(*parser.Ident); ok {
		// Check local variables
		if st, ok := g.varStructTypes[ident.Name]; ok {
			receiverType = st
		}
		// Check class fields
		if receiverType == "" && g.currentClass != "" && g.currentFields[ident.Name] {
			if cls, ok := g.structs[g.currentClass]; ok {
				for _, f := range cls.Fields {
					if f.Name == ident.Name {
						if st, ok := f.Type.(*parser.SimpleType); ok {
							receiverType = st.Name
						}
					}
				}
			}
		}
	}
	if receiverType == "" {
		return ""
	}
	// Look up the method's return type — check local structs first, then imported
	if cls, ok := g.structs[receiverType]; ok {
		for _, m := range cls.Methods {
			if m.Name == sel.Field && m.ReturnType != nil {
				if st, ok := m.ReturnType.(*parser.SimpleType); ok {
					return st.Name
				}
			}
		}
	}
	// Check imported class declarations from all subpackages
	for _, pkgClasses := range g.subpkgStructs {
		if cls, ok := pkgClasses[receiverType]; ok {
			for _, m := range cls.Methods {
				if m.Name == sel.Field && m.ReturnType != nil {
					if st, ok := m.ReturnType.(*parser.SimpleType); ok {
						return st.Name
					}
				}
			}
		}
	}
	return ""
}

// isStructVar checks if an expression refers to a known class instance variable.
// Used to distinguish collection builtins from method calls on class instances.
// Checks local variables, class fields, and constructor parameters.
func (g *Generator) isStructVar(e parser.Expr) bool {
	if ident, ok := e.(*parser.Ident); ok {
		name := ident.Name
		// Check local variables explicitly tracked as struct types
		if _, ok := g.varStructTypes[name]; ok {
			return true
		}
		// Check if it's a field of the current class with a class/struct type
		if g.currentClass != "" && g.currentFields[name] {
			if cls, ok := g.structs[g.currentClass]; ok {
				for _, f := range cls.Fields {
					if f.Name == name {
						if st, ok := f.Type.(*parser.SimpleType); ok {
							return g.isClassType(st.Name)
						}
					}
				}
			}
		}
	}
	// Nested selector / method-call receivers: `o.inner`, `this.outer.inner`,
	// `obj.getInner()` — the resolver walks class declarations looking for a
	// concrete class name. Without this path, a method call on a nested
	// class field was misread by rewrites like `.put()` as a map-set.
	if _, ok := e.(*parser.SelectorExpr); ok {
		if clsName := g.resolveReceiverClassName(e); clsName != "" && g.isClassType(clsName) {
			return true
		}
	}
	if _, ok := e.(*parser.CallExpr); ok {
		if clsName := g.resolveReceiverClassName(e); clsName != "" && g.isClassType(clsName) {
			return true
		}
	}
	return false
}

// --- Error handling (exception pivot) ----------------------------------------

// callReturnsError reports whether an expression is a call to a function
// whose Go signature has a trailing `error` result. Used to inject
// `if err != nil { return err }` at call sites inside try bodies or
// thrower functions, replacing the old explicit `or { }` handler.
//
// Today this covers Zinc functions tracked in errorFuncs. Method calls
// on classes with a throwing method are detected via the class's method
// list. Go stdlib calls fall back to goResolver.ReturnsError.
func (g *Generator) callReturnsError(expr parser.Expr) bool {
	call, ok := expr.(*parser.CallExpr)
	if !ok {
		return false
	}
	switch callee := call.Callee.(type) {
	case *parser.Ident:
		if g.errorFuncs[callee.Name] {
			return true
		}
		// Constructor call shape: `Port(8080)` where Port is a class
		// with a throwing ctor. The actual Go call will be NewPort(...),
		// so look up the widened key.
		if _, isClass := g.structs[callee.Name]; isClass {
			if g.errorFuncs["New"+callee.Name] {
				return true
			}
		}
		// Unqualified Go stdlib call: `ReadFile(path)` with `import os`
		// resolves to `os.ReadFile`. Check the underlying Go signature
		// for a trailing error result.
		if entry, ok := g.unqualifiedNames[callee.Name]; ok && entry.kind == "func" {
			if pkgPath, ok := g.importMap[entry.pkg]; ok {
				if g.goResolver.ReturnsError(pkgPath, entry.name) {
					return true
				}
			}
		}
	case *parser.SelectorExpr:
		// Method on a class: Class.method key in errorFuncs, or the
		// method declaration in a sibling/subpackage class.
		if recv := g.resolveReceiverClassName(callee.Object); recv != "" {
			if g.errorFuncs[recv+"."+callee.Field] {
				return true
			}
			if g.methodBodyThrows(recv, callee.Field) {
				return true
			}
		}
		// Go stdlib package call.
		if ident, ok := callee.Object.(*parser.Ident); ok && !g.isUserScopeShadow(ident.Name) {
			if pkgPath, ok := g.importMap[ident.Name]; ok {
				if g.goResolver.ReturnsError(pkgPath, callee.Field) {
					return true
				}
			}
		}
	}
	return false
}

// methodBodyThrows walks the method declaration (from either local
// structs or subpackage-declared structs) to see whether it can throw
// or return an error. Cross-package thrower detection — errorFuncs
// only gets populated for the current generator's decls, so we need
// to consult the AST directly for sibling/subpackage classes. Walks
// the inheritance chain too so inherited throwers are detected.
func (g *Generator) methodBodyThrows(className, methodName string) bool {
	visited := map[string]bool{}
	return g.methodBodyThrowsRec(className, methodName, visited)
}

func (g *Generator) methodBodyThrowsRec(className, methodName string, visited map[string]bool) bool {
	if visited[className] {
		return false
	}
	visited[className] = true

	lookup := func(cls *parser.ClassDecl) bool {
		if cls == nil {
			return false
		}
		for _, m := range cls.Methods {
			if m.Name == methodName {
				return g.blockCanReturnError(m.Body)
			}
		}
		return false
	}

	var match *parser.ClassDecl
	if cls, ok := g.structs[className]; ok && cls != nil {
		if lookup(cls) {
			return true
		}
		match = cls
	}
	if match == nil {
		for _, classes := range g.subpkgStructs {
			if cls, ok := classes[className]; ok && cls != nil {
				if lookup(cls) {
					return true
				}
				match = cls
				break
			}
		}
	}
	if match == nil {
		return false
	}
	// Method not declared here — walk parents.
	for _, parent := range match.Parents {
		parentName := parent
		if idx := strings.LastIndex(parent, "."); idx >= 0 {
			parentName = parent[idx+1:]
		}
		if g.methodBodyThrowsRec(parentName, methodName, visited) {
			return true
		}
	}
	return false
}

// emitErrReturn emits the error-propagation tail for a captured error
// variable. Precedence:
//   - In a try body: `return err` (from the IIFE)
//   - In a thrower function: `return zero, err` (or `return err` for void)
//   - Otherwise: `panic(err)` — unchecked-exception semantics for a
//     top-level uncaught error (e.g. main() with a typed-only catch).
// The caller is responsible for guarding with `if err != nil { ... }`.
func (g *Generator) emitErrReturn(errVar string) {
	if !g.currentFuncIsThrower {
		g.writeln("panic(%s)", errVar)
		return
	}
	zv := g.zeroValueFor(g.currentReturnType)
	if zv != "" {
		g.writeln("return %s, %s", zv, errVar)
	} else {
		g.writeln("return %s", errVar)
	}
}

// callIsVoidThrower reports whether a call goes to a function whose
// Go signature returns a single `error` (no tuple). True for Zinc
// `void` throwers and Go stdlib error-only funcs (json.Unmarshal etc.).
// False when the call returns (T, error).
func (g *Generator) callIsVoidThrower(expr parser.Expr) bool {
	if g.isErrorOnlyCall(expr) {
		return true
	}
	call, ok := expr.(*parser.CallExpr)
	if !ok {
		return false
	}
	if ident, ok := call.Callee.(*parser.Ident); ok {
		if g.errorFuncs[ident.Name] {
			rt, exists := g.funcReturnTypes[ident.Name]
			return !exists || rt == "" || rt == "void"
		}
		// Unqualified Go stdlib — ask the resolver about return arity.
		if entry, ok := g.unqualifiedNames[ident.Name]; ok && entry.kind == "func" {
			if pkgPath, ok := g.importMap[entry.pkg]; ok {
				return g.goResolver.ReturnsErrorOnly(pkgPath, entry.name)
			}
		}
	}
	if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
		if recv := g.resolveReceiverClassName(sel.Object); recv != "" {
			if g.errorFuncs[recv+"."+sel.Field] {
				// Unknown method return arity — conservative false;
				// class methods declared void are rare in existing code.
				return false
			}
		}
	}
	return false
}

// nextErrName returns a unique local `err` variable name for the
// current function scope. First is `err`, then `err1`, `err2`, ...
func (g *Generator) nextErrName() string {
	name := "err"
	if g.errVarCount > 0 {
		name = fmt.Sprintf("err%d", g.errVarCount)
	}
	g.errVarCount++
	return name
}

// hoistPropagates walks an expression tree, emits the hoist for every
// PropagateExpr it contains (`_pN, errM := inner; if errM != nil { ... }`),
// and returns a rewritten expression with PropagateExprs replaced by
// Idents pointing at the temps. Used by statement emitters so that
// nested `?` usage (e.g. `foo(bar()?) + 1` or `return call()? * 3`)
// lowers to idiomatic Go. LambdaExpr is a separate scope and is not
// descended into — a `?` inside a lambda is the lambda's own concern.
func (g *Generator) hoistPropagates(e parser.Expr) parser.Expr {
	if e == nil {
		return nil
	}
	switch expr := e.(type) {
	case *parser.PropagateExpr:
		inner := g.hoistPropagates(expr.Inner)
		tmpName := fmt.Sprintf("_p%d", g.errVarCount)
		errName := g.nextErrName()
		g.writeln("%s, %s := %s", tmpName, errName, g.formatExpr(inner))
		g.writeln("if %s != nil {", errName)
		g.indent++
		g.emitErrReturn(errName)
		g.indent--
		g.writeln("}")
		return &parser.Ident{Name: tmpName}
	case *parser.BinaryExpr:
		return &parser.BinaryExpr{
			Op:    expr.Op,
			Left:  g.hoistPropagates(expr.Left),
			Right: g.hoistPropagates(expr.Right),
		}
	case *parser.UnaryExpr:
		return &parser.UnaryExpr{Op: expr.Op, Operand: g.hoistPropagates(expr.Operand)}
	case *parser.CallExpr:
		newCall := *expr
		newCall.Callee = g.hoistPropagates(expr.Callee)
		if len(expr.Args) > 0 {
			newArgs := make([]parser.Expr, len(expr.Args))
			for i, a := range expr.Args {
				newArgs[i] = g.hoistPropagates(a)
			}
			newCall.Args = newArgs
		}
		if len(expr.NamedArgs) > 0 {
			newNamed := make([]parser.NamedArg, len(expr.NamedArgs))
			for i, na := range expr.NamedArgs {
				newNamed[i] = parser.NamedArg{Name: na.Name, Value: g.hoistPropagates(na.Value)}
			}
			newCall.NamedArgs = newNamed
		}
		return &newCall
	case *parser.SelectorExpr:
		return &parser.SelectorExpr{Object: g.hoistPropagates(expr.Object), Field: expr.Field}
	case *parser.IndexExpr:
		return &parser.IndexExpr{Object: g.hoistPropagates(expr.Object), Index: g.hoistPropagates(expr.Index)}
	case *parser.SafeNavExpr:
		out := &parser.SafeNavExpr{Object: g.hoistPropagates(expr.Object), Field: expr.Field}
		if expr.Call != nil {
			call := *expr.Call
			if len(expr.Call.Args) > 0 {
				newArgs := make([]parser.Expr, len(expr.Call.Args))
				for i, a := range expr.Call.Args {
					newArgs[i] = g.hoistPropagates(a)
				}
				call.Args = newArgs
			}
			out.Call = &call
		}
		return out
	case *parser.TypeAssertExpr:
		// `is` predicate — pure expression, just descend.
		if expr.IsCheck {
			return &parser.TypeAssertExpr{Object: g.hoistPropagates(expr.Object), TypeName: expr.TypeName, IsCheck: true}
		}
		// `as` cast — failable. Emit comma-ok + error guard, replace with
		// an Ident pointing at the bound value. Mirrors the PropagateExpr
		// arm above; this is the lowering that makes `as` "return error
		// instead of panic" — the function widens to (T, error) via the
		// thrower fixed-point pass and the error propagates up.
		inner := g.hoistPropagates(expr.Object)
		count := g.errVarCount
		tmpName := fmt.Sprintf("_p%d", count)
		okName := fmt.Sprintf("_ok%d", count)
		errName := g.nextErrName() // bumps errVarCount
		goType := g.formatType(&parser.SimpleType{Name: expr.TypeName})
		g.needImport("fmt")
		g.writeln("%s, %s := %s.(%s)", tmpName, okName, g.formatExpr(inner), goType)
		g.writeln("if !%s {", okName)
		g.indent++
		g.writeln("%s := fmt.Errorf(%q)", errName, "type assertion failed: expected "+expr.TypeName)
		g.emitErrReturn(errName)
		g.indent--
		g.writeln("}")
		return &parser.Ident{Name: tmpName}
	case *parser.SpreadExpr:
		return &parser.SpreadExpr{Expr: g.hoistPropagates(expr.Expr)}
	case *parser.RangeExpr:
		return &parser.RangeExpr{Start: g.hoistPropagates(expr.Start), End: g.hoistPropagates(expr.End), Inclusive: expr.Inclusive}
	case *parser.LambdaExpr:
		// Separate scope — don't descend. Any `?` inside the lambda body
		// is the lambda's own codegen concern.
		return expr
	default:
		return e
	}
}


// emitErrorPropagatingVar emits `var x = call()` where call() throws:
//
//	x, err := call()
//	if err != nil { return err }  // or: return zero, err
func (g *Generator) emitErrorPropagatingVar(v *parser.VarStmt) {
	callExpr := g.formatExpr(v.Value)
	varName := v.Name
	if goBuiltins[varName] {
		safe := "_" + varName
		g.renamedVars[varName] = safe
		varName = safe
	}
	errName := g.nextErrName()
	g.writeln("%s, %s := %s", varName, errName, callExpr)
	g.writeln("if %s != nil {", errName)
	g.indent++
	g.emitErrReturn(errName)
	g.indent--
	g.writeln("}")

	// Track type information for subsequent references, mirroring what
	// the main emitVarStmt path does for constructor/call-returning
	// shapes. Conservative — only the forms the smoke tests need.
	if call, ok := v.Value.(*parser.CallExpr); ok {
		if ident, ok := call.Callee.(*parser.Ident); ok {
			if g.isClassType(ident.Name) {
				g.varStructTypes[v.Name] = ident.Name
			} else if strings.HasPrefix(ident.Name, "New") {
				structName := ident.Name[3:]
				if g.isClassType(structName) {
					g.varStructTypes[v.Name] = structName
				}
			}
		}
	}
}

// --- Error handling (or expressions) -----------------------------------------

// emitTypeAssertVar emits the direct form of `var x = expr as T` —
// optionally with an `or { }` handler — using Go's comma-ok type
// assertion. On mismatch, the handler runs (if present) or the error
// propagates via emitErrReturn. Never panics. The enclosing function
// auto-widens to (T, error) because exprContainsPropagate returns true
// for `as`, which the thrower fixed-point pass picks up.
func (g *Generator) emitTypeAssertVar(v *parser.VarStmt, ta *parser.TypeAssertExpr) {
	count := g.errVarCount
	okName := fmt.Sprintf("_ok%d", count)
	errName := g.nextErrName() // bumps errVarCount
	goType := g.formatType(&parser.SimpleType{Name: ta.TypeName})
	g.needImport("fmt")
	g.writeln("%s, %s := %s.(%s)", v.Name, okName, g.formatExpr(ta.Object), goType)
	g.writeln("if !%s {", okName)
	g.indent++
	if v.OrHandler != nil && v.OrHandler.Body != nil {
		// `or { handler }` — bind err for the handler body to reference.
		g.writeln("%s := fmt.Errorf(%q)", errName, "type assertion failed: expected "+ta.TypeName)
		g.writeln("_ = %s", errName)
		savedErrVar := g.currentErrVar
		g.currentErrVar = errName
		g.emitOrBlock(v.OrHandler.Body)
		g.currentErrVar = savedErrVar
	} else {
		// Auto-propagate.
		g.writeln("%s := fmt.Errorf(%q)", errName, "type assertion failed: expected "+ta.TypeName)
		g.emitErrReturn(errName)
	}
	g.indent--
	g.writeln("}")
}

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

	// Detect void-error functions (like json.Unmarshal) that return only error.
	// For these, generate single-value assignment: _err := call()
	// instead of two-value: target, _err := call()
	errorOnly := g.isErrorOnlyCall(value)

	if !errorOnly && handler.Body != nil && len(handler.Body.Stmts) == 1 {
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

	if errorOnly {
		g.writeln("%s := %s", errVar, callExpr)
	} else {
		g.writeln("%s, %s := %s", target, errVar, callExpr)
	}
	g.writeln("if %s != nil {", errVar)
	g.indent++
	if len(handler.MatchCases) > 0 {
		g.emitOrMatch(errVar, handler)
	} else if handler.Body != nil {
		g.emitOrBlock(handler.Body)
	}
	g.indent--
	g.writeln("}")
	g.currentErrVar = savedErrVar
}

// emitOrMatch lowers `or match err { case T -> body ... }` to a Go
// type switch. The zinc match variable (`handler.MatchVar`, default
// `err`) is bound to the typed error inside each case, matching how a
// Go dev would write `switch err := _err.(type) { case *T: ... }`.
// An explicit wildcard (`case _`) becomes `default:`. If no wildcard
// is present and no case re-throws, unmatched errors propagate via
// `return zero, _err`; this forces the enclosing function to be a
// thrower (handled by inference so the signature picks up (T, error)).
func (g *Generator) emitOrMatch(errVar string, handler *parser.OrHandler) {
	matchVar := handler.MatchVar
	if matchVar == "" {
		matchVar = "err"
	}
	savedErrVar := g.currentErrVar
	g.currentErrVar = matchVar

	hasWildcard := false
	for _, c := range handler.MatchCases {
		if c.Type == "" {
			hasWildcard = true
			break
		}
	}

	g.writeln("switch %s := %s.(type) {", matchVar, errVar)
	for _, c := range handler.MatchCases {
		if c.Type == "" {
			g.writeln("default:")
		} else {
			g.writeln("case *%s:", c.Type)
		}
		g.indent++
		g.writeln("_ = %s", matchVar)
		if c.Body != nil {
			for _, s := range c.Body.Stmts {
				g.emitStmt(s)
			}
		}
		g.indent--
	}
	if !hasWildcard {
		g.writeln("default:")
		g.indent++
		g.emitErrReturn(errVar)
		g.indent--
	}
	g.writeln("}")
	g.currentErrVar = savedErrVar
}

// isErrorOnlyCall checks if a call expression returns only error (no other values).
// Handles both package-level functions (json.Unmarshal) and method calls (proc.Start).
func (g *Generator) isErrorOnlyCall(expr parser.Expr) bool {
	call, ok := expr.(*parser.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Callee.(*parser.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.Object.(*parser.Ident)
	if !ok {
		return false
	}

	// Case 1: Package-level function — pkg.Func().
	// Guard against user-scope shadow so a field/param/local with the
	// same name as an imported package isn't treated as one (ZCA-10).
	if !g.isUserScopeShadow(ident.Name) {
		if pkgPath, ok := g.importMap[ident.Name]; ok {
			return g.goResolver.ReturnsErrorOnly(pkgPath, sel.Field)
		}
	}

	// Case 2: Method call on a variable — obj.Method()
	// Look up the variable's Go type and check the method signature
	if goType, ok := g.varGoTypes[ident.Name]; ok {
		return g.goResolver.MethodReturnsErrorOnly(goType, sel.Field)
	}

	return false
}

// emitOrBlock emits a block inside an or-handler, mapping `err` to the current error variable.
func (g *Generator) emitOrBlock(block *parser.BlockStmt) {
	for _, s := range block.Stmts {
		if ret, ok := s.(*parser.ReturnStmt); ok && ret.Value != nil {
			if call, ok := ret.Value.(*parser.CallExpr); ok {
				if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "Error" {
					if len(call.Args) == 1 {
						if argId, ok := call.Args[0].(*parser.Ident); ok && argId.Name == "err" {
							zv := g.zeroValueFor(g.currentReturnType)
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

// emitGoroutineBody emits the body of a goroutine (spawn, go, parallel
// for iteration, concurrent task). Throws and error-returning calls
// inside the body cannot propagate back to the launching thread — Go
// goroutines have no return channel. Per the exception-pivot design:
// UNCAUGHT errors inside a goroutine panic the process. No silent
// failure modes. Errors caught by a `try { } catch` inside the body
// are absorbed normally — the panic only fires when an error would
// otherwise escape to the launcher with nowhere to go.
//
// Shape when the body can propagate an error:
//
//	_gerr := func() error {
//	    body   // throws → return expr; errorFunc calls → if err != nil { return err }
//	    return nil
//	}()
//	if _gerr != nil { panic(_gerr) }
//
// When the body has no error activity, emit the block directly — no
// wrapper. Detection is conservative: treats any throw, Error-return,
// or call to an errorFunc as a propagation source.
func (g *Generator) emitGoroutineBody(body *parser.BlockStmt) {
	if !g.blockCanReturnError(body) {
		g.emitBlock(body)
		return
	}
	errVar := "_gerr"
	if g.errVarCount > 0 {
		errVar = fmt.Sprintf("_gerr%d", g.errVarCount)
	}
	g.errVarCount++
	g.writeln("%s := func() error {", errVar)
	g.indent++
	g.emitBlock(body)
	g.writeln("return nil")
	g.indent--
	g.writeln("}()")
	g.writeln("if %s != nil { panic(%s) }", errVar, errVar)
}

// blockCanReturnError reports whether a block contains any construct that
// causes the enclosing function to be a thrower — direct error triggers
// (`?`, `or { }`) OR a call to a known thrower recorded in g.errorFuncs.
// This is THE canonical "is this body a thrower" check, used both by the
// collectDecls fixed-point pass and by concurrency emitters that need to
// know whether a goroutine body can panic on an uncaught error.
func (g *Generator) blockCanReturnError(block *parser.BlockStmt) bool {
	if block == nil {
		return false
	}
	for _, s := range block.Stmts {
		if g.stmtCanReturnError(s) {
			return true
		}
	}
	return false
}

func (g *Generator) stmtCanReturnError(s parser.Stmt) bool {
	switch stmt := s.(type) {
	case *parser.ReturnStmt:
		if exprContainsPropagate(stmt.Value) {
			return true
		}
		// `return SomeError(...)` where SomeError is Error itself or
		// any descendant — the compiler widens the function signature
		// to (T, error).
		if g.exprIsErrorCtor(stmt.Value) {
			return true
		}
		// `return expr as T` — auto-widens because the cast can fail.
		if exprContainsAsCast(stmt.Value) {
			return true
		}
		// `return thrower()` — direct return of a thrower call. Same
		// rule as `var x = thrower()`: the enclosing function inherits
		// the thrower status. Mirrors VarStmt / AssignStmt / ExprStmt
		// which already check callReturnsError here.
		return g.callReturnsError(stmt.Value)
	case *parser.VarStmt:
		if exprContainsPropagate(stmt.Value) {
			return true
		}
		if g.orHandlerCanReturnError(stmt.OrHandler) {
			return true
		}
		// A non-throwing `or { ... }` handler consumes the call's error —
		// the enclosing function does NOT inherit thrower status from
		// this statement. Without this short-circuit a function like
		//   pub bool foo() {
		//     var x = thrower() or { return false }
		//     return true
		//   }
		// gets falsely classified as throwing because callReturnsError
		// reports yes for thrower(); the `or { }` handling is ignored.
		// The same rule applies to `as` casts — or-handler consumes.
		if stmt.OrHandler != nil {
			return false
		}
		return g.callReturnsError(stmt.Value) || exprContainsAsCast(stmt.Value)
	case *parser.AssignStmt:
		if exprContainsPropagate(stmt.Value) || exprContainsPropagate(stmt.Target) {
			return true
		}
		if g.orHandlerCanReturnError(stmt.OrHandler) {
			return true
		}
		if stmt.OrHandler != nil {
			return false
		}
		return g.callReturnsError(stmt.Value) || exprContainsAsCast(stmt.Value)
	case *parser.ExprStmt:
		if exprContainsPropagate(stmt.Expr) {
			return true
		}
		if g.orHandlerCanReturnError(stmt.OrHandler) {
			return true
		}
		if stmt.OrHandler != nil {
			return false
		}
		return g.callReturnsError(stmt.Expr) || exprContainsAsCast(stmt.Expr)
	case *parser.TupleVarStmt:
		return g.callReturnsError(stmt.Value) || exprContainsAsCast(stmt.Value)
	case *parser.IfStmt:
		if g.blockCanReturnError(stmt.Then) {
			return true
		}
		if stmt.ElseStmt != nil {
			return g.stmtCanReturnError(stmt.ElseStmt)
		}
	case *parser.BlockStmt:
		return g.blockCanReturnError(stmt)
	case *parser.ForStmt:
		return g.blockCanReturnError(stmt.Body)
	case *parser.WhileStmt:
		return g.blockCanReturnError(stmt.Body)
	case *parser.MatchStmt:
		for _, c := range stmt.Cases {
			if g.blockCanReturnError(c.Body) {
				return true
			}
		}
	case *parser.WithStmt:
		return g.blockCanReturnError(stmt.Body)
	}
	return false
}

// exprIsErrorCtor reports whether expr is a constructor call for a
// class that extends Error (nominal check through the class hierarchy).
// Handles:
//   - bare `Foo(...)` — local class, looked up in g.structs
//   - `pkg.Foo(...)` — class in a sibling subpackage (g.subpkgStructs)
// External Go deps are not yet supported — those would need the
// dependency's class decls to be loaded.
func (g *Generator) exprIsErrorCtor(e parser.Expr) bool {
	call, ok := e.(*parser.CallExpr)
	if !ok {
		return false
	}
	switch callee := call.Callee.(type) {
	case *parser.Ident:
		if _, ok := g.structs[callee.Name]; ok {
			return g.classExtendsError(callee.Name, map[string]bool{})
		}
	case *parser.SelectorExpr:
		pkg, ok := callee.Object.(*parser.Ident)
		if !ok {
			return false
		}
		if classes, ok := g.subpkgStructs[pkg.Name]; ok {
			if _, ok := classes[callee.Field]; ok {
				return g.classExtendsError(callee.Field, map[string]bool{})
			}
		}
	}
	return false
}

// orHandlerCanReturnError reports whether an `or { }` / `or match { }`
// handler makes the enclosing function a thrower.
//   - Any case/block body that itself throws (a `throw` or unhandled `?`).
//   - An `or match` with no wildcard (`case _`) case — unmatched errors
//     propagate via `return zero, err` in the generated type switch.
func (g *Generator) orHandlerCanReturnError(h *parser.OrHandler) bool {
	if h == nil {
		return false
	}
	if h.Body != nil && g.blockCanReturnError(h.Body) {
		return true
	}
	if len(h.MatchCases) > 0 {
		hasWildcard := false
		for _, c := range h.MatchCases {
			if c.Type == "" {
				hasWildcard = true
			}
			if c.Body != nil && g.blockCanReturnError(c.Body) {
				return true
			}
		}
		if !hasWildcard {
			return true
		}
	}
	return false
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
	g.emitGoroutineBody(p.Body)
	g.indent--
	g.writeln("}()")
	g.indent--
	g.writeln("}")
	g.writeln("_wg.Wait()")
}

func (g *Generator) emitWithStmt(w *parser.WithStmt) {
	if len(w.Resources) == 1 && w.Resources[0].Name == "_lock" {
		lockExpr := g.formatExpr(w.Resources[0].Value)
		if blockContainsReturn(w.Body) {
			// Body has return statements — use direct defer (returns must reach enclosing function)
			g.writeln("%s.Lock()", lockExpr)
			g.writeln("defer %s.Unlock()", lockExpr)
			g.emitBlock(w.Body)
		} else {
			// No returns — wrap in anonymous function so defer is scoped to the block.
			// Critical for lock blocks inside loops — without this, defer never fires.
			g.writeln("func() {")
			g.indent++
			g.writeln("%s.Lock()", lockExpr)
			g.writeln("defer %s.Unlock()", lockExpr)
			g.emitBlock(w.Body)
			g.indent--
			g.writeln("}()")
		}
		return
	}
	// `using (var r = init) { body }` — block-scoped RAII. When the
	// body has no user returns, wrap in an IIFE so defer fires at
	// block exit. If the body can throw, the IIFE also returns error
	// and propagates outward via emitErrReturn. With user returns,
	// fall back to function-scoped defer.
	if !blockContainsReturn(w.Body) {
		canThrow := g.blockCanReturnError(w.Body)
		if canThrow {
			errVar := "_uerr"
			if g.errVarCount > 0 {
				errVar = fmt.Sprintf("_uerr%d", g.errVarCount)
			}
			g.errVarCount++
			g.writeln("%s := func() error {", errVar)
			g.indent++
			g.withLocalScope(func() {
				for _, r := range w.Resources {
					g.writeln("%s := %s", r.Name, g.formatExpr(r.Value))
					g.writeln("defer %s.Close()", r.Name)
					g.declareLocal(r.Name)
				}
				g.emitBlock(w.Body)
			})
			g.writeln("return nil")
			g.indent--
			g.writeln("}()")
			g.writeln("if %s != nil {", errVar)
			g.indent++
			g.emitErrReturn(errVar)
			g.indent--
			g.writeln("}")
			return
		}
		g.writeln("func() {")
		g.indent++
		g.withLocalScope(func() {
			for _, r := range w.Resources {
				g.writeln("%s := %s", r.Name, g.formatExpr(r.Value))
				g.writeln("defer %s.Close()", r.Name)
				g.declareLocal(r.Name)
			}
			g.emitBlock(w.Body)
		})
		g.indent--
		g.writeln("}()")
		return
	}
	g.withLocalScope(func() {
		for _, r := range w.Resources {
			g.writeln("%s := %s", r.Name, g.formatExpr(r.Value))
			g.writeln("defer %s.Close()", r.Name)
			g.declareLocal(r.Name)
		}
		g.emitBlock(w.Body)
	})
}

// blockEndsInReturn reports whether the final statement of a block is
// a return. Used to decide whether a thrower function needs a synthetic
// trailing `return nil`. Shallow check — doesn't recurse into if/else.
func blockEndsInReturn(block *parser.BlockStmt) bool {
	if block == nil || len(block.Stmts) == 0 {
		return false
	}
	last := block.Stmts[len(block.Stmts)-1]
	switch last.(type) {
	case *parser.ReturnStmt:
		return true
	}
	return false
}

// blockContainsReturn checks if a block contains any return statement (recursively).
func blockContainsReturn(block *parser.BlockStmt) bool {
	if block == nil {
		return false
	}
	for _, s := range block.Stmts {
		switch st := s.(type) {
		case *parser.ReturnStmt:
			return true
		case *parser.IfStmt:
			if blockContainsReturn(st.Then) {
				return true
			}
			if elseBlock, ok := st.ElseStmt.(*parser.BlockStmt); ok {
				if blockContainsReturn(elseBlock) {
					return true
				}
			}
			if elseIf, ok := st.ElseStmt.(*parser.IfStmt); ok {
				if blockContainsReturn(elseIf.Then) {
					return true
				}
			}
		case *parser.ForStmt:
			if blockContainsReturn(st.Body) {
				return true
			}
		case *parser.WhileStmt:
			if blockContainsReturn(st.Body) {
				return true
			}
		case *parser.MatchStmt:
			for _, c := range st.Cases {
				if blockContainsReturn(c.Body) {
					return true
				}
			}
		}
	}
	return false
}

func (g *Generator) emitTupleVarStmt(t *parser.TupleVarStmt) {
	names := strings.Join(t.Names, ", ")
	g.writeln("%s := %s", names, g.formatExpr(t.Value))
	for _, n := range t.Names {
		g.declareLocal(n)
	}
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
	g.withLocalScope(func() {
		for _, s := range block.Stmts {
			g.emitStmt(s)
		}
	})
}

// withLocalScope runs fn with currentLocals shallow-copied, so any names
// added inside fn don't leak to the caller. Used by emitBlock and by
// compound statements (for, with) that bind names visible only in their
// body. Implements lexical block scoping for the implicit-self rewrite.
func (g *Generator) withLocalScope(fn func()) {
	saved := g.currentLocals
	scope := make(map[string]bool, len(saved))
	for k := range saved {
		scope[k] = true
	}
	g.currentLocals = scope
	defer func() { g.currentLocals = saved }()
	fn()
}

// declareLocal registers a name as locally declared in the current scope.
// Called from VarStmt / TupleVarStmt emission and from compound statements
// that bind names (for-range loop vars, with/using/lock resources).
func (g *Generator) declareLocal(name string) {
	if name == "" || g.currentLocals == nil {
		return
	}
	g.currentLocals[name] = true
}
