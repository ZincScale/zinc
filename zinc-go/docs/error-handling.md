# Error Handling

Zinc replaces Go's `if err != nil` pattern with `or` expressions. Functions that can fail return `(T, error)` pairs automatically — you handle the error inline.

## Functions that can fail

Return `Error(message)` to signal failure:

```zinc
fn divide(int a, int b): int {
    if b == 0 { return Error("division by zero") }
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
fn loadAndProcess(): String {
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
for d in divisors {
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
fn safeDivide(int a, int b): int {
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
