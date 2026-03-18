<!-- Licensed under the Apache License, Version 2.0 -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Exploration: PyFlink Pipelines in Zinc v2

**Status:** Exploratory -- not committed to implementation
**Date:** 2026-03-18
**Context:** Zinc v2 transpiles `.zn` files to `.py` files. PyFlink is the Python API for Apache Flink, the industry-standard stream processing engine. This is the user's primary pipeline technology in production. Flink complements Dagster (batch orchestration) and Pathway (lightweight streaming) as the heavyweight enterprise streaming and batch engine.

---

## 1. What is Apache Flink

Apache Flink is a **stream-first, unified batch and stream processing engine** built on the JVM. Unlike Spark, which retrofitted streaming onto a batch engine (micro-batching), Flink was designed from day one to process events as they arrive -- true record-at-a-time streaming with batch as a special case of bounded streams.

### Core Architecture

```
                    +---------------------------+
                    |     Flink Client (API)     |
                    |  Table/SQL | DataStream    |
                    +---------------------------+
                                |
                    +---------------------------+
                    |     JobManager (master)    |
                    |  - job scheduling          |
                    |  - checkpoint coordination |
                    |  - failure recovery        |
                    +---------------------------+
                                |
            +-------------------+-------------------+
            |                   |                   |
    +-------v-------+  +-------v-------+  +-------v-------+
    | TaskManager 1 |  | TaskManager 2 |  | TaskManager N |
    | (worker slots)|  | (worker slots)|  | (worker slots)|
    +---------------+  +---------------+  +---------------+
```

### Core Concepts

| Concept | What It Is |
|---|---|
| **DataStream** | The fundamental abstraction for unbounded (streaming) data. A continuous flow of records that you transform with operators (map, filter, keyBy, window, join). |
| **Table / SQL** | A higher-level relational API. Write SQL or table expressions against streaming or batch data. The optimizer generates efficient execution plans. |
| **Event Time** | Processing based on timestamps embedded in the data, not when the system receives the event. Critical for out-of-order data, late arrivals, and reproducible results. |
| **Watermarks** | A mechanism to track progress in event time. A watermark W means "all events with timestamp <= W have arrived." Triggers window computations and handles late data. |
| **Windows** | Group events by time: tumbling (fixed, non-overlapping), sliding (overlapping), session (gap-based). Windows are first-class citizens, not bolted on. |
| **Stateful Processing** | Each operator can maintain local state (counters, aggregates, custom objects) that is automatically checkpointed. State survives failures and rescaling. |
| **Checkpoints** | Periodic, consistent snapshots of all operator state and stream positions. Enables exactly-once semantics. If a failure occurs, Flink restores from the latest checkpoint and replays. |
| **Savepoints** | User-triggered checkpoints for planned maintenance, upgrades, or migration. Stop a job, upgrade code, restart from the savepoint. |
| **Exactly-Once Semantics** | End-to-end exactly-once processing via two-phase commit with supported sinks (Kafka, JDBC, filesystem). No duplicates, no data loss. |
| **Parallelism** | Each operator runs with configurable parallelism. Flink automatically distributes work across TaskManager slots. Data is partitioned by key for stateful operators. |
| **Fault Tolerance** | Automatic recovery from failures. Checkpoints + replay from Kafka offsets = no data loss. Failed tasks restart on available slots. |

### What Makes Flink Different

1. **True streaming** -- processes each record as it arrives, not in micro-batches. Latency measured in milliseconds, not seconds.
2. **Unified engine** -- the same code processes bounded (batch) and unbounded (streaming) data. No separate batch and streaming APIs to learn.
3. **First-class state** -- managed, checkpointed state is a core feature, not an afterthought. State backends include heap memory, RocksDB, and the new disaggregated state management (Flink 2.x).
4. **Event-time processing** -- watermarks and event-time windows handle out-of-order data correctly. Critical for real-world systems where events arrive late.
5. **Battle-tested at scale** -- used by Alibaba (trillions of events/day), Netflix, Uber, LinkedIn, Stripe, Airbnb, and countless enterprises.

### Current State (Flink 2.x, 2025-2026)

Apache Flink 2.0.0 launched in March 2025 -- the first major release since Flink 1.0 nine years prior. The 2.x series brings:

- **Flink 2.0** (March 2025): Disaggregated state management for cloud-native deployments, materialized tables (unify stream and batch via a single DDL), batch execution optimizations, deep Apache Paimon integration for streaming lakehouse architecture. 165 contributors, 25 FLIPs, 369 issues.
- **Flink 2.1** (July 2025): AI model DDL for managing ML models, `ML_PREDICT` table-valued function for real-time AI inference in SQL, Process Table Functions (PTFs) for user-defined operators with full state/timer access. 116 contributors, 16 FLIPs.
- **Flink 2.2** (December 2025): `ML_PREDICT` for LLM inference, `VECTOR_SEARCH` for real-time vector similarity, enhanced materialized tables, improved PyFlink support, DeltaJoin operator for reduced state in streaming joins, `VARIANT` data type for semi-structured data (JSON). 73 contributors, 9 FLIPs.

---

## 2. PyFlink -- Python API for Apache Flink

PyFlink is Flink's official Python SDK, providing Python access to both the Table API and DataStream API. It bridges Python user code to the JVM-based Flink runtime.

### Architecture

```
Python Process                         JVM Process (Flink)
+----------------------------+         +----------------------------+
| PyFlink API                |         | Flink Runtime              |
|  - Table API               |  Py4J   |  - JobManager              |
|  - DataStream API          | ------> |  - TaskManagers            |
|  - SQL strings             |         |  - State backends          |
|  - UDFs (Python callables) |         |  - Checkpointing           |
+----------------------------+         +----------------------------+
              |                                     |
              v                                     v
    Python UDF Worker              JVM operators (Java/Scala)
    (Apache Beam runner)           (native performance)
```

The key insight: **PyFlink defines the job graph in Python, but execution happens on the JVM.** Pure table/SQL operations run entirely on the JVM at full speed. Python UDFs involve serialization between Python and JVM processes, which adds overhead.

### Two APIs

