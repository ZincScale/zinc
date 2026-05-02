# Zinc — rebuild plan (Phase 3 sequencing)

**Status:** Phase 3 deliverable. Sequenced commit-by-commit roadmap for re-architecting the compiler against the spec (`01-grammar.md` + `02-semantics.md` + `03-type-system.md`). No code edits land until this sequencing has user sign-off.

**Pacing rule.** Every numbered step below is a single coherent commit. Each commit:
- Has a clear pass/fail criterion (usually: e2e green + unit tests green).
- Is independently revertable.
- Pauses for review before the next commit lands when the change is non-trivial.

The plan is grouped into seven sub-phases. Sub-phase 3.1 is short cleanups; 3.2-3.5 build the new pipeline incrementally; 3.6 adds the spec's new language features on top; 3.7 deletes the dead code.

**Risk register at the bottom.**

---

## Phase 3.1 — spec-cleanup commits (small, low-risk, foundational)

These bring the parser/lexer/AST in line with the 22 decisions before any pipeline rewrite begins. Each is a small commit; collectively they take the project from "current" to "spec-compliant from a syntactic perspective."

### 3.1.1 — Drop `fn` keyword
- Remove `TOKEN_FN` from `internal/lexer/token.go` enum, `tokenNames` map, `keywords` map.
- Confirm parser does not consume `fn` anywhere (grep for `lexer.TOKEN_FN`).
- Update `examples-fail/fn_keyword_removed.zn` to confirm it errors with the new message format.
- **Verify:** `go test ./...` + `bash run_e2e.sh` green.

### 3.1.2 — Drop `===` / `!==`
- Remove `TOKEN_REF_EQ`, `TOKEN_REF_NEQ` from token enum/maps.
- Remove `===` / `!==` from `internal/lexer/lexer.go` punctuation handler.
- Remove the two token references from `v2ParseComparison` in `parser_exprs.go`.
- **Verify:** e2e green; nothing in `examples/` uses these tokens.

### 3.1.3 — Drop `??` (null-coalesce)
- Remove `TOKEN_QUESTION_QUESTION` from token enum/maps and the `?.peek() == '?'` lex branch.
- Confirm no parser path consumes it.
- **Verify:** e2e green.

### 3.1.4 — Drop `<-` (channel arrow)
- Remove `TOKEN_CHAN_ARROW` from token enum/maps and the `<.peek() == '-'` lex branch.
- Audit `SendExpr`/`ReceiveExpr` in `parser/ast.go` — already commented as not parser-created; confirm and delete the dead types.
- **Verify:** e2e green; channel ops in examples use `.send()` / `.recv()`.

### 3.1.5 — Drop `->` and `:=`
- Remove `TOKEN_ARROW`, `TOKEN_COLONASSIGN` from token enum/maps and the lex branches.
- Confirm no parser path consumes them.
- **Verify:** e2e green.

### 3.1.6 — `;` retained as optional statement separator (decision reversed 2026-05-01)

The original §8.13 decision (newline-only) was reversed: `;` stays as an optional separator so users can write multiple short statements on a single line. Matches Kotlin/Java tolerance. No code change required — existing parser behavior already supports this. Updated:
- `00-lessons-learned.md` §8.13 — decision flipped
- `01-grammar.md` §5 — grammar describes `;` as optional separator

### 3.1.7 — Drop `OrHandler.MatchCases`
- Remove `MatchCases []*OrMatchCase` and `MatchVar string` fields from `OrHandler` in `parser/ast.go`.
- Remove `OrMatchCase` type entirely.
- Update parser (`v2ParseErrHandler` etc.) to reject `or match err { ... }` form with: `"'or match' is not supported; use 'or { match (err) { ... } }' instead"`.
- Update codegen (`emitOrMatch`) — delete the function.
- **Verify:** e2e green; new fail test `examples-fail/or_match_form.zn` errors correctly.

### 3.1.8 — Codegen non-`pub` consistency
- Audit `formatCallExpr` and friends for places that capitalize a non-pub name at a call site.
- Make non-`pub` Zinc names lowercase at *both* def site and call site, regardless of cross-file/cross-package detection.
- **Verify:** zinc-flow-go's previous `coerceForSchema → CoerceForSchema` mismatch stops happening; can revert the `pub` modifier I added there today as a workaround. e2e green.

