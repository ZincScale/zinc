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
type V2Type struct {
	Name     string   // "int", "String", "List", "Map", "null", "any"
	Args     []V2Type // generic args: list[int] → Args=[int]
	Nullable bool     // Optional[T]
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
	typeBool    = V2Type{Name: "boolean"}
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
}

// V2Checker performs type checking on a v2 AST.
type V2Checker struct {
	errors       []V2Error
	scope        *V2Scope
	fnReturnType *V2Type            // current function's return type
	fnSigs       map[string]V2FnSig // function signatures for call checking
	methodSigs   map[string]map[string]V2FnSig // type → method → signature
	parentTypes  map[string][]string // class → parent types (interfaces, superclasses)
	inLoop       bool               // tracking if inside a loop for break/continue
}

// CheckV2 runs the v2 type checker on a parsed program.
// Returns errors found. Empty slice = all good.
func CheckV2(prog *parser.Program) []V2Error {
	return CheckV2WithContext(prog, nil)
}

// CheckV2WithContext runs the type checker with pre-populated cross-file signatures.
// externalSigs contains function/method signatures from other files in the project.
func CheckV2WithContext(prog *parser.Program, externalSigs *CollectedSigs) []V2Error {
	c := &V2Checker{
		scope:       newV2Scope(nil),
		fnSigs:      make(map[string]V2FnSig),
		methodSigs:  make(map[string]map[string]V2FnSig),
		parentTypes: make(map[string][]string),
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
	}

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

	return c.errors
}

// CollectSignatures extracts function and interface method signatures from a program.
// Used in multi-file compilation to build cross-file context.
// CollectedSigs holds both function and method signatures from a file.
type CollectedSigs struct {
	FnSigs      map[string]V2FnSig
	MethodSigs  map[string]map[string]V2FnSig // type → method → sig
	ParentTypes map[string][]string           // class → parent types
}

