# Zinc — lessons learned from the implementation

**Status:** Phase 0 deliverable. Read-only synthesis of the current parser, lexer, AST, codegen tracking surface, design-churn commits, and bug archaeology. **This is not the spec.** This is the input the spec will be drafted against.

**What this captures:**
- Every syntactic form the parser currently accepts (the de-facto grammar).
- Every design rule the implementation enforces, with the *why* tied to commits or bugs.
- Designs that landed and were ripped — so the spec doesn't accidentally re-introduce them.
- Implicit semantic rules encoded in codegen tracking tables that aren't documented anywhere a user can read.
- Open questions where the empirical record is unclear or where the current behavior may not be what we want for 1.0.

**What this does not do:** propose a grammar, propose semantics, fix anything. The spec gets written *after* this is reviewed.

---

## 1. Syntactic surface (de-facto grammar)

### 1.1 Keywords (47, from `internal/lexer/token.go`)

```
class    interface  data       enum       const      type
fn       init       new        override   readonly
pub      this       super
import   from       package    use
return   if         else       for        while      break    continue
match    case       is         as         in
not      and        or
true     false      null
var      print      defer      assert
spawn    parallel   timeout    select     with       using
end      ⟨reserved: hint? brace closer in some forms⟩
```

**Reserved-but-rarely-used:**
- `fn` — **DECIDED 2026-05-01: drop entirely** from the keyword map. Type-first declarations are the only function form.
- `Err` is a reserved keyword (`commit 4670bf6`: stdlib `Err` renamed to `BaseError` to free the name). Vestige of the Result<T,E> attempt; not currently used by the parser. **Open whether `Err` reservation stays** post-1.0; for spec, treat as reserved.
- `go` keyword **removed** (`token.go:265`: comment "go removed — use spawn instead").

### 1.2 Operators

| Op | Meaning | Notes |
|---|---|---|
| `+ - * / % **` | arithmetic | `**` is right-associative power |
| `== != < <= > >=` | comparison | **`===`/`!==` dropped 2026-05-01** |
| `&& \|\| !` | boolean | also keyword forms `and`, `or`, `not` |
| `& \| ^` | bitwise (binary) | `&` also has prefix meaning (FFI) |
| `<<` `>>` | shift | not single tokens — two adjacent `<` or `>` (no whitespace gap) |
| `=` `+=` `-=` `*=` `/=` | assignment | |
| `..` `..=` | range | exclusive / inclusive |
| `?.` | safe-nav | `obj?.field`, `obj?.method(args)` |
| `?` | postfix optional type marker (`String?`) | **not** propagation — that was ripped (`commit 290bb6b`). **`??` (null-coalesce) dropped 2026-05-01** |
| `...` | spread | `expr...` in args, `Type...` in param |
| `@` | annotation | `@Json("name")`, `@Avro("name")`, etc. |
| `&` (prefix) | FFI address-of | **only** legal as top-level arg of a Go-library call; rejected elsewhere |

**Tokens dropped 2026-05-01:** `===`, `!==`, `??`, `<-` (channel arrow), `->` (reserved-unused), `:=` (reserved-unused). Channel ops use method-call form (`.send(v)`, `.recv()`) only.

**`&` has dual meaning** — bitwise binary AND vs FFI prefix address-of. Disambiguated by parse position: prefix at start of unary, binary at lower precedence. The validator (`codegen_exprs.go:97-107`) rejects prefix `&` outside FFI argument positions with a compile error.

### 1.3 Top-level declarations

| Form | AST | Example |
|---|---|---|
| Package | `PackageDecl` | `package "myapp/utils"` |
| Import | `ImportDecl` | `import core`, `import fabric/registry` |
| Function | `FnDecl` | `pub (Int, error) parseNum(String s) { ... }` |
| Single-expr fn | `FnDecl` (body wraps return) | `int square(int x) = x * x` |
| Class | `ClassDecl` | `class Dog : Animal { ... }` |
| Sealed class | `ClassDecl{IsSealed:true}` | `sealed class Result { ... }` (variants are `DataClassDecl`) |
| Data class | `DataClassDecl` | `data User(String name, int age = 0)` |
| Interface | `InterfaceDecl` | `interface Speaker { String speak() }` |
| Enum | `EnumDecl` | `enum Color { Red, Green, Blue }` |
| Const | `ConstDecl` | `pub const int VERSION = 1` |
| Type alias | `TypeAliasDecl` | `type JsonObject = Map<String, Object>` |
| Test | `TestDecl` | `test "round-trip" { ... }` (in `*_test.zn`) |

**Function declaration shape** (commit `e42d48b`, 2026-04-15): type-first, no `fn`. `void` literal stands in for "no return type" (parsed as ident match, not a keyword). Java/C#/Dart shape.

### 1.4 Statements

