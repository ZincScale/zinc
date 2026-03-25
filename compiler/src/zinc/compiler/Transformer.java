// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import com.github.javaparser.ast.*;
import com.github.javaparser.ast.body.*;
import com.github.javaparser.ast.expr.*;
import com.github.javaparser.ast.stmt.*;
import com.github.javaparser.ast.type.*;
import com.github.javaparser.ast.Modifier.Keyword;

import java.util.List;

// Zinc AST types — use Ast. prefix for types that clash with JavaParser
import zinc.compiler.Ast.Program;
import zinc.compiler.Ast.FnDecl;
import zinc.compiler.Ast.ClassDecl;
import zinc.compiler.Ast.InterfaceDecl;
import zinc.compiler.Ast.DataClassDecl;
import zinc.compiler.Ast.SealedClassDecl;
import zinc.compiler.Ast.EnumDecl;
import zinc.compiler.Ast.ConstDecl;
import zinc.compiler.Ast.Stmt;
import zinc.compiler.Ast.Expr;
import zinc.compiler.Ast.IntLit;
import zinc.compiler.Ast.FloatLit;
import zinc.compiler.Ast.StringLit;
import zinc.compiler.Ast.BoolLit;
import zinc.compiler.Ast.NullLit;
import zinc.compiler.Ast.Ident;
import zinc.compiler.Ast.ThisExpr;
import zinc.compiler.Ast.CallExpr;
import zinc.compiler.Ast.SelectorExpr;
import zinc.compiler.Ast.IndexExpr;
import zinc.compiler.Ast.ListLit;
import zinc.compiler.Ast.StringInterpLit;
import zinc.compiler.Ast.RangeExpr;
import zinc.compiler.Ast.MethodSig;
import zinc.compiler.Ast.CtorDecl;
import zinc.compiler.Ast.FieldDecl;
import zinc.compiler.Ast.ParamDecl;
import zinc.compiler.Ast.Annotation;

/**
 * Transforms Zinc AST into JavaParser AST.
 * Each Zinc node maps to one or more Java AST nodes.
 */
public class Transformer {

    private String className = "Main";
    private java.util.Map<String, TypeInfo> resolvedTypes = java.util.Map.of();

    public Transformer() {}

    public Transformer(String className) {
        this.className = className;
    }

    public Transformer(String className, java.util.Map<String, TypeInfo> resolvedTypes) {
        this.className = className;
        this.resolvedTypes = resolvedTypes;
    }

    // --- Entry point ---------------------------------------------------------

    /**
     * Transforms a Zinc program into multiple CompilationUnits — one per top-level type.
     * Script mode programs get a single Main class.
     */
    public Result<List<CompilationUnit>> transformAll(Program program) {
        var units = new java.util.ArrayList<CompilationUnit>();

        // Script mode — single Main class with everything
        if (!program.stmts().isEmpty()) {
            var result = transform(program);
            if (result.isErr()) return Result.err(((Result.Err<?>) result).errors());
            units.add(result.unwrap());
            return Result.ok(units);
        }

        // Multi-type mode — one CU per top-level declaration
        for (var decl : program.decls()) {
            var cu = new CompilationUnit();
            if (program.pkg() != null) cu.setPackageDeclaration(program.pkg().path());
            for (var imp : program.imports()) cu.addImport(imp.path());

            switch (decl) {
                case ClassDecl cls -> cu.addType(transformClassDecl(cls));
                case InterfaceDecl iface -> cu.addType(transformInterfaceDecl(iface));
                case DataClassDecl data -> cu.addType(transformDataClassDecl(data));
                case SealedClassDecl sealed -> cu.addType(transformSealedClassDecl(sealed));
                case EnumDecl en -> cu.addType(transformEnumDecl(en));
                case FnDecl fn -> {
                    // Top-level function without script — wrap in utility class
                    var utilClass = cu.addClass(className, Keyword.PUBLIC);
                    utilClass.addMember(transformFnDecl(fn));
                }
                case ConstDecl c -> {}
            }
            units.add(cu);
        }

        return Result.ok(units);
    }

    public Result<CompilationUnit> transform(Program program) {
        var cu = new CompilationUnit();

        // Package
        if (program.pkg() != null) {
            cu.setPackageDeclaration(program.pkg().path());
        }

        // Imports
        for (var imp : program.imports()) {
            cu.addImport(imp.path());
        }

        // If there are top-level statements (script mode), wrap in a Main class
        if (!program.stmts().isEmpty()) {
            var mainClass = cu.addClass(className, Keyword.PUBLIC);
            var mainMethod = mainClass.addMethod("main", Keyword.PUBLIC, Keyword.STATIC);
            mainMethod.addParameter("String[]", "args");
            mainMethod.setThrownExceptions(new NodeList<>(new ClassOrInterfaceType(null, "Exception")));
            var body = new BlockStmt();
            for (var stmt : program.stmts()) {
                for (var jStmt : transformStmt(stmt)) {
                    body.addStatement(jStmt);
                }
            }
            mainMethod.setBody(body);

            // Add top-level functions as static methods on Main
            for (var decl : program.decls()) {
                if (decl instanceof FnDecl fn) {
                    mainClass.addMember(transformFnDecl(fn));
                }
            }
        }

        // Top-level declarations (non-script mode)
        for (var decl : program.decls()) {
            switch (decl) {
                case ClassDecl cls -> cu.addType(transformClassDecl(cls));
                case InterfaceDecl iface -> cu.addType(transformInterfaceDecl(iface));
                case DataClassDecl data -> cu.addType(transformDataClassDecl(data));
                case SealedClassDecl sealed -> cu.addType(transformSealedClassDecl(sealed));
                case EnumDecl en -> cu.addType(transformEnumDecl(en));
                case FnDecl fn -> {
                    // Already handled in script mode above
                    if (program.stmts().isEmpty()) {
                        // Non-script: need a class to hold static functions
                        // This is handled by the caller or by wrapping
                    }
                }
                case ConstDecl c -> {} // handled as static fields
            }
        }

        return Result.ok(cu);
    }

