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
Benchmark v2: Fixed Numba strategy to use np.array output (not typed.List).

Python 3.12 / NumPy 2.4.3 / Numba 0.64.0
"""

import time
import functools
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
# 1. Where+Select
# ============================================================

def ws_comprehension(data, arr, n):
    t = n // 2
    return [(x * 2) for x in data if x > t]

def ws_numpy(data, arr, n):
    t = n // 2
    return (arr[arr > t] * 2)  # stay as np.array

@numba.jit(nopython=True)
def _ws_numba(src, threshold):
    count = 0
    for x in src:
        if x > threshold:
            count += 1
    result = np.empty(count, dtype=np.int64)
    j = 0
    for x in src:
        if x > threshold:
            result[j] = x * 2
            j += 1
    return result

def ws_numba(data, arr, n):
    return _ws_numba(arr, n // 2)

# ============================================================
# 2. First (short-circuit)
# ============================================================

def first_comprehension(data, arr, n):
    t = n - 10
    return next(x for x in data if x > t)

def first_numpy(data, arr, n):
    t = n - 10
    return int(arr[arr > t][0])

@numba.jit(nopython=True)
def _first_numba(src, threshold):
    for x in src:
        if x > threshold:
            return x
    return -1

def first_numba(data, arr, n):
    return _first_numba(arr, n - 10)

# ============================================================
# 3. Aggregate (sum)
# ============================================================

def agg_comprehension(data, arr, n):
    return functools.reduce(lambda a, x: a + x, data, 0)

def agg_numpy(data, arr, n):
    return int(np.sum(arr))

@numba.jit(nopython=True)
def _agg_numba(src):
    acc = np.int64(0)
    for x in src:
        acc += x
    return acc

def agg_numba(data, arr, n):
    return _agg_numba(arr)

# ============================================================
# 4. Take(10) — early exit
# ============================================================

def take_comprehension(data, arr, n):
    return [x * 2 for x in data if x > 5][:10]

def take_numpy(data, arr, n):
    return (arr[arr > 5] * 2)[:10]

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

# ============================================================
# 5. Complex: Where + Select + Aggregate (fused)
# ============================================================

def complex_comprehension(data, arr, n):
    return functools.reduce(lambda a, b: a + b, [x * x for x in data if x % 2 == 0], 0)

def complex_numpy(data, arr, n):
    _arr = arr[arr % 2 == 0]
    return int(np.sum(_arr * _arr))

@numba.jit(nopython=True)
def _complex_numba(src):
    acc = np.int64(0)
    for x in src:
        if x % 2 == 0:
            acc += x * x
    return acc

def complex_numba(data, arr, n):
    return _complex_numba(arr)


# ============================================================
# Runner
# ============================================================

def fmt(ns):
    if ns < 1000: return f"{ns} ns"
    elif ns < 1_000_000: return f"{ns/1000:.1f} µs"
    elif ns < 1_000_000_000: return f"{ns/1_000_000:.1f} ms"
    else: return f"{ns/1_000_000_000:.2f} s"

# Go results (ns/op from go test -bench)
GO = {
    "Where+Select": {1_000: 5273, 10_000: 49969, 100_000: 943795, 1_000_000: 7798675, 10_000_000: 44053823},
    "First":        {1_000: 425, 10_000: 3698, 100_000: 39066, 1_000_000: 399668, 10_000_000: 5294944},
    "Aggregate":    {1_000: 341, 10_000: 3689, 100_000: 40645, 1_000_000: 352750, 10_000_000: 3957854},
    "Take(10)":     {1_000: 242, 10_000: 253, 100_000: 265, 1_000_000: 246, 10_000_000: 179},
    "Complex":      {1_000: 357, 10_000: 4030, 100_000: 39443, 1_000_000: 372030, 10_000_000: 3683331},
}

def run_suite(name, comp_fn, np_fn, nb_fn):
    # JIT warmup
    dummy_arr = np.arange(1, 101, dtype=np.int64)
    for _ in range(5):
        nb_fn(list(range(1,101)), dummy_arr, 100)

    rc = bench(comp_fn)
    rn = bench(np_fn)
    rb = bench(nb_fn)
    return rc, rn, rb

if __name__ == "__main__":
    import sys
    print(f"Python {sys.version.split()[0]} / NumPy {np.__version__} / Numba {numba.__version__}")
    print()

    suites = [
        ("Where+Select", ws_comprehension, ws_numpy, ws_numba),
        ("First",        first_comprehension, first_numpy, first_numba),
        ("Aggregate",    agg_comprehension, agg_numpy, agg_numba),
        ("Take(10)",     take_comprehension, take_numpy, take_numba),
        ("Complex",      complex_comprehension, complex_numpy, complex_numba),
    ]

    all_results = {}
    for name, c, n, b in suites:
        print(f"Running {name}...", flush=True)
        all_results[name] = run_suite(name, c, n, b)

    # Print results
    for size in SIZES:
        print(f"\n{'='*95}")
        print(f"  N = {size:,}")
        print(f"{'='*95}")
        print(f"{'Benchmark':<18} | {'Go (fused)':>12} | {'Compreh.':>12} | {'NumPy':>12} | {'Numba':>12} | {'Fastest':>18}")
        print("-" * 95)

        for name in [s[0] for s in suites]:
            rc, rn, rb = all_results[name]
            g = GO[name][size]
            c = rc[size]
            p = rn[size]
            b = rb[size]
            fastest = min(g, c, p, b)
            winner = "Go" if fastest == g else ("Compreh" if fastest == c else ("NumPy" if fastest == p else "Numba"))
            print(f"{name:<18} | {fmt(g):>12} | {fmt(c):>12} | {fmt(p):>12} | {fmt(b):>12} | {winner:>8} {fmt(fastest)}")

    # Summary at 1M
    print(f"\n{'='*95}")
    print(f"  SUMMARY: Speedup vs Go (fused loops) at N=1,000,000")
    print(f"{'='*95}")
    print(f"{'Benchmark':<18} | {'Compreh/Go':>15} | {'NumPy/Go':>15} | {'Numba/Go':>15}")
    print("-" * 70)

    for name in [s[0] for s in suites]:
        rc, rn, rb = all_results[name]
        g = GO[name][1_000_000]
        c = rc[1_000_000]
        p = rn[1_000_000]
        b = rb[1_000_000]

        def vs(val, base):
            r = val / base
            if r >= 1: return f"{r:.1f}x slower"
            return f"{1/r:.1f}x FASTER"

        print(f"{name:<18} | {vs(c,g):>15} | {vs(p,g):>15} | {vs(b,g):>15}")
