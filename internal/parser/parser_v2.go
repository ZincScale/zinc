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
	"zinc/internal/lexer"
)

// ParseV2 parses Zinc v2 syntax (end blocks, fn keyword, script mode).
// Returns a Program where top-level statements are wrapped in FnDecl named "main".
func (p *Parser) ParseV2() *Program {
	prog := &Program{}
	p.skipSemis()

	var topStmts []Stmt

	// Optional package declaration: package com.example.myapp
	if p.check(lexer.TOKEN_PACKAGE) {
		prog.Package = p.v2ParsePackageDecl()
		p.skipSemis()
	}

	for !p.check(lexer.TOKEN_EOF) {
		tok := p.peek()
		switch tok.Type {
		case lexer.TOKEN_IMPORT:
			prog.Imports = append(prog.Imports, p.v2ParseImport())
		case lexer.TOKEN_AT:
			annots := p.v2ParseAnnotations()
			if p.check(lexer.TOKEN_FN) {
				fn := p.v2ParseFnDecl()
				fn.Annotations = annots
				prog.Decls = append(prog.Decls, fn)
			} else if p.check(lexer.TOKEN_CLASS) {
				cls := p.v2ParseClassDecl()
				cls.Annotations = annots
				prog.Decls = append(prog.Decls, cls)
			} else if p.check(lexer.TOKEN_DATA) {
				// data class with annotations
				d := p.v2ParseDataClassDecl()
				prog.Decls = append(prog.Decls, d)
			} else {
				p.errorf("expected fn or class after decorator")
				p.advance()
			}
		case lexer.TOKEN_FN:
			prog.Decls = append(prog.Decls, p.v2ParseFnDecl())
		case lexer.TOKEN_CLASS:
			prog.Decls = append(prog.Decls, p.v2ParseClassDecl())
		case lexer.TOKEN_DATA:
			// Disambiguate: data Name { ... } (declaration) vs data = ... (variable)
			if p.peekAt(1).Type == lexer.TOKEN_IDENT {
				prog.Decls = append(prog.Decls, p.v2ParseDataClassDecl())
			} else {
				s := p.v2ParseStmt()
				if s != nil {
					topStmts = append(topStmts, s)
				}
			}
		case lexer.TOKEN_INTERFACE:
			prog.Decls = append(prog.Decls, p.v2ParseInterfaceDecl())
		case lexer.TOKEN_ENUM:
			prog.Decls = append(prog.Decls, p.v2ParseEnumDecl())
		case lexer.TOKEN_CONST:
			prog.Decls = append(prog.Decls, p.v2ParseConstDecl())
		default:
			// Check for contextual keyword: sealed class
			if tok.Type == lexer.TOKEN_IDENT && tok.Literal == "sealed" && p.peekAt(1).Type == lexer.TOKEN_CLASS {
				p.advance() // consume "sealed"
				cls := p.v2ParseClassDecl()
				cls.IsSealed = true
				prog.Decls = append(prog.Decls, cls)
				break
			}
			// Script mode — top-level statements
			s := p.v2ParseStmt()
			if s != nil {
				topStmts = append(topStmts, s)
			}
		}
		p.skipSemis()
	}

	// Wrap top-level statements in a synthetic __main__ function
	if len(topStmts) > 0 {
		prog.Stmts = topStmts
	}

	return prog
}

// v2IsIdent returns true if the current token can act as an identifier
// (includes contextual keywords like data, match, print).
func (p *Parser) v2IsIdent() bool {
	t := p.peek().Type
	return t == lexer.TOKEN_IDENT || t == lexer.TOKEN_DATA ||
		t == lexer.TOKEN_MATCH || t == lexer.TOKEN_PRINT
}

// v2ExpectIdent expects an identifier, allowing contextual keywords (data, match, etc.)
func (p *Parser) v2ExpectIdent() string {
	if p.v2IsIdent() {
		return p.advance().Literal
	}
	return p.expect(lexer.TOKEN_IDENT).Literal
}

// --- Body parsing ------------------------------------------------

// v2ParseBlock parses a brace-delimited block: { stmts }
func (p *Parser) v2ParseBlock() *BlockStmt {
	p.expect(lexer.TOKEN_LBRACE)
	var stmts []Stmt
	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		s := p.v2ParseStmt()
		if s != nil {
			stmts = append(stmts, s)
		}
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &BlockStmt{Stmts: stmts}
}

