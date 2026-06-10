# Roadmap — BEAM imperative language

## What this is
A familiar **imperative C-family language that transpiles to Erlang/BEAM**, giving mainstream
(Java/C#/Go) developers OTP-grade reliability — supervision, self-healing, distribution — **without**
writing functional code. The pitch: a lightweight alternative to the microservices + Kubernetes tax
for small teams. The language is the on-ramp; the BEAM runtime is the moat.

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
  `./e2e.sh` runs **10 example programs end-to-end on BEAM**. Base language is usable:
  fn, var/assign(+compound), for-range/for-each/while, if/else-if/else, break/continue, early
  return, ints/floats/bools, lists, structs, strings (+interpolation), arithmetic/comparison/logical,
  field & index read, field set, print/println/len.
- **Phase 1.1 multi-module output** (`7e3ec4a`) — codegen emits named Erlang modules, driver
  writes `.erl` files to an outdir, `e2e.sh` compiles with `erlc` and runs `erl -noshell`.
  Escript path retired. All 10 examples green.
- **Multi-file, multi-dir projects** (Phase 2's modules/imports, pulled forward): point the
  transpiler at a directory — every `.src` is a module, `main.src` at the root is the entry,
  `util/math.src` becomes module `util_math`. Surface: `import util/math` (alias = last
  segment), calls `math.sum_to(4)` lower to `util_math:sum_to(4)`. Import resolution and
  fn/arity existence checked at transpile time; `MethodCall` postfix (`x.f(args)`) landed in
  the parser, which Phase 1 actors reuse. e2e case: `examples/multifile/`.
- Dev toolchain: local JDK (Temurin 25 at `~/.local/java/current`, override with `JAVA_BIN`)
  for the transpiler + Docker `erlang:slim` for erlc/BEAM — that stays the dev/CI path
  until Phase 4 replaces it for END USERS with the managed runtime.

---

## Phase 1 (NEXT): the **`actor` surface** — the differentiator
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

## Phase 2: **Erlang FFI + modules/imports** (practicality)
Makes real programs possible — calling existing BEAM libraries (HTTP, JSON, DB, etc.).
- **FFI**: call Erlang/Elixir functions, e.g. `extern fn now() = "erlang:system_time"` or
  qualified `erlang.system_time()`. Lower to `module:function(Args)`. Compiler bridges the
  ugly atom names; users never see them.
- **Modules/imports**: DONE (pulled forward — see "Where we are"). Remaining detail work:
  visibility (everything is exported today), import aliasing/renames if needed.
- This is what turns demos into apps. Detail pass happens when Phase 1 ships.

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
**Phase 1.2 — parse the actor surface.** Open `PLAN.md`, follow Step A (lexer keywords →
AST nodes → parser), keep e2e green, then Step B (codegen) and C (the self-heal demo).
```
cd beam-transpiler && ./e2e.sh        # current green baseline (10/10) before you start
```
