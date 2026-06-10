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
 * Lowers one source file (classes, records, actors) to Erlang modules.
 * Class -> module of functions; record -> map (new -> literal, accessor -> maps:get);
 * actor -> gen_server module (void method = cast, typed = call), one project actor_sup.
 * Core lowerings unchanged from the validated set: SSA locals, loops -> tail recursion,
 * if-phi, early return via throw, break/continue via loop-scoped throw.
 */
class CodeGen {
  record ClassInfo(String module, Map<String, String> methods) {} // "name/arity" -> retType

  private final Program program;
  private final Map<String, ClassInfo> classes;   // project-wide, by class name
  private final Map<String, RecordDecl> records;  // project-wide, by record name
  private final Map<String, ActorDecl> actors = new LinkedHashMap<>(); // this file's
  private final Map<String, String> ffi = new LinkedHashMap<>(); // alias -> erlang module
  private final boolean projectHasActors;

  private String curModule;
  private String curClassName;
  private boolean inActor = false;
  private Map<String, ActorDecl> handleTypes = new HashMap<>(); // var -> spawned actor
  private Map<String, String> varTypes = new HashMap<>();       // var -> type, per method
  private int ctr = 0;
  private List<String> helpers = new ArrayList<>();

  CodeGen(Program program, Map<String, ClassInfo> classes, Map<String, RecordDecl> records,
      boolean projectHasActors) {
    this.program = program;
    this.classes = classes;
    this.records = records;
    this.projectHasActors = projectHasActors;
    for (Import im : program.imports()) {
      // import erlang.<module>; -> FFI binding to that Erlang module, calls pass through
      if (im.path().size() == 2 && im.path().get(0).equals("erlang")) {
        String mod = im.className();
        if (classes.containsKey(mod)) {
          throw new CompileError("FFI import " + im.display() + " collides with class " + mod);
        }
        ffi.put(mod, mod);
        continue;
      }
      if (!classes.containsKey(im.className())) {
        throw new CompileError("unknown import: " + im.display()
            + " (no class " + im.className() + " in the project)");
      }
    }
    for (ActorDecl a : program.actors()) {
      actors.put(a.name(), a);
      var seen = new LinkedHashSet<String>();
      for (MethodDecl m : a.methods()) {
        if (!seen.add(m.name() + "/" + m.params().size())) {
          throw new CompileError("actor " + a.name() + ": duplicate method "
              + m.name() + "/" + m.params().size());
        }
      }
    }
  }

  private static final String FMT_HELPER =
      "'$fmt'(X) when is_binary(X) -> X;\n"
          + "'$fmt'(X) when is_integer(X) -> integer_to_binary(X);\n"
          + "'$fmt'(X) -> iolist_to_binary(io_lib:format(\"~p\", [X])).";

  static final String SUP_SOURCE = "-module(actor_sup).\n"
      + "-behaviour(supervisor).\n"
      + "-export([start_link/0, spawn_child/1, init/1]).\n\n"
      + "start_link() -> supervisor:start_link({local, actor_sup}, ?MODULE, []).\n\n"
      + "spawn_child(Mod) ->\n"
      + "    N = erlang:unique_integer([positive]),\n"
      + "    Name = list_to_atom(atom_to_list(Mod) ++ \"_\" ++ integer_to_list(N)),\n"
      + "    {ok, _} = supervisor:start_child(actor_sup,\n"
      + "        #{id => Name, start => {Mod, start_link, [Name]}, restart => permanent,\n"
      + "          shutdown => 5000, type => worker, modules => [Mod]}),\n"
      + "    Name.\n\n"
      + "init([]) ->\n"
      + "    {ok, {#{strategy => one_for_one, intensity => 1000, period => 3600}, []}}.\n";

  private String fresh(String base) {
    String cap = base.isEmpty() ? "V" : Character.toUpperCase(base.charAt(0)) + base.substring(1);
    return cap + "_" + (ctr++);
  }

  /** main is renamed in class Main, whose generated main/0 wraps it. */
  private String fnName(String src) {
    return ("main".equals(curModule) && src.equals("main")) ? "user_main" : src;
  }

