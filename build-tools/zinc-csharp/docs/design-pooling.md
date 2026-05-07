# Pooling & Memory Borrowing Design

This document explains the pool types available in .NET, why we chose each strategy
for zinc-flow-csharp, and how they interact with the GC.

## The Problem

Every `Execute()` call in the flow pipeline allocates several short-lived objects:

```
Claim() → new QueueEntry
Process() → new FlowFile (via WithAttribute) + new Dictionary (attribute copy) + new SingleResult
Route() → new List<string> (destinations) + new QueueEntry (offer to next queue)
```

At 100K+ flowfiles/sec, this creates millions of Gen0 objects per second. Each Gen0 collection
pauses the thread for ~0.5-2ms. At scale, GC pauses dominate wall-clock time.

**Goal:** Eliminate allocations on the hot path so the GC has nothing to collect.

---

## Pool Types in .NET

### 1. `ArrayPool<T>` (System.Buffers)

**What it pools:** Raw arrays (`T[]`). Ships with the runtime — no dependencies.

**How it works:**
- Maintains per-size buckets of previously returned arrays
- `Rent(minLength)` returns an array >= minLength (may be larger — caller must track actual length)
- `Return(array)` puts it back in the bucket for reuse
- Thread-safe via partitioned buckets (similar to jemalloc size classes)

**When to use:**
- Byte buffers (HTTP bodies, file reads, serialization scratch space)
- Any temporary `T[]` that would otherwise be allocated and immediately discarded
- Especially important for arrays >85KB (these go on the Large Object Heap and trigger Gen2 collections)

**zinc-flow usage:**
```csharp
// Raw content: rent byte[] from pool instead of allocating
public Raw(ReadOnlySpan<byte> data) {
    _rented = ArrayPool<byte>.Shared.Rent(data.Length);
    data.CopyTo(_rented);
}

// FlowQueue: rent backing array from pool
_items = ArrayPool<QueueEntry?>.Shared.Rent(initialSize);
```

**Key detail:** `Shared` is the global singleton. You can also create custom pools with
`ArrayPool<T>.Create(maxArrayLength, maxArraysPerBucket)` for isolation.

---

### 2. `MemoryPool<T>` (System.Buffers)

**What it pools:** `IMemoryOwner<T>` wrappers around `Memory<T>`. Built on top of `ArrayPool<T>`.

**How it works:**
- `Rent(minBufferSize)` returns an `IMemoryOwner<T>` (implements `IDisposable`)
- The owner's `Memory` property gives you a `Memory<T>` slice
- `Dispose()` returns the underlying array to the pool
- Integrates with `using` statements and async pipelines (Pipelines API)

**When to use:**
- When you need `Memory<T>` or `Span<T>` semantics (async I/O, System.IO.Pipelines)
- When you want ownership semantics enforced by `IDisposable`
- Kestrel and ASP.NET use this internally for request/response buffers

**Why we didn't use it here:**
The flow engine operates synchronously within each processor. `ArrayPool<byte>` is sufficient
and avoids the `IMemoryOwner<T>` wrapper allocation. MemoryPool makes more sense for async
I/O pipelines (e.g., if we added network transport).

---

### 3. `ObjectPool<T>` (Microsoft.Extensions.ObjectPool)

**What it pools:** Arbitrary objects. Ships as a NuGet package (Microsoft.Extensions.ObjectPool).

**How it works:**
- Uses a `DefaultObjectPoolPolicy<T>` with `Create()` and `Return()` hooks
- Stores one "fast" item in a field (no locking) + overflow items in an array with `Interlocked.CompareExchange`
- Bounded: default max = `Environment.ProcessorCount * 2`

**When to use:**
- When you need a general-purpose thread-safe pool with bounded size
- ASP.NET middleware, StringBuilder pooling, etc.
- Good default choice when you don't know the access pattern

**Why we didn't use it here:**
- Requires a NuGet dependency (we want zero dependencies)
- Uses `Interlocked.CompareExchange` on every rent/return — unnecessary overhead for
  our single-threaded-per-processor model
- Bounded at `ProcessorCount * 2` by default — we want per-thread unbounded-ish pools

---

### 4. `ConcurrentBag<T>` (System.Collections.Concurrent)

**What it pools:** Any objects, used as an ad-hoc pool. Ships with the runtime.

**How it works:**
- Maintains per-thread linked lists (thread-local storage via `ThreadLocal<T>`)
- `Add()` pushes to the current thread's list (no contention)
- `TryTake()` pops from current thread's list first; if empty, **steals** from another thread's list
- Stealing requires locking the victim thread's list

