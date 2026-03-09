// Package typechecker performs semantic analysis on Zinc ASTs.
// It reports type errors with source line/column information before
// code generation, producing better error messages than the Go compiler.
package typechecker

import (
	"fmt"

	"zinc/internal/parser"
)

// TypeError describes a type-checking error with source location.
type TypeError struct {
	Line int
	Col  int
	Msg  string
}

func (e TypeError) String() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

// --- Scope -------------------------------------------------------------------

// Scope is a lexical variable scope that chains to its parent.
type Scope struct {
	vars   map[string]Type
	parent *Scope
}

func newScope(parent *Scope) *Scope {
	return &Scope{vars: make(map[string]Type), parent: parent}
}

func (s *Scope) define(name string, t Type) {
	s.vars[name] = t
}

func (s *Scope) lookup(name string) (Type, bool) {
	if t, ok := s.vars[name]; ok {
		return t, true
	}
	if s.parent != nil {
		return s.parent.lookup(name)
	}
	return nil, false
}

// --- Checker -----------------------------------------------------------------

// Checker holds all state for type-checking a set of programs.
type Checker struct {
	errors            []TypeError
	classes           map[string]*ClassType
	interfaces        map[string]*InterfaceType
	enums             map[string]*EnumType
	fns               map[string]*FnSig // top-level functions
	scope             *Scope
	currentReturnType Type       // TypeVoid outside any fn
	currentClass      *ClassType // non-nil inside a method body
	currentTypeParams map[string]bool
}

func newChecker() *Checker {
	return &Checker{
		classes:           make(map[string]*ClassType),
		interfaces:        make(map[string]*InterfaceType),
		enums:             make(map[string]*EnumType),
		fns:               make(map[string]*FnSig),
		scope:             newScope(nil),
		currentReturnType: TypeVoid,
		currentTypeParams: make(map[string]bool),
	}
}

func (c *Checker) errorf(line, col int, format string, args ...any) {
	c.errors = append(c.errors, TypeError{
		Line: line,
		Col:  col,
		Msg:  fmt.Sprintf(format, args...),
	})
}

func (c *Checker) pushScope() {
	c.scope = newScope(c.scope)
}

func (c *Checker) popScope() {
	if c.scope.parent != nil {
		c.scope = c.scope.parent
	}
}

// --- Entry points ------------------------------------------------------------

// Check type-checks a single program and returns any errors.
func Check(prog *parser.Program) []TypeError {
	c := newChecker()
	c.prePass([]*parser.Program{prog})
	c.checkProgram(prog)
	return c.errors
}

// CheckAll type-checks multiple programs (one package) together.
func CheckAll(progs []*parser.Program) []TypeError {
	c := newChecker()
	c.prePass(progs)
	for _, prog := range progs {
		c.checkProgram(prog)
	}
	return c.errors
}

// --- Pre-pass: collect declarations ------------------------------------------

