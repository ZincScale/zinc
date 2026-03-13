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

// Package codegen_python transpiles Zinc AST to Python source code.
// This is a prototype backend focused on comparing codegen complexity
// with the Go backend, especially for collection method chains.
package codegen_python

import (
	"fmt"
	"strings"

	"zinc/internal/parser"
)

// Generator converts a Zinc AST to Python source code.
type Generator struct {
	buf           strings.Builder
	indent        int
	neededImports map[string]bool // e.g. "functools", "numpy"
	classNames    map[string]bool
	tmpCounter    int
	Strategy      CollectionStrategy // which backend for collection chains
}

// New creates a Python Generator.
func New() *Generator {
	return &Generator{
		neededImports: make(map[string]bool),
		classNames:    make(map[string]bool),
	}
}

// Generate produces Python source from a Zinc AST.
func (g *Generator) Generate(prog *parser.Program) string {
	// First pass: collect class names
	for _, d := range prog.Decls {
		if cd, ok := d.(*parser.ClassDecl); ok {
			g.classNames[cd.Name] = true
		}
	}

	// Emit declarations
	for _, d := range prog.Decls {
		g.emitDecl(d)
		g.write("\n")
	}

	// Emit main guard
	g.writeln(`if __name__ == "__main__":`)
	g.push()
	g.writeln("main()")
	g.pop()

	// Prepend imports
	var out strings.Builder
	if len(g.neededImports) > 0 {
		for imp := range g.neededImports {
			if imp == "numpy" {
				out.WriteString("import numpy as np\n")
			} else {
				out.WriteString(fmt.Sprintf("import %s\n", imp))
			}
		}
		out.WriteString("\n")
	}
	out.WriteString(g.buf.String())
	return out.String()
}

// --- Output helpers ----------------------------------------------------------

func (g *Generator) write(s string) {
	g.buf.WriteString(s)
}

func (g *Generator) writeln(s string) {
	g.buf.WriteString(strings.Repeat("    ", g.indent))
	g.buf.WriteString(s)
	g.buf.WriteString("\n")
}

func (g *Generator) push() { g.indent++ }
func (g *Generator) pop()  { g.indent-- }

// --- Declarations ------------------------------------------------------------

func (g *Generator) emitDecl(d parser.TopLevelDecl) {
	switch d := d.(type) {
	case *parser.FnDecl:
		g.emitFnDecl(d)
	case *parser.ClassDecl:
		g.emitClassDecl(d)
	case *parser.EnumDecl:
		g.emitEnumDecl(d)
	case *parser.ConstDecl:
		g.emitConstDecl(d)
	case *parser.InterfaceDecl:
		// Python uses duck typing — interfaces are just documentation
		g.writeln(fmt.Sprintf("# interface %s (duck-typed)", d.Name))
	}
}

func (g *Generator) emitFnDecl(d *parser.FnDecl) {
	params := g.formatParams(d.Params)
	g.writeln(fmt.Sprintf("def %s(%s):", d.Name, params))
	g.push()
	if d.Body != nil && len(d.Body.Stmts) > 0 {
		g.emitBlock(d.Body)
	} else {
		g.writeln("pass")
	}
	g.pop()
}

