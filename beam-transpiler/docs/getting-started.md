# Getting started — braces-Python on the BEAM

Write Python-shaped code with braces and types. Get an Erlang/OTP service that supervises
itself, heals on crash, and streams in bounded memory — without writing a supervisor, a
`gen_server`, or a Dockerfile.

```python
def main() {
    print("Hello from braces-Python on BEAM!")
}
```

```
$ zc run hello.zn
Hello from braces-Python on BEAM!
```

That's the whole pitch: the ergonomics of Python (fast, to the point, no ceremony), the
reliability of the BEAM (multicore + supervision for free), and a real type checker so
production doesn't collapse the way interpreted languages let it.

`.zn` is the braces-Python surface. It transpiles to Erlang and runs on the BEAM.

---

## Install

One command lands the `zc` CLI plus a managed JRE and OTP runtime under `~/.zc` — no system
Java, no system Erlang, no Docker (the rustup model):

```sh
curl -fsSL https://github.com/ZincScale/zinc/releases/download/zc-v0.1.0/install.sh | sh
```

Check it:

```sh
zc doctor      # versions, toolchains, resolution
```

If you manage your own OTP, `ZC_SKIP_OTP=1` skips the bundled runtime; install one later with
`zc toolchain install`.

---

## Hello world (script mode)

A single `.zn` file with a top-level `def main()` is a runnable script — no project needed:

```python
# hello.zn
def main() {
    name = "world"
    print(f"Hello, {name}!")
}
```

```sh
zc run hello.zn      # Hello, world!
```

f-strings are explicit (`f"..."`), like Python. Top-level `def main()` is the entry point —
zero `public static void` ceremony.

---

## A real project

```sh
zc new --py myapp
cd myapp
zc run
```

`zc new --py` scaffolds a braces-Python project:

```
myapp/
  zinc.toml        # [project] name/version, [otp] version, [deps] hex packages
  src/main.zn      # def main() { ... }  — the entry point
```

Day-to-day commands:

```sh
zc run             # build + run main()
zc build           # transpile + compile (rebar3 under the hood)
zc test            # run test/**/*.zn
zc add cowboy@2.12.0   # add a hex dependency to zinc.toml
zc fmt src         # reindent by brace depth
zc release         # self-contained OTP release tarball (ERTS + beam + boot)
```

---

## The killer feature: actors + supervision, from your types

A class that extends `Actor` becomes a supervised `gen_server`. Its method return types
decide the messaging: **a typed return (`-> T`) is a synchronous call; no return type is an
async cast.** A class that extends `Application` is an OTP app whose supervision tree is read
straight off its fields.

```python
class Counter(Actor) {
    count = 0
    def incr()       { count = count + 1 }   # no return  => async (cast)
    def get() -> int { return count }             # typed      => sync (call)
    def boom()       { z = 0
                           count = count / z }    # a real crash
}

class Main(Application) {
    c = Counter()                  # a supervised child of the app
    def main() {
        c.incr()
        c.incr()
        c.incr()
        print(c.get())        # 3
        c.boom()              # the process crashes...
        Sys.sleep(100)             # ...the supervisor restarts it...
        print(c.get())        # 0  — same handle, fresh state
        c.incr()
        print(c.get())        # 1  — and it keeps serving
    }
}
```

You wrote no supervision code. The runtime built the `gen_server`, the supervisor, and the
restart strategy from `class C(Actor)` and the `Counter c` field.

---

## A tour of the surface

Everything below is real `.zn` that runs today (it's in `examples/py/`).

**Records, enums, match** — Pythonic attribute access (`p.x`), exhaustive `match`:

```python
record Point(x: int, y: int)
enum Color { RED, GREEN, BLUE }

def describe(c: Color) -> String {
    out = "cool"
    match c {                       # bare labels; `case a, b` and `case _` too
        case RED { out = "warm" }
    }
    return out
}
```

**Collections** — dict/list literals, subscript, and one length spelling `len()`:

```python
scores = {"a": 1, "b": 2}            # inferred HashMap<String,int>
scores["a"] = scores["a"] + 10       # subscript read + write, type-checked
print(scores["a"] + len(scores))     # 13

xs = [10, 20, 30]
print(len(xs))                       # 3   (works on strings, lists, maps)
```

Homogeneous literals are statically typed, so `scores["a"] + "s"` is a **compile error** —
the safety net interpreted languages don't give you. Mixed literals stay dynamic and require
a typed crossing (`host: String = cfg["host"]`).

**Errors** — `try/except`, `raise`, user exceptions, `str(e)` / `e.message`:

```python
class NotFound(Exception) {}

def lookup(id: int) -> int {
    if id > 10 { raise NotFound("no such id") }
    return id * 2
}

def main() {
    try { lookup(99) }
    except NotFound as e { print(e.message) }    # no such id
    print(str(42) + "!")                          # 42!  — str() on any value
}
```

**JSON** — derived record codecs, bare record name (no `.class`):

```python
record User(name: String, age: int)

u: User = Json.decode(User, "{\"name\":\"vin\",\"age\":40}")
print(u.name)                        # vin
```

**HTTP** — a one-shot facade (the builder stays for headers/timeouts):

```python
try {
    http.get("https://example.com")
} except HttpException as e {
    print("request failed")
}
```

**Streaming pipeline** — bounded-memory dataflow across processes via `Channel<T>` and the
file pumps. Each stage runs in its own process; the bounded channels give backpressure:

```python
class Upper(Actor) {
    def run(incoming: Channel<String>, outgoing: Channel<String>) {
        while incoming.hasNext() {
            outgoing.put(incoming.take().toUpperCase())
        }
        outgoing.close()
    }
}
# FileReader.pump(in, ch1) -> Upper.run(ch1, ch2) -> FileWriter.drain(ch2, out)
```

**SQL** — a connection pool that's an `Application` child; transactions ride the failure
ladder (return = COMMIT, `raise` = ROLLBACK):

```python
db = Db("postgres://user:pw@host:5432/db", 4)
db.exec("insert into users values ($1, $2)", "7", "vin")
rows = db.query("select id, name from users where id = $1", "7")
us: List<User> = Json.decodeAll(User, rows)
```

---

## What you get for free

- **Supervision & self-healing** — crashes are isolated and restarted; you write business
  logic, not OTP boilerplate.
- **Multicore** — actors are real BEAM processes, scheduled across all cores.
- **Bounded memory** — `Channel<T>` + the file pumps stream gigabytes without OOM.
- **Static safety** — typed params, checked returns, typed collections; dynamic values are
  quarantined behind guarded crossings that fail loud at the boundary, not silently 10 frames
  later. (There is no `Optional`/`None` ceremony — a nil deref crashes loud and the supervisor
  restarts; that's the null story.)

## Next

- `examples/py/` — every feature above as a runnable, tested `.zn` file.
- `dialects/zinc-python/docs/beam-target-plan.md` — the design doc (how each Python construct
  maps to a BEAM concept).
