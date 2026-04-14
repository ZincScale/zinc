package codegen_go

// Name resolution, type formatting, import tracking, and visibility helpers.
// This file centralizes all logic for mapping Zinc names/types to Go equivalents.

import (
	"fmt"
	"os"
	"strings"

	"zinc-go/internal/parser"
)

// --- Unqualified name resolution ---------------------------------------------

// unqualifiedEntry maps a bare zinc name to its source package.
type unqualifiedEntry struct {
	pkg  string // Go package alias (e.g. "lib", "router", "core")
	name string // zinc name (e.g. "Item", "formatItem", "EQ")
	kind string // "data", "class", "func", "interface", "enum", "enum_variant", "const", "type", "sealed_variant"
}

// goBuiltinNames are Go predeclared identifiers that must never be shadowed
// by unqualified imports.
var goBuiltinNames = map[string]bool{
	"error": true, "any": true, "comparable": true,
	"bool": true, "byte": true, "rune": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "float32": true, "float64": true,
	"complex64": true, "complex128": true, "string": true,
	"len": true, "cap": true, "make": true, "new": true, "append": true,
	"copy": true, "delete": true, "close": true, "panic": true, "recover": true,
	"complex": true, "real": true, "imag": true, "print": true, "println": true,
	"true": true, "false": true, "nil": true, "iota": true,
	"min": true, "max": true, "clear": true,
}

// addUnqualified adds a name to the unqualified map, handling collisions.
func addUnqualified(names map[string]unqualifiedEntry, collisions map[string][]string,
	localNames map[string]bool, pkg, name, kind string) {
	if localNames[name] {
		return // local declaration shadows import
	}
	if goBuiltinNames[name] {
		return // never shadow Go builtins
	}
	if existing, exists := names[name]; exists {
		// Collision: same name from different packages
		collisions[name] = []string{existing.pkg, pkg}
		delete(names, name)
		return
	}
	if pkgs, ok := collisions[name]; ok {
		// Already collided — track additional package
		collisions[name] = append(pkgs, pkg)
		return
	}
	names[name] = unqualifiedEntry{pkg: pkg, name: name, kind: kind}
}

// buildUnqualifiedNames populates the reverse lookup map from all imports.
// Covers zinc subpackages, Go stdlib, and external dependencies.
// Names that collide across packages are excluded (require qualified form).
// Local declarations and Go builtins shadow imported names.
func (g *Generator) buildUnqualifiedNames(prog *parser.Program) {
	g.unqualifiedNames = make(map[string]unqualifiedEntry)

	// Collect local declaration names to avoid shadowing
	localNames := make(map[string]bool)
	for _, d := range prog.Decls {
		switch decl := d.(type) {
		case *parser.FnDecl:
			localNames[decl.Name] = true
		case *parser.ClassDecl:
			localNames[decl.Name] = true
			if decl.IsSealed {
				for _, v := range decl.Variants {
					localNames[v.Name] = true
				}
			}
		case *parser.DataClassDecl:
			localNames[decl.Name] = true
		case *parser.InterfaceDecl:
			localNames[decl.Name] = true
		case *parser.EnumDecl:
			localNames[decl.Name] = true
			for _, v := range decl.Variants {
				localNames[v] = true
			}
		case *parser.ConstDecl:
			localNames[decl.Name] = true
		case *parser.TypeAliasDecl:
			localNames[decl.Name] = true
		}
	}

	// Determine which packages are actually imported
	importedPkgs := make(map[string]bool)
	for _, imp := range prog.Imports {
		parts := strings.Split(imp.Path, ".")
		alias := parts[len(parts)-1]
		if imp.Alias != "" {
			alias = imp.Alias
		}
		importedPkgs[alias] = true
	}

	collisions := make(map[string][]string)

	// 1. Zinc subpackage exports (already collected by project compilation)
	for pkg, exports := range g.subpkgExports {
		if !importedPkgs[pkg] {
			continue
		}
		for name, kind := range exports {
			addUnqualified(g.unqualifiedNames, collisions, localNames, pkg, name, kind)
		}
	}

	// 2. Go stdlib and external dependency exports (via GoTypeResolver)
	for alias, goPath := range g.importMap {
		if !importedPkgs[alias] {
			continue
		}
		// Skip zinc subpackages (already handled above)
		if g.zincSubpackages != nil && g.zincSubpackages[alias] {
			continue
		}
		// Also skip zinc subpackages resolved via module path
		isZincPkg := false
		if g.moduleName != "" && strings.HasPrefix(goPath, g.moduleName+"/") {
			subPath := goPath[len(g.moduleName)+1:]
			if g.zincSubpackages[subPath] {
				isZincPkg = true
			}
		}
		if isZincPkg {
			continue
		}
		// Introspect Go package exports
		exports := g.goResolver.ListExports(goPath)
		for name, kind := range exports {
			addUnqualified(g.unqualifiedNames, collisions, localNames, alias, name, kind)
		}
	}

	// Store collisions for error reporting
	g.unqualifiedCollisions = collisions
}

