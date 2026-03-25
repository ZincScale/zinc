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

public class Parser {
    private final List<Token> tokens;
    private int pos;
    private final List<String> errors = new ArrayList<>();

    public Parser(List<Token> tokens) {
        this.tokens = tokens;
    }

    public List<String> errors() { return errors; }

    // --- Token navigation ----------------------------------------------------

    private Token peek() { return tokens.get(pos); }

    private Token peekAt(int offset) {
        int idx = pos + offset;
        return idx < tokens.size() ? tokens.get(idx) : tokens.getLast();
    }

    private Token advance() { return tokens.get(pos++); }

    private boolean check(TokenType type) { return peek().type() == type; }

    private Token expect(TokenType type) {
        if (check(type)) return advance();
        error("expected " + type + ", got " + peek().type() + " (" + peek().literal() + ")");
        return peek();
    }

    private boolean match(TokenType type) {
        if (check(type)) { advance(); return true; }
        return false;
    }

    private void skipSemis() {
        while (check(SEMICOLON)) advance();
    }

    private void error(String msg) {
        errors.add(peek().line() + ":" + peek().col() + ": " + msg);
    }

    // --- Entry point ---------------------------------------------------------

    public Program parse() {
        skipSemis();
        PackageDecl pkg = null;
        var imports = new ArrayList<ImportDecl>();
        var decls = new ArrayList<TopLevelDecl>();
        var stmts = new ArrayList<Stmt>();

        if (check(PACKAGE)) {
            pkg = parsePackageDecl();
            skipSemis();
        }

        while (!check(EOF)) {
            skipSemis();
            if (check(EOF)) break;

            var tok = peek();
            switch (tok.type()) {
                case IMPORT -> imports.add(parseImport());
                case AT -> {
                    var annots = parseAnnotations();
                    if (check(FN)) {
                        var fn = parseFnDecl();
                        decls.add(new FnDecl(fn.line(), fn.name(), fn.isPub(), fn.typeParams(),
                            fn.params(), fn.returnType(), fn.body(), annots));
                    } else if (check(CLASS)) {
                        decls.add(parseClassDecl(annots));
                    } else {
                        error("expected fn or class after annotation");
                        advance();
                    }
                }
                case FN -> decls.add(parseFnDecl());
                case CLASS -> decls.add(parseClassDecl(List.of()));
                case DATA -> {
                    if (peekAt(1).type() == IDENT) {
                        decls.add(parseDataClassDecl());
                    } else {
                        stmts.add(parseStmt());
                    }
                }
                case INTERFACE -> decls.add(parseInterfaceDecl());
                case ENUM -> decls.add(parseEnumDecl());
                case CONST -> decls.add(parseConstDecl());
                case ABSTRACT -> {
                    advance();
                    if (check(CLASS)) {
                        var cls = parseClassDecl(List.of());
                        decls.add(new ClassDecl(cls.line(), cls.name(), true,
                            cls.typeParams(), cls.parents(), cls.fields(), cls.ctors(),
                            cls.methods(), cls.annotations()));
                    } else {
                        error("expected 'class' after 'abstract'");
                        advance();
                    }
                }
                default -> {
                    // sealed class
                    if (tok.type() == IDENT && tok.literal().equals("sealed") && peekAt(1).type() == CLASS) {
                        advance();
                        var cls = parseClassDecl(List.of());
                        decls.add(parseSealedClass(cls));
                    } else {
                        var s = parseStmt();
                        if (s != null) stmts.add(s);
                    }
                }
            }
            skipSemis();
        }

        return new Program(null, pkg, imports, decls, stmts);
    }

    // --- Package & imports ---------------------------------------------------

    private PackageDecl parsePackageDecl() {
        expect(PACKAGE);
        var sb = new StringBuilder(expect(IDENT).literal());
        while (match(DOT)) sb.append('.').append(expect(IDENT).literal());
        skipSemis();
        return new PackageDecl(sb.toString());
    }

    private ImportDecl parseImport() {
        expect(IMPORT);
        var sb = new StringBuilder(expect(IDENT).literal());
        while (match(DOT)) {
            sb.append('.');
            if (check(STAR)) { sb.append(advance().literal()); break; }
            sb.append(expectIdentOrKeyword());
        }
        String alias = null;
        if (match(AS)) alias = expect(IDENT).literal();
        skipSemis();
        return new ImportDecl(sb.toString(), alias);
    }

    // --- Annotations ---------------------------------------------------------

    private List<Annotation> parseAnnotations() {
        var annots = new ArrayList<Annotation>();
        while (check(AT)) {
            advance();
            String name = expect(IDENT).literal();
            var args = new ArrayList<String>();
            if (match(LPAREN)) {
                if (!check(RPAREN)) {
                    args.add(expect(STRING_LIT).literal());
                    while (match(COMMA)) args.add(expect(STRING_LIT).literal());
                }
                expect(RPAREN);
            }
            annots.add(new Annotation(name, args));
            skipSemis();
        }
        return annots;
    }

