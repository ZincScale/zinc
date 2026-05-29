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

// parseListComprehension lowers `[output for x in iter if cond]` to an
// immediately-invoked closure that accumulates into a slice:
//
//	func() []T {
//	    _comp := []T{}
//	    for x := range iter {
//	        if cond {            // omitted when there is no filter
//	            _comp = append(_comp, output)
//	        }
//	    }
//	    return _comp
//	}()
//
// `output` has already been parsed (it precedes `for` in Python syntax); the
// cursor is on the `for` keyword. The element type T is inferred by typing
// `output` with the loop variable bound to the iterable's element type.
func (p *Parser) parseListComprehension(output parser.Expr) parser.Expr {
	p.advance() // 'for'
	loopVar := p.expectKind(TName).Value
	if p.isKw("in") {
		p.advance()
	} else {
		p.errf(p.cur(), "expected 'in' in comprehension")
	}
	iter := p.parseOr() // parseOr (not parseExpr): don't let a ternary eat the filter `if`
	var cond parser.Expr
	if p.isKw("if") {
		p.advance()
		cond = p.parseExpr()
	}
	p.expectOp("]")

	// Infer the result element type: bind the loop var to the iterable's
	// element type, then type the output expression.
	p.pushScope()
	p.declare(loopVar, p.elemTypeOf(iter))
	resultElem := p.typeOf(output)
	p.popScope()

	listType := &parser.GenericType{Name: "List"}
	if resultElem != tUnknown {
		listType.TypeArgs = []parser.TypeExpr{zincTypeForPy(resultElem)}
	}

	// range(...) iterables lower to a numeric range, like a plain for-loop.
	rangeExpr := iter
	if rng, ok := asRange(iter); ok {
		rangeExpr = rng
	}

	acc := fmt.Sprintf("_comp%d", p.tmpCount)
	p.tmpCount++

	appendStmt := &parser.AssignStmt{
		Target: &parser.Ident{Name: acc}, Op: "=",
		Value: callIdent("append", &parser.Ident{Name: acc}, output),
	}
	var loopBody parser.Stmt = appendStmt
	if cond != nil {
		loopBody = &parser.IfStmt{Cond: cond, Then: &parser.BlockStmt{Stmts: []parser.Stmt{appendStmt}}}
	}

	lambda := &parser.LambdaExpr{
		ReturnType: listType,
		Body: &parser.BlockStmt{Stmts: []parser.Stmt{
			&parser.VarStmt{Name: acc, Value: &parser.ListLit{ExplicitType: listType}},
			&parser.ForStmt{IsRange: true, Item: loopVar, Range: rangeExpr,
				Body: &parser.BlockStmt{Stmts: []parser.Stmt{loopBody}}},
			&parser.ReturnStmt{Value: &parser.Ident{Name: acc}},
		}},
	}
	return &parser.CallExpr{Callee: lambda}
}

// parseCompClause parses the `for a[, b] in iter [if cond]}` tail shared by set
// and dict comprehensions (cursor on `for`; consumes the closing `}`).
func (p *Parser) parseCompClause() (targets []string, iter, cond parser.Expr) {
	p.advance() // 'for'
	targets = []string{p.expectKind(TName).Value}
	for p.acceptOp(",") {
		if p.isKw("in") {
			break
		}
		targets = append(targets, p.expectKind(TName).Value)
	}
	if p.isKw("in") {
		p.advance()
	} else {
		p.errf(p.cur(), "expected 'in' in comprehension")
	}
	iter = p.parseOr() // parseOr: don't let a ternary eat the filter `if`
	if p.isKw("if") {
		p.advance()
		cond = p.parseExpr()
	}
	p.expectOp("}")
	if rng, ok := asRange(iter); ok {
		iter = rng
	}
	return targets, iter, cond
}

// compForStmt builds the comprehension's for-loop. A single target iterates
// directly; multiple targets iterate via zincpyIter and unpack each element
// with zincpyGetItem (for `... for k, v in d.items()`).
func (p *Parser) compForStmt(targets []string, iter parser.Expr, loopBody parser.Stmt) *parser.ForStmt {
	if len(targets) == 1 {
		return &parser.ForStmt{IsRange: true, Item: targets[0], Range: iter,
			Body: &parser.BlockStmt{Stmts: []parser.Stmt{loopBody}}}
	}
	tmp := fmt.Sprintf("_it%d", p.tmpCount)
	p.tmpCount++
	var stmts []parser.Stmt
	for k, t := range targets {
		stmts = append(stmts, &parser.VarStmt{Name: t,
			Value: callIdent("zincpyGetItem", &parser.Ident{Name: tmp}, &parser.IntLit{Value: fmt.Sprintf("%d", k)})})
	}
	stmts = append(stmts, loopBody)
	return &parser.ForStmt{IsRange: true, Item: tmp, Range: callIdent("zincpyIter", iter),
		Body: &parser.BlockStmt{Stmts: stmts}}
}

