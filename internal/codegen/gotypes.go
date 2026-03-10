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

// ReturnsError reports whether pkgPath.funcName has a signature whose last
// result type is the built-in error interface.
func (r *GoTypeResolver) ReturnsError(pkgPath, funcName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.negative[pkgPath] {
		return false
	}

	pkg, ok := r.cache[pkgPath]
	if !ok {
		var err error
		pkg, err = r.imp.Import(pkgPath)
		if err != nil {
			r.negative[pkgPath] = true
			return false
		}
		r.cache[pkgPath] = pkg
	}

	obj := pkg.Scope().Lookup(funcName)
	if obj == nil {
		return false
	}

	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}

	sig := fn.Type().(*types.Signature)
	results := sig.Results()
	if results.Len() == 0 {
		return false
	}

	last := results.At(results.Len() - 1).Type()
	return isErrorType(last)
}

// ReturnsOnlyError reports whether pkgPath.funcName returns just error
// (single return value, no other results).
func (r *GoTypeResolver) ReturnsOnlyError(pkgPath, funcName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.negative[pkgPath] {
		return false
	}

	pkg, ok := r.cache[pkgPath]
	if !ok {
		var err error
		pkg, err = r.imp.Import(pkgPath)
		if err != nil {
			r.negative[pkgPath] = true
			return false
		}
		r.cache[pkgPath] = pkg
	}

	obj := pkg.Scope().Lookup(funcName)
	if obj == nil {
		return false
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Type().(*types.Signature)
	results := sig.Results()
	return results.Len() == 1 && isErrorType(results.At(0).Type())
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