// v2ParseBody parses statements until it hits one of the terminator tokens.
// Does NOT consume the terminator — the caller handles that.
func (p *Parser) v2ParseBody(terminators ...lexer.TokenType) *BlockStmt {
	var stmts []Stmt
	p.skipSemis()
	for !p.check(lexer.TOKEN_EOF) {
		for _, t := range terminators {
			if p.check(t) {
				return &BlockStmt{Stmts: stmts}
			}
		}
		s := p.v2ParseStmt()
		if s != nil {
			stmts = append(stmts, s)
		}
		p.skipSemis()
	}
	return &BlockStmt{Stmts: stmts}
}

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
	case lexer.TOKEN_TRY:
		p.errorf("try/catch is not supported in Zinc — use 'or { }' or 'or match' instead")
		p.advance()
		return nil
	case lexer.TOKEN_RAISE:
		p.errorf("raise/throw is not supported in Zinc — use 'return Error(...)' instead")
		p.advance()
		return nil
	case lexer.TOKEN_SPAWN:
		return p.v2ParseSpawnStmt()
	case lexer.TOKEN_PARALLEL:
		return p.v2ParseParallelForStmt()
	case lexer.TOKEN_CONCURRENT:
		return p.v2ParseConcurrentStmt()
	case lexer.TOKEN_TIMEOUT:
		return p.v2ParseTimeoutStmt()
	case lexer.TOKEN_FN:
		return p.v2ParseFnDeclAsStmt()
	case lexer.TOKEN_IDENT:
		if tok.Literal == "assert" {
			return p.v2ParseAssertStmt()
		}
		if tok.Literal == "del" {
			return p.v2ParseDelStmt()
		}
		if tok.Literal == "yield" {
			return p.v2ParseYieldStmt()
		}
		if tok.Literal == "lock" && p.peekAt(1).Type == lexer.TOKEN_IDENT {
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
		names = append(names, p.v2ExpectIdent())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			names = append(names, p.v2ExpectIdent())
		}
		p.expect(lexer.TOKEN_RPAREN)
		p.expect(lexer.TOKEN_ASSIGN)
		// Check for concurrent { ... }
		if p.check(lexer.TOKEN_CONCURRENT) {
			cs := p.v2ParseConcurrentStmt()
			cs.Names = names
			return cs
		}
		val := p.v2ParseExpr()
		return &TupleVarStmt{Line: line, Names: names, Value: val}
	}

	if p.v2IsTypeAnnotation() {
		// Type is present: var int x = 5, var list<int> nums = []
		typ = p.v2ParseType()
		name = p.v2ExpectIdent()
	} else {
		// No type (inferred): var x = 5
		name = p.v2ExpectIdent()

		// Tuple unpacking: var a, b = expr
		if p.check(lexer.TOKEN_COMMA) {
			names := []string{name}
			for p.check(lexer.TOKEN_COMMA) {
				p.advance()
				names = append(names, p.expect(lexer.TOKEN_IDENT).Literal)
			}
			p.expect(lexer.TOKEN_ASSIGN)
			// Check for concurrent { ... }
			if p.check(lexer.TOKEN_CONCURRENT) {
				cs := p.v2ParseConcurrentStmt()
				cs.Names = names
				return cs
			}
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
	// Exclude contextual keywords that start special statements (assert, del, yield, lock)
	if tok.Literal == "assert" || tok.Literal == "del" || tok.Literal == "yield" || tok.Literal == "lock" {
		return false
	}
	i++
	// After name, must see = or end-of-statement (not ( which would be a function call)
	next := p.peekAt(i)
	return next.Type == lexer.TOKEN_ASSIGN || next.Type == lexer.TOKEN_RBRACE ||
		next.Type == lexer.TOKEN_EOF
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

// v2ParseOrHandler checks for `or` after a failable call.
// Three forms:
//   var x = call() or 0                              — single-expression default
//   var x = call() or { ... }                        — multi-statement handler block
//   var x = call() or match err { case Type -> ... }  — typed error matching
// Returns nil if no or follows.
func (p *Parser) v2ParseErrHandler() *OrHandler {
	if !p.check(lexer.TOKEN_OR) {
		return nil
	}
	p.advance() // consume "or"

	// or match err { case Type -> ... }
	if p.check(lexer.TOKEN_MATCH) {
		return p.v2ParseOrMatch()
	}

	// Brace block: or { handler }
	if p.check(lexer.TOKEN_LBRACE) {
		body := p.v2ParseBlock()
		return &OrHandler{Body: body}
	}

	// Single-expression default: or 0
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

		// Parse body: either a single expression or a block
		var body *BlockStmt
		if p.check(lexer.TOKEN_LBRACE) {
			body = p.v2ParseBlock()
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
	cond := p.v2ParseExpr()
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

// v2ParseForStmt: for item in expr ... end  OR  for i, item in expr ... end
func (p *Parser) v2ParseForStmt() Stmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_FOR)

	// for item in expr
	if p.v2IsIdent() && p.peekAt(1).Type == lexer.TOKEN_IN {
		item := p.advance().Literal
		p.advance() // consume "in"
		rangeExpr := p.v2ParseExpr()
		body := p.v2ParseBlock()
		return &ForStmt{Line: line, IsRange: true, Item: item, Range: rangeExpr, Body: body}
	}

	// for i, item in expr
	if p.v2IsIdent() &&
		p.peekAt(1).Type == lexer.TOKEN_COMMA &&
		(p.peekAt(2).Type == lexer.TOKEN_IDENT || p.peekAt(2).Type == lexer.TOKEN_DATA) &&
		p.peekAt(3).Type == lexer.TOKEN_IN {
		indexVar := p.advance().Literal
		p.advance() // comma
		item := p.advance().Literal
		p.advance() // in
		rangeExpr := p.v2ParseExpr()
		body := p.v2ParseBlock()
		return &ForStmt{Line: line, IsRange: true, IndexVar: indexVar, Item: item, Range: rangeExpr, Body: body}
	}

	// while-style for (bare condition) — shouldn't happen in v2, but handle gracefully
	cond := p.v2ParseExpr()
	body := p.v2ParseBlock()
	return &WhileStmt{Line: line, Cond: cond, Body: body}
}

// v2ParseWhileStmt: while cond ... end
func (p *Parser) v2ParseWhileStmt() *WhileStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_WHILE)
	cond := p.v2ParseExpr()
	body := p.v2ParseBlock()
	return &WhileStmt{Line: line, Cond: cond, Body: body}
}

// v2ParseMatchStmt: match expr { case pat -> expr ... }
func (p *Parser) v2ParseMatchStmt() *MatchStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_MATCH)
	subject := p.v2ParseExpr()
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

// v2ParseTryStmt: try { } catch ExType err { }  OR  try { } catch err { }
func (p *Parser) v2ParseTryStmt() Stmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_TRY)
	tryBody := p.v2ParseBlock()

	if !p.check(lexer.TOKEN_CATCH) {
		return &ExprStmt{Line: line, Expr: &Ident{Name: "__try__"}}
	}

	p.advance() // consume catch
	// Type-first: catch ExType err { }  OR  untyped: catch err { }
	errName := ""
	errType := ""
	if p.check(lexer.TOKEN_IDENT) {
		first := p.advance().Literal
		if p.check(lexer.TOKEN_IDENT) {
			// Two idents: first is type, second is name
			errType = first
			errName = p.advance().Literal
		} else {
			// One ident: just the name (no type)
			errName = first
		}
	}

	catchBody := p.v2ParseBlock()

	return &TryStmt{
		Line:      line,
		Body:      tryBody,
		CatchName: errName,
		CatchType: errType,
		CatchBody: catchBody,
	}
}

