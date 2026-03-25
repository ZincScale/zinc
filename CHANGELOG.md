# Changelog

All notable changes to Zinc are documented in this file. Format follows [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

## [1.0.1] - 2026-03-25

### Changed
- **Self-contained zinc binary** — jpackage bundles JDK (javac, jlink, jdeps) + Mill + compiler. Zero external dependencies.
- **javac via javax.tools API** — no shell-out to javac, uses bundled JDK compiler module
- **In-process class loading** — scripts run via URLClassLoader, no shell-out to java
- **jpackage default** — `zinc build` produces bundled JRE apps (works with ALL Java libraries)
- **GraalVM native-image opt-in** — `zinc build --native` for max performance when libs support it

### Added
- Bundled Mill build tool — no separate Mill install needed
- `zinc build --package` (default) — jpackage + jlink with stripped JRE
- `zinc build --fat-jar` — uber jar via Mill assembly
- `zinc build --docker` — multi-stage Dockerfile with jlink JRE on distroless base
- `zinc build --native` — GraalVM native-image with reachability metadata + tracing agent
- GraalVM reachability metadata auto-download for native builds
- Tracing agent fallback for libraries without bundled metadata
- Package inference from directory structure (src/flow/worker.zn → package flow)
- Distroless Docker base (gcr.io/distroless/base-nossl-debian12:nonroot)
- jlink optimizations: --strip-debug --no-man-pages --compress=zip-6

## [1.0.0] - 2026-03-25

### Changed
- **Compiler rewritten from Go to Java** — self-hosted on Java 25, produces Java 25 output
- **JavaParser-based codegen** — generates Java AST instead of string concatenation
- **Static JDK type database** — replaces runtime reflection for type resolution
- **GraalVM native-image by default** — `zinc build` produces native binaries (~13MB, ~22ms)
- **No throws Exception** — interfaces and methods never declare throws, errors are values
- **Data classes → Java records** — `data Point(int x, int y)` → `record Point(int x, int y) {}`
- **Sealed classes → sealed interfaces** — enables Java 25 pattern matching in switch
- **Spawn → CompletableFuture** — with or-handler supervision and join error propagation
- **Concurrent/parallel → StructuredTaskScope** — with Joiner.awaitAllSuccessfulOrThrow

### Added
- `zinc run <file|dir>` — compile and execute in one command
- `zinc build <file|dir> [--native|--no-native]` — compile to Java + native binary
- `zinc init <name>` — scaffold new project
- Multi-file compilation with cross-file interface resolution
- Mill integration for project builds with Maven dependencies
- Expression lambdas for automatic void/value context inference
- Stream chain detection: `.filter().map().sum()` as single pipeline
- `it` keyword rewriting in stream operations
- `in` operator for contains checks
- Static type database for JDK stdlib (no runtime reflection)
- GraalVM native-image reflection config bundled

### Removed
- Go compiler (~15K lines)
- javap-based type introspection
- goreleaser, Homebrew formula

## [0.10.0] - 2026-03-17

### Added
- **Implicit return** — last expression in a function or method body is automatically returned. `Int square(Int x) { x * x }` just works.
- **Expression if** — `if` can be used in expression position: `var label = if x > 0 { "positive" } else { "negative" }`. Emits C# ternary.
- **Expression match** — `match` can be used in expression position: `var msg = match status { case 1 -> "running" case _ -> "unknown" }`. Emits C# switch expression.
- **Range loops** — `for i in 0..10` (exclusive) and `for i in 1..=10` (inclusive). New `..` and `..=` operators. Emits `Enumerable.Range()`.
- **`--release` flag** — `zinc build --release` strips debug symbols for smaller production binaries.
- **Runtime source maps** — default builds embed debug info (`DebugType=embedded`) so runtime exceptions show `.zn` file and line numbers via `#line` directives.
- **`using static Functions`** — standalone functions are now callable from `main()` without qualification.
- **Cross-package constructor fix** — `models.Dog("Rex")` now correctly emits `models.NewDog("Rex")` in Go backend.
- **Global TypeRegistry** — multi-directory Go projects share type info across all packages.
- 49 E2E tests (6 new: ImplicitReturnMethod, ExpressionIf, ExpressionIfNested, ExpressionMatch, RangeExclusive, RangeInclusive)

### Changed
- Go backend tests gated behind `//go:build gobackend` build tag — run with `go test -tags gobackend`
- Go backend hidden from user-facing CLI help, docs, and installer

## [0.5.0] - 2026-03-16

### Added
- **C# AOT backend** — new default backend targeting .NET 10 Native AOT. Produces 1-2 MB native binaries with ~9ms startup. Classes, interfaces, inheritance, generics, enums, error handling (try/catch), lambdas, string interpolation, safe navigation, and all control flow supported.
- **LINQ collection methods** — `Where`, `Select`, `First`, `FirstOrDefault`, `Last`, `Any`, `All`, `Count`, `Sum`, `Min`, `Max`, `Average`, `Aggregate`, `OrderBy`, `OrderByDescending`, `Take`, `Skip`, `Distinct`, `Zip`, `SelectMany`, `GroupBy`, `ToDictionary`, `ToList`, `ForEach` — all with E2E tests on .NET 10.
- **`zinc.toml` project config** — replaces `go.mod` for project setup. Supports project name/version, build target (csharp/go), optimization toggle, and `[dependencies]` for NuGet packages. No XML.
- **Full C# AOT build pipeline** — `zinc build` reads `zinc.toml`, transpiles `.zn` → `.cs`, generates `.csproj` internally, runs `dotnet publish` with AOT, copies native binary to project root.
- **`zinc run` for C# target** — transpile + `dotnet run` in one command.
- **List/map type inference** — list literals infer element type from contents (`List<int>` instead of `List<object>`), enabling typed LINQ operations.
- **Virtual/override detection** — C# codegen detects method overrides across parent/child classes and emits `virtual`/`override` keywords.
- **Benchmark harness** — Go vs C# AOT performance comparison (`benchmarks/csharp-aot/`). C# AOT 2-3x faster on Where+Select, 1.6 MB binary.

### Changed
- **Default backend** — C# AOT is now the default backend
- **Lambda syntax** — `=>` changed to `->` (matches Java/Kotlin, ergonomic)
- **Variable declaration syntax** — `:=` changed to `var x = expr` (ergonomic — avoids pinky-shift colon)
- **Match case syntax** — `case 1 => { }` changed to `case 1 -> { }`
- **With statement** — `with (f := expr)` changed to `with (f = expr)`
- **For-loop init** — `for i := 0;` changed to `for var i = 0;`
- **Tuple destructuring** — `(a, b) := expr` changed to `var (a, b) = expr`
- **Collection method names** — PascalCase C# LINQ naming (`Add`, `Remove`, `Contains`, `ToUpper`, `Keys`, etc.)
- **`zinc init`** — now creates `zinc.toml` + `main.zn` (was `go.mod` + `main.zn`)

### Removed
- Python backend prototype — removed in favor of C# AOT + Go dual-backend strategy

## [0.4.0] - 2026-03-11

### Added
- Generic class polymorphism — `fn printBox(b: Box<Int>)` correctly detects generic class params as interface-typed, field access uses getters, builtin methods aren't intercepted
- Generic empty list/map literal inference — `this.items = []` in generic class emits `[]T{}` instead of `[]interface{}{}`

### Removed
- LINQ-style collection methods (Where, Select, OrderBy, GroupBy, Aggregate, etc.) — removed from language

## [0.3.2] - 2026-03-10

### Added
- **Auto-generated interfaces for OO polymorphism** — each Zinc class now generates a Go struct (`NameImpl`) and a Go interface (`Name`) with getters, setters, and all public methods
- True polymorphic dispatch — functions accepting a class/interface type can receive any subclass, just like Java/C#/Kotlin
- Compile-time interface satisfaction checks (`var _ Interface = (*Impl)(nil)`)
- Field access through interface-typed parameters uses auto-generated getters/setters
- Safe navigation (`?.`) works correctly with interface types
- Failable method detection through interface-typed parameters — `v.validate()` on an interface-typed class param now correctly detects `(T, error)` and `error` returns
- Void-failable tracking (`voidCanThrowFns`) — methods returning only `error` (no value) emit `err :=` instead of `_, err :=`
- Auto `return nil` for void-failable methods/functions (prevents "missing return" in Go)
- Comprehensive e2e tests: polymorphism, error propagation chains, failable methods via interface, nested with, Go interop, getter collision

### Fixed
- `Optional<ClassName>` no longer generates pointer-to-interface (`*Dog`), which is invalid in Go
- Safe-nav field access on nullable class types uses getters instead of direct field access
- Getter/setter collision detection: if a class already defines `getX()`, the auto-generated getter is skipped
- Failable methods called on interface-typed variables were not detected as failable (error silently dropped)
- Void-failable class methods missing `return nil` at end of body

## [0.3.1] - 2026-03-10

### Added
- Colored error output with ANSI terminal detection (auto-disabled in CI/pipes)
- Project-mode errors now show `.zn` filename instead of directory path
  - Before: `type error[/home/user/myapp]: line 2: ...`
  - After: `type error[main.zn]: line 2: ...`
- Variadic functions, spread operator, multi-arg `.add()`
- Go interop auto-detection via `go/types` for error-returning functions and methods
- Parser→codegen method dispatch refactor (19 specialized AST nodes removed)

### Fixed
- Broken codegen for `defer`, raw strings, match failable detection

## [0.2.0] - 2026-03-10

### Added
- GitHub Actions CI with Go 1.23–1.26 matrix testing
- E2E smoke tests on Ubuntu, RHEL 8, RHEL 9, and Amazon Linux 2023
- `govulncheck` vulnerability scanning in CI pipeline
- Goreleaser for cross-platform binary releases (linux/mac/windows, amd64/arm64)
- Semantic versioning policy (`VERSIONING.md`)
- CHANGELOG.md

### Changed
- Minimum Go version bumped from 1.21 to 1.26
- Version is now injected at build time via ldflags

## [0.1.0] - 2025-01-01

Initial release of Zinc.

### Language Features
- Variables, functions, classes, interfaces, inheritance, generics
- Simplified constructor syntax (`new(...)`)
- Enums with `match` expressions
- Error handling — errors as values with auto-propagation and `or` handlers
- Closures and lambdas (including failable lambdas)
- Concurrency — goroutines and channels
- Default parameters and named arguments
- `with` statement for resource management (auto-close, auto-unlock)
- Type casting (`as` / `is`)
- Go type construction with named fields and automatic pointer inference
- Labeled `break`/`continue`
- Safe navigation `?.`
- Null safety (Kotlin-style)
- Callable function types (`Fn<(Params), Return>`)
- String interpolation
- Tuple unpacking for multi-return functions
- List/string slicing
- `const` declarations
- OO collection methods (`.add()`, `.remove()`, `.size()`, `.clone()`, `.sort()`, `.join()`)
- OO string methods (`.upper()`, `.lower()`, `.contains()`, `.startsWith()`, `.endsWith()`, `.trim()`, `.split()`, `.replace()`)
- Map utility methods (`.keys()`, `.values()`, `.containsKey()`)
- Built-in stdlib aliases (`readFile`, `writeFile`, `httpGet`, `jsonEncode`, `jsonDecode`, etc.)

### Tooling
- `zinc <file.zn>` — single file transpile
- `zinc init [name]` — project scaffolding
- `zinc build [dir]` — transpile + `go build`
- `zinc run [dir]` — transpile + `go run`
- `zinc repl` — interactive REPL
- `--run`, `--watch`, `--verbose`, `--version` flags
- Source maps via `//line` directives
- Multi-file project support with cross-file type registry
- 17 example programs + multi-file project example
