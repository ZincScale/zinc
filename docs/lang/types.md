# Zinc — Type System

## Type Checking

Types are enforced at transpile time. Errors block `.py` output:

```
var int x = "hello"                    // type mismatch: expected int, got str
fn add() int { return "bad" }          // return type mismatch
greet(42)                              // argument 1: expected str, got int
greet("a", "b")                        // expects 1 args, got 2
break                                  // 'break' outside of loop
y = 10                                 // undefined variable "y"
fn f() int { if x > 0 { return 1 } }  // not all code paths return
```

## Built-in Types

| Type | Description |
|---|---|
| `int` | Integer |
| `float` | Floating-point number |
| `str` | String |
| `bool` | Boolean (`true` / `false`) |
| `List<T>` | List of T |
| `Map<K, V>` | Map of K to V |
| `Set<T>` | Set of T |
| `tuple<T, U>` | Tuple |
| `any` | Any type (opt out of checking) |
| `null` | Null value |

## Generic Types

Use angle brackets `<>` for generic type parameters:

```zinc
var List<int> numbers = [1, 2, 3]
var Map<str, int> scores = {"Alice": 100, "Bob": 85}
var Set<str> tags = {"a", "b", "c"}
var List<List<int>> matrix = [[1, 2], [3, 4]]
```

## Type Annotations

Types go before the variable or parameter name:

```zinc
// Variables
var int count = 0
var str name = "Alice"
var List<str> items = []

// Function parameters and return types
fn process(str input, int limit) List<str> {
    return input.split(",").take(limit)
}
```

## Type Checking with `is`

`is` does both identity checks and type checks. The transpiler decides based on context:

### Type Check

When the right-hand side is a type name, `is` generates `isinstance()`:

```zinc
if x is str {
    print(x.upper())            // x narrowed to str in this block
}
if x is int {
    print(x + 1)                // x narrowed to int
}
if x is not list {
    print("not a list")
}
```

### Identity Check

When the right-hand side is a value, `is` generates Python's `is`:

```zinc
if value is null {
    print("no value")
}
if value != null {
    print("has value: {value}")
}
```

## Type Narrowing

After an `is` check, Zinc narrows the type within the block:

```zinc
fn process(any x) {
    if x is str {
        var str s = x            // OK -- x narrowed to str
        print(s.upper())
    }
    if x is int {
        print(x + 1)            // OK -- x narrowed to int
    }
}
```

## Nullable Types

Use `Type?` to indicate a value may be `null`:

```zinc
var str? name = null
var int? age = null

fn find(str id) User? {
    // may return null
}

var User? user = find("abc")
if user != null {
    print(user.name)             // narrowed to User
}
```

## Result Types

`Result<T>` is a generic type for operations that may fail. See [Error Handling](error-handling.md) for full details:

```zinc
fn parse(str input) Result<int> {
    if not input.isdigit() {
        return Err("not a number")
    }
    return int(input)
}
```
