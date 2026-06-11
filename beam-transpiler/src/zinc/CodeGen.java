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
 * Actor -> gen_server module (void method = cast, typed = call); Application -> root
 * supervisor static children; dynamic new -> zinc_dyn_sup (temporary, die with owner).
 * Core lowerings unchanged from the validated set: SSA locals, loops -> tail recursion,
 * if-phi, early return via throw, break/continue via loop-scoped throw.
 */
class CodeGen {
  record ClassInfo(String module, Map<String, String> methods) {} // "name/arity" -> retType

  private final Program program;
  private final Map<String, ClassInfo> classes;   // project-wide, by class name
  private final Map<String, RecordDecl> records;  // project-wide, by record name
  private final Map<String, EnumDecl> enums;      // project-wide, by enum name
  private final Map<String, ActorDecl> allActors; // project-wide: spawn/dispatch anywhere
  private final Map<String, String> actorMods;    // actor simple name -> FQ module
  private final Map<String, Ast.ExceptionDecl> exceptions; // project-wide
  private final Map<String, String> excTags;      // exception simple name -> FQ tag
  private final Map<String, Ast.InterfaceDecl> interfaces;
  private final Map<String, Ast.InstanceClassDecl> instClasses;
  private final Map<String, String> instMods;     // instance class simple name -> FQ module
  private final Map<String, ActorDecl> actors = new LinkedHashMap<>(); // this file's
  private final Map<String, String> ffi = new LinkedHashMap<>(); // alias -> erlang module
  private final boolean projectHasActors;

  private String curModule;
  private String curClassName;
  private boolean inActor = false;
  private Map<String, String> varTypes = new HashMap<>();       // var -> type, per method
  private int ctr = 0;
  private List<String> helpers = new ArrayList<>();

