# Coming from Python

zinc *looks* like Python — defs, `print`, f-strings, `match`, list/dict literals, `for x in
xs`, lambdas — but it's **statically typed and compiles to the BEAM**, so a handful of things
are deliberately different. When dynamic-Python convenience and safe-on-BEAM semantics
conflict, zinc chooses safe. Here's what to know.

### It's typed, and braces, not whitespace
Blocks use `{ }`, not indentation. **Params must be typed** (`def add(a: int, b: int) -> int`);
locals infer from assignment (`n = 7`). Return types are checked, and for an `Actor` the
return type *is* the messaging contract (§ below). f-strings are **explicit** — write
`f"hi {name}"`, not `"hi {name}"`. **`self` is implicit** — write `def incr()` and bare
`count`, not Python's `def incr(self)`/`self.count` (explicit `self.` still works). Chains use `else if`,
not `elif`.

### Concurrency is actors, not asyncio or threads
There is **no `async`/`await`, no `threading`, no GIL, no shared mutable globals.** You create
a process by constructing a class that extends `Actor`; that's the only way. Mutable state
lives *inside* an actor (serialized by its mailbox); everywhere else, data is immutable. You
get **real multicore parallelism** (every actor is a BEAM process scheduled across all cores)
without locks. Fan-out is the worker-actor idiom; a bounded `Channel` gives backpressure.
Processes are microsecond-cheap, so spawn-per-task is normal.

### Construction of an Actor spawns a process
For `class Counter(Actor)`, `Counter()` doesn't make an object — it **spawns a supervised
process** and returns a handle. The constructor runs inside that process. A `Counter()` in a
field is a permanent child; one in a method body is temporary (dies with its owner).

### Records and objects are immutable
Record fields and instance-class fields are set at construction and never reassigned — no
`p.x = v`, no setters. Build a new value instead. **Locals stay fully mutable** (counters,
accumulators). Mutable state that must change over time goes in an actor. Strings are
immutable UTF-8 binaries, but `s += "..."` is an amortized-O(1) append — no `StringBuilder`,
no `"".join(...)` dance.

### `try`/`except` is transactional
A caught `try` **reverts the outer-variable mutations** it made before the raise — the BEAM
can't observe partial bindings, so a half-finished `try` leaves no trace. Exceptions are
unchecked; `raise` inside an actor's call-method relays to the caller (catchable there), and
a genuine bug crashes the process — the supervisor restarts it.

### No `None`-shaped footguns — and no `Optional` ceremony either
There's no implicit `None` flowing through your program waiting to `AttributeError` ten frames
later. The type net catches what corrupts data *silently* (type confusion across boundaries,
mixed-dict misuse). But there's also **no `Optional`/unwrap ceremony** to write: a nil deref
crashes loud and the supervisor restarts the process. That's the null story — runtime
loud-crash + supervision, not type-level fluff. ("Safe but no safer.")

### Collections split by cost
`{...}` is a `HashMap`, `[...]` is a `List`. `d["k"]`, `d["k"] = v`, and `len(x)` work as you'd
expect. Homogeneous literals are statically typed (`{"a":1}` is `HashMap<String,int>`), so
`scores["a"] + "s"` is a compile error; mixed-value dicts stay dynamic and need a typed
crossing (`host: String = cfg["host"]`). For indexing/appending in a loop use `ArrayList`
(O(log n)); plain `List` is for receive/iterate (`get`/`size` are O(n) and warned). `byte[]`
is a binary; `int[]` is a fixed array.

### It's static — no runtime magic
No monkeypatching, no `__getattr__`, no `eval`, no adding attributes at runtime, no duck
typing across unrelated types. Interfaces are nominal (`class X(Greeter)`) and checked at
compile time. What you can't express statically, you reach for the FFI for (below).

### Files and config are explicit
Python code often mixes path strings, dynamic dict config, and open file handles casually. Zinc keeps these boundaries explicit: `Config.decode(RecordType, path)` turns JSON config into a typed record; `Files.list` and `Files.walk` return sorted discovery results; `Files.join`, `baseName`, `dirName`, and `extension` cover the common path operations; scoped `with Files.openReader/openWriter/openAppender` closes handles automatically and keeps large files streaming.

### Small differences
- `Sys.sleep(ms)` instead of `time.sleep` (and it takes milliseconds).
- A service uses `class Main(Application)` with `def main()`; a script/tool is just a
  top-level `def main()`.
- Your own constants are `enum`s (which lower to BEAM atoms); a raw atom is `Tag.of("x")`.
- `int` is arbitrary-precision (BEAM bignums) — no overflow.
- Reliability you'd hand-roll in Python (retries, restarts, process isolation) is the
  runtime's job, not yours.

### What's deferred (not in v1)
`finally` beyond the scoped `with` handles (`Reader`/`Writer`/`HttpStream`), exception
hierarchies beyond one level, comprehensions, generators/`yield`, decorators, keyword/default
args, multi-node distribution. Reach for the `from erlang import …` FFI escape hatch when you
need something the stdlib doesn't cover yet.

---

Back to the **[guide](guide.md)** or **[tutorials](tutorials.md)**.
