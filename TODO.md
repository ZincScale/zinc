# Zinc Feature Roadmap

Convention over configuration for native apps. C# AOT is the default backend, Go is secondary.

---

## Priority Order

### P1 — NuGet Import & Interop ✅ DONE
- Import → `using` mapping with 16 short aliases
- CSharpTypeResolver: .NET reflection probe (3,700+ types), auto `new` for constructors
- Generic annotations: `@Name("args")` → `[Name("args")]` — unlocks all C# attributes

### P2 — Scripting Builtins ✦ NEXT
Reduce ceremony for quick scripts:
- `args` — built-in `List<String>`, maps to command-line args
- `exec(cmd)` — run a shell command, return output as `String`, failable
- `fileExists(path)` — returns `Bool`
- `listDir(path)` — returns `List<String>`, failable
- `pathJoin(parts...)` — path joining
- **Effort:** Quick — just new builtins in codegen

### P3 — `zinc add` / Dependency Management
- `zinc add Newtonsoft.Json` → adds to `[dependencies]` in `zinc.toml`
- `zinc add Serilog --version 4.0.0` → pinned version
- `zinc remove Newtonsoft.Json` → removes dependency
- **Effort:** Medium

### P4 — Data Classes / Records
`data User(String name, Int age)` — immutable DTOs with auto-generated ToString/Equals/GetHashCode. Maps to C# `record`.
- **Effort:** Medium — **write design doc first**

### P5 — Typed Errors
Extend error handling with typed error classes. Maps to C# exception hierarchy.
- **Effort:** Medium — **write design doc first**

### P6 — Structured Concurrency
`await { }` blocks — maps to C# `Task.WhenAll` or Go `sync.WaitGroup`.
- **Effort:** Medium — **write design doc first**

### P7 — VS Code Extension (Syntax Highlighting)
Basic `.zn` editor support — TextMate grammar.
- **Effort:** Quick

### P8 — `zinc test`
Run tests without manual test commands. Maps to `dotnet test` or `go test`.
- **Effort:** Quick

### P9 — `zinc fmt`
Format `.zn` files consistently.
- **Effort:** Medium

---

## Interop Roadmap (by ecosystem)

All use cases are unblocked by P1 (imports + type resolver + annotations).

| Use Case | NuGet Packages | Status |
|----------|---------------|--------|
| **Logging** | Serilog / NLog | ✅ Ready |
| **HTTP client** | System.Net.Http | ✅ Ready |
| **Configuration** | Microsoft.Extensions.Configuration | ✅ Ready |
| **JSON serialization** | System.Text.Json / Newtonsoft | ✅ Ready (`@JsonPropertyName` works) |
| **Dependency injection** | Microsoft.Extensions.DI | ✅ Ready |
| **REST API** | ASP.NET Core | ✅ Ready (`@Route`, `@HttpGet` work) |
| **Database / ORM** | Entity Framework Core | ✅ Ready (`@Table`, `@Column` work) |
| **Testing** | xUnit / NUnit | ⚠ Needs `zinc test` command (P8) |

---

## Revisit Later

| Feature | Effort |
|---------|--------|
| Enhanced destructuring (`var (a, b, c) = ...`) | Medium |
| Operator overloading | Medium |
| Go interop improvements | Medium |
| Async/await bridging | Medium |

---

## Project Infrastructure

| Feature | Status |
|---------|--------|
| CI: .NET SDK in tests | ✅ Done |
| CONTRIBUTING.md | TODO |
| Code coverage reporting | TODO |

---

## Future — IDE & Ecosystem

| Feature | Effort |
|---------|--------|
| LSP server | Large |
| VS Code extension + LSP | Medium (after LSP) |
| Web playground | Large |
| `zinc doc` | Medium |

---

## Completed (v0.8.0)
- Generic annotations: `@Name("args")` → `[Name("args")]` in C#
- Annotations on classes, fields, methods, and functions
- No hardcoded annotation names — pass-through to C# attributes
- E2E tested with `@JsonPropertyName` serialization round-trip
- Split docs into 8 focused topic files + TOC
- Updated README with full documentation table
- 90 unit + 37 E2E + 5 resolver tests

## Completed (v0.7.0)
- NuGet import → `using` mapping with 16 short aliases
- CSharpTypeResolver: .NET reflection probe (3,700+ BCL types)
- Auto `new` for imported constructable classes
- Static class detection (skip `new`)
- Single `Functions` class for all non-main functions
- Unique exception variable names in catch blocks
- AOT trim fixes: SelfContained, JsonSerializer reflection
- Stale .cs cleanup in .zinc-build/

## Completed (v0.6.0)
- All 28 global builtin functions in C# backend
- Failable builtin infrastructure + `or { }` error handling
- `handlerHasHalt` + standalone failable ExprStmt

## Completed (v0.5.0)
- C# AOT backend with LINQ collection methods (22 methods)
- `zinc.toml` project config, full build pipeline
- Cross-file TypeRegistry, `#line` source maps
- Lambda `->`, `var` declarations, list/map type inference
- Virtual/override detection, benchmarks, CI

## Completed (v0.4.0 and earlier)
- Type-before-name syntax, auto-generated interfaces
- Generic class polymorphism, field visibility
- Error handling, Python backend prototype
- Full Go backend (3,326 lines), pointer inference
- REPL, watch mode, examples, CI
