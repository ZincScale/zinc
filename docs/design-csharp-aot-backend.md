# C# AOT Backend Evaluation — Design Doc

## Context

Zinc transpiles `.zn` files to Go, providing OO syntax (classes, interfaces, generics, inheritance) over Go's non-OO runtime. As the language matures and users interact with more Go ecosystem libraries, a tension is emerging: **Go's idioms leak through the abstraction**.

Examples of friction:
- `http.HandlerFunc` — function types implementing interfaces (alien to OO developers)
- `io.Reader` / `io.Writer` — single-method implicit interfaces threaded everywhere
- `context.Context` — first-param convention with no language-level support
- Middleware as `func(next http.Handler) http.Handler` — function chains, not class hierarchies
- Struct embedding instead of true inheritance
- `error` as a bare interface, not a typed exception hierarchy

Enterprise feedback indicates **ecosystem friction** is the primary adoption blocker — not binary size or startup time. This doc evaluates C# AOT as an alternative backend where the target ecosystem natively speaks OO.

## Zinc-to-C# Concept Mapping

This is where C# AOT is compelling. Nearly every Zinc concept maps 1:1 to C#, eliminating hundreds of lines of codegen shims:

| Zinc Concept | Go (current) | C# (proposed) | Codegen complexity |
|---|---|---|---|
| `Dog { }` | `DogImpl` struct + `Dog` interface + getters/setters | `class Dog` | **~5x simpler** |
| `Dog : Animal` | Struct embedding (approximate) | `class Dog : Animal` | **Direct** |
| `interface Speaker` | Implicit satisfaction | `interface ISpeaker` | **Direct** |
| `pub String name` | Lowercase field + `GetName()`/`SetName()` | `public string Name { get; set; }` | **Direct** |
| Private fields | Lowercase naming convention | `private` keyword | **Direct** |
| `new(String name)` | Factory function `NewDog(name)` | `public Dog(string name)` | **Direct** |
| Generics `List<T>` | `[T any]` constraints | `<T>` with `where` constraints | **Direct** |
| `or {}` error handling | `if err != nil` chains | `try/catch` or Result pattern | **Simpler** |
| `print()` | `fmt.Println()` | `Console.WriteLine()` | **Same** |
| `go { }` | `go func() { }()` | `Task.Run(() => { })` | **Similar** |
| Lambda `(x) => x * 2` | `func(x int) int { return x * 2 }` | `(x) => x * 2` | **Direct** |
| String interpolation `"{name}"` | `fmt.Sprintf("%v", name)` | `$"{name}"` | **Direct** |
| `match` | `switch` | `switch` / pattern matching | **Better** (C# patterns) |
| `with` (resources) | `defer` cleanup | `using` statement | **Direct** |
| Enum | `const` + `iota` | `enum` | **Direct** |

### What the Go backend does that C# wouldn't need

| Go codegen complexity | Lines | C# equivalent |
|---|---|---|
| Auto-generated interfaces per class | ~200 | Not needed — classes ARE types |
| Getter/setter method generation | ~150 | C# properties, 1 line each |
| `interfaceVars` / `classVars` tracking | ~100 | Not needed — type system handles dispatch |
| `isBuiltinReceiver()` dispatch logic | ~80 | Not needed — no name collision risk |
| Pointer inference (2 phases) | ~150 | Not needed — C# handles ref/value semantics |
| `*Impl` struct naming convention | ~50 | Not needed — classes are classes |
| **Total eliminated** | **~730 lines** | **0 lines** |

### Estimated codegen size comparison

| Backend | Estimated lines | Complexity |
|---|---|---|
| Go (`internal/codegen/`) | 3,326 | High — OO shims, pointer tracking, interface generation |
| Python (`internal/codegen_python/`) | 633 | Low — duck typing eliminates most shims |
| **C# (projected)** | **~1,200-1,500** | **Medium — 1:1 OO mapping, but need .NET type resolution** |

C# would be simpler than Go (no OO shims) but more complex than Python (C# has a real type system that needs accurate emission).

## Ecosystem Fit

### Go ecosystem — fighting the paradigm

```zinc
// What the user wants to write:
LoggingMiddleware : Middleware {
    Handler next
    new(Handler next) { this.next = next }
    pub handle(Request req, Response res) {
        log(req.path)
        this.next.handle(req, res)
    }
}

// What Go's net/http actually expects:
// func(next http.Handler) http.Handler — a function returning a function
// Zinc must bridge this gap with wrapper code
```

Every Go library that uses function types, implicit interfaces, or channel patterns requires Zinc to build translation layers. This complexity grows with every new Go package users want to consume.

### C# ecosystem — speaking the same language

```zinc
// The same Zinc code maps directly to C#:
LoggingMiddleware : IMiddleware {
    Handler next
    new(Handler next) { this.next = next }
    pub handle(Request req, Response res) {
        log(req.path)
        this.next.handle(req, res)
    }
}

// C# output — no wrapper needed:
// class LoggingMiddleware : IMiddleware {
//     private IHandler next;
//     public LoggingMiddleware(IHandler next) { this.next = next; }
//     public void Handle(HttpRequest req, HttpResponse res) {
//         Console.WriteLine(req.Path);
//         this.next.Handle(req, res);
//     }
// }
```

### Library ecosystem comparison

| Category | Go | C# (.NET) | Zinc fit |
|---|---|---|---|
| HTTP/Web | net/http (function-oriented) | ASP.NET Core (class-oriented) | **C# wins** |
| ORM/Database | sqlx, gorm (struct tags) | Entity Framework (class mapping) | **C# wins** |
| Serialization | encoding/json (struct tags) | System.Text.Json (attributes/source gen) | **C# wins** |
| Dependency Injection | Manual / wire (codegen) | Microsoft.Extensions.DI (native) | **C# wins** |
| Logging | slog (function calls) | ILogger (interface) | **C# wins** |
| Testing | testing.T (function-based) | xUnit/NUnit (class-based) | **C# wins** |
| gRPC | protoc-gen-go (generated) | protoc-gen-csharp (generated) | Tie |
| CLI tools | cobra/flag | System.CommandLine | Tie |
| Concurrency | goroutines + channels | async/await + Tasks | **Go wins** (simpler model) |
| Cross-compilation | `GOOS=linux go build` | Need native toolchain per OS | **Go wins** |

## Performance Comparison

### Collection Processing (1M elements)

Using your existing Go benchmark data as baseline, with C# numbers from published BenchmarkDotNet and .NET performance blog results:

| Benchmark | Go (your data) | C# .NET 10 (published data, scaled) | Winner |
|---|---|---|---|
| **Where+Select** | 7.8 ms | ~1.5-2 ms | **C# (4-5x)** — LINQ deferred + devirtualization |
| **First** | 400 µs | ~300-400 µs | Tie |
| **Aggregate/Sum** | 353 µs | ~170-200 µs | **C# (~2x)** — RyuJIT SIMD |
| **Take(10)** | 246 ns | ~200-300 ns | Tie |
| **Complex chain** | 372 µs | ~400-500 µs | Tie / slight Go edge |

C# is faster on bulk numeric processing (vectorization, SIMD). Go wins on early exit and low-overhead operations. For real application code (not tight loops), they're within 10-20% of each other.

### Operational Characteristics

| Factor | Go | C# AOT (.NET 10/11) | Notes |
|---|---|---|---|
| Binary size (hello world) | ~2 MB | ~1.5-3 MB | C# has closed the gap |
| Binary size (web server) | ~7 MB | ~10-15 MB | Go still smaller |
| Binary size (full app) | ~10-15 MB | ~15-40 MB | Go wins |
| Startup time | 1-5 ms | 5-30 ms | Go faster, both acceptable |
| Memory (RSS) | 3-6 MB | 6-12 MB | Go ~2x less |
| Cross-compile | Trivial | **Not supported cross-OS** | Go wins significantly |
| Single binary | Yes | Yes (self-contained) | Both |

### Benchmarks Game: Go vs C# AOT (full programs)

From the [official Benchmarks Game](https://benchmarksgame-team.pages.debian.net/benchmarksgame/fastest/go-csharpaot.html):

| Benchmark | Go | C# AOT | Winner |
|---|---|---|---|
| fannkuch-redux | 8.36s | 2.16s* | C# AOT |
| n-body | 6.39s | 3.13s* | C# AOT |
| spectral-norm | 5.34s | 0.74s* | C# AOT |
| mandelbrot | 3.77s | 2.96s* | C# AOT |
| fasta | 3.74s | 1.16s | C# AOT |
| k-nucleotide | 7.58s | 3.12s | C# AOT |
| binary-trees | 14.21s | 6.22s | C# AOT |
| pidigits | 0.82s* | 0.75s* | C# AOT |
| regex-redux | 3.23s | 1.36s* | C# AOT |

\* May use hand-written vector instructions or unsafe optimizations.

C# AOT dominates on compute-heavy benchmarks. Go's advantages are operational (size, startup, memory, cross-compile), not raw throughput.

From [programming-language-benchmarks](https://programming-language-benchmarks.vercel.app/go-vs-csharp): **Go wins 16, C# wins 17** — essentially tied across diverse workloads.

## C# AOT Limitations

### Hard limitations (still present in .NET 10/11)

| Limitation | Impact on Zinc |
|---|---|
| No `Assembly.LoadFile` (dynamic loading) | None — Zinc doesn't need plugins |
| No `Reflection.Emit` (runtime codegen) | None — transpiler controls all emitted code |
| Trimming breaks reflection-heavy libraries | Low — Zinc generates explicit code, no reflection |
| No cross-OS compilation | **High** — need CI runners per OS, or PublishAotCross |
| `System.Linq.Expressions` uses interpreter | Low — Zinc would emit for loops, not LINQ expressions |

### The cross-compilation problem

This is the single biggest disadvantage vs Go:

```bash
# Go: trivial
GOOS=linux GOARCH=amd64 go build -o myapp

# C# AOT: need native toolchain for each target
dotnet publish -r linux-x64    # only works ON Linux
dotnet publish -r osx-arm64    # only works ON macOS ARM
dotnet publish -r win-x64      # only works ON Windows
```

**Mitigation**: CI/CD with multi-OS runners (GitHub Actions, etc.) or the [PublishAotCross](https://github.com/MichalStrehovsky/PublishAotCross) NuGet package. This is workable for enterprise (they already have CI) but a step backward for the "build anywhere, run anywhere" story.

## Implementation Approach

### Architecture: shared frontend, pluggable backend

```
.zn source
    ↓
Lexer → Parser → AST → Typechecker
    ↓                        ↓
codegen/          codegen_csharp/
(Go backend)      (C# backend)
    ↓                        ↓
.go output            .cs output
    ↓                        ↓
go build          dotnet publish -r <rid> /p:PublishAot=true
```

Same pattern as the Python backend — new package `internal/codegen_csharp/` consuming the same AST.

### Type resolver: CSharpTypeResolver (analogous to GoTypeResolver)

Instead of `go/types`, use .NET reflection or Roslyn APIs to introspect NuGet packages:

```go
type CSharpTypeResolver struct {
    // Resolves .NET assembly metadata
    // - Does method return Task<T>? (failable detection)
    // - What are the constructor parameters?
    // - Is this an interface or class?
}
```

This could shell out to a small C# helper tool that uses Roslyn to dump type metadata as JSON, consumed by the Zinc transpiler (which is written in Go).

### Zinc CLI changes

```bash
zinc build                    # default: Go backend (unchanged)
zinc build --target csharp    # C# AOT backend
zinc build --target python    # Python backend (existing prototype)
```

### Error handling strategy

Zinc's `or {}` maps to C# exceptions — this is a natural fit since .NET libraries already throw exceptions and C# developers expect `try/catch`.

```zinc
content := readFile("data.txt") or { print("failed") }
```
```csharp
string content;
try {
    content = File.ReadAllText("data.txt");
} catch (Exception ex) {
    Console.WriteLine("failed");
    throw;  // or {} always propagates
}
```

This is simpler than the Go backend's `if err != nil` chains and matches C# conventions exactly. No Result type wrapper needed — exceptions are the idiomatic C# error handling mechanism, and Zinc's `or {}` semantics (always propagate unless `exit()`/`panic()`) map directly to `throw`.

### Phased implementation

**Phase 1: Core language (estimated ~2 weeks)**
- Classes, interfaces, inheritance, generics
- Fields with visibility (pub/private → public/private)
- Constructors, methods, static methods
- Basic types: Int, Float, String, Bool, List, Map
- Control flow: if/else, for, while, match
- Print, string interpolation
- Constants, enums
- E2E tests: transpile → `dotnet build` → run → assert output

**Phase 2: Error handling + ecosystem (estimated ~2 weeks)**
- `or {}` → try/catch
- Failable detection for .NET methods
- CSharpTypeResolver (Roslyn-based metadata introspection)
- Import mapping: `import "net/http"` → `using Microsoft.AspNetCore.*`
- Lambda expressions

**Phase 3: Advanced features (estimated ~2 weeks)**
- `go {}` → `Task.Run()`
- `with` → `using`
- Safe navigation `?.`
- Cross-file registry support
- Source map directives (`#line`)
- `zinc build --target csharp` CLI integration

## Risks and Open Questions

### 1. Import mapping complexity
Go packages map to different .NET namespaces. `import "net/http"` doesn't have a 1:1 C# equivalent — it maps to `Microsoft.AspNetCore.Http`, `Microsoft.AspNetCore.Builder`, `Microsoft.AspNetCore.Hosting`, etc. This mapping layer could grow complex.

**Mitigation**: Start with a curated mapping table for the most common packages. Users can add custom mappings via config.

### 2. Zinc assumes Go semantics in some places
- Value vs reference semantics: Go structs are values by default, C# classes are references. Zinc's pointer inference was built for Go's model.
- Multiple return values: Go functions return `(value, error)`. C# uses exceptions or tuples.
- Go's implicit interface satisfaction vs C#'s explicit `: IFoo`.

These would require Zinc to evolve its semantic model to be backend-agnostic.

### 3. Two ecosystems to maintain
Every new Zinc feature needs implementation in both backends. The Python prototype showed this is manageable if the AST is the contract, but it doubles the ongoing maintenance surface.

### 4. Developer toolchain
Go requires only `go` installed. C# AOT requires the .NET SDK (~500 MB) plus native toolchains. This is heavier and may not be acceptable for all users.

### 5. Cross-compilation gap
Enterprise users deploying to Linux from Mac/Windows development machines will need CI-based builds. This is standard practice but a step backward from Go's `GOOS=linux go build`.

## Decision Framework

| If Zinc's primary audience is... | Best backend |
|---|---|
| DevOps/CLI tool builders | **Go** — small binaries, fast startup, cross-compile |
| Enterprise application developers | **C#** — ecosystem fit, OO libraries, familiar patterns |
| Data scientists / ML engineers | **Python** — NumPy/Polars ecosystem |
| All of the above | **Multi-backend** — let users choose |

## Recommendation

**Proceed with a C# AOT backend prototype.** The OO concept mapping is so natural that the codegen will be significantly simpler than the Go backend. The enterprise feedback is clear: ecosystem friction matters more than binary size.

Suggested approach:
1. Build a minimal prototype (`internal/codegen_csharp/`) covering classes, interfaces, inheritance, and basic types
2. Run the same E2E test suite against both Go and C# output
3. Evaluate real-world friction: pick 3 common enterprise patterns (HTTP handler, database access, JSON serialization) and compare the Zinc code + generated output for both backends
4. Decide whether to promote C# to a first-class backend based on results

The Go backend remains the default and is not going away. This is additive — same strategy as the Python backend, but with a much stronger ecosystem fit argument.

## References

- [.NET Native AOT deployment overview](https://learn.microsoft.com/en-us/dotnet/core/deploying/native-aot/)
- [ASP.NET Core + Native AOT (.NET 10)](https://learn.microsoft.com/en-us/aspnet/core/fundamentals/native-aot)
- [Benchmarks Game: Go vs C# AOT](https://benchmarksgame-team.pages.debian.net/benchmarksgame/fastest/go-csharpaot.html)
- [Programming Language Benchmarks: Go vs C#](https://programming-language-benchmarks.vercel.app/go-vs-csharp)
- [.NET 10 Performance Improvements](https://devblogs.microsoft.com/dotnet/performance-improvements-in-net-10/)
- [PublishAotCross — cross-OS native AOT](https://github.com/MichalStrehovsky/PublishAotCross)
- [NetFabric/LinqBenchmarks](https://github.com/NetFabric/LinqBenchmarks)
- [.NET 11 Preview 1](https://www.infoq.com/news/2026/02/dotnet-11-preview1/)
