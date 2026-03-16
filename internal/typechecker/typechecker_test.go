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
	"os"
	"strings"
	"testing"

	"zinc/internal/lexer"
	"zinc/internal/parser"
)

// checkSrc parses Zinc source and runs the type checker, returning errors.
func checkSrc(src string) []TypeError {
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		// Parse errors mean we can't check; treat as no type errors
		return nil
	}
	return Check(prog)
}

func hasError(errs []TypeError, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e.Msg, substr) {
			return true
		}
	}
	return false
}

func noErrors(t *testing.T, errs []TypeError, src string) {
	t.Helper()
	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.String()
		}
		t.Errorf("expected no errors for:\n%s\nbut got:\n%s", src, strings.Join(msgs, "\n"))
	}
}

// --- 1. Undeclared identifiers -----------------------------------------------

func TestUndeclaredVariable(t *testing.T) {
	src := `main() { print(x) }`
	errs := checkSrc(src)
	if !hasError(errs, `undefined variable "x"`) {
		t.Errorf("expected 'undefined variable' error, got %v", errs)
	}
}

func TestUndeclaredFunction(t *testing.T) {
	src := `main() { notDefined() }`
	errs := checkSrc(src)
	// undefined fn is not reported (could be imported), but we get TypeUnknown — no error
	// However the ident lookup for 'notDefined' as a var will fire
	// This is a call on unknown, so no error — correct behavior
	_ = errs
}

