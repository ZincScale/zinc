<!-- Licensed under the Apache License, Version 2.0 -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Exploration: Dagster Pipelines in Zinc v2

**Status:** Exploratory -- not committed to implementation
**Date:** 2026-03-18
**Context:** Zinc v2 transpiles `.zn` files to `.py` files. Dagster is pure Python. This is a natural fit.

---

## 1. What is Dagster

Dagster is a cloud-native data pipeline orchestrator built around the concept of **software-defined assets** -- the idea that you should define your pipelines around the data they produce, not the tasks that produce it.

### Core Concepts

| Concept | What It Is |
|---|---|
| **Software-Defined Asset** | A Python function decorated with `@asset` that produces a data artifact (table, file, model). Dependencies are declared via function parameters. The asset graph is the pipeline. |
| **Op** | The core unit of computation. An `@op`-decorated function that takes inputs, does work, produces outputs. Lower-level than assets -- used when you need fine-grained control. |
| **Graph** | A DAG of interconnected ops or sub-graphs. Defines the topology without binding to execution details. |
| **Job** | A graph bound to resources, config, and an executor. The runnable unit. |
| **Resource** | An abstraction for external systems (databases, APIs, cloud services). Injected into ops/assets via dependency injection. Swappable per environment. |
| **IO Manager** | Controls how op/asset outputs are stored and loaded. Pluggable -- swap between local files, S3, DuckDB, etc. without changing op logic. |
| **Sensor** | An `@sensor`-decorated function that polls external state and triggers runs when conditions are met (new file arrives, API returns data, upstream asset materializes). |
| **Schedule** | Cron-based trigger for jobs. `@schedule` decorator with a cron string. |
| **Partition** | Splits an asset into logical chunks (by date, category, region). Enables incremental processing and targeted backfills. |
| **Asset Check** | `@asset_check`-decorated function that validates asset data quality after materialization -- row counts, schema validation, value ranges. |
| **Definitions** | The top-level registry that collects all assets, ops, jobs, resources, sensors, and schedules into a deployable unit. |

### How Dagster Differs from Airflow

Airflow is **task-centric** -- you define a DAG of tasks (operators) and schedule them. The focus is "what steps do I run and when." Data lineage is an afterthought bolted on via XCom.

Dagster is **asset-centric** -- you define the data artifacts your system produces and how they depend on each other. The execution plan is derived from the asset graph. Key differences:

- **Airflow**: DAG of tasks. You reason about execution order. Data passes through XCom (limited) or external storage (manual).
- **Dagster**: Graph of assets. You reason about data dependencies. IO managers handle storage transparently.
- **Airflow**: Testing requires spinning up the scheduler, database, and worker. Heavy.
- **Dagster**: Assets are plain Python functions. Unit test with pytest, no infrastructure needed.
- **Airflow**: 30M+ monthly downloads, 80K+ organizations. The incumbent.
- **Dagster**: Fastest-growing alternative (~12K GitHub stars as of early 2026). Asset-centric model resonates with modern data teams.

### Current State (2025-2026)

