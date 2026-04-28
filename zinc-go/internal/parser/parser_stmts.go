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
	"strconv"

	"zinc-go/internal/lexer"
)

// --- Statements --------------------------------------------------------------

func (p *Parser) v2ParseStmt() Stmt {
	p.skipSemis()
	tok := p.peek()

	switch tok.Type {
	case lexer.TOKEN_VAR:
		return p.v2ParseVarOrConstStmt()
	case lexer.TOKEN_CONST:
		return p.v2ParseVarOrConstStmt()
	case lexer.TOKEN_RETURN:
		return p.v2ParseReturnStmt()
	case lexer.TOKEN_IF:
		return p.v2ParseIfStmt()
	case lexer.TOKEN_FOR:
		return p.v2ParseForStmt()
	case lexer.TOKEN_WHILE:
		return p.v2ParseWhileStmt()
	case lexer.TOKEN_MATCH:
		return p.v2ParseMatchStmt()
	case lexer.TOKEN_PRINT:
		// Treat print as a regular function call in v2 (supports multi-arg, kwargs)
		return p.v2ParseExprOrAssignStmt()
	case lexer.TOKEN_BREAK:
		p.advance()
		return &BreakStmt{}
	case lexer.TOKEN_CONTINUE:
		p.advance()
		return &ContinueStmt{}
	case lexer.TOKEN_WITH:
		return p.v2ParseWithStmt()
	case lexer.TOKEN_USING:
		return p.v2ParseUsingStmt()
	case lexer.TOKEN_SPAWN:
		return p.v2ParseSpawnStmt()
	case lexer.TOKEN_PARALLEL:
		return p.v2ParseParallelForStmt()
	case lexer.TOKEN_TIMEOUT:
		return p.v2ParseTimeoutStmt()
	case lexer.TOKEN_SELECT:
		return p.v2ParseSelectStmt()
	case lexer.TOKEN_IDENT:
		if tok.Literal == "assert" {
			return p.v2ParseAssertStmt()
		}
		if tok.Literal == "lock" && p.peekAt(1).Type == lexer.TOKEN_LPAREN {
			return p.v2ParseLockStmt()
		}
		// Type name = expr → typed variable declaration without var keyword
		if p.v2IsTypedVarDecl() {
			return p.v2ParseTypedVarStmt()
		}
	case lexer.TOKEN_DATA:
		// data as variable name (data = ..., data["key"], data.field)
		// vs data class declaration (data Name { ... })
		next := p.peekAt(1)
		if next.Type == lexer.TOKEN_IDENT {
			// data Name { ... } — data class declaration (shouldn't be in function body)
			p.errorf("data class declarations must be at top level")
			p.advance()
			return nil
		}
		// data used as variable: data = x, data["key"], etc.
		return p.v2ParseExprOrAssignStmt()
	case lexer.TOKEN_CLASS, lexer.TOKEN_ENUM:
		p.errorf("class/enum declarations must be at top level")
		p.advance()
		return nil
	}

	// Expression statement or assignment
	return p.v2ParseExprOrAssignStmt()
}