func (g *Generator) emitClassDecl(d *parser.ClassDecl) {
	parent := ""
	if len(d.Parents) > 0 {
		parent = fmt.Sprintf("(%s)", strings.Join(d.Parents, ", "))
	}
	g.writeln(fmt.Sprintf("class %s%s:", d.Name, parent))
	g.push()

	// Constructor
	if d.Ctor != nil {
		params := g.formatParams(d.Ctor.Params)
		if params != "" {
			params = "self, " + params
		} else {
			params = "self"
		}
		g.writeln(fmt.Sprintf("def __init__(%s):", params))
		g.push()
		// Super call
		if len(d.Ctor.SuperArgs) > 0 {
			var args []string
			for _, a := range d.Ctor.SuperArgs {
				args = append(args, g.emitExpr(a))
			}
			g.writeln(fmt.Sprintf("super().__init__(%s)", strings.Join(args, ", ")))
		}
		// Field defaults for fields not set in ctor
		for _, f := range d.Fields {
			if f.Default != nil {
				g.writeln(fmt.Sprintf("self.%s = %s", f.Name, g.emitExpr(f.Default)))
			}
		}
		if d.Ctor.Body != nil {
			g.emitBlock(d.Ctor.Body)
		}
		if len(d.Ctor.SuperArgs) == 0 && len(d.Fields) == 0 && (d.Ctor.Body == nil || len(d.Ctor.Body.Stmts) == 0) {
			g.writeln("pass")
		}
		g.pop()
	} else if len(d.Fields) > 0 {
		// Auto-generate __init__ from fields
		var params []string
		for _, f := range d.Fields {
			if f.Default != nil {
				params = append(params, fmt.Sprintf("%s=%s", f.Name, g.emitExpr(f.Default)))
			} else {
				params = append(params, f.Name)
			}
		}
		g.writeln(fmt.Sprintf("def __init__(self, %s):", strings.Join(params, ", ")))
		g.push()
		for _, f := range d.Fields {
			g.writeln(fmt.Sprintf("self.%s = %s", f.Name, f.Name))
		}
		g.pop()
	}

	// Methods
	for _, m := range d.Methods {
		g.write("\n")
		g.emitMethodDecl(m)
	}

	if d.Ctor == nil && len(d.Fields) == 0 && len(d.Methods) == 0 {
		g.writeln("pass")
	}

	g.pop()
}

func (g *Generator) emitMethodDecl(m *parser.MethodDecl) {
	if m.IsStatic {
		g.writeln("@staticmethod")
		params := g.formatParams(m.Params)
		g.writeln(fmt.Sprintf("def %s(%s):", m.Name, params))
	} else {
		params := g.formatParams(m.Params)
		if params != "" {
			params = "self, " + params
		} else {
			params = "self"
		}
		g.writeln(fmt.Sprintf("def %s(%s):", m.Name, params))
	}
	g.push()
	if m.Body != nil && len(m.Body.Stmts) > 0 {
		g.emitBlock(m.Body)
	} else {
		g.writeln("pass")
	}
	g.pop()
}

func (g *Generator) emitEnumDecl(d *parser.EnumDecl) {
	g.neededImports["enum"] = true
	g.writeln(fmt.Sprintf("class %s(enum.Enum):", d.Name))
	g.push()
	for i, v := range d.Variants {
		g.writeln(fmt.Sprintf("%s = %d", v, i+1))
	}
	g.pop()
}

func (g *Generator) emitConstDecl(d *parser.ConstDecl) {
	g.writeln(fmt.Sprintf("%s = %s", d.Name, g.emitExpr(d.Value)))
}

func (g *Generator) formatParams(params []*parser.ParamDecl) string {
	var parts []string
	for _, p := range params {
		if p.Variadic {
			parts = append(parts, fmt.Sprintf("*%s", p.Name))
		} else if p.Default != nil {
			parts = append(parts, fmt.Sprintf("%s=%s", p.Name, g.emitExpr(p.Default)))
		} else {
			parts = append(parts, p.Name)
		}
	}
	return strings.Join(parts, ", ")
}

// --- Statements --------------------------------------------------------------

func (g *Generator) emitBlock(block *parser.BlockStmt) {
	for _, s := range block.Stmts {
		g.emitStmt(s)
	}
}

