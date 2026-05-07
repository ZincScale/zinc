//! Zig wrapper over the Rust pluto_alloc C ABI.
//!
//! The functions here are 1:1 with the extern "C" symbols in
//! alloc-rs/src/lib.rs. Keeping the wrapper thin so the swap from
//! the phase-0 stub allocator to a real mmtk-backed allocator
//! doesn't ripple up into the VM.

const std = @import("std");

pub const Heap = opaque {};

extern fn pluto_heap_init(heap_bytes: usize) ?*Heap;
extern fn pluto_heap_shutdown(heap: ?*Heap) void;
extern fn pluto_alloc(heap: ?*Heap, size: usize, alignment: usize) ?*anyopaque;
extern fn pluto_post_alloc(heap: ?*Heap, obj: ?*anyopaque, size: usize) void;
extern fn pluto_dealloc_stub(obj: ?*anyopaque, size: usize, alignment: usize) void;
extern fn pluto_stat_bytes_allocated() usize;
extern fn pluto_stat_objects_allocated() usize;

pub fn init(heap_bytes: usize) !*Heap {
    return pluto_heap_init(heap_bytes) orelse error.HeapInitFailed;
}

pub fn shutdown(heap: *Heap) void {
    pluto_heap_shutdown(heap);
}

/// Allocate `size` bytes aligned to `alignment`. Returns the raw
/// pointer; caller must initialize before any operation that could
/// trigger a GC (irrelevant in phase 0; load-bearing in phase 0.5).
pub fn alloc(heap: *Heap, size: usize, alignment: usize) ![*]u8 {
    const ptr = pluto_alloc(heap, size, alignment) orelse return error.OutOfMemory;
    pluto_post_alloc(heap, ptr, size);
    return @ptrCast(@alignCast(ptr));
}

/// Phase-0-only: the stub allocator manages memory via malloc, so we
/// expose a manual free. This goes away once mmtk owns reclamation.
pub fn deallocStub(obj: [*]u8, size: usize, alignment: usize) void {
    pluto_dealloc_stub(@ptrCast(obj), size, alignment);
}

pub const Stats = struct {
    bytes_allocated: usize,
    objects_allocated: usize,
};

pub fn stats() Stats {
    return .{
        .bytes_allocated = pluto_stat_bytes_allocated(),
        .objects_allocated = pluto_stat_objects_allocated(),
    };
}