// v2ParseVarOrConstStmt: var [type] name = expr  OR  const [type] name = expr  OR  var a, b = expr
func (p *Parser) v2ParseVarOrConstStmt() Stmt {
	line := p.peek().Line
	isConst := p.peek().Type == lexer.TOKEN_CONST
	p.advance() // consume var/const

	// Type-first: var type name = expr  OR  inferred: var name = expr
	var typ TypeExpr
	var name string

	// var (a, b, c) = concurrent { ... }  — parenthesized tuple
	if p.check(lexer.TOKEN_LPAREN) {
		p.advance()
		var names []string
		n := p.v2ExpectIdent()
		p.v2ValidateDeclName(n)
		names = append(names, n)
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			n = p.v2ExpectIdent()
			p.v2ValidateDeclName(n)
			names = append(names, n)
		}
		p.expect(lexer.TOKEN_RPAREN)
		p.expect(lexer.TOKEN_ASSIGN)
		val := p.v2ParseExpr()
		return &TupleVarStmt{Line: line, Names: names, Value: val}
	}

	if p.v2IsTypeAnnotation() {
		// Reject `var Type name [= expr]` — `var` means "infer the type."
		// If the type is named, drop `var` (write `Type name` instead).
		// `const Type name = expr` stays valid — const has documented
		// shape with explicit type.
		if !isConst {
			p.errorf("var keyword with explicit type is not allowed; either drop `var` (e.g. `Mutex mu`) or drop the type (e.g. `var mu = ...`)")
		}
		// Type is present: const int X = 5, const list<int> nums = []
		typ = p.v2ParseType()
		name = p.v2ExpectIdent()
		p.v2ValidateDeclName(name)
	} else {
		// No type (inferred): var x = 5
		name = p.v2ExpectIdent()
		p.v2ValidateDeclName(name)

		// Tuple unpacking: var a, b = expr
		if p.check(lexer.TOKEN_COMMA) {
			names := []string{name}
			for p.check(lexer.TOKEN_COMMA) {
				p.advance()
				tupleName := p.expect(lexer.TOKEN_IDENT).Literal
				p.v2ValidateDeclName(tupleName)
				names = append(names, tupleName)
			}
			p.expect(lexer.TOKEN_ASSIGN)
			val := p.v2ParseExpr()
			return &TupleVarStmt{Line: line, Names: names, Value: val}
		}
	}

	var val Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		val = p.v2ParseExpr()
	}

	// Check for Err { handler } block
	handler := p.v2ParseErrHandler()

	return &VarStmt{Line: line, Name: name, Type: typ, Value: val, IsConst: isConst, OrHandler: handler}
}

// v2IsTypedVarDecl checks if the current position starts a typed variable declaration
// without the var keyword: Type name = expr  or  Type name  (no value).
// The type must look like a type annotation (checked via v2IsTypeAnnotation) AND
// the token after the type+name must be = or end-of-statement (not ( which would be a call).
func (p *Parser) v2IsTypedVarDecl() bool {
	if !p.v2IsTypeAnnotation() {
		return false
	}
	// Scan past the type to find the name position
	i := 1
	tok := p.peek()
	// Handle dotted/fully-qualified names: java.util.Map
	for p.peekAt(i).Type == lexer.TOKEN_DOT && isIdentLike(p.peekAt(i+1).Type) {
		i += 2
	}
	// Handle generic: Type<...>
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
	// Handle array: Type[]
	if p.peekAt(i).Type == lexer.TOKEN_LBRACKET && p.peekAt(i+1).Type == lexer.TOKEN_RBRACKET {
		i += 2
	}
	// Handle nullable: Type?
	if p.peekAt(i).Type == lexer.TOKEN_QUESTION {
		i++
	}
	// Next must be an identifier (the variable name)
	nameToken := p.peekAt(i)
	if nameToken.Type != lexer.TOKEN_IDENT && nameToken.Type != lexer.TOKEN_DATA &&
		nameToken.Type != lexer.TOKEN_MATCH && nameToken.Type != lexer.TOKEN_PRINT {
		return false
	}
	// Exclude contextual keywords that start special statements (assert, lock)
	if tok.Literal == "assert" || tok.Literal == "lock" {
		return false
	}
	i++
	// After name, anything except continuations of the name expression
	// (function call, indexing, selector) means we have a typed
	// declaration. Newlines have no token in this lexer, so for
	// `Type name\n nextStmt`, the next token is whatever starts the
	// next statement — accept it. The negative test below rules out
	// the `Type name(args)` shape (which is a function call) and
	// `Type name[...]` / `Type name.field` (continuation of an expr).
	next := p.peekAt(i)
	switch next.Type {
	case lexer.TOKEN_LPAREN, lexer.TOKEN_LBRACKET, lexer.TOKEN_DOT,
		lexer.TOKEN_QUESTION_DOT:
		return false
	}
	return true
}

