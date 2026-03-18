# Testing

Zinc has built-in testing. No test framework to install, no annotations to learn. Write functions that start with `test_`, run `zinc test`.

## Writing Tests

Test functions start with `test_` and take no parameters:

```zinc
// math_test.zn

test_addition() {
    assertEqual(add(1, 2), 3)
}

test_string_greeting() {
    var msg = greet("Zinc")
    assertEqual(msg, "Hello, Zinc!")
}

test_list_operations() {
    var nums = [1, 2, 3]
    assert(nums.size() == 3)
    assertEqual(nums.Sum(), 6)
}
```

All types, functions, and classes defined anywhere in your project are automatically visible to test files. No imports needed.

## Assert Builtins

| Function | What it does | On failure |
|----------|-------------|------------|
| `assert(condition)` | Checks condition is true | "Assertion failed: ..." |
| `assert(condition, message)` | Checks with custom message | Your message |
| `assertEqual(actual, expected)` | Checks values are equal | "Expected X but got Y" |
| `assertNotEqual(actual, expected)` | Checks values differ | "Expected not equal to X" |

### Examples

```zinc
test_assertions() {
    // Boolean condition
    assert(1 + 1 == 2)

    // With custom message
    assert(users.size() > 0, "user list should not be empty")

    // Value equality
    assertEqual(add(2, 3), 5)

    // Not equal
    assertNotEqual(status, "error")
}
```

## Running Tests

```bash
zinc test                        # run all tests in current project
zinc test -v                     # verbose output
zinc test -f test_addition       # run a specific test function
zinc test path/to/project        # test a specific directory
```

### Output

```
$ zinc test
Running 4 test(s)...

  PASS  test_addition (0ms)
  PASS  test_addition_negative (0ms)
  PASS  test_greet (0ms)
  FAIL  test_should_fail
        math_test.zn — Expected 3 but got 2

3 passed, 1 failed
```

Exit code is `1` if any test fails, `0` if all pass. Works with CI pipelines.

## Error Handling in Tests

Use `or { }` to test error cases:

```zinc
Int divide(Int a, Int b) {
    if b == 0 {
        return Error("division by zero")
    }
    return a / b
}

test_divide_by_zero() {
    var result = divide(10, 0) or {
        assert(err == "division by zero")
        return
    }
    panic("expected error, got result")
}

test_divide_success() {
    var result = divide(10, 2) or {
        panic("unexpected error: {err}")
    }
    assertEqual(result, 5)
}
```

## Project Structure

Test files can live anywhere in your project. Convention is `*_test.zn` but any file with `test_*` functions works:

```
myapp/
  zinc.toml
  main.zn
  models/
    user.zn
  services/
    user_service.zn
  tests/
    user_test.zn
    service_test.zn
```

## How It Works

`zinc test` does the following:
1. Scans all `.zn` files for `test_*` functions
2. Transpiles everything to C#
3. Generates a test harness that calls each test in a try/catch
4. Runs via `dotnet run` (not AOT — compile speed matters for tests)
5. Reports results with timing
