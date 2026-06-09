# Roadmap — BEAM imperative language

## What this is
A familiar **imperative C-family language that transpiles to Erlang/BEAM**, giving mainstream
(Java/C#/Go) developers OTP-grade reliability — supervision, self-healing, distribution — **without**
writing functional code. The pitch: a lightweight alternative to the microservices + Kubernetes tax
for small teams. The language is the on-ramp; the BEAM runtime is the moat.

## Where we are (done)
- **`beam-lab/`** — validated that imperative idioms lower cleanly + fast to Erlang/OTP
  (`LOWERING_SPEC.md` is the codegen contract; throw/catch free; supervised self-heal ~16µs).
- **`beam-transpiler/`** — working Dart transpiler (lexer → sealed AST → parser → Erlang codegen).
  `./e2e.sh` runs **10 example programs end-to-end on BEAM**. Base language is usable:
  fn, var/assign(+compound), for-range/for-each/while, if/else-if/else, break/continue, early
  return, ints/floats/bools, lists, structs, strings (+interpolation), arithmetic/comparison/logical,
  field & index read, field set, print/println/len.
- Toolchain is Docker-only: `dart:stable` to transpile, `erlang:slim` to run. No local installs.

---

## Phase 1 (NEXT — start here tomorrow): the **`actor` surface** — the differentiator
This is the whole reason the language exists. Until actors exist, it's "just another imperative
language on BEAM." The Erlang target is **already proven** in `beam-lab/lowering/otp/` — Phase 1 is
about generating that pattern from clean imperative syntax.

### Proposed surface (refine first thing, then build)
```
actor Counter {
  var count = 0                       // actor state

  on incr()        { count = count + 1 }      // no return  -> async cast
  on add(n)        { count = count + n }       // cast with args
  on get()         { return count }            // has return -> sync call (reply)
}

fn main() {
  var c = spawn Counter()             // start a SUPERVISED actor, get a handle
  c.incr()
  c.incr()
  print(c.get())                       // 2   (call)
}
```
Rule of thumb: a handler with `return` is a **call** (sync); without, a **cast** (async). State
fields are `var`s at actor top level; handlers mutate them (SSA → new gen_server state map).

### The big architectural change this forces: **multi-module output**
`gen_server`/`supervisor` need a module per behaviour, so we can no longer emit a single escript.
Phase 1.1 is therefore: **driver emits N `.erl` files, compiles with `erlc`, runs.**
- program with no actors → one `main` module (keep all 10 current examples working)
- each `actor` → one gen_server callback module
- one generated supervisor module (supervised-by-default; dynamic so `spawn` adds children)
- update `e2e.sh` to handle the multi-module case (compile dir, run entry) — mirror
  `beam-lab/run.sh -m`.

### Step order
1. **1.1 Multi-module output** — driver writes `.erl` files + `erlc` + run; existing examples still
   green (regression guard). This is the riskiest plumbing; do it first, in isolation.
2. **1.2 Parse** — keep `actor`/`on`/`spawn` in the AST (don't discard like structs). Handler kind =
   call if it contains `return`, else cast. `spawn Name(args)` expression. `c.method(args)` already
   parses as a call on a field access / method — decide method-call syntax.
3. **1.3 Codegen** — per actor → gen_server module (state = field map; `handle_cast` for casts,
   `handle_call` for calls; handler body lowered with existing SSA machinery, final field map = new
   state). Supervisor module. `spawn Counter()` → `supervisor:start_child` returning the pid/handle.
   `c.incr()` → `gen_server:cast`, `c.get()` → `gen_server:call` (dispatch by the actor's handler
   kind — so codegen needs the actor decls in scope).
4. **1.4 Demo + e2e** — a `counter` actor (incr/add/get) driven from `main` → prints expected value;
   then a **crash + self-heal** demo (a handler that crashes; supervisor auto-restarts; show it keeps
   serving). That self-heal demo IS the proof of the whole thesis — make it the headline e2e case.

### Phase 1 "done" =
A supervised `Counter` actor, written in plain imperative syntax, compiles to gen_server+supervisor,
runs on BEAM, and **survives a crash by auto-restarting** — all from the new surface, verified in
`e2e.sh`.

---

## Phase 2: **Erlang FFI + modules/imports** (practicality)
Makes real programs possible — calling existing BEAM libraries (HTTP, JSON, DB, etc.).
- **FFI**: a way to call Erlang/Elixir functions, e.g. `extern fn now() = "erlang:system_time"` or a
  qualified-call syntax `erlang.system_time()`. Lower to `module:function(Args)`. Compiler bridges
  the ugly atom names; users never see them.
- **Modules/imports**: multi-file programs, `import`, namespacing. (Phase 1's multi-module output is
  a prerequisite, so this gets cheaper after Phase 1.)
- This is what turns demos into apps.

## Phase 3: second-tier base features (round out the language)
Interleave as needed — all incremental on the existing codegen:
- **string ops** — concat, length, split, contains, substring
- **array ops** — push/append, slice, contains; `xs[i] = v` (index assignment → persistent vector or
  an explicit mutable `Buffer` per `LOWERING_SPEC.md` tiers)
- **surface `try`/`catch`** — user-facing error handling (throw/catch already proven)
- **`switch`/`match`** — pattern matching
- **tuples / multiple return**, optionally **lambdas / first-class functions**

## Phase 4: tooling & polish
- **Real CLI** — `<lang> build file.src` / `run` / project scaffold, wrapping the BEAM build (à la
  `gleam build`) so users never touch `erlc`/rebar3 directly. Name the language here.
- **Error source-maps** — map Erlang/compile errors back to source spans (the transpiler tax; plan
  for it now, it eats time later).
- **Optional checker / linter** on the AST (types as opt-in — the one thing worth stealing from
  Gleam: `Option` instead of null).
- (Much later) self-hosting — rewrite the compiler in this language.

---

## Open design decisions to settle (cheap to decide, expensive to change)
1. **Actor handle / method-call syntax** — `c.incr()` vs `send c incr` vs `c ! incr`. (Lean `c.incr()`.)
2. **call vs cast disambiguation** — `return`-in-handler = call (proposed). Confirm.
3. **spawn + supervision** — dynamic supervisor (`spawn` adds children) vs static declared actors.
   Dynamic is more flexible; start there.
4. **state mutation model in handlers** — SSA over the field map, final map = new gen_server state.
5. **Output: single-module-when-no-actors vs always multi-module** — keep single for actor-free
   programs (simpler, faster) and switch to multi only when actors present? Or always multi for
   consistency? (Lean: always multi once Phase 1 lands; retire the escript path.)
6. **language name** — still TBD.

## Start here tomorrow
**Phase 1.1 — multi-module output.** Make the driver emit `.erl` files + `erlc` + run, keep all 10
examples green, then layer the `actor` surface (1.2→1.4) on top. The self-heal e2e is the goal post.
```
cd beam-transpiler && ./e2e.sh        # current green baseline (10/10) before you start
```
