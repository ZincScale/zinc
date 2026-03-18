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

// Dunder method name mapping: Zinc clean names → Python dunder methods
var dunders = map[string]string{
	"init":     "__init__",
	"str":      "__str__",
	"repr":     "__repr__",
	"eq":       "__eq__",
	"hash":     "__hash__",
	"len":      "__len__",
	"iter":     "__iter__",
	"next":     "__next__",
	"contains": "__contains__",
	"get":      "__getitem__",
	"set":      "__setitem__",
	"del":      "__delitem__",
	"add":      "__add__",
	"sub":      "__sub__",
	"mul":      "__mul__",
	"lt":       "__lt__",
	"le":       "__le__",
	"enter":    "__enter__",
	"exit":     "__exit__",
	"call":     "__call__",
}

// GenerateV2 produces Python source from a Zinc v2 AST (end-block syntax, script mode).
func (g *Generator) GenerateV2(prog *parser.Program) string {
	// First pass: collect class names
	for _, d := range prog.Decls {
		switch cd := d.(type) {
		case *parser.ClassDecl:
			g.classNames[cd.Name] = true
		case *parser.DataClassDecl:
			g.classNames[cd.Name] = true
		}
	}

	// Emit imports — consolidate from-imports from same module
	g.emitV2Imports(prog.Imports)
	if len(prog.Imports) > 0 {
		g.write("\n")
	}

	// Emit declarations
	for _, d := range prog.Decls {
		g.emitV2Decl(d)
		g.write("\n")
	}

	// Emit top-level statements (script mode)
	if len(prog.Stmts) > 0 {
		for _, s := range prog.Stmts {
			g.emitV2Stmt(s)
		}
	}

	// Prepend auto-detected imports and runtime
	var out strings.Builder
	if g.needsResultRuntime {
		out.WriteString(ZincResultRuntime)
		out.WriteString("\n")
	}
	if len(g.neededImports) > 0 {
		for imp := range g.neededImports {
			out.WriteString(fmt.Sprintf("import %s\n", imp))
		}
		out.WriteString("\n")
	}
	out.WriteString(g.buf.String())
	return out.String()
}

// --- Imports -----------------------------------------------------------------

// emitV2Imports consolidates from-imports from the same module onto one line.
func (g *Generator) emitV2Imports(imports []*parser.ImportDecl) {
	// Group from-imports by module
	type fromName struct {
		name  string
		alias string
	}
	fromGroups := make(map[string][]fromName) // module → names
	var fromOrder []string                     // preserve order
	var regularImports []*parser.ImportDecl

	for _, imp := range imports {
		if strings.HasPrefix(imp.Path, "from:") {
			parts := strings.SplitN(imp.Path, ":", 3)
			if len(parts) == 3 {
				module := parts[1]
				if _, seen := fromGroups[module]; !seen {
					fromOrder = append(fromOrder, module)
				}
				fromGroups[module] = append(fromGroups[module], fromName{name: parts[2], alias: imp.Alias})
			}
		} else {
			regularImports = append(regularImports, imp)
		}
	}

	// Emit regular imports
	for _, imp := range regularImports {
		if imp.Alias != "" {
			g.writeln(fmt.Sprintf("import %s as %s", imp.Path, imp.Alias))
		} else {
			g.writeln(fmt.Sprintf("import %s", imp.Path))
		}
	}

	// Emit consolidated from-imports
	for _, module := range fromOrder {
		names := fromGroups[module]
		var parts []string
		for _, n := range names {
			if n.alias != "" {
				parts = append(parts, fmt.Sprintf("%s as %s", n.name, n.alias))
			} else {
				parts = append(parts, n.name)
			}
		}
		g.writeln(fmt.Sprintf("from %s import %s", module, strings.Join(parts, ", ")))
	}
}

// --- Declarations ------------------------------------------------------------

