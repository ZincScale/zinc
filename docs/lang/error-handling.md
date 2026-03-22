# Zinc — Error Handling

Zinc uses errors as values. All fallible functions return `Result<T>` and callers handle errors with `or` handlers. There is no try/catch/throw in Zinc.

## Result<T> — Errors as Values

Use `Result<T>` for any operation that can fail — validation, parsing, I/O, missing data, network calls:

```zinc
fn parsePort(String s) Result<int> {
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
fn loadConfig(String path) Config {
    var String content = readFile(path) or {
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
fn divide(double a, double b) Result<double> {
    if b == 0 {
        return Error("division by zero")
    }
    return a / b
}

fn findUser(String id) Result<User> {
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
fn processOrder(String orderId) Result<Receipt> {
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

## or match — Typed Error Handling

Use `or match` to handle different error types with pattern matching:

```zinc
fn loadConfig(String path) Result<Config> {
    var content = readFile(path) or match err {
        case "not found" -> return Error("config missing: {path}")
        case _ -> return Error("cannot read config: {err}")
    }
    return parseConfig(content)
}
```

Handle different error data types:

```zinc
data NotFound(String message)
data Timeout(String message)

fn fetchData(String url) Result<String> {
    var response = httpGet(url) or match err {
        case NotFound -> return Error("resource missing: {err.message}")
        case Timeout -> return Error("request timed out: {err.message}")
        case _ -> return Error("fetch failed: {err}")
    }
    return response.body
}
```

## Custom Error Types

Define error types as data classes:

```zinc
data ValidationError(String field, String reason)
data NotFoundError(String entity, String id)

fn validateAge(String input) Result<int> {
    var age = parseInt(input) or {
        return Error(ValidationError("age", "not a number: {input}"))
    }
    if age < 0 or age > 150 {
        return Error(ValidationError("age", "out of range: {age}"))
    }
    return age
}
```

## When to Use Result<T>

All fallible operations use `Result<T>` — there is no separate exception track:

| Scenario | Pattern |
|---|---|
| Parsing user input | `Result<T>` + `or` fallback |
| Validating form fields | `Result<T>` + `or match` |
| Missing keys in batch processing | `Result<T>` + `or { continue }` |
| Database connection failure | `Result<T>` + `or { return Error(...) }` |
| File system errors | `Result<T>` + `or { log; return }` |
| Network timeout | `Result<T>` + `or match` by error type |

**Rule of thumb**: if a function can fail, it returns `Result<T>`. The caller decides how to handle the error with `or`.

## Summary

| Syntax | Meaning |
|---|---|
| `return Error("msg")` | Return a failed Result from a `Result<T>` function |
| `return value` | Return a successful Result (auto-wrapped in Ok) |
| `expr or defaultValue` | Use default if expr fails |
| `expr or { block }` | Run block if expr fails, last expression is fallback |
| `expr or { return x }` | Exit enclosing function on failure |
| `expr or { continue }` | Skip loop iteration on failure |
| `expr or match err { case T -> }` | Pattern match on the error |

## Java Transpilation

| Zinc | Java |
|---|---|
| `return Error("msg")` | `throw new RuntimeException("msg");` |
| `call() or default` | `try { call(); } catch (Exception e) { default; }` |
| `or match err { case T -> }` | `catch (T e) { ... }` |
