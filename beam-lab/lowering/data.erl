#!/usr/bin/env escript
-mode(compile).
%%% Lowering case: structs+methods, strings, null.
%%%   struct        -> record OR map; method `p.f()` -> `f(P)`; field set -> functional update
%%%   string        -> binary (never charlist)
%%%   nullable T?   -> Option encoding {some,V} | none (no null on BEAM)
-record(point, {x = 0.0, y = 0.0}).

main(_) ->
    Tests = [
        %% struct + method via record: p.dist() -> dist(P)
        {"record method dist",     fun() -> P = #point{x=3.0,y=4.0}, dist(P) end, 5.0},
        {"record field update",    fun() -> P = #point{x=3.0,y=4.0}, P2 = P#point{x=6.0}, P2#point.x end, 6.0},
        {"record orig unchanged",  fun() -> P = #point{x=3.0,y=4.0}, _ = P#point{x=6.0}, P#point.x end, 3.0},

        %% struct as map (the flexible default)
        {"map struct method",      fun() -> P = #{x=>3.0,y=>4.0}, mdist(P) end, 5.0},

        %% strings -> binaries
        {"binary length",          fun() -> byte_size(<<"hello">>) end, 5},
        {"binary concat",          fun() -> A = <<"foo">>, <<A/binary, "bar">> end, <<"foobar">>},
        {"binary not charlist",    fun() -> is_binary(<<"x">>) end, true},

        %% null -> Option ({some,V} | none)
        {"option present",         fun() -> opt_lookup(b, [{a,1},{b,2}]) end, {some, 2}},
        {"option absent",          fun() -> opt_lookup(z, [{a,1},{b,2}]) end, none},
        {"option or-default",      fun() -> opt_or(opt_lookup(z, []), 0) end, 0}
    ],
    run(Tests).

dist(#point{x = X, y = Y})  -> math:sqrt(X * X + Y * Y).
mdist(#{x := X, y := Y})    -> math:sqrt(X * X + Y * Y).

opt_lookup(K, KVs) ->
    case lists:keyfind(K, 1, KVs) of
        {K, V} -> {some, V};
        false  -> none
    end.
opt_or({some, V}, _) -> V;
opt_or(none, D)      -> D.

run(Tests) ->
    {P, F} = lists:foldl(
        fun({Name, Fun, Want}, {Ps, Fs}) ->
            case Fun() =:= Want of
                true  -> io:format("  PASS  ~s~n", [Name]), {Ps + 1, Fs};
                false -> io:format("  FAIL  ~s  got=~p want=~p~n", [Name, Fun(), Want]), {Ps, Fs + 1}
            end
        end, {0, 0}, Tests),
    io:format("~n  data: ~p passed, ~p failed~n", [P, F]),
    halt(case F of 0 -> 0; _ -> 1 end).
