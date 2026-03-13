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

package codegen_python

import (
	"fmt"
	"strings"

	"zinc/internal/parser"
)

// CollectionStrategy controls how collection method chains are emitted.
type CollectionStrategy int

const (
	// StrategyComprehension emits Python list comprehensions (default).
	StrategyComprehension CollectionStrategy = iota
	// StrategyNumPy emits NumPy vectorized operations for numeric data.
	StrategyNumPy
	// StrategyNumba emits Numba JIT-compiled loops for complex operations.
	StrategyNumba
)

// collectionMethods is the set of recognized LINQ-style collection method names.
var collectionMethods = map[string]bool{
	"Where": true, "Select": true, "SelectMany": true,
	"ForEach": true, "Any": true, "All": true,
	"First": true, "FirstOrDefault": true, "Count": true,
	"Take": true, "Skip": true, "Aggregate": true, "ToList": true,
}

// chainStep represents one operation in a collection method chain.
type chainStep struct {
	method string
	args   []parser.Expr
}

// collectionChain represents a parsed collection method chain.
type collectionChain struct {
	source parser.Expr
	steps  []chainStep
}

// unwrapChain checks if an expression is a collection method chain.
func (g *Generator) unwrapChain(expr parser.Expr) *collectionChain {
	call, ok := expr.(*parser.CallExpr)
	if !ok {
		return nil
	}
	sel, ok := call.Callee.(*parser.SelectorExpr)
	if !ok {
		return nil
	}
	if !collectionMethods[sel.Field] {
		return nil
	}
	step := chainStep{method: sel.Field, args: call.Args}
	if inner := g.unwrapChain(sel.Object); inner != nil {
		inner.steps = append(inner.steps, step)
		return inner
	}
	return &collectionChain{source: sel.Object, steps: []chainStep{step}}
}

// --- Lambda helpers (AST-level substitution) ---------------------------------

// extractLambda pulls a LambdaExpr from an expression node.
func extractLambda(expr parser.Expr) *parser.LambdaExpr {
	if l, ok := expr.(*parser.LambdaExpr); ok {
		return l
	}
	return nil
}

// lambdaParam returns the first param name, or fallback.
func lambdaParam(l *parser.LambdaExpr, fallback string) string {
	if l != nil && len(l.Params) > 0 {
		return l.Params[0].Name
	}
	return fallback
}

// emitExprSubst emits an expression, substituting paramName → replacement.
// This is used by all three strategies for correct variable binding.
func (g *Generator) emitExprSubst(expr parser.Expr, paramName, replacement string) string {
	switch e := expr.(type) {
	case *parser.Ident:
		if e.Name == paramName {
			return replacement
		}
		return e.Name
	case *parser.BinaryExpr:
		left := g.emitExprSubst(e.Left, paramName, replacement)
		right := g.emitExprSubst(e.Right, paramName, replacement)
		op := e.Op
		switch op {
		case "&&":
			op = "and"
		case "||":
			op = "or"
		}
		return fmt.Sprintf("(%s %s %s)", left, op, right)
	case *parser.UnaryExpr:
		operand := g.emitExprSubst(e.Operand, paramName, replacement)
		if e.Op == "!" {
			return fmt.Sprintf("not %s", operand)
		}
		return fmt.Sprintf("(%s%s)", e.Op, operand)
	case *parser.CallExpr:
		callee := g.emitExprSubst(e.Callee, paramName, replacement)
		var args []string
		for _, a := range e.Args {
			args = append(args, g.emitExprSubst(a, paramName, replacement))
		}
		return fmt.Sprintf("%s(%s)", callee, strings.Join(args, ", "))
	case *parser.SelectorExpr:
		obj := g.emitExprSubst(e.Object, paramName, replacement)
		return fmt.Sprintf("%s.%s", obj, e.Field)
	case *parser.IndexExpr:
		obj := g.emitExprSubst(e.Object, paramName, replacement)
		idx := g.emitExprSubst(e.Index, paramName, replacement)
		return fmt.Sprintf("%s[%s]", obj, idx)
	default:
		return g.emitExpr(expr)
	}
}

