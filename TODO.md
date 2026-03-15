# Zinc Feature Roadmap

Language is shippable — core features, CLI tooling, multi-file projects, and error reporting all work.

Now targeting Go 1.26 — see "Go 1.26 Codegen Improvements" section for transpilation upgrades enabled by newer Go features.

---

## Priority Order

### P1 — Scripting Builtins
Reduce ceremony for quick scripts. Add thin builtin wrappers over Go stdlib:
- `args` — built-in `List<String>`, maps to `os.Args[1:]`
- `exec(cmd)` — run a shell command, return output as `String`, failable
- `fileExists(path)` — returns `Bool`, maps to `os.Stat` check
- `listDir(path)` — returns `List<String>`, failable, maps to `os.ReadDir`
- `pathJoin(parts...)` — maps to `filepath.Join`
- **Effort:** Quick — just new builtins in codegen, no parser/typechecker changes

### P2 — Pointer Inference for `.new()`
`.new()` on Go types auto-emits `&Type{}` when the receiving API expects a pointer. Context-aware: function argument and nested struct field contexts. Invisible to user — no pointer syntax in Zinc. Design doc: `docs/design-pointer-inference.md`
- **Effort:** Medium

### P3 — Annotations / Decorators
`@Json("name")`, `@Column("id")`, `@Serialize`, `@Validate`, `@Optional` — maps to Go struct tags. Familiar to Java/C#/Kotlin devs. Design doc: `docs/design-annotations-serialization.md`
- **Effort:** Medium

### P4 — Data Classes / Records
`data User(String name, Int age)` — immutable DTOs with auto-generated toString/equality. Kotlin `data class` / Java `record` pattern.
- **Effort:** Medium — **write design doc first** (interaction with annotations, serialization, and auto-generated interfaces needs careful thought)

### P5 — Typed Errors
Extend error handling with typed error classes. `is`/`as` operators and `or {}` handlers already work — this is mostly about error class conventions and codegen.

