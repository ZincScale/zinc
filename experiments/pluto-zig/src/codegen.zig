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
    UnknownIdentifier,
    InvalidAssignmentTarget,
    OutOfMemory,
};

/// A local variable: name → register binding. Locals occupy the
/// lowest registers in the frame; expression temporaries go above.
const Local = struct {
    name: []const u8,
    reg: u8,
};

/// Where a name resolves to in the current function. Used by ident
/// reads, assignments, and call targets.
const Resolution = union(enum) {
    local: u8,
    upvalue: u8,
};

/// Map AST binary ops to Lua opcodes for the register/register form.
/// Returns null for ops not yet implemented in 3.2.0 (logical,
/// concat, bitwise).
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

/// How to compile an AST comparison op to a Lua comparison opcode.
/// Lua only has EQ, LT, LE — gt and gte get implemented as LT/LE
/// with operands swapped (a > b becomes b < a). neq inverts polarity.
const CompareForm = struct { op: OpCode, swap: bool, invert: bool };

fn compareOpCode(op: ast.BinaryOp) ?CompareForm {
    return switch (op) {
        .eq => .{ .op = .eq, .swap = false, .invert = false },
        .neq => .{ .op = .eq, .swap = false, .invert = true },
        .lt => .{ .op = .lt, .swap = false, .invert = false },
        .lte => .{ .op = .le, .swap = false, .invert = false },
        .gt => .{ .op = .lt, .swap = true, .invert = false },
        .gte => .{ .op = .le, .swap = true, .invert = false },
        else => null,
    };
}

