# FlowFile Benchmark Results — Python 3.14t vs .NET 10 vs Java 25

**Date**: 2026-03-20 (updated from 2026-03-19)
**Machine**: 8 CPUs, Linux 4.18.0-553
**Python**: 3.14.3 free-threading build (GIL disabled)
**.NET**: 10.0.5, Server GC, Release build
**Java**: 25.0.1, OpenJDK, ZGC, virtual threads

---

## Key Finding: OwnedBuffer Pooled Lifecycle

The single most important optimization for .NET flow processing. See `docs/design-owned-buffer-pattern.md` for the full design.

**The problem**: Naive .NET allocates `new byte[]` on every FlowFile unpack and repack. At 100KB-1MB sizes, this triggers Gen2 GC storms that make .NET slower than Python.

**The solution**: Rent buffers from `ArrayPool<byte>.Shared`, flow them through `Channel<OwnedBuffer>` as structs, return explicitly. Zero heap allocation in the hot path.

**The result**: .NET goes from slower-than-Python to 2-14x faster at all FlowFile sizes, including the large sizes where Python previously dominated.

---

## Benchmark 1: Queue Throughput (read-only, producer/consumer)

Shuttling FlowFile V3 binary packets through in-memory queues. Consumer unpacks but does not modify.

### .NET Results — Naive vs Optimized (msgs/sec, single consumer)

| FlowFile Size | Naive (new byte[]) | Zero-Copy (Memory slice) | ArrayPool (rent/return) |
|---|---|---|---|
| **1 KB** | 471,160 | 846,151 | 506,158 |
| **10 KB** | 147,253 | 595,495 | 489,086 |
| **100 KB** | 6,808 | 1,373,815 | 263,779 |
| **1 MB** | 1,621 | 1,732,952 | 16,878 |

### .NET GC Impact (single consumer)

| FlowFile Size | Naive Gen2 | Naive Alloc | Zero-Copy Gen2 | Zero-Copy Alloc |
|---|---|---|---|---|
| **1 KB** | 1 | 171 MB | 0 | 71 MB |
| **10 KB** | 4 | 525 MB | 0 | 36 MB |
| **100 KB** | 61 | 984 MB | 0 | ~0 MB |
| **1 MB** | 35 | 2,002 MB | 0 | ~0 MB |

### Python Results (msgs/sec)

| FlowFile Size | threading | asyncio | fanout (4 consumers) |
|---|---|---|---|
| **1 KB** | 134,604 | 183,171 | 168,540 |
| **10 KB** | 74,209 | 130,729 | 147,032 |
| **100 KB** | 57,522 | 80,275 | 113,021 |
| **1 MB** | 14,508 | 13,432 | 31,533 |

### Read-Only Analysis

- **.NET zero-copy dominates all sizes** — Memory<byte> slice is just pointer arithmetic.
- **.NET naive is terrible at 100KB+** — Gen2 GC kills throughput (6.8K vs 1.3M msgs/sec at 100KB).
- **Python is consistent** — refcounted bytes don't trigger GC storms, but never reaches .NET optimized speeds.
- **Multiprocessing is dead** — IPC serialization overhead makes it 10-40x slower than threading. Free-threading replaces it.

---

## Benchmark 2: Mutate FlowFile (unpack -> modify attrs + content -> repack)

The realistic processor workload. Every stage unpacks, modifies attributes, XOR-transforms first 64 bytes of content, repacks, and sends downstream.

### .NET Results — Naive vs Pooled (msgs/sec)

| FlowFile Size | Naive | Optimized | Pooled | Pooled Fanout (4w) |
|---|---|---|---|---|
| **1 KB** | 165,282 | 219,700 | 344,900 | 433,091 |
| **10 KB** | 47,441 | 32,156 | 452,600 | 1,343,421 |
| **100 KB** | 6,455 | 5,602 | 140,632 | 386,519 |
| **1 MB** | 800 | 827 | 9,299 | 28,241 |

### .NET GC Impact — Mutate (single consumer)

| FlowFile Size | Naive Gen2 | Naive Alloc | Pooled Gen2 | Pooled Alloc |
|---|---|---|---|---|
| **1 KB** | 0 | 334 MB | 0 | 72 MB |
| **10 KB** | 1 | 1,046 MB | 0 | 36 MB |
| **100 KB** | 94 | 1,967 MB | 0 | ~0 MB |
| **1 MB** | 55 | 4,003 MB | 0 | ~0 MB |

