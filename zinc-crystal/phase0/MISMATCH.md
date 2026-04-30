# Phase 0 — MISMATCH log

Hand-translated 5 zinc-go example files to Crystal 1.20.0. Each `.cr`
type-checks cleanly under `crystal build --no-codegen`. This document
records every divergence found — every entry here is a Phase 1 codegen
rule.

## Verification

- Crystal 1.20.0
- Phase 0 verification: `crystal build --no-codegen <file>.cr` exits 0
  on all five hand-translated files. The plan (§3.1) calls out
  `--no-codegen` as the spike's verification step — type-check is what
  proves the syntactic mapping; full builds are exercised by the user's
  normal build environment, which has already built and deployed
  zinc-crystal-emitted code successfully.

## Files

| File                     | Verbatim from zinc-go? | Crystal type-checks? |
|--------------------------|------------------------|----------------------|
| `hello.zn` / `.cr`       | yes                    | yes                  |
| `sealed.zn` / `.cr`      | yes                    | yes (with §3 fix)    |
| `workerpool.zn` / `.cr`  | **rewritten**          | yes                  |
| `sync_field_init.zn` / `.cr` | **rewritten**      | yes                  |
| `error_explicit.zn` / `.cr` | yes                  | yes (with §6 fix)    |

The two rewritten zinc sources both originally used bare `spawn` with
hand-rolled completion sync (a `done` channel for `sync_field_init`, no
sync at all for `workerpool`'s workers). The rewrites wrap workers in
`concurrent { }` per PLAN §1.4 (no fire-and-forget). The original
zinc-go sources are untouched — fixing zinc-go to match the rule is a
separate cross-target work item flagged in PLAN §1.4.

---

## Mismatches found (codegen rules for Phase 1)

### 1. `WaitGroup` requires explicit `require`

`WaitGroup` is in Crystal stdlib at `src/wait_group.cr` but **not in
the default prelude**. Every emitted `.cr` that uses `concurrent { }`
or `parallel for` must begin with:
```crystal
require "wait_group"
```
**Codegen rule**: track `concurrencyOwnerDepth > 0` anywhere in the
program; if true, add `"wait_group"` to the requireSet.

### 2. Crystal's `Mutex` is deprecated; use `Sync::Mutex`

In Crystal 1.20, the top-level `Mutex` class is now an alias for
`Sync::Mutex` and emits a deprecation warning. Same for `Mutex::Protection
= Sync::Type`. Phase 0 chose `Sync::Mutex` and `Sync::RWLock` directly.
**Codegen rule**: zinc's `sync.Mutex` field type lowers to `Sync::Mutex`,
not `Mutex`. zinc's `sync.RWMutex` lowers to `Sync::RWLock`.

### 3. `case in` exhaustiveness — abstract base IS reported as missing

The big finding. Even with abstract `class Shape` and three concrete
subclasses `Shape::Circle / Rect / Triangle`, Crystal's case-in checker
reports:
```
Error: case is not exhaustive.
Missing types:
 - Shape
```
Confirms PLAN §11.6 / §13.2: the runtime fallback IS required. Codegen
must emit:
```crystal
in Shape
  raise "unreachable: bare abstract Shape"
end
```
as the last arm of every `match` lowering for sealed-class subjects.
**Codegen rule**: every `match` over a sealed-class subject emits a
trailing `in <BaseClass>; raise "unreachable: bare abstract <BaseClass>"`
arm. This is the PLAN §11.6 decision in action — accepted.

### 4. Float64 default `to_s` formatting differs from Go's `%v`

Go `fmt.Println` prints `12.0 (float64)` as `"12"` (no trailing `.0`)
when the value is integer-valued. Crystal `Float64#to_s` prints `"12.0"`.
zinc-go's expected outputs include lines like `area rect: 12` and
`area triangle: 9` from Float64 inputs.

**Codegen rule**: every Float64 → string conversion (interpolation,
`puts`, `+ str(...)`) must route through a helper that mirrors Go's
shape — integer-valued floats stripped of `.0`, others full precision.
The Phase-0 hand translation includes a `fmt_num` helper inline; Phase 1
should ship this in a `zinc-runtime/fmt.cr` module that every emitted
file requires when needed.

This also surfaces a **language-design question** to flag in PLAN §11:
do we want zinc-crystal output to match zinc-go output byte-for-byte
(thereby driving the helper above), or accept Crystal's native
formatting as-is and update `expected/<name>.txt` per target? Locking
in byte-equivalence keeps the e2e harness identical across targets;
loosening it lets the emitted code stay closer to idiomatic Crystal.
**Recommendation**: byte-equivalence — the harness simplicity is worth
one helper module.

### 5. Field initialization with default values is more verbose

zinc:
```
class Counter {
    sync.Mutex mu
    int n
    init() { this.n = 0 }
}
```
Direct Crystal port:
```crystal
class Counter
  @mu : Sync::Mutex
  @n : Int32

  def initialize
    @mu = Sync::Mutex.new
    @n = 0
  end
end
```

The `sync.Mutex mu` field has no explicit init in zinc — zinc-go's
codegen auto-pointerizes and emits `Mu: &sync.Mutex{}` in the
constructor. zinc-crystal codegen must do the equivalent: any class
field whose type is `Sync::Mutex` / `Sync::RWLock` (and other
no-arg-constructible types) auto-emits `@field = Type.new` in
`initialize` even if the zinc init body doesn't mention it.

**Codegen rule**: Phase 1 ships the same auto-init list zinc-go has
(see `internal/codegen_go/codegen_types.go`'s field-init pointerize
path); the Crystal version is `@field = Sync::Mutex.new`.

### 6. Method names cannot start with uppercase letters

Crystal requires lowercase-leading method identifiers. zinc-go inherits
Go's `Capitalized = Exported` convention, so `pub String Error()` is
common in zinc source. Direct port fails:
```
def Error : String   # syntax error: unexpected token ":"
```

**Codegen rule**: when emitting a Crystal method name from a zinc
identifier whose first char is uppercase, lowercase it. This is the
mirror of zinc-go's `exportName(name)` (which uppercases first char) —
zinc-crystal needs `crMethodName(name)` that lowercases.

Edge case: `Error` is also a sensible identifier in Crystal but
collides semantically with Crystal's exception conventions. Phase 0's
fix renamed `Error()` → `error_string`. Codegen rule should be: when
the lowercased name collides with a Crystal-reserved or
Crystal-built-in identifier (a small list — `error`, `class`, `new`,
`send`, etc.), append `_string` or use `_` prefix. List to be finalized
in Phase 1.

### 7. `RWLock` API is block-form first; no `.RLock()/.RUnlock()`

Crystal `Sync::RWLock` exposes:
- `read(& : -> U) : U` — block form returning the value
- `write(& : -> U) : U` — block form
- `lock_read` / `unlock_read` — imperative
- `lock_write` / `unlock_write` — imperative

zinc-go's source is imperative (`rw.RLock(); ...; rw.RUnlock()`).
**Codegen rule**: lower zinc imperative `rw.RLock()` / `rw.RUnlock()`
pairs to Crystal `lock_read` / `unlock_read` (1:1). Don't try to
convert to block form — that's a control-flow rewrite, not a token
mapping. Same for `rw.Lock()` / `rw.Unlock()` → `lock_write` /
`unlock_write`.

Method-name mapping table:

| zinc / Go-style       | Crystal Sync::RWLock     |
|-----------------------|--------------------------|
| `rw.RLock()`          | `rw.lock_read`           |
| `rw.RUnlock()`        | `rw.unlock_read`         |
| `rw.Lock()`           | `rw.lock_write`          |
| `rw.Unlock()`         | `rw.unlock_write`        |

### 8. `lock (mu) { }` → `mu.synchronize { }` — straightforward, no edge case

Confirmed. `Sync::Mutex#synchronize(& : -> U) : U` is the one-line block
form. Direct lowering.

### 9. `record` macro emits `@`-prefixed `to_s` — overrideable

PLAN §3.3 anticipated this. Crystal's `record Circle, radius : Float64`
auto-emits `to_s` printing `Circle(@radius=5)`. Phase 0 sealed.cr does
NOT use `record` — it uses plain `class Foo < Bar` with explicit
`getter` and explicit `to_s(io)` to match zinc-go's `Circle(radius=5)`
format exactly.

**Codegen decision** (firming up PLAN §3.3): for sealed-class data
members, zinc-crystal emits plain classes with explicit constructors
and explicit `to_s(io : IO)` overrides. We do **not** use the `record`
macro. Reason: `record` brings auto-`to_s` we'd have to override
anyway, plus auto-`==` and other behavior that may diverge. Plain
classes make codegen output more uniform and predictable.

For `data` classes that are NOT inside a sealed (top-level data
classes), the same rule applies — uniform output beats macro magic.

### 10. `loop { }` vs `while (true) { }`

zinc emits `while (true) { ... }` for unbounded loops; zinc-go now
warns and rewrites to `for {}` (Go idiom). Crystal has `loop do ... end`
as the idiom. Phase 0 used `loop do` for the worker's read-until-STOP.
**Codegen rule**: `while (true) { }` lowers to Crystal `loop do ... end`,
matching zinc-go's Go-side behavior of swapping to the idiom.

### 11. Inclusive vs exclusive ranges — already in PLAN §11.11

Confirmed in code: zinc's `for (i in 0..n)` with exclusive intent must
emit Crystal `(0...n).each { |i| ... }`. The double-dot Crystal range is
inclusive. No new finding — just confirming the plan.

### 12. `Nil` returns inside blocks need careful escape

In `Versioned#read`, the natural port:
```crystal
def read : String
  @rw.read do
    return @tag
  end
end
```
makes the `read` block's return type infer to `String`, but the OUTER
`def read : String` requires a value path even after the block. Phase 0
added a trailing `""` line to satisfy the type-checker; runtime never
reaches it because `return @tag` from inside the block escapes the
method.

**Codegen rule**: when a zinc `pub T method() { ...; return v; ... }`
body returns from inside a `lock` / `synchronize` / `read` / `write`
block, the Crystal lowering needs the trailing `unreachable` value of
the right type after the block, OR convert the block-form to an
explicit `lock_read; v = ...; unlock_read; return v` shape. The latter
is closer to the zinc imperative semantics but uglier.

Phase 1: pick one. Recommendation: **emit the imperative pair** (option
B from §7) — the `unreachable trailing value` is sketchy and offends
the type-checker if the trailing zero-value isn't a sane default
(e.g. for an unconstructible class type).

### 13. Tuple-returning thrower → bare value-tuple

zinc:
```
pub (Int, String, error) lookup(String key) {
    if (key == "") { return ParseError("missing key") }
    return 7, "found", null
}
```
Crystal port:
```crystal
def lookup(key : String) : Tuple(Int32, String)
  raise ParseError.new("missing key") if key == ""
  {7, "found"}
end
```

Mappings:
- The trailing `error` slot drops from the signature.
- `return ErrorExpr` in body → `raise ErrorExpr`.
- `return v1, v2, null` (with the trailing `null` for the error slot)
  becomes `{v1, v2}` (a `Tuple` literal) — no second `null`, no third
  slot.

**Codegen rule**: thrower lowering walks the return statements:
- Single zinc-error return: `raise ErrorExpr`.
- Multi-value return ending in `null` (representing "no error"): drop
  the last `null` and emit the rest as a tuple literal.
- Bare value with implicit-no-error: same as above.

### 14. `Exception#message` is `String?`, not `String`

`super(msg)` to Exception sets `message`, but `Exception#message`
returns `String?` (nilable). In `or { print("caught: ${err}") ... }`
the natural lowering `puts "caught: #{err.message}"` works because
Crystal's interpolation handles `Nil` (prints empty string), but if
later code uses the message as a `String`, you need `err.message ||
""`.

Phase 0's `error_string` getter does `message || ""` to be explicit.

**Codegen rule**: when emitting `${err}` or `${err.message}` in zinc, if
the receiver is an Exception subclass, lower to `#{err.message || ""}`
defensively. Or to `#{err}` (which calls `to_s`). The latter is
simpler — Phase 1 should default to `#{err}` for Exception-typed values
in interpolation.

### 15. `import sync` / `import fmt` — explicit-include vs zinc.toml mapping

zinc-go has these as Go stdlib imports auto-resolved. zinc-crystal needs
the equivalent — `import sync` lowers to `require "wait_group"` (and
implicit `Sync::Mutex` already in stdlib), `import fmt` lowers to
nothing (`puts` is built-in). PLAN §4.9 spec'd `import_map.toml` for this.

**Codegen rule**: `import_map.toml` ships in zinc-crystal Phase 1 with at
minimum:
```toml
[mappings]
"sync"           = { require = "wait_group" }
"fmt"            = { drop = true }
"encoding/json"  = { require = "json" }
```
The `drop = true` means "no require emitted; method-call rewrites alone
are enough." Need to also encode the call-site rewrites (`fmt.Println`
→ `puts`, `fmt.Printf` → `printf`, etc.) — open question on where
those rewrites live (`import_map.toml` extension or codegen builtin?).

