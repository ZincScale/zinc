# caravan-csharp

C# / .NET 10 build backend for Caravan projects. One-stop shop: installs .NET, reads `caravan.toml`, generates `.csproj`, produces Native AOT binaries.

## Install

```bash
# One command — installs .NET 10 SDK + caravan-csharp build tool + sets up PATH
curl -LsSf https://raw.githubusercontent.com/CaravanScale/caravan/master/caravan-csharp/install.sh | bash
```

Or from a local clone:

```bash
cd caravan/caravan-csharp
bash install.sh
```

After install, `caravan-csharp` works from any directory with a `caravan.toml`. If .NET is not found, it auto-installs on first run.

## Usage

```bash
caravan-csharp build          # Native AOT binary (default)
caravan-csharp build --jit    # JIT build (fast iteration)
caravan-csharp build linux-arm64  # Cross-compile AOT
caravan-csharp run            # Build + run
caravan-csharp run --jit      # JIT run (faster build, slower runtime)
caravan-csharp test           # Run tests (separate from production binary)
caravan-csharp csproj         # Regenerate .csproj only
caravan-csharp clean          # Remove build artifacts
caravan-csharp doctor         # Check toolchain and project status
```

## caravan.toml Schema

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

1. `caravan-csharp build` checks for .NET 10 (auto-installs if missing)
2. Reads `caravan.toml` and generates `<source_dir>/<name>.csproj`
3. Runs `dotnet publish -r <rid> -c Release` for AOT (or `dotnet build` for `--jit`)
4. Output: `build/<name>` (stripped static binary)

The `.csproj` is regenerated on every build — never edit it by hand.

## Test Separation

Tests live in a separate project (`tests/caravan.toml`) with a `ProjectReference` to the main project:

```
myproject/
├── caravan.toml                  # Main project
├── MyApp/
│   └── Program.cs
└── tests/
    ├── caravan.toml              # Test project (sdk = Web, references = ["../../MyApp/MyApp.csproj"])
    └── Tests/
        ├── Program.cs         # var failures = TestSuite.Run(); return failures > 0 ? 1 : 0;
        └── TestSuite.cs
```

`caravan-csharp test` generates both `.csproj` files and runs the test project. Production binary has zero test code.

## What Gets Installed

| Component | Location | Purpose |
|---|---|---|
| .NET 10 SDK | `~/.dotnet/` | Compiler + runtime |
| caravan-csharp | `~/.caravan/bin/` | Build tool (bash script) |
| PATH entries | `~/.bashrc` or `~/.zshrc` | Auto-added for both |

## Reference Implementation

See [caravan-flow-csharp](https://github.com/CaravanScale/caravan-flow/tree/master/caravan-flow-csharp):
- 15 source files, full flow engine
- ThreadStatic pools, ArrayPool, ref-counted Content, zero-alloc hot paths
- 149 test assertions (separate project)
- 2M+ ff/s session throughput (Native AOT)
- 16MB stripped AOT binary (Web SDK)
