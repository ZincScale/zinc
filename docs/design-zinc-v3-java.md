# Design: Zinc v3 — Convention-Over-Configuration JVM Language

> **Status**: DESIGN — transpilation mappings validated, runtime benchmarked
> **Target**: Java 25 + Quarkus/GraalVM native-image
> **Build**: Mill (YAML config)

## What Groovy Should Have Been

Groovy tried to be "Java without the ceremony." It failed because it was too dynamic (slow, no AOT), its type system was optional (runtime surprises), and it became "the Gradle DSL language" instead of a language people build systems in.

Kotlin succeeded where Groovy failed — static types, null safety, JetBrains tooling. But Kotlin is its own language with its own idioms. Java developers have to learn a new world.

**Zinc is different.** It transpiles to clean, readable Java 25. The output is standard Java you can read, debug, and deploy. No new runtime, no custom bytecode, no magic. The transpiler removes the ceremony and adds the safety that Java lacks.

### Core Principles

1. **Zero ceremony** — write a `.zn` file, run it. No `public static void main`, no semicolons, no project setup for single files.
2. **It's just Java underneath** — full Maven Central ecosystem, all Java libraries work. The output is readable `.java` that any Java developer can maintain.
3. **The transpiler works for you** — auto-inject `this`, generate stream chains from fluent collections, expand `it` lambdas, wrap string interpolation. You write the intent, Zinc handles the mechanics.
4. **Prevent footguns** — enforced types, exhaustive match, null safety, unused variable warnings. The compiler catches what Java lets slip to runtime.
5. **Convention over configuration** — opinionated project structure, Mill for builds, Quarkus for deployment. One way to do things.
6. **Explicit blocks** — braces `{ }`. No whitespace sensitivity. Familiar to every developer.

### What the transpiler does for you

| You write | Zinc generates |
|---|---|
| `data User { String name, int age }` | `record User(String name, int age) {}` |
| `items.filter(x -> x > 0)` | `items.stream().filter(x -> x > 0).toList()` |
| `"Hello, {name}!"` | `"Hello, " + name + "!"` |
| `match shape { case Circle c -> ... }` | `switch (shape) { case Circle c -> ... }` |
| `spawn { task() }` | `Thread.startVirtualThread(() -> { task(); })` |
| `fn String() String` | `@Override public String toString()` |
| `var x = 5` | `var x = 5;` |
| `items.map(it * 2)` | `items.stream().map(x -> x * 2).toList()` |

### Non-goals

- Not a new runtime — we generate Java and compile with javac/GraalVM.
- Not a framework — no DI, no services layer beyond what Quarkus provides. Use Quarkus/Micronaut directly.
- Not a language for language nerds — no monads, no macros, no metaprogramming. Just clean, fast, safe code.

---

## Syntax

### Block Delimiters — Braces `{ }`

```zinc
if x > 10 {
    print("big")
} else {
    print("small")
}

fn process(List<Order> items) List<Order> {
    var result = items.filter(x -> x.status == "active")
    if result.count() > 100 {
        result = result.sort_by(x -> x.priority).take(100)
    }
    return result
}
```

### Multi-line Statements

Automatic continuation when line ends with:
1. `.` — method chaining
2. Binary operator (`+`, `-`, `*`, `and`, `or`, `==`, `>`, etc.)
3. `,` — function args, collections
4. Unmatched `(`, `[`, or `{`
5. `\` — explicit continuation (escape hatch)

```zinc
var result = orders
    .filter(x -> x.status == "active" and
                 x.amount > 1000)
    .sort_by(x -> x.created_at)
    .take(50)
```

### Variables

```zinc
var name = "Alice"          // type inferred as String
var int age = 30             // explicit type
var List<int> scores = []    // explicit generic
const double PI = 3.14159   // immutable
```

Transpiles to:
```java
var name = "Alice";
int age = 30;
List<Integer> scores = new ArrayList<>();
final double PI = 3.14159;
```

### Functions

```zinc
fn greet(String name) String {
    return "Hello, {name}!"
}

// Single-expression shorthand
fn double(int x) int = x * 2

// No return type = void
fn log(String msg) {
    System.out.println(msg)
}
```

Transpiles to:
```java
static String greet(String name) {
    return "Hello, " + name + "!";
}

static int doubleVal(int x) { return x * 2; }