### Python Results — Mutate (msgs/sec)

| FlowFile Size | Single processor | 4-worker fanout |
|---|---|---|
| **1 KB** | 49,516 | 99,477 |
| **10 KB** | 33,543 | 79,041 |
| **100 KB** | 19,938 | 60,777 |
| **1 MB** | 2,605 | 5,669 |

### Mutate Analysis

- **Naive .NET "optimized" was barely better than naive** — `PackagePooled` still called `ToArray()` at the end, triggering the same GC pressure.
- **Pooled lifecycle is the breakthrough** — eliminates `ToArray()` entirely. Buffer stays rented, flows through channel as struct, returned by consumer.
- **.NET pooled beats Python at ALL sizes** — including 100KB-1MB where Python previously dominated.
- **Free-threading helps Python scale** — 4-worker fanout doubles throughput, but still can't match .NET pooled.

---

## Benchmark 3: 3-Stage Pipeline (source -> procA -> procB -> procC -> sink)

The real-world test. Each of 3 processors unpacks, mutates, repacks. Proves the OwnedBuffer pool stays bounded under sustained multi-stage load.

### Head-to-Head (msgs/sec)

| FlowFile Size | Python 3-stage | .NET naive 3-stage | .NET pooled 3-stage | .NET pooled vs Python |
|---|---|---|---|---|
| **1 KB** | 27,037 | 327,413 | **390,567** | **14x** |
| **10 KB** | 24,138 | 84,252 | **287,444** | **12x** |
| **100 KB** | 13,265 | 3,675 | **73,744** | **5.6x** |
| **1 MB** | 2,590 | 492 | **5,274** | **2x** |

### .NET GC — 3-Stage Pipeline

| FlowFile Size | Naive Gen2 | Naive Alloc | Pooled Gen2 | Pooled Alloc |
|---|---|---|---|---|
| **1 KB** | 0 | 1,032 MB | 2 | 347 MB |
| **10 KB** | 13 | 3,153 MB | 2 | 220 MB |
| **100 KB** | 121 | 5,905 MB | 2 | 61 MB |
| **1 MB** | 77 | 12,010 MB | 3 | 149 MB |

### Pipeline Analysis

- **Pool stays bounded**: 2,000 x 1MB through 3 stages = 12 GB garbage (naive) vs 149 MB (pooled). The ArrayPool recycles buffers — each processor returns the incoming buffer before requesting the next message.
- **Naive .NET loses to Python at 100KB+**: 3,675 vs 13,265 at 100KB. GC storms from 121 Gen2 collections.
- **Pooled .NET wins at ALL sizes**: Even at 1MB (worst case), 5,274 vs 2,590 — 2x advantage.
- **Python's immutable bytes is the bottleneck**: Every mutation creates new `bytes(bytearray(content))` — two allocations per stage. No equivalent to ArrayPool.

---

## Benchmark 4: Cancellation Latency (processor stop time)

How fast can a running processor be stopped? Critical for hot-swap/scale operations.

### .NET Results

| Scenario | Throughput (msgs/sec) | Cancel Latency |
|---|---|---|
| Single consumer, 100KB | 1,183,690 | 3,428 us |
| 4-consumer fanout, 100KB | 2,197,137 | 586 us |
| Single consumer, 1MB | 699,996 | 1,722 us |
| 4-consumer fanout, 1MB | 848,887 | **136 us** |

### Python: No Native Cancellation

Python `threading.Thread` has no cooperative cancellation mechanism. Options:
- Sentinel values: must wait for current `queue.get()` to return + process
- `threading.Event`: requires polling, latency = poll interval
- `asyncio`: has cancellation but loses free-threading parallelism

Estimated stop latency: **10-100ms** depending on FlowFile size and processing time.

.NET advantage: `CancellationToken` integrates into `Channel.ReadAsync` — instant cooperative cancellation at the await point. Sub-millisecond in fanout scenarios.

---

## Benchmark 5: HTTP API Throughput

