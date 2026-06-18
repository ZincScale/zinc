# Install

`zc` is the one tool you need. It manages its own runtime — **no system Java, no system
Erlang, no Docker.** (Rustup model: install the dev kit, the dev kit installs the runtime.)

## The one-liner (recommended)

```sh
curl -fsSL https://github.com/ZincScale/zinc/releases/download/zc-v0.1.0/install.sh | sh
```

This lands the `zc` CLI in `~/.zc`, fetches a managed JRE and a pinned OTP, and wires your
`PATH`. Then:

```sh
zc new --py hello && cd hello
zc run            # -> Hello from hello!
```

> **Status:** the live `curl | sh` URL needs the first GitHub release to be cut (tag
> `zc-v*` + uploaded `zc.tar.gz`); that's a one-time, repo-owner step. Until then, use the
> offline or from-source paths below — both fully supported.

### Knobs (env vars)

| var | effect |
|-----|--------|
| `ZC_HOME` | install root (default `~/.zc`) |
| `ZC_VERSION` | release tag to pull (default `zc-v0.1.0`) |
| `ZC_DIST_TARBALL` | install from a local `zc.tar.gz` (offline / air-gapped) |
| `ZC_SKIP_JRE=1` | use host `java` instead of the managed JRE |
| `ZC_SKIP_OTP=1` | skip OTP now; run `zc toolchain install` later |
| `ZC_SKIP_PATH=1` | don't edit shell rc files; manage `PATH` yourself |

## Offline / air-gapped

Build the distributable tarball once (needs a JDK), then install from it — no network for
the `zc` bits:

```sh
cd beam-transpiler
./package.sh                          # builds dist/zc.tar.gz (compiled jar + shim + plugin)
ZC_DIST_TARBALL=dist/zc.tar.gz sh install.sh
```

The managed JRE and OTP are still downloaded unless you set `ZC_SKIP_JRE` / `ZC_SKIP_OTP`
(point at your own with the `zc toolchain` command).

## From the repo (contributors)

With the source checked out you can run `zc` directly — it's a multi-file Java program with
no build step (Java 22+ source launcher):

```sh
cd beam-transpiler
export PATH="$PWD/bin:$PATH"          # the bin/zc shim
zc toolchain install                 # one-time: pull a managed OTP into ~/.zc/otp
zc new --py hello && cd hello && zc run
```

`bin/zc` prefers, in order: `ZINC_JAVA` → a managed JDK in `~/.zc/java` → host `java`.

## Verify

```sh
zc doctor       # versions, managed toolchains, and how the [otp] pin resolves
```

## The commands

```
zc new --py <name>    create a braces-Python project (zinc.toml, src/main.zn)
zc run [dir]          build, then run main()
zc build [dir]        transpile + compile (no run)
zc test [dir]         run the test suite (EUnit underneath)
zc check [dir]        opt-in static net: xref + dialyzer over the FFI basement
zc add <name@ver>     add a hex dependency
zc release [dir]      self-contained OTP release tarball (ERTS + beam + boot script)
zc fmt <file|dir>     reindent
zc toolchain install [ver]   install a managed OTP
zc doctor             check the install
```

Next: the **[Guide](guide.md)** for the language itself, or the
**[Tutorials](tutorials.md)** to build something real.
