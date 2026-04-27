# zinc-csharp porting plan

Delta between `zinc-go` (the shipping Zinc→Go transpiler) and
`zinc-csharp` (today just a build tool), and the work required to
bring a full Zinc→C# transpiler online.

## State of play

### zinc-go — what exists

Zinc language, lexer, parser, AST, codegen, stdlib, build tool, tests.

| Piece | LOC | Notes |
|---|---|---|
| `internal/lexer/` | ~550 | Tokens, keyword table, scanner |
| `internal/parser/` | ~3200 | AST + parser (hand-rolled, recursive-descent) |
| `internal/codegen_go/` | ~6800 | Zinc AST → Go source |
| `cmd/zinc/` | ~1500 | CLI: build / run / test / init / fmt / add / deps |
| `stdlib/src/` | ~300 | errors, logging, config, asserts (Zinc source) |
| `examples/` | 65 e2e tests | Cover all language features |
| Build tooling | Makefile + `.goreleaser.yml` + `install.sh` | Cross-platform release |

Core language features covered:
- Primitives, strings (interp), collections, generics
- Classes (inheritance, sealed, data classes, interfaces)
- Match expressions + destructure
- Error model: `Err` base class, auto-widening via `stmtCanReturnError`,
  `or { }` handling, `return errVal` emits `return zero, errVal`
- Concurrency: `spawn`, `parallel for`, `concurrent { }`, `timeout { }`
- Imports (slash-form Go-style), cross-package subpackage resolution,
  external dep class-decl loading (`loadDepClassDecls`)
- Line directives (`//line file.zn:N`) for error fidelity

### zinc-csharp — what exists

Build tool only. No transpiler.

| Piece | Size | What it is |
|---|---|---|
| `build-tool/zinc-csharp` | 571 LOC bash | Scaffolds `.csproj`, delegates to `dotnet build/run/test`, handles AOT flags, cross-compile, test runner |
| `install.sh` | 140 LOC | Installs .NET SDK + build tool, PATH wiring |
| `docs/design-pooling.md` | Design doc | Memory/pooling strategy for runtime (forward-looking) |
| `tests/e2e/hello/` | 1 test | **Hand-written C#** (`Hello/Program.cs`), not transpiled |
| `zinc.toml` | Project config | Schema exists, parsed by build tool |

What's **missing**: the entire Zinc→C# transpiler. There is no `.zn → .cs`
translation anywhere. The hello test is hand-written C#.

## The delta

| Component | zinc-go has | zinc-csharp has | Work |
|---|---|---|---|
| Language spec (docs, grammar) | Yes, implicit in parser + docs/ | N/A | Reuse |
| Lexer | Hand-rolled Go | — | Port to C#, OR share via cross-lang grammar |
| Parser + AST | Hand-rolled Go | — | Same |
| Codegen | Zinc AST → Go | — | **New**: Zinc AST → C# |
| Stdlib | Zinc `.zn` sources, compiled to Go | — | Same `.zn` sources should compile to C# — stdlib is language-level, not backend-level |
| E2E test suite | 65 tests, each `.zn` + `expected/` | 1 hand-written hello | Reuse the `.zn` sources, add C#-specific `expected/` outputs |
| Build tool | `zinc build/run/test/…` Go CLI | `zinc-csharp build/run/…` bash | Both exist; design question below |
| Packaging | Go → static binary via goreleaser | .NET → Native AOT via dotnet | Different mechanics, both already work |

## Design questions to answer tomorrow

### Q1. Parser/AST sharing strategy

The lexer/parser/AST is backend-agnostic — same Zinc source, same AST
regardless of target. Three options:

- **A. Copy-port.** Hand-translate the Go lexer/parser/AST into C#. No
  coupling between the two repos but any language change needs to be
  applied twice. Matches what zinc-go did (vs. an earlier Python port
  that was abandoned).
- **B. Shared library.** Extract parser into a language-agnostic repo
  (protobuf-defined AST? JSON AST emitted by a thin Go tool?). Both
  backends consume it. Strongest consistency but biggest infrastructure.