### 3.1.9 — FQDN-on-collision Zinc-level error
- In `buildUnqualifiedNames` (`codegen_resolve.go`): when a collision is detected and the user uses the bare name, emit a `g.compileError` with the prescribed message: `"ambiguous bare name X — exported by both A and B. Use A.X or B.X to disambiguate."`
- Currently the resolver returns the bare name as-is and lets Go's compiler surface `undefined: X` — replace with the Zinc-level error including source position.
- **Verify:** new fail test `examples-fail/ambiguous_bare_name.zn` errors with the prescribed message; e2e green.

### 3.1.10 — `zinc --version` parser-feature tag
- Add a `--version` flag to `cmd/zinc/main.go`.
- Print: `zinc-go <semver> (parser-features: v2-2026-05-01)`. The `parser-features` string is bumped whenever the syntactic surface changes.
- **Verify:** `zinc-go --version` works; e2e green.

**Phase 3.1 outcome:** parser/lexer/AST match the spec's syntactic rules. All 10 commits should be green-runnable in sequence with no test regressions. After this phase: codegen still has its 24+ tracking maps, typechecker still dead. The pipeline is unchanged.

---

## Phase 3.2 — wire the typechecker (validate-only)

### 3.2.1 — Add `--typecheck` flag
- New flag on `cmd/zinc/main.go`: `--typecheck` (or env var `ZINC_TYPECHECK=1`).
- Plumb through to `compileMultiFile`.
- **Verify:** flag parses; default is off (no behavioral change).

### 3.2.2 — Wire `CheckV2` per file
- In `compileMultiFile`, after parsing all files: collect signatures via `CollectSignatures`, then run `CheckV2WithContext(prog, externalSigs)` per file.
- When flag is on, fail the build on any V2Error (with positions).
- When flag is off, ignore errors (parity with current behavior).
- **Verify:** with flag off, e2e green (no behavioral change); with flag on, e2e produces a triage list of failures.

### 3.2.3 — Triage CheckV2 false positives
- Run e2e with `--typecheck` on. Categorize each failure:
  - **Real bug** in user code that current pipeline missed → add to a "fixed by typecheck" list, fix the user code in `examples/` if it's a stale example.
  - **False positive** from CheckV2 not understanding a shape → file-list of CheckV2 fixes needed.
  - **CheckV2 gap** (e.g., FFI-imported type → typeAny) → defer to Phase 3.5.
- **Deliverable:** a markdown triage doc inline in `04-rebuild-plan.md` (this file), updated as we go.
- **Verify:** triage complete; commit count depends on bug-fix work.

### 3.2.4 — Fix CheckV2 false positives
- One commit per fix category. Could be 5-15 commits.
- After each: rerun `--typecheck` on full e2e suite; trend toward zero unaccounted failures.
- **Verify:** with `--typecheck` on, e2e green except for the documented "deferred to 3.5" cases.

**Phase 3.2 outcome:** CheckV2 is callable, surfaces some real type errors, has a known gap list for FFI. Codegen unchanged. Flag-gated; default off.

---

## Phase 3.3 — build the bind phase

### 3.3.1 — Define the bind side-map
- New file: `internal/typechecker/bind.go`.
- Types: `BoundProgram { Prog *parser.Program, Symbols map[*parser.Ident]*Symbol, NodeTypes map[parser.Expr]V2Type }`.
- `Symbol` enum: `LocalSym`, `ParamSym`, `FieldSym`, `MethodSym`, `FnSym`, `TypeSym`, `ImportPkgSym`, `GoPkgSym`, `BuiltinSym`.
- **Verify:** types compile; not yet used.

### 3.3.2 — Implement bind walk
- `func Bind(prog *parser.Program, ctx *BindContext) (*BoundProgram, []V2Error)`.
- Walks every `Ident` in the AST, resolves it per the 5-level order in `02-semantics.md` §3.1.
- Records collisions per the spec rule (a Zinc-level error with positions).
- Side-map is keyed by `*parser.Ident` pointer identity, so the same ident at different positions can resolve differently.
- **Verify:** unit tests on Bind for representative AST shapes; no codegen wire-up yet.

### 3.3.3 — Bind tests
- New test suite `internal/typechecker/bind_test.go`.
- One test per resolution shape: bare local, qualified pkg.X, collision detection, shadow rules, cross-pkg with pub.
- **Verify:** unit tests pass.

