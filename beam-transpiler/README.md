# zinc on BEAM

Write Zinc `.zn` code and compile it to Erlang/OTP with supervision, process isolation,
backpressure, and release tooling built in.

```zinc
class Counter : Actor {
    int count = 0

    void incr() { count = count + 1 }      // void -> async cast
    int get() { return count }             // typed return -> sync call
    void crash() { int x = 1 / 0 }         // a real process crash
}

class Main : Application {
    Counter counter = Counter()            // supervised child

    void main() {
        counter.incr()
        print(counter.get())               // 1
        counter.crash()
        Sys.sleep(100)
        print(counter.get())               // 0, restarted with fresh state
    }
}
```

```sh
zc run --once .
```

The core idea is small: `class X : Actor` lowers to a supervised BEAM process, and
`class Main : Application` lowers to an OTP application. Fields define the supervision tree.
Method signatures define the protocol.

## Current Surface

The primary surface is `.zn`: canonical Zinc syntax with BEAM-native actors and applications.
The compiler still accepts the older Python-shaped `.zn` syntax as a compatibility frontend,
but new docs, examples, and applications should use the canonical type-first form. It supports:

- top-level scripts with `void main()` and service projects with `class Main : Application`
- actors, supervision, dynamic children, typed calls, async casts, and restart-stable handles
- records, enums, sealed unions, interfaces, lambdas, lists, dicts, and checked returns
- `try` / `except`, `raise`, interpolated strings, `match`, `for`, `while`, `break`, and `continue`
- JSON record codecs, dynamic JSON access, config decode, files/path helpers, scoped streaming
- `Channel<T>` pipelines, HTTP client/server, Postgres access, logging, encoding, crypto, UUIDs
- `from erlang import ...` FFI for raw OTP or Hex modules
- `zc new`, `zc run`, `zc build`, `zc test`, `zc check`, `zc release`, and managed OTP

The legacy legal-Java `.zinc` frontend remains tested by `./e2e.sh`. Use `zc new --java`
only when you specifically need that compatibility surface.

## Quick Start From This Checkout

```sh
cd beam-transpiler
export PATH="$PWD/bin:$PATH"
zc run examples/zinc/hello.zn
./e2e-zinc.sh
```

Create a project:

```sh
zc new flowtoy
cd flowtoy
zc run
```

For a realistic acceptance app, run the dogfood flow engine:

```sh
cd beam-transpiler
./dogfood/flowdemo/test.sh
```

`flowdemo` loads typed JSON config, recursively discovers input files, filters by extension,
streams records, routes success/failure output, exposes HTTP health/status routes, and proves
worker restart behavior through a black-box test.

## Documentation

- [Install](docs/install.md) - release install, offline install, and contributor setup.
- [Getting started](docs/getting-started.md) - first script, first project, and flowdemo tour.
- [Guide](docs/guide.md) - language, actors, I/O, standard library, and CLI.
- [Tutorials](docs/tutorials.md) - build a small HTTP service and inspect the flowdemo app.
- [Examples](examples/README.md) - tested `.zn` examples by topic.
- [Solidification plan](docs/solidification-plan.md) - current dogfood-driven engineering plan.

## Development Checks

```sh
cd beam-transpiler
./e2e-zinc.sh                  # canonical Zinc-on-BEAM .zn suite
./e2e-py.sh                    # legacy Python-shaped .zn compatibility suite
./dogfood/flowdemo/test.sh     # canonical app acceptance target
./e2e.sh                       # legacy .zinc compatibility suite
```

The compiler is implemented in Java under `src/zinc/`. The CLI lives in `zc/Zc.java`, and the
prelude API stubs are in `zinc-prelude/zinc/`.