// emitExprSubst2 emits an expression, substituting two parameter names.
func (g *Generator) emitExprSubst2(expr parser.Expr, name1, repl1, name2, repl2 string) string {
	switch e := expr.(type) {
	case *parser.Ident:
		if e.Name == name1 {
			return repl1
		}
		if e.Name == name2 {
			return repl2
		}
		return e.Name
	case *parser.BinaryExpr:
		left := g.emitExprSubst2(e.Left, name1, repl1, name2, repl2)
		right := g.emitExprSubst2(e.Right, name1, repl1, name2, repl2)
		op := e.Op
		switch op {
		case "&&":
			op = "and"
		case "||":
			op = "or"
		}
		return fmt.Sprintf("(%s %s %s)", left, op, right)
	case *parser.UnaryExpr:
		operand := g.emitExprSubst2(e.Operand, name1, repl1, name2, repl2)
		if e.Op == "!" {
			return fmt.Sprintf("not %s", operand)
		}
		return fmt.Sprintf("(%s%s)", e.Op, operand)
	default:
		return g.emitExpr(expr)
	}
}

// =============================================================================
// Strategy 1: List Comprehensions (default)
// =============================================================================

// emitCollectionChainVar dispatches to the correct strategy.
func (g *Generator) emitCollectionChainVar(varName string, chain *collectionChain) {
	switch g.Strategy {
	case StrategyNumPy:
		g.emitChainNumPy(varName, chain)
	case StrategyNumba:
		g.emitChainNumba(varName, chain)
	default:
		g.emitChainComprehension(varName, chain)
	}
}

// emitCollectionChainStmt emits a collection chain as a statement (ForEach terminal).
func (g *Generator) emitCollectionChainStmt(chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]
	if lastStep.method != "ForEach" {
		return
	}

	lambda := extractLambda(lastStep.args[0])
	if lambda == nil {
		return
	}
	paramName := lambdaParam(lambda, "_x")

	// Build pipeline up to ForEach
	pipe := g.buildComprehensionPipeline(source, chain.steps[:len(chain.steps)-1])

	g.writeln(fmt.Sprintf("for %s in %s:", paramName, pipe))
	g.push()
	if lambda.Expr != nil {
		g.writeln(g.emitExpr(lambda.Expr))
	} else if lambda.Body != nil {
		g.emitBlock(lambda.Body)
	}
	g.pop()
}

// emitChainComprehension emits collection chains as Python list comprehensions.
func (g *Generator) emitChainComprehension(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	switch lastStep.method {
	case "Any":
		pipe := g.buildComprehensionPipeline(source, chain.steps[:len(chain.steps)-1])
		lambda := extractLambda(lastStep.args[0])
		p := lambdaParam(lambda, "_x")
		body := g.emitExprSubst(lambda.Expr, p, p)
		g.writeln(fmt.Sprintf("%s = any(%s for %s in %s)", varName, body, p, pipe))

	case "All":
		pipe := g.buildComprehensionPipeline(source, chain.steps[:len(chain.steps)-1])
		lambda := extractLambda(lastStep.args[0])
		p := lambdaParam(lambda, "_x")
		body := g.emitExprSubst(lambda.Expr, p, p)
		g.writeln(fmt.Sprintf("%s = all(%s for %s in %s)", varName, body, p, pipe))

	case "Count":
		pipe := g.buildComprehensionPipeline(source, chain.steps[:len(chain.steps)-1])
		g.writeln(fmt.Sprintf("%s = len(%s)", varName, pipe))

	case "Aggregate":
		if len(lastStep.args) >= 2 {
			g.neededImports["functools"] = true
			pipe := g.buildComprehensionPipeline(source, chain.steps[:len(chain.steps)-1])
			seed := g.emitExpr(lastStep.args[0])
			reducer := g.emitExpr(lastStep.args[1])
			g.writeln(fmt.Sprintf("%s = functools.reduce(%s, %s, %s)", varName, reducer, pipe, seed))
		}

	case "First":
		pipe := g.buildComprehensionPipeline(source, chain.steps[:len(chain.steps)-1])
		if len(lastStep.args) > 0 {
			lambda := extractLambda(lastStep.args[0])
			p := lambdaParam(lambda, "_x")
			body := g.emitExprSubst(lambda.Expr, p, p)
			g.writeln(fmt.Sprintf("%s = next(%s for %s in %s if %s)", varName, p, p, pipe, body))
		} else {
			g.writeln(fmt.Sprintf("%s = next(iter(%s))", varName, pipe))
		}

	case "FirstOrDefault":
		pipe := g.buildComprehensionPipeline(source, chain.steps[:len(chain.steps)-1])
		if len(lastStep.args) > 0 {
			lambda := extractLambda(lastStep.args[0])
			p := lambdaParam(lambda, "_x")
			body := g.emitExprSubst(lambda.Expr, p, p)
			g.writeln(fmt.Sprintf("%s = next((%s for %s in %s if %s), None)", varName, p, p, pipe, body))
		} else {
			g.writeln(fmt.Sprintf("%s = next(iter(%s), None)", varName, pipe))
		}

	default:
		// List-producing: Where, Select, Take, Skip, ToList
		pipe := g.buildComprehensionPipeline(source, chain.steps)
		g.writeln(fmt.Sprintf("%s = %s", varName, pipe))
	}
}

