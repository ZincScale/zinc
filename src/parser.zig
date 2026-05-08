//! Parser for Lua 5.4 syntax → AST.
//!
//! Pratt parser for expressions (operator precedence done by lookup
//! tables, no separate precedence-climbing functions per level),
//! recursive descent for statements. AST nodes are arena-allocated
//! by the caller's `std.mem.Allocator`.
//!
//! Phase 3.1.0 covers the common 70% of Lua: literals, all binary +
//! unary operators, function calls (parens form), table constructors,
//! local + assignment, if/elseif/else, while, function declarations,
//! return, break, function-call as statement.
//!
//! Deferred to 3.1.1: numeric and generic `for`, `repeat/until`,
//! `goto/label`, `do/end` blocks, method calls (`a:b()`), no-paren
//! string/table calls (`print"hi"`), full string-escape unescape.
//! Pluto-specific syntax (class, switch, ternary, compound ops) is
//! Phase 3.3.

const std = @import("std");
const lexer = @import("lexer.zig");
const ast = @import("ast.zig");

const Token = lexer.Token;
const TokenKind = lexer.TokenKind;

pub const ParseError = error{
    ExpectedExpr,
    ExpectedStmt,
    ExpectedToken,
    UnexpectedToken,
    InvalidAssignmentTarget,
    InvalidNumber,
    /// strict-Pluto: rejected Lua-equivalent form when a Pluto form
    /// exists. The error message points the user at the canonical
    /// Pluto syntax.
    StrictPlutoViolation,
    OutOfMemory,
} || lexer.LexerError;

