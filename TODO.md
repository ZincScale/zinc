# Zinc Feature Roadmap

Convention over configuration for native apps. C# AOT is the default backend, Go is secondary.

---

## Priority Order

### P1 — NuGet Import & Interop ✦ NEXT
Enable Zinc projects to use third-party .NET libraries seamlessly. This is the #1 blocker for real-world adoption.

**Phase 1: Import → Using mapping**
- `import "Newtonsoft.Json"` → `using Newtonsoft.Json;`
- `import "Serilog"` → `using Serilog;`
- Auto-map common namespaces: `import "http"` → `using Microsoft.AspNetCore.*`
- Zinc `import` generates `using` directives in C# output
- `[dependencies]` in `zinc.toml` → `<PackageReference>` in generated `.csproj`
- **Effort:** Medium

**Phase 2: CSharpTypeResolver**
- Introspect .NET assemblies at transpile time (analogous to GoTypeResolver using `go/types`)
- Auto-detect which methods throw exceptions (failable detection for .NET APIs)
- Auto-detect method signatures for named/default arg resolution
- Options: shell out to a Roslyn helper, or parse NuGet package XML metadata
- **Effort:** Medium-Large

**Phase 3: Interop patterns**
- Consume C# classes/interfaces from Zinc (use them as types in Zinc code)
- Calling static methods: `JsonConvert.SerializeObject(obj)` works via SelectorExpr
- Async/await bridging: `.Result` or `await` for Task-returning methods
- Attribute pass-through: `@JsonProperty("name")` → `[JsonProperty("name")]`
- **Effort:** Large

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

These unlock real-world enterprise use cases and depend on P1 + P2:

| Use Case | NuGet Packages | Zinc Features Needed |
|----------|---------------|---------------------|
| **REST API** | ASP.NET Core | Import mapping, annotations (`@Route`, `@Get`) |
| **JSON serialization** | System.Text.Json / Newtonsoft | Annotations (`@Json`), data classes |
| **Database / ORM** | Entity Framework Core | Annotations (`@Table`, `@Column`), data classes |
| **Logging** | Serilog / NLog | Import mapping (straightforward) |
| **HTTP client** | System.Net.Http | Import mapping, async/await |
| **Dependency injection** | Microsoft.Extensions.DI | Constructor injection (natural fit for Zinc classes) |
| **Configuration** | Microsoft.Extensions.Configuration | Import mapping |
| **Testing** | xUnit / NUnit | `zinc test` command, test annotations |

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
