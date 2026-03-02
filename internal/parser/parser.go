package parser

import (
	"fmt"
	"strings"

	"growler/internal/lexer"
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

// --- Type Expressions --------------------------------------------------------

func (p *Parser) parseType() TypeExpr {
	tok := p.expect(lexer.TOKEN_IDENT)
	name := tok.Literal
	var t TypeExpr
	if p.check(lexer.TOKEN_LT) {
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
	// Optional suffix: Type?
	if p.check(lexer.TOKEN_QUESTION) {
		p.advance()
		return &OptionalType{Inner: t}
	}
	return t
}

// --- Expressions (Pratt parser) -----------------------------------------------

type precedence int

const (
	precNone       precedence = iota
	precOr                    // ||
	precAnd                   // &&
	precEquality              // == !=
	precComparison            // < > <= >=
	precAddSub                // + -
	precMulDiv                // * / %
	precUnary                 // ! -
	precCall                  // . () []
)

func tokenPrec(t lexer.TokenType) precedence {
	switch t {
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
	case lexer.TOKEN_DOT, lexer.TOKEN_LPAREN, lexer.TOKEN_LBRACKET:
		return precCall
	}
	return precNone
}

func (p *Parser) parseExpr() Expr {
	return p.parseExprPrec(precNone)
}

func (p *Parser) parseExprPrec(minPrec precedence) Expr {
	left := p.parseUnary()

	for {
		prec := tokenPrec(p.peek().Type)
		if prec <= minPrec {
			break
		}

		tok := p.advance()
		switch tok.Type {
		case lexer.TOKEN_DOT:
			// Allow keyword 'new' as a field name (e.g. Dog.new(...))
			var field string
			if p.peek().Type == lexer.TOKEN_NEW {
				field = p.advance().Literal
			} else {
				field = p.expect(lexer.TOKEN_IDENT).Literal
			}
			sel := &SelectorExpr{Object: left, Field: field}
			// Check for call: obj.method(...)
			if p.check(lexer.TOKEN_LPAREN) {
				left = p.finishCall(sel)
			} else {
				left = sel
			}
		case lexer.TOKEN_LPAREN:
			left = p.finishCallArgs(left)
		case lexer.TOKEN_LBRACKET:
			idx := p.parseExpr()
			p.expect(lexer.TOKEN_RBRACKET)
			left = &IndexExpr{Object: left, Index: idx}
		default:
			right := p.parseExprPrec(prec)
			left = &BinaryExpr{Left: left, Op: tok.Literal, Right: right}
		}
	}
	return left
}

// finishCall is called when '(' has NOT been consumed yet (e.g. from DOT case).
func (p *Parser) finishCall(callee Expr) Expr {
	// Check for send/receive sugar on SelectorExpr
	if sel, ok := callee.(*SelectorExpr); ok {
		switch sel.Field {
		case "send":
			p.expect(lexer.TOKEN_LPAREN)
			val := p.parseExpr()
			p.expect(lexer.TOKEN_RPAREN)
			return &SendExpr{Chan: sel.Object, Value: val}
		case "receive":
			p.expect(lexer.TOKEN_LPAREN)
			p.expect(lexer.TOKEN_RPAREN)
			return &ReceiveExpr{Chan: sel.Object}
		}
	}
	// Consume '(' then parse args
	p.expect(lexer.TOKEN_LPAREN)
	return p.finishCallArgsNoLParen(callee)
}

// finishCallArgs is called when '(' has already been consumed (Pratt loop TOKEN_LPAREN case).
func (p *Parser) finishCallArgs(callee Expr) Expr {
	return p.finishCallArgsNoLParen(callee)
}

func (p *Parser) finishCallArgsNoLParen(callee Expr) Expr {
	var args []Expr
	if !p.check(lexer.TOKEN_RPAREN) {
		args = append(args, p.parseExpr())
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			args = append(args, p.parseExpr())
		}
	}
	p.expect(lexer.TOKEN_RPAREN)
	return &CallExpr{Callee: callee, Args: args}
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
//	() =>         → peek(1)==RPAREN, peek(2)==FAT_ARROW
//	(name: Type)  → peek(1)==IDENT,  peek(2)==COLON
func (p *Parser) isLambdaStart() bool {
	ahead1 := p.peekAt(1)
	switch ahead1.Type {
	case lexer.TOKEN_RPAREN:
		// () => or (): ReturnType =>
		next := p.peekAt(2).Type
		return next == lexer.TOKEN_FAT_ARROW || next == lexer.TOKEN_COLON
	case lexer.TOKEN_IDENT:
		return p.peekAt(2).Type == lexer.TOKEN_COLON
	}
	return false
}

func (p *Parser) parseLambda() *LambdaExpr {
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

	var retType TypeExpr
	if p.check(lexer.TOKEN_COLON) {
		p.advance()
		retType = p.parseType()
	}

	p.expect(lexer.TOKEN_FAT_ARROW)

	if p.check(lexer.TOKEN_LBRACE) {
		body := p.parseBlock()
		return &LambdaExpr{Params: params, ReturnType: retType, Body: body}
	}
	expr := p.parseExpr()
	return &LambdaExpr{Params: params, ReturnType: retType, Expr: expr}
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
	case lexer.TOKEN_LBRACKET:
		return p.parseListLit()
	case lexer.TOKEN_LBRACE:
		return p.parseMapLit()
	case lexer.TOKEN_IDENT:
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
		return p.parseVarStmt()
	case lexer.TOKEN_RETURN: // parseVarStmt now returns Stmt (VarStmt or TupleVarStmt)
		return p.parseReturnStmt()
	case lexer.TOKEN_IF:
		return p.parseIfStmt()
	case lexer.TOKEN_FOR:
		return p.parseForStmt()
	case lexer.TOKEN_WHILE:
		return p.parseWhileStmt()
	case lexer.TOKEN_GO:
		return p.parseGoStmt()
	case lexer.TOKEN_TRY:
		return p.parseTryStmt()
	case lexer.TOKEN_THROW:
		return p.parseThrowStmt()
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
	case lexer.TOKEN_LBRACE:
		return p.parseBlock()
	}

	// Expression statement or assignment
	return p.parseExprOrAssignStmt()
}

func (p *Parser) parseVarStmt() Stmt {
	p.expect(lexer.TOKEN_VAR)
	// Detect tuple destructuring: var (a, b) = expr
	if p.check(lexer.TOKEN_LPAREN) {
		p.advance() // (
		var names []string
		names = append(names, p.expect(lexer.TOKEN_IDENT).Literal)
		for p.check(lexer.TOKEN_COMMA) {
			p.advance()
			names = append(names, p.expect(lexer.TOKEN_IDENT).Literal)
		}
		p.expect(lexer.TOKEN_RPAREN)
		p.expect(lexer.TOKEN_ASSIGN)
		val := p.parseExpr()
		return &TupleVarStmt{Names: names, Value: val}
	}
	name := p.expect(lexer.TOKEN_IDENT).Literal
	var typ TypeExpr
	if p.check(lexer.TOKEN_COLON) {
		p.advance()
		typ = p.parseType()
	}
	var val Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		val = p.parseExpr()
	}
	return &VarStmt{Name: name, Type: typ, Value: val}
}

