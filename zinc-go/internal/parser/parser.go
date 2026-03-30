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
	"fmt"
	"strings"

	"zinc-go/internal/lexer"
)

// Parser converts a token stream into an AST.
type Parser struct {
	tokens  []lexer.Token
	pos     int
	Errors  []string
}

// New creates a Parser from a list of tokens.
func New(tokens []lexer.Token) *Parser {
	return &Parser{tokens: tokens}
}

// --- Infrastructure ----------------------------------------------------------

func (p *Parser) peek() lexer.Token {
	if p.pos >= len(p.tokens) {
		return lexer.Token{Type: lexer.TOKEN_EOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) peekAt(offset int) lexer.Token {
	idx := p.pos + offset
	if idx >= len(p.tokens) {
		return lexer.Token{Type: lexer.TOKEN_EOF}
	}
	return p.tokens[idx]
}

func (p *Parser) advance() lexer.Token {
	tok := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *Parser) check(t lexer.TokenType) bool {
	return p.peek().Type == t
}

func (p *Parser) match(types ...lexer.TokenType) bool {
	for _, t := range types {
		if p.check(t) {
			return true
		}
	}
	return false
}

func (p *Parser) expect(t lexer.TokenType) lexer.Token {
	tok := p.peek()
	if tok.Type != t {
		p.Errors = append(p.Errors, fmt.Sprintf("%d:%d: expected %s, got %s (%q)",
			tok.Line, tok.Col, t, tok.Type, tok.Literal))
		return tok
	}
	return p.advance()
}

func (p *Parser) skipSemis() {
	for p.check(lexer.TOKEN_SEMICOLON) {
		p.advance()
	}
}

func (p *Parser) errorf(format string, args ...any) {
	tok := p.peek()
	msg := fmt.Sprintf("%d:%d: ", tok.Line, tok.Col) + fmt.Sprintf(format, args...)
	p.Errors = append(p.Errors, msg)
}

// looksLikeReturnType returns true if the next token looks like a return type
// (an identifier starting with an uppercase letter, e.g. Int, String, List).
// Used to parse return types without a colon separator.
func (p *Parser) looksLikeReturnType() bool {
	tok := p.peek()
	return tok.Type == lexer.TOKEN_IDENT && len(tok.Literal) > 0 && tok.Literal[0] >= 'A' && tok.Literal[0] <= 'Z'
}

// parseOptionalReturnType parses an optional return type after a param list.
// Uses `name Type` syntax (no colon).
func (p *Parser) parseOptionalReturnType() TypeExpr {
	if p.looksLikeReturnType() {
		return p.parseType()
	}
	return nil
}

// --- Type Expressions --------------------------------------------------------

func (p *Parser) parseType() TypeExpr {
	tok := p.expect(lexer.TOKEN_IDENT)
	name := tok.Literal
	var t TypeExpr
	if name == "Fn" && p.check(lexer.TOKEN_LPAREN) {
		// Fn(ParamTypes) — void function type
		params := p.parseFnTypeParams()
		t = &FuncTypeExpr{Params: params, ReturnType: nil}
	} else if p.check(lexer.TOKEN_LT) {
		p.advance() // <
		var args []TypeExpr
		args = append(args, p.parseType())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			args = append(args, p.parseType())
		}
		p.expect(lexer.TOKEN_GT)
		t = &GenericType{Name: name, TypeArgs: args}
	} else {
		t = &SimpleType{Name: name}
	}
	// Check for ReturnType Fn(Params) — non-void function type
	if p.check(lexer.TOKEN_IDENT) && p.peek().Literal == "Fn" && p.peekAt(1).Type == lexer.TOKEN_LPAREN {
		p.advance() // consume "Fn"
		params := p.parseFnTypeParams()
		// t is the return type
		fnType := &FuncTypeExpr{Params: params, ReturnType: t}
		// Optional suffix: Fn(...)?
		if p.check(lexer.TOKEN_QUESTION) {
			p.advance()
			return &OptionalType{Inner: fnType}
		}
		return fnType
	}
	// Optional suffix: Type?
	if p.check(lexer.TOKEN_QUESTION) {
		p.advance()
		return &OptionalType{Inner: t}
	}
	return t
}

// parseFnTypeParams parses Fn(P1, P2) parameter types. Called after "Fn" is consumed.
// Returns the list of parameter types.
func (p *Parser) parseFnTypeParams() []TypeExpr {
	p.expect(lexer.TOKEN_LPAREN)
	var params []TypeExpr
	if !p.check(lexer.TOKEN_RPAREN) {
		params = append(params, p.parseType())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			params = append(params, p.parseType())
		}
	}
	p.expect(lexer.TOKEN_RPAREN)
	return params
}

// --- Expressions (Pratt parser) -----------------------------------------------

type precedence int

const (
	precNone            precedence = iota
	precRange                      // .. ..=
	precNullCoalesce               // ??
	precOr                         // ||
	precAnd                        // &&
	precEquality                   // == !=
	precComparison                 // < > <= >=
	precAddSub                     // + -
	precMulDiv                     // * / %
	precTypeOp                     // as is
	precUnary                      // ! -
	precCall                       // . () []
)

func tokenPrec(t lexer.TokenType) precedence {
	switch t {
	case lexer.TOKEN_QUESTION_QUESTION:
		return precNullCoalesce
	case lexer.TOKEN_PIPE_PIPE:
		return precOr
	case lexer.TOKEN_AMP_AMP:
		return precAnd
	case lexer.TOKEN_EQ, lexer.TOKEN_NEQ:
		return precEquality
	case lexer.TOKEN_LT, lexer.TOKEN_LTE, lexer.TOKEN_GT, lexer.TOKEN_GTE:
		return precComparison
	case lexer.TOKEN_PLUS, lexer.TOKEN_MINUS:
		return precAddSub
	case lexer.TOKEN_STAR, lexer.TOKEN_SLASH, lexer.TOKEN_PERCENT:
		return precMulDiv
	case lexer.TOKEN_AS, lexer.TOKEN_IS:
		return precTypeOp
	case lexer.TOKEN_DOT, lexer.TOKEN_LPAREN, lexer.TOKEN_LBRACKET, lexer.TOKEN_QUESTION_DOT:
		return precCall
	case lexer.TOKEN_DOTDOT, lexer.TOKEN_DOTDOTEQ:
		return precRange
	}
	return precNone
}

func (p *Parser) parseExpr() Expr {
	return p.parseExprPrec(precNone)
}

