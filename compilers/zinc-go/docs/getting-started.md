# Getting Started

This guide takes you from zero to a running Zinc project in about five
minutes.

## Install

One-line install (Linux / macOS):

```bash
curl -sL https://raw.githubusercontent.com/ZincScale/zinc/master/compilers/zinc-go/install.sh | bash
```

Or build from source (requires Go 1.26+):

```bash
git clone https://github.com/ZincScale/zinc.git
cd zinc/compilers/zinc-go
make build
sudo make install
```

The binary is installed as **`zinc-go`**. (The name avoids collision
with sibling targets like `zinc-python`; pick the binary matching the
backend you want.)

Verify:

```bash
zinc-go version
# zinc-go dev (parser-features: v2-2026-05-01)
```

## Hello, World

Single-file run — Zinc wraps top-level statements in `main()` for you:

```bash
echo 'print("Hello, World!")' > hello.zn
zinc-go run hello.zn
# Hello, World!
```

`zinc-go run` transpiles the `.zn` to Go in a temp dir, compiles, and
executes. Iteration speed is roughly equivalent to `go run`.

## Your first project

```bash
zinc-go init myapp
cd myapp
```

This scaffolds:

```
myapp/
  zinc.toml          # project config
  src/
    main.zn          # entry point
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

Cross-compile:

```bash
zinc-go build --cross linux/arm64
zinc-go build --cross darwin/arm64
zinc-go build --cross windows/amd64
```

The output binary lands in `zinc-out/`.

## Project layout

```
myapp/
  zinc.toml              # project config — name, deps, replaces
  src/
    main.zn              # entry point (the file declaring `void main()`)
    store/
      store.zn           # subpackage `store` — import as `import store`
    util/
      util.zn            # subpackage `util`
  tests/                 # sibling test directory (optional)
    main_test.zn
  zinc-out/              # build artifacts (gitignore this)
```

Subpackages are directories under `src/`. Their package name is the
directory name. To import a subpackage: `import store`.

## `zinc.toml`

```toml
[project]
name = "myapp"
version = "0.1.0"
main = "main.zn"

[go]
version = "1.26"

[deps]
mux = "github.com/gorilla/mux@v1.8.1"
viper = "github.com/spf13/viper@v1.20.1"

[replace]
# Local path override — used while developing alongside a dep.
# mux = "../gorilla-mux"
```

`[deps]` keys are the *aliases* you write in `import` statements. The
value is the Go module path with an `@version` suffix. Add deps from
the CLI:

```bash
zinc-go add github.com/gorilla/mux@v1.8.1
```

Inside Zinc:

```zinc
import mux

var r = mux.NewRouter()
```

`[replace]` keys match `[deps]` keys, so the two can't get out of
sync. Relative paths in `[replace]` resolve against the manifest
directory.

## Tests

Tests live in `*_test.zn` files. They can sit alongside `src/` files
or in a sibling `tests/` directory. Inside, write `test "name" { ... }`
blocks at the top level — `t` (a `*testing.T`) is implicitly in scope:

```zinc
// src/math_test.zn
import stdlib/asserts

test "addOne returns x + 1" {
    asserts.equalInt(t, addOne(41), 42)
}

test "addOne handles negatives" {
    asserts.equalInt(t, addOne(-10), -9)
}

test "raw t.Errorf works too" {
    if (addOne(0) != 1) {
        t.Errorf("addOne(0) != 1")
    }
}
```

Run with:

```bash
zinc-go test                     # all project tests
zinc-go test -- -v               # forward flags to go test
zinc-go test -- -run TestAddOne  # filter by name
zinc-go test -- -race            # race detector
```

`zinc-go test` transpiles prod + test code, then delegates to `go
test ./...` — so the full Go test toolchain (coverage, `-count`,
`-bench`, IDE integration) is yours.

`stdlib/asserts` provides `equalInt`, `equalString`, `isTrue`,
`isFalse`, `contains`, `fail`, `fatal`.

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
| `zinc-go version` | Print version + parser feature stamp |

Cross-compilation targets: `linux/amd64`, `linux/arm64`,
`darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`.

When `zinc-go build` or `zinc-go run` is invoked in a directory
containing `zinc.toml`, it operates in *project mode* — multi-file,
deps from the manifest. Otherwise it treats the argument as a single
`.zn` file.

## Editor integration

The parser feature stamp (`v2-2026-05-01`) is reported by `zinc-go
version` and is intended for editor plugins or build tooling to pin a
minimum compiler version. The stamp bumps whenever the syntactic
surface changes.

## Next steps

- [Language Tour](language-tour.md) — every feature with runnable examples
- [Why Zinc](why-zinc.md) — the rationale, in long form
- [Interop with Go](interop-with-go.md) — calling Go from Zinc
- [Classes & Inheritance](classes.md)
- [Error Handling](error-handling.md)
- [Concurrency](concurrency.md)
