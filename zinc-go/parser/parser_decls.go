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

package parser

import (
	"zinc-go/lexer"
)

// --- Declarations ------------------------------------------------------------

// v2ParseFnDecl: ReturnType name(params) { body }
//                void name(params) { body }
//                ReturnType name(params) = expr  (single-expression)
//
// Type-first declaration matches Java/C#/Dart shape. The literal `void`
// stands in for "no return type" — `init` constructors keep their own
// keyword shape.
func (p *Parser) v2ParseFnDecl() *FnDecl {
	line := p.peek().Line
	retType := p.v2ParseFnReturnType()
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.v2ValidateDeclName(name)
	typeParams := p.parseTypeParams()
	params := p.v2ParseParamList()

	// Single-expression form: ReturnType name(params) = expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		expr := p.v2ParseExpr()
		body := &BlockStmt{Stmts: []Stmt{&ReturnStmt{Line: line, Value: expr}}}
		return &FnDecl{Line: line, Name: name, TypeParams: typeParams, Params: params, ReturnType: retType, Body: body}
	}

	body := p.v2ParseBlock()
	return &FnDecl{Line: line, Name: name, TypeParams: typeParams, Params: params, ReturnType: retType, Body: body}
}

// v2ParseFnReturnType parses either `void` (returns nil) or any type
// expression — including a multi-value tuple `(T1, T2, ...)`. Used by
// both top-level fn decls and class method decls.
func (p *Parser) v2ParseFnReturnType() TypeExpr {
	tok := p.peek()
	if tok.Type == lexer.TOKEN_IDENT && tok.Literal == "void" {
		p.advance()
		return nil
	}
	return p.v2ParseTypeOrTuple()
}

// v2ParseParamList: (type name, type name = default, ...)
func (p *Parser) v2ParseParamList() []*ParamDecl {
	p.expect(lexer.TOKEN_LPAREN)
	var params []*ParamDecl
	if !p.check(lexer.TOKEN_RPAREN) {
		params = append(params, p.v2ParseParam())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			if p.check(lexer.TOKEN_RPAREN) {
				break // trailing comma
			}
			params = append(params, p.v2ParseParam())
		}
	}
	p.expect(lexer.TOKEN_RPAREN)
	return params
}

// v2ParseParam: [const] type name [= default]  OR  *args  OR  **kwargs
func (p *Parser) v2ParseParam() *ParamDecl {
	// Handle const modifier on params
	isConst := false
	if p.check(lexer.TOKEN_CONST) {
		p.advance()
		isConst = true
	}

	// Handle *args
	variadic := false
	kwVariadic := false
	if p.check(lexer.TOKEN_STAR_STAR) {
		p.advance()
		kwVariadic = true
	} else if p.check(lexer.TOKEN_STAR) {
		p.advance()
		variadic = true
	}

	// Type-first: type name  OR  untyped: name
	var typ TypeExpr
	var name string

	if !variadic && !kwVariadic && p.v2IsTypeAnnotation() {
		// Typed param: int x, list<int> items, str? name
		typ = p.v2ParseType()
		// Check for Java-style variadic: Type... name
		if p.check(lexer.TOKEN_DOTDOTDOT) {
			p.advance()
			variadic = true
		}
		name = p.v2ExpectIdent()
		p.v2ValidateDeclName(name)
	} else {
		// Untyped param or variadic: x, *args, **kwargs
		name = p.v2ExpectIdent()
		p.v2ValidateDeclName(name)
	}

	var def Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		def = p.v2ParseExpr()
	}

	param := &ParamDecl{Name: name, Type: typ, Default: def, Variadic: variadic, IsConst: isConst}
	if kwVariadic {
		param.Name = "**" + name
	}
	return param
}

