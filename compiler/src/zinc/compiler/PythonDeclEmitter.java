// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.HashSet;
import java.util.stream.Collectors;

/**
 * Emits Python top-level declarations from Zinc AST declaration nodes.
 */
public class PythonDeclEmitter {

    private final PythonEmitContext ctx;
    private final PythonExprEmitter exprs;
    private final PythonTypeEmitter types;
    private final PythonStmtEmitter stmts;

    public PythonDeclEmitter(PythonEmitContext ctx, PythonExprEmitter exprs,
                              PythonTypeEmitter types, PythonStmtEmitter stmts) {
        this.ctx = ctx;
        this.exprs = exprs;
        this.types = types;
        this.stmts = stmts;
    }

    // --- Top-level dispatch ---------------------------------------------------

    void emitTopLevelDecl(Ast.TopLevelDecl decl) {
        switch (decl) {
            case Ast.FnDecl fn -> emitFnDecl(fn);
            case Ast.ClassDecl cls -> emitClassDecl(cls);
            case Ast.DataClassDecl data -> emitDataClassDecl(data);
            case Ast.SealedClassDecl sealed -> emitSealedClassDecl(sealed);
            case Ast.EnumDecl en -> emitEnumDecl(en);
            case Ast.InterfaceDecl iface -> emitInterfaceDecl(iface);
            case Ast.ConstDecl con -> emitConstDecl(con);
        }
    }

    // --- Functions ------------------------------------------------------------

    void emitFnDecl(Ast.FnDecl fn) {
        for (var ann : fn.annotations()) {
            stmts.emitAnnotation(ann);
        }

        String retType = fn.returnType() != null ? types.emitType(fn.returnType()) : null;
        String params = stmts.emitParams(fn.params());

        String fnName = fn.name().equals("main") ? "main" : ctx.safeVarName(fn.name());
        if (!fnName.equals(fn.name())) {
            ctx.renamedVars.put(fn.name(), fnName);
        }
        ctx.line("def " + fnName + "(" + params + ")" + (retType != null ? " -> " + retType : "") + ":");

        ctx.indent++;
        if (fn.body() != null && !fn.body().stmts().isEmpty()) {
            stmts.emitBlock(fn.body());
        } else {
            ctx.line("pass");
        }
        ctx.indent--;
    }

    // --- Classes --------------------------------------------------------------

    private void emitClassDecl(Ast.ClassDecl cls) {
        ctx.currentClass = cls.name();
        ctx.currentClassFields.clear();
        ctx.currentClassMethods.clear();
        for (var field : cls.fields()) {
            ctx.currentClassFields.add(field.name());
        }
        for (var method : cls.methods()) {
            ctx.currentClassMethods.add(method.name());
        }
        for (var ann : cls.annotations()) {
            stmts.emitAnnotation(ann);
        }

        String parents = cls.parents().isEmpty() ? "" : "(" + String.join(", ", cls.parents()) + ")";
        ctx.line("class " + cls.name() + parents + ":");
        ctx.indent++;

        boolean hasContent = false;

        // Class-level constants
        for (var field : cls.fields()) {
            if (field.isConst()) {
                ctx.line(field.name() + " = " + exprs.emitExpr(field.defaultValue()));
                hasContent = true;
            }
        }

        // Constructors
        for (var ctor : cls.ctors()) {
            emitConstructor(cls, ctor);
            hasContent = true;
        }

        // If no constructor but has non-const fields, generate __init__
        if (cls.ctors().isEmpty() && cls.fields().stream().anyMatch(f -> !f.isConst())) {
            emitDefaultInit(cls);
            hasContent = true;
        }

        // Properties for pub/readonly fields
        for (var field : cls.fields()) {
            if (field.isConst()) continue;
            if (field.isPub() || field.isReadonly() || field.isInit()) {
                emitProperty(field);
                hasContent = true;
            }
        }

        // Methods
        for (var method : cls.methods()) {
            emitMethodDecl(method);
            hasContent = true;
        }

        if (!hasContent) {
            ctx.line("pass");
        }

        ctx.indent--;
        ctx.currentClass = null;
        ctx.currentClassFields.clear();
        ctx.currentClassMethods.clear();
    }

    private void emitConstructor(Ast.ClassDecl cls, Ast.CtorDecl ctor) {
        String params = stmts.emitParams(ctor.params());
        ctx.line("def __init__(self" + (params.isEmpty() ? "" : ", " + params) + "):");
        ctx.indent++;

        // Super call
        if (!ctor.superArgs().isEmpty()) {
            var superArgsStr = ctor.superArgs().stream()
                .map(exprs::emitExpr).collect(Collectors.joining(", "));
            ctx.line("super().__init__(" + superArgsStr + ")");
        }

        if (ctor.body() != null && !ctor.body().stmts().isEmpty()) {
            stmts.emitBlock(ctor.body());
        } else {
            ctx.line("pass");
        }

        ctx.indent--;
        ctx.blank();
    }

    private void emitDefaultInit(Ast.ClassDecl cls) {
        var nonConstFields = cls.fields().stream().filter(f -> !f.isConst()).toList();
        ctx.line("def __init__(self):");
        ctx.indent++;
        for (var field : nonConstFields) {
            String val = field.defaultValue() != null ? exprs.emitExpr(field.defaultValue()) : "None";
            ctx.line("self._" + field.name() + " = " + val);
        }
        ctx.indent--;
        ctx.blank();
    }

