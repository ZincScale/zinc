# Design: Zinc Flow — Lightweight NiFi-Inspired Flow Processing

> **Status**: REQUIREMENTS GATHERING — collecting ideas, constraints, prior art analysis

## The Problem

NiFi is the gold standard for data flow processing but has real problems:

| Problem | Impact |
|---|---|
| **Bloated** | 1GB+ JVM heap, hundreds of bundled processors, massive install |
| **Not cloud-native** | Designed for single-node; cluster mode is bolted on, not elastic |
| **Not horizontally scalable** | Can't auto-scale individual processors independently |
| **Heavy** | Can't run on edge, can't embed in existing apps |
| **Java-only processors** | Writing custom processors requires Java, Maven, NAR packaging |

**MiNiFi** tried to solve "lightweight" but is neglected and missing features.

**DeltaFi** tried to solve "cloud-native" but:
- Too many tools/technologies in the stack
- Docker containers as processor boundary (slow startup, resource overhead)
- Unproven at scale
- Complex to operate

## What We Want

**NiFi's model. Python's ecosystem. Zinc's simplicity. Cloud-native from day one.**

A processor is a Zinc function. A pipeline connects processors with queues. Processors can be started, stopped, swapped, and scaled independently in production — without redeploying the whole pipeline.

## Core Requirements

### R1 — Processor Model

A processor is a Zinc function that takes a FlowFile and returns one or more FlowFiles:

```zinc
@processor
fn enrich_order(flow: FlowFile): FlowFile {
    var data = json.loads(flow.content)
    data["enriched_at"] = datetime.now().isoformat()
    data["region"] = lookup_region(data["zip_code"])
    return flow.with_content(json.dumps(data))
}
```

- **Stateless by default** — no shared mutable state between invocations
- **Pure function** — input FlowFile in, output FlowFile(s) out
- **Swappable in production** — replace a processor without stopping the pipeline
- **Independent failure** — one processor crashing doesn't kill others
- **Independent scaling** — scale a slow processor to 10 instances while others stay at 1

### R2 — FlowFile

The unit of data flowing through the pipeline. Same concept as NiFi:

```zinc
data FlowFile {
    id: str                    // unique identifier
    attributes: dict[str, str] // metadata (filename, mime.type, source, etc.)
    content: bytes             // the payload (1 byte to 100MB+)
    provenance: list[str]      // processing history
}
```

- **Attributes** — small metadata dict, copied freely between processors
- **Content** — the payload, potentially large (1-8MB typical, up to 100MB+)
- **Content by reference** — large content stored in content repository, FlowFile holds a reference (not copied between processors)
- **Provenance** — track where data came from, what happened to it

### R3 — Pipeline Definition

Pipelines connect processors with typed connections:

```zinc
pipeline order_processing
    // Sources
    source kafka("orders-topic", group="zinc-flow")

    // Processing chain
    -> validate_order
    -> enrich_order

    // Routing — fan out based on attributes or content
    -> route(
        status == "completed" -> process_payment,
        status == "pending"   -> hold_queue,
        _                     -> dead_letter
    )

    // Sinks
    process_payment -> sink kafka("payments-topic")
    hold_queue      -> sink s3("s3://bucket/pending/")
    dead_letter     -> sink filesystem("/var/zinc-flow/dead-letter/")
}
```

### R4 — Hot Swap / Live Processor Management

Critical requirement — must be able to in production:

- **Start** a processor (begin consuming from its input queue)
- **Stop** a processor (stop consuming, let queue buffer)
- **Swap** a processor (replace implementation, zero downtime)
- **Scale** a processor (add/remove instances)
- **Disable** a connection (stop routing to a branch)

```bash
zinc-flow processor stop enrich_order
zinc-flow processor swap enrich_order --version 2.1
zinc-flow processor start enrich_order
zinc-flow processor scale enrich_order --replicas 5
```

This implies:
- Each processor runs as an independent process (not a thread in a monolith)
- Processors communicate via queues (not function calls)
- Queue is the buffer — when a processor is stopped, messages accumulate
- Swap = stop old, deploy new, start new — queue bridges the gap

### R5 — Back-Pressure

When a downstream processor is slow or stopped, upstream should slow down:

- **Queue depth limits** — when queue reaches threshold, upstream blocks
- **Priority** — some FlowFiles are more important than others
- **Overflow** — when queue is full, optionally spill to disk or object storage

### R6 — Fault Tolerance

