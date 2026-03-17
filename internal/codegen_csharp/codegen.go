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

// Package codegen_csharp transpiles Zinc AST to C# source code.
// This is a prototype backend targeting .NET Native AOT compilation.
package codegen_csharp

import (
	"fmt"
	"sort"
	"strings"

	"zinc/internal/codegen"
	"zinc/internal/parser"
)

// failableBuiltins is the set of Zinc built-in functions that can throw.
var failableBuiltins = map[string]bool{
	"readFile":  true,
	"writeFile": true,
	"httpGet":   true,
}

// voidFailableBuiltins is the subset that returns no value (void).
var voidFailableBuiltins = map[string]bool{
	"writeFile": true,
}

// csharpNamespaceMap maps short Zinc import names to C# namespaces.
var csharpNamespaceMap = map[string]string{
	// Common shortcuts
	"http":         "System.Net.Http",
	"json":         "System.Text.Json",
	"io":           "System.IO",
	"math":         "System",
	"collections":  "System.Collections.Generic",
	"linq":         "System.Linq",
	"text":         "System.Text",
	"regex":        "System.Text.RegularExpressions",
	"threading":    "System.Threading",
	"tasks":        "System.Threading.Tasks",
	"crypto":       "System.Security.Cryptography",
	"net":          "System.Net",
	"diagnostics":  "System.Diagnostics",
	"xml":          "System.Xml",
	"data":         "System.Data",
	"reflection":   "System.Reflection",
}

// Generator converts a Zinc AST to C# source code.
type Generator struct {
	buf            strings.Builder
	indent         int
	neededUsings   map[string]bool
	typeResolver   *CSharpTypeResolver
	classNames     map[string]bool
	interfaceNames map[string]bool
	enumNames      map[string]bool
	classMethods   map[string]map[string]bool // className → set of method names
	classParents   map[string][]string        // className → parent names
	canThrowFns    map[string]bool             // function/method names that throw
	srcFile        string
	lastDirectiveLine int
	errCounter     int
	currentRecordFields map[string]bool       // non-nil inside a record method body
}

// New creates a C# Generator for single-file mode.
func New() *Generator {
	return &Generator{
		neededUsings:   map[string]bool{"System": true},
		classNames:     make(map[string]bool),
		interfaceNames: make(map[string]bool),
		enumNames:      make(map[string]bool),
		classMethods:   make(map[string]map[string]bool),
		classParents:   make(map[string][]string),
		canThrowFns:    make(map[string]bool),
	}
}

// NewWithRegistry creates a C# Generator pre-seeded with cross-file type info.
func NewWithRegistry(reg *codegen.TypeRegistry) *Generator {
	g := New()
	for k, v := range reg.ClassNames {
		g.classNames[k] = v
	}
	for k, v := range reg.InterfaceNames {
		g.interfaceNames[k] = v
	}
	for k, v := range reg.EnumNames {
		g.enumNames[k] = v
	}
	for k, v := range reg.CanThrowFns {
		g.canThrowFns[k] = v
	}
	for k, v := range reg.ClassParents {
		g.classParents[k] = v
	}
	return g
}

// SetSourceFile enables #line directive emission for source mapping.
func (g *Generator) SetSourceFile(path string) {
	g.srcFile = path
}

// SetTypeResolver attaches a CSharpTypeResolver for .NET type introspection.
// Also registers classes from the default System namespace.
func (g *Generator) SetTypeResolver(r *CSharpTypeResolver) {
	g.typeResolver = r
	// Register classes from the always-imported System namespace
	for _, className := range r.ClassesInNamespace("System") {
		g.classNames[className] = true
	}
}

