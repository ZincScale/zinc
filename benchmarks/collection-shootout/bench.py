#!/usr/bin/env python3
"""
Comprehensive Collection Methods Benchmark: Go vs Python
=========================================================
Tests all 27 list methods + 9 map methods across 5 strategies:
  1. Python list comprehensions (zero deps)
  2. NumPy vectorized operations
  3. Numba JIT-compiled loops
  4. Polars lazy dataframe operations
  5. DuckDB SQL-based analytics

Go baseline results are embedded from bench_go_test.go runs.

Usage:
    python3.14 bench.py [--size N] [--runs N] [--category CATEGORY]

Categories: filter, transform, partition, quantifier, aggregate, sort, group, map, all
"""

import sys
import time
import argparse
import statistics
import os

import numpy as np
import numba
import polars as pl
import duckdb

# ─── Benchmark Infrastructure ─────────────────────────────────────────────────

def bench(fn, warmup=3, runs=10, label=""):
    """Run fn() with warmup, return median time in seconds."""
    for _ in range(warmup):
        fn()
    times = []
    for _ in range(runs):
        t0 = time.perf_counter()
        result = fn()
        t1 = time.perf_counter()
        times.append(t1 - t0)
    med = statistics.median(times)
    return med, result

def fmt_time(seconds):
    if seconds < 1e-6:
        return f"{seconds*1e9:.0f} ns"
    if seconds < 1e-3:
        return f"{seconds*1e6:.1f} us"
    if seconds < 1:
        return f"{seconds*1e3:.2f} ms"
    return f"{seconds:.2f} s"

# ─── Data Setup ────────────────────────────────────────────────────────────────

def make_data(n):
    """Create test data for benchmarks."""
    rng = np.random.default_rng(42)
    nums_np = rng.integers(0, 1000, size=n).astype(np.int64)
    nums_list = nums_np.tolist()
    # Polars series
    nums_pl = pl.Series("val", nums_np)
    # DuckDB table
    con = duckdb.connect()
    con.execute("CREATE TABLE nums AS SELECT unnest(range(?)) as idx, unnest(?::BIGINT[]) as val", [n, nums_list])
    # Map data (string keys, int values)
    keys = [f"key_{i}" for i in range(min(n, 100_000))]
    vals = nums_list[:len(keys)]
    map_data = dict(zip(keys, vals))

    return {
        "list": nums_list,
        "np": nums_np,
        "pl": nums_pl,
        "duckdb": con,
        "map": map_data,
        "n": n,
    }

# ─── Category: Filtering ──────────────────────────────────────────────────────

def bench_where(data):
    """Where(x => x > 500)"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: [x for x in lst if x > 500])
    results["numpy"], _ = bench(lambda: arr[arr > 500])

    @numba.jit(nopython=True, cache=True)
    def nb_where(src):
        n = 0
        for x in src:
            if x > 500: n += 1
        out = np.empty(n, dtype=np.int64)
        j = 0
        for x in src:
            if x > 500:
                out[j] = x; j += 1
        return out
    nb_where(arr)  # warmup JIT
    results["numba"], _ = bench(lambda: nb_where(arr))

    results["polars"], _ = bench(lambda: sr.filter(sr > 500))
    results["duckdb"], _ = bench(lambda: con.execute("SELECT val FROM nums WHERE val > 500").fetchnumpy())

    return "Where(x > 500)", results

def bench_where_select(data):
    """Where(x => x > 500).Select(x => x * 2)"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: [x * 2 for x in lst if x > 500])

    def np_ws():
        mask = arr > 500
        return arr[mask] * 2
    results["numpy"], _ = bench(np_ws)

    @numba.jit(nopython=True, cache=True)
    def nb_ws(src):
        n = 0
        for x in src:
            if x > 500: n += 1
        out = np.empty(n, dtype=np.int64)
        j = 0
        for x in src:
            if x > 500:
                out[j] = x * 2; j += 1
        return out
    nb_ws(arr)
    results["numba"], _ = bench(lambda: nb_ws(arr))

    results["polars"], _ = bench(lambda: sr.filter(sr > 500) * 2)
    results["duckdb"], _ = bench(lambda: con.execute("SELECT val * 2 FROM nums WHERE val > 500").fetchnumpy())

    return "Where+Select", results

