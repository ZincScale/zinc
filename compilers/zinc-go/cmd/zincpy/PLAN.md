# Python→Go compiler (zincpy) — work plan

Last updated: 2026-06-02. Branch: `python-to-go-compiler`.

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

- 64 "spikes" done, all byte-identical to CPython (contract test green).
- Coverage of **idiomatic annotated everyday Python ≈ 83%** (measured 2026-06-01).
  Core arithmetic, strings, classes, comprehensions, exceptions basics, iterators
  (genexpr), most builtins all working.
- The long tail is a short list of bounded fixes + ~5 sizable features (below) +
  stdlib breadth (reachable today via CPython FFI `import X`).

## Strategy (confirmed 2026-06-02)

The goal is **parity for idiomatic mypy/Pydantic-strict, fully-annotated Python
syntax** — NOT the whole stdlib. Anything outside that runs via FFI into CPython,
which is acceptable because the heavy libraries we'd reach for (pandas, numpy, …)
are native-C under the hood, so the per-call FFI overhead is amortized over real
C work. **Priority: convert as much Python *syntax* to the native-Go path as
possible** — that's where the speedups live (see Performance below) and it shrinks
the FFI surface.

## Performance (measured 2026-06-02)

Native path (annotated → pure Go), standalone binary vs CPython 3.14, best-of-3:

| workload                         | speedup vs CPython |
|----------------------------------|--------------------|
| 30M-iter arithmetic loop         | **94×**            |
| float math (2M ops)              | **59×**            |
| recursive fib                    | **18×**            |
| string concat + len (2M)         | **3.1×**           |
| FFI `math.sqrt` ×2M (slow path)  | **0.13× (7× slower)** |

Takeaways: compute-bound annotated code is **18–95× faster** as native Go; the
FFI fallback is ~7× *slower* than plain CPython (cgo + GIL per call), so it's a
correctness net, not a perf path — every construct we move onto the native path
is a direct win. **Strings are the weak spot (only 3.1×)** because the string
runtime boxes/allocates — see the string-runtime TODO below.

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

- [x] **One-line compound suites** `def f(): return x`, `if c: foo()`, `class E: pass`,
      `try: ... except: ...` on one line. **DONE 2026-06-02** (spike55). `parseBlock` now
      detects a non-NEWLINE token after `:` and parses an inline suite; `parseLineInto`
      drives one logical line of `;`-separated simple statements and is reused by indented
      blocks + the module top level (so `a=1; b=2` works everywhere). Class bodies got the
      same treatment via a shared `parseItem` closure (class.go). Lexer learned `;`. Covers
      def/if/elif/else/for/while/with/try/except(-as)/finally + inline class `pass`.

- [~] **`list.sort()` / `list.reverse()` / `list.insert()` / `list.index()` /
      `list.count()`** **DONE 2026-06-02** (spike57). Mutators lower to `xs = helper(xs,...)`
      via `lowerListMutator` (parser.go, next to `.append`); pure queries via
      `lowerListMethod` in `parseCall`. Generic runtime helpers (`zincpySort[T cmp.Ordered]`,
      `zincpyInsert`, `zincpyListIndex`/`Count`). `sort(reverse=True)` supported (literal
      bool); `sort(key=...)` rejected loudly. Element-comparison sort requires an orderable
      element type (int/float/str) — a list of objects without key= fails to compile (ok).

- [x] **`list.pop()`** **DONE 2026-06-02** (spike64). Built the **statement-hoist buffer**
      (`p.pending`, flushed in `parseLineInto` before each statement) — the infra path (b).
      `hoistPop` (parser.go) emits two hoisted statements — `__pop := zincpyPopVal(xs[,i])`
      (value-read, raises IndexError on empty/out-of-range) then `xs = zincpyPopDrop(xs[,i])`
      (write-back, uses `=` so it mutates the right var even in a nested Go block) — and
      returns the temp, so pop works in EVERY position (bare stmt, `x = xs.pop()`,
      `print(xs.pop())`, `t += xs.pop()`, two pops in one expr). A bare-statement pop discards
      the value (keeps the side-effecting check). Bonus fix: an annotated `list[T] = []`
      now pins its element type (so list methods/iteration work on empty-initialized lists).
      Known limit: pop in a loop CONDITION would hoist once (the body idiom `while xs:
      x = xs.pop()` is exact). **The hoist buffer now also unblocks walrus `:=` cheaply.**

