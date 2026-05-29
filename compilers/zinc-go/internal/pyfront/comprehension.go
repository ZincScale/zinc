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
	iter := p.parseExpr()
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
