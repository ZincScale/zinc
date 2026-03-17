# Design: Transparent Concurrency

**Status:** Proposed
**Date:** 2026-03-17

## Problem

Concurrency is hard. Not because the concepts are hard, but because existing languages force developers to think about the _mechanism_ instead of the _intent_.

- **async/await (C#, JS, Python):** Function coloring — once you go async, everything above must be async. Deadlocks from mixing sync/async. `ConfigureAwait(false)` everywhere.
- **Manual threading (Java, C#):** Thread pools, locks, race conditions, deadlocks. Works but fragile under deadline pressure.
- **Goroutines + channels (Go):** Better, but channels are still a coordination primitive developers must reason about.

The result: most teams either avoid concurrency entirely (leaving performance on the table) or spend disproportionate time debugging threading issues.

## Philosophy

**Developers declare _what_ should run concurrently. The runtime decides _how_.**

No function coloring. No async/await. No manual thread management. You write normal, blocking-looking code. Zinc's runtime figures out the optimal execution strategy.

This is the direction Java is heading with Virtual Threads (Project Loom) and what Erlang/BEAM has done for decades. The insight: if the runtime manages scheduling, it can be smarter than the developer about CPU utilization.

## Design

### `spawn` — Run work concurrently

```zinc
main() {
    var result1 = spawn { fetchUser(1) }
    var result2 = spawn { fetchUser(2) }

    // Both are running concurrently. Accessing .value blocks until ready.
    print(result1.value)
    print(result2.value)
}
```

`spawn` returns a `Future<T>`. Accessing `.value` blocks the current fiber (not the OS thread) until the result is ready.

### `parallel` — Fan out, collect results

```zinc
main() {
    var users = [1, 2, 3, 4, 5]
    var profiles = parallel(users) { fetchProfile(it) }

    // All 5 fetches run concurrently, results collected in order
    for p in profiles {
        print(p.name)
    }
}
```

`parallel` is the common case — map a collection through a concurrent operation. No boilerplate.

### `spawn` blocks — Structured concurrency

```zinc
main() {
    spawn {
        var data = fetchData()        // runs on a fiber
        var enriched = enrich(data)   // still on the fiber
        save(enriched)                // still on the fiber
    }
    // Block exits when all spawned work completes
    // Errors propagate — if any fiber fails, the block cancels siblings
}
```

All spawned work is scoped. No fire-and-forget. No leaked threads. If `main()` exits, all fibers are cancelled.

### No function coloring

This is the key difference from async/await. In Zinc:

```zinc
// This function does I/O. It looks exactly like a non-I/O function.
String fetchUser(Int id) {
    var response = httpGet("https://api.example.com/users/{id}") or {
        return Error("fetch failed: {err}")
    }
    return response
}

// Calling it is normal. No await. No .Result. No async keyword.
main() {
    var user = fetchUser(1)
    print(user)
}
```

When `fetchUser` is called inside a `spawn`, the runtime suspends the fiber during I/O and schedules other work. When called outside `spawn`, it blocks normally. The developer doesn't care.

### `Lock` — Safe shared state

```zinc
main() {
    var counter = Lock(0)

    parallel(0..100) {
        counter.update { value + 1 }    // atomic read-modify-write
    }

    print(counter.value)    // 100
}
```

`Lock<T>` wraps a value with safe concurrent access. No manual lock/unlock. No forgetting to release. The trailing lambda receives the current value and returns the new value.

### `select` — Wait on multiple futures

```zinc
main() {
    var fast = spawn { fetchFromCache(key) }
    var slow = spawn { fetchFromDB(key) }

    var result = select {
        case fast -> fast.value
        case slow -> slow.value
    }
    // Returns whichever finishes first
}
```

## What Zinc Does NOT Have

| Feature | Why Not |
|---------|---------|
| `async` / `await` keywords | Function coloring. Infects entire call chain. |
| Manual `Thread` creation | Too low-level. Runtime manages threads. |
| `lock` / `unlock` / `Mutex` | Use `Lock<T>` instead. Can't forget to unlock. |
| Channels | Coordination primitive — use `Future<T>` and `select` instead. |
| `volatile` / memory fences | Runtime handles memory visibility. |

## C# Backend Mapping

| Zinc | C# |
|------|----|
| `spawn { expr }` | `Task.Run(() => expr)` wrapping a `Task<T>` |
| `future.value` | `.Result` (or `.GetAwaiter().GetResult()`) |
| `parallel(list) { ... }` | `Parallel.ForEachAsync` or `Task.WhenAll` + `Select` |
| `Lock<T>` | `lock` statement + wrapper class |
| `select { case ... }` | `Task.WhenAny` |

In .NET 10, `Task.Run` uses the thread pool efficiently. Future Zinc versions may target .NET's upcoming fiber/virtual-thread support when available.

## Examples

### Web scraper

```zinc
main() {
    var urls = ["https://a.com", "https://b.com", "https://c.com"]
    var pages = parallel(urls) { httpGet(it) or { "" } }

    for page in pages {
        print("got {page.size()} bytes")
    }
}
```

### Producer-consumer with Lock

```zinc
main() {
    var results = Lock(List<String>())

    parallel(0..10) {
        var data = processItem(it)
        results.update { value.Add(data); value }
    }

    print("processed {results.value.Count()} items")
}
```

### Timeout pattern

```zinc
main() {
    var work = spawn { longRunningTask() }
    var timeout = spawn { sleep(5000); Error("timeout") }

    var result = select {
        case work -> work.value
        case timeout -> panic("operation timed out")
    }
}
```

## Implementation Plan

### Phase 1 — `spawn` and `Future<T>`
- `spawn { expr }` → `Task.Run(() => expr)`
- `future.value` → `.Result`
- Structured scoping — block waits for all spawned tasks

### Phase 2 — `parallel`
- `parallel(collection) { ... }` → `Task.WhenAll` + `.Select`
- Ordered results matching input order

### Phase 3 — `Lock<T>` and `select`
- `Lock<T>` → wrapper class with `lock` statement
- `select` → `Task.WhenAny`

## Open Questions

1. **Cancellation:** Should `spawn` blocks support explicit cancellation tokens, or is scope-based cancellation sufficient?
2. **Error strategy:** If one fiber in `parallel` fails, cancel all siblings (fail-fast) or collect partial results?
3. **Backpressure:** Should `parallel` have a concurrency limit (`parallel(items, max: 4) { ... }`)?
4. **CPU-bound vs I/O-bound:** Should the runtime hint between compute and I/O tasks for thread pool tuning?
