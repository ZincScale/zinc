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

package parser

import (
	"testing"

	"zinc/internal/lexer"
)

func parse(src string) (*Program, *Parser) {
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := New(tokens)
	prog := p.Parse()
	return prog, p
}

func assertNoErrors(t *testing.T, p *Parser) {
	t.Helper()
	if len(p.Errors) > 0 {
		t.Fatalf("parse errors: %v", p.Errors)
	}
}

func TestParseFnDecl(t *testing.T) {
	prog, p := parse(`main() { }`)
	assertNoErrors(t, p)
	if len(prog.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %d", len(prog.Decls))
	}
	fn, ok := prog.Decls[0].(*FnDecl)
	if !ok {
		t.Fatal("expected FnDecl")
	}
	if fn.Name != "main" {
		t.Errorf("expected name 'main', got %q", fn.Name)
	}
}

func TestParseFnWithParams(t *testing.T) {
	prog, p := parse(`Int add(Int a, Int b) { return a }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	if len(fn.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(fn.Params))
	}
	if fn.Params[0].Name != "a" {
		t.Errorf("expected param name 'a'")
	}
	if fn.ReturnType == nil {
		t.Error("expected return type")
	}
}

func TestParseVarStmt(t *testing.T) {
	prog, p := parse(`main() { x := 42 }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	v, ok := fn.Body.Stmts[0].(*VarStmt)
	if !ok {
		t.Fatal("expected VarStmt")
	}
	if v.Name != "x" {
		t.Errorf("expected var name 'x', got %q", v.Name)
	}
}

func TestParseIfStmt(t *testing.T) {
	prog, p := parse(`main() { if x { } }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	_, ok := fn.Body.Stmts[0].(*IfStmt)
	if !ok {
		t.Fatal("expected IfStmt")
	}
}

func TestParseIfElse(t *testing.T) {
	prog, p := parse(`main() { if x { } else { } }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	stmt := fn.Body.Stmts[0].(*IfStmt)
	if stmt.ElseStmt == nil {
		t.Error("expected else branch")
	}
}

func TestParseWhile(t *testing.T) {
	prog, p := parse(`main() { while true { } }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	_, ok := fn.Body.Stmts[0].(*WhileStmt)
	if !ok {
		t.Fatal("expected WhileStmt")
	}
}

func TestParseForCStyle(t *testing.T) {
	prog, p := parse(`main() { for i := 0; i; i { } }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	f, ok := fn.Body.Stmts[0].(*ForStmt)
	if !ok {
		t.Fatal("expected ForStmt")
	}
	if f.IsRange {
		t.Error("expected C-style for, not range")
	}
}

func TestParseForIn(t *testing.T) {
	prog, p := parse(`main() { for item in items { } }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	f, ok := fn.Body.Stmts[0].(*ForStmt)
	if !ok {
		t.Fatal("expected ForStmt")
	}
	if !f.IsRange {
		t.Error("expected range for")
	}
	if f.Item != "item" {
		t.Errorf("expected item 'item', got %q", f.Item)
	}
}

func TestParseClass(t *testing.T) {
	src := `Dog {
		String name
		new(String n) { }
		pub String bark() { return name }
	}`
	prog, p := parse(src)
	assertNoErrors(t, p)
	cls, ok := prog.Decls[0].(*ClassDecl)
	if !ok {
		t.Fatal("expected ClassDecl")
	}
	if cls.Name != "Dog" {
		t.Errorf("expected class name 'Dog'")
	}
	if len(cls.Fields) != 1 {
		t.Errorf("expected 1 field, got %d", len(cls.Fields))
	}
	if cls.Ctor == nil {
		t.Error("expected constructor")
	}
	if len(cls.Methods) != 1 {
		t.Errorf("expected 1 method, got %d", len(cls.Methods))
	}
}

func TestParseInterface(t *testing.T) {
	prog, p := parse(`interface Speaker { pub String speak() }`)
	assertNoErrors(t, p)
	iface, ok := prog.Decls[0].(*InterfaceDecl)
	if !ok {
		t.Fatal("expected InterfaceDecl")
	}
	if iface.Name != "Speaker" {
		t.Errorf("expected interface name 'Speaker'")
	}
	if len(iface.Methods) != 1 {
		t.Errorf("expected 1 method sig")
	}
}

func TestParseOrHandler(t *testing.T) {
	prog, p := parse(`main() { x := risky() or { print(err) } }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	v, ok := fn.Body.Stmts[0].(*VarStmt)
	if !ok {
		t.Fatal("expected VarStmt")
	}
	if v.OrHandler == nil {
		t.Fatal("expected OrHandler on VarStmt")
	}
	if len(v.OrHandler.Body.Stmts) != 1 {
		t.Errorf("expected 1 statement in or handler body, got %d", len(v.OrHandler.Body.Stmts))
	}
}

func TestParseOrHandlerOnExprStmt(t *testing.T) {
	prog, p := parse(`main() { risky() or { print(err) } }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	es, ok := fn.Body.Stmts[0].(*ExprStmt)
	if !ok {
		t.Fatal("expected ExprStmt")
	}
	if es.OrHandler == nil {
		t.Fatal("expected OrHandler on ExprStmt")
	}
}

func TestParseOrHandlerOnAssign(t *testing.T) {
	prog, p := parse(`main() { x := 0; x = risky() or { print(err) } }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	a, ok := fn.Body.Stmts[1].(*AssignStmt)
	if !ok {
		t.Fatal("expected AssignStmt")
	}
	if a.OrHandler == nil {
		t.Fatal("expected OrHandler on AssignStmt")
	}
}

func TestParseReturnError(t *testing.T) {
	prog, p := parse(`Int risky() { return Error("oops") }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	ret, ok := fn.Body.Stmts[0].(*ReturnStmt)
	if !ok {
		t.Fatal("expected ReturnStmt")
	}
	call, ok := ret.Value.(*CallExpr)
	if !ok {
		t.Fatal("expected CallExpr as return value")
	}
	ident, ok := call.Callee.(*Ident)
	if !ok || ident.Name != "Error" {
		t.Fatal("expected Error() call")
	}
}

func TestParseImport(t *testing.T) {
	prog, p := parse(`import "fmt"`)
	assertNoErrors(t, p)
	if len(prog.Imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(prog.Imports))
	}
	if prog.Imports[0].Path != "fmt" {
		t.Errorf("expected path 'fmt', got %q", prog.Imports[0].Path)
	}
}

func TestParseImportAlias(t *testing.T) {
	prog, p := parse(`import "math/rand" as rand`)
	assertNoErrors(t, p)
	if prog.Imports[0].Alias != "rand" {
		t.Errorf("expected alias 'rand'")
	}
}

func TestParseBinaryExpr(t *testing.T) {
	prog, p := parse(`main() { x := 1 + 2 }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	v := fn.Body.Stmts[0].(*VarStmt)
	_, ok := v.Value.(*BinaryExpr)
	if !ok {
		t.Fatal("expected BinaryExpr")
	}
}

func TestParseCallExpr(t *testing.T) {
	prog, p := parse(`main() { foo(1, 2) }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	es := fn.Body.Stmts[0].(*ExprStmt)
	call, ok := es.Expr.(*CallExpr)
	if !ok {
		t.Fatal("expected CallExpr")
	}
	if len(call.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(call.Args))
	}
}

func TestParseListLit(t *testing.T) {
	prog, p := parse(`main() { x := [1, 2, 3] }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	v := fn.Body.Stmts[0].(*VarStmt)
	lit, ok := v.Value.(*ListLit)
	if !ok {
		t.Fatal("expected ListLit")
	}
	if len(lit.Elements) != 3 {
		t.Errorf("expected 3 elements, got %d", len(lit.Elements))
	}
}

func TestParseMapLit(t *testing.T) {
	prog, p := parse(`main() { m := {"key": 1} }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	v := fn.Body.Stmts[0].(*VarStmt)
	_, ok := v.Value.(*MapLit)
	if !ok {
		t.Fatal("expected MapLit")
	}
}

func TestParseConstructorNew(t *testing.T) {
	prog, p := parse(`main() { d := Dog("rex") }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	v := fn.Body.Stmts[0].(*VarStmt)
	_, ok := v.Value.(*CallExpr)
	if !ok {
		t.Fatal("expected CallExpr for Dog()")
	}
}

func TestParsePubFn(t *testing.T) {
	prog, p := parse(`pub String greet() { return "hi" }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	if !fn.IsPub {
		t.Error("expected IsPub = true")
	}
}

func TestParsePackageDecl(t *testing.T) {
	prog, p := parse(`package "myapp/utils"
Int add(Int a, Int b) { return a }`)
	assertNoErrors(t, p)
	if prog.Package == nil {
		t.Fatal("expected Package to be non-nil")
	}
	if prog.Package.Path != "myapp/utils" {
		t.Errorf("expected Path 'myapp/utils', got %q", prog.Package.Path)
	}
	if len(prog.Decls) != 1 {
		t.Errorf("expected 1 decl, got %d", len(prog.Decls))
	}
}

func TestParseNoPackageDecl(t *testing.T) {
	prog, p := parse(`main() { }`)
	assertNoErrors(t, p)
	if prog.Package != nil {
		t.Error("expected Package to be nil when not declared")
	}
}

func TestParsePackageDeclWithImports(t *testing.T) {
	prog, p := parse(`package "myapp/models"
import "fmt"
Dog { String name }`)
	assertNoErrors(t, p)
	if prog.Package == nil {
		t.Fatal("expected Package to be non-nil")
	}
	if prog.Package.Path != "myapp/models" {
		t.Errorf("expected path 'myapp/models', got %q", prog.Package.Path)
	}
	if len(prog.Imports) != 1 {
		t.Errorf("expected 1 import, got %d", len(prog.Imports))
	}
	if len(prog.Decls) != 1 {
		t.Errorf("expected 1 decl, got %d", len(prog.Decls))
	}
}

// --- Default parameter values ------------------------------------------------

func TestParseParamWithDefault(t *testing.T) {
	prog, p := parse(`greet(String name, String greeting = "Hello") {}`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	if len(fn.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(fn.Params))
	}
	if fn.Params[0].Default != nil {
		t.Error("expected no default for first param")
	}
	if fn.Params[1].Default == nil {
		t.Fatal("expected default for second param")
	}
	lit, ok := fn.Params[1].Default.(*StringLit)
	if !ok {
		t.Fatal("expected StringLit default")
	}
	if lit.Value != "Hello" {
		t.Errorf("expected default %q, got %q", "Hello", lit.Value)
	}
}

func TestParseCtorParamWithDefault(t *testing.T) {
	prog, p := parse(`Dog {
		String name
		Int age
		new(String name, Int age = 0) {}
	}`)
	assertNoErrors(t, p)
	cls := prog.Decls[0].(*ClassDecl)
	if cls.Ctor == nil {
		t.Fatal("expected ctor")
	}
	if len(cls.Ctor.Params) != 2 {
		t.Fatalf("expected 2 ctor params, got %d", len(cls.Ctor.Params))
	}
	if cls.Ctor.Params[0].Default != nil {
		t.Error("expected no default for first ctor param")
	}
	if cls.Ctor.Params[1].Default == nil {
		t.Fatal("expected default for second ctor param")
	}
	lit, ok := cls.Ctor.Params[1].Default.(*IntLit)
	if !ok {
		t.Fatalf("expected IntLit default, got %T", cls.Ctor.Params[1].Default)
	}
	if lit.Value != "0" {
		t.Errorf("expected default 0, got %q", lit.Value)
	}
}

// --- Named arguments at call sites -------------------------------------------

func TestParseNamedArgs(t *testing.T) {
	prog, p := parse(`main() { foo(x: 5, y: 10) }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	call := fn.Body.Stmts[0].(*ExprStmt).Expr.(*CallExpr)
	if len(call.Args) != 0 {
		t.Errorf("expected 0 positional args, got %d", len(call.Args))
	}
	if len(call.NamedArgs) != 2 {
		t.Fatalf("expected 2 named args, got %d", len(call.NamedArgs))
	}
	if call.NamedArgs[0].Name != "x" {
		t.Errorf("expected named arg x, got %q", call.NamedArgs[0].Name)
	}
	if call.NamedArgs[1].Name != "y" {
		t.Errorf("expected named arg y, got %q", call.NamedArgs[1].Name)
	}
}

func TestParseMixedPositionalAndNamedArgs(t *testing.T) {
	prog, p := parse(`main() { greet("Bob", greeting: "Hi") }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	call := fn.Body.Stmts[0].(*ExprStmt).Expr.(*CallExpr)
	if len(call.Args) != 1 {
		t.Fatalf("expected 1 positional arg, got %d", len(call.Args))
	}
	if len(call.NamedArgs) != 1 {
		t.Fatalf("expected 1 named arg, got %d", len(call.NamedArgs))
	}
	if call.NamedArgs[0].Name != "greeting" {
		t.Errorf("expected named arg greeting, got %q", call.NamedArgs[0].Name)
	}
}

func TestParseWithStmtSingle(t *testing.T) {
	prog, p := parse(`main() { with (f := openFile("x")) { } }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	ws, ok := fn.Body.Stmts[0].(*WithStmt)
	if !ok {
		t.Fatal("expected WithStmt")
	}
	if len(ws.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(ws.Resources))
	}
	if ws.Resources[0].Name != "f" {
		t.Errorf("expected resource name 'f', got %q", ws.Resources[0].Name)
	}
	if _, ok := ws.Resources[0].Value.(*CallExpr); !ok {
		t.Error("expected resource value to be CallExpr")
	}
}

func TestParseWithStmtMultiple(t *testing.T) {
	prog, p := parse(`main() { with (a := foo(), b := bar()) { } }`)
	assertNoErrors(t, p)
	fn := prog.Decls[0].(*FnDecl)
	ws, ok := fn.Body.Stmts[0].(*WithStmt)
	if !ok {
		t.Fatal("expected WithStmt")
	}
	if len(ws.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(ws.Resources))
	}
	if ws.Resources[0].Name != "a" {
		t.Errorf("expected resource[0] name 'a', got %q", ws.Resources[0].Name)
	}
	if ws.Resources[1].Name != "b" {
		t.Errorf("expected resource[1] name 'b', got %q", ws.Resources[1].Name)
	}
}