// v2ParseTypedVarStmt parses: Type name = expr  or  Type name (no value)
func (p *Parser) v2ParseTypedVarStmt() *VarStmt {
	line := p.peek().Line
	typ := p.v2ParseType()
	name := p.v2ExpectIdent()
	var val Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		val = p.v2ParseExpr()
	}
	handler := p.v2ParseErrHandler()
	return &VarStmt{Line: line, Name: name, Type: typ, Value: val, OrHandler: handler}
}

// v2ParseErrHandler previously accepted `or <default>` / `or { handler }` /
// v2ParseErrHandler parses an optional trailing error handler on a
// statement with a fallible RHS:
//
//	var x = call() or fallback            // short default expression
//	var x = call() or { ... }             // block handler; `err` in scope
//	var x = call() or match err { ... }   // typed-error dispatch
//
// Returns nil if no `or` follows. The block form can return/throw to
// propagate, or compute a fallback value — the codegen lowers both to
// `x, err := call(); if err != nil { <handler> }`.
func (p *Parser) v2ParseErrHandler() *OrHandler {
	if !p.check(lexer.TOKEN_OR) {
		return nil
	}
	p.advance() // consume "or"
	if p.check(lexer.TOKEN_MATCH) {
		return p.v2ParseOrMatch()
	}
	if p.check(lexer.TOKEN_LBRACE) {
		body := p.v2ParseBlock()
		return &OrHandler{Body: body}
	}
	// Short form: `or <expr>` — fallback value if the call errors.
	// Wrap as a single-statement block so codegen's short-default fast
	// path in emitOrAssignment can recognise it.
	expr := p.v2ParseExpr()
	return &OrHandler{Body: &BlockStmt{Stmts: []Stmt{&ExprStmt{Expr: expr}}}}
}

// v2ParseOrMatch: or match err { case Type -> body ... case _ -> body }
func (p *Parser) v2ParseOrMatch() *OrHandler {
	p.advance() // consume "match"

	// Parse the error variable name (e.g. "err")
	matchVar := "err"
	if p.check(lexer.TOKEN_IDENT) {
		matchVar = p.advance().Literal
	}

	p.expect(lexer.TOKEN_LBRACE)
	var cases []*OrMatchCase

	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		p.expect(lexer.TOKEN_CASE)

		errType := ""
		if p.check(lexer.TOKEN_IDENT) {
			lit := p.peek().Literal
			if lit == "_" {
				p.advance() // wildcard
			} else {
				errType = p.advance().Literal
			}
		}

		p.expect(lexer.TOKEN_ARROW) // ->

		// Parse body: a brace-block, a bare control-flow statement
		// (return / break / continue), or a single expression.
		var body *BlockStmt
		if p.check(lexer.TOKEN_LBRACE) {
			body = p.v2ParseBlock()
		} else if p.check(lexer.TOKEN_RETURN) ||
			p.check(lexer.TOKEN_BREAK) || p.check(lexer.TOKEN_CONTINUE) {
			stmt := p.v2ParseStmt()
			body = &BlockStmt{Stmts: []Stmt{stmt}}
		} else {
			expr := p.v2ParseExpr()
			body = &BlockStmt{Stmts: []Stmt{&ExprStmt{Expr: expr}}}
		}

		cases = append(cases, &OrMatchCase{Type: errType, Body: body})
	}
	p.expect(lexer.TOKEN_RBRACE)

	return &OrHandler{MatchCases: cases, MatchVar: matchVar}
}

func (p *Parser) v2ParseReturnStmt() *ReturnStmt {
	line := p.peek().Line
	p.advance() // consume return
	// Return with no value if next token is a block terminator
	if p.check(lexer.TOKEN_RBRACE) || p.check(lexer.TOKEN_ELSE) ||
		p.check(lexer.TOKEN_EOF) || p.check(lexer.TOKEN_RBRACE) {
		return &ReturnStmt{Line: line}
	}
	first := p.v2ParseExpr()
	// return a, b → return (a, b) tuple
	if p.check(lexer.TOKEN_COMMA) {
		elems := []Expr{first}
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			elems = append(elems, p.v2ParseExpr())
		}
		return &ReturnStmt{Line: line, Value: &TupleLit{Elements: elems}}
	}
	return &ReturnStmt{Line: line, Value: first}
}

