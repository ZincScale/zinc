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

package codegen

import (
	"fmt"
	"strings"

	"zinc/internal/parser"
)

// collectionMethods is the set of recognized LINQ-style collection method names.
var collectionMethods = map[string]bool{
	"Where":          true,
	"Select":         true,
	"SelectMany":     true,
	"ForEach":        true,
	"Any":            true,
	"All":            true,
	"First":          true,
	"FirstOrDefault": true,
	"Count":          true,
	"Take":           true,
	"Skip":           true,
	"Aggregate":      true,
	"ToList":         true,
}

// isCollectionMethod returns true if the method name is a LINQ-style collection method.
func isCollectionMethod(name string) bool {
	return collectionMethods[name]
}

// chainStep represents one operation in a collection method chain.
type chainStep struct {
	method string
	args   []parser.Expr
}

// collectionChain represents a parsed collection method chain:
// source.Step1(...).Step2(...).Step3(...)
type collectionChain struct {
	source parser.Expr
	steps  []chainStep
}

// unwrapChain checks if an expression is a collection method chain.
// Returns nil if the expression is not a collection chain.
// Unwraps right-to-left: a.Where(...).Select(...) is parsed as
// CallExpr{SelectorExpr{CallExpr{SelectorExpr{a, "Where"}, ...}, "Select"}, ...}
func (g *Generator) unwrapChain(expr parser.Expr) *collectionChain {
	call, ok := expr.(*parser.CallExpr)
	if !ok {
		return nil
	}
	sel, ok := call.Callee.(*parser.SelectorExpr)
	if !ok {
		return nil
	}
	if !isCollectionMethod(sel.Field) {
		return nil
	}

	// Check that the ultimate source is a builtin receiver (list/map, not a class method)
	step := chainStep{method: sel.Field, args: call.Args}

	// Try to unwrap further (the object might be another collection call)
	if inner := g.unwrapChain(sel.Object); inner != nil {
		inner.steps = append(inner.steps, step)
		return inner
	}

	// Base case: the object is the source collection
	// Verify it's a builtin receiver (not a class with a .Where method)
	if !g.isBuiltinReceiverExpr(sel.Object) {
		return nil
	}
	return &collectionChain{
		source: sel.Object,
		steps:  []chainStep{step},
	}
}

// isBuiltinReceiverExpr checks if an expression is a builtin receiver
// (i.e. not a class instance that would have its own methods).
func (g *Generator) isBuiltinReceiverExpr(expr parser.Expr) bool {
	switch e := expr.(type) {
	case *parser.ThisExpr:
		return false
	case *parser.Ident:
		if g.classNames[e.Name] || g.interfaceNames[e.Name] {
			return false
		}
		if _, ok := g.varTypes[e.Name]; ok {
			return false
		}
		if g.classVars[e.Name] != "" {
			return false
		}
		if g.interfaceVars[e.Name] != "" {
			return false
		}
		if g.receiver != "" && e.Name == g.receiver {
			return false
		}
		return true
	case *parser.SelectorExpr:
		// e.g. this.items — field access
		return true
	case *parser.CallExpr:
		// Result of a function call — treat as builtin
		return true
	default:
		return true
	}
}

