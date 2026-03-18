<!-- Licensed under the Apache License, Version 2.0 -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Exploration: Pathway Streaming Pipelines in Zinc v2

**Status:** Exploratory -- not committed to implementation
**Date:** 2026-03-18
**Context:** Zinc v2 transpiles `.zn` files to `.py` files. Pathway is a Python API over a Rust streaming engine. This is a natural fit -- and it fills the real-time streaming gap that Dagster does not cover.

---

## 1. What is Pathway

Pathway is a **unified batch + streaming data processing framework** with a Python API and a high-performance Rust engine based on Differential Dataflow. It handles both real-time streaming and batch workloads with the same code -- you write a pipeline once and it works on static files or live Kafka streams.

### Architecture

```
Python API (user code)
    |
    v
Pathway Runtime (Rust engine)
    |-- Differential Dataflow core
    |-- Incremental computation
    |-- Multithreaded execution
    |-- Persistent state management
```

The key insight: your Python code defines the **dataflow graph**. The Rust engine executes it with native performance, incremental recomputation, and multithreading. You never leave Python, but you get Rust-level throughput.

### Core Concepts

| Concept | What It Is |
|---|---|
| **Table** | The fundamental data structure. An append-only, versioned collection of rows. Every operation produces a new table. Think of it as a live DataFrame that updates as data arrives. |
| **Schema** | A typed class defining table columns. Uses Python type annotations (`pw.Schema` subclass). |
| **Connector (Input)** | Reads data into the system: `pw.io.kafka.read()`, `pw.io.csv.read()`, `pw.io.s3.read()`, `pw.io.postgres.read()`. Supports 300+ sources via Airbyte bridge. |
| **Connector (Output)** | Writes results out: `pw.io.kafka.write()`, `pw.io.jsonlines.write()`, `pw.io.postgres.write()`. |
| **Transformer** | A function or method chain that transforms tables: `.filter()`, `.select()`, `.join()`, `.groupby()`, `.reduce()`, `.windowby()`. |
| **Temporal Join** | Join tables based on time -- e.g., join events with the latest known state at event time. Built-in, not bolted on. |
| **Windowing** | Time-based grouping: tumbling windows, sliding windows, session windows. First-class temporal operations. |
| **Reducer** | Aggregation functions: `pw.reducers.sum`, `pw.reducers.count`, `pw.reducers.avg`, `pw.reducers.min`, `pw.reducers.max`, `pw.reducers.sorted_tuple`. |
| **UDF** | User-defined functions via `@pw.udf` decorator. Run custom Python logic on each row. Can be sync or async. |
| **pw.run()** | Starts the Rust engine. Blocks until all input connectors are exhausted (batch) or runs indefinitely (streaming). |

### How It Differs from Dagster

This is the critical distinction: **Dagster and Pathway solve different problems**.

| Concern | Dagster | Pathway |
|---|---|---|
| **Core job** | Orchestration -- when to run, what depends on what | Computation -- how to process data efficiently |
| **Model** | Asset graph (DAG of data artifacts) | Dataflow graph (stream of table transformations) |
| **Latency** | Minutes to hours (batch scheduling) | Milliseconds to seconds (streaming) |
| **Data movement** | Triggers external tools; data lives in warehouses/lakes | Processes data through its own engine; data flows through |
| **State** | Tracks metadata (what ran, when, what produced) | Tracks data state (incremental computation, temporal joins) |
| **Use case** | "Run this ETL every hour" | "React to every event as it arrives" |

**They are complementary, not competing.** Dagster tells Pathway pipelines when to start, monitors their health, tracks their outputs as assets. Pathway does the actual data crunching at speed.

### Current State (2025-2026)

