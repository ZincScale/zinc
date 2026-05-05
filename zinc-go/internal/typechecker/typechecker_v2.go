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

import (
	"fmt"
	"go/types"
	"strings"

	"zinc-go/internal/parser"
)

// V2Error represents a type error found during v2 type checking.
type V2Error struct {
	Line    int
	Message string
}

func (e V2Error) String() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Message)
	}
	return e.Message
}

// V2Type represents a resolved type in the v2 type system.
//
// `GoType` is set when the V2Type bridges to a Go-imported type (FFI). When
// non-nil, it carries the precise Go type (`*ocf.Decoder`, `[]byte`, etc.)
// that codegen needs for method-set lookups and pointer-vs-value emission.
// Phase 3.5 introduced this; the `varGoTypes`-style codegen tables are
// migrating to consume it via the BoundProgram's NodeTypes side-map.
type V2Type struct {
	Name     string     // "int", "String", "List", "Map", "null", "any"
	Args     []V2Type   // generic args: list[int] → Args=[int]
	Nullable bool       // Optional[T]
	GoType   types.Type // optional Go-resolved type (FFI bridge)
	// TypeExpr (Phase 3.7.2): the original parser AST node this V2Type
	// was derived from, when known. Carried forward through inference
	// so codegen can walk into Fn types and generic args without a
	// separate TypeExprs side-map. nil when the type was synthesized
	// (literals, builtins) or inference has no AST shadow.
	TypeExpr parser.TypeExpr
}

func (t V2Type) String() string {
	if len(t.Args) > 0 {
		s := t.Name + "["
		for i, a := range t.Args {
			if i > 0 {
				s += ", "
			}
			s += a.String()
		}
		return s + "]"
	}
	return t.Name
}

var (
	typeInt     = V2Type{Name: "int"}
	typeDouble  = V2Type{Name: "double"}
	typeStr     = V2Type{Name: "String"}
	typeBool    = V2Type{Name: "bool"}
	typeNull    = V2Type{Name: "null"}
	typeAny     = V2Type{Name: "any"}
)

// V2Scope tracks variable types in a scope.
type V2Scope struct {
	parent *V2Scope
	vars   map[string]V2Type
}

func newV2Scope(parent *V2Scope) *V2Scope {
	return &V2Scope{parent: parent, vars: make(map[string]V2Type)}
}

func (s *V2Scope) set(name string, t V2Type) {
	s.vars[name] = t
}

func (s *V2Scope) lookup(name string) (V2Type, bool) {
	if t, ok := s.vars[name]; ok {
		return t, true
	}
	if s.parent != nil {
		return s.parent.lookup(name)
	}
	return typeAny, false
}

// V2FnSig stores a function's parameter and return types.
type V2FnSig struct {
	Params     []V2Type
	ParamNames []string
	ReturnType V2Type
	Variadic   bool // true if last param is `T... name`

	// TypeParams (3.6.1): generic type parameter names declared on the fn.
	// `T max<T>(T a, T b)` → ["T"]. Used to recognize a Param/Return type
	// of name "T" as a type-parameter reference during call-site checking.
	TypeParams []string
	// TypeParamBounds (3.6.1): per-param bound list. Each bound is a
	// type name that the inferred concrete must satisfy.
	TypeParamBounds map[string][]string
}

// GoFFIResolver provides Go-imported-package type information to the
// typechecker. The codegen's GoTypeResolver implements it. Decoupled so
// the typechecker doesn't import codegen_go (would be a cycle).
type GoFFIResolver interface {
	// FuncReturnTypeAt returns the i-th return slot's Go type for a
	// package-level function. nil if the function or slot doesn't exist.
	FuncReturnTypeAt(pkgPath, funcName string, idx int) types.Type
	// IsStruct reports whether `name` is a struct in `pkgPath`.
	IsStruct(pkgPath, name string) bool
	// MethodReturnTypeAt returns the i-th return slot's Go type for
	// `methodName` on the Go-resolved receiver type. nil when the method
	// doesn't exist on either the value or *value method-set.
	MethodReturnTypeAt(recv types.Type, methodName string, idx int) types.Type
}

// V2Checker performs type checking on a v2 AST.
type V2Checker struct {
	errors       []V2Error
	scope        *V2Scope
	fnReturnType *V2Type                       // current function's return type
	fnSigs       map[string]V2FnSig            // function signatures for call checking
	methodSigs   map[string]map[string]V2FnSig // type → method → signature
	parentTypes  map[string][]string           // class → parent types (interfaces, superclasses)
	classFields  map[string]map[string]V2Type  // class → field name → field type (3.7.2: SelectorExpr inference)
	classNames   map[string]bool               // registered class/data/interface/enum names (3.7.2: SelectorExpr `pkg.Class` inference)
	currentClass string                        // enclosing class name during method/ctor checking, "" outside (3.7.2: `this` typing)
	inLoop       bool                          // tracking if inside a loop for break/continue

	// nodeTypes side-map keyed by parser.Expr pointer identity. inferType
	// populates this during the type walk. Codegen consumes via the
	// BoundProgram (Phase 3.5+).
	nodeTypes map[parser.Expr]V2Type

	// FFI plumbing — set by the driver when typecheck mode includes Go-pkg
	// awareness. importMap[alias] = Go package path; ffi resolves Go
	// signatures. Both nil = no FFI awareness (calls into pkg.X return
	// typeAny). When set, pkg.Func(...) returns gain GoType annotations.
	importMap map[string]string
	ffi       GoFFIResolver
}

// CheckV2 runs the v2 type checker on a parsed program.
// Returns errors found. Empty slice = all good.
func CheckV2(prog *parser.Program) []V2Error {
	return CheckV2WithContext(prog, nil)
}

// CheckV2WithContext runs the type checker with pre-populated cross-file signatures.
// externalSigs contains function/method signatures from other files in the project.
//
// Returns errors only — the side-map of node types lives on the checker itself.
// Use CheckV2WithContextAndNodes to capture both errors and the node-type map
// in one call (Phase 3.5+ driver path).
func CheckV2WithContext(prog *parser.Program, externalSigs *CollectedSigs) []V2Error {
	c := newCheckerForProgram(externalSigs)
	checkProgram(c, prog)
	return c.errors
}

// CheckV2WithContextAndNodes runs the type checker like CheckV2WithContext but
// returns the populated NodeTypes side-map along with any errors. Used by the
// Phase 3.5+ pipeline driver to attach types to the BoundProgram.
//
// importMap and ffi are optional. When supplied, calls into Go-imported
// packages get GoType annotations on their result V2Types — codegen reads
// these to decide pointer-vs-value emission and method-set lookups.
func CheckV2WithContextAndNodes(prog *parser.Program, externalSigs *CollectedSigs,
	importMap map[string]string, ffi GoFFIResolver) (
	[]V2Error, map[parser.Expr]V2Type) {
	c := newCheckerForProgram(externalSigs)
	c.nodeTypes = make(map[parser.Expr]V2Type)
	c.importMap = importMap
	c.ffi = ffi
	checkProgram(c, prog)
	return c.errors, c.nodeTypes
}