// buildComprehensionPipeline composes collection steps into nested comprehensions.
func (g *Generator) buildComprehensionPipeline(source string, steps []chainStep) string {
	expr := source
	for _, step := range steps {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := extractLambda(step.args[0])
				p := lambdaParam(lambda, "_x")
				body := g.emitExprSubst(lambda.Expr, p, p)
				expr = fmt.Sprintf("[%s for %s in %s if %s]", p, p, expr, body)
			}
		case "Select":
			if len(step.args) > 0 {
				lambda := extractLambda(step.args[0])
				p := lambdaParam(lambda, "_x")
				body := g.emitExprSubst(lambda.Expr, p, p)
				expr = fmt.Sprintf("[%s for %s in %s]", body, p, expr)
			}
		case "SelectMany":
			if len(step.args) > 0 {
				lambda := extractLambda(step.args[0])
				p := lambdaParam(lambda, "_x")
				body := g.emitExprSubst(lambda.Expr, p, p)
				expr = fmt.Sprintf("[_y for %s in %s for _y in %s]", p, expr, body)
			}
		case "Take":
			if len(step.args) > 0 {
				expr = fmt.Sprintf("%s[:%s]", expr, g.emitExpr(step.args[0]))
			}
		case "Skip":
			if len(step.args) > 0 {
				expr = fmt.Sprintf("%s[%s:]", expr, g.emitExpr(step.args[0]))
			}
		case "ToList":
			expr = fmt.Sprintf("list(%s)", expr)
		}
	}
	return expr
}

// =============================================================================
// Strategy 2: NumPy — vectorized C-speed array operations
// =============================================================================
//
// NumPy replaces loops entirely with vectorized operations on arrays.
// Where → boolean indexing, Select → vectorized arithmetic, Aggregate → np.sum/prod.
//
// Example Zinc:
//   result := nums.Where(x => x > 5).Select(x => x * 2).ToList()
//
// NumPy output:
//   _arr = np.array(nums)
//   _arr = _arr[_arr > 5]
//   _arr = _arr * 2
//   result = _arr.tolist()
//
// No loops at all. Runs at C speed via BLAS/LAPACK.

