# Copyright 2026 victorybhg
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
Benchmark early-exit strategies for Python codegen.

Compares: Numba, itertools, np.argmax, np.searchsorted
Against Go baseline for First and Take patterns.

Python 3.12 / NumPy 2.4.3 / Numba 0.64.0
"""

import time
import itertools
import numpy as np
import numba

SIZES = [1_000, 10_000, 100_000, 1_000_000, 10_000_000]
WARMUP = 3
RUNS = 10

def bench(fn, sizes=SIZES):
    results = {}
    for n in sizes:
        data = list(range(1, n + 1))
        arr = np.arange(1, n + 1, dtype=np.int64)
        for _ in range(WARMUP):
            fn(data, arr, n)
        times = []
        for _ in range(RUNS):
            t0 = time.perf_counter_ns()
            fn(data, arr, n)
            t1 = time.perf_counter_ns()
            times.append(t1 - t0)
        results[n] = sorted(times)[len(times) // 2]
    return results

# ============================================================
# 1. First (short-circuit) — find first element > threshold
#    threshold = n - 10, so match is near the end
# ============================================================

# --- Numba (current) ---
@numba.jit(nopython=True)
def _first_numba(src, threshold):
    for x in src:
        if x > threshold:
            return x
    return -1

def first_numba(data, arr, n):
    return _first_numba(arr, n - 10)

# --- itertools ---
def first_itertools(data, arr, n):
    t = n - 10
    return next(filter(lambda x: x > t, data))

# --- itertools on array ---
def first_itertools_arr(data, arr, n):
    t = n - 10
    return next(filter(lambda x: x > t, arr))

# --- generator expression ---
def first_genexpr(data, arr, n):
    t = n - 10
    return next(x for x in data if x > t)

# --- np.argmax (finds first True in boolean array) ---
def first_argmax(data, arr, n):
    t = n - 10
    mask = arr > t
    idx = np.argmax(mask)
    return int(arr[idx])

# --- np.searchsorted (binary search — works when sorted) ---
def first_searchsorted(data, arr, n):
    t = n - 10
    idx = np.searchsorted(arr, t, side='right')
    return int(arr[idx])

# --- np.nonzero (all matching indices, take first) ---
def first_nonzero(data, arr, n):
    t = n - 10
    indices = np.nonzero(arr > t)[0]
    return int(arr[indices[0]])

# ============================================================
# 2. Take(10) — first 10 elements matching x > 5
# ============================================================

# --- Numba (current, two-pass with early exit) ---
@numba.jit(nopython=True)
def _take_numba(src):
    result = np.empty(10, dtype=np.int64)
    taken = 0
    for x in src:
        if taken >= 10:
            break
        if x > 5:
            result[taken] = x * 2
            taken += 1
    return result[:taken]

def take_numba(data, arr, n):
    return _take_numba(arr)

# --- itertools.islice + filter ---
def take_itertools(data, arr, n):
    return list(itertools.islice((x * 2 for x in filter(lambda x: x > 5, data)), 10))

# --- itertools on array ---
def take_itertools_arr(data, arr, n):
    return list(itertools.islice((x * 2 for x in filter(lambda x: x > 5, arr)), 10))

# --- generator + manual count ---
def take_genexpr(data, arr, n):
    result = []
    for x in data:
        if len(result) >= 10:
            break
        if x > 5:
            result.append(x * 2)
    return result

# --- numpy slice (no short-circuit) ---
def take_numpy(data, arr, n):
    return (arr[arr > 5] * 2)[:10]

# ============================================================
# 3. Any (short-circuit boolean) — any element > threshold
#    threshold = n - 1, match is at very end
# ============================================================

# --- Numba ---
@numba.jit(nopython=True)
def _any_numba(src, threshold):
    for x in src:
        if x > threshold:
            return True
    return False

def any_numba(data, arr, n):
    return _any_numba(arr, n - 1)

# --- builtin any() ---
def any_builtin(data, arr, n):
    t = n - 1
    return any(x > t for x in data)

# --- np.any ---
def any_numpy(data, arr, n):
    t = n - 1
    return bool(np.any(arr > t))

# --- itertools ---
def any_itertools(data, arr, n):
    t = n - 1
    try:
        next(filter(lambda x: x > t, data))
        return True
    except StopIteration:
        return False

# ============================================================
# Runner
# ============================================================

def fmt(ns):
    if ns < 1000: return f"{ns} ns"
    elif ns < 1_000_000: return f"{ns/1000:.1f} µs"
    elif ns < 1_000_000_000: return f"{ns/1_000_000:.1f} ms"
    else: return f"{ns/1_000_000_000:.2f} s"

GO = {
    "First":    {1_000: 425, 10_000: 3698, 100_000: 39066, 1_000_000: 399668, 10_000_000: 5294944},
    "Take(10)": {1_000: 242, 10_000: 253, 100_000: 265, 1_000_000: 246, 10_000_000: 179},
    "Any":      {1_000: 400, 10_000: 3500, 100_000: 38000, 1_000_000: 380000, 10_000_000: 4800000},
}

def run_group(name, strategies, go_key):
    # JIT warmup for Numba strategies
    dummy_data = list(range(1, 101))
    dummy_arr = np.arange(1, 101, dtype=np.int64)
    for sname, fn in strategies:
        for _ in range(5):
            fn(dummy_data, dummy_arr, 100)

    results = {}
    for sname, fn in strategies:
        print(f"  Benchmarking {sname}...", flush=True)
        results[sname] = bench(fn)

    # Print table
    print(f"\n{'='*120}")
    print(f"  {name}")
    print(f"{'='*120}")

    header_parts = [f"{'N':>12}", f"{'Go':>12}"]
    for sname, _ in strategies:
        header_parts.append(f"{sname:>14}")
    header_parts.append(f"{'Winner':>16}")
    print(" | ".join(header_parts))
    print("-" * 120)

    for size in SIZES:
        g = GO[go_key][size]
        vals = {sname: results[sname][size] for sname, _ in strategies}
        all_vals = {"Go": g, **vals}
        winner_name = min(all_vals, key=all_vals.get)

        parts = [f"{size:>12,}", f"{fmt(g):>12}"]
        for sname, _ in strategies:
            parts.append(f"{fmt(vals[sname]):>14}")
        parts.append(f"{winner_name:>10} {fmt(all_vals[winner_name])}")
        print(" | ".join(parts))

    # Summary at 1M
    print(f"\n  Speedup vs Go at N=1,000,000:")
    g = GO[go_key][1_000_000]
    for sname, _ in strategies:
        v = results[sname][1_000_000]
        r = v / g
        if r >= 1:
            print(f"    {sname:<20} {r:.1f}x slower")
        else:
            print(f"    {sname:<20} {1/r:.1f}x FASTER")
    print()


if __name__ == "__main__":
    import sys
    print(f"Python {sys.version.split()[0]} / NumPy {np.__version__} / Numba {numba.__version__}")
    print()

    print("=== FIRST (find first element > threshold, match near end) ===")
    run_group("First", [
        ("Numba",        first_numba),
        ("itertools",    first_itertools),
        ("iter+arr",     first_itertools_arr),
        ("genexpr",      first_genexpr),
        ("argmax",       first_argmax),
        ("searchsorted", first_searchsorted),
        ("nonzero",      first_nonzero),
    ], "First")

    print("\n=== TAKE(10) (first 10 matching elements with transform) ===")
    run_group("Take(10)", [
        ("Numba",      take_numba),
        ("itertools",  take_itertools),
        ("iter+arr",   take_itertools_arr),
        ("genexpr",    take_genexpr),
        ("numpy",      take_numpy),
    ], "Take(10)")

    print("\n=== ANY (short-circuit boolean, match at very end) ===")
    run_group("Any", [
        ("Numba",      any_numba),
        ("builtin",    any_builtin),
        ("numpy",      any_numpy),
        ("itertools",  any_itertools),
    ], "Any")

    print("""
============================================================
  ANALYSIS
============================================================
Notes:
  - 'itertools' uses filter() + next()/islice() — C-implemented, lazy
  - 'genexpr' uses Python generator expressions — lazy but interpreted
  - 'argmax' uses np.argmax on boolean mask — vectorized but no short-circuit
  - 'searchsorted' uses binary search — O(log n) but only works on sorted data
  - 'nonzero' uses np.nonzero — finds all matches (no short-circuit)
""")