- **C. Transpiler-as-service.** C# backend shells out to `zinc-go`'s
  parser, receives AST as JSON/protobuf, generates C#. Fastest to
  bootstrap, keeps one source of truth for the parser. Runtime
  dependency on Go binary.

**Recommended:** start with **C** for fast bootstrap; revisit once the
C# codegen is feature-complete.

### Q2. Error model in C#

Zinc's error model (as of today): any class with `Error() string` in its
chain is an error. Function returning one auto-widens to `(T, error)`
in Go.

C# has three plausible mappings:

- **Exceptions.** `throw new ConfigError(...)`. Natural C# idiom but
  directly contradicts Zinc's "no exceptions, errors are values"
  principle.
- **`Result<T>` / discriminated union.** Closer to Zinc's semantics —
  the function returns `Result<Provider>` which is either `Ok(value)`
  or `Err(exception)`. Requires defining a Result type in the runtime.
- **Nullable reference + out error.** `Provider TryGetProvider(string
  name, out Err error)`. Matches the Go pattern mechanically.

**Open for your call.**

### Q3. Concurrency primitives

Zinc's `spawn`, `parallel for`, `concurrent { }` map cleanly to Go
goroutines. In C#:
- `spawn` → `Task.Run(...)` fire-and-forget
- `parallel for` → `Parallel.ForEach` or `Task.WhenAll`
- `concurrent { }` → `Task.WhenAll` for all arms, or `Task.WhenAny` for
  `first: true`
- `timeout(dur) { }` → `CancellationTokenSource` + `Task.Delay`

Straightforward mechanical mapping. No open design questions.

### Q4. Stdlib

Zinc's stdlib (`stdlib/src/errors`, `logging`, `config`, `asserts`) is
written in Zinc and compiles to both backends. The existing sources
should transpile to C# with zero changes — *IF* the codegen covers the
features used there. Verify that first as the stdlib compile test.

### Q5. Build tool unification

`zinc-csharp` (bash) and `zinc` (Go CLI) both exist. Differences:

- `zinc build` for a project generates Go in `zinc-out/` and delegates
  to `go build`. One tool.
- `zinc-csharp build` scaffolds a `.csproj` and delegates to
  `dotnet build`. Build tool only — no transpilation.

If we add a C# backend, the mental model could be:
- `zinc build --target=go` (current default)
- `zinc build --target=csharp` (new)
- `zinc-csharp` becomes the .NET-specific build wrapper that `zinc
  build --target=csharp` delegates to for `.csproj` generation + AOT
  flags.

Alternative: keep them completely separate — `zinc` for Go, `zinc-csharp`
for C#, duplicated project-config parsing. Simpler but inconsistent.

## Proposed first-session plan (tomorrow)

Scope-narrow bootstrap to get a `.zn` file compiling to running C#.

1. **Design decisions**: answer Q1, Q2, Q5 at minimum.
2. **Scaffolding**: pick the approach; create the C# codegen repo/module.
3. **Hello-world transpile**: `print("hello")` → `Console.WriteLine("hello")`.
   End-to-end: `.zn` → parse → codegen → `.cs` → `dotnet run`.
4. **One more test**: a class with a method, to exercise the OO
   codegen path.
5. **Inventory**: list every zinc-go e2e test and mark which language
   features it exercises. That's the roadmap for the C# codegen
   feature-complete sprint.

## Files to reference tomorrow

- `zinc-go/internal/codegen_go/codegen_types.go` — class → struct +
  constructor emission
- `zinc-go/internal/codegen_go/codegen_stmts.go` — statement-level
  emission incl. `emitReturnStmt`, `stmtCanReturnError`, or-handler
- `zinc-go/examples/` — 65 test inputs, paired with `expected/` outputs
- `zinc-go/stdlib/src/` — stdlib Zinc sources (should transpile as-is)
- `zinc-csharp/build-tool/zinc-csharp` — existing .csproj
  generation + dotnet wrapping, ready to plug in
- `zinc-csharp/docs/design-pooling.md` — runtime pooling design,
  relevant when emitting hot-path code