func (p *Parser) parseExprPrec(minPrec precedence) Expr {
	left := p.parseUnary()

	for {
		// Check for generic call: ident<Type>(args) before precedence check
		// since '<' would otherwise be parsed as comparison
		if p.peek().Type == lexer.TOKEN_LT {
			if ident, ok := left.(*Ident); ok && p.looksLikeTypeArgs() {
				typeArgs := p.parseCallTypeArgs()
				p.expect(lexer.TOKEN_LPAREN)
				call := p.finishCallArgsNoLParen(ident).(*CallExpr)
				call.TypeArgs = typeArgs
				left = call
				continue
			}
		}

		prec := tokenPrec(p.peek().Type)
		if prec <= minPrec {
			break
		}

		tok := p.advance()
		switch tok.Type {
		case lexer.TOKEN_DOT:
			field := p.expect(lexer.TOKEN_IDENT).Literal
			sel := &SelectorExpr{Object: left, Field: field}
			// Check for call: obj.method(...) or obj.method { trailing lambda }
			if p.check(lexer.TOKEN_LPAREN) {
				left = p.finishCall(sel)
				// Check for trailing lambda after closing paren: obj.method(args) { ... }
				if p.check(lexer.TOKEN_LBRACE) && p.isTrailingLambda() {
					lambda := p.parseTrailingLambda()
					if call, ok := left.(*CallExpr); ok {
						call.Args = append(call.Args, lambda)
					}
				}
			} else if p.check(lexer.TOKEN_LBRACE) && p.isTrailingLambda() {
				// obj.method { trailing lambda } — no parens, lambda is only arg
				lambda := p.parseTrailingLambda()
				left = &CallExpr{Callee: sel, Args: []Expr{lambda}}
			} else {
				left = sel
			}
		case lexer.TOKEN_QUESTION_DOT:
			// ?. safe navigation: obj?.field or obj?.method(...)
			field := p.expect(lexer.TOKEN_IDENT).Literal
			nav := &SafeNavExpr{Object: left, Field: field}
			if p.check(lexer.TOKEN_LPAREN) {
				sel := &SelectorExpr{Object: left, Field: field}
				result := p.finishCall(sel)
				if call, ok := result.(*CallExpr); ok {
					nav.Call = call
				} else {
					p.errorf("cannot use ?.%s() in this context", field)
				}
			}
			left = nav
		case lexer.TOKEN_LPAREN:
			left = p.finishCallArgsNoLParen(left)
			// Check for trailing lambda after closing paren: fn(args) { ... }
			if p.check(lexer.TOKEN_LBRACE) && p.isTrailingLambda() {
				lambda := p.parseTrailingLambda()
				if call, ok := left.(*CallExpr); ok {
					call.Args = append(call.Args, lambda)
				}
			}
		case lexer.TOKEN_LBRACKET:
			// Distinguish slice (a[low:high]) from index (a[idx])
			if p.check(lexer.TOKEN_COLON) {
				// [:high] — no low
				p.advance() // consume ':'
				var high Expr
				if !p.check(lexer.TOKEN_RBRACKET) {
					high = p.parseExpr()
				}
				p.expect(lexer.TOKEN_RBRACKET)
				left = &SliceExpr{Object: left, Low: nil, High: high}
			} else {
				expr := p.parseExpr()
				if p.check(lexer.TOKEN_COLON) {
					// [low:high] or [low:]
					p.advance() // consume ':'
					var high Expr
					if !p.check(lexer.TOKEN_RBRACKET) {
						high = p.parseExpr()
					}
					p.expect(lexer.TOKEN_RBRACKET)
					left = &SliceExpr{Object: left, Low: expr, High: high}
				} else {
					// [idx] — plain index
					p.expect(lexer.TOKEN_RBRACKET)
					left = &IndexExpr{Object: left, Index: expr}
				}
			}
		case lexer.TOKEN_AS:
			typeName := p.expect(lexer.TOKEN_IDENT).Literal
			left = &TypeAssertExpr{Object: left, TypeName: typeName, IsCheck: false}
		case lexer.TOKEN_IS:
			typeName := p.expect(lexer.TOKEN_IDENT).Literal
			left = &TypeAssertExpr{Object: left, TypeName: typeName, IsCheck: true}
		case lexer.TOKEN_DOTDOT:
			right := p.parseExprPrec(prec)
			left = &RangeExpr{Start: left, End: right, Inclusive: false}
		case lexer.TOKEN_DOTDOTEQ:
			right := p.parseExprPrec(prec)
			left = &RangeExpr{Start: left, End: right, Inclusive: true}
		default:
			right := p.parseExprPrec(prec)
			left = &BinaryExpr{Left: left, Op: tok.Literal, Right: right}
		}
	}
	return left
}

// finishCall is called when '(' has NOT been consumed yet (e.g. from DOT case).
// All method calls are parsed as regular CallExpr — builtin dispatch happens in codegen.
func (p *Parser) finishCall(callee Expr) Expr {
	// Special case: .slice(low, high) → SliceExpr (bracket syntax alternative)
	if sel, ok := callee.(*SelectorExpr); ok && sel.Field == "slice" {
		p.expect(lexer.TOKEN_LPAREN)
		low := p.parseExpr()
		var high Expr
		if p.check(lexer.TOKEN_COMMA) {
			p.advance()
			high = p.parseExpr()
		}
		p.expect(lexer.TOKEN_RPAREN)
		return &SliceExpr{Object: sel.Object, Low: low, High: high}
	}
	p.expect(lexer.TOKEN_LPAREN)
	return p.finishCallArgsNoLParen(callee)
}

func (p *Parser) finishCallArgsNoLParen(callee Expr) Expr {
	var args []Expr
	var namedArgs []NamedArg
	seenNamed := false

	if !p.check(lexer.TOKEN_RPAREN) {
		for {
			if p.check(lexer.TOKEN_IDENT) && p.peekAt(1).Type == lexer.TOKEN_COLON {
				seenNamed = true
				name := p.advance().Literal
				p.advance() // consume ':'
				namedArgs = append(namedArgs, NamedArg{Name: name, Value: p.parseExpr()})
			} else {
				if seenNamed {
					p.errorf("positional argument after named argument")
				}
				expr := p.parseExpr()
				// Check for spread: expr...
				if p.check(lexer.TOKEN_DOTDOTDOT) {
					p.advance()
					expr = &SpreadExpr{Expr: expr}
				}
				args = append(args, expr)
			}
			if !p.check(lexer.TOKEN_COMMA) {
				break
			}
			p.advance()
		}
	}
	p.expect(lexer.TOKEN_RPAREN)
	return &CallExpr{Callee: callee, Args: args, NamedArgs: namedArgs}
}

func (p *Parser) parseUnary() Expr {
	tok := p.peek()
	switch tok.Type {
	case lexer.TOKEN_BANG:
		p.advance()
		return &UnaryExpr{Op: "!", Operand: p.parseUnary()}
	case lexer.TOKEN_MINUS:
		p.advance()
		return &UnaryExpr{Op: "-", Operand: p.parseUnary()}
	}
	return p.parsePrimary()
}

// isLambdaStart returns true if the current TOKEN_LPAREN begins a lambda, not a grouping.
// Heuristics (peekAt is relative to current position = the '('):
//
//	() =>              → peek(1)==RPAREN, peek(2)==ARROW
//	(Type name) =>     → peek(1)==IDENT(upper), peek(2)==IDENT(lower) (typed param)
//	(Type... name) =>  → peek(1)==IDENT(upper), peek(2)==DOTDOTDOT (variadic typed)
//	(name) =>          → peek(1)==IDENT(lower),  peek(2)==RPAREN, peek(3)==ARROW (untyped single)
//	(name, name) =>    → peek(1)==IDENT(lower),  peek(2)==COMMA (untyped multi)
func (p *Parser) isLambdaStart() bool {
	ahead1 := p.peekAt(1)
	switch ahead1.Type {
	case lexer.TOKEN_RPAREN:
		// () =>
		next := p.peekAt(2)
		if next.Type == lexer.TOKEN_ARROW {
			return true
		}
	case lexer.TOKEN_IDENT:
		isUpper := len(ahead1.Literal) > 0 && ahead1.Literal[0] >= 'A' && ahead1.Literal[0] <= 'Z'
		if isUpper {
			// (Type name) => ... — typed param (type-before-name)
			// Need to scan past the type (which may be generic: List<Int>) to find the name
			i := 2
			// Skip generic type args <...>
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
			// Skip optional ?
			if p.peekAt(i).Type == lexer.TOKEN_QUESTION {
				i++
			}
			// Skip optional ... (variadic)
			if p.peekAt(i).Type == lexer.TOKEN_DOTDOTDOT {
				i++
			}
			// Next should be lowercase ident (the param name)
			next := p.peekAt(i)
			if next.Type == lexer.TOKEN_IDENT && len(next.Literal) > 0 && next.Literal[0] >= 'a' && next.Literal[0] <= 'z' {
				return true
			}
		} else {
			// Check if it's a lowercase builtin type: (int x) -> ...
			switch ahead1.Literal {
			case "int", "long", "double", "float", "boolean", "bool", "byte", "char", "string", "void":
				// Lowercase type name followed by param name
				ahead2 := p.peekAt(2)
				if ahead2.Type == lexer.TOKEN_IDENT {
					return true // (int x) -> ...
				}
			}
			// lowercase ident — untyped param
			ahead2 := p.peekAt(2)
			if ahead2.Type == lexer.TOKEN_COMMA {
				return true // (name, name) => ...
			}
			if ahead2.Type == lexer.TOKEN_RPAREN && p.peekAt(3).Type == lexer.TOKEN_ARROW {
				return true // (name) => ...
			}
		}
	}
	return false
}

