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

// voidFailableBuiltins is the subset of failableBuiltins whose generated Go
// code returns only error (no value). Used by isVoidFailable to decide whether
// to emit `if err := call; ...` vs `if _, err := call; ...`.
var voidFailableBuiltins = map[string]bool{
	"writeFile": true,
}

// varTypeInfo records the Go type of a local variable for method failable detection.
type varTypeInfo struct {
	PkgPath  string // e.g. "os"
	TypeName string // e.g. "File"
	Pointer  bool   // true for *os.File
}

// classFieldInfo records a class field for getter/setter generation.
type classFieldInfo struct {
	Name  string         // Zinc field name (e.g. "name")
	IsPub bool           // whether the field is pub
	Type  parser.TypeExpr // Zinc type expression
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
	// voidCanThrowFns: subset of canThrowFns where the function/method has no return type (void)
	voidCanThrowFns map[string]bool
	// varTypes: local variable name → Go type info (for method failable detection)
	varTypes map[string]varTypeInfo
	// classVars: local variable name → class name for Zinc class instances
	classVars map[string]string
	// classFields: class name → list of field info (for getter/setter generation)
	classFields map[string][]*classFieldInfo
	// classParents: class name → parent class/interface names
	classParents map[string][]string
	// current receiver name for method emission
	receiver string
	// inCtorBody: true when emitting constructor body (use direct field access)
	inCtorBody bool
	// currentClassName: name of the class being emitted (set during ctor/method emission)
	currentClassName string
	// interfaceVars: variable name → class name for interface-typed class values (function params, etc.)
	// These need getter/setter access instead of direct field access.
	interfaceVars map[string]string
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
	// goResolver: auto-detects Go function signatures via go/types
	goResolver *GoTypeResolver
	// importMap: identifier prefix → full import path (e.g. "sql" → "database/sql")
	importMap map[string]string
}

// New creates a Generator for single-file mode (package = auto-detected).
func New() *Generator {
	return &Generator{
		neededImports:  make(map[string]bool),
		classNames:     make(map[string]bool),
		interfaceNames: make(map[string]bool),
		canThrowFns:     make(map[string]bool),
		voidCanThrowFns: make(map[string]bool),
		varTypes:        make(map[string]varTypeInfo),
		classVars:       make(map[string]string),
		classFields:     make(map[string][]*classFieldInfo),
		classParents:    make(map[string][]string),
		classCtors:      make(map[string]*parser.CtorDecl),
		enumNames:       make(map[string]bool),
		throwingVars:    make(map[string]bool),
		interfaceVars:   make(map[string]string),
		fnParams:        make(map[string][]*parser.ParamDecl),
		methodParams:    make(map[string]map[string][]*parser.ParamDecl),
		goResolver:      NewGoTypeResolver(),
		importMap:       make(map[string]string),
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
		canThrowFns:     make(map[string]bool),
		voidCanThrowFns: make(map[string]bool),
		varTypes:        make(map[string]varTypeInfo),
		classVars:       make(map[string]string),
		classFields:     make(map[string][]*classFieldInfo),
		classParents:    make(map[string][]string),
		classCtors:      make(map[string]*parser.CtorDecl),
		enumNames:       make(map[string]bool),
		throwingVars:    make(map[string]bool),
		interfaceVars:   make(map[string]string),
		packageName:     pkgName,
		fnParams:        make(map[string][]*parser.ParamDecl),
		methodParams:    make(map[string]map[string][]*parser.ParamDecl),
		goResolver:      NewGoTypeResolver(),
		importMap:       make(map[string]string),
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
	for k, v := range reg.VoidCanThrowFns {
		g.voidCanThrowFns[k] = v
	}
	for k, v := range reg.ClassFields {
		g.classFields[k] = v
	}
	for k, v := range reg.ClassParents {
		g.classParents[k] = v
	}
	for k, v := range reg.ClassCtors {
		g.classCtors[k] = v
	}
	for k, v := range reg.FnParams {
		g.fnParams[k] = v
	}
	for k, v := range reg.MethodParams {
		g.methodParams[k] = make(map[string][]*parser.ParamDecl)
		for mk, mv := range v {
			g.methodParams[k][mk] = mv
		}
	}
	return g
}

// Generate converts the program AST to a Go source string.
func (g *Generator) Generate(prog *parser.Program) string {
	// First pass: collect names and mark canThrow
	g.firstPass(prog)

	// Collect user imports and build import map for Go type resolution
	g.userImports = prog.Imports
	g.buildImportMap()

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
			// Collect field info for getter/setter and auto-interface generation
			var fields []*classFieldInfo
			for _, f := range d.Fields {
				fields = append(fields, &classFieldInfo{Name: f.Name, IsPub: f.IsPub, Type: f.Type})
			}
			g.classFields[d.Name] = fields
			g.classParents[d.Name] = d.Parents
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
					if d.ReturnType == nil {
						g.voidCanThrowFns[d.Name] = true
					}
					changed = true
				}
			case *parser.ClassDecl:
				for _, m := range d.Methods {
					if !m.CanThrow && g.bodyIsFailable(m.Body) {
						m.CanThrow = true
						g.canThrowFns[d.Name+"."+m.Name] = true
						if m.ReturnType == nil {
							g.voidCanThrowFns[d.Name+"."+m.Name] = true
						}
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
	case *parser.TupleVarStmt:
		return g.exprIsFailable(st.Value)
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
	case *parser.MatchStmt:
		for _, c := range st.Cases {
			if g.bodyIsFailable(c.Body) {
				return true
			}
		}
	case *parser.WithStmt:
		for _, r := range st.Resources {
			if g.exprIsFailable(r.Value) {
				return true
			}
		}
		return g.bodyIsFailable(st.Body)
	case *parser.DeferStmt:
		return g.exprIsFailable(st.Expr)
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
			if g.canThrowFns[key] || g.goReturnsError(ident.Name, callee.Field) {
				return true
			}
			// Check method on tracked variable type (e.g. f.Write where f is *os.File)
			if info, ok := g.varTypes[ident.Name]; ok {
				return g.goResolver != nil && g.goResolver.MethodReturnsError(info.PkgPath, info.TypeName, callee.Field, info.Pointer)
			}
			// Check method on Zinc class instance (e.g. v.validate where v is AgeValidator)
			if className, ok := g.classVars[ident.Name]; ok {
				return g.canThrowFns[className+"."+callee.Field]
			}
			if className, ok := g.interfaceVars[ident.Name]; ok {
				return g.canThrowFns[className+"."+callee.Field]
			}
		}
		if _, ok := callee.Object.(*parser.ThisExpr); ok {
			return g.canThrowFns[callee.Field]
		}
	}
	return false
}

// buildImportMap populates g.importMap from user imports.
// Maps identifier prefix (alias or last path segment) to full import path.
func (g *Generator) buildImportMap() {
	for _, imp := range g.userImports {
		var prefix string
		if imp.Alias != "" {
			prefix = imp.Alias
		} else {
			prefix = imp.Path
			if idx := strings.LastIndex(prefix, "/"); idx >= 0 {
				prefix = prefix[idx+1:]
			}
		}
		g.importMap[prefix] = imp.Path
	}
}

