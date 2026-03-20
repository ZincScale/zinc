# Zinc v3 Roadmap

Convention-over-configuration JVM language. Transpiles `.zn` → `.java` → javac → JVM.

---

## Completed (v3.0-dev) — Phase 1

### Language
- [x] Brace-block syntax `{ }`, `fn` keyword, script mode
- [x] Java-native types: `int`, `double`, `boolean`, `char`, `long`, `String`, `List<T>`, `Map<K,V>`, `Set<T>`
- [x] Data classes → Java records: `data User(String name, int age)`
- [x] Enums: `enum Color { Red, Green, Blue }`
- [x] Classes with inheritance (`:` syntax), fields, methods
- [x] Visibility: fields private by default, `pub var` → getter+setter, `read var` → getter only
- [x] `init` fields → `private final` + getter
- [x] `override fn` → `@Override` annotation
- [x] `const` → `public static final`
- [x] Two-track error handling: `Result<T>` / `Error` + `try`/`catch`/`throw`
- [x] `throw X from Y` (exception chaining)
- [x] `and`/`or`/`not`, `not in`, `is not`
- [x] `is` type checks: `x is String` → `instanceof`
- [x] Kotlin-style equality: `==` structural (Objects.equals), `===` reference identity
- [x] Expression if (condition-first ternary)
- [x] Lambdas: `x -> expr`, `(a, b) -> expr`, block lambdas
- [x] `it` keyword: `items.filter(it > 0)` → lambda expansion
- [x] String interpolation: `"Hello, {name}!"` → concatenation
- [x] Single-quote strings (literal, no interpolation)
- [x] Triple-quote strings (multi-line)
- [x] `**` power operator → `Math.pow()`
- [x] `in` / `not in` → `.contains()`
- [x] `match`/`case` → Java pattern-matching switch
- [x] `break`/`continue`
- [x] `null` (not `none`)
- [x] Variadic args: `String... messages`
- [x] Named arguments (call-site reordering)
- [x] Constructor auto-`new`: `User("Alice")` → `new User("Alice")`
- [x] Java annotations pass-through: `@Deprecated`, `@Path`, `@GET`

### Type System
- [x] Type mismatches: `var int x = "hello"` → error
- [x] Return type verification: all code paths must return
- [x] Function call arg type and count checking
- [x] Type narrowing after `is` checks
- [x] `break`/`continue` outside loop detection
- [x] Undefined variable detection

### Codegen
- [x] Multi-file output: each `data`, `enum`, `class` → separate `.java` file
- [x] Top-level functions + statements → main class with `main()`
- [x] Builtin mapping: `print` → `System.out.println`, `len` → `.size()`
- [x] Type mapping: primitives pass-through, boxed types for generics

### CLI
- [x] `zinc build <file.zn>` — transpile to Java + compile with javac
- [x] `zinc run <file.zn>` — transpile + compile + run
- [x] `zinc fmt` — format source code
- [x] `zinc repl` — Java-backed REPL
- [x] 62 codegen tests + parser/typechecker tests

---

## Phase 2 — Collections & Ergonomics

- [ ] Collection methods → Stream API codegen (`.filter()`, `.map()`, `.sortBy()`, etc.)
- [ ] `or {}` error handling sugar for Result<T>
- [ ] Tuple types → generated value records
- [ ] Tuple destructuring: `var (x, y) = swap(1, 2)`
- [ ] Safe navigation: `obj?.field`
- [ ] Source mapping: JSR-45 SMAP for debugger integration (.zn → .java line mapping)
- [ ] `sealed class` support in parser

## Phase 3 — Concurrency & Flow Engine

- [ ] `spawn` → `Thread.startVirtualThread()`
- [ ] `concurrent { }` → `StructuredTaskScope` fan-out/fan-in
- [ ] `parallel for` → `StructuredTaskScope` with bounded concurrency
- [ ] `lock` → `ReentrantLock`
- [ ] `timeout(dur) { }` → deadline-aware execution
- [ ] `Channel<T>` → `ArrayBlockingQueue` with close semantics
- [ ] `context` / `with` → `ScopedValue` for context propagation
- [ ] `@processor` / pipeline DSL → Zinc Flow runtime (Java library)

## Phase 4 — Packaging & Production

- [ ] Mill integration: `zinc init` generates `build.mill.yaml`
- [ ] `zinc build --native` → Quarkus + GraalVM native-image
- [ ] `zinc build --docker` / `zinc build --k8s`
- [ ] JLink fallback when native-image fails

## Phase 5 — Ecosystem

- [ ] Zinc Flow processor library (Kafka, S3, HTTP, JDBC connectors)
- [ ] REST API for flow pipeline management
- [ ] TUI dashboard for pipeline monitoring
- [ ] Quarkus dev mode integration (hot-reload processors)

---

## Docs

- [Language Reference](docs/language-reference.md) — index + links to topic guides
- [Design Doc](docs/design-zinc-v3-java.md) — v3 philosophy, Java transpilation
- [Concurrency](docs/design-zinc-concurrency.md) — virtual threads, structured concurrency
- [Transpilation Mapping](docs/design-zinc-java-transpilation.md) — Zinc → Java for every feature
- [OwnedBuffer Pattern](docs/design-owned-buffer-pattern.md) — zero-GC FlowFile processing
- [Zinc Flow](docs/design-zinc-flow.md) — NiFi-inspired flow processing design
- [Benchmark Results](benchmarks/RESULTS.md) — Python vs .NET vs Java performance

## Previous Versions

- v2 (Python target) — shelved, Python threading limitations in pipeline benchmarks
- v1 (C# AOT + Go backends) — shelved, Quarkus/Micronaut cover that space
