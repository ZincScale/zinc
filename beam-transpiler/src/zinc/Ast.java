package zinc;

import java.util.List;

final class Ast {
  private Ast() {}

  record Program(List<Import> imports, List<ClassDecl> classes, List<RecordDecl> records,
      List<ActorDecl> actors, List<EnumDecl> enums, ApplicationDecl application,
      List<ExceptionDecl> exceptions, List<InterfaceDecl> interfaces,
      List<InstanceClassDecl> instanceClasses, List<TestDecl> tests) {}

  /** class CalcTest implements Test { public void addsUp() {...} } — every public void
   *  zero-arg method is a test case (JUnit 3 / EUnit convention), the rest helpers.
   *  Lowers to an EUnit module: process-per-test, parallel. Never ships in releases. */
  record TestDecl(String name, List<MethodDecl> methods, List<String> testMethods) {}

  /** interface Greeter { String greet(String n); } — signatures only; SAM if one. */
  record InterfaceDecl(String name, List<MethodDecl> sigs) {}

  /** class English implements Greeter { ... } — instance class: module + map term,
   *  '$class' => module atom (dispatch + boundary guards), fields ctor-set then immutable. */
  record InstanceClassDecl(String name, String iface, List<FieldDecl> fields,
      MethodDecl ctor, List<MethodDecl> methods) {}

  /** class NotFound extends Exception { String message; } — the one sanctioned extends.
   *  Final value; thrown as erlang:error({zinc_exc, 'fq.tag', FieldsMap}). */
  record ExceptionDecl(String name, List<FieldDecl> fields) {}

  /** class Main implements Application { Actor fields = root children; optional main. }
   *  The explicit root: lowers to the generated root supervisor's static children. */
  record ApplicationDecl(String name, List<FieldDecl> fields, MethodDecl main) {
    String erlMod() {
      return name.toLowerCase();
    }
  }

  /** enum Color { RED, GREEN } — values lower to atoms 'RED', 'GREEN'. */
  record EnumDecl(String name, List<String> values) {}

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

  /** actor -> gen_server module; void method = async cast, typed method = sync call.
   *  ctor (method named like the actor, null if absent) receives spawn args; the child
   *  spec embeds them, so a supervisor restart re-runs the SAME constructor. */
  record ActorDecl(String name, List<FieldDecl> fields, MethodDecl ctor,
      List<MethodDecl> methods) {
    String erlMod() {
      return name.toLowerCase();
    }
  }

  /** init is null for `int count;` — defaulted by type (0, 0.0, false, "", undefined). */
  record FieldDecl(String type, String name, Expr init) {}

  record Block(List<Stmt> stmts) {}

  sealed interface Stmt {}

  /** type is the declared type, or "var" to infer from init. */
  record VarStmt(String type, String name, Expr init) implements Stmt {}

  record AssignStmt(String name, String op, Expr value) implements Stmt {}

  record FieldAssignStmt(String objVar, String field, String op, Expr value) implements Stmt {}

  /** xs[i] = v  -> SSA rebind via array:set (receiver must be T[]). */
  record IndexAssignStmt(String arrVar, Expr index, String op, Expr value) implements Stmt {}

  /** Arrow switch; labels are constants (or bare enum values when the subject is an enum). */
  record SwitchStmt(Expr subject, List<SwitchCase> cases, Block defaultBlock) implements Stmt {}

  record SwitchCase(List<Expr> labels, Block body) {}

  record IfStmt(Expr cond, Block thenBlock, Block elseBlock) implements Stmt {}

  record ForEachStmt(String varType, String varName, Expr iterable, Block body) implements Stmt {}

  record WhileStmt(Expr cond, Block body) implements Stmt {}

  record ReturnStmt(Expr value) implements Stmt {}

  record ExprStmt(Expr expr) implements Stmt {}

  record BreakStmt() implements Stmt {}

  record ContinueStmt() implements Stmt {}

  /** classic for desugared to { init; while }, sharing the enclosing scope. */
  record SeqStmt(List<Stmt> stmts) implements Stmt {}

  /** try {..} catch (NotFound e) {..} catch (Exception e) {..} — clauses match in order;
   *  catch (Exception e) is the catch-all and also catches native BEAM errors. */
  record TryStmt(Block tryBlock, List<CatchClause> clauses) implements Stmt {}

  record CatchClause(String exType, String var, Block body) {}

  /** throw new NotFound("msg") -> erlang:error({zinc_exc, 'fq.tag', FieldsMap}) */
  record ThrowStmt(String exType, List<Expr> args) implements Stmt {}

  sealed interface Expr {}

  record IntLit(long value) implements Expr {}

  record FloatLit(double value) implements Expr {}

  record BoolLit(boolean value) implements Expr {}

  record StrLit(String text) implements Expr {}

  record VarRef(String name) implements Expr {}

  /** array initializer {1, 2, 3} */
  record ListLit(List<Expr> elems) implements Expr {}

  record NewExpr(String typeName, List<Expr> args) implements Expr {}

  /** new int[n] -> array:new(N, {default, <type default>}) */
  record ArrayNewExpr(String elemType, Expr size) implements Expr {}

  record FieldAccess(Expr obj, String field) implements Expr {}

  record Index(Expr obj, Expr index) implements Expr {}

  record Binary(String op, Expr left, Expr right) implements Expr {}

  record Unary(String op, Expr operand) implements Expr {}

  /** bare call -> static method of the enclosing class */
  record Call(String callee, List<Expr> args) implements Expr {}

  /** x.f(args): System.out.println / Thread.sleep / Class.staticMethod / record accessor / actor handle */
  record MethodCall(Expr target, String method, List<Expr> args) implements Expr {}

  record SpawnExpr(String actorName, List<Expr> args) implements Expr {}

  /** x -> e  |  (a, b) -> { ... }  -> Erlang fun; captures must be effectively final. */
  record LambdaExpr(List<String> params, Block body) implements Expr {}
}
