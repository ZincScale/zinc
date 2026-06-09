#!/usr/bin/env escript
-mode(compile).   %% compile with BeamAsm JIT, not interpret — realistic numbers

main(_) ->
    io:format("OTP ~s  (flavor ~p)~n",
              [erlang:system_info(otp_release),
               (catch erlang:system_info(emu_flavor))]),

    N = 50000000,                       %% 50M
    %% warmup (let JIT settle)
    plain_loop(2000000, 0),
    tc_loop(2000000, 0),

    %% ---- Bench 1: try/catch wrapper on the NON-throwing path ----
    {T1, R1} = timer:tc(fun() -> plain_loop(N, 0) end),
    {T2, R2} = timer:tc(fun() -> tc_loop(N, 0) end),
    io:format("~n== Bench 1: per-call try/catch, no throw fired (~p calls) ==~n", [N]),
    io:format("  plain      : ~9.1f ms   ~7.3f ns/call~n", [T1/1000, T1*1000/N]),
    io:format("  try/catch  : ~9.1f ms   ~7.3f ns/call~n", [T2/1000, T2*1000/N]),
    io:format("  OVERHEAD   : ~7.3f ns/call   (checksums ~p / ~p)~n",
              [(T2 - T1) * 1000 / N, R1, R2]),

    %% ---- Bench 2: throw-to-break vs recursive early return ----
    Big = lists:seq(1, 1000000),
    Target = 999999,                    %% near the end -> almost full scan
    Reps = 200,
    {T3, _} = timer:tc(fun() -> rep(Reps, fun() -> search_match(Big, Target) end) end),
    {T4, _} = timer:tc(fun() -> rep(Reps, fun() -> search_throw(Big, Target) end) end),
    io:format("~n== Bench 2: early-exit search, 1M elems x ~p reps ==~n", [Reps]),
    io:format("  recursive match : ~9.1f ms~n", [T3/1000]),
    io:format("  throw/catch     : ~9.1f ms~n", [T4/1000]),
    io:format("  ratio           : ~6.2fx~n", [T4 / T3]),

    %% ---- Bench 3: throw unwinding 50 stack frames vs normal 50-deep return ----
    DReps = 2000000,
    {T5, _} = timer:tc(fun() -> rep(DReps, fun() -> deep_return(50) end) end),
    {T6, _} = timer:tc(fun() -> rep(DReps, fun() -> deep_throw(50) end) end),
    io:format("~n== Bench 3: 50-frame return vs throw-unwind, ~p reps ==~n", [DReps]),
    io:format("  normal return : ~9.1f ms   ~7.1f ns/op~n", [T5/1000, T5*1000/DReps]),
    io:format("  throw unwind  : ~9.1f ms   ~7.1f ns/op~n", [T6/1000, T6*1000/DReps]),
    io:format("  delta         : ~7.1f ns/op~n", [(T6 - T5) * 1000 / DReps]),
    halt().

rep(0, _) -> ok;
rep(K, F) -> F(), rep(K - 1, F).

%% trivial work; never throws
work(N) -> N band 1.

plain_loop(0, Acc) -> Acc;
plain_loop(N, Acc) -> plain_loop(N - 1, Acc + work(N)).

tc_loop(0, Acc) -> Acc;
tc_loop(N, Acc) ->
    V = try work(N) catch throw:{ret, X} -> X end,   %% wrapped, but never throws
    tc_loop(N - 1, Acc + V).

search_match([T | _], T) -> {found, T};
search_match([_ | R], T) -> search_match(R, T);
search_match([], _)      -> notfound.

search_throw(L, T) ->
    try
        lists:foreach(fun(E) -> case E of T -> throw({found, E}); _ -> ok end end, L),
        notfound
    catch throw:{found, V} -> {found, V} end.

deep_return(0) -> 0;
deep_return(N) -> 1 + deep_return(N - 1).

deep_throw(N) -> try descend(N) catch throw:done -> N end.
descend(0) -> throw(done);
descend(N) -> 1 + descend(N - 1).
