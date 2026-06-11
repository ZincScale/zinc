package zinc;

import java.util.List;
import java.util.Set;
import zinc.Ast.ActorDecl;
import zinc.Ast.ApplicationDecl;
import zinc.Ast.ArrayNewExpr;
import zinc.Ast.AssignStmt;
import zinc.Ast.Binary;
import zinc.Ast.Block;
import zinc.Ast.BreakStmt;
import zinc.Ast.Call;
import zinc.Ast.ClassDecl;
import zinc.Ast.ContinueStmt;
import zinc.Ast.Expr;
import zinc.Ast.ExprStmt;
import zinc.Ast.FieldAccess;
import zinc.Ast.FieldAssignStmt;
import zinc.Ast.FieldDecl;
import zinc.Ast.ForEachStmt;
import zinc.Ast.IfStmt;
import zinc.Ast.Index;
import zinc.Ast.IndexAssignStmt;
import zinc.Ast.LambdaExpr;
import zinc.Ast.ListLit;
import zinc.Ast.MethodCall;
import zinc.Ast.MethodDecl;
import zinc.Ast.NewExpr;
import zinc.Ast.Program;
import zinc.Ast.ReturnStmt;
import zinc.Ast.SeqStmt;
import zinc.Ast.SpawnExpr;
import zinc.Ast.Stmt;
import zinc.Ast.SwitchCase;
import zinc.Ast.SwitchStmt;
import zinc.Ast.TryStmt;
import zinc.Ast.Unary;
import zinc.Ast.VarStmt;
import zinc.Ast.WhileStmt;

/**
 * Project-wide resolution pass: `new T(...)` where T is an Actor class is a spawn
 * (the instance IS the live process), rewritten to SpawnExpr. Needs the cross-file
 * actor registry, so it runs in Main after all files are parsed.
 */
final class Resolve {
  private final Set<String> actorNames;

  private Resolve(Set<String> actorNames) {
    this.actorNames = actorNames;
  }

  static Program spawns(Program p, Set<String> actorNames) {
    var r = new Resolve(actorNames);
    return new Program(p.imports(), p.classes().stream().map(r::clazz).toList(), p.records(),
        p.actors().stream().map(r::actor).toList(), p.enums(), r.app(p.application()),
        p.exceptions());
  }

  private ApplicationDecl app(ApplicationDecl a) {
    if (a == null) return null;
    List<FieldDecl> fields = a.fields().stream()
        .map(f -> new FieldDecl(f.type(), f.name(), expr(f.init()))).toList();
    return new ApplicationDecl(a.name(), fields, a.main() == null ? null : method(a.main()));
  }

  private ClassDecl clazz(ClassDecl c) {
    return new ClassDecl(c.name(), c.methods().stream().map(this::method).toList());
  }

  private ActorDecl actor(ActorDecl a) {
    List<FieldDecl> fields = a.fields().stream()
        .map(f -> new FieldDecl(f.type(), f.name(), expr(f.init()))).toList();
    return new ActorDecl(a.name(), fields, a.ctor() == null ? null : method(a.ctor()),
        a.methods().stream().map(this::method).toList());
  }

  private MethodDecl method(MethodDecl m) {
    return new MethodDecl(m.retType(), m.name(), m.params(), block(m.body()));
  }

  private Block block(Block b) {
    return b == null ? null : new Block(b.stmts().stream().map(this::stmt).toList());
  }

  private Stmt stmt(Stmt s) {
    return switch (s) {
      case VarStmt x -> new VarStmt(x.type(), x.name(), expr(x.init()));
      case AssignStmt x -> new AssignStmt(x.name(), x.op(), expr(x.value()));
      case FieldAssignStmt x ->
          new FieldAssignStmt(x.objVar(), x.field(), x.op(), expr(x.value()));
      case IndexAssignStmt x ->
          new IndexAssignStmt(x.arrVar(), expr(x.index()), x.op(), expr(x.value()));
      case SwitchStmt x -> new SwitchStmt(expr(x.subject()),
          x.cases().stream()
              .map(c -> new SwitchCase(c.labels().stream().map(this::expr).toList(),
                  block(c.body())))
              .toList(),
          block(x.defaultBlock()));
      case IfStmt x -> new IfStmt(expr(x.cond()), block(x.thenBlock()), block(x.elseBlock()));
      case ForEachStmt x ->
          new ForEachStmt(x.varType(), x.varName(), expr(x.iterable()), block(x.body()));
      case WhileStmt x -> new WhileStmt(expr(x.cond()), block(x.body()));
      case ReturnStmt x -> new ReturnStmt(expr(x.value()));
      case ExprStmt x -> new ExprStmt(expr(x.expr()));
      case BreakStmt x -> x;
      case ContinueStmt x -> x;
      case SeqStmt x -> new SeqStmt(x.stmts().stream().map(this::stmt).toList());
      case TryStmt x -> new TryStmt(block(x.tryBlock()),
          x.clauses().stream()
              .map(c -> new Ast.CatchClause(c.exType(), c.var(), block(c.body()))).toList());
      case Ast.ThrowStmt x ->
          new Ast.ThrowStmt(x.exType(), x.args().stream().map(this::expr).toList());
    };
  }

  private Expr expr(Expr e) {
    return switch (e) {
      case null -> null;
      case NewExpr x -> {
        List<Expr> args = x.args().stream().map(this::expr).toList();
        yield actorNames.contains(x.typeName()) ? new SpawnExpr(x.typeName(), args)
            : new NewExpr(x.typeName(), args);
      }
      case ListLit x -> new ListLit(x.elems().stream().map(this::expr).toList());
      case ArrayNewExpr x -> new ArrayNewExpr(x.elemType(), expr(x.size()));
      case FieldAccess x -> new FieldAccess(expr(x.obj()), x.field());
      case Index x -> new Index(expr(x.obj()), expr(x.index()));
      case Binary x -> new Binary(x.op(), expr(x.left()), expr(x.right()));
      case Unary x -> new Unary(x.op(), expr(x.operand()));
      case Call x -> new Call(x.callee(), x.args().stream().map(this::expr).toList());
      case MethodCall x ->
          new MethodCall(expr(x.target()), x.method(), x.args().stream().map(this::expr).toList());
      case SpawnExpr x -> new SpawnExpr(x.actorName(), x.args().stream().map(this::expr).toList());
      case LambdaExpr x -> new LambdaExpr(x.params(), block(x.body()));
      default -> e; // literals, VarRef
    };
  }
}
