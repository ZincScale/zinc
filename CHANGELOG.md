# Changelog

All notable changes to Zinc are documented in this file. Format follows [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

## [0.3.2] - 2026-03-10

### Added
- **Auto-generated interfaces for OO polymorphism** — each Zinc class now generates a Go struct (`NameImpl`) and a Go interface (`Name`) with getters, setters, and all public methods
- True polymorphic dispatch — functions accepting a class/interface type can receive any subclass, just like Java/C#/Kotlin
- Compile-time interface satisfaction checks (`var _ Interface = (*Impl)(nil)`)
- Field access through interface-typed parameters uses auto-generated getters/setters
- Safe navigation (`?.`) works correctly with interface types
- Polymorphism e2e tests

### Fixed
- `Optional<ClassName>` no longer generates pointer-to-interface (`*Dog`), which is invalid in Go
- Safe-nav field access on nullable class types uses getters instead of direct field access
- Getter/setter collision detection: if a class already defines `getX()`, the auto-generated getter is skipped

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
- `.new()` on Go types with named field construction
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
