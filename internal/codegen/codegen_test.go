package codegen

import (
	"strings"
	"testing"

	"growler/internal/lexer"
	"growler/internal/parser"
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
	out, errs := transpile(`fn main() { var x: Any = [1, 2, 3] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "[]int{1, 2, 3}")
}

func TestListLiteralStrings(t *testing.T) {
	out, errs := transpile(`fn main() { var x: Any = ["a", "b"] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, `[]string{"a", "b"}`)
}

func TestListLiteralEmpty(t *testing.T) {
	out, errs := transpile(`fn main() { var x: Any = [] }`)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "[]interface{}{}")
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
	assertContains(t, out, "ColorRed Color = iota")
	assertContains(t, out, "ColorGreen")
	assertContains(t, out, "ColorBlue")
}

func TestEnumMemberExpr(t *testing.T) {
	src := `
enum Color { Red, Green, Blue }
fn main() {
    var c: Color = Color.Red
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
fn describe(s: Status): String {
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

func TestLambdaSingleExpr(t *testing.T) {
	src := `fn main() {
    var double = (x: Int): Int => x * 2
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
	src := `fn main() {
    var double = (x: Int) => x * 2
    print(double(3))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func(x int) interface{} { return")
	assertNotContains(t, out, "=>")
}

func TestLambdaZeroParams(t *testing.T) {
	src := `fn main() {
    var greet = (): String => "hello"
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
	src := `fn main() {
    var classify = (x: Int): String => {
        if (x > 0) {
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
fn applyFn(val: Int, callback: Any): Int {
    return callback(val)
}
fn main() {
    var result = applyFn(5, (x: Int): Int => x * 3)
    print(result)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func(x int) int { return")
}

func TestLambdaThrowSignature(t *testing.T) {
	src := `fn main() {
    var safeDivide = (a: Int, b: Int): Int => {
        if (a == 0) {
            throw Error("bad input")
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
	// Normal return inside throwing lambda should append nil
	assertContains(t, out, "return (a / b), nil")
	// throw should emit return zero, error
	assertContains(t, out, `return 0, fmt.Errorf`)
}

func TestLambdaThrowCaughtByTry(t *testing.T) {
	src := `fn main() {
    var safeDivide = (a: Int, b: Int): Int => {
        if (b == 0) {
            throw Error("division by zero")
        }
        return a / b
    }
    try {
        var result = safeDivide(10, 0)
        print(result)
    } catch(err) {
        print("caught")
    }
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Try body should unpack the error from the throwing lambda call
	assertContains(t, out, "_err := safeDivide(10, 0)")
	assertContains(t, out, "if _err != nil { return _err }")
	assertNotContains(t, out, "result := safeDivide") // must NOT be a plain assignment
}

func TestIntegrationMixedThrowingAndNonThrowingLambdas(t *testing.T) {
	src := `fn main() {
    var double = (x: Int): Int => x * 2
    var safeSqrt = (x: Int): Int => {
        if (x < 0) {
            throw Error("negative input")
        }
        return x * x
    }
    print(double(4))
    try {
        var r = safeSqrt(3)
        print(r)
    } catch(err) {
        print("caught: {err}")
    }
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Non-throwing lambda must NOT have error return
	assertContains(t, out, "func(x int) int { return (x * 2) }")
	// Throwing lambda MUST have error return
	assertContains(t, out, "func(x int) (int, error)")
	// Try block must unwrap the throwing lambda call
	assertContains(t, out, "_err := safeSqrt(3)")
	// Non-throwing call must remain a plain assignment
	assertNotContains(t, out, "_err := double(")
}

func TestIntegrationMultipleThrowingCallsInTry(t *testing.T) {
	src := `fn main() {
    var safeDivide = (a: Int, b: Int): Int => {
        if (b == 0) {
            throw Error("division by zero")
        }
        return a / b
    }
    try {
        var r1 = safeDivide(10, 2)
        print(r1)
        var r2 = safeDivide(8, 4)
        print(r2)
    } catch(err) {
        print("caught: {err}")
    }
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Both calls must be unwrapped — plain assignments must not appear
	assertContains(t, out, "_err := safeDivide(10, 2)")
	assertContains(t, out, "_err := safeDivide(8, 4)")
	assertNotContains(t, out, "r1 := safeDivide")
	assertNotContains(t, out, "r2 := safeDivide")
}

func TestIntegrationStringInterpolationInLambda(t *testing.T) {
	src := `fn main() {
    var makeMsg = (name: String): String => "Hello, {name}!"
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
	src := `fn main() {
    var base = 100
    var addBase = (x: Int): Int => x + base
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

func TestIntegrationThrowingLambdaMultipleReturnPaths(t *testing.T) {
	src := `fn main() {
    var classify = (x: Int): String => {
        if (x < 0) {
            throw Error("negative")
        }
        if (x == 0) {
            return "zero"
        }
        return "positive"
    }
    try {
        var r = classify(5)
        print(r)
    } catch(err) {
        print("caught: {err}")
    }
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Throwing lambda must have (string, error) return
	assertContains(t, out, "func(x int) (string, error)")
	// Both regular returns must have nil appended
	assertContains(t, out, `return "zero", nil`)
	assertContains(t, out, `return "positive", nil`)
	// Throw must emit zero value + error
	assertContains(t, out, `return "", fmt.Errorf`)
	// Try block must unwrap the call
	assertContains(t, out, "_err := classify(5)")
}

// --- Default parameters and named arguments ----------------------------------

func TestDefaultAndNamedArgs(t *testing.T) {
	src := `
class Dog {
    var name: String
    var age: Int
    construct new(name: String, age: Int = 0) {
        this.name = name
        this.age = age
    }
}

fn greet(name: String, greeting: String = "Hello") {
    print("{greeting}, {name}!")
}

fn main() {
    var d1 = Dog.new("Rex")
    var d2 = Dog.new("Buddy", 3)
    var d3 = Dog.new(name: "Max")
    var d4 = Dog.new(age: 5, name: "Spot")
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
fn main() {
    with var f = openFile("data.txt") {
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
	assertContains(t, out, "if _l, ok := any(f).(sync.Locker); ok { _l.Lock(); defer _l.Unlock() }")
	assertContains(t, out, "fmt.Println(\"reading\")")
	assertContains(t, out, `"io"`)
	assertContains(t, out, `"sync"`)
}

func TestWithStmtMultipleResources(t *testing.T) {
	src := `
fn main() {
    with var src = openFile("in.txt"), var dst = createFile("out.txt") {
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
fn main() {
    with var a = open("a"), var b = open("b"), var c = open("c") {
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
fn process() {
    with var f = openFile("data.txt") {
        print("reading")
    }
}
fn main() { process() }
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
fn main() {
    var mu = sync.Mutex{}
    with var locked = mu {
        print("critical section")
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "locked := mu")
	assertContains(t, out, "if _l, ok := any(locked).(sync.Locker); ok { _l.Lock(); defer _l.Unlock() }")
}

func TestWithStmtNestedInTry(t *testing.T) {
	src := `
fn main() {
    try {
        with var f = openFile("x") {
            print("ok")
        }
    } catch(err) {
        print("error")
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "f := openFile(\"x\")")
	assertContains(t, out, "if _c, ok := any(f).(io.Closer); ok { defer _c.Close() }")
	assertContains(t, out, "func() error")
}

func TestDefaultParamOnly(t *testing.T) {
	src := `
fn add(x: Int, y: Int = 10): Int {
    return x + y
}
fn main() {
    var r = add(5)
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "add(5, 10)")
}