func (c *Checker) prePass(progs []*parser.Program) {
	// Pass 0: register imported package names as TypeUnknown in root scope
	for _, prog := range progs {
		for _, imp := range prog.Imports {
			alias := imp.Alias
			if alias == "" {
				alias = pkgLastSegment(imp.Path)
			}
			c.scope.define(alias, TypeUnknown)
		}
	}

	// Pass 1: build class/interface/enum type skeletons (names only)
	for _, prog := range progs {
		for _, decl := range prog.Decls {
			switch d := decl.(type) {
			case *parser.ClassDecl:
				ct := &ClassType{
					Name:       d.Name,
					TypeParams: d.TypeParams,
					Parents:    d.Parents,
					Fields:     make(map[string]Type),
					Methods:    make(map[string]*FnSig),
				}
				c.classes[d.Name] = ct
			case *parser.InterfaceDecl:
				it := &InterfaceType{
					Name:    d.Name,
					Methods: make(map[string]*FnSig),
				}
				c.interfaces[d.Name] = it
			case *parser.EnumDecl:
				et := &EnumType{
					Name:     d.Name,
					Variants: d.Variants,
				}
				c.enums[d.Name] = et
			}
		}
	}

	// Pass 2: resolve fields, method sigs, fn sigs (now named types exist)
	for _, prog := range progs {
		for _, decl := range prog.Decls {
			switch d := decl.(type) {
			case *parser.ClassDecl:
				ct := c.classes[d.Name]
				// Activate type params for resolution
				saved := c.currentTypeParams
				c.currentTypeParams = make(map[string]bool)
				for _, tp := range d.TypeParams {
					c.currentTypeParams[tp] = true
				}
				// Fields
				for _, f := range d.Fields {
					ct.Fields[f.Name] = c.resolveTypeExpr(f.Type)
				}
				// Ctor
				if d.Ctor != nil {
					params := make([]Type, len(d.Ctor.Params))
					for i, p := range d.Ctor.Params {
						params[i] = c.resolveTypeExpr(p.Type)
					}
					ct.Ctor = &FnSig{
					Params:     params,
					ParamNames: paramNamesFrom(d.Ctor.Params),
					HasDefault: hasDefaultsFrom(d.Ctor.Params),
					Return:     ct,
				}
				}
				// Methods
				for _, m := range d.Methods {
					params := make([]Type, len(m.Params))
					for i, p := range m.Params {
						params[i] = c.resolveTypeExpr(p.Type)
					}
					ret := TypeVoid
					if m.ReturnType != nil {
						ret = c.resolveTypeExpr(m.ReturnType)
					}
					ct.Methods[m.Name] = &FnSig{
					Params:     params,
					ParamNames: paramNamesFrom(m.Params),
					HasDefault: hasDefaultsFrom(m.Params),
					Return:     ret,
					CanThrow:   m.CanThrow,
				}
				}
				c.currentTypeParams = saved

			case *parser.InterfaceDecl:
				it := c.interfaces[d.Name]
				for _, m := range d.Methods {
					params := make([]Type, len(m.Params))
					for i, p := range m.Params {
						params[i] = c.resolveTypeExpr(p.Type)
					}
					ret := TypeVoid
					if m.ReturnType != nil {
						ret = c.resolveTypeExpr(m.ReturnType)
					}
					it.Methods[m.Name] = &FnSig{Params: params, Return: ret}
				}

			case *parser.FnDecl:
				saved := c.currentTypeParams
				c.currentTypeParams = make(map[string]bool)
				for _, tp := range d.TypeParams {
					c.currentTypeParams[tp] = true
				}
				params := make([]Type, len(d.Params))
				for i, p := range d.Params {
					params[i] = c.resolveTypeExpr(p.Type)
				}
				ret := TypeVoid
				if d.ReturnType != nil {
					ret = c.resolveTypeExpr(d.ReturnType)
				}
				c.fns[d.Name] = &FnSig{
					TypeParams: d.TypeParams,
					Params:     params,
					ParamNames: paramNamesFrom(d.Params),
					HasDefault: hasDefaultsFrom(d.Params),
					Return:     ret,
					CanThrow:   d.CanThrow,
				}
				c.currentTypeParams = saved
			}
		}
	}
}

// --- Program-level checking --------------------------------------------------

func (c *Checker) checkProgram(prog *parser.Program) {
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *parser.FnDecl:
			c.checkFnDecl(d)
		case *parser.ClassDecl:
			c.checkClassDecl(d)
		case *parser.EnumDecl:
			c.checkEnumDecl(d)
		case *parser.ConstDecl:
			c.checkConstDecl(d)
		}
	}
}

func (c *Checker) checkFnDecl(d *parser.FnDecl) {
	// Activate type params
	saved := c.currentTypeParams
	c.currentTypeParams = make(map[string]bool)
	for _, tp := range d.TypeParams {
		c.currentTypeParams[tp] = true
	}

	// Determine return type
	retType := TypeVoid
	if d.ReturnType != nil {
		retType = c.resolveTypeExpr(d.ReturnType)
	}
	savedReturn := c.currentReturnType
	c.currentReturnType = retType

	// Push scope and define params
	c.pushScope()
	for _, p := range d.Params {
		c.scope.define(p.Name, c.resolveTypeExpr(p.Type))
	}

	c.checkBlock(d.Body)

	c.popScope()
	c.currentReturnType = savedReturn
	c.currentTypeParams = saved
}

func (c *Checker) checkClassDecl(d *parser.ClassDecl) {
	ct := c.classes[d.Name]
	if ct == nil {
		return
	}

	// Check parents exist
	for _, parent := range d.Parents {
		if _, ok := c.classes[parent]; ok {
			continue
		}
		if _, ok := c.interfaces[parent]; ok {
			continue
		}
		c.errorf(0, 0, "undefined class/interface %q (used as parent of %s)", parent, d.Name)
	}

	// Activate type params
	saved := c.currentTypeParams
	c.currentTypeParams = make(map[string]bool)
	for _, tp := range d.TypeParams {
		c.currentTypeParams[tp] = true
	}
	savedClass := c.currentClass
	c.currentClass = ct

	// Check ctor
	if d.Ctor != nil {
		savedReturn := c.currentReturnType
		c.currentReturnType = TypeVoid // ctor doesn't return
		c.pushScope()
		// 'this' in scope
		c.scope.define("this", ct)
		for _, p := range d.Ctor.Params {
			c.scope.define(p.Name, c.resolveTypeExpr(p.Type))
		}
		// Define fields in ctor scope
		for fname, ftype := range ct.Fields {
			c.scope.define(fname, ftype)
		}
		c.checkBlock(d.Ctor.Body)
		c.popScope()
		c.currentReturnType = savedReturn
	}

	// Check methods
	for _, m := range d.Methods {
		sig := ct.Methods[m.Name]
		retType := TypeVoid
		if sig != nil {
			retType = sig.Return
		}
		savedReturn := c.currentReturnType
		c.currentReturnType = retType

		c.pushScope()
		c.scope.define("this", ct)
		// Field access via 'this' — define fields in method scope too
		for fname, ftype := range ct.Fields {
			c.scope.define(fname, ftype)
		}
		for _, p := range m.Params {
			c.scope.define(p.Name, c.resolveTypeExpr(p.Type))
		}
		c.checkBlock(m.Body)
		c.popScope()
		c.currentReturnType = savedReturn
	}

	c.currentClass = savedClass
	c.currentTypeParams = saved
}

