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
    /// strict-Pluto: type annotation rejected the value at compile
    /// time (literal RHS doesn't match the declared type).
    TypeAnnotationMismatch,
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
    /// Global access via _ENV: read with GETTABUP A env_upvalue name_const,
    /// write with SETTABUP env_upvalue name_const value.
    global: GlobalAccess,
};

const GlobalAccess = struct {
    env_upvalue: u8,
    name_const: u8,
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
        .concat => .concat,
        .band => .band,
        .bor => .bor,
        .bxor => .bxor,
        .shl => .shl,
        .shr => .shr,
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
    /// Declared return type of the current function. Set on entry to
    /// compileFunctionBody when the AST has an annotation; emitReturn
    /// uses it to enforce `return v` against the declared type.
    return_type: ?ast.TypeExpr,
    /// Stack of break-exit jump lists, one entry per enclosing
    /// breakable construct (`switch`, `while`). Each entry collects
    /// the PCs of placeholder JMPs emitted by `break`; the construct
    /// patches them all to its post-end PC on exit. Empty when no
    /// enclosing breakable, in which case `break` is a compile error.
    break_jumps: std.ArrayList(std.ArrayList(usize)),

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
            .return_type = null,
            .break_jumps = .{ .items = &.{}, .capacity = 0 },
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

    /// Resolve a name to its access form. Tries local, then this
    /// function's upvalues, then walks the parent chain capturing as
    /// needed, then falls back to global access via _ENV. Always
    /// succeeds — Lua treats unbound names as global lookups.
    ///
    /// _ENV is the only name that can't fall back to global (it IS
    /// the global table). It must resolve as local/upvalue or be a
    /// genuine compile error (impossible in normal use since
    /// compileChunk seeds _ENV at upvalue 0 in the top-level proto,
    /// and nested functions chain through).
    fn resolveIdent(self: *Compiler, name: []const u8) CompileError!Resolution {
        if (self.findLocal(name)) |r| return Resolution{ .local = r };
        if (self.findUpvalue(name)) |i| return Resolution{ .upvalue = i };
        if (try self.captureFromParent(name)) |idx| return Resolution{ .upvalue = idx };

        // Global fallback — _ENV must already be available as an
        // upvalue at this point (or capturable up the chain).
        if (std.mem.eql(u8, name, "_ENV")) return error.UnknownIdentifier;
        const env_res = try self.resolveIdent("_ENV");
        const env_idx: u8 = switch (env_res) {
            .upvalue => |i| i,
            else => return error.UnknownIdentifier,
        };
        const k_idx = try self.addConstant(.{ .string = name });
        if (k_idx > 255) return error.TooManyConstants;
        return Resolution{ .global = .{ .env_upvalue = env_idx, .name_const = @intCast(k_idx) } };
    }

    /// Walk parent chain looking for `name` as a local or upvalue.
    /// Each link adds an upvalue desc on the corresponding compiler so
    /// the runtime can chain captures through. Returns this compiler's
    /// upvalue index for `name` if found anywhere up the chain.
    fn captureFromParent(self: *Compiler, name: []const u8) CompileError!?u8 {
        const p = self.parent orelse return null;
        if (p.findLocal(name)) |reg| {
            try self.upvalues.append(self.arena, .{ .name = name, .in_stack = true, .idx = reg });
            return @intCast(self.upvalues.items.len - 1);
        }
        if (p.findUpvalue(name)) |i| {
            try self.upvalues.append(self.arena, .{ .name = name, .in_stack = false, .idx = i });
            return @intCast(self.upvalues.items.len - 1);
        }
        if (try p.captureFromParent(name)) |parent_idx| {
            try self.upvalues.append(self.arena, .{ .name = name, .in_stack = false, .idx = parent_idx });
            return @intCast(self.upvalues.items.len - 1);
        }
        return null;
    }

    /// Compile a top-level chunk into a Proto. Treats the chunk as
    /// the body of a vararg function with no fixed params (Lua's
    /// "main" chunk convention). Seeds `_ENV` as upvalue 0 — the
    /// runtime VM main closure provides the actual cell pointing at
    /// the globals table. Nested function bodies inherit `_ENV`
    /// transparently via the resolveIdent upvalue chain.
    pub fn compileChunk(self: *Compiler, block: *const ast.Block) CompileError!*Proto {
        try self.upvalues.append(self.arena, .{ .name = "_ENV", .in_stack = true, .idx = 0 });
        return self.compileFunctionBody(&.{}, true, null, block);
    }

    /// Compile a function body — params + block — into a Proto. Used
    /// for both the top-level chunk and nested function expressions.
    /// The compiler's local/code/constants/protos state is reset on
    /// entry; nested function compilation uses a *separate* Compiler.
    pub fn compileFunctionBody(
        self: *Compiler,
        params: []const ast.NameWithType,
        is_vararg: bool,
        return_type: ?ast.TypeExpr,
        block: *const ast.Block,
    ) CompileError!*Proto {
        self.num_params = @intCast(params.len);
        self.is_vararg = is_vararg;
        self.return_type = return_type;

        // Bind each formal param as a local in R[0..num_params-1].
        // The VM lays args in those slots when CALL executes.
        for (params) |p| {
            const r = self.allocReg();
            try self.locals.append(self.arena, .{ .name = p.name, .reg = r });
        }

        try self.emit(Instruction.iABC(.varargprep, 0, 0, 0, 0));

        // Emit a TYPECHECK for each typed parameter. Caller-supplied
        // values land in R[0..num_params-1] before the body runs;
        // these instructions enforce the contract.
        for (params, 0..) |p, idx| {
            if (p.type_annot) |annot| {
                if (annot == .atom and annot.atom == .any) continue;
                if (annot != .atom) continue; // optional/union: phase 4.x
                try self.emit(Instruction.iABC(
                    .typecheck,
                    @intCast(idx),
                    0,
                    @intFromEnum(annot.atom),
                    0,
                ));
            }
        }

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
            .switch_stmt => |sw| try self.emitSwitch(sw),
            .break_stmt => try self.emitBreak(),
            .expr_stmt => |e| try self.emitExprStmt(e),
            .local_function => |lf| try self.emitLocalFunction(lf),
            else => return error.Unimplemented,
        }
    }

    /// `break` — exits the innermost enclosing breakable construct
    /// (currently `switch` or `while`). Emits a placeholder JMP and
    /// records its PC on the top break-jump list; the enclosing
    /// emitSwitch / emitWhile patches the offset on exit.
    fn emitBreak(self: *Compiler) CompileError!void {
        if (self.break_jumps.items.len == 0) {
            std.debug.print("strict-pluto: `break` outside of switch / while\n", .{});
            return error.Unimplemented;
        }
        const top = &self.break_jumps.items[self.break_jumps.items.len - 1];
        const pc = self.code.items.len;
        try top.append(self.arena, pc);
        try self.emit(Instruction.iAx(.jmp, 0)); // placeholder, patched later
    }

    /// Function-call statement — call but discard results. CALL with
    /// C=1 means "no results expected". Also handles the method-call
    /// statement form `obj:method(args)`.
    fn emitExprStmt(self: *Compiler, e: *ast.Expr) CompileError!void {
        const reg = self.allocReg();
        switch (e.*) {
            .call => |c| try self.emitCall(c, reg, 0),
            .method_call => |mc| try self.emitMethodCall(mc, reg, 0),
            else => return error.Unimplemented,
        }
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

        // Push a break-jump frame so any `break` in the body lands
        // past the loop. Phase 4.7 will add `continue` similarly.
        try self.break_jumps.append(self.arena, .{ .items = &.{}, .capacity = 0 });

        try self.emitBlock(w.body);

        // Unconditional jump back to loop_start.
        try self.emitJump(loop_start);

        const loop_end = self.code.items.len;
        self.patchJump(jump_to_end, loop_end);

        // Patch every `break` collected in the body to land here.
        var frame = self.break_jumps.pop().?;
        for (frame.items) |jpc| self.patchJump(jpc, loop_end);
        frame.deinit(self.arena);
    }

    /// `switch <expr> case v1[, v2]: ... case ...: ... default: ... end`
    ///
    /// Compiles to a chain of equality tests against the discriminant
    /// (evaluated once into a fresh register). For each case:
    ///
    ///   <eval v_i into tmp>
    ///   EQ D tmp k=1     ; PC++ iff (D == v_i) != 1, i.e. iff NOT
    ///                    ; equal — so the JMP runs iff equal.
    ///   JMP body         ; runs only when D == v_i
    ///   ; (next v_i for the same case)
    ///   JMP next_case    ; falls here when no v_i for this case matched
    /// body:
    ///   <case body>
    ///   JMP after        ; no fallthrough between cases (strict-Pluto)
    /// next_case:
    ///   ...
    /// after_chain:
    ///   <default body, if any>
    /// after:
    ///
    /// `break` inside any case body funnels through the break_jumps
    /// stack to `after`. If the case body already ends with control
    /// flow (return, etc.), the trailing JMP-to-after is dead but
    /// harmless — keeping the codegen uniform is worth a few words of
    /// dead code per terminal case.
    fn emitSwitch(self: *Compiler, sw: ast.Stmt.Switch) CompileError!void {
        // Evaluate the discriminant into a fresh register and reserve
        // it for the lifetime of the chain — case-value temps go
        // above it and get freed each iteration.
        const reg_before = self.next_reg;
        const disc_reg = try self.exprToReg(sw.discriminant);
        const after_disc = self.next_reg;

        // Push the break-jump frame for this switch.
        try self.break_jumps.append(self.arena, .{ .items = &.{}, .capacity = 0 });

        // Each case appends its body-end JMP-to-after into this list.
        var jumps_to_after = std.ArrayList(usize){ .items = &.{}, .capacity = 0 };

        for (sw.cases) |case| {
            // For each value: emit EQ-then-JMP-to-body. Collect the
            // body-jump PCs so we can patch them once the body PC is known.
            var jumps_to_body = std.ArrayList(usize){ .items = &.{}, .capacity = 0 };
            for (case.values) |val_expr| {
                self.next_reg = after_disc;
                const val_reg = try self.exprToReg(val_expr);
                // EQ disc_reg val_reg k=1 — runs next JMP iff equal.
                // (k=0 would invert: JMP runs iff NOT equal — wrong for
                // case-dispatch.)
                try self.emit(Instruction.iABC(.eq, 0, 1, disc_reg, val_reg));
                const j = self.code.items.len;
                try self.emit(Instruction.iAx(.jmp, 0)); // patched to body
                try jumps_to_body.append(self.arena, j);
            }
            self.next_reg = after_disc;

            // None of the values matched — skip this case entirely.
            const skip_case = self.code.items.len;
            try self.emit(Instruction.iAx(.jmp, 0)); // patched to next-case

            // Body lands here. Patch each "JMP body" to this PC.
            const body_pc = self.code.items.len;
            for (jumps_to_body.items) |jpc| self.patchJump(jpc, body_pc);
            try self.emitBlock(case.body);

            // Implicit case end → jump to after-switch (no fallthrough).
            const j_after = self.code.items.len;
            try self.emit(Instruction.iAx(.jmp, 0));
            try jumps_to_after.append(self.arena, j_after);

            // The "no match" skip lands at the start of the next case
            // (or, for the last case, at the default block / after).
            self.patchJump(skip_case, self.code.items.len);
        }

        // Default block (if any) sits where execution falls when no
        // case matched. No skip-jump needed before it — control just
        // arrives here from the last case's skip patch.
        if (sw.default_block) |db| try self.emitBlock(db);

        const after_pc = self.code.items.len;
        for (jumps_to_after.items) |jpc| self.patchJump(jpc, after_pc);

        // Drain break jumps (collected during case bodies) to after_pc.
        var frame = self.break_jumps.pop().?;
        for (frame.items) |jpc| self.patchJump(jpc, after_pc);
        frame.deinit(self.arena);

        // Free the discriminant register.
        self.next_reg = reg_before;
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

    /// `local a: T1, b: T2, ... = e1, e2, ...` — evaluate values into
    /// the next-available registers, then bind names + check types.
    /// Extra names get nil. Type annotations are enforced:
    /// - Literal RHS whose static type doesn't match → compile error
    /// - Non-literal RHS → emit TYPECHECK opcode (runtime assert)
    /// - Missing value (padded with nil) → must allow nil (the type
    ///   either is `any` or, in phase 4.x, optional `T?`)
    fn emitLocal(self: *Compiler, l: ast.Stmt.Local) CompileError!void {
        const base = self.next_reg;
        for (l.values) |val| _ = try self.exprToReg(val);

        // Pad with nil for names without matching values.
        var i: usize = l.values.len;
        while (i < l.names.len) : (i += 1) {
            const r = self.allocReg();
            try self.emit(Instruction.iABC(.loadnil, r, 0, 0, 0));
        }

        // Bind names + emit type checks.
        for (l.names, 0..) |nt, idx| {
            const reg: u8 = @intCast(base + idx);
            try self.locals.append(self.arena, .{ .name = nt.name, .reg = reg });

            // Type enforcement.
            if (nt.type_annot) |annot| {
                const value: ?*const ast.Expr = if (idx < l.values.len) l.values[idx] else null;
                try self.enforceType(annot, value, reg);
            }
        }
    }

    /// Enforce that the value at `reg` matches `annot`. Tries
    /// compile-time check against the literal expression first; if
    /// the value's type can't be determined statically, emits a
    /// runtime TYPECHECK opcode.
    fn enforceType(self: *Compiler, annot: ast.TypeExpr, value: ?*const ast.Expr, reg: u8) CompileError!void {
        const atom = switch (annot) {
            .atom => |a| a,
            .optional => return, // future phase
        };
        if (atom == .any) return; // `any` disables checking

        // Compile-time check: if RHS is a literal whose type we can
        // pin down, verify directly.
        if (value) |v_expr| {
            if (literalType(v_expr.*)) |lit_atom| {
                if (!atomicAccepts(atom, lit_atom)) {
                    std.debug.print(
                        "strict-pluto: type mismatch — expected `{s}`, got literal of type `{s}`\n",
                        .{ atom.name(), lit_atom.name() },
                    );
                    return error.TypeAnnotationMismatch;
                }
                return; // verified statically, no runtime check needed
            }
        } else {
            // No value given (padded nil). Only `any` (handled above)
            // or eventually `T?` should accept this.
            std.debug.print(
                "strict-pluto: type mismatch — expected `{s}`, got nil (no initializer)\n",
                .{atom.name()},
            );
            return error.TypeAnnotationMismatch;
        }

        // Fallback: emit a runtime TYPECHECK against the atom ordinal.
        try self.emit(Instruction.iABC(.typecheck, reg, 0, @intFromEnum(atom), 0));
    }

    /// Single-target assignment. Targets supported: ident (local or
    /// upvalue), field (`obj.name`), index (`obj[key]`). Multi-target
    /// is deferred until we sort out evaluation order semantics.
    fn emitAssign(self: *Compiler, a: ast.Stmt.Assign) CompileError!void {
        if (a.targets.len != 1 or a.values.len != 1) return error.Unimplemented;
        const target = a.targets[0];
        switch (target.*) {
            .ident => |name| {
                const res = try self.resolveIdent(name);
                switch (res) {
                    .local => |r| try self.emitExprToDest(a.values[0], r),
                    .upvalue => |idx| {
                        const reg_before = self.next_reg;
                        const tmp = try self.exprToReg(a.values[0]);
                        try self.emit(Instruction.iABC(.setupval, tmp, 0, idx, 0));
                        self.next_reg = reg_before;
                    },
                    .global => |g| {
                        // SETTABUP A B C: UpValue[A][K[B]:string] := R[C]
                        const reg_before = self.next_reg;
                        const val = try self.exprToReg(a.values[0]);
                        try self.emit(Instruction.iABC(.settabup, g.env_upvalue, 0, g.name_const, val));
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
            // Returning nothing — if the function declares a non-nil
            // return type, that's a static mismatch.
            if (self.return_type) |annot| {
                if (annot == .atom and annot.atom != .any and annot.atom != .nil_) {
                    std.debug.print(
                        "strict-pluto: return type mismatch — declared `{s}`, but `return` (no value) is nil\n",
                        .{annot.atom.name()},
                    );
                    return error.TypeAnnotationMismatch;
                }
            }
            try self.emit(Instruction.iABC(.return0, 0, 0, 0, 0));
            return;
        }
        if (r.values.len == 1) {
            const reg_before = self.next_reg;
            const r1 = try self.exprToReg(r.values[0]);
            // Enforce the function's declared return type, if any.
            if (self.return_type) |annot| {
                try self.enforceType(annot, r.values[0], r1);
            }
            try self.emit(Instruction.iABC(.return1, r1, 0, 0, 0));
            self.next_reg = reg_before;
            return;
        }
        // Multi-value return: phase 4.x adds tuple type annotations.
        // For now, single-return-type annotations on functions with
        // multi-value returns are an error to keep the semantics clean.
        if (self.return_type != null) {
            std.debug.print(
                "strict-pluto: declared a return type but the function returns multiple values — multi-return type annotations are not yet supported\n",
                .{},
            );
            return error.TypeAnnotationMismatch;
        }
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
                const res = try self.resolveIdent(name);
                switch (res) {
                    .local => |r| {
                        if (r != dest) try self.emit(Instruction.iABC(.move, dest, 0, r, 0));
                    },
                    .upvalue => |idx| try self.emit(Instruction.iABC(.getupval, dest, 0, idx, 0)),
                    .global => |g| try self.emit(Instruction.iABC(.gettabup, dest, 0, g.env_upvalue, g.name_const)),
                }
            },
            .binary => |b| try self.emitBinary(b, dest),
            .unary => |u| try self.emitUnary(u, dest),
            .function => |f| try self.emitFunctionExpr(f, dest),
            .call => |c| try self.emitCall(c, dest, 1), // expression context wants 1 result
            .method_call => |mc| try self.emitMethodCall(mc, dest, 1),
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
        const sub_proto = try nested.compileFunctionBody(f.params, f.has_vararg, f.return_type, f.body);
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

    /// `obj:method(args)` — Lua's method-call sugar. Layout is
    ///   dest    = obj.method     (the function to call)
    ///   dest+1  = obj             (passed as the implicit first arg)
    ///   dest+2..= args
    /// Both dest and dest+1 are produced atomically by the SELF
    /// opcode, then the regular CALL handles the rest. `obj` is
    /// evaluated only once even if it's a side-effecting expression.
    fn emitMethodCall(self: *Compiler, mc: ast.Expr.MethodCall, dest: u8, num_results: u8) CompileError!void {
        const reg_before = self.next_reg;
        if (dest >= self.next_reg) self.next_reg = dest + 1;

        // Evaluate the receiver into a temp register above dest. SELF
        // reads from this register to write into both dest and dest+1.
        const recv_reg = try self.exprToReg(mc.receiver);

        const k_idx = try self.addConstant(.{ .string = mc.method });
        if (k_idx > 255) return error.TooManyConstants;

        // Reserve dest, dest+1 for SELF's outputs (method + self).
        // After SELF runs, args land at dest+2, dest+3, ...
        self.next_reg = dest + 2;
        if (self.next_reg > self.max_reg) self.max_reg = self.next_reg;
        try self.emit(Instruction.iABC(.self_, dest, 0, recv_reg, @intCast(k_idx)));

        // Args.
        for (mc.args) |arg| _ = try self.exprToReg(arg);

        // CALL counts include the implicit self arg.
        const b: u8 = @intCast(mc.args.len + 2); // 1 self + N args, +1 for B encoding
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

/// If `e` is a syntactic literal whose runtime type we can pin down
/// at compile time, return the AtomicType. Idents, calls, and
/// binary/unary ops return null (we don't track types through
/// expressions yet — the runtime TYPECHECK opcode covers those).
fn literalType(e: ast.Expr) ?ast.AtomicType {
    return switch (e) {
        .nil => .nil_,
        .boolean => .boolean,
        .integer => .integer,
        .number => .number,
        .string => .string,
        .table => .table,
        .function => .function,
        else => null,
    };
}

/// Does annotation `expected` accept a value with concrete type
/// `actual`? Numeric tower: `number` accepts both `integer` and
/// `number`. Otherwise exact match.
fn atomicAccepts(expected: ast.AtomicType, actual: ast.AtomicType) bool {
    if (expected == actual) return true;
    if (expected == .number and actual == .integer) return true;
    return false;
}

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
