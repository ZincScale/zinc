// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.*;
import java.util.stream.Collectors;

/**
 * Emits Python expressions from Zinc AST expression nodes.
 */
public class PythonExprEmitter {

    private final PythonEmitContext ctx;
    private final PythonTypeEmitter types;
    private PythonStmtEmitter stmts; // set after construction to break circular dep

    public PythonExprEmitter(PythonEmitContext ctx, PythonTypeEmitter types) {
        this.ctx = ctx;
        this.types = types;
    }

    void setStmtEmitter(PythonStmtEmitter stmts) {
        this.stmts = stmts;
    }

    // --- Main dispatch --------------------------------------------------------

    String emitExpr(Ast.Expr expr) {
        if (expr == null) return "None";
        return switch (expr) {
            case Ast.IntLit i -> i.value();
            case Ast.FloatLit f -> f.value();
            case Ast.StringLit s -> "\"" + PythonEmitContext.escapeString(s.value()) + "\"";
            case Ast.StringInterpLit interp -> emitStringInterp(interp);
            case Ast.RawStringLit r -> "r\"" + r.value() + "\"";
            case Ast.BoolLit b -> b.value() ? "True" : "False";
            case Ast.NullLit n -> "None";
            case Ast.Ident id -> emitIdent(id);
            case Ast.ThisExpr t -> "self";
            case Ast.BinaryExpr bin -> emitBinaryExpr(bin);
            case Ast.UnaryExpr un -> emitUnaryExpr(un);
            case Ast.CallExpr call -> emitCallExpr(call);
            case Ast.SelectorExpr sel -> emitSelectorExpr(sel);
            case Ast.SafeNavExpr safe -> emitSafeNavExpr(safe);
            case Ast.IndexExpr idx -> emitExpr(idx.object()) + "[" + emitExpr(idx.index()) + "]";
            case Ast.SliceExpr sl -> emitExpr(sl.object()) + "[" + emitExpr(sl.low()) + ":" + emitExpr(sl.high()) + "]";
            case Ast.ListLit list -> emitListLit(list);
            case Ast.MapLit map -> emitMapLit(map);
            case Ast.LambdaExpr lam -> emitLambdaExpr(lam);
            case Ast.SpawnExpr spawn -> emitSpawnExpr(spawn);
            case Ast.IfExpr ife -> emitIfExpr(ife);
            case Ast.MatchExpr me -> emitMatchExpr(me);
            case Ast.RangeExpr range -> emitRangeExpr(range);
            case Ast.TupleLit tuple -> "(" + tuple.elements().stream().map(this::emitExpr).collect(Collectors.joining(", ")) + (tuple.elements().size() == 1 ? ",)" : ")");
            case Ast.SpreadExpr spread -> "*" + emitExpr(spread.expr());
            case Ast.TypeAssertExpr ta -> emitTypeAssert(ta);
            case Ast.SuperCallExpr sup -> {
                var args = sup.args().stream().map(this::emitExpr).collect(Collectors.joining(", "));
                yield "super().__init__(" + args + ")";
            }
        };
    }

    // --- Identifiers ----------------------------------------------------------

    private String emitIdent(Ast.Ident id) {
        return switch (id.name()) {
            case "print" -> "print";
            case "null" -> "None";
            case "true" -> "True";
            case "false" -> "False";
            default -> {
                // Inside a class method, bare field references → self._field
                if (ctx.insideMethod && ctx.currentClassFields.contains(id.name())) {
                    yield "self._" + id.name();
                }
                // Use renamed name if it was declared as a renamed var/fn
                yield ctx.renamedVars.getOrDefault(id.name(), id.name());
            }
        };
    }

    // --- Binary / Unary -------------------------------------------------------

    private String emitBinaryExpr(Ast.BinaryExpr bin) {
        String left = emitExpr(bin.left());
        String right = emitExpr(bin.right());
        String op = switch (bin.op()) {
            case "&&" -> "and";
            case "||" -> "or";
            case "===" -> "is";
            case "!==" -> "is not";
            case "**" -> "**";
            case "/" -> "//";  // Zinc int division → Python floor division
            case "is" -> {
                yield null; // handled below
            }
            case "is not" -> {
                yield null; // handled below
            }
            case "in" -> "in";
            case "not in" -> "not in";
            default -> bin.op();
        };

        // Special case: type checks
        if (bin.op().equals("is")) {
            return "isinstance(" + left + ", " + types.mapTypeName(right) + ")";
        }
        if (bin.op().equals("is not")) {
            return "not isinstance(" + left + ", " + types.mapTypeName(right) + ")";
        }

        return "(" + left + " " + op + " " + right + ")";
    }

