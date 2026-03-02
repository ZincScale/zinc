package codegen

import (
	"fmt"
	"strings"
	"unicode"

	"growler/internal/parser"
)

// Generator converts a Growler AST to Go source code.
type Generator struct {
	buf            strings.Builder
	indent         int
	neededImports  map[string]bool
	classNames     map[string]bool // set of declared class names
	interfaceNames map[string]bool // set of declared interface names
	// canThrowFns: set of fn/method names that can throw (have ThrowStmt)
	canThrowFns map[string]bool
	// current receiver name for method emission
	receiver string
	// current function return type (for zero-value in throw)
	currentReturnType parser.TypeExpr
	// whether current function CanThrow (affects return stmt emission)
	currentCanThrow bool
	// map class name → CtorDecl for super-arg resolution
	classCtors map[string]*parser.CtorDecl
	// user-specified imports from import decls
	userImports []*parser.ImportDecl
	// enumNames: set of declared enum type names
	enumNames map[string]bool
	// packageName overrides the emitted package declaration.
	// Empty means auto-detect from PackageDecl or default to "main".
	packageName string
}

// New creates a Generator for single-file mode (package = auto-detected).
func New() *Generator {
	return &Generator{
		neededImports:  make(map[string]bool),
		classNames:     make(map[string]bool),
		interfaceNames: make(map[string]bool),
		canThrowFns:    make(map[string]bool),
		classCtors:     make(map[string]*parser.CtorDecl),
		enumNames:      make(map[string]bool),
	}
}

// NewWithRegistry creates a Generator pre-seeded with cross-file type
// information from a TypeRegistry. pkgName is the Go package name to emit;
// pass "" to auto-detect from the file's PackageDecl.
func NewWithRegistry(reg *TypeRegistry, pkgName string) *Generator {
	g := &Generator{
		neededImports:  make(map[string]bool),
		classNames:     make(map[string]bool),
		interfaceNames: make(map[string]bool),
		canThrowFns:    make(map[string]bool),
		classCtors:     make(map[string]*parser.CtorDecl),
		enumNames:      make(map[string]bool),
		packageName:    pkgName,
	}
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
	return g
}

// Generate converts the program AST to a Go source string.
func (g *Generator) Generate(prog *parser.Program) string {
	// First pass: collect names and mark canThrow
	g.firstPass(prog)

	// Collect user imports
	g.userImports = prog.Imports

	// Second pass: emit
	var body strings.Builder
	// swap g.buf with body temporarily
	savedBuf := g.buf
	g.buf = body

	for _, decl := range prog.Decls {
		g.emitTopLevel(decl)
		g.writeln("")
	}

	body = g.buf
	g.buf = savedBuf

	// Build final output
	var out strings.Builder

	// Determine package name: explicit > PackageDecl > "main"
	pkgName := g.packageName
	if pkgName == "" {
		if prog.Package != nil {
			pkgName = lastSegment(prog.Package.Path)
		} else {
			pkgName = "main"
		}
	}
	out.WriteString("package " + pkgName + "\n\n")

	// Imports
	imports := g.buildImports()
	if len(imports) > 0 {
		out.WriteString("import (\n")
		for _, imp := range imports {
			out.WriteString("\t" + imp + "\n")
		}
		out.WriteString(")\n\n")
	}

	out.WriteString(body.String())
	return out.String()
}

// --- First Pass --------------------------------------------------------------

func (g *Generator) firstPass(prog *parser.Program) {
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *parser.ClassDecl:
			g.classNames[d.Name] = true
			if d.Ctor != nil {
				g.classCtors[d.Name] = d.Ctor
			}
		case *parser.InterfaceDecl:
			g.interfaceNames[d.Name] = true
		case *parser.EnumDecl:
			g.enumNames[d.Name] = true
		}
	}

	// Mark canThrow
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *parser.FnDecl:
			if g.bodyCanThrow(d.Body) {
				d.CanThrow = true
				g.canThrowFns[d.Name] = true
			}
		case *parser.ClassDecl:
			for _, m := range d.Methods {
				if g.bodyCanThrow(m.Body) {
					m.CanThrow = true
					g.canThrowFns[d.Name+"."+m.Name] = true
				}
			}
		}
	}
}

func (g *Generator) bodyCanThrow(body *parser.BlockStmt) bool {
	if body == nil {
		return false
	}
	for _, s := range body.Stmts {
		if g.stmtCanThrow(s) {
			return true
		}
	}
	return false
}

func (g *Generator) stmtCanThrow(s parser.Stmt) bool {
	switch st := s.(type) {
	case *parser.ThrowStmt:
		return true
	case *parser.BlockStmt:
		return g.bodyCanThrow(st)
	case *parser.IfStmt:
		if g.bodyCanThrow(st.Then) {
			return true
		}
		if st.ElseStmt != nil {
			if b, ok := st.ElseStmt.(*parser.BlockStmt); ok {
				return g.bodyCanThrow(b)
			}
			if i, ok := st.ElseStmt.(*parser.IfStmt); ok {
				return g.stmtCanThrow(i)
			}
		}
	case *parser.ForStmt:
		return g.bodyCanThrow(st.Body)
	case *parser.WhileStmt:
		return g.bodyCanThrow(st.Body)
	case *parser.GoStmt:
		return g.bodyCanThrow(st.Body)
	}
	return false
}

