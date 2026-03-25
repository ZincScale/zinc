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
	"zinc/internal/typechecker"
)

// Generator produces Java source from a Zinc AST.
type Generator struct {
	buf                strings.Builder
	indent             int
	className          string // derived from filename or "Main"
	pendingAccessors   []fieldAccessor
	tupleTypes         map[int]bool    // track which Tuple arities we need to generate
	arrayVars          map[string]bool // track variables declared as array types
	interfaces         map[string]bool // track which type names are interfaces
	zincMethods        map[string]map[string]bool // className → methodName → canThrow
	errVarStack        []string        // stack of catch variable names for nested or-blocks
	currentClassParents []string       // parents of the class currently being emitted
}

// New creates a new Java code generator.
func New() *Generator {
	return &Generator{tupleTypes: make(map[int]bool), arrayVars: make(map[string]bool), interfaces: make(map[string]bool), zincMethods: make(map[string]map[string]bool)}
}

// OutputFile represents a generated .java file.
type OutputFile struct {
	Name    string // e.g., "User.java"
	Content string
}

// methodCanThrow walks a method body to determine if it can throw an exception.
// Returns true if the body contains: raise, or-handlers, or calls to other methods.
func methodCanThrow(body *parser.BlockStmt) bool {
	if body == nil {
		return false
	}
	for _, s := range body.Stmts {
		if stmtCanThrow(s) {
			return true
		}
	}
	return false
}

func stmtCanThrow(s parser.Stmt) bool {
	switch s := s.(type) {
	case *parser.RaiseStmt:
		return true
	case *parser.ExprStmt:
		return exprCanThrow(s.Expr)
	case *parser.VarStmt:
		if s.OrHandler != nil {
			return true
		}
		return s.Value != nil && exprCanThrow(s.Value)
	case *parser.AssignStmt:
		return exprCanThrow(s.Value)
	case *parser.ReturnStmt:
		return s.Value != nil && exprCanThrow(s.Value)
	case *parser.IfStmt:
		if exprCanThrow(s.Cond) {
			return true
		}
		if s.Then != nil && methodCanThrow(s.Then) {
			return true
		}
		if s.ElseStmt != nil {
			if elseBlock, ok := s.ElseStmt.(*parser.BlockStmt); ok {
				return methodCanThrow(elseBlock)
			}
			return stmtCanThrow(s.ElseStmt)
		}
		return false
	case *parser.ForStmt:
		return methodCanThrow(s.Body)
	case *parser.WhileStmt:
		return methodCanThrow(s.Body)
	case *parser.BlockStmt:
		return methodCanThrow(s)
	case *parser.PrintStmt:
		return false
	case *parser.BreakStmt, *parser.ContinueStmt:
		return false
	default:
		// Unknown statement type — conservatively assume it can throw
		return true
	}
}

func exprCanThrow(e parser.Expr) bool {
	if e == nil {
		return false
	}
	switch e := e.(type) {
	case *parser.IntLit, *parser.FloatLit, *parser.StringLit, *parser.BoolLit, *parser.NullLit:
		return false
	case *parser.Ident:
		return false
	case *parser.ThisExpr:
		return false
	case *parser.SelectorExpr:
		return exprCanThrow(e.Object)
	case *parser.BinaryExpr:
		return exprCanThrow(e.Left) || exprCanThrow(e.Right)
	case *parser.UnaryExpr:
		return exprCanThrow(e.Operand)
	case *parser.CallExpr:
		return true
	case *parser.IndexExpr:
		return true
	case *parser.LambdaExpr:
		return false
	default:
		return true
	}
}

// collectInterfaces scans declarations to build the set of known interface names
// and collect Zinc class method signatures for throws analysis.
func (g *Generator) collectInterfaces(decls []parser.TopLevelDecl) {
	for _, d := range decls {
		if iface, ok := d.(*parser.InterfaceDecl); ok {
			g.interfaces[iface.Name] = true
		}
	}
	// Collect Zinc class method signatures (canThrow analysis)
	for _, d := range decls {
		if cls, ok := d.(*parser.ClassDecl); ok {
			methods := make(map[string]bool)
			for _, m := range cls.Methods {
				methods[m.Name] = methodCanThrow(m.Body)
			}
			g.zincMethods[cls.Name] = methods
		}
	}
}

// RegisterInterface allows external callers (e.g., multi-file compilation)
// to register interface names discovered in other files.
func (g *Generator) RegisterInterface(name string) {
	g.interfaces[name] = true
}

// buildInheritanceClause builds the extends/implements string for a class.
// It checks each parent against the known interface set.
func (g *Generator) buildInheritanceClause(parents []string) string {
	if len(parents) == 0 {
		return ""
	}

	var extendsNames []string
	var implNames []string
	for _, p := range parents {
		if g.interfaces[p] {
			implNames = append(implNames, p)
		} else {
			extendsNames = append(extendsNames, p)
		}
	}

	var parts []string
	if len(extendsNames) > 0 {
		parts = append(parts, "extends "+strings.Join(extendsNames, ", "))
	}
	if len(implNames) > 0 {
		parts = append(parts, "implements "+strings.Join(implNames, ", "))
	}
	return " " + strings.Join(parts, " ")
}

// Generate produces a single Java source string (all in one class).
// Used by tests and simple single-file transpilation.
func (g *Generator) Generate(prog *parser.Program, className string) string {
	g.buf.Reset()
	g.indent = 0
	g.className = className
	g.collectInterfaces(prog.Decls)

	g.emitPackageAndImports(prog.Package, prog.Imports)

	g.writeln("public class %s {", className)
	g.indent++

	for _, d := range prog.Decls {
		g.emitDecl(d)
		g.writeln("")
	}

	if len(prog.Stmts) > 0 {
		g.writeln("public static void main(String[] args) throws Exception {")
		g.indent++
		for _, s := range prog.Stmts {
			g.emitStmt(s)
		}
		g.indent--
		g.writeln("}")
	}

	// Generate tuple record types used in this file
	g.emitTupleRecords()

	g.indent--
	g.writeln("}")

	return g.buf.String()
}

// emitTupleRecords generates record Tuple2, Tuple3, etc. as static inner classes.
func (g *Generator) emitTupleRecords() {
	for n := range g.tupleTypes {
		g.writeln("")
		var typeParams, fields []string
		for i := 0; i < n; i++ {
			tp := fmt.Sprintf("T%d", i)
			typeParams = append(typeParams, tp)
			fields = append(fields, fmt.Sprintf("%s _%d", tp, i))
		}
		g.writeln("record Tuple%d<%s>(%s) {}", n,
			strings.Join(typeParams, ", "), strings.Join(fields, ", "))
	}
}

