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

package codegen_java

import (
	"fmt"
	"strings"

	"zinc/internal/parser"
)

// Generator produces Java source from a Zinc AST.
type Generator struct {
	buf              strings.Builder
	indent           int
	className        string // derived from filename or "Main"
	pendingAccessors []fieldAccessor
}

// New creates a new Java code generator.
func New() *Generator {
	return &Generator{}
}

// Generate produces Java source from a Zinc v2 AST.
// className is derived from the source filename (e.g., "script.zn" -> "Script").
func (g *Generator) Generate(prog *parser.Program, className string) string {
	g.buf.Reset()
	g.indent = 0
	g.className = className

	// Collect imports
	g.emitImports(prog.Imports)

	// Open class wrapper
	g.writeln("public class %s {", className)
	g.indent++

	// Top-level declarations (functions, classes, data, enums)
	for _, d := range prog.Decls {
		g.emitDecl(d)
		g.writeln("")
	}

	// Script-mode: top-level statements → main()
	if len(prog.Stmts) > 0 {
		g.writeln("public static void main(String[] args) {")
		g.indent++
		for _, s := range prog.Stmts {
			g.emitStmt(s)
		}
		g.indent--
		g.writeln("}")
	}

	g.indent--
	g.writeln("}")

	return g.buf.String()
}

// --- Imports -----------------------------------------------------------------

func (g *Generator) emitImports(imports []*parser.ImportDecl) {
	for _, imp := range imports {
		g.writeln("import %s;", imp.Path)
	}
	if len(imports) > 0 {
		g.writeln("")
	}
}

// --- Declarations ------------------------------------------------------------

func (g *Generator) emitDecl(d parser.TopLevelDecl) {
	switch decl := d.(type) {
	case *parser.FnDecl:
		g.emitFnDecl(decl)
	case *parser.ClassDecl:
		g.emitClassDecl(decl)
	case *parser.DataClassDecl:
		g.emitDataClassDecl(decl)
	case *parser.EnumDecl:
		g.emitEnumDecl(decl)
	case *parser.InterfaceDecl:
		g.emitInterfaceDecl(decl)
	case *parser.ConstDecl:
		g.emitConstDecl(decl)
	}
}

// --- Functions ---------------------------------------------------------------

func (g *Generator) emitFnDecl(fn *parser.FnDecl) {
	for _, a := range fn.Annotations {
		g.writeln("@%s", g.formatAnnotation(a))
	}
	vis := "public "
	ret := "void"
	if fn.ReturnType != nil {
		ret = g.formatType(fn.ReturnType)
	}
	params := g.formatParams(fn.Params)

	typeParams := ""
	if len(fn.TypeParams) > 0 {
		typeParams = "<" + strings.Join(fn.TypeParams, ", ") + "> "
	}

	g.writeln("%sstatic %s%s %s(%s) {", vis, typeParams, ret, fn.Name, params)
	g.indent++
	g.emitBlock(fn.Body)
	g.indent--
	g.writeln("}")
}

// --- Classes -----------------------------------------------------------------

func (g *Generator) emitClassDecl(cls *parser.ClassDecl) {
	for _, a := range cls.Annotations {
		g.writeln("@%s", g.formatAnnotation(a))
	}

	ext := ""
	if len(cls.Parents) > 0 {
		// First parent is extends, rest are implements
		ext = " extends " + cls.Parents[0]
		if len(cls.Parents) > 1 {
			ext += " implements " + strings.Join(cls.Parents[1:], ", ")
		}
	}

	typeParams := ""
	if len(cls.TypeParams) > 0 {
		typeParams = "<" + strings.Join(cls.TypeParams, ", ") + ">"
	}

	g.writeln("public static class %s%s%s {", cls.Name, typeParams, ext)
	g.indent++

	// Fields
	g.pendingAccessors = nil
	for _, f := range cls.Fields {
		g.emitFieldDecl(f)
	}
	if len(cls.Fields) > 0 {
		g.writeln("")
	}

	// Constructor
	if cls.Ctor != nil {
		g.emitCtor(cls.Name, cls.Ctor, cls.Parents)
	}

	// Getters/setters
	g.emitAccessors()

	// Methods
	for _, m := range cls.Methods {
		g.emitMethodDecl(m)
		g.writeln("")
	}

	g.indent--
	g.writeln("}")
}

