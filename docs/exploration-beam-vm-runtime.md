<!--
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
-->

# Exploration: BEAM VM as a Runtime for Data Pipeline Processing in Zinc v2

> **Status**: EXPLORATORY — this document is a research artifact, not a committed design.
> It explores whether the BEAM virtual machine (Erlang's VM) could serve as a runtime
> for NiFi/Spark-style pipeline and streaming workloads in Zinc v2.

> **Important disambiguation**: This document is about the **BEAM VM** — the virtual machine
> that runs Erlang, Elixir, and Gleam. It is **NOT** about Apache Beam, which is a separate
> unified programming model for batch and streaming data processing. Despite the similar
> names, these are entirely different technologies. BEAM stands for Bogdan/Bjorn's Erlang
> Abstract Machine.

---

## 1. What is the BEAM VM

The BEAM (Bogdan/Bjorn's Erlang Abstract Machine) is the virtual machine that executes
Erlang, Elixir, Gleam, and LFE (Lisp Flavored Erlang). It was built by Ericsson in the
1980s-90s for telecom switches that needed to run forever with zero downtime. Those
requirements — massive concurrency, fault tolerance, hot upgrades, soft real-time
guarantees — made the BEAM unlike any other VM in production today.

### Lightweight Processes

BEAM processes are not OS threads. They are lightweight green threads managed entirely by
the VM. Each process starts at roughly **2.5 KB of memory** (stack + heap + process control
block). A single BEAM node can run **millions of processes** simultaneously — the hard limit
is approximately 134 million processes per node, though practical limits depend on available
RAM.

Process creation takes **microseconds**, not milliseconds. There is no thread pool to size,
no executor to configure. You spawn a process and the VM handles the rest.

Each process has its own **isolated heap** — there is no shared mutable state. Processes
communicate exclusively through **message passing**. A process sends a message to another
process's mailbox; the receiver pattern-matches on the mailbox to handle it. This
share-nothing architecture eliminates entire categories of concurrency bugs: no locks, no
mutexes, no data races.

### Preemptive Scheduling

The BEAM uses **preemptive scheduling based on reductions** — a "reduction" is a unit of
work (roughly one function call). Each process gets a budget of reductions (typically 4000)
before it is preempted and the next process runs. This is fundamentally different from
cooperative scheduling (Go goroutines, Python asyncio) where a long-running computation
can starve other tasks.

In a multi-core environment, the BEAM creates one **scheduler** per CPU core, each with its
own run queue. A **work-stealing algorithm** balances load across schedulers — if one core's
queue is empty, it steals work from a busy core. This provides true parallelism without
any developer effort.

The reduction-based preemption delivers **soft real-time guarantees** — no single process
can monopolize the CPU, so latency stays bounded. This is why the BEAM powers systems like
WhatsApp (2 million connections per server) and Discord.

### OTP and Supervisors

OTP (Open Telecom Platform) is the standard library and framework bundled with Erlang/BEAM.
Its most important concept is the **supervision tree**:

- A **supervisor** is a process whose only job is to monitor child processes.
- When a child crashes, the supervisor restarts it according to a strategy (one-for-one,
  one-for-all, rest-for-one).
- Supervisors can supervise other supervisors, forming a tree.

This is the **"let it crash" philosophy** — instead of defensive error handling everywhere,
you let processes fail fast and rely on supervisors to restart them in a clean state. The
system self-heals. Ericsson achieved **99.9999999% availability** (nine nines) on their
AXD301 telecom switch using this model.

### Hot Code Loading

The BEAM can upgrade code in a **running system without stopping it**. The VM keeps two
versions of each module in memory — "current" and "old". When new code is loaded, the
current version becomes old and the new code becomes current. Running processes continue
executing the old version until they make a fully qualified function call, at which point
they switch to the current version. This allows zero-downtime deployments — critical for
pipeline systems that cannot afford restart windows.

### Distribution

BEAM has **built-in transparent distribution**. Connecting BEAM nodes into a cluster is a
first-class operation — `Node.connect(:other@host)` in Elixir. Message passing works
identically whether the target process is local or on a remote node. A process does not
need to know (or care) where another process lives. Clusters can scale to thousands of
nodes. This makes the BEAM the only widely-used VM with a built-in distribution model that
allows a program to run on multiple machines transparently.

---

## 2. Why BEAM for Data Pipelines

The BEAM's process model maps almost directly onto the architecture of data pipeline
systems like Apache NiFi and Apache Spark Streaming.

### NiFi / Pipeline Architecture Mapping

| NiFi Concept | BEAM Equivalent |
|---|---|
| Processor | BEAM process |
| FlowFile queue | Process mailbox |
| Backpressure (queue threshold) | GenStage demand-based backpressure |
| Processor group | Supervision tree |
| Controller service | GenServer (stateful process) |
| Clustering | BEAM distribution (built-in) |
| Bulletin board (error reporting) | Supervisor error logging + restart |
| Provenance (data lineage) | Message metadata in process state |

### Why This Mapping Works

**Each processor is a BEAM process.** In NiFi, a processor reads from an input queue,
transforms data, and writes to an output queue. In BEAM, a process receives messages from
its mailbox, processes them, and sends results to downstream processes. The conceptual
model is identical, but BEAM processes are orders of magnitude cheaper — NiFi processors
run as Java threads (~1 MB each), while BEAM processes are ~2.5 KB each.

**Backpressure via mailbox and demand.** NiFi implements backpressure by setting thresholds
on FlowFile queues — when a queue is full, upstream processors stop sending. The BEAM
ecosystem solves this with GenStage's **demand-based backpressure** — consumers tell
producers how many items they can handle, and producers only send that many. This is more
elegant than threshold-based backpressure because it prevents queue buildup entirely.

**Supervision trees for fault tolerance.** When a NiFi processor fails, NiFi restarts it
and replays failed FlowFiles. In BEAM, when a process crashes, its supervisor restarts it
automatically. The supervision tree can be structured to match the pipeline topology —
if a processor stage crashes, only that stage restarts, not the entire pipeline.

**Hot code loading for pipeline upgrades.** NiFi supports updating processor NARs at
runtime. BEAM's hot code loading goes further — you can upgrade the transformation logic
of a running pipeline stage without stopping data flow. The old version drains while the
new version picks up incoming messages.

**Built-in distribution for scaling.** NiFi clustering requires ZooKeeper coordination.
BEAM distribution is built into the VM — pipeline stages can be transparently distributed
across nodes with no external coordination service. A producer on node A can send messages
to a consumer on node B using the same `send` call as local messaging.

---

## 3. BEAM Languages

Four languages run on the BEAM VM, each with different strengths for pipeline workloads.

### Erlang

The original BEAM language, created at Ericsson in 1986. Functional, dynamically typed,
pattern matching, immutable data. Erlang's syntax is Prolog-derived and feels unfamiliar
to most developers. Its standard library (OTP) is battle-tested at a scale few systems
match. Erlang remains the best choice for raw OTP work, but its syntax is a barrier for
adoption.

### Elixir

Created by Jose Valim in 2011. Ruby-inspired syntax on top of the BEAM. Elixir compiles
to BEAM bytecode and has full interop with Erlang modules. It is the most popular BEAM
language today, with a mature ecosystem for web (Phoenix), data pipelines (Broadway/Flow),
and embedded systems (Nerves).

**For pipeline workloads, Elixir is the strongest choice** because of:
- **GenStage** — producer-consumer abstraction with built-in backpressure
- **Flow** — parallel data processing with windowing and aggregation
- **Broadway** — production-ready pipeline framework with batching, rate limiting, and
  connectors for Kafka, SQS, RabbitMQ, Google PubSub

### Gleam

A statically-typed language that compiles to both BEAM bytecode and JavaScript. Currently
at **v1.14.0** (February 2026). Gleam has a Rust-like type system with exhaustive pattern
matching, no nulls, and no exceptions. It was the **2nd "most admired" language** in the
2025 Stack Overflow Developer Survey and was added to the Thoughtworks Technology Radar
in April 2025.

Gleam is particularly interesting for Zinc because:
- It is **statically typed** like Zinc — type errors are caught at compile time
- It compiles to **readable Erlang** (or JavaScript) — similar to Zinc's transpiler approach
- It has **no runtime exceptions** — errors are values (Result types), aligning with
  Zinc's `or {}` error handling philosophy
- It is younger and proves that **new languages on BEAM are viable** in 2025-2026

### LFE (Lisp Flavored Erlang)

A Lisp that runs on the BEAM. Niche but proves BEAM's flexibility as a compile target.
Not relevant for Zinc's goals.

---

## 4. How Zinc Could Use BEAM

Two fundamental approaches, with different tradeoffs.

### Approach A: Transpile Zinc to Elixir (or Gleam/Erlang)

Zinc already transpiles `.zn` files to Python. The same architecture could target Elixir
or Erlang for pipeline-focused workloads.

```
Pipeline:  .zn -> Lexer -> Parser (AST) -> Typechecker -> Elixir Codegen -> .ex -> BEAM bytecode
Scripts:   .zn -> Lexer -> Parser (AST) -> Typechecker -> Python Codegen -> .py -> Python runtime
```

The lexer, parser, and typechecker are backend-agnostic. Only the codegen layer changes.
This is the same pattern Zinc already uses (C# AOT backend + Go backend in v1).

**Advantages:**
- Full access to BEAM's concurrency, fault tolerance, and distribution
- Pipeline code runs natively on the BEAM — no bridging overhead
- Hot code loading for zero-downtime pipeline upgrades
- Gleam as a typed target means type information from Zinc's typechecker carries through

**Disadvantages:**
- Requires building an entirely new codegen backend
- BEAM ecosystem is smaller than Python's — ML/AI libraries are scarce
- Developers need BEAM/OTP installed to run pipeline code
- Two runtimes to understand, debug, and deploy

**Gleam as transpile target:** Since Gleam is statically typed and compiles to readable
Erlang, it could serve as an intermediate representation. Zinc AST -> Gleam source -> BEAM
bytecode. This preserves type safety through the entire pipeline. However, Gleam's
ecosystem is still young, and some OTP patterns (like GenStage) do not have Gleam
equivalents yet.

### Approach B: BEAM as Orchestrator, Python as Worker

Keep Python as the primary runtime, but use the BEAM to **orchestrate** pipeline topology,
backpressure, and fault tolerance. Python processes handle the actual data transformation.

```
BEAM Node (orchestration):
  Supervisor
    |-- Producer Process (reads from Kafka/SQS/files)
    |-- Router Process (distributes work)
    |-- Python Worker Pool (Ports to Python processes)
    |     |-- Python Process 1 (runs .py transformation)
    |     |-- Python Process 2 (runs .py transformation)
    |     |-- Python Process N
    |-- Collector Process (aggregates results)
    |-- Sink Process (writes to output)
```

BEAM communicates with Python via **Ports** (stdin/stdout to separate OS processes) or
**NIFs** (native functions in the BEAM process). Ports are safer — a crash in Python does
not crash the BEAM node. NIFs are faster but risky.

Recent developments make this more feasible:
- **erlang-python** (github.com/benoitc/erlang-python) embeds Python in the BEAM VM with
  dirty NIF scheduling, GIL awareness, and free-threading support for Python 3.13+
- **Pythonx** runs a Python interpreter in the same OS process as Elixir, allowing direct
  function calls and data structure conversion

**Advantages:**
- Full Python ecosystem for data transformation (pandas, scikit-learn, PyTorch, etc.)
- BEAM handles the hard parts (concurrency, backpressure, fault tolerance, distribution)
- Python workers are supervised — if a Python process crashes, BEAM restarts it
- Incremental adoption — add BEAM orchestration to existing Python pipelines

**Disadvantages:**
- Serialization overhead between BEAM and Python (data crosses process boundaries)
- Two runtimes to install, configure, and monitor
- Python's GIL limits per-worker concurrency (mitigated by free-threaded Python 3.13+)
- More operational complexity than pure-Python or pure-BEAM

---

## 5. BEAM vs Python for Pipelines

A direct comparison across the dimensions that matter for data pipeline workloads.

### Concurrency Model

| Dimension | BEAM | Python |
|---|---|---|
| **Concurrency primitive** | Process (~2.5 KB, microsecond spawn) | Thread (~8 MB stack) or coroutine (asyncio) |
| **Parallelism** | True parallel — one scheduler per core | GIL limits threads to one core (free-threaded 3.13+ changes this) |
| **Scale** | Millions of processes per node | Thousands of threads, or multiprocessing (OS process per worker) |
| **Communication** | Message passing (mailbox per process) | Shared memory + locks, or Queue for multiprocessing |
| **Isolation** | Process heaps are isolated — no shared state | Threads share memory — data races possible |
| **Scheduling** | Preemptive (reduction-based, fair) | Cooperative (asyncio) or OS-scheduled (threads) |

**Where BEAM wins:** Massive concurrency (100K+ concurrent pipeline stages), guaranteed
fairness (no stage can starve others), zero shared-state bugs.

**Where Python wins:** Lower barrier to entry, more developers available, simpler mental
model for sequential scripts.

### Fault Tolerance

| Dimension | BEAM | Python |
|---|---|---|
| **Error philosophy** | "Let it crash" — supervisors restart failed processes | Try/except — defensive handling, or crash the whole program |
| **Isolation** | Process crash does not affect other processes | Thread crash can corrupt shared state; exception in one thread can be swallowed silently |
| **Recovery** | Automatic — supervisor restarts in milliseconds | Manual — need external process manager (systemd, Kubernetes) |
| **Granularity** | Per-process (per-pipeline-stage) | Per-program or per-OS-process |

**Where BEAM wins:** Self-healing pipelines. A single malformed record crashes one processor
process; the supervisor restarts it; the pipeline continues. No human intervention.

**Where Python wins:** Simpler for scripts that should just fail loudly and exit.

### Backpressure

| Dimension | BEAM (GenStage/Broadway) | Python |
|---|---|---|
| **Mechanism** | Demand-based — consumers request N items | Queue with maxsize (blocks producer when full) |
| **Granularity** | Per-stage, configurable | Per-queue, manual wiring |
| **Built-in** | Yes (GenStage, Flow, Broadway) | No — must build manually or use Celery/etc. |
| **Rate limiting** | Broadway has token-bucket rate limiter built in | Manual implementation or third-party library |

**Where BEAM wins:** Backpressure is a first-class concept, not an afterthought.

**Where Python wins:** For simple producer-consumer patterns, `queue.Queue(maxsize=N)` is
adequate and requires no framework.

### Distribution

| Dimension | BEAM | Python |
|---|---|---|
| **Multi-node** | Built into the VM — `Node.connect`, transparent message passing | External tools (Dask, Ray, Celery + broker) |
| **Service discovery** | Built-in (epmd) or libcluster | External (Consul, Kubernetes, etc.) |
| **Transparency** | Same code works local and distributed | Different APIs for local vs. distributed |

**Where BEAM wins:** Distribution is not a library, it is part of the VM. Scaling a pipeline
from one node to ten nodes requires zero code changes.

**Where Python wins:** Dask and Ray provide distributed computing with tight NumPy/pandas
integration that BEAM cannot match.

### Ecosystem

| Dimension | BEAM | Python |
|---|---|---|
| **ML/AI** | Nx (numerical Elixir) — young, limited | PyTorch, TensorFlow, scikit-learn — dominant |
| **Data science** | Explorer (dataframes) — functional but small | pandas, Polars, NumPy — industry standard |
| **Web** | Phoenix — excellent for real-time | Django, FastAPI, Flask — massive ecosystem |
| **Pipeline connectors** | Broadway (Kafka, SQS, RabbitMQ, PubSub) | Kafka-python, boto3, etc. — everything |
| **Package count** | ~20K Hex packages | ~500K PyPI packages |

**Where Python wins decisively:** Ecosystem breadth, especially for ML/AI and data science.
This is the single biggest reason to keep Python as Zinc's primary target.

**Where BEAM wins:** Purpose-built pipeline tooling (Broadway) that handles the hard
concurrency problems out of the box.

---

## 6. Elixir GenStage, Broadway, and Flow

These three libraries form a layered pipeline processing stack, built on the same
foundation. They implement exactly the patterns that NiFi and Spark Streaming provide.

### GenStage — The Foundation

GenStage is a producer-consumer abstraction with **demand-driven backpressure**. It defines
three types of stages:

- **Producer** — generates events (e.g., reads from a file, database, or message queue)
- **ProducerConsumer** — receives events, transforms them, emits new events
- **Consumer** — receives events and performs side effects (write to disk, send to API)

The key insight is **demand flows upstream**: a consumer tells its producer "I can handle
10 more events." The producer only generates 10 events. This prevents queue buildup and
memory exhaustion — the system naturally throttles to the speed of the slowest consumer.

### Flow — Parallel Collection Processing

Flow builds on GenStage to provide **parallel map, filter, reduce, and window operations**
over large datasets. Think of it as Elixir's answer to Java Streams or Spark RDDs:

- Data is automatically partitioned across stages
- Each partition runs in its own BEAM process (true parallelism)
- Supports windowing (fixed, sliding, session) for streaming data
- Aggregation functions (reduce, emit) work across partitions

Flow is best for **bounded datasets** that need parallel processing — analogous to Spark
batch jobs.

### Broadway — Production Pipeline Framework

Broadway is the highest-level abstraction, designed for **long-running, production data
ingestion pipelines**. It provides:

- **Built-in producers** for Amazon SQS, Apache Kafka, Google Cloud PubSub, RabbitMQ
- **Automatic concurrency** — configure the number of processors and batchers, Broadway
  handles supervision and work distribution
- **Batching by key** — group messages by a field (e.g., user_id, region) before sending
  to the batcher
- **Rate limiting** — token-bucket rate limiter to throttle throughput to match downstream
  capacity
- **Graceful shutdown** — drains in-flight messages before stopping
- **Telemetry integration** — built-in metrics for monitoring pipeline health
- **Acknowledgment** — messages are only acknowledged to the source after successful
  processing (at-least-once delivery)

A Broadway pipeline is defined declaratively:

```elixir
# Elixir Broadway example (for reference)
defmodule MyPipeline do
  use Broadway

  def start_link(_opts) do
    Broadway.start_link(__MODULE__,
      name: __MODULE__,
      producer: [
        module: {BroadwayKafka.Producer, [
          hosts: [localhost: 9092],
          group_id: "my-group",
          topics: ["events"]
        ]},
        concurrency: 2
      ],
      processors: [
        default: [concurrency: 10]
      ],
      batchers: [
        s3: [concurrency: 4, batch_size: 100, batch_timeout: 5000]
      ]
    )
  end

  def handle_message(_, message, _) do
    message
    |> Message.update_data(&Jason.decode!/1)
    |> Message.put_batcher(:s3)
  end

  def handle_batch(:s3, messages, _batch_info, _context) do
    # Write batch to S3
    messages
  end
end
```

This is the NiFi/Spark pattern expressed in code: producers read from Kafka, processors
transform messages concurrently, batchers group results and write to S3 — with
backpressure, fault tolerance, and rate limiting handled automatically.

---

## 7. Zinc Syntax Possibilities — Pipeline Code Targeting BEAM

If Zinc v2 had a BEAM backend, pipeline code would use the same Zinc syntax (end blocks,
var declarations, fn with colon return types) but transpile to Elixir/Erlang instead of
Python. Here are examples of what this could look like.

### Basic Pipeline Definition

```zinc
// A pipeline that reads from Kafka, transforms events, and writes to S3.
// The @pipeline annotation tells the Zinc compiler to use the BEAM backend.

@pipeline
module EventProcessor

    @producer(source: "kafka", topics: ["user-events"], concurrency: 2)
    fn produce(): stream[dict]
        // Producer configuration — Zinc generates Broadway producer setup
    end

    @processor(concurrency: 10)
    fn process(event: dict): dict
        var parsed = json.decode(event.data)
        var enriched = parsed.merge({
            "processed_at": time.now(),
            "region": lookup_region(parsed["ip"]),
        })
        return enriched
    end

    @batcher(name: "s3", batch_size: 100, batch_timeout: 5000)
    fn batch(events: list[dict]): list[dict]
        var key = "events/{date}/{uuid}.json".format(
            date: time.today(),
            uuid: uuid.v4()
        )
        s3.put_object(bucket: "data-lake", key: key, body: json.encode(events))
        return events
    end

end
```

### Supervised Worker with State

```zinc
// A stateful processor that maintains a running count.
// Transpiles to a GenServer on BEAM.

@service
class MetricsAggregator

    var counts: dict[str, int] = {}
    var window_start: datetime = time.now()

    fn handle(event: dict): none
        var key = event["type"]
        counts[key] = counts.get(key, 0) + 1

        if time.since(window_start) > duration("1m")
            flush()
            window_start = time.now()
        end
    end

    fn flush(): none
        for key, count in counts
            metrics.emit(name: "event_count", tags: {"type": key}, value: count)
        end
        counts = {}
    end

end
```

### Parallel Data Processing with Flow

```zinc
// Process a large CSV file in parallel using Flow (parallel streams).
// Each partition runs in its own BEAM process.

fn analyze_sales(path: str): dict[str, float]
    var results = file.stream_lines(path)
        .map(line -> csv.parse_row(line))
        .filter(row -> row["status"] == "completed")
        .partition_by(row -> row["region"])
        .map(row -> {
            "region": row["region"],
            "amount": float(row["amount"]),
        })
        .reduce_by(
            key: row -> row["region"],
            init: 0.0,
            fn: (acc, row) -> acc + row["amount"]
        )

    return results
end
```

### Supervision Tree Declaration

```zinc
// Declare a supervision tree for a multi-stage pipeline.
// The supervisor monitors all stages and restarts failures.

@supervisor(strategy: "one_for_one", max_restarts: 5, max_seconds: 60)
module DataPipeline

    @child
    var producer = KafkaReader(topics: ["orders", "returns"])

    @child
    var enricher = OrderEnricher(db: postgres_pool)

    @child
    var validator = SchemaValidator(schema: "order_v2")

    @child
    var writer = S3Writer(bucket: "processed-orders", batch_size: 200)

    // Wire the pipeline: producer -> enricher -> validator -> writer
    @flow
    fn topology(): none
        producer
            .pipe(enricher, concurrency: 8)
            .pipe(validator, concurrency: 4)
            .pipe(writer, concurrency: 2)
    end

end
```

### Multi-Node Distribution

```zinc
// Distribute pipeline stages across a cluster.

@distributed
module ClusterPipeline

    @node("ingest@host1")
    var reader = FileReader(path: "/data/incoming/")

    @node("process@host2")
    var transformer = DataTransformer(model: "v3")

    @node("process@host3")
    var transformer2 = DataTransformer(model: "v3")

    @node("store@host4")
    var writer = DatabaseWriter(pool_size: 20)

    @flow
    fn topology(): none
        reader
            .round_robin([transformer, transformer2])
            .pipe(writer)
    end

end
```

### Error Handling in Pipelines

```zinc
// Pipeline-aware error handling using Zinc's or {} blocks.
// Failed messages are routed to a dead-letter queue instead of crashing.

@processor(concurrency: 8)
fn transform(event: dict): dict
    var parsed = json.decode(event.data) or {
        // Decode failed — route to dead letter queue
        dead_letter.send(event, reason: "invalid JSON")
        return skip  // tells the pipeline to drop this message
    }

    var enriched = api.lookup(parsed["user_id"]) or {
        // API call failed — use cached data
        log.warn("API lookup failed, using cache")
        return parsed.merge(cache.get(parsed["user_id"], default: {}))
    }

    return parsed.merge(enriched)
end
```

---

## 8. Large Payload Efficiency — Zero-Copy at NiFi FlowFile Scale

A critical concern for NiFi-style workloads: FlowFiles are typically 1-8MB each. Copying
that data between pipeline stages would consume enormous time and memory. BEAM handles
this natively.

### BEAM Refc Binaries — Pointer Passing, Not Copying

Binaries larger than 64 bytes are stored on a **shared process-independent heap**. When
sent between processes, only an 8-byte pointer is copied — not the data:

```
Process A (Stage 1)          Shared Binary Heap
┌──────────┐                ┌─────────────────┐
│ ptr ──────┼───────────────│ 4MB FlowFile     │
└──────────┘       ┌───────│ content          │
Process B (Stage 2)│        └─────────────────┘
┌──────────┐       │
│ ptr ──────┼──────┘        Reference counted —
└──────────┘                freed when count = 0
```

Sending a 4MB FlowFile between pipeline stages costs ~8 bytes, not 4MB. Sub-binaries
(slicing) are also zero-copy — a slice creates a pointer+offset into the original allocation.

### Techniques at Scale

| Technique | How | Best for |
|---|---|---|
| **Refc binaries** (BEAM built-in) | Shared heap, pointer passing | 64B-100MB payloads |
| **ETS tables** | Shared-memory hash tables, all processes read/write | Lookup tables, state sharing |
| **Persistent terms** | Global immutable storage, zero-copy reads | Config, schemas, routing tables |
| **Content repository** (NiFi's approach) | Payload on disk, pass claim ID | >100MB, durability needed |
| **Apache Arrow / IPC** | Columnar memory layout, mmap | Structured/tabular data |
| **io_uring / sendfile** | Kernel zero-copy I/O | File→network without userspace copy |

### FlowFile Architecture on BEAM

```
FlowFile = {attributes (small dict), content_ref (pointer to refc binary)}

Stage 1 → Stage 2 → Stage 3
  Only attributes + 8-byte pointer move between stages
  Content stays in shared heap or on disk
  Each stage reads content via the ref, never copies it
```

This mirrors NiFi's own architecture — NiFi stores FlowFile content in a content repository
(disk-backed) and processors pass around `ContentClaim` objects (pointers). BEAM gives
the same pattern but **in-memory** with refc binaries, which is faster for sub-100MB payloads.

### The Python Problem

This is where Python struggles for pipeline workloads:

- **multiprocessing**: serializes (pickles) data between processes — full copies every time
- **threading (GIL)**: shared memory but no parallelism (until free-threaded 3.13+)
- **free-threaded Python**: shared memory threads with real parallelism, but loses BEAM's
  fault isolation (one bad thread can corrupt shared state)

A **hybrid architecture** could combine the best of both:

```
BEAM (orchestration, routing, fault tolerance)
  │
  ├─ Python workers via Ports (for ML/AI/data science libs)
  │   └─ Data passed via shared memory or Arrow buffers (zero-copy)
  │
  └─ BEAM processes (for routing, filtering, enrichment)
      └─ Data passed via refc binaries (zero-copy)
```

This gives BEAM's supervision and backpressure for pipeline orchestration, while Python
workers handle the heavy computation with access to NumPy/Polars/ML libraries.

---

## 9. Implementation Considerations

### Dual Backend Architecture

Zinc v1 already demonstrated that multiple backends are feasible (C# AOT + Go). Zinc v2
could follow the same pattern:

```
                          ┌─ Python codegen ─> .py (scripts, data science, ML)
.zn -> AST -> Typechecker ─┤
                          └─ BEAM codegen ──> .ex (pipelines, streaming, services)
```

The **AST and typechecker are shared** — only the codegen layer differs. This is the same
architecture that Gleam uses (Erlang + JavaScript backends) and that Kotlin uses
(JVM + Native + JS backends).

### How Would the Developer Choose?

Several options, from implicit to explicit:

1. **Annotation-driven** — `@pipeline` or `@service` annotations on modules trigger BEAM
   codegen. Unannotated code uses Python. This is the most ergonomic approach.

2. **CLI flag** — `zinc build --target beam` vs `zinc build --target python`. Simple but
   coarse-grained (entire project uses one backend).

3. **Project config** — `zinc.toml` specifies which modules use which backend:
   ```toml
   [targets]
   default = "python"

   [targets.beam]
   modules = ["pipelines/*", "services/*"]
   ```

4. **File extension** — `.zn` for Python, `.znp` for BEAM pipelines. Ugly but unambiguous.

The **annotation-driven approach** feels most natural for Zinc's philosophy. A single
project could have Python scripts for data analysis and BEAM services for pipeline
orchestration, with the compiler routing each module to the right backend.

### Interop Between Backends

The hard question: how does a BEAM pipeline call Python ML code, or vice versa?

**Option 1: Process boundary (Ports).** The BEAM pipeline spawns Python as a supervised
OS process, communicates via stdin/stdout (serialized as JSON or msgpack). Clean isolation,
but serialization overhead for large data.

**Option 2: Embedded Python (NIFs).** Use erlang-python or Pythonx to run Python in the
same BEAM node. Lower overhead, but Python's GIL becomes a bottleneck (mitigated by
free-threaded Python 3.13+).

**Option 3: Shared storage.** BEAM pipeline writes intermediate results to shared storage
(S3, Redis, local filesystem). Python workers read from shared storage. No direct
communication, but adds latency and operational complexity.

**Option 4: HTTP/gRPC boundary.** BEAM pipeline calls Python services over the network.
Clean separation, standard tooling, but adds network latency.

For Zinc, **Option 1 (Ports)** is the most natural starting point — it matches BEAM's
philosophy of process isolation, and the BEAM supervisor can restart crashed Python workers
automatically. For high-throughput scenarios, Option 2 with free-threaded Python could
be explored later.

### Development Effort Estimate

Building a BEAM backend would require:

| Component | Effort | Notes |
|---|---|---|
| Elixir codegen (AST -> .ex) | Large | Similar scope to the existing Go/C# codegen (~3K lines each) |
| OTP pattern codegen (GenServer, Supervisor) | Large | Mapping Zinc annotations to OTP boilerplate |
| Broadway/GenStage integration | Medium | Generating Broadway module definitions from Zinc annotations |
| BEAM-specific type mapping | Medium | Zinc types -> Elixir/Erlang types |
| Port-based Python interop | Medium | Supervised Python worker pool |
| Testing infrastructure | Medium | E2E tests that compile and run on BEAM |
| Documentation | Small | New getting-started guide for pipeline target |

This is a **major undertaking** — comparable to building the original Go or C# backends.
It would only be justified if pipeline/streaming workloads become a primary use case for
Zinc.

### Alternative: BEAM-Inspired Patterns in Python

A lighter-weight approach: instead of targeting the BEAM, implement BEAM-inspired patterns
in pure Python:

- **Lightweight processes** -> Python free-threaded workers (3.13+)
- **Mailbox messaging** -> `queue.Queue` per worker
- **Supervision trees** -> A supervisor class that restarts failed workers
- **Backpressure** -> Bounded queues with demand signaling
- **GenStage pattern** -> Producer-consumer with demand protocol

This would give Zinc some of the BEAM's pipeline ergonomics without the complexity of a
second runtime. The downside: Python's concurrency model (even free-threaded) will never
match the BEAM's ability to run millions of isolated processes with preemptive scheduling.

---

## Summary

| Approach | Concurrency | Fault Tolerance | Ecosystem | Complexity |
|---|---|---|---|---|
| **Pure Python** (current) | Limited (GIL, free-threading helps) | Manual (try/except) | Full Python ecosystem | Low |
| **BEAM backend** (Approach A) | Excellent (millions of processes) | Excellent (supervisors) | Limited (no ML/AI) | High |
| **BEAM orchestrator + Python workers** (Approach B) | Good (BEAM manages, Python works) | Good (BEAM supervises Python) | Full Python ecosystem | Medium-High |
| **BEAM-inspired patterns in Python** | Moderate (free-threaded) | Moderate (custom supervisors) | Full Python ecosystem | Medium |

### Recommendation

The BEAM VM is the gold standard for concurrent, fault-tolerant pipeline processing. Its
process model is a near-perfect match for NiFi/Spark-style data flow architectures. However,
building a full BEAM backend is a major investment that only makes sense if Zinc targets
pipeline/streaming workloads as a primary use case.

**Short term:** Implement BEAM-inspired patterns (supervision, backpressure, demand-driven
producers) in pure Python, targeting free-threaded Python 3.13+. This gives Zinc pipeline
ergonomics without a second runtime.

**Medium term:** If pipeline workloads prove popular, explore **Approach B** (BEAM
orchestrator with Python workers via Ports). This gets BEAM's fault tolerance and
distribution while keeping Python's ecosystem for data transformation.

**Long term:** If the BEAM backend proves valuable, consider **Approach A** (full Elixir
codegen) for pure-pipeline workloads that do not need Python's ML/AI ecosystem. Gleam's
maturation as a typed BEAM language (currently v1.14.0) could make it an attractive
transpile target by then.

---

## References and Further Reading

- [The BEAM Book — Understanding the Erlang Runtime System](https://blog.stenmans.org/theBeamBook/)
- [Deep Diving Into the Erlang Scheduler (AppSignal, 2024)](https://blog.appsignal.com/2024/04/23/deep-diving-into-the-erlang-scheduler.html)
- [How the Erlang BEAM VM Achieves Low Latency](https://thamizhelango.medium.com/how-the-erlang-beam-vm-achieves-low-latency-architecture-insights-8e47d36e0333)
- [BEAM and JVM Virtual Machines — Comparing and Contrasting (Erlang Solutions)](https://www.erlang-solutions.com/blog/beam-jvm-virtual-machines-comparing-and-contrasting/)
- [BEAM VM — Wikipedia](https://en.wikipedia.org/wiki/BEAM_(Erlang_virtual_machine))
- [How Much Memory Is Needed to Run 1M Erlang Processes?](https://hauleth.dev/post/beam-process-memory-usage/)
- [Elixir Broadway — Official Site](https://elixir-broadway.org/)
- [Broadway GitHub — Dashbit](https://github.com/dashbitco/broadway)
- [GenStage — Producer and Consumer Actors with Backpressure](https://github.com/elixir-lang/gen_stage)
- [Understanding Elixir's Broadway (Samuel Mullen)](https://samuelmullen.com/articles/understanding-elixirs-broadway)
- [Constructing Data Processing Workflows with Elixir and Broadway (SoftwareMill)](https://softwaremill.com/constructing-effective-data-processing-workflows-using-elixir-and-broadway/)
- [Gleam Programming Language — Official Site](https://gleam.run/)
- [Gleam v1.14.0 Release Notes](https://gleam.run/news/the-happy-holidays-2025-release/)
- [Interoperability in 2025: Beyond the Erlang VM (Elixir Blog)](https://elixir-lang.org/blog/2025/08/18/interop-and-portability/)
- [erlang-python — Execute Python from Erlang](https://github.com/benoitc/erlang-python)
- [A Brief BEAM Primer — Erlang.org](https://www.erlang.org/blog/a-brief-beam-primer/)
- [Erlang Code Loading Documentation](https://www.erlang.org/doc/system/code_loading.html)
- [Unique Resiliency of the Erlang VM (InfoQ)](https://www.infoq.com/presentations/resilience-beam-erlang-otp/)
