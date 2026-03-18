# Design: Zinc v2 — Typed Python with Explicit Blocks

## The Problem with Python

Python is the **best language for getting things done fast** — massive ecosystem, readable syntax, no compile step. But it has real pain points:

1. **Whitespace sensitivity** — indentation errors are silent and deadly. Copy-paste across editors, mixed tabs/spaces, reformatting breaks semantics.
2. **Types are optional and unenforced** — mypy/pyright exist but nobody is forced to use them. Half the ecosystem is untyped. Runtime `TypeError` surprises.
3. **`self` everywhere** — `def __init__(self, name: str, age: int): self.name = name; self.age = age` is absurd boilerplate.
4. **Class ceremony** — dunder methods, decorators stacking, metaclasses for simple things.

## Zinc v2 Vision

**Get in, write code, ship it, get out.**

Zinc is for developers who want Python's ecosystem and speed of development but are tired of runtime surprises, whitespace bugs, and boilerplate. It transpiles to clean `.py` files — standard Python you can read, debug, and deploy with any tooling.

The transpiler is your co-pilot. It catches mistakes before they hit production, auto-generates the tedious parts (self, dunder methods, type conversions, parallel dispatch), and stays out of your way for everything else.

### Core principles

1. **Zero ceremony** — write a `.zn` file, run it. No project setup, no config, no boilerplate. Single-file scripts are first-class citizens.
2. **Prevent footguns** — enforced types, exhaustive match, null safety, unused variable warnings. The compiler catches what Python lets slip to runtime.
3. **The transpiler works for you** — auto-inject `self`, translate clean method names to dunders, dispatch collections to fast backends, parallelize where safe. You write the intent, Zinc handles the mechanics.
4. **It's just Python underneath** — full pip ecosystem, all Python libraries work. The output is readable `.py` you can hand to anyone.
5. **Explicit blocks** — `end` keyword eliminates whitespace confusion. Copy-paste safely, refactor without counting spaces. Natural for the sci/data crowd (Julia, Crystal, Ruby).
6. **Quick changes in prod** — change a `.zn` file, `zinc run`, done. No compile wait, no deploy pipeline for a one-line fix. But the type checker still has your back.

### What the transpiler does for you

| You write | Zinc handles |
|---|---|
| `fn str(): str` | Generates `def __str__(self)` |
| `items.filter(x -> x > 0)` | Dispatches to Polars/NumPy/comprehension based on data shape and available deps |
| Field access: `name` | Injects `self.name` |
| `data User ... end` | Full `@dataclass` with `__init__`, `__repr__`, `__eq__` |
| `var x = 5` | Infers type, enforces it everywhere `x` is used |
| `.map(x -> process(x))` on large collections | Auto-parallelizes with thread pool (free-threaded Python) |
| `match status ... end` | Warns if cases aren't exhaustive |
| Imports a GIL-dependent library | Warns and falls back gracefully |

### Non-goals

- Not a new runtime — we generate Python and run Python.
- Not a framework — no DI, no services layer, no annotations. Use FastAPI/Flask/Django directly.
- Not a language for language nerds — no monads, no macros, no metaprogramming. Just clean, fast, safe code.

---

## Syntax

### Block Delimiters — `end` Keyword

Zinc uses `end` to close blocks — like Julia, Crystal, Ruby, and Lua. No braces, no significant whitespace. Indentation is for readability only (enforced by `zinc fmt`, ignored by the compiler).

```zinc
if x > 10
    print("big")
else
    print("small")
end

fn process(items: list[dict]): list[dict]
    var result = items.filter(x -> x["status"] == "active")
    if len(result) > 100
        result = result.sort_by(x -> x["priority"]).take(100)
    end
    return result
end
```

Why `end` over braces:
- No shift key needed — pure alpha keys
- Natural for the sci/data audience (Julia, Crystal, Ruby, Matlab, Lua)
- Reads like pseudocode
- Less visual noise than `}` stacking
- Still explicit — no whitespace ambiguity

### Multi-line Statements

