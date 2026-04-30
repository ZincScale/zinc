# zinc-crystal: planning document

> Target: Crystal 1.20.1+ (released 2026-04-16) as a new parallel transpilation
> target in the zinc family. Mirrors `zinc-go` architecturally, but leans on
> Crystal's *execution contexts* (RFC 0002) to deliver structured concurrency
> that goroutines cannot.
>
> Status: planning only. No code written yet.
> Author: drafted 2026-04-30.

---

## 0. Glossary

- **Zinc**: thin source-to-source transpiler that removes syntax warts from a
  target language. The .zn input is hand-editable; the emitted target source
  is also hand-editable.
- **Target**: a language we emit (currently Go, C#, Java, Python).
- **Parser**: target-independent. Lives in `internal/parser/`. Reused verbatim.
- **Codegen**: per-target, lives in `internal/codegen_<lang>/`.
- **Execution context** (Crystal): a scheduling boundary for fibers. Three
  variants exist as of 1.20: `Isolated` (single fiber, single OS thread, no
  intra-context concurrency), `SingleThreaded` (cooperative MT:1), `Parallel`
  (a.k.a. former `MultiThreaded` — N OS threads cooperatively scheduling a
  fiber pool). A fiber spawned inside a context is bound to that context.
- **Spindle / WaitGroup**: Crystal's primitives for joining fibers
  ([`WaitGroup`](https://crystal-lang.org/api/master/WaitGroup.html) is the
  shipped one).

---

## 1. Why Crystal — the thesis

### 1.1 The Go pain point we are escaping

Goroutines are *fire-and-forget*. The runtime gives the goroutine a stack and
forgets about it. Three concrete consequences hit zinc-go users routinely:

1. **Panic isolation is wrong by default.** A panic in a goroutine that is not
   recovered crashes the whole program at an unrelated stack frame, with no
   easy way to channel the panic back to the spawner. zinc-go's
   `examples/concurrency.zn` and `examples/workerpool.zn` both rely on the
   user knowing this.
2. **No "wait for all children".** WaitGroup must be threaded by hand. Cancel
   propagation requires `context.Context` plumbing through every call site.
3. **Leaks are silent.** A goroutine that blocks on a channel that nobody
   closes is not a compile error and not a runtime error.

### 1.2 What Crystal gives us

Crystal 1.20 ships **execution contexts** as a final preview
([release notes](https://crystal-lang.org/2026/04/16/1.20.0-released/)). Three
properties matter for zinc:

1. `Fiber::ExecutionContext::Isolated.new(name) { ... }` produces a handle
   whose `#wait` method **re-raises** any unhandled exception from inside —
   exactly the opposite of goroutine fire-and-forget
   ([docs](https://crystal-lang.org/api/master/Fiber/ExecutionContext/Isolated.html)).
2. `WaitGroup` has a block form (`WaitGroup.wait { ... }`) where every
   `spawn` inside the block is auto-counted. The block does not return until
   every spawned fiber has called `done`
   ([docs](https://crystal-lang.org/api/master/WaitGroup.html)). This *is*
   structured concurrency for the common case.
3. `Channel(T)` has the same `send` / `receive` / `close` shape Go has, plus
   `Channel.select` for the select-statement and `receive?` returning
   `T | Nil` on close — no second-return-value channel-of-zero-and-ok dance
   ([Channel(T) API](https://crystal-lang.org/api/1.18.2/Channel.html)).

### 1.3 What zinc-crystal explicitly buys us

| Concern              | zinc-go (today)                  | zinc-crystal (target)                     |
|----------------------|----------------------------------|-------------------------------------------|
| Spawn handle         | none — goroutine is invisible    | `Isolated` returns a handle with `#wait`  |
| Wait-for-all         | manual WaitGroup                 | `WaitGroup.wait { spawn ...; spawn ... }` |
| Panic propagation    | crashes program                  | Re-raised at `#wait` site                 |
| Cancellation         | manual `context.Context`         | `Channel(Nil)` close-as-cancel idiom      |
| Channel closed signal| `v, ok := <-ch`                  | `ch.receive?` → `T?`                      |
| Type-switch match    | type assertion + switch          | `case ... in` (exhaustive)                |
| Optional types       | `*T` (with footguns)             | `T?` first-class union                    |

### 1.4 Foundational rule: no fire-and-forget

This rule applies to every zinc transpilation target — it is *not* a
zinc-crystal convenience. **Every concurrent task in zinc must have a
structured owner that waits for or cancels it. There is no fire-and-forget
escape hatch.** Stated as a property of the language:

- A `spawn { ... }` is only legal *inside an owner scope*. The owner is
  one of: a `concurrent { }` block, a `parallel for` block, or a `task { }`
  expression that returns a handle the caller is responsible for waiting
  on. Top-level `spawn` outside any owner is a compile error.
- Channels likewise need a consumer with a known lifetime — a producer
  fiber writing to a channel that nobody reads is the same orphan
  pattern, and the same compile-time validation strategy applies (Phase 4
  lint, see §8.4).
- `concurrent { }` is the *primary* spawn site — not an optional
  convenience layered on top of bare `spawn`. The earlier sketch where
  bare `spawn` "kept Go-like fire-and-forget semantics" is wrong and is
  retracted; see §4.5 for the locked-in lowering.

This is recorded in user memory at
`feedback_no_fire_and_forget.md` and shapes:
- §3 (mismatch checklist) — the spawn entry no longer offers a fallback.
- §4.5 (concurrency lowering) — `concurrent { }` and `task { }` are the
  only forms.
- §6.2 (milestone order) — milestone 11 is renamed and there is no
  "spawn (bare)" pass.
- §11 (decisions) — decisions 2 and 3 are no longer open; both ship.
- Cross-target: zinc-go currently lowers `spawn` to bare `go func() {}()`,
  which is on the wrong side of this rule. Flagged as an existing
  inconsistency to fix on a future zinc-go pass; not blocking
  zinc-crystal.

### 1.5 Honest counter-weights (to flag in §10)

- **Slow compile times.** Medium projects (~30 KLOC) reportedly take ~3 min
  to compile and use ~2 GB of memory
  ([dev.to overview](https://dev.to/kojix2/why-is-crystal-compilation-so-slow-29n0)).
  zinc-crystal has no leverage over this.
- **Windows still preview** as of 1.20 — official ZIP/installer exists, but
  the install page still calls it preview
  ([install page](https://crystal-lang.org/install/on_windows/)). UNVERIFIED:
  whether all 1.20 features are present on Windows.
- **Ecosystem thinner than Go's.** No `go/types`-equivalent for resolving
  third-party shard signatures at transpile time.
- **Macro system is powerful but a footgun** — we will deliberately *not*
  emit macros from zinc; emitted Crystal stays plain.

---

## 2. Architecture

### 2.1 Directory layout

zinc-crystal lives as a sibling of zinc-go:

```
zinc/
  zinc-go/          (existing)
  zinc-csharp/      (existing)
  zinc-java/        (existing)
  zinc-python/      (existing)
  zinc-crystal/     (new)
    cmd/
      zinc/
        main.go
        compiler.go
        project.go
    internal/
      lexer/        (vendored from zinc-go, identical)
      parser/       (vendored from zinc-go, identical)
      errs/         (vendored from zinc-go, identical)
      codegen_cr/   (NEW — Crystal codegen)
        codegen.go
        codegen_types.go
        codegen_stmts.go
        codegen_exprs.go
        codegen_calls.go
        codegen_resolve.go
        crtypes.go     (declarative shard type info — see §5)
    examples/
    examples-fail/
    expected/
    stdlib/
    run_e2e.sh
    install.sh
    Makefile
    go.mod
    README.md
```

Decision: **vendor the parser by copy** initially, not by Go module. Rationale:
(a) zinc-go and zinc-crystal will each need parser tweaks at different times,
(b) the parser is ~4 KLOC, manageable; (c) no extra module wiring; (d) once
the dust settles we can hoist parser into `zinc-shared/parser` (Phase 6, out
of scope for this plan). The current targets (zinc-go, zinc-java,
zinc-csharp, zinc-python) all keep their own parser copy too — we mirror.

### 2.2 File-by-file responsibilities (mirrors zinc-go)

For reference, zinc-go sizes today are:

| File                         | Lines | Responsibility                                                     |
|------------------------------|------:|--------------------------------------------------------------------|
| `codegen.go`                 |   965 | `Generator` struct, init, `Generate` / `GenerateFiles` entry       |
| `codegen_types.go`           |  1284 | classes, data classes, sealed, enums, interfaces, fn decls         |
| `codegen_stmts.go`           |  2777 | var, assign, return, if/for/while/match, spawn, parallel, etc.     |
| `codegen_exprs.go`           |   940 | literals, calls, lambdas, string interp                            |
| `codegen_calls.go`           |   817 | call-site lowering: errors via `or { }`, FFI shape, default args   |
| `codegen_resolve.go`         |  1169 | scope/symbol resolution, shadowing, pub-ness                       |
| `gotypes.go`                 |   743 | go/types-driven resolver for third-party packages                  |

zinc-crystal targets roughly the same shape; expect `codegen_stmts.go` to
shrink (Crystal handles more lowering natively) and `crtypes.go` to be
**much** smaller because there is no `go/types` equivalent (see §5).

Initial size budget for `internal/codegen_cr/`:

| File              | Budget | Notes                                                          |
|-------------------|-------:|----------------------------------------------------------------|
| `codegen.go`      | ~ 800  | identical scaffolding, `recvName = "@"` (Crystal `@field`)     |
| `codegen_types.go`| ~1000  | uses Crystal `record`, `class`, `module`, abstract class       |
| `codegen_stmts.go`| ~2200  | most savings from native `case`, native `String?`, no `*T`     |
| `codegen_exprs.go`| ~ 900  | string interp uses `"#{...}"` — direct map                     |
| `codegen_calls.go`| ~ 600  | `or { }` → `begin/rescue` or `Result#or_else`                  |
| `codegen_resolve.go`| ~900 | similar logic to Go                                            |
| `crtypes.go`      | ~ 200  | declarative shard signature table (zinc.toml `[ffi.types]`)    |

### 2.3 Binary

`cmd/zinc/main.go` mirrors zinc-go's. The compiled binary is named `zinc-cr`
on disk (or `zinc` inside its own directory, matching zinc-go's pattern).
The same subcommands ship: `init`, `build`, `run`, `fmt` (a thin wrapper
around `crystal tool format`), `lint` (delegates to `ameba`).

Decision deferred to §10: whether to ship a unified `zinc` binary that
multiplexes targets by flag, or keep one binary per target as today. Plan
assumes per-target.

---

## 3. Phase 0 — feasibility spike

The single goal of Phase 0 is to **prove the syntactic mapping by hand** on a
representative subset of zinc-go's examples, before writing any codegen.

### 3.1 Five representative .zn files to hand-translate

For each, write the expected Crystal output by hand and check it compiles
(`crystal build --no-codegen`) on a Crystal 1.20.1 install. Treat any mismatch
as a Phase-0 risk to escalate.

| .zn file                           | Why representative                                  |
|------------------------------------|-----------------------------------------------------|
| `examples/hello.zn`                | smoke — print, top-level statements                 |
| `examples/sealed.zn`               | sealed class + `match` + data records               |
| `examples/workerpool.zn`           | spawn + Channel + buffered ops                      |
| `examples/sync_field_init.zn`      | class field defaults, mutex                         |
| `examples/error_explicit.zn`       | `(T, error)` thrower + `or { }`                     |

### 3.2 Phase-0 deliverable

A `phase0/` scratch directory under `zinc-crystal/` with:

- five .zn files copied verbatim from zinc-go
- five hand-written .cr files alongside
- a `MISMATCH.md` file listing every place where the natural Crystal version
  diverges from a 1:1 transform of the .zn — these are codegen rules that
  Phase 1 must encode

### 3.3 Mismatch checklist (anticipated, to refine in Phase 0)

- `String?` lowers to Crystal `String?` directly (good — *no* pointer dance,
  unlike Go where we autobox to `*string`)
- `data Circle(double r)` lowers to `record Circle, r : Float64` —
  but zinc data classes also auto-emit a `String()` method; the Crystal
  `record` macro auto-emits `to_s`, *and* the format differs.
  Mismatch: zinc-go emits `Circle(radius=5)`. Crystal `record`'s default
  `to_s` emits `Circle(@radius=5)`. We must override `to_s` to match the
  zinc-go output exactly, or change the spec.
- `Channel<T>(N)` → `Channel(T).new(N)`. Buffered. Direct.
- `spawn { ... }` is only legal *inside an owner scope* — see §1.4. There is
  no top-level fire-and-forget lowering. The owner is either a
  `concurrent { }` block (ownership = the surrounding `WaitGroup`) or a
  `task { }` expression (ownership = the returned handle, which the caller
  must `.wait()`). Outside both, `spawn` is a compile error. See §4.5 for
  the full mapping. Note: zinc-go currently lowers `spawn` to bare
  `go func() {}()` — that is the inconsistency §1.4 flags; not the
  zinc-crystal target.
- `lock (mu) { ... }` → `mu.synchronize { ... }` (Crystal `Mutex#synchronize`).
- `match (s) { case Circle(r) { ... } }` → Crystal `case s in Circle; ... end`,
  with `r = s.radius` introduced inside. Crystal `case in` is exhaustive,
  matching zinc semantics.
- `String? a = null` → `a : String? = nil`.

---

## 4. Phase 1 — core transpiler

### 4.1 Type lowering table

| zinc type             | Crystal type                                | Notes                                                                       |
|-----------------------|---------------------------------------------|-----------------------------------------------------------------------------|
| `int`                 | `Int32`                                     | matches Go's `int` choice; can revisit                                      |
| `long`                | `Int64`                                     |                                                                             |
| `byte`                | `UInt8`                                     |                                                                             |
| `double`              | `Float64`                                   |                                                                             |
| `float`               | `Float32`                                   |                                                                             |
| `boolean`             | `Bool`                                      |                                                                             |
| `String`              | `String`                                    | direct                                                                      |
| `void`                | `Nil`                                       | function with no return becomes `: Nil`                                     |
| `T?`                  | `T?` (`T \| Nil`)                           | first-class — unlike Go's `*T`                                              |
| `List<T>`             | `Array(T)`                                  |                                                                             |
| `Map<K,V>`            | `Hash(K, V)`                                |                                                                             |
| `Set<T>`              | `Set(T)`                                    |                                                                             |
| `Channel<T>`          | `Channel(T)`                                | buffered ctor: `Channel(T).new(n)`                                          |
| `Tuple<A,B>`          | `Tuple(A, B)`                               | also written `{A, B}` literal                                               |
| `(A, B) → C`          | `Proc(A, B, C)`                             | function type                                                               |
| sized `int[N]`        | `StaticArray(Int32, N)`                     | UNVERIFIED behavior parity — confirm in Phase 0                             |
| `error` (built-in)    | `Exception`                                 | the trailing-error tuple `(T, error)` lowers to either `T` + raise, or to a `Result(T, Exception)` shim — see §4.6 |

UNVERIFIED: zinc currently has no `Long`/`Int64` literal suffix; check that
parser handles big ints correctly. (Out of scope for this plan.)

### 4.2 Class lowering

| zinc                                                 | Crystal                                              |
|------------------------------------------------------|------------------------------------------------------|
| `class Foo { ... }`                                  | `class Foo` ... `end`                                |
| `class Dog : Animal { ... }`                         | `class Dog < Animal` ... `end`                       |
| `pub String name`                                    | `getter name : String`                               |
| `String name` (private)                              | `@name : String` (no accessor)                       |
| `pub String name = "x"` (default)                    | `getter name : String = "x"`                         |
| `init(String n) { this.n = n }`                      | `def initialize(@name : String); end`                |
| `data User(String name, int age = 0)`                | `record User, name : String, age : Int32 = 0`        |
| `sealed class Shape { data Circle(double r) ... }`   | abstract class + nested records — see §4.3           |
| `interface Greeter { String greet(String) }`         | `module Greeter; abstract def greet(s : String) : String; end` |
| Generic class `Box<T>`                               | `class Box(T)`                                       |
| Generic interface `Mapper<A, B>`                     | `module Mapper(A, B)` (with `abstract def`s)         |

**Visibility note**: zinc's `pub` is the only modifier. In Crystal, `private`
and `protected` are explicit and *default is public*. So zinc's "pub" does
nothing extra in Crystal *for methods*, but for fields we must emit `getter`
to match zinc's "pub field" semantics (read-only auto-accessor).

### 4.3 Sealed-class lowering — the tricky one

zinc:
```
sealed class Shape {
    data Circle(double radius)
    data Rect(double width, double height)
}
```

Crystal has no `sealed` keyword. Two viable encodings:

**Option A — abstract base + nested records (preferred):**
```crystal
abstract class Shape
end

class Shape::Circle < Shape
  getter radius : Float64
  def initialize(@radius : Float64); end
end

class Shape::Rect < Shape
  getter width : Float64
  getter height : Float64
  def initialize(@width : Float64, @height : Float64); end
end
```

Then `case s in Shape::Circle` produces the exhaustiveness check we want
(Crystal infers the union of subclasses statically when the abstract base
is closed-by-file). UNVERIFIED: whether Crystal's exhaustiveness across
abstract subclasses works without the (still-unmerged) `@[Sealed]` annotation
([RFC #9116](https://github.com/crystal-lang/crystal/issues/9116)). If not,
emit a trailing `else; raise "unreachable"; end` and accept the runtime check.
Mark in §10 as decision-to-revisit.

**Option B — alias of union of records:**
```crystal
record Circle, radius : Float64
record Rect, width : Float64, height : Float64
alias Shape = Circle | Rect
```

Cleaner output, but `Shape` is then not a type you can subclass, and
zinc allows `Shape s` parameters with method dispatch via match — still
fine. The tradeoff: shared methods on `Shape` (zinc supports adding
non-data methods to a sealed parent) require Option A. **Recommendation:
default to Option A; switch to Option B only when the sealed declaration
has zero non-data members.**

### 4.4 Statement lowering (top items)

| zinc                                              | Crystal                                                      |
|---------------------------------------------------|--------------------------------------------------------------|
| `var x = 1`                                       | `x = 1`                                                      |
| `int x = 1`                                       | `x : Int32 = 1`                                              |
| `String? a` (uninitialized)                       | `a : String? = nil`                                          |
| `if (c) { } else { }`                             | `if c; ...; else; ...; end`                                  |
| `while (c) { }`                                   | `while c; ...; end`                                          |
| `for (x in xs) { }`                               | `xs.each do \|x\| ... end`                                   |
| `for (i in 0..n) { }`                             | `(0...n).each do \|i\| ... end` (zinc `..` is half-open)     |
| `for (k, v in m) { }`                             | `m.each do \|k, v\| ... end`                                 |
| `match (s) { case Circle(r) { ... } }`            | `case s in Circle; r = s.radius; ...; end`                   |
| `lock (mu) { ... }`                               | `mu.synchronize { ... }` (assumes `mu : Mutex`)              |
| `defer { close() }`                               | wrap caller in `begin; ...; ensure; close; end` — see §4.7  |
| `assert(x > 0)`                                   | `raise "assertion failed" unless x > 0`                      |
| `print("...")`                                    | `puts "..."`                                                 |
| `return`                                          | `return` (or fall-through if last expression)                |
| `spawn { ... }`                                   | see §4.5                                                     |
| `parallel for (x in xs) { body }`                 | see §4.5                                                     |
| `select { case x = ch.recv(): ... }`              | see §4.5                                                     |
| `timeout(d) { body } or { fb }`                   | see §4.5                                                     |

### 4.5 Concurrency lowering — full table (the thesis cashed out)

The foundational rule from §1.4 is: every spawn has a structured owner. In
zinc-crystal that owner is one of two shapes — `concurrent { }` (block
ownership) or `task { }` (handle ownership). There is no third form. A
bare `spawn { ... }` outside both is a **compile error**, not an alternate
lowering.

#### `spawn { ... }` — only legal inside an owner

zinc rule:
- `spawn` may appear *only* lexically inside a `concurrent { }` block, a
  `parallel for` block, or as the body of a `task { ... }` expression.
- The validator walks the AST and flags any `SpawnStmt` whose enclosing
  scope chain contains no owner. Error: `"spawn { } must be inside
  concurrent { }, parallel for, or task { }; bare spawn has no owner."`
- This is symmetric to the `&` FFI-only rule (`feedback_no_fire_and_forget`
  + the recently shipped explicit-`&` validator). Both restrict a primitive
  to specific syntactic positions; both are enforced post-parse.

#### `concurrent { ... }` — block-owned spawn (THE primary form)

zinc:
```
concurrent {
    spawn { a() }
    spawn { b() }
    spawn { c() }
}
// after this block, all three have completed; any uncaught exception re-raises here
```
Crystal (we emit `wg.spawn` consistently — the conservative form):
```crystal
WaitGroup.wait do |wg|
  wg.spawn do
    a
  end
  wg.spawn do
    b
  end
  wg.spawn do
    c
  end
end
```

UNVERIFIED only insofar as Crystal 1.20 may also accept bare `spawn` inside
the block as auto-counted. We don't rely on that — `wg.spawn` is documented
and works regardless. Phase 0 confirms.

#### `parallel for (x in xs) { body }`

zinc-go lowers this to a WaitGroup of goroutines. zinc-crystal:
```crystal
WaitGroup.wait do |wg|
  xs.each do |x|
    wg.spawn do
      # body
    end
  end
end
```

Same primitive as `concurrent { }`.

#### `task { ... }` — handle-owned spawn (the OTHER legal form, ships in Phase 1)

The handle returned by `task { ... }` is itself the owner. The caller is
responsible for calling `.wait()` (or `.cancel()`) on it. Without a
handle, there is no owner, hence no fire-and-forget escape.

zinc:
```
var t = task { fetchUrl(url) }
// ... later
var result = t.wait()
```
Crystal:
```crystal
t = Fiber::ExecutionContext::Isolated.new("task") do
  fetch_url(url)
end
result = t.wait
```

The Crystal `Isolated` context's `#wait` re-raises unhandled exceptions —
exactly the semantics we want.

This was previously deferred to Phase 2 / marked optional. With §1.4 in
force, **`task { }` ships in Phase 1** alongside `concurrent { }`. Without
both forms, structured concurrency forces every spawn into a single block
shape, which is too inflexible for real programs (e.g. background tasks
whose lifetime exceeds the surrounding statement). Decision §11.3 is
locked.

Open follow-up (lint, not Phase 1 blocker): a `task` handle that is never
`.wait()`-ed and falls out of scope is the same orphan pattern bare
`spawn` is. Add an Ameba-side check `Zinc/UnwaitedTask` in Phase 3
(§8.4).

#### Channel ops

| zinc                       | Crystal                                                    |
|----------------------------|------------------------------------------------------------|
| `Channel<T>(N)`            | `Channel(T).new(N)`                                        |
| `Channel<T>()`             | `Channel(T).new` (unbuffered)                              |
| `ch.send(v)`               | `ch.send(v)`                                               |
| `ch.recv()`                | `ch.receive`                                               |
| `ch.close()`               | `ch.close`                                                 |
| `for (v in ch) { }`        | `loop { v = ch.receive? || break; ... }`                   |
| close-aware recv           | `ch.receive?` returns `T?`                                 |

#### `select { ... }`

zinc:
```
select {
    case x = ch1.recv():
        use(x)
    case ch2.send("v"):
        ok()
    case _:
        fallback()
}
```
Crystal:
```crystal
case Channel.select({ch1.receive_select_action, ch2.send_select_action("v")})
in {0, value}
  x = value.as(Int32)
  use(x)
in {1, _}
  ok
end
```

UNVERIFIED: that's the low-level shape. Crystal also has higher-level
`Channel.receive_first` / `Channel.send_first`. Phase-0 must confirm which
one we generate. The default-case `_:` arm has no clean Crystal
equivalent — it requires `Channel.non_blocking_select` patterns which differ.
Mark in §10.

#### `timeout(d) { body } or { fallback }`

zinc:
```
timeout(50.milliseconds) {
    work()
} or {
    print("timed out")
}
```
Crystal:
```crystal
done = Channel(Nil).new
spawn do
  work
  done.send(nil)
end
select
when done.receive
  # body finished
when timeout(50.milliseconds)
  puts "timed out"
end
```

(Crystal has a `select`/`when timeout(d)` shorthand introduced in 1.x;
Phase-0 to verify exact syntax.)

#### Cancellation

Crystal does not have first-class cancellation tokens. The idiomatic pattern
is **close a `Channel(Nil)` to broadcast cancel**, and every long-running
fiber selects on `cancel.receive?` plus its real work. We document this
pattern but do not make it a zinc keyword in Phase 1 — flag in §10 whether
zinc should grow `cancel` semantics later.

UNVERIFIED: `Fiber.cancel` (referenced in
[issue #6450](https://github.com/crystal-lang/crystal/issues/6450)) — research
showed it's a prototype, not shipped. Plan assumes shipped 1.20 semantics
only.

### 4.6 Error handling — `or { }` lowering

zinc has *one* call-site error form:

```
var ok = parseNum("42") or { print("err: ${err}"); return }
```

The function signature carries an explicit `error` in its return type:
```
pub (Int, error) parseNum(String s) {
    if (s == "") { return ParseError("empty") }
    return 42, null
}
```

#### Lowering option A — Crystal exceptions (preferred)

The `error` tuple slot is dropped from the emitted Crystal signature.
Returning `ParseError(...)` lowers to `raise ParseError.new(...)`. The
function return type becomes just `Int32` (single-value case). The `or { }`
block lowers to `begin/rescue`:

```crystal
def parse_num(s : String) : Int32
  raise ParseError.new("empty") if s.empty?
  42
end

# call site
ok = begin
  parse_num("42")
rescue err : Exception
  puts "err: #{err}"
  return
end
```

Why this is preferred:
- Crystal's standard library is exception-based; raising is the idiomatic
  shape.
- Stack traces work for free.
- Generated code reads naturally; users hand-editing the .cr won't be
  surprised.

Why it's not free:
- Loses the "error is just a value" zinc model the user already paid for.
- For multi-value throwers `(Int, String, error)`, we have to lower to a
  Crystal tuple `{Int32, String}` in the success path and raise on error,
  but the caller does:
  ```
  var (n, s) = lookup("k") or { return }
  ```
  → must lower to a `begin ... rescue ... end` whose result is the
  destructured tuple.

#### Lowering option B — Result(T, E) shim (alternative)

Emit a `Result(T, E)` struct in a vendored helper module; throwers return
`Result(T, Exception)`; `or { }` lowers to `case x in Result::Err`.

Output is more uniform but verbose, and fights Crystal idiom. **Recommendation:
ship Option A; flag Option B for users who want pure-value error handling
later.**

### 4.7 `defer { ... }`

zinc-go uses Go's `defer`. Crystal has no `defer`. Lowering: rewrite the
*enclosing function body* to wrap in `begin ... ensure deferred_block end`.
Multiple defers stack with `ensure` blocks in reverse-declaration order
inside nested `begin`s.

### 4.8 String interpolation

zinc: `"hello ${name}, age ${age + 1}"`
Crystal: `"hello #{name}, age #{age + 1}"` — direct dollar→hash transform.
Trivial.

### 4.9 Imports / packages

zinc:
```
import strings
import fmt
import time
```

Crystal has no per-module import; everything compiles together. Crystal does
have `require` for files. zinc's `import` lowers to **nothing emitted in
the .cr file** — but zinc.toml declares deps that map to `shard.yml`
entries, and `src/main.cr` ends up with `require "./..."` for each emitted
.cr file, plus `require "shard_name"` for each shard dependency.

For zinc-go, `import strings` resolves to Go stdlib package `strings` and
calls become `strings.ToUpper(...)`. For zinc-crystal, the Go stdlib has no
direct mapping; we maintain a **declarative mapping table** in `crtypes.go`
(or a TOML asset) for the well-known shims:

| zinc `import`     | Crystal effect                                              |
|-------------------|-------------------------------------------------------------|
| `strings`         | no `require`; `strings.ToUpper(s)` → `s.upcase`             |
| `time`            | `Time` is built-in; `time.Sleep(x)` → `sleep x`             |
| `fmt`             | `fmt.Println(s)` → `puts s`                                 |
| `math`            | no `require`; `math.Sqrt(x)` → `Math.sqrt(x)`               |
| `os/exec`         | needs a vendored shim shard; flag as Phase-4                |
| `encoding/json`   | `JSON.parse` / `obj.to_json` — emit `require "json"`        |

This table is **declarative, not hard-coded** (per
`feedback_imports_not_hardcoded` in user memory). Stored in
`internal/codegen_cr/import_map.toml` and loaded at codegen init.

### 4.10 Field-init / pointerize regression equivalent

zinc-go has the `sync.Mutex` field auto-pointerize fix. In Crystal, `Mutex`
is a normal class; `@mu : Mutex = Mutex.new` works without ceremony. **No
equivalent fix needed** — this is one place Crystal is mechanically simpler.

---

## 5. Phase 4 deferred: type info

Move the discussion of FFI type info up here because it gates Phase 1.

zinc-go has `gotypes.go` (743 lines) using `go/types` to introspect
third-party Go packages and decide things like "should `pkg.T` lower as
`pkg.T` or `*pkg.T`?". Crystal has no equivalent stable API for shard
introspection. (`crystal tool dependencies` exists but does not give us
type signatures.) Three options:

1. **Don't introspect.** Treat every imported shard call as opaque; emit
   exactly what the user wrote. Works for almost all calls because Crystal
   is more uniform than Go (no `*T` vs `T` distinction at the call site,
   `getter` makes `obj.field` work everywhere). UNVERIFIED edge case:
   generic shard types where we need to know the type-arg count.
2. **Declarative type table in zinc.toml.** Add a section:
   ```toml
   [ffi.types]
   "json::JSON::Any" = "JSON::Any"
   "json::Parse" = "fn(String) -> JSON::Any"
   ```
   The user (or a community-shared file) declares signatures up front.
3. **Parse the shard's source.** Walk shard's .cr files and extract `def`
   signatures. Heavy lift; expensive at compile time.

**Recommendation: ship option 1 in Phase 1, option 2 in Phase 4 if needed.**
Phase 0 will validate that option 1 covers the example set. If not, escalate
to option 2.

---

## 6. Phase 1 — concrete implementation plan, file by file

### 6.1 Bootstrap order

1. Copy `zinc-go/internal/lexer/`, `zinc-go/internal/parser/`,
   `zinc-go/internal/errs/` into `zinc-crystal/internal/`.
2. Stub `zinc-crystal/internal/codegen_cr/codegen.go` with the `Generator`
   struct, `New()`, `Generate(prog *parser.Program) string`, and
   `GenerateFiles(prog, className) []OutputFile`. Returning empty output
   means `go build ./cmd/zinc/` succeeds.
3. Wire `zinc-crystal/cmd/zinc/{main.go,compiler.go,project.go}`. Mostly a
   port of zinc-go's: `compileFile()` calls `codegen.New().GenerateFiles()`,
   writes .cr files, runs `crystal build`.
4. First milestone: `zinc-cr build examples/hello.zn` produces a .cr file
   that prints "hello".

### 6.2 Implementation order inside `internal/codegen_cr/`

Milestones in this order, each green before moving on:

| # | Milestone                            | Examples that pass                              |
|---|--------------------------------------|--------------------------------------------------|
| 1 | top-level statements + `print`       | `hello.zn`                                       |
| 2 | primitive types, var, if, while, for | `control_flow.zn`, `arrays.zn`                   |
| 3 | function declarations                | `functions.zn`, `function_types.zn`              |
| 4 | classes + init + fields + methods    | `classes.zn`, `constructors.zn`, `inheritance.zn`|
| 5 | data classes + sealed                | `sealed.zn`, `types.zn`                          |
| 6 | enums                                | `enums.zn`                                       |
| 7 | nullable types `T?`                  | `nullable.zn`                                    |
| 8 | string interp + escape               | `interp_escape.zn`, `strings.zn`                 |
| 9 | match (case-in)                      | `exhaustive_match.zn`, `type_match.zn`           |
| 10| collections (List/Map/Set)           | `collections.zn`, `nested_generics.zn`           |
| 11| Channels (no spawn yet)              | `channels.zn` — producer/consumer in same fiber  |
| 12| `concurrent { }` + structured spawn  | NEW `structured_concurrency.zn`; port `concurrency.zn` |
| 13| `task { }` + handle.wait()           | NEW `isolated_task.zn`                            |
| 14| `parallel for`                       | `concurrency.zn` (parallel arm), `workerpool.zn` |
| 15| select + timeout                     | `select_stmt.zn`, `timeout_stmt.zn`              |
| 16| validator: bare-spawn rejection      | NEW fail-test `spawn_outside_owner.zn`            |
| 17| `or { }` error form                  | `error_explicit.zn` and friends                  |
| 18| imports + import_map.toml            | `imports.zn`                                     |
| 19| multi-file projects                  | `multifile`, `multipackage`                      |
| 20| third-party shards (zinc.toml)       | NEW `third_party_*` example                      |

Note milestone 11 explicitly does **not** include `spawn` — channels are
exercised via single-fiber sequential code (send N, receive N) so the
spawn-validation infrastructure is in place by milestone 12 before any
real concurrency lands. This keeps the validator from being optional.

### 6.3 Test-first protocol

For each milestone, port the corresponding `.zn` from zinc-go's
`examples/`, write the *expected* `.cr` output by hand under
`zinc-crystal/expected_cr/<name>.cr`, then write the codegen until
diff is clean. **The runtime expected output (`expected/<name>.txt`) is
copied verbatim from zinc-go.** Per
`feedback_no_test_removal`, never delete a test to make it pass.

### 6.4 Generator struct — anticipated fields

Most fields carry over verbatim from zinc-go's `Generator`. Renamed/changed:

| zinc-go field                     | zinc-crystal change                                       |
|-----------------------------------|------------------------------------------------------------|
| `recvName = "this"`               | `recvName = "@"` — Crystal accesses fields via `@field`   |
| `imports map[string]bool`         | repurposed: tracks `require` directives to emit at top    |
| `errorFuncs`                      | retained — used to decide raise-vs-return                 |
| `currentReturnType`               | retained — but no need for "zero values" since Crystal raises|
| `currentReturnIsTuple`            | retained — Crystal supports tuple returns                 |
| `currentFuncIsThrower`            | retained — drives raise-vs-return                         |
| `chainCounter`                    | retained — for stream-like fused operations               |
| `currentFieldGoName map[string]string` | renamed `currentFieldCrName`                          |
| `currentFields`/`currentMethods`  | retained                                                  |

**New fields:**

| Field                        | Purpose                                                    |
|------------------------------|------------------------------------------------------------|
| `concurrencyOwnerDepth int` | nonzero while inside any owner scope (`concurrent { }`, `parallel for`, `task { ... }` body); checked by the spawn validator. The validator's rule mirrors the explicit-`&` validator: `SpawnStmt` outside an owner scope → compile error. |
| `currentOwnerKind string`   | `"concurrent"` / `"parallel"` / `"task"` — drives lowering: `concurrent`/`parallel` emit `wg.spawn { }`; `task` emits the body of the `Isolated` ctor. |
| `wgVarName string`          | name of the surrounding `WaitGroup` var (default `"wg"`)    |
| `requireSet map[string]bool`| shards that need `require "shard_name"` at file top         |
| `importMap map[string]ImportRule` | parsed `import_map.toml`                              |

### 6.5 Codegen entry point sketch

The signature in pseudo-Go (real implementation lives in
`internal/codegen_cr/codegen.go`):

```go
type Generator struct {
    buf strings.Builder
    indent int
    className string
    imports map[string]bool        // already-emitted `require`s
    interfaces map[string]bool     // module names
    structs map[string]*parser.ClassDecl

    // Crystal-specific
    inConcurrentBlock bool
    wgVarName string
    importMap map[string]ImportRule

    // ... carried over from zinc-go
}

func New() *Generator { /* init maps, defaults */ }

func (g *Generator) GenerateFiles(p *parser.Program, className string) []OutputFile {
    // 1. emit `require` directives (computed from p.Imports + importMap)
    // 2. emit type decls (classes, sealed/abstract, modules, enums, records)
    // 3. emit fn decls
    // 4. if p.Stmts non-empty, wrap in `def __zinc_main; ...; end; __zinc_main`
    // 5. write to one output file: <className>.cr
    return []OutputFile{{Name: className + ".cr", Content: g.buf.String()}}
}
```

Note the **single-file output convention.** zinc-go emits one .go per .zn
plus a synthesized `package main`. Crystal does not need that — one .cr is
enough; if the project is multi-file, we generate `src/main.cr` that does
`require "./<other_module>"` for each.

---

## 7. Phase 2 — concurrency lowering deep dive

This phase is mostly already specified in §4.5. The work in Phase 2 is
the *new examples* that exercise execution contexts directly (the
zinc-go corpus has nothing for these). Required new example files:

### 7.1 New `examples/structured_concurrency.zn`

```
// Three workers run in parallel; main waits for all; if any panics,
// the panic re-raises here, not in some unrelated stack frame.

void main() {
    concurrent {
        spawn { print("a"); }
        spawn { print("b"); }
        spawn { print("c"); }
    }
    print("all done")
}
```

Expected Crystal:

```crystal
def main
  WaitGroup.wait do |wg|
    wg.spawn { puts "a" }
    wg.spawn { puts "b" }
    wg.spawn { puts "c" }
  end
  puts "all done"
end

main
```

### 7.2 New `examples/isolated_task.zn` (only if Phase-1 §4.5 task syntax ships)

```
String fetch(String url) {
    return "body of ${url}"
}

void main() {
    var t = task { fetch("https://x") }
    var body = t.wait()
    print(body)
}
```

Expected Crystal:

```crystal
def fetch(url : String) : String
  "body of #{url}"
end

def main
  t = Fiber::ExecutionContext::Isolated.new("task") do
    fetch("https://x")
  end
  body = t.wait
  puts body
end

main
```

### 7.2.1 New `examples-fail/spawn_outside_owner.zn`

Mirrors `examples-fail/addrof_outside_ffi.zn` from zinc-go — proves the
validator rejects bare spawn at compile time.

```
void main() {
    spawn { print("orphan"); }   // ERROR: no owner
}
```

`expected/spawn_outside_owner.txt`:
```
spawn { } must be inside concurrent { }, parallel for, or task { }; bare spawn has no owner.
```

### 7.3 New `examples/cancellation.zn`

Demonstrates the Channel(Nil)-as-cancel idiom. Even without a `cancel`
keyword, the user can build it:

```
void worker(Channel<Bool> cancel) {
    while (true) {
        select {
            case cancel.recv():
                return
            case _:
                // do work
        }
    }
}
```

Plus the corresponding hand-written Crystal output for visual verification.

### 7.4 The `workerpool.zn` port — concrete output

Input must change too — the existing zinc-go `examples/workerpool.zn` uses
bare `spawn` for the workers. With §1.4 in force, the workers must live
inside an owner scope. The natural rewrite wraps the whole pool in a
`concurrent { }`:

zinc (revised):
```
void main() {
    var jobs = Channel<String>(10)
    var results = Channel<String>(10)

    var tasks = ["fetch", "parse", "transform", "validate", "store"]
    for (t in tasks) { jobs.send(t) }

    concurrent {
        for (w in 0..3) {
            spawn {
                while (true) {
                    var job = jobs.recv()
                    if (job == "STOP") { return }
                    results.send("done: ${job}")
                }
            }
        }

        for (i in 0..3) { jobs.send("STOP") }

        spawn {
            for (i in 0..5) {
                print(results.recv())
            }
        }
    }

    print("Worker Pool OK")
}
```

Expected Crystal:

```crystal
def main
  jobs = Channel(String).new(10)
  results = Channel(String).new(10)

  tasks = ["fetch", "parse", "transform", "validate", "store"]
  tasks.each { |t| jobs.send(t) }

  WaitGroup.wait do |wg|
    3.times do |w|
      wg.spawn do
        loop do
          job = jobs.receive
          return if job == "STOP"
          results.send("done: #{job}")
        end
      end
    end

    3.times { |i| jobs.send("STOP") }

    wg.spawn do
      5.times { puts results.receive }
    end
  end

  puts "Worker Pool OK"
end

main
```

Notes:
- The Wave-A port (`examples/workerpool.zn`) for zinc-crystal uses this
  revised source. The original zinc-go file is left alone for now —
  zinc-go's "fix bare spawn" is a separate cross-target work item flagged
  in §1.4.
- `return` inside `loop` inside `spawn` exits the *fiber*, not `main` —
  matches Go's semantics. UNVERIFIED on Crystal: confirm `return` from a
  proc-block in `spawn` doesn't escape. If it does, lower to `break`.

---

## 8. Phase 3 — Ameba style ruleset

zinc-emitted .cr files must be lint-clean by default
(per zinc-go's `feedback_use_zinc_binary` — the emitted code is a product,
not a draft). We pin Ameba and ship a `.ameba.yml` template.

### 8.1 Ameba version

UNVERIFIED current version. Search results showed crystaldoc.info indexes
v1.0.0. We pin to the latest stable — **plan calls for `~> 1.x`** in
`shard.yml`, regenerated whenever Crystal moves majors. Phase-3 task:
verify the latest version on
[crystal-ameba/ameba releases](https://github.com/crystal-ameba/ameba/releases)
when starting Phase 3.

### 8.2 Categories (per
[Ameba README](https://github.com/crystal-ameba/ameba/blob/master/README.md))

| Category    | Stance for zinc output  | Rationale                                    |
|-------------|-------------------------|----------------------------------------------|
| Style       | strict (most enabled)   | Whole point: emitted code looks idiomatic    |
| Lint        | strict (all enabled)    | Catches real bugs we shouldn't emit          |
| Naming      | strict                  | We control casing — should be clean          |
| Performance | strict                  | We should not emit anti-patterns             |
| Metrics     | LOOSE                   | Generated code may have long methods         |

### 8.3 Specific rules to disable (anticipated)

These rules are likely to fight zinc-emitted output. Phase-3 confirms each
by running Ameba on the Phase-1 corpus.

| Rule                              | Reason to disable                                        |
|-----------------------------------|----------------------------------------------------------|
| `Style/RedundantBegin`            | We emit `begin/rescue/end` for every `or { }` — by design |
| `Metrics/CyclomaticComplexity`    | Generated `case in` for sealed unions can be long        |
| `Lint/UnusedArgument`             | We emit `_w` / `_i` for closure args zinc didn't bind    |
| `Style/RedundantNext`             | We emit explicit control flow, not implicit              |
| `Documentation/DocumentationAdmonition` | zinc source has no docstring conventions yet       |

### 8.4 Custom rules to write

Ameba custom rules ship as a separate shard with `Rule::Base` extension
([extension guide](https://crystal-ameba.github.io/2019/07/22/how-to-write-extension/)).
We may want one or two:

1. **`Zinc/NoAmbiguousChannelClose`** — flag a `Channel(T).close` followed by
   sends. zinc's `using_close.zn` example explicitly tests this; the rule
   makes the pattern an error in user-edited .cr.
2. **`Zinc/ExecutionContextOnly`** — flag bare `spawn { ... }` outside of
   either `WaitGroup.wait` or `Fiber::ExecutionContext::Isolated.new`,
   enforcing the §1.4 no-fire-and-forget rule on hand-edited .cr.
   Note: zinc itself rejects this at compile time via the spawn validator
   (§4.5). The Ameba rule guards the *post-edit* boundary — a user opens
   the .cr and pastes a Crystal-idiomatic `spawn` block.

3. **`Zinc/UnwaitedTask`** — flag a `Fiber::ExecutionContext::Isolated.new`
   whose returned handle is never `.wait`-ed nor stored in a long-lived
   field. The handle being orphaned is the same fire-and-forget pattern in
   another shape; this lint is the second half of enforcing §1.4 at the
   `task { }` form.

All three ship as a separate `zinc-ameba-rules` shard, depended on by every
zinc-generated project. Phase-3 blocking for `Zinc/ExecutionContextOnly` —
the core thesis dies in user-edited code without it. `Zinc/UnwaitedTask`
can land slightly later if scoping a robust dataflow check is hard.

### 8.5 `.ameba.yml` skeleton

Emitted into each generated project's `zinc-out/` directory:

```yaml
# Generated by zinc-crystal. Edit if you must, but rerunning `zinc build`
# will overwrite this file. Add overrides to .ameba.local.yml instead.

Globs:
  - "**/*.cr"

Excluded:
  - lib

Style/RedundantBegin:
  Enabled: false
Metrics/CyclomaticComplexity:
  Enabled: false
Lint/UnusedArgument:
  Enabled: false
Style/RedundantNext:
  Enabled: false
Documentation/DocumentationAdmonition:
  Enabled: false
```

UNVERIFIED: the exact rule names above — they match common Ameba conventions
but each must be confirmed against the live ruleset
([rules list](https://github.com/crystal-ameba/ameba#rules)). Phase-3
confirms-or-renames each.

### 8.6 Lint acceptance criterion

A Phase-3 milestone is green when **`ameba` exits 0** on every
`.cr` file emitted from `examples/`, with the above `.ameba.yml`. Any
remaining warning must either be fixed in codegen or get its rule added to
the disable list with rationale.

---

## 9. Phase 4 — FFI / shards / type info

### 9.1 zinc.toml → shard.yml mapping

zinc.toml today (zinc-go shape):
```toml
[project]
name = "myapp"
version = "0.1.0"
main = "main.zn"

[go]
version = "1.26"

[deps]
viper = "github.com/spf13/viper@v1.20.1"

[imports]
viper = "github.com/spf13/viper"
```

zinc-crystal-shaped:
```toml
[project]
name = "myapp"
version = "0.1.0"
main = "main.zn"

[crystal]
version = "1.20.1"

[deps]
db        = "https://github.com/crystal-lang/crystal-db@~> 0.13"
ameba     = "https://github.com/crystal-ameba/ameba@~> 1.0"

[imports]
# zinc `import db` resolves to Crystal `require "db"`
db = "db"
```

Generates `shard.yml`:
```yaml
name: myapp
version: 0.1.0

dependencies:
  db:
    git: https://github.com/crystal-lang/crystal-db
    version: ~> 0.13

development_dependencies:
  ameba:
    git: https://github.com/crystal-ameba/ameba
    version: ~> 1.0

targets:
  myapp:
    main: src/main.cr
```

(per the [shards spec](https://github.com/crystal-lang/shards/blob/master/docs/shard.yml.adoc)).

### 9.2 Type info — see §5

Phase 4 is where we *might* add `[ffi.types]` to zinc.toml, if Phase-1
empirics show option 1 isn't enough.

### 9.2.1 Server-shaped imports — fibers + execution contexts, with caveats

When `import_map.toml` covers a server-shaped library — HTTP server,
websocket server, RPC, anything with a `listen + handle each
connection` pattern — the lowering uses Crystal's native fiber +
execution-context idioms, not a literal translation of the source
target's API. **But** Crystal's stdlib `HTTP::Server` itself isn't
§1.4-compliant out of the box, which is worth being honest about.

What Crystal's `HTTP::Server` actually does (verified in
`stdlib/http/server.cr`):

```crystal
protected def dispatch(io)
  spawn handle_client(io)   # bare spawn, no owner
end
```

Each connection IS processed in its own fiber (true), but that fiber
is bare-spawned — not tracked, not waited on at shutdown. This is
exactly the fire-and-forget pattern §1.4 rejects, sitting inside
Crystal stdlib.

What zinc-crystal needs to emit instead:

  - `import http` / equivalent → a thin wrapper that subclasses
    `HTTP::Server` and overrides `dispatch` to use a `WaitGroup` (so
    shutdown waits for in-flight requests) and/or a
    `Fiber::ExecutionContext::Parallel` (for true multi-threading).
    Ship this in a `zinc-runtime` shard; user code just writes
    `import http`.
  - When zinc source has a hand-rolled server loop
    (`for { conn = listener.accept(); spawn { handle(conn) } }`),
    detection + rewrite to the wrapped server form. Pattern
    detection lands when the first real example needs it.

This connects to PLAN §13 risk #6 ("rule too strict for real
idioms"): Crystal stdlib `HTTP::Server` is exhibit A. zinc-crystal's
job is to provide a wrapped variant that *is* §1.4-compliant, so
users get the rule's guarantees on shutdown / error propagation
without writing the supervisor scaffold themselves.

Same shape applies to DB connection pools, message-queue consumers,
any "fiber-per-thing" server idiom: lower to a wrapped variant that
adds the missing ownership.

### 9.3 FFI escape hatch

Crystal has `lib LibC` for C bindings. Out of scope for zinc Phase 1–4;
out of scope for this plan entirely. Document as future work.

---

## 10. Phase 5 — examples + e2e

### 10.1 Port matrix

Of zinc-go's ~80 `examples/`, we port in three waves:

**Wave A (Phase 1, must-pass before declaring v0.1):** ~40 files
covering primitives, classes, sealed, generics, nullable, match,
collections, control flow, channels, spawn, select, errors, imports.
Each file has a corresponding `expected/<name>.txt` that must match
verbatim.

**Wave B (Phase 2, structured-concurrency-specific):** ~5 *new* files
specific to execution contexts (§7).

**Wave C (Phase 4, third-party):** the `third_party_pointer_slice` and
`multipackage` analogs, but pointing at real Crystal shards.

### 10.2 `run_e2e.sh` adaptation

zinc-go's [run_e2e.sh](file:///home/vrjoshi/proj/zinc/zinc-go/run_e2e.sh)
is reusable structurally. Replace:

| zinc-go                                      | zinc-crystal                                  |
|----------------------------------------------|-----------------------------------------------|
| `go build -o /tmp/zinc-go-bin ./cmd/zinc/`   | identical (still building a Go binary)        |
| `$ZINC_BIN run "$zn"`                        | identical                                     |
| Internally: emit .go, run `go run`           | emit .cr, run `crystal run`                   |

Sort-then-compare logic stays. Sort handles non-determinism in Crystal
hash iteration the same way it handles Go map iteration.

### 10.3 New asserts

- After every successful e2e run, also run `crystal tool format --check`
  on the emitted .cr — if `crystal tool format` would reformat it, our
  codegen is producing non-canonical output and that's a bug.
- Run `ameba` (the linter) and require exit 0.
- Run `crystal build --release` once on a meta-target that requires every
  example, to gate compile-time regressions (catches cases where one file
  alone compiles but the corpus together doesn't).

---

## 11. Open decisions (require user input before Phase 1 begins)

1. **Parallel target or primary target?** Does zinc-crystal aim to *replace*
   zinc-go for greenfield projects (because of structured concurrency), or
   live alongside it? Affects how aggressive we are about feature parity.
   *Recommendation: parallel for now; promote to primary if v0.1 lands clean.*

2. ~~**`concurrent { }` keyword.**~~ **LOCKED (no longer open):** ships in
   Phase 1 as the primary spawn site. Required by the §1.4 no-fire-and-forget
   rule — without an explicit owner block, every `spawn` needs an owner and
   we'd have nowhere to declare it. New zinc syntax.

3. ~~**`task { }` keyword for handle-returning spawn.**~~ **LOCKED:** ships
   in Phase 1 alongside `concurrent { }`. Without `task { }` the only
   spawn site is a block, which forces every concurrent operation into a
   single statement scope — too constraining for real programs (background
   work, request-scoped fibers whose lifetime exceeds a single block).
   The handle returned by `task { }` IS the owner — caller responsible
   for `.wait()` (Ameba lints unwaited handles in Phase 3).

4. **Sealed encoding — abstract class vs union alias.** Mixed sealed
   classes (data + non-data members) force option A; pure-data sealed
   classes can use option B. Should we always emit option A for
   uniformity? *Recommendation: always option A; revisit in v0.2.*

5. **Error-handling lowering — exceptions vs Result(T, E).** §4.6 picks
   exceptions (option A). Confirm. *Recommendation: option A, irreversible
   later only if we ship Result(T, E) as opt-in.*

6. **Match exhaustiveness fallback.** If Crystal's `case in` does not give
   us exhaustiveness for our sealed-as-abstract encoding, do we accept a
   runtime `else; raise` arm? *Recommendation: yes, accept the runtime arm
   and warn at zinc compile time when type-info couldn't prove
   exhaustiveness.*

7. **Linter rigor.** Do we treat ameba violations as build errors or
   warnings in the default `zinc build`? *Recommendation: warnings in
   `build`, errors in `build --strict`.*

8. **Ecosystem scope — bundled shards.** zinc-go has ad-hoc support for
   common Go stdlib packages via `import strings`. zinc-crystal needs the
   same shim list (per §4.9). Where does it live, and who maintains it?
   *Recommendation: ship a curated `import_map.toml` in
   `internal/codegen_cr/`, accept community PRs.*

9. **Windows.** Do we test Windows in CI for zinc-crystal v0.1? Crystal's
   Windows port is still preview. *Recommendation: no — Linux + macOS only
   for v0.1; revisit when Crystal declares Windows GA.*

10. **Compile-time strategy.** Crystal's compile times will hurt the
    `zinc run` workflow. Do we cache the compiled binary across runs?
    *Recommendation: yes — `zinc run` should write to `zinc-out/zinc-app`
    and skip recompile if mtimes match (already what zinc-go does with
    `go build`).*

11. **`for ... in 0..n` half-open vs inclusive.** zinc's `0..n` means
    "0 to n exclusive" (matches Go). Crystal's `..` is inclusive,
    `...` is exclusive. *Decision: we always emit `...` for zinc's `..`.
    Document in the codegen.*

---

## 12. Effort estimate

These are rough ranges, assuming one focused implementer with the user's
zinc fluency.

| Phase                                  | Estimate          |
|----------------------------------------|-------------------|
| Phase 0 — feasibility spike            | 3–5 days          |
| Phase 1 — core transpiler              | 3–4 weeks         |
| Phase 2 — concurrency lowering         | 1–2 weeks         |
| Phase 3 — Ameba ruleset + custom rules | 4–7 days          |
| Phase 4 — FFI/shards/zinc.toml         | 1 week            |
| Phase 5 — examples + e2e               | 1 week (parallel) |
| **Total to v0.1**                      | **~8–10 weeks**   |

For comparison, zinc-go reached this maturity over multiple months of
iteration; the estimate assumes the parser and learnings carry over.

---

## 13. Risks

1. **Compile time.** Crystal compiles slowly. A 500-line .zn file may take
   30+ seconds to validate end-to-end. The `zinc run` developer loop
   suffers. *Mitigation: aggressive caching in §11 decision 10, and a
   parser-only `zinc check` mode that skips Crystal compile.*

2. **Sealed-class exhaustiveness.** If Crystal's `case in` doesn't accept
   our abstract-base-with-subclasses encoding as exhaustive, we lose the
   one feature that motivates sealed classes. *Mitigation: §11 decision 6
   (runtime fallback) plus a Phase-0 spike that proves it on a real
   sealed example before Phase 1 begins.*

3. **Generic edge cases.** Crystal generics use `T` parameters at the
   class level (`class Box(T)`) but require explicit type unions or duck
   typing at the method level. zinc's generic methods may not lower
   cleanly. *Mitigation: the Phase-0 spike includes
   `examples/nested_user_generics.zn`.*

4. **Ecosystem thinness for FFI.** Zinc-go gets `import strings` for free
   from Go's stdlib. Several zinc-go examples use stdlib packages
   (`encoding/json`, `os/exec`, `fsnotify`) — we need shard equivalents
   and an import map for each. *Mitigation: Phase 4 ships only the import
   map needed by the example corpus; users add more declaratively.*

5. **Windows.** Out of scope for v0.1 per §11 decision 9. Documented as a
   risk because users may file bugs.

6. **No-fire-and-forget too strict.** Some real concurrency idioms — a
   detached background fiber, a long-running server's per-request worker
   that outlives the request scope — don't fit naturally into either
   `concurrent { }` or `task { }` with a bounded `.wait()`. The §1.4 rule
   bans them by design. *Mitigation:* the `task { }` handle form covers
   "lifetime exceeds my scope" (caller stores the handle in a field, waits
   later). For genuinely indefinite background work, the answer is a
   long-lived owner object (e.g. a `Server` class with a `shutdown()` that
   `.wait()`s its handles) — code patterns we should document in the
   language guide before users hit this and reach for an escape hatch.
   Re-evaluate after the first real zinc-crystal project hits the
   constraint.

---

## 14. Verification plan

A phase is "done" when:

| Phase | Done criterion                                                                                  |
|-------|-------------------------------------------------------------------------------------------------|
| 0     | All five hand-translated .cr files compile under Crystal 1.20.1; `MISMATCH.md` is complete      |
| 1     | Wave-A (~40) examples pass `run_e2e.sh` byte-for-byte; ≥ 90% of zinc-go's example corpus ports  |
| 2     | The three new structured-concurrency examples pass; `concurrent { }` ships                      |
| 3     | `ameba` exits 0 on every emitted .cr in the example corpus with the shipped `.ameba.yml`        |
| 4     | A real third-party shard example (e.g., `crystal-db`) e2e-passes via zinc.toml                  |
| 5     | `run_e2e.sh` reports PASS=N FAIL=0 SKIP≤2 across the whole port matrix                          |

Each phase milestone gets a tag in git.

---

## 15. Appendix — concrete code mapping reference

> The full transform table for all zinc constructs. The table is the
> spec; if codegen emits something not in this table, that's a bug.

### 15.1 Quick reference, condensed

| zinc                                       | Crystal                                              |
|--------------------------------------------|------------------------------------------------------|
| `void main() { }`                          | `def main; end` + trailing `main` invocation         |
| `int x = 1`                                | `x : Int32 = 1`                                      |
| `var x = 1`                                | `x = 1`                                              |
| `String? a`                                | `a : String? = nil`                                  |
| `List<int> xs = [1,2]`                     | `xs : Array(Int32) = [1, 2]`                         |
| `Map<String,int> m = {"a":1}`              | `m : Hash(String, Int32) = {"a" => 1}`               |
| `class Foo : Bar { }`                      | `class Foo < Bar; end`                               |
| `data User(String n, int a)`               | `record User, n : String, a : Int32`                 |
| `interface I { void f() }`                 | `module I; abstract def f : Nil; end`                |
| `enum Color { Red, Green }`                | `enum Color; Red; Green; end`                        |
| `if (c) { } else { }`                      | `if c; ...; else; ...; end`                          |
| `for (x in xs) { }`                        | `xs.each { \|x\| ... }`                              |
| `for (i in 0..n) { }`                      | `(0...n).each { \|i\| ... }`                         |
| `match (s) { case Circle(r) { ... } }`     | `case s in Shape::Circle; r = s.radius; ...; end`    |
| `lock (mu) { }`                            | `mu.synchronize { }`                                 |
| `print("x: ${x}")`                         | `puts "x: #{x}"`                                     |
| `Channel<T>(N)`                            | `Channel(T).new(N)`                                  |
| `ch.send(v)`                               | `ch.send(v)`                                         |
| `ch.recv()`                                | `ch.receive`                                         |
| `spawn { ... }` (bare, no owner)           | **compile error** — must be inside `concurrent { }`, `parallel for`, or `task { }` |
| `concurrent { spawn{a}; spawn{b} }`        | `WaitGroup.wait { \|wg\| wg.spawn{a}; wg.spawn{b} }` |
| `var t = task { f() }; t.wait()`           | `t = Fiber::ExecutionContext::Isolated.new("task") { f }; t.wait` |
| `parallel for (x in xs) { }`               | `WaitGroup.wait { \|wg\| xs.each {\|x\| wg.spawn{...}}}`|
| `select { case x = ch.recv(): ... }`       | `case Channel.select(...) in {0, v}; x = v.as(T); ...; end` |
| `timeout(d) { } or { }`                    | `select; when ...; when timeout(d); ...; end`        |
| `pub (Int, error) f(...)` body `return E`  | `def f(...) : Int32; raise E.new; end`               |
| `var ok = f(...) or { ... }`               | `ok = begin; f(...); rescue err; ...; end`           |
| `import strings`                           | (no emit) — calls rewritten via `import_map.toml`    |
| `defer { close() }`                        | function body wrapped in `begin; ...; ensure; close; end` |
| `assert(c)`                                | `raise "assertion failed" unless c`                  |

### 15.1.1 Worked examples — full transforms

Below are full input→output transforms for the trickiest constructs, so
codegen has unambiguous targets.

#### A. Sealed class with shared method

zinc:
```
sealed class Shape {
    data Circle(double radius)
    data Rect(double width, double height)

    String label() {
        return "shape"
    }
}

double area(Shape s) {
    match (s) {
        case Circle(r) { return 3.14159 * r * r }
        case Rect(w, h) { return w * h }
    }
    return 0.0
}
```

Crystal:
```crystal
abstract class Shape
  def label : String
    "shape"
  end
end

class Shape::Circle < Shape
  getter radius : Float64
  def initialize(@radius : Float64); end
end

class Shape::Rect < Shape
  getter width : Float64
  getter height : Float64
  def initialize(@width : Float64, @height : Float64); end
end

def area(s : Shape) : Float64
  case s
  in Shape::Circle
    r = s.radius
    3.14159 * r * r
  in Shape::Rect
    w = s.width
    h = s.height
    w * h
  end
end
```

UNVERIFIED: whether `case in` accepts `Shape::Circle` patterns and is
exhaustive without an `else` arm. Phase-0 must confirm; if not, append
`else; raise "unreachable"; end`.

#### B. Thrower function with `or { }` at the call site

zinc:
```
pub (Int, error) parseNum(String s) {
    if (s == "") { return ParseError("empty input") }
    return 42, null
}

void main() {
    var ok = parseNum("hi") or { print("caught: ${err}"); return }
    print("ok: ${ok}")
}
```

Crystal:
```crystal
class ParseError < Exception
end

def parse_num(s : String) : Int32
  raise ParseError.new("empty input") if s == ""
  42
end

def main
  ok = begin
    parse_num("hi")
  rescue err : Exception
    puts "caught: #{err.message}"
    return
  end
  puts "ok: #{ok}"
end

main
```

Notes on the lowering:
- The `(Int, error)` signature collapses to `: Int32`. The `error` slot is
  not a value; it's "this function may raise."
- The `null` in `return 42, null` becomes implicit — emitting only the
  value slot, no second slot.
- The `err` binding in the `or { }` block becomes `err : Exception` (or
  the user's specific error type if zinc's signature was `(Int, MyError)`).
- `${err}` becomes `#{err.message}` because zinc users expect string
  output, not the exception class repr. UNVERIFIED whether this is the
  right default; Phase-0 to confirm.

#### C. `concurrent { }` block — full transform

zinc:
```
import time

void main() {
    var counter = 0
    concurrent {
        spawn {
            time.Sleep(10 * time.Millisecond)
            print("a done")
        }
        spawn {
            time.Sleep(5 * time.Millisecond)
            print("b done")
        }
    }
    print("all children completed")
}
```

Crystal:
```crystal
def main
  counter = 0
  WaitGroup.wait do |wg|
    wg.spawn do
      sleep 10.milliseconds
      puts "a done"
    end
    wg.spawn do
      sleep 5.milliseconds
      puts "b done"
    end
  end
  puts "all children completed"
end

main
```

Codegen invariants:
- While `concurrencyOwnerDepth > 0` and `currentOwnerKind == "concurrent"`,
  every `spawn { ... }` emits as `wg.spawn do ... end`. On block exit, the
  depth decrements and the kind restores.
- The validator (run before codegen, mirroring the zinc-go explicit-`&`
  validator at `internal/codegen_go/codegen_exprs.go`) walks the AST and
  fails the build if any `SpawnStmt` is found outside an owner. Error text:
  `"spawn { } must be inside concurrent { }, parallel for, or task { }; bare spawn has no owner."`

#### D. select statement with timeout

zinc:
```
import time
void main() {
    var ch = Channel<String>(1)
    select {
        case msg = ch.recv():
            print("got: ${msg}")
        case time.After(50 * time.Millisecond).recv():
            print("timeout")
    }
}
```

Crystal:
```crystal
def main
  ch = Channel(String).new(1)
  select
  when msg = ch.receive
    puts "got: #{msg}"
  when timeout(50.milliseconds)
    puts "timeout"
  end
end

main
```

UNVERIFIED: Crystal's `select`/`when` shorthand syntax. If the live
language only supports `Channel.select` with explicit `_select_action`
methods, fall back to that lower-level shape (shown in §4.5).

#### E. Multi-file project

zinc layout:
```
src/main.zn
src/core/model.zn
src/services/greeter.zn
zinc.toml
```

Each .zn file contributes a Crystal source file. zinc-crystal emits:
```
zinc-out/
  src/
    main.cr
    core/
      model.cr
    services/
      greeter.cr
  shard.yml
  shard.lock          (after first `shards install`)
  .ameba.yml
```

`src/main.cr` begins with:
```crystal
require "./core/model"
require "./services/greeter"
```

Cross-package references in zinc (`core.User(...)`) lower to module-scoped
types (`Core::User.new(...)` if we emit `module Core` wrappers per
subpackage; or to plain `User.new(...)` if we use `require` only). Decision:
**emit `module Core` wrappers** — this preserves the namespace zinc
already enforced and avoids name collisions. Codegen rule: a subpackage
named `core` produces:
```crystal
module Core
  # contents of core/model.cr go here, indented one level
end
```

This requires the codegen to know "I am compiling a subpackage file" and
emit the wrapping module. Information already on the parser's `Program`
node via `Package` — same as zinc-go uses for `package core` emission.

### 15.2 Items in zinc-go without a clean Crystal mapping (open)

- `&p` explicit address-of (used for `json.Unmarshal(data, &p)`). Crystal
  has no address-of operator; `JSON.parse` returns a value. Lowering: the
  `&` becomes a no-op in Crystal output, but the call shape changes from
  `json.Unmarshal(data, &p)` to `p = JSON.parse(data).as(Person)` —
  this is a *call-site rewrite*, not a token-level transform. Codegen for
  `import encoding/json` must encode the rewrite specifically. **Open
  question — flag for §11 if more such rewrites surface.**

- `lock (mu) { ... }` for `sync.RWMutex` — Crystal has `Mutex` but no
  built-in RW mutex in stdlib. UNVERIFIED. If absent, we ship a
  `zinc-runtime/rwmutex.cr` shim shard.

- Tuple multi-return functions `(Int, String) f()` — Crystal supports
  returning a `Tuple(Int32, String)` and destructuring at the call site
  with `a, b = f()`. Direct.

- `===` reference equality — Crystal has `same?` for object identity
  (`a.same?(b)`). UNVERIFIED for value types — for `Int32` Crystal `==`
  *is* identity. zinc-go emits `==` for `===` on value types, `unsafe.Pointer`
  cast for ref types. zinc-crystal: `a.same?(b)` for objects, `a == b` for
  value types. Codegen needs to know which.

---

## 16. Sources

- [Crystal 1.20.0 release notes (2026-04-16)](https://crystal-lang.org/2026/04/16/1.20.0-released/)
- [Fiber::ExecutionContext::Isolated](https://crystal-lang.org/api/master/Fiber/ExecutionContext/Isolated.html)
- [Fiber::ExecutionContext::Parallel](https://crystal-lang.org/api/master/Fiber/ExecutionContext/Parallel.html)
- [WaitGroup](https://crystal-lang.org/api/master/WaitGroup.html)
- [Channel(T) API (1.18.2 stable shape)](https://crystal-lang.org/api/1.18.2/Channel.html)
- [Crystal `case` reference (1.19)](https://crystal-lang.org/reference/1.19/syntax_and_semantics/case.html)
- [Crystal Union types](https://crystal-lang.org/reference/1.20/syntax_and_semantics/union_types.html)
- [Crystal compile-time discussion](https://dev.to/kojix2/why-is-crystal-compilation-so-slow-29n0)
- [Crystal Windows install (preview)](https://crystal-lang.org/install/on_windows/)
- [shards spec — shard.yml](https://github.com/crystal-lang/shards/blob/master/docs/shard.yml.adoc)
- [Ameba README](https://github.com/crystal-ameba/ameba/blob/master/README.md)
- [Writing an Ameba extension](https://crystal-ameba.github.io/2019/07/22/how-to-write-extension/)
- [Ameba internals](https://crystal-ameba.github.io/2019/09/03/internals/)
- [RFC #6468 — Structured Concurrency](https://github.com/crystal-lang/crystal/issues/6468)
- [RFC #9116 — `@[Sealed]` annotation](https://github.com/crystal-lang/crystal/issues/9116)
- [RFC 0002 — Execution Contexts](https://github.com/crystal-lang/rfcs/blob/main/text/0002-execution-contexts.md)
- zinc-go source surveyed: `/home/vrjoshi/proj/zinc/zinc-go/internal/codegen_go/`
  (`codegen.go` 965 lines, `codegen_types.go` 1284, `codegen_stmts.go` 2777,
  `codegen_exprs.go` 940, `codegen_calls.go` 817, `codegen_resolve.go` 1169,
  `gotypes.go` 743), `internal/parser/` (4265 lines total),
  `cmd/zinc/{main.go,compiler.go,project.go}`, `run_e2e.sh`, and `examples/`
  (workerpool.zn, sync_field_init.zn, pointer_inference.zn, sealed.zn,
  classes.zn, types.zn, error_explicit.zn, channels.zn, concurrency.zn,
  select_stmt.zn, timeout_stmt.zn, nullable.zn, enums.zn, imports.zn,
  multipackage/).
