package codegen

import (
	"fmt"
	"strings"
	"unicode"

	"zinc/internal/parser"
)

// goMultiReturnFuncs is the set of known Go stdlib functions that return (T, error).
// Used by emitWithStmt to auto-detect multi-return calls.
var goMultiReturnFuncs = map[string]bool{
	"os.Create":     true,
	"os.Open":       true,
	"os.OpenFile":   true,
	"os.CreateTemp": true,
	"os.MkdirTemp":  true,
	"net.Dial":      true,
	"net.Listen":    true,
	"http.Get":      true,
	"http.Post":     true,
	"http.NewRequest": true,
	"sql.Open":      true,
	"tls.Dial":      true,
}

// failableBuiltins is the set of Zinc built-in functions that are failable
// (their generated Go code returns (T, error) or error).
var failableBuiltins = map[string]bool{
	"readFile":  true,
	"writeFile": true,
	"httpGet":   true,
}

// Generator converts a Zinc AST to Go source code.
type Generator struct {
	buf            strings.Builder
	indent         int
	neededImports  map[string]bool
	classNames     map[string]bool // set of declared class names
	interfaceNames map[string]bool // set of declared interface names
	// canThrowFns: set of fn/method names that are failable (return errors)
	canThrowFns map[string]bool
	// current receiver name for method emission
	receiver string
	// current function return type (for zero-value in error returns)
	currentReturnType parser.TypeExpr
	// whether current function is failable (affects return stmt emission)
	currentCanThrow bool
	// whether we are in main() or a goroutine (errors must panic, not return)
	inMainOrGoroutine bool
	// map class name → CtorDecl for super-arg resolution
	classCtors map[string]*parser.CtorDecl
	// user-specified imports from import decls
	userImports []*parser.ImportDecl
	// enumNames: set of declared enum type names
	enumNames map[string]bool
	// throwingVars: local variables that hold failable lambdas
	throwingVars map[string]bool
	// packageName overrides the emitted package declaration.
	// Empty means auto-detect from PackageDecl or default to "main".
	packageName string
	// fnParams: top-level function name → param list (for default/named-arg resolution)
	fnParams map[string][]*parser.ParamDecl
	// methodParams: class name → method name → param list
	methodParams map[string]map[string][]*parser.ParamDecl
	// tmpCounter: monotonic counter for generating unique temp variable names
	tmpCounter int
	// errCounter: monotonic counter for unique error variable names
	errCounter int
	// srcFile: .zn source filename for //line directives (empty = disabled)
	srcFile string
	// lastDirectiveLine: last source line emitted in a //line directive (avoids duplicates)
	lastDirectiveLine int
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
		throwingVars:   make(map[string]bool),
		fnParams:       make(map[string][]*parser.ParamDecl),
		methodParams:   make(map[string]map[string][]*parser.ParamDecl),
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
		throwingVars:   make(map[string]bool),
		packageName:    pkgName,
		fnParams:       make(map[string][]*parser.ParamDecl),
		methodParams:   make(map[string]map[string][]*parser.ParamDecl),
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

// nextErr returns a unique error variable name.
func (g *Generator) nextErr() string {
	name := fmt.Sprintf("_err%d", g.errCounter)
	g.errCounter++
	return name
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

	// Collect fn and method param lists for named-arg / default resolution
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *parser.FnDecl:
			if len(d.Params) > 0 {
				g.fnParams[d.Name] = d.Params
			}
		case *parser.ClassDecl:
			for _, m := range d.Methods {
				if len(m.Params) > 0 {
					if g.methodParams[d.Name] == nil {
						g.methodParams[d.Name] = make(map[string][]*parser.ParamDecl)
					}
					g.methodParams[d.Name][m.Name] = m.Params
				}
			}
		}
	}

	// Mark failable functions using fixed-point iteration (transitive detection)
	for {
		changed := false
		for _, decl := range prog.Decls {
			switch d := decl.(type) {
			case *parser.FnDecl:
				if !d.CanThrow && g.bodyIsFailable(d.Body) {
					d.CanThrow = true
					g.canThrowFns[d.Name] = true
					changed = true
				}
			case *parser.ClassDecl:
				for _, m := range d.Methods {
					if !m.CanThrow && g.bodyIsFailable(m.Body) {
						m.CanThrow = true
						g.canThrowFns[d.Name+"."+m.Name] = true
						changed = true
					}
				}
			}
		}
		if !changed {
			break
		}
	}
}

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
	case *parser.ReturnStmt:
		// return Error(...) makes a function failable
		return g.isReturnError(st)
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
	case *parser.GoStmt:
		return g.bodyIsFailable(st.Body)
	case *parser.WithStmt:
		for _, r := range st.Resources {
			if g.exprIsFailable(r.Value) {
				return true
			}
		}
		return g.bodyIsFailable(st.Body)
	}
	return false
}