func (c *Checker) checkEnumDecl(d *parser.EnumDecl) {
	// Nothing structural to check beyond pre-pass
}

// --- Statement checking ------------------------------------------------------

func (c *Checker) checkBlock(b *parser.BlockStmt) {
	if b == nil {
		return
	}
	c.pushScope()
	for _, s := range b.Stmts {
		c.checkStmt(s)
	}
	c.popScope()
}

func (c *Checker) checkStmt(stmt parser.Stmt) {
	switch s := stmt.(type) {
	case *parser.VarStmt:
		c.checkVarStmt(s)
	case *parser.TupleVarStmt:
		c.checkTupleVarStmt(s)
	case *parser.AssignStmt:
		c.checkAssignStmt(s)
	case *parser.ReturnStmt:
		c.checkReturnStmt(s)
	case *parser.IfStmt:
		c.checkIfStmt(s)
	case *parser.WhileStmt:
		c.checkWhileStmt(s)
	case *parser.ForStmt:
		c.checkForStmt(s)
	case *parser.BlockStmt:
		c.checkBlock(s)
	case *parser.GoStmt:
		c.pushScope()
		for _, st := range s.Body.Stmts {
			c.checkStmt(st)
		}
		c.popScope()
	case *parser.MatchStmt:
		c.checkMatchStmt(s)
	case *parser.PrintStmt:
		c.inferExpr(s.Value) // any type is OK
	case *parser.ExprStmt:
		c.inferExpr(s.Expr)
		c.checkOrHandler(s.OrHandler)
	case *parser.WithStmt:
		c.pushScope()
		for _, r := range s.Resources {
			c.inferExpr(r.Value)
			c.scope.define(r.Name, TypeAny)
			c.checkOrHandler(r.OrHandler)
		}
		for _, st := range s.Body.Stmts {
			c.checkStmt(st)
		}
		c.popScope()
	case *parser.ListAddStmt:
		c.inferExpr(s.List)
		c.inferExpr(s.Value)
	case *parser.MapRemoveStmt:
		c.inferExpr(s.Map)
		c.inferExpr(s.Key)
	case *parser.ListSortStmt:
		c.inferExpr(s.List)
	case *parser.BreakStmt, *parser.ContinueStmt:
		// nothing to check
	}
}

func (c *Checker) checkConstDecl(d *parser.ConstDecl) {
	valType := c.inferExpr(d.Value)
	if d.Type != nil {
		declaredType := c.resolveTypeExpr(d.Type)
		if !Assignable(valType, declaredType) {
			c.errorf(0, 0, "type mismatch: cannot assign %s to const %s of type %s", valType, d.Name, declaredType)
		}
		c.scope.define(d.Name, declaredType)
	} else {
		c.scope.define(d.Name, valType)
	}
}

func (c *Checker) checkVarStmt(s *parser.VarStmt) {
	var inferredType Type = TypeUnknown
	if s.Value != nil {
		inferredType = c.inferExpr(s.Value)
	}

	var declaredType Type
	if s.Type != nil {
		declaredType = c.resolveTypeExpr(s.Type)
	}

	if declaredType != nil && s.Value != nil {
		if !Assignable(inferredType, declaredType) {
			c.errorf(0, 0, "type mismatch: cannot assign %s to %s", inferredType, declaredType)
		}
		// Propagate declared type to empty literals
		if ll, ok := s.Value.(*parser.ListLit); ok && ll.ResolvedType == "" {
			ll.ResolvedType = TypeToGoString(declaredType)
		}
		if ml, ok := s.Value.(*parser.MapLit); ok && ml.ResolvedType == "" {
			ml.ResolvedType = TypeToGoString(declaredType)
		}
		c.scope.define(s.Name, declaredType)
	} else if declaredType != nil {
		c.scope.define(s.Name, declaredType)
	} else {
		c.scope.define(s.Name, inferredType)
	}
	c.checkOrHandler(s.OrHandler)
}

func (c *Checker) checkTupleVarStmt(s *parser.TupleVarStmt) {
	c.inferExpr(s.Value)
	// Define all names as TypeUnknown — no false positives for tuple returns
	for _, name := range s.Names {
		c.scope.define(name, TypeUnknown)
	}
}