func CollectSignatures(prog *parser.Program) CollectedSigs {
	c := &V2Checker{
		scope:       newV2Scope(nil),
		fnSigs:      make(map[string]V2FnSig),
		methodSigs:  make(map[string]map[string]V2FnSig),
		parentTypes: make(map[string][]string),
	}
	for _, d := range prog.Decls {
		c.registerDecl(d)
	}
	return CollectedSigs{FnSigs: c.fnSigs, MethodSigs: c.methodSigs, ParentTypes: c.parentTypes}
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
		for _, p := range d.Params {
			paramTypes = append(paramTypes, c.resolveTypeExpr(p.Type))
			paramNames = append(paramNames, p.Name)
		}
		c.fnSigs[d.Name] = V2FnSig{Params: paramTypes, ParamNames: paramNames, ReturnType: retType}
	case *parser.ClassDecl:
		c.scope.set(d.Name, V2Type{Name: d.Name})
		if len(d.Parents) > 0 {
			names := make([]string, len(d.Parents))
			for i, p := range d.Parents {
				names[i] = p.Name
			}
			c.parentTypes[d.Name] = names
		}
	case *parser.DataClassDecl:
		c.scope.set(d.Name, V2Type{Name: d.Name})
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

		// Check all paths return if function has a return type
		if d.ReturnType != nil && retType.Name != "any" && !c.blockReturns(d.Body) {
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
		// All cases must return, and there must be a wildcard
		hasWildcard := false
		for _, mc := range s.Cases {
			if mc.Pattern == nil {
				hasWildcard = true
			}
			if !c.blockReturns(mc.Body) {
				return false
			}
		}
		return hasWildcard
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
	for _, m := range d.Methods {
		c.checkMethodDecl(m, d.Fields)
	}
}

func (c *V2Checker) checkMethodDecl(m *parser.MethodDecl, fields []*parser.FieldDecl) {
	inner := newV2Scope(c.scope)
	// Add fields to scope
	for _, f := range fields {
		inner.set(f.Name, c.resolveTypeExpr(f.Type))
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
			// Check return value matches declared return type
			if c.fnReturnType != nil && c.fnReturnType.Name != "any" && valType.Name != "any" {
				// For Result[T], Err() returns are always valid
				if c.isResultReturn(c.fnReturnType) && c.isErrCall(s.Value) {
					// OK — Err("msg") is valid for Result[T]
				} else if !c.compatible(*c.fnReturnType, valType) {
					c.errorf(s.Line, "return type mismatch: expected %s, got %s", c.fnReturnType, valType)
				}
			}
		}
	case *parser.IfStmt:
		c.inferType(s.Cond)
		// Type narrowing: if x is Type, narrow x in then-branch
		narrowedScope := c.tryNarrow(s.Cond)
		if narrowedScope != nil {
			prevScope := c.scope
			c.scope = narrowedScope
			c.checkBlock(s.Then)
			c.scope = prevScope
		} else {
			c.checkBlock(s.Then)
		}
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
		c.inferType(s.Value)
		for _, name := range s.Names {
			c.scope.set(name, typeAny)
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
			inner.set(r.Name, typeAny)
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

		if s.Type != nil && !c.compatible(declaredType, valType) {
			c.errorf(s.Line, "type mismatch: variable %q declared as %s but assigned %s",
				s.Name, declaredType, valType)
		}

		if s.Type != nil {
			c.scope.set(s.Name, declaredType)
		} else {
			c.scope.set(s.Name, valType)
			// When using var with or handler, store the resolved type name
			// so codegen can emit the correct Java type instead of Object
			if s.OrHandler != nil && valType.Name != "" && valType.Name != "any" {
				s.ResolvedType = valType.Name
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
		if _, found := c.scope.lookup(ident.Name); !found {
			c.errorf(s.Line, "undefined variable %q", ident.Name)
		}
	}
	c.inferType(s.Value)
}

// --- Type inference ----------------------------------------------------------

func (c *V2Checker) inferType(e parser.Expr) V2Type {
	if e == nil {
		return typeNull
	}
	switch e := e.(type) {
	case *parser.IntLit:
		return typeInt
	case *parser.FloatLit:
		return typeDouble
	case *parser.StringLit, *parser.StringInterpLit, *parser.RawStringLit:
		return typeStr
	case *parser.BoolLit:
		return typeBool
	case *parser.NullLit:
		return typeNull
	case *parser.Ident:
		if t, found := c.scope.lookup(e.Name); found {
			return t
		}
		// Built-in functions — don't error
		return typeAny
	case *parser.BinaryExpr:
		left := c.inferType(e.Left)
		right := c.inferType(e.Right)
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
				// Check arg count (skip if *args/**kwargs)
				if len(argTypes) != len(sig.Params) && len(sig.Params) > 0 {
					hasVariadic := false
					for _, pn := range sig.ParamNames {
						if strings.HasPrefix(pn, "**") || strings.HasPrefix(pn, "*") {
							hasVariadic = true
						}
					}
					if !hasVariadic && len(argTypes) > len(sig.Params) {
						c.errorf(0, "function %q expects %d args, got %d", ident.Name, len(sig.Params), len(argTypes))
					}
				}
				// Check arg types
				for i, argT := range argTypes {
					if i < len(sig.Params) && !c.compatible(sig.Params[i], argT) {
						c.errorf(0, "argument %d of %q: expected %s, got %s",
							i+1, ident.Name, sig.Params[i], argT)
					}
				}
				return sig.ReturnType
			}
		}
		return typeAny
	case *parser.SelectorExpr:
		objType := c.inferType(e.Object)
		// Try to resolve field/method type from Java class
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
		c.inferType(e.Object)
		c.inferType(e.Index)
		return typeAny
	case *parser.ListLit:
		for _, el := range e.Elements {
			c.inferType(el)
		}
		return V2Type{Name: "List"}
	case *parser.MapLit:
		for i := range e.Keys {
			c.inferType(e.Keys[i])
			c.inferType(e.Values[i])
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

// tryNarrow checks if a condition narrows a variable's type.
// Supports: x is Type, isinstance(x, Type)
// Returns a new scope with the narrowed type, or nil.
func (c *V2Checker) tryNarrow(cond parser.Expr) *V2Scope {
	switch e := cond.(type) {
	case *parser.BinaryExpr:
		// x is Type → narrow x to Type
		if e.Op == "is" {
			if ident, ok := e.Left.(*parser.Ident); ok {
				if typeIdent, ok := e.Right.(*parser.Ident); ok {
					narrowed := newV2Scope(c.scope)
					narrowed.set(ident.Name, V2Type{Name: typeIdent.Name})
					return narrowed
				}
			}
		}
	case *parser.CallExpr:
		// isinstance(x, Type) → narrow x to Type
		if callee, ok := e.Callee.(*parser.Ident); ok && callee.Name == "isinstance" {
			if len(e.Args) == 2 {
				if ident, ok := e.Args[0].(*parser.Ident); ok {
					if typeIdent, ok := e.Args[1].(*parser.Ident); ok {
						narrowed := newV2Scope(c.scope)
						narrowed.set(ident.Name, V2Type{Name: typeIdent.Name})
						return narrowed
					}
				}
			}
		}
	}
	return nil
}

// isResultReturn checks if a return type is Result[T].
func (c *V2Checker) isResultReturn(t *V2Type) bool {
	return t != nil && t.Name == "Result"
}

// isErrCall checks if an expression is a call to Err().
func (c *V2Checker) isErrCall(e parser.Expr) bool {
	call, ok := e.(*parser.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Callee.(*parser.Ident)
	return ok && ident.Name == "Err"
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
	// int → float is OK
	if declared.Name == "double" && actual.Name == "int" {
		return true
	}
	// Array type is compatible with List literal ([1, 2, 3] can init int[])
	if strings.HasSuffix(declared.Name, "[]") && actual.Name == "List" {
		return true
	}
	// none is compatible with Optional
	if actual.Name == "null" && declared.Nullable {
		return true
	}
	// null is compatible with any reference type (non-primitive)
	if actual.Name == "null" {
		switch declared.Name {
		case "int", "long", "double", "float", "boolean", "char", "byte", "short":
			return false // primitives can't be null
		default:
			return true // reference types accept null
		}
	}
	// Check if actual type implements/extends declared type
	if parents, ok := c.parentTypes[actual.Name]; ok {
		for _, p := range parents {
			if p == declared.Name {
				return true
			}
		}
	}
	return false
}

// --- Type resolution ---------------------------------------------------------

func (c *V2Checker) resolveTypeExpr(t parser.TypeExpr) V2Type {
	if t == nil {
		return typeAny
	}
	switch t := t.(type) {
	case *parser.SimpleType:
		return V2Type{Name: t.Name}
	case *parser.GenericType:
		var args []V2Type
		for _, a := range t.TypeArgs {
			args = append(args, c.resolveTypeExpr(a))
		}
		return V2Type{Name: t.Name, Args: args}
	case *parser.ArrayType:
		elem := c.resolveTypeExpr(t.ElementType)
		return V2Type{Name: elem.Name + "[]", Args: elem.Args}
	case *parser.OptionalType:
		inner := c.resolveTypeExpr(t.Inner)
		inner.Nullable = true
		return inner
	default:
		return typeAny
	}
}
