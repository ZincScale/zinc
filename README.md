# Zinc — Braces Python

Python with `{}` instead of whitespace and sane method names. Transpiles to clean, editable Python 3.14t (free-threading).

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
```

Output — standard Python, no lock-in:

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

## Install

```bash
curl -LsSf https://raw.githubusercontent.com/ZincScale/zinc/master/install.sh | bash
```

This installs [uv](https://github.com/astral-sh/uv), Python 3.14t (free-threading), and the zinc compiler. Everything goes into `~/.zinc/`.

Or manually:

```bash
git clone https://github.com/ZincScale/zinc.git
export PATH="$PWD/zinc/compiler:$PATH"
```

## What Zinc does

Four transforms, everything else is Python:

1. **Braces → indentation** — `{}` blocks become Python indentation
2. **Method renames** — `init` → `__init__`, `toString` → `__repr__`, `equals` → `__eq__`, etc.
3. **Implicit self** — class methods get `self` injected automatically
4. **Auto f-strings** — strings with `{expr}` become f-strings

## Usage

```bash
zinc run hello.zn              # transpile and run
zinc run src/                  # run a multi-file project
zinc build src/ -o build/      # transpile to .py files
zinc build src/ --native       # native binary via PyInstaller
zinc init myapp                # scaffold a new project
```

## Dependencies

Add deps in `zinc.toml` — zinc uses [uv](https://github.com/astral-sh/uv) to manage them automatically:

```toml
[project]
name = "myapp"
version = "0.1.0"
main = "main.zn"

[python]
version = ">=3.14"
deps = ["httpx", "rich"]
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
  zinc.toml        # project config (deps, entry point)
  src/
    main.zn        # entry point
    models/
      user.zn      # imports work: from models.user import User
    services/
      api.zn
```

## License

[Apache License 2.0](LICENSE)
