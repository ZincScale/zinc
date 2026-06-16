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

  /** class NotFound extends RuntimeException { NotFound(String m){super(m);} } — the one
   *  sanctioned extends. Unchecked (failures auto-relay); thrown as
   *  erlang:error({zinc_exc, 'fq.tag', #{message => ...}}). */
  // ctor: the explicit user constructor (null for runtime-thrown builtins); its
  // super(expr) call supplies the message. Drops the old auto-ctor-from-fields.
  record ExceptionDecl(String name, List<FieldDecl> fields, MethodDecl ctor) {}

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

  record MethodDecl(String retType, String name, List<Param> params, Block body,
      java.util.Set<String> mods) {
    MethodDecl(String retType, String name, List<Param> params, Block body) {
      this(retType, name, params, body, java.util.Set.of());
    }

    boolean isPrivate() {
      return mods.contains("private");
    }
  }

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
  record FieldDecl(String type, String name, Expr init, java.util.Set<String> mods) {
    FieldDecl(String type, String name, Expr init) {
      this(type, name, init, java.util.Set.of());
    }
  }

  record Block(List<Stmt> stmts) {}

  /** Statements carry their source line (0 = synthetic/desugared) — the spine of
   *  source maps: transpile errors and Assert messages cite <file>:<line>. */
  sealed interface Stmt {
    default int line() {
      return 0;
    }
  }

  /** type is the declared type, or "var" to infer from init. */
  record VarStmt(String type, String name, Expr init, boolean isFinal, int line)
      implements Stmt {
    VarStmt(String type, String name, Expr init) {
      this(type, name, init, false, 0);
    }

    VarStmt(String type, String name, Expr init, boolean isFinal) {
      this(type, name, init, isFinal, 0);
    }
  }

  record AssignStmt(String name, String op, Expr value, int line) implements Stmt {
    AssignStmt(String name, String op, Expr value) {
      this(name, op, value, 0);
    }
  }

  record FieldAssignStmt(String objVar, String field, String op, Expr value, int line)
      implements Stmt {
    FieldAssignStmt(String objVar, String field, String op, Expr value) {
      this(objVar, field, op, value, 0);
    }
  }

  /** xs[i] = v  -> SSA rebind via array:set (receiver must be T[]). */
  record IndexAssignStmt(String arrVar, Expr index, String op, Expr value, int line)
      implements Stmt {
    IndexAssignStmt(String arrVar, Expr index, String op, Expr value) {
      this(arrVar, index, op, value, 0);
    }
  }

  /** Arrow switch; labels are constants (or bare enum values when the subject is an enum). */
  record SwitchStmt(Expr subject, List<SwitchCase> cases, Block defaultBlock, int line)
      implements Stmt {
    SwitchStmt(Expr subject, List<SwitchCase> cases, Block defaultBlock) {
      this(subject, cases, defaultBlock, 0);
    }
  }

  record SwitchCase(List<Expr> labels, Block body) {}

  record IfStmt(Expr cond, Block thenBlock, Block elseBlock, int line) implements Stmt {
    IfStmt(Expr cond, Block thenBlock, Block elseBlock) {
      this(cond, thenBlock, elseBlock, 0);
    }
  }

  record ForEachStmt(String varType, String varName, Expr iterable, Block body, int line)
      implements Stmt {
    ForEachStmt(String varType, String varName, Expr iterable, Block body) {
      this(varType, varName, iterable, body, 0);
    }
  }

  record WhileStmt(Expr cond, Block body, int line) implements Stmt {
    WhileStmt(Expr cond, Block body) {
      this(cond, body, 0);
    }
  }

  record ReturnStmt(Expr value, int line) implements Stmt {
    ReturnStmt(Expr value) {
      this(value, 0);
    }
  }

  record ExprStmt(Expr expr, int line) implements Stmt {
    ExprStmt(Expr expr) {
      this(expr, 0);
    }
  }

  record BreakStmt() implements Stmt {}

  record ContinueStmt() implements Stmt {}

  /** classic for desugared to { init; while }, sharing the enclosing scope. */
  record SeqStmt(List<Stmt> stmts) implements Stmt {}

  /** try {..} catch (NotFound e) {..} catch (Exception e) {..} — clauses match in order;
   *  catch (Exception e) is the catch-all and also catches native BEAM errors. */
  record TryStmt(List<Resource> resources, Block tryBlock, List<CatchClause> clauses, int line)
      implements Stmt {
    TryStmt(List<Resource> resources, Block tryBlock, List<CatchClause> clauses) {
      this(resources, tryBlock, clauses, 0);
    }
  }

  record CatchClause(String exType, String var, Block body) {}

  /** try-with-resources head: `try (Type var = init)` — the handle is closed on block
   *  exit (success or throw). v1: scoped AutoCloseable handles only (Reader/Writer). */
  record Resource(String type, String var, Expr init) {}

  /** throw new NotFound("msg") -> erlang:error({zinc_exc, 'fq.tag', FieldsMap}) */
  record ThrowStmt(String exType, List<Expr> args, int line) implements Stmt {
    ThrowStmt(String exType, List<Expr> args) {
      this(exType, args, 0);
    }
  }

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

  record Ternary(Expr cond, Expr thenExpr, Expr elseExpr) implements Expr {}

  /** bare call -> static method of the enclosing class */
  record Call(String callee, List<Expr> args) implements Expr {}

  /** x.f(args): System.out.println / Thread.sleep / Class.staticMethod / record accessor / actor handle */
  record MethodCall(Expr target, String method, List<Expr> args) implements Expr {}

  record SpawnExpr(String actorName, List<Expr> args) implements Expr {}

  /** x -> e  |  (a, b) -> { ... }  -> Erlang fun; captures must be effectively final. */
  record LambdaExpr(List<String> params, Block body) implements Expr {}
}
