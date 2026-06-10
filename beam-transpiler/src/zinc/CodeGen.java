package zinc;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import zinc.Ast.*;

/**
 * Lowers the imperative AST to Erlang module sources (module name -> source).
 * A program with no actors produces a single 'main' module; actors will add
 * one gen_server module each plus a supervisor (Phase 1).
 * Lowerings (validated in beam-lab/LOWERING_SPEC.md):
 *  - mutable locals -> SSA; mutated-across-loop -> threaded accumulators
 *  - for/while -> direct tail-recursive helper (free vars captured, mutated threaded)
 *  - if that mutates -> case returning a tuple of the mutated vars (phi)
 *  - early return -> try / throw({'$ret',V}) / catch
 *  - break/continue -> loop-scoped throw({'$brk'|'$cont', MutTuple}) caught by the helper
 *  - struct -> map; field read -> maps:get; field set -> functional map update
 *  - array -> list; index read -> lists:nth; len -> length
 *  - string -> binary; interpolation -> '$fmt'-formatted binary segments
 */
class CodeGen {
  private final String moduleName;
  private final List<FnDecl> fns;
  /** erlang module name -> set of "fn/arity" it defines (whole project). */
  private final Map<String, Set<String>> moduleFns;
  /** import alias -> erlang module name. */
  private final Map<String, String> imports = new LinkedHashMap<>();
  private int ctr = 0;
  private final List<String> helpers = new ArrayList<>();

  CodeGen(String moduleName, Program program, Map<String, Set<String>> moduleFns) {
    this.moduleName = moduleName;
    this.fns = program.fns();
    this.moduleFns = moduleFns;
    for (Import im : program.imports()) {
      if (im.erlMod().equals("main") || !moduleFns.containsKey(im.erlMod())) {
        throw new CompileError("unknown module: import " + im.display()
            + " (no " + im.display() + ".src in the project)");
      }
      if (imports.put(im.alias(), im.erlMod()) != null) {
        throw new CompileError("duplicate import alias '" + im.alias() + "'");
      }
    }
  }

  private static final String FMT_HELPER =
      "'$fmt'(X) when is_binary(X) -> X;\n"
          + "'$fmt'(X) when is_integer(X) -> integer_to_binary(X);\n"
          + "'$fmt'(X) -> iolist_to_binary(io_lib:format(\"~p\", [X])).";

  private String fresh(String base) {
    String cap = base.isEmpty() ? "V" : Character.toUpperCase(base.charAt(0)) + base.substring(1);
    return cap + "_" + (ctr++);
  }

  /** `main` is renamed only in the entry module, where the generated main/0 wraps it. */
  private String fnName(String src) {
    return (moduleName.equals("main") && src.equals("main")) ? "user_main" : src;
  }

  String generate() {
    var defs = new ArrayList<String>();
    for (var fn : fns) defs.add(genFn(fn));
    var pieces = new ArrayList<>(defs);
    pieces.addAll(helpers);
    pieces.add(FMT_HELPER);
    String body = String.join("\n\n", pieces);
    if (moduleName.equals("main")) {
      return "-module(main).\n"
          + "-export([main/0]).\n"
          + "-compile([nowarn_unused_vars, nowarn_unused_function]).\n\n"
          + "main() -> user_main().\n\n" + body + "\n";
    }
    var exports = new ArrayList<String>();
    for (var fn : fns) exports.add(fn.name() + "/" + fn.params().size());
    return "-module(" + moduleName + ").\n"
        + "-export([" + String.join(", ", exports) + "]).\n"
        + "-compile([nowarn_unused_vars, nowarn_unused_function]).\n\n"
        + body + "\n";
  }

  private String genFn(FnDecl fn) {
    var env = new HashMap<String, String>();
    var params = new ArrayList<String>();
    for (String p : fn.params()) {
      String v = fresh(p);
      env.put(p, v);
      params.add(v);
    }
    List<String> stmts = genStmts(fn.body().stmts(), env, true, null);
    String bodyStr = String.join(",\n        ", stmts);
    String head = fnName(fn.name()) + "(" + String.join(", ", params) + ")";
    if (needsThrow(fn.body())) {
      return head + " ->\n    try\n        " + bodyStr
          + "\n    catch throw:{'$ret', V} -> V end.";
    }
    return head + " ->\n        " + bodyStr + ".";
  }