Long lines are a fact of life with data processing. Zinc handles line continuation naturally — **a statement continues on the next line if it's obviously incomplete:**

**Automatic continuation** — the line ends with an operator, comma, `and`, `or`, or open bracket:

```zinc
var result = orders
    .filter(x -> x.status == "active" and
                 x.amount > 1000)
    .sort_by(x -> x.created_at)
    .take(50)

var config = {
    "host": "localhost",
    "port": 8080,
    "debug": true,
    "tags": [
        "production",
        "us-east-1",
    ],
}

if user.is_active and
   user.role == "admin" and
   user.last_login > cutoff_date
    grant_access(user)
end
```

**Rules (same as Ruby/Julia):**

1. Line ends with `.` → continues (method chaining)
2. Line ends with binary operator (`+`, `-`, `*`, `and`, `or`, `==`, `>`, etc.) → continues
3. Line ends with `,` → continues (function args, collections)
4. Unmatched `(`, `[`, or `{` → continues until closed
5. Line ends with `\` → explicit continuation (escape hatch, rarely needed)

This means **no backslash hell** like Python for multi-line conditions, and **no parenthesizing tricks** to avoid `\`. The parser knows from context.

```zinc
// Python forces this:
//   if (user.is_active and
//       user.role == "admin"):
//
// Or this:
//   if user.is_active and \
//      user.role == "admin":
//
// Zinc — just write it:
if user.is_active and
   user.role == "admin"
    do_thing()
end
```

### Variables

```zinc
var name = "Alice"          // type inferred as str
var age: int = 30           // explicit type
var scores: list[int] = []  // explicit generic
```

Transpiles to:
```python
name: str = "Alice"
age: int = 30
scores: list[int] = []
```

### Functions

```zinc
fn greet(name: str): str
    return "Hello, {name}!"
end

// Single-expression shorthand — no end needed
fn double(x: int): int = x * 2
```

Transpiles to:
```python
def greet(name: str) -> str:
    return f"Hello, {name}!"

def double(x: int) -> int:
    return x * 2
```

### Script Mode (No Main Required)

```zinc
// script.zn — just write code
var name = input("What's your name? ")
print("Hello, {name}!")
```

Transpiles to:
```python
name: str = input("What's your name? ")
print(f"Hello, {name}!")
```

That's it. No main function, no class wrapper. Top-level code just runs.

### String Interpolation

```zinc
print("Hello, {name}! You are {age} years old.")
```

Transpiles to Python f-strings:
```python
print(f"Hello, {name}! You are {age} years old.")
```

### No Dunder Methods

Python's `__dunder__` methods are powerful but ugly and hard to remember. Zinc replaces them with clean, readable names:

| Zinc | Python dunder | Purpose |
|---|---|---|
| `fn init(...)` | `__init__` | Constructor |
| `fn str(): str` | `__str__` | String representation |
| `fn repr(): str` | `__repr__` | Debug representation |
| `fn eq(other: T): bool` | `__eq__` | Equality |
| `fn hash(): int` | `__hash__` | Hash value |
| `fn len(): int` | `__len__` | Length |
| `fn iter()` | `__iter__` | Iteration |
| `fn next()` | `__next__` | Next item |
| `fn contains(item: T): bool` | `__contains__` | `in` operator |
| `fn get(key: K): V` | `__getitem__` | Index/key access |
| `fn set(key: K, val: V)` | `__setitem__` | Index/key assignment |
| `fn del(key: K)` | `__delitem__` | Index/key deletion |
| `fn add(other: T): T` | `__add__` | `+` operator |
| `fn sub(other: T): T` | `__sub__` | `-` operator |
| `fn mul(other: T): T` | `__mul__` | `*` operator |
| `fn lt(other: T): bool` | `__lt__` | `<` comparison |
| `fn le(other: T): bool` | `__le__` | `<=` comparison |
| `fn enter()` | `__enter__` | Context manager enter |
| `fn exit(...)` | `__exit__` | Context manager exit |
| `fn call(...)` | `__call__` | Callable object |

Example:

```zinc
class Stack
    var items: list[int] = []

    fn push(item: int)
        items.append(item)
    end

    fn pop(): int
        return items.pop()
    end

    fn len(): int
        return len(items)
    end

    fn str(): str
        return "Stack({items})"
    end

    fn iter()
        return iter(items)
    end