static void log(String msg) {
    System.out.println(msg);
}
```

### Script Mode (No Main Required)

```zinc
// script.zn — just write code
var name = input("What's your name? ")
print("Hello, {name}!")
```

Transpiles to:
```java
public class Script {
    public static void main(String[] args) {
        var name = System.console().readLine("What's your name? ");
        System.out.println("Hello, " + name + "!");
    }
}
```

Top-level statements are wrapped in a generated `main()`. Class name derived from filename.

### Named Arguments

```zinc
fn connect(String host, int port = 5432, boolean ssl = true) Connection {
    // ...
}

// Call with named args (any order)
var conn = connect(host: "db.example.com", ssl: false)
```

Transpiler reorders to positional and fills defaults:
```java
var conn = connect("db.example.com", 5432, false);
```

For `data` types with many fields, transpiler generates a builder:
```zinc
var user = User(name: "Alice", age: 30, role: "admin")
```

---

## Types

### Primitive Types

| Zinc | Java | Notes |
|---|---|---|
| `int` | `int` / `Integer` | Primitive when possible, boxed in generics |
| `double` | `double` / `Double` | 64-bit (Java `double`). No 32-bit `float` in Zinc. |
| `String` | `String` | |
| `boolean` | `boolean` / `Boolean` | |
| `byte[]` | `byte[]` | Mutable in Java, Zinc tracks this |

### Collection Types

| Zinc | Java | Literal |
|---|---|---|
| `List<T>` | `List<T>` | `[1, 2, 3]` |
| `Map<K, V>` | `Map<K, V>` | `{"a": 1, "b": 2}` |
| `Set<T>` | `Set<T>` | `set(1, 2, 3)` |
| `(T, U)` | Generated record | `(1, "hi")` |

### Nullable Types

```zinc
var String? name = null             // nullable
var int age = 42                 // non-nullable, compiler enforced

fn find(int id) User? {          // may return null
    // ...
}

var user = find(42)
if user != null {
    print(user.name)           // safe — compiler knows it's non-null here
}

// Safe navigation
var email = user?.email?.toLower()
```

Transpiles to null checks in Java. The compiler tracks nullability — no `Optional<T>` wrapping for locals, just null-safe code generation.

### Generics

```zinc
class Stack<T> {
    var List<T> items = []

    fn push(T item) {
        items.add(item)
    }

    fn pop() T? {
        if items.count() == 0 { return none }
        return items.removeLast()
    }
}
```

Transpiles directly to Java generics.

---

## Data Classes → Records

```zinc
data User {
    String name
    int age
    String role = "user"
}
```

Transpiles to:
```java
public record User(String name, int age, String role) {
    public User(String name, int age) {
        this(name, age, "user");
    }
}
```

Auto-generates: constructor, `equals()`, `hashCode()`, `toString()`. Zinc's `data` is Java's `record` with less ceremony.

### Data with Methods

```zinc
data Point {
    double x
    double y

    fn distance(Point other) double {
        return Math.sqrt((x - other.x) ** 2 + (y - other.y) ** 2)
    }
}
```

### Sealed Types (Exhaustive Match)

```zinc
sealed class Shape {
    data Circle { double radius }
    data Rect { double width, double height }
    data Triangle { double base, double height }
}

fn area(Shape shape) double {
    return match shape {
        case Circle c -> Math.PI * c.radius ** 2
        case Rect r -> r.width * r.height
        case Triangle t -> 0.5 * t.base * t.height
        // No default needed — compiler knows all cases covered
    }
}
```

Transpiles to:
```java
public sealed interface Shape permits Circle, Rect, Triangle {}
public record Circle(double radius) implements Shape {}
public record Rect(double width, double height) implements Shape {}
public record Triangle(double base, double height) implements Shape {}

static double area(Shape shape) {
    return switch (shape) {
        case Circle c -> Math.PI * c.radius() * c.radius();
        case Rect r -> r.width() * r.height();
        case Triangle t -> 0.5 * t.base() * t.height();
    };
}
```

---

## Enums

```zinc
enum Color { Red, Green, Blue }

enum HttpStatus {
    Ok = 200
    NotFound = 404
    ServerError = 500

    fn isError() boolean = this.value >= 400
}
```

---

## Classes

```zinc
class Dog {
    var String name
    var String breed
    pub var int age = 0

