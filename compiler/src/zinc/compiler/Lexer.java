// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import static zinc.compiler.TokenType.*;

public class Lexer {
    private final char[] src;
    private int pos;
    private int line = 1;
    private int col = 1;
    private final List<String> errors = new ArrayList<>();

    private static final Map<String, TokenType> KEYWORDS = Map.ofEntries(
        Map.entry("class", CLASS), Map.entry("interface", INTERFACE),
        Map.entry("construct", CONSTRUCT), Map.entry("new", NEW),
        Map.entry("fn", FN), Map.entry("return", RETURN),
        Map.entry("if", IF), Map.entry("else", ELSE),
        Map.entry("for", FOR), Map.entry("while", WHILE),
        Map.entry("break", BREAK), Map.entry("continue", CONTINUE),
        Map.entry("or", OR), Map.entry("print", PRINT),
        Map.entry("var", VAR), Map.entry("pub", PUB),
        Map.entry("static", STATIC), Map.entry("super", SUPER),
        Map.entry("this", THIS), Map.entry("import", IMPORT),
        Map.entry("as", AS), Map.entry("in", IN),
        Map.entry("true", BOOL_LIT), Map.entry("false", BOOL_LIT),
        Map.entry("null", NULL), Map.entry("enum", ENUM),
        Map.entry("match", MATCH), Map.entry("case", CASE),
        Map.entry("package", PACKAGE), Map.entry("const", CONST),
        Map.entry("defer", DEFER), Map.entry("is", IS),
        Map.entry("with", WITH), Map.entry("data", DATA),
        Map.entry("spawn", SPAWN), Map.entry("use", USE),
        Map.entry("readonly", READONLY), Map.entry("override", OVERRIDE),
        Map.entry("end", END), Map.entry("try", TRY),
        Map.entry("catch", CATCH), Map.entry("raise", RAISE),
        Map.entry("not", NOT), Map.entry("and", AND),
        Map.entry("from", FROM),
        Map.entry("init", INIT), Map.entry("abstract", ABSTRACT),
        Map.entry("sealed", SEALED), Map.entry("lock", LOCK)
    );

    public Lexer(String source) {
        this.src = source.toCharArray();
    }

    public List<String> errors() { return errors; }

    public Result<List<Token>> tokenize() {
        var tokens = new ArrayList<Token>();
        while (true) {
            var tok = nextToken();
            tokens.add(tok);
            if (tok.type() == EOF) break;
        }
        if (!errors.isEmpty()) return Result.err(errors);
        return Result.ok(tokens);
    }

