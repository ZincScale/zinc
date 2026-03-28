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
 * Parses expressions using precedence climbing.
 * Lowest to highest: or, and, equality, comparison, is/as, range, add, mul, power, unary, postfix, primary.
 */
public class ExprParser {
    private final ParseContext ctx;
    private final TypeParser types;

    public ExprParser(ParseContext ctx, TypeParser types) {
        this.ctx = ctx;
        this.types = types;
    }

    public Expr parseExpr() { return parseOr(); }

    // --- Precedence levels ---------------------------------------------------

    private Expr parseOr() {
        var left = parseAnd();
        while (ctx.check(PIPE_PIPE)) {
            int line = ctx.peek().line();
            ctx.advance();
            left = new BinaryExpr(line, left, "||", parseAnd());
        }
        return left;
    }

    private Expr parseAnd() {
        var left = parseEquality();
        while (ctx.check(AMP_AMP) || ctx.check(AND)) {
            int line = ctx.peek().line();
            ctx.advance();
            left = new BinaryExpr(line, left, "&&", parseEquality());
        }
        return left;
    }

    private Expr parseEquality() {
        var left = parseComparison();
        while (ctx.check(EQ) || ctx.check(NEQ) || ctx.check(REF_EQ) || ctx.check(REF_NEQ)) {
            int line = ctx.peek().line();
            String op = ctx.advance().literal();
            left = new BinaryExpr(line, left, op, parseComparison());
        }
        return left;
    }

    private Expr parseComparison() {
        var left = parseIs();
        while (ctx.check(LT) || ctx.check(LTE) || ctx.check(GT) || ctx.check(GTE)) {
            int line = ctx.peek().line();
            String op = ctx.advance().literal();
            left = new BinaryExpr(line, left, op, parseIs());
        }
        return left;
    }

    private Expr parseIs() {
        var left = parseRange();
        if (ctx.check(IS)) {
            ctx.advance();
            String typeName = ctx.expect(IDENT).literal();
            return new TypeAssertExpr(left, typeName, true);
        }
        if (ctx.check(AS)) {
            ctx.advance();
            String typeName = ctx.expect(IDENT).literal();
            return new TypeAssertExpr(left, typeName, false);
        }
        // "hello" in str → str.contains("hello")
        if (ctx.check(IN)) {
            int line = ctx.peek().line();
            ctx.advance();
            var container = parseRange();
            // Rewrite as: container.contains(left) — represented as CallExpr
            return new CallExpr(line,
                new SelectorExpr(line, container, "contains"),
                java.util.List.of(left), java.util.List.of(), java.util.List.of(), false);
        }
        return left;
    }

    private Expr parseRange() {
        var left = parseAddition();
        if (ctx.check(DOTDOT)) {
            ctx.advance();
            return new RangeExpr(left, parseAddition(), false);
        }
        if (ctx.check(DOTDOTEQ)) {
            ctx.advance();
            return new RangeExpr(left, parseAddition(), true);
        }
        return left;
    }

    private Expr parseAddition() {
        var left = parseMultiplication();
        while (ctx.check(PLUS) || ctx.check(MINUS)) {
            int line = ctx.peek().line();
            String op = ctx.advance().literal();
            left = new BinaryExpr(line, left, op, parseMultiplication());
        }
        return left;
    }

    private Expr parseMultiplication() {
        var left = parsePower();
        while (ctx.check(STAR) || ctx.check(SLASH) || ctx.check(PERCENT)) {
            int line = ctx.peek().line();
            String op = ctx.advance().literal();
            left = new BinaryExpr(line, left, op, parsePower());
        }
        return left;
    }

    private Expr parsePower() {
        var left = parseUnary();
        if (ctx.check(STAR_STAR)) {
            int line = ctx.peek().line();
            ctx.advance();
            return new BinaryExpr(line, left, "**", parsePower()); // right-associative
        }
        return left;
    }

    private Expr parseUnary() {
        if (ctx.check(MINUS) || ctx.check(BANG) || ctx.check(NOT)) {
            int line = ctx.peek().line();
            String op = ctx.advance().literal();
            if (op.equals("not")) op = "!";
            return new UnaryExpr(line, op, parseUnary());
        }
        return parsePostfix();
    }

