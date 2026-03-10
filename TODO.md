# Zinc Feature Roadmap

Language is shippable — core features, CLI tooling, multi-file projects, and error reporting all work.

---

## Tier 1 — Next Up (developer experience polish)

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 1 | **Functional collection methods** (`.map()`, `.filter()`, `.reduce()`, `.forEach()`) | Core OO/FP pattern; loops are a workaround for now | Medium |
| 2 | **Variadic functions** (`...` params) | Common pattern, currently not supported | Quick-Medium |
| 3 | **Enhanced destructuring** | `var (a, b, c) = ...` beyond 2-tuple; match on struct fields | Medium |

---

## Tier 2 — More Language Features

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 4 | **Operator overloading** | Natural for numeric classes, vectors, money types | Medium |
| 5 | **Interface default methods** | Reduces boilerplate for shared behaviour | Medium |
| 6 | **Zinc stdlib wrappers** | Real `io`, `http`, `json` API in Zinc idioms | Large |

---

## Tier 3 — Tooling Improvements

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 7 | **`zinc fmt`** | Format .zn files consistently | Medium |
| 8 | **`zinc test`** | Run tests without manual `go test` | Quick |
| 9 | **Better project-mode error messages** | Show .zn filename (not dir) in multi-file type errors | Quick |
| 10 | **Color error output** | Better developer experience | Quick |

---

## Completed
- Variables, functions, classes, interfaces, inheritance, generics
- Simplified constructor syntax (`new(...)` — no `construct` keyword needed)
- Enums + match
- Error handling (errors as values with auto-propagation and `or` handlers)
- Closures / lambdas (including failable lambdas)
- Concurrency (goroutines, channels)
- Default parameters + named arguments
- `with` statement (resource management, parenthesized syntax)
- `with` multi-return auto-detection (`with (var f = os.Create(path))`)
- Type casting (`as` / `is`)
- `.new()` on Go types (zero-value + named field construction: `url.URL.new(Scheme: "https", Host: "example.com")`)
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
- Example coverage (17 `.zn` examples covering all major features)
- REPL completeness (auto-print expressions, var persistence, brace-aware multi-line, help command)
- Tuple unpacking, string interpolation, imports, built-ins
- List/string slicing (`list[1:3]`, `s[2:]`, `.slice()` method)
- Source maps (`//line` directives map Go compiler errors back to `.zn` lines)
- Multi-file project completion (cross-file ctors, failable detection, default/named args via shared registry)
- `zinc init` project scaffolding (creates `go.mod` + `main.zn`)
- `zinc build` / `zinc run` for multi-file projects
- `--version` flag
- Type checker error line numbers (all errors now report source line)