aiohttp (Python) vs Kestrel/ASP.NET Core (.NET) — FlowFile ingress/egress over HTTP.

### POST (msgs/sec)

| FlowFile Size | Python c=10 | Python c=50 | .NET c=10 | .NET c=50 |
|---|---|---|---|---|
| **1 KB** | 6,363 | 6,580 | 22,940 | 30,563 |
| **10 KB** | 5,664 | 6,058 | 19,624 | 30,256 |
| **100 KB** | 4,117 | 5,994 | 6,089 | 2,987 |
| **1 MB** | 653 | 611 | 443 | 265 |

### GET Round-trip (msgs/sec)

| FlowFile Size | Python c=10 | Python c=50 | .NET c=10 | .NET c=50 |
|---|---|---|---|---|
| **1 KB** | 6,700 | 7,171 | 38,282 | 62,932 |
| **10 KB** | 6,625 | 6,463 | 27,574 | 52,414 |
| **100 KB** | 5,604 | 5,049 | 9,193 | 4,993 |
| **1 MB** | 1,115 | 1,001 | 683 | 911 |

### HTTP Analysis

- **.NET dominates small payloads** (1-10KB): Kestrel's IO pipeline and thread pool scaling.
- **Python competitive at large payloads** (100KB+): HTTP overhead becomes noise relative to payload.
- **HTTP is not the bottleneck for flow processing** — internal queue throughput is 10-100x higher than HTTP. HTTP is only for ingress/egress at pipeline boundaries.

---

## Benchmark 6: Java 25 — ZGC + Virtual Threads

Java 25 with ZGC (concurrent low-latency collector) and virtual threads (Project Loom). Uses the same OwnedBuffer pooling pattern via a manual `ConcurrentLinkedDeque`-based byte[] pool.

### Queue Throughput — Read-Only (msgs/sec)

| FlowFile Size | Java naive | Java fanout (4c) |
|---|---|---|
| **1 KB** | 719,735 | 1,800,261 |
| **10 KB** | 864,001 | 1,851,377 |
| **100 KB** | 1,081,853 | 2,778,632 |
| **1 MB** | 1,344,152 | 2,824,451 |

Key observation: Java naive doesn't degrade at large sizes. ZGC's concurrent collection avoids the Gen2 stop-the-world pauses that kill .NET naive at 100KB+.

### Mutate FlowFile (msgs/sec)

| FlowFile Size | Java naive | Java pooled |
|---|---|---|
| **1 KB** | 223,710 | 312,839 |
| **10 KB** | 58,796 | 309,553 |
| **100 KB** | 13,888 | 64,043 |
| **1 MB** | 921 | 5,480 |

### 3-Stage Pipeline (msgs/sec)

| FlowFile Size | Java naive | Java pooled |
|---|---|---|
| **1 KB** | 392,346 | 296,709 |
| **10 KB** | 92,664 | 83,712 |
| **100 KB** | 15,719 | 64,997 |
| **1 MB** | 2,222 | 5,590 |

Note: Java pooled is *slower* than Java naive at 1-10KB because `ConcurrentLinkedDeque` pool overhead exceeds GC cost at small sizes. Pooling only helps at 100KB+ where allocation cost dominates.

### Cancellation Latency

| Scenario | Latency |
|---|---|
| Virtual thread interrupt, 100KB | **196 us** |
| Virtual thread interrupt, 1MB | **114 us** |

Best cancellation latency of all three runtimes. Virtual thread `interrupt()` is immediate — no polling, no token checking.

---

## Head-to-Head: 3-Stage Pipeline (the real test)

Source -> ProcA -> ProcB -> ProcC -> Sink. Each processor unpacks, mutates attrs+content, repacks.

### msgs/sec

| Size | Python 3.14t | .NET naive | .NET pooled | Java naive (ZGC) | Java pooled |
|---|---|---|---|---|---|
| **1KB** | 27,037 | 327,413 | **390,567** | 392,346 | 296,709 |
| **10KB** | 24,138 | 84,252 | **287,444** | 92,664 | 83,712 |
| **100KB** | 13,265 | 3,675 | 73,744 | 15,719 | **64,997** |
| **1MB** | 2,590 | 492 | 5,274 | 2,222 | **5,590** |

### Cancellation Latency

