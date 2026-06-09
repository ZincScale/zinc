-module(otp_demo).
-export([main/0]).
%%% Self-heal demonstration: crash the actor, watch the supervisor auto-restart it.

main() ->
    logger:set_primary_config(level, none),   %% quiet the crash report; pid change proves it
    {ok, _Sup} = otp_sup:start_link(),

    P1 = whereis(counter),
    otp_counter:incr(counter),
    otp_counter:incr(counter),
    otp_counter:incr(counter),
    V1 = otp_counter:get(counter),
    io:format("  before crash : pid=~p  count=~p~n", [P1, V1]),

    otp_counter:boom(counter),                %% <-- real crash inside the actor
    settle(P1),                               %% wait for the supervisor to restart it

    P2 = whereis(counter),
    V2 = otp_counter:get(counter),
    io:format("  after  crash : pid=~p  count=~p  (auto-restarted; state reset to init)~n",
              [P2, V2]),

    Healed = is_pid(P2) andalso P2 =/= P1 andalso is_process_alive(P2),
    io:format("~n  SELF-HEAL ~s : new pid, serving again, zero manual intervention~n",
              [case Healed of true -> "PASS"; false -> "FAIL" end]),

    otp_counter:incr(counter),
    io:format("  post-heal incr -> count=~p~n", [otp_counter:get(counter)]),
    ok.

settle(Old) ->
    case whereis(counter) of
        New when is_pid(New), New =/= Old -> ok;
        _ -> timer:sleep(1), settle(Old)
    end.
