//! Abstract syntax tree for the Pluto-Lang source language.
//!
//! The parser produces these nodes; the bytecode emitter (phase 3.2)
//! consumes them. AST nodes are arena-allocated — parse a chunk into
//! one arena, emit bytecode, drop the arena. Nothing here is GC-
//! managed. The runtime String / Table / TValue types in value.zig
//! and friends are unrelated to the AST.

const std = @import("std");

pub const BinaryOp = enum {
    // arithmetic
    add, sub, mul, div, idiv, mod, pow,
    // bitwise
    band, bor, bxor, shl, shr,
    // concat (right-associative in Lua)
    concat,
    // comparison
    eq, neq, lt, lte, gt, gte,
    // logical (short-circuit)
    and_, or_,

    pub fn lexeme(self: BinaryOp) []const u8 {
        return switch (self) {
            .add => "+",      .sub => "-",      .mul => "*",
            .div => "/",      .idiv => "//",    .mod => "%",
            .pow => "^",      .band => "&",     .bor => "|",
            .bxor => "~",     .shl => "<<",     .shr => ">>",
            .concat => "..",  .eq => "==",      .neq => "~=",
            .lt => "<",       .lte => "<=",     .gt => ">",
            .gte => ">=",     .and_ => "and",   .or_ => "or",
        };
    }
};

pub const UnaryOp = enum {
    neg,    // -
    not_,   // not
    len,    // #
    bnot,   // ~

    pub fn lexeme(self: UnaryOp) []const u8 {
        return switch (self) {
            .neg => "-", .not_ => "not", .len => "#", .bnot => "~",
        };
    }
};

pub const Expr = union(enum) {
    // Atoms
    nil: void,
    boolean: bool,
    integer: i64,
    number: f64,
    string: []const u8, // unescaped content (parser does the unescape)
    ident: []const u8,
    vararg: void,       // `...`

    // Compound
    binary: Binary,
    unary: Unary,
    index: Index,        // a[b]
    field: Field,        // a.b  (b is an Ident-like atom)
    call: Call,          // f(args)
    method_call: MethodCall, // obj:method(args) — implicit self
    function: Function,  // function(...) body end
    table: Table,        // { ... }
    new_expr: NewExpr,   // new ClassExpr(args) — strict-Pluto class instantiation
    ternary: Ternary,    // cond ? a : b — strict-Pluto's safe alternative to `a and b or c`

    pub const Binary = struct { op: BinaryOp, lhs: *Expr, rhs: *Expr };
    pub const Unary = struct { op: UnaryOp, operand: *Expr };
    pub const Index = struct { object: *Expr, key: *Expr };
    pub const Field = struct { object: *Expr, name: []const u8 };
    pub const Call = struct { callee: *Expr, args: []const *Expr };
    pub const MethodCall = struct {
        receiver: *Expr,
        method: []const u8,
        args: []const *Expr,
    };
    pub const Ternary = struct {
        cond: *Expr,
        then_expr: *Expr,
        else_expr: *Expr,
    };
    pub const NewExpr = struct {
        /// The class to instantiate. Currently restricted to a primary
        /// expression (typically an ident — `new Foo(...)`); arbitrary
        /// expressions like `new (compute_class())(...)` are rejected
        /// at parse time for clarity.
        class: *Expr,
        args: []const *Expr,
    };
    pub const Function = struct {
        params: []const NameWithType,
        has_vararg: bool,
        /// Declared return type. When set, strict-Pluto checks every
        /// `return v` against this type — compile-time for literal
        /// values, runtime TYPECHECK for computed ones.
        return_type: ?TypeExpr = null,
        body: *Block,
    };
    pub const Table = struct { fields: []const TableField };
    pub const TableField = struct {
        /// null key = positional ("array part"); string key = named
        /// (`{x = 1}`); expr key = computed (`{[k] = v}`).
        key: ?TableKey,
        value: *Expr,
    };
    pub const TableKey = union(enum) {
        named: []const u8,
        computed: *Expr,
    };
};

