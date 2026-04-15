# Error Handling

Zinc replaces Go's `if err != nil` pattern with `or` expressions. Functions that can fail return `(T, error)` pairs automatically — you handle the error inline.

## Functions that can fail

Return `Error(message)` to signal failure:

```zinc
int divide(int a, int b) {
    if (b == 0) { return Error("division by zero") }
    return a / b
}
```

This compiles to a Go function returning `(int, error)`.

## or — fallback value

Provide a default when a call fails:

```zinc
var result = divide(10, 0) or -1    // -1
var ok = divide(10, 2) or -1        // 5
```

## or block — error handler

Run a block of code on failure:

```zinc
divide(10, 0) or {
    print("something went wrong")
}
```

## or block with return — early exit

Return from the enclosing function on error:

```zinc
String loadAndProcess() {
    var data = divide(10, 0) or {
        return "fallback result"
    }
    return "processed: {data}"
}
```

## or block with continue — skip in loops

Skip failed iterations:

```zinc
List<int> divisors = [2, 0, 5, 0, 3]
for (d in divisors) {
    var val = divide(100, d) or {
        continue
    }
    print("100/{d} = {val}")
}
// prints: 100/2 = 50, 100/5 = 20, 100/3 = 33
```

## Error propagation

Propagate errors up the call stack with `return Error(err)`:

```zinc
int safeDivide(int a, int b) {
    var result = divide(a, b) or {
        return Error(err)
    }
    return result
}

var x = safeDivide(10, 0) or -99    // -99
```

The `err` variable is automatically available inside `or` blocks.

## Comparison with Go

| Zinc | Go |
|------|-----|
| `return Error("msg")` | `return 0, errors.New("msg")` |
| `val = f() or default` | `val, err := f(); if err != nil { val = default }` |
| `f() or { return Error(err) }` | `val, err := f(); if err != nil { return 0, err }` |
| `f() or { continue }` | `val, err := f(); if err != nil { continue }` |

Zinc's error handling is zero-cost — it compiles to the exact same Go error patterns, just without the boilerplate.

## Constructors always succeed — use a factory for failable construction

A constructor (`init`) has no failure channel. The caller always gets a fully-constructed instance, so there is no way for `init` to signal "stop, construction failed." A bare `return` inside an `init` body is rejected at compile time:

```zinc
class Config {
    String host
    int port
    init(String host, int port) {
        this.host = host
        if (port < 0) {
            return   // compile error: bare `return` not allowed in ctor body
        }
        this.port = port
    }
}
```

If construction can actually fail, lift the failable check into a factory function that returns `T?` and emits `Error(reason)` on the failure path. The factory constructs via the ctor on success:

```zinc
Config? newConfig(String host, int port) {
    if (port < 0) {
        return Error("port must be non-negative: ${port}")
    }
    return Config(host, port)
}

// Caller handles failure at the call site — same or { } shape
// as any other failable call:
var cfg = newConfig("localhost", -1) or {
    print("bad config: ${err}")
    return
}
```

This keeps construction failure visible at every call site rather than hidden behind a half-built object. It is the same design rule as functions that return `Error(...)` (see [Functions that can fail](#functions-that-can-fail)), applied to the construction boundary.

### Rationale

Silently early-exiting from a constructor — e.g., `if bad_input { return }` — would hand the caller an object whose fields are only partially initialized, with no way to know. That is the exact shape of errors-as-values leaking into a silent-failure design: errors-as-values says every failure must be *reachable* at the call site through `T?` + `or { }`, and a partially-constructed object isn't. The compile-time rejection keeps the discipline visible. Guard-invert (`if good_input { parse(...) }` with no `return`) remains valid for benign empty-input handling where no error is being signalled.
