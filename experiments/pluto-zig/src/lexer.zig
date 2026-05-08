//! Lexer for the Pluto-Lang source language.
//!
//! Targets Lua 5.4 syntax with Pluto's keyword extensions (class,
//! switch, case, default, continue, new). The lexer doesn't take a
//! position on which keywords are reserved vs contextual — that's
//! the parser's job. We emit a `keyword` token kind with the
//! specific keyword as the lexeme, and the parser decides whether to
//! treat it as a reserved word in context.
//!
//! Position tracking: every token carries a byte offset into the
//! source plus its line number (1-based). Column is reconstructable
//! from offset by walking back to the previous newline; the lexer
//! doesn't track it eagerly to keep the hot path tight.

const std = @import("std");

pub const TokenKind = enum {
    // Literals
    int_lit,        // 42, 0xDEAD
    float_lit,      // 3.14, 1.5e10, .5
    string_lit,     // "..." or '...' or [[...]] (long bracket)
    ident,          // identifiers and keywords (parser disambiguates)

    // Single-character punctuation
    plus,           // +
    minus,          // -
    star,           // *
    slash,          // /
    percent,        // %
    caret,          // ^
    hash,           // #
    amp,            // &
    tilde,          // ~
    pipe,           // |
    lparen,         // (
    rparen,         // )
    lbrace,         // {
    rbrace,         // }
    lbracket,       // [
    rbracket,       // ]
    comma,          // ,
    semicolon,      // ;
    colon,          // :
    dot,            // .

    // Multi-character operators
    slash_slash,    // //  (integer division)
    less,           // <
    less_eq,        // <=
    less_less,      // <<  (bit shift left)
    greater,        // >
    greater_eq,     // >=
    greater_greater,// >>  (bit shift right)
    eq,             // =
    eq_eq,          // ==
    bang,           // !   (logical not — strict-Pluto)
    bang_eq,        // !=  (inequality — strict-Pluto)
    tilde_eq,       // ~=  (in strict-Pluto: compound XOR assign only;
                    //      Lua/Pluto-superset's inequality is rejected)
    colon_colon,    // ::  (label)
    dot_dot,        // ..  (concat)
    dot_dot_dot,    // ... (varargs)

    // Compound assignment operators (Pluto, strict-mode mandatory)
    plus_eq,            // +=
    minus_eq,           // -=
    star_eq,            // *=
    slash_eq,           // /=
    slash_slash_eq,     // //=
    percent_eq,         // %=
    caret_eq,           // ^=
    amp_eq,             // &=
    pipe_eq,            // |=
    less_less_eq,       // <<=
    greater_greater_eq, // >>=
    dot_dot_eq,         // ..=

    // End of input
    eof,
};

pub const Token = struct {
    kind: TokenKind,
    /// Byte offset into the source where the lexeme starts.
    offset: u32,
    /// Lexeme length in bytes.
    len: u32,
    /// 1-based line number of the lexeme's first character.
    line: u32,

    pub fn lexeme(self: Token, src: []const u8) []const u8 {
        return src[self.offset .. self.offset + self.len];
    }
};

pub const LexerError = error{
    UnterminatedString,
    UnterminatedLongComment,
    InvalidNumber,
    InvalidEscape,
    UnexpectedChar,
};

