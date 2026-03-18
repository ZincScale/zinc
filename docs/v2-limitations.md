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
- Smart data shape dispatch: list[dict]→Polars, list[numeric]→NumPy (auto)
- Type checker: type mismatches, undefined variables, return types, arg types, break outside loop
- Source maps: Python errors show .zn file and line numbers
- `yield` / generator functions, nested functions
- `del`, `assert`, `with` context managers
- `import`, `from x import a, b` (consolidated), single/double/triple-quote strings
- Nested string interpolation: `"{data["key"]}"` works
- Shebang: `#!/usr/bin/env zinc run`
- `**` power operator, `match`, `break`/`continue`
- `data` is a contextual keyword — fully usable as variable name
- CLI: `zinc run`, `zinc transpile`, `zinc fmt`, `zinc repl`, temp file cleanup

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

## Codegen — Not Yet Implemented

- [x] ~~Auto-self doesn't track inherited fields~~ — fixed, registry resolves parents
- [x] ~~No super() auto-gen~~ — classes with parents generate super().__init__(**kwargs)
- [x] ~~Nested quotes in string interpolation~~ — `"{data["key"]}"` works
- [ ] `match` emits Python 3.10+ syntax — no fallback for older Python
- [x] ~~No source map / line number tracking~~ — errors show .zn file and line numbers
- [ ] Fast serialization builtins (json_load, csv_load) — use imports directly
- [ ] `.parallel_map()` — not implemented (use threads/multiprocessing directly)

## CLI — Not Yet Implemented

- [x] ~~No `zinc fmt`~~ — implemented, reformats with consistent indentation
- [x] ~~No `zinc repl`~~ — implemented, interactive with multi-line block support
- [x] ~~No shebang support~~ — `#!/usr/bin/env zinc run` works

## Type System — Limitations

- [x] ~~Function return type checking~~ — catches `return "hello"` when fn returns int
- [x] ~~Function call arg checking~~ — catches wrong arg types and counts
- [x] ~~break/continue outside loop~~ — caught at transpile time
- [ ] No generic type constraints
- [ ] No Protocol support
- [x] ~~Doesn't verify all code paths return~~ — catches missing returns in if/else/match
- [x] ~~No type narrowing~~ — `if x is str` narrows x in then-branch (generates isinstance)

## Design Doc — All Implemented

- [x] ~~Auto-parallelization of `.map()`~~ — ThreadPoolExecutor on 1000+ items when GIL disabled
- [x] ~~GIL-dependent library detection~~ — warns at transpile time for pandas, numba, etc.
- [x] ~~Free-threaded Python auto-dispatch~~ — detects `sys._is_gil_enabled()` at runtime
- [x] ~~Smart collection dispatch based on data shape~~ — list[dict]→Polars, list[numeric]→NumPy, auto
