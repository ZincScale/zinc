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

import static zinc.compiler.TokenType.*;

/**
 * Shared state for all parser components.
 * Holds the token stream, cursor position, and error list.
 */
public class ParseContext {
    private final List<Token> tokens;
    private int pos;
    private final List<String> errors = new ArrayList<>();

    public ParseContext(List<Token> tokens) {
        this.tokens = tokens;
    }

    public List<String> errors() { return errors; }

    // --- Token navigation ----------------------------------------------------

    public Token peek() { return tokens.get(pos); }

    public Token peekAt(int offset) {
        int idx = pos + offset;
        return idx < tokens.size() ? tokens.get(idx) : tokens.getLast();
    }

    public Token advance() { return tokens.get(pos++); }

    public boolean check(TokenType type) { return peek().type() == type; }

    public Token expect(TokenType type) {
        if (check(type)) return advance();
        error("expected " + type + ", got " + peek().type() + " (" + peek().literal() + ")");
        // Advance past the bad token to avoid infinite loops
        if (!check(EOF)) advance();
        return peek();
    }

    public boolean match(TokenType type) {
        if (check(type)) { advance(); return true; }
        return false;
    }

    public void skipSemis() {
        while (check(SEMICOLON)) advance();
    }

    public void error(String msg) {
        errors.add(peek().line() + ":" + peek().col() + ": " + msg);
    }

    // --- Lookahead helpers ---------------------------------------------------

    public boolean isIdentLike(TokenType t) {
        return t == IDENT || t == DATA || t == MATCH
            || t == PRINT || t == SPAWN || t == INTERFACE || t == SEALED;
    }

    public boolean isTypeStart() {
        var t = peek().type();
        return t == IDENT || t == DATA || t == MATCH || t == SPAWN || t == PRINT;
    }

    public String expectIdentOrKeyword() {
        if (isIdentLike(peek().type())) return advance().literal();
        return expect(IDENT).literal();
    }

    public String parseQualifiedName() {
        var sb = new StringBuilder(expect(IDENT).literal());
        while (check(DOT) && isIdentLike(peekAt(1).type())) {
            advance();
            sb.append('.').append(advance().literal());
        }
        return sb.toString();
    }

    /** Save cursor position for backtracking. */
    public int save() { return pos; }

    /** Restore cursor to a previously saved position. */
    public void restore(int saved) { pos = saved; }
}
