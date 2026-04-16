# Zinc-Go v2 Pivot Plan — "Maintainable Go"

## North star

**The generated Go must be maintainable by a Go developer who has never heard of Zinc.**

This is the zinc-python philosophy applied to Go. Zinc is a wart-remover, not a
reskin. If a feature produces Go that a Go dev wouldn't write by hand — or
worse, would refuse to maintain — the feature is wrong, not the codegen.

The smell test, from an external Go reviewer:

> "If something transpiles to ugly complex Go, revisit the feature."

Zinc's value is in *removing Go's warts* (verbose error boilerplate, `defer`
noise, fiddly declarations), not in *grafting other languages' idioms* onto
Go (try/catch, stream chains, collection methods).

## Scope of this pivot

This is a pivot *of* the exception pivot. The try/catch/throw work shipped in
7c863d0 and its IIFE-wrapped codegen is the primary trigger. While we're
rebuilding the error model, three neighbouring warts came up in review; all
are in scope here because they share one principle and one sweep.

In scope:

1. **Error handling**: try/catch/throw → Result-shaped returns + `?` operator.
2. **Collection methods (streams)**: drop `.map/.filter/.reduce/.sortBy/...`
   chains. Go devs hand-roll loops.
3. **Map/collection view methods** (`.keys()`, `.values()`): revisit — Go devs
   use `for k := range m`.
4. **Variable declarations**: already mostly clean (`:=`), but audit the edge
   cases (package-level, zero-init, fields).

Explicitly *not* in scope (Go reviewer was happy with these):

- Classes, inheritance, `pub` visibility → structs + methods + embedding
- Pointer inference (auto-`&` insertion)
- `using` → `defer` (the one language-level wart-remover the reviewer loved)
- Sealed classes + `match` → interface + type switch
- Generics → Go 1.18+ type parameters
- Subpackages, smart imports, typed literals

---

## Verdict 1 — Error handling: `?` + Result, not try/catch

### Current state (the ugly)

Zinc source:

```zinc
int safeDiv(int a, int b, int fallback) {
    try {
        var r = divide(a, b)
        return r
    } catch (e) {
        return fallback
    }
}
```

Generated Go (actual output, trimmed):

```go
func safeDiv(a int, b int, fallback int) int {
    _tryerr_val, _tryerr_ret, _tryerr := func() (int, bool, error) {
        r, err1 := divide(a, b)
        if err1 != nil {
            return 0, false, err1
        }
        return r, true, nil
    }()
    if !_tryerr_ret && _tryerr != nil {
        e := _tryerr
        _ = e
        return fallback
    }
    if _tryerr_ret {
        return _tryerr_val
    }
    return 0
}
```

No Go dev would write this. The IIFE, the tri-state sentinel, the dead
fallthrough `return 0` — every line is a transpiler artefact. Diffing this
against what a Go dev would write is the whole motivation for the pivot.

### Target shape (the clean)

Zinc source (proposed):

```zinc
int safeDiv(int a, int b, int fallback) {
    var r = divide(a, b) or { return fallback }
    return r
}

// Propagation — `?` bubbles the error up the current frame.
int doubled(int a, int b) throws {
    var r = divide(a, b)?
    return r * 2
}
```

Generated Go:

```go
func safeDiv(a int, b int, fallback int) int {
    r, err := divide(a, b)
    if err != nil {
        return fallback
    }
    return r
}

func doubled(a int, b int) (int, error) {
    r, err := divide(a, b)
    if err != nil {
        return 0, err
    }
    return r * 2, nil
}
```

That's what a Go dev would write. Line-for-line.

### Language surface

- **`throws` in function signature** (or similar marker): function returns
  `(T, error)`. No marker → function returns `T` and cannot propagate.
- **`?` postfix operator**: on a call expression inside a `throws` function,
  unwraps the value or `return ..., err`s early. Composes naturally:
  `var x = foo()?.bar()?` — two early returns, one line.
- **`or { ... }` block**: already exists for error-only functions; generalise
  to value-returning functions. The block runs with `err` in scope and must
  either `return`, `throw`, or produce a fallback value (an expression form).
- **Typed error matching**: `or { err is DivisionException => return 0; ...}`
  or a small `match (err) { ... }` form. Transpiles to `errors.As`.
- **Error types**: any struct with an `Error() string` method. Already works;
  no change.
- **Constructors that fail**: `init(...) throws` → emit
  `NewFoo(...) (*Foo, error)`. Callers use `?` / `or { }` like any other
  throwing call.
- **`throw expr`**: sugar for `return zero, expr` in a `throws` function.
  Only legal inside `throws` functions; outside, it's a compile error.

### What goes away

