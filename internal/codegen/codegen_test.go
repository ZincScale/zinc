package codegen

import (
	"strings"
	"testing"

	"growl/internal/lexer"
	"growl/internal/parser"
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
	out, errs := transpile(`fn main() { print("Hello, World!") }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func main()")
	assertContains(t, out, `fmt.Println("Hello, World!")`)
	assertContains(t, out, `"fmt"`)
}

func TestVarDecl(t *testing.T) {
	out, errs := transpile(`fn main() { var x: Int = 42 }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "x := 42")
}

func TestBinaryExpr(t *testing.T) {
	out, errs := transpile(`fn main() { var x: Int = 1 + 2 }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "(1 + 2)")
}

func TestIfElse(t *testing.T) {
	out, errs := transpile(`fn main() { if (x) { print("yes") } else { print("no") } }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "if x {")
	assertContains(t, out, "} else {")
}

func TestWhile(t *testing.T) {
	out, errs := transpile(`fn main() { while (true) { } }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "for true {")
}

func TestForIn(t *testing.T) {
	out, errs := transpile(`fn main() { for item in items { } }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "for _, item := range items {")
}

func TestForCStyle(t *testing.T) {
	out, errs := transpile(`fn main() { for (var i: Int = 0; i; i) { } }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "for i := 0; i; i {")
}

func TestClass(t *testing.T) {
	src := `class Dog {
		var name: String
		construct new(n: String) {
			this.name = n
		}
		pub fn bark(): String { return "woof" }
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type Dog struct {")
	assertContains(t, out, "Name string")
	assertContains(t, out, "func NewDog(n string) *Dog {")
	assertContains(t, out, "func (d *Dog) Bark() string {")
}

func TestInterface(t *testing.T) {
	src := `interface Speaker {
		pub fn speak(): String
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type Speaker interface {")
	assertContains(t, out, "Speak() string")
}

func TestInterfaceComplianceCheck(t *testing.T) {
	src := `interface Speaker { pub fn speak(): String }
	class Dog : Speaker {
		pub fn speak(): String { return "woof" }
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "var _ Speaker = (*Dog)(nil)")
}

func TestTryCatch(t *testing.T) {
	src := `fn risky(): String {
		throw Error("oops")
	}
	fn main() {
		try {
			var r: String = risky()
		} catch (err) {
			print("caught")
		}
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "fmt.Errorf")
	assertContains(t, out, "if err != nil")
}

func TestConcurrency(t *testing.T) {
	src := `fn main() {
		var ch: Chan<Int> = Chan.new(1)
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
	fn main() { }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `"os"`)
}

func TestListLiteral(t *testing.T) {
	src := `fn main() { var x: Any = [1, 2, 3] }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "[]interface{}{1, 2, 3}")
}

func TestMapLiteral(t *testing.T) {
	src := `fn main() { var m: Any = {"a": 1} }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `map[interface{}]interface{}`)
}

func TestPubFnExported(t *testing.T) {
	src := `pub fn Greet(): String { return "hi" }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func Greet() string {")
}

func TestStaticMethod(t *testing.T) {
	src := `class Math {
		pub static fn square(n: Int): Int { return n }
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func Math_Square(n int) int {")
}

func TestBuiltinLen(t *testing.T) {
	src := `fn main() { var n: Int = len(items) }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "len(items)")
}

func TestBuiltinToString(t *testing.T) {
	src := `fn main() { var s: String = toString(42) }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `fmt.Sprintf("%v", 42)`)
	assertContains(t, out, `"fmt"`)
}

func TestBuiltinStrUpper(t *testing.T) {
	src := `fn main() { var s: String = strUpper("hello") }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `strings.ToUpper("hello")`)
	assertContains(t, out, `"strings"`)
}

func TestBuiltinSortInts(t *testing.T) {
	src := `fn main() { sortInts(nums) }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "sort.Ints(nums)")
	assertContains(t, out, `"sort"`)
}

func TestBuiltinSqrt(t *testing.T) {
	src := `fn main() { var r: Float = sqrt(9.0) }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "math.Sqrt(9.0)")
	assertContains(t, out, `"math"`)
}

func TestTupleUnpack(t *testing.T) {
	src := `fn main() { var (a, b) = getPair() }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "a, b := getPair()")
}

func TestTupleUnpackThree(t *testing.T) {
	src := `fn main() { var (x, y, z) = getTriple() }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "x, y, z := getTriple()")
}

func TestGenericFn(t *testing.T) {
	src := `fn identity<T>(val: T): T { return val }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func identity[T any](val T) T {")
}

func TestGenericFnMultiParam(t *testing.T) {
	src := `fn pair<K, V>(key: K, val: V): K { return key }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func pair[K any, V any](key K, val V) K {")
}

func TestGenericClass(t *testing.T) {
	src := `class Box<T> { var value: T }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type Box[T any] struct {")
}

func TestOptionalType(t *testing.T) {
	src := `fn greet(name: String?): String { return "hi" }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "name *string")
}

func TestOptionalTypeVar(t *testing.T) {
	src := `fn main() { var x: Int? }`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "var x *int")
}

func TestStringInterpolation(t *testing.T) {
	src := `fn main() { var name: String = "World"
		print("Hello, {name}!")
	}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `fmt.Sprintf("Hello, %v!", name)`)
}

func TestStringInterpolationMultiple(t *testing.T) {
	src := `fn main() { var a: Int = 1
		var b: Int = 2
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
	assertContains(t, out, "Red Color = iota")
	assertContains(t, out, "Green")
	assertContains(t, out, "Blue")
}

func TestMatch(t *testing.T) {
	src := `fn main() {
		var x: Int = 1
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
	cases := []struct{ growl, goType string }{
		{"Int", "int"},
		{"Float", "float64"},
		{"String", "string"},
		{"Bool", "bool"},
	}
	for _, c := range cases {
		src := "fn f(x: " + c.growl + ") { }"
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
	out := transpileWithPackage(`fn main() { }`)
	if !strings.HasPrefix(out, "package main\n") {
		t.Errorf("expected 'package main' header, got:\n%s", out)
	}
}

func TestPackageHeaderFromDecl(t *testing.T) {
	out := transpileWithPackage(`package "myapp/utils"
pub fn add(a: Int, b: Int): Int { return a }`)
	if !strings.HasPrefix(out, "package utils\n") {
		t.Errorf("expected 'package utils' header, got:\n%s", out)
	}
}

func TestPackageHeaderFromDeclTopLevel(t *testing.T) {
	out := transpileWithPackage(`package "myapp/models"
class Dog { var name: String }`)
	if !strings.HasPrefix(out, "package models\n") {
		t.Errorf("expected 'package models' header, got:\n%s", out)
	}
}

func TestPackageHeaderSingleSegment(t *testing.T) {
	out := transpileWithPackage(`package "myapp"
fn init() { }`)
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
	l := lexer.New(`fn helper() { }`)
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
	prog1 := mustParse(t, `class Animal { var name: String }`)
	prog2 := mustParse(t, `class Dog : Animal { var age: Int }`)

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
interface Speaker { pub fn speak(): String }
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
fn safe() { }
fn risky() { throw Error("oops") }
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
	prog1 := mustParse(t, `fn readFile() { throw Error("io") }`)
	prog2 := mustParse(t, `fn writeFile() { }`)
	prog3 := mustParse(t, `interface Reader { pub fn read(): String }`)

	reg := BuildRegistry([]*parser.Program{prog1, prog2, prog3})
	if !reg.CanThrowFns["readFile"] {
		t.Error("readFile should be in CanThrowFns")
	}
	if reg.CanThrowFns["writeFile"] {
		t.Error("writeFile should NOT be in CanThrowFns")
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
class Dog { var name: String }`)
	prog2 := mustParse(t, `package "myapp/models"
fn makeDog(): Dog { return Dog.new() }`)

	reg := BuildRegistry([]*parser.Program{prog1, prog2})

	gen := NewWithRegistry(reg, "models")
	out := gen.Generate(prog2)
	// Dog is a class, so return type should be *Dog
	assertContains(t, out, "*Dog")
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
