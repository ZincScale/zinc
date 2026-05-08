//! Virtual machine — dispatches Lua 5.4 bytecode against a register file.
//!
//! Phase 2.0 implements the 19 opcodes the codegen currently emits:
//! literals (LOADI/LOADF/LOADK/LOADNIL/LOADTRUE/LOADFALSE), arithmetic
//! (ADD/SUB/MUL/DIV/IDIV/MOD/POW), unary (UNM/BNOT/NOT/LEN), return
//! (RETURN0/RETURN1/RETURN), plus VARARGPREP as a no-op.
//!
//! Lua semantics that aren't C-defaults:
//! - Integer ops wrap on overflow (modular i64 arithmetic).
//! - `//` is floor-division: -5 // 2 = -3 (not -2).
//! - `%` follows Lua: result has the divisor's sign. -5 % 3 = 1.
//! - `^` always returns a float (not int).
//! - `/` always returns a float, even for two integers.
//! - Mixed-type arithmetic promotes to float.

const std = @import("std");
const bc = @import("bytecode.zig");
const v = @import("value.zig");
const Instruction = bc.Instruction;
const OpCode = bc.OpCode;
const TValue = v.TValue;

pub const RuntimeError = error{
    DivByZero,
    InvalidArithmeticOperand,
    InvalidUnaryOperand,
    NotImplemented,
    UnknownOpcode,
    OutOfMemory,
};

pub const RunResult = struct {
    values: []TValue,
};

pub const VM = struct {
    allocator: std.mem.Allocator,
    proto: *const bc.Proto,
    pc: usize,
    /// Register file. Sized to proto.max_stack at init time.
    registers: []TValue,

    pub fn init(allocator: std.mem.Allocator, proto: *const bc.Proto) !VM {
        const regs = try allocator.alloc(TValue, @max(proto.max_stack, 1));
        @memset(regs, TValue.NIL);
        return .{
            .allocator = allocator,
            .proto = proto,
            .pc = 0,
            .registers = regs,
        };
    }

    pub fn deinit(self: *VM) void {
        self.allocator.free(self.registers);
    }

    /// Run until a return opcode fires. The returned slice is owned
    /// by the VM caller (allocated by `allocator`); free it when done.
    pub fn run(self: *VM) RuntimeError!RunResult {
        while (true) {
            if (self.pc >= self.proto.code.len) {
                // Falling off the end is an implicit `return0`.
                return .{ .values = try self.allocator.alloc(TValue, 0) };
            }
            const instr = self.proto.code[self.pc];
            self.pc += 1;

            switch (instr.opcode()) {
                .varargprep => {}, // No varargs handled in 2.0; trivial no-op.

                .loadi => self.registers[instr.a] = TValue.fromInt(@intCast(instr.unpackSBx())),
                .loadf => self.registers[instr.a] = TValue.fromFloat(@floatFromInt(instr.unpackSBx())),
                .loadk => {
                    const k = self.proto.constants[instr.unpackBx()];
                    self.registers[instr.a] = constToValue(k);
                },
                .loadnil => self.registers[instr.a] = TValue.NIL,
                .loadtrue => self.registers[instr.a] = TValue.TRUE,
                .loadfalse => self.registers[instr.a] = TValue.FALSE,

                .add => self.registers[instr.a] = try arith(.add, self.registers[instr.b], self.registers[instr.c]),
                .sub => self.registers[instr.a] = try arith(.sub, self.registers[instr.b], self.registers[instr.c]),
                .mul => self.registers[instr.a] = try arith(.mul, self.registers[instr.b], self.registers[instr.c]),
                .div => self.registers[instr.a] = try arith(.div, self.registers[instr.b], self.registers[instr.c]),
                .idiv => self.registers[instr.a] = try arith(.idiv, self.registers[instr.b], self.registers[instr.c]),
                .mod => self.registers[instr.a] = try arith(.mod, self.registers[instr.b], self.registers[instr.c]),
                .pow => self.registers[instr.a] = try arith(.pow, self.registers[instr.b], self.registers[instr.c]),

                .unm => self.registers[instr.a] = try unary(.neg, self.registers[instr.b]),
                .bnot => self.registers[instr.a] = try unary(.bnot, self.registers[instr.b]),
                .not_ => self.registers[instr.a] = unaryNot(self.registers[instr.b]),
                .len => self.registers[instr.a] = try unaryLen(self.registers[instr.b]),

                .return0 => return self.makeResult(0, 0),
                .return1 => return self.makeResult(instr.a, 1),
                .return_ => {
                    // RETURN A B: B-1 values starting at R[A]. B==0
                    // would mean "return all to top of stack" in Lua;
                    // we don't emit that form yet so a clean error
                    // suffices.
                    if (instr.b == 0) return error.NotImplemented;
                    return self.makeResult(instr.a, instr.b - 1);
                },

                else => return error.UnknownOpcode,
            }
        }
    }

    fn makeResult(self: *VM, base: u8, count: usize) !RunResult {
        const out = try self.allocator.alloc(TValue, count);
        var i: usize = 0;
        while (i < count) : (i += 1) out[i] = self.registers[base + i];
        return .{ .values = out };
    }
};