/// Optional type annotation on locals + function params.
///
/// strict-Pluto enforces these: when present, values are checked
/// against the annotation at compile time (literal RHS) or runtime
/// (computed RHS via TYPECHECK opcode). Annotations are *optional*
/// — `local x = 5` works fine, `local x: number = 5` adds a check.
pub const TypeExpr = union(enum) {
    atom: AtomicType,
    /// T? — value may be of type T or nil.
    optional: *TypeExpr,
};

pub const AtomicType = enum {
    any, // disables checking — explicit "I don't want a check here"
    nil_,
    boolean,
    number, // covers int + float (Lua 5.4 numeric tower)
    integer, // strict integer subtype
    string,
    table,
    function,

    pub fn fromLexeme(lex: []const u8) ?AtomicType {
        const std_ = @import("std");
        const map = .{
            .{ "any", .any },
            .{ "nil", .nil_ },
            .{ "boolean", .boolean },
            .{ "bool", .boolean }, // accept both nominal forms
            .{ "number", .number },
            .{ "integer", .integer },
            .{ "int", .integer },
            .{ "string", .string },
            .{ "table", .table },
            .{ "function", .function },
        };
        inline for (map) |entry| {
            if (std_.mem.eql(u8, lex, entry[0])) return entry[1];
        }
        return null;
    }

    pub fn name(self: AtomicType) []const u8 {
        return switch (self) {
            .any => "any",
            .nil_ => "nil",
            .boolean => "boolean",
            .number => "number",
            .integer => "integer",
            .string => "string",
            .table => "table",
            .function => "function",
        };
    }
};

/// A local variable declaration / function parameter: name + optional
/// type annotation + optional default value (only meaningful for
/// function params; ignored on local declarations).
pub const NameWithType = struct {
    name: []const u8,
    type_annot: ?TypeExpr = null,
    /// Default expression, evaluated lazily when a function is called
    /// with this parameter omitted (or explicitly nil). Used only on
    /// function parameters; the parser of `local x: T = expr` puts
    /// the value in the local's value list, not here.
    default: ?*Expr = null,
};

