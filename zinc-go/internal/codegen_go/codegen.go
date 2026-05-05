// Package codegen_go generates Go source code from Zinc AST.
//
// The code generator is split across several files:
//   - codegen.go       — Generator struct, initialization, Generate/GenerateFiles entry points
//   - codegen_types.go — Type declarations: classes, data classes, sealed, enums, interfaces
//   - codegen_stmts.go — Statement emission: var, assign, return, if, for, match, etc.
//   - codegen_exprs.go — Expression formatting: literals, calls, lambdas, string interp
//   - codegen_streams.go — Stream operations with loop fusion
//   - gotypes.go       — Go type introspection via go/types
package codegen_go

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
	"zinc-go/internal/typechecker"
)

// recvName is the Go identifier used for method/ctor receivers and as
// the lowering of `this` in expression position. `this` is a reserved
// Zinc keyword (TOKEN_THIS, never lexed as TOKEN_IDENT), so user code
// cannot name a variable, parameter, or field that collides with it —
// which makes it a safe, collision-free receiver name. Reading the
// generated Go, `this.Field` also tracks the Zinc source one-for-one.
const recvName = "this"

// Generator produces Go source from a Zinc AST.
type Generator struct {
	buf            strings.Builder
	indent         int
	className      string // derived from filename or "main"
	imports        map[string]bool
	interfaces     map[string]bool
	structs        map[string]*parser.ClassDecl
	sourceFile     string // for //line directives
	currentFields      map[string]bool   // field names of current class (for implicit self)
	currentFieldGoName map[string]string // zinc field name → Go field name (respects pub)
	currentMethods map[string]bool // method names of current class (for implicit self)
	currentParams  map[string]bool // parameter names (shadow field names)
	currentLocals  map[string]bool // locally-declared var names in scope (shadow fields)
	currentClass   string          // current class name (for pub member lookups)

	// Error handling
	currentReturnType     string            // return type of current function (for zero values in error returns)
	currentOuterReturnType string           // Go return type of the enclosing function, regardless of thrower status. Used by the try-IIFE tuple shape to know T for `(T, bool, error)`.
	currentReturnOptional bool              // true if current function returns T? (pointer type)
	currentFuncParams     []*parser.ParamDecl // params of current function (for lambda type inference)
	currentMethodRetType  string            // Go return type of current method (for channel recv type assertions)

	// Default parameters
	funcSigs map[string][]*parser.ParamDecl // function name → param list

	// Stream operations
	chainCounter int // counter for _chain variables

	// Scope tracking
	errVarCount   int    // counter for unique _err variables in same scope
	currentErrVar string // current error variable name (for or-blocks)

	// currentFuncIsThrower is true while emitting a function whose
	// signature has a trailing `error` return. Drives emitErrReturn:
	// throwers propagate via `return zero, err`; non-throwers panic
	// (unchecked-exception semantics — e.g. main() with uncaught).
	currentFuncIsThrower bool

	// currentReturnIsTuple is true while emitting a function whose
	// declared return type is a TupleType (e.g. `pub (Int, String) foo()`).
	// Drives emitReturnStmt: a TupleLit return value lowers to Go's
	// multi-value `return a, b` form instead of the default `[]interface{}`
	// slice lowering used for tuple values in expression position.
	currentReturnIsTuple bool

	// currentThrowerValueGoTypes holds the Go-formatted types of the
	// value slots of the current thrower's return signature, with the
	// trailing `error` slot peeled off. nil/empty for non-throwers and
	// for bare-error void throwers. Used by emitReturnStmt to render
	// per-slot zero values when emitting `return zero1, ..., zeroN, err`
	// for an error-only return (`return SomeError(...)`) from a
	// multi-value-thrower signature.
	currentThrowerValueGoTypes []string

	// currentReturnIsDeclaredThrower distinguishes declared-thrower
	// functions (signature explicitly contains `error` in the tail —
	// new design) from legacy auto-widen throwers detected by body
	// inspection. Drives emitReturnStmt's per-slot zero-fill path
	// instead of the single-slot currentReturnType-based fallback.
	currentReturnIsDeclaredThrower bool

	// Variable type tracking
	varTypes            map[string]string       // variable name → element type
	ptrVars             map[string]bool         // variables that are pointers (*T from T? returns)
	funcReturnTypes     map[string]string     // function name → Go return type string
	renamedVars         map[string]string     // original name → safe name (for builtin shadows)
	dataClasses         map[string]bool       // data class names that have NewType constructors
	dataClassDecls      map[string]*parser.DataClassDecl // data class name → full decl (for implicit-self in methods)
	typeAliases         map[string]parser.TypeExpr // type alias name → underlying type
	goResolver          *GoTypeResolver       // introspects Go packages at transpile time
	importMap           map[string]string     // import prefix → full Go package path
	typeImports         map[string]string     // short type name → qualified Go name (e.g. "Mutex" → "sync.Mutex")
	activeTypeParams    map[string]bool       // currently-in-scope generic type parameter names

	// Visibility tracking
	pubNames         map[string]bool   // names declared with pub (functions, methods, fields, consts)

	// Subpackage support
	packageName      string            // Go package name (default: "main")
	moduleName       string            // Go module name from zinc.toml (for subpackage import paths)
	zincSubpackages  map[string]bool   // known zinc subpackage names (directory names in src/)
	subpkgExports    map[string]map[string]string // pkg → name → kind ("data", "class", "func", "interface")
	subpkgDataFields map[string]map[string][]*parser.FieldDecl   // pkg → data class name → field params
	localDataFields  map[string][]*parser.FieldDecl              // current package data class name → params
	subpkgStructs    map[string]map[string]*parser.ClassDecl    // pkg → class name → full class decl (for method lookups)
	subpkgTypeAliases map[string]map[string]parser.TypeExpr    // pkg → alias name → underlying TypeExpr (for cross-pkg resolveFuncTypeExpr)
	importAliases    map[string]string // import alias → Go module path (e.g. "stdlib" → "github.com/ZincScale/zinc-stdlib")
	importGoAliases  map[string]string // Go import path → local alias (when alias differs from package name)

	// Unqualified import resolution: bare name → package + kind
	// Built from subpkgExports after import processing. Allows writing
	// Processor instead of lib.Processor when import lib is declared.
	unqualifiedNames      map[string]unqualifiedEntry
	unqualifiedCollisions map[string][]string // name → list of packages that export it
	collisionsReported    map[string]bool     // dedup key "line:name" for collision errors

	// Compile-time errors accumulated during codegen (e.g., non-exhaustive match).
	// Checked by the caller after GenerateFiles returns.
	compileErrors   []string
	compileWarnings []string

	// needsPtrHelper marks that the generated file references _zincPtr,
	// the generic helper that boxes a value into a pointer. Required for
	// `String? foo = "hi"` and similar — Go disallows `&"hi"` directly.
	needsPtrHelper bool

	// addrOfAllowed is true at the start of formatExpr only when the
	// caller has just entered the top-level expression of a Go-library
	// (FFI) call argument. The flag is consumed (cleared) on entry to
	// every formatExpr call, so only the immediate UnaryExpr{Op:"&"}
	// at the top of an FFI arg sees it set. Anywhere else — assignments,
	// returns, var inits, args of zinc-side calls, nested sub-expressions
	// — the flag is false, and a `&x` there is rejected with a clear
	// compile error. This is the only acceptable use of `&` in zinc.
	addrOfAllowed bool

	// addrOfReported guards against re-reporting the same `&` once a
	// misplaced occurrence has been logged at the formatExpr UnaryExpr
	// site. Without it, downstream emit paths that re-format the same
	// expression (e.g. retry paths) would emit duplicate errors.
	addrOfReported map[*parser.UnaryExpr]bool

	// bound is the Phase 3.3 BoundProgram. Nil when typecheck mode is off.
	// Phase 3.4 progressively migrates codegen branches from ad-hoc lookup
	// (the 24+ tracking fields above) to side-map reads via this field.
	bound *typechecker.BoundProgram

	// inferredChanElem maps an untyped `Channel(N)` CallExpr to the Go
	// element type inferred from `ch.send(x)` sites in the same scope.
	// Populated by inferChannelTypes (a pre-walk over a function body)
	// and consulted by the Channel constructor emit so we can emit
	// `chan string` instead of falling back to `chan interface{}`.
	inferredChanElem map[*parser.CallExpr]string
}