// v2ParseRaiseStmt: raise expr [from expr]
func (p *Parser) v2ParseRaiseStmt() *RaiseStmt {
	line := p.peek().Line
	p.advance() // consume raise
	val := p.v2ParseExpr()
	var from Expr
	if p.check(lexer.TOKEN_FROM) {
		p.advance()
		from = p.v2ParseExpr()
	}
	return &RaiseStmt{Line: line, Value: val, From: from}
}

// v2ParseYieldStmt: yield [expr]
func (p *Parser) v2ParseYieldStmt() *YieldStmt {
	line := p.peek().Line
	p.advance() // consume "yield" ident
	// Bare yield if next is end/else/rbrace/EOF
	if p.check(lexer.TOKEN_RBRACE) || p.check(lexer.TOKEN_ELSE) ||
		p.check(lexer.TOKEN_EOF) || p.check(lexer.TOKEN_RBRACE) {
		return &YieldStmt{Line: line}
	}
	return &YieldStmt{Line: line, Value: p.v2ParseExpr()}
}

// v2ParseSpawnStmt: spawn { body } or var x = spawn { expr }
// v2ParseLockStmt: lock mu { body }
func (p *Parser) v2ParseLockStmt() Stmt {
	line := p.peek().Line
	p.advance() // consume "lock"
	lockName := p.expect(lexer.TOKEN_IDENT).Literal
	body := p.v2ParseBlock()
	// Reuse WithStmt — single resource with the lock name
	return &WithStmt{
		Line: line,
		Resources: []*WithResource{{Name: "_lock", Value: &Ident{Name: lockName}}},
		Body: body,
	}
}

func (p *Parser) v2ParseSpawnStmt() Stmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_SPAWN)
	body := p.v2ParseBlock()
	return &ExprStmt{Line: line, Expr: &SpawnExpr{Line: line, Body: body}}
}

// v2ParseParallelForStmt: parallel for item in expr { body }
func (p *Parser) v2ParseParallelForStmt() *ParallelForStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_PARALLEL)
	p.expect(lexer.TOKEN_FOR)

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
	body := p.v2ParseBlock()
	handler := p.v2ParseErrHandler()
	return &ParallelForStmt{Line: line, Item: item, IndexVar: indexVar, Range: rangeExpr, Body: body, OrHandler: handler}
}

// v2ParseConcurrentStmt: concurrent { task1; task2 } or concurrent(first: true) { ... }
func (p *Parser) v2ParseConcurrentStmt() *ConcurrentStmt {
	line := p.peek().Line
	p.advance() // consume "concurrent"

	firstOnly := false
	// Check for concurrent(first: true)
	if p.check(lexer.TOKEN_LPAREN) {
		p.advance()
		for !p.check(lexer.TOKEN_RPAREN) && !p.check(lexer.TOKEN_EOF) {
			name := p.v2ExpectIdent()
			p.expect(lexer.TOKEN_COLON)
			if name == "first" {
				val := p.v2ExpectIdent()
				if val == "true" {
					firstOnly = true
				}
			} else {
				p.v2ParseExpr() // skip unknown named args
			}
			if p.check(lexer.TOKEN_COMMA) {
				p.advance()
			}
		}
		p.expect(lexer.TOKEN_RPAREN)
	}

	// Parse block — each statement is a concurrent task
	p.expect(lexer.TOKEN_LBRACE)
	var tasks []Expr
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		expr := p.v2ParseExpr()
		tasks = append(tasks, expr)
	}
	p.expect(lexer.TOKEN_RBRACE)

	handler := p.v2ParseErrHandler()

	return &ConcurrentStmt{Line: line, Tasks: tasks, FirstOnly: firstOnly, OrHandler: handler}
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