pub const Stmt = union(enum) {
    /// Multi-target assignment: `a, b = e1, e2`. Targets are
    /// restricted to ident / index / field at parse time.
    assign: Assign,
    /// `local a, b = e1, e2`
    local: Local,
    /// `if cond then ... [elseif cond then ...]* [else ...] end`
    if_stmt: If,
    /// `while cond do ... end`
    while_stmt: While,
    /// `for IDENT = start, stop[, step] do body end` — numeric for.
    /// Lowers to a while-style loop in codegen; no new opcodes.
    numeric_for: NumericFor,
    /// `for IDENT [, IDENT2, ...] in iter_expr [, ...] do body end`.
    /// Lua/Pluto's generic-for. Codegen lowers to a loop calling the
    /// iterator triple per Lua's protocol — no new opcodes.
    generic_for: GenericFor,
    /// `switch expr case v1[,v2]: body case v3: body [default: body] end`
    /// Strict-Pluto choice: cases never fall through (each case ends
    /// implicitly). `break` inside a case is the canonical early-exit;
    /// it just lands at the end of the switch like any unterminated case.
    switch_stmt: Switch,
    /// `class Name [extends Parent] <members> end` — strict-Pluto class
    /// declaration. Lowers to a synthesized table with metatable wiring
    /// for inheritance. See codegen.emitClassDecl.
    class_decl: ClassDecl,
    /// `function name.path(args) body end`. Sugar for
    /// `name.path = function(args) body end`. Stored expanded.
    function_decl: FunctionDecl,
    /// `local function name(args) body end`. Distinct from
    /// `local name = function(...)` because it allows the body to
    /// see its own name (recursion).
    local_function: LocalFunction,
    /// `return e1, e2 [;]`
    return_stmt: Return,
    /// `break` or `break N` — exits the Nth-innermost enclosing
    /// breakable construct (loop or switch). N=1 (the default) is
    /// the current innermost; higher N skips outward.
    break_stmt: BreakOrContinue,
    /// `continue` or `continue N` — jumps to the cond evaluation of
    /// the Nth-innermost enclosing loop. Switches don't count toward
    /// the level — only loops, matching Java/JS behavior.
    continue_stmt: BreakOrContinue,
    /// Expression-statement (only function calls reach here in Lua).
    expr_stmt: *Expr,

    pub const Assign = struct { targets: []const *Expr, values: []const *Expr };
    pub const Local = struct { names: []const NameWithType, values: []const *Expr };
    pub const If = struct {
        // Pairs of (cond, then_block) — the first is the `if`, rest
        // are `elseif`s. Optional `else_block` is the trailing else.
        branches: []const Branch,
        else_block: ?*Block,
    };
    pub const Branch = struct { cond: *Expr, body: *Block };
    pub const While = struct { cond: *Expr, body: *Block };
    pub const NumericFor = struct {
        var_name: []const u8,
        start: *Expr,
        stop: *Expr,
        /// Optional step. Defaults to 1 when null. If a literal
        /// integer, codegen uses its sign to pick the comparison
        /// direction (ascending vs descending). Variable steps are
        /// always treated as ascending (caller's responsibility).
        step: ?*Expr,
        body: *Block,
    };
    pub const GenericFor = struct {
        var_names: []const []const u8,
        /// The expressions evaluated once at loop entry to produce
        /// the iterator triple `(iter_fn, state, control)`. Typically
        /// a single `pairs(t)` call, but Lua's grammar accepts up to
        /// three expressions for advanced iterator factories.
        iter_exprs: []const *Expr,
        body: *Block,
    };
    pub const Switch = struct {
        discriminant: *Expr,
        cases: []const Case,
        default_block: ?*Block,
    };
    pub const Case = struct {
        /// One or more values to match (`case 1, 2, 3:`). Comparison
        /// is by value equality (Lua's `==`), evaluated against the
        /// discriminant in source order; first match wins.
        values: []const *Expr,
        body: *Block,
    };
    pub const ClassDecl = struct {
        name: []const u8,
        /// `extends Parent` — single inheritance only. The parent
        /// becomes the class's `__index` so methods inherit through
        /// the metatable chain we already implement.
        parent: ?[]const u8,
        members: []const ClassMember,
    };
    pub const ClassMember = union(enum) {
        method: Method,
        field: Field,
    };
    pub const Method = struct {
        name: []const u8,
        /// The function as written by the user. For non-static methods
        /// the parser prepends a `this` first parameter so method
        /// bodies can refer to the receiver as `this`. Static methods
        /// skip that injection — they're class-level helpers, not
        /// instance-bound.
        func: Expr.Function,
        visibility: Visibility = .private,
        is_static: bool = false,
    };
    pub const Field = struct {
        name: []const u8,
        value: *Expr,
        visibility: Visibility = .private,
        is_static: bool = false,
    };

    /// Strict-Pluto class member visibility. Default is `private` —
    /// you have to explicitly opt members into being externally
    /// accessible. The runtime checks every member access against
    /// these tags (see vm.checkVisibility). The numeric values are
    /// load-bearing: codegen serializes them as integers into the
    /// class's `__visibility` sub-table, and the VM compares those
    /// integers directly without re-mapping.
    pub const Visibility = enum(u8) {
        public = 0,
        protected = 1,
        private = 2,

        pub fn name(self: Visibility) []const u8 {
            return switch (self) {
                .public => "public",
                .protected => "protected",
                .private => "private",
            };
        }
    };
    pub const FunctionDecl = struct {
        /// Function name as a path: `a.b.c.d` -> ["a","b","c","d"].
        /// Single-element path is the common `function name(...)`.
        name_path: []const []const u8,
        is_method: bool, // last separator was ':' (function a:b())
        func: Expr.Function,
    };
    pub const LocalFunction = struct {
        name: []const u8,
        func: Expr.Function,
    };
    pub const Return = struct { values: []const *Expr };
    pub const BreakOrContinue = struct { level: u32 = 1 };
};