// emitCollectionChainVar emits a fused loop for a collection chain assigned to a variable.
// e.g. var result = nums.Where(x => x > 5).Select(x => x * 2)
func (g *Generator) emitCollectionChainVar(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	switch lastStep.method {
	case "Any", "All":
		g.emitAnyAllChain(varName, chain)
		return
	case "Count":
		g.emitCountChain(varName, chain)
		return
	case "Aggregate":
		g.emitAggregateChain(varName, chain)
		return
	case "First":
		g.emitFirstChain(varName, chain, true)
		return
	case "FirstOrDefault":
		g.emitFirstChain(varName, chain, false)
		return
	}

	// List-producing chain (Where, Select, Take, Skip, ToList, etc.)
	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++
	currentVar := elemVar

	// Check for Take/Skip in chain
	var takeVar, skipVar string
	for _, step := range chain.steps {
		if step.method == "Take" && len(step.args) > 0 {
			takeVar = fmt.Sprintf("_take%d", g.tmpCounter)
			g.tmpCounter++
		}
		if step.method == "Skip" && len(step.args) > 0 {
			skipVar = fmt.Sprintf("_skip%d", g.tmpCounter)
			g.tmpCounter++
		}
	}

	// Declare result variable
	g.writeln(fmt.Sprintf("var %s []interface{}", varName))

	// Pre-loop counter variables
	if takeVar != "" {
		g.writeln(fmt.Sprintf("%s := 0", takeVar))
	}
	if skipVar != "" {
		g.writeln(fmt.Sprintf("%s := 0", skipVar))
	}

	// Loop header
	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	// Track how many if-blocks we opened (for closing)
	openIfs := 0

	for _, step := range chain.steps {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					cond := g.emitLambdaExpr(lambda, currentVar)
					g.writeln(fmt.Sprintf("if %s {", cond))
					g.push()
					openIfs++
				}
			}
		case "Select":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					newVar := fmt.Sprintf("_v%d", g.tmpCounter)
					g.tmpCounter++
					body := g.emitLambdaExpr(lambda, currentVar)
					g.writeln(fmt.Sprintf("%s := %s", newVar, body))
					currentVar = newVar
				}
			}
		case "Take":
			if len(step.args) > 0 {
				limit := g.emitExpr(step.args[0])
				g.writeln(fmt.Sprintf("if %s >= %s { break }", takeVar, limit))
			}
		case "Skip":
			if len(step.args) > 0 {
				limit := g.emitExpr(step.args[0])
				g.writeln(fmt.Sprintf("if %s < %s { %s++; continue }", skipVar, limit, skipVar))
			}
		case "ToList":
			// no-op terminal — just means collect into result
		}
	}

	// Accumulate result
	g.writeln(fmt.Sprintf("%s = append(%s, %s)", varName, varName, currentVar))

	// Increment take counter if present
	if takeVar != "" {
		g.writeln(fmt.Sprintf("%s++", takeVar))
	}

	// Close Where if-blocks
	for i := 0; i < openIfs; i++ {
		g.pop()
		g.writeln("}")
	}

	g.pop()
	g.writeln("}")
}

// emitCollectionChainStmt emits a fused loop for a collection chain used as a statement.
// e.g. nums.Where(x => x > 5).ForEach(x => print(x))
func (g *Generator) emitCollectionChainStmt(chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	if lastStep.method != "ForEach" {
		// Non-ForEach terminal as statement — emit as discarded expression
		return
	}

	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++
	currentVar := elemVar

	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	openIfs := 0

	// Process all steps except the last (ForEach)
	for _, step := range chain.steps[:len(chain.steps)-1] {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					cond := g.emitLambdaExpr(lambda, currentVar)
					g.writeln(fmt.Sprintf("if %s {", cond))
					g.push()
					openIfs++
				}
			}
		case "Select":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					newVar := fmt.Sprintf("_v%d", g.tmpCounter)
					g.tmpCounter++
					body := g.emitLambdaExpr(lambda, currentVar)
					g.writeln(fmt.Sprintf("%s := %s", newVar, body))
					currentVar = newVar
				}
			}
		}
	}

	// Emit ForEach body
	lambda := g.extractLambda(lastStep.args[0])
	if lambda != nil && len(lambda.Params) > 0 {
		if lambda.Expr != nil {
			body := g.emitLambdaExpr(lambda, currentVar)
			g.writeln(body)
		} else if lambda.Body != nil {
			// Block-body ForEach — emit statements with substitution
			// For now, use the lambda param name as-is since it matches the loop var
			// We alias the loop var to the lambda param name
			if lambda.Params[0].Name != currentVar {
				g.writeln(fmt.Sprintf("%s := %s", lambda.Params[0].Name, currentVar))
			}
			g.emitBlock(lambda.Body)
		}
	}

	for i := 0; i < openIfs; i++ {
		g.pop()
		g.writeln("}")
	}

	g.pop()
	g.writeln("}")
}