// GenerateFiles produces separate .java files for each top-level type.
// Data classes → records, enums, and classes each get their own file.
// Top-level functions and script statements go into the main class file.
func (g *Generator) GenerateFiles(prog *parser.Program, className string) []OutputFile {
	var files []OutputFile
	g.collectInterfaces(prog.Decls)

	// Separate declarations into types (own file) vs functions (main file)
	var fnDecls []parser.TopLevelDecl
	var constDecls []parser.TopLevelDecl

	for _, d := range prog.Decls {
		switch decl := d.(type) {
		case *parser.DataClassDecl:
			g.buf.Reset()
			g.indent = 0
			g.emitPackageAndImports(prog.Package, prog.Imports)
			g.emitDataClassDecl(decl)
			files = append(files, OutputFile{
				Name:    decl.Name + ".java",
				Content: g.buf.String(),
			})
		case *parser.EnumDecl:
			g.buf.Reset()
			g.indent = 0
			g.emitPackageAndImports(prog.Package, prog.Imports)
			g.emitEnumDecl(decl)
			files = append(files, OutputFile{
				Name:    decl.Name + ".java",
				Content: g.buf.String(),
			})
		case *parser.ClassDecl:
			if decl.IsSealed {
				// Sealed class → sealed interface + variant records
				g.buf.Reset()
				g.indent = 0
				g.emitPackageAndImports(prog.Package, prog.Imports)
				g.emitSealedInterface(decl)
				files = append(files, OutputFile{
					Name:    decl.Name + ".java",
					Content: g.buf.String(),
				})
				for _, v := range decl.Variants {
					g.buf.Reset()
					g.indent = 0
					g.emitPackageAndImports(prog.Package, prog.Imports)
					g.emitSealedVariant(decl.Name, v)
					files = append(files, OutputFile{
						Name:    v.Name + ".java",
						Content: g.buf.String(),
					})
				}
			} else {
				g.buf.Reset()
				g.indent = 0
				g.emitPackageAndImports(prog.Package, prog.Imports)
				g.emitClassDeclTopLevel(decl)
				files = append(files, OutputFile{
					Name:    decl.Name + ".java",
					Content: g.buf.String(),
				})
			}
		case *parser.InterfaceDecl:
			g.buf.Reset()
			g.indent = 0
			g.emitPackageAndImports(prog.Package, prog.Imports)
			g.emitInterfaceDecl(decl)
			files = append(files, OutputFile{
				Name:    decl.Name + ".java",
				Content: g.buf.String(),
			})
		case *parser.FnDecl:
			fnDecls = append(fnDecls, decl)
		case *parser.ConstDecl:
			constDecls = append(constDecls, decl)
		}
	}

	// Main class file: top-level functions + script statements
	if len(fnDecls) > 0 || len(constDecls) > 0 || len(prog.Stmts) > 0 {
		g.buf.Reset()
		g.indent = 0
		g.className = className
		g.emitPackageAndImports(prog.Package, prog.Imports)

		g.writeln("public class %s {", className)
		g.indent++

		for _, d := range constDecls {
			g.emitDecl(d)
			g.writeln("")
		}

		for _, d := range fnDecls {
			g.emitDecl(d)
			g.writeln("")
		}

		// Only generate script-mode main if no explicit fn main() exists
		hasExplicitMain := false
		for _, d := range fnDecls {
			if fn, ok := d.(*parser.FnDecl); ok && fn.Name == "main" {
				hasExplicitMain = true
				break
			}
		}
		if len(prog.Stmts) > 0 && !hasExplicitMain {
			g.writeln("public static void main(String[] args) throws Exception {")
			g.indent++
			for _, s := range prog.Stmts {
				g.emitStmt(s)
			}
			g.indent--
			g.writeln("}")
		}

		g.emitTupleRecords()

		g.indent--
		g.writeln("}")

		files = append(files, OutputFile{
			Name:    className + ".java",
			Content: g.buf.String(),
		})
	}

	return files
}

// emitSealedInterface emits a sealed interface + variant records.
func (g *Generator) emitSealedInterface(cls *parser.ClassDecl) {
	var permits []string
	for _, v := range cls.Variants {
		permits = append(permits, v.Name)
	}
	g.writeln("public sealed interface %s permits %s {}", cls.Name, strings.Join(permits, ", "))
}

func (g *Generator) emitSealedVariant(parent string, d *parser.DataClassDecl) {
	var fields []string
	for _, f := range d.Params {
		typeName := "Object"
		if f.Type != nil {
			typeName = g.formatType(f.Type)
		}
		fields = append(fields, typeName+" "+f.Name)
	}
	g.writeln("public record %s(%s) implements %s {}", d.Name, strings.Join(fields, ", "), parent)
}

// emitClassDeclTopLevel emits a class as a top-level public class (not static inner).
func (g *Generator) emitClassDeclTopLevel(cls *parser.ClassDecl) {
	g.emitClassBody(cls, "public class")
}

// emitClassBody is the shared implementation for top-level and inner class emission.
func (g *Generator) emitClassBody(cls *parser.ClassDecl, classPrefix string) {
	for _, a := range cls.Annotations {
		g.writeln("@%s", g.formatAnnotation(a))
	}

	ext := g.buildInheritanceClause(cls.Parents)

	typeParams := ""
	if len(cls.TypeParams) > 0 {
		typeParams = "<" + strings.Join(cls.TypeParams, ", ") + ">"
	}

	// Abstract modifier
	abstractMod := ""
	if cls.IsAbstract {
		abstractMod = "abstract "
	}

	g.writeln("%s%s %s%s%s {", abstractMod, classPrefix, cls.Name, typeParams, ext)
	g.indent++

	g.currentClassParents = cls.Parents

	// Fields
	g.pendingAccessors = nil
	for _, f := range cls.Fields {
		g.emitFieldDecl(f)
	}
	if len(cls.Fields) > 0 {
		g.writeln("")
	}

	// Constructors (support overloading)
	if len(cls.Ctors) > 0 {
		for _, ctor := range cls.Ctors {
			g.emitCtor(cls.Name, ctor, cls.Parents)
		}
	} else if cls.Ctor != nil {
		g.emitCtor(cls.Name, cls.Ctor, cls.Parents)
	}

	g.emitAccessors()

	// Methods
	for _, m := range cls.Methods {
		if m.IsAbstract {
			// Abstract method — signature only
			ret := "void"
			if m.ReturnType != nil {
				ret = g.formatType(m.ReturnType)
			}
			g.writeln("public abstract %s %s(%s) throws Exception;", ret, m.Name, g.formatParams(m.Params))
		} else {
			g.emitMethodDecl(m)
		}
		g.writeln("")
	}

	g.indent--
	g.writeln("}")
}

// --- Imports -----------------------------------------------------------------

// Standard imports always included — these are used by generated code.
var autoImports = []string{
	"java.util.*",
	"java.util.stream.*",
}

