# Getting Started

## Installation

```bash
go install github.com/victorybhg/zinc/cmd/zinc@latest
```

Or build from source:

```bash
git clone https://github.com/victorybhg/zinc
cd zinc
go build -o zinc ./cmd/zinc/
```

Requires **Go 1.26+** for building the compiler from source, and **.NET 10+ SDK** for building Zinc projects.

## Quick Start

Create a project and run it:

```bash
mkdir myapp && cd myapp
zinc init myapp
zinc run
```

This creates:

```
myapp/
  zinc.toml     # Project config (target, dependencies, optimization)
  main.zn       # Entry point
```

The generated `zinc.toml`:

```toml
[project]
name = "myapp"
version = "0.1.0"

[build]
target = "csharp"
optimize = true      # AOT with full optimizations
```

The generated `main.zn`:

```
main() {
    print("Hello from Zinc!")
}
```

## Building

```bash
zinc build          # → native AOT binary (C# target)
./myapp             # run the binary
```

`zinc build` reads `zinc.toml`, transpiles `.zn` → `.cs`, generates a `.csproj` internally (you never see it), and runs `dotnet publish` with AOT. The native binary is copied to your project root.

## Adding Dependencies

Add NuGet packages in `zinc.toml`:

```toml
[dependencies]
"Newtonsoft.Json" = "13.0.3"
"Serilog" = "4.0.0"
```

These are automatically included in the build — no `dotnet add package` or XML editing needed.

Then import and use them in your Zinc code:

```zinc
import "Newtonsoft.Json"

main() {
    var json = JsonConvert.SerializeObject(42)
    print(json)
}
```

Zinc also provides short aliases for common .NET namespaces:

| Zinc Import | C# Namespace |
|-------------|-------------|
| `import "http"` | `System.Net.Http` |
| `import "json"` | `System.Text.Json` |
| `import "io"` | `System.IO` |
| `import "regex"` | `System.Text.RegularExpressions` |
| `import "threading"` | `System.Threading` |
| `import "tasks"` | `System.Threading.Tasks` |
| `import "diagnostics"` | `System.Diagnostics` |
| `import "net"` | `System.Net` |
| `import "crypto"` | `System.Security.Cryptography` |
| `import "text"` | `System.Text` |
| `import "xml"` | `System.Xml` |

When using `zinc build` or `zinc run`, the compiler runs a .NET type probe that discovers all available types from the BCL and your NuGet dependencies. This means constructor calls like `HttpClient()` and `Stopwatch()` automatically emit `new` — no extra configuration needed.

## Multi-File Projects

Create subdirectories for packages:

```
myapp/
  zinc.toml
  main.zn
  models/
    user.zn
  utils/
    math.zn
```

**`utils/math.zn`** — helper functions:

```
package "myapp/utils"

pub Int add(Int a, Int b) {
    return a + b
}
```

**`models/user.zn`** — a class:

```
package "myapp/models"

User {
    String name
    Int age

    new(String name, Int age) {
        this.name = name
        this.age = age
    }

    pub String greet() {
        return "Hi, I'm {this.name}!"
    }
}
```

**`main.zn`** — import and use:

```
import "myapp/utils"
import "myapp/models"

main() {
    var sum = utils.add(2, 3)
    print(sum)

    var user = models.User("Alice", 30)
    print(user.greet())
}
```

Then build and run:

```bash
zinc run
```

## CLI Reference

```bash
zinc <file.zn>               # transpile a single file
zinc <file.zn> -o out.cs      # specify output file
zinc <file.zn> --run          # transpile and run immediately
zinc <file.zn> --watch        # watch for changes, re-transpile automatically
zinc <file.zn> --verbose      # show token/AST debug info
zinc init [name]              # initialize a new project (creates zinc.toml + main.zn)
zinc build [dir]              # transpile + compile (native AOT binary)
zinc run [dir]                # transpile + run
zinc repl                     # launch interactive REPL
zinc --version                # print version
```

### Project Commands

| Command | What it does |
|---------|-------------|
| `zinc init [name]` | Scaffold a new project (`zinc.toml` + `main.zn`) |
| `zinc build [dir]` | Transpile + compile to native binary |
| `zinc run [dir]` | Transpile + run |
| `zinc repl` | Launch the interactive REPL |

### Single-File Commands

| Flag | Short | Description |
|------|-------|-------------|
| `--run` | `-r` | Transpile and run |
| `--watch` | `-w` | Watch mode (re-transpile on save) |
| `--verbose` | `-v` | Debug output |
| `--version` | `-V` | Print version |
| `-o <file>` | | Output file path |

### Source Maps

Zinc emits line directives in the generated code. If the compiler reports an error, the file and line number point back to your `.zn` source — not the generated output. You debug in Zinc, not in C#.

## Running Examples

The [`examples/`](../examples/) directory contains working Zinc programs:

| Example | Description |
|---------|-------------|
| [`hello.zn`](../examples/hello.zn) | Hello World |
| [`classes.zn`](../examples/classes.zn) | Classes, interfaces, inheritance, polymorphism |
| [`closures.zn`](../examples/closures.zn) | Lambdas, closures, higher-order functions |
| [`enums.zn`](../examples/enums.zn) | Enums + match |
| [`builtins.zn`](../examples/builtins.zn) | Built-in functions — toString, abs, sqrt, jsonEncode, readFile, getEnv |
| [`errors.zn`](../examples/errors.zn) | Error handling with `or` |
| [`generics.zn`](../examples/generics.zn) | Generic functions and classes |
| [`collections.zn`](../examples/collections.zn) | Lists, maps, slicing |
| [`csharp-only/linq.zn`](../examples/csharp-only/linq.zn) | LINQ methods — Where, Select, OrderBy, First, Sum, Aggregate, chaining (C# backend) |
| [`csharp-only/annotations.zn`](../examples/csharp-only/annotations.zn) | Annotations — @JsonPropertyName, @Serializable, C# attributes (C# backend) |
| [`concurrency.zn`](../examples/concurrency.zn) | Channels + goroutines |
| [`with_resources.zn`](../examples/with_resources.zn) | Resource management |

Run any example:

```bash
zinc run
```
