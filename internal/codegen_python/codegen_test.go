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
	return transpileWith(src, StrategyComprehension)
}

func transpileWith(src string, strategy CollectionStrategy) string {
	tokens := lexer.New(src).Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		return "PARSE ERROR: " + strings.Join(p.Errors, "; ")
	}
	g := New()
	g.Strategy = strategy
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

// === Collection Methods: Comprehension Strategy ===

func TestComprehensionWhere(t *testing.T) {
	out := transpile(`
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    evens := nums.Where(x => x % 2 == 0).ToList()
    print(evens)
}
`)
	assertContains(t, out, "[x for x in")
	assertContains(t, out, "((x % 2) == 0)")
}

func TestComprehensionSelect(t *testing.T) {
	out := transpile(`
main() {
    nums := [1, 2, 3, 4, 5]
    doubled := nums.Select(x => x * 2).ToList()
    print(doubled)
}
`)
	assertContains(t, out, "(x * 2) for x in")
}

func TestComprehensionWhereSelect(t *testing.T) {
	out := transpile(`
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    result := nums.Where(x => x > 5).Select(x => x * 2).ToList()
    print(result)
}
`)
	// Correctly substituted: no _x leak
	assertContains(t, out, "for x in")
	assertNotContains(t, out, "_x")
}

func TestComprehensionAny(t *testing.T) {
	out := transpile(`
main() {
    nums := [1, 2, 3, 4, 5]
    hasEven := nums.Any(x => x % 2 == 0)
    print(hasEven)
}
`)
	assertContains(t, out, "any(")
	assertContains(t, out, "for x in")
}

func TestComprehensionAll(t *testing.T) {
	out := transpile(`
main() {
    nums := [2, 4, 6, 8]
    allEven := nums.All(x => x % 2 == 0)
    print(allEven)
}
`)
	assertContains(t, out, "all(")
}

func TestComprehensionFirst(t *testing.T) {
	out := transpile(`
main() {
    nums := [1, 2, 3, 4, 5]
    first := nums.Where(x => x > 3).First()
    print(first)
}
`)
	assertContains(t, out, "next(iter(")
	assertNotContains(t, out, "_found")
}

func TestComprehensionFirstOrDefault(t *testing.T) {
	out := transpile(`
main() {
    nums := [1, 2, 3]
    result := nums.Where(x => x > 10).FirstOrDefault()
    print(result)
}
`)
	assertContains(t, out, "next(iter(")
	assertContains(t, out, "None)")
}

func TestComprehensionCount(t *testing.T) {
	out := transpile(`
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    count := nums.Where(x => x > 5).Count()
    print(count)
}
`)
	assertContains(t, out, "len(")
}

func TestComprehensionTakeSkip(t *testing.T) {
	out := transpile(`
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    result := nums.Where(x => x > 3).Take(5).ToList()
    print(result)
}
`)
	assertContains(t, out, "[:5]")
	assertNotContains(t, out, "_take")
}

func TestComprehensionAggregate(t *testing.T) {
	out := transpile(`
main() {
    nums := [1, 2, 3, 4, 5]
    sum := nums.Aggregate(0, (acc, x) => acc + x)
    print(sum)
}
`)
	assertContains(t, out, "functools.reduce")
	assertContains(t, out, "import functools")
}

func TestComprehensionForEach(t *testing.T) {
	out := transpile(`
main() {
    nums := [1, 2, 3, 4, 5]
    nums.Where(x => x > 3).ForEach(x => x)
}
`)
	assertContains(t, out, "for x in")
}

func TestComprehensionChainComplex(t *testing.T) {
	out := transpile(`
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    result := nums.Where(x => x > 2).Select(x => x * 3).Skip(2).Take(3).ToList()
    print(result)
}
`)
	assertContains(t, out, "for x in")
	assertContains(t, out, "[2:]")
	assertContains(t, out, "[:3]")
}