- [x] **Tuple-unpack from a stored tuple value** `a, b, c = t`. **DONE 2026-06-02**
      (spike58). In `parseUnpackAssign`, a non-call single-expr RHS expands to
      `zincpyGetItem(t, i)` per target (reusing the parallel-assign path); a function CALL
      stays on the Go multi-return path (`a, b := f()`). Targets type as dynamic.

- [x] **`sorted(xs, key=len)` / bare-builtin key** **DONE 2026-06-02** (spike62).
      `builtinKeyFn` (parser.go) rewrites a bare builtin key — len/str/abs/int/float — into
      the lambda it stands for (`lambda __k: zincpyLen(__k)`), so it becomes a real
      func(any) any. Lambdas / other keys are left as-is. (A user function name as key is
      still unsupported — a separate first-class-func-value gap.)

- [x] **`map`/`filter`/`list`/`sum`/... over a `range(...)` argument** **DONE 2026-06-02**
      (spike61). `parseCall` lowers `range(...)` in value position to `zincpyRangeList(...)`
      (a materialized []int, with start/stop/step + negative step). `asRange` also recognizes
      the lowered form, so `for i in range(n)` keeps its optimized numeric loop; a 3-arg
      range iterates the slice. Bonus: 3-arg/negative-step ranges now work in for-loops too.

- [x] **Nested-list element typing** `m = [[1,2],[3,4]]; m[1][0]` **DONE 2026-06-02**
      (spike59). Fixed in codegen: `inferListLitElemType` (codegen_stmts.go) now recurses on
      list-literal elements, so an unannotated list-of-lists types as `[][]int` instead of
      `[]interface{}` (annotated form already worked). Inner types must agree, else `any`.

- [x] **str predicate methods** `isalpha/isdigit/isalnum/isspace/isupper/islower/istitle`
      **DONE 2026-06-02** (spike56). Added to `pyStrMethods` + the `zincpyStrMethod`
      dispatcher + tBool in callType; runtime helpers use `unicode.*` (byte-identical to
      CPython for ASCII and the common cases; exotic Unicode Numeric_Type is approximated).

- [x] **`for c in s` yields a 1-char str, not a Go rune** **DONE 2026-06-02** (spike60).
      `parseFor` now detects a string iterable and ranges `zincpyChars(s)` (a `[]string` of
      length-1 chars, split by code point), binding the loop var as `str` so `c.upper()`,
      `c.isdigit()`, `c in vowels`, etc. all work. Unicode-correct (one char per code point).

## Tier 2 — medium features

- [x] **`from X import name`** **DONE 2026-06-02** (spike63). `parseFromImport` binds each
      imported name (with `as` aliases) into `ffiFromBind` → (mod, attr); a call `sqrt(x)`
      lowers to `zincpyPyCall("math","sqrt",x)` (parseCall) and a bare value `pi` to
      `zincpyPyGet` (parseAtom, when not immediately called). `from X import *` and dotted
      modules are rejected loudly; compile-time modules (typing/dataclasses) stay name-checked
      no-ops. NOTE: like any FFI result, an imported call returns a dynamic value — narrow it
      at an annotated boundary (`n: int = factorial(6)`) before storing into a typed list/var
      (same pre-existing dynamic→typed gap as `import X`; not specific to from-import).

- [ ] **`**kwargs`** **(M)** — currently rejected. Lower to a trailing
      `kwargs map[string]any` / `*zincpyDict` param; calls collect leftover `name=value`
      into it. Pairs with the existing `*args` support.

