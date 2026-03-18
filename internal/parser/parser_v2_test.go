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
	"strings"
	"testing"

	"zinc/internal/lexer"
)

func parseV2(src string) (*Program, []string) {
	lex := lexer.New(src)
	tokens := lex.Tokenize()
	p := New(tokens)
	prog := p.ParseV2()
	return prog, p.Errors
}

func TestV2ScriptMode(t *testing.T) {
	prog, errs := parseV2(`
var x = 42
print("hello")
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(prog.Stmts) != 2 {
		t.Fatalf("expected 2 top-level statements, got %d", len(prog.Stmts))
	}
	// First stmt is var
	if _, ok := prog.Stmts[0].(*VarStmt); !ok {
		t.Errorf("expected VarStmt, got %T", prog.Stmts[0])
	}
	// Second stmt is print
	if _, ok := prog.Stmts[1].(*PrintStmt); !ok {
		t.Errorf("expected PrintStmt, got %T", prog.Stmts[1])
	}
}

func TestV2FnDecl(t *testing.T) {
	prog, errs := parseV2(`
fn greet(name: str): str
    return "Hello, {name}!"
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(prog.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %d", len(prog.Decls))
	}
	fn := prog.Decls[0].(*FnDecl)
	if fn.Name != "greet" {
		t.Errorf("expected fn name 'greet', got %q", fn.Name)
	}
	if len(fn.Params) != 1 || fn.Params[0].Name != "name" {
		t.Errorf("expected 1 param 'name', got %v", fn.Params)
	}
	if fn.ReturnType == nil {
		t.Error("expected return type")
	}
	if len(fn.Body.Stmts) != 1 {
		t.Errorf("expected 1 body stmt, got %d", len(fn.Body.Stmts))
	}
}

func TestV2FnSingleExpr(t *testing.T) {
	prog, errs := parseV2(`fn double(x: int): int = x * 2`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	fn := prog.Decls[0].(*FnDecl)
	if fn.Name != "double" {
		t.Errorf("expected 'double', got %q", fn.Name)
	}
	if len(fn.Body.Stmts) != 1 {
		t.Fatalf("expected 1 body stmt (implicit return), got %d", len(fn.Body.Stmts))
	}
	ret, ok := fn.Body.Stmts[0].(*ReturnStmt)
	if !ok {
		t.Fatalf("expected ReturnStmt, got %T", fn.Body.Stmts[0])
	}
	bin, ok := ret.Value.(*BinaryExpr)
	if !ok || bin.Op != "*" {
		t.Errorf("expected x * 2, got %T", ret.Value)
	}
}

func TestV2IfElseEnd(t *testing.T) {
	prog, errs := parseV2(`
if x > 0
    print("positive")
else if x == 0
    print("zero")
else
    print("negative")
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(prog.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(prog.Stmts))
	}
	ifStmt, ok := prog.Stmts[0].(*IfStmt)
	if !ok {
		t.Fatalf("expected IfStmt, got %T", prog.Stmts[0])
	}
	if ifStmt.ElseStmt == nil {
		t.Fatal("expected else branch")
	}
	// else if
	elseIf, ok := ifStmt.ElseStmt.(*IfStmt)
	if !ok {
		t.Fatalf("expected else-if IfStmt, got %T", ifStmt.ElseStmt)
	}
	// else
	if elseIf.ElseStmt == nil {
		t.Fatal("expected else block after else-if")
	}
}

func TestV2ForLoop(t *testing.T) {
	prog, errs := parseV2(`
for item in items
    print(item)
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	forStmt := prog.Stmts[0].(*ForStmt)
	if !forStmt.IsRange {
		t.Error("expected range-style for")
	}
	if forStmt.Item != "item" {
		t.Errorf("expected item var 'item', got %q", forStmt.Item)
	}
}

func TestV2ForLoopWithIndex(t *testing.T) {
	prog, errs := parseV2(`
for i, item in items
    print(i)
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	forStmt := prog.Stmts[0].(*ForStmt)
	if forStmt.IndexVar != "i" {
		t.Errorf("expected index var 'i', got %q", forStmt.IndexVar)
	}
	if forStmt.Item != "item" {
		t.Errorf("expected item var 'item', got %q", forStmt.Item)
	}
}

func TestV2WhileLoop(t *testing.T) {
	prog, errs := parseV2(`
while running
    process_next()
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	whileStmt := prog.Stmts[0].(*WhileStmt)
	if whileStmt.Body == nil || len(whileStmt.Body.Stmts) != 1 {
		t.Error("expected 1 body stmt")
	}
}

func TestV2Match(t *testing.T) {
	prog, errs := parseV2(`
match command
    case "start" -> start_server()
    case "stop" -> stop_server()
    case _ -> print("unknown")
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	matchStmt := prog.Stmts[0].(*MatchStmt)
	if len(matchStmt.Cases) != 3 {
		t.Fatalf("expected 3 cases, got %d", len(matchStmt.Cases))
	}
	// Last case is wildcard (nil pattern)
	if matchStmt.Cases[2].Pattern != nil {
		t.Error("expected wildcard pattern (nil)")
	}
}

func TestV2DataClass(t *testing.T) {
	prog, errs := parseV2(`
data User
    name: str
    email: str
    age: int = 0
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	data := prog.Decls[0].(*DataClassDecl)
	if data.Name != "User" {
		t.Errorf("expected 'User', got %q", data.Name)
	}
	if len(data.Params) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(data.Params))
	}
	if data.Params[2].Default == nil {
		t.Error("expected default value for 'age'")
	}
}

func TestV2Enum(t *testing.T) {
	prog, errs := parseV2(`
enum Color
    Red
    Green
    Blue
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	enum := prog.Decls[0].(*EnumDecl)
	if len(enum.Variants) != 3 {
		t.Fatalf("expected 3 variants, got %d", len(enum.Variants))
	}
}

func TestV2Class(t *testing.T) {
	prog, errs := parseV2(`
class Stack
    var items: list[int] = []

    fn push(item: int)
        items.append(item)
    end

    fn pop(): int
        return items.pop()
    end
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	cls := prog.Decls[0].(*ClassDecl)
	if cls.Name != "Stack" {
		t.Errorf("expected 'Stack', got %q", cls.Name)
	}
	if len(cls.Fields) != 1 {
		t.Errorf("expected 1 field, got %d", len(cls.Fields))
	}
	if len(cls.Methods) != 2 {
		t.Errorf("expected 2 methods, got %d", len(cls.Methods))
	}
}

func TestV2Import(t *testing.T) {
	prog, errs := parseV2(`
import json
import os.path
from pathlib import Path
from requests import get as http_get
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(prog.Imports) != 4 {
		t.Fatalf("expected 4 imports, got %d", len(prog.Imports))
	}
	if prog.Imports[0].Path != "json" {
		t.Errorf("expected 'json', got %q", prog.Imports[0].Path)
	}
	if prog.Imports[1].Path != "os.path" {
		t.Errorf("expected 'os.path', got %q", prog.Imports[1].Path)
	}
	if !strings.HasPrefix(prog.Imports[2].Path, "from:pathlib:Path") {
		t.Errorf("expected from-import for Path, got %q", prog.Imports[2].Path)
	}
	if prog.Imports[3].Alias != "http_get" {
		t.Errorf("expected alias 'http_get', got %q", prog.Imports[3].Alias)
	}
}

func TestV2Lambda(t *testing.T) {
	prog, errs := parseV2(`var doubled = items.map(x -> x * 2)`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	call := varStmt.Value.(*CallExpr)
	if len(call.Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(call.Args))
	}
	lambda, ok := call.Args[0].(*LambdaExpr)
	if !ok {
		t.Fatalf("expected LambdaExpr, got %T", call.Args[0])
	}
	if len(lambda.Params) != 1 || lambda.Params[0].Name != "x" {
		t.Error("expected lambda param 'x'")
	}
}

func TestV2ExpressionIf(t *testing.T) {
	prog, errs := parseV2(`var label = if count == 1: "item" else: "items"`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	ifExpr, ok := varStmt.Value.(*IfExpr)
	if !ok {
		t.Fatalf("expected IfExpr, got %T", varStmt.Value)
	}
	if ifExpr.Then == nil || ifExpr.Else == nil {
		t.Error("expected both then and else branches")
	}
}

func TestV2AndOrNot(t *testing.T) {
	prog, errs := parseV2(`var x = a and b or not c`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	// Should be: (a && b) || (! c)
	or, ok := varStmt.Value.(*BinaryExpr)
	if !ok || or.Op != "||" {
		t.Fatalf("expected || at top, got %T %v", varStmt.Value, varStmt.Value)
	}
	and, ok := or.Left.(*BinaryExpr)
	if !ok || and.Op != "&&" {
		t.Errorf("expected && on left, got %v", or.Left)
	}
	notExpr, ok := or.Right.(*UnaryExpr)
	if !ok || notExpr.Op != "!" {
		t.Errorf("expected ! on right, got %v", or.Right)
	}
}

func TestV2TryCatch(t *testing.T) {
	prog, errs := parseV2(`
try
    var conn = db.connect(url)
catch err: ConnectionError
    print("failed")
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	tryStmt := prog.Stmts[0].(*TryStmt)
	if tryStmt.CatchName != "err" {
		t.Errorf("expected catch name 'err', got %q", tryStmt.CatchName)
	}
	if tryStmt.CatchType != "ConnectionError" {
		t.Errorf("expected catch type 'ConnectionError', got %q", tryStmt.CatchType)
	}
	if len(tryStmt.Body.Stmts) != 1 {
		t.Errorf("expected 1 try body stmt, got %d", len(tryStmt.Body.Stmts))
	}
	if len(tryStmt.CatchBody.Stmts) != 1 {
		t.Errorf("expected 1 catch body stmt, got %d", len(tryStmt.CatchBody.Stmts))
	}
}

func TestV2StringInterpolation(t *testing.T) {
	prog, errs := parseV2(`print("Hello, {name}!")`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	printStmt := prog.Stmts[0].(*PrintStmt)
	interp, ok := printStmt.Value.(*StringInterpLit)
	if !ok {
		t.Fatalf("expected StringInterpLit, got %T", printStmt.Value)
	}
	if len(interp.Parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(interp.Parts))
	}
}

func TestV2VarWithType(t *testing.T) {
	prog, errs := parseV2(`var age: int = 30`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	if varStmt.Name != "age" {
		t.Errorf("expected 'age', got %q", varStmt.Name)
	}
	if varStmt.Type == nil {
		t.Error("expected explicit type")
	}
}

func TestV2MethodChaining(t *testing.T) {
	prog, errs := parseV2(`
var result = orders.filter(o -> o.status == "active").map(o -> o.amount).sum()
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	// Should be: orders.filter(...).map(...).sum()
	call, ok := varStmt.Value.(*CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr at top, got %T", varStmt.Value)
	}
	sel, ok := call.Callee.(*SelectorExpr)
	if !ok || sel.Field != "sum" {
		t.Errorf("expected .sum() at top, got %v", call.Callee)
	}
}

func TestV2FullScript(t *testing.T) {
	// A complete script combining multiple v2 features
	_, errs := parseV2(`
import json
import sys
from pathlib import Path

data Config
    host: str
    port: int = 8080
end

fn load_config(path: str): Config
    var text = Path(path).read_text()
    var raw = json.loads(text)
    return Config(raw["host"], raw["port"])
end

var config = load_config("config.json")
print("Server at {config.host}:{config.port}")

if len(sys.argv) > 1
    print("Args: {sys.argv}")
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestV2Comprehension(t *testing.T) {
	prog, errs := parseV2(`var squares = [x * x for x in range(10)]`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	comp, ok := varStmt.Value.(*ComprehensionExpr)
	if !ok {
		t.Fatalf("expected ComprehensionExpr, got %T", varStmt.Value)
	}
	if comp.Var != "x" {
		t.Errorf("expected var 'x', got %q", comp.Var)
	}
	if comp.Cond != nil {
		t.Error("expected no condition")
	}
}

func TestV2ComprehensionWithFilter(t *testing.T) {
	prog, errs := parseV2(`var evens = [x for x in numbers if x % 2 == 0]`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	comp := varStmt.Value.(*ComprehensionExpr)
	if comp.Cond == nil {
		t.Error("expected filter condition")
	}
}

func TestV2ComprehensionInCall(t *testing.T) {
	// User writes [x for x in items] everywhere — even inside sum()
	// Codegen decides to strip brackets → generator
	prog, errs := parseV2(`var total = sum([x * x for x in range(10)])`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	call, ok := varStmt.Value.(*CallExpr)
	if !ok {
		t.Fatalf("expected CallExpr, got %T", varStmt.Value)
	}
	if len(call.Args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(call.Args))
	}
	_, ok = call.Args[0].(*ComprehensionExpr)
	if !ok {
		t.Fatalf("expected ComprehensionExpr arg, got %T", call.Args[0])
	}
}

func TestV2NotIn(t *testing.T) {
	prog, errs := parseV2(`var x = "a" not in items`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	bin, ok := varStmt.Value.(*BinaryExpr)
	if !ok || bin.Op != "not in" {
		t.Fatalf("expected 'not in' op, got %T %v", varStmt.Value, varStmt.Value)
	}
}

func TestV2IsNot(t *testing.T) {
	prog, errs := parseV2(`var x = value is not none`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	bin, ok := varStmt.Value.(*BinaryExpr)
	if !ok || bin.Op != "is not" {
		t.Fatalf("expected 'is not' op, got %T", varStmt.Value)
	}
}

func TestV2None(t *testing.T) {
	prog, errs := parseV2(`var x = none`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	_, ok := varStmt.Value.(*NullLit)
	if !ok {
		t.Fatalf("expected NullLit, got %T", varStmt.Value)
	}
}

func TestV2DictComprehension(t *testing.T) {
	prog, errs := parseV2(`var lengths = {word: len(word) for word in words}`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	_, ok := varStmt.Value.(*DictComprehensionExpr)
	if !ok {
		t.Fatalf("expected DictComprehensionExpr, got %T", varStmt.Value)
	}
}

func TestV2TupleUnpacking(t *testing.T) {
	prog, errs := parseV2(`var a, b = get_pair()`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	tuple, ok := prog.Stmts[0].(*TupleVarStmt)
	if !ok {
		t.Fatalf("expected TupleVarStmt, got %T", prog.Stmts[0])
	}
	if len(tuple.Names) != 2 || tuple.Names[0] != "a" || tuple.Names[1] != "b" {
		t.Errorf("expected [a, b], got %v", tuple.Names)
	}
}

func TestV2PowerOperator(t *testing.T) {
	prog, errs := parseV2(`var x = 2 ** 10`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	bin, ok := varStmt.Value.(*BinaryExpr)
	if !ok || bin.Op != "**" {
		t.Fatalf("expected ** op, got %T", varStmt.Value)
	}
}

func TestV2PrivateConvention(t *testing.T) {
	// _prefix fields should parse fine (just naming convention)
	prog, errs := parseV2(`
class Cache
    var _data: dict = {}

    fn _internal_method()
        print("private")
    end
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	cls := prog.Decls[0].(*ClassDecl)
	if cls.Fields[0].Name != "_data" {
		t.Errorf("expected '_data', got %q", cls.Fields[0].Name)
	}
	if cls.Methods[0].Name != "_internal_method" {
		t.Errorf("expected '_internal_method', got %q", cls.Methods[0].Name)
	}
}

func TestV2WithStatement(t *testing.T) {
	prog, errs := parseV2(`
with f = open("test.txt")
    var content = f.read()
    print(content)
end
`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	withStmt, ok := prog.Stmts[0].(*WithStmt)
	if !ok {
		t.Fatalf("expected WithStmt, got %T", prog.Stmts[0])
	}
	if len(withStmt.Resources) != 1 || withStmt.Resources[0].Name != "f" {
		t.Error("expected resource 'f'")
	}
	if len(withStmt.Body.Stmts) != 2 {
		t.Errorf("expected 2 body stmts, got %d", len(withStmt.Body.Stmts))
	}
}

func TestV2SingleQuoteString(t *testing.T) {
	prog, errs := parseV2(`var x = 'hello'`)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	varStmt := prog.Stmts[0].(*VarStmt)
	str, ok := varStmt.Value.(*StringLit)
	if !ok {
		t.Fatalf("expected StringLit, got %T", varStmt.Value)
	}
	if str.Value != "hello" {
		t.Errorf("expected 'hello', got %q", str.Value)
	}
}
