# Language Reference

Zinc compiles to native binaries via **C# AOT**. Convention over configuration — less typing, less ceremony, optimized native output.

## Table of Contents

| Topic | Description |
|-------|-------------|
| [Getting Started](getting-started.md) | Installation, CLI, project setup |
| [Types and Variables](types.md) | Variables, constants, type system, enums, null safety, type casting, string interpolation |
| [Functions](functions.md) | Functions, default/named args, generics, variadic, closures, `Fn` types |
| [Classes and OOP](classes.md) | Classes, interfaces, inheritance, polymorphism, generics, annotations |
| [Collections](collections.md) | List/map literals, slicing, LINQ collection methods |
| [Control Flow](control-flow.md) | If/else, loops, match/switch, safe navigation, concurrency (`spawn`, `parallel`, `Lock<T>`) |
| [Error Handling](error-handling.md) | Errors as values, `or` handlers, failable functions, `with` resources |
| [Imports](imports.md) | .NET imports, NuGet dependencies, type detection |
| [Built-in Functions](builtins.md) | All global builtins — I/O, math, JSON, HTTP, environment, control |
| [Testing](testing.md) | `zinc test`, assert builtins, test conventions |

## Quick Syntax Overview

```zinc
// Variables and constants
var x = 42
pub const String APP = "Zinc"

// Functions
Int add(Int a, Int b) { return a + b }

// Classes
Dog {
    pub String name
    new(String name) { this.name = name }
    pub String bark() { return "{this.name} says Woof!" }
}

// Interfaces and inheritance
interface Speaker { pub String speak() }
Cat : Speaker { pub String speak() { return "Meow!" } }

// Collections + LINQ (C# backend)
var nums = [5, 3, 8, 1, 9]
var big = nums.Where((Int x) -> x > 3).OrderBy((Int x) -> x)

// Error handling
var content = readFile("data.txt") or {
    print("Error: {err}")
    exit(1)
}

// Imports
import "System.Diagnostics"
var sw = Stopwatch()    // auto-emits new Stopwatch()

// Concurrency — no async/await
var result = spawn { fetchData() }
var items = parallel(ids) { process(it) }
print(result.value)

// Entry point
main() {
    print("Hello, Zinc!")
}
```

## Backend

Zinc targets **C# AOT** (Native AOT via .NET). The compiler transpiles `.zn` → `.cs`, generates a `.csproj` internally, and runs `dotnet publish` with AOT to produce a native binary.

| Property | Value |
|----------|-------|
| Binary size | ~1.6 MB |
| Startup | ~9 ms |
| Concurrency | `spawn`, `parallel`, `Lock<T>` |
| Collection methods | LINQ (Where, Select, OrderBy, ...) |
| Error handling | try/catch (generated from `or { }`) |
| Type detection | .NET reflection probe |
| Ecosystem | NuGet packages |
| Config | `zinc.toml` |
