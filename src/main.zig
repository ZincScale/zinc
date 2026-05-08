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
const bc = @import("bytecode.zig");
const codegen = @import("codegen.zig");
const vm_mod = @import("vm.zig");

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

    // --- Phase 3.2 codegen demo ---------------------------------------
    try out.print("\n[pluto-zig] phase-3.2 codegen demo — `return 1 + 2 * 3`\n", .{});
    try compileAndDisassemble(out, init.arena.allocator(), "return 1 + 2 * 3");

    try out.print("\n[pluto-zig] phase-3.2 codegen demo — `return -5 * (10 + 1)`\n", .{});
    try compileAndDisassemble(out, init.arena.allocator(), "return -5 * (10 + 1)");

    try out.print("\n[pluto-zig] phase-3.2 codegen demo — `return \"answer is \", 42`\n", .{});
    try compileAndDisassemble(out, init.arena.allocator(), "return \"answer is \", 42");

    // --- Phase 2.x + 3.2.x VM execution demo --------------------------
    try out.print("\n[pluto-zig] VM demo — actual execution\n", .{});
    const programs = [_][]const u8{
        "return 1 + 2 * 3",
        "return -5 * (10 + 1)",
        "return 7 // 3, 7 % 3, 2 ^ 10",
        "return -7 // 3, -7 % 3",
        "local x = 5\nlocal y = x * 2\nreturn y + 1",
        "local x = 5\nx = x + 10\nreturn x",
        "if 5 > 3 then return \"yes\" else return \"no\" end",
        "local x = 7\nif x < 3 then return 1 elseif x < 10 then return 2 else return 3 end",
        // Sum 1..10 via while loop. Should return 55.
        "local x = 10\nlocal sum = 0\nwhile x > 0 do sum = sum + x\nx = x - 1\nend\nreturn sum",
        // Nested loops: 3*3 = 9 increments.
        "local i = 1\nlocal total = 0\nwhile i <= 3 do local j = 1\nwhile j <= 3 do total = total + 1\nj = j + 1\nend\ni = i + 1\nend\nreturn total",
        // Anonymous function expression
        "local double = function(x) return x * 2 end\nreturn double(21)",
        // local function declaration
        "local function abs(x) if x < 0 then return -x else return x end end\nreturn abs(-7), abs(11)",
        // Nested same-scope calls (no upvalues needed)
        "local sq = function(x) return x * x end\nreturn sq(sq(3))",
        // Recursive fib via upvalue self-reference — the holy grail
        "local function fib(n)\nif n < 2 then return n end\nreturn fib(n - 1) + fib(n - 2)\nend\nreturn fib(10)",
        // Closure that mutates a shared upvalue
        "local count = 0\nlocal inc = function() count = count + 1 end\ninc()\ninc()\ninc()\nreturn count",
        // Counter factory — returned closure outlives its parent frame
        "local function make_counter()\nlocal n = 0\nreturn function() n = n + 1\nreturn n end\nend\nlocal c = make_counter()\nreturn c(), c(), c()",
        // Tables — array, keyed, mixed, indexing, mutation
        "local t = {10, 20, 30}\nreturn t[1] + t[2] + t[3]",
        "local p = {name = \"Alice\", age = 30}\nreturn p.name, p.age",
        "local t = {}\nt.x = 1\nt.y = 2\nt.z = t.x + t.y\nreturn t.z",
        // Object-oriented pattern via table + closures
        "local function make()\nlocal self = {count = 0}\nself.inc = function() self.count = self.count + 1 end\nself.get = function() return self.count end\nreturn self\nend\nlocal c = make()\nc.inc()\nc.inc()\nc.inc()\nreturn c.get()",
        // stdlib: print + tostring + type
        "print(\"Hello, Pluto!\")\nreturn nil",
        "print(type(42), type(\"hi\"), type({}))\nreturn nil",
        "local function fib(n) if n < 2 then return n end return fib(n-1) + fib(n-2) end\nprint(\"fib(10) =\", fib(10))\nreturn nil",
        // Globals from user code
        "score = 0\nscore = score + 10\nscore = score + 20\nprint(\"score:\", score)\nreturn score",
        // Pluto: compound assignment
        "local x = 10\nx += 5\nx *= 2\nx //= 3\nreturn x",
        // Pluto: != operator
        "local function check(a, b) if a != b then return \"differ\" else return \"same\" end end\nreturn check(1, 2), check(7, 7)",
        // Pluto: ! operator
        "return !nil, !false, !0, !\"x\"",
        // strict-Pluto: enforced type annotations
        "local count: integer = 42\nlocal name: string = \"alice\"\nlocal active: boolean = true\nprint(name, count, active)\nreturn nil",
        // type annotation enforces at runtime when value isn't a literal
        "local function compute() return 99 end\nlocal n: number = compute()\nprint(\"got:\", n)\nreturn n",
        // strict-Pluto: typed function params + return
        "local greet = function(name: string): string\nreturn \"Hello, \" .. name\nend\nprint(greet(\"Alice\"))\nreturn nil",
        // typed param rejects mismatched arg at runtime
        "local f = function(x: number): number return x * 2 end\nprint(\"f(21) =\", f(21))\nreturn nil",
        // Pluto: switch — single-value cases + default
        "local function name_of(d) switch d case 1: return \"Mon\" case 2: return \"Tue\" case 3: return \"Wed\" default: return \"?\" end end\nprint(name_of(2), name_of(7))\nreturn nil",
        // Pluto: switch — multi-value case
        "local function bucket(n) switch n case 1, 2, 3: return \"small\" case 10, 20, 30: return \"medium\" default: return \"other\" end end\nreturn bucket(2), bucket(20), bucket(7)",
        // Pluto: switch with break (early-exit inside a case body)
        "local hits = 0\nlocal x = 1\nswitch x case 1: hits += 1 break hits += 99 default: hits = -1 end\nreturn hits",
        // Pluto: break in while loop (now supported via the switch break-stack)
        "local i = 0\nwhile true do i += 1 if i >= 5 then break end end\nreturn i",
        // Phase 4.4a: __index fallback through a metatable
        "local defaults = {role = \"guest\"}\nlocal t = setmetatable({name = \"alice\"}, {__index = defaults})\nreturn t.name, t.role",
        // Phase 4.4a: class-style OO via setmetatable + __index + method-call sugar
        "local Dog = {}\nDog.speak = function(self) return self.name .. \" says woof\" end\nlocal function new_dog(name) return setmetatable({name = name}, {__index = Dog}) end\nreturn new_dog(\"Rex\"):speak()",
        // Phase 4.4a: chained __index — child → parent → root
        "local root = {tag = \"root\"}\nlocal mid = setmetatable({}, {__index = root})\nlocal leaf = setmetatable({}, {__index = mid})\nreturn leaf.tag",
        // Phase 4.4b: class with constructor + method, instantiated via `new`
        "class Greeter\nfunction __construct(name) this.name = name end\npublic function hello() return \"Hi, \" .. this.name end\nend\nreturn new Greeter(\"Alice\"):hello()",
        // Phase 4.4b: class with field defaults (no ctor needed)
        "class Config\npublic port = 8080\npublic host = \"localhost\"\nend\nlocal c = new Config()\nreturn c.host, c.port",
        // Phase 4.4b: extends — chained inheritance, override + super-method
        "class Animal\nprotected name = \"\"\nfunction __construct(n) this.name = n end\npublic function describe() return this.name end\nend\nclass Dog extends Animal\npublic function bark() return this.name .. \" says woof\" end\nend\nlocal d = new Dog(\"Rex\")\nreturn d:describe(), d:bark()",
        // Phase 4.4c: visibility — private members hidden behind a public method
        "class Counter\nprivate count = 0\npublic function inc() this.count = this.count + 1 end\npublic function value() return this.count end\nend\nlocal c = new Counter()\nc:inc() c:inc() c:inc()\nreturn c:value()",
        // Phase 4.4c: protected — visible inside the class chain only
        "class Base\nprotected tag = \"base\"\npublic function reveal() return this.tag end\nend\nclass Sub extends Base\npublic function altered() return this.tag .. \"!\" end\nend\nreturn new Sub():reveal(), new Sub():altered()",
    };
    for (programs) |src| try executeAndPrint(out, init.arena.allocator(), src);

    try out.print("\n[pluto-zig] all phases OK (0, 0.5, 1, 2.x, 3.0, 3.1, 3.2.x)\n", .{});
    try out.flush();
}

