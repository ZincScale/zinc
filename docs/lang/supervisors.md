# Zinc — Supervisors

A supervisor manages the lifecycle of child actors — shutting them down and killing them as a group. Inspired by Erlang/OTP's supervision trees.

## Defining a Supervisor

A supervisor is a construct that holds actor references via constructor injection. Any field whose type is an `actor` is automatically managed — the transpiler generates typed lifecycle cascade methods.

```zinc
actor Worker {
    receive fn doWork(String task) {
        print("working on: {task}")
    }
}

supervisor Team {
    init Worker w1
    init Worker w2

    init(Worker w1, Worker w2) {
        this.w1 = w1
        this.w2 = w2
    }
}

// Create actors, then inject into supervisor
var a = new Worker()
var b = new Worker()
var team = new Team(a, b)
```

No `child` keyword needed — the supervisor knows `Worker` is an actor type and manages it automatically.

## Constructor Injection

Supervisors follow the same constructor injection pattern as everything in Zinc. Actors are created externally and passed in:

```zinc
var enricher = new Enricher(dbConn)
var writer = new Writer(outputDir)
var pipeline = new Pipeline(enricher, writer)
```

This means:
- The caller controls how actors are created
- Dependencies are explicit and visible
- Testing is easy — pass mock actors
- No factory expressions, no hidden construction

## Mixed Fields

Supervisors can have both actor fields and config fields. Only actor-typed fields get lifecycle management:

```zinc
supervisor Pipeline {
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

When `shutdown()` is called, `enricher.shutdown()` and `writer.shutdown()` are called. `name` and `maxRetries` are not actors — they're left alone.

## Lifecycle Methods

Every supervisor gets three auto-generated lifecycle methods:

### shutdown()

Cooperative shutdown — cascades `shutdown()` to all actor-typed fields:

```zinc
team.shutdown()
// Calls: w1.shutdown(), w2.shutdown()
```

### shutdown(timeoutMs)

Cooperative with escalation — tries `shutdown(timeout)` first, then `kill()`:

```zinc
team.shutdown(5000)
// For each actor field:
//   1. Try shutdown(5000) — cooperative with deadline
//   2. If that fails, kill() — brutal
```

### kill()

Brutal kill — cascades `kill()` to all actor-typed fields:

```zinc
team.kill()
// Calls: w1.kill(), w2.kill()
```

## Type Safety

The supervisor calls lifecycle methods directly on typed fields — no reflection, no `Object` casts:

```java
// Generated Java — typed, direct calls
public void shutdown() throws Exception {
    if (w1 != null) { w1.shutdown(); }
    if (w2 != null) { w2.shutdown(); }
}
```

This works because the transpiler maintains a type registry of all `actor` declarations. When it generates supervisor code, it checks each field's type against this registry.

## Nested Supervisors

Supervisors can manage other supervisors, forming a tree:

```zinc
supervisor IngestGroup {
    init HttpWorker http
    init ParseWorker parse

    init(HttpWorker http, ParseWorker parse) {
        this.http = http
        this.parse = parse
    }
}

supervisor EnrichGroup {
    init LookupWorker lookup
    init TransformWorker transform

    init(LookupWorker lookup, TransformWorker transform) {
        this.lookup = lookup
        this.transform = transform
    }
}

// Top-level wiring
var http = new HttpWorker(8080)
var parse = new ParseWorker()
var lookup = new LookupWorker(db)
var transform = new TransformWorker()

var ingest = new IngestGroup(http, parse)
var enrich = new EnrichGroup(lookup, transform)

// Shutting down ingest group shuts down http + parse
ingest.shutdown()
```

Failures are isolated — shutting down one group doesn't affect the other.

## Real-World Example: zinc-flow

```zinc
actor ProcessorWorker {
    init String name
    init ProcessorFn processor
    var ProcessorWorker next = null

    init(String name, ProcessorFn processor) {
        this.name = name
        this.processor = processor
    }

    receive fn process(FlowFile ff) {
        var result = processor.process(ff)
        routeOutput(result)
    }

    receive fn setNext(ProcessorWorker next) {
        this.next = next
    }

    fn routeOutput(ProcessorResult result) {
        match result {
            case Single(f) { if next != null { next.process(f) } }
            case Multiple(ffs) { for f in ffs { if next != null { next.process(f) } } }
            case Drop() { }
        }
    }
}

// Wiring — explicit, top-to-bottom
var enricher = new ProcessorWorker("enrich", enrichFn)
var writer = new ProcessorWorker("write", writeFn)
enricher.setNext(writer)

// Supervisor manages both
supervisor Pipeline {
    init ProcessorWorker enricher
    init ProcessorWorker writer

    init(ProcessorWorker enricher, ProcessorWorker writer) {
        this.enricher = enricher
        this.writer = writer
    }
}

var pipeline = new Pipeline(enricher, writer)
// ... on application shutdown:
pipeline.shutdown(5000)
```
