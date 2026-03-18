# Zinc — Collections Guide

## Overview

Zinc provides two ways to work with collections:

1. **Collection methods** — `.filter()`, `.map()`, `.sum()` etc. on any list
2. **Comprehensions** — `[x for x in items if cond]` inline expressions

The transpiler automatically picks the best backend:

| Your data | Backend | How |
|---|---|---|
| Any list, single method | Inline comprehension | Zero overhead |
| Any list, chained methods (2+) | `_ZincCollection` runtime | Method chaining |
| `list[dict]` + Polars installed | `_ZincPolarsCollection` | Columnar engine |
| `list[int/float]` + NumPy installed | `_ZincNumpyCollection` | Vectorized ops |

You write the same code — the runtime picks the fastest path. Polars and NumPy are auto-installed on first use if needed.

---

## Collection Methods

### filter — keep items matching a condition

```zinc
var numbers = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
var evens = numbers.filter(x -> x % 2 == 0)
// → [2, 4, 6, 8, 10]

var orders = [{"status": "active", "amount": 100}, {"status": "cancelled", "amount": 50}]
var active = orders.filter(o -> o["status"] == "active")
// → [{"status": "active", "amount": 100}]
```

### map — transform each item

```zinc
var doubled = numbers.map(x -> x * 2)
// → [2, 4, 6, 8, 10, 12, 14, 16, 18, 20]

var names = orders.map(o -> o["customer"])
// → ["Alice", "Bob", "Charlie"]
```

### sum, min, max — aggregate

```zinc
var total = numbers.sum()       // → 55
var smallest = numbers.min()    // → 1
var biggest = numbers.max()     // → 10
```

### sort and sort_by — ordering

```zinc
var sorted_nums = numbers.sort()
// → [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]

var by_amount = orders.sort_by(o -> o["amount"])
// → sorted by amount ascending

var by_amount_desc = orders.sort_by(o -> o["amount"], reverse=true)
// → sorted by amount descending
```

### take and skip — slicing

```zinc
var first_3 = numbers.take(3)   // → [1, 2, 3]
var rest = numbers.skip(3)      // → [4, 5, 6, 7, 8, 9, 10]
```

### first — find first match

```zinc
var first_even = numbers.first(x -> x % 2 == 0)  // → 2
var first_item = numbers.first()                   // → 1
```

### any and all — boolean checks

```zinc
var has_big = numbers.any(x -> x > 5)    // → true
var all_pos = numbers.all(x -> x > 0)    // → true
```

### distinct — unique values

```zinc
var items = [1, 2, 2, 3, 3, 3]
var unique = items.distinct()  // → [1, 2, 3]
```

### flat_map — flatten nested results

```zinc
var nested = [[1, 2], [3, 4], [5, 6]]
var flat = nested.flat_map(x -> x)  // → [1, 2, 3, 4, 5, 6]
```

### group_by — group into dict

```zinc
var orders = [
    {"region": "east", "amount": 100},
    {"region": "west", "amount": 200},
    {"region": "east", "amount": 150}
]
var by_region = orders.group_by(o -> o["region"])
// → {"east": [...], "west": [...]}
```

### reduce — accumulate

```zinc
var product = numbers.reduce(1, (acc, x) -> acc * x)
// → 3628800 (10!)
```

### to_list and to_dict — convert

```zinc
var result = numbers.filter(x -> x > 5).to_list()
// → [6, 7, 8, 9, 10]
```

---

## Method Chaining

Chain 2+ methods for pipeline-style processing:

```zinc
var revenue = orders
    .filter(o -> o["status"] == "completed")
    .map(o -> o["amount"])
    .sum()

var top_customers = orders
    .filter(o -> o["amount"] > 100)
    .sort_by(o -> o["amount"], reverse=true)
    .take(5)
    .map(o -> o["customer"])
    .to_list()
```

**Single method** → inline comprehension (zero overhead):
```zinc
// This:
orders.filter(o -> o["status"] == "active")
// Generates:
// [o for o in orders if (o["status"] == "active")]
```

**Chained methods (2+)** → `_zinc_collect()` runtime with smart dispatch:
```zinc
// This:
orders.filter(o -> o["status"] == "active").map(o -> o["amount"]).sum()
// Generates:
// _zinc_collect(orders).filter(lambda o: ...).map(lambda o: ...).sum()
```

---

## Comprehensions

Python-style comprehensions work directly in Zinc:

```zinc
// List comprehension
var squares = [x ** 2 for x in range(10)]

// With filter
var evens = [x for x in numbers if x % 2 == 0]

// Dict comprehension
var lengths = {word: len(word) for word in ["hello", "world", "zinc"]}
```

