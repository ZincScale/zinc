# Pointer Inference for Go Type Construction — Design Document

**Status:** Implemented (2026-03-15) — Phase 1 (function args) + Phase 2 (nested struct fields)

## Overview

When Zinc developers write `tls.Config(Port: 8080)`, they think "create an object" — they don't think about Go's distinction between value types (`Config{}`) and pointer types (`&Config{}`). But many Go APIs expect pointer arguments (`*Config`, `*Options`, `*Settings`). Today, Go type construction always emits a value literal. This design adds context-aware pointer inference: construction emits `&` when the receiving context expects a pointer, and a value otherwise.

The user-facing model stays simple: `Type()` creates an object. The complexity is entirely in the transpiler.

## Design Principles

1. **Invisible to the user** — no new syntax, no `&` operator, no pointer annotations
2. **Context-driven** — the transpiler infers value vs pointer from how the result is used
3. **Safe default** — when context is ambiguous, emit a value (current behavior, no breakage)
4. **Leverage existing infrastructure** — `GoTypeResolver` already does `go/types` lookups

## The Problem

Go APIs pervasively use pointer-to-struct for configuration:

```go
// AWS SDK
svc := s3.New(session, &aws.Config{Region: aws.String("us-west-2")})

// gRPC
grpc.NewServer(grpc.Creds(&tls.Config{MinVersion: tls.VersionTLS12}))

// stdlib
http.ListenAndServeTLS(addr, cert, key, &http.Server{ReadTimeout: 5 * time.Second})
```

Today in Zinc, Go type construction always emits `TypeName{}` (value), which causes a compile error when the Go function expects a pointer. Users have no way to get `&TypeName{}`.

## Proposed Behavior

Go type construction emits `&TypeName{}` or `TypeName{}` depending on what the surrounding context expects:

| Context | Example (Zinc) | Inference | Emitted Go |
|---------|---------------|-----------|------------|
| Function argument | `grpc.Creds(tls.Config(...))` | Check param type via `go/types` | `&tls.Config{...}` if param is `*tls.Config` |
| `:=` with no context | `c := tls.Config(...)` | No type info — default | `tls.Config{}` (value) |
| Nested construction | `http.Server(TLSConfig: tls.Config(...))` | Check field type of `http.Server` | `&tls.Config{}` if field is `*tls.Config` |

### Zinc Syntax — No Change

```zinc
import "crypto/tls"

main() {
    // Argument context — go/types says grpc.Creds wants *tls.Config
    creds := grpc.Creds(tls.Config(MinVersion: tls.VersionTLS12))

    // No context — value (current behavior preserved)
    cfg := tls.Config(MinVersion: tls.VersionTLS12)
}
```

Users never write `&`, `*`, or any pointer syntax. The transpiler handles it entirely based on what the Go API expects.

## Inference Contexts (Detailed)

### Context 1: Function/Method Argument

The most common case. Go type construction appears as an argument to a Go function:

```zinc
grpc.Creds(tls.Config(MinVersion: tls.VersionTLS12))
```

**Resolution:**
1. Look up `grpc.Creds` via `GoTypeResolver` — get parameter types
2. Parameter 0 is `*tls.Config` — pointer
3. Emit `&tls.Config{MinVersion: tls.VersionTLS12}`

**New GoTypeResolver method needed:**
```go
// ParamType returns the type of the i-th parameter of a function.
// Returns (pkgPath, typeName, isPointer, ok).
func (r *GoTypeResolver) ParamType(pkgPath, funcName string, paramIndex int) (string, string, bool, bool)
```

### Context 2: Nested Construction (Struct Field)

```zinc
http.Server(TLSConfig: tls.Config(MinVersion: 3))
```

**Resolution:**
1. Outer construction is on `http.Server`
2. Look up `http.Server.TLSConfig` field type via `go/types` — it's `*tls.Config`
3. Inner construction emits `&tls.Config{MinVersion: 3}`

