# Zinc — semantics (1.0 target)

> **Keyword note (2026-05):** the error-handler keyword is now `catch` (was `or`). The bare `or` token was removed in favour of `||` for boolean OR. Examples and grammar in this doc have been updated to `catch { ... }`; commit history retains the original `or { ... }` rationale.

**Status:** Phase 2 deliverable. Static and dynamic semantics for the syntactic productions defined in `01-grammar.md`. The type system has its own doc (`03-type-system.md`) — this one covers everything else: scoping, visibility, name resolution, error handling, control flow, concurrency, FFI, annotations, and the runtime model.

**Authority order.** When `01-grammar.md`, `02-semantics.md`, and `03-type-system.md` disagree, the conflict is itself a bug. None of these docs override the others — they describe different aspects of the same language and must agree.

---

## 1. Compilation unit

A `.zn` file is a compilation unit. Multiple files share a Zinc **package** (declared via `package "path"` or default `main`). All files in a package are visible to each other without `pub`.

Compilation order, from a tooling/IDE perspective:

1. Lex + parse each file into AST.
2. **Bind** — resolve every `Ident` to a definite symbol; produce a side-map keyed by AST node identity.
3. **Typecheck** — assign every expression a type; verify compatibility, generic constraints, null safety; produce a side-map keyed by AST node identity.
4. **Emit** — codegen consumes parse tree + bind side-map + typecheck side-map; never asks "what does this name mean?" or "what's this type?" at emit time.

Phase 3 of the rebuild reorganizes the compiler around this 4-step model. The bind and typecheck phases are what `internal/typechecker/` will become.

---

## 2. Scoping

### 2.1 Lexical scopes

| Scope | Introduced by | Names visible |
|---|---|---|
| Block | `{ ... }` | Names declared in this block + all enclosing scopes |
| Function | `fn_decl`, `lambda_expr` | Params + body's block scope |
| Method | `method_decl` | `this`, all class fields, params, body's block scope |
| Constructor | `ctor_decl` | `this`, `super`, all class fields, params, body's block scope |
| Class | `class_decl`, `sealed_class_decl`, `data_class_decl` | Field declarations within the class body, methods, ctor |
| File | top-level decls in one `.zn` file | All file-top-level decls + imports |
| Package | the union of all files in a package | All package-top-level decls (regardless of `pub`) |

Inner scopes shadow outer scopes: a local var named `port` shadows a class field `port`, which shadows a top-level const `port`.

### 2.2 Class scopes

`this` is implicitly available in:
- Constructor body.
- Method body.
- Field initializers (with caveats — see §2.5).

`super` is available in:
- Constructor body — calls the parent constructor (`super(args)`).
- Method body — calls the parent's same-named method (`super.method(args)`).

### 2.3 Cross-package access

Crossing a package boundary requires:
1. The target declaration is `pub`. AND
2. The target's package is imported in the calling file (`import target_pkg`).
3. Either bare-name access (if no collision; see §3) or qualified `pkg.Name`.

A non-`pub` declaration is **invisible** outside its package — at any access type, including reflection-style introspection. Codegen emits non-`pub` Zinc names with lowercase Go identifiers consistently at definition and call sites.

### 2.4 Shadow rule

A user-introduced name (local, param, class field, `this`-bound name) **always wins** against any package-import name with the same identifier.

```zinc
import processors          // package alias `processors`

class Fabric {
    var processors = List<Processor>[]      // field

    void register(Processor p) {
        processors.add(p)                    // refers to the field, NOT the package
    }
}
```

This is enforced at the bind phase: an `Ident` whose name matches a local/param/field is bound to that local/param/field; package-import resolution is never attempted.

### 2.5 Field initializer constraints

A field initializer may reference:
- Other fields in the same class **only if those fields appear earlier in the class body**. The class body is parsed top-to-bottom; forward references are not allowed.
- Top-level constants and pure functions.
- `this` is **not** in scope for field initializers — they run before the constructor body. Use the constructor body for `this`-dependent setup.

```zinc
class C {
    int a = 5
    int b = a * 2          // OK — `a` declared above
    int c = d              // ERROR — `d` not yet declared
    int d = 7
}
```

---

## 3. Name resolution

### 3.1 Resolution order

For an unqualified `Ident` at use position, the bind phase consults sources in this order. **First match wins.**

