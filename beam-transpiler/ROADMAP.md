# Roadmap — Java-on-BEAM language

## What this is
**Legal Java syntax that transpiles to Erlang/BEAM** — classic class ceremony, types,
semicolons, `System.out.println` — plus extension keywords only where OTP concepts need them
(`actor`, `spawn`). Java devs get OTP-grade reliability (supervision, self-healing,
distribution) without writing functional code. The pitch: a lightweight alternative to the
microservices + Kubernetes tax for small teams. The language is the on-ramp; BEAM is the moat.
Project-focused: one public class per file, file named after its class, dirs = packages.

The end state this roadmap builds to: **one installer → dev kit → managed runtime → `build` /
`run` / `deploy` to a plain VM**. A user never installs Erlang, never sees `erlc`, never writes
a supervisor — and gets a service that heals itself.

## The end-to-end pipeline (what every phase slots into)
```
file.src ──lex/parse──► AST ──codegen──► N .erl modules ──erlc──► .beam ──erl──► running BEAM
            (Java transpiler)            main + actor modules                    supervised app
                                         + generated supervisor
└────────────────────────────── today: e2e.sh + Docker ─────────────────────────────────────┘
└────────────────────────────── Phase 4: `zc build` / `zc run` (managed runtime, no Docker) ─┘
└────────────────────────────── Phase 5: `zc release` / `zc deploy` (self-contained bundle) ─┘
```

## Where we are (done)
- **`beam-lab/`** — validated that imperative idioms lower cleanly + fast to Erlang/OTP
  (`LOWERING_SPEC.md` is the codegen contract; throw/catch free; supervised self-heal ~16µs).
- **`beam-transpiler/`** — working Java transpiler (lexer → sealed AST → parser → Erlang codegen;
  Java 22+ multi-file source launcher, so `java src/zinc/Main.java` needs no build tool).
  `./e2e.sh` runs **13 example programs end-to-end on BEAM**.
- **Surface = legal Java (rewrite landed after Phase 1):** classes with static methods,
  typed params/locals (+`var`), classic/enhanced `for`, while, if/else-if, break/continue
  (classic-for continue runs the update), `++`/compound assigns, `int[]` + `.length`,
  records as structs (`new Point(1,2)`, `p.x()`; `p.x = v` mutation sugar is an extension),
  `String +` concat (type-aware → binary segments), int/int → `div`, `System.out.println`
  (`~ts` for strings, `~p` otherwise), `Thread.sleep`. Declared types drive codegen
  (void vs typed actor methods, div vs /, concat vs add).
- **Phase 1.1 multi-module output** (`7e3ec4a`) — codegen emits named Erlang modules, driver
  writes `.erl` files to an outdir, `e2e.sh` compiles with `erlc` and runs `erl -noshell`.
  Escript path retired. All 10 examples green.
- **Multi-file, multi-dir projects**: point the transpiler at a directory — each class is a
  module (`class Fmt` → `fmt`), `class Main` with `main(String[] args)` is the entry,
  dirs = packages (`import util.MathUtil;` → `util/MathUtil.src`), file must declare its
  eponymous type. `MathUtil.sumTo(4)` lowers to `mathutil:sumTo(4)`; imports and
  method/arity existence checked at transpile time. e2e case: `examples/multifile/`.
- **Phase 1, the `actor` surface: DONE.** `actor` declarations (typed fields + methods)
  compile to a gen_server module per actor + one generated `actor_sup` (one_for_one, dynamic
  children). Handles are registered names, so they survive restarts; **void method ⇒ async
  cast, typed method ⇒ sync call** (the Java type IS the messaging contract); state = map of
  fields, method bodies reuse the SSA machinery seeded from `maps:get`. v1 limits enforced
  with clear errors: return only as last stmt of a call method, `spawn T()` arg-less and only
  as `var x = spawn T()`, actors file-local, handles not storable/passable. Headline e2e
  `actor_selfheal`: crash a method → supervisor restarts → SAME handle serves (`3/0/1`).
