# Validated Lowering Spec — imperative C-family → Erlang/OTP

**Verdict: GO.** Every `skin`/`remap` idiom lowers to Erlang correctly (**36/36** behavioral
cases pass), the control-flow primitive the whole design rests on (`throw`/`catch`) is **free**,
the differentiator (supervised self-heal) is correct and **microsecond-fast**, and indexed
data structures have a clean **flat-cost** path while the O(n²) traps are avoidable by
construction. This document is the codegen blueprint for the eventual transpiler.

Validated on **OTP 29 + BeamAsm JIT** (official `erlang:slim` image), no local install.

## Reproduce
```
cd beam-lab
./run.sh lowering/control_flow.erl      ./run.sh bench/bench_loops.erl
./run.sh lowering/arrays.erl            ./run.sh bench/bench_arrays.erl
./run.sh lowering/data.erl              ./run.sh bench/bench_throwcatch_isolation.erl
./run.sh lowering/polymorphism.erl
./run.sh -m lowering/otp 'otp_demo:main()'     # self-heal demo
./run.sh -m lowering/otp 'otp_bench:main()'    # B3 ceremony benchmark
```

## Behavioral correctness — 36/36 pass
| File | Cases | Covers |
|---|---|---|
| `control_flow.erl` | 11 | if/switch→case, accumulator loop→fold/recursion, while+multi-mutation→threaded recursion, break/continue→recursion, early-return→throw, labeled break→throw |
| `arrays.erl` | 10 | array-module + map value-semantics; ETS + atomics in-place mutation |
| `data.erl` | 10 | record & map structs + methods, functional field update, strings→binaries, null→Option |
| `polymorphism.erl` | 5 | sealed interface→tagged-tuple dispatch, open interface→vtable |

## Validated lowering matrix
| Idiom | Tag | Lowering (Erlang) | Status |
|---|---|---|---|
| if/else, switch | skin | `case` (+ guards) | ✅ |
| for-each | skin | direct recursion | ✅ |
| while + mutation | skin | tail-recursive helper, mutated vars as args | ✅ |
| mutable locals / accumulator | skin | SSA → accumulator threaded through recursion | ✅ |
| break / continue (single level) | skin | direct recursion (stop / skip) | ✅ |
| break / early-return (nested, labeled) | skin | `throw` → `catch` (unwinds all levels) | ✅ free |
| early return (mid-fn) | skin | wrap body in `try`, `throw {ret,V}` at each return | ✅ free |
| functions / closures | skin | funs; multi-clause = overloading | ✅ |
| struct + method | skin | record/map + `f(P)`; field set = functional update | ✅ |
| array read/update (default) | remap | **`array` module** (value semantics) | ✅ flat |
| mutable buffer | remap | **atomics** (ints) / **ETS** (general), in-place | ✅ O(1) |
| strings | remap | **binaries** (never charlist) | ✅ |
| null | remap | **Option** `{some,V} | none` (no null) | ✅ |
| interface (sealed) | remap | tagged tuple + multi-clause dispatch | ✅ |
| interface (open) | remap | vtable (map of funs) / `apply` | ✅ |
| process / send / receive | distinct | `spawn` / `!` / `receive` | ✅ |
| supervised actor | distinct | `gen_server` + `supervisor` (default) | ✅ self-heals |

## Benchmark results
**Throw/catch (control-flow core) — effectively free**
- `try` wrapper on the non-throwing path: **+2.6 ns/call** (flat; only wrap fns that early-return).
- exit-via-throw vs normal return: **1.00×** (identical). Unwinding 50 frames ≈ a normal return.
- the only expensive shape was `lists:foreach` closure-per-element (**7.4×**) → see loop rule.

**B1 — loops: direct recursion vs `lists:foldl`**
- numeric range sum (10M): recursion **17.6 ms** vs foldl 35.6 ms → **2.02×**.
- over existing 10M list: recursion **19.2 ms** vs foldl 35.6 ms → closure tax **1.85×**.

**B2 — array tiers (ns/op, N = 1k → 1M)**
| op | tuple | array mod | atomics | map | ets |
|---|---|---|---|---|---|
| read | 13 (flat) | 25→33 | 41→53 | 20→**131** | 45→162 |
| update | O(n) trap | 44→**250** flat | **16 (flat)** | 51→**1750** | 58→184 |

Traps (why not to back arrays naively): `lists:nth` read **965→6510 ns** (O(n)); tuple
`setelement` update **426 ns → 1.35 ms** at 1M (O(n) → O(n²) in a loop).

**B3 — OTP ceremony (the differentiator)**
- raw process spawn: **0.83 µs/proc**
- gen_server (actor) start: **5.1 µs/server**
- supervisor restart (crash→serving again): **avg 16.5 µs** (min 10 / max 1082 µs)
- → self-heal is **~5–6 orders of magnitude faster than a Kubernetes pod restart (seconds).**

## Codegen rules (confirmed / refined by the data)
1. **Target = Erlang** (Core Erlang / abstract forms). Dynamic, permissive, direct OTP.
2. **Loops → direct recursion, never `lists:foldl`/`foreach`** (1.85–2.02× + avoids the 7× closure tax).
3. **break / continue / early-return → `throw`/`catch`** for non-local exits; it's free. Wrap a
   function body in `try` **only when it contains an early return/break** (skip the 2.6 ns otherwise).
4. **Default immutable array → the `array` module, NOT maps** (maps degrade to 1.75 µs/update at 1M;
   array module stays ~250 ns). Maps remain fine for sparse/keyed data.
5. **Mutable `Buffer` → atomics** (numeric, 16 ns flat O(1)) **/ ETS** (general terms). Never back an
   indexed array with a list or with tuple-`setelement` (O(n) → silent O(n²)).
6. **Strings → binaries**, always. **null → Option**, baked into the surface (the one thing worth
   stealing from Gleam regardless of target).
7. **Supervised-by-default**: actors lower to `gen_server` under a `supervisor`; the boilerplate in
   `lowering/otp/` is exactly what a one-keyword `actor` surface should generate.

## Forbid list (compile errors — never lower, never let them *look* like they work)
All trip the litmus test *assumes shared mutable memory*: pointer/reference aliasing; mutable struct
fields shared across scope/process; static/global mutable variables; locks / mutexes / semaphores;
threads / async-await with shared memory (use processes); closures mutating captured outer variables;
OOP inheritance / mutable objects; a mutable array aliased across processes.

## Go / No-Go
**GO.** The load-bearing bet holds: imperative C-family lowers to Erlang/OTP **cleanly** (36/36)
and **efficiently** (throw free; loops fast via recursion; arrays flat via array-module/atomics; the
self-heal differentiator is microsecond-scale). No idiom in the `skin`/`remap` set lacks a clean,
fast lowering; the `forbid` set is exactly the shared-mutable-memory constructs that break BEAM's
reliability anyway.

### Next (post-gate, not this milestone)
1. Decide the transpiler's implementation language (Go / Rust / Erlang).
2. Build a walking-skeleton emitter over a minimal AST — **this spec is its codegen contract**.
3. Build the low-ceremony-OTP surface (one-keyword `actor`, supervised-by-default) — the real
   product differentiator, whose target is already validated in `lowering/otp/`.