- [ ] **walrus `:=`** **(S now — infra exists)** — assignment expression. The statement-hoist
      buffer (`p.pending`, flushed in `parseLineInto`) is already built for `list.pop()`, so
      lowering `if (n := len(x)) > 3:` is now just: parse `(name := expr)`, hoist a
      `name := expr` VarStmt into `p.pending`, declare `name`, and return `Ident{name}`. Use
      `hoistPop` as the template. CAVEAT (same as pop): a walrus in a loop CONDITION
      (`while (line := read()):`) would hoist once, not per-iteration — detect a walrus in a
      while/for cond and either reject loudly or re-hoist into the loop body. Statement-position
      and if-condition walrus are exact.

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

## Performance TODOs

- [ ] **String-runtime perf** **(M)** — string-heavy code is only ~3.1× faster than
      CPython (vs 18–95× for numeric code; see Performance above). The string path
      boxes/allocates through `zincpy*` helpers instead of using Go `string` directly.
      Investigate: keep statically-`str`-typed values as native Go `string` end-to-end
      (concat → `+`, `len` → `len()`, indexing → native) and only fall back to the boxed
      runtime for dynamic receivers. Biggest remaining native-path win. Profile `strs`
      benchmark first (2M concat+len loop) to find the hot allocation.
- [ ] **Flag FFI hot paths** **(S)** — FFI calls are ~7× slower than CPython; a construct
      in a hot loop that routes through FFI is a silent perf cliff. Consider a
      `--warn-ffi-in-loop` diagnostic (or note in `--emit`) so users know when they've
      left the native path.

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

## Documentation (do when close to 100%)

- [ ] **Developer guide: using FFI + the dynamic-value boundary** — a docs page with
      worked examples. FFI calls (`import X`, `from X import name`) return a DYNAMIC value
      (boxed `any`); the only friction is crossing dynamic → static. Document the three
      narrowing patterns, each with a runnable example:
      1. annotated assignment — `x: float = sqrt(2.0)` (coerces at the boundary);
      2. explicit conversion — `int(...) / float(...) / str(...) / bool(...)` inline;
      3. stay dynamic end-to-end — don't annotate; arithmetic/print route through the
         dynamic helpers (correct, just slower).
      Call out the gotcha that bites most: a `list[T]` is a real Go slice, so a raw FFI
      result must be narrowed PER ELEMENT before append / inside a comprehension
      (`vals.append(int(randint(...)))`, `[float(sqrt(float(d))) for d in data]`). Frame the
      mental model as a "border crossing": past an FFI call you're in dynamic territory; an
      annotation or `int()/float()/str()` is the passport back to typed Go; the compiler only
      complains on a silent crossing. Also cover: error timing differs from CPython (name
      errors surface at use, not import), and that heavy native-C libs (numpy/pandas) are the
      intended FFI use — their results are meant to stay opaque and flow back into more FFI.
      Pair with the two ergonomic improvements noted below if they land first.
- [ ] **(optional, ergonomics) auto-narrow FFI into typed containers** — when a `list[T]`
      append / typed assignment receives a dynamic FFI call, auto-insert the `T(...)`
      coercion so `vals.append(randint(...))` just works; and improve the compile error to
      name the fix ("narrow with int(...) or annotate") instead of Go's raw
      "need type assertion". Reduces how much the FFI doc above has to caveat.

## Suggested order

1. ~~One-line compound suites~~ — **DONE 2026-06-02** (spike55).
2. ~~Tier-1 quick wins~~ — **DONE 2026-06-02** (spikes 55–64): one-line suites, str
   predicates, list methods (incl. `pop` via the statement-hoist buffer), tuple-value unpack,
   nested-list typing, string iteration, range-as-value, sorted(key=builtin). **Tier 1 is
   fully cleared.** First Tier-2 item (`from X import`) also done (spike63).
3. **walrus `:=`** is now cheap (the hoist buffer exists) — good next quick win. Then the big
   features: **generators (`yield`)**, or Tier-2 breadth (`**kwargs`, custom exceptions,
   function decorators).
4. **String-runtime perf** (Performance TODOs) when ready to chase the native-path
   speedup on string-heavy code — currently the weakest multiplier (3.1×).
