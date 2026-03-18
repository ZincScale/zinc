# Exploration: Apache Beam as a Pipeline Runtime for Zinc v2

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

> **Status: EXPLORATORY** — This document captures research and ideas.
> It is not a commitment to implement any of the features described here.
> Last updated: 2026-03-18.

---

## 1. What is Apache Beam?

Apache Beam is an **open-source, unified programming model** for defining both batch and streaming data processing pipelines. The key insight is that batch is just a special case of streaming (a bounded stream), so one API can describe both.

### Core abstractions

| Concept | What it is |
|---|---|
| **Pipeline** | The top-level container that encapsulates the entire data processing job — reads, transforms, writes. |
| **PCollection** | A distributed, potentially unbounded dataset. Analogous to an RDD in Spark, but with built-in windowing and watermark semantics for streaming. |
| **PTransform** | A data processing operation applied to a PCollection, producing a new PCollection. This is the unit of composition — `Map`, `Filter`, `GroupByKey`, `Combine`, `Flatten`, `Partition`, and custom transforms. |
| **Runner** | The execution engine. Beam itself does not execute pipelines — it delegates to a runner. |
| **I/O Connector** | Read/write adapters for external systems (Kafka, BigQuery, Parquet, JDBC, file systems, Pub/Sub, etc.). |
| **Windowing** | How unbounded data is sliced into finite chunks for aggregation — fixed, sliding, session, or global windows. |
| **Watermark** | A heuristic for "how complete is the data in this window?" — enables late-data handling. |

### The runner ecosystem

Beam's portability story means you write once, run anywhere:

| Runner | Type | Notes |
|---|---|---|
| **DirectRunner** | Local, in-process | For development and testing. Single-threaded, no distribution. |
| **Apache Flink** | Distributed | Strong streaming semantics, low latency, exactly-once. |
| **Apache Spark** | Distributed | Mature batch engine, streaming via Spark Structured Streaming. |
| **Google Dataflow** | Managed cloud | Auto-scaling, fully managed, pay-per-use. The original Beam runner. |
| **Apache Samza** | Distributed | LinkedIn's stream processor, Kafka-native. |
| **Prism** | Local/portable | Newer runner for local testing with better portability support. |

As of Beam 2.71.0 (early 2026), the Python SDK supports Python 3.10 through 3.13, with 3.14 on the roadmap. Python 3.9 support was dropped in late 2025.

---

## 2. How Beam Works in Python

### Pipeline construction

A Beam pipeline in Python reads like a chain of transformations connected by the `|` (pipe) operator:

```python
import apache_beam as beam

with beam.Pipeline() as pipeline:
    (
        pipeline
        | "ReadLines" >> beam.io.ReadFromText("input.txt")
        | "SplitWords" >> beam.FlatMap(lambda line: line.split())
        | "PairWithOne" >> beam.Map(lambda word: (word, 1))
        | "GroupAndSum" >> beam.CombinePerKey(sum)
        | "WriteResults" >> beam.io.WriteToText("output.txt")
    )
```

Key observations:
- The `|` operator feeds a PCollection into a PTransform.
- The `>>` operator is just labeling (for debugging/monitoring).
- Each step produces a new PCollection — fully immutable, no mutation.
- The pipeline is lazily constructed — nothing executes until `pipeline.run()` (or the `with` block exits).

### Common transforms

| Beam transform | Zinc collection equivalent | Purpose |
|---|---|---|
| `beam.Map(fn)` | `.map(fn)` | 1:1 element transformation |
| `beam.FlatMap(fn)` | `.flat_map(fn)` | 1:N element transformation |
| `beam.Filter(fn)` | `.filter(fn)` | Predicate-based filtering |
| `beam.CombinePerKey(fn)` | `.group_by(key).reduce(fn)` | Grouped aggregation |
| `beam.CombineGlobally(fn)` | `.reduce(fn)` | Global aggregation |
| `beam.GroupByKey()` | `.group_by(key)` | Group elements by key |
| `beam.Distinct()` | `.distinct()` | Remove duplicates |
| `beam.Count.PerElement()` | `.count_by(fn)` | Count occurrences |
| `beam.Top.Of(n, key=fn)` | `.sort_by(fn).take(n)` | Top-N elements |

