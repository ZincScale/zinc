# Why Zinc

## The pattern every other host has, and Go doesn't

Look at the languages that run the working world, and notice what's
happened on each one over the last fifteen years:

- **Java got Kotlin.** JetBrains designed it for the JVM. Same runtime,
  same garbage collector, same enormous ecosystem — but classes that feel
  modern, sealed types, null safety, data classes, coroutines, and a
  surface that actively borrows from Scala and C# without taking on
  Scala's complexity. Kotlin took developer mindshare on the JVM in
  under a decade. Android adopted it as the official language. Spring
  ships first-class Kotlin support.

- **JavaScript got TypeScript.** Microsoft layered structural types onto
  JavaScript without breaking the runtime contract. Today, "serious JS"
  largely means TypeScript: Visual Studio Code, the Node.js
  documentation, every major framework's defaults — they're all
  TypeScript-first.

- **Ruby got Crystal.** Same surface syntax, statically typed, AOT
  compiled. And separately, Matz himself has been pushing on a Ruby
  AOT story (mruby and the newer compiler effort), tacit acknowledgment
  that interpreted Ruby alone is leaving performance on the table.

- **Python got Mypy, pyright, Mojo, Cython.** Each one chasing a
  different slice of "Python with types" or "Python that compiles."
  Mojo specifically pitches itself as "the Kotlin of Python."

- **Go has none of these.**

That last bullet is the thesis. Go is excellent for systems work. It's
small, it's fast, it deploys as a single binary, the runtime is
battle-tested, the toolchain is uniform. But the language is
deliberately minimal — and "deliberately minimal" leaves real
ergonomic gaps. Anyone arriving from Kotlin, TypeScript, C#, or even
Java feels them within a week.

## The specific gaps

- **No class inheritance.** Embedding works, but it's struct-shaped, not
  type-shaped. You can't say "Dog is an Animal" — you say "Dog has an
  embedded Animal," and you wire up method promotion by hand.
- **No sealed / ADT types.** An interface is an open set. There's no way
  to say "Shape is one of {Circle, Rect, Triangle}" and have the
  compiler enforce exhaustive handling.
- **No proper error syntax.** `if err != nil` is a *line*, repeated
  thousands of times across any non-trivial codebase. The community
  knows this; that's why every third blog post is "should we have try?"
- **No resource-cleanup expression.** `defer` is function-scoped, not
  block-scoped, which means you can't reliably flush a buffer and then
  read it in the same function. You write IIFEs to scope the defer.
- **No implicit-self.** Every method body starts with `c.this`,
  `c.that`, `c.theOther`. Reading is fine; writing is tedious.
- **No pattern matching.** Type switches exist, but they don't
  destructure data, they don't bind, they don't enforce exhaustiveness.
- **No string interpolation.** `fmt.Sprintf("%s:%d", host, port)`
  forever.
- **Generics arrived late and minimal.** Type parameters work; type
  parameter constraints do not yet feel native.

Each one is fixable individually. Together they're a tax — the kind of
tax developers reading their first Go program after twelve years of
Kotlin notice immediately.

## Why OO matters here

Look at the TIOBE top 5: Python, C++, Java, C#, JavaScript. Four of
those are class-first OO. The fifth (Python) is class-friendly. That's
how the working world thinks. Telling 80% of developers "give up the
abstraction model you've used for fifteen years to get a static binary"
is asking too much. Most of them won't, and the ones who do bounce off
the lack of inheritance, the missing sealed types, the absent data
classes.

Zinc lets them keep OO — full classes, single inheritance, sealed
types, data classes, interfaces — *and* get a Go-class static binary
out the back end. That's the bargain.

## Why AOT matters here

Zinc compiles to a Go binary. That has consequences:

- **Single static binary.** No JVM, no Node, no Python interpreter to
  install. The binary is the deploy artifact.
- **Tens of milliseconds to start.** Cold-start a Lambda, spin up a
  CronJob, exec in a container — done before anything else has finished
  reading the YAML.
- **Sub-10MB binaries are normal.** Stripped, with `-s -w`, common Zinc
  programs land in the 4-8MB range. The same program in a JVM
  distribution is hundreds of megabytes once you bundle the JRE.
- **Cross-compile to anything Go targets.** linux/amd64,
  linux/arm64, darwin/amd64, darwin/arm64, windows/amd64,
  windows/arm64 — `zinc-go build --cross os/arch` and you have a
  binary for it.
- **Drop-in deployable.** Kubernetes scratch containers. Lambda zip
  packages. systemd service files. There is no runtime layer to
  babysit.

These are the same reasons "rewrite it in Go" is a meme — Zinc lets you
do that rewrite without giving up the language features that brought
you to your current stack.

## Why "compiles to Go" specifically (not LLVM, not native)

Compiling to Go (the *language*, emitted as readable source the Go
toolchain then compiles) is a deliberate choice over going straight to
LLVM or producing native code directly:

- **The Go runtime is reused as-is.** Goroutines, the GC, the network
  poller, the file abstraction, the entire stdlib. None of these need
  reimplementing in Zinc. Zinc's `spawn { }` *is* `go func() { }()`.
  Zinc's `Channel<T>` *is* `chan T`. Etc.
- **The Go ecosystem is callable directly.** `import net/http` works.
  `import github.com/spf13/viper` works (after `zinc-go add`). Go
  packages are not "FFI" — they're libraries. There's no JNI, no
  cgo-style boundary, no marshalling layer. Zinc emits a regular Go
  call to a regular Go function.
- **Compilation reuses Go's whole-program optimizer.** Inlining, escape
  analysis, dead-code elimination, devirtualization — Zinc inherits all
  of it for free.
- **The output is auditable.** Zinc emits readable, formatted Go. You
  can read it. You can debug it. You can hand-edit the output if you
  ever need to. There's no black-box bytecode, no generated assembly.
  This is the same property that made TypeScript trustworthy: the
  emitted JS was readable, and you could always drop down to it.

The cost of this choice is that Zinc inherits Go's runtime model. No
threads-as-classes (you get goroutines). No exception-style stack
unwinding (you get `error` values). Zinc embraces those rather than
fighting them.

## Why now

Three things are converging:

1. **Go has crossed into "generic enterprise" status.** It's no longer
   a Kubernetes-only language; it's the default for new microservices,
   CLI tools, and infra glue across most large engineering orgs.
2. **The cohort writing those services trained on TypeScript, Kotlin,
   and C#.** They want classes, sealed types, and pattern matching by
   reflex. Go's minimalism is, for them, a downgrade.
3. **There's no incumbent.** Nobody else has shipped a Kotlin-class
   successor that targets Go. The slot is open.

Zinc is the answer to that. A typed, OO, ergonomic language that
compiles to readable Go, reuses the Go runtime and ecosystem, and
ships as a Go-class static binary. Same trick Kotlin played on the JVM,
TypeScript played on V8, Crystal played on Ruby — applied to the host
that didn't have one yet.

## Status

Zinc is at 1.0 maturity. The compiler's e2e suite is green
(126 examples covering classes, sealed types, generics, errors,
goroutines, FFI, pattern matching, the lot). It's in production use
as the implementation language for
[zinc-flow](https://github.com/ZincScale/zinc-flow), a NiFi-class data
flow engine. The grammar surface is stabilized as `v2-2026-05-01`.

If you want to read code: start with the [language tour](language-tour.md)
or jump straight into [getting started](getting-started.md).