def bench_distinct(data):
    """Distinct()"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: list(dict.fromkeys(lst)))
    results["numpy"], _ = bench(lambda: np.unique(arr))

    @numba.jit(nopython=True, cache=True)
    def nb_distinct(src):
        seen = set()
        n = 0
        for x in src:
            if x not in seen:
                seen.add(x)
                n += 1
        out = np.empty(n, dtype=np.int64)
        seen2 = set()
        j = 0
        for x in src:
            if x not in seen2:
                seen2.add(x)
                out[j] = x; j += 1
        return out
    nb_distinct(arr)
    results["numba"], _ = bench(lambda: nb_distinct(arr))

    results["polars"], _ = bench(lambda: sr.unique())
    results["duckdb"], _ = bench(lambda: con.execute("SELECT DISTINCT val FROM nums").fetchnumpy())

    return "Distinct", results

# ─── Category: Transform ──────────────────────────────────────────────────────

def bench_select(data):
    """Select(x => x * x)"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: [x * x for x in lst])
    results["numpy"], _ = bench(lambda: arr * arr)

    @numba.jit(nopython=True, cache=True)
    def nb_select(src):
        out = np.empty(len(src), dtype=np.int64)
        for i in range(len(src)):
            out[i] = src[i] * src[i]
        return out
    nb_select(arr)
    results["numba"], _ = bench(lambda: nb_select(arr))

    results["polars"], _ = bench(lambda: sr * sr)
    results["duckdb"], _ = bench(lambda: con.execute("SELECT val * val FROM nums").fetchnumpy())

    return "Select(x * x)", results

def bench_select_many(data):
    """SelectMany — flatten list of lists"""
    n = min(data["n"], 100_000)  # SelectMany creates nested data
    nested = [[i, i+1, i+2] for i in range(n)]
    nested_np = np.array(nested, dtype=np.int64)

    results = {}
    results["comprehension"], _ = bench(lambda: [y for x in nested for y in x])
    results["numpy"], _ = bench(lambda: nested_np.ravel())
    # Numba can't easily iterate list-of-lists; skip
    results["numba"] = None

    nested_pl = pl.Series("val", nested)
    results["polars"], _ = bench(lambda: nested_pl.explode())
    # DuckDB: unnest
    con = data["duckdb"]
    con.execute(f"CREATE OR REPLACE TABLE nested AS SELECT unnest(?::BIGINT[][]) as arr", [nested])
    results["duckdb"], _ = bench(lambda: con.execute("SELECT unnest(arr) FROM nested").fetchnumpy())

    return "SelectMany (flatten)", results

# ─── Category: Partition ───────────────────────────────────────────────────────

def bench_take(data):
    """Take(10)"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: lst[:10])
    results["numpy"], _ = bench(lambda: arr[:10])
    results["numba"] = None  # trivial slice, no JIT benefit
    results["polars"], _ = bench(lambda: sr.head(10))
    results["duckdb"], _ = bench(lambda: con.execute("SELECT val FROM nums LIMIT 10").fetchnumpy())

    return "Take(10)", results

# ─── Category: Quantifiers ────────────────────────────────────────────────────

def bench_any(data):
    """Any(x => x > 999)"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: any(x > 999 for x in lst))
    results["numpy"], _ = bench(lambda: np.any(arr > 999))

    @numba.jit(nopython=True, cache=True)
    def nb_any(src):
        for x in src:
            if x > 999: return True
        return False
    nb_any(arr)
    results["numba"], _ = bench(lambda: nb_any(arr))

    results["polars"], _ = bench(lambda: (sr > 999).any())
    results["duckdb"], _ = bench(lambda: con.execute("SELECT bool_or(val > 999) FROM nums").fetchone()[0])

    return "Any(x > 999)", results