func (g *Generator) emitStmt(s parser.Stmt) {
	switch s := s.(type) {
	case *parser.VarStmt:
		g.emitVarStmt(s)
	case *parser.TupleVarStmt:
		g.writeln(fmt.Sprintf("%s = %s", strings.Join(s.Names, ", "), g.emitExpr(s.Value)))
	case *parser.AssignStmt:
		target := g.emitExpr(s.Target)
		val := g.emitExpr(s.Value)
		g.writeln(fmt.Sprintf("%s %s %s", target, s.Op, val))
	case *parser.ReturnStmt:
		if s.Value != nil {
			g.writeln(fmt.Sprintf("return %s", g.emitExpr(s.Value)))
		} else {
			g.writeln("return")
		}
	case *parser.IfStmt:
		g.emitIfStmt(s)
	case *parser.ForStmt:
		g.emitForStmt(s)
	case *parser.WhileStmt:
		cond := g.emitExpr(s.Cond)
		g.writeln(fmt.Sprintf("while %s:", cond))
		g.push()
		g.emitBlock(s.Body)
		g.pop()
	case *parser.PrintStmt:
		g.writeln(fmt.Sprintf("print(%s)", g.emitExpr(s.Value)))
	case *parser.ExprStmt:
		// Check for collection chain used as statement (ForEach)
		if chain := g.unwrapChain(s.Expr); chain != nil {
			g.emitCollectionChainStmt(chain)
			return
		}
		g.writeln(g.emitExpr(s.Expr))
	case *parser.BreakStmt:
		g.writeln("break")
	case *parser.ContinueStmt:
		g.writeln("continue")
	case *parser.MatchStmt:
		g.emitMatchStmt(s)
	case *parser.BlockStmt:
		g.emitBlock(s)
	case *parser.GoStmt:
		g.neededImports["threading"] = true
		g.writeln("threading.Thread(target=lambda: (")
		g.push()
		g.emitBlock(s.Body)
		g.pop()
		g.writeln(")).start()")
	case *parser.DeferStmt:
		// Python has no defer — use atexit or context manager
		g.writeln(fmt.Sprintf("# defer: %s", g.emitExpr(s.Expr)))
	case *parser.WithStmt:
		var resources []string
		for _, r := range s.Resources {
			resources = append(resources, fmt.Sprintf("%s as %s", g.emitExpr(r.Value), r.Name))
		}
		g.writeln(fmt.Sprintf("with %s:", strings.Join(resources, ", ")))
		g.push()
		g.emitBlock(s.Body)
		g.pop()
	}
}

func (g *Generator) emitVarStmt(s *parser.VarStmt) {
	// Check for collection chain
	if s.Value != nil {
		if chain := g.unwrapChain(s.Value); chain != nil {
			g.emitCollectionChainVar(s.Name, chain)
			return
		}
	}

	if s.Value != nil {
		val := g.emitExpr(s.Value)
		if s.OrHandler != nil {
			// Failable: try/except
			g.writeln("try:")
			g.push()
			g.writeln(fmt.Sprintf("%s = %s", s.Name, val))
			g.pop()
			g.writeln("except Exception as err:")
			g.push()
			g.emitBlock(s.OrHandler.Body)
			g.pop()
		} else {
			g.writeln(fmt.Sprintf("%s = %s", s.Name, val))
		}
	} else {
		g.writeln(fmt.Sprintf("%s = None", s.Name))
	}
}

func (g *Generator) emitIfStmt(s *parser.IfStmt) {
	g.writeln(fmt.Sprintf("if %s:", g.emitExpr(s.Cond)))
	g.push()
	g.emitBlock(s.Then)
	g.pop()
	if s.ElseStmt != nil {
		if elseIf, ok := s.ElseStmt.(*parser.IfStmt); ok {
			g.write(strings.Repeat("    ", g.indent))
			g.write(fmt.Sprintf("elif %s:\n", g.emitExpr(elseIf.Cond)))
			g.push()
			g.emitBlock(elseIf.Then)
			g.pop()
			if elseIf.ElseStmt != nil {
				g.emitElseChain(elseIf.ElseStmt)
			}
		} else if block, ok := s.ElseStmt.(*parser.BlockStmt); ok {
			g.writeln("else:")
			g.push()
			g.emitBlock(block)
			g.pop()
		}
	}
}

func (g *Generator) emitElseChain(s parser.Stmt) {
	if elseIf, ok := s.(*parser.IfStmt); ok {
		g.write(strings.Repeat("    ", g.indent))
		g.write(fmt.Sprintf("elif %s:\n", g.emitExpr(elseIf.Cond)))
		g.push()
		g.emitBlock(elseIf.Then)
		g.pop()
		if elseIf.ElseStmt != nil {
			g.emitElseChain(elseIf.ElseStmt)
		}
	} else if block, ok := s.(*parser.BlockStmt); ok {
		g.writeln("else:")
		g.push()
		g.emitBlock(block)
		g.pop()
	}
}

func (g *Generator) emitForStmt(s *parser.ForStmt) {
	if s.IsRange {
		collection := g.emitExpr(s.Range)
		if s.IndexVar != "" {
			g.writeln(fmt.Sprintf("for %s, %s in enumerate(%s):", s.IndexVar, s.Item, collection))
		} else {
			g.writeln(fmt.Sprintf("for %s in %s:", s.Item, collection))
		}
		g.push()
		g.emitBlock(s.Body)
		g.pop()
	} else {
		// C-style for → while
		if s.Init != nil {
			g.emitStmt(s.Init)
		}
		cond := "True"
		if s.Cond != nil {
			cond = g.emitExpr(s.Cond)
		}
		g.writeln(fmt.Sprintf("while %s:", cond))
		g.push()
		g.emitBlock(s.Body)
		if s.Post != nil {
			g.emitStmt(s.Post)
		}
		g.pop()
	}
}