### I/O connectors (Python SDK)

The Python SDK has mature connectors for:
- **File systems**: Text, Avro, Parquet, TFRecord, CSV, JSON
- **Google Cloud**: BigQuery, Pub/Sub, Cloud Storage, Bigtable, Datastore
- **Databases**: JDBC (via cross-language), MongoDB
- **Messaging**: Kafka (via cross-language transforms, with native Python support in progress)

Cross-language transforms let Python pipelines use Java-implemented I/O connectors (like Kafka) through Beam's portability framework. This works but adds operational complexity.

---

## 3. What Zinc Could Do

### The core idea

Zinc v2 already plans to dispatch collection chains to different backends based on data shape:

| Data shape | Backend |
|---|---|
| Small in-memory list | Python list comprehension |
| Structured tabular data | Polars |
| Numeric arrays | NumPy / Numba |
| **Large/distributed data** | **Apache Beam (new)** |

The insight: Zinc's `.filter().map().reduce()` chain syntax is *already* a pipeline description. The transpiler just needs to recognize when the data source implies distributed processing and emit Beam PTransforms instead of local Python.

### When to activate Beam dispatch

The transpiler could detect Beam-appropriate contexts by:

1. **Explicit data source** — if the pipeline reads from `File.stream()`, `Kafka.subscribe()`, `BigQuery.query()`, or similar I/O sources, emit Beam.
2. **Annotation** — a `@pipeline` decorator or `pipeline` block that explicitly opts into distributed execution.
3. **Configuration** — a project-level setting like `runtime: beam` in `zinc.toml`.

Option 2 (explicit annotation) is the safest starting point — no magic, no surprises.

### What the transpiler would generate

A Zinc collection chain like:

```zinc
var results = orders
    .filter(o -> o.status == "active")
    .map(o -> { "id": o.id, "total": o.amount * o.quantity })
    .reduce(0, (acc, o) -> acc + o["total"])
```

In local mode, this transpiles to a Python list comprehension or generator chain. In Beam mode, the same chain would transpile to:

```python
import apache_beam as beam

with beam.Pipeline(options=pipeline_options) as p:
    results = (
        p
        | "ReadOrders" >> beam.io.ReadFromSource(orders_source)
        | "FilterActive" >> beam.Filter(lambda o: o["status"] == "active")
        | "MapToTotals" >> beam.Map(lambda o: {"id": o["id"], "total": o["amount"] * o["quantity"]})
        | "SumTotals" >> beam.CombineGlobally(lambda values: sum(v["total"] for v in values))
    )
```

The Zinc developer writes the same chain. The transpiler handles the Beam boilerplate.

---

## 4. Comparison: NiFi, Spark, and Beam

### Different tools, different philosophies

| Dimension | Apache NiFi | Apache Spark | Apache Beam |
|---|---|---|---|
| **Primary role** | Data flow / integration / routing | Distributed compute engine | Unified programming model (abstraction layer) |
| **Interface** | Visual drag-and-drop UI | Programmatic API (Scala/Java/Python) | Programmatic API (Java/Python/Go) |
| **Execution** | Runs its own cluster | Runs on its own cluster (YARN, K8s, standalone) | Delegates to a runner (Flink, Spark, Dataflow, etc.) |
| **Batch vs stream** | Primarily stream / micro-batch | Batch-first, streaming via Structured Streaming | Unified model — same API for both |
| **Data model** | FlowFiles (content + attributes) | RDDs / DataFrames / Datasets | PCollections |
| **Strengths** | Visual design, provenance tracking, back-pressure | Rich ML/SQL ecosystem, mature, large community | Portability, unified batch/stream, runner flexibility |
| **Weaknesses** | Limited computation, no ML, not for heavy transforms | Two separate APIs for batch/stream, vendor lock-in | Smaller ecosystem, runner feature gaps, extra abstraction layer |

### What Beam unifies

The key value proposition of Beam over Spark or NiFi:

