//! pluto-zig phase-0.5 spike.
//!
//! Same FFI plumbing as phase 0, but the allocator is now backed by
//! bdwgc (Boehm-Demers-Weiser conservative GC). Demonstrates that:
//!
//!   1. Allocations flow through a real GC, not malloc
//!   2. Forcing a collection cycle actually reclaims unreachable memory
//!   3. The heap-size stat shrinks when objects are dropped
//!
//! Phase 0.7 will replace bdwgc with mmtk-core. The API surface stays
//! identical, so this file won't change.

const std = @import("std");
const Io = std.Io;
const alloc = @import("alloc.zig");

const ObjectTag = enum(u8) {
    nil = 0,
    boolean = 1,
    int = 2,
    str_short = 3,
    blob = 4,
};

const ObjectHeader = extern struct {
    tag: ObjectTag,
    _pad: [7]u8 = .{0} ** 7,
    payload: u64 = 0,
};

pub fn main(init: std.process.Init) !void {
    const io = init.io;
    var stdout_buffer: [1024]u8 = undefined;
    var stdout_file_writer: Io.File.Writer = .init(.stdout(), io, &stdout_buffer);
    const out = &stdout_file_writer.interface;

    try out.print("[pluto-zig] phase-0.5 spike — bdwgc-backed\n", .{});

    const heap = try alloc.init(64 * 1024 * 1024);
    defer alloc.shutdown(heap);
    try out.print("[pluto-zig] heap initialized\n", .{});

    // Stage 1: a few small typed objects (the phase-0 demo).
    try printStats(out, heap, "before any allocations");

    const sz = @sizeOf(ObjectHeader);
    const align_to: usize = @alignOf(ObjectHeader);

    const a = try alloc.alloc(heap, sz, align_to);
    const ah: *ObjectHeader = @ptrCast(@alignCast(a));
    ah.* = .{ .tag = .int, .payload = 42 };

    const b = try alloc.alloc(heap, sz, align_to);
    const bh: *ObjectHeader = @ptrCast(@alignCast(b));
    bh.* = .{ .tag = .boolean, .payload = 1 };

    try out.print("[pluto-zig] obj a: tag={s} payload={}\n", .{ @tagName(ah.tag), ah.payload });
    try out.print("[pluto-zig] obj b: tag={s} payload={}\n", .{ @tagName(bh.tag), bh.payload });

    try printStats(out, heap, "after 2 small objects");

    // Stage 2: allocate a wave of larger objects in a scope, then
    // drop the references. After GC, those bytes should be reclaimed.
    try out.print("\n[pluto-zig] allocating 10000 x 256-byte blobs...\n", .{});
    {
        var i: usize = 0;
        while (i < 10_000) : (i += 1) {
            const blob = try alloc.alloc(heap, 256, 8);
            // Touch the memory so it's actually committed.
            blob[0] = @truncate(i);
        }
        // No reference kept — the loop's local `blob` doesn't escape.
    }
    try printStats(out, heap, "after wave of blobs (refs dropped)");

    // Stage 3: force a GC cycle and re-read stats. bdwgc finds nothing
    // pointing at the blobs and reclaims them.
    try out.print("\n[pluto-zig] forcing GC cycle...\n", .{});
    alloc.forceGc();
    try printStats(out, heap, "after forced collection");

    try out.print("\n[pluto-zig] phase-0.5 OK\n", .{});
    try out.flush();
}

fn printStats(out: anytype, heap: *alloc.Heap, label: []const u8) !void {
    _ = heap;
    const s = alloc.stats();
    try out.print(
        "  [stats] {s}: gc_cycles={} heap={} KiB bytes_allocated={} KiB objects={}\n",
        .{
            label,
            s.gc_cycles,
            s.heap_size / 1024,
            s.bytes_allocated / 1024,
            s.objects_allocated,
        },
    );
}
