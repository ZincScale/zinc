# Zinc Feature Roadmap

Convention over configuration for native apps. C# AOT is the default backend, Go is secondary.

---

## Priority Order

### P1 — NuGet Import & Interop ✅ DONE (Phases 1-2), Phase 3 deferred
Enable Zinc projects to use third-party .NET libraries seamlessly.

**Phase 1: Import → Using mapping** ✅ DONE
- `import "Newtonsoft.Json"` → `using Newtonsoft.Json;`
- Short aliases: `import "http"` → `using System.Net.Http;`, `import "json"` → `using System.Text.Json;`, etc.
- `[dependencies]` in `zinc.toml` → `<PackageReference>` in generated `.csproj`

**Phase 2: CSharpTypeResolver** ✅ DONE
- .NET reflection probe discovers 3,700+ types across 130+ namespaces
- Auto-detects constructable classes → `Stopwatch()` emits `new Stopwatch()`
- Auto-detects static classes → `Console`, `Math`, `File` correctly skip `new`

**Phase 3: Advanced interop** — deferred, depends on P2 (annotations)
- Async/await bridging: `.Result` or `await` for Task-returning methods
- Attribute pass-through: `@JsonProperty("name")` → `[JsonProperty("name")]`
- Consuming C# generics/interfaces from Zinc
- **Effort:** Large — blocked by annotations (P2)

### P2 — Annotations / Decorators
`@Json("name")`, `@Column("id")`, `@Serialize`, `@Validate` — maps to C# `[Attribute]` or Go struct tags. Critical for ORM, serialization, and web framework interop.
- Unlocks: Entity Framework, System.Text.Json, ASP.NET model binding
- **Effort:** Medium — design doc exists: `docs/design-annotations-serialization.md`

### P3 — Scripting Builtins
Reduce ceremony for quick scripts. Add thin builtin wrappers:
- `args` — built-in `List<String>`, maps to command-line args
- `exec(cmd)` — run a shell command, return output as `String`, failable
- `fileExists(path)` — returns `Bool`
- `listDir(path)` — returns `List<String>`, failable
- `pathJoin(parts...)` — path joining
- **Effort:** Quick — just new builtins in codegen

### P4 — `zinc add` / Dependency Management
CLI command to add NuGet packages without editing `zinc.toml` manually:
- `zinc add Newtonsoft.Json` → adds to `[dependencies]` in `zinc.toml`
- `zinc add Serilog --version 4.0.0` → pinned version
- `zinc remove Newtonsoft.Json` → removes dependency
- Auto-resolves latest version from NuGet if no version specified
- **Effort:** Medium

### P5 — Data Classes / Records
`data User(String name, Int age)` — immutable DTOs with auto-generated ToString/Equals/GetHashCode. Maps to C# `record` or Go struct with generated methods.
- **Effort:** Medium — **write design doc first**

### P6 — Typed Errors
Extend error handling with typed error classes. Maps to C# exception hierarchy.
- **Effort:** Medium — **write design doc first**

### P7 — Structured Concurrency
`await { }` blocks — maps to C# `Task.WhenAll` or Go `sync.WaitGroup`.
- Needs async/await story for C# backend
- **Effort:** Medium — **write design doc first**

### P8 — VS Code Extension (Syntax Highlighting)
Basic `.zn` editor support — TextMate grammar for keywords, strings, types, comments.
- **Effort:** Quick

### P9 — Project-Wide Watch Mode
`zinc run --watch` / `zinc build --watch` — auto-retranspile on any `.zn` change.
- **Effort:** Medium

### P10 — `zinc test`
Run tests without manual test commands. Maps to `dotnet test` or `go test`.
- **Effort:** Quick

### P11 — `zinc fmt`
Format `.zn` files consistently.
- **Effort:** Medium

---

## Interop Roadmap (by ecosystem)

These unlock real-world enterprise use cases. Import mapping (P1) is done; most depend on annotations (P2).