    private String emitUnaryExpr(Ast.UnaryExpr un) {
        String operand = emitExpr(un.operand());
        return switch (un.op()) {
            case "!" -> "not " + operand;
            case "not" -> "not " + operand;
            case "-" -> "(-" + operand + ")";
            default -> un.op() + operand;
        };
    }

    // --- Calls ----------------------------------------------------------------

    String emitCallExpr(Ast.CallExpr call) {
        // Handle new Foo(args) → Foo(args), mapping type names for constructors
        String callee = emitExpr(call.callee());
        if (call.isNew() && call.callee() instanceof Ast.Ident id) {
            callee = types.mapTypeName(id.name());
        }

        // Map known Java methods to Python
        if (call.callee() instanceof Ast.SelectorExpr sel) {
            String obj = emitExpr(sel.object());
            String method = sel.field();
            String mapped = mapMethodCall(obj, method, call.args());
            if (mapped != null) return mapped;

            // Map Java static calls to Python equivalents
            String fullCall = obj + "." + method;
            String staticMapped = mapStaticCall(fullCall, call.args());
            if (staticMapped != null) return staticMapped;
        }

        // Map top-level Java calls
        if (call.callee() instanceof Ast.Ident id) {
            // Inside a method, bare function calls that aren't builtins/globals → self.method()
            if (ctx.insideMethod && ctx.currentClass != null
                    && !id.name().equals("print") && !id.name().equals("len")
                    && !id.name().equals("str") && !id.name().equals("int")
                    && !id.name().equals("float") && !id.name().equals("bool")
                    && !id.name().equals("super") && !id.name().equals("type")
                    && !id.name().equals("isinstance") && !id.name().equals("range")
                    && !id.name().equals("enumerate") && !id.name().equals("abs")
                    && !PythonEmitContext.PYTHON_BUILTINS.contains(id.name())
                    && !id.name().startsWith("_lambda_")) {
                // Check if it looks like a method call (not a top-level function or constructor)
                // Heuristic: if it starts with lowercase and isn't a known function, assume self
                if (Character.isLowerCase(id.name().charAt(0))) {
                    var callArgs = new ArrayList<String>();
                    for (var arg : call.args()) callArgs.add(emitExpr(arg));
                    return "self." + id.name() + "(" + String.join(", ", callArgs) + ")";
                }
            }

            // Handle known static methods without an object
            String mapped = mapStaticCall(id.name(), call.args());
            if (mapped != null) return mapped;
        }

        var args = new ArrayList<String>();
        for (var arg : call.args()) {
            args.add(emitExpr(arg));
        }
        for (var named : call.namedArgs()) {
            args.add(named.name() + "=" + emitExpr(named.value()));
        }

        return callee + "(" + String.join(", ", args) + ")";
    }

    // --- Static call mapping --------------------------------------------------

    /**
     * Map static method calls to Python equivalents via declarative mapping table.
     */
    String mapStaticCall(String fullName, List<Ast.Expr> args) {
        // Split "Class.method" or try as top-level function
        int dot = fullName.indexOf('.');
        if (dot > 0) {
            String className = fullName.substring(0, dot);
            String method = fullName.substring(dot + 1);
            var emittedArgs = args.stream().map(this::emitExpr).toList();
            var resolved = PythonStdlibMapping.resolveStaticCall(className, method, emittedArgs);
            if (resolved != null) {
                if (resolved.importStmt() != null) ctx.addImport(resolved.importStmt());
                return resolved.expr();
            }
        } else {
            // Top-level function (e.g., sleep)
            var emittedArgs = args.stream().map(this::emitExpr).toList();
            var resolved = PythonStdlibMapping.resolveTopLevelCall(fullName, emittedArgs, ctx.runtimeImportPrefix());
            if (resolved != null) {
                if (resolved.importStmt() != null) {
                    // "from .zinc_runtime import X" style
                    if (resolved.importStmt().startsWith("from ")) {
                        ctx.addFromImport(resolved.importStmt());
                    } else {
                        ctx.addImport(resolved.importStmt());
                    }
                }
                return resolved.expr();
            }
        }
        return null;
    }

