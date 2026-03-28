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
            boolean hasMain = topFns.stream().anyMatch(fn -> fn.name().equals("main"));
            if (hasMain) addZnTraceSupport(mainClass);
            for (var fn : topFns) {
                for (var jMethod : decls.transformFnDeclWithOverloads(fn)) {
                    if (fn.name().equals("main") && jMethod.getParameters().isEmpty()) {
                        jMethod.addParameter("String[]", "args");
                        jMethod.setThrownExceptions(new NodeList<>(new ClassOrInterfaceType(null, "Exception")));
                        jMethod.setBody(wrapMainBody(jMethod.getBody().orElse(new BlockStmt())));
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

    /** Add source map field and trace helper to a class that contains main(). */
    private void addZnTraceSupport(com.github.javaparser.ast.body.ClassOrInterfaceDeclaration mainClass) {
        // Placeholder field — Emitter replaces with actual map data
        var field = mainClass.addFieldWithInitializer(
            "String", "_ZN",
            new StringLiteralExpr("__ZN_PLACEHOLDER__"),
            Keyword.PRIVATE, Keyword.STATIC, Keyword.FINAL);

        // Trace rewriting helper method
        var helper = ctx.parseStmt("""
            {
                var _parts = _ZN.split(":", 2);
                var _file = _parts[0];
                var _map = new java.util.HashMap<Integer, Integer>();
                if (_parts.length > 1 && !_parts[1].isEmpty()) {
                    for (var _e : _parts[1].split(",")) {
                        var _kv = _e.split("=");
                        _map.put(Integer.parseInt(_kv[0]), Integer.parseInt(_kv[1]));
                    }
                }
                System.err.println(ex.getClass().getSimpleName() + ": " + ex.getMessage());
                for (var _f : ex.getStackTrace()) {
                    var _zn = _map.get(_f.getLineNumber());
                    if (_zn == null) _zn = _map.get(_f.getLineNumber() - 1);
                    if (_zn != null) {
                        System.err.println("    at " + _file + ":" + _zn + " (" + _f.getMethodName() + ")");
                    }
                }
            }
            """);
        var method = mainClass.addMethod("_znTrace", Keyword.PRIVATE, Keyword.STATIC);
        method.addParameter("Throwable", "ex");
        method.setBody((BlockStmt) helper);
    }

    /** Wrap a main() body in try-catch that calls _znTrace on error. */
    private BlockStmt wrapMainBody(BlockStmt originalBody) {
        var tryBody = new BlockStmt();
        for (var stmt : originalBody.getStatements()) tryBody.addStatement(stmt.clone());

        var catchBody = new BlockStmt();
        catchBody.addStatement(ctx.parseStmt("_znTrace(_znEx);"));
        catchBody.addStatement(ctx.parseStmt("System.exit(1);"));

        var catchClause = new com.github.javaparser.ast.stmt.CatchClause(
            new com.github.javaparser.ast.body.Parameter(
                new ClassOrInterfaceType(null, "Throwable"), "_znEx"),
            catchBody);

        var wrapped = new BlockStmt();
        wrapped.addStatement(new com.github.javaparser.ast.stmt.TryStmt(
            tryBody, new NodeList<>(catchClause), null));
        return wrapped;
    }

    public Result<CompilationUnit> transform(Program program) {
        var cu = ctx.newCU(program);

        if (!program.stmts().isEmpty()) {
            var mainClass = cu.addClass(ctx.className, Keyword.PUBLIC);
            addZnTraceSupport(mainClass);

            var mainMethod = mainClass.addMethod("main", Keyword.PUBLIC, Keyword.STATIC);
            mainMethod.addParameter("String[]", "args");
            mainMethod.setThrownExceptions(new NodeList<>(new ClassOrInterfaceType(null, "Exception")));
            var body = new BlockStmt();
            for (var stmt : program.stmts()) {
                for (var jStmt : stmts.transformStmt(stmt)) {
                    body.addStatement(jStmt);
                }
            }
            mainMethod.setBody(wrapMainBody(body));

            for (var decl : program.decls()) {
                if (decl instanceof FnDecl fn) {
                    for (var m : decls.transformFnDeclWithOverloads(fn)) mainClass.addMember(m);
                }
            }
        }

        return Result.ok(cu);
    }
}
