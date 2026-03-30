package codegen_go

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
)

// Generator produces Go source from a Zinc AST.
type Generator struct {
	buf            strings.Builder
	indent         int
	className      string // derived from filename or "main"
	imports        map[string]bool
	interfaces     map[string]bool
	structs        map[string]*parser.ClassDecl
	sourceFile     string // for //line directives
	currentFields  map[string]bool // field names of current class (for implicit self)
	currentMethods map[string]bool // method names of current class (for implicit self)
	currentParams  map[string]bool // parameter names (shadow field names)

	// Error handling
	errorFuncs            map[string]bool   // functions that can return errors
	currentReturnType     string            // return type of current function (for zero values)
	currentReturnOptional bool              // true if current function returns T? (pointer type)
	currentFuncParams     []*parser.ParamDecl // params of current function (for lambda type inference)

	// Default parameters
	funcSigs map[string][]*parser.ParamDecl // function name → param list

	// Stream operations
	chainCounter int // counter for _chain variables

	// Scope tracking
	errVarCount   int    // counter for unique _err variables in same scope
	currentErrVar string // current error variable name (for or-blocks)

	// Variable type tracking (for typed slices and stream operations)
	varTypes map[string]string // variable name → element type (e.g. "int", "string")
	ptrVars            map[string]bool // variables that are pointers (*T from T? returns)
	funcReturnsOptional map[string]bool   // functions that return T? (optional)
	funcReturnTypes     map[string]string // function name → Go return type string
	renamedVars         map[string]string // original name → safe name (for builtin shadows)
	// Variable struct type tracking (for getter rewriting)
	varStructTypes map[string]string // variable name → struct type name
	// Data class tracking (for implicit constructor calls)
	dataClasses map[string]bool          // data class names that have NewType constructors
	typeAliases map[string]parser.TypeExpr // type alias name → underlying type
	goResolver  *GoTypeResolver            // introspects Go packages at transpile time
	importMap   map[string]string           // import prefix → full Go package path
	// Smart import resolution: short type name → qualified Go name
	// e.g. "Mutex" → "sync.Mutex" from `import sync.Mutex`
	typeImports map[string]string
}

// New creates a new Go code generator.
func New() *Generator {
	return &Generator{
		imports:        make(map[string]bool),
		interfaces:     make(map[string]bool),
		structs:        make(map[string]*parser.ClassDecl),
		errorFuncs:     make(map[string]bool),
		funcSigs:       make(map[string][]*parser.ParamDecl),
		varTypes:       make(map[string]string),
		ptrVars:            make(map[string]bool),
		funcReturnsOptional: make(map[string]bool),
		funcReturnTypes:     make(map[string]string),
		renamedVars:         make(map[string]string),
		varStructTypes: make(map[string]string),
		dataClasses:    make(map[string]bool),
		typeAliases:    make(map[string]parser.TypeExpr),
		goResolver:     NewGoTypeResolver(),
		importMap:      make(map[string]string),
		typeImports:    make(map[string]string),
	}
}

// OutputFile represents a generated .go file.
type OutputFile struct {
	Name    string
	Content string
}

// SetSourceFile sets the source .zn filename for //line directives.
func (g *Generator) SetSourceFile(path string) {
	g.sourceFile = path
}

// RegisterInterface allows external callers to register interface names.
func (g *Generator) RegisterInterface(name string) {
	g.interfaces[name] = true
}

// collectDecls scans declarations to build lookup tables.
func (g *Generator) collectDecls(decls []parser.TopLevelDecl) {
	for _, d := range decls {
		switch decl := d.(type) {
		case *parser.InterfaceDecl:
			g.interfaces[decl.Name] = true
		case *parser.DataClassDecl:
			g.dataClasses[decl.Name] = true
			// Convert FieldDecl params to ParamDecl for funcSigs
			g.funcSigs["New"+decl.Name] = fieldDeclsToParams(decl.Params)
		case *parser.ClassDecl:
			g.structs[decl.Name] = decl
			// Register sealed class variants (data classes)
			if decl.IsSealed {
				for _, v := range decl.Variants {
					g.dataClasses[v.Name] = true
					g.funcSigs["New"+v.Name] = fieldDeclsToParams(v.Params)
				}
			}
			// Collect constructor signatures
			if decl.Ctor != nil {
				g.funcSigs["New"+decl.Name] = decl.Ctor.Params
			} else if len(decl.Ctors) > 0 {
				g.funcSigs["New"+decl.Name] = decl.Ctors[0].Params
			}
			// Scan methods for error returns
			for _, m := range decl.Methods {
				key := decl.Name + "." + m.Name
				if canReturnError(m.Body) {
					g.errorFuncs[key] = true
				}
			}
		case *parser.TypeAliasDecl:
			g.typeAliases[decl.Name] = decl.Type
		case *parser.FnDecl:
			g.funcSigs[decl.Name] = decl.Params
			if canReturnError(decl.Body) {
				g.errorFuncs[decl.Name] = true
			}
			if _, ok := decl.ReturnType.(*parser.OptionalType); ok {
				g.funcReturnsOptional[decl.Name] = true
			}
			if decl.ReturnType != nil {
				g.funcReturnTypes[decl.Name] = g.formatType(decl.ReturnType)
			}
		}
	}
}

// fieldDeclsToParams converts FieldDecl slice to ParamDecl slice for funcSigs compatibility.
func fieldDeclsToParams(fields []*parser.FieldDecl) []*parser.ParamDecl {
	var params []*parser.ParamDecl
	for _, f := range fields {
		params = append(params, &parser.ParamDecl{
			Name:    f.Name,
			Type:    f.Type,
			Default: f.Default,
		})
	}
	return params
}

// canReturnError walks a function body looking for return Error(...) statements.
func canReturnError(body *parser.BlockStmt) bool {
	if body == nil {
		return false
	}
	for _, s := range body.Stmts {
		if stmtCanReturnError(s) {
			return true
		}
	}
	return false
}

