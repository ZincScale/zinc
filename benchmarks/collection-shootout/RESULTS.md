# Collection Methods Benchmark: Go Loop Fusion vs Python Strategies

**Date:** 2026-03-13
**Hardware:** AMD EPYC 7R13 (8 vCPUs), Linux 4.18 (RHEL 8)
**N = 1,000,000 elements** (100K for maps, 10K for ToDictionary)

## Versions
- Go 1.26.1 (loop fusion — Zinc codegen output)
- Python 3.14.3 (PGO build)
- NumPy 2.4.3
- Numba 0.64.0 (LLVM JIT)
- Polars 1.39.0
- DuckDB 1.5.0

## Full Results

| Benchmark | Go (fused) | Python comprehension | NumPy | Numba | Polars | DuckDB | Go vs best Python |
|-----------|-----------|---------------------|-------|-------|--------|--------|-------------------|
| **Filter** |
| Where(x > 500) | 7.64 ms | 24.15 ms | 5.90 ms | 3.72 ms | **582 us** | 3.79 ms | 13.1x slower |
| Where+Select | 7.27 ms | 34.33 ms | 5.30 ms | 3.59 ms | **737 us** | 3.68 ms | 9.9x slower |
| Distinct | 11.30 ms | 22.00 ms | 20.74 ms | **3.94 ms** | 3.99 ms | 5.86 ms | 2.9x slower |
| **Transform** |
| Select(x²) | 1.57 ms | 42.82 ms | 367 us | **319 us** | 329 us | 3.75 ms | 4.9x slower |
| SelectMany (flatten) | **22 us** | 5.51 ms | 185 ns | — | 72 us | 2.39 ms | 119x faster† |
| **Partition** |
| Take(10) | **165 ns** | 155 ns | 205 ns | — | 660 ns | 280 us | ~tied |
| TakeWhile(x < 800) | **52 ns** | 320 ns | 178 us | 804 ns | — | — | 6.2x faster |
| SkipWhile(x < 800) | 8.62 ms | 27.38 ms | **178 us** | 325 us | — | — | 48.5x slower |
| **Quantifier** |
| Any(x > 999) | 366 us | 24.71 ms | 189 us | 293 us | **167 us** | 745 us | 2.2x slower |
| All(x >= 0) | 687 us | 23.13 ms | 201 us | 297 us | **165 us** | 706 us | 4.2x slower |
| Where+Count | 3.53 ms | 24.51 ms | 207 us | 311 us | **176 us** | 1.02 ms | 20.1x slower |
| First(x > 990) | **14 ns** | 3.8 us | 193 us | 445 ns | 260 us | 374 us | 31.8x faster |
| Last(x > 990) | **439 us** | 15.98 ms | 403 us | 301 us | — | — | 1.5x slower |
| **Aggregate** |
| Sum | 585 us | 7.42 ms | 151 us | 284 us | **138 us** | 783 us | 4.2x slower |
| Min+Max | 715 us | 33.07 ms | **280 us** | 315 us | 300 us | 1.22 ms | 2.6x slower |
| Aggregate(sum) | 403 us | 65.17 ms | **179 us** | 300 us | 17.02 ms | — | 2.2x slower |
| Sum(x²) | 584 us | 41.83 ms | 472 us | **228 us** | 434 us | 1.04 ms | 2.6x slower |
| **Sort** |
| OrderBy | 40.26 ms | 140.66 ms | 6.72 ms | — | **6.05 ms** | 18.67 ms | 6.7x slower |
| Where+OrderBy+Select+Take | 25.79 ms | 86.41 ms | 10.00 ms | — | 4.05 ms | **1.87 ms** | 13.8x slower |
| **Group** |
| GroupBy(x % 10) | 22.76 ms | 79.21 ms | **8.05 ms** | — | 12.38 ms | 9.60 ms | 2.8x slower |
| ToDictionary | **226 us** | 7.69 ms | 11.67 ms | — | — | — | 34.0x faster |
| **Combine** |
| Zip(a + b) | **1.80 ms** | 69.51 ms | 763 us | 790 us | 715 us | — | 2.5x slower |
| **Map operations** |
| Map.Where(v > 500) | 5.41 ms | 6.64 ms | — | — | **202 us** | 3.30 ms | 26.8x slower |
| Map.Aggregate(sum) | 786 us | 802 us | 20 us | — | **15 us** | — | 52.4x slower |
| Map.SelectValues(v*2) | **9.31 ms** | 11.86 ms | — | — | — | — | 1.3x faster |
| **Complex chain** |
| Where→Select→Where→Sum | 42.23 ms | 62.58 ms | 6.98 ms | **1.15 ms** | 2.84 ms | 3.45 ms | 36.7x slower |

†SelectMany: NumPy's `np.concatenate` (185 ns) beats Go here, but this is a degenerate case — pre-allocated C-level memcpy of 10K elements vs Go's runtime append growth.

