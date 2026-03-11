# Zinc Feature Roadmap

Language is shippable — core features, CLI tooling, multi-file projects, and error reporting all work.

---

## Priority Order

### P1 — Functional Collection Methods
LINQ-style chaining: `.Where()`, `.Select()`, `.SelectMany()`, `.Aggregate()`, `.OrderBy()`, `.GroupBy()`, `.Any()`, `.All()`, `.First()`, `.Take()`, `.Skip()`, `.Distinct()`, `.ToList()`, `.ToDictionary()`, `.ForEach()`

- **Codegen strategy:** Loop fusion — compile entire chain into single fused for-loop. No lazy iterators, no intermediate slices.
- **Lambda syntax:** `x => expr` and `(a, b) => expr` (expression-body only for v1)
- **Design doc:** `docs/design-collection-methods.md`
- **Implementation order:** lambdas → single-step methods → chain AST → loop fusion → short-circuit terminals → materialization segmentation
- **Effort:** Medium-Large

### P2 — Annotations / Decorators
`@Json("name")`, `@Column("id")`, `@Serialize`, `@Validate`, `@Optional` — maps to Go struct tags. Familiar to Java/C#/Kotlin devs. Design doc: `docs/design-annotations-serialization.md`
- **Effort:** Medium

### P3 — Data Classes / Records
`data class User(name: String, age: Int)` — immutable DTOs with auto-generated toString/equality. Kotlin `data class` / Java `record` pattern.
- **Effort:** Medium

### P4 — VS Code Extension (Syntax Highlighting)
Basic `.zn` editor support — TextMate grammar for keywords, strings, types, comments.
- **Effort:** Quick

### P5 — Project-Wide Watch Mode
`zinc run --watch` / `zinc build --watch` — current `--watch` is single-file only; projects need auto-retranspile on any `.zn` change.
- **Effort:** Medium

### P6 — `zinc test`
Run tests without manual `go test`.
- **Effort:** Quick

### P7 — `zinc fmt`
Format `.zn` files consistently.
- **Effort:** Medium

### P8 — Error Suggestions
"Did you mean X?" on undefined variables/types, suggest fixes for common mistakes.
- **Effort:** Medium

---

## Revisit Later

| Feature | Why it matters | Effort |
|---------|---------------|--------|
| **Enhanced destructuring** | `var (a, b, c) = ...` beyond 2-tuple; match on struct fields | Medium |
| **Operator overloading** | Natural for numeric classes, vectors, money types | Medium |

---

## Project Infrastructure (parallel track)

| Feature | Why it matters | Effort |
|---------|---------------|--------|
| **CONTRIBUTING.md** | How to set up dev environment, run tests, code style, PR process | Quick |
| **Issue & PR templates** | Structured bug reports, feature requests, PR checklists | Quick |
| **Code coverage reporting** | Track test coverage %, upload to Codecov or similar, badge in README | Quick-Medium |

---

## Future — IDE & Language Intelligence

| Feature | Why it matters | Effort |
|---------|---------------|--------|
| **LSP server** (diagnostics + go-to-definition) | Real-time errors and navigation in any editor | Large |
| **LSP: autocomplete + hover types** | Type info, method suggestions, parameter hints | Large |
| **VS Code extension + LSP integration** | Full IDE experience — highlighting + inline errors + autocomplete | Medium (after LSP) |
| **`zinc debug`** (delve wrapper) | Step-through debugging; `//line` directives map back to `.zn` | Medium |
| **`zinc doc`** | Generate browsable docs from Zinc source | Medium |

---

## Future — Ecosystem & Adoption

| Feature | Why it matters | Effort |
|---------|---------------|--------|
| **Zinc package registry / `zinc.mod`** | Share and reuse Zinc libraries | Large |
| **Web playground** | Try Zinc in the browser — huge for onboarding | Large |
| **REPL enhancements** (tab completion, highlighting) | Makes `zinc repl` a productive tool | Medium |
| **CI/CD templates** | GitHub Actions / Docker examples for Zinc projects | Quick |

---

## Completed
- Color error output with ANSI colors (auto-disabled in CI/piped output)
- Project-mode errors now show .zn filename instead of directory path
- Updated to Go 1.26.1 (minimum Go version bumped from 1.21)
- GitHub Actions CI with matrix testing (Go 1.23–1.26) and `govulncheck`
- E2e smoke tests in CI (`scripts/smoke-test.sh` — transpile + compile + run all 18 examples)
- Semantic versioning policy (`VERSIONING.md`)
- Goreleaser cross-platform releases (linux/mac/windows, amd64/arm64)
- CHANGELOG.md
- `.gitignore` cleanup for generated `.go` files
- License headers + CI compliance check (Apache 2.0 on all source files)
- Install script (`install.sh`) + Homebrew formula (`Formula/zinc.rb`)
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
- Variadic functions (`name: ...Type` params), spread operator (`list...`), multi-arg `.add()`
- Go interop auto-detection via `go/types` for error-returning functions and methods
- Method-level failable detection (variable type tracking for `f.Write()`, `f.Close()`, etc.)
- Parser→codegen method dispatch refactor (removed 19 specialized AST nodes; builtin methods handled in codegen)
- Class/Go-type-aware builtin dispatch (`.add()` on a class calls the method, not `append`)
- Dead code removal (`TOKEN_PRIVATE`, `TOKEN_ARROW`, `FieldDecl.IsPrivate`)
- Auto-generated interfaces for OO polymorphism (class → struct `Impl` + interface, getters/setters, compile-time satisfaction checks)
- Polymorphic function parameters (interface-typed params use getters, concrete `*Impl` uses direct field access)
- Safe navigation works with interface types (`d?.name` → `d.GetName()` for nilable interface-typed vars)
- Failable method detection through interface-typed params (`v.validate()` correctly detects error returns)
- Void-failable tracking for class methods (auto `return nil`, correct `err :=` vs `_, err :=`)
- Generic class polymorphism (`fn printBox(b: Box<Int>)` — generic class params detected as interface-typed, field access uses getters)
- Generic empty list/map literal inference (`this.items = []` in generic class → `[]T{}` not `[]interface{}{}`)
- Generic constructor type inference (Go infers type params from arguments — `Box.new(42)` → `NewBox(42)`)
