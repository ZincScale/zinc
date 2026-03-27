# Python Backend: Design Document

> **Status**: DESIGN COMPLETE — ready for implementation
> **Target**: Python 3.14t (free-threading, no GIL)
> **Scope**: Zinc → Python transpilation, translation matrix, concurrency runtime, deployment, stdlib candidates

## Why Python

Python gives Zinc a QRC (Quick Reaction Capability) backend:

| Factor | Java backend | Python backend |
|---|---|---|
| Cold start | ~500ms (JVM) / ~30ms (GraalVM native) | ~10ms (native binary via Nuitka) |
| Binary size | 119MB (jpackage) / 20-40MB (GraalVM) | 15-60MB (Nuitka/PyInstaller) |
| Ecosystem | Maven Central, enterprise Java | PyPI, data science, ML, scripting |
| Deployment | JVM or GraalVM required | Python 3.14t or compiled binary |
| Concurrency | Virtual threads + StructuredTaskScope | Free-threading (3.14t) + ThreadPoolExecutor |
| Use case | Production services, heavy compute | QRC, data pipelines, ML integration, prototyping |

The Python backend is not a replacement for Java — it's a second target that opens Zinc to the Python ecosystem. A Zinc developer writes once and can deploy to either JVM or Python depending on the workload.

## Translation Matrix

The transpiler doesn't do naive 1:1 translation. It picks the optimal Python idiom based on context — the Zinc developer writes clean loops and functional chains, the transpiler chooses the best Python representation.

### Loops and Iteration

| Zinc | Small (< ~100 items) | Medium (~100-10K) | Large (10K+) or numeric |
|---|---|---|---|
| `for x in items { f(x) }` | `for x in items: f(x)` | `for x in items: f(x)` | Same (iteration is iteration) |
| `items.map(x -> x * 2)` | `[x * 2 for x in items]` | `[x * 2 for x in items]` | `np.array(items) * 2` |
| `items.filter(x -> x > 0)` | `[x for x in items if x > 0]` | `[x for x in items if x > 0]` | `arr[arr > 0]` (numpy boolean indexing) |
| `items.filter(p).map(f)` | `[f(x) for x in items if p(x)]` | `[f(x) for x in items if p(x)]` | `np.vectorize(f)(arr[p(arr)])` |
| `items.sum()` | `sum(items)` | `sum(items)` | `np.sum(arr)` |
| `items.reduce(init, f)` | `functools.reduce(f, items, init)` | `functools.reduce(f, items, init)` | `np.reduce` or pandas |
| `items.sortBy(x -> x.age)` | `sorted(items, key=lambda x: x.age)` | `sorted(items, key=...)` | `df.sort_values('age')` |
| `items.groupBy(x -> x.cat)` | `defaultdict` accumulation | `itertools.groupby` | `df.groupby('cat')` |
| `items.distinct()` | `list(dict.fromkeys(items))` | `list(set(items))` | `pd.unique(arr)` |
| `items.flatMap(x -> x.items)` | `[y for x in items for y in x.items]` | Same | `pd.explode()` |
| `items.count()` | `len(items)` | `len(items)` | `len(arr)` or `df.shape[0]` |
| `items.min()` / `items.max()` | `min(items)` / `max(items)` | Same | `np.min(arr)` / `np.max(arr)` |
| `items.anyMatch(p)` | `any(p(x) for x in items)` | Same | `np.any(p(arr))` |
| `items.allMatch(p)` | `all(p(x) for x in items)` | Same | `np.all(p(arr))` |
| `for i, x in items { }` | `for i, x in enumerate(items):` | Same | Same |
| `for i in 0..10 { }` | `for i in range(10):` | Same | Same |
| `for i in 1..=10 { }` | `for i in range(1, 11):` | Same | Same |

### How the transpiler decides

The transpiler uses type context and hints to choose the right tier:

1. **Type annotation** — if the variable is typed as `DataFrame` or `NDArray`, use pandas/numpy ops
2. **Size hint** — `@large` annotation on a collection triggers numpy/pandas path
3. **Numeric homogeneity** — if the collection is `List<int>` or `List<double>` and used in arithmetic chains, numpy is preferred
4. **Default** — list comprehensions for functional chains, for-loops for imperative blocks

The Zinc developer never thinks about this. They write `items.filter(it > 0).map(it * 2).sum()` and the transpiler picks the optimal Python idiom.

### Data Types

