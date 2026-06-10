%% Provider `zinc clean`: remove generated src/zinc_gen/ directories.
-module(rebar_zinc_prv_clean).
-behaviour(provider).
-export([init/1, do/1, format_error/1]).

init(State) ->
    Provider = providers:create([
        {name, clean},
        {module, ?MODULE},
        {namespace, zinc},
        {bare, true},
        {deps, []},
        {example, "rebar3 zinc clean"},
        {opts, []},
        {short_desc, "Remove zinc-generated Erlang sources"},
        {desc, "Deletes src/zinc_gen/ in each project app."}
    ]),
    {ok, rebar_state:add_provider(State, Provider)}.

do(State) ->
    Apps = case rebar_state:current_app(State) of
               undefined -> rebar_state:project_apps(State);
               App -> [App]
           end,
    lists:foreach(
      fun(A) ->
              Gen = filename:join([rebar_app_info:dir(A), "src", "zinc_gen"]),
              rebar_file_utils:rm_rf(Gen)
      end, Apps),
    {ok, State}.

format_error(Reason) ->
    io_lib:format("~p", [Reason]).
