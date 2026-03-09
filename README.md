<img alt="Growler logo" src="./logo.jpg" />


# Growler

**Growler** is an object-oriented language that transpiles to Go. Write clean, expressive OO code — get fast, idiomatic Go output.

```
fn main() {
    var name: String = "World"
    print("Hello, {name}!")
}
```

Transpiles to:

```go
func main() {
    name := "World"
    fmt.Println(fmt.Sprintf("Hello, %v!", name))
}
```

---

## Why Growler?

Go is fast, simple, and has excellent tooling — but its lack of traditional OO features can feel limiting for developers coming from Java, Kotlin, C#, Python, or TypeScript. Growler bridges that gap:

- **Familiar OO syntax** — classes, interfaces, inheritance, constructors, and `this`
- **Modern conveniences** — null safety (`?.`), string interpolation, `try/catch`, `with` resource management, lambdas, enums, generics
- **Zero runtime overhead** — everything compiles to plain Go; no reflection, no runtime library
- **Full Go interop** — import any Go package, call any Go function, use any Go type
- **Transparent output** — the generated `.go` files are readable, idiomatic, and `go vet`-clean

Growler doesn't replace Go — it's a better way to write it.

---

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](docs/getting-started.md) | Installation, CLI usage, and running examples |
| [Language Reference](docs/language-reference.md) | Complete syntax guide — variables, functions, classes, control flow, and more |
| [Built-in Functions](docs/builtins.md) | All built-in functions with Go equivalents |

---

## Quick Start

```bash
git clone https://github.com/victorybhg/growler
cd growler
go build -o growler ./cmd/growler/
./growler examples/hello.gw --run
```

Requires **Go 1.21+**. See the [Getting Started](docs/getting-started.md) guide for full details.

---

## Feature Highlights

- Classes, interfaces, and inheritance
- Generic functions and classes
- Null safety with `?.` safe navigation
- `try` / `catch` / `throw` error handling
- `with` resource management (auto-close, auto-unlock)
- Closures and higher-order functions (`Fn<>` types)
- Enums with `match` expressions
- String interpolation
- Default parameters and named arguments
- Labeled loops
- Tuple unpacking for multi-return functions
- Constants
- Interactive REPL

---

## License

[Apache License 2.0](LICENSE)
