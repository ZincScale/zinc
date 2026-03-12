# Language Reference

## Variables

```zinc
var x Int = 42
var name String = "Zinc"
var flag Bool = true
var ratio Float = 3.14
var maybeNull String? = null    // optional (nullable) type
```

## Constants

Top-level immutable values declared with `const`:

```zinc
const PI = 3.14159
const MAX_RETRIES Int = 3
const APP_NAME String = "Zinc"

main() {
    print(APP_NAME)
    print(PI * 2.0)
}
```

Transpiles to:

```go
const PI = 3.14159
const MAX_RETRIES int = 3
const APP_NAME string = "Zinc"

func main() {
    fmt.Println(APP_NAME)
    fmt.Println(PI * 2.0)
}
```

## Functions

```zinc
add(a Int, b Int) Int {
    return a + b
}

pub greet(name String) String {
    return "Hello, {name}!"
}
```

### Default Parameter Values

Parameters may declare a default value with `= expr`. Callers that omit the argument receive the default — inlined by the transpiler into the emitted Go call site (no runtime overhead):

```zinc
greet(name String, greeting String = "Hello") {
    print("{greeting}, {name}!")
}

main() {
    greet("Alice")              // greeting defaults to "Hello"
    greet("Bob", "Hi")          // explicit override
}
```

Transpiles to:

```go
func greet(name string, greeting string) {
    fmt.Println(fmt.Sprintf("%v, %v!", greeting, name))
}

func main() {
    greet("Alice", "Hello")
    greet("Bob", "Hi")
}
```

### Named Arguments

Arguments may be passed by name at any call site using `name: value` syntax. Named arguments may appear in any order and can be mixed with leading positional arguments. Positional arguments must always come first.

```zinc
connect(host String, port Int = 8080, tls Bool = false) { }

main() {
    connect("localhost")                       // both defaults used
    connect("example.com", port: 443, tls: true)  // named, positional host
    connect(tls: true, host: "example.com")    // fully named, reordered
}
```

Named arguments also work on constructors:

```zinc
Dog {
    name String
    age Int

    new(name String, age Int = 0) {
        this.name = name
        this.age = age
    }
}

main() {
    d1 := Dog("Rex")              // age defaults to 0
    d2 := Dog("Buddy", 3)         // explicit
    d3 := Dog(name: "Max")        // named, age defaults
    d4 := Dog(age: 5, name: "Spot") // named, reordered
}
```

Transpiles to:

```go
func main() {
    d1 := NewDog("Rex", 0)
    d2 := NewDog("Buddy", 3)
    d3 := NewDog("Max", 0)
    d4 := NewDog("Spot", 5)
}
```

### Generic Functions

```zinc
identity<T>(val T) T {
    return val
}

pair<K, V>(key K, value V) K {
    return key
}
```

### Variadic Functions

Functions can accept a variable number of arguments using `...Type` syntax — the last parameter becomes a variadic parameter:

```zinc
log(level String, msgs ...String) {
    for msg in msgs {
        print("[{level}] {msg}")
    }
}

main() {
    log("INFO", "server started", "listening on :8080")

    // Spread a list into variadic args
    errors := ["timeout", "connection refused"]
    log("ERROR", errors...)
}
```

Transpiles directly to Go's variadic syntax:

```go
func log(level string, msgs ...string) {
    for _, msg := range msgs {
        fmt.Println(fmt.Sprintf("[%v] %v", level, msg))
    }
}

func main() {
    log("INFO", "server started", "listening on :8080")
    errors := []string{"timeout", "connection refused"}
    log("ERROR", errors...)
}
```

## Classes

```zinc
Dog {
    name String
    age Int

    new(name String, age Int = 0) {
        this.name = name
        this.age = age
    }

    pub bark() String {
        return "{this.name} says: Woof!"
    }

    pub static create(name String) Dog {
        return Dog(name)
    }
}
```

### Named Constructors

Every class has one primary constructor declared with `new(...)`, called as `ClassName(...)`. Additional named constructors are `pub static` factory methods that call the constructor internally:

```zinc
Point {
    x Float
    y Float

    new(x Float, y Float) {
        this.x = x
        this.y = y
    }

    // Named constructor — origin
    pub static origin() Point {
        return Point(0.0, 0.0)
    }

    // Named constructor — from a single value
    pub static diagonal(v Float) Point {
        return Point(v, v)
    }
}

main() {
    a := Point(3.0, 4.0)   // primary constructor
    b := Point.origin()         // named constructor
    c := Point.diagonal(5.0)    // named constructor
}
```