**When to use:**
- Producer-consumer patterns where items are added and removed from the same thread
- Multi-threaded pools where cross-thread sharing is needed
- Quick and dirty "pool" when you don't want to write your own

**Why we replaced it:**
- `TryTake()` on an empty bag (cold pool during benchmark startup) does a full scan of all
  thread-local lists before returning false — this is O(threadCount)
- Even on a warm pool, there's overhead from the `ThreadLocal<T>` indirection and the
  `Monitor.Enter` for the thread-local work-stealing list
- Our v1 used ConcurrentBag; switching to ThreadStatic improved offer throughput by ~30%

---

### 5. `[ThreadStatic]` Array Pool (what we use)

**What it pools:** Arbitrary objects, zero contention, zero locking.

**How it works:**
```csharp
internal static class Pool<T> where T : class, new()
{
    private const int MaxPerThread = 256;

    [ThreadStatic]
    private static T[]? t_items;    // Each thread gets its own array

    [ThreadStatic]
    private static int t_count;     // Stack pointer into t_items

    public static T Rent()
    {
        if (t_items is not null && t_count > 0)
            return t_items[--t_count];  // Pop: array index + decrement
        return new T();                 // Cold path: allocate
    }

    public static void Return(T obj)
    {
        t_items ??= new T[MaxPerThread];
        if (t_count < MaxPerThread)
            t_items[t_count++] = obj;   // Push: array index + increment
        // else: pool full, let GC collect it
    }
}
```

**Performance characteristics:**
- `Rent()` hot path: 1 null check + 1 comparison + 1 array access + 1 decrement = **~2-3ns**
- `Return()` hot path: 1 null check + 1 comparison + 1 array access + 1 increment = **~2-3ns**
- Zero contention: each thread's `t_items` is completely independent
- Zero locking: no `Monitor`, no `Interlocked`, no `CAS`
- Cache-friendly: the array is in the thread's L1/L2 cache

**Trade-offs:**
- Objects cannot cross threads (if thread A returns an object, only thread A can rent it back)
- Memory is per-thread: 10 threads × 256 objects × 40 bytes = ~100KB (negligible)
- No `IDisposable` / lifetime tracking — caller must remember to return
- Pool size is fixed; overflow objects are dropped (GC collects them)

**Why this is ideal for zinc-flow:**
Each processor runs on a single thread (goroutine model). FlowFiles are created, processed,
and consumed on the same thread within a single `Execute()` call. The pool never needs
cross-thread access.

---

## `[ThreadStatic]` vs `ThreadLocal<T>` vs `ConcurrentBag`

| Feature | `[ThreadStatic]` | `ThreadLocal<T>` | `ConcurrentBag<T>` |
|---|---|---|---|
| Access cost | Direct field access | Indirection through holder | TLS + linked list |
| Rent/Return | ~2-3ns | ~10-15ns | ~50-200ns |
| Cross-thread | No | No | Yes (stealing) |
| Initialization | Lazy (null on first access) | Factory delegate | Automatic |
| Async-safe | No (thread may change) | No (same) | Yes |
| Memory overhead | Fixed array per thread | Wrapper object per thread | Linked list per thread |

**Why not `ThreadLocal<T>`?** It wraps a `ThreadLocalMap` lookup (~10ns overhead).
`[ThreadStatic]` is a raw field on the thread's TLS block — the JIT emits a direct
memory access via the `fs`/`gs` segment register on x64. Fastest possible TLS access.

**Async caveat:** `[ThreadStatic]` fields are tied to OS threads. If you `await` in the
middle of a rent/return cycle, the continuation may run on a different thread. zinc-flow's
`Execute()` is fully synchronous within the lock, so this is not an issue.

---

## How Objects Flow Through the Pool

```
Execute() call:
  ┌─ Claim()
  │    entry = Pool<QueueEntry>.Rent()   ← from pool (or new if cold)
  │    entry.ClaimedAt = now             ← mutate in-place
  │    invisible[id] = entry             ← stored in dict
  │
  ├─ Process() [AddAttribute]
  │    outFf = Pool<FlowFile>.Rent()     ← from pool
  │    outFf.Attributes = overlay(...)   ← zero-copy overlay
  │    result = Pool<SingleResult>.Rent()← from pool
  │    result.FlowFile = outFf
  │
  ├─ RouteResult()
  │    destBuffer.Clear()                ← reuse list (no alloc)
  │    engine.GetDestinations(attrs, destBuffer)
  │    destQ.Offer(outFf)               ← entry via Pool<QueueEntry>.Rent()
  │
  ├─ Cleanup
  │    Pool<SingleResult>.Return(result) ← back to pool
  │    Pool<FlowFile>.Return(inputFf)    ← back to pool
  │    Ack() → Pool<QueueEntry>.Return() ← back to pool
  └─
```

