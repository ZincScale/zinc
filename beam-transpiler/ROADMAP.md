# Zinc-to-BEAM Roadmap

This roadmap tracks the canonical `.zn` Zinc surface on BEAM. Retired alternate frontends
are no longer release targets, examples, or acceptance gates.

## Phase 1: Canonical-Only Surface

Status: implemented in-tree.

- Active examples live under `examples/zinc` and `examples/zinc_neg`.
- Active gates are `./e2e-zinc.sh`, `./zc/test.sh`, and `./rebar_zinc/test.sh`.
- `zc init`, `zc run`, `zc fmt`, the raw compiler entrypoint, and the rebar plugin use
  `.zn` only.
- Retired frontend gates and examples have been removed from the active tree.

## Phase 2: Compiler Naming and Architecture Cleanup

Status: implemented in-tree.

- Canonical frontend classes are `ZincLexer`, `ZincParser`, and `ZincInfer`.
- The inactive legacy parser and lexer have been removed.
- Diagnostics and maintained comments refer to canonical Zinc and `.zn` sources.

## Phase 3: Project and Test Model

Status: implemented in-tree.

- Project-mode example: `examples/zinc/project_app`.
- Canonical application root: `class Main : Application`.
- Canonical actor declaration: `class Counter : Actor`.
- Canonical tests: `class CounterTest : Test`, where each zero-argument `void` method is
  an EUnit test case.
- Source maps and assertion failures report `.zn` locations.

## Phase 4: Tooling Polish

Next.

- Tighten `zc doctor` around the canonical toolchain.
- Broaden `zc fmt` coverage over project trees and test trees.
- Add release smoke coverage for package/install paths.

## Phase 5: Release Readiness

Next.

- Package only canonical compiler/runtime/plugin assets.
- Keep release docs aligned with the three canonical gates.
- Add CI once the manual release checklist is stable.