    // --- Postfix: dot access, index, call ------------------------------------

    private Expr parsePostfix() {
        var expr = parsePrimary();
        while (true) {
            if (ctx.check(DOT)) {
                int line = ctx.peek().line();
                ctx.advance();
                String field = ctx.expectIdentOrKeyword();
                if (ctx.check(LPAREN)) {
                    expr = parseCallArgs(new SelectorExpr(line, expr, field));
                } else {
                    expr = new SelectorExpr(line, expr, field);
                }
            } else if (ctx.check(QUESTION_DOT)) {
                int line = ctx.peek().line();
                ctx.advance();
                String field = ctx.expectIdentOrKeyword();
                CallExpr call = null;
                if (ctx.check(LPAREN)) {
                    call = (CallExpr) parseCallArgs(new SelectorExpr(line, expr, field));
                }
                expr = new SafeNavExpr(expr, field, call);
            } else if (ctx.check(LBRACKET)) {
                ctx.advance();
                var index = parseExpr();
                ctx.expect(RBRACKET);
                expr = new IndexExpr(expr, index);
            } else if (ctx.check(LPAREN)) {
                expr = parseCallArgs(expr);
            } else {
                break;
            }
        }
        return expr;
    }

    Expr parseCallArgs(Expr callee) {
        int line = ctx.peek().line();
        ctx.expect(LPAREN);
        var args = new ArrayList<Expr>();
        var namedArgs = new ArrayList<NamedArg>();
        if (!ctx.check(RPAREN)) {
            parseCallArg(args, namedArgs);
            while (ctx.match(COMMA)) {
                if (ctx.check(RPAREN)) break;
                parseCallArg(args, namedArgs);
            }
        }
        ctx.expect(RPAREN);
        return new CallExpr(line, callee, args, namedArgs, List.of(), false);
    }

    private void parseCallArg(List<Expr> args, List<NamedArg> namedArgs) {
        if (ctx.check(IDENT) && ctx.peekAt(1).type() == COLON) {
            String name = ctx.advance().literal();
            ctx.advance(); // :
            namedArgs.add(new NamedArg(name, parseExpr()));
        } else {
            args.add(parseExpr());
        }
    }

    // --- Primary expressions -------------------------------------------------

    private Expr parsePrimary() {
        var tok = ctx.peek();
        return switch (tok.type()) {
            case INT_LIT -> { ctx.advance(); yield new IntLit(tok.literal()); }
            case FLOAT_LIT -> { ctx.advance(); yield new FloatLit(tok.literal()); }
            case STRING_LIT -> { ctx.advance(); yield new StringLit(tok.literal()); }
            case INTERP_STRING -> { ctx.advance(); yield parseInterpString(tok.literal()); }
            case RAW_STRING -> { ctx.advance(); yield new RawStringLit(tok.literal()); }
            case BOOL_LIT -> { ctx.advance(); yield new BoolLit(tok.literal().equals("true")); }
            case NULL -> { ctx.advance(); yield new NullLit(); }
            case THIS -> { ctx.advance(); yield new ThisExpr(); }
            case SUPER -> parseSuperCall();
            case NEW -> parseNewExpr();
            case SPAWN -> parseSpawnExpr();
            case IF -> parseIfExpr();
            case MATCH -> parseMatchExpr();
            case IDENT, PRINT, DATA, SEALED -> {
                if (ctx.peekAt(1).type() == ARROW) yield parseLambda();
                else { ctx.advance(); yield new Ident(tok.line(), tok.literal()); }
            }
            case LPAREN -> parseParenOrLambda();
            case LBRACKET -> parseListLit();
            case LBRACE -> parseMapLit();
            default -> {
                ctx.error("unexpected token " + tok.type() + " (" + tok.literal() + ") in expression");
                ctx.advance();
                yield new Ident(tok.line(), "__error__");
            }
        };
    }

