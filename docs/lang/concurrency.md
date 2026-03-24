# Zinc — Concurrency

Zinc runs on Java 25 virtual threads. No async/await, no colored functions. Every function is synchronous — blocking is cheap because virtual threads unmount from carrier threads on I/O.

See `design-zinc-concurrency.md` for the full design with Java transpilation details.

## actor

An actor is an isolated concurrent unit. It owns its state exclusively, communicates via message passing, and runs on its own virtual thread. Actors can be safely killed because no external code references their state.

```zinc
actor Counter {
    var int count = 0

    init(int start) {
        count = start
    }

    // Fire-and-forget — caller doesn't wait
    receive fn increment() {
        count += 1
    }

    receive fn add(int n) {
        count += n
    }

    // Request-reply — caller blocks until response
    receive fn getCount(): int {
        return count
    }

    // Regular fn = private helper, runs on actor thread
    fn validate(int n): boolean {
        return n > 0
    }
}
```

Usage:

```zinc
var counter = new Counter(0)    // actor starts immediately
counter.increment()              // async, returns immediately
counter.add(5)                   // async, returns immediately
var n = counter.getCount()       // blocks until reply: 6
```

### Actor lifecycle

- **`shutdown()`** — cooperative: drains pending messages, waits for actor thread to exit
- **`shutdown(timeoutMs)`** — cooperative with escalation: waits up to timeout, then interrupts
- **`kill()`** — brutal: interrupts thread, discards pending messages, hands thread to reaper

```zinc
counter.shutdown()          // wait for clean exit
counter.shutdown(5000)      // wait 5s, then interrupt
counter.kill()              // immediate kill
```

### Why actors, not spawn

`spawn` creates unstructured threads — fire-and-forget with no error propagation, no lifecycle management, and no safe way to kill. Actors provide:

- **Isolation** — state is private, no shared memory corruption
- **Message passing** — all communication through the mailbox
- **Lifecycle** — shutdown/kill with guaranteed cleanup
- **Brutal kill safety** — because state is owned, thread abandonment is safe

### ActorRuntime

When any actor is killed, its thread is registered with a global reaper. If a killed thread doesn't die within the reaper timeout (default 10s), the system exits with `System.exit(1)`. This guarantees no dangling resources — ever.

Three system states, no fourth:
1. **Running** — actors processing messages
2. **Shutting down** — actors draining and joining
3. **Fatal** — killed thread refused to die → forced exit

## supervisor

A supervisor manages child actors. It can start, stop, and restart them.

```zinc
supervisor Pipeline {
    init String strategy = "one_for_one"
    init int maxRestarts = 3
    init long within = 5000

    child worker1 = new Counter(0)
    child worker2 = new Counter(100)
}
```

- `child` declares a managed actor with a factory expression (used for restart)
- Strategy: `one_for_one` (restart only the failed child), `one_for_all` (restart all on any failure)

```zinc
var sup = new Pipeline()
sup.start()                  // create and start all children
sup.shutdown()               // cascade shutdown to all children
sup.shutdown(5000)           // cascade with timeout, then kill
```

## spawn (deprecated)

> **Deprecated** — use `actor` for long-lived concurrent work, `concurrent` for short-lived fan-out.

`spawn` creates an unstructured virtual thread with no lifecycle management. It is preserved for backward compatibility but emits a compiler warning.

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

## retry

Auto-retry with backoff:

```zinc
var result = retry(max: 3, backoff: exponential(100.millis)) {
    httpClient.post(url, payload)
} or {
    Error("Failed after 3 retries")
}
```

## Channel

Bounded producer/consumer queue for communicating between threads:

```zinc
var ch = new Channel<Order>(capacity: 100)

// Producer actor
actor Producer {
    init Channel<Order> ch

    init(Channel<Order> ch) {
        this.ch = ch
    }

    receive fn produce(List<Order> orders) {
        for order in orders {
            ch.send(order)
        }
        ch.close()
    }
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

## select

Wait on multiple channels:

```zinc
var orders = new Channel<Order>(100)
var cancellations = new Channel<Cancellation>(100)

select {
    case order from orders -> processOrder(order)
    case cancel from cancellations -> processCancellation(cancel)
}
```

## context

Scoped values for implicit context propagation — no parameter drilling:

```zinc
context RequestContext {
    String traceId
    String tenantId
}

// Bind at entry point
with new RequestContext(traceId: uuid(), tenantId: "acme") {
    handleRequest()
}

// Read anywhere in the call chain — no parameter needed
fn handleRequest() {
    var ctx = RequestContext.current()
    print("[{ctx.traceId}] Processing for {ctx.tenantId}")
}
```

Context automatically propagates to child threads spawned with `concurrent` or `parallel for`.

## Practical Example

A parallel web scraper with rate limiting and timeout:

```zinc
fn scrapeUrls(List<String> urls): List<String> {
    var results = new Channel<String>(1000)
    var limiter = new Rate(10.perSecond)

    parallel(max: 4) for url in urls {
        rate limiter {
            var title = timeout(5.seconds) {
                var content = httpClient.get(url)
                parseTitle(content)
            } or {
                "timeout: {url}"
            }
            results.send(title)
        }
    }
    results.close()

    List<String> titles = []
    for title in results {
        titles.add(title)
    }
    return titles
}
```

## Summary

| Primitive | Purpose | Structured? |
|---|---|---|
| `actor` | Isolated concurrent unit with mailbox | Yes (owned) |
| `supervisor` | Manages actor lifecycle and restarts | Yes (owned) |
| `concurrent { }` | Fan-out tasks, collect results | Yes |
| `concurrent(first: true)` | Race, take first result | Yes |
| `parallel for` | Fan-out loop, wait for all | Yes |
| `parallel(max: N) for` | Bounded fan-out | Yes |
| `lock mu { }` | Mutual exclusion | N/A |
| `timeout(dur) { }` | Deadline-aware execution | Yes |
| `retry(max, backoff) { }` | Auto-retry with backoff | N/A |
| `new Channel<T>(n)` | Bounded producer/consumer | N/A |
| `select { }` | Wait on multiple channels | N/A |
| `context T { }` | Scoped value declaration | Yes |
| `with T(...) { }` | Bind scoped value | Yes |
| ~~`spawn { }`~~ | ~~Fire a virtual thread~~ | Deprecated |

### What's NOT in Zinc

- **No `async`/`await`** — virtual threads make blocking cheap. No colored functions.
- **No `synchronized`** — use `lock` (generates `ReentrantLock`).
- **No raw `Thread` API** — use `actor` for long-lived work, `concurrent`/`parallel` for short-lived.
- **No `CompletableFuture` chaining** — use `concurrent { }` for fan-out/fan-in. Actors use it internally for request-reply.
- **No reactive streams** — virtual threads replace the need for reactive programming.
