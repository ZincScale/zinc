# Functional Collection Methods — Design Document

## Overview

LINQ-style chaining on collections, transpiled to fused Go loops. No intermediate allocations, no iterator wrappers, no `.stream()/.collect()` ceremony.

## Syntax

```zinc
result := nums.Where(x => x > 5).Select(x => x * 2).Take(10)
sum := nums.Where(x => x > 5).Aggregate(0, (acc, x) => acc + x)
hasAdmin := users.Any(u => u.isAdmin)
first := users.First(u => u.age > 18) or { return }
names := people.Select(p => p.name).ToList()
sorted := people.Where(p => p.active).OrderBy(p => p.age).Select(p => p.name)
grouped := people.GroupBy(p => p.city)
allTags := posts.SelectMany(p => p.tags)
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

Zinc already has full lambda support (`(Int x) => x * 2`), but collection methods need shorthand forms for ergonomic chaining. Arrow syntax stays as `=>` (consistent with C#/TypeScript).

### Shorthand Levels

```zinc
// Existing (verbose — works today but unusable for chaining)
list.Where((Int x) => x > 5).Select((Int x) => x * 2)

// Level 1: Type inference from context (infer param + return types)
list.Where((x) => x > 5).Select((x) => x * 2)

// Level 2: Single-param, no parens (C#/TypeScript style) — MINIMUM BAR
list.Where(x => x > 5).Select(x => x * 2)

// Level 3: Implicit `it` parameter (Kotlin style) — NICE-TO-HAVE
list.Where(it > 5).Select(it * 2)

// Multi-param always requires parens
list.Aggregate((acc, x) => acc + x)
```

### Implementation Notes

- Types inferred from the method's expected signature (e.g. `.Where` on `List<Int>` knows the lambda takes `Int` and returns `Bool`)
- Type flows left-to-right through chains: `.Select(x => x.toString())` on `List<Int>` infers `x: Int`, output becomes `List<String>`
- `it` is a reserved implicit parameter name for single-param lambdas only
- Expression-body only for v1 (single expression, no blocks in shorthand)

## Codegen Strategy: Loop Fusion

Since Zinc controls the full pipeline (parser → typechecker → codegen), we compile the entire chain into a **single fused loop** rather than using lazy iterators or intermediate slices.

### Example: Where + Select + Take

**Zinc:**
```zinc
result := nums.Where(x => x > 5).Select(x => x * 2).Take(10)
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
sum := nums.Where(x => x > 5).Select(x => x * 2).Aggregate(0, (acc, x) => acc + x)
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
hasAdmin := users.Any(u => u.isAdmin)
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
admin := users.First(u => u.isAdmin) or { return }
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
result := nums.Where(x => x > 5).OrderBy(x => x).Select(x => x * 2).Take(3)
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

## Map Collection Methods

Map methods follow the **Kotlin/Swift model**: type-preserving where possible. `Where` on a map returns a `Map`, not a sequence. This avoids the `.ToDictionary()` ceremony that makes C# map filtering verbose. Cross-language research confirms this is the most ergonomic approach (Kotlin, Swift, Python all return maps from filter).

### Design Decisions