pub const Lexer = struct {
    src: []const u8,
    pos: u32 = 0,
    line: u32 = 1,

    pub fn init(src: []const u8) Lexer {
        return .{ .src = src };
    }

    /// Read the next token. Returns eof when the source is exhausted.
    pub fn next(self: *Lexer) LexerError!Token {
        try self.skipTrivia();
        if (self.atEnd()) return self.makeToken(.eof, self.pos, 0);

        const start = self.pos;
        const c = self.peek();

        // Identifiers and keywords (we don't disambiguate here — the
        // parser knows which idents are reserved in which contexts).
        if (isIdentStart(c)) {
            while (!self.atEnd() and isIdentCont(self.peek())) self.pos += 1;
            return self.makeToken(.ident, start, self.pos - start);
        }

        // Numbers — both forms (int and float) start with a digit, or
        // a dot followed by a digit (`.5`).
        if (isDigit(c) or (c == '.' and self.pos + 1 < self.src.len and isDigit(self.src[self.pos + 1]))) {
            return self.lexNumber(start);
        }

        // Strings — double, single, or long-bracket form.
        if (c == '"' or c == '\'') return self.lexShortString(start, c);
        if (c == '[' and self.startsLongBracket()) return self.lexLongString(start);

        // Operators and punctuation. The branchy section is unavoidable
        // for a hand-written lexer; clarity wins over micro-optimization.
        return self.lexOperator(start);
    }

    fn skipTrivia(self: *Lexer) LexerError!void {
        while (!self.atEnd()) {
            const c = self.peek();
            switch (c) {
                ' ', '\t', '\r' => self.pos += 1,
                '\n' => {
                    self.line += 1;
                    self.pos += 1;
                },
                '-' => {
                    // `--` starts a comment; bare `-` is the minus operator.
                    if (self.pos + 1 < self.src.len and self.src[self.pos + 1] == '-') {
                        self.pos += 2;
                        // Long comment? `--[[ ... ]]` (and `--[=[...]=]` etc.)
                        if (self.pos < self.src.len and self.peek() == '[' and self.startsLongBracket()) {
                            try self.skipLongComment();
                        } else {
                            // Short comment: to end of line.
                            while (!self.atEnd() and self.peek() != '\n') self.pos += 1;
                        }
                    } else {
                        return; // bare minus
                    }
                },
                else => return,
            }
        }
    }

    /// Long bracket = [ [=*[ — the count of `=` between the brackets
    /// is the level. `[[...]]` is level 0, `[=[...]=]` is level 1, etc.
    /// Returns true if positioned at a level-K opening bracket.
    fn startsLongBracket(self: *const Lexer) bool {
        if (self.pos >= self.src.len or self.src[self.pos] != '[') return false;
        var i: u32 = self.pos + 1;
        while (i < self.src.len and self.src[i] == '=') i += 1;
        return i < self.src.len and self.src[i] == '[';
    }

    /// Returns the level (count of `=`) of a long-bracket opener.
    /// Caller has already verified startsLongBracket.
    fn longBracketLevel(self: *Lexer) u32 {
        std.debug.assert(self.src[self.pos] == '[');
        self.pos += 1; // past first [
        var level: u32 = 0;
        while (!self.atEnd() and self.peek() == '=') : (self.pos += 1) level += 1;
        std.debug.assert(self.peek() == '[');
        self.pos += 1; // past second [
        return level;
    }

    fn skipLongComment(self: *Lexer) LexerError!void {
        const level = self.longBracketLevel();
        try self.skipPastLongClose(level, error.UnterminatedLongComment);
    }

    fn skipPastLongClose(self: *Lexer, level: u32, err_kind: LexerError) LexerError!void {
        while (!self.atEnd()) {
            const c = self.peek();
            if (c == '\n') self.line += 1;
            if (c == ']') {
                // Try to match closing ]=*]
                var i: u32 = self.pos + 1;
                var seen: u32 = 0;
                while (i < self.src.len and self.src[i] == '=') : (i += 1) seen += 1;
                if (i < self.src.len and self.src[i] == ']' and seen == level) {
                    self.pos = i + 1;
                    return;
                }
            }
            self.pos += 1;
        }
        return err_kind;
    }

    fn lexNumber(self: *Lexer, start: u32) LexerError!Token {
        var is_float = false;
        var is_hex = false;

        if (self.peek() == '0' and self.pos + 1 < self.src.len) {
            const n = self.src[self.pos + 1];
            if (n == 'x' or n == 'X') {
                is_hex = true;
                self.pos += 2;
                while (!self.atEnd() and isHexDigit(self.peek())) self.pos += 1;
                // Hex floats (0x1.8p4) — Lua supports them; recognize
                // the `.` and `p` exponent.
                if (!self.atEnd() and self.peek() == '.') {
                    is_float = true;
                    self.pos += 1;
                    while (!self.atEnd() and isHexDigit(self.peek())) self.pos += 1;
                }
                if (!self.atEnd() and (self.peek() == 'p' or self.peek() == 'P')) {
                    is_float = true;
                    self.pos += 1;
                    if (!self.atEnd() and (self.peek() == '+' or self.peek() == '-')) self.pos += 1;
                    while (!self.atEnd() and isDigit(self.peek())) self.pos += 1;
                }
                return self.makeToken(if (is_float) .float_lit else .int_lit, start, self.pos - start);
            }
        }

        // Decimal: digits, optional fraction, optional exponent.
        while (!self.atEnd() and isDigit(self.peek())) self.pos += 1;
        if (!self.atEnd() and self.peek() == '.') {
            // Disambiguate from `..` and `...` operators: only consume
            // the dot if followed by digit / exponent / EOF (so `1..s`
            // is `1` `..` `s`, not `1.` `.s`). Standard Lua rule.
            if (self.pos + 1 >= self.src.len or self.src[self.pos + 1] != '.') {
                is_float = true;
                self.pos += 1;
                while (!self.atEnd() and isDigit(self.peek())) self.pos += 1;
            }
        }
        if (!self.atEnd() and (self.peek() == 'e' or self.peek() == 'E')) {
            is_float = true;
            self.pos += 1;
            if (!self.atEnd() and (self.peek() == '+' or self.peek() == '-')) self.pos += 1;
            if (self.atEnd() or !isDigit(self.peek())) return error.InvalidNumber;
            while (!self.atEnd() and isDigit(self.peek())) self.pos += 1;
        }

        return self.makeToken(if (is_float) .float_lit else .int_lit, start, self.pos - start);
    }

    fn lexShortString(self: *Lexer, start: u32, quote: u8) LexerError!Token {
        self.pos += 1; // past opening quote
        while (!self.atEnd()) {
            const c = self.peek();
            if (c == quote) {
                self.pos += 1;
                return self.makeToken(.string_lit, start, self.pos - start);
            }
            if (c == '\n') return error.UnterminatedString; // short strings can't span lines
            if (c == '\\') {
                self.pos += 1;
                if (self.atEnd()) return error.UnterminatedString;
                // Validate escape against Lua 5.4 set; consume payload
                // bytes for x__/d__/z so they don't trip `quote` match.
                const e = self.peek();
                switch (e) {
                    'a', 'b', 'f', 'n', 'r', 't', 'v', '\\', '"', '\'', '\n' => self.pos += 1,
                    'x' => {
                        self.pos += 1;
                        var i: u32 = 0;
                        while (i < 2 and !self.atEnd() and isHexDigit(self.peek())) : (i += 1) self.pos += 1;
                        if (i == 0) return error.InvalidEscape;
                    },
                    'z' => {
                        self.pos += 1;
                        // \z skips following whitespace
                        while (!self.atEnd() and isSpace(self.peek())) {
                            if (self.peek() == '\n') self.line += 1;
                            self.pos += 1;
                        }
                    },
                    '0', '1', '2', '3', '4', '5', '6', '7', '8', '9' => {
                        var i: u32 = 0;
                        while (i < 3 and !self.atEnd() and isDigit(self.peek())) : (i += 1) self.pos += 1;
                    },
                    else => return error.InvalidEscape,
                }
            } else {
                self.pos += 1;
            }
        }
        return error.UnterminatedString;
    }

    fn lexLongString(self: *Lexer, start: u32) LexerError!Token {
        const level = self.longBracketLevel();
        try self.skipPastLongClose(level, error.UnterminatedString);
        return self.makeToken(.string_lit, start, self.pos - start);
    }

    fn lexOperator(self: *Lexer, start: u32) LexerError!Token {
        const c = self.peek();
        self.pos += 1;
        switch (c) {
            '+' => {
                if (self.match('=')) return self.makeToken(.plus_eq, start, 2);
                return self.makeToken(.plus, start, 1);
            },
            '-' => {
                if (self.match('=')) return self.makeToken(.minus_eq, start, 2);
                return self.makeToken(.minus, start, 1);
            },
            '*' => {
                if (self.match('=')) return self.makeToken(.star_eq, start, 2);
                return self.makeToken(.star, start, 1);
            },
            '%' => {
                if (self.match('=')) return self.makeToken(.percent_eq, start, 2);
                return self.makeToken(.percent, start, 1);
            },
            '^' => {
                if (self.match('=')) return self.makeToken(.caret_eq, start, 2);
                return self.makeToken(.caret, start, 1);
            },
            '#' => return self.makeToken(.hash, start, 1),
            '&' => {
                if (self.match('=')) return self.makeToken(.amp_eq, start, 2);
                return self.makeToken(.amp, start, 1);
            },
            '|' => {
                if (self.match('=')) return self.makeToken(.pipe_eq, start, 2);
                return self.makeToken(.pipe, start, 1);
            },
            '~' => {
                if (self.match('=')) return self.makeToken(.tilde_eq, start, 2);
                return self.makeToken(.tilde, start, 1);
            },
            '!' => {
                if (self.match('=')) return self.makeToken(.bang_eq, start, 2);
                return self.makeToken(.bang, start, 1);
            },
            '(' => return self.makeToken(.lparen, start, 1),
            ')' => return self.makeToken(.rparen, start, 1),
            '{' => return self.makeToken(.lbrace, start, 1),
            '}' => return self.makeToken(.rbrace, start, 1),
            '[' => return self.makeToken(.lbracket, start, 1),
            ']' => return self.makeToken(.rbracket, start, 1),
            ',' => return self.makeToken(.comma, start, 1),
            ';' => return self.makeToken(.semicolon, start, 1),
            '/' => {
                if (self.match('/')) {
                    if (self.match('=')) return self.makeToken(.slash_slash_eq, start, 3);
                    return self.makeToken(.slash_slash, start, 2);
                }
                if (self.match('=')) return self.makeToken(.slash_eq, start, 2);
                return self.makeToken(.slash, start, 1);
            },
            '<' => {
                if (self.match('=')) return self.makeToken(.less_eq, start, 2);
                if (self.match('<')) {
                    if (self.match('=')) return self.makeToken(.less_less_eq, start, 3);
                    return self.makeToken(.less_less, start, 2);
                }
                return self.makeToken(.less, start, 1);
            },
            '>' => {
                if (self.match('=')) return self.makeToken(.greater_eq, start, 2);
                if (self.match('>')) {
                    if (self.match('=')) return self.makeToken(.greater_greater_eq, start, 3);
                    return self.makeToken(.greater_greater, start, 2);
                }
                return self.makeToken(.greater, start, 1);
            },
            '=' => {
                if (self.match('=')) return self.makeToken(.eq_eq, start, 2);
                return self.makeToken(.eq, start, 1);
            },
            ':' => {
                if (self.match(':')) return self.makeToken(.colon_colon, start, 2);
                return self.makeToken(.colon, start, 1);
            },
            '.' => {
                if (self.match('.')) {
                    if (self.match('.')) return self.makeToken(.dot_dot_dot, start, 3);
                    if (self.match('=')) return self.makeToken(.dot_dot_eq, start, 3);
                    return self.makeToken(.dot_dot, start, 2);
                }
                return self.makeToken(.dot, start, 1);
            },
            else => return error.UnexpectedChar,
        }
    }

    fn match(self: *Lexer, expected: u8) bool {
        if (self.atEnd() or self.peek() != expected) return false;
        self.pos += 1;
        return true;
    }

    // --- helpers ---------------------------------------------------------

    fn peek(self: *const Lexer) u8 {
        return self.src[self.pos];
    }

    fn atEnd(self: *const Lexer) bool {
        return self.pos >= self.src.len;
    }

    fn makeToken(self: *const Lexer, kind: TokenKind, offset: u32, len: u32) Token {
        return .{ .kind = kind, .offset = offset, .len = len, .line = self.line };
    }
};