    // --- Method call mapping --------------------------------------------------

    /**
     * Map Zinc/Java collection methods to Python equivalents.
     * Returns null if no mapping exists (use default call emission).
     */
    String mapMethodCall(String obj, String method, List<Ast.Expr> args) {
        return switch (method) {
            // Collection methods
            case "size" -> "len(" + obj + ")";
            case "isEmpty" -> "(len(" + obj + ") == 0)";
            case "contains" -> "(" + emitExpr(args.getFirst()) + " in " + obj + ")";
            case "add" -> obj + ".append(" + emitExpr(args.getFirst()) + ")";
            case "addAll" -> obj + ".extend(" + emitExpr(args.getFirst()) + ")";
            case "get" -> obj + "[" + emitExpr(args.getFirst()) + "]";
            case "set" -> {
                yield obj + "[" + emitExpr(args.get(0)) + "] = " + emitExpr(args.get(1));
            }
            case "remove" -> obj + ".remove(" + emitExpr(args.getFirst()) + ")";
            case "removeLast" -> obj + ".pop()";

            // Functional collection methods → list comprehensions / builtins
            case "filter" -> {
                var arg = args.getFirst();
                String cond = containsIt(arg) ? emitItExpr(arg, "_x") : applyLambda(emitExpr(arg), "_x");
                yield "[_x for _x in " + obj + " if " + cond + "]";
            }
            case "map" -> {
                var arg = args.getFirst();
                String expr = containsIt(arg) ? emitItExpr(arg, "_x") : applyLambda(emitExpr(arg), "_x");
                yield "[" + expr + " for _x in " + obj + "]";
            }
            case "forEach" -> {
                var arg = args.getFirst();
                if (arg instanceof Ast.LambdaExpr lam && lam.body().stmts().size() == 1) {
                    String paramName = lam.params().getFirst().name();
                    var body = lam.body().stmts().getFirst();
                    if (body instanceof Ast.ExprStmt es) {
                        yield "for " + paramName + " in " + obj + ":\n" + "    ".repeat(ctx.indent + 1) + emitExpr(es.expr());
                    }
                }
                String fn = emitExpr(arg);
                yield "for _x in " + obj + ":\n" + "    ".repeat(ctx.indent + 1) + applyLambda(fn, "_x");
            }
            case "sum" -> "sum(" + obj + ")";
            case "min" -> "min(" + obj + ")";
            case "max" -> "max(" + obj + ")";
            case "count" -> "len(" + obj + ")";
            case "distinct" -> "list(dict.fromkeys(" + obj + "))";
            case "toList" -> "list(" + obj + ")";
            case "toSet" -> "set(" + obj + ")";
            case "sortBy" -> {
                var arg = args.getFirst();
                String key = containsIt(arg) ? emitItExpr(arg, "_x") : applyLambda(emitExpr(arg), "_x");
                yield "sorted(" + obj + ", key=lambda _x: " + key + ")";
            }
            case "reduce" -> {
                ctx.addImport("functools");
                yield "functools.reduce(" + emitExpr(args.get(1)) + ", " + obj + ", " + emitExpr(args.get(0)) + ")";
            }
            case "anyMatch" -> {
                var arg = args.getFirst();
                String cond = containsIt(arg) ? emitItExpr(arg, "_x") : applyLambda(emitExpr(arg), "_x");
                yield "any(" + cond + " for _x in " + obj + ")";
            }
            case "allMatch" -> {
                var arg = args.getFirst();
                String cond = containsIt(arg) ? emitItExpr(arg, "_x") : applyLambda(emitExpr(arg), "_x");
                yield "all(" + cond + " for _x in " + obj + ")";
            }
            case "noneMatch" -> {
                var arg = args.getFirst();
                String cond = containsIt(arg) ? emitItExpr(arg, "_x") : applyLambda(emitExpr(arg), "_x");
                yield "not any(" + cond + " for _x in " + obj + ")";
            }
            case "findFirst" -> {
                if (args.isEmpty()) {
                    yield "next(iter(" + obj + "), None)";
                }
                var arg = args.getFirst();
                String cond = containsIt(arg) ? emitItExpr(arg, "_x") : applyLambda(emitExpr(arg), "_x");
                yield "next((_x for _x in " + obj + " if " + cond + "), None)";
            }
            case "limit" -> obj + "[:" + emitExpr(args.getFirst()) + "]";
            case "skip" -> obj + "[" + emitExpr(args.getFirst()) + ":]";
            case "groupBy" -> {
                var arg = args.getFirst();
                String key = containsIt(arg) ? emitItExpr(arg, "_x") : applyLambda(emitExpr(arg), "_x");
                ctx.addFromImport("from collections import defaultdict");
                yield "(lambda _d: [_d[" + key.replace("_x", "_item") + "].append(_item) for _item in " + obj + "] and dict(_d) or dict(_d))(defaultdict(list))";
            }

            // String methods
            case "toUpperCase", "upper" -> obj + ".upper()";
            case "toLowerCase", "lower" -> obj + ".lower()";
            case "trim" -> obj + ".strip()";
            case "trimStart", "stripLeading" -> obj + ".lstrip()";
            case "trimEnd", "stripTrailing" -> obj + ".rstrip()";
            case "startsWith" -> obj + ".startswith(" + emitExpr(args.getFirst()) + ")";
            case "endsWith" -> obj + ".endswith(" + emitExpr(args.getFirst()) + ")";
            case "length" -> "len(" + obj + ")";
            case "charAt" -> obj + "[" + emitExpr(args.getFirst()) + "]";
            case "indexOf" -> obj + ".find(" + emitExpr(args.getFirst()) + ")";
            case "substring" -> {
                if (args.size() == 1) yield obj + "[" + emitExpr(args.getFirst()) + ":]";
                yield obj + "[" + emitExpr(args.get(0)) + ":" + emitExpr(args.get(1)) + "]";
            }
            case "split" -> obj + ".split(" + emitExpr(args.getFirst()) + ")";
            case "replace" -> obj + ".replace(" + emitExpr(args.get(0)) + ", " + emitExpr(args.get(1)) + ")";
            case "repeat" -> "(" + obj + " * " + emitExpr(args.getFirst()) + ")";
            case "isBlank" -> "(len(" + obj + ".strip()) == 0)";
            case "toString" -> "str(" + obj + ")";
            case "join" -> {
                if (args.isEmpty()) yield obj + ".join()";
                else yield emitExpr(args.getFirst()) + ".join(" + obj + ")";
            }

            // Map methods
            case "put" -> {
                yield obj + "[" + emitExpr(args.get(0)) + "] = " + emitExpr(args.get(1));
            }
            case "containsKey" -> "(" + emitExpr(args.getFirst()) + " in " + obj + ")";
            case "getOrDefault" -> obj + ".get(" + emitExpr(args.get(0)) + ", " + emitExpr(args.get(1)) + ")";
            case "entrySet" -> obj + ".items()";
            case "keySet" -> obj + ".keys()";
            case "values" -> obj + ".values()";

            // Map.Entry methods
            case "getKey" -> obj + "[0]";
            case "getValue" -> obj + "[1]";

            // Future methods
            case "isDone" -> obj + ".isDone()";
            case "isFailed" -> obj + ".isFailed()";

            // Channel methods
            case "send" -> obj + ".send(" + emitExpr(args.getFirst()) + ")";
            case "receive" -> obj + ".receive()";

            // Getter methods → direct attribute access
            default -> null;
        };
    }