| Form | AST | Notes |
|---|---|---|
| `var name = expr` | `VarStmt` (no `Type`) | type inferred from `expr` |
| `Type name = expr` | `VarStmt` (with `Type`, no `var`) | named type, **`var` is forbidden when type is named** (fail case `var_type_hybrid.zn`) |
| `var x, y = expr` | `TupleVarStmt` | multi-value destructure |
| `target = expr` | `AssignStmt` | also `+=`, `-=`, `*=`, `/=` |
| `return [expr]` | `ReturnStmt` | bare return for `void`; `return v1, v2, err` for tuple |
| `if (cond) { } else { }` | `IfStmt` | **parens required** (fail case `control_flow_bare.zn`) |
| `for (init; cond; post) { }` | `ForStmt` (C-style) | parens required |
| `for item in coll { }` | `ForStmt{IsRange:true}` | parens forbidden |
| `for (i, item) in coll { }` | `ForStmt{IsRange,IndexVar}` | parens around `(i, item)` |
| `while (cond) { }` | `WhileStmt` | parens required |
| `match expr { case ... { } }` | `MatchStmt` | exhaustive over sealed types (codegen errors otherwise) |
| `select { case ... }` | `SelectStmt` | channel multiplex, Go-style |
| `spawn { body } [or { handler }]` | `SpawnExpr` (in stmt position) | virtual thread |
| `parallel for ... { }` | `ParallelForStmt` | bounded with `parallel(max:N) for ... { }` |
| `with (var r = expr, ...) { }` | `WithStmt` | each resource's `.Close()` is deferred |
| `using (var r = expr) { }` | `WithStmt` (overloaded) | resource acquisition shape |
| `lock (mu) { }` | `WithStmt` (overloaded, `Resources[0].Name == "_lock"`) | mutex acquire+release |
| `timeout(dur) { } [or { }]` | `TimeoutStmt` | deadline-bound block |
| `defer expr` | `DeferStmt` | runs at function return |
| `assert cond [, "msg"]` | `AssertStmt` | runtime check |
| `break` / `continue` | `BreakStmt` / `ContinueStmt` | inside loops only |
| `print(expr)` | `PrintStmt` | sugar; prefer `fmt.Printf`/`println` for production |

### 1.5 Expressions (post-parse forms)

`Ident`, `IntLit`, `FloatLit`, `StringLit`, `RawStringLit` (backtick), `StringInterpLit` (`"hi ${name}"`), `BoolLit`, `NullLit`, `BinaryExpr`, `UnaryExpr` (incl. `-x`, `!x`, `&x`), `CallExpr`, `SelectorExpr`, `SafeNavExpr` (`obj?.field`), `IndexExpr`, `SliceExpr` (`arr[lo:hi]`), `ThisExpr`, `SuperCallExpr`, `ListLit` (`[1,2,3]`), `MapLit` (`{"k": v}`), `TupleLit` (`(a, b, c)`), `SpreadExpr` (`xs...`), `LambdaExpr` (`(int x) => x + 1`), `IfExpr` (expression position), `MatchExpr` (expression position), `RangeExpr` (`0..n`, `0..=n`), `CapacityExpr` (`List<T>(cap)`, `Map<K,V>(cap)`), `SizedArrayExpr` (`byte[16]`), `TypeAssertExpr` (`x as T`, `x is T`), `SpawnExpr` (also expression position).

### 1.6 Type expressions

| Form | AST | Example |
|---|---|---|
| Simple | `SimpleType` | `int`, `String`, `MyClass` |
| Qualified | `SimpleType{Name:"core.Schema"}` | `core.Schema` (single-token name with dots) |
| Generic | `GenericType` | `List<int>`, `Map<String, Object>` |
| Optional | `OptionalType` | `String?` (nullable / pointer) |
| Array | `ArrayType` | `int[]`, `byte[]` |
| Function | `FuncTypeExpr` | `Fn<(int, String), bool>` |
| Tuple | `TupleType` | `(int, String, error)` — **return position only** |

`TupleType` is restricted to function/method/Fn-slot return positions (`ast.go:291-298`). A single-element `(T)` is unwrapped at parse time; tuples always have ≥ 2 elements.

---

## 2. Design rules learned the hard way

Each rule has a *why* — a commit, bug, or design conversation that produced it.

### 2.1 Errors-as-values via explicit `error` tail

**Rule.** A function is a thrower iff its declared return signature ends in `error`. Examples: `(Int, error)`, `(error)`, `(T, U, error)`. No body inspection. No auto-widening.

**Why.** Two pivots tried alternatives and were ripped:
- `commit e85a084` (2026-04-15) added try/catch/throw/finally/using. Ripped in `2963d36` (2026-04-24).
- `commit adee3a8` (2026-04-28) added `Result<T, E>` as a built-in marker. Reverted in `3ce091b` (2026-04-29).
- `commit 4d0edcd` (2026-04-28) auto-widened `return thrower()`. Ripped in `0cd6935` (2026-04-29).