func (g *Generator) emitPackageAndImports(pkg *parser.PackageDecl, imports []*parser.ImportDecl) {
	if pkg != nil {
		g.writeln("package %s;", pkg.Path)
		g.writeln("")
	}
	for _, imp := range autoImports {
		g.writeln("import %s;", imp)
	}
	for _, imp := range imports {
		g.writeln("import %s;", imp.Path)
	}
	g.writeln("")
}

// emitImports is a convenience for codegen that doesn't have a package.
func (g *Generator) emitImports(imports []*parser.ImportDecl) {
	g.emitPackageAndImports(nil, imports)
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

	// fn main() → public static void main(String[] args) throws Exception
	if fn.Name == "main" {
		params := "String[] args"
		if len(fn.Params) > 0 {
			params = g.formatParams(fn.Params)
			// Track args param if it's an array
			for _, p := range fn.Params {
				if _, ok := p.Type.(*parser.ArrayType); ok {
					g.arrayVars[p.Name] = true
				}
			}
		}
		g.writeln("public static void main(%s) throws Exception {", params)
		g.indent++
		g.emitBlock(fn.Body)
		g.indent--
		g.writeln("}")
		return
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

	g.writeln("%sstatic %s%s %s(%s) throws Exception {", vis, typeParams, ret, fn.Name, params)
	g.indent++
	g.emitBlock(fn.Body)
	g.indent--
	g.writeln("}")

	// Generate overloads for default parameters
	g.emitDefaultOverloads(fn.Name, ret, fn.Params, false)
}

// --- Classes -----------------------------------------------------------------

func (g *Generator) emitClassDecl(cls *parser.ClassDecl) {
	g.emitClassBody(cls, "public static class")
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
	g.writeln("public %s(%s) throws Exception {", className, params)
	g.indent++
	if len(ctor.SuperArgs) > 0 {
		args := g.formatExprList(ctor.SuperArgs)
		g.writeln("super(%s);", args)
	}
	g.emitBlock(ctor.Body)
	g.indent--
	g.writeln("}")
	g.writeln("")

	// Generate overloads for default parameters
	g.emitDefaultOverloads(className, "", ctor.Params, true)
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

	// Determine if this method actually can throw.
	canThrow := methodCanThrow(m.Body)

	// Check if this method overrides a parent method that doesn't throw.
	// If so, wrap body in try/catch. Otherwise, declare throws Exception.
	needsWrap := false
	if canThrow {
		for _, parent := range g.currentClassParents {
			// Check Zinc interfaces — they all declare throws Exception, so no wrap needed
			if g.interfaces[parent] {
				continue
			}
			// Check Zinc parent classes first — we know their method signatures
			if methods, ok := g.zincMethods[parent]; ok {
				if parentCanThrow, found := methods[m.Name]; found && !parentCanThrow {
					needsWrap = true
					break
				}
				continue
			}
			// Fall through to javap for Java library parents
			javaClass := parent
			if mapped, ok := typechecker.ZincToJavaClass(parent); ok {
				javaClass = mapped
			}
			if found, throws := typechecker.MethodThrows(javaClass, m.Name); found && !throws {
				needsWrap = true
				break
			}
		}
		// All classes implicitly extend java.lang.Object
		if !needsWrap {
			if found, throws := typechecker.MethodThrows("java.lang.Object", m.Name); found && !throws {
				needsWrap = true
			}
		}
	}

	if needsWrap {
		// Override of non-throwing parent — wrap body in try/catch
		g.writeln("%s %s%s %s(%s) {", vis, static, ret, m.Name, params)
		g.indent++
		g.writeln("try {")
		g.indent++
		g.emitBlock(m.Body)
		g.indent--
		g.writeln("} catch (Exception e) { throw new RuntimeException(e); }")
		g.indent--
		g.writeln("}")
	} else if canThrow {
		// Method can throw — declare it
		g.writeln("%s %s%s %s(%s) throws Exception {", vis, static, ret, m.Name, params)
		g.indent++
		g.emitBlock(m.Body)
		g.indent--
		g.writeln("}")
	} else {
		// Method cannot throw — no throws declaration
		g.writeln("%s %s%s %s(%s) {", vis, static, ret, m.Name, params)
		g.indent++
		g.emitBlock(m.Body)
		g.indent--
		g.writeln("}")
	}

	// Generate overloads for default parameters
	g.emitMethodDefaultOverloads(vis, static, ret, m.Name, m.Params)
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

	g.currentClassParents = d.Parents
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
		g.writeln("%s %s(%s) throws Exception;", ret, m.Name, params)
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
		g.emitReturnStmt(stmt)
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
		g.emitExprStmt(stmt)
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
	case *parser.WithStmt:
		g.emitWithStmt(stmt)
	case *parser.ParallelForStmt:
		g.emitParallelForStmt(stmt)
	case *parser.GoStmt:
		// go { body } → Thread.startVirtualThread
		g.writeln("Thread.startVirtualThread(() -> {")
		g.indent++
		g.emitBlock(stmt.Body)
		g.indent--
		g.writeln("});")
	case *parser.ConcurrentStmt:
		g.emitConcurrentStmt(stmt)
	case *parser.TimeoutStmt:
		g.emitTimeoutStmt(stmt)
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

	// Handle `or` error handler: var x = call() or default / or { block } / or match { }
	if v.OrHandler != nil && v.Value != nil {
		// Java var requires initializer — use resolved type if available, else Object
		declType := keyword
		if declType == "var" || declType == "final var" {
			if v.ResolvedType != "" {
				if mapped, ok := zincToJavaType[v.ResolvedType]; ok {
					declType = mapped
				} else {
					declType = v.ResolvedType
				}
			} else {
				declType = "Object"
			}
		}
		g.writeln("%s %s;", declType, v.Name)
		g.emitOrHandler(g.formatExpr(v.Value), v.Name, v.OrHandler)
		return
	}

	if v.Value != nil {
		// Context-dependent array literal: int[] x = [1, 2, 3] → new int[] {1, 2, 3}
		if arrType, ok := v.Type.(*parser.ArrayType); ok {
			g.arrayVars[v.Name] = true
			if listLit, ok := v.Value.(*parser.ListLit); ok {
				elemType := g.formatType(arrType.ElementType)
				if len(listLit.Elements) == 0 {
					g.writeln("%s %s = new %s[0];", keyword, v.Name, elemType)
				} else {
					elems := g.formatExprList(listLit.Elements)
					g.writeln("%s %s = new %s[] {%s};", keyword, v.Name, elemType, elems)
				}
				return
			}
		}
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
		// Range expression: for i in 1..10 → for (int i = 1; i < 10; i++)
		if rangeExpr, ok := f.Range.(*parser.RangeExpr); ok {
			start := g.formatExpr(rangeExpr.Start)
			end := g.formatExpr(rangeExpr.End)
			op := "<"
			if rangeExpr.Inclusive {
				op = "<="
			}
			g.writeln("for (int %s = %s; %s %s %s; %s++) {", f.Item, start, f.Item, op, end, f.Item)
		} else if f.IndexVar != "" {
			// for (key, value) in map → iterate entrySet with destructuring
			g.writeln("for (var _entry : %s.entrySet()) {", g.formatExpr(f.Range))
			g.indent++
			g.writeln("var %s = _entry.getKey();", f.IndexVar)
			g.writeln("var %s = _entry.getValue();", f.Item)
			g.emitBlock(f.Body)
			g.indent--
			g.writeln("}")
			return
		} else {
			g.writeln("for (var %s : %s) {", f.Item, g.formatExpr(f.Range))
		}
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
			g.writeln("case %s -> {", g.formatMatchPattern(c.Pattern))
		}
		g.indent++
		g.emitBlock(c.Body)
		g.indent--
		g.writeln("}")
	}
	g.indent--
	g.writeln("}")
}

