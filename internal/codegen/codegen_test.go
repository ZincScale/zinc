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

package codegen

import (
	"strings"
	"testing"

	"zinc/internal/lexer"
	"zinc/internal/parser"
	"zinc/internal/typechecker"
)

func transpile(src string) (string, []string) {
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		return "", p.Errors
	}
	gen := New()
	return gen.Generate(prog), nil
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("expected output to contain %q\ngot:\n%s", want, got)
	}
}

func assertNotContains(t *testing.T, got, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Errorf("expected output NOT to contain %q\ngot:\n%s", want, got)
	}
}

func TestHelloWorld(t *testing.T) {
	out, errs := transpile(`main() { print("Hello, World!") }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func main()")
	assertContains(t, out, `fmt.Println("Hello, World!")`)
	assertContains(t, out, `"fmt"`)
}

func TestVarDecl(t *testing.T) {
	out, errs := transpile(`main() { x := 42 }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "x := 42")
}

func TestBinaryExpr(t *testing.T) {
	out, errs := transpile(`main() { x := 1 + 2 }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "(1 + 2)")
}

func TestIfElse(t *testing.T) {
	out, errs := transpile(`main() { if x { print("yes") } else { print("no") } }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "if x {")
	assertContains(t, out, "} else {")
}

func TestWhile(t *testing.T) {
	out, errs := transpile(`main() { while true { } }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "for true {")
}

func TestForIn(t *testing.T) {
	out, errs := transpile(`main() { for item in items { } }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "for _, item := range items {")
}

