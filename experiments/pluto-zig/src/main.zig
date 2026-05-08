//! pluto-zig phase-1 spike — TValue, String, Table.
//!
//! Building on phase 0.5's bdwgc-backed allocator, phase 1 adds the
//! Lua data model: a tagged-union TValue, heap-allocated immutable
//! Strings, and an open-addressed hash Table that resizes on demand.
//! All three are GC-managed; the demo allocates a table holding
//! mixed-type keys and values, looks them back up, then forces a
//! collection cycle to verify the live table survives.
//!
//! NaN-boxing was the original phase-1 plan but combines poorly with
//! conservative GC (see comments in value.zig). Tagged unions ship
//! today; NaN-boxing returns when phase 0.7 brings precise GC.

const std = @import("std");
const Io = std.Io;

const alloc = @import("alloc.zig");
const v = @import("value.zig");
const String = @import("string.zig").String;
const Table = @import("table.zig").Table;
const lexer = @import("lexer.zig");
const parser = @import("parser.zig");
const ast = @import("ast.zig");

const TValue = v.TValue;

pub fn main(init: std.process.Init) !void {
    const io = init.io;
    var stdout_buffer: [2048]u8 = undefined;
    var stdout_file_writer: Io.File.Writer = .init(.stdout(), io, &stdout_buffer);
    const out = &stdout_file_writer.interface;

    try out.print("[pluto-zig] phase-1 spike — TValue + String + Table\n", .{});

    const heap = try alloc.init(64 * 1024 * 1024);
    defer alloc.shutdown(heap);

    try printStats(out, "fresh heap");

    // --- Strings ---------------------------------------------------------
    const k_name = try String.create(heap, "name");
    const k_age = try String.create(heap, "age");
    const k_role = try String.create(heap, "role");
    const v_alice = try String.create(heap, "Alice");
    const v_engineer = try String.create(heap, "Engineer");

    try out.print("\n[pluto-zig] strings:\n", .{});
    try out.print("  k_name='{s}' (hash=0x{x})\n", .{ k_name.slice(), k_name.hash });
    try out.print("  v_alice='{s}' (hash=0x{x})\n", .{ v_alice.slice(), v_alice.hash });

    // --- Table with mixed keys ------------------------------------------
    const t = try Table.create(heap);

    try t.set(heap, TValue.fromString(k_name), TValue.fromString(v_alice));
    try t.set(heap, TValue.fromString(k_age), TValue.fromInt(34));
    try t.set(heap, TValue.fromString(k_role), TValue.fromString(v_engineer));
    try t.set(heap, TValue.fromInt(1), TValue.fromString(v_alice)); // mixed key types
    try t.set(heap, TValue.TRUE, TValue.fromFloat(3.14));

    try out.print("\n[pluto-zig] table after 5 inserts (count={}, cap={}):\n", .{ t.len(), t.cap });
    try dumpTable(out, t);

    // --- Lookups exercising every value type ----------------------------
    try out.print("\n[pluto-zig] lookups:\n", .{});
    const got_name = t.get(TValue.fromString(k_name));
    try out.print("  t['name'] -> '{s}'\n", .{got_name.string.slice()});
    const got_age = t.get(TValue.fromString(k_age));
    try out.print("  t['age']  -> {}\n", .{got_age.integer});
    const got_int_key = t.get(TValue.fromInt(1));
    try out.print("  t[1]      -> '{s}'\n", .{got_int_key.string.slice()});
    const got_bool_key = t.get(TValue.TRUE);
    try out.print("  t[true]   -> {d}\n", .{got_bool_key.number});
    const got_missing = t.get(TValue.fromString(k_role));
    try out.print("  t['role'] -> '{s}'\n", .{got_missing.string.slice()});

    try printStats(out, "after table built");

    // --- Force GC, prove table + strings survive ------------------------
    // The Zig stack still holds `t`, `k_name`, etc. — bdwgc finds
    // these via stack scanning, so nothing should be reclaimed.
    try out.print("\n[pluto-zig] forcing GC cycle...\n", .{});
    alloc.forceGc();
    try printStats(out, "after forced GC");

    // Re-read all five values post-GC. If anything was wrongly
    // reclaimed, this would crash or return garbage.
    try out.print("\n[pluto-zig] re-reading post-GC:\n", .{});
    try out.print("  t['name'] -> '{s}'\n", .{t.get(TValue.fromString(k_name)).string.slice()});
    try out.print("  t['age']  -> {}\n", .{t.get(TValue.fromString(k_age)).integer});
    try out.print("  t['role'] -> '{s}'\n", .{t.get(TValue.fromString(k_role)).string.slice()});

    // --- Stress: 1000 string-keyed inserts to exercise resize -----------
    try out.print("\n[pluto-zig] stress: 1000 string-keyed inserts...\n", .{});
    const t2 = try Table.create(heap);
    var i: i64 = 0;
    var key_buf: [32]u8 = undefined;
    while (i < 1000) : (i += 1) {
        const key_str = try std.fmt.bufPrint(&key_buf, "key_{d}", .{i});
        const ks = try String.create(heap, key_str);
        try t2.set(heap, TValue.fromString(ks), TValue.fromInt(i * 7));
    }
    try out.print("  t2 size: count={} cap={}\n", .{ t2.len(), t2.cap });

    // Spot-check a handful of entries.
    const probe_str = try String.create(heap, "key_500");
    const probe_val = t2.get(TValue.fromString(probe_str));
    try out.print("  t2['key_500'] -> {} (expected 3500)\n", .{probe_val.integer});

    try printStats(out, "final");

    // --- Phase 3.0 lexer demo ------------------------------------------
    try out.print("\n[pluto-zig] phase-3.0 lexer demo\n", .{});
    const sample =
        \\local function fib(n)
        \\    if n < 2 then return n end
        \\    return fib(n - 1) + fib(n - 2)
        \\end
        \\
        \\class Greeter
        \\    function __new(name)
        \\        self.name = name
        \\    end
        \\    function greet()
        \\        return "Hello, " .. self.name
        \\    end
        \\end
    ;
    try lexAndDump(out, sample);

    // --- Phase 3.1 parser demo ----------------------------------------
    try out.print("\n[pluto-zig] phase-3.1 parser demo\n", .{});
    const fib_src =
        \\local function fib(n)
        \\    if n < 2 then return n end
        \\    return fib(n - 1) + fib(n - 2)
        \\end
        \\
        \\local result = fib(10)
        \\print("fib(10) = " .. result)
    ;
    try parseAndDump(out, init.arena.allocator(), fib_src);

    try out.print("\n[pluto-zig] phase-1 + phase-3.0 + phase-3.1 OK\n", .{});
    try out.flush();
}

