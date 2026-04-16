# Zinc

Thin transpilers that remove syntax warts from target languages. Clean source in, clean output you can read and edit directly.

| Project | Target | Status |
|---------|--------|--------|
| [zinc-go](zinc-go/) | Go | Active — 57 examples, classes, generics, streams with loop fusion, try/catch/throw, `using` for RAII, subpackages |
| [zinc-python](zinc-python/) | Python 3.14t | Self-hosting, braces syntax, auto f-strings |
| [zinc-csharp](zinc-csharp/) | C# (.NET 10 AOT) | Build tool + transpiler variant used by zinc-flow-csharp |
| [stdlib](stdlib/) | shared | `asserts`, `exceptions`, `config` (Viper), `logging` (slog) — imported via `import stdlib.<module>` |

## License

[Apache License 2.0](LICENSE)