- `try { } catch (T e) { } finally { }` — removed from the grammar.
- Panic/recover-based codegen — already replaced, nothing to remove.
- The IIFE sentinel pattern — dies with try/catch.
- `Exception` base class — optional; `error` interface is enough. Keep a
  lightweight `stdlib.exceptions.Exception` struct for users who want a
  pre-baked `Error()` impl, but don't require inheritance.

### Open questions

- **`finally`** — no direct replacement, and we are *not* adding one.
  `using` is the single cleanup primitive in Zinc. Cleanup that isn't
  naturally tied to a resource should be wrapped as one (a tiny type with a
  `close()` method). We deliberately do not expose `defer` in Zinc source —
  the reviewer was explicit: one idiomatic cleanup construct, not two.
  `defer` remains an output-side artefact that `using` lowers to, and Go
  readers recognise the codegen.
- **Multi-return beyond `(T, error)`** — Go supports `(a, b, err)`. Zinc
  currently has no tuple type. Do we add one, or stay with "at most one
  value + error"? **Proposal**: stay minimal, add later if demand appears.
- **`?` inside expressions vs statements** — restrict `?` to the top of a
  statement (like Rust 2018 pre-NLL), or allow it mid-expression? **Proposal**:
  allow mid-expression but require the containing function to be `throws`.

---

## Verdict 2 — Collection methods: drop them

### Current state

Zinc has a full stream API: `.filter`, `.map`, `.reduce`, `.sortBy`, `.skip`,
`.limit`, `.distinct`, `.anyMatch`, `.allMatch`, `.findFirst`, `.forEach`,
`.groupBy`, plus the `it` keyword and lambda loop-fusion.

### Why it goes

- Every stream op is transpiler-authored helper code or an inlined loop.
  Either way: generated Go that a Go dev wouldn't write.
- Lambda + loop-fusion optimisation is complex compiler surface with no
  payoff the target audience cares about.
- Go devs iterate. `for _, x := range xs { ... }` is the idiom. They
  *prefer* it to chains. This isn't Kotlin.
- Every stream method we keep is a feature we have to maintain, document,
  and debug codegen for — for a population that would rather we didn't.

### Target shape

Zinc source:

```zinc
List<int> evens = []
for (x in numbers) {
    if (x % 2 == 0) { evens.add(x) }
}

int total = 0
for (x in numbers) {
    if (x > 5) { total = total + x * 10 }
}
```

Generated Go:

```go
var evens []int
for _, x := range numbers {
    if x%2 == 0 {
        evens = append(evens, x)
    }
}

total := 0
for _, x := range numbers {
    if x > 5 {
        total += x * 10
    }
}
```

Exactly what a Go dev would write.

### What stays on collections

Simple one-liner sugar that transpiles to obvious Go:

| Zinc          | Go                  | Verdict |
|---------------|---------------------|---------|
| `list.add(x)` | `append(list, x)`   | keep    |
| `list.size()` / `list.length()` | `len(list)` | keep |
| `list[i]`     | `list[i]`           | keep    |
| `map.put(k,v)` | `m[k] = v`         | keep    |
| `map.get(k)`  | `m[k]`              | keep    |
| `map.contains(k)` | `_, ok := m[k]; ok` | keep (common) |
| `for (x in list)` | `for _, x := range list` | keep |
| `for (k, v in map)` | `for k, v := range m` | keep |
| `.filter/.map/.reduce/...` | IIFE or helper | **drop** |
| `map.keys()` | helper loop to build slice | **drop** — use `for k := range m` |
| `map.values()` | helper loop | **drop** |

### Migration impact

Nontrivial: `streams.zn`, the stream-heavy zinc-flow processors, and several
example programs. Every `.filter(...).map(...).sum()` chain becomes an
accumulating for-loop. It's mechanical but there's no automated rewrite.

---

## Verdict 3 — `var` → `:=`: already clean

Today this zinc:

```zinc
var x = 5
String y = "hi"
```

emits:

```go
x := 5
y := "hi"
```

for locals. Nothing to do for the common case.

### Edge cases to audit

- **Package-level declarations**: must emit `var x = 5` (Go doesn't allow
  `:=` at package scope). Check this is correct today.
- **Zero-initialised locals** (`String host` with no initializer): emit
  `var host string`, not `host := ""`. Check.
- **Struct fields**: no `var`/`:=` question, they live inside `type T struct`.
- **Loop variables**: `for i := 0; i < n; i++` — ensure `for` emits `:=`.
- **`const`** at local scope: Go allows `const x = 5` inside funcs but most
  devs use `:=` unless the constness matters. Keep `const` → `const`.

No language-surface changes expected. Audit-only unless something surprises.

---

## Verdict 4 — `using` is the single cleanup primitive

`using (var conn = acquire()) { ... }` → `defer conn.Close()`. Already
clean, and the reviewer specifically called this out as the right shape —
one source-level construct, and the Go it lowers to is instantly
recognisable.

