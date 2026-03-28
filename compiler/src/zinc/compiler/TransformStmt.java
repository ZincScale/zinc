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

import zinc.compiler.Ast.Stmt;
import zinc.compiler.Ast.FnDecl;
import zinc.compiler.Ast.Expr;
import zinc.compiler.Ast.Ident;
import zinc.compiler.Ast.CallExpr;

/**
 * Transforms Zinc AST statements into JavaParser AST statements.
 */
public class TransformStmt {

    private final TransformContext ctx;
    private final TransformExpr exprs;

    public TransformStmt(TransformContext ctx, TransformExpr exprs) {
        this.ctx = ctx;
        this.exprs = exprs;
    }

    // --- Block ----------------------------------------------------------------

    BlockStmt transformBlock(Ast.BlockStmt block) {
        var jBlock = new BlockStmt();
        for (var stmt : block.stmts()) {
            for (var jStmt : transformStmt(stmt)) {
                jBlock.addStatement(jStmt);
            }
        }
        return jBlock;
    }

    // --- Main dispatch --------------------------------------------------------

    List<Statement> transformStmt(Stmt stmt) {
        return switch (stmt) {
            case Ast.VarStmt v -> {
                if (v.orHandler() != null && v.value() != null) yield transformVarWithOrHandlerStmts(v);
                else yield List.of(transformVarStmt(v));
            }
            case Ast.AssignStmt a -> List.of(transformAssignStmt(a));
            case Ast.ReturnStmt r -> List.of(transformReturnStmt(r));
            case Ast.IfStmt i -> List.of(transformIfStmt(i));
            case Ast.ForStmt f -> List.of(transformForStmt(f));
            case Ast.WhileStmt w -> List.of(new WhileStmt(exprs.transformExpr(w.cond()), transformBlock(w.body())));
            case Ast.ExprStmt e -> {
                if (e.orHandler() != null) {
                    yield List.of(transformExprStmtWithOrHandler(e));
                }
                yield List.of(new ExpressionStmt(exprs.transformExpr(e.expr())));
            }
            case Ast.MatchStmt m -> List.of(transformMatchStmt(m));
            case Ast.BreakStmt b -> List.of(new BreakStmt());
            case Ast.ContinueStmt c -> List.of(new ContinueStmt());
            case Ast.BlockStmt b -> List.of(transformBlock(b));
            case Ast.LockStmt l -> List.of(transformLock(l));
            case Ast.WithStmt w -> List.of(transformWith(w));
            case Ast.DeferStmt d -> List.of(new ExpressionStmt(exprs.transformExpr(d.expr())));
            case FnDecl fn -> List.of();
            default -> List.of(new ExpressionStmt(new StringLiteralExpr("/* unsupported: " + stmt.getClass().getSimpleName() + " */")));
        };
    }

    // --- Variable statements --------------------------------------------------

    private Statement transformVarStmt(Ast.VarStmt v) {
        if (ctx.capturedMutables.contains(v.name())) {
            var init = v.value() != null ? exprs.transformExpr(v.value()) : new NullLiteralExpr();
            String elemType = "Object";
            if (v.type() != null) {
                String tn = v.type() instanceof Ast.SimpleType s ? s.name() : "Object";
                elemType = switch (tn) {
                    case "int", "Integer" -> "int";
                    case "double", "Double" -> "double";
                    case "boolean", "Boolean" -> "boolean";
                    case "long", "Long" -> "long";
                    default -> "Object";
                };
            } else if (v.value() instanceof Ast.IntLit) {
                elemType = "int";
            } else if (v.value() instanceof Ast.FloatLit) {
                elemType = "double";
            } else if (v.value() instanceof Ast.BoolLit) {
                elemType = "boolean";
            } else if (v.value() instanceof Ast.StringLit || v.value() instanceof Ast.StringInterpLit) {
                elemType = "Object";
            }
            var arrayType = elemType.equals("Object")
                ? new ClassOrInterfaceType(null, "Object")
                : new PrimitiveType(PrimitiveType.Primitive.valueOf(elemType.toUpperCase()));
            var arrayInit = new ArrayCreationExpr(arrayType,
                new NodeList<>(new ArrayCreationLevel()),
                new ArrayInitializerExpr(new NodeList<>(init)));
            var decl = new VariableDeclarationExpr(new VarType(), "_" + v.name());
            decl.getVariable(0).setInitializer(arrayInit);
            return new ExpressionStmt(decl);
        }

        if (v.type() != null) {
            var type = ctx.transformType(v.type());
            var decl = new VariableDeclarationExpr(type, v.name());
            if (v.value() != null) decl.getVariable(0).setInitializer(
                exprs.transformExprInContext(v.value(), v.type()));
            return new ExpressionStmt(decl);
        }
        var decl = new VariableDeclarationExpr(new VarType(), v.name());
        if (v.value() != null) decl.getVariable(0).setInitializer(exprs.transformExpr(v.value()));
        return new ExpressionStmt(decl);
    }