pub const Parser = struct {
    src: []const u8,
    lex: lexer.Lexer,
    arena: std.mem.Allocator,
    cur: Token,
    /// 1-token lookahead. Many constructs disambiguate from the
    /// second token (e.g. `name = ...` vs `name(...)` vs `name.x = ...`).
    next_tok: Token,

    pub fn init(arena: std.mem.Allocator, src: []const u8) ParseError!Parser {
        var l = lexer.Lexer.init(src);
        const t0 = try l.next();
        const t1 = try l.next();
        return .{ .src = src, .lex = l, .arena = arena, .cur = t0, .next_tok = t1 };
    }

    /// Parse a top-level chunk. The chunk is just a block followed by EOF.
    pub fn parseChunk(self: *Parser) ParseError!*ast.Block {
        const b = try self.parseBlock();
        if (self.cur.kind != .eof) return error.UnexpectedToken;
        return b;
    }

    // --- driver helpers --------------------------------------------------

    fn advance(self: *Parser) ParseError!void {
        self.cur = self.next_tok;
        self.next_tok = try self.lex.next();
    }

    fn expect(self: *Parser, kind: TokenKind) ParseError!void {
        if (self.cur.kind != kind) return error.ExpectedToken;
        try self.advance();
    }

    fn expectKeyword(self: *Parser, kw: []const u8) ParseError!void {
        if (!self.isKeyword(kw)) return error.ExpectedToken;
        try self.advance();
    }

    fn isKeyword(self: *const Parser, kw: []const u8) bool {
        if (self.cur.kind != .ident) return false;
        return std.mem.eql(u8, self.cur.lexeme(self.src), kw);
    }

    fn isAnyKeyword(self: *const Parser, kws: []const []const u8) bool {
        for (kws) |kw| if (self.isKeyword(kw)) return true;
        return false;
    }

    fn alloc(self: *Parser, comptime T: type, value: T) ParseError!*T {
        const ptr = try self.arena.create(T);
        ptr.* = value;
        return ptr;
    }

    // --- block & statements ---------------------------------------------

    fn parseBlock(self: *Parser) ParseError!*ast.Block {
        var stmts = std.ArrayList(ast.Stmt){ .items = &.{}, .capacity = 0 };
        while (!self.atBlockEnd()) {
            // Skip standalone semicolons.
            if (self.cur.kind == .semicolon) {
                try self.advance();
                continue;
            }
            const s = try self.parseStmt();
            try stmts.append(self.arena, s);
            // `return` must be the last statement in a block — Lua 5.4
            // grammar requires it, prevents dead code after return.
            if (s == .return_stmt) break;
        }
        return self.alloc(ast.Block, .{ .stmts = try stmts.toOwnedSlice(self.arena) });
    }

    fn atBlockEnd(self: *const Parser) bool {
        if (self.cur.kind == .eof) return true;
        // Block-terminating keywords. `until` is for repeat/until
        // (deferred); leaving it in place so that future grammar
        // additions don't require re-touching this list.
        return self.isAnyKeyword(&.{ "end", "else", "elseif", "until" });
    }

    fn parseStmt(self: *Parser) ParseError!ast.Stmt {
        if (self.isKeyword("local")) return self.parseLocal();
        if (self.isKeyword("if")) return self.parseIf();
        if (self.isKeyword("while")) return self.parseWhile();
        if (self.isKeyword("function")) return self.parseFunctionDecl();
        if (self.isKeyword("return")) return self.parseReturn();
        if (self.isKeyword("break")) {
            try self.advance();
            return ast.Stmt{ .break_stmt = {} };
        }
        // Otherwise: an expression-starting form. Either a function
        // call (which is a statement) or an assignment (lhs = rhs).
        return self.parseExprStmt();
    }

    fn parseLocal(self: *Parser) ParseError!ast.Stmt {
        try self.expectKeyword("local");
        // `local function name(...) body end` is its own statement
        // form because it allows the name to be in scope inside the
        // body (recursive local functions).
        if (self.isKeyword("function")) {
            try self.advance();
            const name = try self.expectIdentLexeme();
            const func = try self.parseFunctionBody();
            return ast.Stmt{ .local_function = .{ .name = name, .func = func } };
        }

        // local namelist [= exprlist]
        var names = std.ArrayList([]const u8){ .items = &.{}, .capacity = 0 };
        try names.append(self.arena, try self.expectIdentLexeme());
        while (self.cur.kind == .comma) {
            try self.advance();
            try names.append(self.arena, try self.expectIdentLexeme());
        }

        var values = std.ArrayList(*ast.Expr){ .items = &.{}, .capacity = 0 };
        if (self.cur.kind == .eq) {
            try self.advance();
            try self.parseExprList(&values);
        }

        return ast.Stmt{ .local = .{
            .names = try names.toOwnedSlice(self.arena),
            .values = try values.toOwnedSlice(self.arena),
        } };
    }

    fn parseIf(self: *Parser) ParseError!ast.Stmt {
        try self.expectKeyword("if");
        var branches = std.ArrayList(ast.Stmt.Branch){ .items = &.{}, .capacity = 0 };
        const cond0 = try self.parseExpr();
        try self.expectKeyword("then");
        const body0 = try self.parseBlock();
        try branches.append(self.arena, .{ .cond = cond0, .body = body0 });

        while (self.isKeyword("elseif")) {
            try self.advance();
            const cond = try self.parseExpr();
            try self.expectKeyword("then");
            const body = try self.parseBlock();
            try branches.append(self.arena, .{ .cond = cond, .body = body });
        }

        var else_block: ?*ast.Block = null;
        if (self.isKeyword("else")) {
            try self.advance();
            else_block = try self.parseBlock();
        }

        try self.expectKeyword("end");
        return ast.Stmt{ .if_stmt = .{
            .branches = try branches.toOwnedSlice(self.arena),
            .else_block = else_block,
        } };
    }

    fn parseWhile(self: *Parser) ParseError!ast.Stmt {
        try self.expectKeyword("while");
        const cond = try self.parseExpr();
        try self.expectKeyword("do");
        const body = try self.parseBlock();
        try self.expectKeyword("end");
        return ast.Stmt{ .while_stmt = .{ .cond = cond, .body = body } };
    }

    fn parseFunctionDecl(self: *Parser) ParseError!ast.Stmt {
        try self.expectKeyword("function");
        var path = std.ArrayList([]const u8){ .items = &.{}, .capacity = 0 };
        try path.append(self.arena, try self.expectIdentLexeme());
        var is_method = false;
        while (true) {
            if (self.cur.kind == .dot) {
                try self.advance();
                try path.append(self.arena, try self.expectIdentLexeme());
            } else if (self.cur.kind == .colon) {
                try self.advance();
                try path.append(self.arena, try self.expectIdentLexeme());
                is_method = true;
                break; // method receiver only allowed as last segment
            } else break;
        }
        const func = try self.parseFunctionBody();
        return ast.Stmt{ .function_decl = .{
            .name_path = try path.toOwnedSlice(self.arena),
            .is_method = is_method,
            .func = func,
        } };
    }

    fn parseReturn(self: *Parser) ParseError!ast.Stmt {
        try self.expectKeyword("return");
        var values = std.ArrayList(*ast.Expr){ .items = &.{}, .capacity = 0 };
        // `return` with no values is legal (returns nothing).
        if (!self.atBlockEnd() and self.cur.kind != .semicolon) {
            try self.parseExprList(&values);
        }
        if (self.cur.kind == .semicolon) try self.advance();
        return ast.Stmt{ .return_stmt = .{ .values = try values.toOwnedSlice(self.arena) } };
    }

    /// Statement that starts with a primary expression: either a
    /// function-call statement or an assignment. Lua disambiguates
    /// by what follows the prefix expression: `=` or `,` → assignment,
    /// otherwise it must be a call.
    fn parseExprStmt(self: *Parser) ParseError!ast.Stmt {
        const first = try self.parseSuffixed();

        // Compound assignment: target += value, target -= value, etc.
        // Desugars to ordinary assign with a synthesized binary expr.
        // strict-Pluto: this is the *required* form for any
        // self-modifying assignment; `target = target + value` is
        // rejected by detectCompoundViolation below.
        if (compoundOpFor(self.cur.kind)) |bop| {
            if (!isAssignTarget(first.*)) return error.InvalidAssignmentTarget;
            try self.advance();
            const rhs = try self.parseExpr();
            const synth = try self.alloc(ast.Expr, .{ .binary = .{ .op = bop, .lhs = first, .rhs = rhs } });
            const targets = try self.arena.alloc(*ast.Expr, 1);
            targets[0] = first;
            const values = try self.arena.alloc(*ast.Expr, 1);
            values[0] = synth;
            return ast.Stmt{ .assign = .{ .targets = targets, .values = values } };
        }

        if (self.cur.kind == .eq or self.cur.kind == .comma) {
            // Assignment. Targets must be lvalues (ident / index / field).
            if (!isAssignTarget(first.*)) return error.InvalidAssignmentTarget;
            var targets = std.ArrayList(*ast.Expr){ .items = &.{}, .capacity = 0 };
            try targets.append(self.arena, first);
            while (self.cur.kind == .comma) {
                try self.advance();
                const t = try self.parseSuffixed();
                if (!isAssignTarget(t.*)) return error.InvalidAssignmentTarget;
                try targets.append(self.arena, t);
            }
            try self.expect(.eq);
            var values = std.ArrayList(*ast.Expr){ .items = &.{}, .capacity = 0 };
            try self.parseExprList(&values);

            // Note: `x = x + 1` stays valid — compound ops are an
            // additive ergonomic, not a forced canonicalization. See
            // the design discussion in the strict-Pluto plan.

            return ast.Stmt{ .assign = .{
                .targets = try targets.toOwnedSlice(self.arena),
                .values = try values.toOwnedSlice(self.arena),
            } };
        }

        // Otherwise it must be a function-call statement.
        if (first.* != .call) return error.ExpectedStmt;
        return ast.Stmt{ .expr_stmt = first };
    }

    fn isAssignTarget(e: ast.Expr) bool {
        return switch (e) {
            .ident, .index, .field => true,
            else => false,
        };
    }

    fn parseExprList(self: *Parser, list: *std.ArrayList(*ast.Expr)) ParseError!void {
        try list.append(self.arena, try self.parseExpr());
        while (self.cur.kind == .comma) {
            try self.advance();
            try list.append(self.arena, try self.parseExpr());
        }
    }

    fn expectIdentLexeme(self: *Parser) ParseError![]const u8 {
        if (self.cur.kind != .ident) return error.ExpectedToken;
        // Reserved keywords are not valid identifier names. The
        // simple way: rely on the parser's keyword checks at every
        // syntactically meaningful spot; we don't reject keywords-
        // as-names at lex time because the parser may want to use
        // them contextually (Pluto's `class` etc.).
        const text = self.cur.lexeme(self.src);
        try self.advance();
        return text;
    }

    fn parseFunctionBody(self: *Parser) ParseError!ast.Expr.Function {
        try self.expect(.lparen);
        var params = std.ArrayList([]const u8){ .items = &.{}, .capacity = 0 };
        var has_vararg = false;
        if (self.cur.kind != .rparen) {
            while (true) {
                if (self.cur.kind == .dot_dot_dot) {
                    has_vararg = true;
                    try self.advance();
                    break;
                }
                try params.append(self.arena, try self.expectIdentLexeme());
                if (self.cur.kind != .comma) break;
                try self.advance();
            }
        }
        try self.expect(.rparen);
        const body = try self.parseBlock();
        try self.expectKeyword("end");
        return .{
            .params = try params.toOwnedSlice(self.arena),
            .has_vararg = has_vararg,
            .body = body,
        };
    }

    // --- expressions (Pratt) --------------------------------------------

    pub fn parseExpr(self: *Parser) ParseError!*ast.Expr {
        return self.parseExprPrec(0);
    }

    /// Pratt expression parser. Precedence + associativity table is
    /// in binPrec/binAssoc below. Mirrors Lua 5.4's expression
    /// grammar exactly (Lua reference manual §3.4.8).
    fn parseExprPrec(self: *Parser, min_prec: u8) ParseError!*ast.Expr {
        var lhs = try self.parseUnaryOrSuffixed();

        while (true) {
            // strict-Pluto: `~=` in expression context is rejected. The
            // canonical inequality is `!=`. `~=` is reserved for the
            // compound XOR-assign statement form (handled in parseExprStmt).
            if (self.cur.kind == .tilde_eq) {
                return self.rejectLuaForm("`~=` for inequality is not allowed — use `!=` instead (`~=` is the compound XOR-assign in strict-Pluto)");
            }
            const op = self.peekBinaryOp() orelse break;
            const p = binPrec(op);
            if (p < min_prec) break;
            try self.advance();
            const next_min = if (binIsRightAssoc(op)) p else p + 1;
            const rhs = try self.parseExprPrec(next_min);
            lhs = try self.alloc(ast.Expr, .{ .binary = .{ .op = op, .lhs = lhs, .rhs = rhs } });
        }
        return lhs;
    }

    fn parseUnaryOrSuffixed(self: *Parser) ParseError!*ast.Expr {
        // strict-Pluto: reject `not` keyword. The canonical Pluto
        // form is `!`. (Allows compound assignment / arithmetic to
        // work normally — only the boolean operator is locked down.)
        if (self.cur.kind == .ident and std.mem.eql(u8, self.cur.lexeme(self.src), "not")) {
            return self.rejectLuaForm("the `not` keyword is not allowed — use `!` instead");
        }
        // Unary operators: -, !, #, ~. Right-associative at precedence 12.
        if (self.peekUnaryOp()) |op| {
            try self.advance();
            const operand = try self.parseExprPrec(unaryPrec());
            return self.alloc(ast.Expr, .{ .unary = .{ .op = op, .operand = operand } });
        }
        return self.parseSuffixed();
    }

    fn peekBinaryOp(self: *const Parser) ?ast.BinaryOp {
        return switch (self.cur.kind) {
            .plus => .add,           .minus => .sub,
            .star => .mul,           .slash => .div,
            .slash_slash => .idiv,   .percent => .mod,
            .caret => .pow,          .amp => .band,
            .pipe => .bor,           .tilde => .bxor,
            .less_less => .shl,      .greater_greater => .shr,
            .dot_dot => .concat,
            .eq_eq => .eq,
            .bang_eq => .neq, // canonical Pluto inequality
            // .tilde_eq is NOT inequality — it's the compound XOR-assign
            // (statement form, handled by parseExprStmt). If it shows
            // up in expression position, parseExprPrec rejects it.
            .less => .lt,            .less_eq => .lte,
            .greater => .gt,         .greater_eq => .gte,
            .ident => blk: {
                const text = self.cur.lexeme(self.src);
                if (std.mem.eql(u8, text, "and")) break :blk .and_;
                if (std.mem.eql(u8, text, "or")) break :blk .or_;
                break :blk null;
            },
            else => null,
        };
    }

    fn peekUnaryOp(self: *const Parser) ?ast.UnaryOp {
        return switch (self.cur.kind) {
            .minus => .neg,
            .hash => .len,
            .tilde => .bnot,
            .bang => .not_, // canonical Pluto not
            // `not` keyword is rejected at parseUnaryOrSuffixed's entry
            else => null,
        };
    }

    /// strict-Pluto: detect Lua-equivalent forms we reject. Each
    /// returns true if it emitted (well, set up to emit) the
    /// rejection. Caller bails with StrictPlutoViolation. We pair the
    /// detection with diagnostic messages logged via std.debug.print
    /// so the test harness can grep them; phase 4.x will route through
    /// a proper diagnostic sink.
    fn rejectLuaForm(_: *Parser, comptime hint: []const u8) ParseError {
        std.debug.print("strict-pluto: {s}\n", .{hint});
        return error.StrictPlutoViolation;
    }

    /// Suffixed expression: a primary followed by any number of
    /// `.name`, `[expr]`, or `(args)` chained on. Left-associative.
    fn parseSuffixed(self: *Parser) ParseError!*ast.Expr {
        var e = try self.parsePrimary();
        while (true) {
            switch (self.cur.kind) {
                .dot => {
                    try self.advance();
                    const name = try self.expectIdentLexeme();
                    e = try self.alloc(ast.Expr, .{ .field = .{ .object = e, .name = name } });
                },
                .lbracket => {
                    try self.advance();
                    const key = try self.parseExpr();
                    try self.expect(.rbracket);
                    e = try self.alloc(ast.Expr, .{ .index = .{ .object = e, .key = key } });
                },
                .lparen => {
                    try self.advance();
                    var args = std.ArrayList(*ast.Expr){ .items = &.{}, .capacity = 0 };
                    if (self.cur.kind != .rparen) try self.parseExprList(&args);
                    try self.expect(.rparen);
                    e = try self.alloc(ast.Expr, .{ .call = .{
                        .callee = e,
                        .args = try args.toOwnedSlice(self.arena),
                    } });
                },
                else => break,
            }
        }
        return e;
    }

    fn parsePrimary(self: *Parser) ParseError!*ast.Expr {
        const t = self.cur;
        switch (t.kind) {
            .ident => {
                const text = t.lexeme(self.src);
                if (std.mem.eql(u8, text, "nil")) {
                    try self.advance();
                    return self.alloc(ast.Expr, .{ .nil = {} });
                }
                if (std.mem.eql(u8, text, "true")) {
                    try self.advance();
                    return self.alloc(ast.Expr, .{ .boolean = true });
                }
                if (std.mem.eql(u8, text, "false")) {
                    try self.advance();
                    return self.alloc(ast.Expr, .{ .boolean = false });
                }
                if (std.mem.eql(u8, text, "function")) {
                    try self.advance();
                    const f = try self.parseFunctionBody();
                    return self.alloc(ast.Expr, .{ .function = f });
                }
                // Plain identifier reference.
                try self.advance();
                return self.alloc(ast.Expr, .{ .ident = text });
            },
            .int_lit => {
                try self.advance();
                const text = t.lexeme(self.src);
                const n = parseIntLiteral(text) orelse return error.InvalidNumber;
                return self.alloc(ast.Expr, .{ .integer = n });
            },
            .float_lit => {
                try self.advance();
                const text = t.lexeme(self.src);
                const f = std.fmt.parseFloat(f64, text) catch return error.InvalidNumber;
                return self.alloc(ast.Expr, .{ .number = f });
            },
            .string_lit => {
                try self.advance();
                // Strip the surrounding quotes / long-bracket markers.
                // Phase 3.1 doesn't expand escapes — that's a later
                // refinement; the bytecode emitter or constant table
                // builder will handle it.
                const raw = t.lexeme(self.src);
                const inner = stripStringDelimiters(raw);
                return self.alloc(ast.Expr, .{ .string = inner });
            },
            .lparen => {
                try self.advance();
                const inner = try self.parseExpr();
                try self.expect(.rparen);
                return inner;
            },
            .lbrace => return self.parseTable(),
            .dot_dot_dot => {
                try self.advance();
                return self.alloc(ast.Expr, .{ .vararg = {} });
            },
            else => return error.ExpectedExpr,
        }
    }

    fn parseTable(self: *Parser) ParseError!*ast.Expr {
        try self.expect(.lbrace);
        var fields = std.ArrayList(ast.Expr.TableField){ .items = &.{}, .capacity = 0 };
        while (self.cur.kind != .rbrace) {
            const fld = try self.parseTableField();
            try fields.append(self.arena, fld);
            // Lua allows `,` or `;` as separators, both equivalent.
            if (self.cur.kind == .comma or self.cur.kind == .semicolon) {
                try self.advance();
            } else break;
        }
        try self.expect(.rbrace);
        return self.alloc(ast.Expr, .{ .table = .{
            .fields = try fields.toOwnedSlice(self.arena),
        } });
    }

    fn parseTableField(self: *Parser) ParseError!ast.Expr.TableField {
        // [expr] = value   (computed key)
        if (self.cur.kind == .lbracket) {
            try self.advance();
            const key = try self.parseExpr();
            try self.expect(.rbracket);
            try self.expect(.eq);
            const value = try self.parseExpr();
            return .{ .key = .{ .computed = key }, .value = value };
        }
        // name = value   (named key) — but only if the next token is `=`,
        // otherwise it's a positional ident expression.
        if (self.cur.kind == .ident and self.next_tok.kind == .eq) {
            const name = self.cur.lexeme(self.src);
            try self.advance();
            try self.expect(.eq);
            const value = try self.parseExpr();
            return .{ .key = .{ .named = name }, .value = value };
        }
        // Otherwise a positional value (no key).
        const v = try self.parseExpr();
        return .{ .key = null, .value = v };
    }
};

