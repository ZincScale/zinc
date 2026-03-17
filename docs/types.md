# Types and Variables

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

> **Visibility rule:** `const` → private. `pub const` → public.

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

## Enums

```zinc
enum Direction { North, South, East, West }
enum Status { Pending, Active, Closed }
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
}
```

## String Interpolation

```zinc
var name = "Zinc"
var version = 1
print("Welcome to {name} v{version}!")
```

Curly braces inside regular strings are interpolated. Use backtick strings for raw (uninterpolated) text:

```zinc
var raw = `no {interpolation} here`
```