func (p *Parser) parseLambda() *LambdaExpr {
	p.expect(lexer.TOKEN_LPAREN)
	var params []*ParamDecl
	if !p.check(lexer.TOKEN_RPAREN) {
		params = append(params, p.parseLambdaParam())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			params = append(params, p.parseLambdaParam())
		}
	}
	p.expect(lexer.TOKEN_RPAREN)

	// Lambda return types are always inferred (no explicit return type)
	p.expect(lexer.TOKEN_ARROW)

	if p.check(lexer.TOKEN_LBRACE) {
		body := p.parseBlock()
		return &LambdaExpr{Params: params, Body: body}
	}
	expr := p.parseExpr()
	return &LambdaExpr{Params: params, Expr: expr}
}

// parseLambdaParam parses a lambda parameter, which can be:
//   - Type name      (typed, type-before-name)
//   - Type... name   (typed variadic)
//   - name           (untyped shorthand — type inferred from context)
func (p *Parser) parseLambdaParam() *ParamDecl {
	tok := p.peek()
	// Typed param: Type name — detect by checking if next-next token is also an ident
	// This handles both uppercase (String x) and lowercase (int x) types
	isTyped := false
	if tok.Type == lexer.TOKEN_IDENT {
		// Uppercase type name (String, List, Map, etc.)
		if len(tok.Literal) > 0 && tok.Literal[0] >= 'A' && tok.Literal[0] <= 'Z' {
			isTyped = true
		}
		// Lowercase builtin types followed by an ident name
		next := p.peekAt(1)
		if next.Type == lexer.TOKEN_IDENT || next.Type == lexer.TOKEN_DOTDOTDOT {
			switch tok.Literal {
			case "int", "long", "double", "float", "boolean", "bool", "byte", "char", "string", "void":
				isTyped = true
			}
		}
	}
	if isTyped {
		typ := p.parseType()
		variadic := false
		if p.check(lexer.TOKEN_DOTDOTDOT) {
			variadic = true
			p.advance()
		}
		name := p.expect(lexer.TOKEN_IDENT).Literal
		var def Expr
		if p.check(lexer.TOKEN_ASSIGN) {
			p.advance()
			def = p.parseExpr()
		}
		return &ParamDecl{Name: name, Type: typ, Default: def, Variadic: variadic}
	}
	// Untyped param: just the name (type will be inferred during codegen)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	return &ParamDecl{Name: name}
}

// isTrailingLambda returns true if the current '{' starts a trailing lambda,
// not a map literal or block. A trailing lambda is:
//   { expr }           — implicit `it` parameter
//   { a, b -> expr }   — explicit parameters
// We distinguish from map literals by checking if the content has ':' (map) vs '->' (lambda).
func (p *Parser) isTrailingLambda() bool {
	if !p.check(lexer.TOKEN_LBRACE) {
		return false
	}
	// Scan ahead inside the { } to check for -> (lambda) or : (map literal)
	depth := 0
	for i := 0; ; i++ {
		tok := p.peekAt(i)
		if tok.Type == lexer.TOKEN_EOF {
			return false
		}
		if tok.Type == lexer.TOKEN_LBRACE {
			depth++
		} else if tok.Type == lexer.TOKEN_RBRACE {
			depth--
			if depth == 0 {
				// Reached end of block without finding ':' or '->'
				// This is a trailing lambda with implicit `it`
				return true
			}
		} else if depth == 1 {
			if tok.Type == lexer.TOKEN_COLON {
				// Looks like a map literal: { "key": value }
				return false
			}
			if tok.Type == lexer.TOKEN_ARROW {
				// Explicit params: { a, b -> expr }
				return true
			}
		}
	}
}

// parseTrailingLambda parses { expr } or { a, b -> expr } as a LambdaExpr.
func (p *Parser) parseTrailingLambda() *LambdaExpr {
	p.expect(lexer.TOKEN_LBRACE)

	// Check for explicit params: ident [, ident]* ->
	if p.hasTrailingLambdaParams() {
		var params []*ParamDecl
		params = append(params, &ParamDecl{Name: p.expect(lexer.TOKEN_IDENT).Literal})
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			params = append(params, &ParamDecl{Name: p.expect(lexer.TOKEN_IDENT).Literal})
		}
		p.expect(lexer.TOKEN_ARROW)

		// Parse body — single expression or multiple statements
		if p.isTrailingLambdaMultiStmt() {
			body := &BlockStmt{}
			for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
				body.Stmts = append(body.Stmts, p.parseStmt())
				p.skipSemis()
			}
			p.expect(lexer.TOKEN_RBRACE)
			return &LambdaExpr{Params: params, Body: body}
		}
		expr := p.parseExpr()
		p.expect(lexer.TOKEN_RBRACE)
		return &LambdaExpr{Params: params, Expr: expr}
	}

	// Implicit `it` parameter
	// Parse body — single expression or multiple statements
	if p.isTrailingLambdaMultiStmt() {
		body := &BlockStmt{}
		for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
			body.Stmts = append(body.Stmts, p.parseStmt())
			p.skipSemis()
		}
		p.expect(lexer.TOKEN_RBRACE)
		return &LambdaExpr{Params: []*ParamDecl{{Name: "it"}}, Body: body}
	}
	expr := p.parseExpr()
	p.expect(lexer.TOKEN_RBRACE)
	return &LambdaExpr{Params: []*ParamDecl{{Name: "it"}}, Expr: expr}
}

// hasTrailingLambdaParams checks if the trailing lambda starts with: ident [, ident]* ->
func (p *Parser) hasTrailingLambdaParams() bool {
	i := 0
	for {
		tok := p.peekAt(i)
		if tok.Type != lexer.TOKEN_IDENT {
			return false
		}
		// Must be lowercase (not a type name)
		if len(tok.Literal) > 0 && tok.Literal[0] >= 'A' && tok.Literal[0] <= 'Z' {
			return false
		}
		i++
		next := p.peekAt(i)
		if next.Type == lexer.TOKEN_ARROW {
			return true
		}
		if next.Type == lexer.TOKEN_COMMA {
			i++
			continue
		}
		return false
	}
}

// isTrailingLambdaMultiStmt returns true if the trailing lambda body contains
// multiple statements (detected by looking for semicolons or statement-starting keywords).
func (p *Parser) isTrailingLambdaMultiStmt() bool {
	// Scan for semicolons or newline-separated statements at depth 1
	depth := 0
	for i := 0; ; i++ {
		tok := p.peekAt(i)
		if tok.Type == lexer.TOKEN_EOF {
			return false
		}
		if tok.Type == lexer.TOKEN_LBRACE {
			depth++
		} else if tok.Type == lexer.TOKEN_RBRACE {
			if depth == 0 {
				return false // single expression
			}
			depth--
		} else if depth == 0 {
			// Multiple statements indicated by: var, return, if, for, while, print, semicolons
			switch tok.Type {
			case lexer.TOKEN_SEMICOLON:
				return true
			case lexer.TOKEN_VAR, lexer.TOKEN_RETURN, lexer.TOKEN_IF, lexer.TOKEN_FOR, lexer.TOKEN_WHILE, lexer.TOKEN_PRINT:
				return true
			}
		}
	}
}

