# Zinc v2 — Known Limitations

Last updated: 2026-03-18

## What Works

Everything below is implemented, tested, and working end-to-end:

- Script mode (top-level statements, no main required)
- `fn` with colon return types, `{ }` brace blocks, `else if`
- `data` classes → `@dataclass`, `enum` → `enum.Enum`
- Classes with inheritance, auto-self injection (including inherited fields), dunder mapping
- `@staticmethod`, `@classmethod`, `@property`, general decorator pass-through
- Two-track error handling: `Result[T]` / `Err` + `try`/`catch`
- `raise X from Y` (exception chaining)
- `and`/`or`/`not`, `not in`, `is not`, `none`
- Expression if (condition-first ternary)
- Lambdas (`x -> x * 2`), `*args`/`**kwargs`, default args
- Tuple literals `(1, 2, 3)`, `return a, b`
- List/dict comprehensions (auto list vs generator promotion)
- Collection methods: `.filter()`, `.map()`, `.sum()`, `.sort_by()`, `.take()`, etc.
- Smart dispatch: single method → comprehension, chains → `_zinc_collect()` runtime
- Smart data shape dispatch: list[dict]→Polars, list[numeric]→NumPy (auto-install if needed)
- Free-threaded Python by default (GIL disabled), auto-parallelize `.map()` on 1000+ items
- GIL-dependent library warnings at transpile time
- Type checker: mismatches, return types, arg types, all-paths-return, type narrowing
- `is` type checks: `x is str` → isinstance, `x is none` → identity, with type narrowing
- Source maps: Python errors show .zn file and line numbers
- `yield` / generator functions, nested functions
- `del`, `assert`, `with` context managers
- `import`, `from x import a, b` (consolidated), single/double/triple-quote strings
- Nested string interpolation: `"{data["key"]}"` works
- Shebang: `#!/usr/bin/env zinc run`
- `**` power operator, `match`, `break`/`continue`
- `data` is a contextual keyword — fully usable as variable name
- `spawn { }` background threads, `parallel for` thread pool, `with lock { }` critical sections
- CLI: `zinc run`, `zinc transpile`, `zinc fmt`, `zinc repl`, `zinc pack`
- `zinc pack`: PyInstaller, Nuitka, Docker, K8s — all with free-threaded Python
- Auto-generated requirements.txt from imports (polars/numpy always included)

## Parser — Not Yet Implemented

- [x] ~~`data` keyword conflicts~~ — contextual keyword, fully usable as variable name
- [ ] No chained comparisons (`0 < x < 10` — parses but wrong semantics)
- [ ] No walrus operator (`:=` assignment expression)
- [ ] No `async` / `await`
- [ ] No `global` / `nonlocal` keywords
- [ ] No `type` aliases
- [ ] No star import (`from module import *`)
- [x] ~~No tuple literals~~ — implemented: `(1, 2, 3)`, `return a, b`
- [x] ~~No `@property`~~ — works via decorator pass-through

## Remaining Limitations

### Parser
- [ ] No chained comparisons (`0 < x < 10`)
- [ ] No walrus operator (`:=`)
- [ ] No `async` / `await`
- [ ] No `global` / `nonlocal`
- [ ] No `type` aliases
- [ ] No star import (`from module import *`)

### Codegen
- [ ] `match` emits Python 3.10+ syntax — no fallback for older Python

### Type System
- [ ] No generic type constraints
- [ ] No Protocol support
