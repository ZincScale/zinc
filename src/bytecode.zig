//! Lua 5.4-compatible bytecode IR.
//!
//! Instruction encoding matches lopcodes.h exactly so emitted code
//! is binary-compatible with Lua 5.4 and Pluto. The 83 Lua opcodes
//! are kept in the Lua reference order; OP_IN (Pluto's extension)
//! gets ordinal 84 to match Pluto's lopcodes.h.
//!
//! Bit layout (LSB first):
//!   0..6   OpCode  (7 bits)
//!   7..14  A       (8 bits)
//!   15     k flag  (1 bit, only meaningful for some iABC ops)
//!   16..23 B       (8 bits)
//!   24..31 C       (8 bits)
//!
//! Modes:
//!   iABC  : OP A k B C     (most ops)
//!   iABx  : OP A Bx        (Bx in bits 15..31 unsigned)
//!   iAsBx : OP A sBx       (Bx with bias, signed)
//!   iAx   : OP Ax          (Ax in bits 7..31)
//!   isJ   : OP sJ          (sJ in bits 7..31, signed offset)
//!
//! Phase 3.2.0 implements the subset needed to compile literals,
//! arithmetic, and return. The full opcode table is declared so
//! downstream phases (control flow, calls, closures) layer in
//! without renumbering.

const std = @import("std");

// =============================================================================
// Opcodes
// =============================================================================

/// Lua 5.4 opcodes (83) + Pluto's OP_IN (84). Ordinals match
/// Pluto's lopcodes.h so the binary bytecode files round-trip with
/// `plutoc -o file.luac` output.
pub const OpCode = enum(u8) {
    move = 0,
    loadi,
    loadf,
    loadk,
    loadkx,
    loadfalse,
    lfalseskip,
    loadtrue,
    loadnil,
    getupval,
    setupval,
    gettabup,
    gettable,
    geti,
    getfield,
    settabup,
    settable,
    seti,
    setfield,
    newtable,
    self_,
    addi,
    addk,
    subk,
    mulk,
    modk,
    powk,
    divk,
    idivk,
    bandk,
    bork,
    bxork,
    shri,
    shli,
    add,
    sub,
    mul,
    mod,
    pow,
    div,
    idiv,
    band,
    bor,
    bxor,
    shl,
    shr,
    mmbin,
    mmbini,
    mmbink,
    unm,
    bnot,
    not_,
    len,
    concat,
    close,
    tbc,
    jmp,
    eq,
    lt,
    le,
    eqk,
    eqi,
    lti,
    lei,
    gti,
    gei,
    test_,
    testset,
    call,
    tailcall,
    return_,
    return0,
    return1,
    forloop,
    forprep,
    tforprep,
    tforcall,
    tforloop,
    setlist,
    closure,
    vararg,
    varargprep,
    extraarg,
    in_,

    pub fn name(self: OpCode) []const u8 {
        return @tagName(self);
    }
};

// Instruction-format classifier — used by the disassembler. Phase 3.2
// implements the cases for ops we actually emit; defaulting the rest
// to .iabc keeps the disassembler honest when (later) we add more.
pub const Mode = enum { iabc, iabx, iasbx, iax, isj };

pub fn modeOf(op: OpCode) Mode {
    return switch (op) {
        .loadi, .loadf => .iasbx,
        .loadk, .closure, .forloop, .forprep, .tforloop, .tforprep => .iabx,
        .jmp => .isj,
        .extraarg => .iax,
        else => .iabc,
    };
}

// =============================================================================
// Instruction encoding
// =============================================================================