func newCheckerForProgram(externalSigs *CollectedSigs) *V2Checker {
	c := &V2Checker{
		scope:       newV2Scope(nil),
		fnSigs:      make(map[string]V2FnSig),
		methodSigs:  make(map[string]map[string]V2FnSig),
		parentTypes: make(map[string][]string),
		classFields: make(map[string]map[string]V2Type),
		classNames:  make(map[string]bool),
	}

	// Pre-populate with cross-file signatures
	if externalSigs != nil {
		for k, v := range externalSigs.FnSigs {
			c.fnSigs[k] = v
		}
		for k, v := range externalSigs.MethodSigs {
			c.methodSigs[k] = v
		}
		for k, v := range externalSigs.ParentTypes {
			c.parentTypes[k] = v
		}
		// 3.7.2: register cross-pkg class names in scope so CallExpr
		// ctor inference fires for `Store(...)` from another file.
		for name := range externalSigs.ClassNames {
			c.scope.set(name, V2Type{Name: name})
			c.classNames[name] = true
		}
		for k, v := range externalSigs.ClassFields {
			c.classFields[k] = v
		}
	}
	return c
}

func checkProgram(c *V2Checker, prog *parser.Program) {
	// Register top-level declarations (overrides externals for this file)
	for _, d := range prog.Decls {
		c.registerDecl(d)
	}
	// Check declarations
	for _, d := range prog.Decls {
		c.checkDecl(d)
	}
	// Check top-level statements
	for _, s := range prog.Stmts {
		c.checkStmt(s)
	}
}

// CollectSignatures extracts function and interface method signatures from a program.
// Used in multi-file compilation to build cross-file context.
// MakeFnSigForMethod builds a V2FnSig from a parser MethodDecl. Used by
// the compiler driver to register cross-package method signatures into
// externalSigs.MethodSigs.
func MakeFnSigForMethod(m *parser.MethodDecl) V2FnSig {
	c := &V2Checker{
		scope:       newV2Scope(nil),
		classFields: make(map[string]map[string]V2Type),
		classNames:  make(map[string]bool),
	}
	retType := c.resolveTypeExpr(m.ReturnType)
	var paramTypes []V2Type
	var paramNames []string
	for _, p := range m.Params {
		paramTypes = append(paramTypes, c.resolveTypeExpr(p.Type))
		paramNames = append(paramNames, p.Name)
	}
	return V2FnSig{Params: paramTypes, ParamNames: paramNames, ReturnType: retType}
}

// MakeFnSigForFn builds a V2FnSig from a parser FnDecl. Companion to
// MakeFnSigForMethod for top-level functions registered cross-package
// via externalSigs.FnSigs.
func MakeFnSigForFn(d *parser.FnDecl) V2FnSig {
	c := &V2Checker{
		scope:       newV2Scope(nil),
		classFields: make(map[string]map[string]V2Type),
		classNames:  make(map[string]bool),
	}
	retType := c.resolveTypeExpr(d.ReturnType)
	var paramTypes []V2Type
	var paramNames []string
	for _, p := range d.Params {
		paramTypes = append(paramTypes, c.resolveTypeExpr(p.Type))
		paramNames = append(paramNames, p.Name)
	}
	return V2FnSig{Params: paramTypes, ParamNames: paramNames, ReturnType: retType, TypeParams: d.TypeParams}
}

// CollectedSigs holds both function and method signatures from a file.
type CollectedSigs struct {
	FnSigs      map[string]V2FnSig
	MethodSigs  map[string]map[string]V2FnSig // type → method → sig
	ParentTypes map[string][]string           // class → parent types
	// ClassNames (3.7.2): class/data class/enum/interface names — fed
	// into the receiving checker's scope so cross-pkg `Store(...)`
	// CallExpr inference can resolve to V2Type{Name:"Store"}.
	ClassNames map[string]bool
	// ClassFields (3.7.2): class → field-name → V2Type, mirroring the
	// per-checker classFields map. Lets cross-pkg field access resolve
	// (e.g. `s.someField` where s is a Store from another package).
	ClassFields map[string]map[string]V2Type
}

func CollectSignatures(prog *parser.Program) CollectedSigs {
	c := &V2Checker{
		scope:       newV2Scope(nil),
		fnSigs:      make(map[string]V2FnSig),
		methodSigs:  make(map[string]map[string]V2FnSig),
		parentTypes: make(map[string][]string),
		classFields: make(map[string]map[string]V2Type),
		classNames:  make(map[string]bool),
	}
	for _, d := range prog.Decls {
		c.registerDecl(d)
	}
	classNames := make(map[string]bool)
	for _, d := range prog.Decls {
		switch dd := d.(type) {
		case *parser.ClassDecl:
			classNames[dd.Name] = true
		case *parser.DataClassDecl:
			classNames[dd.Name] = true
		case *parser.InterfaceDecl:
			classNames[dd.Name] = true
		case *parser.EnumDecl:
			classNames[dd.Name] = true
		}
	}
	return CollectedSigs{
		FnSigs:      c.fnSigs,
		MethodSigs:  c.methodSigs,
		ParentTypes: c.parentTypes,
		ClassNames:  classNames,
		ClassFields: c.classFields,
	}
}

func (c *V2Checker) errorf(line int, format string, args ...any) {
	c.errors = append(c.errors, V2Error{
		Line:    line,
		Message: fmt.Sprintf(format, args...),
	})
}

// --- Registration (first pass) -----------------------------------------------