    private Expr parseSuperCall() {
        ctx.advance();
        ctx.expect(LPAREN);
        var args = new ArrayList<Expr>();
        if (!ctx.check(RPAREN)) {
            args.add(parseExpr());
            while (ctx.match(COMMA)) args.add(parseExpr());
        }
        ctx.expect(RPAREN);
        return new SuperCallExpr(args);
    }

    private Expr parseNewExpr() {
        int line = ctx.peek().line();
        ctx.advance(); // new
        String name = ctx.expect(IDENT).literal();
        while (ctx.check(DOT) && ctx.isIdentLike(ctx.peekAt(1).type())) {
            ctx.advance();
            name += "." + ctx.advance().literal();
        }
        var typeArgs = new ArrayList<String>();
        if (ctx.match(LT)) {
            typeArgs.add(types.formatType(types.parseType()));
            while (ctx.match(COMMA)) typeArgs.add(types.formatType(types.parseType()));
            ctx.expect(GT);
        }
        var callExpr = (CallExpr) parseCallArgs(new Ident(line, name));
        return new CallExpr(line, callExpr.callee(), callExpr.args(), callExpr.namedArgs(), typeArgs, true);
    }

    private Expr parseSpawnExpr() {
        int line = ctx.peek().line();
        ctx.advance();
        var body = new StmtParser(ctx, this, types).parseBlock();
        OrHandler orHandler = null;
        if (ctx.check(OR)) orHandler = new StmtParser(ctx, this, types).parseOrHandler();
        return new SpawnExpr(line, body, orHandler);
    }

    /**
     * if cond: thenExpr else: elseExpr  (expression-if / ternary)
     */
    private Expr parseIfExpr() {
        ctx.advance(); // if
        var cond = parseExpr();
        ctx.expect(COLON);
        var thenExpr = parseExpr();
        ctx.expect(ELSE);
        ctx.expect(COLON);
        var elseExpr = parseExpr();
        return new Ast.IfExpr(cond, thenExpr, elseExpr);
    }

    private Expr parseMatchExpr() {
        ctx.expect(MATCH);
        var subject = parseExpr();
        ctx.expect(LBRACE);
        var cases = new ArrayList<MatchExprCase>();
        while (!ctx.check(RBRACE) && !ctx.check(EOF)) {
            ctx.skipSemis();
            if (ctx.check(RBRACE)) break;
            ctx.expect(CASE);
            Expr pattern = null;
            if (!ctx.check(IDENT) || !ctx.peek().literal().equals("_")) {
                pattern = parseExpr();
            } else {
                ctx.advance(); // _
            }
            // Match expr can use -> or { }
            if (ctx.check(ARROW)) {
                ctx.advance();
                var value = parseExpr();
                cases.add(new MatchExprCase(pattern, value));
            } else if (ctx.check(LBRACE)) {
                // Block form: case X { expr } — last expr is the value
                ctx.advance();
                Expr value = new NullLit();
                while (!ctx.check(RBRACE) && !ctx.check(EOF)) {
                    ctx.skipSemis();
                    if (ctx.check(RBRACE)) break;
                    value = parseExpr();
                    ctx.skipSemis();
                }
                ctx.expect(RBRACE);
                cases.add(new MatchExprCase(pattern, value));
            }
            ctx.skipSemis();
        }
        ctx.expect(RBRACE);
        return new MatchExpr(subject, cases);
    }

    // --- Lambda --------------------------------------------------------------

    private Expr parseLambda() {
        String paramName = ctx.advance().literal();
        ctx.advance(); // ->
        var param = new ParamDecl(paramName, null, null, false);
        if (ctx.check(LBRACE)) {
            var body = new StmtParser(ctx, this, types).parseBlock();
            return new LambdaExpr(List.of(param), body);
        }
        var expr = parseExpr();
        return new LambdaExpr(List.of(param),
            new BlockStmt(List.of(new ReturnStmt(0, expr))));
    }