// v2ParseIfStmt: if cond { } [else if cond { }] [else { }]
func (p *Parser) v2ParseIfStmt() *IfStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_IF)
	p.expect(lexer.TOKEN_LPAREN)
	cond := p.v2ParseExpr()
	p.expect(lexer.TOKEN_RPAREN)
	then := p.v2ParseBlock()

	var elseStmt Stmt
	if p.check(lexer.TOKEN_ELSE) {
		p.advance()
		if p.check(lexer.TOKEN_IF) {
			elseStmt = p.v2ParseIfStmt()
		} else {
			elseStmt = p.v2ParseBlock()
		}
	}
	return &IfStmt{Line: line, Cond: cond, Then: then, ElseStmt: elseStmt}
}

// v2ParseForStmt: for (item in expr) ... end  OR  for (i, item in expr) ... end
func (p *Parser) v2ParseForStmt() Stmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_FOR)
	p.expect(lexer.TOKEN_LPAREN)

	// for (item in expr)
	if p.v2IsIdent() && p.peekAt(1).Type == lexer.TOKEN_IN {
		item := p.advance().Literal
		p.advance() // consume "in"
		rangeExpr := p.v2ParseExpr()
		p.expect(lexer.TOKEN_RPAREN)
		body := p.v2ParseBlock()
		return &ForStmt{Line: line, IsRange: true, Item: item, Range: rangeExpr, Body: body}
	}

	// for (i, item in expr)
	if p.v2IsIdent() &&
		p.peekAt(1).Type == lexer.TOKEN_COMMA &&
		(p.peekAt(2).Type == lexer.TOKEN_IDENT || p.peekAt(2).Type == lexer.TOKEN_DATA) &&
		p.peekAt(3).Type == lexer.TOKEN_IN {
		indexVar := p.advance().Literal
		p.advance() // comma
		item := p.advance().Literal
		p.advance() // in
		rangeExpr := p.v2ParseExpr()
		p.expect(lexer.TOKEN_RPAREN)
		body := p.v2ParseBlock()
		return &ForStmt{Line: line, IsRange: true, IndexVar: indexVar, Item: item, Range: rangeExpr, Body: body}
	}

	// while-style for (bare condition) — shouldn't happen in v2, but handle gracefully
	cond := p.v2ParseExpr()
	p.expect(lexer.TOKEN_RPAREN)
	body := p.v2ParseBlock()
	return &WhileStmt{Line: line, Cond: cond, Body: body}
}

// v2ParseWhileStmt: while (cond) ... end
func (p *Parser) v2ParseWhileStmt() *WhileStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_WHILE)
	p.expect(lexer.TOKEN_LPAREN)
	cond := p.v2ParseExpr()
	p.expect(lexer.TOKEN_RPAREN)
	body := p.v2ParseBlock()
	return &WhileStmt{Line: line, Cond: cond, Body: body}
}

// v2ParseMatchStmt: match (expr) { case pat -> expr ... }
func (p *Parser) v2ParseMatchStmt() *MatchStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_MATCH)
	p.expect(lexer.TOKEN_LPAREN)
	subject := p.v2ParseExpr()
	p.expect(lexer.TOKEN_RPAREN)
	p.expect(lexer.TOKEN_LBRACE)
	var cases []*MatchCase
	p.skipSemis()
	for p.check(lexer.TOKEN_CASE) {
		p.advance() // consume case
		var pattern Expr
		if p.peek().Type == lexer.TOKEN_IDENT && p.peek().Literal == "_" {
			p.advance() // wildcard
		} else {
			pattern = p.v2ParseExpr()
		}

		if p.check(lexer.TOKEN_ARROW) {
			// Single-line case: case pat -> expr
			p.advance()
			stmt := p.v2ParseStmt()
			cases = append(cases, &MatchCase{Pattern: pattern, Body: &BlockStmt{Stmts: []Stmt{stmt}}})
		} else {
			// Multi-line case with braces: case pat { body }
			body := p.v2ParseBlock()
			cases = append(cases, &MatchCase{Pattern: pattern, Body: body})
		}
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &MatchStmt{Line: line, Subject: subject, Cases: cases}
}