1. **One API for batch and stream** — Spark has separate batch (DataFrame) and streaming (Structured Streaming) APIs. Beam has one.
2. **Runner portability** — write once, deploy to Flink for low-latency streaming, Spark for batch, Dataflow for managed cloud. No code changes.
3. **Windowing as a first-class concept** — event-time processing, watermarks, and triggers are built into the model, not bolted on.

### Where NiFi still wins

NiFi's strengths are complementary to Beam, not competitive:
- **Visual pipeline design** for operations teams
- **Data provenance** — full lineage tracking of every datum
- **Back-pressure** — automatic flow control
- **Protocol adapters** — hundreds of processors for obscure protocols

A realistic architecture uses NiFi for ingestion/routing and Beam for heavy computation.

### Where Spark still wins

- **ML ecosystem** — MLlib, Spark ML, deep learning integrations
- **SQL engine** — Spark SQL is battle-tested at massive scale
- **Community size** — far larger community and job market
- **Interactive analysis** — Spark notebooks (Databricks, Jupyter) are ubiquitous

---

## 5. Zinc Syntax Possibilities

### Pipeline block

A dedicated `pipeline` block makes distributed intent explicit:

```zinc
pipeline OrderAnalytics
    var source = Kafka.subscribe("orders-topic", schema: OrderEvent)

    var active_totals = source
        .filter(o -> o.status == "active")
        .window(fixed: 5.minutes)
        .map(o -> { "region": o.region, "total": o.amount })
        .group_by(o -> o["region"])
        .reduce((a, b) -> a + b)

    active_totals.write_to(BigQuery.table("analytics.regional_totals"))
end
```

This transpiles to a full Beam pipeline with:
- Kafka source via `beam.io.ReadFromKafka`
- `beam.Filter`, `beam.Map`, `beam.GroupByKey`, `beam.CombinePerKey`
- Fixed windowing via `beam.WindowInto`
- BigQuery sink via `beam.io.WriteToBigQuery`

### Pipeline functions

For reusable pipeline stages, regular functions work:

```zinc
fn enrich_orders(orders: PCollection[Order]): PCollection[EnrichedOrder]
    return orders
        .filter(o -> o.amount > 0)
        .map(o -> EnrichedOrder(
            id: o.id,
            total: o.amount * o.quantity,
            tier: classify_tier(o.amount)
        ))
end

fn classify_tier(amount: float): str
    match amount
        case x if x > 10000 -> "enterprise"
        case x if x > 1000 -> "business"
        case _ -> "standard"
    end
end
```

### Streaming with windows and triggers

```zinc
pipeline SensorMonitor
    var readings = Kafka.subscribe("sensor-readings", schema: SensorReading)

    var alerts = readings
        .window(sliding: 1.minute, every: 10.seconds)
        .group_by(r -> r.sensor_id)
        .map((sensor_id, window) -> {
            "sensor_id": sensor_id,
            "avg_temp": window.map(r -> r.temperature).average(),
            "max_temp": window.map(r -> r.temperature).max()
        })
        .filter(agg -> agg["avg_temp"] > 100.0)

    alerts.write_to(PubSub.topic("temperature-alerts"))
    alerts.write_to(BigQuery.table("monitoring.alerts"))
end
```

### Batch file processing

```zinc
pipeline DailyReport
    var transactions = Parquet.read("gs://data/transactions/*.parquet")

    var summary = transactions
        .filter(t -> t.date == today())
        .group_by(t -> t.category)
        .map((category, txns) -> {
            "category": category,
            "count": txns.count(),
            "total": txns.map(t -> t.amount).sum(),
            "average": txns.map(t -> t.amount).average()
        })
        .sort_by(s -> s["total"], descending: true)

    summary.write_to(CSV.file("reports/daily_{today()}.csv"))
    summary.write_to(Console.table())
end
```

### Runner configuration

Runner selection lives in `zinc.toml`, not in code:

```toml
[pipeline]
runner = "direct"          # "direct", "flink", "spark", "dataflow"
parallelism = 4

[pipeline.flink]
master = "localhost:8081"
parallelism = 16

[pipeline.dataflow]
project = "my-gcp-project"
region = "us-central1"
temp_location = "gs://my-bucket/temp"
```

