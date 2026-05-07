<p align="center">
  <img src="logo.png" alt="Zinc" width="320">
</p>

# Zinc

A family of tools that remove syntax warts from target languages. Clean source in, clean output you can read and edit directly.

| Project | Category | Target | What it does |
|---------|----------|--------|--------------|
| [zinc-go](compilers/zinc-go/) | Compiler | Go | Full Zinc language → idiomatic Go. Classes, generics, sealed types, channels, goroutines via `spawn`, `catch { }` error handling. |
| [zinc-python](dialects/zinc-python/) | Dialect | Python 3.14t | Braces → indentation, method-name dunders, implicit `self`, auto f-strings. Roundtrips with `from-py`. |
| [zinc-csharp](build-tools/zinc-csharp/) | Build tool | C# / .NET 10 | Reads `zinc.toml`, generates `.csproj`, drives `dotnet publish` for Native AOT. |
| [zinc-java](build-tools/zinc-java/) | Build tool | Java 25 | Scaffolds an sbt project, drives `zinc init / build / run / test`. Bundles a managed JDK. |

The repo groups projects by what they actually do:

- **`compilers/`** — Zinc-language transpilers. Currently `zinc-go`.
- **`dialects/`** — Other source languages in the Zinc family — different surface syntax, same "fix the warts, emit readable output" ethos.
- **`build-tools/`** — Project-scaffold + build CLIs for native targets. These read `zinc.toml` but don't transpile from Zinc; they wrap the target's native toolchain.

## License

[Apache License 2.0](LICENSE)
