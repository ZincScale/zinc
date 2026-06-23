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
    if (p.classes().isEmpty() && p.actors().isEmpty() && p.instanceClasses().isEmpty()) return p;
    var inf = new PyInfer();
    for (ClassDecl c : p.classes()) {
      for (MethodDecl m : c.methods()) {
        inf.sigs.put(m.name() + "/" + m.params().size(), m.retType());
      }
    }
    List<ClassDecl> classes = p.classes();
    List<ActorDecl> actors = p.actors();
    List<InstanceClassDecl> instanceClasses = p.instanceClasses();
    for (int pass = 0; pass < 3; pass++) { // fixpoint for forward references
      boolean changed = false;
      var nextClasses = new ArrayList<ClassDecl>();
      for (ClassDecl c : classes) {
        var methods = new ArrayList<MethodDecl>();
        for (MethodDecl m : c.methods()) {
          if (m.retType().equals("infer")) {
            String t = inf.inferMethod(m, Map.of());
            if (!t.equals("infer")) {
              changed = true;
              inf.sigs.put(m.name() + "/" + m.params().size(), t);
            }
            methods.add(new MethodDecl(t, m.name(), m.params(), m.body(), m.mods()));
          } else {
            methods.add(m);
          }
        }
        nextClasses.add(new ClassDecl(c.name(), methods));
      }
      classes = nextClasses;
      var nextActors = new ArrayList<ActorDecl>();
      for (ActorDecl a : actors) {
        var fieldTypes = new HashMap<String, String>();
        for (FieldDecl f : a.fields()) {
          fieldTypes.put(f.name(), f.type());
        }
        var methods = new ArrayList<MethodDecl>();
        for (MethodDecl m : a.methods()) {
          if (m.retType().equals("infer")) {
            String t = inf.inferMethod(m, fieldTypes);
            if (!t.equals("infer")) changed = true;
            methods.add(new MethodDecl(t, m.name(), m.params(), m.body(), m.mods()));
          } else {
            methods.add(m);
          }
        }
        nextActors.add(new ActorDecl(a.name(), a.fields(), a.ctor(), methods));
      }
      actors = nextActors;
      var nextInst = new ArrayList<InstanceClassDecl>();
      for (InstanceClassDecl c : instanceClasses) {
        var fieldTypes = new HashMap<String, String>();
        for (FieldDecl f : c.fields()) {
          fieldTypes.put(f.name(), f.type());
        }
        var methods = new ArrayList<MethodDecl>();
        for (MethodDecl m : c.methods()) {
          if (m.retType().equals("infer")) {
            String t = inf.inferMethod(m, fieldTypes);
            if (!t.equals("infer")) changed = true;
            methods.add(new MethodDecl(t, m.name(), m.params(), m.body(), m.mods()));
          } else {
            methods.add(m);
          }
        }
        nextInst.add(new InstanceClassDecl(c.name(), c.iface(), c.fields(), c.ctor(), methods));
      }
      instanceClasses = nextInst;
      if (!changed) break;
    }
    for (ClassDecl c : classes) {
      requireResolved(c.name(), c.methods());
    }
    for (ActorDecl a : actors) {
      requireResolved(a.name(), a.methods());
    }
    for (InstanceClassDecl c : instanceClasses) {
      requireResolved(c.name(), c.methods());
    }
    return new Program(p.imports(), classes, p.records(), actors, p.enums(),
        p.application(), p.exceptions(), p.interfaces(), instanceClasses, p.tests(), p.sealeds());
  }

  private static void requireResolved(String owner, List<MethodDecl> methods) {
    for (MethodDecl m : methods) {
      if (m.retType().equals("infer")) {
        throw new CompileError(owner + "." + m.name()
            + ": cannot infer return type — annotate it with '-> T'");
      }
    }
  }

  /** "void" if the body returns no value; the returned expression's type otherwise;
   *  "infer" (unresolved) if a value is returned but cannot be typed yet. */
  private String inferMethod(MethodDecl m, Map<String, String> fieldTypes) {
    env = new HashMap<>(fieldTypes);
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
        case Ast.DestructureStmt v -> {
          List<String> ts = tupleElems(typeOf(v.init()));
          for (int i = 0; i < v.names().size(); i++) {
            if (!v.types().isEmpty()) env.put(v.names().get(i), v.types().get(i));
            else if (i < ts.size()) env.put(v.names().get(i), ts.get(i));
          }
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
      case NullLit x -> null;
      case StrLit x -> "String";
      case VarRef x -> env.get(x.name());
      case NewExpr x -> x.typeName();
      case Ast.ListLit x -> {
        if (x.explicitType() != null) yield x.explicitType();
        if (x.elems().isEmpty()) yield "List";
        String et = typeOf(x.elems().get(0));
        if (et == null) yield "List";
        for (int i = 1; i < x.elems().size(); i++) {
          if (!et.equals(typeOf(x.elems().get(i)))) yield "List";
        }
        yield "List<" + et + ">";
      }
      case Ast.MapLit x -> {
        if (x.explicitType() != null) yield x.explicitType();
        if (x.keys().isEmpty()) yield "HashMap";
        String k = typeOf(x.keys().get(0)), v = typeOf(x.values().get(0));
        if (k == null || v == null) yield "HashMap";
        for (int i = 1; i < x.keys().size(); i++) {
          if (!k.equals(typeOf(x.keys().get(i))) || !v.equals(typeOf(x.values().get(i)))) {
            yield "HashMap";
          }
        }
        yield "HashMap<" + k + "," + v + ">";
      }
      case Ast.Cast x -> x.type().equals("double") ? "double" : "int";
      case Ast.TupleLit x -> {
        var types = new java.util.ArrayList<String>();
        for (Expr el : x.elems()) {
          String t = typeOf(el);
          if (t == null) yield null;
          types.add(t);
        }
        yield "(" + String.join(",", types) + ")";
      }
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

  private static List<String> tupleElems(String t) {
    if (t == null || !t.startsWith("(") || !t.endsWith(")")) return List.of();
    var out = new java.util.ArrayList<String>();
    String inner = t.substring(1, t.length() - 1);
    int depthAngle = 0, depthTuple = 0, start = 0;
    for (int i = 0; i < inner.length(); i++) {
      char c = inner.charAt(i);
      if (c == '<') depthAngle++;
      else if (c == '>') depthAngle--;
      else if (c == '(') depthTuple++;
      else if (c == ')') depthTuple--;
      else if (c == ',' && depthAngle == 0 && depthTuple == 0) {
        out.add(inner.substring(start, i).trim());
        start = i + 1;
      }
    }
    out.add(inner.substring(start).trim());
    return out;
  }
}