// v2ParseDelStmt: del expr
func (p *Parser) v2ParseDelStmt() *DelStmt {
	line := p.peek().Line
	p.advance() // consume "del" ident
	target := p.v2ParseExpr()
	return &DelStmt{Line: line, Target: target}
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

// --- Declarations ------------------------------------------------------------

// v2ParseFnDecl: fn name(params)[: ReturnType] { body }
//                fn name(params)[: ReturnType] = expr  (single-expression)
func (p *Parser) v2ParseFnDecl() *FnDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_FN)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	typeParams := p.parseTypeParams()
	params := p.v2ParseParamList()

	// Optional return type after colon: fn name(params): Type { }
	var retType TypeExpr
	if p.check(lexer.TOKEN_COLON) {
		p.advance() // consume ':'
		retType = p.v2ParseType()
	}

	// Single-expression form: fn name(params) Type = expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		expr := p.v2ParseExpr()
		body := &BlockStmt{Stmts: []Stmt{&ReturnStmt{Line: line, Value: expr}}}
		return &FnDecl{Line: line, Name: name, TypeParams: typeParams, Params: params, ReturnType: retType, Body: body}
	}

	body := p.v2ParseBlock()
	return &FnDecl{Line: line, Name: name, TypeParams: typeParams, Params: params, ReturnType: retType, Body: body}
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
		name = p.v2ExpectIdent()
	} else {
		// Untyped param or variadic: x, *args, **kwargs
		name = p.v2ExpectIdent()
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
	typeParams := p.parseTypeParams()

	// Optional parent class/interfaces: class Dog : Animal, Serializable
	var parents []string
	if p.check(lexer.TOKEN_COLON) {
		p.advance()
		parents = append(parents, p.expect(lexer.TOKEN_IDENT).Literal)
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			parents = append(parents, p.expect(lexer.TOKEN_IDENT).Literal)
		}
	}

	var fields []*FieldDecl
	var methods []*MethodDecl
	var variants []*DataClassDecl
	var ctor *CtorDecl

	p.expect(lexer.TOKEN_LBRACE)
	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		tok := p.peek()

		if tok.Type == lexer.TOKEN_DATA {
			// Sealed class variant: data Circle(double radius)
			variants = append(variants, p.v2ParseDataClassDecl())
		} else if tok.Type == lexer.TOKEN_AT {
			annots := p.v2ParseAnnotations()
			m := p.v2ParseMethodDecl()
			m.Annotations = annots
			methods = append(methods, m)
		} else if tok.Type == lexer.TOKEN_OVERRIDE {
			// override fn name(...) { ... }
			p.advance() // consume override
			m := p.v2ParseMethodDecl()
			m.IsPub = true // override methods are always public
			m.Annotations = append(m.Annotations, &Annotation{Name: "Override"})
			methods = append(methods, m)
		} else if tok.Type == lexer.TOKEN_PUB {
			next := p.peekAt(1)
			if next.Type == lexer.TOKEN_FN {
				// pub fn name(...) { ... }
				p.advance() // consume pub
				m := p.v2ParseMethodDecl()
				m.IsPub = true
				methods = append(methods, m)
			} else if next.Type == lexer.TOKEN_OVERRIDE {
				// pub override fn name(...) { ... }
				p.advance() // consume pub
				p.advance() // consume override
				m := p.v2ParseMethodDecl()
				m.IsPub = true
				m.Annotations = append(m.Annotations, &Annotation{Name: "Override"})
				methods = append(methods, m)
			} else {
				// pub Type name — public field (no var keyword needed)
				p.advance() // consume pub
				f := p.v2ParseFieldDeclNoKeyword()
				f.IsPub = true
				fields = append(fields, f)
			}
		} else if tok.Type == lexer.TOKEN_READONLY {
			// read Type name — read-only field (no var keyword needed)
			p.advance() // consume read
			f := p.v2ParseFieldDeclNoKeyword()
			f.IsReadonly = true
			fields = append(fields, f)
		} else if tok.Type == lexer.TOKEN_FN {
			m := p.v2ParseMethodDecl()
			m.IsPub = false // private by default
			methods = append(methods, m)
		} else if tok.Type == lexer.TOKEN_INIT && p.peekAt(1).Type == lexer.TOKEN_LPAREN {
			// init(params) { body } — constructor
			p.advance() // consume init
			params := p.v2ParseParamList()
			body := p.v2ParseBlock()
			// Extract super(...) call from body if present
			var superArgs []Expr
			var filteredStmts []Stmt
			for _, s := range body.Stmts {
				if es, ok := s.(*ExprStmt); ok {
					if call, ok := es.Expr.(*CallExpr); ok {
						if ident, ok := call.Callee.(*Ident); ok && ident.Name == "super" {
							superArgs = call.Args
							continue
						}
					}
				}
				filteredStmts = append(filteredStmts, s)
			}
			body.Stmts = filteredStmts
			ctor = &CtorDecl{Params: params, Body: body, SuperArgs: superArgs}
		} else if tok.Type == lexer.TOKEN_VAR || tok.Type == lexer.TOKEN_CONST || tok.Type == lexer.TOKEN_INIT {
			f := p.v2ParseFieldDecl()
			fields = append(fields, f)
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
	return &ClassDecl{Line: line, Name: name, TypeParams: typeParams, Parents: parents, Fields: fields, Ctor: ctor, Methods: methods, Variants: variants}
}

// v2ParseMethodDecl: fn name(params)[: ReturnType] { body }
func (p *Parser) v2ParseMethodDecl() *MethodDecl {
	_ = p.peek().Line
	p.expect(lexer.TOKEN_FN)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	params := p.v2ParseParamList()

	// Optional return type after colon: fn name(params): Type { }
	var retType TypeExpr
	if p.check(lexer.TOKEN_COLON) {
		p.advance() // consume ':'
		retType = p.v2ParseType()
	}

	body := p.v2ParseBlock()
	return &MethodDecl{Name: name, Params: params, ReturnType: retType, Body: body}
}

// v2ParseFieldDecl: var type name [= default]  |  const type name = default  |  init type name
func (p *Parser) v2ParseFieldDecl() *FieldDecl {
	isConst := p.peek().Type == lexer.TOKEN_CONST
	isInit := p.peek().Type == lexer.TOKEN_INIT
	p.advance() // consume var/const/init

	// Type-first: type name
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

// v2IsClassFieldDecl checks if the current IDENT in a class body starts a field
// declaration (Type name [= default]) rather than a method or expression.
func (p *Parser) v2IsClassFieldDecl() bool {
	if !p.v2IsTypeAnnotation() {
		return false
	}
	// Scan past the type to find the name
	i := 1
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
		next.Type == lexer.TOKEN_EOF
}

// v2ParseDataClassDecl: data Name(Type field, Type field = default) [{ methods }]
func (p *Parser) v2ParseDataClassDecl() *DataClassDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_DATA)
	name := p.expect(lexer.TOKEN_IDENT).Literal
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

// v2ParseDataClassParam: Type name [= default]
func (p *Parser) v2ParseDataClassParam() *FieldDecl {
	typ := p.v2ParseType()
	name := p.v2ExpectIdent()
	var def Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		def = p.v2ParseExpr()
	}
	return &FieldDecl{Name: name, IsPub: true, Type: typ, Default: def}
}