inline fn isDigit(c: u8) bool {
    return c >= '0' and c <= '9';
}
inline fn isHexDigit(c: u8) bool {
    return isDigit(c) or (c >= 'a' and c <= 'f') or (c >= 'A' and c <= 'F');
}
inline fn isIdentStart(c: u8) bool {
    return (c >= 'a' and c <= 'z') or (c >= 'A' and c <= 'Z') or c == '_';
}
inline fn isIdentCont(c: u8) bool {
    return isIdentStart(c) or isDigit(c);
}
inline fn isSpace(c: u8) bool {
    return c == ' ' or c == '\t' or c == '\r' or c == '\n';
}

// --- keyword recognition (helper for the parser, not used by the lexer) ---

/// Lua 5.4 reserved keywords plus Pluto's additions. The lexer emits
/// `.ident` for all of these; this helper lets the parser cheaply
/// classify after the fact. Returning null means "not a keyword".
pub fn keywordFor(lexeme: []const u8) ?[]const u8 {
    const list = [_][]const u8{
        // Lua 5.4
        "and",      "break",    "do",       "else",     "elseif",
        "end",      "false",    "for",      "function", "goto",
        "if",       "in",       "local",    "nil",      "not",
        "or",       "repeat",   "return",   "then",     "true",
        "until",    "while",
        // Pluto extensions (https://pluto-lang.org/docs/Compatibility)
        "class",    "switch",   "case",     "default",  "continue",
        "new",      "extends",  "static",   "private",  "public",
    };
    for (list) |kw| {
        if (std.mem.eql(u8, lexeme, kw)) return kw;
    }
    return null;
}

