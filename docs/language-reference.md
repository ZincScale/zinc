# Zinc v3 — Language Reference

Zinc is a convention-over-configuration JVM language. Write `.zn` files, transpile to Java 25.

## Language Topics

| Topic | Description |
|---|---|
| [Variables and Constants](lang/variables.md) | `var`, `const`, `init`, type inference, arrays, tuple unpacking |
| [Functions](lang/functions.md) | `fn`, parameters, return types, `fn main()`, lambdas, varargs, `it` keyword |
| [Classes](lang/classes.md) | Fields, methods, inheritance, data classes (records), enums, sealed types |
| [Control Flow](lang/control-flow.md) | `if`/`else`, `for`, `while`, `match`, `break`/`continue`, `with` |
| [Error Handling](lang/error-handling.md) | `Result<T>`, `Error`, `or` handler, `or match`, `return Error()` |
| [Collections](lang/collections.md) | `filter`, `map`, `sum`, `it` keyword, tuples, stream chains |
| [Type System](lang/types.md) | Built-in types, arrays, generics, nullable `Type?`, type narrowing |
| [Packages and Imports](lang/packages.md) | Directory-as-package, auto-imports, wildcard resolution |
| [Concurrency](lang/concurrency.md) | `spawn`, `parallel for`, `concurrent`, `lock`, `timeout`, virtual threads |
| [Strings](lang/strings.md) | Single-quote, double-quote, triple-quote, interpolation |

## File Structure

A `.zn` file can contain:
- Top-level statements → wrapped in `main()` (script mode)
- `fn main()` → explicit entry point (project mode)
- `fn` declarations → static methods
- `data`, `enum`, `class` declarations → separate `.java` files each

## Operators

```zinc
+ - * / % **                     // arithmetic
== !=                            // structural equality (Objects.equals)
=== !==                          // reference identity
< <= > >=                        // comparison
&& || not                        // boolean
in   not in                      // membership
is   is not                      // type check (instanceof)
null                             // null value
```

## CLI

```bash
zinc init [name]              # scaffold a new project
zinc build <file.zn|dir>      # transpile + compile (Mill if project, javac if script)
zinc build --native <dir>     # GraalVM native binary via Mill
zinc build --docker <dir>     # native binary + Dockerfile
zinc build --k8s <dir>        # Docker + K8s manifest
zinc run <file.zn|dir>        # transpile + compile + run
zinc add <dep>                # add Maven dependency
zinc remove <dep>             # remove dependency
zinc deps                     # list dependencies
zinc fmt <file.zn>            # format source code
zinc repl                     # interactive REPL
zinc update                   # update toolchain (GraalVM, Mill, Quarkus)
```
