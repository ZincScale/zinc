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

[deps]
mux = "github.com/gorilla/mux@v1.8.1"

[replace]
# local path override — optional, used when developing alongside the dep
# mux = "../gorilla-mux"
```

`[deps]` keys are the local aliases you write in Zinc (`import mux`). The value
is `module/path@version`; omit `@version` when a `[replace]` points at a local
directory. Keys in `[replace]` match the same aliases as `[deps]`, so deps and
replaces can't get out of sync.

Add dependencies from the CLI:

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

## Tests

Write `*_test.zn` files with `test "name" { body }` blocks at the top level:

```zinc
import stdlib.asserts

test "addOne returns x + 1" {
    asserts.equalInt(t, addOne(41), 42)
}
```

Run with:

```bash
zinc test .
zinc test . -v                      # verbose (show each test)
zinc test . -run TestAddOne         # filter by name
zinc test . -race                   # race detector
```

`zinc test` transpiles prod + test code, then delegates to `go test ./...`, so
the full Go test toolchain (coverage, `-count`, `-bench`, IDE integration) is
available. `t` is an implicit `*testing.T` in scope inside each test block;
`stdlib.asserts` provides `equalInt`, `equalString`, `isTrue`, `isFalse`,
`contains`, `fail`, `fatal`.

## CLI reference

| Command | Description |
|---------|-------------|
| `zinc init <name>` | Create a new project |
| `zinc run [file\|dir]` | Transpile and run |
| `zinc build [dir]` | Build native binary |
| `zinc build --cross os/arch` | Cross-compile |
| `zinc test [dir] [-- go-test-args]` | Transpile `*_test.zn` and run `go test` |
| `zinc fmt <file\|dir>` | Format source code |
| `zinc add <pkg@version>` | Add a Go dependency |
| `zinc deps` | List dependencies |

## Next steps

- [Language Guide](language-guide.md) — full syntax reference
- [Error Handling](error-handling.md) — `or` expressions and error propagation
- [Concurrency](concurrency.md) — spawn, channels, parallel for
- [Classes & Inheritance](classes.md) — OO features
