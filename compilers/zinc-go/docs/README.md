# Documentation

The Zinc-language compiler that targets Go. Pick the doc closest to what
you're trying to do.

## I want to...

### ...try Zinc out
- **[Getting Started](getting-started.md)** — install, hello world,
  project workflow (`zinc-go init / build / run / test`).
- **[Why Zinc](why-zinc.md)** — the rationale, in long form.

### ...write Zinc code
- **[Language Tour](language-tour.md)** — every feature with runnable
  examples. Start here if you've used Kotlin, TypeScript, or C#.
- **[Classes & Inheritance](classes.md)** — class system, sealed types,
  data classes, implicit-self.
- **[Error Handling](error-handling.md)** — `(T, error)` signatures,
  `catch { }` at call sites, propagation, fallbacks.
- **[Concurrency](concurrency.md)** — `spawn`, `Channel<T>`, `select`,
  `parallel for`, `timeout`.

### ...call Go from Zinc (or read the emitted Go)
- **[Interop with Go](interop-with-go.md)** — calling Go packages, the
  auto-pointerization rules, and what the emitted Go looks like.

### ...hack on the compiler itself
- **[Dev Guide](dev-guide.md)** — architecture, build/test workflow,
  where to add lexer / parser / typechecker / codegen changes.
- **[Grammar](grammar/01-grammar.md)** — formal grammar.
- **[Semantics](grammar/02-semantics.md)** — language semantics.
- **[Type System](grammar/03-type-system.md)** — type rules.
- **[Lessons Learned](grammar/00-lessons-learned.md)** — pre-rebuild
  retrospective; useful background for grammar work.

## Status

Current release: **[v0.7](https://github.com/ZincScale/zinc/releases/latest)**
(2026-05-07). E2E suite: 128 tests green. Grammar surface stamped
`v2-2026-05-01`.
