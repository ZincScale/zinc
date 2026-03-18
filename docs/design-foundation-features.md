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

`import "System.Text.Json"` used Go-style string imports. Zinc no longer targets Go. The quotes added noise and suggested file paths, but these are .NET namespaces.

### Design

```zinc
// Before (deprecated)
import "System.Text.Json"
import "http"
import "Newtonsoft.Json" as nj

// After (current)
use System.Text.Json
use http
use Newtonsoft.Json as nj
```

Rules:
- `use` replaces `import` for .NET namespace imports
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

Enterprise teams need structured logging. `print()` is for debugging. Production code needs log levels, context, and structured output that routes to CloudWatch, OpenSearch, files, etc.

Java teams are used to SLF4J + Logback with `logback.xml` controlling everything. Zinc needs the same power with zero C# logging concepts exposed.

### Design

#### Zinc Code — Simple

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

| Zinc | Serilog Emit | When |
|------|-------------|------|
| `log.debug(msg)` | `Log.Debug(msg)` | Development |
| `log.info(msg)` | `Log.Information(msg)` | Normal operation |
| `log.warn(msg)` | `Log.Warning(msg)` | Potential issues |
| `log.error(msg)` | `Log.Error(msg)` | Failures |

#### Configuration via `zinc.toml`

This is the Logback equivalent. Developer configures logging in `zinc.toml`, Zinc generates the Serilog setup. No `appsettings.json`, no `ILogger`, no DI plumbing.

**Default (zero config):**

If no `[logging]` section exists, Zinc emits console logging at `info` level. Just works.

```
2026-03-18 10:30:00 [INF] server started on port 8080
2026-03-18 10:30:01 [WRN] connection pool low: 2 remaining
```

**Basic config:**

```toml
[logging]
level = "debug"
format = "json"
```

**Full config — per-class levels, multiple outputs:**

```toml
[logging]
level = "info"
format = "text"

[logging.levels]
UserService = "debug"
DataLayer = "error"
HttpClient = "warn"

[logging.console]
enabled = true
format = "text"

[logging.file]
enabled = true
path = "logs/app.log"
rolling = "daily"
retain = 30

[logging.opensearch]
enabled = true
url = "https://search-myapp.us-east-1.es.amazonaws.com"
index = "app-logs-{0:yyyy.MM.dd}"
```

Compare to Logback (Java):

```xml
<!-- This is what your team writes today -->
<configuration>
  <appender name="CONSOLE" class="ch.qos.logback.core.ConsoleAppender">
    <encoder><pattern>%d{yyyy-MM-dd HH:mm:ss} [%level] %logger - %msg%n</pattern></encoder>
  </appender>
  <appender name="FILE" class="ch.qos.logback.core.rolling.RollingFileAppender">
    <file>logs/app.log</file>
    <rollingPolicy class="ch.qos.logback.core.rolling.TimeBasedRollingPolicy">
      <fileNamePattern>logs/app.%d{yyyy-MM-dd}.log</fileNamePattern>
      <maxHistory>30</maxHistory>
    </rollingPolicy>
    <encoder><pattern>%d{yyyy-MM-dd HH:mm:ss} [%level] %logger - %msg%n</pattern></encoder>
  </appender>
  <logger name="com.myapp.service.UserService" level="DEBUG"/>
  <logger name="com.myapp.data" level="ERROR"/>
  <root level="INFO">
    <appender-ref ref="CONSOLE"/>
    <appender-ref ref="FILE"/>
  </root>
</configuration>
```

12 lines of TOML vs 18 lines of XML. Same power.

#### C# Mapping

Zinc generates the Serilog setup code in the program entry point. The developer never sees it.

`zinc.toml`:
```toml
[logging]
level = "info"

[logging.file]
enabled = true
path = "logs/app.log"
rolling = "daily"
```

Generated C#:
```csharp
using Serilog;

Log.Logger = new LoggerConfiguration()
    .MinimumLevel.Information()
    .WriteTo.Console()
    .WriteTo.File("logs/app.log", rollingInterval: RollingInterval.Day)
    .CreateLogger();
```

