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

package codegen_csharp

import (
	"strings"
	"testing"

	"zinc/internal/lexer"
	"zinc/internal/parser"
)

// transpile is the test helper — tokenize → parse → C# codegen.
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

func TestMain_HelloWorld(t *testing.T) {
	out := transpile(`
main() {
    print("hello world")
}
`)
	assertContains(t, out, "public class Program")
	assertContains(t, out, "public static void Main(string[] args)")
	assertContains(t, out, `Console.WriteLine("hello world")`)
}

func TestFunctionDecl(t *testing.T) {
	out := transpile(`
Int add(Int a, Int b) {
    return a + b
}
main() {
    var x = add(1, 2)
    print(x)
}
`)
	assertContains(t, out, "int Add(int a, int b)")
	assertContains(t, out, "return (a + b);")
}

func TestVariables(t *testing.T) {
	out := transpile(`
main() {
    var x = 42
    var name = "hello"
    print(x)
}
`)
	assertContains(t, out, "var x = 42;")
	assertContains(t, out, `var name = "hello";`)
}

func TestClassDecl(t *testing.T) {
	out := transpile(`
Dog {
    pub String name
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
    var d = Dog(name: "Rex", age: 5)
    print(d.bark())
}
`)
	assertContains(t, out, "public class Dog")
	assertContains(t, out, "public string Name;")
	assertContains(t, out, "private int _age = 0;")
	assertContains(t, out, "public Dog(string name, int age = 0)")
	assertContains(t, out, "public string Bark()")
	assertContains(t, out, `return "Woof!";`)
	assertContains(t, out, "new Dog(name: ")
}

func TestClassInheritance(t *testing.T) {
	out := transpile(`
Animal {
    pub String name
    new(String name) { this.name = name }
    pub String speak() { return "..." }
}
Dog : Animal {
    new(String name) { super(name) }
    pub String speak() { return "Woof!" }
}
main() {
    var d = Dog(name: "Rex")
    print(d.speak())
}
`)
	assertContains(t, out, "public class Animal")
	assertContains(t, out, "public class Dog : Animal")
	assertContains(t, out, ": base(name)")
}

func TestInterface(t *testing.T) {
	out := transpile(`
interface Speaker {
    pub String speak()
}
Cat : Speaker {
    new() {}
    pub String speak() { return "Meow!" }
}
main() {
    var c = Cat()
    print(c.speak())
}
`)
	assertContains(t, out, "public interface ISpeaker")
	assertContains(t, out, "string Speak();")
	assertContains(t, out, "public class Cat : ISpeaker")
}

func TestIfElse(t *testing.T) {
	out := transpile(`
main() {
    var x = 10
    if x > 5 { print("big") } else { print("small") }
}
`)
	assertContains(t, out, "if ((x > 5))")
	assertContains(t, out, "else")
}

func TestForRange(t *testing.T) {
	out := transpile(`
main() {
    var items = [1, 2, 3]
    for item in items { print(item) }
}
`)
	assertContains(t, out, "foreach (var item in items)")
}

func TestForRangeWithIndex(t *testing.T) {
	out := transpile(`
main() {
    var items = ["a", "b", "c"]
    for i, item in items { print(i) }
}
`)
	assertContains(t, out, "for (int i = 0; i < items.Count; i++)")
	assertContains(t, out, "var item = items[i];")
}

func TestWhileLoop(t *testing.T) {
	out := transpile(`
main() {
    var x = 0
    while x < 10 { x += 1 }
}
`)
	assertContains(t, out, "while ((x < 10))")
}

func TestMapLiteral(t *testing.T) {
	out := transpile(`
main() {
    var scores = {"Alice": 90, "Bob": 85}
    print(scores)
}
`)
	assertContains(t, out, "new Dictionary<object, object>")
	assertContains(t, out, `{ "Alice", 90 }`)
}

func TestStringInterpolation(t *testing.T) {
	out := transpile(`
main() {
    var name = "world"
    print("hello {name}!")
}
`)
	assertContains(t, out, `$"hello {name}!"`)
}

func TestEnum(t *testing.T) {
	out := transpile(`
enum Color { Red, Green, Blue }
main() { var c = Color.Red }
`)
	assertContains(t, out, "public enum Color")
	assertContains(t, out, "Red,")
	assertContains(t, out, "Green,")
	assertContains(t, out, "Blue")
}

