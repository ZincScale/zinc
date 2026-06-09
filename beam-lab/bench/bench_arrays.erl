#!/usr/bin/env escript
-mode(compile).
%%% B2 — array tiers. Does indexed read/update stay flat as N grows (no silent O(n^2))?
%%% (ns/op figures include one closure call each; what matters is flat-vs-growing across N.)

main(_) ->
    Sizes = [1000, 100000, 1000000],
    Ops = 1000000,

    io:format("~n== B2 reads: ~p indexed reads, ns/op (flat as N grows = good) ==~n", [Ops]),
    io:format("  ~10s ~10s ~10s ~10s ~10s ~10s~n", ["N","map","array","tuple","ets","atomics"]),
    [read_row(N, Ops) || N <- Sizes],

    io:format("~n== B2 updates: ~p indexed updates, ns/op ==~n", [Ops]),
    io:format("  ~10s ~10s ~10s ~10s ~10s~n", ["N","map","array","ets","atomics"]),
    [upd_row(N, Ops) || N <- Sizes],

    io:format("~n== B2 TRAPS: why NOT to back arrays with list/tuple-update (ns/op GROWS with N) ==~n", []),
    io:format("  ~10s ~16s ~20s~n", ["N","list nth (read)","tuple setelem (upd)"]),
    [trap_row(N) || N <- Sizes],
    halt().

read_row(N, Ops) ->
    Map = maps:from_list([{I, I} || I <- lists:seq(0, N - 1)]),
    Arr = array:from_list(lists:seq(0, N - 1)),
    Tup = list_to_tuple(lists:seq(0, N - 1)),
    Ets = ets:new(b, [set]), [ets:insert(Ets, {I, I}) || I <- lists:seq(0, N - 1)],
    At  = atomics:new(N, []), [atomics:put(At, I + 1, I) || I <- lists:seq(0, N - 1)],
    Mns  = rns(Ops, fun(I) -> maps:get(I, Map) end, N),
    Ans  = rns(Ops, fun(I) -> array:get(I, Arr) end, N),
    Tns  = rns(Ops, fun(I) -> element(I + 1, Tup) end, N),
    Ens  = rns(Ops, fun(I) -> ets:lookup_element(Ets, I, 2) end, N),
    Atns = rns(Ops, fun(I) -> atomics:get(At, I + 1) end, N),
    ets:delete(Ets),
    io:format("  ~10w ~10.2f ~10.2f ~10.2f ~10.2f ~10.2f~n", [N, Mns, Ans, Tns, Ens, Atns]).

upd_row(N, Ops) ->
    Map = maps:from_list([{I, I} || I <- lists:seq(0, N - 1)]),
    Arr = array:from_list(lists:seq(0, N - 1)),
    Ets = ets:new(b, [set]), [ets:insert(Ets, {I, I}) || I <- lists:seq(0, N - 1)],
    At  = atomics:new(N, []),
    Mns  = uns(Ops, fun(I, S) -> maps:put(I, I + 1, S) end, Map, N),
    Ans  = uns(Ops, fun(I, S) -> array:set(I, I + 1, S) end, Arr, N),
    Ens  = uns(Ops, fun(I, S) -> ets:insert(Ets, {I, I + 1}), S end, Ets, N),
    Atns = uns(Ops, fun(I, S) -> atomics:put(At, I + 1, I + 1), S end, At, N),
    ets:delete(Ets),
    io:format("  ~10w ~10.2f ~10.2f ~10.2f ~10.2f~n", [N, Mns, Ans, Ens, Atns]).

trap_row(N) ->
    L = lists:seq(0, N - 1),
    Tup = list_to_tuple(lists:seq(0, N - 1)),
    M = 2000,
    Lns = rns(M, fun(I) -> lists:nth(I + 1, L) end, N),
    Tns = uns(M, fun(I, S) -> setelement(I + 1, S, I + 1) end, Tup, N),
    io:format("  ~10w ~16.1f ~20.1f~n", [N, Lns, Tns]).

%% timed read loop (cycles the index across the structure)
rns(Ops, Fun, N) ->
    {T, _} = timer:tc(fun() -> rloop(Ops, 0, Fun, N) end),
    T * 1000 / Ops.
rloop(0, Acc, _F, _N) -> Acc;
rloop(K, Acc, F, N) -> rloop(K - 1, Acc + F(K rem N), F, N).

%% timed update loop (threads the structure through)
uns(Ops, Fun, S0, N) ->
    {T, _} = timer:tc(fun() -> uloop(Ops, S0, Fun, N) end),
    T * 1000 / Ops.
uloop(0, S, _F, _N) -> S;
uloop(K, S, F, N) -> uloop(K - 1, F(K rem N, S), F, N).