func (g *Generator) emitFieldDecl(f *parser.FieldDecl) {
	typeName := "Object"
	if f.Type != nil {
		typeName = g.formatType(f.Type)
	}

	// const → public static final
	if f.IsConst {
		if f.Default != nil {
			g.writeln("public static final %s %s = %s;", typeName, f.Name, g.formatExpr(f.Default))
		} else {
			g.writeln("public static final %s %s;", typeName, f.Name)
		}
		return
	}

	// init → private final + getter
	if f.IsInit {
		g.writeln("private final %s %s;", typeName, f.Name)
		g.pendingAccessors = append(g.pendingAccessors, fieldAccessor{f.Name, typeName, true, false})
		return
	}

	// All fields are private — always
	if f.Default != nil {
		g.writeln("private %s %s = %s;", typeName, f.Name, g.formatExpr(f.Default))
	} else {
		g.writeln("private %s %s;", typeName, f.Name)
	}

	// pub → getter + setter, read → getter only
	if f.IsPub {
		g.pendingAccessors = append(g.pendingAccessors, fieldAccessor{f.Name, typeName, true, true})
	} else if f.IsReadonly {
		g.pendingAccessors = append(g.pendingAccessors, fieldAccessor{f.Name, typeName, true, false})
	}
}

type fieldAccessor struct {
	name     string
	typeName string
	getter   bool
	setter   bool
}

func (g *Generator) emitAccessors() {
	if len(g.pendingAccessors) == 0 {
		return
	}
	g.writeln("")
	for _, a := range g.pendingAccessors {
		capName := strings.ToUpper(a.name[:1]) + a.name[1:]
		if a.getter {
			g.writeln("public %s get%s() { return this.%s; }", a.typeName, capName, a.name)
		}
		if a.setter {
			g.writeln("public void set%s(%s %s) { this.%s = %s; }", capName, a.typeName, a.name, a.name, a.name)
		}
	}
	g.pendingAccessors = nil
}

func (g *Generator) emitCtor(className string, ctor *parser.CtorDecl, parents []string) {
	params := g.formatParams(ctor.Params)
	g.writeln("public %s(%s) {", className, params)
	g.indent++
	if len(ctor.SuperArgs) > 0 {
		args := g.formatExprList(ctor.SuperArgs)
		g.writeln("super(%s);", args)
	}
	g.emitBlock(ctor.Body)
	g.indent--
	g.writeln("}")
	g.writeln("")
}

func (g *Generator) emitMethodDecl(m *parser.MethodDecl) {
	for _, a := range m.Annotations {
		g.writeln("@%s", g.formatAnnotation(a))
	}

	vis := "private"
	if m.IsPub {
		vis = "public"
	}
	static := ""
	if m.IsStatic {
		static = "static "
	}
	ret := "void"
	if m.ReturnType != nil {
		ret = g.formatType(m.ReturnType)
	}
	params := g.formatParams(m.Params)

	g.writeln("%s %s%s %s(%s) {", vis, static, ret, m.Name, params)
	g.indent++
	g.emitBlock(m.Body)
	g.indent--
	g.writeln("}")
}

// --- Data Classes (Records) --------------------------------------------------

func (g *Generator) emitDataClassDecl(d *parser.DataClassDecl) {
	// Data classes → Java records
	var fields []string
	for _, f := range d.Params {
		typeName := "Object"
		if f.Type != nil {
			typeName = g.formatType(f.Type)
		}
		fields = append(fields, typeName+" "+f.Name)
	}

	typeParams := ""
	if len(d.TypeParams) > 0 {
		typeParams = "<" + strings.Join(d.TypeParams, ", ") + ">"
	}

	ext := ""
	if len(d.Parents) > 0 {
		ext = " implements " + strings.Join(d.Parents, ", ")
	}

	g.writeln("public record %s%s(%s)%s {", d.Name, typeParams, strings.Join(fields, ", "), ext)
	g.indent++

	// Methods inside data class
	for _, m := range d.Methods {
		g.emitMethodDecl(m)
		g.writeln("")
	}

	g.indent--
	g.writeln("}")
}