// --- Import Management -------------------------------------------------------

func (g *Generator) buildImports() []string {
	var imports []string

	// User-specified imports first
	added := make(map[string]bool)
	for _, imp := range g.userImports {
		var s string
		if imp.Alias != "" {
			s = fmt.Sprintf(`%s "%s"`, imp.Alias, imp.Path)
		} else {
			s = fmt.Sprintf(`"%s"`, imp.Path)
		}
		imports = append(imports, s)
		// Track the package name to avoid auto-adding
		pkg := imp.Path
		if idx := strings.LastIndex(pkg, "/"); idx >= 0 {
			pkg = pkg[idx+1:]
		}
		added[pkg] = true
	}

	for pkg := range g.neededImports {
		if !added[pkg] {
			imports = append(imports, fmt.Sprintf(`"%s"`, pkg))
		}
	}
	return imports
}

// --- Output Helpers ----------------------------------------------------------

func (g *Generator) write(s string) {
	g.buf.WriteString(s)
}

func (g *Generator) writeln(s string) {
	if s == "" {
		g.buf.WriteString("\n")
		return
	}
	g.buf.WriteString(strings.Repeat("\t", g.indent))
	g.buf.WriteString(s)
	g.buf.WriteString("\n")
}

func (g *Generator) writeIndent() {
	g.buf.WriteString(strings.Repeat("\t", g.indent))
}

func (g *Generator) push() { g.indent++ }
func (g *Generator) pop()  { g.indent-- }

// --- Type Emission -----------------------------------------------------------

func (g *Generator) emitType(t parser.TypeExpr) string {
	if t == nil {
		return ""
	}
	switch typ := t.(type) {
	case *parser.SimpleType:
		return g.emitSimpleType(typ.Name)
	case *parser.GenericType:
		return g.emitGenericType(typ)
	case *parser.OptionalType:
		inner := g.emitType(typ.Inner)
		if inner == "" || inner == "interface{}" {
			return "interface{}"
		}
		// Already a pointer (class type) — don't double-pointer
		if strings.HasPrefix(inner, "*") {
			return inner
		}
		return "*" + inner
	}
	return "interface{}"
}

func (g *Generator) emitSimpleType(name string) string {
	switch name {
	case "Int":
		return "int"
	case "Float":
		return "float64"
	case "String":
		return "string"
	case "Bool":
		return "bool"
	case "Void":
		return ""
	case "Any":
		return "interface{}"
	}
	// Class types become pointer types
	if g.classNames[name] {
		return "*" + name
	}
	// Interface types remain value types
	return name
}

func (g *Generator) emitGenericType(t *parser.GenericType) string {
	switch t.Name {
	case "List":
		if len(t.TypeArgs) == 1 {
			return "[]" + g.emitType(t.TypeArgs[0])
		}
	case "Map":
		if len(t.TypeArgs) == 2 {
			return "map[" + g.emitType(t.TypeArgs[0]) + "]" + g.emitType(t.TypeArgs[1])
		}
	case "Chan":
		if len(t.TypeArgs) == 1 {
			return "chan " + g.emitType(t.TypeArgs[0])
		}
	}
	// Fallback
	args := make([]string, len(t.TypeArgs))
	for i, a := range t.TypeArgs {
		args[i] = g.emitType(a)
	}
	return t.Name + "[" + strings.Join(args, ", ") + "]"
}

// zeroValue returns the Go zero value for a type.
func (g *Generator) zeroValue(t parser.TypeExpr) string {
	if t == nil {
		return ""
	}
	switch typ := t.(type) {
	case *parser.SimpleType:
		switch typ.Name {
		case "Int":
			return "0"
		case "Float":
			return "0.0"
		case "String":
			return `""`
		case "Bool":
			return "false"
		}
		// class/interface/other → nil
		return "nil"
	case *parser.GenericType:
		return "nil"
	case *parser.OptionalType:
		return "nil"
	}
	return "nil"
}

// --- Top-Level Emission ------------------------------------------------------

func (g *Generator) emitTopLevel(decl parser.TopLevelDecl) {
	switch d := decl.(type) {
	case *parser.ClassDecl:
		g.emitClass(d)
	case *parser.InterfaceDecl:
		g.emitInterface(d)
	case *parser.FnDecl:
		g.emitFn(d)
	case *parser.EnumDecl:
		g.emitEnum(d)
	}
}

// --- Enum Emission -----------------------------------------------------------

func (g *Generator) emitEnum(e *parser.EnumDecl) {
	g.writeln(fmt.Sprintf("type %s int", e.Name))
	g.writeln("")
	g.writeln("const (")
	g.push()
	for i, v := range e.Variants {
		if i == 0 {
			g.writeln(fmt.Sprintf("%s %s = iota", v, e.Name))
		} else {
			g.writeln(v)
		}
	}
	g.pop()
	g.writeln(")")
}

