// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.ArrayList;
import java.util.stream.Collectors;

/**
 * Emits Python statements from Zinc AST statement nodes.
 */
public class PythonStmtEmitter {

    private final PythonEmitContext ctx;
    private final PythonExprEmitter exprs;
    private final PythonTypeEmitter types;

    public PythonStmtEmitter(PythonEmitContext ctx, PythonExprEmitter exprs, PythonTypeEmitter types) {
        this.ctx = ctx;
        this.exprs = exprs;
        this.types = types;
    }

    // --- Main dispatch --------------------------------------------------------

    void emitStmt(Ast.Stmt stmt) {
        switch (stmt) {
            case Ast.BlockStmt block -> emitBlock(block);
            case Ast.VarStmt var_ -> emitVarStmt(var_);
            case Ast.TupleVarStmt tv -> emitTupleVarStmt(tv);
            case Ast.AssignStmt assign -> emitAssignStmt(assign);
            case Ast.ReturnStmt ret -> emitReturnStmt(ret);
            case Ast.IfStmt if_ -> emitIfStmt(if_);
            case Ast.ForStmt for_ -> emitForStmt(for_);
            case Ast.WhileStmt while_ -> emitWhileStmt(while_);
            case Ast.MatchStmt match -> emitMatchStmt(match);
            case Ast.BreakStmt b -> ctx.line("break");
            case Ast.ContinueStmt c -> ctx.line("continue");
            case Ast.ExprStmt expr -> emitExprStmt(expr);
            case Ast.FnDecl fn -> emitFnDecl(fn);
            case Ast.WithStmt with -> emitWithStmt(with);
            case Ast.LockStmt lock -> emitLockStmt(lock);
            case Ast.DeferStmt defer -> emitDeferStmt(defer);
        }
    }

    void emitBlock(Ast.BlockStmt block) {
        if (block.stmts().isEmpty()) {
            ctx.line("pass");
            return;
        }
        for (var stmt : block.stmts()) {
            emitStmt(stmt);
        }
    }

    // --- Variable statements --------------------------------------------------

    private void emitVarStmt(Ast.VarStmt var_) {
        String name = ctx.safeVarName(var_.name());
        if (!name.equals(var_.name())) {
            ctx.renamedVars.put(var_.name(), name);
        }
        String value = exprs.emitExpr(var_.value());

        if (var_.orHandler() != null) {
            emitOrWrapped(name, value, var_.orHandler());
            return;
        }

        if (var_.type() != null) {
            ctx.line(name + ": " + types.emitType(var_.type()) + " = " + value);
        } else {
            ctx.line(name + " = " + value);
        }
    }

    private void emitTupleVarStmt(Ast.TupleVarStmt tv) {
        String value = exprs.emitExpr(tv.value());
        if (tv.orHandler() != null) {
            String tmpName = "_tmp_" + tv.names().getFirst();
            emitOrWrapped(tmpName, value, tv.orHandler());
            for (int i = 0; i < tv.names().size(); i++) {
                ctx.line(tv.names().get(i) + " = " + tmpName + "[" + i + "]");
            }
        } else {
            ctx.line(String.join(", ", tv.names()) + " = " + value);
        }
    }

    private void emitAssignStmt(Ast.AssignStmt assign) {
        String target = exprs.emitExpr(assign.target());
        String value = exprs.emitExpr(assign.value());

        // Handle this.x = y → self._x = y (inside class methods)
        if (assign.target() instanceof Ast.SelectorExpr sel
                && sel.object() instanceof Ast.ThisExpr) {
            target = "self._" + sel.field();
        }

        if (assign.orHandler() != null) {
            emitOrWrapped(target, value, assign.orHandler());
        } else {
            ctx.line(target + " " + assign.op() + " " + value);
        }
    }

    // --- Control flow ---------------------------------------------------------

    private void emitReturnStmt(Ast.ReturnStmt ret) {
        if (ret.value() == null) {
            ctx.line("return");
        } else if (ret.value() instanceof Ast.CallExpr call
                && call.callee() instanceof Ast.Ident id && id.name().equals("Error")) {
            ctx.addFromImport("from " + ctx.runtimeImportPrefix() + "zinc_runtime import ZincError");
            var args = call.args().stream().map(exprs::emitExpr)
                .collect(Collectors.joining(", "));
            ctx.line("raise ZincError(" + args + ")");
        } else {
            ctx.line("return " + exprs.emitExpr(ret.value()));
        }
    }