// --- Enums -------------------------------------------------------------------

func (g *Generator) emitEnumDecl(e *parser.EnumDecl) {
	g.writeln("public enum %s {", e.Name)
	g.indent++
	for i, v := range e.Variants {
		sep := ","
		if i == len(e.Variants)-1 {
			sep = ";"
		}
		g.writeln("%s%s", v, sep)
	}
	g.indent--
	g.writeln("}")
}

// --- Interfaces --------------------------------------------------------------

func (g *Generator) emitInterfaceDecl(iface *parser.InterfaceDecl) {
	g.writeln("public interface %s {", iface.Name)
	g.indent++
	for _, m := range iface.Methods {
		ret := "void"
		if m.ReturnType != nil {
			ret = g.formatType(m.ReturnType)
		}
		params := g.formatParams(m.Params)
		g.writeln("%s %s(%s);", ret, m.Name, params)
	}
	g.indent--
	g.writeln("}")
}

// --- Constants ---------------------------------------------------------------

func (g *Generator) emitConstDecl(c *parser.ConstDecl) {
	typeName := "var"
	if c.Type != nil {
		typeName = g.formatType(c.Type)
	}
	g.writeln("public static final %s %s = %s;", typeName, c.Name, g.formatExpr(c.Value))
}

// --- Statements --------------------------------------------------------------

func (g *Generator) emitStmt(s parser.Stmt) {
	switch stmt := s.(type) {
	case *parser.VarStmt:
		g.emitVarStmt(stmt)
	case *parser.AssignStmt:
		g.emitAssignStmt(stmt)
	case *parser.ReturnStmt:
		if stmt.Value != nil {
			g.writeln("return %s;", g.formatExpr(stmt.Value))
		} else {
			g.writeln("return;")
		}
	case *parser.IfStmt:
		g.emitIfStmt(stmt)
	case *parser.ForStmt:
		g.emitForStmt(stmt)
	case *parser.WhileStmt:
		g.writeln("while (%s) {", g.formatExpr(stmt.Cond))
		g.indent++
		g.emitBlock(stmt.Body)
		g.indent--
		g.writeln("}")
	case *parser.MatchStmt:
		g.emitMatchStmt(stmt)
	case *parser.ExprStmt:
		g.writeln("%s;", g.formatExpr(stmt.Expr))
	case *parser.PrintStmt:
		g.writeln("System.out.println(%s);", g.formatExpr(stmt.Value))
	case *parser.TryStmt:
		g.emitTryStmt(stmt)
	case *parser.RaiseStmt:
		g.emitRaiseStmt(stmt)
	case *parser.BreakStmt:
		g.writeln("break;")
	case *parser.ContinueStmt:
		g.writeln("continue;")
	case *parser.BlockStmt:
		g.emitBlock(stmt)
	case *parser.FnDecl:
		// Nested function → local method (Java doesn't support this directly,
		// but we can emit a lambda or a static inner method)
		g.emitFnDecl(stmt)
	case *parser.TupleVarStmt:
		g.emitTupleVarStmt(stmt)
	case *parser.AssertStmt:
		g.emitAssertStmt(stmt)
	}
}

func (g *Generator) emitVarStmt(v *parser.VarStmt) {
	keyword := "var"
	if v.IsConst {
		keyword = "final var"
	}
	if v.Type != nil {
		typeName := g.formatType(v.Type)
		if v.IsConst {
			keyword = "final " + typeName
		} else {
			keyword = typeName
		}
	}
	if v.Value != nil {
		g.writeln("%s %s = %s;", keyword, v.Name, g.formatExpr(v.Value))
	} else {
		g.writeln("%s %s;", keyword, v.Name)
	}
}