    // --- Declarations --------------------------------------------------------

    private MethodDeclaration transformFnDecl(FnDecl fn) {
        var method = new MethodDeclaration();
        method.setName(fn.name());
        method.addModifier(Keyword.PUBLIC, Keyword.STATIC);
        method.setType(fn.returnType() != null ? transformType(fn.returnType()) : new VoidType());

        for (var param : fn.params()) {
            var type = param.type() != null ? transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
            method.addParameter(type, param.name());
        }

        if (fn.body() != null) {
            method.setBody(transformBlock(fn.body()));
        }

        return method;
    }

    private ClassOrInterfaceDeclaration transformClassDecl(ClassDecl cls) {
        var jClass = new ClassOrInterfaceDeclaration();
        jClass.setName(cls.name());
        jClass.addModifier(Keyword.PUBLIC);
        if (cls.isAbstract()) jClass.addModifier(Keyword.ABSTRACT);

        for (var parent : cls.parents()) {
            jClass.addImplementedType(parent);
        }

        // Fields
        for (var field : cls.fields()) {
            var type = field.type() != null ? transformType(field.type()) : new ClassOrInterfaceType(null, "Object");
            var jField = jClass.addField(type, field.name(), field.isPub() ? Keyword.PUBLIC : Keyword.PRIVATE);
            if (field.isInit()) jField.addModifier(Keyword.FINAL);
            if (field.defaultValue() != null) {
                jField.getVariable(0).setInitializer(transformExpr(field.defaultValue()));
            }

            // Getters for pub/readonly/init fields
            if (field.isPub() || field.isReadonly() || field.isInit()) {
                var getter = jClass.addMethod("get" + capitalize(field.name()), Keyword.PUBLIC);
                getter.setType(type);
                getter.setBody(new BlockStmt().addStatement(new ReturnStmt(new NameExpr("this." + field.name()))));
            }
            // Setters for pub fields only
            if (field.isPub() && !field.isReadonly() && !field.isInit()) {
                var setter = jClass.addMethod("set" + capitalize(field.name()), Keyword.PUBLIC);
                setter.setType(new VoidType());
                setter.addParameter(type, field.name());
                setter.setBody(new BlockStmt().addStatement(
                    new ExpressionStmt(new AssignExpr(
                        new NameExpr("this." + field.name()),
                        new NameExpr(field.name()),
                        AssignExpr.Operator.ASSIGN))));
            }
        }

        // Constructors
        for (var ctor : cls.ctors()) {
            var jCtor = jClass.addConstructor(Keyword.PUBLIC);
            for (var param : ctor.params()) {
                var type = param.type() != null ? transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
                jCtor.addParameter(type, param.name());
            }
            var body = transformBlock(ctor.body());
            // Prepend super() if present
            if (!ctor.superArgs().isEmpty()) {
                var superArgs = new NodeList<Expression>();
                for (var arg : ctor.superArgs()) superArgs.add(transformExpr(arg));
                body.getStatements().addFirst(new ExpressionStmt(
                    new MethodCallExpr(null, "super", superArgs)));
            }
            jCtor.setBody(body);
        }

        // Methods
        for (var method : cls.methods()) {
            jClass.addMember(transformMethodDecl(method));
        }

        return jClass;
    }

    private ClassOrInterfaceDeclaration transformInterfaceDecl(InterfaceDecl iface) {
        var jIface = new ClassOrInterfaceDeclaration();
        jIface.setInterface(true);
        jIface.setName(iface.name());
        jIface.addModifier(Keyword.PUBLIC);

        for (var sig : iface.methods()) {
            var method = new MethodDeclaration();
            method.setName(sig.name());
            method.setType(sig.returnType() != null ? transformType(sig.returnType()) : new VoidType());
            method.removeBody(); // interface methods have no body
            for (var param : sig.params()) {
                var type = param.type() != null ? transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
                method.addParameter(type, param.name());
            }
            jIface.addMember(method);
        }

        return jIface;
    }

