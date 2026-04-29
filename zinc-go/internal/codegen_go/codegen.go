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
	"go/types"
	"strings"

	"zinc-go/internal/parser"
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
	errorFuncs            map[string]bool   // functions that can return errors
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

	// pendingLambdaTarget carries the declared Fn<...> target type from
	// the immediate emit site (currently VarStmt LHS) into
	// formatLambdaExpr, so the lambda's Go return type is driven from
	// the target slot instead of falling back to interface{} when the
	// body contains expressions inferLambdaReturnType can't resolve
	// statically (e.g. method calls, field accesses on `this`).
	// Cleared on entry to formatLambdaExpr so nested lambdas don't
	// inherit the outer hint.
	pendingLambdaTarget *parser.FuncTypeExpr

	// Variable type tracking
	varTypes            map[string]string       // variable name → element type
	varTypeExprs        map[string]parser.TypeExpr // variable name → original AST type (for generics)
	varGoTypes          map[string]types.Type   // variable name → Go type (from stdlib call returns)
	ptrVars             map[string]bool         // variables that are pointers (*T from T? returns)
	funcReturnsOptional map[string]bool       // functions that return T? (optional)
	funcReturnTypes     map[string]string     // function name → Go return type string
	renamedVars         map[string]string     // original name → safe name (for builtin shadows)
	varStructTypes      map[string]string     // variable name → struct type name
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
	importAliases    map[string]string // import alias → Go module path (e.g. "stdlib" → "github.com/ZincScale/zinc-stdlib")
	importGoAliases  map[string]string // Go import path → local alias (when alias differs from package name)

	// Unqualified import resolution: bare name → package + kind
	// Built from subpkgExports after import processing. Allows writing
	// Processor instead of lib.Processor when import lib is declared.
	unqualifiedNames      map[string]unqualifiedEntry
	unqualifiedCollisions map[string][]string // name → list of packages that export it

	// Compile-time errors accumulated during codegen (e.g., non-exhaustive match).
	// Checked by the caller after GenerateFiles returns.
	compileErrors []string

	// needsPtrHelper marks that the generated file references _zincPtr,
	// the generic helper that boxes a value into a pointer. Required for
	// `String? foo = "hi"` and similar — Go disallows `&"hi"` directly.
	needsPtrHelper bool
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

// Name resolution, type formatting, and visibility helpers are in codegen_resolve.go.

// New creates a new Go code generator.
func New() *Generator {
	return &Generator{
		imports:             make(map[string]bool),
		interfaces:          make(map[string]bool),
		structs:             make(map[string]*parser.ClassDecl),
		errorFuncs:          make(map[string]bool),
		funcSigs:            make(map[string][]*parser.ParamDecl),
		varTypes:            make(map[string]string),
		varTypeExprs:        make(map[string]parser.TypeExpr),
		varGoTypes:          make(map[string]types.Type),
		ptrVars:             make(map[string]bool),
		funcReturnsOptional: make(map[string]bool),
		funcReturnTypes:     make(map[string]string),
		renamedVars:         make(map[string]string),
		varStructTypes:      make(map[string]string),
		dataClasses:         make(map[string]bool),
		typeAliases:         make(map[string]parser.TypeExpr),
		goResolver:          NewGoTypeResolver(),
		importMap:           make(map[string]string),
		typeImports:         make(map[string]string),
		pubNames:            make(map[string]bool),
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

// SetImportAliases sets the import alias → module path mappings from zinc.toml [imports].
func (g *Generator) SetImportAliases(aliases map[string]string) {
	g.importAliases = aliases
}

// SetSiblingExports registers names from sibling files in the same package.
// These are types, functions, etc. declared in other .zn files in the same directory.
// Go handles cross-file visibility natively within a package, but the codegen
// needs this for constructor name resolution and export capitalization decisions.
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
			// Register sibling functions so cross-file calls get exported names
			if g.funcSigs == nil {
				g.funcSigs = make(map[string][]*parser.ParamDecl)
			}
			if _, exists := g.funcSigs[name]; !exists {
				g.funcSigs[name] = nil // mark as known function (no param info)
			}
		}
		g.pubNames[name] = true // siblings in same package are always visible
	}
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

