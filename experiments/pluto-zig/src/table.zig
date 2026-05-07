//! Table — Lua's universal data structure.
//!
//! Phase 1 ships hash-only: all keys (integer, string, etc.) live in
//! one open-addressed table with linear probing. Lua's classic
//! "array part + hash part" hybrid is a perf optimization; a dense
//! integer-keyed table currently pays a hash hit per access. Phase 2
//! adds the array fast path once the VM has bytecode dispatching
//! through tables on hot paths.
//!
//! Memory model:
//! - Table struct: GC-allocated header
//! - entries[]:    GC-allocated array of Entry, lives separately so
//!                 it can grow independently. The Entry struct holds
//!                 TValues directly; pointer fields inside TValues
//!                 (string, table) are visible to bdwgc's conservative
//!                 scan because they're stored as raw `*String` /
//!                 `*Table` in the union, not NaN-boxed.

const std = @import("std");
const alloc = @import("alloc.zig");
const v = @import("value.zig");

const TValue = v.TValue;

pub const Table = struct {
    cap: u32,
    count: u32,
    entries: [*]Entry,

    pub const Entry = struct {
        key: TValue,
        value: TValue,
    };

    /// Initial capacity. Always a power of two so we can mask instead
    /// of mod for slot indexing. 8 is small enough that empty tables
    /// are cheap, big enough to avoid an immediate resize on the
    /// first few inserts.
    const INITIAL_CAP: u32 = 8;
    /// Resize threshold — doubled at this load factor (count*4 >= cap*3).
    const LOAD_FACTOR_NUM: u32 = 3;
    const LOAD_FACTOR_DEN: u32 = 4;

    pub fn create(heap: *alloc.Heap) !*Table {
        const t_raw = try alloc.alloc(heap, @sizeOf(Table), @alignOf(Table));
        const t: *Table = @ptrCast(@alignCast(t_raw));

        const e_raw = try allocEntries(heap, INITIAL_CAP);

        t.* = .{
            .cap = INITIAL_CAP,
            .count = 0,
            .entries = e_raw,
        };
        return t;
    }

    fn allocEntries(heap: *alloc.Heap, cap: u32) ![*]Entry {
        const bytes = @sizeOf(Entry) * cap;
        const raw = try alloc.alloc(heap, bytes, @alignOf(Entry));
        const arr: [*]Entry = @ptrCast(@alignCast(raw));
        // bdwgc returns zeroed memory, but be explicit so the
        // empty-slot invariant (key == nil) is locally obvious.
        var i: u32 = 0;
        while (i < cap) : (i += 1) {
            arr[i] = .{ .key = TValue.NIL, .value = TValue.NIL };
        }
        return arr;
    }

    /// Look up `key`; return its value or nil.
    pub fn get(self: *Table, key: TValue) TValue {
        if (key == .nil) return TValue.NIL;
        const slot = self.findSlot(key);
        if (self.entries[slot].key == .nil) return TValue.NIL;
        return self.entries[slot].value;
    }

    /// Errors set/resize can return. Declared explicitly to break
    /// the inferred-error-set cycle between set <-> resize (set calls
    /// resize which re-inserts via set).
    pub const Error = error{ NilKey, OutOfMemory, HeapInitFailed };

    /// Set `key` to `value`. `value == nil` deletes the entry.
    /// Resizes if load factor crosses the threshold.
    pub fn set(self: *Table, heap: *alloc.Heap, key: TValue, value: TValue) Error!void {
        if (key == .nil) return error.NilKey;

        // Resize check: do this BEFORE finding the slot so the slot
        // index stays valid.
        if ((self.count + 1) * LOAD_FACTOR_DEN >= self.cap * LOAD_FACTOR_NUM) {
            try self.resize(heap, self.cap * 2);
        }

        const slot = self.findSlot(key);
        const is_new = self.entries[slot].key == .nil;

        // nil-value semantics: if writing nil to a present key, leave
        // the slot in place (open-addressed linear probing breaks if
        // we just empty the slot; tombstones are a future-phase
        // refinement). For a real delete we'd insert a tombstone or
        // shift the probe chain; skipping that for phase 1.
        if (value == .nil and is_new) return;

        self.entries[slot] = .{ .key = key, .value = value };
        if (is_new) self.count += 1;
    }

    /// Linear-probe to find either an existing entry for `key` or the
    /// first empty slot. Caller checks .key == .nil to distinguish.
    fn findSlot(self: *Table, key: TValue) u32 {
        const mask = self.cap - 1;
        var i: u32 = @intCast(key.hash() & mask);
        while (true) {
            const e = &self.entries[i];
            if (e.key == .nil or e.key.eql(key)) return i;
            i = (i + 1) & mask;
        }
    }

    fn resize(self: *Table, heap: *alloc.Heap, new_cap: u32) Error!void {
        const old_entries = self.entries;
        const old_cap = self.cap;

        const new_entries = try allocEntries(heap, new_cap);
        self.entries = new_entries;
        self.cap = new_cap;
        self.count = 0;

        // Re-insert every live entry from the old array. set()
        // recomputes the slot under the new mask.
        var i: u32 = 0;
        while (i < old_cap) : (i += 1) {
            const e = old_entries[i];
            if (e.key != .nil) {
                // Recursive call into set() is safe — load factor is
                // halved post-resize so we won't trigger another
                // resize during this loop.
                try self.set(heap, e.key, e.value);
            }
        }
    }

    /// Number of live entries. Useful for testing + the spike demo.
    pub fn len(self: *const Table) u32 {
        return self.count;
    }

    /// Iterate entries via callback. Gives the demo a clean way to
    /// dump table contents without exposing the slot array.
    pub fn forEach(self: *Table, ctx: anytype, comptime f: fn (@TypeOf(ctx), TValue, TValue) anyerror!void) !void {
        var i: u32 = 0;
        while (i < self.cap) : (i += 1) {
            const e = self.entries[i];
            if (e.key != .nil) try f(ctx, e.key, e.value);
        }
    }
};

