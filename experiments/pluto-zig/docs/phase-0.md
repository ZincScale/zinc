# Phase 0 — FFI plumbing

## What this proves

A Zig executable can:

1. Initialize a heap-like handle in a Rust crate via a C ABI
2. Allocate memory through that crate (with size + alignment) and get a pointer back
3. Write a typed object (an extern struct with a tag + payload) into that memory
4. Read the same data back, getting matching values
5. Read atomic counters that the Rust side updated, proving the FFI is two-way
6. Tear the heap down cleanly

In other words: the **structural plumbing** for a Zig-VM + Rust-allocator architecture is real and the binary actually runs.

## What this does NOT prove

- **There is no GC.** The allocator is malloc-backed; freeing is manual via `pluto_dealloc_stub`. Replacing this with a real collector is Phase 0.5.
- **There is no VM.** No bytecode dispatch, no opcodes, no objects-with-references. Phase 1 builds the Lua-compatible value representation and starts an opcode dispatch loop.
- **There is no Pluto frontend yet.** Phase 3 vendors Pluto's parser and emits bytecode the VM can run.

## Architecture as proven

```
src/main.zig          — entry point, demonstrates the round-trip
src/alloc.zig         — Zig wrapper over the C ABI
alloc-rs/              — Rust crate, no_std, exposes pluto_* C symbols
build.zig             — orchestrates cargo build + Zig link
```

Build: `zig build run`. Cargo runs first (Rust → static archive),
Zig links the archive into the executable.

## The mmtk swap (Phase 0.5)

The Rust crate is the only thing that changes. `alloc-rs/Cargo.toml`
gains an `mmtk-core` dep; `alloc-rs/src/lib.rs` replaces the
malloc-backed bodies with mmtk plan + collector calls. The C ABI
(`pluto_heap_init`, `pluto_alloc`, `pluto_post_alloc`, ...) stays
identical, so neither `src/alloc.zig` nor `src/main.zig` changes.

The hard part of Phase 0.5 is implementing mmtk's **VMBinding** trait:
- `Slot`, `MemorySlice` associated types
- `ActivePlan` — tells mmtk how to find threads
- `Collection` — stop-the-world coordination
- `ObjectModel` — header layout, copy semantics
- `ReferenceGlue` — weak references / finalization
- `Scanning` — walk roots and trace objects

mmtk-julia and mmtk-jikesrvm are the reference implementations to crib
from. Plan to spend 1–2 weeks on the binding before allocation flows
through GenImmix end-to-end.

## Notes on glibc + Zig + Rust

The crate is `#![no_std]` to avoid Rust std pulling in `std::fs` /
`backtrace_rs` paths whose legacy glibc symbols (`stat64`, `lstat64`,
`fstat64`) Zig's linker can't resolve.

Three small shim symbols in `lib.rs` cover RHEL 8's old crt1.o:
`__libc_csu_init`, `__libc_csu_fini`, `rust_eh_personality`. None are
called at runtime — they're appeasing the static linker's symbol
resolution. On modern Ubuntu / Arch / Alpine these shims are harmless.

`panic = "abort"` in both `[profile.release]` and `[profile.dev]` keeps
the `_Unwind_*` symbols out of the archive entirely.
