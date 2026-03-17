# Design: Transparent Concurrency

**Status:** Proposed
**Date:** 2026-03-17

## Problem

Concurrency is hard. Not because the concepts are hard, but because existing languages force developers to think about the _mechanism_ instead of the _intent_.

- **async/await (C#, JS, Python):** Function coloring — once you go async, everything above must be async. Deadlocks from mixing sync/async. `ConfigureAwait(false)` everywhere.
- **Manual threading (Java, C#):** Thread pools, locks, race conditions, deadlocks. Works but fragile under deadline pressure.
- **Goroutines + channels (Go):** Better, but channels are still a coordination primitive developers must reason about.

The result: most teams either avoid concurrency entirely (leaving performance on the table) or spend disproportionate time debugging threading issues amid tight deadlines, business constraints, and changing requirements. Threading beats single-threaded in most cases, but it's difficult to get right.

## Philosophy

**Developers declare _what_ should run concurrently. The runtime decides _how_.**

No function coloring. No async/await. No manual thread management. You write normal, blocking-looking code. Zinc's runtime figures out the optimal execution strategy — which threads to use, when to steal work, when to suspend a fiber.

This is the direction the industry is heading: Java Virtual Threads (Project Loom), Kotlin coroutines, Crystal execution contexts, and what Erlang/BEAM has done for decades. The insight: if the runtime manages scheduling, it can be smarter than the developer about CPU utilization.

## Prior Art

| Language | Model | Strengths | Weaknesses |
|----------|-------|-----------|------------|
| **Go** | Goroutines + channels, M:N scheduler, work stealing | Simple syntax, efficient | Channels are still a coordination primitive |
| **Kotlin** | Coroutines + dispatchers, structured concurrency | Multiple contexts, cancellation | `suspend` keyword = function coloring |
| **Java 21+** | Virtual threads (Project Loom) | Transparent, no coloring | No structured concurrency built-in |
| **Crystal** | Fibers + execution contexts (RFC 0002) | Context types (concurrent/parallel/isolated), work stealing | Complex API surface |
| **Erlang/BEAM** | Processes + message passing, preemptive | Fault isolation, no shared state | Different programming model |

Zinc takes the best from each: Go's simplicity, Kotlin's structured scoping, Java's transparency, and Crystal's execution context architecture.

## Runtime Model

### Fibers — Not threads

A **fiber** is a lightweight unit of work managed by the Zinc runtime. Thousands of fibers can run on a handful of OS threads. Fibers are cooperative — they yield at I/O boundaries, `sleep`, and other suspension points.

Key property: **a fiber can be resumed by any thread**. Fibers don't belong to threads, they belong to an execution context. The runtime moves fibers to wherever there's capacity.

### Execution Contexts

An execution context manages a pool of threads and a scheduler that runs fibers. Zinc provides three context types:

**Parallel (default)** — Multiple threads with work stealing. Fibers run in parallel across all available CPU cores. Idle threads steal fibers from busy threads — no wasted cores, no starvation. This is the only context most developers ever need.

**Isolated** — One fiber, one dedicated thread. For CPU-bound work that would block other fibers (password hashing, compression, video encoding). The fiber owns the thread entirely.

**Single** — One thread, multiple fibers. Fibers run concurrently but never in parallel. Useful when you need to avoid synchronization overhead for a group of fibers that share state — no locks needed within the context, only for cross-context communication.

### Work Stealing

The parallel context uses work-stealing scheduling (inspired by Go and Crystal RFC 0002):

```
Thread 1: [fiber A] [fiber B] [fiber C]    ← busy
Thread 2: [fiber D]                         ← light
Thread 3: (empty)                           ← idle, steals fiber C from Thread 1
```

When a thread's run queue is empty, it steals fibers from the busiest thread. No fibers sit idle while threads sleep. The runtime dynamically balances load across all cores.

### Defaults

Zinc starts a single **parallel** execution context sized to `System.cpu_count` threads. This is the right default for ~95% of applications. Developers who need more control can create additional contexts.

No flags, no configuration, no tuning. It just works.

## Developer API

### `spawn` — Run work concurrently

```zinc
main() {
    var result1 = spawn { fetchUser(1) }
    var result2 = spawn { fetchUser(2) }

    // Both are running concurrently. Accessing .value suspends the
    // current fiber (not the OS thread) until the result is ready.
    print(result1.value)
    print(result2.value)
}
```

`spawn` returns a `Future<T>`. Accessing `.value` suspends the current fiber and lets the runtime schedule other work on the same thread.

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

`parallel` is the common case — map a collection through a concurrent operation. Results are returned in input order. If any fiber fails, remaining fibers are cancelled (fail-fast).

### `parallel` with concurrency limit

```zinc
main() {
    var urls = loadUrls()    // 10,000 URLs

    // At most 50 concurrent fetches — backpressure built in
    var pages = parallel(urls, max: 50) { httpGet(it) or { "" } }
}
```

The `max` parameter caps concurrent fibers. Essential for I/O-bound work against rate-limited APIs or databases. Default: unlimited (bounded only by thread pool size).

### No function coloring

This is the key difference from async/await:

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

When `fetchUser` runs inside a `spawn`, the runtime suspends the fiber during I/O and schedules other fibers on the same thread. When called outside `spawn`, it blocks normally. The developer never has to think about which mode they're in.

### Structured Concurrency

All spawned work is scoped. No fire-and-forget. No leaked fibers.

```zinc
main() {
    // This scope waits for all spawned fibers to complete
    var results = spawn {
        var a = spawn { fetchData("source-a") }
        var b = spawn { fetchData("source-b") }
        merge(a.value, b.value)
    }

    print(results.value)
    // If a fails, b is cancelled. If main() exits, everything is cancelled.
}
```

Fibers form a tree. Parent fibers wait for children. Errors propagate up. Cancellation propagates down. No orphaned work.

### `Lock<T>` — Safe shared state

```zinc
main() {
    var counter = Lock(0)

    parallel(0..100) {
        counter.update { value + 1 }    // atomic read-modify-write
    }

    print(counter.value)    // 100
}
```

`Lock<T>` wraps a value with safe concurrent access. The trailing lambda receives the current value and returns the new value. Can't forget to unlock — the scope handles it.

### `select` — Race multiple futures

```zinc
main() {
    var fast = spawn { fetchFromCache(key) }
    var slow = spawn { fetchFromDB(key) }

    var result = select {
        case fast -> fast.value
        case slow -> slow.value
    }
    // Whichever finishes first wins. The other is cancelled.
}
```

### `spawn(isolated: true)` — Dedicated thread

For CPU-bound work that would block other fibers:

```zinc
main() {
    // Runs on its own thread — won't block other fibers
    var hash = spawn(isolated: true) {
        bcrypt(password, cost: 14)
    }

    // Meanwhile, handle other work on the main context
    var user = fetchUser(id)

    print("hash: {hash.value}")
}
```

The isolated fiber gets a dedicated OS thread. The parallel context's threads remain free for other fibers.

## What Zinc Does NOT Have

| Feature | Why Not |
|---------|---------|
| `async` / `await` keywords | Function coloring. Infects entire call chain. |
| Manual `Thread` creation | Too low-level. Runtime manages threads. |
| `lock` / `unlock` / `Mutex` | Use `Lock<T>` instead. Can't forget to unlock. |
| Channels | Coordination primitive — `Future<T>` + `select` is simpler. |
| `volatile` / memory fences | Runtime handles memory visibility. |
| Thread affinity | Fibers resume on any thread. Runtime decides. |

## C# Backend Mapping

| Zinc | C# Emit |
|------|---------|
| `spawn { expr }` | `Task.Run(() => expr)` returning `Task<T>` |
| `future.value` | `.GetAwaiter().GetResult()` (fiber-aware in future) |
| `parallel(list) { ... }` | `Task.WhenAll(list.Select(x => Task.Run(...)))` |
| `parallel(list, max: N) { ... }` | `SemaphoreSlim` + `Task.WhenAll` |
| `Lock<T>` | Wrapper class with `lock` statement |
| `select { ... }` | `Task.WhenAny` + cancellation of losers |
| `spawn(isolated: true) { ... }` | `Task.Factory.StartNew(..., TaskCreationOptions.LongRunning)` |

### Phase 1 vs Future

Phase 1 maps to .NET's `Task` system — efficient, battle-tested, good enough for most workloads. The thread pool already does work stealing internally.

Future phases may emit a custom fiber scheduler that sits below `Task`, giving us true fiber suspension (not blocking thread pool threads on `.Result`). This becomes important at high concurrency (10K+ fibers). The developer API stays exactly the same — only the emit changes.

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

### API with concurrent enrichment

```zinc
data User(pub String name, pub Int age)
data Profile(pub User user, pub List<String> posts, pub Int followers)

Profile loadProfile(Int userId) {
    var user = spawn { fetchUser(userId) }
    var posts = spawn { fetchPosts(userId) }
    var followers = spawn { fetchFollowerCount(userId) }

    Profile(user.value, posts.value, followers.value)
}

main() {
    var ids = [1, 2, 3, 4, 5]
    var profiles = parallel(ids) { loadProfile(it) }

    for p in profiles {
        print("{p.user.name}: {p.followers} followers, {p.posts.Count()} posts")
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

### CPU-bound isolation

```zinc
main() {
    var passwords = ["pass1", "pass2", "pass3"]

    // Each hash runs on its own isolated thread — doesn't block the fiber pool
    var hashes = parallel(passwords) {
        spawn(isolated: true) { bcrypt(it, cost: 14) }.value
    }

    for h in hashes {
        print(h)
    }
}
```

## Implementation Plan

### Phase 1 — `spawn` and `Future<T>` (v0.11)
- AST: `SpawnExpr` node
- Parser: `spawn { expr }` syntax
- C# codegen: `Task.Run(() => expr)` + `.GetAwaiter().GetResult()` for `.value`
- Structured scoping: parent waits for all child tasks
- Error propagation from child to parent

### Phase 2 — `parallel` (v0.11)
- AST: `ParallelExpr` node
- Parser: `parallel(collection) { expr }` and `parallel(collection, max: N) { expr }`
- C# codegen: `Task.WhenAll` + `Select`, `SemaphoreSlim` for max
- Results in input order

### Phase 3 — `Lock<T>` and `select` (v0.12)
- `Lock<T>`: wrapper class with `lock` statement, `.value` and `.update { }`
- `select`: `Task.WhenAny` with cancellation of non-winners

### Phase 4 — `spawn(isolated: true)` (v0.12)
- C# codegen: `TaskCreationOptions.LongRunning`
- Validates that isolated fibers don't spawn children into the isolation context

### Phase 5 — Custom fiber scheduler (future)
- Replace `Task.Run` with a Zinc-managed fiber scheduler
- True fiber suspension without blocking thread pool threads
- Work stealing across Zinc-managed threads
- Same developer API — only the emit layer changes

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Default context | Parallel, cpu_count threads | Right for 95% of apps. No config needed. |
| Fiber-thread binding | None — any thread can resume any fiber | Enables work stealing, prevents starvation |
| Error strategy in `parallel` | Fail-fast, cancel siblings | Matches structured concurrency expectation |
| `parallel` default max | Unlimited | Let the thread pool manage backpressure by default |
| Cancellation model | Scope-based, automatic | No explicit tokens. Parent scope exit = cancel children. |
| `select` losers | Cancelled automatically | Prevent resource leaks from abandoned futures |
| Phase 1 backing | .NET Task/ThreadPool | Battle-tested, work stealing built in, zero custom runtime |

## Open Questions

1. **Fiber preemption:** Should the runtime preempt fibers that run too long without yielding? Crystal explicitly doesn't, Go does. For Phase 1, cooperative is fine.
2. **Context API:** Should developers be able to create custom execution contexts (`Context.new(threads: 4)`) or is `spawn` + `isolated` sufficient?
3. **`with` + concurrency:** Should `with` (resource management) interact with fiber scoping — e.g., auto-dispose when a fiber is cancelled?