    // --- Selectors / navigation -----------------------------------------------

    private String emitSelectorExpr(Ast.SelectorExpr sel) {
        String obj = emitExpr(sel.object());

        if (sel.object() instanceof Ast.ThisExpr) {
            return "self._" + sel.field();
        }
        if (sel.field().equals("length")) {
            return "len(" + obj + ")";
        }
        if (sel.field().equals("class")) {
            return obj;
        }

        // Check declarative static field mapping (Math.PI, Math.E, etc.)
        if (sel.object() instanceof Ast.Ident id) {
            var resolved = PythonStdlibMapping.resolveStaticField(id.name(), sel.field());
            if (resolved != null) {
                if (resolved.importStmt() != null) ctx.addImport(resolved.importStmt());
                return resolved.expr();
            }
        }

        return obj + "." + sel.field();
    }

    private String emitSafeNavExpr(Ast.SafeNavExpr safe) {
        String obj = emitExpr(safe.object());
        if (safe.call() != null) {
            String mapped = mapMethodCall(obj, safe.field(), safe.call().args());
            if (mapped != null) {
                return "(" + mapped + " if " + obj + " is not None else None)";
            }
            var args = safe.call().args().stream().map(this::emitExpr)
                .collect(Collectors.joining(", "));
            return "(" + obj + "." + safe.field() + "(" + args + ") if " + obj + " is not None else None)";
        }
        if (safe.field().equals("length")) {
            return "(len(" + obj + ") if " + obj + " is not None else None)";
        }
        return "(" + obj + "." + safe.field() + " if " + obj + " is not None else None)";
    }