// =============================================================================
// Tests
// =============================================================================

const testing = std.testing;

fn expectKinds(src: []const u8, want: []const TokenKind) !void {
    var lex = Lexer.init(src);
    for (want) |k| {
        const t = try lex.next();
        if (t.kind != k) {
            std.debug.print("expected {s}, got {s} at offset {}\n", .{ @tagName(k), @tagName(t.kind), t.offset });
            return error.TestUnexpectedKind;
        }
    }
    const last = try lex.next();
    try testing.expectEqual(TokenKind.eof, last.kind);
}

test "empty source" {
    try expectKinds("", &.{});
}

test "single tokens" {
    try expectKinds("+ - * / % ^ # & | ~ ( ) { } [ ] , ; : .", &.{
        .plus, .minus, .star, .slash, .percent, .caret, .hash, .amp,
        .pipe, .tilde, .lparen, .rparen, .lbrace, .rbrace, .lbracket,
        .rbracket, .comma, .semicolon, .colon, .dot,
    });
}

test "multi-character operators" {
    try expectKinds("// == ~= <= >= << >> :: .. ...", &.{
        .slash_slash, .eq_eq, .tilde_eq, .less_eq, .greater_eq,
        .less_less, .greater_greater, .colon_colon, .dot_dot, .dot_dot_dot,
    });
}