func (p *Parser) parsePrimary() Expr {
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
	case lexer.TOKEN_RAW_STRING:
		p.advance()
		return &RawStringLit{Value: tok.Literal}
	case lexer.TOKEN_INTERP_STRING:
		p.advance()
		return p.parseInterpString(tok.Literal)
	case lexer.TOKEN_BOOL_LIT:
		p.advance()
		return &BoolLit{Value: tok.Literal == "true"}
	case lexer.TOKEN_NULL:
		p.advance()
		return &NullLit{}
	case lexer.TOKEN_THIS:
		p.advance()
		return &ThisExpr{}
	case lexer.TOKEN_SUPER:
		p.advance()
		p.expect(lexer.TOKEN_LPAREN)
		var args []Expr
		if !p.check(lexer.TOKEN_RPAREN) {
			args = append(args, p.parseExpr())
			for p.check(lexer.TOKEN_COMMA) {
				p.advance()
				args = append(args, p.parseExpr())
			}
		}
		p.expect(lexer.TOKEN_RPAREN)
		return &SuperCallExpr{Args: args}
	case lexer.TOKEN_LPAREN:
		if p.isLambdaStart() {
			return p.parseLambda()
		}
		p.advance()
		expr := p.parseExpr()
		p.expect(lexer.TOKEN_RPAREN)
		return expr
	case lexer.TOKEN_IF:
		return p.parseIfExpr()
	case lexer.TOKEN_MATCH:
		return p.parseMatchExpr()
	case lexer.TOKEN_SPAWN:
		return p.parseSpawnExpr()
	case lexer.TOKEN_LBRACKET:
		return p.parseListLit()
	case lexer.TOKEN_LBRACE:
		return p.parseMapLit()
	case lexer.TOKEN_IDENT:
		// Check for shorthand lambda: name => expr
		if p.peekAt(1).Type == lexer.TOKEN_ARROW {
			name := p.advance().Literal
			p.advance() // consume =>
			if p.check(lexer.TOKEN_LBRACE) {
				body := p.parseBlock()
				return &LambdaExpr{Params: []*ParamDecl{{Name: name}}, Body: body}
			}
			expr := p.parseExpr()
			return &LambdaExpr{Params: []*ParamDecl{{Name: name}}, Expr: expr}
		}
		p.advance()
		return &Ident{Name: tok.Literal}
	}

	p.errorf("unexpected token %s (%q) in expression", tok.Type, tok.Literal)
	p.advance()
	return &Ident{Name: "_error_"}
}

// parseInterpString converts "Hello, {name}!" raw literal into a StringInterpLit.
// The raw literal has {expr_text} segments already present as literal characters.
// We re-tokenize the segments inline using a sub-lexer.
func (p *Parser) parseInterpString(raw string) Expr {
	var parts []Expr
	i := 0
	runes := []rune(raw)
	var staticBuf strings.Builder

	for i < len(runes) {
		if runes[i] == '{' {
			// flush static text
			if staticBuf.Len() > 0 {
				parts = append(parts, &StringLit{Value: staticBuf.String()})
				staticBuf.Reset()
			}
			// collect until matching }
			i++ // skip {
			var exprBuf strings.Builder
			depth := 1
			for i < len(runes) && depth > 0 {
				if runes[i] == '{' {
					depth++
				} else if runes[i] == '}' {
					depth--
					if depth == 0 {
						i++ // skip }
						break
					}
				}
				exprBuf.WriteRune(runes[i])
				i++
			}
			// parse the expression inside
			subLexer := lexer.New(exprBuf.String())
			subTokens := subLexer.Tokenize()
			subParser := New(subTokens)
			expr := subParser.parseExpr()
			if len(subParser.Errors) > 0 {
				p.Errors = append(p.Errors, subParser.Errors...)
			}
			parts = append(parts, expr)
		} else {
			staticBuf.WriteRune(runes[i])
			i++
		}
	}
	if staticBuf.Len() > 0 {
		parts = append(parts, &StringLit{Value: staticBuf.String()})
	}
	if len(parts) == 1 {
		if sl, ok := parts[0].(*StringLit); ok {
			return sl
		}
	}
	return &StringInterpLit{Parts: parts}
}

func (p *Parser) parseListLit() *ListLit {
	p.expect(lexer.TOKEN_LBRACKET)
	var elems []Expr
	if !p.check(lexer.TOKEN_RBRACKET) {
		elems = append(elems, p.parseExpr())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			if p.check(lexer.TOKEN_RBRACKET) {
				break
			}
			elems = append(elems, p.parseExpr())
		}
	}
	p.expect(lexer.TOKEN_RBRACKET)
	return &ListLit{Elements: elems}
}

func (p *Parser) parseMapLit() *MapLit {
	p.expect(lexer.TOKEN_LBRACE)
	var keys, vals []Expr
	if !p.check(lexer.TOKEN_RBRACE) {
		k := p.parseExpr()
		p.expect(lexer.TOKEN_COLON)
		v := p.parseExpr()
		keys = append(keys, k)
		vals = append(vals, v)
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			if p.check(lexer.TOKEN_RBRACE) {
				break
			}
			k = p.parseExpr()
			p.expect(lexer.TOKEN_COLON)
			v = p.parseExpr()
			keys = append(keys, k)
			vals = append(vals, v)
		}
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &MapLit{Keys: keys, Values: vals}
}

// --- Statements --------------------------------------------------------------

func (p *Parser) parseBlock() *BlockStmt {
	p.expect(lexer.TOKEN_LBRACE)
	var stmts []Stmt
	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		s := p.parseStmt()
		if s != nil {
			stmts = append(stmts, s)
		}
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &BlockStmt{Stmts: stmts}
}

func (p *Parser) parseStmt() Stmt {
	p.skipSemis()
	tok := p.peek()

	switch tok.Type {
	case lexer.TOKEN_VAR:
		// var x = expr → inferred variable declaration
		// var (a, b) = expr → tuple destructuring
		if p.peekAt(1).Type == lexer.TOKEN_LPAREN {
			return p.parseTupleDestructure()
		}
		return p.parseShortVarStmt()
	case lexer.TOKEN_IDENT:
		// Type name = expr → typed variable declaration (e.g. Int x = 5)
		if p.isTypedVarDecl() {
			return p.parseTypedVarStmt()
		}
	case lexer.TOKEN_RETURN:
		return p.parseReturnStmt()
	case lexer.TOKEN_IF:
		return p.parseIfStmt()
	case lexer.TOKEN_FOR:
		return p.parseForStmt()
	case lexer.TOKEN_WHILE:
		return p.parseWhileStmt()
	case lexer.TOKEN_AT:
		// @annotations are handled at the declaration level, not statement level
		p.errorf("unexpected @ in statement context")
		p.advance()
		return nil
	case lexer.TOKEN_GO:
		return p.parseGoStmt()
	case lexer.TOKEN_PRINT:
		return p.parsePrintStmt()
	case lexer.TOKEN_MATCH:
		return p.parseMatchStmt()
	case lexer.TOKEN_BREAK:
		p.advance()
		return &BreakStmt{}
	case lexer.TOKEN_CONTINUE:
		p.advance()
		return &ContinueStmt{}
	case lexer.TOKEN_DEFER:
		p.advance()
		expr := p.parseExpr()
		return &DeferStmt{Expr: expr}
	case lexer.TOKEN_WITH:
		return p.parseWithStmt()
	case lexer.TOKEN_LBRACE:
		return p.parseBlock()
	}

	// Expression statement or assignment
	return p.parseExprOrAssignStmt()
}