1. **Local scope.** Vars declared by `var_stmt`/`tuple_var_stmt`, `for`/`while` loop vars, lambda params, `with`/`using`/`lock` resource bindings, match-arm pattern bindings.
2. **Function scope.** Params of the enclosing function/method/lambda.
3. **Class scope.** Fields of the current class (if inside a method/ctor).
4. **Same-package decls.** All top-level decls in any file of the current package, regardless of `pub`.
5. **Imported zinc subpackage exports.** For every `import alias` where `alias` resolves to a zinc subpackage, the bind phase considers that package's `pub` decls.
6. **Imported Go-package exports.** For every `import alias` where `alias` resolves to a Go module via the `[deps]` table, the bind phase considers the Go package's exported identifiers.
7. **Go builtins.** `error`, `len`, `cap`, `make`, `new`, `append`, `copy`, `delete`, `close`, `panic`, `recover`, `print`, `println`, `min`, `max`, `clear`, etc. These are never shadowable by imports (but are shadowable by local scope).

### 3.2 Collision

If steps 5 and 6 together produce **two or more matches** for the same bare name (e.g., `Schema` from both `core` and `hambaAvro`), the bind phase records a **collision** and emits a compile error:

```
error: ambiguous bare name "Schema" — exported by both core and hambaAvro.
       use core.Schema or hambaAvro.Schema to disambiguate.
```

The error is recorded once per collision-name per file, with the source position of the first ambiguous use.

Note: a collision is **not** a runtime/codegen issue — it's caught at bind time, before typecheck. The user must qualify; no auto-precedence.

### 3.3 Qualified access

`pkg.Name` is the disambiguated form. Bind looks up `pkg` in the import table, then `Name` in `pkg`'s exports. Either lookup failing is a compile error with a position.

`a.b.c` is parsed as `(a.b).c`. Multi-dot qualified names (`fabric.router.EQ`) are valid as type or value references.

---

## 4. Mutability

| Form | Mutability | Reassignment | Notes |
|---|---|---|---|
| `var x = expr` | mutable | yes | typical local |
| `const x = expr` | immutable | no | local constant |
| `var Type field` | mutable | yes | class field, defaults to type's zero value |
| `readonly Type field = expr` | immutable | no | class field, set once at field-init time |
| `init Type field` | immutable post-ctor | yes during ctor, no after | set in `init(...)` body, frozen after return |
| `const Type field = expr` | immutable | no | class const, default required |
| Data class params | immutable | no | data class fields are immutable by design |

**Reassignment** means `x = newvalue`. Internal mutation of the referenced object (`list.add(x)`) is independent — it depends on the type's own contract.

---

## 5. Error handling

### 5.1 Thrower classification

A function or method is a **thrower** iff its declared return type ends in `error`:

```zinc
(int, error) parseNum(String s)        // thrower
(error) writeAll(Writer w)             // thrower (bare-error)
(int, String, error) lookup(String k)  // thrower
int square(int x)                      // not a thrower
void run()                             // not a thrower
```

Detection is **purely syntactic** — bind phase reads the return-type clause. No body inspection. No transitive auto-widening.

### 5.2 Caller obligations

When a thrower is called, the caller **must** consume the error. Three legal forms:

```zinc
// Form A: explicit destructure
var x, err = parseNum(s)
if (err != null) { return ... }

// Form B: inline or-handler — handler runs on err != null
var x = parseNum(s) catch {
    return 0, err
}

// Form C: same-package re-throw — caller is itself a thrower
(int, error) wrap(String s) {
    var x = parseNum(s) catch { return 0, err }
    return x, null
}
```

Calling a thrower without consuming the error is a compile error (the value slot is bound but the error slot is unhandled).

### 5.3 Error values

