# Zinc Feature Roadmap

Language is shippable — core features, CLI tooling, multi-file projects, and error reporting all work.

Now targeting Go 1.26 — see "Go 1.26 Codegen Improvements" section for transpilation upgrades enabled by newer Go features.

---

## Priority Order

### P1 — Go 1.26 Codegen Improvements (Quick Wins)
Upgrade generated Go code to leverage features added in Go 1.22–1.26. These are small, isolated changes that improve output quality with no new Zinc syntax.

- **Range over integers (Go 1.22):** Emit `for i := range n` instead of `for i := 0; i < n; i++` for `range(n)` loops
- **`new(expr)` (Go 1.26):** Emit `new(value)` for nullable/pointer field initialization instead of helper closures
- **`errors.AsType[T]` (Go 1.26):** Use generic type-safe error matching in generated error handling where applicable
- **Loop variable scoping (Go 1.22):** No codegen change needed — Go now handles per-iteration scoping, removing a class of closure-capture bugs in generated code
- **Free performance wins (no codegen changes):** Swiss Tables maps (1.24), Green Tea GC (1.26), faster `io.ReadAll` (1.26), stack-allocated slices (1.26)
- **Effort:** Quick

### P2 — Collection Methods: Codegen Strategy Research
Before implementing collection methods, benchmark codegen strategies, study how mature languages/frameworks solve this, and design lambda shorthand syntax for ergonomic chaining.

- **Strategy A — Loop fusion (current design):** Compile `.Where().Select().First()` chains into a single fused for-loop. No intermediate allocations. Design doc: `docs/design-collection-methods.md`
- **Strategy B — Range-over-func iterators (Go 1.23+):** Emit `iter.Seq[T]` iterator functions that compose via `for v := range fn`. First-class language support may mean the Go compiler optimizes these better than when the design was originally written.
- **Strategy C — Intermediate slices (naive):** Each step materializes a new slice. Simplest to implement, worst for large collections, but possibly fine for small ones.
- **Benchmark plan:**
  - Write equivalent Go code for 3–5 representative chains (filter+map, filter+first, filter+map+reduce, filter+orderby+take)
  - Measure: allocations, throughput, binary size for all three strategies
  - Test at small (100), medium (10K), and large (1M) collection sizes
  - Compare compiler inlining/escape analysis output (`go build -gcflags='-m'`)
- **Idiom comparison — study how other languages do it:**
  - **C# LINQ:** Lazy `IEnumerable<T>` with deferred execution, materializes on terminal ops (`.ToList()`, `.First()`)
  - **Kotlin sequences:** `.asSequence()` for lazy, direct chaining for eager. Compiler does not fuse loops.
  - **Rust iterators:** Zero-cost lazy iterators, compiler inlines + fuses aggressively via monomorphization
  - **Java streams:** `.stream().filter().map().collect()` — lazy, but allocation-heavy; parallel streams for concurrency
  - **Swift:** Lazy via `.lazy` prefix, eager by default. Each step allocates.
  - **Key questions:** Does Go 1.23+ range-over-func get inlined like Rust iterators, or is it more like Java streams with overhead? Is loop fusion worth the codegen complexity, or do iterators perform "close enough"?
- **Lambda shorthand — current syntax is too verbose for chaining:**
  - Today: `list.Where((x: Int): Bool => x > 5).Select((x: Int): Int => x * 2)` — unusable
  - **Level 1 — Type inference:** `list.Where((x) => x > 5)` — infer param types + return type from method signature context
  - **Level 2 — Single-param shorthand:** `list.Where(x => x > 5)` — drop parens for single param (C#/TypeScript style)
  - **Level 3 — Implicit `it`:** `list.Where(=> it > 5)` — skip left side of `=>` entirely, implicit `it` parameter (Kotlin style)
  - Level 2 is the minimum bar for collection methods to feel right. Level 3 is nice-to-have.
  - **Arrow syntax:** Keep `=>` (consistent with C#/TypeScript, already used throughout Zinc). `->` is Kotlin/Java/Rust convention and could conflict with future return type annotations.
  - **Design questions:** How does type inference flow from method receiver type through chain? Does `it` conflict with any existing identifiers?
- **Output:** Update `docs/design-collection-methods.md` with benchmark results, idiom comparison, lambda shorthand design, and final codegen decision
- **Effort:** Quick-Medium (research only, no Zinc code changes)

### P3 — Functional Collection Methods (Implementation)
LINQ-style chaining: `.Where()`, `.Select()`, `.SelectMany()`, `.Aggregate()`, `.OrderBy()`, `.GroupBy()`, `.Any()`, `.All()`, `.First()`, `.Take()`, `.Skip()`, `.Distinct()`, `.ToList()`, `.ToDictionary()`, `.ForEach()`

- **Depends on:** P2 (codegen strategy decision)
- **Implementation order:** single-step methods → chain AST → codegen (per P2 decision) → short-circuit terminals → materialization segmentation
- **Effort:** Medium-Large

### P4 — Annotations / Decorators
`@Json("name")`, `@Column("id")`, `@Serialize`, `@Validate`, `@Optional` — maps to Go struct tags. Familiar to Java/C#/Kotlin devs. Design doc: `docs/design-annotations-serialization.md`
- **Effort:** Medium

### P5 — Data Classes / Records
`data class User(name: String, age: Int)` — immutable DTOs with auto-generated toString/equality. Kotlin `data class` / Java `record` pattern.
- **Effort:** Medium — **write design doc first** (interaction with annotations, serialization, and auto-generated interfaces needs careful thought)

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
