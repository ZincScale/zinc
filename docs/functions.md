# Functions

## Basic Functions

```zinc
Int add(Int a, Int b) {
    return a + b
}

pub String greet(String name) {
    return "Hello, {name}!"
}
```

## Implicit Return

The last expression in a function or method body is automatically returned — no `return` keyword needed:

```zinc
Int double(Int x) { x * 2 }
String greet(String name) { "Hello, {name}!" }

Calculator {
    pub Int square(Int x) { x * x }
}
```

Explicit `return` still works for early returns or clarity.

## Default Parameter Values

```zinc
greet(String name, String greeting = "Hello") {
    print("{greeting}, {name}!")
}

main() {
    greet("Alice")              // greeting defaults to "Hello"
    greet("Bob", "Hi")          // explicit override
}
```

## Named Arguments

Arguments may be passed by name using `name: value` syntax. Named arguments may appear in any order and can be mixed with leading positional arguments:

```zinc
connect(String host, Int port = 8080, Bool tls = false) { }

main() {
    connect("localhost")                           // both defaults used
    connect("example.com", port: 443, tls: true)   // named, positional host
    connect(tls: true, host: "example.com")        // fully named, reordered
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
    var d1 = Dog("Rex")
    var d2 = Dog(name: "Max", age: 5)
}
```

## Generic Functions

```zinc
T identity<T>(T val) {
    return val
}

K pair<K, V>(K key, V value) {
    return key
}
```

## Variadic Functions

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

## Closures / Lambdas

Lambdas use the `(Type param) -> body` syntax. The body is either a single expression or a block `{ ... }`.

```zinc
// Single-expression lambda
var double = (Int x) -> x * 2
var greet  = () -> "Hello!"

// Block-body lambda
var describe = (Int x) -> {
    if x > 0 { return "positive" }
    return "non-positive"
}

// Closure capture
var base = 100
var addBase = (Int x) -> x + base
```

### Failable Lambdas

A lambda that contains `return Error(...)` automatically becomes failable:

```zinc
var safeDivide = (Int a, Int b) -> {
    if b == 0 { return Error("division by zero") }
    return a / b
}

var result = safeDivide(10, 2)
var bad = safeDivide(10, 0) or {
    print("Error: {err}")
    exit(1)
}
```

## Callable Function Types (`Fn`)

Use `ReturnType Fn(ParamTypes)` to declare typed function parameters:

```zinc
Int apply(Int Fn(Int) f, Int x) {
    return f(x)
}

run(Fn() callback) {
    callback()
}

main() {
    var double = (Int x) -> x * 2
    print(apply(double, 7))       // 14
    run(() -> { print("done") })
}
```
