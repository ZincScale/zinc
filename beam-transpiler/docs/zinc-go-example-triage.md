# Zinc-Go Example Triage For BEAM

`compilers/zinc-go/examples` is the syntax reference for canonical Zinc shape, but it is not
a semantic contract for BEAM. Port examples when the construct maps cleanly to BEAM; keep
runtime-specific examples out of `e2e-zinc.sh` until there is an explicit BEAM design.

## Port Directly

- Type-first functions and methods.
- Top-level script statements.
- String interpolation with `${...}`.
- Parenthesized `for (x in xs)`, `while (cond)`, and `match (x)`.
- Range loops `a..b` and `a..=b`.
- Bare enum values when the value name is unambiguous; qualify as `Enum.Value` otherwise.
- Runtime `assert` statements; BEAM currently preserves expression-source diagnostics and
  accepts but does not surface the optional custom message.
- Integer literals in decimal, hex (`0x`/`0X`), binary (`0b`/`0B`), and octal (`0o`/`0O`).
- Integer bitwise and shift operators (`&`, `|`, `^`, `<<`, `>>`) mapped to BEAM integer
  operations.
- Parenthesized arithmetic precedence, including grouped operands under `*`, `/`, and `%`.
- Primitive numeric cast calls (`int(x)`, `long(x)`, `float(x)`, `double(x)`), using BEAM
  integer truncation and float widening.
- Fixed array type syntax and literals (`T[] xs = [...]`) using the existing BEAM array
  lowering.
- Sized primitive array expressions (`int[n]`, `String[n]`, `bool[n]`) when they lower to
  ordinary BEAM value arrays. `byte[]` remains a separate binary-data path.
- Primitive type aliases from Zinc-Go where they are pure surface spelling, such as `bool`
  for BEAM's existing `boolean`, and `long`/`Int`/`Long` to `int`,
  `float`/`Float`/`Double` to `double`.
- Interpolation expressions containing escaped quotes, adapted to BEAM's current map literal
  syntax.
- Typed collection constructors (`List<T>[]`, `List<T>[...]`, `Map<K,V>{...}`) as explicit
  type metadata on BEAM list/map literals; map `.keys()` as an alias for current `keySet()`.
- String method aliases `upper()`, `lower()`, `trimStart()`, and `trimEnd()`.
- Records, enums, sealed unions, interfaces, and nominal classes when they do not rely on
  Go-only codegen.
- Collections, JSON, HTTP facade/client, files/resources, channels, and actor pipelines.

## Adapt To BEAM Semantics

- Concurrency examples should use `Actor`, supervised children, and `Channel`, not Go
  goroutines/select unless a BEAM-native equivalent is designed.
- Failure examples should use `try`/`catch`, actor crashes, and supervision. Do not port
  Zinc-Go's explicit trailing `error` return model as-is.
- Nullability examples may use `null`, `T?`, and `?.` where they map cleanly to BEAM Zinc.
- Generic examples should be adapted as erased type metadata. Collections, generic records,
  and generic functions are supported without monomorphizing runtime code.
- Function-type examples using Zinc-Go `Fn<...>` should be rewritten as single-method
  interfaces. BEAM Zinc's canonical function type is a SAM-style interface backed by an
  Erlang fun.
- Resource cleanup should use existing `with Files.open... as ...`; do not port a separate
  Go-style cleanup spelling.
- `byte[]` is BEAM binary data in existing APIs. `bytes[i]` reads an int byte, and direct
  index assignment is not supported. Do not treat `byte[n]` as an ordinary array until a
  binary builder story is specified.

## Do Not Port As-Is

- `error_*`, `catch_on_assignment`, `as_throws`, and other explicit-error-tail examples.
- Pointer/FFI examples such as address-of, Go pointer inference, struct tags, and Go package
  interop.
- Goroutine/select/timeout examples with Go runtime assumptions.
- Slice-range examples such as `xs[0..3]`; canonical BEAM Zinc uses named sequence, list,
  string, and binary helpers instead.
- Data-class or generic examples that rely on Go-specific value/pointer receiver behavior.