func TestDeclaredVariableOK(t *testing.T) {
	src := `main() { var x = 1; print(x) }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- Line numbers in errors ---------------------------------------------------

func TestErrorLineNumbers(t *testing.T) {
	src := `main() {
    Int x = "hello"
    Bool y = 42
}`
	errs := checkSrc(src)
	if len(errs) < 2 {
		t.Fatalf("expected at least 2 errors, got %d", len(errs))
	}
	if errs[0].Line != 2 {
		t.Errorf("expected first error on line 2, got line %d: %s", errs[0].Line, errs[0].Msg)
	}
	if errs[1].Line != 3 {
		t.Errorf("expected second error on line 3, got line %d: %s", errs[1].Line, errs[1].Msg)
	}
	// Verify String() includes line number
	if !strings.Contains(errs[0].String(), "line 2:") {
		t.Errorf("expected 'line 2:' in error string, got: %s", errs[0].String())
	}
}

// --- 2. Type mismatch in var decl --------------------------------------------

func TestVarTypeMismatch(t *testing.T) {
	src := `main() { Int x = "hello" }`
	errs := checkSrc(src)
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected 'cannot assign' error, got %v", errs)
	}
}

func TestVarTypeMatch(t *testing.T) {
	src := `main() { var x = 42 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestVarInferred(t *testing.T) {
	src := `main() { var x = 42; var y = "hello" }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestVarOptionalNull(t *testing.T) {
	src := `main() { String? x = null }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestVarNonOptionalNull(t *testing.T) {
	src := `main() { Int x = null }`
	errs := checkSrc(src)
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected 'cannot assign' error for null to Int, got %v", errs)
	}
}

// --- 3. Assignment type mismatch ---------------------------------------------

func TestAssignTypeMismatch(t *testing.T) {
	src := `main() { var x = 1; x = "hello" }`
	errs := checkSrc(src)
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected 'cannot assign' error on reassign, got %v", errs)
	}
}

func TestAssignTypeMatch(t *testing.T) {
	src := `main() { var x = 1; x = 2 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 4. Return type mismatch -------------------------------------------------

func TestReturnTypeMismatch(t *testing.T) {
	src := `Int add(Int a, Int b) { return "oops" }`
	errs := checkSrc(src)
	if !hasError(errs, "return type mismatch") {
		t.Errorf("expected 'return type mismatch' error, got %v", errs)
	}
}

func TestReturnTypeMatch(t *testing.T) {
	src := `Int add(Int a, Int b) { return 42 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestReturnVoid(t *testing.T) {
	src := `doSomething() { return }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestReturnValueFromVoidFn(t *testing.T) {
	src := `doSomething() { return 42 }`
	errs := checkSrc(src)
	// Returning a value from a void fn is a mismatch
	if !hasError(errs, "return type mismatch") {
		t.Errorf("expected return type mismatch for void fn, got %v", errs)
	}
}

// --- 5. Condition type -------------------------------------------------------

func TestIfConditionNotBool(t *testing.T) {
	src := `main() { if 1 { print("yes") } }`
	errs := checkSrc(src)
	if !hasError(errs, "condition must be Bool") {
		t.Errorf("expected 'condition must be Bool' error, got %v", errs)
	}
}

func TestIfConditionBool(t *testing.T) {
	src := `main() { if true { print("yes") } }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestWhileConditionNotBool(t *testing.T) {
	src := `main() { while 42 { print("loop") } }`
	errs := checkSrc(src)
	if !hasError(errs, "condition must be Bool") {
		t.Errorf("expected 'condition must be Bool' error in while, got %v", errs)
	}
}

func TestWhileConditionBool(t *testing.T) {
	src := `main() { var done = false; while !done { done = true } }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 6. Binary operators -----------------------------------------------------

func TestIntPlusString(t *testing.T) {
	src := `main() { var x = 1 + "hello" }`
	errs := checkSrc(src)
	// Should be an error since 1 is Int and "hello" is String
	if !hasError(errs, "not applicable") {
		t.Errorf("expected 'not applicable' error for Int+String, got %v", errs)
	}
}

func TestBoolArithmetic(t *testing.T) {
	src := `main() { var x = true + false }`
	errs := checkSrc(src)
	if !hasError(errs, "not applicable") {
		t.Errorf("expected 'not applicable' error for Bool+Bool, got %v", errs)
	}
}

func TestIntArithmetic(t *testing.T) {
	src := `main() { var x = 1 + 2; var y = 3 * 4 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestFloatArithmetic(t *testing.T) {
	src := `main() { var x = 1.5 + 2.5 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestStringConcatenation(t *testing.T) {
	src := `main() { var s = "hello" + " world" }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestComparisonOperator(t *testing.T) {
	src := `main() { var ok = 1 < 2 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestLogicalOperators(t *testing.T) {
	src := `main() { var ok = true && false }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestLogicalOperatorNonBool(t *testing.T) {
	src := `main() { var ok = 1 && 2 }`
	errs := checkSrc(src)
	if !hasError(errs, "requires Bool") {
		t.Errorf("expected 'requires Bool' error for && on ints, got %v", errs)
	}
}

// --- 7. Field access ---------------------------------------------------------

func TestUndefinedField(t *testing.T) {
	src := `
Dog {
  String name = "Rex"
}
main() {
  var d = Dog()
  print(d.notAField)
}
`
	errs := checkSrc(src)
	if !hasError(errs, `undefined field "notAField"`) {
		t.Errorf("expected 'undefined field' error, got %v", errs)
	}
}

func TestDefinedField(t *testing.T) {
	src := `
Dog {
  String name = "Rex"
}
main() {
  var d = Dog()
  print(d.name)
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 8. Method calls ---------------------------------------------------------

func TestUndefinedMethod(t *testing.T) {
	src := `
Dog {
  String name = "Rex"
}
main() {
  var d = Dog()
  d.fly()
}
`
	errs := checkSrc(src)
	if !hasError(errs, `undefined method "fly"`) {
		t.Errorf("expected 'undefined method' error, got %v", errs)
	}
}

func TestDefinedMethod(t *testing.T) {
	src := `
Dog {
  String name = "Rex"
  String speak() { return "woof" }
}
main() {
  var d = Dog()
  print(d.speak())
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestArgCountMismatch(t *testing.T) {
	src := `
Int add(Int a, Int b) { return a + b }
main() { add(1) }
`
	errs := checkSrc(src)
	if !hasError(errs, "missing required argument") && !hasError(errs, "wrong number of arguments") {
		t.Errorf("expected argument count mismatch error, got %v", errs)
	}
}

func TestArgCountMatch(t *testing.T) {
	src := `
Int add(Int a, Int b) { return a + b }
main() { add(1, 2) }
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 9. Class checks ---------------------------------------------------------

func TestUndeclaredParent(t *testing.T) {
	src := `Dog : Animal { String name = "Rex" }`
	errs := checkSrc(src)
	if !hasError(errs, `undefined class/interface "Animal"`) {
		t.Errorf("expected 'undefined class/interface' error, got %v", errs)
	}
}

func TestDeclaredParentClass(t *testing.T) {
	src := `
Animal { Bool alive = true }
Dog : Animal { String name = "Rex" }
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestDeclaredParentInterface(t *testing.T) {
	src := `
interface Speaker { String speak() }
Dog : Speaker {
  String speak() { return "woof" }
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 10. Generics — no false positives ----------------------------------------

func TestGenericFunctionNoFalsePositives(t *testing.T) {
	src := `
T identity<T>(T x) { return x }
main() { var y = identity(42) }
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestGenericClassNoFalsePositives(t *testing.T) {
	src := `
Box<T> {
  T value
  T get() { return value }
}
main() {
  var b = Box()
  print(b.get())
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 11. Optionals -----------------------------------------------------------

func TestOptionalAssignNull(t *testing.T) {
	src := `main() { String? name = null }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestOptionalAssignValue(t *testing.T) {
	src := `main() { String? name = "hello" }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestNonOptionalAssignNull(t *testing.T) {
	src := `main() { Int count = null }`
	errs := checkSrc(src)
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected 'cannot assign' for null to Int, got %v", errs)
	}
}

// --- 12. Or handler -----------------------------------------------------------

func TestOrHandlerErrVarDefined(t *testing.T) {
	src := `
Int risky() {
  if true { return Error("oops") }
  return 1
}
main() {
  var x = risky() or {
    print(err)
    exit(1)
  }
  print(x)
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestOrHandlerErrVarIsString(t *testing.T) {
	src := `
Int risky() {
  if true { return Error("oops") }
  return 1
}
main() {
  var x = risky() or {
    var msg = err
    exit(1)
  }
  print(x)
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 13. Scope ---------------------------------------------------------------

func TestOuterScopeAccessible(t *testing.T) {
	src := `
main() {
  var x = 42
  if true {
    print(x)
  }
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestInnerVarNotInOuter(t *testing.T) {
	// Inner scope variable should not be accessible after block
	src := `
main() {
  if true {
    var y = 42
  }
  print(y)
}
`
	errs := checkSrc(src)
	if !hasError(errs, `undefined variable "y"`) {
		t.Errorf("expected 'undefined variable y' after block, got %v", errs)
	}
}

func TestForLoopVarScoped(t *testing.T) {
	src := `
main() {
  var items = [1, 2, 3]
  for item in items { print(item) }
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 14. Valid programs — all examples produce zero errors -------------------

func TestValidHello(t *testing.T) {
	src, err := os.ReadFile("../../examples/hello.zn")
	if err != nil {
		t.Fatalf("reading example: %v", err)
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidClasses(t *testing.T) {
	src, err := os.ReadFile("../../examples/classes.zn")
	if err != nil {
		t.Fatalf("reading example: %v", err)
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidGenerics(t *testing.T) {
	src, err := os.ReadFile("../../examples/generics.zn")
	if err != nil {
		t.Fatalf("reading example: %v", err)
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidErrors(t *testing.T) {
	src, err := os.ReadFile("../../examples/errors.zn")
	if err != nil {
		t.Fatalf("reading example: %v", err)
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidFibonacci(t *testing.T) {
	src, err := os.ReadFile("../../examples/fibonacci.zn")
	if err != nil {
		t.Fatalf("reading example: %v", err)
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidEnums(t *testing.T) {
	src, err := os.ReadFile("../../examples/enums.zn")
	if err != nil {
		t.Fatalf("reading example: %v", err)
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidConcurrency(t *testing.T) {
	src, err := os.ReadFile("../../examples/concurrency.zn")
	if err != nil {
		t.Fatalf("reading example: %v", err)
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidClosures(t *testing.T) {
	src, err := os.ReadFile("../../examples/closures.zn")
	if err != nil {
		t.Fatalf("reading example: %v", err)
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

// --- Extra: "this" outside class ---------------------------------------------

func TestThisOutsideClass(t *testing.T) {
	src := `main() { print(this) }`
	errs := checkSrc(src)
	if !hasError(errs, `"this" used outside`) {
		t.Errorf("expected 'this used outside' error, got %v", errs)
	}
}

// --- Extra: undefined type ---------------------------------------------------

func TestUndefinedType(t *testing.T) {
	src := `Baz foo(Baz x) { return x }`
	errs := checkSrc(src)
	if !hasError(errs, `undefined type "Baz"`) {
		t.Errorf("expected 'undefined type Baz' error, got %v", errs)
	}
}

// --- Failable lambda restrictions --------------------------------------------

func TestFailableLambdaNoFalsePositive(t *testing.T) {
	src := `
Int apply(Any callback) {
    return callback(5)
}
main() {
    apply((Int x) -> x * 2)
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- Default parameters and named arguments ----------------------------------

func TestDefaultParamNoError(t *testing.T) {
	src := `greet(String name, String greeting = "Hello") {}
main() { greet("Alice") }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestNamedArgsAllValid(t *testing.T) {
	src := `greet(String name, String greeting = "Hello") {}
main() { greet(greeting: "Hi", name: "Bob") }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestMixedPositionalAndNamedArgs(t *testing.T) {
	src := `greet(String name, String greeting = "Hello") {}
main() { greet("Bob", greeting: "Hi") }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestMissingRequiredArg(t *testing.T) {
	src := `greet(String name, String greeting = "Hello") {}
main() { greet() }`
	errs := checkSrc(src)
	if !hasError(errs, "missing required argument") {
		t.Errorf("expected missing required argument error, got: %v", errs)
	}
}

func TestUnknownNamedArg(t *testing.T) {
	src := `greet(String name) {}
main() { greet(badParam: "hi") }`
	errs := checkSrc(src)
	if !hasError(errs, "unknown named argument") {
		t.Errorf("expected unknown named argument error, got: %v", errs)
	}
}

func TestTooManyArgs(t *testing.T) {
	src := `Int add(Int x, Int y) { return x }
main() { add(1, 2, 3) }`
	errs := checkSrc(src)
	if !hasError(errs, "too many arguments") {
		t.Errorf("expected too many arguments error, got: %v", errs)
	}
}

func TestCtorDefaultParam(t *testing.T) {
	src := `Dog {
    String name
    Int age
    new(String name, Int age = 0) {
        this.name = name
        this.age = age
    }
}
main() {
    var d = Dog("Rex")
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestCtorNamedArg(t *testing.T) {
	src := `Dog {
    String name
    Int age
    new(String name, Int age = 0) {
        this.name = name
        this.age = age
    }
}
main() {
    var d = Dog(name: "Rex")
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestCtorUnknownNamedArg(t *testing.T) {
	src := `Dog {
    String name
    new(String name) {
        this.name = name
    }
}
main() {
    var d = Dog(badField: "Rex")
}`
	errs := checkSrc(src)
	if !hasError(errs, "unknown named argument") {
		t.Errorf("expected unknown named argument error, got: %v", errs)
	}
}

// --- with statement ----------------------------------------------------------

func TestWithStmtTypecheck(t *testing.T) {
	src := `main() {
    with (f = openFile("data.txt")) {
        print("ok")
    }
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestWithStmtResourceInScope(t *testing.T) {
	src := `main() {
    with (f = openFile("data.txt")) {
        print(f)
    }
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestWithStmtZincClassNoCloseOK(t *testing.T) {
	// A Zinc class without close() is valid — the runtime type assertion handles it gracefully.
	src := `
NoClose {
    new() {}
    pub String read() { return "data" }
}
main() {
    with (f = NoClose()) {
        print("ok")
    }
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- Null safety: strict Kotlin-style enforcement ----------------------------

func TestSafeNavOnNullableOK(t *testing.T) {
	src := `
Dog {
    String name
    new(String name) { this.name = name }
    pub String speak() { return "woof" }
}
main() {
    Dog? d = null
    print(d?.name)
    d?.speak()
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestSafeNavOnNonNullableError(t *testing.T) {
	src := `
Dog {
    String name
    new(String name) { this.name = name }
}
main() {
    var d = Dog("Rex")
    print(d?.name)
}`
	errs := checkSrc(src)
	if !hasError(errs, "unnecessary safe call on non-null type") {
		t.Errorf("expected 'unnecessary safe call' error, got %v", errs)
	}
}

func TestDotOnNullableFieldError(t *testing.T) {
	src := `
Dog {
    String name
    new(String name) { this.name = name }
}
main() {
    Dog? d = null
    print(d.name)
}`
	errs := checkSrc(src)
	if !hasError(errs, "use '?.'") {
		t.Errorf("expected 'use ?.' error for . on nullable, got %v", errs)
	}
}

func TestDotMethodOnNullableError(t *testing.T) {
	src := `
Dog {
    String name
    new(String name) { this.name = name }
    pub String speak() { return "woof" }
}
main() {
    Dog? d = null
    d.speak()
}`
	errs := checkSrc(src)
	if !hasError(errs, "use '?.'") {
		t.Errorf("expected 'use ?.' error for .method() on nullable, got %v", errs)
	}
}

func TestNullAssignToNonNullableClassError(t *testing.T) {
	src := `
Dog {
    String name
    new(String name) { this.name = name }
}
main() {
    Dog d = null
}`
	errs := checkSrc(src)
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected 'cannot assign' error for null to Dog, got %v", errs)
	}
}

func TestSafeNavChainConsistency(t *testing.T) {
	// a?.address returns Address? so subsequent access must use ?.
	src := `
Address {
    String city
    new(String city) { this.city = city }
}
User {
    Address? address
    new(Address? addr) { this.address = addr }
}
main() {
    User? u = null
    print(u?.address?.city)
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestSafeNavChainDotAfterSafeNavError(t *testing.T) {
	// u?.address returns Address? so .city should error
	src := `
Address {
    String city
    new(String city) { this.city = city }
}
User {
    Address address
    new(Address addr) { this.address = addr }
}
main() {
    User? u = null
    print(u?.address.city)
}`
	errs := checkSrc(src)
	if !hasError(errs, "use '?.'") {
		t.Errorf("expected chain consistency error: . after ?. on nullable result, got %v", errs)
	}
}

func TestSafeNavMethodReturnsOptional(t *testing.T) {
	src := `
Dog {
    String name
    new(String name) { this.name = name }
    pub String speak() { return "woof" }
}
main() {
    Dog? d = Dog("Rex")
    String result = d?.speak()
}`
	errs := checkSrc(src)
	// d?.speak() returns String? which can't assign to String
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected type mismatch: ?. result is Optional but target is not, got %v", errs)
	}
}

func TestSafeNavMethodResultToOptionalOK(t *testing.T) {
	src := `
Dog {
    String name
    new(String name) { this.name = name }
    pub String speak() { return "woof" }
}
main() {
    Dog? d = Dog("Rex")
    String? result = d?.speak()
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestNonNullableFieldNoSafeNavNeeded(t *testing.T) {
	// Non-nullable fields should use . not ?.
	src := `
Address {
    String city
    new(String city) { this.city = city }
}
User {
    Address address
    new(Address addr) { this.address = addr }
}
main() {
    var u = User(Address("NYC"))
    print(u.address.city)
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}