| Zinc | Python | Notes |
|---|---|---|
| `int` | `int` | Python ints are arbitrary precision |
| `double` | `float` | 64-bit float |
| `boolean` | `bool` | |
| `String` | `str` | |
| `byte[]` | `bytes` | |
| `List<T>` | `list[T]` | |
| `Map<K, V>` | `dict[K, V]` | Python dicts preserve insertion order |
| `Set<T>` | `set[T]` | |
| `T[]` (array) | `list[T]` or `numpy.ndarray` | ndarray for numeric types |
| `(T, U)` (tuple) | `tuple[T, U]` | Python tuples are native |
| `Type?` (nullable) | `T \| None` | Python 3.10+ union syntax |
| `any` | `Any` | typing.Any |

### Data Classes / Records

| Zinc | Python |
|---|---|
| `data User(String name, int age)` | `@dataclass(frozen=True, slots=True)` |
| `data Point(double x, double y)` | `@dataclass(frozen=True, slots=True)` |
| Field access: `user.name` | `user.name` — identical |
| Equality: `a == b` | `a == b` — dataclass auto-generates `__eq__` |
| Hashing | Auto-generated for frozen dataclasses |
| toString | `__repr__` auto-generated |

`slots=True` gives memory efficiency. `frozen=True` gives immutability (like Java records).

### Sealed Classes / Sum Types

| Zinc | Python |
|---|---|
| `sealed class Shape { data Circle(r); data Rect(w, h) }` | Base class + `@dataclass` variants + `__match_args__` |
| `match shape { case Circle(r) -> ... }` | `match shape: case Circle(r): ...` (Python 3.10+ structural pattern matching) |

Python 3.10+ pattern matching maps cleanly:

```python
# Zinc: sealed class Shape { data Circle(double r); data Rect(double w, double h) }
class Shape: pass

@dataclass(frozen=True, slots=True)
class Circle(Shape):
    r: float

@dataclass(frozen=True, slots=True)
class Rect(Shape):
    w: float
    h: float

# Zinc: match shape { case Circle(r) -> pi * r ** 2; case Rect(w, h) -> w * h }
match shape:
    case Circle(r):
        result = math.pi * r ** 2
    case Rect(w, h):
        result = w * h
```

### Enums

| Zinc | Python |
|---|---|
| `enum Color { Red, Green, Blue }` | `class Color(Enum): RED = auto(); GREEN = auto(); BLUE = auto()` |
| `match c { case Color.Red -> ... }` | `match c: case Color.RED: ...` |

### Classes

| Zinc | Python |
|---|---|
| `class Foo { ... }` | `class Foo: ...` |
| `fn init(String name) { this.name = name }` | `def __init__(self, name: str): self.name = name` |
| `pub fn bar(): String { }` | `def bar(self) -> str: ...` |
| `class Dog : Animal { }` | `class Dog(Animal): ...` |
| `override fn speak() { }` | `def speak(self): ...` (no annotation needed, Python uses duck typing) |
| `static fn create(): Foo { }` | `@staticmethod def create() -> Foo: ...` |
| `pub String name` | Property with getter/setter |
| `readonly String name` | Property with getter only |
| `init String name` | Set in `__init__`, no setter (convention) |

### Control Flow

| Zinc | Python |
|---|---|
| `if x > 0 { }` | `if x > 0:` |
| `if x > 0: "yes" else: "no"` | `"yes" if x > 0 else "no"` |
| `for x in items { }` | `for x in items:` |
| `while running { }` | `while running:` |
| `match x { case 1 -> ... }` | `match x: case 1: ...` |
| `break` / `continue` | `break` / `continue` |

### Error Handling

| Zinc | Python | Notes |
|---|---|---|
| `return Error("msg")` | `raise ZincError("msg")` | Python uses exceptions natively |
| `call() or default` | `try: call() except: default` | Or use a helper function |
| `call() or { log(err); fallback }` | `try: call() except Exception as err: log(err); fallback` | |
| `data ValidationError(field, reason)` | `class ValidationError(ZincError): ...` | Dataclass exception |

Python's exception model is actually a natural fit — no impedance mismatch. Zinc hides exceptions behind `or` blocks on the Java side because Java exceptions are clunky. Python exceptions are idiomatic and zero-cost on the happy path (unlike Java, Python optimizes for `try` being cheap).

### String Operations