// resolveUnqualifiedType checks if a bare type name is from an imported package.
// Returns the qualified Go type string and true, or "" and false.
func (g *Generator) resolveUnqualifiedType(name string) (string, bool) {
	entry, ok := g.unqualifiedNames[name]
	if !ok {
		return "", false
	}
	// Add the Go import
	if goPath, ok := g.importMap[entry.pkg]; ok {
		g.needImport(goPath)
	}
	// Go types keep their original name (no exportName transform)
	goName := exportName(entry.name)
	if entry.kind == "type" || entry.kind == "func" || entry.kind == "var" || entry.kind == "const" {
		goName = entry.name // Go exports are already correctly cased
	}
	qualified := entry.pkg + "." + goName
	if entry.kind == "class" {
		return "*" + qualified, true
	}
	// For Go stdlib types: check if it's a struct with pointer-receiver methods
	// (e.g. http.Request should be *http.Request)
	if goPath, ok := g.importMap[entry.pkg]; ok {
		if g.goResolver.IsStruct(goPath, goName) &&
			g.goResolver.HasPointerReceiverMethods(goPath, goName) {
			return "*" + qualified, true
		}
	}
	return qualified, true
}

// resolveTypeArg resolves a raw type argument string (from CallExpr.TypeArgs)
// to its Go type. Checks zincToGoType first, then unqualified imports.
// Handles generic type args like "List<int>" or "Map<String, Box<int>>" by
// recursively resolving their nested type parameters.
func (g *Generator) resolveTypeArg(ta string) string {
	ta = strings.TrimSpace(ta)
	// Generic type: Name<args>
	if ltIdx := strings.Index(ta, "<"); ltIdx > 0 && strings.HasSuffix(ta, ">") {
		name := ta[:ltIdx]
		inner := ta[ltIdx+1 : len(ta)-1]
		argParts := splitTopLevelTypeArgs(inner)
		goArgs := make([]string, 0, len(argParts))
		for _, a := range argParts {
			goArgs = append(goArgs, g.resolveTypeArg(a))
		}
		switch name {
		case "List":
			if len(goArgs) > 0 {
				return "[]" + goArgs[0]
			}
			return "[]interface{}"
		case "Map":
			if len(goArgs) >= 2 {
				return "map[" + goArgs[0] + "]" + goArgs[1]
			}
			return "map[string]interface{}"
		case "Set":
			if len(goArgs) > 0 {
				return "map[" + goArgs[0] + "]struct{}"
			}
			return "map[interface{}]struct{}"
		case "Channel", "Chan":
			if len(goArgs) > 0 {
				return "chan " + goArgs[0]
			}
			return "chan interface{}"
		default:
			// User-defined generic class: *ClassName[goArgs...] when the class
			// is a non-data non-sealed class (classes are pointer types in Zinc's
			// Go backend; constructors return *T). Sealed classes + data classes
			// + interfaces stay as values.
			goName := name
			ptrPrefix := ""
			if resolved, ok := g.resolveUnqualifiedType(name); ok {
				// resolveUnqualifiedType already applies * for pointer-typed classes
				if strings.HasPrefix(resolved, "*") {
					ptrPrefix = "*"
					goName = strings.TrimPrefix(resolved, "*")
				} else {
					goName = resolved
				}
			} else if cls, isStruct := g.structs[name]; isStruct {
				// Same-package class: check if it's a pointer-typed class
				if !g.dataClasses[name] && cls != nil && !cls.IsSealed {
					ptrPrefix = "*"
				}
			}
			return ptrPrefix + goName + "[" + strings.Join(goArgs, ", ") + "]"
		}
	}
	if mapped, ok := zincToGoType[ta]; ok {
		return mapped
	}
	if resolved, ok := g.resolveUnqualifiedType(ta); ok {
		return resolved
	}
	// Check if unresolved due to collision across imported packages
	if pkgs, ok := g.unqualifiedCollisions[ta]; ok {
		fmt.Fprintf(os.Stderr, "error: ambiguous type %q — exported by multiple imports: %s. Use qualified form (e.g. %s.%s)\n",
			ta, strings.Join(pkgs, ", "), pkgs[0], ta)
	}
	return ta
}

