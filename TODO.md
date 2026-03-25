# Zinc Roadmap

Convention-over-configuration JVM language. `.zn` → Java 25 → native binary.

---

## Completed — Java Compiler (v1.0-dev)

### Compiler Rewrite
- [x] Rewritten from Go to Java (self-hosted on Java 25)
- [x] JavaParser-based code generation (no string concatenation)
- [x] Static JDK type database (no reflection)
- [x] GraalVM native-image for compiler binary (20MB, sub-second)
- [x] 232 unit tests + 14 e2e tests

### Language
- [x] Data classes → Java records
- [x] Sealed classes → sealed interfaces with permits + record variants
- [x] Pattern matching → Java 25 switch with record patterns
- [x] Errors as values: `or {}`, `return Error()`, no throws Exception
- [x] Virtual threads: spawn (CompletableFuture), concurrent, parallel for
- [x] StructuredTaskScope with Joiner for error propagation
- [x] Fluent collections: .filter(it > 0).map(it * 2).sum()
- [x] String interpolation, method aliases, expression-if, match expressions
- [x] Classes, interfaces, inheritance, constructors, default params, varargs
- [x] Native binary output via GraalVM native-image (13MB, 22ms)

### CLI
- [x] `zinc run <file|dir>` — compile and execute
- [x] `zinc build <file|dir>` — compile to Java (+ native by default)
- [x] `zinc init <name>` — scaffold project
- [x] Mill integration for project builds with dependencies

### Infrastructure
- [x] CI/CD: GitHub Actions with GraalVM JDK 25
- [x] Release: native binaries for Linux/macOS + universal jar
- [x] zinc-flow dogfooding: compiles and runs with Java compiler

---

## Next

### Language
- [ ] REPL
- [ ] `zinc fmt` — code formatter
- [ ] Parameter annotations (`@Body String body`)
- [ ] Class literals (`Foo.class`)
- [ ] Match guard clauses (`case X if cond`)
- [ ] Nested expression-if
- [ ] Named arg reordering

### Tooling
- [ ] LSP server for editor support
- [ ] Dependency management (`zinc add`, `zinc remove`, `zinc deps`)
- [ ] `zinc test` — built-in test runner
- [ ] Source mapping (JSR-45 SMAP) for debugger integration

### Performance
- [ ] Incremental compilation
- [ ] Parallel file compilation
- [ ] Compile-time constant folding
