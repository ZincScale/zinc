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

## Project Infrastructure (parallel track)

These are about making the Zinc repo itself healthy — CI, releases, contribution workflow.

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| P1 | **GitHub Actions CI** | Run `go test ./...` on every push/PR; catch regressions before merge | Quick |
| P2 | **CI matrix testing** | Test across Go versions (1.21 → 1.26) and OS (Linux, macOS, Windows) | Quick |
| P3 | **E2e smoke tests in CI** | Transpile + compile + run example programs as a CI step, not just unit tests | Quick |
| P4 | **Semantic versioning policy** | Clear rules for 0.x bumps; tag releases properly | Quick |
| P5 | **Goreleaser** (cross-platform binaries) | Auto-build linux/mac/windows amd64/arm64 binaries on `git tag`; publish to GitHub Releases | Medium |
| P6 | **CHANGELOG.md** | Track what changed per release; auto-generate from commits or maintain manually | Quick |
| P7 | **Install script / Homebrew formula** | `brew install zinc` or `curl -sSL \| sh` — lower the barrier vs `git clone && go build` | Medium |
| P8 | **CONTRIBUTING.md** | How to set up dev environment, run tests, code style, PR process | Quick |
| P9 | **Issue & PR templates** | Structured bug reports, feature requests, PR checklists | Quick |
| P10 | **`.gitignore` cleanup** | Ignore generated `.go` files in examples, build artifacts, editor configs | Quick |
| P11 | **License headers / compliance check** | Ensure all source files have Apache 2.0 headers; add CI check | Quick |
| P12 | **Code coverage reporting** | Track test coverage %, upload to Codecov or similar, badge in README | Quick-Medium |

---

## Tier 2 — More Language Features

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 5 | **Operator overloading** | Natural for numeric classes, vectors, money types | Medium |
| 6 | **Interface default methods** | Reduces boilerplate for shared behaviour | Medium |
| 7 | **Zinc stdlib wrappers** | Real `io`, `http`, `json` API in Zinc idioms | Large |

---

## Tier 3 — Tooling & Developer Experience

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 8 | **VS Code extension** (syntax highlighting) | Basic `.zn` editor support — TextMate grammar for keywords, strings, types, comments | Quick |
| 9 | **`zinc fmt`** | Format .zn files consistently | Medium |
| 10 | **`zinc test`** | Run tests without manual `go test` | Quick |
| 11 | **Color error output** | Colored, well-formatted errors with source context | Quick |
| 12 | **Better project-mode error messages** | Show .zn filename (not dir) in multi-file type errors | Quick |
| 13 | **Error suggestions** | "Did you mean X?" on undefined variables/types, suggest fixes for common mistakes | Medium |

---

## Tier 4 — IDE & Language Intelligence

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 14 | **LSP server** (basic: diagnostics + go-to-definition) | Real-time errors and navigation in any editor; the #1 thing devs expect from a language | Large |
| 15 | **LSP: autocomplete + hover types** | Makes writing Zinc feel productive — type info, method suggestions, parameter hints | Large |
| 16 | **VS Code extension + LSP integration** | Full IDE experience — highlighting + inline errors + autocomplete + go-to-def | Medium (after LSP exists) |
| 17 | **`zinc debug`** (delve wrapper) | Step-through debugging; `//line` directives already map back to `.zn` source | Medium |
| 18 | **`zinc doc`** | Generate browsable docs from Zinc source (like `go doc`) | Medium |

---

## Tier 5 — Ecosystem & Adoption

| # | Feature | Why it matters | Effort |
|---|---------|---------------|--------|
| 19 | **Zinc package registry / `zinc.mod`** | Share and reuse Zinc libraries beyond copy-paste; currently limited to Go module system | Large |
| 20 | **Web playground** | Try Zinc in the browser — huge for onboarding, no install needed | Large |
| 21 | **REPL enhancements** (tab completion, syntax highlighting) | Makes `zinc repl` a productive exploration tool | Medium |
| 22 | **CI/CD templates** | GitHub Actions / Docker examples for Zinc projects | Quick |

---

## Completed
- Updated to Go 1.26.1 (minimum Go version bumped from 1.21)
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
