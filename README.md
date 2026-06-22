<p align="center">
  <img src="logo.png" alt="Zinc" width="320">
</p>

# Zinc

Zinc is a family of tools for writing cleaner source while still emitting target-native code.
The active project is the BEAM transpiler: Python-shaped `.zn` code with types, actors, and
OTP supervision, compiled to Erlang and run with the `zc` CLI.

| Project | Category | Target | Status |
|---------|----------|--------|--------|
| [beam-transpiler](beam-transpiler/) | Compiler + CLI | Erlang/OTP (BEAM) | **Active.** `.zn` braces-Python -> Erlang/OTP, with supervised actors, streaming I/O, HTTP, JSON, SQL, releases, and managed OTP tooling. |
| `beam-transpiler` legacy `.zinc` surface | Compiler frontend | Erlang/OTP (BEAM) | Maintained as a tested compatibility surface. New docs and examples target `.zn`. |

## Start Here

```sh
cd beam-transpiler
export PATH="$PWD/bin:$PATH"
zc run examples/py/hello.zn
```

Then read:

- [BEAM transpiler README](beam-transpiler/README.md) for the thesis and current status.
- [Getting started](beam-transpiler/docs/getting-started.md) for install, first script, first project, and flowdemo.
- [Guide](beam-transpiler/docs/guide.md) for the language and standard library surface.
- [Examples](beam-transpiler/examples/README.md) for the tested `.zn` reading path.

## Repository Map

- `beam-transpiler/` - the active compiler, `zc` CLI, prelude, examples, and dogfood apps.
- `beam-lab/` - lowering experiments and BEAM performance notes.
- `perf/` - benchmark scripts and results.
- `docs/` - repository-level design notes.

## License

[Apache License 2.0](LICENSE)