    // --- Top-level declarations ----------------------------------------------

    private FnDecl parseFnDecl() {
        int line = peek().line();
        expect(FN);
        String name = expect(IDENT).literal();

        // Optional type params: <T, U>
        var typeParams = new ArrayList<String>();
        if (match(LT)) {
            typeParams.add(expect(IDENT).literal());
            while (match(COMMA)) typeParams.add(expect(IDENT).literal());
            expect(GT);
        }

        expect(LPAREN);
        var params = parseParamList();
        expect(RPAREN);

        TypeExpr returnType = null;
        if (match(COLON)) returnType = parseType();

        var body = parseBlock();
        return new FnDecl(line, name, false, typeParams, params, returnType, body, List.of());
    }

    private ClassDecl parseClassDecl(List<Annotation> annotations) {
        int line = peek().line();
        expect(CLASS);
        String name = expect(IDENT).literal();

        var typeParams = new ArrayList<String>();
        if (match(LT)) {
            typeParams.add(expect(IDENT).literal());
            while (match(COMMA)) typeParams.add(expect(IDENT).literal());
            expect(GT);
        }

        var parents = new ArrayList<String>();
        if (match(COLON)) {
            parents.add(parseQualifiedName());
            while (match(COMMA)) parents.add(parseQualifiedName());
        }

        expect(LBRACE);
        var fields = new ArrayList<FieldDecl>();
        var ctors = new ArrayList<CtorDecl>();
        var methods = new ArrayList<MethodDecl>();
        parseClassBody(fields, ctors, methods);
        expect(RBRACE);

        return new ClassDecl(line, name, false, typeParams, parents, fields, ctors, methods, annotations);
    }

    private void parseClassBody(List<FieldDecl> fields, List<CtorDecl> ctors, List<MethodDecl> methods) {
        while (!check(RBRACE) && !check(EOF)) {
            skipSemis();
            if (check(RBRACE)) break;

            var annots = check(AT) ? parseAnnotations() : List.<Annotation>of();

            boolean isPub = match(PUB);
            boolean isStatic = match(STATIC);
            boolean isReadonly = false;
            boolean isInit = false;
            boolean isAbstract = match(ABSTRACT);

            if (check(READONLY)) { isReadonly = true; advance(); }
            else if (!isPub && !isStatic && !isAbstract && check(INIT)) {
                // Could be init field or constructor
                if (peekAt(1).type() == LPAREN) {
                    ctors.add(parseCtor());
                    continue;
                }
                isInit = true;
                advance();
            }

            if (check(FN)) {
                var m = parseMethodDecl(isPub, isStatic, isAbstract, annots);
                methods.add(m);
            } else if (check(OVERRIDE)) {
                advance();
                if (check(FN)) {
                    var m = parseMethodDecl(isPub, isStatic, false, annots);
                    methods.add(m);
                }
            } else {
                // Field declaration
                var field = parseFieldDecl(isPub, isReadonly, isInit, annots);
                fields.add(field);
            }
            skipSemis();
        }
    }

    private MethodDecl parseMethodDecl(boolean isPub, boolean isStatic, boolean isAbstract, List<Annotation> annotations) {
        expect(FN);
        String name = expect(IDENT).literal();
        expect(LPAREN);
        var params = parseParamList();
        expect(RPAREN);

        TypeExpr returnType = null;
        if (match(COLON)) returnType = parseType();

        BlockStmt body = null;
        if (!isAbstract && check(LBRACE)) {
            body = parseBlock();
        }

        return new MethodDecl(name, isPub, isStatic, isAbstract, params, returnType, body, annotations);
    }

    private CtorDecl parseCtor() {
        expect(INIT);
        expect(LPAREN);
        var params = parseParamList();
        expect(RPAREN);
        var body = parseBlock();
        // Extract super() calls from body
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
        if (check(CONST)) { isConst = true; advance(); }

        TypeExpr type = null;
        if (check(VAR)) {
            advance();
            if (isTypeStart()) type = parseType();
        } else if (isTypeStart()) {
            type = parseType();
        }

        String name = expect(IDENT).literal();
        Expr defaultValue = null;
        if (match(ASSIGN)) defaultValue = parseExpr();

        return new FieldDecl(name, isPub, isReadonly, isConst, isInit, type, defaultValue, annotations);
    }

