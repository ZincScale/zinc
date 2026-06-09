-module(otp_bench).
-export([main/0]).
%%% B3 — OTP ceremony cost: process spawn time, gen_server start, supervisor restart latency.
%%% Backs the pitch "self-healing in microseconds-to-milliseconds, not pod-restart seconds."

main() ->
    logger:set_primary_config(level, none),
    spawn_bench(1000000),
    genserver_bench(100000),
    restart_bench(500),
    ok.

%% raw BEAM process spawn throughput
spawn_bench(N) ->
    {T, _} = timer:tc(fun() -> spawn_n(N) end),
    io:format("  raw process spawn : ~p procs in ~p us  ->  ~.4f us/proc~n",
              [N, T, T / N]).
spawn_n(0) -> ok;
spawn_n(N) -> spawn(fun() -> ok end), spawn_n(N - 1).

%% gen_server start (a supervised "actor" instance)
genserver_bench(N) ->
    {T, _} = timer:tc(fun() -> gs_n(N) end),
    io:format("  gen_server start  : ~p servers in ~p us  ->  ~.4f us/server~n",
              [N, T, T / N]).
gs_n(0) -> ok;
gs_n(N) ->
    {ok, P} = gen_server:start(otp_counter, 0, []),
    gen_server:stop(P),
    gs_n(N - 1).

%% end-to-end self-heal latency: crash -> detected -> restarted -> serving again
restart_bench(Reps) ->
    {ok, _} = otp_sup:start_link(),
    Ts = [one_restart() || _ <- lists:seq(1, Reps)],
    io:format("  supervisor restart latency : avg ~.1f us  (min ~p / max ~p us) over ~p crashes~n",
              [lists:sum(Ts) / Reps, lists:min(Ts), lists:max(Ts), Reps]).

one_restart() ->
    Old = whereis(counter),
    T0 = erlang:monotonic_time(microsecond),
    otp_counter:boom(counter),
    wait_new(Old),
    erlang:monotonic_time(microsecond) - T0.

wait_new(Old) ->
    case whereis(counter) of
        New when is_pid(New), New =/= Old -> ok;
        _ -> wait_new(Old)
    end.