func stmtCanReturnError(s parser.Stmt) bool {
	switch stmt := s.(type) {
	case *parser.ReturnStmt:
		if stmt.Value == nil {
			return false
		}
		if call, ok := stmt.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "Error" {
				return true
			}
		}
		return false
	case *parser.VarStmt:
		// or { return Error(err) } means the function can return errors
		if stmt.OrHandler != nil && stmt.OrHandler.Body != nil {
			if blockCanReturnError(stmt.OrHandler.Body) {
				return true
			}
		}
		return false
	case *parser.ExprStmt:
		if stmt.OrHandler != nil && stmt.OrHandler.Body != nil {
			if blockCanReturnError(stmt.OrHandler.Body) {
				return true
			}
		}
		return false
	case *parser.AssignStmt:
		if stmt.OrHandler != nil && stmt.OrHandler.Body != nil {
			if blockCanReturnError(stmt.OrHandler.Body) {
				return true
			}
		}
		return false
	case *parser.IfStmt:
		if blockCanReturnError(stmt.Then) {
			return true
		}
		if stmt.ElseStmt != nil {
			if stmtCanReturnError(stmt.ElseStmt) {
				return true
			}
		}
		return false
	case *parser.BlockStmt:
		return blockCanReturnError(stmt)
	case *parser.ForStmt:
		return blockCanReturnError(stmt.Body)
	case *parser.WhileStmt:
		return blockCanReturnError(stmt.Body)
	case *parser.MatchStmt:
		for _, c := range stmt.Cases {
			if blockCanReturnError(c.Body) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func blockCanReturnError(block *parser.BlockStmt) bool {
	if block == nil {
		return false
	}
	for _, s := range block.Stmts {
		if stmtCanReturnError(s) {
			return true
		}
	}
	return false
}

// zeroValueFor returns the Go zero value for a given type string.
func zeroValueFor(goType string) string {
	switch goType {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"byte", "rune":
		return "0"
	case "float32", "float64":
		return "0.0"
	case "string":
		return `""`
	case "bool":
		return "false"
	case "":
		return ""
	default:
		if strings.HasPrefix(goType, "*") || strings.HasPrefix(goType, "[]") ||
			strings.HasPrefix(goType, "map[") || strings.HasPrefix(goType, "chan ") ||
			strings.HasPrefix(goType, "func(") || goType == "interface{}" || goType == "error" {
			return "nil"
		}
		// Struct types — use zero value literal
		return goType + "{}"
	}
}

// needImport records that a Go import is required.
func (g *Generator) needImport(pkg string) {
	g.imports[pkg] = true
}

// Generate produces a single .go source file from a Zinc program.
func (g *Generator) Generate(prog *parser.Program, className string) string {
	g.buf.Reset()
	g.indent = 0
	g.className = className
	g.imports = make(map[string]bool)
	g.errorFuncs = make(map[string]bool)
	g.funcSigs = make(map[string][]*parser.ParamDecl)
	g.varTypes = make(map[string]string)
	g.varStructTypes = make(map[string]string)
	g.dataClasses = make(map[string]bool)
	g.typeImports = make(map[string]string)
	g.collectDecls(prog.Decls)

	// Add user imports from Zinc source with smart resolution.
	// Heuristic: if the last segment starts with uppercase, it's a specific
	// type import (e.g. sync.Mutex → import "sync", Mutex → sync.Mutex).
	// Otherwise it's a whole-package import (e.g. net.http → import "net/http").
	for _, imp := range prog.Imports {
		parts := strings.Split(imp.Path, ".")
		lastSeg := parts[len(parts)-1]
		if len(parts) >= 2 && len(lastSeg) > 0 && lastSeg[0] >= 'A' && lastSeg[0] <= 'Z' {
			// Type import: import sync.Mutex → package "sync", register Mutex → sync.Mutex
			pkgParts := parts[:len(parts)-1]
			goPath := strings.Join(pkgParts, "/")
			// The Go qualified name uses the last package segment as prefix
			goPkg := pkgParts[len(pkgParts)-1]
			typeName := lastSeg
			g.needImport(goPath)
			g.typeImports[typeName] = goPkg + "." + typeName
		} else {
			// Package import: import net.http → import "net/http"
			goPath := strings.ReplaceAll(imp.Path, ".", "/")
			g.needImport(goPath)
			// Build import map for Go type resolution: "http" → "net/http"
			g.importMap[lastSeg] = goPath
		}
	}

	// First pass: generate body into a separate buffer to collect imports
	bodyGen := *g
	bodyGen.buf.Reset()

	for _, d := range prog.Decls {
		bodyGen.emitDecl(d)
		bodyGen.writeln("")
	}

	// Script-mode statements → func main()
	hasExplicitMain := false
	for _, d := range prog.Decls {
		if fn, ok := d.(*parser.FnDecl); ok && fn.Name == "main" {
			hasExplicitMain = true
			break
		}
	}
	if len(prog.Stmts) > 0 && !hasExplicitMain {
		bodyGen.writeln("func main() {")
		bodyGen.indent++
		for _, s := range prog.Stmts {
			bodyGen.emitStmt(s)
		}
		bodyGen.indent--
		bodyGen.writeln("}")
	}

	body := bodyGen.buf.String()
	g.imports = bodyGen.imports

	// Now write the final output with package + imports + body
	g.writeln("package main")
	g.writeln("")

	if len(g.imports) > 0 {
		g.writeln("import (")
		g.indent++
		for pkg := range g.imports {
			g.writeln("%q", pkg)
		}
		g.indent--
		g.writeln(")")
		g.writeln("")
	}

	g.buf.WriteString(body)
	return g.buf.String()
}

// GenerateFiles produces separate .go files per type + a main.go for functions/script.
func (g *Generator) GenerateFiles(prog *parser.Program, className string) []OutputFile {
	// For now, generate everything into a single file
	content := g.Generate(prog, className)
	outName := strings.ToLower(className) + ".go"
	// Avoid generating _test.go files — Go treats those as test files
	if strings.HasSuffix(outName, "_test.go") {
		outName = strings.TrimSuffix(outName, "_test.go") + "_main.go"
	}
	return []OutputFile{{Name: outName, Content: content}}
}

// --- Declarations ------------------------------------------------------------

func (g *Generator) emitDecl(d parser.TopLevelDecl) {
	switch decl := d.(type) {
	case *parser.FnDecl:
		g.emitFnDecl(decl)
	case *parser.ClassDecl:
		if decl.IsSealed {
			g.emitSealedDecl(decl)
		} else {
			g.emitClassDecl(decl)
		}
	case *parser.DataClassDecl:
		g.emitDataClassDecl(decl)
	case *parser.EnumDecl:
		g.emitEnumDecl(decl)
	case *parser.InterfaceDecl:
		g.emitInterfaceDecl(decl)
	case *parser.ConstDecl:
		g.emitConstDecl(decl)
	case *parser.TypeAliasDecl:
		g.writeln("type %s = %s", decl.Name, g.formatType(decl.Type))
	}
}

// --- Functions ---------------------------------------------------------------

func (g *Generator) emitFnDecl(fn *parser.FnDecl) {
	if g.sourceFile != "" && fn.Line > 0 {
		g.writeln("//line %s:%d", g.sourceFile, fn.Line)
	}

	name := fn.Name
	if name == "main" {
		g.writeln("func main() {")
		g.indent++
		g.emitBlock(fn.Body)
		g.indent--
		g.writeln("}")
		return
	}

	canError := g.errorFuncs[name]
	goRetType := g.goReturnTypeStr(fn.ReturnType)
	var ret string
	if canError {
		if goRetType == "" {
			ret = " error"
		} else {
			ret = fmt.Sprintf(" (%s, error)", goRetType)
		}
	} else {
		ret = g.formatReturnType(fn.ReturnType, fn.Body)
	}

	// Save/restore state for function scope
	prevRetType := g.currentReturnType
	prevRetOpt := g.currentReturnOptional
	prevErrCount := g.errVarCount
	if canError {
		g.currentReturnType = goRetType
	}
	_, isOptional := fn.ReturnType.(*parser.OptionalType)
	g.currentReturnOptional = isOptional
	if isOptional && !canError {
		g.currentReturnType = goRetType
	}
	g.errVarCount = 0
	g.currentFuncParams = fn.Params

	params := g.formatParams(fn.Params)

	g.writeln("func %s(%s)%s {", name, params, ret)
	g.indent++
	g.emitBlock(fn.Body)
	g.indent--
	g.writeln("}")

	g.currentReturnType = prevRetType
	g.currentReturnOptional = prevRetOpt
	g.errVarCount = prevErrCount
}

// --- Structs (Classes) -------------------------------------------------------

func (g *Generator) emitClassDecl(cls *parser.ClassDecl) {
	if g.sourceFile != "" && cls.Line > 0 {
		g.writeln("//line %s:%d", g.sourceFile, cls.Line)
	}

	name := cls.Name

	// Struct definition
	g.writeln("type %s struct {", name)
	g.indent++

	// Embedded parent (first non-interface parent)
	for _, p := range cls.Parents {
		if !g.interfaces[p] {
			g.writeln("%s", p)
		}
	}

	for _, f := range cls.Fields {
		if f.IsConst {
			continue // const fields → package-level consts
		}
		typeName := "interface{}"
		if f.Type != nil {
			typeName = g.formatType(f.Type)
		}
		g.writeln("%s %s", exportName(f.Name), typeName)
	}
	g.indent--
	g.writeln("}")
	g.writeln("")

	// Emit const fields as package-level constants
	for _, f := range cls.Fields {
		if f.IsConst && f.Default != nil {
			g.writeln("const %s_%s = %s", name, exportName(f.Name), g.formatExpr(f.Default))
		}
	}
	if len(cls.Fields) > 0 {
		hasConsts := false
		for _, f := range cls.Fields {
			if f.IsConst {
				hasConsts = true
			}
		}
		if hasConsts {
			g.writeln("")
		}
	}

	// Constructor → NewType() function
	if cls.Ctor != nil {
		g.emitConstructor(name, cls.Ctor, cls)
	} else if len(cls.Ctors) > 0 {
		g.emitConstructor(name, cls.Ctors[0], cls)
	} else {
		// Generate default constructor with field defaults
		g.writeln("func New%s() *%s {", name, name)
		g.indent++
		// Collect fields with defaults into a struct literal
		var litFields []string
		for _, f := range cls.Fields {
			if f.Default != nil {
				litFields = append(litFields, fmt.Sprintf("%s: %s", exportName(f.Name), g.formatExpr(f.Default)))
			}
		}
		if len(litFields) == 0 {
			g.writeln("return &%s{}", name)
		} else if len(litFields) <= 3 {
			g.writeln("return &%s{%s}", name, strings.Join(litFields, ", "))
		} else {
			g.writeln("return &%s{", name)
			g.indent++
			for _, lf := range litFields {
				g.writeln("%s,", lf)
			}
			g.indent--
			g.writeln("}")
		}
		g.indent--
		g.writeln("}")
		g.writeln("")
	}

	// Methods
	for _, m := range cls.Methods {
		g.emitMethodDecl(name, m)
		g.writeln("")
	}
}

func (g *Generator) emitConstructor(typeName string, ctor *parser.CtorDecl, cls *parser.ClassDecl) {
	// Set current fields/methods for implicit self resolution
	g.currentFields = make(map[string]bool)
	g.currentMethods = make(map[string]bool)
	g.currentParams = make(map[string]bool)
	for _, f := range cls.Fields {
		g.currentFields[f.Name] = true
	}
	for _, method := range cls.Methods {
		g.currentMethods[method.Name] = true
	}
	for _, p := range ctor.Params {
		g.currentParams[p.Name] = true
	}
	defer func() { g.currentFields = nil; g.currentMethods = nil; g.currentParams = nil }()

	params := g.formatParams(ctor.Params)
	g.writeln("func New%s(%s) *%s {", typeName, params, typeName)
	g.indent++

	// Extract field assignments from ctor body: this.field = value → Field: value
	// and separate remaining statements
	var litFields []string
	var remainingStmts []parser.Stmt

	// Handle super() → embedded parent in struct literal
	if len(ctor.SuperArgs) > 0 {
		parentType := ""
		for _, p := range cls.Parents {
			if !g.interfaces[p] {
				parentType = p
				break
			}
		}
		if parentType != "" {
			args := g.formatExprList(ctor.SuperArgs)
			litFields = append(litFields, fmt.Sprintf("%s: *New%s(%s)", parentType, parentType, args))
		}
	}

	if ctor.Body != nil {
		for _, stmt := range ctor.Body.Stmts {
			if assign, ok := stmt.(*parser.AssignStmt); ok && assign.Op == "=" {
				// this.field = value → Field: value in literal
				if sel, ok := assign.Target.(*parser.SelectorExpr); ok {
					if _, isThis := sel.Object.(*parser.ThisExpr); isThis {
						litFields = append(litFields, fmt.Sprintf("%s: %s", exportName(sel.Field), g.formatExpr(assign.Value)))
						continue
					}
					if ident, isIdent := sel.Object.(*parser.Ident); isIdent && ident.Name == "this" {
						litFields = append(litFields, fmt.Sprintf("%s: %s", exportName(sel.Field), g.formatExpr(assign.Value)))
						continue
					}
				}
			}
			// Skip super() call expression (handled above)
			if es, ok := stmt.(*parser.ExprStmt); ok {
				if _, isSuper := es.Expr.(*parser.SuperCallExpr); isSuper {
					continue
				}
			}
			remainingStmts = append(remainingStmts, stmt)
		}
	}

	// Emit struct literal
	if len(litFields) > 0 {
		if len(remainingStmts) == 0 {
			// Pure literal return — clean one-liner or multi-line
			if len(litFields) <= 3 {
				g.writeln("return &%s{%s}", typeName, strings.Join(litFields, ", "))
			} else {
				g.writeln("return &%s{", typeName)
				g.indent++
				for _, f := range litFields {
					g.writeln("%s,", f)
				}
				g.indent--
				g.writeln("}")
			}
		} else {
			// Literal + extra logic
			g.writeln("s := &%s{%s}", typeName, strings.Join(litFields, ", "))
			for _, stmt := range remainingStmts {
				g.emitStmt(stmt)
			}
			g.writeln("return s")
		}
	} else if len(remainingStmts) > 0 {
		g.writeln("s := &%s{}", typeName)
		for _, stmt := range remainingStmts {
			g.emitStmt(stmt)
		}
		g.writeln("return s")
	} else {
		g.writeln("return &%s{}", typeName)
	}

	g.indent--
	g.writeln("}")
	g.writeln("")
}

func (g *Generator) emitMethodDecl(receiver string, m *parser.MethodDecl) {
	// Set current fields/methods for implicit self resolution
	if cls, ok := g.structs[receiver]; ok {
		g.currentFields = make(map[string]bool)
		g.currentMethods = make(map[string]bool)
		g.currentParams = make(map[string]bool)
		// Own fields
		for _, f := range cls.Fields {
			g.currentFields[f.Name] = true
		}
		// Parent fields (walk inheritance chain)
		g.collectParentFields(cls, g.currentFields)
		// Own methods
		for _, method := range cls.Methods {
			g.currentMethods[method.Name] = true
		}
		// Parent methods
		g.collectParentMethods(cls, g.currentMethods)
		for _, p := range m.Params {
			g.currentParams[p.Name] = true
		}
	}
	defer func() { g.currentFields = nil; g.currentMethods = nil; g.currentParams = nil }()

	methodKey := receiver + "." + m.Name
	canError := g.errorFuncs[methodKey]
	goRetType := g.goReturnTypeStr(m.ReturnType)

	var ret string
	if canError {
		if goRetType == "" {
			ret = " error"
		} else {
			ret = fmt.Sprintf(" (%s, error)", goRetType)
		}
	} else {
		ret = g.formatReturnType(m.ReturnType, m.Body)
	}

	// Save/restore currentReturnType for error return emission
	prevRetType := g.currentReturnType
	if canError {
		g.currentReturnType = goRetType
	}

	// Map Zinc method names to Go equivalents
	goMethodName := m.Name
	switch m.Name {
	case "toString":
		goMethodName = "String"
	case "equals":
		goMethodName = "Equal"
	case "hashCode":
		goMethodName = "Hash"
	}

	if m.IsStatic {
		name := receiver + exportName(goMethodName)
		params := g.formatParams(m.Params)
		g.writeln("func %s(%s)%s {", name, params, ret)
	} else {
		// All methods exported — single package, no unexported needed
		vis := exportName(goMethodName)
		params := g.formatParams(m.Params)
		g.writeln("func (s *%s) %s(%s)%s {", receiver, vis, params, ret)
	}
	g.indent++
	g.emitBlock(m.Body)
	g.indent--
	g.writeln("}")

	g.currentReturnType = prevRetType
}

// collectParentFields walks the inheritance chain and adds parent fields to the map.
func (g *Generator) collectParentFields(cls *parser.ClassDecl, fields map[string]bool) {
	for _, p := range cls.Parents {
		if g.interfaces[p] {
			continue
		}
		if parentCls, ok := g.structs[p]; ok {
			for _, f := range parentCls.Fields {
				fields[f.Name] = true
			}
			g.collectParentFields(parentCls, fields)
		}
	}
}

// collectParentMethods walks the inheritance chain and adds parent methods to the map.
func (g *Generator) collectParentMethods(cls *parser.ClassDecl, methods map[string]bool) {
	for _, p := range cls.Parents {
		if g.interfaces[p] {
			continue
		}
		if parentCls, ok := g.structs[p]; ok {
			for _, m := range parentCls.Methods {
				methods[m.Name] = true
			}
			g.collectParentMethods(parentCls, methods)
		}
	}
}

// --- Data Classes (Structs) --------------------------------------------------

func (g *Generator) emitDataClassDecl(d *parser.DataClassDecl) {
	if g.sourceFile != "" && d.Line > 0 {
		g.writeln("//line %s:%d", g.sourceFile, d.Line)
	}

	g.writeln("type %s struct {", d.Name)
	g.indent++
	for _, f := range d.Params {
		typeName := "interface{}"
		if f.Type != nil {
			typeName = g.formatType(f.Type)
		}
		g.writeln("%s %s", exportName(f.Name), typeName)
	}
	g.indent--
	g.writeln("}")
	g.writeln("")

	// Constructor
	var params []string
	var assignments []string
	for _, f := range d.Params {
		typeName := "interface{}"
		if f.Type != nil {
			typeName = g.formatType(f.Type)
		}
		params = append(params, fmt.Sprintf("%s %s", f.Name, typeName))
		assignments = append(assignments, fmt.Sprintf("%s: %s", exportName(f.Name), f.Name))
	}
	g.writeln("func New%s(%s) %s {", d.Name, strings.Join(params, ", "), d.Name)
	g.indent++
	g.writeln("return %s{%s}", d.Name, strings.Join(assignments, ", "))
	g.indent--
	g.writeln("}")
	g.writeln("")

	// String() method for data classes
	g.needImport("fmt")
	var fmtParts []string
	var fmtArgs []string
	for _, f := range d.Params {
		fmtParts = append(fmtParts, f.Name+"=%v")
		fmtArgs = append(fmtArgs, "s."+exportName(f.Name))
	}
	g.writeln("func (s %s) String() string {", d.Name)
	g.indent++
	g.writeln("return fmt.Sprintf(\"%s(%s)\", %s)", d.Name, strings.Join(fmtParts, ", "), strings.Join(fmtArgs, ", "))
	g.indent--
	g.writeln("}")

	// Methods
	for _, m := range d.Methods {
		g.writeln("")
		g.emitMethodDecl(d.Name, m)
	}
}

// --- Sealed types (interface + variant structs) ------------------------------

func (g *Generator) emitSealedDecl(cls *parser.ClassDecl) {
	// Sealed class → interface with private marker method
	g.writeln("type %s interface {", cls.Name)
	g.indent++
	g.writeln("is%s()", cls.Name)
	g.indent--
	g.writeln("}")
	g.writeln("")

	for _, v := range cls.Variants {
		g.emitDataClassDecl(v)
		g.writeln("")
		// Implement the sealed marker
		g.writeln("func (%s) is%s() {}", v.Name, cls.Name)
		g.writeln("")
	}
}

// --- Enums -------------------------------------------------------------------

func (g *Generator) emitEnumDecl(e *parser.EnumDecl) {
	g.writeln("type %s int", e.Name)
	g.writeln("")
	g.writeln("const (")
	g.indent++
	for i, v := range e.Variants {
		if i == 0 {
			g.writeln("%s %s = iota", v, e.Name)
		} else {
			g.writeln("%s", v)
		}
	}
	g.indent--
	g.writeln(")")
}

// --- Interfaces --------------------------------------------------------------

func (g *Generator) emitInterfaceDecl(iface *parser.InterfaceDecl) {
	g.writeln("type %s interface {", iface.Name)
	g.indent++
	for _, m := range iface.Methods {
		ret := ""
		if m.ReturnType != nil {
			ret = " " + g.formatType(m.ReturnType)
		}
		params := g.formatParams(m.Params)
		g.writeln("%s(%s)%s", exportName(m.Name), params, ret)
	}
	g.indent--
	g.writeln("}")
}

// --- Constants ---------------------------------------------------------------

func (g *Generator) emitConstDecl(c *parser.ConstDecl) {
	g.writeln("const %s = %s", exportName(c.Name), g.formatExpr(c.Value))
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
		g.writeln("for %s {", g.formatExpr(stmt.Cond))
		g.indent++
		g.emitBlock(stmt.Body)
		g.indent--
		g.writeln("}")
	case *parser.MatchStmt:
		g.emitMatchStmt(stmt)
	case *parser.ExprStmt:
		g.emitExprStmt(stmt)
	case *parser.PrintStmt:
		g.needImport("fmt")
		// Unwrap: print("msg {x}") → fmt.Printf("msg %v\n", x) instead of fmt.Println(fmt.Sprintf(...))
		if interp, ok := stmt.Value.(*parser.StringInterpLit); ok {
			fmtStr, args := g.formatPrintf(interp)
			if len(args) > 0 {
				g.writeln("fmt.Printf(%q, %s)", fmtStr+"\n", strings.Join(args, ", "))
			} else {
				g.writeln("fmt.Println(%q)", fmtStr)
			}
		} else {
			g.writeln("fmt.Println(%s)", g.formatExpr(stmt.Value))
		}
	case *parser.BreakStmt:
		g.writeln("break")
	case *parser.ContinueStmt:
		g.writeln("continue")
	case *parser.BlockStmt:
		g.emitBlock(stmt)
	case *parser.FnDecl:
		g.emitFnDecl(stmt)
	case *parser.TupleVarStmt:
		g.emitTupleVarStmt(stmt)
	case *parser.GoStmt:
		g.writeln("go func() {")
		g.indent++
		g.emitBlock(stmt.Body)
		g.indent--
		g.writeln("}()")
	case *parser.ParallelForStmt:
		g.emitParallelForStmt(stmt)
	case *parser.ConcurrentStmt:
		g.emitConcurrentStmt(stmt)
	case *parser.WithStmt:
		g.emitWithStmt(stmt)
	case *parser.DeferStmt:
		g.writeln("defer %s", g.formatExpr(stmt.Expr))
	case *parser.AssertStmt:
		g.emitAssertStmt(stmt)
	case *parser.TryStmt:
		// Go doesn't have try/catch — emit as a comment for now
		g.writeln("// try/catch not directly supported in Go")
		g.emitBlock(stmt.Body)
	case *parser.RaiseStmt:
		g.writeln("panic(%s)", g.formatExpr(stmt.Value))
	}
}

// inferListLitElemType infers the Go element type from a list literal's elements.
// Returns "int", "float64", "string", "bool", or "interface{}".
func inferListLitElemType(elements []parser.Expr) string {
	if len(elements) == 0 {
		return "interface{}"
	}
	allInt := true
	allFloat := true
	allString := true
	allBool := true
	for _, e := range elements {
		switch e.(type) {
		case *parser.IntLit:
			allFloat = false
			allString = false
			allBool = false
		case *parser.FloatLit:
			allInt = false
			allString = false
			allBool = false
		case *parser.StringLit, *parser.StringInterpLit:
			allInt = false
			allFloat = false
			allBool = false
		case *parser.BoolLit:
			allInt = false
			allFloat = false
			allString = false
		default:
			return "interface{}"
		}
	}
	if allInt {
		return "int"
	}
	if allFloat {
		return "float64"
	}
	if allString {
		return "string"
	}
	if allBool {
		return "bool"
	}
	return "interface{}"
}

// inferMapLitType infers the Go key and value types from a map literal.
// Returns (keyType, valueType) e.g. ("string", "int").
func inferMapLitType(keys []parser.Expr, values []parser.Expr) (string, string) {
	keyType := "string"
	for _, k := range keys {
		if _, ok := k.(*parser.StringLit); !ok {
			keyType = "interface{}"
			break
		}
	}
	valType := inferListLitElemType(values)
	return keyType, valType
}

// inferSliceElemType looks up the element type for an expression that is a slice.
// Checks variable types first (from previous VarStmt), then falls back to literal inference.
func (g *Generator) inferSliceElemType(expr parser.Expr) string {
	switch e := expr.(type) {
	case *parser.Ident:
		if t, ok := g.varTypes[e.Name]; ok {
			return t
		}
	case *parser.ListLit:
		return inferListLitElemType(e.Elements)
	}
	return "interface{}"
}

func (g *Generator) emitVarStmt(v *parser.VarStmt) {
	if v.OrHandler != nil && v.Value != nil {
		// var x = call() or default → x, err := call(); if err != nil { ... }
		// Track struct type if the call is a constructor (NewType)
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				if call.IsNew {
					if _, exists := g.structs[ident.Name]; exists {
						g.varStructTypes[v.Name] = ident.Name
					}
				} else if strings.HasPrefix(ident.Name, "New") {
					structName := ident.Name[3:]
					if _, exists := g.structs[structName]; exists {
						g.varStructTypes[v.Name] = structName
					}
				}
			}
		}
		g.emitOrAssignment(v.Name, v.Value, v.OrHandler)
		return
	}

	if v.Value != nil {
		// Typed array/slice: int[] nums = [1, 2, 3] → nums := []int{1, 2, 3}
		if arrType, ok := v.Type.(*parser.ArrayType); ok {
			if listLit, ok := v.Value.(*parser.ListLit); ok {
				elemType := g.formatType(arrType.ElementType)
				elems := g.formatExprList(listLit.Elements)
				g.varTypes[v.Name] = elemType
				g.writeln("%s := []%s{%s}", v.Name, elemType, elems)
				return
			}
		}
		// Typed generic: List<int> nums = [...] → nums := []int{...}
		if genType, ok := v.Type.(*parser.GenericType); ok {
			if listLit, ok := v.Value.(*parser.ListLit); ok {
				goType := g.formatType(genType)
				// Extract element type from the Go type (e.g. "[]int" → "int")
				if strings.HasPrefix(goType, "[]") {
					g.varTypes[v.Name] = goType[2:]
				}
				elems := g.formatExprList(listLit.Elements)
				g.writeln("%s := %s{%s}", v.Name, goType, elems)
				return
			}
			if mapLit, ok := v.Value.(*parser.MapLit); ok {
				goType := g.formatType(genType)
				g.varTypes[v.Name] = goType // track as map type for iteration
				var pairs []string
				for i := range mapLit.Keys {
					pairs = append(pairs, fmt.Sprintf("%s: %s", g.formatExpr(mapLit.Keys[i]), g.formatExpr(mapLit.Values[i])))
				}
				g.writeln("%s := %s{%s}", v.Name, goType, strings.Join(pairs, ", "))
				return
			}
		}

		// Track struct types from constructor calls: var s = Server(...) or new Server(...)
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				if call.IsNew {
					if _, exists := g.structs[ident.Name]; exists {
						g.varStructTypes[v.Name] = ident.Name
					}
				} else if strings.HasPrefix(ident.Name, "New") {
					structName := ident.Name[3:]
					if _, exists := g.structs[structName]; exists {
						g.varStructTypes[v.Name] = structName
					}
				} else if _, exists := g.structs[ident.Name]; exists {
					// Bare uppercase call: Server(...) → NewServer(...)
					g.varStructTypes[v.Name] = ident.Name
				}
			}
		}

		// Infer type from list literal when no type annotation
		if listLit, ok := v.Value.(*parser.ListLit); ok && v.Type == nil {
			elemType := inferListLitElemType(listLit.Elements)
			if elemType != "interface{}" {
				g.varTypes[v.Name] = elemType
			}
		}

		// Track pointer vars: variables assigned from T?-returning functions or safe nav
		if call, ok := v.Value.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok {
				if g.funcReturnsOptional[ident.Name] {
					g.ptrVars[v.Name] = true
				}
			}
		}
		// Safe navigation results are also pointers
		if _, ok := v.Value.(*parser.SafeNavExpr); ok {
			g.ptrVars[v.Name] = true
		}

		// Track scalar variable types for is/is-not optimization
		if scalarType := g.inferExprType(v.Value, g.varTypes); scalarType != "" && scalarType != "interface{}" {
			g.varTypes[v.Name] = scalarType
		}

		varName := v.Name
		if goBuiltins[varName] {
			safe := "_" + varName
			g.renamedVars[varName] = safe
			varName = safe
		}
		g.writeln("%s := %s", varName, g.formatExpr(v.Value))
	} else {
		typeName := "interface{}"
		if v.Type != nil {
			typeName = g.formatType(v.Type)
		}
		varName := v.Name
		if goBuiltins[varName] {
			safe := "_" + varName
			g.renamedVars[varName] = safe
			varName = safe
		}
		g.writeln("var %s %s", varName, typeName)
	}
}