    private InterfaceDecl parseInterfaceDecl() {
        int line = peek().line();
        expect(INTERFACE);
        String name = expect(IDENT).literal();
        expect(LBRACE);
        var methods = new ArrayList<MethodSig>();
        while (!check(RBRACE) && !check(EOF)) {
            skipSemis();
            if (check(RBRACE)) break;
            boolean isPub = match(PUB);
            expect(FN);
            String mName = expect(IDENT).literal();
            expect(LPAREN);
            var params = parseParamList();
            expect(RPAREN);
            TypeExpr ret = null;
            if (match(COLON)) ret = parseType();
            methods.add(new MethodSig(mName, isPub, params, ret));
            skipSemis();
        }
        expect(RBRACE);
        return new InterfaceDecl(line, name, methods);
    }

    private DataClassDecl parseDataClassDecl() {
        int line = peek().line();
        expect(DATA);
        String name = expect(IDENT).literal();

        var typeParams = new ArrayList<String>();
        if (match(LT)) {
            typeParams.add(expect(IDENT).literal());
            while (match(COMMA)) typeParams.add(expect(IDENT).literal());
            expect(GT);
        }

        expect(LPAREN);
        var params = new ArrayList<FieldDecl>();
        if (!check(RPAREN)) {
            params.add(parseDataClassParam());
            while (match(COMMA)) params.add(parseDataClassParam());
        }
        expect(RPAREN);

        var parents = new ArrayList<String>();
        if (match(COLON)) {
            parents.add(parseQualifiedName());
            while (match(COMMA)) parents.add(parseQualifiedName());
        }

        var methods = new ArrayList<MethodDecl>();
        if (match(LBRACE)) {
            while (!check(RBRACE) && !check(EOF)) {
                skipSemis();
                if (check(RBRACE)) break;
                boolean isPub = match(PUB);
                boolean isStatic = match(STATIC);
                methods.add(parseMethodDecl(isPub, isStatic, false, List.of()));
                skipSemis();
            }
            expect(RBRACE);
        }

        return new DataClassDecl(line, name, typeParams, parents, params, methods);
    }

    private FieldDecl parseDataClassParam() {
        boolean isPub = match(PUB);
        boolean isReadonly = false;
        if (check(READONLY)) { isReadonly = true; advance(); }
        TypeExpr type = parseType();
        String name = expect(IDENT).literal();
        Expr defaultValue = null;
        if (match(ASSIGN)) defaultValue = parseExpr();
        return new FieldDecl(name, isPub, isReadonly, false, false, type, defaultValue, List.of());
    }

    private EnumDecl parseEnumDecl() {
        int line = peek().line();
        expect(ENUM);
        String name = expect(IDENT).literal();
        expect(LBRACE);
        var variants = new ArrayList<String>();
        if (!check(RBRACE)) {
            variants.add(expect(IDENT).literal());
            while (match(COMMA)) {
                if (check(RBRACE)) break;
                variants.add(expect(IDENT).literal());
            }
        }
        expect(RBRACE);
        return new EnumDecl(line, name, variants);
    }

    private ConstDecl parseConstDecl() {
        int line = peek().line();
        expect(CONST);
        boolean isPub = match(PUB);
        String name = expect(IDENT).literal();
        TypeExpr type = null;
        if (match(COLON)) type = parseType();
        expect(ASSIGN);
        var value = parseExpr();
        return new ConstDecl(line, name, isPub, type, value);
    }

    private SealedClassDecl parseSealedClass(ClassDecl cls) {
        // Re-parse variants from methods list — sealed classes have data variants in body
        // For now, represent as SealedClassDecl with variants parsed from nested data classes
        return new SealedClassDecl(cls.line(), cls.name(), cls.parents(),
            cls.fields(), cls.ctors(), cls.methods(), List.of(), cls.annotations());
    }

    // --- Parameters ----------------------------------------------------------

    private List<ParamDecl> parseParamList() {
        var params = new ArrayList<ParamDecl>();
        if (check(RPAREN)) return params;
        params.add(parseParam());
        while (match(COMMA)) {
            if (check(RPAREN)) break;
            params.add(parseParam());
        }
        return params;
    }

    private ParamDecl parseParam() {
        TypeExpr type = null;
        boolean isVariadic = false;

        if (isTypeStart()) {
            type = parseType();
            if (check(DOTDOTDOT)) { isVariadic = true; advance(); }
        }

        String name = expect(IDENT).literal();
        Expr defaultValue = null;
        if (match(ASSIGN)) defaultValue = parseExpr();
        return new ParamDecl(name, type, defaultValue, isVariadic);
    }

    // --- Types ---------------------------------------------------------------

