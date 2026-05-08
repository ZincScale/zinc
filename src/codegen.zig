//! AST → Lua 5.4 bytecode emitter.
//!
//! Phase 3.2.0 ships the minimum to compile literals, arithmetic,
//! and `return`. Enough to emit bytecode for `return 1 + 2 * 3` and
//! disassemble it. Control flow (jumps), function calls, locals,
//! upvalues, and closures are 3.2.1+.
//!
//! Register model: Lua's VM is register-based. Each function has a
//! register file (local stack frame). Expressions are emitted into
//! the next-free register; binary ops consume two operand registers
//! and write to the destination. Phase 3.2.0 uses a strict
//! stack-discipline allocator: rNext rises and falls with expression
//! evaluation, mirroring lcode.cpp's free_register/exp2nextreg.
//!
//! Constants are interned in the proto's constant table on first use.

const std = @import("std");
const ast = @import("ast.zig");
const bc = @import("bytecode.zig");

const Instruction = bc.Instruction;
const OpCode = bc.OpCode;
const Constant = bc.Constant;
const Proto = bc.Proto;

pub const CompileError = error{
    Unimplemented,
    TooManyConstants,
    StackOverflow,
    OutOfMemory,
};

/// Map AST binary ops to Lua opcodes for the register/register form.
/// Returns null for ops not yet implemented in 3.2.0 (comparison,
/// logical, concat, bitwise).
fn binOpCode(op: ast.BinaryOp) ?OpCode {
    return switch (op) {
        .add => .add,
        .sub => .sub,
        .mul => .mul,
        .div => .div,
        .idiv => .idiv,
        .mod => .mod,
        .pow => .pow,
        else => null,
    };
}

