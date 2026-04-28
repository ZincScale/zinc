# Error handling

Zinc uses **errors as values**, modelled on Go but with the boilerplate removed. Any class extending `Err` from `stdlib/errors` is an error type. Returning one widens the function's Go signature to `(T, error)` automatically. Callers either handle the error inline with `or { ... }` or let it propagate by doing nothing — in which case the caller's signature widens too.

There is no `try / catch / throw / finally`.

## The model in one paragraph

A function that returns an `Err`-extending value transpiles to a Go function returning `(T, error)`. At call sites, you write either `var x = call(...)` (the error propagates — your function widens) or `var x = call(...) or { ... }` (you handle it; `err` is bound inside the block). The `Err` base class has an `Error() string` method, so values satisfy Go's `error` interface and compose with `errors.Is`, `errors.As`, and `fmt.Errorf("%w", ...)` wrapping.

## Defining errors

`stdlib/errors` ships the base class and a small set of common types:

```zinc
// stdlib/errors
pub class Err {
    pub String message
    init(String message) { this.message = message }
    pub String Error() { return message }
    pub String toString() { return "Err(${message})" }
}

pub class IllegalArgumentError : Err { ... }
pub class IllegalStateError    : Err { ... }
pub class IOError              : Err { ... }
pub class ConfigError          : Err { ... }
```

Define your own domain errors by extending `Err`:

```zinc
import stdlib/errors

class ParseError : errors.Err {
    init(String message) { super(message) }
}

class NetworkError : errors.Err {
    pub int statusCode
    init(String message, int statusCode) {
        super(message)
        this.statusCode = statusCode
    }
}
```

Subclass chains work too — anything transitively extending `Err` is an error:

```zinc
class AppError : errors.Err {
    init(String m) { super(m) }
}

class NotFoundError : AppError {        // still an error — chain walked
    init(String m) { super(m) }
}
```

## Returning errors

```zinc
import stdlib/errors

int parseInt(String s) {
    if (s == "") {
        return errors.IllegalArgumentError("empty input")
    }
    return 42
}
```

`parseInt` declares `int` but can also return `IllegalArgumentError`. The compiler widens its Go signature to `(int, error)`. No `?` operator, no marker keyword — the error type in the body is the source of truth.

## Handling at the call site: `or { }`

```zinc
void main() {
    var n = parseInt(input) or {
        print("bad input: ${err}")
        return
    }
    use(n)
}
```

Inside the `or { }` block:

- `err` is bound to the error value.
- The block must `return` (or otherwise exit the function), so control doesn't fall through with `n` undefined.
- The block can do anything Zinc allows — log, fall back, rewrap, re-return.

```zinc
// Fallback value
var port = parsePort(s) or { return 8080 }

// Wrap and re-return
var cfg = loadConfig(path) or {
    return errors.ConfigError("loading ${path} failed: ${err}")
}
```

## Auto-propagation

If you call a throwing function without `or { }`, the error propagates: your function's signature widens to `(T, error)` and the error returns up to your caller.

```zinc
int doubleIt(String s) {
    var n = parseInt(s)        // no `or { }` — propagates
    return n * 2
}
```

`doubleIt` becomes `(int, error)` in Go. Your caller faces the same choice — handle with `or { }` or propagate further.

## Custom errors satisfy Go's `error`

Because `Err.Error()` exists, any `Err`-extending class is a Go `error`. That means generated code interops cleanly with the Go ecosystem:

- Pass to `fmt.Errorf("...: %w", e)` to wrap.
- Match with `errors.As(err, &target)` to extract a concrete type.
- Compose with `errors.Is(err, sentinel)`.

## Functions returning `Err` directly

If a function's "happy path" return type *is* an error, declare it as such — no widening required:

```zinc
errors.Err validate(Config c) {
    if (c.host == "") {
        return errors.IllegalArgumentError("host required")
    }
    return null
}
```

## Goroutines

`spawn { }`, `parallel for`, and `select { case ... }` blocks cannot return errors to their launcher — Go goroutines have no return channel to the caller. Handle errors inside the goroutine, or pass them out through a channel:

```zinc
spawn {
    var ok = doRiskyWork() or {
        logging.error("worker failed", "err", err)
        return
    }
    use(ok)
}
```

For error fan-in, send errors over an explicit channel:

```zinc
var errCh = Channel<errors.Err>(len(items))
parallel for (item in items) {
    process(item) or { errCh.send(err); return }
}
```

## Migration from try/catch

Earlier drafts of Zinc had `try / catch / throw / finally`. Those keywords are gone. The mechanical rewrite:

| Old | New |
|---|---|
| `throw FooError("msg")` | `return errors.FooError("msg")` |
| `try { var x = call() } catch (e) { handle }` | `var x = call() or { handle }` (where `err` replaces `e`) |
| `try { ... } catch (FooError e) { ... } catch (e) { ... }` | One `or { }` plus type checks: `or { if (err is FooError) { ... } else { ... } }` |
| `try { ... } finally { cleanup }` | Wrap the resource and call `cleanup` explicitly, or use Go's `defer` from generated code as needed |