// emitAnyAllChain emits a fused loop for Any/All terminal.
func (g *Generator) emitAnyAllChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]
	isAny := lastStep.method == "Any"

	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++
	currentVar := elemVar

	if isAny {
		g.writeln(fmt.Sprintf("%s := false", varName))
	} else {
		g.writeln(fmt.Sprintf("%s := true", varName))
	}

	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	openIfs := 0

	// Process intermediate steps (Where, Select before Any/All)
	for _, step := range chain.steps[:len(chain.steps)-1] {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					cond := g.emitLambdaExpr(lambda, currentVar)
					g.writeln(fmt.Sprintf("if %s {", cond))
					g.push()
					openIfs++
				}
			}
		case "Select":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					newVar := fmt.Sprintf("_v%d", g.tmpCounter)
					g.tmpCounter++
					body := g.emitLambdaExpr(lambda, currentVar)
					g.writeln(fmt.Sprintf("%s := %s", newVar, body))
					currentVar = newVar
				}
			}
		}
	}

	// Terminal predicate
	lambda := g.extractLambda(lastStep.args[0])
	if lambda != nil && len(lambda.Params) > 0 {
		cond := g.emitLambdaExpr(lambda, currentVar)
		if isAny {
			g.writeln(fmt.Sprintf("if %s {", cond))
			g.push()
			g.writeln(fmt.Sprintf("%s = true", varName))
			g.writeln("break")
			g.pop()
			g.writeln("}")
		} else {
			g.writeln(fmt.Sprintf("if !(%s) {", cond))
			g.push()
			g.writeln(fmt.Sprintf("%s = false", varName))
			g.writeln("break")
			g.pop()
			g.writeln("}")
		}
	}

	for i := 0; i < openIfs; i++ {
		g.pop()
		g.writeln("}")
	}

	g.pop()
	g.writeln("}")
}

// emitCountChain emits a fused loop for Count terminal.
func (g *Generator) emitCountChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++
	currentVar := elemVar

	g.writeln(fmt.Sprintf("%s := 0", varName))
	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	openIfs := 0
	for _, step := range chain.steps[:len(chain.steps)-1] {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					cond := g.emitLambdaExpr(lambda, currentVar)
					g.writeln(fmt.Sprintf("if %s {", cond))
					g.push()
					openIfs++
				}
			}
		}
	}

	g.writeln(fmt.Sprintf("%s++", varName))

	for i := 0; i < openIfs; i++ {
		g.pop()
		g.writeln("}")
	}

	g.pop()
	g.writeln("}")
}

// emitAggregateChain emits a fused loop for Aggregate terminal.
func (g *Generator) emitAggregateChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	// Aggregate(seed, (acc, x) => acc + x)
	if len(lastStep.args) < 2 {
		return
	}
	seed := g.emitExpr(lastStep.args[0])
	lambda := g.extractLambda(lastStep.args[1])
	if lambda == nil || len(lambda.Params) < 2 {
		return
	}

	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++
	currentVar := elemVar

	g.writeln(fmt.Sprintf("%s := %s", varName, seed))
	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	openIfs := 0
	for _, step := range chain.steps[:len(chain.steps)-1] {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				wl := g.extractLambda(step.args[0])
				if wl != nil && len(wl.Params) > 0 {
					cond := g.emitLambdaExpr(wl, currentVar)
					g.writeln(fmt.Sprintf("if %s {", cond))
					g.push()
					openIfs++
				}
			}
		case "Select":
			if len(step.args) > 0 {
				sl := g.extractLambda(step.args[0])
				if sl != nil && len(sl.Params) > 0 {
					newVar := fmt.Sprintf("_v%d", g.tmpCounter)
					g.tmpCounter++
					body := g.emitLambdaExpr(sl, currentVar)
					g.writeln(fmt.Sprintf("%s := %s", newVar, body))
					currentVar = newVar
				}
			}
		}
	}

	// Emit the aggregate: acc = reducer(acc, currentVar)
	reduced := g.emitLambdaExpr2(lambda, varName, currentVar)
	g.writeln(fmt.Sprintf("%s = %s", varName, reduced))

	for i := 0; i < openIfs; i++ {
		g.pop()
		g.writeln("}")
	}

	g.pop()
	g.writeln("}")
}