func (g *Generator) emitChainNumPy(varName string, chain *collectionChain) {
	g.neededImports["numpy"] = true
	source := g.emitExpr(chain.source)

	// Local array variable that we transform step-by-step
	arrVar := fmt.Sprintf("_arr%d", g.tmpCounter)
	g.tmpCounter++
	g.writeln(fmt.Sprintf("%s = np.array(%s)", arrVar, source))

	lastStep := chain.steps[len(chain.steps)-1]

	// Apply all intermediate steps as vectorized ops
	intermediates := chain.steps
	hasTerminal := false
	switch lastStep.method {
	case "Any", "All", "Count", "Aggregate", "First", "FirstOrDefault":
		intermediates = chain.steps[:len(chain.steps)-1]
		hasTerminal = true
	}

	for _, step := range intermediates {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				cond := g.emitNumPySubst(step.args[0], arrVar)
				arrVar = g.nextArr()
				g.writeln(fmt.Sprintf("%s = %s[%s]", arrVar, g.prevArr(arrVar), cond))
			}
		case "Select":
			if len(step.args) > 0 {
				transform := g.emitNumPySubst(step.args[0], arrVar)
				newArr := g.nextArr()
				g.writeln(fmt.Sprintf("%s = %s", newArr, transform))
				arrVar = newArr
			}
		case "Take":
			if len(step.args) > 0 {
				g.writeln(fmt.Sprintf("%s = %s[:%s]", arrVar, arrVar, g.emitExpr(step.args[0])))
			}
		case "Skip":
			if len(step.args) > 0 {
				g.writeln(fmt.Sprintf("%s = %s[%s:]", arrVar, arrVar, g.emitExpr(step.args[0])))
			}
		case "ToList":
			// Applied at the end
		}
	}

	if !hasTerminal {
		// List-producing terminal
		g.writeln(fmt.Sprintf("%s = %s.tolist()", varName, arrVar))
		return
	}

	// Terminal operations
	switch lastStep.method {
	case "Any":
		if len(lastStep.args) > 0 {
			cond := g.emitNumPySubst(lastStep.args[0], arrVar)
			g.writeln(fmt.Sprintf("%s = bool(np.any(%s))", varName, cond))
		}
	case "All":
		if len(lastStep.args) > 0 {
			cond := g.emitNumPySubst(lastStep.args[0], arrVar)
			g.writeln(fmt.Sprintf("%s = bool(np.all(%s))", varName, cond))
		}
	case "Count":
		g.writeln(fmt.Sprintf("%s = len(%s)", varName, arrVar))
	case "Aggregate":
		if len(lastStep.args) >= 2 {
			seed := g.emitExpr(lastStep.args[0])
			lambda := extractLambda(lastStep.args[1])
			if lambda != nil && g.isSimpleAddAggregate(lambda) {
				g.writeln(fmt.Sprintf("%s = int(np.sum(%s)) + %s", varName, arrVar, seed))
			} else if lambda != nil && g.isSimpleMulAggregate(lambda) {
				g.writeln(fmt.Sprintf("%s = int(np.prod(%s)) * %s", varName, arrVar, seed))
			} else {
				// Fallback: functools.reduce on the array
				g.neededImports["functools"] = true
				reducer := g.emitExpr(lastStep.args[1])
				g.writeln(fmt.Sprintf("%s = functools.reduce(%s, %s.tolist(), %s)", varName, reducer, arrVar, seed))
			}
		}
	case "First":
		g.writeln(fmt.Sprintf("%s = int(%s[0])", varName, arrVar))
	case "FirstOrDefault":
		g.writeln(fmt.Sprintf("%s = int(%s[0]) if len(%s) > 0 else None", varName, arrVar, arrVar))
	}
}

// emitNumPySubst substitutes the lambda parameter with the array variable,
// producing vectorized NumPy expressions.
// e.g. x => x > 5 becomes: _arr > 5
// e.g. x => x * 2 becomes: _arr * 2
// e.g. x => x > 5 && x < 10 becomes: (_arr > 5) & (_arr < 10)
func (g *Generator) emitNumPySubst(expr parser.Expr, arrVar string) string {
	lambda := extractLambda(expr)
	if lambda == nil || lambda.Expr == nil {
		return "True"
	}
	return g.emitExprSubstNumPy(lambda.Expr, lambda.Params[0].Name, arrVar)
}

func (g *Generator) emitExprSubstNumPy(expr parser.Expr, paramName, arrVar string) string {
	switch e := expr.(type) {
	case *parser.Ident:
		if e.Name == paramName {
			return arrVar
		}
		return e.Name
	case *parser.BinaryExpr:
		left := g.emitExprSubstNumPy(e.Left, paramName, arrVar)
		right := g.emitExprSubstNumPy(e.Right, paramName, arrVar)
		switch e.Op {
		case "&&":
			return fmt.Sprintf("(%s & %s)", left, right)
		case "||":
			return fmt.Sprintf("(%s | %s)", left, right)
		case "%":
			return fmt.Sprintf("np.mod(%s, %s)", left, right)
		default:
			return fmt.Sprintf("(%s %s %s)", left, op(e.Op), right)
		}
	case *parser.UnaryExpr:
		operand := g.emitExprSubstNumPy(e.Operand, paramName, arrVar)
		if e.Op == "!" {
			return fmt.Sprintf("~(%s)", operand)
		}
		return fmt.Sprintf("(%s%s)", e.Op, operand)
	case *parser.IntLit:
		return e.Value
	case *parser.FloatLit:
		return e.Value
	default:
		return g.emitExpr(expr)
	}
}

