# zinc-go — Zinc to Go transpiler

Zinc syntax → clean, readable Go output.

## Status

**Working:**
- Functions with typed params and return types
- String interpolation → `fmt.Sprintf`
- Control flow: if/else if/else, for-range, while, match/switch, break/continue
- Arrays with typed declarations
- Structs (classes) with constructors, methods, implicit self
- Data classes (records) with auto-generated String()
- Sealed types → interface + variant structs
- Interfaces
- Enums → iota
- Concurrency: `spawn` → goroutine, `parallel for` → WaitGroup
- String methods → `strings.*` stdlib
- Source maps via `//line` directives — Go errors point to .zn files
- `new Type()` → `NewType()`

**Known issues (to fix):**
- **Function types**: `Fn<(Params), Return>` → `func(params) return` — parsed but needs testing
- **Default parameters**: Go doesn't have them — needs variadic/builder pattern
- **Error handling**: `return Error()` / `or` handler → needs `(T, error)` multi-return
- **Stream operations**: `.filter()`, `.map()` on slices — needs Go generics or codegen helpers
- **Nullable types**: `String?` → `*string` pointer semantics
- **Inherited method calls**: cross-struct method resolution
- **Collection methods**: `.add()`, `.put()` work as statements, but chained operations don't yet

## Usage

```bash
# Build the compiler
cd zinc-go
go build -o zinc ./cmd/zinc/

# Transpile and run
./zinc run examples/hello.zn

# Transpile to .go files
./zinc build examples/hello.zn -o output/

# Run directly
./zinc examples/hello.zn
```

## Example

**hello.zn:**
```zinc
fn greet(String name): String {
    return "Hello, {name}!"
}

fn main() {
    print(greet("World"))
}
```

**Output (hello.go):**
```go
package main

import "fmt"

func greet(name string) string {
    return fmt.Sprintf("Hello, %v!", name)
}

func main() {
    fmt.Println(greet("World"))
}
```

## E2E Tests

```bash
bash run_e2e.sh
```

Currently 3/12 passing (hello, arrays, control_flow). The remaining tests exercise features that need deeper Go-specific design work (error handling, generics, default params).

## Architecture

```
zinc-go/
  cmd/zinc/         — CLI: build, run
  internal/
    lexer/          — tokenizer
    parser/         — AST builder (shared with Java-era compiler)
    typechecker/    — type inference
    codegen_go/     — Go code generator (new)
    errs/           — colored error output
  examples/         — .zn test files
  expected/         — expected outputs for e2e
```

The compiler is written in Go. The lexer, parser, and AST are restored from the pre-Java-pivot codebase. The codegen is new — it replaces the Java emitter with a Go emitter.
