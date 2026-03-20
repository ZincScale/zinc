# Zinc v2 — Language Reference

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
| [Functions](lang/functions.md) | `fn`, parameters, return types, single-expression, lambdas, `*args`/`**kwargs`, default and named args, generators |
| [Classes](lang/classes.md) | Fields, methods, inheritance, auto-self, dunder mapping, decorators, `@staticmethod`, `@classmethod`, `@property`, data classes, enums |
| [Control Flow](lang/control-flow.md) | `if`/`else`, expression if, `for`, `while`, `match`, `break`/`continue` |
| [Error Handling](lang/error-handling.md) | `Result<T>`, `Err`, `Err` handler blocks, `try`/`catch`, `raise from` |
| [Collections](lang/collections.md) | `filter`, `map`, `sum`, comprehensions, smart dispatch, tuples |
| [Type System](lang/types.md) | Type checking, type safety errors, type narrowing, generics with `<>`, nullable `Type?` |
| [Concurrency](lang/concurrency.md) | `spawn`, `parallel for`, `with lock`, free-threaded Python |
| [Strings](lang/strings.md) | Single-quote, double-quote, triple-quote, interpolation |

## Operators

```zinc
// Arithmetic
+ - * / % **

// Comparison
== != < <= > >=

// Boolean
and  or  not

// Membership
in   not in

// Type check / Identity
is   is not                  // type check or identity based on rhs

// None
none
```

## Imports

```zinc
import json
import os.path
from pathlib import Path
from os.path import join, exists, basename
```

## Assert

```zinc
assert x > 0, "x must be positive"
assert len(items) > 0
```

## Delete

```zinc
var dict<str, str> config = {"host": "localhost", "secret": "abc123"}
del config["secret"]
```

## Context Managers

```zinc
with f = open("data.txt") {
    var str content = f.read()
}
// f is automatically closed
```

## Shebang

```zinc
#!/usr/bin/env zinc run
print("directly executable!")
```

```bash
chmod +x script.zn
./script.zn
```

## CLI

```bash
zinc run script.zn                    # transpile + run (free-threaded Python)
zinc run script.zn -- arg1 arg2       # pass args to script
zinc transpile script.zn              # output .py file
zinc transpile script.zn -o out.py    # specify output path
zinc fmt script.zn                    # format source code
zinc pack script.zn                   # package with PyInstaller
zinc pack script.zn --format nuitka   # compile to native binary (30-50% faster)
zinc pack script.zn --format docker   # generate Dockerfile
zinc pack script.zn --format k8s      # Dockerfile + K8s manifest
zinc pack myproject/                  # package entire project directory
zinc repl                             # interactive REPL
```

All `zinc run` and `zinc pack` use free-threaded Python (GIL disabled) by default. `PYTHON_GIL=0` is set in generated Dockerfiles and K8s manifests.