func (c *Checker) checkAssignStmt(s *parser.AssignStmt) {
	valType := c.inferExpr(s.Value)

	// Infer target type from scope if it's a simple identifier
	if id, ok := s.Target.(*parser.Ident); ok {
		if targetType, found := c.scope.lookup(id.Name); found {
			if s.Op == "=" && !Assignable(valType, targetType) {
				c.errorf(0, 0, "type mismatch: cannot assign %s to %s", valType, targetType)
			}
		}
		// If not found in scope, it might be a new assignment — no error
		return
	}
	// For selector/index targets, just check value type
	c.inferExpr(s.Target)
	c.checkOrHandler(s.OrHandler)
}

func (c *Checker) checkReturnStmt(s *parser.ReturnStmt) {
	if s.Value == nil {
		if c.currentReturnType != TypeVoid && c.currentReturnType != TypeUnknown {
			c.errorf(0, 0, "return type mismatch: expected %s, got Void", c.currentReturnType)
		}
		return
	}
	retType := c.inferExpr(s.Value)
	if !Assignable(retType, c.currentReturnType) {
		c.errorf(0, 0, "return type mismatch: expected %s, got %s", c.currentReturnType, retType)
	}
}

func (c *Checker) checkIfStmt(s *parser.IfStmt) {
	condType := c.inferExpr(s.Cond)
	if condType != TypeUnknown && !TypeEqual(condType, TypeBool) {
		c.errorf(0, 0, "condition must be Bool, got %s", condType)
	}
	c.checkBlock(s.Then)
	if s.ElseStmt != nil {
		c.checkStmt(s.ElseStmt)
	}
}

func (c *Checker) checkWhileStmt(s *parser.WhileStmt) {
	condType := c.inferExpr(s.Cond)
	if condType != TypeUnknown && !TypeEqual(condType, TypeBool) {
		c.errorf(0, 0, "condition must be Bool, got %s", condType)
	}
	c.checkBlock(s.Body)
}

func (c *Checker) checkForStmt(s *parser.ForStmt) {
	if s.IsRange {
		// Range-style: for item in list { }
		c.pushScope()
		if s.IndexVar != "" {
			c.scope.define(s.IndexVar, TypeUnknown) // key/index variable
		}
		c.scope.define(s.Item, TypeUnknown) // permissive
		c.inferExpr(s.Range)
		for _, st := range s.Body.Stmts {
			c.checkStmt(st)
		}
		c.popScope()
		return
	}
	// C-style: for (init; cond; post) { }
	c.pushScope()
	if s.Init != nil {
		c.checkStmt(s.Init)
	}
	if s.Cond != nil {
		condType := c.inferExpr(s.Cond)
		if condType != TypeUnknown && !TypeEqual(condType, TypeBool) {
			c.errorf(0, 0, "condition must be Bool, got %s", condType)
		}
	}
	if s.Post != nil {
		c.checkStmt(s.Post)
	}
	for _, st := range s.Body.Stmts {
		c.checkStmt(st)
	}
	c.popScope()
}

// checkOrHandler validates an or { } handler block.
// The implicit `err` variable is defined as String in the handler scope.
func (c *Checker) checkOrHandler(h *parser.OrHandler) {
	if h == nil {
		return
	}
	c.pushScope()
	c.scope.define("err", TypeString)
	for _, st := range h.Body.Stmts {
		c.checkStmt(st)
	}
	c.popScope()
}

func (c *Checker) checkMatchStmt(s *parser.MatchStmt) {
	c.inferExpr(s.Subject)
	for _, mc := range s.Cases {
		if mc.Pattern != nil {
			c.inferExpr(mc.Pattern)
		}
		c.checkBlock(mc.Body)
	}
}

// --- Expression inference ----------------------------------------------------

