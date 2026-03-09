# Getting Started

## Installation

```bash
git clone https://github.com/victorybhg/growler
cd growler
go build -o growler ./cmd/growler/
```

Requires **Go 1.21+**.

## CLI Usage

```bash
growler <file.gw>               # transpile to <file>.go
growler <file.gw> -o out.go     # specify output file
growler <file.gw> --run         # transpile and run immediately
growler <file.gw> --watch       # watch for changes, re-transpile automatically
growler <file.gw> --verbose     # show token/AST debug info
growler repl                    # launch interactive REPL
```

## Running Examples

The [`examples/`](../examples/) directory contains working Growler programs covering every major language feature:

| Example | Description |
|---------|-------------|
| [`hello.gw`](../examples/hello.gw) | Hello World + variables |
| [`classes.gw`](../examples/classes.gw) | Classes, interfaces, inheritance |
| [`concurrency.gw`](../examples/concurrency.gw) | Channels + goroutines |
| [`errors.gw`](../examples/errors.gw) | Errors as values, `or` handler |
| [`enums.gw`](../examples/enums.gw) | Enums + match |
| [`generics.gw`](../examples/generics.gw) | Generic functions and classes |
| [`fibonacci.gw`](../examples/fibonacci.gw) | Recursion |
| [`closures.gw`](../examples/closures.gw) | Lambdas, closures, failable lambdas |
| [`safe_navigation.gw`](../examples/safe_navigation.gw) | Safe navigation `?.` with chaining |
| [`with_resources.gw`](../examples/with_resources.gw) | Resource management with `with` |
| [`defaults_and_named_args.gw`](../examples/defaults_and_named_args.gw) | Default parameters + named arguments |
| [`type_casting.gw`](../examples/type_casting.gw) | Type assertions (`as`) and checks (`is`) |
| [`collections.gw`](../examples/collections.gw) | Typed literals, collection methods, iteration |
| [`callable_types.gw`](../examples/callable_types.gw) | `Fn<>` function types + higher-order functions |
| [`labeled_loops.gw`](../examples/labeled_loops.gw) | Labeled `break` and `continue` |
| [`tuple_unpacking.gw`](../examples/tuple_unpacking.gw) | Multi-return unpacking + error handling |
| [`constants.gw`](../examples/constants.gw) | `const` declarations |

Run any example:

```bash
./growler examples/hello.gw --run
```
