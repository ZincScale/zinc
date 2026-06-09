#!/usr/bin/env escript
-mode(compile).
%%% B1 — loop desugaring parity.
%%% Question: should the codegen emit DIRECT RECURSION or lists:foldl for loops?
%%% (Pairs with the throw/catch finding that lists:foreach closures cost 7x.)

main(_) ->
    N = 10000000,                          %% 10M
    %% warmup
    r_range(1000000, 0),
    L = lists:seq(1, N),
    r_list(L, 0),
    _ = lists:foldl(fun(X, A) -> A + X end, 0, lists:seq(1, 1000000)),

    %% B1a: numeric range loop  `for i in 0..N { sum += i }`
    {T1, _} = timer:tc(fun() -> r_range(N, 0) end),
    {T2, _} = timer:tc(fun() -> lists:foldl(fun(X, A) -> A + X end, 0, L) end),
    io:format("~n== B1a: numeric range sum, N=~p ==~n", [N]),
    io:format("  direct recursion (no list)  : ~9.1f ms~n", [T1 / 1000]),
    io:format("  lists:foldl over seq(1,N)   : ~9.1f ms   (~.2fx slower; alloc + closure)~n",
              [T2 / 1000, T2 / T1]),

    %% B1b: loop over an EXISTING list  `for x in xs { sum += x }`
    {T3, _} = timer:tc(fun() -> r_list(L, 0) end),
    io:format("~n== B1b: sum over an existing ~p-elem list ==~n", [N]),
    io:format("  recursion over list : ~9.1f ms~n", [T3 / 1000]),
    io:format("  lists:foldl         : ~9.1f ms~n", [T2 / 1000]),
    io:format("  closure tax (foldl/recursion) : ~.2fx~n", [T2 / T3]),
    halt().

%% direct recursion over a counter — no list materialized
r_range(0, A) -> A;
r_range(N, A) -> r_range(N - 1, A + N).

%% direct recursion over an existing list
r_list([], A)      -> A;
r_list([H | T], A) -> r_list(T, A + H).
