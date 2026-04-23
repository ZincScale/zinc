# <img src="docs/logo.svg" alt="Zinc" width="360">

[![Build](https://github.com/ZincScale/zinc/actions/workflows/ci.yml/badge.svg)](https://github.com/ZincScale/zinc/actions)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](../LICENSE)

**Zinc** removes Go's syntax warts. Write clean, expressive code — get readable Go output and native binaries.

```zinc
class Server {
    pub String host
    pub int port

    init(String host, int port = 8080) {
        this.host = host
        this.port = port
    }

    pub String address() {
        return "${host}:${port}"
    }
}

var s = Server("localhost")
print(s.address())    // localhost:8080
```

## Why Zinc?

Go is fast, simple, and compiles everywhere. But its syntax has rough edges — verbose error handling, no classes, no generics sugar, no string interpolation. Zinc fixes these while keeping everything that makes Go great:

- **Classes & inheritance** — familiar OO syntax compiled to Go structs with embedding
- **Exceptions** — `try/catch/throw` with unchecked exceptions and auto-error-propagation
- **Resource management** — `using (var r = ...) { }` wraps `defer r.Close()` cleanly
- **String interpolation** — `"Hello, ${name}!"` just works
- **Streams** — `list.filter(it > 5).map(it * 2).sum()` with loop fusion
- **Concurrency** — `spawn { }`, `Channel`, `parallel for`; uncaught throws inside goroutines panic the process (no silent failures)
- **Nullable types** — `String?` with safe navigation `?.`
- **Default params** — `void serve(int port = 8080)`
- **Enums & sealed classes** — proper algebraic data types
- **Type-first declarations** — `int add(int a, int b)` / `void main()` (Java/C#/Dart shape)
- **Clean output** — generated Go is readable and editable, with `//line` source maps

## Install

```bash
curl -sL https://raw.githubusercontent.com/ZincScale/zinc/master/zinc-go/install.sh | bash
```

Or build from source:

```bash
git clone https://github.com/ZincScale/zinc.git
cd zinc/zinc-go
make build && sudo make install
```

## Quick start

```bash
# Hello world
echo 'print("Hello, World!")' > hello.zn
zinc run hello.zn

# Create a project
zinc init myapp && cd myapp
zinc run

# Build a native binary
zinc build
./zinc-out/myapp

# Cross-compile
zinc build --cross linux/arm64
```

## Feature highlights

### Exceptions — no `if err != nil` boilerplate

```zinc
import stdlib.exceptions

int parseInt(String s) {
    if (s == "") {
        throw exceptions.IllegalArgumentException("empty input")
    }
    return 42
}

try {
    var n = parseInt(input)
    use(n)
} catch (exceptions.IllegalArgumentException e) {
    print("bad input: ${e.message}")
} catch (e) {
    print("unexpected: ${e.Error()}")
}
```

A function that `throw`s (directly or transitively) gets its Go signature widened to `(T, error)`. At call sites, the compiler emits `if err != nil { return err }` automatically. Typed catches dispatch via `errors.As` — so subclass throws match superclass catches and wrapped errors compose with Go's `errors.Is` / `%w`.

### Resources that clean up automatically

```zinc
using (var file = os.Open(path)) {
    process(file)
}  // file.Close() runs on exit, including via throw
```

### Streams with loop fusion

```zinc
var total = numbers
    .filter(it > 5)
    .map(it * 10)
    .sum()
// Compiled to a single loop — no intermediate allocations
```

### Concurrency

```zinc
spawn { doWork() }

var ch = Channel<String>(10)
ch.send("hello")
var msg = ch.recv()

parallel for (url in urls) {
    fetch(url)
}
```

Uncaught throws inside `spawn { }` panic the process with a stack trace — goroutines can't return errors to their launcher, and silent failure is never the default.

### Classes & inheritance

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

## CLI

| Command | Description |
|---------|-------------|
| `zinc init <name>` | Create a new project |
| `zinc run [file\|dir] [-- args]` | Transpile and run |
| `zinc build [dir] [-o outdir]` | Build native binary |
| `zinc build --cross os/arch` | Cross-compile |
| `zinc test [dir] [-- go-test-args]` | Transpile `*_test.zn` and run `go test` |
| `zinc fmt <file\|dir>` | Format source code |
| `zinc add <pkg@version>` | Add a Go dependency |
| `zinc deps` | List dependencies |

Cross-compilation targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`.

## Project layout

```
myapp/
  zinc.toml              project config (name, deps, replaces)
  src/
    main.zn              entry point
    lib/                 subpackages
  tests/                 sibling test directory
    main_test.zn
```

## Syntax essentials

```zinc
// Variable forms — var infers, drop var when you write the type
var name = "Alice"              // inferred
String greeting = "Hello"       // explicit + init
String host                     // explicit, no init
const PI = 3.14159              // constants

// Control flow requires parens on the header
if (x > 0) { ... } else if (x == 0) { ... } else { ... }
for (i in 0..10) { ... }
while (cond) { ... }
match (x) { case 1 { ... } case _ { ... } }

// Type-first function declarations — no `fn` keyword
int add(int a, int b) { return a + b }
void main() { ... }
String? find(String id) { ... }   // nullable return

// Class fields — declared as Type name, default via =
class Counter {
    pub int value = 0
    pub void inc() { value = value + 1 }
}
```

## Documentation

- [Getting Started](docs/getting-started.md)
- [Language Guide](docs/language-guide.md)
- [Classes & Inheritance](docs/classes.md)
- [Error Handling](docs/error-handling.md)
- [Concurrency](docs/concurrency.md)

## Architecture

```
zinc-go/
  cmd/zinc/           CLI (build, run, init, test, fmt, add, deps)
  internal/
    lexer/            Tokenizer
    parser/           AST builder
    typechecker/      Type inference & checking
    codegen_go/       Go code generator with loop fusion
    errs/             Colored error output
  examples/           57 example programs
  expected/           Expected test outputs
  examples-fail/      Negative tests (syntax errors, compile-time rejections)
  docs/               Documentation
  stdlib/             (lives at ../stdlib) — asserts, exceptions, config, logging
```

## License

[Apache License 2.0](../LICENSE)
