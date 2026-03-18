# Design: Concurrency

**Status:** Phase 1 Implemented
**Date:** 2026-03-17

## Problem

Concurrency is hard. Not because the concepts are hard, but because existing languages force developers to think about the _mechanism_ instead of the _intent_.

- **async/await (C#, JS, Python):** Function coloring — once you go async, everything above must be async. Deadlocks from mixing sync/async.
- **Manual threading (Java, C#):** Thread pools, locks, race conditions, deadlocks. Works but fragile under deadline pressure.
- **Goroutines + channels (Go):** Better, but channels are still a coordination primitive developers must reason about.

Most teams either avoid concurrency entirely (leaving performance on the table) or spend disproportionate time debugging threading issues.

## Philosophy

**Developers declare _what_ should run concurrently. The runtime decides _how_.**

No function coloring. No async/await. No manual thread management. Write normal code. The runtime handles threads.

## Context

Zinc targets web apps, REST APIs, and data pipeline services running on AWS (Lambda, ECS, Kubernetes). The typical request flow:

```
Client → REST API → process → respond
                  → or: accept, process in stages, forward to downstream systems
```

Inter-service communication uses SQS, Kafka, or RabbitMQ. Kubernetes handles process-level restarts. The infrastructure is the resilience layer — not the language runtime.

Within a single request, concurrency means: fan out to multiple APIs or processing steps, wait for results, combine, return. That's it.

## Design

Three primitives. That's the entire concurrency API.

| Primitive | Purpose | Returns |
|-----------|---------|---------|
| `spawn { }` | Run work on a fiber | `Future<T>` |
| `parallel(list) { }` | Spawn over a collection, collect results | `List<T>` |
| `Lock(value)` | Safe shared mutable state | `Lock<T>` |

### `spawn` — Run work concurrently

```zinc
main() {
    var result1 = spawn { fetchUser(1) }
    var result2 = spawn { fetchUser(2) }

    // Both running concurrently. .value suspends current fiber until ready.
    print(result1.value)
    print(result2.value)
}
```

`spawn` returns a `Future<T>`. Accessing `.value` suspends the current fiber (not the OS thread) until the result is ready.

### `parallel` — Spawn over a collection

Sugar over spawn + collect. These two always happen together, so one keyword:

```zinc
main() {
    var users = [1, 2, 3, 4, 5]
    var profiles = parallel(users) { fetchProfile(it) }

    for p in profiles {
        print(p.name)
    }
}
```

`parallel` spawns a fiber per item, waits for all results, returns them in input order. It's not a separate concept from `spawn` — just the shorthand for the collection case.

### No function coloring

```zinc
// This function does I/O. Looks exactly like a non-I/O function.
String fetchUser(Int id) {
    var response = httpGet("https://api.example.com/users/{id}") or {
        return Error("fetch failed: {err}")
    }
    return response
}

// No await. No .Result. No async keyword.
main() {
    var user = fetchUser(1)
    print(user)
}
```

Inside `spawn`, the runtime suspends the fiber during I/O. Outside `spawn`, it blocks normally. The developer doesn't care which.

### Structured scoping

All spawned work is scoped. No fire-and-forget. No leaked fibers.

```zinc
main() {
    var result = spawn {
        var a = spawn { fetchData("source-a") }
        var b = spawn { fetchData("source-b") }
        merge(a.value, b.value)
    }

    print(result.value)
    // If a fails, b is cancelled. If main() exits, everything is cancelled.
}
```

### `Lock<T>` — Safe shared state

```zinc
main() {
    var counter = Lock(0)

    parallel(0..100) {
        counter.update { it + 1 }
    }

    print(counter.value)    // 100
}
```

`Lock<T>` wraps a value with safe concurrent access. `.update { }` locks, receives the current value as `it`, and returns the new value. Can't forget to unlock.

```zinc
var cache = Lock(Map<String, User>())
var stats = Lock(Stats())

parallel(requests) {
    var user = fetchUser(it.userId)

    cache.update { it.Add(user.id, user) }
    stats.update { it.totalProcessed = it.totalProcessed + 1 }
}
```

Each Lock is independently locked — updating `cache` doesn't block fibers updating `stats`.

**Limitation:** If you need to update two Locks atomically (e.g., move an item from one collection to another), there's no built-in transaction. Keep locked operations simple and independent. If multi-Lock atomicity is needed, restructure into a single Lock holding both values.

## What Zinc Does NOT Have

| Feature | Why Not |
|---------|---------|
| `async` / `await` | Function coloring. |
| Channels | Infrastructure (SQS, Kafka, Rabbit) handles inter-service messaging. In-process channels solve a problem most apps don't have. |
| `select` / racing | Rare need within a single request. Can be built from spawn if needed. |
| `supervised` / restart strategies | Kubernetes restarts crashed pods. Infrastructure is the supervisor. |
| Manual threads | Runtime manages threads. |
| `volatile` / memory fences | Runtime handles memory visibility. |

## Runtime Model

The runtime starts a thread pool sized to `cpu_count`. Fibers are scheduled across threads with work stealing — idle threads take work from busy threads. No configuration needed.

Fibers can be resumed by any thread. The runtime moves them to wherever there's capacity.

For Phase 1, this maps directly to .NET's `ThreadPool` which already does work stealing.

## C# Backend Mapping

| Zinc | C# Emit |
|------|---------|
| `spawn { expr }` | `__scope.Spawn<T>(() => expr)` — tracked by `ZincScope` |
| `future.value` | `.GetAwaiter().GetResult()` — re-throws fiber exceptions |
| `future.value or { }` | try/catch on `.Value` — catches fiber errors |
| `parallel(list) { ... }` | `Task.WhenAll(list.Select(x => Task.Run(..., __scope.Token)))` |
| `Lock<T>` | Wrapper class with `lock` statement |
| `main() { ... }` | Wrapped in `using (var __scope = new ZincScope())` — structured exit |

## Examples

### Concurrent API enrichment

```zinc
data Profile(pub String name, pub List<String> posts, pub Int followers)

Profile loadProfile(Int userId) {
    var user = spawn { fetchUser(userId) }
    var posts = spawn { fetchPosts(userId) }
    var followers = spawn { fetchFollowerCount(userId) }

    Profile(user.value.name, posts.value, followers.value)
}

main() {
    var profile = loadProfile(42)
    print("{profile.name}: {profile.followers} followers")
}
```

### Batch processing

```zinc
main() {
    var items = loadWorkItems()
    var results = parallel(items) { process(it) }
    print("processed {results.Count()} items")
}
```

### Shared counter

```zinc
main() {
    var count = Lock(0)

    var results = parallel(0..10) {
        var data = fetchData(it)
        count.update { it + 1 }
        data
    }

    print("fetched {count.value} items")
}
```

## Implementation Plan

### Phase 1 (v0.11) — Complete
- AST: `SpawnExpr`, `ParallelExpr` nodes
- Parser: `spawn { expr }`, `parallel(collection) { expr }`
- C# codegen: `ZincScope` for structured concurrency, `ZincFuture<T>`, `ZincLock<T>`
- Structured scoping: `main()` wrapped in `ZincScope` — all spawned work is scoped, no fire-and-forget
- Error propagation: child failure cancels siblings via `CancellationTokenSource`
- `or { }` on `future.value`: catches fiber errors with familiar Zinc error handling
- `Lock<T>`: emit wrapper class with `lock` statement
- E2E tests: spawn, parallel, Lock, error propagation, sibling cancellation

### Future — only if needed
- `spawn(isolated: true)` for CPU-bound work on a dedicated thread
- Custom fiber scheduler replacing Task.Run for 10K+ fiber workloads
- `supervised` blocks if in-process resilience demand emerges

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Three primitives only | `spawn` + `parallel` + `Lock<T>` | Covers 90% of in-process concurrency. Less to learn. |
| No channels | Infrastructure (SQS/Kafka) handles messaging | Avoids duplicating what the deployment platform already provides. |
| `parallel` is sugar | Spawn per item + collect results | The two-line spawn+collect pattern always happens together. One keyword. |
| No `supervised` | Kubernetes/infrastructure handles restarts | Language shouldn't duplicate platform capabilities. |
| No `select` | Rare within a request | Can be built from spawn. Add if demand emerges. |
| Structured scoping | Parent waits, errors cancel siblings | Prevents leaked fibers. Simple mental model. |
| Phase 1 backing | .NET Task/ThreadPool | Battle-tested, work stealing built in, zero custom runtime. |