  private List<String> genStmts(List<Stmt> stmts, Map<String, String> env,
      boolean topLevel, List<String> loopMut) {
    var out = new ArrayList<String>();
    for (int i = 0; i < stmts.size(); i++) {
      Stmt s = stmts.get(i);
      boolean last = i == stmts.size() - 1;
      switch (s) {
        case VarStmt st -> {
          String v = fresh(st.name());
          out.add(v + " = " + genExpr(st.init(), env));
          env.put(st.name(), v);
        }
        case AssignStmt st -> {
          String cur = envGet(env, st.name());
          String rhs = switch (st.op()) {
            case "=" -> genExpr(st.value(), env);
            case "+=" -> cur + " + " + genExpr(st.value(), env);
            case "-=" -> cur + " - " + genExpr(st.value(), env);
            case "*=" -> cur + " * " + genExpr(st.value(), env);
            default -> throw new CompileError("bad assign op " + st.op());
          };
          String v = fresh(st.name());
          out.add(v + " = " + rhs);
          env.put(st.name(), v);
        }
        case FieldAssignStmt st -> {
          String cur = envGet(env, st.objVar());
          String fe = switch (st.op()) {
            case "=" -> genExpr(st.value(), env);
            case "+=" -> "maps:get(" + st.field() + ", " + cur + ") + " + genExpr(st.value(), env);
            case "-=" -> "maps:get(" + st.field() + ", " + cur + ") - " + genExpr(st.value(), env);
            case "*=" -> "maps:get(" + st.field() + ", " + cur + ") * " + genExpr(st.value(), env);
            default -> throw new CompileError("bad assign op " + st.op());
          };
          String v = fresh(st.objVar());
          out.add(v + " = " + cur + "#{" + st.field() + " := " + fe + "}");
          env.put(st.objVar(), v);
        }
        case ReturnStmt st -> {
          String e = st.value() == null ? "ok" : genExpr(st.value(), env);
          out.add((topLevel && last) ? e : "throw({'$ret', " + e + "})");
        }
        case BreakStmt st -> {
          if (loopMut == null) throw new CompileError("break outside loop");
          out.add("throw({'$brk', " + loopTuple(loopMut, env) + "})");
        }
        case ContinueStmt st -> {
          if (loopMut == null) throw new CompileError("continue outside loop");
          out.add("throw({'$cont', " + loopTuple(loopMut, env) + "})");
        }
        case ExprStmt st -> out.add(genExpr(st.expr(), env));
        case IfStmt st -> out.add(genIf(st, env, loopMut));
        case ForRangeStmt st -> out.add(genForRange(st, env));
        case ForEachStmt st -> out.add(genForEach(st, env));
        case WhileStmt st -> out.add(genWhile(st, env));
      }
    }
    return out;
  }

  private static String envGet(Map<String, String> env, String name) {
    String v = env.get(name);
    if (v == null) throw new CompileError("undefined variable: " + name);
    return v;
  }

  private String loopTuple(List<String> mut, Map<String, String> env) {
    var vals = new ArrayList<String>();
    for (String m : mut) vals.add(envGet(env, m));
    return tupleOf(vals);
  }

