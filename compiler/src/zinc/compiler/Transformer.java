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
import zinc.compiler.Ast.MapLit;
import zinc.compiler.Ast.SafeNavExpr;
import zinc.compiler.Ast.TypeAssertExpr;
import zinc.compiler.Ast.RawStringLit;
import zinc.compiler.Ast.TupleLit;
import zinc.compiler.Ast.SliceExpr;
import zinc.compiler.Ast.SpreadExpr;
import zinc.compiler.Ast.SuperCallExpr;
import zinc.compiler.Ast.MatchExpr;
import zinc.compiler.Ast.MatchExprCase;

/**
 * Transforms Zinc AST into JavaParser AST.
 * Each Zinc node maps to one or more Java AST nodes.
 */
public class Transformer {

    private String className = "Main";
    private java.util.Map<String, TypeInfo> resolvedTypes = java.util.Map.of();
    private java.util.Set<String> interfaceNames = new java.util.HashSet<>();
    private final JavaTypeResolver javaResolver = new JavaTypeResolver();

    public Transformer() {}

    public Transformer(String className) {
        this.className = className;
    }

    public Transformer(String className, java.util.Map<String, TypeInfo> resolvedTypes) {
        this.className = className;
        this.resolvedTypes = resolvedTypes;
    }

    /** Create a new CompilationUnit with standard imports. */
    private CompilationUnit newCU(Program program) {
        var cu = new CompilationUnit();
        if (program.pkg() != null) cu.setPackageDeclaration(program.pkg().path());
        cu.addImport("java.util", false, true);
        cu.addImport("java.util.stream", false, true);
        for (var imp : program.imports()) cu.addImport(imp.path());
        return cu;
    }

    /** Register an interface name from another file (for extends/implements detection). */
    public void registerInterface(String name) {
        interfaceNames.add(name);
    }

    // --- Entry point ---------------------------------------------------------

    /**
     * Transforms a Zinc program into multiple CompilationUnits — one per top-level type.
     * Script mode programs get a single Main class.
     */
    public Result<List<CompilationUnit>> transformAll(Program program) {
        // Pre-scan: collect interface names
        for (var decl : program.decls()) {
            if (decl instanceof InterfaceDecl iface) interfaceNames.add(iface.name());
        }

        var units = new java.util.ArrayList<CompilationUnit>();

        // Script mode — Main class for stmts + top-level fns, separate CUs for types
        if (!program.stmts().isEmpty()) {
            var result = transform(program);
            if (result.isErr()) return Result.err(((Result.Err<?>) result).errors());
            units.add(result.unwrap());

            // Types declared in script mode get their own files
            for (var decl : program.decls()) {
                if (decl instanceof FnDecl) continue;
                emitDeclToUnits(decl, program, units);
            }
            return Result.ok(units);
        }

        // Collect top-level functions — group into one class
        var topFns = program.decls().stream()
            .filter(d -> d instanceof FnDecl).map(d -> (FnDecl) d).toList();

        if (!topFns.isEmpty()) {
            var cu = newCU(program);
            var mainClass = cu.addClass(className, Keyword.PUBLIC);
            for (var fn : topFns) {
                for (var jMethod : transformFnDeclWithOverloads(fn)) {
                    if (fn.name().equals("main") && jMethod.getParameters().isEmpty()) {
                        jMethod.addParameter("String[]", "args");
                        jMethod.setThrownExceptions(new NodeList<>(new ClassOrInterfaceType(null, "Exception")));
                    }
                    mainClass.addMember(jMethod);
                }
            }
            units.add(cu);
        }

        // Other declarations — one CU per type
        for (var decl : program.decls()) {
            if (decl instanceof FnDecl) continue;
            emitDeclToUnits(decl, program, units);
        }

        return Result.ok(units);
    }

    /** Emit a single declaration to its own CompilationUnit(s). */
    private void emitDeclToUnits(Ast.TopLevelDecl decl, Program program, java.util.ArrayList<CompilationUnit> units) {
        var cu = newCU(program);
        switch (decl) {
            case ClassDecl cls -> cu.addType(transformClassDecl(cls));
            case InterfaceDecl iface -> cu.addType(transformInterfaceDecl(iface));
            case DataClassDecl data -> cu.addType(transformDataClassDecl(data));
            case SealedClassDecl sealed -> {
                cu.addType(transformSealedClassDecl(sealed));
                for (var variant : sealed.variants()) {
                    var varCu = newCU(program);
                    var varClass = transformDataClassDecl(variant);
                    // Record implements sealed interface
                    if (varClass instanceof com.github.javaparser.ast.body.RecordDeclaration rec) {
                        rec.addImplementedType(sealed.name());
                    }
                    varCu.addType(varClass);
                    units.add(varCu);
                }
            }
            case EnumDecl en -> cu.addType(transformEnumDecl(en));
            default -> { return; }
        }
        units.add(cu);
    }

