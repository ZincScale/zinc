# Functional Collection Methods — Design Document

## Overview

LINQ-style chaining on collections, transpiled to fused Go loops. No intermediate allocations, no iterator wrappers, no `.stream()/.collect()` ceremony.

## Syntax

```zinc
let result = nums.Where(x => x > 5).Select(x => x * 2).Take(10)
let sum = nums.Where(x => x > 5).Aggregate(0, (acc, x) => acc + x)
let hasAdmin = users.Any(u => u.isAdmin)
let first = users.First(u => u.age > 18) or { return }
let names = people.Select(p => p.name).ToList()
let sorted = people.Where(p => p.active).OrderBy(p => p.age).Select(p => p.name)
let grouped = people.GroupBy(p => p.city)
let allTags = posts.SelectMany(p => p.tags)
```

## Naming (LINQ-style)

| Zinc | C# LINQ | Category |
|------|---------|----------|
| `Where` | Where | Filtering |
| `Select` | Select | Projection |
| `SelectMany` | SelectMany | Flat projection |
| `Aggregate` | Aggregate | Reduction |
| `OrderBy` | OrderBy | Sorting |
| `OrderByDescending` | OrderByDescending | Sorting |
| `GroupBy` | GroupBy | Grouping |
| `Any` | Any | Quantifier (short-circuit) |
| `All` | All | Quantifier (short-circuit) |
| `First` | First | Element (short-circuit, failable) |
| `FirstOrDefault` | FirstOrDefault | Element (short-circuit, returns zero) |
| `Last` | Last | Element (must scan all, failable) |
| `Count` | Count | Aggregation |
| `Sum` | Sum | Aggregation |
| `Min` / `Max` | Min / Max | Aggregation |
| `Take` | Take | Partitioning (early exit) |
| `Skip` | Skip | Partitioning |
| `TakeWhile` | TakeWhile | Partitioning |
| `SkipWhile` | SkipWhile | Partitioning |
| `Distinct` | Distinct | Filtering (streaming with set) |
| `Zip` | Zip | Combining |
| `ForEach` | — | Side effect terminal |
| `ToList` | ToList | Materialization terminal |
| `ToDictionary` | ToDictionary | Materialization terminal |

## Lambda Syntax

```zinc
x => expr                    // single parameter
(acc, x) => acc + x          // multiple parameters
(k, v) => v > 100            // map entry destructuring
```

Expression-body only for v1 (single expression, no blocks).

## Codegen Strategy: Loop Fusion

Since Zinc controls the full pipeline (parser → typechecker → codegen), we compile the entire chain into a **single fused loop** rather than using lazy iterators or intermediate slices.

### Example: Where + Select + Take

**Zinc:**
```zinc
let result = nums.Where(x => x > 5).Select(x => x * 2).Take(10)
```

**Emitted Go:**
```go
var result []int
_count := 0
for _, _v0 := range nums {
    if _count >= 10 { break }
    if _v0 > 5 {
        _v1 := _v0 * 2
        result = append(result, _v1)
        _count++
    }
}
```

### Example: Where + Aggregate (reduce)

**Zinc:**
```zinc
let sum = nums.Where(x => x > 5).Select(x => x * 2).Aggregate(0, (acc, x) => acc + x)
```

**Emitted Go:**
```go
sum := 0
for _, _v0 := range nums {
    if _v0 > 5 {
        _v1 := _v0 * 2
        sum = sum + _v1
    }
}
```

### Example: Any (short-circuit)

**Zinc:**
```zinc
let hasAdmin = users.Any(u => u.isAdmin)
```

**Emitted Go:**
```go
hasAdmin := false
for _, _v0 := range users {
    if _v0.IsAdmin() {
        hasAdmin = true
        break
    }
}
```

### Example: First (failable, short-circuit)

**Zinc:**
```zinc
let admin = users.First(u => u.isAdmin) or { return }
```

**Emitted Go:**
```go
var admin User
var _found bool
for _, _v0 := range users {
    if _v0.IsAdmin() {
        admin = _v0
        _found = true
        break
    }
}
if !_found {
    return fmt.Errorf("no matching element found")
}
```

## Chain Segmentation for Non-Fusible Operations

Some operations require materialization (must see all elements):

| Operation | Why |
|-----------|-----|
| `OrderBy` | Must sort all elements |
| `GroupBy` | Must group all elements |
| `Distinct` | Needs a seen-set |
| `Last` | Must scan all elements |

When these appear mid-chain, the chain is **segmented** at materialization points. Each segment is fused independently.

**Zinc:**
```zinc
let result = nums.Where(x => x > 5).OrderBy(x => x).Select(x => x * 2).Take(3)
```

**Emitted Go:**
```go
// Segment 1: Where → OrderBy (materialization point)
var _seg1 []int
for _, _v0 := range nums {
    if _v0 > 5 {
        _seg1 = append(_seg1, _v0)
    }
}
sort.Slice(_seg1, func(i, j int) bool {
    return _seg1[i] < _seg1[j]
})

// Segment 2: Select → Take (fused from sorted result)
var result []int
_count := 0
for _, _v0 := range _seg1 {
    if _count >= 3 { break }
    _v1 := _v0 * 2
    result = append(result, _v1)
    _count++
}
```

