package zinc;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import zinc.Ast.*;

/**
 * Return-type inference for the braces-Python surface. PyParser marks an unannotated
 * `def` with retType "infer"; this pass resolves it to the type of the method's returned
 * expression (or "void" if it returns no value). Params are annotated and locals infer
 * from their initializers, so a small typer mirroring CodeGen.exprType suffices.
 *
 * Drift is self-correcting: CodeGen re-checks every return via checkBind(retType,
 * exprType(rv)), so a wrong guess here fails transpile loudly rather than miscompiling.
 * Runs to a fixpoint so a helper that returns the result of a later-defined helper still
 * resolves.
 */
final class PyInfer {
  private final Map<String, String> sigs = new HashMap<>(); // callee/arity -> retType
  private Map<String, String> env;                          // local + param types

  private PyInfer() {}

  static Program infer(Program p) {
    if (p.classes().isEmpty()) return p;
    var inf = new PyInfer();
    for (ClassDecl c : p.classes()) {
      for (MethodDecl m : c.methods()) {
        inf.sigs.put(m.name() + "/" + m.params().size(), m.retType());
      }
    }
    List<ClassDecl> classes = p.classes();
    for (int pass = 0; pass < 3; pass++) { // fixpoint for forward references
      boolean changed = false;
      var next = new ArrayList<ClassDecl>();
      for (ClassDecl c : classes) {
        var methods = new ArrayList<MethodDecl>();
        for (MethodDecl m : c.methods()) {
          if (m.retType().equals("infer")) {
            String t = inf.inferMethod(m);
            if (!t.equals("infer")) {
              changed = true;
              inf.sigs.put(m.name() + "/" + m.params().size(), t);
            }
            methods.add(new MethodDecl(t, m.name(), m.params(), m.body(), m.mods()));
          } else {
            methods.add(m);
          }
        }
        next.add(new ClassDecl(c.name(), methods));
      }
      classes = next;
      if (!changed) break;
    }
    for (ClassDecl c : classes) {
      for (MethodDecl m : c.methods()) {
        if (m.retType().equals("infer")) {
          throw new CompileError(c.name() + "." + m.name()
              + ": cannot infer return type — annotate it with '-> T'");
        }
      }
    }
    return new Program(p.imports(), classes, p.records(), p.actors(), p.enums(),
        p.application(), p.exceptions(), p.interfaces(), p.instanceClasses(), p.tests());
  }

  /** "void" if the body returns no value; the returned expression's type otherwise;
   *  "infer" (unresolved) if a value is returned but cannot be typed yet. */
  private String inferMethod(MethodDecl m) {
    env = new HashMap<>();
    for (Param param : m.params()) {
      env.put(param.name(), param.type());
    }
    var rets = new ArrayList<Expr>();
    scan(m.body(), rets);
    if (rets.isEmpty()) return "void";
    for (Expr rv : rets) {
      String t = typeOf(rv);
      if (t != null) return t;
    }
    return "infer"; // value returned but not yet typable — try again next pass
  }

  /** Walk a block: record local types from declarations, collect value-returns. */
  private void scan(Block b, List<Expr> rets) {
    if (b == null) return;
    for (Stmt s : b.stmts()) {
      switch (s) {
        case VarStmt v -> {
          String t = v.type().equals("var") ? typeOf(v.init()) : v.type();
          if (t != null) env.put(v.name(), t);
        }
        case ReturnStmt r -> {
          if (r.value() != null) rets.add(r.value());
        }
        case IfStmt x -> {
          scan(x.thenBlock(), rets);
          scan(x.elseBlock(), rets);
        }
        case WhileStmt x -> scan(x.body(), rets);
        case ForEachStmt x -> scan(x.body(), rets);
        case SeqStmt x -> {
          for (Stmt inner : x.stmts()) {
            scan(new Block(List.of(inner)), rets);
          }
        }
        default -> { }
      }
    }
  }

  /** A subset of CodeGen.exprType over the inference environment; null = unknown. */
  private String typeOf(Expr e) {
    return switch (e) {
      case null -> null;
      case IntLit x -> "int";
      case FloatLit x -> "double";
      case BoolLit x -> "boolean";
      case StrLit x -> "String";
      case VarRef x -> env.get(x.name());
      case NewExpr x -> x.typeName();
      case Ast.Cast x -> x.type().equals("double") ? "double" : "int";
      case Unary x -> x.op().equals("!") ? "boolean" : typeOf(x.operand());
      case Ternary x -> {
        String t = typeOf(x.thenExpr());
        yield t != null ? t : typeOf(x.elseExpr());
      }
      case Binary x -> {
        if (x.op().equals("+") && ("String".equals(typeOf(x.left()))
            || "String".equals(typeOf(x.right())))) {
          yield "String";
        }
        yield switch (x.op()) {
          case "+", "-", "*", "/", "%" ->
              "double".equals(typeOf(x.left())) || "double".equals(typeOf(x.right()))
                  ? "double" : "int";
          default -> "boolean";
        };
      }
      case Call x -> {
        String t = sigs.get(x.callee() + "/" + x.args().size());
        yield t == null || t.equals("infer") ? null : t;
      }
      default -> null;
    };
  }
}