| Zinc | Python |
|---|---|
| `"Hello {name}"` | `f"Hello {name}"` |
| `'no interpolation'` | `'no interpolation'` |
| `` `raw\n` `` | `r'raw\n'` |
| `"""multiline"""` | `"""multiline"""` |
| `"World" in s` | `"World" in s` — identical |
| `s.split(",")` | `s.split(",")` |
| `s.toUpperCase()` | `s.upper()` |
| `s.trim()` | `s.strip()` |
| `s.startsWith("x")` | `s.startswith("x")` |

### Imports

| Zinc | Python |
|---|---|
| `import java.time.Instant` | `from datetime import datetime` (mapped to Python stdlib equivalent) |
| `import models.*` | `from models import *` |
| Directory = package | Directory with `__init__.py` = package |
| Auto-imports within project | Explicit imports (Python requires them) |

## Concurrency: 3.14t Free-Threading Runtime

### The model

Python 3.14t removes the GIL. Threads run in true parallel. Zinc's concurrency primitives map directly to `threading` + `concurrent.futures` — no async/await coloring needed.

### Mapping

| Zinc | Python 3.14t | Lifecycle |
|---|---|---|
| `spawn { task() }` | `scope.submit(task)` | Tracked by parent scope |
| `concurrent { a(); b(); c() }` | `ZincScope` context manager + `executor.submit` per task | All complete or all cancel |
| `concurrent(first: true)` | `concurrent.futures.wait(FIRST_COMPLETED)` + cancel rest | First wins, rest cancelled |
| `parallel for x in xs { f(x) }` | `ZincScope` + `executor.map(f, xs)` | Structured — waits for all |
| `parallel(max: 10) for` | `ThreadPoolExecutor(max_workers=10)` | Bounded directly |
| `lock mu {}` | `with threading.Lock():` | Real mutex (GIL-free, actually needed) |
| `Channel<T>(100)` | `ZincChannel(maxsize=100)` (wraps `queue.Queue` + close) | Bounded, blocking |
| `timeout(5.seconds) { }` | `future.result(timeout=5)` + cancel | Raises on timeout |
| `with resource { }` | `with resource:` | Context manager — identical |

### zinc_runtime.py — The "Good Children" Runtime

The transpiler generates code that imports a thin runtime library. This library enforces structured concurrency — threads are good children that clean up after themselves.

```
zinc_runtime.py
  ZincScope           Structured scope — tracks children, cancels on failure, waits on exit
  ZincChannel         queue.Queue wrapper with close() and iteration (for x in channel)
  ZincTimeout         Executor + future.result(timeout=N) + cancellation
  zinc_main(fn)       Entry point wrapper — signal handlers, top-level scope, clean shutdown
  ZincError           Base exception for Zinc error values
```

**ZincScope contract:**

1. All child tasks (spawn, concurrent, parallel) are submitted to the scope's executor
2. Scope tracks all futures
3. On child failure: cancel all siblings, propagate exception to parent
4. On scope exit (normal): wait for all children to complete
5. On scope exit (exception): cancel all children, then propagate
6. On SIGTERM/SIGINT: cancel all scopes top-down, allow cleanup

This is the Ktor Job / Java ExecutorService / Erlang supervisor pattern — cooperative shutdown with a managed lifecycle. No orphaned threads, no leaked resources.

**Unstructured spawn:**

For fire-and-forget `spawn` blocks, two modes:

| Mode | Behavior | When |
|---|---|---|
| Daemon thread | Dies when main exits — no cleanup | spawn with no result used, no resources |
| Tracked task | Cancelled gracefully on exit | spawn that writes to files/network/DB |

The transpiler decides based on whether the spawn block contains resource operations (file I/O, network, DB). If it does, track. If it's pure computation or logging, daemon.

### Free-Threading Library Compatibility

