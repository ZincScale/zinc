<p align="center">
  <img src="logo.png" alt="Zinc" width="320">
</p>

# Zinc

Zinc is a personal compiler and language-design project: a family of experiments in writing
cleaner source while still emitting target-native code. It is maintained for exploration,
learning, and use in the author's own projects rather than as a proposed mainstream language
or commercial product.

| Project | Category | Target | Status |
|---------|----------|--------|--------|
| [compilers/zinc-go](compilers/zinc-go/) | Compiler + CLI | Go | Personal language/compiler project; feature-complete enough for continued experimentation and existing uses. |
| [dialects/zinc-python](dialects/zinc-python/) | Transpiler + CLI | Python | Earlier braces-Python experiment retained for reference and personal use. |
| [build-tools/zinc-java](build-tools/zinc-java/) | Build tool | Java | Personal toolchain experiment. |
| [build-tools/zinc-csharp](build-tools/zinc-csharp/) | Build tool | C#/.NET | Personal toolchain experiment. |
| [build-tools/pymgr](build-tools/pymgr/) | Development tool | Python projects | Optional uv environment, module/API, refactoring, and loop-analysis coordinator. |

## Repository Map

- `compilers/zinc-go/` - the typed OO language and Go source compiler.
- `dialects/zinc-python/` - the braces-Python to Python transpiler experiment.
- `build-tools/` - Java, C#/.NET, and Python-project tooling experiments.
- `perf/` - benchmark scripts and results.
- `docs/` - repository-level design notes.

## License

[Apache License 2.0](LICENSE)