- **Processor crash** — auto-restart, reprocess from last checkpoint
- **At-least-once delivery** — FlowFile is not removed from input queue until processor confirms success
- **Dead letter queue** — failed FlowFiles routed to DLQ with error metadata
- **Circuit breaker** — if a processor fails N times, stop sending it traffic

```zinc
@processor(retries=3, dead_letter="errors")
fn risky_transform(flow: FlowFile): FlowFile {
    // If this throws, retried 3 times, then sent to "errors" queue
    var result = external_api_call(flow.content)
    return flow.with_content(result)
}
```

### R7 — Sources and Sinks

Built-in connectors for common data sources/sinks:

| Source/Sink | Protocol |
|---|---|
| **Kafka** | Consume/produce topics |
| **S3** | Read/write objects |
| **Filesystem** | Watch directory, write files |
| **HTTP** | Receive webhooks, POST to endpoints |
| **Database** | JDBC query, CDC |
| **MQTT** | IoT message broker |

Custom sources/sinks are just Zinc functions:

```zinc
@source
fn watch_directory(path: str, pattern: str = "*"): FlowFile {
    // yield FlowFiles as files appear
    for file in glob(path, pattern) {
        yield FlowFile(
            attributes={"filename": file.name, "path": str(file)},
            content=file.read_bytes()
        )
    }
}
```

### R8 — GUI / Management Interface

Operators need to:

- **Visualize** the pipeline graph (processors, connections, queue depths)
- **Monitor** throughput, latency, error rates per processor
- **Control** start/stop/scale/swap processors
- **Inspect** FlowFiles (view attributes, content preview, provenance)
- **Configure** processor parameters without redeployment

Options:
- Web UI (React/Vue) talking to a REST API
- Terminal UI (TUI) for CLI-first environments
- REST API is the foundation — any UI is just a client

### R9 — Cloud Native

- **K8s-native** — each processor is a pod (or a process in a pod)
- **Horizontal scaling** — scale processors independently via replicas
- **Stateless processors** — no local state dependency (state in external stores)
- **Config as code** — pipeline definitions are `.zn` files in git
- **No single point of failure** — no "NiFi cluster coordinator"

### R10 — Lightweight

- **No JVM** — Python processes, small memory footprint
- **Fast startup** — processor starts in <1 second (not 30s like NiFi)
- **Embeddable** — can run a mini pipeline inside an existing Python app
- **Edge-capable** — run on a Raspberry Pi, a Lambda function, or a K8s pod

---

## Architecture Options

### Option A — Process-per-Processor with Message Queue

```
                    ┌─────────────┐
                    │  Queue (Redis│
                    │  / Kafka /  │
                    │  in-memory) │
                    └──────┬──────┘
                           │
    ┌──────────┐    ┌──────┴──────┐    ┌──────────┐
    │Processor │───>│   Queue     │───>│Processor │
    │  (proc)  │    │             │    │  (proc)  │
    └──────────┘    └─────────────┘    └──────────┘
```

- Each processor is an OS process (or K8s pod)
- Processors communicate via message queues
- Queue = Redis Streams, Kafka, or in-process (for local dev)
- **Pro**: True isolation, independent scaling, crash recovery
- **Con**: Queue overhead, latency between processors

### Option B — Thread-per-Processor with Shared Memory

```
    ┌─────────────────────────────────┐
    │         Zinc Flow Runtime       │
    │  ┌────────┐  ┌────────┐  ┌───┐ │
    │  │Proc A  │─>│Queue   │─>│B  │ │
    │  │(thread)│  │(deque) │  │   │ │
    │  └────────┘  └────────┘  └───┘ │
    └─────────────────────────────────┘
```

- All processors run as threads in one process (free-threaded Python 3.13+)
- Queues are `collections.deque` or `queue.Queue` (shared memory)
- **Pro**: Low latency, no serialization, simple deployment
- **Con**: One process crash kills all processors, scaling = scaling the whole thing

### Option C — Hybrid (Recommended)

- **Local dev**: Thread-per-processor (fast, simple, single process)
- **Production**: Process-per-processor or pod-per-processor
- Same Zinc pipeline definition works in both modes
- Switch via config: `zinc-flow run pipeline.zn --mode local|distributed`

```
Local:   zinc-flow run pipeline.zn                    # threads, in-memory queues
Prod:    zinc-flow run pipeline.zn --mode distributed  # processes, Redis/Kafka queues
K8s:     zinc-flow deploy pipeline.zn                  # generates K8s manifests
```

---

## Prior Art — What to Learn From

