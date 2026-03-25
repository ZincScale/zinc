<img alt="Zinc mascot" src="./zinc-badger.svg" height="180" /> <img alt="Zinc logo" src="./zinc-wordmark.svg" width="480" />


# Zinc

**Zinc** is a convention-over-configuration JVM language that transpiles to Java 25. Write `.zn` files, get clean syntax, the full Java ecosystem, and native binaries via GraalVM.

```zinc
fn greet(String name): String {
    return "Hello, {name}!"
}

fn main() {
    print(greet("World"))
}
```

```bash
$ zinc run hello.zn
Hello, World!

$ zinc build hello.zn    # produces native binary (13MB, 22ms startup)
$ ./hello
Hello, World!
```

---

## Why Zinc?

Java 25 is the best runtime for server applications. But it has ceremony. Zinc removes it while keeping the ecosystem.

- **Clean syntax** — no semicolons, `fn` instead of method signatures, type inference
- **`data` → Java records** — `data User(String name, int age)` with equals/hashCode/toString
- **Sealed classes** — `sealed class Shape { data Circle(double r) }` with pattern matching
- **Errors as values** — `or {}` handler, no try/catch in user code
- **Virtual threads** — `spawn`, `parallel for`, `concurrent` with `StructuredTaskScope`
- **Fluent collections** — `.filter(it > 0).map(it * 2)` without `.stream()/.toList()`
- **`==` is structural** — `===` for reference identity
- **Native binaries** — GraalVM native-image by default (~13MB, ~22ms startup)
- **No runtime** — transpiles to standard Java 25, no Zinc runtime library

---

## Install

```bash
# Requires: GraalVM JDK 25+
curl -sSL https://raw.githubusercontent.com/ZincScale/zinc/master/install.sh | sh
```

Or build from source:

```bash
cd compiler && make build && make native
# Binary: compiler/zinc
```

---

## Quick Start

```bash
# Script mode — no boilerplate
echo 'print("Hello from Zinc!")' > hello.zn
zinc run hello.zn

# Create a project
zinc init my-app
zinc run my-app/src/main.zn

# Build native binary
zinc build hello.zn
./hello    # 22ms startup
```

---

## Feature Highlights

```zinc
// Data classes → Java records
data User(String name, String email, int age = 0)

// Sealed classes → sealed interfaces + record variants
sealed class Shape {
    data Circle(double radius)
    data Rect(double width, double height)
}

// Pattern matching (Java 25 switch expressions)
var area = match shape {
    case Circle(r) { Math.PI * r ** 2 }
    case Rect(w, h) { w * h }
}

// Error handling — errors as values
fn loadConfig(String path): String {
    var data = Files.readString(Path.of(path)) or {
        return Error("config not found")
    }
    return data
}
var config = loadConfig("app.conf") or { "{}" }

// Virtual threads + structured concurrency
var (user, orders) = concurrent {
    fetchUser(id)
    fetchOrders(id)
}

parallel for order in orders {
    process(order)
}

var worker = spawn {
    while running {
        var msg = inbox.take() or { return }
        handle(msg)
    }
} or {
    print("worker crashed: {err}")
}

// Fluent collections with it keyword
var active = orders.filter(it.status == "active")
var total = active.map(it.amount).sum()
```

---

## How It Works

```
hello.zn → Zinc compiler → Hello.java → javac → native-image → hello
             (native)       (Java 25)   (class)   (optional)   (binary)
```

The Zinc compiler is itself a native binary (20MB). It:
1. **Lexes + parses** Zinc syntax
2. **Type-checks** with a static JDK type database (no reflection)
3. **Transforms** to a Java AST via JavaParser
4. **Emits** clean Java 25 source (records, sealed interfaces, pattern matching)
5. **Compiles** via javac (or delegates to Mill for projects)
6. **Optionally** produces native binaries via GraalVM native-image

---

## Documentation

| Document | Description |
|----------|-------------|
| [Language Reference](docs/language-reference.md) | Full syntax guide |
| [Getting Started](docs/getting-started.md) | First project walkthrough |
| [Concurrency](docs/lang/concurrency.md) | Virtual threads, structured concurrency |
| [Error Handling](docs/lang/error-handling.md) | Errors as values, or handlers |
| [Design: Java Transpilation](docs/design-zinc-v3-java.md) | Architecture and philosophy |

---

## Examples

See [`examples/v3/`](examples/v3/) for working examples with expected output:

| Example | Demonstrates |
|---------|-------------|
| [hello.zn](examples/v3/hello.zn) | Functions, string interpolation |
| [classes.zn](examples/v3/classes.zn) | Classes, data records, inheritance, visibility |
| [sealed.zn](examples/v3/sealed.zn) | Sealed classes, pattern matching |
| [concurrency.zn](examples/v3/concurrency.zn) | spawn, concurrent, parallel for, channels |
| [error_handling.zn](examples/v3/error_handling.zn) | or handlers, return Error |
| [streams.zn](examples/v3/streams.zn) | Collection methods, it keyword, stream chains |
| [functions.zn](examples/v3/functions.zn) | Default params, varargs, lambdas |

All examples verified by automated tests: `cd compiler && make test`

---

## License

[Apache License 2.0](LICENSE)