func (c *Checker) inferExpr(expr parser.Expr) Type {
	if expr == nil {
		return TypeUnknown
	}
	switch e := expr.(type) {
	case *parser.IntLit:
		return TypeInt
	case *parser.FloatLit:
		return TypeFloat
	case *parser.StringLit:
		return TypeString
	case *parser.StringInterpLit:
		for _, part := range e.Parts {
			c.inferExpr(part)
		}
		return TypeString
	case *parser.BoolLit:
		return TypeBool
	case *parser.NullLit:
		return TypeNull
	case *parser.Ident:
		return c.inferIdent(e)
	case *parser.BinaryExpr:
		return c.inferBinary(e)
	case *parser.UnaryExpr:
		return c.inferUnary(e)
	case *parser.CallExpr:
		return c.inferCall(e)
	case *parser.SelectorExpr:
		return c.inferSelector(e)
	case *parser.SafeNavExpr:
		return c.inferSafeNav(e)
	case *parser.IndexExpr:
		return c.inferIndex(e)
	case *parser.ListLit:
		return c.inferListLit(e)
	case *parser.MapLit:
		return c.inferMapLit(e)
	case *parser.ThisExpr:
		return c.inferThis()
	case *parser.ReceiveExpr:
		return c.inferReceive(e)
	case *parser.SendExpr:
		c.inferSend(e)
		return TypeVoid
	case *parser.SuperCallExpr:
		for _, arg := range e.Args {
			c.inferExpr(arg)
		}
		return TypeVoid
	case *parser.LambdaExpr:
		// Phase 1: treat lambda as TypeUnknown; still check body for errors
		savedReturn := c.currentReturnType
		if e.ReturnType != nil {
			c.currentReturnType = c.resolveTypeExpr(e.ReturnType)
		} else {
			c.currentReturnType = TypeVoid
		}
		c.pushScope()
		for _, param := range e.Params {
			c.scope.define(param.Name, c.resolveTypeExpr(param.Type))
		}
		if e.Body != nil {
			c.checkBlock(e.Body)
		} else if e.Expr != nil {
			c.inferExpr(e.Expr)
		}
		c.popScope()
		c.currentReturnType = savedReturn
		return TypeUnknown
	case *parser.TypeAssertExpr:
		c.inferExpr(e.Object)
		if e.IsCheck {
			return TypeBool
		}
		return c.resolveSimpleName(e.TypeName)
	case *parser.SizeExpr:
		c.inferExpr(e.Object)
		return TypeInt
	case *parser.CloneExpr:
		return c.inferExpr(e.Object)
	case *parser.ListAddStmt:
		c.inferExpr(e.List)
		c.inferExpr(e.Value)
		return TypeVoid
	case *parser.MapRemoveStmt:
		c.inferExpr(e.Map)
		c.inferExpr(e.Key)
		return TypeVoid
	case *parser.StringUpperExpr:
		c.inferExpr(e.Object)
		return TypeString
	case *parser.StringLowerExpr:
		c.inferExpr(e.Object)
		return TypeString
	case *parser.StringContainsExpr:
		c.inferExpr(e.Object)
		c.inferExpr(e.Search)
		return TypeBool
	case *parser.StringStartsWithExpr:
		c.inferExpr(e.Object)
		c.inferExpr(e.Prefix)
		return TypeBool
	case *parser.StringEndsWithExpr:
		c.inferExpr(e.Object)
		c.inferExpr(e.Suffix)
		return TypeBool
	case *parser.StringTrimExpr:
		c.inferExpr(e.Object)
		return TypeString
	case *parser.StringSplitExpr:
		c.inferExpr(e.Object)
		c.inferExpr(e.Sep)
		return &ListType{Elem: TypeString}
	case *parser.StringReplaceExpr:
		c.inferExpr(e.Object)
		c.inferExpr(e.Old)
		c.inferExpr(e.New)
		return TypeString
	case *parser.ListJoinExpr:
		c.inferExpr(e.Object)
		c.inferExpr(e.Sep)
		return TypeString
	case *parser.ListSortStmt:
		c.inferExpr(e.List)
		return TypeVoid
	case *parser.MapKeysExpr:
		c.inferExpr(e.Object)
		return TypeUnknown
	case *parser.MapValuesExpr:
		c.inferExpr(e.Object)
		return TypeUnknown
	case *parser.MapContainsExpr:
		c.inferExpr(e.Object)
		c.inferExpr(e.Key)
		return TypeBool
	}
	return TypeUnknown
}

func (c *Checker) inferIdent(e *parser.Ident) Type {
	if t, ok := c.scope.lookup(e.Name); ok {
		return t
	}
	// Check top-level functions
	if sig, ok := c.fns[e.Name]; ok {
		return sig.Return // treat fn reference as its return type for now
	}
	// Check enum variants / enum names
	for _, et := range c.enums {
		if et.Name == e.Name {
			return et
		}
		for _, v := range et.Variants {
			if v == e.Name {
				return et
			}
		}
	}
	// Check class names (used as type reference)
	if ct, ok := c.classes[e.Name]; ok {
		return ct
	}
	c.errorf(0, 0, "undefined variable %q", e.Name)
	return TypeUnknown
}

func (c *Checker) inferBinary(e *parser.BinaryExpr) Type {
	leftType := c.inferExpr(e.Left)
	rightType := c.inferExpr(e.Right)

	if leftType == TypeUnknown || rightType == TypeUnknown {
		return TypeUnknown
	}

	switch e.Op {
	case "+":
		if leftType == TypeString && rightType == TypeString {
			return TypeString
		}
		if isNumeric(leftType) && isNumeric(rightType) {
			return widerNumeric(leftType, rightType)
		}
		c.errorf(0, 0, "type mismatch: operator + not applicable to %s and %s", leftType, rightType)
		return TypeUnknown
	case "-", "*", "/", "%":
		if isNumeric(leftType) && isNumeric(rightType) {
			return widerNumeric(leftType, rightType)
		}
		c.errorf(0, 0, "type mismatch: operator %s not applicable to %s and %s", e.Op, leftType, rightType)
		return TypeUnknown
	case "==", "!=":
		if !TypeEqual(leftType, rightType) {
			c.errorf(0, 0, "type mismatch: cannot compare %s and %s", leftType, rightType)
		}
		return TypeBool
	case "<", "<=", ">", ">=":
		if (isNumeric(leftType) && isNumeric(rightType)) ||
			(leftType == TypeString && rightType == TypeString) {
			return TypeBool
		}
		c.errorf(0, 0, "type mismatch: operator %s not applicable to %s and %s", e.Op, leftType, rightType)
		return TypeUnknown
	case "&&", "||":
		if !TypeEqual(leftType, TypeBool) {
			c.errorf(0, 0, "type mismatch: operator %s requires Bool, got %s", e.Op, leftType)
		}
		if !TypeEqual(rightType, TypeBool) {
			c.errorf(0, 0, "type mismatch: operator %s requires Bool, got %s", e.Op, rightType)
		}
		return TypeBool
	}
	return TypeUnknown
}