pub const Compiler = struct {
    arena: std.mem.Allocator,
    code: std.ArrayList(Instruction),
    constants: std.ArrayList(Constant),
    /// Next free register index. Allocated incrementally as
    /// expressions emit values.
    next_reg: u8,
    /// High-water mark of register usage. Becomes the proto's
    /// max_stack when we finalize.
    max_reg: u8,

    pub fn init(arena: std.mem.Allocator) Compiler {
        return .{
            .arena = arena,
            .code = .{ .items = &.{}, .capacity = 0 },
            .constants = .{ .items = &.{}, .capacity = 0 },
            .next_reg = 0,
            .max_reg = 0,
        };
    }

    /// Compile a top-level chunk into a Proto. Treats the chunk as
    /// the body of a vararg function with no fixed params (Lua's
    /// "main" chunk convention).
    pub fn compileChunk(self: *Compiler, block: *const ast.Block) CompileError!*Proto {
        try self.emit(Instruction.iABC(.varargprep, 0, 0, 0, 0));

        // Phase 3.2.0 only handles a `return expr` chunk. More
        // statement kinds layer in as we extend.
        for (block.stmts) |s| try self.emitStmt(&s);

        // Implicit return at end of chunk if the last statement
        // wasn't itself a return.
        const last_is_return = block.stmts.len > 0 and block.stmts[block.stmts.len - 1] == .return_stmt;
        if (!last_is_return) {
            try self.emit(Instruction.iABC(.return0, 0, 0, 0, 0));
        }

        const proto = try self.arena.create(Proto);
        proto.* = .{
            .num_params = 0,
            .is_vararg = true,
            .max_stack = self.max_reg,
            .code = try self.code.toOwnedSlice(self.arena),
            .constants = try self.constants.toOwnedSlice(self.arena),
            .protos = &.{},
        };
        return proto;
    }

    // --- statements ------------------------------------------------------

    fn emitStmt(self: *Compiler, s: *const ast.Stmt) CompileError!void {
        switch (s.*) {
            .return_stmt => |r| try self.emitReturn(r),
            else => return error.Unimplemented,
        }
    }

    fn emitReturn(self: *Compiler, r: ast.Stmt.Return) CompileError!void {
        if (r.values.len == 0) {
            try self.emit(Instruction.iABC(.return0, 0, 0, 0, 0));
            return;
        }
        if (r.values.len == 1) {
            // Emit the value into a fresh register, then RETURN1 of it.
            const reg_before = self.next_reg;
            const r1 = try self.exprToReg(r.values[0]);
            try self.emit(Instruction.iABC(.return1, r1, 0, 0, 0));
            // Free the register we used for the return value.
            self.next_reg = reg_before;
            return;
        }
        // Multi-value return: lay values in consecutive registers
        // R[A]..R[A+B-2], then RETURN A B 0. Lua's RETURN encodes
        // count as B with bias: B=0 means "until top", B=1 means 0
        // results, B=2 means 1 result, ..., B=N+1 means N results.
        const base = self.next_reg;
        for (r.values) |val| _ = try self.exprToReg(val);
        const b: u8 = @intCast(r.values.len + 1);
        try self.emit(Instruction.iABC(.return_, base, 0, b, 0));
        self.next_reg = base;
    }

    // --- expressions -----------------------------------------------------

    /// Emit an expression and return the register holding its value.
    /// Always advances next_reg by 1.
    fn exprToReg(self: *Compiler, e: *const ast.Expr) CompileError!u8 {
        const dest = self.allocReg();
        try self.emitExprToDest(e, dest);
        return dest;
    }

    /// Emit an expression specifically into the given destination
    /// register. Caller has already reserved `dest`.
    fn emitExprToDest(self: *Compiler, e: *const ast.Expr, dest: u8) CompileError!void {
        switch (e.*) {
            .nil => try self.emit(Instruction.iABC(.loadnil, dest, 0, 0, 0)),
            .boolean => |b| {
                const op: OpCode = if (b) .loadtrue else .loadfalse;
                try self.emit(Instruction.iABC(op, dest, 0, 0, 0));
            },
            .integer => |n| try self.emitLoadInt(dest, n),
            .number => |f| try self.emitLoadFloat(dest, f),
            .string => |s| {
                const k = try self.addConstant(.{ .string = s });
                try self.emit(Instruction.iABx(.loadk, dest, k));
            },
            .binary => |b| try self.emitBinary(b, dest),
            .unary => |u| try self.emitUnary(u, dest),
            else => return error.Unimplemented,
        }
    }

    fn emitLoadInt(self: *Compiler, dest: u8, n: i64) CompileError!void {
        // OP_LOADI takes sBx (signed 17-bit, biased). For values
        // outside that range we fall back to LOADK with an integer
        // constant.
        if (n >= Instruction.SBX_MIN and n <= Instruction.SBX_MAX) {
            try self.emit(Instruction.iAsBx(.loadi, dest, @intCast(n)));
            return;
        }
        const k = try self.addConstant(.{ .integer = n });
        try self.emit(Instruction.iABx(.loadk, dest, k));
    }

    fn emitLoadFloat(self: *Compiler, dest: u8, f: f64) CompileError!void {
        // OP_LOADF only works for floats whose integer value fits in
        // sBx. For everything else, intern as a constant.
        const as_int = std.math.lossyCast(i64, f);
        if (@as(f64, @floatFromInt(as_int)) == f and
            as_int >= Instruction.SBX_MIN and as_int <= Instruction.SBX_MAX)
        {
            try self.emit(Instruction.iAsBx(.loadf, dest, @intCast(as_int)));
            return;
        }
        const k = try self.addConstant(.{ .number = f });
        try self.emit(Instruction.iABx(.loadk, dest, k));
    }

    fn emitBinary(self: *Compiler, b: ast.Expr.Binary, dest: u8) CompileError!void {
        const opcode = binOpCode(b.op) orelse return error.Unimplemented;

        // Strict stack discipline: emit lhs and rhs into temp
        // registers above dest, then collapse back. Lua's lcode.cpp
        // is more clever (it can reuse dest for the lhs), but the
        // simple form is correct and the optimization is a phase
        // 3.2.x improvement.
        const reg_before = self.next_reg;
        // Make sure dest is reserved so allocReg doesn't reuse it.
        if (dest >= self.next_reg) self.next_reg = dest + 1;
        const lhs_reg = try self.exprToReg(b.lhs);
        const rhs_reg = try self.exprToReg(b.rhs);
        try self.emit(Instruction.iABC(opcode, dest, 0, lhs_reg, rhs_reg));
        // Free the operand temps.
        self.next_reg = reg_before;
        if (dest >= self.next_reg) self.next_reg = dest + 1;
    }

    fn emitUnary(self: *Compiler, u: ast.Expr.Unary, dest: u8) CompileError!void {
        const opcode: OpCode = switch (u.op) {
            .neg => .unm,
            .bnot => .bnot,
            .not_ => .not_,
            .len => .len,
        };
        const reg_before = self.next_reg;
        if (dest >= self.next_reg) self.next_reg = dest + 1;
        const operand_reg = try self.exprToReg(u.operand);
        try self.emit(Instruction.iABC(opcode, dest, 0, operand_reg, 0));
        self.next_reg = reg_before;
        if (dest >= self.next_reg) self.next_reg = dest + 1;
    }

    // --- helpers ---------------------------------------------------------

    fn emit(self: *Compiler, instr: Instruction) CompileError!void {
        try self.code.append(self.arena, instr);
    }

    fn allocReg(self: *Compiler) u8 {
        const r = self.next_reg;
        self.next_reg += 1;
        if (self.next_reg > self.max_reg) self.max_reg = self.next_reg;
        return r;
    }

    fn addConstant(self: *Compiler, c: Constant) CompileError!u17 {
        // De-dup by linear search. Lua's constant tables are small
        // (typically under a few hundred entries), so the O(n) check
        // is fine. A real implementation hashes; we'll add that when
        // the proto sizes warrant it.
        for (self.constants.items, 0..) |existing, idx| {
            if (existing.eql(c)) return @intCast(idx);
        }
        const idx = self.constants.items.len;
        if (idx > Instruction.SBX_MAX) return error.TooManyConstants;
        try self.constants.append(self.arena, c);
        return @intCast(idx);
    }
};