| API | Best For | Abstraction Level |
|---|---|---|
| **Table API** | Structured data, SQL-like transformations, aggregations, joins, windowed operations. Declarative -- describe *what* you want. The optimizer generates efficient execution. | High (relational) |
| **DataStream API** | Complex event processing, custom state management, fine-grained control over event time, timers, side outputs. Imperative -- describe *how* to process. | Low (procedural) |

The two APIs are interoperable -- you can convert between `Table` and `DataStream` within the same job.

### Table API Example (Python)

```python
from pyflink.table import EnvironmentSettings, TableEnvironment

env_settings = EnvironmentSettings.in_streaming_mode()
t_env = TableEnvironment.create(env_settings)

# Define source via SQL DDL
t_env.execute_sql("""
    CREATE TABLE orders (
        order_id STRING,
        customer_id STRING,
        amount DOUBLE,
        order_time TIMESTAMP(3),
        WATERMARK FOR order_time AS order_time - INTERVAL '5' SECOND
    ) WITH (
        'connector' = 'kafka',
        'topic' = 'orders',
        'properties.bootstrap.servers' = 'localhost:9092',
        'format' = 'json'
    )
""")

# Query with Table API
orders = t_env.from_path("orders")
result = orders \
    .filter(orders.amount > 100) \
    .group_by(orders.customer_id) \
    .select(
        orders.customer_id,
        orders.amount.sum.alias("total_spent"),
        orders.order_id.count.alias("order_count")
    )
```

### DataStream API Example (Python)

```python
from pyflink.datastream import StreamExecutionEnvironment
from pyflink.common import WatermarkStrategy
from pyflink.datastream.connectors.kafka import KafkaSource

env = StreamExecutionEnvironment.get_execution_environment()

kafka_source = KafkaSource.builder() \
    .set_bootstrap_servers("localhost:9092") \
    .set_topics("events") \
    .set_group_id("my-group") \
    .build()

stream = env.from_source(kafka_source, WatermarkStrategy.no_watermarks(), "kafka")
stream \
    .filter(lambda event: event["type"] == "purchase") \
    .key_by(lambda event: event["user_id"]) \
    .map(lambda event: (event["user_id"], event["amount"]))

env.execute("purchase-processor")
```

### Python UDFs

PyFlink supports user-defined functions written in Python:

- **Scalar UDFs** -- one row in, one value out
- **Table UDFs** -- one row in, multiple rows out
- **Aggregate UDFs** -- many rows in, one value out
- **Pandas UDFs** -- vectorized operations using Pandas DataFrames (batch processing within windows)

UDFs run in a separate Python worker process and communicate with the JVM via Apache Beam's portable runner. This adds serialization overhead but allows full Python library access (NumPy, scikit-learn, etc.).

### Current State and Limitations

**Supported:**
- Table API: near-complete parity with Java
- DataStream API: core operations supported (map, filter, key_by, window, process)
- SQL: full Flink SQL support via `execute_sql()`
- Connectors: Kafka, JDBC, filesystem, Elasticsearch (via SQL DDL)
- State backends: RocksDB, heap, disaggregated (Flink 2.x)
- Python 3.9 through 3.12 (Flink 2.1+; Python 3.8 dropped)

**Limitations:**
- Python UDFs have serialization overhead compared to Java UDFs
- Some advanced DataStream features (async I/O, complex state access patterns) have less ergonomic Python APIs
- Not all connectors have first-class Python builder APIs -- many require SQL DDL for configuration
- Deployment requires a JVM runtime (Flink cluster) alongside the Python environment

**Community direction:**
- Making the API more Pythonic
- Better interactive programming support (retrieving leading rows of unbounded tables)
- Improved documentation and examples
- Enhanced AI/ML integration (Flink 2.1+ model DDL)

---

## 3. Why PyFlink for Zinc

### The Fit

PyFlink is a **Python API that generates standard Python code**. Zinc transpiles `.zn` to `.py`. The transpiled Python imports `pyflink` and defines Flink jobs using the Table API, DataStream API, or SQL. No special runtime beyond what PyFlink already requires (a Flink cluster).

| Zinc Principle | PyFlink Alignment |
|---|---|
| "It's just Python underneath" | PyFlink jobs are Python scripts. Zinc generates them. |
| Zero ceremony | Flink's verbose SQL DDL and environment setup is boilerplate. Zinc can auto-generate it. |
| Enforced types | Flink has strong schemas (SQL types, Row types). Zinc's type enforcement catches schema mismatches at transpile time. |
| The transpiler works for you | Zinc auto-generates `TableEnvironment` setup, SQL DDL for connectors, watermark configuration, and `execute()` calls. |
| Convention over configuration | Flink requires extensive configuration (parallelism, checkpointing, state backend). Zinc can set sensible defaults. |

### What Zinc Adds Over Raw PyFlink Python

1. **No SQL DDL strings** -- Zinc declares connectors as typed data classes; the transpiler generates the CREATE TABLE DDL
2. **No environment boilerplate** -- Zinc auto-generates `TableEnvironment.create()`, checkpoint configuration, and `execute()` calls
3. **Type-safe schemas** -- Zinc `data` classes map to Flink table schemas; the type checker validates column references at transpile time
4. **Unified connector syntax** -- `kafka.source(...)`, `jdbc.sink(...)` instead of multi-line SQL DDL strings
5. **Pipeline-as-keyword** -- `@pipeline(engine: "flink")` declares the engine; the transpiler generates the right Python imports and setup

### Production Context

This is the primary pipeline technology in production. Flink handles:
- Real-time event processing at scale
- Stateful stream processing with exactly-once guarantees
- Complex event processing (CEP) for pattern detection
- Streaming ETL from Kafka to data lakes
- Real-time analytics and dashboards

Zinc support for Flink means the team can write pipeline code in Zinc's cleaner syntax while generating standard PyFlink Python that deploys to existing Flink infrastructure.

### Complementary to Dagster and Pathway

| Tool | Role | Zinc Annotation |
|---|---|---|
| **Dagster** | Batch orchestration -- scheduling, lineage, asset management | `@asset`, `@op`, `@sensor` |
| **Pathway** | Lightweight streaming -- Rust engine, simple Python API, fast iteration | `@pipeline(engine: "pathway")` |
| **Flink** | Enterprise streaming -- JVM engine, massive scale, exactly-once, complex state, Flink SQL | `@pipeline(engine: "flink")` |