### 3.3.4 — Wire Bind into `--typecheck` mode
- In `compileMultiFile` with `--typecheck` on: run `Bind` after parse, before `CheckV2`.
- If Bind reports errors, abort.
- Pass the `BoundProgram` to `CheckV2` (signature evolves).
- **Verify:** e2e with `--typecheck` on still green (or trends greener as Bind catches more bugs that CheckV2 alone missed).

**Phase 3.3 outcome:** the bind phase produces a side-map. Codegen ignores it for now. Flag-gated.

---

## Phase 3.4 — migrate codegen to consume the bind side-map

This is the longest sub-phase by commit count — possibly 20-30 individual commits, each replacing one ad-hoc codegen lookup with a side-map read.

### 3.4.1 — Plumb `BoundProgram` to `Generator`
- New `Generator.bound *BoundProgram` field.
- New `gen.SetBoundProgram(bp)` method.
- `cmd/zinc/compiler.go` passes the BoundProgram through when `--typecheck` is on.
- **Verify:** plumbing only, no behavior change.

### 3.4.2 — Migrate `g.unqualifiedNames` lookups
- Find every `g.unqualifiedNames[name]` lookup. Replace with `g.bound.Symbols[ident]` lookup.
- Each ident expression already has a unique AST node, so the side-map gives a definite answer.
- This eliminates the on-the-fly resolution entirely — the answer is precomputed.
- **Verify:** e2e with `--typecheck` on stays green.

### 3.4.3 — Migrate `isUserScopeShadow`
- All shadow-gate consultations replaced with side-map reads.
- The 5 sub-tables (`varGoTypes`, `varTypes`, etc.) used by `isUserScopeShadow` are no longer queried for resolution purposes — their information lives in the bind side-map's symbol kind.
- **Verify:** today's shadow-gate self-stomp bug is no longer expressible. e2e green.

### 3.4.4 through 3.4.N — Migrate remaining codegen tables
- One per ad-hoc tracking field. Roughly: `varStructTypes`, `varTypeExprs`, `dataClassDecls`, `subpkgExports`, `subpkgStructs`, `funcSigs`, `typeAliases`, etc.
- Each commit: pick a field, remove the populator, replace consumer reads with side-map equivalents, run e2e.
- Stop when only the codegen-emit-time-only fields remain (e.g., `imports` for tracking what to add to the Go file).

### 3.4.X — Delete migrated tracking fields
- After each is no longer read by anyone, the field can be deleted from `Generator`.
- Single cleanup commit at the end of 3.4 that consolidates deletions.

**Phase 3.4 outcome:** codegen is a "consumer" of bound information, not a producer. The 24+ tracking maps shrink to ~5 emit-only ones (imports, current-class context for `this`, etc.).

---

## Phase 3.5 — extend typechecker for FFI (the Go-imported types story)

CheckV2 today knows nothing about Go-imported types, multi-value Go returns, or method calls on Go-typed receivers. After Phase 3.4, the codegen *does* (via `varGoTypes` populator added 2026-05-01), but in a side-channel. Phase 3.5 lifts that knowledge into the typechecker properly.

### 3.5.1 — `V2Type.GoType` field
- Add an optional `GoType types.Type` field on `V2Type` (or a new `V2GoType` variant).
- When CheckV2 encounters `pkg.Func(...)` with `pkg` in importMap, it consults `goResolver.FuncReturnType` and tags the resulting V2Type with the Go-resolved type.
- **Verify:** unit tests for FFI return-type tagging.

### 3.5.2 — Multi-value Go returns
- `TupleVarStmt` with RHS = Go-call: each name binds to the corresponding return slot's GoType.
- Today's codegen-side hack (added today, in `emitTupleVarStmt`) becomes redundant — Phase 3.4 already migrated it to the side-map.
- **Verify:** `var dec, derr = hambaOcf.NewDecoder(rdr)` produces `dec: *ocf.Decoder, derr: error` in the side-map.

### 3.5.3 — Method calls on Go-typed receivers
- When checking `recv.Method(args)`: if `recv` has a `GoType`, use `goResolver.MethodSet(recv.GoType)` to look up `Method`. Resolve return type, param types, etc.
- Permit `&x` in arg positions where the param is Go's `any`/`interface{}`.
- **Verify:** the FFI-method-call regression test (`hamba/ocf round-trip`) still green; the band-aid in `formatCallExpr` (`goMethodOnFFIVar`) becomes redundant and gets deleted.

**Phase 3.5 outcome:** CheckV2 fully understands the FFI surface. The codegen-side `varGoTypes` populator we added today gets deleted. The shadow-gate concept disappears.

