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
    /// strict-Pluto: TYPECHECK opcode failed — value's runtime type
    /// doesn't match the declared annotation.
    TypeAssertionFailed,
    OutOfMemory,
};

pub const RunResult = struct {
    values: []TValue,
};

/// One stack frame on the call stack. `base` is the index into the
/// VM's shared register pool where this frame's R[0] lives, so all
/// register indexing in the dispatch loop is `base + r`.
const Frame = struct {
    proto: *const bc.Proto,
    pc: usize,
    base: usize,
    /// On return, results land at this absolute register in the
    /// caller's frame; the call-site emitter uses this to read them.
    results_at: usize,
    /// How many results the caller wants (Lua's `C-1` from CALL).
    results_wanted: u8,
    /// The closure currently executing this frame. null only for the
    /// top-level chunk (which has no upvalues to read from anyway).
    /// GETUPVAL / SETUPVAL go through closure.upvalues[idx].
    closure: ?*v.Closure,
};

const REGISTER_POOL_SIZE: usize = 8 * 1024;

pub const VM = struct {
    allocator: std.mem.Allocator,
    registers: []TValue,
    frames: std.ArrayList(Frame),
    open_upvalues: std.ArrayList(*v.UpvalueCell),
    /// Globals table — _ENV at runtime. Holds built-ins (print, etc.)
    /// at init time; user globals (`x = 5` at top level) write into
    /// it via SETTABUP.
    globals: *v.Table,
    /// Backing storage for the main closure's _ENV upvalue cell. Held
    /// inline so we don't need to allocate it from the allocator
    /// (and so the cell stays valid for the lifetime of the VM).
    env_storage: TValue,
    env_cell: v.UpvalueCell,
    /// Stdout buffer for `print`, `io.write`, etc. The demo and
    /// tests read this after run() completes.
    output: std.ArrayList(u8),
    /// Interned `__index` string used by tableLookup's metatable
    /// fallback. Allocated once at VM init so the metatable read
    /// path is just a hash + slot probe.
    index_metakey: TValue,

    pub fn init(allocator: std.mem.Allocator, proto: *const bc.Proto) !VM {
        const regs = try allocator.alloc(TValue, REGISTER_POOL_SIZE);
        @memset(regs, TValue.NIL);

        // Build the globals table and register built-ins.
        const globals = try v.Table.createWithAllocator(allocator);
        try registerBuiltins(globals, allocator);

        // Wrap the main proto in a closure that has the globals table
        // as upvalue 0 (_ENV). The cell is closed from the start —
        // its value lives in the VM's env_storage field so the
        // pointer is stable for the VM's lifetime.
        const main_closure = try allocator.create(v.Closure);
        const upvals = try allocator.alloc(*v.UpvalueCell, 1);

        const index_key_str = try v.String.createWithAllocator(allocator, "__index");

        var vm: VM = .{
            .allocator = allocator,
            .registers = regs,
            .frames = .{ .items = &.{}, .capacity = 0 },
            .open_upvalues = .{ .items = &.{}, .capacity = 0 },
            .globals = globals,
            .env_storage = TValue.fromTable(globals),
            .env_cell = undefined,
            .output = .{ .items = &.{}, .capacity = 0 },
            .index_metakey = TValue.fromString(index_key_str),
        };
        // Now that the VM is in its final memory location, point the
        // cell at the env_storage field. (Building this earlier and
        // moving the VM by-value would invalidate the pointer.)
        // Caller takes ownership; the returned struct is what they hold.
        // To make this safe, return a heap-allocated VM... but we use
        // the existing convention of returning a value. Instead, let
        // the caller call vm.bindEnv() right after init to wire it.
        try vm.frames.append(allocator, .{
            .proto = proto,
            .pc = 0,
            .base = 0,
            .results_at = 0,
            .results_wanted = 0,
            .closure = main_closure,
        });
        main_closure.* = .{ .proto = proto, .upvalues = upvals };
        // env_cell.value is wired in bindEnv() after the VM is at its
        // final address. upvals[0] = &vm.env_cell — same caveat.
        return vm;
    }

    /// Wire the _ENV cell to point at this VM's env_storage. Must be
    /// called after `init` once the VM is at its final memory address
    /// (typically right after assigning the result of `init` to a
    /// stack variable). Pointers into `vm.env_storage` and
    /// `&vm.env_cell` need stable addresses.
    pub fn bindEnv(self: *VM) void {
        self.env_cell = .{ .value = &self.env_storage };
        self.frames.items[0].closure.?.upvalues[0] = &self.env_cell;
    }

    pub fn deinit(self: *VM) void {
        self.allocator.free(self.registers);
        self.frames.deinit(self.allocator);
        self.open_upvalues.deinit(self.allocator);
        self.output.deinit(self.allocator);
    }

    fn currentFrame(self: *VM) *Frame {
        return &self.frames.items[self.frames.items.len - 1];
    }

    fn currentProto(self: *VM) *const bc.Proto {
        return self.currentFrame().proto;
    }

    fn reg(self: *VM, r: u8) *TValue {
        return &self.registers[self.currentFrame().base + r];
    }

    /// Read `t[key]`, walking the `__index` metatable chain on misses.
    ///
    /// Lua's read-fallback rule: if the raw lookup misses *and* `t`
    /// has a metatable *and* `mt.__index` is a table, retry the lookup
    /// against that table (recursively). The chain is bounded — both
    /// to avoid infinite loops on bad metatable setups and because
    /// real-world chains are shallow (class → parent → root).
    ///
    /// Function-form `__index(t, key)` is deferred to phase 4.4b/c
    /// (needs to issue a callback into the VM mid-dispatch). Today
    /// only table-form `__index` is honored.
    fn tableLookup(self: *VM, t: *v.Table, key: TValue) TValue {
        var cur: *v.Table = t;
        var hops: u32 = 0;
        while (hops < 64) : (hops += 1) {
            const raw = cur.get(key);
            if (raw != .nil) return raw;
            const mt = cur.metatable orelse return TValue.NIL;
            const idx = mt.get(self.index_metakey);
            switch (idx) {
                .table => |next| cur = next,
                else => return TValue.NIL, // missing or function-form (4.4b)
            }
        }
        return TValue.NIL; // chain too deep — bail
    }

    /// Run until the top-level frame returns. The returned slice is
    /// owned by the VM caller (allocated by `allocator`).
    pub fn run(self: *VM) RuntimeError!RunResult {
        while (true) {
            const frame = self.currentFrame();
            if (frame.pc >= frame.proto.code.len) {
                // Falling off the end is an implicit `return0`.
                if (try self.handleReturn(0, 0)) |result| return result;
                continue;
            }
            const instr = frame.proto.code[frame.pc];
            frame.pc += 1;

            switch (instr.opcode()) {
                .varargprep => {}, // No varargs handled yet; trivial no-op.

                .move => self.reg(instr.a).* = self.reg(instr.b).*,

                .loadi => self.reg(instr.a).* = TValue.fromInt(@intCast(instr.unpackSBx())),
                .loadf => self.reg(instr.a).* = TValue.fromFloat(@floatFromInt(instr.unpackSBx())),
                .loadk => {
                    const k = self.currentProto().constants[instr.unpackBx()];
                    self.reg(instr.a).* = try self.constToValue(k);
                },
                .loadnil => self.reg(instr.a).* = TValue.NIL,
                .loadtrue => self.reg(instr.a).* = TValue.TRUE,
                .loadfalse => self.reg(instr.a).* = TValue.FALSE,

                .add => self.reg(instr.a).* = try arith(.add, self.reg(instr.b).*, self.reg(instr.c).*),
                .sub => self.reg(instr.a).* = try arith(.sub, self.reg(instr.b).*, self.reg(instr.c).*),
                .mul => self.reg(instr.a).* = try arith(.mul, self.reg(instr.b).*, self.reg(instr.c).*),
                .div => self.reg(instr.a).* = try arith(.div, self.reg(instr.b).*, self.reg(instr.c).*),
                .idiv => self.reg(instr.a).* = try arith(.idiv, self.reg(instr.b).*, self.reg(instr.c).*),
                .mod => self.reg(instr.a).* = try arith(.mod, self.reg(instr.b).*, self.reg(instr.c).*),
                .pow => self.reg(instr.a).* = try arith(.pow, self.reg(instr.b).*, self.reg(instr.c).*),

                // String concatenation: R[A] = R[B] .. R[C].
                // Lua's spec coerces numbers to strings; we follow.
                .concat => {
                    const left = self.reg(instr.b).*;
                    const right = self.reg(instr.c).*;
                    var buf: std.ArrayList(u8) = .{ .items = &.{}, .capacity = 0 };
                    formatValueTo(&buf, self.allocator, left) catch return error.OutOfMemory;
                    formatValueTo(&buf, self.allocator, right) catch return error.OutOfMemory;
                    const owned = buf.toOwnedSlice(self.allocator) catch return error.OutOfMemory;
                    const s = v.String.createWithAllocator(self.allocator, owned) catch return error.OutOfMemory;
                    self.allocator.free(owned);
                    self.reg(instr.a).* = TValue.fromString(s);
                },

                // Bitwise: integer-only.
                .band => self.reg(instr.a).* = try bitwise(.band, self.reg(instr.b).*, self.reg(instr.c).*),
                .bor => self.reg(instr.a).* = try bitwise(.bor, self.reg(instr.b).*, self.reg(instr.c).*),
                .bxor => self.reg(instr.a).* = try bitwise(.bxor, self.reg(instr.b).*, self.reg(instr.c).*),
                .shl => self.reg(instr.a).* = try bitwise(.shl, self.reg(instr.b).*, self.reg(instr.c).*),
                .shr => self.reg(instr.a).* = try bitwise(.shr, self.reg(instr.b).*, self.reg(instr.c).*),

                .unm => self.reg(instr.a).* = try unary(.neg, self.reg(instr.b).*),
                .bnot => self.reg(instr.a).* = try unary(.bnot, self.reg(instr.b).*),
                .not_ => self.reg(instr.a).* = unaryNot(self.reg(instr.b).*),
                .len => self.reg(instr.a).* = try unaryLen(self.reg(instr.b).*),

                .eq => {
                    const cond = self.reg(instr.b).eql(self.reg(instr.c).*);
                    if (@intFromBool(cond) != instr.k) frame.pc += 1;
                },
                .lt => {
                    const cond = try compareLess(self.reg(instr.b).*, self.reg(instr.c).*);
                    if (@intFromBool(cond) != instr.k) frame.pc += 1;
                },
                .le => {
                    const cond = try compareLessEq(self.reg(instr.b).*, self.reg(instr.c).*);
                    if (@intFromBool(cond) != instr.k) frame.pc += 1;
                },

                .test_ => {
                    const cond = self.reg(instr.a).isTruthy();
                    if (@intFromBool(cond) != instr.k) frame.pc += 1;
                },

                .lfalseskip => {
                    self.reg(instr.a).* = TValue.FALSE;
                    frame.pc += 1;
                },

                .jmp => {
                    const offset = instr.unpackSJ();
                    frame.pc = @intCast(@as(i64, @intCast(frame.pc)) + offset);
                },

                // CLOSURE A Bx: build a closure from sub-proto Bx,
                // wiring its upvalues per the sub-proto's descriptor
                // table. in_stack=true descriptors create or reuse an
                // open cell pointing at the current frame's register.
                // in_stack=false descriptors share the current
                // closure's upvalue cell (chain through).
                .closure => try self.handleClosure(instr.a, instr.unpackBx()),

                // GETUPVAL A B: R[A] = upvalue[B].value.*
                .getupval => {
                    const cl = self.currentFrame().closure orelse return error.UnknownOpcode;
                    self.reg(instr.a).* = cl.upvalues[instr.b].value.*;
                },

                // SETUPVAL A B: upvalue[B].value.* = R[A]
                .setupval => {
                    const cl = self.currentFrame().closure orelse return error.UnknownOpcode;
                    cl.upvalues[instr.b].value.* = self.reg(instr.a).*;
                },

                // GETTABUP A B C: R[A] = upvalue[B].value.*[K[C]:string]
                // Used for global reads (B == _ENV upvalue, C == name).
                .gettabup => {
                    const cl = self.currentFrame().closure orelse return error.UnknownOpcode;
                    const env = cl.upvalues[instr.b].value.*;
                    if (env != .table) return error.InvalidArithmeticOperand;
                    const k = self.currentProto().constants[instr.c];
                    if (k != .string) return error.InvalidArithmeticOperand;
                    const key = try self.constToValue(k);
                    self.reg(instr.a).* = env.table.get(key);
                },

                // SETTABUP A B C: upvalue[A].value.*[K[B]:string] = R[C]
                .settabup => {
                    const cl = self.currentFrame().closure orelse return error.UnknownOpcode;
                    const env = cl.upvalues[instr.a].value.*;
                    if (env != .table) return error.InvalidArithmeticOperand;
                    const k = self.currentProto().constants[instr.b];
                    if (k != .string) return error.InvalidArithmeticOperand;
                    const key = try self.constToValue(k);
                    const val = self.reg(instr.c).*;
                    env.table.setWithAllocator(self.allocator, key, val) catch
                        return error.OutOfMemory;
                },

                // NEWTABLE A: R[A] = {}.
                // (Lua's NEWTABLE encodes size hints in B/C; we ignore
                // them — the table grows as needed.)
                .newtable => {
                    const t = v.Table.createWithAllocator(self.allocator) catch return error.OutOfMemory;
                    self.reg(instr.a).* = TValue.fromTable(t);
                },

                // GETTABLE A B C: R[A] = R[B][R[C]].
                // Walks the __index chain on miss (phase 4.4a).
                .gettable => {
                    const obj = self.reg(instr.b).*;
                    if (obj != .table) return error.InvalidArithmeticOperand;
                    const key = self.reg(instr.c).*;
                    self.reg(instr.a).* = self.tableLookup(obj.table, key);
                },

                // GETFIELD A B C: R[A] = R[B][K[C]:string].
                // Walks the __index chain on miss (phase 4.4a).
                .getfield => {
                    const obj = self.reg(instr.b).*;
                    if (obj != .table) return error.InvalidArithmeticOperand;
                    const k = self.currentProto().constants[instr.c];
                    if (k != .string) return error.InvalidArithmeticOperand;
                    const key = try self.constToValue(k);
                    self.reg(instr.a).* = self.tableLookup(obj.table, key);
                },

                // SETTABLE A B C: R[A][R[B]] = R[C].
                .settable => {
                    const obj = self.reg(instr.a).*;
                    if (obj != .table) return error.InvalidArithmeticOperand;
                    const key = self.reg(instr.b).*;
                    const val = self.reg(instr.c).*;
                    obj.table.setWithAllocator(self.allocator, key, val) catch
                        return error.OutOfMemory;
                },

                // SETFIELD A B C: R[A][K[B]:string] = R[C].
                .setfield => {
                    const obj = self.reg(instr.a).*;
                    if (obj != .table) return error.InvalidArithmeticOperand;
                    const k = self.currentProto().constants[instr.b];
                    if (k != .string) return error.InvalidArithmeticOperand;
                    const key = try self.constToValue(k);
                    const val = self.reg(instr.c).*;
                    obj.table.setWithAllocator(self.allocator, key, val) catch
                        return error.OutOfMemory;
                },

                // SETLIST A B C: R[A][1..B] = R[A+1..A+B].
                // Bulk-fill the array part of a fresh table from the
                // contiguous registers above A. Used by table
                // constructors with positional fields.
                .setlist => {
                    const obj = self.reg(instr.a).*;
                    if (obj != .table) return error.InvalidArithmeticOperand;
                    var i: u8 = 1;
                    while (i <= instr.b) : (i += 1) {
                        const val = self.reg(instr.a + i).*;
                        obj.table.setWithAllocator(
                            self.allocator,
                            TValue.fromInt(i),
                            val,
                        ) catch return error.OutOfMemory;
                    }
                },

                // SELF A B C: method-call sugar.
                //   R[A+1] = R[B]                       (self argument)
                //   R[A]   = R[B][K[C]:string]          (the method)
                // Used by `obj:method(args)` so the receiver and method
                // land contiguously for a subsequent CALL. Honors the
                // __index chain on the lookup (so inherited methods on
                // class-style tables resolve correctly).
                .self_ => {
                    const obj = self.reg(instr.b).*;
                    if (obj != .table) return error.InvalidArithmeticOperand;
                    const k = self.currentProto().constants[instr.c];
                    if (k != .string) return error.InvalidArithmeticOperand;
                    const key = try self.constToValue(k);
                    // Order matters: write self BEFORE the method, in
                    // case A+1 == B (self IS the method's receiver).
                    self.reg(instr.a + 1).* = obj;
                    self.reg(instr.a).* = self.tableLookup(obj.table, key);
                },

                // CALL A B C: closure at R[A], B-1 args, C-1 expected
                // results. Push a new frame and dispatch into it.
                .call => try self.handleCall(instr.a, instr.b, instr.c),

                // TYPECHECK A B: assert runtime type of R[A] matches
                // the AtomicType ordinal B; raise on mismatch.
                .typecheck => {
                    const ast_mod = @import("ast.zig");
                    const expected: ast_mod.AtomicType = @enumFromInt(instr.b);
                    const actual = atomicTypeOf(self.reg(instr.a).*);
                    const ok = expected == actual or
                        (expected == .number and actual == .integer);
                    if (!ok) {
                        std.debug.print(
                            "strict-pluto runtime: type assertion failed — expected `{s}`, got `{s}`\n",
                            .{ expected.name(), actual.name() },
                        );
                        return error.TypeAssertionFailed;
                    }
                },

                .return0 => if (try self.handleReturn(0, 0)) |r| return r,
                .return1 => if (try self.handleReturn(instr.a, 1)) |r| return r,
                .return_ => {
                    if (instr.b == 0) return error.NotImplemented;
                    if (try self.handleReturn(instr.a, instr.b - 1)) |r| return r;
                },

                else => return error.UnknownOpcode,
            }
        }
    }

    /// Process a CALL: validate the closure, set up a new frame, set
    /// the dispatch loop pointing at it. Args (already laid out by
    /// the caller in registers R[A+1]..R[A+B-1] of the current frame)
    /// become R[1]..R[B-1] of the callee — Lua's convention is that
    /// callee's R[0] is the first arg, which corresponds to caller's
    /// R[A+1]. So callee's frame.base = caller_base + A + 1.
    fn handleCall(self: *VM, a: u8, b: u8, c: u8) RuntimeError!void {
        const callee = self.reg(a).*;

        // Native function: invoke directly, no frame needed.
        if (callee == .native) {
            const cur_frame = self.currentFrame();
            const arg_count: usize = if (b == 0) 0 else b - 1;
            const args_start = cur_frame.base + a + 1;
            const args = self.registers[args_start .. args_start + arg_count];

            const results = callee.native.func(@ptrCast(self), args) catch
                return error.InvalidArithmeticOperand;
            defer self.allocator.free(results);

            const results_at = cur_frame.base + a;
            const wanted: usize = if (c == 0) 0 else c - 1;
            var i: usize = 0;
            while (i < wanted) : (i += 1) {
                self.registers[results_at + i] = if (i < results.len) results[i] else TValue.NIL;
            }
            return;
        }

        if (callee != .closure) return error.InvalidArithmeticOperand;
        const cl = callee.closure;

        const cur = self.currentFrame();
        const callee_base = cur.base + a + 1;
        // results_at: where in the caller's pool the results should
        // land. Lua puts results at R[A]..R[A+C-2] in the caller, so
        // absolute = caller_base + A.
        const results_at = cur.base + a;
        const results_wanted: u8 = if (c == 0) 0 else c - 1;

        try self.frames.append(self.allocator, .{
            .proto = cl.proto,
            .pc = 0,
            .base = callee_base,
            .results_at = results_at,
            .results_wanted = results_wanted,
            .closure = cl,
        });

        // Pad missing args with nil so the callee always sees its
        // declared param count.
        const provided_args: u8 = if (b == 0) 0 else b - 1;
        if (provided_args < cl.proto.num_params) {
            var i: usize = provided_args;
            while (i < cl.proto.num_params) : (i += 1) {
                self.registers[callee_base + i] = TValue.NIL;
            }
        }
    }

    /// CLOSURE handler — build a closure for sub-proto `bx` of the
    /// current proto and place it in R[A]. Walks the sub-proto's
    /// upvalue descriptors and wires each cell appropriately.
    fn handleClosure(self: *VM, a: u8, bx: u17) RuntimeError!void {
        const cur = self.currentFrame();
        const sub = cur.proto.protos[bx];

        const cells = try self.allocator.alloc(*v.UpvalueCell, sub.upvalues.len);
        for (sub.upvalues, 0..) |desc, i| {
            if (desc.in_stack) {
                // Open upvalue capturing from this frame's register.
                const slot_idx = cur.base + desc.idx;
                cells[i] = try self.findOrCreateOpenUpvalue(slot_idx);
            } else {
                // Inherit from the current closure's upvalues. The
                // top-level chunk has no closure, so this branch
                // can't fire there — that's a compiler invariant.
                const parent_cl = cur.closure orelse return error.UnknownOpcode;
                cells[i] = parent_cl.upvalues[desc.idx];
            }
        }

        const cl = try self.allocator.create(v.Closure);
        cl.* = .{ .proto = sub, .upvalues = cells };
        self.reg(a).* = TValue.fromClosure(cl);
    }

    /// Find an existing open upvalue cell pointing at register slot
    /// `slot_idx`, or allocate a new one. Multiple closures capturing
    /// the same local share the same cell so writes are visible.
    fn findOrCreateOpenUpvalue(self: *VM, slot_idx: usize) RuntimeError!*v.UpvalueCell {
        for (self.open_upvalues.items) |cell| {
            // An open cell points into self.registers; check if it's
            // pointing at slot_idx.
            const offset = (@intFromPtr(cell.value) - @intFromPtr(self.registers.ptr)) / @sizeOf(TValue);
            if (offset == slot_idx) return cell;
        }
        const cell = try self.allocator.create(v.UpvalueCell);
        cell.* = .{ .value = &self.registers[slot_idx] };
        try self.open_upvalues.append(self.allocator, cell);
        return cell;
    }

    /// Close any open upvalue cells whose target slot is at or above
    /// `from_slot`. Called when a frame pops — its register slots
    /// are about to become stale, so any cells pointing there get
    /// detached and given their own backing storage.
    fn closeUpvaluesFrom(self: *VM, from_slot: usize) void {
        var i: usize = 0;
        while (i < self.open_upvalues.items.len) {
            const cell = self.open_upvalues.items[i];
            const offset = (@intFromPtr(cell.value) - @intFromPtr(self.registers.ptr)) / @sizeOf(TValue);
            if (offset >= from_slot) {
                cell.storage = cell.value.*;
                cell.value = &cell.storage;
                _ = self.open_upvalues.swapRemove(i);
                continue;
            }
            i += 1;
        }
    }

    /// Process a RETURN: copy results into the caller's results-at
    /// slot, pop the frame. If the popped frame was top-level,
    /// returns the result for `run()` to surface; otherwise returns
    /// null and the dispatch loop continues in the caller.
    fn handleReturn(self: *VM, base: u8, count: usize) RuntimeError!?RunResult {
        const cur = self.currentFrame();

        // Snapshot return values into a small fixed buffer (we only
        // support up to 16 multi-returns for now; matches Lua's
        // typical use). Larger requires either a heap alloc or
        // copying directly while accounting for register aliasing.
        var buf: [16]TValue = undefined;
        if (count > buf.len) return error.NotImplemented;
        var i: usize = 0;
        while (i < count) : (i += 1) buf[i] = self.registers[cur.base + base + i];

        // Capture caller-side info before popping.
        const results_at = cur.results_at;
        const results_wanted = cur.results_wanted;
        const was_top_level = self.frames.items.len == 1;
        const popping_base = cur.base;

        // Before popping, close any upvalues pointing into this
        // frame's registers. After this, the cells own their values
        // independent of the (about-to-be-stale) stack slots.
        self.closeUpvaluesFrom(popping_base);

        _ = self.frames.pop();

        if (was_top_level) {
            // Surface results to run()'s caller.
            const out = try self.allocator.alloc(TValue, count);
            i = 0;
            while (i < count) : (i += 1) out[i] = buf[i];
            return .{ .values = out };
        }

        // Place results at the caller's expected slot. results_wanted
        // is C-1 from the original CALL; truncate or nil-pad.
        i = 0;
        while (i < results_wanted) : (i += 1) {
            self.registers[results_at + i] = if (i < count) buf[i] else TValue.NIL;
        }

        return null;
    }

    fn constToValue(self: *VM, k: bc.Constant) RuntimeError!TValue {
        return switch (k) {
            .nil => TValue.NIL,
            .boolean => |b| TValue.fromBool(b),
            .integer => |n| TValue.fromInt(n),
            .number => |f| TValue.fromFloat(f),
            .string => |bytes| TValue.fromString(
                v.String.createWithAllocator(self.allocator, bytes) catch return error.OutOfMemory,
            ),
        };
    }
};

