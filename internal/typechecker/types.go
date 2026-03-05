package typechecker

import (
	"fmt"
	"strings"

	"growler/internal/parser"
)

// typeKind distinguishes type categories.
type typeKind int

const (
	kindPrimitive typeKind = iota
	kindOptional
	kindList
	kindMap
	kindChan
	kindTypeParam
	kindClass
	kindInterface
	kindEnum
	kindFn
)

// Type is the base interface for all Growler types.
type Type interface {
	typeKind() typeKind
	String() string
}

// --- Primitive types ---------------------------------------------------------

type primitiveType struct {
	name string
	kind typeKind
}

func (p *primitiveType) typeKind() typeKind { return p.kind }
func (p *primitiveType) String() string     { return p.name }

// Package-level primitive singletons.
var (
	TypeInt     Type = &primitiveType{"Int", kindPrimitive}
	TypeFloat   Type = &primitiveType{"Float", kindPrimitive}
	TypeString  Type = &primitiveType{"String", kindPrimitive}
	TypeBool    Type = &primitiveType{"Bool", kindPrimitive}
	TypeVoid    Type = &primitiveType{"Void", kindPrimitive}
	TypeAny     Type = &primitiveType{"Any", kindPrimitive}
	TypeUnknown Type = &primitiveType{"Unknown", kindPrimitive}
	TypeNull    Type = &primitiveType{"Null", kindPrimitive}
)

// --- Composite types ---------------------------------------------------------

// OptionalType: String? → *string in Go
type OptionalType struct {
	Inner Type
}

func (o *OptionalType) typeKind() typeKind { return kindOptional }
func (o *OptionalType) String() string     { return o.Inner.String() + "?" }

// ListType: List<T>
type ListType struct {
	Elem Type
}

func (l *ListType) typeKind() typeKind { return kindList }
func (l *ListType) String() string     { return "List<" + l.Elem.String() + ">" }

// MapType: Map<K,V>
type MapType struct {
	Key   Type
	Value Type
}

func (m *MapType) typeKind() typeKind { return kindMap }
func (m *MapType) String() string     { return "Map<" + m.Key.String() + "," + m.Value.String() + ">" }

// ChanType: Chan<T>
type ChanType struct {
	Elem Type
}

func (c *ChanType) typeKind() typeKind { return kindChan }
func (c *ChanType) String() string     { return "Chan<" + c.Elem.String() + ">" }

// TypeParamType: T, K, V — generic escape hatch
type TypeParamType struct {
	Name string
}

func (t *TypeParamType) typeKind() typeKind { return kindTypeParam }
func (t *TypeParamType) String() string     { return t.Name }

// --- Named types -------------------------------------------------------------

// FnSig describes a function or method signature.
type FnSig struct {
	TypeParams []string
	ParamNames []string // parallel to Params: parameter names (for named-arg validation)
	HasDefault []bool   // parallel to Params: whether each param has a default value
	Params     []Type
	Return     Type
	CanThrow   bool
}

func (f *FnSig) String() string {
	params := make([]string, len(f.Params))
	for i, p := range f.Params {
		params[i] = p.String()
	}
	ret := "Void"
	if f.Return != nil {
		ret = f.Return.String()
	}
	return fmt.Sprintf("fn(%s): %s", strings.Join(params, ", "), ret)
}

// ClassType represents a Growler class.
type ClassType struct {
	Name       string
	TypeParams []string
	Parents    []string
	Fields     map[string]Type
	Methods    map[string]*FnSig
	Ctor       *FnSig
}

func (c *ClassType) typeKind() typeKind { return kindClass }
func (c *ClassType) String() string     { return c.Name }

// InterfaceType represents a Growler interface.
type InterfaceType struct {
	Name    string
	Methods map[string]*FnSig
}

func (i *InterfaceType) typeKind() typeKind { return kindInterface }
func (i *InterfaceType) String() string     { return i.Name }

// EnumType represents a Growler enum.
type EnumType struct {
	Name     string
	Variants []string
}

func (e *EnumType) typeKind() typeKind { return kindEnum }
func (e *EnumType) String() string     { return e.Name }

// --- Type equality and assignability -----------------------------------------