// SetSubpackageStructs registers class declarations from a subpackage for method lookups.
func (g *Generator) SetSubpackageStructs(pkg string, classes map[string]*parser.ClassDecl) {
	if g.subpkgStructs == nil {
		g.subpkgStructs = make(map[string]map[string]*parser.ClassDecl)
	}
	g.subpkgStructs[pkg] = classes
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
				if g.blockCanReturnError(decl.Ctor.Body) {
					g.errorFuncs["New"+decl.Name] = true
				}
			} else if len(decl.Ctors) > 0 {
				g.funcSigs["New"+decl.Name] = decl.Ctors[0].Params
				if g.blockCanReturnError(decl.Ctors[0].Body) {
					g.errorFuncs["New"+decl.Name] = true
				}
			}
			// Class methods and fields — track pub status
			for _, m := range decl.Methods {
				g.pubNames[decl.Name+"."+m.Name] = m.IsPub
				key := decl.Name + "." + m.Name
				if g.blockCanReturnError(m.Body) {
					g.errorFuncs[key] = true
				}
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
			if g.blockCanReturnError(decl.Body) {
				g.errorFuncs[decl.Name] = true
			}
			if _, ok := decl.ReturnType.(*parser.OptionalType); ok {
				g.funcReturnsOptional[decl.Name] = true
			}
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
// The primary thrower-detection check is g.blockCanReturnError in
// codegen_stmts.go — it covers `?` / `or { }` triggers plus calls to
// known throwers. errorFuncs is populated by a fixed-point pass in
// collectDecls: each iteration re-scans every fn/method/ctor body, and
// we repeat until no new entries are added. This closes the call-graph
// so `fn X() { thrower() }` correctly marks X as a thrower too.

// propagateThrowerFixedPoint iteratively extends g.errorFuncs with any
// function whose body calls an already-known thrower. Seeds from the
// direct triggers populated during the first walk of collectDecls.
func (g *Generator) propagateThrowerFixedPoint(prog *parser.Program) {
	for {
		changed := false
		for _, d := range prog.Decls {
			switch decl := d.(type) {
			case *parser.FnDecl:
				if !g.errorFuncs[decl.Name] && g.blockCanReturnError(decl.Body) {
					g.errorFuncs[decl.Name] = true
					changed = true
				}
			case *parser.ClassDecl:
				if decl.Ctor != nil {
					key := "New" + decl.Name
					if !g.errorFuncs[key] && g.blockCanReturnError(decl.Ctor.Body) {
						g.errorFuncs[key] = true
						changed = true
					}
				} else if len(decl.Ctors) > 0 {
					key := "New" + decl.Name
					if !g.errorFuncs[key] && g.blockCanReturnError(decl.Ctors[0].Body) {
						g.errorFuncs[key] = true
						changed = true
					}
				}
				for _, m := range decl.Methods {
					key := decl.Name + "." + m.Name
					if !g.errorFuncs[key] && g.blockCanReturnError(m.Body) {
						g.errorFuncs[key] = true
						changed = true
					}
				}
			}
		}
		if !changed {
			return
		}
	}
}

// exprContainsPropagate reports whether an expression tree contains a
// PropagateExpr (postfix `?`). Used by inference — any `?` in a function
// body promotes that function to a thrower. Lambdas are a separate scope,
// so we do *not* descend into LambdaExpr bodies.
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
// `as` type cast. Like callReturnsError but for the failable cast —
// drives both hoisting (to lower nested `as` to a comma-ok temp) and
// the thrower-inference fixed-point (so functions using `as` widen
// to (T, error)). The `is` predicate form (IsCheck=true) is not
// failable and never matches here.
//
// Distinct from exprContainsPropagate: `?` always widens (or-handler
// can't override), but `as` can be consumed by an or-handler at the
// statement level — same rule as a thrower call. So callers that
// implement the always-widen rule consult exprContainsPropagate; the
// thrower-detection path consults this one alongside callReturnsError.
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
	case *parser.PropagateExpr:
		return exprContainsAsCast(expr.Inner)
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

func exprContainsPropagate(e parser.Expr) bool {
	if e == nil {
		return false
	}
	switch expr := e.(type) {
	case *parser.PropagateExpr:
		return true
	case *parser.CallExpr:
		if exprContainsPropagate(expr.Callee) {
			return true
		}
		for _, a := range expr.Args {
			if exprContainsPropagate(a) {
				return true
			}
		}
		for _, na := range expr.NamedArgs {
			if exprContainsPropagate(na.Value) {
				return true
			}
		}
		return false
	case *parser.BinaryExpr:
		return exprContainsPropagate(expr.Left) || exprContainsPropagate(expr.Right)
	case *parser.UnaryExpr:
		return exprContainsPropagate(expr.Operand)
	case *parser.SelectorExpr:
		return exprContainsPropagate(expr.Object)
	case *parser.SafeNavExpr:
		if exprContainsPropagate(expr.Object) {
			return true
		}
		if expr.Call != nil {
			for _, a := range expr.Call.Args {
				if exprContainsPropagate(a) {
					return true
				}
			}
		}
		return false
	case *parser.IndexExpr:
		return exprContainsPropagate(expr.Object) || exprContainsPropagate(expr.Index)
	case *parser.SliceExpr:
		return exprContainsPropagate(expr.Object) || exprContainsPropagate(expr.Low) || exprContainsPropagate(expr.High)
	case *parser.TypeAssertExpr:
		return exprContainsPropagate(expr.Object)
	case *parser.SpreadExpr:
		return exprContainsPropagate(expr.Expr)
	case *parser.RangeExpr:
		return exprContainsPropagate(expr.Start) || exprContainsPropagate(expr.End)
	case *parser.IfExpr:
		return exprContainsPropagate(expr.Cond) || exprContainsPropagate(expr.Then) || exprContainsPropagate(expr.Else)
	case *parser.MatchExpr:
		if exprContainsPropagate(expr.Subject) {
			return true
		}
		for _, c := range expr.Cases {
			if exprContainsPropagate(c.Value) {
				return true
			}
		}
		return false
	case *parser.LambdaExpr:
		// Separate scope — `?` inside a lambda makes *the lambda* a
		// thrower, not the enclosing function. Don't descend.
		return false
	default:
		return false
	}
}

// --- Code generation entry points --------------------------------------------

// Generate produces a single .go source file from a Zinc program.
func (g *Generator) Generate(prog *parser.Program, className string) string {
	g.buf.Reset()
	g.indent = 0
	g.className = className
	g.imports = make(map[string]bool)
	g.errorFuncs = make(map[string]bool)
	// Preserve funcSigs pre-populated by SetSiblingExports (sibling function awareness).
	if g.funcSigs == nil {
		g.funcSigs = make(map[string][]*parser.ParamDecl)
	}
	g.varTypes = make(map[string]string)
	g.varTypeExprs = make(map[string]parser.TypeExpr)
	g.varGoTypes = make(map[string]types.Type)
	g.varStructTypes = make(map[string]string)
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
	// Close the call graph: a function that only calls throwers is
	// itself a thrower. The initial collectDecls walk seeds errorFuncs
	// from direct triggers (`?`, `or { }`); this fixed-point extends it
	// transitively.
	g.propagateThrowerFixedPoint(prog)

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

		// Check import aliases from zinc.toml [imports] section.
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