// isReturnError checks if a return statement is `return Error(...)`.
func (g *Generator) isReturnError(r *parser.ReturnStmt) bool {
	if r.Value == nil {
		return false
	}
	call, ok := r.Value.(*parser.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Callee.(*parser.Ident)
	return ok && ident.Name == "Error"
}

// exprIsFailable checks if an expression contains a failable call.
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

// callIsFailable checks whether a call expression calls a failable function.
func (g *Generator) callIsFailable(call *parser.CallExpr) bool {
	switch callee := call.Callee.(type) {
	case *parser.Ident:
		return g.canThrowFns[callee.Name] || g.throwingVars[callee.Name] || failableBuiltins[callee.Name]
	case *parser.SelectorExpr:
		if ident, ok := callee.Object.(*parser.Ident); ok {
			key := ident.Name + "." + callee.Field
			return g.canThrowFns[key] || goMultiReturnFuncs[key]
		}
		if _, ok := callee.Object.(*parser.ThisExpr); ok {
			return g.canThrowFns[callee.Field]
		}
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

// SetSourceFile sets the .zn filename for //line directive emission.
func (g *Generator) SetSourceFile(path string) {
	g.srcFile = path
}

// emitLineDirective writes a //line directive mapping the next Go line to the given Zinc source line.
func (g *Generator) emitLineDirective(srcLine int) {
	if g.srcFile == "" || srcLine <= 0 || srcLine == g.lastDirectiveLine {
		return
	}
	g.lastDirectiveLine = srcLine
	g.buf.WriteString(fmt.Sprintf("//line %s:%d\n", g.srcFile, srcLine))
}

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
	case *parser.FuncTypeExpr:
		params := make([]string, len(typ.Params))
		for i, p := range typ.Params {
			params[i] = g.emitType(p)
		}
		ret := g.emitType(typ.ReturnType)
		if ret == "" {
			return "func(" + strings.Join(params, ", ") + ")"
		}
		return "func(" + strings.Join(params, ", ") + ") " + ret
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
	case *parser.FuncTypeExpr:
		return "nil"
	}
	return "nil"
}

// --- Top-Level Emission ------------------------------------------------------

func (g *Generator) emitTopLevel(decl parser.TopLevelDecl) {
	switch d := decl.(type) {
	case *parser.ClassDecl:
		g.emitLineDirective(d.Line)
		g.emitClass(d)
	case *parser.InterfaceDecl:
		g.emitInterface(d)
	case *parser.FnDecl:
		g.emitLineDirective(d.Line)
		g.emitFn(d)
	case *parser.EnumDecl:
		g.emitLineDirective(d.Line)
		g.emitEnum(d)
	case *parser.ConstDecl:
		g.emitLineDirective(d.Line)
		g.emitConstDecl(d)
	}
}

// --- Const Emission ----------------------------------------------------------

func (g *Generator) emitConstDecl(d *parser.ConstDecl) {
	name := d.Name
	if len(name) > 0 {
		name = strings.ToUpper(name[:1]) + name[1:]
	}
	val := g.emitExpr(d.Value)
	if d.Type != nil {
		goType := g.emitType(d.Type)
		g.writeln(fmt.Sprintf("const %s %s = %s", name, goType, val))
	} else {
		g.writeln(fmt.Sprintf("const %s = %s", name, val))
	}
	g.writeln("")
}

// --- Enum Emission -----------------------------------------------------------

func (g *Generator) emitEnum(e *parser.EnumDecl) {
	g.writeln(fmt.Sprintf("type %s int", e.Name))
	g.writeln("")
	g.writeln("const (")
	g.push()
	for i, v := range e.Variants {
		if i == 0 {
			g.writeln(fmt.Sprintf("%s%s %s = iota", e.Name, v, e.Name))
		} else {
			g.writeln(fmt.Sprintf("%s%s", e.Name, v))
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

	// Verify interface implementation (compile-time check).
	// Skip for generic classes: can't instantiate without concrete type args.
	if len(cls.TypeParams) == 0 {
		for _, iface := range ifaces {
			g.writeln(fmt.Sprintf("var _ %s = (*%s)(nil)", iface, cls.Name))
		}
		if len(ifaces) > 0 {
			g.writeln("")
		}
	}

	// 2. Constructor
	classInstStr := cls.Name
	if len(cls.TypeParams) > 0 {
		typeArgs := make([]string, len(cls.TypeParams))
		for i, tp := range cls.TypeParams {
			typeArgs[i] = tp
		}
		classInstStr = cls.Name + "[" + strings.Join(typeArgs, ", ") + "]"
	}
	if cls.Ctor != nil {
		g.emitCtor(cls, baseClass)
	} else {
		// No explicit constructor — emit a default no-arg one so ClassName.new() works.
		g.writeln(fmt.Sprintf("func New%s%s() *%s {", cls.Name, typeParamStr, classInstStr))
		g.push()
		g.writeln(fmt.Sprintf("return &%s{}", classInstStr))
		g.pop()
		g.writeln("}")
		g.writeln("")
	}

	// 3. Methods
	recv := strings.ToLower(cls.Name[:1])
	g.receiver = recv
	for _, m := range cls.Methods {
		g.emitMethod(cls.Name, cls.TypeParams, recv, m)
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

	// Build type parameter string and instantiated type for generic classes.
	// e.g. class Box<T> → func NewBox[T any](v T) *Box[T]
	typeParamStr := ""
	classInstStr := cls.Name
	if len(cls.TypeParams) > 0 {
		constraints := make([]string, len(cls.TypeParams))
		typeArgs := make([]string, len(cls.TypeParams))
		for i, tp := range cls.TypeParams {
			constraints[i] = tp + " any"
			typeArgs[i] = tp
		}
		typeParamStr = "[" + strings.Join(constraints, ", ") + "]"
		classInstStr = cls.Name + "[" + strings.Join(typeArgs, ", ") + "]"
	}

	g.writeln(fmt.Sprintf("func %s%s(%s) *%s {", name, typeParamStr, paramStr, classInstStr))
	g.push()

	// Build struct literal
	g.writeIndent()
	g.write(fmt.Sprintf("obj := &%s{\n", classInstStr))

	g.push()
	// Base class init — call the parent constructor to avoid field-name mismatches.
	if baseClass != "" {
		var superArgStrs []string
		for _, arg := range ctor.SuperArgs {
			superArgStrs = append(superArgStrs, g.emitExpr(arg))
		}
		if g.classCtors[baseClass] != nil {
			// Parent has a named constructor: embed via *NewParent(args...)
			g.writeln(fmt.Sprintf("%s: *New%s(%s),", baseClass, baseClass, strings.Join(superArgStrs, ", ")))
		} else {
			// Parent has no registered constructor: zero-value embed
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
	savedRecv := g.receiver
	g.receiver = "obj"
	for _, s := range ctor.Body.Stmts {
		g.emitStmt(s)
	}
	g.receiver = savedRecv

	g.writeln("return obj")
	g.pop()
	g.writeln("}")
	g.writeln("")
}

func (g *Generator) emitMethod(className string, typeParams []string, recv string, m *parser.MethodDecl) {
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

	// For generic classes, the receiver type must include type params.
	// e.g. func (b *Box[T]) Get() T
	recvTypeStr := className
	if len(typeParams) > 0 {
		recvTypeStr = className + "[" + strings.Join(typeParams, ", ") + "]"
	}

	if m.IsStatic {
		g.writeln(fmt.Sprintf("func %s_%s(%s)%s {", className, name, paramStr, retStr))
	} else {
		g.writeln(fmt.Sprintf("func (%s *%s) %s(%s)%s {", recv, recvTypeStr, name, paramStr, retStr))
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
			constraints[i] = tp + " " + typeParamConstraint(tp, fn.Params)
		}
		typeParamStr = "[" + strings.Join(constraints, ", ") + "]"
	}
	g.writeln(fmt.Sprintf("func %s%s(%s)%s {", name, typeParamStr, paramStr, retStr))
	g.push()
	savedRT := g.currentReturnType
	savedCT := g.currentCanThrow
	savedMG := g.inMainOrGoroutine
	g.currentReturnType = fn.ReturnType
	g.currentCanThrow = fn.CanThrow
	if fn.Name == "main" {
		g.inMainOrGoroutine = true
	}
	g.emitBlock(fn.Body)
	g.currentReturnType = savedRT
	g.currentCanThrow = savedCT
	g.inMainOrGoroutine = savedMG
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
	// Emit //line directive for statements that carry source position
	switch st := s.(type) {
	case *parser.VarStmt:
		g.emitLineDirective(st.Line)
	case *parser.TupleVarStmt:
		g.emitLineDirective(st.Line)
	case *parser.AssignStmt:
		g.emitLineDirective(st.Line)
	case *parser.ReturnStmt:
		g.emitLineDirective(st.Line)
	case *parser.IfStmt:
		g.emitLineDirective(st.Line)
	case *parser.ForStmt:
		g.emitLineDirective(st.Line)
	case *parser.WhileStmt:
		g.emitLineDirective(st.Line)
	case *parser.PrintStmt:
		g.emitLineDirective(st.Line)
	case *parser.ExprStmt:
		g.emitLineDirective(st.Line)
	case *parser.MatchStmt:
		g.emitLineDirective(st.Line)
	case *parser.WithStmt:
		g.emitLineDirective(st.Line)
	}

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
		if st.Label != "" {
			g.writeln(fmt.Sprintf("break %s", st.Label))
		} else {
			g.writeln("break")
		}
	case *parser.ContinueStmt:
		if st.Label != "" {
			g.writeln(fmt.Sprintf("continue %s", st.Label))
		} else {
			g.writeln("continue")
		}
	case *parser.WithStmt:
		g.emitWithStmt(st)
	case *parser.ListAddStmt:
		list := g.emitExpr(st.List)
		val := g.emitExpr(st.Value)
		g.writeln(fmt.Sprintf("%s = append(%s, %s)", list, list, val))
	case *parser.MapRemoveStmt:
		m := g.emitExpr(st.Map)
		key := g.emitExpr(st.Key)
		g.writeln(fmt.Sprintf("delete(%s, %s)", m, key))
	case *parser.ListSortStmt:
		g.neededImports["sort"] = true
		list := g.emitExpr(st.List)
		g.writeln(fmt.Sprintf("sort.Slice(%s, func(i, j int) bool { return %s[i] < %s[j] })", list, list, list))
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
	// Track variables that hold failable lambdas
	if lambda, ok := v.Value.(*parser.LambdaExpr); ok && lambda.Body != nil {
		if g.bodyIsFailable(lambda.Body) {
			g.throwingVars[v.Name] = true
		}
	}

	// Special case: collection .new() constructors — Chan, List, Map
	if v.Value != nil {
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
				if ident, ok := sel.Object.(*parser.Ident); ok && sel.Field == "new" {
					switch ident.Name {
					case "Chan":
						chanType := g.emitType(v.Type)
						bufSize := "0"
						if len(call.Args) > 0 {
							bufSize = g.emitExpr(call.Args[0])
						}
						g.writeln(fmt.Sprintf("%s := make(%s, %s)", v.Name, chanType, bufSize))
						return
					case "List":
						listType := g.emitType(v.Type)
						g.writeln(fmt.Sprintf("%s := %s{}", v.Name, listType))
						return
					case "Map":
						mapType := g.emitType(v.Type)
						g.writeln(fmt.Sprintf("%s := %s{}", v.Name, mapType))
						return
					}
				}
			}
		}
	}

	// Check if value is a failable call — needs error unpacking
	if v.Value != nil {
		if call, ok := v.Value.(*parser.CallExpr); ok && g.callIsFailable(call) {
			g.emitFailableVarStmt(v, call)
			return
		}
	}

	if v.Value != nil {
		valStr := g.emitExpr(v.Value)
		if v.Type != nil {
			if st, ok := v.Type.(*parser.SimpleType); ok {
				if g.enumNames[st.Name] {
					g.writeln(fmt.Sprintf("%s := %s(%s)", v.Name, st.Name, valStr))
					return
				}
				if st.Name == "Any" {
					g.writeln(fmt.Sprintf("var %s interface{} = %s", v.Name, valStr))
					return
				}
			}
			if _, isNull := v.Value.(*parser.NullLit); isNull {
				g.writeln(fmt.Sprintf("var %s %s = nil", v.Name, g.emitType(v.Type)))
				return
			}
		}
		g.writeln(fmt.Sprintf("%s := %s", v.Name, valStr))
	} else if v.Type != nil {
		g.writeln(fmt.Sprintf("var %s %s", v.Name, g.emitType(v.Type)))
	} else {
		g.writeln(fmt.Sprintf("var %s interface{}", v.Name))
	}
}

// emitFailableVarStmt emits a var statement where the value is a failable call.
// Generates: name, _errN := call(); if _errN != nil { <handler or auto-propagate> }
func (g *Generator) emitFailableVarStmt(v *parser.VarStmt, call *parser.CallExpr) {
	errVar := g.nextErr()
	callStr := g.emitFailableCallExpr(call)
	g.writeln(fmt.Sprintf("%s, %s := %s", v.Name, errVar, callStr))
	g.emitErrorCheck(errVar, v.OrHandler)
}

// emitFailableCallExpr emits a call expression in failable context, where we
// need the (T, error) return form. For builtins, this differs from the default
// expression-position emission which uses must-style (panic on error).
func (g *Generator) emitFailableCallExpr(call *parser.CallExpr) string {
	if ident, ok := call.Callee.(*parser.Ident); ok {
		if failableBuiltins[ident.Name] {
			return g.emitFailableBuiltinCall(ident.Name, call)
		}
	}
	return g.emitCallExpr(call)
}

// emitFailableBuiltinCall emits a failable builtin in (T, error) form.
func (g *Generator) emitFailableBuiltinCall(name string, call *parser.CallExpr) string {
	args := make([]string, len(call.Args))
	for i, a := range call.Args {
		args[i] = g.emitExpr(a)
	}
	argStr := strings.Join(args, ", ")
	switch name {
	case "readFile":
		g.neededImports["os"] = true
		return fmt.Sprintf("func() (string, error) { b, err := os.ReadFile(%s); return string(b), err }()", argStr)
	case "writeFile":
		g.neededImports["os"] = true
		if len(call.Args) == 2 {
			return fmt.Sprintf("func() error { return os.WriteFile(%s, []byte(%s), 0644) }()", args[0], args[1])
		}
		return fmt.Sprintf("func() error { return os.WriteFile(%s) }()", argStr)
	case "httpGet":
		g.neededImports["net/http"] = true
		g.neededImports["io"] = true
		return fmt.Sprintf("func() (string, error) { resp, err := http.Get(%s); if err != nil { return \"\", err }; defer resp.Body.Close(); b, err := io.ReadAll(resp.Body); return string(b), err }()", argStr)
	}
	return g.emitCallExpr(call)
}

// emitErrorCheck emits an if-block that checks errVar and either runs the handler
// or auto-propagates the error.
func (g *Generator) emitErrorCheck(errVar string, handler *parser.OrHandler) {
	if handler != nil {
		g.emitOrHandlerBlock(errVar, handler)
		return
	}
	// Auto-propagation
	if g.inMainOrGoroutine {
		g.writeln(fmt.Sprintf("if %s != nil { panic(%s) }", errVar, errVar))
	} else if g.currentCanThrow {
		zero := g.zeroValue(g.currentReturnType)
		if zero != "" {
			g.writeln(fmt.Sprintf("if %s != nil { return %s, %s }", errVar, zero, errVar))
		} else {
			g.writeln(fmt.Sprintf("if %s != nil { return %s }", errVar, errVar))
		}
	} else {
		g.writeln(fmt.Sprintf("if %s != nil { panic(%s) }", errVar, errVar))
	}
}

// emitOrHandlerBlock emits an or { } handler block.
// Inside the handler, `err` is bound to the error variable.
func (g *Generator) emitOrHandlerBlock(errVar string, handler *parser.OrHandler) {
	g.writeIndent()
	g.write(fmt.Sprintf("if %s != nil {\n", errVar))
	g.push()
	// Bind err to the error variable (use .Error() for string representation)
	g.writeln(fmt.Sprintf("err := %s.Error()", errVar))
	g.writeln("_ = err")
	// Check if handler body contains exit/panic calls — if so, no auto-return after
	hasHalt := g.handlerHasHalt(handler.Body)
	g.emitBlock(handler.Body)
	if !hasHalt {
		// Auto-propagate after handler body (handler adds context but error still propagates)
		// Find the last statement's error expression if it's an Error(...) call
		lastErr := g.extractHandlerError(handler.Body)
		if lastErr != "" {
			if g.inMainOrGoroutine {
				g.writeln(fmt.Sprintf("panic(%s)", lastErr))
			} else if g.currentCanThrow {
				zero := g.zeroValue(g.currentReturnType)
				if zero != "" {
					g.writeln(fmt.Sprintf("return %s, %s", zero, lastErr))
				} else {
					g.writeln(fmt.Sprintf("return %s", lastErr))
				}
			} else {
				g.writeln(fmt.Sprintf("panic(%s)", lastErr))
			}
		} else {
			// No explicit Error() in handler, just propagate original error
			if g.inMainOrGoroutine {
				g.writeln(fmt.Sprintf("panic(%s)", errVar))
			} else if g.currentCanThrow {
				zero := g.zeroValue(g.currentReturnType)
				if zero != "" {
					g.writeln(fmt.Sprintf("return %s, %s", zero, errVar))
				} else {
					g.writeln(fmt.Sprintf("return %s", errVar))
				}
			} else {
				g.writeln(fmt.Sprintf("panic(%s)", errVar))
			}
		}
	}
	// Suppress unused variable warning if handler doesn't reference err
	_ = errVar
	g.pop()
	g.writeln("}")
}

// handlerHasHalt checks if the handler body contains exit() or panic() calls.
func (g *Generator) handlerHasHalt(body *parser.BlockStmt) bool {
	if body == nil {
		return false
	}
	for _, s := range body.Stmts {
		if es, ok := s.(*parser.ExprStmt); ok {
			if call, ok := es.Expr.(*parser.CallExpr); ok {
				if ident, ok := call.Callee.(*parser.Ident); ok {
					if ident.Name == "exit" || ident.Name == "panic" {
						return true
					}
				}
			}
		}
	}
	return false
}

// extractHandlerError extracts the error expression from the last statement
// of an or handler body, if it's an Error(...) call expression statement.
func (g *Generator) extractHandlerError(body *parser.BlockStmt) string {
	if body == nil || len(body.Stmts) == 0 {
		return ""
	}
	last := body.Stmts[len(body.Stmts)-1]
	if es, ok := last.(*parser.ExprStmt); ok {
		if call, ok := es.Expr.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "Error" {
				// Remove this from the body so it's not emitted twice
				body.Stmts = body.Stmts[:len(body.Stmts)-1]
				return g.emitErrorExpr(call)
			}
		}
	}
	return ""
}

// emitErrorExpr converts Error("msg") or Error("msg", baseErr) to Go fmt.Errorf.
func (g *Generator) emitErrorExpr(call *parser.CallExpr) string {
	g.neededImports["fmt"] = true
	if len(call.Args) == 0 {
		return `fmt.Errorf("error")`
	}
	if len(call.Args) == 1 {
		return fmt.Sprintf("fmt.Errorf(%s)", g.emitExpr(call.Args[0]))
	}
	// Two args: Error("msg", baseErr) → fmt.Errorf("msg: %w", baseErr)
	msgExpr := g.emitExpr(call.Args[0])
	baseErr := g.emitExpr(call.Args[1])
	// Modify the format string to append ": %w"
	if strings.HasPrefix(msgExpr, "\"") && strings.HasSuffix(msgExpr, "\"") {
		// Strip quotes, append ": %w", re-quote
		inner := msgExpr[1 : len(msgExpr)-1]
		return fmt.Sprintf("fmt.Errorf(\"%s: %%w\", %s)", inner, baseErr)
	}
	// Dynamic string — use Sprintf
	return fmt.Sprintf("fmt.Errorf(\"%%s: %%w\", %s, %s)", msgExpr, baseErr)
}

func (g *Generator) emitAssignStmt(a *parser.AssignStmt) {
	// Check if value is a failable call — needs error unpacking
	if call, ok := a.Value.(*parser.CallExpr); ok && g.callIsFailable(call) {
		errVar := g.nextErr()
		g.writeln("{")
		g.push()
		g.writeln(fmt.Sprintf("_val, %s := %s", errVar, g.emitFailableCallExpr(call)))
		g.emitErrorCheck(errVar, a.OrHandler)
		g.writeln(fmt.Sprintf("%s %s _val", g.emitExpr(a.Target), a.Op))
		g.pop()
		g.writeln("}")
		return
	}
	g.writeln(fmt.Sprintf("%s %s %s", g.emitExpr(a.Target), a.Op, g.emitExpr(a.Value)))
}

func (g *Generator) emitReturnStmt(r *parser.ReturnStmt) {
	if r.Value != nil {
		// Check for return Error(...) — leaf error production
		if call, ok := r.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "Error" {
				errStr := g.emitErrorExpr(call)
				zero := g.zeroValue(g.currentReturnType)
				if zero != "" {
					g.writeln(fmt.Sprintf("return %s, %s", zero, errStr))
				} else {
					g.writeln(fmt.Sprintf("return %s", errStr))
				}
				return
			}
		}
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
	if f.Label != "" {
		g.writeln(fmt.Sprintf("%s:", f.Label))
	}
	if f.IsRange {
		g.writeIndent()
		indexVarName := "_"
		if f.IndexVar != "" {
			indexVarName = f.IndexVar
		}
		g.write(fmt.Sprintf("for %s, %s := range %s ", indexVarName, f.Item, g.emitExpr(f.Range)))
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
	if w.Label != "" {
		g.writeln(fmt.Sprintf("%s:", w.Label))
	}
	g.writeIndent()
	g.write(fmt.Sprintf("for %s ", g.emitExpr(w.Cond)))
	g.write("{\n")
	g.push()
	g.emitBlock(w.Body)
	g.pop()
	g.writeln("}")
}

// emitSafeNavStmt emits safe navigation in statement context (no return value needed).
//   obj?.method(args)  →  if obj != nil { obj.Method(args) }
//
// Follows Kotlin/C#/Swift semantics: if receiver is nil, the call is skipped.
func (g *Generator) emitSafeNavStmt(sn *parser.SafeNavExpr) {
	obj := g.emitExpr(sn.Object)
	field := capitalize(sn.Field)
	if sn.Call != nil {
		var argStrs []string
		for _, a := range sn.Call.Args {
			argStrs = append(argStrs, g.emitExpr(a))
		}
		g.writeln(fmt.Sprintf("if %s != nil { %s.%s(%s) }", obj, obj, field, strings.Join(argStrs, ", ")))
	} else {
		g.writeln(fmt.Sprintf("_ = %s // safe-nav (no-op)", obj))
	}
}

// nextTmp returns a unique temporary variable name.
func (g *Generator) nextTmp() string {
	g.tmpCounter++
	return fmt.Sprintf("_sn%d", g.tmpCounter)
}

// flattenSafeNavChain walks a chain of SafeNavExpr nodes and returns them
// in order from outermost receiver to innermost field/method access.
// e.g. a?.b?.c → [{obj:a, field:b}, {obj:<prev>, field:c}]
func flattenSafeNavChain(sn *parser.SafeNavExpr) []*parser.SafeNavExpr {
	var chain []*parser.SafeNavExpr
	for {
		chain = append([]*parser.SafeNavExpr{sn}, chain...)
		inner, ok := sn.Object.(*parser.SafeNavExpr)
		if !ok {
			break
		}
		sn = inner
	}
	return chain
}

// emitSafeNav emits safe navigation in expression context.
//
// Design follows Kotlin/C#/Swift/TypeScript semantics:
//   - If receiver is nil, the entire expression evaluates to nil (zero value)
//   - Chaining a?.b?.c produces flat sequential nil checks inside a single IIFE
//   - No nested IIFEs — one function, sequential guards, clean generated Go
//
// Single:  obj?.field  →  func() interface{} { if obj != nil { return obj.Field }; return nil }()
// Chain:   a?.b?.c     →  func() interface{} { _v := a; if _v == nil { return nil };
//                            _v2 := _v.B; if _v2 == nil { return nil }; return _v2.C }()
func (g *Generator) emitSafeNav(sn *parser.SafeNavExpr) string {
	chain := flattenSafeNavChain(sn)

	if len(chain) == 1 {
		// Simple case: no chaining — clean single IIFE
		return g.emitSafeNavSingle(sn)
	}

	// Chained case: single IIFE with flat sequential nil checks.
	// Each step gets its own typed variable — no type erasure mid-chain.
	//
	// a?.b?.c generates:
	//   func() interface{} {
	//     _s0 := a; if _s0 == nil { return nil }
	//     _s1 := _s0.B; if _s1 == nil { return nil }
	//     return _s1.C
	//   }()
	var body strings.Builder
	rootObj := g.emitExpr(chain[0].Object)

	prevVar := "_s0"
	body.WriteString(fmt.Sprintf("%s := %s; if %s == nil { return nil }; ", prevVar, rootObj, prevVar))

	for i, step := range chain {
		field := capitalize(step.Field)
		isLast := i == len(chain)-1

		if isLast && step.Call != nil {
			var argStrs []string
			for _, a := range step.Call.Args {
				argStrs = append(argStrs, g.emitExpr(a))
			}
			body.WriteString(fmt.Sprintf("return %s.%s(%s)", prevVar, field, strings.Join(argStrs, ", ")))
		} else if isLast {
			body.WriteString(fmt.Sprintf("return %s.%s", prevVar, field))
		} else {
			nextVar := fmt.Sprintf("_s%d", i+1)
			body.WriteString(fmt.Sprintf("%s := %s.%s; if %s == nil { return nil }; ", nextVar, prevVar, field, nextVar))
			prevVar = nextVar
		}
	}

	return fmt.Sprintf("func() interface{} { %s }()", body.String())
}

// emitSafeNavSingle emits a single (non-chained) safe navigation as an IIFE.
func (g *Generator) emitSafeNavSingle(sn *parser.SafeNavExpr) string {
	obj := g.emitExpr(sn.Object)
	field := capitalize(sn.Field)
	if sn.Call != nil {
		var argStrs []string
		for _, a := range sn.Call.Args {
			argStrs = append(argStrs, g.emitExpr(a))
		}
		args := strings.Join(argStrs, ", ")
		return fmt.Sprintf("func() interface{} { if %s != nil { return %s.%s(%s) }; return nil }()", obj, obj, field, args)
	}
	return fmt.Sprintf("func() interface{} { if %s != nil { return %s.%s }; return nil }()", obj, obj, field)
}

func (g *Generator) emitGoStmt(gs *parser.GoStmt) {
	savedCT := g.currentCanThrow
	savedRT := g.currentReturnType
	savedMG := g.inMainOrGoroutine
	g.currentCanThrow = false
	g.currentReturnType = nil
	g.inMainOrGoroutine = true // goroutines can't return errors — must panic
	g.writeIndent()
	g.write("go func() {\n")
	g.push()
	g.emitBlock(gs.Body)
	g.pop()
	g.writeln("}()")
	g.currentCanThrow = savedCT
	g.currentReturnType = savedRT
	g.inMainOrGoroutine = savedMG
}

// emitWithStmt emits a scoped resource block.
// For each resource two runtime interface checks are emitted:
//   - io.Closer  → defer Close()        (e.g. files, connections)
//   - sync.Locker → Lock() + defer Unlock() (e.g. mutexes, RWMutexes)
//
// Defer order: Closer is registered first so it runs last; Locker is registered
// second so Unlock runs before Close — correct for anything implementing both.
// If a resource implements neither, both assertions are false and nothing happens.
func (g *Generator) emitWithStmt(w *parser.WithStmt) {
	g.neededImports["io"] = true
	g.neededImports["sync"] = true
	// Auto-detect failable calls for each resource.
	for _, r := range w.Resources {
		if r.AutoErr {
			continue
		}
		if call, ok := r.Value.(*parser.CallExpr); ok && g.callIsFailable(call) {
			r.AutoErr = true
		}
	}
	g.writeln("{")
	g.push()
	for _, r := range w.Resources {
		if r.AutoErr {
			errVar := g.nextErr()
			callStr := g.emitExpr(r.Value)
			if call, ok := r.Value.(*parser.CallExpr); ok {
				callStr = g.emitFailableCallExpr(call)
			}
			g.writeln(fmt.Sprintf("%s, %s := %s", r.Name, errVar, callStr))
			g.emitErrorCheck(errVar, r.OrHandler)
		} else {
			g.writeln(fmt.Sprintf("%s := %s", r.Name, g.emitExpr(r.Value)))
		}
		g.writeln(fmt.Sprintf("if _c, ok := any(%s).(io.Closer); ok { defer _c.Close() }", r.Name))
		g.writeln(fmt.Sprintf("if _l, ok := any(&%s).(sync.Locker); ok { _l.Lock(); defer _l.Unlock() } else if _l, ok := any(%s).(sync.Locker); ok { _l.Lock(); defer _l.Unlock() }", r.Name, r.Name))
	}
	g.emitBlock(w.Body)
	g.pop()
	g.writeln("}")
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
	// Dual Stmt+Expr nodes that may flow through ExprStmt — delegate to emitStmt
	switch st := e.Expr.(type) {
	case *parser.ListAddStmt:
		g.emitStmt(st)
		return
	case *parser.MapRemoveStmt:
		g.emitStmt(st)
		return
	case *parser.ListSortStmt:
		g.emitStmt(st)
		return
	}
	// Special: SafeNavExpr as statement — emit clean if-guard, no IIFE
	if sn, ok := e.Expr.(*parser.SafeNavExpr); ok {
		g.emitSafeNavStmt(sn)
		return
	}
	// Failable standalone call — auto-propagate
	if call, ok := e.Expr.(*parser.CallExpr); ok && g.callIsFailable(call) {
		errVar := g.nextErr()
		callStr := g.emitFailableCallExpr(call)
		// For void failable calls (writeFile), the call itself returns just error
		if g.isVoidFailable(call) {
			g.writeln(fmt.Sprintf("if %s := %s; %s != nil {", errVar, callStr, errVar))
		} else {
			g.writeln(fmt.Sprintf("if _, %s := %s; %s != nil {", errVar, callStr, errVar))
		}
		g.push()
		if e.OrHandler != nil {
			g.writeln(fmt.Sprintf("err := %s.Error()", errVar))
			g.writeln("_ = err")
			hasHalt := g.handlerHasHalt(e.OrHandler.Body)
			lastErr := g.extractHandlerError(e.OrHandler.Body)
			g.emitBlock(e.OrHandler.Body)
			if !hasHalt {
				propagateErr := errVar
				if lastErr != "" {
					propagateErr = lastErr
				}
				if g.inMainOrGoroutine {
					g.writeln(fmt.Sprintf("panic(%s)", propagateErr))
				} else if g.currentCanThrow {
					zero := g.zeroValue(g.currentReturnType)
					if zero != "" {
						g.writeln(fmt.Sprintf("return %s, %s", zero, propagateErr))
					} else {
						g.writeln(fmt.Sprintf("return %s", propagateErr))
					}
				} else {
					g.writeln(fmt.Sprintf("panic(%s)", propagateErr))
				}
			}
		} else {
			if g.inMainOrGoroutine {
				g.writeln(fmt.Sprintf("panic(%s)", errVar))
			} else if g.currentCanThrow {
				zero := g.zeroValue(g.currentReturnType)
				if zero != "" {
					g.writeln(fmt.Sprintf("return %s, %s", zero, errVar))
				} else {
					g.writeln(fmt.Sprintf("return %s", errVar))
				}
			} else {
				g.writeln(fmt.Sprintf("panic(%s)", errVar))
			}
		}
		g.pop()
		g.writeln("}")
		return
	}
	g.writeln(g.emitExpr(e.Expr))
}

// isVoidFailable returns true if a failable call returns just error (no value).
func (g *Generator) isVoidFailable(call *parser.CallExpr) bool {
	if ident, ok := call.Callee.(*parser.Ident); ok {
		return ident.Name == "writeFile"
	}
	return false
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
	case *parser.LambdaExpr:
		return g.emitLambda(ex)
	case *parser.SelectorExpr:
		if ident, ok := ex.Object.(*parser.Ident); ok {
			if g.enumNames[ident.Name] {
				return ident.Name + capitalize(ex.Field)
			}
		}
		return fmt.Sprintf("%s.%s", g.emitExpr(ex.Object), capitalize(ex.Field))
	case *parser.SafeNavExpr:
		return g.emitSafeNav(ex)
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", g.emitExpr(ex.Object), g.emitExpr(ex.Index))
	case *parser.SliceExpr:
		low := ""
		if ex.Low != nil {
			low = g.emitExpr(ex.Low)
		}
		high := ""
		if ex.High != nil {
			high = g.emitExpr(ex.High)
		}
		return fmt.Sprintf("%s[%s:%s]", g.emitExpr(ex.Object), low, high)
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
		if ex.ResolvedType != "" {
			return fmt.Sprintf("%s{%s}", ex.ResolvedType, strings.Join(elems, ", "))
		}
		elemType := "interface{}"
		if len(ex.Elements) > 0 {
			switch ex.Elements[0].(type) {
			case *parser.IntLit:
				elemType = "int"
			case *parser.FloatLit:
				elemType = "float64"
			case *parser.StringLit:
				elemType = "string"
			case *parser.BoolLit:
				elemType = "bool"
			}
		}
		return fmt.Sprintf("[]%s{%s}", elemType, strings.Join(elems, ", "))
	case *parser.MapLit:
		var pairs []string
		for i, k := range ex.Keys {
			pairs = append(pairs, g.emitExpr(k)+": "+g.emitExpr(ex.Values[i]))
		}
		if ex.ResolvedType != "" {
			return fmt.Sprintf("%s{%s}", ex.ResolvedType, strings.Join(pairs, ", "))
		}
		return fmt.Sprintf("map[interface{}]interface{}{%s}", strings.Join(pairs, ", "))
	case *parser.TypeAssertExpr:
		obj := g.emitExpr(ex.Object)
		goType := g.emitSimpleType(ex.TypeName)
		if ex.IsCheck {
			// x is Type  →  func() bool { _, ok := x.(Type); return ok }()
			return fmt.Sprintf("func() bool { _, ok := %s.(%s); return ok }()", obj, goType)
		}
		// x as Type  →  x.(Type)
		return fmt.Sprintf("%s.(%s)", obj, goType)
	case *parser.SizeExpr:
		return fmt.Sprintf("len(%s)", g.emitExpr(ex.Object))
	case *parser.CloneExpr:
		obj := g.emitExpr(ex.Object)
		return fmt.Sprintf("append(%s[:0:0], %s...)", obj, obj)
	case *parser.StringUpperExpr:
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.ToUpper(%s)", g.emitExpr(ex.Object))
	case *parser.StringLowerExpr:
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.ToLower(%s)", g.emitExpr(ex.Object))
	case *parser.StringContainsExpr:
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.Contains(%s, %s)", g.emitExpr(ex.Object), g.emitExpr(ex.Search))
	case *parser.StringStartsWithExpr:
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.HasPrefix(%s, %s)", g.emitExpr(ex.Object), g.emitExpr(ex.Prefix))
	case *parser.StringEndsWithExpr:
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.HasSuffix(%s, %s)", g.emitExpr(ex.Object), g.emitExpr(ex.Suffix))
	case *parser.StringTrimExpr:
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.TrimSpace(%s)", g.emitExpr(ex.Object))
	case *parser.StringSplitExpr:
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.Split(%s, %s)", g.emitExpr(ex.Object), g.emitExpr(ex.Sep))
	case *parser.StringReplaceExpr:
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.ReplaceAll(%s, %s, %s)", g.emitExpr(ex.Object), g.emitExpr(ex.Old), g.emitExpr(ex.New))
	case *parser.ListJoinExpr:
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.Join(%s, %s)", g.emitExpr(ex.Object), g.emitExpr(ex.Sep))
	case *parser.MapKeysExpr:
		obj := g.emitExpr(ex.Object)
		return fmt.Sprintf("func() []interface{} { _keys := make([]interface{}, 0, len(%s)); for _k := range %s { _keys = append(_keys, _k) }; return _keys }()", obj, obj)
	case *parser.MapValuesExpr:
		obj := g.emitExpr(ex.Object)
		return fmt.Sprintf("func() []interface{} { _vals := make([]interface{}, 0, len(%s)); for _, _v := range %s { _vals = append(_vals, _v) }; return _vals }()", obj, obj)
	case *parser.MapContainsExpr:
		obj := g.emitExpr(ex.Object)
		key := g.emitExpr(ex.Key)
		return fmt.Sprintf("func() bool { _, _ok := %s[%s]; return _ok }()", obj, key)
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

func (g *Generator) emitLambda(e *parser.LambdaExpr) string {
	var paramParts []string
	for _, p := range e.Params {
		paramParts = append(paramParts, p.Name+" "+g.emitType(p.Type))
	}
	paramStr := strings.Join(paramParts, ", ")

	retStr := ""
	if e.ReturnType != nil {
		retStr = " " + g.emitType(e.ReturnType)
	}

	if e.Expr != nil {
		// Single-expression form
		if retStr == "" {
			retStr = " interface{}"
		}
		return fmt.Sprintf("func(%s)%s { return %s }", paramStr, retStr, g.emitExpr(e.Expr))
	}

	// Block-body form: detect CanThrow
	lambdaCanThrow := g.bodyIsFailable(e.Body)
	if lambdaCanThrow {
		g.neededImports["fmt"] = true
		if retStr == "" {
			retStr = " error"
		} else {
			retStr = " (" + strings.TrimPrefix(retStr, " ") + ", error)"
		}
	}

	sub := &Generator{
		neededImports:     g.neededImports, // shared — imports flow back to parent
		classNames:        g.classNames,
		interfaceNames:    g.interfaceNames,
		enumNames:         g.enumNames,
		canThrowFns:       g.canThrowFns,
		classCtors:        g.classCtors,
		receiver:          g.receiver,
		throwingVars:      g.throwingVars, // share so nested calls resolve
		fnParams:          g.fnParams,
		methodParams:      g.methodParams,
		currentReturnType: e.ReturnType,
		currentCanThrow:   lambdaCanThrow, // was hardcoded false
		indent:            1,
	}
	for _, stmt := range e.Body.Stmts {
		sub.emitStmt(stmt)
	}
	bodyStr := strings.TrimRight(sub.buf.String(), "\n")
	outerIndent := strings.Repeat("\t", g.indent)
	return fmt.Sprintf("func(%s)%s {\n%s\n%s}", paramStr, retStr, bodyStr, outerIndent)
}

// resolveArgs merges positional args, named args, and defaults for a call.
// If params is nil (unknown callee), positional args are emitted followed by
// named arg values in source order (no reordering possible).
func (g *Generator) resolveArgs(params []*parser.ParamDecl, call *parser.CallExpr) []string {
	if len(params) == 0 {
		// No param info: emit positional then named (in order)
		var out []string
		for _, a := range call.Args {
			out = append(out, g.emitExpr(a))
		}
		for _, na := range call.NamedArgs {
			out = append(out, g.emitExpr(na.Value))
		}
		return out
	}

	result := make([]string, len(params))
	// 1. Fill positional args
	for i, arg := range call.Args {
		if i < len(result) {
			result[i] = g.emitExpr(arg)
		}
	}
	// 2. Fill named args (may reorder)
	for _, na := range call.NamedArgs {
		for i, p := range params {
			if p.Name == na.Name {
				result[i] = g.emitExpr(na.Value)
				break
			}
		}
	}
	// 3. Fill remaining slots with defaults
	for i, p := range params {
		if result[i] == "" && p.Default != nil {
			result[i] = g.emitExpr(p.Default)
		}
	}
	return result
}

func (g *Generator) emitCallExpr(call *parser.CallExpr) string {
	switch callee := call.Callee.(type) {
	case *parser.SelectorExpr:
		// Could be ClassName.new(...) → NewClassName(...)
		if ident, ok := callee.Object.(*parser.Ident); ok {
			if g.classNames[ident.Name] && callee.Field == "new" {
				var ctorParams []*parser.ParamDecl
				if ctor := g.classCtors[ident.Name]; ctor != nil {
					ctorParams = ctor.Params
				}
				resolved := g.resolveArgs(ctorParams, call)
				return fmt.Sprintf("New%s(%s)", ident.Name, strings.Join(resolved, ", "))
			}
		}
		// GoType.new() → GoType{} (for types not known as Zinc classes)
		// Handles both simple: Mutex.new() and dotted: sync.Mutex.new()
		if callee.Field == "new" && len(call.Args) == 0 && len(call.NamedArgs) == 0 {
			if ident, ok := callee.Object.(*parser.Ident); ok {
				if !g.classNames[ident.Name] {
					return ident.Name + "{}"
				}
			} else if sel, ok := callee.Object.(*parser.SelectorExpr); ok {
				// pkg.Type.new() → pkg.Type{}
				typeName := g.emitExpr(sel)
				return typeName + "{}"
			}
		}
		// Could be send/receive (already handled via SendExpr/ReceiveExpr in parser)
		obj := g.emitExpr(callee.Object)
		method := capitalize(callee.Field)
		resolved := g.resolveArgs(nil, call)
		return fmt.Sprintf("%s.%s(%s)", obj, method, strings.Join(resolved, ", "))
	case *parser.Ident:
		params := g.fnParams[callee.Name]
		resolved := g.resolveArgs(params, call)
		return g.emitBuiltinCall(callee.Name, strings.Join(resolved, ", "), call.Args, call.TypeArgs)
	default:
		resolved := g.resolveArgs(nil, call)
		return fmt.Sprintf("%s(%s)", g.emitExpr(callee), strings.Join(resolved, ", "))
	}
}

// emitBuiltinCall maps Zinc built-in function names to Go equivalents.
func (g *Generator) emitBuiltinCall(name, argStr string, args []parser.Expr, typeArgs []string) string {
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

	// Panic / exit
	case "panic":
		return fmt.Sprintf("panic(%s)", argStr)
	case "exit":
		g.neededImports["os"] = true
		return fmt.Sprintf("os.Exit(%s)", argStr)

	// File I/O (failable — but in expression position, must panic to produce single value)
	case "readFile":
		g.neededImports["os"] = true
		return fmt.Sprintf("func() string { b, err := os.ReadFile(%s); if err != nil { panic(err) }; return string(b) }()", argStr)
	case "writeFile":
		g.neededImports["os"] = true
		if len(args) == 2 {
			return fmt.Sprintf("func() { if err := os.WriteFile(%s, []byte(%s), 0644); err != nil { panic(err) } }()", g.emitExpr(args[0]), g.emitExpr(args[1]))
		}
		return fmt.Sprintf("os.WriteFile(%s)", argStr)

	// JSON
	case "jsonEncode":
		g.neededImports["encoding/json"] = true
		return fmt.Sprintf("func() string { b, _ := json.Marshal(%s); return string(b) }()", argStr)
	case "jsonDecode":
		g.neededImports["encoding/json"] = true
		if len(typeArgs) > 0 {
			goType := g.emitSimpleType(typeArgs[0])
			if g.classNames[typeArgs[0]] {
				// Class types are already pointers — allocate with new() and unmarshal directly
				structType := typeArgs[0] // raw struct name without *
				return fmt.Sprintf("func() %s { _target := &%s{}; json.Unmarshal([]byte(%s), _target); return _target }()", goType, structType, argStr)
			}
			return fmt.Sprintf("func() %s { var _target %s; json.Unmarshal([]byte(%s), &_target); return _target }()", goType, goType, argStr)
		}
		return fmt.Sprintf("func() map[string]interface{} { var m map[string]interface{}; json.Unmarshal([]byte(%s), &m); return m }()", argStr)

	// HTTP
	case "httpGet":
		g.neededImports["net/http"] = true
		g.neededImports["io"] = true
		return fmt.Sprintf("func() string { resp, err := http.Get(%s); if err != nil { panic(err) }; defer resp.Body.Close(); b, _ := io.ReadAll(resp.Body); return string(b) }()", argStr)

	// Environment
	case "getEnv":
		g.neededImports["os"] = true
		return fmt.Sprintf("os.Getenv(%s)", argStr)
	case "setEnv":
		g.neededImports["os"] = true
		return fmt.Sprintf("os.Setenv(%s)", argStr)

	// Time
	case "now":
		g.neededImports["time"] = true
		return "time.Now()"
	case "sleep":
		g.neededImports["time"] = true
		return fmt.Sprintf("time.Sleep(time.Duration(%s) * time.Millisecond)", argStr)

	// String formatting
	case "sprintf":
		g.neededImports["fmt"] = true
		return fmt.Sprintf("fmt.Sprintf(%s)", argStr)

	// Type checking
	case "typeOf":
		g.neededImports["fmt"] = true
		return fmt.Sprintf("fmt.Sprintf(\"%%T\", %s)", argStr)
	}
	// Default: pass through as-is
	return fmt.Sprintf("%s(%s)", name, argStr)
}

// --- Helpers -----------------------------------------------------------------

// typeParamConstraint returns the Go constraint for a generic type parameter.
// If the parameter is used as a map key in any of the function's params, it
// must be "comparable"; otherwise "any".
func typeParamConstraint(paramName string, fnParams []*parser.ParamDecl) string {
	for _, p := range fnParams {
		if typeExprUsesAsMapKey(paramName, p.Type) {
			return "comparable"
		}
	}
	return "any"
}

// typeExprUsesAsMapKey returns true if typeName appears in a map key position
// within the given type expression.
func typeExprUsesAsMapKey(typeName string, t parser.TypeExpr) bool {
	if t == nil {
		return false
	}
	gt, ok := t.(*parser.GenericType)
	if !ok {
		return false
	}
	if gt.Name == "Map" && len(gt.TypeArgs) >= 1 {
		// First type arg is the key
		if st, ok := gt.TypeArgs[0].(*parser.SimpleType); ok && st.Name == typeName {
			return true
		}
	}
	// Recurse into all type args
	for _, arg := range gt.TypeArgs {
		if typeExprUsesAsMapKey(typeName, arg) {
			return true
		}
	}
	return false
}

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