// formatMatchPattern formats a case pattern for Java switch pattern matching.
// Zinc: case Single(f) → Java: case Single(var f)  (record destructuring)
// Zinc: case Drop()     → Java: case Drop _         (empty record match)
// Zinc: case "literal"  → Java: case "literal"      (constant)
// Zinc: case ident       → Java: case ident          (enum/constant)
func (g *Generator) formatMatchPattern(expr parser.Expr) string {
	if call, ok := expr.(*parser.CallExpr); ok {
		if ident, ok := call.Callee.(*parser.Ident); ok {
			// Record pattern: Type(bindings...)
			if len(call.Args) == 0 {
				// Empty record: case Drop() → case Drop _
				return ident.Name + " _"
			}
			// Destructuring: case Single(f) → case Single(var f)
			var bindings []string
			for _, arg := range call.Args {
				if argIdent, ok := arg.(*parser.Ident); ok {
					bindings = append(bindings, "var "+argIdent.Name)
				} else {
					bindings = append(bindings, g.formatExpr(arg))
				}
			}
			return fmt.Sprintf("%s(%s)", ident.Name, strings.Join(bindings, ", "))
		}
	}
	return g.formatExpr(expr)
}

// emitReturnStmt handles return statements.
// return Error(...) → throw new RuntimeException(...) or throw new CustomType(...)
// return expr → return expr;
func (g *Generator) emitReturnStmt(r *parser.ReturnStmt) {
	if r.Value == nil {
		g.writeln("return;")
		return
	}

	// Detect return Error(...) → throw
	if call, ok := r.Value.(*parser.CallExpr); ok {
		if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "Error" {
			if len(call.Args) == 1 {
				arg := call.Args[0]
				// return Error(CustomType(...)) → throw new CustomType(...)
				if innerCall, ok := arg.(*parser.CallExpr); ok {
					if innerIdent, ok := innerCall.Callee.(*parser.Ident); ok {
						// return Error(NotFound("msg")) → throw new NotFound("msg")
						g.writeln("throw new %s(%s);", innerIdent.Name, g.formatExprList(innerCall.Args))
						return
					}
				}
				// return Error(err) where err is already an exception → wrap and re-throw
				if ident, ok := arg.(*parser.Ident); ok && ident.Name == "err" {
					g.writeln("throw new RuntimeException(%s);", g.currentErrVar())
					return
				}
				// return Error("message") → throw new RuntimeException("message")
				g.writeln("throw new RuntimeException(%s);", g.formatExpr(arg))
				return
			}
		}
	}

	g.writeln("return %s;", g.formatExpr(r.Value))
}

// emitExprStmt handles expression statements with optional or handler.
func (g *Generator) emitExprStmt(es *parser.ExprStmt) {
	if es.OrHandler == nil {
		g.writeln("%s;", g.formatExpr(es.Expr))
		return
	}
	g.emitOrHandler(g.formatExpr(es.Expr), "", es.OrHandler)
}

// pushErrVar generates a unique catch variable name and pushes it on the stack.
func (g *Generator) pushErrVar() string {
	name := "err"
	if len(g.errVarStack) > 0 {
		name = fmt.Sprintf("_err%d", len(g.errVarStack)+1)
	}
	g.errVarStack = append(g.errVarStack, name)
	return name
}

// popErrVar removes the most recent catch variable from the stack.
func (g *Generator) popErrVar() {
	if len(g.errVarStack) > 0 {
		g.errVarStack = g.errVarStack[:len(g.errVarStack)-1]
	}
}

// currentErrVar returns the current catch variable name (top of stack).
func (g *Generator) currentErrVar() string {
	if len(g.errVarStack) > 0 {
		return g.errVarStack[len(g.errVarStack)-1]
	}
	return "err"
}

