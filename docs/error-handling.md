# Error Handling

## Philosophy: Two Tracks

Zinc separates **expected failures** from **exceptional failures**. This is a core design principle.

| | Expected failures | Exceptional failures |
|---|---|---|
| **What** | Validation errors, missing keys, parse failures, "not found" | Disk full, network down, out of memory, permission denied |
| **How often** | Constantly — this is normal data flow | Rarely — something is genuinely broken |
| **Zinc mechanism** | `Result[T]` with `Err {}` blocks | `try/catch` — exceptions |
| **Control flow** | Returned, handled inline | Thrown, caught at boundaries |
| **Performance** | Zero overhead — just a return value | Stack unwinding — expensive |
| **Example** | "User entered invalid email" | "Database connection dropped" |

**The rule:** If you can reasonably expect it to happen during normal operation, it's a Result. If something is genuinely broken, it's an exception.

---

## Track 1: Results (Expected Failures)

### Returning Results

Functions that can fail in expected ways return `Result[T]`. Return the value on success — no `Ok()` wrapper needed. Return `Err("message")` on failure:

```zinc
fn parse_age(input: str) -> Result[int]
    if not input.isdigit()
        return Err("age must be a number, got: {input}")
    end
    var age = int(input)
    if age < 0 or age > 150
        return Err("age must be between 0 and 150, got: {age}")
    end
    return age
end
```

Transpiles to:
```python
from zinc import Ok, Err, Result

def parse_age(input: str) -> Result[int]:
    if not input.isdigit():
        return Err(f"age must be a number, got: {input}")
    age = int(input)
    if age < 0 or age > 150:
        return Err(f"age must be between 0 and 150, got: {age}")
    return Ok(age)  # transpiler wraps in Ok automatically
```

### Handling Results with `Err {}`

When you call a function that returns `Result[T]`, handle the error inline with `Err {}`. The `err` variable is automatically available. If the result is OK, you get the value and continue — no ceremony:

```zinc
// Handle error and bail
var age = parse_age(input) Err {
    print("bad age: {err}")
    return
}
// age is an int here — just keep going
print("Age: {age}")
```

```zinc
// Provide a default value (last expression in Err block)
var age = parse_age(input) Err { 0 }
```

```zinc
// Skip bad records in a loop
for i, record in enumerate(records)
    var age = parse_age(record["age"]) Err {
        errors.append("record {i}: {err}")
        continue
    }
    var email = validate_email(record["email"]) Err {
        errors.append("record {i}: {err}")
        continue
    }
    users.append(User(record["name"], age, email))
end
```

Transpiles to:
```python
for i, record in enumerate(records):
    __r = parse_age(record["age"])
    if __r.is_err():
        errors.append(f"record {i}: {__r.err()}")
        continue
    age = __r.unwrap()
    __r = validate_email(record["email"])
    if __r.is_err():
        errors.append(f"record {i}: {__r.err()}")
        continue
    email = __r.unwrap()
    users.append(User(record["name"], age, email))
```

### Validation Chains

Real-world data processing validates many fields. Zinc makes this clean:

```zinc
fn validate_order(raw: dict) -> Result[Order]
    var id = require_str(raw, "id") Err { return Err(err) }
    var amount = require_float(raw, "amount") Err { return Err(err) }
    var email = validate_email(raw.get("email", "")) Err { return Err(err) }

    if amount <= 0
        return Err("amount must be positive")
    end

    return Order(id, amount, email)
end

// Process a batch — collect errors, don't throw
fn process_batch(records: list[dict]) -> (list[Order], list[str])
    var orders = []
    var errors = []
    for i, raw in enumerate(records)
        var order = validate_order(raw) Err {
            errors.append("record {i}: {err}")
            continue
        }
        orders.append(order)
    end
    return (orders, errors)
end
```

No exceptions. No try/catch. Just data flowing through validation with clear error accumulation. This is how you process 10,000 flowfiles — you don't want to throw and catch 10,000 exceptions.

### Err Block Rules

| Err block ends with | Behavior |
|---|---|
| `return` / `return Err(...)` | Bail out of the function |
| `continue` | Skip to next loop iteration |
| `break` | Exit the loop |
| An expression (e.g. `0`, `""`) | Use as default value |
| Nothing / `print(...)` etc. | Transpiler error — must bail or provide a default |

The transpiler enforces that every `Err {}` block either exits control flow or provides a fallback value. You can't silently ignore an error.

### Common Result Helpers

```zinc
// Built-in validation helpers that return Result
fn require_str(d: dict, key: str) -> Result[str]
fn require_int(d: dict, key: str) -> Result[int]
fn require_float(d: dict, key: str) -> Result[float]
fn validate_email(s: str) -> Result[str]
fn parse_date(s: str, fmt: str) -> Result[datetime]
fn parse_json(s: str) -> Result[dict]
```

These return `Err` with a clear message — never throw.

---

## Track 2: Exceptions (Unexpected Failures)

For things that are genuinely exceptional — I/O failures, system errors, network problems. Use `try/catch` at boundaries, not sprinkled through business logic.

### Try / Catch / Finally

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

### Raising Exceptions

Only for truly exceptional situations:

```zinc
fn connect_db(url: str) -> Connection
    try
        return db.connect(url)
    catch err: Exception
        raise ConnectionError("Cannot reach database at {url}: {err}")
    end
end
```

### Custom Exceptions

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
```

### With (Context Managers)

```zinc
with open("data.txt") as f
    var content = f.read()
    print(content)
end
```

---

## When to Use Which

| Situation | Use | Why |
|---|---|---|
| User input validation | `Result[T]` | Expected — users enter bad data constantly |
| Parsing a field from JSON/CSV | `Result[T]` | Expected — data is messy |
| Record doesn't match schema | `Result[T]` | Expected — you're processing a batch |
| Key missing from dict | `Result[T]` | Expected — data varies |
| File not found | Exception | Could go either way — exception for I/O is conventional |
| Network timeout | Exception | Exceptional — infrastructure failure |
| Out of memory | Exception | Exceptional — system failure |
| Database connection dropped | Exception | Exceptional — infrastructure failure |
| Permission denied | Exception | Exceptional — deployment/config problem |

**The litmus test:** If you'd put it in a loop processing 10,000 records, it should be a Result. If it would stop your entire program, it's an exception.

---

## Transpiler Safety

| Footgun | Zinc prevention |
|---|---|
| Using exceptions for validation | Warning: "consider returning Result instead of raising in a loop" |
| Bare `except:` catching everything | Not allowed — must specify exception type |
| Silently swallowing errors | Warning if `catch` block is empty |
| Ignoring a Result | Error if `Result[T]` return value has no `Err {}` handler |
| `Err {}` that doesn't bail or default | Error — must exit control flow or provide a fallback |
| Catching `BaseException` | Warning — you probably don't want to catch `KeyboardInterrupt` |

## How It Maps

| Zinc | Python |
|---|---|
| `Result[T]` | `zinc.Result[T]` (tiny runtime type) |
| `return value` (in Result fn) | `return Ok(value)` (transpiler wraps) |
| `return Err(message)` | `return Err(message)` |
| `expr Err { handle }` | Unwrap or run error handler |
| `expr Err { default }` | Unwrap or use default value |
| `try ... end` | `try:` |
| `catch err: TypeError` | `except TypeError as err:` |
| `raise` | `raise` |
| `with x as y ... end` | `with x as y:` |
