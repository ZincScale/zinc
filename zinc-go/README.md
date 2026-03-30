# zinc-go — Zinc to Go transpiler

Zinc syntax → clean, readable Go output. OO-friendly language that compiles to native Go binaries.

## Install

```bash
curl -sL https://raw.githubusercontent.com/ZincScale/zinc/master/zinc-go/install.sh | bash
```

Or build from source:
```bash
cd zinc-go && make build && sudo make install
```

## Quick Start

```bash
# Create a project
zinc init myapp
cd myapp

# Run it
zinc run

# Build a native binary
zinc build
./zinc-out/myapp

# Cross-compile
zinc build --cross linux/arm64
```

## Language Features

```zinc
// Classes with inheritance
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

// Error handling — no if err != nil
fn divide(int a, int b): int {
    if b == 0 { return Error("division by zero") }
    return a / b
}
var result = divide(10, 0) or -1

// Streams with loop fusion
var total = numbers.filter(it > 5).map(it * 10).sum()

// Function types
type Handler = Fn<(String), String>
fn middleware(Handler next): Handler { ... }

// Nullable with safe navigation
fn find(String id): String? { ... }
var len = find("1")?.length()

// Concurrency
spawn { doWork() }
parallel for item in items { process(item) }
```

## CLI

| Command | Description |
|---------|-------------|
| `zinc init <name>` | Create a new Zinc project |
| `zinc build [dir] [-o outdir]` | Transpile and compile to native binary |
| `zinc build --cross os/arch` | Cross-compile (linux/amd64, darwin/arm64, etc.) |
| `zinc run [file\|dir] [-- args]` | Transpile and run |
| `zinc fmt <file\|dir>` | Format Zinc source code |
| `zinc add <module@version>` | Add a Go dependency |
| `zinc deps` | List dependencies |

## Project Structure

```
myapp/
  zinc.toml          — project config
  src/
    main.zn          — entry point
  zinc-out/          — build output (generated)
```

**zinc.toml:**
```toml
[project]
name = "myapp"
version = "0.1.0"
main = "main.zn"

[go]
version = "1.26"
deps = []
```

## E2E Tests

```bash
make test    # 18/18 passing
```

## Architecture

```
zinc-go/
  cmd/zinc/           — CLI (build, run, init, fmt, add, deps)
  internal/
    lexer/            — tokenizer
    parser/           — AST builder (v2 syntax: braces, fn, class)
    typechecker/      — type inference
    codegen_go/       — Go code generator with loop fusion
    errs/             — colored error output
  examples/           — 18 e2e test files
  expected/           — expected outputs
  install.sh          — one-command installer
  Makefile            — build, test, cross, release
  .goreleaser.yml     — GitHub release automation
```
