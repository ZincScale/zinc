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

import (
	"strings"
	"testing"

	"zinc-go/internal/lexer"
	"zinc-go/internal/parser"
)

func parse(t *testing.T, src string) *parser.Program {
	t.Helper()
	l := lexer.New(src)
	toks := l.Tokenize()
	if len(l.Errors) > 0 {
		t.Fatalf("lex errors: %v", l.Errors)
	}
	p := parser.New(toks)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		t.Fatalf("parse errors: %v", p.Errors)
	}
	return prog
}

// findIdent returns the first Ident with the given name in `prog`. Walks
// declarations and statements top-down. Used by tests to locate the AST
// node a binding should be checked at.
func findIdent(prog *parser.Program, name string) *parser.Ident {
	var found *parser.Ident
	var walkExpr func(parser.Expr)
	var walkStmt func(parser.Stmt)
	walkExpr = func(e parser.Expr) {
		if found != nil || e == nil {
			return
		}
		switch n := e.(type) {
		case *parser.Ident:
			if n.Name == name {
				found = n
			}
		case *parser.BinaryExpr:
			walkExpr(n.Left)
			walkExpr(n.Right)
		case *parser.UnaryExpr:
			walkExpr(n.Operand)
		case *parser.CallExpr:
			walkExpr(n.Callee)
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *parser.SelectorExpr:
			walkExpr(n.Object)
		case *parser.IndexExpr:
			walkExpr(n.Object)
			walkExpr(n.Index)
		case *parser.ListLit:
			for _, el := range n.Elements {
				walkExpr(el)
			}
		case *parser.MapLit:
			for i := range n.Keys {
				walkExpr(n.Keys[i])
				walkExpr(n.Values[i])
			}
		case *parser.LambdaExpr:
			if n.Body != nil {
				for _, s := range n.Body.Stmts {
					walkStmt(s)
				}
			}
			walkExpr(n.Expr)
		}
	}
	walkStmt = func(s parser.Stmt) {
		if found != nil || s == nil {
			return
		}
		switch n := s.(type) {
		case *parser.BlockStmt:
			for _, x := range n.Stmts {
				walkStmt(x)
			}
		case *parser.VarStmt:
			walkExpr(n.Value)
		case *parser.AssignStmt:
			walkExpr(n.Target)
			walkExpr(n.Value)
		case *parser.ReturnStmt:
			walkExpr(n.Value)
		case *parser.IfStmt:
			walkExpr(n.Cond)
			walkStmt(n.Then)
			walkStmt(n.ElseStmt)
		case *parser.ExprStmt:
			walkExpr(n.Expr)
		case *parser.PrintStmt:
			walkExpr(n.Value)
		case *parser.ForStmt:
			if n.Range != nil {
				walkExpr(n.Range)
			}
			walkStmt(n.Body)
		}
	}
	for _, d := range prog.Decls {
		switch decl := d.(type) {
		case *parser.FnDecl:
			if decl.Body != nil {
				for _, s := range decl.Body.Stmts {
					walkStmt(s)
				}
			}
		case *parser.ConstDecl:
			walkExpr(decl.Value)
		}
	}
	for _, s := range prog.Stmts {
		walkStmt(s)
	}
	return found
}

// TestBindLocalVar — a bare ident referring to a local var binds to SymLocal.
func TestBindLocalVar(t *testing.T) {
	prog := parse(t, `
void main() {
    var x = 5
    print(x)
}
`)
	bp, errs := Bind(prog, &BindContext{})
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	id := findIdent(prog, "x")
	if id == nil {
		t.Fatal("could not locate `x` use site")
	}
	sym, ok := bp.Bindings[id]
	if !ok {
		t.Fatalf("`x` not in bindings")
	}
	if sym.Kind != SymLocal {
		t.Errorf("expected SymLocal, got %s", sym.Kind)
	}
}

// TestBindParam — a bare ident referring to a function param binds to SymParam.
func TestBindParam(t *testing.T) {
	prog := parse(t, `
int square(int n) {
    return n * n
}
`)
	bp, errs := Bind(prog, &BindContext{})
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	id := findIdent(prog, "n")
	if id == nil {
		t.Fatal("could not locate `n` use site")
	}
	sym := bp.Bindings[id]
	if sym.Kind != SymParam {
		t.Errorf("expected SymParam, got %s", sym.Kind)
	}
}

