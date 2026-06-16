# Coming from Java

zinc *is* legal Java — it compiles under `javac` — but it runs on the BEAM, so a handful of
semantics are deliberately different. When Java semantics and safe-on-BEAM semantics
conflict, zinc chooses safe. Here's what to know.

### Concurrency is actors, not threads
There is **no `Thread`, no `ExecutorService`, no `synchronized`, no shared mutable memory.**
You create a process with `new` on a class `implements Actor`; that's the only way. Mutable
state lives *inside* an actor (serialized by its mailbox); everywhere else, data is
immutable. Fan-out is the worker-actor idiom (kick with a cast, join with a call), and a
bounded `Channel` gives backpressure. (Thread pools mostly don't apply — BEAM processes are
microsecond-cheap, so spawn-per-task is normal.)

### Exceptions are unchecked, and `throw` relays
All exceptions `extends RuntimeException` — there are **no checked exceptions and no
`throws` clauses** (zinc failures auto-relay Erlang-style; a checked exception would force
`throws` up every call chain). Catch only what you have a plan for; everything else crashes
the process and the supervisor restarts it. A `throw` inside an actor's call-method relays to
the caller (catchable there); a bug crashes the process.

### `try`/`catch` is transactional
A caught `try` **reverts the outer-variable mutations** it made before the throw — the BEAM
can't observe partial bindings, so a half-finished `try` leaves no trace. Different from Java,
where mutations before a caught exception persist.

### `new` on an Actor spawns a process
For a class `implements Actor`, `new Counter()` doesn't make an object — it **spawns a
supervised process** and returns a handle. The constructor runs inside that process. A `new`
in a field is a permanent child; a `new` in a method body is temporary (dies with its owner).

### Records and objects are immutable
Record fields and instance-class fields are set at construction and never reassigned (no
setters, no `p.x = v`). Build a new value instead. **Locals stay fully mutable** (counters,
accumulators) — that's the SSA machinery. Mutable state that must change over time goes in an
actor.

### Collections split by cost
`List<T>` (receive/iterate; `get`/`size` are O(n) and warned) vs `ArrayList<T>`
(build/index; O(log n)). `List.of`/`List.copyOf` for immutable lists. `byte[]` is a binary,
`int[]` is the array module. `s += ...` is an amortized-O(1) binary append — **no
`StringBuilder` needed.**

### Small differences
- `Sys.sleep(ms)` instead of `Thread.sleep` (the latter's checked `InterruptedException`
  would need a `throws`).
- The entry class must be named **`Main`** (a service uses `implements Application` + instance
  `void main()`; a tool uses `static void main(String[] args)`).
- Atoms are `Tag.of("literal")`; your own constants are `enum`s (which lower to atoms).
- `long` is accepted but is just `int` (BEAM integers are bignums — no 64-bit overflow).
- IDE-managed reliability you'd normally hand-roll — retries, supervision, restart-on-crash —
  is the runtime's job, not yours.

### What's deferred (not in v1)
`finally` / try-with-resources beyond the scoped `Reader`/`Writer`/`HttpStream` handles,
exception hierarchies beyond one level, interface `default` methods, mutating sort, streams,
regex facade, multi-node distribution. Reach for the `import erlang.*` FFI basement if you
need something the stdlib doesn't cover yet.

---

Back to the **[guide](guide.md)** or **[tutorials](tutorials.md)**.