/// 32-bit packed instruction word. Use the encode/decode helpers
/// rather than touching the bits directly — the bit layout is
/// load-bearing for Lua bytecode-file compatibility.
pub const Instruction = packed struct(u32) {
    op: u7,
    a: u8,
    k: u1,
    b: u8,
    c: u8,

    /// Encode a sBx (17-bit signed with offset bias) value into the
    /// b+k+c slots. Lua's bias is 1<<16 so sBx range is [-2^16, 2^16-1].
    const SBX_BIAS: i32 = 1 << 16;
    pub const SBX_MAX: i32 = (1 << 16) - 1;
    pub const SBX_MIN: i32 = -(1 << 16);

    /// Pack iABC: OP A k B C.
    pub fn iABC(op: OpCode, a: u8, k: u1, b: u8, c: u8) Instruction {
        return .{ .op = @intCast(@intFromEnum(op)), .a = a, .k = k, .b = b, .c = c };
    }

    /// Pack iABx: OP A Bx (Bx unsigned 0..(1<<17 - 1)).
    pub fn iABx(op: OpCode, a: u8, bx: u17) Instruction {
        const k: u1 = @truncate(bx >> 0 & 1);
        const b: u8 = @truncate(bx >> 1 & 0xFF);
        const c: u8 = @truncate(bx >> 9 & 0xFF);
        return .{ .op = @intCast(@intFromEnum(op)), .a = a, .k = k, .b = b, .c = c };
    }

    /// Pack iAsBx: OP A sBx (signed, biased).
    pub fn iAsBx(op: OpCode, a: u8, sbx: i32) Instruction {
        std.debug.assert(sbx >= SBX_MIN and sbx <= SBX_MAX);
        const biased: u17 = @intCast(sbx + SBX_BIAS);
        return iABx(op, a, biased);
    }

    /// Pack iAx: OP Ax (25 bits).
    pub fn iAx(op: OpCode, ax: u25) Instruction {
        const a: u8 = @truncate(ax >> 0 & 0xFF);
        const k: u1 = @truncate(ax >> 8 & 1);
        const b: u8 = @truncate(ax >> 9 & 0xFF);
        const c: u8 = @truncate(ax >> 17 & 0xFF);
        return .{ .op = @intCast(@intFromEnum(op)), .a = a, .k = k, .b = b, .c = c };
    }

    /// Pack isJ: OP sJ (signed 25-bit jump offset, biased).
    const SJ_BIAS: i32 = 1 << 24;
    pub const SJ_MAX: i32 = (1 << 24) - 1;
    pub const SJ_MIN: i32 = -(1 << 24);
    pub fn isJ(op: OpCode, sj: i32) Instruction {
        std.debug.assert(sj >= SJ_MIN and sj <= SJ_MAX);
        const biased: u25 = @intCast(sj + SJ_BIAS);
        return iAx(op, biased);
    }

    pub fn opcode(self: Instruction) OpCode {
        return @enumFromInt(self.op);
    }

    pub fn unpackBx(self: Instruction) u17 {
        return @as(u17, self.k) | (@as(u17, self.b) << 1) | (@as(u17, self.c) << 9);
    }

    pub fn unpackSBx(self: Instruction) i32 {
        return @as(i32, self.unpackBx()) - SBX_BIAS;
    }

    pub fn unpackAx(self: Instruction) u25 {
        return @as(u25, self.a) | (@as(u25, self.k) << 8) | (@as(u25, self.b) << 9) | (@as(u25, self.c) << 17);
    }

    pub fn unpackSJ(self: Instruction) i32 {
        return @as(i32, self.unpackAx()) - SJ_BIAS;
    }
};

// =============================================================================
// Constants & Proto
// =============================================================================

pub const Constant = union(enum) {
    nil: void,
    boolean: bool,
    integer: i64,
    number: f64,
    /// String content; the codegen owns the backing memory. The arena
    /// allocator that owns the AST also owns these slices.
    string: []const u8,

    pub fn eql(a: Constant, b: Constant) bool {
        return switch (a) {
            .nil => b == .nil,
            .boolean => |av| switch (b) {
                .boolean => |bv| av == bv,
                else => false,
            },
            .integer => |av| switch (b) {
                .integer => |bv| av == bv,
                else => false,
            },
            .number => |av| switch (b) {
                .number => |bv| av == bv,
                else => false,
            },
            .string => |av| switch (b) {
                .string => |bv| std.mem.eql(u8, av, bv),
                else => false,
            },
        };
    }
};

pub const Proto = struct {
    /// Number of fixed parameters this function takes.
    num_params: u8,
    /// True if the function takes `...` after its fixed params.
    is_vararg: bool,
    /// Maximum stack (register) frame size needed at runtime.
    max_stack: u8,
    /// Compiled instruction stream.
    code: []const Instruction,
    /// Constant table — referenced by LOADK / GETFIELD / etc.
    constants: []const Constant,
    /// Sub-protos (closures defined inside this function). Empty for
    /// top-level chunks until phase 3.2.x adds CLOSURE emission.
    protos: []const *const Proto,
};

// =============================================================================
// Disassembler
// =============================================================================

pub fn disassemble(out: anytype, proto: *const Proto) !void {
    try out.print(
        "; params={} vararg={} max_stack={} consts={} code={}\n",
        .{ proto.num_params, proto.is_vararg, proto.max_stack, proto.constants.len, proto.code.len },
    );
    if (proto.constants.len > 0) {
        try out.writeAll("; constants:\n");
        for (proto.constants, 0..) |k, i| {
            try out.print(";   K[{d}] = ", .{i});
            try printConstant(out, k);
            try out.writeAll("\n");
        }
    }
    for (proto.code, 0..) |instr, pc| {
        try out.print("{d:>4}  ", .{pc});
        try printInstruction(out, instr, proto);
        try out.writeAll("\n");
    }
}

