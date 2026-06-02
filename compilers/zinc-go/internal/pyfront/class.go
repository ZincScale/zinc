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

package pyfront

import "zinc-go/internal/parser"

// dunderMethods maps Python special methods to the Go method names the
// front-end emits and routes to. __str__ becomes Go's Stringer (used by
// print/str); the arithmetic/comparison/len ones are routed from operators
// and len() when an operand is a known class instance.
var dunderMethods = map[string]string{
	"__str__":     "String",
	"__repr__":    "Repr",
	"__eq__":      "Eq",
	"__ne__":      "Ne",
	"__lt__":      "Lt",
	"__le__":      "Le",
	"__gt__":      "Gt",
	"__ge__":      "Ge",
	"__add__":     "Add",
	"__sub__":     "Sub",
	"__mul__":     "Mul",
	"__len__":     "Len",
	"__getitem__": "GetItem",
	"__enter__":   "Enter",
	"__exit__":    "Exit",
}

// isClassProp reports whether class cls exposes pyName as an @property.
func (p *Parser) isClassProp(cls, pyName string) bool {
	props, ok := p.classProps[cls]
	return ok && props[pyName]
}

// parseClassVar records a class-level constant `NAME [: T] = literal`. The
// value is inlined at use sites (classConst), so only literal values are
// supported; a non-literal class variable is rejected with a clear error.
func (p *Parser) parseClassVar(className string) {
	name := p.expectKind(TName).Value
	if p.acceptOp(":") {
		p.parseType() // annotation, ignored
	}
	if !p.acceptOp("=") {
		p.errf(p.cur(), "expected '=' in class variable %q", name)
		return
	}
	val := p.parseExpr()
	p.endSimple()
	if !isLiteralExpr(val) {
		p.errf(p.cur(), "non-literal class variable %q is not yet supported", name)
		return
	}
	if p.classConstVal[className] == nil {
		p.classConstVal[className] = map[string]parser.Expr{}
	}
	p.classConstVal[className][name] = val
}

// classConst returns the inlined value of a class constant accessed as
// `Class.NAME` (obj is the class name) or `self.NAME`/`instance.NAME` (obj is
// an instance), and whether it was found.
func (p *Parser) classConst(obj parser.Expr, field string) (parser.Expr, bool) {
	cls := ""
	if id, ok := obj.(*parser.Ident); ok && p.classNames[id.Name] {
		cls = id.Name // Class.NAME
	} else {
		cls = p.exprClass(obj) // self.NAME / instance.NAME
	}
	if cls == "" {
		return nil, false
	}
	if cv, ok := p.classConstVal[cls]; ok {
		if v, ok := cv[field]; ok {
			return v, true
		}
	}
	return nil, false
}

// isLiteralExpr reports whether e is a constant literal (optionally negated),
// safe to inline at multiple use sites.
func isLiteralExpr(e parser.Expr) bool {
	switch ex := e.(type) {
	case *parser.IntLit, *parser.FloatLit, *parser.StringLit, *parser.BoolLit, *parser.NullLit:
		return true
	case *parser.UnaryExpr:
		return isLiteralExpr(ex.Operand)
	}
	return false
}

// parseClass parses a Python `class` into a zinc ClassDecl. Python attributes
// are implicit (created by `self.x = ...`), whereas zinc needs explicit field
// declarations — so fields are discovered by scanning method bodies for
// self-assignments and their inferred types. `__init__` becomes the
// constructor; other defs become methods with the leading `self` dropped.
// dcField is one @dataclass field: a class-level `name: type [= default]`.
type dcField struct {
	name string
	typ  parser.TypeExpr
	def  parser.Expr
}

