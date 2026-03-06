# Growler Feature Roadmap

Prioritized by impact (high -> low), sub-prioritized by effort (quick -> large).

---

## Tier 1 — High Impact

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 1 | **Callable function types (`Fn<In, Out>`)** | Can't pass closures through `Any`; blocks higher-order patterns | Medium |
| 2 | **Functional collection methods** (`.map()`, `.filter()`, `.reduce()`, `.forEach()`) | Core OO/FP pattern in Java streams, Python, C#, Ruby; currently must use C-style loops | Medium |
| 3 | **Fix `for (k, v) in map` codegen** | Parser supports it but codegen ignores `IndexVar` — always emits `for _, item` | Quick fix |
| 4 | **List/string slicing** (`list[1:3]`, `s[2:]`) | Basic collection operation missing entirely; no `SliceExpr` AST node | Medium |
| 5 | **Map utility methods** (`.keys()`, `.values()`, `.clone()`, `.contains()`) | Common OO map operations; currently no way to get keys/values as lists | Quick-Medium |

---

## Tier 2 — Medium Impact

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 6 | **`const` declarations** | AST + lexer token exist but not wired up; needed for immutability | Quick |
| 7 | **`range(n)` / `range(a, b)` iteration** | Reduces boilerplate for numeric loops; Python/Kotlin idiom | Quick |
| 8 | **Operator overloading** | Natural for numeric classes, vectors, money types | Medium |
| 9 | **Enhanced destructuring** | `var (a, b, c) = ...` beyond 2-tuple; match on struct fields | Medium |
| 10 | **Interface default methods** | Reduces boilerplate for shared behaviour | Medium |
| 11 | **Variadic functions** (`...` params) | Common pattern, currently not supported | Quick-Medium |

---

## Tier 3 — Infrastructure / Polish

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 12 | **Source maps** | Map Go compiler errors back to `.gw` lines | Large |
| 13 | **Growler stdlib wrappers** | Real `io`, `http`, `json` API in Growler idioms | Large |
| 14 | **REPL completeness** | Listed in docs but may be partial | Medium |
| 15 | **Multi-file project completion** | Registry exists; needs cross-file type resolution | Medium-Large |
| 16 | **Better map/list literal type inference** | Maps always emit `map[interface{}]interface{}`; lists infer only from first element | Medium |
| 17 | **Example coverage** | Many features (string methods, type casting, named args, list methods) have no `.gw` example | Quick |

---

## Language Limitations (discovered via e2e tests)

### 1. Callable Function Types (`Any` parameters can't be invoked)

**The problem:** In Growler, `Any` maps to Go's `interface{}`. In Go, you cannot call a variable of type `interface{}` as a function — the compiler requires a concrete or known function type.

This Growler code looks reasonable:
```growler
fn apply(f: Any, x: Int): Int {
    return f(x)   // intended to call f as a function
}

fn main() {
    var double = (x: Int): Int => x * 2
    print(apply(double, 7))
}
```

But it transpiles to invalid Go:
```go
func apply(f interface{}, x int) int {
    return f(x)   // ERROR: cannot call non-function f (variable of type interface{})
}
```

**The fix options:**
- **Generic function types**: `fn apply<F>(f: F, x: Int): Int` -> `func apply[F any](f F, x int) int` — but Go generics don't support calling `f` without a constraint.
- **Dedicated `Fn` type**: `fn apply(f: Fn<Int, Int>, x: Int): Int` -> `func apply(f func(int) int, x int) int` — explicit function type syntax.
- **Type casting workaround** (interim): cast `f` to the expected function type using `as`, once `as` is implemented.

**Current workaround:** Don't pass closures through `Any`. Keep the closure in scope and call it directly, or define a proper interface.

---

### 2. Go Zero-Value Construction (`Type{}` not supported)

**The problem:** Growler has no syntax for constructing a zero-value Go struct. The parser sees `sync.Mutex{}` as a selector expression followed by an unrelated empty block, producing invalid Go.

**Why not struct literal syntax?** Adding `Type { field: val }` would pull Growler towards Go idioms and away from the OO feel of Java/Python/C#/Ruby. Growler's goal is to be familiar to OO developers migrating to Go — same idioms, same patterns.

**Chosen solution: `.new()` on Go types**

Growler already uses `ClassName.new()` for its own classes. The natural OO extension is to allow `.new()` on unrecognised (Go) types too — the constructor call pattern every OO developer knows:

```growler
// Ruby: Mutex.new  |  Java: new Mutex()  |  Python: Mutex()  |  C#: new Mutex()
var mu     = sync.Mutex.new()
var client = http.Client.new()
var buf    = bytes.Buffer.new()
```

Transpiles to idiomatic Go:
```go
mu     := sync.Mutex{}
client := http.Client{}
buf    := bytes.Buffer{}
```

**Implementation:** when codegen sees `X.new()` and `X` is not a known Growler class, emit `X{}` instead of `NewX()`. Single rule, no new syntax needed.

**Named fields** (`http.Client.new(timeout: 30)`) can follow later as a natural extension — same pattern OO devs know as constructor arguments.

**Current workaround:** Use Growler classes for stateful objects instead of raw Go structs.

---

## Completed
- Variables, functions, classes, interfaces, inheritance, generics
- Enums + match
- Error handling (try/catch/throw)
- Closures / lambdas (including throwing lambdas)
- Concurrency (goroutines, channels)
- Default parameters + named arguments
- `with` statement (resource management, parenthesized syntax)
- Type casting (`as` / `is`) with e2e tests
- `.new()` on Go types (zero-value construction)
- Labeled `break`/`continue` (`@label for/while`, `break @label`)
- Safe navigation `?.` (`obj?.field`, `obj?.method()`)
- `with` multi-return auto-detection (`with (var f = os.Create(path))`)
- More stdlib aliases (`readFile`, `writeFile`, `httpGet`, `jsonEncode`, `jsonDecode`, `sprintf`, `typeOf`, `sleep`, `getEnv`, `setEnv`, `now`)
- OO collection methods (`.add()`, `.remove()`, `.size()`, `.clone()`, `.sort()`, `.join()`)
- OO string methods (`.upper()`, `.lower()`, `.contains()`, `.startsWith()`, `.endsWith()`, `.trim()`, `.split()`, `.replace()`)
- Null safety (Kotlin-style strict enforcement)
- Tuple unpacking, string interpolation, imports, built-ins