func (p *Parser) parseConstDecl(isPub bool) *ConstDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_CONST)
	// [pub] const Type NAME = expr  or  [pub] const NAME = expr
	// Disambiguate: if first IDENT is followed by another IDENT, first is type
	var typ TypeExpr
	var name string
	if p.check(lexer.TOKEN_IDENT) && p.peekAt(1).Type == lexer.TOKEN_IDENT {
		// const Type NAME = expr
		typ = p.parseType()
		name = p.expect(lexer.TOKEN_IDENT).Literal
	} else {
		// const NAME = expr (no type)
		name = p.expect(lexer.TOKEN_IDENT).Literal
	}
	p.expect(lexer.TOKEN_ASSIGN)
	val := p.parseExpr()
	return &ConstDecl{Line: line, Name: name, IsPub: isPub, Type: typ, Value: val}
}

// parseShortVarStmt parses: var name = expr [or { handler }]
func (p *Parser) parseShortVarStmt() Stmt {
	line := p.peek().Line
	p.advance()                 // consume var
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.expect(lexer.TOKEN_ASSIGN)
	val := p.parseExpr()
	handler := p.parseOrHandler()
	return &VarStmt{Line: line, Name: name, Value: val, OrHandler: handler}
}

// parseTupleDestructure parses: var (a, b) = expr [or { handler }]
func (p *Parser) parseTupleDestructure() *TupleVarStmt {
	line := p.peek().Line
	p.advance() // consume var
	p.expect(lexer.TOKEN_LPAREN)
	var names []string
	names = append(names, p.expect(lexer.TOKEN_IDENT).Literal)
	for p.check(lexer.TOKEN_COMMA) {
		p.advance()
		names = append(names, p.expect(lexer.TOKEN_IDENT).Literal)
	}
	p.expect(lexer.TOKEN_RPAREN)
	p.expect(lexer.TOKEN_ASSIGN)
	val := p.parseExpr()
	handler := p.parseOrHandler()
	return &TupleVarStmt{Line: line, Names: names, Value: val, OrHandler: handler}
}

// isTypedVarDecl checks if the current position starts a typed variable declaration:
// Type name = expr  or  Type? name = expr  or  Type name  (no value)
// Also handles Fn types: Fn() name, ReturnType Fn(Params) name
// Requires: current is uppercase IDENT (type name), followed by lowercase ident (var name)
func (p *Parser) isTypedVarDecl() bool {
	tok := p.peek()
	if tok.Type != lexer.TOKEN_IDENT || len(tok.Literal) == 0 || tok.Literal[0] < 'A' || tok.Literal[0] > 'Z' {
		return false
	}
	// Scan past the type (which may include ?, <...>, Fn(...), etc.) to find the variable name
	i := 1
	// Handle Fn(...) at start position — void function type
	if tok.Literal == "Fn" && p.peekAt(i).Type == lexer.TOKEN_LPAREN {
		i = p.skipParens(i)
	} else {
		// Skip generic type params <...>
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
		// Check for ReturnType Fn(Params) — non-void function type
		if p.peekAt(i).Type == lexer.TOKEN_IDENT && p.peekAt(i).Literal == "Fn" && p.peekAt(i+1).Type == lexer.TOKEN_LPAREN {
			i++ // skip "Fn"
			i = p.skipParens(i)
		}
	}
	// Skip ? after type
	if p.peekAt(i).Type == lexer.TOKEN_QUESTION {
		i++
	}
	// Next must be lowercase ident (the variable name)
	nameToken := p.peekAt(i)
	if nameToken.Type != lexer.TOKEN_IDENT || len(nameToken.Literal) == 0 || nameToken.Literal[0] < 'a' || nameToken.Literal[0] > 'z' {
		return false
	}
	i++
	// Must be followed by = or end-of-statement (no value)
	t := p.peekAt(i).Type
	return t == lexer.TOKEN_ASSIGN || t == lexer.TOKEN_SEMICOLON || t == lexer.TOKEN_RBRACE || t == lexer.TOKEN_EOF
}

// skipParens skips past a balanced (...) starting at position i (which should be TOKEN_LPAREN).
// Returns the position after the closing paren.
func (p *Parser) skipParens(i int) int {
	depth := 1
	i++ // skip (
	for depth > 0 && p.peekAt(i).Type != lexer.TOKEN_EOF {
		if p.peekAt(i).Type == lexer.TOKEN_LPAREN {
			depth++
		} else if p.peekAt(i).Type == lexer.TOKEN_RPAREN {
			depth--
		}
		i++
	}
	return i
}

// parseTypedVarStmt parses: Type name = expr  or  Type name  (no value)
func (p *Parser) parseTypedVarStmt() *VarStmt {
	line := p.peek().Line
	typ := p.parseType()       // consume type
	name := p.advance().Literal // consume name
	var val Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		val = p.parseExpr()
	}
	handler := p.parseOrHandler()
	return &VarStmt{Line: line, Name: name, Type: typ, Value: val, OrHandler: handler}
}

func (p *Parser) parseReturnStmt() *ReturnStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_RETURN)
	if p.check(lexer.TOKEN_RBRACE) || p.check(lexer.TOKEN_SEMICOLON) || p.check(lexer.TOKEN_EOF) {
		return &ReturnStmt{Line: line}
	}
	return &ReturnStmt{Line: line, Value: p.parseExpr()}
}

func (p *Parser) parseIfStmt() *IfStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_IF)
	// Parens around condition are optional
	hasParen := p.check(lexer.TOKEN_LPAREN)
	if hasParen {
		p.advance()
	}
	cond := p.parseExpr()
	if hasParen {
		p.expect(lexer.TOKEN_RPAREN)
	}
	then := p.parseBlock()
	var elseStmt Stmt
	if p.check(lexer.TOKEN_ELSE) {
		p.advance()
		if p.check(lexer.TOKEN_IF) {
			elseStmt = p.parseIfStmt()
		} else {
			elseStmt = p.parseBlock()
		}
	}
	return &IfStmt{Line: line, Cond: cond, Then: then, ElseStmt: elseStmt}
}

// parseIfExpr parses: if cond { expr } else { expr }
// Used when if appears in expression position (e.g., var x = if cond { a } else { b })
func (p *Parser) parseIfExpr() Expr {
	p.expect(lexer.TOKEN_IF)
	cond := p.parseExpr()
	p.expect(lexer.TOKEN_LBRACE)
	then := p.parseExpr()
	p.expect(lexer.TOKEN_RBRACE)
	p.expect(lexer.TOKEN_ELSE)
	if p.check(lexer.TOKEN_IF) {
		// else if → nested IfExpr
		elseExpr := p.parseIfExpr()
		return &IfExpr{Cond: cond, Then: then, Else: elseExpr}
	}
	p.expect(lexer.TOKEN_LBRACE)
	elseExpr := p.parseExpr()
	p.expect(lexer.TOKEN_RBRACE)
	return &IfExpr{Cond: cond, Then: then, Else: elseExpr}
}

// parseMatchExpr parses: match subject { case pat -> expr, case _ -> expr }
// Used when match appears in expression position (e.g., var x = match val { ... })
func (p *Parser) parseMatchExpr() Expr {
	p.expect(lexer.TOKEN_MATCH)
	subject := p.parseExpr()
	p.expect(lexer.TOKEN_LBRACE)
	var cases []*MatchExprCase
	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		p.expect(lexer.TOKEN_CASE)
		var pattern Expr
		if p.peek().Type == lexer.TOKEN_IDENT && p.peek().Literal == "_" {
			p.advance() // wildcard — pattern stays nil
		} else {
			pattern = p.parseExpr()
		}
		p.expect(lexer.TOKEN_ARROW)
		value := p.parseExpr()
		cases = append(cases, &MatchExprCase{Pattern: pattern, Value: value})
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &MatchExpr{Subject: subject, Cases: cases}
}

func (p *Parser) parseSpawnExpr() Expr {
	line := p.peek().Line
	p.expect(lexer.TOKEN_SPAWN)
	body := p.parseBlock()
	return &SpawnExpr{Line: line, Body: body}
}