func (p *Parser) parseClass(decorators []string) *parser.ClassDecl {
	line := p.advance().Line // 'class'
	name := p.expectKind(TName).Value
	isDataclass := false
	for _, d := range decorators {
		if d == "dataclass" {
			isDataclass = true
		}
	}

	var parents []parser.ParentRef
	if p.acceptOp("(") {
		for !p.isOp(")") && p.cur().Kind != TEOF {
			pn := p.expectKind(TName).Value
			if pn != "object" { // Python's implicit base — no zinc equivalent
				parents = append(parents, parser.ParentRef{Name: pn})
			}
			if !p.acceptOp(",") {
				break
			}
		}
		p.expectOp(")")
	}

	p.expectOp(":")
	// An inline class body (`class E: pass`, `class E(Exception): pass`) puts the
	// suite on the header line; detect it before consuming the NEWLINE the
	// indented form requires. Inline bodies hold only simple statements.
	inline := p.cur().Kind != TNewline

	// Register the class so constructor calls (`x = Name(...)`) and typed
	// params (`p: Name`) can be tracked as instances, and so `self.field` /
	// `obj.method()` types resolve. Fields/methods fill in during the body.
	p.classNames[name] = true
	if p.classFields[name] == nil {
		p.classFields[name] = map[string]pytype{}
	}
	if p.classMethods[name] == nil {
		p.classMethods[name] = map[string]pytype{}
	}
	if p.classProps[name] == nil {
		p.classProps[name] = map[string]bool{}
	}
	if p.classMethodRet[name] == nil {
		p.classMethodRet[name] = map[string]string{}
	}
	if p.classStatics[name] == nil {
		p.classStatics[name] = map[string]bool{}
	}
	// Record the first base class so `super().method(...)` in a body lowers to
	// the embedded parent's method.
	if len(parents) > 0 {
		p.classParent[name] = parents[0].Name
	}
	prevClass := p.currentClass
	p.currentClass = name
	defer func() { p.currentClass = prevClass }()

	cls := &parser.ClassDecl{Line: line, Name: name, Parents: parents}
	var fields []*parser.FieldDecl
	seen := map[string]bool{}
	var dcFields []dcField

	// parseItem reads one class-level item (method, field, class var, pass, or
	// an unmodelled statement). Shared by the inline and indented body loops.
	parseItem := func() {
		if p.isOp("@") {
			decs := p.parseDecorators()
			if p.isKw("def") {
				p.parseClassMethod(cls, &fields, seen, decs)
			} else {
				p.errf(p.cur(), "a decorator must be followed by a def")
			}
			return
		}
		if p.isKw("def") {
			p.parseClassMethod(cls, &fields, seen, nil)
			return
		}
		// @dataclass field: `name: type [= default]` declared at class level.
		if isDataclass && p.cur().Kind == TName && p.peekOp(":") {
			dcFields = append(dcFields, p.parseDataclassField())
			return
		}
		// Class-level constant `NAME [: T] = literal` — inlined at use sites.
		if p.cur().Kind == TName && !p.isKw("pass") &&
			(p.peekOp("=") || p.peekOp(":")) {
			p.parseClassVar(name)
			return
		}
		// pass / docstrings / other class-level statements: not modelled yet.
		if p.isKw("pass") {
			p.advance()
			p.endSimple()
			return
		}
		p.parseStmt()
	}

	if inline {
		// Body shares the header line: `;`-separated simple statements, no INDENT.
		for {
			startPos := p.pos
			parseItem()
			if p.pos == startPos {
				p.advance()
			}
			if !p.isOp(";") {
				break
			}
			p.advance() // ';'
			if k := p.cur().Kind; k == TNewline || k == TEOF {
				p.endSimple()
				break
			}
		}
	} else {
		p.expectKind(TNewline)
		p.expectKind(TIndent)
		for p.cur().Kind != TDedent && p.cur().Kind != TEOF {
			p.skipNewlines()
			if p.cur().Kind == TDedent || p.cur().Kind == TEOF {
				break
			}
			startPos := p.pos
			parseItem()
			if p.pos == startPos {
				p.advance()
			}
		}
		p.expectKind(TDedent)
	}
	if isDataclass {
		p.synthesizeDataclass(cls, name, dcFields, &fields)
	}
	// Python's str() falls back to __repr__ when __str__ is absent. So when a
	// class defines __repr__ (Repr) but no __str__ (String), synthesize a
	// String() that calls Repr() — otherwise print(obj) / str(obj) would show
	// Go's default &{...} instead of the user's repr.
	if m := p.classMethods[name]; m != nil {
		if _, hasRepr := m["Repr"]; hasRepr {
			if _, hasStr := m["String"]; !hasStr {
				cls.Methods = append(cls.Methods, &parser.MethodDecl{
					Name: "String", IsPub: true, ReturnType: &parser.SimpleType{Name: "String"},
					Body: &parser.BlockStmt{Stmts: []parser.Stmt{&parser.ReturnStmt{
						Value: &parser.CallExpr{Callee: &parser.SelectorExpr{
							Object: &parser.ThisExpr{}, Field: "Repr"}}}}},
				})
				m["String"] = tStr
			}
		}
	}
	// A subclass of Exception (a builtin or another user exception) is modelled
	// as a message-carrying zincpyExc, not a Go struct: `raise E(msg)` lowers to
	// panic(zincpyExc{Type:"E", Msg:msg}) and the parent chain is registered at
	// runtime so `except Exception`/`except E` match. A custom __init__ or
	// __str__/__repr__ would change the message/identity — reject loudly rather
	// than diverge from CPython (these are the message-only common case).
	if excParent, ok := p.exceptionParent(parents); ok {
		if cls.Ctor != nil {
			p.errf(Token{Line: line}, "custom __init__ on exception subclass %q is not yet supported (message-only exceptions work)", name)
		}
		for _, m := range cls.Methods {
			if m.Name == "String" || m.Name == "Repr" {
				p.errf(Token{Line: line}, "custom __str__/__repr__ on exception subclass %q is not yet supported", name)
			}
		}
		p.excClasses[name] = excParent
		p.excOrder = append(p.excOrder, name)
		// Undo the regular-class registration so a stray `E(...)` outside a raise
		// isn't lowered to a (nonexistent) NewE constructor.
		delete(p.classNames, name)
		return nil
	}
	cls.Fields = fields
	return cls
}

