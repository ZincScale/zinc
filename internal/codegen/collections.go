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
	// List methods (v1)
	"Where":          true,
	"Select":         true,
	"SelectMany":     true,
	"ForEach":        true,
	"Any":            true,
	"All":            true,
	"First":          true,
	"FirstOrDefault": true,
	"Last":           true,
	"Count":          true,
	"Sum":            true,
	"Min":            true,
	"Max":            true,
	"Take":           true,
	"Skip":           true,
	"Aggregate":      true,
	"ToList":         true,
	"ToDictionary":   true,
	"Zip":            true,
	"Distinct":       true,
	"OrderBy":        true,
	"OrderByDescending": true,
	"GroupBy":        true,
	// Map-specific methods
	"SelectValues":   true,
	"SelectKeys":     true,
}

// materializationMethods require seeing all elements before downstream steps can proceed.
var materializationMethods = map[string]bool{
	"OrderBy":           true,
	"OrderByDescending": true,
}

// inferElemType infers the Go element type of a list source expression.
// Returns "interface{}" if the type cannot be determined.
func (g *Generator) inferElemType(source parser.Expr) string {
	switch s := source.(type) {
	case *parser.ListLit:
		if s.ResolvedType != "" {
			// e.g. "[]int" -> "int"
			if strings.HasPrefix(s.ResolvedType, "[]") {
				return s.ResolvedType[2:]
			}
		}
		if len(s.Elements) > 0 {
			return g.inferLitType(s.Elements[0])
		}
	case *parser.Ident:
		// Check known variable types if we track them
		// For now, fall through
	}
	return "interface{}"
}

// inferMapKeyValTypes infers the Go key and value types of a map source expression.
func (g *Generator) inferMapKeyValTypes(source parser.Expr) (string, string) {
	switch s := source.(type) {
	case *parser.MapLit:
		if s.ResolvedType != "" {
			// e.g. "map[string]int" -> "string", "int"
			if strings.HasPrefix(s.ResolvedType, "map[") {
				inner := s.ResolvedType[4:]
				depth := 0
				for i, ch := range inner {
					if ch == '[' {
						depth++
					} else if ch == ']' {
						if depth == 0 {
							return inner[:i], inner[i+1:]
						}
						depth--
					}
				}
			}
		}
		keyType := "interface{}"
		valType := "interface{}"
		if len(s.Keys) > 0 {
			keyType = g.inferLitType(s.Keys[0])
			valType = g.inferLitType(s.Values[0])
		}
		return keyType, valType
	}
	return "interface{}", "interface{}"
}

// inferLitType returns the Go type of a literal expression.
func (g *Generator) inferLitType(expr parser.Expr) string {
	switch expr.(type) {
	case *parser.IntLit:
		return "int"
	case *parser.FloatLit:
		return "float64"
	case *parser.StringLit:
		return "string"
	case *parser.BoolLit:
		return "bool"
	}
	return "interface{}"
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

// isMapChain detects if a chain operates on a map by checking for map-only methods
// or 2-param lambdas on the first step.
func (g *Generator) isMapChain(chain *collectionChain) bool {
	for _, step := range chain.steps {
		switch step.method {
		case "SelectValues", "SelectKeys":
			return true
		}
		// Check for 2-param lambdas (map iteration: (k, v) => ...)
		if len(step.args) > 0 {
			if lambda := g.extractLambda(step.args[0]); lambda != nil && len(lambda.Params) == 2 {
				// Aggregate also uses 2 params (acc, x) — skip it
				if step.method != "Aggregate" && step.method != "Zip" {
					return true
				}
			}
		}
		// Aggregate with 3-param lambda (acc, k, v) => ... indicates map
		if step.method == "Aggregate" && len(step.args) >= 2 {
			if lambda := g.extractLambda(step.args[1]); lambda != nil && len(lambda.Params) == 3 {
				return true
			}
		}
	}
	return false
}

// hasMaterializationPoint returns true if the chain contains any materialization step.
func hasMaterializationPoint(chain *collectionChain) bool {
	for _, step := range chain.steps {
		if materializationMethods[step.method] {
			return true
		}
	}
	return false
}

// segmentChain splits a chain at materialization points.
// Each segment is a sub-chain; the last step of non-final segments is the materializer.
func segmentChain(chain *collectionChain) []collectionChain {
	var segments []collectionChain
	current := collectionChain{source: chain.source}
	for _, step := range chain.steps {
		current.steps = append(current.steps, step)
		if materializationMethods[step.method] {
			segments = append(segments, current)
			current = collectionChain{} // source set later
		}
	}
	if len(current.steps) > 0 {
		segments = append(segments, current)
	}
	return segments
}

// emitIntermediateSteps processes Where/Select/Take/Skip/SelectMany/Distinct
// steps, returning the current variable name and the number of open if-blocks/for-blocks.
// The caller must close these blocks after emitting the terminal.
type openBlock struct {
	kind string // "if" or "for"
}

func (g *Generator) emitIntermediateSteps(steps []chainStep, elemVar string) (currentVar string, blocks []openBlock) {
	currentVar = elemVar
	for _, step := range steps {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					cond := g.emitLambdaExpr(lambda, currentVar)
					g.writeln(fmt.Sprintf("if %s {", cond))
					g.push()
					blocks = append(blocks, openBlock{"if"})
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
		case "SelectMany":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					subList := g.emitLambdaExpr(lambda, currentVar)
					smVar := fmt.Sprintf("_v%d", g.tmpCounter)
					g.tmpCounter++
					g.writeln(fmt.Sprintf("for _, %s := range %s {", smVar, subList))
					g.push()
					blocks = append(blocks, openBlock{"for"})
					currentVar = smVar
				}
			}
		case "Take":
			// handled by pre-allocated takeVar in caller
		case "Skip":
			// handled by pre-allocated skipVar in caller
		case "Distinct":
			// handled by pre-allocated seenVar in caller
		case "ToList":
			// no-op
		}
	}
	return currentVar, blocks
}

