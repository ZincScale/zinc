// Package codegen_go generates Go source code from Zinc AST.
//
// The code generator is split across several files:
//   - codegen.go       — Generator struct, initialization, Generate/GenerateFiles entry points
//   - codegen_types.go — Type declarations: classes, data classes, sealed, enums, interfaces
//   - codegen_stmts.go — Statement emission: var, assign, return, if, for, match, etc.
//   - codegen_exprs.go — Expression formatting: literals, calls, lambdas, string interp
//   - codegen_streams.go — Stream operations with loop fusion
//   - gotypes.go       — Go type introspection via go/types
package codegen_go

import (
	"fmt"
	"go/types"
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
	currentReturnType     string            // return type of current function (for zero values in error returns)
	currentReturnOptional bool              // true if current function returns T? (pointer type)
	currentFuncParams     []*parser.ParamDecl // params of current function (for lambda type inference)
	currentMethodRetType  string            // Go return type of current method (for channel recv type assertions)

	// Default parameters
	funcSigs map[string][]*parser.ParamDecl // function name → param list

	// Stream operations
	chainCounter int // counter for _chain variables

	// Scope tracking
	errVarCount   int    // counter for unique _err variables in same scope
	currentErrVar string // current error variable name (for or-blocks)

	// Variable type tracking
	varTypes            map[string]string       // variable name → element type
	varTypeExprs        map[string]parser.TypeExpr // variable name → original AST type (for generics)
	varGoTypes          map[string]types.Type   // variable name → Go type (from stdlib call returns)
	ptrVars             map[string]bool         // variables that are pointers (*T from T? returns)
	funcReturnsOptional map[string]bool       // functions that return T? (optional)
	funcReturnTypes     map[string]string     // function name → Go return type string
	renamedVars         map[string]string     // original name → safe name (for builtin shadows)
	varStructTypes      map[string]string     // variable name → struct type name
	dataClasses         map[string]bool       // data class names that have NewType constructors
	typeAliases         map[string]parser.TypeExpr // type alias name → underlying type
	goResolver          *GoTypeResolver       // introspects Go packages at transpile time
	importMap           map[string]string     // import prefix → full Go package path
	typeImports         map[string]string     // short type name → qualified Go name (e.g. "Mutex" → "sync.Mutex")
	activeTypeParams    map[string]bool       // currently-in-scope generic type parameter names

	// Subpackage support
	packageName      string            // Go package name (default: "main")
	moduleName       string            // Go module name from zinc.toml (for subpackage import paths)
	zincSubpackages  map[string]bool   // known zinc subpackage names (directory names in src/)
	subpkgExports    map[string]map[string]string // pkg → name → kind ("data", "class", "func", "interface")
	importAliases    map[string]string // import alias → Go module path (e.g. "stdlib" → "github.com/ZincScale/zinc-stdlib")
	importGoAliases  map[string]string // Go import path → local alias (when alias differs from package name)
}

// isZincSubpackage checks if an identifier is a zinc subpackage alias.
// Handles both direct names ("core") and aliases from nested packages
// ("router" as alias for "fabric/router" via importMap).
func (g *Generator) isZincSubpackage(name string) bool {
	// Direct match (e.g. "core" → subpackages["core"])
	if g.zincSubpackages[name] {
		return true
	}
	// Alias match: if "router" is in importMap and maps to a subpackage path
	if goPath, ok := g.importMap[name]; ok {
		// Strip module prefix to get the subpackage path
		subPath := goPath
		if g.moduleName != "" && strings.HasPrefix(goPath, g.moduleName+"/") {
			subPath = goPath[len(g.moduleName)+1:]
		}
		if g.zincSubpackages[subPath] {
			return true
		}
	}
	return false
}

// isImportAlias checks if an identifier is a package alias from [imports] in zinc.toml.
func (g *Generator) isImportAlias(name string) bool {
	if g.importAliases == nil {
		return false
	}
	// Check if name is a Go package alias that came from an import alias expansion.
	// e.g. "logging" from "import stdlib.logging" where stdlib is aliased
	if goPath, ok := g.importMap[name]; ok {
		for _, modulePath := range g.importAliases {
			if strings.HasPrefix(goPath, modulePath) {
				return true
			}
		}
	}
	return false
}