- Dev toolchain: local JDK (Temurin 25 at `~/.local/java/current`, override with `JAVA_BIN`)
  for the transpiler + Docker `erlang:slim` for erlc/BEAM — that stays the dev/CI path
  until Phase 4 replaces it for END USERS with the managed runtime.

---

## Phase 1 (DONE — see "Where we are"): the **`actor` surface** — the differentiator
The whole reason the language exists. Detailed implementation plan: **`PLAN.md`** (settles
handle syntax `c.incr()`, return⇒call / no-return⇒cast, registered-name handles that survive
restarts, SSA-over-state-map handlers, dynamic `actor_sup`, v1 restrictions).

Steps (each guarded by the 10-example regression suite):
1. **1.2 Parse** — `actor`/`on`/`spawn` keywords, `ActorDecl`/`HandlerDecl`/`SpawnExpr`/
   `MethodCall` AST, parser returns `Program{fns, actors}`. Inert: e2e stays green.
2. **1.3 Codegen** — per actor → gen_server module (state = field map, handlers lowered with
   the existing SSA machinery); one generated `actor_sup` (supervised-by-default, dynamic
   children, handles = registered names so they survive restarts); `spawn`/method-call
   dispatch; `sleep(ms)` builtin. Actor-free programs byte-identical to today.
3. **1.4 Demo + e2e** — `actor_counter.src` (happy path) and `actor_selfheal.src`: crash a
   handler, supervisor auto-restarts, SAME handle keeps serving (`3 / 0 / 1` output). That
   self-heal e2e IS the thesis proof — make it the headline case.

**Done =** a supervised Counter written in plain imperative syntax compiles to
gen_server+supervisor, runs on BEAM, and survives a crash by auto-restarting — verified in `e2e.sh`.
**Status: shipped — `actor_counter` and `actor_selfheal` are green in the 13-case suite.**

## Phase 2: **Erlang FFI + modules/imports** — DONE
- **FFI**: `import erlang.<module>;` binds that OTP module; `lists.sort(xs)` lowers to
  `lists:sort(Xs)`. No arity check (signatures unknown); declared types on the receiving
  var supply string/number typing. e2e: `ffi` -> BEAM-9.
- **Modules/imports**: done earlier (see "Where we are").

### Language-shape round 1: DONE (all designed against the Java surface, e2e 18/18)
- **Atoms**: `Atom.ok` (enum-constant syntax) -> atom `ok`.
- **Tuples**: `Tuple.of(a,b)` -> `{A,B}`; `Tuple.get(t,i)` 0-based; `Erlang.ok(e)` unwraps
  `{ok,V}` or raises catchable `{badmatch,...}` — OTP's ok/error idiom becomes Java's
  value-or-throw idiom.
- **Lambdas**: Java syntax -> Erlang funs; Java's effectively-final rule enforced (it IS
  Erlang's capture semantics). Unlocks lists:map/filter/foldl etc.
- **HashMap**: `new HashMap()` -> `#{}`; `m.put/remove` are statements (SSA rebind, counted
  as mutation by loop threading); `get/containsKey/size` pure.
- **try/catch**: `catch (Exception e)` -> `catch error:E`; assigned vars phi-merge like if;
  internal `'$ret'/'$brk'/'$cont'` signals are throw-class and pass through untouched.

### Language-shape round 2: the java.lang/java.util facade — DONE (e2e 20/20)
**The point of the project: user code is Java, not Erlang-in-braces.** FFI (`import
erlang.*`) is the basement door for libraries; day-to-day code uses Java idioms, and plain
loops are both the most Java and the fastest lowering (direct recursion beat foldl 1.85-2x
in beam-lab). Facade dispatch is by the receiver's STATIC type, so chains work
(`t.replace(..).indexOf(..)`).
- String: length/isEmpty/equals/toUpperCase/toLowerCase/trim/substring/contains/startsWith/
  endsWith/indexOf(byte offset)/replace/split/repeat