    private ClassOrInterfaceDeclaration transformDataClassDecl(DataClassDecl data) {
        // Data class → Java record-like class with constructor, fields, equals, hashCode, toString
        var jClass = new ClassOrInterfaceDeclaration();
        jClass.setName(data.name());
        jClass.addModifier(Keyword.PUBLIC);

        // Fields (final)
        for (var param : data.params()) {
            var type = param.type() != null ? transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
            var field = jClass.addField(type, param.name(), Keyword.PRIVATE, Keyword.FINAL);
        }

        // Constructor
        var ctor = jClass.addConstructor(Keyword.PUBLIC);
        var ctorBody = new BlockStmt();
        for (var param : data.params()) {
            var type = param.type() != null ? transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
            ctor.addParameter(type, param.name());
            ctorBody.addStatement(new ExpressionStmt(new AssignExpr(
                new NameExpr("this." + param.name()),
                new NameExpr(param.name()),
                AssignExpr.Operator.ASSIGN)));
        }
        ctor.setBody(ctorBody);

        // Getters
        for (var param : data.params()) {
            var type = param.type() != null ? transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
            var getter = jClass.addMethod(param.name(), Keyword.PUBLIC);
            getter.setType(type);
            getter.setBody(new BlockStmt().addStatement(new ReturnStmt(new NameExpr("this." + param.name()))));
        }

        // Methods
        for (var method : data.methods()) {
            jClass.addMember(transformMethodDecl(method));
        }

        return jClass;
    }

    private ClassOrInterfaceDeclaration transformSealedClassDecl(SealedClassDecl sealed) {
        var jClass = new ClassOrInterfaceDeclaration();
        jClass.setName(sealed.name());
        jClass.addModifier(Keyword.PUBLIC, Keyword.ABSTRACT);
        // Java sealed classes need permits — add variants
        // For now, generate as abstract class
        return jClass;
    }

    private EnumDeclaration transformEnumDecl(EnumDecl en) {
        var jEnum = new EnumDeclaration();
        jEnum.setName(en.name());
        jEnum.addModifier(Keyword.PUBLIC);
        for (var variant : en.variants()) {
            jEnum.addEnumConstant(variant);
        }
        return jEnum;
    }

    private MethodDeclaration transformMethodDecl(Ast.MethodDecl method) {
        var jMethod = new MethodDeclaration();
        jMethod.setName(method.name());
        if (method.isPub()) jMethod.addModifier(Keyword.PUBLIC);
        else jMethod.addModifier(Keyword.PRIVATE);
        if (method.isStatic()) jMethod.addModifier(Keyword.STATIC);
        if (method.isAbstract()) jMethod.addModifier(Keyword.ABSTRACT);
        jMethod.setType(method.returnType() != null ? transformType(method.returnType()) : new VoidType());

        for (var param : method.params()) {
            var type = param.type() != null ? transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
            jMethod.addParameter(type, param.name());
        }

        if (method.body() != null) {
            jMethod.setBody(transformBlock(method.body()));
        } else {
            jMethod.removeBody();
        }

        return jMethod;
    }

    // --- Types ---------------------------------------------------------------

    private Type transformType(Ast.TypeExpr type) {
        return switch (type) {
            case Ast.SimpleType s -> switch (s.name()) {
                case "int" -> PrimitiveType.intType();
                case "long" -> PrimitiveType.longType();
                case "double" -> PrimitiveType.doubleType();
                case "float" -> PrimitiveType.floatType();
                case "boolean" -> PrimitiveType.booleanType();
                case "byte" -> PrimitiveType.byteType();
                case "char" -> PrimitiveType.charType();
                case "short" -> PrimitiveType.shortType();
                case "void" -> new VoidType();
                default -> new ClassOrInterfaceType(null, s.name());
            };
            case Ast.GenericType g -> {
                var base = new ClassOrInterfaceType(null, g.name());
                var args = new NodeList<Type>();
                for (var arg : g.typeArgs()) args.add(transformType(arg));
                base.setTypeArguments(args);
                yield base;
            }
            case Ast.ArrayType a -> new com.github.javaparser.ast.type.ArrayType(transformType(a.elementType()));
            case Ast.OptionalType o -> transformType(o.inner()); // nullable in Java
            case Ast.FuncType f -> new ClassOrInterfaceType(null, "Object"); // simplified
        };
    }

    // --- Statements ----------------------------------------------------------

    private com.github.javaparser.ast.stmt.BlockStmt transformBlock(Ast.BlockStmt block) {
        var jBlock = new BlockStmt();
        for (var stmt : block.stmts()) {
            for (var jStmt : transformStmt(stmt)) {
                jBlock.addStatement(jStmt);
            }
        }
        return jBlock;
    }

    private List<Statement> transformStmt(Stmt stmt) {
        return switch (stmt) {
            case Ast.VarStmt v -> {
                if (v.orHandler() != null && v.value() != null) yield transformVarWithOrHandlerStmts(v);
                else yield List.of(transformVarStmt(v));
            }
            case Ast.AssignStmt a -> List.of(transformAssignStmt(a));
            case Ast.ReturnStmt r -> List.of(transformReturnStmt(r));
            case Ast.IfStmt i -> List.of(transformIfStmt(i));
            case Ast.ForStmt f -> List.of(transformForStmt(f));
            case Ast.WhileStmt w -> List.of(new WhileStmt(transformExpr(w.cond()), transformBlock(w.body())));
            case Ast.ExprStmt e -> {
                if (e.orHandler() != null) {
                    yield List.of(transformExprStmtWithOrHandler(e));
                }
                yield List.of(new ExpressionStmt(transformExpr(e.expr())));
            }
            case Ast.BreakStmt b -> List.of(new BreakStmt());
            case Ast.ContinueStmt c -> List.of(new ContinueStmt());
            case Ast.BlockStmt b -> List.of(transformBlock(b));
            case Ast.ParallelForStmt p -> List.of(transformParallelFor(p));
            case Ast.ConcurrentStmt c -> List.of(transformConcurrent(c));
            case Ast.TimeoutStmt t -> List.of(transformTimeout(t));
            case Ast.WithStmt w -> List.of(transformWith(w));
            case Ast.DeferStmt d -> List.of(new ExpressionStmt(transformExpr(d.expr()))); // TODO: proper defer
            case FnDecl fn -> List.of(); // nested fn — handled elsewhere
            default -> List.of(new ExpressionStmt(new StringLiteralExpr("/* unsupported: " + stmt.getClass().getSimpleName() + " */")));
        };
    }