// `constToValue` is a method now (needs `self.allocator` to materialize
// runtime String objects from string constants).

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

const BitwiseOp = enum { band, bor, bxor, shl, shr };

fn bitwise(op: BitwiseOp, a: TValue, b: TValue) RuntimeError!TValue {
    if (a != .integer or b != .integer) return error.InvalidArithmeticOperand;
    const av = a.integer;
    const bv = b.integer;
    return switch (op) {
        .band => TValue.fromInt(av & bv),
        .bor => TValue.fromInt(av | bv),
        .bxor => TValue.fromInt(av ^ bv),
        .shl => TValue.fromInt(if (bv >= 64 or bv < 0) 0 else av << @intCast(bv)),
        .shr => TValue.fromInt(if (bv >= 64 or bv < 0) 0 else av >> @intCast(bv)),
    };
}

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

/// Map a runtime TValue to its AtomicType for type-check dispatch.
fn atomicTypeOf(t: TValue) @import("ast.zig").AtomicType {
    return switch (t) {
        .nil => .nil_,
        .boolean => .boolean,
        .integer => .integer,
        .number => .number,
        .string => .string,
        .table => .table,
        .closure, .native => .function,
    };
}

fn unaryLen(x: TValue) RuntimeError!TValue {
    return switch (x) {
        .string => |s| TValue.fromInt(@intCast(s.len)),
        .table => |t| TValue.fromInt(@intCast(t.len())),
        else => error.InvalidUnaryOperand,
    };
}