---

## What did NOT mismatch (sanity)

- String interpolation: `${x}` → `#{x}` is uniform and direct.
- `if`/`while` shape: same control-flow shape, just different keywords.
- Channel ops: `Channel<T>(N)` → `Channel(T).new(N)`, `.send` / `.receive` —
  identical semantics.
- `puts` works for everything that has a `to_s` — no need for `print` /
  `Println` distinction.
- `Tuple(Int32, String)` literal `{7, "found"}` is direct.
- Class inheritance `class Foo : Bar` → `class Foo < Bar`.

---

## Open items for the Phase 0 → Phase 1 handoff

1. **Float64 formatting**: confirm we want byte-equivalence with zinc-go
   output (driving `fmt_num` helper). [§4 above]
2. **Method-name reserved-word collision list**: enumerate exactly which
   Crystal identifiers cause collisions when zinc lowercases. [§6]
3. **Unreachable-value pattern after block-with-early-return**: pick
   imperative-pair vs trailing-zero. [§12]
4. **import_map.toml call-site rewrite vocabulary**: where do
   `fmt.Println` → `puts` rewrites live? [§15]
5. **Crystal `record` macro**: confirmed unused. Re-confirm in Phase 1
   when new data-class shapes appear.
6. **`return` inside `loop` inside `wg.spawn`**: PLAN §7.4 flagged this
   UNVERIFIED. Phase 0 used `break` in workerpool.cr (instead of
   `return`) to be safe — confirm in Phase 1 whether `return` from a
   `wg.spawn` block escapes only the fiber or also `main`. If only the
   fiber, switch back to `return`; if it escapes, keep `break`.
