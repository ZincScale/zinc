# Zinc — Functions

## Basic Functions

Functions are declared with `fn`. Parameters use type-first syntax: `type name`. Return type follows the parameter list.

```zinc
fn greet(String name) String {
    return "Hello, {name}!"
}

fn add(int a, int b) int {
    return a + b
}

fn sayHello() {
    print("Hello!")
}
```

Functions with no return type return `void` implicitly.

## Single-Expression Functions

For short functions, use `=` to define the body as a single expression:

```zinc
fn double(int x) int = x * 2
fn square(int n) int = n * n
fn fullName(String first, String last) String = "{first} {last}"
```

## Default Arguments

Parameters can have default values:

```zinc
fn connect(String host, int port = 80, boolean ssl = false) {
    print("Connecting to {host}:{port}")
}

connect("db.example.com")                        // port=80, ssl=false
connect("db.example.com", 3306)                   // ssl=false
connect("db.example.com", 443, true)              // all explicit
```

## Named Arguments

Use named arguments at the call site for clarity:

```zinc
connect("db.example.com", port=3306, ssl=true)
```

Named arguments work with any function — they are a call-site feature, not a declaration feature. The transpiler reorders them to positional order.

## Variadic Arguments

Use `...` suffix for variadic parameters (Java varargs):

```zinc
fn log(String... messages) {
    for msg in messages {
        print(msg)
    }
}

log("info", "server started", "port 8080")
```

Transpiles to:
```java
static void log(String... messages) {
    for (var msg : messages) {
        System.out.println(msg);
    }
}
```

## Entry Point: `fn main()`

For projects, use `fn main()` as the explicit entry point:

```zinc
fn main() {
    print("Hello from Zinc!")
}
```

Zinc generates `main(String[] args)` automatically. The `args` variable is available inside `fn main()`. For scripts (top-level statements), no `fn main()` is needed — Zinc wraps them automatically.

```zinc
// With explicit args
fn main(String[] args) {
    print("arg count: {args.length}")
}
```

## Lambdas

Lambdas use the `->` arrow syntax:

```zinc
var doubler = x -> x * 2
var adder = (int a, int b) -> a + b

// Used inline with collection methods
items.filter(x -> x > 0)
items.map(x -> x * 2)
items.sortBy(x -> x.age)
```

### The `it` Keyword

Single-parameter lambdas can use `it` instead of naming the parameter:

```zinc
items.filter(it > 0)
items.map(it * 2)
users.sortBy(it.age)
```

See [Collections — The `it` Keyword](collections.md#the-it-keyword) for more examples.

### Multi-Parameter Lambdas

Multi-parameter lambdas require parentheses:

```zinc
var add = (a, b) -> a + b
pairs.map((k, v) -> "{k}={v}")
```

### Block Lambdas

For multi-statement lambdas, use braces:

```zinc
items.forEach(x -> {
    var processed = transform(x)
    save(processed)
})
```

## Return Types

The return type comes after the closing parenthesis of the parameter list:

```zinc
fn parse(String input) int {
    return int(input)
}

fn divide(double a, double b) Result<double> {
    if b == 0 {
        return Error("division by zero")
    }
    return a / b
}
```

## Tuple Return Types

Functions can return multiple values as a tuple:

```zinc
fn minMax(List<int> items) (int, int) {
    return (items.min(), items.max())
}

var (lo, hi) = minMax(numbers)
```

See [Collections — Tuples](collections.md#tuples) for destructuring details.