func TestErrorHandling(t *testing.T) {
	out := transpile(`
main() {
    var x = riskyCall() or { print("failed") }
}
Int riskyCall() { return 42 }
`)
	assertContains(t, out, "try")
	assertContains(t, out, "catch (Exception)")
	assertContains(t, out, "throw;")
}

func TestBuiltinListMethods(t *testing.T) {
	out := transpile(`
main() {
    var items = [1, 2, 3]
    items.add(4)
    var n = items.size()
    print(n)
}
`)
	assertContains(t, out, "items.Add(4)")
	assertContains(t, out, "items.Count")
}

func TestBuiltinStringMethods(t *testing.T) {
	out := transpile(`
main() {
    var s = "Hello World"
    var u = s.upper()
    var l = s.lower()
    var t = s.trim()
    print(u)
}
`)
	assertContains(t, out, "s.ToUpper()")
	assertContains(t, out, "s.ToLower()")
	assertContains(t, out, "s.Trim()")
}

func TestBuiltinMapMethods(t *testing.T) {
	out := transpile(`
main() {
    var m = {"a": 1}
    var k = m.keys()
    var v = m.values()
    print(k)
}
`)
	assertContains(t, out, "m.Keys.ToList()")
	assertContains(t, out, "m.Values.ToList()")
	assertContains(t, out, "using System.Linq;")
}

func TestLambda(t *testing.T) {
	out := transpile(`
main() {
    var f = (Int x) -> x * 2
    print(f)
}
`)
	assertContains(t, out, "x => (x * 2)")
}

func TestMatchStmt(t *testing.T) {
	out := transpile(`
main() {
    var x = 1
    match x {
        case 1 -> { print("one") }
        case 2 -> { print("two") }
        case _ -> { print("other") }
    }
}
`)
	assertContains(t, out, "switch (x)")
	assertContains(t, out, "case 1:")
	assertContains(t, out, "case 2:")
	assertContains(t, out, "default:")
	assertContains(t, out, "break;")
}

func TestSafeNavigation(t *testing.T) {
	out := transpile(`
main() {
    var x = null
    var y = x?.name
}
`)
	assertContains(t, out, "x?.Name")
}

func TestGenericClass(t *testing.T) {
	out := transpile(`
Box<T> {
    pub T value
    new(T value) { this.value = value }
    pub T get() { return this.value }
}
main() {
    var b = Box<Int>(42)
    print(b.get())
}
`)
	assertContains(t, out, "public class Box<T>")
	assertContains(t, out, "public T Value;")
	assertContains(t, out, "public T Get()")
}

func TestWithStatement(t *testing.T) {
	out := transpile(`
main() {
    with (f = openFile("test.txt")) {
        print(f)
    }
}
`)
	assertContains(t, out, "using (var f = ")
}

func TestGoStatement(t *testing.T) {
	out := transpile(`
main() {
    go { print("async") }
}
`)
	assertContains(t, out, "Task.Run(() =>")
	assertContains(t, out, "using System.Threading.Tasks;")
}

func TestConstDecl(t *testing.T) {
	out := transpile(`
pub const Int MAX = 100
main() { print(MAX) }
`)
	assertContains(t, out, "public const int MAX = 100;")
}

func TestTypeMapping(t *testing.T) {
	out := transpile(`
main() {
    List<Int> nums = [1, 2, 3]
    Map<String, Int> scores = {"a": 1}
    print(nums)
}
`)
	assertContains(t, out, "List<int>")
	assertContains(t, out, "Dictionary<string, int>")
	assertContains(t, out, "using System.Collections.Generic;")
}

func TestConstructorCallUsesNew(t *testing.T) {
	out := transpile(`
Point {
    pub Int x
    pub Int y
    new(Int x, Int y) { this.x = x; this.y = y }
}
main() {
    var p = Point(1, 2)
    print(p)
}
`)
	assertContains(t, out, "new Point(1, 2)")
}

func TestPrivateFieldNaming(t *testing.T) {
	out := transpile(`
Secret {
    String hidden
    new(String hidden) { this.hidden = hidden }
}
main() { var s = Secret("shh") }
`)
	assertContains(t, out, "private string _hidden;")
}

func TestUsingsIncluded(t *testing.T) {
	out := transpile(`
main() {
    print("hello")
}
`)
	assertContains(t, out, "using System;")
}