The same Zinc pipeline code runs locally with DirectRunner during development and on Flink/Dataflow in production — no code changes, just config.

---

## 6. Free-Threaded Python Considerations

### Current state (2026)

Python 3.13 introduced the experimental free-threaded build (no-GIL). As of early 2026:

- **Apache Beam supports Python 3.13** but there is no documented testing or optimization specifically for the free-threaded (no-GIL) build.
- Many C extensions that Beam depends on (e.g., grpcio, pyarrow, protobuf) need to be rebuilt and tested for free-threaded compatibility.
- The free-threaded build is still considered experimental by CPython.

### How Beam sidesteps the GIL problem

Beam's architecture already mitigates GIL concerns through its design:

1. **Multi-process, not multi-thread** — Beam's Python SDK uses multi-process parallelism via the Fn API harness. Each worker process has its own GIL.
2. **Runner-level parallelism** — distribution across machines is handled by the runner (Flink/Spark/Dataflow), not by Python threads.
3. **Cross-language transforms** — performance-critical I/O connectors can use Java implementations via the portability framework, bypassing Python entirely.

### Implications for Zinc

| Scenario | GIL impact | Beam behavior |
|---|---|---|
| Local DirectRunner | GIL limits CPU parallelism | Mostly single-threaded anyway; free-threading helps Map/Filter parallelism |
| Distributed runner (Flink/Spark) | GIL irrelevant | Each worker is a separate process |
| I/O-bound pipelines | GIL mostly released during I/O | Free-threading provides modest improvement |
| CPU-heavy transforms | GIL is the bottleneck | Free-threading provides significant improvement for DirectRunner |

### Zinc's strategy

For Zinc v2's pipeline support:

1. **Start with process-based parallelism** — this works today, GIL or no GIL.
2. **Test free-threaded builds** — as Beam and its dependencies certify no-GIL compatibility, Zinc can recommend the free-threaded Python runtime for local execution.
3. **Hybrid dispatch** — for local execution without Beam, Zinc already plans thread-pool dispatch for collection chains. Free-threaded Python makes this actually parallel for CPU-bound work.

The distributed runners (Flink, Spark, Dataflow) make the GIL discussion irrelevant for production workloads — parallelism happens at the process and machine level.

---

## 7. Implementation Options

### Option A: Thin wrapper — Zinc syntax sugar over Beam

**Approach:** The transpiler recognizes `pipeline` blocks and emits standard `apache_beam` Python code. Zinc provides nicer syntax; Beam provides all runtime behavior.

**Pros:**
- Minimal implementation effort — just codegen, no runtime
- Full Beam ecosystem available (all runners, all connectors)
- Users can read/debug the generated Python
- Upgrades to Beam are free — just regenerate

**Cons:**
- Zinc is tied to Beam's API surface
- Error messages come from Beam, not Zinc
- Some Beam concepts (windowing, triggers, side inputs) are hard to simplify

### Option B: Collection chain auto-promotion

**Approach:** The transpiler detects when a collection chain operates on a Beam source (Kafka, BigQuery, etc.) and automatically emits Beam PTransforms instead of local Python.

```zinc
# This looks like normal collection code:
var results = Kafka.subscribe("orders")
    .filter(o -> o.amount > 100)
    .map(o -> o.amount)
    .reduce(0, (a, b) -> a + b)

# But because the source is Kafka, the transpiler emits Beam code.
```

**Pros:**
- Seamless experience — same syntax for local and distributed
- No new keywords or blocks needed
- Zinc's "the transpiler works for you" philosophy

**Cons:**
- Magic — users may not realize they're running a distributed pipeline
- Hard to debug when the local-to-distributed transition causes issues
- Not all local operations map cleanly to Beam transforms

### Option C: Explicit mode annotation

**Approach:** A `@beam` decorator or `pipeline` keyword opts a function into Beam codegen. Everything inside uses Beam. Everything outside is normal Python.

