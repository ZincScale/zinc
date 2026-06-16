# The zinc guide

zinc is **legal Java that compiles to Erlang/OTP.** You write classes, types, semicolons,
`System.out.println` — and the transpiler emits readable Erlang that runs on the BEAM. The
deal, stated once: *Java syntax, marker-declared semantics.* Where the BEAM offers something
Java can't (supervised processes), you opt in with a **marker interface** (`implements Actor`)
— never a new keyword. Every `.zinc` file parses and type-checks under `javac`.

This guide is the whole surface, in order. Each piece has a runnable snippet; the file names
in parentheses are real, tested examples under [`examples/`](../examples).

---

## 1. Programs, files, packages

- One **public class per file**, file named after it. Directories are packages
  (`import util.MathUtil;` → `util/MathUtil.zinc`).
- The entry point is the class named **`Main`**. A tool/script uses
  `public static void main(String[] args)`; a long-running service uses an
  **`Application`** with an instance `void main()` (see §6).

```java
public class Main {
  public static void main(String[] args) {
    System.out.println("hello");
  }
}
```

## 2. Values and types

Primitives: `int`, `long` (an alias for `int` — BEAM integers are arbitrary-precision),
`double`, `boolean`, `String` (a UTF-8 binary). Casts truncate or widen as in Java; locals
are mutable, `final` is enforced.

```java
double d = 7.0 / 2;        // 3.5  (float division)
int    n = 7 / 2;          // 3    (int division truncates)
int    t = (int) 3.99;     // 3    (cast toward zero)
int    big = Long.parseLong("1000000000000");   // bignum, no overflow
```

**Records** are immutable structs — fields set at construction, never reassigned (mutable
state lives in actors, §6):

```java
record Point(int x, int y) {}
Point p = new Point(1, 2);
System.out.println(p.x() + p.y());   // 3
```

**Enums** are atoms; **instance classes** (`class X implements SomeInterface`) are immutable
value objects with methods; **interfaces** are nominally checked and dispatch dynamically;
**lambdas** satisfy single-method interfaces. (`structs`, `interfaces`, `switchenum`)

## 3. Collections — honest costs

zinc splits Java's `List` into two types by *purpose*, because the lowering costs differ:

| type | use it to | backed by | costs |
|------|-----------|-----------|-------|
| `List<T>` | receive / iterate | Erlang list | `get`/`size` are O(n) (warned); for-each is fast |
| `ArrayList<T>` | build / index | Erlang `array` | `add`/`get`/`set` O(log n), `size` O(1) |
| `Map`/`HashMap` | key-value | Erlang map | O(log n) |

`List.of(...)` / `List.copyOf(xs)` make immutable lists; build with an `ArrayList`, bridge
out with `List.copyOf`. Walk a map the Java way:

```java
Map<String, String> cfg = new HashMap<>();
cfg.put("port", "8080");
for (var e : cfg.entrySet()) {              // imperative, mutable locals
  System.out.println(e.getKey() + "=" + e.getValue());
}
cfg.forEach((k, v) -> System.out.println(k));   // or a side-effecting forEach
```

`byte[]` is a binary (`{72, 0, -1}` → 3 raw bytes); `int[]` is the fixed-size array module.
(`arraylist`, `mapiter`, `javacollections`, `proc_binary`)

## 4. Control flow and errors

Classic `if`/`else if`, `for` (classic + enhanced), `while`, `break`/`continue`, arrow
`switch` (constants, enum labels, `default`), and the ternary `?:`.

**`try`/`catch` is transactional**: a caught `try` reverts the outer-variable mutations it
made before the throw (the BEAM can't observe partial bindings — when Java and safe-on-BEAM
semantics conflict, zinc chooses safe). Exceptions are **unchecked** (`extends
RuntimeException`), so there are no `throws` clauses:

```java
class NotFound extends RuntimeException {
  public NotFound(String message) { super(message); }
}

try {
  if (id < 0) throw new NotFound("no such id");
} catch (NotFound e) {
  System.out.println(e.getMessage());     // no such id
} catch (Exception e) {
  System.out.println("catch-all (also catches native BEAM errors)");
}
```

(`trycatch`, `exceptions`, `switchenum`, `ternary`)

## 5. The failure ladder

One model for everything that can go wrong:

1. **Expected failures are exceptions** — Java-style unwind to a typed `catch`. Catch only
   what you have a plan for (bad input, network, external systems).
2. **A throw in an Actor call relays to the caller** (catchable there); the actor survives,
   state intact via the transactional `try`.
3. **Bugs crash the process** — a caller mid-call exits with the same reason (not catchable;
   no retry against broken state).
4. **Crashes hit supervision** — the tree restarts the domain (§6).
5. **Crash loops escalate** to the root, then the VM exits non-zero → systemd's problem.

Process granularity + supervision *is* the recovery story. You rarely write error handling;
you let it crash and be restarted.

## 6. Actors and Applications — the supervision model

This is the differentiator. Two marker interfaces, both prelude (no import):

- **`implements Actor`** — a supervised, stateful process. Fields are state; methods are the
  protocol. **`void` method ⇒ async cast; typed method ⇒ sync call** (the Java return type
  *is* the messaging contract). The instance reference is a handle that survives restarts.
  Methods take **no visibility modifier** — an Actor's methods *are* its public protocol, so
  `public` is the default (and `private` is rejected on them). That's why the snippets below
  write `void put(...)` / `User get(...)` with no `public`.
- **`implements Application`** — the one root per runnable project: an OTP application whose
  **`Actor`-typed fields are its supervised children**, born in declaration order. Hosts
  `main`, receives SIGTERM, owns the exit code.

