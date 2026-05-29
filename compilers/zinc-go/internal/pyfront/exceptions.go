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

import (
	"fmt"

	"zinc-go/internal/parser"
)

// parseRaise lowers `raise X("msg")` to `panic("msg")` and `raise expr` to
// `panic(expr)`. Exception identity/type is not modelled — the message is the
// payload, which matches `print(e)` (CPython prints the message). Bare
// `raise` (re-raise) is not supported yet.
func (p *Parser) parseRaise() parser.Stmt {
	line := p.advance().Line // 'raise'
	if p.cur().Kind == TNewline || p.cur().Kind == TEOF {
		// Bare `raise` re-raises the current exception. Emit a placeholder
		// arg that clauseBody rewrites to the handler's recovered value.
		p.endSimple()
		return &parser.ExprStmt{Line: line, Expr: callIdent("panic", &parser.Ident{Name: rtReraise})}
	}
	expr := p.parseExpr()
	p.endSimple()

	// raise Type(args) → panic(zincpyExc{Type:"Type", Msg: args[0]}). Other
	// forms (re-raising a bound exception variable) → panic(expr).
	if call, ok := expr.(*parser.CallExpr); ok {
		if id, ok := call.Callee.(*parser.Ident); ok {
			var msg parser.Expr = &parser.StringLit{}
			if len(call.Args) >= 1 {
				msg = call.Args[0]
			}
			exc := &parser.StructLit{
				Type: &parser.Ident{Name: "zincpyExc"},
				Fields: []*parser.StructFieldInit{
					{Name: "Type", Value: &parser.StringLit{Value: id.Name}},
					{Name: "Msg", Value: msg},
				},
			}
			return &parser.ExprStmt{Line: line, Expr: callIdent("panic", exc)}
		}
	}
	return &parser.ExprStmt{Line: line, Expr: callIdent("panic", expr)}
}

// exceptClause is one `except [Type(s)] [as name]:` arm.
type exceptClause struct {
	types []string // matched exception type names; empty = bare `except:`
	name  string   // `as name`, or ""
	body  *parser.BlockStmt
}

// parseTry lowers try/except/finally to an immediately-invoked closure using
// Go's defer/recover:
//
//	func() {
//	    defer func() { FINALLY }()              // registered first → runs last
//	    defer func() {                          // runs first
//	        _exc := recover()
//	        if _exc != nil {
//	            e := _exc                        // when `as e`
//	            HANDLER
//	        }
//	    }()
//	    BODY
//	}()
//
// Limits (documented): exception type is ignored (catch-all, single except);
// `return`/`break`/`continue` inside the body don't escape the closure; a
// variable first assigned inside the body isn't visible afterward (pre-declare
// it before the try). `as e` binds the recovered value as interface{} — use it
// via print/str, not a typed assignment.
func (p *Parser) parseTry() parser.Stmt {
	line := p.advance().Line // 'try'
	body := p.parseBlock()

	var clauses []exceptClause
	for {
		p.skipNewlines()
		if !p.isKw("except") {
			break
		}
		p.advance()
		var c exceptClause
		if !p.isOp(":") { // `except Type [as name]` / `except (A, B) [as name]`
			c.types = typeNamesOf(p.parseExpr())
			if p.isKw("as") {
				p.advance()
				c.name = p.expectKind(TName).Value
			}
		}
		c.body = p.parseBlock()
		clauses = append(clauses, c)
	}

	// `else:` runs when the body completes without an exception. Appending it
	// to the body achieves that: a panic in the body skips straight to recover,
	// so else only runs on the no-exception path.
	p.skipNewlines()
	if p.isKw("else") {
		p.advance()
		elseB := p.parseBlock()
		body.Stmts = append(body.Stmts, elseB.Stmts...)
	}

	var finallyB *parser.BlockStmt
	p.skipNewlines()
	if p.isKw("finally") {
		p.advance()
		finallyB = p.parseBlock()
	}

	// Limits: break/continue can't escape the try closure (loop-label work);
	// return in finally can't propagate. Reject rather than miscompile.
	if escapingBreakContinue(body) || anyClause(clauses, escapingBreakContinue) {
		p.errf(Token{Line: line}, "break/continue that escapes a try body is not yet supported")
	}
	if finallyB != nil && returnInBlock(finallyB) {
		p.errf(Token{Line: line}, "return inside a finally block is not yet supported")
	}

	// A return inside body/handlers must propagate out of the recover closure
	// to the enclosing function — use the return-propagation lowering.
	if returnInBlock(body) || anyClause(clauses, returnInBlock) {
		return p.buildTryWithReturn(line, body, clauses, finallyB)
	}
	return &parser.ExprStmt{Line: line, Expr: p.buildTry(body, clauses, finallyB)}
}