// =============================================================================
// Tests
// =============================================================================

const testing = std.testing;
const parser = @import("parser.zig");

fn compileSrc(arena: std.mem.Allocator, src: []const u8) !*Proto {
    var p = try parser.Parser.init(arena, src);
    const block = try p.parseChunk();
    var c = Compiler.init(arena);
    return c.compileChunk(block);
}

fn dump(arena: std.mem.Allocator, proto: *const Proto) ![]const u8 {
    var aw: std.Io.Writer.Allocating = .init(arena);
    try bc.disassemble(&aw.writer, proto);
    return arena.dupe(u8, aw.writer.buffered());
}

test "compile: return literal int" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const proto = try compileSrc(arena, "return 42");
    try testing.expectEqual(@as(usize, 3), proto.code.len);
    try testing.expectEqual(OpCode.varargprep, proto.code[0].opcode());
    try testing.expectEqual(OpCode.loadi, proto.code[1].opcode());
    try testing.expectEqual(@as(i32, 42), proto.code[1].unpackSBx());
    try testing.expectEqual(OpCode.return1, proto.code[2].opcode());
}

test "compile: return literal float via constant table" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const proto = try compileSrc(arena, "return 3.14");
    // 3.14 doesn't fit LOADF's sBx exactly, so it goes to constants.
    try testing.expectEqual(@as(usize, 1), proto.constants.len);
    try testing.expect(proto.constants[0] == .number);
    try testing.expectEqual(OpCode.loadk, proto.code[1].opcode());
}

test "compile: return string constant" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const proto = try compileSrc(arena, "return \"hello\"");
    try testing.expectEqual(@as(usize, 1), proto.constants.len);
    try testing.expect(proto.constants[0] == .string);
    try testing.expectEqualStrings("hello", proto.constants[0].string);
}

test "compile: return nil / true / false" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    {
        const p = try compileSrc(arena, "return nil");
        try testing.expectEqual(OpCode.loadnil, p.code[1].opcode());
    }
    {
        const p = try compileSrc(arena, "return true");
        try testing.expectEqual(OpCode.loadtrue, p.code[1].opcode());
    }
    {
        const p = try compileSrc(arena, "return false");
        try testing.expectEqual(OpCode.loadfalse, p.code[1].opcode());
    }
}

test "compile: arithmetic precedence in bytecode" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // 1 + 2 * 3 should emit: load(1), load(2), load(3), mul(t1, 2, 3),
    // add(dest, 1, t1), return1(dest).
    const proto = try compileSrc(arena, "return 1 + 2 * 3");
    var seen_add = false;
    var seen_mul = false;
    for (proto.code) |i| {
        if (i.opcode() == .add) seen_add = true;
        if (i.opcode() == .mul) seen_mul = true;
    }
    try testing.expect(seen_add);
    try testing.expect(seen_mul);
}

test "compile: unary negation and length" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    {
        const p = try compileSrc(arena, "return -5");
        var seen_unm = false;
        for (p.code) |i| {
            if (i.opcode() == .unm) seen_unm = true;
        }
        try testing.expect(seen_unm);
    }
}

test "compile: constant deduplication" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const proto = try compileSrc(arena, "return \"x\", \"x\", \"y\"");
    // Three values, but only 2 unique strings → 2 constants.
    try testing.expectEqual(@as(usize, 2), proto.constants.len);
}

test "compile: disassembly is well-formed" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const proto = try compileSrc(arena, "return 1 + 2");
    const out = try dump(arena, proto);
    // Sanity: contains the header and the relevant opcodes.
    try testing.expect(std.mem.indexOf(u8, out, "params=") != null);
    try testing.expect(std.mem.indexOf(u8, out, "VARARGPREP") != null or std.mem.indexOf(u8, out, "varargprep") != null);
    try testing.expect(std.mem.indexOf(u8, out, "loadi") != null);
    try testing.expect(std.mem.indexOf(u8, out, "add") != null);
    try testing.expect(std.mem.indexOf(u8, out, "return1") != null);
}

test "compile: unimplemented stmt → clean error" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // `local x = 1` is parsed but not yet emittable; phase 3.2.1 will
    // add it. The codegen returns a clean error rather than crashing.
    try testing.expectError(error.Unimplemented, compileSrc(arena, "local x = 1"));
}
