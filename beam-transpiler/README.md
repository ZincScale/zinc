# zinc — Python on the BEAM

Write Python-shaped code with braces and types. Get an Erlang/OTP service that supervises
itself, heals on crash, and streams gigabytes in bounded memory — without writing a line of
functional code, a supervisor, or a Dockerfile.

```python
# A counter that survives its own crashes. No supervision code — the runtime builds it.
class Counter(Actor) {
    count = 0
    def incr()       { count = count + 1 }   # no return => async message (cast)
    def get() -> int { return count }             # typed     => sync request (call)
    def boom()       { z = 0
                           count = count / z }    # a real crash
}

class Main(Application) {
    c = Counter()                  # a supervised child of the app
    def main() {
        c.incr()
        c.incr()
        c.incr()
        print(c.get())        # 3
        c.boom()              # the process crashes...
        Sys.sleep(100)             # ...the supervisor restarts it...
        print(c.get())        # 0  — same handle, fresh state
        c.incr()
        print(c.get())        # 1  — and it keeps serving
    }
}
```

```
$ zc run
3
0
1
```

That's the whole thesis: a class `(Actor)` becomes a supervised `gen_server`; a class
`(Application)` becomes an OTP application with a supervision tree read straight from your
fields. Method return types pick the messaging — a typed `-> T` is a sync call, no return is
an async cast. You get BEAM-grade reliability (supervision, self-healing, distribution) with
Python muscle memory and a real type checker so production doesn't collapse the way
interpreted languages let it.

`.zn` is the braces-Python surface. It transpiles to Erlang and runs on the BEAM.

## Why

Small teams reach for microservices + Kubernetes to get reliability, scaling, and
isolation — and pay an enormous ops tax for it. The BEAM gives most of that *in the
runtime*. zinc is the on-ramp: familiar syntax, no functional-language friction, and the
deployment story is a self-contained release on a `$10` VM, not a cluster.

## Start here

- **[Getting started](docs/getting-started.md)** — install → hello → a real project →
  actors+supervision → a tour of the surface. The fastest path in.
- **[Examples](examples/py/)** — every feature as a tested, runnable `.zn` program.
- **[Design doc](../dialects/zinc-python/docs/beam-target-plan.md)** — how each Python
  construct maps to a BEAM concept (and the stdlib veneer + type-safety decisions).

## What's real today

Actors + Applications with supervision; the failure ladder (typed exceptions → relay →
crash → restart); records, enums, interfaces, generics, `match`/`case`; dict/list literals
with subscript and `len()`, f-strings, `str()`, `e.message`; **bounded-memory streaming** —
scoped file/HTTP readers & writers (`with`) and a `Channel`-based multi-process pipeline;
`Json` (derived record codecs, `Json.decode(User, …)`), `zinc.http` (client incl. the
`http.get` facade + server over cowboy), `zinc.sql` (Postgres); the `from erlang import …`
FFI escape hatch. The `zc` CLI scaffolds (`zc new --py`), vendors hex deps, builds, runs,
tests, and cuts a self-contained OTP release.

**Type safety, "safe but no safer":** typed params, checked returns, statically-typed
homogeneous collections; dynamic values (foreign JSON, mixed maps) are quarantined behind
guarded crossings that fail loud at the boundary. No `Optional`/`None` ceremony — a nil
deref crashes loud and the supervisor restarts it.

The compiler is a single-pass Java transpiler (`src/zinc/`, frontend `PyLexer`/`PyParser`/
`PyInfer`); `./e2e-py.sh` runs the whole `.zn` suite (transpile → `erlc` → run on a real
BEAM → assert output, including a Postgres-backed SQL test). The docs cite those examples,
so they can't drift. (`./e2e.sh` keeps the original legal-Java surface green — same backend,
Java syntax.)

Roadmap and design history: [ROADMAP.md](ROADMAP.md). Language name: TBD.
