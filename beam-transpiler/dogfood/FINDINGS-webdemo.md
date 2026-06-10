# Dogfood findings — cowboy webdemo (2026-06-10)

Program: `webdemo/` — cowboy HTTP service, zinc class as the cowboy handler, main doubles
as httpc test client. **PASSES** (`test.sh` -> "hello from zinc"). The undiagnosed
first-run timeout was NOT zinc.

## Root cause of the timeout (environment, not zinc)

Corp egress firewall drops **Erlang's TLS ClientHello by fingerprint** (JA3-style).
curl and Java TLS pass; raw TCP and port-80 HTTP pass; Erlang ssl times out on 443 in
every config. Ruled out: docker bridge, MTU (1472 DF pings pass), uid rules, hello size,
DNS. So rebar3 (Erlang TLS) hangs forever at "Verifying dependencies..." fetching cowboy.
Implication for zinc programs on such networks: outbound HTTPS *client* calls from BEAM
are dropped; serving TLS and production datacenter egress are unaffected. Roadmap note:
stdlib HTTP client wants corp-proxy support and optionally a native (libcurl) transport.

## Fixes landed (zc)

- **zc vendors hex deps** into `_checkouts/` via Java TLS: tarball from repo.hex.pm,
  transitive requirements from metadata.config, **MVS-lite** (minimum version of each
  requirement, first-wins). rebar3 then builds fully offline. Doubles as the hermetic/
  reproducible-builds story (no lock file needed when resolution is minimal-version).
- **zc run code path**: checkout deps compile to `_build/default/checkouts/*/ebin`,
  which run() missed (`cowboy_router:compile` undef). Now on `-pa`.
- symlink check in ensureGenerated follows dangling links -> NOFOLLOW.

## Bugs / gaps found

**GAP-9 (P1, the headline): FFI surface is naked Erlang.** Building the cowboy routes /
httpc options means `List.of(Tuple.of(Atom.port, 0))` etc. — no Java dev writes proplists
of tagged tuples. Raw FFI is the right *escape hatch*, wrong *daily surface*. Fix: zinc
stdlib with Java-shaped wrappers (e.g. `zinc.http` client modeled on java.net.http
.HttpClient; server/router API over cowboy) so atoms/tuples stay at the FFI boundary.

**GAP-10: quoted atoms are inexpressible.** cowboy's match-any host is the atom `'_'`;
`Atom.x` only does identifier atoms. Worked around via
`erlang.list_to_atom("_".toCharArray())`. Proposal: `Atom.of("_")` -> `'_'` literal.

**OTP wart (documented, no zinc action): httpc crashes on cert-less systems** even for
plain `http://` URLs — default http_options eagerly build ssl_verify_host_options ->
`pubkey_os_cacerts: no_cacerts_found` (erlang:slim has no CA bundle). Workaround in the
demo: `request/4` with explicit `{ssl, [{verify, verify_none}]}`.

## Validated by this dogfood (no action)

A zinc class IS a cowboy handler (cowboy calls `handler:init/2` by module name);
hex deps transparent to the program; `ranch:get_port` / port-0 ephemeral listen;
`application:ensure_all_started` via FFI; charlist interop via `toCharArray` (GAP-3
fix) carrying its weight for httpc/list_to_atom.
