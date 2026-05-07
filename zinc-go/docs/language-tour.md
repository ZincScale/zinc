# Language Tour

A feature-by-feature walk-through of Zinc, with runnable examples.
Every snippet here corresponds to working code in `examples/` — the
e2e suite compiles and runs all of them.

## Hello, World

```zinc
String greet(String name) {
    return "Hello, ${name}!"
}

void main() {
    print(greet("World"))
    int x = 42
    print("The answer is ${x}")
}
```

- Top-level statements are wrapped in `main()` for short scripts; once
  you declare a `void main()`, that becomes the entry point.
- `pub` makes a top-level binding exported (capitalized in the
  generated Go). Without it, the binding is package-private. The `main`
  function is never `pub`.
- Type-first declarations: `String greet(String name)`, no `fn`
  keyword, in the Java/C#/Dart shape.

## Variables

```zinc
var name = "Alice"              // inferred — `var` requires an initializer
String greeting = "Hello"       // explicit type, no `var`
String host                     // explicit type, no initializer
const PI = 3.14159              // constant, inferred
const String VERSION = "1.0"    // constant, explicit type
```

The hybrid `var Type name` is rejected — pick one form or the other.

## Functions

```zinc
pub int add(int a, int b) {
    return a + b
}

// Single-expression form: no braces, no return keyword
pub int doubled(int x) = x * 2

// Default parameters
pub String greet(String name, String greeting = "Hello") {
    return "${greeting}, ${name}!"
}

// Variadic parameters
pub int sum(int... numbers) {
    var total = 0
    for (n in numbers) { total = total + n }
    return total
}

// Spread at the call site
void wrapper(String msg, any... args) {
    logMsg("INFO", msg, args...)
}
```

## String interpolation

```zinc
var name = "World"
print("Hello, ${name}!")
print("2 + 2 = ${2 + 2}")
print("upper: ${name.toUpperCase()}")
```

Backtick raw strings disable interpolation:

```zinc
var pattern = `\d+\s+\w+`
```

## Control flow

```zinc
// if / else if / else — parens required on the header
if (x > 0)        { print("positive") }
else if (x == 0)  { print("zero") }
else              { print("negative") }

// Expression if (ternary)
var label = if x > 0: "positive" else: "non-positive"

// match — exhaustive on sealed types, otherwise needs `case _`
match (cmd) {
    case "start" { print("starting") }
    case "stop"  { print("stopping") }
    case _       { print("unknown") }
}

// match expression — every arm produces a value
var status = match code {
    case 0 { "ok" }
    case 1 { "warn" }
    case _ { "err" }
}

// for — collection iteration
for (item in list) { print(item) }

// for — map destructure
for (k, v in scores) { print("${k}=${v}") }

// for — ranges
for (i in 0..10)  { ... }       // exclusive: 0..9
for (i in 0..=10) { ... }       // inclusive: 0..10

// while
while (cond) { doWork() }

// break / continue work as expected
```

## Collections

```zinc
List<int> numbers = [1, 2, 3, 4, 5]
numbers.add(6)
print(numbers[0])
print("size: ${numbers.size()}")

Map<String, int> ages = {"Alice": 30, "Bob": 25}
ages.put("Carol", 28)
ages.containsKey("Bob")
ages.delete("Alice")

for (key, value in ages) {
    print("${key} is ${value}")
}

int[] nums = [1, 2, 3]      // fixed-size array
print(nums.length)
```

There is no streams API (`.map`, `.filter`, ...) — write the loop, the
way you would in idiomatic Go.

## Classes

```zinc
class Server {
    pub String host
    pub int port

    init(String host, int port = 8080) {
        this.host = host
        this.port = port
    }

    pub String address() {
        return "${host}:${port}"
    }

    pub String toString() {
        return "Server(${address()})"
    }
}

var s = Server("localhost", 3000)
print(s.address())   // localhost:3000
print(s)             // Server(localhost:3000)
```

`toString()` is honored by `print()` and string interpolation.

### Visibility

