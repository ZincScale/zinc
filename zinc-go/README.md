<p align="center">
  <img src="../logo.png" alt="Zinc" width="320">
</p>

# Zinc

[![Build](https://github.com/ZincScale/zinc/actions/workflows/ci.yml/badge.svg)](https://github.com/ZincScale/zinc/actions)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](../LICENSE)

**Zinc is a typed, OO language that compiles to Go.** The same shape Kotlin
has to the JVM, TypeScript has to JavaScript, Crystal has to Ruby — Zinc
brings to Go.

```zinc
import stdlib/errors

sealed class Shape {
    data Circle(double radius)
    data Rect(double w, double h)
}

pub double area(Shape s) {
    match (s) {
        case Circle(r) { return 3.14159 * r * r }
        case Rect(w, h) { return w * h }
    }
    return 0.0
}

pub (int, error) parseSide(String s) {
    if (s == "") { return errors.IllegalArgumentError("empty") }
    return 4, null
}

void main() {
    var n = parseSide("4") catch { print("bad: ${err}"); return }
    var shapes = [Circle(1.0), Rect(2.0, 3.0)]
    for (s in shapes) { print("area = ${area(s)}") }
    print("n = ${n}")
}
```

One static binary out, no runtime, no GC layer to babysit. Reads like Kotlin
or TypeScript; runs like Go.

## Why Zinc

Every other major language has acquired a typed, ergonomic successor that
targets it natively:

| Host | Successor | Brought |
|---|---|---|
| JVM | **Kotlin** | classes, sealed types, null safety, data classes |
| JavaScript | **TypeScript** | static types, classes, generics |
| Ruby | **Crystal** | static types, AOT compilation |
| Python | **Mojo / Cython** | static types, AOT path for hot code |
| Go | — | (this is the gap Zinc fills) |

Go is excellent for systems work but deliberately minimal: no class
inheritance, no sealed/ADT types, no proper error syntax beyond
`if err != nil`, no resource-cleanup expression, no implicit-self, no
pattern matching, no string interpolation. Developers coming from
Kotlin, TypeScript, or C# feel the friction immediately.

Zinc closes the gap *without* abandoning anything that makes Go great.

- **You keep OO.** Four of the TIOBE top five languages are OO. Telling
  most working developers "give up classes to get a static binary" is
  asking too much. Zinc gives them classes, inheritance, sealed types,
  data classes, *and* the Go binary.
- **You keep AOT.** Single static binary. Tens-of-milliseconds startup.
  Sub-10MB executables. Cross-compile to anything Go targets. Drops
  straight into Kubernetes, Lambda, scratch containers — no runtime
  layer.
- **You keep the Go ecosystem.** `import net/http`, `import sync`,
  `import github.com/spf13/viper` — Go packages import directly into
  Zinc and call natively. No wrappers, no FFI shims, no JNI. The
  generated Go code is readable; you can audit, debug, or hand-edit it.
- **You keep Go's runtime.** Goroutines, channels, the GC, the stdlib —
  battle-tested at scale, reused as-is. Zinc surfaces them as `spawn`,
  `Channel<T>`, `select { }`, `parallel for`.

See [docs/why-zinc.md](docs/why-zinc.md) for the long version.

## Feature highlights

### Errors are values, declared in the signature

```zinc
import stdlib/errors

pub (int, error) parseInt(String s) {
    if (s == "") { return errors.IllegalArgumentError("empty input") }
    return 42, null
}

void main() {
    var n = parseInt(input) catch { print("bad input: ${err}"); return }
    use(n)
}
```

A function whose declared return type ends in `error` is a thrower —
`error` (bare), `(T, error)`, or `(T1, ..., Tn, error)`. Callers handle
inline with `catch { ... }` (where `err` is bound), propagate with
`catch { return err }`, or supply a fallback with `catch { default }`. No
auto-widening. No `?` operator. No `if err != nil` ladder.

### Classes, inheritance, and sealed types

```zinc
class Animal {
    String name
    init(String name) { this.name = name }
    String speak() { return "${name} speaks" }
}

class Dog : Animal {
    init(String name) { super(name) }
    String speak() { return "${name} says Woof" }
}

Animal a = Dog("Rex")
print(a.speak())    // Rex says Woof
```

Sealed types give you exhaustive pattern matching:

```zinc
sealed class Result<T> {
    data Ok(T value)
    data Err(String message)
}
```

Missing variants in a `match` are a compile error.

### `using` — deterministic resource cleanup

```zinc
using (var f = openFile("config.toml")) {
    var contents = f.readAll()
    return parse(contents)        // Close() runs before the caller observes the return
}
```

Multi-resource form closes in reverse order:

```zinc
using (var a = Resource("a"), var b = Resource("b")) {
    a.work()
    b.work()
}
```

This is what `defer` wishes it were — block-scoped, runs *before* a return
value escapes the block (so a `bytes.Buffer.Close()` flushes before the
caller reads the buffer).

### Implicit-self in methods

```zinc
class Counter {
    int value

    init() { value = 0 }                // bare ident, resolves to this.value

    int privateAdd(int n) { return value + n }

    pub int doublePrivate(int n) {
        return privateAdd(n) * 2        // bare method call, resolves to this.privateAdd
    }
}
```

No more `c.value, c.value, c.value` clutter — bare names resolve through
the receiver.

### Auto-pointerization for Go FFI

Zinc fields typed as a Go stdlib struct with pointer-receiver methods
auto-pointerize to `*T` in the generated Go, *and* the constructor
initializes them so they're usable on day one.

```zinc
import sync

class Counter {
    sync.Mutex mu          // becomes *sync.Mutex, init'd to &sync.Mutex{}
    int n

    pub void inc() {
        lock (mu) { n = n + 1 }
    }
}
```

The csharp/java equivalent needs a `Counter()` constructor body that
news up the mutex. Zinc threads it in for you.

When passing a Zinc value to a Go function that takes `*T`, the `&` is
inserted automatically. The only place you write `&` yourself is when
the Go signature is `any` but the runtime contract requires a pointer
(`json.Unmarshal(data, &p)`).

### Concurrency — Go's primitives, clean syntax

```zinc
spawn { doWork() }

var ch = Channel<String>(10)
ch.send("hello")
var msg = ch.recv()

parallel for (url in urls) {
    fetch(url)
}

select {
    case msg = ch.recv():
        print("got: ${msg}")
    case _:
        print("nothing ready")
}

timeout(2 * time.Second) {
    longRunning()
} catch {
    print("deadline exceeded")
}
```

### Generics

```zinc
class Box<T> {
    pub T value
    init(T v) { this.value = v }
    pub T get() { return value }
}

pub T first<T>(List<T> xs) = xs[0]

data Pair<A, B>(A first, B second)
```

Maps directly to Go generics — no monomorphization tax beyond what `go
build` already does.

### Lambdas

```zinc
var doubled = (int x) -> x * 2

apply(() -> {
    for (i in 0..3) { total = total + i }
})
```

### `print()`, string interpolation, ranges

```zinc
var name = "World"
print("Hello, ${name}!")
print("2 + 2 = ${2 + 2}")

for (i in 0..10)  { ... }       // exclusive
for (i in 0..=10) { ... }       // inclusive
```

## Quick start

Install:

```bash
curl -sL https://raw.githubusercontent.com/ZincScale/zinc/master/zinc-go/install.sh | bash
```

Or build from source:

```bash
git clone https://github.com/ZincScale/zinc.git
cd zinc/zinc-go
make build && sudo make install
```

Hello world:

```bash
echo 'print("Hello, World!")' > hello.zn
zinc-go run hello.zn
```

Project workflow:

```bash
zinc-go init myapp && cd myapp
zinc-go run                       # transpile + run
zinc-go test                      # run *_test.zn through go test
zinc-go build                     # native binary into zinc-out/
zinc-go build --cross linux/arm64
```

See [docs/getting-started.md](docs/getting-started.md) for the full
workflow.

## CLI

| Command | Description |
|---------|-------------|
| `zinc-go init <name>` | Create a new project |
| `zinc-go run [file\|dir] [-- args]` | Transpile and run |
| `zinc-go build [dir] [-o outdir]` | Build native binary |
| `zinc-go build --cross os/arch` | Cross-compile |
| `zinc-go test [dir] [-- go-test-args]` | Transpile `*_test.zn` and run `go test` |
| `zinc-go fmt <file\|dir>` | Format source code |
| `zinc-go add <pkg@version>` | Add a Go dependency |
| `zinc-go deps` | List dependencies |

Cross-compilation targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`,
`darwin/arm64`, `windows/amd64`, `windows/arm64`.

## Where to read more

- [Why Zinc](docs/why-zinc.md) — the rationale, in long form
- [Getting Started](docs/getting-started.md) — install, project layout, workflow
- [Language Tour](docs/language-tour.md) — every feature with runnable examples
- [Interop with Go](docs/interop-with-go.md) — calling Go from Zinc
- [Classes & Inheritance](docs/classes.md)
- [Error Handling](docs/error-handling.md)
- [Concurrency](docs/concurrency.md)

## Status

Zinc is at 1.0 maturity. The full e2e suite is green (126 examples), and
the language is in production use as the implementation language for
[zinc-flow](https://github.com/ZincScale/zinc-flow), a NiFi-class data
flow engine. The grammar surface is stabilized as `v2-2026-05-01`
(reported by `zinc-go version`); editor plugins and build tools can pin
on it.

## Architecture

```
zinc-go/
  cmd/zinc/           CLI (build, run, init, test, fmt, add, deps)
  internal/
    lexer/            Tokenizer
    parser/           AST builder
    typechecker/      Type inference & checking
    codegen_go/       Go code generator
    errs/             Colored error output
  examples/           positive e2e tests
  examples-fail/      negative tests (compile-time rejections)
  examples-test/      `test "..." { }` regression suites
  expected/           expected outputs for e2e
  stdlib/src/         errors, asserts, config, logging (written in Zinc)
  docs/               documentation
```

## License

[Apache License 2.0](../LICENSE)