    public Token nextToken() {
        skipShebang();
        skipWhitespaceAndComments();

        int tokLine = line, tokCol = col;
        char ch = peek();

        if (ch == 0) return token(EOF, "", tokLine, tokCol);

        // Triple-quote string
        if (ch == '"' && peekAt(1) == '"' && peekAt(2) == '"')
            return readTripleQuoteString(tokLine, tokCol);

        // Double-quote string (supports interpolation)
        if (ch == '"') return readString('"', tokLine, tokCol);

        // Single-quote string (literal, no interpolation)
        if (ch == '\'') return readLiteralString(tokLine, tokCol);

        // Raw string
        if (ch == '`') return readRawString(tokLine, tokCol);

        // Number
        if (isDigit(ch)) return readNumber(tokLine, tokCol);

        // Identifier or keyword
        if (isLetter(ch)) return readIdent(tokLine, tokCol);

        advance();

        return switch (ch) {
            case '(' -> token(LPAREN, "(", tokLine, tokCol);
            case ')' -> token(RPAREN, ")", tokLine, tokCol);
            case '{' -> token(LBRACE, "{", tokLine, tokCol);
            case '}' -> token(RBRACE, "}", tokLine, tokCol);
            case '[' -> token(LBRACKET, "[", tokLine, tokCol);
            case ']' -> token(RBRACKET, "]", tokLine, tokCol);
            case ',' -> token(COMMA, ",", tokLine, tokCol);
            case ';' -> token(SEMICOLON, ";", tokLine, tokCol);
            case '@' -> token(AT, "@", tokLine, tokCol);
            case '%' -> token(PERCENT, "%", tokLine, tokCol);
            case '.' -> {
                if (peek() == '.' && peekAt(1) == '.') { advance(); advance(); yield token(DOTDOTDOT, "...", tokLine, tokCol); }
                else if (peek() == '.' && peekAt(1) == '=') { advance(); advance(); yield token(DOTDOTEQ, "..=", tokLine, tokCol); }
                else if (peek() == '.') { advance(); yield token(DOTDOT, "..", tokLine, tokCol); }
                else yield token(DOT, ".", tokLine, tokCol);
            }
            case ':' -> {
                if (peek() == '=') { advance(); yield token(COLONASSIGN, ":=", tokLine, tokCol); }
                else yield token(COLON, ":", tokLine, tokCol);
            }
            case '?' -> {
                if (peek() == '.') { advance(); yield token(QUESTION_DOT, "?.", tokLine, tokCol); }
                else if (peek() == '?') { advance(); yield token(QUESTION_QUESTION, "??", tokLine, tokCol); }
                else yield token(QUESTION, "?", tokLine, tokCol);
            }
            case '+' -> {
                if (peek() == '=') { advance(); yield token(PLUS_EQ, "+=", tokLine, tokCol); }
                else yield token(PLUS, "+", tokLine, tokCol);
            }
            case '-' -> {
                if (peek() == '>') { advance(); yield token(ARROW, "->", tokLine, tokCol); }
                else if (peek() == '=') { advance(); yield token(MINUS_EQ, "-=", tokLine, tokCol); }
                else yield token(MINUS, "-", tokLine, tokCol);
            }
            case '*' -> {
                if (peek() == '*') { advance(); yield token(STAR_STAR, "**", tokLine, tokCol); }
                else if (peek() == '=') { advance(); yield token(STAR_EQ, "*=", tokLine, tokCol); }
                else yield token(STAR, "*", tokLine, tokCol);
            }
            case '/' -> {
                if (peek() == '=') { advance(); yield token(SLASH_EQ, "/=", tokLine, tokCol); }
                else yield token(SLASH, "/", tokLine, tokCol);
            }
            case '!' -> {
                if (peek() == '=') {
                    advance();
                    if (peek() == '=') { advance(); yield token(REF_NEQ, "!==", tokLine, tokCol); }
                    else yield token(NEQ, "!=", tokLine, tokCol);
                } else yield token(BANG, "!", tokLine, tokCol);
            }
            case '=' -> {
                if (peek() == '=') {
                    advance();
                    if (peek() == '=') { advance(); yield token(REF_EQ, "===", tokLine, tokCol); }
                    else yield token(EQ, "==", tokLine, tokCol);
                } else yield token(ASSIGN, "=", tokLine, tokCol);
            }
            case '<' -> {
                if (peek() == '=') { advance(); yield token(LTE, "<=", tokLine, tokCol); }
                else yield token(LT, "<", tokLine, tokCol);
            }
            case '>' -> {
                if (peek() == '=') { advance(); yield token(GTE, ">=", tokLine, tokCol); }
                else yield token(GT, ">", tokLine, tokCol);
            }
            case '&' -> {
                if (peek() == '&') { advance(); yield token(AMP_AMP, "&&", tokLine, tokCol); }
                else { errors.add(tokLine + ":" + tokCol + ": unexpected '&'"); yield token(ILLEGAL, "&", tokLine, tokCol); }
            }
            case '|' -> {
                if (peek() == '|') { advance(); yield token(PIPE_PIPE, "||", tokLine, tokCol); }
                else { errors.add(tokLine + ":" + tokCol + ": unexpected '|'"); yield token(ILLEGAL, "|", tokLine, tokCol); }
            }
            default -> {
                errors.add(tokLine + ":" + tokCol + ": unexpected character '" + ch + "'");
                yield token(ILLEGAL, String.valueOf(ch), tokLine, tokCol);
            }
        };
    }

    // --- Helpers ---

    private char peek() { return pos < src.length ? src[pos] : 0; }

    private char peekAt(int offset) {
        int idx = pos + offset;
        return idx < src.length ? src[idx] : 0;
    }

    private char advance() {
        if (pos >= src.length) return 0;
        char ch = src[pos++];
        if (ch == '\n') { line++; col = 1; } else { col++; }
        return ch;
    }

    private Token token(TokenType type, String literal, int line, int col) {
        return new Token(type, literal, line, col);
    }

    private void skipShebang() {
        if (pos == 0 && peek() == '#' && peekAt(1) == '!') {
            while (peek() != '\n' && peek() != 0) advance();
        }
    }

    private void skipWhitespaceAndComments() {
        while (true) {
            char ch = peek();
            if (ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n') {
                advance();
            } else if (ch == '/' && peekAt(1) == '/') {
                while (peek() != '\n' && peek() != 0) advance();
            } else if (ch == '/' && peekAt(1) == '*') {
                advance(); advance();
                while (!(peek() == '*' && peekAt(1) == '/') && peek() != 0) advance();
                if (peek() != 0) { advance(); advance(); }
            } else {
                return;
            }
        }
    }

    // --- String readers ---

