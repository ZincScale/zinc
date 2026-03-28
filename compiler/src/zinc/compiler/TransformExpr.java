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
 * Transforms Zinc AST expressions into JavaParser AST expressions.
 */
public class TransformExpr {

    private final TransformContext ctx;
    private TransformStmt stmts; // set after construction to break circular dep

    public TransformExpr(TransformContext ctx) {
        this.ctx = ctx;
    }

    void setStmtTransformer(TransformStmt stmts) {
        this.stmts = stmts;
    }

    // --- Main dispatch --------------------------------------------------------

    Expression transformExpr(Expr expr) {
        return switch (expr) {
            case IntLit i -> new IntegerLiteralExpr(i.value());
            case FloatLit f -> new DoubleLiteralExpr(f.value());
            case StringLit s -> new StringLiteralExpr(s.value());
            case BoolLit b -> new BooleanLiteralExpr(b.value());
            case NullLit n -> new NullLiteralExpr();
            case Ident id -> {
                if (id.name().equals("print")) yield new NameExpr("System.out.println");
                if (ctx.capturedMutables.contains(id.name())) {
                    yield new ArrayAccessExpr(new NameExpr("_" + id.name()), new IntegerLiteralExpr("0"));
                }
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
                var listOf = new MethodCallExpr(new NameExpr("List"), "of", listArgs);
                yield new ObjectCreationExpr(null,
                    new ClassOrInterfaceType(null, "ArrayList<>"), new NodeList<>(listOf));
            }
            case StringInterpLit interp -> transformInterpString(interp);
            case MapLit map -> {
                if (map.keys().isEmpty()) {
                    yield new ObjectCreationExpr(null, new ClassOrInterfaceType(null, "LinkedHashMap<>"), new NodeList<>());
                }
                var sb = new StringBuilder("new java.util.LinkedHashMap<>()");
                sb.append(" {{ ");
                for (int i = 0; i < map.keys().size(); i++) {
                    sb.append("put(").append(exprToJava(map.keys().get(i)))
                        .append(", ").append(exprToJava(map.values().get(i))).append("); ");
                }
                sb.append("}}");
                yield ctx.parseExpr(sb.toString());
            }
            case RawStringLit raw -> new StringLiteralExpr(raw.value());
            case SafeNavExpr nav -> {
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
                    yield new InstanceOfExpr(transformExpr(ta.object()), new ClassOrInterfaceType(null, ta.typeName()));
                } else {
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
                if (!tuple.elements().isEmpty()) yield transformExpr(tuple.elements().getFirst());
                yield new NullLiteralExpr();
            }
            default -> new NameExpr("/* unsupported: " + expr.getClass().getSimpleName() + " */");
        };
    }

    // --- Binary / Unary -------------------------------------------------------

