// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package typechecker

// AST walker for the bind phase. Each method recursively descends into
// children, manages scope stack push/pop where the AST introduces a new
// lexical scope, and binds every encountered `*parser.Ident` to a
// `Symbol` via `b.bindings[ident] = sym`.
//
// Phase 3.3 scope: handle every AST node kind that can appear in the e2e
// suite. Coverage gaps surfacing during Phase 3.4 codegen migration get
// patched here as encountered.

import "zinc-go/internal/parser"

// --- Declaration walk ------------------------------------------------------

func (b *binder) bindDecl(d parser.TopLevelDecl) {
	switch decl := d.(type) {
	case *parser.FnDecl:
		b.bindFnDecl(decl)
	case *parser.ClassDecl:
		b.bindClassDecl(decl)
	case *parser.DataClassDecl:
		b.bindDataClassDecl(decl)
	case *parser.InterfaceDecl:
		// Interfaces declare method signatures only; no executable body to walk.
		// Method param/return types are TypeExprs, not Idents to bind.
	case *parser.EnumDecl:
		// Enum has no executable body.
	case *parser.ConstDecl:
		if decl.Value != nil {
			b.bindExpr(decl.Value)
		}
	case *parser.TypeAliasDecl:
		// Type alias body is a TypeExpr, not bound here.
	case *parser.TestDecl:
		b.push()
		// `t` is implicitly bound inside test bodies (the *testing.T receiver).
		b.declare("t", Symbol{Kind: SymLocal, Name: "t"})
		if decl.Body != nil {
			b.bindBlock(decl.Body)
		}
		b.pop()
	}
}

func (b *binder) bindFnDecl(decl *parser.FnDecl) {
	prevThrower := b.currentFnIsThrower
	b.currentFnIsThrower = returnDeclaresError(decl.ReturnType)
	defer func() { b.currentFnIsThrower = prevThrower }()
	b.push()
	for _, p := range decl.Params {
		b.declare(p.Name, Symbol{Kind: SymParam, Name: p.Name, DeclType: p.Type})
		if p.Default != nil {
			b.bindExpr(p.Default)
		}
	}
	if decl.Body != nil {
		b.bindBlock(decl.Body)
	}
	b.pop()
}

// returnDeclaresError mirrors codegen's returnTypeDeclaresError —
// a function is a thrower iff its declared return type is a bare
// `error` or a TupleType whose last element is `error`. Local copy
// here so the typechecker binder doesn't reach into codegen_go.
func returnDeclaresError(retType parser.TypeExpr) bool {
	if retType == nil {
		return false
	}
	if isErrorType(retType) {
		return true
	}
	if tup, ok := retType.(*parser.TupleType); ok && len(tup.Elements) > 0 {
		return isErrorType(tup.Elements[len(tup.Elements)-1])
	}
	return false
}

func isErrorType(t parser.TypeExpr) bool {
	if st, ok := t.(*parser.SimpleType); ok {
		return st.Name == "error"
	}
	return false
}

func (b *binder) bindClassDecl(decl *parser.ClassDecl) {
	prev := b.pushClassContext(decl.Name)
	for _, f := range decl.Fields {
		b.currentClassFields[f.Name] = true
		b.currentClassMemberPub[f.Name] = f.IsPub
	}
	for _, m := range decl.Methods {
		b.currentClassMethods[m.Name] = true
		b.currentClassMemberPub[m.Name] = m.IsPub
	}
	// Walk parent chain — Sigs.ParentTypes drives inheritance lookup
	// so a bare ident inside a subclass method resolves to the
	// inherited field/method's Symbol. nil-safe: pre-Sigs callers
	// just lose inheritance resolution at bind time (codegen's
	// existing currentFields fallback covers them as before).
	b.populateInheritedMembers(decl.Name)

	for _, f := range decl.Fields {
		if f.Default != nil {
			b.bindExpr(f.Default)
		}
	}
	if decl.Ctor != nil {
		b.bindCtor(decl.Ctor)
	}
	for _, c := range decl.Ctors {
		b.bindCtor(c)
	}
	for _, m := range decl.Methods {
		b.bindMethodDecl(m)
	}
	if decl.IsSealed {
		for _, v := range decl.Variants {
			b.bindDataClassDecl(v)
		}
	}

	b.popClassContext(prev)
}

