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

// --- Classes (structs with constructors and methods) -------------------------

// emitClassDecl generates a Go struct, constructor, and methods from a Zinc class.
func (g *Generator) emitClassDecl(cls *parser.ClassDecl) {
	if g.sourceFile != "" && cls.Line > 0 {
		g.writeln("//line %s:%d", g.sourceFile, cls.Line)
	}

	name := cls.Name

	// Struct definition
	g.writeln("type %s struct {", name)
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

// emitConstructor generates a NewType() constructor function.
// Handles super() calls, this.field assignments, and remaining logic.
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

// emitMethodDecl generates a method on a receiver struct.
// Maps Zinc method names to Go equivalents (toString → String, etc.).
func (g *Generator) emitMethodDecl(receiver string, m *parser.MethodDecl) {
	// Set current fields/methods for implicit self resolution
	if cls, ok := g.structs[receiver]; ok {
		g.currentFields = make(map[string]bool)
		g.currentMethods = make(map[string]bool)
		g.currentParams = make(map[string]bool)
		for _, f := range cls.Fields {
			g.currentFields[f.Name] = true
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

// --- Data classes (record types with auto-generated String()) -----------------

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

	// String() method
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