    // --- Literals -------------------------------------------------------------

    private String emitListLit(Ast.ListLit list) {
        var elements = list.elements().stream()
            .map(this::emitExpr)
            .collect(Collectors.joining(", "));
        return "[" + elements + "]";
    }

    private String emitMapLit(Ast.MapLit map) {
        if (map.keys().isEmpty()) return "{}";
        var entries = new ArrayList<String>();
        for (int i = 0; i < map.keys().size(); i++) {
            entries.add(emitExpr(map.keys().get(i)) + ": " + emitExpr(map.values().get(i)));
        }
        return "{" + String.join(", ", entries) + "}";
    }

    private String emitStringInterp(Ast.StringInterpLit interp) {
        var sb = new StringBuilder("f\"");
        for (var part : interp.parts()) {
            if (part instanceof Ast.StringLit s) {
                sb.append(PythonEmitContext.escapeString(s.value()));
            } else {
                sb.append("{").append(emitExpr(part)).append("}");
            }
        }
        sb.append("\"");
        return sb.toString();
    }

    // --- Lambda / Spawn -------------------------------------------------------

    String emitLambdaExpr(Ast.LambdaExpr lam) {
        var params = lam.params().stream()
            .map(Ast.ParamDecl::name)
            .collect(Collectors.joining(", "));

        // Single-expression lambda
        if (lam.body().stmts().size() == 1) {
            var stmt = lam.body().stmts().getFirst();
            if (stmt instanceof Ast.ReturnStmt ret) {
                return "lambda " + params + ": " + emitExpr(ret.value());
            }
            if (stmt instanceof Ast.ExprStmt expr) {
                return "lambda " + params + ": " + emitExpr(expr.expr());
            }
        }

        // Multi-statement lambda → hoist as named function
        String fnName = "_lambda_" + (ctx.lambdaCounter++);
        ctx.line("def " + fnName + "(" + params + "):");
        ctx.indent++;
        stmts.emitBlock(lam.body());
        ctx.indent--;

        return fnName;
    }

    String emitSpawnExpr(Ast.SpawnExpr spawn) {
        ctx.addFromImport("from " + ctx.runtimeImportPrefix() + "zinc_runtime import ZincFuture");

        var globals = new java.util.LinkedHashSet<String>();
        globals.addAll(collectAssignedVars(spawn.body()));
        if (spawn.orHandler() != null && spawn.orHandler().body() != null) {
            globals.addAll(collectAssignedVars(spawn.orHandler().body()));
        }

        String fnName = "_spawn_" + (ctx.spawnCounter++);
        ctx.line("def " + fnName + "():");
        ctx.indent++;
        if (!globals.isEmpty()) {
            ctx.line("nonlocal " + String.join(", ", globals));
        }
        stmts.emitBlock(spawn.body());
        ctx.indent--;

        if (spawn.orHandler() != null && spawn.orHandler().body() != null) {
            String orName = fnName + "_or";
            ctx.line("def " + orName + "():");
            ctx.indent++;
            if (!globals.isEmpty()) {
                ctx.line("nonlocal " + String.join(", ", globals));
            }
            stmts.emitBlock(spawn.orHandler().body());
            ctx.indent--;
            return "ZincFuture(" + fnName + ", " + orName + ")";
        }

        return "ZincFuture(" + fnName + ")";
    }

    // --- Conditional / match expressions --------------------------------------

    private String emitIfExpr(Ast.IfExpr ife) {
        return "(" + emitExpr(ife.then()) + " if " + emitExpr(ife.cond()) + " else " + emitExpr(ife.elseExpr()) + ")";
    }

    private String emitMatchExpr(Ast.MatchExpr me) {
        if (me.cases().isEmpty()) return "None";

        var result = new StringBuilder();
        for (int i = 0; i < me.cases().size(); i++) {
            var case_ = me.cases().get(i);
            String value = emitExpr(case_.value());
            String pattern = emitExpr(case_.pattern());

            if (pattern.equals("_") || i == me.cases().size() - 1) {
                result.append(value);
                break;
            }
            result.append("(").append(value).append(" if ")
                .append(emitExpr(me.subject())).append(" == ").append(pattern)
                .append(" else ");
        }

        long openParens = result.chars().filter(c -> c == '(').count();
        long closeParens = result.chars().filter(c -> c == ')').count();
        for (long i = closeParens; i < openParens; i++) {
            result.append(")");
        }

        return result.toString();
    }