  private String genIf(IfStmt s, Map<String, String> env, List<String> loopMut) {
    String cond = genExpr(s.cond(), env);
    var assigned = new LinkedHashSet<String>();
    collectAssigned(s.thenBlock(), assigned);
    if (s.elseBlock() != null) collectAssigned(s.elseBlock(), assigned);
    var phi = assigned.stream().filter(env::containsKey).toList();

    var thenEnv = new HashMap<>(env);
    List<String> thenCode = genStmts(s.thenBlock().stmts(), thenEnv, false, loopMut);
    boolean thenJump = endsInJump(s.thenBlock());

    List<String> elseCode;
    Map<String, String> elseEnv;
    boolean elseJump;
    if (s.elseBlock() != null) {
      elseEnv = new HashMap<>(env);
      elseCode = genStmts(s.elseBlock().stmts(), elseEnv, false, loopMut);
      elseJump = endsInJump(s.elseBlock());
    } else {
      elseEnv = env;
      elseCode = List.of();
      elseJump = false;
    }

    if (phi.isEmpty()) {
      String t = thenCode.isEmpty() ? "ok" : String.join(", ", thenCode);
      String e = elseCode.isEmpty() ? "ok" : String.join(", ", elseCode);
      return "case " + cond + " of true -> " + t + "; false -> " + e + " end";
    }

    String tArm = ifArm(thenCode, thenEnv, thenJump, phi);
    String eArm = ifArm(elseCode, elseEnv, elseJump, phi);
    var newNames = new ArrayList<String>();
    for (String v : phi) newNames.add(fresh(v));
    String lhs = newNames.size() == 1 ? newNames.get(0) : "{" + String.join(", ", newNames) + "}";
    for (int i = 0; i < phi.size(); i++) {
      env.put(phi.get(i), newNames.get(i));
    }
    return lhs + " = case " + cond + " of true -> " + tArm + "; false -> " + eArm + " end";
  }

  private String ifArm(List<String> code, Map<String, String> benv, boolean jump,
      List<String> phi) {
    if (jump) return String.join(", ", code);
    var vals = new ArrayList<String>();
    for (String v : phi) vals.add(envGet(benv, v));
    String tup = vals.size() == 1 ? vals.get(0) : "{" + String.join(", ", vals) + "}";
    var all = new ArrayList<>(code);
    all.add(tup);
    return String.join(", ", all);
  }

  private String genForRange(ForRangeStmt s, Map<String, String> env) {
    String startCode = genExpr(s.start(), env);
    String endCode = genExpr(s.end(), env);
    List<String> mut = mutated(s.body(), env);
    var exclude = new LinkedHashSet<String>();
    exclude.add(s.varName());
    exclude.addAll(mut);
    List<String> free = freeVars(s.body(), env, exclude);

    String helper = "loop_" + (ctr++);
    String iVar = fresh(s.varName());
    String endVar = fresh("end");
    var freeIn = new LinkedHashMap<String, String>();
    for (String f : free) freeIn.put(f, fresh(f));
    var mutIn = new LinkedHashMap<String, String>();
    for (String m : mut) mutIn.put(m, fresh(m));

    var benv = new HashMap<String, String>();
    benv.put(s.varName(), iVar);
    benv.putAll(freeIn);
    benv.putAll(mutIn);
    List<String> bodyCode = genStmts(s.body().stmts(), benv, false, mut);
    var mutOut = new ArrayList<String>();
    for (String m : mut) mutOut.add(benv.get(m));

    var freeP = new ArrayList<String>();
    for (String f : free) freeP.add(freeIn.get(f));
    var head1 = new ArrayList<String>();
    head1.add(iVar);
    head1.add(endVar);
    head1.addAll(freeP);
    for (String m : mut) head1.add(mutIn.get(m));
    var recursePrefix = new ArrayList<String>();
    recursePrefix.add(iVar + " + 1");
    recursePrefix.add(endVar);
    recursePrefix.addAll(freeP);
    var base = new ArrayList<String>();
    base.add("_" + iVar);
    base.add("_" + endVar);
    for (String f : free) base.add("_" + freeIn.get(f));
    for (String m : mut) base.add(mutIn.get(m));
    var resultVals = new ArrayList<String>();
    for (String m : mut) resultVals.add(mutIn.get(m));
    String result = tupleOf(resultVals);
    String clauseBody = loopClauseBody(s.body(), bodyCode, mutOut, mut, recursePrefix, helper);
    helpers.add(helper + "(" + String.join(", ", head1) + ") when " + iVar + " < " + endVar
        + " ->\n        " + clauseBody + ";\n"
        + helper + "(" + String.join(", ", base) + ") ->\n        " + result + ".");

    var callArgs = new ArrayList<String>();
    callArgs.add(startCode);
    callArgs.add(endCode);
    for (String f : free) callArgs.add(env.get(f));
    for (String m : mut) callArgs.add(env.get(m));
    return bindLoop(helper + "(" + String.join(", ", callArgs) + ")", mut, env);
  }

