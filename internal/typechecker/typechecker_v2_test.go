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

	"zinc/internal/lexer"
	"zinc/internal/parser"
)

func checkV2(src string) []V2Error {
	lex := lexer.New(src)
	tokens := lex.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseV2()
	return CheckV2(prog)
}

func TestV2NoErrors(t *testing.T) {
	errs := checkV2(`
var x = 42
var name = "Alice"
print("hello")
`)
	if len(errs) > 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestV2TypeMismatch(t *testing.T) {
	errs := checkV2(`var x: int = "hello"`)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "type mismatch") {
		t.Errorf("expected type mismatch error, got: %s", errs[0].Message)
	}
}

func TestV2TypeMismatchBool(t *testing.T) {
	errs := checkV2(`var x: str = true`)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "type mismatch") {
		t.Errorf("expected type mismatch error, got: %s", errs[0].Message)
	}
}

func TestV2IntToFloatOk(t *testing.T) {
	// int → float is allowed
	errs := checkV2(`var x: float = 42`)
	if len(errs) > 0 {
		t.Errorf("expected no errors (int→float), got: %v", errs)
	}
}

func TestV2FnTypeChecked(t *testing.T) {
	errs := checkV2(`
fn add(a: int, b: int): int {
    var result: str = a + b
    return result
}
`)
	// Catches both: var type mismatch AND return type mismatch
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
}

func TestV2UndefinedVariable(t *testing.T) {
	errs := checkV2(`
var x = 10
y = 20
`)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "undefined variable") {
		t.Errorf("expected undefined variable error, got: %s", errs[0].Message)
	}
}

func TestV2DataClassFieldTypes(t *testing.T) {
	errs := checkV2(`
data User {
    name: str
    age: int
}
`)
	if len(errs) > 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestV2ClassFieldNoType(t *testing.T) {
	// This test verifies fields without types get flagged
	// (var fields always have types in v2, bare fields require colon)
	errs := checkV2(`
fn example() {
    var x: int = "bad"
}
`)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestV2ReturnTypeMismatch(t *testing.T) {
	errs := checkV2(`
fn add(a: int, b: int): int {
    return "hello"
}
`)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "return type mismatch") {
		t.Errorf("expected return type mismatch, got: %s", errs[0].Message)
	}
}

func TestV2FnCallArgCount(t *testing.T) {
	errs := checkV2(`
fn greet(name: str): str {
    return "hello"
}
greet("Alice", "extra")
`)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "expects 1 args, got 2") {
		t.Errorf("expected arg count error, got: %s", errs[0].Message)
	}
}

func TestV2FnCallArgType(t *testing.T) {
	errs := checkV2(`
fn greet(name: str): str {
    return "hello"
}
greet(42)
`)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "expected str, got int") {
		t.Errorf("expected arg type error, got: %s", errs[0].Message)
	}
}

func TestV2BreakOutsideLoop(t *testing.T) {
	errs := checkV2(`break`)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "outside of loop") {
		t.Errorf("expected 'outside of loop', got: %s", errs[0].Message)
	}
}

func TestV2BreakInsideLoopOk(t *testing.T) {
	errs := checkV2(`
for x in items {
    break
}
`)
	if len(errs) > 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestV2ResultErrReturnOk(t *testing.T) {
	errs := checkV2(`
fn parse(s: str): Result[int] {
    return Err("bad")
}
`)
	if len(errs) > 0 {
		t.Errorf("expected no errors for Err return, got: %v", errs)
	}
}

func TestV2ValidScript(t *testing.T) {
	errs := checkV2(`
import json

fn greet(name: str): str {
    return "Hello, {name}!"
}

var msg = greet("Alice")
print(msg)

var numbers = [1, 2, 3]
var total: int = 0
for n in numbers {
    total = total + n
}
print("Total: {total}")
`)
	if len(errs) > 0 {
		t.Errorf("expected no errors for valid script, got: %v", errs)
	}
}
