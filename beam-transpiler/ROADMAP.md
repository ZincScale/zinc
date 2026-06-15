# Roadmap — Java-on-BEAM language

## What this is
**Legal Java syntax that transpiles to Erlang/BEAM** — classic class ceremony, types,
semicolons, `System.out.println` — with ZERO extension keywords: OTP concepts enter as
marker interfaces (`implements Application` / `implements Actor` — the `Serializable`
idiom; no annotations), so every file parses in any Java IDE. Java devs get OTP-grade
reliability (supervision, self-healing, distribution) without writing functional code. The pitch: a lightweight alternative to the
microservices + Kubernetes tax for small teams. The language is the on-ramp; BEAM is the moat.
Project-focused: one public class per file, file named after its class, dirs = packages.

The end state this roadmap builds to: **one installer → dev kit → managed runtime → `build` /
`run` / `deploy` to a plain VM**. A user never installs Erlang, never sees `erlc`, never writes
a supervisor — and gets a service that heals itself.

## The end-to-end pipeline (what every phase slots into)
```
file.zinc ──lex/parse──► AST ──codegen──► N .erl modules ──erlc──► .beam ──erl──► running BEAM
            (Java transpiler)            main + process modules                  supervised app
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
  `./e2e.sh` runs the **legal-Java gate then 55 cases end-to-end on BEAM**.
- **Surface = legal Java (rewrite landed after Phase 1):** classes with static methods,
  typed params/locals (+`var`), classic/enhanced `for`, while, if/else-if, break/continue
  (classic-for continue runs the update), `++`/compound assigns, `int[]` + `.length`,
  records as structs (`new Point(1,2)`, `p.x()`; `p.x = v` mutation sugar is an extension),
  `String +` concat (type-aware → binary segments), int/int → `div`, `System.out.println`
  (`~ts` for strings, `~p` otherwise), `Sys.sleep` (zinc's non-throwing sleep facade;
  `Thread.sleep`'s checked InterruptedException would break legal Java). Declared types drive codegen
  (void vs typed actor methods, div vs /, concat vs add).
- **Phase 1.1 multi-module output** (`7e3ec4a`) — codegen emits named Erlang modules, driver
  writes `.erl` files to an outdir, `e2e.sh` compiles with `erlc` and runs `erl -noshell`.
  Escript path retired. All 10 examples green.
- **Multi-file, multi-dir projects**: point the transpiler at a directory — each class is a
  module (`class Fmt` → `fmt`), `class Main` with `main(String[] args)` is the entry,
  dirs = packages (`import util.MathUtil;` → `util/MathUtil.zinc`), file must declare its
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
3. **1.4 Demo + e2e** — `actor_counter.zinc` (happy path) and `actor_selfheal.zinc`: crash a
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

### rebar3 plugin: DONE (`rebar_zinc/`)
Providers `zinc compile` / `zinc clean`; wire with provider_hooks so plain `rebar3
compile` transpiles `src/**/*.zinc` -> `src/zinc_gen/*.erl` first (transpiler via
`{zinc, [{compiler_home, ..}]}` or ZINC_HOME). Test: `rebar_zinc/test.sh` (fixture
project, rebar3 in docker + host JDK mounted). Forced decision: source extension is now
**`.zinc`** — `.src` collided with rebar3's mandatory `<app>.app.src`.

### `zc` CLI v1: DONE (`zc/Zc.java` + `bin/zc`, patterned on compilers/zinc-go cmd/zinc)
`zc init|build|run|clean|add|deps|version` (fmt stubbed). zinc.toml ([project] name/version,
[otp] version pin, [deps] hex table) -> zc GENERATES rebar.config + <app>.app.src (like
zinc-go generates go.mod) and wires the rebar_zinc plugin via _checkouts; rebar3/erl from
PATH (managed runtime comes in 4.2). Single-file Java CLI, no build step. Test: zc/test.sh
(init -> run -> "Hello from demo!").

### Dogfood 2: DONE — cowboy webdemo (`dogfood/webdemo/`, test.sh green)
Cowboy HTTP service; a zinc class IS the cowboy handler (cowboy calls `handler:init/2` by
module name); main doubles as httpc test client. The "undiagnosed timeout" was NOT zinc:
the corp egress firewall drops **Erlang's TLS ClientHello by fingerprint** (curl/Java
pass), so rebar3's hex fetch hangs forever. Fixes landed: **zc vendors hex deps itself**
(Java TLS; tarball + transitive requirements from metadata.config; minimal-version
selection; into `_checkouts/` — rebar3 then builds fully offline, which doubles as the
hermetic-builds story), checkout ebins on `zc run`'s code path, httpc needs explicit ssl
opts on cert-less systems. Record: `dogfood/FINDINGS-webdemo.md` (GAP-9, GAP-10).

### Next: **the standard library** (GAP-9 — the headline dogfood finding)
Webdemo code reads `List.of(Tuple.of(Atom.port, 0))` — Erlang in Java clothes; no Java
dev writes proplists of tagged tuples. The architecture is three layers:
1. **OTP behaviours = language semantics** (already true: `implements Actor` -> gen_server +
   supervisor; users never see callbacks).
2. **OTP stdlib apps = zinc's JDK**: Java-shaped facades backed by OTP. First surfaces:
   `zinc.http.HttpClient` over httpc, Java-shaped (also the one home for the
   corp-proxy / native-transport escape hatches the firewall finding motivates) and
   `zinc.net` sockets over gen_tcp. Collections facade already exists. Namespaces:
   decision #9, settled — everything `zinc.*`, no `java.*`.
3. **Third-party BEAM packages = FFI** (`import erlang.*` stays the basement door), with
   curated wrappers only where earned — an HTTP server/router API over cowboy first
   (also dissolves GAP-10's `'_'` quoted-atom hack).
Build order (architecture-first — dogfoods test what exists, they don't drive design):
1. **Language v1 completeness spec** — settle, as one coherent design, the surface
   decisions the stdlib APIs depend on. **Generics: SETTLED (2026-06-10, designed with
   user):**
   - **Erased at lowering** (forced: BEAM has no reified types — same model as the JVM;
     specs/dialyzer are Erlang's own erased "generics"). Invariant, raw types legal,
     no inference.
   - **Gradual static checking at transpile time** — the point of having generics:
     every expression is known-typed (declarations, literals, record/Actor/facade/stdlib
     signatures) or unknown (`var` from FFI, `Object`); known-vs-known mismatch = error
     (exact nominal match — no subtyping lattice exists yet); unknown flows freely
     (the FFI basement, analogous to Java raw types/casts).
   - **Runtime boundary guards where unknown crosses into known** (Java's hidden
     `checkcast`, BEAM-idiomatic as guards + let-it-crash): typed binds, **typed Actor
     method entry (= checked message contracts — messages arrive from anywhere)**,
     typed record fields, insertion into known-type-arg collections. SHALLOW only
     (element at the crossing; never deep-scan a collection). Failure = structured
     `{zinc_badtype, expected, got, loc}` crash for supervision. Default ON; build
     flag to strip later if profiling justifies.
   **Modules**: fully-qualified class name = module, as a lowercase dotted atom —
   `util.MathUtil` -> `'util.mathutil'` (Elixir precedent). Collision-free; fixes the
   flat-namespace bug (today `class MathUtil` -> `mathutil` regardless of package).
   Root-package classes unchanged (`main:main()` entry stays).
   NO extension keywords: where an OTP concept needs surfacing, it gets a marker
   interface — Java's native signal for platform-managed semantics (`Serializable`);
   annotations rejected. Both markers are prelude (unqualified, like `String`), zero
   methods, checked by the interface-conformance machinery below.
   **Program model.** One program shape. A project has at most one class
   `implements Application` — the explicit root (the name Spring Boot, J2EE, and OTP
   all agree on; it lowers to an OTP application): it carries the OTP application
   identity (name/version from zinc.toml; [deps] boot before its tree), declares the
   root children as Actor-typed fields, hosts the optional `main(String[] args)`,
   receives SIGTERM, owns the exit code. The Application has no handle, no callers —
   it is the boundary, not a unit. A project without an `Application` is a library.
   A class `implements Actor` (the industry's word for it — Erlang/Akka lineage;
   also frees lowercase "process" to mean only the BEAM thing it lowers to) is the
   supervised stateful unit: fields (state + child processes), methods (void ⇒ cast,
   typed ⇒ call), the instance reference is the handle.
   **`new` spawns.** For an Actor class the instance IS the live process — BEAM has
   no created-but-not-running state, so zinc invents none (cf. Java collapsing the
   new/start two-step in `Thread.startVirtualThread`): `new` runs the constructor
   inside the freshly spawned process (OTP init) and returns the handle; on restart
   the runtime re-runs `new` with the same args, handle survives. `new` returns only
   when the constructor has completed (start_link semantics) — boot is
   deterministic: each child is fully constructed before the next sibling is born.
   Constructors stay cheap; heavy async work starts via a cast, not in the ctor. The contract,
   stated once: Java syntax, marker-declared semantics — `implements Actor` is the
   dev opting into process rules (serialized methods, surviving state, `new` = spawn).
   Liveness (the JVM non-daemon-thread rule): the program exits when `main` (if any)
   has returned AND no Actor instances are alive; otherwise it runs until stopped. Tool,
   server, and mixed programs fall out of this one rule.
   **Tree shape — composition is supervision, nothing hidden.** The tree is read
   from the source: the Application's Actor-fields are the root domain; an Actor's
   Actor-fields are its children — born in declaration order, shut down in reverse.
   No separate tree declaration.
   - Failure flows down, never up: an owner's death takes its domain (subtree
     restarts fresh, constructors re-run); a child's death never harms its owner;
     crash-looping domains escalate stepwise to the root, then the VM exits
     (systemd's layer, Phase 5).
   - Static children (fields) are permanent — always restarted.
   - Dynamic children (`new` in method bodies) join the spawner's domain,
     temporary — die with the owner, not restarted on crash; opt-in restart
     modifier later if a real program earns it. Siblings independent; fail-together
     coupling deferred.
   - Handles orthogonal to domains: lifecycle only; restarts never break handles.
   Lowering: Application -> OTP application + root supervisor; an Actor with children ->
   generated supervisor pair (owner first, children after, rest_for_one); worker
   processes never contain supervisor code.
   **Execution model.** All user code runs inside a BEAM process — no exceptions,
   no other execution contexts. Users create processes only via `new` on an Actor
   class (supervised, per the tree). Every other process is runtime- or stdlib-owned
   and temporary — `main`'s entry process (child of the root domain, runs once;
   crash logged, program exits nonzero if nothing else is alive), HTTP request
   handlers: spawned in the owning domain, never restarted, die with their owner. Supervisors are generated-only; library
   processes (FFI deps) belong to their own OTP apps. Threads are CUT: `Thread.startVirtualThread` never enters the language — a
   raw thread is an Actor with no name, state, or methods; fire-and-forget = cast or
   a temporary `new`; parallel fan-out/join is NOT a new shape — it is the
   worker-Actor idiom: construct (cheap ctor), kick (cast), join (call); mailbox
   FIFO makes the naive code correct (the join call queues behind the kick cast and
   cannot be answered early). No Future type — the supervision tree already owns the
   workers (dynamic children: owner dies ⇒ tasks die; task crashes ⇒ ladder applies
   at the join call). Java needed Future/StructuredTaskScope because it has no
   ownership tree. `Thread.sleep` stays.
   **Instance classes**: module + map term; fields are set by the constructor and then
   immutable — all objects are final. No setters, no field assignment after
   construction; mutable state lives in processes. Locals stay fully mutable (counters,
   temps, accumulators — the existing SSA machinery). Records' `p.x = v` mutation
   sugar is removed for consistency; build a new record instead. Mutability picture:
   locals mutate freely / object fields never / Actor fields across calls (serialized
   by the mailbox).
   **Interfaces**: nominal conformance checked at transpile time (`implements` = all
   methods present, signatures match); subtyping in the checker is one flat hop,
   class -> interface. Runtime: instance maps carry `'$class' => 'pkg.classname'`
   (Elixir `__struct__` precedent); interface call = Erlang dynamic module call
   (`(maps:get('$class', O)):method(O, ...)`). The same tag is what runtime boundary
   guards check for class-typed values. Lambdas satisfy SAM interfaces as plain funs
   (dispatch discriminates fun vs map with one guard); stdlib functional interfaces
   (Function, Predicate, Comparator...) live in the checker, cost nothing at runtime.
   Marker interfaces (zero methods) cost nothing anywhere; `Application` and `Actor`
   are the two well-known ones, special-cased by the transpiler.
   Deferred: default methods, `interface extends`.
   **Failure semantics — one ladder.** (1) Expected failures are exceptions: Java-style
   unwind to a typed catch. (2) A throw in an Actor call-method relays to the caller
   (catchable there); the process survives, state intact via transactional try; the
   generated wrapper catches ONLY the `{zinc_exc, Class, Fields}` shape — deliberate
   throws relay, bugs fall through. (3) Bugs crash the process; a caller mid-call on a
   crashed process exits with the same reason — not catchable in v1 (no retry against
   broken state); crash is transitive along a request. (4) Crashes hit domain policy
   (tree rules above). (5) Crash loops escalate to root -> VM exit nonzero -> systemd.
   Exception surface: `class NotFound extends RuntimeException` — the one sanctioned
   `extends` (unchecked: zinc failures auto-relay, Erlang-style, so a checked
   `extends Exception` would force `throws` clauses up every call chain). An explicit
   constructor is required; its `super(message)` supplies the message (the old
   auto-ctor-from-fields is gone). `throw` -> `erlang:error({zinc_exc, 'pkg.notfound',
   #{message => ...}})`. Typed catch matches the tag, clauses in order; `catch (Exception e)`
   is the catch-all and also catches native BEAM errors (badarith ~
   ArithmeticException; `getMessage()` renders the reason). Exceptions in cast methods
   crash the process — no caller to relay to. Philosophy, stated once: catch only what
   you have a plan for (input/network/external failures); everything else crashes —
   process granularity + supervision IS the recovery story. Deferred: `finally`,
   try-with-resources, exception hierarchies beyond one level.
   **Tags (atoms; closes GAP-10)**: `Tag.of("literal")` produces an Erlang atom
   — the FFI surface formerly `Atom.*`, renamed: a Tag labels protocol shapes (Erlang's
   own "tagged tuple" idiom); docs state "Tag = Erlang atom" once. `Tag.of("_")` ->
   `'_'`, resolved at transpile time; argument must be a compile-time string literal
   (atoms aren't GC'd — runtime minting is a leak; dynamic creation stays explicit via
   FFI `list_to_atom`). The field form `Tag.ok` was DROPPED (legal-Java gate: a Tag
   class can't enumerate the open atom set), so `Tag.of("ok")` is the one syntax; it
   covers non-identifier shapes (`_`, dots, uppercase) and Java-keyword names
   (`Tag.of("if")`). 255-char limit checked. User code models its own constants as enums
   (which lower to atoms); Tag is for foreign protocols only. Emitter rule, universal
   (Tag / enums / module names): emit an atom quoted whenever it isn't safely bare —
   non-lowercase-identifier shape OR Erlang reserved word (`Tag.end` -> `'end'`).
   THE V1 SPEC IS CLOSED. Revised post-close (2026-06-11): extension keywords
   ELIMINATED — `service`/`actor`/`spawn` are gone; the root is a class `implements
   Application`, the stateful unit a class `implements Actor` (marker interfaces,
   prelude), `new` on an Actor spawns. Zero non-Java syntax. Implementation order
   (spec leads; current code still has the old keyword/flat-sup/script model, Atom.*
   naming, mutable records): next is stdlib API design, then implement spec + stdlib,
   webdemo rewrite verifies.
2. **Decision #9 (namespace strategy): SETTLED — everything `zinc.*`** (see Open design
   decisions). Stdlib API design against that settled surface: HTTP client + HTTP
   server first.
   **`zinc.http.HttpClient` v1 — designed (2026-06-11).** Java-shaped, owned by zinc,
   over httpc. `HttpClient.newBuilder().connectTimeout(ms).proxy(host, port).build()`
   — timeouts are plain-int millis everywhere (no `Duration`); proxy + native-TLS-
   transport escape hatches are builder options (the firewall finding's sanctioned
   home). `HttpRequest.newBuilder(url).header(k, v).GET()/.POST(body)/.PUT(body)/
   .DELETE().timeout(ms).build()`; body is `String` or `byte[]` — one `POST(body)`
   method, not same-arity overloads (gradual checker accepts both; both lower to
   binaries, `byte[]` is the native case). Sync `client.send(req)` only — no
   `sendAsync`/futures (threads cut; fan-out = the worker-Actor idiom).
   Returns `HttpResponse`, a final value: `statusCode()`, `body()` (UTF-8 String),
   `bodyBytes()` (byte[]), `header(name)`. 4xx/5xx are responses, not exceptions.
   Expected failures are typed exceptions per the failure ladder:
   `zinc.http.HttpException` (one-level parent, the catch-all) with
   `ConnectException`, `TimeoutException`. `HttpClient` is plain config, no hidden
   state — safe in Actor fields.
   **`zinc.http` server v1 — designed (2026-06-11).** Backed by cowboy;
   process-per-request is cowboy's native model — surfaced, not hidden.
   - `HttpServer` is a stdlib Actor: `new HttpServer(port, router)` spawns it
     listening; as a field it is a static child — supervised, restarted, shut down
     with the tree. Declaration order does real work: state Actors declared
     before the server are born first; handlers close over their handles.
       final Store store = new Store();
       final HttpServer server = new HttpServer(8080, Router.create()
           .get("/users/{id}", req -> Response.ok(store.get(req.pathParam("id")))));
   - Routing is a programmatic table (Javalin/Spark — the no-annotation Java idiom):
     `Router.create().get(path, h).post(...)`, an immutable value. `{id}` path
     params (Spring/JAX-RS muscle memory) read via `pathParam("id")`; trailing `/*`
     wildcard. Lowers to cowboy_router dispatch — GAP-10's `'_'` hack disappears;
     unmatched = 404 for free.
   - `Handler` is a SAM interface (`Response handle(Request req)`): lambdas work; a
     class `implements Handler` when a route family grows. Handlers are stateless;
     state = closed-over Actor handles.
   - `Request`/`Response` are final values. Request: `method()`, `path()`,
     `pathParam(n)`, `queryParam(n)`, `header(n)`, `body()`/`bodyBytes()` — body
     materialized before the handler runs (no lazy streams in v1). Response: static
     factories + immutable chain — `Response.ok(body)`, `Response.status(404)`,
     `.header(k, v)`, `.body(body)`; body String or byte[] (same rule as the
     client). Naming rule in zinc.http: `Http*` = client side (built and sent),
     bare `Request`/`Response` = handler side (received and returned).
   - Failure: each request runs in its own temporary process in the server's
     domain. A handler bug crashes that request only — 500 + log; every other
     in-flight request and the server itself untouched. Return `Response` for
     expected outcomes; no try/catch ceremony. Uncaught zinc exceptions = same 500.
   - Deferred, named: TLS listen config, middleware/filters, static files,
     websockets/streaming.
   **JSON v1 — designed (2026-06-11): typed codecs + dynamic access; no path DSL.**
   Backed by OTP 27's built-in `json` module — zero deps.
   - The workhorse: the transpiler derives codecs on every record, routed through
     the `Json` facade (records can't carry `toJson`/`fromJson` in legal Java) —
     `Json.decode(User.class, s)` (parse + shape-validate, typed `{zinc_badtype...}`
     crash on mismatch), `Json.encode(u)`, and `Json.decodeAll(User.class, rows)`
     for column-keyed DB rows. The class literal names the target record;
     derivation is pure codegen (no reflection, no annotations); nested records
     compose. Lenient on extra fields, crash on missing; nullable/defaults later.
   - The fallback, for foreign JSON: `Json.parse(s)` returns unknown-typed data
     (maps/lists/scalars); `var` chaining flows freely (the FFI rule), and an
     explicit `.asInt()`/`.asText()`/`.asBool()`/`.asNum()` is the guarded crossing
     back into a known type — Python ergonomics with a
     checked boundary, ladder failure mode (no cast ceremony, no
     ClassCastException mysteries).
   - Deferred: path-string accessors (JsonPath/`at()`-style) — convenience over
     the fallback, adds a string mini-language; revisit if dogfoods earn it.
   **Resource doctrine + `zinc.sql` v1 — designed (2026-06-11).** Doctrine, stated
   once: long-lived resources are Actors — acquisition in the constructor,
   self-healing by restart, release by crash (the BEAM closes what a dead process
   owned). A pool is a supervision subtree, not a library trick.
   - `Db` is a stdlib Actor: `new Db(url, n)` spawns the pool parent + n connection
     Actors (permanent children). Connection ctor connects ⇒ fail-fast boot (DB
     down ⇒ crash-loop ⇒ escalate ⇒ VM exit nonzero ⇒ systemd; no half-up
     service); dropped connection self-heals (restart reconnects); mid-query crash
     closes the socket ⇒ Postgres rolls back server-side. The reconnect logic Java
     pools hand-write IS the supervision tree doing its job.
   - Queries don't serialize through the pool (poolboy shape): `db.query(sql,
     params...)` = checkout (fast pool call, hands back a connection) → query runs
     as a call to that connection Actor (n queries on n connections in parallel) →
     checkin. Pool exhausted ⇒ caller blocks; timeout (plain-int ms) ⇒
     `zinc.sql.SqlException`.
   - Transactions are a lambda — begin/commit/rollback unmismatchable:
     `db.transaction(tx -> { tx.exec(...); tx.exec(...); })`. One connection for
     the duration; return ⇒ commit; throw ⇒ rollback + relay (ladder rung 2,
     catchable); crash ⇒ disconnect rolls back (rung 3). The ladder maps onto SQL
     transaction semantics with zero new rules.
   - Rows: dynamic (var-chaining, like foreign JSON) + derived record mapping
     `User.from(rows)` — same pure codegen as `fromJson`, matched by column name.
     No ORM, no annotations, no class literals. Params positional, always prepared
     statements underneath — injection-safe by default; no string-concat path.
   - Postgres first (epgsql via FFI, vendored like cowboy); MySQL later, same
     shape. Migrations deferred — operational tooling, not language.
   **Logging v1 — designed (2026-06-11): the println/Log split.**
   - `System.out.println` stays honest stdout (`System.err` stderr) — Java
     behavior; CLI tools and hello-world produce clean, undecorated output.
   - `Log` joins the prelude: `Log.debug/info/warn/error(msg)` -> BEAM `logger`,
     where supervisor crash reports already land — app logging and runtime
     telemetry in one structured stream, one configuration. The transpiler injects
     module + file:line metadata at zero cost (known statically — no
     stack-walking like Java logging frameworks).
   - Default handler level: info (debug suppressed; enabling is a `zc run` flag /
     release config — Phase 4/5 detail). Under systemd both streams reach
     journald; the split exists for tools and first impressions, not services.
   **Testing v1 — designed (2026-06-11): marker + convention, lowered onto EUnit.**
   - Declaration: test classes under `test/`, `implements Test` (prelude marker);
     every public void method is a test case (JUnit 3 / EUnit convention — no
     annotations, no new syntax). Test code never ships in releases.
   - Execution: every test method runs in its own BEAM process (ExUnit's model),
     parallel by default. Perfect isolation — no shared statics, no ordering
     effects; a crashing test = a crashed process + crash report, runner
     unaffected. Actors a test spawns are its dynamic children: test ends, domain
     dies, everything reclaimed — no teardown callbacks. Supervision is testable
     for real: crash an Actor, assert the restart
     (`Assert.fails(() -> c.divideBy(0)); Assert.equals(0, c.value());`).
   - Assertions: prelude `Assert` — `equals`/`isTrue`/`fails(lambda)` (expected-
     exception variant's exact shape decided at implementation — no class
     literals). Failures throw; the ladder reports. Smart messages: the transpiler
     sees the assertion expression statically and emits operand source + values +
     file:line (ExUnit's power-assert trick at zero runtime cost — same machinery
     as Log metadata injection).
   - Seams: interfaces + hand-written fakes (a lambda suffices for SAMs) —
     explicit contracts, the Mox stance. No mocking framework; the bytecode magic
     Java mocks rely on doesn't exist here to honor anyway.
   - Runner: `zc test` lowers test classes onto EUnit (gleeunit's playbook):
     generated `*_tests` modules wrap each method in `{spawn,...}`/`{inparallel,
     ...}`; rebar3 eunit integration, reporting, and exit codes come free.
     Integration harness (boot the Application, hit it over HTTP) deferred —
     dogfood scripts cover it until something earns more.
3. Implement; webdemo rewritten with zero `Tuple.of`/`Atom.*` in user code verifies it.
Then `import elixir.*` FFI.

## Phase 3: second-tier language features (round out the language)
Interleave as needed — all incremental on the existing codegen:
- **string ops** — concat, length, split, contains, substring
- **array ops** — push/append, slice, contains; `xs[i] = v` (index assignment → persistent
  vector or explicit mutable `Buffer` per `LOWERING_SPEC.md` tiers)
- **surface `try`/`catch`** — user-facing error handling (throw/catch already proven)
- **`switch`/`match`** — pattern matching
- **tuples / multiple return**, optionally **lambdas / first-class functions**
- **DONE (2026-06-12): known-vs-known checking completed.** checkBind now fires
  at every declared-type position — returns (incl. void misuse + SAM lambda
  results), call args across all dispatch families, ctor args (records, instance
  classes, spawns, exception throws, Application children incl. builtin
  Db/HttpServer signatures), field inits, reassignments. Unknown still flows
  free (the FFI rule), runtime guards unchanged. Negative harness:
  `examples/neg/*.zinc` must fail transpile with the expected message (e2e).
- **DONE (2026-06-12): modifiers enforced** — see settled decision #10.
- **Collections-performance audit (2026-06-12) — findings + PROPOSAL (pending user).**
  Measured on this box (erlang:slim, 50k iterations): `xs.add(x)` in a loop =
  **3813 ms** (lowered `Cur ++ [X]`, O(n) each = O(n²) total); the same loop on the
  array module = **8 ms** (475×); `xs.get(i)` index-for over a list = **2260 ms**
  (lists:nth O(n) each). These are THE two everyday Java idioms — accumulate-into-list
  and index-for — so today's lowerings violate the LOWERING_SPEC forbidden tier
  ("never back arrays with list-update") in spirit.
  **SETTLED + IMPLEMENTED (user decision 2026-06-12; renamed 2026-06-15): `List` +
  `ArrayList`, different purposes.** Originally shipped as `ImmutableList` — but that
  name is Guava (`com.google.common.collect`), not JDK, so a `.zinc` using it would
  not compile under `javac` and broke the legal-Java rule. Renamed to `List` with
  `List.of`/`List.copyOf` — both real JDK (9/10) — which keeps every `.zinc` file
  legal Java. `List<T>` = receive/iterate (Erlang list; of/copyOf factories;
  get/size/contains/isEmpty + for-each fast path; mutators = transpile error — stricter
  than Java's mutable interface, so valid zinc stays a subset of valid Java).
  `ArrayList<T>` = build/index (array module: add appends at size O(log n),
  get/set O(log n), size O(1); new ArrayList<>(xs) copies in,
  List.copyOf(xs) bridges out; remove deferred). e2e `arraylist` proves it:
  500k appends + copyOf + iterate — the old ++ lowering would blow the suite timeout.
  Maps were left as-is (Map/HashMap, no perf hole); any future immutable-map naming
  must stay legal JDK (NOT Guava's `ImmutableMap`) — `Map`/`Map.of` is the legal home.
  **Already right (audited, no action):** for-each = direct tail recursion (1.85–2×
  foldl, beam-lab); `s += ...` = amortized-O(1) binary append (StringBuilder is
  UNNECESSARY in zinc — document this; concat chains flatten to one construction);
  maps = maps:* O(log n); `T[]` = array module incl. index-assign; String.split
  returns an array. **Notes:** string:length is grapheme-counting O(n) (semantically
  right; document "don't loop on length()"); '$idx' indexOf returns byte offsets.
  **Facade muscle gaps to fill incrementally (no design needed):** List.indexOf/
  sort/reverse/addAll, String.join/charAt/compareTo/format, Map iteration shape
  (entrySet/forEach), Math.pow/sqrt/floor/ceil/round, Arrays.sort/fill.
- **DONE (2026-06-12): e2e harness hardening** — exit code asserted on every case
  (right output + dirty exit = FAIL), 120s timeout guards hangs, per-case stderr
  contracts (logging: Log warning on stderr; selfheal: supervisor child_terminated
  report). Principle: assert the OBSERVABLE CONTRACT, not a proxy for it.
- **DONE (2026-06-15): legal-Java enforcement gate.** Every `.zinc` surface minus the
  `erlang.*` FFI basement now compiles under `javac` against a prelude jar
  (`zinc-prelude/zinc/*.java` — real Java stub types whose bodies throw
  `UnsupportedOperationException`, package `zinc`). `legaljava.sh` copies each program
  to `<PublicClass>.java` with prepended default imports (`java.util.*` + `zinc.*`,
  Kotlin/Groovy-style), runs javac, asserts zero errors; wired as the fast first phase
  of `e2e.sh`. **30/30 gated programs PASS, 6 FFI-exempt, e2e still 55/55.** Five
  surface changes the gate forced (each a transpiler change + example rewrite):
  (1) atoms are `Tag.of("x")` only — the field form `Tag.x` can't be legal Java;
  (2) JSON moved off records onto the `Json` facade — `Json.encode`/`decode(T.class,_)`/
  `decodeAll`, dynamic reads end in `.asInt()`/`.asText()`/`.asBool()`/`.asNum()`;
  (3) exceptions `extends RuntimeException` (unchecked, no `throws` clauses) with a
  required explicit ctor whose `super(message)` sets the message;
  (4) Application entry is instance `void main()` (Java 25) so the supervision-tree
  fields are legally reachable from the entrypoint; static `main(String[])` still
  accepted; (5) `Sys.sleep` replaces `Thread.sleep` (the latter's checked
  InterruptedException would need a throws clause). Principle: valid zinc is a subset
  of valid Java even for Erlang-shaped features — wrap, don't invent keywords.

## Phase 4: **SDK & toolchain — the one-stop shop**
One installer gets you everything; the dev kit manages the runtime. Rustup/flutter model:
**install dev kit → dev kit installs runtime**. No Docker, no system Erlang, no rebar3.
Working CLI name: `zc` (final language name is open decision #6 — rename then).

- **4.1 The `zc` CLI** (first, still runnable via Docker while the installer doesn't exist):
  - `zc run file.zinc` — transpile → erlc → run, one command, quiet unless it fails
  - `zc build` — project build into `_build/` (.beam + start script)
  - `zc new myapp` — scaffold (src/main.zinc, `zc.toml` with pinned toolchain+OTP versions)
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
- **4.3 Error source-maps** — map erlc/runtime errors back to `.zinc` spans (the transpiler
  tax; budget real time, it's what makes the toolchain feel native instead of leaky).
  **Slice 1 DONE (2026-06-12):** statements carry source lines end-to-end; every
  transpile error cites `<file>:<line>` (statement granularity; decl-level errors cite
  the file), and Assert failures embed `File.zinc:N` in the expr text (P8 deferral
  closed). Locked in by harness asserts. **Remaining:** runtime crash-frame mapping —
  design: transpiler emits a per-module map (function/arity -> source file + method
  line; statement-level later), `zc run`/`zc test` filter erl stderr and annotate
  frames like `counter.erl:23` with `(Counter.zinc:12 incr)`. Honest-lines approach —
  no -file/-line tricks that point at misleading nearby lines; generated .erl stays
  the truth, zc adds the source citation beside it.

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
- **Both artifact forms from one build**: (a) the plain OTP release tarball (the $10-VM
  story) named+hashed the same way — name-version native to the release (.rel pins app +
  ERTS versions), sha256 sidecar as its coordinate; (b) the OCI image wrapping that same
  release. Digests must be meaningful: reproducible builds via `erlc +deterministic` and
  normalized tar (sorted entries, fixed mtimes) so the same source+lock rebuilds to the
  same digest — green lights become verifiable.
- **Later in 5**: `--docker` flag to emit a minimal OCI image for teams that want it;
  multi-node clustering/distribution (BEAM's native distribution is the long-game moat);
  hot code upgrades only if ever justified (systemd restart is fine for the target user).

## Phase 6: polish & bets
- **Design north-star: minimize the dev's cognitive load so they spend it on logic, not
  ceremony.** Every feature is judged by what it lets the dev *stop* thinking about —
  familiar syntax (no new grammar), supervision (no hand-rolled recovery), walled-off
  FFI (you always know which mode you're in), good errors and an opt-in static net (the
  machine holds the boring, error-prone state). Tooling that offloads cognitive load
  beats tooling that adds power. This frames the items below: the docs story and the
  FFI static net are both cognitive-load offloading, not feature count. Let the tools
  help.
- **Documentation story — the dev-facing manual (BLOCKS adoption; do before any launch).**
  One coherent narrative that teaches the WHOLE surface, not a feature dump: the
  Java-as-syntax premise, the Actor/Application supervision model, the failure ladder,
  collections (`List` vs `ArrayList`, when each), JSON via the `Json` facade, the
  `zinc.http`/`zinc.sql` facades — AND the two escape hatches devs WILL hit: the
  `import erlang.<module>` FFI basement (call OUT to any loaded OTP/hex module;
  per-file opt-out of the legal-Java gate; unchecked → runtime `undef`) and the
  `zinc.toml` / `zc add` hex-dependency flow (declare = fetch+build+start; call = same
  FFI as stdlib). Show, don't list: a runnable example per feature, the generated
  Erlang side-by-side where it illuminates (atoms, failure relay), and a "coming from
  Java" page on what's deliberately different (unchecked exceptions, transactional
  `try`, no threads, `Sys.sleep` not `Thread.sleep`). The e2e examples are the source
  of truth — docs cite them so they can't drift. Form factor TBD (mdbook-style site vs
  in-repo guide).
- **Static net over the FFI basement (opt-in).** Inside zinc the surface is statically
  checked (legal-Java gate + known-vs-known types); the `import erlang.X` boundary is
  unchecked pass-through — "Python at the boundary." The BEAM already ships the tools to
  close this without zinc reinventing them: wire `rebar3 xref` (calls to functions that
  exist nowhere on the code path) and Dialyzer (success-typing: bad arity,
  type-incompatible FFI calls) into `zc` as an opt-in check. Covers hex deps too, since
  both analyzers see the whole loaded code path. Turns the basement from "find out at
  runtime" into "checked if you ask."
- **Optional checker / linter** on the AST (types as opt-in — the one thing worth stealing
  from Gleam: `Option` instead of null).
- **Name the language** (open decision #6) — needed by the time the installer/domain exists.
- (Much later) self-hosting — rewrite the compiler in this language.

---

## Open design decisions
1–5 (handle syntax, call/cast rule, supervision model, state mutation, multi-module always)
— **settled in `PLAN.md`**.
6. **language name** — TBD; blocks the installer domain + CLI final name, nothing before Phase 4.
7. **portable OTP distribution — SETTLED (2026-06-12): reuse hexpm/bob prebuilt
   builds** (builds.hex.pm — the Livebook/burrito source; checksummed via builds.txt,
   maintained by the hex team, zero infra for us). Flavor (ubuntu-22.04, -24.04) is
   picked EMPIRICALLY at install: download, run `erl` + load crypto NIFs on this box,
   first flavor that passes wins — no platform guesswork (verified: ubuntu-22.04 runs
   on el9; ubuntu-20.04 has no OTP-29 builds). Revisit owning the builds only if
   compliance/reproducibility or macOS/Windows demand it.
8. **`zc.toml` shape** — project manifest (name, version, toolchain pin, deps later in
   Phase 2-FFI era). Keep minimal; decide at `zc new`.
9. **stdlib namespace strategy — SETTLED (2026-06-11): everything `zinc.*`.** The
   language is a Java facade over Erlang; `java.*` names would promise JDK contracts
   the BEAM can't keep (threads, async, blocking-IO semantics). `zinc.*` owns the
   semantics; APIs stay Java-shaped so muscle memory transfers. Core facade (`String`,
   `List`, `Map`, `System.out`...) is an unqualified prelude, unchanged. New surfaces
   flat under zinc: `zinc.http` (client over httpc, server over cowboy), `zinc.net`
   (sockets over gen_tcp). `java.*` is reserved — declaring or importing it is a
   transpile error that names the zinc equivalent.
10. **modifier semantics — SETTLED + IMPLEMENTED (2026-06-12, "language should be
   rock solid").** Principle: accepted syntax is enforced or rejected, never
   silently decorative. As implemented: `private` = same-class only, transpile
   error elsewhere, and the honest lowering — private functions are NOT exported
   from the Erlang module (same-class qualified calls lower to local calls);
   rejected on Actor methods (an Actor's methods are its protocol; helpers belong
   in a utility class); interface implementations must be public. No modifier =
   public (no fake package-private). `protected` = error (no inheritance).
   `static` = required on utility-class methods, required+public on main,
   rejected on Actor/instance/Test methods and all fields. `final` = enforced on
   locals (no reassign; arrays are values so element-assign counts), on actor
   fields = set at construction (ctor may assign), frozen across messages;
   rejected on methods (no inheritance). Test classes keep the JUnit-3 shape
   (public void zero-arg = case). e2e: `modifiers` positive + 8 neg cases.

## Start here next session
**SPEC + STDLIB: IMPLEMENTED AND VERIFIED (2026-06-11).** The v1 spec is code: marker
interfaces + `new` spawns (P1), supervision-tree codegen (P2), Tag/final-records/FQ
modules/exceptions (P3), interfaces + instance classes + generics + boundary guards
(P4), stdlib Log / HttpClient / JSON / HttpServer-over-cowboy (P5). **Webdemo rewritten
on the new surface and green** — Application tree (Store actor + HttpServer child),
Router lambdas, derived JSON codecs, zero FFI / `Tuple.of` / `Tag.*` in user code:
GAP-9 + GAP-10 closed, cowboy side verified (see `dogfood/FINDINGS-webdemo.md` round 2;
SAM contextual lambda typing landed with it).
**Testing v1: IMPLEMENTED (2026-06-11).** `implements Test` under `test/` (methods
only; public void zero-arg = test case, rest helpers); lowers to an EUnit module —
`{setup, boot-dyn-sup, {inparallel, [{Name, {timeout, 60, {spawn, Case}}}]}}` —
process-per-test, parallel, actors a test spawns die with the test process (dynamic
children). Prelude `Assert.equals/isTrue/fails(zero-arg lambda)`; failures are
`{zinc_assert, #{expected, got, expr}}` with the operand's SOURCE TEXT (mini
unparser; file:line waits on source maps, Phase 4.3). `zc test` = rebar3 eunit
(test profile extra_src_dirs test/ recursive; test/zinc_gen never ships); plugin
transpiles src+test in ONE run (tests see src types). Verified in zc/test.sh:
green suite exit 0, red suite exit 1 + structured report. Noted: the
crash-then-assert-restart example needs a STATIC child — dynamic children are
temporary by design; restart assertions wait on the integration harness
(boot-the-Application) or the opt-in restart modifier.
**Shutdown story — settled (2026-06-12, designed with user).** Stop is outside-in:
SIGTERM (systemd/docker stop) -> OTP signal server -> init:stop -> reverse-order
tree drain, bounded by child-spec timeouts — WORKS TODAY, zero code. Liveness rule,
stated precisely: a program exits when main returns, unless the Application declares
static children (those serve until stopped); actors created in method bodies die
with the process that created them (main included — main is just the entrypoint
method, no special lifetime powers; the supervisor, not main, owns the durable tree,
which is what makes restart possible: a restart needs a recipe held by someone who
outlives the crash — fields donate theirs to the supervisor, methods consume theirs).
Resource cleanup needs NO feature: handles/sockets/ports/ETS die with their owning
process — kill-9-safe by construction; graceful shutdown is politeness, not
correctness. The one residual need, designed: **`close()` — finally at process
granularity** (AutoCloseable idiom). An Actor may declare `public void close()`;
lowering sets trap_exit and runs it as terminate/2 on ORDERLY stop only (tree
shutdown, owner's domain ending, test ending) — NEVER on the actor's own crash
(crashed state is untrustworthy; the crash path's cleanup is the resource side:
rollback, idempotency, supervision). Reverse declaration order + per-child timeout
bounds come free from the existing supervisors; close() is best-effort by
construction — anything that MUST happen belongs in a transaction, not a hook.
This is also why statement-level finally/try-with-resources stayed deferred:
zinc resources live in processes, not blocks. Implement alongside zinc.sql (its
connection actors are the first real user). `System.exit` stays unbuilt until a
dogfood (batch-job shape) earns it. Ctrl-C foreground = BEAM BREAK-menu wart,
Phase 4 zc-run polish; PID-1 exec in release start script = Phase 5 detail.
**zinc.sql + close(): IMPLEMENTED AND VERIFIED (2026-06-12).**
- `close()` is code: an Actor may declare `public void close()` — init sets
  trap_exit, terminate/2 runs the body on orderly reasons only (normal, shutdown,
  {shutdown,_}); never on the actor's own crash; excluded from cast dispatch and
  direct calls are a transpile error. e2e `close` proves the print arrives via the
  script-mode drain (eval-process exit -> linked root sup -> reverse-order tree
  shutdown), deterministic over repeated runs.
- `zinc.sql` is code: 'zinc.sql' runtime module emitted on use — pool_sup
  (rest_for_one) owns the manager (registered under the Db handle) + a one_for_one
  conn_sup of permanent connections (the P2 pair shape; a manager crash reconnects
  everything, a conn crash reconnects one). Connections epgsql:connect in init
  (fail-fast boot, restart-reconnect) and check themselves in; checkout monitors
  the borrower — a borrower crash kills the held conn (Postgres rolls back
  server-side, supervisor replaces it); waiters carry deadlines (pool exhausted
  blocks, then SqlException; expired waiters dropped at hand-out). Every statement
  is epgsql:equery — prepared + positional, no string-concat path. SQL errors are
  domain errors: builtin SqlException (zinc.sql.sqlexception), catchable; the conn
  survives. Conn terminate = the close() idiom hand-written (epgsql:close on
  orderly stop). Facade: `db.query/exec(sql, params...)` (varargs; query -> rows
  as list of column-name maps, exec -> count), `db.transaction(tx -> {...})`
  (lambda param contextually typed Tx; return = COMMIT, throw/crash = ROLLBACK +
  re-raise on the ladder; nesting = transpile error), `User.from(rows)` derived
  row mapping reusing the '$jmap' JSON codec (match by column name,
  crash-on-missing, lenient extras), dynamic rows via unknown .get var-chaining
  with guarded typed binds. Db is an Application field (static child, supervisor
  type/infinity shutdown); `new Db` elsewhere = transpile error pointing at the
  tree. List<T>.get(i) now yields the element type (typed rows compose).
  Verified against real Postgres: `dogfood/sqldemo/` (epgsql 4.7.1 vendored by zc
  through the corp firewall, postgres:16-alpine on a shared docker network) —
  insert/query/derived mapping/commit/rollback-on-throw/SqlException all
  asserted, SIGTERM drain observed. Known v1 waits (in FINDINGS-sqldemo.md):
  checkout/query timeouts not configurable; camelCase record fields vs
  snake_case columns unaddressed; no LISTEN/NOTIFY.
**Strictness pass DONE (2026-06-12, "language should be rock solid"):**
known-vs-known checking completed at every declared-type position + modifiers
enforced (decision #10 settled). Negative-test harness lives in e2e.sh
(examples/neg/, expected-message asserts); e2e is 52 cases (32 pos + 20 neg).
**Phase 4 first slice DONE (2026-06-12): the managed toolchain works.**
`zc toolchain install [ver]` downloads a pinned portable OTP from builds.hex.pm
(decision #7: reuse bob builds; sha256-verified, Java TLS through the corp
firewall) into `~/.zc/otp/<ver>`, runs ./Install, and sanity-checks (erl boots +
crypto NIFs load) — the check picks the flavor, not platform guesswork. rebar3
(one escript) is managed at `~/.zc/bin/rebar3`. `zc build/run/test/clean`
resolve the project's `[otp]` pin against `~/.zc/otp` and prepend its bin to
child PATH — **`zc init` -> `zc run` now works on a bare host: no docker, no
system Erlang** (docker remains the CI path). `zc doctor` reports versions,
toolchains, and pin resolution. Landed with it: project names validated as OTP
app atoms ([a-z][a-z0-9_]*), and a REAL P8 bug fixed — first `zc test` on a
fresh project ran 0 tests (rebar snapshots the test dir before the pre-compile
hook transpiles zinc_gen; eunit then misses the .erl). zc test now runs an
explicit `as test compile` pass first, and zc/test.sh asserts the TEST COUNT,
not just the exit code (the old harness was green for the wrong reason).
Remaining in Phase 4: the curl|sh installer + first-run TUI (4.2 — bundles a
pinned JRE next to OTP so zc needs no host Java; bootstrap script), `zc run
file.zinc` single-file mode, Ctrl-C BREAK-menu polish, and error source-maps
(4.3). **Open decision #6 (language name) is now the blocker for the installer
domain — needs the user.** `System.exit` still waits on a batch-job dogfood.
```
cd beam-transpiler && ./e2e.sh && ./zc/test.sh && ./dogfood/webdemo/test.sh && ./dogfood/sqldemo/test.sh   # green baseline: 31/31 e2e + zc(run+test) + webdemo + sqldemo
```