func (g *Generator) emitAssignStmt(a *parser.AssignStmt) {
	if a.OrHandler != nil {
		// target = call() or default
		targetStr := g.formatExpr(a.Target)
		g.emitOrAssignment(targetStr, a.Value, a.OrHandler)
		return
	}
	g.writeln("%s %s %s", g.formatExpr(a.Target), a.Op, g.formatExpr(a.Value))
}

func (g *Generator) emitReturnStmt(r *parser.ReturnStmt) {
	if r.Value == nil {
		// Bare return in error-returning function → return nil (error only)
		if g.currentReturnType != "" {
			zv := zeroValueFor(g.currentReturnType)
			g.writeln("return %s, nil", zv)
		} else if g.currentReturnType == "" && g.errorFuncs != nil {
			// Check if we're in an error function with no return type
			// bare return → just return nil for the error
			g.writeln("return")
		} else {
			g.writeln("return")
		}
		return
	}

	// return Error(...) → return zero, fmt.Errorf(...)
	if call, ok := r.Value.(*parser.CallExpr); ok {
		if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "Error" {
			zv := zeroValueFor(g.currentReturnType)
			if len(call.Args) == 1 {
				arg := call.Args[0]
				// return Error(CustomType("msg")) → return zero, fmt.Errorf("msg")
				if innerCall, ok := arg.(*parser.CallExpr); ok {
					if _, ok := innerCall.Callee.(*parser.Ident); ok {
						// Use the inner call's first arg as the error message
						g.needImport("fmt")
						if len(innerCall.Args) > 0 {
							msg := g.formatExpr(innerCall.Args[0])
							if zv != "" {
								g.writeln("return %s, fmt.Errorf(%s)", zv, msg)
							} else {
								g.writeln("return fmt.Errorf(%s)", msg)
							}
						} else {
							if zv != "" {
								g.writeln("return %s, fmt.Errorf(\"error\")", zv)
							} else {
								g.writeln("return fmt.Errorf(\"error\")")
							}
						}
						return
					}
				}
				// return Error(err) where err is an identifier → return zero, err
				if id, ok := arg.(*parser.Ident); ok {
					if zv != "" {
						g.writeln("return %s, %s", zv, id.Name)
					} else {
						g.writeln("return %s", id.Name)
					}
					return
				}
				// return Error("message") → return zero, fmt.Errorf("message")
				g.needImport("fmt")
				if zv != "" {
					g.writeln("return %s, fmt.Errorf(%s)", zv, g.formatExpr(arg))
				} else {
					g.writeln("return fmt.Errorf(%s)", g.formatExpr(arg))
				}
				return
			}
			// Error() with no args
			g.needImport("fmt")
			if zv != "" {
				g.writeln("return %s, fmt.Errorf(\"error\")", zv)
			} else {
				g.writeln("return fmt.Errorf(\"error\")")
			}
			return
		}
	}

	// Optional return: wrap value with new() for pointer type (Go 1.26)
	if g.currentReturnOptional {
		// return null → return nil
		if _, ok := r.Value.(*parser.NullLit); ok {
			g.writeln("return nil")
			return
		}
		// return value → return new(value) to create *T from T
		val := g.formatExpr(r.Value)
		g.writeln("return new(%s)", val)
		return
	}

	// Normal return in an error-returning function → return val, nil
	if g.currentReturnType != "" {
		g.writeln("return %s, nil", g.formatExpr(r.Value))
		return
	}

	g.writeln("return %s", g.formatExpr(r.Value))
}

func (g *Generator) emitIfStmt(s *parser.IfStmt) {
	g.writeln("if %s {", g.formatExpr(s.Cond))
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
		if rangeExpr, ok := f.Range.(*parser.RangeExpr); ok {
			// Range expression: for i in 0..10 → for i := 0; i < 10; i++
			start := g.formatExpr(rangeExpr.Start)
			end := g.formatExpr(rangeExpr.End)
			op := "<"
			if rangeExpr.Inclusive {
				op = "<="
			}
			g.writeln("for %s := %s; %s %s %s; %s++ {", f.Item, start, f.Item, op, end, f.Item)
		} else if f.IndexVar != "" {
			// for key, value in map → for key, value := range map
			rangeExpr := g.stripEntrySet(f.Range)
			g.writeln("for %s, %s := range %s {", f.IndexVar, f.Item, rangeExpr)
		} else {
			// for item in list → for _, item := range list
			// for item in map → for item := range map (keys only)
			if g.isEntrySetCall(f.Range) {
				mapExpr := g.stripEntrySet(f.Range)
				g.writeln("for %s := range %s {", f.Item, mapExpr)
			} else if g.isMapVar(f.Range) {
				g.writeln("for %s := range %s {", f.Item, g.formatExpr(f.Range))
			} else {
				g.writeln("for _, %s := range %s {", f.Item, g.formatExpr(f.Range))
			}
		}
	} else {
		// C-style for
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
		g.writeln("for %s; %s; %s {", init, cond, post)
	}
	g.indent++
	g.emitBlock(f.Body)
	g.indent--
	g.writeln("}")
}

// isEntrySetCall checks if an expression is a .entrySet() call.
func (g *Generator) isEntrySetCall(e parser.Expr) bool {
	if call, ok := e.(*parser.CallExpr); ok {
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "entrySet" {
			return true
		}
	}
	return false
}

// isMapVar checks if the range expression is a variable declared as a Map type.
func (g *Generator) isMapVar(e parser.Expr) bool {
	if ident, ok := e.(*parser.Ident); ok {
		if t, ok := g.varTypes[ident.Name]; ok && strings.HasPrefix(t, "map[") {
			return true
		}
		// Also check if it was declared with a Map<K,V> generic type (tracked as "map" prefix)
		if t, ok := g.varTypes[ident.Name]; ok && t == "map" {
			return true
		}
	}
	return false
}

// stripEntrySet removes .entrySet() from a range expression and returns the formatted map expression.
func (g *Generator) stripEntrySet(e parser.Expr) string {
	if call, ok := e.(*parser.CallExpr); ok {
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "entrySet" {
			return g.formatExpr(sel.Object)
		}
	}
	return g.formatExpr(e)
}

func (g *Generator) emitMatchStmt(m *parser.MatchStmt) {
	g.writeln("switch %s {", g.formatExpr(m.Subject))
	for _, c := range m.Cases {
		if c.Pattern == nil {
			g.writeln("default:")
		} else {
			g.writeln("case %s:", g.formatExpr(c.Pattern))
		}
		g.indent++
		g.emitBlock(c.Body)
		g.indent--
	}
	g.writeln("}")
}

func (g *Generator) emitExprStmt(es *parser.ExprStmt) {
	if es.OrHandler != nil {
		g.emitOrAssignment("_", es.Expr, es.OrHandler)
		return
	}
	// spawn { body } → go func() { body }()
	if spawn, ok := es.Expr.(*parser.SpawnExpr); ok {
		g.writeln("go func() {")
		g.indent++
		g.emitBlock(spawn.Body)
		g.indent--
		g.writeln("}()")
		return
	}
	// print("msg {x}") → fmt.Printf("msg %v\n", x)
	if call, ok := es.Expr.(*parser.CallExpr); ok {
		if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "print" && len(call.Args) == 1 {
			if interp, ok := call.Args[0].(*parser.StringInterpLit); ok {
				g.needImport("fmt")
				fmtStr, args := g.formatPrintf(interp)
				if len(args) > 0 {
					g.writeln("fmt.Printf(%q, %s)", fmtStr+"\n", strings.Join(args, ", "))
				} else {
					g.writeln("fmt.Println(%q)", fmtStr)
				}
				return
			}
		}
	}
	// .add() → x = append(x, elem)
	if call, ok := es.Expr.(*parser.CallExpr); ok {
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "add" {
			obj := g.formatExpr(sel.Object)
			args := g.formatExprList(call.Args)
			g.writeln("%s = append(%s, %s)", obj, obj, args)
			return
		}
		// .send() → ch <- val
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "send" && len(call.Args) == 1 {
			obj := g.formatExpr(sel.Object)
			g.writeln("%s <- %s", obj, g.formatExpr(call.Args[0]))
			return
		}
		// .close() → close(ch)
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "close" && len(call.Args) == 0 {
			obj := g.formatExpr(sel.Object)
			g.writeln("close(%s)", obj)
			return
		}
		// .put() → map[key] = value
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "put" && len(call.Args) == 2 {
			obj := g.formatExpr(sel.Object)
			g.writeln("%s[%s] = %s", obj, g.formatExpr(call.Args[0]), g.formatExpr(call.Args[1]))
			return
		}
		// .forEach() as statement
		if sel, ok := call.Callee.(*parser.SelectorExpr); ok && sel.Field == "forEach" && len(call.Args) == 1 {
			// Check if the object is a stream chain (e.g., numbers.filter(pred).forEach(fn))
			if innerCall, ok := sel.Object.(*parser.CallExpr); ok {
				if innerSel, ok := innerCall.Callee.(*parser.SelectorExpr); ok && streamMethods[innerSel.Field] {
					// Fuse the chain: collect filter/map ops and emit a single loop
					g.emitFusedForEachChain(sel.Object, call.Args[0])
					return
				}
			}
			obj := g.formatExpr(sel.Object)
			g.emitForEachStmt(obj, call.Args[0])
			return
		}
	}
	g.writeln("%s", g.formatExpr(es.Expr))
}