Zinc code:
```zinc
log.info("user login", userId: 42, region: "us-east-1")
```

Generated C#:
```csharp
Log.Information("user login {@UserId} {@Region}", 42, "us-east-1");
```

Note: Serilog uses `@` prefix for structured property destructuring — Zinc handles this automatically from the named args.

#### Serilog Sinks Mapped to zinc.toml

| zinc.toml section | Serilog Sink (NuGet) | Use case |
|---|---|---|
| `[logging.console]` | Built-in | Always available |
| `[logging.file]` | `Serilog.Sinks.File` | Local log files |
| `[logging.opensearch]` | `Serilog.Sinks.OpenSearch` | OpenSearch/Elasticsearch |
| `[logging.cloudwatch]` | `Serilog.Sinks.AwsCloudWatch` | AWS CloudWatch Logs |
| `[logging.seq]` | `Serilog.Sinks.Seq` | Centralized log server |

When a sink is enabled in `zinc.toml`, `zinc build` automatically adds the required NuGet package to the generated `.csproj`. The developer never runs `zinc add Serilog.Sinks.File` — it's inferred from config.

#### Auto-Dependency Management

If `zinc.toml` has a `[logging]` section (or if any `log.*` call exists in code):
- `Serilog` is auto-added as a dependency
- `Serilog.Sinks.Console` is auto-added
- Additional sinks added based on `[logging.*]` sections

The developer's `[dependencies]` section stays clean — logging deps are managed by the compiler.

### Implementation

| Step | File | Change |
|------|------|--------|
| 1 | `internal/parser/parser.go` | Parse `log.info(...)`, `log.warn(...)` etc. as builtin calls — `log` is a reserved namespace, not a variable |
| 2 | `internal/codegen_csharp/codegen.go` | Emit Serilog calls: `Log.Information(...)`, `Log.Warning(...)`, etc. |
| 3 | `internal/codegen_csharp/codegen.go` | Emit structured fields: named args → Serilog message template properties |
| 4 | `internal/config/config.go` | Parse `[logging]`, `[logging.levels]`, `[logging.console]`, `[logging.file]`, etc. |
| 5 | `internal/codegen_csharp/codegen.go` | Generate Serilog `LoggerConfiguration` setup from config |
| 6 | `internal/project/build_csharp.go` | Auto-add Serilog NuGet packages to `.csproj` based on config |
| 7 | `docs/builtins.md` | Document logging functions |
| 8 | Unit + E2E tests | All log levels, structured fields, config-driven setup |

### Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Serilog underneath | Industry standard | Battle-tested, structured logging, 100+ sinks, AOT compatible |
| `log.info()` not `logInfo()` | Dot notation | Reads naturally, familiar from every logging framework (SLF4J, Logback, Python, etc.) |
| Config in `zinc.toml` | Single config file | No `appsettings.json`, no Serilog API to learn. One file for everything. |
| Auto-add Serilog deps | Inferred from config/usage | Developer never manually adds logging packages. Convention over configuration. |
| Named args for fields | `log.info("msg", key: value)` | Consistent with Zinc's named arg syntax. Maps to Serilog message templates. |
| Default console at info | Zero config works | `log.info("hello")` works without any `[logging]` section. |
| Per-class log levels | `[logging.levels]` | Matches Logback's `<logger name="..." level="..."/>` — enterprise teams expect this. |

---

## Implementation Order

| Order | Feature | Effort | Dependencies |
|-------|---------|--------|-------------|
| 1 | `use` keyword | Small | None — lexer/parser change |
| 2 | Destructuring | Small | None — parser/codegen, half done |
| 3 | `zinc add` | Medium | NuGet API, config file write-back |
| 4 | `zinc test` | Medium | Assert builtins, test harness generation |
| 5 | Logging | Medium | Config parsing, Serilog codegen, auto-dependency injection |

Features 1-2 are quick wins. Feature 3 (`zinc add`) before logging because logging's auto-dependency management builds on the same NuGet infrastructure. Feature 4 (`zinc test`) is independent. Feature 5 (logging) is the most complex — config parsing, Serilog setup generation, sink auto-detection.

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