end
```

Transpiles to:
```python
class Stack:
    def __init__(self):
        self.items: list[int] = []

    def push(self, item: int):
        self.items.append(item)

    def pop(self) -> int:
        return self.items.pop()

    def __len__(self) -> int:
        return len(self.items)

    def __str__(self) -> str:
        return f"Stack({self.items})"

    def __iter__(self):
        return iter(self.items)
```

The transpiler also auto-injects `self` — no need to write it in Zinc. Fields are accessed directly by name.

### Data Classes

```zinc
data User
    name: str
    email: str
    age: int = 0
end
```

Transpiles to:
```python
from dataclasses import dataclass

@dataclass
class User:
    name: str
    email: str
    age: int = 0
```

### Enums

```zinc
enum Color
    Red
    Green
    Blue
end
```

Transpiles to:
```python
from enum import Enum, auto

class Color(Enum):
    Red = auto()
    Green = auto()
    Blue = auto()
```

### Error Handling — Two Tracks

Zinc separates **expected failures** (validation, parsing, missing data) from **exceptional failures** (disk full, network down). This is a core design principle — no more try/catch for data validation.

**Track 1 — Results for expected failures:**

```zinc
fn parse_age(input: str): Result[int]
    if not input.isdigit()
        return Err("must be a number")
    end
    var age = int(input)
    if age < 0 or age > 150
        return Err("out of range: {age}")
    end
    return age  // just return it — transpiler wraps in Ok
end

// Handle error inline — no ceremony on the happy path
var age = parse_age(input) Err {
    print("bad age: {err}")
    return
}
print("Age: {age}")  // age is an int, just keep going

// Provide a default
var age = parse_age(input) Err { 0 }

// Skip bad records in a batch
for i, record in enumerate(records)
    var age = parse_age(record["age"]) Err {
        errors.append("record {i}: {err}")
        continue
    }
    users.append(User(record["name"], age))
end
```

**Track 2 — Exceptions for unexpected failures:**

```zinc
try
    var conn = db.connect(url)
catch err: ConnectionError
    print("Database down: {err}")
    exit(1)
end
```

**The litmus test:** If you'd put it in a loop processing 10,000 records, it should be a Result. If it would stop your entire program, it's an exception. The transpiler warns if you raise exceptions inside loops.

See `error-handling.md` for full details.

### Imports — Use Python's Ecosystem Directly

```zinc
import json
import os
from pathlib import Path
from requests import get as http_get
```

Transpiles directly — no transformation needed. This is critical: **Zinc doesn't wrap Python libraries.** You use them directly.

### Conditionals

```zinc
if x > 0
    print("positive")
else if x == 0
    print("zero")
else
    print("negative")
end
```

Go-style `else if` (two words) instead of Python's `elif` — reads like English, no special keyword to remember.

### Expression If (Ternary)

Condition-first inline if — no more reading Python ternaries backwards:

```zinc
var encoded = if value != none: str(value).encode("utf-8") else: b""
var label = if count == 1: "item" else: "items"
var access = if user.is_admin: "full" else if user.is_member: "read" else: "none"
```

Transpiles to Python's ternary:
```python
encoded = str(value).encode("utf-8") if value is not None else b""
label = "item" if count == 1 else "items"
```

The colon separates condition from value, reads left-to-right: "if this: that, else: other."

### Loops

```zinc
for item in items
    print(item)
end

for i in range(10)
    print(i)
end

while running
    process_next()
end
```

### Match

```zinc
match command
    case "start" -> start_server()
    case "stop" -> stop_server()
    case other -> print("Unknown: {other}")
