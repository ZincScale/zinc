//! String — heap-allocated, length-prefixed, hash-precomputed.
//!
//! Strings are immutable. The header sits at the start of a single
//! GC-allocated block followed inline by the byte content; one alloc
//! per string. Interning (one canonical String per byte sequence) is
//! deferred to phase 1.x — phase 1.0 ships per-allocation strings.
//!
//! Hash is computed once at creation time and stored in the header,
//! so equality / table lookups don't re-hash on every use.

const std = @import("std");
const alloc = @import("alloc.zig");

pub const String = extern struct {
    /// Wyhash of the byte content.
    hash: u64,
    /// Byte length, not counting any trailing nul (we don't write one).
    len: u32,
    _pad: u32 = 0,
    // Followed inline by `len` bytes of content.

    /// Allocate a new String holding a copy of `bytes` from the GC
    /// heap. Caller keeps a live reference; bdwgc reclaims when
    /// nothing reachable points at it.
    pub fn create(heap: *alloc.Heap, bytes: []const u8) !*String {
        const total_size = @sizeOf(String) + bytes.len;
        const raw = try alloc.alloc(heap, total_size, @alignOf(String));
        return initInPlace(@ptrCast(@alignCast(raw)), bytes);
    }

    /// Allocate via a generic Zig allocator instead of the GC heap.
    /// Used by the VM for runtime string constants — these live for
    /// the program duration, no need to involve the collector.
    /// Phase 0.7 (precise GC) revisits whether to unify this path.
    pub fn createWithAllocator(allocator: std.mem.Allocator, bytes: []const u8) !*String {
        const total_size = @sizeOf(String) + bytes.len;
        const raw = try allocator.alignedAlloc(u8, .@"8", total_size);
        return initInPlace(@ptrCast(@alignCast(raw.ptr)), bytes);
    }

    fn initInPlace(s: *String, bytes: []const u8) *String {
        s.* = .{
            .hash = std.hash.Wyhash.hash(0, bytes),
            .len = @intCast(bytes.len),
        };
        const dst = bytesPtr(s);
        @memcpy(dst[0..bytes.len], bytes);
        return s;
    }

    /// Pointer to the inline byte content (after the header).
    fn bytesPtr(self: *String) [*]u8 {
        const base: [*]u8 = @ptrCast(self);
        return base + @sizeOf(String);
    }

    /// Read-only view of the content bytes.
    pub fn slice(self: *const String) []const u8 {
        const base: [*]const u8 = @ptrCast(self);
        return base[@sizeOf(String)..@sizeOf(String) + self.len];
    }

    /// Content equality. Hash mismatch is a fast reject.
    pub fn eql(a: *const String, b: *const String) bool {
        if (a == b) return true;
        if (a.hash != b.hash or a.len != b.len) return false;
        return std.mem.eql(u8, a.slice(), b.slice());
    }
};

test "String roundtrip" {
    const heap = try alloc.init(1 << 20);
    defer alloc.shutdown(heap);

    const s = try String.create(heap, "hello, pluto");
    try std.testing.expectEqual(@as(u32, 12), s.len);
    try std.testing.expectEqualStrings("hello, pluto", s.slice());
}

test "String equality" {
    const heap = try alloc.init(1 << 20);
    defer alloc.shutdown(heap);

    const a = try String.create(heap, "foo");
    const b = try String.create(heap, "foo");
    const c = try String.create(heap, "bar");

    // Different objects, same content.
    try std.testing.expect(a != b);
    try std.testing.expect(a.eql(b));
    try std.testing.expect(!a.eql(c));
    try std.testing.expectEqual(a.hash, b.hash);
}
