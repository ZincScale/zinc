# Growler Feature Roadmap

Prioritized by impact (high → low), sub-prioritized by effort (quick → large).

---

## Tier 1 — High Impact

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| ~~1~~ | ~~**Type casting (`as`)**~~ | ~~Can't safely work with `Any`, interfaces, or mixed types without it~~ | ~~Done~~ |
| 2 | **More std lib built-in aliases** | Expand the built-ins table — `readFile`, `writeFile`, `httpGet`, JSON encode/decode, etc. | Easy but incremental |
| 3 | **Callable function types** | `Any` parameters can't be invoked in Go — needs a proper function type or generics | Medium (see Language Limitations below) |
| 4 | **Nullable safe-navigation (`?.`)** | `obj?.field`, `obj?.method()` — essential ergonomics with optional types | Medium (parser + nil-guard codegen) |
| ~~5~~ | ~~**`.new()` on Go types**~~ | ~~No way to construct raw Go types like `sync.Mutex{}` or `http.Client{}`~~ | ~~Done~~ |
| 6 | **Growler stdlib wrappers** | A real `io`, `http`, `json` API in Growler idioms so users never hand-write Go imports | Large (design + impl) |
| 7 | **Source maps** | When Go compiler errors reference a `.go` line, map it back to the `.gw` source | Large (thread line/col through entire pipeline) |

---

## Tier 2 — Medium Impact

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 8 | **`break`/`continue` with labels** | Needed for nested loop control; Go supports it natively | Quick (lexer label + codegen passthrough) |
| 9 | **Operator overloading** | Natural for numeric classes, vectors, money types | Medium (parser `operator` keyword + method dispatch) |
| 10 | **Enhanced destructuring** | `var (a, b, c) = ...` beyond 2-tuple; `match` on struct fields | Medium |
| 11 | **Interface default methods** | Reduces boilerplate for shared behaviour | Medium |
| 12 | **`with` multi-return support** | `with var f = os.Create(path)` should auto-unpack `(val, err)`, discarding or throwing on error. Currently requires `var (f, _) = os.Create(path)` then `with var file = f` | Medium (parser + codegen) |

---

## Tier 3 — Lower Impact / Infrastructure

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 13 | **REPL completeness** | Good for experimentation; listed in docs but may be partial | Medium |
| 14 | **Multi-file project completion** | Registry exists; needs proper cross-file type resolution | Medium-large |

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
- **Generic function types**: `fn apply<F>(f: F, x: Int): Int` → `func apply[F any](f F, x int) int` — but Go generics don't support calling `f` without a constraint.
- **Dedicated `Fn` type**: `fn apply(f: Fn<Int, Int>, x: Int): Int` → `func apply(f func(int) int, x int) int` — explicit function type syntax.
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
- `with` statement (resource management)
- Type casting (`as` / `is`) with e2e tests
- `.new()` on Go types (zero-value construction)
- Tuple unpacking, string interpolation, imports, built-ins
