// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

import static zinc.compiler.Ast.*;

/**
 * Type checker and type inference for Zinc AST.
 * Walks the AST, resolves types, and produces a ResolvedTypes map
 * that the Transformer can query for type information.
 */
public class TypeChecker {

    private Scope scope = new Scope();
    private final JavaTypeResolver javaResolver = new JavaTypeResolver();
    private final List<String> errors = new ArrayList<>();

    // Resolved types: keyed by "line:varname" for var declarations
    private final Map<String, TypeInfo> resolvedTypes = new HashMap<>();

    // Function signatures: name → return type
    private final Map<String, TypeInfo> fnSigs = new HashMap<>();

    // Class method signatures: className → methodName → return type
    private final Map<String, Map<String, TypeInfo>> methodSigs = new HashMap<>();

    // Interface tracking
    private final Map<String, Boolean> interfaces = new HashMap<>();

    public List<String> errors() { return errors; }
    public Map<String, TypeInfo> resolvedTypes() { return resolvedTypes; }

    /**
     * Typecheck a program and return resolved types.
     */
    public Result<Map<String, TypeInfo>> check(Program program) {
        // First pass: collect function and class signatures
        collectSignatures(program);

        // Second pass: check and infer types
        for (var decl : program.decls()) {
            checkDecl(decl);
        }
        for (var stmt : program.stmts()) {
            checkStmt(stmt);
        }

        if (!errors.isEmpty()) return Result.err(errors);
        return Result.ok(resolvedTypes);
    }

    // --- Signature collection ------------------------------------------------

    private void collectSignatures(Program program) {
        for (var decl : program.decls()) {
            switch (decl) {
                case FnDecl fn -> fnSigs.put(fn.name(), resolveTypeExpr(fn.returnType()));
                case ClassDecl cls -> {
                    var methods = new HashMap<String, TypeInfo>();
                    for (var m : cls.methods()) {
                        methods.put(m.name(), resolveTypeExpr(m.returnType()));
                    }
                    methodSigs.put(cls.name(), methods);
                }
                case InterfaceDecl iface -> {
                    interfaces.put(iface.name(), true);
                    var methods = new HashMap<String, TypeInfo>();
                    for (var m : iface.methods()) {
                        methods.put(m.name(), resolveTypeExpr(m.returnType()));
                    }
                    methodSigs.put(iface.name(), methods);
                }
                case DataClassDecl data -> {
                    var methods = new HashMap<String, TypeInfo>();
                    // Data class accessors: field name → field type
                    for (var p : data.params()) {
                        methods.put(p.name(), resolveTypeExpr(p.type()));
                    }
                    for (var m : data.methods()) {
                        methods.put(m.name(), resolveTypeExpr(m.returnType()));
                    }
                    methodSigs.put(data.name(), methods);
                }
                default -> {}
            }
        }
    }

    // --- Declaration checking ------------------------------------------------

    private void checkDecl(TopLevelDecl decl) {
        switch (decl) {
            case FnDecl fn -> checkFnDecl(fn);
            case ClassDecl cls -> checkClassDecl(cls);
            default -> {}
        }
    }

    private void checkFnDecl(FnDecl fn) {
        var inner = scope.child();
        for (var p : fn.params()) {
            inner.set(p.name(), resolveTypeExpr(p.type()));
        }
        var prev = scope;
        scope = inner;
        if (fn.body() != null) checkBlock(fn.body());
        scope = prev;
    }

    private void checkClassDecl(ClassDecl cls) {
        var inner = scope.child();
        for (var f : cls.fields()) {
            inner.set(f.name(), resolveTypeExpr(f.type()));
        }
        var prev = scope;
        scope = inner;
        for (var m : cls.methods()) {
            checkMethodDecl(m, cls.fields());
        }
        scope = prev;
    }

    private void checkMethodDecl(MethodDecl m, List<FieldDecl> fields) {
        var inner = scope.child();
        for (var f : fields) inner.set(f.name(), resolveTypeExpr(f.type()));
        for (var p : m.params()) inner.set(p.name(), resolveTypeExpr(p.type()));
        var prev = scope;
        scope = inner;
        if (m.body() != null) checkBlock(m.body());
        scope = prev;
    }