    private Expression transformBinaryExpr(Ast.BinaryExpr bin) {
        var left = transformExpr(bin.left());
        var right = transformExpr(bin.right());

        if (bin.op().equals("==")) {
            if (ctx.isPrimitiveLiteral(bin.left()) && ctx.isPrimitiveLiteral(bin.right())) {
                return new com.github.javaparser.ast.expr.BinaryExpr(left, right,
                    com.github.javaparser.ast.expr.BinaryExpr.Operator.EQUALS);
            }
            return new MethodCallExpr(new NameExpr("java.util.Objects"), "equals",
                new NodeList<>(left, right));
        }
        if (bin.op().equals("!=")) {
            if (ctx.isPrimitiveLiteral(bin.left()) && ctx.isPrimitiveLiteral(bin.right())) {
                return new com.github.javaparser.ast.expr.BinaryExpr(left, right,
                    com.github.javaparser.ast.expr.BinaryExpr.Operator.NOT_EQUALS);
            }
            return new com.github.javaparser.ast.expr.UnaryExpr(
                new MethodCallExpr(new NameExpr("java.util.Objects"), "equals",
                    new NodeList<>(left, right)),
                com.github.javaparser.ast.expr.UnaryExpr.Operator.LOGICAL_COMPLEMENT);
        }
        if (bin.op().equals("===")) {
            return new com.github.javaparser.ast.expr.BinaryExpr(left, right,
                com.github.javaparser.ast.expr.BinaryExpr.Operator.EQUALS);
        }
        if (bin.op().equals("!==")) {
            return new com.github.javaparser.ast.expr.BinaryExpr(left, right,
                com.github.javaparser.ast.expr.BinaryExpr.Operator.NOT_EQUALS);
        }
        if (bin.op().equals("**")) {
            var pow = new MethodCallExpr(new NameExpr("Math"), "pow", new NodeList<>(left, right));
            if (ctx.isPrimitiveLiteral(bin.left()) && ctx.isPrimitiveLiteral(bin.right())
                && bin.left() instanceof IntLit && bin.right() instanceof IntLit) {
                return new CastExpr(PrimitiveType.longType(), pow);
            }
            return pow;
        }

        var op = switch (bin.op()) {
            case "+" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.PLUS;
            case "-" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.MINUS;
            case "*" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.MULTIPLY;
            case "/" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.DIVIDE;
            case "%" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.REMAINDER;
            case "<" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.LESS;
            case "<=" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.LESS_EQUALS;
            case ">" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.GREATER;
            case ">=" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.GREATER_EQUALS;
            case "&&" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.AND;
            case "||" -> com.github.javaparser.ast.expr.BinaryExpr.Operator.OR;
            default -> com.github.javaparser.ast.expr.BinaryExpr.Operator.PLUS;
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

    // --- Calls ----------------------------------------------------------------

    Expression transformCallExpr(CallExpr call) {
        var args = new NodeList<Expression>();
        for (var arg : call.args()) args.add(transformExpr(arg));

        if (call.isNew()) {
            String typeName = ((Ident) call.callee()).name();
            typeName = TransformContext.TYPE_MAP.getOrDefault(typeName, typeName);
            var type = new ClassOrInterfaceType(null, typeName);
            if (!call.typeArgs().isEmpty()) {
                var typeArgs = new NodeList<com.github.javaparser.ast.type.Type>();
                for (var ta : call.typeArgs()) typeArgs.add(new ClassOrInterfaceType(null, ta));
                type.setTypeArguments(typeArgs);
            }
            return new ObjectCreationExpr(null, type, args);
        }

        if (call.callee() instanceof SelectorExpr sel) {
            String methodName = TransformContext.METHOD_ALIASES.getOrDefault(sel.field(), sel.field());

            if (isStreamMethod(methodName)) {
                return transformStreamChain(call);
            }

            if (methodName.equals("join") && !args.isEmpty()) {
                return new MethodCallExpr(new NameExpr("String"), "join",
                    new NodeList<>(args.get(0), transformExpr(sel.object())));
            }

            var result = new MethodCallExpr(transformExpr(sel.object()), methodName, args);
            if (ctx.javaResolver.returnsOptional(ctx.getTypeName(sel.object()), methodName)) {
                return new MethodCallExpr(result, "orElse", new NodeList<>(new NullLiteralExpr()));
            }
            return result;
        }

        if (call.callee() instanceof Ident id) {
            if (id.name().equals("print")) {
                return new MethodCallExpr(new NameExpr("System.out"), "println", args);
            }
            if (id.name().equals("len") && !args.isEmpty()) {
                return new MethodCallExpr(args.get(0), "size");
            }
            if (id.name().equals("sleep")) {
                return new MethodCallExpr(new NameExpr("Thread"), "sleep", args);
            }
            if (id.name().equals("parseInt")) {
                return new MethodCallExpr(new NameExpr("Integer"), "parseInt", args);
            }
            return new MethodCallExpr(null, id.name(), args);
        }

        return new MethodCallExpr(null, "unknown", args);
    }

    // --- String interpolation -------------------------------------------------

    private Expression transformInterpString(StringInterpLit interp) {
        Expression result = null;
        for (var part : interp.parts()) {
            Expression expr;
            if (part instanceof StringLit s) {
                expr = new StringLiteralExpr(s.value());
            } else {
                expr = transformExpr(part);
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

    // --- Lambda / Spawn / Range -----------------------------------------------

    private Expression transformLambda(Ast.LambdaExpr lam) {
        var params = new NodeList<Parameter>();
        for (var p : lam.params()) {
            if (p.type() != null) {
                params.add(new Parameter(ctx.transformType(p.type()), p.name()));
            } else {
                params.add(new Parameter(new UnknownType(), p.name()));
            }
        }
        var body = stmts.transformBlock(lam.body());
        if (body.getStatements().size() == 1
            && body.getStatement(0) instanceof ReturnStmt ret
            && ret.getExpression().isPresent()) {
            return new com.github.javaparser.ast.expr.LambdaExpr(params, ret.getExpression().get());
        }
        return new com.github.javaparser.ast.expr.LambdaExpr(params, body);
    }

    Expression transformSpawn(Ast.SpawnExpr spawn) {
        var capturedMutables = new java.util.HashSet<String>();
        capturedMutables.addAll(stmts.collectAssignedVars(spawn.body()));
        if (spawn.orHandler() != null && spawn.orHandler().body() != null) {
            capturedMutables.addAll(stmts.collectAssignedVars(spawn.orHandler().body()));
        }
        var prevCaptured = new java.util.HashSet<>(ctx.capturedMutables);
        ctx.capturedMutables.addAll(capturedMutables);

        var body = stmts.transformBlock(spawn.body());

        String orHandler = "";
        if (spawn.orHandler() != null && spawn.orHandler().body() != null) {
            var handlerBlock = stmts.transformBlock(spawn.orHandler().body());
            orHandler = handlerBlock.toString().replace("{", "").replace("}", "").trim();
        }

        var tryBody = new BlockStmt();
        for (var stmt : body.getStatements()) tryBody.addStatement(stmt.clone());
        var lastStmt = body.getStatements().isEmpty() ? null
            : body.getStatements().get(body.getStatements().size() - 1);
        boolean endsWithThrowOrReturn = lastStmt instanceof com.github.javaparser.ast.stmt.ThrowStmt
            || lastStmt instanceof ReturnStmt;
        if (!endsWithThrowOrReturn) {
            tryBody.addStatement(new ExpressionStmt(
                new MethodCallExpr(new NameExpr("_f"), "complete", new NodeList<>(new NullLiteralExpr()))));
        }

        var catchBody = new BlockStmt();
        if (!orHandler.isEmpty()) {
            if (spawn.orHandler().body() != null) {
                for (var stmt : spawn.orHandler().body().stmts()) {
                    for (var jStmt : stmts.transformStmt(stmt)) catchBody.addStatement(jStmt);
                }
            }
        }
        catchBody.addStatement(new ExpressionStmt(
            new MethodCallExpr(new NameExpr("_f"), "completeExceptionally", new NodeList<>(new NameExpr("err")))));

        var catchClause = new CatchClause(
            new Parameter(new ClassOrInterfaceType(null, "Exception"), "err"), catchBody);
        var tryCatch = new TryStmt(tryBody, new NodeList<>(catchClause), null);

        var threadBody = new BlockStmt();
        threadBody.addStatement(tryCatch);
        var threadLambda = new com.github.javaparser.ast.expr.LambdaExpr(new NodeList<>(), threadBody);

        var outerBody = new BlockStmt();
        var futureDecl = new VariableDeclarationExpr(new VarType(), "_f");
        futureDecl.getVariable(0).setInitializer(new ObjectCreationExpr(null,
            new ClassOrInterfaceType(null, "java.util.concurrent.CompletableFuture<Void>"), new NodeList<>()));
        outerBody.addStatement(new ExpressionStmt(futureDecl));
        outerBody.addStatement(new ExpressionStmt(
            new MethodCallExpr(
                new MethodCallExpr(new NameExpr("Thread"), "ofVirtual"),
                "start", new NodeList<>(threadLambda))));
        outerBody.addStatement(new ReturnStmt(new NameExpr("_f")));

        var supplierLambda = new com.github.javaparser.ast.expr.LambdaExpr(new NodeList<>(), outerBody);
        var cast = new CastExpr(
            new ClassOrInterfaceType(null, "java.util.function.Supplier")
                .setTypeArguments(new NodeList<>(new ClassOrInterfaceType(null, "java.util.concurrent.CompletableFuture<Void>"))),
            new EnclosedExpr(supplierLambda));

        ctx.capturedMutables.clear();
        ctx.capturedMutables.addAll(prevCaptured);

        return new MethodCallExpr(new EnclosedExpr(cast), "get");
    }

    private Expression transformRange(RangeExpr range) {
        String method = range.inclusive() ? "rangeClosed" : "range";
        return new MethodCallExpr(
            new NameExpr("java.util.stream.IntStream"), method,
            new NodeList<>(transformExpr(range.start()), transformExpr(range.end())));
    }

    /** Match expression → nested ternary. */
    Expression transformMatchExpr(MatchExpr match) {
        var subject = transformExpr(match.subject());
        Expression result = new NullLiteralExpr();
        for (int i = match.cases().size() - 1; i >= 0; i--) {
            var c = match.cases().get(i);
            if (c.pattern() == null) {
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

    // --- Expression in type context -------------------------------------------

    /**
     * Transform expression with type context — handles array literal assignment to array type.
     */
    Expression transformExprInContext(Expr expr, Ast.TypeExpr targetType) {
        if (targetType instanceof Ast.ArrayType arrType && expr instanceof ListLit list) {
            var elemType = ctx.transformType(arrType.elementType());
            var elems = new NodeList<Expression>();
            for (var el : list.elements()) elems.add(transformExpr(el));
            return new ArrayCreationExpr(elemType, new NodeList<>(new ArrayCreationLevel()),
                new ArrayInitializerExpr(elems));
        }
        return transformExpr(expr);
    }

    /** Quick expression to Java source string for inline use. */
    String exprToJava(Expr expr) {
        return transformExpr(expr).toString();
    }

    // --- Stream chain ---------------------------------------------------------

    /**
     * Transform a stream chain: collect all chained stream ops, emit as single stream pipeline.
     */
    private Expression transformStreamChain(CallExpr call) {
        var ops = new java.util.ArrayList<CallExpr>();
        Expr root = call;
        while (root instanceof CallExpr c && c.callee() instanceof SelectorExpr sel
               && isStreamMethod(TransformContext.METHOD_ALIASES.getOrDefault(sel.field(), sel.field()))) {
            ops.addFirst(c);
            root = sel.object();
        }

        Expression stream = new MethodCallExpr(transformExpr(root), "stream");

        for (var op : ops) {
            var sel = (SelectorExpr) op.callee();
            String methodName = TransformContext.METHOD_ALIASES.getOrDefault(sel.field(), sel.field());

            var streamArgs = new NodeList<Expression>();
            for (var arg : op.args()) {
                if (containsIt(arg)) {
                    streamArgs.add(new com.github.javaparser.ast.expr.LambdaExpr(
                        new NodeList<>(new Parameter(new UnknownType(), "_it")), rewriteIt(arg)));
                } else {
                    streamArgs.add(transformExpr(arg));
                }
            }

            switch (methodName) {
                case "sum" -> {
                    stream = new MethodCallExpr(stream, "mapToInt", new NodeList<>(ctx.parseExpr("x -> (int) x")));
                    stream = new MethodCallExpr(stream, "sum");
                }
                case "sortBy" -> {
                    var originalArg = op.args().isEmpty() ? null : op.args().getFirst();
                    if (originalArg instanceof Ident id && id.name().equals("it")) {
                        stream = new MethodCallExpr(stream, "sorted");
                    } else {
                        var comparator = new MethodCallExpr(new NameExpr("Comparator"), "comparing", streamArgs);
                        stream = new MethodCallExpr(stream, "sorted", new NodeList<>(comparator));
                    }
                }
                case "groupBy" -> {
                    var collector = new MethodCallExpr(new NameExpr("java.util.stream.Collectors"), "groupingBy", streamArgs);
                    stream = new MethodCallExpr(stream, "collect", new NodeList<>(collector));
                }
                case "findFirst" -> {
                    if (!streamArgs.isEmpty()) {
                        stream = new MethodCallExpr(stream, "filter", streamArgs);
                    }
                    stream = new MethodCallExpr(stream, "findFirst");
                    stream = new MethodCallExpr(stream, "orElse", new NodeList<>(new NullLiteralExpr()));
                }
                default -> stream = new MethodCallExpr(stream, methodName, streamArgs);
            }
        }

        var lastOp = ops.getLast();
        var lastSel = (SelectorExpr) lastOp.callee();
        String lastName = TransformContext.METHOD_ALIASES.getOrDefault(lastSel.field(), lastSel.field());
        boolean isTerminal = switch (lastName) {
            case "reduce", "forEach", "anyMatch", "allMatch", "noneMatch",
                 "count", "findFirst", "sum", "min", "max", "average",
                 "groupBy" -> true;
            default -> false;
        };

        return isTerminal ? stream : new MethodCallExpr(stream, "toList");
    }

    // --- `it` implicit parameter ----------------------------------------------

    /** Check if an expression contains the `it` implicit parameter. */
    boolean containsIt(Expr expr) {
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
    Expression rewriteIt(Expr expr) {
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

    // --- Helpers --------------------------------------------------------------

    static boolean isStreamMethod(String name) {
        return switch (name) {
            case "filter", "map", "flatMap", "reduce", "forEach", "sorted", "sortBy",
                 "distinct", "limit", "skip", "anyMatch", "allMatch", "noneMatch",
                 "findFirst", "count", "toList", "sum", "min", "max", "average",
                 "groupBy" -> true;
            default -> false;
        };
    }
}
