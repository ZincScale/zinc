# Dev Guide

For people working on the **zinc-go compiler itself** — adding language
features, fixing codegen bugs, extending the typechecker. If you're
writing Zinc code and the compiler is just a tool, you want
[language-tour.md](language-tour.md) instead.

## Build & test

```bash
# from compilers/zinc-go/
make build          # → ./zinc-go
make test           # full e2e suite (positive, negative, project tests)
go test ./...       # unit tests for individual passes
```

The e2e harness lives in `run_e2e.sh`. It transpiles every `examples/*.zn`
file, runs the resulting Go, and diffs the output against
`expected/<name>.txt`. Negative tests under `examples-fail/` assert the
compiler rejects with the expected substring. Project tests under
`examples-test/` run `zinc test` end-to-end.

## Pipeline

Source flows through four passes:

```
.zn source
    ↓  internal/lexer/        Tokenizer
    ↓  internal/parser/       AST builder
    ↓  internal/typechecker/  Type inference, type checking, binding
    ↓  internal/codegen_go/   Go code generator
.go output
```

Each pass owns its data; downstream passes consume the upstream's output
and don't reach back. The hard rule:

> **Typechecker is the single source of truth for type info.**
> Codegen reads `BoundProgram.NodeTypes` and friends; codegen does not
> re-derive types or maintain parallel side-maps.

The codegen_go package was migrated to this rule in early 2026 — three
parallel-state tracking maps were deleted in favor of consulting the
bound program. If you find yourself wanting to stash type info on the
codegen side, that's almost always a sign you should expose it from the
typechecker instead.

## Where to add things

| Change | Where |
|---|---|
| New keyword / token | `internal/lexer/lexer.go` + `internal/parser/parser.go` (parsing) + grammar update in `docs/grammar/` |
| New AST node | `internal/parser/ast.go` |
| New type rule | `internal/typechecker/` (mostly `bind.go` + `bind_walk.go`) |
| New codegen path | `internal/codegen_go/codegen_*.go` — split by area: `_exprs.go`, `_stmts.go`, `_types.go`, `_resolve.go`, `_calls.go`, `_chan_infer.go` |
| New stdlib API | `stdlib/src/<package>/` — written in Zinc, transpiled like any other source |
| New e2e example | `examples/<name>.zn` + `expected/<name>.txt`. Use `examples-fail/` for tests that should be rejected. |

## Stdlib

Zinc's standard library lives at `stdlib/src/`:

```
stdlib/src/
  errors/    BaseError + builtin error subclasses (IllegalArgumentError, IOError, ...)
  asserts/   test-helper assertions
  config/    viper-backed Config wrapper
  logging/   log/slog-backed LogManager
```

These are written in Zinc and transpiled the same way as user code. If
you're adding a stdlib API, write it as Zinc, not Go. The transpiler
treats stdlib calls as ordinary cross-package method calls — no
special-casing.

## Compiler-error UX

When a pass produces a user-facing error, structure it as:

1. **What went wrong** — concrete, locatable. Include the source line
   number and a snippet when feasible.
2. **Why it's rejected** — the rule being enforced.
3. **What to do instead** — the canonical shape, with a one-liner
   example if the fix isn't obvious.

The recent `return v, null` and `return v, errExpr()` rejections in
codegen_stmts.go are good templates: they name the form, explain the
contract, and point at the canonical shape.

## Conventions

- **No comments explaining what code does.** Identifier names carry
  that. Comments are reserved for *why*: hidden invariants, workarounds
  for specific bugs, surprising behavior.
- **Don't reference current task / fix / caller in code comments.** Those
  belong in the commit message; comments rot.
- **Test the fix, then write the test.** Negative tests (`examples-fail/`)
  are cheap insurance against silent regressions of compile errors.
- **Investigate before recommending.** When stuck, read the actual code
  / actual emitted Go — don't speculate about what a pass might do.

## Releasing

`v*` tags trigger the GitHub Actions release workflow
(`.github/workflows/zinc-go.yml`). The `release` job runs goreleaser,
which cross-compiles for linux/darwin/windows × amd64/arm64 and
publishes a GitHub Release with the archives + checksums.

```bash
git tag -a v0.X -m "v0.X — short summary"
git push origin v0.X
```

The workflow's `test` job (build + e2e + project lifecycle smoke test)
must pass before `release` runs.