fn constToValue(k: bc.Constant) TValue {
    return switch (k) {
        .nil => TValue.NIL,
        .boolean => |b| TValue.fromBool(b),
        .integer => |n| TValue.fromInt(n),
        .number => |f| TValue.fromFloat(f),
        // String constants come from the parser arena, not the GC
        // heap. Phase 2.0 doesn't have a runtime String interner yet,
        // so we surface them as a special-case .ident-like value via
        // a stub: emit nil and let later phases promote constants to
        // proper GC strings. Once the codegen needs string-typed
        // returns (concat, GETFIELD), this becomes the place to
        // intern through value.zig's String.
        .string => TValue.NIL, // FIXME(phase 2.x): intern into a GC String
    };
}

// =============================================================================
// Arithmetic — Lua 5.4 mixed-int/float semantics
// =============================================================================

const ArithOp = enum { add, sub, mul, div, idiv, mod, pow };

fn arith(op: ArithOp, a: TValue, b: TValue) RuntimeError!TValue {
    // The five legal value pairs are int/int, int/num, num/int,
    // num/num. Anything else (string, bool, nil, etc.) is a runtime
    // type error.
    const ax = numericKind(a) orelse return error.InvalidArithmeticOperand;
    const bx = numericKind(b) orelse return error.InvalidArithmeticOperand;

    // Some ops are always float: `/` and `^` per Lua 5.4 reference.
    const force_float = (op == .div or op == .pow);

    if (ax == .integer and bx == .integer and !force_float) {
        return intArith(op, a.integer, b.integer);
    }

    // Otherwise promote to float.
    const af: f64 = if (ax == .integer) @floatFromInt(a.integer) else a.number;
    const bf: f64 = if (bx == .integer) @floatFromInt(b.integer) else b.number;
    return floatArith(op, af, bf);
}

const NumericKind = enum { integer, number };

fn numericKind(t: TValue) ?NumericKind {
    return switch (t) {
        .integer => .integer,
        .number => .number,
        else => null,
    };
}

fn intArith(op: ArithOp, a: i64, b: i64) RuntimeError!TValue {
    return switch (op) {
        .add => TValue.fromInt(a +% b),
        .sub => TValue.fromInt(a -% b),
        .mul => TValue.fromInt(a *% b),
        .idiv => blk: {
            if (b == 0) return error.DivByZero;
            // Zig's @divFloor matches Lua's floor-division semantics
            // (rounds toward -inf, not toward zero).
            break :blk TValue.fromInt(@divFloor(a, b));
        },
        .mod => blk: {
            if (b == 0) return error.DivByZero;
            // Zig's @mod matches Lua's mod semantics (result has the
            // divisor's sign).
            break :blk TValue.fromInt(@mod(a, b));
        },
        // div and pow are float-forced and routed via floatArith,
        // so reaching them here would be a bug.
        .div, .pow => unreachable,
    };
}

fn floatArith(op: ArithOp, a: f64, b: f64) RuntimeError!TValue {
    return switch (op) {
        .add => TValue.fromFloat(a + b),
        .sub => TValue.fromFloat(a - b),
        .mul => TValue.fromFloat(a * b),
        .div => TValue.fromFloat(a / b),
        .idiv => TValue.fromFloat(@floor(a / b)),
        .mod => blk: {
            // Lua: a - floor(a/b)*b
            const q = @floor(a / b);
            break :blk TValue.fromFloat(a - q * b);
        },
        .pow => TValue.fromFloat(std.math.pow(f64, a, b)),
    };
}

// =============================================================================
// Unary
// =============================================================================

const UnaryArith = enum { neg, bnot };

fn unary(op: UnaryArith, x: TValue) RuntimeError!TValue {
    return switch (op) {
        .neg => switch (x) {
            .integer => |n| TValue.fromInt(-%n),
            .number => |f| TValue.fromFloat(-f),
            else => error.InvalidUnaryOperand,
        },
        .bnot => switch (x) {
            .integer => |n| TValue.fromInt(~n),
            else => error.InvalidUnaryOperand,
        },
    };
}

