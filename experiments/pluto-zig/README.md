# pluto-zig

An experimental runtime for [Pluto-Lang](https://pluto-lang.org/)
(a Lua superset with proper OO syntax) implemented in Zig with a
Rust-side allocator. Long-term goal: a native-binary scripting
language with a real GC, designed as the prototyping tier above Zinc.

## Status

**Phase 0.5 (bdwgc-backed allocator) — done.** A real, production-
grade GC (Boehm-Demers-Weiser, the same collector Crystal and Mono
ship) runs underneath the Zig VM scaffolding. Allocation pressure
auto-triggers collection cycles; reclaimed memory is reused on the
next wave.

```
$ zig build run
[pluto-zig] phase-0.5 spike — bdwgc-backed
[pluto-zig] heap initialized
  [stats] before any allocations: gc_cycles=1 heap=256 KiB ...
[pluto-zig] obj a: tag=int payload=42
[pluto-zig] obj b: tag=boolean payload=1
  [stats] after 2 small objects: gc_cycles=1 heap=256 KiB ... objects=2

[pluto-zig] allocating 10000 x 256-byte blobs...
  [stats] after wave of blobs (refs dropped): gc_cycles=4 heap=768 KiB bytes_allocated=2657 KiB objects=10002

[pluto-zig] forcing GC cycle...
  [stats] after forced collection: gc_cycles=5 heap=768 KiB ...

[pluto-zig] phase-0.5 OK
```

See [docs/phase-0.5.md](docs/phase-0.5.md) for the bdwgc tradeoffs
and what comes next ([phase-0.md](docs/phase-0.md) covers the
underlying FFI plumbing).

## Build

Requires Zig 0.16, a recent Rust + cargo, and bdwgc installed
(`libgc-dev` / `gc-devel` / `brew install bdw-gc`).

```bash
bash vendor/setup-libgc.sh   # one-time: locate libgc.so and symlink it
zig build run
```

Cargo builds the Rust allocator crate (release profile) into a static
archive; Zig links the archive into the final executable along with
the system's libgc.

## Layout

```
src/
  main.zig    Entry point — phase-0 round-trip demo
  alloc.zig   Zig wrapper over the C ABI
alloc-rs/
  src/lib.rs  no_std Rust crate, malloc-backed allocator
              with the API shape mmtk-core uses
docs/
  phase-0.md  What's proven, what's next
build.zig     Cargo + Zig orchestration
```

## Roadmap

| Phase | Goal | Status |
|---|---|---|
| 0 | Zig <-> Rust FFI plumbing, malloc-backed alloc | done |
| 0.5 | Real GC via bdwgc (conservative, production-grade) | done |
| 1 | NaN-boxed TValue, Table, String types in Zig | next |
| 2 | Lua 5.4 bytecode VM (opcode dispatch, conformance subset) | |
| 3 | Pluto frontend linked, Pluto programs run end-to-end | |
| 4 | Stdlib parity (string / table / io / math / Pluto extras) | |
| 5 | REPL, source maps, error messages, perf tuning | |
| 0.7 | Swap bdwgc for mmtk-core (precise GenImmix) | deferred |
