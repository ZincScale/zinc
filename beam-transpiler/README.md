# zinc — Java on the BEAM

Write plain, legal Java. Get an Erlang/OTP service that supervises itself, heals on crash,
and streams gigabytes in bounded memory — without writing a line of functional code, a
supervisor, or a Dockerfile.

```java
// A counter that survives its own crashes. No supervision code — the runtime builds it.
class Counter implements Actor {
  int count = 0;
  public void incr() { count = count + 1; }   // void  => async message (cast)
  public int get()   { return count; }        // typed => sync request (call)
  public void boom() { int x = 1 / 0; }       // a real crash
}

public class Main implements Application {
  Counter c = new Counter();           // a supervised child of the app

  void main() {
    c.incr(); c.incr(); c.incr();
    System.out.println(c.get());       // 3
    c.boom();                          // the process crashes...
    Sys.sleep(100);                    // ...the supervisor restarts it...
    System.out.println(c.get());       // 0  — same handle, fresh state
    c.incr();
    System.out.println(c.get());       // 1  — and it keeps serving
  }
}
```

```
$ zc run
3
0
1
```

That's the whole thesis: a class `implements Actor` becomes a supervised `gen_server`; a
class `implements Application` becomes an OTP application with a supervision tree read
straight from your fields. You get BEAM-grade reliability (supervision, self-healing,
distribution) with Java muscle memory and **zero extension keywords** — every `.zinc` file
compiles under `javac`.

## Why

Small teams reach for microservices + Kubernetes to get reliability, scaling, and
isolation — and pay an enormous ops tax for it. The BEAM gives most of that *in the
runtime*. zinc is the on-ramp: familiar syntax, no functional-language friction, and the
deployment story is a self-contained release on a `$10` VM, not a cluster.

## Start here

- **[Install](docs/install.md)** — get the `zc` CLI; no system Java, Erlang, or Docker.
- **[Guide](docs/guide.md)** — the whole language, one coherent narrative.
- **[Tutorials](docs/tutorials.md)** — build a real HTTP+JSON service and a streaming pipeline.
- **[Coming from Java](docs/coming-from-java.md)** — what's deliberately different.
- **[Examples](examples/README.md)** — ~45 tested programs as a reading path, easiest first.

## What's real today

Actors + Applications with supervision; the failure ladder (typed exceptions → relay →
crash → restart); records, enums, interfaces, instance classes, generics (erased, gradually
checked); `List`/`ArrayList`/`Map` with honest costs; **bounded-memory streaming** — scoped
file/HTTP readers & writers (try-with-resources) and a `Channel`-based multi-process
pipeline; `Json`, `zinc.http` (client + server over cowboy), `zinc.sql` (Postgres); the
`import erlang.*` FFI basement with an opt-in `zc check` (xref + dialyzer) over it. The
`zc` CLI scaffolds, vendors hex deps, builds, runs, tests, and cuts a self-contained OTP
release.

The compiler is a single-pass Java transpiler (`src/zinc/`); `./e2e.sh` runs the whole
suite (legal-Java gate → transpile → `erlc` → run on a real BEAM → assert output). The
docs cite those examples, so they can't drift.

Roadmap and design history: [ROADMAP.md](ROADMAP.md). Language name: TBD.
