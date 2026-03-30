package codegen_go

import (
	"go/importer"
	"go/types"
	"sync"
)

// GoTypeResolver uses go/importer to introspect Go package signatures
// at transpile time. Answers questions like "does this function expect a pointer?"
// and "does this function return error?" without any external dependencies.
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

func (r *GoTypeResolver) loadPkg(pkgPath string) *types.Package {
	r.mu.Lock()
	defer r.mu.Unlock()
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

func (r *GoTypeResolver) lookupFunc(pkgPath, funcName string) *types.Signature {
	pkg := r.loadPkg(pkgPath)
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

// ParamIsPointer reports whether the i-th parameter of pkgPath.funcName is a pointer type.
func (r *GoTypeResolver) ParamIsPointer(pkgPath, funcName string, paramIndex int) bool {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return false
	}
	params := sig.Params()
	if paramIndex >= params.Len() {
		return false
	}
	_, isPtr := params.At(paramIndex).Type().(*types.Pointer)
	return isPtr
}

// ParamType returns the Go type string for the i-th parameter of pkgPath.funcName.
func (r *GoTypeResolver) ParamType(pkgPath, funcName string, paramIndex int) string {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return ""
	}
	params := sig.Params()
	if paramIndex >= params.Len() {
		return ""
	}
	return params.At(paramIndex).Type().String()
}

// ReturnsError reports whether pkgPath.funcName returns error as last result.
func (r *GoTypeResolver) ReturnsError(pkgPath, funcName string) bool {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return false
	}
	results := sig.Results()
	if results.Len() == 0 {
		return false
	}
	return isErrorType(results.At(results.Len() - 1).Type())
}

// FieldIsPointer reports whether a struct field is a pointer type.
func (r *GoTypeResolver) FieldIsPointer(pkgPath, typeName, fieldName string) bool {
	pkg := r.loadPkg(pkgPath)
	if pkg == nil {
		return false
	}
	obj := pkg.Scope().Lookup(typeName)
	if obj == nil {
		return false
	}
	structType, ok := obj.Type().Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < structType.NumFields(); i++ {
		f := structType.Field(i)
		if f.Name() == fieldName {
			_, isPtr := f.Type().(*types.Pointer)
			return isPtr
		}
	}
	return false
}

// IsType reports whether name is a type (not a function/variable) in pkgPath.
func (r *GoTypeResolver) IsType(pkgPath, name string) bool {
	pkg := r.loadPkg(pkgPath)
	if pkg == nil {
		return false
	}
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		return false
	}
	_, ok := obj.(*types.TypeName)
	return ok
}

// IsStruct reports whether name in pkgPath is a struct type (not interface/alias).
func (r *GoTypeResolver) IsStruct(pkgPath, name string) bool {
	pkg := r.loadPkg(pkgPath)
	if pkg == nil {
		return false
	}
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		return false
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return false
	}
	_, isStruct := tn.Type().Underlying().(*types.Struct)
	return isStruct
}

// FuncParamSignature returns the full Go type string for the i-th parameter.
// For callback params like func(ResponseWriter, *Request), returns the full signature.
func (r *GoTypeResolver) FuncParamSignature(pkgPath, funcName string, paramIndex int) string {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return ""
	}
	params := sig.Params()
	if paramIndex >= params.Len() {
		return ""
	}
	return params.At(paramIndex).Type().String()
}

// FuncParamCallbackSignature checks if the i-th param of pkgPath.funcName is a function type.
// If so, returns the param types of that callback (e.g., for http.HandleFunc param 1,
// returns ["net/http.ResponseWriter", "*net/http.Request"]).
func (r *GoTypeResolver) FuncParamCallbackSignature(pkgPath, funcName string, paramIndex int) []string {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return nil
	}
	params := sig.Params()
	if paramIndex >= params.Len() {
		return nil
	}
	// Check if param is a function type
	paramType := params.At(paramIndex).Type()
	fnSig, ok := paramType.(*types.Signature)
	if !ok {
		return nil
	}
	// Extract param types of the callback
	cbParams := fnSig.Params()
	var result []string
	for i := 0; i < cbParams.Len(); i++ {
		result = append(result, cbParams.At(i).Type().String())
	}
	return result
}

func isErrorType(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() == nil && obj.Name() == "error"
}