// splitTopLevelTypeArgs splits a type-argument string on top-level commas,
// respecting nested angle brackets. e.g. "String, Map<K, V>, int" →
// ["String", "Map<K, V>", "int"].
func splitTopLevelTypeArgs(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

// resolveUnqualifiedExpr checks if a bare identifier is from an imported package.
// Returns the qualified Go expression string and true, or "" and false.
func (g *Generator) resolveUnqualifiedExpr(name string) (string, bool) {
	entry, ok := g.unqualifiedNames[name]
	if !ok {
		return "", false
	}
	if goPath, ok := g.importMap[entry.pkg]; ok {
		g.needImport(goPath)
	}
	// Go exports keep their original casing
	goName := exportName(entry.name)
	if entry.kind == "type" || entry.kind == "func" || entry.kind == "var" || entry.kind == "const" {
		goName = entry.name
	}
	return entry.pkg + "." + goName, true
}

// resolveParentType resolves a parent type name for struct embedding.
// Handles both qualified (lib.Base) and unqualified (Base) forms.
func (g *Generator) resolveParentType(name string) string {
	if strings.Contains(name, ".") {
		// Already qualified
		pkg := strings.SplitN(name, ".", 2)[0]
		if goPath, ok := g.importMap[pkg]; ok {
			g.needImport(goPath)
		}
		return name
	}
	// Try unqualified resolution
	if entry, ok := g.unqualifiedNames[name]; ok {
		if goPath, ok := g.importMap[entry.pkg]; ok {
			g.needImport(goPath)
		}
		return entry.pkg + "." + exportName(entry.name)
	}
	return name
}

// isImportedInterface checks if a parent type name is an interface from an imported package.
func (g *Generator) isImportedInterface(name string) bool {
	// Qualified: lib.Processor
	if strings.Contains(name, ".") {
		pkg := strings.SplitN(name, ".", 2)[0]
		typeName := strings.SplitN(name, ".", 2)[1]
		if exports, ok := g.subpkgExports[pkg]; ok {
			return exports[typeName] == "interface"
		}
	}
	// Unqualified: Processor → check unqualifiedNames
	if entry, ok := g.unqualifiedNames[name]; ok {
		return entry.kind == "interface"
	}
	return false
}

// isLocalVar checks if a name is a local variable, parameter, or field (should not be import-resolved).
func (g *Generator) isLocalVar(name string) bool {
	if g.currentParams != nil && g.currentParams[name] {
		return true
	}
	if g.currentFields != nil && g.currentFields[name] {
		return true
	}
	if _, ok := g.varTypes[name]; ok {
		return true
	}
	return false
}

// --- Package/visibility helpers ----------------------------------------------

// resolveReceiverGenericType returns the GenericType of a receiver
// expression (e.g., the `foo` in `foo.keys()` or `foo.values()`), or nil
// if none is known. Handles:
//
//   - local variables tracked in varTypeExprs
//   - current-class fields with an explicit generic type annotation, or
//     with a MapLit/ListLit default-value that carries ExplicitType
//   - map-index expressions: given `m[k]` where m is Map<K,V>, returns V
//     if V is itself a GenericType (the common case for nested containers)
//
// Motivation: the map-method rewrites for `.keys()` / `.values()` need
// the typed K/V to emit `[]K` / `[]V` instead of `[]interface{}`. For
// receivers that aren't simple locals — class fields, nested index
// expressions — plain-name varTypeExprs lookup doesn't hit (ZCA-11).
func (g *Generator) resolveReceiverGenericType(e parser.Expr) *parser.GenericType {
	// Map-index expression: `m[k].method(...)`. Walk to the map's V type.
	if idx, ok := e.(*parser.IndexExpr); ok {
		if outer := g.resolveReceiverGenericType(idx.Object); outer != nil &&
			outer.Name == "Map" && len(outer.TypeArgs) >= 2 {
			if gt, ok := outer.TypeArgs[1].(*parser.GenericType); ok {
				return gt
			}
		}
		return nil
	}

	ident, ok := e.(*parser.Ident)
	if !ok {
		return nil
	}
	// Local variable with a tracked type expression.
	if te, ok := g.varTypeExprs[ident.Name]; ok {
		if gt, ok := te.(*parser.GenericType); ok {
			return gt
		}
	}
	// Current-class field.
	if g.currentClass != "" && g.currentFields[ident.Name] {
		if cls, ok := g.structs[g.currentClass]; ok {
			for _, f := range cls.Fields {
				if f.Name != ident.Name {
					continue
				}
				if gt, ok := f.Type.(*parser.GenericType); ok {
					return gt
				}
				// `var x = Map<...>{}` — explicit type lives on the MapLit.
				if f.Default != nil {
					if mapLit, ok := f.Default.(*parser.MapLit); ok {
						if gt, ok := mapLit.ExplicitType.(*parser.GenericType); ok {
							return gt
						}
					}
					if listLit, ok := f.Default.(*parser.ListLit); ok {
						if gt, ok := listLit.ExplicitType.(*parser.GenericType); ok {
							return gt
						}
					}
				}
			}
		}
	}
	return nil
}

// isUserScopeShadow returns true when `name` is shadowed by user scope —
// a current-class field, a method parameter, or a tracked local variable.
// Callers that would otherwise interpret `name` as an imported package,
// zinc subpackage, or import alias must consult this first, otherwise a
// user who names a field/var/param after a sibling package gets their
// code misresolved (see ZCA-10 for the original repro).
//
// Normal scoping rule: inner scope shadows outer. Zinc's codegen flattens
// resolution tables across the project, so we need an explicit guard at
// every place the tables are consulted with a user-typed identifier.
func (g *Generator) isUserScopeShadow(name string) bool {
	if g.currentClass != "" && g.currentFields[name] {
		return true
	}
	if g.currentParams != nil && g.currentParams[name] {
		return true
	}
	if _, ok := g.varStructTypes[name]; ok {
		return true
	}
	if _, ok := g.varTypes[name]; ok {
		return true
	}
	if _, ok := g.varGoTypes[name]; ok {
		return true
	}
	if _, ok := g.varTypeExprs[name]; ok {
		return true
	}
	return false
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

// exportIfSubpackage uppercases the first letter of a name when generating
// a subpackage (non-main). In Go, only uppercase names are exported.
func (g *Generator) exportIfSubpackage(name string) string {
	if g.isSubpackage() {
		return goName(name, g.isPub(name))
	}
	return name
}

// isSubpackage returns true if generating code for a non-main package.
func (g *Generator) isSubpackage() bool {
	return g.packageName != "" && g.packageName != "main"
}

// isPub checks if a name was declared with pub.
// For qualified names like "ClassName.methodName", checks the full key.
func (g *Generator) isPub(name string) bool {
	if pub, ok := g.pubNames[name]; ok {
		return pub
	}
	// In main package, everything is accessible (no export needed)
	return g.packageName == "" || g.packageName == "main"
}

// isPubField checks if a field/method on a class is pub.
func (g *Generator) isPubMember(className, memberName string) bool {
	key := className + "." + memberName
	if pub, ok := g.pubNames[key]; ok {
		return pub
	}
	// Default: in main package everything is accessible
	return g.packageName == "" || g.packageName == "main"
}

// --- Name formatting ---------------------------------------------------------

// exportName capitalizes the first letter to make it exported in Go.
// Used for identifiers that are always exported (data class fields, constructors, etc.)
func exportName(name string) string {
	if name == "" {
		return ""
	}
	if name[0] >= 'A' && name[0] <= 'Z' {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// goName returns the Go name for a zinc identifier, respecting pub visibility.
// pub → exported (capitalized), non-pub → unexported (lowercase).
func goName(name string, isPub bool) string {
	if name == "" {
		return ""
	}
	if isPub {
		return exportName(name)
	}
	// Ensure lowercase for unexported
	if name[0] >= 'A' && name[0] <= 'Z' {
		return strings.ToLower(name[:1]) + name[1:]
	}
	return name
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
		// Unqualified import: bare name like Processor → lib.Processor
		if resolved, ok := g.resolveUnqualifiedType(typ.Name); ok {
			return resolved
		}
		// Zinc subpackage qualified type: core.FlowFile → add import for core
		// Also handles nested: router.RulesEngine → add import for fabric/router
		if strings.Contains(typ.Name, ".") {
			pkgPrefix := strings.SplitN(typ.Name, ".", 2)[0]
			typeName := strings.SplitN(typ.Name, ".", 2)[1]
			_ = typeName // used in subpackage/alias checks below
			// Ensure the package is imported for any qualified type reference
			if goPath, ok := g.importMap[pkgPrefix]; ok {
				g.needImport(goPath)
			}
			if g.isZincSubpackage(pkgPrefix) {
				if goPath, ok := g.importMap[pkgPrefix]; ok {
					g.needImport(goPath)
				}
				// Check if it's a class (not data class) from a subpackage → needs pointer
				if exports, ok := g.subpkgExports[pkgPrefix]; ok {
					if exports[typeName] == "class" {
						return "*" + typ.Name
					}
				}
			}
			// Check import aliases — if it's a class from an external zinc package
			if g.isImportAlias(pkgPrefix) {
				if goPath, ok := g.importMap[pkgPrefix]; ok {
					g.needImport(goPath)
					if g.goResolver.IsStruct(goPath, typeName) {
						if g.goResolver.HasFunc(goPath, "New"+typeName) {
							return "*" + typ.Name
						}
					}
				}
			}
			// Check any Go package — if the type is a struct with pointer-receiver
			// methods, it's designed to be used as *T.
			// Non-struct types (type Level int) stay as values.
			if goPath, ok := g.importMap[pkgPrefix]; ok {
				if g.goResolver.IsStruct(goPath, typeName) &&
					g.goResolver.HasPointerReceiverMethods(goPath, typeName) {
					return "*" + typ.Name
				}
			}
		}
		// Classes (non-data, non-sealed) are always pointers in Go.
		// Sealed classes and interfaces are Go interfaces — no pointer.
		if cls, isStruct := g.structs[typ.Name]; isStruct {
			if !g.dataClasses[typ.Name] && cls != nil && !cls.IsSealed {
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
			baseName := typ.Name
			// Unqualified import: bare generic like Box<T> → core.Box[T]
			if entry, ok := g.unqualifiedNames[baseName]; ok {
				if goPath, ok := g.importMap[entry.pkg]; ok {
					g.needImport(goPath)
				}
				baseName = entry.pkg + "." + exportName(entry.name)
			}
			// Pointer prefix for user-defined classes (non-data, non-sealed).
			// Mirrors the SimpleType branch above — classes are pointer types
			// in Zinc's Go backend, so a generic class used as a nested type
			// arg must be emitted as *ClassName[args] too.
			ptrPrefix := ""
			if cls, isStruct := g.structs[typ.Name]; isStruct {
				if !g.dataClasses[typ.Name] && cls != nil && !cls.IsSealed {
					ptrPrefix = "*"
				}
			}
			return fmt.Sprintf("%s%s[%s]", ptrPrefix, baseName, strings.Join(args, ", "))
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

// --- Import tracking ---------------------------------------------------------

// needImport records that a Go import is required.
func (g *Generator) needImport(pkg string) {
	g.imports[pkg] = true
}

// --- Zero values -------------------------------------------------------------

// zeroValueFor returns the Go zero value for a given type string.
// Interface types get "nil" since Go interfaces cannot be instantiated with {}.
func (g *Generator) zeroValueFor(goType string) string {
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
		// Interface types cannot be instantiated — use nil
		if g.interfaces[goType] || g.isImportedInterface(goType) {
			return "nil"
		}
		return goType + "{}"
	}
}

// List/map literal type inference helpers are in codegen_stmts.go.