    public Result<CompilationUnit> transform(Program program) {
        var cu = new CompilationUnit();

        // Package
        if (program.pkg() != null) {
            cu.setPackageDeclaration(program.pkg().path());
        }

        // Standard imports + user imports
        cu.addImport("java.util", false, true); // java.util.*
        cu.addImport("java.util.stream", false, true); // java.util.stream.*
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
                    for (var m : transformFnDeclWithOverloads(fn)) mainClass.addMember(m);
                }
            }
        }

        return Result.ok(cu);
    }

    // --- Declarations --------------------------------------------------------

    private List<MethodDeclaration> transformFnDeclWithOverloads(FnDecl fn) {
        var methods = new java.util.ArrayList<MethodDeclaration>();
        methods.add(transformFnDecl(fn));

        // Generate overloads for default parameters
        var defaults = fn.params().stream().filter(p -> p.defaultValue() != null).toList();
        if (!defaults.isEmpty()) {
            // For each default param, generate an overload without it
            int firstDefault = -1;
            for (int i = 0; i < fn.params().size(); i++) {
                if (fn.params().get(i).defaultValue() != null) { firstDefault = i; break; }
            }
            for (int cut = firstDefault; cut < fn.params().size(); cut++) {
                var overload = new MethodDeclaration();
                overload.setName(fn.name());
                overload.addModifier(Keyword.PUBLIC, Keyword.STATIC);
                overload.setType(fn.returnType() != null ? transformType(fn.returnType()) : new VoidType());

                var callArgs = new NodeList<Expression>();
                for (int i = 0; i < fn.params().size(); i++) {
                    var p = fn.params().get(i);
                    var pType = p.type() != null ? transformType(p.type()) : new ClassOrInterfaceType(null, "Object");
                    if (i < cut) {
                        overload.addParameter(pType, p.name());
                        callArgs.add(new NameExpr(p.name()));
                    } else {
                        callArgs.add(p.defaultValue() != null ? transformExpr(p.defaultValue()) : new NullLiteralExpr());
                    }
                }
                var body = new BlockStmt();
                var delegateCall = new MethodCallExpr(null, fn.name(), callArgs);
                if (fn.returnType() != null) {
                    body.addStatement(new ReturnStmt(delegateCall));
                } else {
                    body.addStatement(new ExpressionStmt(delegateCall));
                }
                overload.setBody(body);
                methods.add(overload);
            }
        }
        return methods;
    }

    private MethodDeclaration transformFnDecl(FnDecl fn) {
        var method = new MethodDeclaration();
        method.setName(fn.name());
        method.addModifier(Keyword.PUBLIC, Keyword.STATIC);
        method.setType(fn.returnType() != null ? transformType(fn.returnType()) : new VoidType());

        for (var param : fn.params()) {
            var type = param.type() != null ? transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
            var jParam = new Parameter(type, param.name());
            if (param.isVariadic()) jParam.setVarArgs(true);
            method.addParameter(jParam);
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
            if (interfaceNames.contains(parent)) {
                jClass.addImplementedType(parent);
            } else {
                jClass.addExtendedType(parent);
            }
        }

        // Fields
        for (var field : cls.fields()) {
            var type = field.type() != null ? transformType(field.type()) : new ClassOrInterfaceType(null, "Object");
            Keyword visibility = field.isPub() ? Keyword.PUBLIC : Keyword.PRIVATE;
            if (field.isConst()) {
                // const → public static final
                var jField = jClass.addField(type, field.name(), Keyword.PUBLIC, Keyword.STATIC, Keyword.FINAL);
                if (field.defaultValue() != null) jField.getVariable(0).setInitializer(transformExpr(field.defaultValue()));
                continue;
            }
            var jField = jClass.addField(type, field.name(), visibility);
            if (field.isInit()) jField.addModifier(Keyword.FINAL);
            if (field.defaultValue() != null) {
                jField.getVariable(0).setInitializer(
                    transformExprInContext(field.defaultValue(), field.type()));
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

            // Generate overloads for default parameters
            int firstDefault = -1;
            for (int i = 0; i < ctor.params().size(); i++) {
                if (ctor.params().get(i).defaultValue() != null) { firstDefault = i; break; }
            }
            if (firstDefault >= 0) {
                for (int cut = firstDefault; cut < ctor.params().size(); cut++) {
                    var overload = jClass.addConstructor(Keyword.PUBLIC);
                    var callArgs = new NodeList<Expression>();
                    for (int i = 0; i < ctor.params().size(); i++) {
                        var p = ctor.params().get(i);
                        var pType = p.type() != null ? transformType(p.type()) : new ClassOrInterfaceType(null, "Object");
                        if (i < cut) {
                            overload.addParameter(pType, p.name());
                            callArgs.add(new NameExpr(p.name()));
                        } else {
                            callArgs.add(p.defaultValue() != null ? transformExpr(p.defaultValue()) : new NullLiteralExpr());
                        }
                    }
                    var overloadBody = new BlockStmt();
                    overloadBody.addStatement(new ExpressionStmt(
                        new MethodCallExpr(null, "this", callArgs)));
                    overload.setBody(overloadBody);
                }
            }
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

    /**
     * data class → Java record (Java 25).
     * data Point(int x, int y) → public record Point(int x, int y) { }
     * Records get: constructor, accessors, equals, hashCode, toString for free.
     */
    private com.github.javaparser.ast.body.TypeDeclaration<?> transformDataClassDecl(DataClassDecl data) {
        var record = new com.github.javaparser.ast.body.RecordDeclaration(
            new NodeList<>(com.github.javaparser.ast.Modifier.publicModifier()),
            data.name());

        // Record parameters
        for (var param : data.params()) {
            var type = param.type() != null ? transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
            record.addParameter(type, param.name());
        }

        // Implemented interfaces
        for (var parent : data.parents()) {
            if (interfaceNames.contains(parent)) {
                record.addImplementedType(parent);
            }
        }

        // Additional methods
        for (var method : data.methods()) {
            record.addMember(transformMethodDecl(method));
        }

        return record;
    }

    /**
     * sealed class → Java sealed interface (Java 25).
     * Records can implement interfaces but can't extend classes.
     * sealed class Shape { data Circle(...), data Rect(...) }
     * → public sealed interface Shape permits Circle, Rect {}
     */
    private ClassOrInterfaceDeclaration transformSealedClassDecl(SealedClassDecl sealed) {
        var jIface = new ClassOrInterfaceDeclaration();
        jIface.setInterface(true);
        jIface.setName(sealed.name());
        jIface.addModifier(Keyword.PUBLIC, Keyword.SEALED);

        // Add permits clause
        var permits = new NodeList<ClassOrInterfaceType>();
        for (var variant : sealed.variants()) {
            permits.add(new ClassOrInterfaceType(null, variant.name()));
        }
        jIface.setPermittedTypes(permits);

        // Methods from sealed class body
        for (var method : sealed.methods()) {
            jIface.addMember(transformMethodDecl(method));
        }

        sealedVariantMap.put(sealed.name(), sealed.variants());
        return jIface;
    }

    private final java.util.Map<String, List<DataClassDecl>> sealedVariantMap = new java.util.HashMap<>();

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
        // Override methods from Object (toString, equals, hashCode) must be public
        boolean isOverride = method.name().equals("toString") || method.name().equals("equals")
            || method.name().equals("hashCode");
        if (method.isPub() || isOverride) jMethod.addModifier(Keyword.PUBLIC);
        else jMethod.addModifier(Keyword.PRIVATE);
        if (method.isStatic()) jMethod.addModifier(Keyword.STATIC);
        if (method.isAbstract()) jMethod.addModifier(Keyword.ABSTRACT);
        if (isOverride) jMethod.addMarkerAnnotation("Override");
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
                String baseName = TYPE_MAP.getOrDefault(g.name(), g.name());
                var base = new ClassOrInterfaceType(null, baseName);
                var args = new NodeList<Type>();
                for (var arg : g.typeArgs()) args.add(transformTypeBoxed(arg));
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
            case Ast.MatchStmt m -> List.of(transformMatchStmt(m));
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
        if (v.type() != null) {
            var type = transformType(v.type());
            var decl = new VariableDeclarationExpr(type, v.name());
            if (v.value() != null) decl.getVariable(0).setInitializer(
                transformExprInContext(v.value(), v.type()));
            return new ExpressionStmt(decl);
        }
        // var inference
        var decl = new VariableDeclarationExpr(new VarType(), v.name());
        if (v.value() != null) decl.getVariable(0).setInitializer(transformExpr(v.value()));
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

    /**
     * match stmt → Java switch with pattern matching (Java 21+) or if/else chain.
     * Record patterns: case Single(f) → case Single(var f)
     * Value patterns: case "ok" → case "ok"
     * Wildcard: case _ → default
     */
    private Statement transformMatchStmt(Ast.MatchStmt m) {
        // Detect if any pattern is a record deconstruction (CallExpr pattern)
        boolean hasRecordPatterns = m.cases().stream()
            .anyMatch(c -> c.pattern() instanceof CallExpr);

        if (hasRecordPatterns) {
            return transformMatchAsSwitch(m);
        }

        // Simple value matching → if/else chain with Objects.equals
        var subject = transformExpr(m.subject());
        IfStmt firstIf = null;
        IfStmt lastIf = null;
        Statement defaultCase = null;

        for (var c : m.cases()) {
            if (c.pattern() == null) {
                defaultCase = transformBlock(c.body());
            } else {
                var cond = new MethodCallExpr(
                    new NameExpr("java.util.Objects"), "equals",
                    new NodeList<>(subject.clone(), transformExpr(c.pattern())));
                var ifStmt = new IfStmt(cond, transformBlock(c.body()), null);
                if (firstIf == null) firstIf = ifStmt;
                else lastIf.setElseStmt(ifStmt);
                lastIf = ifStmt;
            }
        }

        if (firstIf == null) return defaultCase != null ? defaultCase : new BlockStmt();
        if (defaultCase != null && lastIf != null) lastIf.setElseStmt(defaultCase);
        return firstIf;
    }

    /**
     * Generate Java switch with record patterns (Java 21+):
     * switch (subject) {
     *     case Single(var f) -> { body }
     *     case Multiple(var ffs) -> { body }
     *     case Drop _ -> { body }
     *     default -> { body }
     * }
     */
    private Statement transformMatchAsSwitch(Ast.MatchStmt m) {
        var subject = transformExpr(m.subject());
        // Build switch expression as string since JavaParser may not support record patterns natively
        var sb = new StringBuilder();
        sb.append("switch (").append(subject).append(") {\n");
        for (var c : m.cases()) {
            if (c.pattern() == null) {
                // wildcard
                sb.append("    default -> ");
                sb.append(transformBlock(c.body()));
                sb.append("\n");
            } else if (c.pattern() instanceof CallExpr call && call.callee() instanceof Ident typeName) {
                // Record pattern: case Type(var a, var b) -> { body }
                sb.append("    case ").append(typeName.name()).append("(");
                var args = call.args();
                if (args.isEmpty()) {
                    sb.append("var _"); // Empty record: Drop() → Drop _
                    sb.setLength(sb.length() - "var _".length());
                    // Drop with no args → Drop _
                    sb.setLength(sb.length() - 1); // remove (
                    sb.append(" _");
                } else {
                    for (int i = 0; i < args.size(); i++) {
                        if (i > 0) sb.append(", ");
                        if (args.get(i) instanceof Ident id) {
                            sb.append("var ").append(id.name());
                        } else {
                            sb.append("var _p").append(i);
                        }
                    }
                    sb.append(")");
                }
                sb.append(" -> ");
                sb.append(transformBlock(c.body()));
                sb.append("\n");
            } else {
                // Value pattern
                sb.append("    case ").append(transformExpr(c.pattern())).append(" -> ");
                sb.append(transformBlock(c.body()));
                sb.append("\n");
            }
        }
        sb.append("}");
        return parseStmt(sb.toString());
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
            // for key, value in map → for (var _entry : map.entrySet()) { var key = _entry.getKey(); ... }
            if (!f.indexVar().isEmpty()) {
                var forEach = new ForEachStmt();
                forEach.setVariable(new VariableDeclarationExpr(new VarType(), "_entry"));
                forEach.setIterable(new MethodCallExpr(transformExpr(f.range()), "entrySet"));
                var body = transformBlock(f.body());
                // Prepend key/value declarations
                var keyDecl = new ExpressionStmt(new VariableDeclarationExpr(new VarType(), f.indexVar()));
                ((VariableDeclarationExpr) keyDecl.getExpression()).getVariable(0)
                    .setInitializer(new MethodCallExpr(new NameExpr("_entry"), "getKey"));
                var valDecl = new ExpressionStmt(new VariableDeclarationExpr(new VarType(), f.item()));
                ((VariableDeclarationExpr) valDecl.getExpression()).getVariable(0)
                    .setInitializer(new MethodCallExpr(new NameExpr("_entry"), "getValue"));
                body.getStatements().addFirst(valDecl);
                body.getStatements().addFirst(keyDecl);
                forEach.setBody(body);
                return forEach;
            }

            // for item in range → for (var item : range)
            var forEach = new ForEachStmt();
            forEach.setVariable(new VariableDeclarationExpr(new VarType(), f.item()));
            var iterable = transformExpr(f.range());
            // IntStream → boxed for for-each
            if (f.range() instanceof RangeExpr) {
                iterable = new MethodCallExpr(
                    new MethodCallExpr(iterable, "boxed"), "toList");
            }
            forEach.setIterable(iterable);
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
                var listArgs = new NodeList<Expression>();
                for (var el : list.elements()) listArgs.add(transformExpr(el));
                // new ArrayList<>(List.of(...)) — mutable, Java infers type from elements
                var listOf = new MethodCallExpr(new NameExpr("List"), "of", listArgs);
                yield new ObjectCreationExpr(null,
                    new ClassOrInterfaceType(null, "ArrayList<>"), new NodeList<>(listOf));
            }
            case StringInterpLit interp -> transformInterpString(interp);
            case MapLit map -> {
                // Preserve insertion order via LinkedHashMap
                // Use java.util.SequencedMap factory or build inline
                if (map.keys().isEmpty()) {
                    yield new ObjectCreationExpr(null, new ClassOrInterfaceType(null, "LinkedHashMap<>"), new NodeList<>());
                }
                // For small maps, use inline double-brace initialization
                // This creates an anonymous subclass but is the simplest way to preserve order
                // in an expression context without a helper method
                var sb = new StringBuilder("new java.util.LinkedHashMap<>()");
                sb.append(" {{ ");
                for (int i = 0; i < map.keys().size(); i++) {
                    sb.append("put(").append(exprToJava(map.keys().get(i)))
                        .append(", ").append(exprToJava(map.values().get(i))).append("); ");
                }
                sb.append("}}");
                yield parseExpr(sb.toString());
            }
            case RawStringLit raw -> new StringLiteralExpr(raw.value());
            case SafeNavExpr nav -> {
                // obj?.field → (obj != null ? obj.field : null)
                var obj = transformExpr(nav.object());
                var access = new FieldAccessExpr(obj, nav.field());
                yield new ConditionalExpr(
                    new com.github.javaparser.ast.expr.BinaryExpr(obj.clone(), new NullLiteralExpr(),
                        com.github.javaparser.ast.expr.BinaryExpr.Operator.NOT_EQUALS),
                    nav.call() != null ? transformCallExpr(nav.call()) : access,
                    new NullLiteralExpr());
            }
            case TypeAssertExpr ta -> {
                if (ta.isCheck()) {
                    // x is Type → x instanceof Type
                    yield new InstanceOfExpr(transformExpr(ta.object()), new ClassOrInterfaceType(null, ta.typeName()));
                } else {
                    // x as Type → (Type) x
                    yield new CastExpr(new ClassOrInterfaceType(null, ta.typeName()), transformExpr(ta.object()));
                }
            }
            case Ast.IfExpr ifE -> new ConditionalExpr(
                transformExpr(ifE.cond()), transformExpr(ifE.then()), transformExpr(ifE.elseExpr()));
            case MatchExpr matchE -> transformMatchExpr(matchE);
            case Ast.LambdaExpr lam -> transformLambda(lam);
            case Ast.SpawnExpr spawn -> transformSpawn(spawn);
            case RangeExpr range -> transformRange(range);
            case TupleLit tuple -> {
                // Tuples → just use the first element for now (simplified)
                if (!tuple.elements().isEmpty()) yield transformExpr(tuple.elements().getFirst());
                yield new NullLiteralExpr();
            }
            default -> new NameExpr("/* unsupported: " + expr.getClass().getSimpleName() + " */");
        };
    }

    private Expression transformBinaryExpr(Ast.BinaryExpr bin) {
        var left = transformExpr(bin.left());
        var right = transformExpr(bin.right());

        // == in Zinc is structural equality
        // For primitives (int, double, etc.): use Java ==
        // For objects (String, etc.): use Objects.equals()
        if (bin.op().equals("==")) {
            if (isPrimitiveLiteral(bin.left()) && isPrimitiveLiteral(bin.right())) {
                return new com.github.javaparser.ast.expr.BinaryExpr(left, right, BinaryExpr.Operator.EQUALS);
            }
            return new MethodCallExpr(new NameExpr("java.util.Objects"), "equals",
                new NodeList<>(left, right));
        }
        if (bin.op().equals("!=")) {
            if (isPrimitiveLiteral(bin.left()) && isPrimitiveLiteral(bin.right())) {
                return new com.github.javaparser.ast.expr.BinaryExpr(left, right, BinaryExpr.Operator.NOT_EQUALS);
            }
            return new com.github.javaparser.ast.expr.UnaryExpr(
                new MethodCallExpr(new NameExpr("java.util.Objects"), "equals",
                    new NodeList<>(left, right)),
                com.github.javaparser.ast.expr.UnaryExpr.Operator.LOGICAL_COMPLEMENT);
        }
        // === is reference equality → a == b in Java
        if (bin.op().equals("===")) {
            return new com.github.javaparser.ast.expr.BinaryExpr(left, right, BinaryExpr.Operator.EQUALS);
        }
        // !== → a != b in Java
        if (bin.op().equals("!==")) {
            return new com.github.javaparser.ast.expr.BinaryExpr(left, right, BinaryExpr.Operator.NOT_EQUALS);
        }
        // ** → Math.pow(), cast to (long) for int operands
        if (bin.op().equals("**")) {
            var pow = new MethodCallExpr(new NameExpr("Math"), "pow", new NodeList<>(left, right));
            if (isPrimitiveLiteral(bin.left()) && isPrimitiveLiteral(bin.right())
                && bin.left() instanceof IntLit && bin.right() instanceof IntLit) {
                return new CastExpr(PrimitiveType.longType(), pow);
            }
            return pow;
        }

        var op = switch (bin.op()) {
            case "+" -> BinaryExpr.Operator.PLUS;
            case "-" -> BinaryExpr.Operator.MINUS;
            case "*" -> BinaryExpr.Operator.MULTIPLY;
            case "/" -> BinaryExpr.Operator.DIVIDE;
            case "%" -> BinaryExpr.Operator.REMAINDER;
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

    // Zinc method name → Java method name
    private static final java.util.Map<String, String> METHOD_ALIASES = java.util.Map.ofEntries(
        java.util.Map.entry("upper", "toUpperCase"),
        java.util.Map.entry("lower", "toLowerCase"),
        java.util.Map.entry("trim", "strip"),
        java.util.Map.entry("trimStart", "stripLeading"),
        java.util.Map.entry("trimEnd", "stripTrailing"),
        java.util.Map.entry("chars", "toCharArray"),
        java.util.Map.entry("startsWith", "startsWith"),
        java.util.Map.entry("endsWith", "endsWith"),
        java.util.Map.entry("contains", "contains"),
        java.util.Map.entry("replace", "replace"),
        java.util.Map.entry("repeat", "repeat"),
        java.util.Map.entry("isEmpty", "isEmpty"),
        java.util.Map.entry("split", "split"),
        java.util.Map.entry("substring", "substring"),
        java.util.Map.entry("charAt", "charAt"),
        java.util.Map.entry("indexOf", "indexOf")
    );

    // Zinc type → Java type for constructors
    private static final java.util.Map<String, String> TYPE_MAP = java.util.Map.ofEntries(
        java.util.Map.entry("Channel", "java.util.concurrent.ArrayBlockingQueue"),
        java.util.Map.entry("Lock", "java.util.concurrent.locks.ReentrantLock")
    );

    private Expression transformCallExpr(CallExpr call) {
        var args = new NodeList<Expression>();
        for (var arg : call.args()) args.add(transformExpr(arg));

        if (call.isNew()) {
            String typeName = ((Ident) call.callee()).name();
            typeName = TYPE_MAP.getOrDefault(typeName, typeName);
            var type = new ClassOrInterfaceType(null, typeName);
            if (!call.typeArgs().isEmpty()) {
                var typeArgs = new NodeList<Type>();
                for (var ta : call.typeArgs()) typeArgs.add(new ClassOrInterfaceType(null, ta));
                type.setTypeArguments(typeArgs);
            }
            return new ObjectCreationExpr(null, type, args);
        }

        // Method call on object: obj.method(args) with alias resolution
        if (call.callee() instanceof SelectorExpr sel) {
            String methodName = METHOD_ALIASES.getOrDefault(sel.field(), sel.field());

            // Stream methods on collections: .filter(), .map(), .reduce(), etc.
            if (isStreamMethod(methodName)) {
                return transformStreamChain(call);
            }

            var result = new MethodCallExpr(transformExpr(sel.object()), methodName, args);
            // Auto-unwrap Optional returns
            if (javaResolver.returnsOptional(getTypeName(sel.object()), methodName)) {
                return new MethodCallExpr(result, "orElse", new NodeList<>(new NullLiteralExpr()));
            }
            return result;
        }

        // Simple function call: func(args)
        if (call.callee() instanceof Ident id) {
            if (id.name().equals("print")) {
                return new MethodCallExpr(new NameExpr("System.out"), "println", args);
            }
            if (id.name().equals("len") && !args.isEmpty()) {
                return new MethodCallExpr(args.get(0), "size");
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
                // Wrap complex expressions in parens to avoid precedence issues with + concatenation
                if (expr instanceof com.github.javaparser.ast.expr.BinaryExpr
                    || expr instanceof ConditionalExpr
                    || expr instanceof com.github.javaparser.ast.expr.UnaryExpr) {
                    expr = new EnclosedExpr(expr);
                }
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
        // Single-expression lambda: { return expr; } → use expression form
        // This lets Java infer void vs value context automatically
        if (body.getStatements().size() == 1
            && body.getStatement(0) instanceof ReturnStmt ret
            && ret.getExpression().isPresent()) {
            return new com.github.javaparser.ast.expr.LambdaExpr(params, ret.getExpression().get());
        }
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

    /** Match expression → nested ternary. */
    private Expression transformMatchExpr(MatchExpr match) {
        var subject = transformExpr(match.subject());
        // Build nested ternary: subject.equals(case1) ? val1 : subject.equals(case2) ? val2 : default
        Expression result = new NullLiteralExpr(); // default
        for (int i = match.cases().size() - 1; i >= 0; i--) {
            var c = match.cases().get(i);
            if (c.pattern() == null) {
                // Wildcard _ → default
                result = transformExpr(c.value());
            } else {
                var cond = new MethodCallExpr(
                    new NameExpr("java.util.Objects"), "equals",
                    new NodeList<>(subject.clone(), transformExpr(c.pattern())));
                result = new ConditionalExpr(cond, transformExpr(c.value()), result);
            }
        }
        return result;
    }

    /** Transform type with boxing for generics context. */
    private Type transformTypeBoxed(Ast.TypeExpr type) {
        return switch (type) {
            case Ast.SimpleType s -> switch (s.name()) {
                case "int" -> new ClassOrInterfaceType(null, "Integer");
                case "long" -> new ClassOrInterfaceType(null, "Long");
                case "double" -> new ClassOrInterfaceType(null, "Double");
                case "float" -> new ClassOrInterfaceType(null, "Float");
                case "boolean" -> new ClassOrInterfaceType(null, "Boolean");
                case "byte" -> new ClassOrInterfaceType(null, "Byte");
                case "char" -> new ClassOrInterfaceType(null, "Character");
                case "short" -> new ClassOrInterfaceType(null, "Short");
                default -> new ClassOrInterfaceType(null, s.name());
            };
            default -> transformType(type);
        };
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

    /**
     * Transform expression with type context — handles array literal assignment to array type.
     */
    private Expression transformExprInContext(Expr expr, Ast.TypeExpr targetType) {
        if (targetType instanceof Ast.ArrayType arrType && expr instanceof ListLit list) {
            var elemType = transformType(arrType.elementType());
            var elems = new NodeList<Expression>();
            for (var el : list.elements()) elems.add(transformExpr(el));
            return new ArrayCreationExpr(elemType, new NodeList<>(new ArrayCreationLevel()),
                new ArrayInitializerExpr(elems));
        }
        return transformExpr(expr);
    }

    /** Check if expression is definitely a primitive (literal or known primitive var). */
    private boolean isPrimitiveLiteral(Expr expr) {
        return expr instanceof IntLit || expr instanceof FloatLit || expr instanceof BoolLit
            || (expr instanceof Ast.UnaryExpr un && isPrimitiveLiteral(un.operand()));
    }

    /** Get Zinc type name from an expression (best-effort). */
    private String getTypeName(Expr expr) {
        return switch (expr) {
            case Ident id -> id.name();
            case CallExpr call -> call.callee() instanceof Ident id ? id.name() : "Object";
            default -> "Object";
        };
    }

    /** Quick expression to Java source string for inline use. */
    private String exprToJava(Expr expr) {
        return transformExpr(expr).toString();
    }

    /**
     * Transform a stream chain: collect all chained stream ops, emit as single stream pipeline.
     * numbers.filter(it > 5).map(it * 10).sum()
     * → numbers.stream().filter(_it -> _it > 5).mapToInt(_it -> _it * 10).sum()
     */
    private Expression transformStreamChain(CallExpr call) {
        // Collect chain: walk down the selector/call chain until we hit a non-stream receiver
        var ops = new java.util.ArrayList<CallExpr>();
        Expr root = call;
        while (root instanceof CallExpr c && c.callee() instanceof SelectorExpr sel
               && isStreamMethod(METHOD_ALIASES.getOrDefault(sel.field(), sel.field()))) {
            ops.addFirst(c);
            root = sel.object();
        }

        // root is the collection, ops are the stream operations in order
        Expression stream = new MethodCallExpr(transformExpr(root), "stream");

        for (var op : ops) {
            var sel = (SelectorExpr) op.callee();
            String methodName = METHOD_ALIASES.getOrDefault(sel.field(), sel.field());

            var streamArgs = new NodeList<Expression>();
            for (var arg : op.args()) {
                if (containsIt(arg)) {
                    streamArgs.add(new com.github.javaparser.ast.expr.LambdaExpr(
                        new NodeList<>(new Parameter(new UnknownType(), "_it")), rewriteIt(arg)));
                } else {
                    streamArgs.add(transformExpr(arg));
                }
            }

            // Special transforms for certain stream ops
            switch (methodName) {
                case "sum" -> {
                    stream = new MethodCallExpr(stream, "mapToInt", new NodeList<>(parseExpr("x -> (int) x")));
                    stream = new MethodCallExpr(stream, "sum");
                }
                case "sortBy" -> {
                    // sortBy(key) → sorted(Comparator.comparing(key))
                    var comparator = new MethodCallExpr(new NameExpr("Comparator"), "comparing", streamArgs);
                    stream = new MethodCallExpr(stream, "sorted", new NodeList<>(comparator));
                }
                case "groupBy" -> {
                    var collector = new MethodCallExpr(new NameExpr("java.util.stream.Collectors"), "groupingBy", streamArgs);
                    stream = new MethodCallExpr(stream, "collect", new NodeList<>(collector));
                }
                case "findFirst" -> {
                    // findFirst(pred) → filter(pred).findFirst().orElse(null)
                    if (!streamArgs.isEmpty()) {
                        stream = new MethodCallExpr(stream, "filter", streamArgs);
                    }
                    stream = new MethodCallExpr(stream, "findFirst");
                    stream = new MethodCallExpr(stream, "orElse", new NodeList<>(new NullLiteralExpr()));
                }
                default -> stream = new MethodCallExpr(stream, methodName, streamArgs);
            }
        }

        // If the last op is terminal, return as-is. Otherwise wrap in .toList()
        var lastOp = ops.getLast();
        var lastSel = (SelectorExpr) lastOp.callee();
        String lastName = METHOD_ALIASES.getOrDefault(lastSel.field(), lastSel.field());
        boolean isTerminal = switch (lastName) {
            case "reduce", "forEach", "anyMatch", "allMatch", "noneMatch",
                 "count", "findFirst", "sum", "min", "max", "average",
                 "groupBy" -> true;
            default -> false;
        };

        return isTerminal ? stream : new MethodCallExpr(stream, "toList");
    }

    /** Check if an expression contains the `it` implicit parameter. */
    private boolean containsIt(Expr expr) {
        return switch (expr) {
            case Ident id -> id.name().equals("it");
            case Ast.BinaryExpr bin -> containsIt(bin.left()) || containsIt(bin.right());
            case Ast.UnaryExpr un -> containsIt(un.operand());
            case CallExpr call -> call.args().stream().anyMatch(this::containsIt)
                || (call.callee() instanceof SelectorExpr sel && containsIt(sel.object()));
            case SelectorExpr sel -> containsIt(sel.object());
            default -> false;
        };
    }

    /** Rewrite `it` references to `_it` and transform the expression. */
    private Expression rewriteIt(Expr expr) {
        return switch (expr) {
            case Ident id -> id.name().equals("it") ? new NameExpr("_it") : new NameExpr(id.name());
            case Ast.BinaryExpr bin -> new com.github.javaparser.ast.expr.BinaryExpr(
                rewriteIt(bin.left()), rewriteIt(bin.right()),
                switch (bin.op()) {
                    case "+" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.PLUS;
                    case "-" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.MINUS;
                    case "*" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.MULTIPLY;
                    case "/" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.DIVIDE;
                    case "%" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.REMAINDER;
                    case ">" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.GREATER;
                    case "<" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.LESS;
                    case ">=" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.GREATER_EQUALS;
                    case "<=" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.LESS_EQUALS;
                    case "==" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.EQUALS;
                    case "!=" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.NOT_EQUALS;
                    default -> com.github.javaparser.ast.expr.BinaryExpr.Operator.PLUS;
                });
            case CallExpr call -> {
                // it.method() → _it.method()
                if (call.callee() instanceof SelectorExpr sel && containsIt(sel.object())) {
                    var args = new NodeList<Expression>();
                    for (var a : call.args()) args.add(containsIt(a) ? rewriteIt(a) : transformExpr(a));
                    yield new MethodCallExpr(rewriteIt(sel.object()), sel.field(), args);
                }
                yield transformExpr(expr);
            }
            case SelectorExpr sel -> new FieldAccessExpr(rewriteIt(sel.object()), sel.field());
            default -> transformExpr(expr);
        };
    }

    /**
     * Java Stream API methods. When called on a collection, auto-insert .stream()/.toList().
     * This is a fixed set — Java's Stream API doesn't change often.
     */
    private static boolean isStreamMethod(String name) {
        return switch (name) {
            case "filter", "map", "flatMap", "reduce", "forEach", "sorted", "sortBy",
                 "distinct", "limit", "skip", "anyMatch", "allMatch", "noneMatch",
                 "findFirst", "count", "toList", "sum", "min", "max", "average",
                 "groupBy" -> true;
            default -> false;
        };
    }

    /** Parse a Java expression from a string via JavaParser. */
    private Expression parseExpr(String code) {
        return com.github.javaparser.StaticJavaParser.parseExpression(code);
    }

    /** Parse a Java statement from a string via JavaParser (with Java 21+ features). */
    private Statement parseStmt(String code) {
        var config = new com.github.javaparser.ParserConfiguration()
            .setLanguageLevel(com.github.javaparser.ParserConfiguration.LanguageLevel.JAVA_25);
        var parser = new com.github.javaparser.JavaParser(config);
        var result = parser.parseStatement(code);
        if (result.isSuccessful()) return result.getResult().get();
        // Fallback to default parser
        return com.github.javaparser.StaticJavaParser.parseStatement(code);
    }

    private String capitalize(String s) {
        if (s.isEmpty()) return s;
        return Character.toUpperCase(s.charAt(0)) + s.substring(1);
    }
}
