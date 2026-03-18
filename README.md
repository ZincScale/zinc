<img alt="Zinc mascot" src="./zinc-badger.svg" height="180" /> <img alt="Zinc logo" src="./zinc-wordmark.svg" width="480" />


# Zinc

**Zinc** is typed Python with explicit blocks. Write `.zn` files, get type safety, clean syntax, and the full Python ecosystem. The transpiler catches mistakes, generates clean `.py` files, and stays out of your way.

```zinc
import sys

fn greet(name: str): str {
    return "Hello, {name}!"
}

var name = if len(sys.argv) > 1: sys.argv[1] else: "world"
print(greet(name))
```

```bash
$ zinc run hello.zn -- Alice
Hello, Alice!
```

---

## Why Zinc?

Python is the best language for getting things done fast. But it has pain points: whitespace bugs, optional types, `self` boilerplate, dunder ceremonies. Zinc fixes these while keeping Python's ecosystem.

- **Enforced types** — catch wrong types, bad args, missing returns at transpile time
- **Brace blocks `{ }`** — no whitespace ambiguity, familiar C-family syntax
- **Zero boilerplate** — no `self`, no dunders, no `f""` prefix
- **It's just Python** — full pip ecosystem, readable `.py` output
- **Two-track errors** — `Result[T]` for expected failures, exceptions for exceptional ones
- **Source maps** — Python errors show your `.zn` file and line numbers
- **Smart transpiler** — auto-injects `self`, maps dunder methods, optimizes collection chains

---

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](docs/getting-started.md) | Install, hello world, key concepts |
| [Language Reference](docs/language-reference.md) | Full syntax guide |
| [Design Doc](docs/design-zinc-v2-python.md) | Philosophy, decisions, rationale |
| [Known Limitations](docs/v2-limitations.md) | What's not yet implemented |

### Design

| Document | Description |
|----------|-------------|
| [Zinc Flow](docs/design-zinc-flow.md) | NiFi-inspired flow processing |

---

## Install

```bash
go install github.com/victorybhg/zinc/cmd/zinc@latest
```

Requires: Go 1.21+ and Python 3.13+

---

## Quick Start

```bash
# Write a script
echo 'print("Hello from Zinc!")' > hello.zn

# Run it
zinc run hello.zn

# Or transpile to Python
zinc transpile hello.zn    # → hello.py

# Optimize for Polars (large data pipelines)
zinc run pipeline.zn --optimize polars
```

No project setup, no config, no boilerplate. Single files just run.

---

## Feature Highlights

```zinc
// Data classes
data User {
    name: str
    email: str
    age: int = 0
}

// Classes with auto-self and dunder mapping
class Stack {
    var items: list[int] = []
    fn push(item: int) {
        items.append(item)     // → self.items.append(item)
    }
    fn len(): int {            // → __len__(self)
        return len(items)
    }
}

// Two-track error handling
fn parse_port(s: str): Result[int] {
    if not s.isdigit() {
        return Err("not a number")
    }
    return int(s)
}
var port = parse_port("8080") Err 80

// Collection methods → comprehensions
var active = orders.filter(o -> o.status == "active")
var total = sum([o.amount for o in active])

// Expression if
var label = if count == 1: "item" else: "items"

// Generators
fn fibonacci(limit: int) {
    var a = 0
    var b = 1
    while a < limit {
        yield a
        var temp = a
        a = b
        b = temp + b
    }
}
```

---

## Examples

See [`examples/v2/`](examples/v2/) for working examples:

- [`hello.zn`](examples/v2/hello.zn) — hello world with functions
- [`classes.zn`](examples/v2/classes.zn) — classes, inheritance, data classes, enums
- [`closures.zn`](examples/v2/closures.zn) — lambdas, closures, higher-order functions
- [`collections.zn`](examples/v2/collections.zn) — lists, dicts, comprehensions, method chains
- [`constants.zn`](examples/v2/constants.zn) — const declarations
- [`data_processing.zn`](examples/v2/data_processing.zn) — comprehensions, filtering
- [`defaults_and_named_args.zn`](examples/v2/defaults_and_named_args.zn) — default params, named args
- [`enums.zn`](examples/v2/enums.zn) — enums and match expressions
- [`error_handling.zn`](examples/v2/error_handling.zn) — Result[T], Err, try/catch
- [`generators.zn`](examples/v2/generators.zn) — yield, generator functions
- [`polars_pipeline.zn`](examples/v2/polars_pipeline.zn) — data pipeline with --optimize polars
- [`type_checking.zn`](examples/v2/type_checking.zn) — type annotations, `is` type checks, none
- [`variadic.zn`](examples/v2/variadic.zn) — *args and **kwargs

---

## License

[Apache License 2.0](LICENSE)
