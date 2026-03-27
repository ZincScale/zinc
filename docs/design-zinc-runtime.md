# Design: Zinc Runtime — Structured Concurrency & Supervision

> **Status**: DESIGN COMPLETE — ready for implementation
> **Scope**: `zinc_runtime.py` for Python, equivalent patterns in Java Transformer output
> **Principle**: Threads are good children — parents own lifecycle, children never outlive parents

## The Problem

Zinc's concurrency primitives (`spawn`, `parallel for`, `concurrent`, `lock`, `Channel`) need a runtime that enforces structured lifecycle management. Without it:

- Threads outlive their parent scope (orphaned threads)
- Crashes go unhandled (silent failures)
- Shutdown leaves work half-done (lost messages)
- Shared mutable state races (data corruption)

zinc-flow makes this concrete: workers are long-running threads that process items from channels. They need to be started, stopped, swapped, deleted, and inserted — all while the pipeline is running.

## The "Good Children" Contract

Every thread/worker in the system follows these rules:

1. **I know who my parent is** — the scope that created me
2. **I check for stop signals** — between work items, not blocking forever
3. **When told to stop, I finish current work then exit** — no item left half-processed
4. **If I crash, I tell my parent before dying** — parent decides what to do
5. **I never outlive my parent** — when parent shuts down, I shut down

This is the Ktor Job / Erlang supervisor / Go context.Done() pattern.

## Architecture

```
ZincScope (top-level — signal handlers, process lifecycle)
  │
  ├── PipelineScope (one per pipeline)
  │     ├── WorkerScope "add-timestamp"
  │     │     ├── state: RUNNING | STOPPING | STOPPED
  │     │     ├── inbox: ZincChannel
  │     │     ├── outbox: ZincChannel (→ next worker's inbox)
  │     │     ├── processor: fn(FlowFile) -> ProcessorResult
  │     │     ├── thread: OS thread (Python) / virtual thread (Java)
  │     │     └── stopSignal: Event
  │     │
  │     ├── WorkerScope "enrich"
  │     └── WorkerScope "file-sink"
  │
  └── on SIGTERM/SIGINT:
        → pipeline.stop()
        → wait for drain
        → exit(0)
```

## Core Types

### ZincScope

Top-level scope that owns child scopes and handles process signals.

```python
class ZincScope:
    children: list[WorkerScope]

    def spawn(fn) -> WorkerScope       # create + start a child
    def stop_all()                      # signal all children to stop
    def join_all(timeout=30)            # wait for all to finish
    def shutdown()                      # stop_all + join_all + exit
```

On `SIGTERM`/`SIGINT`, the scope calls `shutdown()`. This cascades to all children.

### WorkerScope

A single managed thread with lifecycle.

```python
class WorkerScope:
    name: str
    state: IDLE | RUNNING | STOPPING | STOPPED | FAILED
    stop_event: threading.Event
    thread: threading.Thread
    error: Exception | None

    def start()                         # spawn thread, set state=RUNNING
    def stop()                          # set stop_event, state=STOPPING
    def join(timeout=30) -> bool        # wait for thread to finish
    def is_alive() -> bool
    def swap(new_fn)                    # hot-replace the work function
```

### ZincChannel

Bounded blocking queue with close semantics and timeout receive.

```python
class ZincChannel:
    queue: Queue(maxsize=capacity)
    closed: bool

    def send(item)                      # blocks if full, raises if closed
    def receive(timeout=0.1) -> T|None  # returns None on timeout, raises on closed+empty
    def close()                         # signal no more items
    def __iter__()                      # drain: yield items until closed+empty
```

### PipelineScope

Ordered list of workers connected by channels, with lifecycle operations.

```python
class PipelineScope(ZincScope):
    workers: list[WorkerScope]          # ordered

    def add(name, processor) -> WorkerScope
    def start()                         # start all workers in order
    def stop()                          # stop all workers in reverse order
    def insert_after(name, new_worker)  # splice into pipeline
    def delete(name)                    # remove from pipeline, reconnect neighbors
    def swap(name, new_processor)       # hot-replace processor function
    def is_healthy() -> bool            # all workers running?
```

