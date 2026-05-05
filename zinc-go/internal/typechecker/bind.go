// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package typechecker

// Bind phase — Phase 3.3 of the rebuild.
//
// The bind phase walks the parsed AST and resolves every bare `Ident` to
// a definite `Symbol` per the 5-level resolution order specified in
// `docs/grammar/02-semantics.md` §3.1:
//
//   1. Local scope        (vars, params, lambda args, with-resources, match-arm bindings)
//   2. Function scope     (params of the enclosing function/method/lambda)
//   3. Class scope        (fields of the current class, when inside a method/ctor)
//   4. Same-package decls (any top-level decl in any file of the current package)
//   5. Imported zinc subpackage exports
//   6. Imported Go-package exports
//   7. Go builtins        (error, len, cap, make, etc.)
//
// First match wins. A user-introduced name (steps 1-3) shadows package-import
// names with the same identifier. When steps 5+6 together match a name in
// two or more packages, the bind phase records a collision and emits a
// Zinc-level error pointing at qualification.
//
// Output is a `BoundProgram` whose `Bindings` side-map is keyed by
// `*parser.Ident` pointer identity — the same name at different positions
// can resolve differently. Codegen consumes the side-map directly via
// node-identity lookups, eliminating the on-the-fly resolution path that
// today's `Generator` does via 24+ ad-hoc tracking maps.

import (
	"fmt"
	"sort"
	"strings"

	"zinc-go/internal/parser"
)

// IsTypeKind reports whether the symbol resolves to a type — class, data
// class, interface, enum, type alias, or the generic SymType fallback.
// Used by codegen sites that don't care about the granular flavor (e.g.
// "is this ident a known type to qualify cross-pkg?").
func (k SymbolKind) IsTypeKind() bool {
	switch k {
	case SymType, SymClass, SymDataClass, SymInterface, SymEnum, SymTypeAlias:
		return true
	}
	return false
}

// SymbolKind enumerates what a bound `Ident` resolves to.
type SymbolKind int

const (
	SymUnknown SymbolKind = iota

	// User scope
	SymLocal         // var, for-loop var, lambda arg, with-resource, match binding
	SymParam         // function/method/ctor parameter
	SymField         // class/data class field, accessed bare in a method body via implicit `this`
	SymThis          // explicit `this` reference
	SymSuper         // explicit `super` reference

	// Same-package
	SymFn            // top-level function in current package
	SymMethod        // method on a class (resolved via receiver type, not bare; reserved)
	SymType          // generic type-symbol — used when finer granularity isn't available (back-compat)
	SymClass         // regular class (instances are *T pointers)
	SymDataClass     // data class (instances are T values)
	SymInterface     // interface (also covers sealed classes, which lower to Go interfaces)
	SymEnum          // enum type
	SymTypeAlias     // type alias (`type X = Y`)
	SymConst         // const declaration
	SymEnumVariant   // member of an enum (e.g., `Red` when `enum Color { Red, Green, Blue }`)
	SymSealedVariant // data variant of a sealed class

	// Imports
	SymZincPkg       // zinc subpackage import alias (e.g., `core`, `fabric/registry`)
	SymGoPkg         // Go-imported package alias (e.g., `hambaAvro`, `strings`)

	// Builtins
	SymBuiltin       // Go builtin (error, len, cap, make, append, etc.)
)

func (k SymbolKind) String() string {
	switch k {
	case SymLocal:
		return "local"
	case SymParam:
		return "param"
	case SymField:
		return "field"
	case SymThis:
		return "this"
	case SymSuper:
		return "super"
	case SymFn:
		return "fn"
	case SymMethod:
		return "method"
	case SymType:
		return "type"
	case SymClass:
		return "class"
	case SymDataClass:
		return "data"
	case SymInterface:
		return "interface"
	case SymEnum:
		return "enum"
	case SymTypeAlias:
		return "type-alias"
	case SymConst:
		return "const"
	case SymEnumVariant:
		return "enum-variant"
	case SymSealedVariant:
		return "sealed-variant"
	case SymZincPkg:
		return "zinc-pkg"
	case SymGoPkg:
		return "go-pkg"
	case SymBuiltin:
		return "builtin"
	}
	return "unknown"
}