  Map<String, String> generateAll() {
    var out = new LinkedHashMap<String, String>();
    for (ClassDecl c : program.classes()) {
      helpers = new ArrayList<>();
      curModule = c.erlMod();
      curClassName = c.name();
      out.put(c.erlMod(), genClassModule(c));
    }
    for (ActorDecl a : actors.values()) {
      helpers = new ArrayList<>();
      curModule = a.erlMod();
      curClassName = null;
      inActor = true;
      out.put(a.erlMod(), genActorModule(a));
      inActor = false;
    }
    return out;
  }

  private String genClassModule(ClassDecl c) {
    var defs = new ArrayList<String>();
    for (var m : c.methods()) defs.add(genFn(m));
    var pieces = new ArrayList<String>();
    var exports = new ArrayList<String>();
    boolean isMain = c.erlMod().equals("main");
    if (isMain) {
      exports.add("main/0");
      pieces.add(projectHasActors
          ? "main() ->\n"
              + "    logger:set_primary_config(level, none),\n"
              + "    {ok, _} = actor_sup:start_link(),\n"
              + "    user_main([])."
          : "main() -> user_main([]).");
    }
    for (var m : c.methods()) {
      String n = isMain && m.name().equals("main") ? "user_main" : m.name();
      exports.add(n + "/" + m.params().size());
    }
    pieces.addAll(defs);
    pieces.addAll(helpers);
    pieces.add(FMT_HELPER);
    return "-module(" + c.erlMod() + ").\n"
        + "-export([" + String.join(", ", exports) + "]).\n"
        + "-compile([nowarn_unused_vars, nowarn_unused_function]).\n\n"
        + String.join("\n\n", pieces) + "\n";
  }

  private String genFn(MethodDecl m) {
    handleTypes = new HashMap<>();
    varTypes = new HashMap<>();
    var env = new HashMap<String, String>();
    var params = new ArrayList<String>();
    for (Param p : m.params()) {
      String v = fresh(p.name());
      env.put(p.name(), v);
      varTypes.put(p.name(), p.type());
      params.add(v);
    }
    List<String> stmts = genStmts(m.body().stmts(), env, true, null);
    if (stmts.isEmpty()) stmts = List.of("ok");
    String bodyStr = String.join(",\n        ", stmts);
    String head = fnName(m.name()) + "(" + String.join(", ", params) + ")";
    if (needsThrow(m.body())) {
      return head + " ->\n    try\n        " + bodyStr
          + "\n    catch throw:{'$ret', V} -> V end.";
    }
    return head + " ->\n        " + bodyStr + ".";
  }

  // ---- actors ----

  private String genActorModule(ActorDecl a) {
    var casts = new ArrayList<String>();
    var calls = new ArrayList<String>();
    for (MethodDecl m : a.methods()) {
      if (m.retType().equals("void")) {
        if (hasReturn(m.body())) {
          throw new CompileError("actor " + a.name() + "." + m.name()
              + ": void methods cannot return a value");
        }
        casts.add(genHandler(a, m, false));
      } else {
        calls.add(genHandler(a, m, true));
      }
    }
    var exports = new ArrayList<>(List.of("start_link/1", "init/1"));
    if (!calls.isEmpty()) exports.add("handle_call/3");
    if (!casts.isEmpty()) exports.add("handle_cast/2");

    var pieces = new ArrayList<String>();
    pieces.add("start_link(Name) -> gen_server:start_link({local, Name}, ?MODULE, [], []).");
    pieces.add(genInit(a));
    // no catch-all clauses: unknown messages crash the actor, the supervisor heals it
    if (!casts.isEmpty()) pieces.add(String.join(";\n", casts) + ".");
    if (!calls.isEmpty()) pieces.add(String.join(";\n", calls) + ".");
    pieces.addAll(helpers);
    pieces.add(FMT_HELPER);
    return "-module(" + a.erlMod() + ").\n"
        + "-behaviour(gen_server).\n"
        + "-export([" + String.join(", ", exports) + "]).\n"
        + "-compile([nowarn_unused_vars, nowarn_unused_function]).\n\n"
        + String.join("\n\n", pieces) + "\n";
  }

