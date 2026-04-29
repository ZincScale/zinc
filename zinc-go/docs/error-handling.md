# Error handling

Zinc uses **errors as values**, modelled on Go but with the boilerplate removed. A function is a thrower iff its **declared return type contains `error` in the trailing position** — `error` (bare), `(T, error)`, or `(T1, ..., Tn, error)`. Callers handle errors at the call site with `or { ... }`. There is no `try / catch / throw / finally`, no auto-widening, no `?` operator.

## The model in one paragraph

A function whose declared return type ends in `error` becomes a Go function with `error` as the trailing result. The match is 1:1 with Go's native `(T, error)` shape — no wrapper types, no inference. At the call site, write `var x = call(...) or { ... }` to handle the error (`err` is bound inside the block) or destructure a multi-value thrower with `var (a, b) = call(...) or { ... }`. To propagate from inside another thrower, the canonical form is `or { return err }`. The `BaseError` base class in `stdlib/errors` has an `Error() string` method, so values satisfy Go's `error` interface and compose with `errors.Is`, `errors.As`, and `fmt.Errorf("%w", ...)` wrapping.

## Declaring a thrower

The trailing-`error` rule covers three shapes:

```zinc
import stdlib/errors

// Single-value thrower — Go: func ParseInt(s string) (int, error)
pub (int, error) parseInt(String s) {
    if (s == "") {
        return errors.IllegalArgumentError("empty input")
    }
    return 42, null
}

// Multi-value thrower — Go: func Lookup(k string) (int, string, error)
pub (int, String, error) lookup(String key) {
    if (key == "") {
        return errors.IllegalArgumentError("missing key")
    }
    return 7, "found", null
}

// Bare-error / void thrower — Go: func Validate(s string) error
pub error validate(String s) {
    if (s == "bad") {
        return errors.IllegalArgumentError("bad input")
    }
    return null
}
```

The `(T)` singleton form collapses to `T`, so `(Int) foo()` parses identically to `Int foo()` — both forms are non-throwers.

## Returning from a thrower

Three valid return forms inside a declared thrower:

| Form | Meaning |
|---|---|
| `return v1, ..., vN, null` | Success: every value slot spelled out, `null` for the error slot |
| `return v` (single-value thrower only) | Success shorthand: `return v` from `(T, error)` lowers to `return v, nil` |
| `return errVal` | Failure: any expression whose type extends `BaseError`. Value slots auto-fill with their zero values. |

```zinc
pub (int, String, error) parseUser(String s) {
    if (s == "") {
        return errors.IllegalArgumentError("empty")
        // → emitted as: return 0, "", NewIllegalArgumentError("empty")
    }
    return 42, "alice", null
}
```

The auto-zero-fill is what makes the design ergonomic — you don't repeat `0, ""` on every error path.

## Defining error types

`stdlib/errors` ships the base class and common types:

```zinc
// stdlib/errors
pub class BaseError {
    pub String message
    init(String message) { this.message = message }
    pub String Error() { return message }
}

pub class IllegalArgumentError : BaseError { ... }
pub class IllegalStateError    : BaseError { ... }
pub class IOError              : BaseError { ... }
pub class ConfigError          : BaseError { ... }
```

Define your own by extending `BaseError`:

```zinc
import stdlib/errors

class ParseError : errors.BaseError {
    init(String message) { super(message) }
}

class NetworkError : errors.BaseError {
    pub int statusCode
    init(String message, int statusCode) {
        super(message)
        this.statusCode = statusCode
    }
}
```

Subclass chains work — anything transitively extending `BaseError` is an error.

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

- `err` is bound to the error value (always typed as `error`).
- The block must terminate the binding's scope — typically `return`, `break`, `continue`, or a fallback assignment. Otherwise control would fall through with `n` undefined.

A few common patterns:

```zinc
// Fallback value via single-expression block
var port = parsePort(s) or { 8080 }

// Wrap and re-throw (works inside a thrower function)
var cfg = loadConfig(path) or {
    return errors.ConfigError("loading ${path} failed: ${err}")
}

// Type-dispatched handling
var x = call() or match err {
    case errors.IOError    -> { logging.warn("io"); return }
    case errors.ConfigError -> { logging.warn("config"); return }
}
```