Transpiles to:

```go
type PointImpl struct {
    X float64
    Y float64
}

type Point interface {
    GetX() float64
    SetX(float64)
    GetY() float64
    SetY(float64)
}

func NewPoint(x float64, y float64) *PointImpl {
    obj := &PointImpl{}
    obj.X = x
    obj.Y = y
    return obj
}

func Point_Origin() *PointImpl {
    return NewPoint(0.0, 0.0)
}

func Point_Diagonal(v float64) *PointImpl {
    return NewPoint(v, v)
}
```

> **How it works:** Each Zinc class generates a Go struct (`NameImpl`) and a Go interface (`Name`). The interface includes getters, setters, and all public methods. This enables true OO polymorphism — any function that accepts `Animal` can receive a `Dog`, just like in Java or C#.

### Generic Classes

```zinc
Box<T> {
    value T

    new(v T) {
        this.value = v
    }

    pub get() T {
        return this.value
    }
}

main() {
    // Type inference — Go infers T from the argument
    intBox := Box(42)        // Box<Int>
    strBox := Box("hello")   // Box<String>
    print(intBox.get())             // 42
    print(strBox.get())             // hello
}
```

Generic classes can use empty list/map literals in constructors — the type is inferred from the field declaration:

```zinc
Stack<T> {
    items List<T>

    new(initial T) {
        this.items = []             // inferred as []T{}, not []interface{}{}
        this.items.add(initial)
    }

    pub push(item T) {
        this.items.add(item)
    }

    pub count() Int {
        return this.items.size()
    }
}

main() {
    s := Stack(1)
    s.push(2)
    s.push(3)
    print(s.count())    // 3
}
```

Multi-type-parameter generic classes:

```zinc
Pair<K, V> {
    key K
    val V

    new(key K, val V) {
        this.key = key
        this.val = val
    }
}

main() {
    p := Pair("name", 42)
    print(p.key)    // name
    print(p.val)    // 42
}
```

### Go Type Construction (`.new()`)

Zinc extends its constructor pattern to any Go type via `.new()` — the OO constructor pattern every Java/Python/C#/Ruby developer knows:

```zinc
import "sync"
import "bytes"
import "net/url"

main() {
    // Zero-value construction
    mu := sync.Mutex.new()
    buf := bytes.Buffer.new()

    // Named field construction (just like named args)
    u := url.URL.new(Scheme: "https", Host: "example.com", Path: "/api")
    print(u.String())   // https://example.com/api
}
```

Transpiles to idiomatic Go struct literals:

```go
mu := sync.Mutex{}
buf := bytes.Buffer{}
u := url.URL{Scheme: "https", Host: "example.com", Path: "/api"}
```

## Interfaces

```zinc
interface Speaker {
    pub speak() String
}

Cat : Speaker {
    pub speak() String {
        return "Meow!"
    }
}
```

## Inheritance

```zinc
Animal {
    name String
    new(name String) { this.name = name }
    pub describe() String { return "Animal: {this.name}" }
}

Dog : Animal, Speaker {
    new(name String) {
        super(name)
    }
    pub speak() String { return "Woof!" }
}
```

## Polymorphism

Zinc classes support true OO polymorphism. A function that accepts a class or interface type can receive any subclass:

```zinc
interface Speaker {
    pub speak() String
}

Animal {
    name String
    new(n String) { this.name = n }
}

Dog : Animal, Speaker {
    new(n String) { super(n) }
    pub speak() String {
        return "{this.name} says Woof!"
    }
}

printSpeak(s Speaker) {
    print(s.speak())
}

main() {
    d := Dog("Rex")
    printSpeak(d)         // Rex says Woof!
}
```

This works because each class generates a Go interface. `Dog` satisfies both the `Animal` interface and the `Speaker` interface, so it can be passed to any function expecting either type.

Field access through interface-typed parameters uses auto-generated getters:

```zinc
greet(p Person) {
    print("Hello, {p.name}")  // uses p.GetName() under the hood
}
```

Error handling works seamlessly through polymorphic dispatch. Failable methods on interface-typed parameters are correctly detected:

```zinc
Validator {
    value Int
    new(v Int) { this.value = v }
    pub validate() String {
        if this.value < 0 { return Error("negative") }
        return "ok"
    }
}

check(v Validator) {
    result := v.validate() or {
        print("error: {err}")
        return
    }
    print(result)
}
```

Generic classes also work through interface-typed parameters:

```zinc
Pair<K, V> {
    key K
    val V
    new(key K, val V) { this.key = key; this.val = val }
}

printKey(p Pair<String, Int>) {
    print(p.key)   // uses p.GetKey() under the hood
}

main() {
    p := Pair("hello", 42)
    printKey(p)     // hello
}
```

## Enums

```zinc
enum Direction { North, South, East, West }
enum Status { Pending, Active, Closed }
```

Emits Go `iota` constants:

```go
type Direction int
const (
    DirectionNorth Direction = iota
    DirectionSouth
    DirectionEast
    DirectionWest
)
```

## Collection Literals

List and map literals are automatically typed by the typechecker. When all elements share the same type, the output uses that concrete type instead of `interface{}`:

```zinc
main() {
    nums := [1, 2, 3]             // inferred as []int
    names := ["Alice", "Bob"]     // inferred as []string
    scores := {"math": 95, "sci": 88}  // inferred as map[string]int

    // Mixed types fall back to interface{}
    mixed := [1, "two", 3]        // []interface{}

    // Empty literals use the declared type
    var m Map<String, Int> = {}     // map[string]int{}
    var l List<Int> = []            // []int{}

    // Nested collections work too
    grid := [[1, 2], [3, 4]]      // [][]int
}
```

Transpiles to:

```go
func main() {
    nums := []int{1, 2, 3}
    names := []string{"Alice", "Bob"}
    scores := map[string]int{"math": 95, "sci": 88}
    mixed := []interface{}{1, "two", 3}
    m := map[string]int{}
    l := []int{}
    grid := [][]int{[]int{1, 2}, []int{3, 4}}
}
```

## Slicing

Extract sub-sequences from lists and strings. Both bracket syntax and an OO `.slice()` method are supported:

```zinc
nums := [1, 2, 3, 4, 5]

// Bracket syntax — [low:high], either bound optional
print(nums[1:3])    // [2 3]
print(nums[2:])     // [3 4 5]
print(nums[:3])     // [1 2 3]

// OO method — .slice(start, end) or .slice(start)
print(nums.slice(1, 3))   // [2 3]
print(nums.slice(2))      // [3 4 5]

// Assign slices to new variables
firstTwo := nums[:2]        // [1 2]
middle := nums.slice(1, 4)  // [2 3 4]

// Works on strings too
s := "Hello, Zinc!"
print(s[0:5])          // Hello
print(s.slice(7))      // Zinc!
word := s[7:11]     // Zinc
```

Transpiles directly to Go slice expressions:

```go
firstTwo := nums[:2]
middle := nums[1:4]
s[0:5]
s[7:]
word := s[7:11]
```

## Match / Switch

```zinc
enum Direction { North, South, East, West }

describe(d Direction) String {
    match d {
        case Direction.North => { return "Going North" }
        case Direction.South => { return "Going South" }
        case Direction.East  => { return "Going East"  }
        case Direction.West  => { return "Going West"  }
        case _ => { return "Unknown" }
    }
}
```

## String Interpolation

```zinc
var name String = "Zinc"
version := 1
print("Welcome to {name} v{version}!")
// → fmt.Println(fmt.Sprintf("Welcome to %v v%v!", name, version))
```

## Control Flow

```zinc
// if / else if / else
if x > 0 {
    print("positive")
} else if x < 0 {
    print("negative")
} else {
    print("zero")
}

// while loop
while x > 0 {
    x -= 1
}

// C-style for
for (var i Int = 0; i < 10; i += 1) {
    print(i)
}

// for-in (range)
for item in items {
    print(item)
}

// for-in with index (lists)
for (i, item) in items {
    print("{i}: {item}")
}

// for-in with key-value (maps)
scores := {"Alice": 95, "Bob": 87}
for (name, score) in scores {
    print("{name} got {score}")
}
```

### Labeled Loops

Like Java, Zinc supports labeled `break` and `continue` for nested loop control. Prefix a loop with `@label` and reference it from inner loops:

```zinc
@outer for (i := 0; i < 10; i += 1) {
    for (j := 0; j < 10; j += 1) {
        if j == 5 {
            break @outer       // exits both loops
        }
        if i == j {
            continue @outer    // skips to next i iteration
        }
    }
}
```

Works with both `for` and `while` loops. Transpiles directly to Go's native labeled loops:

```go
outer:
for i := 0; i < 10; i++ {
    for j := 0; j < 10; j++ {
        if j == 5 { break outer }
        if i == j { continue outer }
    }
}
```

## Safe Navigation (`?.`)

Inspired by Kotlin, C#, Swift, and TypeScript. Access fields and call methods on nullable references without manual null checks. If the receiver is `nil`, the entire expression evaluates to `nil` — no crash, no exception:

```zinc
User {
    name String
    address Address?

    new(name String, addr Address?) {
        this.name = name
        this.address = addr
    }
}

Address {
    city String
    new(city String) { this.city = city }
}

main() {
    var user User? = User("Alice", Address("NYC"))

    // Field access — returns nil if user is nil
    name := user?.name           // "Alice"

    // Chaining — each ?. short-circuits independently (like Kotlin)
    city := user?.address?.city   // "NYC"

    // Method call — skipped if receiver is nil
    user?.doSomething()

    // Nil receiver — no crash
    var nobody User? = null
    x := nobody?.name             // nil
    y := nobody?.address?.city    // nil
    nobody?.doSomething()            // no-op
}
```

**Statement context** — when `?.` is used as a statement (void method call), it generates a clean nil guard:

```go
// user?.doSomething()  →
if user != nil { user.DoSomething() }
```

**Expression context** — when used in an assignment, it generates a nil-safe wrapper:

```go
// var name = user?.name  →
name := func() interface{} { if user != nil { return user.Name }; return nil }()
```

**Chained expressions** — `a?.b?.c` generates a single flat function with sequential nil checks (no nested wrappers):

```go
// var city = user?.address?.city  →
city := func() interface{} {
    _s0 := user; if _s0 == nil { return nil }
    _s1 := _s0.Address; if _s1 == nil { return nil }
    return _s1.City
}()
```

## Type Casting (`as` / `is`)

Zinc uses `as` for type assertions and `is` for type checks — familiar from Kotlin, C#, and TypeScript:

```zinc
main() {
    var x Any = 42

    // Type assertion — panics if wrong type (like Kotlin's `as`)
    n := x as Int
    print(n + 1)    // 43

    // Type check — returns Bool (like Kotlin's `is`)
    if x is Int {
        print("it's an Int")
    }
    if x is String {
        print("it's a String")   // not reached
    }
}
```

Transpiles to Go type assertions:

```go
n := x.(int)                                            // as
func() bool { _, ok := x.(int); return ok }()           // is
```

## Null Safety

Zinc enforces Kotlin-style strict null safety. Non-nullable types cannot hold `null`, and nullable types (`Type?`) require safe access:

```zinc
Dog {
    name String
    new(name String) { this.name = name }
}

main() {
    var d Dog = Dog("Rex")
    print(d.name)         // OK — d is non-nullable, use regular dot

    var d2 Dog? = null
    print(d2?.name)       // OK — d2 is nullable, use ?.
    // print(d2.name)     // ERROR: "use '?.' for safe access on nullable type"
    // var d3 Dog = null  // ERROR: "cannot assign null to non-nullable type"
}
```

## Callable Function Types (`Fn<>`)

Use `Fn<(ParamTypes), ReturnType>` to declare typed function parameters — enabling higher-order functions, callbacks, and functional patterns:

```zinc
apply(f Fn<(Int), Int>, x Int) Int {
    return f(x)
}

combine(f Fn<(Int, Int), Int>, a Int, b Int) Int {
    return f(a, b)
}

run(callback Fn<(), Void>) {
    callback()
}

main() {
    double := (x Int) Int => x * 2
    print(apply(double, 7))       // 14

    add := (a Int, b Int) Int => a + b
    print(combine(add, 3, 4))     // 7

    run(() Void => { print("done") })

    // Also works as variable type annotations
    var transform Fn<(String), Int> = (s String) Int => s.size()
    print(transform("hello"))     // 5
}
```

Transpiles to Go's native function types:

```go
func apply(f func(int) int, x int) int { return f(x) }
func combine(f func(int, int) int, a int, b int) int { return f(a, b) }
func run(callback func()) { callback() }
```