// wrapAsPointer formats `_zincPtr(expr)` and flags the helper for
// emission. The helper is `func _zincPtr[T any](v T) *T { return &v }`,
// which makes auto-address-take work uniformly across literal values
// (Go disallows `&"hi"`) and named values.
func (g *Generator) wrapAsPointer(formatted string) string {
	g.needsPtrHelper = true
	return fmt.Sprintf("_zincPtr(%s)", formatted)
}

// CompileErrors returns any compile-time errors the generator detected during
// code generation. A non-empty result should cause the compile to fail.
func (g *Generator) CompileErrors() []string {
	return g.compileErrors
}

// CompileWarnings returns non-fatal advisory messages the generator detected
// during code generation. The driver prints them to stderr but the build
// proceeds. Used for things like `Channel(N)` without a type argument
// (lowers to `chan interface{}` — legal but stylistically loose).
func (g *Generator) CompileWarnings() []string {
	return g.compileWarnings
}

// compileError records a compile-time error with source location. Formatted to
// match Go compiler's "file:line: message" style so editors highlight correctly.
func (g *Generator) compileError(line int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	loc := g.sourceFile
	if loc == "" {
		loc = "<input>"
	}
	g.compileErrors = append(g.compileErrors, fmt.Sprintf("%s:%d: %s", loc, line, msg))
}

// compileWarning records a non-fatal advisory. Same "file:line: message"
// format as compileError; the driver prefixes with `warning: ` so editors
// still parse the location but humans can scan past.
func (g *Generator) compileWarning(line int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	loc := g.sourceFile
	if loc == "" {
		loc = "<input>"
	}
	g.compileWarnings = append(g.compileWarnings, fmt.Sprintf("%s:%d: warning: %s", loc, line, msg))
}

// Name resolution, type formatting, and visibility helpers are in codegen_resolve.go.

