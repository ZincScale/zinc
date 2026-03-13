# Python Backend Codegen Strategy — Benchmark Results & Recommendations

## Context

Zinc's Go backend uses loop fusion for collection methods (740 lines of codegen). This document evaluates a **Python backend** targeting performance-critical collection processing using NumPy and Numba, benchmarked against Go's fused loops.

The Python backend prototype is ~650 lines total (6x simpler than Go's 4020 lines) because Python's duck typing eliminates auto-generated interfaces, pointer tracking, getter/setter generation, and failable detection.

## Three Python Strategies

| Strategy | Mechanism | Dependencies | Strengths | Weaknesses |
|----------|-----------|-------------|-----------|------------|
| **Comprehension** | List comprehensions, `next()`, `functools.reduce` | None | Zero deps, simple codegen | Interpreted — 50-270x slower than Go |
| **NumPy** | Vectorized C-level array ops (`arr[mask]`, `np.sum`) | numpy | Bulk numeric transforms, SIMD | Can't short-circuit, processes entire array |
| **Numba** | JIT-compiles Python loops to LLVM machine code | numba, numpy | Native speed, loop fusion, short-circuit | ~200ms first-call compile overhead, ~600-800ns per-call overhead |

## Benchmark Environment

- Python 3.12.10 / NumPy 2.4.3 / Numba 0.64.0
- Go 1.26 (fused loops, compiled)
- AMD EPYC (linux/amd64)
- Median of 10 runs, 3 warmup iterations
- Source: `benchmarks/python-strategies/`

## Main Benchmark Results (N = 1,000,000)

| Benchmark | Go (fused) | Comprehension | NumPy | Numba | Winner |
|-----------|-----------|---------------|-------|-------|--------|
| **Where+Select** | 7.8 ms | 39.1 ms | 683 µs | **662 µs** | Numba (11.8x faster than Go) |
| **First** | **400 µs** | 11.3 ms | 431 µs | 625 µs | Go (short-circuit advantage) |
| **Aggregate** | 353 µs | 95.5 ms | **167 µs** | 284 µs | NumPy (2.1x faster than Go) |
| **Take(10)** | **246 ns** | 65.3 ms | 1.0 ms | 800 ns | Go (early exit, zero overhead) |
| **Complex chain** | **372 µs** | 97.9 ms | 9.4 ms | 464 µs | Go (1.2x, Numba competitive) |

### Scaling (N = 10,000,000)

| Benchmark | Go | Numba | NumPy | Ratio (best Python / Go) |
|-----------|-----|-------|-------|------------------------|
| Where+Select | 44.1 ms | **26.5 ms** | 27.9 ms | 0.60x (Numba faster) |
| First | 5.3 ms | **5.1 ms** | 6.9 ms | 0.96x (Numba matches) |
| Aggregate | **4.0 ms** | 5.0 ms | 4.0 ms | 1.0x (NumPy matches) |
| Take(10) | **179 ns** | 800 ns | 65.4 ms | 4.5x slower (Numba best) |
| Complex | **3.7 ms** | 5.0 ms | 149.8 ms | 1.4x slower (Numba closest) |

## Early-Exit Strategy Deep Dive

Early exit is where Go has the biggest advantage. Separate benchmarks tested alternative Python strategies for short-circuit operations.

### First (find first matching element, match near end of array)

| Strategy | 1M time | vs Go | Notes |
|----------|---------|-------|-------|
| **np.searchsorted** | **1.6 µs** | **251x faster** | O(log n) binary search — **only works on sorted data** |
| **np.argmax** | 218 µs | 1.8x faster | Vectorized scan for first True in boolean mask |
| **Numba** | 292 µs | 1.4x faster | JIT short-circuit loop |
| Go | 400 µs | baseline | |
| genexpr | 11.3 ms | 28x slower | Python generator, lazy but interpreted |
| itertools | 36.6 ms | 92x slower | filter() + next() — C-implemented but per-element Python↔C crossing kills it |

**Finding:** `np.argmax` is the best general-purpose First — scans the full array but in C with SIMD, beating Go's short-circuit loop. `np.searchsorted` is ideal when data is known to be sorted.

### Take(10) (first 10 matching elements with transform)

| Strategy | 1M time | vs Go | Notes |
|----------|---------|-------|-------|
| Go | **246 ns** | baseline | Zero-overhead break from loop |
| **genexpr** | 1.4 µs | 5.7x slower | Python generator — best Python option |
| **Numba** | 1.9 µs | 7.8x slower | JIT call overhead dominates (~600-800ns) |
| itertools | 3.2 µs | 13x slower | islice + filter — C overhead per element |
| numpy | 1.1 ms | 4503x slower | Processes entire array, no short-circuit |

**Finding:** Go is unbeatable for Take — the actual work is trivial (10 elements), so function call overhead dominates. Generator expressions are the best Python option, simpler than Numba and actually faster for this pattern.

### Any (short-circuit boolean, match at very end)

| Strategy | 1M time | vs Go | Notes |
|----------|---------|-------|-------|
| **np.any** | **187 µs** | **2.0x faster** | Vectorized SIMD scan |
| Go | 380 µs | baseline | |
| Numba | 594 µs | 1.6x slower | |
| builtin any() | 34.6 ms | 91x slower | |

**Finding:** `np.any()` beats everything including Go — SIMD vectorized boolean scan is faster than Go's scalar loop even without short-circuiting.

## Numba Codegen: Two-Pass `np.empty` Pattern

Early benchmarks showed Numba's `numba.typed.List` has catastrophic `tolist()` overhead (~826ms for 500K elements). The fix is a **two-pass pattern**:

```python
@numba.jit(nopython=True)
def _chain_0(_src):
    # Pass 1: count matching elements
    _n = 0
    for _x in _src:
        if _x > 5:
            _n += 1
    # Pass 2: allocate and fill
    _result = np.empty(_n, dtype=np.int64)
    _j = 0
    for _x in _src:
        if _x > 5:
            _result[_j] = _x * _x
            _j += 1
    return _result

result = _chain_0(np.array(nums)).tolist()
```

This reduced Where+Select from **869ms → 662µs** (1312x improvement) at 1M elements. The `np.empty` → `ndarray.tolist()` path is highly optimized in NumPy's C layer.

### Numba Pattern Variants

| Chain type | Pattern | Example |
|-----------|---------|---------|
| **List-producing** (ToList) | Two-pass: count → allocate → fill | Where+Select+ToList |
| **Take(n)** | Single-pass with fixed `np.empty(n)` | Where+Take(10) |
| **Scalar terminal** (Any/All/Count/Aggregate/First) | Single-pass accumulator | Where+Aggregate |

## Recommended Codegen Dispatch

Based on benchmarks, the optimal strategy for each operation:

| Operation | Strategy | Rationale |
|-----------|----------|-----------|
| **Where+Select+ToList** (bulk) | **Numba** | 11.8x faster than Go, two-pass np.empty |
| **Where+Select+Aggregate** (fused reduce) | **Numba** | Competitive with Go (1.1-1.4x), single-pass scalar |
| **Aggregate** (simple sum/min/max) | **NumPy** | `np.sum()`, `np.min()` — 2.1x faster than Go |
| **Any / All** | **NumPy** | `np.any()` / `np.all()` — 2.0x faster than Go, SIMD |
| **First** (general) | **NumPy** | `np.argmax(mask)` — 1.8x faster than Go |
| **First** (sorted data) | **NumPy** | `np.searchsorted()` — 251x faster than Go |
| **Count** (with predicate) | **NumPy** | `np.count_nonzero(mask)` — vectorized |
| **Take(n)** | **Generator** | Generator expression — lowest overhead (5.7x slower than Go, but best Python option) |
| **ForEach** | **Comprehension** | Simple for loop — no performance benefit from JIT for side effects |

### Fallback Strategy

When NumPy/Numba are unavailable (no dependencies), all operations fall back to **comprehensions** (50-270x slower than Go but zero-dependency).

## Codegen Complexity Comparison

| Metric | Go Backend | Python Backend |
|--------|-----------|----------------|
| Total codegen lines | ~4,020 | ~650 |
| Collection method codegen | ~740 | ~280 |
| Auto-generated interfaces | Yes (complex) | No (duck typing) |
| Pointer/value tracking | Yes | No |
| Failable detection | Yes (complex) | No (exceptions) |
| Getter/setter generation | Yes | No |
| Error handling codegen | `if err != nil` chains | `try/except` |
| **Complexity ratio** | **baseline** | **~6x simpler** |

## Key Insights

1. **Python + NumPy/Numba beats Go for bulk array processing.** Where+Select is 11.8x faster due to SIMD vectorization and LLVM JIT compilation. Go's advantage is in low-overhead scalar operations and early exit.

2. **Go wins at early exit.** Take(10) at 1M: Go 246ns vs best Python 1.4µs. When the actual work is trivial, Go's zero-overhead loop dominates.

3. **The optimal Python backend is a hybrid.** No single strategy wins everything. The codegen should dispatch to NumPy for simple vectorized ops, Numba for complex fused chains, and generator expressions for early-exit patterns.

4. **itertools is not competitive.** Despite being C-implemented, the per-element Python↔C boundary crossing makes itertools slower than pure Python generators for most patterns.

5. **Numba's typed.List is a trap.** The `numba.typed.List` → Python `list()` conversion is catastrophically slow. Always use `np.empty` + fill for list-producing chains.

6. **Numba has ~600-800ns call overhead.** This is irrelevant for bulk operations but makes Numba lose to simpler strategies for trivial early-exit patterns.

## Comprehensive Benchmark: 5 Python Strategies (March 2026)

After implementing all 27 list + 9 map collection methods in the Go backend, we ran a full shootout comparing Go loop fusion against 5 Python strategies across 26 benchmarks. See `benchmarks/collection-shootout/` for full code and `RESULTS.md` for detailed tables.

### Environment
- Python 3.14.3 (PGO) / NumPy 2.4.3 / Numba 0.64.0 / Polars 1.39.0 / DuckDB 1.5.0
- Go 1.26.1 (loop fusion — actual Zinc codegen output)

### New Strategies Tested

| Strategy | Mechanism | Best at |
|----------|-----------|---------|
| **Polars** | Lazy dataframes, Apache Arrow, Rust engine | Filter, quantifiers, aggregate, sort (10 wins) |
| **DuckDB** | SQL analytics engine | Complex multi-stage chains with ORDER BY + LIMIT (1 win) |

### Score (26 benchmarks)

| Winner | Count | Examples |
|--------|-------|----------|
| Go loop fusion | 5 | First (14ns), ToDictionary, Take, Map.SelectValues, SelectMany |
| Polars | 10 | Where (582us), Sum (138us), OrderBy (6ms), Any/All (167us), Map.Where |
| Numba | 5 | Select (319us), Sum(x²) (228us), Distinct, ComplexChain, First early-exit |
| NumPy | 3 | MinMax (280us), Aggregate (179us), GroupBy |
| DuckDB | 1 | Where+OrderBy+Select+Take (1.87ms) |
| Comprehension | 2 | ToDictionary, Map.SelectValues |

### Recommended Hybrid Strategy for Python Codegen

```
Short-circuit ops (First, Take):                 → Pure Python (framework overhead > computation)
Map/dict ops (ToDictionary, Map.SelectValues):   → Pure Python dict comprehension
Element-wise compute (Select, Sum with math):    → Numba JIT
Complex chains with ORDER BY:                    → Polars lazy frame (or DuckDB)
Everything else:                                 → Polars lazy frame
```

Minimum deps for v1: **Polars only** (covers 80% of use cases). Add Numba for math-heavy transforms. DuckDB optional.

## Decision

**Defer Python backend implementation.** The benchmarks prove the concept is viable — Python+Numba/Polars can match or beat Go for bulk numeric processing. However:

- Go remains the right default backend for services, APIs, and general-purpose code
- Python backend adds value primarily for ML/data pipeline workloads
- Collection methods are now fully implemented in the Go backend (27 list + 9 map methods with loop fusion)
- Python backend can be revisited when there's concrete demand for ML/data pipeline use cases

The benchmark code is preserved in:
- `benchmarks/python-strategies/` — original 3-strategy prototype
- `benchmarks/collection-shootout/` — comprehensive 5-strategy shootout (Go + Python)
- `internal/codegen_python/` — prototype Python codegen