    private void emitIfStmt(Ast.IfStmt if_) {
        ctx.line("if " + exprs.emitExpr(if_.cond()) + ":");
        ctx.indent++;
        emitBlock(if_.then());
        ctx.indent--;

        if (if_.elseStmt() != null) {
            if (if_.elseStmt() instanceof Ast.IfStmt elseIf) {
                ctx.raw("elif " + exprs.emitExpr(elseIf.cond()) + ":");
                ctx.indent++;
                emitBlock(elseIf.then());
                ctx.indent--;
                if (elseIf.elseStmt() != null) {
                    emitElseChain(elseIf.elseStmt());
                }
            } else if (if_.elseStmt() instanceof Ast.BlockStmt elseBlock) {
                ctx.line("else:");
                ctx.indent++;
                emitBlock(elseBlock);
                ctx.indent--;
            }
        }
    }

    private void emitElseChain(Ast.Stmt elseStmt) {
        if (elseStmt instanceof Ast.IfStmt elseIf) {
            ctx.raw("elif " + exprs.emitExpr(elseIf.cond()) + ":");
            ctx.indent++;
            emitBlock(elseIf.then());
            ctx.indent--;
            if (elseIf.elseStmt() != null) {
                emitElseChain(elseIf.elseStmt());
            }
        } else if (elseStmt instanceof Ast.BlockStmt elseBlock) {
            ctx.line("else:");
            ctx.indent++;
            emitBlock(elseBlock);
            ctx.indent--;
        }
    }

    private void emitForStmt(Ast.ForStmt for_) {
        if (for_.isRange()) {
            String item = for_.item();
            String rangeExpr = exprs.emitExpr(for_.range());

            if (for_.indexVar() != null && !for_.indexVar().isEmpty()) {
                ctx.line("for " + for_.indexVar() + ", " + item + " in " + rangeExpr + ".items():");
            } else if (for_.range() instanceof Ast.RangeExpr range) {
                String start = exprs.emitExpr(range.start());
                String end = exprs.emitExpr(range.end());
                if (range.inclusive()) {
                    ctx.line("for " + item + " in range(" + start + ", " + end + " + 1):");
                } else {
                    ctx.line("for " + item + " in range(" + start + ", " + end + "):");
                }
            } else {
                ctx.line("for " + item + " in " + rangeExpr + ":");
            }
        } else {
            // C-style for loop → while loop
            if (for_.init() != null) emitStmt(for_.init());
            ctx.line("while " + (for_.cond() != null ? exprs.emitExpr(for_.cond()) : "True") + ":");
            ctx.indent++;
            emitBlock(for_.body());
            if (for_.post() != null) emitStmt(for_.post());
            ctx.indent--;
            return;
        }

        ctx.indent++;
        emitBlock(for_.body());
        ctx.indent--;
    }

    private void emitWhileStmt(Ast.WhileStmt while_) {
        ctx.line("while " + exprs.emitExpr(while_.cond()) + ":");
        ctx.indent++;
        emitBlock(while_.body());
        ctx.indent--;
    }

    // --- Match ----------------------------------------------------------------

    private void emitMatchStmt(Ast.MatchStmt match) {
        ctx.line("match " + exprs.emitExpr(match.subject()) + ":");
        ctx.indent++;
        for (var case_ : match.cases()) {
            emitMatchCase(case_);
        }
        ctx.indent--;
    }

    private void emitMatchCase(Ast.MatchCase case_) {
        String pattern = emitMatchPattern(case_.pattern());
        ctx.line("case " + pattern + ":");
        ctx.indent++;
        emitBlock(case_.body());
        ctx.indent--;
    }

    private String emitMatchPattern(Ast.Expr pattern) {
        if (pattern == null) return "_";
        return switch (pattern) {
            case Ast.Ident id -> {
                if (id.name().equals("_")) yield "_";
                yield id.name();
            }
            case Ast.StringLit s -> "\"" + s.value() + "\"";
            case Ast.IntLit i -> i.value();
            case Ast.BoolLit b -> b.value() ? "True" : "False";
            case Ast.NullLit n -> "None";
            case Ast.CallExpr call -> {
                String callee = exprs.emitExpr(call.callee());
                if (call.args().isEmpty()) {
                    yield callee + "()";
                }
                var args = call.args().stream()
                    .map(exprs::emitExpr)
                    .collect(Collectors.joining(", "));
                yield callee + "(" + args + ")";
            }
            default -> exprs.emitExpr(pattern);
        };
    }