    private Statement transformVarStmt(Ast.VarStmt v) {
        Expression init = v.value() != null ? transformExpr(v.value()) : null;
        if (v.type() != null) {
            var type = transformType(v.type());
            var decl = new VariableDeclarationExpr(type, v.name());
            if (init != null) decl.getVariable(0).setInitializer(init);
            return new ExpressionStmt(decl);
        }
        // var inference
        var decl = new VariableDeclarationExpr(new VarType(), v.name());
        if (init != null) decl.getVariable(0).setInitializer(init);
        return new ExpressionStmt(decl);
    }

    /**
     * var x = call() or { default }
     * Returns multiple statements to avoid scoping issues:
     *   Type x;
     *   try { x = call(); } catch (Exception err) { x = default; }
     */
    private List<Statement> transformVarWithOrHandlerStmts(Ast.VarStmt v) {
        var stmts = new java.util.ArrayList<Statement>();

        // Use explicit type, resolved type from typechecker, or Object fallback
        Type javaType;
        if (v.type() != null) {
            javaType = transformType(v.type());
        } else {
            var resolved = resolvedTypes.get(v.line() + ":" + v.name());
            if (resolved != null && !resolved.name().equals("any")) {
                javaType = typeInfoToJavaType(resolved);
            } else {
                javaType = new ClassOrInterfaceType(null, "Object");
            }
        }
        stmts.add(new ExpressionStmt(new VariableDeclarationExpr(javaType, v.name())));

        // Try block: x = call();
        var tryBody = new BlockStmt();
        tryBody.addStatement(new ExpressionStmt(new AssignExpr(
            new NameExpr(v.name()), transformExpr(v.value()), AssignExpr.Operator.ASSIGN)));

        // Catch block: or handler body
        var catchBody = new BlockStmt();
        if (v.orHandler().body() != null) {
            var handlerStmts = v.orHandler().body().stmts();
            if (handlerStmts.size() == 1 && handlerStmts.getFirst() instanceof Ast.ExprStmt es) {
                // Single expression or-handler: or { defaultExpr } → x = defaultExpr
                catchBody.addStatement(new ExpressionStmt(new AssignExpr(
                    new NameExpr(v.name()), transformExpr(es.expr()), AssignExpr.Operator.ASSIGN)));
            } else {
                // Block or-handler — last expression becomes assignment to var
                for (int idx = 0; idx < handlerStmts.size(); idx++) {
                    var stmt = handlerStmts.get(idx);
                    boolean isLast = (idx == handlerStmts.size() - 1);
                    if (isLast && stmt instanceof Ast.ExprStmt es && es.orHandler() == null) {
                        // Last expression in block = fallback value
                        catchBody.addStatement(new ExpressionStmt(new AssignExpr(
                            new NameExpr(v.name()), transformExpr(es.expr()), AssignExpr.Operator.ASSIGN)));
                    } else {
                        for (var jStmt : transformStmt(stmt)) {
                            catchBody.addStatement(jStmt);
                        }
                    }
                }
            }
        }

        var catchClause = new CatchClause(
            new Parameter(new ClassOrInterfaceType(null, "Exception"), "err"),
            catchBody);
        stmts.add(new TryStmt(tryBody, new NodeList<>(catchClause), null));

        return stmts;
    }

    private Statement transformAssignStmt(Ast.AssignStmt a) {
        var op = switch (a.op()) {
            case "=" -> AssignExpr.Operator.ASSIGN;
            case "+=" -> AssignExpr.Operator.PLUS;
            case "-=" -> AssignExpr.Operator.MINUS;
            case "*=" -> AssignExpr.Operator.MULTIPLY;
            case "/=" -> AssignExpr.Operator.DIVIDE;
            default -> AssignExpr.Operator.ASSIGN;
        };
        return new ExpressionStmt(new AssignExpr(transformExpr(a.target()), transformExpr(a.value()), op));
    }