func (b *binder) bindCtor(c *parser.CtorDecl) {
	// Ctors are non-throwers in zinc's current AST — the parser doesn't
	// carry a Throws flag. If a ctor body uses catch with a bare
	// return, it lands in the non-thrower branch (no propagation
	// contract; bare return just stops construction). The downstream
	// codegen-side throwing-ctor inference happens via body walk and
	// runs after bind, so the bare-return check here would race it.
	prevThrower := b.currentFnIsThrower
	b.currentFnIsThrower = false
	defer func() { b.currentFnIsThrower = prevThrower }()
	b.push()
	for _, p := range c.Params {
		b.declare(p.Name, Symbol{Kind: SymParam, Name: p.Name, DeclType: p.Type})
		if p.Default != nil {
			b.bindExpr(p.Default)
		}
	}
	for _, a := range c.SuperArgs {
		b.bindExpr(a)
	}
	if c.Body != nil {
		b.bindBlock(c.Body)
	}
	b.pop()
}

func (b *binder) bindMethodDecl(m *parser.MethodDecl) {
	prevThrower := b.currentFnIsThrower
	b.currentFnIsThrower = returnDeclaresError(m.ReturnType)
	defer func() { b.currentFnIsThrower = prevThrower }()
	b.push()
	for _, p := range m.Params {
		b.declare(p.Name, Symbol{Kind: SymParam, Name: p.Name, DeclType: p.Type})
		if p.Default != nil {
			b.bindExpr(p.Default)
		}
	}
	if m.Body != nil {
		b.bindBlock(m.Body)
	}
	b.pop()
}

func (b *binder) bindDataClassDecl(decl *parser.DataClassDecl) {
	prev := b.pushClassContext(decl.Name)
	for _, f := range decl.Params {
		b.currentClassFields[f.Name] = true
		b.currentClassMemberPub[f.Name] = f.IsPub
	}
	for _, m := range decl.Methods {
		b.currentClassMethods[m.Name] = true
		b.currentClassMemberPub[m.Name] = m.IsPub
	}
	b.populateInheritedMembers(decl.Name)

	for _, f := range decl.Params {
		if f.Default != nil {
			b.bindExpr(f.Default)
		}
	}
	for _, m := range decl.Methods {
		b.bindMethodDecl(m)
	}
	b.popClassContext(prev)
}

// classCtxFrame captures the per-class binder fields so nested class
// declarations restore correctly on exit. push/popClassContext are
// the entry/exit pair used by both bindClassDecl and bindDataClassDecl.
type classCtxFrame struct {
	className string
	fields    map[string]bool
	methods   map[string]bool
	memberPub map[string]bool
}

func (b *binder) pushClassContext(name string) classCtxFrame {
	prev := classCtxFrame{
		className: b.currentClass,
		fields:    b.currentClassFields,
		methods:   b.currentClassMethods,
		memberPub: b.currentClassMemberPub,
	}
	b.currentClass = name
	b.currentClassFields = make(map[string]bool)
	b.currentClassMethods = make(map[string]bool)
	b.currentClassMemberPub = make(map[string]bool)
	return prev
}

func (b *binder) popClassContext(prev classCtxFrame) {
	b.currentClass = prev.className
	b.currentClassFields = prev.fields
	b.currentClassMethods = prev.methods
	b.currentClassMemberPub = prev.memberPub
}