    // --- Statement checking --------------------------------------------------

    private void checkBlock(BlockStmt block) {
        for (var s : block.stmts()) checkStmt(s);
    }

    private void checkStmt(Stmt stmt) {
        switch (stmt) {
            case VarStmt v -> checkVarStmt(v);
            case AssignStmt a -> inferType(a.value());
            case ReturnStmt r -> { if (r.value() != null) inferType(r.value()); }
            case IfStmt i -> {
                inferType(i.cond());
                checkBlock(i.then());
                if (i.elseStmt() instanceof BlockStmt b) checkBlock(b);
                else if (i.elseStmt() instanceof IfStmt ei) checkStmt(ei);
            }
            case ForStmt f -> {
                if (f.isRange()) {
                    var rangeType = inferType(f.range());
                    // Infer item type from range element type
                    TypeInfo itemType = TypeInfo.ANY;
                    if (!rangeType.args().isEmpty()) {
                        itemType = rangeType.args().getFirst();
                    } else if (rangeType.name().endsWith("[]")) {
                        itemType = new TypeInfo(rangeType.name().replace("[]", ""));
                    }
                    scope.set(f.item(), itemType);
                }
                checkBlock(f.body());
            }
            case WhileStmt w -> { inferType(w.cond()); checkBlock(w.body()); }
            case ExprStmt e -> {
                inferType(e.expr());
                if (e.orHandler() != null && e.orHandler().body() != null) checkBlock(e.orHandler().body());
            }
            case BlockStmt b -> checkBlock(b);
            default -> {}
        }
    }

    private void checkVarStmt(VarStmt v) {
        TypeInfo declaredType = v.type() != null ? resolveTypeExpr(v.type()) : null;
        TypeInfo valType = v.value() != null ? inferType(v.value()) : null;

        TypeInfo resolvedType;
        if (declaredType != null) {
            resolvedType = declaredType;
        } else if (valType != null && !valType.name().equals("any")) {
            resolvedType = valType;
        } else {
            resolvedType = TypeInfo.ANY;
        }

        scope.set(v.name(), resolvedType);

        // Store resolved type for or-handler variables (transformer needs this)
        if (v.orHandler() != null && v.type() == null && valType != null) {
            resolvedTypes.put(v.line() + ":" + v.name(), valType);
        }

        // Check or-handler body
        if (v.orHandler() != null && v.orHandler().body() != null) {
            checkBlock(v.orHandler().body());
        }
    }

    // --- Type inference -------------------------------------------------------

    TypeInfo inferType(Expr expr) {
        if (expr == null) return TypeInfo.NULL;
        return switch (expr) {
            case IntLit i -> TypeInfo.INT;
            case FloatLit f -> TypeInfo.DOUBLE;
            case StringLit s -> TypeInfo.STRING;
            case StringInterpLit s -> TypeInfo.STRING;
            case RawStringLit s -> TypeInfo.STRING;
            case BoolLit b -> TypeInfo.BOOLEAN;
            case NullLit n -> TypeInfo.NULL;
            case Ident id -> scope.lookup(id.name()).orElse(TypeInfo.ANY);
            case ThisExpr t -> TypeInfo.ANY; // would need class context
            case BinaryExpr bin -> inferBinaryType(bin);
            case UnaryExpr un -> inferType(un.operand());
            case CallExpr call -> inferCallType(call);
            case SelectorExpr sel -> inferSelectorType(sel);
            case IndexExpr idx -> TypeInfo.ANY;
            case ListLit list -> new TypeInfo("List");
            case MapLit map -> new TypeInfo("Map");
            case LambdaExpr lam -> TypeInfo.ANY;
            case SpawnExpr spawn -> {
                if (spawn.body() != null) checkBlock(spawn.body());
                yield new TypeInfo("CompletableFuture", List.of(TypeInfo.VOID));
            }
            case RangeExpr range -> TypeInfo.INT;
            default -> TypeInfo.ANY;
        };
    }

