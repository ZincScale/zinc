-module(otp_counter).
-behaviour(gen_server).
%%% Lowering target: a low-ceremony "supervised actor" the codegen would EMIT.
%%% Surface intent (future syntax):
%%%     actor Counter(var n = 0) {
%%%         on incr()  { n += 1 }
%%%         on get()   -> n
%%%     }
%%% -> generates this gen_server boilerplate (the ceremony we want to hide).
-export([start_link/0, incr/1, get/1, boom/1]).
-export([init/1, handle_call/3, handle_cast/2]).

start_link() -> gen_server:start_link({local, counter}, ?MODULE, 0, []).

%% public API (generated)
incr(Name) -> gen_server:cast(Name, incr).
get(Name)  -> gen_server:call(Name, get).
boom(Name) -> gen_server:cast(Name, boom).   %% test hook: trigger a real crash

%% callbacks (generated)
init(N) -> {ok, N}.

handle_call(get, _From, N) -> {reply, N, N}.

handle_cast(incr, N) -> {noreply, N + 1};
handle_cast(boom, _N) -> erlang:error(intentional_crash).  %% let it crash