test "identifiers and keyword classification" {
    var lex = Lexer.init("local x = function and class");
    const expected = [_]struct { kind: TokenKind, text: []const u8, kw: bool }{
        .{ .kind = .ident, .text = "local", .kw = true },
        .{ .kind = .ident, .text = "x", .kw = false },
        .{ .kind = .eq, .text = "=", .kw = false },
        .{ .kind = .ident, .text = "function", .kw = true },
        .{ .kind = .ident, .text = "and", .kw = true },
        .{ .kind = .ident, .text = "class", .kw = true }, // Pluto-only
    };
    for (expected) |e| {
        const t = try lex.next();
        try testing.expectEqual(e.kind, t.kind);
        try testing.expectEqualStrings(e.text, t.lexeme(lex.src));
        const kw = keywordFor(t.lexeme(lex.src));
        try testing.expectEqual(e.kw, kw != null);
    }
    try testing.expectEqual(TokenKind.eof, (try lex.next()).kind);
}

test "integer literals" {
    var lex = Lexer.init("0 42 1234 0xFF 0xdeadbeef");
    inline for (.{ "0", "42", "1234", "0xFF", "0xdeadbeef" }) |expected_text| {
        const t = try lex.next();
        try testing.expectEqual(TokenKind.int_lit, t.kind);
        try testing.expectEqualStrings(expected_text, t.lexeme(lex.src));
    }
}

test "float literals" {
    var lex = Lexer.init("3.14 .5 1.5e10 1.5e-10 0x1.8p4");
    inline for (.{ "3.14", ".5", "1.5e10", "1.5e-10", "0x1.8p4" }) |expected_text| {
        const t = try lex.next();
        try testing.expectEqual(TokenKind.float_lit, t.kind);
        try testing.expectEqualStrings(expected_text, t.lexeme(lex.src));
    }
}

