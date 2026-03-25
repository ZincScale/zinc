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
 * Parses type expressions: simple, generic, array, optional.
 */
public class TypeParser {
    private final ParseContext ctx;

    public TypeParser(ParseContext ctx) {
        this.ctx = ctx;
    }

    public TypeExpr parseType() {
        String name = ctx.expect(IDENT).literal();

        // Dotted names: java.util.List
        while (ctx.check(DOT) && ctx.isIdentLike(ctx.peekAt(1).type())) {
            ctx.advance();
            name += "." + ctx.advance().literal();
        }

        TypeExpr type;
        if (ctx.match(LT)) {
            var args = new ArrayList<TypeExpr>();
            args.add(parseType());
            while (ctx.match(COMMA)) args.add(parseType());
            ctx.expect(GT);
            type = new GenericType(name, args);
        } else {
            type = new SimpleType(name);
        }

        // Array suffix: Type[]
        if (ctx.check(LBRACKET) && ctx.peekAt(1).type() == RBRACKET) {
            ctx.advance(); ctx.advance();
            type = new ArrayType(type);
        }

        // Optional suffix: Type?
        if (ctx.match(QUESTION)) type = new OptionalType(type);

        return type;
    }

    /**
     * Detects whether the current position looks like a type annotation
     * (Type name = ...) by lookahead without consuming tokens.
     */
    public boolean isTypeAnnotation() {
        if (!ctx.isTypeStart()) return false;
        int saved = ctx.save();
        try {
            if (ctx.peek().type() != IDENT) return false;
            ctx.advance();
            // Dotted: java.util.Map
            while (ctx.check(DOT) && ctx.isIdentLike(ctx.peekAt(1).type())) { ctx.advance(); ctx.advance(); }
            // Generic: Type<T>
            if (ctx.check(LT)) {
                ctx.advance();
                int depth = 1;
                while (depth > 0 && !ctx.check(EOF)) {
                    if (ctx.check(LT)) depth++;
                    else if (ctx.check(GT)) depth--;
                    ctx.advance();
                }
            }
            // Array: Type[]
            if (ctx.check(LBRACKET) && ctx.peekAt(1).type() == RBRACKET) { ctx.advance(); ctx.advance(); }
            // Optional: Type?
            if (ctx.check(QUESTION)) ctx.advance();
            // Must be followed by an identifier (or keyword used as identifier)
            return ctx.check(IDENT) || ctx.isIdentLike(ctx.peek().type());
        } finally {
            ctx.restore(saved);
        }
    }

    /** Formats a TypeExpr back to its string representation. */
    public String formatType(TypeExpr type) {
        return switch (type) {
            case SimpleType s -> s.name();
            case GenericType g -> g.name() + "<" + String.join(", ", g.typeArgs().stream().map(this::formatType).toList()) + ">";
            case ArrayType a -> formatType(a.elementType()) + "[]";
            case OptionalType o -> formatType(o.inner()) + "?";
            case FuncType f -> "Func";
        };
    }

    /** Parses a parameter list: (Type name, Type name = default, ...) */
    public List<ParamDecl> parseParamList() {
        var params = new ArrayList<ParamDecl>();
        if (ctx.check(RPAREN)) return params;
        params.add(parseParam());
        while (ctx.match(COMMA)) {
            if (ctx.check(RPAREN)) break;
            params.add(parseParam());
        }
        return params;
    }

    private ParamDecl parseParam() {
        TypeExpr type = null;
        boolean isVariadic = false;

        if (ctx.isTypeStart()) {
            type = parseType();
            if (ctx.check(DOTDOTDOT)) { isVariadic = true; ctx.advance(); }
        }

        String name = ctx.expect(IDENT).literal();
        Expr defaultValue = null;
        if (ctx.match(ASSIGN)) defaultValue = new ExprParser(ctx, this).parseExpr();
        return new ParamDecl(name, type, defaultValue, isVariadic);
    }
}
