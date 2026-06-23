# BEAM Zinc Design Notes

This document records language decisions for canonical Zinc on BEAM. Zinc-Go examples remain
useful syntax input, but BEAM Zinc should use BEAM-native semantics where the runtime model
differs.

## 1. Errors

Decision: keep the BEAM way.

- Zinc on BEAM uses unchecked exceptions plus OTP supervision.
- Recoverable local failures use `try` / `catch`.
- Actor failures should crash the actor process and let its supervisor restart it.
- Typed actor calls relay deliberate Zinc exceptions to the caller; bugs still crash the
  actor.
- Do not port Zinc-Go's explicit trailing `error` return model as the canonical BEAM error
  model.

Canonical shape:

```zinc
class NotFound : Exception {}

String find(String id) {
    if (id == "") {
        throw NotFound("missing id")
    }
    return id
}

void main() {
    try {
        print(find(""))
    } catch NotFound e {
        print(e.message)
    }
}
```

Implementation direction:

- Keep `Exception`, `throw`, `try`, and `catch` as the main failure syntax.
- Keep `error_*`, `(T, error)`, and `catch { return err }` Zinc-Go examples out of
  `e2e-zinc.sh`.
- If typed result values are needed later, design them as ordinary sealed records such as
  `Ok(T)` / `Err(String)`, not as a second error channel built into every call.

## 2. Nullability

Decision: nullability is acceptable at this stage; strict null checking is not required now.

- `null` may exist as a value for interoperability and simple optional flows.
- `T?` is accepted as surface type syntax, normalizing to `T` plus nullable metadata.
- The compiler should not attempt a full strict null-safety analysis in the near term.
- Runtime failures from bad null use are acceptable initially.

Implementation direction:

- `null` is accepted as a literal.
- `T?` is accepted as an annotation/metadata layer, not a separate runtime representation.
- Safe navigation `?.` is supported for field and method access. A null receiver returns
  `null`; a non-null receiver performs the normal access.

Runtime representation:

- `null` lowers to the Erlang atom `null`. Missing map keys still use the existing map
  access behavior and are not represented as nullable values.

## 3. Generics

Decision: BEAM does not need reified Java/Go-style generics for normal execution. Generics
should be type metadata, mostly for collections and boundary checks.

BEAM/Erlang values are dynamically typed. The practical mechanism is:

- Keep type parameters in the AST/type strings.
- Use them for local inference, compile-time known-vs-known checks, and shallow boundary
  guards.
- Erase them in generated Erlang terms.
- Do not monomorphize functions/classes unless a future optimizer proves it is worth it.

This matches the current collection path:

```zinc
var xs = List<String>["a", "b"]
var scores = Map<String, int>{"a": 1}
```

The generated runtime values are still a list and a map. `String` and `int` are compiler
metadata used for indexing, iteration, assignment, and guard checks.

Implementation direction:

- Keep collection generics as the first-class generic use case.
- Generic data records and functions are supported as erased type metadata.
- Avoid implementing bounded generics or typeclass-style constraints until there is a real
  BEAM use case.

Likely syntax:

```zinc
record Pair<A, B>(A first, B second)

T identity<T>(T value) {
    return value
}
```

Runtime lowering:

- `Pair<String, int>("x", 1)` lowers like `Pair("x", 1)`.
- `identity<T>` emits one function, not one function per concrete type.

## 4. Tuples And Multi-Returns

Decision: design this as BEAM tuples, not Go multi-return.

BEAM has native tuples, so Zinc can expose tuple values directly without importing Go's
error-tail convention.

Likely syntax:

```zinc
(int, String) user() {
    return (7, "vin")
}

void main() {
    var (id, name) = user()
    print("${id}:${name}")
}
```

Design rules:

- Tuple types are value types: `(A, B, C)`.
- Tuple literals use parentheses with commas: `(a, b)`.
- Destructuring is a binding form, not assignment to arbitrary expressions.
- Tuple arity must match at compile time when known.
- A one-element tuple should not exist syntactically; `(x)` remains grouping.

Implementation direction:

- Tuple literals and tuple type parsing are supported.
- Destructuring `var (a, b) = expr` is supported.
- Typed destructuring is supported: `(int id, String name) = expr`.
- Lower to Erlang tuples `{A, B}`.

## 5. Sequences, Ranges, And Slicing

Decision: do not import Go bracket-slice syntax as canonical BEAM Zinc.

BEAM/Erlang has no general slice operator. The closest runtime mechanisms are library
operations:

- `lists:seq/2` and `lists:seq/3` for integer sequences.
- `lists:sublist/2`, `lists:sublist/3`, and `lists:nthtail/2` for lists.
- `array` module operations for BEAM arrays.
- `string:slice/2` and `string:slice/3` for strings.
- `binary:part/3` for binaries.

Canonical Zinc should expose those through named library/method operations, not `xs[a..b]`.

Candidate syntax:

```zinc
for i in Seq.range(0, 3) {
    print(i)
}

var mid = s.substring(1, 4)
var part = Bytes.slice(data, 1, 3)
var head = Lists.slice(xs, 0, 3)
```

Type behavior:

- `String.substring(start, end)` returns `String` and already lowers to `string:slice`.
- `Bytes.slice(bytes, start, length)` should return `byte[]`.
- `Lists.slice(xs, start, length)` should return `List<T>` and may warn for O(n).
- Array slicing should be a library operation if needed, not bracket syntax.

Implementation direction:

- Prefer `Seq.range` / `Seq.rangeClosed` over `a..b`.
- `Lists.slice(xs, start, length)` and `Bytes.slice(bytes, start, length)` are supported
  helper facades over BEAM list and binary primitives.
- Keep existing `range(a, b)` and `a..b` accepted as compatibility syntax for now, but do
  not promote them as the canonical BEAM spelling.
- Do not add `Slice` / `xs[a..b]` unless a later design explicitly chooses bracket slicing.
- Make list slicing costs explicit in warnings.

## 6. Function Types And Lambdas

Decision: do not introduce `Fn<...>` as canonical syntax. Use BEAM-ish / Java-ish
single-method interfaces as function types, and lower lambda values to Erlang funs.

Canonical syntax:

```zinc
interface Predicate {
    boolean test(int value)
}

boolean any(List<int> values, Predicate pred) {
    for v in values {
        if pred.test(v) {
            return true
        }
    }
    return false
}

void main() {
    Predicate positive = n -> n > 0
    print(any(List<int>[0, -1, 3], positive))
}
```

Design rules:

- A single-method interface is the function type.
- A lambda can bind to a single-method interface target.
- Interface method signatures provide lambda parameter and return types.
- The runtime value for a lambda-backed interface is an Erlang fun.
- Nominal classes can still implement the same interface when a named object is clearer.
- Captures remain effectively final, matching current lambda behavior.
- Arity and return type are checked through the target interface.

This keeps the source familiar to Java users without importing Java's runtime object model
for lambdas. It also avoids the custom `Fn<(A), R>` syntax from Zinc-Go.

Implementation direction:

- Keep the existing SAM-interface lambda lowering as the primary path.
- Add typed lambda parameters only if they improve inference for unambiguous cases; the
  preferred style is to let the target interface type the lambda.
- Do not port `Fn<...>` examples directly. Rewrite them as named interfaces or existing
  domain interfaces.

## 7. Object Model

Decision: keep BEAM Zinc's object model small and explicit.

Current model:

- `record` is immutable data.
- `class X : Actor` is stateful concurrent process.
- `class Main : Application` is a supervision boundary.
- `interface` plus nominal instance classes provide small object-like dispatch.
- Exceptions are nominal exception records.

Design direction:

- Do not import Go pointer/value receiver semantics.
- Do not make general inheritance the central object model.
- Prefer records for values and actors for mutable state.
- If data methods are needed, prefer explicit nominal instance classes or module functions
  before adding broad class inheritance.

Examples to avoid as direct ports:

- Go pointer inference.
- Value receiver vs pointer receiver behavior.
- Struct tags.
- Go package FFI classes.

## 8. Bytes And Binary Data

Decision: `byte[]` is BEAM binary data, not an ordinary mutable array.

BEAM binaries are the right representation for file bytes, crypto, base64, gzip, and HTTP
bodies. Treating `byte[]` as an array would work against the runtime.

Design rules:

- `byte[]` literals lower to binaries.
- `byte[n]` should not mean ordinary array allocation.
- Indexing a `byte[]` returns an int byte value.
- Slicing a `byte[]` should return a `byte[]` binary.
- Mutation is not supported directly; build new binaries through APIs/builders.

Implementation direction:

- Keep `byte[n]` unimplemented until there is a binary builder story.
- Prefer APIs such as `Random.bytes(n)`, `Files.readBytes(path)`, `Base64.decode(s)`, and
  `Hex.decode(s)`.
- `Bytes.slice(bytes, start, length)` is supported and returns `byte[]`.
- `bytes[i]` is supported and returns `int`.
