# Design: OwnedBuffer Pattern — Zero-Allocation FlowFile Processing

> **Status**: VALIDATED — benchmarked 2026-03-20, see `benchmarks/RESULTS.md`

## Problem

NiFi-style flow processing shuttles large binary payloads (FlowFiles) between processor stages via queues. Each processor must:

1. **Unpack** — parse attributes and content from the packed binary
2. **Mutate** — modify attributes and/or transform content
3. **Repack** — serialize the modified FlowFile for the next stage
4. **Enqueue** — send downstream

In .NET, the naive approach allocates `new byte[]` at both unpack (content extraction) and repack (output serialization). At typical FlowFile sizes (10KB-1MB), this creates extreme GC pressure:

```
Naive: 2,000 x 1MB FlowFiles through 3 stages
  = 6,000 unpack allocations (1MB each) + 6,000 repack allocations (1MB each)
  = ~12 GB of garbage
  = 77 Gen2 GC collections
  = 492 msgs/sec
```

Python avoids some of this because `bytes` is refcounted (no generational GC pause), but still allocates on every mutation because `bytes` is immutable.

## Solution: OwnedBuffer Pooled Lifecycle

Rent buffers from `ArrayPool<byte>.Shared`, pass them through `Channel<OwnedBuffer>` as value-type structs, and return them explicitly when done.

### Core Types

```csharp
/// Buffer rented from ArrayPool that carries its actual used length.
/// Flows through Channel<OwnedBuffer>. Consumer calls Return() when done.
struct OwnedBuffer
{
    public byte[] Array;   // rented from ArrayPool<byte>.Shared
    public int Length;     // actual data length (Array.Length >= Length)

    public ReadOnlySpan<byte> Span => Array.AsSpan(0, Length);
    public ReadOnlyMemory<byte> Memory => new(Array, 0, Length);

    public void Return()
    {
        if (Array != null)
            ArrayPool<byte>.Shared.Return(Array);
        Array = null!;
    }
}

/// Lightweight reference to unpacked FlowFile data.
/// Content is a Memory<byte> slice into the original OwnedBuffer — zero-copy.
readonly struct FlowFileRef
{
    public readonly Dictionary<string, string> Attributes;
    public readonly ReadOnlyMemory<byte> Content;
}
```

### Key Operations

```csharp
// Zero-copy unpack: slices into the OwnedBuffer, no content allocation
FlowFileRef UnpackageZeroCopy(OwnedBuffer owned)

// Pack directly into a rented buffer — no ToArray(), no GC allocation
OwnedBuffer PackageToOwned(Dictionary<string, string> attrs, ReadOnlySpan<byte> content)
```

### The Processor Contract

Every processor stage follows this exact sequence:

```
Step 1: Receive OwnedBuffer from upstream channel
Step 2: Zero-copy unpackage (Memory<byte> slice — points into rented buffer)
Step 3: Rent work buffer, copy content, mutate in place
Step 4: PackageToOwned → new OwnedBuffer (writes into fresh rented buffer)
Step 5: Return work buffer to pool
Step 6: Return incoming OwnedBuffer to pool  ← safe: content was copied in step 3
Step 7: Send new OwnedBuffer downstream
```

```csharp
static OwnedBuffer MutateOwnedToOwned(OwnedBuffer incoming, string processorName)
{
    // Step 2: zero-copy unpack
    var ff = FlowFileV3.UnpackageZeroCopy(incoming);
    var attrs = ff.Attributes;
    attrs["processed_by"] = processorName;

    int len = ff.Content.Length;
    var pool = ArrayPool<byte>.Shared;

    // Step 3: rent work buffer, copy + mutate
    var contentBuf = pool.Rent(len);
    ff.Content.Span.CopyTo(contentBuf);
    TransformContent(contentBuf.AsSpan(0, len));

    // Step 4: package into new rented output buffer
    var outOwned = FlowFileV3.PackageToOwned(attrs, contentBuf.AsSpan(0, len));

    // Step 5: return work buffer
    pool.Return(contentBuf);

    // Step 6: return incoming buffer — safe, content was copied in step 3
    incoming.Return();

    return outOwned;
}
```

### Buffer Lifecycle in a 3-Stage Pipeline