test "Table empty get returns nil" {
    const heap = try alloc.init(1 << 20);
    defer alloc.shutdown(heap);

    const t = try Table.create(heap);
    try std.testing.expectEqual(@as(u32, 0), t.len());
    const got = t.get(TValue.fromInt(7));
    try std.testing.expect(got == .nil);
}

test "Table set then get roundtrip" {
    const heap = try alloc.init(1 << 20);
    defer alloc.shutdown(heap);

    const t = try Table.create(heap);
    try t.set(heap, TValue.fromInt(1), TValue.fromInt(100));
    try t.set(heap, TValue.fromInt(2), TValue.fromInt(200));
    try t.set(heap, TValue.fromInt(3), TValue.fromInt(300));

    try std.testing.expectEqual(@as(u32, 3), t.len());
    try std.testing.expectEqual(@as(i64, 100), t.get(TValue.fromInt(1)).integer);
    try std.testing.expectEqual(@as(i64, 200), t.get(TValue.fromInt(2)).integer);
    try std.testing.expectEqual(@as(i64, 300), t.get(TValue.fromInt(3)).integer);
}

test "Table grows past initial capacity" {
    const heap = try alloc.init(1 << 20);
    defer alloc.shutdown(heap);

    const t = try Table.create(heap);
    var i: i64 = 0;
    while (i < 100) : (i += 1) {
        try t.set(heap, TValue.fromInt(i), TValue.fromInt(i * 10));
    }
    try std.testing.expectEqual(@as(u32, 100), t.len());
    // Spot-check a few entries survived the resize chain.
    try std.testing.expectEqual(@as(i64, 0), t.get(TValue.fromInt(0)).integer);
    try std.testing.expectEqual(@as(i64, 420), t.get(TValue.fromInt(42)).integer);
    try std.testing.expectEqual(@as(i64, 990), t.get(TValue.fromInt(99)).integer);
    try std.testing.expect(t.cap >= 128);
}

test "Table accepts mixed key types" {
    const heap = try alloc.init(1 << 20);
    defer alloc.shutdown(heap);

    const String = @import("string.zig").String;
    const t = try Table.create(heap);
    const k_name = try String.create(heap, "name");
    const v_alice = try String.create(heap, "alice");

    try t.set(heap, TValue.fromInt(1), TValue.fromString(v_alice));
    try t.set(heap, TValue.fromString(k_name), TValue.fromString(v_alice));
    try t.set(heap, TValue.TRUE, TValue.fromInt(42));

    try std.testing.expectEqual(@as(u32, 3), t.len());

    const got_name = t.get(TValue.fromString(k_name));
    try std.testing.expect(got_name == .string);
    try std.testing.expectEqualStrings("alice", got_name.string.slice());

    const got_true = t.get(TValue.TRUE);
    try std.testing.expectEqual(@as(i64, 42), got_true.integer);
}
