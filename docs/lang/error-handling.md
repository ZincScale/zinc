# Zinc — Error Handling

Zinc uses a two-track error model: `Result<T>` for expected failures and exceptions for unexpected failures.

## Track 1 — Result<T> for Expected Failures

Use `Result<T>` for validation, parsing, missing data — anything that can fail in normal business logic:

```zinc
fn parsePort(str s) Result<int> {
    if not s.isDigit() {
        return Error("not a number: {s}")
    }
    var int port = int(s)
    if port < 1 or port > 65535 {
        return Error("out of range: {port}")
    }
    return port                      // auto-wrapped in Ok()
}
```

### or — Default Value

Provide a fallback value inline when a Result fails:

```zinc
var int port = parsePort("8080") or 80
```

If `parsePort` returns an `Error`, the variable gets `80` instead.

### or — Error Handler Block

Handle the error with a block. The error is available as `err`:

```zinc
var int port = parsePort(input) or {
    log.warn("bad port: {err}, using default")
    8080                             // last expression is the fallback value
}
```

The `or { }` block must produce a value of the same type — it's the fallback path.

### or — Exit the Function

Use `return` in an `or` block to exit the enclosing function early:

```zinc
fn loadConfig(str path) Config {
    var str content = readFile(path) or {
        log.error("cannot read config: {err}")
        return Config.defaults()     // exits loadConfig, returns default
    }
    return parseConfig(content)
}
```

### or — Skip in Loops

Use `continue` in an `or` block to skip bad records in batch processing:

```zinc
for record in records {
    var int age = parseAge(record.get("age")) or {
        log.warn("skipping bad age: {err}")
        continue                     // skip this record, process next
    }
    process(age)
}
```

### Returning Result<T>

Return a bare value for success (auto-wrapped in `Ok`) or `Error(message)` for failure:

```zinc
fn divide(float a, float b) Result<float> {
    if b == 0 {
        return Error("division by zero")
    }
    return a / b
}

fn findUser(str id) Result<User> {
    var user = db.get(id)
    if user == null {
        return Error("user not found: {id}")
    }
    return user
}
```

### Result Chaining

Chain multiple failable operations — each `or` handles its own failure:

```zinc
fn processOrder(str orderId) Result<Receipt> {
    var order = findOrder(orderId) or {
        return Error("order not found: {err}")
    }
    var payment = chargeCard(order.total) or {
        return Error("payment failed: {err}")
    }
    var receipt = generateReceipt(order, payment) or {
        return Error("receipt generation failed: {err}")
    }
    return receipt
}
```

## Track 2 — Exceptions for Unexpected Failures

Use exceptions for program-stopping failures — network down, disk full, out of memory:

### try / catch

```zinc
try {
    var conn = db.connect(url)
} catch ConnectionError err {
    log.error("database down: {err}")
    throw ServiceUnavailable("database unavailable")
}
```

Multiple catch blocks:

```zinc
try {
    var data = fetchAndParse(url)
} catch ConnectionError err {
    log.error("network error: {err}")
} catch ParseError err {
    log.error("parse error: {err}")
}
```

Catch-all:

```zinc
try {
    riskyOperation()
} catch Exception err {
    log.error("unexpected: {err}")
}
```

### throw

Throw an exception:

```zinc
throw IllegalArgumentException("bad config")
```

### Exception Chaining (throw from)

Chain exceptions to preserve the original cause:

```zinc
try {
    var data = parse(raw)
} catch ParseError err {
    throw ConfigError("invalid config file") from err
}
```

Transpiles to:
```java
try {
    var data = parse(raw);
} catch (ParseError err) {
    throw new ConfigError("invalid config file", err);
}
```

## Choosing Between Track 1 and Track 2

| Scenario | Use | Why |
|---|---|---|
| Parsing user input | `Result<T>` | Expected to fail, caller handles it |
| Validating form fields | `Result<T>` | Normal business logic |
| Missing keys in batch processing | `Result<T>` | Skip and continue |
| Database connection failure | `try/catch` | Infrastructure broken |
| File system errors | `try/catch` | Can't recover locally |
| Out of memory | `try/catch` | Fundamentally broken |
| Network timeout | `try/catch` | Transient infrastructure |

**Rule of thumb**: if the failure is part of normal business logic, use `Result<T>` + `or`. If the failure means something is fundamentally broken, use `try/catch` + `throw`.

## Summary

| Syntax | Meaning |
|---|---|
| `return Error("msg")` | Return a failed Result from a `Result<T>` function |
| `return value` | Return a successful Result (auto-wrapped in Ok) |
| `expr or defaultValue` | Use default if expr fails |
| `expr or { block }` | Run block if expr fails, last expression is fallback |
| `expr or { return x }` | Exit enclosing function on failure |
| `expr or { continue }` | Skip loop iteration on failure |
| `try { } catch Type err { }` | Catch exceptions |
| `throw ExceptionType("msg")` | Throw an exception |
| `throw X from cause` | Throw with chained cause |
