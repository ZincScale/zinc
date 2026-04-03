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
	currentFields      map[string]bool   // field names of current class (for implicit self)
	currentFieldGoName map[string]string // zinc field name → Go field name (respects pub)
	currentMethods map[string]bool // method names of current class (for implicit self)
	currentParams  map[string]bool // parameter names (shadow field names)
	currentClass   string          // current class name (for pub member lookups)

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

	// Visibility tracking
	pubNames         map[string]bool   // names declared with pub (functions, methods, fields, consts)

	// Subpackage support
	packageName      string            // Go package name (default: "main")
	moduleName       string            // Go module name from zinc.toml (for subpackage import paths)
	zincSubpackages  map[string]bool   // known zinc subpackage names (directory names in src/)
	subpkgExports    map[string]map[string]string // pkg → name → kind ("data", "class", "func", "interface")
	subpkgDataFields map[string]map[string][]*parser.FieldDecl // pkg → data class name → field params
	importAliases    map[string]string // import alias → Go module path (e.g. "stdlib" → "github.com/ZincScale/zinc-stdlib")
	importGoAliases  map[string]string // Go import path → local alias (when alias differs from package name)

	// Unqualified import resolution: bare name → package + kind
	// Built from subpkgExports after import processing. Allows writing
	// Processor instead of lib.Processor when import lib is declared.
	unqualifiedNames map[string]unqualifiedEntry
}

// Name resolution, type formatting, and visibility helpers are in codegen_resolve.go.

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
		pubNames:            make(map[string]bool),
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

// SetGoModDir sets the directory containing go.mod for module dependency resolution.
func (g *Generator) SetGoModDir(dir string) {
	g.goResolver.SetDir(dir)
}

// SetImportAliases sets the import alias → module path mappings from zinc.toml [imports].
func (g *Generator) SetImportAliases(aliases map[string]string) {
	g.importAliases = aliases
}

// SetSiblingExports registers names from sibling files in the same package.
// These are types, functions, etc. declared in other .zn files in the same directory.
// Go handles cross-file visibility natively within a package, but the codegen
// needs this for constructor name resolution and export capitalization decisions.
func (g *Generator) SetSiblingExports(exports map[string]string) {
	for name, kind := range exports {
		switch kind {
		case "data":
			g.dataClasses[name] = true
		case "class":
			// Mark as known struct with a placeholder ClassDecl (not nil)
			// so codegen can resolve constructor calls (NewType) and pointer types.
			g.structs[name] = &parser.ClassDecl{Name: name}
		case "interface":
			g.interfaces[name] = true
		}
		g.pubNames[name] = true // siblings in same package are always visible
	}
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
			if decl.IsSealed {
				exports[decl.Name] = "interface" // sealed classes are Go interfaces
				for _, v := range decl.Variants {
					exports[v.Name] = "data"
				}
			} else {
				exports[decl.Name] = "class"
			}
		case *parser.InterfaceDecl:
			exports[decl.Name] = "interface"
		case *parser.FnDecl:
			if decl.Name != "main" {
				exports[decl.Name] = "func"
			}
		case *parser.EnumDecl:
			exports[decl.Name] = "enum"
			for _, v := range decl.Variants {
				exports[v] = "enum_variant"
			}
		case *parser.ConstDecl:
			exports[decl.Name] = "const"
		case *parser.TypeAliasDecl:
			exports[decl.Name] = "type"
		}
	}
	return exports
}

// CollectDataClassFields returns data class field declarations for cross-package
// match destructuring. Keys are data class names, values are their ordered params.
func CollectDataClassFields(prog *parser.Program) map[string][]*parser.FieldDecl {
	fields := make(map[string][]*parser.FieldDecl)
	for _, d := range prog.Decls {
		switch decl := d.(type) {
		case *parser.DataClassDecl:
			fields[decl.Name] = decl.Params
		case *parser.ClassDecl:
			if decl.IsSealed {
				for _, v := range decl.Variants {
					fields[v.Name] = v.Params
				}
			}
		}
	}
	return fields
}

// SetSubpackageDataFields registers data class field info from a subpackage.
func (g *Generator) SetSubpackageDataFields(pkg string, fields map[string][]*parser.FieldDecl) {
	if g.subpkgDataFields == nil {
		g.subpkgDataFields = make(map[string]map[string][]*parser.FieldDecl)
	}
	g.subpkgDataFields[pkg] = fields
}