end
```

Transpiles to:
```python
match command:
    case "start":
        start_server()
    case "stop":
        stop_server()
    case other:
        print(f"Unknown: {other}")
```

### Lambdas

```zinc
var doubled = items.map(x -> x * 2)
var evens = items.filter(x -> x % 2 == 0)
```

Transpiles to:
```python
doubled = list(map(lambda x: x * 2, items))
evens = list(filter(lambda x: x % 2 == 0, items))
```

Or using comprehensions (often more Pythonic):
```python
doubled = [x * 2 for x in items]
evens = [x for x in items if x % 2 == 0]
```

**Decision needed:** Do we prefer method chaining (`.map().filter()`) or keep Python's comprehension style? Comprehensions are more Pythonic but chaining is more readable for long pipelines. Could support both.

### List/Dict/Set Comprehensions

Keep Python's comprehension syntax — it's genuinely good:

```zinc
var squares = [x * x for x in range(10)]
var evens = [x for x in numbers if x % 2 == 0]
var word_lengths = {word: len(word) for word in words}
```

These pass through to Python unchanged (just add braces for blocks around them if needed).

---

## Collection Performance — Smart Dispatch

This is a key differentiator. Zinc v1 benchmarks proved that Python + NumPy/Numba/Polars can **match or beat compiled Go** for bulk data operations. Zinc v2 keeps this — the transpiler generates optimized collection code that raw Python developers would never write by hand.

### The real world: structured data, not just numbers

Real data pipelines process JSON records, Avro messages, NiFi flowfiles, CSV rows — not `list[int]`. Zinc's collection performance needs to handle **structured record processing** at scale:

```zinc
// Processing NiFi flowfiles, JSON records, Avro messages
data Order
    id: str
    customer: str
    amount: float
    status: str
    items: list[dict]
end

var orders = json.loads(read_file("orders.json"))

// Filter + transform + aggregate on structured data
var revenue = orders
    .filter(o -> o.status == "completed")
    .map(o -> o.amount)
    .sum()

var big_orders = orders
    .filter(o -> o.amount > 1000 and o.status == "completed")
    .sort_by(o -> o.amount, reverse=true)
    .take(10)

// Grouping and aggregation
var by_customer = orders
    .group_by(o -> o.customer)
    .map((k, v) -> (k, v.sum(o -> o.amount)))