  CodeGen(Program program, Map<String, ClassInfo> classes, Map<String, RecordDecl> records,
      Map<String, EnumDecl> enums, Map<String, ActorDecl> allActors,
      Map<String, String> actorMods, Map<String, Ast.ExceptionDecl> exceptions,
      Map<String, String> excTags, Map<String, Ast.InterfaceDecl> interfaces,
      Map<String, Ast.InstanceClassDecl> instClasses, Map<String, String> instMods,
      boolean projectHasActors) {
    this.program = program;
    this.classes = classes;
    this.records = records;
    this.enums = enums;
    this.allActors = allActors;
    this.actorMods = actorMods;
    this.exceptions = exceptions;
    this.excTags = excTags;
    this.interfaces = interfaces;
    this.instClasses = instClasses;
    this.instMods = instMods;
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

  private static final String SFX_HELPER =
      "'$sfx'(B, S) -> byte_size(S) =< byte_size(B) andalso\n"
          + "    binary:part(B, byte_size(B) - byte_size(S), byte_size(S)) =:= S.";

  private static final String OK_HELPER =
      "'$ok'({ok, V}) -> V;\n"
          + "'$ok'(Other) -> erlang:error({badmatch, Other}).";

  private static final String IDX_HELPER =
      "'$idx'(B, P) -> case binary:match(B, P) of nomatch -> -1; {Pos, _} -> Pos end.";

  /** catch (Exception e): zinc exceptions unwrap; native errors render a message. */
  private static final String EXNORM_HELPER =
      "'$exnorm'({zinc_exc, _, F}) -> F;\n"
      + "'$exnorm'(R) -> #{'$class' => 'java.lang.exception',\n"
      + "    message => iolist_to_binary(io_lib:format(\"~p\", [R]))}.";

  /** Typed actor call: a deliberate throw in the callee relays here, catchable;\n
   *  bugs crash the callee and this caller exits with the same reason (ladder). */
  private static final String CALL_HELPER =
      "'$call'(N, M) -> case gen_server:call(N, M) of\n"
      + "    {'$zinc_relay', {zinc_exc, _, _} = E} -> erlang:error(E);\n"
      + "    V -> V end.";

  /** Boundary guard: unknown crossing into known. SHALLOW; failure = structured crash
   *  for supervision. Default ON (build flag to strip later if profiling justifies). */
  private static final String CHK_HELPER =
      "'$chk'(V, integer) when is_integer(V) -> V;\n"
      + "'$chk'(V, float) when is_float(V) -> V;\n"
      + "'$chk'(V, boolean) when is_boolean(V) -> V;\n"
      + "'$chk'(V, string) when is_binary(V) -> V;\n"
      + "'$chk'(V, actor) when is_atom(V) -> V;\n"
      + "'$chk'(V, {class, T}) when is_map(V) ->\n"
      + "    case maps:get('$class', V, undefined) of T -> V;\n"
      + "        _ -> erlang:error({zinc_badtype, T, V, ?MODULE}) end;\n"
      + "'$chk'(V, {iface, _}) when is_function(V); is_map(V) -> V;\n"
      + "'$chk'(V, Spec) -> erlang:error({zinc_badtype, Spec, V, ?MODULE}).";

  private static final String JGET_HELPER =
      "'$jget'(M, K, Spec) ->\n"
      + "    case maps:find(K, M) of\n"
      + "        error -> erlang:error({zinc_badtype, {missing, K}, M, ?MODULE});\n"
      + "        {ok, V} -> '$jchk'(V, Spec)\n"
      + "    end.\n"
      + "'$jchk'(V, integer) when is_integer(V) -> V;\n"
      + "'$jchk'(V, number) when is_number(V) -> V;\n"
      + "'$jchk'(V, boolean) when is_boolean(V) -> V;\n"
      + "'$jchk'(V, string) when is_binary(V) -> V;\n"
      + "'$jchk'(V, raw) -> V;\n"
      + "'$jchk'(V, Spec) -> erlang:error({zinc_badtype, Spec, V, ?MODULE}).";

  // per-output-module usage flags: helpers are emitted only when referenced
  private boolean useFmt;
  private boolean useSfx;
  private boolean useOk;
  private boolean useIdx;
  private boolean useExnorm;
  private boolean useCall;
  private boolean useChk;
  private boolean useJget;
  private boolean usedHttp;

  private List<String> usedHelpers() {
    var out = new ArrayList<String>();
    if (useFmt) out.add(FMT_HELPER);
    if (useSfx) out.add(SFX_HELPER);
    if (useOk) out.add(OK_HELPER);
    if (useIdx) out.add(IDX_HELPER);
    if (useExnorm) out.add(EXNORM_HELPER);
    if (useCall) out.add(CALL_HELPER);
    if (useChk) out.add(CHK_HELPER);
    if (useJget) out.add(JGET_HELPER);
    return out;
  }

  private final java.util.Set<String> dispHelpers = new java.util.HashSet<>();

  /** Gradual checking: known-vs-known mismatch is an error; unknown flows free. */
  private void checkBind(String declared, String got, String where) {
    if (got == null || declared.equals(got)) return;            // unknown flows; exact ok
    if (declared.equals("double") && got.equals("int")) return; // widening
    Ast.InstanceClassDecl ic = instClasses.get(got);
    if (ic != null && ic.iface().equals(declared)) return;      // one-hop subtyping
    boolean dn = isNominal(declared) || isPrim(declared);
    boolean gn = isNominal(got) || isPrim(got);
    if (dn && gn) {
      throw new CompileError(where + ": cannot bind a " + got + " to " + declared
          + " (known-vs-known mismatch; exact nominal match required)");
    }
  }

  /** List<String> -> List; type args feed checks/guards, never the lowering. */
  static String baseType(String t) {
    int i = t.indexOf('<');
    return i < 0 ? t : t.substring(0, i);
  }

  /** List<String> -> [String]; Map<String, Integer> -> [String, Integer]; else []. */
  static List<String> typeArgs(String t) {
    int i = t.indexOf('<');
    if (i < 0 || !t.endsWith(">")) return List.of();
    var out = new ArrayList<String>();
    for (String a : t.substring(i + 1, t.length() - 1).split(",")) out.add(a.trim());
    return out;
  }

  /** Runtime boundary guard spec for a declared type; null = not guardable (flows free). */
  private String typeSpec(String t) {
    String b = baseType(t);
    return switch (b) {
      case "int" -> "integer";
      case "double" -> "float";
      case "boolean" -> "boolean";
      case "String" -> "string";
      default -> {
        if (records.containsKey(b)) yield "{class, " + atomLit(b.toLowerCase()) + "}";
        if (instClasses.containsKey(b)) yield "{class, " + atomLit(instMods.get(b)) + "}";
        if (exceptions.containsKey(b)) yield "{class, " + atomLit(excTags.get(b)) + "}";
        if (interfaces.containsKey(b)) yield "{iface, " + atomLit(b.toLowerCase()) + "}";
        if (allActors.containsKey(b)) yield "actor";
        yield null;
      }
    };
  }

  private boolean isPrim(String t) {
    return t.equals("int") || t.equals("double") || t.equals("boolean") || t.equals("String");
  }

  private boolean isNominal(String t) {
    return records.containsKey(t) || instClasses.containsKey(t) || interfaces.containsKey(t)
        || allActors.containsKey(t) || exceptions.containsKey(t) || enums.containsKey(t);
  }

  private void checkInstanceMethod(Ast.InstanceClassDecl ic, MethodCall x) {
    if (ic.methods().stream().noneMatch(m -> m.name().equals(x.method())
        && m.params().size() == x.args().size())) {
      throw new CompileError("class " + ic.name() + " has no method " + x.method() + "/"
          + x.args().size());
    }
  }

  boolean usedHttp() {
    return usedHttp;
  }

  private void resetModuleState() {
    helpers = new ArrayList<>();
    dispHelpers.clear();
    useFmt = false;
    useSfx = false;
    useOk = false;
    useIdx = false;
    useExnorm = false;
    useCall = false;
    useChk = false;
    useJget = false;
    jsonEmitted.clear();
  }

  /** Indent a code list as a clause body: first line padded, inner newlines shifted. */
  private static String block(List<String> code, String pad) {
    if (code.isEmpty()) return pad + "ok";
    return pad + String.join(",\n", code).replace("\n", "\n" + pad);
  }


  /** zinc.http client over httpc — Java-shaped, sync send only (fan-out = worker Actors). */
  static final String HTTP_SOURCE = "-module('zinc.http').\n"
      + "-export([send/2, add_header/3, with_body/3, header/2]).\n\n"
      + "add_header(R, K, V) -> maps:put(headers, maps:get(headers, R, []) ++ [{K, V}], R).\n\n"
      + "with_body(R, M, B) -> maps:put(method, M, maps:put(body, B, R)).\n\n"
      + "header(Resp, Name) ->\n"
      + "    case lists:keyfind(string:lowercase(Name), 1, maps:get(headers, Resp, [])) of\n"
      + "        false -> <<>>;\n"
      + "        {_, V} -> V\n"
      + "    end.\n\n"
      + "send(Client, Req) ->\n"
      + "    {ok, _} = application:ensure_all_started(inets),\n"
      + "    {ok, _} = application:ensure_all_started(ssl),\n"
      + "    ok = set_proxy(Client),\n"
      + "    Url = binary_to_list(maps:get(url, Req)),\n"
      + "    Headers = [{binary_to_list(K), binary_to_list(V)}\n"
      + "               || {K, V} <- maps:get(headers, Req, [])],\n"
      + "    HttpOpts = [{timeout, maps:get(timeout, Req, 30000)},\n"
      + "                {connect_timeout, maps:get(connect_timeout, Client, 5000)},\n"
      + "                {ssl, [{verify, verify_none}]}],\n"
      + "    Method = maps:get(method, Req, get),\n"
      + "    Request = case Method of\n"
      + "        get -> {Url, Headers};\n"
      + "        delete -> {Url, Headers};\n"
      + "        _ -> {Url, Headers, \"application/octet-stream\", maps:get(body, Req, <<>>)}\n"
      + "    end,\n"
      + "    case httpc:request(Method, Request, HttpOpts, [{body_format, binary}]) of\n"
      + "        {ok, {{_, Status, _}, RespHeaders, Body}} ->\n"
      + "            #{status => Status, body => Body,\n"
      + "              headers => [{iolist_to_binary(string:lowercase(K)), iolist_to_binary(V)}\n"
      + "                          || {K, V} <- RespHeaders]};\n"
      + "        {error, {failed_connect, _} = R} -> raise('zinc.http.connectexception', R);\n"
      + "        {error, timeout} -> raise('zinc.http.timeoutexception', timeout);\n"
      + "        {error, R} -> raise('zinc.http.httpexception', R)\n"
      + "    end.\n\n"
      + "set_proxy(Client) ->\n"
      + "    case maps:get(proxy, Client, none) of\n"
      + "        none -> ok;\n"
      + "        {H, P} -> ok = httpc:set_options([{proxy, {{binary_to_list(H), P}, []}}]), ok\n"
      + "    end.\n\n"
      + "raise(Tag, R) ->\n"
      + "    erlang:error({zinc_exc, Tag, #{'$class' => Tag,\n"
      + "        message => iolist_to_binary(io_lib:format(\"~p\", [R]))}}).\n";

  /** Stdlib exceptions: name -> FQ tag. One-level hierarchy via BUILTIN_EXC_CHILDREN. */
  static final String[][] BUILTIN_EXCEPTIONS = {
      {"HttpException", "zinc.http.httpexception"},
      {"ConnectException", "zinc.http.connectexception"},
      {"TimeoutException", "zinc.http.timeoutexception"}};
  static final Map<String, List<String>> BUILTIN_EXC_CHILDREN =
      Map.of("HttpException", List.of("ConnectException", "TimeoutException"));

  /** Dynamic children: temporary (never restarted), die with their spawner (monitor). */
  static final String DYN_SUP_SOURCE = "-module(zinc_dyn_sup).\n"
      + "-behaviour(supervisor).\n"
      + "-export([start_link/0, spawn_child/3, do_start/4, init/1]).\n\n"
      + "start_link() -> supervisor:start_link({local, zinc_dyn_sup}, ?MODULE, []).\n\n"
      + "spawn_child(StartMod, Owner, Args) ->\n"
      + "    N = erlang:unique_integer([positive]),\n"
      + "    Name = list_to_atom(atom_to_list(StartMod) ++ \"_\" ++ integer_to_list(N)),\n"
      + "    {ok, _} = supervisor:start_child(zinc_dyn_sup, [StartMod, Name, Owner, Args]),\n"
      + "    Name.\n\n"
      + "do_start(StartMod, Name, Owner, Args) -> StartMod:start_link(Name, Owner, Args).\n\n"
      + "init([]) ->\n"
      + "    {ok, {#{strategy => simple_one_for_one, intensity => 1000, period => 3600},\n"
      + "          [#{id => zinc_dyn, start => {zinc_dyn_sup, do_start, []},\n"
      + "             restart => temporary, shutdown => 5000, type => worker}]}}.\n";

  /** Root supervisor: zinc_dyn_sup + the Application's static children, decl order. */
  static String rootSupSource(Ast.ApplicationDecl app, Map<String, ActorDecl> actors,
      Map<String, String> actorMods) {
    var specs = new ArrayList<String>();
    specs.add("#{id => zinc_dyn_sup, start => {zinc_dyn_sup, start_link, []},\n"
        + "             restart => permanent, shutdown => infinity, type => supervisor}");
    if (app != null) {
      for (FieldDecl f : app.fields()) {
        specs.add(staticChildSpec(f, "'" + f.name() + "'", actors, actorMods, "Application"));
      }
    }
    return "-module(zinc_root_sup).\n"
        + "-behaviour(supervisor).\n"
        + "-export([start_link/0, init/1]).\n\n"
        + "start_link() -> supervisor:start_link({local, zinc_root_sup}, ?MODULE, []).\n\n"
        + "init([]) ->\n"
        + "    {ok, {#{strategy => one_for_one, intensity => 1000, period => 3600},\n"
        + "          [" + String.join(",\n           ", specs) + "]}}.\n";
  }

  /** `Counter c = new Counter(0)` as a field -> permanent child spec; nameExpr is the
   *  registered-name term (a literal atom at root; computed from the owner's name below). */
  static String staticChildSpec(FieldDecl f, String nameExpr, Map<String, ActorDecl> actors,
      Map<String, String> actorMods, String where) {
    ActorDecl child = actors.get(f.type());
    if (child == null) {
      throw new CompileError(where + " field " + f.name() + ": type " + f.type()
          + " is not an Actor — static children are Actor-typed fields (v1)");
    }
    if (!(f.init() instanceof SpawnExpr sp) || !sp.actorName().equals(f.type())) {
      throw new CompileError(where + " field " + f.name() + " must be initialized: "
          + f.type() + " " + f.name() + " = new " + f.type() + "(...)");
    }
    boolean pair = hasActorChildren(child, actors);
    String childMod = actorMods.get(f.type());
    return "#{id => '" + f.name() + "', start => {" + atomLit(pair ? childMod + "_sup"
        : childMod) + ", start_link, [" + nameExpr + ", none, ["
        + literalArgs(sp.args(), f.name()) + "]]},\n             restart => permanent, "
        + "shutdown => " + (pair ? "infinity" : "5000") + ", type => "
        + (pair ? "supervisor" : "worker") + "}";
  }

  /** An Actor with Actor-typed fields lowers to a supervisor pair (its own domain). */
  static boolean hasActorChildren(ActorDecl a, Map<String, ActorDecl> actors) {
    return a.fields().stream().anyMatch(f -> actors.containsKey(f.type()));
  }

  private static final java.util.Set<String> ERL_RESERVED = java.util.Set.of("after", "and",
      "andalso", "band", "begin", "bnot", "bor", "bsl", "bsr", "bxor", "case", "catch",
      "cond", "div", "else", "end", "fun", "if", "let", "maybe", "not", "of", "or",
      "orelse", "receive", "rem", "try", "when", "xor");

  /** Universal atom emitter: bare only when safely bare, else quoted (escaped). */
  static String atomLit(String name) {
    if (name.length() > 255) {
      throw new CompileError("atom longer than 255 chars: " + name.substring(0, 40) + "...");
    }
    if (name.matches("[a-z][a-zA-Z0-9_]*") && !ERL_RESERVED.contains(name)) return name;
    return "'" + name.replace("\\", "\\\\").replace("'", "\\'") + "'";
  }

  /** Static child ctor args live in supervisor specs: restart re-runs the SAME ctor. */
  private static String literalArgs(List<Expr> args, String where) {
    var out = new ArrayList<String>();
    for (Expr e : args) {
      out.add(switch (e) {
        case IntLit x -> String.valueOf(x.value());
        case FloatLit x -> String.valueOf(x.value());
        case BoolLit x -> String.valueOf(x.value());
        case StrLit x -> "<<\"" + escErl(x.text()) + "\"/utf8>>";
        default -> throw new CompileError("static child '" + where
            + "': constructor args must be literals (v1)");
      });
    }
    return String.join(", ", out);
  }

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
      resetModuleState();
      curModule = classes.get(c.name()).module();
      curClassName = c.name();
      out.put(curModule, genClassModule(c));
    }
    for (ActorDecl a : actors.values()) {
      resetModuleState();
      curModule = actorMods.get(a.name());
      curClassName = null;
      inActor = true;
      out.put(curModule, genActorModule(a));
      inActor = false;
      if (hasActorChildren(a, allActors)) {
        out.put(curModule + "_sup", genPairSup(a));
      }
    }
    for (Ast.InstanceClassDecl c : program.instanceClasses()) {
      resetModuleState();
      curModule = instMods.get(c.name());
      curClassName = c.name();
      out.put(curModule, genInstanceClassModule(c));
    }
    if (program.application() != null) {
      resetModuleState();
      curModule = program.application().erlMod();
      curClassName = program.application().name();
      out.put(curModule, genApplicationModule(program.application()));
    }
    return out;
  }

  /** Instance class -> module: new/N builds the map ('$class' => module atom, fields
   *  ctor-set then immutable); each method takes the instance as its first arg. */
  private String genInstanceClassModule(Ast.InstanceClassDecl c) {
    var exports = new ArrayList<String>();
    var pieces = new ArrayList<String>();

    // new(CtorArgs) -> #{'$class' => 'mod', field => ...}
    varTypes = new HashMap<>();
    var env = new HashMap<String, String>();
    var ps = new ArrayList<String>();
    if (c.ctor() != null) {
      for (Param p : c.ctor().params()) {
        String v = fresh(p.name());
        env.put(p.name(), v);
        varTypes.put(p.name(), p.type());
        ps.add(v);
      }
    }
    var lines = new ArrayList<String>();
    for (FieldDecl f : c.fields()) {
      String v = fresh(f.name());
      lines.add(v + " = " + (f.init() == null ? defaultFor(f.type()) : genExpr(f.init(), env)));
      env.put(f.name(), v);
      varTypes.put(f.name(), f.type());
    }
    if (c.ctor() != null) {
      if (countReturns(c.ctor().body()) > 0) {
        throw new CompileError("class " + c.name() + ": constructor cannot return");
      }
      lines.addAll(genStmts(c.ctor().body().stmts(), env, false, null));
    }
    var entries = new ArrayList<String>(List.of("'$class' => " + atomLit(curModule)));
    for (FieldDecl f : c.fields()) entries.add(f.name() + " => " + envGet(env, f.name()));
    lines.add("#{" + String.join(", ", entries) + "}");
    exports.add("new/" + ps.size());
    pieces.add("new(" + String.join(", ", ps) + ") ->\n" + block(lines, "        ") + ".");

    for (MethodDecl m : c.methods()) {
      exports.add(m.name() + "/" + (m.params().size() + 1));
      pieces.add(genInstanceMethod(c, m));
    }
    pieces.addAll(helpers);
    pieces.addAll(usedHelpers());
    return "-module(" + atomLit(curModule) + ").\n"
        + "-export([" + String.join(", ", exports) + "]).\n"
        + "-compile([nowarn_unused_vars, nowarn_unused_function]).\n\n"
        + String.join("\n\n", pieces) + "\n";
  }

  private String genInstanceMethod(Ast.InstanceClassDecl c, MethodDecl m) {
    varTypes = new HashMap<>();
    var env = new HashMap<String, String>();
    String self = fresh("this");
    env.put("this", self);
    varTypes.put("this", c.name());
    var params = new ArrayList<String>(List.of(self));
    for (Param p : m.params()) {
      String v = fresh(p.name());
      env.put(p.name(), v);
      varTypes.put(p.name(), p.type());
      params.add(v);
    }
    var lines = new ArrayList<String>();
    for (FieldDecl f : c.fields()) {
      String v = fresh(f.name());
      lines.add(v + " = maps:get(" + f.name() + ", " + self + ")");
      env.put(f.name(), v);
      varTypes.put(f.name(), f.type());
    }
    List<String> stmts = genStmts(m.body().stmts(), env, true, null);
    lines.addAll(stmts.isEmpty() ? List.of("ok") : stmts);
    String head = m.name() + "(" + String.join(", ", params) + ")";
    if (needsThrow(m.body())) {
      return head + " ->\n    try\n" + block(lines, "        ")
          + "\n    catch throw:{'$ret', V} -> V end.";
    }
    return head + " ->\n" + block(lines, "        ") + ".";
  }

  /** Actor with Actor children -> its own domain: rest_for_one [owner, kids(one_for_one)].
   *  Owner crash takes the domain (fresh ctors); a child crash restarts only itself. */
  private String genPairSup(ActorDecl a) {
    var kids = new ArrayList<String>();
    for (FieldDecl f : a.fields()) {
      if (!allActors.containsKey(f.type())) continue;
      String nameExpr = "list_to_atom(atom_to_list(Name) ++ \"." + f.name() + "\")";
      kids.add(staticChildSpec(f, nameExpr, allActors, actorMods, a.name()));
    }
    String mod = actorMods.get(a.name());
    return "-module(" + atomLit(mod + "_sup") + ").\n"
        + "-behaviour(supervisor).\n"
        + "-export([start_link/3, start_kids/1, init/1]).\n\n"
        + "start_link(Name, Owner, Args) -> supervisor:start_link(?MODULE, {pair, Name, Owner, Args}).\n\n"
        + "start_kids(Name) -> supervisor:start_link(?MODULE, {kids, Name}).\n\n"
        + "init({pair, Name, Owner, Args}) ->\n"
        + "    {ok, {#{strategy => rest_for_one, intensity => 1000, period => 3600},\n"
        + "          [#{id => owner, start => {" + atomLit(mod) + ", start_link, [Name, Owner, Args]},\n"
        + "             restart => permanent, shutdown => 5000, type => worker},\n"
        + "           #{id => kids, start => {?MODULE, start_kids, [Name]},\n"
        + "             restart => permanent, shutdown => infinity, type => supervisor}]}};\n"
        + "init({kids, Name}) ->\n"
        + "    {ok, {#{strategy => one_for_one, intensity => 1000, period => 3600},\n"
        + "          [" + String.join(",\n           ", kids) + "]}}.\n";
  }

  /** The explicit root: boot the tree, host optional main, own the liveness rule. */
  private String genApplicationModule(Ast.ApplicationDecl app) {
    var exports = new ArrayList<String>(List.of("main/0", "run/0"));
    var pieces = new ArrayList<String>();
    String boot = "    logger:set_primary_config(level, info),\n"
        + "    logger:remove_handler(default),\n"
        + "    ok = logger:add_handler(default, logger_std_h,\n"
        + "        #{config => #{type => standard_error},\n"
        + "          filters => [{progress, {fun logger_filters:progress/2, stop}}]}),\n"
        + "    {ok, _} = zinc_root_sup:start_link(),\n";
    pieces.add("main() ->\n" + boot + (app.main() != null ? "    user_main([])." : "    ok."));
    // liveness: static children alive -> serve until stopped; none -> exit after main
    pieces.add(!app.fields().isEmpty() ? "run() -> main(), timer:sleep(infinity)."
        : "run() -> main().");
    if (app.main() != null) {
      exports.add("user_main/1");
      pieces.add(genAppMain(app));
    }
    pieces.addAll(helpers);
    pieces.addAll(usedHelpers());
    return "-module(" + curModule + ").\n"
        + "-export([" + String.join(", ", exports) + "]).\n"
        + "-compile([nowarn_unused_vars, nowarn_unused_function]).\n\n"
        + String.join("\n\n", pieces) + "\n";
  }

  /** main(String[] args) with the Application's Actor fields bound to their handles
   *  (root children register under the field name — declaration is composition). */
  private String genAppMain(Ast.ApplicationDecl app) {
    varTypes = new HashMap<>();
    var env = new HashMap<String, String>();
    for (FieldDecl f : app.fields()) {
      env.put(f.name(), "'" + f.name() + "'");
      varTypes.put(f.name(), f.type());
    }
    MethodDecl m = app.main();
    var params = new ArrayList<String>();
    for (Param p : m.params()) {
      String v = fresh(p.name());
      env.put(p.name(), v);
      varTypes.put(p.name(), p.type());
      params.add(v);
    }
    List<String> stmts = genStmts(m.body().stmts(), env, true, null);
    if (stmts.isEmpty()) stmts = List.of("ok");
    String head = fnName(m.name()) + "(" + String.join(", ", params) + ")";
    if (needsThrow(m.body())) {
      return head + " ->\n    try\n" + block(stmts, "        ")
          + "\n    catch throw:{'$ret', V} -> V end.";
    }
    return head + " ->\n" + block(stmts, "        ") + ".";
  }

  private String genClassModule(ClassDecl c) {
    var defs = new ArrayList<String>();
    for (var m : c.methods()) defs.add(genFn(m));
    var pieces = new ArrayList<String>();
    var exports = new ArrayList<String>();
    boolean isMain = curModule.equals("main");
    if (isMain) {
      exports.add("main/0");
      exports.add("run/0");
      pieces.add(projectHasActors
          ? "main() ->\n"
              + "    logger:set_primary_config(level, info),\n"
        + "    logger:remove_handler(default),\n"
        + "    ok = logger:add_handler(default, logger_std_h,\n"
        + "        #{config => #{type => standard_error},\n"
        + "          filters => [{progress, {fun logger_filters:progress/2, stop}}]}),\n"
              + "    {ok, _} = zinc_root_sup:start_link(),\n"
              + "    user_main([])."
          : "main() ->\n"
              + "    logger:set_primary_config(level, info),\n"
          + "    logger:remove_handler(default),\n"
          + "    ok = logger:add_handler(default, logger_std_h,\n"
          + "        #{config => #{type => standard_error},\n"
          + "          filters => [{progress, {fun logger_filters:progress/2, stop}}]}),\n"
              + "    user_main([]).");
      pieces.add("run() -> main()."); // script: no static children, exit after main
    }
    for (var m : c.methods()) {
      String n = isMain && m.name().equals("main") ? "user_main" : m.name();
      exports.add(n + "/" + m.params().size());
    }
    pieces.addAll(defs);
    pieces.addAll(helpers);
    pieces.addAll(usedHelpers());
    return "-module(" + atomLit(curModule) + ").\n"
        + "-export([" + String.join(", ", exports) + "]).\n"
        + "-compile([nowarn_unused_vars, nowarn_unused_function]).\n\n"
        + String.join("\n\n", pieces) + "\n";
  }

  private String genFn(MethodDecl m) {
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
    String head = fnName(m.name()) + "(" + String.join(", ", params) + ")";
    if (needsThrow(m.body())) {
      return head + " ->\n    try\n" + block(stmts, "        ")
          + "\n    catch throw:{'$ret', V} -> V end.";
    }
    return head + " ->\n" + block(stmts, "        ") + ".";
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
    var exports = new ArrayList<>(
        List.of("start_link/3", "init/1", "handle_call/3", "handle_cast/2", "handle_info/2"));

    var pieces = new ArrayList<String>();
    // [Name, Owner | Args]: init seeds '$self' from Name; Owner = spawner pid for
    // dynamic children (monitored: die with the owner) or none for static children.
    pieces.add("start_link(Name, Owner, Args) -> "
        + "gen_server:start_link({local, Name}, ?MODULE, [Name, Owner | Args], []).");
    pieces.add(genInit(a));
    // no user catch-all clauses: unknown messages crash the actor, the supervisor heals it
    // (the stubs below keep that semantic and silence the behaviour warning)
    pieces.add(casts.isEmpty()
        ? "handle_cast(Msg, _State) -> erlang:error({unknown_cast, Msg})."
        : String.join(";\n", casts) + ".");
    pieces.add(calls.isEmpty()
        ? "handle_call(Msg, _From, _State) -> erlang:error({unknown_call, Msg})."
        : String.join(";\n", calls) + ".");
    // dynamic child: the monitored owner died -> die with it (temporary, no restart)
    pieces.add("handle_info({'DOWN', _Ref, process, _Pid, _Reason}, State) -> "
        + "{stop, normal, State};\n"
        + "handle_info(Msg, _State) -> erlang:error({unknown_info, Msg}).");
    pieces.addAll(helpers);
    pieces.addAll(usedHelpers());
    return "-module(" + atomLit(curModule) + ").\n"
        + "-behaviour(gen_server).\n"
        + "-export([" + String.join(", ", exports) + "]).\n"
        + "-compile([nowarn_unused_vars, nowarn_unused_function]).\n\n"
        + String.join("\n\n", pieces) + "\n";
  }

  private String genInit(ActorDecl a) {
    varTypes = new HashMap<>();
    var env = new HashMap<String, String>();
    String self = fresh("self");
    env.put("this", self);
    varTypes.put("this", a.name());
    String owner = fresh("owner");
    var ps = new ArrayList<String>(List.of(self, owner));
    if (a.ctor() != null) {
      for (Param p : a.ctor().params()) {
        String v = fresh(p.name());
        env.put(p.name(), v);
        varTypes.put(p.name(), p.type());
        ps.add(v);
      }
    }
    String head = "init([" + String.join(", ", ps) + "])";
    var lines = new ArrayList<String>();
    // dynamic child: watch the spawner; its death is ours (none = static, supervised)
    lines.add("case " + owner + " of none -> ok; _ -> erlang:monitor(process, " + owner
        + ") end");
    for (FieldDecl f : a.fields()) {
      String v = fresh(f.name());
      if (allActors.containsKey(f.type())) {
        // static child: started by this actor's pair supervisor AFTER init returns;
        // the handle is deterministic (owner.field), so we can bind it here
        lines.add(v + " = list_to_atom(atom_to_list(" + self + ") ++ \"." + f.name() + "\")");
      } else {
        lines.add(v + " = " + (f.init() == null ? defaultFor(f.type()) : genExpr(f.init(), env)));
      }
      env.put(f.name(), v);
      varTypes.put(f.name(), f.type());
    }
    if (a.ctor() != null) {
      if (countReturns(a.ctor().body()) > 0) {
        throw new CompileError("actor " + a.name() + ": constructor cannot return");
      }
      lines.addAll(genStmts(a.ctor().body().stmts(), env, false, null));
    }
    lines.add("{ok, " + stateMap(a, env) + "}");
    return head + " ->\n" + block(lines, "        ") + ".";
  }

  private static String defaultFor(String type) {
    return switch (type) {
      case "int" -> "0";
      case "double" -> "0.0";
      case "boolean" -> "false";
      case "String" -> "<<>>";
      default -> "undefined";
    };
  }

  /** One handle_cast/handle_call clause: fields seeded via maps:get, SSA body, new state map. */
  private String genHandler(ActorDecl a, MethodDecl m, boolean isCall) {
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
    for (Param p : m.params()) {
      String spec = typeSpec(p.type());
      if (spec != null) {
        useChk = true; // typed method entry: messages arrive from anywhere
        String gv = fresh(p.name());
        lines.add(gv + " = '$chk'(" + env.get(p.name()) + ", " + spec + ")");
        env.put(p.name(), gv);
      }
    }
    String self = fresh("self");
    lines.add(self + " = maps:get('$self', State)");
    env.put("this", self);
    varTypes.put("this", a.name());
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
    if (isCall) {
      // ladder rung 2: a deliberate throw relays to the caller; the actor survives with
      // its ENTRY state (transactional). Only the {zinc_exc,..} shape is caught —
      // bugs fall through and crash the process (rung 3). Casts have no caller: crash.
      return head + " ->\n    try\n" + block(lines, "        ") + "\n"
          + "    catch error:{zinc_exc, _, _} = ZE -> {reply, {'$zinc_relay', ZE}, State}\n"
          + "    end";
    }
    return head + " ->\n" + block(lines, "        ");
  }

  private String stateMap(ActorDecl a, Map<String, String> env) {
    var entries = new ArrayList<String>();
    entries.add("'$self' => " + envGet(env, "this")); // the handle survives every rebuild
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
          if (!st.type().equals("var") && st.init() != null) {
            checkBind(st.type(), exprType(st.init()), st.name());
          }
          if (st.init() instanceof SpawnExpr sp) {
            ActorDecl a = allActors.get(sp.actorName());
            if (a == null) throw new CompileError("unknown actor: " + sp.actorName());
            int want = a.ctor() == null ? 0 : a.ctor().params().size();
            if (sp.args().size() != want) {
              throw new CompileError("spawn " + a.name() + ": constructor takes " + want
                  + " args, got " + sp.args().size());
            }
            String amod = actorMods.get(a.name());
            String startMod = hasActorChildren(a, allActors) ? amod + "_sup" : amod;
            out.add(v + " = zinc_dyn_sup:spawn_child(" + atomLit(startMod) + ", self(), ["
                + genArgs(sp.args(), env) + "])");
            varTypes.put(st.name(), a.name());
          } else if (st.init() instanceof ListLit && st.type().endsWith("[]")) {
            out.add(v + " = array:from_list(" + genExpr(st.init(), env) + ")");
            varTypes.put(st.name(), st.type());
          } else {
            String spec = st.type().equals("var") ? null : typeSpec(st.type());
            if (spec != null && exprType(st.init()) == null) {
              useChk = true; // typed bind from unknown: guarded crossing
              out.add(v + " = '$chk'(" + genExpr(st.init(), env) + ", " + spec + ")");
            } else {
              out.add(v + " = " + genExpr(st.init(), env));
            }
            varTypes.put(st.name(),
                st.type().equals("var") ? exprType(st.init()) : st.type());
          }
          env.put(st.name(), v);
        }
        case AssignStmt st -> {
          String cur = envGet(env, st.name());
          String rhs;
          if (st.op().equals("+=") && (isStr(new VarRef(st.name())) || isStr(st.value()))) {
            useFmt = true;
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
        case FieldAssignStmt st -> throw new CompileError(
            "objects are final values — fields never mutate after construction: build a new "
                + "record instead (" + st.objVar() + " = new ...). Locals stay mutable; "
                + "mutable state lives in Actors.");
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
          // collection mutation as a statement rebinds the receiver (SSA)
          String rebind = st.expr() instanceof MethodCall mc ? genMutator(mc, env) : null;
          out.add(rebind != null ? rebind : genExpr(st.expr(), env));
        }
        case IndexAssignStmt st -> {
          String t = varTypes.get(st.arrVar());
          if (t == null || !t.endsWith("[]")) {
            throw new CompileError("index assignment needs an array-typed variable, '"
                + st.arrVar() + "' is " + (t == null ? "untyped" : t));
          }
          String cur = envGet(env, st.arrVar());
          String iv = fresh("i");
          out.add(iv + " = " + genExpr(st.index(), env));
          String rhs = switch (st.op()) {
            case "=" -> genExpr(st.value(), env);
            case "+=" -> "array:get(" + iv + ", " + cur + ") + " + genExpr(st.value(), env);
            case "-=" -> "array:get(" + iv + ", " + cur + ") - " + genExpr(st.value(), env);
            case "*=" -> "array:get(" + iv + ", " + cur + ") * " + genExpr(st.value(), env);
            default -> throw new CompileError("bad assign op " + st.op());
          };
          String v = fresh(st.arrVar());
          out.add(v + " = array:set(" + iv + ", " + rhs + ", " + cur + ")");
          env.put(st.arrVar(), v);
        }
        case SwitchStmt st -> out.add(genSwitch(st, env, loopMut));
        case IfStmt st -> out.add(genIf(st, env, loopMut));
        case ForEachStmt st -> out.add(genForEach(st, env));
        case WhileStmt st -> out.add(genWhile(st, env));
        case SeqStmt st -> out.addAll(genStmts(st.stmts(), env, false, loopMut));
        case TryStmt st -> out.add(genTry(st, env, loopMut));
        case Ast.ThrowStmt st -> out.add(genThrow(st, env));
      }
    }
    return out;
  }

  /** m.put/m.remove/list.add as a statement: emit `New = ..., env rebind`; null if not one. */
  private String genMutator(MethodCall mc, Map<String, String> env) {
    if (!(mc.target() instanceof VarRef vr) || !env.containsKey(vr.name())) return null;
    String vt = varTypes.get(vr.name());
    String vb = vt == null ? null : baseType(vt);
    boolean isMap = "HashMap".equals(vb) || "Map".equals(vb);
    boolean isList = "ArrayList".equals(vb) || "List".equals(vb);
    String cur = envGet(env, vr.name());
    String rhs;
    List<String> targs = vt == null ? List.of() : typeArgs(vt);
    if (isMap && mc.method().equals("put")) {
      rhs = "maps:put(" + guarded(mc.args().get(0), targs, 0, env) + ", "
          + guarded(mc.args().get(1), targs, 1, env) + ", " + cur + ")";
    } else if (isMap && mc.method().equals("remove")) {
      rhs = "maps:remove(" + genExpr(mc.args().get(0), env) + ", " + cur + ")";
    } else if (isList && mc.method().equals("add")) {
      rhs = cur + " ++ [" + guarded(mc.args().get(0), targs, 0, env) + "]"; // O(n); buffer tier later
    } else {
      return null;
    }
    String v = fresh(vr.name());
    env.put(vr.name(), v);
    return v + " = " + rhs;
  }

  /** throw new NotFound("x") -> erlang:error({zinc_exc, 'fq.tag', FieldsMap}). */
  private String genThrow(Ast.ThrowStmt s, Map<String, String> env) {
    Ast.ExceptionDecl x = exceptions.get(s.exType());
    if (x == null) {
      throw new CompileError("throw new " + s.exType() + ": unknown exception class"
          + " — declare it: class " + s.exType() + " extends Exception { ... }");
    }
    if (s.args().size() != x.fields().size()) {
      throw new CompileError("throw new " + s.exType() + ": takes " + x.fields().size()
          + " args (its fields, in order), got " + s.args().size());
    }
    String tag = atomLit(excTags.get(s.exType()));
    var entries = new ArrayList<String>();
    entries.add("'$class' => " + tag);
    for (int i = 0; i < x.fields().size(); i++) {
      entries.add(x.fields().get(i).name() + " => " + genExpr(s.args().get(i), env));
    }
    return "erlang:error({zinc_exc, " + tag + ", #{" + String.join(", ", entries) + "}})";
  }

  /** Collection insert: guard the element when the type arg is known and the value isn't. */
  private String guarded(Expr e, List<String> targs, int i, Map<String, String> env) {
    String gen = genExpr(e, env);
    if (i >= targs.size() || exprType(e) != null) return gen;
    String spec = typeSpec(targs.get(i));
    if (spec == null) return gen;
    useChk = true;
    return "'$chk'(" + gen + ", " + spec + ")";
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

    String body = "case " + cond + " of\n"
        + "    true ->\n" + block(armLines(thenCode, thenEnv, thenJump, phi), "        ") + ";\n"
        + "    false ->\n" + block(armLines(elseCode, elseEnv, elseJump, phi), "        ") + "\n"
        + "end";
    return bindPhi(phi, env, body);
  }

  /** Binds the phi tuple to fresh names (and rebinds env), or returns the body as-is. */
  private String bindPhi(List<String> phi, Map<String, String> env, String body) {
    if (phi.isEmpty()) return body;
    var newNames = new ArrayList<String>();
    for (String v : phi) newNames.add(fresh(v));
    String lhs = newNames.size() == 1 ? newNames.get(0) : "{" + String.join(", ", newNames) + "}";
    for (int i = 0; i < phi.size(); i++) {
      env.put(phi.get(i), newNames.get(i));
    }
    return lhs + " = " + body;
  }

  /** Arrow switch -> case with one clause per label; assigned vars phi-merge across arms. */
  private String genSwitch(SwitchStmt s, Map<String, String> env, List<String> loopMut) {
    String subj = genExpr(s.subject(), env);
    String subjType = exprType(s.subject());
    var assigned = new LinkedHashSet<String>();
    for (SwitchCase c : s.cases()) collectAssigned(c.body(), assigned);
    if (s.defaultBlock() != null) collectAssigned(s.defaultBlock(), assigned);
    var phi = assigned.stream().filter(env::containsKey).toList();

    var clauses = new ArrayList<String>();
    for (SwitchCase c : s.cases()) {
      var armEnv = new HashMap<>(env);
      List<String> code = genStmts(c.body().stmts(), armEnv, false, loopMut);
      String arm = block(armLines(code, armEnv, endsInJump(c.body()), phi), "        ");
      for (Expr label : c.labels()) {
        clauses.add("    " + switchLabel(label, subjType) + " ->\n" + arm);
      }
    }
    String defArm;
    if (s.defaultBlock() != null) {
      var defEnv = new HashMap<>(env);
      List<String> code = genStmts(s.defaultBlock().stmts(), defEnv, false, loopMut);
      defArm = block(armLines(code, defEnv, endsInJump(s.defaultBlock()), phi), "        ");
    } else {
      // Java: a non-matching switch statement is a no-op
      defArm = block(armLines(List.of(), env, false, phi), "        ");
    }
    clauses.add("    _ ->\n" + defArm);

    String body = "case " + subj + " of\n" + String.join(";\n", clauses) + "\nend";
    return bindPhi(phi, env, body);
  }

  /** Constant label pattern; bare enum values resolve when the subject is enum-typed. */
  private String switchLabel(Expr label, String subjType) {
    if (label instanceof VarRef vr) {
      EnumDecl ed = subjType == null ? null : enums.get(subjType);
      if (ed != null && ed.values().contains(vr.name())) return "'" + vr.name() + "'";
      throw new CompileError("switch label '" + vr.name() + "' is not a constant"
          + (ed != null ? " of enum " + subjType : ""));
    }
    if (label instanceof Unary u && u.op().equals("-") && u.operand() instanceof IntLit i) {
      return "-" + i.value(); // no parens: patterns reject them
    }
    if (label instanceof IntLit || label instanceof BoolLit || label instanceof StrLit
        || label instanceof FieldAccess) {
      return genExpr(label, new HashMap<>());
    }
    throw new CompileError("switch labels must be constants");
  }

  /**
   * Like if: vars assigned in either block phi-merge. catch error:E only — internal
   * control-flow signals ('$ret'/'$brk'/'$cont') are throw-class and pass through.
   */
  private String genTry(TryStmt s, Map<String, String> env, List<String> loopMut) {
    var assigned = new LinkedHashSet<String>();
    collectAssigned(s.tryBlock(), assigned);
    for (Ast.CatchClause c : s.clauses()) collectAssigned(c.body(), assigned);
    var phi = assigned.stream().filter(env::containsKey).toList();

    var tEnv = new HashMap<>(env);
    List<String> tCode = genStmts(s.tryBlock().stmts(), tEnv, false, loopMut);
    boolean tJump = endsInJump(s.tryBlock());

    var arms = new ArrayList<String>();
    for (Ast.CatchClause c : s.clauses()) {
      var cEnv = new HashMap<>(env);
      String ev = fresh(c.var());
      cEnv.put(c.var(), ev);
      varTypes.put(c.var(), c.exType());
      List<String> cCode;
      String head;
      if (c.exType().equals("Exception")) {
        // catch-all: zinc exceptions unwrap to their fields map; native BEAM errors
        // (badarith ~ ArithmeticException) normalize to #{message => rendered}
        useExnorm = true;
        String raw = fresh("raw");
        head = "error:" + raw + " ->";
        cCode = new ArrayList<>(List.of(ev + " = '$exnorm'(" + raw + ")"));
        cCode.addAll(genStmts(c.body().stmts(), cEnv, false, loopMut));
      } else {
        String tag = excTags.get(c.exType());
        if (tag == null) {
          throw new CompileError("catch (" + c.exType() + "): unknown exception class"
              + " — declare it: class " + c.exType() + " extends Exception { ... }");
        }
        List<String> kids = BUILTIN_EXC_CHILDREN.getOrDefault(c.exType(), List.of());
        if (kids.isEmpty()) {
          head = "error:{zinc_exc, " + atomLit(tag) + ", " + ev + "} ->";
        } else {
          String tv = fresh("t");
          var conds = new ArrayList<String>();
          conds.add(tv + " =:= " + atomLit(tag));
          for (String k : kids) conds.add(tv + " =:= " + atomLit(excTags.get(k)));
          head = "error:{zinc_exc, " + tv + ", " + ev + "} when "
              + String.join("; ", conds) + " ->";
        }
        cCode = genStmts(c.body().stmts(), cEnv, false, loopMut);
      }
      boolean cJump = endsInJump(c.body());
      arms.add(head + "\n" + block(armLines(cCode, cEnv, cJump, phi), "        "));
    }

    String body = "try\n" + block(armLines(tCode, tEnv, tJump, phi), "        ") + "\n"
        + "catch " + String.join(";\n", arms) + "\n"
        + "end";
    return bindPhi(phi, env, body);
  }

  /** Clause body for a phi-merging construct: arm code plus the phi tuple (unless it jumps). */
  private List<String> armLines(List<String> code, Map<String, String> benv, boolean jump,
      List<String> phi) {
    if (jump || phi.isEmpty()) return code;
    var vals = new ArrayList<String>();
    for (String v : phi) vals.add(envGet(benv, v));
    var all = new ArrayList<>(code);
    all.add(vals.size() == 1 ? vals.get(0) : "{" + String.join(", ", vals) + "}");
    return all;
  }

  private String genForEach(ForEachStmt s, Map<String, String> env) {
    String listCode = genExpr(s.iterable(), env);
    String iterType = exprType(s.iterable());
    if (iterType != null && iterType.endsWith("[]")) {
      listCode = "array:to_list(" + listCode + ")";
      if (s.varType().equals("var")) {
        varTypes.put(s.varName(), iterType.substring(0, iterType.length() - 2));
      }
    }
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
    List<String> clauseBody = loopClauseBody(s.body(), bodyCode, mutOut, mut, recursePrefix, helper);
    helpers.add(helper + "(" + String.join(", ", head1) + ") ->\n" + block(clauseBody, "    ")
        + ";\n" + helper + "(" + String.join(", ", base) + ") ->\n    " + result + ".");

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
    List<String> clauseBody = loopClauseBody(s.body(), bodyCode, mutOut, mut, recursePrefix, helper);
    helpers.add(helper + "(" + String.join(", ", head1) + ") ->\n"
        + "    case " + condCode + " of\n"
        + "        true ->\n" + block(clauseBody, "            ") + ";\n"
        + "        false ->\n            " + result + "\n"
        + "    end.");

    var callArgs = new ArrayList<String>();
    for (String f : free) callArgs.add(env.get(f));
    for (String m : mut) callArgs.add(env.get(m));
    return bindLoop(helper + "(" + String.join(", ", callArgs) + ")", mut, env);
  }

  private List<String> loopClauseBody(Block body, List<String> bodyCode, List<String> mutOut,
      List<String> mut, List<String> recursePrefix, String helper) {
    if (!hasBreakContinue(body)) {
      var recArgs = new ArrayList<>(recursePrefix);
      recArgs.addAll(mutOut);
      var all = new ArrayList<>(bodyCode);
      all.add(helper + "(" + String.join(", ", recArgs) + ")");
      return all;
    }
    String sig = fresh("sig");
    var pm = new ArrayList<String>();
    for (String m : mut) pm.add(fresh(m));
    String pat = pm.isEmpty() ? "ok" : (pm.size() == 1 ? pm.get(0) : "{" + String.join(", ", pm) + "}");
    var recArgs = new ArrayList<>(recursePrefix);
    recArgs.addAll(pm);
    var tryBody = new ArrayList<>(bodyCode);
    tryBody.add("{'$cont', " + tupleOf(mutOut) + "}");
    return List.of(
        sig + " = try\n" + block(tryBody, "    ") + "\n"
            + "catch throw:{'$cont', M} -> {'$cont', M}; throw:{'$brk', M} -> {'$brk', M}\n"
            + "end",
        "case " + sig + " of\n"
            + "    {'$cont', " + pat + "} -> " + helper + "(" + String.join(", ", recArgs) + ");\n"
            + "    {'$brk', " + pat + "} -> " + pat + "\n"
            + "end");
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
        if (x.typeName().equals("ArrayList")) {
          if (!x.args().isEmpty()) throw new CompileError("new ArrayList takes no args (v1)");
          yield "[]";
        }
        Ast.InstanceClassDecl ic = instClasses.get(x.typeName());
        if (ic != null) {
          int want = ic.ctor() == null ? 0 : ic.ctor().params().size();
          if (x.args().size() != want) {
            throw new CompileError("new " + x.typeName() + ": constructor takes " + want
                + " args, got " + x.args().size());
          }
          yield atomLit(instMods.get(x.typeName())) + ":new(" + genArgs(x.args(), env) + ")";
        }
        RecordDecl r = records.get(x.typeName());
        if (r == null) throw new CompileError("unknown record type: " + x.typeName());
        if (r.components().size() != x.args().size()) {
          throw new CompileError("new " + x.typeName() + ": expected "
              + r.components().size() + " args, got " + x.args().size());
        }
        var entries = new ArrayList<String>();
        entries.add("'$class' => " + atomLit(x.typeName().toLowerCase()));
        for (int i = 0; i < x.args().size(); i++) {
          entries.add(r.components().get(i).name() + " => " + genExpr(x.args().get(i), env));
        }
        yield "#{" + String.join(", ", entries) + "}";
      }
      case FieldAccess x -> {
        if (x.obj() instanceof VarRef vr && !env.containsKey(vr.name())) {
          // Tag.ok -> the atom ok; Color.RED -> 'RED' (enum values are atoms)
          if (vr.name().equals("Tag")) {
            yield atomLit(x.field());
          }
          if (vr.name().equals("Atom")) {
            throw new CompileError("Atom.* was renamed: use Tag." + x.field()
                + " (Tag.of(\"...\") for non-identifier shapes)");
          }
          EnumDecl ed = enums.get(vr.name());
          if (ed != null) {
            if (!ed.values().contains(x.field())) {
              throw new CompileError("enum " + vr.name() + " has no value " + x.field());
            }
            yield "'" + x.field() + "'";
          }
        }
        if (x.field().equals("length")) {
          String t = exprType(x.obj());
          yield t != null && t.endsWith("[]")
              ? "array:size(" + genExpr(x.obj(), env) + ")"
              : "length(" + genExpr(x.obj(), env) + ")";
        }
        yield "maps:get(" + x.field() + ", " + genExpr(x.obj(), env) + ")";
      }
      case Index x -> {
        String t = exprType(x.obj());
        yield t != null && t.endsWith("[]")
            ? "array:get(" + genExpr(x.index(), env) + ", " + genExpr(x.obj(), env) + ")"
            : "lists:nth((" + genExpr(x.index(), env) + ") + 1, " + genExpr(x.obj(), env) + ")";
      }
      case ArrayNewExpr x -> "array:new(" + genExpr(x.size(), env) + ", {default, "
          + defaultFor(x.elemType()) + "})";
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
          throw new CompileError("an Actor must be bound directly: var x = new "
              + x.actorName() + "(...)  (v1)");
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
    String body = code.size() == 1 && !code.get(0).contains("\n")
        ? " " + code.get(0) + " "
        : "\n" + block(code, "    ") + "\n";
    if (needsThrow(x.body())) {
      String rv = fresh("v");
      body = " try" + body + "catch throw:{'$ret', " + rv + "} -> " + rv + " end ";
    }
    for (var e : savedTypes.entrySet()) {
      if (e.getValue() == null) varTypes.remove(e.getKey());
      else varTypes.put(e.getKey(), e.getValue());
    }
    return "fun(" + String.join(", ", ps) + ") ->" + body + "end";
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
    if (x.target() instanceof VarRef vr) {
      String byName = genNamespaceCall(vr.name(), x, env);
      if (byName != null) return byName;
    }
    // facade dispatch by the receiver's STATIC type — works on chains, not just vars
    String tt = exprType(x.target());
    if ("String".equals(tt)) return genStringMethod(x, env);
    String tb = tt == null ? null : baseType(tt);
    if ("ArrayList".equals(tb) || "List".equals(tb)) return genListMethod(x, env);
    if ("HashMap".equals(tb) || "Map".equals(tb)) return genMapMethod(x, env);
    // actor handle: anything statically typed as an actor (var from spawn, params, fields)
    ActorDecl actor = tt == null ? null : allActors.get(tt);
    if (actor != null) {
      int arity = x.args().size();
      MethodDecl m = actor.methods().stream()
          .filter(h -> h.name().equals(x.method()) && h.params().size() == arity)
          .findFirst().orElseThrow(() -> new CompileError("actor " + actor.name()
              + " has no method " + x.method() + "/" + arity));
      String msg = x.args().isEmpty() ? "{" + x.method() + "}"
          : "{" + x.method() + ", " + genArgs(x.args(), env) + "}";
      if (m.retType().equals("void")) {
        return "gen_server:cast(" + genExpr(x.target(), env) + ", " + msg + ")";
      }
      useCall = true; // typed call: unwraps relayed exceptions (failure-ladder rung 2)
      return "'$call'(" + genExpr(x.target(), env) + ", " + msg + ")";
    }
    String recvH = null;
    if (tt != null) {
      recvH = switch (tt) {
        case "HttpClientBuilder" -> switch (x.method()) {
          case "connectTimeout" ->
              "maps:put(connect_timeout, " + genExpr(x.args().get(0), env) + ", "
                  + genExpr(x.target(), env) + ")";
          case "proxy" -> "maps:put(proxy, {" + genExpr(x.args().get(0), env) + ", "
              + genExpr(x.args().get(1), env) + "}, " + genExpr(x.target(), env) + ")";
          case "build" -> genExpr(x.target(), env);
          default -> throw new CompileError("unsupported: HttpClient builder " + x.method());
        };
        case "HttpRequestBuilder" -> switch (x.method()) {
          case "header" -> "'zinc.http':add_header(" + genExpr(x.target(), env) + ", "
              + genArgs(x.args(), env) + ")";
          case "GET" -> "maps:put(method, get, " + genExpr(x.target(), env) + ")";
          case "DELETE" -> "maps:put(method, delete, " + genExpr(x.target(), env) + ")";
          case "POST" -> "'zinc.http':with_body(" + genExpr(x.target(), env) + ", post, "
              + genExpr(x.args().get(0), env) + ")";
          case "PUT" -> "'zinc.http':with_body(" + genExpr(x.target(), env) + ", put, "
              + genExpr(x.args().get(0), env) + ")";
          case "timeout" -> "maps:put(timeout, " + genExpr(x.args().get(0), env) + ", "
              + genExpr(x.target(), env) + ")";
          case "build" -> genExpr(x.target(), env);
          default -> throw new CompileError("unsupported: HttpRequest builder " + x.method());
        };
        case "HttpClient" -> x.method().equals("send")
            ? "'zinc.http':send(" + genExpr(x.target(), env) + ", "
                + genExpr(x.args().get(0), env) + ")"
            : null;
        case "HttpResponse" -> switch (x.method()) {
          case "statusCode" -> "maps:get(status, " + genExpr(x.target(), env) + ")";
          case "body", "bodyBytes" -> "maps:get(body, " + genExpr(x.target(), env) + ")";
          case "header" -> "'zinc.http':header(" + genExpr(x.target(), env) + ", "
              + genExpr(x.args().get(0), env) + ")";
          default -> throw new CompileError("unsupported: HttpResponse." + x.method());
        };
        default -> null;
      };
    }
    if (recvH != null) {
      usedHttp = true;
      return recvH;
    }
    // instance class (static type known): direct module call, instance as first arg
    Ast.InstanceClassDecl ic = tt == null ? null : instClasses.get(tt);
    if (ic != null) {
      checkInstanceMethod(ic, x);
      return atomLit(instMods.get(tt)) + ":" + x.method() + "("
          + genExpr(x.target(), env)
          + (x.args().isEmpty() ? "" : ", " + genArgs(x.args(), env)) + ")";
    }
    // interface-typed: dynamic dispatch via '$class'; one guard discriminates a SAM fun.
    // Named top-level helper per method/arity — never inline funs in generated code.
    if (tt != null && interfaces.containsKey(tt)) {
      Ast.InterfaceDecl iface = interfaces.get(tt);
      if (iface.sigs().stream().noneMatch(s2 -> s2.name().equals(x.method())
          && s2.params().size() == x.args().size())) {
        throw new CompileError("interface " + tt + " has no method " + x.method() + "/"
            + x.args().size());
      }
      String h = "'$disp_" + x.method() + "_" + x.args().size() + "'";
      if (dispHelpers.add(h)) {
        var as = new ArrayList<String>();
        for (int i = 1; i <= x.args().size(); i++) as.add("A" + i);
        String tail = as.isEmpty() ? "" : ", " + String.join(", ", as);
        helpers.add(h + "(O" + tail + ") when is_function(O) -> O("
            + String.join(", ", as) + ");\n"
            + h + "(O" + tail + ") -> (maps:get('$class', O)):" + x.method() + "(O" + tail
            + ").");
      }
      return h + "(" + genExpr(x.target(), env)
          + (x.args().isEmpty() ? "" : ", " + genArgs(x.args(), env)) + ")";
    }
    // exception value: getMessage() + field accessors (final values, like records)
    if (tt != null && (exceptions.containsKey(tt) || tt.equals("Exception"))
        && x.args().isEmpty()) {
      if (x.method().equals("getMessage")) {
        return "maps:get(message, " + genExpr(x.target(), env) + ", <<>>)";
      }
      Ast.ExceptionDecl xd = exceptions.get(tt);
      if (xd != null && xd.fields().stream().anyMatch(f -> f.name().equals(x.method()))) {
        return "maps:get(" + x.method() + ", " + genExpr(x.target(), env) + ")";
      }
    }
    RecordDecl r = tt == null ? null : records.get(tt);
    if (r != null && x.args().isEmpty()
        && r.components().stream().anyMatch(c -> c.name().equals(x.method()))) {
      return "maps:get(" + x.method() + ", " + genExpr(x.target(), env) + ")";
    }
    if (r != null && x.method().equals("toJson") && x.args().isEmpty()) {
      emitJsonTo(r);
      return "'$tojson_" + r.name().toLowerCase() + "'(" + genExpr(x.target(), env) + ")";
    }
    // dynamic var-chaining over foreign JSON (the FFI rule: unknown flows freely);
    // the typed bind at the end of the chain is the guarded crossing
    if (tt == null && x.method().equals("get") && x.args().size() == 1) {
      return "maps:get(" + genExpr(x.args().get(0), env) + ", "
          + genExpr(x.target(), env) + ")";
    }
    throw new CompileError("unknown method call ." + x.method()
        + " (receiver type: " + (tt == null ? "unknown" : tt) + ")");
  }

  /** Derived JSON codecs: pure codegen from the record shape — no class literals,
   *  no reflection. Lenient on extra fields; missing = {zinc_badtype,{missing,K},..}. */
  private final java.util.Set<String> jsonEmitted = new java.util.HashSet<>();

  private static String jsonSpec(String t) {
    return switch (t) {
      case "int" -> "integer";
      case "double" -> "number";
      case "boolean" -> "boolean";
      case "String" -> "string";
      default -> "raw";
    };
  }

  private void emitJsonFrom(RecordDecl r) {
    String low = r.name().toLowerCase();
    if (!jsonEmitted.add("from_" + low)) return;
    useJget = true;
    var fields = new ArrayList<String>();
    fields.add("'$class' => " + atomLit(low));
    for (Param c2 : r.components()) {
      RecordDecl sub = records.get(baseType(c2.type()));
      String acc;
      if (sub != null) {
        emitJsonFrom(sub);
        acc = "'$jmap_" + sub.name().toLowerCase() + "'('$jget'(M, <<\"" + c2.name()
            + "\">>, raw))";
      } else {
        acc = "'$jget'(M, <<\"" + c2.name() + "\">>, " + jsonSpec(c2.type()) + ")";
      }
      fields.add(c2.name() + " => " + acc);
    }
    helpers.add("'$fromjson_" + low + "'(B) -> '$jmap_" + low + "'(json:decode(B)).");
    helpers.add("'$jmap_" + low + "'(M) ->\n    #{" + String.join(",\n      ", fields)
        + "}.");
  }

  private void emitJsonTo(RecordDecl r) {
    String low = r.name().toLowerCase();
    if (!jsonEmitted.add("to_" + low)) return;
    var fields = new ArrayList<String>();
    for (Param c2 : r.components()) {
      RecordDecl sub = records.get(baseType(c2.type()));
      String v = "maps:get(" + c2.name() + ", R)";
      if (sub != null) {
        emitJsonTo(sub);
        v = "'$jenc_" + sub.name().toLowerCase() + "'(" + v + ")";
      }
      fields.add("<<\"" + c2.name() + "\">> => " + v);
    }
    helpers.add("'$jenc_" + low + "'(R) ->\n    #{" + String.join(",\n      ", fields)
        + "}.");
    helpers.add("'$tojson_" + low + "'(R) -> iolist_to_binary(json:encode('$jenc_" + low
        + "'(R))).");
  }

  /** Builtin namespaces, actor handles, class statics, FFI — all keyed by a bare name. */
  private String genNamespaceCall(String name, MethodCall x, Map<String, String> env) {
    RecordDecl jr = records.get(name);
    if (jr != null && x.method().equals("fromJson") && x.args().size() == 1) {
      emitJsonFrom(jr);
      return "'$fromjson_" + jr.name().toLowerCase() + "'(" + genExpr(x.args().get(0), env)
          + ")";
    }
    switch (name) {
      case "Thread" -> {
        if (x.method().equals("sleep")) {
          return "timer:sleep(" + genExpr(x.args().get(0), env) + ")";
        }
      }
      case "Log" -> {
        // println is the dumb stdout pipe; Log.* is the BEAM logger stream, where
        // supervisor crash reports already land. Module metadata injected statically.
        String lvl = switch (x.method()) {
          case "debug" -> "debug";
          case "info" -> "info";
          case "warn" -> "warning";
          case "error" -> "error";
          default -> throw new CompileError("unsupported: Log." + x.method()
              + " (debug/info/warn/error)");
        };
        String fmt = isStr(x.args().get(0)) ? "~ts" : "~p";
        return "logger:" + lvl + "(\"" + fmt + "\", [" + genExpr(x.args().get(0), env)
            + "], #{module => " + atomLit(curModule) + "})";
      }
      case "Json" -> {
        if (x.method().equals("parse") && x.args().size() == 1) {
          return "json:decode(" + genExpr(x.args().get(0), env) + ")";
        }
        throw new CompileError("unsupported: Json." + x.method() + " (parse)");
      }
      case "HttpClient" -> {
        if (x.method().equals("newBuilder") && x.args().isEmpty()) {
          usedHttp = true;
          return "#{}";
        }
        throw new CompileError("unsupported: HttpClient." + x.method());
      }
      case "HttpRequest" -> {
        if (x.method().equals("newBuilder") && x.args().size() == 1) {
          usedHttp = true;
          return "#{url => " + genExpr(x.args().get(0), env) + ", method => get}";
        }
        throw new CompileError("unsupported: HttpRequest." + x.method()
            + " (newBuilder(url))");
      }
      case "Tag" -> {
        // Tag.of("literal") -> the atom, resolved at transpile time (atoms aren't GC'd;
        // dynamic atom minting stays an explicit FFI act: erlang list_to_atom)
        if (x.method().equals("of") && x.args().size() == 1
            && x.args().get(0) instanceof StrLit s) {
          return atomLit(s.text());
        }
        throw new CompileError("Tag.of takes exactly one compile-time string literal");
      }
      case "Tuple" -> {
        if (x.method().equals("of")) {
          return "{" + genArgs(x.args(), env) + "}";
        }
        if (x.method().equals("get") && x.args().size() == 2) {
          return "erlang:element((" + genExpr(x.args().get(1), env) + ") + 1, "
              + genExpr(x.args().get(0), env) + ")";
        }
        throw new CompileError("unsupported: Tuple." + x.method());
      }
      case "Erlang" -> {
        // Erlang.ok(e) -> unwrap {ok, V} or raise catchable {badmatch, Other}
        if (x.method().equals("ok")) {
          useOk = true;
          return "'$ok'(" + genExpr(x.args().get(0), env) + ")";
        }
      }
      case "Math" -> {
        if (List.of("max", "min", "abs").contains(x.method())) {
          return x.method() + "(" + genArgs(x.args(), env) + ")";
        }
        throw new CompileError("unsupported: Math." + x.method());
      }
      case "Integer" -> {
        if (x.method().equals("parseInt")) {
          return "binary_to_integer(" + genExpr(x.args().get(0), env) + ")";
        }
      }
      case "String" -> {
        if (x.method().equals("valueOf")) {
          useFmt = true;
          return "'$fmt'(" + genExpr(x.args().get(0), env) + ")";
        }
      }
      case "Arrays" -> {
        if (x.method().equals("asList")) {
          return "array:to_list(" + genExpr(x.args().get(0), env) + ")";
        }
        throw new CompileError("unsupported: Arrays." + x.method());
      }
      case "List" -> {
        if (x.method().equals("of") && !env.containsKey("List")) {
          return "[" + genArgs(x.args(), env) + "]";
        }
      }
      case "Map" -> {
        if (x.method().equals("of") && !env.containsKey("Map")) {
          if (x.args().size() % 2 != 0) {
            throw new CompileError("Map.of needs an even number of args");
          }
          var entries = new ArrayList<String>();
          for (int i = 0; i < x.args().size(); i += 2) {
            entries.add(genExpr(x.args().get(i), env) + " => "
                + genExpr(x.args().get(i + 1), env));
          }
          return "#{" + String.join(", ", entries) + "}";
        }
      }
      default -> {}
    }
    if (!env.containsKey(name)) {
      ClassInfo ci = classes.get(name);
      if (ci != null) {
        String key = x.method() + "/" + x.args().size();
        if (!ci.methods().containsKey(key)) {
          throw new CompileError("class " + name + " has no method " + key);
        }
        return atomLit(ci.module()) + ":" + x.method() + "(" + genArgs(x.args(), env) + ")";
      }
      // FFI: erlang module, no arity check (signatures unknown; runtime reports undef)
      String ffiMod = ffi.get(name);
      if (ffiMod != null) {
        return ffiMod + ":" + x.method() + "(" + genArgs(x.args(), env) + ")";
      }
    }
    return null; // fall through to type-based facade dispatch
  }

  private String genArgs(List<Expr> args, Map<String, String> env) {
    var out = new ArrayList<String>();
    for (Expr a : args) out.add(genExpr(a, env));
    return String.join(", ", out);
  }

  // ---- java.util / java.lang facade: users write Java, the compiler writes Erlang ----

  private String genStringMethod(MethodCall x, Map<String, String> env) {
    String r = genExpr(x.target(), env);
    return switch (x.method()) {
      case "length" -> "string:length(" + r + ")";
      case "isEmpty" -> "(" + r + " =:= <<>>)";
      case "equals" -> "(" + r + " =:= " + genExpr(x.args().get(0), env) + ")";
      case "toUpperCase" -> "string:uppercase(" + r + ")";
      case "toLowerCase" -> "string:lowercase(" + r + ")";
      case "trim", "strip" -> "string:trim(" + r + ")";
      case "substring" -> x.args().size() == 1
          ? "string:slice(" + r + ", " + genExpr(x.args().get(0), env) + ")"
          : "string:slice(" + r + ", " + genExpr(x.args().get(0), env) + ", ("
              + genExpr(x.args().get(1), env) + ") - (" + genExpr(x.args().get(0), env) + "))";
      case "contains" -> "(string:find(" + r + ", " + genExpr(x.args().get(0), env)
          + ") =/= nomatch)";
      case "startsWith" -> "(string:prefix(" + r + ", " + genExpr(x.args().get(0), env)
          + ") =/= nomatch)";
      case "endsWith" -> {
        useSfx = true;
        yield "'$sfx'(" + r + ", " + genExpr(x.args().get(0), env) + ")";
      }
      case "indexOf" -> {
        useIdx = true;
        yield "'$idx'(" + r + ", " + genExpr(x.args().get(0), env) + ")"; // byte offset
      }
      case "replace" -> "iolist_to_binary(string:replace(" + r + ", "
          + genExpr(x.args().get(0), env) + ", " + genExpr(x.args().get(1), env) + ", all))";
      case "split" -> "array:from_list(string:split(" + r + ", "
          + genExpr(x.args().get(0), env) + ", all))"; // Java split returns an array
      case "repeat" -> "binary:copy(" + r + ", " + genExpr(x.args().get(0), env) + ")";
      case "toCharArray" -> "binary_to_list(" + r + ")"; // charlist: what old OTP APIs want
      default -> throw new CompileError("unsupported: String." + x.method());
    };
  }

  private String genListMethod(MethodCall x, Map<String, String> env) {
    String r = genExpr(x.target(), env);
    return switch (x.method()) {
      case "get" -> "lists:nth((" + genExpr(x.args().get(0), env) + ") + 1, " + r + ")";
      case "size" -> "length(" + r + ")";
      case "contains" -> "lists:member(" + genExpr(x.args().get(0), env) + ", " + r + ")";
      case "isEmpty" -> "(" + r + " =:= [])";
      case "toArray" -> "array:from_list(" + r + ")";
      case "add" -> throw new CompileError("List.add mutates: use it as a statement");
      default -> throw new CompileError("unsupported: List." + x.method());
    };
  }

  private String genMapMethod(MethodCall x, Map<String, String> env) {
    String r = genExpr(x.target(), env);
    return switch (x.method()) {
      case "get" -> "maps:get(" + genExpr(x.args().get(0), env) + ", " + r + ")";
      case "getOrDefault" -> "maps:get(" + genExpr(x.args().get(0), env) + ", " + r + ", "
          + genExpr(x.args().get(1), env) + ")";
      case "containsKey" -> "maps:is_key(" + genExpr(x.args().get(0), env) + ", " + r + ")";
      case "size" -> "maps:size(" + r + ")";
      case "isEmpty" -> "(map_size(" + r + ") =:= 0)";
      case "keySet" -> "maps:keys(" + r + ")";
      case "values" -> "maps:values(" + r + ")";
      case "put", "remove" -> throw new CompileError(
          "Map." + x.method() + " mutates: use it as a statement");
      default -> throw new CompileError("unsupported: Map." + x.method());
    };
  }

  /** Segments of a string concatenation chain, flattened. */
  private String concatSegs(Expr e, Map<String, String> env) {
    if (e instanceof Binary b && b.op().equals("+") && (isStr(b.left()) || isStr(b.right()))) {
      return concatSegs(b.left(), env) + ", " + concatSegs(b.right(), env);
    }
    if (e instanceof StrLit s) return "\"" + escErl(s.text()) + "\"/utf8";
    useFmt = true;
    return "('$fmt'(" + genExpr(e, env) + "))/binary";
  }

  private static String escErl(String s) {
    return s.replace("\\", "\\\\").replace("\"", "\\\"")
        .replace("\n", "\\n").replace("\t", "\\t").replace("\r", "\\r")
        .replace("\b", "\\b").replace("\f", "\\f").replace("\0", "\\0");
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
        if (x.obj() instanceof VarRef vr && !varTypes.containsKey(vr.name())) {
          if (vr.name().equals("Tag")) yield "Tag";
          if (enums.containsKey(vr.name())) yield vr.name();
        }
        yield x.field().equals("length") ? "int" : null;
      }
      case ArrayNewExpr x -> x.elemType() + "[]";
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
        if (x.target() instanceof VarRef vr) {
          if (vr.name().equals("Tag")) yield x.method().equals("of") ? "Tag" : null;
          if (vr.name().equals("HttpClient")) yield "HttpClientBuilder";
          if (records.containsKey(vr.name()) && x.method().equals("fromJson")) {
            yield vr.name();
          }
          if (vr.name().equals("HttpRequest")) yield "HttpRequestBuilder";
          if (vr.name().equals("Tuple")) yield x.method().equals("of") ? "Tuple" : null;
          if (vr.name().equals("Math")) yield exprType(x.args().get(0));
          if (vr.name().equals("Integer")) yield x.method().equals("parseInt") ? "int" : null;
          if (vr.name().equals("String")) yield x.method().equals("valueOf") ? "String" : null;
          if (vr.name().equals("Arrays")) yield x.method().equals("asList") ? "List" : null;
          if (vr.name().equals("List") && !varTypes.containsKey("List")) {
            yield x.method().equals("of") ? "List" : null;
          }
          if (vr.name().equals("Map") && !varTypes.containsKey("Map")) {
            yield x.method().equals("of") ? "Map" : null;
          }
          if (!varTypes.containsKey(vr.name())) {
            ClassInfo ci = classes.get(vr.name());
            if (ci != null) yield ci.methods().get(x.method() + "/" + x.args().size());
          }
        }
        String tt = exprType(x.target());
        ActorDecl actor = tt == null ? null : allActors.get(tt);
        if (actor != null) {
          yield actor.methods().stream()
              .filter(m -> m.name().equals(x.method()) && m.params().size() == x.args().size())
              .map(MethodDecl::retType).findFirst().orElse(null);
        }
        if ("String".equals(tt)) {
          yield switch (x.method()) {
            case "length", "indexOf" -> "int";
            case "isEmpty", "equals", "contains", "startsWith", "endsWith" -> "boolean";
            case "toUpperCase", "toLowerCase", "trim", "strip", "substring", "replace",
                "repeat" -> "String";
            case "split" -> "String[]";
            case "toCharArray" -> "List";
            default -> null;
          };
        }
        tt = tt == null ? null : baseType(tt);
        if ("ArrayList".equals(tt) || "List".equals(tt)) {
          yield switch (x.method()) {
            case "size" -> "int";
            case "contains", "isEmpty" -> "boolean";
            case "toArray" -> "Object[]";
            default -> null;
          };
        }
        if ("HashMap".equals(tt) || "Map".equals(tt)) {
          yield switch (x.method()) {
            case "size" -> "int";
            case "containsKey", "isEmpty" -> "boolean";
            default -> null;
          };
        }
        if ("HttpClientBuilder".equals(tt)) {
          yield x.method().equals("build") ? "HttpClient" : "HttpClientBuilder";
        }
        if ("HttpRequestBuilder".equals(tt)) {
          yield x.method().equals("build") ? "HttpRequest" : "HttpRequestBuilder";
        }
        if ("HttpClient".equals(tt)) yield x.method().equals("send") ? "HttpResponse" : null;
        if ("HttpResponse".equals(tt)) {
          yield switch (x.method()) {
            case "statusCode" -> "int";
            case "body", "header" -> "String";
            default -> null;
          };
        }
        Ast.InstanceClassDecl icc = tt == null ? null : instClasses.get(tt);
        if (icc != null) {
          yield icc.methods().stream().filter(m -> m.name().equals(x.method()))
              .map(MethodDecl::retType).findFirst().orElse(null);
        }
        if (tt != null && interfaces.containsKey(tt)) {
          yield interfaces.get(tt).sigs().stream().filter(s2 -> s2.name().equals(x.method()))
              .map(MethodDecl::retType).findFirst().orElse(null);
        }
        if (tt != null && (exceptions.containsKey(tt) || tt.equals("Exception"))) {
          if (x.method().equals("getMessage")) yield "String";
          Ast.ExceptionDecl xd = exceptions.get(tt);
          if (xd != null) {
            yield xd.fields().stream().filter(f -> f.name().equals(x.method()))
                .map(FieldDecl::type).findFirst().orElse(null);
          }
        }
        RecordDecl r = tt == null ? null : records.get(tt);
        if (r != null) {
          if (x.method().equals("toJson")) yield "String";
          yield r.components().stream().filter(c -> c.name().equals(x.method()))
              .map(Param::type).findFirst().orElse(null);
        }
        yield null;
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
          && (hasReturn(it.tryBlock())
              || it.clauses().stream().anyMatch(c -> hasReturn(c.body())))) return true;
      if (s instanceof SwitchStmt it && switchHasReturn(it)) return true;
    }
    return false;
  }

  private boolean switchHasReturn(SwitchStmt s) {
    for (SwitchCase c : s.cases()) {
      if (hasReturn(c.body())) return true;
    }
    return s.defaultBlock() != null && hasReturn(s.defaultBlock());
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
          && (hasReturn(it.tryBlock())
              || it.clauses().stream().anyMatch(c -> hasReturn(c.body())))) return true;
      if (s instanceof SwitchStmt it && switchHasReturn(it)) return true;
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
        case TryStmt st -> n += countReturns(st.tryBlock())
            + st.clauses().stream().mapToInt(c -> countReturns(c.body())).sum();
        case SwitchStmt st -> {
          for (SwitchCase c : st.cases()) n += countReturns(c.body());
          if (st.defaultBlock() != null) n += countReturns(st.defaultBlock());
        }
        default -> {}
      }
    }
    return n;
  }

  /**
   * Break/continue belonging to THIS loop: reachable through any non-loop compound
   * statement (if/try/switch), but not through nested loops (incl. SeqStmt's for-while).
   */
  private boolean hasBreakContinue(Block b) {
    for (Stmt s : b.stmts()) {
      if (s instanceof BreakStmt || s instanceof ContinueStmt) return true;
      boolean nested = switch (s) {
        case IfStmt it -> hasBreakContinue(it.thenBlock())
            || (it.elseBlock() != null && hasBreakContinue(it.elseBlock()));
        case TryStmt it -> hasBreakContinue(it.tryBlock())
            || it.clauses().stream().anyMatch(c -> hasBreakContinue(c.body()));
        case SwitchStmt it -> {
          for (SwitchCase c : it.cases()) {
            if (hasBreakContinue(c.body())) yield true;
          }
          yield it.defaultBlock() != null && hasBreakContinue(it.defaultBlock());
        }
        default -> false;
      };
      if (nested) return true;
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
        case IndexAssignStmt st -> out.add(st.arrVar());
        case SwitchStmt st -> {
          for (SwitchCase c : st.cases()) collectAssigned(c.body(), out);
          if (st.defaultBlock() != null) collectAssigned(st.defaultBlock(), out);
        }
        case IfStmt st -> {
          collectAssigned(st.thenBlock(), out);
          if (st.elseBlock() != null) collectAssigned(st.elseBlock(), out);
        }
        case ForEachStmt st -> collectAssigned(st.body(), out);
        case WhileStmt st -> collectAssigned(st.body(), out);
        case SeqStmt st -> collectAssigned(new Block(st.stmts()), out);
        case TryStmt st -> {
          collectAssigned(st.tryBlock(), out);
          for (Ast.CatchClause c : st.clauses()) collectAssigned(c.body(), out);
        }
        case ExprStmt st -> {
          // collection-mutator statements rebind the receiver
          if (st.expr() instanceof MethodCall mc && mc.target() instanceof VarRef vr
              && List.of("put", "remove", "add").contains(mc.method())) {
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
        case IndexAssignStmt st -> {
          out.add(st.arrVar()); // reads the old array
          exprRefs(st.index(), out);
          exprRefs(st.value(), out);
        }
        case SwitchStmt st -> {
          exprRefs(st.subject(), out);
          for (SwitchCase c : st.cases()) blockRefs(c.body(), out);
          if (st.defaultBlock() != null) blockRefs(st.defaultBlock(), out);
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
        case Ast.ThrowStmt st -> {
          for (Expr a : st.args()) exprRefs(a, out);
        }
        case TryStmt st -> {
          blockRefs(st.tryBlock(), out);
          for (Ast.CatchClause c : st.clauses()) blockRefs(c.body(), out);
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
        // namespace/class names land in the set too; freeVars filters by env membership
        exprRefs(x.target(), out);
        for (Expr a : x.args()) exprRefs(a, out);
      }
      case ArrayNewExpr x -> exprRefs(x.size(), out);
      case SpawnExpr x -> {
        for (Expr a : x.args()) exprRefs(a, out);
      }
      case LambdaExpr x -> {
        var inner = new LinkedHashSet<String>();
        blockRefs(x.body(), inner);
        inner.removeAll(x.params());
        out.addAll(inner);
      }
    }
  }
}