// --- Interface Emission ------------------------------------------------------

func (g *Generator) emitInterface(iface *parser.InterfaceDecl) {
	g.writeln(fmt.Sprintf("type %s interface {", iface.Name))
	g.push()
	for _, m := range iface.Methods {
		name := exportName(m.Name, m.IsPub)
		var params []string
		for _, p := range m.Params {
			params = append(params, p.Name+" "+g.emitType(p.Type))
		}
		ret := g.emitType(m.ReturnType)
		sig := name + "(" + strings.Join(params, ", ") + ")"
		if ret != "" {
			sig += " " + ret
		}
		g.writeln(sig)
	}
	g.pop()
	g.writeln("}")
}

// --- Class Emission ----------------------------------------------------------

func (g *Generator) emitClass(cls *parser.ClassDecl) {
	// Determine base class (first parent that is a class) and interfaces
	var baseClass string
	var ifaces []string
	for _, p := range cls.Parents {
		if g.classNames[p] {
			baseClass = p
		} else {
			ifaces = append(ifaces, p)
		}
	}

	// 1. Struct definition
	typeParamStr := ""
	if len(cls.TypeParams) > 0 {
		constraints := make([]string, len(cls.TypeParams))
		for i, tp := range cls.TypeParams {
			constraints[i] = tp + " any"
		}
		typeParamStr = "[" + strings.Join(constraints, ", ") + "]"
	}
	g.writeln(fmt.Sprintf("type %s%s struct {", cls.Name, typeParamStr))
	g.push()
	// Embed base class
	if baseClass != "" {
		g.writeln(baseClass)
	}
	// Own fields
	for _, f := range cls.Fields {
		g.writeln(fmt.Sprintf("%s %s", capitalize(f.Name), g.emitType(f.Type)))
	}
	g.pop()
	g.writeln("}")
	g.writeln("")

	// Verify interface implementation (compile-time check)
	for _, iface := range ifaces {
		g.writeln(fmt.Sprintf("var _ %s = (*%s)(nil)", iface, cls.Name))
	}
	if len(ifaces) > 0 {
		g.writeln("")
	}

	// 2. Constructor
	if cls.Ctor != nil {
		g.emitCtor(cls, baseClass)
	}

	// 3. Methods
	recv := strings.ToLower(cls.Name[:1])
	g.receiver = recv
	for _, m := range cls.Methods {
		g.emitMethod(cls.Name, recv, m)
	}
	g.receiver = ""
}

func (g *Generator) emitCtor(cls *parser.ClassDecl, baseClass string) {
	ctor := cls.Ctor
	name := "New" + cls.Name

	// Build params string
	var params []string
	for _, p := range ctor.Params {
		params = append(params, p.Name+" "+g.emitType(p.Type))
	}
	paramStr := strings.Join(params, ", ")

	g.writeln(fmt.Sprintf("func %s(%s) *%s {", name, paramStr, cls.Name))
	g.push()

	// Build struct literal
	g.writeIndent()
	g.write(fmt.Sprintf("obj := &%s{\n", cls.Name))

	g.push()
	// Base class init
	if baseClass != "" && g.classCtors[baseClass] != nil {
		parentCtor := g.classCtors[baseClass]
		var superArgStrs []string
		// Map super args by position to parent ctor params
		for i, arg := range ctor.SuperArgs {
			_ = i
			superArgStrs = append(superArgStrs, g.emitExpr(arg))
		}
		// Build parent struct literal
		var parentFields []string
		for i, pp := range parentCtor.Params {
			val := ""
			if i < len(superArgStrs) {
				val = superArgStrs[i]
			} else {
				val = g.zeroValue(pp.Type)
			}
			parentFields = append(parentFields, capitalize(pp.Name)+": "+val)
		}
		if len(parentFields) > 0 {
			g.writeln(fmt.Sprintf("%s: %s{%s},", baseClass, baseClass, strings.Join(parentFields, ", ")))
		} else {
			g.writeln(fmt.Sprintf("%s: %s{},", baseClass, baseClass))
		}
	} else if baseClass != "" {
		// super call with raw args
		var superArgStrs []string
		for _, arg := range ctor.SuperArgs {
			superArgStrs = append(superArgStrs, g.emitExpr(arg))
		}
		if len(superArgStrs) > 0 {
			g.writeln(fmt.Sprintf("%s: %s{%s},", baseClass, baseClass, strings.Join(superArgStrs, ", ")))
		} else {
			g.writeln(fmt.Sprintf("%s: %s{},", baseClass, baseClass))
		}
	}
	// Own fields with defaults
	for _, f := range cls.Fields {
		if f.Default != nil {
			g.writeln(fmt.Sprintf("%s: %s,", capitalize(f.Name), g.emitExpr(f.Default)))
		}
	}
	g.pop()
	g.writeIndent()
	g.write("}\n")

	// Body statements (super call already removed)
	recv := strings.ToLower(cls.Name[:1])
	savedRecv := g.receiver
	g.receiver = "obj"
	for _, s := range ctor.Body.Stmts {
		g.emitStmt(s)
	}
	g.receiver = savedRecv
	_ = recv

	g.writeln("return obj")
	g.pop()
	g.writeln("}")
	g.writeln("")
}

