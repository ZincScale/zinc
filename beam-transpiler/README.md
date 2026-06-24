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
It supports:

- top-level scripts with `void main()` and service projects with `class Main : Application`
- actors, supervision, dynamic children, typed calls, async casts, and restart-stable handles
- records, enums, sealed unions, interfaces, lambdas, lists, dicts, and checked returns
- `try` / `catch`, `throw`, interpolated strings, `match`, `for`, `while`, `break`, and `continue`
- JSON record codecs, dynamic JSON access, config decode, files/path helpers, scoped streaming
- `Channel<T>` pipelines, HTTP client/server, Postgres access, logging, encoding, crypto, UUIDs
- `from erlang import ...` FFI for raw OTP or Hex modules
- `zc new`, `zc run`, `zc build`, `zc test`, `zc check`, `zc release`, and managed OTP

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

For a project-mode Application example with a supervised child, run:

```sh
cd beam-transpiler
zc run --once examples/zinc/project_app
```

`project_app` uses `class Main : Application`, a supervised `Counter : Actor` field, and
the normal project build path through `zinc.toml`.

## Documentation

- [Install](docs/install.md) - release install, offline install, and contributor setup.
- [Getting started](docs/getting-started.md) - first script, first project, and actor tour.
- [Guide](docs/guide.md) - language, actors, I/O, standard library, and CLI.
- [Tutorials](docs/tutorials.md) - build a small HTTP service and inspect project examples.
- [Examples](examples/README.md) - tested `.zn` examples by topic.
- [Roadmap](ROADMAP.md) - current engineering plan and release path.
- [Changelog](CHANGELOG.md) - release notes.

## Development Checks

```sh
cd beam-transpiler
./e2e-zinc.sh                  # canonical Zinc-on-BEAM .zn suite
./zc/test.sh                   # zc scaffold/run/test workflow
./rebar_zinc/test.sh           # rebar plugin workflow
./package-test.sh              # release artifact smoke
```

The same four checks run in the `beam-zinc` GitHub Actions workflow for changes under
`beam-transpiler/`.

The compiler is implemented in Java under `src/zinc/`. The CLI lives in `zc/Zc.java`, and the
prelude API stubs are in `zinc-prelude/zinc/`.