// emitOrHandler generates try/catch for or handlers (used by VarStmt and ExprStmt).
func (g *Generator) emitOrHandler(callExpr string, assignTarget string, handler *parser.OrHandler) {
	// or match err { case Type -> ... }
	if handler.MatchCases != nil {
		if assignTarget != "" {
			g.writeln("try { %s = %s; }", assignTarget, callExpr)
		} else {
			g.writeln("try { %s; }", callExpr)
		}
		for _, mc := range handler.MatchCases {
			catchType := "Exception"
			if mc.Type != "" {
				catchType = mc.Type
			}
			varName := handler.MatchVar
			if varName == "" {
				varName = "err"
			}
			g.writeln("catch (%s %s) {", catchType, varName)
			g.indent++
			if assignTarget != "" && len(mc.Body.Stmts) == 1 {
				if es, ok := mc.Body.Stmts[0].(*parser.ExprStmt); ok {
					g.writeln("%s = %s;", assignTarget, g.formatExpr(es.Expr))
				} else {
					g.emitBlock(mc.Body)
				}
			} else {
				g.emitBlock(mc.Body)
			}
			g.indent--
			g.writeln("}")
		}
		return
	}

	// or { block } or or default
	errVar := g.pushErrVar()
	if assignTarget != "" {
		g.writeln("try { %s = %s; } catch (Exception %s) {", assignTarget, callExpr, errVar)
	} else {
		g.writeln("try { %s; } catch (Exception %s) {", callExpr, errVar)
	}
	g.indent++
	if handler.Body != nil && len(handler.Body.Stmts) == 1 {
		if es, ok := handler.Body.Stmts[0].(*parser.ExprStmt); ok {
			if assignTarget != "" {
				g.writeln("%s = %s;", assignTarget, g.formatExpr(es.Expr))
			} else {
				g.writeln("%s;", g.formatExpr(es.Expr))
			}
		} else {
			g.emitBlock(handler.Body)
		}
	} else if handler.Body != nil {
		g.emitBlock(handler.Body)
	}
	g.indent--
	g.writeln("}")
	g.popErrVar()
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

func (g *Generator) emitWithStmt(w *parser.WithStmt) {
	if len(w.Resources) == 1 && w.Resources[0].Name == "_lock" {
		// lock mu { body } → mu.lock(); try { body } finally { mu.unlock(); }
		lockExpr := g.formatExpr(w.Resources[0].Value)
		g.writeln("%s.lock();", lockExpr)
		g.writeln("try {")
		g.indent++
		g.emitBlock(w.Body)
		g.indent--
		g.writeln("} finally {")
		g.indent++
		g.writeln("%s.unlock();", lockExpr)
		g.indent--
		g.writeln("}")
		return
	}
	// General with → try-with-resources
	var resources []string
	for _, r := range w.Resources {
		resources = append(resources, fmt.Sprintf("var %s = %s", r.Name, g.formatExpr(r.Value)))
	}
	g.writeln("try (%s) {", strings.Join(resources, "; "))
	g.indent++
	g.emitBlock(w.Body)
	g.indent--
	g.writeln("}")
}

func (g *Generator) emitParallelForStmt(p *parser.ParallelForStmt) {
	if p.Max > 0 {
		// Bounded concurrency: use Semaphore to limit concurrent tasks
		g.writeln("var _semaphore = new java.util.concurrent.Semaphore(%d);", p.Max)
	}
	g.writeln("try (var _scope = java.util.concurrent.StructuredTaskScope.open(java.util.concurrent.StructuredTaskScope.Joiner.awaitAllSuccessfulOrThrow())) {")
	g.indent++
	g.writeln("for (var %s : %s) {", p.Item, g.formatExpr(p.Range))
	g.indent++
	if p.Max > 0 {
		g.writeln("_semaphore.acquire();")
	}
	g.writeln("_scope.fork(() -> {")
	g.indent++
	if p.Max > 0 {
		g.writeln("try {")
		g.indent++
	}
	g.emitBlock(p.Body)
	g.writeln("return null;")
	if p.Max > 0 {
		g.indent--
		g.writeln("} finally { _semaphore.release(); }")
	}
	g.indent--
	g.writeln("});")
	g.indent--
	g.writeln("}")
	g.writeln("_scope.join();")
	g.indent--
	if p.OrHandler != nil && p.OrHandler.Body != nil {
		g.writeln("} catch (Exception err) {")
		g.indent++
		g.emitBlock(p.OrHandler.Body)
		g.indent--
		g.writeln("}")
	} else {
		g.writeln("}")
	}
}

func (g *Generator) emitConcurrentStmt(c *parser.ConcurrentStmt) {
	scopeOpen := "java.util.concurrent.StructuredTaskScope.open(java.util.concurrent.StructuredTaskScope.Joiner.awaitAllSuccessfulOrThrow())"
	if c.FirstOnly {
		scopeOpen = "java.util.concurrent.StructuredTaskScope.open(java.util.concurrent.StructuredTaskScope.Joiner.anySuccessfulResultOrThrow())"
	}

	if len(c.Names) > 0 {
		for _, name := range c.Names {
			g.writeln("Object %s;", name)
		}
		g.writeln("try (var _scope = %s) {", scopeOpen)
		g.indent++
		for i, task := range c.Tasks {
			g.writeln("var _task%d = _scope.fork(() -> %s);", i, g.formatExpr(task))
		}
		g.writeln("_scope.join();")
		if c.FirstOnly {
			g.writeln("var _result = _scope.result();")
			if len(c.Names) > 0 {
				g.writeln("%s = _result;", c.Names[0])
			}
		} else {
			for i, name := range c.Names {
				if i < len(c.Tasks) {
					g.writeln("%s = _task%d.get();", name, i)
				}
			}
		}
		g.indent--
	} else {
		g.writeln("try (var _scope = %s) {", scopeOpen)
		g.indent++
		for _, task := range c.Tasks {
			g.writeln("_scope.fork(() -> { %s; return null; });", g.formatExpr(task))
		}
		g.writeln("_scope.join();")
		g.indent--
	}
	if c.OrHandler != nil && c.OrHandler.Body != nil {
		g.writeln("} catch (Exception err) {")
		g.indent++
		g.emitBlock(c.OrHandler.Body)
		g.indent--
		g.writeln("}")
	} else {
		g.writeln("}")
	}
}

func (g *Generator) emitTimeoutStmt(t *parser.TimeoutStmt) {
	// timeout(dur) { body } or { fallback }
	// → try (var _scope = StructuredTaskScope.open()) {
	//       var _task = _scope.fork(() -> { body });
	//       _scope.joinUntil(Instant.now().plus(dur));
	//       result = _task.get();
	//   } catch (TimeoutException e) { fallback }
	g.writeln("try (var _scope = java.util.concurrent.StructuredTaskScope.open()) {")
	g.indent++
	g.writeln("_scope.fork(() -> {")
	g.indent++
	g.emitBlock(t.Body)
	g.writeln("return null;")
	g.indent--
	g.writeln("});")
	g.writeln("_scope.joinUntil(java.time.Instant.now().plus(%s));", g.formatExpr(t.Duration))
	g.indent--
	if t.OrHandler != nil && t.OrHandler.Body != nil {
		g.writeln("} catch (java.util.concurrent.TimeoutException err) {")
		g.indent++
		g.emitBlock(t.OrHandler.Body)
		g.indent--
		g.writeln("}")
	} else {
		g.writeln("}")
	}
}

func (g *Generator) emitTupleVarStmt(t *parser.TupleVarStmt) {
	g.writeln("var _tuple = %s;", g.formatExpr(t.Value))
	for i, name := range t.Names {
		g.writeln("var %s = _tuple._%d();", name, i)
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
		// Map 'err' to the current catch variable name (for nested or-blocks)
		if expr.Name == "err" && len(g.errVarStack) > 0 {
			return g.currentErrVar()
		}
		return expr.Name
	case *parser.IntLit:
		return expr.Value
	case *parser.FloatLit:
		return expr.Value
	case *parser.StringLit:
		// Multi-line strings → Java text blocks with auto-indent stripping
		if strings.Contains(expr.Value, "\n") {
			return fmt.Sprintf("\"\"\"\n%s\"\"\"", stripCommonIndent(expr.Value))
		}
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
	case *parser.MatchExpr:
		return g.formatMatchExpr(expr)
	case *parser.BinaryExpr:
		return g.formatBinaryExpr(expr)
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", expr.Op, g.formatExpr(expr.Operand))
	case *parser.CallExpr:
		return g.formatCallExpr(expr)
	case *parser.SelectorExpr:
		return fmt.Sprintf("%s.%s", g.formatExpr(expr.Object), expr.Field)
	case *parser.IndexExpr:
		// Arrays use [] access, Lists use .get()
		if ident, ok := expr.Object.(*parser.Ident); ok && g.arrayVars[ident.Name] {
			return fmt.Sprintf("%s[%s]", g.formatExpr(expr.Object), g.formatExpr(expr.Index))
		}
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
	case *parser.TupleLit:
		n := len(expr.Elements)
		g.tupleTypes[n] = true
		return fmt.Sprintf("new Tuple%d(%s)", n, g.formatExprList(expr.Elements))
	case *parser.SpawnExpr:
		// Capture body by emitting into main buf, then extracting
		startLen := g.buf.Len()
		savedIndent := g.indent
		g.indent = 0
		if expr.Body != nil {
			for _, s := range expr.Body.Stmts {
				g.emitStmt(s)
			}
		}
		body := g.buf.String()[startLen:]
		prefix := g.buf.String()[:startLen]
		g.buf.Reset()
		g.buf.WriteString(prefix)
		g.indent = savedIndent
		trimmed := strings.TrimSpace(body)
		// Wrap body in try-catch to handle checked exceptions from Runnable
		wrappedBody := fmt.Sprintf("try { %s } catch (Exception _ex) { if (_ex instanceof RuntimeException) throw (RuntimeException) _ex; throw new RuntimeException(_ex); }", trimmed)
		// Default handler: wrap in Error (RuntimeException) — always present
		defaultHandler := "throw new RuntimeException(err);"
		if expr.OrHandler != nil && expr.OrHandler.Body != nil {
			// Capture or-handler body
			startLen2 := g.buf.Len()
			g.indent = 0
			for _, s := range expr.OrHandler.Body.Stmts {
				g.emitStmt(s)
			}
			handlerBody := g.buf.String()[startLen2:]
			prefix2 := g.buf.String()[:startLen2]
			g.buf.Reset()
			g.buf.WriteString(prefix2)
			g.indent = savedIndent
			defaultHandler = strings.TrimSpace(handlerBody)
		}
		// spawn always returns Thread, always has UncaughtExceptionHandler
		return fmt.Sprintf("Thread.ofVirtual().uncaughtExceptionHandler((_t, err) -> { %s }).start(() -> { %s })", defaultHandler, wrappedBody)
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
	case "and", "&&":
		return fmt.Sprintf("%s && %s", left, right)
	case "or", "||":
		return fmt.Sprintf("%s || %s", left, right)
	case "not":
		return fmt.Sprintf("!%s", right)
	case "**":
		if b.ResolvedType == "int" || b.ResolvedType == "long" {
			return fmt.Sprintf("(long)Math.pow(%s, %s)", left, right)
		}
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

// streamMethods are collection methods that need .stream() wrapping.
var streamIntermediateOps = map[string]bool{
	"filter": true, "map": true, "flatMap": true, "sortBy": true,
	"distinct": true, "limit": true, "skip": true,
}

var streamTerminalOps = map[string]bool{
	"findFirst": true, "anyMatch": true, "allMatch": true, "noneMatch": true,
	"count": true, "sum": true, "min": true, "max": true,
	"reduce": true, "forEach": true, "toList": true, "toSet": true,
	"toMap": true, "groupBy": true,
}

// collectStreamChain walks a CallExpr chain and collects stream method calls.
// Returns (root object, list of method+args pairs, true) if it's a stream chain.
type streamCall struct {
	method string
	args   []parser.Expr
}

func (g *Generator) collectStreamChain(c *parser.CallExpr) (parser.Expr, []streamCall, bool) {
	var chain []streamCall

	current := c
	for {
		sel, ok := current.Callee.(*parser.SelectorExpr)
		if !ok {
			break
		}
		method := sel.Field
		if !streamIntermediateOps[method] && !streamTerminalOps[method] {
			break
		}
		chain = append([]streamCall{{method: method, args: current.Args}}, chain...)

		// Check if the object is another call in the chain
		if nextCall, ok := sel.Object.(*parser.CallExpr); ok {
			current = nextCall
		} else {
			// Root object found
			return sel.Object, chain, len(chain) > 0
		}
	}

	return nil, nil, false
}

func (g *Generator) formatStreamChain(root parser.Expr, chain []streamCall) string {
	var sb strings.Builder
	sb.WriteString(g.formatExpr(root))
	sb.WriteString(".stream()")

	for _, sc := range chain {
		arg := g.formatStreamArg(sc.args)
		switch sc.method {
		case "filter":
			sb.WriteString(fmt.Sprintf(".filter(%s)", arg))
		case "map":
			sb.WriteString(fmt.Sprintf(".map(%s)", arg))
		case "flatMap":
			sb.WriteString(fmt.Sprintf(".flatMap(%s)", arg))
		case "sortBy":
			sb.WriteString(fmt.Sprintf(".sorted(java.util.Comparator.comparing(%s))", arg))
		case "distinct":
			sb.WriteString(".distinct()")
		case "limit":
			sb.WriteString(fmt.Sprintf(".limit(%s)", arg))
		case "skip":
			sb.WriteString(fmt.Sprintf(".skip(%s)", arg))
		case "findFirst":
			if arg != "" {
				sb.WriteString(fmt.Sprintf(".filter(%s)", arg))
			}
			sb.WriteString(".findFirst().orElse(null)")
			return sb.String()
		case "anyMatch":
			sb.WriteString(fmt.Sprintf(".anyMatch(%s)", arg))
			return sb.String()
		case "allMatch":
			sb.WriteString(fmt.Sprintf(".allMatch(%s)", arg))
			return sb.String()
		case "noneMatch":
			sb.WriteString(fmt.Sprintf(".noneMatch(%s)", arg))
			return sb.String()
		case "count":
			sb.WriteString(".count()")
			return sb.String()
		case "sum":
			sb.WriteString(".mapToInt(Integer::intValue).sum()")
			return sb.String()
		case "min":
			sb.WriteString(".min(java.util.Comparator.naturalOrder()).orElse(null)")
			return sb.String()
		case "max":
			sb.WriteString(".max(java.util.Comparator.naturalOrder()).orElse(null)")
			return sb.String()
		case "reduce":
			sb.WriteString(fmt.Sprintf(".reduce(%s)", arg))
			return sb.String()
		case "forEach":
			sb.WriteString(fmt.Sprintf(".forEach(%s)", arg))
			return sb.String()
		case "toList":
			sb.WriteString(".toList()")
			return sb.String()
		case "toSet":
			sb.WriteString(".collect(java.util.stream.Collectors.toSet())")
			return sb.String()
		case "groupBy":
			sb.WriteString(fmt.Sprintf(".collect(java.util.stream.Collectors.groupingBy(%s))", arg))
			return sb.String()
		case "toMap":
			sb.WriteString(fmt.Sprintf(".collect(java.util.stream.Collectors.toMap(%s))", arg))
			return sb.String()
		}
	}

	// If chain ends with intermediate ops, add .toList() as default terminal
	sb.WriteString(".toList()")
	return sb.String()
}

func (g *Generator) formatStreamArg(args []parser.Expr) string {
	if len(args) == 0 {
		return ""
	}
	var parts []string
	for _, arg := range args {
		if containsIt(arg) {
			parts = append(parts, "_it -> "+g.formatExprIt(arg))
		} else {
			parts = append(parts, g.formatExpr(arg))
		}
	}
	return strings.Join(parts, ", ")
}

// stringMethodAliases maps Zinc convenience names to Java String methods.
var stringMethodAliases = map[string]string{
	"upper":      "toUpperCase",
	"lower":      "toLowerCase",
	"trim":       "strip",
	"trimStart":  "stripLeading",
	"trimEnd":    "stripTrailing",
	"startsWith": "startsWith",
	"endsWith":   "endsWith",
	"contains":   "contains",
	"replace":    "replace",
	"repeat":     "repeat",
	"isEmpty":    "isEmpty",
	"chars":      "toCharArray",
	"split":      "split",
	"substring":  "substring",
	"charAt":     "charAt",
	"indexOf":    "indexOf",
}

func (g *Generator) formatCallExpr(c *parser.CallExpr) string {
	// Check for stream chain: items.filter(x).map(y).sum()
	if root, chain, ok := g.collectStreamChain(c); ok {
		return g.formatStreamChain(root, chain)
	}

	// String method aliases: obj.upper() → obj.toUpperCase()
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if javaMethod, ok := stringMethodAliases[sel.Field]; ok {
			obj := g.formatExpr(sel.Object)
			args := g.formatExprList(c.Args)
			return fmt.Sprintf("%s.%s(%s)", obj, javaMethod, args)
		}
	}

	callee := g.formatExpr(c.Callee)
	args := g.formatExprList(c.Args)
	// Add named args as positional (Java doesn't have named args)
	for _, na := range c.NamedArgs {
		if args != "" {
			args += ", "
		}
		args += g.formatExpr(na.Value)
	}

	// Wrap `it` references in lambda: items.filter(it > 0) → items.filter(_it -> _it > 0)
	var argStrs []string
	hasItRewrite := false
	for _, arg := range c.Args {
		if containsIt(arg) {
			hasItRewrite = true
			argStrs = append(argStrs, "_it -> "+g.formatExprIt(arg))
		} else {
			argStrs = append(argStrs, g.formatExpr(arg))
		}
	}
	for _, na := range c.NamedArgs {
		argStrs = append(argStrs, g.formatExpr(na.Value))
	}
	if hasItRewrite {
		return fmt.Sprintf("%s(%s)", callee, strings.Join(argStrs, ", "))
	}
	args = strings.Join(argStrs, ", ")

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

	// Constructor calls: explicit `new` keyword
	if c.IsNew {
		if mapped, ok := zincToJavaType[callee]; ok {
			callee = mapped
		}
		if len(c.TypeArgs) > 0 {
			var mappedArgs []string
			for _, ta := range c.TypeArgs {
				if mapped, ok := zincToJavaBoxed[ta]; ok {
					mappedArgs = append(mappedArgs, mapped)
				} else if mapped, ok := zincToJavaType[ta]; ok {
					mappedArgs = append(mappedArgs, mapped)
				} else {
					mappedArgs = append(mappedArgs, ta)
				}
			}
			return fmt.Sprintf("new %s<%s>(%s)", callee, strings.Join(mappedArgs, ", "), args)
		}
		return fmt.Sprintf("new %s(%s)", callee, args)
	}

	return fmt.Sprintf("%s(%s)", callee, args)
}

func isBuiltinFunc(name string) bool {
	switch name {
	case "System", "Math", "String", "Integer", "Double", "Boolean", "Objects",
		"Thread", "List", "Map", "Set", "Arrays", "Collections":
		return true
	}
	return false
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
			expr := g.formatExpr(part)
			// Wrap expressions that have lower precedence than + in parens
			// to avoid "str" + a == b being parsed as ("str" + a) == b
			if g.needsInterpParens(part) {
				expr = "(" + expr + ")"
			}
			parts = append(parts, expr)
		}
	}
	return strings.Join(parts, " + ")
}