// v2ParseSelectStmt: select { case ch.recv(): stmts ... case _: stmts }
//
// Each case is one of:
//   case <var> = <chan>.recv(): stmts   — recv with binding
//   case <chan>.recv():        stmts    — recv without binding
//   case <chan>.send(<expr>):  stmts    — send
//   case _:                    stmts    — default (fires when none ready)
//
// Body of each case extends until the next `case` or closing `}`.
// At most one `case _` per select. Maps 1:1 to Go's select statement.
func (p *Parser) v2ParseSelectStmt() *SelectStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_SELECT)
	p.expect(lexer.TOKEN_LBRACE)
	p.skipSemis()

	stmt := &SelectStmt{Line: line}
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		if !p.check(lexer.TOKEN_CASE) {
			p.errorf("expected `case` in select, got %s", p.peek().Literal)
			p.advance()
			continue
		}
		p.advance() // consume `case`

		// Default arm: `case _:`
		if p.peek().Type == lexer.TOKEN_IDENT && p.peek().Literal == "_" &&
			p.peekAt(1).Type == lexer.TOKEN_COLON {
			p.advance() // consume `_`
			p.expect(lexer.TOKEN_COLON)
			body := p.v2ParseSelectCaseBody()
			if stmt.Default != nil {
				p.errorf("select has more than one `case _` (default)")
			}
			stmt.Default = body
			p.skipSemis()
			continue
		}

		c := p.v2ParseSelectCase()
		if c != nil {
			stmt.Cases = append(stmt.Cases, c)
		}
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return stmt
}

// v2ParseSelectCase parses one `case` arm after the `case` keyword has been
// consumed: case-expr `:` body. The case-expr must be one of the three
// supported forms (recv, recv-with-binding, send) — anything else is a parse
// error so we don't silently accept arbitrary expressions.
func (p *Parser) v2ParseSelectCase() *SelectCase {
	// Detect `<var> = <chan>.recv()` shape via lookahead before parsing as
	// a generic expression — assignment isn't an expression in zinc, so
	// v2ParseExpr would reject the `=`.
	if p.v2IsIdent() && p.peekAt(1).Type == lexer.TOKEN_ASSIGN {
		bindName := p.v2ExpectIdent()
		p.advance() // consume `=`
		recvExpr := p.v2ParseExpr()
		p.expect(lexer.TOKEN_COLON)
		body := p.v2ParseSelectCaseBody()
		recvCh, ok := selectRecvChannel(recvExpr)
		if !ok {
			p.errorf("select case binding must be `<var> = <chan>.recv()`")
			return nil
		}
		return &SelectCase{Kind: "recv", Channel: recvCh, Binding: bindName, Body: body}
	}

	expr := p.v2ParseExpr()
	p.expect(lexer.TOKEN_COLON)
	body := p.v2ParseSelectCaseBody()

	// Unwrap to find the underlying call shape.
	if ch, ok := selectRecvChannel(expr); ok {
		return &SelectCase{Kind: "recv", Channel: ch, Body: body}
	}
	if ch, val, ok := selectSendChannel(expr); ok {
		return &SelectCase{Kind: "send", Channel: ch, SendValue: val, Body: body}
	}
	p.errorf("select case must be `<chan>.recv()`, `<var> = <chan>.recv()`, or `<chan>.send(<expr>)`")
	return nil
}

// v2ParseSelectCaseBody parses the statement list for one case until the
// next `case` or closing `}`. Statements separate by newline or `;`.
func (p *Parser) v2ParseSelectCaseBody() *BlockStmt {
	body := &BlockStmt{}
	p.skipSemis()
	for !p.check(lexer.TOKEN_CASE) &&
		!p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		s := p.v2ParseStmt()
		if s != nil {
			body.Stmts = append(body.Stmts, s)
		}
		p.skipSemis()
	}
	return body
}