func (p *Parser) parseReturnStmt() *ReturnStmt {
	p.expect(lexer.TOKEN_RETURN)
	if p.check(lexer.TOKEN_RBRACE) || p.check(lexer.TOKEN_SEMICOLON) || p.check(lexer.TOKEN_EOF) {
		return &ReturnStmt{}
	}
	return &ReturnStmt{Value: p.parseExpr()}
}

func (p *Parser) parseIfStmt() *IfStmt {
	p.expect(lexer.TOKEN_IF)
	p.expect(lexer.TOKEN_LPAREN)
	cond := p.parseExpr()
	p.expect(lexer.TOKEN_RPAREN)
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
	return &IfStmt{Cond: cond, Then: then, ElseStmt: elseStmt}
}

func (p *Parser) parseForStmt() Stmt {
	p.expect(lexer.TOKEN_FOR)

	// Detect: for item in expr { }
	// We look ahead: IDENT "in" expr
	if p.check(lexer.TOKEN_IDENT) && p.peekAt(1).Literal == "in" {
		item := p.advance().Literal // consume ident
		p.advance()                 // consume "in"
		rangeExpr := p.parseExpr()
		body := p.parseBlock()
		return &ForStmt{IsRange: true, Item: item, Range: rangeExpr, Body: body}
	}

	// C-style: for (init; cond; post) { }
	p.expect(lexer.TOKEN_LPAREN)
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
	if !p.check(lexer.TOKEN_RPAREN) {
		post = p.parseVarOrAssign()
	}
	p.expect(lexer.TOKEN_RPAREN)
	body := p.parseBlock()
	return &ForStmt{Init: init, Cond: cond, Post: post, Body: body}
}