// emitFirstChain emits a fused loop for First/FirstOrDefault terminal.
func (g *Generator) emitFirstChain(varName string, chain *collectionChain, failable bool) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++
	currentVar := elemVar

	foundVar := fmt.Sprintf("_found%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("var %s interface{}", varName))
	if failable {
		g.writeln(fmt.Sprintf("%s := false", foundVar))
	}
	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	openIfs := 0
	for _, step := range chain.steps[:len(chain.steps)-1] {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					cond := g.emitLambdaExpr(lambda, currentVar)
					g.writeln(fmt.Sprintf("if %s {", cond))
					g.push()
					openIfs++
				}
			}
		case "Select":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					newVar := fmt.Sprintf("_v%d", g.tmpCounter)
					g.tmpCounter++
					body := g.emitLambdaExpr(lambda, currentVar)
					g.writeln(fmt.Sprintf("%s := %s", newVar, body))
					currentVar = newVar
				}
			}
		}
	}

	// Apply First's own predicate if present
	if len(lastStep.args) > 0 {
		lambda := g.extractLambda(lastStep.args[0])
		if lambda != nil && len(lambda.Params) > 0 {
			cond := g.emitLambdaExpr(lambda, currentVar)
			g.writeln(fmt.Sprintf("if %s {", cond))
			g.push()
			openIfs++
		}
	}

	g.writeln(fmt.Sprintf("%s = %s", varName, currentVar))
	if failable {
		g.writeln(fmt.Sprintf("%s = true", foundVar))
	}
	g.writeln("break")

	for i := 0; i < openIfs; i++ {
		g.pop()
		g.writeln("}")
	}

	g.pop()
	g.writeln("}")

	if failable {
		g.neededImports["fmt"] = true
		g.writeln(fmt.Sprintf("if !%s {", foundVar))
		g.push()
		g.writeln(fmt.Sprintf("panic(fmt.Errorf(\"no matching element found\"))"))
		g.pop()
		g.writeln("}")
	}
}

// extractLambda extracts a LambdaExpr from an expression (it might be the expr directly).
func (g *Generator) extractLambda(expr parser.Expr) *parser.LambdaExpr {
	if lambda, ok := expr.(*parser.LambdaExpr); ok {
		return lambda
	}
	return nil
}

// lambdaContainsFailable recursively checks if an expression tree contains any failable call.
func (g *Generator) lambdaContainsFailable(expr parser.Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *parser.CallExpr:
		if g.callIsFailable(e) {
			return true
		}
		if g.lambdaContainsFailable(e.Callee) {
			return true
		}
		for _, arg := range e.Args {
			if g.lambdaContainsFailable(arg) {
				return true
			}
		}
	case *parser.BinaryExpr:
		return g.lambdaContainsFailable(e.Left) || g.lambdaContainsFailable(e.Right)
	case *parser.UnaryExpr:
		return g.lambdaContainsFailable(e.Operand)
	case *parser.SelectorExpr:
		return g.lambdaContainsFailable(e.Object)
	case *parser.IndexExpr:
		return g.lambdaContainsFailable(e.Object) || g.lambdaContainsFailable(e.Index)
	}
	return false
}

// emitLambdaExpr emits a single-param lambda expression within a collection chain.
// If the expression contains failable calls, binds the param as a local variable
// and lifts failable calls to statements with error propagation. Otherwise uses
// inline substitution for efficiency.
func (g *Generator) emitLambdaExpr(lambda *parser.LambdaExpr, currentVar string) string {
	if g.lambdaContainsFailable(lambda.Expr) {
		g.writeln(fmt.Sprintf("%s := %s", lambda.Params[0].Name, currentVar))
		return g.emitExprLiftFailable(lambda.Expr)
	}
	return g.emitExprSubst(lambda.Expr, lambda.Params[0].Name, currentVar)
}

// emitLambdaExpr2 emits a two-param lambda expression (for Aggregate).
// Handles failable calls within the reducer expression.
func (g *Generator) emitLambdaExpr2(lambda *parser.LambdaExpr, var1, var2 string) string {
	if g.lambdaContainsFailable(lambda.Expr) {
		g.writeln(fmt.Sprintf("%s := %s", lambda.Params[0].Name, var1))
		g.writeln(fmt.Sprintf("%s := %s", lambda.Params[1].Name, var2))
		return g.emitExprLiftFailable(lambda.Expr)
	}
	return g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, var1, lambda.Params[1].Name, var2)
}

