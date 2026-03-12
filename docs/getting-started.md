# Getting Started

## Installation

```bash
git clone https://github.com/victorybhg/zinc
cd zinc
go build -o zinc ./cmd/zinc/
```

Requires **Go 1.26+**. After building, move the `zinc` binary somewhere on your `$PATH` (e.g. `/usr/local/bin/`) so you can use it from any directory.

## Quick Start — Single File

For quick scripts or experimentation, just write a `.zn` file and run it:

```bash
zinc hello.zn --run
```

This transpiles `hello.zn` to `hello.go` and immediately runs it.

## Bootstrapping a Project

For anything beyond a single file, use `zinc init` to scaffold a project:

```bash
mkdir myapp && cd myapp
zinc init myapp
```

This creates two files:

```
myapp/
  go.mod      # Go module (module myapp, go 1.26)
  main.zn     # Entry point
```

The generated `main.zn` contains:

```
main() {
    print("Hello from Zinc!")
}
```

Run it immediately:

```bash
zinc run
```

Or compile to a binary:

```bash
zinc build
./myapp
```

### Adding Packages

Zinc supports multi-file projects with packages, just like Go. Create subdirectories for each package and declare the package at the top of each `.zn` file:

```
myapp/
  go.mod
  main.zn
  models/
    user.zn
  utils/
    math.zn
```

**`utils/math.zn`** — helper functions in a subpackage:

```
package "myapp/utils"

pub add(a Int, b Int) Int {
    return a + b
}
```

**`models/user.zn`** — a class in another subpackage:

```
package "myapp/models"

User {
    name String
    age Int

    new(name String, age Int) {
        this.name = name
        this.age = age
    }

    pub greet() String {
        return "Hi, I'm {this.name}!"
    }
}
```

**`main.zn`** — import and use your packages:

```
import "myapp/utils"
import "myapp/models"

main() {
    var sum Int = utils.Add(2, 3)
    print(sum)

    user := models.NewUser("Alice", 30)
    print(user.Greet())
}
```

Then build and run:

```bash
zinc run
```

`zinc build` and `zinc run` automatically find and transpile all `.zn` files across all subdirectories, resolve cross-file types (constructors, enums, interfaces, default parameters), and invoke the Go toolchain.

### Project Workflow Summary

| Command | What it does |
|---------|-------------|
| `zinc init [name]` | Scaffold a new project (creates `go.mod` + `main.zn`) |
| `zinc run [dir]` | Transpile all `.zn` files and run the project |
| `zinc build [dir]` | Transpile all `.zn` files and compile to a binary |
| `zinc <file.zn> --run` | Transpile and run a single file |
| `zinc <file.zn> --watch` | Watch a file for changes and re-transpile on save |
| `zinc repl` | Launch the interactive REPL |
| `zinc --version` | Print version |

## Watch Mode

For rapid iteration on a single file, use `--watch` to automatically re-transpile on every save:

```bash
zinc hello.zn --watch
```

Output:
```
Watching hello.zn for changes (Ctrl+C to stop)...
[14:32:05] transpiled hello.zn → hello.go
[14:32:11] transpiled hello.zn → hello.go
```

The watcher polls the file every 300ms for modification time changes. On each save it runs the full pipeline (lex → parse → typecheck → codegen → gofmt). Errors are printed with timestamps but don't stop the watcher — fix the error, save again, and it picks up the change.

You can combine it with `-o` to control the output path:

```bash
zinc hello.zn --watch -o build/hello.go
```

**Note:** Watch mode currently works with single files only. For multi-file projects, use `zinc run` or `zinc build` after each change. Project-wide watch mode is a planned enhancement.

## CLI Reference

```bash
zinc <file.zn>               # transpile to <file>.go
zinc <file.zn> -o out.go     # specify output file
zinc <file.zn> --run         # transpile and run immediately
zinc <file.zn> --watch       # watch for changes, re-transpile automatically
zinc <file.zn> --verbose     # show token/AST debug info
zinc init [name]             # initialize a new project (creates go.mod + main.zn)
zinc build [dir]             # transpile all .zn files and compile with go build
zinc run [dir]               # transpile all .zn files and run
zinc repl                    # launch interactive REPL
zinc --version               # print version
```

### Flag Shortcuts

Every flag has a short alias:

| Long | Short | Description |
|------|-------|-------------|
| `--run` | `-r` | Transpile and run |
| `--watch` | `-w` | Watch mode |
| `--verbose` | `-v` | Debug output (token count, declaration count) |
| `--version` | `-V` | Print version |
| `-o <file>` | | Output file path |

### Source Maps

Zinc emits `//line` directives in the generated Go code. This means if the Go compiler reports an error, the file and line number will point back to your `.zn` source — not the generated `.go` file. You debug in Zinc, not in Go.

### Error Output

Errors are printed with color formatting (ANSI colors) when running in a terminal. Colors are automatically disabled when output is piped or in CI environments. Errors include the source file name and line number:

```
error: hello.zn:12 — undefined variable 'x'
```

### Prerequisites

Zinc requires **Go 1.26+** installed on your system. The Go toolchain provides `gofmt` (used to format generated code) and `go build`/`go run` (used by `zinc build` and `zinc run`).

## Running Examples

The [`examples/`](../examples/) directory contains working Zinc programs covering every major language feature:

| Example | Description |
|---------|-------------|
| [`hello.zn`](../examples/hello.zn) | Hello World + variables |
| [`classes.zn`](../examples/classes.zn) | Classes, interfaces, inheritance, polymorphism |
| [`concurrency.zn`](../examples/concurrency.zn) | Channels + goroutines |
| [`errors.zn`](../examples/errors.zn) | Errors as values, `or` handler |
| [`enums.zn`](../examples/enums.zn) | Enums + match |
| [`generics.zn`](../examples/generics.zn) | Generic functions, classes, type inference, polymorphism |
| [`fibonacci.zn`](../examples/fibonacci.zn) | Recursion |
| [`closures.zn`](../examples/closures.zn) | Lambdas, closures, failable lambdas |
| [`safe_navigation.zn`](../examples/safe_navigation.zn) | Safe navigation `?.` with chaining |
| [`with_resources.zn`](../examples/with_resources.zn) | Resource management with `with` |
| [`defaults_and_named_args.zn`](../examples/defaults_and_named_args.zn) | Default parameters + named arguments |
| [`type_casting.zn`](../examples/type_casting.zn) | Type assertions (`as`) and checks (`is`) |
| [`collections.zn`](../examples/collections.zn) | Typed literals, collection methods, slicing, iteration |
| [`callable_types.zn`](../examples/callable_types.zn) | `Fn<>` function types + higher-order functions |
| [`labeled_loops.zn`](../examples/labeled_loops.zn) | Labeled `break` and `continue` |
| [`tuple_unpacking.zn`](../examples/tuple_unpacking.zn) | Multi-return unpacking + error handling |
| [`constants.zn`](../examples/constants.zn) | `const` declarations |
| [`variadic.zn`](../examples/variadic.zn) | Variadic functions, spread operator |

There's also a full [multi-file project example](../examples/myapp/) showing packages, classes with inheritance, and cross-file imports.

Run any example:

```bash
./zinc examples/hello.zn --run
```