func (g *Generator) closeBlocks(blocks []openBlock) {
	for i := len(blocks) - 1; i >= 0; i-- {
		g.pop()
		g.writeln("}")
	}
}

// emitCollectionChainVar emits a fused loop for a collection chain assigned to a variable.
// e.g. var result = nums.Where(x => x > 5).Select(x => x * 2)
func (g *Generator) emitCollectionChainVar(varName string, chain *collectionChain) {
	// Handle materialization segmentation (OrderBy/OrderByDescending in mid-chain)
	if hasMaterializationPoint(chain) {
		g.emitSegmentedChain(varName, chain)
		return
	}

	// Detect map vs list chains
	if g.isMapChain(chain) {
		g.emitMapCollectionChainVar(varName, chain)
		return
	}

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
	case "Last":
		g.emitLastChain(varName, chain)
		return
	case "Sum":
		g.emitSumChain(varName, chain)
		return
	case "Min":
		g.emitMinMaxChain(varName, chain, "<")
		return
	case "Max":
		g.emitMinMaxChain(varName, chain, ">")
		return
	case "ToDictionary":
		g.emitToDictionaryChain(varName, chain)
		return
	case "GroupBy":
		g.emitGroupByChain(varName, chain)
		return
	case "Zip":
		g.emitZipChain(varName, chain)
		return
	}

	// List-producing chain (Where, Select, Take, Skip, Distinct, SelectMany, ToList)
	g.emitListProducingChain(varName, chain)
}