// New creates a new Go code generator.
func New() *Generator {
	return &Generator{
		imports:             make(map[string]bool),
		interfaces:          make(map[string]bool),
		structs:             make(map[string]*parser.ClassDecl),
		funcSigs:            make(map[string][]*parser.ParamDecl),
		varTypes:            make(map[string]string),
		ptrVars:             make(map[string]bool),
		funcReturnTypes:     make(map[string]string),
		renamedVars:         make(map[string]string),
		dataClasses:         make(map[string]bool),
		typeAliases:         make(map[string]parser.TypeExpr),
		goResolver:          NewGoTypeResolver(),
		importMap:           make(map[string]string),
		typeImports:         make(map[string]string),
		pubNames:            make(map[string]bool),
		addrOfReported:      make(map[*parser.UnaryExpr]bool),
	}
}

// OutputFile represents a generated .go file.
type OutputFile struct {
	Name    string
	Content string
}

// SetSourceFile sets the source .zn filename for //line directives.
func (g *Generator) SetSourceFile(path string) {
	g.sourceFile = path
}

// SetPackageName sets the Go package name (default: "main").
func (g *Generator) SetPackageName(name string) {
	g.packageName = name
}

// SetModuleName sets the Go module name for resolving subpackage imports.
func (g *Generator) SetModuleName(name string) {
	g.moduleName = name
}

// SetZincSubpackages sets the known zinc subpackage names.
func (g *Generator) SetZincSubpackages(pkgs map[string]bool) {
	g.zincSubpackages = pkgs
}

// SetGoModDir sets the directory containing go.mod for module dependency resolution.
func (g *Generator) SetGoModDir(dir string) {
	g.goResolver.SetDir(dir)
}

// SetBoundProgram attaches a Phase 3.3 BoundProgram. When set, codegen
// consumes the side-map (resolving every Ident to a Symbol) instead of
// running its own ad-hoc resolution. Phase 3.4 progressively migrates
// codegen branches from on-the-fly lookup to side-map reads; until the
// migration is complete, both paths coexist (gated by `g.bound != nil`).
func (g *Generator) SetBoundProgram(bp *typechecker.BoundProgram) {
	g.bound = bp
}

// SetImportAliases sets the import alias → module path mappings from zinc.toml [deps].
func (g *Generator) SetImportAliases(aliases map[string]string) {
	g.importAliases = aliases
}

// SetSiblingExports registers names from sibling files in the same package.
// These are types, functions, etc. declared in other .zn files in the same directory.
// Go handles cross-file visibility natively within a package via package-level
// scoping. The codegen records the names so it can route calls correctly, but
// the `pub` bit must be plumbed separately via SetSiblingPubs because exports
// alone don't tell us which decls were `pub`-declared. Per the 2026-05-01 spec
// decision, only user-declared `pub` modifiers belong in `g.pubNames` —
// same-package access does not require `pub`.
func (g *Generator) SetSiblingExports(exports map[string]string) {
	for name, kind := range exports {
		switch kind {
		case "data":
			g.dataClasses[name] = true
		case "class":
			// Mark as known struct with a placeholder ClassDecl (not nil)
			// so codegen can resolve constructor calls (NewType) and pointer types.
			g.structs[name] = &parser.ClassDecl{Name: name}
		case "interface":
			g.interfaces[name] = true
		case "func":
			// Register sibling functions so the codegen knows about them.
			// Casing at call sites is determined by `pubNames` membership.
			if g.funcSigs == nil {
				g.funcSigs = make(map[string][]*parser.ParamDecl)
			}
			if _, exists := g.funcSigs[name]; !exists {
				g.funcSigs[name] = nil // mark as known function (no param info)
			}
		}
	}
}

// SetSiblingPubs registers which sibling-file names were `pub`-declared.
// Companion to SetSiblingExports. The user-pub bit can't be inferred from
// exports alone (`CollectExports` returns name→kind without pub-ness).
func (g *Generator) SetSiblingPubs(pubs map[string]bool) {
	for name, isPub := range pubs {
		if isPub {
			g.pubNames[name] = true
		}
	}
}

// CollectPubs returns the names of `pub`-declared top-level decls.
// Companion to CollectExports. Used by SetSiblingPubs in compileMultiFile.
func CollectPubs(prog *parser.Program) map[string]bool {
	pubs := make(map[string]bool)
	for _, d := range prog.Decls {
		switch decl := d.(type) {
		case *parser.FnDecl:
			if decl.IsPub {
				pubs[decl.Name] = true
			}
		case *parser.ConstDecl:
			if decl.IsPub {
				pubs[decl.Name] = true
			}
		}
	}
	return pubs
}

// SetSubpackageExports registers exported names from a subpackage.
func (g *Generator) SetSubpackageExports(pkg string, exports map[string]string) {
	if g.subpkgExports == nil {
		g.subpkgExports = make(map[string]map[string]string)
	}
	g.subpkgExports[pkg] = exports
}

// CollectExports returns a map of exported names from a parsed program.
// Keys are zinc names, values are kinds: "data", "class", "func", "interface".
func CollectExports(prog *parser.Program) map[string]string {
	exports := make(map[string]string)
	for _, d := range prog.Decls {
		switch decl := d.(type) {
		case *parser.DataClassDecl:
			exports[decl.Name] = "data"
		case *parser.ClassDecl:
			if decl.IsSealed {
				exports[decl.Name] = "interface" // sealed classes are Go interfaces
				for _, v := range decl.Variants {
					exports[v.Name] = "data"
				}
			} else {
				exports[decl.Name] = "class"
			}
		case *parser.InterfaceDecl:
			exports[decl.Name] = "interface"
		case *parser.FnDecl:
			if decl.Name != "main" {
				exports[decl.Name] = "func"
			}
		case *parser.EnumDecl:
			exports[decl.Name] = "enum"
			for _, v := range decl.Variants {
				exports[v] = "enum_variant"
			}
		case *parser.ConstDecl:
			exports[decl.Name] = "const"
		case *parser.TypeAliasDecl:
			exports[decl.Name] = "type"
		}
	}
	return exports
}