    private TypeExpr parseType() {
        String name = expect(IDENT).literal();
        // Dotted names: java.util.List
        while (check(DOT) && isIdentLike(peekAt(1).type())) {
            advance();
            name += "." + advance().literal();
        }

        TypeExpr type;
        if (match(LT)) {
            var args = new ArrayList<TypeExpr>();
            args.add(parseType());
            while (match(COMMA)) args.add(parseType());
            expect(GT);
            type = new GenericType(name, args);
        } else {
            type = new SimpleType(name);
        }

        // Array suffix: Type[]
        if (check(LBRACKET) && peekAt(1).type() == RBRACKET) {
            advance(); advance();
            type = new ArrayType(type);
        }

        // Optional suffix: Type?
        if (match(QUESTION)) type = new OptionalType(type);

        return type;
    }

    private boolean isTypeStart() {
        var t = peek().type();
        return t == IDENT || t == DATA || t == MATCH || t == CONCURRENT || t == SPAWN || t == PRINT;
    }

    private boolean isIdentLike(TokenType t) {
        return t == IDENT || t == CONCURRENT || t == DATA || t == MATCH
            || t == PRINT || t == SPAWN || t == INTERFACE;
    }

    private String expectIdentOrKeyword() {
        if (isIdentLike(peek().type())) return advance().literal();
        return expect(IDENT).literal();
    }

    private String parseQualifiedName() {
        var sb = new StringBuilder(expect(IDENT).literal());
        while (check(DOT) && isIdentLike(peekAt(1).type())) {
            advance();
            sb.append('.').append(advance().literal());
        }
        return sb.toString();
    }

    // --- Statements ----------------------------------------------------------

    private Stmt parseStmt() {
        skipSemis();
        var tok = peek();
        return switch (tok.type()) {
            case VAR -> parseVarStmt(false);
            case CONST -> parseVarStmt(true);
            case RETURN -> parseReturnStmt();
            case IF -> parseIfStmt();
            case FOR -> parseForStmt();
            case WHILE -> parseWhileStmt();
            case BREAK -> { advance(); yield new BreakStmt(); }
            case CONTINUE -> { advance(); yield new ContinueStmt(); }
            case MATCH -> parseMatchStmt();
            case WITH -> parseWithStmt();
            case SPAWN -> parseSpawnStmt();
            case PARALLEL -> parseParallelForStmt();
            case CONCURRENT -> parseConcurrentStmt();
            case TIMEOUT -> parseTimeoutStmt();
            case FN -> parseFnDecl();
            case DEFER -> { advance(); yield new DeferStmt(parseExpr()); }
            default -> {
                // Check for typed variable: Type name = expr
                if (isTypeAnnotation()) yield parseTypedVarStmt();
                else yield parseExprStmt();
            }
        };
    }

    private BlockStmt parseBlock() {
        expect(LBRACE);
        var stmts = new ArrayList<Stmt>();
        while (!check(RBRACE) && !check(EOF)) {
            skipSemis();
            if (check(RBRACE)) break;
            var s = parseStmt();
            if (s != null) stmts.add(s);
            skipSemis();
        }
        expect(RBRACE);
        return new BlockStmt(stmts);
    }

    private VarStmt parseVarStmt(boolean isConst) {
        int line = peek().line();
        advance(); // var or const

        TypeExpr type = null;
        if (isTypeAnnotation()) type = parseType();

        String name = expect(IDENT).literal();
        Expr value = null;
        OrHandler orHandler = null;

        if (match(ASSIGN)) {
            value = parseExpr();
            if (check(OR)) orHandler = parseOrHandler();
        }

        return new VarStmt(line, name, type, value, isConst, orHandler);
    }

    private Stmt parseTypedVarStmt() {
        int line = peek().line();
        TypeExpr type = parseType();
        String name = expect(IDENT).literal();
        Expr value = null;
        OrHandler orHandler = null;

        if (match(ASSIGN)) {
            value = parseExpr();
            if (check(OR)) orHandler = parseOrHandler();
        }

        return new VarStmt(line, name, type, value, false, orHandler);
    }

    private ReturnStmt parseReturnStmt() {
        int line = peek().line();
        advance();
        Expr value = null;
        if (!check(RBRACE) && !check(SEMICOLON) && !check(EOF)) {
            value = parseExpr();
        }
        return new ReturnStmt(line, value);
    }

    private IfStmt parseIfStmt() {
        int line = peek().line();
        expect(IF);
        var cond = parseExpr();
        var then = parseBlock();
        Stmt elseStmt = null;
        if (match(ELSE)) {
            if (check(IF)) elseStmt = parseIfStmt();
            else elseStmt = parseBlock();
        }
        return new IfStmt(line, cond, then, elseStmt);
    }

