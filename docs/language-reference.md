# Language Reference

Zinc compiles to native binaries via **C# AOT** (default) or **Go**.

The C# backend uses .NET Native AOT with full tree-shaking (`TrimMode=full`), symbol stripping, and speed optimization. The compiler runs a .NET type probe at transpile time to discover 3,700+ BCL types — so imported constructors like `Stopwatch()` automatically emit `new Stopwatch()`.

The "Transpiles to:" sections below show Go output for reference — the C# backend produces equivalent idiomatic C# code.

## Variables

```zinc
var x = 42
var name = "Zinc"
var flag = true
var ratio = 3.14
String? maybeNull = null    // optional (nullable) type
```

## Constants

Top-level immutable values declared with `const`. By default, constants are package-private. Use `pub const` to export them:

```zinc
const Float INTERNAL_RATE = 0.05        // private — only visible within the package
pub const PI = 3.14159                  // exported
pub const Int MAX_RETRIES = 3           // exported, with explicit type
pub const String APP_NAME = "Zinc"      // exported, with explicit type

main() {
    print(APP_NAME)
    print(PI * 2.0)
}
```

Transpiles to:

```go
const internalRate = 0.05
const PI = 3.14159
const MAX_RETRIES int = 3
const APP_NAME string = "Zinc"

func main() {
    fmt.Println(APP_NAME)
    fmt.Println(PI * 2.0)
}
```

> **Visibility rule:** `const` → unexported (lowercase in Go). `pub const` → exported (uppercase in Go).

## Functions

```zinc
Int add(Int a, Int b) {
    return a + b
}

pub String greet(String name) {
    return "Hello, {name}!"
}
```

On the C# backend, all top-level functions are emitted as static methods inside a single `Functions` class. The `main()` function becomes `Program.Main()`:

```csharp
// Generated C#:
public static class Functions
{
    private static int Add(int a, int b) { return (a + b); }
    public static string Greet(string name) { return $"Hello, {name}!"; }
}
```

### Default Parameter Values

Parameters may declare a default value with `= expr`. Callers that omit the argument receive the default — inlined by the transpiler into the emitted Go call site (no runtime overhead):

```zinc
greet(String name, String greeting = "Hello") {
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
connect(String host, Int port = 8080, Bool tls = false) { }

main() {
    connect("localhost")                       // both defaults used
    connect("example.com", port: 443, tls: true)  // named, positional host
    connect(tls: true, host: "example.com")    // fully named, reordered
}
```

Named arguments also work on constructors:

```zinc
Dog {
    pub String name
    pub Int age

    new(String name, Int age = 0) {
        this.name = name
        this.age = age
    }
}

main() {
    var d1 = Dog("Rex")              // age defaults to 0
    var d2 = Dog("Buddy", 3)         // explicit
    var d3 = Dog(name: "Max")        // named, age defaults
    var d4 = Dog(age: 5, name: "Spot") // named, reordered
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
T identity<T>(T val) {
    return val
}

K pair<K, V>(K key, V value) {
    return key
}
```

### Variadic Functions

Functions can accept a variable number of arguments using `Type...` syntax — the last parameter becomes a variadic parameter:

```zinc
log(String level, String... msgs) {
    for msg in msgs {
        print("[{level}] {msg}")
    }
}

main() {
    log("INFO", "server started", "listening on :8080")

    // Spread a list into variadic args
    var errors = ["timeout", "connection refused"]
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

Fields are **private by default** — accessible only within the class. Prefix with `pub` to make a field public. Public fields generate getter/setter methods in the auto-generated interface, enabling access through polymorphic (interface-typed) references.

```zinc
Dog {
    pub String name       // public — generates GetName()/SetName() in the Dog interface
    pub Int age           // public
    String secret         // private — no getter/setter, only accessible inside Dog

    new(String name, Int age = 0) {
        this.name = name
        this.age = age
        this.secret = "shhh"
    }

    pub String bark() {
        return "{this.name} says: Woof!"
    }

    pub static Dog create(String name) {
        return Dog(name)
    }
}
```

### Named Constructors

Every class has one primary constructor declared with `new(...)`, called as `ClassName(...)`. Additional named constructors are `pub static` factory methods that call the constructor internally:

```zinc
Point {
    pub Float x
    pub Float y

    new(Float x, Float y) {
        this.x = x
        this.y = y
    }

    // Named constructor — origin
    pub static Point origin() {
        return Point(0.0, 0.0)
    }

    // Named constructor — from a single value
    pub static Point diagonal(Float v) {
        return Point(v, v)
    }
}

