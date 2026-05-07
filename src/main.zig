//! pluto-zig phase-0 spike.
//!
//! Proves the FFI plumbing: Zig program calls into the Rust allocator
//! crate, gets a pointer, writes a tagged object header, reads it
//! back, and shuts the heap down. Everything is malloc-backed for
//! now — the point isn't GC, it's that the binding shape is real and
//! the next step (swap stub for mmtk) is mechanical.

const std = @import("std");
const Io = std.Io;
const alloc = @import("alloc.zig");

// A trivial stand-in for the eventual TValue. Phase 0.5 will move
// this into src/object.zig with NaN-boxing and a real type tag table.
const ObjectTag = enum(u8) {
    nil = 0,
    boolean = 1,
    int = 2,
    str_short = 3,
};

const ObjectHeader = extern struct {
    tag: ObjectTag,
    _pad: [7]u8 = .{0} ** 7, // align payload to 8 bytes
    payload: u64 = 0,
};

pub fn main(init: std.process.Init) !void {
    const io = init.io;

    var stdout_buffer: [1024]u8 = undefined;
    var stdout_file_writer: Io.File.Writer = .init(.stdout(), io, &stdout_buffer);
    const out = &stdout_file_writer.interface;

    try out.print("[pluto-zig] phase-0 spike\n", .{});

    // 1. Init the heap with 64 MiB capacity (informational in phase 0).
    const heap = try alloc.init(64 * 1024 * 1024);
    defer alloc.shutdown(heap);
    try out.print("[pluto-zig] heap initialized\n", .{});

    // 2. Allocate three objects of different "types" and write payloads.
    const sz = @sizeOf(ObjectHeader);
    const align_to: usize = @alignOf(ObjectHeader);

    const a = try alloc.alloc(heap, sz, align_to);
    const ah: *ObjectHeader = @ptrCast(@alignCast(a));
    ah.* = .{ .tag = .int, .payload = 42 };

    const b = try alloc.alloc(heap, sz, align_to);
    const bh: *ObjectHeader = @ptrCast(@alignCast(b));
    bh.* = .{ .tag = .boolean, .payload = 1 };

    const c = try alloc.alloc(heap, sz, align_to);
    const ch: *ObjectHeader = @ptrCast(@alignCast(c));
    ch.* = .{ .tag = .str_short, .payload = 0xDEADBEEF };

    // 3. Read them back to prove the round-trip.
    try out.print("[pluto-zig] obj a: tag={s} payload={}\n", .{ @tagName(ah.tag), ah.payload });
    try out.print("[pluto-zig] obj b: tag={s} payload={}\n", .{ @tagName(bh.tag), bh.payload });
    try out.print("[pluto-zig] obj c: tag={s} payload=0x{x}\n", .{ @tagName(ch.tag), ch.payload });

    // 4. Phase-0-only manual free. Phase 0.5 deletes this — mmtk
    //    reclaims via collection cycles, no caller-side dealloc.
    alloc.deallocStub(a, sz, align_to);
    alloc.deallocStub(b, sz, align_to);
    alloc.deallocStub(c, sz, align_to);

    // 5. Stats prove allocation counters incremented across the FFI.
    const s = alloc.stats();
    try out.print(
        "[pluto-zig] lifetime stats: {} objects, {} bytes\n",
        .{ s.objects_allocated, s.bytes_allocated },
    );
    try out.print("[pluto-zig] phase-0 OK\n", .{});

    try out.flush();
}