| Runtime | Mechanism | Latency |
|---|---|---|
| **Java 25** | Virtual thread interrupt | **114-196 us** |
| .NET 10 | CancellationToken + Channel | 136-3,428 us |
| Python 3.14t | None (sentinel polling) | ~10-100 ms |

---

## The Real Tradeoff: Performance Ceiling vs Developer Experience

### .NET 10 (pooled OwnedBuffer)

**Pros:**
- Highest throughput at 1KB-100KB (the common FlowFile range)
- ArrayPool<T>.Shared is a first-class runtime API
- Channel<T> is purpose-built for producer/consumer
- Native AOT for single-binary deployment

**Cons:**
- OwnedBuffer contract is expert-level — every processor author must get rent/return right or leak buffers
- Struct lifecycle management is a footgun
- The optimization that makes it fast (pooled buffers) is the thing that makes it hard to write processors

### Java 25 (ZGC + virtual threads)

**Pros:**
- ZGC makes naive code fast — no pooling needed below 100KB
- Virtual threads give best-in-class cancellation (114 us)
- Processor authors write plain Java — no buffer lifecycle to learn
- Quarkus provides AOT story (GraalVM native-image)
- Massive ecosystem for connectors (Kafka, JDBC, S3 clients all native)
- At 100KB-1MB, Java pooled matches .NET pooled

**Cons:**
- No stdlib ArrayPool — manual pool implementation needed for 100KB+ optimization
- JVM startup slower than .NET AOT (mitigated by Quarkus/GraalVM)
- Gradle/Maven build tooling is heavyweight

### Python 3.14t (free-threading)

**Pros:**
- Simplest processor code to write
- ML/data science ecosystem (Polars, NumPy, scikit-learn)
- Free-threading enables real parallelism

**Cons:**
- Slowest at all pipeline benchmarks (2-14x behind .NET/Java)
- No cooperative cancellation for threads
- Immutable `bytes` forces allocation on every mutation
- Not viable as the flow engine runtime

### The Insight

For a flow engine, the runtime is invisible — users write **processors**. The runtime's job is to be fast and get out of the way. Java ZGC achieves this: processor authors write normal code and get competitive throughput. .NET pooled is faster but pushes complexity onto every processor author.

The question isn't "which is fastest at expert-level optimization?" — it's "which gives the best throughput when the average developer writes a processor?"

| Scenario | .NET | Java | Winner |
|---|---|---|---|
| Expert writes pooled processor | .NET 287K | Java 84K (10KB pipeline) | .NET |
| Average dev writes naive processor | .NET 84K | Java 93K (10KB pipeline) | **Java** |
| Expert writes pooled 100KB+ | .NET 74K | Java 65K (100KB pipeline) | .NET (slight) |
| Average dev writes naive 100KB+ | .NET 3.7K | Java 16K (100KB pipeline) | **Java 4x** |
| Cancellation latency | 136-3428 us | 114-196 us | **Java** |
| Processor developer experience | Complex (OwnedBuffer) | Simple (plain Java) | **Java** |

At 100KB — the typical FlowFile size — a naive Java processor is **4x faster** than a naive .NET processor because ZGC doesn't stop the world for large allocations. The .NET dev has to learn OwnedBuffer to compete. The Java dev just writes code.

---

## Decision: Revisited

**The flow engine runtime should be Java 25** with ZGC and virtual threads.

1. **ZGC makes naive code competitive** — processor authors don't need to learn buffer pooling
2. **Virtual threads give the best cancellation** — 114 us processor stop latency
3. **Pooling is available when needed** — for 100KB+ hot paths, a simple byte[] pool closes the gap with .NET
4. **Quarkus/GraalVM provides AOT** — native-image for single-binary deployment
5. **Ecosystem** — Kafka, JDBC, S3, gRPC clients are all native Java. NiFi itself is Java.
6. **Developer experience** — plain Java processors, no struct lifecycle management

.NET remains the faster option for expert-authored, performance-critical inner loops. But for a platform where the value is in the breadth of processors (connectors, transforms, enrichers), Java's "naive is fast enough" property is the winning attribute.

Python remains the scripting/ML layer — processor logic that needs Polars, NumPy, or scikit-learn can call Python via interop from a Java processor.
