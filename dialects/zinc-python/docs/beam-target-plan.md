# Surface mapping: braces-Python → BEAM

Status: BUILT (2026-06-17) — all decisions below implemented and green on BEAM via
`beam-transpiler/e2e-py.sh` (10/10). Frontend: `PyLexer` + `PyParser` + `PyInfer`
(~900 lines) emitting the existing Ast; `CodeGen`/`Resolve` untouched. Commits
db78933..604d6f1.

Working end-to-end on BEAM (e2e-py: 17 positive + 9 negative): entry/`def main`, locals +
control flow (`while`, `for-in-range`, `if/else if`), functions with return-type inference +
sibling calls, actors (`class C(Actor)`, cast/call from return type), constructors
(`def init`), supervision root (`class Main(Application)`) with crash/restart, protocols
(`interface` + `class X(Greeter)` + SAM `->`/`lambda` lambdas), FFI (`from erlang import m`),
Channel (bounded backpressure), typed locals (`x: T = e`), f-strings (`"{expr}"`),
try/except + raise + user exceptions (`class E(Exception) {}`, actor failure-relay),
records (`record Point(x: int, y: int)`, Pythonic `p.x`), enums (`Color.RED`),
match/case, list literals (`[a, b, c]`), Python `lambda` keyword, multi-file projects
(file -> eponymous class, `from util import mathutil` -> `Mathutil.fn(...)`).

Errors carry `<file>:<line>` (PyParser stamps statement lines); 9 negative tests assert
type mismatches (local/return/reassign/arg), parse errors, and structural errors.

Tooling: `zc new --py <name>` scaffolds a `.zn` project; the rebar_zinc compile provider
globs `**/*.{zinc,zn}`, so `zc build`/`run`/`release` and the installer all work for
braces-Python (the transpile step is the only surface-specific part).

NOTE: `CodeGen`/`Resolve` remained UNTOUCHED for the entire build — records/enums/match,
list literals, everything. The whole language is a frontend (`PyLexer`+`PyParser`+`PyInfer`).

NOT yet built (deferred): dict literals (`{k: v}`), whole-program param inference (untyped
params are dynamic, default int arithmetic — annotate for String/double), standalone
LSP/type-checker (inference lives in the transpiler).

---

## Original design (locked, now implemented)

## Thesis

Keep `beam-transpiler`'s lowering (actors→gen_server, supervision, channels,
dispatch) intact. Replace only the *front* — how the surface spells those
concepts — with braces-Python. Reuse the `zinc-python` frontend (lexer,
braces→blocks, implicit-self, f-strings, parser). The work is the **seam**:
point the existing lowering at braces-Python's spelling of each BEAM concept.

Surface is **braces, not whitespace** — that's the whole reason this dialect
exists. Python ergonomics (fast, to-the-point), `{}` blocks, runs on BEAM.