```

### Dispatch strategy for structured data

| Data shape | Chain pattern | Strategy | Why |
|---|---|---|---|
| **list[dict] / list[record]** — most common | filter + map + aggregate | **Polars** lazy frame | Rust engine, columnar, handles strings/nested data |
| **list[dict]** | complex chains (filter + sort + group + take) | **Polars** or **DuckDB** | SQL-grade query optimization |
| **list[numeric]** | filter + map + sum | **NumPy/Numba** | SIMD, JIT — unbeatable for pure numeric |
| **any data** | first, take(n), any/all | **Generator expression** | Short-circuit, low overhead |
| **any data** | simple iteration / small lists | **Comprehension** | Zero deps, idiomatic |
| **JSON/Avro/CSV parsing** | deserialization | **orjson / fastavro / polars.read_csv** | Fastest parsers available |

The key insight: **Polars is the real workhorse for structured data**, not Numba. Polars handles strings, nested objects, grouping, sorting, joins — all in Rust. Numba is only better for pure numeric math pipelines.

### Fast serialization builtins

For data pipeline scripts, parsing speed matters as much as processing speed:

```zinc
// Zinc provides fast-path builtins that dispatch to best available parser
var data = json_load("big.json")           // orjson > ujson > json (auto-detect)
var records = csv_load("data.csv")         // polars.read_csv > csv.DictReader
var messages = avro_load("events.avro")    // fastavro
```

### Fallback tiers

```
Tier 1 (no deps):    List comprehensions, generator expressions, stdlib json/csv
Tier 2 (polars):     Columnar engine for structured data — pip install polars
Tier 3 (numpy):      Vectorized numeric ops — pip install numpy
Tier 4 (numba):      JIT-compiled numeric loops — pip install numba
Tier 5 (duckdb):     SQL analytics for complex queries — pip install duckdb
```

Zinc detects available packages and dispatches to the best available tier. Scripts work fine with zero deps (Tier 1) but scale to near-native performance with Polars/NumPy installed.

### Collection method names

Use Pythonic names, not LINQ:

| Operation | Zinc syntax |
|---|---|
| filter | `items.filter(x -> x > 0)` |
| map | `items.map(x -> x * 2)` |
| reduce | `items.reduce(0, (acc, x) -> acc + x)` |
| first | `items.first(x -> x > 10)` |
| any / all | `items.any(x -> x > 0)` |
| sum / min / max | `items.sum()` |
| sort | `items.sort()` or `items.sort_by(x -> x.age)` |
| take / skip | `items.take(10)` |
| distinct | `items.distinct()` |
| flat_map | `items.flat_map(x -> x.children)` |
| group_by | `items.group_by(x -> x.category)` |
| to_list / to_dict | `items.filter(...).to_list()` |

---

## What We Ditch from Zinc v1

| Zinc v1 Feature | Why ditch it |
|---|---|
| C#/Go backends | Python only now |
| `class` with full OO (inheritance hierarchies) | Not Pythonic for scripts. Keep `data` for data classes. Allow `class` but keep it simple. |
| Interfaces | Python uses duck typing and protocols — don't reinvent |
| Services / DI | Solved problem (FastAPI, Flask, etc.) |
| `pub` visibility | Python uses `_` convention — follow it |
| LINQ method names (Where, Select, etc.) | Use Pythonic names (filter, map, reduce, etc.) |
| `readonly` fields | Use `@dataclass(frozen=True)` |

## What We Keep

| Feature | Why |
|---|---|
| **Enforced type checking** | The killer feature — Python's types are suggestions, Zinc's are real |
| **`end` blocks** | The other killer feature — no whitespace wars, no braces |
| **String interpolation `{}`** | Better than f-string prefix |
| **`data` classes** | Cleaner than Python's dataclass decorator |
| **`var` inference** | Less noise than explicit types everywhere |
| **Script mode** | Top-level code just runs |
| **`fn` for functions** | Shorter than `def`, visually distinct |
| **`match`** | Keep it, map to Python's match |
| **Builtins** (file I/O, exec, etc.) | Zero-import scripting convenience |

---

## CLI

```bash
zinc run script.zn              # transpile + run with python
zinc run script.zn -- arg1 arg2 # pass args
zinc check script.zn            # type check only, don't run
zinc transpile script.zn        # output .py file
zinc fmt script.zn              # format zinc source
```

No `zinc init`, no `zinc build` for v2. Just files that run.

### Shebang

```zinc
#!/usr/bin/env zinc run
print("Hello from zinc!")
```

### Packaging & Deployment

Sometimes you need to ship a single binary or a self-contained package — not everyone has Python installed, and you don't want to hand someone a `.zn` file and a requirements list.

```bash
zinc pack script.zn                  # single-file executable (default: PyInstaller)
zinc pack script.zn --format docker  # Dockerfile with slim Python image
zinc pack script.zn --format pex     # PEX archive (Python EXecutable)
```

`zinc pack` transpiles to `.py`, resolves dependencies, and packages:

| Tool | Output | Size | Startup | Best for |
|---|---|---|---|---|
| **PyInstaller** | Single binary (--onefile) | 15-50 MB | ~1s | Desktop tools, CLI distribution |
| **Nuitka** | Compiled binary | 10-30 MB | Fast | Performance-sensitive, smaller binaries |
| **PyOxidizer** | Rust-wrapped binary | 15-40 MB | Fast | Hermetic builds, no temp extraction |
| **PEX** | Zip archive (.pex) | Small | Fast | Server deploys where Python exists |
| **Shiv** | Zip archive (.shiv) | Small | Fast | Similar to PEX, simpler |
| **Docker** | Container image | Varies | N/A | Microservices, cloud deploys |

**Default:** PyInstaller `--onefile` — most universal, handles most dependencies. Zinc auto-detects imports and adds hidden imports for common gotchas (numpy, pandas, etc.).

**Example — ship a CLI tool:**

```zinc
// greet.zn
import sys