    private ForStmt parseForStmt() {
        int line = peek().line();
        expect(FOR);

        // Range-style: for item in expr { } or for (i, item) in expr { }
        if (check(IDENT) && peekAt(1).type() == IN) {
            String item = advance().literal();
            expect(IN);
            var range = parseExpr();
            var body = parseBlock();
            return new ForStmt(line, null, null, null, true, "", item, range, body);
        }
        if (check(LPAREN) && peekAt(1).type() == IDENT) {
            // for (i, item) in expr { }
            advance(); // (
            String indexVar = expect(IDENT).literal();
            expect(COMMA);
            String item = expect(IDENT).literal();
            expect(RPAREN);
            expect(IN);
            var range = parseExpr();
            var body = parseBlock();
            return new ForStmt(line, null, null, null, true, indexVar, item, range, body);
        }

        // C-style: for (init; cond; post) { }
        // Simplified — just parse init; cond; post
        var init = parseStmt();
        expect(SEMICOLON);
        var cond = parseExpr();
        expect(SEMICOLON);
        var post = parseStmt();
        var body = parseBlock();
        return new ForStmt(line, init, cond, post, false, "", "", null, body);
    }

    private WhileStmt parseWhileStmt() {
        int line = peek().line();
        expect(WHILE);
        var cond = parseExpr();
        var body = parseBlock();
        return new WhileStmt(line, cond, body);
    }

    private MatchStmt parseMatchStmt() {
        int line = peek().line();
        expect(MATCH);
        var subject = parseExpr();
        expect(LBRACE);
        var cases = new ArrayList<MatchCase>();
        while (!check(RBRACE) && !check(EOF)) {
            skipSemis();
            if (check(RBRACE)) break;
            expect(CASE);
            Expr pattern = null;
            if (!check(IDENT) || !peek().literal().equals("_")) {
                pattern = parseExpr();
            } else {
                advance(); // consume _
            }
            var body = parseBlock();
            cases.add(new MatchCase(pattern, body));
            skipSemis();
        }
        expect(RBRACE);
        return new MatchStmt(line, subject, cases);
    }

    private ExprStmt parseExprStmt() {
        int line = peek().line();
        var expr = parseExpr();
        OrHandler orHandler = null;
        if (check(OR)) orHandler = parseOrHandler();
        return new ExprStmt(line, expr, orHandler);
    }

    private Stmt parseSpawnStmt() {
        int line = peek().line();
        expect(SPAWN);
        var body = parseBlock();
        OrHandler orHandler = null;
        if (check(OR)) orHandler = parseOrHandler();
        return new ExprStmt(line, new SpawnExpr(line, body, orHandler), null);
    }

    private ParallelForStmt parseParallelForStmt() {
        int line = peek().line();
        expect(PARALLEL);
        int max = 0;
        if (match(LPAREN)) {
            // parallel(max: N) for ...
            if (check(IDENT) && peek().literal().equals("max")) {
                advance(); expect(COLON);
                max = Integer.parseInt(expect(INT_LIT).literal());
            }
            expect(RPAREN);
        }
        expect(FOR);
        String item = expect(IDENT).literal();
        expect(IN);
        var range = parseExpr();
        var body = parseBlock();
        OrHandler orHandler = null;
        if (check(OR)) orHandler = parseOrHandler();
        return new ParallelForStmt(line, item, "", range, body, orHandler, max);
    }

    private ConcurrentStmt parseConcurrentStmt() {
        int line = peek().line();
        expect(CONCURRENT);
        boolean firstOnly = false;
        if (match(LPAREN)) {
            if (check(IDENT) && peek().literal().equals("first")) {
                advance(); expect(COLON); advance(); // true
                firstOnly = true;
            }
            expect(RPAREN);
        }
        expect(LBRACE);
        var tasks = new ArrayList<Expr>();
        while (!check(RBRACE) && !check(EOF)) {
            skipSemis();
            if (check(RBRACE)) break;
            tasks.add(parseExpr());
            skipSemis();
        }
        expect(RBRACE);
        OrHandler orHandler = null;
        if (check(OR)) orHandler = parseOrHandler();
        return new ConcurrentStmt(line, tasks, firstOnly, List.of(), orHandler);
    }

    private TimeoutStmt parseTimeoutStmt() {
        int line = peek().line();
        expect(TIMEOUT);
        expect(LPAREN);
        var duration = parseExpr();
        expect(RPAREN);
        var body = parseBlock();
        OrHandler orHandler = null;
        if (check(OR)) orHandler = parseOrHandler();
        return new TimeoutStmt(line, duration, body, orHandler);
    }

    private WithStmt parseWithStmt() {
        int line = peek().line();
        expect(WITH);
        var resources = new ArrayList<WithResource>();
        var value = parseExpr();
        String name = "_resource";
        if (match(AS)) name = expect(IDENT).literal();
        resources.add(new WithResource(name, value, null));
        var body = parseBlock();
        return new WithStmt(line, resources, body);
    }

