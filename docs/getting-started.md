# Getting Started with Zinc

Zinc is typed Python with explicit blocks. Write `.zn` files, run them with `zinc run`. The transpiler catches type errors, generates clean `.py` files, and stays out of your way.

## Install

```bash
go install github.com/victorybhg/zinc/cmd/zinc@latest
```

Requires: Go 1.21+ and Python 3.13+

## Hello World

```zinc
// hello.zn
print("Hello, world!")
```

```bash
zinc run hello.zn
# Hello, world!
```

That's it. No main function, no project setup. Top-level code just runs.

## Your First Script

```zinc
// greet.zn
import sys

fn greet(name: str): str {
    return "Hello, {name}!"
}

var name = if len(sys.argv) > 1: sys.argv[1] else: "world"
print(greet(name))
```

```bash
zinc run greet.zn -- Alice
# Hello, Alice!
```

## Key Differences from Python

| Python | Zinc | Why |
|---|---|---|
| `def greet(name: str) -> str:` | `fn greet(name: str): str` | Shorter, colon for return type |
| Indentation-based blocks | `{ }` braces close blocks | No whitespace bugs |
| `f"Hello, {name}"` | `"Hello, {name}"` | All double-quoted strings interpolate |
| Types optional | Types enforced | Catch errors at transpile time |
| `self.name` everywhere | Just `name` in methods | Auto-injected by transpiler |
| `__init__`, `__str__` | `fn init()`, `fn str()` | Clean names, transpiler maps to dunders |

## Variables

```zinc
var name = "Alice"          // type inferred
var age: int = 30           // explicit type
var scores: list[int] = []  // generic type
```

## Functions

```zinc
fn add(a: int, b: int): int {
    return a + b
}

// Single-expression shorthand
fn double(x: int): int = x * 2
```

## Control Flow

```zinc
if x > 0 {
    print("positive")
} else if x == 0 {
    print("zero")
} else {
    print("negative")
}

for item in items {
    print(item)
}

while running {
    process()
}
```

## Data Classes

```zinc
data User {
    name: str
    email: str
    age: int = 0
}

var u = User("Alice", "alice@example.com")
```

## Error Handling — Two Tracks

**Expected errors** (validation, parsing) use `Result[T]`:

```zinc
fn parse_port(s: str): Result[int] {
    if not s.isdigit() {
        return Err("not a number")
    }
    return int(s)
}

var port = parse_port("8080") Err 80
```

**Unexpected errors** (network down, disk full) use exceptions:

```zinc
try {
    var conn = db.connect(url)
} catch err: ConnectionError {
    print("Failed: {err}")
    exit(1)
}
```

## Imports

Use Python's ecosystem directly:

```zinc
import json
import os
from pathlib import Path
from requests import get as http_get
```

## Type Safety

Type errors are caught automatically during transpilation:

```bash
$ zinc run broken.zn
error: type errors in broken.zn:
  line 2: return type mismatch: expected int, got str
  argument 1 of "greet": expected str, got int
```

No separate `check` command — checking IS transpilation.

## Shebang

Make .zn files directly executable:

```zinc
#!/usr/bin/env zinc run
print("Hello from zinc!")
```

```bash
chmod +x script.zn
./script.zn
```

## CLI

```bash
zinc run script.zn                    # transpile + run
zinc run script.zn -- arg1            # pass args to script
zinc run script.zn --optimize polars  # use Polars for collection chains
zinc transpile script.zn              # output .py file
zinc transpile script.zn -o out.py    # specify output path
zinc fmt script.zn                    # format source code
zinc repl                             # interactive REPL
```

## Next Steps

- [Language Reference](language-reference.md) — full syntax guide
- [Design Doc](design-zinc-v2-python.md) — philosophy and decisions
- [Examples](../examples/v2/) — working examples