---

## Phase 3.6 — new language features (per spec, not yet implemented)

### 3.6.1 — Generic bounds
- Parser: extend `type_params` to accept `<T : Bound1 + Bound2>`.
- AST: new `TypeParam.Bounds []TypeExpr` field.
- CheckV2: at every generic call site, verify that the chosen type arg satisfies all of the param's bounds.
- New built-in interfaces: `Comparable`, `Hashable`, `Equatable`, `Iterable<T>`, `Stringer` (per `03-type-system.md` §4.3).
- **Verify:** new examples `examples/generic_bounds.zn` and `examples-fail/generic_bound_violation.zn` work.

### 3.6.2 — Null safety
- CheckV2: `null` is compatible only with `T?` types; reject assignment of `null` to non-nullable.
- Smart-cast: after `if (x != null)`, `x` is treated as non-null in the then-branch.
- This is a **breaking change** for existing code that uses `null` on non-`T?` reference types. The user has accepted this for 1.0.
- Migration plan: run `--typecheck` on `zinc-flow-go`, fix call sites that break.
- **Verify:** new examples; existing examples migrate cleanly; zinc-flow-go's 123 tests stay green after migration.

### 3.6.3 — `==` on slices/maps is a compile error
- CheckV2: `a == b` where both are `T[]`, `List<T>`, `Map<K,V>`, `Set<T>` → error suggesting `slices.Equal(a, b)` or `maps.Equal(a, b)`.
- **Verify:** new fail test; existing examples don't trip this.

### 3.6.4 — Cross-pkg generic instantiation
- CheckV2: `A.Container<C.Thing>` instantiated in package B works end-to-end.
- Codegen: emits the right Go-generic instantiation with both A and C imports.
- **Verify:** new example `examples/cross_pkg_generic.zn`; existing cross-pkg tests stay green.

### 3.6.5 — Sealed-variant cross-pkg matching
- Bind: cross-package variant references in `match` patterns (`A.Result.Ok(v)`) resolve correctly.
- CheckV2: exhaustivity check works across packages.
- Codegen: emits the right type assertions on the Go interface.
- **Verify:** new example; existing sealed-class tests stay green.

**Phase 3.6 outcome:** the spec's new language features are live. Codegen and typechecker fully aligned with 1.0.

---

## Phase 3.7 — cleanup

### 3.7.1 — Default `--typecheck` to on
- Remove the flag; typecheck always runs.
- **Verify:** all examples green without the flag.

### 3.7.2 — Delete dead codegen tracking maps
- Remove fields from `Generator` that are no longer read after Phase 3.4 + 3.5.
- Squash any remaining duplicate populators.
- **Verify:** e2e green.

### 3.7.3 — Delete `internal/typechecker/go_types.go` Java stubs
- Remove `ZincToJavaClass`, `MethodThrows`, the hand-curated `ResolveZincMethodReturn`.
- Replace `ResolveZincMethodReturn` callers with goResolver-backed lookups.
- **Verify:** e2e green.

### 3.7.4 — Delete `Result[T]` / `isinstance` branches in CheckV2
- Confirm with grep that nothing parses `isinstance(...)` or `Result[T]`.
- Delete the branches in CheckV2.
- **Verify:** e2e green.

### 3.7.5 — Final spec sweep
- Cross-check `01-grammar.md` + `02-semantics.md` + `03-type-system.md` against the implemented compiler.
- Where they disagree, fix the code (the spec is authoritative).
- **Verify:** spec ↔ code consistency check passes.

**Phase 3.7 outcome:** clean tree, dead code gone, spec matches implementation.

---

## Risk register

1. **Phase 3.4 is 20+ commits.** Long. The risk is fatigue and an "almost done" trap where the last few migrations are deferred indefinitely. Mitigation: track progress in this doc; commit count visible.
2. **Phase 3.6.2 (null safety) breaks user code.** zinc-flow-go has many `null`-as-error-sentinel patterns. Migration is non-trivial. Mitigation: do this AFTER Phase 3.5 so the typechecker can give precise errors at every break point.
3. **Phase 3.5 (FFI in CheckV2) requires `goResolver` to be available at typecheck time.** Today it's a codegen-internal. Will need to lift it out. Probably one-day work, could surprise.
4. **Phase 3.6.4 (cross-pkg generics) is hairy.** This is the interaction the spec called out. Mitigation: lots of small examples, incremental rollout.
5. **The spec docs themselves may be wrong.** As we implement, we may discover the spec specifies behavior the user didn't actually want. Mitigation: every commit pause is a chance to flag spec-vs-intent disagreements.

