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
import static zinc.compiler.Ast.*;

/**
 * Parses statements: var, if, for, while, match, return, spawn, concurrent, parallel, etc.
 */
public class StmtParser {
    private final ParseContext ctx;
    private final ExprParser exprs;
    private final TypeParser types;

    public StmtParser(ParseContext ctx, ExprParser exprs, TypeParser types) {
        this.ctx = ctx;
        this.exprs = exprs;
        this.types = types;
    }

    public Stmt parseStmt() {
        ctx.skipSemis();
        var tok = ctx.peek();
        return switch (tok.type()) {
            case VAR -> parseVarStmt(false);
            case CONST -> parseVarStmt(true);
            case RETURN -> parseReturnStmt();
            case IF -> parseIfStmt();
            case FOR -> parseForStmt();
            case WHILE -> parseWhileStmt();
            case BREAK -> { ctx.advance(); yield new BreakStmt(); }
            case CONTINUE -> { ctx.advance(); yield new ContinueStmt(); }
            case MATCH -> parseMatchStmt();
            case WITH -> parseWithStmt();
            case SPAWN -> parseSpawnStmt();
            case PARALLEL -> parseParallelForStmt();
            case CONCURRENT -> parseConcurrentStmt();
            case TIMEOUT -> parseTimeoutStmt();
            case LOCK -> parseLockStmt();
            case FN -> new DeclParser(ctx, exprs, types, this).parseFnDecl();
            case DEFER -> { ctx.advance(); yield new DeferStmt(exprs.parseExpr()); }
            default -> {
                if (types.isTypeAnnotation()) yield parseTypedVarStmt();
                else yield parseExprOrAssignStmt();
            }
        };
    }

    public BlockStmt parseBlock() {
        ctx.expect(LBRACE);
        var stmts = new ArrayList<Stmt>();
        while (!ctx.check(RBRACE) && !ctx.check(EOF)) {
            ctx.skipSemis();
            if (ctx.check(RBRACE)) break;
            var s = parseStmt();
            if (s != null) stmts.add(s);
            ctx.skipSemis();
        }
        ctx.expect(RBRACE);
        return new BlockStmt(stmts);
    }

    // --- Variable declarations -----------------------------------------------

    private VarStmt parseVarStmt(boolean isConst) {
        int line = ctx.peek().line();
        ctx.advance(); // var or const

        TypeExpr type = null;
        if (types.isTypeAnnotation()) type = types.parseType();

        String name = ctx.expectIdentOrKeyword();
        Expr value = null;
        OrHandler orHandler = null;

        if (ctx.match(ASSIGN)) {
            value = exprs.parseExpr();
            if (ctx.check(OR)) orHandler = parseOrHandler();
        }

        return new VarStmt(line, name, type, value, isConst, orHandler);
    }

    private Stmt parseTypedVarStmt() {
        int line = ctx.peek().line();
        TypeExpr type = types.parseType();
        String name = ctx.expectIdentOrKeyword();
        Expr value = null;
        OrHandler orHandler = null;

        if (ctx.match(ASSIGN)) {
            value = exprs.parseExpr();
            if (ctx.check(OR)) orHandler = parseOrHandler();
        }

        return new VarStmt(line, name, type, value, false, orHandler);
    }

    // --- Control flow --------------------------------------------------------

    private ReturnStmt parseReturnStmt() {
        int line = ctx.peek().line();
        ctx.advance();
        Expr value = null;
        if (!ctx.check(RBRACE) && !ctx.check(SEMICOLON) && !ctx.check(EOF)) {
            value = exprs.parseExpr();
        }
        return new ReturnStmt(line, value);
    }

    private IfStmt parseIfStmt() {
        int line = ctx.peek().line();
        ctx.expect(IF);
        var cond = exprs.parseExpr();
        var then = parseBlock();
        Stmt elseStmt = null;
        if (ctx.match(ELSE)) {
            if (ctx.check(IF)) elseStmt = parseIfStmt();
            else elseStmt = parseBlock();
        }
        return new IfStmt(line, cond, then, elseStmt);
    }