`pub` exports the class, field, or method. Without it, the binding is
package-private (lowercase Go name).

### Implicit-self

Inside a method body, bare names resolve through the receiver:

```zinc
class Counter {
    int value

    init() { value = 0 }                     // bare ident → this.value

    int privateAdd(int n) { return value + n }

    pub int doublePrivate(int n) {
        return privateAdd(n) * 2             // bare call → this.privateAdd
    }
}
```

You can still write `this.foo` explicitly. It's just rarely necessary.

### Inheritance

Single inheritance with `super(...)` constructor chaining:

```zinc
class Animal {
    String name
    String sound
    init(String name, String sound) {
        this.name = name
        this.sound = sound
    }
    String speak() { return "${name} says ${sound}" }
}

class Dog : Animal {
    String breed
    init(String name, String breed) {
        super(name, "Woof")
        this.breed = breed
    }
    String toString() { return "Dog(${name}, ${breed})" }
}

var dog = Dog("Rex", "Lab")
print(dog.speak())   // Rex says Woof  (inherited)
print(dog)           // Dog(Rex, Lab)
```

Multi-level inheritance and polymorphism work as expected:

```zinc
class Vehicle { ... }
class Car : Vehicle { ... }
class ElectricCar : Car { ... }

Vehicle v = ElectricCar(...)
```

### Interfaces

```zinc
interface Greeter {
    String greet(String name)
}

class FormalGreeter : Greeter {
    String greet(String name) {
        return "Good day, ${name}."
    }
}

Greeter g = FormalGreeter()
print(g.greet("World"))
```

A class can extend one parent and implement multiple interfaces:
`class Truck : Vehicle, Printable, Loadable`.

## Data classes

Auto-generated record types with `toString()` and structural equality:

```zinc
data User(String name, String email, int age = 0)

var u = User("Alice", "alice@example.com", 30)
print(u)    // User(name=Alice, email=alice@example.com, age=30)
```

Generic data classes:

```zinc
data Pair<A, B>(A first, B second)
```

## Sealed classes & pattern matching

Closed hierarchies for exhaustive pattern matching:

```zinc
sealed class Shape {
    data Circle(double radius)
    data Rect(double width, double height)
    data Triangle(double base, double height)
}

pub double area(Shape s) {
    match (s) {
        case Circle(r)      { return 3.14159 * r * r }
        case Rect(w, h)     { return w * h }
        case Triangle(b, h) { return 0.5 * b * h }
    }
    return 0.0
}

var shapes = [Circle(1.0), Rect(2.0, 3.0), Triangle(4.0, 5.0)]
for (s in shapes) {
    print("${s} → area=${area(s)}")
}
```

Match on a sealed type is **exhaustive** — missing a variant is a
compile-time error.

## Errors as values

A function whose declared return type ends in `error` is a thrower —
`error` (bare), `(T, error)`, or `(T1, ..., Tn, error)`.

```zinc
import stdlib/errors

pub (int, error) parseNum(String s) {
    if (s == "") {
        return errors.IllegalArgumentError("empty input")
    }
    return 42, null
}

pub error validate(String input) {
    if (input == "bad") {
        return errors.IllegalArgumentError("bad input")
    }
    return null
}
```

### Handle at the call site with `catch`

```zinc
void main() {
    var n = parseNum("hello") catch { print("caught: ${err}"); return }
    print("ok: ${n}")
}
```

Inside the `catch { }` block, `err` is bound to the error value. The
block must terminate the binding's scope (typically `return`, `break`,
`continue`) or supply a fallback value:

```zinc
// Fallback value
var port = parsePort(s) catch { 8080 }

// Update an existing local
n = strconv.Atoi("42")           catch { 0 }
m = strconv.Atoi("not a number") catch { -1 }

// Wrap and re-throw (works inside another thrower)
var cfg = loadConfig(path) catch {
    return errors.ConfigError("loading ${path} failed: ${err}")
}
```

### Propagate

There is no implicit propagation and no `?` operator. To forward an
error, the surrounding function must itself be a thrower, and the call
site spells it out:

```zinc
pub (int, error) doubleIt(String s) {
    var n = parseNum(s) catch { return err }
    return n * 2, null
}
```

`return err` from a multi-value thrower auto-fills the value slots
with their zero values, so you don't repeat `return 0, "", err`.

### Multi-value destructure

```zinc
pub (int, String, error) lookup(String key) { ... }

var (n, label) = lookup("foo") catch {
    print("lookup err: ${err}")
    return
}
```

### Custom errors

Extend `errors.BaseError`:

```zinc
import stdlib/errors

class ParseError : errors.BaseError {
    init(String message) { super(message) }
}

class NetworkError : errors.BaseError {
    pub int statusCode
    init(String message, int statusCode) {
        super(message)
        this.statusCode = statusCode
    }
}
```

Anything transitively extending `BaseError` satisfies Go's `error`
interface (via `BaseError.Error() string`) and composes with
`errors.Is`, `errors.As`, and `fmt.Errorf("%w", ...)` wrapping.

See [error-handling.md](error-handling.md) for the full model.

## `using` — deterministic resource cleanup

```zinc
class Resource {
    pub String name
    init(String n) { this.name = n }
    pub void Close() { print("closing ${name}") }
    pub void work() { print("working on ${name}") }
}

void main() {
    using (var r = Resource("r1")) {
        r.work()
    }   // Close() runs here

    // Multi-resource — closed in reverse order
    using (var a = Resource("a"), var b = Resource("b")) {
        a.work()
        b.work()
    }
}
```

`using` is **block-scoped**, not function-scoped — `Close()` runs
*before* the surrounding block's return value is observed by the
caller. That's the property `defer` doesn't have, and it's why you
can flush a `bytes.Buffer.Close()` and then read the buffer in the
same function.

## Generics

Type parameters on functions and classes, mapped 1:1 to Go generics:

```zinc
// Generic function
pub T identity<T>(T x) { return x }

var n = identity<int>(42)
var s = identity<String>("hi")

// Multi-parameter
pub String swap<A, B>(A a, B b) {
    return "${b}, ${a}"
}

// Generic class
class Box<T> {
    pub T value
    init(T v) { this.value = v }
    pub T get() { return value }
}

var b = Box<int>(7)

// Generic interface + class implementing it
interface Queue<T> {
    void push(T item)
    T pop()
    int size()
}

class SimpleQueue<T> : Queue<T> {
    List<T> items = []
    pub void push(T item) { items = append(items, item) }
    pub T pop() {
        var item = items[0]
        items = items[1:]
        return item
    }
    pub int size() { return len(items) }
}

// Generic data class
data Pair<A, B>(A first, B second)
```

Generated Go: `func identity[T any](x T) T { ... }`.

## Lambdas & function types

```zinc
// Function type
type Transform = Fn<(int), int>

pub int applyTwice(int x, Transform f) {
    return f(f(x))
}

// Lambda — single-expression form
var doubled = (int x) -> x * 2
print(applyTwice(3, doubled))            // 12

// Lambda — block-form (multi-statement)
apply(() -> {
    for (i in 0..3) { total = total + i }
    var i = 0
    while (i < 2) {
        total = total + 100
        i = i + 1
    }
})

// Type alias for any type
type Handler = Fn<(String), String>
type IntList = List<int>
```

Function-type aliases also carry trailing-`error` thrower-ness:

```zinc
type ProcessorFactory = Fn<(Config), (Processor, error)>
```

## Concurrency

Zinc surfaces Go's concurrency primitives directly.

```zinc
// spawn — fire a goroutine
spawn { doWork() }

// Channel
var ch = Channel<String>(10)
ch.send("hello")
var msg = ch.recv()
ch.close()

// parallel for — concurrent iteration with implicit WaitGroup
List<String> urls = ["a", "b", "c"]
parallel for (url in urls) {
    fetch(url)
}

// select — multiplex over channel ops, maps 1:1 to Go's select
select {
    case msg = ch.recv():
        print("got: ${msg}")
    case out.send("hi"):
        print("sent")
    case _:
        print("nothing ready")
}

// timeout
import time
timeout(2 * time.Second) {
    longRunning()
} catch {
    print("deadline exceeded")
}
```

