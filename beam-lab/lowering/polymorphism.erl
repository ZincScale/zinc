#!/usr/bin/env escript
-mode(compile).
%%% Lowering case: interfaces / polymorphism.
%%%   (a) sealed interface  -> tagged tuple + multi-clause dispatch  (the common, fast case)
%%%   (b) open interface    -> vtable (map of funs) -> duck-typed dynamic dispatch

main(_) ->
    Shapes = [{circle, 2.0}, {square, 3.0}, {rect, 2.0, 5.0}],
    WantTotal = round3(math:pi() * 4.0 + 9.0 + 10.0),
    Tests = [
        %% (a) sealed `interface Shape { area() }` -> multi-clause area/1
        {"dispatch circle",   fun() -> round3(area({circle, 2.0})) end, 12.566},
        {"dispatch square",   fun() -> area({square, 3.0}) end, 9.0},
        {"dispatch rect",     fun() -> area({rect, 2.0, 5.0}) end, 10.0},
        {"polymorphic total", fun() -> round3(total_area(Shapes)) end, WantTotal},

        %% (b) open interface via vtable (map of funs) -> dynamic/duck dispatch
        {"vtable dispatch",   fun() -> Obj = #{area => fun() -> 42.0 end}, (maps:get(area, Obj))() end, 42.0}
    ],
    run(Tests).

%% sealed interface -> one clause per implementer
area({circle, R})  -> math:pi() * R * R;
area({square, S})  -> S * S;
area({rect, W, H}) -> W * H.

total_area(Shapes) -> lists:foldl(fun(S, Acc) -> Acc + area(S) end, 0.0, Shapes).

round3(X) -> round(X * 1000) / 1000.

run(Tests) ->
    {P, F} = lists:foldl(
        fun({Name, Fun, Want}, {Ps, Fs}) ->
            case Fun() =:= Want of
                true  -> io:format("  PASS  ~s~n", [Name]), {Ps + 1, Fs};
                false -> io:format("  FAIL  ~s  got=~p want=~p~n", [Name, Fun(), Want]), {Ps, Fs + 1}
            end
        end, {0, 0}, Tests),
    io:format("~n  polymorphism: ~p passed, ~p failed~n", [P, F]),
    halt(case F of 0 -> 0; _ -> 1 end).