---

## Status tracker

This section is the source of truth for "where are we in Phase 3." Updated after each commit lands.

### Phase 3.1 (cleanup commits)

- [x] 3.1.1 Drop `fn` keyword
- [x] 3.1.2 Drop `===` / `!==`
- [x] 3.1.3 Drop `??`
- [x] 3.1.4 Drop `<-` and dead `SendExpr`/`ReceiveExpr`
- [x] 3.1.5 Drop `:=` (kept `->` — used in match-expr / lambda)
- [x] 3.1.6 ~~`;` is a stmt-position parse error~~ — **reversed 2026-05-01**: `;` stays as an optional statement separator. Useful for multiple short stmts on a single line (`var x = 1; var y = 2`). Newline is canonical; `;` is a tolerance affordance. No code change required — matches existing parser behavior.
- [x] 3.1.7 Drop `OrHandler.MatchCases`
- [x] 3.1.8 Codegen non-`pub` consistency (added `SetSiblingPubs` plumbing)
- [x] 3.1.9 FQDN-on-collision Zinc-level error
- [x] 3.1.10 `zinc --version` parser-feature tag (`v2-2026-05-01`)

### Phase 3.2 (typechecker wire-up)

- [x] 3.2.1 `--typecheck` flag (env-var `ZINC_TYPECHECK=1`)
- [x] 3.2.2 Wire `CheckV2` per file (in `compileMultiFile`)
- [x] 3.2.3 Triage CheckV2 false positives → 1 found
- [x] 3.2.4 Fix CheckV2 false positives — 1 fix: `bool`/`boolean` type-name normalization in `canonicalTypeName`

**Phase 3.2 outcome:** with `ZINC_TYPECHECK=1`, all 96 e2e green, all 123 zinc-flow-go tests green. The typechecker is callable, surfaces no false positives, and the path is clear to wire it as the source-of-truth for bind/type queries in Phase 3.3.

### Phase 3.3 (bind phase)

- [x] 3.3.1 Define bind side-map types — `internal/typechecker/bind.go` (348 lines)
- [x] 3.3.2 Implement bind walk — `internal/typechecker/bind_walk.go` (326 lines)
- [x] 3.3.3 Bind tests — `internal/typechecker/bind_test.go` (7 tests, all pass)
- [x] 3.3.4 Wire Bind into `runTypecheck` — bind runs before CheckV2 with shared context

**Phase 3.3 outcome:** the bind phase is callable, produces a side-map keyed by `*parser.Ident` identity, and detects bare-name collisions per spec §3.2. With `ZINC_TYPECHECK=1`: e2e 96/96, zinc-flow-go 123/123. Note: cross-package import exports (ZincSubpkgExports / GoPkgExports) are stubbed empty in the bind context for now — collision detection across packages will activate when Phase 3.4 plumbs those through. Locals/params/fields/same-pkg resolution is fully working.

### Phase 3.4 (codegen migration)

