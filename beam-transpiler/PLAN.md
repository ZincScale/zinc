# Phase 1 detail — the `actor` surface — **SHIPPED**

**Status: implemented end-to-end.** All steps below landed (Java impl); `actor_counter` and
`actor_selfheal` are green in `e2e.sh` (13 cases). Deviations from this plan, post-modules:
actors may live in ANY project module (each still becomes its own gen_server module);
handlers calling file-local fns lower to qualified `filemodule:fn(...)` calls, and a file
that declares actors exports its fns to make that work. The doc below is kept as the
design record; "Known debts" at the bottom still apply.

Implementation-level detail for ROADMAP Phase 1 (the master phase plan lives in
ROADMAP.md; this is its Phase 1 appendix), grounded in the current code. Phase 1.1
(multi-module output) is **done** (`7e3ec4a`): codegen returns `{module name -> source}`,
the driver writes `<outdir>/<mod>.erl`, `e2e.sh` compiles with `erlc` and runs
`erl -noshell -eval "main:main()"`. All 10 examples green.

Target generated code = exactly the proven pattern in `beam-lab/lowering/otp/`
(otp_counter.erl / otp_sup.erl / otp_demo.erl).

---

## Design decisions this plan fixes (ROADMAP "open decisions")

1. **Handle syntax**: `c.incr()` — method-call postfix.
2. **call vs cast**: handler containing `return` ⇒ sync call; otherwise async cast. Confirmed.
3. **Supervision**: ONE generated `actor_sup` module, `one_for_one`, dynamic children via
   `supervisor:start_child` with generated unique ids. `intensity` high (e.g. 1000) for v1 so
   demos can crash repeatedly.
4. **Handles survive restarts** (the critical one): `spawn Counter()` does NOT return a pid —
   pids die with the process and the self-heal demo would break. It returns a **registered
   name** (atom `counter_<N>`, generated at spawn time). The child spec embeds the name, so
   the supervisor re-registers the SAME name on restart, and the handle keeps working after
   a crash. `gen_server:cast/call` accept registered names natively.
5. **State model**: gen_server state = map of field atoms. Handler bodies lower with the
   existing SSA machinery: seed env with one fresh var per field (bound via `maps:get`),
   run `genStmts`, rebuild the map from the final env.
6. **Dispatch (cast vs call at the call site)**: codegen keeps a side table
   `var name -> actor type`, populated when it sees `var c = spawn Counter()`. Method calls
   on tracked vars dispatch by that actor's handler table. v1 restriction: calling methods
   on a handle that wasn't directly bound from `spawn` (e.g. passed as a fn param) is a
   compile error — lift later with an unambiguous-handler-name fallback or type annotations.

### v1 restrictions (explicit, error with a clear message)
- In a handler, `return` only as the **last top-level statement** (early return would need
  the `'$ret'` throw to also carry the state map — defer; throw shape `{'$ret', V, State}`
  is the known lift).
- `spawn Counter()` takes no args in v1 (state fields have initializers). Positional
  constructor args → later.
- Actor named `Main`/`main` or colliding with another actor (case-insensitive) → error
  (module name clash).
- Handles can't be stored in structs/lists in v1 (no tracking through them). They CAN be
  returned... no — also error: only `var x = spawn T()` binds a tracked handle.

---

## Step A — lexer + AST + parser (ROADMAP 1.2)

**Lexer.java**: add keywords `actor` → `KW_ACTOR`, `on` → `KW_ON`, `spawn` → `KW_SPAWN`
(TokKind.java: 3 new values). Nothing else — all needed punctuation exists.

**Ast.java**:
```java
record Program(List<FnDecl> fns, List<ActorDecl> actors) {}
record ActorDecl(String name, List<FieldInit> fields,        // var name = init
                 List<HandlerDecl> handlers) {}
record HandlerDecl(String name, List<String> params, Block body) {}
record SpawnExpr(String actorName) implements Expr {}        // spawn Counter()
record MethodCall(Expr target, String method, List<Expr> args) implements Expr {} // c.incr(...)
```
Handler kind is NOT stored — computed in codegen (`hasReturn(body)`), one source of truth.

**Parser.java**:
- `parseProgram()` returns `Program`; loops over `struct | actor | fn`.
- `parseActor()`: `actor Name { (var f = expr)* (on name(params) Block)* }` — fields first,
  then handlers (enforce order; simpler and reads well).
- `parsePostfix()`: after `.ident`, if `(` follows → parse args → `MethodCall(e, name, args)`.
  (Currently `c.incr()` is a parse error, so this is purely additive.)
- `parsePrimary()`: `spawn` keyword → `expect ident, expect ( )` → `SpawnExpr`.

**Callers**: `Main.java` passes `Program` to CodeGen.

*Guard*: after Step A alone, run `./e2e.sh` — 10/10 must stay green (parser changes are
additive; nothing emits yet).

## Step B — codegen (ROADMAP 1.3)

`CodeGen` takes `Program`; builds `Map<String, ActorDecl>` registry up front.
`generateModules()` returns:
- `main` module (as today) — but when actors exist, generated `main/0` becomes:
  ```erlang
  main() ->
      logger:set_primary_config(level, none),   % crash reports go to stdout by default
      {ok, _} = actor_sup:start_link(),         % and would pollute program output
      user_main().
  ```
