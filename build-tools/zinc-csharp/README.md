# zinc-csharp

C# / .NET 10 build backend for Zinc projects. One-stop shop: installs .NET, reads `zinc.toml`, generates `.csproj`, produces Native AOT binaries.

## Install

```bash
# One command — installs .NET 10 SDK + zinc-csharp build tool + sets up PATH
curl -LsSf https://raw.githubusercontent.com/ZincScale/zinc/master/build-tools/zinc-csharp/install.sh | bash
```

Or from a local clone:

```bash
cd zinc/zinc-csharp
bash install.sh
```

After install, `zinc-csharp` works from any directory with a `zinc.toml`. If .NET is not found, it auto-installs on first run.

## Usage

```bash
zinc-csharp build          # Native AOT binary (default)
zinc-csharp build --jit    # JIT build (fast iteration)
zinc-csharp build linux-arm64  # Cross-compile AOT
zinc-csharp run            # Build + run
zinc-csharp run --jit      # JIT run (faster build, slower runtime)
zinc-csharp test           # Run tests (separate from production binary)
zinc-csharp csproj         # Regenerate .csproj only
zinc-csharp clean          # Remove build artifacts
zinc-csharp doctor         # Check toolchain and project status
```

## zinc.toml Schema

```toml
[project]
name = "MyApp"                    # Project name (binary name + csproj)
version = "1.0.0"
main = "MyApp/Program.cs"         # Entry point
source_dir = "MyApp"              # C# source directory (default: project name)
sdk = "Microsoft.NET.Sdk"         # Or "Microsoft.NET.Sdk.Web" for HTTP

[csharp]
framework = "net10.0"             # Target framework
lang_version = "latest"
nullable = true
implicit_usings = true
unsafe = false                    # AllowUnsafeBlocks

[csharp.aot]
enabled = true                    # Native AOT compilation
strip_symbols = true              # Strip debug symbols
invariant_globalization = true    # No ICU dependency
optimization = "Size"             # "Size" or "Speed"
stack_traces = false              # Include stack trace data
trim_metadata = true              # Trim reflection metadata

[csharp.gc]
server = true                     # Server GC (throughput-optimized)

[csharp.nuget]
packages = ["YamlDotNet:16.3.0"]  # NuGet dependencies (name:version)

[csharp.references]
projects = ["../OtherProject/Other.csproj"]  # ProjectReference paths
```

## How It Works

1. `zinc-csharp build` checks for .NET 10 (auto-installs if missing)
2. Reads `zinc.toml` and generates `<source_dir>/<name>.csproj`
3. Runs `dotnet publish -r <rid> -c Release` for AOT (or `dotnet build` for `--jit`)
4. Output: `build/<name>` (stripped static binary)

The `.csproj` is regenerated on every build — never edit it by hand.

## Test Separation

Tests live in a separate project (`tests/zinc.toml`) with a `ProjectReference` to the main project:

```
myproject/
├── zinc.toml                  # Main project
├── MyApp/
│   └── Program.cs
└── tests/
    ├── zinc.toml              # Test project (sdk = Web, references = ["../../MyApp/MyApp.csproj"])
    └── Tests/
        ├── Program.cs         # var failures = TestSuite.Run(); return failures > 0 ? 1 : 0;
        └── TestSuite.cs
```

`zinc-csharp test` generates both `.csproj` files and runs the test project. Production binary has zero test code.

## What Gets Installed

| Component | Location | Purpose |
|---|---|---|
| .NET 10 SDK | `~/.dotnet/` | Compiler + runtime |
| zinc-csharp | `~/.zinc/bin/` | Build tool (bash script) |
| PATH entries | `~/.bashrc` or `~/.zshrc` | Auto-added for both |

## Reference Implementation

See [zinc-flow-csharp](https://github.com/ZincScale/zinc-flow/tree/master/zinc-flow-csharp):
- 15 source files, full flow engine
- ThreadStatic pools, ArrayPool, ref-counted Content, zero-alloc hot paths
- 149 test assertions (separate project)
- 2M+ ff/s session throughput (Native AOT)
- 16MB stripped AOT binary (Web SDK)
