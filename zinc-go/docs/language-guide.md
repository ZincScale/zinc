# Language Guide

Zinc is a clean-syntax language that transpiles to Go. It removes Go's syntax warts while producing readable, idiomatic Go output.

## Variables

```zinc
var name = "Alice"              // inferred type (requires initializer)
String greeting = "Hello"       // explicit type (no `var` — bare form)
String host                     // explicit type, no initializer (fields only)
const PI = 3.14159              // constant, inferred
const String VERSION = "1.0"    // constant, explicit type
```

Rule: `var` means "infer the type from the initializer." If you write the type explicitly, drop `var`. The hybrid `var Type name` is a compile error — write either `var name = expr` (inferred) or `Type name [= expr]` (explicit).

Types: `int`, `double`, `bool`, `String`, `List<T>`, `Map<K,V>`.

## Functions

```zinc
fn add(int a, int b): int {
    return a + b
}

// Default parameters
fn greet(String name, String greeting = "Hello"): String {
    return "${greeting}, ${name}!"
}

greet("Alice")          // "Hello, Alice!"
greet("Bob", "Hey")     // "Hey, Bob!"
```

Constructors (`init`) always succeed — bare `return` inside an `init` body is a compile error. For failable construction, use a factory returning `T?`. See [classes](classes.md#constructors-always-succeed) and [error handling](error-handling.md#constructors-always-succeed--use-a-factory-for-failable-construction).

## String interpolation

Any double-quoted string with `${expr}` is interpolated automatically. Single-quoted strings are literal — no interpolation:

```zinc
var name = "World"
print("Hello, ${name}!")        // Hello, World!
print("2 + 2 = ${2 + 2}")      // 2 + 2 = 4
print('No interpolation: ${name}')  // No interpolation: ${name}
```

## Control flow

```zinc
// If/else
if (x > 0) {
    print("positive")
} else if (x == 0) {
    print("zero")
} else {
    print("negative")
}

// Match (exhaustive pattern matching)
match (direction) {
    case North { print("going north") }
    case South { print("going south") }
    case _ { print("other") }
}

// For loops
for (i in 0..10) {
    print(i)
}

for (item in list) {
    print(item)
}

// While
while (condition) {
    doWork()
}
```

## Collections

```zinc
// Lists
List<int> numbers = [1, 2, 3, 4, 5]
var first = numbers[0]
numbers.add(6)
print("size: {numbers.size()}")

// Maps
Map<String, int> ages = {"Alice": 30, "Bob": 25}
ages.put("Carol", 28)
var age = ages.get("Alice")

// Iteration
for (key, value in ages) {
    print("{key} is {value}")
}
```

## Nullable types

```zinc
fn find(String id): String? {
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

## Streams

Chainable collection operations with loop fusion (compiled to a single pass):

```zinc
List<int> numbers = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]

// Lambda syntax
var evens = numbers.filter(x -> x % 2 == 0)

// it keyword — implicit parameter
var big = numbers.filter(it > 5)
var doubled = numbers.map(it * 2)

// Chaining
var total = numbers.filter(it > 5).map(it * 10).sum()

// Terminal operations
numbers.anyMatch(it > 8)        // true
numbers.allMatch(it > 0)        // true
numbers.findFirst(it > 7)       // 8

// More operations
numbers.skip(3)                 // [4, 5, 6, 7, 8, 9, 10]
numbers.limit(3)                // [1, 2, 3]
numbers.distinct()              // unique elements
numbers.sortBy(it)              // sorted
numbers.reduce(0, (a, b) -> a + b)  // fold
numbers.forEach(x -> print(x))

// GroupBy
List<String> words = ["cat", "car", "dog"]
var grouped = words.groupBy(it.charAt(0))
// {c: [cat, car], d: [dog]}
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
    case Red { print("red!") }
    case Green { print("green!") }
    case _ { print("other") }
}
```

## Sealed classes

Algebraic data types with pattern matching:

```zinc
sealed class Shape {
    data Circle(double radius)
    data Rect(double width, double height)
}

var s = Circle(5.0)
print(s)    // Circle(radius=5)
```

## Function types & type aliases

```zinc
// Function types
type Transform = Fn<(int), int>

fn applyTwice(int x, Transform f): int {
    return f(f(x))
}

var result = applyTwice(3, x -> x + 10)  // 23

// Type aliases
type Handler = Fn<(String), String>
```

## Imports

Import Go standard library packages directly:

```zinc
import strings
import math
import strconv

var upper = strings.ToUpper("hello")
var pi = math.Pi
```

Zinc auto-detects required imports for `fmt`, `sync`, `strconv`, and other common packages.

## Next steps

- [Classes & Inheritance](classes.md)
- [Error Handling](error-handling.md)
- [Concurrency](concurrency.md)
