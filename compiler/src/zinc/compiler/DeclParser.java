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
 * Parses top-level declarations: fn, class, interface, data, enum, const, sealed.
 */
public class DeclParser {
    private final ParseContext ctx;
    private final ExprParser exprs;
    private final TypeParser types;
    private final StmtParser stmts;

    public DeclParser(ParseContext ctx, ExprParser exprs, TypeParser types, StmtParser stmts) {
        this.ctx = ctx;
        this.exprs = exprs;
        this.types = types;
        this.stmts = stmts;
    }

    // --- Functions ------------------------------------------------------------

    public FnDecl parseFnDecl() {
        int line = ctx.peek().line();
        ctx.expect(FN);
        String name = ctx.expect(IDENT).literal();

        var typeParams = new ArrayList<String>();
        if (ctx.match(LT)) {
            typeParams.add(ctx.expect(IDENT).literal());
            while (ctx.match(COMMA)) typeParams.add(ctx.expect(IDENT).literal());
            ctx.expect(GT);
        }

        ctx.expect(LPAREN);
        var params = types.parseParamList();
        ctx.expect(RPAREN);

        TypeExpr returnType = null;
        if (ctx.match(COLON)) returnType = types.parseType();

        // Single-expression function: fn name(params): Type = expr
        if (ctx.match(ASSIGN)) {
            var expr = exprs.parseExpr();
            var body = new BlockStmt(List.of(new ReturnStmt(line, expr)));
            return new FnDecl(line, name, false, typeParams, params, returnType, body, List.of());
        }

        var body = stmts.parseBlock();
        return new FnDecl(line, name, false, typeParams, params, returnType, body, List.of());
    }

    // --- Classes -------------------------------------------------------------

    public ClassDecl parseClassDecl(List<Annotation> annotations) {
        int line = ctx.peek().line();
        ctx.expect(CLASS);
        String name = ctx.expect(IDENT).literal();

        var typeParams = new ArrayList<String>();
        if (ctx.match(LT)) {
            typeParams.add(ctx.expect(IDENT).literal());
            while (ctx.match(COMMA)) typeParams.add(ctx.expect(IDENT).literal());
            ctx.expect(GT);
        }

        var parents = new ArrayList<String>();
        if (ctx.match(COLON)) {
            parents.add(ctx.parseQualifiedName());
            while (ctx.match(COMMA)) parents.add(ctx.parseQualifiedName());
        }

        ctx.expect(LBRACE);
        var fields = new ArrayList<FieldDecl>();
        var ctors = new ArrayList<CtorDecl>();
        var methods = new ArrayList<MethodDecl>();
        parseClassBody(fields, ctors, methods);
        ctx.expect(RBRACE);

        return new ClassDecl(line, name, false, typeParams, parents, fields, ctors, methods, annotations);
    }

    private void parseClassBody(List<FieldDecl> fields, List<CtorDecl> ctors, List<MethodDecl> methods) {
        while (!ctx.check(RBRACE) && !ctx.check(EOF)) {
            ctx.skipSemis();
            if (ctx.check(RBRACE)) break;

            var annots = ctx.check(AT) ? parseAnnotations() : List.<Annotation>of();

            boolean isPub = ctx.match(PUB);
            boolean isStatic = ctx.match(STATIC);
            boolean isReadonly = false;
            boolean isInit = false;
            boolean isAbstract = ctx.match(ABSTRACT);

            if (ctx.check(READONLY)) { isReadonly = true; ctx.advance(); }
            else if (!isPub && !isStatic && !isAbstract && ctx.check(INIT)) {
                if (ctx.peekAt(1).type() == LPAREN) {
                    ctors.add(parseCtor());
                    continue;
                }
                isInit = true;
                ctx.advance();
            }

            // Nested data class (sealed class variant)
            if (ctx.check(DATA) && ctx.peekAt(1).type() == IDENT) {
                // Skip for now — data variants inside sealed class
                parseDataClassDecl();
                continue;
            }

            if (ctx.check(FN)) {
                methods.add(parseMethodDecl(isPub, isStatic, isAbstract, annots));
            } else if (ctx.check(OVERRIDE)) {
                ctx.advance();
                if (ctx.check(FN)) {
                    methods.add(parseMethodDecl(isPub, isStatic, false, annots));
                }
            } else {
                fields.add(parseFieldDecl(isPub, isReadonly, isInit, annots));
            }
            ctx.skipSemis();
        }
    }