## The Worker Loop

The core of every worker — same pattern on both runtimes:

```
fn worker_loop(scope: WorkerScope):
    while not scope.stop_event.is_set():
        item = scope.inbox.receive(timeout=100ms)
        if item is None:
            continue                    # no item, check stop signal

        try:
            result = scope.processor(item)
            match result:
                case Single(ff)  -> scope.outbox.send(ff)
                case Many(ffs)   -> for ff in ffs: scope.outbox.send(ff)
                case Drop        -> pass
        catch err:
            scope.error_handler(scope.name, err)
            if scope.retries > 0:
                retry with backoff
            else:
                send to dead-letter or drop

    # After stop signal: drain remaining items
    while not scope.inbox.is_empty():
        item = scope.inbox.receive(timeout=0)
        if item: process(item)

    scope.state = STOPPED
```

**Key design**: `inbox.receive(timeout=100ms)` — the worker never blocks forever. It wakes up every 100ms to check `stop_event`. This is how the parent can stop the child without killing the thread.

## Lifecycle Operations

### Start

```
worker.start()
  → set state = RUNNING
  → spawn thread with worker_loop
  → register with parent scope
```

### Stop (graceful)

```
worker.stop()
  → set stop_event = true
  → worker loop sees signal on next timeout
  → finishes current item
  → drains remaining inbox items
  → sets state = STOPPED
  → parent's join() returns
```

### Swap (hot-replace processor)

```
worker.swap(new_processor)
  → acquire lock on worker.processor
  → worker.processor = new_processor
  → release lock
  // Worker loop picks up new processor on next iteration
  // No stop/start — channel stays connected, no items lost
```

### Delete (remove from pipeline)

```
pipeline.delete("enrich")
  → stop the worker (graceful, waits for drain)
  → reconnect: prev.outbox → next.inbox
  → remove worker from pipeline list
```

### Insert (splice into pipeline)

```
pipeline.insert_after("add-timestamp", new_worker)
  → new_channel = ZincChannel(capacity)
  → new_worker.inbox = new_channel
  → new_worker.outbox = next_worker.inbox
  → prev_worker.outbox = new_channel
  → start new_worker
```

### Pipeline shutdown

```
pipeline.shutdown()
  → for each worker in REVERSE order:
      worker.stop()
  → for each worker:
      worker.join(timeout=30s)
  → if any worker didn't stop:
      log warning, force-kill
  → close all channels
```

**Reverse order**: stop the sink first so upstream workers' sends don't block on a full channel whose consumer is gone.

## Runtime Mapping

| Concept | Java (virtual threads) | Python 3.14t (OS threads) |
|---|---|---|
| Worker thread | `Thread.ofVirtual().start(loop)` | `threading.Thread(target=loop, daemon=False)` |
| Stop signal | `AtomicBoolean` or `volatile boolean` | `threading.Event` |
| Channel | `ArrayBlockingQueue<T>` | `queue.Queue(maxsize=N)` |
| Receive with timeout | `queue.poll(100, MILLISECONDS)` | `queue.get(timeout=0.1)` with `queue.Empty` catch |
| Lock (for swap) | `ReentrantLock` → Zinc `lock mu {}` | `threading.Lock` → `with mu:` |
| Join with timeout | `thread.join(Duration.ofSeconds(30))` | `thread.join(timeout=30)` |
| Signal handler | `Runtime.addShutdownHook(Thread)` | `signal.signal(SIGTERM, handler)` |
| Closed channel | Custom wrapper with `AtomicBoolean closed` | Custom wrapper with `closed` flag |

## Integration with Zinc Language

### Zinc source (target-agnostic)