### Auto Generator Promotion

The transpiler automatically converts comprehensions to generators inside `sum()`, `any()`, `all()`, `min()`, `max()`, `sorted()`:

```zinc
// You write:
var total = sum([x ** 2 for x in range(1000000)])

// Transpiler generates (generator, not list — saves memory):
// total = sum(x ** 2 for x in range(1000000))
```

---

## Smart Dispatch — Polars and NumPy

When you chain collection methods, the runtime auto-detects the best backend based on your data shape. Polars and NumPy are auto-installed on first use if not already present.

### How it works

```
Your code: orders.filter(o -> o["status"] == "active").map(o -> o["amount"]).sum()

Runtime checks data[0]:
  → dict? → _ZincPolarsCollection (if polars installed, auto-installs if not)
  → int/float? → _ZincNumpyCollection (if numpy installed, auto-installs if not)
  → other? → _ZincCollection (pure Python)
```

### When Polars kicks in

Polars is used for `list[dict]` — structured/tabular data:

```zinc
var sales = [
    {"region": "east", "product": "widget", "amount": 150.0},
    {"region": "west", "product": "gadget", "amount": 300.0},
    {"region": "east", "product": "widget", "amount": 75.0}
]

// Polars handles this — columnar engine, Rust-powered
var total = sales
    .filter(s -> s["status"] == "completed")
    .map(s -> s["amount"])
    .sum()
```

Best for: JSON records, CSV rows, API responses, database results — anything that looks like a table.

### When NumPy kicks in

NumPy is used for `list[int]` or `list[float]` — numeric data:

```zinc
var measurements = [1.5, 2.3, 4.1, 0.8, 3.7, 2.9, 5.2, 1.1]

// NumPy handles this — SIMD vectorized, C-powered
var avg = measurements.sum() / len(measurements)
var above_avg = measurements.filter(x -> x > avg).to_list()
```

Best for: sensor data, time series, coordinates, scientific computing.

### When pure Python is used

Everything else — or when Polars/NumPy can't be installed:

```zinc
var items = ["hello", "world", "zinc"]

// Pure Python comprehensions — no external deps
var upper = items.map(x -> x.upper())
```

### Using Polars directly

For complex operations beyond what collection methods offer, import Polars directly:

```zinc
import polars as pl

var df = pl.read_csv("sales.csv")
var result = df
    .filter(pl.col("status") == "completed")
    .group_by("region")
    .agg(pl.col("amount").sum())

print(result)
```

### Using NumPy directly

For vectorized math, matrix operations, or scientific computing:

```zinc
import numpy as np

var data = np.array([1.0, 2.0, 3.0, 4.0, 5.0])
var normalized = (data - np.mean(data)) / np.std(data)
print("normalized: {normalized}")

// Matrix operations
var matrix = np.array([[1, 2], [3, 4]])
var inverse = np.linalg.inv(matrix)
print("inverse: {inverse}")
```

---

## Parallel Collections

With free-threaded Python, collection processing can be parallelized:

```zinc
// Auto-parallelized .map() on 1000+ items
// (handled by _ZincCollection runtime when GIL is disabled)

// Explicit parallel processing
parallel for item in items {
    process(item)
}
```

---

## Quick Reference

| Method | Description | Example |
|---|---|---|
| `.filter(pred)` | Keep matching items | `items.filter(x -> x > 0)` |
| `.map(fn)` | Transform each item | `items.map(x -> x * 2)` |
| `.sum()` | Sum all items | `items.sum()` |
| `.min()` | Minimum value | `items.min()` |
| `.max()` | Maximum value | `items.max()` |
| `.sort()` | Sort ascending | `items.sort()` |
| `.sort_by(key)` | Sort by key function | `items.sort_by(x -> x.age)` |
| `.take(n)` | First n items | `items.take(5)` |
| `.skip(n)` | Skip first n items | `items.skip(5)` |
| `.first()` | First item | `items.first()` |
| `.first(pred)` | First matching item | `items.first(x -> x > 10)` |
| `.any(pred)` | Any item matches? | `items.any(x -> x > 0)` |
| `.all(pred)` | All items match? | `items.all(x -> x > 0)` |
| `.distinct()` | Unique items | `items.distinct()` |
| `.flat_map(fn)` | Map and flatten | `items.flat_map(x -> x.children)` |
| `.group_by(key)` | Group into dict | `items.group_by(x -> x.category)` |
| `.reduce(init, fn)` | Accumulate | `items.reduce(0, (a, x) -> a + x)` |
| `.to_list()` | Convert to list | `items.filter(...).to_list()` |
| `.to_dict()` | Convert to dict | `items.to_dict()` |