// parseVarOrAssign parses a var decl or assignment for use in for-init/post.
func (p *Parser) parseVarOrAssign() Stmt {
	if p.check(lexer.TOKEN_VAR) {
		return p.parseVarStmt()
	}
	return p.parseExprOrAssignStmt()
}

func (p *Parser) parseWhileStmt() *WhileStmt {
	p.expect(lexer.TOKEN_WHILE)
	p.expect(lexer.TOKEN_LPAREN)
	cond := p.parseExpr()
	p.expect(lexer.TOKEN_RPAREN)
	body := p.parseBlock()
	return &WhileStmt{Cond: cond, Body: body}
}

func (p *Parser) parseGoStmt() *GoStmt {
	p.expect(lexer.TOKEN_GO)
	body := p.parseBlock()
	return &GoStmt{Body: body}
}

func (p *Parser) parseTryStmt() *TryStmt {
	p.expect(lexer.TOKEN_TRY)
	body := p.parseBlock()
	p.expect(lexer.TOKEN_CATCH)
	p.expect(lexer.TOKEN_LPAREN)
	errVar := p.expect(lexer.TOKEN_IDENT).Literal
	p.expect(lexer.TOKEN_RPAREN)
	catchBody := p.parseBlock()
	return &TryStmt{Body: body, ErrVar: errVar, CatchBody: catchBody}
}

func (p *Parser) parseThrowStmt() *ThrowStmt {
	p.expect(lexer.TOKEN_THROW)
	val := p.parseExpr()
	return &ThrowStmt{Value: val}
}

func (p *Parser) parsePrintStmt() *PrintStmt {
	p.expect(lexer.TOKEN_PRINT)
	p.expect(lexer.TOKEN_LPAREN)
	val := p.parseExpr()
	p.expect(lexer.TOKEN_RPAREN)
	return &PrintStmt{Value: val}
}

func (p *Parser) parseMatchStmt() *MatchStmt {
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
		p.expect(lexer.TOKEN_FAT_ARROW)
		body := p.parseBlock()
		cases = append(cases, &MatchCase{Pattern: pattern, Body: body})
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &MatchStmt{Subject: subject, Cases: cases}
}

func (p *Parser) parseExprOrAssignStmt() Stmt {
	expr := p.parseExpr()

	// Check for assignment operators
	tok := p.peek()
	switch tok.Type {
	case lexer.TOKEN_ASSIGN, lexer.TOKEN_PLUS_EQ, lexer.TOKEN_MINUS_EQ,
		lexer.TOKEN_STAR_EQ, lexer.TOKEN_SLASH_EQ:
		p.advance()
		val := p.parseExpr()
		return &AssignStmt{Target: expr, Op: tok.Literal, Value: val}
	}

	return &ExprStmt{Expr: expr}
}

// --- Declarations ------------------------------------------------------------

func (p *Parser) parseFieldDecl() *FieldDecl {
	p.expect(lexer.TOKEN_VAR)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.expect(lexer.TOKEN_COLON)
	typ := p.parseType()
	var def Expr
	if p.check(lexer.TOKEN_ASSIGN) {
		p.advance()
		def = p.parseExpr()
	}
	return &FieldDecl{Name: name, Type: typ, Default: def}
}

