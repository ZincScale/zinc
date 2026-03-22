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
| [Error Handling](lang/error-handling.md) | `Result<T>`, `Error`, `or` handler, `or match`, `return Error()` |
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
&&  ||  not

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
- `fn main()` → explicit entry point, generates `main(String[] args)`
- Multiple `data` declarations → each generates a separate record `.java` file
- Multiple `enum` declarations → each generates a separate enum `.java` file
- Multiple `class` declarations → each generates a separate `.java` file

## Packages and Imports

Directory structure = package. No `package` declaration needed.

```zinc
// src/models/user.zn → package models (automatic)
data User(String name, int age)
```

```zinc
// src/main.zn → root package, auto-imports project types
var u = User("Alice", 30)
```

**Import rules:**
- Project types: auto-imported across packages
- Wildcard: `import models.*` → resolves to specific types
- External: `import java.time.Instant` — pass-through
- Auto-imported: `java.util.*`, `java.util.stream.*`

## Arrays

```zinc
var int[] nums = [1, 2, 3]       // array declaration
var String[] names = ["a", "b"]  // string array
print(nums[0])                   // array access
print(nums.length)               // array length

// In function signatures
fn process(int[] data) int {
    return data.length
}
```

Context-dependent: `int[] x = [1,2,3]` creates an array, `var x = [1,2,3]` creates a List.

## CLI

```bash
zinc init [name]              # scaffold a new project
zinc build <file.zn|dir>      # transpile + compile (Mill if project, javac if script)
zinc build --native <dir>     # GraalVM native binary via Mill
zinc build --docker <dir>     # native binary + Dockerfile
zinc build --k8s <dir>        # Docker + K8s manifest
zinc run <file.zn|dir>        # transpile + compile + run
zinc fmt <file.zn>            # format source code
zinc repl                     # interactive REPL
zinc update                   # update toolchain (GraalVM, Mill, Quarkus)
```
