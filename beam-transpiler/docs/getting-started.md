# Getting Started

This guide takes you from a first `.zn` script to a supervised BEAM application. The
canonical surface is Zinc syntax with type-first declarations, braces for blocks, and
BEAM-native actors.

## Install Or Use The Checkout

Release install, once a `zc-v*` release is available:

```sh
curl -fsSL https://github.com/ZincScale/zinc/releases/download/zc-v0.1.0/install.sh | sh
zc doctor
```

From this repository, no packaging step is needed:

```sh
cd beam-transpiler
export PATH="$PWD/bin:$PATH"
zc doctor
```

If no managed OTP is installed yet, run:

```sh
zc toolchain install 29
```

## Run A Script

Create `hello.zn`:

```zinc
void main() {
    var name = "BEAM"
    print("Hello, " + name + "!")
}
```

Run it directly:

```sh
zc run hello.zn
```

Expected output:

```text
Hello, BEAM!
```

A top-level `void main()` is enough for script mode. No project file is required.

## Create A Project

```sh
zc new myapp
cd myapp
zc run
```

The scaffold contains:

```text
myapp/
  zinc.toml
  src/main.zn
```

Common commands:

```sh
zc run                 # build and run
zc run --once .        # run once even if the project is an Application
zc build               # transpile and compile
zc test                # run test/**/*.zn
zc check --xref        # xref only over generated BEAM/FFI code
zc check               # xref + dialyzer
zc fmt src             # brace-depth formatter
zc release             # self-contained OTP release tarball
zc add cowboy@2.12.0   # add a Hex dependency
```

## Actors And Supervision

An `Actor` is a BEAM process. Its fields are private process state. A no-return method is an
async cast; a typed return method is a sync call. An `Application` owns supervised children
through its actor fields.

```zinc
class Counter : Actor {
    int count = 0

    void incr() { count = count + 1 }
    int get() { return count }
    void crash() { int x = 1 / 0 }
}

class Main : Application {
    Counter counter = Counter()

    void main() {
        counter.incr()
        counter.incr()
        print(counter.get())
        counter.crash()
        Sys.sleep(100)
        print(counter.get())
    }
}
```

Run with `zc run --once .`. The second print is `0`: the actor crashed and restarted with a
fresh state, while the handle stayed valid.

## Config, Files, And Streaming

Typical apps need typed config and file discovery. Zinc keeps that path small and explicit:

```zinc
record AppConfig(inputDir: String, output: String)

class Main : Application {
    void main() {
        AppConfig cfg = Config.decode(AppConfig, "config.json")
        Files.writeString(cfg.output, "")

        for path in Files.walk(cfg.inputDir) {
            if Files.extension(path).equals("jsonl") {
                with Files.openReader(path) as r {
                    with Files.openAppender(cfg.output) as w {
                        while r.hasNextLine() {
                            w.writeLine(Files.baseName(path) + ":" + r.nextLine())
                        }
                    }
                }
            }
        }
    }
}
```

Useful file APIs:

- `Files.join(a, b)`, `Files.baseName(path)`, `Files.dirName(path)`, `Files.extension(path)`
- `Files.list(dir)` for sorted names directly under a directory
- `Files.walk(dir)` for sorted recursive full paths
- `Files.modifiedTime(path)` and `Files.size(path)` for ingest checks
- `Files.openReader/openWriter/openAppender` for scoped, bounded-memory streaming

## Run The Canonical Dogfood App

`dogfood/flowdemo` is the current acceptance app. It proves the pieces compose:

```sh
cd beam-transpiler
./dogfood/flowdemo/test.sh
```

It exercises typed config, recursive file discovery, extension filtering, streaming readers
and writers, success/failure routing, HTTP `/health` and `/status`, and worker restart
evidence.

## Next

- Read the full [guide](guide.md).
- Browse the tested [examples](../examples/README.md).
- Build the HTTP service in [tutorials](tutorials.md).