    fn init(String name, String breed) {
        this.name = name
        this.breed = breed
    }

    fn speak() String = "{name} says woof!"

    fn String() String = "Dog({name}, {breed}, age={age})"
}

class Puppy : Dog {
    fn init(String name) {
        super(name, "Mixed")
    }

    fn speak() String = "{name} says yap!"
}
```

Transpiles to:
```java
public class Dog {
    private String name;
    private String breed;
    public int age = 0;

    public Dog(String name, String breed) {
        this.name = name;
        this.breed = breed;
    }

    public String speak() { return name + " says woof!"; }

    @Override public String toString() {
        return "Dog(" + name + ", " + breed + ", age=" + age + ")";
    }
}
```

### Interfaces

```zinc
interface Speakable {
    fn speak() String
}

interface Serializable {
    fn toBytes() byte[]
    fn fromBytes(byte[] data) this   // static factory
}

class Dog : Speakable {
    fn speak() String = "Woof!"
}
```

---

## Collections — Fluent Without Stream Ceremony

The Zinc value proposition for collections: you write fluent chains, the transpiler adds `.stream()` and `.toList()`.

```zinc
var active = users
    .filter(u -> u.isActive)
    .sort_by(u -> u.lastName)
    .take(10)

var names = users.map(u -> u.name)

var byRole = users.group_by(u -> u.role)

var total = orders.map(o -> o.amount).sum()

var hasAdmin = users.any(u -> u.role == "admin")
```

Transpiles to:
```java
var active = users.stream()
    .filter(u -> u.isActive())
    .sorted(Comparator.comparing(u -> u.lastName()))
    .limit(10)
    .toList();

var names = users.stream().map(u -> u.name()).toList();

var byRole = users.stream().collect(Collectors.groupingBy(u -> u.role()));

var total = orders.stream().mapToDouble(o -> o.amount()).sum();

var hasAdmin = users.stream().anyMatch(u -> u.role().equals("admin"));
```

### The `it` Keyword (Kotlin-style)

Single-parameter lambdas can use `it` instead of naming the parameter:

```zinc
var names = users.map(it.name)
var active = users.filter(it.isActive)
var doubled = numbers.map(it * 2)
```

### Comprehensions (Alternative Syntax)

```zinc
var squares = [x ** 2 for x in range(10)]
var active = [u for u in users if u.isActive]
var lookup = {u.id: u for u in users}
```

Transpiles to stream chains. Comprehensions are syntax sugar for `.filter().map()`.

### Collection Method Reference

| Zinc | Java Stream | Terminal? |
|---|---|---|
| `.filter(pred)` | `.filter(pred)` | No |
| `.map(fn)` | `.map(fn)` | No |
| `.flat_map(fn)` | `.flatMap(fn)` | No |
| `.sort_by(fn)` | `.sorted(Comparator.comparing(fn))` | No |
| `.distinct()` | `.distinct()` | No |
| `.take(n)` | `.limit(n)` | No |
| `.skip(n)` | `.skip(n)` | No |
| `.first()` | `.findFirst().orElse(null)` | Yes |
| `.first(pred)` | `.filter(pred).findFirst().orElse(null)` | Yes |
| `.any(pred)` | `.anyMatch(pred)` | Yes |
| `.all(pred)` | `.allMatch(pred)` | Yes |
| `.count()` | `.count()` or `.size()` | Yes |
| `.sum()` | `.mapToInt/Double(...).sum()` | Yes |
| `.min()` / `.max()` | `.min/max(Comparator)` | Yes |
| `.group_by(fn)` | `.collect(Collectors.groupingBy(fn))` | Yes |
| `.to_list()` | `.toList()` | Yes |
| `.to_dict(k, v)` | `.collect(Collectors.toMap(k, v))` | Yes |
| `.reduce(init, fn)` | `.reduce(init, fn)` | Yes |
| `.for_each(fn)` | `.forEach(fn)` | Yes |

---

## Error Handling — Errors as Values

All errors are values. No `try`, `catch`, or `throw` in Zinc. The transpiler generates Java exception machinery under the hood. See `docs/design-zinc-errors-as-values.md` for the full design.

Functions declare only the success return type:

```zinc
fn parseInt(String s) int {
    var n = s.toInt() or {
        return Error("Not a number: {s}")
    }
    return n
}

