# Design: Zinc Concurrency Model

> **Status**: DESIGN
> **Target**: Java 25 virtual threads + structured concurrency (preview APIs)
> **Quarkus**: `@RunOnVirtualThread` for REST/messaging

## Philosophy

Zinc concurrency is built on three principles:

1. **No colored functions** — there is no `async`/`await`. Every function is synchronous. Virtual threads make blocking cheap. You call `db.query()` and it blocks the virtual thread, not a platform thread. No function coloring, no futures-everywhere, no callback hell.

2. **Structured by default** — concurrent work has a defined lifetime. When a scope exits, all spawned work is done or cancelled. No orphaned threads, no fire-and-forget footguns. You can opt out for background tasks, but the default is safe.

3. **Simple primitives** — `spawn`, `parallel for`, `concurrent { }`, `timeout`, `lock`, `Channel<T>`. Compose them for complex patterns.

---

## Primitives

### 1. `spawn` — Fire a Virtual Thread

The simplest concurrency primitive. Launches work on a new virtual thread.

```zinc
spawn {
    sendEmail(user, "Welcome!")
}
```

Transpiles to:
```java
Thread.startVirtualThread(() -> {
    sendEmail(user, "Welcome!");
});
```

**With a result (future):**

```zinc
var future = spawn {
    fetchUser(id)
}
// ... do other work ...
var user = future.get()
```

Transpiles to:
```java
var future = new CompletableFuture<User>();
Thread.startVirtualThread(() -> {
    try { future.complete(fetchUser(id)); }
    catch (Exception e) { future.completeExceptionally(e); }
});
var user = future.get();
```

**`spawn` is unstructured** — the spawned thread outlives the calling scope. Use `parallel` or `concurrent` for structured work.

---

### 2. `parallel for` — Fan-Out Loop

Run loop iterations concurrently. All iterations must complete before the next statement.

```zinc
parallel for order in orders {
    process(order)
}
// All orders are processed here
```

Transpiles to:
```java
try (var scope = new StructuredTaskScope.ShutdownOnFailure()) {
    for (var order : orders) {
        scope.fork(() -> { process(order); return null; });
    }
    scope.join();
    scope.throwIfFailed();
}
```

**With concurrency limit:**

```zinc
parallel(max: 10) for order in orders {
    process(order)
}
```

Transpiles to a semaphore-bounded structured scope:
```java
var semaphore = new Semaphore(10);
try (var scope = new StructuredTaskScope.ShutdownOnFailure()) {
    for (var order : orders) {
        semaphore.acquire();
        scope.fork(() -> {
            try { process(order); return null; }
            finally { semaphore.release(); }
        });
    }
    scope.join();
    scope.throwIfFailed();
}
```

**With results (parallel map):**

```zinc
var results = parallel for order in orders {
    enrich(order)
}
// results: List<Order> — same order as input
```

Transpiles to forking with result collection, preserving order.

---

### 3. `concurrent` — Structured Fan-Out / Fan-In

Run multiple independent tasks, collect all results. If any task fails, all others are cancelled.

```zinc
var (user, orders, prefs) = concurrent {
    fetchUser(id)
    fetchOrders(id)
    fetchPrefs(id)
}
```

Transpiles to:
```java
User user; List<Order> orders; Prefs prefs;
try (var scope = new StructuredTaskScope.ShutdownOnFailure()) {
    var userTask = scope.fork(() -> fetchUser(id));
    var ordersTask = scope.fork(() -> fetchOrders(id));
    var prefsTask = scope.fork(() -> fetchPrefs(id));
    scope.join();
    scope.throwIfFailed();
    user = userTask.get();
    orders = ordersTask.get();
    prefs = prefsTask.get();
}
```

**First-success (race):**

```zinc
var fastest = concurrent(first: true) {
    fetchFromCacheA(key)
    fetchFromCacheB(key)
    fetchFromDB(key)
}
// Returns whichever completes first, cancels the rest
```

Transpiles to `StructuredTaskScope.ShutdownOnSuccess`.

---

### 4. `lock` — Synchronized Access

Protects shared mutable state. Uses `ReentrantLock` (not `synchronized` — avoids pinning on Java 21-23, though Java 25 fixes this).

```zinc
var counter = 0
var mu = Lock()

parallel for i in range(1000) {
    lock mu {
        counter += 1
    }
}
```

Transpiles to:
```java
var counter = new AtomicInteger(0); // transpiler may optimize to atomic
var mu = new ReentrantLock();

// ... parallel for ...
mu.lock();
try {
    counter.incrementAndGet();
} finally {
    mu.unlock();
}
```

**Transpiler optimization**: if the locked operation is a simple increment/compare-and-swap, the transpiler may emit `AtomicInteger`/`AtomicLong`/`AtomicReference` instead of a lock.