func (g *Generator) emitMethod(className, recv string, m *parser.MethodDecl) {
	name := exportName(m.Name, m.IsPub)

	var params []string
	for _, p := range m.Params {
		params = append(params, p.Name+" "+g.emitType(p.Type))
	}
	paramStr := strings.Join(params, ", ")

	retType := g.emitType(m.ReturnType)
	var retStr string
	if m.CanThrow {
		g.neededImports["fmt"] = true
		if retType != "" {
			retStr = " (" + retType + ", error)"
		} else {
			retStr = " error"
		}
	} else if retType != "" {
		retStr = " " + retType
	}

	if m.IsStatic {
		g.writeln(fmt.Sprintf("func %s_%s(%s)%s {", className, name, paramStr, retStr))
	} else {
		g.writeln(fmt.Sprintf("func (%s *%s) %s(%s)%s {", recv, className, name, paramStr, retStr))
	}
	g.push()
	savedRecv := g.receiver
	savedRT := g.currentReturnType
	savedCT := g.currentCanThrow
	g.receiver = recv
	g.currentReturnType = m.ReturnType
	g.currentCanThrow = m.CanThrow
	g.emitBlock(m.Body)
	g.receiver = savedRecv
	g.currentReturnType = savedRT
	g.currentCanThrow = savedCT
	g.pop()
	g.writeln("}")
	g.writeln("")
}

func (g *Generator) emitFn(fn *parser.FnDecl) {
	name := exportName(fn.Name, fn.IsPub)

	var params []string
	for _, p := range fn.Params {
		params = append(params, p.Name+" "+g.emitType(p.Type))
	}
	paramStr := strings.Join(params, ", ")

	retType := g.emitType(fn.ReturnType)
	var retStr string
	if fn.CanThrow {
		g.neededImports["fmt"] = true
		if retType != "" {
			retStr = " (" + retType + ", error)"
		} else {
			retStr = " error"
		}
	} else if retType != "" {
		retStr = " " + retType
	}

	// main() is special
	if fn.Name == "main" {
		name = "main"
		retStr = ""
	}

	typeParamStr := ""
	if len(fn.TypeParams) > 0 {
		constraints := make([]string, len(fn.TypeParams))
		for i, tp := range fn.TypeParams {
			constraints[i] = tp + " any"
		}
		typeParamStr = "[" + strings.Join(constraints, ", ") + "]"
	}
	g.writeln(fmt.Sprintf("func %s%s(%s)%s {", name, typeParamStr, paramStr, retStr))
	g.push()
	savedRT := g.currentReturnType
	savedCT := g.currentCanThrow
	g.currentReturnType = fn.ReturnType
	g.currentCanThrow = fn.CanThrow
	g.emitBlock(fn.Body)
	g.currentReturnType = savedRT
	g.currentCanThrow = savedCT
	g.pop()
	g.writeln("}")
}

// --- Statement Emission ------------------------------------------------------

func (g *Generator) emitBlock(b *parser.BlockStmt) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		g.emitStmt(s)
	}
}

func (g *Generator) emitStmt(s parser.Stmt) {
	switch st := s.(type) {
	case *parser.VarStmt:
		g.emitVarStmt(st)
	case *parser.TupleVarStmt:
		g.writeln(fmt.Sprintf("%s := %s", strings.Join(st.Names, ", "), g.emitExpr(st.Value)))
	case *parser.AssignStmt:
		g.emitAssignStmt(st)
	case *parser.ReturnStmt:
		g.emitReturnStmt(st)
	case *parser.IfStmt:
		g.emitIfStmt(st)
	case *parser.ForStmt:
		g.emitForStmt(st)
	case *parser.WhileStmt:
		g.emitWhileStmt(st)
	case *parser.GoStmt:
		g.emitGoStmt(st)
	case *parser.TryStmt:
		g.emitTryStmt(st)
	case *parser.ThrowStmt:
		g.emitThrowStmt(st)
	case *parser.PrintStmt:
		g.emitPrintStmt(st)
	case *parser.ExprStmt:
		g.emitExprStmt(st)
	case *parser.BlockStmt:
		g.writeln("{")
		g.push()
		g.emitBlock(st)
		g.pop()
		g.writeln("}")
	case *parser.MatchStmt:
		g.emitMatchStmt(st)
	case *parser.BreakStmt:
		g.writeln("break")
	case *parser.ContinueStmt:
		g.writeln("continue")
	}
}

func (g *Generator) emitMatchStmt(m *parser.MatchStmt) {
	g.writeIndent()
	g.write(fmt.Sprintf("switch %s {\n", g.emitExpr(m.Subject)))
	for _, c := range m.Cases {
		if c.Pattern == nil {
			g.writeln("default:")
		} else {
			g.writeln(fmt.Sprintf("case %s:", g.emitExpr(c.Pattern)))
		}
		g.push()
		g.emitBlock(c.Body)
		g.pop()
	}
	g.writeln("}")
}

