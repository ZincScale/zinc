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
}

// isClassProp reports whether class cls exposes pyName as an @property.
func (p *Parser) isClassProp(cls, pyName string) bool {
	props, ok := p.classProps[cls]
	return ok && props[pyName]
}

// parseClass parses a Python `class` into a zinc ClassDecl. Python attributes
// are implicit (created by `self.x = ...`), whereas zinc needs explicit field
// declarations — so fields are discovered by scanning method bodies for
// self-assignments and their inferred types. `__init__` becomes the
// constructor; other defs become methods with the leading `self` dropped.
func (p *Parser) parseClass() *parser.ClassDecl {
	line := p.advance().Line // 'class'
	name := p.expectKind(TName).Value

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
	p.expectKind(TNewline)
	p.expectKind(TIndent)

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
	prevClass := p.currentClass
	p.currentClass = name
	defer func() { p.currentClass = prevClass }()

	cls := &parser.ClassDecl{Line: line, Name: name, Parents: parents}
	var fields []*parser.FieldDecl
	seen := map[string]bool{}

	for p.cur().Kind != TDedent && p.cur().Kind != TEOF {
		p.skipNewlines()
		if p.cur().Kind == TDedent || p.cur().Kind == TEOF {
			break
		}
		if p.isOp("@") {
			decs := p.parseDecorators()
			if p.isKw("def") {
				p.parseClassMethod(cls, &fields, seen, decs)
			} else {
				p.errf(p.cur(), "a decorator must be followed by a def")
			}
			continue
		}
		if p.isKw("def") {
			p.parseClassMethod(cls, &fields, seen, nil)
			continue
		}
		// pass / docstrings / class-level statements: not modelled yet.
		startPos := p.pos
		if p.isKw("pass") {
			p.advance()
			p.endSimple()
		} else {
			p.parseStmt()
		}
		if p.pos == startPos {
			p.advance()
		}
	}
	p.expectKind(TDedent)
	cls.Fields = fields
	return cls
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
	p.advance() // 'def'
	pyName := p.expectKind(TName).Value
	mname := pyName
	// Dunders map to their Go equivalents (e.g. __str__ → Stringer's String),
	// so operators/len/print route to them.
	if g, ok := dunderMethods[mname]; ok {
		mname = g
	}
	isProperty := false
	for _, d := range decorators {
		if d == "property" {
			isProperty = true
		}
	}
	p.expectOp("(")

	var params []*parser.ParamDecl
	first := true
	for !p.isOp(")") && p.cur().Kind != TEOF {
		pn := p.expectKind(TName).Value
		var pt parser.TypeExpr
		if p.acceptOp(":") {
			pt = p.parseType()
		}
		var def parser.Expr
		if p.acceptOp("=") {
			def = p.parseExpr()
		}
		isSelf := first && pn == "self"
		first = false
		if !isSelf {
			params = append(params, &parser.ParamDecl{Name: pn, Type: pt, Default: def})
		}
		if !p.acceptOp(",") {
			break
		}
	}
	p.expectOp(")")

	var ret parser.TypeExpr
	if p.acceptOp("->") {
		ret = p.parseType()
	}

	prevInMethod := p.inMethod
	prevRet := p.currentFnRet
	p.inMethod = true
	p.currentFnRet = ret
	p.pushScope()
	for _, pa := range params {
		p.declare(pa.Name, typeFromExpr(pa.Type))
		p.recordParamElem(pa)
		p.recordParamInstance(pa)
	}
	body := p.parseBlock()
	p.collectFields(body, fields, seen)
	p.popScope()
	p.inMethod = prevInMethod
	p.currentFnRet = prevRet

	if mname == "__init__" {
		superArgs, superCalled := extractSuperInit(body)
		cls.Ctor = &parser.CtorDecl{Params: params, Body: body, SuperArgs: superArgs, SuperCalled: superCalled}
		return
	}
	ensureTrailingReturn(body, ret)
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
		*fields = append(*fields, &parser.FieldDecl{
			Name:  sel.Field,
			IsPub: true,
			Type:  zincTypeForPy(ft),
		})
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
