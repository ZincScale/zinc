# Getting Started with Zinc

Zinc is a convention-over-configuration JVM language. Write `.zn` files, run them with `zinc run`. The transpiler catches type errors, generates clean Java, and stays out of your way.

## Install

```bash
curl -sSL https://raw.githubusercontent.com/victorybhg/zinc/master/install.sh | sh
```

Installs Zinc + GraalVM JDK 25, Mill, and Quarkus CLI.

## Hello World

```zinc
// hello.zn
print("Hello, world!")
```

```bash
zinc run hello.zn
# Hello, world!
```

That's it. No `public static void main`, no project setup. Top-level code just runs.

## Your First Script

```zinc
// greet.zn
fn greet(String name) String {
    return "Hello, {name}!"
}

var name = if args.length > 0 { args[0] } else { "world" }
print(greet(name))
```

```bash
zinc run greet.zn -- Alice
# Hello, Alice!
```

## Key Differences from Java

| Java | Zinc | Why |
|---|---|---|
| `public static void main(String[] args)` | Top-level code | No ceremony |
| `String greet(String name) { }` | `fn greet(String name) String` | Return type after params |
| `var x = new ArrayList<Integer>()` | `var x = List<int>()` | No `new`, no boxing |
| `record User(String name, int age) {}` | `data User { String name, int age }` | Cleaner syntax |
| `list.stream().filter(...).toList()` | `list.filter(...)` | Auto stream chains |
| `"Hello, " + name + "!"` | `"Hello, {name}!"` | String interpolation |
| `x == null ? null : x.foo()` | `x?.foo()` | Safe navigation |
| Semicolons everywhere | No semicolons | Less noise |

## Variables

```zinc
var name = "Alice"          // type inferred
var int age = 30            // explicit type
var List<int> scores = []   // generic type
```

## Functions

```zinc
fn add(int a, int b) int {
    return a + b
}

// Single-expression shorthand
fn double(int x) int = x * 2
```

## Control Flow

```zinc
if x > 0 {
    print("positive")
} else if x == 0 {
    print("zero")
} else {
    print("negative")
}

for item in items {
    print(item)
}

while running {
    process()
}
```

## Data Classes

```zinc
data User(String name, String email, int age = 0)

var u = User("Alice", "alice@example.com")
```

## Error Handling

Zinc uses errors as values — no try/catch/throw.

```zinc
// Expected errors use Result<T>
fn parse_port(String s) Result<int> {
    var n = Integer.parseInt(s) or { return Error("not a number") }
    return n
}

var port = parse_port("8080") or 80

// Pattern match on errors
var result = riskyCall() or match err {
    case TimeoutError -> defaultValue
    case NotFoundError -> fallback
}
```

## Collections

```zinc
var items = [1, 2, 3, 4, 5]

// Stream chains — no .stream() or .toList() needed
var evens = items.filter(it > 0).map(it * 2)
var total = items.filter(it > 10).sum()

// it keyword — implicit lambda parameter
items.sortBy(it.age)
items.forEach(print(it))
```

## Match

```zinc
match status {
    case 1 -> "running"
    case 2 -> "stopped"
    case _ -> "unknown"
}
```

## Concurrency

```zinc
// Virtual threads
spawn {
    expensive_computation()
}

// Parallel iteration
parallel for item in items {
    process(item)
}
```

## Type Safety

Type errors are caught automatically during transpilation:

```bash
$ zinc run broken.zn
error: type errors in broken.zn:
  line 2: return type mismatch: expected int, got String
  argument 1 of "greet": expected String, got int
```

No separate `check` command — checking IS transpilation.

## CLI

```bash
zinc init myapp                       # scaffold a new project
zinc run src/main.zn                  # transpile + compile + run (Mill if project)
zinc run script.zn -- arg1            # pass args to script
zinc build src/                       # transpile + compile (Mill if project)
zinc build --native src/              # GraalVM native binary via Mill
zinc build --docker src/              # generate Dockerfile + build
zinc build --k8s src/                 # Docker + K8s manifest
zinc fmt script.zn                    # format source code
zinc repl                             # interactive REPL
zinc update                           # update toolchain (GraalVM, Mill, Quarkus)
```

## Next Steps

- [Language Reference](language-reference.md) — full syntax guide
- [Build Guide](guide-mill-build.md) — dependencies, deployment, Docker, K8s, native-image, CI/CD
- [Design Doc](design-zinc-v3-java.md) — philosophy and decisions