// v2ParseInterfaceDecl: interface Name { fn method(params): ReturnType ... }
func (p *Parser) v2ParseInterfaceDecl() *InterfaceDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_INTERFACE)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.expect(lexer.TOKEN_LBRACE)
	var methods []*MethodSig
	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		p.expect(lexer.TOKEN_FN)
		mName := p.expect(lexer.TOKEN_IDENT).Literal
		params := p.v2ParseParamList()
		var retType TypeExpr
		if p.check(lexer.TOKEN_COLON) {
			p.advance()
			retType = p.v2ParseType()
		}
		methods = append(methods, &MethodSig{Name: mName, Params: params, ReturnType: retType})
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &InterfaceDecl{Line: line, Name: name, Methods: methods}
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
	p.expect(lexer.TOKEN_ASSIGN)
	val := p.v2ParseExpr()
	return &ConstDecl{Line: line, Name: name, Type: typ, Value: val}
}

// v2ParseEnumDecl: enum Name { variants }
func (p *Parser) v2ParseEnumDecl() *EnumDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_ENUM)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.expect(lexer.TOKEN_LBRACE)
	var variants []string
	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		variants = append(variants, p.expect(lexer.TOKEN_IDENT).Literal)
		if p.check(lexer.TOKEN_COMMA) {
			p.advance()
		}
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &EnumDecl{Line: line, Name: name, Variants: variants}
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

// v2ParseImport: import java.util.List  OR  import java.util.*
func (p *Parser) v2ParseImport() *ImportDecl {
	p.expect(lexer.TOKEN_IMPORT)
	path := p.v2ExpectIdentOrKeyword()
	for p.check(lexer.TOKEN_DOT) {
		p.advance()
		if p.check(lexer.TOKEN_STAR) {
			// import java.util.*
			p.advance()
			path += ".*"
			break
		}
		path += "." + p.v2ExpectIdentOrKeyword()
	}
	return &ImportDecl{Path: path}
}

