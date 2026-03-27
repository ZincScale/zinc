// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.util.List;

public final class Ast {

    private Ast() {}

    // --- Root ----------------------------------------------------------------

    public record Program(
        String sourceFile,
        PackageDecl pkg,           // nullable
        List<ImportDecl> imports,
        List<TopLevelDecl> decls,
        List<Stmt> stmts           // script mode top-level statements
    ) {}

    public record PackageDecl(String path) {}
    public record ImportDecl(String path, String alias) {}

    // --- Top-level declarations ----------------------------------------------

    public sealed interface TopLevelDecl permits
        ClassDecl, InterfaceDecl, FnDecl, DataClassDecl, EnumDecl, ConstDecl, SealedClassDecl {}

    public record ClassDecl(
        int line, String name, boolean isAbstract,
        List<String> typeParams, List<String> parents,
        List<FieldDecl> fields, List<CtorDecl> ctors,
        List<MethodDecl> methods, List<Annotation> annotations
    ) implements TopLevelDecl {}

    public record SealedClassDecl(
        int line, String name, List<String> parents,
        List<FieldDecl> fields, List<CtorDecl> ctors,
        List<MethodDecl> methods, List<DataClassDecl> variants,
        List<Annotation> annotations
    ) implements TopLevelDecl {}

    public record InterfaceDecl(
        int line, String name, List<MethodSig> methods
    ) implements TopLevelDecl {}

    public record DataClassDecl(
        int line, String name, List<String> typeParams,
        List<String> parents, List<FieldDecl> params,
        List<MethodDecl> methods
    ) implements TopLevelDecl {}

    public record EnumDecl(int line, String name, List<String> variants) implements TopLevelDecl {}

    public record ConstDecl(int line, String name, boolean isPub, TypeExpr type, Expr value) implements TopLevelDecl {}

    public record FnDecl(
        int line, String name, boolean isPub,
        List<String> typeParams, List<ParamDecl> params,
        TypeExpr returnType, BlockStmt body,
        List<Annotation> annotations
    ) implements TopLevelDecl, Stmt {}

    // --- Supporting declarations ---------------------------------------------

    public record MethodSig(String name, boolean isPub, List<ParamDecl> params, TypeExpr returnType) {}

    public record CtorDecl(List<ParamDecl> params, BlockStmt body, List<Expr> superArgs) {}

    public record MethodDecl(
        String name, boolean isPub, boolean isStatic, boolean isAbstract,
        List<ParamDecl> params, TypeExpr returnType,
        BlockStmt body, List<Annotation> annotations
    ) {}

    public record FieldDecl(
        String name, boolean isPub, boolean isReadonly, boolean isConst, boolean isInit,
        TypeExpr type, Expr defaultValue, List<Annotation> annotations
    ) {}

    public record ParamDecl(String name, TypeExpr type, Expr defaultValue, boolean isVariadic) {}

    public record NamedArg(String name, Expr value) {}

    public record Annotation(String name, List<String> args) {}

    // --- Type expressions ----------------------------------------------------

    public sealed interface TypeExpr permits
        SimpleType, GenericType, OptionalType, ArrayType, FuncType {}

    public record SimpleType(String name) implements TypeExpr {}
    public record GenericType(String name, List<TypeExpr> typeArgs) implements TypeExpr {}
    public record OptionalType(TypeExpr inner) implements TypeExpr {}
    public record ArrayType(TypeExpr elementType) implements TypeExpr {}
    public record FuncType(List<TypeExpr> params, TypeExpr returnType) implements TypeExpr {}

    // --- Statements ----------------------------------------------------------

    public sealed interface Stmt permits
        BlockStmt, VarStmt, TupleVarStmt, AssignStmt, ReturnStmt,
        IfStmt, ForStmt, WhileStmt, MatchStmt,
        BreakStmt, ContinueStmt, ExprStmt,
        ParallelForStmt, ConcurrentStmt, TimeoutStmt,
        WithStmt, DeferStmt, LockStmt, FnDecl {}

    public record BlockStmt(List<Stmt> stmts) implements Stmt {}

    public record VarStmt(
        int line, String name, TypeExpr type, Expr value,
        boolean isConst, OrHandler orHandler
    ) implements Stmt {}

    public record TupleVarStmt(int line, List<String> names, Expr value, OrHandler orHandler) implements Stmt {}

    public record AssignStmt(int line, Expr target, String op, Expr value, OrHandler orHandler) implements Stmt {}

