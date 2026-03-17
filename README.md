<img alt="Zinc mascot" src="./zinc-badger.svg" height="180" /> <img alt="Zinc logo" src="./zinc-wordmark.svg" width="480" />


# Zinc

**Zinc** is a convention-over-configuration language that compiles to native binaries via C# AOT. Less typing, less ceremony, optimized native output — think Spring Boot for compiled apps.

```
main() {
    var name = "World"
    print("Hello, {name}!")
}
```

```bash
$ zinc build
  built hello (AOT native binary, 1.3 MB)

$ ./hello
  Hello, World!
```

---

## Why Zinc?

Enterprise developers want **familiar OO syntax** without **ceremony**. Zinc gives you classes, interfaces, generics, LINQ-style collection methods, and error handling — then compiles to a **1-2 MB native binary** with zero runtime dependencies.

- **No boilerplate** — no `public static void Main`, no `using` statements, no XML project files
- **Familiar syntax** — classes, interfaces, inheritance, generics, lambdas, `match`
- **LINQ collection methods** — `Where`, `Select`, `OrderBy`, `Aggregate`, `First`, and more
- **Clean error handling** — `or {}` handlers instead of 6-line try/catch blocks
- **Native AOT binaries** — 1-2 MB, ~9ms startup, fully tree-shaken and optimized
- **Zero config** — `zinc.toml` replaces XML; `zinc build` does the rest
- **Native AOT** — compiles to optimized native binaries via C# AOT

---

## Documentation

### User Guide

| Document | Description |
|----------|-------------|
| [Getting Started](docs/getting-started.md) | Installation, CLI, project setup, dependencies |
| [Language Reference](docs/language-reference.md) | Quick syntax overview and doc index |

### Language

| Document | Description |
|----------|-------------|
| [Types and Variables](docs/types.md) | Variables, constants, type system, enums, null safety, casting |
| [Functions](docs/functions.md) | Functions, defaults, named args, generics, variadic, closures |
| [Classes and OOP](docs/classes.md) | Classes, interfaces, inheritance, polymorphism |
| [Collections](docs/collections.md) | List/map literals, slicing, LINQ methods |
| [Control Flow](docs/control-flow.md) | If/else, loops, match, labeled loops, safe navigation |
| [Error Handling](docs/error-handling.md) | Errors as values, `or` handlers, `with` resources |
| [Imports](docs/imports.md) | .NET imports, NuGet dependencies, type detection |
| [Built-in Functions](docs/builtins.md) | I/O, math, JSON, HTTP, environment, control |

### Design Docs

| Document | Status |
|----------|--------|
| [C# AOT Backend](docs/design-csharp-aot-backend.md) | Implemented |
| [Pointer Inference](docs/design-pointer-inference.md) | Implemented |
| [Annotations & Serialization](docs/design-annotations-serialization.md) | Planned |
| [Syntax Simplification](docs/design-syntax-simplification.md) | Complete |
| [Type-Before-Name](docs/design-type-before-name.md) | Complete |

---

## Installation

**From source** (requires Go 1.26+):

```bash
go install github.com/victorybhg/zinc/cmd/zinc@latest
```

**Pre-built binaries**: download from [GitHub Releases](https://github.com/victorybhg/zinc/releases).

**Prerequisites**: .NET 10+ SDK for building Zinc projects.

---

## Quick Start

```bash
# start a project
mkdir myapp && cd myapp
zinc init myapp
zinc build        # → native AOT binary
./myapp           # → "Hello from Zinc!"

# or just run it
zinc run
```

`zinc init` creates a `zinc.toml` project config and `main.zn` entry point. No XML, no `.csproj`, no ceremony.

See the [Getting Started](docs/getting-started.md) guide for multi-file projects, dependencies, and full CLI reference.

---

## Feature Highlights

- Classes, interfaces, and inheritance (1:1 mapping to C#)
- Generic functions and classes
- LINQ collection methods (`Where`, `Select`, `OrderBy`, `First`, `Sum`, `Aggregate`, etc.)
- Field and constant visibility (`pub` / private by default)
- Null safety with `?.` safe navigation
- Errors as values with auto-propagation and `or` handlers
- `with` resource management (auto-close, auto-unlock)
- Closures and higher-order functions (`Fn` types)
- Enums with `match` expressions
- String interpolation
- Default parameters and named arguments
- Variadic functions and spread operator
- `zinc.toml` project config — no XML
- Native AOT compilation with full optimizations (tree shaking, strip, speed)
- Native AOT compilation via C# backend
- Interactive REPL

---

## Example

```
Dog {
    pub String name

    new(String name) {
        this.name = name
    }

    pub String bark() {
        return "{this.name} says Woof!"
    }
}

main() {
    var dogs = [Dog("Rex"), Dog("Buddy"), Dog("Max")]
    var names = dogs.Select((Dog d) -> d.name).OrderBy((String s) -> s)
    for name in names {
        print(name)
    }
}
```

Output: `Buddy`, `Max`, `Rex`

---

## License

[Apache License 2.0](LICENSE)