var name = if len(sys.argv) > 1: sys.argv[1] else: "world"
print("Hello, {name}!")
```

```bash
zinc pack greet.zn
# produces: dist/greet (Linux/Mac) or dist/greet.exe (Windows)
./dist/greet Alice
# Hello, Alice!
```

**Example — deploy a data processor:**

```zinc
// process.zn
import polars as pl

var df = pl.read_csv(sys.argv[1])
var result = df.filter(pl.col("status") == "active").group_by("region").agg(pl.col("revenue").sum())
result.write_csv("output.csv")
print("Processed {len(df)} rows -> {len(result)} regions")
```

```bash
zinc pack process.zn --format docker
# produces: Dockerfile + process.py
docker build -t processor .
docker run -v ./data:/data processor /data/input.csv
```

**Future:** If Nuitka or PyOxidizer mature further for free-threaded Python, they become the preferred default — faster startup and smaller binaries than PyInstaller.

---

## Python Version Target

**Python 3.13+ (free-threaded / no-GIL mode)**

Target `python3.13t` — the free-threaded build that removes the GIL. This gives Zinc real parallelism for data pipelines without reaching for multiprocessing.

### Why free-threaded Python

- **True parallelism** — threads actually run concurrently on multiple cores. A collection `.map()` over 1M records can fan out across cores.
- **Simpler concurrency model** — `threading` just works, no need for `multiprocessing` with its pickling headaches and process overhead.
- **Data pipelines benefit most** — processing flowfiles, JSON batches, Avro streams in parallel is a natural fit.
- **No fork/pickle issues** — multiprocessing struggles with complex objects, lambdas, database connections. Threads share memory directly.

### Library compatibility

Most major libraries already support or are actively adding no-GIL support:

| Library | Status | Notes |
|---|---|---|
| **NumPy** | Supported (2.1+) | Thread-safe, releases GIL internally anyway |
| **Polars** | Supported | Rust-based, never depended on GIL |
| **orjson** | Supported | Rust-based |
| **fastavro** | Likely safe | C extension, needs verification |
| **requests/httpx** | Supported | I/O-bound, benefits from free-threading |
| **DuckDB** | Supported | C++ engine, own threading |
| **Numba** | **Not yet** | JIT relies on GIL for some internals — check status |
| **pandas** | In progress | Some operations not yet thread-safe |
| **SQLAlchemy** | Supported (2.0+) | Connection pooling is thread-safe |

**Risk mitigation:** Zinc should default to free-threaded mode but provide a fallback. If a script imports a library known to be GIL-dependent, warn at transpile time or fall back to GIL mode.

### Concurrency in Zinc

Free-threading makes simple parallel patterns viable without async complexity:

```zinc
import threading

// Parallel map over chunks — Zinc could auto-parallelize this
var results = items.parallel_map(item -> process(item), workers=4)

// Or explicit threads with shared state (safe without GIL)
var counts = {}
var lock = threading.Lock()

fn count_words(chunk: list[str])
    var local_counts = {}
    for line in chunk
        for word in line.split()
            local_counts[word] = local_counts.get(word, 0) + 1
        end
    end
    with lock
        for word, count in local_counts.items()
            counts[word] = counts.get(word, 0) + count
        end
    end
end
```

**Future potential:** The transpiler could auto-detect pure `.map()` / `.filter()` chains on large collections and generate parallel dispatch code using thread pools. The developer writes sequential-looking code, Zinc makes it parallel.

### Other 3.13+ features
- `match` statements (3.10+)
- Better error messages (3.12+)
- `type` statement for aliases (3.12+)
- f-string improvements (3.12+)
- Improved `asyncio` performance

---

## Example: The Zinc v2 Experience

### Quick script
```zinc
#!/usr/bin/env zinc run
// count-lines.zn — count lines in all .py files

