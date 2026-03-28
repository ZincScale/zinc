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
import zinc.compiler.Ast.InterfaceDecl;

/**
 * Transforms Zinc AST into JavaParser AST.
 * Delegates to TransformDecl, TransformStmt, TransformExpr, TransformContext.
 */
public class Transformer {

    private final TransformContext ctx;
    private final TransformExpr exprs;
    private final TransformStmt stmts;
    private final TransformDecl decls;

    public Transformer() {
        this.ctx = new TransformContext();
        this.exprs = new TransformExpr(ctx);
        this.stmts = new TransformStmt(ctx, exprs);
        this.decls = new TransformDecl(ctx, exprs, stmts);
        this.exprs.setStmtTransformer(stmts);
    }

    public Transformer(String className) {
        this.ctx = new TransformContext(className);
        this.exprs = new TransformExpr(ctx);
        this.stmts = new TransformStmt(ctx, exprs);
        this.decls = new TransformDecl(ctx, exprs, stmts);
        this.exprs.setStmtTransformer(stmts);
    }

    public Transformer(String className, java.util.Map<String, TypeInfo> resolvedTypes) {
        this.ctx = new TransformContext(className, resolvedTypes);
        this.exprs = new TransformExpr(ctx);
        this.stmts = new TransformStmt(ctx, exprs);
        this.decls = new TransformDecl(ctx, exprs, stmts);
        this.exprs.setStmtTransformer(stmts);
    }

    /** Register an interface name from another file (for extends/implements detection). */
    public void registerInterface(String name) {
        ctx.interfaceNames.add(name);
    }

    // --- Entry point ---------------------------------------------------------

    /**
     * Transforms a Zinc program into multiple CompilationUnits — one per top-level type.
     * Script mode programs get a single Main class.
     */
    public Result<List<CompilationUnit>> transformAll(Program program) {
        // Pre-scan: collect interface names
        for (var decl : program.decls()) {
            if (decl instanceof InterfaceDecl iface) ctx.interfaceNames.add(iface.name());
        }

        // Pre-scan: collect variables captured by spawns (need Object[] wrapping)
        stmts.prescanCapturedMutables(program.stmts());

        var units = new java.util.ArrayList<CompilationUnit>();

        // Script mode — Main class for stmts + top-level fns, separate CUs for types
        if (!program.stmts().isEmpty()) {
            var result = transform(program);
            if (result.isErr()) return Result.err(((Result.Err<?>) result).errors());
            units.add(result.unwrap());

            for (var decl : program.decls()) {
                if (decl instanceof FnDecl) continue;
                decls.emitDeclToUnits(decl, program, units);
            }
            return Result.ok(units);
        }

        // Collect top-level functions — group into one class
        var topFns = program.decls().stream()
            .filter(d -> d instanceof FnDecl).map(d -> (FnDecl) d).toList();

        if (!topFns.isEmpty()) {
            var cu = ctx.newCU(program);
            var mainClass = cu.addClass(ctx.className, Keyword.PUBLIC);
            for (var fn : topFns) {
                for (var jMethod : decls.transformFnDeclWithOverloads(fn)) {
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
            decls.emitDeclToUnits(decl, program, units);
        }

        return Result.ok(units);
    }

    public Result<CompilationUnit> transform(Program program) {
        var cu = ctx.newCU(program);

        if (!program.stmts().isEmpty()) {
            var mainClass = cu.addClass(ctx.className, Keyword.PUBLIC);
            var mainMethod = mainClass.addMethod("main", Keyword.PUBLIC, Keyword.STATIC);
            mainMethod.addParameter("String[]", "args");
            mainMethod.setThrownExceptions(new NodeList<>(new ClassOrInterfaceType(null, "Exception")));
            var body = new BlockStmt();
            for (var stmt : program.stmts()) {
                for (var jStmt : stmts.transformStmt(stmt)) {
                    body.addStatement(jStmt);
                }
            }
            mainMethod.setBody(body);

            for (var decl : program.decls()) {
                if (decl instanceof FnDecl fn) {
                    for (var m : decls.transformFnDeclWithOverloads(fn)) mainClass.addMember(m);
                }
            }
        }

        return Result.ok(cu);
    }
}
