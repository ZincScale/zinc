# Zinc — Type System

## Type Checking

Types are enforced at transpile time. Errors block compilation:

```
int x = "hello"                        // type mismatch: expected int, got String
fn add(): int { return "bad" }         // return type mismatch
greet(42)                              // argument 1: expected String, got int
greet("a", "b")                        // expects 1 args, got 2
break                                  // 'break' outside of loop
y = 10                                 // undefined variable "y"
fn f(): int { if x > 0 { return 1 } }  // not all code paths return
```

## Built-in Types

| Type | Description |
|---|---|
| `int` | Integer |
| `double` | Floating-point number |
| `String` | String |
| `boolean` | Boolean (`true` / `false`) |
| `List<T>` | List of T |
| `Map<K, V>` | Map of K to V |
| `Set<T>` | Set of T |
| `Type[]` | Array of Type (e.g., `int[]`, `String[]`) |
| `byte[]` | Byte array (for I/O, serialization) |
| `tuple<T, U>` | Tuple |
| `any` | Any type (opt out of checking) |
| `null` | Null value |

## Generic Types

Use angle brackets `<>` for generic type parameters:

```zinc
List<int> numbers = [1, 2, 3]
Map<String, int> scores = {"Alice": 100, "Bob": 85}
Set<String> tags = {"a", "b", "c"}
List<List<int>> matrix = [[1, 2], [3, 4]]
```

## Arrays

Arrays use `Type[]` syntax:

```zinc
int[] nums = [1, 2, 3]
String[] names = ["Alice", "Bob"]
byte[] data = [0, 1, 2]
```

Array access and length:

```zinc
print(nums[0])         // index access
print(nums.length)     // array length (not .size())
```

### Context-Dependent Inference

The `[1, 2, 3]` literal creates different types based on context:

```zinc
int[] nums = [1, 2, 3]          // array: new int[] {1, 2, 3}
List<int> nums = [1, 2, 3]      // list: new ArrayList<>(List.of(1, 2, 3))
var nums = [1, 2, 3]            // default: ArrayList (backwards compatible)
```

### Arrays in Functions

```zinc
fn sum(int[] numbers): int {
    int total = 0
    for n in numbers { total = total + n }
    return total
}
```

## Type Annotations

Types go before the variable or parameter name:

```zinc
// Variables
int count = 0
String name = "Alice"
List<String> items = []

// Function parameters and return types
fn process(String input, int limit): List<String> {
    return input.split(",").take(limit)
}
```

## Type Checking with `is`

`is` is a type check operator. It checks whether a value is an instance of a type:

```zinc
if x is String {
    print(x.toUpperCase())      // x narrowed to String in this block
}
if x is int {
    print(x + 1)                // x narrowed to int
}
if x is not List {
    print("not a list")
}
```

Transpiles to Java `instanceof`:
```java
if (x instanceof String) { ... }
if (x instanceof Integer) { ... }
if (!(x instanceof List)) { ... }
```

For null checks, use `==` and `!=`:

```zinc
if value == null {
    print("no value")
}
if value != null {
    print("has value: {value}")
}
```

## Equality

Zinc follows Kotlin conventions:

| Zinc | Java | Meaning |
|---|---|---|
| `a == b` | `Objects.equals(a, b)` | Structural equality (same values) |
| `a != b` | `!Objects.equals(a, b)` | Structural inequality |
| `a === b` | `a == b` | Reference identity (same object) |
| `a !== b` | `a != b` | Reference non-identity |

```zinc
var a = User("Alice", 30)
var b = User("Alice", 30)

a == b       // true — same values (data class generates equals())
a === b      // false — different objects
a != b       // false
a !== b      // true
```

For classes without an `equals()` override, `==` falls through to reference comparison (default `Object.equals()` behavior). `data` classes (records) auto-generate `equals()` based on field values.

## Type Narrowing

After an `is` check, Zinc narrows the type within the block:

```zinc
fn process(any x) {
    if x is String {
        String s = x                // OK -- x narrowed to String
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
String? name = null
int? age = null

fn find(String id): User? {
    // may return null
}

User? user = find("abc")
if user != null {
    print(user.name)             // narrowed to User
}
```

## Result Types

`Result<T>` is a generic type for operations that may fail. See [Error Handling](error-handling.md) for full details:

```zinc
fn parse(String input): Result<int> {
    if not input.isdigit() {
        return Error("not a number")
    }
    return int(input)
}
```
