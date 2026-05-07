//! Zig wrapper over the Rust pluto_alloc C ABI.
//!
//! Phase 0.5: backed by bdwgc. Reclamation is automatic — no
//! caller-side dealloc. Phase 0.7 will swap to mmtk-core's GenImmix
//! behind the same C symbols, so this file does not change.

const std = @import("std");

pub const Heap = opaque {};

extern fn pluto_heap_init(heap_bytes: usize) ?*Heap;
extern fn pluto_heap_shutdown(heap: ?*Heap) void;
extern fn pluto_alloc(heap: ?*Heap, size: usize, alignment: usize) ?*anyopaque;
extern fn pluto_post_alloc(heap: ?*Heap, obj: ?*anyopaque, size: usize) void;
extern fn pluto_force_gc() void;
extern fn pluto_stat_bytes_allocated() usize;
extern fn pluto_stat_objects_allocated() usize;
extern fn pluto_stat_heap_size() usize;
extern fn pluto_stat_gc_count() usize;

pub fn init(heap_bytes: usize) !*Heap {
    return pluto_heap_init(heap_bytes) orelse error.HeapInitFailed;
}

pub fn shutdown(heap: *Heap) void {
    pluto_heap_shutdown(heap);
}

/// Allocate a GC-managed block. The returned pointer is tracked by
/// the collector — keep a reference somewhere reachable or it'll be
/// reclaimed at the next cycle.
pub fn alloc(heap: *Heap, size: usize, alignment: usize) ![*]u8 {
    const ptr = pluto_alloc(heap, size, alignment) orelse return error.OutOfMemory;
    pluto_post_alloc(heap, ptr, size);
    return @ptrCast(@alignCast(ptr));
}

/// Force a GC cycle. Spike-only — production code never calls this.
pub fn forceGc() void {
    pluto_force_gc();
}

pub const Stats = struct {
    /// Lifetime bytes allocated through the GC, per bdwgc.
    bytes_allocated: usize,
    /// Allocation calls counted in the Rust crate (sanity check FFI).
    objects_allocated: usize,
    /// Currently reserved heap bytes; reflects post-collection state.
    heap_size: usize,
    /// Number of full GC cycles since init.
    gc_cycles: usize,
};

pub fn stats() Stats {
    return .{
        .bytes_allocated = pluto_stat_bytes_allocated(),
        .objects_allocated = pluto_stat_objects_allocated(),
        .heap_size = pluto_stat_heap_size(),
        .gc_cycles = pluto_stat_gc_count(),
    };
}
