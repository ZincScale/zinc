# pluto-zig

An experimental runtime for [Pluto-Lang](https://pluto-lang.org/)
(a Lua superset with proper OO syntax) implemented in Zig with a
Rust-side allocator. Long-term goal: a native-binary scripting
language with a real GC, designed as the prototyping tier above Zinc.

## Status

**Phase 0 (FFI plumbing) — done.** Zig program calls into a Rust
allocator crate over a C ABI, allocates and reads typed objects,
prints lifetime stats, shuts down cleanly.

```
$ zig build run
[pluto-zig] phase-0 spike
[pluto-zig] heap initialized
[pluto-zig] obj a: tag=int payload=42
[pluto-zig] obj b: tag=boolean payload=1
[pluto-zig] obj c: tag=str_short payload=0xdeadbeef
[pluto-zig] lifetime stats: 3 objects, 48 bytes
[pluto-zig] phase-0 OK
```

See [docs/phase-0.md](docs/phase-0.md) for what this proves and
what comes next.

## Build

Requires Zig 0.16 and a recent Rust + cargo.

```bash
zig build run
```

Cargo builds the Rust allocator crate (release profile) into a static
archive; Zig links the archive into the final executable.

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
| 0.5 | Swap stub allocator for mmtk-core (GenImmix) | next |
| 1 | NaN-boxed TValue, Table, String types in Zig | |
| 2 | Lua 5.4 bytecode VM (opcode dispatch, conformance subset) | |
| 3 | Pluto frontend linked, Pluto programs run end-to-end | |
| 4 | Stdlib parity (string / table / io / math / Pluto extras) | |
| 5 | REPL, source maps, error messages, perf tuning | |
