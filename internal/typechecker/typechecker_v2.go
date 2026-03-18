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

	"zinc/internal/parser"
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
	Name     string   // "int", "str", "list", "dict", "none", "any"
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
	typeInt    = V2Type{Name: "int"}
	typeFloat  = V2Type{Name: "float"}
	typeStr    = V2Type{Name: "str"}
	typeBool   = V2Type{Name: "bool"}
	typeNone   = V2Type{Name: "none"}
	typeAny    = V2Type{Name: "any"}
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
	inLoop       bool               // tracking if inside a loop for break/continue
}

// CheckV2 runs the v2 type checker on a parsed program.
// Returns errors found. Empty slice = all good.
func CheckV2(prog *parser.Program) []V2Error {
	c := &V2Checker{
		scope:  newV2Scope(nil),
		fnSigs: make(map[string]V2FnSig),
	}

	// Register top-level declarations
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
	case *parser.DataClassDecl:
		c.scope.set(d.Name, V2Type{Name: d.Name})
	case *parser.EnumDecl:
		c.scope.set(d.Name, V2Type{Name: d.Name})
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
	}

	c.fnReturnType = nil
	c.scope = prevScope
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
			inner.set(s.Item, typeAny)
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
	case *parser.TryStmt:
		c.checkBlock(s.Body)
		if s.CatchBody != nil {
			inner := newV2Scope(c.scope)
			if s.CatchName != "" {
				inner.set(s.CatchName, typeAny)
			}
			prevScope := c.scope
			c.scope = inner
			c.checkBlock(s.CatchBody)
			c.scope = prevScope
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
	case *parser.YieldStmt:
		if s.Value != nil {
			c.inferType(s.Value)
		}
	case *parser.AssertStmt:
		c.inferType(s.Cond)
	case *parser.DelStmt:
		c.inferType(s.Target)
	case *parser.RaiseStmt:
		c.inferType(s.Value)
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
		return typeNone
	}
	switch e := e.(type) {
	case *parser.IntLit:
		return typeInt
	case *parser.FloatLit:
		return typeFloat
	case *parser.StringLit, *parser.StringInterpLit, *parser.RawStringLit:
		return typeStr
	case *parser.BoolLit:
		return typeBool
	case *parser.NullLit:
		return typeNone
	case *parser.Ident:
		if t, found := c.scope.lookup(e.Name); found {
			return t
		}
		// Built-in functions — don't error
		return typeAny
	case *parser.BinaryExpr:
		left := c.inferType(e.Left)
		right := c.inferType(e.Right)
		return c.inferBinaryType(e.Op, left, right)
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
		c.inferType(e.Object)
		return typeAny
	case *parser.IndexExpr:
		c.inferType(e.Object)
		c.inferType(e.Index)
		return typeAny
	case *parser.ListLit:
		for _, el := range e.Elements {
			c.inferType(el)
		}
		return V2Type{Name: "list"}
	case *parser.MapLit:
		for i := range e.Keys {
			c.inferType(e.Keys[i])
			c.inferType(e.Values[i])
		}
		return V2Type{Name: "dict"}
	case *parser.LambdaExpr:
		return typeAny
	case *parser.IfExpr:
		c.inferType(e.Cond)
		thenType := c.inferType(e.Then)
		c.inferType(e.Else)
		return thenType
	case *parser.ComprehensionExpr:
		return V2Type{Name: "list"}
	case *parser.DictComprehensionExpr:
		return V2Type{Name: "dict"}
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
	default:
		return typeAny
	}
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
		if left.Name == "float" || right.Name == "float" {
			return typeFloat
		}
		if left.Name == "int" && right.Name == "int" {
			return typeInt
		}
		if left.Name == "str" && op == "+" {
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
	if declared.Name == "float" && actual.Name == "int" {
		return true
	}
	// none is compatible with Optional
	if actual.Name == "none" && declared.Nullable {
		return true
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
	case *parser.OptionalType:
		inner := c.resolveTypeExpr(t.Inner)
		inner.Nullable = true
		return inner
	default:
		return typeAny
	}
}