func (p *Parser) parseForStmt() Stmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_FOR)

	// Detect: for item in expr { }
	// We look ahead: IDENT "in" expr
	if p.check(lexer.TOKEN_IDENT) && p.peekAt(1).Literal == "in" {
		item := p.advance().Literal // consume ident
		p.advance()                 // consume "in"
		rangeExpr := p.parseExpr()
		body := p.parseBlock()
		return &ForStmt{Line: line, IsRange: true, Item: item, Range: rangeExpr, Body: body}
	}

	// Detect: for i, item in expr { } (new) or for (i, item) in expr { } (legacy)
	if p.check(lexer.TOKEN_IDENT) &&
		p.peekAt(1).Type == lexer.TOKEN_COMMA &&
		p.peekAt(2).Type == lexer.TOKEN_IDENT &&
		p.peekAt(3).Literal == "in" {
		indexVar := p.advance().Literal // i
		p.advance()                     // ,
		item := p.advance().Literal     // item
		p.advance()                     // in
		rangeExpr := p.parseExpr()
		body := p.parseBlock()
		return &ForStmt{Line: line, IsRange: true, IndexVar: indexVar, Item: item, Range: rangeExpr, Body: body}
	}
	// Legacy: for (i, item) in expr { }
	if p.check(lexer.TOKEN_LPAREN) &&
		p.peekAt(1).Type == lexer.TOKEN_IDENT &&
		p.peekAt(2).Type == lexer.TOKEN_COMMA &&
		p.peekAt(3).Type == lexer.TOKEN_IDENT &&
		p.peekAt(4).Type == lexer.TOKEN_RPAREN &&
		p.peekAt(5).Literal == "in" {
		p.advance()                      // (
		indexVar := p.advance().Literal  // i
		p.advance()                      // ,
		item := p.advance().Literal      // item
		p.advance()                      // )
		p.advance()                      // in
		rangeExpr := p.parseExpr()
		body := p.parseBlock()
		return &ForStmt{Line: line, IsRange: true, IndexVar: indexVar, Item: item, Range: rangeExpr, Body: body}
	}

	// C-style: for init; cond; post { } (new) or for (init; cond; post) { } (legacy)
	hasParen := p.check(lexer.TOKEN_LPAREN)
	if hasParen {
		p.advance()
	}
	var init Stmt
	if !p.check(lexer.TOKEN_SEMICOLON) {
		init = p.parseVarOrAssign()
	}
	p.expect(lexer.TOKEN_SEMICOLON)
	var cond Expr
	if !p.check(lexer.TOKEN_SEMICOLON) {
		cond = p.parseExpr()
	}
	p.expect(lexer.TOKEN_SEMICOLON)
	var post Stmt
	if hasParen {
		if !p.check(lexer.TOKEN_RPAREN) {
			post = p.parseVarOrAssign()
		}
		p.expect(lexer.TOKEN_RPAREN)
	} else {
		if !p.check(lexer.TOKEN_LBRACE) {
			post = p.parseVarOrAssign()
		}
	}
	body := p.parseBlock()
	return &ForStmt{Line: line, Init: init, Cond: cond, Post: post, Body: body}
}

// parseVarOrAssign parses a short var decl or assignment for use in for-init/post.
func (p *Parser) parseVarOrAssign() Stmt {
	if p.check(lexer.TOKEN_VAR) {
		return p.parseShortVarStmt()
	}
	return p.parseExprOrAssignStmt()
}

func (p *Parser) parseWhileStmt() *WhileStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_WHILE)
	// Parens around condition are optional
	hasParen := p.check(lexer.TOKEN_LPAREN)
	if hasParen {
		p.advance()
	}
	cond := p.parseExpr()
	if hasParen {
		p.expect(lexer.TOKEN_RPAREN)
	}
	body := p.parseBlock()
	return &WhileStmt{Line: line, Cond: cond, Body: body}
}

func (p *Parser) parseGoStmt() *GoStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_GO)
	body := p.parseBlock()
	return &GoStmt{Line: line, Body: body}
}

// parseOrHandler parses an optional `or { block }` after a failable call.
// Returns nil if the current token is not TOKEN_OR.
func (p *Parser) parseOrHandler() *OrHandler {
	if !p.check(lexer.TOKEN_OR) {
		return nil
	}
	p.advance() // consume 'or'
	body := p.parseBlock()
	return &OrHandler{Body: body}
}

func (p *Parser) parsePrintStmt() *PrintStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_PRINT)
	p.expect(lexer.TOKEN_LPAREN)
	val := p.parseExpr()
	p.expect(lexer.TOKEN_RPAREN)
	return &PrintStmt{Line: line, Value: val}
}

func (p *Parser) parseMatchStmt() *MatchStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_MATCH)
	subject := p.parseExpr()
	p.expect(lexer.TOKEN_LBRACE)
	var cases []*MatchCase
	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		p.expect(lexer.TOKEN_CASE)
		var pattern Expr
		// case _ => is the wildcard/default
		if p.peek().Type == lexer.TOKEN_IDENT && p.peek().Literal == "_" {
			p.advance() // consume _; pattern stays nil
		} else {
			pattern = p.parseExpr()
		}
		p.expect(lexer.TOKEN_ARROW)
		body := p.parseBlock()
		cases = append(cases, &MatchCase{Pattern: pattern, Body: body})
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &MatchStmt{Line: line, Subject: subject, Cases: cases}
}

func (p *Parser) parseWithStmt() *WithStmt {
	line := p.peek().Line
	p.expect(lexer.TOKEN_WITH)
	p.expect(lexer.TOKEN_LPAREN)
	var resources []*WithResource
	for {
		name := p.expect(lexer.TOKEN_IDENT).Literal
		p.expect(lexer.TOKEN_ASSIGN)
		val := p.parseExpr()
		handler := p.parseOrHandler()
		resources = append(resources, &WithResource{Name: name, Value: val, OrHandler: handler})
		if !p.check(lexer.TOKEN_COMMA) {
			break
		}
		p.advance()
	}
	p.expect(lexer.TOKEN_RPAREN)
	body := p.parseBlock()
	return &WithStmt{Line: line, Resources: resources, Body: body}
}

func (p *Parser) parseExprOrAssignStmt() Stmt {
	line := p.peek().Line
	expr := p.parseExpr()

	// Check for assignment operators
	tok := p.peek()
	switch tok.Type {
	case lexer.TOKEN_ASSIGN, lexer.TOKEN_PLUS_EQ, lexer.TOKEN_MINUS_EQ,
		lexer.TOKEN_STAR_EQ, lexer.TOKEN_SLASH_EQ:
		p.advance()
		val := p.parseExpr()
		handler := p.parseOrHandler()
		return &AssignStmt{Line: line, Target: expr, Op: tok.Literal, Value: val, OrHandler: handler}
	}

	handler := p.parseOrHandler()
	return &ExprStmt{Line: line, Expr: expr, OrHandler: handler}
}

// --- Declarations ------------------------------------------------------------

// parseAnnotations collects zero or more @Name or @Name("arg1", "arg2") annotations.
func (p *Parser) parseAnnotations() []*Annotation {
	var annotations []*Annotation
	for p.check(lexer.TOKEN_AT) {
		p.advance() // consume @
		name := p.expect(lexer.TOKEN_IDENT).Literal
		var args []string
		if p.check(lexer.TOKEN_LPAREN) {
			p.advance() // consume (
			for !p.check(lexer.TOKEN_RPAREN) && !p.check(lexer.TOKEN_EOF) {
				arg := p.expect(lexer.TOKEN_STRING_LIT).Literal
				args = append(args, arg)
				if p.check(lexer.TOKEN_COMMA) {
					p.advance()
				}
			}
			p.expect(lexer.TOKEN_RPAREN)
		}
		annotations = append(annotations, &Annotation{Name: name, Args: args})
		p.skipSemis()
	}
	return annotations
}

