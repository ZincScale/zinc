# Zinc — Supervisors

A supervisor manages actor lifecycle — starting their threads, shutting them down, and killing them. In Zinc, supervisors are classes that extend the `Supervisor` abstract base class.

## Defining a Supervisor

```zinc
class Team : Supervisor {
    init Counter c1
    init Counter c2

    init(Counter c1, Counter c2) {
        this.c1 = c1
        this.c2 = c2
    }
}
```

No special syntax — just a class with actor-typed fields. The compiler detects which fields are actors (via the type registry) and generates lifecycle methods for them.

## Constructor Injection

Supervisors follow the same constructor injection pattern as everything in Zinc. Actors are created externally and passed in:

```zinc
var a = new Counter(0)
var b = new Counter(100)
var team = new Team(a, b)
```

The supervisor receives actors — it doesn't create them. Dependencies are explicit, testable, no factory magic.

## start()

Activates all actor-typed fields — creates their mailbox (`ArrayBlockingQueue`) and starts their virtual thread:

```zinc
team.start()
// Now a.increment() goes through mailbox (async)
// Before start(), a.increment() was synchronous (direct mode)
```

The mailbox capacity is read from each actor's `mailboxCapacity()` method (default 1000, overridable).

## shutdown()

Cooperative shutdown — sets each actor's running flag to false, sends a wake-up message, and joins the thread:

```zinc
team.shutdown()
// Waits for both actors to finish processing pending messages and exit
```

## shutdown(timeoutMs)

Cooperative with escalation — waits up to the timeout, then interrupts:

```zinc
team.shutdown(5000)
// For each actor: drain + wait 5s, then interrupt if still running
```

## kill()

Brutal termination — interrupts actor threads, clears mailboxes, registers with the reaper:

```zinc
team.kill()
// All actor threads interrupted, mailboxes cleared
// Reaper monitors killed threads — System.exit(1) if they refuse to die
```

## Mixed Fields

Supervisors can have both actor fields and config fields. Only actor-typed fields get lifecycle management:

```zinc
class Pipeline : Supervisor {
    init String name
    init int maxRetries
    init Worker enricher
    init Writer writer

    init(String name, int maxRetries, Worker enricher, Writer writer) {
        this.name = name
        this.maxRetries = maxRetries
        this.enricher = enricher
        this.writer = writer
    }
}
```

`shutdown()` cascades to `enricher` and `writer`. `name` and `maxRetries` are untouched — they're not actors.

## ActorRuntime

The supervisor uses an `ActorRuntime` interface for the kill/reaper mechanism. In production, `DefaultActorRuntime` provides the reaper thread. In tests, inject a mock:

```zinc
// Production
var runtime = new DefaultActorRuntime(10000)  // 10s reaper timeout

// Testing
class MockRuntime : ActorRuntime {
    var int killCount = 0
    pub fn pendingKill(Thread t) { killCount += 1 }
}
var runtime = new MockRuntime()
```

## Type Safety

The supervisor calls lifecycle methods directly on typed fields — no reflection:

```java
// Generated Java — direct typed calls
public void shutdown() throws Exception {
    if (enricher != null && enricher._actorThread != null) {
        enricher._running = false;
        enricher._mailbox.add(() -> {});
        enricher._actorThread.join();
    }
}
```

## Nested Supervisors

Supervisors can manage other supervisors:

```zinc
class IngestGroup : Supervisor {
    init HttpWorker http
    init ParseWorker parse

    init(HttpWorker http, ParseWorker parse) {
        this.http = http
        this.parse = parse
    }
}

class EnrichGroup : Supervisor {
    init LookupWorker lookup

    init(LookupWorker lookup) {
        this.lookup = lookup
    }
}
```

## Testing

Actors are inert on construction — no threads, no mailbox. Test them directly:

```zinc
// Unit test — no supervisor, no threads
var counter = new Counter(0)
counter.increment()
counter.increment()
var n = counter.getCount()
assert n == 2

// Supervisor test — mock runtime
var mockRuntime = new MockRuntime()
var counter = new Counter(0)
var team = new Team(counter)
team.kill()
assert mockRuntime.killCount == 1
```

## Thread Lifecycle — Fully Accounted

1. **Supervisor creates thread** → supervisor holds reference via `start()`
2. **Normal processing** → errors caught in message loop, actor continues
3. **Cooperative shutdown** → supervisor drains mailbox, joins thread
4. **Kill** → supervisor interrupts, clears mailbox, registers with reaper
5. **Reaper timeout** → `System.exit(1)`

No thread exists without an owner. No thread can dangle.

## See Also

- [Actors](actors.md) — defining actors, dual-mode, state ownership
- [Concurrency](concurrency.md) — all concurrency primitives
- [Guide: Actors](../guide-actors.md) — patterns, testing, migration
- [Example: supervisors.zn](../../examples/v3/supervisors.zn) — e2e test scenarios
- [Example: actor_project/](../../examples/v3/actor_project/) — multi-file project