// emitListProducingChain emits a fused loop for chains that produce a list.
func (g *Generator) emitListProducingChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	// Check for Take/Skip/Distinct in chain
	var takeVar, skipVar, seenVar string
	for _, step := range chain.steps {
		if step.method == "Take" && len(step.args) > 0 {
			takeVar = fmt.Sprintf("_take%d", g.tmpCounter)
			g.tmpCounter++
		}
		if step.method == "Skip" && len(step.args) > 0 {
			skipVar = fmt.Sprintf("_skip%d", g.tmpCounter)
			g.tmpCounter++
		}
		if step.method == "Distinct" {
			seenVar = fmt.Sprintf("_seen%d", g.tmpCounter)
			g.tmpCounter++
		}
	}

	// Declare result variable with inferred element type
	hasSelectMany := false
	for _, step := range chain.steps {
		if step.method == "SelectMany" {
			hasSelectMany = true
			break
		}
	}
	elemType := g.inferElemType(chain.source)
	if hasSelectMany {
		// SelectMany flattens: if source is [][]T, result is []T
		if elemType != "interface{}" && strings.HasPrefix(elemType, "[]") {
			g.writeln(fmt.Sprintf("var %s %s", varName, elemType))
		} else {
			// Use type inference: declare var after first element
			g.writeln(fmt.Sprintf("var %s []interface{}", varName))
		}
	} else if elemType != "interface{}" {
		g.writeln(fmt.Sprintf("var %s []%s", varName, elemType))
	} else {
		// Use source slice trick to preserve concrete type
		g.writeln(fmt.Sprintf("%s := %s[:0:0]", varName, source))
	}

	// Pre-loop variables
	if takeVar != "" {
		g.writeln(fmt.Sprintf("%s := 0", takeVar))
	}
	if skipVar != "" {
		g.writeln(fmt.Sprintf("%s := 0", skipVar))
	}
	if seenVar != "" {
		g.writeln(fmt.Sprintf("%s := make(map[%s]bool)", seenVar, elemType))
	}

	// Loop header
	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	// Process steps
	currentVar := elemVar
	var blocks []openBlock

	for _, step := range chain.steps {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					cond := g.emitLambdaExpr(lambda, currentVar)
					g.writeln(fmt.Sprintf("if %s {", cond))
					g.push()
					blocks = append(blocks, openBlock{"if"})
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
		case "SelectMany":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) > 0 {
					subList := g.emitLambdaExpr(lambda, currentVar)
					smVar := fmt.Sprintf("_v%d", g.tmpCounter)
					g.tmpCounter++
					g.writeln(fmt.Sprintf("for _, %s := range %s {", smVar, subList))
					g.push()
					blocks = append(blocks, openBlock{"for"})
					currentVar = smVar
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
		case "Distinct":
			g.writeln(fmt.Sprintf("if %s[%s] { continue }", seenVar, currentVar))
			g.writeln(fmt.Sprintf("%s[%s] = true", seenVar, currentVar))
		case "ToList":
			// no-op
		}
	}

	// Accumulate result
	g.writeln(fmt.Sprintf("%s = append(%s, %s)", varName, varName, currentVar))

	// Increment take counter if present
	if takeVar != "" {
		g.writeln(fmt.Sprintf("%s++", takeVar))
	}

	// Close blocks
	g.closeBlocks(blocks)

	g.pop()
	g.writeln("}")
}

// emitCollectionChainStmt emits a fused loop for a collection chain used as a statement.
// e.g. nums.Where(x => x > 5).ForEach(x => print(x))
func (g *Generator) emitCollectionChainStmt(chain *collectionChain) {
	lastStep := chain.steps[len(chain.steps)-1]

	if lastStep.method != "ForEach" {
		// Non-ForEach terminal as statement — emit as discarded expression
		return
	}

	// Detect map chain
	if g.isMapChain(chain) {
		g.emitMapCollectionChainStmt(chain)
		return
	}

	source := g.emitExpr(chain.source)
	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	currentVar, blocks := g.emitIntermediateSteps(chain.steps[:len(chain.steps)-1], elemVar)

	// Emit ForEach body
	lambda := g.extractLambda(lastStep.args[0])
	if lambda != nil && len(lambda.Params) > 0 {
		if lambda.Expr != nil {
			body := g.emitLambdaExpr(lambda, currentVar)
			g.writeln(body)
		} else if lambda.Body != nil {
			if lambda.Params[0].Name != currentVar {
				g.writeln(fmt.Sprintf("%s := %s", lambda.Params[0].Name, currentVar))
			}
			g.emitBlock(lambda.Body)
		}
	}

	g.closeBlocks(blocks)
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

// emitLastChain emits a fused loop for Last terminal (scans all, takes last match).
func (g *Generator) emitLastChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	foundVar := fmt.Sprintf("_found%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("var %s interface{}", varName))
	g.writeln(fmt.Sprintf("%s := false", foundVar))
	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	currentVar, blocks := g.emitIntermediateSteps(chain.steps[:len(chain.steps)-1], elemVar)

	// Apply Last's own predicate if present
	if len(lastStep.args) > 0 {
		lambda := g.extractLambda(lastStep.args[0])
		if lambda != nil && len(lambda.Params) > 0 {
			cond := g.emitLambdaExpr(lambda, currentVar)
			g.writeln(fmt.Sprintf("if %s {", cond))
			g.push()
			blocks = append(blocks, openBlock{"if"})
		}
	}

	g.writeln(fmt.Sprintf("%s = %s", varName, currentVar))
	g.writeln(fmt.Sprintf("%s = true", foundVar))
	// No break — Last must scan all elements

	g.closeBlocks(blocks)
	g.pop()
	g.writeln("}")

	g.neededImports["fmt"] = true
	g.writeln(fmt.Sprintf("if !%s {", foundVar))
	g.push()
	g.writeln(fmt.Sprintf("panic(fmt.Errorf(\"no matching element found\"))"))
	g.pop()
	g.writeln("}")
}

// emitSumChain emits a fused loop for Sum terminal.
func (g *Generator) emitSumChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("%s := 0", varName))
	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	currentVar, blocks := g.emitIntermediateSteps(chain.steps[:len(chain.steps)-1], elemVar)

	// Sum with optional selector: .Sum(x => x.age) or .Sum()
	if len(lastStep.args) > 0 {
		lambda := g.extractLambda(lastStep.args[0])
		if lambda != nil && len(lambda.Params) > 0 {
			newVar := fmt.Sprintf("_v%d", g.tmpCounter)
			g.tmpCounter++
			body := g.emitLambdaExpr(lambda, currentVar)
			g.writeln(fmt.Sprintf("%s := %s", newVar, body))
			currentVar = newVar
		}
	}

	g.writeln(fmt.Sprintf("%s += %s", varName, currentVar))

	g.closeBlocks(blocks)
	g.pop()
	g.writeln("}")
}