**Final form** lands in `2caae44` (2026-04-29) and `0cd6935`: explicit-error-tail-is-the-only-thrower-marker. Syntactic detection.

**How to apply.** Declaring a thrower: `pub (Int, error) parseNum(String s) { ... }`. Returning success: `return 42, null`. Returning error: `return ParseError("...")` (codegen widens to multi-value).

### 2.2 `or { }` handlers consume errors and cast failures

**Rule.** `var x = call() or { ... }` runs the handler if the call returns a non-nil error. The handler has `err` in scope and may `return ...err...`, `continue`, `break`, or supply a default value. Same shape applies to `as T or { ... }` for cast failures.

**Why.** `or { }` survived all three error-handling pivots. Generalized to value-returning calls in `commit d6ab3b2` (2026-04-16, "v2 slice 2"). Generalized to `as` in `7542e68` (2026-04-28, "as returns an error instead of panicking").

**Variant.** `or match err { case ErrType -> { ... } case _ -> { ... } }` for type-switching errors. Less common; spec needs to confirm if this stays.

### 2.3 Explicit `&` at FFI seams; rejected elsewhere

**Rule.** Prefix `&x` is legal only as the top-level argument expression of a Go-library call. Two FFI seams recognized:
- Package call: `pkg.Func(...)` where `pkg` is a `[deps]` import alias.
- Method call on a Go-typed receiver: `var.Method(...)` where `var`'s type came from a Go function (tracked via `varGoTypes`).

Anywhere else (assignments, returns, var inits, args of zinc-side calls, nested sub-expressions): compile error.

**Why.** Originally the compiler had a hand-curated `implicitPointerParams` table that auto-`&`'d at known sites (json.Unmarshal, hamba.Unmarshal, etc.). Replaced by explicit-`&` in `commit 207bb7c` (2026-04-30) — *"explicit `&` for FFI, drop hand-curated implicitPointerParams"*. Maintenance trap: every new Go lib needed a compiler patch.

**Bug archaeology.** When the implicit table was removed, 13 `json.Unmarshal(data, target)` sites in zinc-flow-go silently no-op'd (the map was passed by value). Caught after a fresh-binary rebuild surfaced the regression. Lesson encoded in the rule: the FFI seam must be visible at the call site.

**Compiler-side rules.**
- For **explicit `*T`** Go param: compiler auto-inserts `&`. User writes `f(x)`, codegen emits `f(&x)`. (E.g., method receivers for pointer-receiver methods.)
- For **`any` / `interface{}`** Go param: user **must** write `&x` explicitly. (E.g., json.Unmarshal, hamba.Unmarshal, dec.Decode.)

### 2.4 FQDN required on collision (no auto-precedence)

**Rule.** When a bare name is exported by two or more imports (e.g., `Schema` from both `core` and `hambaAvro`), the user must qualify (`core.Schema` or `hambaAvro.Schema`). No "first wins" / "zinc wins over Go" / etc.

**Why.** Today (2026-05-01): the user explicitly rejected a precedence-based fix to the codegen, on grounds that "the compiler shouldn't automagically fix it for you." Rule matches Java/Rust/C#/TS behavior. Python-style silent winners are not the model.

**Decision (2026-05-01).** Collision must produce a **Zinc-level error pointing at qualification**, not let Go's compiler surface `undefined: X` downstream. The error message names the colliding packages and suggests both qualified forms (e.g., `ambiguous bare name "Schema" — exported by both core and hambaAvro. Use core.Schema or hambaAvro.Schema.`).

### 2.5 `var` is only for type inference

**Rule.** `var name = expr` (type inferred). `Type name = expr` (named type, no `var`). The hybrid `var Type name = expr` is rejected.

**Why.** Fail case `examples-fail/var_type_hybrid.zn` documents three rejected shapes (class field, local with no init, local with redundant `var`). One way to write each form, no ambiguity.

### 2.6 Parens required around control-flow conditions

**Rule.** `if (cond) { ... }`, `while (cond) { ... }`, `for (init; cond; post) { ... }`, `match (subject) { ... }`. Bare `if cond { }` is a compile error.

**Why.** Fail case `control_flow_bare.zn`: matches Java/C#/Swift; eliminates parser ambiguity with brace literals at the start of `cond`.

### 2.7 `pub` for cross-package visibility only

**Rule (decided 2026-05-01).** Within a Zinc package, all declarations are visible across files — no `pub` required. `pub` is only needed for cross-package access. Matches Go's package-level scoping model.

**Codegen implication.** A non-`pub` name must be emitted with the same Go identifier at the definition site and at every call site. Today's behavior — lowercasing the def and capitalizing some call sites — is a bug under this rule. The fix: choose one casing for non-`pub` names (lowercase, matching Go's "unexported" convention) and use it consistently. Same-package cross-file access falls out of Go's package scoping for free.