- **What already works:** `is`/`as` type operators, `or {}` handlers with `err` variable, failable functions, error wrapping
- **What's needed:**
  - Convention for error classes (auto-generate `Error() string` method implementing Go's `error` interface)
  - Codegen: emit `errors.AsType[T]` (Go 1.26) when `err is SomeError` appears in `or {}` blocks
  - Wire up `err as NotFoundError` to unwrap to the concrete type
- **Syntax — all existing constructs, no new keywords:**
  ```
  NotFoundError {
      String path
      new(String path) { this.path = path }
  }

  // Throwing (already works — failable return)
  return NotFoundError(path: "/users/42")

  // Catching (existing is/as + existing or {})
  user := db.findUser(id) or {
      if err is NotFoundError { return defaultUser() }
  }
  ```
- **Auto-propagation preserves types:** Current codegen passes `return _err` directly (no re-wrapping), so typed errors survive the call chain. `Error("context", baseErr)` wraps with `%w`, and `errors.AsType` walks `%w` chains — types preserved in both cases. Verified in codegen.
- **Design questions:** Should error classes require a `message` field? Auto-generate `Error()` from class name + fields? Interaction with error wrapping (`Error("context", baseErr)`)?
- **Effort:** Medium — **write design doc first**

### P6 — Structured Concurrency
Current `go { }` is fire-and-forget. Add a grouped concurrency construct that launches goroutines and waits for completion, leveraging `sync.WaitGroup.Go()` (Go 1.25).

- **Possible syntax:** `await { go { task1() } go { task2() } }` — transpiles to `WaitGroup.Go()` + `Wait()`
- **Needs design:** Error propagation from child goroutines, cancellation via context, result collection
- **Effort:** Medium — **write design doc first** (touches error handling, panic recovery, context propagation)

### P7 — VS Code Extension (Syntax Highlighting)
Basic `.zn` editor support — TextMate grammar for keywords, strings, types, comments.
- **Effort:** Quick

### P8 — Project-Wide Watch Mode
`zinc run --watch` / `zinc build --watch` — current `--watch` is single-file only; projects need auto-retranspile on any `.zn` change.
- **Effort:** Medium

### P9 — `zinc test`
Run tests without manual `go test`.
- **Effort:** Quick

### P10 — `zinc fmt`
Format `.zn` files consistently.
- **Effort:** Medium

### P11 — Error Suggestions
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
- Type-before-name syntax migration — C-style `Type name` declarations, `ReturnType Fn(Params)` function types, `Type... name` variadic, lambda return types always inferred (design doc: `docs/design-type-before-name.md`)
- Syntax simplification — dropped `class`/`fn`/`construct`/`var` keywords, parens-free `if`/`while`/`for`, `:=` inference, `name Type` declarations (design doc: `docs/design-syntax-simplification.md`)
- Failable tuple destructuring (`(val, err) := fn()` with auto-propagation)
- Python backend prototype — benchmarked Comprehension/NumPy/Numba strategies vs Go fused loops (design doc: `docs/design-python-codegen-strategy.md`, code: `internal/codegen_python/`, benchmarks: `benchmarks/python-strategies/`)
- Comprehensive Python codegen benchmark — 5 strategies (Comprehension/NumPy/Numba/Polars/DuckDB) across 26 benchmarks. (benchmarks: `benchmarks/collection-shootout/`)
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
- `with` multi-return auto-detection (`with (f := os.Create(path))`)
- Type casting (`as` / `is`)
- `.new()` on Go types (zero-value + named field construction: `url.URL.new(Scheme: "https", Host: "example.com")`)
- Labeled `break`/`continue` (`@label for/while`, `break @label`)
- Safe navigation `?.` (`obj?.field`, `obj?.method()`)
- Null safety (Kotlin-style strict enforcement)
- Callable function types (`ReturnType Fn(Params)` → `func(params) return`)
- OO collection methods (`.add()`, `.remove()`, `.size()`, `.clone()`, `.sort()`, `.join()`)
- OO string methods (`.upper()`, `.lower()`, `.contains()`, `.startsWith()`, `.endsWith()`, `.trim()`, `.split()`, `.replace()`)
- Map utility methods (`.keys()`, `.values()`, `.containsKey()`)
- `for (k, v) in map` codegen fix
- More stdlib aliases (`readFile`, `writeFile`, `httpGet`, `jsonEncode`, `jsonDecode`, `sprintf`, `typeOf`, `sleep`, `getEnv`, `setEnv`, `now`)
- Better map/list literal type inference (typechecker annotates AST → codegen emits typed literals)
- `const` declarations (top-level immutable values)
- Example coverage (18 `.zn` examples covering all major features)
- REPL completeness (auto-print expressions, var persistence, brace-aware multi-line, help command)
- Tuple unpacking, string interpolation, imports, built-ins
- List/string slicing (`list[1:3]`, `s[2:]`, `.slice()` method)
- Source maps (`//line` directives map Go compiler errors back to `.zn` lines)
- Multi-file project completion (cross-file ctors, failable detection, default/named args via shared registry)
- `zinc init` project scaffolding (creates `go.mod` + `main.zn`)
- `zinc build` / `zinc run` for multi-file projects
- `--version` flag
- Type checker error line numbers (all errors now report source line)
- Variadic functions (`Type... name` params), spread operator (`list...`), multi-arg `.add()`
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
- Generic class polymorphism (`printBox(Box<Int> b)` — generic class params detected as interface-typed, field access uses getters)
- Generic empty list/map literal inference (`this.items = []` in generic class → `[]T{}` not `[]interface{}{}`)
- Generic constructor type inference (Go infers type params from arguments — `Box.new(42)` → `NewBox(42)`)
- Map literal type inference (concrete Go types like `map[string]int` instead of `map[interface{}]interface{}`)
- Nested list literal type inference (`[][]int` for list-of-lists)
- Field and constant visibility (`pub` modifier, private by default — pub fields generate getters/setters, pub constants export as capitalized)
- Collection methods simplification — removed LINQ-style functional methods (filter/map/reduce); prefer for loops