- **License:** BSL 1.1 -- unlimited non-commercial use, most commercial use free. Converts to Apache 2.0 after 4 years.
- **GitHub:** 60K+ stars, active development
- **Install:** `pip install -U pathway` (Python 3.10+, Linux and macOS)
- **Engine:** Rust-based, built on Differential Dataflow (Timely Dataflow lineage from Frank McSherry's research at Microsoft)
- **Connectors:** Kafka, S3, PostgreSQL, Google Drive, SharePoint, Airbyte (300+ sources), custom Python connectors
- **Enterprise:** Distributed computing, exactly-once guarantees, cloud deployment support

---

## 2. Why Pathway for Zinc

### The Fit

Pathway is **pure Python API over a Rust engine**. Since Zinc v2 transpiles `.zn` to `.py`, it can generate Pathway code directly. No special runtime, no plugins, no FFI boundary to cross.

| Zinc Principle | Pathway Alignment |
|---|---|
| "It's just Python underneath" | Pathway pipelines are Python scripts. Zinc generates them. |
| Zero ceremony | Pathway's API is already clean. Zinc makes it cleaner with typed schemas and pipeline keywords. |
| Enforced types | Pathway uses `pw.Schema` with Python type hints. Zinc's type enforcement catches schema mismatches at transpile time. |
| The transpiler works for you | Zinc auto-generates `pw.Schema` classes, wires connectors, injects `pw.run()`. |
| Free-threaded Python | Pathway's Rust engine already bypasses the GIL. Free-threaded Python (3.13+) helps UDFs run in parallel. |

### What Zinc Adds Over Raw Pathway Python

1. **Typed schemas without class boilerplate** -- Zinc infers schema types from pipeline declarations
2. **Pipeline-as-keyword** -- `pipeline` block instead of loose function calls
3. **Connector validation** -- Zinc type-checks that connector schemas match downstream transformations
4. **Auto-generated `pw.run()`** -- Zinc injects the engine start call so scripts don't forget it
5. **Unified import** -- Zinc resolves `kafka`, `s3`, `postgres` to proper `pw.io.*` connectors

### The Streaming Gap

The [Dagster exploration](exploration-dagster-pipelines.md) identified a clear limitation:

> **"When Dagster Is Not the Right Tool: Real-time streaming. Dagster is batch-oriented. For sub-second latency, use Kafka/Flink/NiFi."**

Pathway fills exactly this gap. With both Dagster and Pathway support, Zinc covers the full spectrum:

- **Dagster**: Batch orchestration, asset management, scheduling, lineage tracking
- **Pathway**: Real-time streaming, incremental computation, sub-second latency, temporal operations

---

## 3. NiFi Concepts to Pathway Mapping

For teams moving from NiFi-style data flow to Pathway:

| NiFi Concept | Pathway Equivalent | Notes |
|---|---|---|
| **Processor** | Transformer (`.filter()`, `.select()`, `.join()`, UDF) | NiFi processors transform data in-flight. Pathway transformers do the same on tables. |
| **FlowFile** | Table row | In NiFi, data flows as FlowFiles with attributes + content. In Pathway, data flows as rows in tables with typed columns. |
| **FlowFile Attributes** | Table columns | NiFi attributes are key-value metadata. Pathway columns are typed fields on a schema. |
| **Connection (queue)** | Table reference (implicit) | NiFi connects processors via bounded queues. Pathway connects transformers via table references -- output of one is input to the next. |
| **Back-Pressure** | Engine-managed (Rust runtime) | NiFi connections have back-pressure thresholds. Pathway's Rust engine handles backpressure internally via Timely Dataflow's progress tracking. No manual configuration needed. |
| **Process Group** | Pipeline block / module | Logical grouping. In Zinc, a `pipeline` block or separate `.zn` file. |
| **Controller Service** | Resource / connector config | Shared services (database pools, Kafka clients). Configured once, used across transformers. |
| **Provenance** | Temporal versioning | NiFi tracks FlowFile history. Pathway tracks temporal versions of every row -- you can query "what was the state at time T." |
| **Input/Output Port** | Input/output connector | `pw.io.kafka.read()` / `pw.io.kafka.write()`. Data enters and exits the pipeline through connectors. |
| **Funnel** | `pw.Table.concat()` | Merge multiple streams into one table. |
| **Bulletin Board** | Monitoring dashboard | Pathway includes a built-in dashboard tracking connector throughput and system latency. |

### Key Difference: Both Are Push-Based

Unlike Dagster (which is pull-based -- the orchestrator decides when to run), **both NiFi and Pathway are push-based**. Data flows through as it arrives. This makes Pathway a much more natural migration target for NiFi streaming workloads than Dagster.

| | NiFi | Pathway |
|---|---|---|
| **Model** | Push (FlowFiles flow through processors) | Push (rows flow through transformers) |
| **Latency** | Milliseconds | Milliseconds |
| **State** | Content + attribute repositories | Incremental state in Rust engine |
| **Scaling** | NiFi cluster (JVM-based) | Multithreaded Rust + distributed (enterprise) |
| **Language** | Java (processors) + XML config | Python (API) + Rust (engine) |

---

## 4. Pathway vs Dagster -- Complementary Tools

### Different Problems, Different Layers

```
                    +-----------------------+
                    |   Dagster (Orchestrate)|
                    |   "When to run"       |
                    |   "What depends on    |
                    |    what"              |
                    +-----------+-----------+
                                |
                    triggers / monitors
                                |
                    +-----------v-----------+
                    |   Pathway (Compute)   |
                    |   "How to process"    |
                    |   "React to events"   |
                    +-----------------------+
```

### Side-by-Side

| | Dagster | Pathway |
|---|---|---|
| **You use it when...** | You need to schedule ETL, track data lineage, manage dependencies between datasets | You need to process events in real-time, react to Kafka streams, compute incremental aggregations |
| **Data model** | Assets (named data artifacts in external storage) | Tables (live, versioned, in-engine data) |
| **Processing** | Delegates to external tools (DuckDB, Spark, dbt) | Processes data itself (Rust engine) |
| **Latency** | Minutes (batch) | Milliseconds (streaming) |
| **Incremental** | Partition-based (rerun partitions that changed) | Row-based (recompute only affected outputs) |
| **Deployment** | `dagster dev` / Dagster+ cloud | `python pipeline.py` / Docker / K8s |

### Using Both Together

A realistic enterprise setup:

```
Kafka topic (events) ----> Pathway pipeline (real-time processing)
                                |
                                v
                           PostgreSQL (processed results)
                                |
                                v
                      Dagster (hourly asset materialization)
                                |
                                v
                           DuckDB/Snowflake (analytics warehouse)
```

Dagster can trigger Pathway pipelines as ops, monitor their health via sensors, and track their outputs as assets. Pathway does the heavy lifting on the data.

### How Zinc Targets Both

```zinc
// real_time.zn — Pathway pipeline (streaming)
@pipeline(engine: "pathway")
fn process_events()
    var events = kafka.read("events-topic", schema: EventSchema)
    var enriched = events
        .filter(e -> e.event_type == "purchase")
        .join(products, events.product_id == products.id)
        .select(
            user_id: events.user_id,
            product_name: products.name,
            amount: events.amount,
            timestamp: events.timestamp
        )
    postgres.write(enriched, "enriched_purchases")
end

// batch_analytics.zn — Dagster asset (batch)
@asset(group: "analytics", schedule: "0 * * * *")
fn hourly_revenue(db: DuckDB)
    db.execute("
        INSERT INTO hourly_revenue
        SELECT date_trunc('hour', timestamp) as hour,
               SUM(amount) as total
        FROM enriched_purchases
        WHERE timestamp > NOW() - INTERVAL '2 hours'
        GROUP BY 1
    ")
end
```

The Zinc transpiler recognizes `@pipeline(engine: "pathway")` and generates Pathway Python code. It recognizes `@asset` and generates Dagster Python code. Same language, same project, two engines.

---

## 5. Zinc Syntax for Pathway Pipelines

### Pathway Python (Before)

```python
import pathway as pw

class EventSchema(pw.Schema):
    user_id: str
    event_type: str
    product_id: int
    amount: float
    timestamp: str

class ProductSchema(pw.Schema):
    id: int
    name: str
    category: str
    price: float

# Read from Kafka
events = pw.io.kafka.read(
    rdkafka_settings={"bootstrap.servers": "localhost:9092", "group.id": "zinc-pipeline"},
    topic="user-events",
    schema=EventSchema,
    format="json",
    autocommit_duration_ms=1000,
)

# Read product catalog from PostgreSQL (CDC)
products = pw.io.postgres.read(
    connection_string="postgresql://user:pass@localhost:5432/shop",
    schema=ProductSchema,
    table_name="products",
)

# Filter and enrich
purchases = events.filter(pw.this.event_type == "purchase")
enriched = purchases.join(
    products,
    pw.left.product_id == pw.right.id,
).select(
    user_id=pw.left.user_id,
    product_name=pw.right.name,
    category=pw.right.category,
    amount=pw.left.amount,
    timestamp=pw.left.timestamp,
)

# Windowed aggregation -- revenue per category per hour
hourly_revenue = enriched.windowby(
    enriched.timestamp,
    window=pw.temporal.tumbling(duration=pw.Duration(hours=1)),
    behavior=pw.temporal.common_behavior(cutoff=pw.Duration(minutes=5)),
).reduce(
    category=pw.reducers.argmin(pw.this.timestamp, pw.this.category),
    total_revenue=pw.reducers.sum(pw.this.amount),
    order_count=pw.reducers.count(),
)

# Write results
pw.io.kafka.write(enriched, rdkafka_settings={"bootstrap.servers": "localhost:9092"}, topic_name="enriched-purchases")
pw.io.jsonlines.write(hourly_revenue, "hourly_revenue.jsonl")

pw.run(monitoring_level=pw.MonitoringLevel.ALL)
```

### Zinc v2 (After)

```zinc
// purchase_pipeline.zn — real-time purchase enrichment and aggregation
import pathway

// Schemas are data classes — Zinc generates pw.Schema subclasses
data EventSchema
    user_id: str
    event_type: str
    product_id: int
    amount: float
    timestamp: str
end

data ProductSchema
    id: int
    name: str
    category: str
    price: float
end

// Pipeline declaration — Zinc wires connectors and calls pw.run()
@pipeline(engine: "pathway")
fn purchase_enrichment()
    // Read from Kafka
    var events = kafka.read(
        servers: "localhost:9092",
        group: "zinc-pipeline",
        topic: "user-events",
        schema: EventSchema,
        format: "json"
    )

    // Read product catalog via PostgreSQL CDC
    var products = postgres.read(
        connection: "postgresql://user:pass@localhost:5432/shop",
        schema: ProductSchema,
        table: "products"
    )

    // Filter and enrich
    var purchases = events.filter(e -> e.event_type == "purchase")
    var enriched = purchases
        .join(products, purchases.product_id == products.id)
        .select(
            user_id: purchases.user_id,
            product_name: products.name,
            category: products.category,
            amount: purchases.amount,
            timestamp: purchases.timestamp
        )

    // Windowed aggregation — revenue per category per hour
    var hourly_revenue = enriched
        .window(tumbling: 1.hours, cutoff: 5.minutes)
        .reduce(
            category: min_by(timestamp, category),
            total_revenue: sum(amount),
            order_count: count()
        )

    // Output
    kafka.write(enriched, servers: "localhost:9092", topic: "enriched-purchases")
    jsonlines.write(hourly_revenue, "hourly_revenue.jsonl")
end
```

### What the Transpiler Does

| Zinc v2 | Generated Python |
|---|---|
| `data EventSchema ... end` | `class EventSchema(pw.Schema): ...` |
| `kafka.read(servers: ..., topic: ...)` | `pw.io.kafka.read(rdkafka_settings={"bootstrap.servers": ...}, topic=..., schema=..., format=...)` |
| `postgres.read(connection: ..., table: ...)` | `pw.io.postgres.read(connection_string=..., table_name=..., schema=...)` |
| `.filter(e -> e.event_type == "purchase")` | `.filter(pw.this.event_type == "purchase")` |
| `.join(products, a.x == b.y)` | `.join(products, pw.left.x == pw.right.y)` |
| `.select(name: expr)` | `.select(name=expr)` |
| `.window(tumbling: 1.hours)` | `.windowby(..., window=pw.temporal.tumbling(duration=pw.Duration(hours=1)))` |
| `.reduce(total: sum(amount))` | `.reduce(total=pw.reducers.sum(pw.this.amount))` |
| `kafka.write(table, topic: ...)` | `pw.io.kafka.write(table, rdkafka_settings=..., topic_name=...)` |
| _(auto-generated)_ | `pw.run(monitoring_level=pw.MonitoringLevel.ALL)` |

### More Examples

#### Real-Time CDC (Database Change Capture)

```zinc
// cdc_sync.zn — sync PostgreSQL changes to Kafka in real-time
import pathway

data OrderSchema
    id: int
    customer_id: int
    amount: float
    status: str
    updated_at: str
end

@pipeline(engine: "pathway")
fn order_sync()
    var orders = postgres.read(
        connection: "postgresql://user:pass@localhost:5432/shop",
        schema: OrderSchema,
        table: "orders"
    )

    // Only forward completed orders
    var completed = orders.filter(o -> o.status == "completed")

    // Enrich with computed fields
    var enriched = completed.select(
        id: completed.id,
        customer_id: completed.customer_id,
        amount: completed.amount,
        tax: completed.amount * 0.08,
        total: completed.amount * 1.08,
        updated_at: completed.updated_at
    )

    kafka.write(enriched, servers: "localhost:9092", topic: "completed-orders")
end
```

#### Real-Time Alerting

```zinc
// alerts.zn — alert on anomalous purchase amounts
import pathway

data PurchaseSchema
    user_id: str
    amount: float
    timestamp: str
end

@pipeline(engine: "pathway")
fn anomaly_alerts()
    var purchases = kafka.read(
        servers: "localhost:9092",
        topic: "purchases",
        schema: PurchaseSchema,
        format: "json"
    )

    // Compute rolling average per user (1-hour window)
    var user_avg = purchases
        .window(sliding: 1.hours, hop: 5.minutes)
        .reduce(
            user_id: first(user_id),
            avg_amount: avg(amount),
            max_amount: max(amount),
            tx_count: count()
        )

    // Flag anomalies — purchases > 3x the user's average
    var anomalies = purchases
        .join(user_avg, purchases.user_id == user_avg.user_id)
        .filter(p -> p.amount > user_avg.avg_amount * 3.0)
        .select(
            user_id: purchases.user_id,
            amount: purchases.amount,
            avg_amount: user_avg.avg_amount,
            ratio: purchases.amount / user_avg.avg_amount,
            timestamp: purchases.timestamp
        )

    kafka.write(anomalies, servers: "localhost:9092", topic: "purchase-anomalies")
end
```

#### Batch Mode (Same Code, File Input)

```zinc
// batch_etl.zn — same pipeline logic, batch input from CSV
import pathway

data SalesSchema
    region: str
    product: str
    quantity: int
    price: float
    date: str
end

@pipeline(engine: "pathway")
fn sales_summary()
    // Batch: read from CSV directory (processes all files, then exits)
    var sales = csv.read("data/sales/", schema: SalesSchema)

    var summary = sales
        .filter(s -> s.quantity > 0)
        .groupby(sales.region, sales.product)
        .reduce(
            total_quantity: sum(quantity),
            total_revenue: sum(quantity * price),
            avg_price: avg(price)
        )

    jsonlines.write(summary, "output/sales_summary.jsonl")
end
```

The same pipeline works for streaming -- replace `csv.read(...)` with `kafka.read(...)` and the pipeline runs continuously instead of terminating after processing all files.

---

## 6. Pathway Ecosystem

### LLM Tooling -- Real-Time RAG

Pathway has a dedicated LLM extension (`pathway.xpacks.llm`) that enables **real-time Retrieval-Augmented Generation**. Unlike static RAG (embed documents once, query forever), Pathway RAG updates the vector index as documents change.

```zinc
// rag_pipeline.zn — real-time document indexing for RAG
import pathway

@pipeline(engine: "pathway")
fn document_index()
    // Watch a directory for new/changed documents
    var docs = filesystem.read("docs/", format: "binary", with_metadata: true)

    // Parse, chunk, and embed
    var chunks = docs
        .map(d -> parse_document(d.data))
        .flatten()
        .map(chunk -> {
            "text": chunk,
            "embedding": embed(chunk, model: "text-embedding-3-small")
        })

    // Real-time vector index — updates incrementally as docs change
    var index = pathway.vector_index(chunks, dimensions: 1536)

    // Serve queries via HTTP
    pathway.serve(index, host: "0.0.0.0", port: 8080)
end
```

What makes this unique:
- **Incremental updates** -- when a document changes, only affected chunks are re-embedded. Not the entire corpus.
- **Real-time** -- new documents are indexed within seconds, not hours.
- **No separate vector DB** -- the index lives in-engine. No Pinecone/Weaviate/Qdrant dependency (though you can write to one if needed).
- **LangChain/LlamaIndex compatible** -- Pathway's vector store works as a drop-in retriever for existing LLM frameworks.

### Kafka Integration

Pathway treats Kafka as a first-class citizen -- not an afterthought connector:

- **Consumer groups** -- proper offset management, rebalancing
- **JSON, Avro, Protobuf** format support
- **Exactly-once semantics** (enterprise) or at-least-once (open source)
- **Multiple topics** -- read from and write to multiple topics in one pipeline
- **Autocommit control** -- configurable commit intervals

### Database CDC (Change Data Capture)

Read live changes from PostgreSQL via logical replication. The table in Pathway reflects the current state of the database table and updates in real-time.

### S3 / Cloud Storage

- Read from S3 buckets (watch for new files)
- Write results to S3
- Works with MinIO for local development

### Connectors Summary

| Category | Connectors |
|---|---|
| **Messaging** | Kafka, Redpanda, NATS (via custom) |
| **Databases** | PostgreSQL (CDC), MongoDB, SQLite |
| **Cloud Storage** | S3, Google Drive, SharePoint, Google Cloud Storage |
| **Files** | CSV, JSON, JSONLines, Parquet, plaintext, binary |
| **HTTP** | REST API polling, webhook receiver |
| **Integration** | Airbyte (300+ sources), Debezium |
| **AI/ML** | LLM wrappers (OpenAI, etc.), vector index, embedders |
| **Custom** | Python connector API for any source/sink |

---

## 7. Performance

### Rust Engine

Pathway's engine is written in Rust, built on Differential Dataflow. Key characteristics:

- **Multithreaded** -- utilizes all available CPU cores without Python's GIL limitation
- **Incremental computation** -- when input data changes, only affected computations are re-run. Not the entire pipeline.
- **Memory-efficient** -- Rust's ownership model means no garbage collection pauses
- **Native speed** -- transformations (filter, join, aggregate) execute in Rust, not Python

### Incremental Computation (The Key Differentiator)

Traditional batch processing recomputes everything on every run:

```
Input: 1M rows (1000 new) --> Process ALL 1M rows --> Output
```

Pathway's incremental model:

```
Input: 1M rows (1000 new) --> Process ONLY 1000 changed rows --> Update output
```

This means:
- A pipeline processing 1M events/hour with 1000 new events/second only processes 1000 rows/second, not 1M.
- Joins, aggregations, and windows all update incrementally.
- State is maintained in the Rust engine between updates.

### Pathway vs Flink vs Spark Streaming

| | Pathway | Apache Flink | Spark Structured Streaming |
|---|---|---|---|
| **Language** | Python API, Rust engine | Java/Scala (PyFlink exists but limited) | Scala/Python (PySpark) |
| **Deployment** | `pip install pathway`, Docker | Dedicated cluster (JobManager + TaskManagers) | Spark cluster |
| **Startup** | Seconds | Minutes (cluster startup) | Minutes (cluster startup) |
| **Latency** | Milliseconds | Milliseconds | Seconds to minutes (micro-batch) |
| **Incremental** | Yes (Differential Dataflow) | Yes (state backends) | Partial (micro-batch approximation) |
| **Python UDFs** | Native (runs in-process) | Serialized over network (slow) | Serialized via Py4J (slow) |
| **Infrastructure** | None (single process) to K8s | Heavy (ZooKeeper + cluster) | Heavy (Spark cluster) |
| **Learning curve** | Low (Python + SQL) | High (Java/Scala, complex APIs) | Medium (Spark concepts) |
| **Best for** | Python teams, moderate scale, fast iteration | Large-scale production streaming, JVM shops | Existing Spark infrastructure, batch-first |
| **Weakness** | Newer, smaller community, BSL license | Complex ops, JVM overhead, poor Python story | Not true streaming (micro-batch), high latency |

### Benchmarks

Pathway's benchmark repository shows competitive performance against Flink and Spark on standard workloads (wordcount, windowed aggregation). The Rust engine provides throughput comparable to JVM-based systems while maintaining Python's ease of use.

Key performance characteristics:
- **Throughput**: Handles millions of events per second on commodity hardware
- **Latency**: Sub-second end-to-end for streaming workloads
- **Memory**: Efficient state management via Rust -- no JVM heap tuning
- **Scaling**: Multithreaded on single machine; distributed in enterprise edition

### How It Handles Large Payloads

Unlike NiFi (which streams binary content through processors), Pathway is optimized for **structured data** -- rows with typed columns. For large payloads:

1. **Reference pattern** -- store large content (files, images) in object storage (S3), pass references (URLs, keys) through the pipeline
2. **Binary columns** -- Pathway supports `bytes` type columns for moderate-sized binary data
3. **Chunking** -- for document processing (RAG), Pathway's LLM extension handles splitting large documents into processable chunks

---

## 8. Implementation Approach

### How It Works

Since Zinc transpiles to `.py`, it generates standard Pathway Python code. No custom runtime, no plugins. The generated `.py` file imports `pathway` and runs as a normal Python script.

### Phase 1: Pipeline Annotation

The Zinc parser recognizes `@pipeline(engine: "pathway")` and emits Pathway-specific Python code.

**Transpiler changes:**
- **Parser:** Recognize `@pipeline` annotation with `engine` parameter. Inside a pipeline block, treat `kafka`, `postgres`, `csv`, `s3`, `jsonlines`, `filesystem` as Pathway connector references.
- **Typechecker:** Validate that connector schemas match downstream operations. Check that `.filter()` predicates reference valid columns. Verify `.join()` keys exist on both tables.
- **Codegen:** Emit `import pathway as pw`, generate `pw.Schema` classes from `data` declarations inside pipeline scope, translate connector calls to `pw.io.*`, translate method chains to Pathway API, append `pw.run()`.

### Phase 2: Schema Generation

Zinc `data` classes inside pipeline scope generate `pw.Schema` subclasses:

```zinc
data EventSchema
    user_id: str
    amount: float
    timestamp: str
end
```

Generates:

```python
class EventSchema(pw.Schema):
    user_id: str
    amount: float
    timestamp: str
```

### Phase 3: Connector Simplification

Zinc simplifies Pathway's verbose connector configuration:

```zinc
// Zinc — clean, minimal
var events = kafka.read(
    servers: "localhost:9092",
    group: "my-group",
    topic: "events",
    schema: EventSchema,
    format: "json"
)
```

Generates:

```python
# Python — full Pathway API
events = pw.io.kafka.read(
    rdkafka_settings={
        "bootstrap.servers": "localhost:9092",
        "group.id": "my-group",
    },
    topic="events",
    schema=EventSchema,
    format="json",
    autocommit_duration_ms=1000,
)
```

The transpiler:
- Maps `servers` to `rdkafka_settings["bootstrap.servers"]`
- Maps `group` to `rdkafka_settings["group.id"]`
- Adds sensible defaults (`autocommit_duration_ms`)
- Validates that `schema` parameter references a known `data` class

### Phase 4: Method Chain Translation

Zinc's collection methods map to Pathway operations:

| Zinc | Pathway |
|---|---|
| `.filter(x -> expr)` | `.filter(pw.this.col op value)` or `.filter(lambda x: expr)` |
| `.select(name: expr)` | `.select(name=expr)` |
| `.join(other, condition)` | `.join(other, pw.left.x == pw.right.y).select(...)` |
| `.groupby(cols)` | `.groupby(pw.this.col1, pw.this.col2)` |
| `.reduce(name: agg(col))` | `.reduce(name=pw.reducers.agg(pw.this.col))` |
| `.window(tumbling: 1.hours)` | `.windowby(..., window=pw.temporal.tumbling(duration=pw.Duration(hours=1)))` |
| `.concat(other)` | `pw.Table.concat(table1, table2)` |

### Phase 5: Dagster Integration (Optional)

When a project uses both Dagster and Pathway, Zinc can generate a Dagster asset that wraps a Pathway pipeline:

```zinc
// Dagster asset that runs a Pathway pipeline
@asset(group: "streaming")
fn purchase_enrichment_status(): dict
    // Run the Pathway pipeline as a subprocess or import
    var result = pathway.run_batch("purchase_pipeline.py")
    return {"rows_processed": result.count, "status": "completed"}
end
```

### Phase 6: Project Scaffolding

`zinc init --pipeline pathway` generates a Pathway-ready project:

```
my_stream/
    main.zn              # entry point pipeline
    schemas/
        events.zn        # shared schema definitions
    pipelines/
        ingest.zn        # input pipeline
        transform.zn     # transformation pipeline
        output.zn        # output pipeline
    zinc.toml            # project config
```

The `zinc.toml` includes:

```toml
[project]
name = "my_stream"
type = "pathway"

[pathway]
monitoring = true
workers = 4

[pathway.kafka]
servers = "localhost:9092"
group = "my-stream"
```

---

## 9. Dagster + Pathway -- The Full Picture

With both exploration docs, the Zinc v2 data processing story is complete:

```
+-------------------+     +-------------------+     +-------------------+
|  Zinc .zn files   | --> |  zinc build       | --> |  .py files        |
|  (pipeline code)  |     |  (transpiler)     |     |  (Dagster/Pathway)|
+-------------------+     +-------------------+     +-------------------+

                               generates

           +--------------------------------------------------+
           |                                                  |
           v                                                  v
   Dagster Python (.py)                            Pathway Python (.py)
   - @dg.asset functions                           - pw.Table operations
   - Definitions auto-gen                          - pw.run() auto-gen
   - Batch orchestration                           - Streaming computation
   - Schedules, sensors                            - Real-time processing
   - Asset lineage                                 - Incremental updates
           |                                                  |
           v                                                  v
   dagster dev / dagster+                          python pipeline.py
   (orchestration UI)                              (streaming engine)
```

### When to Use Which

| Workload | Engine | Why |
|---|---|---|
| Hourly ETL from warehouse | Dagster | Batch scheduling, dependency management, retry logic |
| Real-time event processing | Pathway | Sub-second latency, incremental computation |
| CDC database sync | Pathway | Real-time change capture, continuous processing |
| ML model training pipeline | Dagster | Asset tracking, experiment lineage, scheduled retraining |
| Real-time RAG / document indexing | Pathway | Incremental vector index updates, live document watching |
| Data quality monitoring | Both | Dagster for scheduled checks, Pathway for real-time anomaly detection |
| File processing (batch of CSVs) | Either | Pathway for incremental, Dagster for orchestrated |

---

## Open Questions

1. **Pipeline keyword vs annotation**: Should Zinc use `@pipeline(engine: "pathway")` (annotation) or `pipeline "pathway" ... end` (keyword block)? Annotations are consistent with Dagster. Keywords are more Zinc-native.

2. **Schema sharing**: When Dagster assets and Pathway pipelines share schemas, how does the transpiler generate both `pw.Schema` and `@dataclass` from the same `data` declaration? Context-dependent codegen?

3. **Connector config in code vs config**: Should Kafka broker addresses live in `.zn` files or `zinc.toml`? Code is explicit, config is environment-aware. Probably config with code override.

4. **Testing Pathway pipelines**: Pathway supports `pw.debug.table_from_markdown()` for creating test tables. Zinc should make pipeline testing as clean as function testing.

5. **Error handling in pipelines**: What happens when a UDF fails on one row? Pathway has `@pw.udf` with optional error handling. Zinc's `or {}` error blocks could map to this.

6. **Monitoring integration**: Should `zinc dev --pipeline` wrap Pathway's monitoring dashboard? Combine it with Dagster's UI for a unified view?

7. **Distributed mode**: Pathway enterprise supports distributed execution. How does this affect the generated code? Should Zinc abstract over single-node vs distributed?

---

## References

- [Pathway Official Site](https://pathway.com)
- [Pathway GitHub Repository](https://github.com/pathwaycom/pathway) (60K+ stars)
- [Pathway Documentation](https://pathway.com/developers/documentation)
- [Pathway LLM Extension](https://pathway.com/developers/templates/llm-alert-pathway)
- [Pathway Benchmarks vs Flink/Spark](https://github.com/pathwaycom/pathway-benchmarks)
- [Pathway Kafka Connector](https://pathway.com/developers/api-docs/pathway-io/kafka)
- [Pathway PostgreSQL CDC](https://pathway.com/developers/api-docs/pathway-io/postgres)
- [Pathway Temporal Operations](https://pathway.com/developers/api-docs/temporal)
- [Pathway Vector Index (RAG)](https://pathway.com/developers/templates/vector-store-pipeline)
- [Dagster Exploration (companion doc)](exploration-dagster-pipelines.md)
- [Zinc v2 Design Doc](design-zinc-v2-python.md)
- [Differential Dataflow (Rust foundation)](https://github.com/TimelyDataflow/differential-dataflow)
