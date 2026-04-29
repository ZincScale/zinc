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

The binary is installed as `zinc-go`.

## Your first program

Create `hello.zn`:

```zinc
print("Hello, World!")
```

Run it:

```bash
zinc-go run hello.zn
```

Zinc transpiles to Go and runs the result. No boilerplate needed — top-level code is wrapped in `main()` automatically.

## Create a project

```bash
zinc-go init myapp
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
zinc-go run
```

Build a native binary:

```bash
zinc-go build
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
replaces can't get out of sync. Relative paths in `[replace]` resolve against
the manifest directory.

Add dependencies from the CLI:

```bash
zinc-go add github.com/gorilla/mux@v1.8.1
```

## Cross-compilation

Build for any platform:

```bash
zinc-go build --cross linux/amd64
zinc-go build --cross darwin/arm64
zinc-go build --cross windows/amd64
```

## Tests

Tests live in `*_test.zn` files. They can sit alongside `src/` files or in a sibling `tests/` directory:

```
myapp/
  src/
    main.zn
    main_test.zn       # OR
  tests/
    main_test.zn
```

Write `test "name" { body }` blocks at the top level of any `*_test.zn` file. `t` is an implicit `*testing.T` in scope:

```zinc
import stdlib/asserts

test "addOne returns x + 1" {
    asserts.equalInt(t, addOne(41), 42)
}

test "raw t.Errorf works too" {
    if (addOne(-10) != -9) {
        t.Errorf("addOne(-10) != -9")
    }
}
```

Run with:

```bash
zinc-go test                     # runs project tests
zinc-go test -- -v               # forward flags to go test
zinc-go test -- -run TestAddOne  # filter by name
zinc-go test -- -race            # race detector
```

`zinc-go test` transpiles prod + test code, then delegates to `go test ./...`, so the full Go test toolchain (coverage, `-count`, `-bench`, IDE integration) is available. `stdlib/asserts` provides `equalInt`, `equalString`, `isTrue`, `isFalse`, `contains`, `fail`, `fatal`.

## CLI reference

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
| `zinc-go version` | Print version |

## Next steps

- [Language Guide](language-guide.md) — full syntax reference
- [Classes & Inheritance](classes.md) — OO features
- [Error Handling](error-handling.md) — `(T, error)` signatures, `or { }` at call sites
- [Concurrency](concurrency.md) — spawn, channels, parallel for, select