// builtinExcNames is the set of CPython builtin exception type names the runtime
// hierarchy (zincpyExcParents) knows about, plus the two roots. A class deriving
// from any of these (or another user exception) is an exception subclass.
var builtinExcNames = map[string]bool{
	"BaseException": true, "Exception": true, "ValueError": true, "TypeError": true,
	"KeyError": true, "IndexError": true, "LookupError": true, "ZeroDivisionError": true,
	"ArithmeticError": true, "RuntimeError": true, "NotImplementedError": true,
	"StopIteration": true, "AttributeError": true, "OSError": true,
}

// exceptionParent returns the first base that makes this class an exception
// subclass (a builtin exception or an already-seen user exception), and true.
func (p *Parser) exceptionParent(parents []parser.ParentRef) (string, bool) {
	for _, pr := range parents {
		if builtinExcNames[pr.Name] || p.excClasses[pr.Name] != "" {
			return pr.Name, true
		}
	}
	return "", false
}

// parseDataclassField parses one `name: type [= default]` field line.
func (p *Parser) parseDataclassField() dcField {
	name := p.expectKind(TName).Value
	p.expectOp(":")
	typ := p.parseType()
	var def parser.Expr
	if p.acceptOp("=") {
		def = p.parseExpr()
	}
	p.endSimple()
	return dcField{name: name, typ: typ, def: def}
}

