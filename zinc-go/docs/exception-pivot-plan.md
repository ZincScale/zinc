# Zinc exception pivot — continuation plan

**Status:** Phases 1 + 2 shipped in commit `e85a084`. This doc captures the remaining work (Phases 3a–5) so it can be picked up cleanly.

## Context

zinc switched from errors-as-values (`T?` + `Error()` + `or {}`) to C#-style `try/catch/throw` for control-flow errors. Phase 1 added the syntax (lexer + AST + parser + a panic/recover codegen). Phase 2 unwound the "ctors always succeed" rule.

**Phase 1's codegen used panic/recover.** That needs to be reworked: the user's preferred model is **error returns** (`if err != nil { return err }`), not panic. The shape is identical to what Go programmers write by hand — Zinc's compiler synthesizes the boilerplate.

`ProcessorResult.Failure` stays as a data variant for FlowFile-level routing. Only control-flow errors (config, IO, programmer mistakes) become exceptions.

## Open design — locked in

1. **Unchecked exceptions** (no `throws` clause).
2. **`using (var r = init) { body }`** for resources (already shipped; reuses WithStmt).
3. **`finally`** kept but documented as discouraged (use `using` for cleanup).
4. **Constructors can throw** (Phase 2 done).
5. **Stdlib `Exception` base class** — every throwable extends it. Implements Go's `error` interface via `Error() string` method.
6. **No `??` null-coalescing** — Zinc doesn't have it; provider-fallback patterns become explicit `if (p == null) { ... }`.
7. **Typed catch dispatch via `errors.As`** — handles wrapped errors.
8. **Untyped Go errors auto-wrapped as Exception** when caught at `catch (Exception e)` boundary.

## Phase 3a — rewrite codegen for throw + try/catch (error-return model)

### `throw expr`

Inside any function or try-IIFE: emit `return zero, expr`.

`expr` must satisfy Go's `error` interface — i.e. either an instance of an Exception subclass (which has `Error() string`) or a raw Go error.

Touchpoint: `internal/codegen_go/codegen_stmts.go:emitThrowStmt`. Replace the existing `panic(expr)` emission.

### `try { body } catch (Type e) { handler } [catch ...] [finally { cleanup }]`

Wrap body in an IIFE returning `error`. Each statement in body that calls an error-returning function gets `if err != nil { return err }` woven in. After the IIFE, dispatch to catches.

Generated shape:

```go
_err := func() error {
    re, err := regexp.Compile(pattern)
    if err != nil { return err }
    matches := re.FindStringSubmatch(text)
    use(matches)
    return nil
}()
if _err != nil {
    if e, ok := errors.As[*ConfigException](_err); ok {
        // catch (ConfigException e) handler
    } else {
        // wrap raw error as Exception, run catch (Exception e) handler
        e := wrapAsException(_err)
        // ...
    }
}
```

For `finally`: defer the cleanup block before the IIFE result is checked (or run inline after the dispatch).

Touchpoint: `internal/codegen_go/codegen_stmts.go:emitTryStmt`. Currently uses panic/recover (~80 lines). Replace with the IIFE+error-check shape (~100 lines, similar size).

The "in try context" tracking goes on the Generator struct — when set, calls to error-returning functions inject the error check at the next statement boundary instead of propagating to the enclosing function.

### Implicit error return for thrower functions

Extend `canReturnError` (in `codegen.go:389`) to detect:
- `throw` statements (direct + nested in if/for/while/match)
- Calls to functions in the `errorFuncs` map (transitive — same as today)
- Try blocks whose catches don't cover all throwables (the uncaught propagate)

Functions matching get the implicit `error` return value. The current machinery for this already exists; extend the walker.

### Catch dispatch — `errors.As` for typed catches

For `catch (ConfigException e) { ... }`:

```go
var e *ConfigException
if errors.As(_err, &e) {
    // handler body, with `e` in scope
}
```

`errors.As` walks wrapped error chains, so `fmt.Errorf("ctx: %w", inner)` style wrapping continues to work.

For `catch (Exception e) { ... }` — the catch-all base type:

```go
var e *Exception
if errors.As(_err, &e) {
    // err was already an Exception or subclass
} else {
    // wrap raw Go error as Exception
    e = NewException(_err.Error())
}
// handler body
```

For untyped `catch (e) { ... }`:

```go
e := _err  // raw Go error interface
// handler body
```

For bare `catch { ... }` (no var):

```go
// handler body
```

If no catch matches, propagate the err: `return zero, _err` (or panic at top level if function doesn't declare error return — that's a compile error from the codegen).

### Function call sites

Inside a try block: call → check err → propagate to IIFE.

Outside a try block, in a thrower function: call → check err → return up.

Outside a try block, in a non-thrower function: call → check err → must propagate (forces caller function to become a thrower).

The codegen needs to track "in try context" via Generator state. When a call site emits, it asks the Generator: am I in a try? If yes, propagate via `return err` (returns from IIFE). If no, propagate via `return zero, err` (returns from outer function).

## Phase 3b — strip `or { }` from parser

Replace `v2ParseErrHandler` (parser_stmts.go:260) body with a parse error:

```go
func (p *Parser) v2ParseErrHandler() *OrHandler {
    if p.check(lexer.TOKEN_OR) {
        p.errorf("`or { }` is removed — wrap the call in `try { } catch { }` instead")
        // consume the block to avoid cascading errors
        p.advance()
        if p.check(lexer.TOKEN_LBRACE) {
            p.v2ParseBlock()
        }
    }
    return nil
}
```

The `OrHandler` struct field on VarStmt/AssignStmt/etc. stays for now (dead field) — removing it touches every codegen path that references it, which adds risk. Leave for cleanup pass.

## Phase 3c — strip `Error()` codegen special-cases

After 3b, `or {}` blocks no longer parse, so the `emitOrBlock` path is unreachable. The remaining `Error()` special-cases live in:

- `codegen.go:canReturnError` — detects `return Error(...)`. Remove (functions throw now, not return Error).
- `codegen_stmts.go:emitReturnStmt` — translates `return Error("msg")` → `return zero, fmt.Errorf("msg")`. Remove.

After this, `Error("msg")` becomes a regular call to a function named `Error` — would fail to resolve, which is the desired error. Users `throw NewConfigException("msg")` instead.

## Phase 3d — stdlib `Exception` base class

New file: `stdlib/src/exceptions/exceptions.zn`

```zinc
// Exception — base class for all Zinc exceptions. Satisfies Go's
// `error` interface via Error(). Custom exceptions extend Exception.

class Exception {
    pub String message
    pub String stackTrace = ""

    init(String message) {
        this.message = message
    }

    pub String Error() {
        return message
    }

    pub String toString() {
        return "Exception(${message})"
    }
}

// Common subclasses — convention, not exhaustive. Users add their own.

class IllegalArgumentException : Exception {
    init(String message) { super(message) }
}

class IllegalStateException : Exception {
    init(String message) { super(message) }
}

class IOException : Exception {
    init(String message) { super(message) }
}

class ConfigException : Exception {
    init(String message) { super(message) }
}
```

Update zinc-flow's import map to pull this in.

## Phase 4 — zinc-flow sweep (~40 + 5 sites)

Categories from the earlier audit:

| Category | Count | Translation pattern |
|---|---|---|
| **A. Provider lookup with fallback** | 3 | `if (p == null) { use_default } else { p as ContentProvider }` |
| **B. Go interop → Failure** | 7 | `try { var re = regexp.Compile(p) } catch (e) { return Failure("...", ff) }` |
| **C. Go interop → default** | 6 | `try { var data = ReadFile(path); return data } catch (e) { return byte[0] }` |
| **D. Go interop → log+ignore** | 4 | `try { f() } catch (e) { print(e.Error()) }` |
| **E. Misc complex** | 20 | Case-by-case |
| **F. `Error()` returns** | 5 | `throw ConfigException("msg")` |

Plus the same scan against `tests/` and `stdlib/src/` for completeness.

## Phase 5 — docs

- **`zinc-go/docs/error-handling.md`** — full rewrite. Was about errors-as-values. New shape: try/catch/throw, Exception base class, using for resources, finally discouraged.
- **`zinc-go/docs/classes.md`** — remove "Constructors always succeed" section. Add "constructors can throw" + example.
- **`zinc-go/docs/language-guide.md`** — add try/catch + using examples in the control-flow section.
- **`zinc-go/docs/concurrency.md`** — note that errors inside spawn { } are not catchable from outside (Go goroutine semantics).

## Re-entry checklist

When picking this up:

1. Read this doc.
2. Confirm `e85a084` is on origin/master (Phase 1+2 baseline).
3. Run e2e — should be 65/65 in zinc-go.
4. Run zinc-flow tests — should be 86/86.
5. Phase 3a is the heaviest single piece — start there. The IIFE+error-check codegen is the new pattern; everything else builds on it.
6. After Phase 3a smoke-tests cleanly with a hand-written example, do 3b (parser strip), 3c (Error strip), 3d (stdlib Exception).
7. Phase 4 sweep is mechanical once 3a-d are in place.
8. Phase 5 docs go last — easy once the language is final.

## Why the panic/recover model in Phase 1 is wrong

The Phase 1 implementation in `codegen_stmts.go:emitTryStmt` and `emitThrowStmt` uses `panic(expr)` and `defer func() { recover() }()`. This works but:

1. **Not idiomatic Go.** Go culture treats panic as "exceptional" — process-ending unless explicitly recovered. Using it as the regular control-flow mechanism is a misuse.
2. **No interop with Go's error interface.** Catching a thrown Zinc value via type assertion against the recovered `interface{}` doesn't compose with Go libraries that return `error`.
3. **Stack traces are wrong.** Go's panic prints a stack trace at panic time; recover suppresses it. For typed catches that re-raise, you lose the original panic site.

The error-return model fixes all three. It compiles to the same shape Go programmers write by hand: `if err != nil { return err }`. Catches dispatch via `errors.As`, which composes with the entire Go ecosystem.

The Phase 1 codegen IS still functional (try/catch/throw work end-to-end via panic/recover) — it just doesn't match Zinc's intended model. The rewrite swaps the codegen body without changing the surface syntax, so any user code written against Phase 1 keeps working.