func (p *Parser) parseFieldDecl(isPub bool) *FieldDecl {
	// [pub] [readonly] Type name [= default]
	isReadonly := false
	if p.check(lexer.TOKEN_READONLY) {
		isReadonly = true
		p.advance()
	}
	typ := p.parseType()
	name := p.expect(lexer.TOKEN_IDENT).Literal
	var def Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		def = p.parseExpr()
	}
	return &FieldDecl{Name: name, IsPub: isPub, IsReadonly: isReadonly, Type: typ, Default: def}
}

func (p *Parser) parseParam() *ParamDecl {
	// Type-before-name: Type name  or  Type... name
	typ := p.parseType()
	variadic := false
	if p.check(lexer.TOKEN_DOTDOTDOT) {
		variadic = true
		p.advance()
	}
	name := p.expect(lexer.TOKEN_IDENT).Literal
	var def Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		def = p.parseExpr()
	}
	return &ParamDecl{Name: name, Type: typ, Default: def, Variadic: variadic}
}

func (p *Parser) parseParamList() []*ParamDecl {
	p.expect(lexer.TOKEN_LPAREN)
	var params []*ParamDecl
	if !p.check(lexer.TOKEN_RPAREN) {
		params = append(params, p.parseParam())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			params = append(params, p.parseParam())
		}
	}
	p.expect(lexer.TOKEN_RPAREN)
	return params
}

func (p *Parser) parseCtorDecl() *CtorDecl {
	p.expect(lexer.TOKEN_NEW)
	params := p.parseParamList()
	body := p.parseBlock()

	// Extract super(args) from body (first ExprStmt with SuperCallExpr)
	var superArgs []Expr
	var newStmts []Stmt
	for _, s := range body.Stmts {
		if es, ok := s.(*ExprStmt); ok {
			if sc, ok := es.Expr.(*SuperCallExpr); ok {
				superArgs = sc.Args
				continue // remove from body
			}
		}
		newStmts = append(newStmts, s)
	}
	body.Stmts = newStmts

	return &CtorDecl{Params: params, Body: body, SuperArgs: superArgs}
}

func (p *Parser) parseMethodDecl() *MethodDecl {
	isPub := false
	isStatic := false
	if p.check(lexer.TOKEN_PUB) {
		isPub = true
		p.advance()
	}
	if p.check(lexer.TOKEN_STATIC) {
		isStatic = true
		p.advance()
	}
	// Type-before-name: [pub] [static] ReturnType name(params) { }
	// If current token is uppercase and next is lowercase ident followed by ( or <,
	// then it's a return type. Otherwise it's the method name (void return).
	var retType TypeExpr
	if p.looksLikeReturnType() {
		// Could be return type or void method name — disambiguate:
		// uppercase followed by lowercase+( = return type
		// uppercase followed by ( or < = void method name (e.g. MyMethod(...))
		// But method names are lowercase by convention in Zinc, so uppercase = return type
		// unless followed directly by ( or <
		next := p.peekAt(1)
		if next.Type == lexer.TOKEN_LPAREN || next.Type == lexer.TOKEN_LT {
			// It's the method name (void return), don't parse as type
		} else {
			retType = p.parseType()
		}
	}
	name := p.expect(lexer.TOKEN_IDENT).Literal
	params := p.parseParamList()
	body := p.parseBlock()
	return &MethodDecl{
		Name: name, IsPub: isPub, IsStatic: isStatic,
		Params: params, ReturnType: retType, Body: body,
	}
}

func (p *Parser) parseMethodSig() *MethodSig {
	isPub := false
	if p.check(lexer.TOKEN_PUB) {
		isPub = true
		p.advance()
	}
	// Type-before-name: [pub] ReturnType name(params) or [pub] name(params)
	var retType TypeExpr
	if p.looksLikeReturnType() {
		next := p.peekAt(1)
		if next.Type == lexer.TOKEN_LPAREN || next.Type == lexer.TOKEN_LT {
			// void method sig — the ident is the method name
		} else {
			retType = p.parseType()
		}
	}
	name := p.expect(lexer.TOKEN_IDENT).Literal
	params := p.parseParamList()
	return &MethodSig{Name: name, IsPub: isPub, Params: params, ReturnType: retType}
}

// isClassMethodDecl determines if the current uppercase IDENT in a class body
// starts a method declaration (with return type) rather than a field declaration.
// In type-before-name syntax:
//   - Field:  Type name [= default]     → after type+name, we see =, ;, }, EOF, or newline
//   - Method: ReturnType name(params)   → after type+name, we see ( or <
func (p *Parser) isClassMethodDecl() bool {
	return p.isClassMethodDeclAt(0)
}

func (p *Parser) isClassMethodDeclAt(offset int) bool {
	// Scan past the type (which may be generic, optional, etc.)
	i := offset + 1
	// Skip generic type args <...>
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
	// Skip ? after type
	if p.peekAt(i).Type == lexer.TOKEN_QUESTION {
		i++
	}
	// Now we should be at the name (lowercase ident)
	nameToken := p.peekAt(i)
	if nameToken.Type != lexer.TOKEN_IDENT {
		return false
	}
	i++
	// Check what follows the name
	next := p.peekAt(i)
	return next.Type == lexer.TOKEN_LPAREN || next.Type == lexer.TOKEN_LT
}

func (p *Parser) parseClassDecl() *ClassDecl {
	line := p.peek().Line
	name := p.expect(lexer.TOKEN_IDENT).Literal
	typeParams := p.parseTypeParams()
	var parents []string
	if p.check(lexer.TOKEN_COLON) {
		p.advance()
		parents = append(parents, p.expect(lexer.TOKEN_IDENT).Literal)
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			parents = append(parents, p.expect(lexer.TOKEN_IDENT).Literal)
		}
	}
	p.expect(lexer.TOKEN_LBRACE)

	var fields []*FieldDecl
	var ctor *CtorDecl
	var methods []*MethodDecl

	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		// Collect annotations before each member
		annots := p.parseAnnotations()

		tok := p.peek()
		switch {
		case tok.Type == lexer.TOKEN_NEW:
			ctor = p.parseCtorDecl()
		case tok.Type == lexer.TOKEN_STATIC:
			m := p.parseMethodDecl()
			m.Annotations = annots
			methods = append(methods, m)
		case tok.Type == lexer.TOKEN_READONLY:
			// readonly field (private readonly)
			f := p.parseFieldDecl(false)
			f.Annotations = annots
			fields = append(fields, f)
		case tok.Type == lexer.TOKEN_PUB:
			// pub can prefix methods or fields
			// Peek past pub to disambiguate
			next := p.peekAt(1)
			if next.Type == lexer.TOKEN_READONLY {
				// pub readonly — field
				p.advance() // consume pub
				f := p.parseFieldDecl(true)
				f.Annotations = annots
				fields = append(fields, f)
			} else if next.Type == lexer.TOKEN_STATIC {
				// pub static — always a method
				m := p.parseMethodDecl()
				m.Annotations = annots
				methods = append(methods, m)
			} else if next.Type == lexer.TOKEN_IDENT {
				nextIsUpper := len(next.Literal) > 0 && next.Literal[0] >= 'A' && next.Literal[0] <= 'Z'
				if !nextIsUpper {
					// pub lowercaseName( — void method
					m := p.parseMethodDecl()
					m.Annotations = annots
					methods = append(methods, m)
				} else {
					// pub UpperType — could be field or method with return type
					if p.isClassMethodDeclAt(1) {
						m := p.parseMethodDecl()
						m.Annotations = annots
						methods = append(methods, m)
					} else {
						p.advance() // consume pub
						f := p.parseFieldDecl(true)
						f.Annotations = annots
						fields = append(fields, f)
					}
				}
			} else {
				m := p.parseMethodDecl()
				m.Annotations = annots
				methods = append(methods, m)
			}
		case tok.Type == lexer.TOKEN_IDENT:
			isUpper := len(tok.Literal) > 0 && tok.Literal[0] >= 'A' && tok.Literal[0] <= 'Z'
			if !isUpper {
				next := p.peekAt(1)
				if next.Type == lexer.TOKEN_LPAREN || next.Type == lexer.TOKEN_LT {
					m := p.parseMethodDecl()
					m.Annotations = annots
					methods = append(methods, m)
				} else {
					p.errorf("unexpected token %s after method name %q in class body", next.Type, tok.Literal)
					p.advance()
				}
			} else {
				if p.isClassMethodDecl() {
					m := p.parseMethodDecl()
					m.Annotations = annots
					methods = append(methods, m)
				} else {
					f := p.parseFieldDecl(false)
					f.Annotations = annots
					fields = append(fields, f)
				}
			}
		default:
			p.errorf("unexpected token %s in class body", tok.Type)
			p.advance()
		}
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &ClassDecl{Line: line, Name: name, TypeParams: typeParams, Parents: parents, Fields: fields, Ctor: ctor, Methods: methods}
}

