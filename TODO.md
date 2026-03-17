# Zinc Feature Roadmap

Convention over configuration. Less typing, less ceremony.

---

## Priority Order — Expressiveness

### ~~P1 — Implicit Return + Expression If/Match~~ ✅ Done
Last expression in a block is the return value. If and match become expressions.

```zinc
// Implicit return — no `return` keyword needed
Int double(Int x) { x * 2 }
String greet(String name) { "Hello, {name}!" }

// Expression if — returns a value
var label = if x > 0 { "positive" } else { "negative" }

// Expression match — returns a value
var msg = match status {
    case Status.Active -> "running"
    case Status.Closed -> "done"
    case _ -> "unknown"
}
```

- Last expression in function/lambda body is implicit return
- `if` and `match` can be used in expression position
- Explicit `return` still works for early returns
- **Effort:** Medium (parser + codegen)

### ~~P2 — Ranges~~ ✅ Done
Replace C-style for loops.

```zinc
// TODAY:
for (var i = 0; i < 10; i += 1) { print(i) }

// AFTER:
for i in 0..10 { print(i) }        // 0 to 9 (exclusive end)
for i in 0..=10 { print(i) }       // 0 to 10 (inclusive)
for i in 10..0 { print(i) }        // countdown

var firstFive = nums[0..5]          // slice syntax already exists
```

- `..` exclusive end (like Kotlin `until`, Rust `..`)
- `..=` inclusive end (like Rust `..=`)
- Works in for loops and slice expressions
- **Effort:** Quick (lexer + parser + codegen)

### P3 — Scripting Builtins
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

### P4 — `zinc add` / Dependency Management
```bash
zinc add Newtonsoft.Json
zinc add Serilog --version 4.0.0
zinc remove Newtonsoft.Json
```
- **Effort:** Medium

### P5 — VS Code Extension
TextMate grammar for `.zn` syntax highlighting.
- **Effort:** Quick

### P6 — `zinc test`
`zinc test` → `dotnet test` or `go test`.
- **Effort:** Quick

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

**Zinc today (10 lines):**
```zinc
data User(pub String name, pub Int age)

main() {
    var users = [User("Alice", 30), User("Bob", 25), User("Carol", 35)]
    var seniors = users.Where { it.age > 28 }
                       .Select { it.name }
                       .OrderBy { it }
    var config = readFile("config.json") or { print(err); exit(1) }
    for name in seniors { print(name) }
}
```

**Zinc after P1 (9 lines) — implicit return:**
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

All unblocked (imports + type resolver + annotations).

| Use Case | Status |
|----------|--------|
| Logging, HTTP, Config, JSON, DI | ✅ Ready |
| REST API, ORM, Serialization | ✅ Ready |
| Testing (xUnit) | ⚠ Needs `zinc test` (P6) |

---

## Revisit Later

| Feature | Notes |
|---------|-------|
| Typed errors | Exception hierarchy |
| Structured concurrency | `await { }` blocks |
| Operator overloading | Via .NET interop |
| Destructuring | `var (name, age) = user` |

---

## Completed (v0.10.0)
- Implicit return — last expression in function/method body is the return value
- Expression if — `var x = if cond { a } else { b }` (emits C# ternary)
- Expression match — `var x = match val { case 1 -> "a" case _ -> "b" }` (emits C# switch expression)
- Range loops — `for i in 0..10` (exclusive) and `for i in 0..=10` (inclusive)
- `--release` flag — strips debug symbols for production builds
- Embedded debug info by default — runtime errors show `.zn` line numbers

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