func TestForCStyle(t *testing.T) {
	out, errs := transpile(`main() { for (i := 0; i; i) { } }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "for i := 0; i; i {")
}

func TestClass(t *testing.T) {
	src := `Dog {
		pub String name
		new(String n) {
			this.name = n
		}
		pub String bark() { return "woof" }
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type DogImpl struct {")
	assertContains(t, out, "Name string")
	assertContains(t, out, "type Dog interface {")
	assertContains(t, out, "func NewDog(n string) *DogImpl {")
	assertContains(t, out, "func (d *DogImpl) Bark() string {")
}

func TestPrivateField(t *testing.T) {
	src := `Dog {
		String secret
		pub String name
		new(String n, String s) {
			this.name = n
			this.secret = s
		}
		pub String greet() { return this.name + this.secret }
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Private field: lowercase in struct, no getter/setter
	assertContains(t, out, "secret string")
	assertNotContains(t, out, "GetSecret()")
	assertNotContains(t, out, "SetSecret(")
	// Pub field: capitalized in struct, with getter/setter
	assertContains(t, out, "Name string")
	assertContains(t, out, "GetName()")
	assertContains(t, out, "SetName(")
}

func TestPrivateConst(t *testing.T) {
	src := `const rate = 0.05
	pub const MAX = 100`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Private const: uncapitalized
	assertContains(t, out, "const rate = 0.05")
	// Pub const: capitalized
	assertContains(t, out, "const MAX = 100")
}

func TestInterface(t *testing.T) {
	src := `interface Speaker {
		pub String speak()
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type Speaker interface {")
	assertContains(t, out, "Speak() string")
}

func TestInterfaceComplianceCheck(t *testing.T) {
	src := `interface Speaker { pub String speak() }
	Dog : Speaker {
		pub String speak() { return "woof" }
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "var _ Speaker = (*DogImpl)(nil)")
}

func TestReturnErrorAndOrHandler(t *testing.T) {
	src := `String risky() {
		return Error("oops")
	}
	main() {
		r := risky() or {
			print("caught")
			exit(1)
		}
		print(r)
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "fmt.Errorf")
	assertContains(t, out, "!= nil")
}

func TestConcurrency(t *testing.T) {
	src := `main() {
		Chan<Int> ch = Chan(1)
		go {
			ch.send(42)
		}
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "make(chan int, 1)")
	assertContains(t, out, "go func()")
	assertContains(t, out, "ch <- 42")
}

func TestImport(t *testing.T) {
	src := `import "os"
	main() { }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `"os"`)
}

func TestListLiteral(t *testing.T) {
	out, errs := transpile(`main() { x := [1, 2, 3] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "[]int{1, 2, 3}")
}

func TestListLiteralStrings(t *testing.T) {
	out, errs := transpile(`main() { x := ["a", "b"] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `[]string{"a", "b"}`)
}

func TestListLiteralEmpty(t *testing.T) {
	out, errs := transpile(`main() { x := [] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "[]interface{}{}")
}

func TestMapLiteral(t *testing.T) {
	src := `main() { m := {"a": 1} }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `map[string]int`)
}

func TestPubFnExported(t *testing.T) {
	src := `pub String greet() { return "hi" }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func Greet() string {")
}

func TestStaticMethod(t *testing.T) {
	src := `Math {
		pub static Int square(Int n) { return n }
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func Math_Square(n int) int {")
}

func TestBuiltinSize(t *testing.T) {
	src := `main() { n := items.size() }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "len(items)")
}

func TestBuiltinToString(t *testing.T) {
	src := `main() { s := toString(42) }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `fmt.Sprintf("%v", 42)`)
	assertContains(t, out, `"fmt"`)
}

func TestBuiltinUpper(t *testing.T) {
	src := `main() { s := "hello".upper() }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `strings.ToUpper("hello")`)
	assertContains(t, out, `"strings"`)
}

func TestBuiltinSort(t *testing.T) {
	src := `main() { nums.sort() }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "sort.Slice(nums")
	assertContains(t, out, `"sort"`)
}

func TestBuiltinSqrt(t *testing.T) {
	src := `main() { r := sqrt(9.0) }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "math.Sqrt(9.0)")
	assertContains(t, out, `"math"`)
}

func TestTupleUnpack(t *testing.T) {
	src := `main() { (a, b) := getPair() }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "a, b := getPair()")
}

func TestTupleUnpackThree(t *testing.T) {
	src := `main() { (x, y, z) := getTriple() }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "x, y, z := getTriple()")
}

func TestTupleUnpackFailable(t *testing.T) {
	src := `import "os"
main() {
    (r, w) := os.Pipe()
    print(r)
    print(w)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Should emit: r, w, _err1 := os.Pipe()  (error auto-appended)
	assertContains(t, out, "r, w, _err")
	assertContains(t, out, ":= os.Pipe()")
	// Should emit error check
	assertContains(t, out, "!= nil")
}

func TestGenericFn(t *testing.T) {
	src := `T identity<T>(T val) { return val }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func identity[T any](val T) T {")
}

func TestGenericFnMultiParam(t *testing.T) {
	src := `K pair<K, V>(K key, V val) { return key }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func pair[K any, V any](key K, val V) K {")
}

func TestGenericClass(t *testing.T) {
	src := `Box<T> { T value }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type BoxImpl[T any] struct {")
}

func TestOptionalType(t *testing.T) {
	src := `String greet(String? name) { return "hi" }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "name *string")
}

func TestOptionalTypeVar(t *testing.T) {
	src := `main() { Int? x }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "var x *int")
}

func TestStringInterpolation(t *testing.T) {
	src := `main() { name := "World"
		print("Hello, {name}!")
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `fmt.Sprintf("Hello, %v!", name)`)
}

func TestStringInterpolationMultiple(t *testing.T) {
	src := `main() { a := 1
		b := 2
		print("Sum of {a} and {b}")
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `fmt.Sprintf("Sum of %v and %v", a, b)`)
}

func TestEnum(t *testing.T) {
	src := `enum Color { Red, Green, Blue }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type Color int")
	assertContains(t, out, "ColorRed Color = iota")
	assertContains(t, out, "ColorGreen")
	assertContains(t, out, "ColorBlue")
}

func TestEnumMemberExpr(t *testing.T) {
	src := `
enum Color { Red, Green, Blue }
main() {
    c := Color.Red
    print(c)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "ColorRed Color = iota")
	assertContains(t, out, "ColorRed")       // used as value
	assertNotContains(t, out, "Color.Red")   // no dot in emitted Go
}

func TestMatchWithEnumMembers(t *testing.T) {
	src := `
enum Status { Active, Idle, Done }
String describe(Status s) {
    match s {
        case Status.Active => { return "active" }
        case Status.Idle   => { return "idle" }
        case _ => { return "done" }
    }
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "case StatusActive:")
	assertContains(t, out, "case StatusIdle:")
	assertNotContains(t, out, "case 0:")
	assertNotContains(t, out, "Status.Active")
}

func TestMatch(t *testing.T) {
	src := `main() {
		x := 1
		match x {
			case 1 => { print("one") }
			case 2 => { print("two") }
			case _ => { print("other") }
		}
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "switch x {")
	assertContains(t, out, "case 1:")
	assertContains(t, out, "case 2:")
	assertContains(t, out, "default:")
}

func TestTypeMapping(t *testing.T) {
	cases := []struct{ srcType, goType string }{
		{"Int", "int"},
		{"Float", "float64"},
		{"String", "string"},
		{"Bool", "bool"},
	}
	for _, c := range cases {
		src := "f(" + c.srcType + " x) { }"
		out, errs := transpile(src)
		if errs != nil {
			t.Fatal(errs)
		}
		assertContains(t, out, "x "+c.goType)
	}
}

// --- Package system tests ----------------------------------------------------

func transpileWithPackage(src string) string {
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	gen := New()
	return gen.Generate(prog)
}

func TestPackageHeaderDefault(t *testing.T) {
	out := transpileWithPackage(`main() { }`)
	if !strings.HasPrefix(out, "package main\n") {
		t.Errorf("expected 'package main' header, got:\n%s", out)
	}
}

func TestPackageHeaderFromDecl(t *testing.T) {
	out := transpileWithPackage(`package "myapp/utils"
pub Int add(Int a, Int b) { return a }`)
	if !strings.HasPrefix(out, "package utils\n") {
		t.Errorf("expected 'package utils' header, got:\n%s", out)
	}
}

func TestPackageHeaderFromDeclTopLevel(t *testing.T) {
	out := transpileWithPackage(`package "myapp/models"
Dog { String name }`)
	if !strings.HasPrefix(out, "package models\n") {
		t.Errorf("expected 'package models' header, got:\n%s", out)
	}
}

func TestPackageHeaderSingleSegment(t *testing.T) {
	out := transpileWithPackage(`package "myapp"
init() { }`)
	if !strings.HasPrefix(out, "package myapp\n") {
		t.Errorf("expected 'package myapp' header, got:\n%s", out)
	}
}

func TestNewWithRegistrySeeds(t *testing.T) {
	reg := NewTypeRegistry()
	reg.ClassNames["Dog"] = true
	reg.InterfaceNames["Speaker"] = true
	reg.EnumNames["Color"] = true
	reg.CanThrowFns["readFile"] = true

	gen := NewWithRegistry(reg, "models")
	if !gen.classNames["Dog"] {
		t.Error("expected Dog in classNames")
	}
	if !gen.interfaceNames["Speaker"] {
		t.Error("expected Speaker in interfaceNames")
	}
	if !gen.enumNames["Color"] {
		t.Error("expected Color in enumNames")
	}
	if !gen.canThrowFns["readFile"] {
		t.Error("expected readFile in canThrowFns")
	}
	if gen.packageName != "models" {
		t.Errorf("expected packageName 'models', got %q", gen.packageName)
	}
}

func TestNewWithRegistryPackageHeader(t *testing.T) {
	reg := NewTypeRegistry()
	l := lexer.New(`helper() { }`)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()

	gen := NewWithRegistry(reg, "utils")
	out := gen.Generate(prog)
	if !strings.HasPrefix(out, "package utils\n") {
		t.Errorf("expected 'package utils' header from registry, got:\n%s", out)
	}
}

func TestBuildRegistryClassNames(t *testing.T) {
	prog1 := mustParse(t, `Animal { String name }`)
	prog2 := mustParse(t, `Dog : Animal { Int age }`)

	reg := BuildRegistry([]*parser.Program{prog1, prog2})
	if !reg.ClassNames["Animal"] {
		t.Error("expected Animal in registry ClassNames")
	}
	if !reg.ClassNames["Dog"] {
		t.Error("expected Dog in registry ClassNames")
	}
}

func TestBuildRegistryInterfaceAndEnum(t *testing.T) {
	prog := mustParse(t, `
interface Speaker { pub String speak() }
enum Color { Red, Green, Blue }
`)
	reg := BuildRegistry([]*parser.Program{prog})
	if !reg.InterfaceNames["Speaker"] {
		t.Error("expected Speaker in registry InterfaceNames")
	}
	if !reg.EnumNames["Color"] {
		t.Error("expected Color in registry EnumNames")
	}
}

func TestBuildRegistryCanThrowFns(t *testing.T) {
	prog := mustParse(t, `
safe() { }
Int risky() { return Error("oops") }
`)
	reg := BuildRegistry([]*parser.Program{prog})
	if reg.CanThrowFns["safe"] {
		t.Error("safe should NOT be in CanThrowFns")
	}
	if !reg.CanThrowFns["risky"] {
		t.Error("risky should be in CanThrowFns")
	}
}

func TestBuildRegistryMultipleFiles(t *testing.T) {
	prog1 := mustParse(t, `String loadData() { return Error("io") }`)
	prog2 := mustParse(t, `saveData() { }`)
	prog3 := mustParse(t, `interface Reader { pub String read() }`)

	reg := BuildRegistry([]*parser.Program{prog1, prog2, prog3})
	if !reg.CanThrowFns["loadData"] {
		t.Error("loadData should be in CanThrowFns")
	}
	if reg.CanThrowFns["saveData"] {
		t.Error("saveData should NOT be in CanThrowFns")
	}
	if !reg.InterfaceNames["Reader"] {
		t.Error("Reader should be in InterfaceNames")
	}
}

func TestCrossFileTypeResolution(t *testing.T) {
	// Simulate two files in the same package:
	// file1 defines class Dog, file2 uses Dog as a type.
	// With shared registry, Dog is known to be a class (→ *Dog in Go).
	prog1 := mustParse(t, `package "myapp/models"
Dog { String name }`)
	prog2 := mustParse(t, `package "myapp/models"
Dog makeDog() { return Dog() }`)

	reg := BuildRegistry([]*parser.Program{prog1, prog2})

	gen := NewWithRegistry(reg, "models")
	out := gen.Generate(prog2)
	// Dog is a class; in cross-file mode, file2 sees Dog as the interface type
	// Return type is Dog (the interface), constructor is NewDog()
	assertContains(t, out, "Dog")
	assertContains(t, out, "NewDog()")
}

// mustParse is a helper that parses src and fails the test on errors.
func mustParse(t *testing.T, src string) *parser.Program {
	t.Helper()
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		t.Fatalf("parse errors: %v", p.Errors)
	}
	return prog
}

func TestLastSegment(t *testing.T) {
	cases := []struct{ path, want string }{
		{"myapp/utils", "utils"},
		{"myapp/models/sub", "sub"},
		{"myapp", "myapp"},
		{"", ""},
	}
	for _, c := range cases {
		got := lastSegment(c.path)
		if got != c.want {
			t.Errorf("lastSegment(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestLambdaSingleExpr(t *testing.T) {
	src := `main() {
    double := (Int x) => x * 2
    print(double(5))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func(x int) int { return (x * 2) }")
	assertNotContains(t, out, "=>")
}

func TestLambdaNoReturnType(t *testing.T) {
	src := `main() {
    double := (Int x) => x * 2
    print(double(3))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func(x int) int { return")
	assertNotContains(t, out, "=>")
}

func TestLambdaZeroParams(t *testing.T) {
	src := `main() {
    greet := () => "hello"
    print(greet())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func() string { return")
	assertNotContains(t, out, "=>")
}

func TestLambdaBlockBody(t *testing.T) {
	src := `main() {
    classify := (Int x) => {
        if x > 0 {
            return "positive"
        }
        return "non-positive"
    }
    print(classify(5))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func(x int) string {")
	assertContains(t, out, `"positive"`)
	assertContains(t, out, `"non-positive"`)
	assertNotContains(t, out, "=>")
}

func TestLambdaAsArgument(t *testing.T) {
	src := `
Int applyFn(Int val, Any callback) {
    return callback(val)
}
main() {
    result := applyFn(5, (Int x) => x * 3)
    print(result)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func(x int) int { return")
}

func TestLambdaFailableSignature(t *testing.T) {
	src := `main() {
    safeDivide := (Int a, Int b) => {
        if a == 0 {
            return Error("bad input")
        }
        return a / b
    }
    print(safeDivide(10, 2))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Lambda should emit (int, error) return type
	assertContains(t, out, "func(a int, b int) (int, error)")
	// Normal return inside failable lambda should append nil
	assertContains(t, out, "return (a / b), nil")
	// return Error should emit return zero, error
	assertContains(t, out, `return 0, fmt.Errorf`)
}

func TestLambdaFailableAutoPropagate(t *testing.T) {
	src := `main() {
    safeDivide := (Int a, Int b) => {
        if b == 0 {
            return Error("division by zero")
        }
        return a / b
    }
    result := safeDivide(10, 0) or {
        print("caught")
        exit(1)
    }
    print(result)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Failable lambda call should unpack error
	assertContains(t, out, "safeDivide(10, 0)")
	assertContains(t, out, "!= nil")
}

func TestMixedFailableAndNonFailableLambdas(t *testing.T) {
	src := `main() {
    double := (Int x) => x * 2
    safeSqrt := (Int x) => {
        if x < 0 {
            return Error("negative input")
        }
        return x * x
    }
    print(double(4))
    r := safeSqrt(3)
    print(r)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Non-failable lambda must NOT have error return
	assertContains(t, out, "func(x int) int { return (x * 2) }")
	// Failable lambda MUST have error return
	assertContains(t, out, "func(x int) (int, error)")
	// Failable call auto-propagates with error check
	assertContains(t, out, "!= nil")
	// Non-failable call must remain a plain assignment
	assertNotContains(t, out, "_err0 := double(")
}

func TestMultipleFailableCallsInMain(t *testing.T) {
	src := `main() {
    safeDivide := (Int a, Int b) => {
        if b == 0 {
            return Error("division by zero")
        }
        return a / b
    }
    r1 := safeDivide(10, 2)
    print(r1)
    r2 := safeDivide(8, 4)
    print(r2)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Both calls must be unwrapped with error checks (r1, _err := ... not r1 := ...)
	assertContains(t, out, "safeDivide(10, 2)")
	assertContains(t, out, "safeDivide(8, 4)")
	// Must have error unpacking — check for _err variables
	assertContains(t, out, "_err0")
	assertContains(t, out, "_err1")
}

func TestIntegrationStringInterpolationInLambda(t *testing.T) {
	src := `main() {
    makeMsg := (String name) => "Hello, {name}!"
    print(makeMsg("World"))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Interpolation must become Sprintf inside the func literal
	assertContains(t, out, `fmt.Sprintf("Hello, %v!", name)`)
	assertNotContains(t, out, "=>")
}

func TestIntegrationLambdaCapturesOuterVar(t *testing.T) {
	src := `main() {
    base := 100
    addBase := (Int x) => x + base
    print(addBase(5))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Outer variable must appear inside the func literal body
	assertContains(t, out, "func(x int) int { return (x + base) }")
	// base must be declared before the lambda
	assertContains(t, out, "base := 100")
}

func TestFailableLambdaMultipleReturnPaths(t *testing.T) {
	src := `main() {
    classify := (Int x) => {
        if x < 0 {
            return Error("negative")
        }
        if x == 0 {
            return "zero"
        }
        return "positive"
    }
    r := classify(5)
    print(r)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Failable lambda must have (string, error) return
	assertContains(t, out, "func(x int) (string, error)")
	// Both regular returns must have nil appended
	assertContains(t, out, `return "zero", nil`)
	assertContains(t, out, `return "positive", nil`)
	// return Error must emit zero value + error
	assertContains(t, out, `return "", fmt.Errorf`)
	// Failable call auto-propagates
	assertContains(t, out, "!= nil")
}

// --- Default parameters and named arguments ----------------------------------

func TestDefaultAndNamedArgs(t *testing.T) {
	src := `
Dog {
    String name
    Int age
    new(String name, Int age = 0) {
        this.name = name
        this.age = age
    }
}

greet(String name, String greeting = "Hello") {
    print("{greeting}, {name}!")
}

main() {
    d1 := Dog("Rex")
    d2 := Dog("Buddy", 3)
    d3 := Dog(name: "Max")
    d4 := Dog(age: 5, name: "Spot")
    greet("Alice")
    greet("Bob", greeting: "Hi")
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Default age=0 filled in
	assertContains(t, out, `NewDog("Rex", 0)`)
	// Fully explicit
	assertContains(t, out, `NewDog("Buddy", 3)`)
	// Named arg with default filled in
	assertContains(t, out, `NewDog("Max", 0)`)
	// Named args reordered
	assertContains(t, out, `NewDog("Spot", 5)`)
	// Function default filled in (greet is not pub so stays lowercase)
	assertContains(t, out, `greet("Alice", "Hello")`)
	// Named override
	assertContains(t, out, `greet("Bob", "Hi")`)
}

func TestWithStmt(t *testing.T) {
	src := `
main() {
    with (f := openFile("data.txt")) {
        print("reading")
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "f := openFile(\"data.txt\")")
	assertContains(t, out, "if _c, ok := any(f).(io.Closer); ok { defer _c.Close() }")
	assertContains(t, out, "if _l, ok := any(&f).(sync.Locker); ok { _l.Lock(); defer _l.Unlock() } else if _l, ok := any(f).(sync.Locker); ok { _l.Lock(); defer _l.Unlock() }")
	assertContains(t, out, "fmt.Println(\"reading\")")
	assertContains(t, out, `"io"`)
	assertContains(t, out, `"sync"`)
}

func TestWithStmtMultipleResources(t *testing.T) {
	src := `
main() {
    with (src := openFile("in.txt"), dst := createFile("out.txt")) {
        print("copying")
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "src := openFile(\"in.txt\")")
	assertContains(t, out, "if _c, ok := any(src).(io.Closer); ok { defer _c.Close() }")
	assertContains(t, out, "dst := createFile(\"out.txt\")")
	assertContains(t, out, "if _c, ok := any(dst).(io.Closer); ok { defer _c.Close() }")
}

func TestWithStmtThreeResources(t *testing.T) {
	src := `
main() {
    with (a := open("a"), b := open("b"), c := open("c")) {
        print("ok")
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "a := open(\"a\")")
	assertContains(t, out, "if _c, ok := any(a).(io.Closer); ok { defer _c.Close() }")
	assertContains(t, out, "b := open(\"b\")")
	assertContains(t, out, "if _c, ok := any(b).(io.Closer); ok { defer _c.Close() }")
	assertContains(t, out, "c := open(\"c\")")
	assertContains(t, out, "if _c, ok := any(c).(io.Closer); ok { defer _c.Close() }")
}

func TestWithStmtInsideFunction(t *testing.T) {
	src := `
process() {
    with (f := openFile("data.txt")) {
        print("reading")
    }
}
main() { process() }
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func process()")
	assertContains(t, out, "f := openFile(\"data.txt\")")
	assertContains(t, out, "if _c, ok := any(f).(io.Closer); ok { defer _c.Close() }")
}

func TestWithStmtLocker(t *testing.T) {
	src := `
import "sync"
main() {
    mu := sync.Mutex.new()
    with (locked := mu) {
        print("critical section")
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "locked := mu")
	assertContains(t, out, "if _l, ok := any(&locked).(sync.Locker); ok { _l.Lock(); defer _l.Unlock() } else if _l, ok := any(locked).(sync.Locker); ok { _l.Lock(); defer _l.Unlock() }")
}

func TestWithStmtWithOrHandler(t *testing.T) {
	src := `
main() {
    with (f := openFile("x") or {
        print("error")
        exit(1)
    }) {
        print("ok")
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "openFile(\"x\")")
	assertContains(t, out, "if _c, ok := any(f).(io.Closer); ok { defer _c.Close() }")
}

func TestDefaultParamOnly(t *testing.T) {
	src := `
Int add(Int x, Int y = 10) {
    return x + y
}
main() {
    r := add(5)
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "add(5, 10)")
}

func transpileWithTypes(src string) (string, []string) {
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		return "", p.Errors
	}
	typechecker.Check(prog)
	gen := New()
	return gen.Generate(prog), nil
}

func TestTypedMapLiteral(t *testing.T) {
	out, errs := transpileWithTypes(`main() { m := {"a": 1, "b": 2} }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "map[string]int")
	assertNotContains(t, out, "interface{}")
}

func TestTypedListLiteral(t *testing.T) {
	out, errs := transpileWithTypes(`main() { nums := [1, 2, 3] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "[]int{")
}

func TestMixedListFallsBackToAny(t *testing.T) {
	out, errs := transpileWithTypes(`main() { m := [1, "a"] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "[]interface{}{")
}

func TestEmptyMapWithDeclaredType(t *testing.T) {
	out, errs := transpileWithTypes(`main() { Map<String, Int> m = {} }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "map[string]int{")
}

func TestEmptyListWithDeclaredType(t *testing.T) {
	out, errs := transpileWithTypes(`main() { List<Int> l = [] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "[]int{")
}

func TestNestedListLiteral(t *testing.T) {
	out, errs := transpileWithTypes(`main() { m := [[1, 2], [3, 4]] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "[][]int{")
}

func TestConstDecl(t *testing.T) {
	out, errs := transpile(`
pub const Float PI = 3.14
pub const MAX = 100
main() { print(PI) }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "const PI float64 = 3.14")
	assertContains(t, out, "const MAX = 100")
}

// --- Go type .new() with named fields ----------------------------------------

func TestGoTypeNewZeroValue(t *testing.T) {
	out, errs := transpile(`import "sync"
main() { mu := sync.Mutex.new() }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "sync.Mutex{}")
}

func TestGoTypeNewNamedFields(t *testing.T) {
	out, errs := transpile(`import "net/http"
main() { c := http.Client.new(Timeout: 30) }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "http.Client{Timeout: 30}")
}

func TestGoTypeNewMultipleNamedFields(t *testing.T) {
	out, errs := transpile(`import "net/http"
main() { c := http.Client.new(Timeout: 30, MaxIdleConns: 10) }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "http.Client{Timeout: 30, MaxIdleConns: 10}")
}

func TestGoTypeNewSimpleName(t *testing.T) {
	// Non-Zinc, non-dotted type
	out, errs := transpile(`main() { x := Config.new(Port: 8080, Host: "localhost") }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `Config{Port: 8080, Host: "localhost"}`)
}

// --- Index expressions -------------------------------------------------------

func TestIndexExpr(t *testing.T) {
	out, errs := transpile(`main() { x := nums[0] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "nums[0]")
}

func TestIndexAssign(t *testing.T) {
	out, errs := transpile(`main() { nums[1] = 99 }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "nums[1] = 99")
}

func TestMapIndexExpr(t *testing.T) {
	out, errs := transpile(`main() { x := m["key"] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `m["key"]`)
}

// --- Break / Continue --------------------------------------------------------

func TestBreakStmt(t *testing.T) {
	out, errs := transpile(`main() { while true { break } }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "break")
}

func TestContinueStmt(t *testing.T) {
	out, errs := transpile(`main() { for item in items { continue } }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "continue")
}

// --- Slicing -----------------------------------------------------------------

func TestSliceBracketSyntax(t *testing.T) {
	out, errs := transpile(`main() {
	nums := [1, 2, 3, 4, 5]
	print(nums[1:3])
	print(nums[2:])
	print(nums[:3])
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "nums[1:3]")
	assertContains(t, out, "nums[2:]")
	assertContains(t, out, "nums[:3]")
}

func TestSliceMethodSyntax(t *testing.T) {
	out, errs := transpile(`main() {
	nums := [1, 2, 3, 4, 5]
	print(nums.slice(1, 3))
	print(nums.slice(2))
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "nums[1:3]")
	assertContains(t, out, "nums[2:]")
}

func TestSliceStringBracket(t *testing.T) {
	out, errs := transpile(`main() {
	s := "hello"
	print(s[1:4])
	print(s[:3])
	print(s[2:])
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `s[1:4]`)
	assertContains(t, out, `s[:3]`)
	assertContains(t, out, `s[2:]`)
}

func TestSliceStringMethod(t *testing.T) {
	out, errs := transpile(`main() {
	s := "hello"
	print(s.slice(1, 4))
	print(s.slice(2))
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `s[1:4]`)
	assertContains(t, out, `s[2:]`)
}

func transpileWithSourceMap(src, srcFile string) (string, []string) {
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		return "", p.Errors
	}
	gen := New()
	gen.SetSourceFile(srcFile)
	return gen.Generate(prog), nil
}

func TestSourceMapDirectives(t *testing.T) {
	out, errs := transpileWithSourceMap(`main() {
	x := 42
	print(x)
}`, "test.zn")
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "//line test.zn:1")
	assertContains(t, out, "//line test.zn:2")
	assertContains(t, out, "//line test.zn:3")
}

func TestSourceMapDisabledByDefault(t *testing.T) {
	out, errs := transpile(`main() {
	x := 42
	print(x)
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	if strings.Contains(out, "//line") {
		t.Errorf("expected no //line directives when source file not set, got:\n%s", out)
	}
}

func TestSourceMapTopLevelDecls(t *testing.T) {
	out, errs := transpileWithSourceMap(`const PI = 3.14
enum Color { Red, Green }
main() { print(PI) }`, "app.zn")
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "//line app.zn:1")
	assertContains(t, out, "//line app.zn:2")
	assertContains(t, out, "//line app.zn:3")
}

// --- Variadic Functions ------------------------------------------------------

func TestVariadicFnDecl(t *testing.T) {
	out, errs := transpile(`
greet(String prefix, String... names) {
    print(prefix)
}
main() { greet("Hello") }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "names ...string")
}

func TestVariadicMethodDecl(t *testing.T) {
	out, errs := transpile(`
Logger {
    pub log(String... parts) {
        print("logging")
    }
}
main() { l := Logger(); l.log("a", "b") }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "parts ...string")
}

func TestSpreadExpr(t *testing.T) {
	out, errs := transpile(`
Int sum(Int... nums) {
    return 0
}
main() {
    items := [1, 2, 3]
    sum(items...)
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "items...")
}

func TestListAddMultipleArgs(t *testing.T) {
	out, errs := transpile(`
main() {
    items := [1, 2, 3]
    items.add(4, 5, 6)
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "append(items, 4, 5, 6)")
}

func TestListAddSpread(t *testing.T) {
	out, errs := transpile(`
main() {
    items := [1, 2, 3]
    more := [4, 5, 6]
    items.add(more...)
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "append(items, more...)")
}

// --- GoTypeResolver unit tests -----------------------------------------------

func TestGoTypeResolverKnownFuncs(t *testing.T) {
	r := NewGoTypeResolver()

	// os.Open returns (*File, error)
	if !r.ReturnsError("os", "Open") {
		t.Error("expected os.Open to return error")
	}
	// strconv.Atoi returns (int, error) — not in hardcoded list
	if !r.ReturnsError("strconv", "Atoi") {
		t.Error("expected strconv.Atoi to return error")
	}
	// fmt.Println returns (int, error)
	if !r.ReturnsError("fmt", "Println") {
		t.Error("expected fmt.Println to return error")
	}
	// net/http.Get returns (*Response, error)
	if !r.ReturnsError("net/http", "Get") {
		t.Error("expected net/http.Get to return error")
	}
}

func TestGoTypeResolverNonFailable(t *testing.T) {
	r := NewGoTypeResolver()

	// fmt.Sprintf does NOT return error
	if r.ReturnsError("fmt", "Sprintf") {
		t.Error("expected fmt.Sprintf to NOT return error")
	}
	// strings.Contains does NOT return error
	if r.ReturnsError("strings", "Contains") {
		t.Error("expected strings.Contains to NOT return error")
	}
}

func TestGoTypeResolverBadPackage(t *testing.T) {
	r := NewGoTypeResolver()

	// non-existent package should return false, not panic
	if r.ReturnsError("nonexistent/pkg", "Foo") {
		t.Error("expected false for non-existent package")
	}
}

func TestGoTypeResolverBadFunc(t *testing.T) {
	r := NewGoTypeResolver()

	// non-existent function in valid package
	if r.ReturnsError("os", "NonExistentFunc") {
		t.Error("expected false for non-existent function")
	}
}

// --- Auto-detection integration in codegen -----------------------------------

func TestAutoDetectStrconvAtoi(t *testing.T) {
	out, errs := transpile(`
import "strconv"

main() {
    n := strconv.Atoi("42") or { print("fail"); halt }
    print(n)
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	// Should generate multi-return unpacking
	assertContains(t, out, "strconv.Atoi")
	assertContains(t, out, "!= nil")
}

func TestAutoDetectJsonMarshal(t *testing.T) {
	out, errs := transpile(`
import "encoding/json"

main() {
    data := json.Marshal("hello") or { print("fail"); halt }
    print(data)
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "json.Marshal")
	assertContains(t, out, "!= nil")
}

// --- Phase 1 fixes: DeferStmt, RawStringLit, MatchStmt failable detection ---

func TestDeferStmt(t *testing.T) {
	out, errs := transpile(`
import "fmt"
main() {
    defer fmt.Println("goodbye")
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `defer fmt.Println("goodbye")`)
}

func TestRawStringLit(t *testing.T) {
	out, errs := transpile("main() { s := `hello\\nworld` }")
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "`hello\\nworld`")
}

func TestMatchStmtFailable(t *testing.T) {
	out, errs := transpile(`
String classify(Int x) {
    match x {
        case 1 => { return Error("bad") }
        case 2 => { return "two" }
    }
    return "other"
}`)
	if errs != nil {
		t.Fatal(errs)
	}
	// Function should be detected as failable — returns (string, error)
	assertContains(t, out, "error")
}

func TestMethodFailableDetection(t *testing.T) {
	src := `
import "os"

String doWrite() {
    f := os.Open("test.txt")
    f.WriteString("hello")
    return "done"
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// f.WriteString returns (int, error) — should be detected as failable
	// The function should auto-propagate the error → returns (string, error)
	assertContains(t, out, "error")
	assertContains(t, out, "WriteString")
}

func TestMethodVoidFailableDetection(t *testing.T) {
	src := `
import "os"

doClose() {
    f := os.Open("test.txt")
    f.Close()
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// f.Close() returns just error — should be detected as void failable
	assertContains(t, out, "error")
	// Void failable uses: if _errN := f.Close(); _errN != nil {
	assertContains(t, out, "f.Close()")
}

// --- Lambda shorthand ---

func TestLambdaShorthandSingleParam(t *testing.T) {
	src := `main() {
	double := x => x * 2
	print(double(5))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func(x interface{})")
	assertNotContains(t, out, "=>")
}

func TestLambdaShorthandParens(t *testing.T) {
	src := `main() {
	add := (a, b) => a + b
	print(add(1, 2))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func(a interface{}, b interface{})")
	assertNotContains(t, out, "=>")
}

func TestLambdaShorthandMixedTyped(t *testing.T) {
	// Typed lambda syntax
	src := `main() {
	double := (Int x) => x * 2
	print(double(5))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func(x int) int")
}

// --- Collection methods ---

func TestWhereSimple(t *testing.T) {
	src := `main() {
	nums := [1, 2, 3, 4, 5, 6]
	evens := nums.Where(x => x % 2 == 0)
	print(evens)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "for _, _v")
	assertContains(t, out, "% 2) == 0")
	assertContains(t, out, "append(evens")
}

func TestSelectSimple(t *testing.T) {
	src := `main() {
	nums := [1, 2, 3]
	doubled := nums.Select(x => x * 2)
	print(doubled)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "for _, _v")
	assertContains(t, out, "* 2")
	assertContains(t, out, "append(doubled")
}

func TestForEachSimple(t *testing.T) {
	src := `main() {
	nums := [1, 2, 3]
	nums.ForEach((Int x) => x + 1)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "for _, _v")
}

func TestWhereSelectChain(t *testing.T) {
	src := `main() {
	nums := [1, 2, 3, 4, 5, 6]
	result := nums.Where(x => x > 3).Select(x => x * 10)
	print(result)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Should be a single fused loop, not nested loops
	assertContains(t, out, "for _, _v")
	assertContains(t, out, "> 3")
	assertContains(t, out, "* 10")
	assertContains(t, out, "append(result")
}

func TestAnyTerminal(t *testing.T) {
	src := `main() {
	nums := [1, 2, 3, 4, 5]
	hasEven := nums.Any(x => x % 2 == 0)
	print(hasEven)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "hasEven := false")
	assertContains(t, out, "hasEven = true")
	assertContains(t, out, "break")
}

func TestAggregateTerminal(t *testing.T) {
	src := `main() {
	nums := [1, 2, 3, 4, 5]
	sum := nums.Aggregate(0, (acc, x) => acc + x)
	print(sum)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "sum := 0")
	assertContains(t, out, "sum = (sum")
}

func TestWhereForEachChain(t *testing.T) {
	src := `main() {
	nums := [1, 2, 3, 4, 5, 6]
	nums.Where(x => x % 2 == 0).ForEach((Int x) => x + 1)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "for _, _v")
	assertContains(t, out, "% 2) == 0")
}

func TestTakeSimple(t *testing.T) {
	src := `main() {
	nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
	first3 := nums.Take(3)
	print(first3)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "break")
	assertContains(t, out, "append(first3")
}

func TestCountTerminal(t *testing.T) {
	src := `main() {
	nums := [1, 2, 3, 4, 5, 6]
	evenCount := nums.Where(x => x % 2 == 0).Count()
	print(evenCount)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "evenCount := 0")
	assertContains(t, out, "evenCount++")
}

func TestSelectWithFailableLambda(t *testing.T) {
	src := `
Int safeDivide(Int x) {
	if x == 0 { return Error("zero") }
	return 100 / x
}
main() {
	nums := [2, 5]
	result := nums.Select(x => safeDivide(x))
	print(result)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Should emit failable call with error check inside the loop
	assertContains(t, out, "_fv")
	assertContains(t, out, "_err")
	assertContains(t, out, "!= nil")
}

func TestWhereWithFailableLambda(t *testing.T) {
	src := `
Bool isValid(Int x) {
	if x < 0 { return Error("negative") }
	return x > 3
}
main() {
	nums := [1, 4, 5]
	result := nums.Where(x => isValid(x))
	print(result)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Should emit failable call with error check, then use result in if
	assertContains(t, out, "_fv")
	assertContains(t, out, "_err")
	assertContains(t, out, "if _fv")
}
