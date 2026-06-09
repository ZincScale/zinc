#!/usr/bin/env escript
-mode(compile).
%%% Lowering case: array indexing -> two tiers.
%%%   default `arr[i]` / `arr[i]=x`  -> immutable persistent (array module), value semantics
%%%   explicit `Buffer`              -> process-local mutable (ETS / atomics), in-place O(1)
%%% Each case is its own fun/0 so variable scopes don't collide.

main(_) ->
    Tests = [
        %% ---- immutable persistent default: array module (value semantics) ----
        {"arr read",            fun() -> A = array:from_list([10,20,30]), array:get(1, A) end, 20},
        {"arr set",             fun() -> A = array:from_list([10,20,30]), A2 = array:set(1,99,A), array:get(1, A2) end, 99},
        {"arr orig unchanged",  fun() -> A = array:from_list([10,20,30]), _ = array:set(1,99,A), array:get(1, A) end, 20},

        %% ---- immutable persistent alt: map (value semantics; good for sparse/keyed) ----
        {"map read",            fun() -> M = #{0=>10,1=>20,2=>30}, maps:get(1, M) end, 20},
        {"map func-update",     fun() -> M = #{0=>10,1=>20,2=>30}, M2 = M#{1:=99}, maps:get(1, M2) end, 99},
        {"map orig unchanged",  fun() -> M = #{0=>10,1=>20,2=>30}, _ = M#{1:=99}, maps:get(1, M) end, 20},

        %% ---- explicit mutable Buffer: ETS (in-place) ----
        {"ets read",            fun() -> E = ets:new(b,[set]), ets:insert(E,{1,20}), ets:lookup_element(E,1,2) end, 20},
        {"ets in-place mutate", fun() -> E = ets:new(b,[set]), ets:insert(E,{1,20}), ets:insert(E,{1,99}), ets:lookup_element(E,1,2) end, 99},

        %% ---- explicit mutable Buffer: atomics (integers, in-place) ----
        {"atomics put/get",     fun() -> At = atomics:new(3,[]), atomics:put(At,1,20), atomics:get(At,1) end, 20},
        {"atomics add",         fun() -> At = atomics:new(3,[]), atomics:put(At,1,20), atomics:add(At,1,5), atomics:get(At,1) end, 25}
    ],
    run(Tests).

run(Tests) ->
    {P, F} = lists:foldl(
        fun({Name, Fun, Want}, {Ps, Fs}) ->
            case Fun() =:= Want of
                true  -> io:format("  PASS  ~s~n", [Name]), {Ps + 1, Fs};
                false -> io:format("  FAIL  ~s  got=~p want=~p~n", [Name, Fun(), Want]), {Ps, Fs + 1}
            end
        end, {0, 0}, Tests),
    io:format("~n  arrays: ~p passed, ~p failed~n", [P, F]),
    halt(case F of 0 -> 0; _ -> 1 end).