    private String emitRangeExpr(Ast.RangeExpr range) {
        String start = emitExpr(range.start());
        String end = emitExpr(range.end());
        if (range.inclusive()) {
            return "range(" + start + ", " + end + " + 1)";
        }
        return "range(" + start + ", " + end + ")";
    }

    private String emitTypeAssert(Ast.TypeAssertExpr ta) {
        String obj = emitExpr(ta.object());
        String type = types.mapTypeName(ta.typeName());
        if (ta.isCheck()) {
            return "isinstance(" + obj + ", " + type + ")";
        }
        return obj;
    }

    // --- Lambda helpers -------------------------------------------------------

    /**
     * Apply a lambda to a variable, inlining when possible.
     */
    private String applyLambda(String lambdaStr, String varName) {
        if (lambdaStr.startsWith("lambda ")) {
            int colonIdx = lambdaStr.indexOf(": ");
            if (colonIdx > 0) {
                String paramPart = lambdaStr.substring(7, colonIdx).strip();
                String body = lambdaStr.substring(colonIdx + 2);
                if (!paramPart.contains(",")) {
                    String param = paramPart.strip();
                    return body.replaceAll("\\b" + java.util.regex.Pattern.quote(param) + "\\b", varName);
                }
            }
        }
        return lambdaStr + "(" + varName + ")";
    }

    // --- `it` implicit parameter ----------------------------------------------

    /** Check if an expression uses the `it` implicit parameter. */
    boolean containsIt(Ast.Expr expr) {
        return switch (expr) {
            case Ast.Ident id -> id.name().equals("it");
            case Ast.BinaryExpr bin -> containsIt(bin.left()) || containsIt(bin.right());
            case Ast.UnaryExpr un -> containsIt(un.operand());
            case Ast.CallExpr call -> {
                boolean inCallee = containsIt(call.callee());
                boolean inArgs = call.args().stream().anyMatch(this::containsIt);
                yield inCallee || inArgs;
            }
            case Ast.SelectorExpr sel -> containsIt(sel.object());
            case Ast.IndexExpr idx -> containsIt(idx.object()) || containsIt(idx.index());
            case Ast.IfExpr ife -> containsIt(ife.cond()) || containsIt(ife.then()) || containsIt(ife.elseExpr());
            default -> false;
        };
    }

    /** Emit an expression with `it` replaced by varName. */
    String emitItExpr(Ast.Expr expr, String varName) {
        return switch (expr) {
            case Ast.Ident id -> id.name().equals("it") ? varName : emitExpr(id);
            case Ast.BinaryExpr bin ->
                "(" + emitItExpr(bin.left(), varName) + " " + PythonEmitContext.mapOp(bin.op()) + " " + emitItExpr(bin.right(), varName) + ")";
            case Ast.UnaryExpr un -> {
                String op = un.op().equals("!") || un.op().equals("not") ? "not " : un.op();
                yield op + emitItExpr(un.operand(), varName);
            }
            case Ast.SelectorExpr sel -> {
                String obj = emitItExpr(sel.object(), varName);
                if (sel.field().equals("length")) yield "len(" + obj + ")";
                else yield obj + "." + sel.field();
            }
            case Ast.CallExpr call -> {
                if (call.callee() instanceof Ast.SelectorExpr sel && containsIt(sel.object())) {
                    String obj = emitItExpr(sel.object(), varName);
                    var callArgs = call.args().stream().map(this::emitExpr)
                        .collect(Collectors.joining(", "));
                    String mapped = mapMethodCall(obj, sel.field(), call.args());
                    if (mapped != null) yield mapped.replace(obj, emitItExpr(sel.object(), varName));
                    yield obj + "." + sel.field() + "(" + callArgs + ")";
                }
                yield emitExpr(call);
            }
            default -> emitExpr(expr);
        };
    }

    // --- Helpers --------------------------------------------------------------

    Set<String> collectAssignedVars(Ast.Stmt stmt) {
        var vars = new java.util.LinkedHashSet<String>();
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
            default -> {}
        }
        return vars;
    }
}
