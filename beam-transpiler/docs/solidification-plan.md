# Solidification plan

Status: design gate. Do not implement isolated gaps from this file without first tying
them to the canonical app acceptance target and the architecture slice they belong to.

This plan exists to avoid the trap of quick feature patches followed by many small
correction loops. The current `.zn` surface already runs a broad BEAM path through
`e2e-py.sh`; the next work should prove typical-application completeness and harden the
tooling around that proof.

## Source of truth

Use these as the current-state baseline:

- `e2e-py.sh`: authoritative `.zn` behavior, positive and negative cases.
- `examples/py/`: runnable feature examples.
- `docs/guide.md`: user-facing language contract.
- `zc/Zc.java`: real tool behavior.
- `ROADMAP.md` "Start here next session": historical plan and deferred frontier.

Older Java-surface roadmap sections are design history unless the current `.zn` code and
`e2e-py.sh` still exercise them.

## Current baseline

The current `.zn` path already supports:

- Script and project entry points: top-level `def main()` and `class Main(Application)`.
- Actors and supervision: `class T(Actor)`, typed call vs void cast, static and dynamic
  children, restart with stable handles, `close()` on orderly shutdown.
- Python-shaped surface: implicit `self`, typed params, inferred locals/returns, f-strings,
  `if`/`else if`, `while`, `for`, `range`, `match`, lambdas, `try`/`except`, `raise`.
- Data model: records, enums, sealed unions, nominal interfaces, instance classes, typed
  list/dict literals, dynamic crossings from foreign JSON/maps.
- Stdlib: `Json`, `Files`, scoped `Reader`/`Writer`, `Channel<T>`, file pump actors,
  `HttpClient`/`http` facade, `HttpServer`/`Router`, `Db`, `Log`, encoding/crypto helpers.
- Tooling: `zc run/build/test/check/fmt/release`, managed OTP, source-mapped run errors,
  opt-in xref/dialyzer over FFI.

The biggest remaining risk is not syntax breadth. It is whether these pieces compose into
a representative app with stable tooling and no hidden runtime or diagnostic holes.

## Canonical app target

Create one dogfood app that acts as the product acceptance test: `dogfood/flowdemo`.
Its design lives in `dogfood/flowdemo/DESIGN.md`.

It should model a small NiFi-style flow engine because zinc-flow is the canonical feature
pressure source:

1. Load typed config.
2. Discover or receive input files.
3. Stream records in bounded memory.
4. Parse JSON/CSV-like records into a runtime schema/row model.
5. Process rows through actors.
6. Route results through a sealed `Result` type: success, routed, failed.
7. Run bounded parallel workers with backpressure.
8. Write output and failure routes.
9. Expose HTTP health/status/metrics.
10. Crash a worker and prove supervision or process isolation keeps the flow alive.

Acceptance criteria:

- Runs through `zc run --once` for deterministic CI mode.
- Has a long-running service mode with `HttpServer`.
- Uses only `.zn` sources unless a missing FFI wrapper is intentionally being tested.
- Has a test script that asserts output, status endpoint, and one crash/restart behavior.
- Every feature gap discovered while building it is recorded in this document before
  implementation.

## Gap categories

### A. Application configuration

Typical apps need layered config before they need more syntax.

Required design questions:

- Manifest vs runtime config: what belongs in `zinc.toml` and what belongs in app config?
- Supported format for v1: TOML, JSON, or simple properties.
- Typed decode shape: record decode, dynamic map, or dedicated `Config` facade.
- Env overrides and defaults.
- Missing/invalid config diagnostics with file:line or key path.

Do not quick-fix by adding one `Files.readString` helper example. The architecture should
define one blessed config path and use it in `flowdemo`.

Likely v1:

- `Config.load(path)` returns dynamic map-like data.
- `Config.decode(RecordType, path)` mirrors `Json.decode`.
- Env lookup stays `System.getenv`.

### B. Path and file discovery

`Files` covers direct reads/writes and streaming, but typical file-ingest apps need path
operations.

Required surface:

- path join
- basename, dirname, extension
- list directory with filtering
- recursive walk or glob
- file metadata needed by ingest processors