// v2ExpectIdentOrKeyword consumes and returns the current token's literal
// if it is an IDENT or any keyword token that could appear as a Java
// package/class name segment (e.g., "concurrent" in java.util.concurrent).
func (p *Parser) v2ExpectIdentOrKeyword() string {
	tok := p.peek()
	if tok.Type == lexer.TOKEN_IDENT || tok.Type == lexer.TOKEN_CONCURRENT ||
		tok.Type == lexer.TOKEN_DATA || tok.Type == lexer.TOKEN_MATCH ||
		tok.Type == lexer.TOKEN_PRINT || tok.Type == lexer.TOKEN_SPAWN ||
		tok.Type == lexer.TOKEN_INTERFACE {
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
				// Collect raw arg tokens as strings
				tok := p.advance()
				args = append(args, tok.Literal)
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

// --- Types (v2: angle-bracket generics <>) -----------------------------------

// v2ParseType: str, int, list<int>, dict<str, int>, str?
func (p *Parser) v2ParseType() TypeExpr {
	tok := p.expect(lexer.TOKEN_IDENT)
	name := tok.Literal

	var typ TypeExpr

	// Angle-bracket generics: List<int>, Map<String, int>
	if p.check(lexer.TOKEN_LT) {
		p.advance() // consume <
		var args []TypeExpr
		args = append(args, p.v2ParseType())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			args = append(args, p.v2ParseType())
		}
		p.expect(lexer.TOKEN_GT)
		typ = &GenericType{Name: name, TypeArgs: args}
	} else {
		typ = &SimpleType{Name: name}
	}

	// Array suffix: Type[] or Type<T>[]
	if p.check(lexer.TOKEN_LBRACKET) && p.peekAt(1).Type == lexer.TOKEN_RBRACKET {
		p.advance() // consume [
		p.advance() // consume ]
		typ = &ArrayType{ElementType: typ}
	}

	// Optional suffix: Type?, Type[]?
	if p.check(lexer.TOKEN_QUESTION) {
		p.advance()
		return &OptionalType{Inner: typ}
	}

	return typ
}

// v2IsTypeAnnotation checks if the current position looks like a type followed
// by a name (for var/const/init declarations). Returns true for patterns like:
//   ident ident    → simple type + name
//   ident<         → generic type
//   ident? ident   → nullable type + name
func (p *Parser) v2IsTypeAnnotation() bool {
	if !p.v2IsIdent() {
		return false
	}
	next := p.peekAt(1)
	// ident ident → type name
	if next.Type == lexer.TOKEN_IDENT || next.Type == lexer.TOKEN_DATA ||
		next.Type == lexer.TOKEN_MATCH || next.Type == lexer.TOKEN_PRINT {
		return true
	}
	// ident< → generic type
	if next.Type == lexer.TOKEN_LT {
		return true
	}
	// ident[] ident → array type + name
	if next.Type == lexer.TOKEN_LBRACKET {
		peek2 := p.peekAt(2)
		if peek2.Type == lexer.TOKEN_RBRACKET {
			peek3 := p.peekAt(3)
			return peek3.Type == lexer.TOKEN_IDENT || peek3.Type == lexer.TOKEN_DATA ||
				peek3.Type == lexer.TOKEN_MATCH || peek3.Type == lexer.TOKEN_PRINT
		}
	}
	// ident? ident → nullable type + name
	if next.Type == lexer.TOKEN_QUESTION {
		peek2 := p.peekAt(2)
		return peek2.Type == lexer.TOKEN_IDENT || peek2.Type == lexer.TOKEN_DATA ||
			peek2.Type == lexer.TOKEN_MATCH || peek2.Type == lexer.TOKEN_PRINT
	}
	return false
}

// --- Expressions -------------------------------------------------------------

// v2ParseExpr is the top-level expression parser for v2.
func (p *Parser) v2ParseExpr() Expr {
	return p.v2ParseTernary()
}

// v2ParseTernary: if cond: trueVal else: falseVal  (expression if)
func (p *Parser) v2ParseTernary() Expr {
	if p.check(lexer.TOKEN_IF) {
		return p.v2ParseIfExpr()
	}
	return p.v2ParseOr()
}

// v2ParseIfExpr: if cond: trueVal else: falseVal
func (p *Parser) v2ParseIfExpr() Expr {
	p.expect(lexer.TOKEN_IF)
	cond := p.v2ParseOr()
	p.expect(lexer.TOKEN_COLON)
	then := p.v2ParseOr()
	p.expect(lexer.TOKEN_ELSE)
	if p.check(lexer.TOKEN_IF) {
		// Chained: else if cond: val else: val
		elseExpr := p.v2ParseIfExpr()
		return &IfExpr{Cond: cond, Then: then, Else: elseExpr}
	}
	p.expect(lexer.TOKEN_COLON)
	elseExpr := p.v2ParseOr()
	return &IfExpr{Cond: cond, Then: then, Else: elseExpr}
}

// v2ParseOr: expr or expr
func (p *Parser) v2ParseOr() Expr {
	left := p.v2ParseAnd()
	for p.check(lexer.TOKEN_PIPE_PIPE) {
		op := p.advance().Literal
		right := p.v2ParseAnd()
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left
}

// v2ParseAnd: expr and expr
func (p *Parser) v2ParseAnd() Expr {
	left := p.v2ParseNot()
	for p.check(lexer.TOKEN_AMP_AMP) {
		op := p.advance().Literal
		right := p.v2ParseNot()
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left
}

// v2ParseNot: not expr
func (p *Parser) v2ParseNot() Expr {
	if p.check(lexer.TOKEN_NOT) || p.check(lexer.TOKEN_BANG) {
		op := p.advance().Literal
		if op == "not" {
			op = "!" // normalize
		}
		operand := p.v2ParseNot()
		return &UnaryExpr{Op: op, Operand: operand}
	}
	return p.v2ParseComparison()
}

// v2ParseComparison: expr (== != < <= > >= is in not_in) expr
func (p *Parser) v2ParseComparison() Expr {
	left := p.v2ParseRange()
	for {
		// Handle "not in" as a compound operator
		if p.check(lexer.TOKEN_NOT) && p.peekAt(1).Type == lexer.TOKEN_IN {
			p.advance() // consume not
			p.advance() // consume in
			right := p.v2ParseAddSub()
			left = &BinaryExpr{Left: left, Op: "not in", Right: right}
			continue
		}
		// Handle "is not" as a compound operator
		if p.check(lexer.TOKEN_IS) && p.peekAt(1).Type == lexer.TOKEN_NOT {
			p.advance() // consume is
			p.advance() // consume not
			right := p.v2ParseAddSub()
			left = &BinaryExpr{Left: left, Op: "is not", Right: right}
			continue
		}
		if !p.match(lexer.TOKEN_EQ, lexer.TOKEN_NEQ, lexer.TOKEN_REF_EQ, lexer.TOKEN_REF_NEQ,
			lexer.TOKEN_LT, lexer.TOKEN_LTE, lexer.TOKEN_GT, lexer.TOKEN_GTE,
			lexer.TOKEN_IS, lexer.TOKEN_IN) {
			break
		}
		op := p.advance().Literal
		right := p.v2ParseAddSub()
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left
}

// v2ParseRange: expr .. expr, expr ..= expr
func (p *Parser) v2ParseRange() Expr {
	left := p.v2ParseAddSub()
	if p.check(lexer.TOKEN_DOTDOT) {
		p.advance()
		right := p.v2ParseAddSub()
		return &RangeExpr{Start: left, End: right, Inclusive: false}
	}
	if p.check(lexer.TOKEN_DOTDOTEQ) {
		p.advance()
		right := p.v2ParseAddSub()
		return &RangeExpr{Start: left, End: right, Inclusive: true}
	}
	return left
}

// v2ParseAddSub: expr (+|-) expr
func (p *Parser) v2ParseAddSub() Expr {
	left := p.v2ParseMulDiv()
	for p.match(lexer.TOKEN_PLUS, lexer.TOKEN_MINUS) {
		op := p.advance().Literal
		right := p.v2ParseMulDiv()
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left
}

// v2ParseMulDiv: expr (*|/|%) expr
func (p *Parser) v2ParseMulDiv() Expr {
	left := p.v2ParseUnary()
	for p.match(lexer.TOKEN_STAR, lexer.TOKEN_SLASH, lexer.TOKEN_PERCENT) {
		op := p.advance().Literal
		right := p.v2ParseUnary()
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left
}

// v2ParseUnary: -expr
func (p *Parser) v2ParseUnary() Expr {
	if p.check(lexer.TOKEN_MINUS) {
		op := p.advance().Literal
		operand := p.v2ParseUnary()
		return &UnaryExpr{Op: op, Operand: operand}
	}
	return p.v2ParsePower()
}

// v2ParsePower: base ** exponent (right-associative)
func (p *Parser) v2ParsePower() Expr {
	base := p.v2ParsePostfix()
	if p.check(lexer.TOKEN_STAR_STAR) {
		p.advance()
		exp := p.v2ParseUnary() // right-associative
		return &BinaryExpr{Left: base, Op: "**", Right: exp}
	}
	return base
}

// v2ParsePostfix: primary followed by .field, [index], (args)
func (p *Parser) v2ParsePostfix() Expr {
	expr := p.v2ParsePrimary()
	for {
		switch {
		case p.check(lexer.TOKEN_QUESTION_DOT):
			// Safe navigation: obj?.field or obj?.method(args)
			p.advance()
			field := p.expect(lexer.TOKEN_IDENT).Literal
			if p.check(lexer.TOKEN_LPAREN) {
				// obj?.method(args)
				call := p.v2ParseCallArgs(&Ident{Name: field})
				callExpr := call.(*CallExpr)
				expr = &SafeNavExpr{Object: expr, Field: field, Call: callExpr}
			} else {
				expr = &SafeNavExpr{Object: expr, Field: field}
			}
		case p.check(lexer.TOKEN_DOT):
			p.advance()
			field := p.v2ExpectIdentOrKeyword()
			expr = &SelectorExpr{Object: expr, Field: field}
		case p.check(lexer.TOKEN_LBRACKET):
			p.advance()
			// Check for slice: obj[low:high]
			if p.check(lexer.TOKEN_COLON) {
				p.advance()
				var high Expr
				if !p.check(lexer.TOKEN_RBRACKET) {
					high = p.v2ParseExpr()
				}
				p.expect(lexer.TOKEN_RBRACKET)
				expr = &SliceExpr{Object: expr, High: high}
			} else {
				idx := p.v2ParseExpr()
				if p.check(lexer.TOKEN_COLON) {
					p.advance()
					var high Expr
					if !p.check(lexer.TOKEN_RBRACKET) {
						high = p.v2ParseExpr()
					}
					p.expect(lexer.TOKEN_RBRACKET)
					expr = &SliceExpr{Object: expr, Low: idx, High: high}
				} else {
					p.expect(lexer.TOKEN_RBRACKET)
					expr = &IndexExpr{Object: expr, Index: idx}
				}
			}
		case p.check(lexer.TOKEN_LPAREN):
			expr = p.v2ParseCallArgs(expr)
		default:
			return expr
		}
	}
}

// v2ParseCallArgs: callee(args)
func (p *Parser) v2ParseCallArgs(callee Expr) Expr {
	p.expect(lexer.TOKEN_LPAREN)
	var args []Expr
	var namedArgs []NamedArg

	if !p.check(lexer.TOKEN_RPAREN) {
		p.v2ParseCallArg(&args, &namedArgs)
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			if p.check(lexer.TOKEN_RPAREN) {
				break
			}
			p.v2ParseCallArg(&args, &namedArgs)
		}
	}
	p.expect(lexer.TOKEN_RPAREN)
	return &CallExpr{Callee: callee, Args: args, NamedArgs: namedArgs}
}

// v2ParseCallArg: expr or name=expr
// Note: comprehensions [expr for var in iter] are parsed as regular expressions
// and auto-promoted to generators by codegen when inside sum(), any(), etc.
func (p *Parser) v2ParseCallArg(args *[]Expr, namedArgs *[]NamedArg) {
	// Check for named arg: ident = expr
	if p.check(lexer.TOKEN_IDENT) && p.peekAt(1).Type == lexer.TOKEN_ASSIGN {
		name := p.advance().Literal
		p.advance() // consume =
		val := p.v2ParseExpr()
		*namedArgs = append(*namedArgs, NamedArg{Name: name, Value: val})
		return
	}
	arg := p.v2ParseExpr()
	*args = append(*args, arg)
}

// v2ParsePrimary: literals, identifiers, parens, lists, dicts, lambdas
func (p *Parser) v2ParsePrimary() Expr {
	tok := p.peek()

	switch tok.Type {
	case lexer.TOKEN_INT_LIT:
		p.advance()
		return &IntLit{Value: tok.Literal}
	case lexer.TOKEN_FLOAT_LIT:
		p.advance()
		return &FloatLit{Value: tok.Literal}
	case lexer.TOKEN_STRING_LIT:
		p.advance()
		return &StringLit{Value: tok.Literal}
	case lexer.TOKEN_INTERP_STRING:
		p.advance()
		return p.v2ParseInterpString(tok.Literal)
	case lexer.TOKEN_RAW_STRING:
		p.advance()
		return &RawStringLit{Value: tok.Literal}
	case lexer.TOKEN_BOOL_LIT:
		p.advance()
		return &BoolLit{Value: tok.Literal == "true"}
	case lexer.TOKEN_NULL, lexer.TOKEN_NONE:
		p.advance()
		return &NullLit{}
	case lexer.TOKEN_SPAWN:
		line := tok.Line
		p.advance()
		body := p.v2ParseBlock()
		return &SpawnExpr{Line: line, Body: body}
	case lexer.TOKEN_THIS:
		p.advance()
		return &Ident{Name: "this"}
	case lexer.TOKEN_SUPER:
		p.advance()
		return &Ident{Name: "super"}
	case lexer.TOKEN_IDENT, lexer.TOKEN_PRINT, lexer.TOKEN_DATA:
		// print and data are regular identifiers in expression context
		// Check for lambda: name -> expr
		if p.peekAt(1).Type == lexer.TOKEN_ARROW {
			return p.v2ParseLambda()
		}
		p.advance()
		return &Ident{Name: tok.Literal}
	case lexer.TOKEN_LPAREN:
		// Could be: (expr), tuple, or (params) -> expr lambda
		return p.v2ParseParenOrLambda()
	case lexer.TOKEN_LBRACKET:
		return p.v2ParseListLit()
	case lexer.TOKEN_LBRACE:
		return p.v2ParseDictLit()
	}

	p.errorf("unexpected token %s (%q) in expression", tok.Type, tok.Literal)
	p.advance()
	return &Ident{Name: "__error__"}
}

// v2ParseLambda: name -> expr
func (p *Parser) v2ParseLambda() Expr {
	name := p.advance().Literal // param name
	p.advance()                 // consume ->
	// Lambda body — parse expression
	expr := p.v2ParseExpr()
	param := &ParamDecl{Name: name}
	return &LambdaExpr{Params: []*ParamDecl{param}, Expr: expr}
}

// v2ParseParenOrLambda: (expr), (a, b) -> expr, or (a, b, c) tuple
func (p *Parser) v2ParseParenOrLambda() Expr {
	// Look ahead to detect lambda: (ident, ...) ->
	if p.v2LooksLikeLambdaParams() {
		return p.v2ParseMultiParamLambda()
	}
	// Parse first expression
	p.advance() // consume (
	if p.check(lexer.TOKEN_RPAREN) {
		p.advance()
		return &TupleLit{} // empty tuple ()
	}
	first := p.v2ParseExpr()
	// If comma follows, it's a tuple: (a, b, c)
	if p.check(lexer.TOKEN_COMMA) {
		elems := []Expr{first}
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			if p.check(lexer.TOKEN_RPAREN) {
				break // trailing comma: (a,)
			}
			elems = append(elems, p.v2ParseExpr())
		}
		p.expect(lexer.TOKEN_RPAREN)
		return &TupleLit{Elements: elems}
	}
	// Single expression in parens: (expr)
	p.expect(lexer.TOKEN_RPAREN)
	return first
}

// v2LooksLikeLambdaParams checks if ( starts lambda params.
func (p *Parser) v2LooksLikeLambdaParams() bool {
	// Quick check: ( ident ) ->  or  ( ident , ... ) ->
	off := 1
	for {
		if p.peekAt(off).Type != lexer.TOKEN_IDENT {
			return false
		}
		off++
		if p.peekAt(off).Type == lexer.TOKEN_RPAREN {
			return p.peekAt(off+1).Type == lexer.TOKEN_ARROW
		}
		if p.peekAt(off).Type == lexer.TOKEN_COMMA {
			off++
			continue
		}
		return false
	}
}

// v2ParseMultiParamLambda: (a, b) -> expr
func (p *Parser) v2ParseMultiParamLambda() Expr {
	p.advance() // consume (
	var params []*ParamDecl
	params = append(params, &ParamDecl{Name: p.expect(lexer.TOKEN_IDENT).Literal})
	for p.check(lexer.TOKEN_COMMA) {
		p.advance()
		params = append(params, &ParamDecl{Name: p.expect(lexer.TOKEN_IDENT).Literal})
	}
	p.expect(lexer.TOKEN_RPAREN)
	p.expect(lexer.TOKEN_ARROW)
	expr := p.v2ParseExpr()
	return &LambdaExpr{Params: params, Expr: expr}
}

// v2ParseListLit: [elem, elem, ...]  OR  [expr for var in iterable [if cond]]
func (p *Parser) v2ParseListLit() Expr {
	p.advance() // consume [
	if p.check(lexer.TOKEN_RBRACKET) {
		p.advance()
		return &ListLit{}
	}

	// Parse first expression
	first := p.v2ParseExpr()

	// Check for comprehension: [expr for var in iterable [if cond]]
	if p.check(lexer.TOKEN_FOR) {
		p.advance() // consume for
		varName := p.expect(lexer.TOKEN_IDENT).Literal
		p.expect(lexer.TOKEN_IN)
		iter := p.v2ParseExpr()
		var cond Expr
		if p.check(lexer.TOKEN_IF) {
			p.advance()
			cond = p.v2ParseExpr()
		}
		p.expect(lexer.TOKEN_RBRACKET)
		return &ComprehensionExpr{Expr: first, Var: varName, Iter: iter, Cond: cond}
	}

	// Regular list literal
	elems := []Expr{first}
	for p.check(lexer.TOKEN_COMMA) {
		p.advance()
		if p.check(lexer.TOKEN_RBRACKET) {
			break
		}
		elems = append(elems, p.v2ParseExpr())
	}
	p.expect(lexer.TOKEN_RBRACKET)
	return &ListLit{Elements: elems}
}

// v2ParseDictLit: {key: val, ...}  OR  {keyExpr: valExpr for var in iterable [if cond]}
func (p *Parser) v2ParseDictLit() Expr {
	p.advance() // consume {
	if p.check(lexer.TOKEN_RBRACE) {
		p.advance()
		return &MapLit{}
	}

	// Parse first key: value
	k := p.v2ParseExpr()
	p.expect(lexer.TOKEN_COLON)
	v := p.v2ParseExpr()

	// Check for dict comprehension: {k: v for var in iterable}
	if p.check(lexer.TOKEN_FOR) {
		p.advance()
		varName := p.expect(lexer.TOKEN_IDENT).Literal
		p.expect(lexer.TOKEN_IN)
		iter := p.v2ParseExpr()
		var cond Expr
		if p.check(lexer.TOKEN_IF) {
			p.advance()
			cond = p.v2ParseExpr()
		}
		p.expect(lexer.TOKEN_RBRACE)
		return &DictComprehensionExpr{Key: k, Val: v, Var: varName, Iter: iter, Cond: cond}
	}

	// Regular dict literal
	keys := []Expr{k}
	vals := []Expr{v}
	for p.check(lexer.TOKEN_COMMA) {
		p.advance()
		if p.check(lexer.TOKEN_RBRACE) {
			break
		}
		k = p.v2ParseExpr()
		p.expect(lexer.TOKEN_COLON)
		v = p.v2ParseExpr()
		keys = append(keys, k)
		vals = append(vals, v)
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &MapLit{Keys: keys, Values: vals}
}

// v2ParseInterpString converts "Hello, {name}!" into StringInterpLit.
func (p *Parser) v2ParseInterpString(raw string) Expr {
	var parts []Expr
	buf := ""
	i := 0
	runes := []rune(raw)
	for i < len(runes) {
		if runes[i] == '{' {
			if buf != "" {
				parts = append(parts, &StringLit{Value: buf})
				buf = ""
			}
			i++ // skip {
			exprStr := ""
			depth := 1
			for i < len(runes) && depth > 0 {
				if runes[i] == '{' {
					depth++
				} else if runes[i] == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
				exprStr += string(runes[i])
				i++
			}
			if i < len(runes) {
				i++ // skip }
			}
			// Parse the interpolated expression
			lex := lexer.New(exprStr)
			tokens := lex.Tokenize()
			subParser := New(tokens)
			expr := subParser.v2ParseExpr()
			parts = append(parts, expr)
		} else {
			buf += string(runes[i])
			i++
		}
	}
	if buf != "" {
		parts = append(parts, &StringLit{Value: buf})
	}
	if len(parts) == 1 {
		if s, ok := parts[0].(*StringLit); ok {
			return s
		}
	}
	return &StringInterpLit{Parts: parts}
}
