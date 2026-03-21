# Design: Zinc → Java Transpilation Mapping

> **Status**: ANALYSIS — mapping Zinc syntax to Java 25 target
> **Context**: Zinc is becoming "what Groovy should have been" — a convention-over-config JVM language with static types, brace syntax, and AOT compilation via Quarkus/GraalVM.

## The Identity

Groovy failed because:
- Too dynamic (invokedynamic overhead, no AOT)
- No real type safety
- Became "the Gradle DSL language" instead of a language people write apps in
- Kotlin ate its lunch with better type safety + JetBrains backing

Zinc succeeds where Groovy failed because:
- **Statically typed** — every Zinc type maps to a concrete Java type. GraalVM can optimize.
- **Brace syntax** — familiar to Java/C#/Go developers. No whitespace sensitivity.
- **Convention over configuration** — project structure, build (Mill), deployment (Quarkus) are opinionated.
- **Flow engine as the killer app** — not a build tool, a data processing platform.
- **AOT story** — Quarkus + GraalVM native-image, not JVM-only.

---

## Clean Mappings (Zinc → Java 25)

These translate directly with minimal or no transformation:

### Variables

| Zinc | Java | Notes |
|---|---|---|
| `var x = 5` | `var x = 5;` | Java has `var` since 11. Identical semantics. |
| `var String name = "hi"` | `String name = "hi";` | Explicit type annotation |
| `const PI = 3.14` | `final var PI = 3.14;` | `const` → `final` |

### Data Classes → Records

| Zinc | Java |
|---|---|
| `data User { String name, int age }` | `record User(String name, int age) {}` |
| `data Point { double x, double y }` | `record Point(double x, double y) {}` |
| `data frozen Config { String host }` | `record Config(String host) {}` (records are already immutable) |

Java records are a perfect match — auto `equals()`, `hashCode()`, `toString()`. Zinc's `data` was designed for this.

### Enums

| Zinc | Java |
|---|---|
| `enum Color { Red, Green, Blue }` | `enum Color { RED, GREEN, BLUE }` |
| `enum Status { Active = 1 }` | `enum Status { ACTIVE(1); ... }` |

### Functions

| Zinc | Java |
|---|---|
| `fn greet(String name) String { return "Hi {name}" }` | `static String greet(String name) { return "Hi " + name; }` |
| `fn double(int x) int = x * 2` | `static int doubleVal(int x) { return x * 2; }` |
| `fn process() none { ... }` | `static void process() { ... }` |

### Lambdas

| Zinc | Java |
|---|---|
| `x -> x * 2` | `x -> x * 2` | **Identical.** |
| `(x, y) -> x + y` | `(x, y) -> x + y` | **Identical.** |
| `items.map(x -> x * 2)` | `items.stream().map(x -> x * 2)` | Stream wrapping needed |
| `items.map(it * 2)` | `items.stream().map(x -> x * 2)` | `it` expands to explicit param |

### Control Flow

| Zinc | Java |
|---|---|
| `if x > 0 { ... }` | `if (x > 0) { ... }` | Add parens |
| `for item in items { ... }` | `for (var item : items) { ... }` | Enhanced for-each |
| `while running { ... }` | `while (running) { ... }` | Add parens |
| `match cmd { case "start" -> ... }` | `switch (cmd) { case "start" -> ...; }` | Java 21+ pattern matching switch |

### Pattern Matching

| Zinc | Java 25 |
|---|---|
| `match shape { case Circle c -> ... case Rect r -> ... }` | `switch (shape) { case Circle c -> ... case Rect r -> ... }` |
| `case _ -> default()` | `default -> default();` |

Java 21+ pattern matching is a near-exact match for Zinc's `match`.

### Error Handling

| Zinc | Java |
|---|---|
| `try { risky() } catch IOException e { ... }` | `try { risky(); } catch (IOException e) { ... }` |
| `raise new IllegalArgumentException("bad")` | `throw new IllegalArgumentException("bad");` |

### Classes

| Zinc | Java |
|---|---|
| `class Dog { var String name }` | `class Dog { private String name; }` |
| `pub String name` | `public String name;` |
| `fn init(String name) { this.name = name }` | Constructor: `Dog(String name) { this.name = name; }` |
| `class Puppy : Dog { ... }` | `class Puppy extends Dog { ... }` |
| `interface Speaker { fn speak() String }` | `interface Speaker { String speak(); }` |
| `static fn create() Dog` | `static Dog create()` |