func (c *Checker) inferUnary(e *parser.UnaryExpr) Type {
	operandType := c.inferExpr(e.Operand)
	switch e.Op {
	case "!":
		if operandType != TypeUnknown && !TypeEqual(operandType, TypeBool) {
			c.errorf(0, 0, "type mismatch: operator ! requires Bool, got %s", operandType)
		}
		return TypeBool
	case "-":
		if operandType != TypeUnknown && !isNumeric(operandType) {
			c.errorf(0, 0, "type mismatch: operator - requires numeric type, got %s", operandType)
		}
		return operandType
	}
	return TypeUnknown
}

func (c *Checker) inferCall(e *parser.CallExpr) Type {
	// SelectorExpr callee: obj.method(args) or Dog.new(args)
	if sel, ok := e.Callee.(*parser.SelectorExpr); ok {
		return c.inferMethodCall(sel, e.Args, e.NamedArgs)
	}

	// Direct function call: fn(args)
	if id, ok := e.Callee.(*parser.Ident); ok {
		return c.inferFnCall(id.Name, e.Args, e.NamedArgs)
	}

	// Other callable expression
	c.inferExpr(e.Callee)
	for _, arg := range e.Args {
		c.inferExpr(arg)
	}
	for _, na := range e.NamedArgs {
		c.inferExpr(na.Value)
	}
	return TypeUnknown
}

func (c *Checker) inferFnCall(name string, args []parser.Expr, namedArgs []parser.NamedArg) Type {
	for _, arg := range args {
		c.inferExpr(arg)
	}
	for _, na := range namedArgs {
		c.inferExpr(na.Value)
	}
	if sig, ok := c.fns[name]; ok {
		c.validateArgs(sig, name, args, namedArgs)
		return sig.Return
	}
	// Could be a built-in or imported fn — don't report error
	return TypeUnknown
}

func (c *Checker) inferMethodCall(sel *parser.SelectorExpr, args []parser.Expr, namedArgs []parser.NamedArg) Type {
	for _, arg := range args {
		c.inferExpr(arg)
	}
	for _, na := range namedArgs {
		c.inferExpr(na.Value)
	}

	// Check for ClassName.new(args) → constructor call
	if id, ok := sel.Object.(*parser.Ident); ok {
		if ct, found := c.classes[id.Name]; found && sel.Field == "new" {
			if ct.Ctor != nil && !ct.isGeneric() {
				c.validateArgs(ct.Ctor, id.Name+".new", args, namedArgs)
			}
			return ct
		}
	}

	// Handle built-in container constructors: Chan.new, List.new, Map.new
	if id, ok := sel.Object.(*parser.Ident); ok && sel.Field == "new" {
		switch id.Name {
		case "Chan":
			return &ChanType{Elem: TypeUnknown}
		case "List":
			return &ListType{Elem: TypeUnknown}
		case "Map":
			return &MapType{Key: TypeUnknown, Value: TypeUnknown}
		}
	}

	// Regular method call: obj.method(args)
	objType := c.inferExpr(sel.Object)
	if objType == TypeUnknown {
		return TypeUnknown
	}

	// Reject `.method()` on nullable types — must use `?.method()`
	if opt, ok := objType.(*OptionalType); ok {
		c.errorf(0, 0, "cannot call method %q on nullable type %s; use '?.' for safe access", sel.Field, opt)
		return TypeUnknown
	}

	if ct, ok := objType.(*ClassType); ok {
		if sig, found := c.findMethod(ct, sel.Field); found {
			c.validateArgs(sig, ct.Name+"."+sel.Field, args, namedArgs)
			return sig.Return
		}
		c.errorf(0, 0, "undefined method %q on %s", sel.Field, ct.Name)
		return TypeUnknown
	}

	if it, ok := objType.(*InterfaceType); ok {
		if sig, found := it.Methods[sel.Field]; found {
			return sig.Return
		}
		c.errorf(0, 0, "undefined method %q on %s", sel.Field, it.Name)
		return TypeUnknown
	}

	// Built-in methods on primitives (e.g. string.len(), list.push()) — no errors
	return TypeUnknown
}