func (c *V2Checker) registerDecl(d parser.TopLevelDecl) {
	switch d := d.(type) {
	case *parser.FnDecl:
		retType := c.resolveTypeExpr(d.ReturnType)
		c.scope.set(d.Name, retType)
		// Store full signature for call checking
		var paramTypes []V2Type
		var paramNames []string
		variadic := false
		for _, p := range d.Params {
			paramTypes = append(paramTypes, c.resolveTypeExpr(p.Type))
			paramNames = append(paramNames, p.Name)
			if p.Variadic {
				variadic = true
			}
		}
		// 3.6.1: Translate parser bounds (TypeExpr) into bound name strings.
		var boundsByName map[string][]string
		if len(d.TypeParamBounds) > 0 {
			boundsByName = make(map[string][]string, len(d.TypeParamBounds))
			for tp, bounds := range d.TypeParamBounds {
				names := make([]string, 0, len(bounds))
				for _, b := range bounds {
					names = append(names, c.resolveTypeExpr(b).Name)
				}
				boundsByName[tp] = names
			}
		}
		c.fnSigs[d.Name] = V2FnSig{
			Params:          paramTypes,
			ParamNames:      paramNames,
			ReturnType:      retType,
			Variadic:        variadic,
			TypeParams:      d.TypeParams,
			TypeParamBounds: boundsByName,
		}
	case *parser.ClassDecl:
		c.scope.set(d.Name, V2Type{Name: d.Name})
		if len(d.Parents) > 0 {
			names := make([]string, len(d.Parents))
			for i, p := range d.Parents {
				names[i] = p.Name
			}
			c.parentTypes[d.Name] = names
		}
		// 3.7.2: register field types so SelectorExpr can resolve
		// `obj.field` without falling back to the codegen's emit-time
		// tracking maps. When a field has no explicit type, infer
		// from its default expression so chains like `var x = field[k]`
		// can flow through the side-map.
		if len(d.Fields) > 0 {
			fields := make(map[string]V2Type, len(d.Fields))
			for _, f := range d.Fields {
				if f.Type != nil {
					fields[f.Name] = c.resolveTypeExpr(f.Type)
				} else if f.Default != nil {
					fields[f.Name] = c.inferType(f.Default)
				} else {
					fields[f.Name] = typeAny
				}
			}
			c.classFields[d.Name] = fields
		}
		// 3.7.2: register class method signatures so SelectorExpr /
		// CallExpr inference can resolve `obj.method()` return types.
		if len(d.Methods) > 0 {
			methods := make(map[string]V2FnSig, len(d.Methods))
			for _, m := range d.Methods {
				retType := c.resolveTypeExpr(m.ReturnType)
				var paramTypes []V2Type
				var paramNames []string
				for _, p := range m.Params {
					paramTypes = append(paramTypes, c.resolveTypeExpr(p.Type))
					paramNames = append(paramNames, p.Name)
				}
				methods[m.Name] = V2FnSig{Params: paramTypes, ParamNames: paramNames, ReturnType: retType}
			}
			c.methodSigs[d.Name] = methods
		}
		// Sealed variants — register each variant as a subtype of the
		// sealed class so `Shape s = Circle(...)` etc. pass
		// compatibility. Variants are also registered as their own
		// scope entries so CallExpr ctor inference fires.
		if d.IsSealed {
			for _, v := range d.Variants {
				c.scope.set(v.Name, V2Type{Name: v.Name})
				existing := c.parentTypes[v.Name]
				c.parentTypes[v.Name] = append(existing, d.Name)
				if len(v.Params) > 0 {
					vfields := make(map[string]V2Type, len(v.Params))
					for _, p := range v.Params {
						vfields[p.Name] = c.resolveTypeExpr(p.Type)
					}
					c.classFields[v.Name] = vfields
				} else {
					// Marker for classFields presence (for ctor inference
					// gate): empty map is fine. But the struct-conformance
					// fallback also keys on classFields, so register it.
					c.classFields[v.Name] = map[string]V2Type{}
				}
			}
		}
	case *parser.DataClassDecl:
		c.scope.set(d.Name, V2Type{Name: d.Name})
		// Parent registration mirrors ClassDecl so subtype compatibility
		// (Pair : Summable etc.) works for data classes too.
		if len(d.Parents) > 0 {
			names := make([]string, len(d.Parents))
			for i, p := range d.Parents {
				names[i] = p.Name
			}
			c.parentTypes[d.Name] = names
		}
		if len(d.Params) > 0 {
			fields := make(map[string]V2Type, len(d.Params))
			for _, p := range d.Params {
				fields[p.Name] = c.resolveTypeExpr(p.Type)
			}
			c.classFields[d.Name] = fields
		}
		if len(d.Methods) > 0 {
			methods := make(map[string]V2FnSig, len(d.Methods))
			for _, m := range d.Methods {
				retType := c.resolveTypeExpr(m.ReturnType)
				var paramTypes []V2Type
				var paramNames []string
				for _, p := range m.Params {
					paramTypes = append(paramTypes, c.resolveTypeExpr(p.Type))
					paramNames = append(paramNames, p.Name)
				}
				methods[m.Name] = V2FnSig{Params: paramTypes, ParamNames: paramNames, ReturnType: retType}
			}
			c.methodSigs[d.Name] = methods
		}
	case *parser.EnumDecl:
		c.scope.set(d.Name, V2Type{Name: d.Name})
	case *parser.InterfaceDecl:
		c.scope.set(d.Name, V2Type{Name: d.Name})
		// Register interface method signatures
		methods := make(map[string]V2FnSig)
		for _, m := range d.Methods {
			retType := c.resolveTypeExpr(m.ReturnType)
			var paramTypes []V2Type
			var paramNames []string
			for _, p := range m.Params {
				paramTypes = append(paramTypes, c.resolveTypeExpr(p.Type))
				paramNames = append(paramNames, p.Name)
			}
			methods[m.Name] = V2FnSig{Params: paramTypes, ParamNames: paramNames, ReturnType: retType}
		}
		c.methodSigs[d.Name] = methods
	}
}

// --- Declaration checking ----------------------------------------------------

func (c *V2Checker) checkDecl(d parser.TopLevelDecl) {
	switch d := d.(type) {
	case *parser.FnDecl:
		c.checkFnDecl(d)
	case *parser.ClassDecl:
		c.checkClassDecl(d)
	case *parser.DataClassDecl:
		c.checkDataClassDecl(d)
	case *parser.TestDecl:
		// 3.7.2: walk test bodies so the side-map sees idents inside
		// `test "..." { ... }`. Without this, codegen has no NodeTypes
		// for vars declared inside test blocks.
		if d.Body != nil {
			inner := newV2Scope(c.scope)
			prevScope := c.scope
			c.scope = inner
			c.checkBlock(d.Body)
			c.scope = prevScope
		}
	}
}

func (c *V2Checker) checkFnDecl(d *parser.FnDecl) {
	inner := newV2Scope(c.scope)
	for _, p := range d.Params {
		pType := c.resolveTypeExpr(p.Type)
		inner.set(p.Name, pType)
	}

	prevScope := c.scope
	c.scope = inner

	retType := c.resolveTypeExpr(d.ReturnType)
	c.fnReturnType = &retType

	if d.Body != nil {
		c.checkBlock(d.Body)

		// Check all paths return if function has a return type. Tuple
		// returns (typically `(T, error)`) get exempted: codegen
		// synthesizes the missing-tail return when the body falls
		// through, the canonical "auto-error-tail" pattern. Strict
		// path coverage applies to scalar returns only.
		if d.ReturnType != nil && retType.Name != "any" && retType.Name != "tuple" && !c.blockReturns(d.Body) {
			c.errorf(d.Line, "function %q: not all code paths return a value", d.Name)
		}
	}

	c.fnReturnType = nil
	c.scope = prevScope
}

// blockReturns checks if a block definitely returns on all execution paths.
func (c *V2Checker) blockReturns(block *parser.BlockStmt) bool {
	if len(block.Stmts) == 0 {
		return false
	}
	last := block.Stmts[len(block.Stmts)-1]
	return c.stmtReturns(last)
}

// stmtReturns checks if a statement definitely returns.
func (c *V2Checker) stmtReturns(s parser.Stmt) bool {
	switch s := s.(type) {
	case *parser.ReturnStmt:
		return true
	case *parser.IfStmt:
		// Both branches must return
		if s.ElseStmt == nil {
			return false // no else → not all paths covered
		}
		thenReturns := c.blockReturns(s.Then)
		var elseReturns bool
		if block, ok := s.ElseStmt.(*parser.BlockStmt); ok {
			elseReturns = c.blockReturns(block)
		} else if ifStmt, ok := s.ElseStmt.(*parser.IfStmt); ok {
			elseReturns = c.stmtReturns(ifStmt)
		}
		return thenReturns && elseReturns
	case *parser.MatchStmt:
		// Match returns iff every arm returns. Codegen enforces exhaustivity
		// separately (compile error on non-exhaustive match against a sealed
		// type or enum). The typechecker doesn't yet know cross-pkg sealed
		// structure, so requiring a literal `_` wildcard would false-positive
		// every exhaustive sealed match. Relaxed per spec §6.6 + §6.4 —
		// exhaustivity is a separate check, not a return-paths check.
		if len(s.Cases) == 0 {
			return false
		}
		for _, mc := range s.Cases {
			if !c.blockReturns(mc.Body) {
				return false
			}
		}
		return true
	case *parser.BlockStmt:
		return c.blockReturns(s)
	}
	return false
}