func (g *Generator) emitMatchStmt(s *parser.MatchStmt) {
	subject := g.emitExpr(s.Subject)
	for i, c := range s.Cases {
		keyword := "if"
		if i > 0 {
			keyword = "elif"
		}
		if c.Pattern == nil {
			g.writeln("else:")
		} else {
			g.writeln(fmt.Sprintf("%s %s == %s:", keyword, subject, g.emitExpr(c.Pattern)))
		}
		g.push()
		g.emitBlock(c.Body)
		g.pop()
	}
}

// --- Expressions -------------------------------------------------------------

func (g *Generator) emitExpr(e parser.Expr) string {
	switch e := e.(type) {
	case *parser.IntLit:
		return e.Value
	case *parser.FloatLit:
		return e.Value
	case *parser.StringLit:
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
		return e.Name
	case *parser.ThisExpr:
		return "self"
	case *parser.BinaryExpr:
		return g.emitBinaryExpr(e)
	case *parser.UnaryExpr:
		if e.Op == "!" {
			return fmt.Sprintf("not %s", g.emitExpr(e.Operand))
		}
		return fmt.Sprintf("(%s%s)", e.Op, g.emitExpr(e.Operand))
	case *parser.CallExpr:
		return g.emitCallExpr(e)
	case *parser.SelectorExpr:
		return fmt.Sprintf("%s.%s", g.emitExpr(e.Object), e.Field)
	case *parser.SafeNavExpr:
		obj := g.emitExpr(e.Object)
		if e.Call != nil {
			args := g.formatCallArgs(e.Call)
			return fmt.Sprintf("%s.%s(%s) if %s is not None else None", obj, e.Field, args, obj)
		}
		return fmt.Sprintf("%s.%s if %s is not None else None", obj, e.Field, obj)
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", g.emitExpr(e.Object), g.emitExpr(e.Index))
	case *parser.SliceExpr:
		obj := g.emitExpr(e.Object)
		low, high := "", ""
		if e.Low != nil {
			low = g.emitExpr(e.Low)
		}
		if e.High != nil {
			high = g.emitExpr(e.High)
		}
		return fmt.Sprintf("%s[%s:%s]", obj, low, high)
	case *parser.ListLit:
		var elems []string
		for _, el := range e.Elements {
			elems = append(elems, g.emitExpr(el))
		}
		return fmt.Sprintf("[%s]", strings.Join(elems, ", "))
	case *parser.MapLit:
		var entries []string
		for i, k := range e.Keys {
			entries = append(entries, fmt.Sprintf("%s: %s", g.emitExpr(k), g.emitExpr(e.Values[i])))
		}
		return fmt.Sprintf("{%s}", strings.Join(entries, ", "))
	case *parser.LambdaExpr:
		return g.emitLambda(e)
	case *parser.StringInterpLit:
		return g.emitInterpString(e)
	case *parser.TypeAssertExpr:
		obj := g.emitExpr(e.Object)
		if e.IsCheck {
			return fmt.Sprintf("isinstance(%s, %s)", obj, e.TypeName)
		}
		return obj // Python doesn't need explicit casts
	case *parser.SpreadExpr:
		return fmt.Sprintf("*%s", g.emitExpr(e.Expr))
	case *parser.SuperCallExpr:
		var args []string
		for _, a := range e.Args {
			args = append(args, g.emitExpr(a))
		}
		return fmt.Sprintf("super().__init__(%s)", strings.Join(args, ", "))
	default:
		return "None  # unsupported expr"
	}
}

func (g *Generator) emitBinaryExpr(e *parser.BinaryExpr) string {
	left := g.emitExpr(e.Left)
	right := g.emitExpr(e.Right)
	op := e.Op
	switch op {
	case "&&":
		op = "and"
	case "||":
		op = "or"
	case "??":
		// Null coalesce: left if left is not None else right
		return fmt.Sprintf("(%s if %s is not None else %s)", left, left, right)
	}
	return fmt.Sprintf("(%s %s %s)", left, op, right)
}