**Bug retired by this decision.** §7.5's `coerceForSchema → CoerceForSchema` mismatch becomes a codegen consistency bug to fix (always lowercase non-pub names), not an unresolved spec question.

### 2.8 Multi-value returns via `TupleType`

**Rule.** `(T, U, error) f(...)` declares a function returning three values. `TupleType` is restricted to function/method/Fn-slot return positions. Cannot be used as a value type or var type.

**Why.** `commit f77a9d5` (2026-04-29) added `TupleType` as the foundation for the new error-tail design. Before this, multi-value returns were a special-case codegen path.

**Destructure.** `var x, y, err = f()` → `TupleVarStmt`.

### 2.9 Class ctors return `*T`; data class ctors return `T`

**Rule.** Calling `MyClass(args)` where `MyClass` is a `class` returns `*MyClass` (Go pointer). Calling `MyData(args)` where `MyData` is a `data` class returns `MyData` (value).

**Why.** Class instances have identity (mutable state, methods); data classes are records. Pointer-vs-value semantics tracked in `g.dataClasses` and `g.structs` codegen maps.

### 2.10 No `fn` keyword for function declarations

See 1.1. `fn` is in the lexer but not consumed by the v2 function-decl parser.

### 2.11 Go keyword removed (use `spawn`)

`go { body }` was the original goroutine syntax; renamed to `spawn { body }` to avoid Zinc/Go keyword collision in user-facing docs (`token.go:265` comment).

### 2.12 `as` and `is` accept qualified types

**Rule.** `expr as core.Schema`, `obj is fabric.Processor` are valid. `commit a8b3e9d` (2026-04-29).

**Why.** Cross-package generics + sealed-variant matching needed qualified type names in `as`/`is`. Original parser only accepted single-token identifiers.

---

## 3. Designs that landed and were ripped

These are in the spec only as **rejected alternatives** — so future contributors don't re-propose them without knowing what failed.

| Design | Landed | Ripped | Reason |
|---|---|---|---|
| try/catch/throw/finally/using | `e85a084` (2026-04-15) | `2963d36` (2026-04-24) | "errors as values is confusing everyone" pivot, then reverted to errors-as-values when the throw/catch surface area didn't fit Go's panic/recover model cleanly |
| `Result<T, E>` built-in marker | `adee3a8` (2026-04-28) | `3ce091b` (2026-04-29) | Required pervasive lambda-target awareness; chosen design (explicit `error` tail) achieved the same goals with less compiler complexity |
| `?` postfix propagation operator | `08b2d03` (2026-04-16) | `290bb6b` (2026-04-29) | Implicit propagation conflicted with explicit-error-tail-as-the-only-thrower-marker rule |
| Implicit auto-widen of `return thrower()` | `4d0edcd` (2026-04-28) | `0cd6935` (2026-04-29) | Magical; explicit-error-tail-as-the-only-thrower-marker rule rejects body inspection |
| `concurrent` / `static` / `abstract` modifiers | (initial) | `517d4eb` (2026-04-28) | Reduce surface area; static/abstract didn't fit Zinc's class model; concurrent was redundant with `spawn`/`parallel` |
| Slashy imports (`import "path/to/pkg"`) | (initial) | `2963d36` (2026-04-24) | Confusing with Go-package import syntax; replaced with `import alias` form |
| Streams / slices / maps in stdlib | (initial) | `2963d36` (2026-04-24) | Underspecified; deferred to post-1.0 |
| `fn` keyword for function decl | (initial) | `e42d48b` (2026-04-15) | Replaced with type-first declaration to match Java/C#/Dart |
| Implicit pointer table (`implicitPointerParams`) | (early) | `207bb7c` (2026-04-30) | Maintenance trap; replaced by explicit `&` at FFI seams |

**Vestiges from these pivots still in the codebase:**
- `Err` keyword reservation (was the stdlib type, renamed in `4670bf6`)
- `OrMatchCase` AST node ("exception type" wording in `ast.go:426` — was for try/catch)
- `ResolvedType` fields on `BinaryExpr`/`ListLit`/`MapLit`/`VarStmt` — placeholder for typechecker output that never wired up
- `fn` keyword still in `token.go`/`keywords` map
- The `internal/typechecker/` directory itself (810 lines, dead code — see §6)

---

## 4. FFI semantics (current behavior)

### 4.1 Two seam types

1. **Package call:** `pkg.Func(args)` where `pkg` is a `[deps]` import alias. Detected via `g.importMap[pkg]` lookup in `formatCallExpr`.
2. **Method call on Go-typed receiver:** `var.Method(args)` where `var`'s Go type came from a Go function call. Detected via `g.varGoTypes[var]` lookup. Added `2026-05-01` after the shadow-gate self-stomp bug.