// emitForEachStmt emits a for-range loop for .forEach()
func (g *Generator) emitForEachStmt(obj string, fn parser.Expr) {
	if lambda, ok := fn.(*parser.LambdaExpr); ok {
		if len(lambda.Params) == 1 {
			paramName := lambda.Params[0].Name
			g.writeln("for _, %s := range %s {", paramName, obj)
			g.indent++
			if lambda.Expr != nil {
				g.writeln("%s", g.formatExpr(lambda.Expr))
			} else if lambda.Body != nil {
				g.emitBlock(lambda.Body)
			}
			g.indent--
			g.writeln("}")
			return
		}
	}
	// Fallback: check for `it` keyword
	if containsIt(fn) {
		g.writeln("for _, _it := range %s {", obj)
		g.indent++
		g.writeln("%s", g.formatExprIt(fn))
		g.indent--
		g.writeln("}")
		return
	}
	// General case: fn is a function reference
	fnStr := g.formatExpr(fn)
	g.writeln("for _, _v := range %s {", obj)
	g.indent++
	g.writeln("%s(_v)", fnStr)
	g.indent--
	g.writeln("}")
}

// emitFusedForEachChain fuses a stream chain ending in forEach into a single loop.
// Example: numbers.filter(pred).forEach(fn) → for _, _it := range numbers { if pred { fn(_it) } }
func (g *Generator) emitFusedForEachChain(chainExpr parser.Expr, forEachFn parser.Expr) {
	// Collect the chain of stream operations (innermost to outermost)
	type chainOp struct {
		method string
		args   []parser.Expr
	}
	var chain []chainOp
	obj := chainExpr
	for {
		if call, ok := obj.(*parser.CallExpr); ok {
			if sel, ok := call.Callee.(*parser.SelectorExpr); ok && streamMethods[sel.Field] {
				chain = append(chain, chainOp{method: sel.Field, args: call.Args})
				obj = sel.Object
				continue
			}
		}
		break
	}
	// Reverse so chain goes from source to terminal
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	// Check if all operations are fusible (filter/map only)
	fusible := true
	for _, op := range chain {
		if op.method != "filter" && op.method != "map" {
			fusible = false
			break
		}
	}

	if !fusible {
		// Fallback: evaluate the chain normally and iterate
		source := g.formatExpr(chainExpr)
		g.emitForEachStmt(source, forEachFn)
		return
	}

	source := g.formatExpr(obj)

	// Determine the forEach body
	var forEachBody func(iterVar string)
	if lambda, ok := forEachFn.(*parser.LambdaExpr); ok && len(lambda.Params) == 1 {
		paramName := lambda.Params[0].Name
		forEachBody = func(iterVar string) {
			if paramName != iterVar {
				g.writeln("%s := %s", paramName, iterVar)
			}
			if lambda.Expr != nil {
				g.writeln("%s", g.formatExpr(lambda.Expr))
			} else if lambda.Body != nil {
				g.emitBlock(lambda.Body)
			}
		}
	} else if containsIt(forEachFn) {
		forEachBody = func(_ string) {
			g.writeln("%s", g.formatExprIt(forEachFn))
		}
	} else {
		fnStr := g.formatExpr(forEachFn)
		forEachBody = func(iterVar string) {
			g.writeln("%s(%s)", fnStr, iterVar)
		}
	}

	// Emit fused loop
	g.writeln("for _, _it := range %s {", source)
	g.indent++
	iterVar := "_it"
	for _, op := range chain {
		switch op.method {
		case "filter":
			pred := g.streamLambdaBody(op.args)
			g.writeln("if !(%s) { continue }", pred)
		case "map":
			transform := g.streamLambdaBody(op.args)
			g.writeln("_it = %s", transform)
		}
	}
	forEachBody(iterVar)
	g.indent--
	g.writeln("}")
}

// emitOrAssignment handles: target = call() or default / or { block }
func (g *Generator) emitOrAssignment(target string, value parser.Expr, handler *parser.OrHandler) {
	callExpr := g.formatExpr(value)

	// Use unique error variable name to avoid redeclaration
	errVar := "_err"
	if g.errVarCount > 0 {
		errVar = fmt.Sprintf("_err%d", g.errVarCount)
	}
	g.errVarCount++
	savedErrVar := g.currentErrVar
	g.currentErrVar = errVar

	if handler.Body != nil && len(handler.Body.Stmts) == 1 {
		// Single-statement or handler
		if es, ok := handler.Body.Stmts[0].(*parser.ExprStmt); ok && target != "_" {
			// or default_value — assign the expression as a fallback
			g.writeln("%s, %s := %s", target, errVar, callExpr)
			g.writeln("if %s != nil {", errVar)
			g.indent++
			g.writeln("%s = %s", target, g.formatExpr(es.Expr))
			g.indent--
			g.writeln("}")
			g.currentErrVar = savedErrVar
			return
		}
	}

	g.writeln("%s, %s := %s", target, errVar, callExpr)
	g.writeln("if %s != nil {", errVar)
	g.indent++
	if handler.Body != nil {
		g.emitOrBlock(handler.Body)
	}
	g.indent--
	g.writeln("}")
	g.currentErrVar = savedErrVar
}

// emitOrBlock emits a block inside an or-handler, mapping `err` to `_err`.
func (g *Generator) emitOrBlock(block *parser.BlockStmt) {
	for _, s := range block.Stmts {
		// Special handling for return Error(err) in or-blocks
		if ret, ok := s.(*parser.ReturnStmt); ok && ret.Value != nil {
			if call, ok := ret.Value.(*parser.CallExpr); ok {
				if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "Error" {
					if len(call.Args) == 1 {
						if argId, ok := call.Args[0].(*parser.Ident); ok && argId.Name == "err" {
							// return Error(err) in or-block → return zeroVal, errVar
							zv := zeroValueFor(g.currentReturnType)
							if zv != "" {
								g.writeln("return %s, %s", zv, g.currentErrVar)
							} else {
								g.writeln("return %s", g.currentErrVar)
							}
							continue
						}
					}
				}
			}
		}
		g.emitStmt(s)
	}
}

func (g *Generator) emitParallelForStmt(p *parser.ParallelForStmt) {
	g.needImport("sync")
	g.writeln("var _wg sync.WaitGroup")
	if p.Max > 0 {
		g.writeln("_sem := make(chan struct{}, %d)", p.Max)
	}
	g.writeln("for _, %s := range %s {", p.Item, g.formatExpr(p.Range))
	g.indent++
	g.writeln("%s := %s // capture", p.Item, p.Item)
	g.writeln("_wg.Add(1)")
	if p.Max > 0 {
		g.writeln("_sem <- struct{}{}")
	}
	g.writeln("go func() {")
	g.indent++
	g.writeln("defer _wg.Done()")
	if p.Max > 0 {
		g.writeln("defer func() { <-_sem }()")
	}
	g.emitBlock(p.Body)
	g.indent--
	g.writeln("}()")
	g.indent--
	g.writeln("}")
	g.writeln("_wg.Wait()")
}

func (g *Generator) emitConcurrentStmt(c *parser.ConcurrentStmt) {
	g.needImport("sync")
	g.writeln("var _wg sync.WaitGroup")
	for _, task := range c.Tasks {
		g.writeln("_wg.Add(1)")
		g.writeln("go func() {")
		g.indent++
		g.writeln("defer _wg.Done()")
		g.writeln("%s", g.formatExpr(task))
		g.indent--
		g.writeln("}()")
	}
	g.writeln("_wg.Wait()")
}

func (g *Generator) emitWithStmt(w *parser.WithStmt) {
	if len(w.Resources) == 1 && w.Resources[0].Name == "_lock" {
		// lock mu { body } → mu.Lock(); defer mu.Unlock(); body
		lockExpr := g.formatExpr(w.Resources[0].Value)
		g.writeln("%s.Lock()", lockExpr)
		g.writeln("defer %s.Unlock()", lockExpr)
		g.emitBlock(w.Body)
		return
	}
	// General with → open + defer close
	for _, r := range w.Resources {
		g.writeln("%s := %s", r.Name, g.formatExpr(r.Value))
		g.writeln("defer %s.Close()", r.Name)
	}
	g.emitBlock(w.Body)
}

func (g *Generator) emitTupleVarStmt(t *parser.TupleVarStmt) {
	names := strings.Join(t.Names, ", ")
	g.writeln("%s := %s", names, g.formatExpr(t.Value))
}

func (g *Generator) emitAssertStmt(a *parser.AssertStmt) {
	if a.Message != nil {
		g.writeln("if !(%s) { panic(%s) }", g.formatExpr(a.Cond), g.formatExpr(a.Message))
	} else {
		g.writeln("if !(%s) { panic(\"assertion failed\") }", g.formatExpr(a.Cond))
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
		if expr.Name == "this" {
			return "s"
		}
		// Map `err` to current error variable in or-block context
		if expr.Name == "err" && g.currentErrVar != "" {
			return g.currentErrVar
		}
		// Implicit self: bare field name → s.Field in method/ctor context
		// But not if it's a parameter name (params shadow fields)
		if g.currentFields != nil && g.currentFields[expr.Name] && !g.currentParams[expr.Name] {
			return "s." + exportName(expr.Name)
		}
		// Rename vars that shadow Go builtins (tracked at declaration)
		if g.renamedVars != nil {
			if renamed, ok := g.renamedVars[expr.Name]; ok {
				return renamed
			}
		}
		return expr.Name
	case *parser.IntLit:
		return expr.Value
	case *parser.FloatLit:
		return expr.Value
	case *parser.StringLit:
		if strings.Contains(expr.Value, "\n") {
			return fmt.Sprintf("`%s`", expr.Value)
		}
		return fmt.Sprintf("%q", expr.Value)
	case *parser.StringInterpLit:
		return g.formatStringInterp(expr)
	case *parser.BoolLit:
		if expr.Value {
			return "true"
		}
		return "false"
	case *parser.NullLit:
		return "nil"
	case *parser.BinaryExpr:
		return g.formatBinaryExpr(expr)
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", expr.Op, g.formatExpr(expr.Operand))
	case *parser.CallExpr:
		return g.formatCallExpr(expr)
	case *parser.SelectorExpr:
		// .length → len()
		if expr.Field == "length" || expr.Field == "size" {
			return fmt.Sprintf("len(%s)", g.formatExpr(expr.Object))
		}
		// Check if accessing a const field on a class: Config.VERSION → Config_VERSION
		if ident, ok := expr.Object.(*parser.Ident); ok {
			if cls, ok := g.structs[ident.Name]; ok {
				for _, f := range cls.Fields {
					if f.IsConst && f.Name == expr.Field {
						return fmt.Sprintf("%s_%s", ident.Name, exportName(expr.Field))
					}
				}
			}
		}
		return fmt.Sprintf("%s.%s", g.formatExpr(expr.Object), exportName(expr.Field))
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", g.formatExpr(expr.Object), g.formatExpr(expr.Index))
	case *parser.SliceExpr:
		low := ""
		high := ""
		if expr.Low != nil {
			low = g.formatExpr(expr.Low)
		}
		if expr.High != nil {
			high = g.formatExpr(expr.High)
		}
		return fmt.Sprintf("%s[%s:%s]", g.formatExpr(expr.Object), low, high)
	case *parser.ListLit:
		if len(expr.Elements) == 0 {
			return "[]interface{}{}"
		}
		elems := g.formatExprList(expr.Elements)
		elemType := inferListLitElemType(expr.Elements)
		return fmt.Sprintf("[]%s{%s}", elemType, elems)
	case *parser.MapLit:
		if len(expr.Keys) == 0 {
			return "map[string]interface{}{}"
		}
		var pairs []string
		for i := range expr.Keys {
			pairs = append(pairs, fmt.Sprintf("%s: %s", g.formatExpr(expr.Keys[i]), g.formatExpr(expr.Values[i])))
		}
		keyType, valType := inferMapLitType(expr.Keys, expr.Values)
		return fmt.Sprintf("map[%s]%s{%s}", keyType, valType, strings.Join(pairs, ", "))
	case *parser.LambdaExpr:
		return g.formatLambdaExpr(expr)
	case *parser.ThisExpr:
		return "s"
	case *parser.SuperCallExpr:
		return fmt.Sprintf("/* super(%s) */", g.formatExprList(expr.Args))
	case *parser.TypeAssertExpr:
		goType := g.formatType(&parser.SimpleType{Name: expr.TypeName})
		if expr.IsCheck {
			obj := g.formatExpr(expr.Object)
			// For interface types, use type assertion; for concrete types, use reflect
			// Simple approach: try type assertion, fall back to reflect
			g.needImport("reflect")
			return fmt.Sprintf("(reflect.TypeOf(%s).String() == \"%s\" || reflect.TypeOf(%s).Kind().String() == \"%s\")", obj, goType, obj, goType)
		}
		return fmt.Sprintf("%s.(%s)", g.formatExpr(expr.Object), goType)
	case *parser.SafeNavExpr:
		obj := g.formatExpr(expr.Object)
		deref := "*" + obj // dereference pointer for method calls
		if expr.Call != nil {
			args := g.formatExprList(expr.Call.Args)
			// Handle string methods on *string
			field := expr.Field
			if field == "length" {
				return fmt.Sprintf("func() *int { if %s == nil { return nil }; _v := len(%s); return new(_v) }()", obj, deref)
			}
			// Check string method mapping
			if goFunc, ok := stringMethodMapping[field]; ok {
				g.needImport("strings")
				if args != "" {
					return fmt.Sprintf("func() *string { if %s == nil { return nil }; _v := %s(%s, %s); return new(_v) }()", obj, goFunc, deref, args)
				}
				return fmt.Sprintf("func() *string { if %s == nil { return nil }; _v := %s(%s); return new(_v) }()", obj, goFunc, deref)
			}
			return fmt.Sprintf("func() interface{} { if %s != nil { return %s.%s(%s) }; return nil }()", obj, deref, exportName(field), args)
		}
		if expr.Field == "length" {
			return fmt.Sprintf("func() *int { if %s == nil { return nil }; _v := len(%s); return new(_v) }()", obj, deref)
		}
		return fmt.Sprintf("func() interface{} { if %s != nil { return %s.%s }; return nil }()", obj, deref, exportName(expr.Field))
	case *parser.TupleLit:
		// Go doesn't have tuples — use a struct or slice
		return fmt.Sprintf("[]interface{}{%s}", g.formatExprList(expr.Elements))
	case *parser.SpawnExpr:
		// spawn as expression — emit inline goroutine
		// Note: spawn as statement is handled in emitExprStmt
		return "/* spawn */"
	case *parser.IfExpr:
		retType := "interface{}"
		thenType := g.inferExprType(expr.Then, g.varTypes)
		elseType := g.inferExprType(expr.Else, g.varTypes)
		if thenType != "" && thenType == elseType {
			retType = thenType
		} else if thenType != "" && thenType != "interface{}" {
			retType = thenType
		} else if elseType != "" && elseType != "interface{}" {
			retType = elseType
		}
		return fmt.Sprintf("func() %s { if %s { return %s }; return %s }()",
			retType, g.formatExpr(expr.Cond), g.formatExpr(expr.Then), g.formatExpr(expr.Else))
	case *parser.MatchExpr:
		return g.formatMatchExpr(expr)
	case *parser.RangeExpr:
		// Range as value — not directly expressible in Go
		return fmt.Sprintf("/* range %s..%s */", g.formatExpr(expr.Start), g.formatExpr(expr.End))
	case *parser.RawStringLit:
		return fmt.Sprintf("`%s`", expr.Value)
	case *parser.SpreadExpr:
		return g.formatExpr(expr.Expr) + "..."
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
		g.needImport("math")
		return fmt.Sprintf("math.Pow(float64(%s), float64(%s))", left, right)
	case "==":
		return fmt.Sprintf("%s == %s", left, right)
	case "!=":
		return fmt.Sprintf("%s != %s", left, right)
	case "===":
		// Reference identity — same as == in Go for pointers
		return fmt.Sprintf("%s == %s", left, right)
	case "!==":
		return fmt.Sprintf("%s != %s", left, right)
	case "in":
		return g.formatInExpr(b.Left, b.Right, left, right)
	case "not in":
		return "!" + g.formatInExpr(b.Left, b.Right, left, right)
	case "is":
		// Map Zinc type name to Go reflect name
		goType := g.formatType(&parser.SimpleType{Name: right})
		// If the variable has a known concrete type that matches, avoid reflect
		knownType := g.inferExprType(b.Left, g.varTypes)
		if knownType != "" && knownType != "interface{}" && knownType == goType {
			// Use a no-op reference to keep the variable "used" in Go
			return fmt.Sprintf("func() bool { _ = %s; return true }()", left)
		}
		g.needImport("reflect")
		return fmt.Sprintf("(reflect.TypeOf(%s).String() == \"%s\" || reflect.TypeOf(%s).Kind().String() == \"%s\")", left, goType, left, goType)
	case "is not":
		goType := g.formatType(&parser.SimpleType{Name: right})
		// If the variable has a known concrete type that matches, avoid reflect
		knownType := g.inferExprType(b.Left, g.varTypes)
		if knownType != "" && knownType != "interface{}" && knownType == goType {
			return fmt.Sprintf("func() bool { _ = %s; return false }()", left)
		}
		g.needImport("reflect")
		return fmt.Sprintf("!(reflect.TypeOf(%s).String() == \"%s\" || reflect.TypeOf(%s).Kind().String() == \"%s\")", left, goType, left, goType)
	default:
		return fmt.Sprintf("%s %s %s", left, b.Op, right)
	}
}