// emitMinMaxChain emits a fused loop for Min/Max terminal.
// Uses source[0] to initialize the result with the correct type.
func (g *Generator) emitMinMaxChain(varName string, chain *collectionChain, op string) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++
	firstVar := fmt.Sprintf("_first%d", g.tmpCounter)
	g.tmpCounter++

	// Initialize with first element to get correct type
	g.writeln(fmt.Sprintf("%s := %s[0]", varName, source))
	g.writeln(fmt.Sprintf("%s := true", firstVar))
	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	currentVar, blocks := g.emitIntermediateSteps(chain.steps[:len(chain.steps)-1], elemVar)

	// Min/Max with optional selector
	valVar := currentVar
	if len(lastStep.args) > 0 {
		lambda := g.extractLambda(lastStep.args[0])
		if lambda != nil && len(lambda.Params) > 0 {
			newVar := fmt.Sprintf("_v%d", g.tmpCounter)
			g.tmpCounter++
			body := g.emitLambdaExpr(lambda, currentVar)
			g.writeln(fmt.Sprintf("%s := %s", newVar, body))
			valVar = newVar
		}
	}

	g.writeln(fmt.Sprintf("if %s || %s %s %s {", firstVar, valVar, op, varName))
	g.push()
	g.writeln(fmt.Sprintf("%s = %s", varName, valVar))
	g.writeln(fmt.Sprintf("%s = false", firstVar))
	g.pop()
	g.writeln("}")

	g.closeBlocks(blocks)
	g.pop()
	g.writeln("}")
}

// emitToDictionaryChain emits a fused loop for ToDictionary terminal.
// ToDictionary(keySelector) or ToDictionary(keySelector, valueSelector)
func (g *Generator) emitToDictionaryChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("%s := make(map[interface{}]interface{})", varName))
	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	currentVar, blocks := g.emitIntermediateSteps(chain.steps[:len(chain.steps)-1], elemVar)

	if len(lastStep.args) >= 1 {
		keyLambda := g.extractLambda(lastStep.args[0])
		if keyLambda != nil && len(keyLambda.Params) > 0 {
			keyExpr := g.emitLambdaExpr(keyLambda, currentVar)
			if len(lastStep.args) >= 2 {
				valLambda := g.extractLambda(lastStep.args[1])
				if valLambda != nil && len(valLambda.Params) > 0 {
					valExpr := g.emitLambdaExpr(valLambda, currentVar)
					g.writeln(fmt.Sprintf("%s[%s] = %s", varName, keyExpr, valExpr))
				}
			} else {
				g.writeln(fmt.Sprintf("%s[%s] = %s", varName, keyExpr, currentVar))
			}
		}
	}

	g.closeBlocks(blocks)
	g.pop()
	g.writeln("}")
}