func (c *Checker) inferSelector(e *parser.SelectorExpr) Type {
	objType := c.inferExpr(e.Object)
	if objType == TypeUnknown {
		return TypeUnknown
	}

	// Reject `.` on nullable types — must use `?.`
	if opt, ok := objType.(*OptionalType); ok {
		c.errorf(0, 0, "cannot access member %q on nullable type %s; use '?.' for safe access", e.Field, opt)
		return TypeUnknown
	}

	// Enum.Variant — check enum type
	if id, ok := e.Object.(*parser.Ident); ok {
		if et, found := c.enums[id.Name]; found {
			for _, v := range et.Variants {
				if v == e.Field {
					return et
				}
			}
			c.errorf(0, 0, "undefined variant %q on enum %s", e.Field, id.Name)
			return TypeUnknown
		}
	}

	if ct, ok := objType.(*ClassType); ok {
		if t, found := c.findField(ct, e.Field); found {
			return t
		}
		// It might be a method reference — check methods
		if _, found := c.findMethod(ct, e.Field); found {
			return TypeUnknown // method reference, not invoked
		}
		c.errorf(0, 0, "undefined field %q on %s", e.Field, ct.Name)
		return TypeUnknown
	}

	// Built-in field access on primitives or unknown — permissive
	return TypeUnknown
}

// inferSafeNav type-checks a safe navigation expression (e.g. obj?.field, obj?.method()).
// Rules:
//   - obj must be a nullable type (T?) — using ?. on non-nullable is an error
//   - The result is always Optional (T?) to enforce chain consistency
func (c *Checker) inferSafeNav(e *parser.SafeNavExpr) Type {
	objType := c.inferExpr(e.Object)
	if e.Call != nil {
		for _, arg := range e.Call.Args {
			c.inferExpr(arg)
		}
		for _, na := range e.Call.NamedArgs {
			c.inferExpr(na.Value)
		}
	}

	if objType == TypeUnknown || objType == TypeAny {
		return TypeUnknown
	}

	// Unwrap OptionalType to get inner type
	inner, isOptional := objType.(*OptionalType)
	if !isOptional {
		c.errorf(0, 0, "unnecessary safe call on non-null type %s; use '.' instead", objType)
		// Still resolve the field/method for downstream checking
		return c.resolveMemberType(objType, e.Field, e.Call)
	}

	// Resolve the field/method on the inner (unwrapped) type
	memberType := c.resolveMemberType(inner.Inner, e.Field, e.Call)
	if memberType == TypeUnknown || memberType == TypeVoid {
		return memberType
	}

	// Wrap the result in Optional to enforce chain consistency
	// (unless it's already optional)
	if _, alreadyOpt := memberType.(*OptionalType); alreadyOpt {
		return memberType
	}
	return &OptionalType{Inner: memberType}
}

// resolveMemberType looks up a field or method on a type and returns its type.
func (c *Checker) resolveMemberType(t Type, field string, call *parser.CallExpr) Type {
	if ct, ok := t.(*ClassType); ok {
		if call != nil {
			// Method call
			if sig, found := c.findMethod(ct, field); found {
				return sig.Return
			}
			return TypeUnknown
		}
		// Field access
		if ft, found := c.findField(ct, field); found {
			return ft
		}
		if _, found := c.findMethod(ct, field); found {
			return TypeUnknown
		}
		return TypeUnknown
	}
	if it, ok := t.(*InterfaceType); ok {
		if call != nil {
			if sig, found := it.Methods[field]; found {
				return sig.Return
			}
		}
		return TypeUnknown
	}
	return TypeUnknown
}

func (c *Checker) inferIndex(e *parser.IndexExpr) Type {
	objType := c.inferExpr(e.Object)
	c.inferExpr(e.Index)

	if lt, ok := objType.(*ListType); ok {
		return lt.Elem
	}
	if mt, ok := objType.(*MapType); ok {
		return mt.Value
	}
	return TypeUnknown
}

func (c *Checker) inferListLit(e *parser.ListLit) Type {
	if len(e.Elements) == 0 {
		return &ListType{Elem: TypeUnknown}
	}
	types := make([]Type, len(e.Elements))
	for i, el := range e.Elements {
		types[i] = c.inferExpr(el)
	}
	elem := commonType(types)
	result := &ListType{Elem: elem}
	e.ResolvedType = TypeToGoString(result)
	return result
}

func (c *Checker) inferMapLit(e *parser.MapLit) Type {
	if len(e.Keys) == 0 {
		return &MapType{Key: TypeUnknown, Value: TypeUnknown}
	}
	keyTypes := make([]Type, len(e.Keys))
	valTypes := make([]Type, len(e.Values))
	for i := range e.Keys {
		keyTypes[i] = c.inferExpr(e.Keys[i])
		valTypes[i] = c.inferExpr(e.Values[i])
	}
	result := &MapType{Key: commonType(keyTypes), Value: commonType(valTypes)}
	e.ResolvedType = TypeToGoString(result)
	return result
}