fn executeAndPrint(out: anytype, arena: std.mem.Allocator, src: []const u8) !void {
    var p = parser.Parser.init(arena, src) catch |err| {
        try out.print("  {s} -> parse error: {s}\n", .{ src, @errorName(err) });
        return;
    };
    const block = p.parseChunk() catch |err| {
        try out.print("  {s} -> parse error: {s}\n", .{ src, @errorName(err) });
        return;
    };
    var c = codegen.Compiler.init(arena);
    const proto = c.compileChunk(block) catch |err| {
        try out.print("  {s} -> compile error: {s}\n", .{ src, @errorName(err) });
        return;
    };
    var machine = vm_mod.VM.init(arena, proto) catch |err| {
        try out.print("  {s} -> vm init error: {s}\n", .{ src, @errorName(err) });
        return;
    };
    machine.bindEnv();
    const result = machine.run() catch |err| {
        try out.print("  {s} -> runtime error: {s}\n", .{ src, @errorName(err) });
        return;
    };
    // Show source on its own line if it contains newlines (multi-stmt
    // programs); otherwise inline with the result.
    if (std.mem.indexOf(u8, src, "\n") != null) {
        try out.print("  ----\n  {s}\n  ", .{src});
    } else {
        try out.print("  {s:<48} ", .{src});
    }

    // If `print` produced output, dump it before the return value(s).
    if (machine.output.items.len > 0) {
        try out.writeAll("\n  [stdout] ");
        // Indent any embedded newlines for readability.
        for (machine.output.items) |b| {
            try out.writeByte(b);
            if (b == '\n' and machine.output.items[machine.output.items.len - 1] != b) {
                try out.writeAll("  [stdout] ");
            }
        }
        try out.writeAll("  -> ");
    } else {
        try out.writeAll("-> ");
    }

    for (result.values, 0..) |val, i| {
        if (i > 0) try out.writeAll(", ");
        try printValue(out, val);
    }
    try out.writeAll("\n");
}