// emitGroupByChain emits a fused loop for GroupBy terminal.
func (g *Generator) emitGroupByChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	elemVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("%s := make(map[interface{}][]interface{})", varName))
	g.writeln(fmt.Sprintf("for _, %s := range %s {", elemVar, source))
	g.push()

	currentVar, blocks := g.emitIntermediateSteps(chain.steps[:len(chain.steps)-1], elemVar)

	if len(lastStep.args) > 0 {
		lambda := g.extractLambda(lastStep.args[0])
		if lambda != nil && len(lambda.Params) > 0 {
			keyExpr := g.emitLambdaExpr(lambda, currentVar)
			keyVar := fmt.Sprintf("_k%d", g.tmpCounter)
			g.tmpCounter++
			g.writeln(fmt.Sprintf("%s := %s", keyVar, keyExpr))
			g.writeln(fmt.Sprintf("%s[%s] = append(%s[%s], %s)", varName, keyVar, varName, keyVar, currentVar))
		}
	}

	g.closeBlocks(blocks)
	g.pop()
	g.writeln("}")
}

// emitZipChain emits a Zip operation combining two lists.
// list1.Zip(list2, (a, b) => expr)
func (g *Generator) emitZipChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	if len(lastStep.args) < 2 {
		return
	}

	otherList := g.emitExpr(lastStep.args[0])
	lambda := g.extractLambda(lastStep.args[1])
	if lambda == nil || len(lambda.Params) < 2 {
		return
	}

	idxVar := fmt.Sprintf("_i%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("var %s []interface{}", varName))
	g.writeln(fmt.Sprintf("for %s := 0; %s < len(%s) && %s < len(%s); %s++ {",
		idxVar, idxVar, source, idxVar, otherList, idxVar))
	g.push()

	aVar := fmt.Sprintf("%s[%s]", source, idxVar)
	bVar := fmt.Sprintf("%s[%s]", otherList, idxVar)
	body := g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, aVar, lambda.Params[1].Name, bVar)
	g.writeln(fmt.Sprintf("%s = append(%s, %s)", varName, varName, body))

	g.pop()
	g.writeln("}")
}

// emitSegmentedChain handles chains with materialization points (OrderBy).
func (g *Generator) emitSegmentedChain(varName string, chain *collectionChain) {
	segments := segmentChain(chain)

	// Track the current source for each segment
	currentSource := chain.source

	for i, seg := range segments {
		isLast := i == len(segments)-1
		lastIdx := len(seg.steps) - 1
		lastMethod := seg.steps[lastIdx].method

		if materializationMethods[lastMethod] {
			// This segment ends with a materializer (OrderBy etc.)
			// If this is the last segment, use the user's variable name
			segVar := varName
			if !isLast {
				segVar = fmt.Sprintf("_seg%d", g.tmpCounter)
				g.tmpCounter++
			}

			// Emit the fusible steps before the materializer as a list-producing chain
			preSteps := seg.steps[:lastIdx]
			if len(preSteps) > 0 {
				// Create a new slice to avoid overwriting the materializer step
				steps := make([]chainStep, len(preSteps)+1)
				copy(steps, preSteps)
				steps[len(preSteps)] = chainStep{method: "ToList"}
				preChain := &collectionChain{source: currentSource, steps: steps}
				g.emitListProducingChain(segVar, preChain)
			} else {
				// No pre-steps, copy source into mutable slice (preserves concrete type)
				sourceExpr := g.emitExpr(currentSource)
				g.writeln(fmt.Sprintf("%s := append(%s[:0:0], %s...)", segVar, sourceExpr, sourceExpr))
			}

			// Emit the materializer
			g.emitMaterializer(segVar, seg.steps[lastIdx])

			// Next segment's source is this segment's result
			currentSource = &parser.Ident{Name: segVar}
		} else {
			// Final (or only non-materializer) segment
			targetVar := varName
			if !isLast {
				targetVar = fmt.Sprintf("_seg%d", g.tmpCounter)
				g.tmpCounter++
			}
			subChain := &collectionChain{source: currentSource, steps: seg.steps}
			g.emitCollectionChainVar(targetVar, subChain)
			currentSource = &parser.Ident{Name: targetVar}
		}
	}
}

// emitMaterializer emits the materialization operation in-place on a variable.
func (g *Generator) emitMaterializer(varName string, step chainStep) {
	switch step.method {
	case "OrderBy", "OrderByDescending":
		g.neededImports["sort"] = true
		if len(step.args) > 0 {
			lambda := g.extractLambda(step.args[0])
			if lambda != nil && len(lambda.Params) > 0 {
				iKey := g.emitExprSubst(lambda.Expr, lambda.Params[0].Name, fmt.Sprintf("%s[i]", varName))
				jKey := g.emitExprSubst(lambda.Expr, lambda.Params[0].Name, fmt.Sprintf("%s[j]", varName))
				op := "<"
				if step.method == "OrderByDescending" {
					op = ">"
				}
				g.writeln(fmt.Sprintf("sort.Slice(%s, func(i, j int) bool {", varName))
				g.push()
				g.writeln(fmt.Sprintf("return %s %s %s", iKey, op, jKey))
				g.pop()
				g.writeln("})")
			}
		}
	}
}