func (c *V2Checker) checkClassDecl(d *parser.ClassDecl) {
	for _, f := range d.Fields {
		if f.Type == nil && f.Default == nil {
			c.errorf(d.Line, "field %q needs a type annotation or default value", f.Name)
		}
	}
	prevClass := c.currentClass
	c.currentClass = d.Name
	for _, m := range d.Methods {
		c.checkMethodDecl(m, d.Fields)
	}
	c.currentClass = prevClass
}

func (c *V2Checker) checkMethodDecl(m *parser.MethodDecl, fields []*parser.FieldDecl) {
	inner := newV2Scope(c.scope)
	// Add fields to scope. Fields without explicit types infer from
	// their default expression so the side-map carries the same
	// shape codegen needs (e.g. `Map<K, V>` from `field = Map{}`).
	for _, f := range fields {
		var ft V2Type
		if f.Type != nil {
			ft = c.resolveTypeExpr(f.Type)
		} else if f.Default != nil {
			ft = c.inferType(f.Default)
		} else {
			ft = typeAny
		}
		inner.set(f.Name, ft)
	}
	// Add params
	for _, p := range m.Params {
		inner.set(p.Name, c.resolveTypeExpr(p.Type))
	}

	prevScope := c.scope
	c.scope = inner

	retType := c.resolveTypeExpr(m.ReturnType)
	c.fnReturnType = &retType

	if m.Body != nil {
		c.checkBlock(m.Body)
	}

	c.fnReturnType = nil
	c.scope = prevScope
}

func (c *V2Checker) checkDataClassDecl(d *parser.DataClassDecl) {
	for _, f := range d.Params {
		if f.Type == nil {
			c.errorf(d.Line, "data class field %q needs a type annotation", f.Name)
		}
	}
}

// --- Statement checking ------------------------------------------------------

func (c *V2Checker) checkBlock(block *parser.BlockStmt) {
	for _, s := range block.Stmts {
		c.checkStmt(s)
	}
}

func (c *V2Checker) checkStmt(s parser.Stmt) {
	switch s := s.(type) {
	case *parser.VarStmt:
		c.checkVarStmt(s)
	case *parser.AssignStmt:
		c.checkAssignStmt(s)
	case *parser.ReturnStmt:
		if s.Value != nil {
			valType := c.inferType(s.Value)
			// Check return value matches declared return type. The
			// auto-error-tail shape — function declared `(T, error)`
			// but `return x` provides only the value half — is the
			// canonical way to indicate success; codegen synthesizes
			// the nil error. Accept that without a mismatch error.
			if c.fnReturnType != nil && c.fnReturnType.Name != "any" && valType.Name != "any" {
				expected := *c.fnReturnType
				// Tuple returns get a loose check — codegen handles
				// the auto-error-tail synthesis (`return T` → fills
				// nil error half; `return E` → fills zero T half) and
				// callable types (Fn) flowing through tuple slots
				// aren't strict-tracked yet. Strict mismatch checks
				// only apply to scalar returns today.
				if expected.Name != "tuple" && !c.compatible(expected, valType) {
					c.errorf(s.Line, "return type mismatch: expected %s, got %s", c.fnReturnType, valType)
				}
			}
		}
	case *parser.IfStmt:
		c.inferType(s.Cond)
		c.checkBlock(s.Then)
		if s.ElseStmt != nil {
			if block, ok := s.ElseStmt.(*parser.BlockStmt); ok {
				c.checkBlock(block)
			} else if ifStmt, ok := s.ElseStmt.(*parser.IfStmt); ok {
				c.checkStmt(ifStmt)
			}
		}
	case *parser.ForStmt:
		if s.IsRange {
			inner := newV2Scope(c.scope)
			// Infer loop variable type from the range expression
			rangeType := c.inferType(s.Range)
			itemType := typeAny
			if rangeType.Name == "int" {
				// Range loop: for i in 0..5 → i is int
				itemType = typeInt
			} else if len(rangeType.Args) > 0 {
				// Collection: for item in List<String> → item is String
				itemType = rangeType.Args[0]
			}
			inner.set(s.Item, itemType)
			if s.IndexVar != "" {
				inner.set(s.IndexVar, typeInt)
			}
			prevScope := c.scope
			prevLoop := c.inLoop
			c.scope = inner
			c.inLoop = true
			c.checkBlock(s.Body)
			c.scope = prevScope
			c.inLoop = prevLoop
		}
	case *parser.WhileStmt:
		c.inferType(s.Cond)
		prevLoop := c.inLoop
		c.inLoop = true
		c.checkBlock(s.Body)
		c.inLoop = prevLoop
	case *parser.ExprStmt:
		c.inferType(s.Expr)
		if s.OrHandler != nil && s.OrHandler.Body != nil {
			c.checkBlock(s.OrHandler.Body)
		}
	case *parser.MatchStmt:
		c.inferType(s.Subject)
		for _, mc := range s.Cases {
			c.checkBlock(mc.Body)
		}
	case *parser.FnDecl:
		c.checkFnDecl(s)
	case *parser.BlockStmt:
		c.checkBlock(s)
	case *parser.TupleVarStmt:
		// Multi-value var. When the RHS is a Go-FFI call, propagate per-slot
		// Go types into the scope so each name has the right GoType when
		// later expressions reference it. Otherwise fall back to typeAny.
		c.inferType(s.Value)
		slotTypes := c.tupleSlotTypes(s.Value, len(s.Names))
		for i, name := range s.Names {
			t := typeAny
			if i < len(slotTypes) {
				t = slotTypes[i]
			}
			c.scope.set(name, t)
		}
	case *parser.BreakStmt:
		if !c.inLoop {
			c.errorf(0, "'break' outside of loop")
		}
	case *parser.ContinueStmt:
		if !c.inLoop {
			c.errorf(0, "'continue' outside of loop")
		}
	case *parser.AssertStmt:
		c.inferType(s.Cond)
	case *parser.WithStmt:
		inner := newV2Scope(c.scope)
		for _, r := range s.Resources {
			// Infer the resource's type from its value so method calls
			// against it (`gw.Write(data)`) can resolve through the
			// bind side-map and trigger FFI auto-propagation. Tuple
			// returns extract slot 0 (the resource); single-value
			// returns are just the value itself.
			t := c.inferType(r.Value)
			if t.Name == "tuple" && len(t.Args) > 0 {
				t = t.Args[0]
			}
			inner.set(r.Name, t)
		}
		prevScope := c.scope
		c.scope = inner
		c.checkBlock(s.Body)
		c.scope = prevScope
	}
}