main() {
    var a = Point(3.0, 4.0)   // primary constructor
    var b = Point.origin()         // named constructor
    var c = Point.diagonal(5.0)    // named constructor
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
    pub T value

    new(T v) {
        this.value = v
    }

    pub T get() {
        return this.value
    }
}

main() {
    // Type inference — Go infers T from the argument
    var intBox = Box(42)        // Box<Int>
    var strBox = Box("hello")   // Box<String>
    print(intBox.get())             // 42
    print(strBox.get())             // hello
}
```

Generic classes can use empty list/map literals in constructors — the type is inferred from the field declaration:

```zinc
Stack<T> {
    pub List<T> items

    new(T initial) {
        this.items = []             // inferred as []T{}, not []interface{}{}
        this.items.add(initial)
    }

    pub push(T item) {
        this.items.add(item)
    }

    pub Int count() {
        return this.items.size()
    }
}

main() {
    var s = Stack(1)
    s.push(2)
    s.push(3)
    print(s.count())    // 3
}
```

Multi-type-parameter generic classes:

```zinc
Pair<K, V> {
    pub K key
    pub V val

    new(K key, V val) {
        this.key = key
        this.val = val
    }
}

main() {
    var p = Pair("name", 42)
    print(p.key)    // name
    print(p.val)    // 42
}
```

### Go Type Construction

Zinc uses the same constructor pattern for Go types — call the type like a function:

```zinc
import "sync"
import "bytes"
import "net/url"

main() {
    // Zero-value construction
    var mu = sync.Mutex()
    var buf = bytes.Buffer()

    // Named field construction (just like named args)
    var u = url.URL(Scheme: "https", Host: "example.com", Path: "/api")
    print(u.String())   // https://example.com/api
}
```

Transpiles to idiomatic Go struct literals:

```go
mu := sync.Mutex{}
buf := bytes.Buffer{}
u := url.URL{Scheme: "https", Host: "example.com", Path: "/api"}
```

#### Pointer Inference

Many Go APIs expect pointer-to-struct parameters (`*tls.Config`, `*http.Server`, etc.). Zinc automatically infers when `&` is needed — you never write pointer syntax:

```zinc
import "net/http"
import "crypto/tls"

main() {
    // http.Server.TLSConfig is *tls.Config — Zinc auto-emits &tls.Config{...}
    var s = http.Server(TLSConfig: tls.Config(MinVersion: 3))

    // tls.Dial's 3rd param is *tls.Config — auto-emits &tls.Config{}
    var conn = tls.Dial("tcp", "example.com:443", tls.Config())

    // No pointer context — emits value (tls.Config{})
    var cfg = tls.Config(MinVersion: 3)
}
```

The transpiler uses `go/types` to inspect Go function signatures and struct field types at transpile time. When a Go type construction appears as:
- A **function argument** where the parameter is a pointer type → emits `&Type{}`
- A **struct field value** where the field is a pointer type → emits `&Type{}`
- A **variable assignment** with no type context → emits `Type{}` (safe default)

## Interfaces

```zinc
interface Speaker {
    pub String speak()
}

Cat : Speaker {
    pub String speak() {
        return "Meow!"
    }
}
```

## Inheritance

```zinc
Animal {
    pub String name
    new(String name) { this.name = name }
    pub String describe() { return "Animal: {this.name}" }
}

Dog : Animal, Speaker {
    new(String name) {
        super(name)
    }
    pub String speak() { return "Woof!" }
}
```

## Polymorphism

Zinc classes support true OO polymorphism. A function that accepts a class or interface type can receive any subclass:

```zinc
interface Speaker {
    pub String speak()
}

Animal {
    pub String name
    new(String n) { this.name = n }
}