## Type Inference Through Chains

The typechecker walks left to right:

```
nums: List<Int>
  .Where(x => x > 5)        → x: Int, pred returns Bool → List<Int>
  .Select(x => x * 2)       → x: Int, expr is Int → List<Int>
  .Select(x => x.toString()) → x: Int, expr is String → List<String>
  .Aggregate("", (a, x) => a + x) → acc: String, x: String → String
```

Lambda parameter types are inferred from the upstream element type. No explicit type annotations needed.

## AST Representation

```
ChainExpr {
    Source: Expression        // the starting collection
    Steps: []ChainStep        // ordered list of operations
}

ChainStep {
    Method: string            // "Where", "Select", "Aggregate", etc.
    Args: []Expression        // lambda and any other args
}

LambdaExpr {
    Params: []string          // parameter names
    Body: Expression          // single expression (v1)
}
```

The parser recognizes method call chains where method names match known collection methods and arguments are lambdas. Codegen sees the entire chain as one unit.

## Map Operations

For `Map<K, V>` types:

```zinc
let expensive = prices.Where((k, v) => v > 100)   // Map → Map
let names = prices.Keys()                           // already exists
let vals = prices.Values()                          // already exists
```

Map-specific chaining deferred to after list chaining is solid.

## Implementation Order

1. **Lambda expressions** — parser + typechecker for `x => expr` and `(a, b) => expr`
2. **Single-step methods** — `Where`, `Select`, `ForEach` (no chaining, just emit a for loop)
3. **Chain recognition** — parser builds `ChainExpr` for multi-step chains
4. **Loop fusion codegen** — fuse Where+Select+terminal combinations
5. **Short-circuit terminals** — `Any`, `All`, `First`, `Take`
6. **Materialization segmentation** — handle `OrderBy`, `GroupBy` in chains
7. **Aggregate** and remaining terminals

Steps 1-4 deliver a working v1. Steps 5-7 round it out.

## v1 Method Set

**Must have:**
Where, Select, SelectMany, Aggregate, ForEach, Any, All, First, Count, Take, Skip, ToList

**v1.1:**
OrderBy, OrderByDescending, GroupBy, Distinct, Zip, Sum, Min, Max, TakeWhile, SkipWhile, Last, FirstOrDefault, ToDictionary

## Why Loop Fusion (not lazy iterators)