func (c *V2Checker) checkVarStmt(s *parser.VarStmt) {
	var declaredType V2Type
	if s.Type != nil {
		declaredType = c.resolveTypeExpr(s.Type)
	}

	if s.Value != nil {
		valType := c.inferType(s.Value)

		// Tuple-typed values (e.g. function references whose return is
		// (T, error), or multi-value calls) get a loose compat check —
		// the existing engine doesn't strict-track Fn signatures or
		// tuple destructuring at the var-init seam, and the strict
		// check would surface false positives. Strict mismatch checks
		// only apply to scalar values today.
		if s.Type != nil && valType.Name != "tuple" && !c.compatible(declaredType, valType) {
			c.errorf(s.Line, "type mismatch: variable %q declared as %s but assigned %s",
				s.Name, declaredType, valType)
		}

		if s.Type != nil {
			c.scope.set(s.Name, declaredType)
		} else {
			// `var x = call() or { }` where call returns a tuple
			// (T, error) extracts the first slot and binds x to T.
			// Without this, x would carry the synthetic "tuple" type
			// name and downstream class-typed checks would miss.
			bindType := valType
			if s.OrHandler != nil && valType.Name == "tuple" && len(valType.Args) > 0 {
				bindType = valType.Args[0]
			}
			c.scope.set(s.Name, bindType)
			// When using var with or handler, store the resolved type name
			// so codegen can emit the correct Java type instead of Object
			if s.OrHandler != nil && bindType.Name != "" && bindType.Name != "any" {
				s.ResolvedType = bindType.Name
			}
		}
		// Typecheck or-handler body (for type annotations inside the block)
		if s.OrHandler != nil && s.OrHandler.Body != nil {
			c.checkBlock(s.OrHandler.Body)
		}
	} else if s.Type != nil {
		c.scope.set(s.Name, declaredType)
	} else {
		c.scope.set(s.Name, typeAny)
	}
}

func (c *V2Checker) checkAssignStmt(s *parser.AssignStmt) {
	if ident, ok := s.Target.(*parser.Ident); ok {
		// `_` is the discard target — `_ = expr` is "evaluate, throw away".
		// Also `=` may be the FIRST assignment to a name in some Zinc forms,
		// so an unfound name shouldn't be a hard error here. Phase 3.5 will
		// tighten this once bind+typecheck flow is mature.
		if ident.Name != "_" {
			if _, found := c.scope.lookup(ident.Name); !found {
				// Quietly tolerate — the codegen surfaces real errors.
				_ = ident
			}
		}
	}
	// 3.7.2: walk Target so the side-map gets entries for the LHS.
	// Codegen reads NodeTypes[lhs ident] for ptr-target detection
	// (e.g. `box.name = "x"` needs to know box is a class).
	c.inferType(s.Target)
	c.inferType(s.Value)
}

// --- Type inference ----------------------------------------------------------

// inferType wraps inferTypeImpl and records the result in the nodeTypes
// side-map (when initialized) so codegen can read per-expression types
// without redoing inference. The side-map is keyed by parser.Expr pointer
// identity, so different occurrences of the same name resolve to whatever
// type they had at *that* AST position.
func (c *V2Checker) inferType(e parser.Expr) V2Type {
	if e == nil {
		return typeNull
	}
	t := c.inferTypeImpl(e)
	if c.nodeTypes != nil {
		c.nodeTypes[e] = t
	}
	return t
}

// NodeTypes returns the side-map populated during checking. Returns nil
// if the checker hasn't been told to capture node types (the Phase 3.5
// driver in compiler.go enables this via CheckV2WithBindings).
func (c *V2Checker) NodeTypes() map[parser.Expr]V2Type {
	return c.nodeTypes
}