Both paths set `g.addrOfAllowed = true` while formatting each arg, allowing top-level `&x`.

### 4.2 Auto-`&` vs explicit-`&`

| Go param shape | User writes | Codegen emits | Why |
|---|---|---|---|
| `*T` (explicit pointer) | `f(x)` | `f(&x)` | Static signature is unambiguous |
| `any` / `interface{}` | `f(&x)` | `f(&x)` | Runtime contract requires pointer; no signature signal; user must opt in |
| Multi-value Go return | `var a, b, c = f()` | `a, b, c := f()` + per-slot `varGoTypes` populated | Tracking added 2026-05-01 |

### 4.3 Validator

`codegen_exprs.go:97-107`: any prefix `&` reached via `formatExpr` with `addrOfAllowed=false` records a compile error. `addrOfReported` map prevents double-reporting.

---

## 5. Scoping and visibility

### 5.1 Bare-name resolution order (codegen-side)

1. Local var / param / class field (lexical scope)
2. Same-package siblings (registered via `SetSiblingExports`)
3. Imported zinc subpackage exports (`g.subpkgExports`)
4. Imported Go-package exports (`g.importMap` → `g.goResolver.ListExports`)
5. Go builtin (e.g., `error`, `len`, `make`) — never shadowed

Collision detection: if step 3 or step 4 produces the same name from two different packages, that name is removed from `unqualifiedNames` and added to `unqualifiedCollisions`. Bare use becomes a fall-through to the unqualified name in generated Go (which is then rejected by Go's compiler).

### 5.2 The shadow gate (`isUserScopeShadow`)

Returns true when a name is already claimed by user scope (any of: `currentClass.fields`, `currentParams`, `currentLocals`, `varStructTypes`, `varTypes`, `varGoTypes`, `varTypeExprs`).

**Used at every site where a bare ident might be interpreted as a package alias.** Without the gate, a user-named field/var/param that happens to share a name with an imported package gets misrouted (`ZCA-10` repro: `class Fabric { var processors = ... }` with a sibling `processors/` package).

**Gotcha discovered 2026-05-01:** when adding new tracking fields (e.g., `varGoTypes` populator for tuple slots), the new field's membership counts as user scope. This can stomp logic that *uses* the tracking for a different purpose. Today's bug: I added `varGoTypes["dec"] = *Decoder` to detect FFI method calls, but the FFI-detection branch was gated by `!isUserScopeShadow`, which now evaluated to `false` because `dec` was in `varGoTypes`. Self-stomp.

**Lesson for the spec:** the resolver and the type-info tracker are two different queries. Today they share the same table set. They shouldn't.

### 5.3 `pub` and cross-file visibility

See §2.7. Effectively: package-level visibility within a Zinc package (matching Go's behavior); `pub` is required for cross-package access.

---

## 6. Type system gaps

### 6.1 The dead typechecker

`internal/typechecker/typechecker_v2.go` (810 lines) has **zero call sites in the live pipeline**. Was wired between `commit 810b30f` (2026-03-18) and `commit 7155aed` (2026-03-25, "Remove Go compiler — Java compiler is now the sole implementation"). When the Go transpiler was restored in `commit 8000081` (2026-04-23), the wire-up didn't come back. Silent regression.

**What it can do:** scope tracking, basic type inference (`int`, `String`, `bool`, etc.), function arg-count + arg-type checks against signatures, return-value type compatibility, all-paths-return validation, type narrowing on `is`.

**What it can't do (Java-era vestiges):**
- Java-flavored primitives (`byte`, `short`, `char`)
- `ResolveZincMethodReturn` is a hand-curated table for `String.upper`, `List.size`, etc.
- Recognizes `Result[T]` and `Err()` calls (dead since the Result revert)
- Recognizes `isinstance(x, T)` (Python-ism, removed elsewhere)
- `go_types.go` has stubs `ZincToJavaClass` and `MethodThrows` returning empty/false ("no-op stub for Go backend")

**What it has no concept of:**
- Go-imported types (`*ocf.Decoder`, `*sync.Mutex`)
- Multi-value Go returns (`TupleVarStmt` sets all slots to `typeAny`)
- Method calls on Go-typed receivers
- Generics (`LambdaExpr → typeAny`; no constraint solving)
- Cross-package symbols (no analog of `subpkgExports`)
- `pub` visibility
- The `&` FFI validator

### 6.2 What checks types right now

The Go compiler. Codegen has only **5 explicit `g.compileError(...)` sites:**
1. `&` outside FFI position (`codegen_exprs.go:101`)
2. Match exhaustivity (`codegen_stmts.go:1465`)
3. Three unreachable type-resolution failures (`codegen_types.go:906`, `911`, `1093`)

Everything else falls through to generated Go. Type errors surface as `undefined: X` / `cannot use Y as Z` from `go build`/`go test`. `//line` directives map back to `.zn` lines, but the *message* is Go's.

### 6.3 24+ codegen tracking fields encoding implicit semantics

From `Generator` struct (`codegen.go:30-164`). Each is an unwritten semantic rule:

| Field | Implicit rule encoded |
|---|---|
| `varTypes` | bare ident → element type (string, for scalar tracking) |
| `varTypeExprs` | bare ident → original AST type (for generics) |
| `varGoTypes` | bare ident → Go-resolved type (for FFI detection) |
| `varStructTypes` | bare ident → struct type name (for method dispatch) |
| `ptrVars` | bare ident is bound to a pointer (`T?` returns) |
| `funcSigs` | function name → param list (for callable validation) |
| `funcReturnTypes` | function name → Go return type (for tuple destructure) |
| `funcReturnsOptional` | function name returns `T?` |
| `errorFuncs` | function is a thrower (has `error` tail) |
| `dataClasses` | data class names (constructors return value) |
| `dataClassDecls` | data class field decls (for cross-package match destructure) |
| `structs` | regular class names (constructors return `*T`) |
| `interfaces` | interface names (Go interface emit) |
| `typeAliases` | alias name → underlying type expr |
| `pubNames` | declarations marked `pub` |
| `currentFields/Params/Locals` | active lexical scope |
| `renamedVars` | shadow renames (e.g., user var `error` → `_error`) |
| `subpkgExports` | cross-package export inventory |
| `subpkgStructs` | cross-package class decls (for method lookup) |
| `subpkgDataFields` | cross-package data class fields |
| `subpkgTypeAliases` | cross-package type aliases |
| `localDataFields` | same-package data class fields |
| `importMap` | import alias → Go module path |
| `importGoAliases` | Go path → local alias (when alias differs from package name) |
| `importAliases` | import alias → module path |
| `typeImports` | short type name → qualified Go name |
| `activeTypeParams` | currently-in-scope generic type parameters |
| `unqualifiedNames` | bare name → resolved entry (subpkg or Go pkg) |
| `unqualifiedCollisions` | bare name → list of colliding pkgs |
| `addrOfAllowed` | `&` permitted at current expression position |
| `addrOfReported` | dedup for misplaced-`&` errors |

**The duplication problem.** The typechecker has 3 of these (`fnSigs`, `methodSigs`, `parentTypes`); codegen has 24+. Each new feature adds another tracking map to codegen because the typechecker isn't running and there's no shared schema for "this ident has this type / kind / scope."

---

## 7. Bug archaeology — rules that weren't explicit

Each bug here is a rule the spec needs to make explicit.

### 7.1 The shadow-gate self-stomp (2026-05-01) — STRUCTURAL FIX REQUIRED

**Bug.** Added `varGoTypes` populator for tuple-var slots so `var dec, _ = hambaOcf.NewDecoder(...)` would tag `dec` as Go-typed. Used the tag to detect FFI on method calls. But the FFI-detection branch was gated behind `!isUserScopeShadow`, which (because `varGoTypes` membership counts as user scope) evaluated to `false`. The detection branch never ran.

**Rule for spec.** Resolution and type-info-tracking must be conceptually separate: bare-name → which package question vs typed-var → which type question. Conflating them causes circular gating.

**Decision (2026-05-01).** The band-aid fix shipped today (check `varGoTypes` outside the shadow gate) is **not** the long-term fix. The structural answer is the typechecker rewire / spec-driven re-arch — a real bind/typecheck phase that resolves names + binds types up front, with codegen consuming the resolved tree. Until that lands, the band-aid stays in place to keep zinc-flow-go green; after the rewire, both the shadow-gate concept and the codegen's 24+ tracking fields collapse into a single resolved-AST consumer. **Phase 0 → Phase 1 (formal grammar) → Phase 2 (semantic spec) → Phase 3 (rebuilt pipeline) is the path.**

### 7.2 The 13-site `&` regression (2026-04-30)

**Bug.** `implicitPointerParams` was removed from the compiler in `commit 207bb7c`. Existing user code had 13 `json.Unmarshal(data, target)` sites (no `&`) that previously got auto-`&`'d. Removal silently broke them — Unmarshal received a value, no-op'd, decoded data was empty. Caught by failing OCF round-trip tests after a fresh compiler binary install.

**Rule for spec.** Whenever a Go FFI param has runtime-pointer-required behavior but a static `any` signature, the user must write `&x`. Compiler does not paper over this.

### 7.3 Schema/Field collision (2026-05-01)

**Bug.** zinc-flow-go's `avro_binary.zn` imports both `core` and `hambaAvro`. Both export `Schema` and `Field` (different types). Bare `Schema(...)` was ambiguous; codegen put both into `unqualifiedCollisions` and removed both from `unqualifiedNames`. Bare uses fell through to Go's compiler with `undefined: Schema`.

**Rule for spec.** Ambiguous bare names are user errors. Resolution is to qualify (`core.Schema` or `hambaAvro.Schema`). Should be a Zinc-level error message, not a Go-level `undefined: X`.

### 7.4 Stale binary parse error (2026-04-30 → 05-01)

**Bug.** Prefix-`&` parser (`v2ParseUnary`) was added late. Pre-existing installed `zinc-go` binary didn't have it. User-side `&got` reported `unexpected token UNKNOWN ("&")`. Confused multiple debugging sessions before the binary rebuild was discovered.

**Rule for spec / tooling.** Compiler version + parser-feature compatibility needs a version stamp visible at `zinc --version`. Cached binaries are a regular gotcha during dev.

### 7.5 Pub/non-pub cross-file leak (2026-05-01) — RESOLVED

**Bug.** `coerceForSchema` (no `pub`) was defined in `avro_binary.zn`. Called from `avro_ocf.zn` (same package). Codegen exported the call site as `CoerceForSchema` (capitalized) but emitted the definition as `coerceForSchema` (lowercase). `undefined: CoerceForSchema`.

**Decision (2026-05-01).** No `pub` needed within a package — Go-style package-level scoping (see §2.7). Codegen action: always lowercase non-`pub` names at both definition and call sites; rely on Go's package scoping for cross-file access. Capitalize only when `pub` is set.

---

## 8. Open questions for the spec — RESOLVED 2026-05-01

All 15 walkthrough decisions below. Each becomes an entry in the decision log (§ end).

1. **`fn` keyword** → **drop entirely.**
2. **`OrMatchCase` (`or match err { case T -> ... }`)** → **drop.** Removes `OrHandler.MatchCases` field; users compose via `var x = f() or { match err { ... } }`.
3. **`?` (postfix)** → **stays as optional-type marker only.** `??` token **dropped**.
4. **`===` / `!==`** → **dropped.** Plain `==` / `!=` are the only equality operators. **`==` semantics: Go-identity** — pointer-compare for class instances, field-by-field for data classes, byte-equal for primitives, **compile error for maps and slices**.
5. **`Type x = call() or { ... }`** → **allowed.** Symmetric with `var x = call() or { ... }`; handler runs on err, value slot binds to `x`.
6. **Sealed-variant syntax** → **nested data classes inside the sealed parent body, newline-separated:**
   ```
   sealed class Result<T> {
       data Ok(T value)
       data Err(String message)
   }
   ```
7. **`pub` semantics** → **only required for cross-package** (resolved in §2.7).
8. **`with`/`using`/`lock`** → **keep one `WithStmt` AST node**, document the overload (`Resources[0].Name == "_lock"` is mutex; otherwise `.Close()`-deferred resources).
9. **Generic constraints** → **add bounds** `<T : Comparable, U : Hashable>`. Real type system; constraint solver required.
10. **`@Annotation`** → **built-in only / fixed set** for 1.0 (`@Json/@Yaml/@Toml/@Avro/@Test`). User-defined annotations are a 1.x feature.
11. **`select` syntax** → **method-call form** (`case x = ch.recv():`).
12. **Channel send/recv** → **method-call form only**; arrow tokens (`<-`) **removed from lexer**.
13. **Statement separator** → **newline OR `;`** (optional). `;` allows multiple short statements on one line (`var x = 1; var y = 2`). Matches Kotlin/Java tolerance. Newline is the canonical separator; `;` is the affordance for one-liners.
14. **Cross-package type system** → **full cross-pkg generic instantiation supported in 1.0** (including sealed variants matched across packages).
15. **`null` in type system** → **null-safe.** Only `T?` accepts `null`; `T` rejects `null` at compile time. Significant break from current behavior — codegen + typechecker work to enforce.

---

## 9. What the spec should produce

Based on §1-8, the spec needs at minimum:

1. **Lexical grammar** — token types, keywords, operators, whitespace/comments. Already pretty clean; lift from `lexer.go` and `token.go`.
2. **Syntactic grammar** — EBNF for declarations, statements, expressions, type expressions. Lift from the parser; cross-check against examples.
3. **Static semantics**
   - Scoping rules (lexical, package-level, cross-file).
   - Visibility (`pub` model).
   - Name resolution (unqualified vs qualified, collision behavior).
   - Type system (compatibility, inference, generics).
   - FFI rules (`&` placement, Go-typed receivers, pointer-vs-value classes).
   - Error-handling shape (explicit-error tail, `or { }`).
4. **Dynamic semantics** — how each construct behaves at runtime (mostly delegated to Go's runtime; spec just needs to document what's exposed).
5. **Compatibility commitments** — what's stable post-1.0, what's experimental, what can change.

---

## 10. Sign-off checklist for next phase

Before drafting the formal grammar, the user should review this doc and confirm or override:

- [x] **§2 design rules** *(walkthrough 2026-05-01)*
- [x] **§2.4 — FQDN-on-collision produces a Zinc-level error pointing at qualification** *(2026-05-01)*
- [x] **§3 — ripped designs do not return** *(2026-05-01)*
- [x] **§4 — FFI: auto-`&` for explicit `*T`, explicit `&` for `any`** *(2026-05-01)*
- [x] **§5.1 — name-resolution order: local → same-pkg → zinc subpkg → Go imports → Go builtins** *(2026-05-01)*
- [x] **§2.7 / §7.5 — `pub` only needed across packages** *(2026-05-01)*
- [x] **§5.2 / §7.1 — typechecker rewire / spec-driven re-arch is the structural fix; band-aid stays until then** *(2026-05-01)*
- [x] **§7 bug-derived rules — all 5 confirmed** *(2026-05-01)*
- [x] **§8 open questions — all 15 answered** *(2026-05-01)*
- [ ] Anything missing — stuff in the user's head that didn't surface from code archaeology

After sign-off, Phase 1 is the formal EBNF grammar drafted from §1, validated against `examples/` and `examples-fail/`.

---

## Decision log

| Date | Decision | Sections affected |
|---|---|---|
| 2026-05-01 | `pub` only required for cross-package access; intra-package cross-file access is free (Go-style package scoping) | §2.7, §7.5 |
| 2026-05-01 | Shadow-gate self-stomp fixed by typechecker rewire / spec-driven re-arch, not by ad-hoc patches | §5.2, §7.1 |
| 2026-05-01 | FQDN-on-collision is a Zinc-level error with a "use X.foo or Y.foo" message — not deferred to Go's `undefined: X` | §2.4 |
| 2026-05-01 | All §3 ripped designs are confirmed final — try/catch, Result<T,E>, `?` propagation, auto-widen, `concurrent`/`static`/`abstract`, slashy imports, stdlib streams/slices/maps, `fn` decl, `implicitPointerParams` — none return | §3 |
| 2026-05-01 | FFI auto-`&` for explicit `*T` Go params; explicit `&x` required for `any` / `interface{}` Go params | §4 |
| 2026-05-01 | Bare-name resolution order: local scope → same-pkg → zinc subpkg → Go imports → Go builtins | §5.1 |
| 2026-05-01 | §7.2 confirmed (FFI `&` rule, restatement of §4); §7.3 confirmed (collision rule, =§2.4); §7.4: `zinc --version` with parser-feature tag **required for 1.0** | §7 |
| 2026-05-01 | `fn` keyword **dropped entirely** | §1.1, §8.1 |
| 2026-05-01 | `OrMatchCase` / `or match err { case T -> ... }` **dropped**; `OrHandler.MatchCases` field deletes | §8.2, ast.go:418 |
| 2026-05-01 | `??` (null-coalesce) **dropped**; token removed from lexer | §1.2, §8.3 |
| 2026-05-01 | `===` / `!==` **dropped**; only `==` / `!=` for equality | §1.2, §8.4 |
| 2026-05-01 | `==` semantics: **Go-identity** — pointer-compare for class instances, field-by-field for data classes, byte-equal for primitives, compile error for maps/slices | §8.4 follow-up |
| 2026-05-01 | `Type x = call() or { ... }` **allowed** (named-type var with or-handler); symmetric with `var` form | §8.5 |
| 2026-05-01 | Sealed-variant syntax: nested data classes inside the sealed parent body, **newline-separated** | §8.6 |
| 2026-05-01 | `with`/`using`/`lock` **keep one `WithStmt` node**; document the overload | §8.8 |
| 2026-05-01 | Generic **bounds added**: `<T : Comparable, U : Hashable>`. Constraint solver required | §8.9 |
| 2026-05-01 | `@Annotation` **built-in only / fixed set** for 1.0 (`@Json/@Yaml/@Toml/@Avro/@Test`); user-defined deferred to 1.x | §8.10 |
| 2026-05-01 | `select` syntax: **method-call form** (`case x = ch.recv():`) | §8.11 |
| 2026-05-01 | Channel ops: **method-call form only**; arrow tokens (`<-`) **removed from lexer** | §1.2, §8.12 |
| 2026-05-01 | Statement separator: **newline OR `;`** (optional). `;` lets users put multiple short stmts on one line (Kotlin-style tolerance). | §8.13 |
| 2026-05-01 | Cross-pkg type system: **full cross-pkg generic instantiation supported in 1.0** (including sealed variants across packages) | §8.14 |
| 2026-05-01 | `null` in type system: **null-safe** — only `T?` accepts `null`; `T` rejects `null` at compile time | §8.15 |