    // --- Or handler ----------------------------------------------------------

    private OrHandler parseOrHandler() {
        expect(OR);
        if (check(MATCH)) {
            // or match err { case Type -> ... }
            advance();
            String matchVar = expect(IDENT).literal();
            expect(LBRACE);
            var cases = new ArrayList<OrMatchCase>();
            while (!check(RBRACE) && !check(EOF)) {
                skipSemis();
                if (check(RBRACE)) break;
                expect(CASE);
                String type = check(IDENT) && !peek().literal().equals("_") ? expect(IDENT).literal() : "";
                if (type.isEmpty() && check(IDENT)) advance(); // consume _
                var body = parseBlock();
                cases.add(new OrMatchCase(type, body));
                skipSemis();
            }
            expect(RBRACE);
            return new OrHandler(null, cases, matchVar);
        }
        // or { body } or or defaultExpr
        if (check(LBRACE)) {
            var body = parseBlock();
            return new OrHandler(body, null, null);
        }
        // or defaultValue (single expression)
        var defaultExpr = parseExpr();
        var stmts = List.<Stmt>of(new ExprStmt(peek().line(), defaultExpr, null));
        return new OrHandler(new BlockStmt(stmts), null, null);
    }

    // --- Type annotation detection -------------------------------------------

    private boolean isTypeAnnotation() {
        if (!isTypeStart()) return false;
        // Look ahead: Type name = ... or Type name
        int saved = pos;
        try {
            // Skip the type
            if (peek().type() != IDENT) return false;
            advance();
            // Dotted: java.util.Map
            while (check(DOT) && isIdentLike(peekAt(1).type())) { advance(); advance(); }
            // Generic: Type<T>
            if (check(LT)) {
                advance();
                int depth = 1;
                while (depth > 0 && !check(EOF)) {
                    if (check(LT)) depth++;
                    else if (check(GT)) depth--;
                    advance();
                }
            }
            // Array: Type[]
            if (check(LBRACKET) && peekAt(1).type() == RBRACKET) { advance(); advance(); }
            // Optional: Type?
            if (check(QUESTION)) advance();
            // Must be followed by an identifier (the variable name)
            return check(IDENT);
        } finally {
            pos = saved;
        }
    }

    // --- Expressions (Pratt parser) ------------------------------------------

    private Expr parseExpr() { return parseOr(); }

    private Expr parseOr() {
        var left = parseAnd();
        while (check(PIPE_PIPE)) {
            advance();
            left = new BinaryExpr(left, "||", parseAnd());
        }
        return left;
    }

    private Expr parseAnd() {
        var left = parseEquality();
        while (check(AMP_AMP) || (check(AND))) {
            advance();
            left = new BinaryExpr(left, "&&", parseEquality());
        }
        return left;
    }

    private Expr parseEquality() {
        var left = parseComparison();
        while (check(EQ) || check(NEQ) || check(REF_EQ) || check(REF_NEQ)) {
            String op = advance().literal();
            left = new BinaryExpr(left, op, parseComparison());
        }
        return left;
    }

    private Expr parseComparison() {
        var left = parseIs();
        while (check(LT) || check(LTE) || check(GT) || check(GTE)) {
            String op = advance().literal();
            left = new BinaryExpr(left, op, parseIs());
        }
        return left;
    }

    private Expr parseIs() {
        var left = parseRange();
        if (check(IS)) {
            advance();
            String typeName = expect(IDENT).literal();
            return new TypeAssertExpr(left, typeName, true);
        }
        if (check(AS)) {
            advance();
            String typeName = expect(IDENT).literal();
            return new TypeAssertExpr(left, typeName, false);
        }
        return left;
    }

    private Expr parseRange() {
        var left = parseAddition();
        if (check(DOTDOT)) {
            advance();
            return new RangeExpr(left, parseAddition(), false);
        }
        if (check(DOTDOTEQ)) {
            advance();
            return new RangeExpr(left, parseAddition(), true);
        }
        return left;
    }

    private Expr parseAddition() {
        var left = parseMultiplication();
        while (check(PLUS) || check(MINUS)) {
            String op = advance().literal();
            left = new BinaryExpr(left, op, parseMultiplication());
        }
        return left;
    }

    private Expr parseMultiplication() {
        var left = parsePower();
        while (check(STAR) || check(SLASH) || check(PERCENT)) {
            String op = advance().literal();
            left = new BinaryExpr(left, op, parsePower());
        }
        return left;
    }

    private Expr parsePower() {
        var left = parseUnary();
        if (check(STAR_STAR)) {
            advance();
            return new BinaryExpr(left, "**", parsePower()); // right-associative
        }
        return left;
    }

