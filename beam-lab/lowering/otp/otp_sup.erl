-module(otp_sup).
-behaviour(supervisor).
%%% Lowering target: the supervision the codegen makes DEFAULT (supervised-by-default).
%%% This is what "low-ceremony OTP" hides — no hand-written supervisor in the surface.
-export([start_link/0, init/1]).

start_link() -> supervisor:start_link({local, otp_sup}, ?MODULE, []).

init([]) ->
    %% high intensity so the benchmark can crash it many times without the
    %% supervisor giving up; a real default would be modest (e.g. 3 in 5s).
    Flags = #{strategy => one_for_one, intensity => 1000000, period => 3600},
    Child = #{id      => counter,
              start   => {otp_counter, start_link, []},
              restart => permanent,
              shutdown => 5000,
              type    => worker,
              modules => [otp_counter]},
    {ok, {Flags, [Child]}}.