func (g *Generator) emitAssignStmt(a *parser.AssignStmt) {
	g.writeln("%s %s %s;", g.formatExpr(a.Target), a.Op, g.formatExpr(a.Value))
}

func (g *Generator) emitIfStmt(s *parser.IfStmt) {
	g.writeln("if (%s) {", g.formatExpr(s.Cond))
	g.indent++
	g.emitBlock(s.Then)
	g.indent--
	if s.ElseStmt != nil {
		switch e := s.ElseStmt.(type) {
		case *parser.IfStmt:
			g.write("} else ")
			g.emitIfStmt(e)
			return
		case *parser.BlockStmt:
			g.writeln("} else {")
			g.indent++
			g.emitBlock(e)
			g.indent--
		}
	}
	g.writeln("}")
}

func (g *Generator) emitForStmt(f *parser.ForStmt) {
	if f.IsRange {
		g.writeln("for (var %s : %s) {", f.Item, g.formatExpr(f.Range))
	} else {
		init := ""
		if f.Init != nil {
			init = g.formatStmtInline(f.Init)
		}
		cond := ""
		if f.Cond != nil {
			cond = g.formatExpr(f.Cond)
		}
		post := ""
		if f.Post != nil {
			post = g.formatStmtInline(f.Post)
		}
		g.writeln("for (%s; %s; %s) {", init, cond, post)
	}
	g.indent++
	g.emitBlock(f.Body)
	g.indent--
	g.writeln("}")
}

func (g *Generator) emitMatchStmt(m *parser.MatchStmt) {
	g.writeln("switch (%s) {", g.formatExpr(m.Subject))
	g.indent++
	for _, c := range m.Cases {
		if c.Pattern == nil {
			g.writeln("default -> {")
		} else {
			g.writeln("case %s -> {", g.formatExpr(c.Pattern))
		}
		g.indent++
		g.emitBlock(c.Body)
		g.indent--
		g.writeln("}")
	}
	g.indent--
	g.writeln("}")
}

func (g *Generator) emitTryStmt(t *parser.TryStmt) {
	g.writeln("try {")
	g.indent++
	g.emitBlock(t.Body)
	g.indent--
	catchType := "Exception"
	if t.CatchType != "" {
		catchType = t.CatchType
	}
	catchName := "e"
	if t.CatchName != "" {
		catchName = t.CatchName
	}
	g.writeln("} catch (%s %s) {", catchType, catchName)
	g.indent++
	g.emitBlock(t.CatchBody)
	g.indent--
	g.writeln("}")
}

func (g *Generator) emitRaiseStmt(r *parser.RaiseStmt) {
	g.writeln("throw %s;", g.formatExpr(r.Value))
}

func (g *Generator) emitTupleVarStmt(t *parser.TupleVarStmt) {
	// Java has no tuple unpacking — assign to temp, extract
	g.writeln("var _tuple = %s;", g.formatExpr(t.Value))
	for i, name := range t.Names {
		g.writeln("var %s = _tuple.get%d();", name, i)
	}
}

func (g *Generator) emitAssertStmt(a *parser.AssertStmt) {
	if a.Message != nil {
		g.writeln("assert %s : %s;", g.formatExpr(a.Cond), g.formatExpr(a.Message))
	} else {
		g.writeln("assert %s;", g.formatExpr(a.Cond))
	}
}

func (g *Generator) emitBlock(block *parser.BlockStmt) {
	if block == nil {
		return
	}
	for _, s := range block.Stmts {
		g.emitStmt(s)
	}
}

// --- Expressions -------------------------------------------------------------