// formatInExpr handles the `in` operator for strings, maps, and slices.
func (g *Generator) formatInExpr(leftExpr, rightExpr parser.Expr, left, right string) string {
	// String "in" check: "x" in str → strings.Contains(str, "x")
	if _, ok := leftExpr.(*parser.StringLit); ok {
		g.needImport("strings")
		return fmt.Sprintf("strings.Contains(%s, %s)", right, left)
	}
	// If the right side looks like it could be a map, use _, ok pattern
	// We use an IIFE to handle both map and slice cases
	return fmt.Sprintf("func() bool { for _, _v := range %s { if _v == %s { return true } }; return false }()", right, left)
}

// stringMethodMapping maps Zinc string methods to Go equivalents.
var stringMethodMapping = map[string]string{
	"upper":      "strings.ToUpper",
	"lower":      "strings.ToLower",
	"trim":       "strings.TrimSpace",
	"contains":   "strings.Contains",
	"startsWith": "strings.HasPrefix",
	"endsWith":   "strings.HasSuffix",
	"split":      "strings.Split",
	"repeat":     "strings.Repeat",
	"indexOf":    "strings.Index",
}

// streamMethods is the set of methods that trigger stream/inline-loop codegen.
var streamMethods = map[string]bool{
	"filter": true, "map": true, "sum": true,
	"anyMatch": true, "allMatch": true, "noneMatch": true,
	"findFirst": true, "skip": true, "limit": true,
	"distinct": true, "reduce": true, "forEach": true,
	"sortBy": true, "groupBy": true,
}

func (g *Generator) formatCallExpr(c *parser.CallExpr) string {
	// String method rewrites
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if goFunc, ok := stringMethodMapping[sel.Field]; ok {
			g.needImport("strings")
			obj := g.formatExpr(sel.Object)
			args := g.formatExprList(c.Args)
			if args != "" {
				return fmt.Sprintf("%s(%s, %s)", goFunc, obj, args)
			}
			return fmt.Sprintf("%s(%s)", goFunc, obj)
		}

		// Stream operations — detect chains and single calls
		if streamMethods[sel.Field] {
			return g.formatStreamExpr(sel, c.Args)
		}

		// Collection methods
		obj := g.formatExpr(sel.Object)
		switch sel.Field {
		case "add":
			args := g.formatExprList(c.Args)
			return fmt.Sprintf("append(%s, %s)", obj, args)
		case "put":
			if len(c.Args) == 2 {
				return fmt.Sprintf("func() { %s[%s] = %s }()", obj, g.formatExpr(c.Args[0]), g.formatExpr(c.Args[1]))
			}
		case "send":
			// ch.send(val) → ch <- val
			if len(c.Args) == 1 {
				return fmt.Sprintf("func() { %s <- %s }()", obj, g.formatExpr(c.Args[0]))
			}
		case "recv":
			// ch.recv() → <-ch
			return fmt.Sprintf("<-%s", obj)
		case "close":
			// ch.close() → close(ch)
			return fmt.Sprintf("close(%s)", obj)
		case "size":
			return fmt.Sprintf("len(%s)", obj)
		case "isEmpty":
			return fmt.Sprintf("len(%s) == 0", obj)
		case "length":
			return fmt.Sprintf("len(%s)", obj)
		case "charAt":
			return fmt.Sprintf("string(%s[%s])", obj, g.formatExprList(c.Args))
		case "substring":
			args := c.Args
			if len(args) == 2 {
				return fmt.Sprintf("%s[%s:%s]", obj, g.formatExpr(args[0]), g.formatExpr(args[1]))
			}
			return fmt.Sprintf("%s[%s:]", obj, g.formatExpr(args[0]))
		case "replace":
			g.needImport("strings")
			if len(c.Args) == 2 {
				return fmt.Sprintf("strings.ReplaceAll(%s, %s, %s)", obj, g.formatExpr(c.Args[0]), g.formatExpr(c.Args[1]))
			}
		case "trimStart":
			g.needImport("strings")
			return fmt.Sprintf("strings.TrimLeft(%s, \" \\t\\n\\r\")", obj)
		case "trimEnd":
			g.needImport("strings")
			return fmt.Sprintf("strings.TrimRight(%s, \" \\t\\n\\r\")", obj)
		case "upper":
			g.needImport("strings")
			return fmt.Sprintf("strings.ToUpper(%s)", obj)
		case "lower":
			g.needImport("strings")
			return fmt.Sprintf("strings.ToLower(%s)", obj)
		case "entrySet":
			// map.entrySet() → just the map (used in for range)
			return obj
		case "getKey":
			return obj + ".Key"
		case "getValue":
			return obj + ".Value"
		case "join":
			g.needImport("strings")
			if len(c.Args) == 1 {
				return fmt.Sprintf("strings.Join(%s, %s)", obj, g.formatExpr(c.Args[0]))
			}
			return fmt.Sprintf("strings.Join(%s, \"\")", obj)
		case "keys":
			return fmt.Sprintf("func() []interface{} { _keys := make([]interface{}, 0, len(%s)); for _k := range %s { _keys = append(_keys, _k) }; return _keys }()", obj, obj)
		case "values":
			return fmt.Sprintf("func() []interface{} { _vals := make([]interface{}, 0, len(%s)); for _, _v := range %s { _vals = append(_vals, _v) }; return _vals }()", obj, obj)
		case "containsKey":
			if len(c.Args) == 1 {
				return fmt.Sprintf("func() bool { _, _ok := %s[%s]; return _ok }()", obj, g.formatExpr(c.Args[0]))
			}
		case "remove":
			if len(c.Args) == 1 {
				return fmt.Sprintf("delete(%s, %s)", obj, g.formatExpr(c.Args[0]))
			}
		case "sort":
			g.needImport("sort")
			return fmt.Sprintf("func() { sort.Slice(%s, func(i, j int) bool { return %s[i] < %s[j] }) }()", obj, obj, obj)
		case "reverse":
			return fmt.Sprintf("func() { for _i, _j := 0, len(%s)-1; _i < _j; _i, _j = _i+1, _j-1 { %s[_i], %s[_j] = %s[_j], %s[_i] } }()", obj, obj, obj, obj, obj)
		default:
			// Getter pattern on known struct variables: obj.getHost() → obj.Host
			if strings.HasPrefix(sel.Field, "get") && len(sel.Field) > 3 && len(c.Args) == 0 {
				fieldName := strings.ToLower(sel.Field[3:4]) + sel.Field[4:]
				// Check if the object is a known struct variable
				if ident, ok := sel.Object.(*parser.Ident); ok {
					if structName, ok := g.varStructTypes[ident.Name]; ok {
						if cls, ok := g.structs[structName]; ok {
							for _, f := range cls.Fields {
								if f.Name == fieldName {
									return fmt.Sprintf("%s.%s", obj, exportName(fieldName))
								}
							}
						}
					}
				}
			}
		}
	}

	callee := g.formatExpr(c.Callee)

	// Set.of(...) → map[T]struct{}{...}
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if ident, ok := sel.Object.(*parser.Ident); ok && ident.Name == "Set" && sel.Field == "of" {
			var elems []string
			for _, a := range c.Args {
				elems = append(elems, fmt.Sprintf("%s: {}", g.formatExpr(a)))
			}
			return fmt.Sprintf("map[interface{}]struct{}{%s}", strings.Join(elems, ", "))
		}
	}

	// Implicit self method calls: address() → s.Address() in method context
	if ident, ok := c.Callee.(*parser.Ident); ok && g.currentMethods != nil {
		if g.currentMethods[ident.Name] {
			callee = "s." + exportName(ident.Name)
		}
		// Getter pattern: getField() → s.Field
		if strings.HasPrefix(ident.Name, "get") && len(ident.Name) > 3 {
			fieldName := strings.ToLower(ident.Name[3:4]) + ident.Name[4:]
			if g.currentFields != nil && g.currentFields[fieldName] {
				return "s." + exportName(fieldName)
			}
		}
	}

	// Resolve Go function's expected param types for callback adaptation
	var goExpectedParams [][]string // per-arg: expected callback param types (nil if not callback)
	if sel, ok := c.Callee.(*parser.SelectorExpr); ok {
		if ident, ok := sel.Object.(*parser.Ident); ok {
			if pkgPath, ok := g.importMap[ident.Name]; ok {
				goExpectedParams = make([][]string, len(c.Args))
				for i := range c.Args {
					goExpectedParams[i] = g.goResolver.FuncParamCallbackSignature(pkgPath, sel.Field, i)
				}
			}
		}
	}

	// Rewrite `it` keyword in args + adapt callback signatures
	var argStrs []string
	for i, arg := range c.Args {
		if containsIt(arg) {
			argStrs = append(argStrs, g.formatExprIt(arg))
		} else if ident, ok := arg.(*parser.Ident); ok && goExpectedParams != nil && goExpectedParams[i] != nil {
			// This arg is a function reference being passed to a Go function that expects
			// a specific callback signature. Emit an adapter wrapper if needed.
			argStrs = append(argStrs, g.adaptCallback(ident.Name, goExpectedParams[i]))
		} else {
			argStrs = append(argStrs, g.formatExpr(arg))
		}
	}
	for _, na := range c.NamedArgs {
		argStrs = append(argStrs, g.formatExpr(na.Value))
	}
	args := strings.Join(argStrs, ", ")

	// Default parameters: fill in missing args
	if ident, ok := c.Callee.(*parser.Ident); ok {
		args = g.fillDefaultArgs(ident.Name, c.Args, c.NamedArgs, args)
	}

	// Builtin rewrites
	switch callee {
	case "print":
		g.needImport("fmt")
		return fmt.Sprintf("fmt.Println(%s)", args)
	case "len":
		return fmt.Sprintf("len(%s)", args)
	case "str":
		g.needImport("fmt")
		return fmt.Sprintf("fmt.Sprint(%s)", args)
	case "int":
		g.needImport("strconv")
		return fmt.Sprintf("strconv.Atoi(%s)", args)
	case "float":
		g.needImport("strconv")
		return fmt.Sprintf("strconv.ParseFloat(%s, 64)", args)
	case "input":
		g.needImport("fmt")
		return fmt.Sprintf("func() string { var s string; fmt.Scanln(&s); return s }()")
	case "make":
		return fmt.Sprintf("make(%s)", args)
	case "delete":
		return fmt.Sprintf("delete(%s)", args)
	case "append":
		return fmt.Sprintf("append(%s)", args)
	case "close":
		return fmt.Sprintf("close(%s)", args)
	}

	// Channel(size) → make(chan interface{}, size)
	// channel(size) → make(chan interface{}, size)
	if callee == "Channel" || callee == "channel" || callee == "Chan" {
		chanType := "interface{}"
		if len(c.TypeArgs) > 0 {
			if mapped, ok := zincToGoType[c.TypeArgs[0]]; ok {
				chanType = mapped
			} else {
				chanType = c.TypeArgs[0]
			}
		}
		if args != "" {
			return fmt.Sprintf("make(chan %s, %s)", chanType, args)
		}
		return fmt.Sprintf("make(chan %s)", chanType)
	}

	// Constructor calls: new Type() → NewType()
	if c.IsNew {
		ctorName := "New" + callee
		args = g.fillDefaultArgs(ctorName, c.Args, c.NamedArgs, args)
		return fmt.Sprintf("%s(%s)", ctorName, args)
	}

	// Implicit constructor: Type(args) → NewType(args) when Type is a known struct or data class
	if ident, ok := c.Callee.(*parser.Ident); ok {
		if _, isStruct := g.structs[ident.Name]; isStruct {
			ctorName := "New" + ident.Name
			args = g.fillDefaultArgs(ctorName, c.Args, c.NamedArgs, args)
			return fmt.Sprintf("%s(%s)", ctorName, args)
		}
		if g.dataClasses[ident.Name] {
			ctorName := "New" + ident.Name
			args = g.fillDefaultArgs(ctorName, c.Args, c.NamedArgs, args)
			return fmt.Sprintf("%s(%s)", ctorName, args)
		}
	}

	return fmt.Sprintf("%s(%s)", callee, args)
}