  private String genForEach(ForEachStmt s, Map<String, String> env) {
    String listCode = genExpr(s.iterable(), env);
    List<String> mut = mutated(s.body(), env);
    var exclude = new LinkedHashSet<String>();
    exclude.add(s.varName());
    exclude.addAll(mut);
    List<String> free = freeVars(s.body(), env, exclude);

    String helper = "loop_" + (ctr++);
    String elemVar = fresh(s.varName());
    String restVar = fresh("rest");
    var freeIn = new LinkedHashMap<String, String>();
    for (String f : free) freeIn.put(f, fresh(f));
    var mutIn = new LinkedHashMap<String, String>();
    for (String m : mut) mutIn.put(m, fresh(m));

    var benv = new HashMap<String, String>();
    benv.put(s.varName(), elemVar);
    benv.putAll(freeIn);
    benv.putAll(mutIn);
    List<String> bodyCode = genStmts(s.body().stmts(), benv, false, mut);
    var mutOut = new ArrayList<String>();
    for (String m : mut) mutOut.add(benv.get(m));

    var freeP = new ArrayList<String>();
    for (String f : free) freeP.add(freeIn.get(f));
    var head1 = new ArrayList<String>();
    head1.add("[" + elemVar + " | " + restVar + "]");
    head1.addAll(freeP);
    for (String m : mut) head1.add(mutIn.get(m));
    var recursePrefix = new ArrayList<String>();
    recursePrefix.add(restVar);
    recursePrefix.addAll(freeP);
    var base = new ArrayList<String>();
    base.add("[]");
    for (String f : free) base.add("_" + freeIn.get(f));
    for (String m : mut) base.add(mutIn.get(m));
    var resultVals = new ArrayList<String>();
    for (String m : mut) resultVals.add(mutIn.get(m));
    String result = tupleOf(resultVals);
    String clauseBody = loopClauseBody(s.body(), bodyCode, mutOut, mut, recursePrefix, helper);
    helpers.add(helper + "(" + String.join(", ", head1) + ") ->\n        " + clauseBody + ";\n"
        + helper + "(" + String.join(", ", base) + ") ->\n        " + result + ".");

    var callArgs = new ArrayList<String>();
    callArgs.add(listCode);
    for (String f : free) callArgs.add(env.get(f));
    for (String m : mut) callArgs.add(env.get(m));
    return bindLoop(helper + "(" + String.join(", ", callArgs) + ")", mut, env);
  }

  private String genWhile(WhileStmt s, Map<String, String> env) {
    List<String> mut = mutated(s.body(), env);
    var refs = new LinkedHashSet<String>();
    exprRefs(s.cond(), refs);
    blockRefs(s.body(), refs);
    var free = refs.stream().filter(v -> env.containsKey(v) && !mut.contains(v)).toList();

    String helper = "loop_" + (ctr++);
    var freeIn = new LinkedHashMap<String, String>();
    for (String f : free) freeIn.put(f, fresh(f));
    var mutIn = new LinkedHashMap<String, String>();
    for (String m : mut) mutIn.put(m, fresh(m));
    var benv = new HashMap<String, String>();
    benv.putAll(freeIn);
    benv.putAll(mutIn);
    String condCode = genExpr(s.cond(), benv);
    List<String> bodyCode = genStmts(s.body().stmts(), benv, false, mut);
    var mutOut = new ArrayList<String>();
    for (String m : mut) mutOut.add(benv.get(m));

    var freeP = new ArrayList<String>();
    for (String f : free) freeP.add(freeIn.get(f));
    var head1 = new ArrayList<String>(freeP);
    for (String m : mut) head1.add(mutIn.get(m));
    var recursePrefix = new ArrayList<String>(freeP);
    var resultVals = new ArrayList<String>();
    for (String m : mut) resultVals.add(mutIn.get(m));
    String result = tupleOf(resultVals);
    String clauseBody = loopClauseBody(s.body(), bodyCode, mutOut, mut, recursePrefix, helper);
    helpers.add(helper + "(" + String.join(", ", head1) + ") ->\n"
        + "        case " + condCode + " of\n"
        + "            true -> " + clauseBody + ";\n"
        + "            false -> " + result + "\n"
        + "        end.");

    var callArgs = new ArrayList<String>();
    for (String f : free) callArgs.add(env.get(f));
    for (String m : mut) callArgs.add(env.get(m));
    return bindLoop(helper + "(" + String.join(", ", callArgs) + ")", mut, env);
  }