## Closures / Lambdas

Lambdas use the `(params) ReturnType => body` syntax. The body is either a
single expression or a block `{ ... }`.

```zinc
// Single-expression lambda (inferred as a func literal)
double := (x Int) Int => x * 2
greet  := () String => "Hello!"

// Block-body lambda
describe := (x Int) String => {
    if x > 0 {
        return "positive"
    }
    return "non-positive"
}

// Closure capture — lambda body may reference outer variables
base   := 100
addBase := (x Int) Int => x + base

// String interpolation works inside lambda bodies
makeMsg := (name String) String => "Hello, {name}!"
```

Transpiles to idiomatic Go `func` literals:

```go
double  := func(x int) int { return (x * 2) }
greet   := func() string { return "Hello!" }
describe := func(x int) string { ... }
base    := 100
addBase := func(x int) int { return (x + base) }
makeMsg := func(name string) string { return fmt.Sprintf("Hello, %v!", name) }
```

### Failable Lambdas

A lambda that contains `return Error(...)` automatically gets an `error` return
appended to its signature. Calls to failable lambdas auto-propagate errors:

```zinc
safeDivide := (a Int, b Int) Int => {
    if b == 0 {
        return Error("division by zero")
    }
    return a / b
}

result := safeDivide(10, 2)   // auto-propagates error
print(result)

bad := safeDivide(10, 0) or {
    print("Error: {err}")
    exit(1)
}
```

Transpiles to:

```go
safeDivide := func(a int, b int) (int, error) {
    if b == 0 {
        return 0, fmt.Errorf("division by zero")
    }
    return (a / b), nil
}
result, _err0 := safeDivide(10, 2)
if _err0 != nil { panic(_err0) }
fmt.Println(result)
bad, _err1 := safeDivide(10, 0)
if _err1 != nil {
    err := _err1.Error()
    fmt.Println(fmt.Sprintf("Error: %v", err))
    os.Exit(1)
}
```

## With (Resource Management)

The `with` statement is Zinc's equivalent of Java's try-with-resources, Python's `with`, and C#'s `using`. It ensures resources are cleaned up automatically when the block exits:

- **Files** (anything implementing `io.Closer`) → `defer Close()`
- **Mutexes** (anything implementing `sync.Locker`) → `Lock()` + `defer Unlock()`

No manual cleanup needed — same OO ergonomics Java/C#/Python developers expect.

```zinc
import "os"

main() {
    with (f := os.Stdin) {
        // f is closed automatically when the block exits
        print("reading file")
    }
}
```

### Auto-Detected Multi-Return

Many Go functions return `(value, error)`. Zinc auto-detects these and unpacks the tuple, throwing on error — no manual error handling needed:

```zinc
import "os"

main() {
    // os.Create returns (*File, error) — auto-detected and unpacked
    with (f := os.Create("output.txt")) {
        f.WriteString("hello from Zinc")
    }
    // f is closed automatically, error was auto-checked
}
```

Transpiles to:

```go
func main() {
    {
        f, _err0 := os.Create("output.txt")
        if _err0 != nil { panic(_err0) }
        if _c, ok := any(f).(io.Closer); ok { defer _c.Close() }
        f.WriteString("hello from Zinc")
    }
}
```

### `with` + `or` Handler

When a `with` resource is failable, use an `or` handler to add context or halt:

```zinc
with (f := os.Open("/nonexistent/file") or {
    print("caught: {err}")
    exit(1)
}) {
    print("should not reach")
}
```

### Mutex Locking

`with` auto-detects `sync.Locker` and locks/unlocks — like Java's `synchronized` or Python's `with lock`:

```zinc
import "sync"

main() {
    counter := 0
    with (mu := sync.Mutex.new()) {
        counter += 1    // mutex locked here, unlocked when block exits
    }
}
```

### Multiple Resources

Comma-separated resources are closed in reverse order (LIFO), matching Go's `defer` stack:

```zinc
import "os"

main() {
    with (f1 := os.Create("a.txt"), f2 := os.Create("b.txt")) {
        f1.WriteString("file A")
        f2.WriteString("file B")
    }
    // f2 closes first, then f1
}
```

## Error Handling

Zinc uses errors as values with auto-propagation — no try/catch needed:

```zinc
divide(a Int, b Int) Int {
    if b == 0 {
        return Error("division by zero")
    }
    return a / b
}

main() {
    // Auto-propagation: panics in main if error occurs
    result := divide(10, 2)
    print(result)

    // Or handler: add context, log, and halt
    bad := divide(10, 0) or {
        print("caught: {err}")
        exit(0)
    }
}
```

## Concurrency

```zinc
main() {
    var ch Chan<Int> = Chan.new(1)

    go {
        ch.send(42)
    }

    var val Int = ch.receive()
    print(val)
}
```

## Tuple Unpacking

Zinc maps directly to Go's multi-return. You can unpack any Go function that returns multiple values via `import`:

```zinc
import "strconv"

main() {
    // strconv.Atoi returns (int, error)
    var (n, err) = strconv.Atoi("42")
}
```

> **Note:** Both names in `var (a, b) = ...` must be used. If you only need one value, assign the other to `_` using a regular `var`.

## Imports

```zinc
import "os"
import "math/rand" as rand

main() {
    var args Any = os.Args
}
```

## Collection Methods (LINQ-style)

Zinc provides LINQ-style collection methods that compile to fused Go loops — no intermediate allocations, no iterator wrappers.

### Basic Usage

```zinc
nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]

// Filter
evens := nums.Where(x => x % 2 == 0)

// Transform
doubled := nums.Select(x => x * 2)

// Chain — compiles to a single fused for-loop
result := nums.Where(x => x > 5).Select(x => x * 2).Take(3)
```

### Lambda Shorthand

Collection methods use a concise lambda syntax — no type annotations needed:

```zinc
// Single parameter — no parens needed
nums.Where(x => x > 5)
nums.Select(x => x * 2)

// Multi-parameter (parens required)
nums.Aggregate(0, (acc, x) => acc + x)
```

### Available Methods

| Method | Description | Terminal? |
|--------|-------------|-----------|
| `Where(x => bool)` | Filter elements | No |
| `Select(x => expr)` | Transform elements | No |
| `Take(n)` | Take first n elements | No |
| `Skip(n)` | Skip first n elements | No |
| `ForEach(x => stmt)` | Execute side effect per element | Yes |
| `Any(x => bool)` | True if any element matches | Yes |
| `All(x => bool)` | True if all elements match | Yes |
| `First(x => bool)` | First matching element (panics if none) | Yes |
| `FirstOrDefault(x => bool)` | First matching element (zero value if none) | Yes |
| `Count()` | Count elements (after filtering) | Yes |
| `Aggregate(seed, (acc, x) => expr)` | Reduce to single value | Yes |
| `ToList()` | Materialize chain to list | Yes |

### Codegen: Loop Fusion

Chains compile to a single fused loop. No intermediate slices are created:

```zinc
result := nums.Where(x => x > 5).Select(x => x * 2).Take(3)
```

Transpiles to:

```go
var result []interface{}
_take0 := 0
for _, _v0 := range nums {
    if _v0 > 5 {
        _v1 := _v0 * 2
        if _take0 >= 3 { break }
        result = append(result, _v1)
        _take0++
    }
}
```

### Error Propagation in Lambdas

Failable functions work seamlessly inside collection lambdas. Errors auto-propagate — same as everywhere else in Zinc:

```zinc
safeDivide(x Int) Int {
    if x == 0 { return Error("division by zero") }
    return 100 / x
}

main() {
    nums := [2, 5, 10]
    result := nums.Select(x => safeDivide(x))
    // If any element causes an error, it propagates immediately
    for n in result { print(n) }
}
```

The transpiler detects failable calls inside lambda expressions and lifts them into statement-level error checks within the fused loop:

```go
for _, _v0 := range nums {
    x := _v0
    _fv0, _err0 := safeDivide(x)
    if _err0 != nil { panic(_err0) }
    result = append(result, _fv0)
}
```

This works with all chain methods — `Where`, `Select`, `Aggregate`, `Any`, `All`, `First`, etc.

## Type System

| Zinc     | Go          |
|-------------|-------------|
| `Int`       | `int`       |
| `Float`     | `float64`   |
| `String`    | `string`    |
| `Bool`      | `bool`      |
| `Any`       | `interface{}`|
| `String?`   | `*string`   |
| `List<T>`   | `[]T`       |
| `Map<K,V>`  | `map[K]V`   |
| `Chan<T>`   | `chan T`    |