## Multi-value destructure with `or { }`

A thrower whose return is `(T1, ..., Tn, error)` destructures into N value names — the error slot is captured in the implicit `err` binding:

```zinc
var (n, label) = lookup("foo") or {
    print("lookup err: ${err}")
    return
}
print("got: ${n}/${label}")
```

The compiler emits `n, label, _err := lookup("foo"); if _err != nil { ... }`.

## Propagation: `or { return err }`

There is no implicit propagation and no `?` operator. To forward an error to your caller, the calling function must itself be a declared thrower, and the call site spells out the propagation:

```zinc
pub (int, error) doubleIt(String s) {
    var n = parseInt(s) or { return err }
    return n * 2
}
```

`return err` from a `(T1, ..., Tn, error)` thrower auto-fills the value slots with their zero values, so the user never types `return 0, "", err`.

## Functions returning `BaseError` directly

A method whose "happy path" return type is the error itself uses the bare-error form:

```zinc
pub error validate(Config c) {
    if (c.host == "") {
        return errors.IllegalArgumentError("host required")
    }
    return null
}
```

This emits `func validate(c *Config) error`.

## Function-typed slots: `Fn<...>` with thrower returns

Function-type aliases follow the same trailing-`error` rule:

```zinc
// A factory that produces a Processor or fails
type ProcessorFactory = Fn<(Config), (Processor, error)>

pub class Registry {
    Map<String, ProcessorFactory> factories

    pub (Processor, error) create(String name, Config cfg) {
        var fac = factories[name]
        return fac(cfg)              // pass-through; fac()'s tuple flows out
    }
}
```

The transpiler resolves the alias at the call site (`fac(cfg)`) and recognizes the call as a thrower — so the surrounding `(Processor, error)` signature absorbs the call cleanly. Cross-package alias resolution works the same way; the compiler propagates `type` declarations across packages automatically.

## Lambdas with thrower bodies

A lambda assigned to a `Fn<..., (T, error)>` slot picks up the thrower context from the target type. Both expression-form and block-form bodies work:

```zinc
Fn<(int), (int, error)> dbl = (int x) -> (x * 2, null)

Fn<(int), (int, error)> safeDiv = (int n) -> {
    if (n == 0) {
        return errors.IllegalArgumentError("zero")
    }
    return 100 / n, null
}
```

The same hint flows through method args — `reg.register("dbl", (int x) -> ...)` lets the lambda's body emit with the registry method's parameter type as the target.

## Custom errors satisfy Go's `error`

Because `BaseError.Error()` exists, every `BaseError`-extending class is a Go `error`. Generated code interops cleanly with the Go ecosystem:

- Pass to `fmt.Errorf("...: %w", e)` to wrap.
- Match with `errors.As(err, &target)` to extract a concrete type.
- Compose with `errors.Is(err, sentinel)`.

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
var errCh = Channel<error>(len(items))
parallel for (item in items) {
    process(item) or { errCh.send(err); return }
}
```

## Migration from older Zinc

If you have older Zinc code that relied on the auto-widen design (returning a `BaseError` from a function declared with a non-error type), the rewrite is mechanical:

| Old (auto-widen) | New (explicit) |
|---|---|
| `int parse(String s) { return ParseError(...) }` | `(int, error) parse(String s) { return ParseError(...) }` |
| `int wrap(String s) { var n = parse(s); return n*2 }` | `(int, error) wrap(String s) { var n = parse(s) or { return err }; return n*2 }` |
| `error validate(...) { return null }` | unchanged — bare `error` was always valid |
| `var x = thrower()` (implicit propagate) | `var x = thrower() or { return err }` |

Earlier drafts also had `try / catch / throw / finally` and an experimental `Result<T, E>` wrapper. Both are gone:

| Older form | New form |
|---|---|
| `throw FooError("msg")` | `return errors.FooError("msg")` |
| `try { var x = call() } catch (e) { handle }` | `var x = call() or { handle }` (where `err` replaces `e`) |
| `Result<T, E> foo()` | `(T, error) foo()` (`E` collapses to the single `error` interface — use a class hierarchy on top of `BaseError` if you need typed dispatch) |
| `Ok(v)` / `Err(e)` | `return v, null` / `return e` |