// =============================================================================
// strict-Pluto: compound-op + redundant-self-assign detection
// =============================================================================

fn compoundOpFor(kind: lexer.TokenKind) ?ast.BinaryOp {
    return switch (kind) {
        .plus_eq => .add,
        .minus_eq => .sub,
        .star_eq => .mul,
        .slash_eq => .div,
        .slash_slash_eq => .idiv,
        .percent_eq => .mod,
        .caret_eq => .pow,
        .amp_eq => .band,
        .pipe_eq => .bor,
        // tilde_eq is the compound XOR-assign in strict-Pluto. The
        // expression-context inequality use is rejected separately.
        .tilde_eq => .bxor,
        .less_less_eq => .shl,
        .greater_greater_eq => .shr,
        .dot_dot_eq => .concat,
        else => null,
    };
}

/// Returns the binary-op lexeme if `value` is `target OP something`
/// where `target` and `value.lhs` refer to the same syntactic place
/// (currently only ident comparisons; field/index comparison is
/// future work because it'd need to compare full path expressions).
fn detectCompoundViolation(target: *const ast.Expr, value: *const ast.Expr) ParseError!?[]const u8 {
    if (target.* != .ident) return null;
    if (value.* != .binary) return null;
    const b = value.binary;
    if (b.lhs.* != .ident) return null;
    if (!std.mem.eql(u8, target.ident, b.lhs.ident)) return null;
    return b.op.lexeme();
}

