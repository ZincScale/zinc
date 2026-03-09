package codegen

import "zinc/internal/parser"

// TypeRegistry holds cross-file type information for a multi-file package.
// It is populated from all source files in a package before any file is
// generated, enabling cross-file type resolution.
type TypeRegistry struct {
	ClassNames     map[string]bool
	InterfaceNames map[string]bool
	EnumNames      map[string]bool
	CanThrowFns    map[string]bool
}

// NewTypeRegistry creates an empty TypeRegistry.
func NewTypeRegistry() *TypeRegistry {
	return &TypeRegistry{
		ClassNames:     make(map[string]bool),
		InterfaceNames: make(map[string]bool),
		EnumNames:      make(map[string]bool),
		CanThrowFns:    make(map[string]bool),
	}
}

// BuildRegistry creates a fully-populated TypeRegistry from a slice of programs
// (all files in one package). It performs two passes:
//
//  1. Collect class, interface, and enum names.
//  2. Mark functions/methods that directly contain throw statements as CanThrow.
func BuildRegistry(progs []*parser.Program) *TypeRegistry {
	reg := NewTypeRegistry()

	// Pass 1: collect type names
	for _, prog := range progs {
		for _, decl := range prog.Decls {
			switch d := decl.(type) {
			case *parser.ClassDecl:
				reg.ClassNames[d.Name] = true
			case *parser.InterfaceDecl:
				reg.InterfaceNames[d.Name] = true
			case *parser.EnumDecl:
				reg.EnumNames[d.Name] = true
			}
		}
	}

	// Pass 2: mark failable fns using transitive fixed-point iteration
	g := &Generator{canThrowFns: make(map[string]bool)}
	changed := true
	for changed {
		changed = false
		for _, prog := range progs {
			for _, decl := range prog.Decls {
				switch d := decl.(type) {
				case *parser.FnDecl:
					if !reg.CanThrowFns[d.Name] && g.bodyIsFailable(d.Body) {
						d.CanThrow = true
						reg.CanThrowFns[d.Name] = true
						g.canThrowFns[d.Name] = true
						changed = true
					}
				case *parser.ClassDecl:
					for _, m := range d.Methods {
						key := d.Name + "." + m.Name
						if !reg.CanThrowFns[key] && g.bodyIsFailable(m.Body) {
							m.CanThrow = true
							reg.CanThrowFns[key] = true
							g.canThrowFns[key] = true
							changed = true
						}
					}
				}
			}
		}
	}

	return reg
}

// lastSegment returns the last path segment, e.g. "myapp/utils" → "utils".
func lastSegment(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
