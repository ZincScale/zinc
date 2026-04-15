# Error handling

Zinc uses **try/catch/throw** for control-flow errors, modeled on C# with unchecked exceptions. Anything that throws extends `Exception` from `stdlib.exceptions` and satisfies Go's `error` interface via an `Error() string` method — so Zinc exceptions compose with Go's `errors.Is`, `errors.As`, and `fmt.Errorf("%w", ...)` wrapping.

## The model in one paragraph

A function that calls `throw` (or calls another thrower and doesn't catch) has its Go signature widened to `(T, error)`. At call sites, the compiler emits `if err != nil { return err }` automatically — no explicit `or { }` handler is needed. Inside a `try { } catch (T e) { }` block, errors flow to the catches via `errors.As`, so typed catches still match even when the error was wrapped with `%w` upstream. Uncaught throws inside `spawn { }` or `go { }` panic the process — goroutines can't return errors to their launcher, and silent failure is never the default.

## Throwing

```zinc
import stdlib.exceptions

int parseInt(String s) {
    if (s == "") {
        throw exceptions.IllegalArgumentException("empty input")
    }
    // ... normal path
    return 42
}
```

`throw expr` requires `expr` to satisfy Go's `error` interface. All stdlib exception types do (via the `Error()` method on the base `Exception`). Custom exceptions work the same way:

```zinc
class ParseException {
    pub String message
    init(String message) { this.message = message }
    pub String Error() { return message }
}
```

Keep the class small: one field, one `Error()` method. The rest of the error ergonomics (wrapping, typed matching) comes from Go's `errors` package.

## Catching

```zinc
try {
    var n = parseInt(raw)
    process(n)
} catch (exceptions.IllegalArgumentException e) {
    logging.warn("bad input", "value", raw, "reason", e.message)
} catch (e) {
    // Universal catch — binds the raw error.
    logging.error("unexpected", "err", e.Error())
}
```

Rules:

- **Typed catches** use `errors.As` under the hood, so they match across `fmt.Errorf("%w", inner)` wrapping chains.
- **Universal catch** (bare `catch { }` or `catch (e) { }`) absorbs anything the body can throw.
- **Multiple catches** probe in source order — first match wins.
- **Uncaught** errors propagate up through any function that can throw, stopping at the first surrounding try with a matching catch.
- **No `throws` clause.** Exceptions are unchecked.

## Finally

```zinc
try {
    session.begin()
    work(session)
    session.commit()
} catch (e) {
    session.rollback()
    throw e
} finally {
    session.close()
}
```

`finally` runs on every exit path — after a matching catch, before propagation of an unhandled error, and after a user `return` inside the try.

Prefer `using` over `finally` for resource cleanup — it's shorter and harder to misuse:

```zinc
using (var session = db.open()) {
    work(session)
    session.commit()
}  // session.Close() runs on exit, including via throw
```

## Constructors can throw

An `init { }` that throws aborts object construction — no partially-initialized object escapes:

```zinc
class Config {
    pub String path
    pub Map<String, String> values

    init(String path) {
        if (!exists(path)) {
            throw exceptions.IOException("config not found: ${path}")
        }
        this.path = path
        this.values = load(path)
    }
}
```

Callers handle this like any other thrower:

```zinc
try {
    var cfg = Config("/etc/app.yml")
    run(cfg)
} catch (exceptions.IOException e) {
    fatal("startup failed: ${e.message}")
}
```

## Goroutines

`spawn { }`, `go { }`, `parallel for`, and `concurrent { }` cannot propagate errors back to the launching thread — Go goroutines have no return channel to the caller. The compiler's contract:

- **Caught** inside the goroutine body: the error is handled locally. No panic.
- **Uncaught**: the process panics with a stack trace pointing at the spawn site. **No silent failures.**

```zinc
spawn {
    try {
        processOne(item)
    } catch (e) {
        logging.error("worker failed", "err", e.Error())
    }
}
```

If you want the error back on the launcher, use an actor (mailbox-based, errors travel as messages) or an explicit error channel. Raw `spawn` is fire-and-forget by design.

## What's gone

The pre-2026 errors-as-values machinery — `T?` plus `return Error("msg")` plus `call() or { ... }` — was removed in the exception pivot. Migration is mechanical:

| Old | New |
|---|---|
| `return Error("msg")` | `throw exceptions.ConfigException("msg")` |
| `var x = call() or default` | `try { var x = call(); ... } catch (e) { ... }` |
| `var x = call() or { return -1 }` | `try { var x = call() } catch (e) { return -1 }` |
| `call() or { log(err) }` | `try { call() } catch (e) { log(e.Error()) }` |

The parser rejects stray `or` with the migration hint pointing at try/catch.

## The `Exception` hierarchy

`stdlib.exceptions` ships a minimal set:

- `Exception` — base, satisfies Go's `error`
- `IllegalArgumentException` — bad argument
- `IllegalStateException` — wrong state for the operation
- `IOException` — I/O failure
- `ConfigException` — config or setup error

Define your own domain exceptions by extending `Exception`:

```zinc
import stdlib.exceptions

class AuthException : exceptions.Exception {
    init(String message) { super(message) }
}
```

Subclasses inherit `Error()` via struct embedding — no boilerplate.
