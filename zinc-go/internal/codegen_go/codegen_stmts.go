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
	case *parser.ConcurrentStmt:
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
	case *parser.ConcurrentStmt:
		g.emitConcurrentStmt(stmt)
	case *parser.WithStmt:
		g.emitWithStmt(stmt)
	case *parser.DeferStmt:
		g.writeln("defer %s", g.formatExpr(stmt.Expr))
	case *parser.AssertStmt:
		g.emitAssertStmt(stmt)
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
	// Explicit error propagation: `var x = call()?`.
	// Strip the PropagateExpr wrapper and fall through to the same
	// propagation codegen the implicit path uses.
	if prop, ok := v.Value.(*parser.PropagateExpr); ok {
		stripped := *v
		stripped.Value = prop.Inner
		g.emitErrorPropagatingVar(&stripped)
		return
	}
	// Nested propagation: `var x = foo(bar()?) + 1`. Hoist each `?` to a
	// temp above this statement, then emit the var with the rewritten RHS.
	if v.Value != nil && exprContainsPropagate(v.Value) {
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
	}
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
	// Nested propagation on RHS: `x = foo(bar()?) + 1`. Hoist, then emit.
	if a.Value != nil && exprContainsPropagate(a.Value) {
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
	g.writeln("%s %s %s", g.formatExpr(a.Target), a.Op, g.formatExpr(a.Value))
}

func (g *Generator) emitReturnStmt(r *parser.ReturnStmt) {
	// Explicit error propagation: any `?` in the returned expression.
	// hoistPropagates emits `_pN, errM := inner; if errM != nil { return zero, errM }`
	// for each `?`, then returns a rewritten value expression that
	// references the temps. Top-level `return call()?` and nested
	// `return call()? * 3` both go through this path.
	if r.Value != nil && exprContainsPropagate(r.Value) {
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
	// Nested `?` inside an expression statement: e.g. `foo(bar()?)` as a
	// bare statement. Hoist the `?`s, then emit the rewritten statement.
	if exprContainsPropagate(es.Expr) {
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
		return &parser.TypeAssertExpr{Object: g.hoistPropagates(expr.Object), TypeName: expr.TypeName, IsCheck: expr.IsCheck}
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
		return exprContainsPropagate(stmt.Value)
	case *parser.VarStmt:
		if exprContainsPropagate(stmt.Value) {
			return true
		}
		if g.orHandlerCanReturnError(stmt.OrHandler) {
			return true
		}
		return g.callReturnsError(stmt.Value)
	case *parser.AssignStmt:
		if exprContainsPropagate(stmt.Value) || exprContainsPropagate(stmt.Target) {
			return true
		}
		if g.orHandlerCanReturnError(stmt.OrHandler) {
			return true
		}
		return g.callReturnsError(stmt.Value)
	case *parser.ExprStmt:
		if exprContainsPropagate(stmt.Expr) {
			return true
		}
		if g.orHandlerCanReturnError(stmt.OrHandler) {
			return true
		}
		return g.callReturnsError(stmt.Expr)
	case *parser.TupleVarStmt:
		return g.callReturnsError(stmt.Value)
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

func (g *Generator) emitConcurrentStmt(c *parser.ConcurrentStmt) {
	g.needImport("sync")
	g.writeln("var _wg sync.WaitGroup")
	for _, task := range c.Tasks {
		g.writeln("_wg.Add(1)")
		g.writeln("go func() {")
		g.indent++
		g.writeln("defer _wg.Done()")
		// If the task is a call to a throwing function, panic on
		// uncaught error — same contract as spawn/go bodies.
		if g.callReturnsError(task) {
			if g.callIsVoidThrower(task) {
				g.writeln("if _gerr := %s; _gerr != nil { panic(_gerr) }", g.formatExpr(task))
			} else {
				g.writeln("if _, _gerr := %s; _gerr != nil { panic(_gerr) }", g.formatExpr(task))
			}
		} else {
			g.writeln("%s", g.formatExpr(task))
		}
		g.indent--
		g.writeln("}()")
	}
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
			for _, r := range w.Resources {
				g.writeln("%s := %s", r.Name, g.formatExpr(r.Value))
				g.writeln("defer %s.Close()", r.Name)
			}
			g.emitBlock(w.Body)
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
		for _, r := range w.Resources {
			g.writeln("%s := %s", r.Name, g.formatExpr(r.Value))
			g.writeln("defer %s.Close()", r.Name)
		}
		g.emitBlock(w.Body)
		g.indent--
		g.writeln("}()")
		return
	}
	for _, r := range w.Resources {
		g.writeln("%s := %s", r.Name, g.formatExpr(r.Value))
		g.writeln("defer %s.Close()", r.Name)
	}
	g.emitBlock(w.Body)
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