// CollectDataClassFields returns data class field declarations for cross-package
// match destructuring. Keys are data class names, values are their ordered params.
func CollectDataClassFields(prog *parser.Program) map[string][]*parser.FieldDecl {
	fields := make(map[string][]*parser.FieldDecl)
	for _, d := range prog.Decls {
		switch decl := d.(type) {
		case *parser.DataClassDecl:
			fields[decl.Name] = decl.Params
		case *parser.ClassDecl:
			if decl.IsSealed {
				for _, v := range decl.Variants {
					fields[v.Name] = v.Params
				}
			}
		}
	}
	return fields
}

// CollectClassDecls returns full class declarations for cross-package method lookups.
func CollectClassDecls(prog *parser.Program) map[string]*parser.ClassDecl {
	classes := make(map[string]*parser.ClassDecl)
	for _, d := range prog.Decls {
		if decl, ok := d.(*parser.ClassDecl); ok && !decl.IsSealed {
			classes[decl.Name] = decl
		}
	}
	return classes
}

// CollectTypeAliases gathers `type Name = TypeExpr` declarations from a
// program. Used to propagate subpackage aliases across generators so
// `resolveFuncTypeExpr` can peel a SimpleType-name through an alias
// declared in a different package — required for cross-package method
// arg resolution where the param type is a Fn-alias from the callee's
// package (e.g. `Factory` → `Fn<(...), (T, error)>`).
func CollectTypeAliases(prog *parser.Program) map[string]parser.TypeExpr {
	aliases := make(map[string]parser.TypeExpr)
	for _, d := range prog.Decls {
		if decl, ok := d.(*parser.TypeAliasDecl); ok {
			aliases[decl.Name] = decl.Type
		}
	}
	return aliases
}

// v2ReturnsError reports whether a typechecker V2Type return signature
// has trailing `error` — i.e. the function/method is a thrower. Mirrors
// the codegen-side returnTypeDeclaresError but reads V2's tuple shape
// (Name:"tuple", Args:[..., error]).
func v2ReturnsError(rt typechecker.V2Type) bool {
	if rt.Name == "error" {
		return true
	}
	if rt.Name == "tuple" && len(rt.Args) > 0 {
		return rt.Args[len(rt.Args)-1].Name == "error"
	}
	return false
}

// fnReturnsError reports whether a top-level function (by bare name) is
// a thrower. Reads bound.Sigs.FnSigs (cross-pkg + cross-file aware).
// Returns false when bound is unavailable — only legacy single-file paths
// without typecheck wiring would lose detection, and runTypecheck attaches
// Sigs on every normal compile path (compileFile, compileMultiFile,
// compileDirWithSubpackages).
func (g *Generator) fnReturnsError(name string) bool {
	if g.bound != nil && g.bound.Sigs != nil {
		if fsig, ok := g.bound.Sigs.FnSigs[name]; ok {
			return v2ReturnsError(fsig.ReturnType)
		}
	}
	return false
}

// methodReturnsError reports whether a method (class + method name) is
// a thrower. Same lookup precedence as fnReturnsError.
func (g *Generator) methodReturnsError(class, method string) bool {
	if g.bound != nil && g.bound.Sigs != nil {
		if methods, ok := g.bound.Sigs.MethodSigs[class]; ok {
			if msig, ok := methods[method]; ok {
				return v2ReturnsError(msig.ReturnType)
			}
		}
	}
	return false
}

// lookupTypeAlias resolves a type-alias name to its underlying TypeExpr.
// Prefers bound.TypeAliases (the typechecker's canonical per-file table)
// when available; falls back to the codegen-side g.typeAliases for
// legacy paths that bypass the bound side-map.
func (g *Generator) lookupTypeAlias(name string) (parser.TypeExpr, bool) {
	if g.bound != nil && g.bound.TypeAliases != nil {
		if t, ok := g.bound.TypeAliases[name]; ok {
			return t, true
		}
	}
	if t, ok := g.typeAliases[name]; ok {
		return t, true
	}
	return nil, false
}

// SetSubpackageStructs registers class declarations from a subpackage for method lookups.
func (g *Generator) SetSubpackageStructs(pkg string, classes map[string]*parser.ClassDecl) {
	if g.subpkgStructs == nil {
		g.subpkgStructs = make(map[string]map[string]*parser.ClassDecl)
	}
	g.subpkgStructs[pkg] = classes
}