// v2ParseClassDecl: class Name[(Parent)] ... end
func (p *Parser) v2ParseClassDecl() *ClassDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_CLASS)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.v2ValidateDeclName(name)
	typeParams := p.parseTypeParams()

	// Optional parent class/interfaces: class Dog : Animal, Serializable, Queue<T>, core.Describable
	var parents []string
	if p.check(lexer.TOKEN_COLON) {
		p.advance()
		parentName := p.expect(lexer.TOKEN_IDENT).Literal
		// Dotted parent: core.Describable
		for p.check(lexer.TOKEN_DOT) && isIdentLike(p.peekAt(1).Type) {
			p.advance()
			parentName += "." + p.advance().Literal
		}
		parents = append(parents, parentName)
		// Skip generic type args on parent: Queue<T> → consume <T>
		if p.check(lexer.TOKEN_LT) {
			p.advance()
			for !p.check(lexer.TOKEN_GT) && !p.check(lexer.TOKEN_EOF) {
				p.advance()
			}
			p.expect(lexer.TOKEN_GT)
		}
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			parentName = p.expect(lexer.TOKEN_IDENT).Literal
			for p.check(lexer.TOKEN_DOT) && isIdentLike(p.peekAt(1).Type) {
				p.advance()
				parentName += "." + p.advance().Literal
			}
			parents = append(parents, parentName)
			if p.check(lexer.TOKEN_LT) {
				p.advance()
				for !p.check(lexer.TOKEN_GT) && !p.check(lexer.TOKEN_EOF) {
					p.advance()
				}
				p.expect(lexer.TOKEN_GT)
			}
		}
	}

	var fields []*FieldDecl
	var methods []*MethodDecl
	var variants []*DataClassDecl
	var ctors []*CtorDecl

	p.expect(lexer.TOKEN_LBRACE)
	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		tok := p.peek()

		if tok.Type == lexer.TOKEN_DATA {
			// Sealed class variant: data Circle(double radius)
			variants = append(variants, p.v2ParseDataClassDecl())
		} else if tok.Type == lexer.TOKEN_AT {
			annots := p.v2ParseAnnotations()
			isPub := false
			if p.check(lexer.TOKEN_PUB) {
				isPub = true
				p.advance()
			}
			// Dispatch on what follows: a field (var/const/bare type-first)
			// or a method. Annotations on fields drive Go struct tags;
			// on methods they're recognized markers like @Override.
			next := p.peek()
			if next.Type == lexer.TOKEN_VAR || next.Type == lexer.TOKEN_CONST {
				f := p.v2ParseFieldDecl()
				f.Annotations = annots
				if isPub {
					f.IsPub = true
				}
				fields = append(fields, f)
			} else if p.v2IsFnDeclTypeFirst() {
				m := p.v2ParseMethodDecl()
				m.Annotations = annots
				m.IsPub = m.IsPub || isPub
				methods = append(methods, m)
			} else if next.Type == lexer.TOKEN_IDENT && p.v2IsClassFieldDecl() {
				f := p.v2ParseFieldDeclNoKeyword()
				f.Annotations = annots
				if isPub {
					f.IsPub = true
				}
				fields = append(fields, f)
			} else {
				// Fallback to method — preserves existing behavior for
				// cases the type-first detector can't classify upfront.
				m := p.v2ParseMethodDecl()
				m.Annotations = annots
				m.IsPub = m.IsPub || isPub
				methods = append(methods, m)
			}
		} else if tok.Type == lexer.TOKEN_OVERRIDE {
			// override ReturnType name(...) { ... }
			p.advance() // consume override
			m := p.v2ParseMethodDecl()
			m.IsPub = true // override methods are always public
			m.Annotations = append(m.Annotations, &Annotation{Name: "Override"})
			methods = append(methods, m)
		} else if tok.Type == lexer.TOKEN_PUB {
			next := p.peekAt(1)
			if next.Type == lexer.TOKEN_OVERRIDE {
				// pub override ReturnType name(...) { ... }
				p.advance() // consume pub
				p.advance() // consume override
				m := p.v2ParseMethodDecl()
				m.IsPub = true
				m.Annotations = append(m.Annotations, &Annotation{Name: "Override"})
				methods = append(methods, m)
			} else {
				// Could be `pub ReturnType name(...)` (method) or
				// `pub Type name [= default]` (field). Lookahead past
				// `pub` and use the type-first detector to choose.
				p.advance() // consume pub
				if p.v2IsFnDeclTypeFirst() {
					m := p.v2ParseMethodDecl()
					m.IsPub = true
					methods = append(methods, m)
				} else {
					f := p.v2ParseFieldDeclNoKeyword()
					f.IsPub = true
					fields = append(fields, f)
				}
			}
		} else if tok.Type == lexer.TOKEN_READONLY {
			// read Type name — read-only field (no var keyword needed)
			p.advance() // consume read
			f := p.v2ParseFieldDeclNoKeyword()
			f.IsReadonly = true
			fields = append(fields, f)
		} else if tok.Type == lexer.TOKEN_INIT && p.peekAt(1).Type == lexer.TOKEN_LPAREN {
			// init(params) { body } — constructor (supports overloading)
			p.advance() // consume init
			params := p.v2ParseParamList()
			body := p.v2ParseBlock()
			// Extract super(...) call from body if present
			var superArgs []Expr
			var superCalled bool
			var filteredStmts []Stmt
			for _, s := range body.Stmts {
				if es, ok := s.(*ExprStmt); ok {
					if call, ok := es.Expr.(*CallExpr); ok {
						if ident, ok := call.Callee.(*Ident); ok && ident.Name == "super" {
							superArgs = call.Args
							superCalled = true
							continue
						}
					}
				}
				filteredStmts = append(filteredStmts, s)
			}
			body.Stmts = filteredStmts
			ctors = append(ctors, &CtorDecl{Params: params, Body: body, SuperArgs: superArgs, SuperCalled: superCalled})
		} else if tok.Type == lexer.TOKEN_VAR || tok.Type == lexer.TOKEN_CONST || tok.Type == lexer.TOKEN_INIT {
			f := p.v2ParseFieldDecl()
			fields = append(fields, f)
		} else if p.v2IsFnDeclTypeFirst() {
			// Private method: `ReturnType name(...) { ... }`
			m := p.v2ParseMethodDecl()
			m.IsPub = false
			methods = append(methods, m)
		} else if tok.Type == lexer.TOKEN_IDENT && p.v2IsClassFieldDecl() {
			// Type name = default — private field without var keyword
			f := p.v2ParseFieldDeclNoKeyword()
			fields = append(fields, f)
		} else {
			p.errorf("unexpected token %s in class body", tok.Type)
			p.advance()
		}
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	// Back-compat: set Ctor to the first constructor if any
	var ctor *CtorDecl
	if len(ctors) > 0 {
		ctor = ctors[0]
	}
	return &ClassDecl{Line: line, Name: name, TypeParams: typeParams, Parents: parents, Fields: fields, Ctor: ctor, Ctors: ctors, Methods: methods, Variants: variants}
}