// selectRecvChannel inspects an expression looking for `<chan>.recv()` and
// returns the channel sub-expression on match.
func selectRecvChannel(e Expr) (Expr, bool) {
	call, ok := e.(*CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := call.Callee.(*SelectorExpr)
	if !ok || sel.Field != "recv" || len(call.Args) != 0 {
		return nil, false
	}
	return sel.Object, true
}

// selectSendChannel inspects an expression looking for `<chan>.send(<value>)`
// and returns the channel + value on match.
func selectSendChannel(e Expr) (Expr, Expr, bool) {
	call, ok := e.(*CallExpr)
	if !ok {
		return nil, nil, false
	}
	sel, ok := call.Callee.(*SelectorExpr)
	if !ok || sel.Field != "send" || len(call.Args) != 1 {
		return nil, nil, false
	}
	return sel.Object, call.Args[0], true
}

// v2ParseWithStmt: with name = expr { } OR with expr { }
func (p *Parser) v2ParseWithStmt() *WithStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_WITH)

	var resources []*WithResource
	for {
		if p.check(lexer.TOKEN_VAR) {
			p.advance()
		}
		// Check if it's "name = expr" or just "expr"
		if p.v2IsIdent() && p.peekAt(1).Type == lexer.TOKEN_ASSIGN {
			name := p.v2ExpectIdent()
			p.advance() // consume =
			val := p.v2ParseExpr()
			resources = append(resources, &WithResource{Name: name, Value: val})
		} else {
			// Just an expression: with lock { }, with open("f") { }
			val := p.v2ParseExpr()
			resources = append(resources, &WithResource{Name: "_ctx", Value: val})
		}
		if !p.check(lexer.TOKEN_COMMA) {
			break
		}
		p.advance()
	}

	body := p.v2ParseBlock()
	return &WithStmt{Line: line, Resources: resources, Body: body}
}

// v2ParseLockStmt: lock (mu) { body }
func (p *Parser) v2ParseLockStmt() Stmt {
	line := p.peek().Line
	p.advance() // consume "lock"
	p.expect(lexer.TOKEN_LPAREN)
	lockName := p.expect(lexer.TOKEN_IDENT).Literal
	p.expect(lexer.TOKEN_RPAREN)
	body := p.v2ParseBlock()
	// Reuse WithStmt — single resource with the lock name
	return &WithStmt{
		Line:      line,
		Resources: []*WithResource{{Name: "_lock", Value: &Ident{Name: lockName}}},
		Body:      body,
	}
}

// v2ParseUsingStmt: using (var name = expr [, var name = expr]) { body }
// C#-style resource acquisition. Each named resource has .Close()
// deferred. Reuses WithStmt under the hood.
func (p *Parser) v2ParseUsingStmt() Stmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_USING)
	p.expect(lexer.TOKEN_LPAREN)
	var resources []*WithResource
	for {
		// Optional `var` keyword inside the using clause — same shape
		// as csharp `using (var f = ...)`. We don't require it.
		if p.check(lexer.TOKEN_VAR) {
			p.advance()
		}
		name := p.v2ExpectIdent()
		p.expect(lexer.TOKEN_ASSIGN)
		val := p.v2ParseExpr()
		resources = append(resources, &WithResource{Name: name, Value: val})
		if !p.check(lexer.TOKEN_COMMA) {
			break
		}
		p.advance() // consume ,
	}
	p.expect(lexer.TOKEN_RPAREN)
	body := p.v2ParseBlock()
	return &WithStmt{Line: line, Resources: resources, Body: body}
}

func (p *Parser) v2ParseSpawnStmt() Stmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_SPAWN)
	body := p.v2ParseBlock()
	spawn := &SpawnExpr{Line: line, Body: body}
	// Optional: spawn { } or { handler }
	var orHandler *OrHandler
	if p.check(lexer.TOKEN_OR) {
		p.advance()
		orHandler = &OrHandler{Body: p.v2ParseBlock()}
		spawn.OrHandler = orHandler
	}
	return &ExprStmt{Line: line, Expr: spawn}
}