// =============================================================================
// Ordered comparison (Lua's <, <=)
// =============================================================================

fn compareLess(a: TValue, b: TValue) RuntimeError!bool {
    return switch (a) {
        .integer => |av| switch (b) {
            .integer => |bv| av < bv,
            .number => |bv| @as(f64, @floatFromInt(av)) < bv,
            else => error.InvalidArithmeticOperand,
        },
        .number => |av| switch (b) {
            .integer => |bv| av < @as(f64, @floatFromInt(bv)),
            .number => |bv| av < bv,
            else => error.InvalidArithmeticOperand,
        },
        .string => |av| switch (b) {
            .string => |bv| std.mem.lessThan(u8, av.slice(), bv.slice()),
            else => error.InvalidArithmeticOperand,
        },
        else => error.InvalidArithmeticOperand,
    };
}

fn compareLessEq(a: TValue, b: TValue) RuntimeError!bool {
    return switch (a) {
        .integer => |av| switch (b) {
            .integer => |bv| av <= bv,
            .number => |bv| @as(f64, @floatFromInt(av)) <= bv,
            else => error.InvalidArithmeticOperand,
        },
        .number => |av| switch (b) {
            .integer => |bv| av <= @as(f64, @floatFromInt(bv)),
            .number => |bv| av <= bv,
            else => error.InvalidArithmeticOperand,
        },
        .string => |av| switch (b) {
            .string => |bv| !std.mem.lessThan(u8, bv.slice(), av.slice()),
            else => error.InvalidArithmeticOperand,
        },
        else => error.InvalidArithmeticOperand,
    };
}

