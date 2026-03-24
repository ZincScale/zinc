# Guide: Actors in Zinc

A practical guide to using actors in Zinc — when to use them, common patterns, testing, and pitfalls.

## When to Use Actors

| Situation | Use | Why |
|---|---|---|
| Long-lived background work | `actor` | Lifecycle management, safe kill |
| Shared mutable state | `actor` | Isolation, no locks needed |
| Request-reply service | `actor` | Sequential processing, no races |
| Pipeline stage | `actor` | Actor-to-actor message passing |
| Short-lived parallel work | `concurrent { }` | Scoped, all-or-nothing |
| Parallel iteration | `parallel for` | Structured, bounded |
| Timeout-guarded operation | `timeout(dur) { }` | Deadline enforcement |
| One-off background task | Don't — use `concurrent` | Actors have overhead; use for sustained work |

**Rule of thumb**: if the concurrent unit has a *name* and a *lifecycle* (start, stop, restart), it's an actor. If it's a one-shot computation, use `concurrent` or `parallel for`.

## Pattern: Stateful Service

An actor that encapsulates a resource and exposes operations:

```zinc
actor Cache {
    var Map<String, String> store = new HashMap()

    receive fn put(String key, String value) {
        store.put(key, value)
    }

    receive fn get(String key): String {
        return store.getOrDefault(key, null)
    }

    receive fn size(): int {
        return store.size()
    }

    receive fn clear() {
        store.clear()
    }
}
```

No locks, no `synchronized`, no `ConcurrentHashMap`. The actor processes one message at a time, so state access is inherently thread-safe.

## Pattern: Pipeline

Chain actors together where each stage transforms data and forwards to the next:

```zinc
actor Parser {
    init Enricher next

    init(Enricher next) {
        this.next = next
    }

    receive fn process(String raw) {
        var data = Json.parse(raw)
        next.enrich(data)
    }
}

actor Enricher {
    init Writer next

    init(Writer next) {
        this.next = next
    }

    receive fn enrich(Map<String, String> data) {
        data.put("enriched_at", Instant.now().toString())
        next.write(data)
    }
}

actor Writer {
    init String outputDir

    init(String outputDir) {
        this.outputDir = outputDir
    }

    receive fn write(Map<String, String> data) {
        var path = outputDir + "/" + data.get("id") + ".json"
        Files.writeString(Path.of(path), Json.toJson(data))
    }
}

// Wire the pipeline — read bottom-up
var writer = new Writer("/tmp/output")
var enricher = new Enricher(writer)
var parser = new Parser(enricher)

// Feed data
parser.process("{\"id\": \"1\", \"name\": \"Alice\"}")
```

Each actor can be independently:
- Stopped and restarted (without affecting others)
- Replaced with a different implementation
- Monitored for throughput and errors

## Pattern: Worker Pool

Multiple actors consuming from a shared source:

```zinc
actor Worker {
    init String name
    init Channel<String> tasks

    init(String name, Channel<String> tasks) {
        this.name = name
        this.tasks = tasks
    }

    receive fn start() {
        while true {
            var task = tasks.poll(100, TimeUnit.MILLISECONDS) or { continue }
            processTask(task)
        }
    }

    fn processTask(String task) {
        print("[{name}] processing: {task}")
        Thread.sleep(50)  // simulate work
    }
}

// Create shared channel and workers
var tasks = new Channel<String>(1000)
var w1 = new Worker("w1", tasks)
var w2 = new Worker("w2", tasks)
var w3 = new Worker("w3", tasks)

w1.start()
w2.start()
w3.start()

// Enqueue work
for i in 0..100 {
    tasks.put("task-{i}")
}
```

Note: in this pattern, `start()` is a fire-and-forget `receive fn` that runs a loop on the actor thread. Other receive fns (like `stop()` or `getStats()`) will be queued and processed after the loop exits.

## Pattern: Aggregator

Collect results from multiple sources:

```zinc
actor Aggregator {
    var List<String> results = new ArrayList()
    var int expected = 0

    receive fn expect(int count) {
        expected = count
    }

    receive fn addResult(String result) {
        results.add(result)
    }

    receive fn isComplete(): boolean {
        return results.size() >= expected
    }

    receive fn getResults(): List<String> {
        return new ArrayList(results)
    }
}
```

## Pattern: Request-Reply with Timeout

For request-reply calls that might hang, wrap in a `timeout`:

```zinc
var result = timeout(5000) {
    slowActor.computeSomething(input)
} or {
    "fallback value"
}
```

The `timeout` block wraps the blocking `computeSomething()` call. If the actor doesn't respond in 5 seconds, the fallback is used.

## Testing Actors

### Unit testing a processor function