// v2ParseMethodDecl: [abstract] ReturnType name(params) [{ body }]
//                    [abstract] void name(params) [{ body }]
func (p *Parser) v2ParseMethodDecl() *MethodDecl {
	_ = p.peek().Line
	retType := p.v2ParseFnReturnType()
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.v2ValidateDeclName(name)
	params := p.v2ParseParamList()

	// Body is optional — interface methods declare the signature only.
	var body *BlockStmt
	if p.check(lexer.TOKEN_LBRACE) {
		body = p.v2ParseBlock()
	}
	return &MethodDecl{Name: name, Params: params, ReturnType: retType, Body: body}
}

// v2ParseFieldDecl: var name = default  |  const type name = default  |  init type name
// `var` fields require an initializer (type is inferred). For an uninitialized
// field, write the bare form `Type name` (handled by v2ParseFieldDeclNoKeyword).
func (p *Parser) v2ParseFieldDecl() *FieldDecl {
	isConst := p.peek().Type == lexer.TOKEN_CONST
	isInit := p.peek().Type == lexer.TOKEN_INIT
	p.advance() // consume var/const/init

	// Type-first: type name
	var typ TypeExpr
	var name string
	if p.v2IsTypeAnnotation() {
		// Reject `var Type name` for fields — `var` means "infer the
		// type from the initializer." For an uninitialized field with
		// an explicit type, drop `var` and write `Type name`.
		// `const Type name` and `init Type name` keep their shapes.
		if !isConst && !isInit {
			p.errorf("var keyword with explicit type is not allowed; either drop `var` (e.g. `Mutex mu`) or drop the type (e.g. `var mu = ...`)")
		}
		typ = p.v2ParseType()
		name = p.v2ExpectIdent()
	} else {
		name = p.v2ExpectIdent()
	}

	var def Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		def = p.v2ParseExpr()
	}
	return &FieldDecl{Name: name, Type: typ, Default: def, IsConst: isConst, IsInit: isInit}
}