**`new` spawns.** `new Counter()` runs the constructor *inside* a freshly spawned process and
returns the handle. A `new` in a field = a **static child** (permanent, restarted). A `new`
in a method body = a **dynamic child** (temporary, dies with its owner). Composition *is*
supervision — an Actor's Actor-fields are its children; failure flows down, never up.

```java
class Counter implements Actor {
  int count = 0;
  void incr() { count = count + 1; }   // cast
  int get()   { return count; }        // call
}

public class Main implements Application {
  Counter c = new Counter();           // a permanent, supervised child
  void main() {
    c.incr();
    System.out.println(c.get());        // 1
  }
}
```

Crash it and the supervisor restarts it with the same handle — see the
[README hook](../README.md) and `actor_selfheal`, `actor_children`, `actor_counter`.

A program exits when `main` returns **and** no actors are alive; a service with static
children runs until stopped. An Actor may declare `public void close()` — run as `terminate`
on *orderly* stop only (resource cleanup; `close`).

## 7. I/O and streaming — bounded memory, 8 KB → 5 GB

The rule: **streaming is explicit; copying large files through memory is a non-starter.**
Three lifetimes:

**Whole-file (small):** one self-contained call, no handle.
```java
String cfg = Files.readString("app.conf");
Files.writeString("out.txt", cfg);
```

**Scoped streaming (large, one pass):** a `Reader`/`Writer` held for a `try`-block — raw +
read-ahead, in-process, closed at block exit. Constant memory regardless of file size.
```java
try (Reader r = Files.openReader(in)) {
  try (Writer w = Files.openWriter(out)) {
    while (r.hasNextLine()) {
      w.writeLine(r.nextLine().toUpperCase());   // one line resident at a time
    }
  }
}
```
`HttpStream` works identically for an HTTP body (demand-driven, so the producer can't flood
you):
```java
try (HttpStream s = client.openStream(req)) {
  try (Writer w = Files.openWriter(path)) {
    while (s.hasNextChunk()) w.write(s.nextChunk());   // HTTP -> file, bounded
  }
}
```

**Cross-process pipeline (parallel + bounded):** a `Channel<T>` is a bounded backpressure
buffer between actors (NiFi connection / `BlockingQueue`) — `put` blocks when full, the
consumer pulls. `FileReader`/`FileWriter` are ready-made pump actors; a transform stage is
just an actor that drains one channel and feeds the next, run via a cast so it loops in its
own process:
```java
class Upper implements Actor {
  void run(Channel<String> in, Channel<String> out) {
    while (in.hasNext()) out.put(in.take().toUpperCase());   // the real work
    out.close();
  }
}
// read -> transform -> write, three processes, paced by the bounded channels:
Channel<String> a = new Channel<>(64);
Channel<String> b = new Channel<>(64);
new FileReader(in, a);                  // file -> a
Upper up = new Upper();                 // a spawn must bind to a var (then use the handle)
up.run(a, b);                           // a -> uppercase -> b; cast, so it loops in up's process
FileWriter fw = new FileWriter(b, out); // b -> file
fw.join();                              // wait for the pipeline to finish
```
N workers can drain one `Channel` for automatic work-stealing + backpressure.
(`fileio`, `filestream`, `filewrite`, `httpstream`, `channel`, `pipeline`)

## 8. Standard library

- **`Json`** — derived record codecs (no reflection): `Json.encode(rec)`,
  `Json.decode(User.class, s)`, `Json.decodeList(User.class, arrayJson)` → `List<User>`;
  plus dynamic access (`Json.parse(s).get("k").asInt()`) for foreign JSON. (`json`, `proc_json`)
- **`zinc.http`** — client (`HttpClient.newBuilder()...send(req)`) and a server: an
  `HttpServer` Actor + a programmatic `Router` with `{id}` path params; handlers are lambdas.
  (`http_client`)
- **`zinc.sql`** — a `Db` connection pool (a supervision subtree); `db.query(sql, params...)`,
  lambda `db.transaction(tx -> {...})`; always prepared statements. (Postgres.)
- **`Log`** — `Log.info/warn/error(msg)` → BEAM logger (where crash reports land);
  `System.out.println` stays clean stdout. (`logging`)

## 9. The FFI basement and dependencies

Day-to-day code is pure Java. When you need a raw OTP/hex module, open the basement:

```java
import erlang.lists;            // binds the OTP `lists` module
...
var sorted = lists.sort(xs);    // lowers to lists:sort(Xs); no arity check (unchecked)
```

A file using `import erlang.*` opts out of the legal-Java gate for that file — it's the
"Python at the boundary." Add hex packages with `zc add cowboy@2.12.0`; declaring a dep
fetches + builds it, and you call it through the same FFI. The basement is unchecked by
default, but **`zc check`** runs xref + dialyzer over it (and your deps) on demand — calls to
functions that exist nowhere, bad arity, type-incompatible FFI. (`ffi`, `atoms_tuples`)

## 10. Generics and modifiers

Generics are **erased** at lowering (the BEAM has no reified types, same as the JVM) but
**gradually checked** at transpile time: known-vs-known mismatch is an error, unknown (from
FFI / `Object`) flows freely, and runtime guards check where unknown crosses into a known
type. Modifiers are enforced, never decorative: `private` = same-class (not exported);
`final` = no reassignment; `static` required on utility `main`, rejected on actor methods;
`protected` is an error (no inheritance). (`guards`, `modifiers`)

---

Next: the **[Tutorials](tutorials.md)** build a real service and a pipeline; **[Coming from
Java](coming-from-java.md)** lists what's deliberately different.
