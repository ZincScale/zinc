# Plan: wrap the `erlang.*` FFI in a stdlib

Status: PLANNING — not started.

## Goal

Today some zinc programs reach straight into the raw Erlang basement with
`import erlang.<module>;`. Those files are *exempt* from the legal-Java gate
(`legaljava.sh` skips any file matching `^import erlang\.`). The basement leaks
OTP module names (`gen_tcp`, `inet`, `io_lib`) into user source, which is not
legal Java and not clean.

Wrap that basement behind real stdlib types so user code imports clean
`zinc.*` surface instead of `erlang.*`. Result: the shipped examples stop being
EXEMPT-FFI and pass the legal-Java gate like everything else.

## Why this matters (the positioning)

The promise to a Java developer is: **you program normally, exactly as you
would on the JVM, and it runs on the BEAM VM.** Nothing about the source should
feel foreign:

- It is legal Java, top to bottom — `javac` accepts every line (the legal-Java
  gate enforces this).
- Even **actors feel normal**, because the actor model is already familiar to
  Java developers via Apache Pekko/Akka. The dev writes ordinary actor-shaped
  Java; they don't learn Erlang idioms.
- Then it runs on BEAM — where actors, supervision, and lightweight processes
  actually belong, and where the tiny self-contained runtime story holds.

A raw `import erlang.gen_tcp;` breaks that promise: it's not legal Java, it
forces the dev to think in OTP module names, and it punches a hole in the
"program normally" illusion. Wrapping the FFI in a clean stdlib closes the last
holes so the promise is true *end to end* — networking and the rest look like
ordinary Java, same as the actor surface already does.

Pekko is the *mental model* a Java dev brings, **not** a runtime target. We are
not running on the JVM/Pekko (see Non-goals); we only borrow the familiarity.

## Non-goals

- No JVM execution. Target stays BEAM-only; the transpiler remains the single
  source of semantics. (The Pekko/JVM-runtime idea was considered and dropped —
  it would fight the tiny-runtime thesis and force two semantics to agree.)
- No new language features. This is library + lowering work, not syntax.
- Not removing the `erlang.*` mechanism itself — it stays as the sanctioned
  escape hatch for un-wrapped BIFs (see "Escape hatch" below).

## Current surface (what to wrap)

Across 6 files (`ffi.zinc`, `guards.zinc`, `lambdas.zinc`, `atoms_tuples.zinc`,
`tcpserver.zinc`, `dogfood/tcp_line_server.zinc`):

| Raw FFI | Functions used | Disposition |
|---------|----------------|-------------|
| `erlang.lists`   | sum, sort, max, map, foldl, filter, nth | fold into idiomatic surface |
| `erlang.string`  | uppercase, split                        | fold into idiomatic surface |
| `erlang.gen_tcp` | listen, accept, connect, send, recv, close, controlling_process | out of scope — raw-FFI demo only |
| `erlang.inet`    | port                                    | out of scope — raw-FFI demo only |
| `erlang.io_lib`  | nl                                      | out of scope — below HTTP layer |
| `erlang` (bare)  | whereis, list_to_binary                 | out of scope — below HTTP layer |

Middleware stops at HTTP (Category B below), so the bottom four rows stay below
the surface as lowering internals + escape hatch. Only the top two rows
(`lists`, `string`) are user-facing cleanup.

### Scope: the abstraction ceiling is HTTP

The target audience is **middleware developers**. Nobody in middleware writes
raw TCP — the abstraction stops at **HTTP, including HTTP streaming**. That
surface already exists in the prelude and is already clean legal Java:
`HttpServer`, `HttpClient`, `HttpStream`, `Router`, `Handler`, `Request`,
`Response`, `HttpRequest`/`HttpResponse`.

So raw `gen_tcp`/`inet` is **out of scope for wrapping** — it is not the level
these developers work at. The TCP examples (`tcpserver.zinc`,
`tcp_line_server.zinc`) stay as deliberate raw-FFI escape-hatch demos (see
"Escape hatch"); we do not build a `java.net` lowering for them.

This collapses the real work to: (A) fold `lists.*`/`string.*` into idiomatic
Java, and (B) confirm the existing HTTP/streaming surface is complete and
exempt-free. Everything else is already done or out of scope.

Two distinct categories, handled differently:

### Category A — fold into `zinc.Lists` / `zinc.Strings` helpers (DECIDED)