    private Statement transformReturnStmt(Ast.ReturnStmt r) {
        if (r.value() == null) return new ReturnStmt();

        // return Error(expr) → throw new RuntimeException(expr)
        // This is the Java boundary — Zinc errors become Java exceptions for propagation
        if (r.value() instanceof CallExpr call
            && call.callee() instanceof Ident id
            && id.name().equals("Error")) {
            if (!call.args().isEmpty()) {
                var arg = call.args().getFirst();
                // return Error(CustomType(...)) → throw new CustomType(...)
                if (arg instanceof CallExpr innerCall
                    && innerCall.callee() instanceof Ident innerId
                    && Character.isUpperCase(innerId.name().charAt(0))) {
                    var args = new NodeList<Expression>();
                    for (var a : innerCall.args()) args.add(transformExpr(a));
                    return new ThrowStmt(new ObjectCreationExpr(null,
                        new ClassOrInterfaceType(null, innerId.name()), args));
                }
                // return Error(err) or return Error("msg") → throw new RuntimeException(...)
                return new ThrowStmt(new ObjectCreationExpr(null,
                    new ClassOrInterfaceType(null, "RuntimeException"),
                    new NodeList<>(transformExpr(arg))));
            }
            return new ThrowStmt(new ObjectCreationExpr(null,
                new ClassOrInterfaceType(null, "RuntimeException"),
                new NodeList<>(new StringLiteralExpr("error"))));
        }

        return new ReturnStmt(transformExpr(r.value()));
    }

    /**
     * call() or { handler }
     * → try { call(); } catch (Exception err) { handler; }
     */
    private Statement transformExprStmtWithOrHandler(Ast.ExprStmt e) {
        var tryBody = new BlockStmt();
        tryBody.addStatement(new ExpressionStmt(transformExpr(e.expr())));

        var catchBody = new BlockStmt();
        if (e.orHandler().body() != null) {
            for (var stmt : e.orHandler().body().stmts()) {
                for (var jStmt : transformStmt(stmt)) {
                    catchBody.addStatement(jStmt);
                }
            }
        }

        var catchClause = new CatchClause(
            new Parameter(new ClassOrInterfaceType(null, "Exception"), "err"),
            catchBody);
        return new TryStmt(tryBody, new NodeList<>(catchClause), null);
    }

    // --- Concurrency ---------------------------------------------------------

    /**
     * parallel for item in items { body }
     * → try (var _scope = StructuredTaskScope.open(Joiner.awaitAllSuccessfulOrThrow())) {
     *       for (var item : items) { _scope.fork(() -> { body; return null; }); }
     *       _scope.join();
     *   }
     */
    private Statement transformParallelFor(Ast.ParallelForStmt p) {
        var scopeType = "java.util.concurrent.StructuredTaskScope";
        var joiner = "java.util.concurrent.StructuredTaskScope.Joiner.awaitAllSuccessfulOrThrow()";

        // Build: _scope.fork(() -> { body; return null; })
        var lambdaBody = transformBlock(p.body());
        lambdaBody.addStatement(new ReturnStmt(new NullLiteralExpr()));
        var forkLambda = new com.github.javaparser.ast.expr.LambdaExpr(new NodeList<>(), lambdaBody);
        var forkCall = new MethodCallExpr(new NameExpr("_scope"), "fork", new NodeList<>(forkLambda));

        // Semaphore for bounded concurrency
        var outerBlock = new BlockStmt();
        if (p.max() > 0) {
            outerBlock.addStatement(parseStmt("var _semaphore = new java.util.concurrent.Semaphore(" + p.max() + ");"));
        }

        // for (var item : range) { _scope.fork(...) }
        var forBody = new BlockStmt();
        if (p.max() > 0) {
            forBody.addStatement(new ExpressionStmt(new MethodCallExpr(new NameExpr("_semaphore"), "acquire")));
            // Wrap fork in try-finally for semaphore release
            var tryBody = new BlockStmt();
            tryBody.addStatement(new ExpressionStmt(forkCall));
            var finallyBody = new BlockStmt();
            finallyBody.addStatement(new ExpressionStmt(new MethodCallExpr(new NameExpr("_semaphore"), "release")));
            forBody.addStatement(new TryStmt(tryBody, new NodeList<>(), finallyBody));
        } else {
            forBody.addStatement(new ExpressionStmt(forkCall));
        }

        var forEach = new ForEachStmt(
            new VariableDeclarationExpr(new VarType(), p.item()),
            transformExpr(p.range()), forBody);

        // try (var _scope = ...) { forEach; _scope.join(); }
        var tryBody = new BlockStmt();
        tryBody.addStatement(forEach);
        tryBody.addStatement(new ExpressionStmt(new MethodCallExpr(new NameExpr("_scope"), "join")));

        var scopeInit = new VariableDeclarationExpr(new VarType(), "_scope");
        scopeInit.getVariable(0).setInitializer(parseExpr(scopeType + ".open(" + joiner + ")"));

        if (p.orHandler() != null) {
            var catchBody = new BlockStmt();
            if (p.orHandler().body() != null) {
                for (var stmt : p.orHandler().body().stmts()) {
                    for (var jStmt : transformStmt(stmt)) catchBody.addStatement(jStmt);
                }
            }
            var catchClause = new CatchClause(
                new Parameter(new ClassOrInterfaceType(null, "Exception"), "err"), catchBody);
            outerBlock.addStatement(new TryStmt(
                new NodeList<>(scopeInit), tryBody,
                new NodeList<>(catchClause), null));
        } else {
            outerBlock.addStatement(new TryStmt(
                new NodeList<>(scopeInit), tryBody,
                new NodeList<>(), null));
        }

        return outerBlock;
    }