Design concern:

- Avoid pretending to be `pathlib` unless the API is complete enough. Prefer a small
  `Path`/`Files` facade with explicit BEAM-backed semantics.

Acceptance target:

- `flowdemo` can scan an input directory, select `*.json` or `*.csv`, process each file,
  and move/write outputs.

### C. Time, scheduling, and liveness

Typical services need periodic work and timestamps.

Required surface:

- current wall-clock timestamp
- monotonic elapsed time
- sleep already exists via `Sys.sleep`
- periodic scheduler or timer actor
- timeout settings for workers and queues

Design concern:

- Timers are processes and ownership matters. A scheduler must have a supervision/lifecycle
  story, not just a static helper that leaks work.

Acceptance target:

- `flowdemo` can poll a directory or expose uptime/last-run metrics without ad hoc loops in
  `main`.

### D. Process orchestration

The roadmap's next non-deploy frontier is a distribution-aware process orchestration layer.
This should come after, or at least be designed with, distribution in mind.

Required concepts:

- bounded worker pool
- fan-out/fan-in
- backpressure by default
- worker crash behavior
- ordered vs unordered output
- graceful drain/stop
- metrics hooks

Design concern:

- Do not introduce futures/promises unless the actor model fails a concrete use case.
  The expected primitive should be built from Actor + Channel + supervision.

Likely v1:

- A stdlib `Pool<TIn,TOut>` or `Executor` Actor.
- Fixed-size strategy first.
- Per-task and work-stealing strategies deferred until `flowdemo` or distribution earns
  them.

Acceptance target:

- `flowdemo` processes records through N workers with bounded memory and deterministic
  shutdown.

### E. HTTP service completeness

HTTP is present, but typical apps need more than simple routes.

Gaps to validate:

- request body streaming for large uploads
- middleware or filters
- static file serving
- health/readiness helpers
- structured error responses
- route-level metrics

Design concern:

- Keep handler state as actor handles; do not smuggle mutable globals into HTTP helpers.

Acceptance target:

- `flowdemo` exposes `/health`, `/metrics` or `/status`, and returns useful error bodies for
  expected failures.

### F. Observability

Supervision is only product-grade if operators can see what happened.

Required surface:

- structured logs with module/file/line metadata
- counters and timers
- actor/pool health
- request/processing counts
- crash reports mapped back to `.zn`

Design concern:

- Decide whether metrics are a stdlib Actor, an HTTP helper, or both. Avoid embedding
  metrics logic in every demo actor.

Acceptance target:

- `flowdemo` reports processed/succeeded/failed counts and last error.

### G. Testing and CI hardening

The current e2e suite is strong but mostly feature-example oriented.

Required additions:

- `.zn` negative tests comparable in breadth to legacy `.zinc` negatives.
- Compatibility test for explicit `self.field` if that remains supported.
- `zc test` source-map coverage for EUnit crash output.
- Dogfood app tests.
- Release smoke: build release, boot it, hit health endpoint, stop cleanly.
- CI job split: fast transpiler tests, BEAM e2e, Docker/Postgres/integration.

Design concern:

- Tests should assert observable contracts: output, exit code, stderr/log contract, source
  locations, generated release boot behavior. Avoid tests that only assert a proxy.

### H. Distribution

The roadmap calls distribution the next major proof point. This is not just a deployment
feature; it affects process orchestration design.

Required design questions:

- node naming and cookies/secrets
- how `zc run` starts multiple local nodes for demos/tests
- remote actor addressing model
- failure behavior across node disconnects
- distribution-aware pool placement
- how releases configure clustering

Acceptance target:

- Two-node local demo: node A accepts input/status, node B runs workers, worker crash is
  isolated, node disconnect has a clear failure mode.

## Non-goals until earned

Do not prioritize these without a `flowdemo` or distribution use case:

- comprehensions
- generators/yield
- decorators
- keyword/default args
- broad exception hierarchy
- full stream API
- general-purpose sorting API
- reflection or service loading
- shared-memory concurrency primitives

These may be useful later, but they are not the shortest path to a typical BEAM-backed
application.

## Implementation sequence

