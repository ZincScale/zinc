# Getting Started

`zinc-python` is a thin transpiler from `.zn` (braces Python) to standard Python 3.14t. It does four mechanical transforms — braces → indentation, method-name dunders, implicit `self`, auto f-strings — and leaves everything else untouched. The output is plain Python you can read, edit, and ship without lock-in.

## Install

```bash
curl -LsSf https://raw.githubusercontent.com/ZincScale/zinc/master/zinc-python/install.sh | bash
```

The installer puts everything under `~/.zinc-python/`:

- [uv](https://github.com/astral-sh/uv) — Python toolchain manager
- Python 3.14t (free-threading)
- The `zinc-python` CLI

## Hello world

Create `hello.zn`:

```zinc
def main() {
    name = "world"
    print("Hello, {name}!")
}
```

Run it:

```bash
zinc-python run hello.zn
```

`zinc-python` transpiles to Python in a temp dir, then runs it. Output:

```python
def main():
    name = "world"
    print(f"Hello, {name}!")

if __name__ == "__main__":
    main()
```

## Commands

```bash
zinc-python run <file|dir>        # transpile and run
zinc-python build <input> -o out  # transpile to .py files
zinc-python build <input> --native  # build a single binary via PyInstaller
zinc-python init <name>           # scaffold a new project
zinc-python from-py <input>       # reverse: transpile .py → .zn
```

## Project layout

```bash
zinc-python init myapp
cd myapp
zinc-python run
```

Scaffold:

```
myapp/
  zinc.toml        # project config
  src/
    main.zn        # entry point
```

`zinc-python run` (no args) runs the project at `./` if a `zinc.toml` is present.

## `zinc.toml`

```toml
[project]
name = "myapp"
version = "0.1.0"
main = "main.zn"

[python]
version = ">=3.14"
deps = ["httpx", "rich"]
```

Dependencies in `[python].deps` are managed by `uv` automatically — no manual `pip install`.

## Multi-file projects

Imports work the way Python's do. A file at `src/models/user.zn` is imported as:

```zinc
from models.user import User
```

The directory layout maps directly to the import path — no path rewriting.

## Reverse direction: `from-py`

If you have an existing Python codebase, `from-py` transpiles it to Zinc:

```bash
zinc-python from-py existing.py            # writes to zn/existing.zn
zinc-python from-py mypkg/ -o zn-src/      # whole package
```

This is useful for migrating an existing project, or for round-tripping to validate that `zinc-python` preserves semantics on real code.

## Further reading

- [Language transforms](language.md) — what zinc-python actually changes (and what it doesn't)
- [Method name mappings](dunders.md) — full table of `init` → `__init__`, `toString` → `__repr__`, etc.