  private String genInit(ActorDecl a) {
    handleTypes = new HashMap<>();
    varTypes = new HashMap<>();
    var env = new HashMap<String, String>();
    var lines = new ArrayList<String>();
    for (FieldDecl f : a.fields()) {
      String v = fresh(f.name());
      lines.add(v + " = " + genExpr(f.init(), env));
      env.put(f.name(), v);
      varTypes.put(f.name(), f.type());
    }
    lines.add("{ok, " + stateMap(a, env) + "}");
    return "init([]) ->\n        " + String.join(",\n        ", lines) + ".";
  }

  /** One handle_cast/handle_call clause: fields seeded via maps:get, SSA body, new state map. */
  private String genHandler(ActorDecl a, MethodDecl m, boolean isCall) {
    handleTypes = new HashMap<>();
    varTypes = new HashMap<>();
    var env = new HashMap<String, String>();
    var params = new ArrayList<String>();
    for (Param p : m.params()) {
      String v = fresh(p.name());
      env.put(p.name(), v);
      varTypes.put(p.name(), p.type());
      params.add(v);
    }
    var lines = new ArrayList<String>();
    for (FieldDecl f : a.fields()) {
      String v = fresh(f.name());
      lines.add(v + " = maps:get(" + f.name() + ", State)");
      env.put(f.name(), v);
      varTypes.put(f.name(), f.type());
    }

    var stmts = m.body().stmts();
    if (isCall) {
      boolean lastIsReturn = !stmts.isEmpty()
          && stmts.get(stmts.size() - 1) instanceof ReturnStmt r && r.value() != null;
      if (!lastIsReturn || countReturns(m.body()) != 1) {
        throw new CompileError("actor " + a.name() + "." + m.name()
            + ": 'return' must be the last statement (v1)");
      }
      lines.addAll(genStmts(stmts.subList(0, stmts.size() - 1), env, false, null));
      String reply = genExpr(((ReturnStmt) stmts.get(stmts.size() - 1)).value(), env);
      lines.add("{reply, " + reply + ", " + stateMap(a, env) + "}");
    } else {
      lines.addAll(genStmts(stmts, env, false, null));
      lines.add("{noreply, " + stateMap(a, env) + "}");
    }

    String msg = params.isEmpty() ? "{" + m.name() + "}"
        : "{" + m.name() + ", " + String.join(", ", params) + "}";
    String head = isCall ? "handle_call(" + msg + ", _From, State)"
        : "handle_cast(" + msg + ", State)";
    return head + " ->\n        " + String.join(",\n        ", lines);
  }

  private String stateMap(ActorDecl a, Map<String, String> env) {
    var entries = new ArrayList<String>();
    for (FieldDecl f : a.fields()) entries.add(f.name() + " => " + envGet(env, f.name()));
    return "#{" + String.join(", ", entries) + "}";
  }

  // ---- statements ----

