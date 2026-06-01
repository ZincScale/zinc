# Python→Go compiler (zincpy) — work plan

Last updated: 2026-06-01. Branch: `python-to-go-compiler`.

## What this is

A Python→Go compiler: valid (type-annotated) CPython in, native Go out, **byte-identical
runtime behavior** vs CPython. Front-end lives in `internal/pyfront/` and reuses the
existing Zinc typechecker + Go codegen. The contract is: supported features compile and
match CPython exactly; unsupported features fail to compile with a clear error (never a
silent divergence). Zinc itself is abandoned — edit shared codegen/typechecker freely.

**Design assumption (important):** target code is **mypy-strict + Pydantic = fully
annotated**. Optimize for annotated code; unannotated/duck-typed routes through dynamic
runtime helpers (`zincpy*`) but is the secondary path.

## Current status

- 54 "spikes" done, all byte-identical to CPython (contract test green).
- Coverage of **idiomatic annotated everyday Python ≈ 83%** (measured 2026-06-01).
  Core arithmetic, strings, classes, comprehensions, exceptions basics, iterators
  (genexpr), most builtins all working.
- The long tail is a short list of bounded fixes + ~5 sizable features (below) +
  stdlib breadth (reachable today via CPython FFI `import X`).

## How to work

From `compilers/zinc-go/`:

```bash
go build ./...                                  # build
go run ./cmd/zincpy file.py                     # compile + run a .py
go run ./cmd/zincpy --emit file.py              # print generated Go
go test ./cmd/zincpy/                            # CONTRACT TEST: every testdata/spike*.py
                                                 #   vs CPython, must be byte-identical
diff <(python3 f.py) <(go run ./cmd/zincpy f.py) # quick one-off check
```

`python3` here is the 3.14 venv at `/home/vrjoshi/.venv`; the FFI links the matching
libpython automatically (see `pythonConfigTool` in `cmd/zincpy/main.go`).

**Workflow for each feature:** add a `testdata/spikeNN_<name>.py`, make it byte-identical,
keep the whole contract suite green, commit. One feature cluster per commit.

**GOTCHA:** `internal/pyfront/runtime.go` is `const RuntimeGo = \`...\`` — a backtick
raw-string literal. NEVER put a backtick anywhere inside it (comments included) — it
silently terminates the literal. (Hit this twice now.)

## Key files

- `internal/pyfront/parser.go` — lexing-driven recursive-descent parser; statement &
  expression lowering. Precedence chain: parseTernary → parseOr → … → parseAdditive →
  parseMultiplicative → parseUnary → parsePower → parsePostfix → parseAtom.
- `internal/pyfront/infer.go` — `pytype` (tInt/tFloat/tStr/tBool/tDynamic/tUnknown),
  `typeOf`, `callType` (return-type registry for zincpy* helpers — ADD new helpers here),
  `numericBinary`.
- `internal/pyfront/class.go` — class/method parsing, dunders.
- `internal/pyfront/exceptions.go` — try/except/finally/raise lowering.
- `internal/pyfront/comprehension.go` — list/dict/set comprehensions + genexpr.
- `internal/pyfront/strings.go` — f-strings (`zincpyFormat`), str-method lowering.
- `internal/pyfront/runtime.go` — the emitted Go runtime (`zincpy*` helpers). Backtick
  raw-string — see gotcha.
- `cmd/zincpy/main.go` — driver, FFI cgo flags.

---

# TODO — prioritized

Effort: S = <½ day, M = ~1 day, L = multi-day.

## Tier 1 — quick wins (bounded, high frequency)

- [ ] **One-line compound suites** `def f(): return x`, `if c: foo()`, `class E: pass`,
      `try: ... except: ...` on one line. **(M, HIGHEST leverage)** — the parser requires
      NEWLINE+INDENT after every `:`. Real-world code uses the inline form constantly, and
      it silently inflates "failure" counts. Fix in `parseBlock` (or wherever a suite is
      read after `:`): if the next token is not NEWLINE, parse a single simple statement
      (or `;`-separated list) as the body. Touches def/class/if/while/for/with/try/except/
      else/finally. Test: re-run the terse survey in `/tmp/survey/` — should jump sharply.

- [ ] **`list.sort()` / `list.reverse()` / `list.pop()` / `list.insert()` / `list.index()`
      / `list.count()`** **(S)** — currently `xs.sort()` → `xs.Sort undefined`. `.sort()`
      mutates in place; lower like `.append` (statement rewrite) using `sort.Slice`, or to a
      runtime helper. `sort(key=, reverse=)` kwargs too. See the `.append` rewrite in
      `parseSimpleStmt` (parser.go ~570) for the in-place-mutation pattern.

- [ ] **Tuple-unpack from a stored tuple value** `a, b, c = t` where `t` is a
      `zincpyTuple`. **(S)** — currently "assignment mismatch: 3 vars but 1 value". In
      `parseUnpackAssign`, when the RHS is a single expr that's a tuple/dynamic, emit
      `a, b, c = zincpyGetItem(t,0), zincpyGetItem(t,1), zincpyGetItem(t,2)`.

