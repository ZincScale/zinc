# Zinc Feature Roadmap

Convention over configuration. Less typing, less ceremony.

---

## Priority Order

### P1 — Concurrency: `spawn`, `parallel`, `Lock<T>` ✦ NEXT
Three primitives for concurrent programming. No async/await, no function coloring.

```zinc
// spawn — run work concurrently, get a Future<T>
var user = spawn { fetchUser(1) }
var posts = spawn { fetchPosts(1) }
print(user.value)
print(posts.value)

// parallel — fan-out over a collection
var profiles = parallel(userIds) { fetchProfile(it) }

// Lock<T> — safe shared state
var count = Lock(0)
parallel(0..100) { count.update { value = value + 1 } }
print(count.value)
```

- `spawn { expr }` → `Future<T>`, `.value` to collect
- `parallel(list) { expr }` → spawn per item, collect in order
- `Lock<T>` → thread-safe wrapper with `.value` and `.update { }`
- Structured scoping — parent waits for children, errors cancel siblings
- See [design doc](docs/design-concurrency.md)
- **Effort:** Medium (AST + parser + codegen + runtime wrapper)

### P2 — Scripting Builtins
Make CLI tools trivial.

```zinc
main() {
    if args.Count() < 2 { print("usage: tool <file>"); exit(1) }
    var content = readFile(args[1]) or { print(err); exit(1) }
    print(content)
}
```

- `args` — built-in `List<String>` for command-line args
- `exec(cmd)` — run shell command, failable
- `fileExists(path)` → `Bool`
- `listDir(path)` → `List<String>`, failable
- **Effort:** Quick

### P3 — VS Code Extension
TextMate grammar for `.zn` syntax highlighting.
- **Effort:** Quick

### P4 — `zinc add` / Dependency Management
```bash
zinc add Newtonsoft.Json
zinc add Serilog --version 4.0.0
zinc remove Newtonsoft.Json
```
- **Effort:** Medium

### P5 — `zinc test`
```bash
zinc test       # discovers and runs test functions
```
- **Effort:** Medium

---

## Expressiveness Comparison

Same program across languages:

**Python (8 lines):**
```python
users = [User("Alice", 30), User("Bob", 25), User("Carol", 35)]
seniors = [u.name for u in users if u.age > 28]
config = json.load(open("config.json"))
for name in sorted(seniors):
    print(name)
```

**Kotlin (8 lines):**
```kotlin
data class User(val name: String, val age: Int)
val users = listOf(User("Alice", 30), User("Bob", 25), User("Carol", 35))
val seniors = users.filter { it.age > 28 }.map { it.name }.sorted()
val config = File("config.json").readText()
seniors.forEach { println(it) }
```

**Zinc (9 lines):**
```zinc
data User(pub String name, pub Int age)

main() {
    var users = [User("Alice", 30), User("Bob", 25), User("Carol", 35)]
    var seniors = users.Where { it.age > 28 }
                       .Select { it.name }
                       .OrderBy { it }
    var config = readFile("config.json") or { print(err); exit(1) }
    seniors.ForEach { print(it) }
}
```

**On par with Kotlin. Compiles to a 1.6 MB native binary.**

---

## Interop Roadmap

| Use Case | Status |
|----------|--------|
| Logging, HTTP, Config, JSON, DI | Ready |
| REST API, ORM, Serialization | Ready |
| Testing (xUnit) | Needs `zinc test` (P5) |

---

## Revisit Later

| Feature | Notes |
|---------|-------|
| Typed errors | Exception hierarchy |
| Operator overloading | Via .NET interop |
| Destructuring | `var (name, age) = user` |
| Channels | Only if in-process producer/consumer demand emerges |
| Supervised blocks | Only if in-process resilience demand emerges (K8s handles restarts) |

---

## Completed (v0.10.0)
- Implicit return — last expression in function/method body is the return value
- Expression if — `var x = if cond { a } else { b }` (emits C# ternary)
- Expression match — `var x = match val { case 1 -> "a" case _ -> "b" }` (emits C# switch expression)
- Range loops — `for i in 0..10` (exclusive) and `for i in 0..=10` (inclusive)
- `--release` flag — strips debug symbols for production builds
- Embedded debug info by default — runtime errors show `.zn` line numbers
- Removed labeled loops, Go-only features (channels, goroutines, tuple destructuring, defer)
- Cleaned up 22 stale .go files, 4 Go-only examples

## Completed (v0.9.0)
- Trailing lambdas + `it` keyword (Kotlin-style `{ it > 3 }`)
- Data classes (`data User(pub String name, pub Int age)` → C# `record`)
- Batched E2E test runner (43 tests in ~9s, single dotnet build)

## Completed (v0.8.0)
- Generic annotations, doc restructure, dead code cleanup

## Completed (v0.7.0)
- NuGet imports, CSharpTypeResolver, auto `new`, AOT fixes

## Completed (v0.6.0)
- 28 builtins, failable infrastructure, `or { }` error handling

## Completed (v0.5.0 and earlier)
- C# AOT backend, LINQ, `zinc.toml`, TypeRegistry, OO polymorphism
- Go backend, pointer inference, REPL, CI