func (g *Generator) emitVarStmt(v *parser.VarStmt) {
	// Special case: Chan.new(n) where type is Chan<T>
	if v.Value != nil {
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				if ident, ok := sel.Object.(*parser.Ident); ok && ident.Name == "Chan" && sel.Field == "new" {
					chanType := g.emitType(v.Type)
					bufSize := "0"
					if len(call.Args) > 0 {
						bufSize = g.emitExpr(call.Args[0])
					}
					g.writeln(fmt.Sprintf("%s := make(%s, %s)", v.Name, chanType, bufSize))
					return
				}
			}
		}
	}

	if v.Value != nil {
		valStr := g.emitExpr(v.Value)
		if v.Type != nil {
			// typed: var x Type = val → x := Type(val) or typed literal
			g.writeln(fmt.Sprintf("%s := %s", v.Name, valStr))
		} else {
			g.writeln(fmt.Sprintf("%s := %s", v.Name, valStr))
		}
	} else if v.Type != nil {
		// no value: var x Type  → var x GoType
		g.writeln(fmt.Sprintf("var %s %s", v.Name, g.emitType(v.Type)))
	} else {
		g.writeln(fmt.Sprintf("var %s interface{}", v.Name))
	}
}

func (g *Generator) emitAssignStmt(a *parser.AssignStmt) {
	g.writeln(fmt.Sprintf("%s %s %s", g.emitExpr(a.Target), a.Op, g.emitExpr(a.Value)))
}

func (g *Generator) emitReturnStmt(r *parser.ReturnStmt) {
	if r.Value != nil {
		val := g.emitExpr(r.Value)
		if g.currentCanThrow {
			g.writeln(fmt.Sprintf("return %s, nil", val))
		} else {
			g.writeln(fmt.Sprintf("return %s", val))
		}
	} else {
		if g.currentCanThrow {
			g.writeln("return nil")
		} else {
			g.writeln("return")
		}
	}
}

func (g *Generator) emitIfStmt(i *parser.IfStmt) {
	g.writeIndent()
	g.write(fmt.Sprintf("if %s ", g.emitExpr(i.Cond)))
	g.write("{\n")
	g.push()
	g.emitBlock(i.Then)
	g.pop()
	if i.ElseStmt != nil {
		g.writeIndent()
		g.write("} else ")
		switch e := i.ElseStmt.(type) {
		case *parser.IfStmt:
			g.write(fmt.Sprintf("if %s ", g.emitExpr(e.Cond)))
			g.write("{\n")
			g.push()
			g.emitBlock(e.Then)
			g.pop()
			// Recurse for else-if chain
			if e.ElseStmt != nil {
				g.writeIndent()
				g.write("} else {\n")
				g.push()
				g.emitStmt(e.ElseStmt)
				g.pop()
			}
			g.writeln("}")
		case *parser.BlockStmt:
			g.write("{\n")
			g.push()
			g.emitBlock(e)
			g.pop()
			g.writeln("}")
		}
	} else {
		g.writeln("}")
	}
}

func (g *Generator) emitForStmt(f *parser.ForStmt) {
	if f.IsRange {
		g.writeIndent()
		g.write(fmt.Sprintf("for _, %s := range %s ", f.Item, g.emitExpr(f.Range)))
		g.write("{\n")
		g.push()
		g.emitBlock(f.Body)
		g.pop()
		g.writeln("}")
		return
	}

	g.writeIndent()
	g.write("for ")
	if f.Init != nil {
		g.write(g.stmtInline(f.Init))
	}
	g.write("; ")
	if f.Cond != nil {
		g.write(g.emitExpr(f.Cond))
	}
	g.write("; ")
	if f.Post != nil {
		g.write(g.stmtInline(f.Post))
	}
	g.write(" {\n")
	g.push()
	g.emitBlock(f.Body)
	g.pop()
	g.writeln("}")
}

// stmtInline emits a statement as a single-line string (for for-init/post).
func (g *Generator) stmtInline(s parser.Stmt) string {
	switch st := s.(type) {
	case *parser.TupleVarStmt:
		return fmt.Sprintf("%s := %s", strings.Join(st.Names, ", "), g.emitExpr(st.Value))
	case *parser.VarStmt:
		if st.Value != nil {
			return fmt.Sprintf("%s := %s", st.Name, g.emitExpr(st.Value))
		}
		return fmt.Sprintf("var %s %s", st.Name, g.emitType(st.Type))
	case *parser.AssignStmt:
		return fmt.Sprintf("%s %s %s", g.emitExpr(st.Target), st.Op, g.emitExpr(st.Value))
	case *parser.ExprStmt:
		return g.emitExpr(st.Expr)
	}
	return ""
}

func (g *Generator) emitWhileStmt(w *parser.WhileStmt) {
	g.writeIndent()
	g.write(fmt.Sprintf("for %s ", g.emitExpr(w.Cond)))
	g.write("{\n")
	g.push()
	g.emitBlock(w.Body)
	g.pop()
	g.writeln("}")
}

