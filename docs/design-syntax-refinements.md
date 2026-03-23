# Design Doc: Syntax Refinements — `var` Elision & Return Type Colon

**Status:** Proposed
**Date:** 2026-03-23

## Motivation

As Zinc codebases grow, two sources of visual noise emerge:

1. **Redundant `var` keyword** — When an explicit type is present, `var` adds no information:
   ```
   var int x = 42              // var + type is redundant
   var List<String> names = [] // same
   ```

2. **Return types blend into parameter lists** — Especially with annotations above a function, the return type gets lost:
   ```
   @Route("/users")
   @GET
   fn getUsers(String query) List<User> {
   ```

## Change 1: Drop `var` When Type Is Present

### Rules

- **Type present → no `var`**: The type itself signals a declaration.
  ```
  int x = 42
  List<String> names = ["Alice", "Bob"]
  Map<String, int> ages = {"Alice": 30}
  ```

- **Type absent → `var` required**: Inference still needs a declaration keyword to distinguish from reassignment.
  ```
  var x = 42
  var names = ["Alice", "Bob"]
  var u = User("Alice", "alice@example.com")
  ```

- **`init` unchanged**: Still means final (set once, read-only after).
  ```
  init String name = "default"   // explicit type, no var
  init name = "default"          // inferred type, no var (init already signals declaration)
  ```

### Class fields

`var` is dropped from field visibility modifiers. `pub` and `readonly` stand alone:

```
class Animal {
    pub String name = ""           // getter + setter
    readonly String sound = ""         // getter only
    String internal = ""           // private (default)
    var secret = ""                // private, type inferred (var needed for inference)
}
```

This is consistent: `var` is only ever needed when the type is absent.

### Reassignment

Reassignment uses bare `=` with no keyword, same as today:
```
int x = 42      // declaration
x = 100         // reassignment (no keyword)
```

No ambiguity because declarations require either a type or `var`.

### Summary

| Context | Explicit Type | Inferred Type |
|---|---|---|
| Local variable | `int x = 42` | `var x = 42` |
| Class field (private) | `String name = ""` | `var name = ""` |
| Class field (readable) | `readonly String name = ""` | `readonly name = ""` |
| Class field (public) | `pub String name = ""` | `pub name = ""` |
| Final (init) | `init String name = "x"` | `init name = "x"` |

## Change 2: Colon for Return Types

### Current syntax
```
fn greet(String name) String {
fn divide(int a, int b) int {
fn main() {
```

### New syntax
```
fn greet(String name): String {
fn divide(int a, int b): int {
fn main() {
```

### Rules

- `:` separates the parameter list from the return type.
- Void functions omit the colon and return type entirely (no change).
- Applies to all function declarations: top-level, class methods, override methods.
- Does **not** apply to lambdas — they continue to use `->`:
  ```
  var evens = numbers.filter(x -> x % 2 == 0)
  ```

### Why `:` over `->`

| Consideration | `:` | `->` |
|---|---|---|
| Conflict with lambdas | None | Overloads lambda syntax |
| Precedent | Kotlin, TypeScript, PHP 7+ | Rust, Swift, Python |
| Why those langs chose `->` | N/A | `:` was taken for `name: Type` param style |
| Zinc param style | `Type name` (Java-style) | — |
| Keystrokes | 1, no shift | 2, shift required |

Since Zinc uses `Type name` for parameters (not `name: Type`), the colon is unambiguous and available.

### Readability with annotations

Before:
```
@Route("/users")
@GET
fn getUsers(String query) List<User> {
```

After:
```
@Route("/users")
@GET
fn getUsers(String query): List<User> {
```

The `:` creates a clear visual anchor for "here comes the return type."

## Migration

Both changes are backwards-incompatible syntax changes. Since Zinc is pre-1.0, this is acceptable.

### Parser changes
- Accept `Type name = expr` as a variable declaration (no `var` keyword) in local and class field contexts.
- Keep `var name = expr` for type-inferred declarations.
- Require `:` before return type in function declarations.
- Reject old `fn name() Type {` syntax.

### Codegen changes
- None — these are purely syntactic; the AST and Java output remain identical.

### Test + example updates
- Update all 112 codegen tests.
- Update all examples in `examples/v3/`.
- Update all documentation in `docs/lang/`.

## Examples After Both Changes

```zinc
// Hello World
fn greet(String name): String {
    return "Hello, {name}!"
}

fn main() {
    print(greet("World"))
    int x = 42
    print("The answer is {x}")
}

// Collections
List<String> names = ["Alice", "Bob", "Charlie"]
var evens = numbers.filter(x -> x % 2 == 0)

// Classes
class Dog : Animal {
    pub String breed = ""

    override fn toString(): String {
        return "Dog({getName()}, {getBreed()})"
    }
}

// Error handling
fn divide(int a, int b): int {
    if b == 0 { return Error("division by zero") }
    return a / b
}

var result = divide(10, 0) or -1
```
