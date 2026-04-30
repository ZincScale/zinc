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
	"zinc-go/internal/lexer"
)

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

// v2ParseOr: expr || expr
// NOTE: 'or' keyword is NOT a boolean operator — it's the error handler.
// Use || for boolean OR. 'or' is parsed at statement level (or { }, or match, or default).
func (p *Parser) v2ParseOr() Expr {
	left := p.v2ParseAnd()
	for p.check(lexer.TOKEN_PIPE_PIPE) {
		p.advance() // consume ||
		right := p.v2ParseAnd()
		left = &BinaryExpr{Left: left, Op: "||", Right: right}
	}
	return left
}

// v2ParseAnd: expr and expr  OR  expr && expr
func (p *Parser) v2ParseAnd() Expr {
	left := p.v2ParseNot()
	for p.check(lexer.TOKEN_AMP_AMP) || p.check(lexer.TOKEN_AND) {
		p.advance() // consume && or 'and'
		right := p.v2ParseNot()
		left = &BinaryExpr{Left: left, Op: "&&", Right: right}
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
	return p.v2ParseBitwiseOr()
}

// v2ParseBitwiseOr: expr | expr
func (p *Parser) v2ParseBitwiseOr() Expr {
	left := p.v2ParseBitwiseXor()
	for p.check(lexer.TOKEN_PIPE) {
		p.advance()
		right := p.v2ParseBitwiseXor()
		left = &BinaryExpr{Left: left, Op: "|", Right: right}
	}
	return left
}

// v2ParseBitwiseXor: expr ^ expr
func (p *Parser) v2ParseBitwiseXor() Expr {
	left := p.v2ParseBitwiseAnd()
	for p.check(lexer.TOKEN_CARET) {
		p.advance()
		right := p.v2ParseBitwiseAnd()
		left = &BinaryExpr{Left: left, Op: "^", Right: right}
	}
	return left
}

// v2ParseBitwiseAnd: expr & expr
func (p *Parser) v2ParseBitwiseAnd() Expr {
	left := p.v2ParseComparison()
	for p.check(lexer.TOKEN_AMP) {
		p.advance()
		right := p.v2ParseComparison()
		left = &BinaryExpr{Left: left, Op: "&", Right: right}
	}
	return left
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
		// Guard: if two adjacent < or > tokens form a shift operator, don't consume as comparison
		if (p.check(lexer.TOKEN_GT) || p.check(lexer.TOKEN_LT)) {
			cur := p.peek()
			next := p.peekAt(1)
			if cur.Type == next.Type && cur.Line == next.Line && next.Col == cur.Col+1 {
				break // let v2ParseShift handle it
			}
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
	// Handle "as Type" for type casting: expr as TypeName, expr as pkg.TypeName.
	// The trailing type is parsed as IDENT (DOT IDENT)* so qualified references
	// like `hambaAvro.RecordSchema` or `os.File` reach codegen as a single
	// dotted string. formatType (codegen_resolve.go) already splits on `.` to
	// resolve the package alias, so no AST or codegen change is needed here.
	if p.check(lexer.TOKEN_AS) {
		p.advance() // consume as
		typeName := p.advance().Literal
		for p.check(lexer.TOKEN_DOT) && p.peekAt(1).Type == lexer.TOKEN_IDENT {
			p.advance() // consume .
			typeName += "." + p.advance().Literal
		}
		left = &TypeAssertExpr{Object: left, TypeName: typeName, IsCheck: false}
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
	left := p.v2ParseShift()
	for p.match(lexer.TOKEN_PLUS, lexer.TOKEN_MINUS) {
		op := p.advance().Literal
		right := p.v2ParseShift()
		left = &BinaryExpr{Left: left, Op: op, Right: right}
	}
	return left
}

// v2ParseShift: expr << expr, expr >> expr
// Two adjacent < or > tokens (no whitespace gap) form a shift operator.
// This only fires in expression context — generics are parsed in type context
// (v2ParseType) which never calls this function, so Map<String, List<int>> is safe.
func (p *Parser) v2ParseShift() Expr {
	left := p.v2ParseMulDiv()
	for {
		cur := p.peek()
		next := p.peekAt(1)
		// Right shift: two adjacent > tokens
		if cur.Type == lexer.TOKEN_GT && next.Type == lexer.TOKEN_GT &&
			cur.Line == next.Line && next.Col == cur.Col+1 {
			p.advance() // consume first >
			p.advance() // consume second >
			right := p.v2ParseMulDiv()
			left = &BinaryExpr{Left: left, Op: ">>", Right: right}
			continue
		}
		// Left shift: two adjacent < tokens
		if cur.Type == lexer.TOKEN_LT && next.Type == lexer.TOKEN_LT &&
			cur.Line == next.Line && next.Col == cur.Col+1 {
			p.advance() // consume first <
			p.advance() // consume second <
			right := p.v2ParseMulDiv()
			left = &BinaryExpr{Left: left, Op: "<<", Right: right}
			continue
		}
		break
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

// v2ParseUnary: -expr | &expr
//
// Prefix `&` exists solely as an FFI escape hatch — for cases where a Go
// library's runtime contract requires a pointer but its static signature
// is `any` (e.g. hamba/avro `Unmarshal(data, v any)`). The validator then
// rejects `&x` anywhere except as a top-level argument to a call into an
// imported Go package. Binary `&` (bitwise AND) lives at lower precedence
// in v2ParseBitwiseAnd; entering this prefix branch is unambiguous because
// it only fires at the start of a unary position.
func (p *Parser) v2ParseUnary() Expr {
	if p.check(lexer.TOKEN_MINUS) || p.check(lexer.TOKEN_AMP) {
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
	return p.v2ParsePostfixFrom(p.v2ParsePrimary())
}

func (p *Parser) v2ParsePostfixFrom(expr Expr) Expr {
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
		case p.check(lexer.TOKEN_LT):
			// Check for generic call: ident<Type>(args) or pkg.Type<Args>(args)
			_, isIdent := expr.(*Ident)
			_, isSel := expr.(*SelectorExpr)
			if (isIdent || isSel) && p.looksLikeTypeArgs() {
				typeArgs := p.parseCallTypeArgs()
				p.expect(lexer.TOKEN_LPAREN)
				call := p.finishCallArgsNoLParen(expr).(*CallExpr)
				call.TypeArgs = typeArgs
				expr = call
			} else {
				return expr
			}
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
	// Check for spread: arg...
	if p.check(lexer.TOKEN_DOTDOTDOT) {
		p.advance()
		arg = &SpreadExpr{Expr: arg}
	}
	*args = append(*args, arg)
}

// v2ParseMatchExpr: match subject { case pat { expr } ... }
// Returns a MatchExpr for use in expression position (e.g., var x = match ...)
func (p *Parser) v2ParseMatchExpr() Expr {
	p.expect(lexer.TOKEN_MATCH)
	subject := p.v2ParseExpr()
	p.expect(lexer.TOKEN_LBRACE)
	p.skipSemis()

	var cases []*MatchExprCase
	for !p.check(lexer.TOKEN_RBRACE) && !p.check(lexer.TOKEN_EOF) {
		p.expect(lexer.TOKEN_CASE)
		var pattern Expr
		if p.check(lexer.TOKEN_IDENT) && p.peek().Literal == "_" {
			p.advance() // wildcard
			pattern = nil
		} else {
			pattern = p.v2ParseExpr()
		}
		// case pattern { value-expr }  OR  case pattern -> value-expr
		if p.check(lexer.TOKEN_LBRACE) {
			p.advance()
			value := p.v2ParseExpr()
			p.skipSemis()
			p.expect(lexer.TOKEN_RBRACE)
			cases = append(cases, &MatchExprCase{Pattern: pattern, Value: value})
		} else if p.check(lexer.TOKEN_ARROW) {
			p.advance()
			value := p.v2ParseExpr()
			cases = append(cases, &MatchExprCase{Pattern: pattern, Value: value})
		}
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &MatchExpr{Subject: subject, Cases: cases}
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
	case lexer.TOKEN_NULL:
		p.advance()
		return &NullLit{}
	case lexer.TOKEN_SPAWN:
		line := tok.Line
		p.advance()
		body := p.v2ParseBlock()
		spawn := &SpawnExpr{Line: line, Body: body}
		if p.check(lexer.TOKEN_OR) {
			p.advance()
			spawn.OrHandler = &OrHandler{Body: p.v2ParseBlock()}
		}
		return spawn
	case lexer.TOKEN_NEW:
		p.advance() // consume "new"
		// Parse type name (possibly dotted: bytes.Buffer, java.util.ArrayList)
		nameTok := p.expect(lexer.TOKEN_IDENT)
		name := nameTok.Literal
		for p.check(lexer.TOKEN_DOT) && isIdentLike(p.peekAt(1).Type) {
			p.advance() // consume .
			name += "." + p.advance().Literal
		}
		// Parse optional generic type args: <T, U>
		var typeArgs []string
		if p.check(lexer.TOKEN_LT) {
			p.advance() // consume <
			typeArgs = append(typeArgs, p.formatTypeExpr(p.v2ParseType()))
			for p.check(lexer.TOKEN_COMMA) {
				p.advance()
				typeArgs = append(typeArgs, p.formatTypeExpr(p.v2ParseType()))
			}
			p.expect(lexer.TOKEN_GT)
		}
		// Parens are optional: `new bytes.Buffer` ≡ `new bytes.Buffer()`. The
		// no-paren form is the natural shape for zero-value-usable Go-stdlib
		// types (sync.Mutex, bytes.Buffer, strings.Builder) where there are
		// no init args. Either way the result is a CallExpr with IsNew=true.
		ident := &Ident{Name: name}
		var call *CallExpr
		if p.check(lexer.TOKEN_LPAREN) {
			call = p.v2ParseCallArgs(ident).(*CallExpr)
		} else {
			call = &CallExpr{Callee: ident}
		}
		call.TypeArgs = typeArgs
		call.IsNew = true
		return call
	case lexer.TOKEN_THIS:
		p.advance()
		return &Ident{Name: "this"}
	case lexer.TOKEN_SUPER:
		p.advance()
		return &Ident{Name: "super"}
	case lexer.TOKEN_MATCH:
		return p.v2ParseMatchExpr()
	case lexer.TOKEN_IDENT, lexer.TOKEN_PRINT, lexer.TOKEN_DATA:
		// print and data are regular identifiers in expression context
		// Check for sized array: Type[size] — e.g. byte[4], int[10], String[5]
		if p.peekAt(1).Type == lexer.TOKEN_LBRACKET && p.looksLikeSizedArray() {
			name := p.advance().Literal
			p.advance() // consume [
			size := p.v2ParseExpr()
			p.expect(lexer.TOKEN_RBRACKET)
			return &SizedArrayExpr{ElementType: name, Size: size}
		}
		// Check for lambda: name -> expr
		if p.peekAt(1).Type == lexer.TOKEN_ARROW {
			return p.v2ParseLambda()
		}
		// Check for collection with capacity: List<T>(cap) or Map<K,V>(cap)
		if (tok.Literal == "List" || tok.Literal == "Map") && p.peekAt(1).Type == lexer.TOKEN_LT && p.looksLikeCapacityAt(1) {
			name := p.advance().Literal
			p.advance() // consume <
			var typeArgs []TypeExpr
			typeArgs = append(typeArgs, p.v2ParseType())
			for p.check(lexer.TOKEN_COMMA) {
				p.advance()
				typeArgs = append(typeArgs, p.v2ParseType())
			}
			p.expect(lexer.TOKEN_GT)
			typ := &GenericType{Name: name, TypeArgs: typeArgs}
			p.advance() // consume (
			capacity := p.v2ParseExpr()
			p.expect(lexer.TOKEN_RPAREN)
			return &CapacityExpr{CollectionType: typ, Capacity: capacity}
		}
		// Check for typed literal: List<Type>[] or Map<K,V>{}
		if p.peekAt(1).Type == lexer.TOKEN_LT && p.looksLikeTypedLiteralAt(1) {
			name := p.advance().Literal
			// Parse <TypeArgs> manually (don't use v2ParseTypeFrom which eats [])
			p.advance() // consume <
			var typeArgs []TypeExpr
			typeArgs = append(typeArgs, p.v2ParseType())
			for p.check(lexer.TOKEN_COMMA) {
				p.advance()
				typeArgs = append(typeArgs, p.v2ParseType())
			}
			p.expect(lexer.TOKEN_GT)
			typ := &GenericType{Name: name, TypeArgs: typeArgs}

			if p.check(lexer.TOKEN_LBRACKET) {
				p.advance() // consume [
				if p.check(lexer.TOKEN_RBRACKET) {
					p.advance() // consume ]
					return &ListLit{ExplicitType: typ}
				}
				// Non-empty: List<String>["a", "b", "c"]
				var elems []Expr
				elems = append(elems, p.v2ParseExpr())
				for p.check(lexer.TOKEN_COMMA) {
					p.advance()
					if p.check(lexer.TOKEN_RBRACKET) { break }
					elems = append(elems, p.v2ParseExpr())
				}
				p.expect(lexer.TOKEN_RBRACKET)
				return &ListLit{Elements: elems, ExplicitType: typ}
			} else if p.check(lexer.TOKEN_LBRACE) {
				p.advance() // {
				if p.check(lexer.TOKEN_RBRACE) {
					p.advance() // }
					return &MapLit{ExplicitType: typ}
				}
				// Non-empty: Map<K,V>{"a": 1}
				var keys, vals []Expr
				k := p.v2ParseExpr()
				p.expect(lexer.TOKEN_COLON)
				v := p.v2ParseExpr()
				keys = append(keys, k)
				vals = append(vals, v)
				for p.check(lexer.TOKEN_COMMA) {
					p.advance()
					if p.check(lexer.TOKEN_RBRACE) { break }
					k = p.v2ParseExpr()
					p.expect(lexer.TOKEN_COLON)
					v = p.v2ParseExpr()
					keys = append(keys, k)
					vals = append(vals, v)
				}
				p.expect(lexer.TOKEN_RBRACE)
				return &MapLit{Keys: keys, Values: vals, ExplicitType: typ}
			}
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

// v2ParseLambda: name -> expr  OR  name -> { stmts }
func (p *Parser) v2ParseLambda() Expr {
	name := p.advance().Literal // param name
	p.advance()                 // consume ->
	param := &ParamDecl{Name: name}
	// Block lambda: x -> { ... }
	if p.check(lexer.TOKEN_LBRACE) {
		body := p.v2ParseBlock()
		return &LambdaExpr{Params: []*ParamDecl{param}, Body: body}
	}
	// Expression lambda: x -> expr
	expr := p.v2ParseExpr()
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

// looksLikeSizedArray checks if current position is Type[size] (sized array creation).
// Distinguishes from arr[i] (indexing) by checking if the ident is a known type name.
func (p *Parser) looksLikeSizedArray() bool {
	name := p.peek().Literal
	// Known primitive types
	switch name {
	case "byte", "int", "long", "float", "double", "boolean", "bool", "String", "string", "char":
		// Must have [ expr ] where expr is not empty
		// peekAt(1) is [, peekAt(2) must not be ] (that would be Type[] empty array)
		return p.peekAt(2).Type != lexer.TOKEN_RBRACKET
	}
	return false
}

// v2LooksLikeLambdaParams checks if ( starts lambda params.
// Handles: untyped (a, b) ->, typed (int a, String b) ->,
// zero-param () ->, and qualified typed (io.Writer w, slog.HandlerOptions opts) ->
func (p *Parser) v2LooksLikeLambdaParams() bool {
	// Zero-param lambda: () ->
	if p.peekAt(1).Type == lexer.TOKEN_RPAREN && p.peekAt(2).Type == lexer.TOKEN_ARROW {
		return true
	}
	off := 1
	for {
		tok := p.peekAt(off)
		if tok.Type != lexer.TOKEN_IDENT {
			return false
		}
		off++
		// Skip dotted type name: io.Writer, slog.HandlerOptions
		for p.peekAt(off).Type == lexer.TOKEN_DOT && p.peekAt(off+1).Type == lexer.TOKEN_IDENT {
			off += 2
		}
		// Skip generic type args: List<String>, Map<K,V>
		if p.peekAt(off).Type == lexer.TOKEN_LT {
			depth := 1
			off++
			for depth > 0 && p.peekAt(off).Type != lexer.TOKEN_EOF {
				if p.peekAt(off).Type == lexer.TOKEN_LT {
					depth++
				} else if p.peekAt(off).Type == lexer.TOKEN_GT {
					depth--
				}
				off++
			}
		}
		next := p.peekAt(off)
		// Typed param: Type name — the next token is another ident (the name)
		if next.Type == lexer.TOKEN_IDENT {
			off++ // skip the name
			next = p.peekAt(off)
		}
		// After param (typed or untyped), expect ) or ,
		if next.Type == lexer.TOKEN_RPAREN {
			return p.peekAt(off+1).Type == lexer.TOKEN_ARROW
		}
		if next.Type == lexer.TOKEN_COMMA {
			off++
			continue
		}
		return false
	}
}

// v2ParseMultiParamLambda: (a, b) -> expr  OR  (a, b) -> { stmts }
// Also handles zero-param lambdas: () -> expr / () -> { stmts }.
func (p *Parser) v2ParseMultiParamLambda() Expr {
	p.advance() // consume (
	var params []*ParamDecl
	if !p.check(lexer.TOKEN_RPAREN) {
		params = append(params, p.parseLambdaParam())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			params = append(params, p.parseLambdaParam())
		}
	}
	p.expect(lexer.TOKEN_RPAREN)
	p.expect(lexer.TOKEN_ARROW)
	// Block lambda: (a, b) -> { ... }
	if p.check(lexer.TOKEN_LBRACE) {
		body := p.v2ParseBlock()
		return &LambdaExpr{Params: params, Body: body}
	}
	// Expression lambda: (a, b) -> expr
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

// v2ParseInterpString converts "Hello, ${name}!" into StringInterpLit.
func (p *Parser) v2ParseInterpString(raw string) Expr {
	var parts []Expr
	buf := ""
	i := 0
	runes := []rune(raw)
	for i < len(runes) {
		if runes[i] == '$' && i+1 < len(runes) && runes[i+1] == '{' {
			if buf != "" {
				parts = append(parts, &StringLit{Value: buf})
				buf = ""
			}
			i += 2 // skip ${
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