    private MethodDecl parseMethodDecl(boolean isPub, boolean isStatic, boolean isAbstract, List<Annotation> annotations) {
        ctx.expect(FN);
        String name = ctx.expect(IDENT).literal();
        ctx.expect(LPAREN);
        var params = types.parseParamList();
        ctx.expect(RPAREN);

        TypeExpr returnType = null;
        if (ctx.match(COLON)) returnType = types.parseType();

        BlockStmt body = null;
        if (!isAbstract && ctx.check(LBRACE)) body = stmts.parseBlock();

        return new MethodDecl(name, isPub, isStatic, isAbstract, params, returnType, body, annotations);
    }

    private CtorDecl parseCtor() {
        ctx.expect(INIT);
        ctx.expect(LPAREN);
        var params = types.parseParamList();
        ctx.expect(RPAREN);
        var body = stmts.parseBlock();

        var superArgs = new ArrayList<Expr>();
        var filteredStmts = new ArrayList<Stmt>();
        for (var s : body.stmts()) {
            if (s instanceof ExprStmt es && es.expr() instanceof SuperCallExpr sc) {
                superArgs.addAll(sc.args());
            } else {
                filteredStmts.add(s);
            }
        }
        return new CtorDecl(params, new BlockStmt(filteredStmts), superArgs);
    }

    private FieldDecl parseFieldDecl(boolean isPub, boolean isReadonly, boolean isInit, List<Annotation> annotations) {
        boolean isConst = false;
        if (ctx.check(CONST)) { isConst = true; ctx.advance(); }

        TypeExpr type = null;
        if (ctx.check(VAR)) {
            ctx.advance();
            if (ctx.isTypeStart()) type = types.parseType();
        } else if (ctx.isTypeStart()) {
            type = types.parseType();
        }

        String name = ctx.expect(IDENT).literal();
        Expr defaultValue = null;
        if (ctx.match(ASSIGN)) defaultValue = exprs.parseExpr();

        return new FieldDecl(name, isPub, isReadonly, isConst, isInit, type, defaultValue, annotations);
    }

    // --- Interfaces ----------------------------------------------------------

    public InterfaceDecl parseInterfaceDecl() {
        int line = ctx.peek().line();
        ctx.expect(INTERFACE);
        String name = ctx.expect(IDENT).literal();
        ctx.expect(LBRACE);
        var methods = new ArrayList<MethodSig>();
        while (!ctx.check(RBRACE) && !ctx.check(EOF)) {
            ctx.skipSemis();
            if (ctx.check(RBRACE)) break;
            boolean isPub = ctx.match(PUB);
            ctx.expect(FN);
            String mName = ctx.expect(IDENT).literal();
            ctx.expect(LPAREN);
            var params = types.parseParamList();
            ctx.expect(RPAREN);
            TypeExpr ret = null;
            if (ctx.match(COLON)) ret = types.parseType();
            methods.add(new MethodSig(mName, isPub, params, ret));
            ctx.skipSemis();
        }
        ctx.expect(RBRACE);
        return new InterfaceDecl(line, name, methods);
    }

    // --- Data classes ---------------------------------------------------------

