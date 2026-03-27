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

## Change 3: Chained Comparisons

### Current syntax
```zinc
if x > 1 && x < 10 {
    print("in range")
}

if a >= 0 && a <= 255 && b >= 0 && b <= 255 {
    print("both are bytes")
}
```

### New syntax
```zinc
if 1 < x < 10 {
    print("in range")
}

if 0 <= a <= 255 && 0 <= b <= 255 {
    print("both are bytes")
}
```

### Rules

- Any sequence of comparison operators can be chained: `<`, `<=`, `>`, `>=`, `==`, `!=`
- `a < b < c` desugars to `a < b && b < c` — each intermediate operand is evaluated once
- Chains of arbitrary length are allowed: `0 <= x < y < z <= 100`
- Mixed operators are allowed: `a < b == c <= d`
- `==` and `!=` can appear in chains but should not be mixed with each other: `a == b != c` is confusing and should be written as two separate conditions
- Chaining does **not** apply to `===`, `!==`, `is`, `is not` — these remain binary only

### Transpilation

| Zinc | Java |
|---|---|
| `1 < x < 10` | `1 < x && x < 10` |
| `0 <= a <= 255` | `0 <= a && a <= 255` |
| `a < b < c < d` | `a < b && b < c && c < d` |

When the middle operand is a method call or expression with side effects, the transpiler extracts it to a temporary variable to avoid double evaluation:

```zinc
1 < expensive() < 10
```
Transpiles to:
```java
var _tmp = expensive();
1 < _tmp && _tmp < 10;
```

### Parser changes

- In expression parsing, after parsing a comparison `a < b`, check if the next token is also a comparison operator. If so, continue the chain.
- Build an AST node `ChainedComparison(List<Expr> operands, List<CompOp> operators)` alongside the existing `BinaryExpr`.

### Codegen changes

- Emit `&&`-joined pairwise comparisons in Java output.
- Extract non-trivial middle operands to temp variables.
- Python output: emit directly (Python supports chained comparisons natively).

## Change 4: Rest Unpacking

### Current syntax
```zinc
var (x, y) = getPoint()
var (name, age) = getUser()
```

### New syntax — adds `...rest` patterns
```zinc
var (first, ...rest) = items          // first = items[0], rest = items[1..]
var (first, second, ...rest) = items  // first = items[0], second = items[1], rest = items[2..]
var (first, ...middle, last) = items  // first, last split, middle gets everything between
```

### Rules

- `...name` captures remaining elements as a `List<T>`
- Only one `...` allowed per unpacking pattern
- `...rest` can appear at any position: beginning, middle, or end
- Works in:
  - Variable declarations: `var (first, ...rest) = items`
  - For-loop destructuring: `for (key, ...values) in grouped { }`
  - Match case patterns: `case [first, ...rest] -> ...`
- The rest variable is always a `List<T>` (or array, matching the source type)

### Examples

```zinc
List<int> nums = [1, 2, 3, 4, 5]

// Head and tail
var (head, ...tail) = nums       // head = 1, tail = [2, 3, 4, 5]

// First, last, and middle
var (first, ...middle, last) = nums  // first = 1, middle = [2, 3, 4], last = 5

// In match expressions
match args {
    case [cmd, ...flags] -> run(cmd, flags)
    case [] -> showHelp()
}

// Practical: processing CSV header + rows
var (header, ...rows) = lines
for row in rows {
    process(header, row)
}
```

### Transpilation

| Zinc | Java |
|---|---|
| `var (first, ...rest) = items` | `var first = items.get(0); var rest = items.subList(1, items.size());` |
| `var (first, ...mid, last) = items` | `var first = items.get(0); var last = items.get(items.size()-1); var mid = items.subList(1, items.size()-1);` |
| `var (...init, last) = items` | `var last = items.get(items.size()-1); var init = items.subList(0, items.size()-1);` |

Python output: emit directly — Python supports `first, *rest = items` natively.

### Parser changes

- In tuple/destructuring patterns, recognize `...identifier` as a rest pattern.
- Validate at most one `...` per pattern.
- New AST node: `RestPattern(String name)` within `TupleUnpack`.

### Codegen changes

- Emit `List.subList()` calls for rest extraction in Java.
- Emit `*name` in Python.

## Change 5: Assert Statements

### Syntax
```zinc
assert x > 0
assert x > 0, "x must be positive, got {x}"
```

### Rules

- `assert condition` — if condition is false, throws `AssertionError`
- `assert condition, message` — includes a message (supports string interpolation)
- Asserts are **always enabled** — no silent removal in production builds. If you assert something, you mean it.
- Asserts are for **invariants and programmer errors**, not for input validation. Use `return Error(...)` for expected failure cases.

### When to use assert vs Error

| Situation | Use | Why |
|---|---|---|
| User provided bad input | `return Error("...")` | Expected — caller handles it |
| External API returned garbage | `return Error("...")` | Expected — caller handles it |
| Internal logic invariant violated | `assert x > 0` | Bug — should never happen |
| Precondition on internal function | `assert list.size() > 0` | Bug — caller is broken |
| Post-condition sanity check | `assert result >= 0` | Bug — this function is broken |

### Examples

```zinc
fn binarySearch(List<int> sorted, int target): int {
    assert sorted.size() > 0, "cannot search empty list"

    int lo = 0
    int hi = sorted.size() - 1

    while lo <= hi {
        int mid = (lo + hi) / 2
        assert 0 <= mid < sorted.size(), "mid out of bounds: {mid}"

        if sorted.get(mid) == target { return mid }
        if sorted.get(mid) < target { lo = mid + 1 }
        else { hi = mid - 1 }
    }
    return -1
}
```

### Transpilation

| Zinc | Java |
|---|---|
| `assert x > 0` | `assert x > 0;` (Java assert — enabled via `-ea`) |
| `assert x > 0, "msg {x}"` | `assert x > 0 : "msg " + x;` |

Note: Java asserts are disabled by default and enabled with `-ea`. Since Zinc controls the runner (`zinc run`), we always pass `-ea`. For jpackaged/native builds, the transpiler can optionally emit `if (!cond) throw new AssertionError(msg)` instead to guarantee asserts are never stripped.

Python output: `assert x > 0, f"msg {x}"` — Python asserts are stripped with `-O` but we don't use `-O`.

### Parser changes

- New statement type: `AssertStmt(Expr condition, Expr? message)`
- Keyword `assert` added to lexer

### Codegen changes

- Emit Java `assert` or `if + throw AssertionError` depending on build mode.
- Emit Python `assert` directly.
