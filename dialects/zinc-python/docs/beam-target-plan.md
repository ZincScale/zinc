# Surface mapping: braces-Python → BEAM

Status: DESIGN — decisions locked, not started.

## Thesis

Keep `beam-transpiler`'s lowering (actors→gen_server, supervision, channels,
dispatch) intact. Replace only the *front* — how the surface spells those
concepts — with braces-Python. Reuse the `zinc-python` frontend (lexer,
braces→blocks, implicit-self, f-strings, parser). The work is the **seam**:
point the existing lowering at braces-Python's spelling of each BEAM concept.

Surface is **braces, not whitespace** — that's the whole reason this dialect
exists. Python ergonomics (fast, to-the-point), `{}` blocks, runs on BEAM.

This is a **typed** language, not dynamic Python: return types are checked and
load-bearing (see #2). braces-Python today only transpiles; a type checker is
new work here, but the type info BEAM dispatch wants is the same info the
cast/call rule needs — one checker pays for both.

## Decisions (locked)

### 1. Marking an actor — base class
```
class Counter(Actor) {
    count = 0
    def incr(self) { self.count = self.count + 1 }
}
```
`(Actor)` base class maps 1:1 onto the old `implements Actor`. One mechanism
covers Actor / Application / protocol marks. **Seam: tiny.**

### 2. Cast vs call — checked return type
```
def incr(self) { ... }            # no return type   -> cast (async)
def get(self) -> int { ... }      # real return type -> call (sync, blocks)
```
Rule: a real `-> T` = sync **call**; no annotation or `-> None` = async
**cast**. Return types are **type-checked**, not optional hints — the checker
verifies the annotation matches the actual returns, and the cast/call lowering
keys off it. Safe default falls out: a missing annotation is a cast
(fire-and-forget, can't deadlock); a wrong cast where a value was wanted is
loud. **This rule is the whole architecture risk — concentrated in one place.**

### 3. Entry point & supervision — clean default, opt-in root
```
def main() { ... }                # script: top-level main, zero ceremony

class Main(Application) {          # supervised app: children are class fields
    worker = Worker()
    def main(self) { ... }
}
```
The `__main__` guard is auto-injected, so bare `def main()` is the entry point
with no ceremony — the clean form is the **default**, `class Main(Application)`
is the opt-in upgrade for supervised children. (Inverts the legal-Java problem
where clean main was gated behind Application.) **Seam: small.**

### 4. Protocols & dispatch — nominal via base class
```
class English(Greeter) {
    def greet(self, name) -> str { return "hello " + name }
}
l = lambda n: "lambda " + n        # SAM lambda satisfies a 1-method protocol
```
Keep **nominal** conformance (`class X(Greeter)`) — maps straight onto the
existing tag-dispatch lowering. Python's structural `typing.Protocol` is
**deferred to v2** (it needs backend dispatch changes). SAM lambdas carry over.
**Seam: tiny.**

### 5. FFI escape hatch — Pythonic import, same passthrough
```
from erlang import gen_tcp, inet      # was: import erlang.gen_tcp;
```
Same alias→module passthrough, same "exempt from the surface lint" status.
Only the spelling changes. **Seam: trivial.**

### Smaller rulings
- **Dunders — dropped.** The `toString`→`__repr__` style renaming is
  Python-target-specific; BEAM has no Python dunders, so it's not needed.
- **Records/structs — keep the existing lightweight convention.** We already do
  records; no `@dataclass`. Lowers to the existing record/tuple path.

## Falls out for free (Python fits BEAM better than Java here)
- **Mutable actor state** — `self.count = self.count + 1` is natural Python,
  lowers to gen_server state as today. No field/`var` ceremony.
- **Bootstrap ceremony** — gone. No `public static void main(String[] args)`.
- **`self`** — implicit-self frontend handles it; `this.x` → `self.x`.

## The seam, summarized
Four of five concepts are **base-class marks** the frontend already parses — so
backend changes are mostly "read the mark from a Python base list instead of a
Java `implements` clause." The genuinely new logic is:
1. **#2 cast/call from checked return types** — the one architecture risk.
2. **The type checker** — new component; minimally checks return types (drives
   cast/call); broader gradual typing TBD.

## Tooling
No javac gate (we don't target the JVM). Editor support comes from an **LSP we
build** — which is the high-value layer anyway (go-to-def, completion, the
type-checker's diagnostics inline). The cheap free-tooling that legal-Java gave
is replaced by the checker + LSP.

## Open / next
- Build order: (a) retarget emission Actor→gen_server for the base-class mark,
  (b) the return-type checker + cast/call, (c) supervision root, (d) protocols,
  (e) FFI import, (f) Channel. Each has an existing beam-transpiler analog to
  port against.
- Type checker scope: start with return-type checking (required by #2); decide
  how far gradual typing goes for params/locals as a follow-up.