After warmup, the pool reaches steady-state: every `Rent()` hits the pool (no `new`),
every `Return()` has space. **Zero allocations per Execute() call** in steady state.

---

## Ref-Counted Content

Content (Raw byte buffers) is shared across FlowFiles via `WithAttribute`. A ref count
tracks how many FlowFiles reference the same Content:

```csharp
// WithAttribute: shared content → increment ref count
public static FlowFile WithAttribute(FlowFile ff, string key, string value)
{
    ff.Content.AddRef();  // _refCount++
    return Rent(ff.NumericId, ff.Attributes.With(key, value), ff.Content, ff.Timestamp);
}

// Return: decrement; when zero → return ArrayPool bytes + pool Raw shell
public static void Return(FlowFile ff)
{
    ff.Content?.Release();  // if (--_refCount == 0) { ArrayPool.Return + Pool.Return }
    ff.Content = null!;
    Pool<FlowFile>.Return(ff);
}
```

**Why non-atomic?** FlowFiles move through pipeline stages sequentially. When Thread A
offers a FlowFile to a queue, Thread A is done with it. Thread B claims it later — there
is no concurrent access to the same Content. `_refCount++` / `_refCount--` is sufficient.

**Lifecycle example** (2-hop pipeline):
```
Create(data)        → Raw refCount=1, FF1
WithAttribute(FF1)  → refCount=2, FF2 shares Raw with FF1
Return(FF1)         → refCount=1 (FF1 done, FF2 still in next queue)
WithAttribute(FF2)  → refCount=2, FF3 shares Raw
Return(FF2)         → refCount=1
Return(FF3)         → refCount=0 → ArrayPool.Return(bytes) + Pool<Raw>.Return(shell)
```

All pooled result types: `SingleResult`, `RoutedResult`, `MultipleResult`, `FailureResult`.
Each has `Rent()` and `Return()` static methods. ProcessSession returns them after consumption.

---

## Remaining Allocations (Not Pooled)

Everything on the hot path is pooled. The remaining allocations are in setup:

1. **Initial `Dictionary<string,string>`** passed to `FlowFile.Create()` — caller-provided.
   Use `FlowFile.CreateEmpty()` when attributes are empty to avoid this allocation.

2. **Dictionary internal resizing** in FlowQueue's invisible map — `Dictionary<long, QueueEntry>`
   resizes its bucket array as it grows. Pre-sizing at construction helps but doesn't eliminate it.

---

## ArrayPool in the Queue

The queue's backing array (`_items`) is rented from `ArrayPool<QueueEntry?>`:

```csharp
// Initial allocation
_items = ArrayPool<QueueEntry?>.Shared.Rent(initialSize);

// Growth (when tail reaches capacity)
var newItems = ArrayPool<QueueEntry?>.Shared.Rent(newSize);
Array.Copy(_items, _head, newItems, 0, liveCount);
ArrayPool<QueueEntry?>.Shared.Return(_items, clearArray: false);
_items = newItems;
```

This matters because queue backing arrays can be large (100K+ entries). Without pooling,
each queue resize allocates a new array on the LOH (>85KB), triggering Gen2 collection.
With `ArrayPool`, the old array is returned and the new one may be a recycled buffer.

---

## GC Impact

Evolution across optimization phases (100K session benchmark, 2 hops, AOT):

| Phase | Gen0 | Session rate | Key change |
|---|---|---|---|
| v1 (no pooling) | 21-27 | 265-552K ff/s | baseline |
| v2 (ThreadStatic pools) | 14-16 | 900K-2.08M ff/s | Pool FF, QueueEntry, SingleResult |
| v3 (ref-counted + full pool) | **gc0: 0** | **2.0-2.5M ff/s** | Ref-counted Content, pool Raw/all results |

With ref counting, **zero GC collections occur during session execution** (gc0: 0 at all sizes).
The remaining Gen0 collections happen during benchmark setup (pre-loading FlowFiles with
`new Dictionary` allocations) — not during the timed execution phase.
