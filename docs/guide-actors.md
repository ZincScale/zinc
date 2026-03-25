# Guide: Actors in Zinc

A practical guide to using actors — when to use them, common patterns, testing, and pitfalls.

## When to Use Actors

| Situation | Use | Why |
|---|---|---|
| Long-lived background work | `class : Actor` | Lifecycle management, safe kill |
| Shared mutable state | `class : Actor` | Isolation, no locks needed |
| Request-reply service | `class : Actor` | Sequential processing, no races |
| Pipeline stage | `class : Actor` | Actor-to-actor message passing |
| Short-lived parallel work | `concurrent { }` | Scoped, all-or-nothing |
| Parallel iteration | `parallel for` | Structured, bounded |
| Timeout-guarded operation | `timeout(dur) { }` | Deadline enforcement |

**Rule of thumb**: if the concurrent unit has a *name* and a *lifecycle* (start, stop, restart), it's an actor. If it's a one-shot computation, use `concurrent` or `parallel for`.

## Pattern: Stateful Service

```zinc
class Cache : Actor {
    var Map<String, String> store = new HashMap()

    pub fn put(String key, String value) {
        store.put(key, value)
    }

    pub fn get(String key): String {
        return store.getOrDefault(key, null)
    }

    pub fn size(): int {
        return store.size()
    }
}
```

No locks needed. The actor processes one message at a time — state access is inherently thread-safe.

## Pattern: Pipeline

```zinc
class Parser : Actor {
    init Enricher next

    init(Enricher next) {
        this.next = next
    }

    pub fn process(String raw) {
        var data = Json.parse(raw)
        next.enrich(data)
    }
}

class Enricher : Actor {
    init Writer next

    init(Writer next) {
        this.next = next
    }

    pub fn enrich(Map<String, String> data) {
        data.put("enriched_at", Instant.now().toString())
        next.write(data)
    }
}

class Writer : Actor {
    init String outputDir

    init(String outputDir) {
        this.outputDir = outputDir
    }

    pub fn write(Map<String, String> data) {
        Files.writeString(Path.of(outputDir + "/" + data.get("id") + ".json"), Json.toJson(data))
    }
}

// Wire the pipeline
var writer = new Writer("/tmp/output")
var enricher = new Enricher(writer)
var parser = new Parser(enricher)

// Supervisor manages all
class Pipeline : Supervisor {
    init Parser parser
    init Enricher enricher
    init Writer writer

    init(Parser parser, Enricher enricher, Writer writer) {
        this.parser = parser
        this.enricher = enricher
        this.writer = writer
    }
}

var sup = new Pipeline(parser, enricher, writer)
sup.start()

parser.process("{\"id\": \"1\", \"name\": \"Alice\"}")
```

## Pattern: Request-Reply with Timeout

```zinc
var result = timeout(5000) {
    slowActor.computeSomething(input)
} or {
    "fallback value"
}
```

## Testing Actors

### Unit test — direct mode, no threads

```zinc
var counter = new Counter(0)
counter.increment()
counter.increment()
counter.add(8)
var n = counter.getCount()
assert n == 10
```

No `Thread.sleep`, no timing, no async. Actors are plain classes in direct mode.

### Integration test — supervised mode

```zinc
var counter = new Counter(0)
class TestSup : Supervisor {
    init Counter c
    init(Counter c) { this.c = c }
}
var sup = new TestSup(counter)
sup.start()

counter.increment()
counter.increment()
Thread.sleep(100)  // let async messages process
var n = counter.getCount()
assert n == 2

sup.shutdown()
```

### Testing supervisor lifecycle

```zinc
class MockRuntime : ActorRuntime {
    var int killCount = 0
    pub fn pendingKill(Thread t) { killCount += 1 }
}

var runtime = new MockRuntime()
var counter = new Counter(0)
var sup = new TestSup(counter)
sup.kill()
assert runtime.killCount == 1
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
            // poll and process
        }
    }
}
```

### After (Actor + Supervisor)

```zinc
class Worker : Actor {
    pub fn process(FlowFile ff) {
        var result = transform(ff)
        next.process(result)
    }
}

class Pipeline : Supervisor {
    init Worker worker
    init(Worker worker) { this.worker = worker }
}

var worker = new Worker()
var sup = new Pipeline(worker)
sup.start()
// worker.process(ff) now goes through mailbox
```

## Anti-Patterns

### Don't: share mutable state between actors

```zinc
// BAD
var shared = new ArrayList()
class A : Actor { pub fn add(String s) { shared.add(s) } }
class B : Actor { pub fn add(String s) { shared.add(s) } }
```

If two actors need to coordinate, one actor owns the state, the other sends messages to it.

### Don't: create actors for one-off work

```zinc
// BAD — overhead for a single operation
var result = new OneShot().compute(42)

// GOOD — use concurrent
var (a, b) = concurrent { computeA(); computeB() }
```

### Don't: use actors without a supervisor in production

Unsupervised actors work in direct mode (synchronous) — useful for testing, but no threads, no async, no lifecycle management.

## Comparison with Other Actor Systems

| Feature | Zinc | Erlang/OTP | Akka/Pekko |
|---|---|---|---|
| Declaration | `class : Actor` | `module` + behaviour | `class extends AbstractActor` |
| Mailbox | `ArrayBlockingQueue` | Process mailbox | Configurable |
| Threading | 1 virtual thread / actor | 1 BEAM process | Shared thread pool |
| Kill safety | Safe (owned state) | Safe (no shared memory) | Unsafe (shared JVM heap) |
| Supervision | `class : Supervisor` | `supervisor` behaviour | `SupervisorStrategy` |
| Testing | Direct mode (no threads) | eunit | TestKit |
| Dependency | None (language feature) | OTP stdlib | Akka library (100+ JARs) |

## See Also

- [Actors — Language Reference](lang/actors.md) — syntax, semantics, lifecycle
- [Supervisors — Language Reference](lang/supervisors.md) — typed lifecycle cascade
- [Concurrency](lang/concurrency.md) — all concurrency primitives
- [Example: actors.zn](../examples/v3/actors.zn) — e2e test scenarios
- [Example: supervisors.zn](../examples/v3/supervisors.zn) — supervisor scenarios
- [Example: actor_project/](../examples/v3/actor_project/) — multi-file project
