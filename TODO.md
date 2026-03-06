# Growler Feature Roadmap

Prioritized for shipping a usable language binary people can try out.

---

## Tier 1 — Next Up

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 1 | **List/string slicing** (`list[1:3]`, `s[2:]`) | Basic collection operation missing entirely; no `SliceExpr` AST node | Medium |

---

## Tier 2 — Infrastructure / Polish (make it shippable)

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 3 | **Source maps** | Map Go compiler errors back to `.gw` lines | Large |
| 4 | **Better map/list literal type inference** | Maps always emit `map[interface{}]interface{}`; lists infer only from first element | Medium |
| 5 | **Multi-file project completion** | Registry exists; needs cross-file type resolution | Medium-Large |
| 6 | **REPL completeness** | Listed in docs but may be partial | Medium |
| 7 | **Example coverage** | Many features have no `.gw` example | Quick |
| 8 | **`const` declarations** | AST + lexer token exist but not wired up; needed for immutability | Quick |
| 9 | **`range(n)` / `range(a, b)` iteration** | Reduces boilerplate for numeric loops; Python/Kotlin idiom | Quick |

---

## Tier 3 — More Language Features (after shippable)

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 10 | **Functional collection methods** (`.map()`, `.filter()`, `.reduce()`, `.forEach()`) | Core OO/FP pattern; loops are a workaround for now | Medium |
| 11 | **Operator overloading** | Natural for numeric classes, vectors, money types | Medium |
| 12 | **Enhanced destructuring** | `var (a, b, c) = ...` beyond 2-tuple; match on struct fields | Medium |
| 13 | **Interface default methods** | Reduces boilerplate for shared behaviour | Medium |
| 14 | **Variadic functions** (`...` params) | Common pattern, currently not supported | Quick-Medium |
| 15 | **Growler stdlib wrappers** | Real `io`, `http`, `json` API in Growler idioms | Large |

---

## Language Limitations (discovered via e2e tests)

### 1. Go Zero-Value Construction (`Type{}` not supported)

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
- Map utility methods (`.keys()`, `.values()`, `.containsKey()`)
- Callable function types (`Fn<(Params), Return>` → `func(params) return`)
- Fix `for (k, v) in map` codegen (IndexVar now emitted correctly)
- Null safety (Kotlin-style strict enforcement)
- Tuple unpacking, string interpolation, imports, built-ins
