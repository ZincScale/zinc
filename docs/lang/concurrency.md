# Zinc — Concurrency

Zinc runs on Java 25 virtual threads. No async/await, no colored functions. Every function is synchronous — blocking is cheap because virtual threads unmount from carrier threads on I/O.

## spawn

Run a block on a new virtual thread. Returns a future you can join or ignore:

```zinc
// Fire-and-forget
spawn {
    sendEmail(user, "Welcome!")
}

// Get a future back
var task = spawn {
    doExpensiveWork()
}
task.join()  // block until done
print("isDone: {task.isDone()}")
```

With error supervision — the `or` handler runs in the thread if the body throws:

```zinc
var task = spawn {
    return Error("something broke")
} or {
    print("supervisor caught error")
}

// Caller can also catch on join
task.join() or {
    print("caller caught it too")
}
```

Future methods:
- `.join()` — block until the thread completes; rethrows if the task failed
- `.isDone()` — check if the thread has completed (success or failure)
- `.isFailed()` — check if the thread threw an exception

## Parallel loops with spawn

For parallel iteration, combine `for` + `spawn`:

```zinc
var mu = new Lock()
var total = 0
for item in items {
    spawn {
        var result = compute(item)
        lock mu {
            total = total + result
        }
    }
}
sleep(500)  // wait for threads to finish
```

## lock

Mutual exclusion for shared mutable state:

```zinc
var mu = new Lock()
var counter = 0

for i in 1..10 {
    spawn {
        lock mu {
            counter = counter + 1
        }
    }
}
```

Transpiles to `ReentrantLock.lock()` / `unlock()` with try-finally (Java) or `with` context manager (Python).

## Channel

Bounded producer/consumer queue for communicating between threads:

```zinc
Channel<String> ch = new Channel(10)

// Producer
spawn {
    for order in incomingOrders() {
        ch.send(order)
    }
}

// Consumer
var msg = ch.receive() or "timeout"
```

## Summary

| Primitive | Purpose |
|---|---|
| `spawn { }` | Fire a virtual thread, get a future |
| `spawn { } or { }` | Spawn with error supervision |
| `task.join()` | Block until thread completes |
| `lock mu { }` | Mutual exclusion |
| `new Channel<T>(n)` | Bounded producer/consumer queue |

### What's NOT in Zinc

- **No `async`/`await`** — virtual threads make blocking cheap. No colored functions.
- **No `synchronized`** — use `lock` (generates `ReentrantLock`).
- **No raw `Thread` API** — use `spawn`.
- **No `CompletableFuture` chaining** — use `spawn` + `.join()`.
- **No reactive streams** — virtual threads replace the need for reactive programming.