1. **`Where` returns `Map<K,V>`** — type-preserving (Kotlin/Swift style), not a sequence requiring explicit `.ToDictionary()` (C# style)
2. **`SelectValues` and `SelectKeys`** — dedicated map-to-map transforms. The most-requested missing methods in C#/Rust. Named with LINQ `Select` prefix for consistency.
3. **Plain `Select` on a map returns `List<T>`** — universal across all languages. Free transform may not produce key-value pairs.
4. **All map lambdas receive `(k, v)`** — two-param lambda signals map context. Typechecker infers from source collection type.
5. **`Aggregate` on maps takes `(acc, k, v)`** — three-param lambda for map reduction.

### v1 Map Method Set

| Method | Lambda | Returns | Notes |
|--------|--------|---------|-------|
| `Where` | `(k, v) => Bool` | `Map<K,V>` | Type-preserving filter |
| `SelectValues` | `(k, v) => NewV` | `Map<K, NewV>` | Transform values, keep keys |
| `SelectKeys` | `(k, v) => NewK` | `Map<NewK, V>` | Transform keys, keep values |
| `Select` | `(k, v) => T` | `List<T>` | Free transform → list |
| `ForEach` | `(k, v) => void` | void | Side effects |
| `Any` | `(k, v) => Bool` | `Bool` | Short-circuit |
| `All` | `(k, v) => Bool` | `Bool` | Short-circuit |
| `Count` | `(k, v) => Bool` | `Int` | Count matching entries |
| `Aggregate` | `(acc, k, v) => T` | `T` | Reduction |

### Syntax Examples

```zinc
scores := {"Alice": 90, "Bob": 60, "Carol": 85}

// Where — filter entries, returns Map<String, Int>
passing := scores.Where((k, v) => v >= 80)
// {"Alice": 90, "Carol": 85}

// SelectValues — transform values, returns Map<String, Int>
doubled := scores.SelectValues((k, v) => v * 2)
// {"Alice": 180, "Bob": 120, "Carol": 170}

// SelectKeys — transform keys, returns Map<String, Int>
upper := scores.SelectKeys((k, v) => k.toUpper())
// {"ALICE": 90, "BOB": 60, "CAROL": 85}

// Select — free transform, returns List<String>
labels := scores.Select((k, v) => k + ": " + v.toString())
// ["Alice: 90", "Bob: 60", "Carol: 85"]

// ForEach
scores.ForEach((k, v) => print(k + " scored " + v.toString()))

// Any / All
hasHigh := scores.Any((k, v) => v > 85)
allPass := scores.All((k, v) => v >= 60)

// Count
highCount := scores.Count((k, v) => v > 80)

// Aggregate
total := scores.Aggregate(0, (acc, k, v) => acc + v)
```

### Chaining

Map methods that return maps can be chained. When a method returns `List<T>`, subsequent methods use single-param list lambdas:

```zinc
// Map → Map → Map (stays in map-land)
result := scores.Where((k, v) => v > 50).SelectValues((k, v) => v * 2)

// Map → Map → List (transitions to list-land)
names := scores.Where((k, v) => v > 80).Select((k, v) => k)

// Map → List → terminal
hasLongName := scores.Select((k, v) => k).Any(name => name.len() > 5)
```

### Codegen

All map methods use `for k, v := range` with loop fusion where possible.

**Where (single step):**
```zinc
passing := scores.Where((k, v) => v >= 80)
```
```go
passing := make(map[string]int)
for _k0, _v0 := range scores {
    if _v0 >= 80 {
        passing[_k0] = _v0
    }
}
```

**Where + SelectValues (fused):**
```zinc
result := scores.Where((k, v) => v > 50).SelectValues((k, v) => v * 2)
```
```go
result := make(map[string]int)
for _k0, _v0 := range scores {
    if _v0 > 50 {
        result[_k0] = _v0 * 2
    }
}
```

**Where + Select (map → list transition):**
```zinc
names := scores.Where((k, v) => v >= 80).Select((k, v) => k)
```
```go
var names []string
for _k0, _v0 := range scores {
    if _v0 >= 80 {
        names = append(names, _k0)
    }
}
```

**Where + Aggregate (fused, no allocation):**
```zinc
total := scores.Where((k, v) => v > 50).Aggregate(0, (acc, k, v) => acc + v)
```
```go
total := 0
for _k0, _v0 := range scores {
    if _v0 > 50 {
        total = total + _v0
    }
}
```

**Any (short-circuit):**
```zinc
hasHigh := scores.Any((k, v) => v > 85)
```
```go
hasHigh := false
for _, _v0 := range scores {
    if _v0 > 85 {
        hasHigh = true
        break
    }
}
```

### Future Map Methods (deferred)

- `OrderBy` on maps (requires materializing to sorted list of pairs)
- Set operations (union, intersect, subtract) on maps by key

## Implementation Order

Lambda expressions already exist (`(Int x) => x * 2`, block-body, failable). Shorthand and type inference are incremental additions.

1. **Lambda shorthand** — parser support for `x => expr` (no parens, no types) and `it` implicit param
2. **Single-step methods** — `Where`, `Select`, `ForEach` (no chaining, just emit a for loop)
3. **Chain recognition** — parser builds `ChainExpr` for multi-step chains
4. **Loop fusion codegen** — fuse Where+Select+terminal combinations
5. **Short-circuit terminals** — `Any`, `All`, `First`, `Take`
6. **Materialization segmentation** — handle `OrderBy`, `GroupBy` in chains
7. **Aggregate** and remaining terminals

Steps 1-4 deliver a working v1. Steps 5-7 round it out.

**Current status:** Steps 1-7 implemented. Both the v1 and v1.1 method sets are fully functional with loop fusion codegen. All 27 list methods and 9 map methods work with chaining. Lambda shorthand (`x => expr`, `(x, y) => expr`) works. Failable lambda support complete — errors auto-propagate from within collection chain lambdas via `emitExprLiftFailable`. Chain segmentation handles OrderBy/OrderByDescending materialization points. Map literal type inference emits concrete Go types (e.g. `map[string]int`). Parallel collections (`AsParallel()`) are planned for v2.

## v1 Method Set (all implemented)

**List methods (27):**
Where, Select, SelectMany, Aggregate, ForEach, Any, All, First, FirstOrDefault, Last, Count, Sum, Min, Max, Take, Skip, TakeWhile, SkipWhile, OrderBy, OrderByDescending, GroupBy, Distinct, Zip, ToList, ToDictionary

**Map methods (9):**
Where, SelectValues, SelectKeys, Select, ForEach, Any, All, Count, Aggregate

## Why Loop Fusion — Benchmark Results (Go 1.26)

Benchmarked three strategies on AMD EPYC (linux/amd64) at 100, 10K, and 1M element sizes. Source: `benchmarks/collection-strategies/bench_test.go`

### Filter + Map (`.Where().Select().ToList()`)

| Size | Fused | Iterator (range-over-func) | Naive (intermediate slices) |
|------|-------|---------------------------|----------------------------|
| 100 | 420 ns | 750 ns (1.8x slower) | 420 ns (1.0x) |
| 10K | 20 µs | 47 µs (2.3x) | 27 µs (1.3x) |
| 1M | 3.4 ms | 4.6 ms (1.4x) | 3.7 ms (1.1x) |

### Filter + First (short-circuit — most dramatic difference)

| Size | Fused | Iterator | Naive |
|------|-------|----------|-------|
| 100 | 17 ns / 0 alloc | 33 ns / 0 alloc | 318 ns / 960B |
| 10K | 1.5 µs / 0 alloc | 3.0 µs / 0 alloc | 23 µs / 128KB |
| 1M | 153 µs / 0 alloc | 299 µs / 0 alloc | **5.1 ms / 21MB** |

### Filter + Map + Reduce (no output allocation needed)

| Size | Fused | Iterator | Naive |
|------|-------|----------|-------|
| 100 | **65 ns / 0 alloc** | 493 ns / 56B (7.5x) | 497 ns / 1.4KB |
| 10K | **7.3 µs / 0 alloc** | 42 µs (5.7x) | 41 µs / 169KB |
| 1M | **640 µs / 0 alloc** | 4.1 ms (6.4x) | 5.6 ms / 25MB |

### Filter + Take(10) (early termination)

| Size | Fused | Iterator | Naive |
|------|-------|----------|-------|
| 100 | 200 ns | 415 ns (2x) | 257 ns |
| 10K | 210 ns | 415 ns (2x) | 16 µs (76x) |
| 1M | **191 ns** | 390 ns (2x) | **2.1 ms (11,000x!)** |

### Conclusions

1. **Fused wins everywhere** — 2-7x faster than iterators, especially on reduce/aggregate chains
2. **Go 1.23+ range-over-func iterators do NOT inline like Rust** — consistent 2x overhead from function call dispatch per element. Closer to Java streams than Rust zero-cost iterators
3. **Naive is catastrophic for short-circuit + early termination** — Filter+Take on 1M: fused 191ns vs naive 2.1ms (11,000x). Materializes entire filtered list before taking 10
4. **Naive is OK for simple chains at small sizes** — Filter+Map competitive with fused at 100 elements
5. **Iterator allocations are minimal** — but per-element function call overhead is the real cost

**Decision: Loop fusion confirmed.** Gives us the results of lazy evaluation (single pass, no intermediates, early exit) without the mechanism (iterator wrappers). Generated Go is simple, readable, and what a human would write.

## Why Not Intermediate Slices

Eager evaluation (each step creates a new slice) is simplest to implement but wasteful. A chain like `list.Where(...).Select(...).Take(10)` would filter the *entire* list, map the *entire* filtered list, then take 10. Loop fusion does it in one pass, stopping at 10. Benchmarks show 11,000x difference at scale.

## Parallel Collections via `AsParallel()` (PLINQ-style)

Goroutines are cheap (~2KB stack), making Go uniquely suited for parallel collection processing. Zinc leverages this with an explicit `AsParallel()` opt-in, combining **loop fusion + parallelism** — each goroutine runs a fused loop over its chunk.

### Syntax

```zinc
// Sequential (default) — fused single loop
result := nums.Where(x => x > 5).Select(x => x * 2).ToList()

// Parallel — fused loop per chunk, goroutine per chunk
result := nums.AsParallel().Where(x => x > 5).Select(x => heavyCompute(x)).ToList()

// Parallel aggregate
sum := nums.AsParallel().Where(x => x > 5).Aggregate(0, (acc, x) => acc + x)

// Parallel short-circuit
hasNeg := nums.AsParallel().Any(x => x < 0)
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
result := nums.AsParallel().Where(x => x > 5).Select(x => x * 2).ToList()
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
sum := nums.AsParallel().Where(x => x > 5).Aggregate(0, (acc, x) => acc + x)
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
hasNeg := nums.AsParallel().Any(x => x < 0)
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