fn targetText(e: *const ast.Expr) []const u8 {
    return switch (e.*) {
        .ident => |s| s,
        else => "<target>",
    };
}

// =============================================================================
// Operator precedence + associativity (Lua 5.4 reference §3.4.8)
// =============================================================================

fn binPrec(op: ast.BinaryOp) u8 {
    return switch (op) {
        .or_ => 1,
        .and_ => 2,
        .lt, .gt, .lte, .gte, .eq, .neq => 3,
        .bor => 4,
        .bxor => 5,
        .band => 6,
        .shl, .shr => 7,
        .concat => 9, // right-associative; sits between + and *
        .add, .sub => 10,
        .mul, .div, .idiv, .mod => 11,
        // unary at 12
        .pow => 14, // above unary; right-associative
    };
}

fn binIsRightAssoc(op: ast.BinaryOp) bool {
    return switch (op) {
        .concat, .pow => true,
        else => false,
    };
}

fn unaryPrec() u8 {
    return 12;
}

// =============================================================================
// Helpers
// =============================================================================

fn parseIntLiteral(text: []const u8) ?i64 {
    if (text.len > 2 and text[0] == '0' and (text[1] == 'x' or text[1] == 'X')) {
        return std.fmt.parseInt(i64, text[2..], 16) catch null;
    }
    return std.fmt.parseInt(i64, text, 10) catch null;
}

