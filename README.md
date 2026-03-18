<img alt="Zinc mascot" src="./zinc-badger.svg" height="180" /> <img alt="Zinc logo" src="./zinc-wordmark.svg" width="480" />


# Zinc

**Zinc** is typed Python with explicit blocks. Write `.zn` files, get type safety, clean syntax, and the full Python ecosystem. The transpiler catches mistakes, generates clean `.py` files, and stays out of your way.

```zinc
import sys

fn greet(name: str): str
    return "Hello, {name}!"
end

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

- **Enforced types** — catch `TypeError` at transpile time, not in production
- **`end` blocks** — no whitespace ambiguity, copy-paste safely
- **Zero boilerplate** — no `self`, no dunders, no `f""` prefix
- **It's just Python** — full pip ecosystem, readable `.py` output
- **Two-track errors** — `Result[T]` for expected failures, exceptions for exceptional ones
- **Smart transpiler** — auto-injects `self`, maps dunder methods, optimizes collection chains

---

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](docs/getting-started.md) | Install, hello world, key concepts |
| [Language Reference](docs/language-reference.md) | Full syntax guide |
| [Design Doc](docs/design-zinc-v2-python.md) | Philosophy, decisions, rationale |
| [Known Limitations](docs/v2-limitations.md) | What's not yet implemented |

### Explorations

| Document | Description |
|----------|-------------|
| [Dagster Pipelines](docs/exploration-dagster-pipelines.md) | Batch orchestration |
| [Pathway Streaming](docs/exploration-pathway-pipelines.md) | Real-time streaming |
| [PyFlink](docs/exploration-pyflink-pipelines.md) | Enterprise stream processing |

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
data User
    name: str
    email: str
    age: int = 0
end

// Classes with auto-self and dunder mapping
class Stack
    var items: list[int] = []
    fn push(item: int)
        items.append(item)     // → self.items.append(item)
    end
    fn len(): int              // → __len__(self)
        return len(items)
    end
end

// Two-track error handling
fn parse_port(s: str): Result[int]
    if not s.isdigit()
        return Err("not a number")
    end
    return int(s)
end
var port = parse_port("8080") Err { 80 }

// Collection methods → comprehensions
var active = orders.filter(o -> o.status == "active")
var total = sum([o.amount for o in active])

// Expression if
var label = if count == 1: "item" else: "items"

// Generators
fn fibonacci(limit: int)
    var a = 0
    var b = 1
    while a < limit
        yield a
        var temp = a
        a = b
        b = temp + b
    end
end
```

---

## Examples

See [`examples/v2/`](examples/v2/) for working examples:

- [`hello.zn`](examples/v2/hello.zn) — hello world with functions
- [`data_processing.zn`](examples/v2/data_processing.zn) — comprehensions, filtering
- [`classes.zn`](examples/v2/classes.zn) — classes, inheritance, data classes
- [`error_handling.zn`](examples/v2/error_handling.zn) — Result[T], Err {}, try/catch
- [`generators.zn`](examples/v2/generators.zn) — yield, generator functions

---

## License

[Apache License 2.0](LICENSE)
