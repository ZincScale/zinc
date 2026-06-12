# sqldemo findings (zinc.sql v1, 2026-06-12)

**Result: PASSED on try 1** against real Postgres (16-alpine, shared docker
network). One pre-flight transpiler gap found while drafting the demo, fixed
before the run: `List<User>.get(i)` returned unknown (List facade ignored type
args), so `us.get(0).name()` failed to dispatch — `get` now yields the element
type.

## Worked as designed
- Pool boots before main (root sup order), connections connect in init,
  checkin race is benign (manager starts first, casts queue).
- Transaction lambda: commit on return, rollback + relay on a thrown user
  exception — typed catch sees it, the delete was rolled back server-side.
- SQL errors come back catchable as SqlException; the connection survives
  (next query on the same pool fine).
- SIGTERM drain: conns close via the terminate/2 close idiom; "SIGTERM
  received - shutting down" on stderr, stdout stays clean.
- epgsql 4.7.1 vendored by zc through the corp TLS firewall, zero deps of its
  own.

## v1 waits (recorded, not bugs)
- Checkout timeout fixed at 5000 ms, query timeout infinity — not configurable
  (where would it live: Db ctor? per-query?). Decide when a dogfood needs it.
- Checkout-timeout race: a waiter that gives up exactly as the manager replies
  leaks the conn until the borrower process dies (deadline check at hand-out
  closes all but a microsecond window; poolboy-style cancel protocol if it
  ever shows up in practice).
- Record fields match column names verbatim: camelCase field vs snake_case
  column needs `select ... as userId` aliases for now.
- null columns arrive as the atom `null` — fine through dynamic access, but a
  typed bind ('$chk' string/integer) crashes; nullable story is the JSON one
  (deferred together).
- No LISTEN/NOTIFY, no streaming/cursors (rows fully materialize).
- epgsql internals relied on positionally: #column name = element 2,
  #error = {error, Sev, Code, Codename, Msg, Extra} — pin on epgsql upgrades.