// Symbol is the resolved meaning of a bound `Ident`.
type Symbol struct {
	Kind     SymbolKind
	Name     string // the zinc-side name
	Pkg      string // for cross-pkg refs: alias of the source package; "" for same-pkg or local
	Owner    string // for SymEnumVariant / SymSealedVariant / SymMethod: enum / sealed / class name
	DeclLine int    // declaration site line number, 0 if not tracked (used by future "go to def")
	// DeclType (Phase 3.7.2): the declared TypeExpr at the declaration
	// site for SymLocal/SymParam/SymField. Carries the original AST so
	// codegen can walk into Fn types (`Fn<(...), error>`) for thrower
	// detection and into generic types for type-arg substitution.
	// Nil when the type was inferred (`var x = expr`) or N/A.
	DeclType parser.TypeExpr
	// IsPub (Phase C/P1.3): set iff the declaring decl had `pub`. Carried
	// by SymFn / SymClass / SymDataClass / SymInterface / SymConst /
	// SymEnum and the field/method aspects of class symbols. Replaces
	// the codegen-side g.pubNames map in a future P3 commit; today it's
	// populated but not yet read.
	IsPub bool
}

// BoundProgram is the bind phase's output: the parsed AST plus side-maps
// keyed by AST node pointer identity.
//
// `Bindings` (Phase 3.3) — every Ident → Symbol resolution.
// `NodeTypes` (Phase 3.5) — every Expr → V2Type from CheckV2's inference.
//   Optional; nil when the typecheck driver didn't populate it. Codegen
//   should null-check before consulting.
type BoundProgram struct {
	Prog      *parser.Program
	Bindings  map[*parser.Ident]Symbol
	NodeTypes map[parser.Expr]V2Type
	// Sigs (Phase C/P1): package-level CollectedSigs aggregate produced
	// by the typecheck driver. Shared pointer across every program in
	// the same package — gives codegen a single canonical source for
	// FnSigs / MethodSigs / ClassFields / ClassNames / ParentTypes
	// that previously lived in parallel codegen-side maps. nil when
	// the typecheck driver didn't attach one (e.g. legacy single-file
	// paths that bypass runTypecheck).
	Sigs *CollectedSigs
	// TypeAliases (Phase C/P1.4): file-local `type Name = TypeExpr`
	// bindings. Replaces codegen-side g.typeAliases in a future P3
	// commit. Populated during Bind from TypeAliasDecl nodes; map is
	// nil when the file has no type aliases.
	TypeAliases map[string]parser.TypeExpr
}

// LookupSymbolByName scans Bindings for any Ident with the given name and
// returns the first matching Symbol. O(N) over Bindings, but typical use is
// codegen-time pub-status / kind queries that don't run in tight loops.
// Returns (Symbol{}, false) when no match. Pre-condition: bp != nil.
//
// When multiple Idents share a name (e.g. method-call site + decl), they
// resolve to the same Symbol if they refer to the same decl, so any match
// is correct for the kind/pub questions codegen asks. For shadowing cases
// (a SymLocal hiding a SymFn), the first hit may be either — codegen
// should prefer per-Ident bindings via Bindings[ident] when the AST node
// is available.
func (bp *BoundProgram) LookupSymbolByName(name string) (Symbol, bool) {
	for _, sym := range bp.Bindings {
		if sym.Name == name {
			return sym, true
		}
	}
	return Symbol{}, false
}

// BindContext supplies cross-package and cross-file information needed to
// resolve `Ident`s correctly. The caller (compiler driver) collects this
// from all parsed programs in a package and passes the same context to
// every per-file Bind() call.
type BindContext struct {
	// SiblingFns / SiblingTypes / SiblingConsts / SiblingEnumVariants — names
	// declared in OTHER files of the SAME package as the program being bound.
	// Population is the driver's responsibility (see CollectBindContext below).
	SiblingFns          map[string]bool
	SiblingTypes        map[string]bool
	SiblingConsts       map[string]bool
	SiblingEnumVariants map[string]string // variant → enum type name
	SiblingSealed       map[string]string // variant → sealed parent name

	// ZincSubpkgExports — exported names from imported zinc subpackages.
	// Keyed by package alias (`core`, `fabric/registry`, etc.).
	// Inner map: exported name → kind ("data", "class", "func", "interface",
	// "const", "type", "enum_variant").
	ZincSubpkgExports map[string]map[string]string

	// GoPkgExports — exported names from imported Go packages.
	// Same shape as ZincSubpkgExports but for Go imports.
	GoPkgExports map[string]map[string]string

	// ImportAliases — the set of import aliases that appear in the current
	// file's `import` declarations. Names from steps 5/6 only count if the
	// alias is actually imported.
	ImportAliases map[string]bool
}

