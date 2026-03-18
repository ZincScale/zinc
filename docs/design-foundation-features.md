# Design: Foundation Features

**Status:** Proposed
**Date:** 2026-03-18

## Overview

Five features that complete Zinc's foundation for enterprise development. Each is a building block — not speculative, needed for real apps.

| Feature | What | Why |
|---------|------|-----|
| `use` keyword | Replace `import` with `use` | Drop Go-style string imports, cleaner syntax |
| Destructuring | `var (a, b) = expr`, tuple returns | Multiple returns, clean API handling |
| `zinc test` | Test discovery + runner | Teams won't adopt without tests |
| `zinc add` | NuGet dependency management | Can't build real apps without packages |
| Logging | `log.info()`, `log.warn()`, `log.error()` | Enterprise requirement |

---

## 1. `use` Keyword

### Problem

`import "System.Text.Json"` uses Go-style string imports. Zinc no longer targets Go. The quotes add noise and suggest file paths, but these are .NET namespaces.

### Design

```zinc
// Before
import "System.Text.Json"
import "http"
import "Newtonsoft.Json" as nj

// After
use System.Text.Json
use http
use Newtonsoft.Json as nj
```

Rules:
- `use` replaces `import` everywhere
- No quotes — namespace is parsed as a dotted identifier sequence
- Short aliases still work: `use http` → `System.Net.Http`
- `as` alias still works: `use Foo.Bar as fb`
- `import` becomes a reserved-but-deprecated keyword (parser emits a warning)

### C# Mapping

No change to output. `use System.Text.Json` still emits `using System.Text.Json;`.

### Implementation

| Step | File | Change |
|------|------|--------|
| 1 | `internal/lexer/token.go` | Add `TOKEN_USE` keyword |
| 2 | `internal/lexer/lexer.go` | Map `"use"` → `TOKEN_USE` in keyword table |
| 3 | `internal/parser/parser.go` | `parseUseDecl()` — parse dotted identifier (no string literal), optional `as` alias |
| 4 | `internal/parser/parser.go` | Keep `parseImportDecl()` for backwards compat, emit deprecation warning |
| 5 | `internal/codegen_csharp/codegen.go` | `processImport` unchanged — it already receives a path string |
| 6 | `docs/imports.md` | Update all examples |
| 7 | `examples/` | Update all `.zn` files |

### Examples

```zinc
use System.Text.Json
use System.Text.Json.Serialization
use http
use json as j

main() {
    var client = HttpClient()
    var sw = Stopwatch()
}
```

---

## 2. Destructuring

### Problem

Zinc has `TupleVarStmt` in the parser and codegen (`var (a, b) = expr`) but no way to create tuples. Functions can't return multiple values. This blocks clean API patterns like:

```zinc
var (quotient, remainder) = divide(10, 3)
var (ok, value) = tryParse("42")
```

### Design

#### Tuple Literals

```zinc
var pair = (1, "hello")
var triple = (true, 42, "yes")
```

Emits C# `ValueTuple`:
```csharp
var pair = (1, "hello");
var triple = (true, 42, "yes");
```

#### Tuple Return Types

```zinc
(Int, Int) divide(Int a, Int b) {
    return (a / b, a % b)
}

(Bool, String) tryParse(String input) {
    var n = parseInt(input) or { return (false, "") }
    return (true, toString(n))
}
```

Emits:
```csharp
(int, int) Divide(int a, int b)
{
    return (a / b, a % b);
}
```

#### Destructuring Assignment

Already works in parser. Just needs E2E validation:

```zinc
var (q, r) = divide(10, 3)
print("quotient: {q}, remainder: {r}")
```

Emits:
```csharp
var _tuple = Divide(10, 3);
var q = _tuple.Item1;
var r = _tuple.Item2;
```

#### With `or { }`

```zinc
var (ok, value) = tryParse("abc") or {
    print("parse failed: {err}")
    return
}
```

### Implementation

