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

// parser.go — legacy v1 parser with shared utilities.
//
// The v2 parser (parser_v2.go) is the active parser. This file remains because
// v2 calls parseLambdaParam() and finishCallArgsNoLParen(), which transitively
// depend on v1's expression/type parsing. Once those two functions are migrated
// to use v2ParseExpr/v2ParseType, this file can be deleted.
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
				namedArgs = append(namedArgs, NamedArg{Name: name, Value: p.v2ParseExpr()})
			} else {
				if seenNamed {
					p.errorf("positional argument after named argument")
				}
				expr := p.v2ParseExpr()
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
		// Dotted type name: io.Writer, slog.HandlerOptions
		// Pattern: pkg.Type paramName (,|))
		if p.peekAt(1).Type == lexer.TOKEN_DOT {
			// Walk past dotted segments: io.Writer → off lands on last segment
			off := 2
			for p.peekAt(off).Type == lexer.TOKEN_IDENT && p.peekAt(off+1).Type == lexer.TOKEN_DOT {
				off += 2
			}
			// off points to last type segment (e.g. Writer)
			// off+1 should be param name (ident), off+2 should be , or )
			if p.peekAt(off).Type == lexer.TOKEN_IDENT &&
				p.peekAt(off+1).Type == lexer.TOKEN_IDENT &&
				(p.peekAt(off+2).Type == lexer.TOKEN_COMMA || p.peekAt(off+2).Type == lexer.TOKEN_RPAREN) {
				isTyped = true
			}
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
		typ := p.v2ParseType()
		variadic := false
		if p.check(lexer.TOKEN_DOTDOTDOT) {
			variadic = true
			p.advance()
		}
		name := p.expect(lexer.TOKEN_IDENT).Literal
		var def Expr
		if p.check(lexer.TOKEN_ASSIGN) {
			p.advance()
			def = p.v2ParseExpr()
		}
		return &ParamDecl{Name: name, Type: typ, Default: def, Variadic: variadic}
	}
	// Untyped param: just the name (type will be inferred during codegen)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	return &ParamDecl{Name: name}
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
		// Support dotted type args: core.FlowFile
		for p.peekAt(off).Type == lexer.TOKEN_DOT && p.peekAt(off+1).Type == lexer.TOKEN_IDENT {
			off += 2 // skip . and ident
		}
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

// looksLikeCapacityAt checks if <Type, ...>(capacity) follows — for List<T>(cap) and Map<K,V>(cap).
func (p *Parser) looksLikeCapacityAt(base int) bool {
	off := base + 1
	depth := 1
	for depth > 0 {
		tok := p.peekAt(off).Type
		if tok == lexer.TOKEN_EOF {
			return false
		}
		if tok == lexer.TOKEN_LT {
			depth++
		} else if tok == lexer.TOKEN_GT {
			depth--
			if depth == 0 {
				return p.peekAt(off+1).Type == lexer.TOKEN_LPAREN
			}
		}
		off++
	}
	return false
}

// looksLikeTypedLiteral checks if we're at < and the pattern is <Type, ...>[] or <Type, ...>{}.
// This enables: var x = List<int>[]  or  var m = Map<String, int>{}
func (p *Parser) looksLikeTypedLiteral() bool {
	return p.looksLikeTypedLiteralAt(0)
}

// looksLikeTypedLiteralAt checks from a given offset whether we see <Type, ...>[] or <Type, ...>{}.
// Supports nested generics: Map<String, List<RoutingRule>>{}
func (p *Parser) looksLikeTypedLiteralAt(base int) bool {
	off := base + 1 // skip '<'
	depth := 1
	for depth > 0 {
		tok := p.peekAt(off).Type
		if tok == lexer.TOKEN_EOF {
			return false
		}
		if tok == lexer.TOKEN_LT {
			depth++
		} else if tok == lexer.TOKEN_GT {
			depth--
			if depth == 0 {
				// Closing > of the outermost generic — check what follows
				next := p.peekAt(off + 1).Type
				// >[] or >[...] — typed list literal (empty or non-empty)
				if next == lexer.TOKEN_LBRACKET {
					return true
				}
				// >{} or >{...} — typed map literal (empty or non-empty)
				if next == lexer.TOKEN_LBRACE {
					return true
				}
				// >(capacity) — collection with initial capacity (List/Map only)
				// Don't match here — handled separately in primary expr parser
				return false
			}
		}
		off++
	}
	return false
}

// parseCallTypeArgs parses <Type, Type, ...> at a call site.
// Supports dotted names: <core.FlowFile, String>
func (p *Parser) parseCallTypeArgs() []string {
	p.expect(lexer.TOKEN_LT)
	var args []string
	name := p.expect(lexer.TOKEN_IDENT).Literal
	for p.check(lexer.TOKEN_DOT) && isIdentLike(p.peekAt(1).Type) {
		p.advance()
		name += "." + p.advance().Literal
	}
	args = append(args, name)
	for p.check(lexer.TOKEN_COMMA) {
		p.advance()
		name = p.expect(lexer.TOKEN_IDENT).Literal
		for p.check(lexer.TOKEN_DOT) && isIdentLike(p.peekAt(1).Type) {
			p.advance()
			name += "." + p.advance().Literal
		}
		args = append(args, name)
	}
	p.expect(lexer.TOKEN_GT)
	return args
}

// --- Program -----------------------------------------------------------------

// Parse parses the full program and returns the AST.
// Parse is the v1 entry point — deprecated, use ParseV2() instead.

// ErrorString returns all parser errors as a single string.
func (p *Parser) ErrorString() string {
	return strings.Join(p.Errors, "\n")
}
