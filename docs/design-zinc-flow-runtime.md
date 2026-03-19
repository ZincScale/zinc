# Design: Zinc Flow Runtime — Architecture & Implementation

> **Status**: DESIGN — architecture validated by benchmarks, ready for Phase 1 implementation

## Architecture Decision

**Processor Groups** — the middle ground between NiFi's monolith and DeltaFi's container-per-processor.

```
NiFi:      Everything in one JVM            → can't scale pieces independently
DeltaFi:   Every processor in a container   → IPC overhead kills small processors
Zinc Flow: YOU choose the group boundaries  → group fast processors, isolate slow ones
```

### The Model

- A **Processor Group** is the unit of deployment (one pod, one process)
- Within a group: **threads + in-memory queues** (zero serialization, pointer swaps, 100K+ msgs/sec)
- Between groups: **NATS JetStream** (serialization only at group boundaries)
- **Local dev**: all groups collapse into one process, everything is in-memory
- **K8s**: each group becomes a Deployment with its own replica count

```
Pod 1 (ingest-group, 1 replica):
  [http-source] -> [parse] -> [validate]     ← threads, queue.Queue
                                    |
                             NATS JetStream   ← cross-group boundary
                                    |
Pod 2 (enrich-group, 10 replicas):            ← the slow one, scaled out
  [enrich] -> [lookup]                        ← threads, queue.Queue
                  |
             NATS JetStream
                  |
Pod 3 (output-group, 2 replicas):
  [format] -> [kafka-sink]                    ← threads, queue.Queue
```

### Why This Works (Benchmark Evidence)

| Scenario | Python 3.14t | Notes |
|----------|-------------|-------|
| Queue 100KB FlowFiles | 100K msgs/s (asyncio), 142K msgs/s (4-thread fanout) | Faster than .NET due to refcounted bytes |
| Queue 1MB FlowFiles | 30K msgs/s (4-thread fanout) = **30 GB/s** | Free-threading delivers real parallelism |
| Queue 1KB FlowFiles | 88-188K msgs/s | Adequate for control/routing messages |
| HTTP ingress 100KB | 4-6K msgs/s | Sufficient for pipeline source/sink |

NiFi typically handles 10K-100K msgs/sec. Python 3.14t exceeds this at all FlowFile sizes.

### Thread-Level Manageability

Threads give you everything process isolation gives you for management, because the **queue is the decoupling boundary**:

```zinc
class ProcessorGroup {
    var name: str
    var workers: dict[str, ProcessorWorker] = {}

    // Stop a processor — thread exits, items accumulate in its queue
    fn stop_processor(proc_name: str) {
        workers[proc_name].stop()
    }

    // Start — new thread picks up backlog from queue
    fn start_processor(proc_name: str) {
        workers[proc_name].start()
    }

    // Swap — stop, replace function, start. Queue bridges the gap.
    fn swap_processor(proc_name: str, new_fn: Callable) {
        var worker = workers[proc_name]
        worker.stop()
        worker.thread.join()
        worker.fn = new_fn
        worker.start()
    }

    // Scale — multiple threads consuming from same queue
    fn scale_processor(proc_name: str, replicas: int) {
        workers[proc_name].set_replicas(replicas)
    }

    // Update config — swap function with new closure capturing new config
    fn update_config(proc_name: str, config: dict) {
        var worker = workers[proc_name]
        worker.config = config
        // Processor reads config on each invocation
    }
}
```

The only thing you lose vs process isolation is crash protection — one native extension segfault takes down the group. But Python exceptions are caught by the worker loop and routed to DLQ.

---

## Core Types

### FlowFile

```zinc
data FlowFile {
    id: str
    attributes: dict[str, str]
    content: bytes
    timestamp: float
    provenance: list[str] = []

    fn with_content(new_content: bytes): FlowFile {
        return FlowFile(
            id=id,
            attributes=attributes,
            content=new_content,
            timestamp=time.time(),
            provenance=provenance + ["transformed"],
        )
    }

    fn with_attribute(key: str, value: str): FlowFile {
        var attrs = dict(attributes)
        attrs[key] = value
        return FlowFile(
            id=id,
            attributes=attrs,
            content=content,
            timestamp=time.time(),
            provenance=provenance,
        )
    }

    fn content_size(): int = len(content)
}
```

FlowFiles are **immutable** — processors return new FlowFiles. This aligns with Python's refcounted `bytes` advantage (no copies, just pointer swaps through queues).

### Content Reference (Phase 2 — Large Payloads)

Not needed in Phase 1. The benchmarks showed Python handles 100KB-1MB blobs through queues efficiently. Content references are for the Phase 2 distributed case where FlowFiles cross pod boundaries and you don't want to serialize 10MB through NATS.

```zinc
data ContentRef {
    store: str       // "file", "nfs", "s3-compat"
    key: str         // content identifier
    size: int        // byte length
    hash: str        // sha256 for dedup/integrity
}
```

When a FlowFile crosses a group boundary, the runtime decides:
- **< 256KB**: serialize inline through NATS message (fast enough)
- **>= 256KB**: store content on shared filesystem, pass ContentRef through NATS

Content store options by deployment:
- **Local dev**: local filesystem (`/tmp/zinc-flow/content/`)
- **Single node prod**: local filesystem
- **Multi-node K8s**: shared filesystem via NFS or K8s `ReadWriteMany` PVC (EFS, Azure Files, etc.)
- **Future**: pluggable interface for S3-compatible stores (SeaweedFS, RustFS, Garage) when needed

**Explicitly not supported**: Rook/Ceph. Too complex, too heavy, too many failure modes for what is essentially a temporary blob cache.

NiFi uses local filesystem for its content repository and it works at scale. Keep it simple.

---

## Processor Model

### Processor Definition

A processor is a Zinc function decorated with `@flow.processor`:

```zinc
import flow

@flow.processor
fn enrich_order(ff: FlowFile): FlowFile {
    var data = json.loads(ff.content)
    data["enriched_at"] = datetime.now().isoformat()
    data["region"] = lookup_region(data["zip_code"])
    return ff.with_content(json.dumps(data).encode())
}
```

Processors can return:
- **One FlowFile** — 1:1 transform
- **A list of FlowFiles** — 1:N split/fan-out
- **None** — filter/drop
- **A tuple of (route, FlowFile)** — routed output

```zinc
// 1:N — split a batch into individual records
@flow.processor
fn split_batch(ff: FlowFile): list[FlowFile] {
    var records = json.loads(ff.content)
    return records.Select((r, i) ->
        FlowFile(
            id=uuid4().hex,
            attributes=ff.attributes | {"record.index": str(i)},
            content=json.dumps(r).encode(),
            timestamp=time.time(),
        )
    )
}

// Filter — return none to drop
@flow.processor
fn filter_valid(ff: FlowFile): FlowFile? {
    var data = json.loads(ff.content)
    if data.get("status") == "invalid" {
        return none
    }
    return ff
}

// Routing — return tagged outputs
@flow.processor(outputs=["success", "failure", "retry"])
fn validate_order(ff: FlowFile): tuple[str, FlowFile] {
    var data = json.loads(ff.content)
    if not data.get("order_id") {
        return "failure", ff.with_attribute("error", "missing order_id")
    }
    if data.get("amount", 0) <= 0 {
        return "retry", ff.with_attribute("error", "invalid amount")
    }
    return "success", ff
}
```

### Processor Lifecycle

Each processor runs as a **worker loop** consuming from its input queue:

```zinc
class ProcessorWorker {
    var name: str
    var fn: Callable
    var input_queue: FlowQueue
    var output_queues: dict[str, FlowQueue]
    var state: str = "stopped"  // stopped, running, paused
    var threads: list[threading.Thread] = []
    var config: dict = {}
    var stats: ProcessorStats

    fn start() {
        state = "running"
        if len(threads) == 0 {
            _add_thread()
        }
    }

    fn stop() {
        state = "stopped"
        for t in threads {
            t.join(timeout=5.0)
        }
        threads.clear()
    }

    fn set_replicas(n: int) {
        while len(threads) < n {
            _add_thread()
        }
        while len(threads) > n {
            // Remove last thread — it will exit on next loop iteration
            // (all threads share the state flag)
            threads.pop()
        }
    }

    fn _add_thread() {
        var t = threading.Thread(target=_run_loop, name="{name}-{len(threads)}", daemon=true)
        t.start()
        threads.append(t)
    }

    fn _run_loop() {
        while state == "running" {
            var ff = input_queue.get(timeout=0.1)
            if ff is none {
                continue
            }

            var start_time = time.perf_counter()
            var result = _execute(ff) or {
                _handle_failure(ff, err)
                continue
            }

            var elapsed = time.perf_counter() - start_time
            stats.record(elapsed, ff.content_size())
            _route_output(result)
        }
    }

    fn _execute(ff: FlowFile): Any {
        for attempt in range(config.get("max_retries", 0) + 1) {
            var result = fn(ff) or {
                if attempt < config.get("max_retries", 0) {
                    var delay = config.get("retry_delay", 1.0) * (2 ** attempt)
                    time.sleep(delay)
                    continue
                }
                raise err
            }
            return result
        }
    }

    fn _route_output(result: Any) {
        match result {
            FlowFile ff -> {
                output_queues["default"].put(ff)
            }
            list[FlowFile] ffs -> {
                for ff in ffs {
                    output_queues["default"].put(ff)
                }
            }
            tuple[str, FlowFile] (route, ff) -> {
                output_queues.get(route, output_queues["default"]).put(ff)
            }
            none -> {
                // Dropped
            }
        }
    }

    fn _handle_failure(ff: FlowFile, err: Exception) {
        stats.record_error()
        if "dead_letter" in output_queues {
            var dlq_ff = ff.with_attribute("error", str(err))
                          .with_attribute("error.processor", name)
                          .with_attribute("error.timestamp", datetime.now().isoformat())
            output_queues["dead_letter"].put(dlq_ff)
        }
    }
}
```

---

## Pipeline Definition

### Pipeline DSL

```zinc
import flow

@flow.processor
fn parse_json(ff: FlowFile): FlowFile { ... }

@flow.processor
fn validate(ff: FlowFile): FlowFile { ... }

@flow.processor
fn enrich(ff: FlowFile): FlowFile { ... }

@flow.processor
fn format_output(ff: FlowFile): FlowFile { ... }

// --- Pipeline with processor groups ---

var pipeline = flow.Pipeline("order_processing")

// Group 1: ingest (lightweight, 1 replica)
var ingest = flow.Group("ingest") {
    flow.source.http(port=8080)
    -> parse_json
    -> validate
}

// Group 2: enrichment (slow, needs scaling)
var enrich_group = flow.Group("enrich", replicas=10) {
    enrich
}

// Group 3: output
var output = flow.Group("output", replicas=2) {
    format_output
    -> flow.sink.kafka("processed-orders")
}

// Connect groups (these become distributed queues)
pipeline.connect(ingest, enrich_group)
pipeline.connect(enrich_group, output)

pipeline.run()
```

### Local Dev Mode

In local dev, group boundaries are ignored — everything runs as threads in one process with in-memory queues:

```bash
# Local — all groups in one process, in-memory queues
zinc flow run pipeline.zn

# K8s — each group is a Deployment, NATS JetStream between groups
zinc flow deploy pipeline.zn --nats nats://nats:4222
```

### Pipeline Object