fn stripStringDelimiters(raw: []const u8) []const u8 {
    if (raw.len < 2) return raw;
    if (raw[0] == '"' or raw[0] == '\'') return raw[1 .. raw.len - 1];
    if (raw[0] == '[') {
        // Long bracket: skip past `[=*[` and trailing `]=*]`.
        var i: usize = 1;
        while (i < raw.len and raw[i] == '=') i += 1;
        std.debug.assert(raw[i] == '[');
        i += 1;
        const end = raw.len - i;
        return raw[i..end];
    }
    return raw;
}

// =============================================================================
// Tests
// =============================================================================

const testing = std.testing;

fn parseAndDump(arena: std.mem.Allocator, src: []const u8) ![]const u8 {
    var p = try Parser.init(arena, src);
    const block = try p.parseChunk();
    var aw: std.Io.Writer.Allocating = .init(arena);
    try ast.dumpBlock(&aw.writer, block, 0);
    return arena.dupe(u8, aw.writer.buffered());
}

test "parse: literal expressions" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "return 1, 2.5, true, nil, \"hi\", x");
    try testing.expectEqualStrings("(return 1 2.5 true nil \"hi\" x)\n", out);
}

test "parse: arithmetic precedence" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // 1 + 2 * 3 → +(1, *(2, 3)) — multiplication binds tighter
    const out = try parseAndDump(arena, "return 1 + 2 * 3");
    try testing.expectEqualStrings("(return (+ 1 (* 2 3)))\n", out);
}

