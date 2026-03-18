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
- [x] ~~No `assert` statement~~ — implemented
- [ ] No `type` aliases
- [x] ~~No decorators~~ — general decorator pass-through implemented
- [ ] No nested function definitions
- [x] ~~`from x import a, b`~~ — multiple names implemented (but generates separate lines)
- [ ] No star import (`from module import *`)
- [x] ~~No `@staticmethod` / `@classmethod` / `@property`~~ — staticmethod/classmethod implemented
- [x] ~~`print` is a special statement~~ — now a regular function call

## Codegen (Critical — design doc promises these)

- [ ] **Result[T] / Err {} error handling** — core design feature, zero implementation
- [ ] **Smart collection dispatch** — Polars/NumPy/Numba tiers not implemented, only comprehensions
- [ ] **Fast serialization builtins** — json_load(), csv_load(), avro_load() not implemented
- [ ] **.parallel_map()** — free-threaded parallel dispatch not implemented
- [ ] `from x import a, b` generates separate lines instead of consolidated
- [ ] `.filter()` with lambdas generates awkward `(lambda x: ...)(x)` pattern

## Codegen (Other)

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