| Step | File | Change |
|------|------|--------|
| 1 | `internal/parser/ast.go` | Add `TupleExpr` node: `Elements []Expr` |
| 2 | `internal/parser/parser.go` | Detect tuple literal in `parseExpr` — `(expr, expr, ...)` vs grouped expr `(expr)` |
| 3 | `internal/codegen_csharp/codegen.go` | `emitTupleExpr` → `(e1, e2, ...)` |
| 4 | `internal/parser/parser.go` | Parse tuple return types in function declarations: `(Type, Type) name(...)` |
| 5 | `internal/codegen_csharp/codegen.go` | Emit tuple return type: `(int, string)` |
| 6 | Unit tests | Tuple literal, tuple return, destructuring |
| 7 | E2E tests | Full round-trip: define function → call → destructure → print |

### Edge Cases

- Single-element tuple `(42,)` — not supported. Use a variable.
- Nested tuples `((1, 2), 3)` — not supported in Phase 1.
- Destructuring in `for` loops — not in Phase 1.

---

## 3. `zinc test`

### Problem

Enterprise teams live in tests. No testing story = no adoption. `zinc test` must feel as simple as `go test` or `pytest` — discover tests, run them, report results.

### Design

#### Test Functions

Convention: functions named `test_*` are tests.

```zinc
// math_test.zn

test_addition() {
    assert(1 + 1 == 2)
}

test_division() {
    assertEqual(10 / 2, 5)
}

test_division_by_zero() {
    var result = divide(10, 0) or {
        assert(err == "division by zero")
        return
    }
    panic("expected error")
}

test_string_operations() {
    var name = "Alice"
    assertEqual(name.length(), 5)
    assert(name.startsWith("Al"))
}
```

Rules:
- Test functions take no parameters and return nothing
- File naming: `*_test.zn` (convention, not enforced — all `test_*` functions discovered)
- Tests run in isolation — each test is a separate invocation
- No test classes needed — top-level functions only

#### Assert Builtins

| Zinc | C# Emit | Behavior |
|------|---------|----------|
| `assert(condition)` | `if (!condition) throw new Exception("Assertion failed")` | Fails test with message |
| `assert(condition, message)` | `if (!condition) throw new Exception(message)` | Fails with custom message |
| `assertEqual(actual, expected)` | `if (!actual.Equals(expected)) throw new Exception(...)` | Fails with "expected X, got Y" |
| `assertNotEqual(actual, expected)` | `if (actual.Equals(expected)) throw new Exception(...)` | Fails if equal |
| `assertContains(haystack, needle)` | `if (!haystack.Contains(needle)) throw ...` | String/collection contains |

#### CLI

```bash
zinc test                    # run all tests in current project
zinc test math_test.zn       # run tests in specific file
zinc test -f test_addition   # run specific test function
zinc test -v                 # verbose — print each test name
```

#### Output

```
$ zinc test
Running 4 tests...

  PASS  test_addition (2ms)
  PASS  test_division (1ms)
  PASS  test_division_by_zero (3ms)
  FAIL  test_string_operations (2ms)
        math_test.zn:15 — expected 5, got 4

3 passed, 1 failed (8ms)
```

#### How It Works

1. `zinc test` scans project for all `test_*` functions
2. Generates a test harness `.cs` file that calls each test in a try/catch
3. Compiles and runs via `dotnet run` (not AOT — speed over binary size for tests)
4. Parses output, formats results

Generated test harness (internal, user never sees):

```csharp
public class Program
{
    public static void Main(string[] args)
    {
        var tests = new List<(string name, Action fn)>
        {
            ("test_addition", () => TestAddition()),
            ("test_division", () => TestDivision()),
        };

        int passed = 0, failed = 0;
        foreach (var (name, fn) in tests)
        {
            try
            {
                var sw = System.Diagnostics.Stopwatch.StartNew();
                fn();
                sw.Stop();
                Console.WriteLine($"  PASS  {name} ({sw.ElapsedMilliseconds}ms)");
                passed++;
            }
            catch (Exception ex)
            {
                Console.WriteLine($"  FAIL  {name}");
                Console.WriteLine($"        {ex.Message}");
                failed++;
            }
        }
        Console.WriteLine($"\n{passed} passed, {failed} failed");
        if (failed > 0) Environment.Exit(1);
    }
}
```

