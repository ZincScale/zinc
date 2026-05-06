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

	// Use the FnDecl's own IsPub directly — the FnDecl is in hand, no
	// need to round-trip through bound.LookupSymbolByNameAndKind to
	// recover what we already know.
	name := fn.Name
	if g.isSubpackage() {
		name = goName(fn.Name, fn.IsPub)
	}
	if fn.Name == "main" {
		g.writeln("func main() {")
		g.indent++
		g.inferChannelTypes(fn.Body)
		g.emitBlock(fn.Body)
		g.indent--
		g.writeln("}")
		return
	}

	// Thrower lookup: prefer bound.Sigs.FnSigs (cross-pkg + cross-file
	// aware via the typecheck driver's externalSigs aggregate).
	canError := g.fnReturnsError(fn.Name)
	declaredThrower := returnTypeDeclaresError(fn.ReturnType)
	goRetType := g.goReturnTypeStr(fn.ReturnType)
	var ret string
	var valueGoTypes []string // value-slot Go types (sans trailing `error`)
	if declaredThrower {
		// User wrote `error` (bare) or `(..., error)` in the signature.
		// formatType already produces the right Go shape — no wrapping.
		ret = g.formatReturnType(fn.ReturnType, fn.Body)
		for _, t := range throwerValueTypes(fn.ReturnType) {
			valueGoTypes = append(valueGoTypes, g.formatType(t))
		}
	} else if canError {
		// Legacy auto-widen path: thrower-detected from body, signature
		// declared a non-error type. Wrap as `(T, error)`.
		if goRetType == "" {
			ret = " error"
		} else {
			ret = fmt.Sprintf(" (%s, error)", goRetType)
			valueGoTypes = []string{goRetType}
		}
	} else {
		ret = g.formatReturnType(fn.ReturnType, fn.Body)
		// Non-thrower tuple returns still need per-slot zero values for the
		// fallthrough emit below — `goRetType` carries the full
		// "(A, B, C)" string which zeroValueFor can't produce a valid Go
		// composite literal for. Populate valueGoTypes so the fallthrough
		// uses the same per-slot path as the thrower branches.
		if tup, ok := fn.ReturnType.(*parser.TupleType); ok {
			for _, t := range tup.Elements {
				valueGoTypes = append(valueGoTypes, g.formatType(t))
			}
		}
	}

	// Save/restore state for function scope
	prevRetType := g.currentReturnType
	prevOuterRetType := g.currentOuterReturnType
	prevRetOpt := g.currentReturnOptional
	prevErrCount := g.errVarCount
	prevIsThrower := g.currentFuncIsThrower
	prevIsTuple := g.currentReturnIsTuple
	prevValueGoTypes := g.currentThrowerValueGoTypes
	prevDeclaredThrower := g.currentReturnIsDeclaredThrower
	// Thrower iff syntactically declared OR detected by body-walk.
	// Body-walk path (canError) is the legacy widening path; declared-
	// thrower (declaredThrower) is the new explicit-`error` form.
	g.currentFuncIsThrower = canError || declaredThrower
	g.currentReturnIsDeclaredThrower = declaredThrower
	g.currentOuterReturnType = goRetType
	g.currentThrowerValueGoTypes = valueGoTypes
	if declaredThrower {
		// Declared throwers manage zero-value rendering per slot via
		// currentThrowerValueGoTypes — leave currentReturnType empty
		// so the legacy single-slot zero path doesn't fire.
		g.currentReturnType = ""
	} else if canError {
		g.currentReturnType = goRetType
	}
	_, isOptional := fn.ReturnType.(*parser.OptionalType)
	g.currentReturnOptional = isOptional
	if isOptional && !canError {
		g.currentReturnType = goRetType
	}
	_, isTuple := fn.ReturnType.(*parser.TupleType)
	g.currentReturnIsTuple = isTuple
	g.errVarCount = 0
	g.currentFuncParams = fn.Params

	// Pointer-optional params are answered via bound.NodeTypes[ident]
	// .Nullable in the body — checkFnDecl adds each param to the inner
	// scope with its resolved type, and inferType writes that V2Type to
	// the side-map for every Ident reference.

	// Set active type params for generic functions
	if len(fn.TypeParams) > 0 {
		g.activeTypeParams = make(map[string]bool)
		for _, tp := range fn.TypeParams {
			g.activeTypeParams[tp] = true
		}
	}

	params := g.formatParams(fn.Params)

	// Class-typed param tracking flows through bound.Bindings /
	// bound.NodeTypes — checkFnDecl populates the inner scope so each
	// Ident reference inside the body resolves to the right V2Type.
	tparams := goTypeParamsWithBounds(fn.TypeParams, fn.TypeParamBounds)
	g.trackTypeParamImports(fn.TypeParamBounds)
	g.writeln("func %s%s(%s)%s {", name, tparams, params, ret)
	g.indent++
	g.inferChannelTypes(fn.Body)
	g.emitBlock(fn.Body)
	// Ensure all paths return. Go requires an explicit return when the
	// last statement isn't a return — especially after a try/catch
	// where each branch returns but the compiler can't see it. Tail
	// shape depends on thrower + return-type combination.
	if !blockEndsInReturn(fn.Body) {
		if declaredThrower {
			// Per-slot zeros for value slots, then nil for the error.
			parts := make([]string, 0, len(valueGoTypes)+1)
			for _, vt := range valueGoTypes {
				parts = append(parts, g.zeroValueFor(vt))
			}
			parts = append(parts, "nil")
			g.writeln("return %s", strings.Join(parts, ", "))
		} else if canError {
			if goRetType == "" {
				g.writeln("return nil")
			} else {
				g.writeln("return %s, nil", g.zeroValueFor(goRetType))
			}
		} else if len(valueGoTypes) > 0 {
			// Tuple return: emit per-slot zeros so we don't produce
			// `(A, B, C){}` (invalid Go composite literal on a tuple).
			parts := make([]string, 0, len(valueGoTypes))
			for _, vt := range valueGoTypes {
				parts = append(parts, g.zeroValueFor(vt))
			}
			g.writeln("return %s", strings.Join(parts, ", "))
		} else if goRetType != "" {
			g.writeln("return %s", g.zeroValueFor(goRetType))
		}
	}
	g.indent--
	g.writeln("}")

	g.currentReturnType = prevRetType
	g.currentOuterReturnType = prevOuterRetType
	g.currentReturnOptional = prevRetOpt
	g.errVarCount = prevErrCount
	g.currentFuncIsThrower = prevIsThrower
	g.currentReturnIsTuple = prevIsTuple
	g.currentThrowerValueGoTypes = prevValueGoTypes
	g.currentReturnIsDeclaredThrower = prevDeclaredThrower
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
		if !g.isInterface(p.Name) && !g.isImportedInterface(p.Name) {
			resolved := g.resolveParentType(p.Name)
			g.writeln("%s", resolved)
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
		fieldName := goName(f.Name, f.IsPub || !g.isSubpackage())
		if tag := g.fieldTagString(f.Annotations, cls.Line); tag != "" {
			g.writeln("%s %s `%s`", fieldName, typeName, tag)
		} else {
			g.writeln("%s %s", fieldName, typeName)
		}
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

	// Exception-inheritance hook: when a class extends a throwable
	// (has Error() method, directly or inherited), auto-emit an
	// Unwrap() method pointing at the embedded parent. errors.As
	// walks the Unwrap chain, so a catch (Parent e) matches a thrown
	// *Child. Without this, struct embedding alone doesn't satisfy
	// errors.As — the types are distinct.
	if parent := g.exceptionParent(cls); parent != "" {
		tpArgs := goTypeArgs(cls.TypeParams)
		g.writeln("func (%s *%s%s) Unwrap() error {", recvName, name, tpArgs)
		g.indent++
		g.writeln("return &%s.%s", recvName, parent)
		g.indent--
		g.writeln("}")
		g.writeln("")
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
				continue
			}
			// Auto-init Go-stdlib pointer-receiver fields (sync.Mutex etc.) so the
			// pointer isn't left nil — first method call would otherwise segfault.
			if f.Type != nil {
				if goType, ok := g.isAutoPointerizedGoStructField(f.Type); ok {
					litFields = append(litFields, fmt.Sprintf("%s: &%s{}",
						goName(f.Name, f.IsPub || !g.isSubpackage()), goType))
				}
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

	// Field type-expression tracking moved to the bind side-map
	// (classFields + V2Type.TypeExpr) per Phase 3.7.2.
	_ = cls

	// Methods
	for _, m := range cls.Methods {
		g.emitMethodDecl(name, m, cls.TypeParams)
		g.writeln("")
	}

	if len(cls.TypeParams) > 0 {
		g.activeTypeParams = nil
	}
}

// exceptionParent returns the first non-interface parent of cls whose
// inheritance chain declares an Error() method — i.e. the embedded
// struct that makes this class a Zinc exception. Used to decide
// whether to auto-emit Unwrap() so errors.As walks the chain.
// Returns the parent name (for the embedded field reference) or "".
func (g *Generator) exceptionParent(cls *parser.ClassDecl) string {
	for _, p := range cls.Parents {
		if g.isInterface(p.Name) || g.isImportedInterface(p.Name) {
			continue
		}
		name := p.Name
		if idx := strings.LastIndex(p.Name, "."); idx >= 0 {
			name = p.Name[idx+1:]
		}
		if g.classExtendsError(name, map[string]bool{}) {
			return name
		}
	}
	return ""
}

// classExtendsError reports whether className is Error itself or a
// descendant — the nominal "is an error" check for widening. Walks the
// class's own methods + parent chain; the base Error class has an
// Error() string method, which is what all descendants inherit and
// what satisfies Go's error interface. Visited cycle guard.
func (g *Generator) classExtendsError(className string, visited map[string]bool) bool {
	if visited[className] {
		return false
	}
	visited[className] = true
	check := func(cls *parser.ClassDecl) bool {
		if cls == nil {
			return false
		}
		for _, m := range cls.Methods {
			if m.Name == "Error" {
				return true
			}
		}
		return false
	}
	var hit *parser.ClassDecl
	if cls, ok := g.structs[className]; ok && cls != nil {
		if check(cls) {
			return true
		}
		hit = cls
	}
	if hit == nil {
		for _, classes := range g.subpkgStructs {
			if cls, ok := classes[className]; ok && cls != nil {
				if check(cls) {
					return true
				}
				hit = cls
				break
			}
		}
	}
	if hit == nil {
		return false
	}
	for _, p := range hit.Parents {
		name := p.Name
		if idx := strings.LastIndex(p.Name, "."); idx >= 0 {
			name = p.Name[idx+1:]
		}
		if g.classExtendsError(name, visited) {
			return true
		}
	}
	return false
}

// emitConstructor generates a NewType() constructor function.
// Handles super() calls, this.field assignments, and remaining logic.
func (g *Generator) emitConstructor(typeName string, ctor *parser.CtorDecl, cls *parser.ClassDecl) {
	// Constructors can throw — bare return + early exit are now valid
	// because failable construction uses `throw ConfigException(...)`
	// instead of returning a partial object. The bare-return-in-ctor
	// rejection from commits 26b21cd + 70ec27b was unwound when Zinc
	// switched to C#-style exceptions.
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
	g.currentClass = typeName
	defer func() { g.currentFields = nil; g.currentFieldGoName = nil; g.currentMethods = nil; g.currentParams = nil; g.currentClass = "" }()

	tpDecl := goTypeParams(cls.TypeParams)
	tpArgs := goTypeArgs(cls.TypeParams)
	params := g.formatParams(ctor.Params)
	ctorThrows := g.fnReturnsError("New" + typeName)
	if ctorThrows {
		g.writeln("func New%s%s(%s) (*%s%s, error) {", typeName, tpDecl, params, typeName, tpArgs)
	} else {
		g.writeln("func New%s%s(%s) *%s%s {", typeName, tpDecl, params, typeName, tpArgs)
	}
	g.indent++

	// Save/restore thrower state. Inside a throwing ctor body, throw
	// and auto-error-check emit `return zero, err` to match the
	// widened signature.
	prevThrower := g.currentFuncIsThrower
	prevRetType := g.currentReturnType
	prevOuterRetType := g.currentOuterReturnType
	g.currentFuncIsThrower = ctorThrows
	if ctorThrows {
		g.currentReturnType = "*" + typeName + tpArgs
	}
	g.currentOuterReturnType = "*" + typeName + tpArgs
	defer func() {
		g.currentFuncIsThrower = prevThrower
		g.currentReturnType = prevRetType
		g.currentOuterReturnType = prevOuterRetType
	}()

	// Extract field assignments from ctor body: this.field = value → Field: value
	var litFields []string
	var remainingStmts []parser.Stmt

	// Handle super(...) → embedded parent initialization. Triggers on any
	// super() call regardless of arg count — bare super() must still emit
	// `Base: *NewBase()` so the parent's ctor (which initializes auto-
	// pointerized fields like *sync.Mutex) actually runs. Pre-fix we keyed
	// off `len(SuperArgs) > 0` and silently dropped zero-arg super, leaving
	// the embedded base zero-valued and any inherited mutex-typed field nil.
	if ctor.SuperCalled {
		parentType := ""
		for _, p := range cls.Parents {
			if !g.isInterface(p.Name) {
				parentType = p.Name
				break
			}
		}
		if parentType != "" {
			args := g.formatExprList(ctor.SuperArgs)
			// Cross-package parent (`pkg.Type`): the embedded struct field
			// is named with the unqualified type (Go embedding rule), and
			// the constructor lives in the parent package as `pkg.NewType`.
			// Same-package parent stays as `Type` / `NewType`.
			fieldName := parentType
			ctorRef := "New" + parentType
			if idx := strings.LastIndex(parentType, "."); idx >= 0 {
				pkg := parentType[:idx]
				typeName := parentType[idx+1:]
				fieldName = typeName
				ctorRef = pkg + ".New" + typeName
			}
			litFields = append(litFields, fmt.Sprintf("%s: *%s(%s)", fieldName, ctorRef, args))
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
					_, isThis := sel.Object.(*parser.ThisExpr)
					if !isThis {
						if ident, isIdent := sel.Object.(*parser.Ident); isIdent && ident.Name == "this" {
							isThis = true
						}
					}
					if isThis {
						val := g.formatExpr(assign.Value)
						// Auto-address-take a value-form struct literal when
						// the field is auto-pointerized (e.g.
						// `this.client = http.Client{...}` lands in a
						// *http.Client slot). Same rationale as the
						// emitAssignStmt branch — but the constructor folds
						// init-body assigns into the struct literal directly
						// and bypasses that path.
						if _, isStructLit := assign.Value.(*parser.StructLit); isStructLit {
							for _, fld := range cls.Fields {
								if fld.Name == sel.Field {
									if _, ok := g.isAutoPointerizedGoStructField(fld.Type); ok {
										val = "&" + val
									}
									break
								}
							}
						}
						litFields = append(litFields, fmt.Sprintf("%s: %s", fieldGoName, val))
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
		fieldGoName := exportName(f.Name)
		if gn, ok := g.currentFieldGoName[f.Name]; ok {
			fieldGoName = gn
		}
		if assignedFields[fieldGoName] || assignedFields[exportName(f.Name)] {
			continue
		}
		if f.Default != nil {
			val := g.formatExpr(f.Default)
			if f.Type != nil {
				if _, isListLit := f.Default.(*parser.ListLit); isListLit {
					val = g.formatType(f.Type) + "{}"
				}
				if _, isMapLit := f.Default.(*parser.MapLit); isMapLit {
					val = g.formatType(f.Type) + "{}"
				}
			}
			litFields = append(litFields, fmt.Sprintf("%s: %s", fieldGoName, val))
			continue
		}
		// Auto-init Go-stdlib pointer-receiver fields (sync.Mutex etc.) so the
		// pointer isn't left nil — first method call would otherwise segfault.
		if f.Type != nil {
			if goType, ok := g.isAutoPointerizedGoStructField(f.Type); ok {
				litFields = append(litFields, fmt.Sprintf("%s: &%s{}", fieldGoName, goType))
			}
		}
	}

	// Emit struct literal. Throwing ctors have their return shape
	// widened to `(*Type, error)` so all returns pair nil error.
	typeNameTA := typeName + tpArgs
	retVal := func(v string) {
		if ctorThrows {
			g.writeln("return %s, nil", v)
		} else {
			g.writeln("return %s", v)
		}
	}
	if len(litFields) > 0 {
		if len(remainingStmts) == 0 {
			if len(litFields) <= 3 {
				retVal(fmt.Sprintf("&%s{%s}", typeNameTA, strings.Join(litFields, ", ")))
			} else {
				g.writeln("return &%s{", typeNameTA)
				g.indent++
				for _, f := range litFields {
					g.writeln("%s,", f)
				}
				g.indent--
				if ctorThrows {
					g.writeln("}, nil")
				} else {
					g.writeln("}")
				}
			}
		} else {
			g.writeln("%s := &%s{%s}", recvName, typeNameTA, strings.Join(litFields, ", "))
			for _, stmt := range remainingStmts {
				g.emitStmt(stmt)
			}
			retVal(recvName)
		}
	} else if len(remainingStmts) > 0 {
		g.writeln("%s := &%s{}", recvName, typeNameTA)
		for _, stmt := range remainingStmts {
			g.emitStmt(stmt)
		}
		retVal(recvName)
	} else {
		retVal(fmt.Sprintf("&%s{}", typeNameTA))
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
	} else if dc, ok := g.dataClassDecls[receiver]; ok {
		// Data classes have params (which become exported fields) and methods.
		// Mirror the regular-class setup so bare `a` in a method body resolves
		// to `this.A` via the implicit-self path.
		g.currentFields = make(map[string]bool)
		g.currentFieldGoName = make(map[string]string)
		g.currentMethods = make(map[string]bool)
		g.currentParams = make(map[string]bool)
		for _, f := range dc.Params {
			g.currentFields[f.Name] = true
			g.currentFieldGoName[f.Name] = exportName(f.Name) // data fields are always exported
		}
		for _, method := range dc.Methods {
			g.currentMethods[method.Name] = true
		}
		for _, p := range m.Params {
			g.currentParams[p.Name] = true
		}
	}
	g.currentClass = receiver
	// Pointer-optional and class-typed params are answered via
	// bound.NodeTypes — checkFnDecl populates the inner scope, so
	// every Ident reference inside the body sees the right V2Type.
	defer func() {
		g.currentFields = nil
		g.currentFieldGoName = nil
		g.currentMethods = nil
		g.currentParams = nil
		g.currentClass = ""
	}()

	canError := g.methodReturnsError(receiver, m.Name)
	declaredThrower := returnTypeDeclaresError(m.ReturnType)
	goRetType := g.goReturnTypeStr(m.ReturnType)

	// If the return type is a known class (not data class), return *Type
	// to match constructor return types (NewType() returns *Type).
	if simpleType, ok := m.ReturnType.(*parser.SimpleType); ok {
		if _, isStruct := g.structs[simpleType.Name]; isStruct {
			goRetType = "*" + simpleType.Name
		}
	}

	var ret string
	var valueGoTypes []string
	if declaredThrower {
		ret = g.formatReturnType(m.ReturnType, m.Body)
		for _, t := range throwerValueTypes(m.ReturnType) {
			valueGoTypes = append(valueGoTypes, g.formatType(t))
		}
	} else if canError {
		if goRetType == "" {
			ret = " error"
		} else {
			ret = fmt.Sprintf(" (%s, error)", goRetType)
			valueGoTypes = []string{goRetType}
		}
	} else if simpleType, ok := m.ReturnType.(*parser.SimpleType); ok {
		if _, isStruct := g.structs[simpleType.Name]; isStruct {
			ret = " *" + simpleType.Name
		} else {
			ret = g.formatReturnType(m.ReturnType, m.Body)
		}
	} else {
		ret = g.formatReturnType(m.ReturnType, m.Body)
		// Mirror the function-level fix: tuple-returning methods need
		// per-slot zeros for fallthrough fallback emission.
		if tup, ok := m.ReturnType.(*parser.TupleType); ok {
			for _, t := range tup.Elements {
				valueGoTypes = append(valueGoTypes, g.formatType(t))
			}
		}
	}

	prevRetType := g.currentReturnType
	prevOuterRetType := g.currentOuterReturnType
	prevMethodRetType := g.currentMethodRetType
	prevIsThrower := g.currentFuncIsThrower
	prevIsTuple := g.currentReturnIsTuple
	prevRetOpt := g.currentReturnOptional
	prevValueGoTypes := g.currentThrowerValueGoTypes
	prevDeclaredThrower := g.currentReturnIsDeclaredThrower
	g.currentFuncIsThrower = canError || declaredThrower
	g.currentReturnIsDeclaredThrower = declaredThrower
	g.currentOuterReturnType = goRetType
	g.currentThrowerValueGoTypes = valueGoTypes
	if declaredThrower {
		g.currentReturnType = ""
	} else if canError {
		g.currentReturnType = goRetType
	}
	_, isTuple := m.ReturnType.(*parser.TupleType)
	g.currentReturnIsTuple = isTuple
	_, isOptional := m.ReturnType.(*parser.OptionalType)
	g.currentReturnOptional = isOptional
	if isOptional && !canError {
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
	{
		vis := goName(goMethodName, methodPub)
		params := g.formatParams(m.Params)
		// Data classes are values — methods take a value receiver to match
		// the value-returning constructor and the auto-generated String().
		// This keeps Pair (value) satisfying interfaces, addressable in maps,
		// and consistent with Go conventions for record types.
		recvPrefix := "*"
		if g.isDataClass(receiver) {
			recvPrefix = ""
		}
		g.writeln("func (%s %s%s) %s(%s)%s {", recvName, recvPrefix, receiverTA, vis, params, ret)
	}
	g.indent++
	g.emitBlock(m.Body)
	if !blockEndsInReturn(m.Body) {
		if declaredThrower {
			parts := make([]string, 0, len(valueGoTypes)+1)
			for _, vt := range valueGoTypes {
				parts = append(parts, g.zeroValueFor(vt))
			}
			parts = append(parts, "nil")
			g.writeln("return %s", strings.Join(parts, ", "))
		} else if canError {
			if goRetType == "" {
				g.writeln("return nil")
			} else {
				g.writeln("return %s, nil", g.zeroValueFor(goRetType))
			}
		} else if len(valueGoTypes) > 0 {
			parts := make([]string, 0, len(valueGoTypes))
			for _, vt := range valueGoTypes {
				parts = append(parts, g.zeroValueFor(vt))
			}
			g.writeln("return %s", strings.Join(parts, ", "))
		} else if goRetType != "" && !strings.HasPrefix(ret, " *") {
			// Non-thrower T-returning method: emit zero fallback.
			g.writeln("return %s", g.zeroValueFor(goRetType))
		}
	}
	g.indent--
	g.writeln("}")

	g.currentReturnType = prevRetType
	g.currentOuterReturnType = prevOuterRetType
	g.currentMethodRetType = prevMethodRetType
	g.currentFuncIsThrower = prevIsThrower
	g.currentReturnIsTuple = prevIsTuple
	g.currentReturnOptional = prevRetOpt
	g.currentThrowerValueGoTypes = prevValueGoTypes
	g.currentReturnIsDeclaredThrower = prevDeclaredThrower
}

// collectParentFields walks the inheritance chain and adds parent fields to the map.
func (g *Generator) collectParentFields(cls *parser.ClassDecl, fields map[string]bool) {
	for _, p := range cls.Parents {
		if g.isInterface(p.Name) {
			continue
		}
		if parentCls, ok := g.structs[p.Name]; ok {
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
		if g.isInterface(p.Name) {
			continue
		}
		if parentCls, ok := g.structs[p.Name]; ok {
			for _, m := range parentCls.Methods {
				methods[m.Name] = true
			}
			g.collectParentMethods(parentCls, methods)
		}
	}
}

// fieldTagString builds the Go struct tag string (without surrounding
// backticks) for a field's annotations. Recognizes @Json / @Yaml / @Toml
// and joins multiple tags with spaces, e.g.
//
//   @Json("name") @Yaml("name") String userName
//
// becomes the tag `json:"name" yaml:"name"`. Tag options like `omitempty`
// are trailing bare-ident args:
//
//   @Json("email", omitempty) → json:"email,omitempty"
//
// Unknown field annotations are a compile error — fields only carry
// serialization tags today. Returns "" when there are no annotations.
func (g *Generator) fieldTagString(annots []*parser.Annotation, fallbackLine int) string {
	if len(annots) == 0 {
		return ""
	}
	var parts []string
	for _, a := range annots {
		format, ok := fieldTagFormat(a.Name)
		if !ok {
			g.compileError(fallbackLine,
				"unknown field annotation @%s — supported: @Json, @Yaml, @Toml, @Avro", a.Name)
			continue
		}
		if len(a.Args) == 0 {
			g.compileError(fallbackLine,
				"annotation @%s requires a field name, e.g. @%s(\"name\")", a.Name, a.Name)
			continue
		}
		// Args[0] is the field name — wrapped in quotes by the parser
		// when the source was a string literal. Strip the outer quotes
		// to get the raw name.
		nameLit := strings.Trim(a.Args[0], `"`)
		tagValue := nameLit
		for _, opt := range a.Args[1:] {
			// Trailing options like `omitempty` are bare idents, no quotes.
			tagValue += "," + strings.Trim(opt, `"`)
		}
		parts = append(parts, fmt.Sprintf(`%s:%q`, format, tagValue))
	}
	return strings.Join(parts, " ")
}

// fieldTagFormat maps a Zinc annotation name to its Go struct-tag key.
// Unknown names return ("", false) so the caller can emit a compile error.
func fieldTagFormat(name string) (string, bool) {
	switch name {
	case "Json":
		return "json", true
	case "Yaml":
		return "yaml", true
	case "Toml":
		return "toml", true
	case "Avro":
		return "avro", true
	}
	return "", false
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
		fieldName := exportName(f.Name)
		if tag := g.fieldTagString(f.Annotations, d.Line); tag != "" {
			g.writeln("%s %s `%s`", fieldName, typeName, tag)
		} else {
			g.writeln("%s %s", fieldName, typeName)
		}
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
		fmtArgs = append(fmtArgs, recvName+"."+exportName(f.Name))
	}
	g.writeln("func (%s %s) String() string {", recvName, nameTA)
	g.indent++
	g.writeln("return fmt.Sprintf(\"%s(%s)\", %s)", d.Name, strings.Join(fmtParts, ", "), strings.Join(fmtArgs, ", "))
	g.indent--
	g.writeln("}")

	// Methods — but first reject any that would mutate the receiver.
	// Data classes are immutable records: emit a compile error for
	// `this.field = ...` or bare `field = ...` (where field shadows nothing).
	// To "update" a data class, return a new instance.
	fieldSet := make(map[string]bool, len(d.Params))
	for _, f := range d.Params {
		fieldSet[f.Name] = true
	}
	for _, m := range d.Methods {
		g.validateDataClassMethod(d.Name, m, fieldSet)
	}

	for _, m := range d.Methods {
		g.writeln("")
		g.emitMethodDecl(d.Name, m, d.TypeParams)
	}

	if len(d.TypeParams) > 0 {
		g.activeTypeParams = nil
	}
}

// --- Data-class immutability check ------------------------------------------

// validateDataClassMethod walks a data-class method body and reports any
// statement that would mutate the receiver. Catches:
//   - `this.field = expr` (and compound forms +=, -=, etc.)
//   - bare `field = expr` where field is a data-class field
//   - mutation through the receiver, e.g. `this.list[i] = ...` or
//     `list[i] = ...` when `list` is a field — the leftmost subject of the
//     LHS is `this` or a field.
//
// Method parameters and locally-declared vars shadow fields. The walker
// tracks declarations as it descends so a local named after a field
// doesn't trigger a false positive — matches the codegen's implicit-self
// rewrite semantics. Use `return DataClass(newA, b)` to "update" a data
// class instead.
func (g *Generator) validateDataClassMethod(typeName string, m *parser.MethodDecl, fields map[string]bool) {
	shadow := make(map[string]bool, len(m.Params))
	for _, p := range m.Params {
		shadow[p.Name] = true
	}
	g.checkBlockMutation(m.Body, typeName, m.Name, fields, shadow)
}

// checkBlockMutation walks a block; declarations inside are scoped to a
// fresh shadow map (extended from the caller's) so they don't leak out.
func (g *Generator) checkBlockMutation(block *parser.BlockStmt, typeName, methodName string, fields, parentShadow map[string]bool) {
	if block == nil {
		return
	}
	scope := make(map[string]bool, len(parentShadow))
	for k := range parentShadow {
		scope[k] = true
	}
	for _, s := range block.Stmts {
		g.checkStmtMutation(s, typeName, methodName, fields, scope)
	}
}

func (g *Generator) checkStmtMutation(s parser.Stmt, typeName, methodName string, fields, scope map[string]bool) {
	switch stmt := s.(type) {
	case *parser.VarStmt:
		// Local declaration shadows fields for subsequent statements in this
		// scope (and any nested blocks). Don't add until *after* checking the
		// RHS expression — but we don't recurse into expressions for the
		// mutation check, so adding here is correct ordering-wise.
		if stmt.Name != "" {
			scope[stmt.Name] = true
		}
	case *parser.TupleVarStmt:
		for _, n := range stmt.Names {
			if n != "" {
				scope[n] = true
			}
		}
	case *parser.AssignStmt:
		if g.lhsMutatesReceiver(stmt.Target, fields, scope) {
			g.compileError(stmt.Line,
				"cannot mutate data-class field via %q in method %s.%s — data classes are immutable; return a new instance instead (e.g. `return %s(...)`)",
				lhsDescription(stmt.Target), typeName, methodName, typeName)
		}
	case *parser.IfStmt:
		g.checkBlockMutation(stmt.Then, typeName, methodName, fields, scope)
		if stmt.ElseStmt != nil {
			g.checkStmtMutation(stmt.ElseStmt, typeName, methodName, fields, scope)
		}
	case *parser.BlockStmt:
		g.checkBlockMutation(stmt, typeName, methodName, fields, scope)
	case *parser.ForStmt:
		// Loop-bound names shadow fields inside the body.
		inner := make(map[string]bool, len(scope)+2)
		for k := range scope {
			inner[k] = true
		}
		if stmt.IsRange {
			if stmt.Item != "" {
				inner[stmt.Item] = true
			}
			if stmt.IndexVar != "" {
				inner[stmt.IndexVar] = true
			}
		} else if init, ok := stmt.Init.(*parser.VarStmt); ok && init.Name != "" {
			inner[init.Name] = true
		}
		g.checkBlockMutation(stmt.Body, typeName, methodName, fields, inner)
	case *parser.WhileStmt:
		g.checkBlockMutation(stmt.Body, typeName, methodName, fields, scope)
	case *parser.MatchStmt:
		for _, c := range stmt.Cases {
			g.checkBlockMutation(c.Body, typeName, methodName, fields, scope)
		}
	case *parser.SelectStmt:
		for _, c := range stmt.Cases {
			g.checkBlockMutation(c.Body, typeName, methodName, fields, scope)
		}
	case *parser.WithStmt:
		// Resource bindings shadow fields inside the body.
		inner := make(map[string]bool, len(scope)+len(stmt.Resources))
		for k := range scope {
			inner[k] = true
		}
		for _, r := range stmt.Resources {
			if r.Name != "" {
				inner[r.Name] = true
			}
		}
		g.checkBlockMutation(stmt.Body, typeName, methodName, fields, inner)
	case *parser.ParallelForStmt:
		g.checkBlockMutation(stmt.Body, typeName, methodName, fields, scope)
	case *parser.TimeoutStmt:
		g.checkBlockMutation(stmt.Body, typeName, methodName, fields, scope)
	}
}

// lhsMutatesReceiver reports whether the leftmost subject of an assignment
// target is `this` (or a data-class field name that isn't shadowed by a
// method parameter).
func (g *Generator) lhsMutatesReceiver(e parser.Expr, fields, shadow map[string]bool) bool {
	for {
		switch ex := e.(type) {
		case *parser.SelectorExpr:
			e = ex.Object
		case *parser.IndexExpr:
			e = ex.Object
		case *parser.ThisExpr:
			return true
		case *parser.Ident:
			if ex.Name == "this" {
				return true
			}
			return fields[ex.Name] && !shadow[ex.Name]
		default:
			return false
		}
	}
}

// lhsDescription renders an LHS expression for use in the compile-error
// message. Best-effort — falls back to a generic label when the shape
// doesn't fit the common patterns.
func lhsDescription(e parser.Expr) string {
	switch ex := e.(type) {
	case *parser.SelectorExpr:
		return lhsDescription(ex.Object) + "." + ex.Field
	case *parser.IndexExpr:
		return lhsDescription(ex.Object) + "[...]"
	case *parser.ThisExpr:
		return "this"
	case *parser.Ident:
		return ex.Name
	}
	return "<expr>"
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
					chanType = g.resolveTypeArg(call.TypeArgs[0])
				}
				return "chan " + chanType
			}
		}
	}
	// Fall back to general expression type inference
	t := g.inferExprType(expr)
	if t != "" && t != "interface{}" {
		return t
	}
	return "interface{}"
}

// --- Constants ---------------------------------------------------------------

func (g *Generator) emitConstDecl(c *parser.ConstDecl) {
	g.writeln("const %s = %s", goName(c.Name, c.IsPub || !g.isSubpackage()), g.formatExpr(c.Value))
}