// formatMatchExpr emits a Java switch expression: switch (subject) { case X -> val; ... }
func (g *Generator) formatMatchExpr(m *parser.MatchExpr) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("switch (%s) { ", g.formatExpr(m.Subject)))
	for _, c := range m.Cases {
		if c.Pattern == nil {
			sb.WriteString(fmt.Sprintf("default -> %s; ", g.formatExpr(c.Value)))
		} else {
			sb.WriteString(fmt.Sprintf("case %s -> %s; ", g.formatMatchPattern(c.Pattern), g.formatExpr(c.Value)))
		}
	}
	sb.WriteString("}")
	return sb.String()
}

// needsInterpParens returns true if an expression needs parentheses
// when used inside string concatenation (+ has higher precedence than
// comparison, equality, and logical operators in Java).
func (g *Generator) needsInterpParens(expr parser.Expr) bool {
	switch e := expr.(type) {
	case *parser.BinaryExpr:
		switch e.Op {
		case "==", "!=", "===", "!==", "<", ">", "<=", ">=",
			"&&", "||":
			return true
		}
	case *parser.IfExpr:
		return true
	}
	return false
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
	"Channel": "java.util.concurrent.ArrayBlockingQueue",
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
	case *parser.ArrayType:
		return g.formatType(typ.ElementType) + "[]"
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

// emitDefaultOverloads generates overloaded constructors or static functions
// for each suffix of default parameters.
// e.g., fn foo(int a, int b = 10, int c = 20) generates:
//   foo(int a, int b) { foo(a, b, 20); }
//   foo(int a) { foo(a, 10, 20); }
func (g *Generator) emitDefaultOverloads(name string, retType string, params []*parser.ParamDecl, isCtor bool) {
	// Find first param with default
	firstDefault := -1
	for i, p := range params {
		if p.Default != nil {
			firstDefault = i
			break
		}
	}
	if firstDefault < 0 {
		return
	}

	// Generate overloads: from (all params minus last default) down to (just required params)
	for cutoff := len(params) - 1; cutoff >= firstDefault; cutoff-- {
		required := params[:cutoff]
		paramStr := g.formatParams(required)

		// Build call args: required params by name + defaults for the rest
		var callArgs []string
		for _, p := range required {
			callArgs = append(callArgs, p.Name)
		}
		for _, p := range params[cutoff:] {
			callArgs = append(callArgs, g.formatExpr(p.Default))
		}
		argStr := strings.Join(callArgs, ", ")

		if isCtor {
			g.writeln("public %s(%s) throws Exception {", name, paramStr)
			g.indent++
			g.writeln("this(%s);", argStr)
			g.indent--
			g.writeln("}")
			g.writeln("")
		} else {
			if retType == "void" {
				g.writeln("static void %s(%s) throws Exception {", name, paramStr)
				g.indent++
				g.writeln("%s(%s);", name, argStr)
			} else {
				g.writeln("static %s %s(%s) throws Exception {", retType, name, paramStr)
				g.indent++
				g.writeln("return %s(%s);", name, argStr)
			}
			g.indent--
			g.writeln("}")
			g.writeln("")
		}
	}
}

// emitMethodDefaultOverloads generates overloaded methods for default params.
func (g *Generator) emitMethodDefaultOverloads(vis, static, retType, name string, params []*parser.ParamDecl) {
	firstDefault := -1
	for i, p := range params {
		if p.Default != nil {
			firstDefault = i
			break
		}
	}
	if firstDefault < 0 {
		return
	}

	for cutoff := len(params) - 1; cutoff >= firstDefault; cutoff-- {
		required := params[:cutoff]
		paramStr := g.formatParams(required)

		var callArgs []string
		for _, p := range required {
			callArgs = append(callArgs, p.Name)
		}
		for _, p := range params[cutoff:] {
			callArgs = append(callArgs, g.formatExpr(p.Default))
		}
		argStr := strings.Join(callArgs, ", ")

		if retType == "void" {
			g.writeln("%s %s%s %s(%s) {", vis, static, retType, name, paramStr)
			g.indent++
			g.writeln("%s(%s);", name, argStr)
		} else {
			g.writeln("%s %s%s %s(%s) {", vis, static, retType, name, paramStr)
			g.indent++
			g.writeln("return %s(%s);", name, argStr)
		}
		g.indent--
		g.writeln("}")
	}
}

func (g *Generator) formatParams(params []*parser.ParamDecl) string {
	var parts []string
	for _, p := range params {
		typeName := "Object"
		if p.Type != nil {
			typeName = g.formatType(p.Type)
			// Track array params for [] access generation
			if _, ok := p.Type.(*parser.ArrayType); ok {
				g.arrayVars[p.Name] = true
			}
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
			return fmt.Sprintf("%s %s = %s;", keyword, stmt.Name, g.formatExpr(stmt.Value))
		}
		return fmt.Sprintf("%s %s;", keyword, stmt.Name)
	case *parser.AssignStmt:
		return fmt.Sprintf("%s %s %s;", g.formatExpr(stmt.Target), stmt.Op, g.formatExpr(stmt.Value))
	case *parser.ExprStmt:
		if stmt.OrHandler != nil {
			// Inline try/catch for or-handlers in expression statements
			errVar := g.pushErrVar()
			result := fmt.Sprintf("try { %s; } catch (Exception %s) { ", g.formatExpr(stmt.Expr), errVar)
			if stmt.OrHandler.Body != nil {
				for _, s := range stmt.OrHandler.Body.Stmts {
					result += g.formatStmtInline(s) + " "
				}
			}
			result += "}"
			g.popErrVar()
			return result
		}
		return g.formatExpr(stmt.Expr) + ";"
	case *parser.ReturnStmt:
		if stmt.Value != nil {
			// Detect return Error(...) in inline context
			if call, ok := stmt.Value.(*parser.CallExpr); ok {
				if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "Error" && len(call.Args) == 1 {
					arg := call.Args[0]
					if innerCall, ok := arg.(*parser.CallExpr); ok {
						if innerIdent, ok := innerCall.Callee.(*parser.Ident); ok {
							return fmt.Sprintf("throw new %s(%s);", innerIdent.Name, g.formatExprList(innerCall.Args))
						}
					}
					if id, ok := arg.(*parser.Ident); ok && id.Name == "err" {
						return fmt.Sprintf("throw new RuntimeException(%s);", g.currentErrVar())
					}
					return fmt.Sprintf("throw new RuntimeException(%s);", g.formatExpr(arg))
				}
			}
			return "return " + g.formatExpr(stmt.Value) + ";"
		}
		return "return;"
	default:
		return "/* inline stmt */"
	}
}


// --- it keyword helpers ------------------------------------------------------

// containsIt checks if an expression tree contains Ident("it").
func containsIt(e parser.Expr) bool {
	switch expr := e.(type) {
	case *parser.Ident:
		return expr.Name == "it"
	case *parser.BinaryExpr:
		return containsIt(expr.Left) || containsIt(expr.Right)
	case *parser.UnaryExpr:
		return containsIt(expr.Operand)
	case *parser.SelectorExpr:
		return containsIt(expr.Object)
	case *parser.CallExpr:
		if containsIt(expr.Callee) {
			return true
		}
		for _, a := range expr.Args {
			if containsIt(a) {
				return true
			}
		}
		return false
	case *parser.IndexExpr:
		return containsIt(expr.Object) || containsIt(expr.Index)
	default:
		return false
	}
}

// formatExprIt formats an expression, replacing Ident("it") with "_it".
func (g *Generator) formatExprIt(e parser.Expr) string {
	switch expr := e.(type) {
	case *parser.Ident:
		if expr.Name == "it" {
			return "_it"
		}
		return expr.Name
	case *parser.BinaryExpr:
		left := g.formatExprIt(expr.Left)
		right := g.formatExprIt(expr.Right)
		// Reuse the same operator mapping
		switch expr.Op {
		case "and":
			return fmt.Sprintf("%s && %s", left, right)
		case "or":
			return fmt.Sprintf("%s || %s", left, right)
		case "**":
			if expr.ResolvedType == "int" || expr.ResolvedType == "long" {
				return fmt.Sprintf("(long)Math.pow(%s, %s)", left, right)
			}
			return fmt.Sprintf("Math.pow(%s, %s)", left, right)
		case "==":
			return fmt.Sprintf("java.util.Objects.equals(%s, %s)", left, right)
		case "!=":
			return fmt.Sprintf("!java.util.Objects.equals(%s, %s)", left, right)
		default:
			return fmt.Sprintf("%s %s %s", left, expr.Op, right)
		}
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", expr.Op, g.formatExprIt(expr.Operand))
	case *parser.SelectorExpr:
		return fmt.Sprintf("%s.%s", g.formatExprIt(expr.Object), expr.Field)
	case *parser.CallExpr:
		callee := g.formatExprIt(expr.Callee)
		var args []string
		for _, a := range expr.Args {
			args = append(args, g.formatExprIt(a))
		}
		return fmt.Sprintf("%s(%s)", callee, strings.Join(args, ", "))
	default:
		return g.formatExpr(e)
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

// stripCommonIndent removes the common leading whitespace from all lines.
func stripCommonIndent(s string) string {
	lines := strings.Split(s, "\n")
	// Find minimum indent (ignoring empty lines)
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if minIndent < 0 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent <= 0 {
		return s
	}
	var result []string
	for _, line := range lines {
		if len(line) >= minIndent {
			result = append(result, line[minIndent:])
		} else {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}