// SetSubpackageTypeAliases registers `type Name = ...` aliases from a
// subpackage so cross-package callers can peel them via
// resolveFuncTypeExpr. Without this, a `Factory` param declared in
// lib but resolved during main's emit reads as an opaque SimpleType
// and the lambda-target inference for method args silently drops.
func (g *Generator) SetSubpackageTypeAliases(pkg string, aliases map[string]parser.TypeExpr) {
	if g.subpkgTypeAliases == nil {
		g.subpkgTypeAliases = make(map[string]map[string]parser.TypeExpr)
	}
	g.subpkgTypeAliases[pkg] = aliases
}

// SetSubpackageDataFields registers data class field info from a subpackage.
func (g *Generator) SetSubpackageDataFields(pkg string, fields map[string][]*parser.FieldDecl) {
	if g.subpkgDataFields == nil {
		g.subpkgDataFields = make(map[string]map[string][]*parser.FieldDecl)
	}
	g.subpkgDataFields[pkg] = fields
}

// RegisterInterface allows external callers to register interface names.
func (g *Generator) RegisterInterface(name string) {
	g.interfaces[name] = true
}

// --- Declaration scanning ----------------------------------------------------

// collectDecls scans declarations to build lookup tables for types,
// constructors, error functions, type aliases, and pub visibility.
func (g *Generator) collectDecls(decls []parser.TopLevelDecl) {
	for _, d := range decls {
		switch decl := d.(type) {
		case *parser.InterfaceDecl:
			g.interfaces[decl.Name] = true
			// Interface methods — track pub status
			for _, m := range decl.Methods {
				g.pubNames[decl.Name+"."+m.Name] = m.IsPub
			}
		case *parser.DataClassDecl:
			g.dataClasses[decl.Name] = true
			if g.dataClassDecls == nil {
				g.dataClassDecls = make(map[string]*parser.DataClassDecl)
			}
			g.dataClassDecls[decl.Name] = decl
			g.funcSigs["New"+decl.Name] = fieldDeclsToParams(decl.Params)
			if g.localDataFields == nil {
				g.localDataFields = make(map[string][]*parser.FieldDecl)
			}
			g.localDataFields[decl.Name] = decl.Params
			// Data class fields — track pub status
			for _, f := range decl.Params {
				g.pubNames[decl.Name+"."+f.Name] = f.IsPub
			}
		case *parser.ClassDecl:
			g.structs[decl.Name] = decl
			if decl.IsSealed {
				g.interfaces[decl.Name] = true
				for _, v := range decl.Variants {
					g.dataClasses[v.Name] = true
					if g.dataClassDecls == nil {
						g.dataClassDecls = make(map[string]*parser.DataClassDecl)
					}
					g.dataClassDecls[v.Name] = v
					g.funcSigs["New"+v.Name] = fieldDeclsToParams(v.Params)
					if g.localDataFields == nil {
						g.localDataFields = make(map[string][]*parser.FieldDecl)
					}
					g.localDataFields[v.Name] = v.Params
					for _, f := range v.Params {
						g.pubNames[v.Name+"."+f.Name] = f.IsPub
					}
				}
			}
			if decl.Ctor != nil {
				g.funcSigs["New"+decl.Name] = decl.Ctor.Params
			} else if len(decl.Ctors) > 0 {
				g.funcSigs["New"+decl.Name] = decl.Ctors[0].Params
			}
			// Class methods and fields — track pub status. Thrower-ness
			// is purely syntactic now: explicit `error` in the declared
			// return type. Constructor throwers must use a factory fn
			// with an explicit `(T, error)` return — `init` blocks no
			// longer auto-widen.
			for _, m := range decl.Methods {
				g.pubNames[decl.Name+"."+m.Name] = m.IsPub
				// Method thrower / optional / pointer-shape lookups all
				// flow through bound.Sigs.MethodSigs (see methodReturnsError
				// and callReturnIsPointer). No codegen-side method-level
				// tracking populated here.
			}
			for _, f := range decl.Fields {
				g.pubNames[decl.Name+"."+f.Name] = f.IsPub
			}
		case *parser.TypeAliasDecl:
			g.typeAliases[decl.Name] = decl.Type
		case *parser.ConstDecl:
			g.pubNames[decl.Name] = decl.IsPub
		case *parser.FnDecl:
			g.pubNames[decl.Name] = decl.IsPub
			g.funcSigs[decl.Name] = decl.Params
			// Thrower / optional return classification flows through
			// bound.Sigs.FnSigs (see fnReturnsError / callReturnIsPointer).
			// funcReturnTypes is retained as a per-file Go-formatted-type
			// cache for inferExprType — that consumer wants the
			// pre-formatted Go string and the side-effect of registering
			// imports happens via formatType during collectDecls, which
			// is the right time for those side-effects.
			if decl.ReturnType != nil {
				g.funcReturnTypes[decl.Name] = g.formatType(decl.ReturnType)
			}
		}
	}
}

// fieldDeclsToParams converts FieldDecl slice to ParamDecl slice for funcSigs.
func fieldDeclsToParams(fields []*parser.FieldDecl) []*parser.ParamDecl {
	var params []*parser.ParamDecl
	for _, f := range fields {
		params = append(params, &parser.ParamDecl{
			Name:    f.Name,
			Type:    f.Type,
			Default: f.Default,
		})
	}
	return params
}

// --- Error detection ---------------------------------------------------------
//
// Thrower-ness is now purely syntactic: a function is a thrower iff its
// declared return type contains `error` (bare or as the trailing element
// of a TupleType). Thrower classification reads from bound.Sigs.FnSigs
// during collectDecls; no body inspection, no cross-package fixed-point.