func anyClause(clauses []exceptClause, f func(*parser.BlockStmt) bool) bool {
	for _, c := range clauses {
		if f(c.body) {
			return true
		}
	}
	return false
}

// typeNamesOf extracts exception type names from an except clause's type
// expression: a bare Ident → one name; a parenthesized tuple → several.
func typeNamesOf(e parser.Expr) []string {
	switch t := e.(type) {
	case *parser.Ident:
		return []string{t.Name}
	case *parser.TupleLit:
		return identNames(t.Elements)
	case *parser.CallExpr:
		// A parenthesized tuple of types `(A, B)` is lowered by parseAtom to a
		// zincpyNewTuple(A, B) call; recover the type names from its args.
		if id, ok := t.Callee.(*parser.Ident); ok && id.Name == "zincpyNewTuple" {
			return identNames(t.Args)
		}
	}
	return nil
}

func identNames(exprs []parser.Expr) []string {
	var names []string
	for _, el := range exprs {
		if id, ok := el.(*parser.Ident); ok {
			names = append(names, id.Name)
		}
	}
	return names
}

// buildTryWithReturn wraps the try closure so a `return` inside it propagates
// to the enclosing function. The enclosing scope gets a result slot `_rv` and
// a flag `_ret`; each `return X` in the body/handlers is rewritten to
// `_rv = X; _ret = true; return` (which leaves the closure), and after the
// closure runs `if _ret { return _rv }` re-emits the return.
func (p *Parser) buildTryWithReturn(line int, body *parser.BlockStmt, clauses []exceptClause, finallyB *parser.BlockStmt) parser.Stmt {
	rt := p.currentFnRet
	if rt == nil {
		p.errf(Token{Line: line}, "a function with a `return` inside a try needs a return type annotation")
		return &parser.ExprStmt{Line: line, Expr: p.buildTry(body, clauses, finallyB)}
	}
	if _, isTuple := rt.(*parser.TupleType); isTuple {
		p.errf(Token{Line: line}, "return inside a try is not yet supported for multiple-return (tuple) functions")
		return &parser.ExprStmt{Line: line, Expr: p.buildTry(body, clauses, finallyB)}
	}

	id := p.tmpCount
	p.tmpCount++
	rv := fmt.Sprintf("_rv%d", id)
	ret := fmt.Sprintf("_ret%d", id)

	p.rewriteReturns(body, rv, ret)
	for _, c := range clauses {
		p.rewriteReturns(c.body, rv, ret)
	}

	iife := p.buildTry(body, clauses, finallyB)
	stmts := []parser.Stmt{
		&parser.VarStmt{Line: line, Name: rv, Type: rt},
		&parser.VarStmt{Line: line, Name: ret, Value: &parser.BoolLit{}},
		&parser.ExprStmt{Line: line, Expr: iife},
		&parser.IfStmt{Line: line, Cond: &parser.Ident{Name: ret},
			Then: &parser.BlockStmt{Stmts: []parser.Stmt{
				&parser.ReturnStmt{Line: line, Value: &parser.Ident{Name: rv}},
			}}},
	}
	return &parser.BlockStmt{Stmts: stmts}
}

// rewriteReturns replaces every `return X` in a block (descending into
// if/for/while, not nested functions) with `_rv = X; _ret = true; return`.
func (p *Parser) rewriteReturns(b *parser.BlockStmt, rv, ret string) {
	if b == nil {
		return
	}
	for i, s := range b.Stmts {
		b.Stmts[i] = p.rewriteReturnStmt(s, rv, ret)
	}
}