pub const Block = struct { stmts: []const Stmt };

// =============================================================================
// Pretty-printer (used by parser tests + the demo to validate AST shape)
// =============================================================================

pub fn dumpBlock(out: anytype, b: *const Block, indent: u32) !void {
    for (b.stmts) |s| try dumpStmt(out, &s, indent);
}

pub fn dumpStmt(out: anytype, s: *const Stmt, indent: u32) anyerror!void {
    try writeIndent(out, indent);
    switch (s.*) {
        .assign => |a| {
            try out.writeAll("(assign ");
            for (a.targets, 0..) |t, i| {
                if (i > 0) try out.writeAll(",");
                try dumpExpr(out, t);
            }
            try out.writeAll(" = ");
            for (a.values, 0..) |val, i| {
                if (i > 0) try out.writeAll(",");
                try dumpExpr(out, val);
            }
            try out.writeAll(")\n");
        },
        .local => |l| {
            try out.writeAll("(local ");
            for (l.names, 0..) |nt, i| {
                if (i > 0) try out.writeAll(",");
                try out.writeAll(nt.name);
                if (nt.type_annot) |ta| {
                    try out.writeAll(":");
                    try dumpType(out, ta);
                }
            }
            if (l.values.len > 0) {
                try out.writeAll(" = ");
                for (l.values, 0..) |val, i| {
                    if (i > 0) try out.writeAll(",");
                    try dumpExpr(out, val);
                }
            }
            try out.writeAll(")\n");
        },
        .if_stmt => |if_s| {
            try out.writeAll("(if\n");
            for (if_s.branches, 0..) |br, i| {
                try writeIndent(out, indent + 1);
                if (i == 0) try out.writeAll("if ") else try out.writeAll("elseif ");
                try dumpExpr(out, br.cond);
                try out.writeAll(" then\n");
                try dumpBlock(out, br.body, indent + 2);
            }
            if (if_s.else_block) |eb| {
                try writeIndent(out, indent + 1);
                try out.writeAll("else\n");
                try dumpBlock(out, eb, indent + 2);
            }
            try writeIndent(out, indent);
            try out.writeAll(")\n");
        },
        .while_stmt => |w| {
            try out.writeAll("(while ");
            try dumpExpr(out, w.cond);
            try out.writeAll("\n");
            try dumpBlock(out, w.body, indent + 1);
            try writeIndent(out, indent);
            try out.writeAll(")\n");
        },
        .numeric_for => |fr| {
            try out.writeAll("(for ");
            try out.writeAll(fr.var_name);
            try out.writeAll(" = ");
            try dumpExpr(out, fr.start);
            try out.writeAll(" ");
            try dumpExpr(out, fr.stop);
            if (fr.step) |step_e| {
                try out.writeAll(" ");
                try dumpExpr(out, step_e);
            }
            try out.writeAll("\n");
            try dumpBlock(out, fr.body, indent + 1);
            try writeIndent(out, indent);
            try out.writeAll(")\n");
        },
        .generic_for => |gf| {
            try out.writeAll("(for ");
            for (gf.var_names, 0..) |n, i| {
                if (i > 0) try out.writeAll(",");
                try out.writeAll(n);
            }
            try out.writeAll(" in ");
            for (gf.iter_exprs, 0..) |ex, i| {
                if (i > 0) try out.writeAll(",");
                try dumpExpr(out, ex);
            }
            try out.writeAll("\n");
            try dumpBlock(out, gf.body, indent + 1);
            try writeIndent(out, indent);
            try out.writeAll(")\n");
        },
        .class_decl => |cd| {
            try out.writeAll("(class ");
            try out.writeAll(cd.name);
            if (cd.parent) |p| {
                try out.writeAll(" extends ");
                try out.writeAll(p);
            }
            try out.writeAll("\n");
            for (cd.members) |m| {
                try writeIndent(out, indent + 1);
                switch (m) {
                    .method => |mm| {
                        try out.writeAll(mm.visibility.name());
                        try out.writeAll(if (mm.is_static) " static method " else " method ");
                        try out.writeAll(mm.name);
                        try out.writeAll("(");
                        try dumpParams(out, mm.func.params, mm.func.has_vararg);
                        try out.writeAll(")\n");
                        try dumpBlock(out, mm.func.body, indent + 2);
                    },
                    .field => |f| {
                        try out.writeAll(f.visibility.name());
                        try out.writeAll(if (f.is_static) " static field " else " field ");
                        try out.writeAll(f.name);
                        try out.writeAll(" = ");
                        try dumpExpr(out, f.value);
                        try out.writeAll("\n");
                    },
                }
            }
            try writeIndent(out, indent);
            try out.writeAll(")\n");
        },
        .switch_stmt => |sw| {
            try out.writeAll("(switch ");
            try dumpExpr(out, sw.discriminant);
            try out.writeAll("\n");
            for (sw.cases) |case| {
                try writeIndent(out, indent + 1);
                try out.writeAll("case ");
                for (case.values, 0..) |val, i| {
                    if (i > 0) try out.writeAll(",");
                    try dumpExpr(out, val);
                }
                try out.writeAll(":\n");
                try dumpBlock(out, case.body, indent + 2);
            }
            if (sw.default_block) |db| {
                try writeIndent(out, indent + 1);
                try out.writeAll("default:\n");
                try dumpBlock(out, db, indent + 2);
            }
            try writeIndent(out, indent);
            try out.writeAll(")\n");
        },
        .function_decl => |fd| {
            try out.writeAll("(function-decl ");
            for (fd.name_path, 0..) |seg, i| {
                if (i > 0) try out.writeAll(if (i == fd.name_path.len - 1 and fd.is_method) ":" else ".");
                try out.writeAll(seg);
            }
            try out.writeAll("(");
            try dumpParams(out, fd.func.params, fd.func.has_vararg);
            try out.writeAll(")");
            if (fd.func.return_type) |rt| {
                try out.writeAll(":");
                try dumpType(out, rt);
            }
            try out.writeAll("\n");
            try dumpBlock(out, fd.func.body, indent + 1);
            try writeIndent(out, indent);
            try out.writeAll(")\n");
        },
        .local_function => |lf| {
            try out.writeAll("(local-function ");
            try out.writeAll(lf.name);
            try out.writeAll("(");
            try dumpParams(out, lf.func.params, lf.func.has_vararg);
            try out.writeAll(")");
            if (lf.func.return_type) |rt| {
                try out.writeAll(":");
                try dumpType(out, rt);
            }
            try out.writeAll("\n");
            try dumpBlock(out, lf.func.body, indent + 1);
            try writeIndent(out, indent);
            try out.writeAll(")\n");
        },
        .return_stmt => |r| {
            try out.writeAll("(return");
            for (r.values) |val| {
                try out.writeAll(" ");
                try dumpExpr(out, val);
            }
            try out.writeAll(")\n");
        },
        .break_stmt => |bs| {
            if (bs.level == 1) try out.writeAll("(break)\n")
            else try out.print("(break {})\n", .{bs.level});
        },
        .continue_stmt => |cs| {
            if (cs.level == 1) try out.writeAll("(continue)\n")
            else try out.print("(continue {})\n", .{cs.level});
        },
        .expr_stmt => |e| {
            try out.writeAll("(stmt ");
            try dumpExpr(out, e);
            try out.writeAll(")\n");
        },
    }
}

