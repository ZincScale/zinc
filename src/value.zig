//! TValue — the runtime value representation.
//!
//! Phase 1 ships a tagged union (Zig `union(enum)`). The original
//! plan was NaN-boxing, but that combines poorly with bdwgc's
//! conservative scanning: a NaN-boxed pointer has type bits in its
//! high 16, so bdwgc's "does this look like a heap pointer" check
//! fails and the referenced object can be reclaimed prematurely.
//!
//! Tagged union → ~16 bytes per value instead of NaN-boxing's 8, but
//! correct under bdwgc and trivial to read. NaN-boxing returns as a
//! perf optimization once the collector is precise (phase 0.7 swap
//! to mmtk-core's GenImmix).

const std = @import("std");

pub const String = @import("string.zig").String;
pub const Table = @import("table.zig").Table;
const bc = @import("bytecode.zig");

/// A Lua closure — a function value at runtime. Holds a reference to
/// the compiled Proto and an array of upvalue cells captured from the
/// enclosing scope at CLOSURE-time.
pub const Closure = struct {
    proto: *const bc.Proto,
    upvalues: []*UpvalueCell,
};

/// One captured upvalue. Models Lua's open/closed transition:
///
/// - Open:   `value` points at a stack slot in some live frame.
///           Reads and writes go through that slot directly, so
///           multiple closures (and the parent function) all see the
///           same updates.
/// - Closed: `value` points at this cell's own `storage`. Happens
///           when the parent frame pops — the value gets copied into
///           the cell so it survives.
pub const UpvalueCell = struct {
    /// Pointer to the live TValue. Either points into a register
    /// pool slot (open) or at this cell's own `storage` (closed).
    value: *TValue,
    /// Backing TValue once the cell is closed. Untouched while open.
    storage: TValue = .{ .nil = {} },
};

pub const Tag = enum(u8) {
    nil,
    boolean,
    integer,
    number, // f64 — Lua's "number" subtype
    string,
    table,
    closure,
};

pub const TValue = union(Tag) {
    nil: void,
    boolean: bool,
    integer: i64,
    number: f64,
    string: *String,
    table: *Table,
    closure: *Closure,

    pub const NIL: TValue = .{ .nil = {} };
    pub const TRUE: TValue = .{ .boolean = true };
    pub const FALSE: TValue = .{ .boolean = false };

    pub fn fromInt(i: i64) TValue {
        return .{ .integer = i };
    }

    pub fn fromFloat(f: f64) TValue {
        return .{ .number = f };
    }

    pub fn fromBool(b: bool) TValue {
        return .{ .boolean = b };
    }

    pub fn fromString(s: *String) TValue {
        return .{ .string = s };
    }

    pub fn fromTable(t: *Table) TValue {
        return .{ .table = t };
    }

    pub fn fromClosure(c: *Closure) TValue {
        return .{ .closure = c };
    }

    /// Lua truthiness: only `nil` and `false` are falsy.
    pub fn isTruthy(self: TValue) bool {
        return switch (self) {
            .nil => false,
            .boolean => |b| b,
            else => true,
        };
    }

    /// Equality matching Lua's `==`. Cross-type comparisons return
    /// false; numeric coercion (int <-> float) is symmetric.
    pub fn eql(a: TValue, b: TValue) bool {
        return switch (a) {
            .nil => b == .nil,
            .boolean => |av| switch (b) {
                .boolean => |bv| av == bv,
                else => false,
            },
            .integer => |av| switch (b) {
                .integer => |bv| av == bv,
                .number => |bv| @as(f64, @floatFromInt(av)) == bv,
                else => false,
            },
            .number => |av| switch (b) {
                .number => |bv| av == bv,
                .integer => |bv| av == @as(f64, @floatFromInt(bv)),
                else => false,
            },
            .string => |av| switch (b) {
                .string => |bv| av == bv or av.eql(bv),
                else => false,
            },
            .table => |av| switch (b) {
                .table => |bv| av == bv, // identity, like Lua
                else => false,
            },
            .closure => |av| switch (b) {
                .closure => |bv| av == bv, // identity
                else => false,
            },
        };
    }

    /// Hash for use as a Table key. Strings hash by content; tables
    /// hash by identity. Numbers hash by their bit pattern.
    pub fn hash(self: TValue) u64 {
        return switch (self) {
            .nil => 0,
            .boolean => |b| if (b) 1 else 2,
            .integer => |i| std.hash.Wyhash.hash(0, std.mem.asBytes(&i)),
            .number => |f| std.hash.Wyhash.hash(0, std.mem.asBytes(&f)),
            .string => |s| s.hash,
            .table => |t| @intFromPtr(t),
            .closure => |c| @intFromPtr(c),
        };
    }

    pub fn typeName(self: TValue) []const u8 {
        return @tagName(@as(Tag, self));
    }
};

test "TValue tags and equality" {
    const a = TValue.fromInt(42);
    const b = TValue.fromInt(42);
    const c = TValue.fromInt(43);
    const d = TValue.fromFloat(42.0);
    try std.testing.expect(a.eql(b));
    try std.testing.expect(!a.eql(c));
    // Numeric coercion: int 42 == float 42.0
    try std.testing.expect(a.eql(d));
}

test "TValue truthiness" {
    try std.testing.expect(!TValue.NIL.isTruthy());
    try std.testing.expect(!TValue.FALSE.isTruthy());
    try std.testing.expect(TValue.TRUE.isTruthy());
    try std.testing.expect(TValue.fromInt(0).isTruthy()); // 0 is truthy in Lua
    try std.testing.expect(TValue.fromInt(1).isTruthy());
}

test "TValue type name" {
    try std.testing.expectEqualStrings("nil", TValue.NIL.typeName());
    try std.testing.expectEqualStrings("integer", TValue.fromInt(1).typeName());
    try std.testing.expectEqualStrings("number", TValue.fromFloat(1.0).typeName());
}
