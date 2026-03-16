# Zinc Feature Roadmap

Convention over configuration for native apps. C# AOT is the default backend, Go is secondary.

---

## Priority Order

### P1 — C# Backend Completeness
Bring the C# backend to feature parity with the Go backend:
- Multi-file project support (cross-file TypeRegistry for C# codegen)
- Import mapping (Zinc imports → `using` statements + NuGet packages)
- CSharpTypeResolver (Roslyn-based .NET type introspection, analogous to GoTypeResolver)
- `#line` directives for source maps
- **Effort:** Medium-Large

### P2 — Scripting Builtins
Reduce ceremony for quick scripts. Add thin builtin wrappers:
- `args` — built-in `List<String>`, maps to command-line args
- `exec(cmd)` — run a shell command, return output as `String`, failable
- `fileExists(path)` — returns `Bool`
- `listDir(path)` — returns `List<String>`, failable
- `pathJoin(parts...)` — path joining
- **Effort:** Quick — just new builtins in codegen, no parser/typechecker changes

### P3 — Annotations / Decorators
`@Json("name")`, `@Column("id")`, `@Serialize`, `@Validate`, `@Optional` — maps to C# attributes or Go struct tags. Familiar to Java/C#/Kotlin devs. Design doc: `docs/design-annotations-serialization.md`
- **Effort:** Medium

### P4 — Data Classes / Records
`data User(String name, Int age)` — immutable DTOs with auto-generated toString/equality. Kotlin `data class` / C# `record` pattern.
- **Effort:** Medium — **write design doc first**

### P5 — Typed Errors
Extend error handling with typed error classes. Maps to C# exception hierarchy or Go typed errors.
- **Effort:** Medium — **write design doc first**

### P6 — Structured Concurrency
`await { }` blocks — maps to C# `Task.WhenAll` or Go `sync.WaitGroup.Go()`.
- **Effort:** Medium — **write design doc first**

### P7 — VS Code Extension (Syntax Highlighting)
Basic `.zn` editor support — TextMate grammar for keywords, strings, types, comments.
- **Effort:** Quick

### P8 — Project-Wide Watch Mode
`zinc run --watch` / `zinc build --watch` — auto-retranspile on any `.zn` change.
- **Effort:** Medium

### P9 — `zinc test`
Run tests without manual test commands.
- **Effort:** Quick

### P10 — `zinc fmt`
Format `.zn` files consistently.
- **Effort:** Medium

---

## Revisit Later

| Feature | Why it matters | Effort |
|---------|---------------|--------|
| **Enhanced destructuring** | `var (a, b, c) = ...` beyond 2-tuple | Medium |
| **Operator overloading** | Natural for numeric classes, vectors, money types | Medium |

---

## Project Infrastructure

| Feature | Why it matters | Effort |
|---------|---------------|--------|
| **CI: .NET SDK in smoke tests** | C# AOT E2E tests need .NET 10 in CI | Quick |
| **CONTRIBUTING.md** | Dev environment setup, test patterns, PR process | Quick |
| **Code coverage reporting** | Track test coverage %, badge in README | Quick-Medium |

---

## Future — IDE & Ecosystem

| Feature | Why it matters | Effort |
|---------|---------------|--------|
| **LSP server** | Real-time errors and navigation in any editor | Large |
| **VS Code extension + LSP** | Full IDE experience | Medium (after LSP) |
| **Web playground** | Try Zinc in the browser | Large |
| **`zinc doc`** | Generate browsable docs from Zinc source | Medium |

---

## Completed (v0.5.0)
- C# AOT backend prototype with 37 unit + 17 E2E tests
- LINQ collection methods (Where, Select, First, Any, All, Sum, Min, Max, OrderBy, Take, Skip, Distinct, Aggregate, GroupBy, ToDictionary, etc.)
- `zinc.toml` project config (no XML)
- Full C# AOT build pipeline (`zinc build` → native binary)
- Lambda syntax `->` (was `=>`)
- Variable declaration `var x = expr` (was `x := expr`)
- List/map type inference for C# backend
- Virtual/override detection for C# classes
- Go vs C# AOT performance benchmarks
- Pointer inference for Go type construction ✅

## Completed (v0.4.0 and earlier)
- Type-before-name syntax migration
- Auto-generated interfaces for OO polymorphism
- Generic class polymorphism
- Field and constant visibility (`pub` modifier)
- Error handling (errors as values, auto-propagation, `or` handlers)
- Python backend prototype + benchmarks (deferred)
- Color error output, source maps, multi-file projects
- CI with matrix testing, govulncheck, smoke tests
- Full Go backend (3,326 lines codegen)
- REPL, watch mode, all examples