func (p *Parser) parseInterfaceDecl() *InterfaceDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_INTERFACE)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.expect(lexer.TOKEN_LBRACE)
	var methods []*MethodSig
	p.skipSemis()
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		methods = append(methods, p.parseMethodSig())
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &InterfaceDecl{Line: line, Name: name, Methods: methods}
}

// parseTypeParams parses optional <T, U> type parameter list.
func (p *Parser) parseTypeParams() []string {
	if !p.check(lexer.TOKEN_LT) {
		return nil
	}
	p.advance() // <
	var params []string
	params = append(params, p.expect(lexer.TOKEN_IDENT).Literal)
	for p.check(lexer.TOKEN_COMMA) {
		p.advance()
		params = append(params, p.expect(lexer.TOKEN_IDENT).Literal)
	}
	p.expect(lexer.TOKEN_GT)
	return params
}

// looksLikeTypeArgs peeks ahead to determine if '<' starts type arguments
// (e.g. <Config>, <K, V>) followed by '(' — not a comparison operator.
func (p *Parser) looksLikeTypeArgs() bool {
	off := 1 // skip '<'
	for {
		if p.peekAt(off).Type != lexer.TOKEN_IDENT {
			return false
		}
		off++ // skip ident
		if p.peekAt(off).Type == lexer.TOKEN_GT {
			// Check that '>' is followed by '(' — confirms call syntax
			return p.peekAt(off+1).Type == lexer.TOKEN_LPAREN
		}
		if p.peekAt(off).Type != lexer.TOKEN_COMMA {
			return false
		}
		off++ // skip comma
	}
}

// parseCallTypeArgs parses <Type, Type, ...> at a call site.
func (p *Parser) parseCallTypeArgs() []string {
	p.expect(lexer.TOKEN_LT)
	var args []string
	args = append(args, p.expect(lexer.TOKEN_IDENT).Literal)
	for p.check(lexer.TOKEN_COMMA) {
		p.advance()
		args = append(args, p.expect(lexer.TOKEN_IDENT).Literal)
	}
	p.expect(lexer.TOKEN_GT)
	return args
}

func (p *Parser) parseFnDecl(isPub bool) *FnDecl {
	line := p.peek().Line
	// Type-before-name: ReturnType name<T>(params) { } or name(params) { }
	var retType TypeExpr
	if p.looksLikeReturnType() {
		// Could be return type or function name — disambiguate:
		// uppercase followed by lowercase ident = return type (e.g. Int add(...))
		// uppercase followed by ( or < = function name (unlikely since fn names are lowercase)
		next := p.peekAt(1)
		if next.Type == lexer.TOKEN_IDENT && len(next.Literal) > 0 && next.Literal[0] >= 'a' && next.Literal[0] <= 'z' {
			retType = p.parseType()
		}
	}
	name := p.expect(lexer.TOKEN_IDENT).Literal
	typeParams := p.parseTypeParams()
	params := p.parseParamList()
	body := p.parseBlock()
	return &FnDecl{Line: line, Name: name, IsPub: isPub, TypeParams: typeParams, Params: params, ReturnType: retType, Body: body}
}

func (p *Parser) parseEnumDecl() *EnumDecl {
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

// parseDataClassDecl parses: data Name[<T>](params) [: Parents] [{ methods }]
func (p *Parser) parseDataClassDecl() *DataClassDecl {
	line := p.peek().Line
	p.expect(lexer.TOKEN_DATA)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	typeParams := p.parseTypeParams()

	// Parse constructor params — these become fields
	p.expect(lexer.TOKEN_LPAREN)
	var params []*FieldDecl
	if !p.check(lexer.TOKEN_RPAREN) {
		params = append(params, p.parseDataClassParam())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			if p.check(lexer.TOKEN_RPAREN) {
				break
			}
			params = append(params, p.parseDataClassParam())
		}
	}
	p.expect(lexer.TOKEN_RPAREN)

	// Optional parents
	var parents []string
	if p.check(lexer.TOKEN_COLON) {
		p.advance()
		parents = append(parents, p.expect(lexer.TOKEN_IDENT).Literal)
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			parents = append(parents, p.expect(lexer.TOKEN_IDENT).Literal)
		}
	}

	// Optional body with methods
	var methods []*MethodDecl
	if p.check(lexer.TOKEN_LBRACE) {
		p.advance()
		p.skipSemis()
		for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
			annots := p.parseAnnotations()
			m := p.parseMethodDecl()
			m.Annotations = annots
			methods = append(methods, m)
			p.skipSemis()
		}
		p.expect(lexer.TOKEN_RBRACE)
	}

	return &DataClassDecl{
		Line:       line,
		Name:       name,
		TypeParams: typeParams,
		Parents:    parents,
		Params:     params,
		Methods:    methods,
	}
}

// parseDataClassParam parses a single data class parameter: [pub] Type name
func (p *Parser) parseDataClassParam() *FieldDecl {
	isPub := false
	if p.check(lexer.TOKEN_PUB) {
		isPub = true
		p.advance()
	}
	typ := p.parseType()
	name := p.expect(lexer.TOKEN_IDENT).Literal
	return &FieldDecl{Name: name, IsPub: isPub, Type: typ}
}

func (p *Parser) parseImportDecl() *ImportDecl {
	p.expect(lexer.TOKEN_IMPORT)
	path := p.expect(lexer.TOKEN_STRING_LIT).Literal
	alias := ""
	if p.check(lexer.TOKEN_AS) {
		p.advance()
		alias = p.expect(lexer.TOKEN_IDENT).Literal
	}
	return &ImportDecl{Path: path, Alias: alias}
}

func (p *Parser) parseUseDecl() *ImportDecl {
	p.expect(lexer.TOKEN_USE)
	// Parse dotted identifier: System.Text.Json or just "http"
	path := p.expect(lexer.TOKEN_IDENT).Literal
	for p.check(lexer.TOKEN_DOT) {
		p.advance()
		path += "." + p.expect(lexer.TOKEN_IDENT).Literal
	}
	alias := ""
	if p.check(lexer.TOKEN_AS) {
		p.advance()
		alias = p.expect(lexer.TOKEN_IDENT).Literal
	}
	return &ImportDecl{Path: path, Alias: alias}
}

func (p *Parser) parsePackageDecl() *PackageDecl {
	p.expect(lexer.TOKEN_PACKAGE)
	path := p.expect(lexer.TOKEN_STRING_LIT).Literal
	return &PackageDecl{Path: path}
}

// --- Program -----------------------------------------------------------------

// Parse parses the full program and returns the AST.
// Parse is the v1 entry point — deprecated, use ParseV2() instead.

// ErrorString returns all parser errors as a single string.
func (p *Parser) ErrorString() string {
	return strings.Join(p.Errors, "\n")
}