This is a **typed** language, not dynamic Python: return types are checked and
load-bearing (see #2). braces-Python today only transpiles; a type checker is
new work here, but the type info BEAM dispatch wants is the same info the
cast/call rule needs — one checker pays for both.

## Decisions (locked)

### 1. Marking an actor — base class
```
class Counter(Actor) {
    count = 0
    def incr(self) { self.count = self.count + 1 }
}
```
`(Actor)` base class maps 1:1 onto the old `implements Actor`. One mechanism
covers Actor / Application / protocol marks. **Seam: tiny.**

### 2. Cast vs call — checked return type
```
def incr(self) { ... }            # no return type   -> cast (async)
def get(self) -> int { ... }      # real return type -> call (sync, blocks)
```
Rule: a real `-> T` = sync **call**; no annotation or `-> None` = async
**cast**. Return types are **type-checked**, not optional hints — the checker
verifies the annotation matches the actual returns, and the cast/call lowering
keys off it. Safe default falls out: a missing annotation is a cast
(fire-and-forget, can't deadlock); a wrong cast where a value was wanted is
loud. **This rule is the whole architecture risk — concentrated in one place.**

### 3. Entry point & supervision — clean default, opt-in root
```
def main() { ... }                # script: top-level main, zero ceremony

class Main(Application) {          # supervised app: children are class fields
    worker = Worker()
    def main(self) { ... }
}
```
The `__main__` guard is auto-injected, so bare `def main()` is the entry point
with no ceremony — the clean form is the **default**, `class Main(Application)`
is the opt-in upgrade for supervised children. (Inverts the legal-Java problem
where clean main was gated behind Application.) **Seam: small.**

### 4. Protocols & dispatch — nominal via base class
```
class English(Greeter) {
    def greet(self, name) -> str { return "hello " + name }
}
l = lambda n: "lambda " + n        # SAM lambda satisfies a 1-method protocol
```
Keep **nominal** conformance (`class X(Greeter)`) — maps straight onto the
existing tag-dispatch lowering. Python's structural `typing.Protocol` is
**deferred to v2** (it needs backend dispatch changes). SAM lambdas carry over.
**Seam: tiny.**

### 5. FFI escape hatch — Pythonic import, same passthrough
```
from erlang import gen_tcp, inet      # was: import erlang.gen_tcp;
```
Same alias→module passthrough, same "exempt from the surface lint" status.
Only the spelling changes. **Seam: trivial.**

### Smaller rulings
- **Dunders — dropped.** The `toString`→`__repr__` style renaming is
  Python-target-specific; BEAM has no Python dunders, so it's not needed.
- **Records/structs — keep the existing lightweight convention.** We already do
  records; no `@dataclass`. Lowers to the existing record/tuple path.

## Falls out for free (Python fits BEAM better than Java here)
- **Mutable actor state** — `self.count = self.count + 1` is natural Python,
  lowers to gen_server state as today. No field/`var` ceremony.
- **Bootstrap ceremony** — gone. No `public static void main(String[] args)`.
- **`self`** — implicit-self frontend handles it; `this.x` → `self.x`.

## The seam, summarized
Four of five concepts are **base-class marks** the frontend already parses — so
backend changes are mostly "read the mark from a Python base list instead of a
Java `implements` clause." The genuinely new logic is:
1. **#2 cast/call from checked return types** — the one architecture risk.
2. **The type checker** — new component; minimally checks return types (drives
   cast/call); broader gradual typing TBD.

## Tooling
No javac gate (we don't target the JVM). Editor support comes from an **LSP we
build** — which is the high-value layer anyway (go-to-def, completion, the
type-checker's diagnostics inline). The cheap free-tooling that legal-Java gave
is replaced by the checker + LSP.

## Open / next
- Build order: (a) retarget emission Actor→gen_server for the base-class mark,
  (b) the return-type checker + cast/call, (c) supervision root, (d) protocols,
  (e) FFI import, (f) Channel. Each has an existing beam-transpiler analog to
  port against.
- Type checker scope: start with return-type checking (required by #2); decide
  how far gradual typing goes for params/locals as a follow-up.

---

# Stdlib veneer (spec — 2026-06-18)

**STATUS: ALL 5 ITEMS DONE (2026-06-18).** Commits: 721a8c3 (subscript + `len()`),
a2d1c6d (`.class` drop + `str()` + `e.message`), 270686d (`http.get` facade). Both
suites green (braces-Python e2e + legal-Java e2e + javac gate). As-built deltas from
the spec below: `str()` reuses the existing general `$fmt` helper (not a new `$str`);
item 1 also gave homogeneous **list** literals a `List<T>` type (not just dicts); and a
follow-on (3c079ed) made PyParser **keep generic type args** so `Channel<String>`/
`List<int>`/`Map<String,int>` annotations work. Coverage lives in `examples/py/veneer.zn`
plus `json.zn`/`exceptions.zn`/`http_facade.zn`.

## Why
The `.zn` grammar is already Python-familiar, but the **library idioms still read
like Java** — `HttpClient.newBuilder()`, `Json.decode(User.class, …)`,
`e.getMessage()`, `map.get/.put/.size()`, `xs.length` vs `dict.size()`. A day-one
Python/JS/PHP/Ruby dev hits that wall even though the syntax welcomes them. This
veneer adds Pythonic spellings as **additive aliases** over the existing BEAM
lowering — nothing is removed, the legal-Java surface keeps compiling. It is the
first deliberate CodeGen touch since the braces-Python build, so **every item must
keep `legaljava.sh` + `e2e.sh` green** (run both after each). Each change is sugar
over machinery that already exists: no new semantics, no Resolve changes. The
frontend is untouched — everything lands in CodeGen + one PyInfer return-type line.

Verified scoping facts: `m["k"] = v` already parses to `IndexAssignStmt`
(PyParser.java:455); dict literals are already typed `HashMap` (PyInfer.java:174,
PyParser.java:346); the `'zinc.http'` runtime module that `send` calls already
exists.

## Items

### 1. Subscript indexing for dicts — `d["k"]` / `d["k"] = v` ✅ DONE
The most visible inconsistency (lists subscript, dicts don't).
- **Read** — `genExpr` `case Index` (CodeGen.java:2521): add a `Map`/`HashMap`
  base-type branch → `maps:get(Key, M)`, ordered `T[]`→`array:get`,
  `Map`→`maps:get`, else `lists:nth`.
- **Write** — `IndexAssignStmt` lowering (CodeGen.java:1938), today array-only:
  add a `Map`/`HashMap` branch → `maps:put(Key, Val, M)` with the SSA rebind that
  genMutator's `put` uses (1982); type-guard the value via existing `guarded(...)`.
- Compat: Java surface never indexes maps with `[]`; `.get/.put` stay working.

### 2. Global `len(x)` — one spelling for length ✅ DONE
- Builtin in `genExpr` `case Call` (CodeGen.java:2545), dispatched on
  `exprType(arg)`: `String`→`string:length`, `List`→`length` (**keeps the O(n)
  cost warning, per the 2026-06-12 honest-cost policy**), `ArrayList`/`T[]`→
  `array:size`, `Map`/`HashMap`→`maps:size`; else a clear compile error.
- Return type `int` — add `len` to the inference switch near CodeGen.java:3575.
- Guard: only a builtin when no user function named `len` is in scope (don't
  hijack the Java surface).

### 3. `str(x)` and `e.message` ✅ DONE
- **`str(x)`** — builtin Call, **general over any value**: already-`String`
  passthrough; everything else (int/float/bool/binary/exception) via one prelude
  helper `'$str'/1`. Return type `String`.
- **`e.message`** — `FieldAccess` `.message` on an exception-typed value reuses
  the `getMessage` lowering (CodeGen.java:2897–2900 / 3802). Dispatched on the
  exception type, so a record with a real `message` field is unaffected.

### 4. Drop `.class` in `Json.decode` — accept the bare record name ✅ DONE
- `classLitRecord` (CodeGen.java:2957): if `classLitName` returns null, also
  accept a bare `VarRef` naming a known record. `Json.decode(User, s)` works;
  `User.class` still works. Flows to `decodeAll` and other `classLitRecord` callers.

### 5. `http.get(url)` facade — keep the builder for power users ✅ DONE
- `http` namespace in `genNamespaceCall`: `http.get(url)`, `http.post(url, body)`,
  `http.put`, `http.delete` → convenience entries `'zinc.http':get/1`, `post/2`,
  etc. that build the default client + request and call existing `send` (same
  `HttpException` ladder). Return type = `send`'s response type.
- Only item needing a non-CodeGen edit: add those 4 functions to the `'zinc.http'`
  runtime `.erl`. The Java-style builder stays.

## Consistency / docs / tests
- Document `len()` and subscript as the **canonical** spellings; keep
  `.size()/.length/.get/.put` working for compat.
- e2e-py coverage: extend `dict`, `collections`, `json`, `exceptions`,
  `http_client` (or one new `veneer.zn`) to exercise `d["k"]`, `d["k"]=v`,
  `len()`, `str(e)`/`e.message`, `Json.decode(User, …)`, `http.get`; add to
  `examples=(…)` + `want` in `e2e-py.sh`.

## Order (by leverage)
1. Subscript + `len()` — kills the most visible inconsistency
2. `.class` drop — smallest change
3. `str()` / `.message`
4. `http.get` facade — only item touching a prelude `.erl`

## Type safety — the veneer must not reopen dynamic footguns
The point of the surface is Python ergonomics **with** static teeth, so production
doesn't collapse the way interpreted languages allow. The existing posture is
**typed-by-default, dynamic-quarantined**: params must be typed (`parameter 'a'
needs a type`), return types checked, `String`↔`int` binding errors, exhaustive
match — and the *only* dynamic values are explicit crossings (JSON fields, raw map
gets) that pass a **guarded runtime check** (`guarded(...)` / `'$jchk'`,
CodeGen.java:1871, 2049), so a wrong dynamic value fails **loud at the boundary**,
not silently 10 frames later. The veneer keeps that contract:

- **`len(x)` / `str(x)`** — total and type-directed; no new dynamic surface.
- **`Json.decode(User, …)` / `http.get`** — return concretely-typed values.
- **Dict subscript is the one risk.** Rule:
  - **Homogeneous literal** (`{"a":1, "b":2}`) → inferred `HashMap<K,V>`; `d["k"]`
    is concretely typed, so `d["k"] + "s"` is a **compile error**. (Strictly more
    safety than today, where `MapLit` infers bare `HashMap`.)
  - **Heterogeneous literal** (`{"host":"localhost", "port":8080}`) → inferred
    `Map<String, dynamic>`; `d["k"]` is **dynamic** and behaves like `.get()`
    today: it cannot be used directly in a typed op — it must first cross into a
    concrete type via an annotation (`port: int = config["port"]`), which inserts
    the guarded runtime check. So `config["port"] + 1` without the crossing is a
    compile error; the crossing makes it safe and fails loud if the value isn't
    really an `int`. This matches the existing `json.zn` pattern
    (`host: String = config.get("host")`).
  - Implementation: PyInfer `MapLit` (PyInfer.java:174) returns the join of value
    types — `HashMap<K,V>` when all values agree, `Map<String, dynamic>` otherwise.
    Subscript read (CodeGen.java:2521) routes dynamic-valued maps through the same
    guarded-crossing path `.get()` already uses.

### Deliberately not doing — Optional/None safety
**Decision (2026-06-18): no Optional/None construct.** "Keep it safe, but no
safer." Compile-time null-safety adds unwrap/`?`/Optional ceremony that slows
day-one programming — the exact fluff this surface exists to avoid. And the BEAM
target already gives the safety net for free: a nil/badmatch **crashes the process
loud and immediately** (fail-fast, never a silent wrong value), and supervision
restarts it. That's the null-safety story — runtime loud-crash + supervision, not
type-level ceremony. The bar is: catch the footguns that corrupt data silently
(type confusion across boundaries, mixed-dict misuse), not the ones the runtime
already makes loud.