// goReturnsError checks if prefix.funcName is a Go function returning error.
// Falls back to the hardcoded goMultiReturnFuncs list if resolution fails.
func (g *Generator) goReturnsError(prefix, funcName string) bool {
	if pkgPath, ok := g.importMap[prefix]; ok {
		if g.goResolver != nil && g.goResolver.ReturnsError(pkgPath, funcName) {
			return true
		}
	}
	// Fallback to hardcoded list
	key := prefix + "." + funcName
	return goMultiReturnFuncs[key]
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
		// Already a pointer — don't double-pointer
		if strings.HasPrefix(inner, "*") {
			return inner
		}
		// Class types are interfaces (already nilable) — don't add pointer
		if st, ok := typ.Inner.(*parser.SimpleType); ok && g.classNames[st.Name] {
			return inner
		}
		// Interface types are also already nilable
		if st, ok := typ.Inner.(*parser.SimpleType); ok && g.interfaceNames[st.Name] {
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
	// Class types use the auto-generated interface (enables polymorphism)
	if g.classNames[name] {
		return name
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
		g.emitLineDirective(d.Line)
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
	name := exportName(d.Name, d.IsPub)
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
			if p.Variadic {
				params = append(params, p.Name+" ..."+g.emitType(p.Type))
			} else {
				params = append(params, p.Name+" "+g.emitType(p.Type))
			}
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

	implName := cls.Name + "Impl"

	// 1. Struct definition (implementation type)
	typeParamStr := ""
	if len(cls.TypeParams) > 0 {
		constraints := make([]string, len(cls.TypeParams))
		for i, tp := range cls.TypeParams {
			constraints[i] = tp + " any"
		}
		typeParamStr = "[" + strings.Join(constraints, ", ") + "]"
	}
	g.writeln(fmt.Sprintf("type %s%s struct {", implName, typeParamStr))
	g.push()
	// Embed base class (as Impl type)
	if baseClass != "" {
		g.writeln(baseClass + "Impl")
	}
	// Own fields
	for _, f := range cls.Fields {
		fieldName := exportName(f.Name, f.IsPub)
		g.writeln(fmt.Sprintf("%s %s", fieldName, g.emitType(f.Type)))
	}
	g.pop()
	g.writeln("}")
	g.writeln("")

	// 2. Getters and setters for own fields (skip if class already has a method with that name)
	recv := strings.ToLower(cls.Name[:1])
	recvImplStr := implName
	if len(cls.TypeParams) > 0 {
		recvImplStr = implName + "[" + strings.Join(cls.TypeParams, ", ") + "]"
	}
	// Build set of existing method names to avoid getter/setter collisions
	existingMethods := make(map[string]bool)
	for _, m := range cls.Methods {
		existingMethods[capitalize(m.Name)] = true
	}
	for _, f := range cls.Fields {
		if !f.IsPub {
			continue
		}
		goFieldName := capitalize(f.Name)
		goType := g.emitType(f.Type)
		getterName := "Get" + goFieldName
		setterName := "Set" + goFieldName
		if !existingMethods[getterName] {
			g.writeln(fmt.Sprintf("func (%s *%s) %s() %s { return %s.%s }", recv, recvImplStr, getterName, goType, recv, goFieldName))
		}
		if !existingMethods[setterName] {
			g.writeln(fmt.Sprintf("func (%s *%s) %s(v %s) { %s.%s = v }", recv, recvImplStr, setterName, goType, recv, goFieldName))
		}
	}
	if len(cls.Fields) > 0 {
		g.writeln("")
	}

	// 3. Auto-interface (the public type)
	g.emitClassInterface(cls, baseClass, ifaces)

	// Verify interface implementation (compile-time check).
	// Skip for generic classes: can't instantiate without concrete type args.
	if len(cls.TypeParams) == 0 {
		g.writeln(fmt.Sprintf("var _ %s = (*%s)(nil)", cls.Name, implName))
		for _, iface := range ifaces {
			g.writeln(fmt.Sprintf("var _ %s = (*%s)(nil)", iface, implName))
		}
		g.writeln("")
	}

	// 4. Constructor
	classInstStr := implName
	if len(cls.TypeParams) > 0 {
		typeArgs := make([]string, len(cls.TypeParams))
		for i, tp := range cls.TypeParams {
			typeArgs[i] = tp
		}
		classInstStr = implName + "[" + strings.Join(typeArgs, ", ") + "]"
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

	// 5. Methods
	g.receiver = recv
	for _, m := range cls.Methods {
		g.emitMethod(cls.Name, cls.TypeParams, recv, m)
	}
	g.receiver = ""
}

// emitClassInterface generates the auto-interface for a class.
// The interface includes: getters/setters for all fields (own + inherited)
// and all public non-static methods.
func (g *Generator) emitClassInterface(cls *parser.ClassDecl, baseClass string, ifaces []string) {
	typeParamStr := ""
	if len(cls.TypeParams) > 0 {
		constraints := make([]string, len(cls.TypeParams))
		for i, tp := range cls.TypeParams {
			constraints[i] = tp + " any"
		}
		typeParamStr = "[" + strings.Join(constraints, ", ") + "]"
	}

	g.writeln(fmt.Sprintf("type %s%s interface {", cls.Name, typeParamStr))
	g.push()

	// Embed parent class interface (gives us inherited getters + methods)
	if baseClass != "" {
		if len(cls.TypeParams) > 0 {
			g.writeln(baseClass + "[" + strings.Join(cls.TypeParams, ", ") + "]")
		} else {
			g.writeln(baseClass)
		}
	}
	// Embed declared interfaces
	for _, iface := range ifaces {
		g.writeln(iface)
	}

	// Own field getters and setters (skip if class has matching method)
	methodNames := make(map[string]bool)
	for _, m := range cls.Methods {
		if m.IsPub && !m.IsStatic {
			methodNames[capitalize(m.Name)] = true
		}
	}
	for _, f := range cls.Fields {
		if !f.IsPub {
			continue
		}
		goFieldName := capitalize(f.Name)
		goType := g.emitType(f.Type)
		if !methodNames["Get"+goFieldName] {
			g.writeln(fmt.Sprintf("Get%s() %s", goFieldName, goType))
		}
		if !methodNames["Set"+goFieldName] {
			g.writeln(fmt.Sprintf("Set%s(%s)", goFieldName, goType))
		}
	}

	// Own public non-static methods
	for _, m := range cls.Methods {
		if !m.IsPub || m.IsStatic {
			continue
		}
		name := capitalize(m.Name)
		var params []string
		for _, p := range m.Params {
			if p.Variadic {
				params = append(params, p.Name+" ..."+g.emitType(p.Type))
			} else {
				params = append(params, p.Name+" "+g.emitType(p.Type))
			}
		}
		ret := g.emitType(m.ReturnType)
		sig := name + "(" + strings.Join(params, ", ") + ")"
		if m.CanThrow {
			if ret != "" {
				sig += " (" + ret + ", error)"
			} else {
				sig += " error"
			}
		} else if ret != "" {
			sig += " " + ret
		}
		g.writeln(sig)
	}

	g.pop()
	g.writeln("}")
	g.writeln("")
}

func (g *Generator) emitCtor(cls *parser.ClassDecl, baseClass string) {
	ctor := cls.Ctor
	name := "New" + cls.Name
	implName := cls.Name + "Impl"

	// Build params string
	var params []string
	for _, p := range ctor.Params {
		if p.Variadic {
			params = append(params, p.Name+" ..."+g.emitType(p.Type))
		} else {
			params = append(params, p.Name+" "+g.emitType(p.Type))
		}
	}
	paramStr := strings.Join(params, ", ")

	// Build type parameter string and instantiated type for generic classes.
	// e.g. class Box<T> → func NewBox[T any](v T) *BoxImpl[T]
	typeParamStr := ""
	classInstStr := implName
	if len(cls.TypeParams) > 0 {
		constraints := make([]string, len(cls.TypeParams))
		typeArgs := make([]string, len(cls.TypeParams))
		for i, tp := range cls.TypeParams {
			constraints[i] = tp + " any"
			typeArgs[i] = tp
		}
		typeParamStr = "[" + strings.Join(constraints, ", ") + "]"
		classInstStr = implName + "[" + strings.Join(typeArgs, ", ") + "]"
	}

	g.writeln(fmt.Sprintf("func %s%s(%s) *%s {", name, typeParamStr, paramStr, classInstStr))
	g.push()

	// Build struct literal
	g.writeIndent()
	g.write(fmt.Sprintf("obj := &%s{\n", classInstStr))

	g.push()
	// Base class init — call the parent constructor to avoid field-name mismatches.
	if baseClass != "" {
		baseImplName := baseClass + "Impl"
		var superArgStrs []string
		for _, arg := range ctor.SuperArgs {
			superArgStrs = append(superArgStrs, g.emitExpr(arg))
		}
		if g.classCtors[baseClass] != nil {
			// Parent has a named constructor: embed via *NewParent(args...)
			g.writeln(fmt.Sprintf("%s: *New%s(%s),", baseImplName, baseClass, strings.Join(superArgStrs, ", ")))
		} else {
			// Parent has no registered constructor: zero-value embed
			g.writeln(fmt.Sprintf("%s: %s{},", baseImplName, baseImplName))
		}
	}
	// Own fields with defaults
	for _, f := range cls.Fields {
		if f.Default != nil {
			g.writeln(fmt.Sprintf("%s: %s,", exportName(f.Name, f.IsPub), g.emitExpr(f.Default)))
		}
	}
	g.pop()
	g.writeIndent()
	g.write("}\n")

	// Body statements (super call already removed)
	savedRecv := g.receiver
	savedClass := g.currentClassName
	g.receiver = "obj"
	g.inCtorBody = true
	g.currentClassName = cls.Name
	for _, s := range ctor.Body.Stmts {
		g.emitStmt(s)
	}
	g.inCtorBody = false
	g.receiver = savedRecv
	g.currentClassName = savedClass

	g.writeln("return obj")
	g.pop()
	g.writeln("}")
	g.writeln("")
}

func (g *Generator) emitMethod(className string, typeParams []string, recv string, m *parser.MethodDecl) {
	name := exportName(m.Name, m.IsPub)

	var params []string
	for _, p := range m.Params {
		if p.Variadic {
			params = append(params, p.Name+" ..."+g.emitType(p.Type))
		} else {
			params = append(params, p.Name+" "+g.emitType(p.Type))
		}
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
	// e.g. func (b *BoxImpl[T]) Get() T
	implName := className + "Impl"
	recvTypeStr := implName
	if len(typeParams) > 0 {
		recvTypeStr = implName + "[" + strings.Join(typeParams, ", ") + "]"
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
	savedIV := g.interfaceVars
	savedClass := g.currentClassName
	g.receiver = recv
	g.currentReturnType = m.ReturnType
	g.currentCanThrow = m.CanThrow
	g.currentClassName = className
	// Track params with class types as interface-typed variables (need getters)
	g.interfaceVars = make(map[string]string)
	for k, v := range savedIV {
		g.interfaceVars[k] = v
	}
	for _, p := range m.Params {
		if st, ok := p.Type.(*parser.SimpleType); ok && g.classNames[st.Name] {
			g.interfaceVars[p.Name] = st.Name
		}
		if gt, ok := p.Type.(*parser.GenericType); ok && g.classNames[gt.Name] {
			g.interfaceVars[p.Name] = gt.Name
		}
	}
	g.emitBlock(m.Body)
	// Void-failable methods need explicit return nil at end
	if m.CanThrow && m.ReturnType == nil {
		g.writeln("return nil")
	}
	g.receiver = savedRecv
	g.currentReturnType = savedRT
	g.currentClassName = savedClass
	g.currentCanThrow = savedCT
	g.interfaceVars = savedIV
	g.pop()
	g.writeln("}")
	g.writeln("")
}

func (g *Generator) emitFn(fn *parser.FnDecl) {
	name := exportName(fn.Name, fn.IsPub)

	var params []string
	for _, p := range fn.Params {
		if p.Variadic {
			params = append(params, p.Name+" ..."+g.emitType(p.Type))
		} else {
			params = append(params, p.Name+" "+g.emitType(p.Type))
		}
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
	savedIV := g.interfaceVars
	g.currentReturnType = fn.ReturnType
	g.currentCanThrow = fn.CanThrow
	if fn.Name == "main" {
		g.inMainOrGoroutine = true
	}
	// Track params with class types as interface-typed variables (need getters)
	g.interfaceVars = make(map[string]string)
	for k, v := range savedIV {
		g.interfaceVars[k] = v
	}
	for _, p := range fn.Params {
		if st, ok := p.Type.(*parser.SimpleType); ok && g.classNames[st.Name] {
			g.interfaceVars[p.Name] = st.Name
		}
		if gt, ok := p.Type.(*parser.GenericType); ok && g.classNames[gt.Name] {
			g.interfaceVars[p.Name] = gt.Name
		}
	}
	g.emitBlock(fn.Body)
	// Void-failable functions need explicit return nil at end
	if fn.CanThrow && fn.ReturnType == nil && fn.Name != "main" {
		g.writeln("return nil")
	}
	g.currentReturnType = savedRT
	g.currentCanThrow = savedCT
	g.inMainOrGoroutine = savedMG
	g.interfaceVars = savedIV
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
	case *parser.GoStmt:
		g.emitLineDirective(st.Line)
	}

	switch st := s.(type) {
	case *parser.VarStmt:
		g.emitVarStmt(st)
	case *parser.TupleVarStmt:
		g.emitTupleVarStmt(st)
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
	case *parser.DeferStmt:
		g.writeln(fmt.Sprintf("defer %s", g.emitExpr(st.Expr)))
	case *parser.WithStmt:
		g.emitWithStmt(st)
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

	// Check for collection method chains: var result = list.Where(...).Select(...)
	if v.Value != nil {
		if chain := g.unwrapChain(v.Value); chain != nil {
			g.emitCollectionChainVar(v.Name, chain)
			return
		}
	}

	// Special case: collection constructors — Chan, List, Map
	// Supports both Chan.new(1) (legacy) and Chan(1) (new syntax)
	if v.Value != nil {
		if call, ok := v.Value.(*parser.CallExpr); ok {
			ctorName := ""
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "new" {
				if ident, ok := sel.Object.(*parser.Ident); ok {
					ctorName = ident.Name
				}
			} else if ident, ok := call.Callee.(*parser.Ident); ok {
				ctorName = ident.Name
			}
			switch ctorName {
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

	// Check if value is a failable call — needs error unpacking
	if v.Value != nil {
		if call, ok := v.Value.(*parser.CallExpr); ok && g.callIsFailable(call) {
			g.emitFailableVarStmt(v, call)
			return
		}
	}

	// Track variable type for method dispatch
	if v.Value != nil {
		if call, ok := v.Value.(*parser.CallExpr); ok {
			g.recordVarTypeFromCall(v.Name, call)
			// Track class instance variables (ClassName.new() or ClassName())
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "new" {
				if ident, ok := sel.Object.(*parser.Ident); ok && g.classNames[ident.Name] {
					g.classVars[v.Name] = ident.Name
				}
			}
			if ident, ok := call.Callee.(*parser.Ident); ok && g.classNames[ident.Name] {
				g.classVars[v.Name] = ident.Name
			}
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
	// Track variable type for method failable detection
	g.recordVarTypeFromCall(v.Name, call)
}

// emitTupleVarStmt emits a tuple destructuring statement.
// If the call is failable (returns error as last value), the error is
// auto-captured and propagated — Zinc code never sees the error value.
// (a, b) := goFunc()  →  a, b, _err1 := goFunc(); if _err1 != nil { return ... }
func (g *Generator) emitTupleVarStmt(t *parser.TupleVarStmt) {
	if call, ok := t.Value.(*parser.CallExpr); ok && g.callIsFailable(call) {
		errVar := g.nextErr()
		callStr := g.emitFailableCallExpr(call)
		g.writeln(fmt.Sprintf("%s, %s := %s", strings.Join(t.Names, ", "), errVar, callStr))
		g.emitErrorCheck(errVar, t.OrHandler)
		return
	}
	g.writeln(fmt.Sprintf("%s := %s", strings.Join(t.Names, ", "), g.emitExpr(t.Value)))
}

// recordVarTypeFromCall extracts Go type info from a call expression and
// records it in varTypes for the given variable name.
func (g *Generator) recordVarTypeFromCall(varName string, call *parser.CallExpr) {
	if g.goResolver == nil {
		return
	}
	sel, ok := call.Callee.(*parser.SelectorExpr)
	if !ok {
		return
	}
	// Case 1: pkg.Func() — package-level function (e.g. os.Open)
	if ident, ok := sel.Object.(*parser.Ident); ok && sel.Field != "new" {
		if pkgPath, ok := g.importMap[ident.Name]; ok {
			if retPkg, retType, ptr, ok := g.goResolver.FuncReturnType(pkgPath, sel.Field); ok {
				g.varTypes[varName] = varTypeInfo{PkgPath: retPkg, TypeName: retType, Pointer: ptr}
			}
		}
		return
	}
	// Case 2: pkg.Type.new() — Go type constructor (e.g. sync.Mutex.new())
	if sel.Field == "new" {
		if innerSel, ok := sel.Object.(*parser.SelectorExpr); ok {
			if ident, ok := innerSel.Object.(*parser.Ident); ok {
				if pkgPath, ok := g.importMap[ident.Name]; ok {
					g.varTypes[varName] = varTypeInfo{PkgPath: pkgPath, TypeName: innerSel.Field}
				}
			}
		}
	}
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
	// Resolve empty list/map literal type from class field type (for generics)
	g.resolveEmptyLiteralFromField(a.Target, a.Value)
	// Check if value is a failable call — needs error unpacking
	if call, ok := a.Value.(*parser.CallExpr); ok && g.callIsFailable(call) {
		errVar := g.nextErr()
		g.writeln("{")
		g.push()
		g.writeln(fmt.Sprintf("_val, %s := %s", errVar, g.emitFailableCallExpr(call)))
		g.emitErrorCheck(errVar, a.OrHandler)
		target := g.emitClassFieldAssignTarget(a.Target)
		if target != "" {
			g.writeln(fmt.Sprintf("%s(_val)", target))
		} else {
			g.writeln(fmt.Sprintf("%s %s _val", g.emitExpr(a.Target), a.Op))
		}
		g.pop()
		g.writeln("}")
		return
	}
	// Check if target is a class field — use setter
	if sel, ok := a.Target.(*parser.SelectorExpr); ok && a.Op == "=" && g.isClassFieldAccess(sel) {
		g.writeln(fmt.Sprintf("%s.Set%s(%s)", g.emitExpr(sel.Object), capitalize(sel.Field), g.emitExpr(a.Value)))
		return
	}
	g.writeln(fmt.Sprintf("%s %s %s", g.emitExpr(a.Target), a.Op, g.emitExpr(a.Value)))
}

// resolveEmptyLiteralFromField resolves the type of an empty list/map literal
// from the class field type. This handles cases like `this.items = []` in a
// generic class where the field is `List<T>` — emits `[]T{}` instead of `[]interface{}{}`.
func (g *Generator) resolveEmptyLiteralFromField(target parser.Expr, value parser.Expr) {
	sel, ok := target.(*parser.SelectorExpr)
	if !ok || g.currentClassName == "" {
		return
	}
	// Only handle this.field assignments
	if _, isThis := sel.Object.(*parser.ThisExpr); !isThis {
		return
	}
	fieldName := sel.Field
	// Look up field type from class fields
	for _, f := range g.classFields[g.currentClassName] {
		if f.Name != fieldName {
			continue
		}
		if gt, ok := f.Type.(*parser.GenericType); ok {
			if ll, ok := value.(*parser.ListLit); ok && ll.ResolvedType == "" && gt.Name == "List" && len(gt.TypeArgs) == 1 {
				ll.ResolvedType = "[]" + g.emitType(gt.TypeArgs[0])
			}
			if ml, ok := value.(*parser.MapLit); ok && ml.ResolvedType == "" && gt.Name == "Map" && len(gt.TypeArgs) == 2 {
				ml.ResolvedType = "map[" + g.emitType(gt.TypeArgs[0]) + "]" + g.emitType(gt.TypeArgs[1])
			}
		}
		break
	}
}

// emitClassFieldAssignTarget checks if the target is a class field and returns
// the setter call prefix (e.g. "obj.SetName") or "" if not a class field.
func (g *Generator) emitClassFieldAssignTarget(target parser.Expr) string {
	if sel, ok := target.(*parser.SelectorExpr); ok && g.isClassFieldAccess(sel) {
		return fmt.Sprintf("%s.Set%s", g.emitExpr(sel.Object), capitalize(sel.Field))
	}
	return ""
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
		// Check for builtin statement methods (add, remove, sort, send)
		switch sn.Field {
		case "add":
			if len(sn.Call.Args) > 0 {
				if len(sn.Call.Args) == 1 {
					if sp, ok := sn.Call.Args[0].(*parser.SpreadExpr); ok {
						val := g.emitExpr(sp.Expr)
						g.writeln(fmt.Sprintf("if %s != nil { %s = append(%s, %s...) }", obj, obj, obj, val))
						return
					}
				}
				var vals []string
				for _, a := range sn.Call.Args {
					vals = append(vals, g.emitExpr(a))
				}
				g.writeln(fmt.Sprintf("if %s != nil { %s = append(%s, %s) }", obj, obj, obj, strings.Join(vals, ", ")))
				return
			}
		case "remove":
			if len(sn.Call.Args) > 0 {
				key := g.emitExpr(sn.Call.Args[0])
				g.writeln(fmt.Sprintf("if %s != nil { delete(%s, %s) }", obj, obj, key))
				return
			}
		case "sort":
			g.neededImports["sort"] = true
			g.writeln(fmt.Sprintf("if %s != nil { sort.Slice(%s, func(i, j int) bool { return %s[i] < %s[j] }) }", obj, obj, obj, obj))
			return
		case "send":
			if len(sn.Call.Args) > 0 {
				val := g.emitExpr(sn.Call.Args[0])
				g.writeln(fmt.Sprintf("if %s != nil { %s <- %s }", obj, obj, val))
				return
			}
		}
		// Check for builtin expression methods
		if code := g.emitBuiltinMethodOnObj(obj, sn.Field, sn.Call); code != "" {
			g.writeln(fmt.Sprintf("if %s != nil { _ = %s }", obj, code))
			return
		}
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

		// For class fields, use getter (safe-nav targets may be interface-typed)
		fieldAccess := fmt.Sprintf("%s.%s", prevVar, field)
		if g.hasAnyClassField(step.Field) {
			fieldAccess = fmt.Sprintf("%s.Get%s()", prevVar, field)
		}

		if isLast && step.Call != nil {
			var argStrs []string
			for _, a := range step.Call.Args {
				argStrs = append(argStrs, g.emitExpr(a))
			}
			body.WriteString(fmt.Sprintf("return %s.%s(%s)", prevVar, field, strings.Join(argStrs, ", ")))
		} else if isLast {
			body.WriteString(fmt.Sprintf("return %s", fieldAccess))
		} else {
			nextVar := fmt.Sprintf("_s%d", i+1)
			body.WriteString(fmt.Sprintf("%s := %s; if %s == nil { return nil }; ", nextVar, fieldAccess, nextVar))
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
		// Check if this is a builtin method call
		if code := g.emitBuiltinMethodOnObj(obj, sn.Field, sn.Call); code != "" {
			return fmt.Sprintf("func() interface{} { if %s != nil { return %s }; return nil }()", obj, code)
		}
		var argStrs []string
		for _, a := range sn.Call.Args {
			argStrs = append(argStrs, g.emitExpr(a))
		}
		args := strings.Join(argStrs, ", ")
		return fmt.Sprintf("func() interface{} { if %s != nil { return %s.%s(%s) }; return nil }()", obj, obj, field, args)
	}
	// If field is a class field, use getter (safe-nav targets may be interface-typed)
	if g.hasAnyClassField(sn.Field) {
		getter := "Get" + capitalize(sn.Field)
		return fmt.Sprintf("func() interface{} { if %s != nil { return %s.%s() }; return nil }()", obj, obj, getter)
	}
	return fmt.Sprintf("func() interface{} { if %s != nil { return %s.%s }; return nil }()", obj, obj, field)
}

// emitBuiltinMethodOnObj generates Go code for a builtin method call on an already-emitted
// object expression. Returns empty string if not a builtin.
func (g *Generator) emitBuiltinMethodOnObj(objCode, methodName string, call *parser.CallExpr) string {
	switch methodName {
	case "size":
		return fmt.Sprintf("len(%s)", objCode)
	case "clone":
		return fmt.Sprintf("append(%s[:0:0], %s...)", objCode, objCode)
	case "upper":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.ToUpper(%s)", objCode)
	case "lower":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.ToLower(%s)", objCode)
	case "contains":
		if len(call.Args) > 0 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.Contains(%s, %s)", objCode, g.emitExpr(call.Args[0]))
		}
	case "startsWith":
		if len(call.Args) > 0 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.HasPrefix(%s, %s)", objCode, g.emitExpr(call.Args[0]))
		}
	case "endsWith":
		if len(call.Args) > 0 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.HasSuffix(%s, %s)", objCode, g.emitExpr(call.Args[0]))
		}
	case "trim":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.TrimSpace(%s)", objCode)
	case "split":
		if len(call.Args) > 0 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.Split(%s, %s)", objCode, g.emitExpr(call.Args[0]))
		}
	case "replace":
		if len(call.Args) >= 2 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.ReplaceAll(%s, %s, %s)", objCode, g.emitExpr(call.Args[0]), g.emitExpr(call.Args[1]))
		}
	case "join":
		if len(call.Args) > 0 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.Join(%s, %s)", objCode, g.emitExpr(call.Args[0]))
		}
	case "keys":
		return fmt.Sprintf("func() []interface{} { _keys := make([]interface{}, 0, len(%s)); for _k := range %s { _keys = append(_keys, _k) }; return _keys }()", objCode, objCode)
	case "values":
		return fmt.Sprintf("func() []interface{} { _vals := make([]interface{}, 0, len(%s)); for _, _v := range %s { _vals = append(_vals, _v) }; return _vals }()", objCode, objCode)
	case "containsKey":
		if len(call.Args) > 0 {
			return fmt.Sprintf("func() bool { _, _ok := %s[%s]; return _ok }()", objCode, g.emitExpr(call.Args[0]))
		}
	case "receive":
		return fmt.Sprintf("<-%s", objCode)
	}
	return ""
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
				// Track variable type for method failable detection
				g.recordVarTypeFromCall(r.Name, call)
			}
			g.writeln(fmt.Sprintf("%s, %s := %s", r.Name, errVar, callStr))
			g.emitErrorCheck(errVar, r.OrHandler)
		} else {
			g.writeln(fmt.Sprintf("%s := %s", r.Name, g.emitExpr(r.Value)))
			// Track type from non-failable GoType.new() calls
			if call, ok := r.Value.(*parser.CallExpr); ok {
				g.recordVarTypeFromCall(r.Name, call)
			}
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
	// Collection method chains in statement context (ForEach, etc.)
	if chain := g.unwrapChain(e.Expr); chain != nil {
		g.emitCollectionChainStmt(chain)
		return
	}
	// Builtin method calls in statement context (add, remove, sort, send)
	if call, ok := e.Expr.(*parser.CallExpr); ok {
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
			if g.emitBuiltinMethodStmt(sel, call) {
				return
			}
		}
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
		return voidFailableBuiltins[ident.Name] || g.voidCanThrowFns[ident.Name]
	}
	if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
		if ident, ok := sel.Object.(*parser.Ident); ok {
			// Check Go package-level functions that return only error (e.g. os.Remove)
			if pkgPath, ok := g.importMap[ident.Name]; ok {
				if g.goResolver != nil && g.goResolver.ReturnsOnlyError(pkgPath, sel.Field) {
					return true
				}
			}
			// Check method on tracked variable type (e.g. f.Close where f is *os.File)
			if info, ok := g.varTypes[ident.Name]; ok {
				return g.goResolver != nil && g.goResolver.MethodReturnsOnlyError(info.PkgPath, info.TypeName, sel.Field, info.Pointer)
			}
			// Check method on Zinc class instance
			if className, ok := g.classVars[ident.Name]; ok {
				return g.voidCanThrowFns[className+"."+sel.Field]
			}
			if className, ok := g.interfaceVars[ident.Name]; ok {
				return g.voidCanThrowFns[className+"."+sel.Field]
			}
		}
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
	case *parser.RawStringLit:
		return "`" + ex.Value + "`"
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
	case *parser.SpreadExpr:
		return g.emitExpr(ex.Expr) + "..."
	case *parser.LambdaExpr:
		return g.emitLambda(ex)
	case *parser.SelectorExpr:
		if ident, ok := ex.Object.(*parser.Ident); ok {
			if g.enumNames[ident.Name] {
				return ident.Name + capitalize(ex.Field)
			}
		}
		// Check if this is a class field access — use getter
		if g.isClassFieldAccess(ex) {
			return fmt.Sprintf("%s.Get%s()", g.emitExpr(ex.Object), capitalize(ex.Field))
		}
		return fmt.Sprintf("%s.%s", g.emitExpr(ex.Object), g.resolveGoFieldName(ex))
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
			switch inner := ex.Elements[0].(type) {
			case *parser.IntLit:
				elemType = "int"
			case *parser.FloatLit:
				elemType = "float64"
			case *parser.StringLit:
				elemType = "string"
			case *parser.BoolLit:
				elemType = "bool"
			case *parser.ListLit:
				// Nested list — infer inner element type
				innerType := "interface{}"
				if len(inner.Elements) > 0 {
					switch inner.Elements[0].(type) {
					case *parser.IntLit:
						innerType = "int"
					case *parser.FloatLit:
						innerType = "float64"
					case *parser.StringLit:
						innerType = "string"
					case *parser.BoolLit:
						innerType = "bool"
					}
				}
				elemType = "[]" + innerType
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
		keyType := "interface{}"
		valType := "interface{}"
		if len(ex.Keys) > 0 {
			switch ex.Keys[0].(type) {
			case *parser.IntLit:
				keyType = "int"
			case *parser.FloatLit:
				keyType = "float64"
			case *parser.StringLit:
				keyType = "string"
			case *parser.BoolLit:
				keyType = "bool"
			}
			switch ex.Values[0].(type) {
			case *parser.IntLit:
				valType = "int"
			case *parser.FloatLit:
				valType = "float64"
			case *parser.StringLit:
				valType = "string"
			case *parser.BoolLit:
				valType = "bool"
			}
		}
		return fmt.Sprintf("map[%s]%s{%s}", keyType, valType, strings.Join(pairs, ", "))
	case *parser.TypeAssertExpr:
		obj := g.emitExpr(ex.Object)
		goType := g.emitSimpleType(ex.TypeName)
		if ex.IsCheck {
			// x is Type  →  func() bool { _, ok := x.(Type); return ok }()
			return fmt.Sprintf("func() bool { _, ok := %s.(%s); return ok }()", obj, goType)
		}
		// x as Type  →  x.(Type)
		return fmt.Sprintf("%s.(%s)", obj, goType)
	}
	return "/* unknown expr */"
}

// isBuiltinReceiver returns true if the receiver should use builtin method dispatch
// (i.e., it's NOT a known Zinc class, interface, or tracked Go type).
// isClassFieldAccess checks if a selector expression is a field access on a class instance
// where getter/setter should be used (i.e., the receiver might be an interface type).
// Returns false for:
// - Constructor body (obj is concrete struct)
// - this.field / receiver.field inside methods (receiver is concrete struct)
// - Package access, Go type access, enum access
// Returns true for:
// - classVar.field (variable from .new() call — it could be typed as interface)
func (g *Generator) isClassFieldAccess(sel *parser.SelectorExpr) bool {
	if g.inCtorBody {
		return false
	}
	fieldName := sel.Field
	// this.field inside methods — receiver is concrete *Impl, use direct access
	if _, ok := sel.Object.(*parser.ThisExpr); ok {
		return false
	}
	if ident, ok := sel.Object.(*parser.Ident); ok {
		// Skip package/enum/interface access
		if g.enumNames[ident.Name] || g.interfaceNames[ident.Name] {
			return false
		}
		if _, ok := g.importMap[ident.Name]; ok {
			return false // package access like os.Stdin
		}
		if _, ok := g.varTypes[ident.Name]; ok {
			return false // Go type variable
		}
		// Method receiver (this) — concrete struct, use direct access
		if g.receiver != "" && ident.Name == g.receiver {
			return false
		}
		// Class name itself (static access)
		if g.classNames[ident.Name] {
			return false
		}
		// Interface-typed variables (function params with class type) need getters
		if _, ok := g.interfaceVars[ident.Name]; ok {
			return g.hasAnyClassField(fieldName)
		}
		// classVars from .new() are concrete *Impl types — direct field access
	}
	return false
}

// hasClassField checks if a class (including inherited fields) has a field with the given name.
func (g *Generator) hasClassField(className, fieldName string) bool {
	for _, f := range g.classFields[className] {
		if f.Name == fieldName {
			return true
		}
	}
	// Check parent classes
	for _, parent := range g.classParents[className] {
		if g.classNames[parent] && g.hasClassField(parent, fieldName) {
			return true
		}
	}
	return false
}

// goFieldName returns the Go struct field name for a Zinc field.
// Pub fields are capitalized; private fields are uncapitalized.
func (g *Generator) goFieldName(className, fieldName string) string {
	for _, f := range g.classFields[className] {
		if f.Name == fieldName {
			return exportName(f.Name, f.IsPub)
		}
	}
	// Check parent classes
	for _, parent := range g.classParents[className] {
		if g.classNames[parent] {
			if name := g.goFieldName(parent, fieldName); name != "" {
				return name
			}
		}
	}
	// Fallback: capitalize (for Go interop / unknown types)
	return capitalize(fieldName)
}

// resolveGoFieldName resolves the Go field name for a selector expression
// by determining the class from context (this, receiver, classVars, interfaceVars).
func (g *Generator) resolveGoFieldName(sel *parser.SelectorExpr) string {
	// Determine class name from context
	className := ""
	if _, ok := sel.Object.(*parser.ThisExpr); ok {
		className = g.currentClassName
	} else if ident, ok := sel.Object.(*parser.Ident); ok {
		if g.receiver != "" && ident.Name == g.receiver {
			className = g.currentClassName
		} else if cn, ok := g.classVars[ident.Name]; ok {
			className = cn
		} else if cn, ok := g.interfaceVars[ident.Name]; ok {
			className = cn
		}
	}
	if className != "" {
		return g.goFieldName(className, sel.Field)
	}
	return capitalize(sel.Field)
}

// hasAnyClassField checks if any declared class has a field with this name.
func (g *Generator) hasAnyClassField(fieldName string) bool {
	for className := range g.classNames {
		if g.hasClassField(className, fieldName) {
			return true
		}
	}
	return false
}

func (g *Generator) isBuiltinReceiver(sel *parser.SelectorExpr) bool {
	if _, ok := sel.Object.(*parser.ThisExpr); ok {
		return false // this.method() is always a real method
	}
	if ident, ok := sel.Object.(*parser.Ident); ok {
		if g.classNames[ident.Name] || g.interfaceNames[ident.Name] {
			return false // ClassName.staticMethod() or interface reference
		}
		if _, ok := g.varTypes[ident.Name]; ok {
			return false // tracked Go type variable
		}
		if _, ok := g.classVars[ident.Name]; ok {
			return false // variable holding a Zinc class instance
		}
		if _, ok := g.interfaceVars[ident.Name]; ok {
			return false // interface-typed variable (class param)
		}
		// Also check if receiver matches the method receiver name (inside class methods)
		if g.receiver != "" && ident.Name == g.receiver {
			return false
		}
	}
	return true
}

// emitBuiltinMethodCall handles builtin method calls in expression context.
// Returns the Go code and true if it was a builtin; ("", false) otherwise.
func (g *Generator) emitBuiltinMethodCall(sel *parser.SelectorExpr, call *parser.CallExpr) (string, bool) {
	if !g.isBuiltinReceiver(sel) {
		return "", false
	}
	obj := g.emitExpr(sel.Object)
	switch sel.Field {
	case "size":
		return fmt.Sprintf("len(%s)", obj), true
	case "clone":
		return fmt.Sprintf("append(%s[:0:0], %s...)", obj, obj), true
	case "upper":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.ToUpper(%s)", obj), true
	case "lower":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.ToLower(%s)", obj), true
	case "contains":
		if len(call.Args) > 0 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.Contains(%s, %s)", obj, g.emitExpr(call.Args[0])), true
		}
	case "startsWith":
		if len(call.Args) > 0 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.HasPrefix(%s, %s)", obj, g.emitExpr(call.Args[0])), true
		}
	case "endsWith":
		if len(call.Args) > 0 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.HasSuffix(%s, %s)", obj, g.emitExpr(call.Args[0])), true
		}
	case "trim":
		g.neededImports["strings"] = true
		return fmt.Sprintf("strings.TrimSpace(%s)", obj), true
	case "split":
		if len(call.Args) > 0 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.Split(%s, %s)", obj, g.emitExpr(call.Args[0])), true
		}
	case "replace":
		if len(call.Args) >= 2 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.ReplaceAll(%s, %s, %s)", obj, g.emitExpr(call.Args[0]), g.emitExpr(call.Args[1])), true
		}
	case "join":
		if len(call.Args) > 0 {
			g.neededImports["strings"] = true
			return fmt.Sprintf("strings.Join(%s, %s)", obj, g.emitExpr(call.Args[0])), true
		}
	case "keys":
		return fmt.Sprintf("func() []interface{} { _keys := make([]interface{}, 0, len(%s)); for _k := range %s { _keys = append(_keys, _k) }; return _keys }()", obj, obj), true
	case "values":
		return fmt.Sprintf("func() []interface{} { _vals := make([]interface{}, 0, len(%s)); for _, _v := range %s { _vals = append(_vals, _v) }; return _vals }()", obj, obj), true
	case "containsKey":
		if len(call.Args) > 0 {
			key := g.emitExpr(call.Args[0])
			return fmt.Sprintf("func() bool { _, _ok := %s[%s]; return _ok }()", obj, key), true
		}
	case "receive":
		return fmt.Sprintf("<-%s", obj), true
	case "send":
		if len(call.Args) > 0 {
			return fmt.Sprintf("func() { %s <- %s }()", obj, g.emitExpr(call.Args[0])), true
		}
	}
	return "", false
}