// populateInheritedMembers walks the parent chain via Sigs.ParentTypes
// and adds each ancestor's fields + methods to the binder's flat
// member maps. Fields are looked up in Sigs.ClassFields, methods in
// Sigs.MethodSigs, and per-member pub status in Sigs.MemberIsPub.
//
// Cycles are broken by a `seen` set so a malformed inheritance graph
// can't loop the binder forever (the typechecker reports the cycle
// separately; bind shouldn't crash).
func (b *binder) populateInheritedMembers(className string) {
	if b.ctx.Sigs == nil {
		return
	}
	seen := map[string]bool{className: true}
	queue := append([]string{}, b.ctx.Sigs.ParentTypes[className]...)
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		if seen[parent] {
			continue
		}
		seen[parent] = true
		for fName := range b.ctx.Sigs.ClassFields[parent] {
			if _, already := b.currentClassFields[fName]; already {
				continue // local override wins for shadowing
			}
			b.currentClassFields[fName] = true
			b.currentClassMemberPub[fName] = b.ctx.Sigs.MemberIsPub[parent+"."+fName]
		}
		for mName := range b.ctx.Sigs.MethodSigs[parent] {
			if _, already := b.currentClassMethods[mName]; already {
				continue
			}
			b.currentClassMethods[mName] = true
			b.currentClassMemberPub[mName] = b.ctx.Sigs.MemberIsPub[parent+"."+mName]
		}
		queue = append(queue, b.ctx.Sigs.ParentTypes[parent]...)
	}
}

// --- Statement walk --------------------------------------------------------

func (b *binder) bindBlock(block *parser.BlockStmt) {
	if block == nil {
		return
	}
	b.push()
	for _, s := range block.Stmts {
		b.bindStmt(s)
	}
	b.pop()
}

func (b *binder) bindStmt(s parser.Stmt) {
	switch stmt := s.(type) {
	case *parser.BlockStmt:
		b.bindBlock(stmt)
	case *parser.VarStmt:
		b.bindVarStmt(stmt)
	case *parser.TupleVarStmt:
		b.bindTupleVarStmt(stmt)
	case *parser.AssignStmt:
		b.bindAssignStmt(stmt)
	case *parser.ReturnStmt:
		if stmt.Value != nil {
			b.bindExpr(stmt.Value)
		}
	case *parser.IfStmt:
		b.bindExpr(stmt.Cond)
		b.bindBlock(stmt.Then)
		if stmt.ElseStmt != nil {
			b.bindStmt(stmt.ElseStmt)
		}
	case *parser.ForStmt:
		b.bindForStmt(stmt)
	case *parser.WhileStmt:
		b.bindExpr(stmt.Cond)
		b.bindBlock(stmt.Body)
	case *parser.MatchStmt:
		b.bindMatchStmt(stmt)
	case *parser.SelectStmt:
		b.bindSelectStmt(stmt)
	case *parser.GoStmt:
		b.bindBlock(stmt.Body)
	case *parser.ParallelForStmt:
		b.bindParallelForStmt(stmt)
	case *parser.WithStmt:
		b.bindWithStmt(stmt)
	case *parser.TimeoutStmt:
		b.bindExpr(stmt.Duration)
		b.bindBlock(stmt.Body)
		if stmt.OrHandler != nil {
			b.bindOrHandler(stmt.OrHandler)
		}
	case *parser.DeferStmt:
		b.bindExpr(stmt.Expr)
	case *parser.AssertStmt:
		b.bindExpr(stmt.Cond)
		if stmt.Message != nil {
			b.bindExpr(stmt.Message)
		}
	case *parser.PrintStmt:
		b.bindExpr(stmt.Value)
	case *parser.ExprStmt:
		b.bindExpr(stmt.Expr)
		if stmt.OrHandler != nil {
			b.bindOrHandler(stmt.OrHandler)
		}
	case *parser.FnDecl:
		b.bindFnDecl(stmt)
	case *parser.BreakStmt, *parser.ContinueStmt:
		// No children to bind.
	}
}

func (b *binder) bindVarStmt(s *parser.VarStmt) {
	if s.Value != nil {
		b.bindExpr(s.Value)
	}
	if s.OrHandler != nil {
		b.bindOrHandler(s.OrHandler)
	}
	b.declare(s.Name, Symbol{Kind: SymLocal, Name: s.Name, DeclLine: s.Line, DeclType: s.Type})
}

func (b *binder) bindTupleVarStmt(s *parser.TupleVarStmt) {
	if s.Value != nil {
		b.bindExpr(s.Value)
	}
	if s.OrHandler != nil {
		b.bindOrHandler(s.OrHandler)
	}
	for _, name := range s.Names {
		b.declare(name, Symbol{Kind: SymLocal, Name: name, DeclLine: s.Line})
	}
}

