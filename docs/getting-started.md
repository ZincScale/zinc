# Getting Started

## Installation

```bash
git clone https://github.com/victorybhg/zinc
cd zinc
go build -o zinc ./cmd/zinc/
```

Requires **Go 1.21+**.

## CLI Usage

```bash
zinc <file.zn>               # transpile to <file>.go
zinc <file.zn> -o out.go     # specify output file
zinc <file.zn> --run         # transpile and run immediately
zinc <file.zn> --watch       # watch for changes, re-transpile automatically
zinc <file.zn> --verbose     # show token/AST debug info
zinc repl                    # launch interactive REPL
```

## Running Examples

The [`examples/`](../examples/) directory contains working Zinc programs covering every major language feature:

| Example | Description |
|---------|-------------|
| [`hello.zn`](../examples/hello.zn) | Hello World + variables |
| [`classes.zn`](../examples/classes.zn) | Classes, interfaces, inheritance |
| [`concurrency.zn`](../examples/concurrency.zn) | Channels + goroutines |
| [`errors.zn`](../examples/errors.zn) | Errors as values, `or` handler |
| [`enums.zn`](../examples/enums.zn) | Enums + match |
| [`generics.zn`](../examples/generics.zn) | Generic functions and classes |
| [`fibonacci.zn`](../examples/fibonacci.zn) | Recursion |
| [`closures.zn`](../examples/closures.zn) | Lambdas, closures, failable lambdas |
| [`safe_navigation.zn`](../examples/safe_navigation.zn) | Safe navigation `?.` with chaining |
| [`with_resources.zn`](../examples/with_resources.zn) | Resource management with `with` |
| [`defaults_and_named_args.zn`](../examples/defaults_and_named_args.zn) | Default parameters + named arguments |
| [`type_casting.zn`](../examples/type_casting.zn) | Type assertions (`as`) and checks (`is`) |
| [`collections.zn`](../examples/collections.zn) | Typed literals, collection methods, iteration |
| [`callable_types.zn`](../examples/callable_types.zn) | `Fn<>` function types + higher-order functions |
| [`labeled_loops.zn`](../examples/labeled_loops.zn) | Labeled `break` and `continue` |
| [`tuple_unpacking.zn`](../examples/tuple_unpacking.zn) | Multi-return unpacking + error handling |
| [`constants.zn`](../examples/constants.zn) | `const` declarations |

Run any example:

```bash
./zinc examples/hello.zn --run
```
