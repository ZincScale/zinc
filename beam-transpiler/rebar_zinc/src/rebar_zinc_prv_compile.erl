%% Provider `zinc compile`: src/**/*.zinc -> src/zinc_gen/*.erl via the zinc transpiler.
%% Wire it before erlc with:
%%   {provider_hooks, [{pre, [{compile, {zinc, compile}}]}]}.
-module(rebar_zinc_prv_compile).
-behaviour(provider).
-export([init/1, do/1, format_error/1]).

init(State) ->
    Provider = providers:create([
        {name, compile},
        {module, ?MODULE},
        {namespace, zinc},
        {bare, true},
        {deps, []},
        {example, "rebar3 zinc compile"},
        {opts, []},
        {short_desc, "Transpile zinc .zinc (Java-syntax) sources to Erlang"},
        {desc, "Finds src/**/*.zinc in each project app and emits .erl into src/zinc_gen/. "
               "Transpiler location: {zinc, [{compiler_home, Dir}]} in rebar.config, or "
               "the ZINC_HOME env var; java binary via {java, Path} or ZINC_JAVA."}
    ]),
    {ok, rebar_state:add_provider(State, Provider)}.

do(State) ->
    Cfg = rebar_state:get(State, zinc, []),
    Apps = case rebar_state:current_app(State) of
               undefined -> rebar_state:project_apps(State);
               App -> [App]
           end,
    lists:foreach(fun(A) -> compile_app(A, Cfg) end, Apps),
    {ok, State}.

format_error(Reason) ->
    io_lib:format("~p", [Reason]).

compile_app(App, Cfg) ->
    Dir = rebar_app_info:dir(App),
    SrcDir = filename:join(Dir, "src"),
    TestDir = filename:join(Dir, "test"),
    Srcs = filelib:wildcard("**/*.zinc", SrcDir),
    Tests = filelib:wildcard("**/*.zinc", TestDir),
    case Srcs of
        [] ->
            ok;
        _ ->
            OutDir = filename:join(SrcDir, "zinc_gen"),
            TestOut = filename:join(TestDir, "zinc_gen"),
            clean_generated(OutDir),
            clean_generated(TestOut),
            %% test/**/*.zinc rides along: one transpiler run sees the whole project
            %% (tests reference src types); test output never lands under src/
            TestArgs = case Tests of
                           [] -> "";
                           _ -> io_lib:format(" ~ts ~ts", [TestDir, TestOut])
                       end,
            Cmd = lists:flatten(io_lib:format("~ts ~ts ~ts ~ts~ts",
                                              [java_cmd(Cfg), compiler_main(Cfg),
                                               SrcDir, OutDir, TestArgs])),
            rebar_api:debug("zinc: ~ts", [Cmd]),
            case rebar_utils:sh(Cmd, [{use_stdout, false}, return_on_error]) of
                {ok, _} ->
                    rebar_api:info("zinc: transpiled ~b file(s) -> ~ts",
                                   [length(Srcs) + length(Tests), OutDir]);
                {error, {_Code, Output}} ->
                    rebar_api:abort("zinc transpile failed:~n~ts", [Output])
            end
    end.

%% the transpiler rewrites the full module set; drop stale .erl first
clean_generated(OutDir) ->
    [file:delete(F) || F <- filelib:wildcard(filename:join(OutDir, "*.erl"))],
    ok.

java_cmd(Cfg) ->
    proplists:get_value(java, Cfg, env_or("ZINC_JAVA", "java")).

compiler_main(Cfg) ->
    case proplists:get_value(compiler_home, Cfg, env_or("ZINC_HOME", undefined)) of
        undefined ->
            rebar_api:abort(
              "zinc: transpiler not found. Set {zinc, [{compiler_home, Dir}]} in "
              "rebar.config or the ZINC_HOME env var.", []);
        Home ->
            filename:join([Home, "src", "zinc", "Main.java"])
    end.

env_or(Var, Default) ->
    case os:getenv(Var) of
        false -> Default;
        Val -> Val
    end.