```zinc
class Pipeline {
    var name: str
    var groups: dict[str, ProcessorGroup] = {}
    var group_connections: list[GroupConnection] = []
    var mode: str = "local"  // "local" or "distributed"

    fn connect(source: ProcessorGroup, target: ProcessorGroup) {
        group_connections.append(GroupConnection(source=source.name, target=target.name))
    }

    fn run() {
        if mode == "local" {
            _run_local()
        } else {
            _run_distributed()
        }
    }

    fn _run_local() {
        // Collapse all groups into one process
        // All connections use in-memory queues
        print("Pipeline '{name}' starting (local mode)")
        for group in groups.values() {
            for worker in group.workers.values() {
                worker.start()
            }
        }
        _wait_for_shutdown()
    }

    fn _run_distributed() {
        // Only start this process's group
        // Cross-group connections use NATS JetStream
        var my_group = os.environ.get("ZINC_FLOW_GROUP")
        if my_group {
            groups[my_group].start()
        }
    }

    fn status(): dict {
        var result = {}
        for group_name, group in groups.items() {
            for proc_name, worker in group.workers.items() {
                result["{group_name}/{proc_name}"] = {
                    "state": worker.state,
                    "queue_depth": worker.input_queue.qsize(),
                    "processed": worker.stats.count,
                    "errors": worker.stats.errors,
                    "avg_ms": worker.stats.avg_latency_ms,
                    "msgs_per_sec": worker.stats.throughput,
                    "replicas": len(worker.threads),
                }
            }
        }
        return result
    }
}
```

---

## Queue Abstraction

The queue backend is pluggable — same interface, different implementations:

```zinc
class FlowQueue {
    fn put(ff: FlowFile) { ... }
    fn get(timeout: float = none): FlowFile? { ... }
    fn qsize(): int { ... }
}

// In-memory (within a group) — benchmarked at 88-188K msgs/sec
class LocalQueue(FlowQueue) {
    var q: queue.Queue

    fn init(maxsize: int = 10_000) {
        q = queue.Queue(maxsize=maxsize)
    }

    fn put(ff: FlowFile) {
        q.put(ff)  // blocks when full → natural back-pressure
    }

    fn get(timeout: float = none): FlowFile? {
        return q.get(timeout=timeout) or { return none }
    }
}

// NATS JetStream (between groups) — cross-pod communication
class NatsQueue(FlowQueue) {
    var nc: nats.Connection
    var js: nats.JetStream
    var stream: str
    var subject: str
    var consumer_name: str
    var sub: nats.Subscription?

    fn init(nats_url: str, stream: str, subject: str, consumer_name: str) {
        nc = nats.connect(nats_url)
        js = nc.jetstream()

        // Create stream if not exists
        js.add_stream(name=stream, subjects=[subject]) or { }

        // Create durable consumer (competing consumers for scaling)
        sub = js.pull_subscribe(subject, durable=consumer_name)
    }

    var content_store: ContentStore?  // for large payloads

    fn put(ff: FlowFile) {
        if ff.content_size() < 256 * 1024 or content_store is none {
            // Small: inline in NATS message
            var data = serialize_flowfile(ff)
            js.publish(subject, data)
        } else {
            // Large: store content on shared filesystem, pass reference
            var key = "{ff.id}-content"
            content_store.put(key, ff.content)
            var light_ff = ff.with_content(key.encode())
            var data = serialize_flowfile(light_ff)
            js.publish(subject, data, headers={"Content-Ref": "true"})
        }
    }

    fn get(timeout: float = none): FlowFile? {
        var msgs = sub.fetch(1, timeout=timeout) or { return none }
        if len(msgs) == 0 {
            return none
        }
        var msg = msgs[0]
        var ff = deserialize_flowfile(msg.data)

        if msg.headers and msg.headers.get("Content-Ref") == "true" {
            // Retrieve large content from shared filesystem
            var key = ff.content.decode()
            ff = ff.with_content(content_store.get(key))
            content_store.delete(key)  // consumed, clean up
        }

        msg.ack()
        return ff
    }
}
```

---

## Back-Pressure

Back-pressure propagates naturally via bounded queues:

```zinc
data BackPressureConfig {
    queue_depth_warn: int = 5_000      // log warning
    queue_depth_throttle: int = 8_000  // slow upstream
    queue_depth_block: int = 10_000    // block upstream put() call
}
```

- **Within a group**: `queue.Queue(maxsize=N)` — `put()` blocks when full. Upstream thread sleeps until downstream catches up. Zero overhead.
- **Between groups**: NATS JetStream stream limits. Configure max messages or max bytes on the stream. When limit is reached, NATS rejects publishes — upstream group backs off.

No spill-to-disk in Phase 1. The bounded queue is sufficient — it's exactly how NiFi does it.

---

## Routing Model

Routing determines which queue a FlowFile goes to after a processor finishes with it. Two levels:

### Within a Group — Direct Queue Routing

Inside a group, routing is local — the worker loop pushes to the right output queue based on the processor's return value. Fast, no network, no serialization.

```zinc
// Processor returns a route tag
@flow.processor(outputs=["valid", "invalid", "retry"])
fn validate(ff: FlowFile): tuple[str, FlowFile] {
    if not ff.attributes.get("order_id") {
        return "invalid", ff
    }
    return "valid", ff
}
// Worker loop pushes to output_queues["valid"] or output_queues["invalid"]
```

This handles static routing within a group. No routing table needed — the wiring is defined when you `group.connect()`.

### Between Groups — NATS Subject-Based Routing

Cross-group routing uses NATS subjects. This is where NATS shines — subjects are hierarchical and support wildcards, giving us content-based routing for free.

```
Subject hierarchy:
  zinc-flow.{pipeline}.{group}.{route}

Examples:
  zinc-flow.orders.enrich.high-priority
  zinc-flow.orders.enrich.default
  zinc-flow.orders.archive.csv
  zinc-flow.orders.archive.json
```

A **routing table** in the state store maps FlowFile attributes to NATS subjects. This table is updatable in production without redeploying processors.