  private List<String> genStmts(List<Stmt> stmts, Map<String, String> env,
      boolean topLevel, List<String> loopMut) {
    var out = new ArrayList<String>();
    for (int i = 0; i < stmts.size(); i++) {
      Stmt s = stmts.get(i);
      boolean last = i == stmts.size() - 1;
      switch (s) {
        case VarStmt st -> {
          String v = fresh(st.name());
          if (st.init() instanceof SpawnExpr sp) {
            ActorDecl a = actors.get(sp.actorName());
            if (a == null) {
              throw new CompileError("unknown actor: " + sp.actorName()
                  + " (actors are file-local in v1)");
            }
            out.add(v + " = actor_sup:spawn_child(" + a.erlMod() + ")");
            handleTypes.put(st.name(), a);
            varTypes.put(st.name(), a.name());
          } else {
            out.add(v + " = " + genExpr(st.init(), env));
            varTypes.put(st.name(),
                st.type().equals("var") ? exprType(st.init()) : st.type());
          }
          env.put(st.name(), v);
        }
        case AssignStmt st -> {
          String cur = envGet(env, st.name());
          String rhs;
          if (st.op().equals("+=") && (isStr(new VarRef(st.name())) || isStr(st.value()))) {
            rhs = "<<('$fmt'(" + cur + "))/binary, " + concatSegs(st.value(), env) + ">>";
          } else {
            rhs = switch (st.op()) {
              case "=" -> genExpr(st.value(), env);
              case "+=" -> cur + " + " + genExpr(st.value(), env);
              case "-=" -> cur + " - " + genExpr(st.value(), env);
              case "*=" -> cur + " * " + genExpr(st.value(), env);
              default -> throw new CompileError("bad assign op " + st.op());
            };
          }
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
        case ExprStmt st -> {
          // map mutation as a statement rebinds the receiver (SSA)
          if (st.expr() instanceof MethodCall mc && mc.target() instanceof VarRef vr
              && "HashMap".equals(varTypes.get(vr.name())) && env.containsKey(vr.name())
              && (mc.method().equals("put") || mc.method().equals("remove"))) {
            String cur = envGet(env, vr.name());
            String rhs = mc.method().equals("put")
                ? "maps:put(" + genExpr(mc.args().get(0), env) + ", "
                    + genExpr(mc.args().get(1), env) + ", " + cur + ")"
                : "maps:remove(" + genExpr(mc.args().get(0), env) + ", " + cur + ")";
            String v = fresh(vr.name());
            out.add(v + " = " + rhs);
            env.put(vr.name(), v);
          } else {
            out.add(genExpr(st.expr(), env));
          }
        }
        case IfStmt st -> out.add(genIf(st, env, loopMut));
        case ForEachStmt st -> out.add(genForEach(st, env));
        case WhileStmt st -> out.add(genWhile(st, env));
        case SeqStmt st -> out.addAll(genStmts(st.stmts(), env, false, loopMut));
        case TryStmt st -> out.add(genTry(st, env, loopMut));
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

  /**
   * Like if: vars assigned in either block phi-merge. catch error:E only — internal
   * control-flow signals ('$ret'/'$brk'/'$cont') are throw-class and pass through.
   */
  private String genTry(TryStmt s, Map<String, String> env, List<String> loopMut) {
    var assigned = new LinkedHashSet<String>();
    collectAssigned(s.tryBlock(), assigned);
    collectAssigned(s.catchBlock(), assigned);
    var phi = assigned.stream().filter(env::containsKey).toList();

    var tEnv = new HashMap<>(env);
    List<String> tCode = genStmts(s.tryBlock().stmts(), tEnv, false, loopMut);
    boolean tJump = endsInJump(s.tryBlock());

    var cEnv = new HashMap<>(env);
    String ev = fresh(s.exVar());
    cEnv.put(s.exVar(), ev);
    List<String> cCode = genStmts(s.catchBlock().stmts(), cEnv, false, loopMut);
    boolean cJump = endsInJump(s.catchBlock());

    if (phi.isEmpty()) {
      String t = tCode.isEmpty() ? "ok" : String.join(", ", tCode);
      String c = cCode.isEmpty() ? "ok" : String.join(", ", cCode);
      return "try " + t + " catch error:" + ev + " -> " + c + " end";
    }
    String tArm = ifArm(tCode, tEnv, tJump, phi);
    String cArm = ifArm(cCode, cEnv, cJump, phi);
    var newNames = new ArrayList<String>();
    for (String v : phi) newNames.add(fresh(v));
    String lhs = newNames.size() == 1 ? newNames.get(0) : "{" + String.join(", ", newNames) + "}";
    for (int i = 0; i < phi.size(); i++) {
      env.put(phi.get(i), newNames.get(i));
    }
    return lhs + " = try " + tArm + " catch error:" + ev + " -> " + cArm + " end";
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
    if (!s.varType().equals("var")) varTypes.put(s.varName(), s.varType());
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

  // ---- expressions ----

  private String genExpr(Expr e, Map<String, String> env) {
    return switch (e) {
      case IntLit x -> String.valueOf(x.value());
      case FloatLit x -> String.valueOf(x.value());
      case BoolLit x -> x.value() ? "true" : "false";
      case StrLit x -> "<<\"" + escErl(x.text()) + "\"/utf8>>";
      case VarRef x -> envGet(env, x.name());
      case ListLit x -> {
        var elems = new ArrayList<String>();
        for (Expr el : x.elems()) elems.add(genExpr(el, env));
        yield "[" + String.join(", ", elems) + "]";
      }
      case NewExpr x -> {
        if (x.typeName().equals("HashMap")) {
          if (!x.args().isEmpty()) throw new CompileError("new HashMap takes no args (v1)");
          yield "#{}";
        }
        RecordDecl r = records.get(x.typeName());
        if (r == null) throw new CompileError("unknown record type: " + x.typeName());
        if (r.components().size() != x.args().size()) {
          throw new CompileError("new " + x.typeName() + ": expected "
              + r.components().size() + " args, got " + x.args().size());
        }
        var entries = new ArrayList<String>();
        for (int i = 0; i < x.args().size(); i++) {
          entries.add(r.components().get(i).name() + " => " + genExpr(x.args().get(i), env));
        }
        yield "#{" + String.join(", ", entries) + "}";
      }
      case FieldAccess x -> {
        // Atom.ok -> the atom ok (enum-constant syntax)
        if (x.obj() instanceof VarRef vr && vr.name().equals("Atom") && !env.containsKey("Atom")) {
          if (!x.field().matches("[a-z][a-zA-Z0-9_]*")) {
            throw new CompileError("Atom." + x.field() + ": atoms must start lowercase");
          }
          yield x.field();
        }
        if (x.field().equals("length")) yield "length(" + genExpr(x.obj(), env) + ")";
        yield "maps:get(" + x.field() + ", " + genExpr(x.obj(), env) + ")";
      }
      case Index x -> "lists:nth((" + genExpr(x.index(), env) + ") + 1, " + genExpr(x.obj(), env) + ")";
      case Unary x -> {
        String inner = genExpr(x.operand(), env);
        yield x.op().equals("!") ? "(not " + inner + ")" : "(" + x.op() + inner + ")";
      }
      case Binary x -> {
        if (x.op().equals("+") && (isStr(x.left()) || isStr(x.right()))) {
          yield "<<" + concatSegs(x.left(), env) + ", " + concatSegs(x.right(), env) + ">>";
        }
        yield "(" + genExpr(x.left(), env) + " " + erlOp(x.op(), x.left(), x.right()) + " "
            + genExpr(x.right(), env) + ")";
      }
      case Call x -> {
        if (inActor) {
          throw new CompileError("inside an actor, call static methods as Class.method(...)");
        }
        var args = new ArrayList<String>();
        for (Expr a : x.args()) args.add(genExpr(a, env));
        yield fnName(x.callee()) + "(" + String.join(", ", args) + ")";
      }
      case MethodCall x -> genMethodCall(x, env);
      case SpawnExpr x ->
          throw new CompileError("spawn must be bound directly: var x = spawn "
              + x.actorName() + "()  (v1)");
      case LambdaExpr x -> genLambda(x, env);
    };
  }

  /** Erlang fun; Java's effectively-final capture rule == Erlang's semantics, enforced. */
  private String genLambda(LambdaExpr x, Map<String, String> env) {
    var bad = new LinkedHashSet<String>();
    collectAssigned(x.body(), bad);
    for (String b : bad) {
      if (env.containsKey(b) && !x.params().contains(b)) {
        throw new CompileError("lambda: captured variable '" + b
            + "' must be effectively final (cannot assign or mutate it)");
      }
    }
    var lenv = new HashMap<>(env);
    var savedTypes = new HashMap<String, String>();
    var ps = new ArrayList<String>();
    for (String p : x.params()) {
      savedTypes.put(p, varTypes.get(p));
      varTypes.remove(p);
      String v = fresh(p);
      lenv.put(p, v);
      ps.add(v);
    }
    List<String> code = genStmts(x.body().stmts(), lenv, true, null);
    if (code.isEmpty()) code = List.of("ok");
    String body = String.join(",\n            ", code);
    if (needsThrow(x.body())) {
      String rv = fresh("v");
      body = "try " + body + " catch throw:{'$ret', " + rv + "} -> " + rv + " end";
    }
    for (var e : savedTypes.entrySet()) {
      if (e.getValue() == null) varTypes.remove(e.getKey());
      else varTypes.put(e.getKey(), e.getValue());
    }
    return "fun(" + String.join(", ", ps) + ") -> " + body + " end";
  }

  private String genMethodCall(MethodCall x, Map<String, String> env) {
    // System.out.println / System.out.print
    if (x.target() instanceof FieldAccess fa && fa.obj() instanceof VarRef sys
        && sys.name().equals("System") && fa.field().equals("out")) {
      if (!x.method().equals("println") && !x.method().equals("print")) {
        throw new CompileError("unsupported: System.out." + x.method());
      }
      String nl = x.method().equals("println") ? "~n" : "";
      String fmt = isStr(x.args().get(0)) ? "~ts" : "~p";
      return "io:format(\"" + fmt + nl + "\", [" + genExpr(x.args().get(0), env) + "])";
    }
    if (!(x.target() instanceof VarRef vr)) {
      throw new CompileError("unsupported method call target");
    }
    // Thread.sleep(ms)
    if (vr.name().equals("Thread") && x.method().equals("sleep")) {
      return "timer:sleep(" + genExpr(x.args().get(0), env) + ")";
    }
    // Tuple.of(a, b) -> {A, B};  Tuple.get(t, i) -> element(i+1, T)
    if (vr.name().equals("Tuple")) {
      if (x.method().equals("of")) {
        var args = new ArrayList<String>();
        for (Expr a : x.args()) args.add(genExpr(a, env));
        return "{" + String.join(", ", args) + "}";
      }
      if (x.method().equals("get") && x.args().size() == 2) {
        return "erlang:element((" + genExpr(x.args().get(1), env) + ") + 1, "
            + genExpr(x.args().get(0), env) + ")";
      }
      throw new CompileError("unsupported: Tuple." + x.method());
    }
    // Erlang.ok(e) -> unwrap {ok, V} or raise catchable {badmatch, Other}
    if (vr.name().equals("Erlang") && x.method().equals("ok")) {
      String v = fresh("ok");
      String o = fresh("err");
      return "case " + genExpr(x.args().get(0), env) + " of {ok, " + v + "} -> " + v
          + "; " + o + " -> erlang:error({badmatch, " + o + "}) end";
    }
    // actor handle
    ActorDecl actor = handleTypes.get(vr.name());
    if (actor != null) {
      int arity = x.args().size();
      MethodDecl m = actor.methods().stream()
          .filter(h -> h.name().equals(x.method()) && h.params().size() == arity)
          .findFirst().orElseThrow(() -> new CompileError("actor " + actor.name()
              + " has no method " + x.method() + "/" + arity));
      var margs = new ArrayList<String>();
      for (Expr arg : x.args()) margs.add(genExpr(arg, env));
      String msg = margs.isEmpty() ? "{" + x.method() + "}"
          : "{" + x.method() + ", " + String.join(", ", margs) + "}";
      String kind = m.retType().equals("void") ? "cast" : "call";
      return "gen_server:" + kind + "(" + envGet(env, vr.name()) + ", " + msg + ")";
    }
    // record accessor p.x(); HashMap reads (put/remove are statements -> SSA rebind)
    if (env.containsKey(vr.name())) {
      if ("HashMap".equals(varTypes.get(vr.name()))) {
        String m = envGet(env, vr.name());
        return switch (x.method()) {
          case "get" -> "maps:get(" + genExpr(x.args().get(0), env) + ", " + m + ")";
          case "containsKey" -> "maps:is_key(" + genExpr(x.args().get(0), env) + ", " + m + ")";
          case "size" -> "maps:size(" + m + ")";
          case "put", "remove" -> throw new CompileError(
              "HashMap." + x.method() + " mutates: use it as a statement");
          default -> throw new CompileError("unsupported: HashMap." + x.method());
        };
      }
      RecordDecl r = records.get(varTypes.get(vr.name()));
      if (r != null && x.args().isEmpty()
          && r.components().stream().anyMatch(c -> c.name().equals(x.method()))) {
        return "maps:get(" + x.method() + ", " + envGet(env, vr.name()) + ")";
      }
      throw new CompileError("unsupported method call on variable '" + vr.name() + "'");
    }
    // FFI: erlang module, no arity check (signatures unknown; runtime reports undef)
    String ffiMod = ffi.get(vr.name());
    if (ffiMod != null) {
      var args = new ArrayList<String>();
      for (Expr a : x.args()) args.add(genExpr(a, env));
      return ffiMod + ":" + x.method() + "(" + String.join(", ", args) + ")";
    }
    // static method of another class
    ClassInfo ci = classes.get(vr.name());
    if (ci != null) {
      String key = x.method() + "/" + x.args().size();
      if (!ci.methods().containsKey(key)) {
        throw new CompileError("class " + vr.name() + " has no method " + key);
      }
      var args = new ArrayList<String>();
      for (Expr a : x.args()) args.add(genExpr(a, env));
      return ci.module() + ":" + x.method() + "(" + String.join(", ", args) + ")";
    }
    throw new CompileError("unknown method call target '" + vr.name() + "'");
  }

  /** Segments of a string concatenation chain, flattened. */
  private String concatSegs(Expr e, Map<String, String> env) {
    if (e instanceof Binary b && b.op().equals("+") && (isStr(b.left()) || isStr(b.right()))) {
      return concatSegs(b.left(), env) + ", " + concatSegs(b.right(), env);
    }
    if (e instanceof StrLit s) return "\"" + escErl(s.text()) + "\"/utf8";
    return "('$fmt'(" + genExpr(e, env) + "))/binary";
  }

  private static String escErl(String s) {
    return s.replace("\\", "\\\\").replace("\"", "\\\"");
  }

  /** int/int -> div (Java semantics); anything float-ish -> /. */
  private String erlOp(String op, Expr l, Expr r) {
    return switch (op) {
      case "+", "-", "*" -> op;
      case "/" -> "int".equals(exprType(l)) && "int".equals(exprType(r)) ? "div" : "/";
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

  // ---- types (declared + inferred; just enough for concat, div and println) ----

  private boolean isStr(Expr e) {
    return "String".equals(exprType(e));
  }

  private String exprType(Expr e) {
    return switch (e) {
      case IntLit x -> "int";
      case FloatLit x -> "double";
      case BoolLit x -> "boolean";
      case StrLit x -> "String";
      case VarRef x -> varTypes.get(x.name());
      case ListLit x -> null;
      case NewExpr x -> x.typeName();
      case FieldAccess x -> {
        if (x.obj() instanceof VarRef vr && vr.name().equals("Atom")) yield "Atom";
        yield x.field().equals("length") ? "int" : null;
      }
      case LambdaExpr x -> "Function";
      case Index x -> {
        String t = exprType(x.obj());
        yield t != null && t.endsWith("[]") ? t.substring(0, t.length() - 2) : null;
      }
      case Unary x -> x.op().equals("!") ? "boolean" : exprType(x.operand());
      case Binary x -> {
        if (x.op().equals("+") && (isStr(x.left()) || isStr(x.right()))) yield "String";
        yield switch (x.op()) {
          case "+", "-", "*", "/", "%" ->
              "double".equals(exprType(x.left())) || "double".equals(exprType(x.right()))
                  ? "double" : "int";
          default -> "boolean";
        };
      }
      case Call x -> {
        ClassInfo ci = curClassName == null ? null : classes.get(curClassName);
        yield ci == null ? null : ci.methods().get(x.callee() + "/" + x.args().size());
      }
      case MethodCall x -> {
        if (!(x.target() instanceof VarRef vr)) yield null;
        if (vr.name().equals("Tuple")) yield x.method().equals("of") ? "Tuple" : null;
        if ("HashMap".equals(varTypes.get(vr.name()))) {
          yield switch (x.method()) {
            case "size" -> "int";
            case "containsKey" -> "boolean";
            default -> null;
          };
        }
        ActorDecl actor = handleTypes.get(vr.name());
        if (actor != null) {
          yield actor.methods().stream()
              .filter(m -> m.name().equals(x.method()) && m.params().size() == x.args().size())
              .map(MethodDecl::retType).findFirst().orElse(null);
        }
        RecordDecl r = records.get(varTypes.get(vr.name()));
        if (r != null) {
          yield r.components().stream().filter(c -> c.name().equals(x.method()))
              .map(Param::type).findFirst().orElse(null);
        }
        ClassInfo ci = classes.get(vr.name());
        yield ci == null ? null : ci.methods().get(x.method() + "/" + x.args().size());
      }
      case SpawnExpr x -> x.actorName();
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
      if (s instanceof ForEachStmt it && hasReturn(it.body())) return true;
      if (s instanceof WhileStmt it && hasReturn(it.body())) return true;
      if (s instanceof SeqStmt it && hasReturn(new Block(it.stmts()))) return true;
      if (s instanceof TryStmt it
          && (hasReturn(it.tryBlock()) || hasReturn(it.catchBlock()))) return true;
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
      if (s instanceof ForEachStmt it && hasReturn(it.body())) return true;
      if (s instanceof WhileStmt it && hasReturn(it.body())) return true;
      if (s instanceof SeqStmt it && hasReturn(new Block(it.stmts()))) return true;
      if (s instanceof TryStmt it
          && (hasReturn(it.tryBlock()) || hasReturn(it.catchBlock()))) return true;
    }
    return false;
  }

  private int countReturns(Block b) {
    int n = 0;
    for (Stmt s : b.stmts()) {
      switch (s) {
        case ReturnStmt st -> n++;
        case IfStmt st -> {
          n += countReturns(st.thenBlock());
          if (st.elseBlock() != null) n += countReturns(st.elseBlock());
        }
        case ForEachStmt st -> n += countReturns(st.body());
        case WhileStmt st -> n += countReturns(st.body());
        case SeqStmt st -> n += countReturns(new Block(st.stmts()));
        case TryStmt st -> n += countReturns(st.tryBlock()) + countReturns(st.catchBlock());
        default -> {}
      }
    }
    return n;
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
        case ForEachStmt st -> collectAssigned(st.body(), out);
        case WhileStmt st -> collectAssigned(st.body(), out);
        case SeqStmt st -> collectAssigned(new Block(st.stmts()), out);
        case TryStmt st -> {
          collectAssigned(st.tryBlock(), out);
          collectAssigned(st.catchBlock(), out);
        }
        case ExprStmt st -> {
          // m.put/m.remove statements rebind the receiver
          if (st.expr() instanceof MethodCall mc && mc.target() instanceof VarRef vr
              && (mc.method().equals("put") || mc.method().equals("remove"))) {
            out.add(vr.name());
          }
        }
        default -> {}
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
        case ForEachStmt st -> {
          exprRefs(st.iterable(), out);
          blockRefs(st.body(), out);
        }
        case WhileStmt st -> {
          exprRefs(st.cond(), out);
          blockRefs(st.body(), out);
        }
        case SeqStmt st -> blockRefs(new Block(st.stmts()), out);
        case TryStmt st -> {
          blockRefs(st.tryBlock(), out);
          blockRefs(st.catchBlock(), out);
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
      case StrLit x -> {}
      case VarRef x -> out.add(x.name());
      case ListLit x -> {
        for (Expr el : x.elems()) exprRefs(el, out);
      }
      case NewExpr x -> {
        for (Expr a : x.args()) exprRefs(a, out);
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
        // class names/System/Thread aren't data deps, but actor handles and records are
        if (x.target() instanceof VarRef vr
            && (handleTypes.containsKey(vr.name()) || varTypes.containsKey(vr.name()))) {
          out.add(vr.name());
        }
        for (Expr a : x.args()) exprRefs(a, out);
      }
      case SpawnExpr x -> {}
      case LambdaExpr x -> {
        var inner = new LinkedHashSet<String>();
        blockRefs(x.body(), inner);
        inner.removeAll(x.params());
        out.addAll(inner);
      }
    }
  }
}