These become real stdlib helper classes, not FFI. **They must be Java-
compatible**: every signature is something javac accepts and a Java dev reads as
ordinary code, built on `java.util.function` (`Function`, `BiFunction`,
`Predicate`) and `java.util.List`. The transpiler lowers each helper call to the
corresponding `lists:`/`string:` BIF — only the *spelling* in source changes;
runtime behavior is unchanged.

- **`zinc.Lists`** (static methods, mirrors `java.util.Collections`/Stream
  idioms):
  - `sum(List<Integer>)`, `max(List<T>)`, `sort(List<T>)`
  - `map(List<T>, Function<T,R>)`, `filter(List<T>, Predicate<T>)`
  - `foldl(List<T>, A, BiFunction<A,T,A>)`, `nth(List<T>, int)`
- **`zinc.Strings`** (static methods):
  - `upper(String)` (→ `string:uppercase`), `split(String, String)`

Where a plain JDK method is the exact equivalent (`String.toUpperCase()`,
`String.split`), examples may use that directly — but the helper home is
`zinc.Strings`/`zinc.Lists` so the list/functional ops have a consistent,
Java-compatible surface. Goal: `ffi.zinc`, `guards.zinc`, `lambdas.zinc`,
`atoms_tuples.zinc` drop `import erlang.*` and use the helpers.

### Category B — networking: HTTP already covers it, raw TCP is out of scope

The I/O ceiling for middleware is HTTP, and the prelude already provides it as
clean legal Java (`HttpServer`, `HttpClient`, `HttpStream`, `Router`, etc.). So
there is **no networking wrapping to do** — the work here is verification, not
construction:

- Confirm the HTTP + `HttpStream` surface has no `erlang.*` leaks and is fully
  exercised by an exempt-free example.
- `gen_tcp`/`inet`/`io_lib`/`whereis`/`list_to_binary` stay below the HTTP
  surface as lowering internals + the raw-FFI escape hatch. They do not get a
  user-facing wrapper, because the target dev never reaches that layer.

## Lowering

For each wrapper, `CodeGen.java` must recognize the new prelude
type/method and emit the corresponding Erlang. Today `import erlang.<m>` sets up
an alias→module passthrough (`CodeGen.java:71`). The new path is: prelude type
method calls → known Erlang BIF/op, the same way `HttpServer`/`Channel` already
lower. No passthrough; explicit mapping per method.

## Escape hatch — and the FFI test (DECIDED)

`import erlang.*` stays as the documented FFI basement for BIFs nobody has
wrapped yet. The raw-TCP demos (`tcpserver.zinc`, `dogfood/tcp_line_server.zinc`)
are **kept on purpose as the FFI regression test** — they are how we prove the
FFI mechanism actually works. Requirements:

- They stay EXEMPT-FFI in `legaljava.sh` by design (raw FFI is not legal Java —
  that's the point of the escape hatch).
- `e2e.sh` must **actually build and run them on BEAM** and assert behavior, so
  a broken `erlang.*` passthrough fails CI. Verifying the demos still compile is
  not enough — the FFI must be exercised end to end.

The non-TCP examples (`lists`/`string`) migrate off `erlang.*` per Category A.

## Migration

1. Build Category A surface + lowering; rewrite `ffi.zinc`, `guards.zinc`,
   `lambdas.zinc`, `atoms_tuples.zinc` to plain Java calls.
2. Verify the HTTP/`HttpStream` surface (Category B) is exempt-free and complete
   — this is confirmation, not construction.
3. Leave `tcpserver.zinc` / `dogfood/tcp_line_server.zinc` as the retained
   raw-FFI escape-hatch demos.
4. Update `legaljava.sh` expectations: EXEMPT-FFI drops to just the raw-TCP
   demos. Confirm the gate is green.
5. Confirm e2e (`e2e.sh`) still passes — Category A lowering must produce
   identical Erlang behavior to the old passthrough.

## Decisions

- **Category A home** — DECIDED: `zinc.Lists` / `zinc.Strings` helpers with
  Java-compatible signatures.
- **Raw-TCP demos** — DECIDED: keep both, as the FFI regression test; e2e must
  run them on BEAM.

Remaining verification task (not a fork): confirm nothing in the HTTP/streaming
path still leaks `erlang.*`. Expected clean already; check during step 2.

## Definition of done

- No HTTP-or-above example imports `erlang.*`; `lists.*`/`string.*` examples are
  rewritten to plain Java.
- Remaining EXEMPT-FFI is only the deliberate raw-TCP escape-hatch demo(s).
- `legaljava.sh`: PASS for all.
- `e2e.sh`: green (behavior unchanged).
- The `erlang.*` mechanism still works for ad-hoc FFI.
