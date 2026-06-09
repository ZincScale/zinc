#!/usr/bin/env escript
-mode(compile).
%%% Lowering case: imperative C-family control flow -> Erlang.
%%% Each function is what the codegen would EMIT for the surface shown in its comment.
%%% main/1 asserts behavioral correctness (PASS/FAIL).

main(_) ->
    Tests = [
        %% if/else + switch  ->  case
        {"if   sign(-5)",            sign(-5),                       negative},
        {"if   sign(0)",             sign(0),                        zero},
        {"sw   day_kind(sun)",       day_kind(sun),                  weekend},
        {"sw   day_kind(wed)",       day_kind(wed),                  weekday},

        %% mutable accumulator loop:  for x in xs { sum += x }
        {"loop sum_loop",            sum_loop([1,2,3,4,5]),          15},

        %% while + MULTIPLE mutated vars:  var i=0,p=1; while i<n { p*=2; i+=1 }
        {"while pow2(10)",           while_pow2(10),                 1024},

        %% break/continue (single level) -> direct recursion
        %% for x in xs { if even(x) continue; if x>50 break; acc+=x }
        {"break/continue collect",   collect([2,1,3,51,5], 50),      4},

        %% early return (multiple mid-fn returns) -> throw/catch
        {"early classify(-1)",       classify(-1),                   neg},
        {"early classify(0)",        classify(0),                    zero},
        {"early classify(7)",        classify(7),                    pos},

        %% early return FROM INSIDE NESTED LOOPS (labeled break) -> throw
        %% the case Gleam can't express; throw unwinds both loops at once
        {"labeled find_pair",        find_pair([1,2,3],[10,20,30],23), {found,{3,20}}}
    ],
    run(Tests).

%% ---- if/else + switch  ->  case ----------------------------------------
%% surface: if x<0 {neg} else if x==0 {zero} else {pos}
sign(X) ->
    case X of
        _ when X < 0 -> negative;
        0            -> zero;
        _            -> positive
    end.

%% surface: switch d { case sat,sun: weekend; default: weekday }
day_kind(D) ->
    case D of
        sat -> weekend;
        sun -> weekend;
        _   -> weekday
    end.

%% ---- mutable accumulator loop  ->  tail recursion threading the accumulator
sum_loop(Xs) -> sum_loop(Xs, 0).
sum_loop([], Acc)      -> Acc;
sum_loop([H | T], Acc) -> sum_loop(T, Acc + H).

%% ---- while + multiple mutated vars  ->  helper threading {I,P} as args
while_pow2(N) -> while_pow2(0, 1, N).
while_pow2(I, P, N) when I < N -> while_pow2(I + 1, P * 2, N);
while_pow2(_, P, _)            -> P.

%% ---- break/continue (single level)  ->  direct recursion -----------------
%% continue = recurse skipping the body tail; break = stop recursing (return acc).
collect(Xs, Threshold) -> collect(Xs, Threshold, 0).
collect([], _, Acc) -> Acc;
collect([H | T], Th, Acc) ->
    case H rem 2 =:= 0 of
        true  -> collect(T, Th, Acc);              %% continue (skip evens)
        false ->
            case H > Th of
                true  -> Acc;                      %% break
                false -> collect(T, Th, Acc + H)
            end
    end.

%% ---- early return (multi mid-fn)  ->  throw/catch ------------------------
%% surface: if x<0 {return neg}; if x==0 {return zero}; expensive(); return pos
classify(X) ->
    try
        (X < 0)   andalso throw({ret, neg}),
        (X =:= 0) andalso throw({ret, zero}),
        pos
    catch throw:{ret, V} -> V end.

%% ---- early return from inside NESTED loops (labeled break)  ->  throw -----
%% surface: for i in xs { for j in ys { if i+j==t { return (i,j) } } } ; return none
find_pair(Xs, Ys, Target) ->
    try
        loop_i(Xs, Ys, Target),
        none
    catch throw:{found, P} -> {found, P} end.

loop_i([], _, _) -> ok;
loop_i([I | Is], Ys, T) -> loop_j(I, Ys, T), loop_i(Is, Ys, T).

loop_j(_, [], _) -> ok;
loop_j(I, [J | Js], T) ->
    case I + J =:= T of
        true  -> throw({found, {I, J}});   %% escapes BOTH loops at once
        false -> loop_j(I, Js, T)
    end.

%% ---- behavioral assert runner --------------------------------------------
run(Tests) ->
    {P, F} = lists:foldl(
        fun({Name, Got, Want}, {Ps, Fs}) ->
            case Got =:= Want of
                true  -> io:format("  PASS  ~s~n", [Name]), {Ps + 1, Fs};
                false -> io:format("  FAIL  ~s  got=~p want=~p~n", [Name, Got, Want]),
                         {Ps, Fs + 1}
            end
        end, {0, 0}, Tests),
    io:format("~n  control_flow: ~p passed, ~p failed~n", [P, F]),
    halt(case F of 0 -> 0; _ -> 1 end).