    private List<Statement> transformVarWithOrHandlerStmts(Ast.VarStmt v) {
        var stmts = new java.util.ArrayList<Statement>();

        com.github.javaparser.ast.type.Type javaType;
        if (v.type() != null) {
            javaType = ctx.transformType(v.type());
        } else {
            var resolved = ctx.resolvedTypes.get(v.line() + ":" + v.name());
            if (resolved != null && !resolved.name().equals("any")) {
                javaType = ctx.typeInfoToJavaType(resolved);
            } else {
                javaType = new ClassOrInterfaceType(null, "Object");
            }
        }
        stmts.add(new ExpressionStmt(new VariableDeclarationExpr(javaType, v.name())));

        var tryBody = new BlockStmt();
        tryBody.addStatement(new ExpressionStmt(new AssignExpr(
            new NameExpr(v.name()), exprs.transformExpr(v.value()), AssignExpr.Operator.ASSIGN)));

        var catchBody = new BlockStmt();
        if (v.orHandler().body() != null) {
            var handlerStmts = v.orHandler().body().stmts();
            if (handlerStmts.size() == 1 && handlerStmts.getFirst() instanceof Ast.ExprStmt es) {
                catchBody.addStatement(new ExpressionStmt(new AssignExpr(
                    new NameExpr(v.name()), exprs.transformExpr(es.expr()), AssignExpr.Operator.ASSIGN)));
            } else {
                for (int idx = 0; idx < handlerStmts.size(); idx++) {
                    var stmt = handlerStmts.get(idx);
                    boolean isLast = (idx == handlerStmts.size() - 1);
                    if (isLast && stmt instanceof Ast.ExprStmt es && es.orHandler() == null) {
                        catchBody.addStatement(new ExpressionStmt(new AssignExpr(
                            new NameExpr(v.name()), exprs.transformExpr(es.expr()), AssignExpr.Operator.ASSIGN)));
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
        return new ExpressionStmt(new AssignExpr(exprs.transformExpr(a.target()), exprs.transformExpr(a.value()), op));
    }

    // --- Control flow ---------------------------------------------------------

    private Statement transformReturnStmt(Ast.ReturnStmt r) {
        if (r.value() == null) return new ReturnStmt();

        if (r.value() instanceof CallExpr call
            && call.callee() instanceof Ident id
            && id.name().equals("Error")) {
            if (!call.args().isEmpty()) {
                var arg = call.args().getFirst();
                if (arg instanceof CallExpr innerCall
                    && innerCall.callee() instanceof Ident innerId
                    && Character.isUpperCase(innerId.name().charAt(0))) {
                    var args = new NodeList<Expression>();
                    for (var a : innerCall.args()) args.add(exprs.transformExpr(a));
                    return new ThrowStmt(new ObjectCreationExpr(null,
                        new ClassOrInterfaceType(null, innerId.name()), args));
                }
                return new ThrowStmt(new ObjectCreationExpr(null,
                    new ClassOrInterfaceType(null, "RuntimeException"),
                    new NodeList<>(exprs.transformExpr(arg))));
            }
            return new ThrowStmt(new ObjectCreationExpr(null,
                new ClassOrInterfaceType(null, "RuntimeException"),
                new NodeList<>(new StringLiteralExpr("error"))));
        }

        return new ReturnStmt(exprs.transformExpr(r.value()));
    }

    private Statement transformIfStmt(Ast.IfStmt i) {
        var jIf = new IfStmt();
        jIf.setCondition(exprs.transformExpr(i.cond()));
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
            if (!f.indexVar().isEmpty()) {
                var forEach = new ForEachStmt();
                forEach.setVariable(new VariableDeclarationExpr(new VarType(), "_entry"));
                forEach.setIterable(new MethodCallExpr(exprs.transformExpr(f.range()), "entrySet"));
                var body = transformBlock(f.body());
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

            var forEach = new ForEachStmt();
            forEach.setVariable(new VariableDeclarationExpr(new VarType(), f.item()));
            var iterable = exprs.transformExpr(f.range());
            if (f.range() instanceof Ast.RangeExpr) {
                iterable = new MethodCallExpr(
                    new MethodCallExpr(iterable, "boxed"), "toList");
            }
            forEach.setIterable(iterable);
            forEach.setBody(transformBlock(f.body()));
            return forEach;
        }
        return new BlockStmt(); // TODO: C-style for
    }

    // --- Or handler -----------------------------------------------------------

    private Statement transformExprStmtWithOrHandler(Ast.ExprStmt e) {
        var tryBody = new BlockStmt();
        tryBody.addStatement(new ExpressionStmt(exprs.transformExpr(e.expr())));

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

    // --- Match ----------------------------------------------------------------

    private Statement transformMatchStmt(Ast.MatchStmt m) {
        boolean hasRecordPatterns = m.cases().stream()
            .anyMatch(c -> c.pattern() instanceof CallExpr);

        if (hasRecordPatterns) {
            return transformMatchAsSwitch(m);
        }

        var subject = exprs.transformExpr(m.subject());
        IfStmt firstIf = null;
        IfStmt lastIf = null;
        Statement defaultCase = null;

        for (var c : m.cases()) {
            if (c.pattern() == null) {
                defaultCase = transformBlock(c.body());
            } else {
                var cond = new MethodCallExpr(
                    new NameExpr("java.util.Objects"), "equals",
                    new NodeList<>(subject.clone(), exprs.transformExpr(c.pattern())));
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

    private Statement transformMatchAsSwitch(Ast.MatchStmt m) {
        var subject = exprs.transformExpr(m.subject());
        var sb = new StringBuilder();
        sb.append("switch (").append(subject).append(") {\n");
        for (var c : m.cases()) {
            if (c.pattern() == null) {
                sb.append("    default -> ");
                sb.append(transformBlock(c.body()));
                sb.append("\n");
            } else if (c.pattern() instanceof CallExpr call && call.callee() instanceof Ident typeName) {
                sb.append("    case ").append(typeName.name()).append("(");
                var args = call.args();
                if (args.isEmpty()) {
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
                sb.append("    case ").append(exprs.transformExpr(c.pattern())).append(" -> ");
                sb.append(transformBlock(c.body()));
                sb.append("\n");
            }
        }
        sb.append("}");
        return ctx.parseStmt(sb.toString());
    }

    // --- Concurrency ----------------------------------------------------------

    private Statement transformLock(Ast.LockStmt l) {
        var mutex = exprs.transformExpr(l.mutex());
        var lockCall = new ExpressionStmt(new MethodCallExpr(mutex.clone(), "lock"));
        var tryBody = transformBlock(l.body());
        var finallyBody = new BlockStmt();
        finallyBody.addStatement(new ExpressionStmt(new MethodCallExpr(mutex.clone(), "unlock")));
        var outerBlock = new BlockStmt();
        outerBlock.addStatement(lockCall);
        outerBlock.addStatement(new TryStmt(tryBody, new NodeList<>(), finallyBody));
        return outerBlock;
    }

    private Statement transformWith(Ast.WithStmt w) {
        var tryBody = transformBlock(w.body());
        var resources = new NodeList<Expression>();
        for (var res : w.resources()) {
            var decl = new VariableDeclarationExpr(new VarType(), res.name());
            decl.getVariable(0).setInitializer(exprs.transformExpr(res.value()));
            resources.add(decl);
        }
        return new TryStmt(resources, tryBody, new NodeList<>(), null);
    }

    // --- Prescan for captured mutables ----------------------------------------

    void prescanCapturedMutables(List<Ast.Stmt> stmts) {
        for (var stmt : stmts) {
            prescanStmt(stmt);
        }
    }

    private void prescanStmt(Ast.Stmt stmt) {
        switch (stmt) {
            case Ast.ExprStmt e -> prescanExpr(e.expr());
            case Ast.VarStmt v -> { if (v.value() != null) prescanExpr(v.value()); }
            case Ast.AssignStmt a -> { if (a.value() != null) prescanExpr(a.value()); }
            case Ast.IfStmt i -> {
                prescanStmt(i.then());
                if (i.elseStmt() != null) prescanStmt(i.elseStmt());
            }
            case Ast.BlockStmt b -> prescanCapturedMutables(b.stmts());
            case Ast.ForStmt f -> prescanStmt(f.body());
            case Ast.WhileStmt w -> prescanStmt(w.body());
            default -> {}
        }
    }

    private void prescanExpr(Ast.Expr expr) {
        if (expr instanceof Ast.SpawnExpr spawn) {
            ctx.capturedMutables.addAll(collectAssignedVars(spawn.body()));
            if (spawn.orHandler() != null && spawn.orHandler().body() != null) {
                ctx.capturedMutables.addAll(collectAssignedVars(spawn.orHandler().body()));
            }
        }
    }

    /** Collect variable names assigned inside a Zinc statement tree. */
    java.util.Set<String> collectAssignedVars(Ast.Stmt stmt) {
        var vars = new java.util.HashSet<String>();
        switch (stmt) {
            case Ast.AssignStmt a -> {
                if (a.target() instanceof Ast.Ident id) vars.add(id.name());
            }
            case Ast.BlockStmt b -> { for (var s : b.stmts()) vars.addAll(collectAssignedVars(s)); }
            case Ast.IfStmt i -> {
                vars.addAll(collectAssignedVars(i.then()));
                if (i.elseStmt() != null) vars.addAll(collectAssignedVars(i.elseStmt()));
            }
            case Ast.ForStmt f -> vars.addAll(collectAssignedVars(f.body()));
            case Ast.WhileStmt w -> vars.addAll(collectAssignedVars(w.body()));
            case Ast.LockStmt l -> vars.addAll(collectAssignedVars(l.body()));
            case Ast.ExprStmt e -> {}
            default -> {}
        }
        return vars;
    }
}