| System | What to steal | What to avoid |
|---|---|---|
| **NiFi** | FlowFile model, provenance, back-pressure, processor lifecycle | JVM bloat, monolithic cluster, NAR packaging |
| **DeltaFi** | K8s-native, plugin architecture | Docker-per-processor overhead, complexity |
| **MiNiFi** | Lightweight footprint | Neglected, missing features |
| **Prefect** | Python-native, decorator-based task definition | Batch-oriented, not streaming |
| **Flink** | Exactly-once, checkpointing, watermarks | JVM, operational complexity |
| **Luigi** | Simple task dependencies | No streaming, no real-time |
| **Temporal** | Workflow durability, replay | Too general, not data-flow specific |

---

## Content Repository — Large Payload Strategy

NiFi's key insight: FlowFile content is stored in a content repository, and processors pass references (claims) not copies.

For Zinc Flow:

```
FlowFile passed between processors:
  { attributes: {...}, content_ref: "content://abc123" }   ← 200 bytes

Actual content stored separately:
  content://abc123 → 4MB payload in content store
```

Content store options by mode:
- **Local dev**: filesystem directory (e.g., `/tmp/zinc-flow/content/`)
- **Production**: S3, MinIO, or shared filesystem
- **In-memory**: for small payloads (<64KB), skip the store entirely

This means passing a 4MB FlowFile between processors costs ~200 bytes (the reference), not 4MB.

---

## Research Findings (2026-03-18)

### Architecture Decision: Pure Python

Benchmarked free-threaded Python queue throughput: **301K msg/sec** — comparable to NiFi (100K-500K). No need for Go/Rust runtime. Stay pure Python.

### Key Insight: FlowFile = list[dict] = Polars DataFrame

Everything maps to `list[dict]`, which Polars auto-accelerates:

```
NiFi FlowFile       → dict (attributes + content)
Batch of FlowFiles  → list[dict]
list[dict]          → Polars DataFrame (auto, Rust engine)

Avro records        → list[dict] (fastavro)
JSON array          → list[dict] (orjson/json)
CSV rows            → list[dict] (polars.read_csv)
Parquet             → list[dict] (polars.read_parquet)
Database rows       → list[dict] (SQLAlchemy)
Kafka messages      → list[dict] (confluent-kafka)
```

The smart dispatch already built into Zinc auto-promotes `list[dict]` chains to Polars. Processors that filter/map/aggregate FlowFiles are automatically Polars-accelerated.

FlowFile V3 binary format is only needed for NiFi wire compatibility (import/export), not for internal processing.

### Stack

```
Zinc Flow (Python)      — orchestration, queues, routing, lifecycle
Polars (Rust)           — heavy data processing inside processors
NumPy (C)               — numeric computation
Free-threaded Python    — real parallelism between processors
Single binary           — zinc pack bundles everything
```

## Open Questions

1. **Queue technology** — Redis Streams? Kafka? Custom? Should be pluggable.
2. **GUI framework** — Web UI vs TUI vs both? REST API first, UI second.
3. **Processor discovery** — how does the runtime find and load processor functions?
4. **Versioning** — how to version processors for hot-swap?
5. **Schema enforcement** — should FlowFile content have typed schemas?
6. **Multi-tenancy** — multiple pipelines sharing infrastructure?
7. **Expression language** — NiFi has expression language for attribute routing. Do we need one, or is Zinc expressive enough?
8. **Monitoring** — Prometheus metrics? OpenTelemetry? Built-in dashboard?
9. **State management** — some processors need state (counters, windows, dedup). Where does state live?
10. **Ordering guarantees** — FIFO per key? Per partition? Best-effort?

---

## Implementation Phases

### Phase 1 — MVP (Local Dev)
- FlowFile data class
- Processor decorator (`@processor`)
- Pipeline definition DSL
- In-memory queues (thread-safe deques)
- Thread-per-processor execution
- `zinc-flow run pipeline.zn`
- Basic CLI: start/stop processors

### Phase 2 — Production Ready
- Redis Streams as queue backend
- Process-per-processor mode
- Content repository (filesystem)
- Back-pressure and queue depth limits
- Dead letter queues
- Retry/circuit breaker
- REST API for management
- Basic web UI (pipeline graph, queue depths)

### Phase 3 — Cloud Native
- K8s operator for pipeline deployment
- Auto-scaling based on queue depth
- S3/MinIO content repository
- Kafka source/sink connectors
- Prometheus metrics export
- Hot-swap processor versions

### Phase 4 — Enterprise
- Provenance tracking and lineage
- Schema registry integration
- Role-based access control
- Audit logging
- Multi-pipeline management
- Expression language for routing