// --- Map Collection Methods ---

// emitMapCollectionChainVar emits a fused loop for a map collection chain.
func (g *Generator) emitMapCollectionChainVar(varName string, chain *collectionChain) {
	lastStep := chain.steps[len(chain.steps)-1]

	switch lastStep.method {
	case "Any", "All":
		g.emitMapAnyAllChain(varName, chain)
		return
	case "Count":
		g.emitMapCountChain(varName, chain)
		return
	case "Aggregate":
		g.emitMapAggregateChain(varName, chain)
		return
	case "Select":
		// Map Select returns List — use list accumulation
		g.emitMapSelectToListChain(varName, chain)
		return
	}

	// Map-producing chain: Where, SelectValues, SelectKeys
	source := g.emitExpr(chain.source)
	keyType, valType := g.inferMapKeyValTypes(chain.source)
	keyVar := fmt.Sprintf("_k%d", g.tmpCounter)
	g.tmpCounter++
	valVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("%s := make(map[%s]%s)", varName, keyType, valType))
	g.writeln(fmt.Sprintf("for %s, %s := range %s {", keyVar, valVar, source))
	g.push()
	g.writeln(fmt.Sprintf("_ = %s", keyVar))
	g.writeln(fmt.Sprintf("_ = %s", valVar))

	currentKey := keyVar
	currentVal := valVar
	var blocks []openBlock

	for _, step := range chain.steps {
		switch step.method {
		case "Where":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) >= 2 {
					cond := g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, currentKey, lambda.Params[1].Name, currentVal)
					g.writeln(fmt.Sprintf("if %s {", cond))
					g.push()
					blocks = append(blocks, openBlock{"if"})
				}
			}
		case "SelectValues":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) >= 2 {
					newVal := fmt.Sprintf("_v%d", g.tmpCounter)
					g.tmpCounter++
					body := g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, currentKey, lambda.Params[1].Name, currentVal)
					g.writeln(fmt.Sprintf("%s := %s", newVal, body))
					currentVal = newVal
				}
			}
		case "SelectKeys":
			if len(step.args) > 0 {
				lambda := g.extractLambda(step.args[0])
				if lambda != nil && len(lambda.Params) >= 2 {
					newKey := fmt.Sprintf("_k%d", g.tmpCounter)
					g.tmpCounter++
					body := g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, currentKey, lambda.Params[1].Name, currentVal)
					g.writeln(fmt.Sprintf("%s := %s", newKey, body))
					currentKey = newKey
				}
			}
		}
	}

	g.writeln(fmt.Sprintf("%s[%s] = %s", varName, currentKey, currentVal))

	g.closeBlocks(blocks)
	g.pop()
	g.writeln("}")
}

// emitMapCollectionChainStmt emits a map ForEach chain.
func (g *Generator) emitMapCollectionChainStmt(chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	keyVar := fmt.Sprintf("_k%d", g.tmpCounter)
	g.tmpCounter++
	valVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("for %s, %s := range %s {", keyVar, valVar, source))
	g.push()
	g.writeln(fmt.Sprintf("_ = %s", keyVar))
	g.writeln(fmt.Sprintf("_ = %s", valVar))

	currentKey := keyVar
	currentVal := valVar
	var blocks []openBlock

	// Process intermediate Where steps
	for _, step := range chain.steps[:len(chain.steps)-1] {
		if step.method == "Where" && len(step.args) > 0 {
			lambda := g.extractLambda(step.args[0])
			if lambda != nil && len(lambda.Params) >= 2 {
				cond := g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, currentKey, lambda.Params[1].Name, currentVal)
				g.writeln(fmt.Sprintf("if %s {", cond))
				g.push()
				blocks = append(blocks, openBlock{"if"})
			}
		}
	}

	// ForEach body
	lambda := g.extractLambda(lastStep.args[0])
	if lambda != nil && len(lambda.Params) >= 2 {
		if lambda.Expr != nil {
			body := g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, currentKey, lambda.Params[1].Name, currentVal)
			g.writeln(body)
		} else if lambda.Body != nil {
			g.writeln(fmt.Sprintf("%s := %s", lambda.Params[0].Name, currentKey))
			g.writeln(fmt.Sprintf("_ = %s", lambda.Params[0].Name))
			g.writeln(fmt.Sprintf("%s := %s", lambda.Params[1].Name, currentVal))
			g.writeln(fmt.Sprintf("_ = %s", lambda.Params[1].Name))
			g.emitBlock(lambda.Body)
		}
	}

	g.closeBlocks(blocks)
	g.pop()
	g.writeln("}")
}