func (g *Generator) formatExpr(e parser.Expr) string {
	switch expr := e.(type) {
	case *parser.Ident:
		return expr.Name
	case *parser.IntLit:
		return expr.Value
	case *parser.FloatLit:
		return expr.Value
	case *parser.StringLit:
		return fmt.Sprintf("\"%s\"", expr.Value)
	case *parser.StringInterpLit:
		return g.formatStringInterp(expr)
	case *parser.BoolLit:
		if expr.Value {
			return "true"
		}
		return "false"
	case *parser.NullLit:
		return "null"
	case *parser.BinaryExpr:
		return g.formatBinaryExpr(expr)
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", expr.Op, g.formatExpr(expr.Operand))
	case *parser.CallExpr:
		return g.formatCallExpr(expr)
	case *parser.SelectorExpr:
		return fmt.Sprintf("%s.%s", g.formatExpr(expr.Object), expr.Field)
	case *parser.IndexExpr:
		return fmt.Sprintf("%s.get(%s)", g.formatExpr(expr.Object), g.formatExpr(expr.Index))
	case *parser.ListLit:
		if len(expr.Elements) == 0 {
			return "new java.util.ArrayList<>()"
		}
		elems := g.formatExprList(expr.Elements)
		return fmt.Sprintf("new java.util.ArrayList<>(java.util.List.of(%s))", elems)
	case *parser.MapLit:
		if len(expr.Keys) == 0 {
			return "new java.util.HashMap<>()"
		}
		var pairs []string
		for i := range expr.Keys {
			pairs = append(pairs, fmt.Sprintf("%s, %s", g.formatExpr(expr.Keys[i]), g.formatExpr(expr.Values[i])))
		}
		return fmt.Sprintf("new java.util.HashMap<>(java.util.Map.of(%s))", strings.Join(pairs, ", "))
	case *parser.LambdaExpr:
		return g.formatLambdaExpr(expr)
	case *parser.ThisExpr:
		return "this"
	case *parser.SuperCallExpr:
		return fmt.Sprintf("super(%s)", g.formatExprList(expr.Args))
	case *parser.TypeAssertExpr:
		if expr.IsCheck {
			return fmt.Sprintf("%s instanceof %s", g.formatExpr(expr.Object), expr.TypeName)
		}
		return fmt.Sprintf("(%s) %s", expr.TypeName, g.formatExpr(expr.Object))
	case *parser.SafeNavExpr:
		obj := g.formatExpr(expr.Object)
		if expr.Call != nil {
			args := g.formatExprList(expr.Call.Args)
			return fmt.Sprintf("(%s != null ? %s.%s(%s) : null)", obj, obj, expr.Field, args)
		}
		return fmt.Sprintf("(%s != null ? %s.%s : null)", obj, obj, expr.Field)
	case *parser.SpawnExpr:
		return "Thread.startVirtualThread(() -> { /* spawn body */ })"
	case *parser.IfExpr:
		return fmt.Sprintf("(%s ? %s : %s)", g.formatExpr(expr.Cond), g.formatExpr(expr.Then), g.formatExpr(expr.Else))
	case *parser.RangeExpr:
		// Java has no range literal — use IntStream
		if expr.Inclusive {
			return fmt.Sprintf("java.util.stream.IntStream.rangeClosed(%s, %s)", g.formatExpr(expr.Start), g.formatExpr(expr.End))
		}
		return fmt.Sprintf("java.util.stream.IntStream.range(%s, %s)", g.formatExpr(expr.Start), g.formatExpr(expr.End))
	default:
		return "/* unknown expr */"
	}
}

func (g *Generator) formatBinaryExpr(b *parser.BinaryExpr) string {
	left := g.formatExpr(b.Left)
	right := g.formatExpr(b.Right)

	switch b.Op {
	case "and":
		return fmt.Sprintf("%s && %s", left, right)
	case "or":
		return fmt.Sprintf("%s || %s", left, right)
	case "not":
		return fmt.Sprintf("!%s", right)
	case "**":
		return fmt.Sprintf("Math.pow(%s, %s)", left, right)
	case "==":
		// Structural equality (Kotlin convention)
		return fmt.Sprintf("java.util.Objects.equals(%s, %s)", left, right)
	case "!=":
		return fmt.Sprintf("!java.util.Objects.equals(%s, %s)", left, right)
	case "===":
		// Reference identity
		return fmt.Sprintf("%s == %s", left, right)
	case "!==":
		return fmt.Sprintf("%s != %s", left, right)
	case "in":
		return fmt.Sprintf("%s.contains(%s)", right, left)
	case "not in":
		return fmt.Sprintf("!%s.contains(%s)", right, left)
	case "is":
		return fmt.Sprintf("%s instanceof %s", left, right)
	case "is not":
		return fmt.Sprintf("!(%s instanceof %s)", left, right)
	default:
		return fmt.Sprintf("%s %s %s", left, b.Op, right)
	}
}

