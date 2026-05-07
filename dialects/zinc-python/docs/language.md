# Language transforms

`zinc-python` does four mechanical transforms. Everything else — every keyword, every standard-library function, every semantic — is plain Python.

## 1. Braces → indentation

Blocks use `{ }` instead of significant whitespace. The transpiler turns them into Python's standard indentation.

```zinc
def greet(name) {
    if name {
        print("hello, {name}")
    } else {
        print("hello, stranger")
    }
}
```

→

```python
def greet(name):
    if name:
        print(f"hello, {name}")
    else:
        print("hello, stranger")
```

`if`, `elif`, `else`, `for`, `while`, `with`, `def`, `class`, `try`, `except`, `finally` all use the brace form. The colon at the end of the header is added for you.

## 2. Method-name dunders

In Zinc, classes use familiar method names. The transpiler rewrites them to Python's `__dunder__` form:

```zinc
class Point {
    def init(x, y) {
        self.x = x
        self.y = y
    }

    def toString() {
        return "Point({self.x}, {self.y})"
    }

    def equals(other) {
        return self.x == other.x and self.y == other.y
    }

    def hashCode() {
        return hash((self.x, self.y))
    }
}
```

→

```python
class Point:
    def __init__(self, x, y):
        self.x = x
        self.y = y

    def __repr__(self):
        return f"Point({self.x}, {self.y})"

    def __eq__(self, other):
        return self.x == other.x and self.y == other.y

    def __hash__(self):
        return hash((self.x, self.y))
```

See [dunders.md](dunders.md) for the full mapping.

You can still write `__add__`, `__len__`, etc. directly — the transpiler only rewrites the names in the table. Anything not in the table passes through unchanged.

## 3. Implicit `self`

Class methods get `self` injected as the first parameter:

```zinc
class Counter {
    def init(start=0) {
        self.count = start
    }

    def increment(n=1) {
        self.count += n
    }
}
```

You write the method as if `self` is already implicit; the transpiler adds it to the signature.

## 4. Auto f-strings

Any string literal containing `{expr}` becomes an f-string:

```zinc
name = "world"
print("Hello, {name}!")
print("2 + 2 = {2 + 2}")
```

→

```python
name = "world"
print(f"Hello, {name}!")
print(f"2 + 2 = {2 + 2}")
```

Strings without `{...}` stay as plain strings. To write a literal `{` in a string, double it: `"{{not interpolated}}"`.

## What's NOT transformed

Everything else is Python. In particular:

- All keywords (`def`, `class`, `if`, `for`, `while`, `try / except / finally`, `with`, `raise`, `return`, `yield`, `async`, `await`, `import`, `from ... import`).
- Operators (`and`, `or`, `not`, `in`, `is`).
- Comprehensions (`[x for x in xs if pred]`).
- Decorators (`@dataclass`, etc.).
- Type hints — write Python type hints exactly as you would in `.py`.
- All standard-library calls.
- All third-party packages.

## Error handling

Plain Python `try / except / finally / raise`:

```zinc
def divide(a, b) {
    if b == 0 {
        raise ValueError("division by zero")
    }
    return a / b
}

def main() {
    try {
        print(divide(10, 0))
    } except ValueError as e {
        print("caught: {e}")
    } finally {
        print("done")
    }
}
```

The braces are the only difference from idiomatic Python.

## Imports

```zinc
import json
from pathlib import Path
from models.user import User    // local module at src/models/user.zn
```

Same syntax as Python. Local imports use the directory layout under `src/`.