// adaptCallback generates an adapter for a Zinc function being passed to a Go
// function that expects a specific callback signature. If the Zinc function's
// param types don't match (e.g., Go expects *http.Request but Zinc has http.Request),
// emit a wrapper that converts. If types match, just pass the function directly.
func (g *Generator) adaptCallback(funcName string, goExpectedTypes []string) string {
	// Look up the Zinc function's params
	zincParams, ok := g.funcSigs[funcName]
	if !ok || len(zincParams) != len(goExpectedTypes) {
		return funcName // can't adapt, pass as-is
	}

	// Check if any param types differ (need adaptation)
	needsAdapter := false
	for i, expected := range goExpectedTypes {
		zincType := "interface{}"
		if zincParams[i].Type != nil {
			zincType = g.formatType(zincParams[i].Type)
		}
		// Normalize: strip package paths for comparison
		// Go gives us "net/http.ResponseWriter" but we have "http.ResponseWriter"
		normalizedExpected := expected
		if idx := strings.LastIndex(expected, "/"); idx >= 0 {
			// "*net/http.Request" → "*http.Request"
			prefix := ""
			if strings.HasPrefix(expected, "*") {
				prefix = "*"
				expected = expected[1:]
			}
			if idx := strings.LastIndex(expected, "/"); idx >= 0 {
				normalizedExpected = prefix + expected[idx+1:]
			}
		}
		if normalizedExpected != zincType {
			needsAdapter = true
			break
		}
	}

	if !needsAdapter {
		return funcName
	}

	// Generate adapter: func(goParam1, goParam2) { funcName(adapted1, adapted2) }
	var adapterParams []string
	var callArgs []string
	for i, expected := range goExpectedTypes {
		paramName := fmt.Sprintf("_p%d", i)
		// Normalize expected type for Go code
		goType := expected
		isPointer := strings.HasPrefix(goType, "*")
		if idx := strings.LastIndex(goType, "/"); idx >= 0 {
			prefix := ""
			rest := goType
			if strings.HasPrefix(goType, "*") {
				prefix = "*"
				rest = goType[1:]
			}
			if idx := strings.LastIndex(rest, "/"); idx >= 0 {
				goType = prefix + rest[idx+1:]
			}
		}
		adapterParams = append(adapterParams, paramName+" "+goType)

		// Check if Zinc function expects value but Go passes pointer → deref
		zincType := "interface{}"
		if i < len(zincParams) && zincParams[i].Type != nil {
			zincType = g.formatType(zincParams[i].Type)
		}
		zincIsPointer := strings.HasPrefix(zincType, "*")
		if isPointer && !zincIsPointer {
			callArgs = append(callArgs, "*"+paramName) // deref pointer for Zinc function
		} else if !isPointer && zincIsPointer {
			callArgs = append(callArgs, "&"+paramName) // take address for Zinc function
		} else {
			callArgs = append(callArgs, paramName)
		}
	}

	return fmt.Sprintf("func(%s) { %s(%s) }",
		strings.Join(adapterParams, ", "),
		funcName,
		strings.Join(callArgs, ", "))
}

// fillDefaultArgs fills in missing positional args with defaults from funcSigs.
func (g *Generator) fillDefaultArgs(funcName string, posArgs []parser.Expr, namedArgs []parser.NamedArg, currentArgs string) string {
	sig, ok := g.funcSigs[funcName]
	if !ok {
		return currentArgs
	}
	totalProvided := len(posArgs) + len(namedArgs)
	if totalProvided >= len(sig) {
		return currentArgs
	}
	// Fill in defaults for missing params
	var parts []string
	// Add existing positional args
	for _, a := range posArgs {
		parts = append(parts, g.formatExpr(a))
	}
	for _, na := range namedArgs {
		parts = append(parts, g.formatExpr(na.Value))
	}
	// Fill in defaults
	for i := totalProvided; i < len(sig); i++ {
		if sig[i].Default != nil {
			parts = append(parts, g.formatExpr(sig[i].Default))
		}
	}
	return strings.Join(parts, ", ")
}

// --- Stream operations (inline loop codegen) ---------------------------------

// formatStreamExpr handles stream method calls, including chains.
// It unwraps chained calls, generates each step as a separate variable,
// and returns the final value.
func (g *Generator) formatStreamExpr(sel *parser.SelectorExpr, args []parser.Expr) string {
	// Collect the chain of stream operations from innermost to outermost
	var chain []streamOp
	chain = append(chain, streamOp{method: sel.Field, args: args})

	// Walk the chain downward
	obj := sel.Object
	for {
		if call, ok := obj.(*parser.CallExpr); ok {
			if innerSel, ok := call.Callee.(*parser.SelectorExpr); ok && streamMethods[innerSel.Field] {
				chain = append(chain, streamOp{method: innerSel.Field, args: call.Args})
				obj = innerSel.Object
				continue
			}
		}
		break
	}

	// Reverse chain so it goes from source → terminal
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	sourceExpr := g.formatExpr(obj)

	// Single operation (no chain) → use IIFE
	if len(chain) == 1 {
		return g.formatSingleStreamOp(sourceExpr, obj, chain[0].method, chain[0].args)
	}

	// Chained operations → try loop fusion first
	elemType := g.inferSliceElemType(obj)
	lastOp := chain[len(chain)-1].method

	// Loop fusion: filter → [map →] terminal in a single pass
	// Detect: filter?, map?, terminal (sum/reduce/forEach/anyMatch/allMatch/noneMatch/findFirst/count)
	if fused := g.tryLoopFusion(sourceExpr, chain, elemType); fused != "" {
		return fused
	}

	// Fallback: intermediate variables (non-fusible chains)
	retType := "interface{}"
	switch lastOp {
	case "sum":
		if elemType == "int" || elemType == "float64" {
			retType = elemType
		} else {
			retType = "int"
		}
	case "anyMatch", "allMatch", "noneMatch":
		retType = "bool"
	case "reduce":
		retType = elemType
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("func() %s {\n", retType))
	currentVar := sourceExpr
	currentElemType := elemType
	for i, op := range chain {
		varName := fmt.Sprintf("_chain%d", i)
		innerCode := g.formatSingleStreamAssignTyped(varName, currentVar, op.method, op.args, currentElemType)
		sb.WriteString(innerCode)
		currentVar = varName
	}
	sb.WriteString(fmt.Sprintf("return %s\n", currentVar))
	sb.WriteString("}()")
	return sb.String()
}