func (b *binder) bindAssignStmt(s *parser.AssignStmt) {
	b.bindExpr(s.Target)
	b.bindExpr(s.Value)
	if s.OrHandler != nil {
		b.bindOrHandler(s.OrHandler)
	}
}

func (b *binder) bindForStmt(s *parser.ForStmt) {
	b.push()
	if s.IsRange {
		if s.Range != nil {
			b.bindExpr(s.Range)
		}
		b.declare(s.Item, Symbol{Kind: SymLocal, Name: s.Item, DeclLine: s.Line})
		if s.IndexVar != "" {
			b.declare(s.IndexVar, Symbol{Kind: SymLocal, Name: s.IndexVar, DeclLine: s.Line})
		}
	} else {
		if s.Init != nil {
			b.bindStmt(s.Init)
		}
		if s.Cond != nil {
			b.bindExpr(s.Cond)
		}
		if s.Post != nil {
			b.bindStmt(s.Post)
		}
	}
	b.bindBlock(s.Body)
	b.pop()
}

func (b *binder) bindMatchStmt(s *parser.MatchStmt) {
	b.bindExpr(s.Subject)
	for _, c := range s.Cases {
		b.push()
		if c.Pattern != nil {
			// Pattern can introduce bindings (sealed-variant destructure).
			b.bindMatchPattern(c.Pattern)
		}
		b.bindBlock(c.Body)
		b.pop()
	}
}

// bindMatchPattern walks a match pattern. Identifiers in destructure
// positions (e.g. `Ok(value)` — `value` is a binding, not a use) are
// declared as locals. Bare identifiers that match an enum variant or
// sealed variant are uses and get resolved.
func (b *binder) bindMatchPattern(p parser.Expr) {
	switch pat := p.(type) {
	case *parser.Ident:
		// Could be a binding (rare bare-ident pattern) or a use of an
		// enum variant. Resolve through the resolver: if it matches a
		// known variant, treat as use; else treat as binding.
		sym := b.resolve(pat.Name, 0)
		if sym.Kind == SymEnumVariant || sym.Kind == SymSealedVariant {
			b.bindings[pat] = sym
		} else {
			// Treat as binding — variable name introduced by the pattern.
			b.declare(pat.Name, Symbol{Kind: SymLocal, Name: pat.Name})
			b.bindings[pat] = Symbol{Kind: SymLocal, Name: pat.Name}
		}
	case *parser.CallExpr:
		// Sealed-variant destructure: VariantName(arg1, arg2, ...).
		// Callee is the variant ident; args are bindings or wildcards.
		if id, ok := pat.Callee.(*parser.Ident); ok {
			sym := b.resolve(id.Name, 0)
			b.bindings[id] = sym
		}
		for _, a := range pat.Args {
			if id, ok := a.(*parser.Ident); ok && id.Name != "_" {
				b.declare(id.Name, Symbol{Kind: SymLocal, Name: id.Name})
				b.bindings[id] = Symbol{Kind: SymLocal, Name: id.Name}
			}
		}
	default:
		b.bindExpr(p)
	}
}

func (b *binder) bindSelectStmt(s *parser.SelectStmt) {
	for _, c := range s.Cases {
		b.push()
		if c.Channel != nil {
			b.bindExpr(c.Channel)
		}
		if c.SendValue != nil {
			b.bindExpr(c.SendValue)
		}
		if c.Binding != "" {
			b.declare(c.Binding, Symbol{Kind: SymLocal, Name: c.Binding})
		}
		if c.Body != nil {
			b.bindBlock(c.Body)
		}
		b.pop()
	}
	if s.Default != nil {
		b.bindBlock(s.Default)
	}
}