// v2ParseFieldDeclNoKeyword parses a field without var/const/init prefix: Type name [= default]
func (p *Parser) v2ParseFieldDeclNoKeyword() *FieldDecl {
	var typ TypeExpr
	var name string
	if p.v2IsTypeAnnotation() {
		typ = p.v2ParseType()
		name = p.v2ExpectIdent()
	} else {
		name = p.v2ExpectIdent()
	}
	var def Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		def = p.v2ParseExpr()
	}
	return &FieldDecl{Name: name, Type: typ, Default: def}
}

// v2SkipTypeAnnotation walks past a type annotation starting at offset
// `start` (relative to the current token) and returns the offset of the
// token after the type. Mirrors the logic v2IsClassFieldDecl used to
// embed inline. Use the result to peek at what follows the type — e.g.
// IDENT for a name, LPAREN for a function signature.
func (p *Parser) v2SkipTypeAnnotation(start int) int {
	i := start + 1
	// Dotted type: sync.Mutex, http.ResponseWriter, etc.
	for p.peekAt(i).Type == lexer.TOKEN_DOT && isIdentLike(p.peekAt(i+1).Type) {
		i += 2
	}
	// Generic: Type<...>
	if p.peekAt(i).Type == lexer.TOKEN_LT {
		depth := 1
		i++
		for depth > 0 && p.peekAt(i).Type != lexer.TOKEN_EOF {
			if p.peekAt(i).Type == lexer.TOKEN_LT {
				depth++
			} else if p.peekAt(i).Type == lexer.TOKEN_GT {
				depth--
			}
			i++
		}
	}
	// Array: Type[]
	if p.peekAt(i).Type == lexer.TOKEN_LBRACKET && p.peekAt(i+1).Type == lexer.TOKEN_RBRACKET {
		i += 2
	}
	// Nullable: Type?
	if p.peekAt(i).Type == lexer.TOKEN_QUESTION {
		i++
	}
	return i
}

// v2IsFnDeclTypeFirst returns true if the current position starts a
// type-first function/method declaration: `ReturnType name[<...>](` or
// `void name[<...>](`. Used by both top-level and class-body dispatchers.
// Generic functions like `T identity<T>(T x)` carry type params between
// the name and the `(`, so the lookahead skips them.
func (p *Parser) v2IsFnDeclTypeFirst() bool {
	// `void name [<...>] (`
	if p.peek().Type == lexer.TOKEN_IDENT && p.peek().Literal == "void" {
		if !isIdentLike(p.peekAt(1).Type) {
			return false
		}
		j := p.v2SkipFnNameTypeParams(2)
		return p.peekAt(j).Type == lexer.TOKEN_LPAREN
	}
	// `(T1, T2, ...) name [<...>] (` — tuple return type
	if p.peek().Type == lexer.TOKEN_LPAREN {
		i := p.v2SkipBalancedParens(0)
		if i < 0 {
			return false
		}
		if !isIdentLike(p.peekAt(i).Type) {
			return false
		}
		j := p.v2SkipFnNameTypeParams(i + 1)
		return p.peekAt(j).Type == lexer.TOKEN_LPAREN
	}
	if !p.v2IsTypeAnnotation() {
		return false
	}
	i := p.v2SkipTypeAnnotation(0)
	if !isIdentLike(p.peekAt(i).Type) {
		return false
	}
	j := p.v2SkipFnNameTypeParams(i + 1)
	return p.peekAt(j).Type == lexer.TOKEN_LPAREN
}