// Fallback value
var port = parseInt(input) or 8080

// Handler block (err is implicit)
var port = parseInt(input) or {
    print("Bad port: {err}")
    8080
}

// Auto-propagate (no handler — error flows to caller)
var port = parseInt(input)

// Typed error matching (rare — API boundaries)
var user = fetchUser(id) or match err {
    case NotFound -> return Response(404, "not found")
    case _ -> return Response(500, "internal error")
}

// Return errors from functions
fn fetchUser(int id) User {
    var resp = http.get("/users/{id}") or {
        return Error("request failed: {err}")
    }
    if resp.status == 404 {
        return Error(NotFound("user not found"))
    }
    return parse(resp.body)
}
```

| Zinc | Java (generated) |
|---|---|
| `return Error("msg")` | `throw new RuntimeException("msg")` |
| `return Error(NotFound("x"))` | `throw new NotFound("x")` |
| `call() or default` | `try { call(); } catch (Exception e) { default; }` |
| `call() or { block }` | `try { call(); } catch (Exception err) { block; }` |
| `call() or match err { ... }` | `try { call(); } catch (TypeA err) { ... } catch (...) { ... }` |
| No handler | Plain call — exceptions propagate naturally |

---

## Concurrency — Virtual Threads

Java 25 virtual threads are the concurrency model. No callbacks, no async/await, no colored functions.

### Spawn

```zinc
spawn {
    processOrder(order)
}

// With result
var future = spawn {
    fetchUser(id)
}
var user = future.get()
```

Transpiles to:
```java
Thread.startVirtualThread(() -> {
    processOrder(order);
});

var future = new CompletableFuture<User>();
Thread.startVirtualThread(() -> {
    future.complete(fetchUser(id));
});
var user = future.get();
```

### Parallel For

```zinc
parallel for order in orders {
    process(order)
}
```

Transpiles to `StructuredTaskScope`:
```java
try (var scope = new StructuredTaskScope.ShutdownOnFailure()) {
    for (var order : orders) {
        scope.fork(() -> { process(order); return null; });
    }
    scope.join();
    scope.throwIfFailed();
}
```

### Structured Concurrency (Fan-out / Fan-in)

```zinc
var (user, orders, prefs) = parallel {
    spawn fetchUser(id)
    spawn fetchOrders(id)
    spawn fetchPrefs(id)
}
// All three complete or all cancel — no orphaned threads
```

---

## String Interpolation

```zinc
var name = "Alice"
var greeting = "Hello, {name}!"
var math = "2 + 2 = {2 + 2}"
var nested = "User: {user.name} (age {user.age})"
```

Transpiles to concatenation for simple cases, `String.format()` for complex expressions:
```java
var greeting = "Hello, " + name + "!";
var math = "2 + 2 = " + (2 + 2);
```

---

## Imports

```zinc
import java.util.List
import java.nio.file.Path
from java.util import Map, Set
from java.util.stream import Collectors

// Zinc standard library (bundled)
import zinc.io.readFile
import zinc.json.parse
```

Transpiler hoists all imports to top of generated `.java` file regardless of where they appear in the `.zn` source.

---

## Annotations / Decorators

```zinc
@Deprecated
fn oldMethod() String = "use newMethod instead"

@Override
fn toString() String = "MyClass"

// Quarkus endpoint
@Path("/users")
class UserResource {
    @GET
    fn list() List<User> {
        return userService.findAll()
    }

    @POST
    fn create(User user) User {
        return userService.save(user)
    }
}
```

Annotations map directly to Java annotations. Quarkus, Jakarta EE, and any Java annotation library works unchanged.

---

## Project Structure

```
myapp/
  build.mill.yaml          # Mill build config
  src/
    main.zn                # entry point
    models/
      user.zn
      order.zn
    services/
      user_service.zn
  test/
    user_test.zn
    order_test.zn
```

### build.mill.yaml

```yaml
extends: JavaModule
jvmVersion: 25

mvnDeps:
  - io.quarkus:quarkus-core:3.x
  - io.quarkus:quarkus-rest:3.x

test:
  mvnDeps:
    - io.quarkus:quarkus-junit5:3.x
