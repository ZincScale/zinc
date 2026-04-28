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
- **Errors as values, with auto-widening** — return any class extending `Err` and the function's Go signature widens to `(T, error)` automatically; handle with `or { ... }` at call sites
- **String interpolation** — `"Hello, ${name}!"` just works
- **Concurrency** — `spawn { }`, `Channel<T>`, `parallel for`, full `select { case ... }`
- **Sealed classes + match** — algebraic data types with exhaustive pattern matching
- **Generics** — type parameters on functions and classes, mapped to Go generics
- **Nullable types** — `String?` with safe navigation `?.`
- **Default & variadic params** — `void serve(int port = 8080)`, `int sum(int... xs)`
- **Type-first declarations** — `int add(int a, int b)` / `void main()` (Java/C#/Dart shape)
- **Clean output** — generated Go is readable and editable

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
zinc-go run hello.zn

# Create a project
zinc-go init myapp && cd myapp
zinc-go run

# Build a native binary
zinc-go build
./zinc-out/myapp

# Cross-compile
zinc-go build --cross linux/arm64
```

## Feature highlights

### Errors are values — no `if err != nil` boilerplate

```zinc
import stdlib/errors

int parseInt(String s) {
    if (s == "") {
        return errors.IllegalArgumentError("empty input")
    }
    return 42
}

void main() {
    var n = parseInt(input) or { print("bad input: ${err}"); return }
    use(n)
}
```

A function that returns an `Err`-extending class has its Go signature auto-widened to `(T, error)`. Callers either handle inline with `or { ... }` (where `err` is bound) or omit it — in which case the caller's signature widens too, propagating the error up.

### Sealed classes & pattern matching

```zinc
sealed class Shape {
    data Circle(double radius)
    data Rect(double width, double height)
}

double area(Shape s) {
    match (s) {
        case Circle(r) { return 3.14159 * r * r }
        case Rect(w, h) { return w * h }
    }
    return 0.0
}
```

Match is exhaustive on sealed types — missing variants are a compile error.

### Concurrency

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
```

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
| `zinc-go init <name>` | Create a new project |
| `zinc-go run [file\|dir] [-- args]` | Transpile and run |
| `zinc-go build [dir] [-o outdir]` | Build native binary |
| `zinc-go build --cross os/arch` | Cross-compile |
| `zinc-go test [dir] [-- go-test-args]` | Transpile `*_test.zn` and run `go test` |
| `zinc-go fmt <file\|dir>` | Format source code |
| `zinc-go add <pkg@version>` | Add a Go dependency |
| `zinc-go deps` | List dependencies |

Cross-compilation targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`.

## Project layout

```
myapp/
  zinc.toml              project config (name, deps, replaces)
  src/
    main.zn              entry point
    lib/                 subpackages
  tests/                 sibling test directory (or *_test.zn alongside src)
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
for (i in 0..10) { ... }        // exclusive
for (i in 0..=10) { ... }       // inclusive
while (cond) { ... }
match (x) { case 1 { ... } case _ { ... } }

// Expression if
var label = if x > 0: "positive" else: "non-positive"

// Type-first function declarations — no `fn` keyword
int add(int a, int b) { return a + b }
int doubled(int x) = x * 2          // single-expression form
void main() { ... }
String? find(String id) { ... }     // nullable return
int sum(int... xs) { ... }          // variadic

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
