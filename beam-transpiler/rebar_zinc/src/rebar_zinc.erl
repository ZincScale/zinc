-module(rebar_zinc).
-export([init/1]).

init(State) ->
    {ok, State1} = rebar_zinc_prv_compile:init(State),
    rebar_zinc_prv_clean:init(State1).