test "parse: power right-associative" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // 2 ^ 3 ^ 2 → ^(2, ^(3, 2))
    const out = try parseAndDump(arena, "return 2 ^ 3 ^ 2");
    try testing.expectEqualStrings("(return (^ 2 (^ 3 2)))\n", out);
}

test "parse: concat right-associative" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "return \"a\" .. \"b\" .. \"c\"");
    try testing.expectEqualStrings("(return (.. \"a\" (.. \"b\" \"c\")))\n", out);
}

test "parse: comparison and logical" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "return a < b and b < c or d");
    try testing.expectEqualStrings("(return (or (and (< a b) (< b c)) d))\n", out);
}

test "parse: unary (strict-Pluto: ! not `not`)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "return -1 + #t + !flag");
    try testing.expectEqualStrings("(return (+ (+ (- 1) (# t)) (not flag)))\n", out);
}

test "parse: `not` keyword is rejected — use !" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    var p = try Parser.init(arena, "return not x");
    try testing.expectError(error.StrictPlutoViolation, p.parseChunk());
}

test "parse: `~=` for inequality is rejected — use !=" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    var p = try Parser.init(arena, "return a ~= b");
    try testing.expectError(error.StrictPlutoViolation, p.parseChunk());
}

test "parse: != for inequality" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "return a != b");
    try testing.expectEqualStrings("(return (~= a b))\n", out);
}

