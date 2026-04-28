<p align="center">
  <img src="logo.png" alt="Zinc" width="320">
</p>

# Zinc

Thin transpilers that remove syntax warts from target languages. Clean source in, clean output you can read and edit directly.

| Project | Target | What it does |
|---------|--------|--------------|
| [zinc-go](zinc-go/) | Go | Full Zinc language → idiomatic Go. Classes, generics, sealed types, channels, goroutines via `spawn`, `or { }` error handling. |
| [zinc-csharp](zinc-csharp/) | C# / .NET 10 | Build tool: reads `zinc.toml`, generates `.csproj`, drives `dotnet publish` for Native AOT. |
| [zinc-python](zinc-python/) | Python 3.14t | Braces → indentation, method-name dunders, implicit `self`, auto f-strings. Roundtrips with `from-py`. |
| [zinc-java](zinc-java/) | Java 25 | Build tool: scaffolds an sbt project, drives `zinc init / build / run / test`. Bundles a managed JDK. |

## License

[Apache License 2.0](LICENSE)
