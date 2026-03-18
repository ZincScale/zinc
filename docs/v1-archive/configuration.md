# Configuration

Zinc projects are configured via `zinc.toml` in the project root. Run `zinc init` to generate one.

## Minimal Config

```toml
[project]
name = "myapp"
version = "0.1.0"

[build]
target = "csharp"
```

This is all you need. Zinc defaults to C# AOT with optimizations enabled.

## Full Reference

```toml
[project]
name = "myapp"
version = "0.1.0"

[build]
target = "csharp"     # "csharp" (default) — only supported target
optimize = true       # AOT with speed optimizations (default: true)

# Dependencies — managed via zinc add/remove
[dependencies]
Serilog = "4.3.1"
"AWSSDK.SQS" = "4.0.2"
"Newtonsoft.Json" = "13.0.3"

# NuGet source configuration (optional — defaults to nuget.org)
[nuget]
source = "https://api.nuget.org/v3-flatcontainer"   # default source for zinc add

# Named sources for private registries
[[nuget.sources]]
name = "github"
url = "https://nuget.pkg.github.com/yourorg/index.json"
auth = "env:GITHUB_TOKEN"
type = "bearer"

[[nuget.sources]]
name = "artifactory"
url = "https://yourorg.jfrog.io/artifactory/api/nuget/v3/nuget-local/flat"
auth = "env:ARTIFACTORY_TOKEN"
type = "bearer"
```

## Sections

### `[project]`

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `name` | string | directory name | Project name — used for binary name and .csproj |
| `version` | string | `"0.1.0"` | Project version |

### `[build]`

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `target` | string | `"csharp"` | Compilation target |
| `optimize` | bool | `true` | Enable AOT with speed optimizations. Set to `false` for faster dev builds. |

The `--release` flag on `zinc build --release` strips debug symbols for production.

### `[dependencies]`

Package dependencies managed by `zinc add` and `zinc remove`:

```bash
zinc add Serilog                           # latest from nuget.org
zinc add AWSSDK.SQS --version 4.0.2       # specific version
zinc add MyLib --source github             # from named source
zinc remove Serilog                        # remove
zinc deps                                  # list all
```

Dependencies are written to `zinc.toml` and become `<PackageReference>` entries in the generated `.csproj`.

### `[nuget]`

Configure where `zinc add` looks for packages. Optional — defaults to the public NuGet registry.

#### Simple: single custom source

```toml
[nuget]
source = "https://pkgs.dev.azure.com/yourorg/_packaging/feed/nuget/v3/flat2"
```

#### Enterprise: multiple sources with auth

```toml
[[nuget.sources]]
name = "github"
url = "https://nuget.pkg.github.com/yourorg/index.json"
auth = "env:GITHUB_TOKEN"
type = "bearer"

[[nuget.sources]]
name = "artifactory"
url = "https://yourorg.jfrog.io/artifactory/api/nuget/v3/nuget-local/flat"
auth = "env:ARTIFACTORY_TOKEN"
type = "bearer"
```

Then use `--source` to target a specific registry:

```bash
zinc add MyCompany.Shared --source github
zinc add Internal.Utils --source artifactory
zinc add Serilog                             # still uses nuget.org (default)
```

#### Auth configuration

| Key | Type | Description |
|-----|------|-------------|
| `name` | string | Source name — used with `--source` flag |
| `url` | string | NuGet flat container API URL |
| `auth` | string | Auth token reference — use `env:VARNAME` to read from environment |
| `type` | string | Auth type: `"bearer"` (default) or `"basic"` |

**Never store tokens directly in zinc.toml.** Use `env:VARNAME` to reference environment variables:

```toml
auth = "env:GITHUB_TOKEN"       # reads $GITHUB_TOKEN at runtime
auth = "env:ARTIFACTORY_TOKEN"  # reads $ARTIFACTORY_TOKEN at runtime
```

Set the environment variables in your shell profile or CI secrets:

```bash
# Local development
export GITHUB_TOKEN="ghp_xxxxxxxxxxxx"
export ARTIFACTORY_TOKEN="your-api-key"

# GitHub Actions
# Settings → Secrets → GITHUB_TOKEN (auto-provided)
# Settings → Secrets → ARTIFACTORY_TOKEN (add manually)
```

### GitHub Packages Setup

1. Create a Personal Access Token with `read:packages` scope
2. Set it as an environment variable:
   ```bash
   export GITHUB_TOKEN="ghp_xxxxxxxxxxxx"
   ```
3. Add to `zinc.toml`:
   ```toml
   [[nuget.sources]]
   name = "github"
   url = "https://nuget.pkg.github.com/yourorg/index.json"
   auth = "env:GITHUB_TOKEN"
   type = "bearer"
   ```
4. Install packages:
   ```bash
   zinc add MyCompany.Shared --source github
   ```

### JFrog Artifactory Setup

1. Get an API key from Artifactory → User Profile → API Key
2. Set it as an environment variable:
   ```bash
   export ARTIFACTORY_TOKEN="your-api-key"
   ```
3. Add to `zinc.toml`:
   ```toml
   [[nuget.sources]]
   name = "artifactory"
   url = "https://yourorg.jfrog.io/artifactory/api/nuget/v3/nuget-local/flat"
   auth = "env:ARTIFACTORY_TOKEN"
   type = "bearer"
   ```
4. Install packages:
   ```bash
   zinc add Internal.Utils --source artifactory
   ```

## Build Output

`zinc build` generates all build artifacts in `.zinc-build/`:

```
myapp/
  zinc.toml
  main.zn
  .zinc-build/         ← generated, git-ignored
    main.cs
    myapp.csproj
    bin/
    obj/
  myapp                ← final native binary (copied to project root)
```

Add `.zinc-build/` to your `.gitignore`.
