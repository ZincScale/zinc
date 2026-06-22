# flowdemo design

Status: Phase 0 design. Do not implement runtime/compiler changes from this doc until the
initial app skeleton proves the gap is real.

## Goal

Build a small NiFi-style flow app in `.zn` that proves typical application composition:
configuration, file discovery, streaming ingest, typed/dynamic records, processor actors,
result routing, bounded parallelism, output, HTTP status, and failure isolation.

This is the product acceptance dogfood for the BEAM transpiler. It should use existing
language features first; any workaround becomes an explicit gap record.

## Runtime modes

`zc run --once .`

- Creates temporary input/output dirs.
- Writes a small config and input fixtures.
- Runs one bounded flow to completion.
- Prints deterministic summary lines for CI.
- Exits cleanly.

`zc run .`

- Boots `class Main(Application)`.
- Starts the flow service and HTTP status endpoint.
- Runs until stopped.

## Flow shape

```text
config -> input discovery -> FileReader -> parser -> workers -> router -> FileWriter
                                      \-> failed route
                                      \-> metrics/status
```

Minimum processors:

- `DiscoverFiles`: finds input files under a configured directory.
- `ParseJsonLine`: parses line-delimited JSON into `Row`.
- `ValidateRequired`: routes missing required fields to failure.
- `Enrich`: adds or transforms one field.
- `WriteRoute`: writes success/failure outputs.

## Data model

Use current features:

```python
enum FieldType { INT, STRING, DOUBLE, BOOL }
record Field(name: String, ftype: FieldType, required: boolean)
record Schema(name: String, fields: List<Field>)
record Row(schema: Schema, values: Map<String, Object>)
record FlowFile(path: String, attrs: Map<String, String>, row: Row)

sealed ProcResult {
    Success(ff: FlowFile)
    Routed(route: String, ff: FlowFile)
    Failed(reason: String, ff: FlowFile)
}
```

Use `record_model.zn` and `sealed.zn` as seeds, but move from example snippets to an app.

## Actor model

Initial app uses handwritten actors, not a new pool abstraction:

- `Metrics(Actor)`: counts processed/succeeded/failed and last error.
- `Parser(Actor)`: drains line channel, emits `ProcResult` channel.
- `Worker(Actor)`: drains result/input channel, validates/enriches/routes.
- `Writer(Actor)`: drains success/failure channels and writes output.
- `Flow(Actor)`: owns channels and starts one run.
- `Main(Application)`: owns `Flow`, `Metrics`, and optional `HttpServer`.

This intentionally tests whether current Actor + Channel primitives are enough before
adding a stdlib pool/executor.

## HTTP status

Service mode exposes:

- `GET /health` -> `ok`
- `GET /status` -> JSON summary from `Metrics`
- `POST /run` -> starts a flow run if idle, returns accepted/conflict

No middleware, static files, or request-body streaming in v1.

## Test script

`dogfood/flowdemo/test.sh` should assert:

- `zc run --once .` exits 0.
- stdout summary is deterministic.
- success output file contains expected transformed records.
- failure output file contains expected failures.
- service mode starts, `/health` and `/status` respond, then shuts down.
- one processor crash is isolated or restarted, and metrics/status show the flow continues.

Keep tests black-box. Assert output, exit code, files, HTTP responses, and source-mapped
stderr where relevant.

## Initial implementation rule

Phase 1 must use existing features only. Allowed local helper functions/classes are fine.
Compiler/runtime changes are not allowed until the first skeleton records a concrete gap.

## Gap decisions

| Area | Initial decision | Reason | Revisit trigger |
| --- | --- | --- | --- |
| Config | Use JSON config via `Json.parse` or `Json.decode` | Existing capability; enough to validate app shape | Repeated boilerplate or poor diagnostics |
| Path discovery | Start with explicit configured file list | Avoid adding path/glob API before app exists | Skeleton needs directory polling/listing |
| Scheduling | No periodic polling in first pass | `--once` should be deterministic | Service mode needs recurring scans |
| Pool/executor | Handwritten N workers + channels | Tests current primitives first | Duplication or unclear failure semantics |
| Metrics | `Metrics(Actor)` | Fits BEAM/process model | Multiple apps need same pattern |
| HTTP | Health/status/run only | Current Router is enough | Middleware/errors/streaming needed |
| Regex | Avoid | Not needed for JSON-line first pass | CSV/log parsing earns it |
| Distribution | Out of Phase 1 | Needs separate design | Flowdemo local mode is solid |

## Expected Phase 1 stdout

Exact lines can change when implemented, but keep this style:

```text
processed=4
success=3
failed=1
health=ok
restart=ok
```

## Phase 1 exit criteria

- `dogfood/flowdemo` is a runnable `.zn` project.
- `test.sh` passes on a clean checkout with managed toolchain available.
- No compiler/runtime changes were made.
- `docs/solidification-plan.md` gap list is updated with actual friction.

## Phase 1 skeleton notes

The first skeleton uses existing features only and passes `test.sh`. Resource slice updates are now incorporated.

Observed friction:

- Actor call methods need a single final `return`; branch returns inside `try` are rejected.
- Nested scoped `Writer` resources originally hit a local/resource resolution issue; the resource slice fixed it and flowdemo now uses nested scoped writers.
- File discovery uses sorted `Files.list` plus `Files.join`; walk/glob remain deferred.
- Service-mode HTTP status is implemented in the skeleton via in-app client checks.

## Service-mode slice

Implemented with existing HTTP support:

- `HttpServer` is a static `Application` child on port 18081.
- `GET /health` returns `ok`.
- `GET /status` returns the `Metrics` summary.
- The deterministic `--once` path validates these routes with `HttpClient` after the flow run.

This keeps the test self-contained; a separate long-running process harness can be added later.
