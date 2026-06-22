# Examples

The primary examples are `.zn` programs in [`py/`](py/). They are run by
[`../e2e-py.sh`](../e2e-py.sh): each file is transpiled, compiled, executed on a real BEAM,
and checked against expected output.

Run the whole suite:

```sh
cd beam-transpiler
./e2e-py.sh
```

Run one example directly:

```sh
zc run examples/py/hello.zn
zc run examples/py/resources.zn
```

## Recommended Reading Path

| Topic | Examples |
|-------|----------|
| First programs | [`hello.zn`](py/hello.zn), [`functions.zn`](py/functions.zn), [`countdown.zn`](py/countdown.zn), [`fizzbuzz.zn`](py/fizzbuzz.zn) |
| Control flow | [`bools.zn`](py/bools.zn), [`ternary.zn`](py/ternary.zn), [`breakcont.zn`](py/breakcont.zn), [`match.zn`](py/match.zn) |
| Types and values | [`records.zn`](py/records.zn), [`record_model.zn`](py/record_model.zn), [`sealed.zn`](py/sealed.zn), [`protocols.zn`](py/protocols.zn) |
| Collections | [`collections.zn`](py/collections.zn), [`dict.zn`](py/dict.zn), [`multifile/`](py/multifile) |
| Errors | [`trycatch.zn`](py/trycatch.zn), [`exceptions.zn`](py/exceptions.zn) |
| Actors and supervision | [`counter.zn`](py/counter.zn), [`counter_init.zn`](py/counter_init.zn), [`supervised.zn`](py/supervised.zn), [`selfheal.zn`](py/selfheal.zn) |
| Files and resources | [`fileio.zn`](py/fileio.zn), [`filestream.zn`](py/filestream.zn), [`resources.zn`](py/resources.zn) |
| Channels and pipelines | [`channel.zn`](py/channel.zn), [`pipeline.zn`](py/pipeline.zn) |
| JSON and HTTP | [`json.zn`](py/json.zn), [`http_client.zn`](py/http_client.zn), [`http_facade.zn`](py/http_facade.zn) |
| SQL | [`sql.zn`](py/sql.zn) |
| Encoding and auth utilities | [`encoding.zn`](py/encoding.zn), [`webauth.zn`](py/webauth.zn) |
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

[`py_neg/`](py_neg) contains `.zn` programs that must fail to compile. These pin down type
errors, invalid application shapes, non-exhaustive matches, reassignment rules, and inference
cycles. Expected errors are asserted in `../e2e-py.sh`.

## Legacy Legal-Java Examples

[`programs/`](programs) and [`neg/`](neg) are the older legal-Java `.zinc` surface. They use
the same BEAM backend and remain tested by `../e2e.sh`, but new users should start with
`.zn` examples above.