// isSimpleAddAggregate checks if a 2-param lambda is: (acc, x) => acc + x
func (g *Generator) isSimpleAddAggregate(l *parser.LambdaExpr) bool {
	bin, ok := l.Expr.(*parser.BinaryExpr)
	if !ok || bin.Op != "+" {
		return false
	}
	lhs, lok := bin.Left.(*parser.Ident)
	rhs, rok := bin.Right.(*parser.Ident)
	return lok && rok && lhs.Name == l.Params[0].Name && rhs.Name == l.Params[1].Name
}

// isSimpleMulAggregate checks if a 2-param lambda is: (acc, x) => acc * x
func (g *Generator) isSimpleMulAggregate(l *parser.LambdaExpr) bool {
	bin, ok := l.Expr.(*parser.BinaryExpr)
	if !ok || bin.Op != "*" {
		return false
	}
	lhs, lok := bin.Left.(*parser.Ident)
	rhs, rok := bin.Right.(*parser.Ident)
	return lok && rok && lhs.Name == l.Params[0].Name && rhs.Name == l.Params[1].Name
}

func op(s string) string { return s }

// Array variable helpers for NumPy pipeline
func (g *Generator) nextArr() string {
	name := fmt.Sprintf("_arr%d", g.tmpCounter)
	g.tmpCounter++
	return name
}

func (g *Generator) prevArr(current string) string {
	// Parse the counter from current name to get previous
	var n int
	fmt.Sscanf(current, "_arr%d", &n)
	if n > 0 {
		return fmt.Sprintf("_arr%d", n-1)
	}
	return current
}

// =============================================================================
// Strategy 3: Numba JIT — compiled loops for complex lambdas
// =============================================================================
//
// Numba JIT-compiles Python loops to machine code via LLVM. First call has
// ~200ms compile overhead, subsequent calls run at C/Fortran speed.
//
// For list-producing chains, uses a two-pass strategy:
//   Pass 1: count matching elements
//   Pass 2: allocate np.empty(count) and fill
// This avoids numba.typed.List which has ~800ms tolist() overhead.
//
// Example Zinc:
//   result := nums.Where(x => x > 5).Select(x => x * x).ToList()
//
// Numba output:
//   @numba.jit(nopython=True)
//   def _chain_0(_src):
//       _n = 0
//       for _x in _src:
//           if _x > 5:
//               _n += 1
//       _result = np.empty(_n, dtype=np.int64)
//       _j = 0
//       for _x in _src:
//           if _x > 5:
//               _result[_j] = _x * _x
//               _j += 1
//       return _result
//   result = _chain_0(np.array(nums)).tolist()

func (g *Generator) emitChainNumba(varName string, chain *collectionChain) {
	g.neededImports["numba"] = true
	g.neededImports["numpy"] = true

	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]
	funcName := fmt.Sprintf("_chain_%d", g.tmpCounter)
	g.tmpCounter++

	// Determine terminal type
	isListProducing := true
	hasTake := false
	switch lastStep.method {
	case "Any", "All", "Count", "First", "FirstOrDefault", "Aggregate":
		isListProducing = false
	}
	for _, step := range chain.steps {
		if step.method == "Take" {
			hasTake = true
		}
	}

	// Emit the JIT function
	g.writeln("@numba.jit(nopython=True)")
	g.writeln(fmt.Sprintf("def %s(_src):", funcName))
	g.push()

	if isListProducing && !hasTake {
		// Two-pass: count then fill (avoids typed.List overhead)
		g.emitNumbaCountPass(chain)
		g.writeln("_result = np.empty(_n, dtype=np.int64)")
		g.writeln("_j = 0")
		g.emitNumbaFillPass(chain, "_result", "_j")
		g.writeln("return _result")
	} else if isListProducing && hasTake {
		// Take chains: use fixed-size array (we know the max size)
		takeLimit := g.findTakeLimit(chain)
		g.writeln(fmt.Sprintf("_result = np.empty(%s, dtype=np.int64)", takeLimit))
		g.writeln("_taken = 0")
		g.emitNumbaTakePass(chain)
		g.writeln("return _result[:_taken]")
	} else {
		// Scalar terminal: single-pass
		g.emitNumbaScalarPass(varName, chain)
	}

	g.pop() // close function

	// Call the JIT function
	if isListProducing {
		g.writeln(fmt.Sprintf("%s = %s(np.array(%s)).tolist()", varName, funcName, source))
	} else {
		g.writeln(fmt.Sprintf("%s = %s(np.array(%s))", varName, funcName, source))
	}
}

