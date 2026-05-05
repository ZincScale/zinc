# Codegen State Audit

Input to the C-track refactor — making the typechecker the single source of truth for type info, then stripping codegen-side maps that duplicate it.

This doc enumerates two things:

1. **Codegen maps** the `Generator` carries today (what populates them, what reads them, what they're for, where they overlap).
2. **Typechecker `BoundProgram` surface** today — what it actually exposes vs. what `CheckV2` knows internally but doesn't surface.

The intersection — codegen maps whose data the typechecker already computes but doesn't expose — is the work plan for C.

---

## Executive summary

The Generator carries **~50 fields** total, of which roughly 25 are tracking maps for type/symbol info. Most overlap with internal state the typechecker already has but **doesn't expose through `BoundProgram`**.

Key finding: `BoundProgram` exposes only `Bindings` (Ident → Symbol) and `NodeTypes` (Expr → V2Type). The typechecker's `CollectedSigs` (function/method signatures, class fields, parent types, class names) — produced during `CheckV2` — is **not** part of `BoundProgram`. Codegen can't read it, so it duplicates the work.

C-track move: extend `BoundProgram` to expose the existing internal tables, fix the few cases where the typechecker doesn't fully resolve (cross-pkg method-call return types, lambda inference targets, FFI pointer detection), then strip codegen's parallel tracking.

Per-feature trackers that survive C: ones that are genuinely codegen-side concerns (Go-builtin shadow renames, _zincPtr emission flag, indent buffer, error-var counters) — small, local, no overlap with type system.

---

## Part 1 — Codegen map inventory

Grouped by purpose. For each: **populator → reader → invariant → overlap**.

### 1.1 Class taxonomy (current package)

| Field | Populator | Reader | Invariant | Overlap |
|---|---|---|---|---|
| `structs[name] *parser.ClassDecl` | `collectDecls` (full decls); `SetSiblingExports` (placeholder w/ Name only) | `formatType`, `isClassType`, `valueIsAlreadyPointer`, `targetIsPointerOptional`, `currentMethods` setup | `name` is a class iff present and `cls != nil && !cls.IsSealed` | typechecker has `ClassNames` + `ClassFields` + `MethodSigs`. Sibling-class placeholders here are the gap fix from this session — should come from typechecker instead. |
| `interfaces[name] bool` | `collectDecls` for `IsSealed` and InterfaceDecl; `SetSiblingExports` kind=="interface" | `formatType`, `useExplicitType` decision in var-decl | Set iff name is an interface or sealed parent | typechecker `Symbol.Kind == SymInterface` covers this. |
| `dataClasses[name] bool` | `collectDecls` (DataClassDecl, sealed variants); `SetSiblingExports` kind=="data" | `formatType` (skip pointer wrap), `isClassType` | Set iff name is a data class | typechecker `SymDataClass` covers this. |
| `dataClassDecls[name] *parser.DataClassDecl` | `collectDecls` | `formatLambdaExpr`, implicit-self ctor lookup | Maps name → full decl for fields | typechecker `ClassFields[name]` covers fields; the AST node is needed only for codegen-specific rendering. |

**Overlap pattern:** four maps form one logical taxonomy ("what kind is this name?"). Could be a single `kindOf(name) → SymbolKind` query. Today's typechecker already has this via `Bindings` for Idents — but bare-name lookups (during template emission, before AST traversal) need direct access.

### 1.2 Symbol kinds (current package)

| Field | Populator | Reader | Invariant | Overlap |
|---|---|---|---|---|
| `pubNames[name] bool` | `collectDecls` | `exportName` decisions, cross-pkg call resolution | Set iff `pub` modifier present | typechecker `Symbol.IsPub` could carry this; not currently exposed. |
| `funcSigs[name] []*parser.ParamDecl` | `collectDecls` (FnDecl, ctor) | Default-arg fill, lambda type inference | Maps fn name → param list | typechecker `FnSigs[name]` is `V2FnSig` with full param info. Not exposed via BoundProgram. |
| `errorFuncs[name] bool` / `errorFuncs[class.method]` | `collectDecls` via `returnTypeDeclaresError` | `callReturnsError`, `callIsVoidThrower` | Set iff signature has trailing `error` | Derivable from typechecker `V2FnSig.ReturnType`. |
| `typeAliases[name] parser.TypeExpr` | `collectDecls` (TypeAliasDecl) | `formatType` (peel alias) | Maps alias → underlying TypeExpr | typechecker has aliases via `SymTypeAlias` symbol; the underlying TypeExpr isn't exposed cleanly. |
| `currentMethods[name] bool` | Per-class emission setup | Implicit-self method-call detection in lambdas | Set iff name is a method of currentClass | Derivable from typechecker `MethodSigs[currentClass]`. |
| `currentFields[name] bool` / `currentFieldGoName[name] string` | Per-class emission setup | Implicit-self field access | Set iff name is a field of currentClass | typechecker `ClassFields[currentClass]` has fields; Go name mapping is codegen concern. |
| `currentParams[name] bool` / `currentLocals[name] bool` | Per-function emission setup | Shadow detection (param/local hides field) | Tracks scope at the active emit site | typechecker scopes resolve this during `Bind`; codegen uses bare-name shadow checks. |

**Overlap pattern:** classic compile-time symbol table. Typechecker `Symbol` covers all of these but isn't queryable by bare name from codegen.

### 1.3 Type tracking (current package)

| Field | Populator | Reader | Invariant | Overlap |
|---|---|---|---|---|
| `varTypes[name] string` | `inferExprType` results in var-decls; ad-hoc set from list-literal element inference | `valueIsAlreadyPointer` Ident fallback (this session), `inferExprType` recursive lookups | Maps local name → Go type string when known | `bound.NodeTypes[expr]` is more authoritative; codegen falls back when bound info missing. |
| `ptrVars[name] bool` | Var-decl branches (no-init nullable; with-init from func return; or-handler shape — three sites added incrementally) | `valueIsAlreadyPointer`, `exprIsPointerOptional`, auto-deref decisions | Set iff name is a `*T` local that may be nil | Three populator sites is the smell. Typechecker `V2Type.Nullable + Symbol.Kind` covers this. |
| `funcReturnsOptional[name] bool` | `collectDecls` for FnDecl + ClassDecl methods | `valueIsAlreadyPointer`, var-decl ptrVar registration, `exprIsPointerOptional` | Set iff fn/method declared return is `T?` | Derivable from typechecker `V2FnSig.ReturnType.Nullable`. |
| `funcReturnTypes[name] string` | `collectDecls` for FnDecl (full type); methods (this session, with prefix-* + tuple-first-element extraction) | `valueIsAlreadyPointer`, `inferExprType` for CallExpr, `callIsVoidThrower` | Maps fn/method name → Go return type string | Derivable from typechecker `V2FnSig.ReturnType` formatted by codegen. |
| `renamedVars[name] string` | Var-decl when name collides with Go builtin | Reference rewrite at use sites | Codegen-only concern (Go syntax) | No overlap. **Survives C.** |
| `activeTypeParams[name] bool` | Generic-fn / generic-method emission entry | `formatType` (keep T as-is) | Set iff name is an in-scope generic type parameter | typechecker tracks this via scope stacks; codegen uses bare-name fallback. |

**Overlap pattern:** four of the six maps are derivable from typechecker `V2FnSig` / `V2Type` data. Not exposed today.

### 1.4 Cross-package wiring

| Field | Populator | Reader | Invariant | Overlap |
|---|---|---|---|---|
| `subpkgExports[pkg][name] string` | `SetSubpackageExports` (compiler driver) | Many `formatType` paths for qualified names; `unqualifiedNames` builder | Maps pkg alias → name → kind | typechecker has per-package `CollectedSigs` already; needs aggregation across packages. |
| `subpkgStructs[pkg][name] *parser.ClassDecl` | `SetSubpackageStructs` (compiler driver) | Cross-pkg method/field lookups, `isClassType` qualified path | Full class decls from each imported subpackage | Same shape as `structs[]` but per-package. The typechecker has class info per program but not aggregated. |
| `subpkgDataFields[pkg][name] []*parser.FieldDecl` | `SetSubpackageDataFields` | Data-class field access on cross-pkg references | Data class field params per pkg | Subset of subpkgStructs but for data classes. |
| `subpkgTypeAliases[pkg][name] parser.TypeExpr` | `SetSubpackageTypeAliases` | Cross-pkg `Fn<...>` alias resolution | Type aliases per pkg | Same as typeAliases but per-pkg. |
| `localDataFields[name] []*parser.FieldDecl` | `collectDecls` (DataClassDecl + sealed variants) | Implicit-self in data-class methods, ctor expansions | Current-pkg data class fields | Same shape as subpkgDataFields["this"]. |
| `unqualifiedNames[name] unqualifiedEntry` | Built post-hoc from subpkgExports + import processing | Bare-name resolution to qualified types | Maps short name → (pkg, kind) | Derivable from subpkgExports + import map; today it's a separate denormalized table. |
| `unqualifiedCollisions[name] []string` | Same builder | Collision error reporting | Tracks ambiguity | Pure error-reporting state. |
| `importMap[alias] string` | Import-statement processing | `formatType` for qualified types, `needImport` calls | Maps zinc import alias → Go package path | Codegen-side, no typechecker equivalent — survives C. |
| `typeImports[name] string` | Built during import processing for unqualified type aliases | `formatType` SimpleType branch | Maps short type name → qualified Go form | Subset of importMap behavior. |
| `importAliases[alias] string` | `SetImportAliases` from zinc.toml `[deps]` | Cross-pkg call resolution | External Go module aliases | Codegen/build-side concern — survives C. |
| `importGoAliases[goPath] string` | Auto-aliased via shadow pre-scan | Go-side import alias rewrite | When user-scope name shadows an import package | Codegen-only — survives C. |
| `zincSubpackages[name] bool` | `SetZincSubpackages` | `isZincSubpackage` checks | Set of known zinc subpackage names | Driver-side; could move to typechecker context. |

**Overlap pattern:** seven maps describe "what's in the other packages." The typechecker has per-program info but no aggregator. Adding a `BoundProject` (multi-program aggregate) would unify all of these.

### 1.5 Per-function emission state (transient, function-local)

| Field | Populator | Reader | Survives C? |
|---|---|---|---|
| `currentReturnType` | Function emit entry | Zero-value rendering in error returns | Yes — codegen-local, derivable from declared return |
| `currentOuterReturnType` | Function emit entry | try-IIFE tuple shape | Yes — codegen-local |
| `currentReturnOptional` | Function emit entry | Return-statement codegen | Could be replaced by querying `V2FnSig.ReturnType.Nullable` |
| `currentFuncParams` | Function emit entry | Lambda type inference | Could be `bound.LookupSymbol(currentFnName).Sig.Params` once exposed |
| `currentMethodRetType` | Method emit entry | Channel recv type assertions | Codegen-local |
| `currentReturnIsTuple` | Function emit entry | Multi-value return codegen | Derivable from `ReturnType is TupleType` |
| `currentThrowerValueGoTypes` | Function emit entry (tuple peel) | Per-slot zero rendering | Codegen-local |
| `currentReturnIsDeclaredThrower` | Function emit entry | Return-statement zero-fill path | Derivable from declared return |
| `currentFuncIsThrower` | Function emit entry | `emitErrReturn` policy | Derivable from declared return |
| `currentErrVar` / `errVarCount` | OrHandler / try emission | Error-var name generation | Codegen-only |
| `pendingLambdaTarget` | Var-decl LHS Fn type | `formatLambdaExpr` return type hint | Codegen-only |
| `currentClass` | Per-class emission | Pub-member lookups, implicit-self | Codegen-local |

**Overlap pattern:** these are emit-state, not type-tracking. Most stay; a few could be derived rather than tracked. Not the heart of the refactor.

### 1.6 Codegen knobs (no overlap)

`buf`, `indent`, `chainCounter`, `addrOfAllowed`, `addrOfReported`, `needsPtrHelper`, `compileErrors`, `compileWarnings`, `collisionsReported`, `goResolver`, `inferredChanElem`, `imports`, `sourceFile`, `className`, `packageName`, `moduleName`.

These survive C unchanged.

---

## Part 2 — Typechecker `BoundProgram` surface

What's exposed today via `internal/typechecker.BoundProgram`:

```go
type BoundProgram struct {
    Prog      *parser.Program
    Bindings  map[*parser.Ident]Symbol  // Ident → Symbol (Kind, Name, Pkg, Owner, DeclType)
    NodeTypes map[parser.Expr]V2Type    // Expr → V2Type (Name, Args, Nullable, GoType)
}
```

What `CheckV2` produces internally but **doesn't** expose:

```go
type CollectedSigs struct {
    FnSigs      map[string]V2FnSig                  // free function signatures
    MethodSigs  map[string]map[string]V2FnSig       // class → method → sig
    ParentTypes map[string][]string                 // class → parent class names
    ClassNames  map[string]bool                     // every named type
    ClassFields map[string]map[string]V2Type        // class → field → type
}
```

Plus per-checker scope chain (param/local resolution) that resolves at bind-time but doesn't outlive the function.

### Gaps the typechecker doesn't fully resolve today

Items the codegen had to fill in because the typechecker leaves them ambiguous:

1. **Cross-package method-call return types.** `obj.method()` where method lives in another package — typechecker resolves the receiver but not the method-return-type. Codegen has `subpkgStructs` to look it up post-hoc.
2. **Same-package cross-file method returns.** Typechecker has per-program `MethodSigs`; cross-file aggregation isn't exposed. Codegen `RegisterSiblingMethods` (added this session) fills the gap.
3. **Lambda inference targets.** A lambda passed to a function expecting `Fn<(int), int>` — typechecker doesn't always propagate the expected type into the lambda body. Codegen carries `pendingLambdaTarget` to bridge.
4. **FFI pointer-type detection.** Whether `time.Time` is a value type or `bytes.Buffer` is a pointer type — currently resolved by codegen's `goResolver` walking Go's `go/packages`. Not in the typechecker's domain at all today.
5. **Implicit-self method calls.** Inside method `A.foo()`, calling `bar()` where `bar` is also a method of A. Bare-Ident at the call site; typechecker may or may not resolve it depending on bind context completeness.
6. **Generic instantiation in cross-package `as` casts.** Recently fixed in zinc-go (commit `4b2cd2a`); the bound info in `as` RHS is type-expression-grammar parsing, not name lookup.
7. **Optional-unwrap result types.** `entry as VersionEntry` where entry is `VersionEntry?` — codegen knows the result is non-nullable VersionEntry but the typechecker doesn't always propagate that into the bound NodeType for downstream `(... as T).field` expressions.

---

## Part 3 — Overlap matrix (codegen → typechecker)

| Codegen map | Typechecker equivalent | Status today |
|---|---|---|
| `structs[name]` (current pkg, class taxonomy) | `Symbol.Kind == SymClass` | typechecker has it, BoundProgram doesn't expose bare-name lookup |
| `interfaces[name]` | `Symbol.Kind == SymInterface` | same |
| `dataClasses[name]` | `Symbol.Kind == SymDataClass` | same |
| `funcSigs[name]` (current pkg) | `CollectedSigs.FnSigs[name]` | typechecker has it, not in BoundProgram |
| `errorFuncs[name]` | derivable from `V2FnSig.ReturnType` (trailing error) | derivable, computed inline today |
| `funcReturnsOptional[name]` | `V2FnSig.ReturnType.Nullable` | derivable, computed inline today |
| `funcReturnTypes[name]` | `V2FnSig.ReturnType` formatted | derivable, formatting is codegen concern |
| `currentMethods` / `currentFields` | `CollectedSigs.MethodSigs[class]` / `ClassFields[class]` | typechecker has it, not exposed |
| `varTypes[name]` (locals) | `bound.NodeTypes[v.Value]` for var-decl init | partially redundant; bound is more authoritative when available |
| `ptrVars[name]` | `Symbol.DeclType` (OptionalType check) | derivable when Symbol available |
| `subpkgStructs[pkg][name]` (cross-pkg classes) | per-package `CollectedSigs.ClassNames` | typechecker has it per-pkg, not aggregated |
| `subpkgDataFields[pkg][name]` | per-pkg `CollectedSigs.ClassFields` | same |
| `localDataFields[name]` | current-pkg `CollectedSigs.ClassFields` | same |
| `typeAliases[name]` / `subpkgTypeAliases` | `Symbol.Kind == SymTypeAlias` + a yet-to-add aliases table | typechecker partially tracks (kind only, not target) |
| `unqualifiedNames` / `unqualifiedCollisions` | `Bindings[ident]` resolves the same lookup | resolved at bind time per-Ident; bare-name table isn't exposed |
| `pubNames[name]` | `Symbol.IsPub` | not currently in Symbol; trivial to add |
| `activeTypeParams[name]` | typechecker scope stack | not exposed; codegen rebuilds locally |

**Survives C unchanged:** `renamedVars`, `importMap`, `typeImports`, `importAliases`, `importGoAliases`, `inferredChanElem`, all per-function emission state, all codegen knobs.

---

## Part 4 — Path to C

Concrete items, ordered roughly by dependency. Each is a separate commit / PR-sized chunk; they're mostly independent so they can interleave.

### Phase 1 — extend BoundProgram

P1.1. **Aggregate `CollectedSigs` into a `BoundProject` type.** Today each program has its own `CollectedSigs`. The driver (`compileMultiFile`, `compileDirWithSubpackagesAndExtras`) merges per-pkg info into `allClassDecls`, `allExports` etc. Move that aggregation into the typechecker's `Bind` / `CheckV2` driver and produce a single `BoundProject` covering all packages.

P1.2. **Add `IsPub` to `Symbol`.** Trivial — `Bind` already sees the modifier.

P1.3. **Add `TypeAliases` to `BoundProgram`.** Map from alias name to underlying TypeExpr. `Bind` already records SymTypeAlias kind; surface the target.

P1.4. **Surface `FnSigs`, `MethodSigs`, `ClassFields`, `ParentTypes` via `BoundProject`.** Already computed by `CollectSignatures`; just expose.

### Phase 2 — fill typechecker resolution gaps

P2.1. **Resolve cross-pkg method-call return types in CheckV2.** When checking `obj.method()`, if obj's type is in another package, look up the method via the now-aggregated `BoundProject.MethodSigs`. Annotate the call's NodeType with the resolved return.

P2.2. **Resolve same-package cross-file method calls.** Run `CollectSignatures` per package (already happens) then make the result available to every per-program checker. Drops `RegisterSiblingMethods` from codegen.

P2.3. **Propagate lambda inference targets.** When checking a CallExpr where the parameter type is `Fn<...>`, bind the lambda body's expected return into its NodeType. Drops `pendingLambdaTarget`.

P2.4. **Hand FFI pointer detection to the typechecker.** Move `goResolver` ownership from codegen to the typechecker (or add a typechecker-side adapter). The typechecker decides "this Go-pkg type is `*T` or `T`"; codegen reads the result.

P2.5. **Propagate optional-unwrap result types.** `(x as T)` where x is `T?` should give NodeType{Name:T, Nullable:false} so downstream selectors work.

### Phase 3 — strip codegen-side trackers

Each item below is "codegen reads `bound.X` instead of its own map; delete the map."

P3.1. **`structs` / `interfaces` / `dataClasses` / `dataClassDecls`** → consolidated `bound.LookupSymbol(name)` returning kind + decl.

P3.2. **`funcSigs` / `errorFuncs` / `funcReturnsOptional` / `funcReturnTypes`** → `bound.FnSigs[name]` + helpers (`IsThrower`, `ReturnsOptional`, `GoReturnType`).

P3.3. **`currentMethods` / `currentFields` / `currentFieldGoName` / `currentParams` / `currentLocals`** → `bound.LookupInScope(currentFn, name)` returning Symbol.

P3.4. **`subpkgExports` / `subpkgStructs` / `subpkgDataFields` / `subpkgTypeAliases` / `localDataFields`** → `bound.LookupAcrossPkg(pkg, name)` returning Symbol.

P3.5. **`varTypes` / `ptrVars`** → `bound.NodeTypes` + `Symbol.DeclType`.

P3.6. **`unqualifiedNames` / `unqualifiedCollisions`** → folded into `bound.LookupSymbol`.

P3.7. **`typeAliases`** → `bound.TypeAliases`.

P3.8. **`pubNames`** → `bound.LookupSymbol(name).IsPub`.

P3.9. **`activeTypeParams`** → `bound.InScope(name, SymType)`.

### Phase 4 — verify

P4.1. **Regression**: 111 e2e + zinc-flow-go + every dependent project's suite green.
P4.2. **Add regression probes** for the seam-bugs caught this session (the three nullable-codegen fixes) so future codegen changes can't silently re-break the same shapes. Probes already exist as `/tmp/zinc-bug-repro/{bug1.zn, bug2_xpkg/}` — promote to `examples/` or `examples-test/`.
P4.3. **Track Generator field count.** Current: ~50 fields, ~25 type-related maps. Target: ~30 fields, type-related maps replaced by `bound *BoundProject` and a small handful of codegen-only state.

---

## Estimates revisited

Original C estimate: **2-4 weeks**.

After audit, the work decomposes:

- Phase 1 (extend BoundProgram): **~2-3 days.** Mostly mechanical surfacing of already-computed tables.
- Phase 2 (fill resolution gaps): **~1 week.** Each gap is small; aggregate is real work, especially FFI pointer detection moving across boundaries.
- Phase 3 (strip codegen maps): **~1 week.** Mechanical but spread across many files (10K LoC of codegen). Each P3 item is a commit; net deletion should be larger than additions.
- Phase 4 (verify + regression probes): **~1-2 days.**

**Total: ~3 weeks.** Slightly higher than the original 2-week guess, because Phase 1 + 4 weren't in the prior estimate.

This can run on a long-lived branch with periodic rebases against parity work landing on master. Each phase ends with a green suite, so the branch stays mergeable throughout.