// exprContainsNestedThrowerCall reports whether the expression tree
// contains a thrower call somewhere strictly inside a sub-expression
// position. The top-level expression itself is excluded — when it IS
// a thrower call, the statement-level codegen path already handles
// it (emitErrorPropagatingVar for var-stmt, pass-through return for
// return-stmt). Drives the hoist trigger sites so that `return
// f(g(x))` where `g` throws auto-hoists `g` to a temp before the
// outer call is emitted, with no `?` markup needed.
func (g *Generator) exprContainsNestedThrowerCall(e parser.Expr) bool {
	return g.containsThrowerCallRec(e, true)
}

func (g *Generator) containsThrowerCallRec(e parser.Expr, atTopLevel bool) bool {
	if e == nil {
		return false
	}
	switch ex := e.(type) {
	case *parser.CallExpr:
		if !atTopLevel && g.callReturnsError(ex) && !g.callIsVoidThrower(ex) {
			return true
		}
		if g.containsThrowerCallRec(ex.Callee, false) {
			return true
		}
		for _, a := range ex.Args {
			if g.containsThrowerCallRec(a, false) {
				return true
			}
		}
		for _, na := range ex.NamedArgs {
			if g.containsThrowerCallRec(na.Value, false) {
				return true
			}
		}
		return false
	case *parser.BinaryExpr:
		return g.containsThrowerCallRec(ex.Left, false) ||
			g.containsThrowerCallRec(ex.Right, false)
	case *parser.UnaryExpr:
		return g.containsThrowerCallRec(ex.Operand, false)
	case *parser.SelectorExpr:
		return g.containsThrowerCallRec(ex.Object, false)
	case *parser.IndexExpr:
		return g.containsThrowerCallRec(ex.Object, false) ||
			g.containsThrowerCallRec(ex.Index, false)
	case *parser.SliceExpr:
		return g.containsThrowerCallRec(ex.Object, false) ||
			g.containsThrowerCallRec(ex.Low, false) ||
			g.containsThrowerCallRec(ex.High, false)
	case *parser.SpreadExpr:
		return g.containsThrowerCallRec(ex.Expr, false)
	case *parser.RangeExpr:
		return g.containsThrowerCallRec(ex.Start, false) ||
			g.containsThrowerCallRec(ex.End, false)
	case *parser.SafeNavExpr:
		if g.containsThrowerCallRec(ex.Object, false) {
			return true
		}
		if ex.Call != nil {
			for _, a := range ex.Call.Args {
				if g.containsThrowerCallRec(a, false) {
					return true
				}
			}
		}
		return false
	case *parser.TypeAssertExpr:
		return g.containsThrowerCallRec(ex.Object, false)
	}
	return false
}

// exprContainsAsCast reports whether an expression tree contains an
// `as` type cast. Drives hoisting — nested `as` lowers to a comma-ok
// temp + error guard above the surrounding statement. The `is`
// predicate form (IsCheck=true) is not failable and never matches.
func exprContainsAsCast(e parser.Expr) bool {
	if e == nil {
		return false
	}
	switch expr := e.(type) {
	case *parser.TypeAssertExpr:
		if !expr.IsCheck {
			return true
		}
		return exprContainsAsCast(expr.Object)
	case *parser.CallExpr:
		if exprContainsAsCast(expr.Callee) {
			return true
		}
		for _, a := range expr.Args {
			if exprContainsAsCast(a) {
				return true
			}
		}
		for _, na := range expr.NamedArgs {
			if exprContainsAsCast(na.Value) {
				return true
			}
		}
		return false
	case *parser.BinaryExpr:
		return exprContainsAsCast(expr.Left) || exprContainsAsCast(expr.Right)
	case *parser.UnaryExpr:
		return exprContainsAsCast(expr.Operand)
	case *parser.SelectorExpr:
		return exprContainsAsCast(expr.Object)
	case *parser.SafeNavExpr:
		if exprContainsAsCast(expr.Object) {
			return true
		}
		if expr.Call != nil {
			for _, a := range expr.Call.Args {
				if exprContainsAsCast(a) {
					return true
				}
			}
		}
		return false
	case *parser.IndexExpr:
		return exprContainsAsCast(expr.Object) || exprContainsAsCast(expr.Index)
	case *parser.SliceExpr:
		return exprContainsAsCast(expr.Object) || exprContainsAsCast(expr.Low) || exprContainsAsCast(expr.High)
	case *parser.SpreadExpr:
		return exprContainsAsCast(expr.Expr)
	case *parser.RangeExpr:
		return exprContainsAsCast(expr.Start) || exprContainsAsCast(expr.End)
	}
	return false
}

// --- Code generation entry points --------------------------------------------

