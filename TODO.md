# Zinc Feature Roadmap

Convention over configuration for native apps. Less typing, less ceremony.

---

## Priority Order — Expressiveness First

### P1 — Data Classes ✦ NEXT
Eliminate constructor boilerplate — the #1 LOC waster in Zinc today.

```zinc
// Before: 8 lines
User {
    pub String name
    pub Int age
    new(String name, Int age) {
        this.name = name
        this.age = age
    }
}

// After: 1 line
data User(pub String name, pub Int age)
```

- Maps to C# `record` or class with auto-generated constructor
- Auto-generates ToString
- Fields declared inline with the class name
- Still supports methods if needed: `data User(...) { pub String greet() { ... } }`
- **Effort:** Medium

### P2 — Lambda Type Inference
Remove redundant type annotations from lambdas — the type is inferrable from context.

```zinc
// Before:
var big = nums.Where((Int x) -> x > 3)
var doubled = nums.Select((Int x) -> x * 2)

// After:
var big = nums.Where(x -> x > 3)
var doubled = nums.Select(x -> x * 2)
```

- Infer param types from the collection element type
- Multi-param: `(a, b) -> a + b` instead of `(Int a, Int b) -> a + b`
- Still allow explicit types when needed for clarity
- **Effort:** Medium (parser + typechecker changes)

### P3 — `or die` / `or exit` Shorthands
The `or { print(err); exit(1) }` pattern is ubiquitous. Make it a one-liner.

```zinc
// Before: 3 lines
var content = readFile("data.txt") or {
    print("Error: {err}")
    exit(1)
}

// After: 1 line
var content = readFile("data.txt") or die       // prints err + exits
var content = readFile("data.txt") or exit(1)   // exits with code
var content = readFile("data.txt") or ""        // default value
```

- `or die` → print error message to stderr + exit(1)
- `or exit(N)` → exit with specific code
- `or <default>` → use default value on error
- **Effort:** Quick (parser + codegen)

### P4 — Auto-Assign Constructor Params
When constructor params match field names, auto-assign them.

```zinc
// Before:
Dog {
    pub String name
    pub Int age
    new(String name, Int age) {
        this.name = name
        this.age = age
    }
}

// After:
Dog {
    pub String name
    pub Int age
    new(String name, Int age) {}  // auto-assigns matching fields
}
```

- If constructor body is empty or doesn't assign a matching field, auto-assign it
- Explicit assignments override auto-assign
- **Effort:** Quick (codegen-only, no parser changes)

### P5 — `args` and Scripting Builtins
Make CLI tools trivial:

```zinc
main() {
    if args.Count() < 2 { print("usage: tool <file>"); exit(1) }
    var content = readFile(args[1]) or die
    print(content)
}
```

- `args` — built-in `List<String>`, maps to command-line args
- `exec(cmd)` — run shell command, return output, failable
- `fileExists(path)` → `Bool`
- `listDir(path)` → `List<String>`, failable
- `pathJoin(parts...)` → path joining
- **Effort:** Quick

### P6 — `zinc add` / Dependency Management
```bash
zinc add Newtonsoft.Json              # adds to zinc.toml
zinc add Serilog --version 4.0.0     # pinned version
zinc remove Newtonsoft.Json           # removes
```
- **Effort:** Medium

### P7 — VS Code Extension (Syntax Highlighting)
Basic `.zn` editor support — TextMate grammar.
- **Effort:** Quick

### P8 — `zinc test`
`zinc test` → runs `dotnet test` or `go test`.
- **Effort:** Quick

---

## Expressiveness Comparison

What a real program looks like today vs. after P1-P4:

```zinc
// ===== TODAY (21 lines) =====
User {
    pub String name
    pub Int age
    new(String name, Int age) {
        this.name = name
        this.age = age
    }
}

main() {
    var users = [User("Alice", 30), User("Bob", 25)]
    var names = users.Select((User u) -> u.name)
                     .Where((String s) -> s.StartsWith("A"))
    for name in names { print(name) }

    var content = readFile("data.txt") or {
        print("Error: {err}")
        exit(1)
    }
    print(content)
}

// ===== AFTER P1-P4 (11 lines) =====
data User(pub String name, pub Int age)

main() {
    var users = [User("Alice", 30), User("Bob", 25)]
    var names = users.Select(u -> u.name)
                     .Where(s -> s.StartsWith("A"))
    for name in names { print(name) }

    var content = readFile("data.txt") or die
    print(content)
}
```

**48% fewer lines for the same logic.**

---

## Interop Roadmap

All unblocked by v0.8.0 (imports + type resolver + annotations).

| Use Case | Status |
|----------|--------|
| Logging (Serilog/NLog) | ✅ Ready |
| HTTP client | ✅ Ready |
| Configuration | ✅ Ready |
| JSON serialization | ✅ Ready |
| Dependency injection | ✅ Ready |
| REST API (ASP.NET) | ✅ Ready |
| Database/ORM (EF Core) | ✅ Ready |
| Testing (xUnit) | ⚠ Needs `zinc test` (P8) |

---

## Revisit Later

| Feature | Notes |
|---------|-------|
| Typed errors | Exception hierarchy, `catch UserError` |
| Structured concurrency | `await { }` blocks |
| Operator overloading | Via .NET interop |
| Enhanced destructuring | `var (a, b, c) = ...` |

---

## Project Infrastructure

| Feature | Status |
|---------|--------|
| CI: .NET SDK in tests | ✅ Done |
| CONTRIBUTING.md | TODO |
| Code coverage | TODO |

---

## Future — IDE & Ecosystem

| Feature | Effort |
|---------|--------|
| LSP server | Large |
| VS Code extension + LSP | Medium |
| Web playground | Large |
| `zinc doc` | Medium |

---

## Completed (v0.8.0)
- Generic annotations: `@Name("args")` → `[Name("args")]` in C#
- Split docs into 8 focused topic files
- Removed dead code from Generator (6 fields, 1 method)

## Completed (v0.7.0)
- NuGet import → `using` mapping (16 aliases)
- CSharpTypeResolver: .NET reflection probe (3,700+ types)
- Auto `new` for constructors, static class detection
- Single Functions class, unique catch vars, AOT trim fixes

## Completed (v0.6.0)
- 28 global builtins in C# backend
- Failable infrastructure + `or { }` error handling

## Completed (v0.5.0)
- C# AOT backend, LINQ (22 methods), `zinc.toml`, build pipeline
- TypeRegistry, source maps, lambda `->`, `var`, type inference

## Completed (v0.4.0 and earlier)
- Type-before-name, OO polymorphism, generics, error handling
- Go backend (3,326 lines), pointer inference, REPL, CI