func (g *Generator) formatCallExpr(c *parser.CallExpr) string {
	callee := g.formatExpr(c.Callee)
	args := g.formatExprList(c.Args)
	// Add named args as positional (Java doesn't have named args)
	for _, na := range c.NamedArgs {
		if args != "" {
			args += ", "
		}
		args += g.formatExpr(na.Value)
	}

	// Map Zinc builtins to Java equivalents
	switch callee {
	case "print":
		return fmt.Sprintf("System.out.println(%s)", args)
	case "len":
		return fmt.Sprintf("%s.size()", args)
	case "range":
		return fmt.Sprintf("java.util.stream.IntStream.range(0, %s).boxed().toList()", args)
	case "input":
		return fmt.Sprintf("System.console().readLine(%s)", args)
	case "str":
		return fmt.Sprintf("String.valueOf(%s)", args)
	case "int":
		return fmt.Sprintf("Integer.parseInt(%s)", args)
	case "float":
		return fmt.Sprintf("Double.parseDouble(%s)", args)
	}

	return fmt.Sprintf("%s(%s)", callee, args)
}

func (g *Generator) formatLambdaExpr(l *parser.LambdaExpr) string {
	params := g.formatLambdaParams(l.Params)
	if l.Expr != nil {
		return fmt.Sprintf("%s -> %s", params, g.formatExpr(l.Expr))
	}
	// Block lambda
	var stmts []string
	if l.Body != nil {
		for _, s := range l.Body.Stmts {
			stmts = append(stmts, g.formatStmtInline(s))
		}
	}
	return fmt.Sprintf("%s -> { %s }", params, strings.Join(stmts, " "))
}

func (g *Generator) formatStringInterp(s *parser.StringInterpLit) string {
	var parts []string
	for _, p := range s.Parts {
		switch part := p.(type) {
		case *parser.StringLit:
			parts = append(parts, fmt.Sprintf("\"%s\"", part.Value))
		default:
			parts = append(parts, g.formatExpr(part))
		}
	}
	return strings.Join(parts, " + ")
}

// --- Type formatting ---------------------------------------------------------

// zincToJavaType maps Zinc primitive/collection type names to Java equivalents.
var zincToJavaType = map[string]string{
	"int":     "int",
	"double":  "double",
	"String":  "String",
	"boolean": "boolean",
	"char":    "char",
	"long":    "long",
	"byte[]":  "byte[]",
	"List":    "List",
	"Map":     "Map",
	"Set":     "Set",
}

// zincToJavaBoxed maps Zinc primitives to their boxed Java equivalents (for generics).
var zincToJavaBoxed = map[string]string{
	"int":     "Integer",
	"double":  "Double",
	"boolean": "Boolean",
	"char":    "Character",
	"long":    "Long",
}