- Dagster 1.x stable, with active development on the Components framework (GA October 2025)
- Dagster+ (cloud) offers serverless and hybrid deployment, RBAC, SSO, branch deployments, CI/CD
- Airflow 3.0 released April 2025 with asset-aware scheduling (catching up to Dagster's model)
- Prefect remains strong for event-driven, dynamic workflows but lacks Dagster's asset lineage

---

## 2. Why Dagster for Zinc

The key insight: **Dagster is pure Python**. Every Dagster concept -- assets, ops, graphs, resources, sensors, schedules -- is just a decorated Python function or class. Since Zinc v2 transpiles to `.py` files, Zinc can generate standard Dagster code directly. No special runtime, no plugins, no FFI.

### The Fit

| Zinc Principle | Dagster Alignment |
|---|---|
| "It's just Python underneath" | Dagster assets are just Python functions. Zinc generates them. |
| Zero ceremony | Dagster's `@asset` decorator is already minimal. Zinc can make it even cleaner. |
| Enforced types | Dagster has its own type system (`DagsterType`) plus supports Python type hints. Zinc's type enforcement maps naturally. |
| Convention over configuration | Dagster's `Definitions` auto-discovery scans modules for decorated objects. Zinc can auto-generate the `Definitions` object. |
| The transpiler works for you | Zinc can auto-inject Dagster boilerplate: imports, `Definitions`, resource wiring, IO manager config. |

### What Zinc Adds Over Raw Dagster Python

1. **No decorator stacking** -- Zinc uses keywords or annotations instead of `@asset`, `@op`, `@resource` decorator chains
2. **Auto-generated Definitions** -- Zinc scans all `.zn` files in a project and generates the `Definitions` object automatically
3. **Type safety** -- Zinc enforces types at transpile time; Dagster only checks at runtime
4. **Less boilerplate** -- No `import dagster`, no `self` on resources, no `context: AssetExecutionContext` parameter unless needed
5. **Cleaner dependencies** -- Asset dependencies declared via typed parameters, validated by the Zinc type checker before generating Python

---

## 3. NiFi Concepts to Dagster Mapping

For teams moving from NiFi-style data flow to code-defined pipelines:

| NiFi Concept | Dagster Equivalent | Notes |
|---|---|---|
| **Processor** | `@asset` or `@op` | A single processing step. Assets are preferred -- they produce named, trackable outputs. |
| **FlowFile** | Asset materialization / op output | In NiFi, data flows as FlowFiles with attributes + content. In Dagster, data is materialized as assets with metadata. |
| **Connection** | Asset dependency (function parameter) | NiFi connects processors via queues. Dagster connects assets via function signatures -- if asset B takes asset A as a parameter, B depends on A. |
| **Process Group** | `@graph` or asset group | Logical grouping of related processors/assets. |
| **Back-Pressure** | Concurrency limits + partitions | NiFi connections have back-pressure thresholds. Dagster uses run concurrency limits (`max_concurrent_runs`), op-level concurrency keys, and partition-based incremental processing. |
| **Provenance** | Asset lineage + metadata | NiFi tracks every FlowFile's history. Dagster tracks asset materialization events, metadata, and full lineage graphs via the UI. |
| **Controller Service** | Resource | Shared services (database pools, API clients) injected into processors/assets. |
| **Bulletin Board** | Asset checks + sensors | Alerting and validation. Asset checks validate data quality; sensors monitor external state. |
| **Input/Output Port** | Graph inputs/outputs | How data enters/exits a process group or graph. |
| **Funnel** | Multi-asset or `@graph` | Merge multiple streams into one. |
| **Parameter Context** | Resource config / `EnvVar` | Runtime configuration injected into processors/assets. |

### Key Difference: Push vs Pull

NiFi is a **push-based** system -- data flows through processors as events arrive. Dagster is **pull-based** -- the orchestrator decides when to materialize assets based on schedules, sensors, or manual triggers.

For streaming/real-time NiFi workloads, Dagster is not a direct replacement. But for batch ETL, data transformation, and ML pipelines -- the majority of NiFi enterprise usage -- Dagster's asset model is cleaner and more maintainable.

---

## 4. Zinc Syntax for Pipelines

### Dagster Python (Before)

```python
import dagster as dg
from dagster_duckdb import DuckDBResource

@dg.asset(
    group_name="raw",
    metadata={"dagster/storage_kind": "duckdb"},
)
def raw_orders(context: dg.AssetExecutionContext, duckdb: DuckDBResource) -> None:
    with duckdb.get_connection() as conn:
        conn.execute("""
            CREATE TABLE IF NOT EXISTS raw_orders AS
            SELECT * FROM read_csv('data/orders.csv')
        """)
    context.log.info("Loaded raw orders")


@dg.asset(
    deps=[raw_orders],
    group_name="transformed",
)
def completed_orders(duckdb: DuckDBResource) -> None:
    with duckdb.get_connection() as conn:
        conn.execute("""
            CREATE TABLE IF NOT EXISTS completed_orders AS
            SELECT * FROM raw_orders WHERE status = 'completed'
        """)


@dg.asset_check(asset=completed_orders)
def check_completed_orders_not_empty(duckdb: DuckDBResource) -> dg.AssetCheckResult:
    with duckdb.get_connection() as conn:
        count = conn.execute("SELECT COUNT(*) FROM completed_orders").fetchone()[0]
    return dg.AssetCheckResult(
        passed=count > 0,
        metadata={"row_count": count},
    )


@dg.schedule(cron_schedule="0 6 * * *", target="*")
def daily_refresh(context: dg.ScheduleEvaluationContext):
    return dg.RunRequest()


defs = dg.Definitions(
    assets=[raw_orders, completed_orders],
    asset_checks=[check_completed_orders_not_empty],
    schedules=[daily_refresh],
    resources={"duckdb": DuckDBResource(database="warehouse.db")},
)
```

### Zinc v2 (After)

```zinc
// pipeline.zn — order processing pipeline
import duckdb

// Assets are just functions with the `asset` annotation
// Dependencies declared via typed parameters — the transpiler wires them

@asset(group: "raw", storage: "duckdb")
fn raw_orders(db: DuckDB)
    db.execute("
        CREATE TABLE IF NOT EXISTS raw_orders AS
        SELECT * FROM read_csv('data/orders.csv')
    ")
    log.info("Loaded raw orders")
end

@asset(group: "transformed")
fn completed_orders(db: DuckDB, raw_orders: Asset)
    db.execute("
        CREATE TABLE IF NOT EXISTS completed_orders AS
        SELECT * FROM raw_orders WHERE status = 'completed'
    ")
end

// Asset checks — validate data after materialization
@check(completed_orders)
fn check_not_empty(db: DuckDB): CheckResult
    var count = db.execute("SELECT COUNT(*) FROM completed_orders").fetchone()[0]
    return CheckResult(passed: count > 0, metadata: {"row_count": count})
end

// Schedule — cron trigger
@schedule("0 6 * * *")
fn daily_refresh(): RunRequest
    return RunRequest()
end

// Resources — declared at module level, auto-collected
@resource
var db = DuckDB(database: "warehouse.db")

// No Definitions object needed — Zinc auto-generates it from annotations
```

**What the transpiler generates:** The Zinc compiler scans all `@asset`, `@check`, `@schedule`, `@sensor`, and `@resource` annotations, then auto-generates the `Definitions(...)` object at the bottom of the output `.py` file. The developer never writes it.

### More Examples

#### Op-Based Pipeline (Fine-Grained Control)

```zinc
// etl_ops.zn — when you need ops instead of assets

@op
fn extract_csv(path: str): list[dict]
    var reader = csv.DictReader(open(path))
    return list(reader)
end

@op
fn transform_orders(raw: list[dict]): list[dict]
    return raw.filter(r -> r["status"] == "completed")
        .map(r -> {
            "id": r["id"],
            "amount": float(r["amount"]),
            "customer": r["customer"].upper()
        })
end

@op
fn load_to_db(orders: list[dict], db: DuckDB)
    db.execute_many("INSERT INTO orders VALUES (?, ?, ?)", orders)
end

// Graph wires ops together
@graph
fn order_etl()
    var raw = extract_csv("data/orders.csv")
    var transformed = transform_orders(raw)
    load_to_db(transformed)
end
```

#### Sensor (Event-Driven)

```zinc
// sensors.zn — watch for new files

@sensor(target: order_etl, interval: 30)
fn new_file_sensor(): SensorResult
    var files = os.listdir("/data/incoming")
    var new_csvs = files.filter(f -> f.endswith(".csv"))

    if new_csvs.is_empty()
        return SkipReason("No new files")
    end

    return RunRequest(
        run_config: {"ops": {"extract_csv": {"path": new_csvs[0]}}}
    )
end
```

#### Partitioned Asset

```zinc
// partitioned.zn — daily partitioned processing

@asset(
    partitions: DailyPartition(start: "2024-01-01"),
    group: "analytics"
)
fn daily_revenue(db: DuckDB, partition_key: str): float
    var result = db.execute("
        SELECT SUM(amount) FROM orders
        WHERE date = '{partition_key}'
    ").fetchone()[0]
    return result or 0.0
end
```

### Transpilation Mapping

| Zinc v2 | Generated Python |
|---|---|
| `@asset(group: "raw")` | `@dg.asset(group_name="raw")` |
| `@op` | `@dg.op` |
| `@check(target_asset)` | `@dg.asset_check(asset=target_asset)` |
| `@schedule("0 6 * * *")` | `@dg.schedule(cron_schedule="0 6 * * *", target="*")` |
| `@sensor(target: job, interval: 30)` | `@dg.sensor(job=job, minimum_interval_seconds=30)` |
| `@resource` | `dg.ConfigurableResource` subclass or plain resource |
| `db: DuckDB` parameter | `duckdb: DuckDBResource` with Dagster resource injection |
| `log.info(...)` | `context.log.info(...)` (context auto-injected) |
| _(auto-generated)_ | `defs = dg.Definitions(assets=[...], ...)` |

---

## 5. Dagster Ecosystem

Dagster's integration library covers the modern data stack. Since these are all Python packages, Zinc can use them directly -- the transpiled `.py` output imports them like any other dependency.

### Key Integrations

| Integration | Package | What It Does |
|---|---|---|
| **dbt** | `dagster-dbt` | Wraps dbt projects as Dagster assets. Each dbt model becomes an asset with lineage. `DbtProject` and `DbtProjectComponent` for declarative config. |
| **DuckDB** | `dagster-duckdb` | IO managers for DuckDB: `DuckDBPandasIOManager`, `DuckDBPolarsIOManager`, `DuckDBPySparkIOManager`. Read/write assets as DataFrames. |
| **Polars** | `dagster-polars` | Use Polars eager or lazy DataFrames as asset inputs/outputs. Native integration with Dagster's type system. |
| **Spark** | `dagster-spark` | Configure and run Spark jobs as ops/assets. PySpark DataFrame IO. |
| **Airbyte** | `dagster-airbyte` | Orchestrate Airbyte connections as assets. Trigger syncs, track lineage from source to warehouse. |
| **dbt + Airbyte** | DAD stack | Airbyte (Extract-Load) + dbt (Transform) + Dagster (Orchestrate). Full ELT pipeline. |
| **Snowflake** | `dagster-snowflake` | Snowflake resource and IO managers. |
| **BigQuery** | `dagster-gcp` | BigQuery IO managers, GCS resources. |
| **AWS** | `dagster-aws` | S3 IO managers, ECS launchers, Glue resources. |
| **Azure** | `dagster-azure` | Blob Storage and ADLS2 resources (added 2025). |
| **Pandas** | `dagster-pandas` | DataFrame type constraints and validation. |
| **OpenAI** | `dagster-openai` | Orchestrate LLM calls as assets/ops. |
| **Tableau/Looker** | `dagster-tableau`, `dagster-looker` | BI tool integration -- dashboards as downstream assets with lineage. |

### Zinc + Dagster Ecosystem

Because Zinc transpiles to standard `.py` files:

```zinc
// zinc can use any dagster integration directly
import duckdb
import polars

@asset(group: "analytics")
fn customer_summary(db: DuckDB): polars.DataFrame
    var df = db.execute("SELECT * FROM customers").pl()
    return df.group_by("region")
        .agg([
            polars.col("revenue").sum().alias("total_revenue"),
            polars.col("id").count().alias("customer_count")
        ])
end
```

The Zinc transpiler resolves `DuckDB` to `DuckDBResource` and generates proper Dagster resource injection. The developer writes clean, typed code; the transpiler handles the wiring.

---

## 6. Implementation Approach

### Phase 1: Annotation Recognition

The Zinc parser recognizes pipeline annotations: `@asset`, `@op`, `@graph`, `@check`, `@sensor`, `@schedule`, `@resource`. These are not arbitrary decorators -- they are known to the transpiler and generate specific Dagster patterns.

**Transpiler changes:**
- Lexer: `@` already tokenized (annotation support exists from v1 C# backend)
- Parser: Map annotation names to known Dagster concepts. Validate parameters.
- Typechecker: Verify asset dependencies match declared types. Check resource parameters exist.
- Codegen: Generate `@dg.asset`, `@dg.op`, etc. with proper Dagster import and parameter translation.

### Phase 2: Auto-Generated Definitions

After transpiling all `.zn` files in a project, the Zinc build step:

1. Scans all generated `.py` files for `@dg.asset`, `@dg.op`, `@dg.sensor`, etc.
2. Generates a `definitions.py` file that imports and collects everything:

```python
# AUTO-GENERATED by zinc build — do not edit
import dagster as dg
from pipeline import raw_orders, completed_orders, check_not_empty, daily_refresh

defs = dg.Definitions(
    assets=[raw_orders, completed_orders],
    asset_checks=[check_not_empty],
    schedules=[daily_refresh],
    resources={"db": DuckDBResource(database="warehouse.db")},
)
```

Alternatively, use Dagster's `load_definitions_from_module()` for auto-discovery, which scans module scope for decorated objects. The Zinc build step would generate a single entry point that calls this.

### Phase 3: Resource Injection

Zinc simplifies Dagster's resource injection:

```zinc
// Zinc — declare resource type on parameter
@asset
fn my_asset(db: DuckDB, s3: S3Resource)
    // ...
end
```

Transpiles to:

```python
@dg.asset
def my_asset(
    context: dg.AssetExecutionContext,
    db: DuckDBResource,
    s3: S3Resource,
) -> None:
    # ...
```

The transpiler:
- Recognizes known resource types (`DuckDB` -> `DuckDBResource`, etc.)
- Auto-injects `context: AssetExecutionContext` when `log` is used in the function body
- Maps Zinc named-arg style (`DuckDB(database: "foo")`) to Python keyword args (`DuckDBResource(database="foo")`)

### Phase 4: Project Scaffolding

`zinc init --pipeline` generates a Dagster-ready project:

```
my_pipeline/
    main.zn           # entry point with resource declarations
    assets/
        raw.zn        # raw data assets
        transform.zn  # transformation assets
    checks/
        quality.zn    # asset checks
    schedules.zn      # schedules and sensors
    zinc.toml         # project config
```

The `zinc.toml` includes:

```toml
[project]
name = "my_pipeline"
type = "dagster"

[dagster]
module = "my_pipeline"
resources.db = { type = "DuckDB", database = "warehouse.db" }
```

---

## 7. Performance at Scale

### How Dagster Handles Large Data

Dagster does **not** move data through the orchestrator. Unlike NiFi (where FlowFile content passes through the system), Dagster assets produce data in external systems (databases, object stores, data lakes). The orchestrator tracks **metadata** -- what was produced, when, by whom, with what config.

| Concern | Dagster Approach | NiFi Approach |
|---|---|---|
| **Large payloads** | IO managers write to external storage (S3, DuckDB, Snowflake). Only metadata passes through Dagster. | FlowFile content streams through processors. Content repository handles large files. |
| **Incremental processing** | Partitions. Materialize only the partitions that changed. Backfill specific date ranges. | Back-pressure on connections. Processors consume at their own pace. |
| **Backpressure** | Concurrency limits at run, op, and asset level. `max_concurrent_runs` on the instance. Concurrency keys on ops. | Connection queue thresholds. When queue hits limit, upstream processor pauses. |
| **Fault tolerance** | Per-partition retries. Failed partitions don't block others. Op-level retry policies. | Processor-level retry with penalty. FlowFiles route to failure relationships. |
| **Parallelism** | Multi-process executor, Celery executor, Dagster+ serverless auto-scaling. Partition-level parallelism. | Thread-based within NiFi JVM. Clustered NiFi for horizontal scale. |
| **Observability** | Built-in asset lineage graph, materialization history, metadata timeline, asset checks dashboard. | Provenance repository, bulletin board, data flow visualization. |

### Dagster Concurrency Controls

```zinc
// Zinc syntax for concurrency-limited asset
@asset(
    partitions: DailyPartition(start: "2024-01-01"),
    concurrency: 5  // max 5 partitions running simultaneously
)
fn daily_report(db: DuckDB, partition_key: str)
    // process one day's data
    db.execute("INSERT INTO reports SELECT * FROM raw WHERE date = '{partition_key}'")
end
```

Transpiles to:

```python
@dg.asset(
    partitions_def=dg.DailyPartitionsDefinition(start_date="2024-01-01"),
    op_tags={"dagster/concurrency_key": "daily_report", "dagster/max_concurrent": "5"},
)
def daily_report(context: dg.AssetExecutionContext, db: DuckDBResource) -> None:
    partition_key = context.partition_key
    db.execute(f"INSERT INTO reports SELECT * FROM raw WHERE date = '{partition_key}'")
```

### When Dagster Is Not the Right Tool

- **Real-time streaming**: Dagster is batch-oriented. For sub-second latency, use Kafka/Flink/NiFi.
- **Binary data routing**: NiFi excels at routing binary content (images, PDFs, sensor data) between systems. Dagster is about data transformations.
- **Simple cron jobs**: If you just need to run a script on a schedule, `cron` or a task queue is simpler.

---

## 8. Comparison: Dagster vs Prefect vs Airflow for Zinc

| Factor | Dagster | Prefect | Airflow |
|---|---|---|---|
| **Core model** | Asset-centric (data-first) | Task-centric (flow-first) | Task-centric (DAG-first) |
| **Pure Python** | Yes -- decorators on functions | Yes -- decorators on functions | Mostly -- but Operators are class-heavy |
| **Type system** | Built-in `DagsterType` + Python hints | Python hints only | No built-in type system |
| **Zinc transpilation fit** | Excellent -- `@asset` functions are clean targets | Good -- `@flow`/`@task` are clean | Moderate -- Operator classes are verbose |
| **Local dev** | First-class -- `dagster dev` with UI | Good -- `prefect server start` | Painful -- full infra required |
| **Asset lineage** | Built-in, core feature | Artifacts (limited) | Asset-aware scheduling (new in 3.0) |
| **Cloud offering** | Dagster+ (serverless/hybrid) | Prefect Cloud | Astronomer, MWAA |
| **Community (2026)** | ~12K GitHub stars, growing fast | ~18K GitHub stars | ~39K GitHub stars, incumbent |

**Recommendation for Zinc:** Dagster is the strongest fit because:
1. Asset-centric model aligns with Zinc's "define what you produce" philosophy
2. Pure Python decorators are ideal transpilation targets
3. Built-in type system complements Zinc's type enforcement
4. Local development story is excellent (no infrastructure to spin up)
5. Dagster's `Definitions` auto-discovery means Zinc can generate clean, modular code

---

## Open Questions

1. **Annotation vs keyword**: Should Zinc use `@asset` (annotation, like Dagster) or `asset fn` (keyword, like Kotlin)? Annotations are closer to the generated Python. Keywords are more "Zinc."

2. **Resource declaration syntax**: Should resources live in `zinc.toml` (config) or in `.zn` files (code)? Dagster supports both patterns.

3. **Multi-file asset graphs**: When assets span multiple `.zn` files, how does the transpiler resolve cross-file dependencies? Dagster handles this via module imports -- Zinc could mirror this.

4. **Testing story**: Zinc should generate testable assets. Dagster assets are plain functions -- Zinc's test runner could call them directly with mock resources.

5. **Dagster UI integration**: The Dagster UI (`dagster dev`) works on generated `.py` files. Should `zinc dev` wrap `dagster dev` and add file watching for `.zn` changes?

---

## References

- [Dagster Software-Defined Assets](https://dagster.io/glossary/software-defined-assets)
- [Dagster GitHub Repository](https://github.com/dagster-io/dagster)
- [Dagster Concepts](https://docs.dagster.io/getting-started/concepts)
- [Dagster Ops and Jobs](https://docs.dagster.io/guides/build/ops)
- [Dagster Partitions](https://docs.dagster.io/guides/build/partitions-and-backfills/partitioning-assets)
- [Dagster Concurrency](https://docs.dagster.io/guides/operate/managing-concurrency)
- [Dagster Backpressure](https://dagster.io/glossary/data-backpressure)
- [Dagster Integrations](https://docs.dagster.io/integrations)
- [Dagster DuckDB Integration](https://docs.dagster.io/integrations/libraries/duckdb/using-duckdb-with-dagster)
- [Dagster Sensors](https://docs.dagster.io/guides/automate/sensors)
- [Dagster Asset Checks](https://docs.dagster.io/guides/test/asset-checks)
- [Dagster Definitions API](https://docs.dagster.io/api/dagster/definitions)
- [Dagster Pricing](https://dagster.io/pricing)
- [Dagster vs Airflow vs Prefect (2026)](https://bix-tech.com/airflow-vs-dagster-vs-prefect-which-workflow-orchestrator-should-you-choose-in-2026/)
- [Orchestration Showdown: Dagster vs Prefect vs Airflow](https://www.zenml.io/blog/orchestration-showdown-dagster-vs-prefect-vs-airflow)
- [FreeAgent: Comparing Prefect, Dagster, Airflow, and Mage](https://engineering.freeagent.com/2025/05/29/decoding-data-orchestration-tools-comparing-prefect-dagster-airflow-and-mage/)