import os

var total = 0
for root, dirs, files in os.walk(".")
    for f in files
        if f.endswith(".py")
            var path = os.path.join(root, f)
            var lines = len(open(path).readlines())
            print("{path}: {lines}")
            total += lines
        end
    end
end
print("\nTotal: {total} lines")
```

### Data processing
```zinc
#!/usr/bin/env zinc run
// analyze.zn — quick CSV analysis

import csv
import sys

var reader = csv.DictReader(sys.stdin)
var rows = list(reader)

var total = sum(float(r["amount"]) for r in rows)
var avg = total / len(rows)

print("Rows: {len(rows)}")
print("Total: {total:.2f}")
print("Average: {avg:.2f}")

var big = [r for r in rows if float(r["amount"]) > avg]
print("Above average: {len(big)}")
```

### API client
```zinc
// fetch-issues.zn

import requests
import json

fn get_issues(repo: str): list[dict]
    var resp = requests.get("https://api.github.com/repos/{repo}/issues")
    resp.raise_for_status()
    return resp.json()
end

var issues = get_issues("python/cpython")
for issue in issues[:10]
    print("#{issue['number']}: {issue['title']}")
end
```

---

## Decisions Made

1. **`end` blocks** — decided. Matches Julia/Crystal/Ruby, natural for sci/data crowd, no shift key needed.

2. **Multi-line continuation** — implicit via trailing operators, commas, open brackets. No backslash hell.

3. **Two-track error handling** — `Result[T]` for expected failures (validation, parsing), exceptions only for truly exceptional cases. No `or {}` — clean separation.

4. **No dunders** — clean method names (`fn str()`, `fn len()`, etc.), transpiler maps to `__dunder__`.

5. **Auto-inject `self`** — fields accessed by name, transpiler adds `self.` prefix.

6. **`else if`** — two words instead of Python's `elif`. Reads like English.

7. **Expression if** — condition-first ternary: `if cond: val else: val`. Transpiles to Python's backwards ternary.

8. **Colon for return types** — `fn greet(name: str): str` instead of `->`. Colon is home-row, no shift needed. Lambdas keep `->` arrow (`x -> x * 2`).

## Open Questions

1. **Method chaining vs comprehensions?** — Could support both. Comprehensions pass through to Python naturally. Method chaining (`.filter().map()`) is more readable for long pipelines and enables smart dispatch. Leaning: support both.

2. **How much OO to keep?** — `data` for data classes is great. Full `class` with methods for when you need it. But discourage deep inheritance. No interfaces — use Python's Protocol if needed.

3. **Zinc runtime library?** — Ideally zero. Builtins like `exec()`, `read_file()` could inline their Python equivalents. But a tiny `zinc.py` runtime for collection dispatch and parallel_map might be practical.

4. **Type system depth?** — Start with Python's type syntax (str, int, list[str], dict[str, int], Optional[str], Union[str, int]). Zinc enforces them at transpile time. Generics and protocols can come later.

5. **Async?** — Python's async/await is powerful but adds complexity. Free-threading may reduce the need. Defer to v2.1?

---

## Implementation Plan

### Phase 1 — Minimum Viable Transpiler
- Lexer/parser for: `var`, `fn`, `if/else if/else`, `for`, `while`, `return`, `print`
- `end` blocks → indentation conversion
- Multi-line statement continuation
- String interpolation → f-strings
- Type inference for `var`
- Type checking (basic: int, str, float, bool, list, dict)
- `zinc run` and `zinc transpile` commands

### Phase 2 — Pythonic Features
- `data` → `@dataclass`
- `match` → `match`
- `import` pass-through
- Comprehension support
- `enum` support

### Phase 3 — Scripting Power
- Shebang support
- Builtins (file I/O, exec, path utils)
- Result type runtime library

### Phase 4 — Ecosystem
- Multi-file support with module resolution
- `zinc check` for CI type checking
- LSP / editor support
- Package management integration (pip/uv)
