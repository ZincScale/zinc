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

// ParseV2 parses Zinc v2 syntax (end blocks, fn keyword, script mode).
// Returns a Program where top-level statements are wrapped in FnDecl named "main".
func (p *Parser) ParseV2() (prog *Program) {
	prog = &Program{}

	// Recover from parse errors — stop immediately with a clear error
	// rather than continuing with garbled state.
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(parseError); !ok {
				panic(r) // re-panic non-parse errors
			}
		}
	}()

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
			if p.v2IsFnDeclTypeFirst() {
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
				p.errorf("expected function or class after decorator")
			}
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
		case lexer.TOKEN_PUB:
			p.advance() // consume "pub"
			if p.v2IsFnDeclTypeFirst() {
				fn := p.v2ParseFnDecl()
				fn.IsPub = true
				prog.Decls = append(prog.Decls, fn)
				continue
			}
			switch p.peek().Type {
			case lexer.TOKEN_CONST:
				c := p.v2ParseConstDecl()
				c.IsPub = true
				prog.Decls = append(prog.Decls, c)
			case lexer.TOKEN_CLASS:
				cls := p.v2ParseClassDecl()
				prog.Decls = append(prog.Decls, cls)
			case lexer.TOKEN_DATA:
				d := p.v2ParseDataClassDecl()
				prog.Decls = append(prog.Decls, d)
			case lexer.TOKEN_INTERFACE:
				iface := p.v2ParseInterfaceDecl()
				prog.Decls = append(prog.Decls, iface)
			default:
				p.errorf("expected function, const, class, data, or interface after 'pub'")
			}
		case lexer.TOKEN_CONST:
			prog.Decls = append(prog.Decls, p.v2ParseConstDecl())
		case lexer.TOKEN_TYPE:
			prog.Decls = append(prog.Decls, p.v2ParseTypeAlias())
		default:
			// Check for contextual keyword: sealed class
			if tok.Type == lexer.TOKEN_IDENT && tok.Literal == "sealed" && p.peekAt(1).Type == lexer.TOKEN_CLASS {
				p.advance() // consume "sealed"
				cls := p.v2ParseClassDecl()
				cls.IsSealed = true
				prog.Decls = append(prog.Decls, cls)
				break
			}
			// Check for contextual keyword: test "name" { body }
			if tok.Type == lexer.TOKEN_IDENT && tok.Literal == "test" && p.peekAt(1).Type == lexer.TOKEN_STRING_LIT {
				prog.Decls = append(prog.Decls, p.v2ParseTestDecl())
				break
			}
			// Type-first function declaration: `ReturnType name(...)` /
			// `void name(...)`. Detection lookahead avoids confusing
			// these with top-level statements.
			if p.v2IsFnDeclTypeFirst() {
				prog.Decls = append(prog.Decls, p.v2ParseFnDecl())
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

// reservedTypeNames are builtin type and cast names that cannot be used
// as variable, function, class, parameter, or other declaration names.
// These names are reserved because the codegen rewrites calls to them
// (e.g., long(x) → int64(x), str(x) → fmt.Sprint(x)).
var reservedTypeNames = map[string]bool{
	"int": true, "long": true, "float": true, "double": true,
	"str": true, "bool": true, "byte": true, "char": true,
	"string": true, "void": true,
}

// v2ValidateDeclName checks that a declared name does not shadow a reserved
// builtin type or function. Call this at declaration sites only (not references).
func (p *Parser) v2ValidateDeclName(name string) {
	if reservedTypeNames[name] {
		p.errorf("'%s' is a reserved builtin and cannot be used as a name", name)
	}
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