    public DataClassDecl parseDataClassDecl() {
        int line = ctx.peek().line();
        ctx.expect(DATA);
        String name = ctx.expect(IDENT).literal();

        var typeParams = new ArrayList<String>();
        if (ctx.match(LT)) {
            typeParams.add(ctx.expect(IDENT).literal());
            while (ctx.match(COMMA)) typeParams.add(ctx.expect(IDENT).literal());
            ctx.expect(GT);
        }

        ctx.expect(LPAREN);
        var params = new ArrayList<FieldDecl>();
        if (!ctx.check(RPAREN)) {
            params.add(parseDataClassParam());
            while (ctx.match(COMMA)) params.add(parseDataClassParam());
        }
        ctx.expect(RPAREN);

        var parents = new ArrayList<String>();
        if (ctx.match(COLON)) {
            parents.add(ctx.parseQualifiedName());
            while (ctx.match(COMMA)) parents.add(ctx.parseQualifiedName());
        }

        var methods = new ArrayList<MethodDecl>();
        if (ctx.match(LBRACE)) {
            while (!ctx.check(RBRACE) && !ctx.check(EOF)) {
                ctx.skipSemis();
                if (ctx.check(RBRACE)) break;
                boolean isPub = ctx.match(PUB);
                boolean isStatic = ctx.match(STATIC);
                methods.add(parseMethodDecl(isPub, isStatic, false, List.of()));
                ctx.skipSemis();
            }
            ctx.expect(RBRACE);
        }

        return new DataClassDecl(line, name, typeParams, parents, params, methods);
    }

    private FieldDecl parseDataClassParam() {
        boolean isPub = ctx.match(PUB);
        boolean isReadonly = false;
        if (ctx.check(READONLY)) { isReadonly = true; ctx.advance(); }
        TypeExpr type = types.parseType();
        String name = ctx.expect(IDENT).literal();
        Expr defaultValue = null;
        if (ctx.match(ASSIGN)) defaultValue = exprs.parseExpr();
        return new FieldDecl(name, isPub, isReadonly, false, false, type, defaultValue, List.of());
    }

    // --- Enum ----------------------------------------------------------------

    public EnumDecl parseEnumDecl() {
        int line = ctx.peek().line();
        ctx.expect(ENUM);
        String name = ctx.expect(IDENT).literal();
        ctx.expect(LBRACE);
        var variants = new ArrayList<String>();
        if (!ctx.check(RBRACE)) {
            variants.add(ctx.expect(IDENT).literal());
            while (ctx.match(COMMA)) {
                if (ctx.check(RBRACE)) break;
                variants.add(ctx.expect(IDENT).literal());
            }
        }
        ctx.expect(RBRACE);
        return new EnumDecl(line, name, variants);
    }

    // --- Const ---------------------------------------------------------------

    public ConstDecl parseConstDecl() {
        int line = ctx.peek().line();
        ctx.expect(CONST);
        boolean isPub = ctx.match(PUB);
        String name = ctx.expect(IDENT).literal();
        TypeExpr type = null;
        if (ctx.match(COLON)) type = types.parseType();
        ctx.expect(ASSIGN);
        var value = exprs.parseExpr();
        return new ConstDecl(line, name, isPub, type, value);
    }

    // --- Sealed class --------------------------------------------------------

    public SealedClassDecl parseSealedClass(ClassDecl cls) {
        return new SealedClassDecl(cls.line(), cls.name(), cls.parents(),
            cls.fields(), cls.ctors(), cls.methods(), List.of(), cls.annotations());
    }

    // --- Annotations ---------------------------------------------------------

    public List<Annotation> parseAnnotations() {
        var annots = new ArrayList<Annotation>();
        while (ctx.check(AT)) {
            ctx.advance();
            String name = ctx.expect(IDENT).literal();
            var args = new ArrayList<String>();
            if (ctx.match(LPAREN)) {
                if (!ctx.check(RPAREN)) {
                    args.add(ctx.expect(STRING_LIT).literal());
                    while (ctx.match(COMMA)) args.add(ctx.expect(STRING_LIT).literal());
                }
                ctx.expect(RPAREN);
            }
            annots.add(new Annotation(name, args));
            ctx.skipSemis();
        }
        return annots;
    }
}
