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

import zinc.compiler.Ast.Program;
import zinc.compiler.Ast.Expr;
import zinc.compiler.Ast.IntLit;
import zinc.compiler.Ast.FloatLit;
import zinc.compiler.Ast.BoolLit;
import zinc.compiler.Ast.Ident;
import zinc.compiler.Ast.DataClassDecl;

/**
 * Shared state and utilities for all Transformer components.
 */
public class TransformContext {

    String className = "Main";
    java.util.Map<String, TypeInfo> resolvedTypes = java.util.Map.of();
    final java.util.Set<String> interfaceNames = new java.util.HashSet<>();
    final JavaTypeResolver javaResolver = new JavaTypeResolver();
    final java.util.Set<String> capturedMutables = new java.util.HashSet<>();
    final java.util.Map<String, java.util.List<DataClassDecl>> sealedVariantMap = new java.util.HashMap<>();

    // Zinc method name → Java method name
    static final java.util.Map<String, String> METHOD_ALIASES = java.util.Map.ofEntries(
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
        java.util.Map.entry("indexOf", "indexOf"),
        // Channel methods
        java.util.Map.entry("send", "put"),
        java.util.Map.entry("receive", "take"),
        // Future/spawn result methods
        java.util.Map.entry("isFailed", "isCompletedExceptionally")
    );

    // Zinc type → Java type for constructors
    static final java.util.Map<String, String> TYPE_MAP = java.util.Map.ofEntries(
        java.util.Map.entry("Channel", "java.util.concurrent.ArrayBlockingQueue"),
        java.util.Map.entry("Lock", "java.util.concurrent.locks.ReentrantLock")
    );

    public TransformContext() {}

    public TransformContext(String className) {
        this.className = className;
    }

    public TransformContext(String className, java.util.Map<String, TypeInfo> resolvedTypes) {
        this.className = className;
        this.resolvedTypes = resolvedTypes;
    }

    // --- Type transforms ------------------------------------------------------

    Type transformType(Ast.TypeExpr type) {
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

    /** Transform type with boxing for generics context. */
    Type transformTypeBoxed(Ast.TypeExpr type) {
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
    Type typeInfoToJavaType(TypeInfo info) {
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

    // --- CompilationUnit factory ----------------------------------------------

    CompilationUnit newCU(Program program) {
        var cu = new CompilationUnit();
        if (program.pkg() != null) cu.setPackageDeclaration(program.pkg().path());
        cu.addImport("java.util", false, true);
        cu.addImport("java.util.stream", false, true);
        for (var imp : program.imports()) cu.addImport(imp.path());
        return cu;
    }

    // --- Helpers --------------------------------------------------------------

    /** Check if expression is definitely a primitive (literal or known primitive var). */
    boolean isPrimitiveLiteral(Expr expr) {
        return expr instanceof IntLit || expr instanceof FloatLit || expr instanceof BoolLit
            || (expr instanceof Ast.UnaryExpr un && isPrimitiveLiteral(un.operand()));
    }

    /** Get Zinc type name from an expression (best-effort). */
    String getTypeName(Expr expr) {
        return switch (expr) {
            case Ident id -> id.name();
            case Ast.CallExpr call -> call.callee() instanceof Ident id ? id.name() : "Object";
            default -> "Object";
        };
    }

    String capitalize(String s) {
        if (s.isEmpty()) return s;
        return Character.toUpperCase(s.charAt(0)) + s.substring(1);
    }

    /** Parse a Java expression from a string via JavaParser. */
    Expression parseExpr(String code) {
        var config = new com.github.javaparser.ParserConfiguration()
            .setLanguageLevel(com.github.javaparser.ParserConfiguration.LanguageLevel.JAVA_25);
        config.getProcessors().clear();
        var parser = new com.github.javaparser.JavaParser(config);
        var result = parser.parseExpression(code);
        if (result.isSuccessful()) return result.getResult().get();
        return com.github.javaparser.StaticJavaParser.parseExpression(code);
    }

    /** Parse a Java statement from a string via JavaParser. */
    Statement parseStmt(String code) {
        var config = new com.github.javaparser.ParserConfiguration()
            .setLanguageLevel(com.github.javaparser.ParserConfiguration.LanguageLevel.JAVA_25);
        config.getProcessors().clear();
        var parser = new com.github.javaparser.JavaParser(config);
        var result = parser.parseStatement(code);
        if (result.isSuccessful()) return result.getResult().get();
        return com.github.javaparser.StaticJavaParser.parseStatement(code);
    }
}