Go's compiler doesn't inline through interface method calls reliably. Lazy iterator wrappers (C#/Rust style) would be *slower* in Go than fused loops. Loop fusion gives us the *result* of lazy evaluation (single pass, no intermediates, early exit) without the *mechanism* (iterator wrapper structs). The generated Go is simple, readable, and exactly what a human would write.

## Why Not Intermediate Slices

Eager evaluation (each step creates a new slice) is simplest to implement but wasteful. A chain like `list.Where(...).Select(...).Take(10)` would filter the *entire* list, map the *entire* filtered list, then take 10. Loop fusion does it in one pass, stopping at 10.

## Parallel Collections via `AsParallel()` (PLINQ-style)

Goroutines are cheap (~2KB stack), making Go uniquely suited for parallel collection processing. Zinc leverages this with an explicit `AsParallel()` opt-in, combining **loop fusion + parallelism** — each goroutine runs a fused loop over its chunk.

### Syntax

```zinc
// Sequential (default) — fused single loop
let result = nums.Where(x => x > 5).Select(x => x * 2).ToList()

// Parallel — fused loop per chunk, goroutine per chunk
let result = nums.AsParallel().Where(x => x > 5).Select(x => heavyCompute(x)).ToList()

// Parallel aggregate
let sum = nums.AsParallel().Where(x => x > 5).Aggregate(0, (acc, x) => acc + x)

// Parallel short-circuit
let hasNeg = nums.AsParallel().Any(x => x < 0)
```

### Design Decisions

1. **Explicit opt-in** — `AsParallel()` is required. Parallelism has overhead (goroutine creation, chunk allocation, merge step). For small collections or cheap lambdas, sequential is faster. The user decides.
2. **Order-preserving by default** — Chunks are processed in parallel but merged in index order. Could add `.AsUnordered()` later for more speed when order doesn't matter.
3. **Worker count = `runtime.NumCPU()`** — Sensible default. Could add `.WithDegreeOfParallelism(n)` later.
4. **Chunk-based partitioning** — The source collection is split into `NumCPU()` contiguous chunks. Each goroutine processes one chunk with a fused loop.

### Parallelizability by Operation

| Operation | Parallelizable? | Strategy |
|-----------|----------------|----------|
| `Where` | Yes | Each chunk filters independently |
| `Select` | Yes | Each chunk maps independently |
| `SelectMany` | Yes | Each chunk flat-maps independently |
| `ForEach` | Yes | Each chunk runs side effects independently |
| `Aggregate` | Yes | Each chunk reduces → merge partial results |
| `Any` | Yes | First goroutine to find match cancels others via context |
| `All` | Yes | First goroutine to find violation cancels others |
| `Count` | Yes | Sum per-chunk counts |
| `Sum` / `Min` / `Max` | Yes | Reduce per chunk → merge |
| `OrderBy` | Partially | Parallel sort per chunk → merge sort |
| `Take` / `Skip` | No | Position-dependent, falls back to sequential |
| `First` | Tricky | Parallel search then pick lowest-index match |
| `Distinct` | Tricky | Per-chunk sets → merge sets |

Non-parallelizable operations in a parallel chain fall back to sequential execution for those steps.

### Codegen: Parallel Where + Select

**Zinc:**
```zinc
let result = nums.AsParallel().Where(x => x > 5).Select(x => x * 2).ToList()
```

**Emitted Go:**
```go
_numWorkers := runtime.NumCPU()
_chunkSize := (len(nums) + _numWorkers - 1) / _numWorkers
_chunks := make([][]int, _numWorkers)
var _wg sync.WaitGroup
for _w := 0; _w < _numWorkers; _w++ {
    _wg.Add(1)
    go func(w int) {
        defer _wg.Done()
        _start := w * _chunkSize
        _end := _start + _chunkSize
        if _end > len(nums) { _end = len(nums) }
        var _chunk []int
        for _, _v0 := range nums[_start:_end] {
            if _v0 > 5 {                    // Where — fused
                _v1 := _v0 * 2              // Select — fused
                _chunk = append(_chunk, _v1)
            }
        }
        _chunks[w] = _chunk
    }(_w)
}
_wg.Wait()
// Merge in order
var result []int
for _, _c := range _chunks {
    result = append(result, _c...)
}
```

Each goroutine gets a fused loop — no intermediate allocations within a chunk.

### Codegen: Parallel Aggregate

**Zinc:**
```zinc
let sum = nums.AsParallel().Where(x => x > 5).Aggregate(0, (acc, x) => acc + x)
```

**Emitted Go:**
```go
_numWorkers := runtime.NumCPU()
_chunkSize := (len(nums) + _numWorkers - 1) / _numWorkers
_partials := make([]int, _numWorkers)
var _wg sync.WaitGroup
for _w := 0; _w < _numWorkers; _w++ {
    _wg.Add(1)
    go func(w int) {
        defer _wg.Done()
        _start := w * _chunkSize
        _end := _start + _chunkSize
        if _end > len(nums) { _end = len(nums) }
        _acc := 0                           // initial value per chunk
        for _, _v0 := range nums[_start:_end] {
            if _v0 > 5 {                    // Where — fused
                _acc = _acc + _v0           // Aggregate — fused
            }
        }
        _partials[w] = _acc
    }(_w)
}
_wg.Wait()
// Merge partial results
sum := 0
for _, _p := range _partials {
    sum = sum + _p
}
```

Note: parallel Aggregate assumes the operation is **associative** (addition, multiplication, min, max, concatenation). This is the same tradeoff PLINQ makes — the user's responsibility.

### Codegen: Parallel Any (early cancellation)

**Zinc:**
```zinc
let hasNeg = nums.AsParallel().Any(x => x < 0)
```

**Emitted Go:**
```go
_numWorkers := runtime.NumCPU()
_chunkSize := (len(nums) + _numWorkers - 1) / _numWorkers
_ctx, _cancel := context.WithCancel(context.Background())
defer _cancel()
var _found atomic.Bool
var _wg sync.WaitGroup
for _w := 0; _w < _numWorkers; _w++ {
    _wg.Add(1)
    go func(w int) {
        defer _wg.Done()
        _start := w * _chunkSize
        _end := _start + _chunkSize
        if _end > len(nums) { _end = len(nums) }
        for _, _v0 := range nums[_start:_end] {
            if _ctx.Err() != nil { return } // another goroutine found it
            if _v0 < 0 {
                _found.Store(true)
                _cancel()                   // cancel other goroutines
                return
            }
        }
    }(_w)
}
_wg.Wait()
hasNeg := _found.Load()
```

### Implementation Order for Parallelism

Parallelism is **v1.1** — build after sequential collection methods are solid.

1. **`AsParallel()` recognition** — parser/typechecker flag on ChainExpr
2. **Parallel Where + Select** — chunk splitting, goroutine-per-chunk, ordered merge
3. **Parallel Aggregate** — partitioned reduce with merge
4. **Parallel short-circuit** — `Any`/`All` with context cancellation
5. **Parallel ForEach** — simplest parallel terminal
6. **Sequential fallback** — `Take`/`Skip`/`First` in parallel chains fall back gracefully

### Why This Matters

Most languages require external libraries or complex runtime machinery for parallel collections (Java parallel streams, C# PLINQ's thread pool, Rust's rayon). Zinc emits **raw goroutines + fused loops** — no runtime framework, no thread pool overhead, just Go's scheduler doing what it does best. The generated code is transparent, debuggable, and as fast as hand-written concurrent Go.
