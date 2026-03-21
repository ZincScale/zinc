# FlowFile Benchmark Results — Java 25

**Date**: 2026-03-20
**Machine**: 8 CPUs, Linux 4.18.0-553
**Java**: 25.0.1, OpenJDK, ZGC, virtual threads

---

## Java 25 — ZGC + Virtual Threads

Java 25 with ZGC (concurrent low-latency collector) and virtual threads (Project Loom). Uses a manual `ConcurrentLinkedDeque`-based byte[] pool for large FlowFiles (100KB+).

### Queue Throughput — Read-Only (msgs/sec)

| FlowFile Size | Java naive | Java fanout (4c) |
|---|---|---|
| **1 KB** | 719,735 | 1,800,261 |
| **10 KB** | 864,001 | 1,851,377 |
| **100 KB** | 1,081,853 | 2,778,632 |
| **1 MB** | 1,344,152 | 2,824,451 |

Key observation: Java naive doesn't degrade at large sizes. ZGC's concurrent collection avoids stop-the-world pauses at 100KB+.

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

Best cancellation latency — virtual thread `interrupt()` is immediate, no polling or token checking.

---

## Why Java 25

1. **ZGC makes naive code competitive** — processor authors don't need to learn buffer pooling
2. **Virtual threads give the best cancellation** — 114 us processor stop latency
3. **Pooling is available when needed** — for 100KB+ hot paths, a simple byte[] pool closes the gap
4. **Quarkus/GraalVM provides AOT** — native-image for single-binary deployment
5. **Ecosystem** — Kafka, JDBC, S3, gRPC clients are all native Java
6. **Developer experience** — plain Java processors, no struct lifecycle management