---

## 4. NiFi Concepts to Flink Mapping

For teams with NiFi background, here is how NiFi concepts map onto Flink:

| NiFi Concept | Flink Equivalent | Notes |
|---|---|---|
| **Processor** | Operator (map, filter, flatMap, window, join, process) | NiFi processors transform FlowFiles. Flink operators transform DataStream records or Table rows. |
| **FlowFile** | Record / Row | NiFi FlowFiles carry content + attributes. Flink records are typed rows in a DataStream or Table. |
| **FlowFile Attributes** | Record fields / Table columns | NiFi key-value attributes map to typed fields in Flink schemas. |
| **Connection (queue)** | DataStream / intermediate result | NiFi connects processors via bounded queues. Flink connects operators via data streams, with network buffers managed by the runtime. |
| **Back-Pressure** | Credit-based flow control | NiFi connections have configurable back-pressure thresholds. Flink uses credit-based flow control between TaskManagers -- downstream operators signal how much data they can accept. Automatic, no manual configuration. |
| **Process Group** | Job / sub-topology | Logical grouping. A Flink job is a DAG of operators. Sub-topologies can be composed via the Table API or reusable DataStream functions. |
| **Exactly-Once** | Checkpoint barriers + two-phase commit | NiFi achieves exactly-once via content/attribute repositories and transaction rollback. Flink uses Chandy-Lamport checkpoint barriers flowing through the stream, combined with two-phase commit for sinks (Kafka, JDBC). |
| **Controller Service** | Configuration / resource setup | Shared services like database pools. In Flink, connectors are configured via SQL DDL properties or builder APIs. |
| **Provenance** | Metrics + state inspection | NiFi tracks every FlowFile's full history. Flink provides operator metrics, checkpoint metadata, and state inspection (queryable state, state processor API). Flink 2.1 added SQL-based state querying from checkpoints. |
| **Input/Output Port** | Source / Sink connector | Data enters via source connectors (Kafka, filesystem, JDBC) and exits via sink connectors. |
| **Funnel** | Union operator | Merge multiple streams: `stream1.union(stream2, stream3)`. |
| **Parameter Context** | Job configuration / `ParameterTool` | Runtime configuration injected via command-line args, config files, or environment variables. |
| **NiFi Cluster** | Flink Cluster (JobManager + TaskManagers) | NiFi clusters distribute processors across nodes. Flink clusters distribute operator parallelism across TaskManager slots. |
| **Site-to-Site** | Network shuffle / cross-region replication | NiFi site-to-site transfers data between clusters. Flink handles data shuffling internally; cross-region replication is handled at the Kafka/storage layer. |

### Key Differences

| | NiFi | Flink |
|---|---|---|
| **Model** | Visual dataflow (drag-and-drop processors) | Code-defined job graph (Java/Python/SQL) |
| **Latency** | Milliseconds (per FlowFile) | Milliseconds (per record) |
| **State** | Content + attribute repositories on disk | Managed state backends (RocksDB, disaggregated) with checkpointing |
| **Scaling** | NiFi cluster (each node runs all processors) | Flink cluster (operators distributed across slots with key-based partitioning) |
| **Exactly-Once** | Repository-based transactions | Checkpoint barriers + two-phase commit |
| **Language** | Java (custom processors) + XML/JSON config (flows) | Java/Scala/Python/SQL |
| **Batch** | Not designed for batch | First-class batch mode (bounded streams) |
| **SQL** | NiFi has RecordPath, not SQL | Full ANSI SQL with streaming extensions |

**Migration note:** NiFi's visual flow model does not translate directly to code. The mapping is conceptual. Teams moving from NiFi to Flink typically redesign their flows as Flink SQL queries or DataStream/Table API jobs, which is where Zinc's cleaner syntax helps reduce the learning curve.

---

## 5. Zinc Syntax for Flink Pipelines

### Streaming Example -- Real-Time Order Processing

#### PyFlink Python (Before)

```python
from pyflink.table import EnvironmentSettings, TableEnvironment

env_settings = EnvironmentSettings.in_streaming_mode()
t_env = TableEnvironment.create(env_settings)

t_env.get_config().set("execution.checkpointing.interval", "60000")
t_env.get_config().set("execution.checkpointing.mode", "EXACTLY_ONCE")
t_env.get_config().set("state.backend.type", "rocksdb")

t_env.execute_sql("""
    CREATE TABLE orders (
        order_id STRING,
        customer_id STRING,
        product_id STRING,
        amount DOUBLE,
        status STRING,
        order_time TIMESTAMP(3),
        WATERMARK FOR order_time AS order_time - INTERVAL '10' SECOND
    ) WITH (
        'connector' = 'kafka',
        'topic' = 'orders',
        'properties.bootstrap.servers' = 'kafka:9092',
        'properties.group.id' = 'order-processor',
        'format' = 'json',
        'scan.startup.mode' = 'latest-offset'
    )
""")

t_env.execute_sql("""
    CREATE TABLE completed_orders (
        order_id STRING,
        customer_id STRING,
        amount DOUBLE,
        order_time TIMESTAMP(3)
    ) WITH (
        'connector' = 'kafka',
        'topic' = 'completed-orders',
        'properties.bootstrap.servers' = 'kafka:9092',
        'format' = 'json'
    )
""")

t_env.execute_sql("""
    CREATE TABLE hourly_revenue (
        window_start TIMESTAMP(3),
        window_end TIMESTAMP(3),
        total_revenue DOUBLE,
        order_count BIGINT
    ) WITH (
        'connector' = 'jdbc',
        'url' = 'jdbc:postgresql://localhost:5432/analytics',
        'table-name' = 'hourly_revenue',
        'username' = 'user',
        'password' = 'pass'
    )
""")

orders = t_env.from_path("orders")
completed = orders.filter(orders.status == "completed")

t_env.create_temporary_view("completed_orders_view", completed)
t_env.execute_sql("""
    INSERT INTO completed_orders
    SELECT order_id, customer_id, amount, order_time
    FROM completed_orders_view
""")

t_env.execute_sql("""
    INSERT INTO hourly_revenue
    SELECT
        TUMBLE_START(order_time, INTERVAL '1' HOUR) AS window_start,
        TUMBLE_END(order_time, INTERVAL '1' HOUR) AS window_end,
        SUM(amount) AS total_revenue,
        COUNT(*) AS order_count
    FROM completed_orders_view
    GROUP BY TUMBLE(order_time, INTERVAL '1' HOUR)
""")
```