// Generate produces a single .go source file from a Zinc program.
func (g *Generator) Generate(prog *parser.Program, className string) string {
	g.buf.Reset()
	g.indent = 0
	g.className = className
	g.imports = make(map[string]bool)
	// Preserve funcSigs pre-populated by SetSiblingExports (sibling function awareness).
	if g.funcSigs == nil {
		g.funcSigs = make(map[string][]*parser.ParamDecl)
	}
	g.varTypes = make(map[string]string)
	// Preserve dataClasses, interfaces, structs, and pubNames
	// pre-populated by SetSiblingExports (sibling file awareness).
	if g.dataClasses == nil {
		g.dataClasses = make(map[string]bool)
	}
	g.typeImports = make(map[string]string)
	if g.pubNames == nil {
		g.pubNames = make(map[string]bool)
	}
	g.importGoAliases = make(map[string]string)
	g.collectDecls(prog.Decls)

	// Register user imports for resolution — but don't add to g.imports yet.
	// The codegen will call needImport() when it actually references a package,
	// so only used imports appear in the output.
	for _, imp := range prog.Imports {
		parts := strings.Split(imp.Path, ".")
		lastSeg := parts[len(parts)-1]

		// Determine the local alias name for this import
		// If "import X as Y" was used, alias is Y; otherwise it's the last path segment
		localName := lastSeg
		if imp.Alias != "" {
			localName = imp.Alias
		}

		// Check import aliases from zinc.toml [deps] section.
		// Handles both: "import viper" (direct alias) and "import stdlib.config" (prefix alias)
		if g.importAliases != nil {
			// Direct alias: import viper → viper = "github.com/spf13/viper"
			if modulePath, ok := g.importAliases[imp.Path]; ok {
				g.importMap[localName] = modulePath
				// Go package name is last segment of the module path
				goPkgName := modulePath[strings.LastIndex(modulePath, "/")+1:]
				if localName != goPkgName {
					g.importGoAliases[modulePath] = localName
				}
				continue
			}
			// Prefix alias: import stdlib.config → stdlib = "github.com/ZincScale/zinc-stdlib"
			if len(parts) >= 2 {
				if modulePath, ok := g.importAliases[parts[0]]; ok {
					subPath := strings.Join(parts[1:], "/")
					goPath := modulePath + "/" + subPath
					g.importMap[localName] = goPath
					goPkgName := parts[len(parts)-1]
					if localName != goPkgName {
						g.importGoAliases[goPath] = localName
					}
					continue
				}
			}
		}

		// Check if this is a zinc subpackage import.
		subpkgPath := strings.ReplaceAll(imp.Path, ".", "/")
		if g.zincSubpackages[subpkgPath] {
			goPath := subpkgPath
			if g.moduleName != "" {
				goPath = g.moduleName + "/" + subpkgPath
			}
			g.importMap[localName] = goPath
			// Register Go import alias if localName differs from directory name
			dirName := parts[len(parts)-1]
			if localName != dirName {
				g.importGoAliases[goPath] = localName
			}
			continue
		}

		if len(parts) >= 2 && len(lastSeg) > 0 && lastSeg[0] >= 'A' && lastSeg[0] <= 'Z' {
			// Type import: sync.Mutex → register Mutex → sync.Mutex
			pkgParts := parts[:len(parts)-1]
			goPath := strings.Join(pkgParts, "/")
			goPkg := pkgParts[len(pkgParts)-1]
			typeName := lastSeg
			g.typeImports[typeName] = goPkg + "." + typeName
			g.importMap[goPkg] = goPath
		} else {
			// Package import: net.http → import "net/http"
			goPath := strings.ReplaceAll(imp.Path, ".", "/")
			g.importMap[localName] = goPath
			// Register Go import alias if localName differs from last path segment
			goLastSeg := parts[len(parts)-1]
			if localName != goLastSeg {
				g.importGoAliases[goPath] = localName
			}
		}
	}

	// Build unqualified name resolution from subpackage exports.
	// Allows writing Processor instead of lib.Processor.
	g.buildUnqualifiedNames(prog)

	// Shadow pre-scan: find all top-level var names + nested var names
	// inside function/method bodies. Any name matching an imported
	// package alias triggers a Go-side auto-alias for that import, so
	// references to the import don't get masked by the local var when
	// Go resolves the name. Lets the user write `var api = ...` plus
	// `fabric.api.X(...)` (or even `ApiHandler(fab)`) without a clash.
	g.registerShadowAliases(prog)

	// First pass: generate body into a separate buffer to collect imports
	bodyGen := *g
	bodyGen.buf.Reset()

	for _, d := range prog.Decls {
		bodyGen.emitDecl(d)
		bodyGen.writeln("")
	}

	// Script-mode statements → func main()
	hasExplicitMain := false
	for _, d := range prog.Decls {
		if fn, ok := d.(*parser.FnDecl); ok && fn.Name == "main" {
			hasExplicitMain = true
			break
		}
	}
	if len(prog.Stmts) > 0 && !hasExplicitMain {
		bodyGen.writeln("func main() {")
		bodyGen.indent++
		bodyGen.inferChannelTypes(&parser.BlockStmt{Stmts: prog.Stmts})
		for _, s := range prog.Stmts {
			if line := stmtLine(s); bodyGen.sourceFile != "" && line > 0 {
				// Go's //line directive must start at column 0 —
				// leading whitespace disables it. Write raw to skip
				// writeln's indent tabs.
				fmt.Fprintf(&bodyGen.buf, "//line %s:%d\n", bodyGen.sourceFile, line)
			}
			bodyGen.emitStmt(s)
		}
		bodyGen.indent--
		bodyGen.writeln("}")
	}

	body := bodyGen.buf.String()
	g.imports = bodyGen.imports
	g.importGoAliases = bodyGen.importGoAliases
	// Propagate any compile errors recorded during body emit back to the
	// outer Generator so compileFile / compileMultiFile can surface them.
	// bodyGen is a value copy (see `bodyGen := *g` above); the outer g
	// wouldn't otherwise see errors appended to the copy's slice.
	g.compileErrors = append(g.compileErrors, bodyGen.compileErrors...)
	g.compileWarnings = append(g.compileWarnings, bodyGen.compileWarnings...)
	// Same propagation for the auto-address-take helper flag — set
	// during body emission, consumed by the preamble below.
	if bodyGen.needsPtrHelper {
		g.needsPtrHelper = true
	}

	// Write final output: package + imports + body
	pkgName := g.packageName
	if pkgName == "" {
		pkgName = "main"
	}
	g.writeln("package %s", pkgName)
	g.writeln("")

	if len(g.imports) > 0 {
		g.writeln("import (")
		g.indent++
		for pkg := range g.imports {
			if alias, ok := g.importGoAliases[pkg]; ok {
				g.writeln("%s %q", alias, pkg)
			} else {
				g.writeln("%q", pkg)
			}
		}
		g.indent--
		g.writeln(")")
		g.writeln("")
	}

	// Emit the auto-address-take helper if any wrapAsPointer call needed
	// it. Only generated when used so files without nullable assignments
	// stay clean.
	if g.needsPtrHelper {
		g.writeln("func _zincPtr[T any](v T) *T { return &v }")
		g.writeln("")
	}

	g.buf.WriteString(body)
	return g.buf.String()
}