    /**
     * concurrent { task1; task2 }
     * → try (var _scope = StructuredTaskScope.open(Joiner.awaitAllSuccessfulOrThrow())) {
     *       _scope.fork(() -> task1);
     *       _scope.fork(() -> task2);
     *       _scope.join();
     *   }
     */
    private Statement transformConcurrent(Ast.ConcurrentStmt c) {
        var joiner = c.firstOnly()
            ? "java.util.concurrent.StructuredTaskScope.Joiner.anySuccessfulResultOrThrow()"
            : "java.util.concurrent.StructuredTaskScope.Joiner.awaitAllSuccessfulOrThrow()";

        var tryBody = new BlockStmt();
        for (var task : c.tasks()) {
            var lambdaBody = new BlockStmt();
            lambdaBody.addStatement(new ReturnStmt(transformExpr(task)));
            var lambda = new com.github.javaparser.ast.expr.LambdaExpr(new NodeList<>(), lambdaBody);
            tryBody.addStatement(new ExpressionStmt(
                new MethodCallExpr(new NameExpr("_scope"), "fork", new NodeList<>(lambda))));
        }
        tryBody.addStatement(new ExpressionStmt(new MethodCallExpr(new NameExpr("_scope"), "join")));

        var scopeInit = new VariableDeclarationExpr(new VarType(), "_scope");
        scopeInit.getVariable(0).setInitializer(parseExpr(
            "java.util.concurrent.StructuredTaskScope.open(" + joiner + ")"));

        if (c.orHandler() != null) {
            var catchBody = new BlockStmt();
            if (c.orHandler().body() != null) {
                for (var stmt : c.orHandler().body().stmts()) {
                    for (var jStmt : transformStmt(stmt)) catchBody.addStatement(jStmt);
                }
            }
            var catchClause = new CatchClause(
                new Parameter(new ClassOrInterfaceType(null, "Exception"), "err"), catchBody);
            return new TryStmt(new NodeList<>(scopeInit), tryBody, new NodeList<>(catchClause), null);
        }

        return new TryStmt(new NodeList<>(scopeInit), tryBody, new NodeList<>(), null);
    }

    /**
     * timeout(dur) { body } or { fallback }
     * → try (var _scope = StructuredTaskScope.open()) { ... joinUntil ... }
     */
    private Statement transformTimeout(Ast.TimeoutStmt t) {
        var tryBody = new BlockStmt();
        var lambdaBody = transformBlock(t.body());
        lambdaBody.addStatement(new ReturnStmt(new NullLiteralExpr()));
        var lambda = new com.github.javaparser.ast.expr.LambdaExpr(new NodeList<>(), lambdaBody);
        tryBody.addStatement(new ExpressionStmt(
            new MethodCallExpr(new NameExpr("_scope"), "fork", new NodeList<>(lambda))));
        tryBody.addStatement(new ExpressionStmt(
            new MethodCallExpr(new NameExpr("_scope"), "joinUntil",
                new NodeList<>(parseExpr("java.time.Instant.now().plus(" + transformExpr(t.duration()) + ")")))));

        var scopeInit = new VariableDeclarationExpr(new VarType(), "_scope");
        scopeInit.getVariable(0).setInitializer(parseExpr("java.util.concurrent.StructuredTaskScope.open()"));

        if (t.orHandler() != null) {
            var catchBody = new BlockStmt();
            if (t.orHandler().body() != null) {
                for (var stmt : t.orHandler().body().stmts()) {
                    for (var jStmt : transformStmt(stmt)) catchBody.addStatement(jStmt);
                }
            }
            var catchClause = new CatchClause(
                new Parameter(new ClassOrInterfaceType(null, "java.util.concurrent.TimeoutException"), "err"),
                catchBody);
            return new TryStmt(new NodeList<>(scopeInit), tryBody, new NodeList<>(catchClause), null);
        }

        return new TryStmt(new NodeList<>(scopeInit), tryBody, new NodeList<>(), null);
    }

    /**
     * with expr as name { body } → try (var name = expr) { body }
     */
    private Statement transformWith(Ast.WithStmt w) {
        var tryBody = transformBlock(w.body());
        var resources = new NodeList<Expression>();
        for (var res : w.resources()) {
            var decl = new VariableDeclarationExpr(new VarType(), res.name());
            decl.getVariable(0).setInitializer(transformExpr(res.value()));
            resources.add(decl);
        }
        return new TryStmt(resources, tryBody, new NodeList<>(), null);
    }

    // --- Control flow --------------------------------------------------------

    private Statement transformIfStmt(Ast.IfStmt i) {
        var jIf = new IfStmt();
        jIf.setCondition(transformExpr(i.cond()));
        jIf.setThenStmt(transformBlock(i.then()));
        if (i.elseStmt() != null) {
            if (i.elseStmt() instanceof Ast.IfStmt elseIf) {
                jIf.setElseStmt((IfStmt) transformIfStmt(elseIf));
            } else if (i.elseStmt() instanceof Ast.BlockStmt elseBlock) {
                jIf.setElseStmt(transformBlock(elseBlock));
            }
        }
        return jIf;
    }

    private Statement transformForStmt(Ast.ForStmt f) {
        if (f.isRange()) {
            // for item in range → for (var item : range)
            var forEach = new ForEachStmt();
            forEach.setVariable(new VariableDeclarationExpr(new VarType(), f.item()));
            forEach.setIterable(transformExpr(f.range()));
            forEach.setBody(transformBlock(f.body()));
            return forEach;
        }
        // C-style for — simplified
        return new BlockStmt(); // TODO: C-style for
    }