// v2SkipBalancedParens walks past a `(...)` block starting at `start`
// (which must point at `(`), returning the index of the token after the
// matching `)`. Returns -1 on mismatch / EOF. Used to skip a
// parenthesized tuple type in lookahead positions.
func (p *Parser) v2SkipBalancedParens(start int) int {
	if p.peekAt(start).Type != lexer.TOKEN_LPAREN {
		return -1
	}
	depth := 1
	i := start + 1
	for depth > 0 {
		switch p.peekAt(i).Type {
		case lexer.TOKEN_EOF:
			return -1
		case lexer.TOKEN_LPAREN:
			depth++
		case lexer.TOKEN_RPAREN:
			depth--
		}
		i++
	}
	return i
}

// v2SkipFnNameTypeParams skips a `<T, U, ...>` block right after a
// function name, returning the index of the token after `>`. If no `<`
// is present, returns `start` unchanged. Caller still needs to check
// that the result is `(` to confirm a function decl.
func (p *Parser) v2SkipFnNameTypeParams(start int) int {
	if p.peekAt(start).Type != lexer.TOKEN_LT {
		return start
	}
	depth := 1
	i := start + 1
	for depth > 0 && p.peekAt(i).Type != lexer.TOKEN_EOF {
		if p.peekAt(i).Type == lexer.TOKEN_LT {
			depth++
		} else if p.peekAt(i).Type == lexer.TOKEN_GT {
			depth--
		}
		i++
	}
	return i
}

// v2IsClassFieldDecl checks if the current IDENT in a class body starts a field
// declaration (Type name [= default]) rather than a method or expression.
func (p *Parser) v2IsClassFieldDecl() bool {
	if !p.v2IsTypeAnnotation() {
		return false
	}
	i := p.v2SkipTypeAnnotation(0)
	// Next should be the field name (ident)
	nameToken := p.peekAt(i)
	if nameToken.Type != lexer.TOKEN_IDENT && nameToken.Type != lexer.TOKEN_DATA &&
		nameToken.Type != lexer.TOKEN_MATCH && nameToken.Type != lexer.TOKEN_PRINT {
		return false
	}
	i++
	// After name: = (with default) or end of statement (no default)
	// NOT ( which would be a method call
	next := p.peekAt(i)
	return next.Type == lexer.TOKEN_ASSIGN || next.Type == lexer.TOKEN_RBRACE ||
		next.Type == lexer.TOKEN_EOF || next.Type == lexer.TOKEN_SEMICOLON ||
		next.Type == lexer.TOKEN_IDENT || // next field or method
		next.Type == lexer.TOKEN_PUB || next.Type == lexer.TOKEN_READONLY ||
		next.Type == lexer.TOKEN_INIT ||
		next.Type == lexer.TOKEN_CONST || next.Type == lexer.TOKEN_VAR ||
		next.Type == lexer.TOKEN_OVERRIDE ||
		next.Type == lexer.TOKEN_AT // annotation
}