```zinc
data RoutingRule {
    name: str
    condition: str        // Zinc expression evaluated against FlowFile
    target_subject: str   // NATS subject to publish to
    priority: int = 0     // higher priority rules evaluated first
}

// Example routing table (stored in state store, editable via API/TUI)
var rules = [
    RoutingRule(
        name="high-priority",
        condition='ff.attributes["priority"] == "high"',
        target_subject="zinc-flow.orders.enrich.high",
        priority=10,
    ),
    RoutingRule(
        name="csv-files",
        condition='ff.attributes["mime.type"] == "text/csv"',
        target_subject="zinc-flow.orders.csv-processing.default",
        priority=5,
    ),
    RoutingRule(
        name="default",
        condition="true",
        target_subject="zinc-flow.orders.enrich.default",
        priority=0,
    ),
]
```

Consumer groups subscribe with wildcards for natural scaling:
- `enrich-group` subscribes to `zinc-flow.orders.enrich.>` — receives all enrich traffic
- All 10 replicas compete for messages via NATS consumer groups
- Adding a new route just means publishing to a new subject — consumers pick it up automatically if the wildcard matches

### Route Evaluation

The runtime evaluates routing rules when a FlowFile exits a group's last processor:

```zinc
class Router {
    var rules: list[RoutingRule]

    fn route(ff: FlowFile): str {
        // Rules sorted by priority (highest first)
        for rule in rules {
            if evaluate_condition(rule.condition, ff) {
                return rule.target_subject
            }
        }
        return default_subject
    }

    fn evaluate_condition(condition: str, ff: FlowFile): bool {
        // Condition is a Zinc expression — validated at save time by transpiler
        // Evaluated at runtime against the FlowFile
        return eval_zinc_expr(condition, {"ff": ff})
    }
}
```

### Dynamic Routing Changes

Routing rules live in the state store and can be changed in production:

```bash
# Add a new routing rule — takes effect immediately
zinc flow route add --name "eu-traffic" \
    --condition 'ff.attributes["region"] == "eu"' \
    --target "zinc-flow.orders.eu-processing.default" \
    --priority 8

# List current rules
zinc flow routes
  [10] high-priority  → zinc-flow.orders.enrich.high
  [ 8] eu-traffic     → zinc-flow.orders.eu-processing.default
  [ 5] csv-files      → zinc-flow.orders.csv-processing.default
  [ 0] default        → zinc-flow.orders.enrich.default

# Remove a rule
zinc flow route remove eu-traffic

# All changes are versioned in the state store — rollback works
```

---

## Cross-Cutting Concerns

Three categories of cross-cutting concerns that span all processors:

### 1. Shared Services

Processors often need shared infrastructure — database connections, HTTP clients, SSL contexts. Instead of each processor managing its own, a service registry provides shared instances.

```zinc
class ServiceRegistry {
    var services: dict[str, Any] = {}

    fn register(name: str, service: Any) {
        services[name] = service
    }

    fn get(name: str): Any {
        return services[name]
    }
}

// Register shared services at pipeline startup
var services = flow.ServiceRegistry()
services.register("db", PostgresPool(url=secrets.get("DB_URL"), max_connections=10))
services.register("http", HttpClient(timeout=30, ssl_context=secrets.get("TLS_CERT")))
services.register("cache", RedisClient(url=secrets.get("REDIS_URL")))
```

Processors access services via their context:

```zinc
@flow.processor
fn enrich(ff: FlowFile, ctx: ProcessorContext): FlowFile {
    var db = ctx.service("db")
    var result = db.query("SELECT region FROM customers WHERE id = ?", ff.attributes["customer_id"])
    return ff.with_attribute("region", result["region"])
}
```

Services are shared across all processors in a group (same process, same connection pool). Between groups, each group has its own service instances.

### 2. Secrets Management

Processors need credentials — API keys, database passwords, TLS certs. These must never be hardcoded or stored in the pipeline definition.

A pluggable **secrets provider** resolves secret references at runtime:

```zinc
class SecretsProvider {
    fn get(key: str): str { ... }
}

// Implementations
class EnvSecrets(SecretsProvider) {
    // Reads from environment variables — simplest, K8s-native
    fn get(key: str): str {
        return os.environ[key]
    }
}

class FileSecrets(SecretsProvider) {
    // Reads from files — mounted K8s secrets
    var base_path: str = "/var/run/secrets"

    fn get(key: str): str {
        return open(os.path.join(base_path, key)).read().strip()
    }
}

class VaultSecrets(SecretsProvider) {
    // Reads from HashiCorp Vault
    var client: VaultClient

    fn get(key: str): str {
        return client.read("secret/data/zinc-flow/{key}")["data"]["value"]
    }
}
```

Processor config references secrets with `${secrets.KEY}` syntax, resolved at startup:

```zinc
@flow.processor(config={
    "api_key": "${secrets.ENRICHMENT_API_KEY}",
    "db_url": "${secrets.DB_URL}",
})
fn enrich(ff: FlowFile, ctx: ProcessorContext): FlowFile {
    var key = ctx.config["api_key"]   // resolved from secrets provider
    var result = http_get("https://api.example.com/enrich", headers={"Authorization": key})
    return ff.with_content(result)
}
```

Provider chain: try Vault first, fall back to file secrets, fall back to env vars. Configurable per deployment.

```bash
# Local dev — env vars
export ENRICHMENT_API_KEY=dev-key-123
zinc flow run pipeline.zn

# K8s — mounted secrets
zinc flow run pipeline.zn --secrets file:///var/run/secrets

# Prod — Vault
zinc flow run pipeline.zn --secrets vault://vault:8200/secret/zinc-flow
```

### 3. Observability (Logging, Metrics, Telemetry)

Observability is **automatic** — the runtime handles it. Processors don't need to add logging or metrics code. The worker loop instruments everything.

#### Logging

Every FlowFile enter/exit is logged automatically by the worker loop:

```zinc
// Inside ProcessorWorker._run_loop() — automatic, not user code
fn _run_loop() {
    while state == "running" {
        var ff = input_queue.get(timeout=0.1)
        if ff is none { continue }

        log.debug("processor={name} action=enter flowfile={ff.id} attrs={ff.attributes}")

        var start_time = time.perf_counter()
        var result = _execute(ff) or {
            log.error("processor={name} action=error flowfile={ff.id} error={err}")
            _handle_failure(ff, err)
            continue
        }

        var elapsed = time.perf_counter() - start_time
        log.debug("processor={name} action=exit flowfile={ff.id} elapsed_ms={elapsed*1000:.1f}")

        stats.record(elapsed, ff.content_size())
        _route_output(result)
    }
}
```

Structured logging (JSON) by default. Log level configurable per processor:

```bash
zinc flow log-level parse_json DEBUG    # verbose for one processor
zinc flow log-level enrich ERROR        # quiet for another
```

#### Metrics

Automatic Prometheus metrics emitted by the worker loop — zero processor code needed:

```
# Counters
zinc_flow_processed_total{pipeline="orders", group="main", processor="enrich"}
zinc_flow_errors_total{pipeline="orders", group="main", processor="enrich"}
zinc_flow_bytes_total{pipeline="orders", group="main", processor="enrich"}

# Gauges
zinc_flow_queue_depth{pipeline="orders", group="main", queue="enrich_input"}
zinc_flow_processor_state{pipeline="orders", group="main", processor="enrich"}  # 0=stopped, 1=running
zinc_flow_replicas{pipeline="orders", group="main", processor="enrich"}

# Histograms
zinc_flow_processing_duration_seconds{pipeline="orders", group="main", processor="enrich"}
zinc_flow_flowfile_size_bytes{pipeline="orders", group="main", processor="enrich"}
```

Exposed via `/metrics` endpoint on the management API. Plugs into existing Grafana/alerting stacks.

#### Distributed Tracing (Phase 3)

FlowFiles carry a trace context in their attributes as they move through the pipeline:

```zinc
// Automatic — runtime adds trace context to every FlowFile
ff.attributes["trace.id"] = "abc123"
ff.attributes["trace.span.id"] = "def456"
ff.attributes["trace.parent.id"] = "ghi789"
```

When a FlowFile crosses a group boundary (via NATS), the trace context propagates. OpenTelemetry exporter sends spans to Jaeger/Zipkin/etc. You can trace a single FlowFile's journey through the entire pipeline across groups and pods.

### Interceptor Model

All three concerns (services, secrets, observability) are implemented as **interceptors** on the worker loop — not as processor code. The processor function stays clean:

```zinc
// What the developer writes — clean business logic only
@flow.processor
fn enrich(ff: FlowFile, ctx: ProcessorContext): FlowFile {
    var db = ctx.service("db")
    var data = json.loads(ff.content)
    data["region"] = db.query("SELECT region FROM zip WHERE code = ?", data["zip"])
    return ff.with_content(json.dumps(data).encode())
}

// What the runtime wraps it with — automatic
//   → resolve secrets into config
//   → inject services via ctx
//   → log enter/exit
//   → emit metrics
//   → propagate trace context
//   → handle errors → DLQ
//   → record stats
```

The developer writes business logic. The runtime handles everything else.

---

## Sources and Sinks

Sources produce FlowFiles into the pipeline. Sinks consume them out. Both run on their own thread within their group.

```zinc
// HTTP source — runs its own async event loop on a thread
@flow.source
fn http_source(port: int = 8080, path: str = "/ingest") {
    fn handle_post(request) {
        var body = request.read()
        var content_type = request.headers.get("Content-Type", "")

        if content_type == "application/flowfile-v3" {
            var attrs, content, _ = unpackage_flowfile(body)
            yield FlowFile(id=uuid4().hex, attributes=attrs, content=content, timestamp=time.time())
        } else {
            yield FlowFile(
                id=uuid4().hex,
                attributes={"http.method": "POST", "http.path": request.path, "mime.type": content_type},
                content=body,
                timestamp=time.time(),
            )
        }
    }
}

// Kafka source
@flow.source
fn kafka_source(brokers: str, topic: str, group: str) {
    var consumer = KafkaConsumer(brokers, topic, group)
    for msg in consumer {
        yield FlowFile(
            id=uuid4().hex,
            attributes={"kafka.topic": msg.topic, "kafka.partition": str(msg.partition), "kafka.offset": str(msg.offset)},
            content=msg.value,
            timestamp=time.time(),
        )
    }
}

// Filesystem sink
@flow.sink
fn file_sink(base_path: str) {
    fn write(ff: FlowFile) {
        var filename = ff.attributes.get("filename", ff.id)
        var path = os.path.join(base_path, filename)
        os.makedirs(os.path.dirname(path), exist_ok=true)
        with f = open(path, "wb") {
            f.write(ff.content)
        }
    }
}

// HTTP sink
@flow.sink
fn http_sink(url: str) {
    fn send(ff: FlowFile) {
        var headers = {"Content-Type": ff.attributes.get("mime.type", "application/octet-stream")}
        for key, value in ff.attributes.items() {
            headers["X-FlowFile-{key}"] = value
        }
        httpx.post(url, content=ff.content, headers=headers)
    }
}
```

---

## Management API

REST API for runtime control. Any UI (web, TUI, CLI) is a client for this.

```zinc
class FlowAPI {
    var pipeline: Pipeline

    fn init(pipeline: Pipeline, port: int = 8081) {
        var app = aiohttp_server(port)
        app.route("GET",  "/api/pipeline",                   get_pipeline)
        app.route("GET",  "/api/groups",                     get_groups)
        app.route("GET",  "/api/processors",                 get_processors)
        app.route("POST", "/api/processors/{name}/start",    start_processor)
        app.route("POST", "/api/processors/{name}/stop",     stop_processor)
        app.route("POST", "/api/processors/{name}/scale",    scale_processor)
        app.route("POST", "/api/processors/{name}/swap",     swap_processor)
        app.route("POST", "/api/processors/{name}/config",   update_config)
        app.route("GET",  "/api/queues",                     get_queues)
        app.route("GET",  "/api/stats",                      get_stats)
        app.route("GET",  "/api/health",                     health_check)
    }
}
```