    public record ReturnStmt(int line, Expr value) implements Stmt {}

    public record IfStmt(int line, Expr cond, BlockStmt then, Stmt elseStmt) implements Stmt {}

    public record ForStmt(
        int line,
        Stmt init, Expr cond, Stmt post,           // C-style
        boolean isRange, String indexVar, String item, Expr range, // range-style
        BlockStmt body
    ) implements Stmt {}

    public record WhileStmt(int line, Expr cond, BlockStmt body) implements Stmt {}

    public record MatchStmt(int line, Expr subject, List<MatchCase> cases) implements Stmt {}
    public record MatchCase(Expr pattern, BlockStmt body) {}

    public record BreakStmt() implements Stmt {}
    public record ContinueStmt() implements Stmt {}

    public record ExprStmt(int line, Expr expr, OrHandler orHandler) implements Stmt {}

    public record ParallelForStmt(
        int line, String item, String indexVar, Expr range,
        BlockStmt body, OrHandler orHandler, int max
    ) implements Stmt {}

    public record ConcurrentStmt(
        int line, List<Expr> tasks, boolean firstOnly,
        List<String> names, OrHandler orHandler
    ) implements Stmt {}

    public record TimeoutStmt(int line, Expr duration, BlockStmt body, OrHandler orHandler) implements Stmt {}

    public record WithStmt(int line, List<WithResource> resources, BlockStmt body) implements Stmt {}
    public record WithResource(String name, Expr value, OrHandler orHandler) {}

    public record DeferStmt(Expr expr) implements Stmt {}

    public record LockStmt(int line, Expr mutex, BlockStmt body) implements Stmt {}

    // --- Error handling ------------------------------------------------------

    public record OrHandler(BlockStmt body, List<OrMatchCase> matchCases, String matchVar) {}
    public record OrMatchCase(String type, BlockStmt body) {}

    // --- Expressions ---------------------------------------------------------

    public sealed interface Expr permits
        IntLit, FloatLit, StringLit, StringInterpLit, RawStringLit,
        BoolLit, NullLit, Ident, ThisExpr,
        BinaryExpr, UnaryExpr, CallExpr, SelectorExpr, SafeNavExpr,
        IndexExpr, SliceExpr, ListLit, MapLit,
        LambdaExpr, SpawnExpr, IfExpr, MatchExpr,
        RangeExpr, TupleLit, SpreadExpr, TypeAssertExpr,
        SuperCallExpr {}

    public record IntLit(String value) implements Expr {}
    public record FloatLit(String value) implements Expr {}
    public record StringLit(String value) implements Expr {}
    public record StringInterpLit(List<Expr> parts) implements Expr {}
    public record RawStringLit(String value) implements Expr {}
    public record BoolLit(boolean value) implements Expr {}
    public record NullLit() implements Expr {}
    public record Ident(String name) implements Expr {}
    public record ThisExpr() implements Expr {}

    public record BinaryExpr(Expr left, String op, Expr right) implements Expr {}
    public record UnaryExpr(String op, Expr operand) implements Expr {}

    public record CallExpr(
        Expr callee, List<Expr> args, List<NamedArg> namedArgs,
        List<String> typeArgs, boolean isNew
    ) implements Expr {}

    public record SelectorExpr(Expr object, String field) implements Expr {}
    public record SafeNavExpr(Expr object, String field, CallExpr call) implements Expr {}
    public record IndexExpr(Expr object, Expr index) implements Expr {}
    public record SliceExpr(Expr object, Expr low, Expr high) implements Expr {}

    public record ListLit(List<Expr> elements) implements Expr {}
    public record MapLit(List<Expr> keys, List<Expr> values) implements Expr {}

    public record LambdaExpr(List<ParamDecl> params, BlockStmt body) implements Expr {}

    public record SpawnExpr(int line, BlockStmt body, OrHandler orHandler) implements Expr {}

    public record IfExpr(Expr cond, Expr then, Expr elseExpr) implements Expr {}
    public record MatchExpr(Expr subject, List<MatchExprCase> cases) implements Expr {}
    public record MatchExprCase(Expr pattern, Expr value) {}

    public record RangeExpr(Expr start, Expr end, boolean inclusive) implements Expr {}
    public record TupleLit(List<Expr> elements) implements Expr {}
    public record SpreadExpr(Expr expr) implements Expr {}
    public record TypeAssertExpr(Expr object, String typeName, boolean isCheck) implements Expr {}
    public record SuperCallExpr(List<Expr> args) implements Expr {}
}