test "strings double, single, escapes" {
    var lex = Lexer.init(
        \\"hello" 'world' "with\nescape" "tab\there" "\x41B" "\65"
    );
    var i: u32 = 0;
    while (i < 6) : (i += 1) {
        const t = try lex.next();
        try testing.expectEqual(TokenKind.string_lit, t.kind);
    }
    try testing.expectEqual(TokenKind.eof, (try lex.next()).kind);
}

test "long-bracket string" {
    var lex = Lexer.init("[[foo bar]] [=[has ]] inside]=]");
    const a = try lex.next();
    try testing.expectEqual(TokenKind.string_lit, a.kind);
    try testing.expectEqualStrings("[[foo bar]]", a.lexeme(lex.src));
    const b = try lex.next();
    try testing.expectEqual(TokenKind.string_lit, b.kind);
    try testing.expectEqualStrings("[=[has ]] inside]=]", b.lexeme(lex.src));
}

test "comments — single and long" {
    var lex = Lexer.init(
        \\local x = 1 -- this is a comment
        \\local y = 2
        \\--[[
        \\  multi-line
        \\  comment
        \\]]
        \\local z = 3
    );
    // Just count meaningful tokens: 3 x (local ident = int)
    var count: u32 = 0;
    while (true) {
        const t = try lex.next();
        if (t.kind == .eof) break;
        count += 1;
    }
    try testing.expectEqual(@as(u32, 12), count); // 3 statements * 4 tokens each
}

test "ambiguous . and .." {
    // `1..2` should lex as int_lit(1), dot_dot, int_lit(2) — not 1. .2
    var lex = Lexer.init("1..2");
    try testing.expectEqual(TokenKind.int_lit, (try lex.next()).kind);
    try testing.expectEqual(TokenKind.dot_dot, (try lex.next()).kind);
    try testing.expectEqual(TokenKind.int_lit, (try lex.next()).kind);
    try testing.expectEqual(TokenKind.eof, (try lex.next()).kind);
}

test "line tracking" {
    var lex = Lexer.init(
        \\local
        \\x = 1
        \\
        \\local y
    );
    try testing.expectEqual(@as(u32, 1), (try lex.next()).line); // local
    try testing.expectEqual(@as(u32, 2), (try lex.next()).line); // x
    try testing.expectEqual(@as(u32, 2), (try lex.next()).line); // =
    try testing.expectEqual(@as(u32, 2), (try lex.next()).line); // 1
    try testing.expectEqual(@as(u32, 4), (try lex.next()).line); // local
    try testing.expectEqual(@as(u32, 4), (try lex.next()).line); // y
}

test "Pluto class keyword" {
    var lex = Lexer.init("class Foo extends Bar end");
    try testing.expectEqualStrings("class", (try lex.next()).lexeme(lex.src));
    try testing.expectEqualStrings("Foo", (try lex.next()).lexeme(lex.src));
    try testing.expectEqualStrings("extends", (try lex.next()).lexeme(lex.src));
    try testing.expectEqualStrings("Bar", (try lex.next()).lexeme(lex.src));
    try testing.expectEqualStrings("end", (try lex.next()).lexeme(lex.src));
    try testing.expectEqual(TokenKind.eof, (try lex.next()).kind);
}

test "error: unterminated string" {
    var lex = Lexer.init("\"hello");
    const r = lex.next();
    try testing.expectError(error.UnterminatedString, r);
}

test "error: bad escape" {
    var lex = Lexer.init("\"\\q\"");
    const r = lex.next();
    try testing.expectError(error.InvalidEscape, r);
}

test "real-world snippet" {
    // A representative chunk of Pluto/Lua showing many constructs at once.
    var lex = Lexer.init(
        \\-- Compute the nth Fibonacci number.
        \\local function fib(n)
        \\    if n < 2 then return n end
        \\    return fib(n - 1) + fib(n - 2)
        \\end
        \\
        \\local result = fib(10)
        \\print("fib(10) = " .. result)
    );
    var count: u32 = 0;
    while (true) {
        const t = try lex.next();
        if (t.kind == .eof) break;
        count += 1;
    }
    // We don't check exact token count here; the value of the test is
    // catching any error in scanning a real-shaped program.
    try testing.expect(count > 20);
}