// goBuiltinNames mirrors the codegen's set; bind phase needs the same list
// to mark builtins as never-shadowable-by-imports (but shadowable by locals).
var goBuiltinNames = map[string]bool{
	"error": true, "any": true, "comparable": true,
	"bool": true, "byte": true, "rune": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "float32": true, "float64": true,
	"complex64": true, "complex128": true, "string": true,
	"len": true, "cap": true, "make": true, "new": true, "append": true,
	"copy": true, "delete": true, "close": true, "panic": true, "recover": true,
	"complex": true, "real": true, "imag": true, "print": true, "println": true,
	"true": true, "false": true, "nil": true, "iota": true,
	"min": true, "max": true, "clear": true,
}

// --- Binder state ----------------------------------------------------------

type binder struct {
	ctx      *BindContext
	bindings map[*parser.Ident]Symbol
	errors   []V2Error

	// scopes is a stack of lexical scopes. scopes[0] is the file scope (which
	// receives same-pkg decls before walking begins). Function/method/block
	// entry pushes a new scope; exit pops.
	scopes []map[string]Symbol

	// currentClass tracks the class context for resolving `this`/field refs.
	currentClass     string
	currentClassFields map[string]bool

	// reportedCollisions dedups collision errors per (line, name) so repeated
	// uses don't produce repeated errors.
	reportedCollisions map[string]bool
}

func newBinder(ctx *BindContext) *binder {
	if ctx == nil {
		ctx = &BindContext{}
	}
	return &binder{
		ctx:                ctx,
		bindings:           make(map[*parser.Ident]Symbol),
		scopes:             []map[string]Symbol{make(map[string]Symbol)},
		reportedCollisions: make(map[string]bool),
	}
}

func (b *binder) errorf(line int, format string, args ...any) {
	b.errors = append(b.errors, V2Error{
		Line:    line,
		Message: fmt.Sprintf(format, args...),
	})
}

func (b *binder) push() {
	b.scopes = append(b.scopes, make(map[string]Symbol))
}

func (b *binder) pop() {
	if len(b.scopes) > 1 {
		b.scopes = b.scopes[:len(b.scopes)-1]
	}
}

// declare adds `name` to the innermost scope with the given Symbol.
// If the name already exists in the innermost scope, the new declaration
// overwrites it (allowing legal re-declaration patterns like loop vars
// rebinding within blocks). True shadowing across scopes is fine — we
// never look up beyond the first hit.
func (b *binder) declare(name string, sym Symbol) {
	if name == "" || name == "_" {
		return
	}
	b.scopes[len(b.scopes)-1][name] = sym
}

// lookupScope walks the scope stack inside-out, returning the first match.
func (b *binder) lookupScope(name string) (Symbol, bool) {
	for i := len(b.scopes) - 1; i >= 0; i-- {
		if sym, ok := b.scopes[i][name]; ok {
			return sym, true
		}
	}
	return Symbol{}, false
}