test "parse: `~=` as compound XOR-assign in statement form (still allowed)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // `x ~= 5` at statement position is the compound XOR-assign:
    // x = x ~ 5. Disambiguated from inequality by being a statement,
    // not an expression context.
    const out = try parseAndDump(arena, "x ~= 5");
    try testing.expectEqualStrings("(assign x = (~ x 5))\n", out);
}

test "parse: compound assignment" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Compound op desugars at parse time to assign + binary expr.
    const out = try parseAndDump(arena, "x += 1");
    try testing.expectEqualStrings("(assign x = (+ x 1))\n", out);
}

test "parse: `x = x + 1` is still valid (compound ops are additive, not forced)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "x = x + 1");
    try testing.expectEqualStrings("(assign x = (+ x 1))\n", out);
}

test "parse: function call chain" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "return f(1)(2).x[3]");
    try testing.expectEqualStrings("(return f(1)(2).x[3])\n", out);
}

test "parse: local declaration" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "local x = 5\nlocal a, b = 1, 2\nlocal c");
    try testing.expectEqualStrings("(local x = 5)\n(local a,b = 1,2)\n(local c)\n", out);
}

test "parse: assignment" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "x = 1\ny.z = 2\nt[1] = 3\na, b = 1, 2");
    try testing.expectEqualStrings(
        \\(assign x = 1)
        \\(assign y.z = 2)
        \\(assign t[1] = 3)
        \\(assign a,b = 1,2)
        \\
    , out);
}