fn parseAndDump(out: anytype, arena: std.mem.Allocator, src: []const u8) !void {
    var p = parser.Parser.init(arena, src) catch |err| {
        try out.print("  parser init failed: {s}\n", .{@errorName(err)});
        return;
    };
    const block = p.parseChunk() catch |err| {
        try out.print("  parse error: {s}\n", .{@errorName(err)});
        return;
    };
    try ast.dumpBlock(out, block, 1);
}

fn lexAndDump(out: anytype, src: []const u8) !void {
    var lex = lexer.Lexer.init(src);
    var n: u32 = 0;
    while (true) {
        const tok = lex.next() catch |err| {
            try out.print("  lex error: {s}\n", .{@errorName(err)});
            return;
        };
        if (tok.kind == .eof) break;
        const text = tok.lexeme(src);
        const kw = lexer.keywordFor(text);
        const tag = if (kw != null) "keyword" else @tagName(tok.kind);
        try out.print("  L{d:>2}  {s:<14}  '{s}'\n", .{ tok.line, tag, text });
        n += 1;
    }
    try out.print("  ({d} tokens)\n", .{n});
}

fn dumpTable(out: anytype, t: *Table) !void {
    const Ctx = struct { out: @TypeOf(out) };
    const ctx = Ctx{ .out = out };
    try t.forEach(ctx, struct {
        fn entry(c: Ctx, key: TValue, value: TValue) anyerror!void {
            try printSlot(c.out, key);
            try c.out.print(" = ", .{});
            try printSlot(c.out, value);
            try c.out.print("\n", .{});
        }
    }.entry);
}

fn printSlot(out: anytype, val: TValue) !void {
    try out.print("  ", .{});
    switch (val) {
        .nil => try out.print("nil", .{}),
        .boolean => |b| try out.print("{}", .{b}),
        .integer => |i| try out.print("{}", .{i}),
        .number => |f| try out.print("{d}", .{f}),
        .string => |s| try out.print("'{s}'", .{s.slice()}),
        .table => |tt| try out.print("<table {*}>", .{tt}),
    }
}

fn printStats(out: anytype, label: []const u8) !void {
    const s = alloc.stats();
    try out.print(
        "  [stats] {s}: gc_cycles={} heap={} KiB lifetime={} KiB objects={}\n",
        .{
            label,
            s.gc_cycles,
            s.heap_size / 1024,
            s.bytes_allocated / 1024,
            s.objects_allocated,
        },
    );
}