// resolve performs the full 5-level resolution for a bare identifier.
// Returns the bound Symbol, or Symbol{Kind: SymUnknown} when nothing matches.
// Records a collision error when steps 5+6 produce >1 match.
func (b *binder) resolve(name string, line int) Symbol {
	// 1-3. Local / param / class scopes (managed via the scope stack +
	// currentClassFields). Locals and params live in pushed scopes; class
	// fields live conceptually in the file scope's current-class context.
	if sym, ok := b.lookupScope(name); ok {
		return sym
	}
	if b.currentClass != "" && b.currentClassFields[name] {
		return Symbol{Kind: SymField, Name: name, Pkg: b.currentClass}
	}

	// 4. Same-package decls.
	if b.ctx.SiblingFns[name] {
		return Symbol{Kind: SymFn, Name: name}
	}
	if b.ctx.SiblingTypes[name] {
		return Symbol{Kind: SymType, Name: name}
	}
	if b.ctx.SiblingConsts[name] {
		return Symbol{Kind: SymConst, Name: name}
	}
	if enum, ok := b.ctx.SiblingEnumVariants[name]; ok {
		return Symbol{Kind: SymEnumVariant, Name: name, Owner: enum}
	}
	if sealed, ok := b.ctx.SiblingSealed[name]; ok {
		return Symbol{Kind: SymSealedVariant, Name: name, Owner: sealed}
	}

	// 4.5. Package alias itself — when `name` matches an imported alias,
	// bind it as the package symbol (not a name from that package). This
	// is the case for `core` in `core.Schema(...)` — the receiver Ident
	// is the alias.
	if b.ctx.ImportAliases[name] {
		if _, isZinc := b.ctx.ZincSubpkgExports[name]; isZinc {
			return Symbol{Kind: SymZincPkg, Name: name}
		}
		if _, isGo := b.ctx.GoPkgExports[name]; isGo {
			return Symbol{Kind: SymGoPkg, Name: name}
		}
		// Imported but exports not loaded at this layer (Phase 3.3-era
		// stub for Go pkgs in compileMultiFile). Default to GoPkg —
		// matches the common case (Go stdlib + third-party deps).
		return Symbol{Kind: SymGoPkg, Name: name}
	}

	// 5+6. Imported package exports. Detect collisions across the union of
	// zinc and Go imports.
	var matches []string // package aliases that export `name`
	for alias, exports := range b.ctx.ZincSubpkgExports {
		if !b.ctx.ImportAliases[alias] {
			continue
		}
		if _, ok := exports[name]; ok {
			matches = append(matches, alias)
		}
	}
	for alias, exports := range b.ctx.GoPkgExports {
		if !b.ctx.ImportAliases[alias] {
			continue
		}
		if _, ok := exports[name]; ok {
			matches = append(matches, alias)
		}
	}
	if len(matches) > 1 {
		b.reportCollision(line, name, matches)
		return Symbol{Kind: SymUnknown, Name: name}
	}
	if len(matches) == 1 {
		alias := matches[0]
		// Determine kind from the exporting package's export table.
		if exports, ok := b.ctx.ZincSubpkgExports[alias]; ok {
			if k, ok := exports[name]; ok {
				return Symbol{Kind: kindFromExport(k), Name: name, Pkg: alias}
			}
		}
		if exports, ok := b.ctx.GoPkgExports[alias]; ok {
			if k, ok := exports[name]; ok {
				return Symbol{Kind: kindFromExport(k), Name: name, Pkg: alias}
			}
		}
	}

	// 7. Go builtins. Never shadowable by imports (already past steps 5+6),
	// but locals (1-3) shadow them, which we already returned above.
	if goBuiltinNames[name] {
		return Symbol{Kind: SymBuiltin, Name: name}
	}

	// Unresolved — Symbol{Kind: SymUnknown} with the original name preserved.
	// The caller may treat unresolved bare names as user errors at use site,
	// or leave them for later type-check / codegen passes to error on.
	return Symbol{Kind: SymUnknown, Name: name}
}

// kindFromExport maps the export-table kind string to a SymbolKind.
// Granular kinds when known (data / class / interface / enum / type alias);
// SymType is the "generic type" fallback for any string we don't recognise.
func kindFromExport(k string) SymbolKind {
	switch k {
	case "func":
		return SymFn
	case "data":
		return SymDataClass
	case "class":
		return SymClass
	case "interface":
		return SymInterface
	case "enum":
		return SymEnum
	case "type":
		return SymTypeAlias
	case "const":
		return SymConst
	case "enum_variant":
		return SymEnumVariant
	}
	return SymUnknown
}

// reportCollision emits a Zinc-level error for an ambiguous bare name.
// Dedups per (line, name) so the same site doesn't error repeatedly.
func (b *binder) reportCollision(line int, name string, pkgs []string) {
	key := fmt.Sprintf("%d:%s", line, name)
	if b.reportedCollisions[key] {
		return
	}
	b.reportedCollisions[key] = true
	sortedPkgs := append([]string(nil), pkgs...)
	sort.Strings(sortedPkgs)
	suggestions := make([]string, len(sortedPkgs))
	for i, p := range sortedPkgs {
		suggestions[i] = fmt.Sprintf("%s.%s", p, name)
	}
	b.errorf(line,
		"ambiguous bare name %q — exported by both %s. Use %s to disambiguate.",
		name, strings.Join(sortedPkgs, " and "), strings.Join(suggestions, " or "))
}

// --- Public entry point ----------------------------------------------------

// Bind walks `prog` and produces a BoundProgram with every reachable Ident
// resolved to a Symbol. Returns a list of bind-time errors (collisions,
// unresolved references in strict mode — currently only collisions).
//
// Phase 3.3 scope: handle the common AST shapes (decl + stmt + expr trees).
// Phase 3.4 will progressively migrate codegen consumers from ad-hoc lookup
// to side-map reads, and may surface bind-coverage gaps that require
// extensions here.
func Bind(prog *parser.Program, ctx *BindContext) (*BoundProgram, []V2Error) {
	b := newBinder(ctx)

	// Pre-populate file scope with this file's own top-level decls.
	// They're indistinguishable from sibling decls for resolution purposes,
	// but we put them in scopes[0] so the same lookup path works.
	b.collectFileTopLevel(prog)

	// Walk every declaration.
	for _, d := range prog.Decls {
		b.bindDecl(d)
	}
	// Walk top-level script statements.
	for _, s := range prog.Stmts {
		b.bindStmt(s)
	}

	// Collect type aliases declared at the top level. Codegen consumers
	// peel `type Foo = Fn<...>` aliases when resolving generic call args
	// and Fn lambda targets.
	var typeAliases map[string]parser.TypeExpr
	for _, d := range prog.Decls {
		if alias, ok := d.(*parser.TypeAliasDecl); ok {
			if typeAliases == nil {
				typeAliases = make(map[string]parser.TypeExpr)
			}
			typeAliases[alias.Name] = alias.Type
		}
	}

	return &BoundProgram{
		Prog:        prog,
		Bindings:    b.bindings,
		TypeAliases: typeAliases,
	}, b.errors
}

