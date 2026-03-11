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

import "zinc/internal/parser"

// TypeRegistry holds cross-file type information for a multi-file package.
// It is populated from all source files in a package before any file is
// generated, enabling cross-file type resolution.
type TypeRegistry struct {
	ClassNames     map[string]bool
	InterfaceNames map[string]bool
	EnumNames      map[string]bool
	CanThrowFns     map[string]bool
	VoidCanThrowFns map[string]bool
	ClassCtors      map[string]*parser.CtorDecl            // class name → constructor
	FnParams       map[string][]*parser.ParamDecl         // function name → params
	MethodParams   map[string]map[string][]*parser.ParamDecl // class → method → params
	ClassFields    map[string][]*classFieldInfo            // class name → fields
	ClassParents   map[string][]string                     // class name → parent names
}

// NewTypeRegistry creates an empty TypeRegistry.
func NewTypeRegistry() *TypeRegistry {
	return &TypeRegistry{
		ClassNames:     make(map[string]bool),
		InterfaceNames: make(map[string]bool),
		EnumNames:      make(map[string]bool),
		CanThrowFns:     make(map[string]bool),
		VoidCanThrowFns: make(map[string]bool),
		ClassCtors:      make(map[string]*parser.CtorDecl),
		FnParams:       make(map[string][]*parser.ParamDecl),
		MethodParams:   make(map[string]map[string][]*parser.ParamDecl),
		ClassFields:    make(map[string][]*classFieldInfo),
		ClassParents:   make(map[string][]string),
	}
}

// BuildRegistry creates a fully-populated TypeRegistry from a slice of programs
// (all files in one package). It performs two passes:
//
//  1. Collect class, interface, and enum names.
//  2. Mark functions/methods that directly return Error(...) as CanThrow.
func BuildRegistry(progs []*parser.Program) *TypeRegistry {
	reg := NewTypeRegistry()

	// Pass 1: collect type names, constructors, and parameter lists
	for _, prog := range progs {
		for _, decl := range prog.Decls {
			switch d := decl.(type) {
			case *parser.ClassDecl:
				reg.ClassNames[d.Name] = true
				if d.Ctor != nil {
					reg.ClassCtors[d.Name] = d.Ctor
				}
				for _, m := range d.Methods {
					if len(m.Params) > 0 {
						if reg.MethodParams[d.Name] == nil {
							reg.MethodParams[d.Name] = make(map[string][]*parser.ParamDecl)
						}
						reg.MethodParams[d.Name][m.Name] = m.Params
					}
				}
				var fields []*classFieldInfo
				for _, f := range d.Fields {
					fields = append(fields, &classFieldInfo{Name: f.Name, Type: f.Type})
				}
				reg.ClassFields[d.Name] = fields
				reg.ClassParents[d.Name] = d.Parents
			case *parser.InterfaceDecl:
				reg.InterfaceNames[d.Name] = true
			case *parser.EnumDecl:
				reg.EnumNames[d.Name] = true
			case *parser.FnDecl:
				if len(d.Params) > 0 {
					reg.FnParams[d.Name] = d.Params
				}
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
						if d.ReturnType == nil {
							reg.VoidCanThrowFns[d.Name] = true
						}
						changed = true
					}
				case *parser.ClassDecl:
					for _, m := range d.Methods {
						key := d.Name + "." + m.Name
						if !reg.CanThrowFns[key] && g.bodyIsFailable(m.Body) {
							m.CanThrow = true
							reg.CanThrowFns[key] = true
							g.canThrowFns[key] = true
							if m.ReturnType == nil {
								reg.VoidCanThrowFns[key] = true
							}
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