// emitNumbaCountPass emits the counting pass for two-pass list-producing chains.
func (g *Generator) emitNumbaCountPass(chain *collectionChain) {
	g.writeln("_n = 0")
	g.writeln("for _x in _src:")
	g.push()

	indents := 0
	for _, step := range chain.steps {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := extractLambda(step.args[0])
				p := lambdaParam(lambda, "_x")
				cond := g.emitExprSubst(lambda.Expr, p, "_x")
				g.writeln(fmt.Sprintf("if %s:", cond))
				g.push()
				indents++
			}
		case "Skip":
			// Skip doesn't affect count — handled in fill pass
		case "Select", "ToList":
			// Don't affect count
		}
	}
	g.writeln("_n += 1")
	for i := 0; i < indents; i++ {
		g.pop()
	}
	g.pop() // close for
}

// emitNumbaFillPass emits the filling pass — allocates and fills np.empty array.
func (g *Generator) emitNumbaFillPass(chain *collectionChain, resultVar, idxVar string) {
	g.writeln("for _x in _src:")
	g.push()

	currentVar := "_x"
	indents := 0

	// Check for Skip
	var skipLimit string
	for _, step := range chain.steps {
		if step.method == "Skip" && len(step.args) > 0 {
			skipLimit = g.emitExpr(step.args[0])
		}
	}
	if skipLimit != "" {
		g.writeln(fmt.Sprintf("_skipped_%s = 0", idxVar))
	}

	for _, step := range chain.steps {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := extractLambda(step.args[0])
				p := lambdaParam(lambda, "_x")
				cond := g.emitExprSubst(lambda.Expr, p, currentVar)
				g.writeln(fmt.Sprintf("if %s:", cond))
				g.push()
				indents++
			}
		case "Select":
			if len(step.args) > 0 {
				lambda := extractLambda(step.args[0])
				p := lambdaParam(lambda, "_x")
				body := g.emitExprSubst(lambda.Expr, p, currentVar)
				newVar := fmt.Sprintf("_v%d", g.tmpCounter)
				g.tmpCounter++
				g.writeln(fmt.Sprintf("%s = %s", newVar, body))
				currentVar = newVar
			}
		case "Skip":
			// Handled via counter above
		case "ToList":
			// Terminal — no-op
		}
	}

	g.writeln(fmt.Sprintf("%s[%s] = %s", resultVar, idxVar, currentVar))
	g.writeln(fmt.Sprintf("%s += 1", idxVar))

	for i := 0; i < indents; i++ {
		g.pop()
	}
	g.pop() // close for
}

// emitNumbaTakePass emits a single-pass loop with early exit for Take chains.
func (g *Generator) emitNumbaTakePass(chain *collectionChain) {
	g.writeln("for _x in _src:")
	g.push()

	currentVar := "_x"
	indents := 0

	var skipLimit string
	for _, step := range chain.steps {
		if step.method == "Skip" && len(step.args) > 0 {
			skipLimit = g.emitExpr(step.args[0])
		}
	}
	if skipLimit != "" {
		g.writeln("_skipped = 0")
	}

	for _, step := range chain.steps {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := extractLambda(step.args[0])
				p := lambdaParam(lambda, "_x")
				cond := g.emitExprSubst(lambda.Expr, p, currentVar)
				g.writeln(fmt.Sprintf("if %s:", cond))
				g.push()
				indents++
			}
		case "Select":
			if len(step.args) > 0 {
				lambda := extractLambda(step.args[0])
				p := lambdaParam(lambda, "_x")
				body := g.emitExprSubst(lambda.Expr, p, currentVar)
				newVar := fmt.Sprintf("_v%d", g.tmpCounter)
				g.tmpCounter++
				g.writeln(fmt.Sprintf("%s = %s", newVar, body))
				currentVar = newVar
			}
		case "Skip":
			if skipLimit != "" {
				g.writeln(fmt.Sprintf("if _skipped < %s:", skipLimit))
				g.push()
				g.writeln("_skipped += 1")
				g.writeln("continue")
				g.pop()
			}
		case "Take":
			takeLimit := g.emitExpr(step.args[0])
			g.writeln(fmt.Sprintf("if _taken >= %s:", takeLimit))
			g.push()
			g.writeln("break")
			g.pop()
		case "ToList":
			// Terminal
		}
	}

	g.writeln(fmt.Sprintf("_result[_taken] = %s", currentVar))
	g.writeln("_taken += 1")

	for i := 0; i < indents; i++ {
		g.pop()
	}
	g.pop() // close for
}