```zinc
@beam(runner: "auto")
fn process_orders(): None
    var orders = Kafka.subscribe("orders-topic")
    var totals = orders
        .filter(o -> o.status == "active")
        .map(o -> o.amount)
        .reduce(0, (a, b) -> a + b)
    totals.write_to(Console.print())
end
```

**Pros:**
- Explicit — no surprises about what runs where
- Clear boundary between local and distributed code
- Easy to document and teach

**Cons:**
- Two mental models — "inside @beam" vs "outside @beam"
- Users must learn when to opt in

### Recommended path

**Start with Option C (explicit annotation), evolve toward Option A (pipeline blocks).**

Rationale:
- Explicit is better than implicit — especially for distributed systems where debugging is hard.
- `pipeline` blocks as a first-class Zinc construct (Option A) are cleaner than decorators, but decorators work with existing infrastructure.
- Option B (auto-promotion) is appealing but too magical for a v1. Could be a future refinement once users understand the model.

### Detection: local vs distributed

The transpiler could use simple heuristics:

| Signal | Execution mode |
|---|---|
| Source is a local list/file | Local Python (comprehension/generator) |
| Source is `Kafka`, `PubSub`, `BigQuery`, etc. | Beam pipeline |
| `@beam` or `pipeline` block | Always Beam |
| `zinc.toml` has `[pipeline]` config | Project-level default |
| Collection size exceeds threshold | *Future* — runtime dispatch (ambitious) |

### Implementation phases

1. **Phase 1: Research** (this document) — understand Beam's model and API surface.
2. **Phase 2: Prototype** — hand-write the Beam Python code that Zinc would generate for 3-5 common patterns. Verify it runs on DirectRunner.
3. **Phase 3: Codegen** — add `pipeline` block parsing to the Zinc lexer/parser. Add Beam codegen to the Python transpiler.
4. **Phase 4: I/O connectors** — map Zinc's `Kafka.subscribe()`, `BigQuery.query()`, etc. to Beam I/O transforms.
5. **Phase 5: Runner config** — wire `zinc.toml` pipeline settings to Beam `PipelineOptions`.
6. **Phase 6: Streaming** — add windowing, triggers, and watermark syntax to Zinc.

---

## Open Questions

1. **Dependency weight** — `apache-beam` is a heavy dependency (~200 MB with extras). Should Zinc make it optional (`zinc install beam`)?
2. **Error mapping** — Beam errors are notoriously opaque. Can Zinc provide better error messages by wrapping or translating them?
3. **Testing story** — how do users test Beam pipelines locally? DirectRunner works but is slow. Should Zinc provide a mock pipeline runner?
4. **Schema enforcement** — Beam has its own schema system. How does it interact with Zinc's type system?
5. **Side inputs** — Beam's side-input pattern (broadcast joins) doesn't map naturally to collection chains. How should Zinc expose this?
6. **Stateful processing** — Beam supports stateful DoFns for sessionization, dedup, etc. This doesn't fit the functional chain model. Dedicated syntax?

---

## References

- [Apache Beam Programming Guide](https://beam.apache.org/documentation/programming-guide/)
- [Apache Beam Python SDK](https://beam.apache.org/documentation/sdks/python/)
- [Apache Beam Python SDK Quickstart](https://beam.apache.org/get-started/quickstart/python/)
- [Apache Beam Capability Matrix (Runner Comparison)](https://beam.apache.org/documentation/runners/capability-matrix/)
- [Apache Beam I/O Connectors](https://beam.apache.org/documentation/io/connectors/)
- [Python SDK Roadmap](https://beam.apache.org/roadmap/python-sdk/)
- [Beam vs Spark Comparison](https://quix.io/blog/beam-vs-spark-big-data-solutions-compared)
- [Apache Beam vs NiFi on StackShare](https://stackshare.io/stackups/apache-beam-vs-apache-nifi)
- [Python 3.13 Free-Threading Guide](https://py-free-threading.github.io/running-gil-disabled/)
- [Beam Python SDK on PyPI (v2.71.0)](https://pypi.org/project/apache-beam/)
- [Zinc v2 Design Document](design-zinc-v2-python.md)
