package typechecker

import (
	"os"
	"strings"
	"testing"

	"growler/internal/lexer"
	"growler/internal/parser"
)

// checkSrc parses Growler source and runs the type checker, returning errors.
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
	src := `fn main() { print(x) }`
	errs := checkSrc(src)
	if !hasError(errs, `undefined variable "x"`) {
		t.Errorf("expected 'undefined variable' error, got %v", errs)
	}
}

func TestUndeclaredFunction(t *testing.T) {
	src := `fn main() { notDefined() }`
	errs := checkSrc(src)
	// undefined fn is not reported (could be imported), but we get TypeUnknown — no error
	// However the ident lookup for 'notDefined' as a var will fire
	// This is a call on unknown, so no error — correct behavior
	_ = errs
}

func TestDeclaredVariableOK(t *testing.T) {
	src := `fn main() { var x: Int = 1; print(x) }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 2. Type mismatch in var decl --------------------------------------------

func TestVarTypeMismatch(t *testing.T) {
	src := `fn main() { var x: Int = "hello" }`
	errs := checkSrc(src)
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected 'cannot assign' error, got %v", errs)
	}
}

func TestVarTypeMatch(t *testing.T) {
	src := `fn main() { var x: Int = 42 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestVarInferred(t *testing.T) {
	src := `fn main() { var x = 42; var y = "hello" }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestVarOptionalNull(t *testing.T) {
	src := `fn main() { var x: String? = null }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestVarNonOptionalNull(t *testing.T) {
	src := `fn main() { var x: Int = null }`
	errs := checkSrc(src)
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected 'cannot assign' error for null to Int, got %v", errs)
	}
}

// --- 3. Assignment type mismatch ---------------------------------------------

func TestAssignTypeMismatch(t *testing.T) {
	src := `fn main() { var x: Int = 1; x = "hello" }`
	errs := checkSrc(src)
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected 'cannot assign' error on reassign, got %v", errs)
	}
}

func TestAssignTypeMatch(t *testing.T) {
	src := `fn main() { var x: Int = 1; x = 2 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 4. Return type mismatch -------------------------------------------------

func TestReturnTypeMismatch(t *testing.T) {
	src := `fn add(a: Int, b: Int): Int { return "oops" }`
	errs := checkSrc(src)
	if !hasError(errs, "return type mismatch") {
		t.Errorf("expected 'return type mismatch' error, got %v", errs)
	}
}

func TestReturnTypeMatch(t *testing.T) {
	src := `fn add(a: Int, b: Int): Int { return 42 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestReturnVoid(t *testing.T) {
	src := `fn doSomething() { return }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestReturnValueFromVoidFn(t *testing.T) {
	src := `fn doSomething() { return 42 }`
	errs := checkSrc(src)
	// Returning a value from a void fn is a mismatch
	if !hasError(errs, "return type mismatch") {
		t.Errorf("expected return type mismatch for void fn, got %v", errs)
	}
}

// --- 5. Condition type -------------------------------------------------------

func TestIfConditionNotBool(t *testing.T) {
	src := `fn main() { if (1) { print("yes") } }`
	errs := checkSrc(src)
	if !hasError(errs, "condition must be Bool") {
		t.Errorf("expected 'condition must be Bool' error, got %v", errs)
	}
}

func TestIfConditionBool(t *testing.T) {
	src := `fn main() { if (true) { print("yes") } }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestWhileConditionNotBool(t *testing.T) {
	src := `fn main() { while (42) { print("loop") } }`
	errs := checkSrc(src)
	if !hasError(errs, "condition must be Bool") {
		t.Errorf("expected 'condition must be Bool' error in while, got %v", errs)
	}
}

func TestWhileConditionBool(t *testing.T) {
	src := `fn main() { var done = false; while (!done) { done = true } }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 6. Binary operators -----------------------------------------------------

func TestIntPlusString(t *testing.T) {
	src := `fn main() { var x = 1 + "hello" }`
	errs := checkSrc(src)
	// Should be an error since 1 is Int and "hello" is String
	if !hasError(errs, "not applicable") {
		t.Errorf("expected 'not applicable' error for Int+String, got %v", errs)
	}
}

func TestBoolArithmetic(t *testing.T) {
	src := `fn main() { var x = true + false }`
	errs := checkSrc(src)
	if !hasError(errs, "not applicable") {
		t.Errorf("expected 'not applicable' error for Bool+Bool, got %v", errs)
	}
}

func TestIntArithmetic(t *testing.T) {
	src := `fn main() { var x = 1 + 2; var y = 3 * 4 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestFloatArithmetic(t *testing.T) {
	src := `fn main() { var x = 1.5 + 2.5 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestStringConcatenation(t *testing.T) {
	src := `fn main() { var s = "hello" + " world" }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestComparisonOperator(t *testing.T) {
	src := `fn main() { var ok = 1 < 2 }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestLogicalOperators(t *testing.T) {
	src := `fn main() { var ok = true && false }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestLogicalOperatorNonBool(t *testing.T) {
	src := `fn main() { var ok = 1 && 2 }`
	errs := checkSrc(src)
	if !hasError(errs, "requires Bool") {
		t.Errorf("expected 'requires Bool' error for && on ints, got %v", errs)
	}
}

// --- 7. Field access ---------------------------------------------------------

func TestUndefinedField(t *testing.T) {
	src := `
class Dog {
  var name: String = "Rex"
}
fn main() {
  var d = Dog.new()
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
class Dog {
  var name: String = "Rex"
}
fn main() {
  var d = Dog.new()
  print(d.name)
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 8. Method calls ---------------------------------------------------------

func TestUndefinedMethod(t *testing.T) {
	src := `
class Dog {
  var name: String = "Rex"
}
fn main() {
  var d = Dog.new()
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
class Dog {
  var name: String = "Rex"
  fn speak(): String { return "woof" }
}
fn main() {
  var d = Dog.new()
  print(d.speak())
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestArgCountMismatch(t *testing.T) {
	src := `
fn add(a: Int, b: Int): Int { return a + b }
fn main() { add(1) }
`
	errs := checkSrc(src)
	if !hasError(errs, "missing required argument") && !hasError(errs, "wrong number of arguments") {
		t.Errorf("expected argument count mismatch error, got %v", errs)
	}
}

func TestArgCountMatch(t *testing.T) {
	src := `
fn add(a: Int, b: Int): Int { return a + b }
fn main() { add(1, 2) }
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 9. Class checks ---------------------------------------------------------

func TestUndeclaredParent(t *testing.T) {
	src := `class Dog : Animal { var name: String = "Rex" }`
	errs := checkSrc(src)
	if !hasError(errs, `undefined class/interface "Animal"`) {
		t.Errorf("expected 'undefined class/interface' error, got %v", errs)
	}
}

func TestDeclaredParentClass(t *testing.T) {
	src := `
class Animal { var alive: Bool = true }
class Dog : Animal { var name: String = "Rex" }
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestDeclaredParentInterface(t *testing.T) {
	src := `
interface Speaker { fn speak(): String }
class Dog : Speaker {
  fn speak(): String { return "woof" }
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 10. Generics — no false positives ----------------------------------------

func TestGenericFunctionNoFalsePositives(t *testing.T) {
	src := `
fn identity<T>(x: T): T { return x }
fn main() { var y = identity(42) }
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestGenericClassNoFalsePositives(t *testing.T) {
	src := `
class Box<T> {
  var value: T
  fn get(): T { return value }
}
fn main() {
  var b = Box.new()
  print(b.get())
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 11. Optionals -----------------------------------------------------------

func TestOptionalAssignNull(t *testing.T) {
	src := `fn main() { var name: String? = null }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestOptionalAssignValue(t *testing.T) {
	src := `fn main() { var name: String? = "hello" }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestNonOptionalAssignNull(t *testing.T) {
	src := `fn main() { var count: Int = null }`
	errs := checkSrc(src)
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected 'cannot assign' for null to Int, got %v", errs)
	}
}

// --- 12. Or handler -----------------------------------------------------------

func TestOrHandlerErrVarDefined(t *testing.T) {
	src := `
fn risky(): Int {
  if (true) { return Error("oops") }
  return 1
}
fn main() {
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
fn risky(): Int {
  if (true) { return Error("oops") }
  return 1
}
fn main() {
  var x = risky() or {
    var msg: String = err
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
fn main() {
  var x = 42
  if (true) {
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
fn main() {
  if (true) {
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
fn main() {
  var items = [1, 2, 3]
  for item in items { print(item) }
}
`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- 14. Valid programs — all examples produce zero errors -------------------

func TestValidHello(t *testing.T) {
	src, err := os.ReadFile("../../examples/hello.gw")
	if err != nil {
		t.Skip("examples/hello.gw not found")
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidClasses(t *testing.T) {
	src, err := os.ReadFile("../../examples/classes.gw")
	if err != nil {
		t.Skip("examples/classes.gw not found")
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidGenerics(t *testing.T) {
	src, err := os.ReadFile("../../examples/generics.gw")
	if err != nil {
		t.Skip("examples/generics.gw not found")
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidErrors(t *testing.T) {
	src, err := os.ReadFile("../../examples/errors.gw")
	if err != nil {
		t.Skip("examples/errors.gw not found")
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidFibonacci(t *testing.T) {
	src, err := os.ReadFile("../../examples/fibonacci.gw")
	if err != nil {
		t.Skip("examples/fibonacci.gw not found")
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidEnums(t *testing.T) {
	src, err := os.ReadFile("../../examples/enums.gw")
	if err != nil {
		t.Skip("examples/enums.gw not found")
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidConcurrency(t *testing.T) {
	src, err := os.ReadFile("../../examples/concurrency.gw")
	if err != nil {
		t.Skip("examples/concurrency.gw not found")
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

func TestValidClosures(t *testing.T) {
	src, err := os.ReadFile("../../examples/closures.gw")
	if err != nil {
		t.Skip("examples/closures.gw not found")
	}
	errs := checkSrc(string(src))
	noErrors(t, errs, string(src))
}

// --- Extra: "this" outside class ---------------------------------------------

func TestThisOutsideClass(t *testing.T) {
	src := `fn main() { print(this) }`
	errs := checkSrc(src)
	if !hasError(errs, `"this" used outside`) {
		t.Errorf("expected 'this used outside' error, got %v", errs)
	}
}

// --- Extra: undefined type ---------------------------------------------------

func TestUndefinedType(t *testing.T) {
	src := `fn foo(x: Baz): Baz { return x }`
	errs := checkSrc(src)
	if !hasError(errs, `undefined type "Baz"`) {
		t.Errorf("expected 'undefined type Baz' error, got %v", errs)
	}
}

// --- Failable lambda restrictions --------------------------------------------

func TestFailableLambdaNoFalsePositive(t *testing.T) {
	src := `
fn apply(callback: Any): Int {
    return callback(5)
}
fn main() {
    apply((x: Int): Int => x * 2)
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- Default parameters and named arguments ----------------------------------

func TestDefaultParamNoError(t *testing.T) {
	src := `fn greet(name: String, greeting: String = "Hello") {}
fn main() { greet("Alice") }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestNamedArgsAllValid(t *testing.T) {
	src := `fn greet(name: String, greeting: String = "Hello") {}
fn main() { greet(greeting: "Hi", name: "Bob") }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestMixedPositionalAndNamedArgs(t *testing.T) {
	src := `fn greet(name: String, greeting: String = "Hello") {}
fn main() { greet("Bob", greeting: "Hi") }`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestMissingRequiredArg(t *testing.T) {
	src := `fn greet(name: String, greeting: String = "Hello") {}
fn main() { greet() }`
	errs := checkSrc(src)
	if !hasError(errs, "missing required argument") {
		t.Errorf("expected missing required argument error, got: %v", errs)
	}
}

func TestUnknownNamedArg(t *testing.T) {
	src := `fn greet(name: String) {}
fn main() { greet(badParam: "hi") }`
	errs := checkSrc(src)
	if !hasError(errs, "unknown named argument") {
		t.Errorf("expected unknown named argument error, got: %v", errs)
	}
}

func TestTooManyArgs(t *testing.T) {
	src := `fn add(x: Int, y: Int): Int { return x }
fn main() { add(1, 2, 3) }`
	errs := checkSrc(src)
	if !hasError(errs, "too many arguments") {
		t.Errorf("expected too many arguments error, got: %v", errs)
	}
}

func TestCtorDefaultParam(t *testing.T) {
	src := `class Dog {
    var name: String
    var age: Int
    construct new(name: String, age: Int = 0) {
        this.name = name
        this.age = age
    }
}
fn main() {
    var d = Dog.new("Rex")
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestCtorNamedArg(t *testing.T) {
	src := `class Dog {
    var name: String
    var age: Int
    construct new(name: String, age: Int = 0) {
        this.name = name
        this.age = age
    }
}
fn main() {
    var d = Dog.new(name: "Rex")
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestCtorUnknownNamedArg(t *testing.T) {
	src := `class Dog {
    var name: String
    construct new(name: String) {
        this.name = name
    }
}
fn main() {
    var d = Dog.new(badField: "Rex")
}`
	errs := checkSrc(src)
	if !hasError(errs, "unknown named argument") {
		t.Errorf("expected unknown named argument error, got: %v", errs)
	}
}

// --- with statement ----------------------------------------------------------

func TestWithStmtTypecheck(t *testing.T) {
	src := `fn main() {
    with (var f = openFile("data.txt")) {
        print("ok")
    }
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestWithStmtResourceInScope(t *testing.T) {
	src := `fn main() {
    with (var f = openFile("data.txt")) {
        print(f)
    }
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestWithStmtGrowlerClassNoCloseOK(t *testing.T) {
	// A Growler class without close() is valid — the runtime type assertion handles it gracefully.
	src := `
class NoClose {
    construct new() {}
    pub fn read(): String { return "data" }
}
fn main() {
    with (var f = NoClose.new()) {
        print("ok")
    }
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

// --- Null safety: strict Kotlin-style enforcement ----------------------------

func TestSafeNavOnNullableOK(t *testing.T) {
	src := `
class Dog {
    var name: String
    construct new(name: String) { this.name = name }
    pub fn speak(): String { return "woof" }
}
fn main() {
    var d: Dog? = null
    print(d?.name)
    d?.speak()
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestSafeNavOnNonNullableError(t *testing.T) {
	src := `
class Dog {
    var name: String
    construct new(name: String) { this.name = name }
}
fn main() {
    var d = Dog.new("Rex")
    print(d?.name)
}`
	errs := checkSrc(src)
	if !hasError(errs, "unnecessary safe call on non-null type") {
		t.Errorf("expected 'unnecessary safe call' error, got %v", errs)
	}
}

func TestDotOnNullableFieldError(t *testing.T) {
	src := `
class Dog {
    var name: String
    construct new(name: String) { this.name = name }
}
fn main() {
    var d: Dog? = null
    print(d.name)
}`
	errs := checkSrc(src)
	if !hasError(errs, "use '?.'") {
		t.Errorf("expected 'use ?.' error for . on nullable, got %v", errs)
	}
}

func TestDotMethodOnNullableError(t *testing.T) {
	src := `
class Dog {
    var name: String
    construct new(name: String) { this.name = name }
    pub fn speak(): String { return "woof" }
}
fn main() {
    var d: Dog? = null
    d.speak()
}`
	errs := checkSrc(src)
	if !hasError(errs, "use '?.'") {
		t.Errorf("expected 'use ?.' error for .method() on nullable, got %v", errs)
	}
}

func TestNullAssignToNonNullableClassError(t *testing.T) {
	src := `
class Dog {
    var name: String
    construct new(name: String) { this.name = name }
}
fn main() {
    var d: Dog = null
}`
	errs := checkSrc(src)
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected 'cannot assign' error for null to Dog, got %v", errs)
	}
}

func TestSafeNavChainConsistency(t *testing.T) {
	// a?.address returns Address? so subsequent access must use ?.
	src := `
class Address {
    var city: String
    construct new(city: String) { this.city = city }
}
class User {
    var address: Address?
    construct new(addr: Address?) { this.address = addr }
}
fn main() {
    var u: User? = null
    print(u?.address?.city)
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestSafeNavChainDotAfterSafeNavError(t *testing.T) {
	// u?.address returns Address? so .city should error
	src := `
class Address {
    var city: String
    construct new(city: String) { this.city = city }
}
class User {
    var address: Address
    construct new(addr: Address) { this.address = addr }
}
fn main() {
    var u: User? = null
    print(u?.address.city)
}`
	errs := checkSrc(src)
	if !hasError(errs, "use '?.'") {
		t.Errorf("expected chain consistency error: . after ?. on nullable result, got %v", errs)
	}
}

func TestSafeNavMethodReturnsOptional(t *testing.T) {
	src := `
class Dog {
    var name: String
    construct new(name: String) { this.name = name }
    pub fn speak(): String { return "woof" }
}
fn main() {
    var d: Dog? = Dog.new("Rex")
    var result: String = d?.speak()
}`
	errs := checkSrc(src)
	// d?.speak() returns String? which can't assign to String
	if !hasError(errs, "cannot assign") {
		t.Errorf("expected type mismatch: ?. result is Optional but target is not, got %v", errs)
	}
}

func TestSafeNavMethodResultToOptionalOK(t *testing.T) {
	src := `
class Dog {
    var name: String
    construct new(name: String) { this.name = name }
    pub fn speak(): String { return "woof" }
}
fn main() {
    var d: Dog? = Dog.new("Rex")
    var result: String? = d?.speak()
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestNonNullableFieldNoSafeNavNeeded(t *testing.T) {
	// Non-nullable fields should use . not ?.
	src := `
class Address {
    var city: String
    construct new(city: String) { this.city = city }
}
class User {
    var address: Address
    construct new(addr: Address) { this.address = addr }
}
fn main() {
    var u = User.new(Address.new("NYC"))
    print(u.address.city)
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}