### Phase 0: design lock

Deliverables:

- This document reviewed and amended.
- A `flowdemo` one-page design with source layout, runtime modes, expected output, and
  test assertions.
- A gap decision table: implement, defer, or cover by FFI.

Exit criteria:

- No code changes beyond docs and optional skeleton files.
- Every planned implementation item has an acceptance test identified.

### Phase 1: flowdemo skeleton with existing features only

Build the canonical app using only current language features.

Expected result:

- It will reveal real gaps without speculative implementation.
- Any workaround must be documented in the app README and back-linked here.

Exit criteria:

- The app runs and tests, even if it uses small local workaround functions.
- Gap list is updated from actual friction.

### Phase 2: first architecture slice

Pick the highest-leverage gap from Phase 1. Likely candidates:

- `Config`
- `Path`/file discovery
- app test harness
- process pool

For the chosen slice:

- write API design
- update docs
- add positive examples
- add negative diagnostics
- implement
- run full relevant suites

No other gap rides along unless it is a dependency named in the design.

### Phase 3: tooling hardening

Before adding more features:

- CI matrix
- `zc test` mapped crash output
- release smoke test
- dogfood app test integration

### Phase 4: distribution proof

Implement the two-node demo and the minimum tool support needed to run it locally.

### Phase 5: orchestration stdlib

Design and implement worker-pool/fan-out primitives, using the distribution lessons so the
API is location-transparent from the start.

## Decision record template

Every feature added from this plan should get a small record:

```text
Decision:
Context:
API:
Lowering/runtime model:
Failure behavior:
Type behavior:
Tests:
Docs:
Deferred:
```

If any of those fields are unclear, the feature is not ready to implement.

## Observed flowdemo Phase 1 gaps

These came from building the first `dogfood/flowdemo` skeleton with existing features only.

| Gap | Evidence | Decision |
| --- | --- | --- |
| Actor call methods require one final return | `Worker.process` could not return from branches inside `try`; transpiler rejected it with the existing v1 rule | Not a compiler change now. Keep examples in final-return style; revisit only if real code becomes awkward. |
| Nested scoped writers exposed local/resource friction | `with Files.openAppender(successPath)` inside loops originally failed because resource initializers were missing from free-variable analysis | Closed in resource slice: resource initializer refs now participate in loop capture; `resources.zn` and `flowdemo` use nested scoped writers. |
| File discovery was too raw | `Files.list(inputDir)` had no stable order or path helpers | Partially closed: `Files.list` is sorted; added `Files.join/baseName/extension`. Walk/glob deferred. |
| Service-mode test is self-contained but not external | `flowdemo` validates HTTP via in-app `HttpClient`, not a separate long-running harness | Good enough for Phase 1. Add external harness only for release/deploy smoke. |
| Service tests need port allocation | First HTTP slice failed on fixed port 8081 with `eaddrinuse` | Use a high fixed port for now. Design a test-port allocation strategy before adding more service dogfoods. |

## Resource slice design: Files + Config + scoped handles

Decision: keep this as a small library/tooling slice, not a language expansion. Implemented in this slice.

API:

- `Files.list(dir)` returns names in stable sorted order.
- `Files.join(a, b)` joins one path segment without duplicate slash handling surprises.
- `Files.baseName(path)` returns the final path segment.
- `Files.extension(path)` returns the suffix after the final dot in the base name, or empty string.
- `Config.decode(RecordType, path)` reads a small JSON config file and decodes it through the existing derived record codec.

Lowering/runtime model:

- Path helpers live in `'zinc.io'`; they are pure binary/list operations over file paths.
- `Config.decode` is compile-time sugar over `Json.decode(T, Files.readString(path))`; no new runtime module.
- Scoped resource fix: resource initializer expressions participate in loop/free-variable analysis, so nested `with Files.openWriter(successPath)` inside loops captures `successPath` correctly.

Failure behavior:

- File errors remain `IOException` through existing `zinc.io` helpers.
- Config parse/type errors use the existing JSON/type failure ladder.

Deferred:

- recursive walk/glob
- path normalization/absolute/relative APIs
- env overrides/layered config
- TOML/YAML
