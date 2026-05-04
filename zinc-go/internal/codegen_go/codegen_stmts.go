package codegen_go

// Statement emission: var, assign, return, if, for, while, match,
// parallel for, concurrent, spawn, defer, assert, etc.

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
	"zinc-go/internal/typechecker"
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
		// Hoist failable constructs in the cond before the for-header.
		// Note: hoist runs once at entry — if the body mutates state
		// such that the cond would re-evaluate, the hoisted temp is
		// stale. For thrower-call conds this is the right semantic
		// (re-throwing every iteration would be surprising); a user
		// who needs per-iteration evaluation should do it inside the
		// body via `var x = thrower() or { break }`.
		cond := stmt.Cond
		if exprContainsAsCast(cond) || g.exprContainsNestedThrowerCall(cond) {
			cond = g.hoistArg(cond)
		} else if call, ok := cond.(*parser.CallExpr); ok && g.callReturnsError(call) && !g.callIsVoidThrower(call) {
			cond = g.hoistArg(cond)
		}
		// `while (true) { ... }` lowers to Go's idiomatic infinite-loop
		// form `for { ... }` rather than `for true { ... }`. Both compile,
		// but bare `for {}` is what a Go dev would write.
		if lit, ok := cond.(*parser.BoolLit); ok && lit.Value {
			g.writeln("for {")
		} else {
			g.writeln("for %s {", g.formatExpr(cond))
		}
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
		// Side-map first (Phase 3.4 type-tracking migration) — falls back
		// to ad-hoc varTypes when bind isn't on.
		if g.bound != nil {
			if t, ok := g.bound.NodeTypes[e]; ok && t.Name != "" && t.Name != "any" {
				return t.Name
			}
		}
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
	// auto-propagate via emitErrReturn — the enclosing function must be
	// declared a thrower (signature with `error` tail) for this to work.
	if ta, ok := v.Value.(*parser.TypeAssertExpr); ok && !ta.IsCheck {
		g.emitTypeAssertVar(v, ta)
		return
	}
	// Nested failable expr: hoist each `as` (and nested thrower call) to
	// a temp above this statement, then emit the var with the rewritten RHS.
	if v.Value != nil && (exprContainsAsCast(v.Value) || g.exprContainsNestedThrowerCall(v.Value)) {
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
						_ = ident.Name // varStructTypes write removed
					}
				} else if strings.HasPrefix(ident.Name, "New") {
					structName := ident.Name[3:]
					if g.isClassType(structName) {
						_ = structName // varStructTypes write removed
					}
				}
			}
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				if pkg, ok := sel.Object.(*parser.Ident); ok {
					if exports, ok := g.subpkgExports[pkg.Name]; ok {
						if kind := exports[sel.Field]; kind == "class" || kind == "data" {
							_ = sel.Field // varStructTypes write removed
						}
					}
				}
				// Track method call return type
				retType := g.resolveMethodReturnType(sel)
				if retType != "" && g.isClassType(retType) {
					_ = retType // varStructTypes write removed
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
						_ = ident.Name // varStructTypes write removed
					}
				} else if strings.HasPrefix(ident.Name, "New") {
					structName := ident.Name[3:]
					if g.isClassType(structName) {
						_ = structName // varStructTypes write removed
					}
				} else if g.isClassType(ident.Name) {
					_ = ident.Name // varStructTypes write removed
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
								_ = st.Name // varStructTypes write removed
							}
							_ = m // gt-tracking removed (Phase 3.7.2): side-map covers it
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
							_ = sel.Field // varStructTypes write removed
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
					_ = retType // varStructTypes write removed
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
				_ = sel // gt-tracking removed (Phase 3.7.2): side-map covers it
			}
		}

		// Infer type from list literal
		if listLit, ok := v.Value.(*parser.ListLit); ok && v.Type == nil {
			elemType := inferListLitElemType(listLit.Elements)
			if elemType != "interface{}" {
				g.varTypes[v.Name] = elemType
			}
		}

		// Track pointer vars from optional-returning functions. Three paths:
		//   1. Top-level FnDecls and class methods — `funcReturnsOptional`
		//      was populated during collectDecls (keyed by unqualified
		//      name; methods register their own name there too).
		//   2. Method calls on a typed receiver — `obj.method()` where
		//      obj's class has the method registered in funcReturnsOptional.
		//   3. Bind side-map fallback — read the V2Type for the init
		//      expression and check `Nullable`. Picks up paths the
		//      codegen-side tables don't cover.
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				if g.funcReturnsOptional[ident.Name] {
					g.ptrVars[v.Name] = true
				}
			}
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				if g.funcReturnsOptional[sel.Field] {
					g.ptrVars[v.Name] = true
				}
			}
		}
		if g.bound != nil {
			if t, ok := g.bound.NodeTypes[v.Value]; ok && t.Nullable {
				g.ptrVars[v.Name] = true
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
				if ident, ok := sel.Object.(*parser.Ident); ok && !g.isUserScopeShadowIdent(ident) {
					if pkgPath, ok := g.importMap[ident.Name]; ok {
						if retType := g.goResolver.FuncReturnType(pkgPath, sel.Field); retType != nil {
							_ = retType // varGoTypes write removed (Phase 3.7.2)
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
		// LHS is Fn<...> and RHS is a lambda literal: publish the target
		// Fn type so formatLambdaExpr can drive the lambda's Go return
		// type from the slot, instead of defaulting to interface{} when
		// the body has expressions inferLambdaReturnType can't resolve.
		if _, isLambda := v.Value.(*parser.LambdaExpr); isLambda && v.Type != nil {
			if ft := g.resolveFuncTypeExpr(v.Type); ft != nil {
				prev := g.pendingLambdaTarget
				g.pendingLambdaTarget = ft
				defer func() { g.pendingLambdaTarget = prev }()
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
	// Nested failable expr on RHS: hoist any `as` cast or nested
	// thrower call to temps, then emit the rewritten assign.
	if a.Value != nil && (exprContainsAsCast(a.Value) || g.exprContainsNestedThrowerCall(a.Value)) {
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
		// Class-typed var: classes lower to *Class in Go, so the
		// underlying value is already a pointer. Without this branch the
		// auto-address-take would double-wrap class instances assigned
		// from constructor calls.
		if g.bound != nil {
			if t, ok := g.bound.NodeTypes[v]; ok && g.isClassType(t.Name) {
				return true
			}
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
	// Final fallback: any expression whose static type resolves to a
	// class is already a `*Class` in Go (classes always lower to
	// pointers). This catches IndexExpr (`m[k]`, `xs[i]`) and other
	// shapes that the type-specific cases above don't cover.
	if g.bound != nil {
		if t, ok := g.bound.NodeTypes[value]; ok && g.isClassType(t.Name) {
			return true
		}
	}
	return false
}

func (g *Generator) emitReturnStmt(r *parser.ReturnStmt) {
	// Hoist failable sub-expressions in the returned value: `as` casts
	// and nested thrower calls each get lifted to a temp + error guard
	// above the return, with the value expression rewritten to
	// reference the temps.
	if r.Value != nil && (exprContainsAsCast(r.Value) || g.exprContainsNestedThrowerCall(r.Value)) {
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

	// Declared-thrower handling (signature explicitly contains `error` in
	// the tail). Distinct from the legacy auto-widen path which uses
	// currentReturnType for a single value-slot Go type — declared
	// throwers carry per-slot Go types in currentThrowerValueGoTypes so
	// multi-value forms can render zero values correctly.
	if g.currentReturnIsDeclaredThrower {
		valueTypes := g.currentThrowerValueGoTypes
		// `return SomeError(...)` — single error-class value. Fill value
		// slots with zero, then the error. Also catches `return err` in
		// an `or { }` block, where `err` is the in-scope error variable
		// — propagation shorthand for `or { return err }` from a thrower
		// caller.
		if g.exprIsErrorCtor(r.Value) || g.exprIsErrorVar(r.Value) {
			parts := make([]string, 0, len(valueTypes)+1)
			for _, vt := range valueTypes {
				parts = append(parts, g.zeroValueFor(vt))
			}
			parts = append(parts, g.formatExpr(r.Value))
			g.writeln("return %s", strings.Join(parts, ", "))
			return
		}
		// `return thrower()` — the call already returns the matching
		// (..., error) tuple; pass through.
		if g.callReturnsError(r.Value) {
			g.writeln("return %s", g.formatExpr(r.Value))
			return
		}
		// Bare-error thrower returning a single value (typically `null`):
		// emit it as the error slot directly.
		if len(valueTypes) == 0 {
			g.writeln("return %s", g.formatExpr(r.Value))
			return
		}
		// Single-value thrower (`(T, error)`) returning a non-error,
		// non-tuple value: auto-fill the error slot with nil. Lets the
		// user write `return v` instead of `return v, null` in the
		// success path. Multi-value throwers must spell out all slots.
		if len(valueTypes) == 1 {
			if _, isTup := r.Value.(*parser.TupleLit); !isTup {
				g.writeln("return %s, nil", g.formatExpr(r.Value))
				return
			}
		}
		// TupleLit with all slots — fall through to the existing
		// currentReturnIsTuple branch, which emits comma-separated.
		// Anything else (single non-error value into a multi-value
		// thrower) is malformed — let Go surface the type error.
	}

	// Multi-value return: function declared with TupleType return type
	// (e.g. `pub (Int, String) foo()`) and the value is a TupleLit (built
	// by the parser from `return a, b` or explicit `(a, b)`). Lower to
	// Go's `return a, b` form instead of the default TupleLit-as-slice.
	if g.currentReturnIsTuple {
		if tup, ok := r.Value.(*parser.TupleLit); ok {
			var parts []string
			for _, el := range tup.Elements {
				parts = append(parts, g.formatExpr(el))
			}
			g.writeln("return %s", strings.Join(parts, ", "))
			return
		}
	}

	// Optional return: a function declared `T?` lowers to a `*T` Go
	// signature (per spec §1.3). The body's return value may be any of:
	//   - `null`             → emit `return nil`
	//   - already a pointer   → pass through (class types format with a
	//                          leading `*`, so no wrap; same for an
	//                          existing `*T` ident or a function call
	//                          whose return type is a pointer)
	//   - a value type        → wrap with `_zincPtr(...)` so the value
	//                          gets boxed into a heap pointer that
	//                          satisfies the `*T` return slot
	if g.currentReturnOptional {
		if _, ok := r.Value.(*parser.NullLit); ok {
			g.writeln("return nil")
			return
		}
		val := g.formatExpr(r.Value)
		if g.valueIsAlreadyPointer(r.Value) {
			g.writeln("return %s", val)
			return
		}
		g.writeln("return %s", g.wrapAsPointer(val))
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
	// Hoist any failable construct in the condition before emitting
	// the `if` header. The condition is a single-value position, so
	// thrower calls (which return tuples) and `?`/`as` casts must be
	// lifted to temps above the `if`. After hoisting the condition is
	// guaranteed to be a single-value expression suitable for Go's
	// `if cond { ... }`.
	cond := s.Cond
	if exprContainsAsCast(cond) || g.exprContainsNestedThrowerCall(cond) {
		cond = g.hoistArg(cond)
	} else if call, ok := cond.(*parser.CallExpr); ok && g.callReturnsError(call) && !g.callIsVoidThrower(call) {
		// Top-level thrower call as the cond — also a single-value
		// position, hoist directly via hoistArg.
		cond = g.hoistArg(cond)
	}
	// Map containsKey check in cond position emits the natural Go form
	// `if _, _ok := m[k]; _ok { ... }` instead of an IIFE wrapper. Pure
	// readability win in the generated Go (Zinc's value-prop is that
	// the output is editable). Negation `!m.containsKey(k)` lowers to
	// `; !_ok`. Only fires when the call is in plain or single-negated
	// position; compound boolean exprs fall back to the IIFE form.
	if init, ck, ok := g.tryContainsKeyIfHeader(cond); ok {
		g.writeln("if %s; %s {", init, ck)
	} else {
		g.writeln("if %s {", g.formatExpr(cond))
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
		if g.bound != nil {
			if t, ok := g.bound.NodeTypes[ident]; ok && t.Name == "Map" {
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
		if g.bound != nil {
			if t, ok := g.bound.NodeTypes[ident]; ok {
				if t.Name == "List" || strings.HasSuffix(t.Name, "[]") {
					return true
				}
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
		if g.bound != nil {
			if t, ok := g.bound.NodeTypes[ident]; ok && t.Name == "Channel" {
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
				// Side-map first (Phase 3.4 resolution migration): bind
				// resolved this ident to a definite Symbol; SymType /
				// SymSealedVariant means it's a known type/variant pattern.
				if g.bound != nil {
					if sym, ok := g.bound.Bindings[ident]; ok {
						switch sym.Kind {
						case typechecker.SymType, typechecker.SymClass, typechecker.SymDataClass,
							typechecker.SymInterface, typechecker.SymEnum, typechecker.SymSealedVariant:
							return true
						}
					}
				}
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
			} else if g.bound != nil {
				// Side-map first (Phase 3.4 resolution migration). With
				// granular SymKind we know whether the cross-pkg type is
				// a data class (no pointer) vs a regular class/interface
				// (pointer prefix).
				handled := false
				if sym, ok := g.bound.Bindings[ident]; ok && sym.Pkg != "" {
					switch sym.Kind {
					case typechecker.SymDataClass, typechecker.SymSealedVariant:
						typeName = sym.Pkg + "." + exportName(ident.Name)
						isDataClass = true
						handled = true
					case typechecker.SymClass, typechecker.SymInterface:
						typeName = "*" + sym.Pkg + "." + exportName(ident.Name)
						handled = true
					}
				}
				if !handled {
					if entry, ok := g.unqualifiedNames[ident.Name]; ok {
						typeName = entry.pkg + "." + exportName(ident.Name)
						if entry.kind == "class" || entry.kind == "interface" {
							typeName = "*" + typeName
						}
						isDataClass = entry.kind == "data"
					} else if _, ok := g.structs[ident.Name]; ok {
						typeName = "*" + exportName(ident.Name)
					}
				}
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
				// Side-map first (Phase 3.4 resolution migration). With
				// granular SymKind, only data classes / sealed variants
				// have positional fields to destructure.
				if g.bound != nil {
					if sym, ok := g.bound.Bindings[callerIdent]; ok && sym.Pkg != "" {
						switch sym.Kind {
						case typechecker.SymDataClass, typechecker.SymSealedVariant:
							if pkgFields, ok := g.subpkgDataFields[sym.Pkg]; ok {
								fieldParams = pkgFields[callerIdent.Name]
							}
						}
					}
				}
				if fieldParams == nil {
					if entry, ok := g.unqualifiedNames[callerIdent.Name]; ok {
						if pkgFields, ok := g.subpkgDataFields[entry.pkg]; ok {
							fieldParams = pkgFields[callerIdent.Name]
						}
					}
				}
			} else if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				// Qualified pattern `pkg.Variant(binders...)` — look up
				// fieldParams via the package alias rather than the bind
				// side-map (the SelectorExpr's outer Ident binds to a
				// package alias, not the variant). Without this branch,
				// `case lib.Ok(v) → v := _v.V` (wrong field name from
				// the binder) instead of `_v.Value` (the data field).
				if pkgIdent, ok := sel.Object.(*parser.Ident); ok {
					if pkgFields, ok := g.subpkgDataFields[pkgIdent.Name]; ok {
						fieldParams = pkgFields[sel.Field]
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

	// If the match isn't provably exhaustive (non-sealed subject, or sealed
	// with missing variants and no wildcard), add a synthetic default that
	// panics so an unmatched runtime type fails loudly. Go itself doesn't
	// require type-switches to be exhaustive — control just falls out of
	// the switch on no match — but for sealed-typed subjects we want the
	// panic, and for matches that the typechecker flagged as non-exhaustive
	// we want a visible failure rather than silent fall-through.
	if !g.matchIsExhaustive(m) {
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
	sealed, missing := g.matchSealedCoverage(m)
	if sealed != nil && len(missing) > 0 {
		g.compileError(m.Line,
			"non-exhaustive match on sealed type %q: missing variant(s) %s (add case(s) or an else branch)",
			sealed.Name, strings.Join(missing, ", "))
	}
}

// matchIsExhaustive reports whether the match is provably exhaustive — either
// it has a wildcard/else arm, or it covers every variant of a sealed type.
// Used to suppress dead default + trailing-return emissions after the switch.
// Returns false for non-sealed type-switches (matching on Object, etc.) since
// we can't enumerate possible runtime types.
func (g *Generator) matchIsExhaustive(m *parser.MatchStmt) bool {
	for _, c := range m.Cases {
		if c.Pattern == nil {
			return true
		}
	}
	sealed, missing := g.matchSealedCoverage(m)
	return sealed != nil && len(missing) == 0
}

// matchSealedCoverage identifies the sealed class being matched (if any) and
// returns the variants left uncovered. Returns (nil, nil) for matches that
// don't target a sealed class. Wildcard/else arms make missing empty.
func (g *Generator) matchSealedCoverage(m *parser.MatchStmt) (*parser.ClassDecl, []string) {
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
		return nil, nil
	}

	covered := make(map[string]bool)
	for _, c := range m.Cases {
		if c.Pattern == nil {
			return sealed, nil // wildcard covers everything remaining
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
	return sealed, missing
}

// --- Expression statements (spawn, print, collection methods, forEach) -------

func (g *Generator) emitExprStmt(es *parser.ExprStmt) {
	if es.OrHandler != nil {
		g.emitOrAssignment("_", es.Expr, es.OrHandler)
		return
	}
	// Nested failable expr in an expression statement: hoist any `as`
	// cast or nested thrower call to temps, then emit the rewritten
	// statement.
	if exprContainsAsCast(es.Expr) || g.exprContainsNestedThrowerCall(es.Expr) {
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
	// Qualified cross-package names like `core.Schema` — split on the
	// dot and consult subpkgStructs by package.
	if idx := strings.IndexByte(name, '.'); idx > 0 {
		pkg, bare := name[:idx], name[idx+1:]
		if pkgClasses, ok := g.subpkgStructs[pkg]; ok {
			if _, ok := pkgClasses[bare]; ok {
				return true
			}
		}
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
		// Side-map first — when the typechecker has resolved this Ident
		// to a struct/data class, that's the authoritative answer.
		if g.bound != nil {
			if t, ok := g.bound.NodeTypes[ident]; ok && t.Name != "" && t.Name != "any" {
				receiverType = t.Name
			}
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
		// Side-map first: bind tells us this Ident's V2Type. If the
		// type name is a known class/data-class, treat it as a struct.
		if g.bound != nil {
			if t, ok := g.bound.NodeTypes[ident]; ok && g.isClassType(t.Name) {
				return true
			}
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
		// for a trailing error result. Side-map first (Phase 3.4
		// resolution migration); falls back to ad-hoc unqualifiedNames
		// when bound is unset.
		if g.bound != nil {
			if sym, ok := g.bound.Bindings[callee]; ok && sym.Kind == typechecker.SymFn && sym.Pkg != "" {
				if pkgPath, ok := g.importMap[sym.Pkg]; ok {
					if g.goResolver.ReturnsError(pkgPath, callee.Name) {
						return true
					}
				}
			}
		}
		if entry, ok := g.unqualifiedNames[callee.Name]; ok && entry.kind == "func" {
			if pkgPath, ok := g.importMap[entry.pkg]; ok {
				if g.goResolver.ReturnsError(pkgPath, entry.name) {
					return true
				}
			}
		}
		// Fn-typed local invoked by name: `var fac = registry[name]; fac(ctx, cfg)`.
		// Side-map only (Phase 3.7.2): Symbol.DeclType for explicit
		// `var fac: Fn<...> = ...`, then NodeTypes[useIdent].TypeExpr
		// for inferred locals. Both Map-index and class-field paths
		// now flow through V2Type.TypeExpr / classFields.
		var declType parser.TypeExpr
		if g.bound != nil {
			if sym, ok := g.bound.Bindings[callee]; ok && sym.DeclType != nil {
				declType = sym.DeclType
			}
			if declType == nil {
				if t, ok := g.bound.NodeTypes[callee]; ok && t.TypeExpr != nil {
					declType = t.TypeExpr
				}
			}
		}
		if declType != nil {
			if ft := g.resolveFuncTypeExpr(declType); ft != nil {
				if returnTypeDeclaresError(ft.ReturnType) {
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
		if ident, ok := callee.Object.(*parser.Ident); ok && !g.isUserScopeShadowIdent(ident) {
			if pkgPath, ok := g.importMap[ident.Name]; ok {
				if g.goResolver.ReturnsError(pkgPath, callee.Field) {
					return true
				}
			}
		}
	}
	return false
}

// resolveFuncTypeExpr returns the underlying FuncTypeExpr for `t`,
// peeling type aliases as needed (single-package and cross-package).
// Returns nil if `t` doesn't bottom out at a function type.
func (g *Generator) resolveFuncTypeExpr(t parser.TypeExpr) *parser.FuncTypeExpr {
	if t == nil {
		return nil
	}
	if ft, ok := t.(*parser.FuncTypeExpr); ok {
		return ft
	}
	if simple, ok := t.(*parser.SimpleType); ok {
		if alias, exists := g.typeAliases[simple.Name]; exists {
			return g.resolveFuncTypeExpr(alias)
		}
		// Cross-package: param/field types referencing an alias declared
		// in another package surface here as a bare SimpleType.
		// subpkgTypeAliases is populated by the compiler driver from
		// every imported package's CollectTypeAliases output.
		for _, pkgAliases := range g.subpkgTypeAliases {
			if alias, exists := pkgAliases[simple.Name]; exists {
				return g.resolveFuncTypeExpr(alias)
			}
		}
	}
	return nil
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
				// Syntactic check — the declared return type is the
				// definitive thrower marker. No body inspection.
				return returnTypeDeclaresError(m.ReturnType)
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
		parentName := parent.Name
		if idx := strings.LastIndex(parent.Name, "."); idx >= 0 {
			parentName = parent.Name[idx+1:]
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
	// Declared-thrower path: render per-slot zeros from
	// currentThrowerValueGoTypes, then the error. Bare-error throwers
	// (no value slots) emit just `return errVar`.
	if g.currentReturnIsDeclaredThrower {
		if len(g.currentThrowerValueGoTypes) == 0 {
			g.writeln("return %s", errVar)
			return
		}
		parts := make([]string, 0, len(g.currentThrowerValueGoTypes)+1)
		for _, vt := range g.currentThrowerValueGoTypes {
			parts = append(parts, g.zeroValueFor(vt))
		}
		parts = append(parts, errVar)
		g.writeln("return %s", strings.Join(parts, ", "))
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
			// "error" is the bare-error declared form (`pub error f()`)
			// — no value slot, so it's a void thrower for call-site
			// destructure purposes.
			return !exists || rt == "" || rt == "void" || rt == "error"
		}
		// Unqualified Go stdlib — ask the resolver about return arity.
		// Side-map first; falls back to ad-hoc unqualifiedNames.
		if g.bound != nil {
			if sym, ok := g.bound.Bindings[ident]; ok && sym.Kind == typechecker.SymFn && sym.Pkg != "" {
				if pkgPath, ok := g.importMap[sym.Pkg]; ok {
					return g.goResolver.ReturnsErrorOnly(pkgPath, ident.Name)
				}
			}
		}
		if entry, ok := g.unqualifiedNames[ident.Name]; ok && entry.kind == "func" {
			if pkgPath, ok := g.importMap[entry.pkg]; ok {
				return g.goResolver.ReturnsErrorOnly(pkgPath, entry.name)
			}
		}
		// Fn-typed local: a bare-`error` slot like `Fn<(...), error>`
		// dispatches to the void destructure form (`_err := f()`).
		// Side-map only; same shape as callReturnsError.
		var declType parser.TypeExpr
		if g.bound != nil {
			if sym, ok := g.bound.Bindings[ident]; ok && sym.DeclType != nil {
				declType = sym.DeclType
			}
			if declType == nil {
				if t, ok := g.bound.NodeTypes[ident]; ok && t.TypeExpr != nil {
					declType = t.TypeExpr
				}
			}
		}
		if declType != nil {
			if ft := g.resolveFuncTypeExpr(declType); ft != nil {
				if isZincErrorType(ft.ReturnType) {
					return true
				}
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
// failable sub-expression it contains (TypeAssertExpr `as` casts and
// nested thrower-call expressions via hoistArg), and returns a
// rewritten expression with those constructs replaced by Idents
// pointing at the hoisted temps. Used by statement emitters so
// `return f(g(x))` where g throws, `foo(bar() as T) + 1`, and
// similar shapes lower to idiomatic Go. LambdaExpr is a separate
// scope and is not descended into — a failable construct inside a
// lambda is the lambda's own codegen concern.
//
// The top-level entry expression itself is NOT hoisted as a
// thrower-call — that case is handled by the calling statement
// emitter (emitErrorPropagatingVar for `var x = thrower()`,
// pass-through return for `return thrower()`, etc.). To hoist a
// nested position, descend via hoistArg, which catches thrower
// calls in addition to running hoistPropagates' descent.
func (g *Generator) hoistPropagates(e parser.Expr) parser.Expr {
	if e == nil {
		return nil
	}
	switch expr := e.(type) {
	case *parser.BinaryExpr:
		return &parser.BinaryExpr{
			Op:    expr.Op,
			Left:  g.hoistArg(expr.Left),
			Right: g.hoistArg(expr.Right),
		}
	case *parser.UnaryExpr:
		return &parser.UnaryExpr{Op: expr.Op, Operand: g.hoistArg(expr.Operand)}
	case *parser.CallExpr:
		newCall := *expr
		newCall.Callee = g.hoistArg(expr.Callee)
		if len(expr.Args) > 0 {
			newArgs := make([]parser.Expr, len(expr.Args))
			for i, a := range expr.Args {
				newArgs[i] = g.hoistArg(a)
			}
			newCall.Args = newArgs
		}
		if len(expr.NamedArgs) > 0 {
			newNamed := make([]parser.NamedArg, len(expr.NamedArgs))
			for i, na := range expr.NamedArgs {
				newNamed[i] = parser.NamedArg{Name: na.Name, Value: g.hoistArg(na.Value)}
			}
			newCall.NamedArgs = newNamed
		}
		return &newCall
	case *parser.SelectorExpr:
		return &parser.SelectorExpr{Object: g.hoistArg(expr.Object), Field: expr.Field}
	case *parser.IndexExpr:
		return &parser.IndexExpr{Object: g.hoistArg(expr.Object), Index: g.hoistArg(expr.Index)}
	case *parser.SafeNavExpr:
		out := &parser.SafeNavExpr{Object: g.hoistArg(expr.Object), Field: expr.Field}
		if expr.Call != nil {
			call := *expr.Call
			if len(expr.Call.Args) > 0 {
				newArgs := make([]parser.Expr, len(expr.Call.Args))
				for i, a := range expr.Call.Args {
					newArgs[i] = g.hoistArg(a)
				}
				call.Args = newArgs
			}
			out.Call = &call
		}
		return out
	case *parser.TypeAssertExpr:
		// `is` predicate — pure expression, just descend.
		if expr.IsCheck {
			return &parser.TypeAssertExpr{Object: g.hoistArg(expr.Object), TypeExpr: expr.TypeExpr, TypeName: expr.TypeName, IsCheck: true}
		}
		// `as` cast — failable. Emit comma-ok + error guard (or, for
		// `T? as T` unwrap, nil-check + deref), replace with an Ident
		// pointing at the bound value. The enclosing function must
		// declare `error` in its return tail to absorb the propagation
		// emitted by emitErrReturn.
		inner := g.hoistArg(expr.Object)
		count := g.errVarCount
		tmpName := fmt.Sprintf("_p%d", count)
		okName := fmt.Sprintf("_ok%d", count)
		errName := g.nextErrName() // bumps errVarCount
		var goType string
		if expr.TypeExpr != nil {
			goType = g.formatType(expr.TypeExpr)
		} else {
			goType = g.formatType(&parser.SimpleType{Name: expr.TypeName})
		}
		g.needImport("fmt")
		operand := g.formatExpr(inner)
		if g.exprIsPointerOptional(inner) {
			// Optional unwrap: `T? as T`. Operand is `*T`; nil → null
			// unwrap error, non-nil → pass through (class target,
			// goType already `*Tag`) or deref (value-type target,
			// goType `string` while operand is `*string`).
			targetIsPointer := strings.HasPrefix(goType, "*")
			g.writeln("var %s %s", tmpName, goType)
			g.writeln("%s := %s != nil", okName, operand)
			g.writeln("if %s {", okName)
			g.indent++
			if targetIsPointer {
				g.writeln("%s = %s", tmpName, operand)
			} else {
				g.writeln("%s = *%s", tmpName, operand)
			}
			g.indent--
			g.writeln("} else {")
			g.indent++
			g.writeln("%s := fmt.Errorf(%q)", errName, "null unwrap (as "+expr.TypeName+")")
			g.emitErrReturn(errName)
			g.indent--
			g.writeln("}")
			return &parser.Ident{Name: tmpName}
		}
		g.writeln("%s, %s := %s.(%s)", tmpName, okName, operand, goType)
		g.writeln("if !%s {", okName)
		g.indent++
		g.writeln("%s := fmt.Errorf(%q)", errName, "type assertion failed: expected "+expr.TypeName)
		g.emitErrReturn(errName)
		g.indent--
		g.writeln("}")
		return &parser.Ident{Name: tmpName}
	case *parser.SpreadExpr:
		return &parser.SpreadExpr{Expr: g.hoistArg(expr.Expr)}
	case *parser.RangeExpr:
		return &parser.RangeExpr{Start: g.hoistArg(expr.Start), End: g.hoistArg(expr.End), Inclusive: expr.Inclusive}
	case *parser.LambdaExpr:
		// Separate scope — don't descend. Any failable construct inside
		// the lambda body is the lambda's own codegen concern.
		return expr
	default:
		return e
	}
}

// condCanReturnError reports whether a condition expression (if/for/
// while header) contains a failable construct that would widen the
// enclosing function. The condition is a single-value position, so
// any thrower call (top-level OR nested), `?`, or `as` cast there
// makes the function a thrower.
func (g *Generator) condCanReturnError(cond parser.Expr) bool {
	if cond == nil {
		return false
	}
	if exprContainsAsCast(cond) {
		return true
	}
	if g.exprContainsNestedThrowerCall(cond) {
		return true
	}
	if call, ok := cond.(*parser.CallExpr); ok && g.callReturnsError(call) {
		return true
	}
	return false
}

// hoistArg is hoistPropagates extended to also lift bare thrower calls
// in nested positions. Used by hoistPropagates' recursive descent.
// When this expression itself is a thrower call (and not a void
// thrower, which can't legally appear in a value position), emits
// `_pN, errM := call(); if errM != nil { ... }` and returns an
// Ident referring to the temp. Otherwise delegates to
// hoistPropagates so `?` / `as` are still handled.
func (g *Generator) hoistArg(e parser.Expr) parser.Expr {
	if e == nil {
		return nil
	}
	// Recurse into children first (catches `?`/`as`/nested-thrower
	// calls deeper in the tree). The result might still be a CallExpr
	// at the top — if so, check whether THIS call is itself a thrower
	// and hoist it too.
	rewritten := g.hoistPropagates(e)
	if call, ok := rewritten.(*parser.CallExpr); ok &&
		g.callReturnsError(call) && !g.callIsVoidThrower(call) {
		tmpName := fmt.Sprintf("_p%d", g.errVarCount)
		errName := g.nextErrName()
		g.writeln("%s, %s := %s", tmpName, errName, g.formatExpr(call))
		g.writeln("if %s != nil {", errName)
		g.indent++
		g.emitErrReturn(errName)
		g.indent--
		g.writeln("}")
		return &parser.Ident{Name: tmpName}
	}
	return rewritten
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
				_ = ident.Name // varStructTypes write removed
			} else if strings.HasPrefix(ident.Name, "New") {
				structName := ident.Name[3:]
				if g.isClassType(structName) {
					_ = structName // varStructTypes write removed
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
// must declare `error` in its return tail for propagation to compile.
//
// `T? as T` (forced-unwrap form) takes a different shape: the operand
// is stored as `*T` (per the T? lowering), not as an interface{}. Go
// rejects `any(*T).(T)` for non-pointer-to-interface concretes, and
// even where it works the runtime semantics ("does the dynamic type
// match T?") aren't what `T? as T` means ("is the pointer non-nil,
// then deref"). For the optional-unwrap shape we emit the nil-check
// + deref directly, with the same `or { }` / auto-propagate handling
// for the null case.
func (g *Generator) emitTypeAssertVar(v *parser.VarStmt, ta *parser.TypeAssertExpr) {
	count := g.errVarCount
	okName := fmt.Sprintf("_ok%d", count)
	errName := g.nextErrName() // bumps errVarCount
	var goType string
	if ta.TypeExpr != nil {
		goType = g.formatType(ta.TypeExpr)
	} else {
		goType = g.formatType(&parser.SimpleType{Name: ta.TypeName})
	}
	g.needImport("fmt")
	operand := g.formatExpr(ta.Object)

	// Optional unwrap path: operand is `*T`. Go's type-assertion shape
	// doesn't apply here. For value-type T (String? → *string), the
	// non-null branch dereferences to obtain T. For class-type T
	// (Tag? → *Tag, since classes already lower to *Tag and T? doesn't
	// double-pointer per the ee605e0 fix), the operand IS the *Tag and
	// no deref is needed — the target var is also *Tag.
	if g.exprIsPointerOptional(ta.Object) {
		targetIsPointer := strings.HasPrefix(goType, "*")
		g.writeln("var %s %s", v.Name, goType)
		g.writeln("%s := %s != nil", okName, operand)
		g.writeln("if %s {", okName)
		g.indent++
		if targetIsPointer {
			g.writeln("%s = %s", v.Name, operand)
		} else {
			g.writeln("%s = *%s", v.Name, operand)
		}
		g.indent--
		g.writeln("} else {")
		g.indent++
		if v.OrHandler != nil && v.OrHandler.Body != nil {
			g.writeln("%s := fmt.Errorf(%q)", errName, "null unwrap (as "+ta.TypeName+")")
			g.writeln("_ = %s", errName)
			savedErrVar := g.currentErrVar
			g.currentErrVar = errName
			g.emitOrBlock(v.OrHandler.Body)
			g.currentErrVar = savedErrVar
		} else {
			g.writeln("%s := fmt.Errorf(%q)", errName, "null unwrap (as "+ta.TypeName+")")
			g.emitErrReturn(errName)
		}
		g.indent--
		g.writeln("}")
		return
	}

	// Wrap operand in any() so type assertion compiles regardless of the
	// operand's declared type. Bare `concrete.(T)` is rejected by Go when
	// the concrete type isn't an interface, which broke `as pkg.Type`
	// against any concrete-typed local (e.g. `r := strings.NewReader(...)`).
	g.writeln("%s, %s := any(%s).(%s)", v.Name, okName, operand, goType)
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

	// Detect void-error functions (Go-stdlib `json.Unmarshal` AND Zinc
	// void throwers like `void check(...)` that auto-widened to a single
	// `error` return) so we generate `_err := call()` instead of
	// `_, _err := call()` for them.
	errorOnly := g.callIsVoidThrower(value)

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
	if handler.Body != nil {
		g.emitOrBlock(handler.Body)
	}
	g.indent--
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
	if !g.isUserScopeShadowIdent(ident) {
		if pkgPath, ok := g.importMap[ident.Name]; ok {
			return g.goResolver.ReturnsErrorOnly(pkgPath, sel.Field)
		}
	}

	// Case 2: Method call on a variable — obj.Method()
	// Side-map first (Phase 3.7.2): bind+typecheck supplied a GoType
	// for this Ident's binding when it came from an FFI call slot.
	if g.bound != nil {
		if t, ok := g.bound.NodeTypes[ident]; ok && t.GoType != nil {
			return g.goResolver.MethodReturnsErrorOnly(t.GoType, sel.Field)
		}
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
// The body is emitted with currentFuncIsThrower = false so unhandled
// errors lower to `panic(err)` directly at the error site (see
// emitErrReturn) rather than `return err`, which would be invalid Go
// inside the enclosing `go func()` shell that has no return type.
// Save/restore covers the case where the enclosing function is itself
// a thrower.
func (g *Generator) emitGoroutineBody(body *parser.BlockStmt) {
	prevIsThrower := g.currentFuncIsThrower
	g.currentFuncIsThrower = false
	defer func() { g.currentFuncIsThrower = prevIsThrower }()
	g.emitBlock(body)
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
		// `return SomeError(...)` where SomeError is Error itself or
		// any descendant — the function's signature should declare
		// `error` in the tail.
		if g.exprIsErrorCtor(stmt.Value) {
			return true
		}
		if exprContainsAsCast(stmt.Value) {
			return true
		}
		// `return thrower()` — direct return of a thrower call.
		return g.callReturnsError(stmt.Value) || g.exprContainsNestedThrowerCall(stmt.Value)
	case *parser.VarStmt:
		if g.orHandlerCanReturnError(stmt.OrHandler) {
			return true
		}
		// A non-throwing `or { ... }` handler consumes the call's error,
		// so a thrower call on the RHS doesn't make the surrounding body
		// fail — same rule for `as` casts.
		if stmt.OrHandler != nil {
			return false
		}
		return g.callReturnsError(stmt.Value) || exprContainsAsCast(stmt.Value) || g.exprContainsNestedThrowerCall(stmt.Value)
	case *parser.AssignStmt:
		if g.orHandlerCanReturnError(stmt.OrHandler) {
			return true
		}
		if stmt.OrHandler != nil {
			return false
		}
		return g.callReturnsError(stmt.Value) || exprContainsAsCast(stmt.Value) || g.exprContainsNestedThrowerCall(stmt.Value)
	case *parser.ExprStmt:
		if g.orHandlerCanReturnError(stmt.OrHandler) {
			return true
		}
		if stmt.OrHandler != nil {
			return false
		}
		return g.callReturnsError(stmt.Expr) || exprContainsAsCast(stmt.Expr) || g.exprContainsNestedThrowerCall(stmt.Expr)
	case *parser.TupleVarStmt:
		return g.callReturnsError(stmt.Value) || exprContainsAsCast(stmt.Value) || g.exprContainsNestedThrowerCall(stmt.Value)
	case *parser.IfStmt:
		// Condition may itself be (or contain) a thrower call — that
		// hoists at emit time and widens the enclosing function.
		if g.condCanReturnError(stmt.Cond) {
			return true
		}
		if g.blockCanReturnError(stmt.Then) {
			return true
		}
		if stmt.ElseStmt != nil {
			return g.stmtCanReturnError(stmt.ElseStmt)
		}
	case *parser.BlockStmt:
		return g.blockCanReturnError(stmt)
	case *parser.ForStmt:
		if g.condCanReturnError(stmt.Cond) {
			return true
		}
		return g.blockCanReturnError(stmt.Body)
	case *parser.WhileStmt:
		if g.condCanReturnError(stmt.Cond) {
			return true
		}
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
// exprIsErrorVar reports whether `e` is the in-scope error variable
// `err` made visible inside an `or { }` block. Used by emitReturnStmt
// in declared-thrower context: `or { return err }` is the canonical
// propagation form, and `return err` from a multi-value-thrower caller
// must zero-fill the value slots so the resulting Go is well-typed.
func (g *Generator) exprIsErrorVar(e parser.Expr) bool {
	if g.currentErrVar == "" {
		return false
	}
	id, ok := e.(*parser.Ident)
	return ok && id.Name == "err"
}

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
//
// Note: even when the last statement is an exhaustive sealed match where
// every arm returns, we still emit the synthetic trailing return. Go's
// compiler can't prove sealed-type exhaustiveness (Shape is just an
// interface from Go's POV), so it requires either a default arm or a
// trailing return for "all paths return." Since codegen drops the
// synthetic default for exhaustive matches (see emitTypeSwitchMatch),
// the trailing return is the load-bearing fallback.
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
	// Track Go return-slot types so a later method call on a tuple-bound
	// var (e.g. `var dec, derr = hambaOcf.NewDecoder(...); dec.Decode(...)`)
	// is recognized as an FFI seam — required for the explicit-`&`
	// validator and for callReturnsPointer-style introspection.
	if call, ok := t.Value.(*parser.CallExpr); ok {
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
			// Phase 3.7.2: tuple-var FFI-type tracking now flows through
			// the bind side-map (NodeTypes[ident].GoType set by tupleSlotTypes).
			_ = sel
		}
	}
	if t.OrHandler != nil {
		// Multi-value thrower destructure with `or { }`: the call returns
		// (v1, ..., vN, error); destructure into the user's N names plus
		// an _err slot, then run the handler if non-nil.
		errVar := "_err"
		if g.errVarCount > 0 {
			errVar = fmt.Sprintf("_err%d", g.errVarCount)
		}
		g.errVarCount++
		savedErrVar := g.currentErrVar
		g.currentErrVar = errVar

		callExpr := g.formatExpr(t.Value)
		names := append([]string{}, t.Names...)
		names = append(names, errVar)
		g.writeln("%s := %s", strings.Join(names, ", "), callExpr)
		g.writeln("if %s != nil {", errVar)
		g.indent++
		if t.OrHandler.Body != nil {
			g.emitOrBlock(t.OrHandler.Body)
		}
		g.indent--
		g.writeln("}")
		g.currentErrVar = savedErrVar
		for _, n := range t.Names {
			g.declareLocal(n)
		}
		return
	}
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
