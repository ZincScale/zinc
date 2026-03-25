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
 * Entry point for parsing Zinc source code.
 * Delegates to ExprParser, StmtParser, DeclParser, and TypeParser.
 */
public class Parser {
    private final ParseContext ctx;
    private final TypeParser types;
    private final ExprParser exprs;
    private final StmtParser stmts;
    private final DeclParser decls;

    public Parser(List<Token> tokens) {
        this.ctx = new ParseContext(tokens);
        this.types = new TypeParser(ctx);
        this.exprs = new ExprParser(ctx, types);
        this.stmts = new StmtParser(ctx, exprs, types);
        this.decls = new DeclParser(ctx, exprs, types, stmts);
    }

    public List<String> errors() { return ctx.errors(); }

    public Result<Ast.Program> parseResult() {
        var program = parse();
        if (!ctx.errors().isEmpty()) return Result.err(ctx.errors());
        return Result.ok(program);
    }

    public Ast.Program parse() {
        ctx.skipSemis();
        PackageDecl pkg = null;
        var imports = new ArrayList<ImportDecl>();
        var topDecls = new ArrayList<TopLevelDecl>();
        var topStmts = new ArrayList<Stmt>();

        if (ctx.check(PACKAGE)) {
            pkg = parsePackageDecl();
            ctx.skipSemis();
        }

        while (!ctx.check(EOF)) {
            ctx.skipSemis();
            if (ctx.check(EOF)) break;

            var tok = ctx.peek();
            switch (tok.type()) {
                case IMPORT -> imports.add(parseImport());
                case AT -> {
                    var annots = decls.parseAnnotations();
                    if (ctx.check(FN)) {
                        var fn = decls.parseFnDecl();
                        topDecls.add(new FnDecl(fn.line(), fn.name(), fn.isPub(), fn.typeParams(),
                            fn.params(), fn.returnType(), fn.body(), annots));
                    } else if (ctx.check(CLASS)) {
                        topDecls.add(decls.parseClassDecl(annots));
                    } else {
                        ctx.error("expected fn or class after annotation");
                        ctx.advance();
                    }
                }
                case FN -> topDecls.add(decls.parseFnDecl());
                case CLASS -> topDecls.add(decls.parseClassDecl(List.of()));
                case DATA -> {
                    if (ctx.peekAt(1).type() == IDENT) topDecls.add(decls.parseDataClassDecl());
                    else topStmts.add(stmts.parseStmt());
                }
                case INTERFACE -> topDecls.add(decls.parseInterfaceDecl());
                case ENUM -> topDecls.add(decls.parseEnumDecl());
                case CONST -> topDecls.add(decls.parseConstDecl());
                case ABSTRACT -> {
                    ctx.advance();
                    if (ctx.check(CLASS)) {
                        var cls = decls.parseClassDecl(List.of());
                        topDecls.add(new ClassDecl(cls.line(), cls.name(), true,
                            cls.typeParams(), cls.parents(), cls.fields(), cls.ctors(),
                            cls.methods(), cls.annotations()));
                    } else {
                        ctx.error("expected 'class' after 'abstract'");
                        ctx.advance();
                    }
                }
                default -> {
                    if (tok.type() == SEALED && ctx.peekAt(1).type() == CLASS) {
                        ctx.advance();
                        var cls = decls.parseClassDecl(List.of());
                        topDecls.add(decls.parseSealedClass(cls));
                    } else {
                        var s = stmts.parseStmt();
                        if (s != null) topStmts.add(s);
                    }
                }
            }
            ctx.skipSemis();
        }

        return new Program(null, pkg, imports, topDecls, topStmts);
    }

    // --- Package & imports ---------------------------------------------------

    private PackageDecl parsePackageDecl() {
        ctx.expect(PACKAGE);
        var sb = new StringBuilder(ctx.expect(IDENT).literal());
        while (ctx.match(DOT)) sb.append('.').append(ctx.expect(IDENT).literal());
        ctx.skipSemis();
        return new PackageDecl(sb.toString());
    }

    private ImportDecl parseImport() {
        ctx.expect(IMPORT);
        var sb = new StringBuilder(ctx.expect(IDENT).literal());
        while (ctx.match(DOT)) {
            sb.append('.');
            if (ctx.check(STAR)) { sb.append(ctx.advance().literal()); break; }
            sb.append(ctx.expectIdentOrKeyword());
        }
        String alias = null;
        if (ctx.match(AS)) alias = ctx.expect(IDENT).literal();
        ctx.skipSemis();
        return new ImportDecl(sb.toString(), alias);
    }
}
