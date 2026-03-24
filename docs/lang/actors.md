# Zinc — Actors

Actors are isolated concurrent units. An actor owns its state exclusively, communicates through message passing, and runs on its own virtual thread. Because state is never shared, actors can be safely killed without risk of corruption.

## Defining an Actor

Use the `actor` keyword. Define message handlers with `receive fn`:

```zinc
actor Counter {
    var int count = 0

    receive fn increment() {
        count += 1
    }

    receive fn getCount(): int {
        return count
    }
}
```

An actor starts immediately on construction:

```zinc
var counter = new Counter()
counter.increment()            // async — returns immediately
var n = counter.getCount()     // blocks until reply: 1
```

## receive fn — Message Handlers

`receive fn` defines the actor's public API. There are two forms:

### Fire-and-forget (void return)

The caller enqueues the message and returns immediately. The actor processes it later on its own thread:

```zinc
receive fn increment() {
    count += 1
}

counter.increment()  // returns immediately, doesn't wait
```

### Request-reply (non-void return)

The caller blocks until the actor processes the message and returns a result:

```zinc
receive fn getCount(): int {
    return count
}

var n = counter.getCount()  // blocks until actor replies
```

Request-reply uses `CompletableFuture` under the hood — the caller's thread is parked (cheap with virtual threads) until the actor thread completes the future.

### Parameters

Receive functions take any number of parameters:

```zinc
receive fn add(int n) {
    count += n
}

receive fn transfer(String from, String to, int amount): boolean {
    // ...
    return true
}
```

## State Ownership

All actor fields are **private** — no getters, no setters, no external access. This is enforced by the transpiler. The only way to interact with actor state is through `receive fn`:

```zinc
actor Account {
    var int balance = 0

    receive fn deposit(int amount) {
        balance += amount
    }

    receive fn getBalance(): int {
        return balance
    }
}

var account = new Account()
// account.balance          ← compile error: field is private
account.deposit(100)         // ← the only way in
var b = account.getBalance() // ← the only way out
```

This isolation guarantee is what makes actors safe to kill — no external code holds a reference to the actor's state.

## Constructors

Actors support constructors with `init`:

```zinc
actor Worker {
    init String name
    init ProcessorFn processor

    init(String name, ProcessorFn processor) {
        this.name = name
        this.processor = processor
    }

    receive fn process(FlowFile ff) {
        var result = processor.process(ff)
        // ...
    }
}

var worker = new Worker("enricher", enrichFn)
```

The constructor body runs before the actor thread starts. Dependencies are injected through the constructor — no DI framework needed.

## Private Helper Methods

Regular `fn` (without `receive`) defines private helper methods. They run on the actor thread and can only be called from within the actor:

```zinc
actor Calculator {
    var int result = 0

    receive fn compute(int a, int b): int {
        result = doAdd(a, b)
        return result
    }

    fn doAdd(int a, int b): int {
        return a + b
    }
}
```

Helpers are useful for factoring out logic shared between multiple `receive fn` handlers.

## Actor-to-Actor Messaging

Actors can hold references to other actors and send messages to them:

```zinc
actor Logger {
    receive fn log(String msg) {
        print("[LOG] {msg}")
    }
}

actor Worker {
    init Logger logger

    init(Logger logger) {
        this.logger = logger
    }

    receive fn doWork(String task) {
        logger.log("starting: {task}")
        // ... do work ...
        logger.log("finished: {task}")
    }
}

var logger = new Logger()
var worker = new Worker(logger)
worker.doWork("process order")
```

Messages between actors are always async (fire-and-forget) or blocking (request-reply) — never direct method calls. This preserves isolation.

## Implementing Interfaces

Actors can implement interfaces:

```zinc
interface Pingable {
    fn ping(): String
}

actor PingActor : Pingable {
    receive fn ping(): String {
        return "pong"
    }
}
```

## Message Ordering

Messages sent to a single actor are processed in **FIFO order** — the order they were sent:

```zinc
actor Logger {
    var String log = ""

    receive fn append(String s) {
        log = log + s
    }

    receive fn getLog(): String {
        return log
    }
}

var logger = new Logger()
logger.append("a")
logger.append("b")
logger.append("c")
Thread.sleep(100)
var result = logger.getLog()  // always "abc", never reordered
```

When multiple threads send to the same actor, messages from different senders are interleaved but each sender's messages maintain their relative order.

## Lifecycle

Every actor has three lifecycle methods, generated automatically:

### shutdown()

Cooperative shutdown. Drains pending messages, then waits for the actor thread to exit:

```zinc
counter.shutdown()  // blocks until actor finishes and exits
```

### shutdown(timeoutMs)

Cooperative with escalation. Waits up to the timeout, then interrupts:

```zinc
counter.shutdown(5000)  // wait 5s, then interrupt if still running
```

### kill()

Brutal kill. Interrupts the actor thread immediately, discards pending messages, and registers the thread with the ActorRuntime reaper:

```zinc
counter.kill()  // immediate termination
```

If the killed thread doesn't die within the reaper timeout (default 10 seconds), the ActorRuntime calls `System.exit(1)` — guaranteeing no dangling resources.

### Three system states

1. **Running** — actors processing messages normally
2. **Shutting down** — `shutdown()` called, actors draining
3. **Fatal** — killed thread refused to die → `System.exit(1)`

There is no fourth state. No silent thread leaks.

## Error Handling

If a `receive fn` throws an exception:

- **Fire-and-forget**: the exception is caught by the actor's message loop, logged to stderr, and the actor continues processing the next message (Erlang-style resilience)
- **Request-reply**: the exception is propagated to the caller via `CompletableFuture.completeExceptionally()` — the caller sees it as an `ExecutionException` wrapping the original

```zinc
actor Risky {
    receive fn mayFail(int n): int {
        if n < 0 {
            raise "negative input"
        }
        return n * 2
    }
}

var r = new Risky()
var result = r.mayFail(-1)  // throws ExecutionException wrapping "negative input"
```

The actor itself is not killed by the exception — it continues serving subsequent messages. This is the "let it crash" philosophy: individual message failures don't bring down the actor.

## What Actors Replace

Actors replace `spawn` (deprecated) for all concurrent work that needs lifecycle management:

| Before (spawn) | After (actor) |
|---|---|
| `spawn { runLoop() }` | Actor with `receive fn` |
| No error propagation | Exceptions caught/propagated |
| No lifecycle management | `shutdown()`, `kill()` |
| No safe kill | Kill is safe (state is owned) |
| Raw thread, no supervision | Supervisor manages restarts |

For short-lived parallel work (fan-out/fan-in), continue using `concurrent { }` or `parallel for` — those use `StructuredTaskScope` and are the right tool for bounded, scoped work.

## Java Transpilation

An actor transpiles to a Java class with:
- A `LinkedBlockingQueue<Runnable>` mailbox
- A virtual thread running the message loop
- `receive fn` → methods that enqueue lambdas onto the mailbox
- Request-reply → `CompletableFuture` for the return value
- Generated `shutdown()`, `shutdown(long)`, `kill()` methods

See `docs/design-zinc-concurrency.md` for full transpilation details.