fn unaryNot(x: TValue) TValue {
    return TValue.fromBool(!x.isTruthy());
}

fn unaryLen(x: TValue) RuntimeError!TValue {
    return switch (x) {
        .string => |s| TValue.fromInt(@intCast(s.len)),
        .table => |t| TValue.fromInt(@intCast(t.len())),
        else => error.InvalidUnaryOperand,
    };
}

// =============================================================================
// Tests
// =============================================================================

const testing = std.testing;
const parser = @import("parser.zig");
const codegen = @import("codegen.zig");

fn runSrc(arena: std.mem.Allocator, src: []const u8) ![]TValue {
    var p = try parser.Parser.init(arena, src);
    const block = try p.parseChunk();
    var c = codegen.Compiler.init(arena);
    const proto = try c.compileChunk(block);
    var vm = try VM.init(arena, proto);
    const result = try vm.run();
    return result.values;
}

test "vm: return literal int" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return 42");
    try testing.expectEqual(@as(usize, 1), r.len);
    try testing.expectEqual(@as(i64, 42), r[0].integer);
}

test "vm: arithmetic precedence — 1 + 2 * 3 = 7" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return 1 + 2 * 3");
    try testing.expectEqual(@as(i64, 7), r[0].integer);
}

test "vm: parens override precedence — (1 + 2) * 3 = 9" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return (1 + 2) * 3");
    try testing.expectEqual(@as(i64, 9), r[0].integer);
}

test "vm: unary negation — -5 * (10 + 1) = -55" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return -5 * (10 + 1)");
    try testing.expectEqual(@as(i64, -55), r[0].integer);
}

test "vm: integer division — 7 // 3 = 2" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return 7 // 3");
    try testing.expectEqual(@as(i64, 2), r[0].integer);
}

test "vm: floor division of negative — -7 // 3 = -3 (not -2)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return -7 // 3");
    try testing.expectEqual(@as(i64, -3), r[0].integer);
}

test "vm: mod follows divisor sign — -7 % 3 = 2" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return -7 % 3");
    try testing.expectEqual(@as(i64, 2), r[0].integer);
}

test "vm: pow returns float — 2 ^ 10 = 1024.0" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return 2 ^ 10");
    try testing.expect(r[0] == .number);
    try testing.expectEqual(@as(f64, 1024.0), r[0].number);
}

test "vm: float / int division — 1 / 2 = 0.5" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return 1 / 2");
    try testing.expect(r[0] == .number);
    try testing.expectEqual(@as(f64, 0.5), r[0].number);
}

test "vm: int + float promotes to float" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return 1 + 2.5");
    try testing.expect(r[0] == .number);
    try testing.expectEqual(@as(f64, 3.5), r[0].number);
}

test "vm: nil / true / false literals" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    {
        const r = try runSrc(arena, "return nil");
        try testing.expect(r[0] == .nil);
    }
    {
        const r = try runSrc(arena, "return true");
        try testing.expect(r[0] == .boolean);
        try testing.expect(r[0].boolean);
    }
    {
        const r = try runSrc(arena, "return false");
        try testing.expect(r[0] == .boolean);
        try testing.expect(!r[0].boolean);
    }
}

test "vm: not operator" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    {
        const r = try runSrc(arena, "return not nil");
        try testing.expect(r[0] == .boolean and r[0].boolean);
    }
    {
        const r = try runSrc(arena, "return not 0");
        // In Lua, 0 is truthy, so `not 0` is false.
        try testing.expect(r[0] == .boolean and !r[0].boolean);
    }
    {
        const r = try runSrc(arena, "return not false");
        try testing.expect(r[0] == .boolean and r[0].boolean);
    }
}

test "vm: bitwise not of integer" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return ~0");
    try testing.expectEqual(@as(i64, -1), r[0].integer);
}

test "vm: multi-value return" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return 1, 2, 3");
    try testing.expectEqual(@as(usize, 3), r.len);
    try testing.expectEqual(@as(i64, 1), r[0].integer);
    try testing.expectEqual(@as(i64, 2), r[1].integer);
    try testing.expectEqual(@as(i64, 3), r[2].integer);
}

test "vm: division by zero is a runtime error" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    try testing.expectError(error.DivByZero, runSrc(arena, "return 5 // 0"));
}

test "vm: arithmetic on non-numeric is a runtime error" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // String concat with `+` fails — Lua only does numeric `+`.
    try testing.expectError(error.InvalidArithmeticOperand, runSrc(arena, "return true + 1"));
}
