# Zinc — type system (1.0 target)

**Status:** Phase 2 deliverable (companion to `02-semantics.md`). Defines the type universe, compatibility rules, inference, generics with bounds, null safety, FFI bridging, and equality semantics.

**Authority order.** Type-system rules in this doc are normative. When `01-grammar.md` (syntax) describes a form, this doc gives it meaning at the type level. When `02-semantics.md` describes runtime/scoping behavior, the type rules here constrain what those forms can mean statically.

---

## 1. Type universe

### 1.1 Primitive types

| Zinc | Go lowering | Range / behavior |
|---|---|---|
| `bool` | `bool` | true/false |
| `byte` | `byte` (uint8) | 0..255 |
| `int` | `int` (Go's machine-int, typically 64-bit) | platform-dependent width |
| `long` | `int64` | -2^63 .. 2^63-1 |
| `float` | `float32` | IEEE 754 single |
| `double` | `float64` | IEEE 754 double |
| `String` | `string` | immutable, UTF-8 |

Notes:
- **No** `short`, `char`, `uint*` primitives. (Java vestiges — removed from the V2Checker scaffolding during the rewire.) If specific widths are needed, FFI to Go.
- `int` is platform-dependent; use `long` for explicit 64-bit. `int` arithmetic that would overflow is **wraparound** (Go semantics), not a panic.
- `String` is value-typed but immutable. `==` is byte-equal.

### 1.2 Reference types

| Form | Definition | Lowering |
|---|---|---|
| **Class** | `class C { ... }` | Go interface or struct + pointer; methods have virtual dispatch |
| **Sealed class** | `sealed class S { data Variant1(...); ... }` | Go interface; variants are concrete data classes implementing it |
| **Interface** | `interface I { ... }` | Go interface |
| **Data class** | `data D(...)` | Go struct (value-typed, no virtual dispatch) |

A `class` instance has identity (pointer). A `data` instance has value semantics (struct copy on assignment). A `sealed class` is an algebraic data type — open hierarchy of `data` variants.

### 1.3 Composite types

| Form | Lowering | Notes |
|---|---|---|
| `T[]` | `[]T` | Go slice. Mutable. |
| `List<T>` | `[]T` | Same as `T[]`; idiomatic for collections |
| `Map<K, V>` | `map[K]V` | K must satisfy `Hashable` |
| `Set<T>` | `map[T]struct{}` | T must satisfy `Hashable` |
| `Channel<T>` (also `Chan<T>`) | `chan T` | Send+recv channel |
| `T?` | `*T` for non-pointer T; `*T` (unchanged) for pointer T | Nullable. See §5. |
| `Fn<(P1, P2), R>` | `func(P1, P2) R` | Function type |

Empty array/slice literal: `int[0]` (sized) or `List<int>[]` (typed-empty). The latter is preferred when the type can't be inferred from elements.

Capacity hint: `List<int>(1024)` allocates with cap 1024 and len 0.

### 1.4 Tuple types — return position only

```
(T1, T2, ..., Tn)        // n >= 2
```

Tuples are **not** values. They appear only at:
- Function/method return-type position: `(int, error) f()`.
- `Fn<...>` return-type position: `Fn<(String,), (int, error)>`.

Tuple destructure binds named slots:
```
var x, err = f()         // names match tuple arity
```

A 1-element tuple `(T)` is unwrapped to `T` at parse time. There is no zero-element tuple (use `void`).

### 1.5 The `any` type — FFI bridge only

`any` is a Zinc type that bridges to Go's `interface{}`. It is permitted **only** as:
1. The slot type when calling into a Go FFI seam whose Go param is `any`.
2. The return-slot type when storing a Go FFI result with `any` static type.
3. The element type of `Map<String, any>` and similar when interoperating with parsed JSON/YAML.

**Restrictions:**
- User code may not declare `any T` as a top-level variable type or struct field type **except** through `Object` (which is the Zinc-level alias). The bind phase rejects bare `any` outside the FFI seam.
- A value of static type `any` can only be cast to a concrete type via `as T or { ... }` or pattern-matched via `match (v) { case ... }`.

This restriction prevents `any` from becoming a "anything goes" type-evasion hatch. It exists for FFI; that's the only excuse.

---

## 2. Type compatibility

A value of type `Actual` is **compatible** with a slot of type `Declared` (assignment, return, arg-pass) iff one of the following holds.

### 2.1 Identity

`Actual ≡ Declared` (structurally). For data classes and sealed variants, structural identity is name-based: `core.Schema` and `hambaAvro.Schema` are not the same type even if they have the same field shape.

### 2.2 Numeric widening

Implicit widening is allowed in **one direction**:

| From | To |
|---|---|
| `int` | `long`, `float`, `double` |
| `long` | `double` |
| `float` | `double` |
| `byte` | `int`, `long` |

No implicit narrowing. `var i: int = aLong` is a compile error; use explicit cast `i = aLong as int` (which is a value cast, may truncate).

### 2.3 Subtype

`Sub` is a subtype of `Super` iff:
- `Sub` extends `Super` (class hierarchy via `:` declaration), OR
- `Sub` implements `Super` (when `Super` is an interface), OR
- `Sub` is a sealed-variant of `Super` (when `Super` is a sealed class).

Subtype assignment is implicit: `Animal a = Dog("Rex")` is OK if `Dog : Animal`.

### 2.4 Tuple compatibility

Two tuple types `(A1, ..., An)` and `(B1, ..., Bn)` are compatible iff they have the same arity and each `Ai` is compatible with `Bi`. No structural inference across mismatched tuples.

### 2.5 Null compatibility

`null` is compatible **only** with `T?` types (the explicit nullable). See §5.

**Exception:** the built-in `error` type is implicitly nullable. A function returning `(T, error)` returns `value, null` on success without needing `error?`. This is a single, deliberate carve-out for the errors-as-values success-path idiom; no other type is implicitly nullable.

### 2.6 Function compatibility

`Fn<(P1, ..., Pn), R>` is compatible with a slot of type `Fn<(Q1, ..., Qn), S>` iff:
- Each `Pi` is compatible with `Qi` (parameter types are *invariant* in 1.0 — no contravariance).
- `R` is compatible with `S` (return is also invariant).

This is more restrictive than Go (which has full structural compatibility) but easier to type-check and matches user expectations. Variance is a 1.x concern.

### 2.7 `any` compatibility (FFI only)

A value of any type is compatible with an `any` slot **at an FFI call site**. Any other use of `any` requires a downcast.

---

## 3. Type inference

### 3.1 Variable declarations

`var x = expr` — `x`'s type is the static type of `expr`.

`var x = call() or { return null }` — `x`'s type is the **success-slot** type of `call()`. The handler's return is checked separately against the enclosing function's return type.

`var x, y, err = f()` — each name binds to the corresponding tuple slot type.

### 3.2 Lambda return type inference

```
(int x) => x + 1
```

Inferred return type: `int`. The body is type-checked with each param's declared type, and the return slot is the body's expression type (or the unified type of all `return` statements in a block body).

When a lambda is passed to a `Fn<(P,), R>` slot, the param types may also be inferred from `P`:

```
fn applyOnce(int x, Fn<(int,), int> f): int {
    return f(x)
}
applyOnce(5, (x) => x * 2)        // x: int, return type: int
```

### 3.3 Generic substitution at call sites

When a generic function is called:

```
<T> List<T> repeat(T item, int n)
```

Substitution determines `T` from the argument types:

```
var xs = repeat("hi", 3)         // T = String
var ys = repeat<int>(0, 5)       // explicit T = int
```

Inference fails when args don't agree on `T` (`repeat("hi", 5.0)` if both are bound to `T`); compile error.

---

## 4. Generics and constraint solving

### 4.1 Type parameter declaration

```
<T>                      // bare type param, implicitly bound by "any"
<T : Comparable>         // single bound
<T : Comparable + Hashable>     // multi-bound (intersection)
<T, U : Hashable>        // multiple type params, only U is bounded
<T> where T : Comparable, T : Hashable     // 1.x: where-clause form (deferred)
```

### 4.2 Bound satisfaction

A bound is a Zinc interface or class. `T : Comparable` means `T` satisfies the `Comparable` interface (has a `compareTo(T) int` method).

The constraint solver verifies, at each call site:
- For each type argument `T_i`, the chosen concrete type satisfies all of `T_i`'s declared bounds.

```
<T : Comparable> T max(T a, T b) {
    if (a.compareTo(b) > 0) { return a }
    return b
}

max(3, 5)                       // OK: int satisfies Comparable
max("a", "b")                   // OK: String satisfies Comparable
max(MyClass(...), MyClass(...)) // ERROR: MyClass doesn't satisfy Comparable
```

### 4.3 Built-in bounds

The 1.0 standard bounds:

| Bound | Definition |
|---|---|
| `Comparable` | has `compareTo(self) int` |
| `Hashable` | has `hashCode() int` and `==` |
| `Equatable` | has `==` |
| `Iterable<T>` | has `iter() Iterator<T>` |
| `Stringer` | has `String() String` |

Primitive types satisfy: `int`, `long`, `float`, `double`, `byte`, `String` are `Comparable + Hashable + Equatable + Stringer`. `bool` is `Equatable + Hashable + Stringer` (not `Comparable`).

User-defined bounds are simply user-defined interfaces (`interface MyBound { ... }`).

### 4.4 Cross-package generic instantiation

A generic type or function defined in package A can be instantiated in package B with a type from package C:

```zinc
// in package A
<T> data Box<T>(T value)

// in package C
data Thing(int n)

// in package B
import A
import C
var b = A.Box<C.Thing>(C.Thing(42))
```

The bind phase resolves `A.Box<C.Thing>` to a fully-qualified instantiation. Codegen emits a Go-generic instantiation that imports both A and C.

**Sealed-variant cross-pkg matching:**

```zinc
// in package A
sealed class Result<T> { data Ok(T value); data Err(String message) }

// in package B
import A
fn handle(A.Result<String> r) {
    match (r) {
        case A.Ok(v)  -> log.info("ok: ${v}")
        case A.Err(m) -> log.error("err: ${m}")
    }
}
```

Variant patterns must qualify the variant name when used cross-package. Bare `Ok(v)` would be ambiguous (or refer to a same-package `Ok` if one exists).

### 4.5 No variance for 1.0

Type-parameter variance (covariance, contravariance, invariance) is **invariance only** for 1.0. `Box<Dog>` is not assignable to `Box<Animal>` even though `Dog : Animal`. This matches Go's generics behavior.

Variance markers (`<out T>`, `<in T>`) are deferred to 1.x.

---

## 5. Null safety

### 5.1 The rule

`T?` is the **only** nullable type in Zinc. A value of type `T` (without `?`) cannot be `null` at compile time. Assigning `null` to a `T` slot is a compile error.

```zinc
String x = null         // ERROR: String is not nullable
String? y = null        // OK
String z = y            // ERROR: y might be null; can't assign to non-nullable String
```

This is a **break from current behavior** — today's V2Checker allows `null` on any reference type. The rebuild enforces null safety.

### 5.2 Smart-cast on null check

After an `if (x != null) { ... }` guard, `x` is treated as the non-null type `T` (smart-cast):

```zinc
String? name = lookup(id)
if (name != null) {
    print(name.length)        // name is String (non-null) here
}
print(name.length)            // ERROR: name is String? — must check or unwrap
```

The narrowing applies to the `then` branch only. After the `if`, the original nullable type is restored unless the `else` branch returned/threw.

### 5.3 Forced unwrap

A `T? force` (`force` is a method on `T?`) panics if the value is null and returns the underlying `T` otherwise. Use sparingly; prefer guards.

```zinc
String? maybe = lookup(id)
String s = maybe.force()        // panics if maybe is null
```

(Open question for Phase 3: should this be a method `.force()`, an operator `!`, or an explicit cast `maybe as String`? Today's `as` already serves this; reuse it.)

### 5.4 `null`-coalescing

The `??` operator was **dropped** (decision 2026-05-01). Use an `if`:

```zinc
String name = maybe != null ? maybe : "default"      // ternary form via if-expr
// OR
String name = if maybe != null : maybe else : "default"
```

(Spec note: confirm whether the `?:` ternary is a separate parse-level form or just sugar for `if expr : a else : b`. Phase 3 implementation choice.)

---

## 6. FFI type bridging

### 6.1 Go-imported types

When a Zinc file imports a Go package via `[deps]`, the bind phase exposes that package's exported types as if they were Zinc types. They participate in:
- Variable type declarations: `hambaOcf.Decoder dec = ...`
- Generic type arguments: `List<sql.Row>` (via FFI bridge)
- Method-call receivers: `dec.Decode(&got)`

But **not**:
- As bounds in generic constraints (Go interfaces and Zinc interfaces are distinct concepts; cross-pollination is deferred)
- As parents in `class C : GoType` (Zinc inheritance is Zinc-only)

### 6.2 Type-tracking through tuple destructure

A multi-value Go return:

```zinc
var dec, derr = hambaOcf.NewDecoder(rdr)
```

Binds `dec` to type `*ocf.Decoder` (Go-resolved) and `derr` to `error`. The bind phase records each name's Go type.

Subsequent access:

```zinc
dec.Decode(&got)
```

The bind phase consults `dec`'s recorded type (`*ocf.Decoder`); the typecheck phase uses Go's method-set on that type to resolve `.Decode`. The `&got` is permitted because `dec` is Go-typed (FFI-method-call seam).

### 6.3 Pointer-vs-value at FFI

| Zinc type | Go-call signature | Codegen |
|---|---|---|
| `class C` instance | `*C` (Go pointer) | passes pointer directly |
| `data D` instance | `*D` (Go pointer expected) | auto-`&D` |
| `data D` instance | `any` (interface{}) | user writes `&d` |
| primitive | `*T` | auto-`&v` |
| primitive | `any` | user writes `&v` |
| `T?` (`*T` Go-side) | `*T` | passes the pointer (already nullable) |

### 6.4 The closed-world FFI assumption

The compiler relies on **introspection of Go module sources** at transpile time (`internal/codegen_go/gotypes.go`'s `GoTypeResolver`). For each imported Go package, it reads:
- Type definitions (struct, interface, type alias)
- Function signatures (param types, return types)
- Method sets (for pointer-vs-value receivers)

This works because Zinc's FFI is statically resolved at Zinc-compile time, before the Go compiler sees the generated code. There is no runtime FFI — every Go call lowers to a direct Go call site at codegen.

---

## 7. Equality and hashing

### 7.1 `==` semantics

Decision 2026-05-01: **Go-identity** semantics.

| LHS / RHS type | `==` behavior |
|---|---|
| Two primitives (same type) | byte-equal |
| Two `String` | byte-equal |
| Two class pointers | pointer identity |
| Two data class values | field-by-field |
| Two `T?` of same `T` | both null = equal; one null one non-null = not equal; both non-null = recurse on `T` |
| Two slices / Lists | **compile error** — use `slices.Equal(a, b)` |
| Two maps | **compile error** — use `maps.Equal(a, b)` |
| Mixed types | **compile error** unless one side widens to the other (e.g., `int == long` after widening) |

`!=` is the negation of `==` with the same rules.

### 7.2 `Hashable` requirement

For a type to be a `Map` key, `Set` element, or otherwise hash-keyed, it must satisfy `Hashable`:
- Has `==` (which all primitives, `String`, and field-comparable data classes satisfy automatically).
- Has `hashCode() int`.

Primitives, `String`, and data classes whose fields are all `Hashable` automatically satisfy `Hashable`. Classes do **not** automatically satisfy it (pointer-identity hashing is unreliable across pointer churn); user must explicitly implement.

### 7.3 `Comparable` requirement

For `<`, `<=`, `>`, `>=` to work, both sides must satisfy `Comparable`. Primitives and `String` do. `bool` does not.

User types implement `Comparable` by providing `compareTo(T self) int` returning negative/zero/positive.

### 7.4 `Equatable`

A weaker form of `Hashable` that requires only `==`. Useful as a generic bound when you don't need hashing.

---

## 8. Match exhaustivity

### 8.1 The rule

When `match (subject)` runs, the typecheck phase examines `subject`'s static type:

- **Sealed class:** every variant must be matched, OR a wildcard arm `_` must be present.
- **Enum:** every variant must be listed, OR wildcard.
- **Open type** (e.g., `int`, `String`, `any`): wildcard required.

Non-exhaustive match is a compile error:

```
error: match on Result<String> is not exhaustive — missing case: Err
       at example.zn:42:5
       hint: add `case Err(_)` to cover the variant, or add `case _` for a catch-all
```

### 8.2 Reachability

Match arms are checked top-to-bottom. An arm is **unreachable** if every value reaching it has already been matched by an earlier arm. Unreachable arms produce a warning (not an error) — common patterns like exhaustive sealed match followed by `case _` for safety are legal.

### 8.3 Pattern types

| Pattern | Matches | Bindings |
|---|---|---|
| `_` | anything | none |
| `42` | values equal to 42 | none |
| `"hello"` | values equal to "hello" | none |
| `MyEnum.Red` | the Red variant | none |
| `Ok(v)` | `Ok` variant | `v` bound to its value field |
| `Ok(_)` | `Ok` variant | nothing bound |
| `Pair(_, b)` | `Pair` with any first, second bound | `b` bound |
| `0..10` | int subjects in 0..10 | none |
| `Person { name, age: 30 }` | (1.x: struct destructure with field equality + binding) | `name` |

### 8.4 Match in expression position

`match_expr` (§6 of grammar) is the expression-position form: `match (subject) { case pat -> expr ... }`. Same exhaustivity rules apply. Each arm's expression must produce a compatible type; the match expression's type is the unified type of all arms.

---

## 9. Type errors and diagnostics

Every type error must include:
1. The source span (file, line, column-start, column-end) of the offending construct.
2. The expected type and the actual type, formatted human-readably.
3. A suggestion when there's an obvious fix (e.g., for null-safety: "wrap in `?` or guard with `if (x != null)`").

Example error format (matches Rust/Swift diagnostic style):

```
error[E0042]: type mismatch
  --> src/avro_binary.zn:152:34
   |
152|     bytes := hambaAvro.Marshal(hSchema, recordValues)
   |                                ^^^^^^^ expected `*hambaAvro.Schema`, found `core.Schema`
   |
help: cast through Avro JSON: `parseHambaSchema(emitSchemaJson(recordValues))`
```

(The exact format is a Phase 3 implementation choice; the requirement is that diagnostics carry positions, expected/actual types, and suggestions where possible.)

---

## 10. Open questions for Phase 3

1. **Forced unwrap syntax.** §5.3: `.force()` method, `!` operator, or reuse `as T`. Pick one.
2. **Ternary `?:`.** Today's grammar uses `if cond : a else : b` for expression-position if. Should `cond ? a : b` be sugar for the same thing? Or stay with the `if` form only?
3. **`Stringer` automatic conformance.** Should every type implement `String()` by default (Go-style auto-derive), or require an explicit interface declaration?
4. **Equality customization.** Today's `==` is Go-identity for class pointers. Should classes be allowed to override `==` via an `equals` method (Java/Kotlin style)? Decision impacts `Map<Class, V>` key-hashing.
5. **`any` widening into Zinc-internal calls.** Currently the spec restricts `any` to FFI seams. If a Zinc lib needs to accept "any value" (e.g., a logging API), we'd need a widening rule. Defer or design.
6. **Generic specialization.** Should generic functions emit one Go function per type instantiation (specialization, fast), or use Go's interface dispatch under the hood (one impl, slower)? Go's generics support both; the spec needs to commit.
7. **Constraint inference.** When a user writes `<T> max(T a, T b)` and the body uses `<` on `T`, can the compiler infer `T : Comparable` automatically? Or is the bound declaration required? Defer to 1.x.
8. **Cross-pkg interface satisfaction.** A Zinc class in package A can satisfy a Zinc interface in package B without explicit `: B.IFace`. Today: structural satisfaction (Go-style). Decision: keep structural, or require explicit declaration (Java-style)? Affects user expectations.

These don't block Phase 2 sign-off; Phase 3 needs to answer them as the typechecker is implemented.