func (p *Parser) rewriteReturnStmt(s parser.Stmt, rv, ret string) parser.Stmt {
	switch st := s.(type) {
	case *parser.ReturnStmt:
		seq := []parser.Stmt{}
		if st.Value != nil {
			seq = append(seq, &parser.AssignStmt{Target: &parser.Ident{Name: rv}, Op: "=", Value: st.Value})
		}
		seq = append(seq,
			&parser.AssignStmt{Target: &parser.Ident{Name: ret}, Op: "=", Value: &parser.BoolLit{Value: true}},
			&parser.ReturnStmt{}) // bare return — leaves the closure
		return &parser.BlockStmt{Stmts: seq}
	case *parser.IfStmt:
		p.rewriteReturns(st.Then, rv, ret)
		if st.ElseStmt != nil {
			st.ElseStmt = p.rewriteReturnStmt(st.ElseStmt, rv, ret)
		}
		return st
	case *parser.ForStmt:
		p.rewriteReturns(st.Body, rv, ret)
		return st
	case *parser.WhileStmt:
		p.rewriteReturns(st.Body, rv, ret)
		return st
	case *parser.BlockStmt:
		p.rewriteReturns(st, rv, ret)
		return st
	}
	return s
}

// returnInBlock reports whether a block contains a `return` (descending into
// if/for/while, but not nested functions/lambdas).
func returnInBlock(b *parser.BlockStmt) bool {
	if b == nil {
		return false
	}
	for _, s := range b.Stmts {
		switch st := s.(type) {
		case *parser.ReturnStmt:
			return true
		case *parser.IfStmt:
			if returnInBlock(st.Then) || returnInBlock(elseBlock(st.ElseStmt)) {
				return true
			}
		case *parser.ForStmt:
			if returnInBlock(st.Body) {
				return true
			}
		case *parser.WhileStmt:
			if returnInBlock(st.Body) {
				return true
			}
		}
	}
	return false
}

// escapingBreakContinue reports whether a block has a break/continue that
// would escape the try (i.e. not bound to a loop nested inside the try).
func escapingBreakContinue(b *parser.BlockStmt) bool {
	if b == nil {
		return false
	}
	for _, s := range b.Stmts {
		switch st := s.(type) {
		case *parser.BreakStmt, *parser.ContinueStmt:
			return true
		case *parser.IfStmt:
			if escapingBreakContinue(st.Then) || escapingBreakContinue(elseBlock(st.ElseStmt)) {
				return true
			}
			// nested loops (For/While) are intentionally NOT descended: their
			// own break/continue stays bound to them.
		}
	}
	return false
}

// elseBlock normalizes an IfStmt.ElseStmt (which may be a *BlockStmt or an
// *IfStmt for elif) into a block for scanning.
func elseBlock(s parser.Stmt) *parser.BlockStmt {
	switch e := s.(type) {
	case *parser.BlockStmt:
		return e
	case *parser.IfStmt:
		return &parser.BlockStmt{Stmts: []parser.Stmt{e}}
	}
	return nil
}

// buildTry assembles the defer/recover closure: a deferred finally (if any),
// a deferred recover that dispatches the except clauses, then the body.
func (p *Parser) buildTry(body *parser.BlockStmt, clauses []exceptClause, finallyB *parser.BlockStmt) parser.Expr {
	var stmts []parser.Stmt

	// defer finally first so it runs after the recover handler (defers are LIFO).
	if finallyB != nil {
		stmts = append(stmts, deferClosure(finallyB))
	}

	if len(clauses) > 0 {
		excVar := fmt.Sprintf("_exc%d", p.tmpCount)
		p.tmpCount++
		recoverBody := &parser.BlockStmt{Stmts: []parser.Stmt{
			&parser.VarStmt{Name: excVar, Value: callIdent("recover")},
			&parser.IfStmt{
				Cond: &parser.BinaryExpr{Left: &parser.Ident{Name: excVar}, Op: "!=", Right: &parser.NullLit{}},
				Then: &parser.BlockStmt{Stmts: []parser.Stmt{buildMatchChain(excVar, clauses)}},
			},
		}}
		stmts = append(stmts, deferClosure(recoverBody))
	}

	stmts = append(stmts, body.Stmts...)
	return &parser.CallExpr{Callee: &parser.LambdaExpr{Body: &parser.BlockStmt{Stmts: stmts}}}
}