```zinc
// Define a pipeline
var pipeline = new Pipeline()
pipeline.add("add-timestamp", (ff) -> {
    return new Single(ff.withAttribute("processed", "true"))
})
pipeline.add("file-sink", new FileSink("/tmp/out"))
pipeline.start()

// HTTP endpoint feeds the pipeline
app.post("/flow", (ctx) -> {
    var ff = new FlowFile(ctx.body())
    pipeline.send(ff)
    ctx.result("accepted")
})

// Graceful shutdown
onShutdown {
    pipeline.stop()
    app.stop()
}
```

### Java output

```java
var pipeline = new Pipeline();
pipeline.add("add-timestamp", ff ->
    new Single(ff.withAttribute("processed", "true")));
pipeline.add("file-sink", new FileSink("/tmp/out"));
pipeline.start();

// Shutdown hook
Runtime.getRuntime().addShutdownHook(Thread.ofVirtual().unstarted(() -> {
    pipeline.stop();
    app.stop();
}));
```

### Python output

```python
pipeline = Pipeline()
pipeline.add("add-timestamp", lambda ff:
    Single(ff.with_attribute("processed", "true")))
pipeline.add("file-sink", FileSink("/tmp/out"))
pipeline.start()

# Shutdown handler
def _shutdown(sig, frame):
    pipeline.stop()
    app.stop()
    sys.exit(0)
signal.signal(signal.SIGTERM, _shutdown)
signal.signal(signal.SIGINT, _shutdown)
```

## Error Handling & Supervision

### Worker crash

When a worker's processor throws:

1. **Error handler fires** — `errorHandler.onError(name, err)` — log, metric, alert
2. **Retry if configured** — `maxRetries` with exponential backoff
3. **Dead-letter if retries exhausted** — send failed item to dead-letter channel
4. **Worker continues** — it doesn't die, it processes the next item

### Pipeline-level failure

If a worker crashes in a way that the error handler can't recover:

1. Worker state → `FAILED`
2. Parent scope detects via `is_healthy()` check
3. Options: restart worker, stop pipeline, alert operator

### Supervision strategies (future)

| Strategy | Behavior |
|---|---|
| `one_for_one` | Restart only the failed worker (default) |
| `all_for_one` | Stop all workers, restart all |
| `rest_for_one` | Stop failed + all downstream, restart |

This maps to Erlang/OTP supervision strategies. Not needed for v1 but the architecture supports it.

## Implementation Plan

### Phase 1: zinc_runtime.py core

1. `ZincChannel` — Queue wrapper with close + timeout receive + iteration
2. `WorkerScope` — thread lifecycle (start/stop/join/swap) with stop_event
3. `ZincScope` — child tracking + signal handlers
4. `sleep()` — `time.sleep(ms / 1000)` wrapper

### Phase 2: PythonEmitter concurrency

5. `spawn {}` → `scope.spawn(lambda: body)` using WorkerScope
6. `parallel for` → ThreadPoolExecutor with ZincScope tracking
7. `concurrent {}` → multiple spawns + join_all
8. `lock mu {}` → `with mu:` (already done)
9. `Channel<T>` → `ZincChannel(maxsize=N)`
10. `sleep(ms)` → `zinc_sleep(ms)`

### Phase 3: Pipeline integration

11. `PipelineScope` — ordered workers with channel wiring
12. `insert_after` / `delete` / `swap` operations
13. Graceful shutdown via signal handlers
14. Port zinc-flow to use the runtime

### Phase 4: Java runtime alignment

15. Ensure Java Transformer output follows same lifecycle patterns
16. Shutdown hooks for Java pipelines
17. Channel close semantics for Java (ArrayBlockingQueue wrapper)

## References

- [Ktor Structured Concurrency](https://ktor.io/docs/coroutines.html) — Job hierarchy, parent-child lifecycle
- [Java StructuredTaskScope](https://docs.oracle.com/en/java/javase/25/docs/api/java.base/java/util/concurrent/StructuredTaskScope.html) — JEP 453
- [Erlang/OTP Supervisors](https://www.erlang.org/doc/design_principles/sup_princ.html) — supervision trees
- [Go context.Context](https://pkg.go.dev/context) — cancellation propagation
- [Python threading](https://docs.python.org/3.14/library/threading.html) — Event, Thread, Lock
