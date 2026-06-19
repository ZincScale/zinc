# The zinc guide

zinc is **braces-Python that compiles to Erlang/OTP.** You write Python-shaped code — with
braces instead of significant whitespace, and types — and the transpiler emits readable
Erlang that runs on the BEAM. The deal, stated once: *Python ergonomics, typed, with the
BEAM's reliability built in.* Where the BEAM offers something Python can't (supervised
processes), you opt in with a **base class** (`class C(Actor)`) — never a new keyword.

This guide is the whole surface, in order. Each piece has a runnable snippet; the file names
in parentheses are real, tested examples under [`examples/py/`](../examples/py).

---

## 1. Programs, files, modules

- A top-level **`def main()`** is the entry point of a script or tool — zero ceremony.
- A long-running service uses an **`Application`** with an instance `def main(self)` (see §6).
- Each file is a module, referenced by its **lowercase file name** (Pythonic):
  `from util import mathutil` imports `util/mathutil.zn`, then you call `mathutil.fn(...)`.
  Same-directory modules need no import. (Classes/actors/records stay CapWords.)

```python
def main() {
    print("hello")
}
```

## 2. Values and types

Primitives: `int` (arbitrary-precision — no overflow), `double`, `boolean`, `String` (a
UTF-8 binary). Locals infer their type from assignment; params must be typed. f-strings are
explicit (`f"..."`), like Python.

```python
d = 7.0 / 2          # 3.5  (float division)
n = 7 / 2            # 3    (int division truncates)
name = "beam"
print(f"hi {name}, n={n}")    # hi beam, n=3
```

**Records** are immutable structs — fields set at construction, read with Pythonic attribute
access, never reassigned (mutable state lives in actors, §6):

```python
record Point(x: int, y: int)

p = Point(1, 2)
print(p.x + p.y)     # 3
```

**Enums** are atoms; **instance classes** (`class X(SomeInterface)`) are immutable value
objects with methods; **interfaces** (`interface`) are nominally checked and dispatch
dynamically; **lambdas** (`n -> n * 3` or `lambda n: n * 3`) satisfy single-method
interfaces. (`records`, `protocols`, `collections`, `match`)

## 3. Collections — Pythonic, with honest costs

Dict and list literals; subscript indexing; `len()` is the one length spelling.

```python
scores = {"a": 1, "b": 2}        # inferred HashMap<String,int>
scores["a"] = scores["a"] + 10   # subscript read + write, type-checked
print(scores["a"] + len(scores)) # 13

xs = [10, 20, 30]
print(xs[0] + len(xs))           # 13   (len works on strings, lists, maps, arrays)
```

Homogeneous literals are statically typed, so `scores["a"] + "s"` is a **compile error**.
Mixed-value dicts stay dynamic and need a typed crossing (`host: String = cfg["host"]`).
Under the hood zinc splits Java's `List` by *purpose*, because the lowering costs differ:

| type | use it to | backed by | costs |
|------|-----------|-----------|-------|
| `List<T>` | receive / iterate | Erlang list | `get`/`size` are O(n) (warned); for-each is fast |
| `ArrayList<T>` | build / index | Erlang `array` | `add`/`get`/`set` O(log n), `size` O(1) |
| `Map`/`HashMap` | key-value | Erlang map | O(log n) |

`d["k"]`/`d.get(k)`/`len(d)` all work; `byte[]` is a binary, `int[]` is the array module.
(`collections`, `dict`, `multifile`)

## 4. Control flow and errors

Classic `if` / `else if` / `else`, `for i in range(a, b)`, `for x in xs`, `while`,
`break`/`continue`, `match`/`case` (constants, bare enum labels, `case a, b`, `case _`), and
the ternary (`big if n > 5 else small`).

**`try`/`except` is transactional**: a caught `try` reverts the outer-variable mutations it
made before the raise (the BEAM can't observe partial bindings — when ergonomics and
safe-on-BEAM semantics conflict, zinc chooses safe). Exceptions are **unchecked**:

```python
class NotFound(Exception) {}

def main() {
    try {
        raise NotFound("no such id")
    } except NotFound as e {
        print(e.message)              # no such id   (str(e) also works)
    } except Exception as e {
        print("catch-all (also catches native BEAM errors)")
    }
}
```

(`trycatch`, `exceptions`, `match`, `ternary`)

## 5. The failure ladder

One model for everything that can go wrong:

1. **Expected failures are exceptions** — unwind to a typed `except`. Catch only what you have
   a plan for (bad input, network, external systems).
2. **A raise in an Actor call relays to the caller** (catchable there); the actor survives,
   state intact via the transactional `try`.
3. **Bugs crash the process** — a caller mid-call exits with the same reason (not catchable;
   no retry against broken state).
4. **Crashes hit supervision** — the tree restarts the domain (§6).
5. **Crash loops escalate** to the root, then the VM exits non-zero → systemd's problem.

Process granularity + supervision *is* the recovery story. You rarely write error handling;
you let it crash and be restarted.

## 6. Actors and Applications — the supervision model

This is the differentiator. Two base classes, both prelude (no import):

- **`(Actor)`** — a supervised, stateful process. Fields are state; methods are the protocol,
  accessed via `self`. **No return type ⇒ async cast; typed `-> T` ⇒ sync call** (the return
  type *is* the messaging contract). The instance reference is a handle that survives
  restarts. A method with no `self`-prefixed name is `public` (its message protocol); mark
  in-process helpers `_private`-style by keeping them out of the call surface — a private
  helper is a plain local function, not a message handler.
- **`(Application)`** — the one root per runnable service: an OTP application whose
  **Actor-typed fields are its supervised children**, born in declaration order. Hosts
  `main`, receives SIGTERM, owns the exit code.

