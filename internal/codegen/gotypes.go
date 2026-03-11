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

import (
	"go/importer"
	"go/types"
	"sync"
)

// GoTypeResolver uses go/importer to introspect Go package signatures
// at transpile time. It answers "does function F in package P return error?"
// without any external dependencies.
type GoTypeResolver struct {
	imp      types.Importer
	cache    map[string]*types.Package
	negative map[string]bool // packages that failed to import
	mu       sync.Mutex
}

// NewGoTypeResolver creates a resolver backed by the default (gc) importer.
func NewGoTypeResolver() *GoTypeResolver {
	return &GoTypeResolver{
		imp:      importer.Default(),
		cache:    make(map[string]*types.Package),
		negative: make(map[string]bool),
	}
}

// loadPkgLocked loads a package using cache. Caller must hold mu.
func (r *GoTypeResolver) loadPkgLocked(pkgPath string) *types.Package {
	if r.negative[pkgPath] {
		return nil
	}
	if pkg, ok := r.cache[pkgPath]; ok {
		return pkg
	}
	pkg, err := r.imp.Import(pkgPath)
	if err != nil {
		r.negative[pkgPath] = true
		return nil
	}
	r.cache[pkgPath] = pkg
	return pkg
}

// lookupFuncLocked finds a package-level function. Caller must hold mu.
func (r *GoTypeResolver) lookupFuncLocked(pkgPath, funcName string) *types.Signature {
	pkg := r.loadPkgLocked(pkgPath)
	if pkg == nil {
		return nil
	}
	obj := pkg.Scope().Lookup(funcName)
	if obj == nil {
		return nil
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	return fn.Type().(*types.Signature)
}

// lookupMethodLocked finds a method on a named type. Caller must hold mu.
func (r *GoTypeResolver) lookupMethodLocked(pkgPath, typeName, methodName string, pointer bool) *types.Signature {
	pkg := r.loadPkgLocked(pkgPath)
	if pkg == nil {
		return nil
	}
	obj := pkg.Scope().Lookup(typeName)
	if obj == nil {
		return nil
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		return nil
	}
	var recv types.Type = named
	if pointer {
		recv = types.NewPointer(named)
	}
	m, _, _ := types.LookupFieldOrMethod(recv, true, pkg, methodName)
	if m == nil {
		return nil
	}
	fn, ok := m.(*types.Func)
	if !ok {
		return nil
	}
	return fn.Type().(*types.Signature)
}

// ReturnsError reports whether pkgPath.funcName has a signature whose last
// result type is the built-in error interface.
func (r *GoTypeResolver) ReturnsError(pkgPath, funcName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	sig := r.lookupFuncLocked(pkgPath, funcName)
	if sig == nil {
		return false
	}
	results := sig.Results()
	if results.Len() == 0 {
		return false
	}
	return isErrorType(results.At(results.Len() - 1).Type())
}

// ReturnsOnlyError reports whether pkgPath.funcName returns just error
// (single return value, no other results).
func (r *GoTypeResolver) ReturnsOnlyError(pkgPath, funcName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	sig := r.lookupFuncLocked(pkgPath, funcName)
	if sig == nil {
		return false
	}
	results := sig.Results()
	return results.Len() == 1 && isErrorType(results.At(0).Type())
}

// FuncReturnType extracts the first non-error return type from pkgPath.funcName.
// Returns the package path, type name, whether it's a pointer, and whether lookup succeeded.
func (r *GoTypeResolver) FuncReturnType(pkgPath, funcName string) (retPkgPath, retTypeName string, pointer bool, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sig := r.lookupFuncLocked(pkgPath, funcName)
	if sig == nil {
		return "", "", false, false
	}
	results := sig.Results()
	for i := 0; i < results.Len(); i++ {
		t := results.At(i).Type()
		if isErrorType(t) {
			continue
		}
		return extractNamedType(t)
	}
	return "", "", false, false
}

// MethodReturnsError checks if typeName.methodName in pkgPath returns error.
func (r *GoTypeResolver) MethodReturnsError(pkgPath, typeName, methodName string, pointer bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	sig := r.lookupMethodLocked(pkgPath, typeName, methodName, pointer)
	if sig == nil {
		return false
	}
	results := sig.Results()
	if results.Len() == 0 {
		return false
	}
	return isErrorType(results.At(results.Len() - 1).Type())
}

// MethodReturnsOnlyError checks if typeName.methodName returns just error.
func (r *GoTypeResolver) MethodReturnsOnlyError(pkgPath, typeName, methodName string, pointer bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	sig := r.lookupMethodLocked(pkgPath, typeName, methodName, pointer)
	if sig == nil {
		return false
	}
	results := sig.Results()
	return results.Len() == 1 && isErrorType(results.At(0).Type())
}

// extractNamedType extracts package path and type name from a types.Type.
func extractNamedType(t types.Type) (pkgPath, typeName string, pointer bool, ok bool) {
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		t = ptr.Elem()
		pointer = true
	}
	named, isNamed := t.(*types.Named)
	if !isNamed {
		return "", "", false, false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return "", "", false, false
	}
	return obj.Pkg().Path(), obj.Name(), pointer, true
}

// isErrorType checks if t is the built-in error interface.
func isErrorType(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() == nil && obj.Name() == "error"
}