    private Expr parseParenOrLambda() {
        int saved = ctx.save();
        if (tryParseLambdaParams()) {
            ctx.restore(saved);
            return parseMultiParamLambda();
        }
        ctx.restore(saved);

        ctx.advance(); // (
        var expr = parseExpr();
        if (ctx.match(COMMA)) {
            var elements = new ArrayList<Expr>();
            elements.add(expr);
            elements.add(parseExpr());
            while (ctx.match(COMMA)) elements.add(parseExpr());
            ctx.expect(RPAREN);
            return new TupleLit(elements);
        }
        ctx.expect(RPAREN);
        return expr;
    }

    private boolean tryParseLambdaParams() {
        if (!ctx.match(LPAREN)) return false;
        if (ctx.match(RPAREN)) return ctx.check(ARROW);
        int depth = 1;
        while (depth > 0 && !ctx.check(EOF)) {
            if (ctx.check(LPAREN)) depth++;
            else if (ctx.check(RPAREN)) depth--;
            ctx.advance();
        }
        return ctx.check(ARROW);
    }

    private Expr parseMultiParamLambda() {
        ctx.expect(LPAREN);
        var params = new ArrayList<ParamDecl>();
        if (!ctx.check(RPAREN)) {
            params.add(parseLambdaParam());
            while (ctx.match(COMMA)) params.add(parseLambdaParam());
        }
        ctx.expect(RPAREN);
        ctx.expect(ARROW);
        if (ctx.check(LBRACE)) {
            return new LambdaExpr(params, new StmtParser(ctx, this, types).parseBlock());
        }
        var expr = parseExpr();
        return new LambdaExpr(params,
            new BlockStmt(List.of(new ReturnStmt(0, expr))));
    }

    private ParamDecl parseLambdaParam() {
        TypeExpr type = null;
        String name;
        if (ctx.isTypeStart() && ctx.peekAt(1).type() == IDENT) {
            type = types.parseType();
            name = ctx.expect(IDENT).literal();
        } else {
            name = ctx.expect(IDENT).literal();
        }
        return new ParamDecl(name, type, null, false);
    }

    // --- Literals ------------------------------------------------------------

    private Expr parseListLit() {
        ctx.advance(); // [
        var elements = new ArrayList<Expr>();
        if (!ctx.check(RBRACKET)) {
            elements.add(parseExpr());
            while (ctx.match(COMMA)) {
                if (ctx.check(RBRACKET)) break;
                elements.add(parseExpr());
            }
        }
        ctx.expect(RBRACKET);
        return new ListLit(elements);
    }

    private Expr parseMapLit() {
        ctx.advance(); // {
        var keys = new ArrayList<Expr>();
        var values = new ArrayList<Expr>();
        if (!ctx.check(RBRACE)) {
            keys.add(parseExpr());
            ctx.expect(COLON);
            values.add(parseExpr());
            while (ctx.match(COMMA)) {
                if (ctx.check(RBRACE)) break;
                keys.add(parseExpr());
                ctx.expect(COLON);
                values.add(parseExpr());
            }
        }
        ctx.expect(RBRACE);
        return new MapLit(keys, values);
    }

    Expr parseInterpString(String raw) {
        var parts = new ArrayList<Expr>();
        var sb = new StringBuilder();
        int i = 0;
        while (i < raw.length()) {
            char ch = raw.charAt(i);
            if (ch == '{') {
                if (!sb.isEmpty()) { parts.add(new StringLit(sb.toString())); sb.setLength(0); }
                i++;
                int depth = 1;
                var exprStr = new StringBuilder();
                while (i < raw.length() && depth > 0) {
                    if (raw.charAt(i) == '{') depth++;
                    else if (raw.charAt(i) == '}') { depth--; if (depth == 0) break; }
                    exprStr.append(raw.charAt(i));
                    i++;
                }
                if (i < raw.length()) i++; // skip closing }
                var lexer = new Lexer(exprStr.toString());
                var tokens = lexer.tokenize().or(List.of(new Token(TokenType.EOF, "", 0, 0)));
                var subCtx = new ParseContext(tokens);
                var subExpr = new ExprParser(subCtx, types);
                parts.add(subExpr.parseExpr());
            } else {
                sb.append(ch);
                i++;
            }
        }
        if (!sb.isEmpty()) parts.add(new StringLit(sb.toString()));
        return new StringInterpLit(parts);
    }
}