#### Zinc v2 (After)

```zinc
// order_pipeline.zn -- real-time order processing with Flink
import flink

// Schemas are data classes -- Zinc generates Flink SQL DDL
data Order
    order_id: str
    customer_id: str
    product_id: str
    amount: float
    status: str
    order_time: timestamp
end

data CompletedOrder
    order_id: str
    customer_id: str
    amount: float
    order_time: timestamp
end

data HourlyRevenue
    window_start: timestamp
    window_end: timestamp
    total_revenue: float
    order_count: int
end

// Pipeline declaration
@pipeline(engine: "flink", checkpoint: 60.seconds, mode: "exactly_once")
fn order_processing()
    // Sources -- Zinc generates CREATE TABLE DDL
    var orders = kafka.source(
        topic: "orders",
        servers: "kafka:9092",
        group: "order-processor",
        schema: Order,
        watermark: order_time - 10.seconds
    )

    // Filter completed orders
    var completed = orders.filter(o -> o.status == "completed")

    // Sink 1 -- forward to Kafka topic
    kafka.sink(completed,
        topic: "completed-orders",
        servers: "kafka:9092",
        schema: CompletedOrder
    )

    // Sink 2 -- hourly revenue aggregation to JDBC
    var hourly = completed
        .window(tumbling: 1.hours, on: order_time)
        .select(
            window_start: window_start(),
            window_end: window_end(),
            total_revenue: sum(amount),
            order_count: count()
        )

    jdbc.sink(hourly,
        url: "jdbc:postgresql://localhost:5432/analytics",
        table: "hourly_revenue",
        schema: HourlyRevenue,
        username: "user",
        password: "pass"
    )
end
```

### Batch Example -- Historical Data Processing

```zinc
// backfill.zn -- batch reprocessing of historical orders
import flink

data SalesRecord
    region: str
    product: str
    quantity: int
    price: float
    sale_date: str
end

data RegionalSummary
    region: str
    total_revenue: float
    total_quantity: int
    avg_price: float
end

@pipeline(engine: "flink", mode: "batch")
fn sales_backfill()
    // Read from filesystem (bounded -- batch mode)
    var sales = filesystem.source(
        path: "s3://data-lake/sales/2025/",
        format: "parquet",
        schema: SalesRecord
    )

    // Aggregate by region
    var summary = sales
        .group_by(s -> s.region)
        .select(
            region: region,
            total_revenue: sum(quantity * price),
            total_quantity: sum(quantity),
            avg_price: avg(price)
        )

    // Write to Iceberg table
    iceberg.sink(summary,
        catalog: "hive_catalog",
        database: "analytics",
        table: "regional_summary",
        schema: RegionalSummary
    )
end
```

### Stateful Processing Example -- Session Windows

```zinc
// sessions.zn -- user session tracking with stateful processing
import flink

data ClickEvent
    user_id: str
    page: str
    action: str
    event_time: timestamp
end

data UserSession
    user_id: str
    session_start: timestamp
    session_end: timestamp
    page_count: int
    pages: list[str]
end

@pipeline(engine: "flink", checkpoint: 30.seconds, mode: "exactly_once")
fn session_tracker()
    var clicks = kafka.source(
        topic: "clickstream",
        servers: "kafka:9092",
        schema: ClickEvent,
        watermark: event_time - 30.seconds
    )

    // Session windows -- gap of 15 minutes
    var sessions = clicks
        .key_by(c -> c.user_id)
        .window(session: 15.minutes, on: event_time)
        .select(
            user_id: first(user_id),
            session_start: min(event_time),
            session_end: max(event_time),
            page_count: count(),
            pages: collect(page)
        )

    kafka.sink(sessions,
        topic: "user-sessions",
        servers: "kafka:9092",
        schema: UserSession
    )
end
```

### Flink SQL Passthrough

For complex queries or teams already fluent in Flink SQL, Zinc supports inline SQL:

```zinc
// sql_pipeline.zn -- using Flink SQL directly
import flink

@pipeline(engine: "flink", checkpoint: 60.seconds)
fn revenue_analytics()
    // Define sources
    kafka.source(
        topic: "orders",
        servers: "kafka:9092",
        schema: Order,
        watermark: order_time - 10.seconds,
        name: "orders"
    )

    // Use Flink SQL directly for complex queries
    var result = flink.sql("
        SELECT
            customer_id,
            TUMBLE_START(order_time, INTERVAL '1' HOUR) AS window_start,
            SUM(amount) AS total_spent,
            COUNT(*) AS order_count,
            AVG(amount) AS avg_order
        FROM orders
        WHERE status = 'completed'
        GROUP BY
            customer_id,
            TUMBLE(order_time, INTERVAL '1' HOUR)
        HAVING SUM(amount) > 1000
    ")

    jdbc.sink(result,
        url: "jdbc:postgresql://localhost:5432/analytics",
        table: "customer_hourly_spend"
    )
end
```

### UDF Example

```zinc
// udf_pipeline.zn -- custom Python UDF in Flink
import flink

@udf(result_type: "str")
fn categorize_amount(amount: float): str
    if amount > 1000
        return "high"
    else if amount > 100
        return "medium"
    else
        return "low"
    end
end

@pipeline(engine: "flink", checkpoint: 60.seconds)
fn enriched_orders()
    var orders = kafka.source(
        topic: "orders",
        servers: "kafka:9092",
        schema: Order,
        watermark: order_time - 10.seconds
    )

    // Use UDF in pipeline
    var enriched = orders.select(
        order_id: order_id,
        customer_id: customer_id,
        amount: amount,
        category: categorize_amount(amount),
        order_time: order_time
    )

    kafka.sink(enriched, topic: "enriched-orders", servers: "kafka:9092")
end
```

### Transpilation Mapping

