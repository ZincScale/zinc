# Zinc — Concurrency

Zinc runs on Java 25 virtual threads. No async/await, no colored functions. Every function is synchronous — blocking is cheap because virtual threads unmount from carrier threads on I/O.

All structured primitives (`concurrent`, `parallel for`, `timeout`) transpile to Java 25's `StructuredTaskScope`.

## spawn

Run a block on a new virtual thread (unstructured, fire-and-forget):

```zinc
spawn {
    sendEmail(user, "Welcome!")
}
```

`spawn` is unstructured — the thread outlives the calling scope. Use `concurrent` or `parallel for` for structured work.

## concurrent

Fan-out multiple tasks, collect all results. If any task fails, all others are cancelled:

```zinc
var (user, orders, prefs) = concurrent {
    fetchUser(id)
    fetchOrders(id)
    fetchPrefs(id)
}
// All three complete or all cancel — no orphaned threads
```

Race — take the first result, cancel the rest:

```zinc
var fastest = concurrent(first: true) {
    fetchFromCacheA(key)
    fetchFromCacheB(key)
    fetchFromDB(key)
}
```

## parallel for

Process items concurrently. All iterations must complete before the next statement:

```zinc
parallel for order in orders {
    process(order)
}
// All orders processed here
```

With concurrency limit:

```zinc
parallel(max: 10) for order in orders {
    process(order)
}
```

With results (parallel map):

```zinc
var results = parallel for order in orders {
    enrich(order)
}
// results: List<Order> — same order as input
```

## lock

Mutual exclusion for shared mutable state:

```zinc
var mu = new Lock()
int counter = 0

parallel for item in items {
    int result = compute(item)
    lock mu {
        counter = counter + result
    }
}
```

The transpiler may optimize simple cases to `AtomicInteger` / `AtomicLong` instead of a lock.

## timeout

Deadline-aware execution with fallback:

```zinc
var result = timeout(5.seconds) {
    slowExternalApi(request)
} or {
    cachedFallback(request)
}
```

## Channel

Bounded producer/consumer queue for communicating between threads:

```zinc
var ch = new Channel<Order>(capacity: 100)

// Producer
spawn {
    for order in incomingOrders() {
        ch.send(order)
    }
    ch.close()
}

// Consumer
for order in ch {
    process(order)
}
```

Fan-out with multiple consumers:

```zinc
var ch = new Channel<Order>(capacity: 100)

parallel(max: 4) {
    for order in ch {
        process(order)
    }
}
```

## Summary

| Primitive | Purpose | Structured? |
|---|---|---|
| `spawn { }` | Fire a virtual thread | No |
| `concurrent { }` | Fan-out tasks, collect results | Yes |
| `concurrent(first: true)` | Race, take first result | Yes |
| `parallel for` | Fan-out loop, wait for all | Yes |
| `parallel(max: N) for` | Bounded fan-out | Yes |
| `lock mu { }` | Mutual exclusion | N/A |
| `timeout(dur) { }` | Deadline-aware execution | Yes |
| `new Channel<T>(n)` | Bounded producer/consumer | N/A |

### What's NOT in Zinc

- **No `async`/`await`** — virtual threads make blocking cheap. No colored functions.
- **No `synchronized`** — use `lock` (generates `ReentrantLock`).
- **No raw `Thread` API** — use `spawn`, `concurrent`, `parallel`.
- **No `CompletableFuture` chaining** — use `concurrent { }` for fan-out/fan-in.
- **No reactive streams** — virtual threads replace the need for reactive programming.