test "parse: if/elseif/else" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "if x then return 1 elseif y then return 2 else return 3 end");
    try testing.expectEqualStrings(
        \\(if
        \\  if x then
        \\    (return 1)
        \\  elseif y then
        \\    (return 2)
        \\  else
        \\    (return 3)
        \\)
        \\
    , out);
}

test "parse: while loop (strict-Pluto: x += 1 not x = x + 1)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "while x < 10 do x += 1 end");
    try testing.expectEqualStrings(
        \\(while (< x 10)
        \\  (assign x = (+ x 1))
        \\)
        \\
    , out);
}

test "parse: function declaration" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "function fib(n) if n < 2 then return n end return fib(n-1) + fib(n-2) end");
    try testing.expectEqualStrings(
        \\(function-decl fib(n)
        \\  (if
        \\    if (< n 2) then
        \\      (return n)
        \\  )
        \\  (return (+ fib((- n 1)) fib((- n 2))))
        \\)
        \\
    , out);
}

test "parse: local function" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "local function f(x, y) return x + y end");
    try testing.expectEqualStrings(
        \\(local-function f(x,y)
        \\  (return (+ x y))
        \\)
        \\
    , out);
}

test "parse: function expression" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "local f = function(x) return x * 2 end");
    try testing.expectEqualStrings("(local f = function(x) ...end)\n", out);
}

test "parse: table constructor" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "local t = {1, 2, name = \"alice\", [k] = v, 3}");
    try testing.expectEqualStrings("(local t = {1,2,name=\"alice\",[k]=v,3})\n", out);
}

test "parse: function call statement" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "print(\"hello\")");
    try testing.expectEqualStrings("(stmt print(\"hello\"))\n", out);
}

test "parse: break" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    const out = try parseAndDump(arena, "while true do break end");
    try testing.expectEqualStrings(
        \\(while true
        \\  (break)
        \\)
        \\
    , out);
}

test "parse: error - assign to non-lvalue (call result)" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    // Function-call results are not assignable. (Note: `(x) = 1` is
    // technically rejected by Lua's grammar via the var/prefixexp
    // distinction, but our parser flattens parens; we don't enforce
    // that strictness in 3.1.0. `f() = 1` is the unambiguous case.)
    var p = try Parser.init(arena, "f() = 1");
    try testing.expectError(error.InvalidAssignmentTarget, p.parseChunk());
}

test "parse: error - missing expression in `if = end`" {
    var arena_state = std.heap.ArenaAllocator.init(testing.allocator);
    defer arena_state.deinit();
    const arena = arena_state.allocator();

    var p = try Parser.init(arena, "if = end");
    try testing.expectError(error.ExpectedExpr, p.parseChunk());
}