    // --- Expressions ---------------------------------------------------------

    private Expression transformExpr(Expr expr) {
        return switch (expr) {
            case IntLit i -> new IntegerLiteralExpr(i.value());
            case FloatLit f -> new DoubleLiteralExpr(f.value());
            case StringLit s -> new StringLiteralExpr(s.value());
            case BoolLit b -> new BooleanLiteralExpr(b.value());
            case NullLit n -> new NullLiteralExpr();
            case Ident id -> {
                if (id.name().equals("print")) yield new NameExpr("System.out.println");
                yield new NameExpr(id.name());
            }
            case ThisExpr t -> new com.github.javaparser.ast.expr.ThisExpr();
            case Ast.BinaryExpr bin -> transformBinaryExpr(bin);
            case Ast.UnaryExpr un -> transformUnaryExpr(un);
            case CallExpr call -> transformCallExpr(call);
            case SelectorExpr sel -> new FieldAccessExpr(transformExpr(sel.object()), sel.field());
            case IndexExpr idx -> new ArrayAccessExpr(transformExpr(idx.object()), transformExpr(idx.index()));
            case ListLit list -> {
                var init = new ArrayInitializerExpr();
                var values = new NodeList<Expression>();
                for (var el : list.elements()) values.add(transformExpr(el));
                init.setValues(values);
                yield new MethodCallExpr(new NameExpr("java.util.List"), "of",
                    new NodeList<>(list.elements().stream().map(this::transformExpr).toArray(Expression[]::new)));
            }
            case StringInterpLit interp -> transformInterpString(interp);
            case Ast.LambdaExpr lam -> transformLambda(lam);
            case Ast.SpawnExpr spawn -> transformSpawn(spawn);
            case RangeExpr range -> transformRange(range);
            default -> new NameExpr("/* unsupported: " + expr.getClass().getSimpleName() + " */");
        };
    }

    private Expression transformBinaryExpr(Ast.BinaryExpr bin) {
        var left = transformExpr(bin.left());
        var right = transformExpr(bin.right());
        var op = switch (bin.op()) {
            case "+" -> BinaryExpr.Operator.PLUS;
            case "-" -> BinaryExpr.Operator.MINUS;
            case "*" -> BinaryExpr.Operator.MULTIPLY;
            case "/" -> BinaryExpr.Operator.DIVIDE;
            case "%" -> BinaryExpr.Operator.REMAINDER;
            case "==" -> BinaryExpr.Operator.EQUALS;
            case "!=" -> BinaryExpr.Operator.NOT_EQUALS;
            case "<" -> BinaryExpr.Operator.LESS;
            case "<=" -> BinaryExpr.Operator.LESS_EQUALS;
            case ">" -> BinaryExpr.Operator.GREATER;
            case ">=" -> BinaryExpr.Operator.GREATER_EQUALS;
            case "&&" -> BinaryExpr.Operator.AND;
            case "||" -> BinaryExpr.Operator.OR;
            default -> BinaryExpr.Operator.PLUS;
        };
        return new com.github.javaparser.ast.expr.BinaryExpr(left, right, op);
    }

    private Expression transformUnaryExpr(Ast.UnaryExpr un) {
        var operand = transformExpr(un.operand());
        var op = switch (un.op()) {
            case "-" -> com.github.javaparser.ast.expr.UnaryExpr.Operator.MINUS;
            case "!" -> com.github.javaparser.ast.expr.UnaryExpr.Operator.LOGICAL_COMPLEMENT;
            default -> com.github.javaparser.ast.expr.UnaryExpr.Operator.MINUS;
        };
        return new com.github.javaparser.ast.expr.UnaryExpr(operand, op);
    }

    private Expression transformCallExpr(CallExpr call) {
        var args = new NodeList<Expression>();
        for (var arg : call.args()) args.add(transformExpr(arg));

        if (call.isNew()) {
            var type = new ClassOrInterfaceType(null, ((Ident) call.callee()).name());
            if (!call.typeArgs().isEmpty()) {
                var typeArgs = new NodeList<Type>();
                for (var ta : call.typeArgs()) typeArgs.add(new ClassOrInterfaceType(null, ta));
                type.setTypeArguments(typeArgs);
            }
            return new ObjectCreationExpr(null, type, args);
        }

        // Method call on object: obj.method(args)
        if (call.callee() instanceof SelectorExpr sel) {
            return new MethodCallExpr(transformExpr(sel.object()), sel.field(), args);
        }

        // Simple function call: func(args)
        if (call.callee() instanceof Ident id) {
            if (id.name().equals("print")) {
                return new MethodCallExpr(new NameExpr("System.out"), "println", args);
            }
            return new MethodCallExpr(null, id.name(), args);
        }

        return new MethodCallExpr(null, "unknown", args);
    }

    private Expression transformInterpString(StringInterpLit interp) {
        Expression result = null;
        for (var part : interp.parts()) {
            Expression expr;
            if (part instanceof StringLit s) {
                expr = new StringLiteralExpr(s.value());
            } else {
                expr = transformExpr(part);
            }
            if (result == null) {
                result = expr;
            } else {
                result = new com.github.javaparser.ast.expr.BinaryExpr(result, expr,
                    com.github.javaparser.ast.expr.BinaryExpr.Operator.PLUS);
            }
        }
        return result != null ? result : new StringLiteralExpr("");
    }

