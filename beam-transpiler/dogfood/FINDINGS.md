# Dogfood findings — TCP line server (2026-06-10)

> **STATUS: all resolved in the follow-up round** (same day). GAP-1/4 fixed; GAP-2/3/6
> implemented (List.of/Map.of, toCharArray, `this` via '$self'); GAP-5 stubs emitted;
> GAP-7/8 documented/deferred. The natural-Java server is now e2e case `tcpserver`.
> New decision found while fixing: **try/catch is transactional** — a caught try reverts
> outer-var mutations to try-entry values (Erlang can't observe partial bindings; differs
> from Java, documented in examples/trycatch.zinc).

Program: `tcp_line_server.zinc` — Acceptor actor + one Conn actor per connection,
socket handoff via `controlling_process`, main doubles as test client. **Works**
(HELLO WORLD / BEAM ME UP) on try 2; try 1 deadlocked on GAP-4. Transpiler was NOT
modified; every gap routed around in the program and recorded here.

## Bugs (P0 — fix before anything else)

**GAP-1: break/continue inside try/catch escapes the loop.**
Evidence: `probe_break_in_try.zinc` → `{nocatch, {'$brk', 2}}` crash.
Cause: `hasBreakContinue` only descends IfStmt, so the loop helper never installs its
`'$brk'/'$cont'` catcher. Same hole for SwitchStmt arms.
Fix architecture: `hasBreakContinue` must mirror same-loop-scope reachability through ALL
non-loop compound statements (IfStmt, TryStmt, SwitchStmt; not nested loops, not SeqStmt's
inner while). The signals are throw-class and user catch is error-class, so once the
catcher exists, composition is already sound (verified: return-inside-try works today,
`probe_return_in_try.zinc` → 30).

**GAP-4: string escape sequences don't exist.**
`"\n"` lexes as backslash+n; codegen then emits `"\\n"`. Killed the server silently: no
line terminator ever sent, `{packet, line}` recv deadlocked both directions.
Fix architecture: decode Java escapes in the LEXER (\n \t \r \" \\ \0; \u later); encode
on EMIT (`escErl` maps newline -> `\n` etc.). Two clean boundaries, no raw-text leakage.

## Design gaps (P1-P2 — decide, then implement once)

**GAP-3: charlist-expecting OTP APIs.** Our String = binary; `gen_tcp:connect` rejects
binaries as hostnames (worked around with the IP tuple `Tuple.of(127,0,0,1)`).
Proposal: `s.toCharArray()` (real Java method name) -> `binary_to_list(S)`.

**GAP-6: actors can't reference themselves** (no `this`): can't self-cast, so
long-running work must block its handler (acceptable for Conn, limiting in general).
Proposal: start_link passes the registered name into init -> hidden `'$self'` state key;
`this` in actor methods -> that name. Self-casts then dispatch like any handle.

**GAP-2: option/proplist building is verbose** (ArrayList + N adds).
Proposal: `List.of(...)` (Java 9 idiom) -> list literal; consider `Map.of(...)`.

## Cosmetics / conventions (P3)

**GAP-5:** cast-only actor -> erlc warning `undefined callback handle_call/3`. Emit a
crashing stub clause (same let-it-crash semantics, no warning).
**GAP-7:** opaque FFI values are declared `Object` — fine; document the convention.
**GAP-8:** `Erlang.ok` failure says `{badmatch, ...}` with no source location — folds into
the Phase 4.3 source-maps work, not a one-off.

## Validated by this dogfood (no action)
Actors spawning actors; ports/sockets through casts; handle == registered name is
load-bearing AND user-visible (`erlang.whereis(handle)` works); blocking recv loop inside
a cast handler as a connection pattern; FFI imports incl. `import erlang.erlang;`;
return-inside-try; typed-handle params.