func (g *Generator) emitGoStmt(gs *parser.GoStmt) {
	g.writeIndent()
	g.write("go func() {\n")
	g.push()
	g.emitBlock(gs.Body)
	g.pop()
	g.writeln("}()")
}

// emitTryStmt emits a try/catch block.
// The try body is wrapped in a func() error closure so that the first
// throwing call returns immediately and the catch block runs exactly once.
func (g *Generator) emitTryStmt(t *parser.TryStmt) {
	g.neededImports["fmt"] = true
	g.writeln("{")
	g.push()
	g.writeIndent()
	g.write(fmt.Sprintf("%s := func() error {\n", t.ErrVar))
	g.push()
	g.emitTryBody(t.Body)
	g.writeln("return nil")
	g.pop()
	g.writeln("}()")
	// Single catch block — runs at most once
	g.writeIndent()
	g.write(fmt.Sprintf("if %s != nil ", t.ErrVar))
	g.write("{\n")
	g.push()
	g.emitBlock(t.CatchBody)
	g.pop()
	g.writeln("}")
	g.pop()
	g.writeln("}")
}

func (g *Generator) emitTryBody(body *parser.BlockStmt) {
	for _, s := range body.Stmts {
		if g.stmtHasThrowingCall(s) {
			g.emitStmtWithErrReturn(s)
		} else {
			g.emitStmt(s)
		}
	}
}

func (g *Generator) stmtHasThrowingCall(s parser.Stmt) bool {
	switch st := s.(type) {
	case *parser.ExprStmt:
		return g.exprHasThrowingCall(st.Expr)
	case *parser.VarStmt:
		if st.Value != nil {
			return g.exprHasThrowingCall(st.Value)
		}
	case *parser.AssignStmt:
		return g.exprHasThrowingCall(st.Value)
	case *parser.ReturnStmt:
		if st.Value != nil {
			return g.exprHasThrowingCall(st.Value)
		}
	}
	return false
}

func (g *Generator) exprHasThrowingCall(e parser.Expr) bool {
	switch ex := e.(type) {
	case *parser.CallExpr:
		return g.callCanThrow(ex)
	}
	return false
}

func (g *Generator) callCanThrow(call *parser.CallExpr) bool {
	switch callee := call.Callee.(type) {
	case *parser.Ident:
		return g.canThrowFns[callee.Name]
	case *parser.SelectorExpr:
		if ident, ok := callee.Object.(*parser.Ident); ok {
			return g.canThrowFns[ident.Name+"."+callee.Field]
		}
		if _, ok := callee.Object.(*parser.ThisExpr); ok {
			return g.canThrowFns[callee.Field]
		}
	}
	return false
}

// emitStmtWithErrReturn emits a statement inside a try closure body.
// Throwing calls are unpacked and return the error immediately on failure.
func (g *Generator) emitStmtWithErrReturn(s parser.Stmt) {
	switch st := s.(type) {
	case *parser.ExprStmt:
		if call, ok := st.Expr.(*parser.CallExpr); ok {
			g.writeln(fmt.Sprintf("if _err := %s; _err != nil { return _err }", g.emitCallExpr(call)))
		} else {
			g.emitExprStmt(st)
		}
	case *parser.VarStmt:
		if st.Value != nil {
			if call, ok := st.Value.(*parser.CallExpr); ok {
				g.writeln(fmt.Sprintf("%s, _err := %s", st.Name, g.emitCallExpr(call)))
				g.writeln("if _err != nil { return _err }")
				return
			}
		}
		g.emitVarStmt(st)
	case *parser.AssignStmt:
		if call, ok := st.Value.(*parser.CallExpr); ok {
			g.writeln(fmt.Sprintf("%s, _err := %s", g.emitExpr(st.Target), g.emitCallExpr(call)))
			g.writeln("if _err != nil { return _err }")
			return
		}
		g.emitAssignStmt(st)
	default:
		g.emitStmt(s)
	}
}

func (g *Generator) inferCallReturnType(call *parser.CallExpr) string {
	// Simple heuristic: if the function is known and has a return type
	// For now just return empty — we don't track return types of arbitrary calls
	return ""
}

func (g *Generator) emitThrowStmt(t *parser.ThrowStmt) {
	g.neededImports["fmt"] = true
	// The thrown value must be something with a message — typically Error("msg") call
	// We emit: return <zero>, fmt.Errorf(msg)
	var errStr string
	switch v := t.Value.(type) {
	case *parser.CallExpr:
		// Error("msg") → fmt.Errorf("msg")
		if ident, ok := v.Callee.(*parser.Ident); ok && ident.Name == "Error" {
			if len(v.Args) > 0 {
				errStr = fmt.Sprintf("fmt.Errorf(%s)", g.emitExpr(v.Args[0]))
			} else {
				errStr = `fmt.Errorf("error")`
			}
		} else {
			errStr = fmt.Sprintf("fmt.Errorf(\"%%v\", %s)", g.emitExpr(t.Value))
		}
	default:
		errStr = fmt.Sprintf("fmt.Errorf(\"%%v\", %s)", g.emitExpr(t.Value))
	}

	zero := g.zeroValue(g.currentReturnType)
	if zero != "" {
		g.writeln(fmt.Sprintf("return %s, %s", zero, errStr))
	} else {
		g.writeln(fmt.Sprintf("return %s", errStr))
	}
}