Test the `ProcessorFn` independently — no actor needed:

```zinc
ProcessorFn addTimestamp = (ff) -> {
    return new Single(ff.withAttribute("processed", "true"))
}

var input = new FlowFile("id1", {}, "hello".getBytes(), System.currentTimeMillis(), [])
var result = addTimestamp.process(input)
match result {
    case Single(ff) {
        assert ff.attributes().get("processed") == "true"
    }
}
```

### Integration testing an actor

Create the actor, send messages, verify state:

```zinc
var counter = new Counter(0)
counter.increment()
counter.increment()
counter.add(8)
Thread.sleep(100)  // let messages process
var n = counter.getCount()
assert n == 10
counter.shutdown()
```

The `Thread.sleep` is needed because fire-and-forget messages are async. For request-reply, no sleep is needed — the call blocks until the actor responds.

### Testing actor chains

Wire actors together, send input, verify final output:

```zinc
var sink = new TestSink()  // actor that collects results
var processor = new Worker("test", transformFn)
processor.setNext(sink)

processor.process(testFlowFile)
Thread.sleep(100)

var results = sink.getAll()
assert results.size() == 1
assert results.get(0).attributes().get("transformed") == "true"
```

### Testing shutdown

Verify that shutdown drains pending messages:

```zinc
var counter = new Counter(0)
for i in 0..100 { counter.increment() }
counter.shutdown()  // should drain all 100 increments
// After shutdown returns, all messages were processed
```

### Testing kill

Verify that kill discards pending messages:

```zinc
var counter = new Counter(0)
for i in 0..1000 { counter.increment() }
counter.kill()  // discards pending, doesn't wait
Thread.sleep(200)
// Counter's thread should be dead
```

## Migration from spawn

### Before (spawn)

```zinc
class Worker {
    var String state = "stopped"

    fn start() {
        state = "running"
        spawn { runLoop() }  // unstructured, no lifecycle
    }

    fn runLoop() {
        while state == "running" {
            // ... poll and process ...
        }
    }

    fn stop() {
        state = "stopped"  // hope the thread notices
    }
}
```

Problems: no error propagation, no safe kill, no supervision, thread can leak.

### After (actor)

```zinc
actor Worker {
    receive fn process(FlowFile ff) {
        // process one item at a time
        var result = transform(ff)
        next.process(result)
    }
}
```

Benefits: isolated state, lifecycle management, safe kill, supervisor can restart on failure.

## Anti-Patterns

### Don't: blocking receive fn

```zinc
// BAD — blocks the actor thread forever, no other messages processed
actor BadWorker {
    receive fn start() {
        while true {
            Thread.sleep(1000)
            doWork()
        }
    }

    receive fn stop() {
        // Never reached — start() blocks forever
    }
}
```

If you need a continuous loop, make the actor's primary job be processing messages. Don't run infinite loops in receive fns — that prevents the actor from handling other messages.

### Don't: share state between actors

```zinc
// BAD — shared mutable list defeats actor isolation
var shared = new ArrayList()
actor A { receive fn add(String s) { shared.add(s) } }
actor B { receive fn add(String s) { shared.add(s) } }
// Both actors mutate shared — race condition!
```

If two actors need to coordinate, use message passing: one actor owns the state, the other sends messages to it.

### Don't: create actors for one-off work

```zinc
// BAD — overhead of mailbox + thread for a single operation
var result = new OneShot().compute(42)

// GOOD — use concurrent for one-off parallel work
var (a, b) = concurrent { computeA(); computeB() }
```

Actors have startup overhead (mailbox allocation, thread creation). Use them for sustained concurrent work, not one-shot computations.

## Comparison with Other Actor Systems

| Feature | Zinc | Erlang/OTP | Akka/Pekko |
|---|---|---|---|
| Mailbox | `LinkedBlockingQueue` | Process mailbox | Bounded/unbounded |
| Threading | 1 virtual thread / actor | 1 BEAM process | Shared thread pool |
| Kill safety | Safe (owned state) | Safe (no shared memory) | Unsafe (shared JVM heap) |
| Supervision | `supervisor` keyword | `supervisor` behaviour | `SupervisorStrategy` class |
| Typing | Static (Zinc types) | Dynamic | `ActorRef[T]` |
| Message passing | `receive fn` | `receive` pattern match | `tell` / `ask` |
| Runtime overhead | ~zero (virtual threads) | ~zero (BEAM lightweight) | Moderate (dispatcher config) |
| Dependency | None (language primitive) | OTP stdlib | Akka library (100+ JARs) |

Zinc's actor model is closest to Erlang's in philosophy (isolation, let-it-crash, supervision) but runs on the JVM with static typing and zero framework dependencies.
