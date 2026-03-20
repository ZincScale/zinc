# Zinc v3 — Language Reference

## Blocks

Blocks use `{ }` braces. Indentation is for readability only.

```zinc
fn example() {
    if true {
        print("yes")
    }
}
```

## Language Topics

| Topic | Description |
|---|---|
| [Variables and Constants](lang/variables.md) | `var`, `const`, `init`, type inference, initialization, tuple unpacking |
| [Functions](lang/functions.md) | `fn`, parameters, return types, single-expression, lambdas, varargs, default and named args |
| [Classes](lang/classes.md) | Fields, methods, inheritance, auto-this, method mapping, annotations, data classes (records), enums |
| [Control Flow](lang/control-flow.md) | `if`/`else`, expression if, `for`, `while`, `match`, `break`/`continue` |
| [Error Handling](lang/error-handling.md) | `Result<T>`, `Err`, `Err` handler blocks, `try`/`catch`, `raise from` |
| [Collections](lang/collections.md) | `filter`, `map`, `sum`, `it` keyword, tuples |
| [Type System](lang/types.md) | Type checking, type safety errors, type narrowing, generics with `<>`, nullable `Type?` |
| [Concurrency](lang/concurrency.md) | `spawn`, `parallel for`, `concurrent`, `lock`, `timeout`, `context`, virtual threads |
| [Strings](lang/strings.md) | Single-quote, double-quote, triple-quote, interpolation |

## Operators

```zinc
// Arithmetic
+ - * / % **

// Structural equality (Kotlin convention)
== !=                        // Objects.equals() — same values

// Reference identity
=== !==                      // same object in memory

// Comparison
< <= > >=

// Boolean
and  or  not

// Membership
in   not in

// Type check
is   is not                  // instanceof

// Null
null
```

## Imports

```zinc
import java.util.List
import java.nio.file.Path
import java.time.Instant
```

## Assert

```zinc
assert x > 0, "x must be positive"
assert items.size() > 0
```

## Try-with-Resources

```zinc
with f = FileReader("data.txt") {
    var String content = f.readLine()
}
// f is automatically closed
```

Transpiles to Java try-with-resources:
```java
try (var f = new FileReader("data.txt")) {
    String content = f.readLine();
}
```

## File Structure

A single `.zn` file can contain:
- Top-level functions → `static` methods in a class named after the file
- Top-level statements → wrapped in `main()`
- Multiple `data` declarations → each generates a separate record `.java` file
- Multiple `enum` declarations → each generates a separate enum `.java` file
- Multiple `class` declarations → each generates a separate `.java` file

## CLI

```bash
zinc build <file.zn>          # transpile to .java + compile with javac
zinc run <file.zn>            # transpile + compile + run
zinc fmt <file.zn>            # format source code
zinc repl                     # interactive REPL
```
