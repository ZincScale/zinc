# Getting Started

## Install

One-line install (Linux / macOS):

```bash
curl -sL https://raw.githubusercontent.com/ZincScale/zinc/master/zinc-go/install.sh | bash
```

Or build from source:

```bash
git clone https://github.com/ZincScale/zinc.git
cd zinc/zinc-go
make build
sudo make install
```

## Your first program

Create `hello.zn`:

```zinc
print("Hello, World!")
```

Run it:

```bash
zinc run hello.zn
```

Zinc transpiles to Go and runs the result. No boilerplate needed — top-level code is wrapped in `main()` automatically.

## Create a project

```bash
zinc init myapp
cd myapp
```

This scaffolds:

```
myapp/
  zinc.toml        # project config
  src/
    main.zn        # entry point
```

Run the project:

```bash
zinc run
```

Build a native binary:

```bash
zinc build
./zinc-out/myapp
```

## Project config

`zinc.toml` defines your project:

```toml
[project]
name = "myapp"
version = "0.1.0"
main = "main.zn"

[go]
version = "1.26"
deps = []
```

Add dependencies with:

```bash
zinc add github.com/gorilla/mux@v1.8.1
```

## Cross-compilation

Build for any platform:

```bash
zinc build --cross linux/amd64
zinc build --cross darwin/arm64
zinc build --cross windows/amd64
```

## CLI reference

| Command | Description |
|---------|-------------|
| `zinc init <name>` | Create a new project |
| `zinc run [file\|dir]` | Transpile and run |
| `zinc build [dir]` | Build native binary |
| `zinc build --cross os/arch` | Cross-compile |
| `zinc fmt <file\|dir>` | Format source code |
| `zinc add <pkg@version>` | Add a Go dependency |
| `zinc deps` | List dependencies |

## Next steps

- [Language Guide](language-guide.md) — full syntax reference
- [Error Handling](error-handling.md) — `or` expressions and error propagation
- [Concurrency](concurrency.md) — spawn, channels, parallel for
- [Classes & Inheritance](classes.md) — OO features