**No `defer` exposure in Zinc source.** We deliberately reject adding a
bare `defer { }` statement, even though it would be a 1:1 codegen. Two
cleanup primitives is a wart; one is an idiom. Non-resource cleanup (emit
a metric on exit, flush a buffer, release a counter) should be wrapped as
a resource with a `close()` method and used the same way.

```zinc
class ExitMetric {
    String name
    init(String name) { this.name = name }
    pub close() { metrics.emit(this.name) }
}

using (var _ = ExitMetric("request.done")) {
    doWork()
}
```

Lowers to:

```go
func() {
    _ := NewExitMetric("request.done")
    defer _.Close()
    doWork()
}()
```

One construct, idiomatic output, no `defer` in the user's head.

---

## What the full language surface looks like after v2

```zinc
// Variables — bare, :=-shaped
var x = 5
String name = "Alice"

// Functions
int divide(int a, int b) throws {
    if (b == 0) { throw DivisionException("div by zero") }
    return a / b
}

// Error propagation — `?`
int doubled(int a, int b) throws {
    return divide(a, b)? * 2
}

// Error handling — `or { }` with err in scope
int safeDiv(int a, int b, int fallback) {
    return divide(a, b) or { return fallback }
}

// Resources and cleanup — `using` is the one primitive
using (var f = open("x.txt")) {
    f.write("hi")
}

// Non-resource cleanup: wrap it as a resource, use `using`. No `defer` in
// source — `using` lowers to `defer` on the output side and that's enough.

// Iteration — no streams
int sum = 0
for (x in numbers) {
    if (x > 5) { sum = sum + x }
}

// Everything else: classes, sealed+match, generics, pointer inference,
// subpackages, smart imports — unchanged.
```

## Phasing

**Phase A — Error pivot (breaking)**
1. Add `throws` marker to function signatures; typecheck.
2. Implement `?` postfix; codegen to `if err != nil { return ..., err }`.
3. Generalise `or { }` to value-returning calls; expose `err` in block scope.
4. Add typed error matching (`or { err is T => ... }` or `match(err)`).
5. Strip `try/catch/finally/throw` codegen + grammar. Keep `throw` keyword
   but retarget it to `return zero, err` shape inside `throws` functions.
6. Migrate constructors-that-throw to `NewFoo() (*Foo, error)`.
7. Rewrite `docs/error-handling.md` and the language-guide section.
8. e2e: rewrite `error_handling.zn`, `ctor_throw.zn`, `try_*.txt` goldens.

**Phase B — Streams removal (breaking)**
1. Remove `.filter/.map/.reduce/.sortBy/.skip/.limit/.distinct/.anyMatch/`
   `.allMatch/.findFirst/.forEach/.groupBy` from parser + codegen.
2. Remove `it` keyword and lambda loop-fusion pass.
3. Remove `map.keys()` / `map.values()` codegen.
4. Keep: `.add`, `.size`/`.length`, indexing, `.put`, `.get`, `.contains`.
5. Delete `streams.zn` example; keep iteration examples.
6. Rewrite the language-guide Streams section as "Iteration."

**Phase C — `var` audit (additive, small)**
1. Audit `var` codegen at package scope / zero-init / loop / field sites.
2. Fix any case that emits non-idiomatic Go.
3. No `defer` exposure — `using` stays the sole cleanup primitive. If any
   current zinc-flow site relies on `finally` for non-resource cleanup,
   rewrite as a resource type with `close()`.

**Phase D — zinc-flow migration**
1. Sweep zinc-flow for try/catch → `?`/`or`.
2. Sweep for stream chains → for-loops.
3. Re-run Phase 1 goldens; expect clean output.

**Phase E — docs + communication**
1. `language-guide.md`, `error-handling.md`, `classes.md` rewrites.
2. README "What zinc-go is / isn't" — hammer the "maintainable Go" point.
3. CHANGELOG with migration cookbook (before/after snippets).

## Re-entry checklist

- [ ] Design locked on `throws` keyword vs alternative (e.g. `!` suffix).
- [ ] Design locked on `or { }` expression vs statement form.
- [ ] Design locked on typed-error matching surface.
- [ ] `?` works on chained calls (`a()?.b()?`).
- [ ] Stream API removed from parser; `it` removed; loop-fusion pass deleted.
- [ ] `map.keys()` / `map.values()` removed.
- [ ] Zero `defer` tokens in Zinc source surface — `using` is the only
      cleanup primitive.
- [ ] All e2e tests pass on the new shape (no test removals).
- [ ] zinc-flow compiles clean on the new shape.
- [ ] Generated Go for three reference programs (error-handling,
      classes+inheritance, a zinc-flow processor) read by a Go dev without
      zinc context — no "wtf is this" moments.

## Principle

If a Go dev reads the generated output and thinks "I'd delete Zinc and
maintain this directly" — that's the bar. v2 is done when every feature
clears it.