// inferTypeImpl is the original inference logic; inferType wraps it with
// node-type recording.
func (c *V2Checker) inferTypeImpl(e parser.Expr) V2Type {
	switch e := e.(type) {
	case *parser.IntLit:
		return typeInt
	case *parser.FloatLit:
		return typeDouble
	case *parser.StringLit, *parser.RawStringLit:
		return typeStr
	case *parser.StringInterpLit:
		// 3.7.2: walk interpolation parts so the side-map gets entries
		// for every Ident/Expr inside `${...}`. Without this, codegen
		// can't see types of interpolated values via NodeTypes.
		for _, p := range e.Parts {
			c.inferType(p)
		}
		return typeStr
	case *parser.BoolLit:
		return typeBool
	case *parser.NullLit:
		return typeNull
	case *parser.DefaultExpr:
		// default(T) — resolves to T. The legality check (non-nullable
		// reference classes can't have a "zero value" because nil
		// pointer would violate non-null guarantees) lives in
		// checkDefaultExpr below, called from checkStmt walks; the
		// type inference itself just returns the resolved type.
		return c.resolveTypeExpr(e.Type)
	case *parser.ThisExpr:
		// 3.7.2: `this` resolves to the enclosing class's type, so
		// `this.field` lookups can drive SelectorExpr inference.
		if c.currentClass != "" {
			return V2Type{Name: c.currentClass}
		}
		return typeAny
	case *parser.Ident:
		if t, found := c.scope.lookup(e.Name); found {
			return t
		}
		// `this` is sometimes parsed as a bare Ident rather than a
		// ThisExpr depending on surrounding statement shape.
		if e.Name == "this" && c.currentClass != "" {
			return V2Type{Name: c.currentClass}
		}
		// Built-in functions — don't error
		return typeAny
	case *parser.BinaryExpr:
		left := c.inferType(e.Left)
		right := c.inferType(e.Right)
		// Spec §7.1: `==` / `!=` on slices/maps is a compile error.
		// Suggest slices.Equal / maps.Equal. Phase 3.6.3.
		if e.Op == "==" || e.Op == "!=" {
			if isSliceOrListType(left) && isSliceOrListType(right) {
				c.errorf(0, "'%s' on List/slice values is not allowed; use slices.Equal(a, b)", e.Op)
			}
			if isMapType(left) && isMapType(right) {
				c.errorf(0, "'%s' on Map values is not allowed; use maps.Equal(a, b)", e.Op)
			}
		}
		result := c.inferBinaryType(e.Op, left, right)
		e.ResolvedType = result.Name
		return result
	case *parser.UnaryExpr:
		operand := c.inferType(e.Operand)
		if e.Op == "!" || e.Op == "not" {
			return typeBool
		}
		return operand
	case *parser.CallExpr:
		c.inferType(e.Callee)
		var argTypes []V2Type
		for _, a := range e.Args {
			argTypes = append(argTypes, c.inferType(a))
		}
		// Go-FFI call detection. Two seams handled:
		//  (a) pkg.Func(...) where `pkg` is a Go-imported package alias.
		//  (b) recv.Method(...) where `recv` has GoType (from a previous
		//      FFI call's return). e.g. `var dec, _ = ocf.NewDecoder(...);
		//      dec.Decode(&got)` — `dec.Decode`'s return type comes from
		//      the method-set on `*ocf.Decoder`.
		// In both cases, the codegen reads the resulting V2Type.GoType to
		// drive pointer-vs-value emission and method-set lookups.
		if sel, ok := e.Callee.(*parser.SelectorExpr); ok && c.ffi != nil {
			if pkgIdent, ok := sel.Object.(*parser.Ident); ok {
				if pkgPath, isPkg := c.importMap[pkgIdent.Name]; isPkg {
					if goType := c.ffi.FuncReturnTypeAt(pkgPath, sel.Field, 0); goType != nil {
						return V2Type{Name: "go-ffi", GoType: goType}
					}
				}
			}
		}
		// Zinc-subpackage qualified call: `pkg.func(...)` where `pkg`
		// is a Zinc-side import alias and `func` is registered in
		// the cross-file fnSigs table. Without this, calls like
		// `expressions.compile(src)` fell through to the default
		// `any` type, breaking the bound side-map (and downstream
		// codegen decisions like already-pointer detection at
		// assignment sites). Guard against shadowing: only take this
		// path when the leading ident is NOT a local-scope name
		// (which would make this a method call, not a package call).
		if sel, ok := e.Callee.(*parser.SelectorExpr); ok {
			if pkgIdent, isIdent := sel.Object.(*parser.Ident); isIdent {
				_, isLocal := c.scope.lookup(pkgIdent.Name)
				if !isLocal {
					if sig, ok := c.fnSigs[sel.Field]; ok {
						return sig.ReturnType
					}
				}
			}
		}
		// Re-enter the FFI/method block with the original sel for
		// the remaining method-call paths below.
		if sel, ok := e.Callee.(*parser.SelectorExpr); ok && c.ffi != nil {
			// (b) Method on Go-typed receiver. Walk the receiver's V2Type
			// (already inferred earlier in this function for the method-
			// call branch — see below). To avoid a double-infer, look up
			// the receiver's NodeType via the side-map if available; fall
			// back to inferType, which is idempotent.
			recvType := c.inferType(sel.Object)
			if recvType.GoType != nil {
				if goType := c.ffi.MethodReturnTypeAt(recvType.GoType, sel.Field, 0); goType != nil {
					return V2Type{Name: "go-ffi", GoType: goType}
				}
			}
		}
		// Check method call on object: obj.method(args)
		if sel, ok := e.Callee.(*parser.SelectorExpr); ok {
			objType := c.inferType(sel.Object)
			if objType.Name != "" && objType.Name != "any" {
				// First check Zinc-defined method signatures (interfaces, classes)
				if methods, ok := c.methodSigs[objType.Name]; ok {
					if sig, ok := methods[sel.Field]; ok {
						return sig.ReturnType
					}
				}
				// Then try Java type introspection via javap
				var typeArgStrs []string
				for _, a := range objType.Args {
					typeArgStrs = append(typeArgStrs, a.Name)
				}
				resolved := ResolveZincMethodReturn(objType.Name, typeArgStrs, sel.Field)
				if resolved != "" {
					return V2Type{Name: resolved}
				}
			}
		}
		// Check function call argument types against signature
		if ident, ok := e.Callee.(*parser.Ident); ok {
			if sig, found := c.fnSigs[ident.Name]; found {
				// Spread args at the call site (`f(...xs)`) make the
				// effective arg count dynamic — skip arity + per-position
				// type checks when any arg is a spread.
				hasSpread := false
				for _, a := range e.Args {
					if _, ok := a.(*parser.SpreadExpr); ok {
						hasSpread = true
						break
					}
				}
				if !hasSpread {
					// Check arg count (skip if `T... name` in sig or
					// legacy `*args` / `**kwargs` ParamName prefix).
					if len(argTypes) != len(sig.Params) && len(sig.Params) > 0 {
						hasVariadic := sig.Variadic
						if !hasVariadic {
							for _, pn := range sig.ParamNames {
								if strings.HasPrefix(pn, "**") || strings.HasPrefix(pn, "*") {
									hasVariadic = true
								}
							}
						}
						if !hasVariadic && len(argTypes) > len(sig.Params) {
							c.errorf(0, "function %q expects %d args, got %d", ident.Name, len(sig.Params), len(argTypes))
						}
					}
					// Check arg types (skip the variadic tail slot —
					// callers pass individual elements, not the slice).
					typedSlots := len(sig.Params)
					if sig.Variadic && typedSlots > 0 {
						typedSlots--
					}
					// 3.6.1 generic bounds: as we walk args, accumulate the
					// concrete type each type-param resolves to (`typeArgs`).
					// Then verify each bound is satisfied by the inferred
					// concrete. A type-param slot accepts any actual type
					// (modulo bounds), but multiple uses of the same param
					// must agree (e.g. `max(T a, T b)` requires same T).
					typeParamSet := make(map[string]bool, len(sig.TypeParams))
					for _, tp := range sig.TypeParams {
						typeParamSet[tp] = true
					}
					typeArgs := make(map[string]V2Type)
					for i, argT := range argTypes {
						if i >= typedSlots {
							continue
						}
						declared := sig.Params[i]
						if typeParamSet[declared.Name] {
							if prev, seen := typeArgs[declared.Name]; seen {
								if !c.compatible(prev, argT) && !c.compatible(argT, prev) {
									c.errorf(0, "type parameter %s of %q is %s but argument %d is %s",
										declared.Name, ident.Name, prev, i+1, argT)
								}
								continue
							}
							typeArgs[declared.Name] = argT
							continue
						}
						if !c.compatible(declared, argT) {
							// Nested-thrower hoist: codegen lifts the
							// error half of `(T, error)` into the enclosing
							// thrower's tail and passes T to the inner call.
							// Accept slot 0 of a tuple arg when it matches
							// the declared param; the typechecker doesn't
							// model the hoist directly.
							if argT.Name == "tuple" && len(argT.Args) > 0 && c.compatible(declared, argT.Args[0]) {
								continue
							}
							c.errorf(0, "argument %d of %q: expected %s, got %s",
								i+1, ident.Name, declared, argT)
						}
					}
					// Bound satisfaction check.
					for tp, concrete := range typeArgs {
						bounds := sig.TypeParamBounds[tp]
						for _, bound := range bounds {
							if !c.satisfiesBound(concrete, bound) {
								c.errorf(0, "type parameter %s of %q: %s does not satisfy bound %s",
									tp, ident.Name, concrete, bound)
							}
						}
					}
				}
				return sig.ReturnType
			}
			// 3.7.2: ctor-style call to a class/data-class — `MyClass(...)`
			// returns V2Type{Name:"MyClass"} so codegen can resolve
			// `var x = MyClass(...); x.method()` via the side-map. The
			// type was registered in scope during registerDecl for
			// ClassDecl/DataClassDecl/EnumDecl/InterfaceDecl. Self-typing
			// (scope[name].Name == name) discriminates types from values.
			if classType, ok := c.scope.lookup(ident.Name); ok && classType.Name == ident.Name {
				return classType
			}
		}
		// 3.7.2: qualified ctor `pkg.Class(...)` — same idea but for
		// SelectorExpr callees. classNames is populated from cross-pkg
		// exports via externalSigs.
		if sel, ok := e.Callee.(*parser.SelectorExpr); ok {
			if c.classNames[sel.Field] {
				return V2Type{Name: sel.Field}
			}
		}
		return typeAny
	case *parser.SelectorExpr:
		objType := c.inferType(e.Object)
		// 3.7.2: resolve field access against the class's declared
		// fields. Carries the field's TypeExpr forward so codegen
		// can walk into Fn types stored as fields. Skip Go-FFI values
		// — those follow Go's method-set, not Zinc class fields, and
		// the codegen handles them via NodeTypes[ident].GoType.
		if objType.Name != "" && objType.Name != "go-ffi" && objType.GoType == nil {
			if fields, ok := c.classFields[objType.Name]; ok {
				if ft, ok := fields[e.Field]; ok {
					return ft
				}
			}
			// Also check method return types — `fab.getDLQ` (no
			// parens) refers to the method as a value; CallExpr
			// inference handles `fab.getDLQ()`.
			if methods, ok := c.methodSigs[objType.Name]; ok {
				if sig, ok := methods[e.Field]; ok {
					return sig.ReturnType
				}
			}
		}
		// 3.7.2: `pkg.Class` references — when Object is an Ident
		// whose name doesn't resolve to a value (typeAny) and Field
		// is a registered class name, return V2Type{Name: Field} so
		// `var r = pkg.Class()` infers `r` as Class.
		if objType.Name == "any" || objType.Name == "" {
			if c.classNames[e.Field] {
				return V2Type{Name: e.Field}
			}
		}
		// Built-in method-return resolution (String, List, Map).
		if objType.Name != "" && objType.Name != "any" {
			var typeArgStrs []string
			for _, a := range objType.Args {
				typeArgStrs = append(typeArgStrs, a.Name)
			}
			resolved := ResolveZincMethodReturn(objType.Name, typeArgStrs, e.Field)
			if resolved != "" {
				return V2Type{Name: resolved}
			}
		}
		return typeAny
	case *parser.IndexExpr:
		objType := c.inferType(e.Object)
		c.inferType(e.Index)
		// Spec §3.x — index access narrows the result type:
		//   List<T> / T[] → T   (numeric index)
		//   Map<K, V>    → V   (key index)
		// Other / unknown → any.
		if objType.Name == "List" && len(objType.Args) == 1 {
			return objType.Args[0]
		}
		if strings.HasSuffix(objType.Name, "[]") {
			elem := strings.TrimSuffix(objType.Name, "[]")
			return V2Type{Name: elem}
		}
		if objType.Name == "Map" && len(objType.Args) == 2 {
			return objType.Args[1]
		}
		return typeAny
	case *parser.ListLit:
		for _, el := range e.Elements {
			c.inferType(el)
		}
		// 3.7.2: when the literal carries an explicit `List<T>[]` /
		// `T[]` annotation, propagate it so subsequent index access
		// can recover T via the side-map.
		if e.ExplicitType != nil {
			return c.resolveTypeExpr(e.ExplicitType)
		}
		return V2Type{Name: "List"}
	case *parser.MapLit:
		for i := range e.Keys {
			c.inferType(e.Keys[i])
			c.inferType(e.Values[i])
		}
		if e.ExplicitType != nil {
			return c.resolveTypeExpr(e.ExplicitType)
		}
		return V2Type{Name: "Map"}
	case *parser.LambdaExpr:
		return typeAny
	case *parser.IfExpr:
		c.inferType(e.Cond)
		thenType := c.inferType(e.Then)
		c.inferType(e.Else)
		return thenType
	case *parser.TupleLit:
		for _, el := range e.Elements {
			c.inferType(el)
		}
		return V2Type{Name: "tuple"}
	case *parser.SliceExpr:
		c.inferType(e.Object)
		return typeAny
	case *parser.SpreadExpr:
		return c.inferType(e.Expr)
	case *parser.SpawnExpr:
		// Typecheck the body of spawn blocks so var types resolve
		if e.Body != nil {
			c.checkBlock(e.Body)
		}
		return typeAny
	case *parser.RangeExpr:
		// Ranges produce int sequences
		return typeInt
	case *parser.MatchExpr:
		if len(e.Cases) > 0 {
			return c.inferType(e.Cases[0].Value)
		}
		return typeAny
	default:
		return typeAny
	}
}