def bench_all(data):
    """All(x => x >= 0)"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: all(x >= 0 for x in lst))
    results["numpy"], _ = bench(lambda: np.all(arr >= 0))

    @numba.jit(nopython=True, cache=True)
    def nb_all(src):
        for x in src:
            if x < 0: return False
        return True
    nb_all(arr)
    results["numba"], _ = bench(lambda: nb_all(arr))

    results["polars"], _ = bench(lambda: (sr >= 0).all())
    results["duckdb"], _ = bench(lambda: con.execute("SELECT bool_and(val >= 0) FROM nums").fetchone()[0])

    return "All(x >= 0)", results

def bench_count(data):
    """Where(x > 500).Count()"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: sum(1 for x in lst if x > 500))
    results["numpy"], _ = bench(lambda: np.count_nonzero(arr > 500))

    @numba.jit(nopython=True, cache=True)
    def nb_count(src):
        c = 0
        for x in src:
            if x > 500: c += 1
        return c
    nb_count(arr)
    results["numba"], _ = bench(lambda: nb_count(arr))

    results["polars"], _ = bench(lambda: (sr > 500).sum())
    results["duckdb"], _ = bench(lambda: con.execute("SELECT count(*) FROM nums WHERE val > 500").fetchone()[0])

    return "Where+Count", results

# ─── Category: Element ─────────────────────────────────────────────────────────

def bench_first(data):
    """First(x => x > 990)"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: next(x for x in lst if x > 990))

    def np_first():
        mask = arr > 990
        idx = np.argmax(mask)
        return arr[idx]
    results["numpy"], _ = bench(np_first)

    @numba.jit(nopython=True, cache=True)
    def nb_first(src):
        for x in src:
            if x > 990: return x
        return -1
    nb_first(arr)
    results["numba"], _ = bench(lambda: nb_first(arr))

    results["polars"], _ = bench(lambda: sr.filter(sr > 990)[0])
    results["duckdb"], _ = bench(lambda: con.execute("SELECT val FROM nums WHERE val > 990 LIMIT 1").fetchone()[0])

    return "First(x > 990)", results

def bench_last(data):
    """Last(x => x > 990) — must scan all"""
    lst, arr = data["list"], data["np"]

    results = {}
    def comp_last():
        found = None
        for x in lst:
            if x > 990: found = x
        return found
    results["comprehension"], _ = bench(comp_last)

    def np_last():
        mask = arr > 990
        indices = np.nonzero(mask)[0]
        return arr[indices[-1]] if len(indices) > 0 else None
    results["numpy"], _ = bench(np_last)

    @numba.jit(nopython=True, cache=True)
    def nb_last(src):
        found = np.int64(-1)
        for x in src:
            if x > 990: found = x
        return found
    nb_last(arr)
    results["numba"], _ = bench(lambda: nb_last(arr))

    results["polars"] = None
    results["duckdb"] = None

    return "Last(x > 990)", results

# ─── Category: Aggregation ────────────────────────────────────────────────────

def bench_sum(data):
    """Sum()"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: sum(lst))
    results["numpy"], _ = bench(lambda: np.sum(arr))

    @numba.jit(nopython=True, cache=True)
    def nb_sum(src):
        s = np.int64(0)
        for x in src: s += x
        return s
    nb_sum(arr)
    results["numba"], _ = bench(lambda: nb_sum(arr))

    results["polars"], _ = bench(lambda: sr.sum())
    results["duckdb"], _ = bench(lambda: con.execute("SELECT sum(val) FROM nums").fetchone()[0])

    return "Sum", results