| Use Case | NuGet Packages | Status | Remaining |
|----------|---------------|--------|-----------|
| **Logging** | Serilog / NLog | ✅ Ready | Import mapping works now |
| **HTTP client** | System.Net.Http | ✅ Ready | `HttpClient()` auto-emits `new` |
| **Configuration** | Microsoft.Extensions.Configuration | ✅ Ready | Import mapping works now |
| **JSON serialization** | System.Text.Json / Newtonsoft | ⚠ Partial | Works for static calls; annotations needed for class decoration |
| **Dependency injection** | Microsoft.Extensions.DI | ⚠ Partial | Constructor injection works; service registration needs static calls |
| **REST API** | ASP.NET Core | ❌ Blocked | Needs annotations (`@Route`, `@Get`) |
| **Database / ORM** | Entity Framework Core | ❌ Blocked | Needs annotations (`@Table`, `@Column`), data classes |
| **Testing** | xUnit / NUnit | ❌ Blocked | Needs `zinc test` command, test annotations |

---

## Revisit Later

| Feature | Why it matters | Effort |
|---------|---------------|--------|
| **Enhanced destructuring** | `var (a, b, c) = ...` beyond 2-tuple | Medium |
| **Operator overloading** | Natural for numeric classes, vectors, money types | Medium |
| **Go interop improvements** | GoTypeResolver enhancements for more Go libraries | Medium |

---

## Project Infrastructure

| Feature | Status | Effort |
|---------|--------|--------|
| **CI: .NET SDK in tests** | ✅ Done | — |
| **CONTRIBUTING.md** | TODO | Quick |
| **Code coverage reporting** | TODO | Quick-Medium |

---

## Future — IDE & Ecosystem

| Feature | Why it matters | Effort |
|---------|---------------|--------|
| **LSP server** | Real-time errors and navigation in any editor | Large |
| **VS Code extension + LSP** | Full IDE experience | Medium (after LSP) |
| **Web playground** | Try Zinc in the browser | Large |
| **`zinc doc`** | Generate browsable docs from Zinc source | Medium |

---

## Completed (v0.7.0)
- NuGet import → `using` mapping with 16 short aliases (http, json, io, regex, etc.)
- CSharpTypeResolver: .NET reflection probe discovers 3,700+ BCL types at transpile time
- Auto `new` for imported constructable classes (Stopwatch, HttpClient, StringBuilder, etc.)
- Static class detection (Console, Math, File skip `new`)
- Single `Functions` class for all non-main functions (was emitting duplicate class per function)
- Unique exception variable names in catch blocks (nested try/catch safe)
- AOT trim fixes: SelfContained=true, JsonSerializerIsReflectionEnabledByDefault=true
- Stale .cs file cleanup in .zinc-build/ before regenerating
- 82 unit + 35 E2E + 5 resolver tests for C# backend

## Completed (v0.6.0)
- All 28 global builtin functions in C# backend
- Failable builtin infrastructure (callIsFailable, bodyIsFailable, fixed-point transitive marking)
- `or { }` error handling for C# failable builtins — try/catch with `err` binding
- `handlerHasHalt` — skip auto-propagation when handler ends with exit/panic
- Standalone failable ExprStmt support (`writeFile(...) or { }` as statement)
- `examples/builtins.zn` — new example covering all builtin categories
- Updated docs: builtins.md (C# column), language-reference.md, getting-started.md

## Completed (v0.5.0)
- C# AOT backend with 37 unit + 17 E2E tests
- LINQ collection methods (22 methods)
- `zinc.toml` project config (no XML)
- Full C# AOT build pipeline (`zinc build` → native binary)
- Cross-file TypeRegistry for C# backend
- `#line` source map directives
- Lambda syntax `->` (was `=>`)
- Variable declaration `var x = expr` (was `:=`)
- List/map type inference for C# backend
- Virtual/override detection for C# classes
- Go vs C# AOT performance benchmarks
- Dynamic dotnet lookup in E2E tests
- CI with .NET 10 SDK
- Updated installer, Homebrew formula, docs

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
- Pointer inference for Go type construction
- REPL, watch mode, all examples