Dog : Animal, Speaker {
    new(String n) { super(n) }
    pub String speak() {
        return "{this.name} says Woof!"
    }
}

printSpeak(Speaker s) {
    print(s.speak())
}

main() {
    var d = Dog("Rex")
    printSpeak(d)         // Rex says Woof!
}
```

This works because each class generates a Go interface. `Dog` satisfies both the `Animal` interface and the `Speaker` interface, so it can be passed to any function expecting either type.

Field access through interface-typed parameters uses auto-generated getters:

```zinc
greet(Person p) {
    print("Hello, {p.name}")  // uses p.GetName() under the hood
}
```

Error handling works seamlessly through polymorphic dispatch. Failable methods on interface-typed parameters are correctly detected:

```zinc
Validator {
    pub Int value
    new(Int v) { this.value = v }
    pub String validate() {
        if this.value < 0 { return Error("negative") }
        return "ok"
    }
}

check(Validator v) {
    var result = v.validate() or {
        print("error: {err}")
        return
    }
    print(result)
}
```

Generic classes also work through interface-typed parameters:

```zinc
Pair<K, V> {
    pub K key
    pub V val
    new(K key, V val) { this.key = key; this.val = val }
}

printKey(Pair<String, Int> p) {
    print(p.key)   // uses p.GetKey() under the hood
}

