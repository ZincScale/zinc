# Examples

The canonical Zinc-on-BEAM examples are `.zn` programs in [`zinc/`](zinc/) and are run by
[`../e2e-zinc.sh`](../e2e-zinc.sh). The broader compatibility corpus still lives in
[`py/`](py/) and exercises the older Python-shaped frontend through
[`../e2e-py.sh`](../e2e-py.sh). Each file is transpiled, compiled, executed on a real BEAM,
and checked against expected output.

When using `compilers/zinc-go/examples` as syntax guidance, follow
[`../docs/zinc-go-example-triage.md`](../docs/zinc-go-example-triage.md): port canonical
syntax that maps cleanly to BEAM, and do not import Go runtime semantics by accident.

Run the whole suite:

```sh
cd beam-transpiler
./e2e-py.sh
./e2e-zinc.sh
```

Run one example directly:

```sh
zc run examples/py/hello.zn
zc run examples/zinc/hello.zn
zc run examples/py/resources.zn
```

## Recommended Reading Path

| Topic | Examples |
|-------|----------|
| Canonical first programs | [`hello.zn`](zinc/hello.zn), [`functions.zn`](zinc/functions.zn), [`counter.zn`](zinc/counter.zn) |
| Legacy compatibility first programs | [`hello.zn`](py/hello.zn), [`functions.zn`](py/functions.zn), [`countdown.zn`](py/countdown.zn), [`fizzbuzz.zn`](py/fizzbuzz.zn) |
| Control flow | [`fizzbuzz.zn`](zinc/fizzbuzz.zn), [`bools.zn`](zinc/bools.zn), [`floats.zn`](zinc/floats.zn), [`ternary.zn`](py/ternary.zn), [`breakcont.zn`](py/breakcont.zn), [`match.zn`](zinc/match.zn) |
| Types and values | [`records.zn`](zinc/records.zn), [`sealed.zn`](zinc/sealed.zn), [`record_model.zn`](zinc/record_model.zn) |
| Collections | [`collections.zn`](zinc/collections.zn), [`dict.zn`](py/dict.zn), [`multifile/`](zinc/multifile) |
| Errors | [`trycatch.zn`](zinc/trycatch.zn), [`exceptions.zn`](zinc/exceptions.zn) |
| Actors and supervision | [`counter.zn`](zinc/counter.zn), [`supervised.zn`](zinc/supervised.zn), [`counter_init.zn`](py/counter_init.zn), [`selfheal.zn`](py/selfheal.zn) |
| Interfaces | [`protocols.zn`](zinc/protocols.zn), [`lambdas_sam.zn`](zinc/lambdas_sam.zn) |
| Files and resources | [`fileio.zn`](zinc/fileio.zn), [`filestream.zn`](zinc/filestream.zn), [`resources.zn`](zinc/resources.zn) |
| Channels and pipelines | [`channel.zn`](zinc/channel.zn), [`pipeline.zn`](zinc/pipeline.zn) |
| JSON and HTTP | [`json.zn`](zinc/json.zn), [`http_client.zn`](zinc/http_client.zn), [`http_facade.zn`](zinc/http_facade.zn) |
| SQL | [`sql.zn`](py/sql.zn) |
| Encoding and auth utilities | [`encoding.zn`](zinc/encoding.zn), [`webauth.zn`](zinc/webauth.zn) |
| FFI | [`ffi.zn`](py/ffi.zn) |

## Application Example

The canonical app-level example is not under `examples/py`; it lives in
[`../dogfood/flowdemo`](../dogfood/flowdemo). It uses typed config, recursive file discovery,
streaming readers/writers, actor workers, result routing, HTTP status routes, and restart
checks.

```sh
cd beam-transpiler
./dogfood/flowdemo/test.sh
```

## Negative Examples

[`zinc_neg/`](zinc_neg) and [`py_neg/`](py_neg) contain `.zn` programs that must fail to
compile. These pin down type errors, invalid application shapes, non-exhaustive matches,
reassignment rules, and inference cycles. Expected errors are asserted in `../e2e-zinc.sh`
and `../e2e-py.sh`.

## Legacy Legal-Java Examples

[`programs/`](programs) and [`neg/`](neg) are the older legal-Java `.zinc` surface. They use
the same BEAM backend and remain tested by `../e2e.sh`, but new users should start with
`.zn` examples above.