Any value that satisfies the `Error()` method (Go's `error` interface) can be used in the error slot. Stdlib provides `BaseError` as the conventional root; user code typically subclasses it:

```zinc
class ParseError : BaseError {
    init(String msg) { super(msg) }
}

(int, error) parseNum(String s) {
    if (s == "") {
        return ParseError("empty input")    // codegen widens to (zero, ParseError(...))
    }
    return 42, null
}
```

`return X` where `X` satisfies the error interface, in a thrower whose return is `(T1, ..., Tn, error)`, lowers to `return zero1, ..., zeroN, X`. This is the **only** automatic widening.

### 5.4 `catch { }` semantics

The `catch { }` block runs when the failable expression yields a non-null error. Inside the handler:
- `err` is bound to the error value.
- The handler may `return ...err...`, `continue`, `break`, supply a default value (single-statement `catch { default }` form), or `match (err) { ... }` to type-switch.
- The handler is **not** a closure capturing the original call site's value slot — value bindings happen only on the success path.

```zinc
var content = readFile(path) catch {
    log.warn("read failed: ${err}")
    return null
}
// `content` is bound only when readFile succeeded.
```

### 5.5 `as T catch { }` for cast failure

```zinc
var t = v as Target catch { return null }
```

If the runtime cast fails, the handler runs with `err` bound to a `CastError` describing the failure. Same semantics as the throw path.

### 5.6 Dataflow `Failure` is not error handling

`ProcessorResult.Failure(reason, ff)` is a sealed-variant of `ProcessorResult`. It's a **value**, not an error. Routing it back through a flow's "failure" connection is a deliberate dataflow choice. Never throw from a processor's `process(ff)` for routine FlowFile failures — the throughput cost of per-FF panic/recover is unacceptable.

The distinction:
- **Error**: something went wrong that the *caller* must handle.
- **Failure**: this FlowFile didn't transform; route it to the failure connection. Caller's downstream processors decide the policy.

### 5.7 `null` in error slots

The error slot is `null` on success. Patterns that compare `err != null` are idiomatic:

```zinc
var x, err = f()
if (err != null) {
    // failed
}
```

`null` is the only nil-value keyword in Zinc — `nil` is **not** a Zinc keyword; the lexer treats `nil` as an IDENT, which only resolves in FFI contexts (Go's builtin `nil`). Spec, source, and stdlib all use `null` consistently.

Type-system note: `error` is implicitly nullable. A function with return signature `(T, error)` returns `value, null` on the success path without needing the `error?` annotation. This is the lone exception to spec §3.x's "null is compatible only with `T?`" rule, and it preserves the idiomatic Go-style success-path shape.

---

## 6. Control flow

### 6.1 `if` / `else`

Standard. Parens required around condition. `else if` chains by repeating `if`. There is no expression-position `if` outside of `if_expr` (which uses `if cond : a else : b` form, not braces).

### 6.2 `for`

Two forms:
- C-style: `for (init; cond; post) { ... }` — `init` is a `var_stmt` or `assign_stmt`; `cond` is a `bool` expr; `post` is an `assign_stmt`.
- Range: `for item in coll { ... }` or `for (idx, item) in coll { ... }`. Iterates over collections (list, map, range, channel).

For map iteration: `for (k, v) in mymap { ... }` iterates entries. Iteration order is **unspecified** (matches Go's map iteration).

### 6.3 `while`

`while (cond) { ... }`. Pre-test loop. There is no `do-while`.

### 6.4 `match`

`match (subject) { case pattern { body } ... }`.

Patterns:
- **Wildcard:** `_` — matches anything.
- **Sealed-variant destructure:** `Ok(value)` or `Err(_)` — matches the variant and binds positional fields. `_` in a binding position discards.
- **Equality literal:** `42`, `"hello"`, `MyEnum.Red` — matches when the subject equals.
- **Range** (over numeric subjects): `case 0..10` — matches values in the range.

**Exhaustivity.** When the subject's static type is a sealed class or enum, every variant must be covered, OR a wildcard arm must be present. Non-exhaustive match is a compile error:

```
error: match on Result is not exhaustive — missing case Err.
       add `case Err(_)` or `case _` to cover all variants.
```

Subjects whose static type is open (`String`, `int`, `any`, etc.) require a wildcard. Match arms execute top-to-bottom until one matches.

### 6.5 `break` / `continue`

Only valid inside a loop body (`for`, `while`, `parallel for`). Compile error otherwise.

### 6.6 All-paths-return

A function with a non-void return type must have all execution paths end in `return`. The check (in typecheck phase) walks:
- Block: last statement returns iff it returns.
- `if`: returns iff both branches return.
- `match`: returns iff every arm returns AND the match is exhaustive.
- Loops: don't count toward returning (the body might never run).

A void function (no declared return type) is exempt.

---

## 7. Concurrency

### 7.1 `spawn`

`spawn { body } [catch { handler }]` runs `body` in a new goroutine. Returns immediately. There is no thread handle; goroutines exit on `return` from the body or end-of-block.

```zinc
spawn {
    process(item)
} catch {
    log.error("spawn failed: ${err}")
}
```

The `catch { }` handler runs only if a panic propagates out of the body. Routine errors are handled inside the body via standard `catch { }` shape.

`spawn` is also valid as an expression (`spawn_expr`), but the current 1.0 use is statement-position.

### 7.2 `parallel for`

```zinc
parallel for item in items { body }                   // unbounded
parallel(max: 8) for item in items { body }           // bounded by N concurrent
```

Iterates `items`, running each `body` in a goroutine. Bounded form uses a semaphore for max concurrent execution. The whole construct returns when all goroutines finish.

`catch { }` handler at the end catches panics from any iteration. There is no per-iteration error aggregation by default; if you need that, use a channel.

### 7.3 `select`

Maps to Go's `select`. Each case is restricted to a channel send or receive (in method-call form):

```zinc
select {
    case x = ch1.recv():
        process(x)
    case ch2.send(value):
        log.info("sent")
    case _ = timer.recv():
        log.warn("timeout")
    default:
        // runs when no case is ready
}
```

The parser enforces that each case's expression is `<chan>.recv()` or `<chan>.send(arg)`. Default case is optional.

### 7.4 `with` / `using` / `lock`

All three lower to `WithStmt`. Semantics:

| Form | Lowering |
|---|---|
| `with (var f = openFile(p)) { body }` | `f := openFile(p); defer f.Close(); body` |
| `using (var f = openFile(p)) { body }` | same as `with` (single-resource sugar) |
| `lock (mu) { body }` | `mu.Lock(); defer mu.Unlock(); body` |

Multiple resources in `with`: deferred in reverse declaration order (Go's `defer` LIFO).

`lock` requires the resource to satisfy a `Lockable` interface (`Lock()`/`Unlock()`). Code paths that early-`return` or panic still trigger the deferred unlock.

### 7.5 `timeout`

```zinc
timeout(duration) { body } [catch { fallback }]
```

Runs `body` with a deadline. If the deadline elapses before `body` completes, the goroutine is signaled via the deadline context; `catch { }` runs with `err` bound to a deadline-exceeded error.

Implementation: lowers to a `context.WithTimeout` + goroutine + select. The `body` should respect cancellation (check ctx.Done()) for the timeout to be observed.

### 7.6 `defer`

`defer expr` registers a call to evaluate at the enclosing function's exit. Go semantics. LIFO order.

---

## 8. FFI semantics

Zinc compiles to Go and frequently calls into Go-imported packages. The FFI surface has its own rules, drawn from §4 of `00-lessons-learned.md`.

### 8.1 Two FFI seams

The bind phase tags every call expression with one of:

| Seam | Detection |
|---|---|
| **Package call** | Callee is `pkg.Func(args)` where `pkg` is a Go-import alias |
| **Method on Go-typed receiver** | Callee is `var.Method(args)` where `var` was bound to a value of a Go-imported type (typically returned from a previous Go FFI call) |
| **Zinc-internal call** | Callee is anything else (local fn, same-pkg fn, zinc subpkg fn, method on a Zinc class) |

### 8.2 Auto-`&` vs explicit `&`

For each FFI call's arguments:

| Go param shape | User writes | Codegen emits |
|---|---|---|
| Explicit `*T` | `f(x)` | `f(&x)` |
| `any` / `interface{}` | `f(&x)` | `f(&x)` |
| Concrete value type `T` | `f(x)` | `f(x)` |

The auto-`&` rule fires only at FFI seams. Zinc-internal calls never auto-`&`.

### 8.3 Multi-value Go returns

When `var a, b = pkg.Func(args)` (or `var a, b = recv.Method(args)`) is a multi-value-returning Go call, **each name binds to the corresponding return slot's Go-resolved type**. This per-slot type is consulted later when `a` or `b` participates in a method call (§8.1's "method on Go-typed receiver" detection) or in a tuple destructure.

```zinc
var dec, derr = hambaOcf.NewDecoder(rdr)
//   ^ *ocf.Decoder    ^ error
dec.Decode(&got) catch { ... }    // dec.Decode is "method on Go-typed receiver"; & permitted
```

### 8.4 The `&` validator

After parsing, the static check walks the AST and verifies every prefix-`&` expression is the **top-level argument** of a call into one of the two FFI seams. Anywhere else is a compile error:

```
error: '&' (address-of) is only allowed as an argument to a Go-library call;
       reject it elsewhere — assignments, returns, var inits, args of
       zinc-side calls, or nested sub-expressions.
```

Acceptable positions:
- `pkgFunc.Call(&x)` — top-level arg, package call. ✓
- `goVar.Method(&x)` — top-level arg, Go-typed receiver method. ✓

Unacceptable positions:
- `var p = &x` — assignment. ✗
- `return &x` — return. ✗
- `f(g(&x))` — nested sub-expression. ✗
- `myZincFn(&x)` — Zinc-internal call. ✗
- `pkgFunc.Call(g(&x))` — `&x` is argument to `g`, not the FFI call. ✗

### 8.5 Pointer-vs-value class semantics at FFI

Zinc class instances are pointers (`*T` in Go). Data class instances are values (`T` in Go). When passing to FFI:

- Class instance to a Go `*T` param: passes the pointer directly (no `&`).
- Class instance to a Go `any` param: explicit `&` required (the user is widening pointer-of-pointer).
- Data class instance to a Go `*T` param: auto-`&` (`&dataInstance`).
- Data class instance to a Go `any` param: explicit `&` required.

This is summarized: "the user opts in to `&` for `any` boundaries; the compiler injects `&` for explicit `*T` shapes."

---

## 9. Annotations

Closed set for 1.0:

| Annotation | Where | Meaning |
|---|---|---|
| `@Json("name")` | data class field | Override the field's JSON serialization name |
| `@Yaml("name")` | data class field | Override the field's YAML serialization name |
| `@Toml("name")` | data class field | Override the field's TOML serialization name |
| `@Avro("name")` | data class field | Map to hamba/avro-style Avro field name |
| `@Test` | function | Mark as a test (alternative to `*_test.zn` + `test "name"`) |

Codegen lowers `@Json/@Yaml/@Toml/@Avro` to Go struct tags on the corresponding field. `@Test` registers the function with the test harness.

Unknown `@Name` is a compile error at parse time. User-defined annotations are a 1.x feature, not 1.0.

---

## 10. Runtime model

Zinc's runtime is **Go's runtime**. The compiler emits Go source; `go build` produces a native binary; the binary uses Go's GC, scheduler, channels, panic/recover, etc.

What this means for users:

| Aspect | Behavior |
|---|---|
| Memory | Garbage collected (Go's concurrent mark-sweep) |
| Goroutines | Go's M:N scheduler; cheap, millions OK |
| Channels | Go's channel primitive |
| GC pauses | Submillisecond on modern Go (1.21+) |
| Class instances | Heap-allocated `*T` |
| Data class instances | Stack-allocated `T` (escape analysis may promote) |
| Panics | `panic(value)` in Go; Zinc users see only stdlib `panic`/`assert` triggering |
| `recover` | Available via stdlib; not directly exposed in Zinc (caught panics bubble up to a top-level wrapper) |

What's deliberately **not** Go-native:
- **`error` is value-typed, not interface-typed.** Sub-classes of `BaseError` are exposed via a Zinc class hierarchy; codegen emits Go interface satisfaction.
- **Method dispatch is virtual on classes** — Zinc classes always emit Go interfaces with method tables, not bare structs. Data classes are bare structs (no virtual dispatch).
- **Generics emit Go generics** for collections (`List<T>` → `[]T`); user-generic functions emit Go-generic functions.

---

## 11. Open questions for Phase 3

1. **Bind phase representation.** Side-map keyed by AST node identity, or rewrite AST into a `BoundProgram`? Side-map is less invasive; rewrite is cleaner long-term. Phase 3 decision.
2. **Typecheck phase representation.** Same question. Same answer expected (start with side-map, evaluate rewrite later).
3. **Module graph.** How are zinc-subpackage dependencies reflected in the bind phase? Currently `cmd/zinc/compiler.go` does ad-hoc multi-pass; phase 3 wants a proper module graph.
4. **Error types catalog.** Stdlib defines `BaseError` and subclasses. The catalog of error subclasses (`ParseError`, `ConfigError`, `IOError`, etc.) is currently scattered. 1.0 should curate the set.
5. **Runtime panic policy.** When does the runtime panic vs return an error? (Today: assertion failures, integer divide-by-zero, out-of-bounds index — all panic. Channel-on-closed-channel — panic. Map-key-not-present — returns zero value. These should be explicit in the spec.)
6. **`defer` interaction with `catch { }`.** A deferred call inside an `catch { }` handler — when does it run? Spec needs to nail down.
7. **Smart-cast preservation across `catch { }`.** If `x is T` narrows `x` in the then-branch, does the narrowing survive into a subsequent `catch { }`? Today: probably not; spec should be explicit.

These are all things to nail down during the Phase 3 rebuild, not before.