| Zinc v2 | Generated PyFlink Python |
|---|---|
| `@pipeline(engine: "flink")` | `TableEnvironment.create(EnvironmentSettings.in_streaming_mode())` + checkpoint config |
| `@pipeline(engine: "flink", mode: "batch")` | `TableEnvironment.create(EnvironmentSettings.in_batch_mode())` |
| `data Order ... end` | `CREATE TABLE orders (...) WITH (...)` SQL DDL string |
| `kafka.source(topic: ..., schema: ...)` | `execute_sql("CREATE TABLE ... WITH ('connector'='kafka', ...)")` |
| `jdbc.sink(table, url: ..., table: ...)` | `execute_sql("CREATE TABLE ... WITH ('connector'='jdbc', ...)")` + `INSERT INTO` |
| `.filter(o -> o.status == "completed")` | `.filter(col("status") == "completed")` or SQL WHERE clause |
| `.window(tumbling: 1.hours, on: field)` | `TUMBLE(field, INTERVAL '1' HOUR)` in SQL or `.window(Tumble.over(...))` |
| `.group_by(f -> f.region)` | `.group_by(col("region"))` |
| `.select(name: sum(amount))` | `.select(col("amount").sum.alias("name"))` |
| `.key_by(c -> c.user_id)` | `.key_by(lambda r: r["user_id"])` (DataStream) |
| `@udf(result_type: "str")` | `@udf(result_type=DataTypes.STRING())` |
| `flink.sql("...")` | `t_env.execute_sql("...")` |
| `watermark: field - 10.seconds` | `WATERMARK FOR field AS field - INTERVAL '10' SECOND` |
| `checkpoint: 60.seconds` | `t_env.get_config().set("execution.checkpointing.interval", "60000")` |

---

## 6. Flink Ecosystem

Flink's connector and integration ecosystem is extensive, covering the modern data stack end-to-end.

### Connectors

| Connector | Direction | Notes |
|---|---|---|
| **Apache Kafka** | Source + Sink | The primary integration. Exactly-once via two-phase commit. Consumer group management, offset tracking, topic pattern subscriptions. |
| **JDBC** | Source + Sink | PostgreSQL, MySQL, Oracle, SQL Server. Lookup joins for enrichment. Upsert mode for idempotent writes. |
| **Elasticsearch / OpenSearch** | Sink | Real-time indexing of streaming results. Bulk writes with configurable flush intervals. |
| **Amazon S3 / HDFS** | Source + Sink | Parquet, ORC, Avro, CSV, JSON. Streaming sink with rolling file policies. Exactly-once via checkpoint coordination. |
| **Apache Hive** | Source + Sink | Read/write Hive tables. Streaming sink to Hive partitions. |
| **Apache Iceberg** | Source + Sink | Modern table format with ACID transactions. Read streaming changes (incremental reads). Flink is a primary compute engine for Iceberg. |
| **Apache Paimon** | Source + Sink | Streaming lakehouse. Deep integration in Flink 2.x. Real-time writes with compaction. |
| **Apache Pulsar** | Source + Sink | Alternative to Kafka. Native Flink connector. |
| **MongoDB** | Source + Sink | Read/write MongoDB collections. CDC support. |
| **HBase** | Source + Sink | Read/write HBase tables. Lookup joins. |
| **Redis** | Lookup + Sink | Cache lookups and writes. |
| **Kinesis** | Source + Sink | AWS Kinesis Data Streams integration. |

### Flink SQL

Flink SQL is ANSI SQL-compliant with streaming extensions. It is increasingly the primary way teams define Flink jobs:

- **DDL**: `CREATE TABLE`, `CREATE VIEW`, `CREATE FUNCTION`, `CREATE MATERIALIZED TABLE` (Flink 2.x)
- **DML**: `INSERT INTO`, `SELECT ... FROM ... WHERE ... GROUP BY ... HAVING`
- **Streaming extensions**: `TUMBLE()`, `HOP()`, `SESSION()` window functions, `MATCH_RECOGNIZE` for pattern detection, `WATERMARK FOR` declarations
- **Joins**: Regular joins, temporal joins (versioned table lookup at event time), lookup joins (external system enrichment), interval joins
- **AI/ML** (Flink 2.1+): `ML_PREDICT()` for model inference, `VECTOR_SEARCH()` for similarity search, `ML_FORECAST()` and `ML_ANOMALY_DETECTION()` for time-series analysis

### Flink CDC (Change Data Capture)

Flink CDC is a separate project (Apache Flink CDC 3.x) providing source connectors that capture database changes in real-time:

- **Supported databases**: MySQL, PostgreSQL, Oracle, SQL Server, MongoDB, TiDB, OceanBase, Db2
- **Features**: Full snapshot + incremental binlog reading, exactly-once semantics, schema evolution, sharding table synchronization
- **Architecture**: DataStream API connectors (no Debezium/Kafka required -- reads binlogs directly) and Table/SQL API connectors (single-table CDC via DDL)
- **YAML pipelines** (Flink CDC 3.x): Define entire database synchronization pipelines in YAML -- full database sync, table routing, column projection, computed columns

```yaml
# Flink CDC 3.x YAML pipeline example
source:
  type: mysql
  hostname: mysql-host
  port: 3306
  username: root
  password: password
  tables: orders_db.\.*

sink:
  type: kafka
  properties.bootstrap.servers: kafka:9092

pipeline:
  name: MySQL to Kafka CDC
  parallelism: 4
```

---

## 7. Performance and Scale

### How Flink Handles Scale

| Concern | Flink Approach |
|---|---|
| **Large state** | RocksDB state backend stores state on local SSDs with incremental checkpoints. Flink 2.x disaggregated state management offloads state to remote storage (S3, HDFS) for cloud-native elasticity. |
| **Exactly-once** | Chandy-Lamport checkpoint barriers flow through the data stream. All operators snapshot their state atomically. Two-phase commit for sinks (Kafka, JDBC) ensures end-to-end exactly-once. |
| **Backpressure** | Credit-based flow control between operators. Downstream operators signal available capacity to upstream. No data is dropped -- the entire pipeline slows down gracefully. |
| **Parallelism** | Each operator runs with configurable parallelism (1 to thousands). Data is hash-partitioned by key for stateful operators. Rescaling redistributes state across new parallelism. |
| **Checkpointing** | Asynchronous, incremental checkpoints. RocksDB snapshots happen in the background without blocking processing. Checkpoint intervals are configurable (seconds to minutes). |
| **Recovery** | On failure, Flink restores from the latest checkpoint: reloads operator state, resets Kafka offsets, replays data. Recovery time depends on state size and source replay speed. |
| **Network** | Network buffer pools, credit-based flow control, and data compression reduce network overhead. Flink's shuffle service handles data exchange between operators. |
| **Late data** | Watermarks track event-time progress. Late events (after watermark) can be collected via side outputs or trigger window updates (allowed lateness). |