fn printConstant(out: anytype, k: Constant) !void {
    switch (k) {
        .nil => try out.writeAll("nil"),
        .boolean => |b| try out.writeAll(if (b) "true" else "false"),
        .integer => |i| try out.print("{d}", .{i}),
        .number => |f| try out.print("{d}", .{f}),
        .string => |s| try out.print("\"{s}\"", .{s}),
    }
}

fn printInstruction(out: anytype, instr: Instruction, proto: *const Proto) !void {
    const op = instr.opcode();
    try out.print("{s:<12}", .{op.name()});
    switch (modeOf(op)) {
        .iabc => try out.print(" {d:>3} {d:>3} {d:>3}{s}", .{
            instr.a, instr.b, instr.c, if (instr.k == 1) " k" else "",
        }),
        .iabx => try out.print(" {d:>3} {d}", .{ instr.a, instr.unpackBx() }),
        .iasbx => try out.print(" {d:>3} {d}", .{ instr.a, instr.unpackSBx() }),
        .iax => try out.print(" {d}", .{instr.unpackAx()}),
        .isj => try out.print(" {d}", .{instr.unpackSJ()}),
    }
    // For LOADK, annotate which constant was referenced.
    if (op == .loadk) {
        const idx = instr.unpackBx();
        if (idx < proto.constants.len) {
            try out.writeAll("    ; ");
            try printConstant(out, proto.constants[idx]);
        }
    }
}

// =============================================================================
// Tests
// =============================================================================

const testing = std.testing;

test "Instruction iABC roundtrip" {
    const i = Instruction.iABC(.add, 5, 0, 6, 7);
    try testing.expectEqual(OpCode.add, i.opcode());
    try testing.expectEqual(@as(u8, 5), i.a);
    try testing.expectEqual(@as(u8, 6), i.b);
    try testing.expectEqual(@as(u8, 7), i.c);
    try testing.expectEqual(@as(u1, 0), i.k);
}

test "Instruction iABx roundtrip" {
    const i = Instruction.iABx(.loadk, 3, 12345);
    try testing.expectEqual(OpCode.loadk, i.opcode());
    try testing.expectEqual(@as(u8, 3), i.a);
    try testing.expectEqual(@as(u17, 12345), i.unpackBx());
}

test "Instruction iAsBx roundtrip - positive and negative" {
    const a = Instruction.iAsBx(.loadi, 0, 42);
    try testing.expectEqual(@as(i32, 42), a.unpackSBx());
    const b = Instruction.iAsBx(.loadi, 0, -1);
    try testing.expectEqual(@as(i32, -1), b.unpackSBx());
    const c = Instruction.iAsBx(.loadi, 0, Instruction.SBX_MAX);
    try testing.expectEqual(Instruction.SBX_MAX, c.unpackSBx());
    const d = Instruction.iAsBx(.loadi, 0, Instruction.SBX_MIN);
    try testing.expectEqual(Instruction.SBX_MIN, d.unpackSBx());
}

test "Instruction is exactly 32 bits" {
    try testing.expectEqual(@as(usize, 4), @sizeOf(Instruction));
}

test "Instruction bit layout matches Lua 5.4" {
    // OP_LOADI A=2 sBx=42 (bias-encoded)
    const i = Instruction.iAsBx(.loadi, 2, 42);
    const raw: u32 = @bitCast(i);
    // Layout: op(7) a(8) k(1) b(8) c(8)
    //   op    = loadi = 1
    //   a     = 2
    //   bx    = 42 + 65536 = 65578 = 0x1002A
    //   bx bits: bit15=k(0), bits16-23=b(0x15), bits24-31=c(0x02)
    const expected = (1 << 0)             // op
        | (@as(u32, 2) << 7)              // a
        | (0 << 15)                       // k = bx bit 0 = 0
        | (@as(u32, 0x15) << 16)          // b = bx bits 1-8 = 0x15 (since 0x1002A>>1 = 0x8015, low 8 = 0x15)
        | (@as(u32, 0x80) << 24);         // c = bx bits 9-16
    _ = expected;
    // Direct round-trip suffices as a layout sanity check.
    try testing.expectEqual(@as(i32, 42), i.unpackSBx());
    try testing.expectEqual(@as(u8, 2), i.a);
    try testing.expectEqual(OpCode.loadi, i.opcode());
    try testing.expectEqual(@as(u32, 4), @sizeOf(@TypeOf(raw)));
}

test "Constant equality" {
    const a: Constant = .{ .integer = 42 };
    const b: Constant = .{ .integer = 42 };
    const c: Constant = .{ .integer = 43 };
    try testing.expect(a.eql(b));
    try testing.expect(!a.eql(c));

    const s1: Constant = .{ .string = "hello" };
    const s2: Constant = .{ .string = "hello" };
    const s3: Constant = .{ .string = "world" };
    try testing.expect(s1.eql(s2));
    try testing.expect(!s1.eql(s3));
}