// declareCompTargets binds the comprehension loop variable(s) for typing the
// output/key/value: a single target gets the iterable's element type, multiple
// targets are dynamic (unpacked at runtime).
func (p *Parser) declareCompTargets(targets []string, iter parser.Expr) {
	if len(targets) == 1 {
		p.declare(targets[0], p.elemTypeOf(iter))
		return
	}
	for _, t := range targets {
		p.declare(t, tDynamic)
	}
}

// parseSetComprehension lowers `{output for x in iter [if cond]}` to an IIFE
// accumulating into a *zincpySet.
func (p *Parser) parseSetComprehension(outStart int) parser.Expr {
	// cursor is on `for`. Parse the clause to learn the loop variable(s)...
	targets, iter, cond := p.parseCompClause()
	endPos := p.pos
	// ...then re-parse the output expression with them bound, so e.g. `v * 2`
	// over a dynamic unpacked var routes through the dynamic operators.
	p.pos = outStart
	p.pushScope()
	p.declareCompTargets(targets, iter)
	output := p.parseExpr()
	p.popScope()
	p.pos = endPos

	acc := fmt.Sprintf("_setc%d", p.tmpCount)
	p.tmpCount++
	add := &parser.ExprStmt{Expr: &parser.CallExpr{
		Callee: &parser.SelectorExpr{Object: &parser.Ident{Name: acc}, Field: "Add"},
		Args:   []parser.Expr{output},
	}}
	var loopBody parser.Stmt = add
	if cond != nil {
		loopBody = &parser.IfStmt{Cond: p.truthyWrap(cond), Then: &parser.BlockStmt{Stmts: []parser.Stmt{add}}}
	}
	lambda := &parser.LambdaExpr{
		ReturnType: &parser.SimpleType{Name: "*zincpySet"},
		Body: &parser.BlockStmt{Stmts: []parser.Stmt{
			&parser.VarStmt{Name: acc, Value: callIdent("zincpyNewSet")},
			p.compForStmt(targets, iter, loopBody),
			&parser.ReturnStmt{Value: &parser.Ident{Name: acc}},
		}},
	}
	iife := &parser.CallExpr{Callee: lambda}
	p.setExprMeta[iife] = true
	return iife
}

// parseDictComprehension lowers `{key: val for x in iter [if cond]}` to an IIFE
// accumulating into a *zincpyDict.
func (p *Parser) parseDictComprehension(outStart int) parser.Expr {
	// cursor is on `for`. Parse the clause for the loop variable(s)...
	targets, iter, cond := p.parseCompClause()
	endPos := p.pos
	// ...then re-parse `key: val` with them bound (so arithmetic on dynamic
	// unpacked vars lowers correctly) and recover the key/value types.
	p.pos = outStart
	p.pushScope()
	p.declareCompTargets(targets, iter)
	key := p.parseExpr()
	p.expectOp(":")
	val := p.parseExpr()
	keyT, valT := p.typeOf(key), p.typeOf(val)
	p.popScope()
	p.pos = endPos

	acc := fmt.Sprintf("_dictc%d", p.tmpCount)
	p.tmpCount++
	set := &parser.ExprStmt{Expr: &parser.CallExpr{
		Callee: &parser.SelectorExpr{Object: &parser.Ident{Name: acc}, Field: "Set"},
		Args:   []parser.Expr{key, val},
	}}
	var loopBody parser.Stmt = set
	if cond != nil {
		loopBody = &parser.IfStmt{Cond: p.truthyWrap(cond), Then: &parser.BlockStmt{Stmts: []parser.Stmt{set}}}
	}
	lambda := &parser.LambdaExpr{
		ReturnType: &parser.SimpleType{Name: "*zincpyDict"},
		Body: &parser.BlockStmt{Stmts: []parser.Stmt{
			&parser.VarStmt{Name: acc, Value: callIdent("zincpyNewDict")},
			p.compForStmt(targets, iter, loopBody),
			&parser.ReturnStmt{Value: &parser.Ident{Name: acc}},
		}},
	}
	iife := &parser.CallExpr{Callee: lambda}
	// Track the dict's key/value types so later d[k] reads assert to the value
	// type.
	p.dictExprMeta[iife] = dictMeta{key: keyT, val: valT}
	return iife
}
