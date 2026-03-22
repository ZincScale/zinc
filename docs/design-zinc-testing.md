# Design: Zinc Testing Framework

> **Status**: DESIGN
> **Context**: Every Java project needs testing. JUnit 5 requires annotations, imports, assertion imports, and ceremony. Zinc should make testing as natural as writing code — convention over configuration.

## Philosophy

- **Zero imports** — test files just work
- **Zero annotations** — `test "name" { }` not `@Test void name()`
- **Assert is built-in** — no `import static org.junit.jupiter.api.Assertions.*`
- **Transpiles to JUnit 5** — Mill runs tests natively, CI/CD just works
- **Convention**: files in `test/` are test files, files ending in `_test.zn` are test files

## Syntax

### Basic Tests

```zinc
// test/calculator_test.zn

test "adds two numbers" {
    assert add(1, 2) == 3
}

test "subtracts two numbers" {
    assert subtract(5, 3) == 2
}

test "handles division by zero" {
    var result = divide(10, 0) or -1
    assert result == -1
}
```

Transpiles to:

```java
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

public class CalculatorTest {
    @Test
    void adds_two_numbers() {
        assertEquals(3, Calculator.add(1, 2));
    }
    // ...
}
```

### Assertions

Zinc's `assert` generates descriptive failure messages:

```zinc
assert x == 3                    // assertEquals(3, x)
assert x != 0                    // assertNotEquals(0, x)
assert x > 0                     // assertTrue(x > 0)
assert items.size() == 3         // assertEquals(3, items.size())
assert name == "Alice"           // assertEquals("Alice", name)
assert result == null            // assertNull(result)
assert result != null            // assertNotNull(result)
assert items.contains("Bob")    // assertTrue(items.contains("Bob"))
```

Failure output:

```
FAIL: adds two numbers
  assert add(1, 2) == 3
  expected: 3
  actual:   4
```

### Data-Driven Tests

Parameterized tests with inline data:

```zinc
test "max of two numbers" with [
    [1, 2, 2],
    [3, 1, 3],
    [5, 5, 5],
    [-1, 0, 0],
] { a, b, expected ->
    assert max(a, b) == expected
}
```

Transpiles to JUnit 5 `@ParameterizedTest` + `@MethodSource`.

### Test Groups

Group related tests with shared setup/teardown:

```zinc
test group "user service" {
    var db = TestDb()

    before {
        db.reset()
    }

    after {
        db.close()
    }

    test "creates user" {
        var user = User("Alice", 30)
        db.save(user)
        assert db.count("users") == 1
    }

    test "finds user by name" {
        db.save(User("Bob", 25))
        var found = db.findByName("Bob") or null
        assert found != null
        assert found.name() == "Bob"
    }
}
```

Transpiles to JUnit 5 `@Nested` class with `@BeforeEach` / `@AfterEach`.

### Skip and Focus

```zinc
skip test "not ready yet" {
    assert false
}

// Run only this test (strip before commit)
focus test "debugging this" {
    assert calculate() == 42
}
```

`skip` → JUnit `@Disabled`. `focus` → run filter (Mill integration).

## CLI

```bash
zinc test                     # run all tests (mill test)
zinc test test/user_test.zn   # run specific test file
zinc test --filter "user"     # run tests matching pattern
```

`zinc test` delegates to `mill test` which uses JUnit 5 under the hood.

## Project Structure

```
myapp/
  build.mill.yaml
  src/
    main.zn
    models/
      user.zn
  test/
    user_test.zn              # tests for user.zn
    calculator_test.zn        # tests for calculator
```

Convention:
- Test files live in `test/` directory
- Test files end with `_test.zn`
- Test files can import project types (auto-resolved like src/)
- `zinc init` already creates `test/` directory

## Dependencies

`zinc init` should include JUnit 5 in the generated `build.mill.yaml`:

```yaml
extends: [JavaModule, NativeImageModule]
jvmVersion: 25

mvnDeps: []

testMvnDeps:
  - org.junit.jupiter:junit-jupiter:5.11.0
```

Or better — Zinc auto-includes JUnit when test files exist. Zero config.

## Transpilation Mapping

| Zinc | JUnit 5 |
|---|---|
| `test "name" { }` | `@Test void name() { }` |
| `test "name" with [...] { }` | `@ParameterizedTest @MethodSource` |
| `test group "name" { }` | `@Nested class Name { }` |
| `before { }` | `@BeforeEach void setUp() { }` |
| `after { }` | `@AfterEach void tearDown() { }` |
| `skip test` | `@Disabled @Test` |
| `assert x == y` | `assertEquals(y, x)` |
| `assert x != null` | `assertNotNull(x)` |
| `assert x > 0` | `assertTrue(x > 0)` |
| `zinc test` | `mill test` |

## What We Skip (For Now)

- **Mocking** — use Mockito via `zinc add org.mockito:mockito-core:5.x`. Don't reinvent.
- **given/when/then BDD** — adds words without value for most tests
- **@Shared state** — too niche, use test group `before` instead
- **Async test helpers** — use Zinc's `concurrent {}` / `timeout` directly
- **Coverage** — Mill has JaCoCo integration, use it directly

## Implementation Plan

### Phase 1 — Basic Tests
- Parser: `test "name" { body }` as a top-level declaration
- Codegen: transpile to JUnit 5 `@Test` methods
- CLI: `zinc test` → `mill test`
- Assert improvements: better failure messages

### Phase 2 — Data-Driven + Groups
- Parser: `test "name" with [...] { args -> body }`
- Parser: `test group "name" { before/after/tests }`
- Codegen: `@ParameterizedTest`, `@Nested`, `@BeforeEach`/`@AfterEach`

### Phase 3 — DX
- `skip test`, `focus test`
- Test filtering: `zinc test --filter "user"`
- Better assertion failure messages with expression evaluation