// New creates a new Go code generator.
func New() *Generator {
	return &Generator{
		imports:             make(map[string]bool),
		interfaces:          make(map[string]bool),
		structs:             make(map[string]*parser.ClassDecl),
		errorFuncs:          make(map[string]bool),
		funcSigs:            make(map[string][]*parser.ParamDecl),
		varTypes:            make(map[string]string),
		varTypeExprs:        make(map[string]parser.TypeExpr),
		varGoTypes:          make(map[string]types.Type),
		ptrVars:             make(map[string]bool),
		funcReturnsOptional: make(map[string]bool),
		funcReturnTypes:     make(map[string]string),
		renamedVars:         make(map[string]string),
		varStructTypes:      make(map[string]string),
		dataClasses:         make(map[string]bool),
		typeAliases:         make(map[string]parser.TypeExpr),
		goResolver:          NewGoTypeResolver(),
		importMap:           make(map[string]string),
		typeImports:         make(map[string]string),
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

// SetPackageName sets the Go package name (default: "main").
func (g *Generator) SetPackageName(name string) {
	g.packageName = name
}

// SetModuleName sets the Go module name for resolving subpackage imports.
func (g *Generator) SetModuleName(name string) {
	g.moduleName = name
}

// SetZincSubpackages sets the known zinc subpackage names.
func (g *Generator) SetZincSubpackages(pkgs map[string]bool) {
	g.zincSubpackages = pkgs
}

// SetImportAliases sets the import alias → module path mappings from zinc.toml [imports].
func (g *Generator) SetImportAliases(aliases map[string]string) {
	g.importAliases = aliases
}

// SetSubpackageExports registers exported names from a subpackage.
func (g *Generator) SetSubpackageExports(pkg string, exports map[string]string) {
	if g.subpkgExports == nil {
		g.subpkgExports = make(map[string]map[string]string)
	}
	g.subpkgExports[pkg] = exports
}

// CollectExports returns a map of exported names from a parsed program.
// Keys are zinc names, values are kinds: "data", "class", "func", "interface".
func CollectExports(prog *parser.Program) map[string]string {
	exports := make(map[string]string)
	for _, d := range prog.Decls {
		switch decl := d.(type) {
		case *parser.DataClassDecl:
			exports[decl.Name] = "data"
		case *parser.ClassDecl:
			exports[decl.Name] = "class"
			// Export sealed class variants (they are data classes)
			if decl.IsSealed {
				for _, v := range decl.Variants {
					exports[v.Name] = "data"
				}
			}
		case *parser.InterfaceDecl:
			exports[decl.Name] = "interface"
		case *parser.FnDecl:
			if decl.Name != "main" {
				exports[decl.Name] = "func"
			}
		case *parser.EnumDecl:
			exports[decl.Name] = "enum"
		case *parser.ConstDecl:
			exports[decl.Name] = "const"
		case *parser.TypeAliasDecl:
			exports[decl.Name] = "type"
		}
	}
	return exports
}

// isSubpackage returns true if generating code for a non-main package.
func (g *Generator) isSubpackage() bool {
	return g.packageName != "" && g.packageName != "main"
}

// exportIfSubpackage uppercases the first letter of a name when generating
// a subpackage (non-main). In Go, only uppercase names are exported.
func (g *Generator) exportIfSubpackage(name string) string {
	if g.isSubpackage() {
		return exportName(name)
	}
	return name
}

// RegisterInterface allows external callers to register interface names.
func (g *Generator) RegisterInterface(name string) {
	g.interfaces[name] = true
}

// --- Declaration scanning ----------------------------------------------------

// collectDecls scans declarations to build lookup tables for types,
// constructors, error functions, and type aliases.
func (g *Generator) collectDecls(decls []parser.TopLevelDecl) {
	for _, d := range decls {
		switch decl := d.(type) {
		case *parser.InterfaceDecl:
			g.interfaces[decl.Name] = true
		case *parser.DataClassDecl:
			g.dataClasses[decl.Name] = true
			g.funcSigs["New"+decl.Name] = fieldDeclsToParams(decl.Params)
		case *parser.ClassDecl:
			g.structs[decl.Name] = decl
			if decl.IsSealed {
				for _, v := range decl.Variants {
					g.dataClasses[v.Name] = true
					g.funcSigs["New"+v.Name] = fieldDeclsToParams(v.Params)
				}
			}
			if decl.Ctor != nil {
				g.funcSigs["New"+decl.Name] = decl.Ctor.Params
			} else if len(decl.Ctors) > 0 {
				g.funcSigs["New"+decl.Name] = decl.Ctors[0].Params
			}
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

// fieldDeclsToParams converts FieldDecl slice to ParamDecl slice for funcSigs.
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

// --- Error detection ---------------------------------------------------------

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

// --- Zero values and imports -------------------------------------------------

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
		return goType + "{}"
	}
}

// needImport records that a Go import is required.
func (g *Generator) needImport(pkg string) {
	g.imports[pkg] = true
}

// --- Code generation entry points --------------------------------------------

// Generate produces a single .go source file from a Zinc program.
func (g *Generator) Generate(prog *parser.Program, className string) string {
	g.buf.Reset()
	g.indent = 0
	g.className = className
	g.imports = make(map[string]bool)
	g.errorFuncs = make(map[string]bool)
	g.funcSigs = make(map[string][]*parser.ParamDecl)
	g.varTypes = make(map[string]string)
	g.varTypeExprs = make(map[string]parser.TypeExpr)
	g.varGoTypes = make(map[string]types.Type)
	g.varStructTypes = make(map[string]string)
	g.dataClasses = make(map[string]bool)
	g.typeImports = make(map[string]string)
	g.importGoAliases = make(map[string]string)
	g.collectDecls(prog.Decls)

	// Register user imports for resolution — but don't add to g.imports yet.
	// The codegen will call needImport() when it actually references a package,
	// so only used imports appear in the output.
	for _, imp := range prog.Imports {
		parts := strings.Split(imp.Path, ".")
		lastSeg := parts[len(parts)-1]

		// Determine the local alias name for this import
		// If "import X as Y" was used, alias is Y; otherwise it's the last path segment
		localName := lastSeg
		if imp.Alias != "" {
			localName = imp.Alias
		}

		// Check import aliases from zinc.toml: "import stdlib.config" where stdlib → "github.com/..."
		if len(parts) >= 2 && g.importAliases != nil {
			if modulePath, ok := g.importAliases[parts[0]]; ok {
				subPath := strings.Join(parts[1:], "/")
				goPath := modulePath + "/" + subPath
				g.importMap[localName] = goPath
				// Register Go import alias if localName differs from Go package name
				goPkgName := parts[len(parts)-1]
				if localName != goPkgName {
					g.importGoAliases[goPath] = localName
				}
				continue
			}
		}

		// Check if this is a zinc subpackage import.
		subpkgPath := strings.ReplaceAll(imp.Path, ".", "/")
		if g.zincSubpackages[subpkgPath] {
			goPath := subpkgPath
			if g.moduleName != "" {
				goPath = g.moduleName + "/" + subpkgPath
			}
			g.importMap[localName] = goPath
			// Register Go import alias if localName differs from directory name
			dirName := parts[len(parts)-1]
			if localName != dirName {
				g.importGoAliases[goPath] = localName
			}
			continue
		}

		if len(parts) >= 2 && len(lastSeg) > 0 && lastSeg[0] >= 'A' && lastSeg[0] <= 'Z' {
			// Type import: sync.Mutex → register Mutex → sync.Mutex
			pkgParts := parts[:len(parts)-1]
			goPath := strings.Join(pkgParts, "/")
			goPkg := pkgParts[len(pkgParts)-1]
			typeName := lastSeg
			g.typeImports[typeName] = goPkg + "." + typeName
			g.importMap[goPkg] = goPath
		} else {
			// Package import: net.http → import "net/http"
			goPath := strings.ReplaceAll(imp.Path, ".", "/")
			g.importMap[localName] = goPath
			// Register Go import alias if localName differs from last path segment
			goLastSeg := parts[len(parts)-1]
			if localName != goLastSeg {
				g.importGoAliases[goPath] = localName
			}
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
	g.importGoAliases = bodyGen.importGoAliases

	// Write final output: package + imports + body
	pkgName := g.packageName
	if pkgName == "" {
		pkgName = "main"
	}
	g.writeln("package %s", pkgName)
	g.writeln("")

	if len(g.imports) > 0 {
		g.writeln("import (")
		g.indent++
		for pkg := range g.imports {
			if alias, ok := g.importGoAliases[pkg]; ok {
				g.writeln("%s %q", alias, pkg)
			} else {
				g.writeln("%q", pkg)
			}
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
	content := g.Generate(prog, className)
	outName := strings.ToLower(className) + ".go"
	if strings.HasSuffix(outName, "_test.go") {
		outName = strings.TrimSuffix(outName, "_test.go") + "_main.go"
	}
	return []OutputFile{{Name: outName, Content: content}}
}

// --- Declaration dispatch ----------------------------------------------------

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

// goTypeParams returns the Go type parameter clause, e.g. "[T any, U any]".
// Returns "" when params is empty.
func goTypeParams(params []string) string {
	if len(params) == 0 {
		return ""
	}
	var parts []string
	for _, p := range params {
		parts = append(parts, p+" any")
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// goTypeArgs returns the Go type argument clause, e.g. "[T, U]".
// Returns "" when params is empty.
func goTypeArgs(params []string) string {
	if len(params) == 0 {
		return ""
	}
	return "[" + strings.Join(params, ", ") + "]"
}

// formatType converts a Zinc type expression to its Go type string.
func (g *Generator) formatType(t parser.TypeExpr) string {
	switch typ := t.(type) {
	case *parser.SimpleType:
		// If it's an active generic type parameter, keep as-is
		if g.activeTypeParams[typ.Name] {
			return typ.Name
		}
		if mapped, ok := zincToGoType[typ.Name]; ok {
			return mapped
		}
		if _, ok := g.typeAliases[typ.Name]; ok {
			return typ.Name
		}
		if qualified, ok := g.typeImports[typ.Name]; ok {
			// Add the package import for this type reference
			pkgPrefix := strings.SplitN(qualified, ".", 2)[0]
			if goPath, ok := g.importMap[pkgPrefix]; ok {
				g.needImport(goPath)
			}
			return qualified
		}
		// Zinc subpackage qualified type: core.FlowFile → add import for core
		// Also handles nested: router.RulesEngine → add import for fabric/router
		if strings.Contains(typ.Name, ".") {
			pkgPrefix := strings.SplitN(typ.Name, ".", 2)[0]
			if g.isZincSubpackage(pkgPrefix) {
				if goPath, ok := g.importMap[pkgPrefix]; ok {
					g.needImport(goPath)
				}
			}
		}
		// Classes (non-data, non-sealed) are always pointers in Go.
		// Sealed classes and interfaces are Go interfaces — no pointer.
		if cls, isStruct := g.structs[typ.Name]; isStruct {
			if !g.dataClasses[typ.Name] && !cls.IsSealed {
				return "*" + typ.Name
			}
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

// goReturnTypeStr returns the Go type string for a return type.
func (g *Generator) goReturnTypeStr(retType parser.TypeExpr) string {
	if retType == nil {
		return ""
	}
	return g.formatType(retType)
}

// formatReturnType builds the Go return type string (with leading space).
func (g *Generator) formatReturnType(retType parser.TypeExpr, body *parser.BlockStmt) string {
	if retType == nil {
		return ""
	}
	return " " + g.formatType(retType)
}

// formatParams formats function parameters as a Go parameter list.
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

// formatExprList formats a slice of expressions as a comma-separated string.
func (g *Generator) formatExprList(exprs []parser.Expr) string {
	var parts []string
	for _, e := range exprs {
		parts = append(parts, g.formatExpr(e))
	}
	return strings.Join(parts, ", ")
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
