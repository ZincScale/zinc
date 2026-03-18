# Zinc v2 Roadmap

Typed Python with explicit blocks. Transpiles `.zn` → `.py`.

---

## Completed (v2.0-dev)

- [x] Lexer: v2 tokens (end, try, catch, raise, not, and, from, none, **)
- [x] Parser: end-block syntax, fn keyword, colon return types, script mode
- [x] Python codegen: full transpilation pipeline
- [x] CLI: `zinc run`, `zinc transpile`
- [x] Data classes → `@dataclass`
- [x] Enums → `enum.Enum`
- [x] Classes with inheritance, auto-self injection, dunder mapping
- [x] Two-track error handling: Result[T] / Err {} + try/catch/end
- [x] and/or/not, not in, is not, none
- [x] Expression if (condition-first ternary)
- [x] Lambdas (x -> expr)
- [x] Comprehensions (auto list/generator), dict comprehensions
- [x] Collection methods (.filter, .map, .sum, .sort_by, etc.)
- [x] Decorators (@cache, @staticmethod, @classmethod)
- [x] *args / **kwargs, default args
- [x] yield / generator functions
- [x] Triple-quote multi-line strings
- [x] Nested functions
- [x] del statement, assert statement
- [x] raise X from Y (exception chaining)
- [x] Single-quote strings (literal, no interpolation)
- [x] Smart string quoting in output
- [x] with/end context managers
- [x] from x import a, b (consolidated)
- [x] ** power operator
- [x] data keyword usable as variable name
- [x] 104 v2 tests (parser + codegen)

## In Progress

- [ ] **Type checker** — enforce types at transpile time (the #1 differentiator)
- [ ] **Smart collection dispatch** — Polars for structured, NumPy for numeric

## Next

- [ ] Tuple literals (1, 2 as expression)
- [ ] async / await
- [ ] Chained comparisons (0 < x < 10)
- [ ] Inherited field auto-self (needs symbol table)
- [ ] zinc fmt (formatter)
- [ ] Union types (str | int)
- [ ] @property decorator support
- [ ] Source maps for error messages

## Explorations

- [Dagster](docs/exploration-dagster-pipelines.md) — batch pipeline orchestration
- [Pathway](docs/exploration-pathway-pipelines.md) — real-time streaming
- [PyFlink](docs/exploration-pyflink-pipelines.md) — enterprise stream processing

## v1 Archive

v1 (C# AOT + Go backends) is archived in `docs/v1-archive/` and `examples/v1-archive/`.