pub fn dumpExpr(out: anytype, e: *const Expr) anyerror!void {
    switch (e.*) {
        .nil => try out.writeAll("nil"),
        .boolean => |b| try out.writeAll(if (b) "true" else "false"),
        .integer => |i| try out.print("{}", .{i}),
        .number => |f| try out.print("{d}", .{f}),
        .string => |s| try out.print("\"{s}\"", .{s}),
        .ident => |s| try out.writeAll(s),
        .vararg => try out.writeAll("..."),
        .binary => |b| {
            try out.writeAll("(");
            try out.writeAll(b.op.lexeme());
            try out.writeAll(" ");
            try dumpExpr(out, b.lhs);
            try out.writeAll(" ");
            try dumpExpr(out, b.rhs);
            try out.writeAll(")");
        },
        .unary => |u| {
            try out.writeAll("(");
            try out.writeAll(u.op.lexeme());
            try out.writeAll(" ");
            try dumpExpr(out, u.operand);
            try out.writeAll(")");
        },
        .index => |idx| {
            try dumpExpr(out, idx.object);
            try out.writeAll("[");
            try dumpExpr(out, idx.key);
            try out.writeAll("]");
        },
        .field => |f| {
            try dumpExpr(out, f.object);
            try out.writeAll(".");
            try out.writeAll(f.name);
        },
        .call => |c| {
            try dumpExpr(out, c.callee);
            try out.writeAll("(");
            for (c.args, 0..) |a, i| {
                if (i > 0) try out.writeAll(",");
                try dumpExpr(out, a);
            }
            try out.writeAll(")");
        },
        .new_expr => |ne| {
            try out.writeAll("(new ");
            try dumpExpr(out, ne.class);
            for (ne.args) |a| {
                try out.writeAll(" ");
                try dumpExpr(out, a);
            }
            try out.writeAll(")");
        },
        .ternary => |t| {
            try out.writeAll("(?: ");
            try dumpExpr(out, t.cond);
            try out.writeAll(" ");
            try dumpExpr(out, t.then_expr);
            try out.writeAll(" ");
            try dumpExpr(out, t.else_expr);
            try out.writeAll(")");
        },
        .method_call => |mc| {
            try dumpExpr(out, mc.receiver);
            try out.writeAll(":");
            try out.writeAll(mc.method);
            try out.writeAll("(");
            for (mc.args, 0..) |a, i| {
                if (i > 0) try out.writeAll(",");
                try dumpExpr(out, a);
            }
            try out.writeAll(")");
        },
        .function => |f| {
            try out.writeAll("function(");
            try dumpParams(out, f.params, f.has_vararg);
            try out.writeAll(") ...end");
        },
        .table => |t| {
            try out.writeAll("{");
            for (t.fields, 0..) |fld, i| {
                if (i > 0) try out.writeAll(",");
                if (fld.key) |k| switch (k) {
                    .named => |n| {
                        try out.writeAll(n);
                        try out.writeAll("=");
                    },
                    .computed => |ce| {
                        try out.writeAll("[");
                        try dumpExpr(out, ce);
                        try out.writeAll("]=");
                    },
                };
                try dumpExpr(out, fld.value);
            }
            try out.writeAll("}");
        },
    }
}

fn dumpParams(out: anytype, params: []const NameWithType, has_vararg: bool) !void {
    for (params, 0..) |p, i| {
        if (i > 0) try out.writeAll(",");
        try out.writeAll(p.name);
        if (p.type_annot) |ta| {
            try out.writeAll(":");
            try dumpType(out, ta);
        }
    }
    if (has_vararg) {
        if (params.len > 0) try out.writeAll(",");
        try out.writeAll("...");
    }
}

pub fn dumpType(out: anytype, t: TypeExpr) anyerror!void {
    switch (t) {
        .atom => |a| try out.writeAll(a.name()),
        .optional => |inner| {
            try dumpType(out, inner.*);
            try out.writeAll("?");
        },
    }
}

fn writeIndent(out: anytype, indent: u32) !void {
    var i: u32 = 0;
    while (i < indent) : (i += 1) try out.writeAll("  ");
}