    private Token readString(char quote, int tokLine, int tokCol) {
        advance(); // opening quote
        var sb = new StringBuilder();
        boolean hasInterp = false;
        while (true) {
            char ch = peek();
            if (ch == 0 || ch == '\n') { errors.add(tokLine + ":" + tokCol + ": unterminated string"); break; }
            if (ch == quote) { advance(); break; }
            if (ch == '{') {
                hasInterp = true;
                sb.append(advance());
                int depth = 1;
                while (depth > 0 && peek() != 0) {
                    char ic = peek();
                    if (ic == '{') depth++;
                    else if (ic == '}') {
                        depth--;
                        if (depth == 0) { sb.append(advance()); break; }
                    } else if (ic == '"' || ic == '\'') {
                        char q = ic;
                        sb.append(advance()); // opening quote
                        while (peek() != 0 && peek() != q) {
                            if (peek() == '\\') sb.append(advance());
                            sb.append(advance());
                        }
                        if (peek() == q) sb.append(advance());
                        continue;
                    }
                    sb.append(advance());
                }
                continue;
            }
            if (ch == '\\') {
                advance();
                char esc = advance();
                switch (esc) {
                    case 'n' -> sb.append('\n');
                    case 't' -> sb.append('\t');
                    case '"' -> sb.append('"');
                    case '\'' -> sb.append('\'');
                    case '\\' -> sb.append('\\');
                    case 'r' -> sb.append('\r');
                    default -> { sb.append('\\'); sb.append(esc); }
                }
                continue;
            }
            sb.append(advance());
        }
        return token(hasInterp ? INTERP_STRING : STRING_LIT, sb.toString(), tokLine, tokCol);
    }

    private Token readLiteralString(int tokLine, int tokCol) {
        advance(); // opening '
        var sb = new StringBuilder();
        while (true) {
            char ch = peek();
            if (ch == 0 || ch == '\n') { errors.add(tokLine + ":" + tokCol + ": unterminated string"); break; }
            if (ch == '\'') { advance(); break; }
            if (ch == '\\') {
                advance();
                char esc = advance();
                switch (esc) {
                    case 'n' -> sb.append('\n');
                    case 't' -> sb.append('\t');
                    case '\'' -> sb.append('\'');
                    case '\\' -> sb.append('\\');
                    default -> { sb.append('\\'); sb.append(esc); }
                }
                continue;
            }
            sb.append(advance());
        }
        return token(STRING_LIT, sb.toString(), tokLine, tokCol);
    }

    private Token readRawString(int tokLine, int tokCol) {
        advance(); // opening `
        var sb = new StringBuilder();
        while (true) {
            char ch = peek();
            if (ch == 0) { errors.add(tokLine + ":" + tokCol + ": unterminated raw string"); break; }
            if (ch == '`') { advance(); break; }
            sb.append(advance());
        }
        return token(RAW_STRING, sb.toString(), tokLine, tokCol);
    }

    private Token readTripleQuoteString(int tokLine, int tokCol) {
        advance(); advance(); advance(); // consume """
        var sb = new StringBuilder();
        boolean hasInterp = false;
        while (true) {
            char ch = peek();
            if (ch == 0) { errors.add(tokLine + ":" + tokCol + ": unterminated triple-quoted string"); break; }
            if (ch == '"' && peekAt(1) == '"' && peekAt(2) == '"') {
                advance(); advance(); advance();
                break;
            }
            if (ch == '{') hasInterp = true;
            sb.append(advance());
        }
        var content = stripIndent(sb.toString());
        return token(hasInterp ? INTERP_STRING : STRING_LIT, content, tokLine, tokCol);
    }

    /** Strip common leading whitespace from multi-line strings. */
    private String stripIndent(String s) {
        if (s.startsWith("\n")) s = s.substring(1);
        // Remove trailing whitespace-only line (before closing """)
        int lastNl = s.lastIndexOf('\n');
        if (lastNl >= 0 && s.substring(lastNl + 1).isBlank()) s = s.substring(0, lastNl + 1);
        var lines = s.split("\n", -1);
        int minIndent = Integer.MAX_VALUE;
        for (var line : lines) {
            if (line.isBlank()) continue;
            int indent = 0;
            for (char c : line.toCharArray()) { if (c == ' ') indent++; else break; }
            minIndent = Math.min(minIndent, indent);
        }
        if (minIndent == Integer.MAX_VALUE || minIndent == 0) return s;
        var result = new StringBuilder();
        for (int i = 0; i < lines.length; i++) {
            if (i > 0) result.append('\n');
            if (lines[i].length() > minIndent) result.append(lines[i].substring(minIndent));
        }
        return result.toString();
    }

    // --- Number and identifier ---

    private Token readNumber(int tokLine, int tokCol) {
        int start = pos;
        boolean isFloat = false;
        while (isDigit(peek())) advance();
        if (peek() == '.' && isDigit(peekAt(1))) {
            isFloat = true;
            advance();
            while (isDigit(peek())) advance();
        }
        String lit = new String(src, start, pos - start);
        return token(isFloat ? FLOAT_LIT : INT_LIT, lit, tokLine, tokCol);
    }

    private Token readIdent(int tokLine, int tokCol) {
        int start = pos;
        while (isLetter(peek()) || isDigit(peek())) advance();
        String lit = new String(src, start, pos - start);
        TokenType type = KEYWORDS.getOrDefault(lit, IDENT);
        return token(type, lit, tokLine, tokCol);
    }

    private static boolean isDigit(char ch) { return ch >= '0' && ch <= '9'; }
    private static boolean isLetter(char ch) { return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'; }
}
