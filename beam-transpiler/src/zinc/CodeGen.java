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
  record ClassInfo(String module, Map<String, MethodDecl> methods) {} // "name/arity" -> decl

  private final String srcFile; // display path of this file, for <file>:<line> errors
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
  private String curRetType; // enclosing method's declared return type; null = unknown
  private int curLine;       // line of the statement being lowered; 0 = unknown
  private final Set<String> finalVars = new LinkedHashSet<>(); // per method body
  private boolean inActor = false;
  private Map<String, String> varTypes = new HashMap<>();       // var -> type, per method
  private int ctr = 0;
  private List<String> helpers = new ArrayList<>();

  CodeGen(String srcFile, Program program, Map<String, ClassInfo> classes,
      Map<String, RecordDecl> records,
      Map<String, EnumDecl> enums, Map<String, ActorDecl> allActors,
      Map<String, String> actorMods, Map<String, Ast.ExceptionDecl> exceptions,
      Map<String, String> excTags, Map<String, Ast.InterfaceDecl> interfaces,
      Map<String, Ast.InstanceClassDecl> instClasses, Map<String, String> instMods,
      boolean projectHasActors) {
    this.srcFile = srcFile;
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
  private static final String LIDX_HELPER =
      "'$lindexof'(L, X) -> '$lindexof'(L, X, 0).\n"
      + "'$lindexof'([X | _], X, I) -> I;\n"
      + "'$lindexof'([_ | T], X, I) -> '$lindexof'(T, X, I + 1);\n"
      + "'$lindexof'([], _, _) -> -1.";
  private static final String STRCMP_HELPER =
      "'$strcmp'(A, B) when A < B -> -1;\n"
      + "'$strcmp'(A, B) when A > B -> 1;\n"
      + "'$strcmp'(_, _) -> 0.";

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

  /** Assert failures are structured errors; EUnit reports them, the ladder owns them.
   *  Src is the operand's source text — the power-assert trick at zero runtime cost
   *  (== not =:= so ints and doubles compare by value, Java's widening). */
  private static final String ASSERT_HELPERS =
      "'$assert_eq'(E, A, _) when E == A -> ok;\n"
      + "'$assert_eq'(E, A, Src) ->\n"
      + "    erlang:error({zinc_assert, #{expected => E, got => A, expr => Src}}).\n"
      + "'$assert_true'(true, _) -> ok;\n"
      + "'$assert_true'(V, Src) ->\n"
      + "    erlang:error({zinc_assert, #{expected => true, got => V, expr => Src}}).\n"
      + "'$assert_fails'(F, Src) ->\n"
      + "    R = try F() of _ -> returned catch _:_ -> crashed end,\n"
      + "    case R of crashed -> ok;\n"
      + "        returned -> erlang:error({zinc_assert,\n"
      + "            #{expected => crash, got => returned, expr => Src}}) end.";

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
  private boolean useLidx;
  private boolean useStrcmp;
  private boolean useExnorm;
  private boolean useCall;
  private boolean useChk;
  private boolean useJget;
  private boolean useAssert;
  private boolean usedHttp;
  private boolean usedServer;
  private boolean usedSql;
  private boolean usedIo;

  private List<String> usedHelpers() {
    var out = new ArrayList<String>();
    if (useFmt) out.add(FMT_HELPER);
    if (useSfx) out.add(SFX_HELPER);
    if (useOk) out.add(OK_HELPER);
    if (useIdx) out.add(IDX_HELPER);
    if (useLidx) out.add(LIDX_HELPER);
    if (useStrcmp) out.add(STRCMP_HELPER);
    if (useExnorm) out.add(EXNORM_HELPER);
    if (useCall) out.add(CALL_HELPER);
    if (useChk) out.add(CHK_HELPER);
    if (useJget) out.add(JGET_HELPER);
    if (useAssert) out.add(ASSERT_HELPERS);
    return out;
  }

  private final java.util.Set<String> dispHelpers = new java.util.HashSet<>();

  /** Gradual checking: known-vs-known mismatch is an error; unknown flows free. */
  private void checkBind(String declared, String got, String where) {
    if (declared == null || got == null || declared.equals(got)) return; // unknown flows
    if (got.equals("void")) {
      throw new CompileError(where + ": cannot use a void method's result as a value");
    }
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
    String inner = t.substring(i + 1, t.length() - 1);
    int depth = 0, start = 0;          // split on TOP-LEVEL commas only (nested generics)
    for (int j = 0; j < inner.length(); j++) {
      char c = inner.charAt(j);
      if (c == '<') depth++;
      else if (c == '>') depth--;
      else if (c == ',' && depth == 0) { out.add(inner.substring(start, j).trim()); start = j + 1; }
    }
    out.add(inner.substring(start).trim());
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

  /** Known-vs-known per argument; unknown args flow free (the FFI rule). */
  private void checkArgs(String where, List<Param> params, List<Expr> args) {
    for (int i = 0; i < params.size() && i < args.size(); i++) {
      checkBind(params.get(i).type(), exprType(args.get(i)),
          where + " arg " + (i + 1) + " ('" + params.get(i).name() + "')");
    }
  }

  private void checkInstanceMethod(Ast.InstanceClassDecl ic, MethodCall x) {
    MethodDecl m = ic.methods().stream().filter(m2 -> m2.name().equals(x.method())
        && m2.params().size() == x.args().size()).findFirst().orElseThrow(
            () -> new CompileError("class " + ic.name() + " has no method " + x.method()
                + "/" + x.args().size()));
    if (m.isPrivate() && !ic.name().equals(curClassName)) {
      throw new CompileError(ic.name() + "." + x.method() + " is private");
    }
    checkArgs(ic.name() + "." + x.method(), m.params(), x.args());
  }

  boolean usedHttp() {
    return usedHttp;
  }

  boolean usedServer() {
    return usedServer;
  }

  boolean usedSql() {
    return usedSql;
  }

  boolean usedIo() {
    return usedIo;
  }

  private void resetModuleState() {
    helpers = new ArrayList<>();
    dispHelpers.clear();
    useFmt = false;
    useSfx = false;
    useOk = false;
    useIdx = false;
    useLidx = false;
    useStrcmp = false;
    useExnorm = false;
    useCall = false;
    useChk = false;
    useJget = false;
    jsonEmitted.clear();
  }

  /** Join statement lines as a clause body (first line padded, inner newlines shifted),
   *  skipping commas around standalone source-map markers (`%@L<n>` comment lines) —
   *  they're not statements, so they get no comma. */
  private static String block(List<String> code, String pad) {
    if (code.isEmpty()) return pad + "ok";
    var sb = new StringBuilder();
    for (int i = 0; i < code.size(); i++) {
      sb.append(code.get(i));
      if (!isMarker(code.get(i))) {
        boolean moreStmts = false;
        for (int j = i + 1; j < code.size(); j++) {
          if (!isMarker(code.get(j))) { moreStmts = true; break; }
        }
        if (moreStmts) sb.append(",");
      }
      if (i < code.size() - 1) sb.append("\n");
    }
    return pad + sb.toString().replace("\n", "\n" + pad);
  }

  private static boolean isMarker(String line) {
    return line.matches("%@L\\d+");
  }


  /** zinc.http client over httpc — Java-shaped, sync send only (fan-out = worker Actors). */
  static final String HTTP_SOURCE = "-module('zinc.http').\n"
      + "-export([send/2, add_header/3, with_body/3, header/2,\n"
      + "         open_stream/2, s_has_next_chunk/1, s_next_chunk/1, s_header/2, s_close/1]).\n\n"
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
      + "%% --- streaming response: demand-driven ({self, once}) so a slow consumer can't\n"
      + "%% flood the mailbox; chunks (refc binaries) arrive one at a time, pulled by\n"
      + "%% stream_next. One-chunk lookahead in the process dict gives hasNextChunk (no null).\n"
      + "open_stream(Client, Req) ->\n"
      + "    {ok, _} = application:ensure_all_started(inets),\n"
      + "    {ok, _} = application:ensure_all_started(ssl),\n"
      + "    ok = set_proxy(Client),\n"
      + "    Url = binary_to_list(maps:get(url, Req)),\n"
      + "    Headers = [{binary_to_list(K), binary_to_list(V)}\n"
      + "               || {K, V} <- maps:get(headers, Req, [])],\n"
      + "    HttpOpts = [{connect_timeout, maps:get(connect_timeout, Client, 5000)},\n"
      + "                {ssl, [{verify, verify_none}]}],\n"
      + "    Method = maps:get(method, Req, get),\n"
      + "    Request = case Method of\n"
      + "        get -> {Url, Headers};\n"
      + "        delete -> {Url, Headers};\n"
      + "        _ -> {Url, Headers, \"application/octet-stream\", maps:get(body, Req, <<>>)}\n"
      + "    end,\n"
      + "    Opts = [{sync, false}, {stream, {self, once}}, {body_format, binary}],\n"
      + "    {ok, Id} = httpc:request(Method, Request, HttpOpts, Opts),\n"
      + "    receive\n"
      + "        {http, {Id, stream_start, RH, Pid}} -> {httpstream, Id, Pid, hdrs(RH)};\n"
      + "        {http, {Id, {error, R}}} -> raise('zinc.http.httpexception', R)\n"
      + "    after maps:get(timeout, Req, 30000) ->\n"
      + "        httpc:cancel_request(Id), raise('zinc.http.timeoutexception', timeout)\n"
      + "    end.\n\n"
      + "hdrs(RH) -> [{iolist_to_binary(string:lowercase(K)), iolist_to_binary(V)}\n"
      + "             || {K, V} <- RH].\n\n"
      + "s_has_next_chunk({httpstream, Id, Pid, _}) ->\n"
      + "    case get({zinc_hs, Id}) of\n"
      + "        {chunk, _} -> true;\n"
      + "        eof -> false;\n"
      + "        undefined ->\n"
      + "            ok = httpc:stream_next(Pid),\n"
      + "            receive\n"
      + "                {http, {Id, stream, Part}} -> put({zinc_hs, Id}, {chunk, Part}), true;\n"
      + "                {http, {Id, stream_end, _}} -> put({zinc_hs, Id}, eof), false;\n"
      + "                {http, {Id, {error, R}}} -> raise('zinc.http.httpexception', R)\n"
      + "            after 30000 -> raise('zinc.http.timeoutexception', timeout) end\n"
      + "    end.\n\n"
      + "s_next_chunk({httpstream, Id, _, _} = S) ->\n"
      + "    case get({zinc_hs, Id}) of\n"
      + "        {chunk, C} -> erase({zinc_hs, Id}), C;\n"
      + "        _ ->\n"
      + "            case s_has_next_chunk(S) of\n"
      + "                true -> {chunk, C} = get({zinc_hs, Id}), erase({zinc_hs, Id}), C;\n"
      + "                false -> raise('zinc.http.httpexception', <<\"read past end of stream\">>)\n"
      + "            end\n"
      + "    end.\n\n"
      + "s_header({httpstream, _, _, H}, Name) ->\n"
      + "    case lists:keyfind(string:lowercase(Name), 1, H) of\n"
      + "        false -> <<>>;\n"
      + "        {_, V} -> V\n"
      + "    end.\n\n"
      + "s_close({httpstream, Id, _, _}) -> erase({zinc_hs, Id}), httpc:cancel_request(Id), ok.\n\n"
      + "raise(Tag, R) ->\n"
      + "    erlang:error({zinc_exc, Tag, #{'$class' => Tag,\n"
      + "        message => iolist_to_binary(io_lib:format(\"~p\", [R]))}}).\n";


  /** zinc.http server over cowboy: HttpServer is a stdlib Actor (new = listening);
   *  process-per-request is cowboy's native model — a handler crash 500s that request
   *  only. Routes: {Method, Path, Handler}; {id} segments become cowboy bindings. */
  static final String SERVER_SOURCE = "-module('zinc.httpserver').\n"
      + "-behaviour(gen_server).\n"
      + "-export([start_link/3, start_boot/2, route/4, init/1, handle_call/3,\n"
      + "         handle_cast/2, handle_info/2]).\n"
      + "-export([init/2, cowboy_init/2]).\n"
      + "-export([req_path_param/2, req_query/2, req_header/2, resp_header/3]).\n\n"
      + "start_boot(Name, {M, F}) -> start_link(Name, none, M:F()).\n\n"
      + "start_link(Name, Owner, Args) ->\n"
      + "    gen_server:start_link({local, Name}, ?MODULE, [Name, Owner | Args], []).\n\n"
      + "route(R, M, P, H) -> R ++ [{M, P, H}].\n\n"
      + "init([Name, Owner, Port, Routes]) ->\n"
      + "    case Owner of none -> ok; _ -> erlang:monitor(process, Owner) end,\n"
      + "    {ok, _} = application:ensure_all_started(cowboy),\n"
      + "    ByPath = lists:foldl(fun({M, P, H}, Acc) ->\n"
      + "        Acc#{P => maps:put(M, H, maps:get(P, Acc, #{}))}\n"
      + "    end, #{}, Routes),\n"
      + "    Paths = [{cowpath(P), ?MODULE, {init, Ms}} || {P, Ms} <- maps:to_list(ByPath)],\n"
      + "    Dispatch = cowboy_router:compile([{'_', Paths}]),\n"
      + "    {ok, _} = cowboy:start_clear({zinc_http, Name}, [{port, Port}],\n"
      + "        #{env => #{dispatch => Dispatch}}),\n"
      + "    {ok, #{'$self' => Name}}.\n\n"
      + "%% {id} -> :id, trailing /* -> [...]\n"
      + "cowpath(P) ->\n"
      + "    P1 = binary:replace(P, <<\"{\">>, <<\":\">>, [global]),\n"
      + "    P2 = binary:replace(P1, <<\"}\">>, <<>>, [global]),\n"
      + "    binary_to_list(binary:replace(P2, <<\"/*\">>, <<\"/[...]\">>)).\n\n"
      + "handle_call(Msg, _From, _State) -> erlang:error({unknown_call, Msg}).\n"
      + "handle_cast(Msg, _State) -> erlang:error({unknown_cast, Msg}).\n"
      + "handle_info({'DOWN', _Ref, process, _Pid, _Reason}, State) -> {stop, normal, State};\n"
      + "handle_info(_Msg, State) -> {noreply, State}.\n\n"
      + "%% cowboy 2.x handler: runs in the request process; crash = 500 for this request\n"
      + "cowboy_init(Req0, {init, Methods}) ->\n"
      + "    M = method_atom(cowboy_req:method(Req0)),\n"
      + "    case maps:find(M, Methods) of\n"
      + "        error -> {ok, cowboy_req:reply(405, #{}, <<>>, Req0), {init, Methods}};\n"
      + "        {ok, H} ->\n"
      + "            {ok, Body, Req1} = read_all(Req0, <<>>),\n"
      + "            PathParams = maps:fold(fun(K, V, A) ->\n"
      + "                maps:put(atom_to_binary(K, utf8), V, A) end,\n"
      + "                #{}, cowboy_req:bindings(Req1)),\n"
      + "            ZReq = #{method => M, path => cowboy_req:path(Req1),\n"
      + "                     path_params => PathParams,\n"
      + "                     query => cowboy_req:parse_qs(Req1),\n"
      + "                     headers => cowboy_req:headers(Req1), body => Body},\n"
      + "            ZResp = invoke(H, ZReq),\n"
      + "            RHeaders = maps:from_list(maps:get(headers, ZResp, [])),\n"
      + "            Req2 = cowboy_req:reply(maps:get(status, ZResp, 200), RHeaders,\n"
      + "                maps:get(body, ZResp, <<>>), Req1),\n"
      + "            {ok, Req2, {init, Methods}}\n"
      + "    end.\n\n"
      + "init(Req, State) -> cowboy_init(Req, State).\n\n"
      + "read_all(Req, Acc) ->\n"
      + "    case cowboy_req:read_body(Req) of\n"
      + "        {ok, B, R} -> {ok, <<Acc/binary, B/binary>>, R};\n"
      + "        {more, B, R} -> read_all(R, <<Acc/binary, B/binary>>)\n"
      + "    end.\n\n"
      + "invoke(H, ZReq) when is_function(H) -> H(ZReq);\n"
      + "invoke(H, ZReq) -> (maps:get('$class', H)):handle(H, ZReq).\n\n"
      + "method_atom(<<\"GET\">>) -> get;\n"
      + "method_atom(<<\"POST\">>) -> post;\n"
      + "method_atom(<<\"PUT\">>) -> put;\n"
      + "method_atom(<<\"DELETE\">>) -> delete;\n"
      + "method_atom(B) -> binary_to_atom(string:lowercase(B), utf8).\n"
      + "\n"
      + "%% Request/Response accessors used by the facade\n"
      + "resp_header(R, K, V) ->\n"
      + "    maps:put(headers, maps:get(headers, R, []) ++ [{K, V}], R).\n"
      + "req_path_param(R, N) -> maps:get(N, maps:get(path_params, R, #{}), <<>>).\n"
      + "req_query(R, N) ->\n"
      + "    case lists:keyfind(N, 1, maps:get(query, R, [])) of\n"
      + "        false -> <<>>;\n"
      + "        {_, V} -> V\n"
      + "    end.\n"
      + "req_header(R, N) -> maps:get(N, maps:get(headers, R, #{}), <<>>).\n";

  /** zinc.sql over epgsql: the pool is a supervision subtree, not a library trick —
   *  pool_sup (rest_for_one) owns the manager (registered under the Db handle) and a
   *  one_for_one conn_sup of permanent connections. Connections connect in init
   *  (DB down at boot = crash-loop = fail-fast; a dropped connection self-heals by
   *  restart) and check themselves in. Checkout monitors the borrower: a borrower
   *  crash kills the held connection — Postgres rolls back server-side, the
   *  supervisor reconnects. One module, roles discriminated by the init argument. */
  static final String SQL_SOURCE = "-module('zinc.sql').\n"
      + "-behaviour(gen_server).\n"
      + "-export([start_link/3, start_boot/2, init/1, handle_call/3, handle_cast/2,\n"
      + "         handle_info/2, terminate/2]).\n"
      + "-export([start_mgr/1, start_conn_sup/3, start_conn/2]).\n"
      + "-export([query/3, exec/3, transaction/2, conn_query/3, conn_exec/3]).\n\n"
      + "-define(CHECKOUT_TIMEOUT, 5000).\n\n"
      + "start_boot(Name, {M, F}) -> start_link(Name, none, M:F()).\n\n"
      + "start_link(Name, _Owner, [Url, N]) ->\n"
      + "    supervisor:start_link(?MODULE, {pool, Name, Url, N}).\n\n"
      + "start_mgr(Name) -> gen_server:start_link({local, Name}, ?MODULE, {mgr, Name}, []).\n"
      + "start_conn_sup(Name, Url, N) ->\n"
      + "    supervisor:start_link(?MODULE, {conns, Name, Url, N}).\n"
      + "start_conn(Name, Url) -> gen_server:start_link(?MODULE, {conn, Name, Url}, []).\n\n"
      + "%% supervisor roles\n"
      + "init({pool, Name, Url, N}) ->\n"
      + "    {ok, {#{strategy => rest_for_one, intensity => 1000, period => 3600},\n"
      + "          [#{id => mgr, start => {?MODULE, start_mgr, [Name]},\n"
      + "             restart => permanent, shutdown => 5000, type => worker},\n"
      + "           #{id => conns, start => {?MODULE, start_conn_sup, [Name, Url, N]},\n"
      + "             restart => permanent, shutdown => infinity, type => supervisor}]}};\n"
      + "init({conns, Name, Url, N}) ->\n"
      + "    {ok, {#{strategy => one_for_one, intensity => 1000, period => 3600},\n"
      + "          [#{id => {conn, I}, start => {?MODULE, start_conn, [Name, Url]},\n"
      + "             restart => permanent, shutdown => 5000, type => worker}\n"
      + "           || I <- lists:seq(1, N)]}};\n"
      + "%% pool manager: free conns, FIFO waiters (with deadlines), borrower monitors\n"
      + "init({mgr, _Name}) ->\n"
      + "    {ok, #{free => [], waiting => queue:new(), borrowed => #{}, conns => #{}}};\n"
      + "%% connection: connect in the constructor (fail-fast boot, restart reconnects)\n"
      + "init({conn, Mgr, Url}) ->\n"
      + "    process_flag(trap_exit, true),\n"
      + "    {ok, C} = epgsql:connect(conn_opts(Url)),\n"
      + "    gen_server:cast(Mgr, {checkin_new, self()}),\n"
      + "    {ok, #{conn => C}}.\n\n"
      + "conn_opts(Url) ->\n"
      + "    U = uri_string:parse(Url),\n"
      + "    [User, Pass] = case binary:split(maps:get(userinfo, U, <<>>), <<\":\">>) of\n"
      + "        [Us] -> [Us, <<>>];\n"
      + "        [Us, Pw] -> [Us, Pw]\n"
      + "    end,\n"
      + "    Db = case maps:get(path, U, <<>>) of\n"
      + "        <<\"/\", D/binary>> -> D;\n"
      + "        Other -> Other\n"
      + "    end,\n"
      + "    #{host => binary_to_list(maps:get(host, U, <<\"127.0.0.1\">>)),\n"
      + "      port => maps:get(port, U, 5432),\n"
      + "      username => binary_to_list(User), password => binary_to_list(Pass),\n"
      + "      database => binary_to_list(Db)}.\n\n"
      + "handle_call({checkout, Deadline}, {Pid, _} = From, S = #{free := Free}) ->\n"
      + "    case Free of\n"
      + "        [C | Rest] -> {reply, C, lend(C, Pid, S#{free := Rest})};\n"
      + "        [] -> {noreply, S#{waiting := queue:in({From, Pid, Deadline},\n"
      + "                                              maps:get(waiting, S))}}\n"
      + "    end;\n"
      + "%% connection role: every statement is parse/bind/execute — prepared, positional\n"
      + "handle_call({q, Sql, Params}, _From, S = #{conn := C}) ->\n"
      + "    {reply, case epgsql:equery(C, Sql, Params) of\n"
      + "        {ok, Cols, Rows} -> {rows, rows_to_maps(Cols, Rows)};\n"
      + "        {ok, Count} -> {count, Count};\n"
      + "        {ok, Count, Cols, Rows} -> {both, Count, rows_to_maps(Cols, Rows)};\n"
      + "        {error, E} -> {sqlerror, fmt_err(E)}\n"
      + "    end, S};\n"
      + "handle_call({tx, Cmd}, _From, S = #{conn := C}) ->\n"
      + "    {reply, case epgsql:squery(C, Cmd) of\n"
      + "        {error, E} -> {sqlerror, fmt_err(E)};\n"
      + "        _Ok -> ok\n"
      + "    end, S};\n"
      + "handle_call(Msg, _From, _State) -> erlang:error({unknown_call, Msg}).\n\n"
      + "handle_cast({checkin_new, C}, S = #{conns := Conns}) ->\n"
      + "    Ref = erlang:monitor(process, C),\n"
      + "    {noreply, give(C, S#{conns := Conns#{C => Ref}})};\n"
      + "handle_cast({checkin, C}, S = #{borrowed := B}) ->\n"
      + "    case maps:take(C, B) of\n"
      + "        {Ref, B2} -> erlang:demonitor(Ref, [flush]),\n"
      + "                     {noreply, give(C, S#{borrowed := B2})};\n"
      + "        error -> {noreply, S}  %% conn died while borrowed: stale checkin\n"
      + "    end;\n"
      + "handle_cast(Msg, _State) -> erlang:error({unknown_cast, Msg}).\n\n"
      + "lend(C, Pid, S = #{borrowed := B}) ->\n"
      + "    Ref = erlang:monitor(process, Pid),\n"
      + "    S#{borrowed := B#{C => Ref}}.\n\n"
      + "%% hand a free conn to the first waiter still inside its deadline, else shelve it\n"
      + "give(C, S = #{waiting := W, free := Free}) ->\n"
      + "    case queue:out(W) of\n"
      + "        {empty, _} -> S#{free := [C | Free]};\n"
      + "        {{value, {From, Pid, Deadline}}, W2} ->\n"
      + "            case erlang:monotonic_time(millisecond) =< Deadline of\n"
      + "                true -> gen_server:reply(From, C), lend(C, Pid, S#{waiting := W2});\n"
      + "                false -> give(C, S#{waiting := W2})\n"
      + "            end\n"
      + "    end.\n\n"
      + "%% conn role: epgsql (linked) died -> crash too, the supervisor reconnects\n"
      + "handle_info({'EXIT', _Pid, Reason}, S = #{conn := _}) -> {stop, Reason, S};\n"
      + "handle_info({'DOWN', Ref, process, Pid, _R},\n"
      + "            S = #{conns := Conns, free := Free, borrowed := B}) ->\n"
      + "    case maps:take(Pid, Conns) of\n"
      + "        {_CRef, Conns2} ->  %% a connection died: forget it everywhere\n"
      + "            S2 = S#{conns := Conns2, free := lists:delete(Pid, Free)},\n"
      + "            case maps:take(Pid, B) of\n"
      + "                {BRef, B2} -> erlang:demonitor(BRef, [flush]),\n"
      + "                              {noreply, S2#{borrowed := B2}};\n"
      + "                error -> {noreply, S2}\n"
      + "            end;\n"
      + "        error ->  %% a borrower died holding a conn: kill it (server-side\n"
      + "            %% rollback), the supervisor replaces it with a fresh one\n"
      + "            Held = [C || {C, R} <- maps:to_list(B), R =:= Ref],\n"
      + "            lists:foreach(fun(C) -> exit(C, kill) end, Held),\n"
      + "            {noreply, S#{borrowed := maps:without(Held, B)}}\n"
      + "    end;\n"
      + "handle_info(_Msg, State) -> {noreply, State}.\n\n"
      + "%% close() idiom, hand-written: release the socket on orderly stop only\n"
      + "terminate(Reason, #{conn := C}) when Reason == normal; Reason == shutdown;\n"
      + "        is_tuple(Reason), element(1, Reason) == shutdown ->\n"
      + "    epgsql:close(C), ok;\n"
      + "terminate(_Reason, _State) -> ok.\n\n"
      + "%% #column name is element 2 across epgsql 4.x; values arrive pre-decoded\n"
      + "rows_to_maps(Cols, Rows) ->\n"
      + "    Names = [element(2, Col) || Col <- Cols],\n"
      + "    [maps:from_list(lists:zip(Names, tuple_to_list(R))) || R <- Rows].\n\n"
      + "fmt_err({error, _Sev, _Code, Codename, Msg, _Extra}) ->\n"
      + "    iolist_to_binary([atom_to_binary(Codename, utf8), <<\": \">>, Msg]);\n"
      + "fmt_err(E) -> iolist_to_binary(io_lib:format(\"~p\", [E])).\n\n"
      + "%% client side (runs in the caller's process)\n"
      + "query(Pool, Sql, Params) -> with_conn(Pool, {q, Sql, Params}, rows).\n"
      + "exec(Pool, Sql, Params) -> with_conn(Pool, {q, Sql, Params}, count).\n\n"
      + "with_conn(Pool, Msg, Want) ->\n"
      + "    C = checkout(Pool),\n"
      + "    try pick(Want, gen_server:call(C, Msg, infinity))\n"
      + "    after gen_server:cast(Pool, {checkin, C})\n"
      + "    end.\n\n"
      + "conn_query(C, Sql, Params) -> pick(rows, gen_server:call(C, {q, Sql, Params}, infinity)).\n"
      + "conn_exec(C, Sql, Params) -> pick(count, gen_server:call(C, {q, Sql, Params}, infinity)).\n\n"
      + "pick(rows, {rows, R}) -> R;\n"
      + "pick(rows, {count, _}) -> [];\n"
      + "pick(rows, {both, _, R}) -> R;\n"
      + "pick(count, {rows, R}) -> length(R);\n"
      + "pick(count, {count, N}) -> N;\n"
      + "pick(count, {both, N, _}) -> N;\n"
      + "pick(_, {sqlerror, M}) -> raise(M).\n\n"
      + "checkout(Pool) ->\n"
      + "    Deadline = erlang:monotonic_time(millisecond) + ?CHECKOUT_TIMEOUT,\n"
      + "    try gen_server:call(Pool, {checkout, Deadline}, ?CHECKOUT_TIMEOUT)\n"
      + "    catch exit:{timeout, _} -> raise(<<\"pool checkout timed out\">>)\n"
      + "    end.\n\n"
      + "%% one connection for the duration; return = COMMIT, any escape = ROLLBACK +\n"
      + "%% re-raise (a thrown zinc exception stays catchable, a bug stays a crash)\n"
      + "transaction(Pool, F) ->\n"
      + "    C = checkout(Pool),\n"
      + "    try\n"
      + "        tx(C, <<\"BEGIN\">>),\n"
      + "        R = F(C),\n"
      + "        tx(C, <<\"COMMIT\">>),\n"
      + "        R\n"
      + "    catch Class:Reason:ST ->\n"
      + "        try tx(C, <<\"ROLLBACK\">>) catch _:_ -> ok end,\n"
      + "        erlang:raise(Class, Reason, ST)\n"
      + "    after\n"
      + "        gen_server:cast(Pool, {checkin, C})\n"
      + "    end.\n\n"
      + "tx(C, Cmd) ->\n"
      + "    case gen_server:call(C, {tx, Cmd}, infinity) of\n"
      + "        ok -> ok;\n"
      + "        {sqlerror, M} -> raise(M)\n"
      + "    end.\n\n"
      + "raise(M) ->\n"
      + "    erlang:error({zinc_exc, 'zinc.sql.sqlexception',\n"
      + "        #{'$class' => 'zinc.sql.sqlexception', message => M}}).\n";

  /** Stdlib exceptions: name -> FQ tag. One-level hierarchy via BUILTIN_EXC_CHILDREN. */
  static final String[][] BUILTIN_EXCEPTIONS = {
      {"HttpException", "zinc.http.httpexception"},
      {"ConnectException", "zinc.http.connectexception"},
      {"TimeoutException", "zinc.http.timeoutexception"},
      {"SqlException", "zinc.sql.sqlexception"},
      {"IOException", "zinc.io.ioexception"}};
  static final Map<String, List<String>> BUILTIN_EXC_CHILDREN =
      Map.of("HttpException", List.of("ConnectException", "TimeoutException"));

  /** zinc.io runtime: whole-file ops + getenv. The value-or-throw idiom over Erlang's
   *  {ok,_}/{error,_} -- {error,Reason} becomes a catchable zinc.io.IOException. readString
   *  reads the WHOLE file (small files); streaming primitives come in slice 2. */
  static final String IO_SOURCE = "-module('zinc.io').\n"
      + "-export([read_string/1, read_bytes/1, read_lines/1, write_string/2,\n"
      + "         append_string/2, write_bytes/2, exists/1, is_dir/1, list/1,\n"
      + "         mkdirs/1, delete/1, fsize/1, getenv/1,\n"
      + "         open_reader/1, r_has_next_line/1, r_next_line/1,\n"
      + "         open_writer/1, open_appender/1, w_write/2, w_writeln/2, close/1]).\n\n"
      + "%% --- scoped streaming handles (try-with-resources): a raw fd held in THIS process,\n"
      + "%% closed at block exit. Reads/writes happen in-process (synchronous) so a read->write\n"
      + "%% loop is backpressured + bounded -- constant memory, never slurps the whole file. ---\n"
      + "open_reader(P) ->\n"
      + "    case file:open(P, [read, raw, binary, {read_ahead, 65536}]) of\n"
      + "        {ok, H} -> {reader, H, P};\n"
      + "        {error, R} -> raise(R, P)\n"
      + "    end.\n"
      + "%% Scanner-style: a one-line lookahead buffered in the process dict (the fd is\n"
      + "%% process-bound, so this is safe within the handle's scope). No null at EOF.\n"
      + "r_has_next_line({reader, H, P}) ->\n"
      + "    case get({zinc_rd, H}) of\n"
      + "        {line, _} -> true;\n"
      + "        eof -> false;\n"
      + "        undefined ->\n"
      + "            case file:read_line(H) of\n"
      + "                {ok, L} -> put({zinc_rd, H}, {line, L}), true;\n"
      + "                eof -> put({zinc_rd, H}, eof), false;\n"
      + "                {error, R} -> raise(R, P)\n"
      + "            end\n"
      + "    end.\n"
      + "r_next_line({reader, H, P}) ->\n"
      + "    case get({zinc_rd, H}) of\n"
      + "        {line, L} -> erase({zinc_rd, H}), strip_eol(L);\n"
      + "        eof -> raise(<<\"read past end of file\">>, P);\n"
      + "        undefined ->\n"
      + "            case file:read_line(H) of\n"
      + "                {ok, L} -> strip_eol(L);\n"
      + "                eof -> raise(<<\"read past end of file\">>, P);\n"
      + "                {error, R} -> raise(R, P)\n"
      + "            end\n"
      + "    end.\n"
      + "open_writer(P)   -> open_w(P, [write, raw, binary, {delayed_write, 262144, 2000}]).\n"
      + "open_appender(P) -> open_w(P, [append, raw, binary, {delayed_write, 262144, 2000}]).\n"
      + "open_w(P, Opts) ->\n"
      + "    case file:open(P, Opts) of\n"
      + "        {ok, H} -> {writer, H, P};\n"
      + "        {error, R} -> raise(R, P)\n"
      + "    end.\n"
      + "w_write({writer, H, P}, S) ->\n"
      + "    case file:write(H, S) of ok -> ok; {error, R} -> raise(R, P) end.\n"
      + "w_writeln(W, S) -> w_write(W, [S, <<\"\\n\">>]).\n"
      + "%% closed at try-with-resources block exit (or explicit close); tolerant of double\n"
      + "%% close. A flush failure on a writer surfaces as IOException (durability).\n"
      + "close({reader, H, _}) -> erase({zinc_rd, H}), catch file:close(H), ok;\n"
      + "close({writer, H, P}) -> case file:close(H) of ok -> ok; {error, R} -> raise(R, P) end.\n"
      + "strip_eol(L) ->\n"
      + "    N = byte_size(L),\n"
      + "    case L of\n"
      + "        <<Body:(N-2)/binary, \"\\r\\n\">> -> Body;\n"
      + "        <<Body:(N-1)/binary, \"\\n\">> -> Body;\n"
      + "        _ -> L\n"
      + "    end.\n\n"
      + "read_string(P) -> ok_or_raise(file:read_file(P), P).\n"
      + "read_bytes(P)  -> ok_or_raise(file:read_file(P), P).\n"
      + "read_lines(P)  ->\n"
      + "    B = ok_or_raise(file:read_file(P), P),\n"
      + "    drop_trailing_empty(binary:split(B, [<<\"\\r\\n\">>, <<\"\\n\">>], [global])).\n"
      + "write_string(P, S)  -> unit_or_raise(file:write_file(P, S), P).\n"
      + "append_string(P, S) -> unit_or_raise(file:write_file(P, S, [append]), P).\n"
      + "write_bytes(P, B)   -> unit_or_raise(file:write_file(P, B), P).\n"
      + "exists(P) -> filelib:is_file(P).\n"
      + "is_dir(P) -> filelib:is_dir(P).\n"
      + "list(P) ->\n"
      + "    case file:list_dir(P) of\n"
      + "        {ok, Names} -> [unicode:characters_to_binary(N) || N <- Names];\n"
      + "        {error, R}  -> raise(R, P)\n"
      + "    end.\n"
      + "mkdirs(P) -> unit_or_raise(filelib:ensure_path(P), P).\n"
      + "delete(P) ->\n"
      + "    R = case filelib:is_dir(P) of true -> file:del_dir(P); false -> file:delete(P) end,\n"
      + "    unit_or_raise(R, P).\n"
      + "fsize(P) -> filelib:file_size(P).\n"
      + "getenv(N) ->\n"
      + "    case os:getenv(binary_to_list(N)) of\n"
      + "        false -> <<>>;\n"
      + "        V -> unicode:characters_to_binary(V)\n"
      + "    end.\n\n"
      + "ok_or_raise({ok, V}, _) -> V;\n"
      + "ok_or_raise({error, R}, P) -> raise(R, P).\n"
      + "unit_or_raise(ok, _) -> ok;\n"
      + "unit_or_raise({error, R}, P) -> raise(R, P).\n"
      + "drop_trailing_empty(L) ->\n"
      + "    case lists:reverse(L) of [<<>> | T] -> lists:reverse(T); _ -> L end.\n\n"
      + "raise(R, P) ->\n"
      + "    Msg = iolist_to_binary([reason(R), <<\": \">>, P]),\n"
      + "    erlang:error({zinc_exc, 'zinc.io.ioexception',\n"
      + "        #{'$class' => 'zinc.io.ioexception', message => Msg}}).\n"
      + "reason(enoent)  -> <<\"no such file or directory\">>;\n"
      + "reason(eacces)  -> <<\"permission denied\">>;\n"
      + "reason(eisdir)  -> <<\"is a directory\">>;\n"
      + "reason(enotdir) -> <<\"not a directory\">>;\n"
      + "reason(eexist)  -> <<\"already exists\">>;\n"
      + "reason(enospc)  -> <<\"no space left on device\">>;\n"
      + "reason(B) when is_binary(B) -> B;\n"
      + "reason(R) when is_atom(R) -> atom_to_list(R);\n"
      + "reason(R) -> io_lib:format(\"~p\", [R]).\n";

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

  /** OTP application callback: boots the supervision tree. Used by `zc release` (the .app
   *  declares {mod, {zinc_app, []}}); inert under `zc run` (which boots via main:run/0). */
  static final String APP_SOURCE = "-module(zinc_app).\n"
      + "-behaviour(application).\n"
      + "-export([start/2, stop/1]).\n\n"
      + "start(_Type, _Args) -> zinc_root_sup:start_link().\n\n"
      + "stop(_State) -> ok.\n";

  /** Root supervisor: zinc_dyn_sup + the Application's static children, decl order. */
  static String rootSupSource(Ast.ApplicationDecl app, Map<String, ActorDecl> actors,
      Map<String, String> actorMods) {
    var specs = new ArrayList<String>();
    specs.add("#{id => zinc_dyn_sup, start => {zinc_dyn_sup, start_link, []},\n"
        + "             restart => permanent, shutdown => infinity, type => supervisor}");
    if (app != null) {
      for (FieldDecl f : app.fields()) {
        specs.add(appChildSpec(f, actors, actorMods));
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

  /** Application field -> root child: ctor args come from the generated
   *  '\$childargs_<field>'/0 (siblings as handles, full expressions — not just literals). */
  static String appChildSpec(FieldDecl f, Map<String, ActorDecl> actors,
      Map<String, String> actorMods) {
    String startMod;
    boolean pair = false;
    if (f.type().equals("HttpServer")) {
      startMod = "zinc.httpserver";
    } else if (f.type().equals("Db")) {
      startMod = "zinc.sql"; // the pool is a supervision subtree
      pair = true;
    } else {
      ActorDecl child = actors.get(f.type());
      if (child == null) {
        throw new CompileError("Application field " + f.name() + ": type " + f.type()
            + " is not an Actor — static children are Actor-typed fields (v1)");
      }
      pair = hasActorChildren(child, actors);
      startMod = actorMods.get(f.type()) + (pair ? "_sup" : "");
    }
    return "#{id => '" + f.name() + "', start => {" + atomLit(startMod)
        + ", start_boot, ['" + f.name() + "', {main, '$childargs_" + f.name() + "'}]},\n"
        + "             restart => permanent, shutdown => " + (pair ? "infinity" : "5000")
        + ", type => " + (pair ? "supervisor" : "worker") + "}";
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
    List<Param> ps = child.ctor() == null ? List.of() : child.ctor().params();
    if (sp.args().size() != ps.size()) {
      throw new CompileError("new " + f.type() + ": constructor takes " + ps.size()
          + " args, got " + sp.args().size());
    }
    for (int i = 0; i < ps.size(); i++) {
      String lt = switch (sp.args().get(i)) {
        case IntLit ignored -> "int";
        case FloatLit ignored -> "double";
        case BoolLit ignored -> "boolean";
        case StrLit ignored -> "String";
        default -> null;
      };
      if (lt != null && !lt.equals(ps.get(i).type())
          && !(ps.get(i).type().equals("double") && lt.equals("int"))) {
        throw new CompileError("new " + f.type() + " arg " + (i + 1) + " ('"
            + ps.get(i).name() + "'): cannot bind a " + lt + " to " + ps.get(i).type());
      }
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
    for (Ast.TestDecl t : program.tests()) {
      resetModuleState();
      curModule = classes.get(t.name()).module();
      curClassName = t.name();
      out.put(curModule, genTestModule(t));
    }
    if (program.application() != null) {
      resetModuleState();
      curModule = program.application().erlMod();
      curClassName = program.application().name();
      out.put(curModule, genApplicationModule(program.application()));
    }
    // source-map header: every module here came from this one .zinc file. Paired with
    // the per-line `%@N` markers (genStmts), zc maps a crash frame back to file:line.
    // Honest-lines: these are comments, the generated Erlang stays the truth.
    out.replaceAll((mod, src) -> "%% @zinc-src " + srcFile + "\n" + src);
    return out;
  }

  /** Instance class -> module: new/N builds the map ('$class' => module atom, fields
   *  ctor-set then immutable); each method takes the instance as its first arg. */
  private String genInstanceClassModule(Ast.InstanceClassDecl c) {
    var exports = new ArrayList<String>();
    var pieces = new ArrayList<String>();

    // new(CtorArgs) -> #{'$class' => 'mod', field => ...}
    varTypes = new HashMap<>();
    finalVars.clear();
    curRetType = null; // ctor: returns already rejected below
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
      if (f.init() != null) checkBind(f.type(), exprType(f.init()), "field " + f.name());
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
    finalVars.clear();
    curRetType = m.retType();
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
        + "-export([start_link/3, start_boot/2, start_kids/1, init/1]).\n\n"
        + "start_link(Name, Owner, Args) -> supervisor:start_link(?MODULE, {pair, Name, Owner, Args}).\n\n"
        + "start_boot(Name, {M, F}) -> start_link(Name, none, M:F()).\n\n"
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
    // entry call: static main(String[]) -> user_main([]); instance void main() -> user_main()
    String mainCall = app.main() == null ? "    ok."
        : app.main().params().isEmpty() ? "    user_main()." : "    user_main([]).";
    pieces.add("main() ->\n" + boot + mainCall);
    // liveness: static children alive -> serve until stopped; none -> exit after main
    pieces.add(!app.fields().isEmpty() ? "run() -> main(), timer:sleep(infinity)."
        : "run() -> main().");
    var fEnv = new HashMap<String, String>();
    var fTypes = new HashMap<String, String>();
    for (FieldDecl f : app.fields()) {
      varTypes = new HashMap<>(fTypes);
      List<Expr> args = switch (f.init()) {
        case SpawnExpr sp -> sp.args();
        case NewExpr nx when nx.typeName().equals(f.type()) -> nx.args();
        case null, default -> throw new CompileError("Application field " + f.name()
            + " must be initialized: " + f.type() + " " + f.name() + " = new "
            + f.type() + "(...)");
      };
      List<Param> ctorParams = switch (f.type()) {
        case "Db" -> List.of(new Param("String", "url"), new Param("int", "connections"));
        case "HttpServer" -> List.of(new Param("int", "port"), new Param("Router", "routes"));
        default -> {
          ActorDecl a = allActors.get(f.type());
          yield a == null || a.ctor() == null ? List.of() : a.ctor().params();
        }
      };
      if (args.size() != ctorParams.size()) {
        throw new CompileError("new " + f.type() + ": constructor takes "
            + ctorParams.size() + " args, got " + args.size());
      }
      checkArgs("new " + f.type(), ctorParams, args);
      exports.add("'$childargs_" + f.name() + "'/0");
      pieces.add("'$childargs_" + f.name() + "'() ->\n    ["
          + genArgs(args, new HashMap<>(fEnv)) + "].");
      fEnv.put(f.name(), "'" + f.name() + "'");
      fTypes.put(f.name(), f.type());
    }
    if (app.main() != null) {
      exports.add("user_main/" + app.main().params().size());
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
    finalVars.clear();
    curRetType = app.main().retType();
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
      if (m.isPrivate()) continue; // honest lowering: private = not exported
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

  /** Test class -> EUnit module: '$zinc_test_'/0 is the generator EUnit discovers
   *  (name ends in _test_); every case runs in its own process ({spawn}), all cases
   *  in parallel ({inparallel}) — perfect isolation, ExUnit's model. Actors a test
   *  spawns are its dynamic children: the test process dies, the domain dies. */
  private String genTestModule(Ast.TestDecl t) {
    var defs = new ArrayList<String>();
    for (var m : t.methods()) defs.add(genFn(m));
    var exports = new ArrayList<String>(List.of("'$zinc_test_'/0"));
    for (var m : t.methods()) {
      if (m.isPrivate()) continue;
      exports.add(m.name() + "/" + m.params().size());
    }
    var entries = new ArrayList<String>();
    for (String name : t.testMethods()) {
      entries.add("{<<\"" + t.name() + "." + name + "\">>, {timeout, 60, {spawn, fun "
          + name + "/0}}}");
    }
    var pieces = new ArrayList<String>();
    pieces.add("'$zinc_test_'() ->\n"
        + "    {setup, fun '$zinc_boot'/0,\n"
        + "     {inparallel,\n"
        + "      [" + String.join(",\n       ", entries) + "]}}.");
    // dyn_sup must be up for tests that spawn actors; idempotent across modules
    pieces.add(projectHasActors
        ? "'$zinc_boot'() ->\n"
            + "    case whereis(zinc_dyn_sup) of\n"
            + "        undefined -> {ok, P} = zinc_dyn_sup:start_link(), erlang:unlink(P), ok;\n"
            + "        _ -> ok\n"
            + "    end."
        : "'$zinc_boot'() -> ok.");
    pieces.addAll(defs);
    pieces.addAll(helpers);
    pieces.addAll(usedHelpers());
    return "-module(" + atomLit(curModule) + ").\n"
        + "-export([" + String.join(", ", exports) + "]).\n"
        + "-compile([nowarn_unused_vars, nowarn_unused_function]).\n\n"
        + String.join("\n\n", pieces) + "\n";
  }

  /** Operand source text as an Erlang binary, for assert failure messages —
   *  prefixed <file>:<line> (the statement being lowered) so failures jump to source. */
  private String srcBin(Expr e) {
    String loc = curLine > 0 ? srcFile + ":" + curLine + ": " : "";
    return "<<\"" + (loc + exprSrc(e)).replace("\\", "\\\\").replace("\"", "\\\"") + "\">>";
  }

  /** Minimal unparser — renders the common shapes; enough to recognize the operand. */
  private static String exprSrc(Expr e) {
    return switch (e) {
      case IntLit x -> String.valueOf(x.value());
      case FloatLit x -> String.valueOf(x.value());
      case BoolLit x -> String.valueOf(x.value());
      case StrLit x -> "\"" + x.text() + "\"";
      case VarRef x -> x.name();
      case FieldAccess x -> exprSrc(x.obj()) + "." + x.field();
      case Index x -> exprSrc(x.obj()) + "[" + exprSrc(x.index()) + "]";
      case Binary x -> exprSrc(x.left()) + " " + x.op() + " " + exprSrc(x.right());
      case Unary x -> x.op() + exprSrc(x.operand());
      case Ternary x -> exprSrc(x.cond()) + " ? " + exprSrc(x.thenExpr()) + " : "
          + exprSrc(x.elseExpr());
      case Call x -> x.callee() + "(" + srcList(x.args()) + ")";
      case MethodCall x -> exprSrc(x.target()) + "." + x.method() + "(" + srcList(x.args()) + ")";
      case NewExpr x -> "new " + x.typeName() + "(" + srcList(x.args()) + ")";
      case SpawnExpr x -> "new " + x.actorName() + "(" + srcList(x.args()) + ")";
      case LambdaExpr x -> {
        List<Stmt> ss = x.body().stmts();
        String body = ss.size() == 1 ? switch (ss.get(0)) {
          case ExprStmt st -> exprSrc(st.expr());
          case ReturnStmt st -> exprSrc(st.value());
          default -> "...";
        } : "...";
        yield "(" + String.join(", ", x.params()) + ") -> " + body;
      }
      case null, default -> "expr";
    };
  }

  private static String srcList(List<Expr> es) {
    return String.join(", ", es.stream().map(CodeGen::exprSrc).toList());
  }

  private String genFn(MethodDecl m) {
    varTypes = new HashMap<>();
    finalVars.clear();
    curRetType = m.retType();
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

  /** close() — finally at process granularity (AutoCloseable idiom): runs as
   *  terminate/2 on ORDERLY stop only, never on the actor's own crash (crashed
   *  state is untrustworthy; the crash path's cleanup is rollback/supervision). */
  static MethodDecl closeHook(ActorDecl a) {
    for (MethodDecl m : a.methods()) {
      if (!m.name().equals("close")) continue;
      if (!m.retType().equals("void") || !m.params().isEmpty()) {
        throw new CompileError("actor " + a.name()
            + ".close: the shutdown hook is 'public void close()' (no params)");
      }
      return m;
    }
    return null;
  }

  private String genActorModule(ActorDecl a) {
    MethodDecl close = closeHook(a);
    // final fields: set at construction (init or ctor), frozen across messages
    for (FieldDecl f : a.fields()) {
      if (!f.mods().contains("final")) continue;
      for (MethodDecl m : a.methods()) {
        var assigned = new LinkedHashSet<String>();
        collectAssigned(m.body(), assigned);
        if (assigned.contains(f.name())) {
          throw new CompileError("actor " + a.name() + "." + m.name()
              + ": final field '" + f.name() + "' cannot be reassigned");
        }
      }
    }
    var casts = new ArrayList<String>();
    var calls = new ArrayList<String>();
    for (MethodDecl m : a.methods()) {
      if (m == close) continue;
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
    var exports = new ArrayList<>(List.of("start_link/3", "start_boot/2", "init/1",
        "handle_call/3", "handle_cast/2", "handle_info/2"));
    if (close != null) exports.add("terminate/2");

    var pieces = new ArrayList<String>();
    // [Name, Owner | Args]: init seeds '$self' from Name; Owner = spawner pid for
    // dynamic children (monitored: die with the owner) or none for static children.
    pieces.add("start_link(Name, Owner, Args) -> "
        + "gen_server:start_link({local, Name}, ?MODULE, [Name, Owner | Args], []).");
    // boot-time ctor args come from a generated function (siblings referenced by handle)
    pieces.add("start_boot(Name, {M, F}) -> start_link(Name, none, M:F()).");
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
        + (close == null
            ? "handle_info(Msg, _State) -> erlang:error({unknown_info, Msg})."
            // trap_exit means 'EXIT' arrives as info on orderly shutdown: let
            // gen_server's default exit handling run terminate, don't crash on it
            : "handle_info({'EXIT', _Pid, Reason}, State) -> {stop, Reason, State};\n"
              + "handle_info(Msg, _State) -> erlang:error({unknown_info, Msg})."));
    if (close != null) pieces.add(genCloseTerminate(a, close));
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
    finalVars.clear();
    curRetType = null; // ctor: returns already rejected below
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
    // close(): orderly shutdown must reach terminate/2, so exits become signals
    if (closeHook(a) != null) lines.add("process_flag(trap_exit, true)");
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
        if (f.init() != null) checkBind(f.type(), exprType(f.init()), "field " + f.name());
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

  /** terminate/2 for close(): orderly reasons only (normal, shutdown, {shutdown,_});
   *  any other reason is the actor's own crash — close does NOT run (best-effort by
   *  construction: anything that MUST happen belongs in a transaction, not a hook). */
  private String genCloseTerminate(ActorDecl a, MethodDecl close) {
    if (hasReturn(close.body())) {
      throw new CompileError("actor " + a.name()
          + ".close: void methods cannot return a value");
    }
    varTypes = new HashMap<>();
    finalVars.clear();
    curRetType = "void";
    var env = new HashMap<String, String>();
    var lines = new ArrayList<String>();
    var refs = new LinkedHashSet<String>();
    blockRefs(close.body(), refs);
    if (refs.contains("this")) {
      String self = fresh("self");
      lines.add(self + " = maps:get('$self', State)");
      env.put("this", self);
    }
    varTypes.put("this", a.name());
    for (FieldDecl f : a.fields()) {
      varTypes.put(f.name(), f.type());
      if (!refs.contains(f.name())) continue; // unused fields: no dead binds
      String v = fresh(f.name());
      lines.add(v + " = maps:get(" + f.name() + ", State)");
      env.put(f.name(), v);
    }
    lines.addAll(genStmts(close.body().stmts(), env, false, null));
    lines.add("ok");
    return "terminate(Reason, State) when Reason == normal; Reason == shutdown;\n"
        + "        is_tuple(Reason), element(1, Reason) == shutdown ->\n"
        + block(lines, "    ") + ";\n"
        + "terminate(_Reason, _State) -> ok.";
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
    finalVars.clear();
    curRetType = m.retType();
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
      Expr rv = ((ReturnStmt) stmts.get(stmts.size() - 1)).value();
      checkBind(m.retType(), exprType(rv), "return"); // last-stmt return: checked here
      String reply = genExpr(rv, env);
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

  /** Any CompileError below a statement gets the statement's <file>:<line> prefix —
   *  nested blocks prefix first, the outer pass-through keeps it single. */
  private CompileError at(CompileError e) {
    if (curLine > 0 && !e.getMessage().startsWith(srcFile + ":")) {
      return new CompileError(srcFile + ":" + curLine + ": " + e.getMessage());
    }
    return e;
  }

  private List<String> genStmts(List<Stmt> stmts, Map<String, String> env,
      boolean topLevel, List<String> loopMut) {
    var out = new ArrayList<String>();
    for (int i = 0; i < stmts.size(); i++) {
      Stmt s = stmts.get(i);
      boolean last = i == stmts.size() - 1;
      if (s.line() > 0) curLine = s.line();
      try {
        int before = out.size();
        genStmt(s, out, env, topLevel, last, loopMut);
        // source-map marker: a standalone `%@L<srcline>` comment right before the
        // statement's generated code. zc maps a crash's generated line back by scanning
        // up to the nearest marker. block() drops commas around markers; honest-lines
        // (comments only) keep the generated Erlang the truth.
        if (s.line() > 0 && out.size() > before) {
          out.add(before, "%@L" + s.line());
        }
      } catch (CompileError e) {
        throw at(e);
      }
    }
    return out;
  }

  private void genStmt(Stmt s, List<String> out, Map<String, String> env,
      boolean topLevel, boolean last, List<String> loopMut) {
      switch (s) {
        case VarStmt st -> {
          String v = fresh(st.name());
          if (st.isFinal()) finalVars.add(st.name());
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
            if (a.ctor() != null) {
              checkArgs("new " + a.name(), a.ctor().params(), sp.args());
            }
            String amod = actorMods.get(a.name());
            String startMod = hasActorChildren(a, allActors) ? amod + "_sup" : amod;
            out.add(v + " = zinc_dyn_sup:spawn_child(" + atomLit(startMod) + ", self(), ["
                + genArgs(sp.args(), env) + "])");
            varTypes.put(st.name(), a.name());
          } else if (st.init() instanceof ListLit bl && st.type().equals("byte[]")) {
            // byte[] is a binary, not the array module: {72, 0, -1} -> <<72, 0, 255>>
            out.add(v + " = " + byteBinary(bl, env));
            varTypes.put(st.name(), "byte[]");
          } else if (st.init() instanceof ListLit && st.type().endsWith("[]")) {
            out.add(v + " = array:from_list(" + genExpr(st.init(), env) + ")");
            varTypes.put(st.name(), st.type());
          } else if (st.init() instanceof LambdaExpr && interfaces.containsKey(st.type())) {
            // SAM bind: the interface types the lambda's params
            out.add(v + " = " + genSamArg(st.init(), env, st.type()));
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
          if (finalVars.contains(st.name())) {
            throw new CompileError("final variable '" + st.name() + "' cannot be reassigned");
          }
          String cur = envGet(env, st.name());
          String rhs;
          if (st.op().equals("+=") && (isStr(new VarRef(st.name())) || isStr(st.value()))) {
            useFmt = true;
            rhs = "<<('$fmt'(" + cur + "))/binary, " + concatSegs(st.value(), env) + ">>";
          } else {
            // reassignment is a bind against the variable's declared/inferred type
            if (st.op().equals("=")) {
              checkBind(varTypes.get(st.name()), exprType(st.value()), st.name());
            }
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
          if (st.value() == null) {
            if (curRetType != null && !curRetType.equals("void")) {
              throw new CompileError("return: method declares " + curRetType
                  + " but returns no value");
            }
          } else if ("void".equals(curRetType)) {
            throw new CompileError("return: void method cannot return a value");
          } else {
            checkBind(curRetType, exprType(st.value()), "return");
          }
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
          if (finalVars.contains(st.arrVar())) {
            throw new CompileError("final variable '" + st.arrVar()
                + "' cannot be assigned (zinc arrays are values — final freezes them)");
          }
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

  /** m.put/m.remove/list.add as a statement: emit `New = ..., env rebind`; null if not one. */
  private String genMutator(MethodCall mc, Map<String, String> env) {
    if (!(mc.target() instanceof VarRef vr) || !env.containsKey(vr.name())) return null;
    String vt = varTypes.get(vr.name());
    String vb = vt == null ? null : baseType(vt);
    boolean isMap = "HashMap".equals(vb) || "Map".equals(vb);
    boolean isArrayList = "ArrayList".equals(vb);
    String cur = envGet(env, vr.name());
    String rhs;
    List<String> targs = vt == null ? List.of() : typeArgs(vt);
    if (isMap && mc.method().equals("put")) {
      rhs = "maps:put(" + guarded(mc.args().get(0), targs, 0, env) + ", "
          + guarded(mc.args().get(1), targs, 1, env) + ", " + cur + ")";
    } else if (isMap && mc.method().equals("remove")) {
      rhs = "maps:remove(" + genExpr(mc.args().get(0), env) + ", " + cur + ")";
    } else if (isArrayList && mc.method().equals("add")) {
      // append at size: O(log n) on the array module (++ on a list was O(n^2) in loops)
      rhs = "array:set(array:size(" + cur + "), "
          + guarded(mc.args().get(0), targs, 0, env) + ", " + cur + ")";
    } else if (isArrayList && mc.method().equals("set") && mc.args().size() == 2) {
      rhs = "array:set(" + genExpr(mc.args().get(0), env) + ", "
          + guarded(mc.args().get(1), targs, 0, env) + ", " + cur + ")";
    } else if ("List".equals(vb)
        && List.of("add", "set", "remove", "clear").contains(mc.method())) {
      throw new CompileError("List is read-only — build with an ArrayList, "
          + "then List.copyOf(xs)");
    } else {
      return null;
    }
    String v = fresh(vr.name());
    env.put(vr.name(), v);
    return v + " = " + rhs;
  }

  /** throw new NotFound("x") -> erlang:error({zinc_exc, 'fq.tag', #{message => "x"}}).
   *  Args bind to the explicit ctor's params; its super(expr) supplies the message. */
  private String genThrow(Ast.ThrowStmt s, Map<String, String> env) {
    Ast.ExceptionDecl x = exceptions.get(s.exType());
    if (x == null) {
      throw new CompileError("throw new " + s.exType() + ": unknown exception class"
          + " — declare it: class " + s.exType() + " extends RuntimeException { ... }");
    }
    MethodDecl ctor = x.ctor();
    if (ctor == null) {
      throw new CompileError("throw new " + s.exType()
          + ": this exception is raised by the runtime, not user code");
    }
    if (s.args().size() != ctor.params().size()) {
      throw new CompileError("throw new " + s.exType() + ": takes " + ctor.params().size()
          + " args (its constructor params), got " + s.args().size());
    }
    var cenv = new HashMap<>(env);
    for (int i = 0; i < ctor.params().size(); i++) {
      Param p = ctor.params().get(i);
      checkBind(p.type(), exprType(s.args().get(i)),
          "throw new " + s.exType() + " ('" + p.name() + "')");
      cenv.put(p.name(), genExpr(s.args().get(i), env));
    }
    String tag = atomLit(excTags.get(s.exType()));
    String msg = ctorMessage(ctor, cenv);
    return "erlang:error({zinc_exc, " + tag + ", #{'$class' => " + tag
        + (msg == null ? "" : ", message => " + msg) + "}})";
  }

  /** Pull the message out of an exception ctor: the argument to its super(expr) call,
   *  evaluated with the ctor params bound to the throw args. null = no super(). */
  private String ctorMessage(MethodDecl ctor, Map<String, String> cenv) {
    for (Stmt st : ctor.body().stmts()) {
      if (st instanceof Ast.ExprStmt es && es.expr() instanceof Call c
          && c.callee().equals("super") && c.args().size() == 1) {
        return genExpr(c.args().get(0), cenv);
      }
    }
    return null;
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
    // try-with-resources: bind each handle before the try, close it in `after` (reverse
    // order). If an init raises, nothing is opened yet, so no close runs (Java semantics).
    var preBinds = new ArrayList<String>();
    var closes = new ArrayList<String>();
    var bodyEnv = new HashMap<>(env);
    for (Ast.Resource r : s.resources()) {
      String closeFn = switch (r.type()) {
        case "Reader", "Writer" -> { usedIo = true; yield "'zinc.io':close"; }
        case "HttpStream" -> { usedHttp = true; yield "'zinc.http':s_close"; }
        default -> throw new CompileError("try-with-resources supports scoped Reader/Writer"
            + "/HttpStream handles (v1), not " + r.type());
      };
      String rv = fresh(r.var());
      preBinds.add(rv + " = " + genExpr(r.init(), bodyEnv));
      bodyEnv.put(r.var(), rv);
      varTypes.put(r.var(), r.type());
      closes.add(0, closeFn + "(" + rv + ")"); // closed in reverse declaration order
    }

    var assigned = new LinkedHashSet<String>();
    collectAssigned(s.tryBlock(), assigned);
    for (Ast.CatchClause c : s.clauses()) collectAssigned(c.body(), assigned);
    var phi = assigned.stream().filter(env::containsKey).toList();

    var tEnv = new HashMap<>(bodyEnv);
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
              + " — declare it: class " + c.exType() + " extends RuntimeException { ... }");
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

    String catchPart = arms.isEmpty() ? ""
        : "\ncatch " + String.join(";\n", arms);
    String afterPart = closes.isEmpty() ? ""
        : "\nafter\n" + block(List.of(String.join(",\n", closes)), "        ");
    String body = "try\n" + block(armLines(tCode, tEnv, tJump, phi), "        ")
        + catchPart + afterPart + "\nend";
    String core = bindPhi(phi, env, body);
    if (preBinds.isEmpty()) return core;
    return String.join(",\n", preBinds) + ",\n" + core;
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
    } else if (iterType != null && baseType(iterType).equals("ArrayList")) {
      // one O(n) conversion, then the direct-recursion fast path
      listCode = "array:to_list(" + listCode + ")";
      List<String> ts = typeArgs(iterType);
      if (s.varType().equals("var") && ts.size() == 1) {
        varTypes.put(s.varName(), ts.get(0));
      }
    } else if (iterType != null && baseType(iterType).equals("List")) {
      List<String> ts = typeArgs(iterType);
      if (s.varType().equals("var") && ts.size() == 1) {
        varTypes.put(s.varName(), ts.get(0));
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
          // array-module backed: get/set/add O(log n), size O(1) (a list was O(n)/O(n^2))
          if (x.args().isEmpty()) yield "array:new()";
          if (x.args().size() == 1) { // copy-in bridge: new ArrayList<>(immutableList)
            yield "array:from_list(" + genExpr(x.args().get(0), env) + ")";
          }
          throw new CompileError("new ArrayList takes no args, or one list to copy");
        }
        if (x.typeName().equals("List")) {
          throw new CompileError(
              "new List: use List.of(...) or List.copyOf(xs)");
        }
        if (x.typeName().equals("Db") || x.typeName().equals("HttpServer")) {
          // long-lived resources live in the tree: ctor acquires, restart heals
          throw new CompileError("v1: " + x.typeName()
              + " is a static child — declare it as an Application field: "
              + x.typeName() + " x = new " + x.typeName() + "(...)");
        }
        Ast.InstanceClassDecl ic = instClasses.get(x.typeName());
        if (ic != null) {
          int want = ic.ctor() == null ? 0 : ic.ctor().params().size();
          if (x.args().size() != want) {
            throw new CompileError("new " + x.typeName() + ": constructor takes " + want
                + " args, got " + x.args().size());
          }
          if (ic.ctor() != null) {
            checkArgs("new " + x.typeName(), ic.ctor().params(), x.args());
          }
          yield atomLit(instMods.get(x.typeName())) + ":new(" + genArgs(x.args(), env) + ")";
        }
        RecordDecl r = records.get(x.typeName());
        if (r == null) throw new CompileError("unknown record type: " + x.typeName());
        if (r.components().size() != x.args().size()) {
          throw new CompileError("new " + x.typeName() + ": expected "
              + r.components().size() + " args, got " + x.args().size());
        }
        checkArgs("new " + x.typeName(), r.components(), x.args());
        var entries = new ArrayList<String>();
        entries.add("'$class' => " + atomLit(x.typeName().toLowerCase()));
        for (int i = 0; i < x.args().size(); i++) {
          entries.add(r.components().get(i).name() + " => " + genExpr(x.args().get(i), env));
        }
        yield "#{" + String.join(", ", entries) + "}";
      }
      case FieldAccess x -> {
        if (x.obj() instanceof VarRef vr && !env.containsKey(vr.name())) {
          // Color.RED -> 'RED' (enum values are atoms). Atoms are Tag.of("..."):
          // the field form Tag.x isn't legal Java (Tag can't enumerate every atom).
          if (vr.name().equals("Tag") || vr.name().equals("Atom")) {
            throw new CompileError(vr.name() + "." + x.field()
                + " is not valid: write atoms as Tag.of(\"" + x.field() + "\")");
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
      case Ternary x -> "(case " + genExpr(x.cond(), env) + " of true -> "
          + genExpr(x.thenExpr(), env) + "; false -> " + genExpr(x.elseExpr(), env) + " end)";
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
        ClassInfo ci = curClassName == null ? null : classes.get(curClassName);
        MethodDecl md = ci == null ? null
            : ci.methods().get(x.callee() + "/" + x.args().size());
        if (md != null) checkArgs(x.callee(), md.params(), x.args());
        var args = new ArrayList<String>();
        for (Expr a : x.args()) args.add(genExpr(a, env));
        yield fnName(x.callee()) + "(" + String.join(", ", args) + ")";
      }
      case MethodCall x -> genMethodCall(x, env);
      case SpawnExpr x ->
          throw new CompileError("an Actor must be bound directly: var x = new "
              + x.actorName() + "(...)  (v1)");
      case LambdaExpr x -> genLambda(x, env, null);
    };
  }

  /** Lambda in a SAM-interface position: params take the interface method's types,
   *  so facade dispatch works inside the body (req -> req.pathParam("id")). */
  private String genSamArg(Expr e, Map<String, String> env, String ifaceName) {
    if (e instanceof LambdaExpr lx) {
      Ast.InterfaceDecl iface = interfaces.get(ifaceName);
      if (iface != null && iface.sigs().size() == 1
          && iface.sigs().get(0).params().size() == lx.params().size()) {
        return genLambda(lx, env,
            iface.sigs().get(0).params().stream().map(Param::type).toList(),
            iface.sigs().get(0).retType());
      }
    }
    return genExpr(e, env);
  }

  private String genLambda(LambdaExpr x, Map<String, String> env, List<String> paramTypes) {
    return genLambda(x, env, paramTypes, null);
  }

  /** Erlang fun; Java's effectively-final capture rule == Erlang's semantics, enforced.
   *  paramTypes/retType (from a SAM context) type the body; null = unknown (FFI rule). */
  private String genLambda(LambdaExpr x, Map<String, String> env, List<String> paramTypes,
      String retType) {
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
    String savedRet = curRetType;
    curRetType = retType; // returns inside the lambda are the LAMBDA's returns
    var ps = new ArrayList<String>();
    for (int i = 0; i < x.params().size(); i++) {
      String p = x.params().get(i);
      savedTypes.put(p, varTypes.get(p));
      String pt = paramTypes == null || i >= paramTypes.size() ? null : paramTypes.get(i);
      if (pt == null) varTypes.remove(p); else varTypes.put(p, pt);
      String v = fresh(p);
      lenv.put(p, v);
      ps.add(v);
    }
    // expression-bodied lambda: the implicit value is the SAM return
    if (retType != null && !x.body().stmts().isEmpty()
        && x.body().stmts().get(x.body().stmts().size() - 1) instanceof ExprStmt es) {
      checkBind(retType, exprType(es.expr()), "lambda result");
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
    curRetType = savedRet;
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
    if ("List".equals(tb)) return genListMethod(x, env);
    if ("ArrayList".equals(tb)) return genArrayListMethod(x, env);
    if ("HashMap".equals(tb) || "Map".equals(tb)) return genMapMethod(x, env);
    // actor handle: anything statically typed as an actor (var from spawn, params, fields)
    ActorDecl actor = tt == null ? null : allActors.get(tt);
    if (actor != null) {
      int arity = x.args().size();
      if (x.method().equals("close") && arity == 0 && closeHook(actor) != null) {
        throw new CompileError("actor " + actor.name() + ".close() runs automatically "
            + "on orderly stop — it cannot be called directly");
      }
      MethodDecl m = actor.methods().stream()
          .filter(h -> h.name().equals(x.method()) && h.params().size() == arity)
          .findFirst().orElseThrow(() -> new CompileError("actor " + actor.name()
              + " has no method " + x.method() + "/" + arity));
      checkArgs(actor.name() + "." + x.method(), m.params(), x.args());
      String msg = x.args().isEmpty() ? "{" + x.method() + "}"
          : "{" + x.method() + ", " + genArgs(x.args(), env) + "}";
      if (m.retType().equals("void")) {
        return "gen_server:cast(" + genExpr(x.target(), env) + ", " + msg + ")";
      }
      useCall = true; // typed call: unwraps relayed exceptions (failure-ladder rung 2)
      return "'$call'(" + genExpr(x.target(), env) + ", " + msg + ")";
    }
    // zinc.sql: db.query/exec are varargs (sql, params...); transaction takes a
    // one-arg lambda — begin/commit/rollback are unmismatchable by construction
    if ("Db".equals(tt) || "Tx".equals(tt)) {
      usedSql = true;
      boolean isTx = "Tx".equals(tt);
      switch (x.method()) {
        case "query", "exec" -> {
          if (x.args().isEmpty()) {
            throw new CompileError(tt + "." + x.method() + " takes (sql, params...)");
          }
          checkBind("String", exprType(x.args().get(0)), tt + "." + x.method() + " sql");
          String fn = (isTx ? "conn_" : "") + (x.method().equals("query") ? "query" : "exec");
          var ps = new ArrayList<String>();
          for (Expr p : x.args().subList(1, x.args().size())) ps.add(genExpr(p, env));
          return "'zinc.sql':" + fn + "(" + genExpr(x.target(), env) + ", "
              + genExpr(x.args().get(0), env) + ", [" + String.join(", ", ps) + "])";
        }
        case "transaction" -> {
          if (isTx) {
            throw new CompileError("transactions do not nest (v1)");
          }
          if (x.args().size() != 1 || !(x.args().get(0) instanceof LambdaExpr lx)
              || lx.params().size() != 1) {
            throw new CompileError(
                "db.transaction takes a one-arg lambda: db.transaction(tx -> { ... })");
          }
          return "'zinc.sql':transaction(" + genExpr(x.target(), env) + ", "
              + genLambda(lx, env, List.of("Tx")) + ")";
        }
        default -> throw new CompileError("unsupported: " + tt + "." + x.method()
            + " (query/exec" + (isTx ? "" : "/transaction") + ")");
      }
    }
    // scoped file handles (try-with-resources): reads/writes hit the fd IN this process,
    // so a read->write loop is synchronous = backpressured + bounded (never a mailbox).
    if ("Writer".equals(tt)) {
      usedIo = true;
      String w = genExpr(x.target(), env);
      String a0 = x.args().isEmpty() ? null : genExpr(x.args().get(0), env);
      return switch (x.method()) {
        case "write" -> "'zinc.io':w_write(" + w + ", " + a0 + ")";
        case "writeLine" -> "'zinc.io':w_writeln(" + w + ", " + a0 + ")";
        case "close" -> "'zinc.io':close(" + w + ")";
        default -> throw new CompileError("unsupported: Writer." + x.method()
            + " (write/writeLine/close)");
      };
    }
    if ("Reader".equals(tt)) {
      usedIo = true;
      String r = genExpr(x.target(), env);
      return switch (x.method()) {
        case "hasNextLine" -> "'zinc.io':r_has_next_line(" + r + ")";
        case "nextLine" -> "'zinc.io':r_next_line(" + r + ")";
        case "close" -> "'zinc.io':close(" + r + ")";
        default -> throw new CompileError("unsupported: Reader." + x.method()
            + " (hasNextLine/nextLine/close)");
      };
    }
    // Map.Entry (from entrySet()): a {K, V} tuple
    if (tt != null && "Entry".equals(baseType(tt))) {
      String e = genExpr(x.target(), env);
      return switch (x.method()) {
        case "getKey" -> "element(1, " + e + ")";
        case "getValue" -> "element(2, " + e + ")";
        default -> throw new CompileError("unsupported: Map.Entry." + x.method()
            + " (getKey/getValue)");
      };
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
        case "Router" -> switch (x.method()) {
          case "get", "post", "put", "delete" ->
              "'zinc.httpserver':route(" + genExpr(x.target(), env) + ", " + x.method()
                  + ", " + genExpr(x.args().get(0), env) + ", "
                  + genSamArg(x.args().get(1), env, "Handler") + ")";
          default -> throw new CompileError("unsupported: Router." + x.method());
        };
        case "Request" -> switch (x.method()) {
          case "method" -> "atom_to_binary(maps:get(method, " + genExpr(x.target(), env)
              + "), utf8)";
          case "path" -> "maps:get(path, " + genExpr(x.target(), env) + ")";
          case "body", "bodyBytes" -> "maps:get(body, " + genExpr(x.target(), env) + ")";
          case "pathParam" -> "'zinc.httpserver':req_path_param(" + genExpr(x.target(), env)
              + ", " + genExpr(x.args().get(0), env) + ")";
          case "queryParam" -> "'zinc.httpserver':req_query(" + genExpr(x.target(), env)
              + ", " + genExpr(x.args().get(0), env) + ")";
          case "header" -> "'zinc.httpserver':req_header(" + genExpr(x.target(), env)
              + ", " + genExpr(x.args().get(0), env) + ")";
          default -> throw new CompileError("unsupported: Request." + x.method());
        };
        case "Response" -> switch (x.method()) {
          case "header" -> "'zinc.httpserver':resp_header(" + genExpr(x.target(), env)
              + ", " + genArgs(x.args(), env) + ")";
          case "body" -> "maps:put(body, " + genExpr(x.args().get(0), env) + ", "
              + genExpr(x.target(), env) + ")";
          default -> throw new CompileError("unsupported: Response." + x.method());
        };
        case "HttpClient" -> switch (x.method()) {
          case "send" -> "'zinc.http':send(" + genExpr(x.target(), env) + ", "
              + genExpr(x.args().get(0), env) + ")";
          case "openStream" -> "'zinc.http':open_stream(" + genExpr(x.target(), env) + ", "
              + genExpr(x.args().get(0), env) + ")";
          default -> null;
        };
        case "HttpStream" -> switch (x.method()) {
          case "hasNextChunk" -> "'zinc.http':s_has_next_chunk(" + genExpr(x.target(), env) + ")";
          case "nextChunk" -> "'zinc.http':s_next_chunk(" + genExpr(x.target(), env) + ")";
          case "header" -> "'zinc.http':s_header(" + genExpr(x.target(), env) + ", "
              + genExpr(x.args().get(0), env) + ")";
          case "close" -> "'zinc.http':s_close(" + genExpr(x.target(), env) + ")";
          default -> throw new CompileError("unsupported: HttpStream." + x.method()
              + " (hasNextChunk/nextChunk/header)");
        };
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
      MethodDecl sig = iface.sigs().stream().filter(s2 -> s2.name().equals(x.method())
          && s2.params().size() == x.args().size()).findFirst().orElseThrow(
              () -> new CompileError("interface " + tt + " has no method " + x.method()
                  + "/" + x.args().size()));
      checkArgs(tt + "." + x.method(), sig.params(), x.args());
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
    // dynamic var-chaining over foreign JSON (the FFI rule: unknown flows freely);
    // .asInt()/.asText()/.asBool()/.asNum() is the guarded crossing back into a known type
    if (tt == null && x.args().isEmpty()) {
      String spec = switch (x.method()) {
        case "asInt" -> "integer";
        case "asText" -> "string";
        case "asBool" -> "boolean";
        case "asNum" -> "float";
        default -> null;
      };
      if (spec != null) {
        useChk = true;
        return "'$chk'(" + genExpr(x.target(), env) + ", " + spec + ")";
      }
    }
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

  /** User.class -> "User" (null if e isn't a class literal). The class literal is the
   *  only place a bare type name appears as a value; it names the codec's target. */
  private static String classLitName(Expr e) {
    return e instanceof FieldAccess fa && fa.field().equals("class")
        && fa.obj() instanceof VarRef vr ? vr.name() : null;
  }

  private RecordDecl classLitRecord(Expr e, String where) {
    String n = classLitName(e);
    RecordDecl r = n == null ? null : records.get(n);
    if (r == null) {
      throw new CompileError(where + " expects a record class literal, e.g. User.class");
    }
    return r;
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
    switch (name) {
      case "Sys" -> {
        // zinc's sleep facade: unlike java.lang.Thread.sleep it throws no checked
        // InterruptedException, so it stays legal Java without a throws clause.
        if (x.method().equals("sleep")) {
          return "timer:sleep(" + genExpr(x.args().get(0), env) + ")";
        }
        throw new CompileError("unsupported: Sys." + x.method() + " (sleep)");
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
      case "Router" -> {
        if (x.method().equals("create") && x.args().isEmpty()) {
          usedServer = true;
          return "[]";
        }
        throw new CompileError("unsupported: Router." + x.method() + " (create)");
      }
      case "Response" -> {
        usedServer = true;
        if (x.method().equals("ok") && x.args().size() == 1) {
          return "#{status => 200, body => " + genExpr(x.args().get(0), env) + "}";
        }
        if (x.method().equals("status") && x.args().size() == 1) {
          return "#{status => " + genExpr(x.args().get(0), env) + ", body => <<>>}";
        }
        throw new CompileError("unsupported: Response." + x.method() + " (ok/status)");
      }
      case "Json" -> {
        if (x.method().equals("parse") && x.args().size() == 1) {
          return "json:decode(" + genExpr(x.args().get(0), env) + ")";
        }
        // Json.encode(rec) -> derived codec from the record's static type
        if (x.method().equals("encode") && x.args().size() == 1) {
          RecordDecl r = records.get(baseType(exprType(x.args().get(0))));
          if (r == null) {
            throw new CompileError("Json.encode expects a record value");
          }
          emitJsonTo(r);
          return "'$tojson_" + r.name().toLowerCase() + "'("
              + genExpr(x.args().get(0), env) + ")";
        }
        // Json.decode(User.class, s) / Json.decodeAll(User.class, rows): the class
        // literal names the target record (rows are maps keyed by column name).
        if (x.method().equals("decode") && x.args().size() == 2) {
          RecordDecl r = classLitRecord(x.args().get(0), "Json.decode");
          emitJsonFrom(r);
          return "'$fromjson_" + r.name().toLowerCase() + "'("
              + genExpr(x.args().get(1), env) + ")";
        }
        if (x.method().equals("decodeAll") && x.args().size() == 2) {
          RecordDecl r = classLitRecord(x.args().get(0), "Json.decodeAll");
          emitJsonFrom(r);
          String low = r.name().toLowerCase();
          if (jsonEmitted.add("rows_" + low)) {
            helpers.add("'$fromrows_" + low + "'(Rows) -> ['$jmap_" + low
                + "'(R) || R <- Rows].");
          }
          return "'$fromrows_" + low + "'(" + genExpr(x.args().get(1), env) + ")";
        }
        // decodeList(T.class, json) -> List<T>: parse a JSON ARRAY of objects into records
        if (x.method().equals("decodeList") && x.args().size() == 2) {
          RecordDecl r = classLitRecord(x.args().get(0), "Json.decodeList");
          emitJsonFrom(r);
          String low = r.name().toLowerCase();
          if (jsonEmitted.add("jsonlist_" + low)) {
            helpers.add("'$fromjsonlist_" + low + "'(B) -> ['$jmap_" + low
                + "'(M) || M <- json:decode(B)].");
          }
          return "'$fromjsonlist_" + low + "'(" + genExpr(x.args().get(1), env) + ")";
        }
        throw new CompileError("unsupported: Json." + x.method()
            + " (parse/encode/decode/decodeAll/decodeList)");
      }
      case "Assert" -> {
        useAssert = true;
        if (x.method().equals("equals") && x.args().size() == 2) {
          return "'$assert_eq'(" + genExpr(x.args().get(0), env) + ", "
              + genExpr(x.args().get(1), env) + ", " + srcBin(x.args().get(1)) + ")";
        }
        if (x.method().equals("isTrue") && x.args().size() == 1) {
          return "'$assert_true'(" + genExpr(x.args().get(0), env) + ", "
              + srcBin(x.args().get(0)) + ")";
        }
        if (x.method().equals("fails") && x.args().size() == 1) {
          if (!(x.args().get(0) instanceof LambdaExpr lx) || !lx.params().isEmpty()) {
            throw new CompileError("Assert.fails takes a zero-arg lambda: "
                + "Assert.fails(() -> c.boom())");
          }
          return "'$assert_fails'(" + genExpr(x.args().get(0), env) + ", "
              + srcBin(x.args().get(0)) + ")";
        }
        throw new CompileError("unsupported: Assert." + x.method()
            + " (equals/isTrue/fails)");
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
          return x.method() + "(" + genArgs(x.args(), env) + ")"; // auto-imported BIFs
        }
        if (List.of("sqrt", "pow", "floor", "ceil").contains(x.method())) {
          return "math:" + x.method() + "(" + genArgs(x.args(), env) + ")";
        }
        if (x.method().equals("round")) { // Java Math.round(x) = floor(x + 0.5), returns long
          return "floor((" + genExpr(x.args().get(0), env) + ") + 0.5)";
        }
        throw new CompileError("unsupported: Math." + x.method()
            + " (max/min/abs/sqrt/pow/floor/ceil/round)");
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
        if (x.method().equals("join") && x.args().size() >= 2) {
          // String.join(sep, list) or String.join(sep, a, b, ...) -> one binary
          String sep = genExpr(x.args().get(0), env);
          String list = x.args().size() == 2 ? genExpr(x.args().get(1), env)
              : "[" + genArgs(x.args().subList(1, x.args().size()), env) + "]";
          return "iolist_to_binary(lists:join(" + sep + ", " + list + "))";
        }
        if (x.method().equals("format") && !x.args().isEmpty()) {
          if (!(x.args().get(0) instanceof StrLit f)) {
            throw new CompileError("String.format needs a compile-time format-string literal");
          }
          String erlFmt = translateFormat(f.text()); // %s/%d/%f/%.Nf/%x/%b/%n/%%
          String fmtArgs = genArgs(x.args().subList(1, x.args().size()), env);
          return "iolist_to_binary(io_lib:format(\"" + erlFmt + "\", [" + fmtArgs + "]))";
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
        if (x.method().equals("copyOf") && x.args().size() == 1
            && !env.containsKey("List")) {
          // bridge out of an ArrayList (a plain list passes through unchanged)
          String a = genExpr(x.args().get(0), env);
          String at = exprType(x.args().get(0));
          return "ArrayList".equals(at == null ? null : baseType(at))
              ? "array:to_list(" + a + ")" : a;
        }
        throw new CompileError("unsupported: List." + x.method()
            + " (of/copyOf)");
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
      case "Files" -> {
        usedIo = true;
        String fn = switch (x.method()) {
          case "readString" -> "read_string";
          case "readBytes" -> "read_bytes";
          case "readLines" -> "read_lines";
          case "writeString" -> "write_string";
          case "appendString" -> "append_string";
          case "writeBytes" -> "write_bytes";
          case "exists" -> "exists";
          case "isDirectory" -> "is_dir";
          case "list" -> "list";
          case "createDirectories" -> "mkdirs";
          case "delete" -> "delete";
          case "size" -> "fsize";
          case "openReader" -> "open_reader";   // scoped streaming -- use in try-with-resources
          case "openWriter" -> "open_writer";
          case "openAppender" -> "open_appender";
          default -> throw new CompileError("unsupported: Files." + x.method());
        };
        return "'zinc.io':" + fn + "(" + genArgs(x.args(), env) + ")";
      }
      case "System" -> {
        if (x.method().equals("getenv") && x.args().size() == 1) {
          usedIo = true; // unset var -> empty string (zinc has no null); v1
          return "'zinc.io':getenv(" + genExpr(x.args().get(0), env) + ")";
        }
        throw new CompileError("unsupported: System." + x.method() + " (getenv)");
      }
      default -> {}
    }
    if (!env.containsKey(name)) {
      ClassInfo ci = classes.get(name);
      if (ci != null) {
        String key = x.method() + "/" + x.args().size();
        MethodDecl md = ci.methods().get(key);
        if (md == null) {
          throw new CompileError("class " + name + " has no method " + key);
        }
        if (md.isPrivate() && !name.equals(curClassName)) {
          throw new CompileError(name + "." + x.method() + " is private");
        }
        checkArgs(name + "." + x.method(), md.params(), x.args());
        // same-class qualified call: local function (privates are not exported)
        if (name.equals(curClassName)) {
          return fnName(x.method()) + "(" + genArgs(x.args(), env) + ")";
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

  /** byte[] literal -> Erlang binary. Each 8-bit segment takes the value mod 256, so a
   *  Java signed byte like -1 becomes 0xFF (the legal-Java way to write a high byte). */
  private String byteBinary(ListLit bl, Map<String, String> env) {
    var segs = new ArrayList<String>();
    for (Expr e : bl.elems()) segs.add("(" + genExpr(e, env) + ")");
    return "<<" + String.join(", ", segs) + ">>";
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
      case "charAt" -> // zinc strings are UTF-8: the i-th character as a 1-char string
          "string:slice(" + r + ", " + genExpr(x.args().get(0), env) + ", 1)";
      case "compareTo" -> {
        useStrcmp = true;
        yield "'$strcmp'(" + r + ", " + genExpr(x.args().get(0), env) + ")"; // -1 / 0 / 1
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

  /** O(n)-on-List warning (user policy 2026-06-12): get/size are O(1) in
   *  Java muscle memory but linear on a linked list — flag every use, name the fix.
   *  contains is exempt: O(n) in Java too, and zinc has nothing faster to point at. */
  private void warnLinear(String what, String fix) {
    System.err.println("warning: " + srcFile + (curLine > 0 ? ":" + curLine : "")
        + ": " + what + " — " + fix);
  }

  /** List: the receive/iterate type — an Erlang list. Read ops only;
   *  for-each is the intended access (direct recursion, the validated fast path). */
  private String genListMethod(MethodCall x, Map<String, String> env) {
    String r = genExpr(x.target(), env);
    return switch (x.method()) {
      case "get" -> {
        warnLinear("List.get is O(n)",
            "for indexing use new ArrayList<>(xs); to walk it use for-each");
        yield "lists:nth((" + genExpr(x.args().get(0), env) + ") + 1, " + r + ")";
      }
      case "size" -> {
        warnLinear("List.size is O(n) (walks the list)",
            "ArrayList.size is O(1)");
        yield "length(" + r + ")";
      }
      case "contains" -> "lists:member(" + genExpr(x.args().get(0), env) + ", " + r + ")";
      case "indexOf" -> {
        useLidx = true;
        yield "'$lindexof'(" + r + ", " + genExpr(x.args().get(0), env) + ")"; // -1 if absent
      }
      case "isEmpty" -> "(" + r + " =:= [])";
      case "getFirst" -> "hd(" + r + ")";
      case "getLast" -> "lists:last(" + r + ")";
      case "toArray" -> "array:from_list(" + r + ")";
      case "add", "set", "remove", "clear", "sort", "reverse", "addAll" ->
          throw new CompileError("List is read-only — build with an ArrayList, then "
              + "List.copyOf(xs)");
      default -> throw new CompileError("unsupported: List." + x.method());
    };
  }

  /** ArrayList: the build/index type — array-module backed, honest costs. */
  private String genArrayListMethod(MethodCall x, Map<String, String> env) {
    String r = genExpr(x.target(), env);
    return switch (x.method()) {
      case "get" -> "array:get(" + genExpr(x.args().get(0), env) + ", " + r + ")";
      case "getFirst" -> "array:get(0, " + r + ")";
      case "getLast" -> "array:get(array:size(" + r + ") - 1, " + r + ")";
      case "size" -> "array:size(" + r + ")";
      case "isEmpty" -> "(array:size(" + r + ") =:= 0)";
      case "contains" -> "lists:member(" + genExpr(x.args().get(0), env)
          + ", array:to_list(" + r + "))";
      case "toArray" -> r; // already an array term
      case "add", "set" -> throw new CompileError(
          "ArrayList." + x.method() + " mutates: use it as a statement");
      case "remove" -> throw new CompileError(
          "ArrayList.remove: not supported (v1) — rebuild without the element");
      default -> throw new CompileError("unsupported: ArrayList." + x.method());
    };
  }

  /** [K, V] from a Map&lt;K,V&gt;/HashMap&lt;K,V&gt; type, or [null, null] for a raw map. */
  private List<String> entryTypes(String mapType) {
    List<String> ts = mapType == null ? List.of() : typeArgs(mapType);
    return ts.size() == 2 ? ts : java.util.Arrays.asList(null, null);
  }

  private String genMapMethod(MethodCall x, Map<String, String> env) {
    String r = genExpr(x.target(), env);
    return switch (x.method()) {
      case "get" -> "maps:get(" + genExpr(x.args().get(0), env) + ", " + r + ")";
      case "getOrDefault" -> "maps:get(" + genExpr(x.args().get(0), env) + ", " + r + ", "
          + genExpr(x.args().get(1), env) + ")";
      case "containsKey" -> "maps:is_key(" + genExpr(x.args().get(0), env) + ", " + r + ")";
      case "containsValue" -> "lists:member(" + genExpr(x.args().get(0), env)
          + ", maps:values(" + r + "))";
      case "size" -> "maps:size(" + r + ")";
      case "isEmpty" -> "(map_size(" + r + ") =:= 0)";
      case "keySet" -> "maps:keys(" + r + ")";
      case "values" -> "maps:values(" + r + ")";
      case "entrySet" -> "maps:to_list(" + r + ")"; // list of {K,V} -> for-each + e.getKey()
      case "forEach" -> {
        // forEach((k, v) -> {...}): side-effects (a lambda can't mutate captured locals;
        // for accumulation use `for (var e : m.entrySet())` instead)
        if (!(x.args().get(0) instanceof LambdaExpr lx) || lx.params().size() != 2) {
          throw new CompileError("Map.forEach takes a 2-arg lambda: m.forEach((k, v) -> {...})");
        }
        yield "maps:foreach(" + genLambda(lx, env, entryTypes(exprType(x.target())))
            + ", " + r + ")";
      }
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

  /** Java printf format -> Erlang io_lib:format control string. Supports the common
   *  specifiers; literal text is escaped for the Erlang string literal it lands in. */
  private static String translateFormat(String f) {
    var sb = new StringBuilder();
    for (int i = 0; i < f.length(); i++) {
      char c = f.charAt(i);
      if (c == '~') { sb.append("~~"); continue; }   // erl directive char -> literal tilde
      if (c != '%') { appendErlLit(sb, c); continue; }
      if (++i >= f.length()) throw new CompileError("String.format: dangling % in format");
      char d = f.charAt(i);
      switch (d) {
        case '%' -> sb.append('%');
        case 'n' -> sb.append("~n");
        case 's' -> sb.append("~ts");
        case 'd' -> sb.append("~w");
        case 'f' -> sb.append("~f");
        case 'x' -> sb.append("~.16b");
        case 'b' -> sb.append("~w");
        case '.' -> {                                 // %.Nf precision
          var num = new StringBuilder();
          int j = i + 1;
          while (j < f.length() && Character.isDigit(f.charAt(j))) num.append(f.charAt(j++));
          if (num.length() == 0 || j >= f.length() || f.charAt(j) != 'f') {
            throw new CompileError("String.format: only %.Nf precision is supported");
          }
          sb.append("~.").append(num).append('f');
          i = j;
        }
        default -> throw new CompileError("String.format: unsupported specifier %" + d
            + " (s/d/f/.Nf/x/b/n/%)");
      }
    }
    return sb.toString();
  }

  private static void appendErlLit(StringBuilder sb, char c) {
    switch (c) {
      case '"' -> sb.append("\\\"");
      case '\\' -> sb.append("\\\\");
      case '\n' -> sb.append("\\n");
      case '\t' -> sb.append("\\t");
      case '\r' -> sb.append("\\r");
      default -> sb.append(c);
    }
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
      case Ternary x -> {
        String t = exprType(x.thenExpr()); // both branches share a type; fall back to else
        yield t != null ? t : exprType(x.elseExpr());
      }
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
        MethodDecl md = ci == null ? null
            : ci.methods().get(x.callee() + "/" + x.args().size());
        yield md == null ? null : md.retType();
      }
      case MethodCall x -> {
        // .asInt()/.asText()/.asBool()/.asNum(): guarded crossing off a dynamic JSON value
        String asx = switch (x.method()) {
          case "asInt" -> "int";
          case "asText" -> "String";
          case "asBool" -> "boolean";
          case "asNum" -> "double";
          default -> null;
        };
        if (asx != null) yield asx;
        if (x.target() instanceof VarRef vr) {
          if (vr.name().equals("Tag")) yield x.method().equals("of") ? "Tag" : null;
          if (vr.name().equals("HttpClient")) yield "HttpClientBuilder";
          if (vr.name().equals("Json")) {
            String rec = classLitName(x.args().isEmpty() ? null : x.args().get(0));
            yield switch (x.method()) {
              case "encode" -> "String";
              case "decode" -> rec != null && records.containsKey(rec) ? rec : null;
              case "decodeAll", "decodeList" -> rec != null && records.containsKey(rec)
                  ? "List<" + rec + ">" : null;
              default -> null; // parse: dynamic
            };
          }
          if (vr.name().equals("HttpRequest")) yield "HttpRequestBuilder";
          if (vr.name().equals("Router")) yield "Router";
          if (vr.name().equals("Response")) yield "Response";
          if (vr.name().equals("Tuple")) yield x.method().equals("of") ? "Tuple" : null;
          if (vr.name().equals("Math")) {
            yield switch (x.method()) {
              case "sqrt", "pow", "floor", "ceil" -> "double";
              case "round" -> "int";
              default -> exprType(x.args().get(0)); // max/min/abs preserve the arg type
            };
          }
          if (vr.name().equals("Integer")) yield x.method().equals("parseInt") ? "int" : null;
          if (vr.name().equals("Files")) {
            yield switch (x.method()) {
              case "readString" -> "String";
              case "readBytes" -> "byte[]";
              case "readLines", "list" -> "List<String>";
              case "exists", "isDirectory" -> "boolean";
              case "size" -> "int";
              case "openReader" -> "Reader";
              case "openWriter", "openAppender" -> "Writer";
              default -> null; // void: writes, mkdirs, delete
            };
          }
          if (vr.name().equals("System")) {
            yield x.method().equals("getenv") ? "String" : null;
          }
          if (vr.name().equals("String")) {
            yield List.of("valueOf", "join", "format").contains(x.method()) ? "String" : null;
          }
          if (vr.name().equals("Arrays")) {
            yield x.method().equals("asList") ? "List" : null;
          }
          if (vr.name().equals("List") && !varTypes.containsKey("List")) {
            yield List.of("of", "copyOf").contains(x.method()) ? "List" : null;
          }
          if (vr.name().equals("Map") && !varTypes.containsKey("Map")) {
            yield x.method().equals("of") ? "Map" : null;
          }
          if (!varTypes.containsKey(vr.name())) {
            ClassInfo ci = classes.get(vr.name());
            if (ci != null) {
              MethodDecl md = ci.methods().get(x.method() + "/" + x.args().size());
              if (md != null) yield md.retType();
              yield null;
            }
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
            case "length", "indexOf", "compareTo" -> "int";
            case "isEmpty", "equals", "contains", "startsWith", "endsWith" -> "boolean";
            case "toUpperCase", "toLowerCase", "trim", "strip", "substring", "replace",
                "repeat", "charAt" -> "String";
            case "split" -> "String[]";
            case "toCharArray" -> "List";
            default -> null;
          };
        }
        List<String> targs = tt == null ? List.of() : typeArgs(tt);
        tt = tt == null ? null : baseType(tt);
        if ("ArrayList".equals(tt) || "List".equals(tt)) {
          yield switch (x.method()) {
            case "size", "indexOf" -> "int";
            case "contains", "isEmpty" -> "boolean";
            case "toArray" -> "Object[]";
            case "get", "getFirst", "getLast" -> targs.size() == 1 ? targs.get(0) : null;
            default -> null;
          };
        }
        if ("HashMap".equals(tt) || "Map".equals(tt)) {
          yield switch (x.method()) {
            case "size" -> "int";
            case "containsKey", "containsValue", "isEmpty" -> "boolean";
            case "entrySet" -> targs.size() == 2
                ? "List<Entry<" + targs.get(0) + "," + targs.get(1) + ">>" : "List<Entry>";
            default -> null; // forEach: void
          };
        }
        if ("Entry".equals(tt)) {
          yield switch (x.method()) {
            case "getKey" -> targs.isEmpty() ? null : targs.get(0);
            case "getValue" -> targs.size() < 2 ? null : targs.get(1);
            default -> null;
          };
        }
        if ("Reader".equals(tt)) {
          yield switch (x.method()) {
            case "hasNextLine" -> "boolean";
            case "nextLine" -> "String";
            default -> null; // close: void
          };
        }
        if ("Writer".equals(tt)) yield null; // write/writeLine/close: void
        if ("HttpClientBuilder".equals(tt)) {
          yield x.method().equals("build") ? "HttpClient" : "HttpClientBuilder";
        }
        if ("HttpRequestBuilder".equals(tt)) {
          yield x.method().equals("build") ? "HttpRequest" : "HttpRequestBuilder";
        }
        if ("HttpClient".equals(tt)) yield switch (x.method()) {
          case "send" -> "HttpResponse";
          case "openStream" -> "HttpStream";
          default -> null;
        };
        if ("HttpStream".equals(tt)) yield switch (x.method()) {
          case "hasNextChunk" -> "boolean";
          case "nextChunk" -> "byte[]";
          case "header" -> "String";
          default -> null; // close: void
        };
        if ("Router".equals(tt)) yield "Router";
        if ("Response".equals(tt)) yield "Response";
        if ("Request".equals(tt)) {
          yield switch (x.method()) {
            case "method", "path", "pathParam", "queryParam", "header", "body" -> "String";
            default -> null;
          };
        }
        if ("HttpResponse".equals(tt)) {
          yield switch (x.method()) {
            case "statusCode" -> "int";
            case "body", "header" -> "String";
            default -> null;
          };
        }
        if ("Db".equals(tt) || "Tx".equals(tt)) {
          yield switch (x.method()) {
            case "query" -> "List";
            case "exec" -> "int";
            default -> null; // transaction: the lambda's result, unknown (v1)
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
          // collection-mutator statements rebind the receiver — but a same-named
          // method on an Actor handle is a cast, no rebind
          if (st.expr() instanceof MethodCall mc && mc.target() instanceof VarRef vr
              && List.of("put", "remove", "add").contains(mc.method())
              && !allActors.containsKey(String.valueOf(varTypes.get(vr.name())))) {
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
      case Ternary x -> {
        exprRefs(x.cond(), out);
        exprRefs(x.thenExpr(), out);
        exprRefs(x.elseExpr(), out);
      }
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