- one module per actor (lowercased name), shape per `otp_counter.erl`:
  ```erlang
  -module(counter).
  -behaviour(gen_server).
  -export([start_link/1, init/1, handle_call/3, handle_cast/2]).
  start_link(Name) -> gen_server:start_link({local, Name}, ?MODULE, [], []).
  init([]) -> {ok, #{count => 0}}.              % field inits lowered with _genStmts/_genExpr
  handle_cast({incr}, State) ->
      Count_0 = maps:get(count, State),         % seed env from state
      Count_1 = Count_0 + 1,                    % existing SSA lowering, unchanged
      {noreply, #{count => Count_1}};           % final env -> new state map
  handle_cast({add, N_2}, State) -> ...
  handle_call({get}, _From, State) ->
      Count_3 = maps:get(count, State),
      {reply, Count_3, #{count => Count_3}}.
  ```
  Call handlers: reply value = lowered `return` expr (which must be last stmt — checked).
  No catch-all clauses — unknown messages crash the actor (let it crash; the sup heals).
- one `actor_sup` module (only when actors exist), shape per `otp_sup.erl` + dynamic spawn:
  ```erlang
  -module(actor_sup).
  -behaviour(supervisor).
  -export([start_link/0, spawn_child/1, init/1]).
  start_link() -> supervisor:start_link({local, actor_sup}, ?MODULE, []).
  spawn_child(Mod) ->
      N = erlang:unique_integer([positive]),
      Name = list_to_atom(atom_to_list(Mod) ++ "_" ++ integer_to_list(N)),
      {ok, _} = supervisor:start_child(actor_sup,
          #{id => Name, start => {Mod, start_link, [Name]}, restart => permanent,
            shutdown => 5000, type => worker, modules => [Mod]}),
      Name.                                      % the handle IS the registered name
  init([]) ->
      {ok, {#{strategy => one_for_one, intensity => 1000, period => 3600}, []}}.
  ```

**Expression lowering** (in `genExpr`):
- `SpawnExpr('Counter')` → `actor_sup:spawn_child(counter)`. In `VarStmt`, when init is a
  SpawnExpr, record `handleTypes[varName] = 'Counter'`.
- `MethodCall(target c, m, args)`: target must be a tracked handle var; look up handler `m`
  in that actor (error if missing). Cast → `gen_server:cast(C_x, {m, Args...})`;
  call → `gen_server:call(C_x, {m, Args...})`.
- Builtin `sleep(ms)` → `timer:sleep(Ms)` (needed by the self-heal demo to ride out the
  restart window; one line next to print/println/len).

*Guard*: 10/10 still green (actor-free programs take the exact same path — no `actor_sup`
module, no logger line, byte-identical main except none of this triggers).

## Step C — examples + e2e (ROADMAP 1.4)

1. `examples/actor_counter.zinc` — the happy path:
   ```
   actor Counter {
     var count = 0
     on incr()  { count = count + 1 }
     on add(n)  { count = count + n }
     on get()   { return count }
   }
   fn main() {
     var c = spawn Counter()
     c.incr()
     c.incr()
     c.add(5)
     print(c.get())          // 7
   }
   ```
2. `examples/actor_selfheal.zinc` — THE headline demo (thesis proof):
   ```
   actor Counter {
     var count = 0
     on incr() { count = count + 1 }
     on get()  { return count }
     on boom() { var x = 1 / 0 }      // real crash inside the actor (badarith)
   }
   fn main() {
     var c = spawn Counter()
     c.incr()
     c.incr()
     c.incr()
     print(c.get())          // 3   <- working actor
     c.boom()                //     <- crashes the gen_server process
     sleep(100)              //     <- supervisor restarts it (same registered name)
     print(c.get())          // 0   <- SAME handle serves again; state reset to init
     c.incr()
     print(c.get())          // 1   <- and it keeps working
   }
   ```
   Expected output `3 / 0 / 1` proves: crash happened (state reset), supervisor restarted
   it (handle answers at all), service continues. Zero supervision code in the surface.
3. `e2e.sh`: add both to `examples` array. Multi-line expectation:
   `want[actor_selfheal]=$'3\n0\n1'` (and `$got` already preserves newlines).
   Note: a `gen_server:call` during the restart window exits `noproc` — that's what
   `sleep(100)` avoids; if flaky in CI, bump to 250.

## Step D — docs + wrap-up

- Update `ROADMAP.md`: mark 1.1–1.4 done, point Phase 2 as next.
- Update this file with anything learned (esp. if the logger / restart-window behavior
  surprises).
- Commit per step (A, B+C can land together if B was guarded green mid-way).

---

## Order & why

A (parse, inert) → guard → B (codegen) → C (examples prove it) → D. Riskiest unknowns are
in B: (a) handler SSA reseeding from the state map — mitigated by reusing `genStmts`
verbatim with a pre-seeded env; (b) restart/registered-name timing in the demo — mitigated
by `sleep`, and the pattern is already proven end-to-end in `beam-lab/lowering/otp/`
(`../beam-lab/run.sh -m lowering/otp 'otp_demo:main()'` to see it run today).

## Known debts created (intentional, don't fix in v1)
- Early `return` in handlers unsupported (error). Lift = `{'$ret', V, State}` throw shape.
- Handle tracking is var-binding-only; no handles in params/structs/lists.
- `logger` fully silenced in actor programs (hides real crash reports too). Right fix
  later: logger handler → stderr, keep stdout pure.
- `list_to_atom` per spawn = atom leak under unbounded spawning. Fine for v1; revisit
  with `{via, Registry}` naming if it ever matters.
- High restart intensity (1000) — a real default would be ~3 in 5s once apps exist.