// emitNumbaScalarPass emits a single-pass loop for scalar terminals.
func (g *Generator) emitNumbaScalarPass(varName string, chain *collectionChain) {
	lastStep := chain.steps[len(chain.steps)-1]

	// Initialize terminal state
	switch lastStep.method {
	case "Any":
		g.writeln("_found = False")
	case "All":
		g.writeln("_found = True")
	case "Count":
		g.writeln("_count = 0")
	case "Aggregate":
		if len(lastStep.args) >= 2 {
			g.writeln(fmt.Sprintf("_acc = %s", g.emitExpr(lastStep.args[0])))
		}
	case "First", "FirstOrDefault":
		g.writeln("_first = np.int64(0)")
		g.writeln("_found_first = False")
	}

	g.writeln("for _x in _src:")
	g.push()

	currentVar := "_x"
	indents := 0

	for _, step := range chain.steps {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := extractLambda(step.args[0])
				p := lambdaParam(lambda, "_x")
				cond := g.emitExprSubst(lambda.Expr, p, currentVar)
				g.writeln(fmt.Sprintf("if %s:", cond))
				g.push()
				indents++
			}
		case "Select":
			if len(step.args) > 0 {
				lambda := extractLambda(step.args[0])
				p := lambdaParam(lambda, "_x")
				body := g.emitExprSubst(lambda.Expr, p, currentVar)
				newVar := fmt.Sprintf("_v%d", g.tmpCounter)
				g.tmpCounter++
				g.writeln(fmt.Sprintf("%s = %s", newVar, body))
				currentVar = newVar
			}
		case "Any":
			lambda := extractLambda(step.args[0])
			p := lambdaParam(lambda, "_x")
			cond := g.emitExprSubst(lambda.Expr, p, currentVar)
			g.writeln(fmt.Sprintf("if %s:", cond))
			g.push()
			g.writeln("_found = True")
			g.writeln("break")
			g.pop()
		case "All":
			lambda := extractLambda(step.args[0])
			p := lambdaParam(lambda, "_x")
			cond := g.emitExprSubst(lambda.Expr, p, currentVar)
			g.writeln(fmt.Sprintf("if not (%s):", cond))
			g.push()
			g.writeln("_found = False")
			g.writeln("break")
			g.pop()
		case "Count":
			g.writeln("_count += 1")
		case "Aggregate":
			if len(step.args) >= 2 {
				lambda := extractLambda(step.args[1])
				if lambda != nil && len(lambda.Params) >= 2 {
					body := g.emitExprSubst2(lambda.Expr,
						lambda.Params[0].Name, "_acc",
						lambda.Params[1].Name, currentVar)
					g.writeln(fmt.Sprintf("_acc = %s", body))
				}
			}
		case "First":
			g.writeln(fmt.Sprintf("_first = %s", currentVar))
			g.writeln("_found_first = True")
			g.writeln("break")
		case "FirstOrDefault":
			g.writeln(fmt.Sprintf("_first = %s", currentVar))
			g.writeln("_found_first = True")
			g.writeln("break")
		}
	}

	for i := 0; i < indents; i++ {
		g.pop()
	}
	g.pop() // close for

	// Return
	switch lastStep.method {
	case "Any", "All":
		g.writeln("return _found")
	case "Count":
		g.writeln("return _count")
	case "Aggregate":
		g.writeln("return _acc")
	case "First":
		g.writeln("return _first")
	case "FirstOrDefault":
		g.writeln("return _first if _found_first else 0")
	}
}

// findTakeLimit finds the Take limit in a chain.
func (g *Generator) findTakeLimit(chain *collectionChain) string {
	for _, step := range chain.steps {
		if step.method == "Take" && len(step.args) > 0 {
			return g.emitExpr(step.args[0])
		}
	}
	return "0"
}