// buildMatchChain builds the except dispatch as nested if/else over
// zincpyMatch(_exc, type), binding `as` names, with a trailing re-raise
// (panic(_exc)) when no bare `except:` clause is present (Python re-raises an
// unmatched exception).
func buildMatchChain(excVar string, clauses []exceptClause) parser.Stmt {
	var acc parser.Stmt = &parser.BlockStmt{Stmts: []parser.Stmt{
		&parser.ExprStmt{Expr: callIdent("panic", &parser.Ident{Name: excVar})},
	}}
	for i := len(clauses) - 1; i >= 0; i-- {
		c := clauses[i]
		cbody := clauseBody(c, excVar)
		if len(c.types) == 0 { // bare except — catches everything
			acc = cbody
			continue
		}
		acc = &parser.IfStmt{Cond: matchCond(excVar, c.types), Then: cbody, ElseStmt: acc}
	}
	return acc
}

func clauseBody(c exceptClause, excVar string) *parser.BlockStmt {
	rewriteReraise(c.body, excVar) // bare `raise` → panic(excVar)
	stmts := c.body.Stmts
	if c.name != "" {
		stmts = append([]parser.Stmt{
			&parser.VarStmt{Name: c.name, Value: &parser.Ident{Name: excVar}},
		}, stmts...)
	}
	return &parser.BlockStmt{Stmts: stmts}
}

// rewriteReraise replaces the bare-raise placeholder ident with the handler's
// recovered-exception variable, descending into nested blocks.
func rewriteReraise(b *parser.BlockStmt, excVar string) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		switch st := s.(type) {
		case *parser.ExprStmt:
			if call, ok := st.Expr.(*parser.CallExpr); ok && len(call.Args) == 1 {
				if id, ok := call.Args[0].(*parser.Ident); ok && id.Name == rtReraise {
					id.Name = excVar
				}
			}
		case *parser.IfStmt:
			rewriteReraise(st.Then, excVar)
			rewriteReraise(elseBlock(st.ElseStmt), excVar)
		case *parser.ForStmt:
			rewriteReraise(st.Body, excVar)
		case *parser.WhileStmt:
			rewriteReraise(st.Body, excVar)
		case *parser.BlockStmt:
			rewriteReraise(st, excVar)
		}
	}
}

func matchCond(excVar string, types []string) parser.Expr {
	cond := matchCall(excVar, types[0])
	for _, t := range types[1:] {
		cond = &parser.BinaryExpr{Left: cond, Op: "||", Right: matchCall(excVar, t)}
	}
	return cond
}

func matchCall(excVar, typ string) parser.Expr {
	return &parser.CallExpr{
		Callee: &parser.Ident{Name: "zincpyMatch"},
		Args:   []parser.Expr{&parser.Ident{Name: excVar}, &parser.StringLit{Value: typ}},
	}
}

// deferClosure builds `defer func() { body }()`.
func deferClosure(body *parser.BlockStmt) parser.Stmt {
	return &parser.DeferStmt{Expr: &parser.CallExpr{Callee: &parser.LambdaExpr{Body: body}}}
}

// ensureTrailingReturn appends a zero-value `return` to a non-void function
// body that doesn't already end in one. Needed because lowerings like
// return-in-try (and Python functions ending in if/else or loops) leave the
// final return implicit, which Go/the typechecker reject as "not all paths
// return". The trailing return is unreachable when the body always returns.
func ensureTrailingReturn(body *parser.BlockStmt, ret parser.TypeExpr) {
	if ret == nil || body == nil {
		return
	}
	if n := len(body.Stmts); n > 0 {
		if _, ok := body.Stmts[n-1].(*parser.ReturnStmt); ok {
			return
		}
	}
	body.Stmts = append(body.Stmts, &parser.ReturnStmt{Value: &parser.DefaultExpr{Type: ret}})
}