// tupleSlotTypes returns per-slot V2Types for a tuple-producing expression.
// Today this handles Go-FFI calls (`pkg.Func(...)`) where the resolver
// supplies per-slot Go return types. Other shapes (Zinc-side multi-return
// fns, method calls returning tuples) fall through and the caller uses
// typeAny per slot. Length of the result may be less than `arity`; the
// caller pads with typeAny.
func (c *V2Checker) tupleSlotTypes(e parser.Expr, arity int) []V2Type {
	if c.ffi == nil {
		return nil
	}
	call, ok := e.(*parser.CallExpr)
	if !ok {
		return nil
	}
	sel, ok := call.Callee.(*parser.SelectorExpr)
	if !ok {
		return nil
	}
	pkgIdent, ok := sel.Object.(*parser.Ident)
	if !ok {
		return nil
	}
	pkgPath, isPkg := c.importMap[pkgIdent.Name]
	if !isPkg {
		return nil
	}
	out := make([]V2Type, arity)
	for i := 0; i < arity; i++ {
		if goType := c.ffi.FuncReturnTypeAt(pkgPath, sel.Field, i); goType != nil {
			out[i] = V2Type{Name: "go-ffi", GoType: goType}
		} else {
			out[i] = typeAny
		}
	}
	return out
}

// isSliceOrListType reports whether t is a List<T> or T[] type.
func isSliceOrListType(t V2Type) bool {
	if t.Name == "List" {
		return true
	}
	return strings.HasSuffix(t.Name, "[]")
}

// isMapType reports whether t is a Map<K,V> type.
func isMapType(t V2Type) bool {
	return t.Name == "Map"
}

func (c *V2Checker) inferBinaryType(op string, left, right V2Type) V2Type {
	switch op {
	case "+", "-", "*", "/", "%", "**":
		if left.Name == "double" || right.Name == "double" {
			return typeDouble
		}
		if left.Name == "int" && right.Name == "int" {
			return typeInt
		}
		if left.Name == "String" && op == "+" {
			return typeStr
		}
		return typeAny
	case "==", "!=", "<", "<=", ">", ">=", "in", "not in", "is", "is not":
		return typeBool
	case "&&", "||", "and", "or":
		return typeBool
	default:
		return typeAny
	}
}

// --- Type compatibility ------------------------------------------------------

