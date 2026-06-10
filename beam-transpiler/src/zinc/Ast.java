package zinc;

import java.util.List;

final class Ast {
  private Ast() {}

  record Program(List<Import> imports, List<FnDecl> fns) {}

  /** import util/math -> path [util, math], alias "math", Erlang module util_math. */
  record Import(List<String> path) {
    String alias() {
      return path.get(path.size() - 1);
    }

    String erlMod() {
      return String.join("_", path);
    }

    String display() {
      return String.join("/", path);
    }
  }

  record FnDecl(String name, List<String> params, Block body) {}

  record Block(List<Stmt> stmts) {}

  sealed interface Stmt {}

  record VarStmt(String name, Expr init) implements Stmt {}

  record AssignStmt(String name, String op, Expr value) implements Stmt {}

  record FieldAssignStmt(String objVar, String field, String op, Expr value) implements Stmt {}

  /** elseBlock is null when there is no else branch. */
  record IfStmt(Expr cond, Block thenBlock, Block elseBlock) implements Stmt {}

  record ForRangeStmt(String varName, Expr start, Expr end, Block body) implements Stmt {}

  record ForEachStmt(String varName, Expr iterable, Block body) implements Stmt {}

  record WhileStmt(Expr cond, Block body) implements Stmt {}

  /** value is null for a bare `return`. */
  record ReturnStmt(Expr value) implements Stmt {}

  record ExprStmt(Expr expr) implements Stmt {}

  record BreakStmt() implements Stmt {}

  record ContinueStmt() implements Stmt {}

  sealed interface Expr {}

  record IntLit(long value) implements Expr {}

  record FloatLit(double value) implements Expr {}

  record BoolLit(boolean value) implements Expr {}

  record VarRef(String name) implements Expr {}

  record ListLit(List<Expr> elems) implements Expr {}

  record FieldInit(String name, Expr value) {}

  record StructLit(String name, List<FieldInit> fields) implements Expr {}

  record FieldAccess(Expr obj, String field) implements Expr {}

  record Index(Expr obj, Expr index) implements Expr {}

  record Binary(String op, Expr left, Expr right) implements Expr {}

  record Unary(String op, Expr operand) implements Expr {}

  record Call(String callee, List<Expr> args) implements Expr {}

  /** x.f(args) — today only valid when x is an imported module alias; actors reuse it later. */
  record MethodCall(Expr target, String method, List<Expr> args) implements Expr {}

  record StrLit(List<StrPart> parts) implements Expr {}

  sealed interface StrPart {}

  record StrText(String text) implements StrPart {}

  record StrExpr(Expr expr) implements StrPart {}
}