// TestBindBuiltin — bare `len` binds to SymBuiltin.
func TestBindBuiltin(t *testing.T) {
	prog := parse(t, `
void main() {
    var xs = [1, 2, 3]
    print(len(xs))
}
`)
	bp, errs := Bind(prog, &BindContext{})
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	id := findIdent(prog, "len")
	if id == nil {
		t.Fatal("could not locate `len` use site")
	}
	sym := bp.Bindings[id]
	if sym.Kind != SymBuiltin {
		t.Errorf("expected SymBuiltin, got %s", sym.Kind)
	}
}

// TestBindShadow — local var shadows a same-package fn of the same name.
// User scope wins per the spec's shadow rule.
func TestBindShadow(t *testing.T) {
	prog := parse(t, `
void main() {
    var helper = 42
    print(helper)
}
`)
	ctx := &BindContext{
		SiblingFns: map[string]bool{"helper": true},
	}
	bp, errs := Bind(prog, ctx)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	id := findIdent(prog, "helper")
	if id == nil {
		t.Fatal("could not locate `helper` use site")
	}
	sym := bp.Bindings[id]
	if sym.Kind != SymLocal {
		t.Errorf("expected SymLocal (user-scope wins), got %s", sym.Kind)
	}
}

// TestBindCollision — bare name exported by two imports → Zinc-level error.
func TestBindCollision(t *testing.T) {
	prog := parse(t, `
import core
import hambaAvro

void main() {
    var s = Schema()
    print(s)
}
`)
	ctx := &BindContext{
		ZincSubpkgExports: map[string]map[string]string{
			"core": {"Schema": "data"},
		},
		GoPkgExports: map[string]map[string]string{
			"hambaAvro": {"Schema": "type"},
		},
		ImportAliases: map[string]bool{"core": true, "hambaAvro": true},
	}
	_, errs := Bind(prog, ctx)
	if len(errs) == 0 {
		t.Fatal("expected collision error, got none")
	}
	msg := errs[0].Message
	if !strings.Contains(msg, "ambiguous bare name") {
		t.Errorf("expected 'ambiguous bare name' in error, got: %s", msg)
	}
	if !strings.Contains(msg, "core") || !strings.Contains(msg, "hambaAvro") {
		t.Errorf("expected both colliding pkgs in message, got: %s", msg)
	}
	if !strings.Contains(msg, "core.Schema") || !strings.Contains(msg, "hambaAvro.Schema") {
		t.Errorf("expected suggestions, got: %s", msg)
	}
}

// TestBindCrossPkgFn — bare name resolves to a single zinc-subpkg export.
func TestBindCrossPkgFn(t *testing.T) {
	prog := parse(t, `
import core

void main() {
    print(makeRecord())
}
`)
	ctx := &BindContext{
		ZincSubpkgExports: map[string]map[string]string{
			"core": {"makeRecord": "func"},
		},
		ImportAliases: map[string]bool{"core": true},
	}
	bp, errs := Bind(prog, ctx)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	id := findIdent(prog, "makeRecord")
	if id == nil {
		t.Fatal("could not locate `makeRecord` use site")
	}
	sym := bp.Bindings[id]
	if sym.Kind != SymFn {
		t.Errorf("expected SymFn, got %s", sym.Kind)
	}
	if sym.Pkg != "core" {
		t.Errorf("expected Pkg=core, got %q", sym.Pkg)
	}
}

// TestBindSamePkgSibling — bare name resolves to a sibling fn in the same package.
func TestBindSamePkgSibling(t *testing.T) {
	prog := parse(t, `
void main() {
    helper()
}
`)
	ctx := &BindContext{
		SiblingFns: map[string]bool{"helper": true},
	}
	bp, errs := Bind(prog, ctx)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	id := findIdent(prog, "helper")
	if id == nil {
		t.Fatal("could not locate `helper` use site")
	}
	sym := bp.Bindings[id]
	if sym.Kind != SymFn {
		t.Errorf("expected SymFn, got %s", sym.Kind)
	}
	if sym.Pkg != "" {
		t.Errorf("expected empty Pkg (same package), got %q", sym.Pkg)
	}
}