// emitMapAnyAllChain emits Any/All for map chains.
func (g *Generator) emitMapAnyAllChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]
	isAny := lastStep.method == "Any"

	keyVar := fmt.Sprintf("_k%d", g.tmpCounter)
	g.tmpCounter++
	valVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	if isAny {
		g.writeln(fmt.Sprintf("%s := false", varName))
	} else {
		g.writeln(fmt.Sprintf("%s := true", varName))
	}

	g.writeln(fmt.Sprintf("for %s, %s := range %s {", keyVar, valVar, source))
	g.push()

	// Process intermediate Where
	var blocks []openBlock
	for _, step := range chain.steps[:len(chain.steps)-1] {
		if step.method == "Where" && len(step.args) > 0 {
			lambda := g.extractLambda(step.args[0])
			if lambda != nil && len(lambda.Params) >= 2 {
				cond := g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, keyVar, lambda.Params[1].Name, valVar)
				g.writeln(fmt.Sprintf("if %s {", cond))
				g.push()
				blocks = append(blocks, openBlock{"if"})
			}
		}
	}

	// Terminal
	lambda := g.extractLambda(lastStep.args[0])
	if lambda != nil && len(lambda.Params) >= 2 {
		cond := g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, keyVar, lambda.Params[1].Name, valVar)
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

	// suppress unused var warning for key in Any/All
	g.writeln(fmt.Sprintf("_ = %s", keyVar))

	g.closeBlocks(blocks)
	g.pop()
	g.writeln("}")
}

// emitMapCountChain emits Count for map chains.
func (g *Generator) emitMapCountChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)

	keyVar := fmt.Sprintf("_k%d", g.tmpCounter)
	g.tmpCounter++
	valVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("%s := 0", varName))
	g.writeln(fmt.Sprintf("for %s, %s := range %s {", keyVar, valVar, source))
	g.push()

	var blocks []openBlock
	for _, step := range chain.steps[:len(chain.steps)-1] {
		if step.method == "Where" && len(step.args) > 0 {
			lambda := g.extractLambda(step.args[0])
			if lambda != nil && len(lambda.Params) >= 2 {
				cond := g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, keyVar, lambda.Params[1].Name, valVar)
				g.writeln(fmt.Sprintf("if %s {", cond))
				g.push()
				blocks = append(blocks, openBlock{"if"})
			}
		}
	}

	g.writeln(fmt.Sprintf("%s++", varName))

	// suppress unused var warnings
	g.writeln(fmt.Sprintf("_ = %s", keyVar))
	g.writeln(fmt.Sprintf("_ = %s", valVar))

	g.closeBlocks(blocks)
	g.pop()
	g.writeln("}")
}

// emitMapAggregateChain emits Aggregate for map chains with (acc, k, v) lambda.
func (g *Generator) emitMapAggregateChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	if len(lastStep.args) < 2 {
		return
	}
	seed := g.emitExpr(lastStep.args[0])
	lambda := g.extractLambda(lastStep.args[1])
	if lambda == nil || len(lambda.Params) < 3 {
		return
	}

	keyVar := fmt.Sprintf("_k%d", g.tmpCounter)
	g.tmpCounter++
	valVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("%s := %s", varName, seed))
	g.writeln(fmt.Sprintf("for %s, %s := range %s {", keyVar, valVar, source))
	g.push()
	g.writeln(fmt.Sprintf("_ = %s", keyVar))
	g.writeln(fmt.Sprintf("_ = %s", valVar))

	// Process intermediate Where
	var blocks []openBlock
	for _, step := range chain.steps[:len(chain.steps)-1] {
		if step.method == "Where" && len(step.args) > 0 {
			wl := g.extractLambda(step.args[0])
			if wl != nil && len(wl.Params) >= 2 {
				cond := g.emitExprSubst2(wl.Expr, wl.Params[0].Name, keyVar, wl.Params[1].Name, valVar)
				g.writeln(fmt.Sprintf("if %s {", cond))
				g.push()
				blocks = append(blocks, openBlock{"if"})
			}
		}
	}

	// 3-param substitution: (acc, k, v)
	reduced := g.emitExprSubst3(lambda.Expr,
		lambda.Params[0].Name, varName,
		lambda.Params[1].Name, keyVar,
		lambda.Params[2].Name, valVar)
	g.writeln(fmt.Sprintf("%s = %s", varName, reduced))

	g.closeBlocks(blocks)
	g.pop()
	g.writeln("}")
}