  /**
   * The per-iteration body of a loop helper: either a simple "run body, recurse"
   * or, if the body has break/continue, a try that catches the loop signals.
   */
  private String loopClauseBody(Block body, List<String> bodyCode, List<String> mutOut,
      List<String> mut, List<String> recursePrefix, String helper) {
    if (!hasBreakContinue(body)) {
      var recArgs = new ArrayList<>(recursePrefix);
      recArgs.addAll(mutOut);
      String rec = helper + "(" + String.join(", ", recArgs) + ")";
      var all = new ArrayList<>(bodyCode);
      all.add(rec);
      return String.join(",\n        ", all);
    }
    String sig = fresh("sig");
    String payload = tupleOf(mutOut);
    var pm = new ArrayList<String>();
    for (String m : mut) pm.add(fresh(m));
    String pat = pm.isEmpty() ? "ok" : (pm.size() == 1 ? pm.get(0) : "{" + String.join(", ", pm) + "}");
    var recArgs = new ArrayList<>(recursePrefix);
    recArgs.addAll(pm);
    String recurse = helper + "(" + String.join(", ", recArgs) + ")";
    String joined = String.join(",\n            ", bodyCode);
    return sig + " = try\n            " + joined + ",\n            {'$cont', " + payload + "}\n"
        + "        catch throw:{'$cont', M} -> {'$cont', M}; throw:{'$brk', M} -> {'$brk', M} end,\n"
        + "        case " + sig + " of {'$cont', " + pat + "} -> " + recurse
        + "; {'$brk', " + pat + "} -> " + pat + " end";
  }

  private String tupleOf(List<String> xs) {
    if (xs.isEmpty()) return "ok";
    if (xs.size() == 1) return xs.get(0);
    return "{" + String.join(", ", xs) + "}";
  }

  private String bindLoop(String call, List<String> mut, Map<String, String> env) {
    if (mut.isEmpty()) return call;
    var newNames = new ArrayList<String>();
    for (String m : mut) newNames.add(fresh(m));
    String lhs = newNames.size() == 1 ? newNames.get(0) : "{" + String.join(", ", newNames) + "}";
    for (int i = 0; i < mut.size(); i++) {
      env.put(mut.get(i), newNames.get(i));
    }
    return lhs + " = " + call;
  }