func (b *binder) bindParallelForStmt(s *parser.ParallelForStmt) {
	b.push()
	if s.Range != nil {
		b.bindExpr(s.Range)
	}
	b.declare(s.Item, Symbol{Kind: SymLocal, Name: s.Item, DeclLine: s.Line})
	if s.IndexVar != "" {
		b.declare(s.IndexVar, Symbol{Kind: SymLocal, Name: s.IndexVar, DeclLine: s.Line})
	}
	b.bindBlock(s.Body)
	if s.OrHandler != nil {
		b.bindOrHandler(s.OrHandler)
	}
	b.pop()
}

func (b *binder) bindWithStmt(s *parser.WithStmt) {
	b.push()
	for _, r := range s.Resources {
		if r.Value != nil {
			b.bindExpr(r.Value)
		}
		if r.OrHandler != nil {
			b.bindOrHandler(r.OrHandler)
		}
		b.declare(r.Name, Symbol{Kind: SymLocal, Name: r.Name})
	}
	b.bindBlock(s.Body)
	b.pop()
}

func (b *binder) bindOrHandler(h *parser.OrHandler) {
	if h == nil || h.Body == nil {
		return
	}
	b.push()
	// `err` is implicitly available inside a catch-block.
	b.declare("err", Symbol{Kind: SymLocal, Name: "err"})
	for _, s := range h.Body.Stmts {
		b.bindStmt(s)
		// In a thrower, bare `return` inside catch silently swallows
		// the error (`return zero, nil`) — force `return err` (or
		// `return v..., err` for a multi-slot return) so propagation
		// is explicit. In a non-thrower (most importantly void
		// functions like HTTP handlers), bare return is the natural
		// "log the error and bail" pattern: there's no error slot in
		// the signature to propagate to, and `return err` would be a
		// type mismatch. Allow it there.
		if b.currentFnIsThrower {
			checkBareReturnInCatch(s, &b.errors)
		}
	}
	b.pop()
}

// checkBareReturnInCatch walks a statement looking for ReturnStmts
// with nil Value (bare `return`). Recurses through nested if / for /
// while / block / with constructs, but stops at lambda or fn
// boundaries — bare `return` inside a lambda body is the lambda's
// return, not the catch's, and that's fine.
func checkBareReturnInCatch(s parser.Stmt, errors *[]V2Error) {
	switch n := s.(type) {
	case *parser.ReturnStmt:
		if n.Value == nil {
			*errors = append(*errors, V2Error{
				Line: n.Line,
				Message: "bare `return` inside `catch { }` is not allowed — " +
					"use `return err` to propagate, `return v..., err` for an explicit " +
					"multi-slot return, or a fallback expression " +
					"(`catch { 0 }`). bare return would silently swallow the error.",
			})
		}
	case *parser.IfStmt:
		if n.Then != nil {
			for _, sub := range n.Then.Stmts {
				checkBareReturnInCatch(sub, errors)
			}
		}
		if n.ElseStmt != nil {
			checkBareReturnInCatch(n.ElseStmt, errors)
		}
	case *parser.ForStmt:
		if n.Body != nil {
			for _, sub := range n.Body.Stmts {
				checkBareReturnInCatch(sub, errors)
			}
		}
	case *parser.WhileStmt:
		if n.Body != nil {
			for _, sub := range n.Body.Stmts {
				checkBareReturnInCatch(sub, errors)
			}
		}
	case *parser.BlockStmt:
		for _, sub := range n.Stmts {
			checkBareReturnInCatch(sub, errors)
		}
	case *parser.WithStmt:
		if n.Body != nil {
			for _, sub := range n.Body.Stmts {
				checkBareReturnInCatch(sub, errors)
			}
		}
	case *parser.MatchStmt:
		for _, c := range n.Cases {
			if c.Body != nil {
				for _, sub := range c.Body.Stmts {
					checkBareReturnInCatch(sub, errors)
				}
			}
		}
	}
}

// --- Expression walk -------------------------------------------------------

