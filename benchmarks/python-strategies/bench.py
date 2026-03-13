"""
Benchmark: Zinc collection method codegen strategies.

Compares three Python strategies for the same Zinc collection chains:
  1. List comprehensions (pure Python)
  2. NumPy vectorized operations
  3. Numba JIT-compiled loops

Each benchmark runs the same logical operation at multiple data sizes.
Python 3.12 / NumPy 2.4.3 / Numba 0.64.0

Generated from Zinc collection method chains:
  nums.Where(x => x > 5).Select(x => x * 2).ToList()
  nums.Where(x => x > threshold).First()
  nums.Aggregate(0, (acc, x) => acc + x)
  nums.Where(x => x > 5).Select(x => x * 2).Take(10).ToList()
"""

import time
import functools
import numpy as np
import numba

# === Sizes ===
SIZES = [1_000, 10_000, 100_000, 1_000_000, 10_000_000]
WARMUP = 3
RUNS = 10

def bench(name, fn, sizes=SIZES):
    """Run a benchmark function at multiple sizes and report results."""
    results = {}
    for n in sizes:
        data = list(range(1, n + 1))
        arr = np.arange(1, n + 1, dtype=np.int64)

        # Warmup
        for _ in range(WARMUP):
            fn(data, arr, n)

        # Timed runs
        times = []
        for _ in range(RUNS):
            t0 = time.perf_counter_ns()
            result = fn(data, arr, n)
            t1 = time.perf_counter_ns()
            times.append(t1 - t0)

        median_ns = sorted(times)[len(times) // 2]
        results[n] = median_ns
    return results


# =============================================================================
# Benchmark 1: Where + Select chain
# Zinc: nums.Where(x => x > n/2).Select(x => x * 2).ToList()
# =============================================================================

def where_select_comprehension(data, arr, n):
    threshold = n // 2
    return list([(x * 2) for x in [x for x in data if (x > threshold)]])

def where_select_numpy(data, arr, n):
    threshold = n // 2
    _arr = arr[arr > threshold]
    _arr = _arr * 2
    return _arr.tolist()

@numba.jit(nopython=True)
def _where_select_numba_jit(src, threshold):
    result = numba.typed.List.empty_list(numba.int64)
    for x in src:
        if x > threshold:
            result.append(x * 2)
    return result

def where_select_numba(data, arr, n):
    threshold = n // 2
    return list(_where_select_numba_jit(arr, threshold))


# =============================================================================
# Benchmark 2: Where + First (short-circuit)
# Zinc: nums.Where(x => x > threshold).First()
# =============================================================================

def first_comprehension(data, arr, n):
    threshold = n - 10  # near the end to test short-circuit
    return next(x for x in data if x > threshold)

def first_numpy(data, arr, n):
    threshold = n - 10
    _arr = arr[arr > threshold]
    return int(_arr[0])

@numba.jit(nopython=True)
def _first_numba_jit(src, threshold):
    for x in src:
        if x > threshold:
            return x
    return -1

def first_numba(data, arr, n):
    threshold = n - 10
    return _first_numba_jit(arr, threshold)


# =============================================================================
# Benchmark 3: Aggregate (sum)
# Zinc: nums.Aggregate(0, (acc, x) => acc + x)
# =============================================================================

def aggregate_comprehension(data, arr, n):
    return functools.reduce(lambda acc, x: acc + x, data, 0)

def aggregate_numpy(data, arr, n):
    return int(np.sum(arr))

@numba.jit(nopython=True)
def _aggregate_numba_jit(src):
    acc = 0
    for x in src:
        acc = acc + x
    return acc

def aggregate_numba(data, arr, n):
    return _aggregate_numba_jit(arr)


# =============================================================================
# Benchmark 4: Where + Select + Take (early termination)
# Zinc: nums.Where(x => x > 5).Select(x => x * 2).Take(10).ToList()
# =============================================================================

def take_comprehension(data, arr, n):
    return list([(x * 2) for x in [x for x in data if x > 5]][:10])

def take_numpy(data, arr, n):
    _arr = arr[arr > 5]
    _arr = _arr * 2
    _arr = _arr[:10]
    return _arr.tolist()

@numba.jit(nopython=True)
def _take_numba_jit(src):
    result = numba.typed.List.empty_list(numba.int64)
    taken = 0
    for x in src:
        if taken >= 10:
            break
        if x > 5:
            result.append(x * 2)
            taken += 1
    return result

def take_numba(data, arr, n):
    return list(_take_numba_jit(arr))


# =============================================================================
# Benchmark 5: Any (short-circuit boolean)
# Zinc: nums.Any(x => x > threshold)
# =============================================================================

def any_comprehension(data, arr, n):
    threshold = n - 1  # second-to-last element
    return any(x > threshold for x in data)

def any_numpy(data, arr, n):
    threshold = n - 1
    return bool(np.any(arr > threshold))

@numba.jit(nopython=True)
def _any_numba_jit(src, threshold):
    for x in src:
        if x > threshold:
            return True
    return False

def any_numba(data, arr, n):
    threshold = n - 1
    return _any_numba_jit(arr, threshold)


# =============================================================================
# Benchmark 6: Complex chain — Where + Select + Aggregate
# Zinc: nums.Where(x => x % 2 == 0).Select(x => x * x).Aggregate(0, (a, b) => a + b)
# =============================================================================

def complex_comprehension(data, arr, n):
    filtered = [x for x in data if x % 2 == 0]
    mapped = [x * x for x in filtered]
    return functools.reduce(lambda a, b: a + b, mapped, 0)

def complex_numpy(data, arr, n):
    _arr = arr[np.mod(arr, 2) == 0]
    _arr = _arr * _arr
    return int(np.sum(_arr))

@numba.jit(nopython=True)
def _complex_numba_jit(src):
    acc = 0
    for x in src:
        if x % 2 == 0:
            acc = acc + x * x
    return acc

def complex_numba(data, arr, n):
    return _complex_numba_jit(arr)


# =============================================================================
# Runner
# =============================================================================

def format_time(ns):
    if ns < 1_000:
        return f"{ns} ns"
    elif ns < 1_000_000:
        return f"{ns / 1_000:.1f} µs"
    elif ns < 1_000_000_000:
        return f"{ns / 1_000_000:.1f} ms"
    else:
        return f"{ns / 1_000_000_000:.2f} s"

def run_benchmark(name, comprehension_fn, numpy_fn, numba_fn):
    print(f"\n{'='*70}")
    print(f"  {name}")
    print(f"{'='*70}")

    # JIT warmup for Numba (compile on first call)
    dummy_data = list(range(1, 101))
    dummy_arr = np.arange(1, 101, dtype=np.int64)
    for _ in range(3):
        numba_fn(dummy_data, dummy_arr, 100)

    r_comp = bench(f"{name}/comprehension", comprehension_fn)
    r_np = bench(f"{name}/numpy", numpy_fn)
    r_nb = bench(f"{name}/numba", numba_fn)

    # Print table
    header = f"{'Size':>12} | {'Comprehension':>15} | {'NumPy':>15} | {'Numba':>15} | {'Fastest':>12}"
    print(header)
    print("-" * len(header))
    for n in SIZES:
        c, p, b = r_comp[n], r_np[n], r_nb[n]
        fastest = min(c, p, b)
        winner = "Compreh" if fastest == c else ("NumPy" if fastest == p else "Numba")
        speedup_comp = c / fastest if fastest > 0 else 0
        print(f"{n:>12,} | {format_time(c):>15} | {format_time(p):>15} | {format_time(b):>15} | {winner:>7} ({speedup_comp:.1f}x)" if winner != "Compreh" else
              f"{n:>12,} | {format_time(c):>15} | {format_time(p):>15} | {format_time(b):>15} | {winner:>7}")


if __name__ == "__main__":
    import sys
    print(f"Python {sys.version}")
    print(f"NumPy  {np.__version__}")
    print(f"Numba  {numba.__version__}")

    run_benchmark("Where+Select chain", where_select_comprehension, where_select_numpy, where_select_numba)
    run_benchmark("Where+First (short-circuit)", first_comprehension, first_numpy, first_numba)
    run_benchmark("Aggregate (sum)", aggregate_comprehension, aggregate_numpy, aggregate_numba)
    run_benchmark("Where+Select+Take(10)", take_comprehension, take_numpy, take_numba)
    run_benchmark("Any (short-circuit)", any_comprehension, any_numpy, any_numba)
    run_benchmark("Where+Select+Aggregate", complex_comprehension, complex_numpy, complex_numba)

    print(f"\n{'='*70}")
    print("  Summary")
    print(f"{'='*70}")
    print("""
Comprehension: Pure Python list comprehensions. No dependencies.
               Good baseline, familiar idiom, lazy-ish via generators.

NumPy:         Vectorized C-level array operations. No Python loops.
               Best for bulk numeric transforms (Where+Select, Aggregate).
               Weakness: can't short-circuit (processes entire array).

Numba:         JIT-compiles Python loops to machine code via LLVM.
               Best for complex lambdas and short-circuit operations.
               ~200ms first-call compile overhead (amortized).
               Combines loop fusion with native speed.
""")