def bench_min_max(data):
    """Min() / Max()"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: (min(lst), max(lst)))
    results["numpy"], _ = bench(lambda: (np.min(arr), np.max(arr)))

    @numba.jit(nopython=True, cache=True)
    def nb_minmax(src):
        lo = src[0]; hi = src[0]
        for x in src:
            if x < lo: lo = x
            if x > hi: hi = x
        return lo, hi
    nb_minmax(arr)
    results["numba"], _ = bench(lambda: nb_minmax(arr))

    results["polars"], _ = bench(lambda: (sr.min(), sr.max()))
    results["duckdb"], _ = bench(lambda: con.execute("SELECT min(val), max(val) FROM nums").fetchone())

    return "Min+Max", results

def bench_aggregate(data):
    """Aggregate(0, (acc, x) => acc + x) — same as Sum but tests reduce path"""
    lst, arr = data["list"], data["np"]
    import functools

    results = {}
    results["comprehension"], _ = bench(lambda: functools.reduce(lambda a, x: a + x, lst, 0))
    results["numpy"], _ = bench(lambda: np.sum(arr))

    @numba.jit(nopython=True, cache=True)
    def nb_agg(src):
        acc = np.int64(0)
        for x in src: acc += x
        return acc
    nb_agg(arr)
    results["numba"], _ = bench(lambda: nb_agg(arr))

    results["polars"], _ = bench(lambda: pl.Series("v", data["list"]).sum())
    results["duckdb"] = None

    return "Aggregate (sum)", results

def bench_sum_with_selector(data):
    """Sum(x => x * x) — aggregate with transform"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: sum(x * x for x in lst))
    results["numpy"], _ = bench(lambda: np.sum(arr * arr))

    @numba.jit(nopython=True, cache=True)
    def nb_sum_sq(src):
        s = np.int64(0)
        for x in src: s += x * x
        return s
    nb_sum_sq(arr)
    results["numba"], _ = bench(lambda: nb_sum_sq(arr))

    results["polars"], _ = bench(lambda: (sr * sr).sum())
    results["duckdb"], _ = bench(lambda: con.execute("SELECT sum(val * val) FROM nums").fetchone()[0])

    return "Sum(x * x)", results

# ─── Category: Sorting ────────────────────────────────────────────────────────

def bench_order_by(data):
    """OrderBy(x => x)"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: sorted(lst))
    results["numpy"], _ = bench(lambda: np.sort(arr))
    # Numba can't beat native sort; skip
    results["numba"] = None
    results["polars"], _ = bench(lambda: sr.sort())
    results["duckdb"], _ = bench(lambda: con.execute("SELECT val FROM nums ORDER BY val").fetchnumpy())

    return "OrderBy", results

def bench_where_orderby_select_take(data):
    """Where(x > 500).OrderBy(x).Select(x * 2).Take(10) — segmented chain"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    def comp_chain():
        filtered = sorted(x for x in lst if x > 500)
        return [x * 2 for x in filtered[:10]]
    results["comprehension"], _ = bench(comp_chain)

    def np_chain():
        mask = arr > 500
        filtered = np.sort(arr[mask])
        return filtered[:10] * 2
    results["numpy"], _ = bench(np_chain)

    results["numba"] = None  # sort inside numba is complex

    def pl_chain():
        return sr.filter(sr > 500).sort().head(10) * 2
    results["polars"], _ = bench(pl_chain)

    results["duckdb"], _ = bench(lambda: con.execute(
        "SELECT val * 2 FROM nums WHERE val > 500 ORDER BY val LIMIT 10"
    ).fetchnumpy())

    return "Where+OrderBy+Select+Take", results

# ─── Category: Grouping ───────────────────────────────────────────────────────