---

### 5. `channel` — Bounded Producer/Consumer Queue

Typed, bounded, blocking queue for communicating between concurrent tasks.

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

Transpiles to:
```java
var ch = new ArrayBlockingQueue<Order>(100);

Thread.startVirtualThread(() -> {
    for (var order : incomingOrders()) {
        ch.put(order); // blocks if full (backpressure)
    }
    ch.put(SENTINEL); // or use a wrapper with close semantics
});

// Consumer iterates until closed
for (Order order = ch.take(); order != SENTINEL; order = ch.take()) {
    process(order);
}
```

**Fan-out (multiple consumers):**

```zinc
var ch = Channel<Order>(capacity: 100)

// 4 consumers compete for items
parallel(max: 4) {
    for order in ch {
        process(order)
    }
}
```

**Select (wait on multiple channels):**

```zinc
var orders = Channel<Order>(100)
var cancellations = Channel<Cancellation>(100)

select {
    case order from orders -> processOrder(order)
    case cancel from cancellations -> processCancellation(cancel)
}
```

Transpiles to polling with `poll(timeout)` across queues, or a custom `Selector` utility.

---

### 6. `timeout` — Deadline-Aware Execution

Wrap any block with a deadline. If the work doesn't complete in time, it's cancelled.

```zinc
var result = timeout(5.seconds) {
    slowExternalApi(request)
} or {
    cachedFallback(request)
}
```

Transpiles to:
```java
Result result;
try (var scope = new StructuredTaskScope.ShutdownOnFailure()) {
    var task = scope.fork(() -> slowExternalApi(request));
    scope.joinUntil(Instant.now().plusSeconds(5));
    result = task.get();
} catch (TimeoutException e) {
    result = cachedFallback(request);
}
```

---

### 7. `rate` — Rate-Limited Execution

Limit how many operations per time window. Essential for API calls, database queries, external services.

```zinc
var limiter = Rate(100.perSecond)

parallel for user in users {
    rate limiter {
        callExternalApi(user)
    }
}
```

Transpiles to a semaphore with a replenishing scheduled task, or integrates with a library like Resilience4j.

---

### 8. `retry` — Automatic Retry with Backoff

```zinc
var result = retry(max: 3, backoff: exponential(100.millis)) {
    httpClient.post(url, payload)
} or {
    Err("Failed after 3 retries")
}
```

Transpiles to a retry loop with delay:
```java
Result result = null;
long delay = 100;
for (int attempt = 0; attempt < 3; attempt++) {
    try {
        result = httpClient.post(url, payload);
        break;
    } catch (Exception e) {
        if (attempt == 2) { result = Err.of("Failed after 3 retries"); break; }
        Thread.sleep(delay);
        delay *= 2;
    }
}
```

---

## GraalVM Native Image Considerations

- **Virtual threads**: fully supported in GraalVM native-image
- **StructuredTaskScope**: supported with `--enable-preview`
- **ReentrantLock**: fully supported
- **ArrayBlockingQueue**: fully supported

---

## Quarkus Integration

### REST Endpoints

```zinc
@Path("/orders")
@RunOnVirtualThread
class OrderResource {
    @GET
    fn list() List<Order> {
        // This blocks a virtual thread, not a platform thread
        return db.query("SELECT * FROM orders")
    }

    @POST
    fn create(Order order) Order {
        var saved = db.save(order)
        // Fire-and-forget: notify downstream
        spawn { notifyWarehouse(saved) }
        return saved
    }
}
```

- `@RunOnVirtualThread` tells Quarkus to dispatch to virtual threads
- Blocking DB calls are fine — virtual thread unmounts, carrier thread freed
- Connection pool (20 by default) becomes the bottleneck, not threads
- Java 25 fixes `synchronized` pinning — all JDBC drivers work correctly

### Scheduled Tasks

```zinc
@Scheduled(every: "5m")
@RunOnVirtualThread
fn cleanupExpired() {
    var expired = db.query("SELECT * FROM orders WHERE status = 'expired'")
    parallel(max: 10) for order in expired {
        archiveOrder(order)
    }
}
```

### Messaging (Kafka/NATS)

```zinc
@Incoming("orders")
@RunOnVirtualThread
fn processOrder(Order order) {
    // Each message processed on its own virtual thread
    var enriched = enrich(order)
    var validated = validate(enriched)
    db.save(validated)
}
```

---

## Summary: Zinc Concurrency Primitives

### Language Keywords (transpiler generates Java code)

These are part of the Zinc grammar. The transpiler emits the Java concurrency code.