func (g *Generator) emitCallExpr(e *parser.CallExpr) string {
	// Handle constructor calls: ClassName(args) → ClassName(args) (same in Python!)
	callee := g.emitExpr(e.Callee)

	// Handle builtin method calls
	if sel, ok := e.Callee.(*parser.SelectorExpr); ok {
		if result, handled := g.emitBuiltinMethod(sel, e); handled {
			return result
		}
	}

	args := g.formatCallArgs(e)
	return fmt.Sprintf("%s(%s)", callee, args)
}

func (g *Generator) formatCallArgs(e *parser.CallExpr) string {
	var parts []string
	for _, a := range e.Args {
		parts = append(parts, g.emitExpr(a))
	}
	for _, na := range e.NamedArgs {
		parts = append(parts, fmt.Sprintf("%s=%s", na.Name, g.emitExpr(na.Value)))
	}
	return strings.Join(parts, ", ")
}

// emitBuiltinMethod handles Zinc builtin methods → Python equivalents.
func (g *Generator) emitBuiltinMethod(sel *parser.SelectorExpr, call *parser.CallExpr) (string, bool) {
	obj := g.emitExpr(sel.Object)
	switch sel.Field {
	// List methods
	case "add":
		if len(call.Args) == 1 {
			return fmt.Sprintf("%s.append(%s)", obj, g.emitExpr(call.Args[0])), true
		}
		// Multi-arg add
		var args []string
		for _, a := range call.Args {
			args = append(args, g.emitExpr(a))
		}
		return fmt.Sprintf("%s.extend([%s])", obj, strings.Join(args, ", ")), true
	case "remove":
		return fmt.Sprintf("%s.remove(%s)", obj, g.emitExpr(call.Args[0])), true
	case "size":
		return fmt.Sprintf("len(%s)", obj), true
	case "clone":
		return fmt.Sprintf("%s.copy()", obj), true
	case "sort":
		return fmt.Sprintf("sorted(%s)", obj), true
	case "join":
		return fmt.Sprintf("%s.join(%s)", g.emitExpr(call.Args[0]), obj), true
	// String methods
	case "upper":
		return fmt.Sprintf("%s.upper()", obj), true
	case "lower":
		return fmt.Sprintf("%s.lower()", obj), true
	case "contains":
		return fmt.Sprintf("(%s in %s)", g.emitExpr(call.Args[0]), obj), true
	case "startsWith":
		return fmt.Sprintf("%s.startswith(%s)", obj, g.emitExpr(call.Args[0])), true
	case "endsWith":
		return fmt.Sprintf("%s.endswith(%s)", obj, g.emitExpr(call.Args[0])), true
	case "trim":
		return fmt.Sprintf("%s.strip()", obj), true
	case "split":
		return fmt.Sprintf("%s.split(%s)", obj, g.emitExpr(call.Args[0])), true
	case "replace":
		return fmt.Sprintf("%s.replace(%s, %s)", obj, g.emitExpr(call.Args[0]), g.emitExpr(call.Args[1])), true
	// Map methods
	case "keys":
		return fmt.Sprintf("list(%s.keys())", obj), true
	case "values":
		return fmt.Sprintf("list(%s.values())", obj), true
	case "containsKey":
		return fmt.Sprintf("(%s in %s)", g.emitExpr(call.Args[0]), obj), true
	}
	return "", false
}

func (g *Generator) emitLambda(e *parser.LambdaExpr) string {
	var params []string
	for _, p := range e.Params {
		params = append(params, p.Name)
	}
	paramStr := strings.Join(params, ", ")

	if e.Expr != nil {
		return fmt.Sprintf("lambda %s: %s", paramStr, g.emitExpr(e.Expr))
	}
	// Block-body lambda — Python can't do this inline, emit as-is for now
	return fmt.Sprintf("lambda %s: None  # block lambda", paramStr)
}

func (g *Generator) emitInterpString(e *parser.StringInterpLit) string {
	var parts []string
	for _, p := range e.Parts {
		if sl, ok := p.(*parser.StringLit); ok {
			parts = append(parts, sl.Value)
		} else {
			parts = append(parts, fmt.Sprintf("{%s}", g.emitExpr(p)))
		}
	}
	return fmt.Sprintf(`f"%s"`, strings.Join(parts, ""))
}
