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

func (b *binder) bindClassDecl(decl *parser.ClassDecl) {
	prevClass := b.currentClass
	prevFields := b.currentClassFields
	b.currentClass = decl.Name
	b.currentClassFields = make(map[string]bool)
	for _, f := range decl.Fields {
		b.currentClassFields[f.Name] = true
	}

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

	b.currentClass = prevClass
	b.currentClassFields = prevFields
}

func (b *binder) bindCtor(c *parser.CtorDecl) {
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
	prevClass := b.currentClass
	prevFields := b.currentClassFields
	b.currentClass = decl.Name
	b.currentClassFields = make(map[string]bool)
	for _, f := range decl.Params {
		b.currentClassFields[f.Name] = true
	}
	for _, f := range decl.Params {
		if f.Default != nil {
			b.bindExpr(f.Default)
		}
	}
	for _, m := range decl.Methods {
		b.bindMethodDecl(m)
	}
	b.currentClass = prevClass
	b.currentClassFields = prevFields
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
	// `err` is implicitly available inside an or-block.
	b.declare("err", Symbol{Kind: SymLocal, Name: "err"})
	for _, s := range h.Body.Stmts {
		b.bindStmt(s)
	}
	b.pop()
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