    private ForStmt parseForStmt() {
        int line = ctx.peek().line();
        ctx.expect(FOR);

        // Range-style: for item in expr { }
        if (ctx.check(IDENT) && ctx.peekAt(1).type() == IN) {
            String item = ctx.advance().literal();
            ctx.expect(IN);
            var range = exprs.parseExpr();
            var body = parseBlock();
            return new ForStmt(line, null, null, null, true, "", item, range, body);
        }
        // for key, value in expr { } (bare, no parens)
        if (ctx.check(IDENT) && ctx.peekAt(1).type() == COMMA
            && ctx.peekAt(2).type() == IDENT && ctx.peekAt(3).type() == IN) {
            String indexVar = ctx.advance().literal();
            ctx.advance(); // ,
            String item = ctx.advance().literal();
            ctx.expect(IN);
            var range = exprs.parseExpr();
            var body = parseBlock();
            return new ForStmt(line, null, null, null, true, indexVar, item, range, body);
        }
        // for (i, item) in expr { }
        if (ctx.check(LPAREN) && ctx.peekAt(1).type() == IDENT) {
            ctx.advance(); // (
            String indexVar = ctx.expect(IDENT).literal();
            ctx.expect(COMMA);
            String item = ctx.expect(IDENT).literal();
            ctx.expect(RPAREN);
            ctx.expect(IN);
            var range = exprs.parseExpr();
            var body = parseBlock();
            return new ForStmt(line, null, null, null, true, indexVar, item, range, body);
        }

        // C-style
        var init = parseStmt();
        ctx.expect(SEMICOLON);
        var cond = exprs.parseExpr();
        ctx.expect(SEMICOLON);
        var post = parseStmt();
        var body = parseBlock();
        return new ForStmt(line, init, cond, post, false, "", "", null, body);
    }

    private WhileStmt parseWhileStmt() {
        int line = ctx.peek().line();
        ctx.expect(WHILE);
        var cond = exprs.parseExpr();
        var body = parseBlock();
        return new WhileStmt(line, cond, body);
    }

    private MatchStmt parseMatchStmt() {
        int line = ctx.peek().line();
        ctx.expect(MATCH);
        var subject = exprs.parseExpr();
        ctx.expect(LBRACE);
        var cases = new ArrayList<MatchCase>();
        while (!ctx.check(RBRACE) && !ctx.check(EOF)) {
            ctx.skipSemis();
            if (ctx.check(RBRACE)) break;
            ctx.expect(CASE);
            Expr pattern = null;
            if (!ctx.check(IDENT) || !ctx.peek().literal().equals("_")) {
                pattern = exprs.parseExpr();
            } else {
                ctx.advance(); // _
            }
            var body = parseBlock();
            cases.add(new MatchCase(pattern, body));
            ctx.skipSemis();
        }
        ctx.expect(RBRACE);
        return new MatchStmt(line, subject, cases);
    }

    // --- Expression statement ------------------------------------------------

    private Stmt parseExprOrAssignStmt() {
        int line = ctx.peek().line();
        var expr = exprs.parseExpr();

        // Assignment: expr = value, expr += value, etc.
        if (ctx.check(ASSIGN) || ctx.check(PLUS_EQ) || ctx.check(MINUS_EQ)
            || ctx.check(STAR_EQ) || ctx.check(SLASH_EQ)) {
            String op = ctx.advance().literal();
            var value = exprs.parseExpr();
            OrHandler orHandler = null;
            if (ctx.check(OR)) orHandler = parseOrHandler();
            return new AssignStmt(line, expr, op, value, orHandler);
        }

        OrHandler orHandler = null;
        if (ctx.check(OR)) orHandler = parseOrHandler();
        return new ExprStmt(line, expr, orHandler);
    }

    // --- Concurrency ---------------------------------------------------------

    private Stmt parseSpawnStmt() {
        int line = ctx.peek().line();
        ctx.expect(SPAWN);
        var body = parseBlock();
        OrHandler orHandler = null;
        if (ctx.check(OR)) orHandler = parseOrHandler();
        return new ExprStmt(line, new SpawnExpr(line, body, orHandler), null);
    }