func (g *Generator) emitPrintStmt(p *parser.PrintStmt) {
	g.neededImports["fmt"] = true
	// If value is an interp string, avoid double-wrapping in Println(Sprintf(...))
	if interp, ok := p.Value.(*parser.StringInterpLit); ok {
		g.writeln(fmt.Sprintf("fmt.Println(%s)", g.emitStringInterp(interp)))
	} else {
		g.writeln(fmt.Sprintf("fmt.Println(%s)", g.emitExpr(p.Value)))
	}
}

func (g *Generator) emitExprStmt(e *parser.ExprStmt) {
	// Special: SendExpr as statement
	if se, ok := e.Expr.(*parser.SendExpr); ok {
		g.writeln(fmt.Sprintf("%s <- %s", g.emitExpr(se.Chan), g.emitExpr(se.Value)))
		return
	}
	g.writeln(g.emitExpr(e.Expr))
}

// --- Expression Emission -----------------------------------------------------

func (g *Generator) emitExpr(e parser.Expr) string {
	if e == nil {
		return ""
	}
	switch ex := e.(type) {
	case *parser.IntLit:
		return ex.Value
	case *parser.FloatLit:
		return ex.Value
	case *parser.StringLit:
		return fmt.Sprintf("%q", ex.Value)
	case *parser.StringInterpLit:
		return g.emitStringInterp(ex)
	case *parser.BoolLit:
		if ex.Value {
			return "true"
		}
		return "false"
	case *parser.NullLit:
		return "nil"
	case *parser.Ident:
		return ex.Name
	case *parser.ThisExpr:
		if g.receiver != "" {
			return g.receiver
		}
		return "this"
	case *parser.SuperCallExpr:
		// Should have been extracted by parser; emit as comment
		return "/* super */"
	case *parser.BinaryExpr:
		return fmt.Sprintf("(%s %s %s)", g.emitExpr(ex.Left), ex.Op, g.emitExpr(ex.Right))
	case *parser.UnaryExpr:
		return fmt.Sprintf("(%s%s)", ex.Op, g.emitExpr(ex.Operand))
	case *parser.CallExpr:
		return g.emitCallExpr(ex)
	case *parser.SelectorExpr:
		return fmt.Sprintf("%s.%s", g.emitExpr(ex.Object), capitalize(ex.Field))
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", g.emitExpr(ex.Object), g.emitExpr(ex.Index))
	case *parser.SendExpr:
		// As expression (rare) — but send is usually a stmt
		return fmt.Sprintf("func() { %s <- %s }()", g.emitExpr(ex.Chan), g.emitExpr(ex.Value))
	case *parser.ReceiveExpr:
		return fmt.Sprintf("<-%s", g.emitExpr(ex.Chan))
	case *parser.ListLit:
		var elems []string
		for _, el := range ex.Elements {
			elems = append(elems, g.emitExpr(el))
		}
		return fmt.Sprintf("[]interface{}{%s}", strings.Join(elems, ", "))
	case *parser.MapLit:
		var pairs []string
		for i, k := range ex.Keys {
			pairs = append(pairs, g.emitExpr(k)+": "+g.emitExpr(ex.Values[i]))
		}
		return fmt.Sprintf("map[interface{}]interface{}{%s}", strings.Join(pairs, ", "))
	}
	return "/* unknown expr */"
}

func (g *Generator) emitStringInterp(s *parser.StringInterpLit) string {
	g.neededImports["fmt"] = true
	var fmtParts []string
	var args []string
	for _, part := range s.Parts {
		if sl, ok := part.(*parser.StringLit); ok {
			// Escape % in static text so fmt.Sprintf doesn't misinterpret it
			fmtParts = append(fmtParts, strings.ReplaceAll(sl.Value, "%", "%%"))
		} else {
			fmtParts = append(fmtParts, "%v")
			args = append(args, g.emitExpr(part))
		}
	}
	format := strings.Join(fmtParts, "")
	if len(args) == 0 {
		return fmt.Sprintf("%q", format)
	}
	return fmt.Sprintf("fmt.Sprintf(%q, %s)", format, strings.Join(args, ", "))
}