// emitMapSelectToListChain emits Select on a map that returns a List.
func (g *Generator) emitMapSelectToListChain(varName string, chain *collectionChain) {
	source := g.emitExpr(chain.source)
	lastStep := chain.steps[len(chain.steps)-1]

	keyVar := fmt.Sprintf("_k%d", g.tmpCounter)
	g.tmpCounter++
	valVar := fmt.Sprintf("_v%d", g.tmpCounter)
	g.tmpCounter++

	g.writeln(fmt.Sprintf("var %s []interface{}", varName))
	g.writeln(fmt.Sprintf("for %s, %s := range %s {", keyVar, valVar, source))
	g.push()
	g.writeln(fmt.Sprintf("_ = %s", keyVar))
	g.writeln(fmt.Sprintf("_ = %s", valVar))

	var blocks []openBlock
	for _, step := range chain.steps[:len(chain.steps)-1] {
		if step.method == "Where" && len(step.args) > 0 {
			lambda := g.extractLambda(step.args[0])
			if lambda != nil && len(lambda.Params) >= 2 {
				cond := g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, keyVar, lambda.Params[1].Name, valVar)
				g.writeln(fmt.Sprintf("if %s {", cond))
				g.push()
				blocks = append(blocks, openBlock{"if"})
			}
		}
	}

	lambda := g.extractLambda(lastStep.args[0])
	if lambda != nil && len(lambda.Params) >= 2 {
		body := g.emitExprSubst2(lambda.Expr, lambda.Params[0].Name, keyVar, lambda.Params[1].Name, valVar)
		g.writeln(fmt.Sprintf("%s = append(%s, %s)", varName, varName, body))
	}

	g.closeBlocks(blocks)
	g.pop()
	g.writeln("}")
}

// emitExprSubst3 emits an expression with three variable substitutions.
func (g *Generator) emitExprSubst3(expr parser.Expr, n1, r1, n2, r2, n3, r3 string) string {
	switch e := expr.(type) {
	case *parser.Ident:
		if e.Name == n1 {
			return r1
		}
		if e.Name == n2 {
			return r2
		}
		if e.Name == n3 {
			return r3
		}
		return g.emitExpr(expr)
	case *parser.BinaryExpr:
		left := g.emitExprSubst3(e.Left, n1, r1, n2, r2, n3, r3)
		right := g.emitExprSubst3(e.Right, n1, r1, n2, r2, n3, r3)
		return fmt.Sprintf("(%s %s %s)", left, e.Op, right)
	case *parser.UnaryExpr:
		operand := g.emitExprSubst3(e.Operand, n1, r1, n2, r2, n3, r3)
		return fmt.Sprintf("(%s%s)", e.Op, operand)
	case *parser.SelectorExpr:
		obj := g.emitExprSubst3(e.Object, n1, r1, n2, r2, n3, r3)
		return fmt.Sprintf("%s.%s", obj, capitalize(e.Field))
	case *parser.CallExpr:
		callee := g.emitExprSubst3(e.Callee, n1, r1, n2, r2, n3, r3)
		var args []string
		for _, arg := range e.Args {
			args = append(args, g.emitExprSubst3(arg, n1, r1, n2, r2, n3, r3))
		}
		return fmt.Sprintf("%s(%s)", callee, strings.Join(args, ", "))
	case *parser.IndexExpr:
		obj := g.emitExprSubst3(e.Object, n1, r1, n2, r2, n3, r3)
		idx := g.emitExprSubst3(e.Index, n1, r1, n2, r2, n3, r3)
		return fmt.Sprintf("%s[%s]", obj, idx)
	default:
		return g.emitExpr(expr)
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
