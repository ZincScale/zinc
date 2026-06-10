package zinc;

import java.util.List;

final class Ast {
  private Ast() {}

  record Program(List<Import> imports, List<ClassDecl> classes, List<RecordDecl> records,
      List<ActorDecl> actors) {}

  /** import util.MathUtil; -> class MathUtil defined in util/MathUtil.src */
  record Import(List<String> path) {
    String className() {
      return path.get(path.size() - 1);
    }

    String display() {
      return String.join(".", path);
    }
  }

  record ClassDecl(String name, List<MethodDecl> methods) {
    String erlMod() {
      return name.toLowerCase();
    }
  }

  record MethodDecl(String retType, String name, List<Param> params, Block body) {}

  record Param(String type, String name) {}

  /** record Point(int x, int y) {} -> map; accessors p.x() -> maps:get */
  record RecordDecl(String name, List<Param> components) {}

  /** actor -> gen_server module; void method = async cast, typed method = sync call */
  record ActorDecl(String name, List<FieldDecl> fields, List<MethodDecl> methods) {
    String erlMod() {
      return name.toLowerCase();
    }
  }

  record FieldDecl(String type, String name, Expr init) {}

  record Block(List<Stmt> stmts) {}

  sealed interface Stmt {}

  /** type is the declared type, or "var" to infer from init. */
  record VarStmt(String type, String name, Expr init) implements Stmt {}

  record AssignStmt(String name, String op, Expr value) implements Stmt {}

  record FieldAssignStmt(String objVar, String field, String op, Expr value) implements Stmt {}

  record IfStmt(Expr cond, Block thenBlock, Block elseBlock) implements Stmt {}

  record ForEachStmt(String varType, String varName, Expr iterable, Block body) implements Stmt {}

  record WhileStmt(Expr cond, Block body) implements Stmt {}

  record ReturnStmt(Expr value) implements Stmt {}

  record ExprStmt(Expr expr) implements Stmt {}

  record BreakStmt() implements Stmt {}

  record ContinueStmt() implements Stmt {}

  /** classic for desugared to { init; while }, sharing the enclosing scope. */
  record SeqStmt(List<Stmt> stmts) implements Stmt {}

  sealed interface Expr {}

  record IntLit(long value) implements Expr {}

  record FloatLit(double value) implements Expr {}

  record BoolLit(boolean value) implements Expr {}

  record StrLit(String text) implements Expr {}

  record VarRef(String name) implements Expr {}

  /** array initializer {1, 2, 3} */
  record ListLit(List<Expr> elems) implements Expr {}

  record NewExpr(String typeName, List<Expr> args) implements Expr {}

  record FieldAccess(Expr obj, String field) implements Expr {}

  record Index(Expr obj, Expr index) implements Expr {}

  record Binary(String op, Expr left, Expr right) implements Expr {}

  record Unary(String op, Expr operand) implements Expr {}

  /** bare call -> static method of the enclosing class */
  record Call(String callee, List<Expr> args) implements Expr {}

  /** x.f(args): System.out.println / Thread.sleep / Class.staticMethod / record accessor / actor handle */
  record MethodCall(Expr target, String method, List<Expr> args) implements Expr {}

  record SpawnExpr(String actorName) implements Expr {}
}
