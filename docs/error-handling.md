# Error Handling

Zinc uses errors as values with auto-propagation — no try/catch syntax in user code.

## Returning Errors

Functions that can fail return `Error(...)`. The return type stays the same — the compiler handles the error plumbing:

```zinc
Int divide(Int a, Int b) {
    if b == 0 {
        return Error("division by zero")
    }
    return a / b
}
```

## Auto-Propagation

In `main()`, unhandled errors panic. In other functions, errors auto-propagate to the caller:

```zinc
main() {
    var result = divide(10, 2)    // panics if error
    print(result)
}
```

## Or Handlers

Use `or { }` to handle errors inline. The `err` variable is automatically available inside the handler:

```zinc
var bad = divide(10, 0) or {
    print("caught: {err}")
    exit(0)
}
```

If the handler ends with `exit()` or `panic()`, the error is not re-thrown. Otherwise it auto-propagates after the handler runs.

## Failable Built-in Functions

`readFile`, `writeFile`, and `httpGet` are failable:

```zinc
var content = readFile("data.txt") or {
    print("Error: {err}")
    exit(1)
}

writeFile("output.txt", "hello") or {
    print("Write failed: {err}")
}

var body = httpGet("https://api.example.com/data") or {
    print("Request failed: {err}")
    exit(1)
}
```

## With (Resource Management)

The `with` statement is Zinc's equivalent of Java's try-with-resources, Python's `with`, and C#'s `using`:

```zinc
import "os"

main() {
    with (f = os.Create("output.txt")) {
        f.WriteString("hello from Zinc")
    }
    // f is closed automatically
}
```

### `with` + `or` Handler

```zinc
with (f = os.Open("/nonexistent/file") or {
    print("caught: {err}")
    exit(1)
}) {
    print("should not reach")
}
```

### Mutex Locking

`with` auto-detects `sync.Locker` and locks/unlocks:

```zinc
import "sync"

main() {
    var counter = 0
    with (mu = sync.Mutex()) {
        counter += 1    // mutex locked here, unlocked when block exits
    }
}
```

### Multiple Resources

```zinc
with (f1 = os.Create("a.txt"), f2 = os.Create("b.txt")) {
    f1.WriteString("file A")
    f2.WriteString("file B")
}
// f2 closes first, then f1 (LIFO)
```

## Runtime Error Reporting

Zinc emits `#line` directives so runtime exceptions show your `.zn` source file and line number — not the generated C#. By default, `zinc build` embeds debug info for full stack traces. Use `zinc build --release` for production builds that strip symbols.

## How It Works

| Concept | C# Backend |
|---------|------------|
| Error type | `Exception` |
| `or { }` | `try/catch (Exception)` |
| `err` variable | `exception.Message` (string) |
| Auto-propagation | `throw;` |
| `panic()` | `throw new Exception(msg)` |
| `exit()` | `Environment.Exit(code)` |