---

## CLI

```bash
# Local dev — all groups in one process
zinc flow run pipeline.zn

# Distributed — specify NATS server
zinc flow run pipeline.zn --mode distributed --nats nats://localhost:4222

# Deploy to K8s — generates Deployments per group
zinc flow deploy pipeline.zn --namespace prod --nats nats://nats:4222

# Runtime management
zinc flow status                                    # pipeline overview
zinc flow groups                                    # group status + replicas
zinc flow processors                                # per-processor stats
zinc flow processor stop enrich                     # stop a processor
zinc flow processor start enrich                    # start it
zinc flow processor scale enrich --replicas 4       # scale within group
zinc flow group scale enrich-group --replicas 10    # scale the K8s deployment
zinc flow queues                                    # queue depths
```

---

## Open Questions — Resolved

### Q1: Queue technology — Redis Streams? Kafka? NATS? Custom?

**Answer: Pluggable. NATS JetStream for messaging. Separate tools for state and content.**

- **Within groups**: always `queue.Queue` (in-memory, no choice needed)
- **Between groups**: NATS JetStream for message transport

| Concern | Tool | Why |
|---------|------|-----|
| **Messaging** (cross-group queues) | NATS JetStream | Lightweight (~20MB), consumer groups, purpose-built for messaging, K8s-native |
| **State + audit trail** (flow graph, config, history) | etcd or PostgreSQL | Strong consistency, proven revision history, read-your-writes guaranteed |
| **Large content** (FlowFiles > 256KB crossing groups) | Filesystem (local/NFS) | Zero dependencies, proven (NiFi uses filesystem too). Pluggable interface for future S3-compatible options (SeaweedFS, RustFS, Garage, etc.) |

Why not all-in-one NATS:
- NATS KV is experimental in Python client, no read-your-writes guarantee, max 64 history entries per key
- NATS Object Store has broken listing at scale (3.5+ sec per item)
- Jepsen found write loss under coordinated failures (2-min flush interval)
- Single point of failure if NATS handles messaging + state + content

Each tool does what it's best at. NATS dying doesn't lose your flow state. Filesystem content survives NATS restarts.

Why NATS JetStream for messaging specifically:
- **Purpose-built for messaging** — unlike Redis (cache first) or Kafka (distributed log first)
- **Lightweight single binary** — ~20MB, starts in milliseconds
- **Consumer groups** — competing consumers for scaling processor groups across pods
- **At-least-once AND exactly-once** — configurable per stream
- **Cloud-native** — designed for K8s, automatic clustering
- **Good Python client** — `nats-py` with async support

Redis Streams and Kafka available as pluggable alternatives for teams that already have them.

### Q2: GUI framework — Web UI vs TUI vs both?

**Answer: REST API first, TUI second, Web UI later.**

- Phase 1: CLI only (`zinc flow status/processors/queues`)
- Phase 2: REST API (the management API above) — enables any UI
- Phase 2: TUI using the REST API — terminal dashboard showing pipeline graph, queue depths, throughput. Fits the CLI-first Zinc philosophy.
- Phase 3+: Web UI if there's demand. Not a priority — most operators are comfortable with CLI/TUI, and web UIs are expensive to build and maintain.

### Q3 + Q4: Processor discovery, versioning, and hot-swap

**Answer: Two modes — static imports for dev, processor catalog for prod. Full audit trail with rollback.**

#### Dev Mode — Static Imports

For local development, processors are Zinc functions imported explicitly. Fast iteration, no infrastructure needed.

```zinc
// pipeline.zn
import flow
from processors.enrichment import enrich_order
from processors.validation import validate_order

var pipeline = flow.Pipeline("orders")
var group = flow.Group("main")
group.add_processor("validate", validate_order)
group.add_processor("enrich", enrich_order)
```

#### Prod Mode — Processor Catalog

In production, processors are registered in a **catalog** (stored in the state store — etcd or PostgreSQL). The runtime loads processors by name and version at startup, and can hot-swap them without redeployment.

This is critical for NiFi-like operations — operators need to rewire flows in production without going through a full dev/test/deploy cycle. That immediate response capability is NiFi's killer feature.

```bash
# Publish a processor to the catalog
zinc flow processor publish enrich_order --version 1.0 --package ./processors/enrichment/
zinc flow processor publish enrich_order --version 2.1 --package ./processors/enrichment_v2/

# List available processors
zinc flow processor list
  enrich_order     v1.0, v2.1
  validate_order   v1.0
  parse_json       v1.0, v1.1

# Pipeline references by name@version — resolved from catalog
zinc flow processor swap enrich --to enrich_order@2.1
```

Pipeline definition in prod references catalog entries, not imports:

```zinc
var group = flow.Group("main")
group.add_processor("validate", "validate_order@1.0")   // resolved from catalog
group.add_processor("enrich", "enrich_order@2.1")        // swappable without redeploy
```

#### Hot-Swap Mechanics

Under the hood, swap is: stop worker thread → `importlib.reload()` the new module → start worker thread. The queue bridges the gap — items accumulate during the swap, new version picks them up.

```bash
# Swap in production — zero downtime
zinc flow processor swap enrich --to enrich_order@2.2 --reason "fixing timezone bug"
```

#### Versioned Flow State + Audit Trail + Rollback

Every change to the running flow (swap, scale, config, rewire) is a new revision in the state store. Full audit trail with instant rollback.

