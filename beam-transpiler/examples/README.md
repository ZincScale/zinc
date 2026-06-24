# Examples

The canonical Zinc-on-BEAM examples are `.zn` programs in [`zinc/`](zinc/) and are run by
[`../e2e-zinc.sh`](../e2e-zinc.sh). Each file is transpiled, compiled, executed on a real
BEAM, and checked against expected output.

When using `compilers/zinc-go/examples` as syntax guidance, follow
[`../docs/zinc-go-example-triage.md`](../docs/zinc-go-example-triage.md): port canonical
syntax that maps cleanly to BEAM, and do not import Go runtime semantics by accident.

Run the whole suite:

```sh
cd beam-transpiler
./e2e-zinc.sh
```

Run one example directly:

```sh
zc run examples/zinc/hello.zn
zc run examples/zinc/resources.zn
```

## Recommended Reading Path

| Topic | Examples |
|-------|----------|
| Canonical first programs | [`hello.zn`](zinc/hello.zn), [`functions.zn`](zinc/functions.zn), [`counter.zn`](zinc/counter.zn) |
| Control flow | [`fizzbuzz.zn`](zinc/fizzbuzz.zn), [`bools.zn`](zinc/bools.zn), [`floats.zn`](zinc/floats.zn), [`match.zn`](zinc/match.zn) |
| Types and values | [`records.zn`](zinc/records.zn), [`sealed.zn`](zinc/sealed.zn), [`record_model.zn`](zinc/record_model.zn) |
| Collections | [`collections.zn`](zinc/collections.zn), [`multifile/`](zinc/multifile) |
| Errors | [`trycatch.zn`](zinc/trycatch.zn), [`exceptions.zn`](zinc/exceptions.zn) |
| Actors and supervision | [`counter.zn`](zinc/counter.zn), [`supervised.zn`](zinc/supervised.zn), [`project_app/`](zinc/project_app) |
| Interfaces | [`protocols.zn`](zinc/protocols.zn), [`lambdas_sam.zn`](zinc/lambdas_sam.zn) |
| Files and resources | [`fileio.zn`](zinc/fileio.zn), [`filestream.zn`](zinc/filestream.zn), [`resources.zn`](zinc/resources.zn) |
| Channels and pipelines | [`channel.zn`](zinc/channel.zn), [`pipeline.zn`](zinc/pipeline.zn) |
| JSON and HTTP | [`json.zn`](zinc/json.zn), [`http_client.zn`](zinc/http_client.zn), [`http_facade.zn`](zinc/http_facade.zn) |
| Encoding and auth utilities | [`encoding.zn`](zinc/encoding.zn), [`webauth.zn`](zinc/webauth.zn) |

## Application Example

The canonical app-level example lives in [`zinc/project_app`](zinc/project_app). It uses
`zinc.toml`, `class Main : Application`, and a supervised Actor child.

```sh
cd beam-transpiler
zc run --once examples/zinc/project_app
```

## Negative Examples

[`zinc_neg/`](zinc_neg) contains `.zn` programs that must fail to compile. These pin down
type errors, invalid application shapes, reserved names, reassignment rules, tuple arity,
and lambda typing. Expected errors are asserted in `../e2e-zinc.sh`.
