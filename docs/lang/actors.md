# Zinc — Actors

Actors are isolated concurrent units. An actor owns its state exclusively, communicates through message passing, and can be safely killed because no external code accesses its state.

In Zinc, actors are classes that extend the `Actor` abstract base class. No special keyword — just inheritance.

## Defining an Actor

```zinc
class Counter : Actor {
    var int count = 0

    pub fn increment() {
        count += 1
    }

    pub fn getCount(): int {
        return count
    }
}
```

- `class Counter : Actor` — extends Actor, gets actor behavior
- `pub fn` — message handler (dual-mode: direct in test, mailbox when supervised)
- `fn` — private helper (always runs directly on the actor thread)

## Dual-Mode: Direct and Supervised

Actor methods work in two modes depending on whether a supervisor has started the actor:

### Direct mode (testing)

Without a supervisor, actor methods execute synchronously — just like regular class methods:

```zinc
var counter = new Counter(0)
counter.increment()            // synchronous, same thread
counter.increment()
var n = counter.getCount()     // returns 2 immediately
```

No threads, no mailbox, no async. Perfect for unit testing.

### Supervised mode (production)

When a supervisor calls `start()`, actor methods route through the mailbox:

```zinc
var counter = new Counter(0)
var sup = new Pipeline(counter)
sup.start()                     // activates counter — mailbox + thread created

counter.increment()             // async — enqueued to mailbox
var n = counter.getCount()      // blocks until actor processes and replies
```

The supervisor owns the thread lifecycle. See [Supervisors](supervisors.md).

## Fire-and-Forget vs Request-Reply

### Fire-and-forget (void return)

The caller enqueues the message and returns immediately:

```zinc
pub fn increment() {
    count += 1
}

counter.increment()  // returns immediately in supervised mode
```

### Request-reply (non-void return)

The caller blocks until the actor processes the message and returns a result:

```zinc
pub fn getCount(): int {
    return count
}

var n = counter.getCount()  // blocks until reply
```

Uses `CompletableFuture` under the hood — the caller's virtual thread parks cheaply until the actor responds.

## State Ownership

All actor fields are **private** — no getters, no setters, no external access. The transpiler enforces this. The only way to interact with actor state is through `pub fn`:

```zinc
class Account : Actor {
    var int balance = 0

    pub fn deposit(int amount) {
        balance += amount
    }

    pub fn getBalance(): int {
        return balance
    }
}

var account = new Account()
// account.balance          ← not accessible
account.deposit(100)         // the only way in
var b = account.getBalance() // the only way out
```

## Constructors

```zinc
class Worker : Actor {
    init String name
    init int maxRetries

    init(String name, int maxRetries) {
        this.name = name
        this.maxRetries = maxRetries
    }

    pub fn process(String task): String {
        return "{name}: processed {task}"
    }
}

var worker = new Worker("enricher", 3)
```

The constructor runs immediately — it sets up state. The actor thread starts later when a supervisor calls `start()`.

## Private Helper Methods

Regular `fn` (without `pub`) are private helpers. They run on the actor thread when called from within a `pub fn`:

```zinc
class Calculator : Actor {
    pub fn compute(int a, int b): int {
        return doAdd(a, b)
    }

    fn doAdd(int a, int b): int {
        return a + b
    }
}
```

## Actor-to-Actor Messaging

Actors can hold references to other actors and send messages:

```zinc
class Logger : Actor {
    pub fn log(String msg) {
        print("[LOG] {msg}")
    }
}

class Worker : Actor {
    init Logger logger

    init(Logger logger) {
        this.logger = logger
    }

    pub fn doWork(String task) {
        logger.log("starting: {task}")
        // ... work ...
        logger.log("finished: {task}")
    }
}
```

## Overriding Mailbox Capacity

The default mailbox size is 1000. Override the `mailboxCapacity()` method to change it:

```zinc
class HighThroughput : Actor {
    override fn mailboxCapacity(): int {
        return 50000
    }

    pub fn process(String msg) {
        // handles high volume
    }
}
```

The supervisor reads this value when creating the actor's mailbox in `start()`.

## Error Handling

In supervised mode:

- **Fire-and-forget**: exceptions are caught by the actor's message loop, logged, and the actor continues processing. One bad message doesn't kill the actor.
- **Request-reply**: exceptions are propagated to the caller via `CompletableFuture` — the caller sees the original exception.

In direct mode, exceptions propagate normally (no mailbox wrapping).

## Message Ordering

Messages to a single actor are processed in **FIFO order**. When multiple threads send to the same actor, each sender's messages maintain their relative order.

## What Actors Replace

Actors replace `spawn` (deprecated) for all concurrent work needing lifecycle management:

| Before (spawn) | After (Actor) |
|---|---|
| `spawn { runLoop() }` | `class Worker : Actor { pub fn process() }` |
| No error handling | Exceptions caught/propagated |
| No lifecycle | Supervisor manages start/shutdown/kill |
| No safe kill | Kill is safe (state is owned) |
| Raw thread | Virtual thread managed by supervisor |

For short-lived parallel work, continue using `concurrent { }` or `parallel for`.

## See Also

- [Supervisors](supervisors.md) — managing actor lifecycle with start/shutdown/kill
- [Concurrency](concurrency.md) — all concurrency primitives
- [Guide: Actors](../guide-actors.md) — patterns, testing, migration
- [Example: actors.zn](../../examples/v3/actors.zn) — e2e test scenarios
- [Example: actor_project/](../../examples/v3/actor_project/) — multi-file project