    private Expression transformLambda(Ast.LambdaExpr lam) {
        var params = new NodeList<Parameter>();
        for (var p : lam.params()) {
            if (p.type() != null) {
                params.add(new Parameter(transformType(p.type()), p.name()));
            } else {
                params.add(new Parameter(new UnknownType(), p.name()));
            }
        }
        var body = transformBlock(lam.body());
        return new com.github.javaparser.ast.expr.LambdaExpr(params, body);
    }

    private Expression transformRange(RangeExpr range) {
        // 1..5 → IntStream.range(1, 5)
        // 1..=5 → IntStream.rangeClosed(1, 5)
        String method = range.inclusive() ? "rangeClosed" : "range";
        return new MethodCallExpr(
            new NameExpr("java.util.stream.IntStream"), method,
            new NodeList<>(transformExpr(range.start()), transformExpr(range.end())));
    }

    // --- Helpers -------------------------------------------------------------

    /**
     * spawn { body } or { handler }
     * → CompletableFuture<Void> via inline supplier that starts a virtual thread
     */
    private Expression transformSpawn(Ast.SpawnExpr spawn) {
        var body = transformBlock(spawn.body());

        // Build or-handler code
        String orHandler = "";
        if (spawn.orHandler() != null && spawn.orHandler().body() != null) {
            var handlerBlock = transformBlock(spawn.orHandler().body());
            orHandler = handlerBlock.toString().replace("{", "").replace("}", "").trim();
        }

        // Build: (() -> { var _f = new CompletableFuture<Void>();
        //   Thread.ofVirtual().start(() -> {
        //     try { body; _f.complete(null); }
        //     catch (Exception err) { orHandler; _f.completeExceptionally(err); }
        //   }); return _f; }).get()
        var tryBody = new BlockStmt();
        for (var stmt : body.getStatements()) tryBody.addStatement(stmt.clone());
        tryBody.addStatement(parseStmt("_f.complete(null);"));

        var catchBody = new BlockStmt();
        if (!orHandler.isEmpty()) {
            // Parse handler statements
            if (spawn.orHandler().body() != null) {
                for (var stmt : spawn.orHandler().body().stmts()) {
                    for (var jStmt : transformStmt(stmt)) catchBody.addStatement(jStmt);
                }
            }
        }
        catchBody.addStatement(parseStmt("_f.completeExceptionally(err);"));

        var catchClause = new CatchClause(
            new Parameter(new ClassOrInterfaceType(null, "Exception"), "err"), catchBody);
        var tryCatch = new TryStmt(tryBody, new NodeList<>(catchClause), null);

        var threadBody = new BlockStmt();
        threadBody.addStatement(tryCatch);
        var threadLambda = new com.github.javaparser.ast.expr.LambdaExpr(new NodeList<>(), threadBody);

        var outerBody = new BlockStmt();
        outerBody.addStatement(parseStmt("var _f = new java.util.concurrent.CompletableFuture<Void>();"));
        outerBody.addStatement(new ExpressionStmt(
            new MethodCallExpr(
                new MethodCallExpr(new NameExpr("Thread"), "ofVirtual"),
                "start", new NodeList<>(threadLambda))));
        outerBody.addStatement(new ReturnStmt(new NameExpr("_f")));

        var supplierLambda = new com.github.javaparser.ast.expr.LambdaExpr(new NodeList<>(), outerBody);
        // Cast to Supplier and call get()
        var cast = new CastExpr(
            new ClassOrInterfaceType(null, "java.util.function.Supplier")
                .setTypeArguments(new NodeList<>(new ClassOrInterfaceType(null, "java.util.concurrent.CompletableFuture<Void>"))),
            new EnclosedExpr(supplierLambda));
        return new MethodCallExpr(new EnclosedExpr(cast), "get");
    }

    /** Convert TypeInfo to JavaParser Type. */
    private Type typeInfoToJavaType(TypeInfo info) {
        return switch (info.name()) {
            case "int" -> PrimitiveType.intType();
            case "long" -> PrimitiveType.longType();
            case "double" -> PrimitiveType.doubleType();
            case "float" -> PrimitiveType.floatType();
            case "boolean" -> PrimitiveType.booleanType();
            case "byte" -> PrimitiveType.byteType();
            case "char" -> PrimitiveType.charType();
            case "short" -> PrimitiveType.shortType();
            case "void" -> new VoidType();
            default -> {
                var type = new ClassOrInterfaceType(null, info.name());
                if (!info.args().isEmpty()) {
                    var args = new NodeList<Type>();
                    for (var arg : info.args()) args.add(typeInfoToJavaType(arg));
                    type.setTypeArguments(args);
                }
                yield type;
            }
        };
    }

    /** Parse a Java expression from a string via JavaParser. */
    private Expression parseExpr(String code) {
        return com.github.javaparser.StaticJavaParser.parseExpression(code);
    }

    /** Parse a Java statement from a string via JavaParser. */
    private Statement parseStmt(String code) {
        return com.github.javaparser.StaticJavaParser.parseStatement(code);
    }

    private String capitalize(String s) {
        if (s.isEmpty()) return s;
        return Character.toUpperCase(s.charAt(0)) + s.substring(1);
    }
}