// v2ParseDataClassDecl: data Name(Type field, Type field = default) [{ methods }]
func (p *Parser) v2ParseDataClassDecl() *DataClassDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_DATA)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.v2ValidateDeclName(name)
	typeParams := p.parseTypeParams()

	// Parse record-style params: data User(String name, int age = 0)
	p.expect(lexer.TOKEN_LPAREN)
	var params []*FieldDecl
	if !p.check(lexer.TOKEN_RPAREN) {
		params = append(params, p.v2ParseDataClassParam())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			if p.check(lexer.TOKEN_RPAREN) {
				break // trailing comma
			}
			params = append(params, p.v2ParseDataClassParam())
		}
	}
	p.expect(lexer.TOKEN_RPAREN)

	// Optional parents: data User(String name) : Serializable
	var parents []string
	if p.check(lexer.TOKEN_COLON) {
		p.advance()
		parents = append(parents, p.expect(lexer.TOKEN_IDENT).Literal)
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			parents = append(parents, p.expect(lexer.TOKEN_IDENT).Literal)
		}
	}

	// Optional body with methods: data User(String name) { fn ... }
	var methods []*MethodDecl
	if p.check(lexer.TOKEN_LBRACE) {
		p.advance()
		p.skipSemis()
		for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
			if p.peek().Type == lexer.TOKEN_PUB {
				p.advance()
				m := p.v2ParseMethodDecl()
				m.IsPub = true
				methods = append(methods, m)
			} else {
				methods = append(methods, p.v2ParseMethodDecl())
			}
			p.skipSemis()
		}
		p.expect(lexer.TOKEN_RBRACE)
	}

	return &DataClassDecl{Line: line, Name: name, TypeParams: typeParams, Parents: parents, Params: params, Methods: methods}
}

// v2ParseDataClassParam: [@Annotations] Type name [= default]
func (p *Parser) v2ParseDataClassParam() *FieldDecl {
	var annots []*Annotation
	if p.check(lexer.TOKEN_AT) {
		annots = p.v2ParseAnnotations()
	}
	typ := p.v2ParseType()
	name := p.v2ExpectIdent()
	var def Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		def = p.v2ParseExpr()
	}
	return &FieldDecl{Name: name, IsPub: true, Type: typ, Default: def, Annotations: annots}
}

// v2ParseInterfaceDecl: interface Name { ReturnType method(params) ... }
func (p *Parser) v2ParseInterfaceDecl() *InterfaceDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_INTERFACE)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.v2ValidateDeclName(name)
	typeParams := p.parseTypeParams()
	p.expect(lexer.TOKEN_LBRACE)
	var methods []*MethodSig
	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		// Interface methods are always pub — skip optional pub keyword
		if p.check(lexer.TOKEN_PUB) {
			p.advance()
		}
		retType := p.v2ParseFnReturnType()
		mName := p.expect(lexer.TOKEN_IDENT).Literal
		p.v2ValidateDeclName(mName)
		params := p.v2ParseParamList()
		methods = append(methods, &MethodSig{Name: mName, IsPub: true, Params: params, ReturnType: retType})
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &InterfaceDecl{Line: line, Name: name, TypeParams: typeParams, Methods: methods}
}

// v2ParseConstDecl: const [type] NAME = expr (top-level constant)
func (p *Parser) v2ParseConstDecl() *ConstDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_CONST)
	var typ TypeExpr
	var name string
	if p.v2IsTypeAnnotation() {
		typ = p.v2ParseType()
		name = p.v2ExpectIdent()
	} else {
		name = p.v2ExpectIdent()
	}
	p.v2ValidateDeclName(name)
	p.expect(lexer.TOKEN_ASSIGN)
	val := p.v2ParseExpr()
	return &ConstDecl{Line: line, Name: name, Type: typ, Value: val}
}

// v2ParseTypeAlias: type Name = TypeExpr
func (p *Parser) v2ParseTypeAlias() *TypeAliasDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_TYPE)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.v2ValidateDeclName(name)
	p.expect(lexer.TOKEN_ASSIGN)
	typ := p.v2ParseType()
	return &TypeAliasDecl{Line: line, Name: name, Type: typ}
}

// v2ParseEnumDecl: enum Name { variants }
func (p *Parser) v2ParseEnumDecl() *EnumDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_ENUM)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.v2ValidateDeclName(name)
	p.expect(lexer.TOKEN_LBRACE)
	var variants []string
	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		vName := p.expect(lexer.TOKEN_IDENT).Literal
		p.v2ValidateDeclName(vName)
		variants = append(variants, vName)
		if p.check(lexer.TOKEN_COMMA) {
			p.advance()
		}
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &EnumDecl{Line: line, Name: name, Variants: variants}
}