### Concurrency → Virtual Threads

| Zinc | Java 25 |
|---|---|
| `spawn { task() }` | `Thread.startVirtualThread(() -> task());` |
| `parallel for item in items { process(item) }` | `try (var scope = new StructuredTaskScope<>()) { for (var item : items) scope.fork(() -> { process(item); return null; }); scope.join(); }` |

### Context Managers → Try-with-Resources

| Zinc | Java |
|---|---|
| `with f = open("file") { ... }` | `try (var f = new FileReader("file")) { ... }` |

---

## Pythonisms to Remove or Remap

These were added during the Python pivot and need rethinking for Java:

### 1. `bytes` type
- **Python**: first-class `bytes` type with slicing, immutable
- **Java**: `byte[]` (mutable), `ByteBuffer`, or `ReadOnlyMemory` equivalent
- **Zinc decision**: `byte[]` maps to `byte[]`. For high-performance use cases, `ByteBuffer` or `MemorySegment` (Panama API) available. Hide behind Zinc's `byte[]` type.

### 2. `**kwargs` / named arguments
- **Python**: native keyword arguments
- **Java**: no keyword args. Alternatives: Builder pattern, record constructors
- **Zinc decision**: Keep named args in Zinc syntax. Transpiler generates builder or positional call. For `data` types, record constructor order matches declaration order.

### 3. Tuple unpacking
- **Python**: `(a, b) = (1, 2)`
- **Java**: no destructuring (yet — may come in future JDK)
- **Zinc decision**: Transpiler expands to individual assignments: `var a = tuple.first(); var b = tuple.second();`

### 4. List/dict comprehensions
- **Python**: `[x * 2 for x in items if x > 0]`
- **Java**: `items.stream().filter(x -> x > 0).map(x -> x * 2).toList()`
- **Zinc decision**: Keep comprehension syntax. Transpiler converts to stream chains. This is actually cleaner than Python's comprehension syntax for complex cases.

### 5. Smart dispatch (Polars/NumPy)
- **Python**: transpiler auto-chooses Polars for `list<dict>`, NumPy for `list<int>`
- **Java**: no equivalent. But Java has excellent stream performance and can use Apache Arrow / Tablesaw for columnar.
- **Zinc decision**: Smart dispatch concept stays but backends change. `List<Map>` chains → Java streams (or Tablesaw if data-heavy). Numeric chains → JVM SIMD vectorization (Vector API). This is a transpiler optimization, invisible to users.

### 6. Dunder method mapping
- **Python**: `fn str()` → `__str__`, `fn eq()` → `__eq__`
- **Java**: `fn str()` → `toString()`, `fn eq()` → `equals()`, `fn hash()` → `hashCode()`
- **Zinc decision**: Same concept, different target names. The mapping table just changes.

| Zinc | Python | Java |
|---|---|---|
| `fn String()` | `__str__` | `toString()` |
| `fn eq(other)` | `__eq__` | `equals(Object other)` |
| `fn hash()` | `__hash__` | `hashCode()` |
| `fn len()` | `__len__` | `size()` |
| `fn iter()` | `__iter__` | `iterator()` (implements `Iterable<T>`) |
| `fn compare(other)` | `__lt__` etc. | `compareTo(T other)` (implements `Comparable<T>`) |
| `fn get(key)` | `__getitem__` | `get(K key)` |
| `fn set(key, val)` | `__setitem__` | `put(K key, V val)` |
| `fn contains(item)` | `__contains__` | `contains(Object item)` |
| `fn enter()` / `fn exit()` | `__enter__` / `__exit__` | `implements AutoCloseable` → `close()` |
| `fn call(...)` | `__call__` | Implement functional interface (e.g., `Runnable`, `Function<T,R>`) |

### 7. `none` → `null` vs `Optional<T>`
- **Python**: `None` is a value, `Optional[T]` is a type hint
- **Java**: `null` is a value, `Optional<T>` is a wrapper type
- **Zinc decision**: `none` → `null` in generated code. `Type?` nullable syntax → compiler tracks nullability. Use `Optional<T>` only at API boundaries (method returns), never for fields or locals. This matches modern Java best practice.

### 8. String interpolation
- **Python**: f-strings `f"Hello {name}"`
- **Java**: String templates were removed from recent JDKs. Use `"Hello " + name` or `String.format("Hello %s", name)` or `STR` processor if available.
- **Zinc decision**: `"Hello {name}"` is Zinc syntax. Transpiler generates `"Hello " + name` for simple cases, `String.format()` for complex expressions. If Java brings back string templates, switch to that.