    private ParallelForStmt parseParallelForStmt() {
        int line = ctx.peek().line();
        ctx.expect(PARALLEL);
        int max = 0;
        if (ctx.match(LPAREN)) {
            if (ctx.check(IDENT) && ctx.peek().literal().equals("max")) {
                ctx.advance(); ctx.expect(COLON);
                max = Integer.parseInt(ctx.expect(INT_LIT).literal());
            }
            ctx.expect(RPAREN);
        }
        ctx.expect(FOR);
        String item = ctx.expect(IDENT).literal();
        ctx.expect(IN);
        var range = exprs.parseExpr();
        var body = parseBlock();
        OrHandler orHandler = null;
        if (ctx.check(OR)) orHandler = parseOrHandler();
        return new ParallelForStmt(line, item, "", range, body, orHandler, max);
    }

    private ConcurrentStmt parseConcurrentStmt() {
        int line = ctx.peek().line();
        ctx.expect(CONCURRENT);
        boolean firstOnly = false;
        if (ctx.match(LPAREN)) {
            if (ctx.check(IDENT) && ctx.peek().literal().equals("first")) {
                ctx.advance(); ctx.expect(COLON); ctx.advance(); // true
                firstOnly = true;
            }
            ctx.expect(RPAREN);
        }
        ctx.expect(LBRACE);
        var tasks = new ArrayList<Expr>();
        while (!ctx.check(RBRACE) && !ctx.check(EOF)) {
            ctx.skipSemis();
            if (ctx.check(RBRACE)) break;
            tasks.add(exprs.parseExpr());
            ctx.skipSemis();
        }
        ctx.expect(RBRACE);
        OrHandler orHandler = null;
        if (ctx.check(OR)) orHandler = parseOrHandler();
        return new ConcurrentStmt(line, tasks, firstOnly, List.of(), orHandler);
    }

    private TimeoutStmt parseTimeoutStmt() {
        int line = ctx.peek().line();
        ctx.expect(TIMEOUT);
        ctx.expect(LPAREN);
        var duration = exprs.parseExpr();
        ctx.expect(RPAREN);
        var body = parseBlock();
        OrHandler orHandler = null;
        if (ctx.check(OR)) orHandler = parseOrHandler();
        return new TimeoutStmt(line, duration, body, orHandler);
    }

    private LockStmt parseLockStmt() {
        int line = ctx.peek().line();
        ctx.expect(LOCK);
        var mutex = exprs.parseExpr();
        var body = parseBlock();
        return new LockStmt(line, mutex, body);
    }

    private WithStmt parseWithStmt() {
        int line = ctx.peek().line();
        ctx.expect(WITH);
        var value = exprs.parseExpr();
        String name = "_resource";
        if (ctx.match(AS)) name = ctx.expect(IDENT).literal();
        var resources = List.of(new WithResource(name, value, null));
        var body = parseBlock();
        return new WithStmt(line, resources, body);
    }

    // --- Or handler ----------------------------------------------------------

    public OrHandler parseOrHandler() {
        ctx.expect(OR);
        if (ctx.check(MATCH)) {
            ctx.advance();
            String matchVar = ctx.expect(IDENT).literal();
            ctx.expect(LBRACE);
            var cases = new ArrayList<OrMatchCase>();
            while (!ctx.check(RBRACE) && !ctx.check(EOF)) {
                ctx.skipSemis();
                if (ctx.check(RBRACE)) break;
                ctx.expect(CASE);
                String type = ctx.check(IDENT) && !ctx.peek().literal().equals("_") ? ctx.expect(IDENT).literal() : "";
                if (type.isEmpty() && ctx.check(IDENT)) ctx.advance(); // _
                var body = parseBlock();
                cases.add(new OrMatchCase(type, body));
                ctx.skipSemis();
            }
            ctx.expect(RBRACE);
            return new OrHandler(null, cases, matchVar);
        }
        if (ctx.check(LBRACE)) {
            var body = parseBlock();
            return new OrHandler(body, null, null);
        }
        // or defaultValue
        var defaultExpr = exprs.parseExpr();
        var stmts = List.<Stmt>of(new ExprStmt(ctx.peek().line(), defaultExpr, null));
        return new OrHandler(new BlockStmt(stmts), null, null);
    }
}