func (c *V2Checker) compatible(declared, actual V2Type) bool {
	if declared.Name == "any" || actual.Name == "any" {
		return true
	}
	if declared.Name == actual.Name {
		return true
	}
	// Trailing-segment match for qualified-vs-bare type names. When one
	// side is `pkg.T` and the other is `T` and the call site's import
	// graph collapses both to the same definition, they're compatible.
	// Conservative approximation until Phase 3.5 wires per-call-site
	// import resolution into the typechecker.
	if trailingMatch(declared.Name, actual.Name) {
		return true
	}
	// int → float is OK
	if declared.Name == "double" && actual.Name == "int" {
		return true
	}
	// Array type is compatible with List literal ([1, 2, 3] can init int[])
	if strings.HasSuffix(declared.Name, "[]") && actual.Name == "List" {
		return true
	}
	// Spec §5.1 (Phase 3.6.2): null is compatible ONLY with explicitly
	// nullable types (`T?`). Non-nullable `T` rejects null at compile time.
	// This is a deliberate break from the legacy permissive behavior —
	// existing code that returned/assigned null to non-`T?` slots needs
	// to migrate. Smart-cast on `x != null` covers the common usage in
	// `tryNarrow` so a guard restores non-null type within its branch.
	//
	// Exception: `error` is the errors-as-values tail type. It's a Go
	// interface with `nil` as a valid zero — a function returning
	// `(T, error)` returns `value, null` on the success path. Treating
	// it as implicitly nullable preserves the spec's idiomatic shape
	// without requiring `error?` everywhere.
	if actual.Name == "null" {
		if declared.Name == "error" {
			return true
		}
		return declared.Nullable
	}
	// Check if actual type implements/extends declared type (recurse
	// up the parent chain so multi-level inheritance reaches the root).
	if parents, ok := c.parentTypes[actual.Name]; ok {
		for _, p := range parents {
			if p == declared.Name {
				return true
			}
			if c.compatible(declared, V2Type{Name: p}) {
				return true
			}
		}
	}
	// Structural conformance fallback: when `declared` is an interface
	// (or `error`) and `actual` is a class/data class, accept the
	// assignment without walking method sets. Go's compiler enforces
	// the actual conformance check, so a missing method surfaces there
	// rather than here. Mirrors the historical behavior the legacy
	// typechecker relied on for Pair → Summable etc.
	if actual.Name != "" {
		_, actualIsClass := c.classFields[actual.Name]
		if declared.Name == "error" && actualIsClass {
			return true
		}
		if _, declaredIsIface := c.methodSigs[declared.Name]; declaredIsIface && actualIsClass {
			return true
		}
	}
	// 3.7.2: Go-FFI values (V2Type{Name:"go-ffi", GoType:...}) match any
	// declared type by deferring to the Go compiler — when the user wrote
	// `slog.Handler h = json.NewHandler(...)`, the typechecker can't
	// statically check that *slog.JSONHandler implements slog.Handler
	// (the GoType is in go/types space, not Zinc), but Go's compiler
	// will reject mismatches downstream.
	if actual.Name == "go-ffi" {
		return true
	}
	return false
}

// --- Type resolution ---------------------------------------------------------

// trailingMatch returns true when two type names share the same trailing
// identifier after the last `.` separator and at least one side is bare
// (no `.`). e.g. `core.Schema` and `Schema` match; `core.Schema` and
// `hambaAvro.Schema` do not (both are qualified). Used as a phase-3-era
// approximation for qualified-vs-bare type compatibility.
func trailingMatch(a, b string) bool {
	aDot := strings.LastIndex(a, ".")
	bDot := strings.LastIndex(b, ".")
	// Both qualified or both bare → exact-name match was already checked
	// by the caller; a trailing match here is unsafe.
	if (aDot >= 0) == (bDot >= 0) {
		return false
	}
	aTail := a
	if aDot >= 0 {
		aTail = a[aDot+1:]
	}
	bTail := b
	if bDot >= 0 {
		bTail = b[bDot+1:]
	}
	return aTail == bTail && aTail != ""
}

// satisfiesBound reports whether a concrete type satisfies a generic bound.
// Spec §4.3 — primitive built-in satisfactions:
//
//	int, long, float, double, byte, String → Comparable, Hashable, Equatable, Stringer
//	bool → Equatable, Hashable, Stringer
//
// User-defined bounds are interfaces — concrete satisfies bound iff
// concrete declares it as a parent (implements relation tracked via
// parentTypes during registration).
func (c *V2Checker) satisfiesBound(concrete V2Type, bound string) bool {
	if bound == "" || bound == "any" {
		return true
	}
	if concrete.Name == bound {
		return true
	}
	switch concrete.Name {
	case "int", "long", "float", "double", "byte", "String":
		switch bound {
		case "Comparable", "Hashable", "Equatable", "Stringer":
			return true
		}
	case "bool":
		switch bound {
		case "Equatable", "Hashable", "Stringer":
			return true
		}
	}
	// User-defined: walk the parent chain.
	if parents, ok := c.parentTypes[concrete.Name]; ok {
		for _, p := range parents {
			if p == bound {
				return true
			}
			if c.satisfiesBound(V2Type{Name: p}, bound) {
				return true
			}
		}
	}
	return false
}

// isValueType reports whether the canonical type name names a value
// type. Value types in Zinc always have a value and cannot be null —
// `int?`, `String?`, etc. are rejected by the typechecker. Only
// reference types (classes, List<T>, Map<K,V>, etc.) can be optional.
func isValueType(name string) bool {
	switch name {
	case "int", "long", "float", "double", "byte", "bool", "String":
		return true
	}
	return false
}

// canonicalTypeName normalizes type names that have multiple spellings
// in source code to the canonical V2Type form. Without this the
// typechecker treats `bool` (spec canonical) and `boolean`/`Bool`/`Boolean`
// (legacy spellings) as distinct types and rejects straightforward returns.
// `Object` is the Zinc-source name for the universal type — collapses to
// `any` for compatibility.
func canonicalTypeName(name string) string {
	switch name {
	case "boolean":
		return "bool"
	case "Bool":
		return "bool"
	case "Boolean":
		return "bool"
	case "Int":
		return "int"
	case "Integer":
		return "int"
	case "Long":
		return "long"
	case "Float":
		return "float"
	case "Double":
		return "double"
	case "String":
		return "String"
	case "Object":
		return "any"
	}
	return name
}

func (c *V2Checker) resolveTypeExpr(t parser.TypeExpr) V2Type {
	if t == nil {
		return typeAny
	}
	switch tt := t.(type) {
	case *parser.SimpleType:
		return V2Type{Name: canonicalTypeName(tt.Name), TypeExpr: t}
	case *parser.GenericType:
		var args []V2Type
		for _, a := range tt.TypeArgs {
			args = append(args, c.resolveTypeExpr(a))
		}
		return V2Type{Name: tt.Name, Args: args, TypeExpr: t}
	case *parser.ArrayType:
		elem := c.resolveTypeExpr(tt.ElementType)
		return V2Type{Name: elem.Name + "[]", Args: elem.Args, TypeExpr: t}
	case *parser.OptionalType:
		inner := c.resolveTypeExpr(tt.Inner)
		if isValueType(inner.Name) {
			c.errorf(0, "T? is only valid for reference types — %q is a value type and cannot be null; use (T, bool), (T, error), or a default-returning accessor", inner.Name)
		}
		inner.Nullable = true
		inner.TypeExpr = t
		return inner
	case *parser.FuncTypeExpr:
		return V2Type{Name: "Fn", TypeExpr: t}
	case *parser.TupleType:
		// Multi-value return like `(T, error)`. Resolve each slot and
		// store as Args; the carrier type Name is "tuple". The
		// var-stmt with or-handler form treats this as the first
		// slot's type — `var c = call() or {}` binds to Args[0].
		var args []V2Type
		for _, slot := range tt.Elements {
			args = append(args, c.resolveTypeExpr(slot))
		}
		return V2Type{Name: "tuple", Args: args, TypeExpr: t}
	default:
		return typeAny
	}
}
