# Zinc v2 — Known Limitations

Track issues to fix before v2 is production-ready.

## Parser

- [ ] `data` keyword conflicts with variable names (can't use `data` as identifier)
- [ ] No multi-line string literals (triple quotes)
- [ ] No chained comparisons (`0 < x < 10`)
- [ ] No walrus operator (`:=` assignment expression)
- [ ] No `yield` / generator functions
- [ ] No `async` / `await`
- [ ] No `global` / `nonlocal` keywords
- [ ] No `del` statement
- [ ] No `assert` statement
- [ ] No `type` aliases
- [ ] No decorators beyond `@asset`/`@op` (need general decorator pass-through)
- [ ] No nested function definitions
- [ ] `from x import a, b` — only imports single name, not multiple
- [ ] No star import (`from module import *`)
- [ ] No `@staticmethod` / `@classmethod` / `@property`
- [ ] `print` is a special statement, not a regular function (can't do `print(a, b, sep=", ")`)

## Codegen

- [ ] Auto-self injection doesn't track inherited fields
- [ ] No `__init__` generation for inherited class fields (super() calls)
- [ ] Comprehension inside string interpolation breaks (nested quotes)
- [ ] `match` emits Python 3.10+ `match/case` — no fallback for older Python
- [ ] Collection method chains (.filter().map()) use simple comprehension — no Polars/NumPy dispatch yet
- [ ] No source map / line number tracking for error messages
- [ ] `raise` doesn't support `raise X from Y` syntax

## CLI

- [ ] `zinc run` leaves generated .py file in working directory
- [ ] No `zinc check` (type checking without running)
- [ ] No `zinc fmt` (formatter)
- [ ] No `zinc repl` for v2
- [ ] No `--target python3.x` version flag
- [ ] No shebang support (`#!/usr/bin/env zinc run`)

## Type System

- [ ] No type checker for v2 (v1 type checker doesn't understand v2 syntax)
- [ ] No generic type constraints
- [ ] No union types (`str | int`)
- [ ] No Protocol support