// =============================================================================
// Built-in functions (the basic stdlib)
// =============================================================================
//
// Each builtin is a NativeFn the VM exposes via the globals table.
// They take `*anyopaque` (cast back to *VM) plus the args slice, and
// return a slice of result values allocated from the VM's allocator.
// The VM frees the slice after copying values into result registers.

fn registerBuiltins(globals: *v.Table, allocator: std.mem.Allocator) !void {
    const builtins = [_]struct { name: []const u8, fn_ptr: *const v.NativeFn }{
        .{ .name = "print", .fn_ptr = &builtin_print },
        .{ .name = "tostring", .fn_ptr = &builtin_tostring },
        .{ .name = "type", .fn_ptr = &builtin_type },
        .{ .name = "tonumber", .fn_ptr = &builtin_tonumber },
        .{ .name = "ipairs", .fn_ptr = &builtin_ipairs },
        .{ .name = "setmetatable", .fn_ptr = &builtin_setmetatable },
        .{ .name = "getmetatable", .fn_ptr = &builtin_getmetatable },
    };
    for (builtins) |b| {
        const key_str = try v.String.createWithAllocator(allocator, b.name);
        try globals.setWithAllocator(allocator, TValue.fromString(key_str), TValue.fromNative(b.fn_ptr));
    }
}

const builtin_print: v.NativeFn = .{ .name = "print", .func = lua_print };
const builtin_tostring: v.NativeFn = .{ .name = "tostring", .func = lua_tostring };
const builtin_type: v.NativeFn = .{ .name = "type", .func = lua_type };
const builtin_tonumber: v.NativeFn = .{ .name = "tonumber", .func = lua_tonumber };
const builtin_ipairs: v.NativeFn = .{ .name = "ipairs", .func = lua_ipairs };
const builtin_setmetatable: v.NativeFn = .{ .name = "setmetatable", .func = lua_setmetatable };
const builtin_getmetatable: v.NativeFn = .{ .name = "getmetatable", .func = lua_getmetatable };

fn lua_print(vm_ptr: *anyopaque, args: []const TValue) anyerror![]TValue {
    const vm: *VM = @ptrCast(@alignCast(vm_ptr));
    var i: usize = 0;
    while (i < args.len) : (i += 1) {
        if (i > 0) try vm.output.append(vm.allocator, '\t');
        try formatValueTo(&vm.output, vm.allocator, args[i]);
    }
    try vm.output.append(vm.allocator, '\n');
    return try vm.allocator.alloc(TValue, 0);
}

fn lua_tostring(vm_ptr: *anyopaque, args: []const TValue) anyerror![]TValue {
    const vm: *VM = @ptrCast(@alignCast(vm_ptr));
    var buf: std.ArrayList(u8) = .{ .items = &.{}, .capacity = 0 };
    if (args.len > 0) try formatValueTo(&buf, vm.allocator, args[0]);
    const owned = try buf.toOwnedSlice(vm.allocator);
    const s = try v.String.createWithAllocator(vm.allocator, owned);
    vm.allocator.free(owned);
    const out = try vm.allocator.alloc(TValue, 1);
    out[0] = TValue.fromString(s);
    return out;
}

fn lua_type(vm_ptr: *anyopaque, args: []const TValue) anyerror![]TValue {
    const vm: *VM = @ptrCast(@alignCast(vm_ptr));
    const name: []const u8 = if (args.len == 0) "nil" else switch (args[0]) {
        .nil => "nil",
        .boolean => "boolean",
        .integer, .number => "number",
        .string => "string",
        .table => "table",
        .closure, .native => "function",
    };
    const s = try v.String.createWithAllocator(vm.allocator, name);
    const out = try vm.allocator.alloc(TValue, 1);
    out[0] = TValue.fromString(s);
    return out;
}

