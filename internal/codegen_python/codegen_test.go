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

package codegen_python

import (
	"strings"
	"testing"

	"zinc/internal/lexer"
	"zinc/internal/parser"
)

// transpile is the test helper — tokenize → parse → Python codegen.
func transpile(src string) string {
	tokens := lexer.New(src).Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		return "PARSE ERROR: " + strings.Join(p.Errors, "; ")
	}
	g := New()
	return g.Generate(prog)
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("output does not contain %q\n--- got ---\n%s", want, got)
	}
}

func assertNotContains(t *testing.T, got, notWant string) {
	t.Helper()
	if strings.Contains(got, notWant) {
		t.Errorf("output should not contain %q\n--- got ---\n%s", notWant, got)
	}
}

// === Core Language ===

func TestFunctionDecl(t *testing.T) {
	out := transpile(`
greet(String name) {
    print("hello")
}
main() {
    greet("world")
}
`)
	assertContains(t, out, "def greet(name):")
	assertContains(t, out, `print("hello")`)
	assertContains(t, out, "def main():")
}

func TestFunctionWithReturn(t *testing.T) {
	out := transpile(`
Int add(Int a, Int b) {
    return a + b
}
main() {
    x := add(1, 2)
    print(x)
}
`)
	assertContains(t, out, "def add(a, b):")
	assertContains(t, out, "return (a + b)")
}

func TestVariables(t *testing.T) {
	out := transpile(`
main() {
    x := 42
    name := "hello"
    items := [1, 2, 3]
    print(x)
}
`)
	assertContains(t, out, "x = 42")
	assertContains(t, out, `name = "hello"`)
	assertContains(t, out, "items = [1, 2, 3]")
}

func TestClassDecl(t *testing.T) {
	out := transpile(`
Dog {
    String name
    Int age = 0

    new(String name, Int age = 0) {
        this.name = name
        this.age = age
    }

    pub String bark() {
        return "Woof!"
    }
}
main() {
    d := Dog(name: "Rex", age: 5)
    print(d.bark())
}
`)
	assertContains(t, out, "class Dog:")
	assertContains(t, out, "def __init__(self, name, age=0):")
	assertContains(t, out, "self.name = name")
	assertContains(t, out, "def bark(self):")
	assertContains(t, out, `return "Woof!"`)
}

func TestClassInheritance(t *testing.T) {
	out := transpile(`
Animal {
    String name
    new(String name) { this.name = name }
    pub String speak() { return "..." }
}
Dog : Animal {
    new(String name) { super(name) }
    pub String speak() { return "Woof!" }
}
main() {
    d := Dog(name: "Rex")
    print(d.speak())
}
`)
	assertContains(t, out, "class Animal:")
	assertContains(t, out, "class Dog(Animal):")
	assertContains(t, out, "super().__init__(")
}

func TestIfElse(t *testing.T) {
	out := transpile(`
main() {
    x := 10
    if x > 5 { print("big") } else { print("small") }
}
`)
	assertContains(t, out, "if (x > 5):")
	assertContains(t, out, "else:")
}

func TestForRange(t *testing.T) {
	out := transpile(`
main() {
    items := [1, 2, 3]
    for item in items { print(item) }
}
`)
	assertContains(t, out, "for item in items:")
}

func TestForRangeWithIndex(t *testing.T) {
	out := transpile(`
main() {
    items := ["a", "b", "c"]
    for i, item in items { print(i) }
}
`)
	assertContains(t, out, "for i, item in enumerate(items):")
}

func TestMapLiteral(t *testing.T) {
	out := transpile(`
main() {
    scores := {"Alice": 90, "Bob": 85}
    print(scores)
}
`)
	assertContains(t, out, `scores = {"Alice": 90, "Bob": 85}`)
}

func TestStringInterpolation(t *testing.T) {
	out := transpile(`
main() {
    name := "world"
    print("hello {name}!")
}
`)
	assertContains(t, out, `f"hello {name}!"`)
}

func TestEnum(t *testing.T) {
	out := transpile(`
enum Color { Red, Green, Blue }
main() { c := Color.Red }
`)
	assertContains(t, out, "import enum")
	assertContains(t, out, "class Color(enum.Enum):")
	assertContains(t, out, "Red = 1")
}

func TestErrorHandling(t *testing.T) {
	out := transpile(`
main() {
    x := riskyCall() or { print("failed"); return }
}
Int riskyCall() { return 42 }
`)
	assertContains(t, out, "try:")
	assertContains(t, out, "except Exception as err:")
}

func TestBuiltinMethods(t *testing.T) {
	out := transpile(`
main() {
    items := [1, 2, 3]
    items.add(4)
    n := items.size()
    s := "Hello World"
    u := s.upper()
    print(n)
}
`)
	assertContains(t, out, "items.append(4)")
	assertContains(t, out, "len(items)")
	assertContains(t, out, "s.upper()")
}

func TestLambda(t *testing.T) {
	out := transpile(`
Int apply(Int x, Int Fn(Int) f) { return f(x) }
main() {
    result := apply(5, x => x * 2)
    print(result)
}
`)
	assertContains(t, out, "lambda x: (x * 2)")
}