// commonType returns the unified type from a slice: all same → that type,
// TypeUnknown skipped, mismatched → TypeAny.
func commonType(types []Type) Type {
	var unified Type
	for _, t := range types {
		if t == TypeUnknown {
			continue
		}
		if unified == nil {
			unified = t
		} else if !TypeEqual(unified, t) {
			return TypeAny
		}
	}
	if unified == nil {
		return TypeUnknown
	}
	return unified
}

func (c *Checker) inferThis() Type {
	if c.currentClass == nil {
		c.errorf(0, 0, `"this" used outside of a class method`)
		return TypeUnknown
	}
	return c.currentClass
}

func (c *Checker) inferReceive(e *parser.ReceiveExpr) Type {
	chType := c.inferExpr(e.Chan)
	if ct, ok := chType.(*ChanType); ok {
		return ct.Elem
	}
	return TypeUnknown
}

func (c *Checker) inferSend(e *parser.SendExpr) {
	chType := c.inferExpr(e.Chan)
	valType := c.inferExpr(e.Value)
	if ct, ok := chType.(*ChanType); ok {
		if !Assignable(valType, ct.Elem) {
			c.errorf(0, 0, "type mismatch: cannot send %s to Chan<%s>", valType, ct.Elem)
		}
	}
}

// --- Helpers -----------------------------------------------------------------

// paramNamesFrom extracts parameter names from a ParamDecl slice.
func paramNamesFrom(params []*parser.ParamDecl) []string {
	names := make([]string, len(params))
	for i, p := range params {
		names[i] = p.Name
	}
	return names
}

// hasDefaultsFrom extracts which params have default values.
func hasDefaultsFrom(params []*parser.ParamDecl) []bool {
	result := make([]bool, len(params))
	for i, p := range params {
		result[i] = p.Default != nil
	}
	return result
}

// validateArgs validates positional and named arguments against a function signature.
// It reports errors for too many args, unknown named arg names, and missing required args.
func (c *Checker) validateArgs(sig *FnSig, callName string, args []parser.Expr, namedArgs []parser.NamedArg) {
	if sig.isGeneric() {
		return
	}
	totalProvided := len(args) + len(namedArgs)
	if totalProvided > len(sig.Params) {
		c.errorf(0, 0, "too many arguments to %s: expected at most %d, got %d",
			callName, len(sig.Params), totalProvided)
		return
	}
	// Validate named arg names
	for _, na := range namedArgs {
		found := false
		for _, pname := range sig.ParamNames {
			if pname == na.Name {
				found = true
				break
			}
		}
		if !found {
			c.errorf(0, 0, "unknown named argument %q to %s", na.Name, callName)
		}
	}
	// Check all required params are covered
	covered := make([]bool, len(sig.Params))
	for i := range args {
		if i < len(covered) {
			covered[i] = true
		}
	}
	for _, na := range namedArgs {
		for i, pname := range sig.ParamNames {
			if pname == na.Name {
				covered[i] = true
				break
			}
		}
	}
	for i := range sig.Params {
		if !covered[i] {
			isRequired := i >= len(sig.HasDefault) || !sig.HasDefault[i]
			if isRequired {
				paramName := fmt.Sprintf("param %d", i+1)
				if i < len(sig.ParamNames) {
					paramName = sig.ParamNames[i]
				}
				c.errorf(0, 0, "missing required argument %q to %s", paramName, callName)
			}
		}
	}
}

// findMethod walks the class hierarchy to find a method by name.
func (c *Checker) findMethod(ct *ClassType, name string) (*FnSig, bool) {
	if sig, ok := ct.Methods[name]; ok {
		return sig, true
	}
	for _, parent := range ct.Parents {
		if pct, ok := c.classes[parent]; ok {
			if sig, ok := c.findMethod(pct, name); ok {
				return sig, true
			}
		}
	}
	return nil, false
}

// findField walks the class hierarchy to find a field by name.
func (c *Checker) findField(ct *ClassType, name string) (Type, bool) {
	if t, ok := ct.Fields[name]; ok {
		return t, true
	}
	for _, parent := range ct.Parents {
		if pct, ok := c.classes[parent]; ok {
			if t, ok := c.findField(pct, name); ok {
				return t, true
			}
		}
	}
	return nil, false
}

func pkgLastSegment(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

func isNumeric(t Type) bool {
	return t == TypeInt || t == TypeFloat
}

func widerNumeric(a, b Type) Type {
	if a == TypeFloat || b == TypeFloat {
		return TypeFloat
	}
	return TypeInt
}

func (s *FnSig) isGeneric() bool {
	return len(s.TypeParams) > 0
}

func (c *ClassType) isGeneric() bool {
	return len(c.TypeParams) > 0
}

