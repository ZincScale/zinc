<img alt="Zinc mascot" src="./zinc-badger.svg" height="180" /> <img alt="Zinc logo" src="./zinc-wordmark.svg" width="480" />


# Zinc

**Zinc** is a convention-over-configuration JVM language. Write `.zn` files, get type safety, clean syntax, and the full Java ecosystem. The transpiler generates clean `.java` files, compiles with javac, and runs on Java 25 virtual threads.

```zinc
fn greet(String name) String {
    return "Hello, {name}!"
}

var name = "world"
print(greet(name))
```

```bash
$ zinc run hello.zn
Hello, world!
```

---

## Why Zinc?

Java is the best runtime for server applications. But it has ceremony: semicolons, `public static void main`, stream().collect(toList()), verbose generics. Zinc removes the ceremony while keeping Java's ecosystem.

- **No semicolons** — newline terminates statements
- **No `public static void main`** — script mode, top-level statements
- **Enforced types** — Java-native types, compile-time checked by javac
- **Brace blocks `{ }`** — familiar C-family syntax
- **`data` → records** — `data User(String name, int age)` with auto equals/hashCode/toString
- **Fluent collections** — `.filter().map().sortBy()` without `.stream()/.toList()`
- **`it` keyword** — `items.filter(it > 0)` instead of `items.filter(x -> x > 0)`
- **Two-track errors** — `Result<T>` + `or {}` for expected failures, exceptions for unexpected
- **Virtual threads** — `spawn`, `parallel for`, `concurrent` for structured concurrency
- **Kotlin-style equality** — `==` structural, `===` reference identity
- **Visibility model** — fields private by default, `pub` generates getters/setters
- **Deploy anywhere** — Quarkus + GraalVM native-image, JLink, Docker

---

## Documentation

| Document | Description |
|----------|-------------|
| [Language Reference](docs/language-reference.md) | Full syntax guide |
| [Design Doc](docs/design-zinc-v3-java.md) | v3 philosophy, Java transpilation, packaging |
| [Concurrency](docs/design-zinc-concurrency.md) | Virtual threads, structured concurrency |
| [Zinc Flow](docs/design-zinc-flow.md) | NiFi-inspired flow processing |

---

## Install

```bash
go install github.com/victorybhg/zinc/cmd/zinc@latest
```

Requires: Go 1.21+ (for building Zinc) and JDK 25+ (for compiling/running output)

---

## Quick Start

```bash
# Write a script
cat <<'ZN' > hello.zn
print("Hello from Zinc!")
ZN

# Run it
zinc run hello.zn

# Build (transpile to Java + compile)
zinc build hello.zn
```

No project setup, no config, no boilerplate. Single files just run.

---

## Feature Highlights

```zinc
// Data classes → Java records
data User(String name, String email, int age = 0)

// Classes with visibility + auto-this
class Stack {
    var List<int> items = []

    pub fn push(int item) {
        items.add(item)              // auto-injects this.items
    }

    pub fn size() int {
        return items.size()
    }
}

// Two-track error handling
fn parsePort(String s) Result<int> {
    if not s.isDigit() {
        return Error("not a number")
    }
    return int(s)
}
var port = parsePort("8080") or 80

// Fluent collections with it keyword
var active = orders.filter(it.status == "active")
var total = active.map(it.amount).sum()

// Expression if
var String label = if count == 1: "item" else: "items"

// Virtual threads — real parallelism
parallel for item in items {
    process(item)
}

var (user, orders) = concurrent {
    fetchUser(id)
    fetchOrders(id)
}

// Match expressions with destructuring
var double area = match shape {
    case Circle(r) -> Math.PI * r ** 2
    case Rect(w, h) -> w * h
}
```

---

## Examples

See [`examples/v3/`](examples/v3/) for working examples:

- [`hello.zn`](examples/v3/hello.zn) — hello world with functions
- [`classes.zn`](examples/v3/classes.zn) — classes, inheritance, data records
- [`collections.zn`](examples/v3/collections.zn) — filter, map, it keyword
- [`error_handling.zn`](examples/v3/error_handling.zn) — Result<T>, Error, or, try/catch

---

## License

[Apache License 2.0](LICENSE)
