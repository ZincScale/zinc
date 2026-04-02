package codegen_go

// Type declarations: functions, classes, data classes, sealed classes,
// enums, interfaces, and constants.

import (
	"fmt"
	"strings"

	"zinc-go/internal/parser"
)

// emitFnDecl generates a top-level Go function from a Zinc fn declaration.
func (g *Generator) emitFnDecl(fn *parser.FnDecl) {
	if g.sourceFile != "" && fn.Line > 0 {
		g.writeln("//line %s:%d", g.sourceFile, fn.Line)
	}

	name := g.exportIfSubpackage(fn.Name)
	if fn.Name == "main" {
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

	// Set active type params for generic functions
	if len(fn.TypeParams) > 0 {
		g.activeTypeParams = make(map[string]bool)
		for _, tp := range fn.TypeParams {
			g.activeTypeParams[tp] = true
		}
	}

	params := g.formatParams(fn.Params)

	// Register parameter type expressions for type-aware codegen
	for _, p := range fn.Params {
		if genType, ok := p.Type.(*parser.GenericType); ok {
			g.varTypeExprs[p.Name] = genType
		}
	}

	g.writeln("func %s%s(%s)%s {", name, goTypeParams(fn.TypeParams), params, ret)
	g.indent++
	g.emitBlock(fn.Body)
	g.indent--
	g.writeln("}")

	g.currentReturnType = prevRetType
	g.currentReturnOptional = prevRetOpt
	g.errVarCount = prevErrCount
	if len(fn.TypeParams) > 0 {
		g.activeTypeParams = nil
	}
}

// --- Classes (structs with constructors and methods) -------------------------

// emitClassDecl generates a Go struct, constructor, and methods from a Zinc class.
func (g *Generator) emitClassDecl(cls *parser.ClassDecl) {
	if g.sourceFile != "" && cls.Line > 0 {
		g.writeln("//line %s:%d", g.sourceFile, cls.Line)
	}

	name := cls.Name

	// Set active type params for generic classes
	if len(cls.TypeParams) > 0 {
		g.activeTypeParams = make(map[string]bool)
		for _, tp := range cls.TypeParams {
			g.activeTypeParams[tp] = true
		}
	}

	// Struct definition
	g.writeln("type %s%s struct {", name, goTypeParams(cls.TypeParams))
	g.indent++

	// Embedded parent (first non-interface parent for inheritance)
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
		} else if f.Default != nil {
			if listLit, ok := f.Default.(*parser.ListLit); ok && listLit.ExplicitType != nil {
				typeName = g.formatType(listLit.ExplicitType)
			} else if mapLit, ok := f.Default.(*parser.MapLit); ok && mapLit.ExplicitType != nil {
				typeName = g.formatType(mapLit.ExplicitType)
			} else {
				typeName = g.inferFieldType(f.Default)
			}
		}
		g.writeln("%s %s", goName(f.Name, f.IsPub || !g.isSubpackage()), typeName)
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
		tpDecl := goTypeParams(cls.TypeParams)
		tpArgs := goTypeArgs(cls.TypeParams)
		g.writeln("func New%s%s() *%s%s {", name, tpDecl, name, tpArgs)
		g.indent++
		var litFields []string
		for _, f := range cls.Fields {
			if f.Default != nil {
				val := g.formatExpr(f.Default)
				// Use typed empty literal for typed fields: List<T> x = [] → []T{}, Map<K,V> x = {} → map[K]V{}
				if f.Type != nil {
					if _, isListLit := f.Default.(*parser.ListLit); isListLit {
						val = g.formatType(f.Type) + "{}"
					}
					if _, isMapLit := f.Default.(*parser.MapLit); isMapLit {
						val = g.formatType(f.Type) + "{}"
					}
				}
				litFields = append(litFields, fmt.Sprintf("%s: %s", goName(f.Name, f.IsPub || !g.isSubpackage()), val))
			}
		}
		nameTA := name + tpArgs
		if len(litFields) == 0 {
			g.writeln("return &%s{}", nameTA)
		} else if len(litFields) <= 3 {
			g.writeln("return &%s{%s}", nameTA, strings.Join(litFields, ", "))
		} else {
			g.writeln("return &%s{", nameTA)
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

	// Register field type expressions for type-aware codegen (e.g. map.keys())
	for _, f := range cls.Fields {
		fieldExpr := "s." + exportName(f.Name)
		if f.Type != nil {
			if genType, ok := f.Type.(*parser.GenericType); ok {
				g.varTypeExprs[fieldExpr] = genType
			}
		} else if f.Default != nil {
			if listLit, ok := f.Default.(*parser.ListLit); ok && listLit.ExplicitType != nil {
				g.varTypeExprs[fieldExpr] = listLit.ExplicitType
			} else if mapLit, ok := f.Default.(*parser.MapLit); ok && mapLit.ExplicitType != nil {
				g.varTypeExprs[fieldExpr] = mapLit.ExplicitType
			}
		}
	}

	// Methods
	for _, m := range cls.Methods {
		g.emitMethodDecl(name, m, cls.TypeParams)
		g.writeln("")
	}

	if len(cls.TypeParams) > 0 {
		g.activeTypeParams = nil
	}
}

// emitConstructor generates a NewType() constructor function.
// Handles super() calls, this.field assignments, and remaining logic.
func (g *Generator) emitConstructor(typeName string, ctor *parser.CtorDecl, cls *parser.ClassDecl) {
	// Set current fields/methods for implicit self resolution
	g.currentFields = make(map[string]bool)
	g.currentFieldGoName = make(map[string]string)
	g.currentMethods = make(map[string]bool)
	g.currentParams = make(map[string]bool)
	for _, f := range cls.Fields {
		g.currentFields[f.Name] = true
		g.currentFieldGoName[f.Name] = goName(f.Name, f.IsPub || !g.isSubpackage())
	}
	for _, method := range cls.Methods {
		g.currentMethods[method.Name] = true
	}
	for _, p := range ctor.Params {
		g.currentParams[p.Name] = true
	}
	defer func() { g.currentFields = nil; g.currentFieldGoName = nil; g.currentMethods = nil; g.currentParams = nil }()

	tpDecl := goTypeParams(cls.TypeParams)
	tpArgs := goTypeArgs(cls.TypeParams)
	params := g.formatParams(ctor.Params)
	g.writeln("func New%s%s(%s) *%s%s {", typeName, tpDecl, params, typeName, tpArgs)
	g.indent++

	// Extract field assignments from ctor body: this.field = value → Field: value
	var litFields []string
	var remainingStmts []parser.Stmt

	// Handle super() → embedded parent initialization
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
					fieldGoName := exportName(sel.Field) // default
					if gn, ok := g.currentFieldGoName[sel.Field]; ok {
						fieldGoName = gn
					}
					if _, isThis := sel.Object.(*parser.ThisExpr); isThis {
						litFields = append(litFields, fmt.Sprintf("%s: %s", fieldGoName, g.formatExpr(assign.Value)))
						continue
					}
					if ident, isIdent := sel.Object.(*parser.Ident); isIdent && ident.Name == "this" {
						litFields = append(litFields, fmt.Sprintf("%s: %s", fieldGoName, g.formatExpr(assign.Value)))
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

	// Add field defaults for fields not assigned in the constructor body
	assignedFields := make(map[string]bool)
	for _, lf := range litFields {
		// Extract field name from "FieldName: value"
		if idx := strings.Index(lf, ":"); idx > 0 {
			assignedFields[lf[:idx]] = true
		}
	}
	for _, f := range cls.Fields {
		if f.Default != nil && !assignedFields[exportName(f.Name)] {
			val := g.formatExpr(f.Default)
			if f.Type != nil {
				if _, isListLit := f.Default.(*parser.ListLit); isListLit {
					val = g.formatType(f.Type) + "{}"
				}
				if _, isMapLit := f.Default.(*parser.MapLit); isMapLit {
					val = g.formatType(f.Type) + "{}"
				}
			}
			fieldGoName := exportName(f.Name)
			if gn, ok := g.currentFieldGoName[f.Name]; ok {
				fieldGoName = gn
			}
			litFields = append(litFields, fmt.Sprintf("%s: %s", fieldGoName, val))
		}
	}

	// Emit struct literal
	typeNameTA := typeName + tpArgs
	if len(litFields) > 0 {
		if len(remainingStmts) == 0 {
			if len(litFields) <= 3 {
				g.writeln("return &%s{%s}", typeNameTA, strings.Join(litFields, ", "))
			} else {
				g.writeln("return &%s{", typeNameTA)
				g.indent++
				for _, f := range litFields {
					g.writeln("%s,", f)
				}
				g.indent--
				g.writeln("}")
			}
		} else {
			g.writeln("s := &%s{%s}", typeNameTA, strings.Join(litFields, ", "))
			for _, stmt := range remainingStmts {
				g.emitStmt(stmt)
			}
			g.writeln("return s")
		}
	} else if len(remainingStmts) > 0 {
		g.writeln("s := &%s{}", typeNameTA)
		for _, stmt := range remainingStmts {
			g.emitStmt(stmt)
		}
		g.writeln("return s")
	} else {
		g.writeln("return &%s{}", typeNameTA)
	}

	g.indent--
	g.writeln("}")
	g.writeln("")
}

// emitMethodDecl generates a method on a receiver struct.
// Maps Zinc method names to Go equivalents (toString → String, etc.).
// typeParams is optional — set when the receiver is a generic type.
func (g *Generator) emitMethodDecl(receiver string, m *parser.MethodDecl, typeParams ...[]string) {
	var tps []string
	if len(typeParams) > 0 {
		tps = typeParams[0]
	}
	// Set current fields/methods for implicit self resolution
	if cls, ok := g.structs[receiver]; ok {
		g.currentFields = make(map[string]bool)
		g.currentFieldGoName = make(map[string]string)
		g.currentMethods = make(map[string]bool)
		g.currentParams = make(map[string]bool)
		for _, f := range cls.Fields {
			g.currentFields[f.Name] = true
			g.currentFieldGoName[f.Name] = goName(f.Name, f.IsPub || !g.isSubpackage())
		}
		g.collectParentFields(cls, g.currentFields)
		for _, method := range cls.Methods {
			g.currentMethods[method.Name] = true
		}
		g.collectParentMethods(cls, g.currentMethods)
		for _, p := range m.Params {
			g.currentParams[p.Name] = true
		}
	}
	defer func() { g.currentFields = nil; g.currentFieldGoName = nil; g.currentMethods = nil; g.currentParams = nil }()

	methodKey := receiver + "." + m.Name
	canError := g.errorFuncs[methodKey]
	goRetType := g.goReturnTypeStr(m.ReturnType)

	// If the return type is a known class (not data class), return *Type
	// to match constructor return types (NewType() returns *Type).
	if simpleType, ok := m.ReturnType.(*parser.SimpleType); ok {
		if _, isStruct := g.structs[simpleType.Name]; isStruct {
			goRetType = "*" + simpleType.Name
		}
	}

	var ret string
	if canError {
		if goRetType == "" {
			ret = " error"
		} else {
			ret = fmt.Sprintf(" (%s, error)", goRetType)
		}
	} else if simpleType, ok := m.ReturnType.(*parser.SimpleType); ok {
		if _, isStruct := g.structs[simpleType.Name]; isStruct {
			ret = " *" + simpleType.Name
		} else {
			ret = g.formatReturnType(m.ReturnType, m.Body)
		}
	} else {
		ret = g.formatReturnType(m.ReturnType, m.Body)
	}

	prevRetType := g.currentReturnType
	prevMethodRetType := g.currentMethodRetType
	if canError {
		g.currentReturnType = goRetType
	}
	g.currentMethodRetType = goRetType

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

	receiverTA := receiver + goTypeArgs(tps)
	methodPub := m.IsPub || !g.isSubpackage()
	if m.IsStatic {
		name := receiver + goName(goMethodName, methodPub)
		params := g.formatParams(m.Params)
		g.writeln("func %s(%s)%s {", name, params, ret)
	} else {
		vis := goName(goMethodName, methodPub)
		params := g.formatParams(m.Params)
		g.writeln("func (s *%s) %s(%s)%s {", receiverTA, vis, params, ret)
	}
	g.indent++
	g.emitBlock(m.Body)
	g.indent--
	g.writeln("}")

	g.currentReturnType = prevRetType
	g.currentMethodRetType = prevMethodRetType
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

// --- Data classes (record types with auto-generated String()) -----------------

func (g *Generator) emitDataClassDecl(d *parser.DataClassDecl) {
	if g.sourceFile != "" && d.Line > 0 {
		g.writeln("//line %s:%d", g.sourceFile, d.Line)
	}

	// Set active type params for generic data classes
	if len(d.TypeParams) > 0 {
		g.activeTypeParams = make(map[string]bool)
		for _, tp := range d.TypeParams {
			g.activeTypeParams[tp] = true
		}
	}

	tpDecl := goTypeParams(d.TypeParams)
	tpArgs := goTypeArgs(d.TypeParams)
	nameTA := d.Name + tpArgs

	g.writeln("type %s%s struct {", d.Name, tpDecl)
	g.indent++
	for _, f := range d.Params {
		typeName := "interface{}"
		if f.Type != nil {
			typeName = g.formatType(f.Type)
		}
		// Data class fields are always exported (data = transparent DTO)
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
	g.writeln("func New%s%s(%s) %s {", d.Name, tpDecl, strings.Join(params, ", "), nameTA)
	g.indent++
	g.writeln("return %s{%s}", nameTA, strings.Join(assignments, ", "))
	g.indent--
	g.writeln("}")
	g.writeln("")

	// String() method
	g.needImport("fmt")
	var fmtParts []string
	var fmtArgs []string
	for _, f := range d.Params {
		fmtParts = append(fmtParts, f.Name+"=%v")
		fmtArgs = append(fmtArgs, "s."+exportName(f.Name))
	}
	g.writeln("func (s %s) String() string {", nameTA)
	g.indent++
	g.writeln("return fmt.Sprintf(\"%s(%s)\", %s)", d.Name, strings.Join(fmtParts, ", "), strings.Join(fmtArgs, ", "))
	g.indent--
	g.writeln("}")

	// Methods
	for _, m := range d.Methods {
		g.writeln("")
		g.emitMethodDecl(d.Name, m, d.TypeParams)
	}

	if len(d.TypeParams) > 0 {
		g.activeTypeParams = nil
	}
}

// --- Sealed classes (algebraic data types) -----------------------------------

// emitSealedDecl generates a Go interface with a private marker method,
// plus variant data classes that implement it.
func (g *Generator) emitSealedDecl(cls *parser.ClassDecl) {
	g.writeln("type %s interface {", cls.Name)
	g.indent++
	g.writeln("is%s()", cls.Name)
	g.indent--
	g.writeln("}")
	g.writeln("")

	for _, v := range cls.Variants {
		g.emitDataClassDecl(v)
		g.writeln("")
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
	// Set active type params for generic interfaces
	if len(iface.TypeParams) > 0 {
		g.activeTypeParams = make(map[string]bool)
		for _, tp := range iface.TypeParams {
			g.activeTypeParams[tp] = true
		}
	}

	g.writeln("type %s%s interface {", iface.Name, goTypeParams(iface.TypeParams))
	g.indent++
	for _, m := range iface.Methods {
		ret := ""
		if m.ReturnType != nil {
			ret = " " + g.formatType(m.ReturnType)
		}
		params := g.formatParams(m.Params)
		g.writeln("%s(%s)%s", goName(m.Name, m.IsPub || !g.isSubpackage()), params, ret)
	}
	g.indent--
	g.writeln("}")

	if len(iface.TypeParams) > 0 {
		g.activeTypeParams = nil
	}
}

// --- Field type inference -----------------------------------------------------

// inferFieldType infers the Go type for a class field from its initializer expression.
// Handles Channel() → chan interface{}, literals, and constructor calls.
func (g *Generator) inferFieldType(expr parser.Expr) string {
	if call, ok := expr.(*parser.CallExpr); ok {
		if ident, ok := call.Callee.(*parser.Ident); ok {
			switch ident.Name {
			case "Channel", "channel", "Chan":
				chanType := "interface{}"
				if len(call.TypeArgs) > 0 {
					if mapped, ok := zincToGoType[call.TypeArgs[0]]; ok {
						chanType = mapped
					} else {
						chanType = call.TypeArgs[0]
					}
				}
				return "chan " + chanType
			}
		}
	}
	// Fall back to general expression type inference
	t := g.inferExprType(expr, g.varTypes)
	if t != "" && t != "interface{}" {
		return t
	}
	return "interface{}"
}

// --- Constants ---------------------------------------------------------------

func (g *Generator) emitConstDecl(c *parser.ConstDecl) {
	g.writeln("const %s = %s", goName(c.Name, c.IsPub || !g.isSubpackage()), g.formatExpr(c.Value))
}
