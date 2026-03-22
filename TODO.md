# Zinc v3 Roadmap

Convention-over-configuration JVM language. Transpiles `.zn` → `.java` → javac → JVM.

---

## Completed (v3.0-dev) — Phase 1

### Language
- [x] Brace-block syntax `{ }`, script mode
- [x] Java-native types: `int`, `double`, `boolean`, `char`, `long`, `String`, `List<T>`, `Map<K,V>`, `Set<T>`
- [x] Data classes → Java records: `data User(String name, int age)`
- [x] Enums: `enum Color { Red, Green, Blue }`
- [x] Classes with inheritance (`:` syntax), fields, methods
- [x] Visibility: fields private by default, `pub var` → getter+setter, `read var` → getter only
- [x] `init` fields → `private final` + getter
- [x] `override fn` → `@Override` annotation
- [x] `const` → `public static final`
- [x] Errors as values: `return Error()`, `or {}`, `or match` — no try/catch/throw
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
- [x] 100 codegen tests + parser/typechecker tests

---

## Completed — Phase 2

### Collections & Stream API
- [x] Collection methods → Stream API: `.filter()`, `.map()`, `.sortBy()`, `.limit()`, `.skip()`, `.distinct()`
- [x] Terminal ops: `.sum()`, `.anyMatch()`, `.allMatch()`, `.findFirst()`, `.forEach()`, `.groupBy()`, `.reduce()`
- [x] Chain detection: `items.filter(x).map(y).sum()` → single stream pipeline
- [x] `it` keyword in streams: `items.filter(it > 0).map(it * 2).sortBy(it.age)`
- [x] Auto-imports: `java.util.*` and `java.util.stream.*`

### Error Handling
- [x] `or` error handler: `var x = call() or default` → try/catch with fallback
- [x] `or { block }` handler with multi-statement fallback
- [x] `&&` / `||` for boolean (freed `or` keyword for error handling)

### Tuples
- [x] Tuple literals: `(a, b)` → `new Tuple2(a, b)` with auto-generated record
- [x] Tuple destructuring: `var x, y = swap(1, 2)` → `_tuple._0()`, `_tuple._1()`
- [x] Tuple records auto-generated as inner classes

### Packages & Imports
- [x] `package com.example` → Java package statement + directory structure
- [x] `import java.util.List`, `import java.util.*` — Java imports
- [x] Directory builds: `zinc build myproject/` scans all `.zn` files
- [x] Multi-file run: auto-discovers sibling `.zn` files with same package

### Type Features
- [x] Safe navigation: `obj?.field`, `obj?.method()` → null-check ternary
- [x] `sealed class` → sealed interface + variant records (separate files)
- [x] 100 codegen tests passing

### Deferred
- [ ] Source mapping: JSR-45 SMAP for debugger integration (.zn → .java line mapping)

## Phase 3 — Concurrency

- [x] `spawn` → `Thread.startVirtualThread()`
- [x] `concurrent { }` → `StructuredTaskScope` fan-out/fan-in
- [x] `parallel for` → `StructuredTaskScope` with bounded concurrency
- [x] `lock` → `ReentrantLock`
- [x] `timeout(dur) { }` → deadline-aware execution
- [x] `Channel<T>` → `ArrayBlockingQueue` with close semantics
- [x] Errors as values: `return Error()`, `or match`, no try/catch/throw

## Phase 4 — Packaging & Production

- [x] Mill is Zinc's build tool — full dependency management, fat JARs, native images
- [x] `zinc init [name]` — scaffold project with `build.mill.yaml`, `src/main.zn`, `.gitignore`
- [x] `zinc build` — delegates to `mill compile` for projects
- [x] `zinc run` — delegates to `mill run` for projects
- [x] `zinc build --native` → `mill nativeImage` (GraalVM AOT)
- [x] `zinc build --docker` → native binary + distroless Dockerfile (or JVM fallback)
- [x] `zinc build --k8s` → Docker + K8s manifest
- [x] `zinc update` — update toolchain (GraalVM, Mill, Quarkus)
- [x] Single installer (`install.sh`) for full toolchain

## Phase 5 — Ecosystem

- [x] `zinc update` — toolchain updater (done in Phase 4)
- [ ] Standard library: HTTP client, JSON, file I/O wrappers
- [ ] Quarkus dev mode integration (hot-reload)
- [ ] IDE support: syntax highlighting, LSP

---

## Docs

- [Language Reference](docs/language-reference.md) — index + links to topic guides
- [Design Doc](docs/design-zinc-v3-java.md) — v3 philosophy, Java transpilation
- [Concurrency](docs/design-zinc-concurrency.md) — virtual threads, structured concurrency
- [Transpilation Mapping](docs/design-zinc-java-transpilation.md) — Zinc → Java for every feature
- [Build Guide](docs/guide-mill-build.md) — Mill, dependencies, Docker, native-image, CI/CD

## Previous Versions

- v2 (Python target) — shelved
- v1 (C# AOT + Go backends) — shelved