pub const Compiler = struct {
    arena: std.mem.Allocator,
    /// Enclosing compiler when this is a nested function body. null
    /// for the top-level chunk. Used by ident resolution to walk
    /// outward looking for upvalue captures.
    parent: ?*Compiler,
    code: std.ArrayList(Instruction),
    constants: std.ArrayList(Constant),
    protos: std.ArrayList(*const bc.Proto),
    /// Upvalue descriptors for the proto being compiled — populated
    /// as the body references names from enclosing scopes.
    upvalues: std.ArrayList(bc.UpvalueDesc),
    locals: std.ArrayList(Local),
    next_reg: u8,
    max_reg: u8,
    num_params: u8,
    is_vararg: bool,

    pub fn init(arena: std.mem.Allocator) Compiler {
        return .{
            .arena = arena,
            .parent = null,
            .code = .{ .items = &.{}, .capacity = 0 },
            .constants = .{ .items = &.{}, .capacity = 0 },
            .protos = .{ .items = &.{}, .capacity = 0 },
            .upvalues = .{ .items = &.{}, .capacity = 0 },
            .locals = .{ .items = &.{}, .capacity = 0 },
            .next_reg = 0,
            .max_reg = 0,
            .num_params = 0,
            .is_vararg = true, // top-level chunk is vararg
        };
    }

    fn initNested(arena: std.mem.Allocator, parent: *Compiler) Compiler {
        var c = Compiler.init(arena);
        c.parent = parent;
        return c;
    }

    fn findLocal(self: *const Compiler, name: []const u8) ?u8 {
        // Search backwards so inner shadows outer.
        var i: usize = self.locals.items.len;
        while (i > 0) {
            i -= 1;
            const l = self.locals.items[i];
            if (std.mem.eql(u8, l.name, name)) return l.reg;
        }
        return null;
    }

    fn findUpvalue(self: *const Compiler, name: []const u8) ?u8 {
        for (self.upvalues.items, 0..) |u, i| {
            if (std.mem.eql(u8, u.name, name)) return @intCast(i);
        }
        return null;
    }

    /// Look the name up in this function's locals, then upvalues, then
    /// recursively in enclosing scopes. If found in an enclosing scope,
    /// register an upvalue descriptor in *this* function so the runtime
    /// can wire it up at CLOSURE time.
    fn resolveIdent(self: *Compiler, name: []const u8) CompileError!?Resolution {
        if (self.findLocal(name)) |r| return Resolution{ .local = r };
        if (self.findUpvalue(name)) |i| return Resolution{ .upvalue = i };
        if (self.parent) |p| {
            const parent_res = try p.resolveIdent(name) orelse return null;
            const desc: bc.UpvalueDesc = switch (parent_res) {
                .local => |reg| .{ .name = name, .in_stack = true, .idx = reg },
                .upvalue => |i| .{ .name = name, .in_stack = false, .idx = i },
            };
            const new_idx = self.upvalues.items.len;
            try self.upvalues.append(self.arena, desc);
            return Resolution{ .upvalue = @intCast(new_idx) };
        }
        return null;
    }

    /// Compile a top-level chunk into a Proto. Treats the chunk as
    /// the body of a vararg function with no fixed params (Lua's
    /// "main" chunk convention).
    pub fn compileChunk(self: *Compiler, block: *const ast.Block) CompileError!*Proto {
        return self.compileFunctionBody(&.{}, true, block);
    }

    /// Compile a function body — params + block — into a Proto. Used
    /// for both the top-level chunk and nested function expressions.
    /// The compiler's local/code/constants/protos state is reset on
    /// entry; nested function compilation uses a *separate* Compiler.
    pub fn compileFunctionBody(
        self: *Compiler,
        params: []const []const u8,
        is_vararg: bool,
        block: *const ast.Block,
    ) CompileError!*Proto {
        self.num_params = @intCast(params.len);
        self.is_vararg = is_vararg;

        // Bind each formal param as a local in R[0..num_params-1].
        // The VM lays args in those slots when CALL executes.
        for (params) |name| {
            const r = self.allocReg();
            try self.locals.append(self.arena, .{ .name = name, .reg = r });
        }

        try self.emit(Instruction.iABC(.varargprep, 0, 0, 0, 0));

        for (block.stmts) |s| try self.emitStmt(&s);

        // Implicit return at end of body if the last statement
        // wasn't itself a return.
        const last_is_return = block.stmts.len > 0 and block.stmts[block.stmts.len - 1] == .return_stmt;
        if (!last_is_return) {
            try self.emit(Instruction.iABC(.return0, 0, 0, 0, 0));
        }

        const proto = try self.arena.create(Proto);
        proto.* = .{
            .num_params = self.num_params,
            .is_vararg = self.is_vararg,
            .max_stack = self.max_reg,
            .code = try self.code.toOwnedSlice(self.arena),
            .constants = try self.constants.toOwnedSlice(self.arena),
            .protos = try self.protos.toOwnedSlice(self.arena),
            .upvalues = try self.upvalues.toOwnedSlice(self.arena),
        };
        return proto;
    }

    // --- statements ------------------------------------------------------

    fn emitStmt(self: *Compiler, s: *const ast.Stmt) CompileError!void {
        switch (s.*) {
            .return_stmt => |r| try self.emitReturn(r),
            .local => |l| try self.emitLocal(l),
            .assign => |a| try self.emitAssign(a),
            .if_stmt => |if_s| try self.emitIf(if_s),
            .while_stmt => |w| try self.emitWhile(w),
            .expr_stmt => |e| try self.emitExprStmt(e),
            .local_function => |lf| try self.emitLocalFunction(lf),
            else => return error.Unimplemented,
        }
    }

    /// Function-call statement — call but discard results. CALL with
    /// C=1 means "no results expected".
    fn emitExprStmt(self: *Compiler, e: *ast.Expr) CompileError!void {
        if (e.* != .call) return error.Unimplemented;
        const reg = self.allocReg();
        try self.emitCall(e.call, reg, 0);
        self.next_reg = reg; // free the temp
    }

    /// `local function f(args) body end`. Lua's spec: declare the
    /// local *before* compiling the body so the body can reference
    /// `f` recursively (it becomes an upvalue capture in our model).
    /// Sequence:
    ///   1. allocate a register, register `f` as a local at that reg
    ///   2. emit LOADNIL into the register (initial value while the
    ///      body compiles — the open upvalue points here)
    ///   3. compile body (body's references to f register as upvalues)
    ///   4. emit CLOSURE into the same register; the open upvalue
    ///      now sees the closure value
    fn emitLocalFunction(self: *Compiler, lf: ast.Stmt.LocalFunction) CompileError!void {
        const reg = self.allocReg();
        try self.locals.append(self.arena, .{ .name = lf.name, .reg = reg });
        try self.emit(Instruction.iABC(.loadnil, reg, 0, 0, 0));
        try self.emitFunctionExpr(lf.func, reg);
    }

    /// `if c1 then b1 elseif c2 then b2 else eb end`. Compiles to:
    ///
    ///   <eval c1 into X>
    ///   TEST X k=0           ; skip next (the JMP) if X is truthy
    ///   JMP <else1>          ; otherwise fall through to else1
    ///   <b1>
    ///   JMP <after>
    /// else1:
    ///   <eval c2 into X>
    ///   TEST X k=0
    ///   JMP <else2>
    ///   <b2>
    ///   JMP <after>
    /// else2:
    ///   <eb>
    /// after:
    ///
    /// Forward jumps are emitted with placeholder offsets and patched
    /// once their targets are known.
    fn emitIf(self: *Compiler, if_s: ast.Stmt.If) CompileError!void {
        var jumps_to_after = std.ArrayList(usize){ .items = &.{}, .capacity = 0 };

        for (if_s.branches) |br| {
            // Evaluate condition into a temp register (freed after
            // the test consumes it — the test only reads the value).
            const reg_before = self.next_reg;
            const cond_reg = try self.exprToReg(br.cond);

            // TEST cond_reg k=0 — if cond_reg is truthy, skip next.
            try self.emit(Instruction.iABC(.test_, cond_reg, 0, 0, 0));

            // The jump-to-next-branch (or end). Patched later.
            const jump_to_next_branch = self.code.items.len;
            try self.emit(Instruction.iAx(.jmp, 0)); // placeholder

            // Free the cond register once the test+jump have it
            // captured in PC; the body re-claims any temp regs.
            self.next_reg = reg_before;

            // Body of this branch.
            try self.emitBlock(br.body);

            // After running the body, jump unconditionally past any
            // remaining branches and the else.
            const jump_to_after = self.code.items.len;
            try self.emit(Instruction.iAx(.jmp, 0)); // placeholder
            try jumps_to_after.append(self.arena, jump_to_after);

            // Patch the jump-to-next-branch to land on the next
            // instruction (start of the next branch or else).
            self.patchJump(jump_to_next_branch, self.code.items.len);
        }

        // Else block (optional).
        if (if_s.else_block) |eb| try self.emitBlock(eb);

        // All "jumps to after" land here.
        const after_pc = self.code.items.len;
        for (jumps_to_after.items) |j| self.patchJump(j, after_pc);
    }

    /// `while cond do body end`. Compiles to:
    ///
    /// loop_start:
    ///   <eval cond into X>
    ///   TEST X k=0           ; skip next (the JMP) if X is truthy
    ///   JMP <loop_end>       ; otherwise jump out
    ///   <body>
    ///   JMP <loop_start>
    /// loop_end:
    fn emitWhile(self: *Compiler, w: ast.Stmt.While) CompileError!void {
        const loop_start = self.code.items.len;

        const reg_before = self.next_reg;
        const cond_reg = try self.exprToReg(w.cond);
        try self.emit(Instruction.iABC(.test_, cond_reg, 0, 0, 0));
        const jump_to_end = self.code.items.len;
        try self.emit(Instruction.iAx(.jmp, 0)); // placeholder
        self.next_reg = reg_before;

        try self.emitBlock(w.body);

        // Unconditional jump back to loop_start.
        try self.emitJump(loop_start);

        const loop_end = self.code.items.len;
        self.patchJump(jump_to_end, loop_end);
    }

    fn emitBlock(self: *Compiler, b: *const ast.Block) CompileError!void {
        // No scope tracking yet — locals declared inside an if/while
        // body persist after it ends. Phase 3.2.x adds proper block
        // scoping with locals.shrink + register reclaim.
        for (b.stmts) |s| try self.emitStmt(&s);
    }

    /// Emit a JMP whose target is already known (i.e. a backwards
    /// jump such as while's loop-back).
    fn emitJump(self: *Compiler, target_pc: usize) CompileError!void {
        const here = self.code.items.len;
        // After this JMP runs, PC will be `here + 1`. We want it to
        // become `target_pc`. So the offset is target_pc - (here+1).
        const offset = @as(i32, @intCast(target_pc)) - @as(i32, @intCast(here + 1));
        try self.emit(Instruction.isJ(.jmp, offset));
    }

    /// Patch a JMP that was emitted with a placeholder offset, now
    /// that its target PC is known.
    fn patchJump(self: *Compiler, jump_pc: usize, target_pc: usize) void {
        const offset = @as(i32, @intCast(target_pc)) - @as(i32, @intCast(jump_pc + 1));
        self.code.items[jump_pc] = Instruction.isJ(.jmp, offset);
    }

    /// `local a, b, ... = e1, e2, ...` — evaluate values into the
    /// next-available registers, then bind the names to those
    /// registers (in order). Extra names get nil.
    fn emitLocal(self: *Compiler, l: ast.Stmt.Local) CompileError!void {
        const base = self.next_reg;
        // Evaluate each value into the next free register. exprToReg
        // bumps next_reg, so values land at base, base+1, base+2, ...
        for (l.values) |val| _ = try self.exprToReg(val);
        // Pad with nil for names without matching values.
        var i: usize = l.values.len;
        while (i < l.names.len) : (i += 1) {
            const r = self.allocReg();
            try self.emit(Instruction.iABC(.loadnil, r, 0, 0, 0));
        }
        // Bind the names. The registers we just filled become locals
        // and stay reserved for the rest of the chunk (no scope exit
        // in 3.2.1 — that's the job of phase 3.2.x when blocks land).
        for (l.names, 0..) |name, idx| {
            try self.locals.append(self.arena, .{
                .name = name,
                .reg = @intCast(base + idx),
            });
        }
    }

    /// Single-target assignment. Targets supported: ident (local or
    /// upvalue), field (`obj.name`), index (`obj[key]`). Multi-target
    /// is deferred until we sort out evaluation order semantics.
    fn emitAssign(self: *Compiler, a: ast.Stmt.Assign) CompileError!void {
        if (a.targets.len != 1 or a.values.len != 1) return error.Unimplemented;
        const target = a.targets[0];
        switch (target.*) {
            .ident => |name| {
                const res = try self.resolveIdent(name) orelse return error.UnknownIdentifier;
                switch (res) {
                    .local => |r| try self.emitExprToDest(a.values[0], r),
                    .upvalue => |idx| {
                        const reg_before = self.next_reg;
                        const tmp = try self.exprToReg(a.values[0]);
                        try self.emit(Instruction.iABC(.setupval, tmp, 0, idx, 0));
                        self.next_reg = reg_before;
                    },
                }
            },
            .field => |f| {
                // obj.name = value -> SETFIELD obj_reg, k_idx, val_reg
                const reg_before = self.next_reg;
                const obj_reg = try self.exprToReg(f.object);
                const val_reg = try self.exprToReg(a.values[0]);
                const k_idx = try self.addConstant(.{ .string = f.name });
                if (k_idx > 255) return error.TooManyConstants;
                try self.emit(Instruction.iABC(.setfield, obj_reg, 0, @intCast(k_idx), val_reg));
                self.next_reg = reg_before;
            },
            .index => |idx| {
                // obj[key] = value -> SETTABLE obj_reg, key_reg, val_reg
                const reg_before = self.next_reg;
                const obj_reg = try self.exprToReg(idx.object);
                const key_reg = try self.exprToReg(idx.key);
                const val_reg = try self.exprToReg(a.values[0]);
                try self.emit(Instruction.iABC(.settable, obj_reg, 0, key_reg, val_reg));
                self.next_reg = reg_before;
            },
            else => return error.InvalidAssignmentTarget,
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
            .ident => |name| {
                const res = try self.resolveIdent(name) orelse return error.UnknownIdentifier;
                switch (res) {
                    .local => |r| {
                        if (r != dest) try self.emit(Instruction.iABC(.move, dest, 0, r, 0));
                    },
                    .upvalue => |idx| try self.emit(Instruction.iABC(.getupval, dest, 0, idx, 0)),
                }
            },
            .binary => |b| try self.emitBinary(b, dest),
            .unary => |u| try self.emitUnary(u, dest),
            .function => |f| try self.emitFunctionExpr(f, dest),
            .call => |c| try self.emitCall(c, dest, 1), // expression context wants 1 result
            .table => |t| try self.emitTableExpr(t, dest),
            .field => |f| try self.emitFieldGet(f, dest),
            .index => |i| try self.emitIndexGet(i, dest),
            else => return error.Unimplemented,
        }
    }

    /// Table constructor: `{1, 2, 3}`, `{a = 1}`, `{[k] = v}`, mixed.
    /// Strategy: emit NEWTABLE, then evaluate positional ("array part")
    /// fields into consecutive registers above `dest` and bulk-set
    /// them with SETLIST. Named/computed fields get their own SETFIELD
    /// or SETTABLE per field. Order is preserved.
    fn emitTableExpr(self: *Compiler, t: ast.Expr.Table, dest: u8) CompileError!void {
        const reg_before = self.next_reg;
        if (dest >= self.next_reg) self.next_reg = dest + 1;

        try self.emit(Instruction.iABC(.newtable, dest, 0, 0, 0));

        // First pass: positional values into R[dest+1], R[dest+2], ...
        var pos_count: u8 = 0;
        for (t.fields) |f| {
            if (f.key == null) {
                // Force the value into the next contiguous register.
                const target = dest + 1 + pos_count;
                if (target >= self.next_reg) self.next_reg = target + 1;
                if (target + 1 > self.max_reg) self.max_reg = target + 1;
                try self.emitExprToDest(f.value, target);
                pos_count += 1;
            }
        }

        // SETLIST t[1..pos_count] = R[dest+1..dest+pos_count].
        if (pos_count > 0) {
            try self.emit(Instruction.iABC(.setlist, dest, 0, pos_count, 0));
        }

        // Second pass: keyed fields. For named keys we use SETFIELD
        // (B = string-constant index). For computed keys, SETTABLE.
        for (t.fields) |f| {
            if (f.key) |k| switch (k) {
                .named => |name| {
                    const k_idx = try self.addConstant(.{ .string = name });
                    if (k_idx > 255) return error.TooManyConstants;
                    const val_reg_before = self.next_reg;
                    const val_reg = try self.exprToReg(f.value);
                    try self.emit(Instruction.iABC(.setfield, dest, 0, @intCast(k_idx), val_reg));
                    self.next_reg = val_reg_before;
                },
                .computed => |key_expr| {
                    const ks_before = self.next_reg;
                    const key_reg = try self.exprToReg(key_expr);
                    const val_reg = try self.exprToReg(f.value);
                    try self.emit(Instruction.iABC(.settable, dest, 0, key_reg, val_reg));
                    self.next_reg = ks_before;
                },
            };
        }

        self.next_reg = reg_before;
        if (dest >= self.next_reg) self.next_reg = dest + 1;
    }

    fn emitFieldGet(self: *Compiler, f: ast.Expr.Field, dest: u8) CompileError!void {
        const reg_before = self.next_reg;
        if (dest >= self.next_reg) self.next_reg = dest + 1;
        const obj_reg = try self.exprToReg(f.object);
        const k_idx = try self.addConstant(.{ .string = f.name });
        if (k_idx > 255) return error.TooManyConstants;
        try self.emit(Instruction.iABC(.getfield, dest, 0, obj_reg, @intCast(k_idx)));
        self.next_reg = reg_before;
        if (dest >= self.next_reg) self.next_reg = dest + 1;
    }

    fn emitIndexGet(self: *Compiler, i: ast.Expr.Index, dest: u8) CompileError!void {
        const reg_before = self.next_reg;
        if (dest >= self.next_reg) self.next_reg = dest + 1;
        const obj_reg = try self.exprToReg(i.object);
        const key_reg = try self.exprToReg(i.key);
        try self.emit(Instruction.iABC(.gettable, dest, 0, obj_reg, key_reg));
        self.next_reg = reg_before;
        if (dest >= self.next_reg) self.next_reg = dest + 1;
    }

    /// Compile a `function(args) body end` expression into a sub-
    /// Proto, register it on the parent, and emit CLOSURE A Bx. The
    /// nested compiler resolves names by walking up to `self`, so
    /// any reference to an enclosing local registers as an upvalue
    /// descriptor on the sub-Proto. The runtime CLOSURE handler reads
    /// those descriptors to wire the cells.
    fn emitFunctionExpr(self: *Compiler, f: ast.Expr.Function, dest: u8) CompileError!void {
        var nested = Compiler.initNested(self.arena, self);
        const sub_proto = try nested.compileFunctionBody(f.params, f.has_vararg, f.body);
        const proto_idx: u17 = @intCast(self.protos.items.len);
        try self.protos.append(self.arena, sub_proto);
        try self.emit(Instruction.iABx(.closure, dest, proto_idx));
    }

    /// Emit a call expression. `num_results` is how many values the
    /// caller expects (1 for expression context, N for `local a,b,c
    /// = f()`, special case 0 for statement context with no
    /// assignment). Lua's CALL encodes count as B (args+1) and C
    /// (results+1).
    fn emitCall(self: *Compiler, c: ast.Expr.Call, dest: u8, num_results: u8) CompileError!void {
        // Lay out the call: [closure, arg1, arg2, ...] in consecutive
        // registers starting at dest. Lua requires the closure and
        // args to be contiguous because CALL expects them that way.
        const reg_before = self.next_reg;
        if (dest >= self.next_reg) self.next_reg = dest + 1;

        // Closure into dest.
        try self.emitExprToDest(c.callee, dest);
        // Args into dest+1, dest+2, ...
        for (c.args) |arg| _ = try self.exprToReg(arg);

        // CALL A B C: A is closure register, B-1 is arg count,
        // C-1 is expected result count.
        const b: u8 = @intCast(c.args.len + 1);
        const c_res: u8 = num_results + 1;
        try self.emit(Instruction.iABC(.call, dest, 0, b, c_res));

        self.next_reg = reg_before;
        if (dest >= self.next_reg) self.next_reg = dest + 1;
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
        // Comparison ops materialize a boolean into `dest` via the
        // Lua idiom: cmp + LFALSESKIP + LOADTRUE.
        if (compareOpCode(b.op)) |cmp| return self.emitCompareToReg(b, cmp, dest);

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

    /// Materialize a comparison into `dest` as a boolean. Pattern:
    ///
    ///   <eval lhs, rhs into temps>
    ///   <CMP> lhs rhs k=0     ; skip next when comparison is TRUE
    ///   LFALSESKIP dest        ; FALSE case: dest=false, skip next
    ///   LOADTRUE dest          ; TRUE case: lands here from cmp's skip
    ///
    /// Trace TRUE case: CMP skips LFALSESKIP, LOADTRUE runs → dest=true.
    /// Trace FALSE case: LFALSESKIP runs → dest=false, then skips
    ///   the LOADTRUE → dest stays false.
    ///
    /// `neq`, `gt`, `gte` ride on EQ/LT/LE: neq inverts via k=1, the
    /// >-style ops swap operands.
    fn emitCompareToReg(self: *Compiler, b: ast.Expr.Binary, form: CompareForm, dest: u8) CompileError!void {
        const reg_before = self.next_reg;
        if (dest >= self.next_reg) self.next_reg = dest + 1;
        const lhs = try self.exprToReg(b.lhs);
        const rhs = try self.exprToReg(b.rhs);

        const left = if (form.swap) rhs else lhs;
        const right = if (form.swap) lhs else rhs;
        const k: u1 = if (form.invert) 1 else 0;

        try self.emit(Instruction.iABC(form.op, 0, k, left, right));
        try self.emit(Instruction.iABC(.lfalseskip, dest, 0, 0, 0));
        try self.emit(Instruction.iABC(.loadtrue, dest, 0, 0, 0));

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

    // Named-function declarations (`function foo.bar(...) end`) need
    // the global table or upvalues to bind the name; phase 3.2.4 will
    // bring them in. Local function decls already work.
    try testing.expectError(error.Unimplemented, compileSrc(arena, "function f() return 1 end"));
}
