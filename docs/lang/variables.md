# Zinc — Variables and Constants

## Variables

Use `var` for type-inferred variables. When you specify an explicit type, drop the `var` keyword. Zinc supports type inference, explicit types, and tuple unpacking.

```zinc
var x = 42                      // inferred as int
int x = 42                      // explicit type
String name = "Alice"              // explicit type
List<int> items = []            // generic type
var a, b = swap(1, 2)          // tuple unpacking
```

### Type Inference

When you omit the type, Zinc infers it from the right-hand side:

```zinc
var count = 0                   // int
var name = "Bob"                // String
var ratio = 3.14                // double
var active = true               // boolean
var tags = ["a", "b", "c"]     // List<String>
```

### Explicit Types

Place the type before the variable name (no `var` keyword needed):

```zinc
int count = 0
String name = "Bob"
double ratio = 3.14
boolean active = true
List<String> tags = []
Map<String, int> scores = {}
int[] scores = [85, 92, 78]
String[] args = []
```

### Tuple Unpacking

Functions that return multiple values can be unpacked directly:

```zinc
fn swap(int a, int b): (int, int) {
    return b, a
}
var x, y = swap(1, 2)          // x=2, y=1
```

Transpiles to a generated record for the return type, with field extraction at the call site.

### Reassignment

Variables declared with `var` can be reassigned:

```zinc
var x = 10
x = 20                          // OK
```

### Initialization Required

Every variable must be initialized at declaration. There are no uninitialized variables in Zinc:

```zinc
var x = 0                       // OK — explicit default
List<int> items = []            // OK — empty list
```

## Constants (`const`)

Use `const` for immutable bindings. Constants must be initialized and cannot be reassigned:

```zinc
const PI = 3.14159
const int MAX_RETRIES = 3          // const keeps the keyword
const String APP_NAME = "zinc-app"
```

### Constant Parameters

Parameters can be marked `const` to prevent reassignment inside the function body:

```zinc
fn greet(const String name): String {
    // name = "other"           // compile error: cannot reassign const parameter
    return "Hello, {name}!"
}
```

### Class-Level Constants

```zinc
class Config {
    const String NAME = "default"
    const int VERSION = 1
}
```

### `const` on Collections

`const` on a collection is reference-only — the variable cannot be reassigned, but the contents are still mutable:

```zinc
const List<int> items = [1, 2, 3]
items.add(4)                    // OK — contents are mutable
// items = [5, 6]              // compile error: cannot reassign const
```

## Init Fields (`init`)

Use `init` for fields that must be set in the constructor and are frozen after construction:

```zinc
class User {
    init String name               // must be set in constructor
    init String email              // frozen after construction

    fn init(String name, String email) {
        this.name = name
        this.email = email
    }
}

var u = new User("Alice", "alice@example.com")
// u.name = "Bob"              // compile error: init field is frozen
```

`init` fields are useful for identity fields that should never change after the object is created.