// v2ParseTestDecl: `test "name" { body }` — a test case at top level.
// The contextual `test` keyword has already been peeked by the caller; we
// consume it here along with the string literal and block body.
func (p *Parser) v2ParseTestDecl() *TestDecl {
	line := p.peek().Line
	p.advance() // consume "test"
	name := p.expect(lexer.TOKEN_STRING_LIT).Literal
	body := p.v2ParseBlock()
	return &TestDecl{Line: line, Name: name, Body: body}
}

// --- Imports -----------------------------------------------------------------

// v2ParsePackageDecl: package com.example.myapp
func (p *Parser) v2ParsePackageDecl() *PackageDecl {
	p.expect(lexer.TOKEN_PACKAGE)
	path := p.expect(lexer.TOKEN_IDENT).Literal
	for p.check(lexer.TOKEN_DOT) {
		p.advance()
		path += "." + p.expect(lexer.TOKEN_IDENT).Literal
	}
	return &PackageDecl{Path: path}
}

// v2ParseImport: Go-style `import pkg`, `import pkg/sub`, `import pkg as alias`.
// Multi-segment paths are slash-separated (`net/http`, `encoding/json`,
// `log/slog`). The parser stores the path with dots internally; codegen
// converts back to slashes for the emitted Go import path.
func (p *Parser) v2ParseImport() *ImportDecl {
	p.expect(lexer.TOKEN_IMPORT)
	path := p.v2ExpectIdentOrKeyword()
	for p.check(lexer.TOKEN_SLASH) {
		p.advance()
		path += "." + p.v2ExpectIdentOrKeyword()
	}
	// Check for alias: import X as Y
	var alias string
	if p.check(lexer.TOKEN_AS) {
		p.advance()
		alias = p.v2ExpectIdentOrKeyword()
	}
	return &ImportDecl{Path: path, Alias: alias}
}

// isIdentLike returns true if the token type can be used as a name segment
// (identifier or keyword that could appear in a dotted path like java.util.concurrent).
func isIdentLike(t lexer.TokenType) bool {
	return t == lexer.TOKEN_IDENT ||
		t == lexer.TOKEN_DATA || t == lexer.TOKEN_MATCH ||
		t == lexer.TOKEN_PRINT || t == lexer.TOKEN_SPAWN ||
		t == lexer.TOKEN_INTERFACE
}

// v2ExpectIdentOrKeyword consumes and returns the current token's literal
// if it is an IDENT or any keyword token that could appear as a Java
// package/class name segment (e.g., "concurrent" in java.util.concurrent).
func (p *Parser) v2ExpectIdentOrKeyword() string {
	tok := p.peek()
	if isIdentLike(tok.Type) {
		return p.advance().Literal
	}
	return p.expect(lexer.TOKEN_IDENT).Literal
}

// --- Annotations/Decorators --------------------------------------------------

// v2ParseAnnotations: @name or @name(...) — one or more decorators
func (p *Parser) v2ParseAnnotations() []*Annotation {
	var annots []*Annotation
	for p.check(lexer.TOKEN_AT) {
		p.advance() // consume @
		name := p.expect(lexer.TOKEN_IDENT).Literal
		// Support dotted names: @dagster.asset
		for p.check(lexer.TOKEN_DOT) {
			p.advance()
			name += "." + p.expect(lexer.TOKEN_IDENT).Literal
		}
		var args []string
		if p.check(lexer.TOKEN_LPAREN) {
			p.advance()
			for !p.check(lexer.TOKEN_RPAREN) && !p.check(lexer.TOKEN_EOF) {
				// Collect raw arg tokens — preserve string quotes
				tok := p.advance()
				if tok.Type == lexer.TOKEN_STRING_LIT || tok.Type == lexer.TOKEN_RAW_STRING {
					args = append(args, "\""+tok.Literal+"\"")
				} else {
					args = append(args, tok.Literal)
				}
				if p.check(lexer.TOKEN_COMMA) {
					p.advance()
				}
			}
			p.expect(lexer.TOKEN_RPAREN)
		}
		annots = append(annots, &Annotation{Name: name, Args: args})
		p.skipSemis()
	}
	return annots
}
