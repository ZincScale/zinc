# zinc-csharp

C# / .NET 10 build backend for Zinc projects. Reads `zinc.toml`, generates `.csproj`, produces Native AOT binaries.

## Install

```bash
# Copy the build tool to your PATH
cp zinc-csharp/build-tool/zinc-csharp ~/.zinc/bin/

# Or symlink
ln -s $(pwd)/zinc-csharp/build-tool/zinc-csharp ~/.zinc/bin/zinc-csharp
```

Requires .NET 10 SDK: `curl -sSL https://dot.net/v1/dotnet-install.sh | bash -s -- --channel 10.0`

## Usage

From any directory with a `zinc.toml`:

```bash
zinc-csharp build          # Native AOT binary (default)
zinc-csharp build --jit    # JIT build (fast iteration)
zinc-csharp run            # Build + run
zinc-csharp test           # Run tests (excluded from production binary)
zinc-csharp clean          # Remove build artifacts
```

## zinc.toml Schema

```toml
[project]
name = "MyApp"                    # Project name (used for binary name + csproj)
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
```

## How It Works

1. `zinc-csharp build` reads `zinc.toml`
2. Generates `<source_dir>/<name>.csproj` with all settings
3. Runs `dotnet publish -r <rid> -c Release` for AOT
4. Output: `build/<name>` (static binary)

The `.csproj` is generated on every build — never edit it by hand. All configuration lives in `zinc.toml`.

## Test Separation

`zinc-csharp test` generates a separate `.csproj` that includes `Tests/` files and disables AOT. Tests are never compiled into the production binary.

## Reference Implementation

See `zinc-flow/zinc-flow-csharp/` for a complete example:
- 15 source files, 6,300 LOC
- ThreadStatic object pools, ArrayPool, zero-alloc hot paths
- 149 test assertions
- 2.5M+ flowfiles/sec session throughput (AOT)