    // --- Expression statements ------------------------------------------------

    private void emitExprStmt(Ast.ExprStmt expr) {
        if (expr.orHandler() != null) {
            emitOrWrapped(null, exprs.emitExpr(expr.expr()), expr.orHandler());
        } else {
            ctx.line(exprs.emitExpr(expr.expr()));
        }
    }

    // --- Resource / lock / defer ----------------------------------------------

    private void emitWithStmt(Ast.WithStmt with) {
        var parts = new ArrayList<String>();
        for (var res : with.resources()) {
            parts.add(exprs.emitExpr(res.value()) + " as " + res.name());
        }
        ctx.line("with " + String.join(", ", parts) + ":");
        ctx.indent++;
        emitBlock(with.body());
        ctx.indent--;
    }

    private void emitLockStmt(Ast.LockStmt lock) {
        ctx.addImport("threading");
        ctx.line("with " + exprs.emitExpr(lock.mutex()) + ":");
        ctx.indent++;
        emitBlock(lock.body());
        ctx.indent--;
    }

    private void emitDeferStmt(Ast.DeferStmt defer) {
        ctx.addImport("atexit");
        ctx.line("atexit.register(lambda: " + exprs.emitExpr(defer.expr()) + ")");
    }

    // --- Nested function declarations -----------------------------------------

    private void emitFnDecl(Ast.FnDecl fn) {
        for (var ann : fn.annotations()) {
            emitAnnotation(ann);
        }

        String retType = fn.returnType() != null ? types.emitType(fn.returnType()) : null;
        String params = emitParams(fn.params());

        String fnName = fn.name().equals("main") ? "main" : ctx.safeVarName(fn.name());
        if (!fnName.equals(fn.name())) {
            ctx.renamedVars.put(fn.name(), fnName);
        }
        ctx.line("def " + fnName + "(" + params + ")" + (retType != null ? " -> " + retType : "") + ":");

        ctx.indent++;
        if (fn.body() != null && !fn.body().stmts().isEmpty()) {
            emitBlock(fn.body());
        } else {
            ctx.line("pass");
        }
        ctx.indent--;
    }

    // --- Or handler -----------------------------------------------------------

    void emitOrWrapped(String target, String value, Ast.OrHandler handler) {
        ctx.line("try:");
        ctx.indent++;
        if (target != null) {
            ctx.line(target + " = " + value);
        } else {
            ctx.line(value);
        }
        ctx.indent--;

        if (handler.matchCases() != null && !handler.matchCases().isEmpty()) {
            for (var mc : handler.matchCases()) {
                ctx.line("except " + mc.type() + " as err:");
                ctx.indent++;
                emitBlock(mc.body());
                ctx.indent--;
            }
        } else {
            ctx.line("except Exception as err:");
            ctx.indent++;
            if (handler.body() != null && !handler.body().stmts().isEmpty()) {
                var stmts = handler.body().stmts();
                if (target != null && stmts.size() == 1 && stmts.getFirst() instanceof Ast.ExprStmt expr
                        && expr.orHandler() == null) {
                    ctx.line(target + " = " + exprs.emitExpr(expr.expr()));
                } else {
                    emitBlock(handler.body());
                }
            } else {
                if (target != null) {
                    ctx.line(target + " = None");
                } else {
                    ctx.line("pass");
                }
            }
            ctx.indent--;
        }
    }

    // --- Shared helpers (also used by DeclEmitter) ----------------------------

    String emitParams(java.util.List<Ast.ParamDecl> params) {
        var parts = new ArrayList<String>();
        for (var p : params) {
            String part = p.name();
            if (p.type() != null) {
                part += ": " + types.emitType(p.type());
            }
            if (p.isVariadic()) {
                part = "*" + part;
            }
            if (p.defaultValue() != null) {
                part += " = " + exprs.emitExpr(p.defaultValue());
            }
            parts.add(part);
        }
        return String.join(", ", parts);
    }

    void emitAnnotation(Ast.Annotation ann) {
        switch (ann.name()) {
            case "Override" -> {}
            case "Deprecated" -> ctx.line("# @deprecated");
            default -> ctx.line("# @" + ann.name() + (ann.args().isEmpty() ? "" : "(" + String.join(", ", ann.args()) + ")"));
        }
    }
}