    private Expr parseUnary() {
        if (check(MINUS) || check(BANG) || check(NOT)) {
            String op = advance().literal();
            if (op.equals("not")) op = "!";
            return new UnaryExpr(op, parseUnary());
        }
        return parsePostfix();
    }

    private Expr parsePostfix() {
        var expr = parsePrimary();
        while (true) {
            if (check(DOT)) {
                advance();
                String field = expectIdentOrKeyword();
                if (check(LPAREN)) {
                    expr = parseCallArgs(new SelectorExpr(expr, field));
                } else {
                    expr = new SelectorExpr(expr, field);
                }
            } else if (check(QUESTION_DOT)) {
                advance();
                String field = expectIdentOrKeyword();
                CallExpr call = null;
                if (check(LPAREN)) {
                    call = (CallExpr) parseCallArgs(new SelectorExpr(expr, field));
                }
                expr = new SafeNavExpr(expr, field, call);
            } else if (check(LBRACKET)) {
                advance();
                var index = parseExpr();
                expect(RBRACKET);
                expr = new IndexExpr(expr, index);
            } else if (check(LPAREN)) {
                expr = parseCallArgs(expr);
            } else {
                break;
            }
        }
        return expr;
    }

    private Expr parseCallArgs(Expr callee) {
        expect(LPAREN);
        var args = new ArrayList<Expr>();
        var namedArgs = new ArrayList<NamedArg>();
        if (!check(RPAREN)) {
            parseCallArg(args, namedArgs);
            while (match(COMMA)) {
                if (check(RPAREN)) break;
                parseCallArg(args, namedArgs);
            }
        }
        expect(RPAREN);
        return new CallExpr(callee, args, namedArgs, List.of(), false);
    }

    private void parseCallArg(List<Expr> args, List<NamedArg> namedArgs) {
        // Check for named arg: name: value
        if (check(IDENT) && peekAt(1).type() == COLON) {
            String name = advance().literal();
            advance(); // :
            namedArgs.add(new NamedArg(name, parseExpr()));
        } else {
            args.add(parseExpr());
        }
    }

    private Expr parsePrimary() {
        var tok = peek();
        return switch (tok.type()) {
            case INT_LIT -> { advance(); yield new IntLit(tok.literal()); }
            case FLOAT_LIT -> { advance(); yield new FloatLit(tok.literal()); }
            case STRING_LIT -> { advance(); yield new StringLit(tok.literal()); }
            case INTERP_STRING -> { advance(); yield parseInterpString(tok.literal()); }
            case RAW_STRING -> { advance(); yield new RawStringLit(tok.literal()); }
            case BOOL_LIT -> { advance(); yield new BoolLit(tok.literal().equals("true")); }
            case NULL -> { advance(); yield new NullLit(); }
            case THIS -> { advance(); yield new ThisExpr(); }
            case SUPER -> {
                advance();
                expect(LPAREN);
                var args = new ArrayList<Expr>();
                if (!check(RPAREN)) {
                    args.add(parseExpr());
                    while (match(COMMA)) args.add(parseExpr());
                }
                expect(RPAREN);
                yield new SuperCallExpr(args);
            }
            case NEW -> {
                advance();
                // new Type<T>(args)
                String name = expect(IDENT).literal();
                while (check(DOT) && isIdentLike(peekAt(1).type())) {
                    advance();
                    name += "." + advance().literal();
                }
                var typeArgs = new ArrayList<String>();
                if (match(LT)) {
                    typeArgs.add(formatType(parseType()));
                    while (match(COMMA)) typeArgs.add(formatType(parseType()));
                    expect(GT);
                }
                var callExpr = (CallExpr) parseCallArgs(new Ident(name));
                yield new CallExpr(callExpr.callee(), callExpr.args(), callExpr.namedArgs(), typeArgs, true);
            }
            case SPAWN -> {
                int line = tok.line();
                advance();
                var body = parseBlock();
                OrHandler orHandler = null;
                if (check(OR)) orHandler = parseOrHandler();
                yield new SpawnExpr(line, body, orHandler);
            }
            case MATCH -> parseMatchExpr();
            case IDENT, PRINT, DATA -> {
                // Check for lambda: name -> expr
                if (peekAt(1).type() == ARROW) yield parseLambda();
                else { advance(); yield new Ident(tok.literal()); }
            }
            case LPAREN -> parseParenOrLambda();
            case LBRACKET -> parseListLit();
            case LBRACE -> parseMapLit();
            default -> {
                error("unexpected token " + tok.type() + " (" + tok.literal() + ") in expression");
                advance();
                yield new Ident("__error__");
            }
        };
    }

    // --- Lambda, list, map, interpolation ------------------------------------