fn lua_tonumber(vm_ptr: *anyopaque, args: []const TValue) anyerror![]TValue {
    const vm: *VM = @ptrCast(@alignCast(vm_ptr));
    const out = try vm.allocator.alloc(TValue, 1);
    if (args.len == 0) {
        out[0] = TValue.NIL;
        return out;
    }
    out[0] = switch (args[0]) {
        .integer, .number => args[0],
        .string => |s| blk: {
            const text = s.slice();
            if (std.fmt.parseInt(i64, text, 10)) |n| {
                break :blk TValue.fromInt(n);
            } else |_| {}
            if (std.fmt.parseFloat(f64, text)) |f| {
                break :blk TValue.fromFloat(f);
            } else |_| {}
            break :blk TValue.NIL;
        },
        else => TValue.NIL,
    };
    return out;
}

/// `setmetatable(t, m)` — set t's metatable to m (or clear with nil),
/// return t. Mismatches the Lua signature minimally: we don't enforce
/// the `__metatable` protection field (mainline Lua refuses if it's
/// set). Callers in strict-Pluto programs are typically the class
/// runtime, which doesn't set that field.
fn lua_setmetatable(vm_ptr: *anyopaque, args: []const TValue) anyerror![]TValue {
    const vm: *VM = @ptrCast(@alignCast(vm_ptr));
    if (args.len < 1 or args[0] != .table) return error.InvalidArithmeticOperand;
    const t = args[0].table;
    if (args.len >= 2) switch (args[1]) {
        .nil => t.metatable = null,
        .table => |m| t.metatable = m,
        else => return error.InvalidArithmeticOperand,
    };
    const out = try vm.allocator.alloc(TValue, 1);
    out[0] = args[0];
    return out;
}

/// `getmetatable(t)` — return t's metatable or nil.
fn lua_getmetatable(vm_ptr: *anyopaque, args: []const TValue) anyerror![]TValue {
    const vm: *VM = @ptrCast(@alignCast(vm_ptr));
    const out = try vm.allocator.alloc(TValue, 1);
    out[0] = if (args.len >= 1 and args[0] == .table)
        if (args[0].table.metatable) |m| TValue.fromTable(m) else TValue.NIL
    else
        TValue.NIL;
    return out;
}

/// Phase 3.5 stub: ipairs is supposed to return an iterator triple
/// (iterator function, table, control). Real iteration support
/// requires generic-for codegen (deferred). For now we just make the
/// symbol resolvable so demo programs don't crash on `ipairs(t)` —
/// returning nil triple disables iteration cleanly.
fn lua_ipairs(vm_ptr: *anyopaque, args: []const TValue) anyerror![]TValue {
    const vm: *VM = @ptrCast(@alignCast(vm_ptr));
    _ = args;
    const out = try vm.allocator.alloc(TValue, 3);
    out[0] = TValue.NIL;
    out[1] = TValue.NIL;
    out[2] = TValue.NIL;
    return out;
}

/// Format a TValue's print-form into `buf`. Used by print, tostring,
/// and the demo's printValue. Mirrors Lua's tostring rules.
fn formatValueTo(buf: *std.ArrayList(u8), allocator: std.mem.Allocator, val: TValue) !void {
    var stack_buf: [64]u8 = undefined;
    switch (val) {
        .nil => try buf.appendSlice(allocator, "nil"),
        .boolean => |b| try buf.appendSlice(allocator, if (b) "true" else "false"),
        .integer => |i| try buf.appendSlice(allocator, try std.fmt.bufPrint(&stack_buf, "{d}", .{i})),
        .number => |f| try buf.appendSlice(allocator, try std.fmt.bufPrint(&stack_buf, "{d}", .{f})),
        .string => |s| try buf.appendSlice(allocator, s.slice()),
        .table => |t| try buf.appendSlice(allocator, try std.fmt.bufPrint(&stack_buf, "table: 0x{x}", .{@intFromPtr(t)})),
        .closure => |c| try buf.appendSlice(allocator, try std.fmt.bufPrint(&stack_buf, "function: 0x{x}", .{@intFromPtr(c)})),
        .native => |n| try buf.appendSlice(allocator, try std.fmt.bufPrint(&stack_buf, "function: native '{s}'", .{n.name})),
    }
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
    vm.bindEnv();
    const result = try vm.run();
    return result.values;
}

/// Run + return the print-output buffer alongside results. Used by
/// stdlib tests that exercise `print`.
fn runSrcWithOutput(arena: std.mem.Allocator, src: []const u8) !struct { values: []TValue, output: []const u8 } {
    var p = try parser.Parser.init(arena, src);
    const block = try p.parseChunk();
    var c = codegen.Compiler.init(arena);
    const proto = try c.compileChunk(block);
    var vm = try VM.init(arena, proto);
    vm.bindEnv();
    const result = try vm.run();
    return .{ .values = result.values, .output = vm.output.items };
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
        const r = try runSrc(arena, "return !nil");
        try testing.expect(r[0] == .boolean and r[0].boolean);
    }
    {
        const r = try runSrc(arena, "return !0");
        // In Lua, 0 is truthy, so `not 0` is false.
        try testing.expect(r[0] == .boolean and !r[0].boolean);
    }
    {
        const r = try runSrc(arena, "return !false");
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

// === Phase 3.2.1 + 2.1: locals + MOVE ========================================

test "vm: local variable round-trip" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "local x = 5\nreturn x");
    try testing.expectEqual(@as(i64, 5), r[0].integer);
}

test "vm: local with arithmetic on right side" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "local x = 3 * 7\nreturn x");
    try testing.expectEqual(@as(i64, 21), r[0].integer);
}

test "vm: chained locals" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local x = 5
        \\local y = x * 2
        \\local z = y + 1
        \\return z
    );
    try testing.expectEqual(@as(i64, 11), r[0].integer);
}

test "vm: local without initializer is nil" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "local x\nreturn x");
    try testing.expect(r[0] == .nil);
}

test "vm: multi-target local with extra names" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Lua: extra names get nil. `local a, b, c = 1` -> a=1, b=nil, c=nil.
    const r = try runSrc(arena, "local a, b, c = 1\nreturn a, b, c");
    try testing.expectEqual(@as(usize, 3), r.len);
    try testing.expectEqual(@as(i64, 1), r[0].integer);
    try testing.expect(r[1] == .nil);
    try testing.expect(r[2] == .nil);
}

test "vm: assignment updates local" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local x = 5
        \\x += 10
        \\return x
    );
    try testing.expectEqual(@as(i64, 15), r[0].integer);
}

test "vm: unknown identifier is a global lookup → nil" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Lua treats unbound names as `_ENV.name` lookups. If _ENV doesn't
    // hold the name, the result is nil — never a compile error.
    const r = try runSrc(arena, "return undefined_thing");
    try testing.expect(r[0] == .nil);
}

// === Phase 3.2.2 + 2.2: comparison + control flow ============================

test "vm: comparison materializes booleans" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    {
        const r = try runSrc(arena, "return 3 < 5");
        try testing.expect(r[0].boolean);
    }
    {
        const r = try runSrc(arena, "return 5 < 3");
        try testing.expect(!r[0].boolean);
    }
    {
        const r = try runSrc(arena, "return 3 == 3");
        try testing.expect(r[0].boolean);
    }
    {
        const r = try runSrc(arena, "return 3 != 4");
        try testing.expect(r[0].boolean);
    }
    {
        const r = try runSrc(arena, "return 5 > 3");
        try testing.expect(r[0].boolean);
    }
    {
        const r = try runSrc(arena, "return 5 >= 5");
        try testing.expect(r[0].boolean);
    }
    {
        const r = try runSrc(arena, "return 5 <= 4");
        try testing.expect(!r[0].boolean);
    }
}

test "vm: if-then takes the branch when true" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "if 5 > 3 then return 1 end\nreturn 0");
    try testing.expectEqual(@as(i64, 1), r[0].integer);
}

test "vm: if-then-else takes else when false" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "if 5 < 3 then return 1 else return 2 end");
    try testing.expectEqual(@as(i64, 2), r[0].integer);
}

test "vm: if-elseif-else picks the right arm" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const src =
        \\local x = 5
        \\if x < 3 then return 1
        \\elseif x < 7 then return 2
        \\elseif x < 100 then return 3
        \\else return 4 end
    ;
    const r = try runSrc(arena, src);
    try testing.expectEqual(@as(i64, 2), r[0].integer);
}

