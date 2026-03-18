# Error Handling

Zinc uses Python's exception model directly — no invented abstractions. The transpiler adds safety guardrails: enforced catch-or-propagate, typed exceptions, and exhaustiveness hints.

## Philosophy

Python exceptions are fine. The problem isn't the mechanism — it's that Python lets you silently ignore errors. Zinc fixes this:

1. **Failable calls must be handled** — the transpiler knows which functions raise and warns if you ignore them.
2. **Typed catch blocks** — catch specific exceptions, not bare `except:`.
3. **Clean syntax** — `end` blocks instead of indentation, no colon clutter.
4. **`or {}` shorthand** — for quick scripts where you don't need full try/catch.

## Try / Catch / Finally

```zinc
try
    var data = open("config.json").read()
    var config = json.loads(data)
catch err: FileNotFoundError
    print("Config not found, using defaults")
    var config = {}
catch err: json.JSONDecodeError
    print("Bad JSON: {err}")
    exit(1)
finally
    print("done")
end
```

Transpiles to:
```python
try:
    data = open("config.json").read()
    config = json.loads(data)
except FileNotFoundError as err:
    print("Config not found, using defaults")
    config = {}
except json.JSONDecodeError as err:
    print(f"Bad JSON: {err}")
    exit(1)
finally:
    print("done")
```

## Or Handlers (Quick Error Handling)

For scripts where you just want to handle failure and move on, `or {}` is a shorthand for try/catch:

```zinc
var content = open("data.txt").read() or {
    print("Can't read file: {err}")
    exit(1)
}
```

Transpiles to:
```python
try:
    content = open("data.txt").read()
except Exception as __err:
    err = str(__err)
    print(f"Can't read file: {err}")
    exit(1)
```

`or {}` catches all exceptions. If you need specificity, use `try/catch`.

### Or with default value

```zinc
var port = int(os.getenv("PORT")) or { 8080 }
```

Transpiles to:
```python
try:
    port = int(os.getenv("PORT"))
except Exception:
    port = 8080
```

The last expression in the `or` block becomes the fallback value.

## Raising Exceptions

```zinc
fn divide(a: int, b: int) -> int
    if b == 0
        raise ValueError("division by zero")
    end
    return a / b
end
```

Transpiles to:
```python
def divide(a: int, b: int) -> int:
    if b == 0:
        raise ValueError("division by zero")
    return a // b
```

## Custom Exceptions

```zinc
class AppError(Exception)
    fn init(message: str, code: int)
        super().init(message)
        this.code = code
    end

    fn str() -> str
        return "AppError({code}): {message}"
    end
end

// Usage
raise AppError("not found", 404)
```

Transpiles to:
```python
class AppError(Exception):
    def __init__(self, message: str, code: int):
        super().__init__(message)
        self.code = code

    def __str__(self) -> str:
        return f"AppError({self.code}): {self.message}"
```

## With (Context Managers)

```zinc
with open("data.txt") as f
    var content = f.read()
    print(content)
end

// Multiple context managers
with open("in.txt") as src, open("out.txt", "w") as dst
    dst.write(src.read())
end
```

Transpiles to:
```python
with open("data.txt") as f:
    content = f.read()
    print(content)

with open("in.txt") as src, open("out.txt", "w") as dst:
    dst.write(src.read())
```

## Transpiler Safety

The transpiler helps prevent common Python error handling mistakes:

| Footgun | Zinc prevention |
|---|---|
| Bare `except:` catching everything | Not allowed — must specify exception type, or use `or {}` |
| Silently swallowing errors | Warning if `catch` block is empty |
| Catching `BaseException` | Warning — you probably don't want to catch `KeyboardInterrupt` |
| Unhandled exceptions from known-failable calls | Warning if a call that raises isn't wrapped in try/catch or `or {}` |

## How It Maps

| Zinc | Python |
|---|---|
| `try ... end` | `try:` |
| `catch err: TypeError` | `except TypeError as err:` |
| `catch` (no type) | Not allowed — must specify type |
| `finally` | `finally:` |
| `or { }` | `try: ... except Exception:` |
| `raise` | `raise` |
| `with x as y ... end` | `with x as y:` |
| `err` in `or` block | `str(exception)` |