    private void emitProperty(Ast.FieldDecl field) {
        // Getter
        ctx.line("@property");
        ctx.line("def " + field.name() + "(self)" +
            (field.type() != null ? " -> " + types.emitType(field.type()) : "") + ":");
        ctx.indent++;
        ctx.line("return self._" + field.name());
        ctx.indent--;
        ctx.blank();

        // Setter (only for pub, not readonly/init)
        if (field.isPub() && !field.isReadonly() && !field.isInit()) {
            ctx.line("@" + field.name() + ".setter");
            ctx.line("def " + field.name() + "(self, value" +
                (field.type() != null ? ": " + types.emitType(field.type()) : "") + "):");
            ctx.indent++;
            ctx.line("self._" + field.name() + " = value");
            ctx.indent--;
            ctx.blank();
        }
    }

    // --- Methods --------------------------------------------------------------

    private void emitMethodDecl(Ast.MethodDecl method) {
        for (var ann : method.annotations()) {
            stmts.emitAnnotation(ann);
        }

        if (method.isStatic()) {
            ctx.line("@staticmethod");
        }

        String params = stmts.emitParams(method.params());
        String retType = method.returnType() != null ? " -> " + types.emitType(method.returnType()) : "";

        if (method.isStatic()) {
            ctx.line("def " + method.name() + "(" + params + ")" + retType + ":");
        } else {
            String pyName = switch (method.name()) {
                case "init" -> "__init__";
                case "toString" -> "__repr__";
                case "equals" -> "__eq__";
                case "hashCode" -> "__hash__";
                default -> method.name();
            };
            ctx.line("def " + pyName + "(self" + (params.isEmpty() ? "" : ", " + params) + ")" + retType + ":");
        }

        ctx.indent++;
        ctx.insideMethod = true;
        if (method.isAbstract() || method.body() == null || method.body().stmts().isEmpty()) {
            ctx.line("pass");
        } else {
            stmts.emitBlock(method.body());
        }
        ctx.insideMethod = false;
        ctx.indent--;
        ctx.blank();
    }

    // --- Data Classes ---------------------------------------------------------

    private void emitDataClassDecl(Ast.DataClassDecl data) {
        ctx.addFromImport("from dataclasses import dataclass");

        String parents = data.parents().isEmpty() ? "" : "(" + String.join(", ", data.parents()) + ")";

        ctx.line("@dataclass(frozen=True, slots=True)");
        ctx.line("class " + data.name() + parents + ":");
        ctx.indent++;

        if (data.params().isEmpty() && data.methods().isEmpty()) {
            ctx.line("pass");
        } else {
            for (var param : data.params()) {
                String typeStr = param.type() != null ? types.emitType(param.type()) : "object";
                if (param.defaultValue() != null) {
                    ctx.line(param.name() + ": " + typeStr + " = " + exprs.emitExpr(param.defaultValue()));
                } else {
                    ctx.line(param.name() + ": " + typeStr);
                }
            }

            for (var method : data.methods()) {
                ctx.blank();
                emitMethodDecl(method);
            }
        }

        ctx.indent--;
    }

    // --- Sealed Classes -------------------------------------------------------

    private void emitSealedClassDecl(Ast.SealedClassDecl sealed) {
        String parents = sealed.parents().isEmpty() ? "" : "(" + String.join(", ", sealed.parents()) + ")";
        ctx.line("class " + sealed.name() + parents + ":");
        ctx.indent++;

        boolean hasContent = false;
        for (var method : sealed.methods()) {
            emitMethodDecl(method);
            hasContent = true;
        }
        if (!hasContent) {
            ctx.line("pass");
        }

        ctx.indent--;
        ctx.blank();

        // Variant data classes
        for (var variant : sealed.variants()) {
            ctx.addFromImport("from dataclasses import dataclass");
            ctx.line("@dataclass(frozen=True, slots=True)");
            ctx.line("class " + variant.name() + "(" + sealed.name() + "):");
            ctx.indent++;

            if (variant.params().isEmpty() && variant.methods().isEmpty()) {
                ctx.line("pass");
            } else {
                for (var param : variant.params()) {
                    String typeStr = param.type() != null ? types.emitType(param.type()) : "object";
                    if (param.defaultValue() != null) {
                        ctx.line(param.name() + ": " + typeStr + " = " + exprs.emitExpr(param.defaultValue()));
                    } else {
                        ctx.line(param.name() + ": " + typeStr);
                    }
                }
                for (var method : variant.methods()) {
                    ctx.blank();
                    emitMethodDecl(method);
                }
            }

            ctx.indent--;
            ctx.blank();
        }
    }

    // --- Enums ----------------------------------------------------------------

    private void emitEnumDecl(Ast.EnumDecl en) {
        ctx.addFromImport("from enum import Enum, auto");
        ctx.line("class " + en.name() + "(Enum):");
        ctx.indent++;
        for (var variant : en.variants()) {
            ctx.line(variant + " = auto()");
        }
        ctx.indent--;
    }

    // --- Interfaces -----------------------------------------------------------

    private void emitInterfaceDecl(Ast.InterfaceDecl iface) {
        ctx.addFromImport("from abc import ABC, abstractmethod");
        ctx.line("class " + iface.name() + "(ABC):");
        ctx.indent++;

        if (iface.methods().isEmpty()) {
            ctx.line("pass");
        } else {
            for (var sig : iface.methods()) {
                String params = stmts.emitParams(sig.params());
                String retType = sig.returnType() != null ? " -> " + types.emitType(sig.returnType()) : "";
                ctx.line("@abstractmethod");
                ctx.line("def " + sig.name() + "(self" + (params.isEmpty() ? "" : ", " + params) + ")" + retType + ":");
                ctx.indent++;
                ctx.line("pass");
                ctx.indent--;
                ctx.blank();
            }
        }

        ctx.indent--;
    }

    // --- Constants ------------------------------------------------------------

    private void emitConstDecl(Ast.ConstDecl con) {
        ctx.line(con.name() + " = " + exprs.emitExpr(con.value()));
    }
}