// TypeEqual returns true if from and to are the same type structurally.
func TypeEqual(a, b Type) bool {
	if a == TypeUnknown || b == TypeUnknown {
		return true
	}
	if a == b {
		return true
	}
	switch at := a.(type) {
	case *OptionalType:
		if bt, ok := b.(*OptionalType); ok {
			return TypeEqual(at.Inner, bt.Inner)
		}
		return false
	case *ListType:
		if bt, ok := b.(*ListType); ok {
			return TypeEqual(at.Elem, bt.Elem)
		}
		return false
	case *MapType:
		if bt, ok := b.(*MapType); ok {
			return TypeEqual(at.Key, bt.Key) && TypeEqual(at.Value, bt.Value)
		}
		return false
	case *ChanType:
		if bt, ok := b.(*ChanType); ok {
			return TypeEqual(at.Elem, bt.Elem)
		}
		return false
	case *TypeParamType:
		if bt, ok := b.(*TypeParamType); ok {
			return at.Name == bt.Name
		}
		return true // generic: permissive
	case *ClassType:
		if bt, ok := b.(*ClassType); ok {
			return at.Name == bt.Name
		}
		return false
	case *InterfaceType:
		if bt, ok := b.(*InterfaceType); ok {
			return at.Name == bt.Name
		}
		return false
	case *EnumType:
		if bt, ok := b.(*EnumType); ok {
			return at.Name == bt.Name
		}
		return false
	}
	return false
}

// Assignable returns true if a value of type `from` can be assigned to `to`.
func Assignable(from, to Type) bool {
	if from == TypeUnknown || to == TypeUnknown {
		return true // error recovery: don't cascade
	}
	if to == TypeAny {
		return true
	}
	if from == TypeAny {
		return true
	}
	// Either side is a generic type parameter → permissive
	if from.typeKind() == kindTypeParam || to.typeKind() == kindTypeParam {
		return true
	}
	// Int is assignable to enum types (iota-based enums in Growler)
	if from == TypeInt {
		if _, ok := to.(*EnumType); ok {
			return true
		}
	}
	// Enum values are also comparable/assignable to Int
	if from.typeKind() == kindEnum && to == TypeInt {
		return true
	}
	// null is assignable to optionals and Any
	if from == TypeNull {
		if to == TypeAny {
			return true
		}
		if _, ok := to.(*OptionalType); ok {
			return true
		}
		return false
	}
	// Assigning to an optional: inner type or null
	if opt, ok := to.(*OptionalType); ok {
		if from == TypeNull {
			return true
		}
		return Assignable(from, opt.Inner)
	}
	// ClassType to InterfaceType: check if class lists interface in Parents
	if ct, ok := from.(*ClassType); ok {
		if it, ok2 := to.(*InterfaceType); ok2 {
			for _, parent := range ct.Parents {
				if parent == it.Name {
					return true
				}
			}
			return false
		}
	}
	return TypeEqual(from, to)
}

// --- Type expression resolution ----------------------------------------------

// resolveTypeExpr converts a parser.TypeExpr into a checker Type.
// The checker parameter provides context (class/interface/enum/typeParam tables).
func (c *Checker) resolveTypeExpr(tex parser.TypeExpr) Type {
	if tex == nil {
		return TypeVoid
	}
	switch t := tex.(type) {
	case *parser.SimpleType:
		return c.resolveSimpleName(t.Name)
	case *parser.GenericType:
		switch t.Name {
		case "List":
			if len(t.TypeArgs) >= 1 {
				return &ListType{Elem: c.resolveTypeExpr(t.TypeArgs[0])}
			}
			return &ListType{Elem: TypeAny}
		case "Map":
			k, v := TypeAny, TypeAny
			if len(t.TypeArgs) >= 1 {
				k = c.resolveTypeExpr(t.TypeArgs[0])
			}
			if len(t.TypeArgs) >= 2 {
				v = c.resolveTypeExpr(t.TypeArgs[1])
			}
			return &MapType{Key: k, Value: v}
		case "Chan":
			if len(t.TypeArgs) >= 1 {
				return &ChanType{Elem: c.resolveTypeExpr(t.TypeArgs[0])}
			}
			return &ChanType{Elem: TypeAny}
		default:
			// Unknown generic — resolve name, ignore params
			return c.resolveSimpleName(t.Name)
		}
	case *parser.OptionalType:
		return &OptionalType{Inner: c.resolveTypeExpr(t.Inner)}
	}
	return TypeUnknown
}

// resolveSimpleName maps a bare type name to a Type.
func (c *Checker) resolveSimpleName(name string) Type {
	switch name {
	case "Int":
		return TypeInt
	case "Float":
		return TypeFloat
	case "String":
		return TypeString
	case "Bool":
		return TypeBool
	case "Void":
		return TypeVoid
	case "Any":
		return TypeAny
	}
	// Check active generic type params
	if c.currentTypeParams[name] {
		return &TypeParamType{Name: name}
	}
	// Named types
	if ct, ok := c.classes[name]; ok {
		return ct
	}
	if it, ok := c.interfaces[name]; ok {
		return it
	}
	if et, ok := c.enums[name]; ok {
		return et
	}
	// Unknown type — report error
	c.errorf(0, 0, "undefined type %q", name)
	return TypeUnknown
}