| Zinc | Go |
|------|-----|
| `spawn { ... }` | `go func() { ... }()` |
| `var ch = Channel<T>(n)` | `ch := make(chan T, n)` |
| `ch.send(val)` | `ch <- val` |
| `ch.recv()` | `<-ch` |
| `parallel for (x in xs) { ... }` | `sync.WaitGroup` + per-iteration goroutine |
| `select { case x = ch.recv(): ... }` | `select { case x := <-ch: ... }` |
| `select { case _: ... }` | `select { default: ... }` |
| `timeout(d) { ... } catch { ... }` | `select` on `time.After(d)` |

See [concurrency.md](concurrency.md) for more.

## Imports & FFI

Zinc imports Go packages directly:

```zinc
import time
import strings
import strconv
import net/http
import encoding/json

var upper = strings.ToUpper("hello")
var n = strconv.Atoi("42") catch { 0 }
http.HandleFunc("/", helloHandler)
```

Zinc subpackages and stdlib use slash-separated paths:

```zinc
import stdlib/errors
import stdlib/asserts
import store               // sibling subpackage in this project
```

Third-party Go modules go through `zinc.toml`:

```toml
[deps]
mux = "github.com/gorilla/mux@v1.8.1"
```

```zinc
import mux
mux.NewRouter()
```

### Auto-pointerization

Zinc fields typed as a Go struct with pointer-receiver methods
auto-pointerize to `*T` in the generated Go, *and* the constructor
news up the value:

```zinc
import sync
import bytes

class Counter {
    sync.Mutex mu             // → *sync.Mutex, init'd to &sync.Mutex{}
    int n

    pub void inc() {
        lock (mu) { n = n + 1 }
    }
}

class Holder {
    bytes.Buffer buf          // → *bytes.Buffer
    init() { this.buf = bytes.Buffer{} }
    pub void writeIn(String s) {
        this.buf = bytes.Buffer{}
        this.buf.WriteString(s)
    }
}
```

Zinc also auto-inserts `&` when passing a value to a Go function whose
signature is `*T`. The only place you write `&` yourself is when the
Go signature is `any` / `interface{}` but the runtime contract requires
a pointer (canonical case: `json.Unmarshal`):

```zinc
import encoding/json

var p = Person("", 0)
json.Unmarshal(data, &p) catch { return }
```

See [interop-with-go.md](interop-with-go.md) for the full rules.

## Equality & nullability

```zinc
var a = "hello"
var b = "hello"
print(a == b)    // structural equality — true
print(a === b)   // reference identity

// Nullable types
String? find(String id) {
    if (id == "1") { return "Alice" }
    return null
}

var user = find("1")
var len = user?.length()        // safe navigation
if (user is String) { print("found") }
```

## Enums

```zinc
enum Color { Red, Green, Blue }

var c = Red

match (c) {
    case Red   { print("red") }
    case Green { print("green") }
    case _     { print("other") }
}
```

## Constants

```zinc
const String VERSION = "1.0"
const PI = 3.14159
```

## What Zinc deliberately *doesn't* have

- No streams API. Write loops.
- No exceptions / `try / catch / throw / finally`. Errors are values.
- No `defer`. Use `using`.
- No `?` operator. Errors propagate via `catch { return err }`.
- No reflection-based serialization sugar. Use Go libs (`encoding/json`,
  `hamba/avro`, etc.) directly.
- No lambdas with implicit args (no `it`). Spell out the parameters.

The principle: anywhere Go has a clean answer, Zinc surfaces it.
Anywhere Go's syntax is the friction, Zinc smooths it.

## Next steps

- [Why Zinc](why-zinc.md)
- [Getting Started](getting-started.md)
- [Interop with Go](interop-with-go.md)
- [Classes & Inheritance](classes.md)
- [Error Handling](error-handling.md)
- [Concurrency](concurrency.md)
