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

| Language | Model | What Zinc takes |
|----------|-------|-----------------|
| **Erlang/BEAM** | Processes, message passing, supervisors, preemptive scheduling, "let it crash" | Supervision trees, process isolation, fault recovery, preemption philosophy |
| **Go** | Goroutines, M:N scheduler, work stealing | Simple `spawn` syntax, work stealing scheduler |
| **Java 21+** | Virtual threads (Project Loom) | Transparent concurrency — no function coloring |
| **Crystal** | Fibers + execution contexts (RFC 0002) | Context types (parallel/isolated/single), fiber-thread independence |
| **Kotlin** | Coroutines + structured concurrency | Scoped cancellation, parent-child fiber trees |

BEAM is the gold standard for resilient concurrent systems. Running since the 80s, powering telecom (five 9s uptime), WhatsApp (2M connections/server), Discord. Its key insight: **fault isolation at the process level + automatic recovery via supervision is more reliable than trying to write bug-free code.** Zinc adopts this philosophy while keeping a familiar OO syntax.

## Runtime Model

### Fibers — Lightweight processes

A **fiber** is a lightweight unit of work managed by the Zinc runtime. Inspired by BEAM processes and Go goroutines. Thousands of fibers can run on a handful of OS threads.

Key properties:
- **Any thread can resume any fiber** — fibers don't belong to threads, they belong to an execution context. The runtime moves fibers to wherever there's capacity.
- **Cooperative with preemption safety net** — fibers yield at I/O boundaries, `sleep`, and other suspension points. The runtime tracks execution budget and can preempt fibers that run too long without yielding (inspired by BEAM's reduction counting).
- **Isolated failure** — a crashing fiber doesn't take down other fibers. Errors propagate through the supervision tree, not through shared memory corruption.

### Execution Contexts

An execution context manages a pool of threads and a scheduler that runs fibers. Zinc starts with one context type and adds more as needed:

**Parallel (default, Phase 1)** — Multiple threads with work stealing. Fibers run in parallel across all available CPU cores. Idle threads steal fibers from busy threads — no wasted cores, no starvation. Sized to `cpu_count`. This is the only context most developers ever need.

**Isolated (Phase 2)** — One fiber, one dedicated thread. For CPU-bound work that would block other fibers (password hashing, compression, video encoding). Simple to add once parallel works — it's just a context with one thread and one fiber.

**Single (future)** — One thread, multiple fibers. Fibers run concurrently but never in parallel. No locks needed within the context. Useful for specific optimization scenarios.

### Work Stealing

The parallel context uses work-stealing scheduling:

```
Thread 1: [fiber A] [fiber B] [fiber C]    ← busy
Thread 2: [fiber D]                         ← light
Thread 3: (empty)                           ← idle, steals fiber C from Thread 1
```

When a thread's run queue is empty, it steals fibers from the busiest thread. No fibers sit idle while threads sleep. The runtime dynamically balances load across all cores.

### Defaults

Zinc starts a single **parallel** execution context sized to `cpu_count` threads. No flags, no configuration, no tuning. It just works.

## Developer API

Five primitives. That's the entire concurrency API.

| Primitive | Purpose | Returns |
|-----------|---------|---------|
| `spawn { }` | Run work on a fiber | `Future<T>` |
| `parallel(list) { }` | Fan-out/fan-in over a collection | `List<T>` |
| `Lock(value)` | Safe shared mutable state | `Lock<T>` |
| `select { }` | Race multiple futures | `T` |
| `supervised { }` | Auto-restart crashed fibers | — |

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

Results returned in input order. If any fiber fails, remaining fibers are cancelled (fail-fast).

With concurrency limit:

```zinc
main() {
    var urls = loadUrls()    // 10,000 URLs

    // At most 50 concurrent fetches — backpressure built in
    var pages = parallel(urls, max: 50) { httpGet(it) or { "" } }
}
```

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

When `fetchUser` runs inside a `spawn`, the runtime suspends the fiber during I/O and schedules other fibers. When called outside `spawn`, it blocks normally. The developer never thinks about it.

### Structured Concurrency

All spawned work is scoped. No fire-and-forget. No leaked fibers.

```zinc
main() {
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

### `supervised` — Let it crash, recover automatically

Inspired by Erlang/OTP supervisors. Instead of defensive try/catch everywhere, let fibers crash and let the supervisor handle recovery:

```zinc
main() {
    supervised {
        spawn { handleConnections(8080) }   // auto-restarts on crash
        spawn { processJobQueue() }          // auto-restarts on crash
        spawn { collectMetrics() }           // auto-restarts on crash
    }
    // If any fiber crashes, the supervisor restarts it.
    // The supervised block runs until explicitly stopped.
}
```

The supervisor logs the crash, restarts the fiber, and life goes on. This is how you build systems that run for months without intervention.

#### Restart strategies

```zinc
// Default: restart just the crashed fiber
supervised {
    spawn { serviceA() }
    spawn { serviceB() }
}

// one_for_all: if one crashes, restart all (for interdependent fibers)
supervised(strategy: one_for_all) {
    spawn { database() }
    spawn { cache() }       // depends on database
    spawn { api() }         // depends on both
}

// Restart budget: if a fiber crashes more than 5 times in 60 seconds, stop
supervised(max_restarts: 5, within: 60) {
    spawn { flakeyService() }
}
```

Strategies match Erlang/OTP's proven model:
- **one_for_one** (default): restart only the crashed fiber
- **one_for_all**: restart all fibers when one crashes
- **rest_for_one** (future): restart the crashed fiber and everything started after it

### `spawn(isolated: true)` — Dedicated thread

For CPU-bound work that would block other fibers:

```zinc
main() {
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
| Channels | Coordination primitive — `Future<T>` + `select` is simpler for most cases. |
| `volatile` / memory fences | Runtime handles memory visibility. |
| Thread affinity | Fibers resume on any thread. Runtime decides. |
| Callbacks / promises | Blocking `.value` is simpler. No callback hell. |

## C# Backend Mapping

| Zinc | C# Emit |
|------|---------|
| `spawn { expr }` | `Task.Run(() => expr)` returning `Task<T>` |
| `future.value` | `.GetAwaiter().GetResult()` (fiber-aware in future) |
| `parallel(list) { ... }` | `Task.WhenAll(list.Select(x => Task.Run(...)))` |
| `parallel(list, max: N) { ... }` | `SemaphoreSlim` + `Task.WhenAll` |
| `Lock<T>` | Wrapper class with `lock` statement |
| `select { ... }` | `Task.WhenAny` + `CancellationTokenSource` |
| `spawn(isolated: true) { ... }` | `Task.Factory.StartNew(..., TaskCreationOptions.LongRunning)` |
| `supervised { ... }` | While-loop with try/catch per spawned task, restart logic |

### Phase 1 vs Future Runtime

Phase 1 maps to .NET's `Task` system — efficient, battle-tested, good enough for most workloads. The .NET thread pool already does work stealing internally.

Future phases replace `Task.Run` with a Zinc-managed fiber scheduler — true fiber suspension without blocking thread pool threads, BEAM-style preemption budgets, custom work stealing. The developer API stays exactly the same — only the emit layer changes. This becomes important at high concurrency (10K+ fibers).

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

### Resilient web server

```zinc
main() {
    supervised {
        // HTTP listener — restarts if it crashes
        spawn { listenHttp(8080) }

        // Background workers — each restarts independently
        spawn { processEmailQueue() }
        spawn { syncExternalData() }
        spawn { cleanupExpiredSessions() }
    }
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

### CPU-bound isolation

```zinc
main() {
    var passwords = ["pass1", "pass2", "pass3"]

    var hashes = parallel(passwords) {
        spawn(isolated: true) { bcrypt(it, cost: 14) }.value
    }

    for h in hashes {
        print(h)
    }
}
```

### Nested supervision

```zinc
main() {
    supervised {
        // Database layer — if db crashes, restart cache too
        supervised(strategy: one_for_all) {
            spawn { databasePool() }
            spawn { queryCache() }
        }

        // API layer — each handler restarts independently
        supervised {
            spawn { handleUsers() }
            spawn { handleOrders() }
            spawn { handlePayments() }
        }
    }
}
```

## Implementation Plan

### Phase 1 — `spawn`, `Future<T>`, `parallel` (v0.11)
- AST: `SpawnExpr`, `ParallelExpr` nodes
- Parser: `spawn { expr }`, `parallel(collection) { expr }`, `parallel(collection, max: N) { expr }`
- C# codegen: `Task.Run`, `Task.WhenAll`, `SemaphoreSlim`
- `future.value` → `.GetAwaiter().GetResult()`
- Structured scoping: parent waits for all child tasks
- Error propagation: child failure cancels siblings via `CancellationTokenSource`
- E2E tests

### Phase 2 — `Lock<T>`, `select`, `spawn(isolated: true)` (v0.12)
- `Lock<T>`: emit wrapper class with `lock` statement
- `select { case ... }`: emit `Task.WhenAny` + cancellation
- `spawn(isolated: true)`: emit `TaskCreationOptions.LongRunning`

### Phase 3 — `supervised` (v0.12)
- Supervisor loop: try/catch + restart per fiber
- `strategy: one_for_one` (default), `one_for_all`
- `max_restarts` / `within` budget — stop if crashing too fast
- Crash logging

### Phase 4 — Custom fiber scheduler (future)
- Replace `Task.Run` with Zinc-managed fiber scheduler
- True fiber suspension without blocking thread pool threads
- Preemption budget (BEAM-style reduction counting)
- Work stealing across Zinc-managed threads
- Same developer API — only the emit layer changes

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Default context | Parallel, cpu_count threads | Right for 95% of apps. No config needed. |
| Fiber-thread binding | None — any thread resumes any fiber | Enables work stealing, prevents starvation |
| Error strategy in `parallel` | Fail-fast, cancel siblings | Matches structured concurrency expectation |
| `parallel` default max | Unlimited | Thread pool manages backpressure by default |
| Cancellation model | Scope-based, automatic | No explicit tokens. Scope exit = cancel children. |
| `select` losers | Cancelled automatically | Prevent resource leaks |
| Supervision default | one_for_one | Most fibers are independent. Match Erlang default. |
| Phase 1 backing | .NET Task/ThreadPool | Battle-tested, work stealing built in, zero custom runtime |
| Preemption | Cooperative now, preemptive in Phase 4 | Cooperative is simpler to implement correctly first |

## Open Questions

1. **Message passing:** Should fibers communicate via typed mailboxes (BEAM-style `receive`) in addition to shared state (`Lock<T>`)? Could be cleaner for actor-like patterns.
2. **Supervision nesting:** How deep should supervision trees go? Erlang allows arbitrary depth. Should Zinc?
3. **Hot restart state:** When a supervised fiber restarts, should it receive any state from its previous incarnation, or always start fresh?
4. **`with` + cancellation:** Should `with` (resource management) auto-dispose when a fiber is cancelled mid-block?
