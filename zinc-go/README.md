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

    pub fn address(): String {
        return "{host}:{port}"
    }
}

var s = Server("localhost")
print(s.address())    // localhost:8080
```

## Why Zinc?

Go is fast, simple, and compiles everywhere. But its syntax has rough edges — verbose error handling, no classes, no generics sugar, no string interpolation. Zinc fixes these while keeping everything that makes Go great:

- **Classes & inheritance** — familiar OO syntax compiled to Go structs with embedding
- **Error handling** — `or` expressions replace `if err != nil` boilerplate
- **String interpolation** — `"Hello, ${name}!"` just works
- **Streams** — `list.filter(it > 5).map(it * 2).sum()` with loop fusion
- **Concurrency** — `spawn {}`, `Channel`, `parallel for`
- **Nullable types** — `String?` with safe navigation `?.`
- **Default params** — `fn serve(int port = 8080)`
- **Enums & sealed classes** — proper algebraic data types
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

### Error handling without boilerplate

```zinc
fn divide(int a, int b): int {
    if b == 0 { return Error("division by zero") }
    return a / b
}

var result = divide(10, 0) or -1           // fallback value
divide(10, 0) or { return Error(err) }     // propagate
divide(10, 0) or { continue }             // skip in loops
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

var ch = Channel(10)
ch.send("hello")
var msg = ch.recv()

parallel for url in urls {
    fetch(url)
}
```

### Classes & inheritance

```zinc
class Animal {
    String name
    init(String name) { this.name = name }
    fn speak(): String { return "${name} speaks" }
}

class Dog : Animal {
    init(String name) { super(name) }
    fn speak(): String { return "${name} says Woof" }
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
| `zinc fmt <file\|dir>` | Format source code |
| `zinc add <pkg@version>` | Add a Go dependency |
| `zinc deps` | List dependencies |

Cross-compilation targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`.

## Documentation

- [Getting Started](docs/getting-started.md)
- [Language Guide](docs/language-guide.md)
- [Classes & Inheritance](docs/classes.md)
- [Error Handling](docs/error-handling.md)
- [Concurrency](docs/concurrency.md)

## Architecture

```
zinc-go/
  cmd/zinc/           CLI (build, run, init, fmt, add, deps)
  internal/
    lexer/            Tokenizer
    parser/           AST builder
    typechecker/      Type inference & checking
    codegen_go/       Go code generator with loop fusion
    errs/             Colored error output
  examples/           24 example programs
  expected/           Expected test outputs
  docs/               Documentation
```

## License

[Apache License 2.0](../LICENSE)