```bash
# Audit trail — every change is recorded
zinc flow history
  Rev 49  2026-03-19 14:23  vrjoshi  swap enrich -> enrich_order@2.2 "fixing timezone bug"
  Rev 48  2026-03-19 10:15  ops-bot  scale enrich-group replicas=10
  Rev 47  2026-03-18 09:00  deploy   full deploy from git@abc123
  Rev 46  2026-03-17 16:45  vrjoshi  rewire: added dead_letter after validate

# Diff between revisions
zinc flow diff 47 49

# Rollback — instant, one command
zinc flow rollback                   # revert to previous revision
zinc flow rollback --to 47           # revert to specific revision

# Detect drift from git
zinc flow drift
  enrich: catalog says enrich_order@2.2, git says enrich_order@2.1
  enrich-group: running 10 replicas, git says 3
```

This solves NiFi's Achilles heel: live graph changes are powerful but dangerous without auditability. Every change is tracked, diffable, and reversible. You get NiFi's speed of response with git-level safety.

#### Catalog Storage

The processor catalog stores:
- **Processor metadata**: name, version, description, input/output schemas, config schema
- **Processor code**: module path or package reference (the actual `.zn` files)
- **Flow graph**: current processor wiring, group assignments, connection routes
- **Revision history**: every change with who/when/why/what

All stored in the state store (etcd or PostgreSQL). Phase 1 uses local filesystem for the catalog. Phase 2 adds the distributed state store.

### Q5: Schema enforcement — should FlowFile content have typed schemas?

**Answer: No. The framework does not enforce content schemas.**

FlowFile content is `bytes`. The framework doesn't know or care what's inside — JSON, CSV, Avro, Parquet, binary, images, whatever. Content is opaque to the runtime.

Validation is the dataflow developer's responsibility. They add validation processors where needed:

```zinc
@flow.processor(outputs=["valid", "invalid"])
fn validate_json_schema(ff: FlowFile): tuple[str, FlowFile] {
    var data = json.loads(ff.content) or {
        return "invalid", ff.with_attribute("error", "not valid JSON")
    }
    if "id" not in data or "type" not in data {
        return "invalid", ff.with_attribute("error", "missing required fields")
    }
    return "valid", ff
}
```

This is the right level of abstraction — the developer knows their data, the framework doesn't. NiFi works the same way.

### Q6: Multi-tenancy — multiple pipelines sharing infrastructure?

**Answer: Multiple pipelines, shared NATS, namespace isolation.**

Each pipeline has a name. Stream/subject names are namespaced: `zinc-flow.{pipeline}.{group}.{connection}`. Multiple pipelines can share the same NATS server without conflict.

No multi-tenant auth/isolation in Phase 1. If you need it, run separate NATS servers or use NATS accounts/auth.

### Q7: Expression language for routing?

**Answer: Zinc IS the expression language. No separate DSL.**

NiFi needs SpEL because processors are configured via XML/UI. SpEL is terrible — no validation, no autocomplete, cryptic errors, impossible to debug.

Zinc already has a parser and type checker. We use Zinc expressions directly and provide real tooling on top.

#### Developer mode — full Zinc code

Developers write routing logic as Zinc processors:

```zinc
@flow.processor(outputs=["high", "normal", "low"])
fn route_by_priority(ff: FlowFile): tuple[str, FlowFile] {
    var priority = ff.attributes.get("priority", "normal")
    return priority, ff
}
```

#### Low-code UI — two modes

**Simple mode** — form-based, no code. Covers 80% of use cases:

```
Route where:  [attribute ▼] [filename]  [contains ▼]  [.csv]   → route "csv_path"
              [attribute ▼] [priority]  [equals ▼]    [high]   → route "urgent"
              [otherwise]                               → route "default"

Set attribute: [key] processed_at  [value] {now()}
```

**Advanced mode** — Zinc expression for the 20% that need logic:

```zinc
ff.attributes["amount"].to_int() > 1000 and ff.attributes["region"] == "us-east"
```

The UI validates advanced expressions in real-time by running them through the Zinc transpiler — syntax errors, type errors, unknown attributes all caught before save. Autocomplete for `ff.attributes["..."]` based on what upstream processors are known to produce.

#### Why this is better than SpEL

- **Same language** the developer already knows — no separate DSL to learn
- **Full parser/type checker** validates expressions before they hit production
- **Autocomplete** is possible because we control the toolchain
- **Real error messages** — not `EL1008E: Property or field 'x' cannot be found`
- **Testable** — expressions are Zinc code, you can unit test them

### Q8: Monitoring — Prometheus? OpenTelemetry?

**Answer: Prometheus metrics export. Phase 2.**

- Phase 1: Stats printed to terminal (msgs/sec, queue depth, errors)
- Phase 2: `/metrics` endpoint in Prometheus exposition format. Standard counters/gauges:
  - `zinc_flow_processed_total{processor="name"}` — counter
  - `zinc_flow_errors_total{processor="name"}` — counter
  - `zinc_flow_queue_depth{queue="name"}` — gauge
  - `zinc_flow_processing_seconds{processor="name"}` — histogram
- This plugs into existing Grafana/alerting stacks with zero custom tooling.

OpenTelemetry tracing (trace a FlowFile through the pipeline) is Phase 3 — nice to have, not critical for MVP.

### Q9: State management — counters, windows, dedup?

**Answer: External state stores. Processors are stateless by default.**

Stateful processors (dedup, windowed aggregation, counters) read/write state to an external store:

```zinc
@flow.processor
fn dedup(ff: FlowFile): FlowFile? {
    var key = ff.attributes.get("dedup.key")
    var seen = state_store.get("dedup:seen:{key}")
    if seen is not none {
        return none  // drop duplicate
    }
    state_store.put("dedup:seen:{key}", "1")
    return ff
}
```

The processor is still stateless from the runtime's perspective — it can be restarted, scaled, or swapped without losing state. State lives in the external state store (etcd, PostgreSQL, Redis), not in the processor thread.

This is the same pattern Flink uses (state backends) and it's what makes horizontal scaling work — any replica of the processor can access the shared state.

### Q10: Ordering guarantees — FIFO per key? Per partition?

**Answer: Best-effort FIFO within a group. Keyed ordering between groups.**