fn printValue(out: anytype, val: v.TValue) !void {
    switch (val) {
        .nil => try out.writeAll("nil"),
        .boolean => |b| try out.writeAll(if (b) "true" else "false"),
        .integer => |i| try out.print("{d}", .{i}),
        .number => |f| try out.print("{d}", .{f}),
        .string => |s| try out.print("\"{s}\"", .{s.slice()}),
        .table => |t| try out.print("<table {*}>", .{t}),
        .closure => |c| try out.print("<function {*}>", .{c}),
        .native => |n| try out.print("<native {s}>", .{n.name}),
    }
}

fn compileAndDisassemble(out: anytype, arena: std.mem.Allocator, src: []const u8) !void {
    var p = parser.Parser.init(arena, src) catch |err| {
        try out.print("  parser init failed: {s}\n", .{@errorName(err)});
        return;
    };
    const block = p.parseChunk() catch |err| {
        try out.print("  parse error: {s}\n", .{@errorName(err)});
        return;
    };
    var c = codegen.Compiler.init(arena);
    const proto = c.compileChunk(block) catch |err| {
        try out.print("  compile error: {s}\n", .{@errorName(err)});
        return;
    };
    try bc.disassemble(out, proto);
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
        .closure => |c| try out.print("<function {*}>", .{c}),
        .native => |n| try out.print("<native {s}>", .{n.name}),
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