**New GoTypeResolver method needed:**
```go
// FieldType returns the type of a struct field.
// Returns (pkgPath, typeName, isPointer, ok).
func (r *GoTypeResolver) FieldType(pkgPath, typeName, fieldName string) (string, string, bool, bool)
```

### Context 3: No Context (`:=`)

```zinc
cfg := tls.Config(MinVersion: tls.VersionTLS12)
```

**Resolution:** No type context available. Emit value literal `tls.Config{...}` (current behavior). This is safe — if the user later passes `cfg` to a function that wants `*tls.Config`, Go's compiler will report the type mismatch, and the user can restructure (pass the construction inline as argument).

## Implementation Plan

### Phase 1: Function Argument Context (covers 90% of cases)

This is the highest-value context and can be implemented without any syntax changes.

**Step 1: Add `ParamType()` to GoTypeResolver**

```go
func (r *GoTypeResolver) ParamType(pkgPath, funcName string, paramIndex int) (string, string, bool, bool) {
    pkg := r.loadPackage(pkgPath)
    if pkg == nil { return "", "", false, false }

    obj := pkg.Scope().Lookup(funcName)
    if obj == nil { return "", "", false, false }

    fn, ok := obj.(*types.Func)
    if !ok { return "", "", false, false }

    sig := fn.Type().(*types.Signature)
    params := sig.Params()
    if paramIndex >= params.Len() { return "", "", false, false }

    return extractTypeInfo(params.At(paramIndex).Type())
}
```

Also add `MethodParamType()` for method calls:

```go
func (r *GoTypeResolver) MethodParamType(pkgPath, typeName, methodName string, pointer bool, paramIndex int) (string, string, bool, bool)
```

**Step 2: Add `FieldType()` to GoTypeResolver**

For nested construction resolution (struct fields):

```go
func (r *GoTypeResolver) FieldType(pkgPath, typeName, fieldName string) (string, string, bool, bool) {
    pkg := r.loadPackage(pkgPath)
    if pkg == nil { return "", "", false, false }

    obj := pkg.Scope().Lookup(typeName)
    if obj == nil { return "", "", false, false }

    structType, ok := obj.Type().Underlying().(*types.Struct)
    if !ok { return "", "", false, false }

    for i := 0; i < structType.NumFields(); i++ {
        f := structType.Field(i)
        if f.Name() == fieldName {
            return extractTypeInfo(f.Type())
        }
    }
    return "", "", false, false
}
```

**Step 3: Thread context through `emitCallExpr`**

The core change. When `emitCallExpr` encounters a Go type construction inside a function call, it checks the expected parameter type:

```go
func (g *Generator) emitCallExpr(call *parser.CallExpr) string {
    // ... existing code ...

    // When emitting args for a function call, check param types
    for i, arg := range call.Args {
        if innerCall, ok := arg.(*parser.CallExpr); ok {
            if isGoTypeConstruction(innerCall) {
                // Check if param i expects a pointer
                if needsPointer(outerFuncPkg, outerFuncName, i) {
                    // Emit &Type{} instead of Type{}
                }
            }
        }
    }
}
```

**Implementation approach:** Rather than modifying `emitCallExpr`'s signature, use a context field on the Generator:

```go
type Generator struct {
    // ... existing fields ...
    expectPointer bool  // set by parent context before emitting Go type construction
}
```

Before emitting each argument of a Go function call, check if that parameter expects a pointer. If yes, set `expectPointer = true`, emit the arg (which will call `emitGoTypeNew`), then reset it.

**Step 4: Modify `emitGoTypeNew` to respect context**

```go
func (g *Generator) emitGoTypeNew(typeName string, call *parser.CallExpr) string {
    prefix := ""
    if g.expectPointer {
        prefix = "&"
    }

    if len(call.Args) == 0 && len(call.NamedArgs) == 0 {
        return prefix + typeName + "{}"
    }
    // ... existing field emission ...
    return fmt.Sprintf("%s%s{%s}", prefix, typeName, strings.Join(fields, ", "))
}
```

### Phase 2: Nested Construction (Struct Field Context)

After Phase 1 lands, extend to detect pointer fields within struct literals:

```zinc
http.Server(TLSConfig: tls.Config(...))
```

When emitting named args of a Go type construction, look up each field's type via `FieldType()`. If the field is a pointer type and the value is another construction, set `expectPointer = true`.

Phase 1 + Phase 2 cover the vast majority of real-world use cases. No pointer syntax is ever exposed to users.

## Edge Cases

### Zinc Class Constructors — No Change

Zinc class constructors already return `*Impl` (pointer). This feature only affects Go type construction:

```zinc
Dog("Rex")            // Already emits NewDog("Rex") which returns *DogImpl
sync.Mutex()          // This is what we're changing — value vs &
```

### Variadic Parameters

```zinc
foo(configs...)  // spread — not a construction, no inference needed
foo(tls.Config(), tls.Config())  // multiple constructions — check each param index
```

For variadic params, the element type determines pointer-ness. `ParamType` should handle `...T` by returning the element type.

### Interface Parameters

```zinc
foo(sync.Mutex())  // foo expects sync.Locker (interface)
```

If the param type is an interface, construction should emit value (or pointer, depending on which satisfies the interface). `go/types` can check method sets. In practice, most Go interfaces are satisfied by pointer receivers, so `&` is usually correct — but we should verify via `types.Implements()`.

### `nil`-able Parameters

Some APIs accept `*Config` where `nil` means "use defaults":

```zinc
http.ListenAndServe(":8080", null)  // handler is http.Handler (interface), nil OK
```

This doesn't involve construction — `null` → `nil` is a separate concern.

## Testing Strategy

### Unit Tests (codegen_test.go)

```go
// Phase 1: function argument context
TestGoTypePointerParam()
// http.ListenAndServe expects *http.Server → &http.Server{}
// input:  http.ListenAndServe(":8080", http.Server())
// expect: http.ListenAndServe(":8080", &http.Server{})

TestGoTypeValueParam()
// Function expects value type → no &
// input:  time.NewTimer(time.Duration())
// expect: stays as value

TestGoTypePointerWithNamedArgs()
// input:  grpc.Creds(tls.Config(MinVersion: 3))
// expect: &tls.Config{MinVersion: 3}

TestGoTypeNoContext()
// := assignment — no change
// input:  cfg := tls.Config()
// expect: tls.Config{} (value, as before)
```

### E2E Tests (e2e_test.go)

```go
TestE2EGoTypePointerInference()
// Real Go API call with pointer parameter
// Transpile → compile → run → verify no compile errors
```

### Backward Compatibility

All existing tests must continue to pass unchanged. The `:=` default (value) preserves current behavior.

## What This Does NOT Cover

- **No pointer syntax in Zinc** — no `*Type`, no `&`, no pointer annotations, ever
- **Pointer arithmetic** — not applicable (Go doesn't have it either)
- **Dereferencing** — not needed; Go handles this transparently for method calls

Pointers are a Go implementation detail. Zinc developers work with objects — the transpiler decides whether the Go output needs `&` or not. The goal is strictly: make Go type construction work correctly when passed to APIs expecting pointers, without the user knowing about pointers at all.

## Alternatives Considered

| Alternative | Decision | Reason |
|------------|----------|--------|
| Always emit `&` for construction | Rejected | Breaks APIs expecting value types; some Go APIs return value types intentionally (e.g., `time.Time`) |
| New syntax like `.ref()` or `.ptr()` | Rejected | Exposes pointer semantics — violates the OO mental model |
| Let users write `&Config{}` directly | Rejected | Go-ism, not OO. Zinc should abstract this away |
| Only infer for known stdlib packages | Rejected | Would miss third-party APIs (AWS, gRPC, etc.) which are the primary use case |

## Summary

- **Phase 1** (function argument context) covers 90%+ of real-world pointer inference needs
- **Phase 2** (nested struct fields) covers the remaining common case
- No syntax changes, no breaking changes, no pointer concepts exposed to users
- Leverages existing `GoTypeResolver` infrastructure
- Safe default: emit value when context is unknown