Based on the [py-free-threading compatibility tracker](https://py-free-threading.github.io/tracking/), these libraries are confirmed compatible with Python 3.14t:

**Data processing:**

| Library | Version | Use case |
|---|---|---|
| NumPy | 2.1.0 | Numeric arrays, vectorized ops |
| pandas | 2.2.3 | DataFrames, tabular data |
| PyArrow | 18.0.0 | Parquet, Arrow columnar format |
| SciPy | 1.15.0 | Scientific computing |
| scikit-learn | 1.6.0 | Machine learning |
| h5py | 3.16.0 | HDF5 file format |
| bottleneck | 1.5.0 | Fast NumPy aggregations |

**Serialization:**

| Library | Version | Use case |
|---|---|---|
| msgspec | 0.20.0 | Fast JSON/MessagePack (faster than orjson) |
| PyYAML | 6.0.3 | YAML |
| pydantic | 2.11.0 | Data validation + serialization |
| lz4 | 4.4.5 | Fast compression |
| zstandard | 0.25.0 | Zstd compression |
| cramjam | 2.11.0 | Multiple compression algorithms |

**Networking / messaging:**

| Library | Version | Use case |
|---|---|---|
| aiohttp | 3.13.0 | HTTP client/server |
| PyZMQ | 27.0.0 | ZeroMQ messaging |
| cryptography | 46.0.0 | TLS, crypto |
| bcrypt | 4.3.0 | Password hashing |
| pycares | 4.11.0 | Async DNS |

**Database:**

| Library | Version | Use case |
|---|---|---|
| SQLAlchemy | 2.0.45 | Database ORM/Core |

**ML / AI:**

| Library | Version | Use case |
|---|---|---|
| PyTorch | 2.6.0 | Deep learning |
| JAX | 0.5.1 | Differentiable computing |
| scikit-learn | 1.6.0 | Traditional ML |
| ONNX | 1.18.0 | Model interchange |
| Pillow | 11.0.0 | Image processing |
| matplotlib | 3.9.0 | Plotting / visualization |

**Not yet compatible (in progress):**

| Library | Workaround |
|---|---|
| protobuf | Use msgspec for serialization |
| grpcio | Use HTTP/2 client directly |
| lxml | Use stdlib `xml.etree.ElementTree` |
| psycopg | Use SQLAlchemy (compatible) |
| polars | Use pandas (compatible) |
| orjson | Use msgspec (compatible, equally fast) |
| tornado | Use aiohttp or starlette |

## What Python Has That Zinc Should Steal

The Python ecosystem has idioms and libraries so good they deserve to be first-class Zinc features — either in the standard library or the core language.

### Language-Level Candidates

**1. Slicing syntax**

Python: `items[1:5]`, `items[-3:]`, `items[::2]` (every other element)
Zinc today: `items.subList(1, 5)` (verbose)

Zinc could add: `items[1..5]`, `items[-3..]`, `items[..step 2]`

This maps to `List.subList()` on Java and native slicing on Python. Zero-cost abstraction.

**2. Tuple unpacking everywhere**

Python: `a, b = b, a` (swap), `first, *rest = items` (destructure with rest), `for k, v in dict.items():`

Zinc has tuple unpacking for function returns but could extend to:
- Swap: `a, b = b, a`
- Rest patterns: `var first, ...rest = items`
- Iteration: already has `for i, x in items { }`

**3. Comprehension-style collection construction**

Python: `[x * 2 for x in items if x > 0]`

Zinc's functional chains already do this (`items.filter(it > 0).map(it * 2)`) but a comprehension syntax could be an alternative:
`var doubled = [it * 2 for it in items if it > 0]`

This is syntactic sugar — transpiles to Stream API on Java, list comprehension on Python.

**4. Multiple assignment / destructuring in match**

Zinc already has this. Good — keep it.

**5. `_` as discard**

Python: `_, value = get_pair()` (discard first element)
Zinc could use `_` consistently for "I don't care about this value."

**6. Walrus operator (assignment expression)**

Python: `if (n := len(items)) > 10: print(f"too many: {n}")`

Useful for avoiding repeated computation in conditions. Zinc could add:
`if var n = items.size(); n > 10 { print("too many: {n}") }`

### Standard Library Candidates

**7. DataFrame as a first-class collection**

pandas DataFrames are so ubiquitous they could be a Zinc stdlib type:

```zinc
// Zinc with DataFrame support
var df = DataFrame.read("users.csv")
var adults = df.filter(it["age"] > 18)
var by_city = adults.groupBy("city").count()
var sorted = by_city.sortBy("count", desc=true)
sorted.write("output.parquet")
```

On Java: transpiles to Apache Arrow + Tablesaw or similar
On Python: transpiles directly to pandas

This is the single highest-value addition from the Python ecosystem. Data manipulation is the most common programming task, and DataFrames are the best abstraction for it.

**8. NDArray / tensor as a first-class numeric type**

```zinc
// Zinc with numeric array support
var a = NDArray.of([1.0, 2.0, 3.0, 4.0])
var b = a * 2.0 + 1.0          // vectorized arithmetic
var c = a.reshape(2, 2)        // matrix
var d = c @ c                   // matrix multiply
var mask = a > 2.0              // boolean mask
var filtered = a[mask]          // fancy indexing
```

On Java: transpiles to ND4J or Panama Vector API
On Python: transpiles directly to numpy

For ML, scientific computing, and numeric processing, this eliminates the need to drop into Python/numpy manually.

**9. Built-in serialization**

Python's ecosystem has msgspec (zero-copy, schema-validated serialization). Zinc could have:

```zinc
// Automatic serialization from data classes
data User(String name, int age)

var json = User.toJson(user)           // {"name": "Alice", "age": 30}
var user = User.fromJson(jsonString)   // deserialization with validation
var bytes = User.toMsgpack(user)       // binary format
var parquet = users.toParquet("out.parquet")  // columnar
```

On Java: transpiles to Jackson
On Python: transpiles to msgspec or pydantic

Every data class automatically gets serialization — no annotations, no configuration.

**10. HTTP client as stdlib**

Python's `requests` / `httpx` are best-in-class. Zinc could have:

```zinc
var resp = Http.get("https://api.example.com/users")
var users = resp.json(List<User>)

var resp = Http.post("https://api.example.com/users", body=user.toJson())
```

On Java: transpiles to `java.net.http.HttpClient`
On Python: transpiles to `httpx`

**11. Path / filesystem operations**

Python's `pathlib` is excellent. Zinc could have:

```zinc
var p = Path("data/users.csv")
var content = p.readText()
var lines = p.readLines()
p.parent.mkdir()
for f in Path("data").glob("*.csv") { process(f) }
```

On Java: transpiles to `java.nio.file.Path` + `Files`
On Python: transpiles to `pathlib.Path`

**12. Date/time**

Python's datetime is simpler than Java's java.time. Zinc could pick the best of both:

```zinc
var now = DateTime.now()
var tomorrow = now + 1.day
var formatted = now.format("yyyy-MM-dd")
var parsed = DateTime.parse("2026-03-27", "yyyy-MM-dd")
var diff = end - start    // Duration
```

### Priority ranking for stdlib additions

| Feature | Value | Effort | Priority |
|---|---|---|---|
| DataFrame (pandas equivalent) | Very high — data manipulation is everywhere | High — need dual-backend impl | P1 |
| Built-in serialization (JSON, Msgpack, Parquet) | Very high — every app serializes | Medium | P1 |
| HTTP client | High — every app calls APIs | Low — thin wrapper | P1 |
| NDArray / tensor | High for ML/numeric | High — need dual-backend impl | P2 |
| Slicing syntax | Medium — QoL improvement | Low — syntactic sugar | P2 |
| Path / filesystem | Medium — common operations | Low — thin wrapper | P2 |
| Date/time | Medium — common operations | Low — thin wrapper | P3 |
| Comprehension syntax | Low — functional chains already exist | Low | P3 |

## Binary Deployment

### Deployment modes

| Mode | Base image | Size | Use case |
|---|---|---|---|
| **Docker (standard)** | `python:3.14t-slim` + Java 25 | ~300MB | Development, CI, both backends |
| **Docker (minimal)** | `chainguard/python` | 60-90MB | Production Python-only |
| **Docker (distroless)** | Nuitka binary + distroless base | 15-50MB | Production, minimal surface |
| **Docker (scratch)** | PyInstaller + staticx | 8-60MB | Smallest possible, fragile |
| **Native binary** | Nuitka `--mode=onefile` | 15-50MB | Direct deployment, no container |

### Standard deployment: Docker with Java + Python

For development and dual-backend deployment, a base image with both runtimes:

```dockerfile
FROM python:3.14t-slim

# Add Java 25 (GraalVM or Eclipse Temurin)
COPY --from=eclipse-temurin:25-jre /opt/java /opt/java
ENV PATH="/opt/java/bin:$PATH"

# Install Python deps via uv
COPY --from=ghcr.io/astral-sh/uv:latest /uv /bin/uv
COPY pyproject.toml .
RUN uv sync --no-dev

# Copy transpiled code
COPY src/ src/

# Run
CMD ["python", "-Xgil=0", "src/main.py"]
```

Image size: ~300MB (Java JRE ~150MB + Python ~120MB + deps)

### Minimal Python deployment: Chainguard distroless

```dockerfile
# Build stage
FROM python:3.14t-slim AS builder
COPY --from=ghcr.io/astral-sh/uv:latest /uv /bin/uv
COPY . .
RUN uv sync --no-dev

# Runtime stage — no shell, no package manager, zero CVEs
FROM chainguard/python:latest
COPY --from=builder /app /app
ENTRYPOINT ["python", "-Xgil=0", "/app/src/main.py"]
```

Image size: 60-90MB

### Native binary deployment: Nuitka to distroless

```dockerfile
# Build stage — compile Python to native binary
FROM python:3.14t AS builder
RUN pip install nuitka
COPY . .
RUN python -m nuitka --mode=onefile --output-dir=/dist src/main.py

# Runtime stage — near-scratch
FROM gcr.io/distroless/cc-debian12
COPY --from=builder /dist/main.bin /app/main
ENTRYPOINT ["/app/main"]
```

Image size: 15-50MB (Nuitka binary + minimal libc)

Notes:
- Nuitka free-threading support is experimental but active (issue #3572 resolved for 3.14t)
- `--mode=onefile` bundles everything including libpython
- UPX compression can further reduce by ~40%

### Scratch deployment: PyInstaller + staticx

```dockerfile
# Build stage
FROM python:3.14t AS builder
RUN pip install pyinstaller staticx
COPY . .
RUN pyinstaller --onefile src/main.py
RUN staticx dist/main dist/main-static

# Runtime — true scratch, nothing else
FROM scratch
COPY --from=builder /dist/main-static /main
COPY --from=builder /tmp /tmp
ENTRYPOINT ["/main"]
```

Image size: 8-60MB

Notes:
- Needs `/tmp` for PyInstaller onefile extraction at runtime
- staticx wraps the binary with a static loader
- Most fragile option — hard to debug, no shell access
- Best for absolute minimum size requirements

### Deployment decision matrix

| Constraint | Recommended approach |
|---|---|
| Need both Java and Python backends | Standard Docker (Java + Python) |
| Python-only, production, need security | Chainguard distroless |
| Smallest possible, willing to accept fragility | PyInstaller + staticx + scratch |
| Direct deployment (no container) | Nuitka onefile binary |
| CI/testing | Standard Docker or python:3.14t-slim |

## Implementation Plan

### Phase 1: Core transpilation

1. Add Python emitter to zinc compiler (alongside Java emitter)
2. Implement basic type mapping (primitives, strings, collections)
3. Implement control flow (if/else, for, while, match)
4. Implement data classes → frozen dataclasses
5. Implement sealed classes → base class + match
6. Implement classes (inheritance, methods, visibility)
7. Test on simple zinc programs

### Phase 2: Translation matrix

8. Implement collection method chains → list comprehensions
9. Implement `it` keyword → lambda in comprehension
10. Implement string interpolation → f-strings
11. Implement error handling → try/except with ZincError
12. Implement `with` blocks → context managers
13. Test on zinc-flow

### Phase 3: Concurrency runtime

14. Build `zinc_runtime.py` (ZincScope, ZincChannel, ZincTimeout, zinc_main)
15. Implement `spawn` → scope.submit
16. Implement `concurrent {}` → ZincScope + futures
17. Implement `parallel for` → executor.map in scope
18. Implement `lock` → threading.Lock context manager
19. Implement `Channel<T>` → ZincChannel
20. Implement `timeout` → future.result(timeout=N)
21. Test concurrency on zinc-flow pipeline

### Phase 4: Deployment

22. Dockerfile templates for each deployment mode
23. Nuitka build integration (`zinc build --python --native`)
24. PyInstaller build integration (`zinc build --python --onefile`)
25. Test on distroless and scratch containers

### Phase 5: Stdlib extensions (future)

26. DataFrame support (pandas on Python, Tablesaw on Java)
27. Built-in serialization (msgspec on Python, Jackson on Java)
28. HTTP client (httpx on Python, java.net.http on Java)
29. NDArray support (numpy on Python, ND4J/Panama on Java)

## References

- [Python 3.14t free-threading](https://docs.python.org/3.14/whatsnew/3.14.html)
- [Free-threading compatibility tracker](https://py-free-threading.github.io/tracking/)
- [PEP 703 — Making the Global Interpreter Lock Optional](https://peps.python.org/pep-0703/)
- [PEP 779 — Free-threading no longer experimental in 3.14](https://peps.python.org/pep-0779/)
- [Nuitka free-threading support](https://github.com/Nuitka/Nuitka/issues/3572)
- [Chainguard Python images](https://images.chainguard.dev/directory/image/python/overview)
- [python-build-standalone (Astral)](https://github.com/astral-sh/python-build-standalone)
- [uv Docker guide](https://docs.astral.sh/uv/guides/integration/docker/)