**Construction spawns.** `Counter()` runs the constructor *inside* a freshly spawned process
and returns the handle. A field = a **static child** (permanent, restarted). One in a method
body = a **dynamic child** (temporary, dies with its owner). Composition *is* supervision —
an Actor's Actor-fields are its children; failure flows down, never up.

```python
class Counter(Actor) {
    count = 0
    def incr(self)       { self.count = self.count + 1 }   # cast
    def get(self) -> int { return self.count }             # call
}

class Main(Application) {
    c = Counter()                  # a permanent, supervised child
    def main(self) {
        self.c.incr()
        print(self.c.get())        # 1
    }
}
```

Crash it and the supervisor restarts it with the same handle — see the
[README hook](../README.md) and `selfheal`, `supervised`, `counter`.

A program exits when `main` returns **and** no actors are alive; a service with static
children runs until stopped. An Actor may declare `def close(self)` — run as `terminate` on
*orderly* stop only (resource cleanup).

## 7. I/O and streaming — bounded memory, 8 KB → 5 GB

The rule: **streaming is explicit; copying large files through memory is a non-starter.**
Three lifetimes:

**Whole-file (small):** one self-contained call, no handle.
```python
cfg = Files.readString("app.conf")
Files.writeString("out.txt", cfg)
```

**Scoped streaming (large, one pass):** a `Reader`/`Writer` held for a `with`-block — raw +
read-ahead, in-process, closed at block exit. Constant memory regardless of file size.
```python
with Files.openReader(src) as r {
    with Files.openWriter(dst) as w {
        while r.hasNextLine() {
            w.writeLine(r.nextLine().toUpperCase())   # one line resident at a time
        }
    }
}
```
`HttpStream` works identically for an HTTP body (demand-driven, so the producer can't flood
you): `with client.openStream(req) as s { ... }`.

**Cross-process pipeline (parallel + bounded):** a `Channel<T>` is a bounded backpressure
buffer between actors — `put` blocks when full, the consumer pulls. `FileReader.pump` /
`FileWriter.drain` are ready-made source/sink pumps (spawn-and-go); a transform stage is just
an actor that drains one channel and feeds the next:
```python
class Upper(Actor) {                                       # a transform stage
    def run(self, incoming: Channel<String>, outgoing: Channel<String>) {
        while incoming.hasNext() {
            outgoing.put(incoming.take().toUpperCase())
        }
        outgoing.close()
    }
}
# read -> transform -> write, three processes, paced by the bounded channels:
rawLines = Channel(64)                       # reader -> transform
upperLines = Channel(64)                     # transform -> writer
FileReader.pump(inputFile, rawLines)         # file -> rawLines (spawns a background reader)
up = Upper()
up.run(rawLines, upperLines)                 # cast, so it loops in up's own process
fw = FileWriter.drain(upperLines, outputFile)   # upperLines -> file (handle to join on)
fw.join()                                    # wait for the pipeline to finish
```
N workers can drain one `Channel` for automatic work-stealing + backpressure.
(`fileio`, `filestream`, `channel`, `pipeline`)

## 8. Standard library

- **`Json`** — derived record codecs (no reflection): `Json.encode(rec)`,
  `Json.decode(User, s)` (bare record name; `User.class` also works),
  `Json.decodeList(User, arrayJson)` → `List<User>`; plus dynamic access
  (`Json.parse(s).get("k").asInt()`) for foreign JSON. (`json`)
- **`zinc.http`** — a client: the `HttpClient.newBuilder()...send(req)` builder for full
  control, or the one-shot facade `http.get(url)` / `http.post(url, body)` / `put` / `delete`.
  Plus a server: an `HttpServer` Actor + a programmatic `Router` with `{id}` path params;
  handlers are lambdas. (`http_client`, `http_facade`; server in `dogfood/webdemo`)
- **`zinc.sql`** — a `Db` connection pool (a supervision subtree, an Application child);
  `db.query(sql, params...)`, `db.exec(...)`, lambda `db.transaction(tx -> {...})` (return =
  COMMIT, `raise` = ROLLBACK); always prepared statements. (Postgres; `sql`)
- **`Log`** — `Log.info/warn/error(msg)` → BEAM logger (where crash reports land);
  `print` stays clean stdout.

## 9. The FFI escape hatch and dependencies

Day-to-day code is pure braces-Python. When you need a raw OTP/hex module, open the hatch:

```python
from erlang import lists        # binds the OTP `lists` module

sorted = lists.sort(xs)         # lowers to lists:sort(Xs); passthrough, unchecked
```

Add hex packages with `zc add cowboy@2.12.0`; declaring a dep fetches + builds it, and you
call it through the same FFI. The basement is unchecked by default, but **`zc check`** runs
xref + dialyzer over it (and your deps) on demand — calls to functions that exist nowhere,
bad arity, type-incompatible FFI. (`ffi`)

## 10. Generics and the type net

Generic annotations are **kept** — `Channel<String>`, `List<int>`, `Map<String,int>` carry
their element type, so `take()`/`get()`/`d["k"]` are typed. They're erased at lowering (the
BEAM has no reified types) but **gradually checked** at transpile time: known-vs-known
mismatch is an error, dynamic values (foreign JSON, mixed maps) flow freely until a guarded
crossing checks them at the boundary. Params must be typed; returns are checked.

**"Safe but no safer":** the net catches what corrupts data *silently* (type confusion across
boundaries, mixed-dict misuse). There is **no `Optional`/`None` ceremony** — a nil deref
crashes loud and the supervisor restarts it; that's the null story. (`veneer`)

---

Next: the **[Tutorials](tutorials.md)** build a real service; **[Coming from
Python](coming-from-python.md)** lists what's deliberately different.