func (g *Generator) formatType(t parser.TypeExpr) string {
	switch typ := t.(type) {
	case *parser.SimpleType:
		if mapped, ok := zincToJavaType[typ.Name]; ok {
			return mapped
		}
		return typ.Name
	case *parser.GenericType:
		baseName := typ.Name
		if mapped, ok := zincToJavaType[baseName]; ok {
			baseName = mapped
		}
		var args []string
		for _, a := range typ.TypeArgs {
			args = append(args, g.formatTypeBoxed(a))
		}
		return fmt.Sprintf("%s<%s>", baseName, strings.Join(args, ", "))
	case *parser.OptionalType:
		// Nullable in Java — just use the type (compiler tracks nullability)
		return g.formatType(typ.Inner)
	case *parser.FuncTypeExpr:
		// Map to java.util.function interfaces
		if typ.ReturnType == nil {
			if len(typ.Params) == 1 {
				return fmt.Sprintf("java.util.function.Consumer<%s>", g.formatTypeBoxed(typ.Params[0]))
			}
			return "Runnable"
		}
		if len(typ.Params) == 0 {
			return fmt.Sprintf("java.util.function.Supplier<%s>", g.formatTypeBoxed(typ.ReturnType))
		}
		if len(typ.Params) == 1 {
			return fmt.Sprintf("java.util.function.Function<%s, %s>", g.formatTypeBoxed(typ.Params[0]), g.formatTypeBoxed(typ.ReturnType))
		}
		return "Object" // fallback
	default:
		return "Object"
	}
}

// formatTypeBoxed returns the boxed version of a type (for use in generics).
func (g *Generator) formatTypeBoxed(t parser.TypeExpr) string {
	if st, ok := t.(*parser.SimpleType); ok {
		if boxed, ok := zincToJavaBoxed[st.Name]; ok {
			return boxed
		}
	}
	return g.formatType(t)
}

// --- Helpers -----------------------------------------------------------------

func (g *Generator) formatParams(params []*parser.ParamDecl) string {
	var parts []string
	for _, p := range params {
		typeName := "Object"
		if p.Type != nil {
			typeName = g.formatType(p.Type)
		}
		if p.Variadic {
			typeName += "..."
		}
		parts = append(parts, typeName+" "+p.Name)
	}
	return strings.Join(parts, ", ")
}

func (g *Generator) formatLambdaParams(params []*parser.ParamDecl) string {
	if len(params) == 1 && params[0].Type == nil {
		return params[0].Name
	}
	var parts []string
	for _, p := range params {
		if p.Type != nil {
			parts = append(parts, g.formatType(p.Type)+" "+p.Name)
		} else {
			parts = append(parts, p.Name)
		}
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func (g *Generator) formatExprList(exprs []parser.Expr) string {
	var parts []string
	for _, e := range exprs {
		parts = append(parts, g.formatExpr(e))
	}
	return strings.Join(parts, ", ")
}

func (g *Generator) formatAnnotation(a *parser.Annotation) string {
	if len(a.Args) == 0 {
		return a.Name
	}
	return fmt.Sprintf("%s(%s)", a.Name, strings.Join(a.Args, ", "))
}

func (g *Generator) formatStmtInline(s parser.Stmt) string {
	switch stmt := s.(type) {
	case *parser.VarStmt:
		keyword := "var"
		if stmt.Type != nil {
			keyword = g.formatType(stmt.Type)
		}
		if stmt.Value != nil {
			return fmt.Sprintf("%s %s = %s", keyword, stmt.Name, g.formatExpr(stmt.Value))
		}
		return fmt.Sprintf("%s %s", keyword, stmt.Name)
	case *parser.AssignStmt:
		return fmt.Sprintf("%s %s %s", g.formatExpr(stmt.Target), stmt.Op, g.formatExpr(stmt.Value))
	case *parser.ExprStmt:
		return g.formatExpr(stmt.Expr) + ";"
	case *parser.ReturnStmt:
		if stmt.Value != nil {
			return "return " + g.formatExpr(stmt.Value) + ";"
		}
		return "return;"
	default:
		return "/* inline stmt */"
	}
}


// --- Output helpers ----------------------------------------------------------

func (g *Generator) writeln(format string, args ...interface{}) {
	g.buf.WriteString(strings.Repeat("    ", g.indent))
	fmt.Fprintf(&g.buf, format, args...)
	g.buf.WriteString("\n")
}

func (g *Generator) write(format string, args ...interface{}) {
	g.buf.WriteString(strings.Repeat("    ", g.indent))
	fmt.Fprintf(&g.buf, format, args...)
}