- [ ] **`sorted(xs, key=len)` / bare-builtin key** **(S)** — "len (built-in) must be
      called". A bare builtin name as a value (function reference) isn't supported. Either
      special-case common keys (len/str/abs) into a lambda, or make bare builtin idents
      usable as first-class func values.

- [ ] **`map`/`filter` over a `range(...)` argument** **(S)** — `filter(f, range(6))` →
      "unexpected keyword range". `range(...)` as a call argument (not loop iterable) isn't
      lowered to an iterable value. Make `range(...)` in value position produce a
      materialized list / `zincpyRange` runtime iterable.

- [ ] **Nested-list element typing** `m = [[1,2],[3,4]]; m[1][0]` **(S/M)** — `m[1]` types
      as `interface{}` so the second index fails. Track element types of list-of-list
      literals (recurse in `recordElemType` / `elemType`).

- [ ] **str predicate methods** `isalpha/isdigit/isalnum/isspace/isupper/islower/
      startswith-already-done` **(S)** — add to `pyStrMethods` map + runtime helpers +
      `zincpyStrMethod` dispatcher (all in strings.go/runtime.go; return tBool in callType).

- [ ] **`for c in s` yields a 1-char str, not a Go rune** **(S/M)** — `for c in "abc":
      c.upper()` fails because `c` is a Go `rune`. String iteration should bind each char as
      a 1-char string (Python semantics). Fix in `parseFor` string-iteration path.

## Tier 2 — medium features

- [ ] **`from X import name`** **(M)** — currently rejected (use `import X` + `X.name`).
      For FFI modules, lower `from math import sqrt` to bind `sqrt` → `zincpyPyCall("math",
      "sqrt", ...)`. For compile-time modules (dataclasses/typing) it's already name-aware.
      Big ergonomic win for stdlib. Start in the import parsing + `ffiModBind`.

- [ ] **`**kwargs`** **(M)** — currently rejected. Lower to a trailing
      `kwargs map[string]any` / `*zincpyDict` param; calls collect leftover `name=value`
      into it. Pairs with the existing `*args` support.

- [ ] **walrus `:=`** **(M)** — assignment expression. Lower `if (n := len(x)) > 3:` to a
      pre-statement `n := len(x)` hoist + use `n`. Needs an expression-context statement
      hoist (similar to how `as`-casts hoist in codegen).

- [ ] **Custom exception subclasses** `class E(Exception): pass` + `raise E(msg)` +
      `except E` **(M)** — user exception classes aren't modeled (Exception base undefined).
      Map a user class deriving Exception/a builtin-exc to a `zincpyExc`-compatible value,
      register it in the `zincpyExcParents` chain at runtime, and route raise/except.

- [ ] **decorators on plain functions** `@deco def f(): ...` **(M)** — top-level function
      decorators (class decorators/@property/@dataclass already work). Lower `@deco` to
      `f = deco(f)` after the def.

- [ ] **`-> None` audit / void-method polish** — DONE for returns; verify methods, lambdas,
      and `__init__` paths all treat None as void everywhere.

- [ ] **`__repr__` without `__str__` fallback** **(S)** — a class with only `__repr__`
      prints Go's `&{..}` instead of using Repr. Python's `__str__` defaults to `__repr__`:
      when a class defines `__repr__` (Repr) but not `__str__` (String), emit a `String()`
      that calls `Repr()`. In class.go dunder handling.

## Tier 3 — big features (the real "what's left")

- [ ] **Generators / `yield`** **(L, highest-value big item)** — `def g(): yield x`.
      Two options: (a) goroutine + channel per generator (simple, idiomatic-ish, has
      teardown concerns); (b) compile the function to a state machine struct implementing a
      `Next() (any, bool)` iterator. Recommend starting with (a) behind a `zincpyGen`
      runtime type, supporting `for x in g()` and `list(g())`. Also `next()` / `iter()`.

- [ ] **`match` statement** **(L)** — structural pattern matching. Start with literal +
      capture + class patterns; lower to an if/else chain. Sequence/mapping patterns later.

- [ ] **`async`/`await`** **(L, maybe out of scope)** — goroutines/channels or defer.

- [ ] **bignum > int64** **(L, maybe out of scope)** — Python ints are unbounded; we use
      int64. `big.Int` opt-in if needed. Document the limitation otherwise.

## Cross-cutting

- [ ] **Re-run the coverage survey** after each cluster: regenerate `/tmp/surv` (idiomatic
      annotated) and `/tmp/survey` (terse — measures the one-line-suite fix). Track the %.
- [ ] **stdlib breadth** is reachable via FFI `import X` today (math/json/re/itertools/
      collections all work through libpython). `from X import` (Tier 2) makes it ergonomic.

## Suggested order for tomorrow

1. **One-line compound suites** (Tier 1) — unblocks the most real-world code in one shot.
2. **list.sort + tuple-unpack + str predicates + nested-list typing** — fast cluster,
   knocks out 4 survey failures.
3. Then pick **generators (`yield`)** as the first big feature, or **from-import** for
   stdlib ergonomics.