- ArrayList/List: add (statement, `++` O(n) — buffer tier later), get/size/contains/isEmpty
- HashMap/Map: put/remove (statements), get/getOrDefault/containsKey/size/isEmpty/keySet/values
- Statics: Math.max/min/abs, Integer.parseInt, String.valueOf

### Language-shape round 3: DONE (e2e 22/22) — shape complete, ready for dogfood
- **Arrays = Erlang `array` module** (the LOWERING_SPEC tier): `T[]` is fixed-size +
  indexable (`xs[i] = v` -> `array:set`, O(log n)); `List` stays an Erlang list (growable,
  iterable). Bridges: `Arrays.asList(xs)` / `list.toArray()` — also the explicit FFI
  boundary. `new int[n]` -> `array:new` with typed default; `.length` -> `array:size`;
  for-each converts once per loop.
- **switch** (arrow form): constants + bare enum labels (subject-typed), multi-label,
  default; phi-merge across arms; no-match without default is a no-op (Java semantics).
- **enum -> atoms**: `enum Color { RED }`, `Color.RED` -> `'RED'`; switch + == work.
- **Actor constructors**: `spawn Counter(40)` -> ctor params via init args, embedded in the
  child spec, so a supervisor restart re-runs the SAME constructor. Fields may omit
  initializers (typed defaults).
- **Cross-file actors + typed handles**: actor registry is project-wide; handle dispatch is
  by static type (`Counter c` param/field works) — no more var-binding-only tracking.

Out of shape scope (deliberately): typechecker (Phase 6), error source-maps (Phase 4.3),
interfaces, streams facade, handles in collections (need generics story).

### Dogfood: DONE — TCP line server (e2e `tcpserver`, 23/23)
Acceptor actor + per-connection actors + socket handoff + FFI; worked on try 2 (try 1 hit
the then-missing string escapes). Findings round landed: string escapes (lexer decode/emit
encode), break/continue through try+switch, `this` self-handle ('$self' state key, every
actor), `s.toCharArray()` for charlist APIs, `List.of`/`Map.of`, handle_call/cast stubs
(no behaviour warnings). **Language semantic decided: try/catch is transactional** — a
caught try reverts outer-var mutations to try-entry values (differs from Java; Erlang
cannot observe partial bindings). Record: `dogfood/FINDINGS.md`.

### Next: more dogfood breadth (JSON/OTP-27 `json`, a hex dep via the future rebar3
plugin) or start Phase 4 tooling (`zc` CLI wrapping transpile+erlc+run).

## Phase 3: second-tier language features (round out the language)
Interleave as needed — all incremental on the existing codegen:
- **string ops** — concat, length, split, contains, substring
- **array ops** — push/append, slice, contains; `xs[i] = v` (index assignment → persistent
  vector or explicit mutable `Buffer` per `LOWERING_SPEC.md` tiers)
- **surface `try`/`catch`** — user-facing error handling (throw/catch already proven)
- **`switch`/`match`** — pattern matching
- **tuples / multiple return**, optionally **lambdas / first-class functions**