// synthesizeDataclass generates the constructor, struct fields, __repr__
// (String) and __eq__ (Eq) for an @dataclass from its declared fields.
func (p *Parser) synthesizeDataclass(cls *parser.ClassDecl, name string, dcFields []dcField, fields *[]*parser.FieldDecl) {
	var ctorParams []*parser.ParamDecl
	var ctorBody []parser.Stmt
	for _, f := range dcFields {
		*fields = append(*fields, &parser.FieldDecl{Name: f.name, IsPub: true, Type: f.typ})
		p.classFields[name][f.name] = typeFromExpr(f.typ)
		ctorParams = append(ctorParams, &parser.ParamDecl{Name: f.name, Type: f.typ, Default: f.def})
		ctorBody = append(ctorBody, &parser.AssignStmt{
			Target: &parser.SelectorExpr{Object: &parser.ThisExpr{}, Field: f.name},
			Op:     "=",
			Value:  &parser.Ident{Name: f.name},
		})
	}
	cls.Ctor = &parser.CtorDecl{Params: ctorParams, Body: &parser.BlockStmt{Stmts: ctorBody}}
	p.defParams[name] = ctorParams

	// __repr__ → String(): "Name(f1=<repr>, f2=<repr>)"
	repr := concatStr(&parser.StringLit{Value: name + "("})
	for i, f := range dcFields {
		sep := ""
		if i > 0 {
			sep = ", "
		}
		repr = concatStr(repr, &parser.StringLit{Value: sep + f.name + "="},
			callIdent("zincpyRepr", &parser.SelectorExpr{Object: &parser.ThisExpr{}, Field: f.name}))
	}
	repr = concatStr(repr, &parser.StringLit{Value: ")"})
	cls.Methods = append(cls.Methods, &parser.MethodDecl{
		Name: "String", IsPub: true, ReturnType: &parser.SimpleType{Name: "String"},
		Body: &parser.BlockStmt{Stmts: []parser.Stmt{&parser.ReturnStmt{Value: repr}}},
	})
	p.classMethods[name]["String"] = tStr

	// __eq__ → Eq(other): all fields equal (via zincpyEq for cross-type safety)
	var eq parser.Expr
	for _, f := range dcFields {
		cmp := callIdent("zincpyEq",
			&parser.SelectorExpr{Object: &parser.ThisExpr{}, Field: f.name},
			&parser.SelectorExpr{Object: &parser.Ident{Name: "other"}, Field: f.name})
		if eq == nil {
			eq = cmp
		} else {
			eq = &parser.BinaryExpr{Left: eq, Op: "&&", Right: cmp}
		}
	}
	if eq == nil {
		eq = &parser.BoolLit{Value: true}
	}
	cls.Methods = append(cls.Methods, &parser.MethodDecl{
		Name: "Eq", IsPub: true,
		Params:     []*parser.ParamDecl{{Name: "other", Type: &parser.SimpleType{Name: name}}},
		ReturnType: &parser.SimpleType{Name: "bool"},
		Body:       &parser.BlockStmt{Stmts: []parser.Stmt{&parser.ReturnStmt{Value: eq}}},
	})
	p.classMethods[name]["Eq"] = tBool
}

// concatStr chains string expressions with `+`.
func concatStr(parts ...parser.Expr) parser.Expr {
	e := parts[0]
	for _, part := range parts[1:] {
		e = &parser.BinaryExpr{Left: e, Op: "+", Right: part}
	}
	return e
}

// parseClassMethod parses one `def` inside a class, routing __init__ to the
// constructor and everything else to a method. The leading `self` parameter
// is dropped (zinc methods have an implicit receiver).
// parseDecorators consumes one or more `@name[(...)]` lines and returns the
// decorator base names. Decorator call arguments are skipped (not modelled).
func (p *Parser) parseDecorators() []string {
	var decs []string
	for p.isOp("@") {
		p.advance()
		name := p.parseDottedName()
		if p.isOp("(") { // skip @deco(args)
			depth := 0
			for p.cur().Kind != TEOF {
				if p.isOp("(") {
					depth++
				} else if p.isOp(")") {
					depth--
					if depth == 0 {
						p.advance()
						break
					}
				}
				p.advance()
			}
		}
		decs = append(decs, name)
		p.skipNewlines()
	}
	return decs
}