main() {
    var p = Pair("hello", 42)
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
    var nums = [1, 2, 3]             // inferred as []int
    var names = ["Alice", "Bob"]     // inferred as []string
    var scores = {"math": 95, "sci": 88}  // inferred as map[string]int

    // Mixed types fall back to interface{}
    var mixed = [1, "two", 3]        // []interface{}

    // Empty literals use the declared type
    Map<String, Int> m = {}     // map[string]int{}
    List<Int> l = []            // []int{}

    // Nested collections work too
    var grid = [[1, 2], [3, 4]]      // [][]int
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
var nums = [1, 2, 3, 4, 5]

// Bracket syntax — [low:high], either bound optional
print(nums[1:3])    // [2 3]
print(nums[2:])     // [3 4 5]
print(nums[:3])     // [1 2 3]

// OO method — .slice(start, end) or .slice(start)
print(nums.slice(1, 3))   // [2 3]
print(nums.slice(2))      // [3 4 5]

// Assign slices to new variables
var firstTwo = nums[:2]        // [1 2]
var middle = nums.slice(1, 4)  // [2 3 4]

// Works on strings too
var s = "Hello, Zinc!"
print(s[0:5])          // Hello
print(s.slice(7))      // Zinc!
var word = s[7:11]     // Zinc
```

Transpiles directly to Go slice expressions:

```go
firstTwo := nums[:2]
middle := nums[1:4]
s[0:5]
s[7:]
word := s[7:11]
```

## Collection Methods (C# Backend)

When targeting the C# AOT backend, Zinc supports LINQ-style collection methods that map directly to C# LINQ:

```zinc
main() {
    var nums = [5, 3, 8, 1, 9, 2, 7, 4, 6]

    // Filtering
    var evens = nums.Where((Int x) -> x % 2 == 0)          // [8, 2, 4, 6]

    // Transformation
    var doubled = nums.Select((Int x) -> x * 2)             // [10, 6, 16, ...]

    // Sorting
    var sorted = nums.OrderBy((Int x) -> x)                 // [1, 2, 3, ...]
    var desc = nums.OrderByDescending((Int x) -> x)         // [9, 8, 7, ...]

    // Querying
    var first = nums.First((Int x) -> x > 7)                // 8
    var hasNeg = nums.Any((Int x) -> x < 0)                 // false
    var allPos = nums.All((Int x) -> x > 0)                 // true

    // Aggregation
    var total = nums.Sum()                                   // 45
    var lo = nums.Min()                                      // 1
    var hi = nums.Max()                                      // 9
    var count = nums.Count((Int x) -> x > 5)                // 4
    var product = nums.Aggregate(1, (Int a, Int x) -> a * x)

    // Subsetting
    var top3 = nums.Take(3)                                  // [5, 3, 8]
    var rest = nums.Skip(3)                                  // [1, 9, 2, 7, 4, 6]
    var unique = [1, 2, 2, 3].Distinct()                     // [1, 2, 3]

    // Chaining — compose multiple operations
    var result = nums.Where((Int x) -> x % 2 == 0)
                     .Select((Int x) -> x * x)
                     .OrderBy((Int x) -> x)
                     .Take(3)
}
```

### Full Method Reference

| Method | Description | Example |
|--------|-------------|---------|
| `Where(predicate)` | Filter elements | `nums.Where((x) -> x > 5)` |
| `Select(transform)` | Transform elements | `nums.Select((x) -> x * 2)` |
| `First()` / `First(predicate)` | First element (or first matching) | `nums.First((x) -> x > 5)` |
| `FirstOrDefault(predicate)` | First matching or default | `nums.FirstOrDefault((x) -> x > 100)` |
| `Last()` / `Last(predicate)` | Last element | `nums.Last()` |
| `Any()` / `Any(predicate)` | True if any match | `nums.Any((x) -> x < 0)` |
| `All(predicate)` | True if all match | `nums.All((x) -> x > 0)` |
| `Count()` / `Count(predicate)` | Count (optionally with filter) | `nums.Count((x) -> x > 5)` |
| `Sum()` / `Sum(selector)` | Sum values | `nums.Sum()` |
| `Min()` / `Max()` | Minimum / maximum | `nums.Min()` |
| `Average()` | Average value | `nums.Average()` |
| `Aggregate(seed, func)` | Fold / reduce | `nums.Aggregate(0, (a, x) -> a + x)` |
| `OrderBy(key)` | Sort ascending | `nums.OrderBy((x) -> x)` |
| `OrderByDescending(key)` | Sort descending | `nums.OrderByDescending((x) -> x)` |
| `Take(n)` | First n elements | `nums.Take(3)` |
| `Skip(n)` | Skip first n | `nums.Skip(3)` |
| `Distinct()` | Remove duplicates | `nums.Distinct()` |
| `SelectMany(func)` | Flatten nested collections | `lists.SelectMany((x) -> x)` |
| `GroupBy(key)` | Group by key | `items.GroupBy((x) -> x.category)` |
| `Zip(other, func)` | Combine two lists | `a.Zip(b, (x, y) -> x + y)` |
| `ToDictionary(key, value)` | Convert to map | `items.ToDictionary((x) -> x.id, (x) -> x)` |
| `ToList()` | Materialize to list | `query.ToList()` |
| `ForEach(action)` | Execute action per element | `nums.ForEach((x) -> x * 2)` |

> **Note:** Collection methods are available on the C# backend. On the Go backend, use `for` loops for equivalent operations.

## Match / Switch

```zinc
enum Direction { North, South, East, West }

String describe(Direction d) {
    match d {
        case Direction.North -> { return "Going North" }
        case Direction.South -> { return "Going South" }
        case Direction.East  -> { return "Going East"  }
        case Direction.West  -> { return "Going West"  }
        case _ -> { return "Unknown" }
    }
}
```

## String Interpolation

```zinc
var name = "Zinc"
var version = 1
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
for (var i = 0; i < 10; i += 1) {
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
var scores = {"Alice": 95, "Bob": 87}
for (name, score) in scores {
    print("{name} got {score}")
}
```

### Labeled Loops

Like Java, Zinc supports labeled `break` and `continue` for nested loop control. Prefix a loop with `@label` and reference it from inner loops:

```zinc
@outer for (var i = 0; i < 10; i += 1) {
    for (var j = 0; j < 10; j += 1) {
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
    pub String name
    pub Address? address

    new(String name, Address? addr) {
        this.name = name
        this.address = addr
    }
}

Address {
    pub String city
    new(String city) { this.city = city }
}

main() {
    User? user = User("Alice", Address("NYC"))

    // Field access — returns nil if user is nil
    var name = user?.name           // "Alice"

    // Chaining — each ?. short-circuits independently (like Kotlin)
    var city = user?.address?.city   // "NYC"

    // Method call — skipped if receiver is nil
    user?.doSomething()

    // Nil receiver — no crash
    User? nobody = null
    var x = nobody?.name             // nil
    var y = nobody?.address?.city    // nil
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
// name := user?.name  →
name := func() interface{} { if user != nil { return user.Name }; return nil }()
```

**Chained expressions** — `a?.b?.c` generates a single flat function with sequential nil checks (no nested wrappers):

```go
// city := user?.address?.city  →
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
    Any x = 42

    // Type assertion — panics if wrong type (like Kotlin's `as`)
    var n = x as Int
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
    pub String name
    new(String name) { this.name = name }
}

main() {
    var d = Dog("Rex")
    print(d.name)         // OK — d is non-nullable, use regular dot

    Dog? d2 = null
    print(d2?.name)       // OK — d2 is nullable, use ?.
    // print(d2.name)     // ERROR: "use '?.' for safe access on nullable type"
    // Dog d3 = null      // ERROR: "cannot assign null to non-nullable type"
}
```

## Callable Function Types (`Fn`)

Use `ReturnType Fn(ParamTypes)` to declare typed function parameters — enabling higher-order functions, callbacks, and functional patterns:

```zinc
Int apply(Int Fn(Int) f, Int x) {
    return f(x)
}

Int combine(Int Fn(Int, Int) f, Int a, Int b) {
    return f(a, b)
}

run(Fn() callback) {
    callback()
}

main() {
    var double = (Int x) -> x * 2
    print(apply(double, 7))       // 14

    var add = (Int a, Int b) -> a + b
    print(combine(add, 3, 4))     // 7

    run(() -> { print("done") })

    // Also works as variable type annotations
    Int Fn(String) transform = (String s) -> s.size()
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

Lambdas use the `(Type param) -> body` syntax. The body is either a
single expression or a block `{ ... }`.

```zinc
// Single-expression lambda (inferred as a func literal)
var double = (Int x) -> x * 2
var greet  = () -> "Hello!"

// Block-body lambda
var describe = (Int x) -> {
    if x > 0 {
        return "positive"
    }
    return "non-positive"
}

// Closure capture — lambda body may reference outer variables
var base   = 100
var addBase = (Int x) -> x + base

// String interpolation works inside lambda bodies
var makeMsg = (String name) -> "Hello, {name}!"
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
var safeDivide = (Int a, Int b) -> {
    if b == 0 {
        return Error("division by zero")
    }
    return a / b
}

var result = safeDivide(10, 2)   // auto-propagates error
print(result)

var bad = safeDivide(10, 0) or {
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
    with (f = os.Stdin) {
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
    with (f = os.Create("output.txt")) {
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
with (f = os.Open("/nonexistent/file") or {
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
    var counter = 0
    with (mu = sync.Mutex()) {
        counter += 1    // mutex locked here, unlocked when block exits
    }
}
```

### Multiple Resources

Comma-separated resources are closed in reverse order (LIFO), matching Go's `defer` stack:

```zinc
import "os"

main() {
    with (f1 = os.Create("a.txt"), f2 = os.Create("b.txt")) {
        f1.WriteString("file A")
        f2.WriteString("file B")
    }
    // f2 closes first, then f1
}
```

## Built-in Functions

Zinc provides global built-in functions that work on both backends. The transpiler automatically adds the required imports.

### Type Conversions

```zinc
var s = toString(42)           // "42"
var n = toInt("42")            // 42 (alias: parseInt)
var f = toFloat("3.14")       // 3.14 (alias: parseFloat)
var b = toBool("true")        // true
var t = typeOf(42)             // "Int32" (C#) or "int" (Go)
```

### Math

```zinc
var a = abs(-7)                // 7
var s = sqrt(16.0)             // 4
var p = pow(2.0, 10.0)         // 1024
var f = floor(3.7)             // 3
var c = ceil(3.2)              // 4
var r = round(3.5)             // 4
var hi = max(3, 7)             // 7
var lo = min(3, 7)             // 3
```

### I/O and Files

```zinc
var line = readLine()                          // read from stdin

var content = readFile("data.txt") or {        // failable
    print("Error: {err}")
    exit(1)
}

writeFile("out.txt", "hello") or {             // failable
    print("Write failed: {err}")
}
```

### JSON

```zinc
var json = jsonEncode(42)                      // "42"
var val = jsonDecode<Int>(json)                // 42
```

### HTTP

```zinc
var body = httpGet("https://example.com") or { // failable
    print("Request failed: {err}")
    exit(1)
}
```

### Environment & Time

```zinc
setEnv("APP_MODE", "production")
var mode = getEnv("APP_MODE")                  // "production"
var timestamp = now()                           // current time as string
sleep(1000)                                     // pause 1 second
```

### String Formatting

```zinc
// Go backend: uses %s/%d format verbs
// C# backend: uses {0}/{1} placeholders
var msg = sprintf("{0} is {1}", "age", 30)     // "age is 30"
```

### Control

```zinc
panic("something went wrong")                  // throw/panic — halts execution
exit(1)                                         // exit with code
```

> See [builtins.md](builtins.md) for the complete reference with backend-specific output.

## Error Handling

Zinc uses errors as values with auto-propagation — no try/catch needed:

```zinc
Int divide(Int a, Int b) {
    if b == 0 {
        return Error("division by zero")
    }
    return a / b
}

main() {
    // Auto-propagation: panics in main if error occurs
    var result = divide(10, 2)
    print(result)

    // Or handler: add context, log, and halt
    var bad = divide(10, 0) or {
        print("caught: {err}")
        exit(0)
    }
}
```

## Concurrency

```zinc
main() {
    Chan<Int> ch = Chan(1)

    go {
        ch.send(42)
    }

    var val = ch.receive()
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

> **Note:** Both names in `var (a, b) = ...` must be used. If you only need one value, assign the other to `_`.

## Imports

Zinc uses `import` statements to bring in external packages. On the C# backend, imports map to `using` directives. On the Go backend, they map to Go imports.

### .NET Namespace Imports (C# Backend)

```zinc
import "System.Text.Json"           // → using System.Text.Json;
import "Newtonsoft.Json"             // → using Newtonsoft.Json;
import "Serilog"                     // → using Serilog;

// Short aliases for common namespaces
import "http"                        // → using System.Net.Http;
import "json"                        // → using System.Text.Json;
import "io"                          // → using System.IO;
import "regex"                       // → using System.Text.RegularExpressions;
import "threading"                   // → using System.Threading;
import "tasks"                       // → using System.Threading.Tasks;

main() {
    // Use imported types directly
    var s = JsonSerializer.Serialize(42)
    print(s)
}
```

Imported .NET types are automatically recognized. Constructor calls emit `new`:

```zinc
import "System.Diagnostics"
import "http"
import "System.Text"

main() {
    var sw = Stopwatch()           // → new Stopwatch()
    var client = HttpClient()      // → new HttpClient()
    var sb = StringBuilder()       // → new StringBuilder()

    sw.Start()
    // ...
    sw.Stop()
}
```

Static classes (`Console`, `Math`, `File`, etc.) are detected automatically and don't receive `new`.

NuGet packages are declared in `zinc.toml` under `[dependencies]` — they become `<PackageReference>` entries in the generated `.csproj`.

### Go Package Imports (Go Backend)

```zinc
import "os"
import "math/rand" as rand

main() {
    Any args = os.Args
}
```

### Local Package Imports

```zinc
import "myapp/utils"                 // cross-file import (handled by TypeRegistry)
```

Local imports (paths containing `/`) are resolved by the build system — all `.zn` files in a directory share a namespace.

## Type System

| Zinc     | Go          | C#            |
|-------------|-------------|---------------|
| `Int`       | `int`       | `int`         |
| `Float`     | `float64`   | `double`      |
| `String`    | `string`    | `string`      |
| `Bool`      | `bool`      | `bool`        |
| `Byte`      | `byte`      | `byte`        |
| `Any`       | `interface{}`| `object`     |
| `Error`     | `error`     | `Exception`   |
| `String?`   | `*string`   | `string?`     |
| `List<T>`   | `[]T`       | `List<T>`     |
| `Map<K,V>`  | `map[K]V`   | `Dictionary<K,V>` |
| `Chan<T>`   | `chan T`    | `Channel<T>`  |