### Implementation

| Step | File | Change |
|------|------|--------|
| 1 | `internal/codegen_csharp/codegen.go` | Add `assert`, `assertEqual`, `assertNotEqual`, `assertContains` builtins |
| 2 | `internal/project/test_csharp.go` | New file — test discovery (scan for `test_*` functions), harness generation, runner |
| 3 | `cmd/zinc/main.go` | Add `test` subcommand |
| 4 | `docs/builtins.md` | Document assert builtins |
| 5 | Unit tests | Assert builtins emit correctly |
| 6 | E2E tests | Full zinc test run with pass/fail scenarios |

### Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| `test_*` convention | Function name prefix | No new keywords. Java teams understand naming conventions (JUnit 3 used this). |
| No test classes | Top-level functions | Avoid ceremony. Tests are functions, not methods in a class. |
| `dotnet run` not AOT | Speed over binary size | Tests run in dev, not production. Compile speed matters more. |
| Assert builtins, not a library | Language-level | Tests are too important to require imports. `assert` should just work. |
| No `@Test` annotation | Convention over annotation | Keeps it simple. Annotations are for .NET interop, not Zinc features. |

---

## 4. `zinc add`

### Problem

Currently, adding a NuGet dependency requires manually editing `zinc.toml`. Enterprise projects use dozens of packages (AWS SDK, Serilog, JSON libraries, etc.). This should be one command.

### Design

#### Commands

```bash
zinc add Serilog                       # latest version
zinc add AWSSDK.SQS --version 3.7.0   # specific version
zinc add Serilog AWSSDK.S3 AWSSDK.SQS # multiple at once
zinc remove Serilog                    # remove dependency
zinc deps                              # list current dependencies
```

#### What `zinc add` Does

1. Queries NuGet API for the package: `https://api.nuget.org/v3-flatcontainer/{id}/index.json`
2. Resolves latest stable version (or specified version)
3. Adds entry to `zinc.toml` under `[dependencies]`
4. Prints confirmation: `Added Serilog 4.1.0`

#### What `zinc remove` Does

1. Removes entry from `zinc.toml` `[dependencies]`
2. Prints confirmation: `Removed Serilog`

#### What `zinc deps` Does

1. Reads `zinc.toml` and prints dependency table:

```
$ zinc deps
Dependencies:
  Serilog          4.1.0
  AWSSDK.SQS       3.7.0
  Newtonsoft.Json  13.0.3
```

#### zinc.toml Format

No change to existing format:

```toml
[dependencies]
"Serilog" = "4.1.0"
"AWSSDK.SQS" = "3.7.0"
```

### Implementation

| Step | File | Change |
|------|------|--------|
| 1 | `internal/config/config.go` | Add `AddDependency(name, version)`, `RemoveDependency(name)` methods |
| 2 | `internal/config/config.go` | Add `SaveToFile()` — write config back to zinc.toml |
| 3 | `internal/nuget/resolve.go` | New file — query NuGet API for latest version |
| 4 | `cmd/zinc/main.go` | Add `add`, `remove`, `deps` subcommands |
| 5 | Unit tests | Config add/remove, NuGet version resolution |

### NuGet Version Resolution

```go
// GET https://api.nuget.org/v3-flatcontainer/{package-id}/index.json
// Returns: { "versions": ["1.0.0", "2.0.0", "3.0.0-rc1", "3.0.0"] }
// Pick latest non-prerelease version
```

### Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| NuGet API directly | No `dotnet` CLI dependency | Faster, no .NET SDK needed for `zinc add` |
| Latest stable default | Skip prereleases | Enterprise teams want stability |
| Multiple packages in one command | `zinc add A B C` | Batch is faster, common pattern |
| No lock file (Phase 1) | Rely on NuGet restore | Lock files are important but not foundational |

---

## 5. Logging

### Problem

Enterprise teams need structured logging. `print()` is for debugging. Production code needs log levels, context, and structured output.

### Design

#### Builtins

```zinc
main() {
    log.info("server started on port {port}")
    log.warn("connection pool low: {available} remaining")
    log.error("request failed: {err}")
    log.debug("processing item {id}")
}
```

