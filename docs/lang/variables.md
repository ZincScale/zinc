# Zinc — Variables and Constants

## Variables

Use `var` to declare variables. Zinc supports type inference, explicit types, and tuple unpacking.

```zinc
var x = 42                      // inferred as int
var int x = 42                  // explicit type
var str name = "Alice"          // explicit type
var List<int> items = []        // generic type
var a, b = divmod(10, 3)        // tuple unpacking
```

### Type Inference

When you omit the type, Zinc infers it from the right-hand side:

```zinc
var count = 0                   // int
var name = "Bob"                // str
var ratio = 3.14                // float
var active = true               // bool
var tags = ["a", "b", "c"]     // List<str>
```

### Explicit Types

Place the type before the variable name:

```zinc
var int count = 0
var str name = "Bob"
var float ratio = 3.14
var bool active = true
var List<str> tags = []
var Map<str, int> scores = {}
```

### Tuple Unpacking

Functions that return multiple values can be unpacked directly:

```zinc
fn swap(int a, int b) {
    return b, a
}
var x, y = swap(1, 2)          // x=2, y=1
```

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
var List<int> items = []        // OK — empty list
```

## Constants (`const`)

Use `const` for immutable bindings. Constants must be initialized and cannot be reassigned:

```zinc
const PI = 3.14159
const int MAX_RETRIES = 3
const str APP_NAME = "zinc-app"
```

### Constant Parameters

Parameters can be marked `const` to prevent reassignment inside the function body:

```zinc
fn greet(const str name) str {
    // name = "other"           // compile error: cannot reassign const parameter
    return "Hello, {name}!"
}
```

### Class-Level Constants

```zinc
class Config {
    const str NAME = "default"
    const int VERSION = 1
}
```

### `const` on Collections

`const` on a collection is reference-only -- the variable cannot be reassigned, but the contents are still mutable:

```zinc
const List<int> items = [1, 2, 3]
items.append(4)                 // OK — contents are mutable
// items = [5, 6]              // compile error: cannot reassign const
```

## Init Fields (`init`)

Use `init` for fields that must be set in the constructor and are frozen after construction:

```zinc
class User {
    init str name               // must be set in constructor
    init str email              // frozen after construction

    fn init(str name, str email) {
        self.name = name
        self.email = email
    }
}

var u = User("Alice", "alice@example.com")
// u.name = "Bob"              // compile error: init field is frozen
```

`init` fields are useful for identity fields that should never change after the object is created.