    private Expr parseLambda() {
        String paramName = advance().literal();
        advance(); // ->
        var param = new ParamDecl(paramName, null, null, false);
        if (check(LBRACE)) {
            var body = parseBlock();
            return new LambdaExpr(List.of(param), body);
        }
        var expr = parseExpr();
        return new LambdaExpr(List.of(param),
            new BlockStmt(List.of(new ReturnStmt(0, expr))));
    }

    private Expr parseParenOrLambda() {
        // Could be: (expr), tuple, or (params) -> lambda
        // Try lambda first
        int saved = pos;
        if (tryParseLambdaParams()) {
            pos = saved;
            return parseMultiParamLambda();
        }
        pos = saved;

        // Parenthesized expression or tuple
        advance(); // (
        var expr = parseExpr();
        if (match(COMMA)) {
            // Tuple
            var elements = new ArrayList<Expr>();
            elements.add(expr);
            elements.add(parseExpr());
            while (match(COMMA)) elements.add(parseExpr());
            expect(RPAREN);
            return new TupleLit(elements);
        }
        expect(RPAREN);
        return expr;
    }

    private boolean tryParseLambdaParams() {
        if (!match(LPAREN)) return false;
        if (match(RPAREN)) return check(ARROW);
        // Skip params
        int depth = 1;
        while (depth > 0 && !check(EOF)) {
            if (check(LPAREN)) depth++;
            else if (check(RPAREN)) depth--;
            advance();
        }
        return check(ARROW);
    }

    private Expr parseMultiParamLambda() {
        expect(LPAREN);
        var params = new ArrayList<ParamDecl>();
        if (!check(RPAREN)) {
            params.add(parseLambdaParam());
            while (match(COMMA)) params.add(parseLambdaParam());
        }
        expect(RPAREN);
        expect(ARROW);
        if (check(LBRACE)) {
            return new LambdaExpr(params, parseBlock());
        }
        var expr = parseExpr();
        return new LambdaExpr(params,
            new BlockStmt(List.of(new ReturnStmt(0, expr))));
    }

    private ParamDecl parseLambdaParam() {
        TypeExpr type = null;
        String name;
        if (isTypeStart() && peekAt(1).type() == IDENT) {
            type = parseType();
            name = expect(IDENT).literal();
        } else {
            name = expect(IDENT).literal();
        }
        return new ParamDecl(name, type, null, false);
    }

    private Expr parseListLit() {
        advance(); // [
        var elements = new ArrayList<Expr>();
        if (!check(RBRACKET)) {
            elements.add(parseExpr());
            while (match(COMMA)) {
                if (check(RBRACKET)) break;
                elements.add(parseExpr());
            }
        }
        expect(RBRACKET);
        return new ListLit(elements);
    }

    private Expr parseMapLit() {
        advance(); // {
        var keys = new ArrayList<Expr>();
        var values = new ArrayList<Expr>();
        if (!check(RBRACE)) {
            keys.add(parseExpr());
            expect(COLON);
            values.add(parseExpr());
            while (match(COMMA)) {
                if (check(RBRACE)) break;
                keys.add(parseExpr());
                expect(COLON);
                values.add(parseExpr());
            }
        }
        expect(RBRACE);
        return new MapLit(keys, values);
    }

    private Expr parseInterpString(String raw) {
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
                // Parse the interpolated expression
                var lexer = new Lexer(exprStr.toString());
                var tokens = lexer.tokenize();
                var parser = new Parser(tokens);
                parts.add(parser.parseExpr());
            } else {
                sb.append(ch);
                i++;
            }
        }
        if (!sb.isEmpty()) parts.add(new StringLit(sb.toString()));
        return new StringInterpLit(parts);
    }

    private Expr parseMatchExpr() {
        expect(MATCH);
        var subject = parseExpr();
        expect(LBRACE);
        var cases = new ArrayList<MatchExprCase>();
        while (!check(RBRACE) && !check(EOF)) {
            skipSemis();
            if (check(RBRACE)) break;
            expect(CASE);
            Expr pattern = null;
            if (!check(IDENT) || !peek().literal().equals("_")) {
                pattern = parseExpr();
            } else {
                advance();
            }
            expect(ARROW);
            var value = parseExpr();
            cases.add(new MatchExprCase(pattern, value));
            skipSemis();
        }
        expect(RBRACE);
        return new MatchExpr(subject, cases);
    }

    // --- Helpers --------------------------------------------------------------

    private String formatType(TypeExpr type) {
        return switch (type) {
            case SimpleType s -> s.name();
            case GenericType g -> g.name() + "<" + String.join(", ", g.typeArgs().stream().map(this::formatType).toList()) + ">";
            case ArrayType a -> formatType(a.elementType()) + "[]";
            case OptionalType o -> formatType(o.inner()) + "?";
            case FuncType f -> "Func";
        };
    }
}