- **Within a group (single replica)**: FIFO guaranteed — `queue.Queue` is FIFO, single consumer thread.
- **Within a group (multiple replicas)**: best-effort — multiple threads compete for items. Order is not guaranteed across threads.
- **Between groups**: NATS JetStream is FIFO within a stream. For keyed ordering, use subject-based routing (e.g., `orders.{region}`) so related messages go to the same subject and are consumed in order.

For most data pipeline workloads, best-effort ordering is fine. If a processor needs strict ordering (e.g., CDC events), run it with 1 replica or use keyed partitioning.

---

## Project Structure

```
zinc-flow/
    flow/
        __init__.zn          # @processor, @source, @sink decorators, Pipeline, Group
        flowfile.zn          # FlowFile data class
        pipeline.zn          # Pipeline, ProcessorGroup, Connection
        worker.zn            # ProcessorWorker, run loop
        queue.zn             # FlowQueue interface, LocalQueue
        queue_nats.zn        # NatsQueue — NATS JetStream (Phase 2)
        stats.zn             # ProcessorStats, throughput tracking
        serialization.zn     # FlowFile serialization for cross-group transport
        sources/
            http.zn          # HTTP source (aiohttp)
            kafka.zn         # Kafka consumer source
            filesystem.zn    # Directory watcher source
        sinks/
            http.zn          # HTTP POST sink
            kafka.zn         # Kafka producer sink
            filesystem.zn    # File writer sink
            s3.zn            # S3 object writer sink
    cli/
        flow_cmd.zn          # zinc flow run/status/processor/queues
    tests/
        test_flowfile.zn
        test_pipeline.zn
        test_worker.zn
        test_queue.zn
        test_e2e.zn          # full pipeline integration tests
```

---

## Implementation Phases

### Phase 1 — MVP (Local Dev)

1. **FlowFile** data class with `with_content()`, `with_attribute()`
2. **`@flow.processor` decorator** — wraps a function for pipeline use
3. **LocalQueue** — `queue.Queue` wrapper with stats
4. **ProcessorWorker** — thread-based consumer loop with retry and DLQ
5. **ProcessorGroup** — group of workers with start/stop/scale
6. **Pipeline** — connect groups, run all workers in local mode
7. **HTTP source** — accept POSTs, emit FlowFiles
8. **File sink** — write FlowFiles to disk
9. **CLI** — `zinc flow run pipeline.zn`
10. **Stats** — msgs/sec, queue depth, errors printed to terminal every 5s

**Not in Phase 1**: Pipeline DSL (`->` syntax), distributed queues, content repository, management REST API, K8s deploy, hot-swap, Prometheus metrics.

### Phase 2 — Production Ready

- Pipeline DSL with `->` chaining and group definitions
- NATS JetStream as cross-group queue backend
- Shared filesystem (NFS or K8s PVC) for large FlowFile content crossing group boundaries
- etcd or PostgreSQL for state store (flow graph, audit trail, processor state)
- REST management API (start/stop/scale/swap/config)
- Prometheus `/metrics` endpoint
- Back-pressure via NATS stream limits
- `zinc flow deploy` generates Docker Compose for multi-group

### Phase 3 — Cloud Native

- K8s operator: `zinc flow deploy` generates Deployments per group
- Auto-scaling groups based on queue depth (HPA with custom metrics from NATS consumer lag)
- Kafka queue backend option (pluggable alternative)
- OpenTelemetry tracing (FlowFile lineage across groups)
- TUI dashboard

### Phase 4 — Enterprise

- Provenance tracking and lineage visualization
- Schema validation (optional per-processor)
- Role-based access control on management API
- Audit logging
- Multi-pipeline management
- Web UI

---

## Phase 1 End-to-End Example

```zinc
import flow

@flow.processor
fn parse_json(ff: FlowFile): FlowFile {
    var data = json.loads(ff.content)
    return ff.with_attribute("record_type", data.get("type", "unknown"))
              .with_content(json.dumps(data, indent=2).encode())
}

@flow.processor
fn add_timestamp(ff: FlowFile): FlowFile {
    return ff.with_attribute("processed_at", datetime.now().isoformat())
}

@flow.processor(outputs=["valid", "invalid"])
fn validate(ff: FlowFile): tuple[str, FlowFile] {
    var data = json.loads(ff.content)
    if "id" in data and "type" in data {
        return "valid", ff
    }
    return "invalid", ff.with_attribute("error", "missing required fields")
}

// Phase 1 — explicit wiring (no DSL yet)
var pipeline = flow.Pipeline("ingest")

var group = flow.Group("main")
group.add_source(flow.sources.http(port=8080, path="/data"))
group.add_processor("parse", parse_json)
group.add_processor("timestamp", add_timestamp)
group.add_processor("validate", validate)
group.add_sink("valid_out", flow.sinks.filesystem("/data/output/valid/"), route="valid")
group.add_sink("invalid_out", flow.sinks.filesystem("/data/output/invalid/"), route="invalid")

group.connect("source", "parse")
group.connect("parse", "timestamp")
group.connect("timestamp", "validate")
group.connect("validate", "valid_out", route="valid")
group.connect("validate", "invalid_out", route="invalid")

pipeline.add_group(group)
pipeline.run()
```

```bash
$ zinc flow run ingest.zn
Pipeline 'ingest' starting (local mode)...
  [main/http-source]   listening on :8080/data
  [main/parse]         running (1 thread)
  [main/timestamp]     running (1 thread)
  [main/validate]      running (1 thread)
  [main/valid_out]     writing to /data/output/valid/
  [main/invalid_out]   writing to /data/output/invalid/

Pipeline running. Ctrl+C to stop.

Stats (every 5s):
  main/parse:      1,247 msgs/s | queue: 12/10000 | errors: 0
  main/timestamp:  1,245 msgs/s | queue:  3/10000 | errors: 0
  main/validate:   1,243 msgs/s | queue:  5/10000 | errors: 2
```