| Primitive | Purpose | Java Mapping | Structured? |
|---|---|---|---|
| `spawn { }` | Fire a virtual thread | `Thread.startVirtualThread()` | No |
| `parallel for` | Fan-out loop, wait for all | `StructuredTaskScope.ShutdownOnFailure` | Yes |
| `parallel(max: N) for` | Bounded fan-out | `StructuredTaskScope` + `Semaphore` | Yes |
| `concurrent { }` | Fan-out tasks, collect results | `StructuredTaskScope` + `fork()` | Yes |
| `concurrent(first: true)` | Race, take first result | `StructuredTaskScope.ShutdownOnSuccess` | Yes |
| `lock mu { }` | Mutual exclusion | `ReentrantLock` (or `AtomicX` optimization) | N/A |
| `timeout(dur) { }` | Deadline-aware execution | `joinUntil(Instant)` | Yes |

### Standard Library (`zinc.concurrent`)

These are types and functions, not keywords. They compose with language primitives.

| Type/Function | Purpose | Java Mapping |
|---|---|---|
| `Channel<T>(capacity)` | Bounded producer/consumer queue | `ArrayBlockingQueue<T>` + close semantics |
| `select { case x from ch -> }` | Wait on multiple channels | Multi-queue poll/transfer |
| `Rate(n.perSecond)` | Rate limiter | `Semaphore` + `ScheduledExecutor` replenish |
| `retry(max, backoff) { }` | Auto-retry with backoff | Retry loop with `Thread.sleep()` |
| `Lock()` | Create a reentrant lock | `ReentrantLock` |
| `ReadWriteLock()` | Read-write lock | `ReentrantReadWriteLock` |
| `Semaphore(permits)` | Counting semaphore | `java.util.concurrent.Semaphore` |
| `Latch(count)` | One-shot barrier | `CountDownLatch` |
| `Barrier(parties)` | Cyclic barrier | `CyclicBarrier` |

The distinction matters: language keywords are optimized by the transpiler (e.g., `lock` on an integer may emit `AtomicInteger` instead of a lock). Library types are straightforward wrappers that compose with keywords.

### What's NOT in Zinc

- **No `async`/`await`** — virtual threads make blocking cheap. No colored functions.
- **No `synchronized` keyword** — use `lock` (generates `ReentrantLock`). Avoids pinning on older JVMs.
- **No raw `Thread` API** — use `spawn`, `parallel`, `concurrent`. The transpiler generates the thread management.
- **No `CompletableFuture` chaining** — use `concurrent { }` for fan-out/fan-in. The transpiler generates the futures.
- **No reactive streams** — virtual threads replace the need for reactive programming. Block freely.

---

## Examples

### REST API: Fan-out to multiple services

```zinc
@Path("/dashboard")
class DashboardResource {
    @GET
    fn get(int userId) Dashboard {
        var (user, orders, notifications) = concurrent {
            userService.find(userId)
            orderService.recent(userId, limit: 10)
            notificationService.unread(userId)
        }
        return Dashboard(user: user, orders: orders, notifications: notifications)
    }
}
```

### CLI tool: Process files with bounded concurrency

```zinc
var files = glob("data/*.csv")
var results = parallel(max: 4) for file in files {
    parseCsv(file)
}
print("Processed {results.count()} files")
```

### Web scraper: Producer/consumer with rate limiting

```zinc
var urls = Channel<String>(1000)
var results = Channel<Page>(1000)
var limiter = Rate(10.perSecond)

// Producer
spawn {
    for url in loadUrls("sitemap.xml") {
        urls.send(url)
    }
    urls.close()
}

// 4 worker consumers
parallel(max: 4) {
    for url in urls {
        rate limiter {
            var page = retry(max: 3, backoff: exponential(500.millis)) {
                httpClient.get(url)
            } or { null }
            if page != null {
                results.send(page)
            }
        }
    }
}
results.close()

// Sink
for page in results {
    saveToDisk(page)
}
```

### Batch job: Timeout + fallback

```zinc
fn enrichUser(User user) User {
    var profile = timeout(2.seconds) {
        externalProfileApi.fetch(user.email)
    } or {
        Profile(name: user.name, source: "fallback")
    }
    return user.withProfile(profile)
}
```

### Request-scoped context in a web app

```zinc
context RequestContext {
    String traceId
    String tenantId
    String? principal
}

@Path("/api")
class ApiResource {
    @GET
    fn handle(@HeaderParam("X-Trace-Id") String traceId,
              @HeaderParam("X-Tenant") String tenantId) Response {
        with RequestContext(traceId: traceId, tenantId: tenantId) {
            // Every service call in this scope sees the context
            var data = dataService.fetch()
            auditService.log("accessed data")  // reads RequestContext.current()
            return Response.ok(data)
        }
    }
}
