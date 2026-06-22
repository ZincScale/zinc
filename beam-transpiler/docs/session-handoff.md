# Session handoff

Date: 2026-06-22

## Current focus

The BEAM transpiler is in the solidification/dogfood phase. The active direction is no
longer to add isolated missing APIs. Use `docs/solidification-plan.md` as the gate:
prove typical application completeness through `dogfood/flowdemo`, record real gaps by
architecture area, then implement one planned slice at a time with acceptance tests.

## What is done

- Added the active planning gate to `ROADMAP.md`.
- Added `docs/solidification-plan.md`.
- Added the canonical dogfood app under `dogfood/flowdemo`.
- Implemented the first config/resources slice:
  - `Config.decode(RecordType, path)`
  - `Files.join`
  - `Files.baseName`
  - `Files.extension`
  - deterministic sorted `Files.list`
  - `Files.dirName`
  - recursive sorted `Files.walk`
  - `Files.modifiedTime`
  - scoped resource reference handling for `TryStmt`
- Added `zinc-prelude/zinc/Config.java`.
- Added `examples/py/resources.zn` and wired it into `e2e-py.sh`.

## Current verification

These checks passed:

```sh
cd beam-transpiler
./dogfood/flowdemo/test.sh
./e2e-py.sh
```

`flowdemo` currently proves:

- JSON config decode into a typed record.
- Directory discovery via sorted `Files.list` and recursive `Files.walk`.
- Path construction and basename/dirname/extension helpers.
- Input filtering by extension and file modified-time metadata.
- Streaming input with nested scoped readers/writers.
- Success/failure routing.
- HTTP `/health` and `/status` checks.
- Worker crash/restart evidence through the black-box test.

## Remaining work

Before moving on, review and commit the current uncommitted path-discovery slice if it looks right.

Next implementation should come from `docs/solidification-plan.md`, likely one of:

- Scheduling/liveness: wall-clock time, monotonic elapsed time, timers, uptime/last-run
  metrics.
- Process orchestration: bounded worker pool, fan-out/fan-in, deterministic drain and
  worker crash behavior.
- Testing/release hardening: dogfood CI wiring, release smoke, service process harness.

Keep using `dogfood/flowdemo` as the acceptance target. Do not add more isolated stdlib or
compiler features unless the dogfood app or the solidification plan earns them.

