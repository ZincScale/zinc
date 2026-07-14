# Why Zinc

Zinc is a personal language-design and compiler project. It explores a typed,
object-oriented surface language that emits readable Go and reuses the Go
toolchain, runtime, and package ecosystem.

It is not intended to replace Go, compete for mainstream language adoption,
or ask teams to accept the opportunity cost of a new general-purpose language.
Its value is in the implementation work, the design experiments, and its use in
the author's own projects.

## What the project explores

Go deliberately keeps its language small. Zinc experiments with a different
surface over the same practical runtime model:

- classes, single inheritance, interfaces, sealed types, and data classes;
- exhaustive pattern matching and destructuring;
- structured syntax for Go-style error values;
- implicit self, string interpolation, and block-scoped resource cleanup;
- goroutines, channels, selection, and parallel iteration through higher-level
  syntax;
- direct use of Go packages from Zinc code.

These are compiler and language-design questions, not claims that the resulting
language should displace its host.

## Why emit Go

Emitting Go keeps the project focused on language design rather than rebuilding
a production runtime:

- Go supplies garbage collection, scheduling, channels, networking, and the
  standard library.
- The Go compiler supplies optimization, native-code generation, and portable
  static binaries.
- Generated output is readable and auditable.
- Existing Go packages can be called directly rather than through a separate
  foreign-function runtime.

The tradeoff is deliberate: Zinc inherits Go's runtime and interoperability
rules, and some source-language features require non-trivial lowering into Go.
Those mapping problems are themselves a central part of the project.

## Scope and status

The compiler and its test suite remain useful for existing personal projects
and future experiments. The grammar has a versioned surface, the generated Go
can be inspected directly, and the implementation is documented for anyone
interested in compiler construction.

There is no mainstream-adoption or commercial-product objective. Future work is
driven by personal use, curiosity, and the value of the experiment.

To explore it, start with the [language tour](language-tour.md) or
[getting started](getting-started.md).