func (b *binder) bindExpr(e parser.Expr) {
	if e == nil {
		return
	}
	switch expr := e.(type) {
	case *parser.Ident:
		b.bindings[expr] = b.resolve(expr.Name, 0)
	case *parser.BinaryExpr:
		b.bindExpr(expr.Left)
		b.bindExpr(expr.Right)
	case *parser.UnaryExpr:
		b.bindExpr(expr.Operand)
	case *parser.CallExpr:
		b.bindExpr(expr.Callee)
		for _, a := range expr.Args {
			b.bindExpr(a)
		}
		for _, na := range expr.NamedArgs {
			b.bindExpr(na.Value)
		}
	case *parser.SelectorExpr:
		// `obj.field` — bind the receiver only. The field name is
		// resolved against the receiver's type, which is a typecheck
		// concern (Phase 3.5+), not a name-resolution concern here.
		b.bindExpr(expr.Object)
	case *parser.SafeNavExpr:
		b.bindExpr(expr.Object)
		if expr.Call != nil {
			for _, a := range expr.Call.Args {
				b.bindExpr(a)
			}
			for _, na := range expr.Call.NamedArgs {
				b.bindExpr(na.Value)
			}
		}
	case *parser.IndexExpr:
		b.bindExpr(expr.Object)
		b.bindExpr(expr.Index)
	case *parser.SliceExpr:
		b.bindExpr(expr.Object)
		if expr.Low != nil {
			b.bindExpr(expr.Low)
		}
		if expr.High != nil {
			b.bindExpr(expr.High)
		}
	case *parser.ThisExpr:
		// `this` is implicit in the current class context. No Ident to bind.
	case *parser.SuperCallExpr:
		for _, a := range expr.Args {
			b.bindExpr(a)
		}
	case *parser.ListLit:
		for _, el := range expr.Elements {
			b.bindExpr(el)
		}
	case *parser.MapLit:
		for i := range expr.Keys {
			b.bindExpr(expr.Keys[i])
			b.bindExpr(expr.Values[i])
		}
	case *parser.TupleLit:
		for _, el := range expr.Elements {
			b.bindExpr(el)
		}
	case *parser.SpreadExpr:
		b.bindExpr(expr.Expr)
	case *parser.LambdaExpr:
		b.push()
		for _, p := range expr.Params {
			b.declare(p.Name, Symbol{Kind: SymParam, Name: p.Name, DeclType: p.Type})
			if p.Default != nil {
				b.bindExpr(p.Default)
			}
		}
		if expr.Body != nil {
			for _, s := range expr.Body.Stmts {
				b.bindStmt(s)
			}
		}
		if expr.Expr != nil {
			b.bindExpr(expr.Expr)
		}
		b.pop()
	case *parser.SpawnExpr:
		if expr.Body != nil {
			b.bindBlock(expr.Body)
		}
		if expr.OrHandler != nil {
			b.bindOrHandler(expr.OrHandler)
		}
	case *parser.IfExpr:
		b.bindExpr(expr.Cond)
		b.bindExpr(expr.Then)
		b.bindExpr(expr.Else)
	case *parser.MatchExpr:
		b.bindExpr(expr.Subject)
		for _, c := range expr.Cases {
			b.push()
			if c.Pattern != nil {
				b.bindMatchPattern(c.Pattern)
			}
			b.bindExpr(c.Value)
			b.pop()
		}
	case *parser.RangeExpr:
		if expr.Start != nil {
			b.bindExpr(expr.Start)
		}
		if expr.End != nil {
			b.bindExpr(expr.End)
		}
	case *parser.TypeAssertExpr:
		b.bindExpr(expr.Object)
		// Expression-attached or-handler (parsed onto the as-cast at line
		// position): its body is real Zinc code and needs to be walked
		// so identifier references inside resolve through Bindings —
		// otherwise calls inside `expr as T or { return foo() }` lose
		// pub-function exportName resolution and emit lowercase.
		if expr.OrHandler != nil {
			b.bindOrHandler(expr.OrHandler)
		}
	case *parser.CapacityExpr:
		b.bindExpr(expr.Capacity)
	case *parser.SizedArrayExpr:
		b.bindExpr(expr.Size)
	}
	// Literals (int / float / string / bool / null / raw / interp) carry
	// no Idents to bind. StringInterpLit's parts are handled via formatExpr
	// at codegen time; bind sees them as opaque for now. Phase 3.4 may add
	// per-part recursion when migrating string-interp codegen.
}