### Flink vs NiFi: Performance Characteristics

| Aspect | NiFi | Flink |
|---|---|---|
| **Throughput** | Moderate -- designed for data routing, not heavy computation. Millions of FlowFiles/day typical. | Very high -- designed for computational workloads. Trillions of events/day at Alibaba. |
| **Latency** | Low (ms per FlowFile) but no event-time semantics | Low (ms per record) with full event-time and watermark support |
| **State** | Content/attribute repositories (disk-based) | Managed state backends with checkpointing (memory, RocksDB, disaggregated) |
| **Windowed aggregations** | Not native -- requires custom processors | First-class: tumbling, sliding, session windows with event-time |
| **Joins** | Limited -- requires buffering in processors | Full: regular, temporal, interval, lookup joins |
| **Exactly-once** | Per-FlowFile transactions | End-to-end with checkpoint barriers + two-phase commit |
| **Batch** | Not designed for it | First-class batch mode (bounded streams) |
| **SQL** | No | Full ANSI SQL with streaming extensions |

### Flink vs Spark Streaming

| Aspect | Flink | Spark Structured Streaming |
|---|---|---|
| **Processing model** | True streaming (record-at-a-time) | Micro-batch (configurable interval) |
| **Latency** | Milliseconds (10-100ms typical) | Hundreds of ms to seconds |
| **State management** | Native, managed, incremental checkpoints | RocksDB-backed (Spark 3.5+), narrowing the gap |
| **Event time** | First-class with watermarks, late data handling | Supported but less flexible |
| **Exactly-once** | Checkpoint barriers + two-phase commit | Micro-batch atomicity + idempotent sinks |
| **SQL** | Flink SQL (streaming-first) | Spark SQL (batch-first, streaming extensions) |
| **Ecosystem** | Streaming-focused: Kafka, CDC, Iceberg, Paimon | Broader: ML (MLlib), graph (GraphX), batch analytics |
| **Operational** | Requires understanding checkpoint tuning, watermarks | Easier for batch/hybrid; Spark-native teams stay productive |
| **When to choose** | Sub-second latency, complex stateful processing, event-time | Team already uses Spark, latency tolerance in seconds, hybrid batch/streaming |

---

## 8. Flink vs Pathway vs Dagster

### Positioning

```
                Lightweight                              Heavyweight
                (fast setup)                             (maximum power)
    +-----------+-------------------+-----------------------+
    |  Dagster   |     Pathway      |       Flink           |
    |  (batch    |  (lightweight    |  (enterprise          |
    |  orchestr) |   streaming)    |   streaming + batch)  |
    +-----------+-------------------+-----------------------+
    Schedule     React to events    Process at massive scale
    Track assets Sub-second latency Exactly-once guarantees
    Manage deps  Simple Python API  Flink SQL, CDC, Iceberg
    UI/lineage   Rust engine speed  Managed state, checkpoints
```

### Detailed Comparison

| | Dagster | Pathway | Flink |
|---|---|---|---|
| **Primary job** | Batch orchestration | Lightweight streaming | Enterprise streaming + batch |
| **Engine** | Python (delegates to external tools) | Rust (Differential Dataflow) | JVM (custom distributed runtime) |
| **Latency** | Minutes (batch scheduling) | Milliseconds | Milliseconds |
| **State** | Metadata (what ran, when) | In-engine incremental state | Managed state backends with checkpointing |
| **Scale** | Coordinates jobs; scale is in the delegated tools | Single-node to moderate distributed | Massive (trillions of events/day) |
| **Exactly-once** | N/A (orchestrator, not processor) | Enterprise tier | Core feature (checkpoints + 2PC) |
| **SQL** | No (delegates to dbt, DuckDB, etc.) | No | Full ANSI SQL with streaming extensions |
| **CDC** | Via sensors (polls for changes) | Built-in connectors (Postgres, Debezium) | Flink CDC project (MySQL, PG, Oracle, etc.) |
| **Deployment** | `dagster dev` / Dagster+ cloud | `python script.py` / Docker | Flink cluster (K8s, YARN, standalone) |
| **Setup complexity** | Low | Low | High (JVM, cluster, monitoring) |
| **Learning curve** | Gentle (Python decorators) | Gentle (DataFrame-like API) | Steep (streaming semantics, watermarks, state) |
| **License** | Apache 2.0 | BSL 1.1 (converts to Apache 2.0) | Apache 2.0 |
| **Best for** | "Run this ETL daily, track lineage" | "React to Kafka events, enrich in real-time" | "Process billions of events with exactly-once, complex windowing, and SQL" |

### When to Use Each

**Use Dagster when:**
- Batch ETL, data pipeline orchestration
- You need scheduling, lineage tracking, and asset management
- You want a UI to monitor and debug pipeline runs
- Your processing is delegated to dbt, DuckDB, Spark, or external tools

**Use Pathway when:**
- Lightweight real-time streaming
- Simple event processing, enrichment, alerting
- You want to start fast without cluster infrastructure
- Single-node or moderate scale is sufficient
- Rust-engine performance is a priority

**Use Flink when:**
- Enterprise-scale stream processing (billions of events)
- Complex stateful processing with exactly-once guarantees
- You need Flink SQL for declarative stream/batch processing
- Event-time processing with watermarks and late data handling
- CDC pipelines from databases to Kafka/data lakes
- The organization already runs Flink infrastructure
- Regulatory or compliance requirements demand exactly-once

### Using All Three Together

A realistic enterprise architecture with all three:

```
Database (CDC)  -----> Flink CDC -----> Kafka
                                          |
Kafka (events)  -----> Flink (complex    |
                        stateful         |
                        processing)  --->+---> Kafka (enriched)
                                          |         |
                                          |    Pathway (lightweight
                                          |    real-time alerting,
                                          |    anomaly detection)
                                          |         |
                                          v         v
                                    Data Lake (Iceberg/Paimon)
                                          |
                                    Dagster (batch orchestration,
                                    hourly/daily asset materialization,
                                    dbt transforms, reporting)
                                          |
                                          v
                                    Analytics Warehouse
```

- **Flink**: Heavy lifting -- CDC, complex event processing, stateful aggregations, exactly-once ETL to the data lake
- **Pathway**: Lightweight reactions -- real-time alerts, simple enrichment, low-latency dashboards
- **Dagster**: Batch orchestration -- schedule dbt transforms, materialize analytics assets, track lineage

### Zinc Targets All Three

```zinc
// Same project, three engines

// Flink -- complex stateful streaming at scale
@pipeline(engine: "flink", checkpoint: 60.seconds, mode: "exactly_once")
fn process_transactions()
    var txns = kafka.source(topic: "transactions", schema: Transaction)
    var enriched = txns
        .key_by(t -> t.account_id)
        .window(tumbling: 5.minutes, on: event_time)
        .select(
            account_id: first(account_id),
            total: sum(amount),
            count: count(),
            avg: avg(amount)
        )
    kafka.sink(enriched, topic: "account-summaries")
end

// Pathway -- lightweight real-time alerting
@pipeline(engine: "pathway")
fn fraud_alerts()
    var summaries = kafka.read(
        topic: "account-summaries",
        schema: AccountSummary
    )
    var suspicious = summaries
        .filter(s -> s.total > 50000 or s.count > 100)
    kafka.write(suspicious, topic: "fraud-alerts")
end

// Dagster -- batch orchestration and reporting
@asset(group: "compliance", schedule: "0 8 * * *")
fn daily_fraud_report(db: DuckDB)
    db.execute("
        INSERT INTO fraud_reports
        SELECT * FROM fraud_alerts
        WHERE alert_date = CURRENT_DATE - INTERVAL '1 day'
    ")
end
```

---

## 9. Implementation Approach

### Phase 1: Table API Code Generation (Recommended Start)

The Table API is the better starting point for Zinc because:
1. It is higher-level and declarative -- closer to Zinc's style
2. Connector configuration maps cleanly to SQL DDL (generated by the transpiler)
3. The optimizer generates efficient execution plans
4. Most PyFlink users prefer the Table API for standard ETL/streaming jobs

**Transpiler changes:**

- **Lexer/Parser**: `@pipeline(engine: "flink")` annotation already supported (same pattern as Pathway). New keywords: `kafka.source()`, `jdbc.sink()`, `iceberg.sink()`, etc.
- **Typechecker**: Validate that `data` class field types map to Flink SQL types. Check that `.select()`, `.filter()`, `.window()` column references match the schema.
- **Codegen**: Generate PyFlink Python code:
  1. Import `pyflink.table` and related modules
  2. Create `TableEnvironment` with mode (streaming/batch) and checkpoint config
  3. Generate `CREATE TABLE` DDL for each source/sink connector
  4. Generate Table API operations or SQL queries
  5. Generate `INSERT INTO` statements or `execute_insert()` calls
  6. Handle UDFs: generate `@udf` decorated Python functions and register them

### What the Transpiled Output Looks Like

Given this Zinc input:

```zinc
@pipeline(engine: "flink", checkpoint: 60.seconds, mode: "exactly_once")
fn order_processing()
    var orders = kafka.source(
        topic: "orders",
        servers: "kafka:9092",
        group: "order-processor",
        schema: Order,
        watermark: order_time - 10.seconds
    )

    var completed = orders.filter(o -> o.status == "completed")

    kafka.sink(completed,
        topic: "completed-orders",
        servers: "kafka:9092",
        schema: CompletedOrder
    )
end
```

The transpiler generates:

```python
# Generated by zinc transpile -- do not edit
from pyflink.table import EnvironmentSettings, TableEnvironment

def order_processing():
    env_settings = EnvironmentSettings.in_streaming_mode()
    t_env = TableEnvironment.create(env_settings)

    # Checkpoint configuration
    t_env.get_config().set("execution.checkpointing.interval", "60000")
    t_env.get_config().set("execution.checkpointing.mode", "EXACTLY_ONCE")

    # Source: orders
    t_env.execute_sql("""
        CREATE TABLE orders (
            order_id STRING,
            customer_id STRING,
            product_id STRING,
            amount DOUBLE,
            status STRING,
            order_time TIMESTAMP(3),
            WATERMARK FOR order_time AS order_time - INTERVAL '10' SECOND
        ) WITH (
            'connector' = 'kafka',
            'topic' = 'orders',
            'properties.bootstrap.servers' = 'kafka:9092',
            'properties.group.id' = 'order-processor',
            'format' = 'json',
            'scan.startup.mode' = 'latest-offset'
        )
    """)

    # Sink: completed-orders
    t_env.execute_sql("""
        CREATE TABLE completed_orders (
            order_id STRING,
            customer_id STRING,
            amount DOUBLE,
            order_time TIMESTAMP(3)
        ) WITH (
            'connector' = 'kafka',
            'topic' = 'completed-orders',
            'properties.bootstrap.servers' = 'kafka:9092',
            'format' = 'json'
        )
    """)

    # Pipeline logic
    orders = t_env.from_path("orders")
    completed = orders.filter(orders.status == "completed")

    completed.execute_insert("completed_orders")


if __name__ == "__main__":
    order_processing()
```

### Phase 2: DataStream API Support

For users who need low-level control (custom state, timers, side outputs), add DataStream API code generation:

```zinc
// Explicit DataStream mode for fine-grained control
@pipeline(engine: "flink", api: "datastream", checkpoint: 30.seconds)
fn custom_session_tracker()
    var clicks = kafka.source(
        topic: "clickstream",
        servers: "kafka:9092",
        schema: ClickEvent,
        watermark: event_time - 30.seconds
    )

    var sessions = clicks
        .key_by(c -> c.user_id)
        .process(SessionProcessor())

    kafka.sink(sessions, topic: "sessions", servers: "kafka:9092")
end

// Custom stateful processor
class SessionProcessor extends KeyedProcessFunction
    var session_state: ValueState[Session]

    fn process_element(click: ClickEvent, ctx: Context)
        var session = session_state.value() or Session(start: click.event_time)
        session.page_count += 1
        session.last_activity = click.event_time
        session_state.update(session)

        // Set timer to close session after 15 minutes of inactivity
        ctx.timer_service().register_event_time_timer(
            click.event_time + 15.minutes
        )
    end

    fn on_timer(timestamp: int, ctx: OnTimerContext)
        var session = session_state.value()
        if session != null and timestamp - session.last_activity >= 15.minutes
            ctx.output(session)
            session_state.clear()
        end
    end
end
```

### Phase 3: Flink SQL Passthrough

Support inline Flink SQL for complex queries that are easier to express in SQL:

```zinc
@pipeline(engine: "flink")
fn analytics()
    // Register sources
    kafka.source(topic: "orders", schema: Order, name: "orders")
    kafka.source(topic: "customers", schema: Customer, name: "customers")

    // Complex query in Flink SQL
    flink.sql("
        CREATE MATERIALIZED TABLE customer_lifetime_value
        FRESHNESS = INTERVAL '1' HOUR
        AS SELECT
            c.customer_id,
            c.name,
            SUM(o.amount) AS lifetime_value,
            COUNT(*) AS order_count,
            MAX(o.order_time) AS last_order
        FROM orders o
        JOIN customers c ON o.customer_id = c.customer_id
        GROUP BY c.customer_id, c.name
    ")
end
```

### Deployment

PyFlink jobs deploy to a Flink cluster. Zinc generates standard `.py` files that work with any PyFlink deployment option:

| Deployment | How |
|---|---|
| **Kubernetes (Flink Operator)** | `zinc build` generates `.py` files. Package in Docker image with Flink base. Deploy via Flink Kubernetes Operator CRD. |
| **YARN** | Submit `.py` files via `flink run --python`. Application mode, session mode, or per-job mode. |
| **Standalone** | Start Flink cluster, submit `.py` files via CLI. |
| **Managed (Ververica, Confluent, AWS KDA)** | Package `.py` files as ZIP/JAR. Upload to managed platform. |
| **Local development** | `zinc run` transpiles and runs locally with PyFlink's embedded mini-cluster. |

### Type Mapping

| Zinc Type | Flink SQL Type | Python Type |
|---|---|---|
| `str` | `STRING` | `str` |
| `int` | `BIGINT` | `int` |
| `float` | `DOUBLE` | `float` |
| `bool` | `BOOLEAN` | `bool` |
| `timestamp` | `TIMESTAMP(3)` | `datetime` |
| `date` | `DATE` | `date` |
| `bytes` | `BYTES` | `bytes` |
| `list[str]` | `ARRAY<STRING>` | `list[str]` |
| `dict[str, int]` | `MAP<STRING, BIGINT>` | `dict[str, int]` |
| `decimal` | `DECIMAL(38, 18)` | `Decimal` |

---

## Open Questions

1. **Table API vs SQL generation** -- Should Zinc generate Table API calls (`.filter()`, `.select()`) or SQL strings? Table API is more type-safe but SQL is more flexible and widely understood. Could default to Table API for simple operations and SQL for complex queries.

2. **Connector configuration** -- Flink connectors have many options (Kafka: deserializers, offset modes, consumer configs). How much should Zinc expose vs. hide behind conventions? Start minimal, allow `config: {...}` escape hatch for advanced options.

3. **State backend configuration** -- RocksDB, heap, disaggregated. This is operational, not application logic. Could be a `zinc.toml` project-level setting rather than per-pipeline annotation.

4. **UDF performance** -- Python UDFs have serialization overhead. Should Zinc warn when UDFs are used in hot paths? Could suggest Pandas UDFs (vectorized) for batch-oriented operations.

5. **Testing** -- How to test Flink pipelines locally? PyFlink's mini-cluster supports local execution. Zinc could generate test harnesses that run pipelines against in-memory sources/sinks.

6. **Multi-statement execution** -- PyFlink `execute_sql()` is a statement-at-a-time API. Multiple INSERT INTO statements require a `StatementSet`. The transpiler needs to detect multiple sinks and generate `StatementSet` code.

---

## References

- [Apache Flink 2.0.0 Release](https://flink.apache.org/2025/03/24/apache-flink-2.0.0-a-new-era-of-real-time-data-processing/)
- [Apache Flink 2.1.0 Release](https://flink.apache.org/2025/07/31/apache-flink-2.1.0-ushers-in-a-new-era-of-unified-real-time-data--ai-with-comprehensive-upgrades/)
- [Apache Flink 2.2.0 Release](https://flink.apache.org/2025/12/04/apache-flink-2.2.0-advancing-real-time-data--ai-and-empowering-stream-processing-for-the-ai-era/)
- [PyFlink Documentation](https://nightlies.apache.org/flink/flink-docs-stable/api/python/index.html)
- [PyFlink Deep Dive (Quix)](https://quix.io/blog/pyflink-deep-dive)
- [All You Need to Know About PyFlink (Ververica)](https://www.ververica.com/blog/all-you-need-to-know-about-pyflink)
- [Flink CDC GitHub](https://github.com/apache/flink-cdc)
- [Flink vs Spark Comparison (Decodable)](https://www.decodable.co/blog/comparing-apache-flink-and-spark-for-modern-stream-data-processing)
- [Flink vs Spark (AWS)](https://aws.amazon.com/blogs/big-data/a-side-by-side-comparison-of-apache-spark-and-apache-flink-for-common-streaming-use-cases/)
- [PyFlink on PyPI](https://pypi.org/project/apache-flink/)
- [Flink SQL Past, Present, Future (Ververica)](https://www.ververica.com/blog/apache-flink-sql-past-present-and-future)
- [PyFlink Kubernetes Deployment](https://pyflink.readthedocs.io/en/main/getting_started/installation/kubernetes.html)
- [Hands-On Introduction to PyFlink (Decodable)](https://www.decodable.co/blog/a-hands-on-introduction-to-pyflink)