// emitBuiltinMethodStmt handles builtin method calls in statement context.
// Returns true if it was handled as a builtin.
func (g *Generator) emitBuiltinMethodStmt(sel *parser.SelectorExpr, call *parser.CallExpr) bool {
	if !g.isBuiltinReceiver(sel) {
		return false
	}
	obj := g.emitExpr(sel.Object)
	switch sel.Field {
	case "add":
		if len(call.Args) == 1 {
			if sp, ok := call.Args[0].(*parser.SpreadExpr); ok {
				val := g.emitExpr(sp.Expr)
				g.writeln(fmt.Sprintf("%s = append(%s, %s...)", obj, obj, val))
				return true
			}
		}
		var vals []string
		for _, a := range call.Args {
			vals = append(vals, g.emitExpr(a))
		}
		g.writeln(fmt.Sprintf("%s = append(%s, %s)", obj, obj, strings.Join(vals, ", ")))
		return true
	case "remove":
		if len(call.Args) > 0 {
			key := g.emitExpr(call.Args[0])
			g.writeln(fmt.Sprintf("delete(%s, %s)", obj, key))
			return true
		}
	case "sort":
		g.neededImports["sort"] = true
		g.writeln(fmt.Sprintf("sort.Slice(%s, func(i, j int) bool { return %s[i] < %s[j] })", obj, obj, obj))
		return true
	case "send":
		if len(call.Args) > 0 {
			val := g.emitExpr(call.Args[0])
			g.writeln(fmt.Sprintf("%s <- %s", obj, val))
			return true
		}
	}
	return false
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

// inferExprType attempts to infer the Go return type of a lambda expression
// from its body expression and parameter types. Falls back to "interface{}" if
// the type cannot be determined.
func (g *Generator) inferExprType(expr parser.Expr, params []*parser.ParamDecl) string {
	// Build a map of param name → Go type for lookups
	paramTypes := make(map[string]string)
	for _, p := range params {
		if p.Type != nil {
			paramTypes[p.Name] = g.emitType(p.Type)
		}
	}
	return g.inferExprTypeWithParams(expr, paramTypes)
}

// inferExprTypeWithParams returns a Zinc-level type name (e.g. "Int", "String", "Bool")
// for the expression, or "interface{}" if unable to determine.
// paramTypes maps param names to Go-level type strings (e.g. "int", "string").
func (g *Generator) inferExprTypeWithParams(expr parser.Expr, paramTypes map[string]string) string {
	switch e := expr.(type) {
	case *parser.IntLit:
		return "Int"
	case *parser.FloatLit:
		return "Float"
	case *parser.StringLit, *parser.RawStringLit, *parser.StringInterpLit:
		return "String"
	case *parser.BoolLit:
		return "Bool"
	case *parser.Ident:
		if t, ok := paramTypes[e.Name]; ok {
			return goTypeToZincType(t)
		}
		return "interface{}"
	case *parser.BinaryExpr:
		switch e.Op {
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			return "Bool"
		case "+":
			lt := g.inferExprTypeWithParams(e.Left, paramTypes)
			if lt == "String" {
				return "String"
			}
			return lt
		case "-", "*", "/", "%":
			return g.inferExprTypeWithParams(e.Left, paramTypes)
		}
	case *parser.UnaryExpr:
		if e.Op == "!" {
			return "Bool"
		}
		return g.inferExprTypeWithParams(e.Operand, paramTypes)
	case *parser.CallExpr:
		if sel, ok := e.Callee.(*parser.SelectorExpr); ok {
			switch sel.Field {
			case "upper", "lower", "trim", "replace", "join":
				return "String"
			case "size", "len":
				return "Int"
			case "contains", "startsWith", "endsWith":
				return "Bool"
			}
		}
		return "interface{}"
	case *parser.SelectorExpr:
		return "interface{}"
	case *parser.LambdaExpr:
		// Build Go function type signature for returned lambdas
		var paramTypes []string
		for _, param := range e.Params {
			if param.Type != nil {
				paramTypes = append(paramTypes, g.emitType(param.Type))
			} else {
				paramTypes = append(paramTypes, "interface{}")
			}
		}
		retType := ""
		if e.ReturnType != nil {
			retType = " " + g.emitType(e.ReturnType)
		} else if e.Expr != nil {
			inferred := g.inferExprType(e.Expr, e.Params)
			if inferred != "" && inferred != "interface{}" {
				retType = " " + g.emitType(&parser.SimpleType{Name: inferred})
			}
		} else if e.Body != nil {
			if inferred := g.inferBlockReturnType(e.Body, e.Params); inferred != "" && inferred != "interface{}" {
				retType = " " + g.emitType(&parser.SimpleType{Name: inferred})
			}
		}
		return "func(" + strings.Join(paramTypes, ", ") + ")" + retType
	}
	return "interface{}"
}

// goTypeToZincType converts a Go type name to a Zinc type name.
func goTypeToZincType(goType string) string {
	switch goType {
	case "int":
		return "Int"
	case "float64":
		return "Float"
	case "string":
		return "String"
	case "bool":
		return "Bool"
	default:
		return goType // pass through for custom types
	}
}

// effectiveLambdaReturnType returns the explicit return type if present,
// otherwise creates a synthetic SimpleType from the inferred block return type.
func (g *Generator) effectiveLambdaReturnType(e *parser.LambdaExpr) parser.TypeExpr {
	if e.ReturnType != nil {
		return e.ReturnType
	}
	if e.Body != nil {
		if inferred := g.inferBlockReturnType(e.Body, e.Params); inferred != "" && inferred != "interface{}" {
			return &parser.SimpleType{Name: inferred}
		}
	}
	return nil
}

// isErrorReturn checks if an expression is an error-creating call (Error(...), fmt.Errorf(...))
// used to skip error-path returns when inferring lambda return types.
func (g *Generator) isErrorReturn(expr parser.Expr) bool {
	call, ok := expr.(*parser.CallExpr)
	if !ok {
		return false
	}
	if ident, ok := call.Callee.(*parser.Ident); ok {
		return ident.Name == "Error"
	}
	if sel, ok := call.Callee.(*parser.SelectorExpr); ok {
		return sel.Field == "Errorf" || sel.Field == "Error"
	}
	return false
}

// inferBlockReturnType infers the return type of a block-body lambda by looking
// at the return statements in the body. Returns "" if unable to infer.
func (g *Generator) inferBlockReturnType(body *parser.BlockStmt, params []*parser.ParamDecl) string {
	paramTypes := make(map[string]string)
	for _, p := range params {
		if p.Type != nil {
			paramTypes[p.Name] = g.emitType(p.Type)
		}
	}
	// Walk top-level and if/else return statements
	var returnTypes []string
	g.collectReturnTypes(body.Stmts, paramTypes, &returnTypes)
	if len(returnTypes) == 0 {
		return ""
	}
	// All return types should agree
	first := returnTypes[0]
	for _, t := range returnTypes[1:] {
		if t != first {
			return "interface{}" // conflicting types
		}
	}
	return first
}

func (g *Generator) collectReturnTypes(stmts []parser.Stmt, paramTypes map[string]string, types *[]string) {
	for _, s := range stmts {
		switch stmt := s.(type) {
		case *parser.ReturnStmt:
			if stmt.Value != nil {
				// Skip error-path returns (return Error(...), return fmt.Errorf(...))
				if g.isErrorReturn(stmt.Value) {
					continue
				}
				*types = append(*types, g.inferExprTypeWithParams(stmt.Value, paramTypes))
			}
		case *parser.IfStmt:
			g.collectReturnTypes(stmt.Then.Stmts, paramTypes, types)
			if stmt.ElseStmt != nil {
				if block, ok := stmt.ElseStmt.(*parser.BlockStmt); ok {
					g.collectReturnTypes(block.Stmts, paramTypes, types)
				} else if elseIf, ok := stmt.ElseStmt.(*parser.IfStmt); ok {
					g.collectReturnTypes(elseIf.Then.Stmts, paramTypes, types)
					if elseIf.ElseStmt != nil {
						if block, ok := elseIf.ElseStmt.(*parser.BlockStmt); ok {
							g.collectReturnTypes(block.Stmts, paramTypes, types)
						}
					}
				}
			}
		}
	}
}

func (g *Generator) emitLambda(e *parser.LambdaExpr) string {
	var paramParts []string
	for _, p := range e.Params {
		typStr := "interface{}"
		if p.Type != nil {
			typStr = g.emitType(p.Type)
		}
		if p.Variadic {
			paramParts = append(paramParts, p.Name+" ..."+typStr)
		} else {
			paramParts = append(paramParts, p.Name+" "+typStr)
		}
	}
	paramStr := strings.Join(paramParts, ", ")

	retStr := ""
	if e.ReturnType != nil {
		retStr = " " + g.emitType(e.ReturnType)
	}

	if e.Expr != nil {
		// Single-expression form — infer return type from expression if not explicit
		if retStr == "" {
			inferred := g.inferExprType(e.Expr, e.Params)
			retStr = " " + g.emitType(&parser.SimpleType{Name: inferred})
		}
		return fmt.Sprintf("func(%s)%s { return %s }", paramStr, retStr, g.emitExpr(e.Expr))
	}

	// Block-body form: infer return type from return statements if not explicit
	if retStr == "" {
		if inferred := g.inferBlockReturnType(e.Body, e.Params); inferred != "" {
			retStr = " " + g.emitType(&parser.SimpleType{Name: inferred})
		}
	}

	// Detect CanThrow
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
		voidCanThrowFns:   g.voidCanThrowFns,
		varTypes:          g.varTypes, // share so method failable detection works
		classVars:         g.classVars,
		interfaceVars:     g.interfaceVars,
		classCtors:        g.classCtors,
		receiver:          g.receiver,
		throwingVars:      g.throwingVars, // share so nested calls resolve
		fnParams:          g.fnParams,
		methodParams:      g.methodParams,
		goResolver:        g.goResolver,
		importMap:         g.importMap,
		currentReturnType: g.effectiveLambdaReturnType(e),
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

	// Check if the last param is variadic
	isVariadic := len(params) > 0 && params[len(params)-1].Variadic
	fixedCount := len(params)
	if isVariadic {
		fixedCount = len(params) - 1
	}

	result := make([]string, fixedCount)
	// 1. Fill fixed positional args
	for i, arg := range call.Args {
		if i < fixedCount {
			result[i] = g.emitExpr(arg)
		}
	}
	// 2. Fill named args (may reorder) — only for fixed params
	for _, na := range call.NamedArgs {
		for i, p := range params[:fixedCount] {
			if p.Name == na.Name {
				result[i] = g.emitExpr(na.Value)
				break
			}
		}
	}
	// 3. Fill remaining fixed slots with defaults
	for i := 0; i < fixedCount; i++ {
		if result[i] == "" && params[i].Default != nil {
			result[i] = g.emitExpr(params[i].Default)
		}
	}
	// 4. Append variadic args (positional args beyond fixed params)
	if isVariadic {
		for i := fixedCount; i < len(call.Args); i++ {
			result = append(result, g.emitExpr(call.Args[i]))
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
		// GoType.new(...) → GoType{Field: val, ...} (for types not known as Zinc classes)
		// Handles both simple: Mutex.new() and dotted: sync.Mutex.new()
		// Named args become struct field initializers, positional args are passed through.
		if callee.Field == "new" {
			var typeName string
			isGoType := false
			if ident, ok := callee.Object.(*parser.Ident); ok {
				if !g.classNames[ident.Name] {
					typeName = ident.Name
					isGoType = true
				}
			} else if sel, ok := callee.Object.(*parser.SelectorExpr); ok {
				typeName = g.emitExpr(sel)
				isGoType = true
			}
			if isGoType {
				return g.emitGoTypeNew(typeName, call)
			}
		}
		// Check for builtin method calls (size, upper, etc.)
		if code, ok := g.emitBuiltinMethodCall(callee, call); ok {
			return code
		}
		obj := g.emitExpr(callee.Object)
		method := capitalize(callee.Field)
		// Look up method params for default/named-arg resolution.
		// Try all classes for a matching method name.
		var methodParamList []*parser.ParamDecl
		for _, methods := range g.methodParams {
			if p, ok := methods[callee.Field]; ok {
				methodParamList = p
				break
			}
		}
		resolved := g.resolveArgs(methodParamList, call)
		return fmt.Sprintf("%s.%s(%s)", obj, method, strings.Join(resolved, ", "))
	case *parser.Ident:
		// ClassName(args) → NewClassName(args) — constructor without .new()
		if g.classNames[callee.Name] {
			var ctorParams []*parser.ParamDecl
			if ctor := g.classCtors[callee.Name]; ctor != nil {
				ctorParams = ctor.Params
			}
			resolved := g.resolveArgs(ctorParams, call)
			return fmt.Sprintf("New%s(%s)", callee.Name, strings.Join(resolved, ", "))
		}
		params := g.fnParams[callee.Name]
		resolved := g.resolveArgs(params, call)
		return g.emitBuiltinCall(callee.Name, strings.Join(resolved, ", "), call.Args, call.TypeArgs)
	default:
		resolved := g.resolveArgs(nil, call)
		return fmt.Sprintf("%s(%s)", g.emitExpr(callee), strings.Join(resolved, ", "))
	}
}

// emitBuiltinCall maps Zinc built-in function names to Go equivalents.
// emitGoTypeNew emits a Go struct literal: TypeName{Field: val, ...}.
// Positional args are emitted as-is (rare for Go structs), named args become field initializers.
func (g *Generator) emitGoTypeNew(typeName string, call *parser.CallExpr) string {
	if len(call.Args) == 0 && len(call.NamedArgs) == 0 {
		return typeName + "{}"
	}
	var fields []string
	for _, arg := range call.Args {
		fields = append(fields, g.emitExpr(arg))
	}
	for _, na := range call.NamedArgs {
		fields = append(fields, fmt.Sprintf("%s: %s", capitalize(na.Name), g.emitExpr(na.Value)))
	}
	return fmt.Sprintf("%s{%s}", typeName, strings.Join(fields, ", "))
}

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