  private String genExpr(Expr e, Map<String, String> env) {
    return switch (e) {
      case IntLit x -> String.valueOf(x.value());
      case FloatLit x -> String.valueOf(x.value());
      case BoolLit x -> x.value() ? "true" : "false";
      case VarRef x -> envGet(env, x.name());
      case ListLit x -> {
        var elems = new ArrayList<String>();
        for (Expr el : x.elems()) elems.add(genExpr(el, env));
        yield "[" + String.join(", ", elems) + "]";
      }
      case StructLit x -> {
        var entries = new ArrayList<String>();
        for (FieldInit f : x.fields()) entries.add(f.name() + " => " + genExpr(f.value(), env));
        yield "#{" + String.join(", ", entries) + "}";
      }
      case FieldAccess x -> "maps:get(" + x.field() + ", " + genExpr(x.obj(), env) + ")";
      case Index x -> "lists:nth((" + genExpr(x.index(), env) + ") + 1, " + genExpr(x.obj(), env) + ")";
      case Unary x -> {
        String inner = genExpr(x.operand(), env);
        yield x.op().equals("!") ? "(not " + inner + ")" : "(" + x.op() + inner + ")";
      }
      case Binary x -> "(" + genExpr(x.left(), env) + " " + erlOp(x.op()) + " "
          + genExpr(x.right(), env) + ")";
      case Call x -> {
        if (x.callee().equals("print")) {
          yield "io:format(\"~p~n\", [" + genExpr(x.args().get(0), env) + "])";
        }
        if (x.callee().equals("println")) {
          yield "io:format(\"~ts~n\", [" + genExpr(x.args().get(0), env) + "])";
        }
        if (x.callee().equals("len")) {
          yield "length(" + genExpr(x.args().get(0), env) + ")";
        }
        var args = new ArrayList<String>();
        for (Expr a : x.args()) args.add(genExpr(a, env));
        yield fnName(x.callee()) + "(" + String.join(", ", args) + ")";
      }
      case MethodCall x -> {
        if (!(x.target() instanceof VarRef vr)) {
          throw new CompileError("method call target must be an imported module alias");
        }
        if (env.containsKey(vr.name()) || !imports.containsKey(vr.name())) {
          throw new CompileError("method calls are only supported on imported modules, '"
              + vr.name() + "' is not an import (actors come in Phase 1)");
        }
        String mod = imports.get(vr.name());
        String key = x.method() + "/" + x.args().size();
        if (!moduleFns.get(mod).contains(key)) {
          throw new CompileError("module '" + vr.name() + "' has no function " + key);
        }
        var args = new ArrayList<String>();
        for (Expr a : x.args()) args.add(genExpr(a, env));
        yield mod + ":" + x.method() + "(" + String.join(", ", args) + ")";
      }
      case StrLit x -> {
        if (x.parts().isEmpty()) yield "<<>>";
        var segs = new ArrayList<String>();
        for (StrPart p : x.parts()) {
          segs.add(switch (p) {
            case StrText t -> "\"" + escErl(t.text()) + "\"/utf8";
            case StrExpr ex -> "('$fmt'(" + genExpr(ex.expr(), env) + "))/binary";
          });
        }
        yield "<<" + String.join(", ", segs) + ">>";
      }
    };
  }

  private static String escErl(String s) {
    return s.replace("\\", "\\\\").replace("\"", "\\\"");
  }

  private static String erlOp(String op) {
    return switch (op) {
      case "+" -> "+";
      case "-" -> "-";
      case "*" -> "*";
      case "/" -> "/";
      case "%" -> "rem";
      case "&&" -> "andalso";
      case "||" -> "orelse";
      case "==" -> "=:=";
      case "!=" -> "=/=";
      case "<" -> "<";
      case ">" -> ">";
      case "<=" -> "=<";
      case ">=" -> ">=";
      default -> throw new CompileError("bad op " + op);
    };
  }

  // ---- analysis ----
  private boolean needsThrow(Block b) {
    List<Stmt> stmts = b.stmts();
    for (int i = 0; i < stmts.size(); i++) {
      Stmt s = stmts.get(i);
      if (s instanceof ReturnStmt && i != stmts.size() - 1) return true;
      if (s instanceof IfStmt it
          && (hasReturn(it.thenBlock()) || (it.elseBlock() != null && hasReturn(it.elseBlock())))) {
        return true;
      }
      if (s instanceof ForRangeStmt it && hasReturn(it.body())) return true;
      if (s instanceof ForEachStmt it && hasReturn(it.body())) return true;
      if (s instanceof WhileStmt it && hasReturn(it.body())) return true;
    }
    return false;
  }

  private boolean hasReturn(Block b) {
    for (Stmt s : b.stmts()) {
      if (s instanceof ReturnStmt) return true;
      if (s instanceof IfStmt it
          && (hasReturn(it.thenBlock()) || (it.elseBlock() != null && hasReturn(it.elseBlock())))) {
        return true;
      }
      if (s instanceof ForRangeStmt it && hasReturn(it.body())) return true;
      if (s instanceof ForEachStmt it && hasReturn(it.body())) return true;
      if (s instanceof WhileStmt it && hasReturn(it.body())) return true;
    }
    return false;
  }

  /** Break/continue belonging to THIS loop (does not descend into nested loops). */
  private boolean hasBreakContinue(Block b) {
    for (Stmt s : b.stmts()) {
      if (s instanceof BreakStmt || s instanceof ContinueStmt) return true;
      if (s instanceof IfStmt it
          && (hasBreakContinue(it.thenBlock())
              || (it.elseBlock() != null && hasBreakContinue(it.elseBlock())))) {
        return true;
      }
    }
    return false;
  }