test "vm: while loop counts down" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const src =
        \\local x = 10
        \\local sum = 0
        \\while x > 0 do
        \\    sum += x
        \\    x -= 1
        \\end
        \\return sum
    ;
    const r = try runSrc(arena, src);
    // 10+9+8+...+1 = 55
    try testing.expectEqual(@as(i64, 55), r[0].integer);
}

test "vm: while loop never enters when condition starts false" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local x = 0
        \\while x > 0 do x -= 1 end
        \\return x
    );
    try testing.expectEqual(@as(i64, 0), r[0].integer);
}

test "vm: nested while loops" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Compute sum_{i=1..3} i*3 = 3+6+9 = 18 via nested loops.
    const src =
        \\local i = 1
        \\local total = 0
        \\while i <= 3 do
        \\    local j = 1
        \\    while j <= 3 do
        \\        total += 1
        \\        j += 1
        \\    end
        \\    i += 1
        \\end
        \\return total
    ;
    const r = try runSrc(arena, src);
    try testing.expectEqual(@as(i64, 9), r[0].integer);
}

test "vm: comparison returns boolean from local" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local result = (10 - 5) == 5
        \\return result
    );
    try testing.expect(r[0].boolean);
}

// === Phase 3.2.3 + 2.3: function calls =======================================

test "vm: anonymous function called inline" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local f = function(x) return x * 2 end
        \\return f(21)
    );
    try testing.expectEqual(@as(i64, 42), r[0].integer);
}

test "vm: function with multiple args" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local add = function(x, y) return x + y end
        \\return add(3, 4)
    );
    try testing.expectEqual(@as(i64, 7), r[0].integer);
}

test "vm: function with internal locals" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local mult_then_add = function(x, y, z)
        \\    local product = x * y
        \\    return product + z
        \\end
        \\return mult_then_add(3, 4, 5)
    );
    try testing.expectEqual(@as(i64, 17), r[0].integer);
}

test "vm: function with control flow" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local abs = function(x)
        \\    if x < 0 then return -x else return x end
        \\end
        \\return abs(-5), abs(7)
    );
    try testing.expectEqual(@as(i64, 5), r[0].integer);
    try testing.expectEqual(@as(i64, 7), r[1].integer);
}

test "vm: nested calls (no closure-over-outer)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Both calls happen at the same scope level — no inner function
    // captures outer locals, so no upvalues required.
    const r = try runSrc(arena,
        \\local double = function(x) return x * 2 end
        \\return double(double(5))
    );
    try testing.expectEqual(@as(i64, 20), r[0].integer);
}

test "vm: closure captures outer local as upvalue" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // `quadruple` body references `double` from the enclosing scope
    // — the compiler resolves it as an upvalue, the runtime CLOSURE
    // captures it as an open cell pointing at the outer's register.
    const r = try runSrc(arena,
        \\local double = function(x) return x * 2 end
        \\local quadruple = function(x) return double(double(x)) end
        \\return quadruple(5)
    );
    try testing.expectEqual(@as(i64, 20), r[0].integer);
}

// === Phase 3.2.4 + 2.4: upvalues =============================================

test "vm: recursive fib via local function (the holy grail)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local function fib(n)
        \\    if n < 2 then return n end
        \\    return fib(n - 1) + fib(n - 2)
        \\end
        \\return fib(10)
    );
    // fib(10) = 55
    try testing.expectEqual(@as(i64, 55), r[0].integer);
}

test "vm: closure-shared upvalue sees writes from both sides" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // `inc` modifies `count` (an upvalue) — `read` sees it.
    // Both closures share the same UpvalueCell.
    const r = try runSrc(arena,
        \\local count = 0
        \\local inc = function() count += 1 end
        \\local read = function() return count end
        \\inc()
        \\inc()
        \\inc()
        \\return read()
    );
    try testing.expectEqual(@as(i64, 3), r[0].integer);
}

test "vm: deeply nested closures chain upvalues correctly" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // The inner-inner function captures `x` through TWO levels of
    // function nesting — middle's upvalue desc has in_stack=true
    // (captures from outer's register), inner's has in_stack=false
    // (chains through middle's upvalue). Tests the compiler's
    // upvalue chain traversal and the runtime's chain-through path.
    const r = try runSrc(arena,
        \\local x = 100
        \\local outer = function()
        \\    local inner = function() return x end
        \\    return inner()
        \\end
        \\return outer()
    );
    try testing.expectEqual(@as(i64, 100), r[0].integer);
}

// === Phase 3.2.5 + 2.5: tables ===============================================

test "vm: empty table constructor" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return {}");
    try testing.expect(r[0] == .table);
    try testing.expectEqual(@as(u32, 0), r[0].table.len());
}

test "vm: array-style table" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local t = {10, 20, 30}
        \\return t[1], t[2], t[3]
    );
    try testing.expectEqual(@as(i64, 10), r[0].integer);
    try testing.expectEqual(@as(i64, 20), r[1].integer);
    try testing.expectEqual(@as(i64, 30), r[2].integer);
}

test "vm: keyed table with string keys" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local t = {name = "alice", age = 30}
        \\return t.name, t.age
    );
    try testing.expectEqualStrings("alice", r[0].string.slice());
    try testing.expectEqual(@as(i64, 30), r[1].integer);
}

test "vm: computed-key table" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local k = "dynamic"
        \\local t = {[k] = 42}
        \\return t.dynamic, t[k]
    );
    try testing.expectEqual(@as(i64, 42), r[0].integer);
    try testing.expectEqual(@as(i64, 42), r[1].integer);
}

test "vm: mixed table" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local t = {1, 2, name = "mixed", 3}
        \\return t[1], t[2], t[3], t.name
    );
    try testing.expectEqual(@as(i64, 1), r[0].integer);
    try testing.expectEqual(@as(i64, 2), r[1].integer);
    try testing.expectEqual(@as(i64, 3), r[2].integer);
    try testing.expectEqualStrings("mixed", r[3].string.slice());
}

test "vm: table assignment via index" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local t = {}
        \\t[1] = 100
        \\t[2] = 200
        \\t.tag = "custom"
        \\return t[1], t[2], t.tag
    );
    try testing.expectEqual(@as(i64, 100), r[0].integer);
    try testing.expectEqual(@as(i64, 200), r[1].integer);
    try testing.expectEqualStrings("custom", r[2].string.slice());
}

test "vm: missing table key returns nil" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local t = {a = 1}
        \\return t.b, t.missing
    );
    try testing.expect(r[0] == .nil);
    try testing.expect(r[1] == .nil);
}

test "vm: table as function argument and return value" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local function copy_with_doubled_value(t)
        \\    return {n = t.n * 2}
        \\end
        \\local result = copy_with_doubled_value({n = 21})
        \\return result.n
    );
    try testing.expectEqual(@as(i64, 42), r[0].integer);
}

test "vm: tables and closures together" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Counter object with two methods sharing state via a table.
    const r = try runSrc(arena,
        \\local function make()
        \\    local self = {count = 0}
        \\    self.inc = function() self.count = self.count + 1 end
        \\    self.get = function() return self.count end
        \\    return self
        \\end
        \\local c = make()
        \\c.inc()
        \\c.inc()
        \\c.inc()
        \\return c.get()
    );
    try testing.expectEqual(@as(i64, 3), r[0].integer);
}

// === Phase 3.5: stdlib (print, tostring, type, tonumber) ====================

test "stdlib: print writes to output buffer" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrcWithOutput(arena, "print(\"hello\", \"world\")");
    try testing.expectEqualStrings("hello\tworld\n", r.output);
}

test "stdlib: print formats values like Lua tostring" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrcWithOutput(arena, "print(42, 3.14, true, nil)");
    try testing.expectEqualStrings("42\t3.14\ttrue\tnil\n", r.output);
}

