# Getting Started

`zinc-csharp` is a build tool for C# / .NET 10 projects. It reads `zinc.toml`, generates a `.csproj`, and drives `dotnet publish` to produce a Native AOT binary. The `.csproj` is regenerated on every build — never edit it by hand.

## Install

```bash
curl -LsSf https://raw.githubusercontent.com/ZincScale/zinc/master/build-tools/zinc-csharp/install.sh | bash
```

The installer:

1. Installs the .NET 10 SDK to `~/.dotnet/` if it isn't already present.
2. Drops the `zinc-csharp` shell script into `~/.zinc/bin/`.
3. Adds both directories to `PATH` in `~/.bashrc` / `~/.zshrc`.

After install, `zinc-csharp` works from any directory containing a `zinc.toml`.

## Your first project

Create a directory with a `zinc.toml`:

```toml
[project]
name = "Hello"
version = "1.0.0"
main = "Hello/Program.cs"
source_dir = "Hello"
sdk = "Microsoft.NET.Sdk"

[csharp]
framework = "net10.0"
nullable = true
implicit_usings = true

[csharp.aot]
enabled = true
```

Drop a `Hello/Program.cs`:

```csharp
Console.WriteLine("Hello, World!");
```

Build and run:

```bash
zinc-csharp run            # JIT for fast iteration
zinc-csharp build          # Native AOT release binary
./build/Hello              # the AOT binary
```

## Commands

```bash
zinc-csharp build              # AOT publish (default)
zinc-csharp build --jit        # JIT build for fast iteration
zinc-csharp build linux-arm64  # Cross-compile AOT to a specific RID
zinc-csharp run                # Build + run
zinc-csharp run --jit          # JIT run
zinc-csharp test               # Run the test project
zinc-csharp csproj             # Regenerate .csproj only (don't build)
zinc-csharp clean              # Remove build artifacts
zinc-csharp doctor             # Check toolchain + project status
```

## `zinc.toml` reference

```toml
[project]
name = "MyApp"                    # Binary name + .csproj name
version = "1.0.0"
main = "MyApp/Program.cs"         # Entry point
source_dir = "MyApp"              # C# source dir (default: name)
sdk = "Microsoft.NET.Sdk"         # Or "Microsoft.NET.Sdk.Web" for ASP.NET

[csharp]
framework = "net10.0"
lang_version = "latest"
nullable = true
implicit_usings = true
unsafe = false                    # AllowUnsafeBlocks

[csharp.aot]
enabled = true                    # Native AOT compilation
strip_symbols = true
invariant_globalization = true    # No ICU dependency — smaller binary
optimization = "Size"             # "Size" or "Speed"
stack_traces = false
trim_metadata = true

[csharp.gc]
server = true                     # Server GC (throughput-optimized)

[csharp.nuget]
packages = ["YamlDotNet:16.3.0"]  # name:version

[csharp.references]
projects = ["../OtherProject/Other.csproj"]
```

## Test project layout

Tests live in a separate project that references the main one:

```
myproject/
├── zinc.toml                 # Main project
├── MyApp/
│   └── Program.cs
└── tests/
    ├── zinc.toml             # references = ["../MyApp/MyApp.csproj"]
    └── Tests/
        ├── Program.cs        # entry: var fails = TestSuite.Run(); return fails > 0 ? 1 : 0;
        └── TestSuite.cs
```

`zinc-csharp test` generates both `.csproj` files and runs the test project. Production binaries contain zero test code.

## How a build runs

1. `zinc-csharp build` confirms .NET 10 is present (auto-installs if not).
2. Reads `zinc.toml` and writes `<source_dir>/<name>.csproj`.
3. Runs `dotnet publish -r <rid> -c Release` for AOT, or `dotnet build` for `--jit`.
4. Output lands in `build/<name>` (stripped static binary for AOT).

## Further reading

- [Pooling & memory design](design-pooling.md) — strategies used by zinc-flow-csharp on hot paths (`ArrayPool`, ref-counted content, ThreadStatic pools).
