# Design: Errors as Values — Unified Error Handling

> **Status**: DESIGN
> **Replaces**: Two-track error handling (Result<T> + exceptions)

## Summary

All errors in Zinc are values. There is no `try`, `catch`, or `throw` in Zinc syntax. The transpiler generates Java exception machinery under the hood. Every external Java call is wrapped in try/catch — the JVM optimizes non-throwing try blocks to zero cost.

## Motivation

The two-track model (Result<T> for expected failures, exceptions for unexpected ones) forces developers to decide upfront which track an error belongs to. In practice:

- Most errors just propagate upward or crash the program
- Validation errors get logged and skipped
- Only API boundaries need typed error matching
- Nobody handles different exception types differently except in I/O

One mechanism covers all of these.

## Function signatures — success type only

Functions declare only the success return type. No `Result<T>`, no `throws` clause.

```zinc
fn fetchUser(int id) User { ... }
fn parsePort(String s) int { ... }
fn add(int a, int b) int { ... }
```

Transpiles to identical Java signatures:

```java
public static User fetchUser(int id) { ... }
public static int parsePort(String s) { ... }
public static int add(int a, int b) { ... }
```

A function that contains `return Error(...)` can fail. A function that doesn't, can't. The caller doesn't need to know — they use `or` if they want to handle it, or let it propagate if they don't. No changes to the type system or function signature parsing.

## Syntax

### 1. No handler — auto-propagate (the default)

```zinc
var user = fetchUser(id)
```

If `fetchUser` fails, the error propagates to the caller automatically. No ceremony.

**Transpiles to:**
```java
var user = fetchUser(id);
```

Plain Java call. Exceptions propagate naturally up the call stack.

### 2. Fallback value — `or default`

```zinc
var port = parsePort(input) or 8080
```

**Transpiles to:**
```java
int port;
try { port = parsePort(input); } catch (Exception _err) { port = 8080; }
```

### 3. Handler block — `or { }`

```zinc
var user = fetchUser(id) or {
    log("fetch failed: {err}")
    return Error(err)
}
```

`err` is implicitly available in the `or` block (the caught exception).

**Transpiles to:**
```java
User user;
try {
    user = fetchUser(id);
} catch (Exception err) {
    log("fetch failed: " + err);
    throw err;
}
```

### 4. Continue in loops

```zinc
for id in ids {
    var user = fetchUser(id) or {
        failures.add(err)
        continue
    }
    process(user)
}
```

**Transpiles to:**
```java
for (var id : ids) {
    User user;
    try {
        user = fetchUser(id);
    } catch (Exception err) {
        failures.add(err);
        continue;
    }
    process(user);
}
```

### 5. Typed error matching — `or match` (rare, API boundaries)

```zinc
var user = fetchUser(id) or match err {
    case NotFound -> return Response(404, "not found")
    case Timeout -> return Response(504, "gateway timeout")
    case _ -> return Response(500, "internal error")
}
```

**Transpiles to:**
```java
User user;
try {
    user = fetchUser(id);
} catch (NotFoundException err) {
    return new Response(404, "not found");
} catch (TimeoutException err) {
    return new Response(504, "gateway timeout");
} catch (Exception err) {
    return new Response(500, "internal error");
}
```

### 6. Returning errors — `return Error(...)`

```zinc
fn fetchUser(int id) User {
    var resp = http.get("/users/{id}") or {
        return Error("request failed: {err}")
    }
    if resp.status == 404 {
        return Error("user not found")
    }
    return parse(resp.body)
}
```

`return Error(msg)` creates and throws an exception. The caller never sees exceptions — they see `or` handlers.

**Transpiles to:**
```java
public static User fetchUser(int id) {
    Response resp;
    try {
        resp = http.get("/users/" + id);
    } catch (Exception err) {
        throw new RuntimeException("request failed: " + err);
    }
    if (resp.status == 404) {
        throw new RuntimeException("user not found");
    }
    return parse(resp.body);
}
```

### 7. Custom error types

```zinc
data NotFound(String message)
data Timeout(String message)

fn fetchUser(int id) User {
    var resp = http.get("/users/{id}") or {
        return Error(Timeout("timed out fetching user {id}"))
    }
    if resp.status == 404 {
        return Error(NotFound("user {id} not found"))
    }
    return parse(resp.body)
}
```

Custom error types are just data classes. `return Error(NotFound(...))` throws the data class as an exception.

**Transpiles to:** data class extends RuntimeException, throw instance.

### 8. With — resource management + error handling

```zinc
with var conn = db.connect() or {
    return Error("db unavailable: {err}")
} {
    var rows = conn.query("SELECT * FROM users") or {
        return Error("query failed: {err}")
    }
    process(rows)
}
```

The `or` on the `with` handles resource creation failure. If creation succeeds, the resource is auto-closed after the block.

**Transpiles to:**
```java
Connection conn;
try {
    conn = db.connect();
} catch (Exception err) {
    throw new RuntimeException("db unavailable: " + err);
}
try (conn) {
    List<Row> rows;
    try {
        rows = conn.query("SELECT * FROM users");
    } catch (Exception err) {
        throw new RuntimeException("query failed: " + err);
    }
    process(rows);
}
```

## What gets removed from Zinc

| Removed | Replacement |
|---|---|
| `try { } catch { }` | `or { }` on the call |
| `throw X` | `return Error(X)` |
| `Result<T>` return type | Implicit — all functions can fail |
| `catch ExceptionType e` | `or match err { case Type -> }` |

## What stays

| Feature | How it works |
|---|---|
| `or default` | Unchanged |
| `or { block }` | Unchanged, `err` implicit |
| `with` (try-with-resources) | Works with `or` on resource creation |
| `err` variable | Implicitly available in `or` blocks |

## Transpilation rules

1. **No `or` handler** — emit plain Java call. Exceptions propagate naturally.
2. **`or default`** — wrap in `try { } catch (Exception _err) { x = default; }`.
3. **`or { block }`** — wrap in `try { } catch (Exception err) { block }`.
4. **`or match err { cases }`** — wrap in `try { } catch (TypeA err) { } catch (TypeB err) { } ...`.
5. **`return Error(msg)`** — emit `throw new RuntimeException(msg)`.
6. **`return Error(CustomType(...))`** — emit `throw new CustomType(...)` (data class extends RuntimeException).
7. **`with var x = call() or { } { body }`** — separate try/catch for creation, then `try (x) { body }`.
8. **All external Java calls** — no special wrapping. JVM try/catch has zero cost on happy path.

## Design rationale

**Why not track failability?** Every external Java method could throw (unchecked exceptions exist everywhere). Tracking which calls can fail adds complexity for no benefit. The JVM's try/catch is free when no exception is thrown — the JIT eliminates it entirely on the happy path.

**Why not checked exceptions?** Java tried this. Everyone hated it. Zinc's `or` handlers are opt-in — you add them where you care, and errors propagate silently where you don't.

**Why `return Error(...)` instead of `throw`?** Consistency. In Zinc, errors are values you return, not control flow you throw. The transpiler maps to throw, but the mental model is: your function returns either a value or an error.
