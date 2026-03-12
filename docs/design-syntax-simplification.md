# Syntax Simplification — Design Document

## Goal

Reduce ceremony and boilerplate in Zinc. When intent is clear from context, don't require keywords or punctuation that add nothing. Make Zinc feel like writing pseudocode that compiles.

## Decisions

### 1. Drop `class` keyword

Top-level `Name { }` (capitalized identifier + braces) is unambiguous — no other construct uses this pattern. Functions have parens, enums keep the `enum` keyword, interfaces keep the `interface` keyword.

```zinc
// Before
class Dog { }

// After
Dog { }
```

### 2. Drop `fn` keyword

Methods inside classes and top-level functions don't need `fn`. The pattern `name(params) { }` is unambiguous in both contexts. `main()` is distinguishable from a class (lowercase, has parens).

```zinc
// Before
fn main() { }
fn add(a: Int, b: Int): Int { return a + b }
class Dog {
    pub fn bark(): String { return "Woof!" }
}

// After
main() { }
add(a Int, b Int) Int { return a + b }
Dog {
    pub bark() String { return "Woof!" }
}
```

### 3. Drop `var` for class fields

Inside a class body, `name Type` is unambiguously a field declaration. `var` adds nothing.

```zinc
// Before
class Dog {
    var name: String
    var age: Int = 0
}

// After
Dog {
    name String
    age Int = 0
}
```

### 4. `name Type` order, no colon

Switch from `name: Type` (Kotlin/Swift) to `name Type` (Go style). No colon needed — the parser distinguishes names from types. Applies to fields, parameters, and local variable declarations.

```zinc
// Before
var name: String = "Rex"
var age: Int = 0
fn greet(name: String, greeting: String = "Hello") { }

// After
String name = "Rex"       // explicit type (but see decision 4b)
Int age = 0
greet(name String, greeting String = "Hello") { }
```

**Local variable shorthand — `:=` for inferred declarations:**

```zinc
x := 42              // inferred Int
name := "hello"      // inferred String
items := [1, 2, 3]   // inferred List<Int>

Int count = 0         // explicit type when needed
String? label = null  // nullable with explicit type
```

**`var` is kept for mutable local variables when an explicit type is needed:**

```zinc
x := 42               // inferred, mutable
let y = 42             // inferred, immutable
Int count = 0          // explicit type, mutable
let Int max = 100      // explicit type, immutable
```

### 5. Keep `new` for constructors

Constructor stays as `new(params) { }` inside the class body. Avoids coupling constructor label to class name (renaming class would require renaming constructor).

```zinc
Dog {
    name String
    age Int = 0

    new(name String, age Int = 0) {
        this.name = name
        this.age = age
    }
}
```

### 6. Drop `.new()` at call site

Construction uses `ClassName(args)` directly — like Python, Kotlin, Swift, Dart.

```zinc
// Before
var d = Dog.new(name: "Rex", age: 5)

// After
d := Dog(name: "Rex", age: 5)
```

### 7. Return type after parens, no colon

Return type follows the parameter list, no colon separator. Consistent with `name Type` order — the function "name" is the whole `methodName(params)` part, and the type follows.

```zinc
// Before
fn add(a: Int, b: Int): Int { return a + b }
pub fn bark(): String { return "Woof!" }

// After
add(a Int, b Int) Int { return a + b }
pub bark() String { return "Woof!" }
```

### 8. No return type = void

If a function/method doesn't return anything, just omit the return type. No `Void` keyword needed.

```zinc
main() { print("hello") }
pub process() { doWork() }
```

### 9. No implicit return

Every function that returns a value uses explicit `return`. No special case for single-expression bodies. Consistent regardless of function size.

```zinc
// Always explicit
Int double(Int x) { return x * 2 }

// Not this
Int double(Int x) { x * 2 }   // NO — always use return
```

### 10. Drop parentheses on `if`/`while`/`for`

Go/Rust/Swift style — the `{` delimits the condition from the body. Parens are pure ceremony.

```zinc
// Before
if (x > 5) { print("big") }
while (running) { process() }
for (i, item) in list { print(item) }

// After
if x > 5 { print("big") }
while running { process() }
for i, item in list { print(item) }
```

### 11. Colon usage is now orthogonal

Colon is no longer overloaded. Two uses, one meaning each:

| Syntax | Meaning | Example |
|--------|---------|---------|
| `:` | Key-value separator | `Dog(name: "Rex")`, `{"key": val}` |
| `:=` | Inferred declaration | `x := 42` |

Previously colon meant three things (type annotation, return type, key-value). Now it means one thing: "maps to a value."

## Full Before & After

### Before (current Zinc)

```zinc
class Dog {
    var name: String
    var age: Int = 0

    new(name: String, age: Int = 0) {
        this.name = name
        this.age = age
    }

    pub fn bark(): String {
        return "Woof, I'm {name}!"
    }

    pub fn isOld(): Bool {
        return age > 10
    }

    fn secret() {
        print("shh")
    }
}

class Puppy : Dog {
    var toy: String

    new(name: String, toy: String) {
        super(name: name, age: 0)
        this.toy = toy
    }

    pub fn play(): String {
        return "{name} plays with {toy}"
    }
}

enum Color {
    Red, Green, Blue
}

interface Drawable {
    pub fn draw(): String
}

fn classify(x: Int): String {
    if (x > 100) {
        return "big"
    } else if (x > 10) {
        return "medium"
    } else {
        return "small"
    }
}

fn main() {
    var d = Dog.new(name: "Rex", age: 5)
    var p = Puppy.new(name: "Spot", toy: "ball")
    let scores = {"Alice": 90, "Bob": 60}

    if (d.isOld()) {
        print("Old dog")
    } else {
        print(d.bark())
    }

    for (name, score) in scores {
        print("{name}: {score}")
    }

    var items: List<Int> = [1, 2, 3, 4, 5]
    let big = items.Where(x => x > 3).Select(x => x * 2).ToList()
    print(big.join(", "))

    var result = riskyCall() or {
        print("failed: {err}")
        return
    }
}
```

### After (simplified Zinc)

```zinc
Dog {
    name String
    age Int = 0

    new(name String, age Int = 0) {
        this.name = name
        this.age = age
    }

    pub bark() String {
        return "Woof, I'm {name}!"
    }

    pub isOld() Bool {
        return age > 10
    }

    secret() {
        print("shh")
    }
}

Puppy : Dog {
    toy String

    new(name String, toy String) {
        super(name: name, age: 0)
        this.toy = toy
    }

    pub play() String {
        return "{name} plays with {toy}"
    }
}

enum Color {
    Red, Green, Blue
}

interface Drawable {
    pub draw() String
}

classify(Int x) String {
    if x > 100 {
        return "big"
    } else if x > 10 {
        return "medium"
    } else {
        return "small"
    }
}

main() {
    d := Dog(name: "Rex", age: 5)
    p := Puppy(name: "Spot", toy: "ball")
    let scores = {"Alice": 90, "Bob": 60}

    if d.isOld() {
        print("Old dog")
    } else {
        print(d.bark())
    }

    for name, score in scores {
        print("{name}: {score}")
    }

    List<Int> items = [1, 2, 3, 4, 5]
    let big = items.Where(x => x > 3).Select(x => x * 2).ToList()
    print(big.join(", "))

    result := riskyCall() or {
        print("failed: {err}")
        return
    }
}
```

## What Stays the Same

- `enum` keyword (body is too different from classes)
- `interface` keyword (signals intent)
- `let` for immutable bindings
- `return` always explicit
- `pub` for visibility
- `static` for static methods
- String interpolation `{expr}`
- `or { }` error handling
- Lambda syntax `x => expr`, `(x, y) => expr`
- Named arguments with colon `fn(name: value)`
- Collection methods, generics, all existing features

## Migration

This is a breaking change to all existing `.zn` files. Implementation should:

1. Update lexer/parser to accept new syntax
2. (Optional) Support old syntax with deprecation warnings during transition
3. Update all examples, tests, and documentation
4. Provide a `zinc migrate` command to auto-convert old syntax

## Implementation Order

1. **Drop `if`/`while`/`for` parens** — parser change only, lowest risk
2. **Drop `fn` keyword** — parser change, update function/method parsing
3. **Drop `class` keyword** — parser change for top-level declarations
4. **`name Type` (no colon)** — parser change for params, fields, variables
5. **Return type after parens (no colon)** — parser change for function signatures
6. **Drop `.new()` at call site** — parser + codegen change
7. **Drop `var` for fields** — parser change for class body
8. **Update all examples, tests, docs**
