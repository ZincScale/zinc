# Zinc — Concurrency

Zinc runs on Java 25 virtual threads. No async/await, no colored functions. Every function is synchronous — blocking is cheap because virtual threads unmount from carrier threads on I/O.

See `design-zinc-concurrency.md` for the full design with Java transpilation details.

## spawn

Run a block on a new virtual thread:

```zinc
spawn {
    sendEmail(user, "Welcome!")
}
```

With a result (future):

```zinc
var future = spawn {
    fetchUser(id)
}
print("main continues...")
var user = future.get()
```

Spawn multiple tasks:

```zinc
var f1 = spawn { download("file1.zip") }
var f2 = spawn { download("file2.zip") }
var f3 = spawn { download("file3.zip") }

var r1 = f1.get()
var r2 = f2.get()
var r3 = f3.get()
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
var mu = Lock()
var int counter = 0

parallel for item in items {
    var int result = compute(item)
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
    Err("Failed after 3 retries")
}
```

## Channel

Bounded producer/consumer queue for communicating between threads:

```zinc
var ch = Channel<Order>(capacity: 100)

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
var ch = Channel<Order>(capacity: 100)

parallel(max: 4) {
    for order in ch {
        process(order)
    }
}
```

## select

Wait on multiple channels:

```zinc
var orders = Channel<Order>(100)
var cancellations = Channel<Cancellation>(100)

select {
    case order from orders -> processOrder(order)
    case cancel from cancellations -> processCancellation(cancel)
}
```

## context

Scoped values for implicit context propagation — no parameter drilling:

```zinc
context RequestContext {
    str traceId
    str tenantId
}

// Bind at entry point
with RequestContext(traceId: uuid(), tenantId: "acme") {
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
fn scrapeUrls(List<str> urls) List<str> {
    var results = Channel<str>(1000)
    var limiter = Rate(10.perSecond)

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

    var List<str> titles = []
    for title in results {
        titles.add(title)
    }
    return titles
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
| `retry(max, backoff) { }` | Auto-retry with backoff | N/A |
| `Channel<T>(n)` | Bounded producer/consumer | N/A |
| `select { }` | Wait on multiple channels | N/A |
| `context T { }` | Scoped value declaration | Yes |
| `with T(...) { }` | Bind scoped value | Yes |

### What's NOT in Zinc

- **No `async`/`await`** — virtual threads make blocking cheap. No colored functions.
- **No `synchronized`** — use `lock` (generates `ReentrantLock`).
- **No raw `Thread` API** — use `spawn`, `parallel`, `concurrent`.
- **No `CompletableFuture` chaining** — use `concurrent { }` for fan-out/fan-in.
- **No reactive streams** — virtual threads replace the need for reactive programming.