func (g *Generator) emitV2Decl(d parser.TopLevelDecl) {
	switch d := d.(type) {
	case *parser.FnDecl:
		g.emitV2FnDecl(d)
	case *parser.ClassDecl:
		g.emitV2ClassDecl(d)
	case *parser.DataClassDecl:
		g.emitV2DataClassDecl(d)
	case *parser.EnumDecl:
		g.emitEnumDecl(d)
	case *parser.ConstDecl:
		g.emitConstDecl(d)
	}
}

func (g *Generator) emitV2FnDecl(d *parser.FnDecl) {
	// Emit decorators
	for _, a := range d.Annotations {
		if len(a.Args) > 0 {
			g.writeln(fmt.Sprintf("@%s(%s)", a.Name, strings.Join(a.Args, ", ")))
		} else {
			g.writeln(fmt.Sprintf("@%s", a.Name))
		}
	}

	// Check if this function returns Result[T]
	isResultFn := g.isResultType(d.ReturnType)
	if isResultFn {
		g.needsResultRuntime = true
	}

	params := g.v2FormatParams(d.Params)
	retAnnotation := ""
	if d.ReturnType != nil {
		retAnnotation = " -> " + g.v2FormatType(d.ReturnType)
	}
	g.writeln(fmt.Sprintf("def %s(%s)%s:", d.Name, params, retAnnotation))
	g.push()

	prevResultFn := g.inResultFn
	g.inResultFn = isResultFn

	if d.Body != nil && len(d.Body.Stmts) > 0 {
		g.emitV2Block(d.Body)
	} else {
		g.writeln("pass")
	}

	g.inResultFn = prevResultFn
	g.pop()
}