    private TypeInfo inferBinaryType(BinaryExpr bin) {
        var left = inferType(bin.left());
        var right = inferType(bin.right());
        return switch (bin.op()) {
            case "==", "!=", "<", "<=", ">", ">=", "&&", "||" -> TypeInfo.BOOLEAN;
            case "+", "-", "*", "/", "%" -> {
                if (left.name().equals("double") || right.name().equals("double")) yield TypeInfo.DOUBLE;
                if (left.name().equals("String") || right.name().equals("String")) yield TypeInfo.STRING;
                yield TypeInfo.INT;
            }
            case "**" -> TypeInfo.DOUBLE;
            default -> left;
        };
    }

    private TypeInfo inferCallType(CallExpr call) {
        // new Type(...) — returns the type
        if (call.isNew() && call.callee() instanceof Ident id) {
            if (!call.typeArgs().isEmpty()) {
                var args = call.typeArgs().stream().map(TypeInfo::new).toList();
                return new TypeInfo(id.name(), args);
            }
            return new TypeInfo(id.name());
        }

        // Method call on object: obj.method(args)
        if (call.callee() instanceof SelectorExpr sel) {
            var objType = inferType(sel.object());
            // If object is an identifier starting with uppercase, treat as Java class
            if (objType.name().equals("any") && sel.object() instanceof Ident objId
                && !objId.name().isEmpty() && Character.isUpperCase(objId.name().charAt(0))) {
                objType = new TypeInfo(objId.name());
            }
            if (!objType.name().equals("any")) {
                // Check Zinc method signatures
                var methods = methodSigs.get(objType.name());
                if (methods != null && methods.containsKey(sel.field())) {
                    return methods.get(sel.field());
                }
                // Check Java reflection
                var resolved = javaResolver.resolveMethodReturn(objType.name(), objType.args(), sel.field());
                if (resolved.isPresent()) return resolved.get();
            }
        }

        // Simple function call: func(args)
        if (call.callee() instanceof Ident id) {
            // Check Zinc functions
            var retType = fnSigs.get(id.name());
            if (retType != null) return retType;
        }

        return TypeInfo.ANY;
    }

    private TypeInfo inferSelectorType(SelectorExpr sel) {
        var objType = inferType(sel.object());
        if (!objType.name().equals("any")) {
            var methods = methodSigs.get(objType.name());
            if (methods != null && methods.containsKey(sel.field())) {
                return methods.get(sel.field());
            }
            var resolved = javaResolver.resolveMethodReturn(objType.name(), objType.args(), sel.field());
            if (resolved.isPresent()) return resolved.get();
        }
        return TypeInfo.ANY;
    }

    // --- Type expression resolution ------------------------------------------

    TypeInfo resolveTypeExpr(Ast.TypeExpr type) {
        if (type == null) return TypeInfo.VOID;
        return switch (type) {
            case SimpleType s -> switch (s.name()) {
                case "int" -> TypeInfo.INT;
                case "long" -> TypeInfo.LONG;
                case "double" -> TypeInfo.DOUBLE;
                case "float" -> TypeInfo.FLOAT;
                case "boolean" -> TypeInfo.BOOLEAN;
                case "byte" -> TypeInfo.BYTE;
                case "char" -> TypeInfo.CHAR;
                case "short" -> TypeInfo.SHORT;
                case "String" -> TypeInfo.STRING;
                case "void" -> TypeInfo.VOID;
                default -> new TypeInfo(s.name());
            };
            case GenericType g -> {
                var args = g.typeArgs().stream().map(this::resolveTypeExpr).toList();
                yield new TypeInfo(g.name(), args);
            }
            case ArrayType a -> new TypeInfo(resolveTypeExpr(a.elementType()).name() + "[]");
            case OptionalType o -> {
                var inner = resolveTypeExpr(o.inner());
                yield new TypeInfo(inner.name(), inner.args(), true);
            }
            case FuncType f -> TypeInfo.ANY;
        };
    }
}
