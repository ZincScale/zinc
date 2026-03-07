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
| 2 | **Source maps** | Map Go compiler errors back to `.gw` lines | Large |
| 3 | **Multi-file project completion** | Registry exists; needs cross-file type resolution | Medium-Large |
| 4 | **REPL completeness** | Listed in docs but may be partial | Medium |

---

## Tier 3 — More Language Features (after shippable)

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 8 | **Functional collection methods** (`.map()`, `.filter()`, `.reduce()`, `.forEach()`) | Core OO/FP pattern; loops are a workaround for now | Medium |
| 9 | **Operator overloading** | Natural for numeric classes, vectors, money types | Medium |
| 10 | **Enhanced destructuring** | `var (a, b, c) = ...` beyond 2-tuple; match on struct fields | Medium |
| 11 | **Interface default methods** | Reduces boilerplate for shared behaviour | Medium |
| 12 | **Variadic functions** (`...` params) | Common pattern, currently not supported | Quick-Medium |
| 13 | **Growler stdlib wrappers** | Real `io`, `http`, `json` API in Growler idioms | Large |

---

## Known Limitations

### 1. Go Zero-Value Construction (`Type{}` not supported)

**The problem:** Growler has no syntax for constructing a zero-value Go struct. The parser sees `sync.Mutex{}` as a selector expression followed by an unrelated empty block, producing invalid Go.

**Chosen solution: `.new()` on Go types** — when codegen sees `X.new()` and `X` is not a known Growler class, emit `X{}` instead of `NewX()`.

```growler
var mu = sync.Mutex.new()    // → sync.Mutex{}
var buf = bytes.Buffer.new() // → bytes.Buffer{}
```

**Named fields** (`http.Client.new(timeout: 30)`) can follow later as a natural extension of named args.

---

## Completed
- Variables, functions, classes, interfaces, inheritance, generics
- Simplified constructor syntax (`new(...)` — no `construct` keyword needed)
- Enums + match
- Error handling (try/catch/throw)
- Closures / lambdas (including throwing lambdas)
- Concurrency (goroutines, channels)
- Default parameters + named arguments
- `with` statement (resource management, parenthesized syntax)
- `with` multi-return auto-detection (`with (var f = os.Create(path))`)
- Type casting (`as` / `is`)
- `.new()` on Go types (zero-value construction)
- Labeled `break`/`continue` (`@label for/while`, `break @label`)
- Safe navigation `?.` (`obj?.field`, `obj?.method()`)
- Null safety (Kotlin-style strict enforcement)
- Callable function types (`Fn<(Params), Return>` → `func(params) return`)
- OO collection methods (`.add()`, `.remove()`, `.size()`, `.clone()`, `.sort()`, `.join()`)
- OO string methods (`.upper()`, `.lower()`, `.contains()`, `.startsWith()`, `.endsWith()`, `.trim()`, `.split()`, `.replace()`)
- Map utility methods (`.keys()`, `.values()`, `.containsKey()`)
- `for (k, v) in map` codegen fix
- More stdlib aliases (`readFile`, `writeFile`, `httpGet`, `jsonEncode`, `jsonDecode`, `sprintf`, `typeOf`, `sleep`, `getEnv`, `setEnv`, `now`)
- Better map/list literal type inference (typechecker annotates AST → codegen emits typed literals)
- `const` declarations (top-level immutable values)
- Example coverage (17 `.gw` examples covering all major features)
- Tuple unpacking, string interpolation, imports, built-ins