// === Collection Methods: NumPy Strategy ===

func TestNumPyWhereSelect(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    result := nums.Where(x => x > 5).Select(x => x * 2).ToList()
    print(result)
}
`, StrategyNumPy)
	assertContains(t, out, "import numpy as np")
	assertContains(t, out, "np.array(nums)")
	// Boolean indexing for Where
	assertContains(t, out, "> 5")
	// Vectorized arithmetic for Select
	assertContains(t, out, "* 2")
	assertContains(t, out, ".tolist()")
}

func TestNumPyAny(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [1, 2, 3, 4, 5]
    hasEven := nums.Any(x => x % 2 == 0)
    print(hasEven)
}
`, StrategyNumPy)
	assertContains(t, out, "np.any(")
}

func TestNumPyAll(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [2, 4, 6, 8]
    allEven := nums.All(x => x % 2 == 0)
    print(allEven)
}
`, StrategyNumPy)
	assertContains(t, out, "np.all(")
}

func TestNumPyAggregate(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [1, 2, 3, 4, 5]
    total := nums.Aggregate(0, (acc, x) => acc + x)
    print(total)
}
`, StrategyNumPy)
	// Should detect simple sum pattern and use np.sum
	assertContains(t, out, "np.sum(")
}

func TestNumPyFirst(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    first := nums.Where(x => x > 5).First()
    print(first)
}
`, StrategyNumPy)
	assertContains(t, out, "[0]")
}

func TestNumPyTakeSkip(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    result := nums.Skip(3).Take(4).ToList()
    print(result)
}
`, StrategyNumPy)
	assertContains(t, out, "[3:]")
	assertContains(t, out, "[:4]")
}

func TestNumPyWhereCount(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    n := nums.Where(x => x > 5).Count()
    print(n)
}
`, StrategyNumPy)
	assertContains(t, out, "len(")
	assertContains(t, out, "> 5")
}

// === Collection Methods: Numba Strategy ===

func TestNumbaWhereSelect(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    result := nums.Where(x => x > 5).Select(x => x * 2).ToList()
    print(result)
}
`, StrategyNumba)
	assertContains(t, out, "import numba")
	assertContains(t, out, "@numba.jit(nopython=True)")
	assertContains(t, out, "def _chain_")
	assertContains(t, out, "for _x in _src:")
	assertContains(t, out, "if (_x > 5):")
	assertContains(t, out, "* 2")
	assertContains(t, out, "np.empty(_n, dtype=np.int64)")
	assertContains(t, out, "_result[_j] =")
}

func TestNumbaAggregate(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [1, 2, 3, 4, 5]
    total := nums.Aggregate(0, (acc, x) => acc + x)
    print(total)
}
`, StrategyNumba)
	assertContains(t, out, "@numba.jit(nopython=True)")
	assertContains(t, out, "_acc = 0")
	assertContains(t, out, "_acc = (_acc + _x)")
}

func TestNumbaAny(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [1, 2, 3, 4, 5]
    hasEven := nums.Any(x => x % 2 == 0)
    print(hasEven)
}
`, StrategyNumba)
	assertContains(t, out, "@numba.jit(nopython=True)")
	assertContains(t, out, "_found = False")
	assertContains(t, out, "_found = True")
	assertContains(t, out, "break")
}

func TestNumbaAll(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [2, 4, 6, 8]
    allEven := nums.All(x => x % 2 == 0)
    print(allEven)
}
`, StrategyNumba)
	assertContains(t, out, "_found = True")
	assertContains(t, out, "_found = False")
}

func TestNumbaTakeSkip(t *testing.T) {
	out := transpileWith(`
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    result := nums.Where(x => x > 2).Take(3).ToList()
    print(result)
}
`, StrategyNumba)
	assertContains(t, out, "_taken = 0")
	assertContains(t, out, "if _taken >= 3:")
	assertContains(t, out, "break")
}
