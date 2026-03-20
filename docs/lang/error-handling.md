# Zinc — Error Handling

Zinc uses a two-track error model: `Result<T>` for expected failures and exceptions for unexpected failures.

## Track 1 — Result<T> for Expected Failures

Use `Result<T>` for validation, parsing, missing data — anything you would put in a loop over 10,000 records:

```zinc
fn parse_port(str s) Result<int> {
    if not s.isdigit() {
        return Err("not a number: {s}")
    }
    var int port = int(s)
    if port < 1 or port > 65535 {
        return Err("out of range: {port}")
    }
    return port                  // auto-wrapped in Ok()
}
```

### Err with Default Value

Provide a fallback value inline:

```zinc
var int port = parse_port("8080") Err 80
```

If `parse_port` returns an `Err`, the variable gets the default value `80` instead.

### Err Handler Block

Handle the error with a block. The error is available as `err`:

```zinc
var int port = parse_port(input) Err {
    print("bad port: {err}")
    return
}
```

### Batch Processing

`Err` blocks work naturally with `continue` to skip bad records:

```zinc
for record in records {
    var int age = parse_age(record["age"]) Err {
        print("skipping: {err}")
        continue
    }
    process(age)
}
```

### Returning Result<T>

Return a bare value for success (auto-wrapped in `Ok`) or `Err(message)` for failure:

```zinc
fn divide(float a, float b) Result<float> {
    if b == 0 {
        return Err("division by zero")
    }
    return a / b
}

fn find_user(str id) Result<User> {
    var user = db.get(id)
    if user is none {
        return Err("user not found: {id}")
    }
    return user
}
```

## Track 2 — Exceptions for Unexpected Failures

Use exceptions for program-stopping failures -- network down, disk full, out of memory:

### try / catch

```zinc
try {
    var conn = db.connect(url)
} catch ConnectionError err {
    print("database down: {err}")
    exit(1)
}
```

Multiple catch blocks:

```zinc
try {
    var data = fetch_and_parse(url)
} catch ConnectionError err {
    print("network error: {err}")
} catch ValueError err {
    print("parse error: {err}")
}
```

### raise

Raise an exception directly:

```zinc
raise ValueError("bad config")
```

### Exception Chaining (raise from)

Chain exceptions to preserve the original cause:

```zinc
try {
    var data = parse(raw)
} catch ParseError err {
    raise ConfigError("invalid config file") from err
}
```

## Choosing Between Track 1 and Track 2

| Scenario | Use |
|---|---|
| Parsing user input | `Result<T>` |
| Validating form fields | `Result<T>` |
| Missing dict keys in batch | `Result<T>` |
| Database connection failure | `try/catch` |
| File system errors | `try/catch` |
| Out of memory | `try/catch` |
| Network timeout | `try/catch` |

Rule of thumb: if the failure is part of normal business logic, use `Result<T>`. If the failure means something is fundamentally broken, use exceptions.