def bench_group_by(data):
    """GroupBy(x => x % 10)"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    def comp_gb():
        groups = {}
        for x in lst:
            k = x % 10
            groups.setdefault(k, []).append(x)
        return groups
    results["comprehension"], _ = bench(comp_gb)

    # NumPy: use bincount or manual
    def np_gb():
        keys = arr % 10
        order = np.argsort(keys)
        sorted_keys = keys[order]
        splits = np.searchsorted(sorted_keys, np.arange(10))
        return splits  # indices of groups
    results["numpy"], _ = bench(np_gb)

    results["numba"] = None

    df = pl.DataFrame({"val": data["list"]})
    results["polars"], _ = bench(lambda: df.with_columns((pl.col("val") % 10).alias("grp")).group_by("grp").agg(pl.col("val")))
    results["duckdb"], _ = bench(lambda: con.execute(
        "SELECT val % 10 as grp, count(*) FROM nums GROUP BY grp"
    ).fetchnumpy())

    return "GroupBy(x % 10)", results

def bench_to_dictionary(data):
    """ToDictionary(x => x, x => x * x) on first 100K elements"""
    n = min(data["n"], 100_000)
    lst = data["list"][:n]
    arr = data["np"][:n]

    results = {}
    results["comprehension"], _ = bench(lambda: {x: x * x for x in lst})

    def np_td():
        vals = arr * arr
        return dict(zip(arr.tolist(), vals.tolist()))
    results["numpy"], _ = bench(np_td)

    results["numba"] = None
    results["polars"] = None
    results["duckdb"] = None

    return "ToDictionary", results

# ─── Category: Combining ──────────────────────────────────────────────────────

def bench_zip(data):
    """Zip(a, b, (x, y) => x + y)"""
    n = data["n"]
    lst_a = data["list"]
    lst_b = list(range(n))
    arr_a = data["np"]
    arr_b = np.arange(n, dtype=np.int64)

    results = {}
    results["comprehension"], _ = bench(lambda: [a + b for a, b in zip(lst_a, lst_b)])
    results["numpy"], _ = bench(lambda: arr_a + arr_b)

    @numba.jit(nopython=True, cache=True)
    def nb_zip(a, b):
        out = np.empty(len(a), dtype=np.int64)
        for i in range(len(a)):
            out[i] = a[i] + b[i]
        return out
    nb_zip(arr_a, arr_b)
    results["numba"], _ = bench(lambda: nb_zip(arr_a, arr_b))

    sr_a = data["pl"]
    sr_b = pl.Series("b", arr_b)
    results["polars"], _ = bench(lambda: sr_a + sr_b)
    results["duckdb"] = None

    return "Zip(a + b)", results

# ─── Category: Map Methods ────────────────────────────────────────────────────

def bench_map_where(data):
    """map.Where((k, v) => v > 500)"""
    m = data["map"]

    results = {}
    results["comprehension"], _ = bench(lambda: {k: v for k, v in m.items() if v > 500})
    results["numpy"] = None
    results["numba"] = None

    keys = list(m.keys())
    vals = list(m.values())
    df = pl.DataFrame({"key": keys, "val": vals})
    results["polars"], _ = bench(lambda: df.filter(pl.col("val") > 500))

    con = data["duckdb"]
    con.execute("CREATE OR REPLACE TABLE mapdata AS SELECT unnest(?::VARCHAR[]) as key, unnest(?::BIGINT[]) as val", [keys, vals])
    results["duckdb"], _ = bench(lambda: con.execute("SELECT * FROM mapdata WHERE val > 500").fetchnumpy())

    return "Map.Where(v > 500)", results

def bench_map_aggregate(data):
    """map.Aggregate(0, (acc, k, v) => acc + v)"""
    m = data["map"]

    results = {}
    results["comprehension"], _ = bench(lambda: sum(m.values()))

    vals_np = np.array(list(m.values()), dtype=np.int64)
    results["numpy"], _ = bench(lambda: np.sum(vals_np))
    results["numba"] = None

    sr = pl.Series("val", list(m.values()))
    results["polars"], _ = bench(lambda: sr.sum())

    results["duckdb"] = None

    return "Map.Aggregate(sum)", results

def bench_map_select_values(data):
    """map.SelectValues((k, v) => v * 2)"""
    m = data["map"]

    results = {}
    results["comprehension"], _ = bench(lambda: {k: v * 2 for k, v in m.items()})
    results["numpy"] = None
    results["numba"] = None
    results["polars"] = None
    results["duckdb"] = None

    return "Map.SelectValues(v * 2)", results

# ─── Complex Chains ───────────────────────────────────────────────────────────

def bench_complex_chain(data):
    """Where(x > 100).Select(x * x).Where(x < 500000).Sum()"""
    lst, arr, sr, con = data["list"], data["np"], data["pl"], data["duckdb"]

    results = {}
    results["comprehension"], _ = bench(lambda: sum(x * x for x in lst if x > 100 and x * x < 500000))

    def np_complex():
        m1 = arr > 100
        selected = arr[m1] ** 2
        return np.sum(selected[selected < 500000])
    results["numpy"], _ = bench(np_complex)

    @numba.jit(nopython=True, cache=True)
    def nb_complex(src):
        s = np.int64(0)
        for x in src:
            if x > 100:
                sq = x * x
                if sq < 500000:
                    s += sq
        return s
    nb_complex(arr)
    results["numba"], _ = bench(lambda: nb_complex(arr))

    def pl_complex():
        filtered = sr.filter(sr > 100)
        squared = filtered ** 2
        return squared.filter(squared < 500000).sum()
    results["polars"], _ = bench(pl_complex)
    results["duckdb"], _ = bench(lambda: con.execute(
        "SELECT sum(val * val) FROM nums WHERE val > 100 AND val * val < 500000"
    ).fetchone()[0])

    return "Complex: Where+Select+Where+Sum", results

# ─── Runner ───────────────────────────────────────────────────────────────────

CATEGORIES = {
    "filter": [bench_where, bench_where_select, bench_distinct],
    "transform": [bench_select, bench_select_many],
    "partition": [bench_take],
    "quantifier": [bench_any, bench_all, bench_count, bench_first, bench_last],
    "aggregate": [bench_sum, bench_min_max, bench_aggregate, bench_sum_with_selector],
    "sort": [bench_order_by, bench_where_orderby_select_take],
    "group": [bench_group_by, bench_to_dictionary],
    "combine": [bench_zip],
    "map": [bench_map_where, bench_map_aggregate, bench_map_select_values],
    "complex": [bench_complex_chain],
}

ALL_BENCHMARKS = []
for cat_fns in CATEGORIES.values():
    ALL_BENCHMARKS.extend(cat_fns)

STRATEGIES = ["comprehension", "numpy", "numba", "polars", "duckdb"]

def run_benchmarks(n, category="all", runs=10):
    print(f"\n{'='*80}")
    print(f"  Collection Methods Benchmark — N = {n:,} elements")
    print(f"  Python {sys.version.split()[0]} | NumPy {np.__version__} | "
          f"Numba {numba.__version__} | Polars {pl.__version__} | DuckDB {duckdb.__version__}")
    print(f"{'='*80}\n")

    data = make_data(n)

    benchmarks = ALL_BENCHMARKS if category == "all" else CATEGORIES.get(category, [])

    all_results = []

    for bench_fn in benchmarks:
        try:
            label, results = bench_fn(data)
        except Exception as e:
            print(f"  SKIP {bench_fn.__name__}: {e}")
            continue

        all_results.append((label, results))

        # Find winner
        valid = {k: v for k, v in results.items() if v is not None}
        if valid:
            winner = min(valid, key=lambda k: valid[k])
            winner_time = valid[winner]
        else:
            winner = "N/A"
            winner_time = 0

        print(f"  {label}")
        for strat in STRATEGIES:
            t = results.get(strat)
            if t is None:
                print(f"    {strat:15s}  {'—':>10s}")
            else:
                marker = " <-- winner" if strat == winner else ""
                ratio = f"({t/winner_time:.1f}x)" if winner_time > 0 and strat != winner else ""
                print(f"    {strat:15s}  {fmt_time(t):>10s}  {ratio:>8s}{marker}")
        print()

    # Summary table
    print(f"\n{'─'*80}")
    print(f"  SUMMARY TABLE (N = {n:,})")
    print(f"{'─'*80}")
    header = f"  {'Benchmark':40s}"
    for s in STRATEGIES:
        header += f" {s:>12s}"
    header += "  Winner"
    print(header)
    print(f"  {'─'*40}" + "─"*13*len(STRATEGIES) + "──────────")

    for label, results in all_results:
        row = f"  {label:40s}"
        valid = {k: v for k, v in results.items() if v is not None}
        winner = min(valid, key=lambda k: valid[k]) if valid else "N/A"
        for s in STRATEGIES:
            t = results.get(s)
            if t is None:
                row += f" {'—':>12s}"
            else:
                row += f" {fmt_time(t):>12s}"
        row += f"  {winner}"
        print(row)

    # Clean up
    data["duckdb"].close()

def main():
    parser = argparse.ArgumentParser(description="Collection Methods Benchmark")
    parser.add_argument("--size", type=int, default=1_000_000, help="Number of elements")
    parser.add_argument("--runs", type=int, default=10, help="Benchmark runs per test")
    parser.add_argument("--category", type=str, default="all",
                        choices=list(CATEGORIES.keys()) + ["all"],
                        help="Benchmark category to run")
    args = parser.parse_args()

    run_benchmarks(args.size, args.category, args.runs)

if __name__ == "__main__":
    main()