func (p *Parser) parseClassMethod(cls *parser.ClassDecl, fields *[]*parser.FieldDecl, seen map[string]bool, decorators []string) {
	line := p.advance().Line // 'def'
	pyName := p.expectKind(TName).Value
	mname := pyName
	// Dunders map to their Go equivalents (e.g. __str__ → Stringer's String),
	// so operators/len/print route to them.
	if g, ok := dunderMethods[mname]; ok {
		mname = g
	}
	isProperty, isStatic, isClassmethod := false, false, false
	for _, d := range decorators {
		switch d {
		case "property":
			isProperty = true
		case "staticmethod":
			isStatic = true
		case "classmethod":
			isClassmethod = true
		}
	}
	p.expectOp("(")

	var params []*parser.ParamDecl
	first := true
	for !p.isOp(")") && p.cur().Kind != TEOF {
		if p.isOp("**") {
			p.errf(p.cur(), "**kwargs is not yet supported")
		}
		variadic := p.acceptOp("*") // *args
		pn := goSafe(p.expectKind(TName).Value)
		var pt parser.TypeExpr
		if p.acceptOp(":") {
			pt = p.parseType()
		}
		var def parser.Expr
		if p.acceptOp("=") {
			def = p.parseExpr()
		}
		// Drop the implicit receiver: `self` for a normal method, `cls` for a
		// @classmethod. A @staticmethod has no implicit first parameter.
		dropFirst := first && ((pn == "self" && !isStatic && !isClassmethod) ||
			(pn == "cls" && isClassmethod))
		first = false
		if !dropFirst {
			params = append(params, &parser.ParamDecl{Name: pn, Type: pt, Default: def, Variadic: variadic})
		}
		if !p.acceptOp(",") {
			break
		}
	}
	p.expectOp(")")

	var ret parser.TypeExpr
	if p.acceptOp("->") {
		ret = voidIfNone(p.parseType())
	}
	// __enter__ conventionally `return self`; default its return type to the
	// class so the return type-checks and `with ... as x` binds an instance.
	if pyName == "__enter__" && ret == nil {
		ret = &parser.SimpleType{Name: p.currentClass}
	}

	prevInMethod := p.inMethod
	prevRet := p.currentFnRet
	prevCls := p.clsAlias
	// static/class methods have no `self`; a @classmethod binds `cls` to the
	// class for the body.
	p.inMethod = !isStatic && !isClassmethod
	if isClassmethod {
		p.clsAlias = p.currentClass
	}
	p.currentFnRet = ret
	p.pushScope()
	for _, pa := range params {
		p.declare(pa.Name, paramPyType(pa))
		p.recordParamElem(pa)
		p.recordParamInstance(pa)
	}
	body := p.parseBlock()
	if !isStatic && !isClassmethod {
		p.collectFields(body, fields, seen)
	}
	p.popScope()
	p.inMethod = prevInMethod
	p.currentFnRet = prevRet
	p.clsAlias = prevCls

	ensureTrailingReturn(body, ret)

	// @staticmethod / @classmethod are lifted to a package function named
	// Class_method, called via Class.method(...) / obj.method(...).
	if isStatic || isClassmethod {
		fnName := p.currentClass + "_" + pyName
		p.classStatics[p.currentClass][pyName] = true
		p.fnRet[fnName] = typeFromExpr(ret)
		p.pendingDecls = append(p.pendingDecls, &parser.FnDecl{
			Line: line, Name: fnName, Params: params, ReturnType: ret, Body: body,
		})
		return
	}

	if mname == "__init__" {
		if p.currentClass != "" {
			p.defParams[p.currentClass] = params // constructor signature for kwargs/defaults
		}
		superArgs, superCalled := extractSuperInit(body)
		cls.Ctor = &parser.CtorDecl{Params: params, Body: body, SuperArgs: superArgs, SuperCalled: superCalled}
		return
	}
	if p.currentClass != "" {
		p.classMethods[p.currentClass][mname] = typeFromExpr(ret)
		if isProperty {
			p.classProps[p.currentClass][pyName] = true
		}
		// Record a method that returns an instance, so operator-dunder results
		// chain (a + b + c) and attribute access on a method result resolves.
		if st, ok := ret.(*parser.SimpleType); ok && p.classNames[st.Name] {
			p.classMethodRet[p.currentClass][mname] = st.Name
		}
	}
	cls.Methods = append(cls.Methods, &parser.MethodDecl{
		Name: mname, IsPub: true, Params: params, ReturnType: ret, Body: body,
	})
}

