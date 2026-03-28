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

import zinc.compiler.Ast.Program;
import zinc.compiler.Ast.FnDecl;
import zinc.compiler.Ast.ClassDecl;
import zinc.compiler.Ast.InterfaceDecl;
import zinc.compiler.Ast.DataClassDecl;
import zinc.compiler.Ast.SealedClassDecl;
import zinc.compiler.Ast.EnumDecl;

/**
 * Transforms Zinc AST declarations into JavaParser AST declarations.
 */
public class TransformDecl {

    private final TransformContext ctx;
    private final TransformExpr exprs;
    private final TransformStmt stmts;

    public TransformDecl(TransformContext ctx, TransformExpr exprs, TransformStmt stmts) {
        this.ctx = ctx;
        this.exprs = exprs;
        this.stmts = stmts;
    }

    // --- Entry point ----------------------------------------------------------

    /** Emit a single declaration to its own CompilationUnit(s). */
    void emitDeclToUnits(Ast.TopLevelDecl decl, Program program, java.util.ArrayList<CompilationUnit> units) {
        var cu = ctx.newCU(program);
        switch (decl) {
            case ClassDecl cls -> cu.addType(transformClassDecl(cls));
            case InterfaceDecl iface -> cu.addType(transformInterfaceDecl(iface));
            case DataClassDecl data -> cu.addType(transformDataClassDecl(data));
            case SealedClassDecl sealed -> {
                cu.addType(transformSealedClassDecl(sealed));
                for (var variant : sealed.variants()) {
                    var varCu = ctx.newCU(program);
                    var varClass = transformDataClassDecl(variant);
                    if (varClass instanceof RecordDeclaration rec) {
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

    // --- Functions ------------------------------------------------------------

    List<MethodDeclaration> transformFnDeclWithOverloads(FnDecl fn) {
        var methods = new java.util.ArrayList<MethodDeclaration>();
        methods.add(transformFnDecl(fn));

        var defaults = fn.params().stream().filter(p -> p.defaultValue() != null).toList();
        if (!defaults.isEmpty()) {
            int firstDefault = -1;
            for (int i = 0; i < fn.params().size(); i++) {
                if (fn.params().get(i).defaultValue() != null) { firstDefault = i; break; }
            }
            for (int cut = firstDefault; cut < fn.params().size(); cut++) {
                var overload = new MethodDeclaration();
                overload.setName(fn.name());
                overload.addModifier(Keyword.PUBLIC, Keyword.STATIC);
                overload.setType(fn.returnType() != null ? ctx.transformType(fn.returnType()) : new VoidType());

                var callArgs = new NodeList<Expression>();
                for (int i = 0; i < fn.params().size(); i++) {
                    var p = fn.params().get(i);
                    var pType = p.type() != null ? ctx.transformType(p.type()) : new ClassOrInterfaceType(null, "Object");
                    if (i < cut) {
                        overload.addParameter(pType, p.name());
                        callArgs.add(new NameExpr(p.name()));
                    } else {
                        callArgs.add(p.defaultValue() != null ? exprs.transformExpr(p.defaultValue()) : new NullLiteralExpr());
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
        method.setType(fn.returnType() != null ? ctx.transformType(fn.returnType()) : new VoidType());

        for (var param : fn.params()) {
            var type = param.type() != null ? ctx.transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
            var jParam = new Parameter(type, param.name());
            if (param.isVariadic()) jParam.setVarArgs(true);
            method.addParameter(jParam);
        }

        if (fn.body() != null) {
            method.setBody(stmts.transformBlock(fn.body()));
        }

        return method;
    }

    // --- Classes --------------------------------------------------------------

    ClassOrInterfaceDeclaration transformClassDecl(ClassDecl cls) {
        var jClass = new ClassOrInterfaceDeclaration();
        jClass.setName(cls.name());
        jClass.addModifier(Keyword.PUBLIC);
        if (cls.isAbstract()) jClass.addModifier(Keyword.ABSTRACT);

        for (var parent : cls.parents()) {
            if (ctx.interfaceNames.contains(parent)) {
                jClass.addImplementedType(parent);
            } else {
                jClass.addExtendedType(parent);
            }
        }

        // Fields
        for (var field : cls.fields()) {
            var type = field.type() != null ? ctx.transformType(field.type()) : new ClassOrInterfaceType(null, "Object");
            Keyword visibility = field.isPub() ? Keyword.PUBLIC : Keyword.PRIVATE;
            if (field.isConst()) {
                var jField = jClass.addField(type, field.name(), Keyword.PUBLIC, Keyword.STATIC, Keyword.FINAL);
                if (field.defaultValue() != null) jField.getVariable(0).setInitializer(exprs.transformExpr(field.defaultValue()));
                continue;
            }
            var jField = jClass.addField(type, field.name(), visibility);
            if (field.isInit()) jField.addModifier(Keyword.FINAL);
            if (field.defaultValue() != null) {
                jField.getVariable(0).setInitializer(
                    exprs.transformExprInContext(field.defaultValue(), field.type()));
            }

            if (field.isPub() || field.isReadonly() || field.isInit()) {
                var getter = jClass.addMethod("get" + ctx.capitalize(field.name()), Keyword.PUBLIC);
                getter.setType(type);
                getter.setBody(new BlockStmt().addStatement(new ReturnStmt(new NameExpr("this." + field.name()))));
            }
            if (field.isPub() && !field.isReadonly() && !field.isInit()) {
                var setter = jClass.addMethod("set" + ctx.capitalize(field.name()), Keyword.PUBLIC);
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
                var type = param.type() != null ? ctx.transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
                jCtor.addParameter(type, param.name());
            }
            var body = stmts.transformBlock(ctor.body());
            if (!ctor.superArgs().isEmpty()) {
                var superArgs = new NodeList<Expression>();
                for (var arg : ctor.superArgs()) superArgs.add(exprs.transformExpr(arg));
                body.getStatements().addFirst(new ExpressionStmt(
                    new MethodCallExpr(null, "super", superArgs)));
            }
            jCtor.setBody(body);

            // Constructor overloads for default parameters
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
                        var pType = p.type() != null ? ctx.transformType(p.type()) : new ClassOrInterfaceType(null, "Object");
                        if (i < cut) {
                            overload.addParameter(pType, p.name());
                            callArgs.add(new NameExpr(p.name()));
                        } else {
                            callArgs.add(p.defaultValue() != null ? exprs.transformExpr(p.defaultValue()) : new NullLiteralExpr());
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

    // --- Interfaces -----------------------------------------------------------

    ClassOrInterfaceDeclaration transformInterfaceDecl(InterfaceDecl iface) {
        var jIface = new ClassOrInterfaceDeclaration();
        jIface.setInterface(true);
        jIface.setName(iface.name());
        jIface.addModifier(Keyword.PUBLIC);

        for (var sig : iface.methods()) {
            var method = new MethodDeclaration();
            method.setName(sig.name());
            method.setType(sig.returnType() != null ? ctx.transformType(sig.returnType()) : new VoidType());
            method.removeBody();
            for (var param : sig.params()) {
                var type = param.type() != null ? ctx.transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
                method.addParameter(type, param.name());
            }
            jIface.addMember(method);
        }

        return jIface;
    }

    // --- Data classes (records) ------------------------------------------------

    TypeDeclaration<?> transformDataClassDecl(DataClassDecl data) {
        var record = new RecordDeclaration(
            new NodeList<>(com.github.javaparser.ast.Modifier.publicModifier()),
            data.name());

        for (var param : data.params()) {
            var type = param.type() != null ? ctx.transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
            record.addParameter(type, param.name());
        }

        for (var parent : data.parents()) {
            if (ctx.interfaceNames.contains(parent)) {
                record.addImplementedType(parent);
            }
        }

        for (var method : data.methods()) {
            record.addMember(transformMethodDecl(method));
        }

        return record;
    }

    // --- Sealed classes -------------------------------------------------------

    ClassOrInterfaceDeclaration transformSealedClassDecl(SealedClassDecl sealed) {
        var jIface = new ClassOrInterfaceDeclaration();
        jIface.setInterface(true);
        jIface.setName(sealed.name());
        jIface.addModifier(Keyword.PUBLIC, Keyword.SEALED);

        var permits = new NodeList<ClassOrInterfaceType>();
        for (var variant : sealed.variants()) {
            permits.add(new ClassOrInterfaceType(null, variant.name()));
        }
        jIface.setPermittedTypes(permits);

        for (var method : sealed.methods()) {
            jIface.addMember(transformMethodDecl(method));
        }

        ctx.sealedVariantMap.put(sealed.name(), sealed.variants());

        return jIface;
    }

    // --- Enums ----------------------------------------------------------------

    EnumDeclaration transformEnumDecl(EnumDecl en) {
        var jEnum = new EnumDeclaration();
        jEnum.setName(en.name());
        jEnum.addModifier(Keyword.PUBLIC);
        for (var variant : en.variants()) {
            jEnum.addEnumConstant(variant);
        }
        return jEnum;
    }

    // --- Methods --------------------------------------------------------------

    MethodDeclaration transformMethodDecl(Ast.MethodDecl method) {
        var jMethod = new MethodDeclaration();
        jMethod.setName(method.name());
        boolean isOverride = method.name().equals("toString") || method.name().equals("equals")
            || method.name().equals("hashCode");
        if (method.isPub() || isOverride) jMethod.addModifier(Keyword.PUBLIC);
        else jMethod.addModifier(Keyword.PRIVATE);
        if (method.isStatic()) jMethod.addModifier(Keyword.STATIC);
        if (method.isAbstract()) jMethod.addModifier(Keyword.ABSTRACT);
        if (isOverride) jMethod.addMarkerAnnotation("Override");
        jMethod.setType(method.returnType() != null ? ctx.transformType(method.returnType()) : new VoidType());

        for (var param : method.params()) {
            var type = param.type() != null ? ctx.transformType(param.type()) : new ClassOrInterfaceType(null, "Object");
            jMethod.addParameter(type, param.name());
        }

        if (method.body() != null) {
            jMethod.setBody(stmts.transformBlock(method.body()));
        } else {
            jMethod.removeBody();
        }

        return jMethod;
    }
}
