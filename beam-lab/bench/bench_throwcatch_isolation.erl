#!/usr/bin/env escript
-mode(compile).

main(_) ->
    Big = lists:seq(1, 1000000),
    Target = 999999,
    Reps = 300,
    %% warmup
    rep(5, fun() -> search_match(Big, Target) end),
    rep(5, fun() -> search_rec_throw(Big, Target) end),
    rep(5, fun() -> search_foreach_throw(Big, Target) end),

    {T1,_} = timer:tc(fun() -> rep(Reps, fun() -> search_match(Big, Target) end) end),
    {T2,_} = timer:tc(fun() -> rep(Reps, fun() -> search_rec_throw(Big, Target) end) end),
    {T3,_} = timer:tc(fun() -> rep(Reps, fun() -> search_foreach_throw(Big, Target) end) end),

    io:format("~nIsolating throw cost: 1M-elem search x ~p reps~n", [Reps]),
    io:format("  (A) recursive, normal return      : ~9.1f ms   (baseline)~n", [T1/1000]),
    io:format("  (B) recursive, EXIT VIA THROW     : ~9.1f ms   ~5.2fx vs A~n", [T2/1000, T2/T1]),
    io:format("  (C) lists:foreach + throw         : ~9.1f ms   ~5.2fx vs A~n", [T3/1000, T3/T1]),
    io:format("~n  => (B vs A) isolates throw; (C vs B) isolates the closure-per-element cost~n"),
    halt().

rep(0, _) -> ok;
rep(K, F) -> F(), rep(K - 1, F).

%% A: pure recursion, returns a tuple normally
search_match([T | _], T) -> {found, T};
search_match([_ | R], T) -> search_match(R, T);
search_match([], _)      -> notfound.

%% B: SAME recursion shape, but exits via throw instead of normal return
search_rec_throw(L, T) ->
    try go(L, T) catch throw:{found, V} -> {found, V} end.
go([T | _], T) -> throw({found, T});
go([_ | R], T) -> go(R, T);
go([], _)      -> notfound.

%% C: higher-order foreach + throw (closure invoked per element)
search_foreach_throw(L, T) ->
    try
        lists:foreach(fun(E) -> case E of T -> throw({found, E}); _ -> ok end end, L),
        notfound
    catch throw:{found, V} -> {found, V} end.