- [x] 3.4.1 Plumb `BoundProgram` to `Generator` — `Generator.bound *typechecker.BoundProgram` + `SetBoundProgram`; threaded through `compileMultiFile` via `runTypecheck`'s new return value.
- [x] 3.4.2 PoC migration of Ident expression lookup — Local/Param/Field/Builtin cases short-circuit through the side-map.
- [x] 3.4.3 Migrate `isUserScopeShadow` — added side-map-aware `isUserScopeShadowIdent(*parser.Ident)`. 9 call sites updated. Today's shadow-gate self-stomp bug class structurally impossible when bound is set.
- [x] 3.4.4 Migrate same-pkg Fn/Const Idents to side-map.
- [x] 3.4.5 Wire bind+typecheck into `compileSubpackages` (multi-package projects). Includes per-prog AST reuse (avoid re-parse for emit) and `crossPkgExports` plumbing via `runTypecheck`'s new param.
- [x] 3.4.6 Migrate cross-pkg Fn/Type/Const/EnumVariant Idents to side-map. Added `Symbol.Owner` field for variant ownership (separate from package alias). Side-map fast path now handles same-pkg AND cross-pkg cases; `resolveUnqualifiedExpr` only kicks in when bind has no answer.
- [x] 3.4.7 Migrate `varGoTypes` consumer at FFI-method-call detection.
- [x] 3.4.8 Migrate `callReturnsPointer`'s method-on-tracked-var path.
- [x] 3.4.9 Migrate `inferSliceElemType`'s scalar-type lookup.
- [x] 3.4.10 Migrate channel-typedness fallback in `select` recv path.
- [x] 3.4.11 Migrate getter-pattern shortcut's struct-type lookup.
- [x] 3.4.12 Migrate cross-pkg constructor-call resolution in `formatCallExpr` (`Item(args)` → `lib.NewItem(args)` via side-map's SymType/SymFn).
- [x] 3.4.13 Migrate match-pattern type-switch detection (`isTypeSwitchMatch`) — recognizes SymType/SymSealedVariant via side-map.
- [x] 3.4.14 Migrate `callReturnsError`'s unqualified-Go-stdlib branch.
- [x] 3.4.15 Migrate `callIsVoidThrower`'s unqualified-Go-stdlib branch.
- [x] **3.4.16/17 Re-done** with granular SymKind. Added `SymClass`, `SymDataClass`, `SymInterface`, `SymEnum`, `SymTypeAlias` (split from the original SymType). `kindFromExport` returns granular kinds; `collectFileTopLevel` uses them for own-file decls. Existing readers updated to match the wider set. Match-stmt typeName + sealed-variant fieldParams migrations now consult the side-map cleanly.
- [ ] 3.4.18..N Other resolution / type-tracking migrations remaining (~5 sites, mostly string-keyed AST positions where bind doesn't yet have a TypeBindings side-map).
- [ ] 3.4.X Delete migrated fields

**Phase 3.4 false-positive fixes during this work** (CheckV2 / typechecker patches):
- `bool`/`boolean` and `Object`/`any` type-name normalization (`canonicalTypeName`).
- `MatchStmt`'s return-paths check no longer requires a literal `_` wildcard (codegen handles exhaustivity separately).
- `_ = expr` discard target tolerated in `checkAssignStmt`.
- Trailing-segment compatibility for qualified-vs-bare type names (e.g. `core.Schema` ≡ `Schema`) until Phase 3.5 wires per-call-site import resolution.

### Phase 3.5 (typechecker FFI)

- [x] 3.5.1 `V2Type.GoType` field + NodeTypes side-map.
- [x] 3.5.2 Tag Go-FFI call returns with GoType. `GoFFIResolver` interface; codegen's `*GoTypeResolver` implements it.
- [x] 3.5.3 Multi-value Go return slot tracking — `var dec, derr = pkg.NewDecoder(...)` populates each slot's GoType via `tupleSlotTypes` helper using `FuncReturnTypeAt(..., i)`.
- [x] 3.5.4 Method calls on Go-typed receivers — `recv.Method(...)` where `recv.GoType != nil` looks up Method via `MethodReturnTypeAt` (added to `*GoTypeResolver`, exposed via `GoFFIResolver` interface).

**Phase 3.5 outcome:** the typechecker fully understands the Go FFI surface. NodeTypes side-map carries GoType for every FFI-touching expression (package call, multi-value tuple-var, method on Go-typed receiver). Codegen migrations of `varGoTypes`/`varTypes`/`varStructTypes`/`varTypeExprs` are now structurally unblocked — replace their lookups with `bp.NodeTypes[expr]` reads.

### Phase 3.6 (new language features)

- [x] 3.6.1 Generic bounds — `<T : Comparable>` parser support; `V2FnSig.TypeParams`/`TypeParamBounds`; call-site type-param unification + bound-satisfaction check (`satisfiesBound` with primitive built-in table per spec §4.3); codegen emits Go constraints (`Comparable→cmp.Ordered`, `Hashable/Equatable→comparable`, `Stringer→fmt.Stringer`) via `goTypeParamsWithBounds` + `trackTypeParamImports`. Positive (`generic_bounds`) + negative (`generic_bound_violation`) e2e tests in place.
- [x] 3.6.2 Null safety — type compatibility rule + smart-cast on `if (x != null)`. zinc-flow-go migrated `readBody` to `(byte[], error)` shape. e2e (96/96) and zinc-flow-go suite green.
- [x] 3.6.3 `==` on slices/maps compile error. CheckV2's BinaryExpr handler emits a Zinc-level error suggesting `slices.Equal` / `maps.Equal`.
- [x] 3.6.4 Cross-pkg generic instantiation — verified end-to-end via `cross_pkg_generic` (2-pkg) and `cross_pkg_generic_3pkg` (A defines generic, B/workshop bridges, main consumes). Worked out-of-the-box on the existing codegen.
- [x] 3.6.5 Sealed-variant cross-pkg matching — `cross_pkg_sealed_qualified` example added; codegen bug fixed (qualified pattern `lib.Variant(binders)` was using binder names as field names because the SelectorExpr branch wasn't looking up `subpkgDataFields` by alias).

### Phase 3.7 (cleanup)

- [x] 3.7.1 Default `--typecheck` to on. `ZINC_TYPECHECK=0` escape hatch removed; typecheck unconditional. Bind/check wired into all four `codegen.New()` paths (single-file `compileFile`, multi-file, leaf packages, root files). Three latent typechecker bugs fixed that the gate had been masking: `Int`/`Bool` canonicalization, `error` accepts `null` (errors-as-values success path), variadic params propagate via `V2FnSig.Variadic`.
- [x] 3.7.2 Codegen → side-map consultation order — readers consult the bind side-map first (`inferExprType` Ident path, `isMapVar`/`isListVar`/`isChannelVar`, `isStructVar`, `resolveMethodReturnType`, `resolveReceiverClassName`, `resolveReceiverGenericType`, the Fn-type readers in `callReturnsError` / `callIsVoidThrower`). `Symbol.DeclType` added to carry declared `parser.TypeExpr` for SymLocal/SymParam, populated by the bind walk. Codegen tracking maps (`varTypes`/`varStructTypes`/`varTypeExprs`) remain as gap-filler for emit-time type tracking that bind/check doesn't cover (e.g. `var x = m[key]` → typechecker returns `any`; codegen's existing inference fills the gap). Original task framing ("delete dead maps") was wrong: the maps aren't dead, they cover complementary ground to the bind side-map. The deliverable is the consultation order: side-map first, codegen-tracking second.
- [x] 3.7.3 Delete `go_types.go` Java stubs (`ZincToJavaClass`, `MethodThrows` — both no-op).
- [x] 3.7.4 Delete `Result[T]` / `isinstance` dead branches in CheckV2.
- [x] 3.7.5 Final spec sweep — high-impact drifts resolved: 02-semantics.md §5.7 (`null` in error slots) + §5.2 examples switched from `err != nil` to `err != null`; §5.4 reworded for null-error semantics. 03-type-system.md §2.5 documents the `error`-implicit-null carve-out. `V2Type` for `bool` is spec-canonical `"bool"` (legacy spellings `Bool`/`Boolean`/`boolean` collapse via `canonicalTypeName`). Lexer keyword count corrected in 01-grammar.md §1.2 (45 keywords; `construct` removed since the parser never used it; `assert`/`Err`/`void` documented as spec-reserved IDENTs).

---

## Estimate

- Phase 3.1: 1–2 days (10 small commits)
- Phase 3.2: 3–5 days (4 commits + triage)
- Phase 3.3: 1 week (4 commits + tests)
- Phase 3.4: 2–3 weeks (20–30 commits, mechanical but careful)
- Phase 3.5: 1 week (3 commits + integration)
- Phase 3.6: 2–3 weeks (5 commits, each with example + fail-test work)
- Phase 3.7: 2–3 days (5 cleanup commits)

**Total: 7–10 weeks of focused work.** Less if the user reviews fast and commits don't get blocked; more if Phase 3.4 surfaces issues that need spec revisions.

---

## What I want from you next

Sign off (or override) on:
1. **Sequencing.** Is 3.1 → 3.2 → 3.3 → 3.4 → 3.5 → 3.6 → 3.7 the right order? Or do you want, say, 3.6.2 (null safety) earlier because it'll surface bugs in user code that we want to find before the bigger migrations?
2. **First commit.** I'm proposing 3.1.1 (drop `fn` keyword) as the first commit. Smallest, lowest-risk. Confirm.
3. **Pacing.** Do you want to review every commit, or only the boundaries between sub-phases (3.1 → 3.2, etc.)? Default I'd suggest: every commit in 3.1 + 3.2 + 3.3 (small/foundational); every 5 commits in 3.4 (mechanical); every commit in 3.5 + 3.6 (new behavior).
4. **Anything missing.** Stuff in the spec that needs implementation but isn't in the plan, or stuff in the plan that doesn't match the spec.
