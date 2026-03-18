# Zinc v2 — Known Limitations

Last updated: 2026-03-18

## What Works

Everything below is implemented, tested, and working end-to-end:

- Script mode (top-level statements, no main required)
- `fn` with colon return types, `end` blocks, `else if`
- `data` classes → `@dataclass`, `enum` → `enum.Enum`
- Classes with inheritance, auto-self injection, dunder mapping
- `@staticmethod`, `@classmethod`, general decorator pass-through
- Two-track error handling: `Result[T]` / `Err {}` + `try`/`catch`/`end`
- `raise X from Y` (exception chaining)
- `and`/`or`/`not`, `not in`, `is not`, `none`
- Expression if (condition-first ternary)
- Lambdas (`x -> x * 2`), `*args`/`**kwargs`, default args
- List/dict comprehensions (auto list vs generator promotion)
- Collection methods: `.filter()`, `.map()`, `.sum()`, `.sort_by()`, `.take()`, etc.
- Smart dispatch: single method → comprehension, chains → `_zinc_collect()` runtime
- `--optimize polars` → Polars lazy frame pipelines at transpile time
- Type checker: catches type mismatches, undefined variables at transpile time
- `yield` / generator functions, nested functions
- `del`, `assert`, `with`/`end` context managers
- `import`, `from x import a, b` (consolidated), single/double/triple-quote strings
- `**` power operator, `match`/`end`, `break`/`continue`
- CLI: `zinc run`, `zinc transpile`, `--optimize`, temp file cleanup

## Parser — Not Yet Implemented

- [ ] `data` keyword conflicts with variable names in some contexts
- [ ] No chained comparisons (`0 < x < 10` — parses but wrong semantics)
- [ ] No walrus operator (`:=` assignment expression)
- [ ] No `async` / `await`
- [ ] No `global` / `nonlocal` keywords
- [ ] No `type` aliases
- [ ] No star import (`from module import *`)
- [ ] No tuple literals (`(1, 2, 3)` as expression — use `[1, 2, 3]` for now)
- [ ] No `@property` decorator (use regular methods)

## Codegen — Not Yet Implemented

- [ ] Auto-self injection doesn't track inherited fields (use `self.field` explicitly)
- [ ] No `__init__` generation from inherited parent fields (no super() auto-gen)
- [ ] Nested quotes in string interpolation (`"{data["key"]}"` — use temp var)
- [ ] `match` emits Python 3.10+ syntax — no fallback for older Python
- [ ] No NumPy-specific `--optimize numpy` codegen (Polars works)
- [ ] No source map / line number tracking in generated .py
- [ ] Fast serialization builtins (json_load, csv_load) — use imports directly
- [ ] `.parallel_map()` — not implemented (use threads/multiprocessing directly)

## CLI — Not Yet Implemented

- [ ] No `zinc fmt` (formatter)
- [ ] No `zinc repl` for v2
- [ ] No shebang support (`#!/usr/bin/env zinc run`)

## Type System — Limitations

- [ ] Basic type inference only (int, str, float, bool, list, dict)
- [ ] No generic type constraints
- [ ] No union types (`str | int`)
- [ ] No Protocol support
- [ ] Function return type checking is limited (doesn't verify all paths return)
- [ ] No type narrowing after `isinstance` checks

## Design Doc Over-Promises (Not Yet Implemented)

These are in the design doc but not yet built:

- [ ] Auto-parallelization of `.map()` on large collections
- [ ] GIL-dependent library detection and warnings
- [ ] Free-threaded Python auto-dispatch
- [ ] Smart collection dispatch based on data shape (currently manual `--optimize` flag)