// extractSuperInit finds a `super().__init__(args)` statement in a constructor
// body, removes it, and returns its args (Zinc initializes the embedded parent
// from CtorDecl.SuperArgs/SuperCalled, not from a body statement).
func extractSuperInit(body *parser.BlockStmt) ([]parser.Expr, bool) {
	for i, s := range body.Stmts {
		es, ok := s.(*parser.ExprStmt)
		if !ok {
			continue
		}
		call, ok := es.Expr.(*parser.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Callee.(*parser.SelectorExpr)
		if !ok || sel.Field != "__init__" {
			continue
		}
		inner, ok := sel.Object.(*parser.CallExpr)
		if !ok {
			continue
		}
		if id, ok := inner.Callee.(*parser.Ident); !ok || id.Name != "super" {
			continue
		}
		body.Stmts = append(body.Stmts[:i:i], body.Stmts[i+1:]...)
		return call.Args, true
	}
	return nil, false
}

// collectFields scans a method body's top-level statements for `self.X = ...`
// and records each new attribute as a struct field, typed from the assigned
// value. Nested assignments (inside if/for) are a known gap.
func (p *Parser) collectFields(b *parser.BlockStmt, fields *[]*parser.FieldDecl, seen map[string]bool) {
	for _, s := range b.Stmts {
		as, ok := s.(*parser.AssignStmt)
		if !ok {
			continue
		}
		sel, ok := as.Target.(*parser.SelectorExpr)
		if !ok {
			continue
		}
		if _, isThis := sel.Object.(*parser.ThisExpr); !isThis {
			continue
		}
		if seen[sel.Field] {
			continue
		}
		seen[sel.Field] = true
		ft := p.typeOf(as.Value)
		if p.currentClass != "" {
			p.classFields[p.currentClass][sel.Field] = ft
		}
		// A list-literal field gets a concrete Go slice type ([]T) rather than
		// Any, so len(self.f) / self.f[i] / `for x in self.f` / self.f.append()
		// work natively. (Dict/set fields still infer as Any — see gaps.)
		fieldType := zincTypeForPy(ft)
		if lit, ok := as.Value.(*parser.ListLit); ok {
			gt := &parser.GenericType{Name: "List"}
			if len(lit.Elements) > 0 {
				if elem := p.typeOf(lit.Elements[0]); elem != tUnknown {
					gt.TypeArgs = []parser.TypeExpr{zincTypeForPy(elem)}
				}
			}
			fieldType = gt
			// Share the type node with the literal so a later refinement (from
			// self.f.append(v)) updates the constructor initializer too — the
			// empty `[]` then emits []T{} matching the []T field.
			lit.ExplicitType = gt
		}
		fd := &parser.FieldDecl{Name: sel.Field, IsPub: true, Type: fieldType}
		*fields = append(*fields, fd)
		if p.currentClass != "" {
			if p.classFieldDecl[p.currentClass] == nil {
				p.classFieldDecl[p.currentClass] = map[string]*parser.FieldDecl{}
			}
			p.classFieldDecl[p.currentClass][sel.Field] = fd
		}
	}
}

// refineListFieldElem infers an empty-list field's element type from a
// `self.field.append(v)` call: if the field is a List with no element type yet,
// set it from the appended value. Lets `self.items = []` then
// `self.items.append(int)` become []int so accumulation type-checks.
func (p *Parser) refineListFieldElem(field string, val parser.Expr) {
	if p.currentClass == "" {
		return
	}
	fd := p.classFieldDecl[p.currentClass][field]
	if fd == nil {
		return
	}
	gt, ok := fd.Type.(*parser.GenericType)
	if !ok || gt.Name != "List" || len(gt.TypeArgs) > 0 {
		return
	}
	if elem := p.typeOf(val); elem != tUnknown {
		gt.TypeArgs = []parser.TypeExpr{zincTypeForPy(elem)}
	}
}

// zincTypeForPy maps an inferred pytype to a zinc type node. Unknown becomes
// Any (interface{}), which keeps the field usable even without inference.
func zincTypeForPy(t pytype) parser.TypeExpr {
	switch t {
	case tInt:
		return &parser.SimpleType{Name: "int"}
	case tFloat:
		// Python float is double-precision; "double" matches how float
		// literals and `float` annotations are typed (a bare "float" is 32-bit
		// to the typechecker — see mapPyType).
		return &parser.SimpleType{Name: "double"}
	case tStr:
		return &parser.SimpleType{Name: "String"}
	case tBool:
		return &parser.SimpleType{Name: "bool"}
	}
	return &parser.SimpleType{Name: "Any"}
}