```
Source          ProcA           ProcB           ProcC           Sink
  |               |               |               |               |
  |--[OwnedBuf]-->|               |               |               |
  |               |--[OwnedBuf]-->|               |               |
  |               |  Return(in)   |--[OwnedBuf]-->|               |
  |               |               |  Return(in)   |--[OwnedBuf]-->|
  |               |               |               |  Return(in)   | Return(final)
```

At any instant, each processor holds **at most 3 rented buffers**:
- `incoming` — being read (returned after content is copied)
- `contentBuf` — work buffer for mutation (returned after repackage)
- `outOwned` — output being written to downstream channel (returned by next stage)

The pool recycles these buffers. Under sustained load, no new allocations occur after warmup.

### Pool Boundedness Proof

Benchmark: 2,000 x 1MB FlowFiles through 3 stages

| Metric | Naive (`new byte[]`) | Pooled (OwnedBuffer) |
|---|---|---|
| Gen0 collections | 77 | 3 |
| Gen1 collections | 77 | 3 |
| Gen2 collections | 77 | 3 |
| Total allocated | 12,010 MB | 149 MB |
| **Throughput** | **492 msgs/s** | **5,274 msgs/s** |

The 149MB residual allocation is from `Dictionary<string,string>` attribute creation (not content buffers). Future optimization: pool attribute dictionaries.

## Why Not Python

Python `bytes` is immutable. Every content mutation requires:
```python
ba = bytearray(content)    # allocate mutable copy
ba[i] ^= 0xAA              # mutate
new_content = bytes(ba)     # allocate immutable copy
```

Two allocations per mutation, per stage. No equivalent to ArrayPool rent/return. Free-threading helps parallelism but not per-item allocation cost.

| FlowFile Size | Python 3-stage | .NET pooled 3-stage | .NET advantage |
|---|---|---|---|
| 1KB | 27,037 msgs/s | 390,567 msgs/s | 14x |
| 10KB | 24,138 | 287,444 | 12x |
| 100KB | 13,265 | 73,744 | 5.6x |
| 1MB | 2,590 | 5,274 | 2x |

## Why Not Python — Cancellation

.NET `CancellationToken` integrates into `Channel.ReadAsync`:

```csharp
await foreach (var item in channel.Reader.ReadAllAsync(cts.Token))
{
    // Instantly cancellable — no polling, no sentinel
}
```

Processor stop latency: **0.1-3.4ms**.

Python `threading.Thread` has no cooperative cancellation. Options are:
- Sentinel values (fragile, doesn't handle mid-processing cancel)
- `threading.Event` polling (latency depends on poll interval)
- `asyncio` cancellation (works but single-threaded, loses free-threading parallelism)

For hot-swap/stop/scale processor lifecycle in a flow engine, this is a fundamental limitation.

## Implications for Zinc Flow

### 1. Runtime is .NET

The OwnedBuffer pattern + CancellationToken + Channel<T> give 2-14x throughput advantage over Python at realistic FlowFile workloads. Combined with native AOT compilation for deployment, .NET is the right runtime for the flow engine.

### 2. Zinc Syntax Hides the Complexity

Developers write:
```zinc
@processor
fn enrich_order(flow: FlowFile): FlowFile {
    flow.attributes["enriched"] = "true"
    return flow.with_content(transform(flow.content))
}
```

The Zinc compiler generates code that follows the OwnedBuffer contract. The developer never sees ArrayPool, OwnedBuffer, or Return().

### 3. Serialization Only at Boundaries

Within a ProcessorGroup (same process): `Channel<OwnedBuffer>` — zero-copy struct passing.
Between ProcessorGroups (NATS): serialize to wire format — this is the only place we pay serialization cost.

### 4. Future Optimizations

- **Attribute dictionary pooling** — `ObjectPool<Dictionary<string,string>>` to eliminate the remaining GC pressure
- **Content-by-reference** — for read-only processors, skip the content copy entirely (step 3 becomes a no-op)
- **SIMD content transforms** — `Vector256<byte>` for XOR/checksum operations on content buffers
- **Pinned buffers for I/O** — `GCHandle.Alloc` for network send without copying (or use `MemoryPool<byte>` with `Pin()`)

## Reference

- Benchmarks: `benchmarks/flowfile-queue/dotnet/Program.cs`
- Python comparison: `benchmarks/flowfile-queue/python/queue_bench.py`
- Results: `benchmarks/RESULTS.md`
- GC pressure patterns: https://dev.to/adrianbailador/reducing-garbage-collector-gc-pressure-in-net-practical-patterns-and-tools-5al3
