# Zinc v2 Roadmap

Typed Python with explicit blocks. Transpiles `.zn` → `.py`. Free-threaded Python by default.

---

## Completed (v2.0-dev)

### Language
- [x] Brace-block syntax `{ }`, `fn` keyword, colon return types, script mode
- [x] Data classes → `@dataclass`, enums → `enum.Enum`
- [x] Classes with inheritance, auto-self injection (including inherited fields), dunder mapping
- [x] `@staticmethod`, `@classmethod`, `@property`, general decorator pass-through
- [x] Two-track error handling: `Result[T]` / `Err` + `try`/`catch`
- [x] `raise X from Y` (exception chaining)
- [x] `and`/`or`/`not`, `not in`, `is not`, `none`
- [x] `is` type checks: `x is str` → `isinstance()`, `x is none` → identity
- [x] Expression if (condition-first ternary)
- [x] Lambdas (`x -> expr`), `*args`/`**kwargs`, default args
- [x] Tuple literals `(1, 2, 3)`, `return a, b`
- [x] Comprehensions (auto list/generator), dict comprehensions
- [x] Collection methods (`.filter`, `.map`, `.sum`, `.sort_by`, etc.)
- [x] `yield` / generator functions, nested functions
- [x] `del`, `assert`, `with` context managers
- [x] Single-quote (literal), double-quote (interpolation), triple-quote (multi-line)
- [x] Nested string interpolation: `"{data["key"]}"`
- [x] `**` power operator, `match`/`case`, `break`/`continue`
- [x] `data` contextual keyword — fully usable as variable name
- [x] Shebang: `#!/usr/bin/env zinc run`

### Type System
- [x] Type mismatches: `var x: int = "hello"` → error
- [x] Return type verification: all code paths must return
- [x] Function call arg type and count checking
- [x] Type narrowing after `is` checks
- [x] `break`/`continue` outside loop detection
- [x] Undefined variable detection
- [x] GIL-dependent library warnings at transpile time

### Smart Dispatch
- [x] Single method → inline comprehension (zero overhead)
- [x] Chained methods → `_zinc_collect()` runtime
- [x] Auto data shape detection: `list[dict]` → Polars, `list[numeric]` → NumPy
- [x] Auto-install polars/numpy on first use if not installed
- [x] Free-threaded auto-parallelize: `.map()` on 1000+ items uses ThreadPoolExecutor
- [x] `spawn { }` — background thread, returns Future
- [x] `parallel for` — process items across thread pool (8.5x speedup measured)
- [x] `with lock { }` — thread-safe critical sections

### CLI & Tooling
- [x] `zinc run` — free-threaded Python by default (finds python3.14t)
- [x] `zinc transpile` — output .py file
- [x] `zinc fmt` — format source code
- [x] `zinc repl` — interactive REPL with multi-line support
- [x] `zinc pack` — PyInstaller binary
- [x] `zinc pack --format nuitka` — compiled native binary (30-50% faster)
- [x] `zinc pack --format docker` — Dockerfile with free-threaded Python from source
- [x] `zinc pack --format k8s` — Dockerfile + K8s deployment manifest
- [x] Project directory support: `zinc pack myproject/`
- [x] Auto-generated `requirements.txt` from imports (polars/numpy always included)
- [x] Source maps: Python errors show .zn file and line numbers
- [x] 115+ tests (parser + codegen + type checker)

## Next

### Type-first declaration syntax
Consistent `<type> <name>` ordering everywhere (Java/C#/Dart-style), replacing Python-style `<name>: <type>`.

**Variable declarations:**
- `int x = 5` — explicit type, mutable
- `var x = 5` — inferred type, mutable (`var` = type inference only)
- `const int x = 5` — explicit type, immutable
- `const x = 5` — inferred type, immutable

**Nullable types:**
- `int? x = none` — nullable, transpiles to `Optional[int]`
- `str? name = none` — safe navigation: `name?.upper()` short-circuits to `none`

**Function signatures:**
- `fn greet(str name) str` — return type after params, no arrow
- `fn greet(const str name) str` — param cannot be reassigned in body
- `fn find(str key) str?` — nullable return type

**Class fields:**
- `const str NAME = "default"` — compile-time constant with default
- `init str name` — set in constructor, frozen after
- `str name` — mutable field

**`const` vs `init`:**
- `const` — used everywhere (locals, params, class fields with defaults). Assign once, cannot reassign.
- `init` — class fields only. No default value, set in constructor, frozen after.

### Other language features

- [ ] Zinc Flow — lightweight NiFi-inspired flow processing (see design docs)
- [ ] `data` classes with methods — `data` auto-generates `__init__`, `__repr__`, `__eq__`, `__hash__`, `copy()` from fields; all fields frozen (immutable); methods and everything else work same as `class`. Transpiles to `@dataclass(frozen=True)` + `copy()` via `dataclasses.replace()`
- [ ] Dict merge with `+` operator — `a + b` returns new dict (Kotlin-style), transpiles to Python `a | b`
- [ ] Chained comparisons (`0 < x < 10`)
- [ ] async / await
- [ ] Generic type constraints
- [ ] Protocol support (interfaces for design-by-interface pattern)

## Docs

- [Getting Started](docs/getting-started.md) — install, hello world, key concepts
- [Language Reference](docs/language-reference.md) — complete syntax guide
- [Deployment Guide](docs/deployment.md) — Docker, K8s, PyInstaller, Nuitka, CI/CD
- [Design Doc](docs/design-zinc-v2-python.md) — philosophy and decisions
- [Zinc Flow](docs/design-zinc-flow.md) — NiFi replacement design
- [Known Limitations](docs/v2-limitations.md) — what's not yet done

## v1 Archive

v1 (C# AOT + Go backends) is archived in `docs/v1-archive/` and `examples/v1-archive/`.