```

---

## CLI

```bash
zinc <file.zn>              # transpile single file to .java
zinc init [name]            # scaffold project with Mill
zinc build [dir]            # transpile + compile (Mill + javac)
zinc build --native [dir]   # transpile + GraalVM native-image (via Quarkus)
zinc run [dir]              # transpile + run
zinc test [dir]             # transpile + run tests
zinc check <file.zn>        # type check only
zinc fmt <file.zn>          # auto-format
zinc repl                   # interactive REPL (JShell-based)
```

---

## Packaging & Deployment

| Command | Output | Tool |
|---|---|---|
| `zinc build` | `.class` files | javac |
| `zinc build --native` | Native binary (~30MB) | Quarkus + GraalVM native-image |
| `zinc build --jlink` | Self-contained JRE + app | JLink |
| `zinc build --docker` | Docker image | Quarkus container build |
| `zinc build --k8s` | Docker + K8s manifests | Quarkus + kubectl |

**Default is native-image via Quarkus.** If GraalVM native-image fails for a specific dependency (reflection-heavy libraries), fall back to JLink (self-contained JRE, ~50MB, 500ms startup) or standard JVM jar.

### GraalVM Fragility Mitigation

GraalVM native-image requires all reflection to be declared at build time. Quarkus solves this by:
1. Build-time class scanning — no runtime reflection for CDI, REST, serialization
2. GraalVM configuration generated automatically by Quarkus extensions
3. Large library compatibility matrix tested in CI

If a user brings a reflection-heavy library that breaks native-image, `zinc build` falls back to JLink automatically with a warning.

---

## What Zinc Adds Over Java (Why Not Just Write Java?)

| Pain Point | Java | Zinc |
|---|---|---|
| **Boilerplate** | `public static void main(String[] args)` | Top-level statements |
| **Semicolons** | Required everywhere | None |
| **Null safety** | NPE is runtime surprise | `Type?` with compiler tracking |
| **Stream ceremony** | `.stream()...toList()` everywhere | Fluent chains, transpiler adds |
| **String interpolation** | `"Hello " + name` or STR templates (unstable) | `"Hello {name}"` |
| **Data classes** | `record` keyword + permits boilerplate | `data` keyword, minimal |
| **Pattern matching** | `switch` with `case Type t ->` | `match` with exhaustiveness |
| **Concurrency** | `Thread.startVirtualThread(() -> { ... })` | `spawn { ... }` |
| **Named args** | Not supported | Always available |
| **Error handling** | Checked exceptions or raw try/catch | Errors as values: `or {}` / `or match` / `return Error()` |
| **Lambdas** | Must name parameter | `it` keyword for single-param |
| **Build tool** | Gradle/Maven ceremony | Mill YAML, `zinc build` |
| **Project setup** | archetype/initializr + config | `zinc init`, run immediately |

---

## Implementation Phases

### Phase 1 — Core Language Transpiler (Zinc → .java → javac)
- Lexer/parser (reuse existing Go-based frontend or rewrite in Java for self-hosting)
- Type checker with null safety
- Codegen: variables, functions, classes, data/records, enums, interfaces
- Control flow: if/else, for, while, match
- Lambdas + `it` keyword
- String interpolation
- Error handling (errors as values, `or` handlers)
- Imports (hoist to top)
- Script mode (wrap in main)
- CLI: `zinc transpile`, `zinc run`, `zinc check`

### Phase 2 — Collections & Ergonomics
- Collection method → Stream API codegen
- Named arguments
- Comprehensions → stream chains
- Trailing lambdas
- `or {}` / `or match` / `return Error()` — errors as values
- Tuple types → generated records
- Safe navigation `?.`
- `zinc fmt` formatter

### Phase 3 — Concurrency
- `spawn` → virtual threads
- `parallel for` → StructuredTaskScope
- `concurrent { }` → fan-out/fan-in
- `timeout(dur) { }` → deadline-aware execution
- `Channel<T>` → bounded producer/consumer queue
- `lock` → ReentrantLock

### Phase 4 — Packaging & Production
- Mill integration (`zinc init` generates `build.mill.yaml`)
- `zinc build --native` (Quarkus + GraalVM)
- `zinc build --docker` / `zinc build --k8s`
- JLink fallback when native-image fails
- `zinc repl` (JShell-based)

### Phase 5 — Ecosystem
- Standard library: HTTP client, JSON, file I/O wrappers
- Quarkus dev mode integration (hot-reload)
- IDE support: syntax highlighting, LSP