func (g *Generator) emitCallExpr(call *parser.CallExpr) string {
	var args []string
	for _, a := range call.Args {
		args = append(args, g.emitExpr(a))
	}
	argStr := strings.Join(args, ", ")

	switch callee := call.Callee.(type) {
	case *parser.SelectorExpr:
		// Could be ClassName.new(...) → NewClassName(...)
		if ident, ok := callee.Object.(*parser.Ident); ok {
			if g.classNames[ident.Name] && callee.Field == "new" {
				return fmt.Sprintf("New%s(%s)", ident.Name, argStr)
			}
		}
		// Could be send/receive (already handled via SendExpr/ReceiveExpr in parser)
		obj := g.emitExpr(callee.Object)
		method := capitalize(callee.Field)
		return fmt.Sprintf("%s.%s(%s)", obj, method, argStr)
	case *parser.Ident:
		return g.emitBuiltinCall(callee.Name, argStr, call.Args)
	default:
		return fmt.Sprintf("%s(%s)", g.emitExpr(callee), argStr)
	}
}

// emitBuiltinCall maps Growler built-in function names to Go equivalents.
func (g *Generator) emitBuiltinCall(name, argStr string, args []parser.Expr) string {
	switch name {
	// I/O
	case "print":
		g.neededImports["fmt"] = true
		return fmt.Sprintf("fmt.Println(%s)", argStr)
	case "printf":
		g.neededImports["fmt"] = true
		return fmt.Sprintf("fmt.Printf(%s)", argStr)
	case "readLine":
		g.neededImports["bufio"] = true
		g.neededImports["os"] = true
		return "func() string { s, _ := bufio.NewReader(os.Stdin).ReadString('\\n'); return s }()"

	// Type conversions
	case "toString":
		g.neededImports["fmt"] = true
		return fmt.Sprintf("fmt.Sprintf(\"%%v\", %s)", argStr)
	case "toInt", "parseInt":
		g.neededImports["strconv"] = true
		return fmt.Sprintf("func() int { n, _ := strconv.Atoi(%s); return n }()", argStr)
	case "toFloat", "parseFloat":
		g.neededImports["strconv"] = true
		return fmt.Sprintf("func() float64 { f, _ := strconv.ParseFloat(%s, 64); return f }()", argStr)
	case "toBool":
		g.neededImports["strconv"] = true
		return fmt.Sprintf("func() bool { b, _ := strconv.ParseBool(%s); return b }()", argStr)

	// Collections
	case "len":
		return fmt.Sprintf("len(%s)", argStr)
	case "append":
		return fmt.Sprintf("append(%s)", argStr)
	case "make":
		return fmt.Sprintf("make(%s)", argStr)
	case "delete":
		return fmt.Sprintf("delete(%s)", argStr)
	case "copy":
		return fmt.Sprintf("copy(%s)", argStr)

	// Math
	case "abs":
		g.neededImports["math"] = true
		if len(args) == 1 {
			return fmt.Sprintf("math.Abs(float64(%s))", argStr)
		}
	case "sqrt":
		g.neededImports["math"] = true
		return fmt.Sprintf("math.Sqrt(%s)", argStr)
	case "pow":
		g.neededImports["math"] = true
		return fmt.Sprintf("math.Pow(%s)", argStr)
	case "floor":
		g.neededImports["math"] = true
		return fmt.Sprintf("math.Floor(%s)", argStr)
	case "ceil":
		g.neededImports["math"] = true
		return fmt.Sprintf("math.Ceil(%s)", argStr)
	case "round":
		g.neededImports["math"] = true
		return fmt.Sprintf("math.Round(%s)", argStr)
	case "max":
		g.neededImports["math"] = true
		return fmt.Sprintf("math.Max(%s)", argStr)
	case "min":
		g.neededImports["math"] = true
		return fmt.Sprintf("math.Min(%s)", argStr)

	// String operations
	case "strLen":
		return fmt.Sprintf("len(%s)", argStr)
	case "strUpper":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.ToUpper(%s)", argStr)
	case "strLower":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.ToLower(%s)", argStr)
	case "strContains":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.Contains(%s)", argStr)
	case "strHasPrefix":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.HasPrefix(%s)", argStr)
	case "strHasSuffix":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.HasSuffix(%s)", argStr)
	case "strTrim":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.TrimSpace(%s)", argStr)
	case "strSplit":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.Split(%s)", argStr)
	case "strJoin":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.Join(%s)", argStr)
	case "strReplace":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.ReplaceAll(%s)", argStr)

	// Panic / exit
	case "panic":
		return fmt.Sprintf("panic(%s)", argStr)
	case "exit":
		g.neededImports["os"] = true
		return fmt.Sprintf("os.Exit(%s)", argStr)

	// Sorting
	case "sortInts":
		g.neededImports["sort"] = true
		return fmt.Sprintf("sort.Ints(%s)", argStr)
	case "sortStrings":
		g.neededImports["sort"] = true
		return fmt.Sprintf("sort.Strings(%s)", argStr)
	case "sortFloats":
		g.neededImports["sort"] = true
		return fmt.Sprintf("sort.Float64s(%s)", argStr)
	}
	// Default: pass through as-is
	return fmt.Sprintf("%s(%s)", name, argStr)
}

// --- Helpers -----------------------------------------------------------------

// exportName returns an exported (uppercase) or unexported (lowercase) name.
func exportName(name string, pub bool) string {
	if name == "" {
		return name
	}
	if pub {
		return capitalize(name)
	}
	return uncapitalize(name)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func uncapitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}