// emitExprLiftFailable emits an expression, lifting any failable calls into
// statement-level assignments with error checks. The lambda parameter must
// already be bound as a local variable before calling this.
// This is the failable-aware counterpart of emitExprSubst.
func (g *Generator) emitExprLiftFailable(expr parser.Expr) string {
	switch e := expr.(type) {
	case *parser.CallExpr:
		if g.callIsFailable(e) {
			tmpVar := fmt.Sprintf("_fv%d", g.tmpCounter)
			g.tmpCounter++
			errVar := g.nextErr()
			callStr := g.emitFailableCallExpr(e)
			g.writeln(fmt.Sprintf("%s, %s := %s", tmpVar, errVar, callStr))
			g.emitErrorCheck(errVar, nil)
			return tmpVar
		}
		return g.emitExpr(e)
	case *parser.BinaryExpr:
		left := g.emitExprLiftFailable(e.Left)
		right := g.emitExprLiftFailable(e.Right)
		return fmt.Sprintf("(%s %s %s)", left, e.Op, right)
	case *parser.UnaryExpr:
		operand := g.emitExprLiftFailable(e.Operand)
		return fmt.Sprintf("(%s%s)", e.Op, operand)
	default:
		return g.emitExpr(expr)
	}
}

// emitExprSubst emits an expression with a single variable substitution.
// When the expression contains an Ident matching paramName, it's replaced with replacement.
func (g *Generator) emitExprSubst(expr parser.Expr, paramName, replacement string) string {
	switch e := expr.(type) {
	case *parser.Ident:
		if e.Name == paramName {
			return replacement
		}
		return g.emitExpr(expr)
	case *parser.BinaryExpr:
		left := g.emitExprSubst(e.Left, paramName, replacement)
		right := g.emitExprSubst(e.Right, paramName, replacement)
		return fmt.Sprintf("(%s %s %s)", left, e.Op, right)
	case *parser.UnaryExpr:
		operand := g.emitExprSubst(e.Operand, paramName, replacement)
		return fmt.Sprintf("(%s%s)", e.Op, operand)
	case *parser.SelectorExpr:
		obj := g.emitExprSubst(e.Object, paramName, replacement)
		field := capitalize(e.Field)
		return fmt.Sprintf("%s.%s", obj, field)
	case *parser.CallExpr:
		callee := g.emitExprSubst(e.Callee, paramName, replacement)
		var args []string
		for _, arg := range e.Args {
			args = append(args, g.emitExprSubst(arg, paramName, replacement))
		}
		return fmt.Sprintf("%s(%s)", callee, strings.Join(args, ", "))
	case *parser.IndexExpr:
		obj := g.emitExprSubst(e.Object, paramName, replacement)
		idx := g.emitExprSubst(e.Index, paramName, replacement)
		return fmt.Sprintf("%s[%s]", obj, idx)
	default:
		return g.emitExpr(expr)
	}
}

// emitExprSubst2 emits an expression with two variable substitutions.
func (g *Generator) emitExprSubst2(expr parser.Expr, name1, repl1, name2, repl2 string) string {
	switch e := expr.(type) {
	case *parser.Ident:
		if e.Name == name1 {
			return repl1
		}
		if e.Name == name2 {
			return repl2
		}
		return g.emitExpr(expr)
	case *parser.BinaryExpr:
		left := g.emitExprSubst2(e.Left, name1, repl1, name2, repl2)
		right := g.emitExprSubst2(e.Right, name1, repl1, name2, repl2)
		return fmt.Sprintf("(%s %s %s)", left, e.Op, right)
	case *parser.UnaryExpr:
		operand := g.emitExprSubst2(e.Operand, name1, repl1, name2, repl2)
		return fmt.Sprintf("(%s%s)", e.Op, operand)
	case *parser.SelectorExpr:
		obj := g.emitExprSubst2(e.Object, name1, repl1, name2, repl2)
		return fmt.Sprintf("%s.%s", obj, capitalize(e.Field))
	case *parser.IndexExpr:
		obj := g.emitExprSubst2(e.Object, name1, repl1, name2, repl2)
		idx := g.emitExprSubst2(e.Index, name1, repl1, name2, repl2)
		return fmt.Sprintf("%s[%s]", obj, idx)
	case *parser.CallExpr:
		callee := g.emitExprSubst2(e.Callee, name1, repl1, name2, repl2)
		var args []string
		for _, arg := range e.Args {
			args = append(args, g.emitExprSubst2(arg, name1, repl1, name2, repl2))
		}
		return fmt.Sprintf("%s(%s)", callee, strings.Join(args, ", "))
	default:
		return g.emitExpr(expr)
	}
}
