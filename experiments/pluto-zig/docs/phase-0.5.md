# Phase 0.5 — bdwgc as the allocator

## What this proves

Phase 0 demonstrated the FFI plumbing with a malloc-backed stub. Phase
0.5 replaces the stub with **bdwgc** (the Boehm-Demers-Weiser
conservative GC) — a real, production-grade collector used by Crystal,
Mono, and GCC's compiler infrastructure. The C ABI between Zig and
Rust is unchanged; only the bodies of the allocator functions differ.

Demo output:

```
[pluto-zig] phase-0.5 spike — bdwgc-backed
[pluto-zig] heap initialized
  [stats] before any allocations: gc_cycles=1 heap=256 KiB bytes_allocated=0 KiB objects=0
[pluto-zig] obj a: tag=int payload=42
[pluto-zig] obj b: tag=boolean payload=1
  [stats] after 2 small objects: gc_cycles=1 heap=256 KiB bytes_allocated=0 KiB objects=2

[pluto-zig] allocating 10000 x 256-byte blobs...
  [stats] after wave of blobs (refs dropped): gc_cycles=4 heap=768 KiB bytes_allocated=2657 KiB objects=10002

[pluto-zig] forcing GC cycle...
  [stats] after forced collection: gc_cycles=5 heap=768 KiB bytes_allocated=2657 KiB objects=10002

[pluto-zig] phase-0.5 OK
```

What to read from those numbers:

- `gc_cycles` jumps **1 → 4** during the 10k-object wave. bdwgc decided
  on its own that a collection was warranted three times mid-loop. We
  didn't ask. That's a real collector reacting to allocation pressure.
- `bytes_allocated` lifetime is **2.6 MiB**, but the heap is only
  **768 KiB**. The collector reclaimed roughly 2/3 of the allocations
  during the run. Without GC, the heap would equal the lifetime total.
- `heap_size` doesn't shrink after the forced collection. bdwgc keeps
  reclaimed pages for reuse rather than returning them to the OS —
  this is policy, not a bug. The next wave of allocations will reuse
  those slots without growing the heap.

## What changed from phase 0

Only `alloc-rs/src/lib.rs` and a tiny build-side detail. The Zig main
gained a few lines to print stats and trigger a forced GC for the
demo, but those use the same wrapper API.

| File | Phase 0 | Phase 0.5 |
|---|---|---|
| `alloc-rs/src/lib.rs` | malloc + free | `GC_init` + `GC_malloc` |
| `src/alloc.zig` | + `deallocStub` exposed | `forceGc` + heap stats exposed |
| `build.zig` | — | links `-lgc` from `vendor/lib/` |
| `vendor/lib/libgc.so` | — | symlink to system `libgc.so.1` |

## bdwgc's tradeoffs

**Conservative scanning.** bdwgc finds roots by scanning the stack,
registers, and heap memory for any 8-byte aligned value that *looks*
like a pointer into a known heap object. It doesn't know object
layouts; it doesn't trust type information. This means:

- **Pro**: zero binding work. We don't tell it about our objects'
  internal layouts; it figures it out by tracing memory.
- **Con**: false retention. Any value that happens to look like a
  pointer (a large integer, a misinterpreted floating-point bit
  pattern) keeps live whatever it "points at." On 64-bit systems the
  false-positive rate is low, but it's nonzero.
- **Con**: no copying / compaction. Heap fragments over long-running
  programs.

These are the well-known costs of conservative collection. Crystal
runs on bdwgc in production and ships real programs with it; the
tradeoffs are real but not deal-breakers for a scripting language.

## Why not mmtk yet

mmtk-core gives precise generational collection (GenImmix) with sub-10ms
pauses, but the cost is implementing the `VMBinding` trait plus
`ActivePlan`, `Collection`, `ObjectModel`, `ReferenceGlue`, and
`Scanning`. Reference bindings (mmtk-julia, mmtk-jikesrvm) run
1500-3000 lines of Rust each. That's a focused 1-2 week investment,
and worth doing properly when we're ready to graduate.

Phase 0.7 is the mmtk swap. Phase 1 (TValue, Table, String, opcode
dispatch) is the better next step from here — bdwgc gives us a
working collector to build the VM against, and the mmtk graduation
becomes the engineering prize after the language has shape.

## Build notes

The project ships a `vendor/lib/libgc.so` symlink pointing at the
system's `libgc.so.1`. This avoids requiring the `gc-devel` package
to be installed (which is what would normally provide the unversioned
`libgc.so` name the linker looks for).

If your system's libgc lives elsewhere, replace the symlink:

```bash
ln -sf /path/to/libgc.so.1 vendor/lib/libgc.so
```

Phase 0.7 won't need this — mmtk-core compiles into the static
archive, no system library hunt.
