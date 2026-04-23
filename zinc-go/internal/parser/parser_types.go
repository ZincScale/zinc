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
	"strings"

	"zinc-go/internal/lexer"
)

// --- Types (v2: angle-bracket generics <>) -----------------------------------

// v2ParseType: str, int, list<int>, dict<str, int>, str?
func (p *Parser) v2ParseType() TypeExpr {
	tok := p.expect(lexer.TOKEN_IDENT)
	name := tok.Literal

	// Dotted/fully-qualified names: java.util.Map
	for p.check(lexer.TOKEN_DOT) && isIdentLike(p.peekAt(1).Type) {
		p.advance() // consume .
		name += "." + p.advance().Literal
	}

	return p.v2ParseTypeFrom(name)
}

// v2ParseTypeFrom continues parsing a type expression given a name already consumed.
func (p *Parser) v2ParseTypeFrom(name string) TypeExpr {

	var typ TypeExpr

	// Function type: Fn<(ParamTypes), ReturnType> or Fn<(ParamTypes)>
	if name == "Fn" && p.check(lexer.TOKEN_LT) {
		p.advance() // consume <
		p.expect(lexer.TOKEN_LPAREN)
		var params []TypeExpr
		if !p.check(lexer.TOKEN_RPAREN) {
			params = append(params, p.v2ParseType())
			for p.check(lexer.TOKEN_COMMA) {
				p.advance()
				params = append(params, p.v2ParseType())
			}
		}
		p.expect(lexer.TOKEN_RPAREN)
		var retType TypeExpr
		if p.check(lexer.TOKEN_COMMA) {
			p.advance()
			retType = p.v2ParseType()
		}
		p.expect(lexer.TOKEN_GT)
		typ = &FuncTypeExpr{Params: params, ReturnType: retType}
	} else if p.check(lexer.TOKEN_LT) {
		// Angle-bracket generics: List<int>, Map<String, int>
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

// formatTypeExpr converts a TypeExpr back to its string representation.
func (p *Parser) formatTypeExpr(t TypeExpr) string {
	switch t := t.(type) {
	case *SimpleType:
		return t.Name
	case *GenericType:
		var args []string
		for _, a := range t.TypeArgs {
			args = append(args, p.formatTypeExpr(a))
		}
		return t.Name + "<" + strings.Join(args, ", ") + ">"
	case *ArrayType:
		return p.formatTypeExpr(t.ElementType) + "[]"
	case *OptionalType:
		return p.formatTypeExpr(t.Inner) + "?"
	default:
		return "Object"
	}
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
	// Skip past dotted segments: java.util.Map → find position after last ident
	i := 1
	for p.peekAt(i).Type == lexer.TOKEN_DOT && isIdentLike(p.peekAt(i+1).Type) {
		i += 2
	}
	next := p.peekAt(i)
	// ident ident → type name
	if next.Type == lexer.TOKEN_IDENT || next.Type == lexer.TOKEN_DATA ||
		next.Type == lexer.TOKEN_MATCH || next.Type == lexer.TOKEN_PRINT {
		return true
	}
	// ident< → generic type
	if next.Type == lexer.TOKEN_LT {
		return true
	}
	// ident... → variadic type (Type... name)
	if next.Type == lexer.TOKEN_DOTDOTDOT {
		return true
	}
	// ident[] ident → array type + name
	if next.Type == lexer.TOKEN_LBRACKET {
		peek2 := p.peekAt(i + 1)
		if peek2.Type == lexer.TOKEN_RBRACKET {
			peek3 := p.peekAt(i + 2)
			return peek3.Type == lexer.TOKEN_IDENT || peek3.Type == lexer.TOKEN_DATA ||
				peek3.Type == lexer.TOKEN_MATCH || peek3.Type == lexer.TOKEN_PRINT
		}
	}
	// ident? ident → nullable type + name
	if next.Type == lexer.TOKEN_QUESTION {
		peek2 := p.peekAt(i + 1)
		return peek2.Type == lexer.TOKEN_IDENT || peek2.Type == lexer.TOKEN_DATA ||
			peek2.Type == lexer.TOKEN_MATCH || peek2.Type == lexer.TOKEN_PRINT
	}
	return false
}