test "stdlib: type returns the value's type" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return type(42), type(\"x\"), type({}), type(nil), type(true), type(print)");
    try testing.expectEqualStrings("number", r[0].string.slice());
    try testing.expectEqualStrings("string", r[1].string.slice());
    try testing.expectEqualStrings("table", r[2].string.slice());
    try testing.expectEqualStrings("nil", r[3].string.slice());
    try testing.expectEqualStrings("boolean", r[4].string.slice());
    try testing.expectEqualStrings("function", r[5].string.slice());
}

test "stdlib: tostring for various values" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return tostring(42), tostring(3.14), tostring(nil)");
    try testing.expectEqualStrings("42", r[0].string.slice());
    try testing.expectEqualStrings("3.14", r[1].string.slice());
    try testing.expectEqualStrings("nil", r[2].string.slice());
}

test "stdlib: tonumber parses strings" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "return tonumber(\"42\"), tonumber(\"3.14\"), tonumber(\"bogus\")");
    try testing.expectEqual(@as(i64, 42), r[0].integer);
    try testing.expectEqual(@as(f64, 3.14), r[1].number);
    try testing.expect(r[2] == .nil);
}

test "stdlib: globals are settable from user code" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Top-level `x = 5` (no `local`) writes to _ENV.x.
    // Reading x reads from _ENV.x. Lua's classic global semantics.
    const r = try runSrc(arena,
        \\x = 100
        \\y = x + 1
        \\return y
    );
    try testing.expectEqual(@as(i64, 101), r[0].integer);
}

test "stdlib: closures + globals + print together" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrcWithOutput(arena,
        \\local function fib(n)
        \\    if n < 2 then return n end
        \\    return fib(n - 1) + fib(n - 2)
        \\end
        \\print("fib(10) =", fib(10))
        \\print("fib(15) =", fib(15))
    );
    try testing.expectEqualStrings("fib(10) =\t55\nfib(15) =\t610\n", r.output);
}

// === Phase 4.1: enforced type annotations =====================================

test "type: literal RHS matches annotation — no error" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "local x: number = 42\nreturn x");
    try testing.expectEqual(@as(i64, 42), r[0].integer);
}

test "type: literal RHS mismatch — compile error" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // `local foo: string = 0.0` — number literal can't satisfy a
    // string annotation. Caught at compile time, no bytecode emitted.
    try testing.expectError(error.TypeAnnotationMismatch, runSrc(arena,
        \\local foo: string = 0.0
        \\return foo
    ));
}

test "type: integer accepted for `number` annotation (numeric tower)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena, "local x: number = 42\nreturn x");
    try testing.expectEqual(@as(i64, 42), r[0].integer);
}

test "type: float NOT accepted for `integer` annotation" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    try testing.expectError(error.TypeAnnotationMismatch, runSrc(arena,
        \\local x: integer = 3.14
        \\return x
    ));
}

test "type: `any` disables checking" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local x: any = "string"
        \\return x
    );
    try testing.expectEqualStrings("string", r[0].string.slice());
}

test "type: function call result enforced at runtime" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Call result type isn't statically known → TYPECHECK opcode
    // verifies at runtime. Here the runtime value matches; passes.
    const r = try runSrc(arena,
        \\local f = function() return 99 end
        \\local x: number = f()
        \\return x
    );
    try testing.expectEqual(@as(i64, 99), r[0].integer);
}

test "type: function call result mismatch fails at runtime" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Call returns a string, annotation says number → TYPECHECK
    // raises at runtime. Static check can't reject this because
    // we don't track types through expressions yet.
    try testing.expectError(error.TypeAssertionFailed, runSrc(arena,
        \\local f = function() return "not a number" end
        \\local x: number = f()
        \\return x
    ));
}

test "type: missing initializer rejected for non-any annotation" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // `local x: string` with no initializer → x would be nil,
    // doesn't satisfy `string`. Compile error.
    try testing.expectError(error.TypeAnnotationMismatch, runSrc(arena, "local x: string"));
}

test "type: unknown type name rejected" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    try testing.expectError(error.StrictPlutoViolation, runSrc(arena,
        \\local x: Banana = 5
        \\return x
    ));
}

test "type: per-name annotations in multi-local" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local a: integer, b: string = 1, "two"
        \\return a, b
    );
    try testing.expectEqual(@as(i64, 1), r[0].integer);
    try testing.expectEqualStrings("two", r[1].string.slice());
}

// === Phase 4.2: typed function params + return ================================

test "function: typed param accepts matching arg" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local f = function(x: number) return x * 2 end
        \\return f(21)
    );
    try testing.expectEqual(@as(i64, 42), r[0].integer);
}

test "function: typed param rejects mismatched arg at runtime" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    try testing.expectError(error.TypeAssertionFailed, runSrc(arena,
        \\local f = function(x: number) return x * 2 end
        \\return f("not a number")
    ));
}

test "function: typed return matches at runtime" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local f = function(): number return 99 end
        \\return f()
    );
    try testing.expectEqual(@as(i64, 99), r[0].integer);
}

test "function: typed return rejects literal mismatch at compile time" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    try testing.expectError(error.TypeAnnotationMismatch, runSrc(arena,
        \\local f = function(): number return "not a number" end
        \\return f()
    ));
}

test "function: typed return rejects computed mismatch at runtime" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Inner function returns a string; outer claims to return a number.
    // Compile-time can't see through the inner call, so TYPECHECK
    // catches it at runtime.
    try testing.expectError(error.TypeAssertionFailed, runSrc(arena,
        \\local make_string = function() return "oops" end
        \\local f = function(): number return make_string() end
        \\return f()
    ));
}

test "function: bare `return` rejected when type is declared" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // `return` (no value) is nil, which doesn't satisfy `: number`.
    try testing.expectError(error.TypeAnnotationMismatch, runSrc(arena,
        \\local f = function(): number return end
        \\return f()
    ));
}

test "function: multiple typed params" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local concat = function(a: string, b: string): string
        \\    return a .. b
        \\end
        \\return concat("hello, ", "world")
    );
    try testing.expectEqualStrings("hello, world", r[0].string.slice());
}

test "function: typed param rejects on second arg too" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    try testing.expectError(error.TypeAssertionFailed, runSrc(arena,
        \\local f = function(a: number, b: string) return a end
        \\return f(1, 2)
    ));
}

test "function: untyped params still work alongside typed ones" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Mix typed and untyped — only the typed ones are enforced.
    const r = try runSrc(arena,
        \\local f = function(a: number, b)
        \\    return a + 1, b
        \\end
        \\return f(5, "anything")
    );
    try testing.expectEqual(@as(i64, 6), r[0].integer);
}

test "vm: indexing non-table is a type error" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    try testing.expectError(error.InvalidArithmeticOperand, runSrc(arena,
        \\local x = 5
        \\return x[1]
    ));
}

test "vm: upvalue survives parent return (closes correctly)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // `make_counter` returns a closure that captures its local `n`.
    // When make_counter returns, the cell must close (copy n's value
    // into the cell's storage) so the returned closure still works.
    const r = try runSrc(arena,
        \\local function make_counter()
        \\    local n = 0
        \\    return function()
        \\        n += 1
        \\        return n
        \\    end
        \\end
        \\local c = make_counter()
        \\return c(), c(), c()
    );
    try testing.expectEqual(@as(i64, 1), r[0].integer);
    try testing.expectEqual(@as(i64, 2), r[1].integer);
    try testing.expectEqual(@as(i64, 3), r[2].integer);
}

test "vm: function returns multiple values" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Multi-return is mostly captured at the bytecode level — when a
    // call's result feeds another expression, only the first result
    // is taken. For top-level return propagation, our codegen
    // currently emits CALL with C=2 (one expected result), so only
    // the first value flows through. Real multi-value chaining is a
    // codegen refinement (CALL C=0 for "return all"). Phase 2.3 just
    // checks the call returns correctly when one value is requested.
    const r = try runSrc(arena,
        \\local f = function() return 1, 2, 3 end
        \\local first = f()
        \\return first
    );
    try testing.expectEqual(@as(i64, 1), r[0].integer);
}

test "vm: local function declaration" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local function square(x) return x * x end
        \\return square(8)
    );
    try testing.expectEqual(@as(i64, 64), r[0].integer);
}

