# Zinc Feature Roadmap

Convention over configuration. Less typing, less ceremony.

---

## Priority Order — Expressiveness

### P1 — Trailing Lambdas + `it` Keyword ✦ NEXT
The single biggest readability win. Every LINQ chain gets cleaner.

```zinc
// TODAY: noisy, type-heavy
var names = users.Where((User u) -> u.age > 28)
                 .Select((User u) -> u.name)
                 .OrderBy((String s) -> s)

// AFTER: clean, Kotlin-style
var names = users.Where { it.age > 28 }
                 .Select { it.name }
                 .OrderBy { it }
```

- Single-param lambdas auto-bind `it` (like Kotlin)
- Trailing lambda: last arg is a block `{ }` outside parens
- Multi-param still uses arrow: `.Aggregate(0) { acc, x -> acc + x }`
- Type inference from collection element type
- **Effort:** Medium (parser + typechecker + codegen)

### P2 — Data Classes
Eliminate constructor boilerplate.

```zinc
data User(pub String name, pub Int age)

// Equivalent to:
User {
    pub String name
    pub Int age
    new(String name, Int age) { this.name = name; this.age = age }
}
```

- Maps to C# `record` or class with auto-constructor
- Auto-generates ToString
- Can add methods: `data User(...) { pub String greet() { ... } }`
- **Effort:** Medium

### P3 — Implicit Return + Expression If/Match
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

### P4 — Ranges
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

### P5 — Scripting Builtins
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

### P6 — `zinc add` / Dependency Management
```bash
zinc add Newtonsoft.Json
zinc add Serilog --version 4.0.0
zinc remove Newtonsoft.Json
```
- **Effort:** Medium

### P7 — VS Code Extension
TextMate grammar for `.zn` syntax highlighting.
- **Effort:** Quick

### P8 — `zinc test`
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

**Zinc today (15 lines):**
```zinc
User {
    pub String name
    pub Int age
    new(String name, Int age) { this.name = name; this.age = age }
}
main() {
    var users = [User("Alice", 30), User("Bob", 25), User("Carol", 35)]
    var seniors = users.Where((User u) -> u.age > 28)
                       .Select((User u) -> u.name)
                       .OrderBy((String s) -> s)
    var config = readFile("config.json") or { print(err); exit(1) }
    for name in seniors { print(name) }
}
```

**Zinc after P1-P3 (9 lines):**
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

**On par with Kotlin. Compiles to a 1.6 MB native binary.**

---

## Interop Roadmap

All unblocked (imports + type resolver + annotations).

| Use Case | Status |
|----------|--------|
| Logging, HTTP, Config, JSON, DI | ✅ Ready |
| REST API, ORM, Serialization | ✅ Ready |
| Testing (xUnit) | ⚠ Needs `zinc test` (P8) |

---

## Revisit Later

| Feature | Notes |
|---------|-------|
| Typed errors | Exception hierarchy |
| Structured concurrency | `await { }` blocks |
| Operator overloading | Via .NET interop |
| Destructuring | `var (name, age) = user` |

---

## Completed (v0.8.0)
- Generic annotations, doc restructure, dead code cleanup

## Completed (v0.7.0)
- NuGet imports, CSharpTypeResolver, auto `new`, AOT fixes

## Completed (v0.6.0)
- 28 builtins, failable infrastructure, `or { }` error handling

## Completed (v0.5.0 and earlier)
- C# AOT backend, LINQ, `zinc.toml`, TypeRegistry, OO polymorphism
- Go backend, pointer inference, REPL, CI