### 9. Multiple return values
- **Python**: `return a, b` → tuple
- **Java**: no tuples. Return a record or use out-parameters.
- **Zinc decision**: `fn split() (String, String)` transpiles to a generated record: `record SplitResult(String first, String second)`. Tuple unpacking at call site: `(a, b) = split()` → `var result = split(); var a = result.first(); var b = result.second();`

### 10. Dynamic imports
- **Python**: `import json` at any point in the file
- **Java**: imports must be at top of file
- **Zinc decision**: Transpiler collects all `import` statements and hoists to top of generated `.java` file. Zinc allows imports anywhere (convenience), Java output has them at top (requirement).

---

## Java 25 Features Zinc Should Adopt

### Already in Zinc (map directly)
- `var` (type inference) — Zinc has it
- Records — Zinc's `data` keyword
- Pattern matching switch — Zinc's `match`
- Virtual threads — Zinc's `spawn`
- Enhanced for-each — Zinc's `for in`

### Should Add to Zinc

#### 1. Sealed types (for exhaustive match)
```zinc
sealed class Shape {
    data Circle { double radius }
    data Rect { double width, double height }
}

// Compiler enforces all cases handled:
match shape {
    case Circle c -> pi * c.radius ** 2
    case Rect r -> r.width * r.height
    // No default needed — exhaustive
}
```
Maps to Java:
```java
sealed interface Shape permits Circle, Rect {}
record Circle(double radius) implements Shape {}
record Rect(double width, double height) implements Shape {}
```

#### 2. Structured concurrency
```zinc
// Zinc: parallel with result collection
var results = parallel {
    spawn fetchUser(id)
    spawn fetchOrders(id)
    spawn fetchPrefs(id)
}
// All complete or all cancel
```
Maps to Java `StructuredTaskScope.ShutdownOnFailure`.

#### 3. Foreign Function & Memory API (for native interop)
Not surfaced in Zinc syntax, but the runtime can use Panama API for:
- Direct memory access (zero-copy large payloads)
- Native library calls (SIMD, compression, crypto)

---

## Collection Method Mapping

Zinc uses fluent method chaining. Java requires `.stream()` entry and `.toList()` / `.collect()` exit.

| Zinc | Java |
|---|---|
| `items.filter(x -> x > 0)` | `items.stream().filter(x -> x > 0).toList()` |
| `items.map(x -> x * 2)` | `items.stream().map(x -> x * 2).toList()` |
| `items.filter(...).map(...)` | `items.stream().filter(...).map(...).toList()` |
| `items.first(x -> x > 10)` | `items.stream().filter(x -> x > 10).findFirst().orElse(null)` |
| `items.any(x -> x > 0)` | `items.stream().anyMatch(x -> x > 0)` |
| `items.all(x -> x > 0)` | `items.stream().allMatch(x -> x > 0)` |
| `items.sum()` | `items.stream().mapToInt(Integer::intValue).sum()` |
| `items.sort_by(x -> x.age)` | `items.stream().sorted(Comparator.comparing(x -> x.age())).toList()` |
| `items.group_by(x -> x.cat)` | `items.stream().collect(Collectors.groupingBy(x -> x.cat()))` |
| `items.distinct()` | `items.stream().distinct().toList()` |
| `items.take(10)` | `items.stream().limit(10).toList()` |
| `items.skip(5)` | `items.stream().skip(5).toList()` |
| `items.flat_map(x -> x.children)` | `items.stream().flatMap(x -> x.children().stream()).toList()` |
| `items.count()` | `items.size()` (or `items.stream().count()` for lazy) |
| `items.reduce(0, (a,x) -> a + x)` | `items.stream().reduce(0, (a,x) -> a + x)` |

The transpiler should be smart about terminal operations:
- If the chain result is iterated (for loop), skip `.toList()` — just return the stream.
- If the chain result is assigned to a variable or returned, add `.toList()`.
- Short-circuit ops (`first`, `any`, `all`) don't need `.toList()`.

---

## Type Mapping

