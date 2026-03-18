# Zinc v2 Roadmap

Typed Python with explicit blocks. Transpiles `.zn` → `.py`.

---

## Completed (v2.0-dev)

- [x] Lexer: v2 tokens (try, catch, raise, not, and, from, none, **)
- [x] Parser: brace-block syntax `{ }`, fn keyword, colon return types, script mode
- [x] Python codegen: full transpilation pipeline
- [x] CLI: `zinc run`, `zinc transpile`, `--optimize polars`
- [x] Data classes → `@dataclass`
- [x] Enums → `enum.Enum`
- [x] Classes with inheritance, auto-self injection (including inherited fields), dunder mapping
- [x] Two-track error handling: Result[T] / Err + try/catch
- [x] and/or/not, not in, is not, none
- [x] Expression if (condition-first ternary)
- [x] Lambdas (x -> expr), *args/**kwargs, default args
- [x] Comprehensions (auto list/generator), dict comprehensions
- [x] Collection methods (.filter, .map, .sum, .sort_by, etc.)
- [x] Smart dispatch: single method → comprehension, chains → _zinc_collect() runtime
- [x] `--optimize polars` → Polars lazy frame pipelines at transpile time
- [x] Decorators (@cache, @staticmethod, @classmethod, @property)
- [x] yield / generator functions, nested functions
- [x] Tuple literals (1, 2, 3), return a, b
- [x] del, assert, with context managers
- [x] raise X from Y (exception chaining)
- [x] Single-quote strings (literal), double-quote (interpolation), triple-quote (multi-line)
- [x] ** power operator, match/case
- [x] Type checker: catches type mismatches, undefined variables at transpile time
- [x] Source maps: Python errors show .zn file and line numbers
- [x] `data` keyword: contextual — works as variable name in all contexts
- [x] super().__init__(**kwargs) auto-generated for child classes
- [x] from x import a, b (consolidated on one line)
- [x] 115+ v2 tests (parser + codegen + type checker)

## Next

- [ ] Zinc Flow — lightweight NiFi-inspired flow processing (see design doc)
- [ ] Chained comparisons (`0 < x < 10`)
- [ ] async / await
- [ ] zinc fmt (formatter)
- [ ] zinc repl for v2

## Design Docs

- [Zinc Flow](docs/design-zinc-flow.md) — NiFi replacement, processor graph processing
- [v2 Design](docs/design-zinc-v2-python.md) — language philosophy and decisions
- [Known Limitations](docs/v2-limitations.md) — what's not yet implemented

## v1 Archive

v1 (C# AOT + Go backends) is archived in `docs/v1-archive/` and `examples/v1-archive/`.