## Phase 4: **SDK & toolchain — the one-stop shop**
One installer gets you everything; the dev kit manages the runtime. Rustup/flutter model:
**install dev kit → dev kit installs runtime**. No Docker, no system Erlang, no rebar3.
Working CLI name: `zc` (final language name is open decision #6 — rename then).

- **4.1 The `zc` CLI** (first, still runnable via Docker while the installer doesn't exist):
  - `zc run file.src` — transpile → erlc → run, one command, quiet unless it fails
  - `zc build` — project build into `_build/` (.beam + start script)
  - `zc new myapp` — scaffold (src/main.src, `zc.toml` with pinned toolchain+OTP versions)
  - `zc doctor` — verify dev kit + runtime install, print versions, suggest fixes
  - End users never see Java, same way they never see Erlang: the installer's managed-runtime
    model (4.2) covers both — a pinned JRE lands in `~/.zc/` next to the pinned OTP build, and
    `zc` is a thin launcher over them. Everything compiles **to BEAM**; internally `zc` drives
    the standard Erlang toolchain (erlc, relx for Phase 5 releases) — users only ever see `zc`.
- **4.2 The installer (one-stop shop)**:
  - `curl -fsSL get.<lang>.dev | sh` (and a Windows installer later) → installs `zc` into
    `~/.zc/bin`, adds to PATH, then first-run TUI: pick/confirm OTP version → downloads a
    **pinned portable OTP build** into `~/.zc/otp/<ver>` (prebuilt per platform — the same
    trick Elixir-burrito/Livebook use). Progress bars, resumable, checksummed.
  - The runtime is **owned by the dev kit**: `zc toolchain list|install|use`, per-project pin
    in `zc.toml`. Users NEVER apt-install erlang. This is the moment Docker stops being a
    user-facing requirement (CI keeps it).
- **4.3 Error source-maps** — map erlc/runtime errors back to `.src` spans (the transpiler
  tax; budget real time, it's what makes the toolchain feel native instead of leaky).

## Phase 5: **deploy — the anti-K8s story**
A small team ships a self-healing service to a $10 VM with one command. This is the pitch
made real, and it's pure BEAM strength:
- **`zc release`** — self-contained bundle: ERTS + compiled .beam + boot script in a tarball.
  Target machine needs NOTHING installed (no Erlang, no container runtime).
- **`zc deploy user@host`** — scp the release, install a systemd unit (restart-on-boot;
  BEAM+supervisors handle everything above process level), health-check, flip a `current`
  symlink, roll back on failed health check. Deploy #2 is an upgrade.
- **Compliance workflow (no scanning in zc)**: deploys need a manual green light from a
  separate security/compliance team that runs Trivy — we have no scanner access.
  Flow: `zc build` emits a derived mix.lock (rebar.lock is the dep truth; Trivy reads
  mix.lock) -> human ships it to compliance -> green light -> deploy. No gating in the tool.
- **Identity-validated deploys**: compliance does NOT continuously scan prod (tight
  timing/network constraints) — they validate by name + version + container hash (docker
  coordinates). So releases must be immutable and coordinate-addressable: version from
  zc.toml, content digest at build time, and the `--docker` OCI image (digest = the
  compliance coordinate) is first-class, not a later add-on. Deploy ships the exact
  green-lit digest — never rebuild at deploy. (OTP/ERTS isn't in any lockfile — covered
  by zc's toolchain pin, bumped on advisories, same model as the zinc-go Go pin.)
- **Later in 5**: `--docker` flag to emit a minimal OCI image for teams that want it;
  multi-node clustering/distribution (BEAM's native distribution is the long-game moat);
  hot code upgrades only if ever justified (systemd restart is fine for the target user).

## Phase 6: polish & bets
- **Optional checker / linter** on the AST (types as opt-in — the one thing worth stealing
  from Gleam: `Option` instead of null).
- **Name the language** (open decision #6) — needed by the time the installer/domain exists.
- (Much later) self-hosting — rewrite the compiler in this language.

---

## Open design decisions
1–5 (handle syntax, call/cast rule, supervision model, state mutation, multi-module always)
— **settled in `PLAN.md`**.
6. **language name** — TBD; blocks the installer domain + CLI final name, nothing before Phase 4.
7. **portable OTP distribution** — build our own per-platform OTP tarballs vs reuse existing
   prebuilt ones; decide early in Phase 4.2.
8. **`zc.toml` shape** — project manifest (name, version, toolchain pin, deps later in
   Phase 2-FFI era). Keep minimal; decide at `zc new`.

## Start here next session
**Phase 2 — Erlang FFI** (modules/imports already done): call existing BEAM libraries from
the surface language, lowering to `module:function(Args)`. Then Phase 3 features as needed.
```
cd beam-transpiler && ./e2e.sh        # current green baseline (13/13) before you start
```