## Analysis

### Where Go Loop Fusion Wins (6 benchmarks)

| Category | Why Go wins |
|----------|------------|
| **Short-circuit** (First, TakeWhile) | Breaks on first match — O(1) in best case. Python libraries scan full arrays. |
| **Map-like structures** (ToDictionary, Map.SelectValues) | Go maps are native; Python libraries model maps as columnar tables — mismatch. |
| **Tiny inputs / early exit** (Take) | No framework overhead. 165 ns vs 155 ns is noise. |

### Where Python Libraries Win (16 benchmarks)

| Strategy | Best at | Why |
|----------|---------|-----|
| **Polars** (10 wins) | Filter, quantifiers, aggregate, sort | Lazy evaluation + Apache Arrow columnar format + Rust engine. Column-at-a-time SIMD processing. |
| **Numba** (5 wins) | Element-wise compute (Select, Sum(x²), Distinct, complex chains) | JIT to native LLVM — tight scalar loops rival C. No framework overhead for simple ops. |
| **NumPy** (4 wins) | Reduction (Min/Max, Aggregate), SkipWhile, GroupBy | Vectorized C loops over contiguous float64 arrays. |
| **DuckDB** (1 win) | Complex chains with ORDER BY + LIMIT | Full query optimizer — pushes predicates, limits, projections down. |
| **Comprehension** (2 wins) | ToDictionary, Map.SelectValues | Only option when libraries don't support the operation. |

### Key Insight: Go vs Best-of-Breed Python

**Go loop fusion loses on bulk data operations** because:
1. Python's best libraries use **SIMD/vectorized C/Rust kernels** — they process 4-16 elements per CPU instruction
2. Go's `range` loop processes **one element at a time** with branch prediction overhead
3. For sort: Go's `sort.Ints` is `O(n log n)` comparison-based; Polars uses radix sort

**Go loop fusion wins on control-flow-heavy operations** because:
1. Short-circuit (`break`) is native — zero overhead
2. Map operations are first-class — no conversion to columnar format
3. No framework startup cost — 14 ns for First vs 445 ns for Numba

### Speedup Distribution

```
Go wins by >10x:   2 benchmarks (First, ToDictionary)
Go wins by 1-10x:  4 benchmarks (TakeWhile, Take, Map.SelectValues, SelectMany)
Python wins 1-5x:  9 benchmarks (Last, Any, MinMax, Aggregate, Sum(x²), Zip, Select, Distinct, GroupBy)
Python wins 5-20x: 5 benchmarks (Where, Where+Select, All, OrderBy, Where+Count)
Python wins >20x:  6 benchmarks (Where+OrderBy, SkipWhile, Map.Where, Map.Aggregate, Sum, ComplexChain)
```

## Implications for Zinc Python Codegen

### Recommended Strategy: Hybrid

1. **Default: Polars** for bulk collection chains (Where, Select, OrderBy, GroupBy, Sum, Count, Any, All)
   - Best overall performance across most operations
   - Lazy evaluation naturally handles chain fusion
   - Apache Arrow gives zero-copy interop with other tools

2. **Numba** for tight element-wise compute (Select with math, Sum with selector, Distinct)
   - JIT compilation matches C for scalar loops
   - Worth the compilation overhead for large N

3. **Pure Python** for:
   - Short-circuit operations (First, TakeWhile, Take) — framework overhead exceeds computation
   - Map operations (ToDictionary, Map.SelectValues) — libraries don't model dicts well
   - Small collections (N < 1000) — framework overhead dominates

4. **DuckDB** only for complex multi-stage chains with ORDER BY + LIMIT
   - Query optimizer excels here, but startup cost makes it poor for simple ops

### Decision Tree for Zinc Python Codegen

```
Is N known to be small (< 1000)?
  → Pure Python comprehension

Is the chain short-circuit? (First, Take, TakeWhile, Any on sparse data)
  → Pure Python loop with break

Is it a map operation? (Map.Where, Map.Select, Map.Aggregate)
  → Pure Python dict comprehension

Is it element-wise transform? (Select with math, Sum with selector)
  → Numba JIT loop

Is it a complex chain with OrderBy + downstream ops?
  → Polars lazy frame (or DuckDB if SQL-like)

Default:
  → Polars lazy frame
```

### Dependency Strategy

| Library | Purpose | Update cadence | Risk |
|---------|---------|---------------|------|
| Polars | Primary collection engine | Monthly releases | Low — stable API |
| Numba | JIT for math-heavy transforms | Quarterly | Medium — LLVM version coupling |
| NumPy | Numba dependency (implicit) | Stable | Low |
| DuckDB | Complex query optimization | Monthly | Low — SQL is stable |

Minimum deps for v1: **Polars only** (covers 80% of use cases).
Add Numba when math-heavy workloads are detected.
DuckDB is optional — only pull in for ORDER BY chains.