| Zinc | Java | Notes |
|---|---|---|
| `int` | `int` / `Integer` | Primitive when possible, boxed in generics |
| `double` | `double` / `Double` | Java `float` is 32-bit; Zinc `double` = Java `double` (64-bit) |
| `String` | `String` | |
| `boolean` | `boolean` / `Boolean` | |
| `byte[]` | `byte[]` | |
| `List<T>` | `List<T>` | `java.util.List` |
| `Map<K, V>` | `Map<K, V>` | `java.util.Map` |
| `Set<T>` | `Set<T>` | `java.util.Set` |
| `(T, U)` | Generated record | Zinc tuples → named records |
| `T?` | `T` (nullable) | Compiler tracks nullability, no `Optional` for locals |
| `Result<T>` | Custom `Result<T>` class or `sealed interface` | Generate once in runtime lib |
| `Fn<(A, B), R>` | `BiFunction<A, B, R>` | Map to java.util.function types |

---

## What Zinc Adds Over Java (The Value Proposition)

These are the things that make Zinc worth using instead of raw Java:

1. **No semicolons** — newline terminates statements
2. **No `public static void main`** — script mode, top-level statements
3. **`data` instead of `record` ceremony** — fewer keywords, no `implements`, auto-derives
4. **`it` keyword** — `items.map(it * 2)` vs `items.stream().map(x -> x * 2)`
5. **Trailing lambdas** — cleaner callback APIs
6. **`or {}` error handling** — `val x = parse(s) or { default }` instead of try/catch boilerplate
7. **No checked exceptions** — all exceptions are unchecked. Use `Result<T>` for expected failures.
8. **Fluent collections without `.stream()/.toList()`** — transpiler adds them
9. **Convention-over-config project structure** — `zinc init`, `zinc build`, `zinc run`
10. **Null safety** — `Type?` syntax with compiler enforcement, no NPEs from untracked nulls
12. **String interpolation** — `"Hello {name}"` without ceremony
13. **Named arguments** — always available, transpiler generates positional or builder
14. **`spawn` / `parallel for`** — virtual threads without boilerplate
15. **Smart collection dispatch** — transpiler chooses optimal backend (streams vs parallel streams vs Tablesaw)

---

## What Changes from the Python Design

| Feature | Python Target | Java Target | Change |
|---|---|---|---|
| Smart dispatch | Polars/NumPy | Streams/Parallel streams/Tablesaw | Backend swaps, concept stays |
| `bytes` | `bytes` (immutable, refcounted) | `byte[]` (mutable) or `ByteBuffer` | Different memory model |
| Free-threading | Python 3.14t (no GIL) | Virtual threads (always had true threading) | Java is better here |
| Packaging | PyInstaller/Nuitka/Docker | Quarkus native-image/JLink/Docker | GraalVM AOT |
| Dunders | `__str__`, `__eq__`, etc. | `toString()`, `equals()`, etc. | Name mapping changes |
| Type system | Gradual (type hints) | Static (compile-time enforced) | Java is stricter — good |
| None handling | `None` is untyped | `null` with nullability tracking | Zinc adds safety over Java |
| Imports | Anywhere in file | Hoisted to top | Transpiler handles |
| REPL | Python REPL | JShell or custom | JShell-based |
| Startup time | ~50ms (Python) | ~30ms (GraalVM native) / ~500ms (JVM) | Native-image for CLI tools |

---

## Implementation Priority

### Phase 1 — Core Language (transpile to .java, compile with javac)
- var/const/types → Java locals and fields
- fn → methods (static for top-level, instance for class methods)
- data → records
- enum → Java enums
- if/else/for/while → Java control flow
- match → Java pattern-matching switch
- lambdas → Java lambdas
- class/interface/inheritance → Java classes
- try/catch/raise → Java exceptions
- import → Java imports (hoisted)
- string interpolation → concatenation or String.format

### Phase 2 — Collections & Ergonomics
- Collection methods → Stream API mapping
- `it` keyword → lambda expansion
- Trailing lambdas → desugaring
- Named arguments → positional reorder or builder
- Tuple types → generated records
- Comprehensions → stream chains
- `or {}` error handling → try/catch sugar

### Phase 3 — Concurrency
- spawn → Thread.startVirtualThread
- parallel for / concurrent { } → StructuredTaskScope
- timeout(dur) { } → StructuredTaskScope + joinUntil
- Channel<T> → ArrayBlockingQueue
- lock → ReentrantLock

### Phase 4 — Packaging & Deployment (Mill + Quarkus)
- zinc init → Mill project scaffold
- zinc build → mill compile + native-image
- zinc run → mill run
- zinc pack → native-image / JLink / Docker
