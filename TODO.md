# Zinc Feature Roadmap

Convention over configuration. Less typing, less ceremony.

---

## Priority Order

### P1 — Logging (Serilog)
Structured logging backed by Serilog, configured via zinc.toml. See [design doc](docs/design-foundation-features.md#5-logging).

```zinc
log.info("user login", userId: 42, region: "us-east-1")
log.error("payment failed", orderId: "abc-123")
```

- **Effort:** Medium

### P2 — VS Code Extension
TextMate grammar for `.zn` syntax highlighting.
- **Effort:** Quick

### P3 — `service` Keyword / Convention Routing
The "Spring Boot play" — convention-based web services. See [identity doc](docs/design-foundation-features.md#what-this-enables).
- **Effort:** Large — needs design doc first

---

## Interop Roadmap

| Use Case | Status |
|----------|--------|
| Logging, Config, JSON | Ready (logging needs Serilog impl) |
| HTTP client, REST calls | Ready (`httpGet` builtin) |
| Testing | Ready (`zinc test`) |
| Dependencies | Ready (`zinc add/remove/deps`) |
| Private NuGet repos | Ready (GitHub Packages, Artifactory) |
| REST API server | Needs `service` keyword (P3) |
| Database / ORM | Future |

---

## Revisit Later

| Feature | Notes |
|---------|-------|
| Drop Go compilation target | Go backend functional but dual-backend leaks Go idioms. C# AOT is sole output. Keep Go as compiler impl language. |
| Typed errors | Exception hierarchy |
| Operator overloading | Via .NET interop |
| Channels | Only if in-process producer/consumer demand emerges |
| Supervised blocks | Only if in-process resilience demand emerges (K8s handles restarts) |
| Scripting builtins | `args`, `exec(cmd)`, `fileExists`, `listDir` — deprioritized, not the enterprise play |

---

## Completed (v0.12.0)
- `use` keyword: `use System.Text.Json` replaces `import "System.Text.Json"` (no quotes, dotted identifiers)
- `zinc test`: test discovery (`test_*` convention), assert builtins, test harness generation
- `zinc add/remove/deps`: NuGet dependency management with version resolution
- Private NuGet sources: GitHub Packages and Artifactory with env-var auth
- Proper TOML parser (pelletier/go-toml/v2) replacing hand-rolled parser
- Type checker: lenient for .NET types (uppercase identifiers pass through)
- Functions now `public static` and `partial class` for multi-file support
- CI: .NET 10 on all runners, C# smoke tests, dropped Go-backend smoke tests
- Assert builtins: `assert`, `assertEqual`, `assertNotEqual`
- 70 E2E tests, 6 C# smoke tests, all passing

## Completed (v0.11.0)
- Concurrency: `spawn { }` → `Future<T>`, `parallel(list) { }` → `List<T>`, `Lock(value)` → thread-safe wrapper
- No async/await, no function coloring — three primitives only
- `ZincScope` structured concurrency — all spawned work is scoped, no fire-and-forget
- Error propagation: child failure cancels siblings via `CancellationTokenSource`
- `or { }` on `future.value` and `parallel(list) { }` — catches fiber errors with `AggregateException` unwrapping
- See [design doc](docs/design-concurrency.md)

## Completed (v0.10.0)
- Implicit return, expression if, expression match, range loops
- `--release` flag, embedded debug info
- Removed Go-only features

## Completed (v0.9.0)
- Trailing lambdas + `it` keyword, data classes, batched E2E runner

## Completed (v0.8.0)
- Generic annotations, doc restructure, dead code cleanup

## Completed (v0.7.0)
- NuGet imports, CSharpTypeResolver, auto `new`, AOT fixes

## Completed (v0.6.0)
- 28 builtins, failable infrastructure, `or { }` error handling

## Completed (v0.5.0 and earlier)
- C# AOT backend, LINQ, `zinc.toml`, TypeRegistry, OO polymorphism
- Go backend, pointer inference, REPL, CI