// RegisterInterface allows external callers to register interface names.
func (g *Generator) RegisterInterface(name string) {
	g.interfaces[name] = true
}

// --- Declaration scanning ----------------------------------------------------

// collectDecls scans declarations to build lookup tables for types,
// constructors, error functions, type aliases, and pub visibility.
func (g *Generator) collectDecls(decls []parser.TopLevelDecl) {
	for _, d := range decls {
		switch decl := d.(type) {
		case *parser.InterfaceDecl:
			g.interfaces[decl.Name] = true
			// Interface methods — track pub status
			for _, m := range decl.Methods {
				g.pubNames[decl.Name+"."+m.Name] = m.IsPub
			}
		case *parser.DataClassDecl:
			g.dataClasses[decl.Name] = true
			g.funcSigs["New"+decl.Name] = fieldDeclsToParams(decl.Params)
			// Data class fields — track pub status
			for _, f := range decl.Params {
				g.pubNames[decl.Name+"."+f.Name] = f.IsPub
			}
		case *parser.ClassDecl:
			g.structs[decl.Name] = decl
			if decl.IsSealed {
				g.interfaces[decl.Name] = true
				for _, v := range decl.Variants {
					g.dataClasses[v.Name] = true
					g.funcSigs["New"+v.Name] = fieldDeclsToParams(v.Params)
					for _, f := range v.Params {
						g.pubNames[v.Name+"."+f.Name] = f.IsPub
					}
				}
			}
			if decl.Ctor != nil {
				g.funcSigs["New"+decl.Name] = decl.Ctor.Params
			} else if len(decl.Ctors) > 0 {
				g.funcSigs["New"+decl.Name] = decl.Ctors[0].Params
			}
			// Class methods and fields — track pub status
			for _, m := range decl.Methods {
				g.pubNames[decl.Name+"."+m.Name] = m.IsPub
				key := decl.Name + "." + m.Name
				if canReturnError(m.Body) {
					g.errorFuncs[key] = true
				}
			}
			for _, f := range decl.Fields {
				g.pubNames[decl.Name+"."+f.Name] = f.IsPub
			}
		case *parser.TypeAliasDecl:
			g.typeAliases[decl.Name] = decl.Type
		case *parser.ConstDecl:
			g.pubNames[decl.Name] = decl.IsPub
		case *parser.FnDecl:
			g.pubNames[decl.Name] = decl.IsPub
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
	// Preserve dataClasses, interfaces, structs, and pubNames
	// pre-populated by SetSiblingExports (sibling file awareness).
	if g.dataClasses == nil {
		g.dataClasses = make(map[string]bool)
	}
	g.typeImports = make(map[string]string)
	if g.pubNames == nil {
		g.pubNames = make(map[string]bool)
	}
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

		// Check import aliases from zinc.toml [imports] section.
		// Handles both: "import viper" (direct alias) and "import stdlib.config" (prefix alias)
		if g.importAliases != nil {
			// Direct alias: import viper → viper = "github.com/spf13/viper"
			if modulePath, ok := g.importAliases[imp.Path]; ok {
				g.importMap[localName] = modulePath
				// Go package name is last segment of the module path
				goPkgName := modulePath[strings.LastIndex(modulePath, "/")+1:]
				if localName != goPkgName {
					g.importGoAliases[modulePath] = localName
				}
				continue
			}
			// Prefix alias: import stdlib.config → stdlib = "github.com/ZincScale/zinc-stdlib"
			if len(parts) >= 2 {
				if modulePath, ok := g.importAliases[parts[0]]; ok {
					subPath := strings.Join(parts[1:], "/")
					goPath := modulePath + "/" + subPath
					g.importMap[localName] = goPath
					goPkgName := parts[len(parts)-1]
					if localName != goPkgName {
						g.importGoAliases[goPath] = localName
					}
					continue
				}
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

	// Build unqualified name resolution from subpackage exports.
	// Allows writing Processor instead of lib.Processor.
	g.buildUnqualifiedNames(prog)

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

// Name helpers (exportName, goName, isPub, etc.) are in codegen_resolve.go.