// v2ParseParallelForStmt: parallel [(max: N)] for item in expr { body }
func (p *Parser) v2ParseParallelForStmt() *ParallelForStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_PARALLEL)

	// Optional: parallel(max: N) for ...
	maxConcurrency := 0
	if p.check(lexer.TOKEN_LPAREN) {
		p.advance()
		for !p.check(lexer.TOKEN_RPAREN) && !p.check(lexer.TOKEN_EOF) {
			name := p.v2ExpectIdent()
			p.expect(lexer.TOKEN_COLON)
			if name == "max" {
				tok := p.advance()
				val, err := strconv.Atoi(tok.Literal)
				if err == nil {
					maxConcurrency = val
				}
			} else {
				p.advance() // skip unknown param value
			}
			if p.check(lexer.TOKEN_COMMA) {
				p.advance()
			}
		}
		p.expect(lexer.TOKEN_RPAREN)
	}

	p.expect(lexer.TOKEN_FOR)
	p.expect(lexer.TOKEN_LPAREN)

	var item, indexVar string
	if p.v2IsIdent() && p.peekAt(1).Type == lexer.TOKEN_COMMA {
		indexVar = p.advance().Literal
		p.advance() // comma
		item = p.v2ExpectIdent()
	} else {
		item = p.v2ExpectIdent()
	}
	p.expect(lexer.TOKEN_IN)
	rangeExpr := p.v2ParseExpr()
	p.expect(lexer.TOKEN_RPAREN)
	body := p.v2ParseBlock()
	handler := p.v2ParseErrHandler()
	return &ParallelForStmt{Line: line, Item: item, IndexVar: indexVar, Range: rangeExpr, Body: body, OrHandler: handler, Max: maxConcurrency}
}

// v2ParseTimeoutStmt: timeout(dur) { body } [or { fallback }]
func (p *Parser) v2ParseTimeoutStmt() *TimeoutStmt {
	line := p.peek().Line
	p.advance() // consume "timeout"

	p.expect(lexer.TOKEN_LPAREN)
	dur := p.v2ParseExpr()
	p.expect(lexer.TOKEN_RPAREN)

	body := p.v2ParseBlock()
	handler := p.v2ParseErrHandler()

	return &TimeoutStmt{Line: line, Duration: dur, Body: body, OrHandler: handler}
}

// v2ParseFnDeclAsStmt parses a nested function definition as a statement.
func (p *Parser) v2ParseFnDeclAsStmt() *FnDecl {
	return p.v2ParseFnDecl()
}

// v2ParseAssertStmt: assert expr [, "message"]
func (p *Parser) v2ParseAssertStmt() *AssertStmt {
	line := p.peek().Line
	p.advance() // consume "assert" ident
	cond := p.v2ParseExpr()
	var msg Expr
	if p.check(lexer.TOKEN_COMMA) {
		p.advance()
		msg = p.v2ParseExpr()
	}
	return &AssertStmt{Line: line, Cond: cond, Message: msg}
}

// v2ParseExprOrAssignStmt handles expression statements and assignments.
func (p *Parser) v2ParseExprOrAssignStmt() Stmt {
	line := p.peek().Line
	expr := p.v2ParseExpr()

	// Check for assignment operators
	if p.match(lexer.TOKEN_ASSIGN, lexer.TOKEN_PLUS_EQ, lexer.TOKEN_MINUS_EQ,
		lexer.TOKEN_STAR_EQ, lexer.TOKEN_SLASH_EQ) {
		op := p.advance().Literal
		val := p.v2ParseExpr()
		return &AssignStmt{Line: line, Target: expr, Op: op, Value: val}
	}

	// Check for or handler on expression statements: call() or { ... }
	handler := p.v2ParseErrHandler()
	return &ExprStmt{Line: line, Expr: expr, OrHandler: handler}
}