test "vm: function-call-as-statement (results discarded)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // The value of f(...) is discarded; only side effects (none here)
    // would be observed. We're testing that the codegen emits CALL
    // with C=1 (zero results requested) and the VM doesn't crash.
    const r = try runSrc(arena,
        \\local x = 0
        \\local set = function(v) return v end
        \\set(99)
        \\return x
    );
    try testing.expectEqual(@as(i64, 0), r[0].integer);
}

// === Phase 4.4a: metatables, __index, method calls =========================

test "vm: setmetatable / getmetatable round-trip" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local t = {}
        \\local mt = {}
        \\setmetatable(t, mt)
        \\return getmetatable(t) == mt
    );
    try testing.expectEqual(true, r[0].boolean);
}

test "vm: __index falls back to a metatable on read miss" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // `t` has no `name` key directly; the metatable's __index points
    // at a default-table that does. Lua/Pluto resolves t.name through
    // mt.__index.name. The own-key `count = 7` is read raw.
    const r = try runSrc(arena,
        \\local defaults = {name = "anon", role = "guest"}
        \\local mt = {__index = defaults}
        \\local t = setmetatable({count = 7}, mt)
        \\return t.name, t.role, t.count
    );
    try testing.expectEqualStrings("anon", r[0].string.slice());
    try testing.expectEqualStrings("guest", r[1].string.slice());
    try testing.expectEqual(@as(i64, 7), r[2].integer);
}

test "vm: __index chain walks transitively (parent → grandparent)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local grand = {tag = "grand"}
        \\local parent = setmetatable({}, {__index = grand})
        \\local child = setmetatable({}, {__index = parent})
        \\return child.tag
    );
    try testing.expectEqualStrings("grand", r[0].string.slice());
}

test "vm: method call obj:method(args) passes obj as self" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // `:` desugars to passing the receiver as the first arg. Define
    // greet as `function(self, name)` so we can verify both args.
    const r = try runSrc(arena,
        \\local obj = {prefix = "Hi, "}
        \\obj.greet = function(self, name) return self.prefix .. name end
        \\return obj:greet("Alice")
    );
    try testing.expectEqualStrings("Hi, Alice", r[0].string.slice());
}

test "vm: method dispatch through __index (class-style)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // The shape every Lua-OO tutorial uses: a "class" table holds
    // methods, instances are tables with the class as their metatable's
    // __index. `inst:speak()` resolves `speak` via the metatable chain
    // and invokes it with `inst` as self.
    const r = try runSrc(arena,
        \\local Dog = {}
        \\Dog.speak = function(self) return self.name .. " says woof" end
        \\local function new_dog(name)
        \\  return setmetatable({name = name}, {__index = Dog})
        \\end
        \\local d = new_dog("Rex")
        \\return d:speak()
    );
    try testing.expectEqualStrings("Rex says woof", r[0].string.slice());
}

test "vm: method call evaluates receiver only once" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // If receiver were eval'd twice, calls would equal 2. SELF reads
    // the receiver register once and reuses it for both the method
    // lookup and the implicit self argument.
    const r = try runSrc(arena,
        \\local calls = 0
        \\local obj = {}
        \\obj.run = function(self) return "ok" end
        \\local function get_obj() calls += 1 return obj end
        \\local result = get_obj():run()
        \\return calls, result
    );
    try testing.expectEqual(@as(i64, 1), r[0].integer);
    try testing.expectEqualStrings("ok", r[1].string.slice());
}

test "vm: method-call statement form (discards results)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Method call as a bare statement — no assignment, results
    // discarded. The body still runs and mutates shared state.
    const r = try runSrc(arena,
        \\local box = {n = 0}
        \\box.bump = function(self) self.n = self.n + 1 end
        \\box:bump()
        \\box:bump()
        \\box:bump()
        \\return box.n
    );
    try testing.expectEqual(@as(i64, 3), r[0].integer);
}

test "vm: switch — single-value cases pick the matching branch" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local x = 2
        \\local result
        \\switch x
        \\  case 1: result = "one"
        \\  case 2: result = "two"
        \\  case 3: result = "three"
        \\  default: result = "other"
        \\end
        \\return result
    );
    try testing.expectEqualStrings("two", r[0].string.slice());
}

test "vm: switch — default clause runs when nothing matches" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local x = 99
        \\local result
        \\switch x
        \\  case 1: result = "one"
        \\  case 2: result = "two"
        \\  default: result = "other"
        \\end
        \\return result
    );
    try testing.expectEqualStrings("other", r[0].string.slice());
}

test "vm: switch — no default and no match leaves vars untouched" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local x = 99
        \\local result = "untouched"
        \\switch x
        \\  case 1: result = "one"
        \\  case 2: result = "two"
        \\end
        \\return result
    );
    try testing.expectEqualStrings("untouched", r[0].string.slice());
}

test "vm: switch — multi-value case (case 1, 2, 3:) matches any value" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Verify each value in the multi-value case independently routes
    // into the same body. Run three times via a helper-flavored fn.
    const src =
        \\local function classify(n)
        \\  switch n
        \\    case 1, 2, 3: return "small"
        \\    case 10, 20, 30: return "medium"
        \\    default: return "other"
        \\  end
        \\end
        \\return classify(2), classify(20), classify(7)
    ;
    const r = try runSrc(arena, src);
    try testing.expectEqualStrings("small", r[0].string.slice());
    try testing.expectEqualStrings("medium", r[1].string.slice());
    try testing.expectEqualStrings("other", r[2].string.slice());
}

test "vm: switch — string discriminant" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local s = "go"
        \\local n
        \\switch s
        \\  case "stop": n = 0
        \\  case "go", "run": n = 1
        \\  default: n = -1
        \\end
        \\return n
    );
    try testing.expectEqual(@as(i64, 1), r[0].integer);
}

test "vm: switch — no fallthrough between cases" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // strict-Pluto: case 1 sets x=1 and stops. We must NOT see x mutated
    // by the case 2 body. Without fallthrough x stays 1; with fallthrough
    // it would become 2 (or 99 from default).
    const r = try runSrc(arena,
        \\local x = 0
        \\switch 1
        \\  case 1: x = 1
        \\  case 2: x = 2
        \\  default: x = 99
        \\end
        \\return x
    );
    try testing.expectEqual(@as(i64, 1), r[0].integer);
}

test "vm: switch — break inside a case body short-circuits" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // `break` mid-case skips the rest of that case body and lands at
    // the end of the switch. `done` should NOT be set.
    const r = try runSrc(arena,
        \\local x = 1
        \\local entered = false
        \\local done = false
        \\switch x
        \\  case 1:
        \\    entered = true
        \\    break
        \\    done = true
        \\  default: x = 99
        \\end
        \\return entered, done
    );
    try testing.expectEqual(true, r[0].boolean);
    try testing.expectEqual(false, r[1].boolean);
}

test "vm: switch — break in while loop now also works" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // The break_jumps stack added for switch generalizes: while loops
    // also now respect `break`. Phase 4.7 will add `continue` and
    // multi-level forms on top of the same machinery.
    const r = try runSrc(arena,
        \\local i = 0
        \\while true do
        \\  i += 1
        \\  if i >= 5 then break end
        \\end
        \\return i
    );
    try testing.expectEqual(@as(i64, 5), r[0].integer);
}

test "vm: switch — nested switches, break exits innermost" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const r = try runSrc(arena,
        \\local outer = 0
        \\local inner = 0
        \\switch 1
        \\  case 1:
        \\    outer = 10
        \\    switch 2
        \\      case 2:
        \\        inner = 20
        \\        break
        \\        inner = 999
        \\    end
        \\    outer = outer + 1
        \\end
        \\return outer, inner
    );
    try testing.expectEqual(@as(i64, 11), r[0].integer);
    try testing.expectEqual(@as(i64, 20), r[1].integer);
}

test "vm: break outside any switch/while is a compile error" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    try testing.expectError(
        error.Unimplemented,
        runSrc(arena, "break\nreturn 1"),
    );
}

test "vm: calling a non-closure is a type error" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    try testing.expectError(
        error.InvalidArithmeticOperand,
        runSrc(arena,
            \\local x = 5
            \\return x(1)
        ),
    );
}