#### Structured Fields

```zinc
log.info("user login", userId: 42, region: "us-east-1")
log.error("payment failed", orderId: "abc-123", amount: 99.99)
```

#### Log Levels

| Zinc | C# Emit | When |
|------|---------|------|
| `log.debug(msg)` | `Console.Error.WriteLine($"DEBUG: {msg}")` | Development |
| `log.info(msg)` | `Console.Error.WriteLine($"INFO: {msg}")` | Normal operation |
| `log.warn(msg)` | `Console.Error.WriteLine($"WARN: {msg}")` | Potential issues |
| `log.error(msg)` | `Console.Error.WriteLine($"ERROR: {msg}")` | Failures |

Phase 1 emits to stderr with level prefix. This keeps it simple, works in Lambda (CloudWatch captures stderr), and doesn't require any NuGet dependency.

#### Structured Output (Phase 1)

Plain text with key-value pairs appended:

```
INFO: user login [userId=42, region=us-east-1]
ERROR: payment failed [orderId=abc-123, amount=99.99]
```

#### JSON Output (Phase 2 — later)

When structured logging demand emerges:

```json
{"level":"info","msg":"user login","userId":42,"region":"us-east-1","timestamp":"2026-03-18T10:30:00Z"}
```

### Implementation

| Step | File | Change |
|------|------|--------|
| 1 | `internal/parser/parser.go` | Parse `log.info(...)`, `log.warn(...)` etc. as builtin calls — `log` is a reserved namespace, not a variable |
| 2 | `internal/codegen_csharp/codegen.go` | Emit `Console.Error.WriteLine(...)` with level prefix |
| 3 | Named args in log calls | Parse `key: value` pairs after the message string |
| 4 | `docs/builtins.md` | Document logging functions |
| 5 | Unit + E2E tests | All log levels, structured fields |

### Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| `log.info()` not `logInfo()` | Dot notation | Reads naturally, groups log functions visually, familiar from every logging framework |
| stderr not stdout | `Console.Error` | Separates log output from program output. Lambda/CloudWatch captures stderr. |
| No external dependency | Built-in | Logging is too fundamental to require `zinc add Serilog`. |
| Phase 1 plain text | Simple | JSON logging can come later. Plain text works for development and CloudWatch. |
| Named args for fields | `log.info("msg", key: value)` | Consistent with Zinc's named arg syntax. No new concepts. |

---

## Implementation Order

| Order | Feature | Effort | Dependencies |
|-------|---------|--------|-------------|
| 1 | `use` keyword | Small | None — lexer/parser change |
| 2 | Destructuring | Small | None — parser/codegen, half done |
| 3 | Logging | Small | None — builtin emission |
| 4 | `zinc test` | Medium | Assert builtins, test harness generation |
| 5 | `zinc add` | Medium | NuGet API, config file write-back |

Features 1-3 are independent and could be done in parallel. Feature 4 depends on 1-3 being stable (tests will use all of them). Feature 5 is independent.

---

## What This Enables

After these five features, a Zinc developer can:

```zinc
use json

data User(pub Int id, pub String name, pub String email)

(User, String) fetchUser(Int id) {
    var response = httpGet("https://api.example.com/users/{id}") or {
        return (User(0, "", ""), err)
    }
    var user = jsonDecode<User>(response)
    log.info("fetched user", userId: id, name: user.name)
    return (user, "")
}

main() {
    var (user, err) = fetchUser(42)
    if err != "" {
        log.error("failed to fetch user: {err}")
        exit(1)
    }
    print("{user.name} — {user.email}")
}
```

Test it:

```zinc
// user_test.zn

test_fetch_user_formats_name() {
    var user = User(1, "Alice", "alice@example.com")
    assertEqual("{user.name} — {user.email}", "Alice — alice@example.com")
}

test_fetch_user_handles_error() {
    var (user, err) = fetchUser(-1) or {
        assert(err != "")
        return
    }
}
```

Add dependencies:

```bash
zinc add AWSSDK.SQS
zinc add Serilog
zinc test
zinc build
```

This is the foundation for the service/Lambda layer that comes next.