// GenerateFiles produces separate .go files per type + a main.go for functions/script.
func (g *Generator) GenerateFiles(prog *parser.Program, className string) []OutputFile {
	content := g.Generate(prog, className)
	// Natural filename mapping. *_test.zn files go to *_test.go (picked up by
	// `go test`, skipped by `go build`). Non-test files are excluded from the
	// regular build pipeline upstream via collect*ZnFiles — so if we see an
	// _test suffix here, the caller intends a test build.
	return []OutputFile{{Name: strings.ToLower(className) + ".go", Content: content}}
}

// --- Declaration dispatch ----------------------------------------------------

func (g *Generator) emitDecl(d parser.TopLevelDecl) {
	switch decl := d.(type) {
	case *parser.FnDecl:
		g.emitFnDecl(decl)
	case *parser.ClassDecl:
		if decl.IsSealed {
			g.emitSealedDecl(decl)
		} else {
			g.emitClassDecl(decl)
		}
	case *parser.DataClassDecl:
		g.emitDataClassDecl(decl)
	case *parser.EnumDecl:
		g.emitEnumDecl(decl)
	case *parser.InterfaceDecl:
		g.emitInterfaceDecl(decl)
	case *parser.ConstDecl:
		g.emitConstDecl(decl)
	case *parser.TypeAliasDecl:
		g.writeln("type %s = %s", decl.Name, g.formatType(decl.Type))
	case *parser.TestDecl:
		g.emitTestDecl(decl)
	}
}

// emitTestDecl generates a Go test function from a `test "name" { body }` block.
// Name is munged to a legal Go identifier prefixed with "Test". The body runs
// with `t *testing.T` in scope so stdlib/testing helpers can signal failures.
func (g *Generator) emitTestDecl(d *parser.TestDecl) {
	g.needImport("testing")
	if g.sourceFile != "" && d.Line > 0 {
		g.writeln("//line %s:%d", g.sourceFile, d.Line)
	}
	goName := testGoName(d.Name)
	g.writeln("func %s(t *testing.T) {", goName)
	g.indent++
	g.emitBlock(d.Body)
	g.indent--
	g.writeln("}")
}

// testGoName converts a free-form test name into a legal Go identifier
// prefixed with "Test". Keeps alphanumeric characters, title-cases word
// breaks, drops everything else. Empty name → "TestUnnamed".
//
//	"update-attribute rejects missing key" → "TestUpdateAttributeRejectsMissingKey"
//	"parses valid json"                    → "TestParsesValidJson"
//	"x == y"                               → "TestXY"
func testGoName(raw string) string {
	var out strings.Builder
	out.WriteString("Test")
	wantUpper := true
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r)
			wantUpper = false
		case r >= 'a' && r <= 'z':
			if wantUpper {
				out.WriteRune(r - ('a' - 'A'))
				wantUpper = false
			} else {
				out.WriteRune(r)
			}
		case r >= '0' && r <= '9':
			out.WriteRune(r)
			wantUpper = false
		default:
			wantUpper = true
		}
	}
	if out.Len() == len("Test") {
		out.WriteString("Unnamed")
	}
	return out.String()
}

// --- Output helpers ----------------------------------------------------------

func (g *Generator) writeln(format string, args ...interface{}) {
	g.buf.WriteString(strings.Repeat("\t", g.indent))
	fmt.Fprintf(&g.buf, format, args...)
	g.buf.WriteString("\n")
}

func (g *Generator) write(format string, args ...interface{}) {
	g.buf.WriteString(strings.Repeat("\t", g.indent))
	fmt.Fprintf(&g.buf, format, args...)
}

// Name helpers (exportName, goName, isPub, etc.) are in codegen_resolve.go.