// tryLoopFusion attempts to fuse a chain of stream operations into a single loop.
// Fusible patterns: [filter]* → [map]? → terminal
// Returns empty string if fusion is not possible.
func (g *Generator) tryLoopFusion(source string, chain []streamOp, elemType string) string {
	if len(chain) < 2 {
		return ""
	}

	// Classify each operation
	var filters []string   // filter predicates
	var mapExpr string     // map transformation (at most one)
	var terminal streamOp  // terminal operation
	terminalIdx := -1

	terminals := map[string]bool{
		"sum": true, "reduce": true, "forEach": true, "count": true,
		"anyMatch": true, "allMatch": true, "noneMatch": true, "findFirst": true,
	}
	intermediates := map[string]bool{
		"filter": true, "map": true,
	}

	for i, op := range chain {
		if terminals[op.method] {
			terminal = op
			terminalIdx = i
			break
		}
		if !intermediates[op.method] {
			return "" // non-fusible intermediate (sortBy, distinct, skip, limit)
		}
	}

	if terminalIdx < 0 {
		return "" // no terminal — ends with intermediate (produces a list), can't fuse into single value
	}

	// Collect filters and map before the terminal
	for i := 0; i < terminalIdx; i++ {
		op := chain[i]
		switch op.method {
		case "filter":
			filters = append(filters, g.streamLambdaBody(op.args))
		case "map":
			mapExpr = g.streamLambdaBody(op.args)
		default:
			return "" // can't fuse
		}
	}

	// Build the fused loop
	var sb strings.Builder
	iterVar := "_it"
	mappedVar := "_it"

	switch terminal.method {
	case "sum":
		retType := elemType
		if retType == "" || retType == "interface{}" {
			retType = "int"
		}
		sb.WriteString(fmt.Sprintf("func() %s { _acc := %s; for _, %s := range %s {",
			retType, zeroValueFor(retType), iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		if mapExpr != "" {
			sb.WriteString(fmt.Sprintf("; _v := %s; _acc += _v", mapExpr))
		} else {
			sb.WriteString("; _acc += _it")
		}
		sb.WriteString(" }; return _acc }()")

	case "count":
		sb.WriteString(fmt.Sprintf("func() int { _acc := 0; for _, %s := range %s {", iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		sb.WriteString("; _acc++")
		sb.WriteString(" }; return _acc }()")

	case "anyMatch":
		pred := g.streamLambdaBody(terminal.args)
		sb.WriteString(fmt.Sprintf("func() bool { for _, %s := range %s {", iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		if mapExpr != "" {
			mappedVar = "_v"
			sb.WriteString(fmt.Sprintf("; %s := %s", mappedVar, mapExpr))
		}
		// Replace _it with mappedVar in predicate if we have a map
		finalPred := pred
		if mapExpr != "" {
			finalPred = strings.ReplaceAll(pred, "_it", mappedVar)
		}
		sb.WriteString(fmt.Sprintf("; if %s { return true }", finalPred))
		sb.WriteString(" }; return false }()")

	case "allMatch":
		pred := g.streamLambdaBody(terminal.args)
		sb.WriteString(fmt.Sprintf("func() bool { for _, %s := range %s {", iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		finalPred := pred
		if mapExpr != "" {
			mappedVar = "_v"
			sb.WriteString(fmt.Sprintf("; %s := %s", mappedVar, mapExpr))
			finalPred = strings.ReplaceAll(pred, "_it", mappedVar)
		}
		sb.WriteString(fmt.Sprintf("; if !(%s) { return false }", finalPred))
		sb.WriteString(" }; return true }()")

	case "noneMatch":
		pred := g.streamLambdaBody(terminal.args)
		sb.WriteString(fmt.Sprintf("func() bool { for _, %s := range %s {", iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		finalPred := pred
		if mapExpr != "" {
			mappedVar = "_v"
			sb.WriteString(fmt.Sprintf("; %s := %s", mappedVar, mapExpr))
			finalPred = strings.ReplaceAll(pred, "_it", mappedVar)
		}
		sb.WriteString(fmt.Sprintf("; if %s { return false }", finalPred))
		sb.WriteString(" }; return true }()")

	case "findFirst":
		pred := g.streamLambdaBody(terminal.args)
		sb.WriteString(fmt.Sprintf("func() %s { for _, %s := range %s {", elemType, iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		finalPred := pred
		if mapExpr != "" {
			mappedVar = "_v"
			sb.WriteString(fmt.Sprintf("; %s := %s", mappedVar, mapExpr))
			finalPred = strings.ReplaceAll(pred, "_it", mappedVar)
		}
		sb.WriteString(fmt.Sprintf("; if %s { return _it }", finalPred))
		sb.WriteString(fmt.Sprintf(" }; var _zero %s; return _zero }()", elemType))

	case "forEach":
		body := g.streamLambdaBody(terminal.args)
		sb.WriteString(fmt.Sprintf("func() { for _, %s := range %s {", iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		if mapExpr != "" {
			sb.WriteString(fmt.Sprintf("; _it = %s", mapExpr))
		}
		sb.WriteString(fmt.Sprintf("; %s", body))
		sb.WriteString(" } }()")

	case "reduce":
		if len(terminal.args) < 2 {
			return ""
		}
		initVal := g.formatExpr(terminal.args[0])
		reduceBody := g.streamReduceBody(terminal.args[1])
		sb.WriteString(fmt.Sprintf("func() %s { _acc := %s; for _, %s := range %s {",
			elemType, initVal, iterVar, source))
		for _, f := range filters {
			sb.WriteString(fmt.Sprintf(" if !(%s) { continue }", f))
		}
		if mapExpr != "" {
			sb.WriteString(fmt.Sprintf("; _it = %s", mapExpr))
		}
		sb.WriteString(fmt.Sprintf("; _acc = %s", reduceBody))
		sb.WriteString(" }; return _acc }()")

	default:
		return "" // unknown terminal
	}

	return sb.String()
}

type streamOp = struct {
	method string
	args   []parser.Expr
}

// formatSingleStreamOp generates an IIFE for a single stream operation.
func (g *Generator) formatSingleStreamOp(source string, sourceExpr parser.Expr, method string, args []parser.Expr) string {
	elemType := g.inferSliceElemType(sourceExpr)
	sliceType := "[]" + elemType
	switch method {
	case "filter":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("func() %s { var _r %s; for _, _it := range %s { if %s { _r = append(_r, _it) } }; return _r }()", sliceType, sliceType, source, pred)
	case "map":
		transform := g.streamLambdaBody(args)
		return fmt.Sprintf("func() %s { _r := make(%s, len(%s)); for _i, _it := range %s { _r[_i] = %s }; return _r }()", sliceType, sliceType, source, source, transform)
	case "sum":
		if elemType == "int" {
			return fmt.Sprintf("func() int { _s := 0; for _, _it := range %s { _s += _it }; return _s }()", source)
		}
		if elemType == "float64" {
			return fmt.Sprintf("func() float64 { _s := 0.0; for _, _it := range %s { _s += _it }; return _s }()", source)
		}
		return fmt.Sprintf("func() int { _s := 0; for _, _it := range %s { _s += _it.(int) }; return _s }()", source)
	case "anyMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("func() bool { for _, _it := range %s { if %s { return true } }; return false }()", source, pred)
	case "allMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("func() bool { for _, _it := range %s { if !(%s) { return false } }; return true }()", source, pred)
	case "noneMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("func() bool { for _, _it := range %s { if %s { return false } }; return true }()", source, pred)
	case "findFirst":
		pred := g.streamLambdaBody(args)
		if elemType != "interface{}" {
			return fmt.Sprintf("func() %s { for _, _it := range %s { if %s { return _it } }; var _zero %s; return _zero }()", elemType, source, pred, elemType)
		}
		return fmt.Sprintf("func() interface{} { for _, _it := range %s { if %s { return _it } }; return nil }()", source, pred)
	case "skip":
		if len(args) > 0 {
			n := g.formatExpr(args[0])
			return fmt.Sprintf("%s[%s:]", source, n)
		}
		return source
	case "limit":
		if len(args) > 0 {
			n := g.formatExpr(args[0])
			return fmt.Sprintf("%s[:%s]", source, n)
		}
		return source
	case "distinct":
		return fmt.Sprintf("func() %s { _seen := map[%s]bool{}; var _r %s; for _, _it := range %s { if !_seen[_it] { _seen[_it] = true; _r = append(_r, _it) } }; return _r }()", sliceType, elemType, sliceType, source)
	case "reduce":
		if len(args) >= 2 {
			init := g.formatExpr(args[0])
			fn := g.streamReduceBody(args[1])
			if elemType != "interface{}" {
				return fmt.Sprintf("func() %s { _acc := %s; for _, _it := range %s { _acc = %s }; return _acc }()", elemType, init, source, fn)
			}
			return fmt.Sprintf("func() interface{} { _acc := %s; for _, _it := range %s { _acc = %s }; return _acc }()", init, source, fn)
		}
		return source
	case "forEach":
		if len(args) > 0 {
			body := g.streamLambdaBody(args)
			return fmt.Sprintf("func() { for _, _it := range %s { _ = %s } }()", source, body)
		}
		return source
	case "sortBy":
		if len(args) > 0 {
			g.needImport("sort")
			key := g.streamLambdaBody(args)
			cmp := g.sortByComparison(key, elemType)
			return fmt.Sprintf("func() %s { _r := make(%s, len(%s)); copy(_r, %s); sort.Slice(_r, func(_i, _j int) bool { _it := _r[_i]; _ = _it; _a := %s; _it = _r[_j]; _b := %s; return %s }); return _r }()", sliceType, sliceType, source, source, key, key, cmp)
		}
		return source
	case "groupBy":
		if len(args) > 0 {
			key := g.streamLambdaBody(args)
			keyType := g.inferGroupByKeyType(key, args)
			return fmt.Sprintf("func() map[%s]%s { _r := map[%s]%s{}; for _, _it := range %s { _k := %s; _r[_k] = append(_r[_k], _it) }; return _r }()", keyType, sliceType, keyType, sliceType, source, key)
		}
		return source
	default:
		return fmt.Sprintf("%s.%s()", source, method)
	}
}

// formatSingleStreamAssignTyped generates code that assigns the result of a stream op to a variable,
// using the known element type for the source slice.
func (g *Generator) formatSingleStreamAssignTyped(varName, source, method string, args []parser.Expr, elemType string) string {
	sliceType := "[]" + elemType
	switch method {
	case "filter":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("var %s %s\nfor _, _it := range %s { if %s { %s = append(%s, _it) } }\n", varName, sliceType, source, pred, varName, varName)
	case "map":
		transform := g.streamLambdaBody(args)
		return fmt.Sprintf("%s := make(%s, len(%s))\nfor _i, _it := range %s { %s[_i] = %s }\n", varName, sliceType, source, source, varName, transform)
	case "sum":
		if elemType == "int" || elemType == "float64" {
			return fmt.Sprintf("%s := 0\nfor _, _it := range %s { %s += _it }\n", varName, source, varName)
		}
		return fmt.Sprintf("%s := 0\nfor _, _it := range %s { _s, _ok := _it.(int); if _ok { %s += _s } }\n", varName, source, varName)
	case "anyMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("%s := false\nfor _, _it := range %s { if %s { %s = true; break } }\n", varName, source, pred, varName)
	case "allMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("%s := true\nfor _, _it := range %s { if !(%s) { %s = false; break } }\n", varName, source, pred, varName)
	case "noneMatch":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("%s := true\nfor _, _it := range %s { if %s { %s = false; break } }\n", varName, source, pred, varName)
	case "findFirst":
		pred := g.streamLambdaBody(args)
		return fmt.Sprintf("var %s %s\nfor _, _it := range %s { if %s { %s = _it; break } }\n", varName, elemType, source, pred, varName)
	case "skip":
		if len(args) > 0 {
			n := g.formatExpr(args[0])
			return fmt.Sprintf("%s := %s[%s:]\n", varName, source, n)
		}
		return fmt.Sprintf("%s := %s\n", varName, source)
	case "limit":
		if len(args) > 0 {
			n := g.formatExpr(args[0])
			return fmt.Sprintf("%s := %s[:%s]\n", varName, source, n)
		}
		return fmt.Sprintf("%s := %s\n", varName, source)
	case "distinct":
		return fmt.Sprintf("_seen_%s := map[%s]bool{}\nvar %s %s\nfor _, _it := range %s { if !_seen_%s[_it] { _seen_%s[_it] = true; %s = append(%s, _it) } }\n",
			varName, elemType, varName, sliceType, source, varName, varName, varName, varName)
	case "reduce":
		if len(args) >= 2 {
			init := g.formatExpr(args[0])
			fn := g.streamReduceBody(args[1])
			return fmt.Sprintf("%s := %s\nfor _, _it := range %s { %s = %s }\n", varName, init, source, varName, fn)
		}
		return fmt.Sprintf("%s := %s\n", varName, source)
	case "forEach":
		if len(args) > 0 {
			body := g.streamLambdaBody(args)
			return fmt.Sprintf("for _, _it := range %s { _ = %s }\n%s := 0\n_ = %s\n", source, body, varName, varName)
		}
		return fmt.Sprintf("%s := 0\n_ = %s\n", varName, varName)
	case "sortBy":
		if len(args) > 0 {
			g.needImport("sort")
			key := g.streamLambdaBody(args)
			cmp := g.sortByComparison(key, elemType)
			return fmt.Sprintf("%s := make(%s, len(%s))\ncopy(%s, %s)\nsort.Slice(%s, func(_i, _j int) bool { _it := %s[_i]; _ = _it; _a := %s; _it = %s[_j]; _b := %s; return %s })\n",
				varName, sliceType, source, varName, source, varName, varName, key, varName, key, cmp)
		}
		return fmt.Sprintf("%s := %s\n", varName, source)
	case "groupBy":
		if len(args) > 0 {
			key := g.streamLambdaBody(args)
			keyType := g.inferGroupByKeyType(key, args)
			return fmt.Sprintf("%s := map[%s]%s{}\nfor _, _it := range %s { _k := %s; %s[_k] = append(%s[_k], _it) }\n",
				varName, keyType, sliceType, source, key, varName, varName)
		}
		return fmt.Sprintf("%s := %s\n", varName, source)
	default:
		return fmt.Sprintf("%s := %s\n", varName, source)
	}
}

// sortByComparison returns the comparison expression for sortBy.
// For known types (int, float64, string), compares directly: _a < _b.
// For interface{}, falls back to fmt.Sprint(_a) < fmt.Sprint(_b).
func (g *Generator) sortByComparison(keyExpr string, elemType string) string {
	// Infer key type from the key expression
	keyType := ""
	switch {
	case elemType == "int" || elemType == "float64" || elemType == "string":
		keyType = elemType
	case strings.HasPrefix(keyExpr, "string("):
		keyType = "string"
	case strings.HasPrefix(keyExpr, "int(") || strings.HasPrefix(keyExpr, "len("):
		keyType = "int"
	case strings.HasPrefix(keyExpr, "float64("):
		keyType = "float64"
	}
	if keyType == "int" || keyType == "float64" || keyType == "string" {
		return "_a < _b"
	}
	g.needImport("fmt")
	return "fmt.Sprint(_a) < fmt.Sprint(_b)"
}

// inferGroupByKeyType infers the key type for groupBy from the lambda expression.
// Returns "string" for string conversions/methods, "int" for int conversions/len, otherwise "interface{}".
func (g *Generator) inferGroupByKeyType(keyExpr string, args []parser.Expr) string {
	// Check the generated key expression for type hints
	switch {
	case strings.HasPrefix(keyExpr, "string("):
		return "string"
	case strings.HasPrefix(keyExpr, "int(") || strings.HasPrefix(keyExpr, "len("):
		return "int"
	case strings.HasPrefix(keyExpr, "float64("):
		return "float64"
	}
	// Try to infer from the lambda's expression AST
	if len(args) > 0 {
		if lambda, ok := args[0].(*parser.LambdaExpr); ok && lambda.Expr != nil {
			t := g.inferExprType(lambda.Expr, g.varTypes)
			if t != "" && t != "interface{}" {
				return t
			}
		}
	}
	// Default to string as it's the most common groupBy key type
	return "string"
}

// streamLambdaBody extracts the body expression from a lambda or `it`-expression arg.
func (g *Generator) streamLambdaBody(args []parser.Expr) string {
	if len(args) == 0 {
		return "true"
	}
	arg := args[0]
	// Lambda with explicit params
	if lambda, ok := arg.(*parser.LambdaExpr); ok {
		if lambda.Expr != nil {
			if len(lambda.Params) == 1 {
				// Replace param name with _it
				return g.replaceIdent(lambda.Expr, lambda.Params[0].Name, "_it")
			}
			return g.formatExpr(lambda.Expr)
		}
	}
	// Expression using `it`
	if containsIt(arg) {
		return g.formatExprIt(arg)
	}
	// Just a plain expression or function reference
	return g.formatExpr(arg) + "(_it)"
}

// streamReduceBody extracts the body for a reduce operation.
// The accumulator is referenced as _acc.
func (g *Generator) streamReduceBody(arg parser.Expr) string {
	if lambda, ok := arg.(*parser.LambdaExpr); ok {
		if lambda.Expr != nil && len(lambda.Params) == 2 {
			// Replace first param with _acc, second with _it
			replaced := g.replaceIdent(lambda.Expr, lambda.Params[0].Name, "_acc")
			// Need to do second replacement in the string
			replaced = strings.ReplaceAll(replaced, lambda.Params[1].Name, "_it")
			return replaced
		}
	}
	return g.formatExpr(arg) + "(_acc, _it)"
}

// replaceIdent formats an expression, replacing occurrences of oldName with newName.
func (g *Generator) replaceIdent(e parser.Expr, oldName, newName string) string {
	switch expr := e.(type) {
	case *parser.Ident:
		if expr.Name == oldName {
			return newName
		}
		return g.formatExpr(e)
	case *parser.BinaryExpr:
		left := g.replaceIdent(expr.Left, oldName, newName)
		right := g.replaceIdent(expr.Right, oldName, newName)
		op := expr.Op
		switch op {
		case "and":
			op = "&&"
		case "or":
			op = "||"
		}
		return fmt.Sprintf("%s %s %s", left, op, right)
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", expr.Op, g.replaceIdent(expr.Operand, oldName, newName))
	case *parser.SelectorExpr:
		return fmt.Sprintf("%s.%s", g.replaceIdent(expr.Object, oldName, newName), exportName(expr.Field))
	case *parser.CallExpr:
		// Handle method call rewrites (length, charAt, etc.)
		if sel, ok := expr.Callee.(*parser.SelectorExpr); ok {
			obj := g.replaceIdent(sel.Object, oldName, newName)
			var replArgs []string
			for _, a := range expr.Args {
				replArgs = append(replArgs, g.replaceIdent(a, oldName, newName))
			}
			switch sel.Field {
			case "length", "size":
				return fmt.Sprintf("len(%s)", obj)
			case "charAt":
				if len(replArgs) > 0 {
					return fmt.Sprintf("string(%s[%s])", obj, replArgs[0])
				}
			case "substring":
				if len(replArgs) == 2 {
					return fmt.Sprintf("%s[%s:%s]", obj, replArgs[0], replArgs[1])
				}
				if len(replArgs) == 1 {
					return fmt.Sprintf("%s[%s:]", obj, replArgs[0])
				}
			case "upper":
				g.needImport("strings")
				return fmt.Sprintf("strings.ToUpper(%s)", obj)
			case "lower":
				g.needImport("strings")
				return fmt.Sprintf("strings.ToLower(%s)", obj)
			case "contains":
				g.needImport("strings")
				if len(replArgs) > 0 {
					return fmt.Sprintf("strings.Contains(%s, %s)", obj, replArgs[0])
				}
			case "replace":
				g.needImport("strings")
				if len(replArgs) == 2 {
					return fmt.Sprintf("strings.ReplaceAll(%s, %s, %s)", obj, replArgs[0], replArgs[1])
				}
			}
			// Check string method mapping
			if goFunc, ok := stringMethodMapping[sel.Field]; ok {
				g.needImport("strings")
				if len(replArgs) > 0 {
					return fmt.Sprintf("%s(%s, %s)", goFunc, obj, strings.Join(replArgs, ", "))
				}
				return fmt.Sprintf("%s(%s)", goFunc, obj)
			}
			return fmt.Sprintf("%s.%s(%s)", obj, exportName(sel.Field), strings.Join(replArgs, ", "))
		}
		callee := g.replaceIdent(expr.Callee, oldName, newName)
		var args []string
		for _, a := range expr.Args {
			args = append(args, g.replaceIdent(a, oldName, newName))
		}
		// Builtin rewrites in stream context
		if ident, ok := expr.Callee.(*parser.Ident); ok {
			switch ident.Name {
			case "print":
				g.needImport("fmt")
				return fmt.Sprintf("fmt.Println(%s)", strings.Join(args, ", "))
			case "len":
				return fmt.Sprintf("len(%s)", strings.Join(args, ", "))
			case "str":
				g.needImport("fmt")
				return fmt.Sprintf("fmt.Sprint(%s)", strings.Join(args, ", "))
			}
		}
		return fmt.Sprintf("%s(%s)", callee, strings.Join(args, ", "))
	case *parser.IndexExpr:
		return fmt.Sprintf("%s[%s]", g.replaceIdent(expr.Object, oldName, newName), g.replaceIdent(expr.Index, oldName, newName))
	default:
		return g.formatExpr(e)
	}
}

// --- Lambda and misc expression formatting -----------------------------------

func (g *Generator) formatLambdaExpr(l *parser.LambdaExpr) string {
	var params []string
	var firstParamType string
	allTyped := true
	for _, p := range l.Params {
		typeName := "interface{}"
		if p.Type != nil {
			typeName = g.formatType(p.Type)
			if firstParamType == "" {
				firstParamType = typeName
			}
		} else {
			allTyped = false
		}
		params = append(params, p.Name+" "+typeName)
	}
	paramStr := strings.Join(params, ", ")

	if l.Expr != nil {
		// Infer return type
		retType := "interface{}"
		if l.ReturnType != nil {
			retType = g.formatType(l.ReturnType)
		} else if allTyped && firstParamType != "" {
			// Infer from expression: if it's arithmetic on typed params, return same type
			retType = g.inferLambdaReturnType(l.Expr, l.Params)
		}

		// Void lambda (expression is a print/void call)
		if g.isVoidExpr(l.Expr) {
			return fmt.Sprintf("func(%s) { %s }", paramStr, g.formatExpr(l.Expr))
		}

		return fmt.Sprintf("func(%s) %s { return %s }", paramStr, retType, g.formatExpr(l.Expr))
	}
	// Block lambda — infer return type from return statements in body
	if l.Body != nil && len(l.Body.Stmts) > 0 {
		// Build param type map for inference (include enclosing function params)
		paramTypes := map[string]string{}
		for _, p := range g.currentFuncParams {
			if p.Type != nil {
				paramTypes[p.Name] = g.formatType(p.Type)
			}
		}
		for _, p := range l.Params {
			if p.Type != nil {
				paramTypes[p.Name] = g.formatType(p.Type)
			}
		}

		// Check for return statements to infer return type
		blockRetType := ""
		for _, s := range l.Body.Stmts {
			if ret, ok := s.(*parser.ReturnStmt); ok && ret.Value != nil {
				blockRetType = g.inferExprType(ret.Value, paramTypes)
				break
			}
		}

		var stmts []string
		for _, s := range l.Body.Stmts {
			stmts = append(stmts, g.formatStmtInline(s))
		}

		if blockRetType != "" && blockRetType != "interface{}" {
			return fmt.Sprintf("func(%s) %s { %s }", paramStr, blockRetType, strings.Join(stmts, "; "))
		}
		return fmt.Sprintf("func(%s) { %s }", paramStr, strings.Join(stmts, "; "))
	}
	return fmt.Sprintf("func(%s) {}", paramStr)
}

// inferLambdaReturnType infers the return type of a lambda from its expression and param types.
func (g *Generator) inferLambdaReturnType(expr parser.Expr, params []*parser.ParamDecl) string {
	paramTypes := map[string]string{}
	// Include enclosing function's params (for closures like middleware)
	for _, p := range g.currentFuncParams {
		if p.Type != nil {
			paramTypes[p.Name] = g.formatType(p.Type)
		}
	}
	// Lambda's own params override
	for _, p := range params {
		if p.Type != nil {
			paramTypes[p.Name] = g.formatType(p.Type)
		}
	}

	return g.inferExprType(expr, paramTypes)
}

// inferExprType infers the Go type of an expression given known variable types.
func (g *Generator) inferExprType(expr parser.Expr, known map[string]string) string {
	switch e := expr.(type) {
	case *parser.IntLit:
		return "int"
	case *parser.FloatLit:
		return "float64"
	case *parser.StringLit, *parser.StringInterpLit:
		return "string"
	case *parser.BoolLit:
		return "bool"
	case *parser.Ident:
		if t, ok := known[e.Name]; ok {
			return t
		}
	case *parser.BinaryExpr:
		lt := g.inferExprType(e.Left, known)
		rt := g.inferExprType(e.Right, known)
		// Arithmetic/comparison on same numeric types
		if lt == rt && lt != "" {
			switch e.Op {
			case "+", "-", "*", "/", "%":
				return lt
			case ">", "<", ">=", "<=", "==", "!=":
				return "bool"
			}
		}
		if lt == "int" || rt == "int" {
			switch e.Op {
			case "+", "-", "*", "/", "%":
				return "int"
			}
		}
		if lt == "string" || rt == "string" {
			if e.Op == "+" {
				return "string"
			}
		}
	case *parser.CallExpr:
		if ident, ok := e.Callee.(*parser.Ident); ok {
			// Check known function return types
			if rt, ok := g.funcReturnTypes[ident.Name]; ok {
				return rt
			}
			// Check if callee is a param with function type
			if t, ok := known[ident.Name]; ok {
				// Resolve type aliases
				resolved := t
				if alias, ok := g.typeAliases[t]; ok {
					resolved = g.formatType(alias)
				}
				if strings.HasPrefix(resolved, "func(") {
					if idx := strings.LastIndex(resolved, ") "); idx >= 0 {
						return strings.TrimSpace(resolved[idx+2:])
					}
				}
			}
			switch ident.Name {
			case "len":
				return "int"
			case "str":
				return "string"
			}
		}
	}
	return "interface{}"
}

// isVoidExpr checks if an expression is a void call (print, etc.)
func (g *Generator) isVoidExpr(expr parser.Expr) bool {
	if call, ok := expr.(*parser.CallExpr); ok {
		if ident, ok := call.Callee.(*parser.Ident); ok {
			switch ident.Name {
			case "print", "println":
				return true
			}
		}
	}
	return false
}

func (g *Generator) formatStringInterp(s *parser.StringInterpLit) string {
	g.needImport("fmt")
	var fmtStr strings.Builder
	var args []string
	for _, p := range s.Parts {
		switch part := p.(type) {
		case *parser.StringLit:
			// Escape % signs for fmt.Sprintf
			escaped := strings.ReplaceAll(part.Value, "%", "%%")
			fmtStr.WriteString(escaped)
		default:
			fmtStr.WriteString("%v")
			expr := g.formatExpr(part)
			// Deref pointer vars for clean printing
			isPtr := false
			if ident, ok := part.(*parser.Ident); ok {
				isPtr = g.ptrVars[ident.Name]
			}
			if isPtr {
				expr = fmt.Sprintf("func() interface{} { if %s != nil { return *%s }; return \"null\" }()", expr, expr)
			}
			args = append(args, expr)
		}
	}
	if len(args) == 0 {
		return fmt.Sprintf("%q", fmtStr.String())
	}
	return fmt.Sprintf("fmt.Sprintf(%q, %s)", fmtStr.String(), strings.Join(args, ", "))
}

// formatPrintf returns the format string and args separately for use in fmt.Printf.
func (g *Generator) formatPrintf(s *parser.StringInterpLit) (string, []string) {
	var fmtStr strings.Builder
	var args []string
	for _, p := range s.Parts {
		switch part := p.(type) {
		case *parser.StringLit:
			escaped := strings.ReplaceAll(part.Value, "%", "%%")
			fmtStr.WriteString(escaped)
		default:
			fmtStr.WriteString("%v")
			expr := g.formatExpr(part)
			isPtr := false
			if ident, ok := part.(*parser.Ident); ok {
				isPtr = g.ptrVars[ident.Name]
			}
			if isPtr {
				expr = fmt.Sprintf("func() interface{} { if %s != nil { return *%s }; return \"null\" }()", expr, expr)
			}
			args = append(args, expr)
		}
	}
	return fmtStr.String(), args
}

func (g *Generator) formatMatchExpr(m *parser.MatchExpr) string {
	// Go doesn't have switch expressions — use IIFE
	var sb strings.Builder
	sb.WriteString("func() interface{} { switch ")
	sb.WriteString(g.formatExpr(m.Subject))
	sb.WriteString(" { ")
	for _, c := range m.Cases {
		if c.Pattern == nil {
			sb.WriteString(fmt.Sprintf("default: return %s; ", g.formatExpr(c.Value)))
		} else {
			sb.WriteString(fmt.Sprintf("case %s: return %s; ", g.formatExpr(c.Pattern), g.formatExpr(c.Value)))
		}
	}
	sb.WriteString("}; return nil }()")
	return sb.String()
}

// --- Type formatting ---------------------------------------------------------

var zincToGoType = map[string]string{
	"int":     "int",
	"Int":     "int",
	"double":  "float64",
	"Double":  "float64",
	"float":   "float64",
	"Float":   "float64",
	"String":  "string",
	"string":  "string",
	"boolean": "bool",
	"Boolean": "bool",
	"bool":    "bool",
	"Bool":    "bool",
	"char":    "rune",
	"Char":    "rune",
	"long":    "int64",
	"Long":    "int64",
	"byte":    "byte",
	"Byte":    "byte",
	"void":    "",
	"Void":    "",
	"Object":  "interface{}",
	"Any":     "interface{}",
}

func (g *Generator) formatType(t parser.TypeExpr) string {
	switch typ := t.(type) {
	case *parser.SimpleType:
		if mapped, ok := zincToGoType[typ.Name]; ok {
			return mapped
		}
		// Type alias — use the alias name (Go will resolve it via type declaration)
		if _, ok := g.typeAliases[typ.Name]; ok {
			return typ.Name
		}
		// Smart import resolution: Mutex → sync.Mutex if imported via `import sync.Mutex`
		if qualified, ok := g.typeImports[typ.Name]; ok {
			return qualified
		}
		return typ.Name
	case *parser.GenericType:
		switch typ.Name {
		case "List":
			if len(typ.TypeArgs) > 0 {
				return "[]" + g.formatType(typ.TypeArgs[0])
			}
			return "[]interface{}"
		case "Map":
			if len(typ.TypeArgs) >= 2 {
				return fmt.Sprintf("map[%s]%s", g.formatType(typ.TypeArgs[0]), g.formatType(typ.TypeArgs[1]))
			}
			return "map[string]interface{}"
		case "Set":
			if len(typ.TypeArgs) > 0 {
				return fmt.Sprintf("map[%s]struct{}", g.formatType(typ.TypeArgs[0]))
			}
			return "map[interface{}]struct{}"
		case "Channel", "Chan":
			if len(typ.TypeArgs) > 0 {
				return "chan " + g.formatType(typ.TypeArgs[0])
			}
			return "chan interface{}"
		default:
			// Generic struct — Go 1.18+ generics
			var args []string
			for _, a := range typ.TypeArgs {
				args = append(args, g.formatType(a))
			}
			return fmt.Sprintf("%s[%s]", typ.Name, strings.Join(args, ", "))
		}
	case *parser.ArrayType:
		return "[]" + g.formatType(typ.ElementType)
	case *parser.OptionalType:
		return "*" + g.formatType(typ.Inner)
	case *parser.FuncTypeExpr:
		var params []string
		for _, p := range typ.Params {
			params = append(params, g.formatType(p))
		}
		ret := ""
		if typ.ReturnType != nil {
			ret = " " + g.formatType(typ.ReturnType)
		}
		return fmt.Sprintf("func(%s)%s", strings.Join(params, ", "), ret)
	default:
		return "interface{}"
	}
}

// goReturnTypeStr returns the Go type string for a return type (without leading space).
func (g *Generator) goReturnTypeStr(retType parser.TypeExpr) string {
	if retType == nil {
		return ""
	}
	return g.formatType(retType)
}

// formatReturnType builds the Go return type string including error if needed.
func (g *Generator) formatReturnType(retType parser.TypeExpr, body *parser.BlockStmt) string {
	if retType == nil {
		return ""
	}
	return " " + g.formatType(retType)
}

// formatParams formats function parameters.
func (g *Generator) formatParams(params []*parser.ParamDecl) string {
	var parts []string
	for _, p := range params {
		typeName := "interface{}"
		if p.Type != nil {
			typeName = g.formatType(p.Type)
		}
		if p.Variadic {
			typeName = "..." + typeName
		}
		parts = append(parts, p.Name+" "+typeName)
	}
	return strings.Join(parts, ", ")
}

func (g *Generator) formatExprList(exprs []parser.Expr) string {
	var parts []string
	for _, e := range exprs {
		parts = append(parts, g.formatExpr(e))
	}
	return strings.Join(parts, ", ")
}

func (g *Generator) formatStmtInline(s parser.Stmt) string {
	switch stmt := s.(type) {
	case *parser.VarStmt:
		if stmt.Value != nil {
			return fmt.Sprintf("%s := %s", stmt.Name, g.formatExpr(stmt.Value))
		}
		return fmt.Sprintf("var %s interface{}", stmt.Name)
	case *parser.AssignStmt:
		return fmt.Sprintf("%s %s %s", g.formatExpr(stmt.Target), stmt.Op, g.formatExpr(stmt.Value))
	case *parser.ExprStmt:
		// Optimize print with string interpolation: use fmt.Printf instead of fmt.Println(fmt.Sprintf(...))
		if call, ok := stmt.Expr.(*parser.CallExpr); ok {
			if ident, ok := call.Callee.(*parser.Ident); ok && ident.Name == "print" && len(call.Args) == 1 {
				if interp, ok := call.Args[0].(*parser.StringInterpLit); ok {
					g.needImport("fmt")
					fmtStr, args := g.formatPrintf(interp)
					if len(args) > 0 {
						return fmt.Sprintf("fmt.Printf(%q, %s)", fmtStr+"\n", strings.Join(args, ", "))
					}
					return fmt.Sprintf("fmt.Println(%q)", fmtStr)
				}
			}
		}
		return g.formatExpr(stmt.Expr)
	case *parser.ReturnStmt:
		if stmt.Value != nil {
			return "return " + g.formatExpr(stmt.Value)
		}
		return "return"
	default:
		return "/* inline stmt */"
	}
}

// --- it keyword helpers ------------------------------------------------------

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
		switch expr.Op {
		case "and":
			return fmt.Sprintf("%s && %s", left, right)
		case "or":
			return fmt.Sprintf("%s || %s", left, right)
		default:
			return fmt.Sprintf("%s %s %s", left, expr.Op, right)
		}
	case *parser.UnaryExpr:
		return fmt.Sprintf("%s%s", expr.Op, g.formatExprIt(expr.Operand))
	case *parser.SelectorExpr:
		obj := g.formatExprIt(expr.Object)
		// Handle .length / .size as field access
		if expr.Field == "length" || expr.Field == "size" {
			return fmt.Sprintf("len(%s)", obj)
		}
		return fmt.Sprintf("%s.%s", obj, expr.Field)
	case *parser.CallExpr:
		// Handle method call rewrites for `it` expressions
		if sel, ok := expr.Callee.(*parser.SelectorExpr); ok {
			obj := g.formatExprIt(sel.Object)
			var itArgs []string
			for _, a := range expr.Args {
				itArgs = append(itArgs, g.formatExprIt(a))
			}
			switch sel.Field {
			case "length", "size":
				return fmt.Sprintf("len(%s)", obj)
			case "charAt":
				if len(itArgs) > 0 {
					return fmt.Sprintf("string(%s[%s])", obj, itArgs[0])
				}
			case "substring":
				if len(itArgs) == 2 {
					return fmt.Sprintf("%s[%s:%s]", obj, itArgs[0], itArgs[1])
				}
				if len(itArgs) == 1 {
					return fmt.Sprintf("%s[%s:]", obj, itArgs[0])
				}
			case "upper":
				g.needImport("strings")
				return fmt.Sprintf("strings.ToUpper(%s)", obj)
			case "lower":
				g.needImport("strings")
				return fmt.Sprintf("strings.ToLower(%s)", obj)
			}
			// Check string method mapping
			if goFunc, ok := stringMethodMapping[sel.Field]; ok {
				g.needImport("strings")
				if len(itArgs) > 0 {
					return fmt.Sprintf("%s(%s, %s)", goFunc, obj, strings.Join(itArgs, ", "))
				}
				return fmt.Sprintf("%s(%s)", goFunc, obj)
			}
			return fmt.Sprintf("%s.%s(%s)", obj, exportName(sel.Field), strings.Join(itArgs, ", "))
		}
		callee := g.formatExprIt(expr.Callee)
		var args []string
		for _, a := range expr.Args {
			args = append(args, g.formatExprIt(a))
		}
		// Builtin rewrites
		if ident, ok := expr.Callee.(*parser.Ident); ok {
			switch ident.Name {
			case "print":
				g.needImport("fmt")
				return fmt.Sprintf("fmt.Println(%s)", strings.Join(args, ", "))
			case "len":
				return fmt.Sprintf("len(%s)", strings.Join(args, ", "))
			}
		}
		return fmt.Sprintf("%s(%s)", callee, strings.Join(args, ", "))
	default:
		return g.formatExpr(e)
	}
}

// --- Output helpers ----------------------------------------------------------

func (g *Generator) writeln(format string, args ...interface{}) {
	g.buf.WriteString(strings.Repeat("\t", g.indent))
	fmt.Fprintf(&g.buf, format, args...)
	g.buf.WriteString("\n")
}

func (g *Generator) write(format string, args ...interface{}) {
	g.buf.WriteString(strings.Repeat("\t", g.indent))
	fmt.Fprintf(&g.buf, format, args...)
}

// --- Name helpers ------------------------------------------------------------

// exportName capitalizes the first letter to make it exported in Go.
func exportName(name string) string {
	if name == "" {
		return ""
	}
	// Already capitalized
	if name[0] >= 'A' && name[0] <= 'Z' {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// goBuiltins are Go builtin names that can't be used as variable names.
var goBuiltins = map[string]bool{
	"len": true, "cap": true, "make": true, "new": true, "append": true,
	"copy": true, "delete": true, "close": true, "panic": true, "recover": true,
	"complex": true, "real": true, "imag": true,
	"min": true, "max": true, "clear": true,
}

// safeVarName returns a variable name that doesn't shadow Go builtins.
func safeVarName(name string) string {
	if goBuiltins[name] {
		return "_" + name
	}
	return name
}