func (p *Parser) parseParam() *ParamDecl {
	name := p.expect(lexer.TOKEN_IDENT).Literal
	p.expect(lexer.TOKEN_COLON)
	typ := p.parseType()
	return &ParamDecl{Name: name, Type: typ}
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
	p.expect(lexer.TOKEN_CONSTRUCT)
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
	p.expect(lexer.TOKEN_FN)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	params := p.parseParamList()
	var retType TypeExpr
	if p.check(lexer.TOKEN_COLON) {
		p.advance()
		retType = p.parseType()
	}
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
	p.expect(lexer.TOKEN_FN)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	params := p.parseParamList()
	var retType TypeExpr
	if p.check(lexer.TOKEN_COLON) {
		p.advance()
		retType = p.parseType()
	}
	return &MethodSig{Name: name, IsPub: isPub, Params: params, ReturnType: retType}
}

func (p *Parser) parseClassDecl() *ClassDecl {
	p.expect(lexer.TOKEN_CLASS)
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
		tok := p.peek()
		switch {
		case tok.Type == lexer.TOKEN_VAR:
			fields = append(fields, p.parseFieldDecl())
		case tok.Type == lexer.TOKEN_CONSTRUCT:
			ctor = p.parseCtorDecl()
		case tok.Type == lexer.TOKEN_PUB || tok.Type == lexer.TOKEN_STATIC || tok.Type == lexer.TOKEN_FN:
			methods = append(methods, p.parseMethodDecl())
		default:
			p.errorf("unexpected token %s in class body", tok.Type)
			p.advance()
		}
		p.skipSemis()
	}
	p.expect(lexer.TOKEN_RBRACE)
	return &ClassDecl{Name: name, TypeParams: typeParams, Parents: parents, Fields: fields, Ctor: ctor, Methods: methods}
}

func (p *Parser) parseInterfaceDecl() *InterfaceDecl {
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
	return &InterfaceDecl{Name: name, Methods: methods}
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

func (p *Parser) parseFnDecl(isPub bool) *FnDecl {
	p.expect(lexer.TOKEN_FN)
	name := p.expect(lexer.TOKEN_IDENT).Literal
	typeParams := p.parseTypeParams()
	params := p.parseParamList()
	var retType TypeExpr
	if p.check(lexer.TOKEN_COLON) {
		p.advance()
		retType = p.parseType()
	}
	body := p.parseBlock()
	return &FnDecl{Name: name, IsPub: isPub, TypeParams: typeParams, Params: params, ReturnType: retType, Body: body}
}

func (p *Parser) parseEnumDecl() *EnumDecl {
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
	return &EnumDecl{Name: name, Variants: variants}
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

func (p *Parser) parsePackageDecl() *PackageDecl {
	p.expect(lexer.TOKEN_PACKAGE)
	path := p.expect(lexer.TOKEN_STRING_LIT).Literal
	return &PackageDecl{Path: path}
}

// --- Program -----------------------------------------------------------------

// Parse parses the full program and returns the AST.
func (p *Parser) Parse() *Program {
	prog := &Program{}
	p.skipSemis()
	// Optional package declaration (must be first)
	if p.check(lexer.TOKEN_PACKAGE) {
		prog.Package = p.parsePackageDecl()
		p.skipSemis()
	}
	for !p.check(lexer.TOKEN_EOF) {
		tok := p.peek()
		switch tok.Type {
		case lexer.TOKEN_IMPORT:
			prog.Imports = append(prog.Imports, p.parseImportDecl())
		case lexer.TOKEN_ENUM:
			prog.Decls = append(prog.Decls, p.parseEnumDecl())
		case lexer.TOKEN_CLASS:
			prog.Decls = append(prog.Decls, p.parseClassDecl())
		case lexer.TOKEN_INTERFACE:
			prog.Decls = append(prog.Decls, p.parseInterfaceDecl())
		case lexer.TOKEN_PUB:
			p.advance()
			if p.check(lexer.TOKEN_FN) {
				prog.Decls = append(prog.Decls, p.parseFnDecl(true))
			} else {
				p.errorf("expected fn after pub")
			}
		case lexer.TOKEN_FN:
			prog.Decls = append(prog.Decls, p.parseFnDecl(false))
		default:
			p.errorf("unexpected top-level token %s (%q)", tok.Type, tok.Literal)
			p.advance()
		}
		p.skipSemis()
	}
	return prog
}

// ErrorString returns all parser errors as a single string.
func (p *Parser) ErrorString() string {
	return strings.Join(p.Errors, "\n")
}