// collectFileTopLevel adds this file's top-level decls to the file scope.
// Same-package siblings (other files) are added via BindContext at construction.
func (b *binder) collectFileTopLevel(prog *parser.Program) {
	for _, d := range prog.Decls {
		switch decl := d.(type) {
		case *parser.FnDecl:
			b.declare(decl.Name, Symbol{Kind: SymFn, Name: decl.Name, DeclLine: decl.Line, IsPub: decl.IsPub})
		case *parser.ClassDecl:
			kind := SymClass
			if decl.IsSealed {
				kind = SymInterface // sealed class lowers to Go interface
			}
			b.declare(decl.Name, Symbol{Kind: kind, Name: decl.Name, DeclLine: decl.Line})
			if decl.IsSealed {
				for _, v := range decl.Variants {
					b.declare(v.Name, Symbol{Kind: SymSealedVariant, Name: v.Name, Owner: decl.Name, DeclLine: v.Line})
				}
			}
		case *parser.DataClassDecl:
			b.declare(decl.Name, Symbol{Kind: SymDataClass, Name: decl.Name, DeclLine: decl.Line})
		case *parser.InterfaceDecl:
			b.declare(decl.Name, Symbol{Kind: SymInterface, Name: decl.Name, DeclLine: decl.Line})
		case *parser.EnumDecl:
			b.declare(decl.Name, Symbol{Kind: SymEnum, Name: decl.Name, DeclLine: decl.Line})
			for _, v := range decl.Variants {
				b.declare(v, Symbol{Kind: SymEnumVariant, Name: v, Owner: decl.Name})
			}
		case *parser.ConstDecl:
			b.declare(decl.Name, Symbol{Kind: SymConst, Name: decl.Name, DeclLine: decl.Line, IsPub: decl.IsPub})
		case *parser.TypeAliasDecl:
			b.declare(decl.Name, Symbol{Kind: SymTypeAlias, Name: decl.Name, DeclLine: decl.Line})
		}
	}
}

// CollectBindContext builds a BindContext from a merged-program AST.
// Driver helper: same-package siblings are extracted into the context
// fields. Cross-package fields (ZincSubpkgExports / GoPkgExports /
// ImportAliases) are populated separately by the caller.
func CollectBindContext(merged *parser.Program) *BindContext {
	ctx := &BindContext{
		SiblingFns:          make(map[string]bool),
		SiblingTypes:        make(map[string]bool),
		SiblingConsts:       make(map[string]bool),
		SiblingEnumVariants: make(map[string]string),
		SiblingSealed:       make(map[string]string),
		ZincSubpkgExports:   make(map[string]map[string]string),
		GoPkgExports:        make(map[string]map[string]string),
		ImportAliases:       make(map[string]bool),
	}
	for _, d := range merged.Decls {
		switch decl := d.(type) {
		case *parser.FnDecl:
			ctx.SiblingFns[decl.Name] = true
		case *parser.ClassDecl:
			ctx.SiblingTypes[decl.Name] = true
			if decl.IsSealed {
				for _, v := range decl.Variants {
					ctx.SiblingSealed[v.Name] = decl.Name
				}
			}
		case *parser.DataClassDecl:
			ctx.SiblingTypes[decl.Name] = true
		case *parser.InterfaceDecl:
			ctx.SiblingTypes[decl.Name] = true
		case *parser.EnumDecl:
			ctx.SiblingTypes[decl.Name] = true
			for _, v := range decl.Variants {
				ctx.SiblingEnumVariants[v] = decl.Name
			}
		case *parser.ConstDecl:
			ctx.SiblingConsts[decl.Name] = true
		case *parser.TypeAliasDecl:
			ctx.SiblingTypes[decl.Name] = true
		}
	}
	return ctx
}
