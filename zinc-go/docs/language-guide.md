# Language Guide

Zinc is a clean-syntax language that transpiles to Go. It removes Go's syntax warts while producing readable, idiomatic Go output.

## Variables

```zinc
var name = "Alice"              // inferred type (requires initializer)
String greeting = "Hello"       // explicit type (no `var` — bare form)
String host                     // explicit type, no initializer
const PI = 3.14159              // constant, inferred
const String VERSION = "1.0"    // constant, explicit type
```

Rule: `var` means "infer the type from the initializer." If you write the type explicitly, drop `var`. The hybrid `var Type name` is a compile error — write either `var name = expr` (inferred) or `Type name [= expr]` (explicit).

Primitive types: `int`, `long`, `double`, `float`, `bool`, `byte`, `String`. Container types: `List<T>`, `Map<K,V>`, fixed-size arrays `T[]`.

## Functions

```zinc
int add(int a, int b) {
    return a + b
}

// Single-expression form — no braces, no return
int doubled(int x) = x * 2

// Default parameters
String greet(String name, String greeting = "Hello") {
    return "${greeting}, ${name}!"
}

greet("Alice")          // "Hello, Alice!"
greet("Bob", "Hey")     // "Hey, Bob!"

// Variadic parameters
int sum(int... numbers) {
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

Double-quoted strings interpolate `${expr}`:

```zinc
var name = "World"
print("Hello, ${name}!")        // Hello, World!
print("2 + 2 = ${2 + 2}")       // 2 + 2 = 4
```

## Control flow

```zinc
// If / else if / else — parens required on the header
if (x > 0) {
    print("positive")
} else if (x == 0) {
    print("zero")
} else {
    print("negative")
}

// Expression if (ternary)
var label = if x > 0: "positive" else: "non-positive"

// Match — exhaustive on sealed types, otherwise needs `case _`
match (cmd) {
    case "start" { print("starting") }
    case "stop"  { print("stopping") }
    case _       { print("unknown") }
}

// Match expression — every arm produces a value
var status = match code {
    case 0 { "ok" }
    case 1 { "warn" }
    case _ { "err" }
}

// For — collection iteration
for (item in list) {
    print(item)
}

// For — map destructure
for (k, v in scores) {
    print("${k}=${v}")
}

// For — ranges
for (i in 0..10)  { ... }       // exclusive: 0..9
for (i in 0..=10) { ... }       // inclusive: 0..10

// While
while (cond) {
    doWork()
}

// break / continue work as expected
```

## Collections

```zinc
// Lists
List<int> numbers = [1, 2, 3, 4, 5]
var first = numbers[0]
numbers.add(6)
print("size: ${numbers.size()}")

// Maps
Map<String, int> ages = {"Alice": 30, "Bob": 25}
ages.put("Carol", 28)
var age = ages.get("Alice")
ages.containsKey("Bob")
ages.delete("Alice")
var keys = ages.keys()
var vals = ages.values()

// Iteration
for (key, value in ages) {
    print("${key} is ${value}")
}

// Fixed-size arrays
int[] nums = [1, 2, 3]
print(nums.length)
```

There is no streams API (`.filter`, `.map`, `.reduce`, ...). Write the loop — that's the idiomatic Go you'd hand-write anyway.

## Nullable types

```zinc
String? find(String id) {
    if (id == "1") { return "Alice" }
    return null
}

var user = find("1")

// Safe navigation — returns null if receiver is null
var len = user?.length()

// Type checks
if (user is String) {
    print("found")
}
```

## Equality

```zinc
var a = "hello"
var b = "hello"
print(a == b)    // structural equality — true
print(a === b)   // reference identity
```

## Enums

```zinc
enum Color {
    Red,
    Green,
    Blue
}

var c = Red

match (c) {
    case Red   { print("red") }
    case Green { print("green") }
    case _     { print("other") }
}
```

## Sealed classes

Algebraic data types with exhaustive pattern matching:

```zinc
sealed class Shape {
    data Circle(double radius)
    data Rect(double width, double height)
    data Triangle(double base, double height)
}

double area(Shape s) {
    match (s) {
        case Circle(r)    { return 3.14159 * r * r }
        case Rect(w, h)   { return w * h }
        case Triangle(b, h) { return 0.5 * b * h }
    }
    return 0.0
}
```

Match on a sealed type is exhaustive — the compiler rejects missing variants.

## Generics

Both functions and classes can be generic:

```zinc
// Generic function
T identity<T>(T x) {
    return x
}

var n = identity<int>(42)
var s = identity<String>("hi")

// Multi-parameter
String swap<A, B>(A a, B b) {
    return "${b}, ${a}"
}

// Generic single-expression form
T second<T>(List<T> items) = items[1]

// Generic class
class Box<T> {
    pub T value
    init(T v) { this.value = v }
    pub T get() { return value }
}

var b = Box<int>(7)

// Generic data class
data Pair<A, B>(A first, B second)
```

Type parameters map directly to Go type parameters (`func identity[T any](x T) T`).

## Function types & type aliases

```zinc
// Function types
type Transform = Fn<(int), int>

int applyTwice(int x, Transform f) {
    return f(f(x))
}

var result = applyTwice(3, (int x) -> x + 10)  // 23

// Lambdas
var doubled = (int x) -> x * 2

// Type aliases for any type
type Handler = Fn<(String), String>
type IntList = List<int>
```

## Imports

Go stdlib packages import directly:

```zinc
import time
import strings
import strconv

var upper = strings.ToUpper("hello")
var pi = math.Pi
```

Zinc subpackages and stdlib use slash-separated paths:

```zinc
import stdlib/errors
import stdlib/asserts
import store               // sibling subpackage in this project
```

External Go modules go through `zinc.toml`:

```toml
[deps]
mux = "github.com/gorilla/mux@v1.8.1"
```

```zinc
import mux

mux.NewRouter()
```

## Errors

See [error-handling.md](error-handling.md). The short version: any class extending `Err` is an error. Returning one auto-widens the function signature; handle with `or { }` at the call site or let it propagate.

## Concurrency

See [concurrency.md](concurrency.md). `spawn { }`, `Channel<T>(n)`, `parallel for`, and `select { case ... }` are all first-class.

## Next steps

- [Classes & Inheritance](classes.md)
- [Error Handling](error-handling.md)
- [Concurrency](concurrency.md)
