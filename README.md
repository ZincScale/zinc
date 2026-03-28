# Zinc — Braces Python

Python with `{}` instead of whitespace and sane method names.

```zinc
class Greeter {
    def init(name) {
        self.name = name
    }

    def toString() {
        return "Hello, {self.name}!"
    }
}

def main() {
    g = Greeter("world")
    print(g)
}

if __name__ == "__main__" {
    main()
}
```

Transpiles to clean, editable Python:

```python
class Greeter:
    def __init__(self, name):
        self.name = name

    def __repr__(self):
        return f"Hello, {self.name}!"

def main():
    g = Greeter("world")
    print(g)

if __name__ == "__main__":
    main()
```

## What Zinc does

Four transforms, everything else is Python:

1. **Braces → indentation** — `{}` blocks become Python indentation
2. **Method renames** — `init` → `__init__`, `toString` → `__repr__`, `equals` → `__eq__`, etc.
3. **Implicit self** — class methods get `self` injected automatically
4. **Auto f-strings** — strings with `{expr}` become f-strings

## Install

```bash
git clone https://github.com/ZincScale/zinc.git
export PATH="$PWD/zinc:$PATH"
```

Requires Python 3.12+.

## Usage

```bash
zinc run hello.zn              # transpile and run
zinc run src/                  # run a multi-file project
zinc build src/ -o build/      # transpile to .py files
zinc build src/ --native       # native binary via Nuitka
zinc init myapp                # scaffold a new project
```

## Method name mappings

| Zinc | Python |
|------|--------|
| `init` | `__init__` |
| `toString` | `__repr__` |
| `toStr` | `__str__` |
| `equals` | `__eq__` |
| `notEquals` | `__ne__` |
| `hashCode` | `__hash__` |
| `getItem` | `__getitem__` |
| `setItem` | `__setitem__` |
| `delItem` | `__delitem__` |
| `enter` | `__enter__` |
| `exit` | `__exit__` |

Everything else passes through unchanged. Use `__add__`, `__len__`, etc. directly when needed.

## Project structure

```
myapp/
  zinc.toml        # project config
  src/
    main.zn        # entry point
    models/
      user.zn      # imports work: from models.user import User
    services/
      api.zn
```

## License

Apache 2.0