  private boolean endsInJump(Block b) {
    if (b.stmts().isEmpty()) return false;
    Stmt l = b.stmts().get(b.stmts().size() - 1);
    return l instanceof ReturnStmt || l instanceof BreakStmt || l instanceof ContinueStmt;
  }

  private List<String> mutated(Block b, Map<String, String> env) {
    var a = new LinkedHashSet<String>();
    collectAssigned(b, a);
    return a.stream().filter(env::containsKey).toList();
  }

  private void collectAssigned(Block b, Set<String> out) {
    for (Stmt s : b.stmts()) {
      switch (s) {
        case AssignStmt st -> out.add(st.name());
        case FieldAssignStmt st -> out.add(st.objVar());
        case IfStmt st -> {
          collectAssigned(st.thenBlock(), out);
          if (st.elseBlock() != null) collectAssigned(st.elseBlock(), out);
        }
        case ForRangeStmt st -> collectAssigned(st.body(), out);
        case ForEachStmt st -> collectAssigned(st.body(), out);
        case WhileStmt st -> collectAssigned(st.body(), out);
        case VarStmt st -> {}
        case ReturnStmt st -> {}
        case ExprStmt st -> {}
        case BreakStmt st -> {}
        case ContinueStmt st -> {}
      }
    }
  }

  private List<String> freeVars(Block b, Map<String, String> env, Set<String> exclude) {
    var refs = new LinkedHashSet<String>();
    blockRefs(b, refs);
    return refs.stream().filter(v -> env.containsKey(v) && !exclude.contains(v)).toList();
  }

  private void blockRefs(Block b, Set<String> out) {
    for (Stmt s : b.stmts()) {
      switch (s) {
        case VarStmt st -> exprRefs(st.init(), out);
        case AssignStmt st -> {
          if (!st.op().equals("=")) out.add(st.name());
          exprRefs(st.value(), out);
        }
        case FieldAssignStmt st -> {
          out.add(st.objVar());
          exprRefs(st.value(), out);
        }
        case ReturnStmt st -> {
          if (st.value() != null) exprRefs(st.value(), out);
        }
        case ExprStmt st -> exprRefs(st.expr(), out);
        case IfStmt st -> {
          exprRefs(st.cond(), out);
          blockRefs(st.thenBlock(), out);
          if (st.elseBlock() != null) blockRefs(st.elseBlock(), out);
        }
        case ForRangeStmt st -> {
          exprRefs(st.start(), out);
          exprRefs(st.end(), out);
          blockRefs(st.body(), out);
        }
        case ForEachStmt st -> {
          exprRefs(st.iterable(), out);
          blockRefs(st.body(), out);
        }
        case WhileStmt st -> {
          exprRefs(st.cond(), out);
          blockRefs(st.body(), out);
        }
        case BreakStmt st -> {}
        case ContinueStmt st -> {}
      }
    }
  }

  private void exprRefs(Expr e, Set<String> out) {
    switch (e) {
      case IntLit x -> {}
      case FloatLit x -> {}
      case BoolLit x -> {}
      case VarRef x -> out.add(x.name());
      case ListLit x -> {
        for (Expr el : x.elems()) exprRefs(el, out);
      }
      case StructLit x -> {
        for (FieldInit f : x.fields()) exprRefs(f.value(), out);
      }
      case FieldAccess x -> exprRefs(x.obj(), out);
      case Index x -> {
        exprRefs(x.obj(), out);
        exprRefs(x.index(), out);
      }
      case Unary x -> exprRefs(x.operand(), out);
      case Binary x -> {
        exprRefs(x.left(), out);
        exprRefs(x.right(), out);
      }
      case Call x -> {
        for (Expr a : x.args()) exprRefs(a, out);
      }
      case MethodCall x -> {
        // target stays out: a module alias is not a data dependency to thread through loops
        for (Expr a : x.args()) exprRefs(a, out);
      }
      case StrLit x -> {
        for (StrPart p : x.parts()) {
          if (p instanceof StrExpr se) exprRefs(se.expr(), out);
        }
      }
    }
  }
}