// Generate produces C# source from a Zinc AST.
func (g *Generator) Generate(prog *parser.Program) string {
	// First pass: collect type names and method signatures
	for _, d := range prog.Decls {
		switch d := d.(type) {
		case *parser.ClassDecl:
			g.classNames[d.Name] = true
			methods := make(map[string]bool)
			for _, m := range d.Methods {
				methods[m.Name] = true
			}
			g.classMethods[d.Name] = methods
			g.classParents[d.Name] = d.Parents
		case *parser.DataClassDecl:
			g.classNames[d.Name] = true
			methods := make(map[string]bool)
			for _, m := range d.Methods {
				methods[m.Name] = true
			}
			g.classMethods[d.Name] = methods
			g.classParents[d.Name] = d.Parents
		case *parser.InterfaceDecl:
			g.interfaceNames[d.Name] = true
		case *parser.EnumDecl:
			g.enumNames[d.Name] = true
		}
	}

	// Fixed-point: transitively mark user fns as failable
	for {
		changed := false
		for _, d := range prog.Decls {
			switch d := d.(type) {
			case *parser.FnDecl:
				if !g.canThrowFns[d.Name] && g.bodyIsFailable(d.Body) {
					g.canThrowFns[d.Name] = true
					changed = true
				}
			case *parser.ClassDecl:
				for _, m := range d.Methods {
					key := d.Name + "." + m.Name
					if !g.canThrowFns[key] && g.bodyIsFailable(m.Body) {
						g.canThrowFns[key] = true
						changed = true
					}
				}
			case *parser.DataClassDecl:
				for _, m := range d.Methods {
					key := d.Name + "." + m.Name
					if !g.canThrowFns[key] && g.bodyIsFailable(m.Body) {
						g.canThrowFns[key] = true
						changed = true
					}
				}
			}
		}
		if !changed {
			break
		}
	}

	// Process imports from prog.Imports
	for _, imp := range prog.Imports {
		g.processImport(imp)
	}

	// Separate top-level functions from other declarations.
	// All non-main functions go into a single static Functions class.
	var fnDecls []*parser.FnDecl
	var otherDecls []parser.TopLevelDecl
	for _, d := range prog.Decls {
		if fn, ok := d.(*parser.FnDecl); ok && fn.Name != "main" {
			fnDecls = append(fnDecls, fn)
		} else {
			otherDecls = append(otherDecls, d)
		}
	}

	// Emit non-function declarations (classes, interfaces, enums, consts, main)
	first := true
	for _, d := range otherDecls {
		if !first {
			g.write("\n")
		}
		first = false
		g.emitDecl(d)
	}

	// Emit all non-main functions inside a single Functions class
	if len(fnDecls) > 0 {
		if !first {
			g.write("\n")
		}
		g.neededUsings["static Functions"] = true
		g.writeln("public static class Functions")
		g.writeln("{")
		g.push()
		for i, fn := range fnDecls {
			if i > 0 {
				g.write("\n")
			}
			g.emitLineDirective(fn.Line)
			g.emitFnBody(fn)
		}
		g.pop()
		g.writeln("}")
	}

	// Prepend usings (sorted for deterministic output)
	var usings []string
	for u := range g.neededUsings {
		usings = append(usings, u)
	}
	sort.Strings(usings)
	var out strings.Builder
	for _, u := range usings {
		out.WriteString(fmt.Sprintf("using %s;\n", u))
	}
	out.WriteString("\n")
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

func (g *Generator) nextErr() string {
	g.errCounter++
	return fmt.Sprintf("_err%d", g.errCounter)
}

// --- Declarations ------------------------------------------------------------

// processImport maps a Zinc import to a C# using directive.
//
// Import resolution order:
//  1. Short aliases: "http" → "System.Net.Http", "json" → "System.Text.Json"
//  2. Direct namespace: "Newtonsoft.Json" → using Newtonsoft.Json;
//  3. Local packages: "myapp/utils" → skipped (handled by TypeRegistry)
func (g *Generator) processImport(imp *parser.ImportDecl) {
	path := imp.Path

	// Skip local package imports (contain "/" — cross-file resolution via TypeRegistry)
	if strings.Contains(path, "/") {
		return
	}

	// Determine the C# namespace
	ns := path
	if mapped, ok := csharpNamespaceMap[path]; ok {
		ns = mapped
	}

	g.neededUsings[ns] = true

	// If a type resolver is available, register imported classes so that
	// constructor calls like Stopwatch() emit `new Stopwatch()`.
	if g.typeResolver != nil {
		for _, className := range g.typeResolver.ClassesInNamespace(ns) {
			g.classNames[className] = true
		}
	}
}

func (g *Generator) emitLineDirective(line int) {
	if g.srcFile == "" || line <= 0 || line == g.lastDirectiveLine {
		return
	}
	g.lastDirectiveLine = line
	g.writeln(fmt.Sprintf("#line %d \"%s\"", line, g.srcFile))
}

func (g *Generator) emitDecl(d parser.TopLevelDecl) {
	switch d := d.(type) {
	case *parser.FnDecl:
		g.emitLineDirective(d.Line)
		g.emitFnDecl(d)
	case *parser.ClassDecl:
		g.emitLineDirective(d.Line)
		g.emitClassDecl(d)
	case *parser.DataClassDecl:
		g.emitLineDirective(d.Line)
		g.emitDataClassDecl(d)
	case *parser.InterfaceDecl:
		g.emitLineDirective(d.Line)
		g.emitInterfaceDecl(d)
	case *parser.EnumDecl:
		g.emitLineDirective(d.Line)
		g.emitEnumDecl(d)
	case *parser.ConstDecl:
		g.emitLineDirective(d.Line)
		g.emitConstDecl(d)
	}
}

func (g *Generator) emitFnDecl(d *parser.FnDecl) {
	if d.Name == "main" {
		// C# entry point
		g.writeln("public class Program")
		g.writeln("{")
		g.push()
		g.writeln("public static void Main(string[] args)")
		g.writeln("{")
		g.push()
		if d.Body != nil {
			g.emitBlock(d.Body)
		}
		g.pop()
		g.writeln("}")
		g.pop()
		g.writeln("}")
		return
	}

	// Standalone function — wrapped in Functions class by Generate()
	g.emitFnBody(d)
}

// emitAnnotations emits C# attributes for a list of Zinc annotations.
// @Foo → [Foo], @Foo("bar") → [Foo("bar")], @Foo("a", "b") → [Foo("a", "b")]
func (g *Generator) emitAnnotations(annotations []*parser.Annotation) {
	for _, a := range annotations {
		if len(a.Args) == 0 {
			g.writeln(fmt.Sprintf("[%s]", a.Name))
		} else {
			quoted := make([]string, len(a.Args))
			for i, arg := range a.Args {
				quoted[i] = fmt.Sprintf(`"%s"`, arg)
			}
			g.writeln(fmt.Sprintf("[%s(%s)]", a.Name, strings.Join(quoted, ", ")))
		}
	}
}

// emitFnBody emits a single static method (without the wrapping class).
func (g *Generator) emitFnBody(d *parser.FnDecl) {
	g.emitAnnotations(d.Annotations)
	retType := g.emitType(d.ReturnType)
	vis := "private"
	if d.IsPub {
		vis = "public"
	}
	params := g.formatParams(d.Params)
	typeParams := g.formatTypeParams(d.TypeParams)
	g.writeln(fmt.Sprintf("%s static %s %s%s(%s)", vis, retType, capitalize(d.Name), typeParams, params))
	g.writeln("{")
	g.push()
	if d.Body != nil {
		g.emitBlockWithImplicitReturn(d.Body, d.ReturnType != nil)
	}
	g.pop()
	g.writeln("}")
}

func (g *Generator) emitClassDecl(d *parser.ClassDecl) {
	// Build parents list
	var parents []string
	for _, p := range d.Parents {
		if g.interfaceNames[p] {
			parents = append(parents, "I"+p)
		} else {
			parents = append(parents, p)
		}
	}

	typeParams := g.formatTypeParams(d.TypeParams)
	inheritance := ""
	if len(parents) > 0 {
		inheritance = " : " + strings.Join(parents, ", ")
	}

	g.emitAnnotations(d.Annotations)
	g.writeln(fmt.Sprintf("public class %s%s%s", d.Name, typeParams, inheritance))
	g.writeln("{")
	g.push()

	// Fields
	for _, f := range d.Fields {
		g.emitAnnotations(f.Annotations)
		fieldType := g.emitType(f.Type)
		vis := "private"
		if f.IsPub {
			vis = "public"
		}
		fname := g.fieldName(f.Name, f.IsPub)
		if f.Default != nil {
			g.writeln(fmt.Sprintf("%s %s %s = %s;", vis, fieldType, fname, g.emitExpr(f.Default)))
		} else {
			g.writeln(fmt.Sprintf("%s %s %s;", vis, fieldType, fname))
		}
	}

	if len(d.Fields) > 0 && (d.Ctor != nil || len(d.Methods) > 0) {
		g.write("\n")
	}

	// Constructor
	if d.Ctor != nil {
		params := g.formatParams(d.Ctor.Params)
		g.writeln(fmt.Sprintf("public %s(%s)", d.Name, params))
		// Super call
		if len(d.Ctor.SuperArgs) > 0 {
			var args []string
			for _, a := range d.Ctor.SuperArgs {
				args = append(args, g.emitExpr(a))
			}
			g.indent++
			g.writeln(fmt.Sprintf(": base(%s)", strings.Join(args, ", ")))
			g.indent--
		}
		g.writeln("{")
		g.push()
		if d.Ctor.Body != nil {
			g.emitBlock(d.Ctor.Body)
		}
		g.pop()
		g.writeln("}")
	}

	// Methods
	for _, m := range d.Methods {
		g.write("\n")
		g.emitMethodDecl(d.Name, d.TypeParams, d.Parents, m)
	}

	g.pop()
	g.writeln("}")
}

func (g *Generator) emitDataClassDecl(d *parser.DataClassDecl) {
	// Build parents list
	var parents []string
	for _, p := range d.Parents {
		if g.interfaceNames[p] {
			parents = append(parents, "I"+p)
		} else {
			parents = append(parents, p)
		}
	}

	typeParams := g.formatTypeParams(d.TypeParams)
	inheritance := ""
	if len(parents) > 0 {
		inheritance = " : " + strings.Join(parents, ", ")
	}

	// Build record parameter list
	var params []string
	for _, f := range d.Params {
		fieldType := g.emitType(f.Type)
		fname := capitalize(f.Name)
		params = append(params, fmt.Sprintf("%s %s", fieldType, fname))
	}

	if len(d.Methods) == 0 {
		// Simple record — single line
		g.writeln(fmt.Sprintf("public record %s%s(%s)%s;", d.Name, typeParams, strings.Join(params, ", "), inheritance))
	} else {
		// Record with body — set currentRecordFields so bare field refs get capitalized
		fieldSet := make(map[string]bool)
		for _, f := range d.Params {
			fieldSet[f.Name] = true
		}
		g.writeln(fmt.Sprintf("public record %s%s(%s)%s", d.Name, typeParams, strings.Join(params, ", "), inheritance))
		g.writeln("{")
		g.push()
		saved := g.currentRecordFields
		g.currentRecordFields = fieldSet
		for _, m := range d.Methods {
			g.write("\n")
			g.emitMethodDecl(d.Name, d.TypeParams, d.Parents, m)
		}
		g.currentRecordFields = saved
		g.pop()
		g.writeln("}")
	}
}

func (g *Generator) emitMethodDecl(className string, classTypeParams []string, parents []string, m *parser.MethodDecl) {
	g.emitAnnotations(m.Annotations)
	retType := g.emitType(m.ReturnType)
	vis := "private"
	if m.IsPub {
		vis = "public"
	}
	params := g.formatParams(m.Params)
	typeParams := g.formatTypeParams(nil)

	modifier := ""
	if m.IsStatic {
		modifier = "static "
	} else if g.parentHasMethod(parents, m.Name) {
		modifier = "override "
	} else if g.isOverriddenByChild(className, m.Name) {
		modifier = "virtual "
	}

	g.writeln(fmt.Sprintf("%s %s%s %s%s(%s)", vis, modifier, retType, capitalize(m.Name), typeParams, params))
	g.writeln("{")
	g.push()
	if m.Body != nil && len(m.Body.Stmts) > 0 {
		g.emitBlockWithImplicitReturn(m.Body, m.ReturnType != nil)
	}
	g.pop()
	g.writeln("}")
}

func (g *Generator) emitInterfaceDecl(d *parser.InterfaceDecl) {
	g.writeln(fmt.Sprintf("public interface I%s", d.Name))
	g.writeln("{")
	g.push()
	for _, m := range d.Methods {
		retType := g.emitType(m.ReturnType)
		params := g.formatParams(m.Params)
		g.writeln(fmt.Sprintf("%s %s(%s);", retType, capitalize(m.Name), params))
	}
	g.pop()
	g.writeln("}")
}

func (g *Generator) emitEnumDecl(d *parser.EnumDecl) {
	g.writeln(fmt.Sprintf("public enum %s", d.Name))
	g.writeln("{")
	g.push()
	for i, v := range d.Variants {
		comma := ","
		if i == len(d.Variants)-1 {
			comma = ""
		}
		g.writeln(fmt.Sprintf("%s%s", v, comma))
	}
	g.pop()
	g.writeln("}")
}

func (g *Generator) emitConstDecl(d *parser.ConstDecl) {
	constType := g.emitType(d.Type)
	if constType == "var" {
		constType = "object"
	}
	vis := "private"
	if d.IsPub {
		vis = "public"
	}
	g.writeln(fmt.Sprintf("%s const %s %s = %s;", vis, constType, capitalize(d.Name), g.emitExpr(d.Value)))
}

// --- Statements --------------------------------------------------------------

func (g *Generator) emitBlock(block *parser.BlockStmt) {
	for _, s := range block.Stmts {
		g.emitStmt(s)
	}
}

// emitBlockWithImplicitReturn emits a block where the last ExprStmt is
// automatically treated as a return statement if hasReturnType is true.
func (g *Generator) emitBlockWithImplicitReturn(block *parser.BlockStmt, hasReturnType bool) {
	if !hasReturnType || len(block.Stmts) == 0 {
		g.emitBlock(block)
		return
	}
	// Emit all but last statement normally
	for _, s := range block.Stmts[:len(block.Stmts)-1] {
		g.emitStmt(s)
	}
	// Check if last statement is a bare ExprStmt (no or-handler) — make it a return
	last := block.Stmts[len(block.Stmts)-1]
	if es, ok := last.(*parser.ExprStmt); ok && es.OrHandler == nil {
		g.emitLineDirective(es.Line)
		g.writeln(fmt.Sprintf("return %s;", g.emitExpr(es.Expr)))
		return
	}
	// Otherwise emit normally (e.g., last stmt is an if/return/etc.)
	g.emitStmt(last)
}

func (g *Generator) emitStmt(s parser.Stmt) {
	switch s := s.(type) {
	case *parser.VarStmt:
		g.emitVarStmt(s)
	case *parser.TupleVarStmt:
		val := g.emitExpr(s.Value)
		g.writeln(fmt.Sprintf("var _tuple = %s;", val))
		for i, name := range s.Names {
			g.writeln(fmt.Sprintf("var %s = _tuple.Item%d;", name, i+1))
		}
	case *parser.AssignStmt:
		target := g.emitExpr(s.Target)
		val := g.emitExpr(s.Value)
		g.writeln(fmt.Sprintf("%s %s %s;", target, s.Op, val))
	case *parser.ReturnStmt:
		if s.Value != nil {
			g.writeln(fmt.Sprintf("return %s;", g.emitExpr(s.Value)))
		} else {
			g.writeln("return;")
		}
	case *parser.IfStmt:
		g.emitIfStmt(s)
	case *parser.ForStmt:
		g.emitForStmt(s)
	case *parser.WhileStmt:
		g.writeln(fmt.Sprintf("while (%s)", g.emitExpr(s.Cond)))
		g.writeln("{")
		g.push()
		g.emitBlock(s.Body)
		g.pop()
		g.writeln("}")
	case *parser.PrintStmt:
		g.neededUsings["System"] = true
		g.writeln(fmt.Sprintf("Console.WriteLine(%s);", g.emitExpr(s.Value)))
	case *parser.ExprStmt:
		if s.OrHandler != nil {
			if call, ok := s.Expr.(*parser.CallExpr); ok {
				g.emitFailableExprStmt(call, s.OrHandler)
				return
			}
		}
		g.writeln(fmt.Sprintf("%s;", g.emitExpr(s.Expr)))
	case *parser.BreakStmt:
		g.writeln("break;")
	case *parser.ContinueStmt:
		g.writeln("continue;")
	case *parser.MatchStmt:
		g.emitMatchStmt(s)
	case *parser.BlockStmt:
		g.writeln("{")
		g.push()
		g.emitBlock(s)
		g.pop()
		g.writeln("}")
	case *parser.GoStmt:
		g.neededUsings["System.Threading.Tasks"] = true
		g.writeln("Task.Run(() =>")
		g.writeln("{")
		g.push()
		g.emitBlock(s.Body)
		g.pop()
		g.writeln("});")
	case *parser.DeferStmt:
		// C# doesn't have defer — approximate with comment
		g.writeln(fmt.Sprintf("// defer: %s", g.emitExpr(s.Expr)))
	case *parser.WithStmt:
		g.emitWithStmt(s)
	}
}

func (g *Generator) emitVarStmt(s *parser.VarStmt) {
	if s.Value == nil {
		varType := g.emitType(s.Type)
		if varType == "var" {
			g.writeln(fmt.Sprintf("%s %s = default;", varType, s.Name))
		} else {
			g.writeln(fmt.Sprintf("%s %s = default(%s);", varType, s.Name, varType))
		}
		return
	}

	val := g.emitExpr(s.Value)

	if s.OrHandler != nil {
		// Failable: try/catch — or {} always propagates
		varType := "object"
		if s.Type != nil {
			varType = g.emitType(s.Type)
		}
		exVar := g.nextErr()
		g.writeln(fmt.Sprintf("%s %s;", varType, s.Name))
		g.writeln("try")
		g.writeln("{")
		g.push()
		g.writeln(fmt.Sprintf("%s = %s;", s.Name, val))
		g.pop()
		g.writeln("}")
		g.writeln(fmt.Sprintf("catch (Exception %s)", exVar))
		g.writeln("{")
		g.push()
		g.writeln(fmt.Sprintf("var err = %s.Message;", exVar))
		if s.OrHandler.Body != nil && len(s.OrHandler.Body.Stmts) > 0 {
			g.emitBlock(s.OrHandler.Body)
		}
		if !g.handlerHasHalt(s.OrHandler.Body) {
			g.writeln("throw;")
		}
		g.pop()
		g.writeln("}")
	} else if s.Type != nil {
		g.writeln(fmt.Sprintf("%s %s = %s;", g.emitType(s.Type), s.Name, val))
	} else {
		g.writeln(fmt.Sprintf("var %s = %s;", s.Name, val))
	}
}

func (g *Generator) emitIfStmt(s *parser.IfStmt) {
	g.writeln(fmt.Sprintf("if (%s)", g.emitExpr(s.Cond)))
	g.writeln("{")
	g.push()
	g.emitBlock(s.Then)
	g.pop()
	g.writeln("}")
	if s.ElseStmt != nil {
		if elseIf, ok := s.ElseStmt.(*parser.IfStmt); ok {
			g.write(strings.Repeat("    ", g.indent))
			g.write(fmt.Sprintf("else if (%s)\n", g.emitExpr(elseIf.Cond)))
			g.writeln("{")
			g.push()
			g.emitBlock(elseIf.Then)
			g.pop()
			g.writeln("}")
			if elseIf.ElseStmt != nil {
				g.emitElseChain(elseIf.ElseStmt)
			}
		} else if block, ok := s.ElseStmt.(*parser.BlockStmt); ok {
			g.writeln("else")
			g.writeln("{")
			g.push()
			g.emitBlock(block)
			g.pop()
			g.writeln("}")
		}
	}
}

func (g *Generator) emitElseChain(s parser.Stmt) {
	if elseIf, ok := s.(*parser.IfStmt); ok {
		g.write(strings.Repeat("    ", g.indent))
		g.write(fmt.Sprintf("else if (%s)\n", g.emitExpr(elseIf.Cond)))
		g.writeln("{")
		g.push()
		g.emitBlock(elseIf.Then)
		g.pop()
		g.writeln("}")
		if elseIf.ElseStmt != nil {
			g.emitElseChain(elseIf.ElseStmt)
		}
	} else if block, ok := s.(*parser.BlockStmt); ok {
		g.writeln("else")
		g.writeln("{")
		g.push()
		g.emitBlock(block)
		g.pop()
		g.writeln("}")
	}
}

func (g *Generator) emitForStmt(s *parser.ForStmt) {
	if s.IsRange {
		collection := g.emitExpr(s.Range)
		if s.IndexVar != "" {
			// for i, item in collection → indexed loop
			g.writeln(fmt.Sprintf("for (int %s = 0; %s < %s.Count; %s++)", s.IndexVar, s.IndexVar, collection, s.IndexVar))
			g.writeln("{")
			g.push()
			g.writeln(fmt.Sprintf("var %s = %s[%s];", s.Item, collection, s.IndexVar))
			g.emitBlock(s.Body)
			g.pop()
			g.writeln("}")
		} else {
			g.writeln(fmt.Sprintf("foreach (var %s in %s)", s.Item, collection))
			g.writeln("{")
			g.push()
			g.emitBlock(s.Body)
			g.pop()
			g.writeln("}")
		}
	} else {
		// C-style for
		init := ""
		if s.Init != nil {
			init = g.stmtInline(s.Init)
		}
		cond := ""
		if s.Cond != nil {
			cond = g.emitExpr(s.Cond)
		}
		post := ""
		if s.Post != nil {
			post = g.stmtInline(s.Post)
		}
		g.writeln(fmt.Sprintf("for (%s; %s; %s)", init, cond, post))
		g.writeln("{")
		g.push()
		g.emitBlock(s.Body)
		g.pop()
		g.writeln("}")
	}
}

func (g *Generator) emitMatchStmt(s *parser.MatchStmt) {
	subject := g.emitExpr(s.Subject)
	g.writeln(fmt.Sprintf("switch (%s)", subject))
	g.writeln("{")
	g.push()
	for _, c := range s.Cases {
		if c.Pattern == nil {
			g.writeln("default:")
		} else {
			g.writeln(fmt.Sprintf("case %s:", g.emitExpr(c.Pattern)))
		}
		g.push()
		g.emitBlock(c.Body)
		g.writeln("break;")
		g.pop()
	}
	g.pop()
	g.writeln("}")
}

func (g *Generator) emitWithStmt(s *parser.WithStmt) {
	for _, r := range s.Resources {
		g.writeln(fmt.Sprintf("using (var %s = %s)", r.Name, g.emitExpr(r.Value)))
	}
	g.writeln("{")
	g.push()
	g.emitBlock(s.Body)
	g.pop()
	g.writeln("}")
}

// stmtInline returns a single statement as an inline expression (no semicolon/newline).
func (g *Generator) stmtInline(s parser.Stmt) string {
	switch s := s.(type) {
	case *parser.VarStmt:
		if s.Value != nil {
			return fmt.Sprintf("var %s = %s", s.Name, g.emitExpr(s.Value))
		}
		return fmt.Sprintf("var %s = default", s.Name)
	case *parser.AssignStmt:
		return fmt.Sprintf("%s %s %s", g.emitExpr(s.Target), s.Op, g.emitExpr(s.Value))
	case *parser.ExprStmt:
		return g.emitExpr(s.Expr)
	default:
		return "/* unsupported */"
	}
}

// --- Expressions -------------------------------------------------------------

func (g *Generator) emitExpr(e parser.Expr) string {
	if e == nil {
		return ""
	}
	switch e := e.(type) {
	case *parser.IntLit:
		return e.Value
	case *parser.FloatLit:
		return e.Value
	case *parser.StringLit:
		return fmt.Sprintf(`"%s"`, e.Value)
	case *parser.RawStringLit:
		return fmt.Sprintf(`@"%s"`, strings.ReplaceAll(e.Value, `"`, `""`))
	case *parser.BoolLit:
		if e.Value {
			return "true"
		}
		return "false"
	case *parser.NullLit:
		return "null"
	case *parser.Ident:
		// Inside a record method, bare field references must be PascalCase
		if g.currentRecordFields != nil && g.currentRecordFields[e.Name] {
			return capitalize(e.Name)
		}
		return e.Name
	case *parser.ThisExpr:
		return "this"
	case *parser.BinaryExpr:
		return g.emitBinaryExpr(e)
	case *parser.UnaryExpr:
		return fmt.Sprintf("(%s%s)", e.Op, g.emitExpr(e.Operand))
	case *parser.CallExpr:
		return g.emitCallExpr(e)
	case *parser.SelectorExpr:
		return fmt.Sprintf("%s.%s", g.emitExpr(e.Object), capitalize(e.Field))
	case *parser.SafeNavExpr:
		obj := g.emitExpr(e.Object)
		if e.Call != nil {
			args := g.formatCallArgs(e.Call)
			return fmt.Sprintf("%s?.%s(%s)", obj, capitalize(e.Field), args)
		}
		return fmt.Sprintf("%s?.%s", obj, capitalize(e.Field))
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", g.emitExpr(e.Object), g.emitExpr(e.Index))
	case *parser.SliceExpr:
		obj := g.emitExpr(e.Object)
		if e.Low != nil && e.High != nil {
			low := g.emitExpr(e.Low)
			high := g.emitExpr(e.High)
			return fmt.Sprintf("%s[%s..%s]", obj, low, high)
		} else if e.Low != nil {
			return fmt.Sprintf("%s[%s..]", obj, g.emitExpr(e.Low))
		} else if e.High != nil {
			return fmt.Sprintf("%s[..%s]", obj, g.emitExpr(e.High))
		}
		return fmt.Sprintf("%s[..]", obj)
	case *parser.ListLit:
		g.neededUsings["System.Collections.Generic"] = true
		var elems []string
		for _, el := range e.Elements {
			elems = append(elems, g.emitExpr(el))
		}
		elemType := g.inferListElemType(e)
		return fmt.Sprintf("new List<%s> { %s }", elemType, strings.Join(elems, ", "))
	case *parser.MapLit:
		g.neededUsings["System.Collections.Generic"] = true
		var entries []string
		for i, k := range e.Keys {
			entries = append(entries, fmt.Sprintf("{ %s, %s }", g.emitExpr(k), g.emitExpr(e.Values[i])))
		}
		keyType, valType := g.inferMapTypes(e)
		return fmt.Sprintf("new Dictionary<%s, %s> { %s }", keyType, valType, strings.Join(entries, ", "))
	case *parser.LambdaExpr:
		return g.emitLambda(e)
	case *parser.StringInterpLit:
		return g.emitInterpString(e)
	case *parser.TypeAssertExpr:
		obj := g.emitExpr(e.Object)
		if e.IsCheck {
			return fmt.Sprintf("%s is %s", obj, e.TypeName)
		}
		return fmt.Sprintf("(%s)%s", e.TypeName, obj)
	case *parser.SpreadExpr:
		return g.emitExpr(e.Expr)
	case *parser.SuperCallExpr:
		var args []string
		for _, a := range e.Args {
			args = append(args, g.emitExpr(a))
		}
		return fmt.Sprintf("base(%s)", strings.Join(args, ", "))
	case *parser.IfExpr:
		cond := g.emitExpr(e.Cond)
		then := g.emitExpr(e.Then)
		elseVal := g.emitExpr(e.Else)
		return fmt.Sprintf("(%s ? %s : %s)", cond, then, elseVal)
	case *parser.MatchExpr:
		subject := g.emitExpr(e.Subject)
		var arms []string
		for _, c := range e.Cases {
			val := g.emitExpr(c.Value)
			if c.Pattern == nil {
				arms = append(arms, fmt.Sprintf("_ => %s", val))
			} else {
				arms = append(arms, fmt.Sprintf("%s => %s", g.emitExpr(c.Pattern), val))
			}
		}
		return fmt.Sprintf("(%s switch { %s })", subject, strings.Join(arms, ", "))
	case *parser.RangeExpr:
		g.neededUsings["System.Linq"] = true
		start := g.emitExpr(e.Start)
		end := g.emitExpr(e.End)
		if e.Inclusive {
			return fmt.Sprintf("Enumerable.Range(%s, %s - %s + 1)", start, end, start)
		}
		return fmt.Sprintf("Enumerable.Range(%s, %s - %s)", start, end, start)
	default:
		return "default /* unsupported expr */"
	}
}

func (g *Generator) emitBinaryExpr(e *parser.BinaryExpr) string {
	left := g.emitExpr(e.Left)
	right := g.emitExpr(e.Right)
	op := e.Op
	switch op {
	case "??":
		return fmt.Sprintf("(%s ?? %s)", left, right)
	}
	return fmt.Sprintf("(%s %s %s)", left, op, right)
}

func (g *Generator) emitCallExpr(e *parser.CallExpr) string {
	// Handle builtin method calls
	if sel, ok := e.Callee.(*parser.SelectorExpr); ok {
		if result, handled := g.emitBuiltinMethod(sel, e); handled {
			return result
		}
	}

	// Constructor calls: ClassName(args) → new ClassName(args)
	if ident, ok := e.Callee.(*parser.Ident); ok {
		if g.classNames[ident.Name] {
			args := g.formatCallArgs(e)
			typeArgs := ""
			if len(e.TypeArgs) > 0 {
				typeArgs = fmt.Sprintf("<%s>", strings.Join(e.TypeArgs, ", "))
			}
			return fmt.Sprintf("new %s%s(%s)", ident.Name, typeArgs, args)
		}

		// Builtin function calls
		if code, ok := g.emitBuiltinCall(ident.Name, e.Args, e.TypeArgs); ok {
			return code
		}
	}

	callee := g.emitExpr(e.Callee)
	args := g.formatCallArgs(e)
	return fmt.Sprintf("%s(%s)", callee, args)
}

func (g *Generator) formatCallArgs(e *parser.CallExpr) string {
	var parts []string
	for _, a := range e.Args {
		parts = append(parts, g.emitExpr(a))
	}
	for _, na := range e.NamedArgs {
		parts = append(parts, fmt.Sprintf("%s: %s", na.Name, g.emitExpr(na.Value)))
	}
	return strings.Join(parts, ", ")
}

func (g *Generator) emitBuiltinMethod(sel *parser.SelectorExpr, call *parser.CallExpr) (string, bool) {
	obj := g.emitExpr(sel.Object)
	switch sel.Field {

	// --- List mutation methods ------------------------------------------------

	case "Add":
		if len(call.Args) == 1 {
			return fmt.Sprintf("%s.Add(%s)", obj, g.emitExpr(call.Args[0])), true
		}
		var stmts []string
		for _, a := range call.Args {
			stmts = append(stmts, fmt.Sprintf("%s.Add(%s)", obj, g.emitExpr(a)))
		}
		return strings.Join(stmts, "; "), true
	case "Remove":
		return fmt.Sprintf("%s.Remove(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Clear":
		return fmt.Sprintf("%s.Clear()", obj), true
	case "Insert":
		return fmt.Sprintf("%s.Insert(%s, %s)", obj, g.emitExpr(call.Args[0]), g.emitExpr(call.Args[1])), true
	case "RemoveAt":
		return fmt.Sprintf("%s.RemoveAt(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Reverse":
		return fmt.Sprintf("%s.Reverse()", obj), true
	case "Sort":
		if len(call.Args) == 0 {
			return fmt.Sprintf("%s.Sort()", obj), true
		}
		return fmt.Sprintf("%s.Sort((%s))", obj, g.emitExpr(call.Args[0])), true

	// --- List/collection query methods (LINQ) --------------------------------

	case "Where":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.Where(%s).ToList()", obj, g.emitExpr(call.Args[0])), true
	case "Select":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.Select(%s).ToList()", obj, g.emitExpr(call.Args[0])), true
	case "First":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 0 {
			return fmt.Sprintf("%s.First()", obj), true
		}
		return fmt.Sprintf("%s.First(%s)", obj, g.emitExpr(call.Args[0])), true
	case "FirstOrDefault":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 0 {
			return fmt.Sprintf("%s.FirstOrDefault()", obj), true
		}
		return fmt.Sprintf("%s.FirstOrDefault(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Last":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 0 {
			return fmt.Sprintf("%s.Last()", obj), true
		}
		return fmt.Sprintf("%s.Last(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Any":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 0 {
			return fmt.Sprintf("%s.Any()", obj), true
		}
		return fmt.Sprintf("%s.Any(%s)", obj, g.emitExpr(call.Args[0])), true
	case "All":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.All(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Count":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 0 {
			return fmt.Sprintf("%s.Count()", obj), true
		}
		return fmt.Sprintf("%s.Count(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Sum":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 0 {
			return fmt.Sprintf("%s.Sum()", obj), true
		}
		return fmt.Sprintf("%s.Sum(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Min":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 0 {
			return fmt.Sprintf("%s.Min()", obj), true
		}
		return fmt.Sprintf("%s.Min(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Max":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 0 {
			return fmt.Sprintf("%s.Max()", obj), true
		}
		return fmt.Sprintf("%s.Max(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Average":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 0 {
			return fmt.Sprintf("%s.Average()", obj), true
		}
		return fmt.Sprintf("%s.Average(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Aggregate":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 2 {
			return fmt.Sprintf("%s.Aggregate(%s, %s)", obj, g.emitExpr(call.Args[0]), g.emitExpr(call.Args[1])), true
		}
		return fmt.Sprintf("%s.Aggregate(%s)", obj, g.emitExpr(call.Args[0])), true
	case "OrderBy":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.OrderBy(%s).ToList()", obj, g.emitExpr(call.Args[0])), true
	case "OrderByDescending":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.OrderByDescending(%s).ToList()", obj, g.emitExpr(call.Args[0])), true
	case "Take":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.Take(%s).ToList()", obj, g.emitExpr(call.Args[0])), true
	case "Skip":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.Skip(%s).ToList()", obj, g.emitExpr(call.Args[0])), true
	case "Distinct":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.Distinct().ToList()", obj), true
	case "Zip":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 2 {
			return fmt.Sprintf("%s.Zip(%s, %s).ToList()", obj, g.emitExpr(call.Args[0]), g.emitExpr(call.Args[1])), true
		}
		return fmt.Sprintf("%s.Zip(%s).ToList()", obj, g.emitExpr(call.Args[0])), true
	case "SelectMany":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.SelectMany(%s).ToList()", obj, g.emitExpr(call.Args[0])), true
	case "GroupBy":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.GroupBy(%s).ToList()", obj, g.emitExpr(call.Args[0])), true
	case "ToDictionary":
		g.neededUsings["System.Linq"] = true
		if len(call.Args) == 2 {
			return fmt.Sprintf("%s.ToDictionary(%s, %s)", obj, g.emitExpr(call.Args[0]), g.emitExpr(call.Args[1])), true
		}
		return fmt.Sprintf("%s.ToDictionary(%s)", obj, g.emitExpr(call.Args[0])), true
	case "ToList":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.ToList()", obj), true
	case "ForEach":
		return fmt.Sprintf("%s.ForEach(%s)", obj, g.emitExpr(call.Args[0])), true

	// --- List property-like methods ------------------------------------------

	case "Contains":
		return fmt.Sprintf("%s.Contains(%s)", obj, g.emitExpr(call.Args[0])), true

	// --- String methods ------------------------------------------------------

	case "ToUpper":
		return fmt.Sprintf("%s.ToUpper()", obj), true
	case "ToLower":
		return fmt.Sprintf("%s.ToLower()", obj), true
	case "StartsWith":
		return fmt.Sprintf("%s.StartsWith(%s)", obj, g.emitExpr(call.Args[0])), true
	case "EndsWith":
		return fmt.Sprintf("%s.EndsWith(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Trim":
		return fmt.Sprintf("%s.Trim()", obj), true
	case "Split":
		return fmt.Sprintf("%s.Split(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Replace":
		return fmt.Sprintf("%s.Replace(%s, %s)", obj, g.emitExpr(call.Args[0]), g.emitExpr(call.Args[1])), true
	case "Substring":
		if len(call.Args) == 2 {
			return fmt.Sprintf("%s.Substring(%s, %s)", obj, g.emitExpr(call.Args[0]), g.emitExpr(call.Args[1])), true
		}
		return fmt.Sprintf("%s.Substring(%s)", obj, g.emitExpr(call.Args[0])), true
	case "IndexOf":
		return fmt.Sprintf("%s.IndexOf(%s)", obj, g.emitExpr(call.Args[0])), true
	case "Join":
		return fmt.Sprintf("string.Join(%s, %s)", g.emitExpr(call.Args[0]), obj), true
	case "Length":
		return fmt.Sprintf("%s.Length", obj), true

	// --- Map methods ---------------------------------------------------------

	case "Keys":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.Keys.ToList()", obj), true
	case "Values":
		g.neededUsings["System.Linq"] = true
		return fmt.Sprintf("%s.Values.ToList()", obj), true
	case "ContainsKey":
		return fmt.Sprintf("%s.ContainsKey(%s)", obj, g.emitExpr(call.Args[0])), true
	case "ContainsValue":
		return fmt.Sprintf("%s.ContainsValue(%s)", obj, g.emitExpr(call.Args[0])), true
	}
	return "", false
}

func (g *Generator) emitLambda(e *parser.LambdaExpr) string {
	var params []string
	for _, p := range e.Params {
		params = append(params, p.Name)
	}
	paramStr := strings.Join(params, ", ")
	if len(params) != 1 {
		paramStr = "(" + paramStr + ")"
	}

	if e.Expr != nil {
		return fmt.Sprintf("%s => %s", paramStr, g.emitExpr(e.Expr))
	}
	// Block-body lambda
	var body strings.Builder
	body.WriteString(fmt.Sprintf("%s =>\n", paramStr))
	body.WriteString(strings.Repeat("    ", g.indent) + "{\n")
	g.indent++
	oldBuf := g.buf
	g.buf = strings.Builder{}
	g.emitBlock(e.Body)
	body.WriteString(g.buf.String())
	g.buf = oldBuf
	g.indent--
	body.WriteString(strings.Repeat("    ", g.indent) + "}")
	return body.String()
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
	return fmt.Sprintf(`$"%s"`, strings.Join(parts, ""))
}

// --- Type mapping ------------------------------------------------------------

func (g *Generator) emitType(t parser.TypeExpr) string {
	if t == nil {
		return "void"
	}
	switch t := t.(type) {
	case *parser.SimpleType:
		return g.mapSimpleType(t.Name)
	case *parser.GenericType:
		return g.mapGenericType(t)
	case *parser.OptionalType:
		inner := g.emitType(t.Inner)
		return inner + "?"
	case *parser.FuncTypeExpr:
		if t.ReturnType == nil {
			var paramTypes []string
			for _, p := range t.Params {
				paramTypes = append(paramTypes, g.emitType(p))
			}
			return fmt.Sprintf("Action<%s>", strings.Join(paramTypes, ", "))
		}
		var paramTypes []string
		for _, p := range t.Params {
			paramTypes = append(paramTypes, g.emitType(p))
		}
		paramTypes = append(paramTypes, g.emitType(t.ReturnType))
		return fmt.Sprintf("Func<%s>", strings.Join(paramTypes, ", "))
	default:
		return "object"
	}
}

func (g *Generator) mapSimpleType(name string) string {
	switch name {
	case "Int":
		return "int"
	case "Float":
		return "double"
	case "String":
		return "string"
	case "Bool":
		return "bool"
	case "Byte":
		return "byte"
	case "Error":
		return "Exception"
	case "Any":
		return "object"
	default:
		if g.interfaceNames[name] {
			return "I" + name
		}
		return name
	}
}

func (g *Generator) mapGenericType(t *parser.GenericType) string {
	g.neededUsings["System.Collections.Generic"] = true
	switch t.Name {
	case "List":
		if len(t.TypeArgs) > 0 {
			return fmt.Sprintf("List<%s>", g.emitType(t.TypeArgs[0]))
		}
		return "List<object>"
	case "Map":
		if len(t.TypeArgs) >= 2 {
			return fmt.Sprintf("Dictionary<%s, %s>", g.emitType(t.TypeArgs[0]), g.emitType(t.TypeArgs[1]))
		}
		return "Dictionary<object, object>"
	case "Chan":
		g.neededUsings["System.Threading.Channels"] = true
		if len(t.TypeArgs) > 0 {
			return fmt.Sprintf("Channel<%s>", g.emitType(t.TypeArgs[0]))
		}
		return "Channel<object>"
	case "Fn":
		// Fn<(Int, String), Bool> → Func<int, string, bool>
		return g.emitType(t) // handled by FuncTypeExpr
	default:
		// User generic type
		var args []string
		for _, a := range t.TypeArgs {
			args = append(args, g.emitType(a))
		}
		base := t.Name
		if g.interfaceNames[base] {
			base = "I" + base
		}
		return fmt.Sprintf("%s<%s>", base, strings.Join(args, ", "))
	}
}

// --- Builtin functions -------------------------------------------------------

// emitBuiltinCall maps a Zinc global builtin function to its C# equivalent.
// Returns the C# code and true if the name is a known builtin, or ("", false).
func (g *Generator) emitBuiltinCall(name string, args []parser.Expr, typeArgs []string) (string, bool) {
	argStrs := make([]string, len(args))
	for i, a := range args {
		argStrs[i] = g.emitExpr(a)
	}
	argStr := strings.Join(argStrs, ", ")

	switch name {
	// I/O
	case "readLine":
		return "Console.ReadLine()", true

	// Conversions
	case "toString":
		return fmt.Sprintf("(%s).ToString()", argStrs[0]), true
	case "toInt", "parseInt":
		return fmt.Sprintf("int.Parse(%s)", argStrs[0]), true
	case "toFloat", "parseFloat":
		return fmt.Sprintf("double.Parse(%s)", argStrs[0]), true
	case "toBool":
		return fmt.Sprintf("bool.Parse(%s)", argStrs[0]), true

	// Inspect
	case "typeOf":
		return fmt.Sprintf("(%s).GetType().Name", argStrs[0]), true

	// Math
	case "abs":
		return fmt.Sprintf("Math.Abs(%s)", argStrs[0]), true
	case "sqrt":
		return fmt.Sprintf("Math.Sqrt(%s)", argStrs[0]), true
	case "pow":
		return fmt.Sprintf("Math.Pow(%s)", argStr), true
	case "floor":
		return fmt.Sprintf("Math.Floor(%s)", argStrs[0]), true
	case "ceil":
		return fmt.Sprintf("Math.Ceiling(%s)", argStrs[0]), true
	case "round":
		return fmt.Sprintf("Math.Round(%s)", argStrs[0]), true
	case "max":
		return fmt.Sprintf("Math.Max(%s)", argStr), true
	case "min":
		return fmt.Sprintf("Math.Min(%s)", argStr), true

	// Control
	case "panic":
		return fmt.Sprintf("throw new Exception(%s)", argStrs[0]), true
	case "exit":
		return fmt.Sprintf("Environment.Exit(%s)", argStrs[0]), true

	// Environment
	case "getEnv":
		return fmt.Sprintf("Environment.GetEnvironmentVariable(%s)", argStrs[0]), true
	case "setEnv":
		return fmt.Sprintf("Environment.SetEnvironmentVariable(%s)", argStr), true

	// Time
	case "now":
		return "DateTime.Now.ToString()", true
	case "sleep":
		g.neededUsings["System.Threading"] = true
		return fmt.Sprintf("Thread.Sleep(%s)", argStrs[0]), true

	// String formatting
	case "sprintf":
		return fmt.Sprintf("string.Format(%s)", argStr), true

	// JSON
	case "jsonEncode":
		g.neededUsings["System.Text.Json"] = true
		return fmt.Sprintf("JsonSerializer.Serialize(%s)", argStrs[0]), true
	case "jsonDecode":
		g.neededUsings["System.Text.Json"] = true
		if len(typeArgs) > 0 {
			csType := g.mapSimpleType(typeArgs[0])
			return fmt.Sprintf("JsonSerializer.Deserialize<%s>(%s)", csType, argStrs[0]), true
		}
		return fmt.Sprintf("JsonSerializer.Deserialize<object>(%s)", argStrs[0]), true

	// File I/O
	case "readFile":
		g.neededUsings["System.IO"] = true
		return fmt.Sprintf("File.ReadAllText(%s)", argStrs[0]), true
	case "writeFile":
		g.neededUsings["System.IO"] = true
		return fmt.Sprintf("File.WriteAllText(%s)", argStr), true

	// HTTP
	case "httpGet":
		g.neededUsings["System.Net.Http"] = true
		return fmt.Sprintf("new HttpClient().GetStringAsync(%s).Result", argStrs[0]), true
	}

	return "", false
}

// callIsFailable checks whether a call expression calls a failable function.
func (g *Generator) callIsFailable(call *parser.CallExpr) bool {
	if ident, ok := call.Callee.(*parser.Ident); ok {
		return g.canThrowFns[ident.Name] || failableBuiltins[ident.Name]
	}
	return false
}

// bodyIsFailable checks if a block body contains any failable calls.
func (g *Generator) bodyIsFailable(body *parser.BlockStmt) bool {
	if body == nil {
		return false
	}
	for _, s := range body.Stmts {
		if g.stmtIsFailable(s) {
			return true
		}
	}
	return false
}

func (g *Generator) stmtIsFailable(s parser.Stmt) bool {
	switch st := s.(type) {
	case *parser.ExprStmt:
		return g.exprIsFailable(st.Expr)
	case *parser.VarStmt:
		if st.Value != nil {
			return g.exprIsFailable(st.Value)
		}
	case *parser.AssignStmt:
		return g.exprIsFailable(st.Value)
	case *parser.BlockStmt:
		return g.bodyIsFailable(st)
	case *parser.IfStmt:
		if g.bodyIsFailable(st.Then) {
			return true
		}
		if st.ElseStmt != nil {
			if b, ok := st.ElseStmt.(*parser.BlockStmt); ok {
				return g.bodyIsFailable(b)
			}
			if i, ok := st.ElseStmt.(*parser.IfStmt); ok {
				return g.stmtIsFailable(i)
			}
		}
	case *parser.ForStmt:
		return g.bodyIsFailable(st.Body)
	case *parser.WhileStmt:
		return g.bodyIsFailable(st.Body)
	case *parser.MatchStmt:
		for _, c := range st.Cases {
			if g.bodyIsFailable(c.Body) {
				return true
			}
		}
	}
	return false
}

func (g *Generator) exprIsFailable(e parser.Expr) bool {
	if e == nil {
		return false
	}
	call, ok := e.(*parser.CallExpr)
	if !ok {
		return false
	}
	return g.callIsFailable(call)
}

// handlerHasHalt checks if the last statement in a handler is return/exit/panic.
func (g *Generator) handlerHasHalt(body *parser.BlockStmt) bool {
	if body == nil || len(body.Stmts) == 0 {
		return false
	}
	last := body.Stmts[len(body.Stmts)-1]
	if _, ok := last.(*parser.ReturnStmt); ok {
		return true
	}
	if es, ok := last.(*parser.ExprStmt); ok {
		if call, ok := es.Expr.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				return ident.Name == "exit" || ident.Name == "panic"
			}
		}
	}
	return false
}

// emitFailableExprStmt emits a standalone failable call with an or-handler as ExprStmt.
func (g *Generator) emitFailableExprStmt(call *parser.CallExpr, handler *parser.OrHandler) {
	exVar := g.nextErr()
	val := g.emitExpr(call)
	g.writeln("try")
	g.writeln("{")
	g.push()
	g.writeln(fmt.Sprintf("%s;", val))
	g.pop()
	g.writeln("}")
	g.writeln(fmt.Sprintf("catch (Exception %s)", exVar))
	g.writeln("{")
	g.push()
	g.writeln(fmt.Sprintf("var err = %s.Message;", exVar))
	if handler.Body != nil && len(handler.Body.Stmts) > 0 {
		g.emitBlock(handler.Body)
	}
	if !g.handlerHasHalt(handler.Body) {
		g.writeln("throw;")
	}
	g.pop()
	g.writeln("}")
}

// --- Helpers -----------------------------------------------------------------

// parentHasMethod checks if any parent class defines a method with the given name.
func (g *Generator) parentHasMethod(parents []string, methodName string) bool {
	for _, p := range parents {
		if g.interfaceNames[p] {
			continue // interface methods are implemented, not overridden
		}
		if methods, ok := g.classMethods[p]; ok {
			if methods[methodName] {
				return true
			}
		}
	}
	return false
}

// isOverriddenByChild checks if any child class overrides a method from this class.
func (g *Generator) isOverriddenByChild(className, methodName string) bool {
	for childName, parents := range g.classParents {
		for _, p := range parents {
			if p == className {
				if methods, ok := g.classMethods[childName]; ok {
					if methods[methodName] {
						return true
					}
				}
			}
		}
	}
	return false
}

// inferListElemType infers the C# element type from list literal contents.
func (g *Generator) inferListElemType(e *parser.ListLit) string {
	if len(e.Elements) == 0 {
		return "object"
	}
	return g.inferExprCSharpType(e.Elements[0])
}

// inferMapTypes infers C# key and value types from map literal contents.
func (g *Generator) inferMapTypes(e *parser.MapLit) (string, string) {
	if len(e.Keys) == 0 {
		return "object", "object"
	}
	return g.inferExprCSharpType(e.Keys[0]), g.inferExprCSharpType(e.Values[0])
}

// inferExprCSharpType returns the C# type of a literal expression.
func (g *Generator) inferExprCSharpType(e parser.Expr) string {
	switch e.(type) {
	case *parser.IntLit:
		return "int"
	case *parser.FloatLit:
		return "double"
	case *parser.StringLit, *parser.RawStringLit, *parser.StringInterpLit:
		return "string"
	case *parser.BoolLit:
		return "bool"
	default:
		return "object"
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (g *Generator) fieldName(name string, isPublic bool) string {
	if isPublic {
		return capitalize(name)
	}
	// Private fields: prefix with underscore (C# convention)
	return "_" + name
}

func (g *Generator) formatParams(params []*parser.ParamDecl) string {
	var parts []string
	for _, p := range params {
		paramType := g.emitType(p.Type)
		if p.Variadic {
			parts = append(parts, fmt.Sprintf("params %s[] %s", paramType, p.Name))
		} else if p.Default != nil {
			parts = append(parts, fmt.Sprintf("%s %s = %s", paramType, p.Name, g.emitExpr(p.Default)))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s", paramType, p.Name))
		}
	}
	return strings.Join(parts, ", ")
}

func (g *Generator) formatTypeParams(typeParams []string) string {
	if len(typeParams) == 0 {
		return ""
	}
	return fmt.Sprintf("<%s>", strings.Join(typeParams, ", "))
}