// isErrCall checks if an expression is a call to Err().
func (g *Generator) isErrCall(e parser.Expr) bool {
	call, ok := e.(*parser.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Callee.(*parser.Ident)
	return ok && ident.Name == "Err"
}

// isResultType checks if a type expression is Result[T].
func (g *Generator) isResultType(t parser.TypeExpr) bool {
	if gt, ok := t.(*parser.GenericType); ok {
		return gt.Name == "Result"
	}
	return false
}

func (g *Generator) emitV2ClassDecl(d *parser.ClassDecl) {
	if len(d.Parents) > 0 {
		g.writeln(fmt.Sprintf("class %s(%s):", d.Name, strings.Join(d.Parents, ", ")))
	} else {
		g.writeln(fmt.Sprintf("class %s:", d.Name))
	}
	g.push()

	// Track fields for auto-self injection
	g.currentClassFields = make(map[string]bool)
	for _, f := range d.Fields {
		g.currentClassFields[f.Name] = true
	}

	// Auto-generate __init__ from fields
	if len(d.Fields) > 0 {
		var initParams []string
		for _, f := range d.Fields {
			param := f.Name
			if f.Type != nil {
				param += ": " + g.v2FormatType(f.Type)
			}
			if f.Default != nil {
				param += " = " + g.emitV2Expr(f.Default)
			}
			initParams = append(initParams, param)
		}
		g.writeln(fmt.Sprintf("def __init__(self, %s):", strings.Join(initParams, ", ")))
		g.push()
		for _, f := range d.Fields {
			g.writeln(fmt.Sprintf("self.%s = %s", f.Name, f.Name))
		}
		g.pop()
	}

	// Methods
	for _, m := range d.Methods {
		if len(d.Fields) > 0 || m != d.Methods[0] {
			g.write("\n")
		}
		g.emitV2MethodDecl(m)
	}

	if len(d.Fields) == 0 && len(d.Methods) == 0 {
		g.writeln("pass")
	}

	g.currentClassFields = nil
	g.pop()
}

func (g *Generator) emitV2MethodDecl(m *parser.MethodDecl) {
	// Emit decorators
	isStatic := false
	isClassMethod := false
	for _, a := range m.Annotations {
		if len(a.Args) > 0 {
			g.writeln(fmt.Sprintf("@%s(%s)", a.Name, strings.Join(a.Args, ", ")))
		} else {
			g.writeln(fmt.Sprintf("@%s", a.Name))
		}
		if a.Name == "staticmethod" {
			isStatic = true
		}
		if a.Name == "classmethod" {
			isClassMethod = true
		}
	}

	pyName := m.Name
	// Only map to dunder if it's a regular instance method (not static/classmethod)
	if !isStatic && !isClassMethod {
		if dn, ok := dunders[m.Name]; ok {
			pyName = dn
		}
	}

	params := g.v2FormatParams(m.Params)
	if isStatic {
		// No self/cls param
	} else if isClassMethod {
		if params != "" {
			params = "cls, " + params
		} else {
			params = "cls"
		}
	} else {
		if params != "" {
			params = "self, " + params
		} else {
			params = "self"
		}
	}

	retAnnotation := ""
	if m.ReturnType != nil {
		retAnnotation = " -> " + g.v2FormatType(m.ReturnType)
	}

	g.writeln(fmt.Sprintf("def %s(%s)%s:", pyName, params, retAnnotation))
	g.push()
	if m.Body != nil && len(m.Body.Stmts) > 0 {
		g.emitV2Block(m.Body)
	} else {
		g.writeln("pass")
	}
	g.pop()
}

func (g *Generator) emitV2DataClassDecl(d *parser.DataClassDecl) {
	g.neededImports["dataclasses"] = true
	g.writeln("@dataclasses.dataclass")
	g.writeln(fmt.Sprintf("class %s:", d.Name))
	g.push()

	for _, f := range d.Params {
		annotation := ""
		if f.Type != nil {
			annotation = ": " + g.v2FormatType(f.Type)
		}
		if f.Default != nil {
			g.writeln(fmt.Sprintf("%s%s = %s", f.Name, annotation, g.emitV2Expr(f.Default)))
		} else {
			g.writeln(fmt.Sprintf("%s%s", f.Name, annotation))
		}
	}

	// Methods
	for _, m := range d.Methods {
		g.write("\n")
		g.emitV2MethodDecl(m)
	}

	if len(d.Params) == 0 && len(d.Methods) == 0 {
		g.writeln("pass")
	}

	g.pop()
}

// --- Statements --------------------------------------------------------------

func (g *Generator) emitV2Block(block *parser.BlockStmt) {
	if len(block.Stmts) == 0 {
		g.writeln("pass")
		return
	}
	for _, s := range block.Stmts {
		g.emitV2Stmt(s)
	}
}

func (g *Generator) emitV2Stmt(s parser.Stmt) {
	switch s := s.(type) {
	case *parser.VarStmt:
		g.emitV2VarStmt(s)
	case *parser.TupleVarStmt:
		g.writeln(fmt.Sprintf("%s = %s", strings.Join(s.Names, ", "), g.emitV2Expr(s.Value)))
	case *parser.AssignStmt:
		g.writeln(fmt.Sprintf("%s %s %s", g.emitV2Expr(s.Target), s.Op, g.emitV2Expr(s.Value)))
	case *parser.ReturnStmt:
		if s.Value != nil {
			val := g.emitV2Expr(s.Value)
			// In Result-returning functions, wrap bare returns in Ok()
			// but don't double-wrap Err() calls
			if g.inResultFn && !g.isErrCall(s.Value) {
				g.writeln(fmt.Sprintf("return Ok(%s)", val))
			} else {
				g.writeln(fmt.Sprintf("return %s", val))
			}
		} else {
			g.writeln("return")
		}
	case *parser.IfStmt:
		g.emitV2IfStmt(s)
	case *parser.ForStmt:
		g.emitV2ForStmt(s)
	case *parser.WhileStmt:
		g.writeln(fmt.Sprintf("while %s:", g.emitV2Expr(s.Cond)))
		g.push()
		g.emitV2Block(s.Body)
		g.pop()
	case *parser.PrintStmt:
		g.writeln(fmt.Sprintf("print(%s)", g.emitV2Expr(s.Value)))
	case *parser.ExprStmt:
		g.writeln(g.emitV2Expr(s.Expr))
	case *parser.BreakStmt:
		g.writeln("break")
	case *parser.ContinueStmt:
		g.writeln("continue")
	case *parser.MatchStmt:
		g.emitV2MatchStmt(s)
	case *parser.BlockStmt:
		g.emitV2Block(s)
	case *parser.TryStmt:
		g.emitV2TryStmt(s)
	case *parser.RaiseStmt:
		if s.From != nil {
			g.writeln(fmt.Sprintf("raise %s from %s", g.emitV2Expr(s.Value), g.emitV2Expr(s.From)))
		} else {
			g.writeln(fmt.Sprintf("raise %s", g.emitV2Expr(s.Value)))
		}
	case *parser.DelStmt:
		g.writeln(fmt.Sprintf("del %s", g.emitV2Expr(s.Target)))
	case *parser.AssertStmt:
		if s.Message != nil {
			g.writeln(fmt.Sprintf("assert %s, %s", g.emitV2Expr(s.Cond), g.emitV2Expr(s.Message)))
		} else {
			g.writeln(fmt.Sprintf("assert %s", g.emitV2Expr(s.Cond)))
		}
	case *parser.FnDecl:
		// Nested function definition
		g.emitV2FnDecl(s)
	case *parser.WithStmt:
		var resources []string
		for _, r := range s.Resources {
			resources = append(resources, fmt.Sprintf("%s as %s", g.emitV2Expr(r.Value), r.Name))
		}
		g.writeln(fmt.Sprintf("with %s:", strings.Join(resources, ", ")))
		g.push()
		g.emitV2Block(s.Body)
		g.pop()
	}
}

func (g *Generator) emitV2VarStmt(s *parser.VarStmt) {
	if s.Value != nil && s.OrHandler != nil {
		// Err {} handler: var x = call() Err { handler }
		g.needsResultRuntime = true
		val := g.emitV2Expr(s.Value)
		g.writeln(fmt.Sprintf("_result = %s", val))

		// Check if handler is a single expression (default value)
		if len(s.OrHandler.Body.Stmts) == 1 {
			if es, ok := s.OrHandler.Body.Stmts[0].(*parser.ExprStmt); ok {
				// Single expression default: var x = call() Err { 0 }
				def := g.emitV2Expr(es.Expr)
				g.writeln(fmt.Sprintf("%s = _result.value if _result.is_ok() else %s", s.Name, def))
				return
			}
		}

		// Multi-statement handler: var x = call() Err { print("bad"); return }
		g.writeln("if _result.is_err():")
		g.push()
		g.writeln("err = _result.error")
		g.emitV2Block(s.OrHandler.Body)
		g.pop()
		g.writeln("else:")
		g.push()
		g.writeln(fmt.Sprintf("%s = _result.value", s.Name))
		g.pop()
		return
	}

	if s.Value != nil {
		val := g.emitV2Expr(s.Value)
		if s.Type != nil {
			g.writeln(fmt.Sprintf("%s: %s = %s", s.Name, g.v2FormatType(s.Type), val))
		} else {
			g.writeln(fmt.Sprintf("%s = %s", s.Name, val))
		}
	} else if s.Type != nil {
		g.writeln(fmt.Sprintf("%s: %s", s.Name, g.v2FormatType(s.Type)))
	} else {
		g.writeln(fmt.Sprintf("%s = None", s.Name))
	}
}

func (g *Generator) emitV2IfStmt(s *parser.IfStmt) {
	g.writeln(fmt.Sprintf("if %s:", g.emitV2Expr(s.Cond)))
	g.push()
	g.emitV2Block(s.Then)
	g.pop()
	if s.ElseStmt != nil {
		if elseIf, ok := s.ElseStmt.(*parser.IfStmt); ok {
			g.write(strings.Repeat("    ", g.indent))
			g.write(fmt.Sprintf("elif %s:\n", g.emitV2Expr(elseIf.Cond)))
			g.push()
			g.emitV2Block(elseIf.Then)
			g.pop()
			if elseIf.ElseStmt != nil {
				g.emitV2ElseChain(elseIf.ElseStmt)
			}
		} else if block, ok := s.ElseStmt.(*parser.BlockStmt); ok {
			g.writeln("else:")
			g.push()
			g.emitV2Block(block)
			g.pop()
		}
	}
}

func (g *Generator) emitV2ElseChain(s parser.Stmt) {
	if elseIf, ok := s.(*parser.IfStmt); ok {
		g.write(strings.Repeat("    ", g.indent))
		g.write(fmt.Sprintf("elif %s:\n", g.emitV2Expr(elseIf.Cond)))
		g.push()
		g.emitV2Block(elseIf.Then)
		g.pop()
		if elseIf.ElseStmt != nil {
			g.emitV2ElseChain(elseIf.ElseStmt)
		}
	} else if block, ok := s.(*parser.BlockStmt); ok {
		g.writeln("else:")
		g.push()
		g.emitV2Block(block)
		g.pop()
	}
}

func (g *Generator) emitV2ForStmt(s *parser.ForStmt) {
	if s.IsRange {
		collection := g.emitV2Expr(s.Range)
		if s.IndexVar != "" {
			g.writeln(fmt.Sprintf("for %s, %s in enumerate(%s):", s.IndexVar, s.Item, collection))
		} else {
			g.writeln(fmt.Sprintf("for %s in %s:", s.Item, collection))
		}
		g.push()
		g.emitV2Block(s.Body)
		g.pop()
	}
}

func (g *Generator) emitV2MatchStmt(s *parser.MatchStmt) {
	g.writeln(fmt.Sprintf("match %s:", g.emitV2Expr(s.Subject)))
	g.push()
	for _, c := range s.Cases {
		if c.Pattern == nil {
			g.writeln("case _:")
		} else {
			g.writeln(fmt.Sprintf("case %s:", g.emitV2Expr(c.Pattern)))
		}
		g.push()
		g.emitV2Block(c.Body)
		g.pop()
	}
	g.pop()
}

func (g *Generator) emitV2TryStmt(s *parser.TryStmt) {
	g.writeln("try:")
	g.push()
	g.emitV2Block(s.Body)
	g.pop()

	if s.CatchType != "" {
		g.writeln(fmt.Sprintf("except %s as %s:", s.CatchType, s.CatchName))
	} else if s.CatchName != "" {
		g.writeln(fmt.Sprintf("except Exception as %s:", s.CatchName))
	} else {
		g.writeln("except Exception:")
	}
	g.push()
	g.emitV2Block(s.CatchBody)
	g.pop()
}

// --- Expressions -------------------------------------------------------------

func (g *Generator) emitV2Expr(e parser.Expr) string {
	switch e := e.(type) {
	case *parser.IntLit:
		return e.Value
	case *parser.FloatLit:
		return e.Value
	case *parser.StringLit:
		if strings.Contains(e.Value, "\n") {
			return fmt.Sprintf(`"""%s"""`, e.Value)
		}
		// Use single quotes if the string contains double quotes
		if strings.Contains(e.Value, `"`) {
			return fmt.Sprintf(`'%s'`, e.Value)
		}
		return fmt.Sprintf(`"%s"`, e.Value)
	case *parser.RawStringLit:
		return fmt.Sprintf(`r"%s"`, e.Value)
	case *parser.BoolLit:
		if e.Value {
			return "True"
		}
		return "False"
	case *parser.NullLit:
		return "None"
	case *parser.Ident:
		// Auto-inject self. for class field access
		if g.currentClassFields != nil && g.currentClassFields[e.Name] {
			return "self." + e.Name
		}
		return e.Name
	case *parser.BinaryExpr:
		return g.emitV2BinaryExpr(e)
	case *parser.UnaryExpr:
		if e.Op == "!" {
			return fmt.Sprintf("not %s", g.emitV2Expr(e.Operand))
		}
		return fmt.Sprintf("(%s%s)", e.Op, g.emitV2Expr(e.Operand))
	case *parser.CallExpr:
		return g.emitV2CallExpr(e)
	case *parser.SelectorExpr:
		return fmt.Sprintf("%s.%s", g.emitV2Expr(e.Object), e.Field)
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", g.emitV2Expr(e.Object), g.emitV2Expr(e.Index))
	case *parser.SliceExpr:
		obj := g.emitV2Expr(e.Object)
		low, high := "", ""
		if e.Low != nil {
			low = g.emitV2Expr(e.Low)
		}
		if e.High != nil {
			high = g.emitV2Expr(e.High)
		}
		return fmt.Sprintf("%s[%s:%s]", obj, low, high)
	case *parser.ListLit:
		var elems []string
		for _, el := range e.Elements {
			elems = append(elems, g.emitV2Expr(el))
		}
		return fmt.Sprintf("[%s]", strings.Join(elems, ", "))
	case *parser.MapLit:
		var entries []string
		for i, k := range e.Keys {
			entries = append(entries, fmt.Sprintf("%s: %s", g.emitV2Expr(k), g.emitV2Expr(e.Values[i])))
		}
		return fmt.Sprintf("{%s}", strings.Join(entries, ", "))
	case *parser.LambdaExpr:
		return g.emitV2Lambda(e)
	case *parser.StringInterpLit:
		return g.emitV2InterpString(e)
	case *parser.IfExpr:
		return g.emitV2IfExpr(e)
	case *parser.ComprehensionExpr:
		return g.emitV2Comprehension(e)
	case *parser.DictComprehensionExpr:
		return g.emitV2DictComprehension(e)
	case *parser.SpreadExpr:
		return fmt.Sprintf("*%s", g.emitV2Expr(e.Expr))
	default:
		return "None  # unsupported expr"
	}
}

func (g *Generator) emitV2BinaryExpr(e *parser.BinaryExpr) string {
	left := g.emitV2Expr(e.Left)
	right := g.emitV2Expr(e.Right)
	op := e.Op
	switch op {
	case "&&":
		op = "and"
	case "||":
		op = "or"
	// "not in", "is not", "is", "in" pass through directly to Python
	}
	return fmt.Sprintf("(%s %s %s)", left, op, right)
}

// generatorFriendly lists builtins where a generator is better than a list.
// These consume items lazily — no need to build the full list in memory.
var generatorFriendly = map[string]bool{
	"sum": true, "any": true, "all": true, "min": true, "max": true,
	"len": true, "sorted": true, "list": true, "set": true, "tuple": true,
	"dict": true, "enumerate": true, "zip": true, "next": true,
}

func (g *Generator) emitV2CallExpr(e *parser.CallExpr) string {
	callee := g.emitV2Expr(e.Callee)

	// Handle collection methods: .filter(), .map(), etc.
	if sel, ok := e.Callee.(*parser.SelectorExpr); ok {
		if result, handled := g.emitV2CollectionMethod(sel, e); handled {
			return result
		}
	}

	// Decide: comprehensions inside generator-friendly builtins → generator (no brackets)
	useGenerator := generatorFriendly[callee]

	var parts []string
	for _, a := range e.Args {
		if comp, ok := a.(*parser.ComprehensionExpr); ok {
			parts = append(parts, g.emitV2ComprehensionInner(comp, !useGenerator))
		} else {
			parts = append(parts, g.emitV2Expr(a))
		}
	}
	for _, na := range e.NamedArgs {
		parts = append(parts, fmt.Sprintf("%s=%s", na.Name, g.emitV2Expr(na.Value)))
	}
	return fmt.Sprintf("%s(%s)", callee, strings.Join(parts, ", "))
}

// emitV2CollectionMethod handles Zinc v2 collection chain methods → Python.
func (g *Generator) emitV2CollectionMethod(sel *parser.SelectorExpr, call *parser.CallExpr) (string, bool) {
	obj := g.emitV2Expr(sel.Object)
	switch sel.Field {
	case "filter":
		if len(call.Args) == 1 {
			// If the predicate is a lambda, inline its body directly
			if lam, ok := call.Args[0].(*parser.LambdaExpr); ok && lam.Expr != nil && len(lam.Params) == 1 {
				paramName := lam.Params[0].Name
				body := g.emitV2Expr(lam.Expr)
				return fmt.Sprintf("[%s for %s in %s if %s]", paramName, paramName, obj, body), true
			}
			pred := g.emitV2Expr(call.Args[0])
			return fmt.Sprintf("[x for x in %s if %s(x)]", obj, pred), true
		}
	case "map":
		if len(call.Args) == 1 {
			// If the mapper is a lambda, inline its body directly
			if lam, ok := call.Args[0].(*parser.LambdaExpr); ok && lam.Expr != nil && len(lam.Params) == 1 {
				paramName := lam.Params[0].Name
				body := g.emitV2Expr(lam.Expr)
				return fmt.Sprintf("[%s for %s in %s]", body, paramName, obj), true
			}
			fn := g.emitV2Expr(call.Args[0])
			return fmt.Sprintf("[%s(x) for x in %s]", fn, obj), true
		}
	case "sum":
		return fmt.Sprintf("sum(%s)", obj), true
	case "min":
		return fmt.Sprintf("min(%s)", obj), true
	case "max":
		return fmt.Sprintf("max(%s)", obj), true
	case "sort":
		return fmt.Sprintf("sorted(%s)", obj), true
	case "sort_by":
		if len(call.Args) == 1 {
			key := g.emitV2Expr(call.Args[0])
			reverse := ""
			for _, na := range call.NamedArgs {
				if na.Name == "reverse" {
					reverse = fmt.Sprintf(", reverse=%s", g.emitV2Expr(na.Value))
				}
			}
			return fmt.Sprintf("sorted(%s, key=%s%s)", obj, key, reverse), true
		}
	case "take":
		if len(call.Args) == 1 {
			return fmt.Sprintf("%s[:%s]", obj, g.emitV2Expr(call.Args[0])), true
		}
	case "skip":
		if len(call.Args) == 1 {
			return fmt.Sprintf("%s[%s:]", obj, g.emitV2Expr(call.Args[0])), true
		}
	case "first":
		if len(call.Args) == 1 {
			pred := g.emitV2Expr(call.Args[0])
			return fmt.Sprintf("next(x for x in %s if %s(x))", obj, pred), true
		}
		return fmt.Sprintf("%s[0]", obj), true
	case "any":
		if len(call.Args) == 1 {
			pred := g.emitV2Expr(call.Args[0])
			return fmt.Sprintf("any(%s(x) for x in %s)", pred, obj), true
		}
	case "all":
		if len(call.Args) == 1 {
			pred := g.emitV2Expr(call.Args[0])
			return fmt.Sprintf("all(%s(x) for x in %s)", pred, obj), true
		}
	case "distinct":
		return fmt.Sprintf("list(set(%s))", obj), true
	case "flat_map":
		if len(call.Args) == 1 {
			fn := g.emitV2Expr(call.Args[0])
			return fmt.Sprintf("[item for x in %s for item in %s(x)]", obj, fn), true
		}
	case "reduce":
		if len(call.Args) == 2 {
			g.neededImports["functools"] = true
			init := g.emitV2Expr(call.Args[0])
			fn := g.emitV2Expr(call.Args[1])
			return fmt.Sprintf("functools.reduce(%s, %s, %s)", fn, obj, init), true
		}
	case "group_by":
		if len(call.Args) == 1 {
			g.neededImports["itertools"] = true
			key := g.emitV2Expr(call.Args[0])
			return fmt.Sprintf("{k: list(v) for k, v in itertools.groupby(sorted(%s, key=%s), key=%s)}", obj, key, key), true
		}
	case "to_list":
		return fmt.Sprintf("list(%s)", obj), true
	case "to_dict":
		return fmt.Sprintf("dict(%s)", obj), true
	case "append":
		if len(call.Args) == 1 {
			return fmt.Sprintf("%s.append(%s)", obj, g.emitV2Expr(call.Args[0])), true
		}
	}
	return "", false
}

func (g *Generator) emitV2Lambda(e *parser.LambdaExpr) string {
	var params []string
	for _, p := range e.Params {
		params = append(params, p.Name)
	}
	paramStr := strings.Join(params, ", ")
	if e.Expr != nil {
		return fmt.Sprintf("lambda %s: %s", paramStr, g.emitV2Expr(e.Expr))
	}
	return fmt.Sprintf("lambda %s: None", paramStr)
}

func (g *Generator) emitV2InterpString(e *parser.StringInterpLit) string {
	var parts []string
	for _, p := range e.Parts {
		if sl, ok := p.(*parser.StringLit); ok {
			parts = append(parts, sl.Value)
		} else {
			parts = append(parts, fmt.Sprintf("{%s}", g.emitV2Expr(p)))
		}
	}
	return fmt.Sprintf(`f"%s"`, strings.Join(parts, ""))
}

func (g *Generator) emitV2Comprehension(e *parser.ComprehensionExpr) string {
	return g.emitV2ComprehensionInner(e, true)
}

func (g *Generator) emitV2ComprehensionInner(e *parser.ComprehensionExpr, withBrackets bool) string {
	expr := g.emitV2Expr(e.Expr)
	iter := g.emitV2Expr(e.Iter)
	body := fmt.Sprintf("%s for %s in %s", expr, e.Var, iter)
	if e.Cond != nil {
		body += " if " + g.emitV2Expr(e.Cond)
	}
	if withBrackets {
		return "[" + body + "]"
	}
	return body
}

func (g *Generator) emitV2DictComprehension(e *parser.DictComprehensionExpr) string {
	key := g.emitV2Expr(e.Key)
	val := g.emitV2Expr(e.Val)
	iter := g.emitV2Expr(e.Iter)
	body := fmt.Sprintf("%s: %s for %s in %s", key, val, e.Var, iter)
	if e.Cond != nil {
		body += " if " + g.emitV2Expr(e.Cond)
	}
	return "{" + body + "}"
}

func (g *Generator) emitV2IfExpr(e *parser.IfExpr) string {
	then := g.emitV2Expr(e.Then)
	cond := g.emitV2Expr(e.Cond)
	elseVal := g.emitV2Expr(e.Else)
	return fmt.Sprintf("%s if %s else %s", then, cond, elseVal)
}

// --- Type formatting ---------------------------------------------------------

func (g *Generator) v2FormatType(t parser.TypeExpr) string {
	switch t := t.(type) {
	case *parser.SimpleType:
		return t.Name
	case *parser.GenericType:
		var args []string
		for _, a := range t.TypeArgs {
			args = append(args, g.v2FormatType(a))
		}
		return fmt.Sprintf("%s[%s]", t.Name, strings.Join(args, ", "))
	case *parser.OptionalType:
		return fmt.Sprintf("Optional[%s]", g.v2FormatType(t.Inner))
	default:
		return "Any"
	}
}

func (g *Generator) v2FormatParams(params []*parser.ParamDecl) string {
	var parts []string
	for _, p := range params {
		var s string
		if strings.HasPrefix(p.Name, "**") {
			s = p.Name // **kwargs
		} else if p.Variadic {
			s = "*" + p.Name // *args
		} else {
			s = p.Name
		}
		if p.Type != nil {
			s += ": " + g.v2FormatType(p.Type)
		}
		if p.Default != nil {
			s += " = " + g.emitV2Expr(p.Default)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}
