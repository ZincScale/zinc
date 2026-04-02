package codegen_go

import (
	"go/importer"
	"go/types"
	"sync"

	"golang.org/x/tools/go/packages"
)

// GoTypeResolver introspects Go package signatures at transpile time.
// Uses go/packages for module dependencies and falls back to go/importer for stdlib.
type GoTypeResolver struct {
	imp      types.Importer
	cache    map[string]*types.Package
	negative map[string]bool // packages that failed to import
	mu       sync.Mutex
	dir      string // working directory with go.mod (for module resolution)
}

// NewGoTypeResolver creates a resolver backed by the default (gc) importer.
func NewGoTypeResolver() *GoTypeResolver {
	return &GoTypeResolver{
		imp:      importer.Default(),
		cache:    make(map[string]*types.Package),
		negative: make(map[string]bool),
	}
}

// SetDir sets the working directory for module resolution.
// Must point to a directory with a go.mod file.
func (r *GoTypeResolver) SetDir(dir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dir = dir
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
	// Try stdlib importer first (fast, no external tools needed)
	pkg, err := r.imp.Import(pkgPath)
	if err == nil {
		r.cache[pkgPath] = pkg
		return pkg
	}
	// Fall back to go/packages for module dependencies
	if r.dir != "" {
		pkg = r.loadPkgViaGoPackages(pkgPath)
		if pkg != nil {
			r.cache[pkgPath] = pkg
			return pkg
		}
	}
	r.negative[pkgPath] = true
	return nil
}

func (r *GoTypeResolver) loadPkgViaGoPackages(pkgPath string) *types.Package {
	cfg := &packages.Config{
		Mode: packages.NeedTypes | packages.NeedImports,
		Dir:  r.dir,
	}
	pkgs, err := packages.Load(cfg, pkgPath)
	if err != nil || len(pkgs) == 0 {
		return nil
	}
	if pkgs[0].Types == nil {
		return nil
	}
	return pkgs[0].Types
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

// FuncReturnType returns the Go type of the first return value of pkgPath.funcName.
func (r *GoTypeResolver) FuncReturnType(pkgPath, funcName string) types.Type {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return nil
	}
	results := sig.Results()
	if results.Len() == 0 {
		return nil
	}
	return results.At(0).Type()
}

// MethodReturnsErrorOnly reports whether typ.methodName returns exactly one value and it's error.
func (r *GoTypeResolver) MethodReturnsErrorOnly(typ types.Type, methodName string) bool {
	if typ == nil {
		return false
	}
	mset := types.NewMethodSet(typ)
	sel := mset.Lookup(nil, methodName)
	if sel == nil {
		if _, isPtr := typ.(*types.Pointer); !isPtr {
			mset = types.NewMethodSet(types.NewPointer(typ))
			sel = mset.Lookup(nil, methodName)
		}
	}
	if sel == nil {
		return false
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Type().(*types.Signature)
	results := sig.Results()
	return results.Len() == 1 && isErrorType(results.At(0).Type())
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

// HasFunc reports whether pkgPath has a function named funcName.
func (r *GoTypeResolver) HasFunc(pkgPath, funcName string) bool {
	return r.lookupFunc(pkgPath, funcName) != nil
}

// FuncParamSignature returns the full Go type string for the i-th parameter.
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

// FuncParamCallbackSignature checks if the i-th param is a function type.
// Returns the param types of that callback.
func (r *GoTypeResolver) FuncParamCallbackSignature(pkgPath, funcName string, paramIndex int) []string {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return nil
	}
	params := sig.Params()
	if paramIndex >= params.Len() {
		return nil
	}
	paramType := params.At(paramIndex).Type()
	fnSig, ok := paramType.(*types.Signature)
	if !ok {
		return nil
	}
	cbParams := fnSig.Params()
	var result []string
	for i := 0; i < cbParams.Len(); i++ {
		result = append(result, cbParams.At(i).Type().String())
	}
	return result
}

// ParamIsBytes reports whether the i-th parameter is []byte.
func (r *GoTypeResolver) ParamIsBytes(pkgPath, funcName string, paramIndex int) bool {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return false
	}
	params := sig.Params()
	if paramIndex >= params.Len() {
		return false
	}
	slice, ok := params.At(paramIndex).Type().(*types.Slice)
	if !ok {
		return false
	}
	basic, ok := slice.Elem().(*types.Basic)
	return ok && basic.Kind() == types.Byte
}

// FuncReturnsPointer reports whether the first return value is a pointer.
func (r *GoTypeResolver) FuncReturnsPointer(pkgPath, funcName string) bool {
	retType := r.FuncReturnType(pkgPath, funcName)
	if retType == nil {
		return false
	}
	_, isPtr := retType.(*types.Pointer)
	return isPtr
}

// ExprReturnsPointer checks if a call returns a pointer (function or method).
func (r *GoTypeResolver) ExprReturnsPointer(pkgPath, funcName string, receiverType types.Type) bool {
	if pkgPath != "" {
		return r.FuncReturnsPointer(pkgPath, funcName)
	}
	if receiverType != nil {
		mset := types.NewMethodSet(receiverType)
		sel := mset.Lookup(nil, funcName)
		if sel == nil {
			if _, isPtr := receiverType.(*types.Pointer); !isPtr {
				mset = types.NewMethodSet(types.NewPointer(receiverType))
				sel = mset.Lookup(nil, funcName)
			}
		}
		if sel != nil {
			if fn, ok := sel.Obj().(*types.Func); ok {
				sig := fn.Type().(*types.Signature)
				results := sig.Results()
				if results.Len() > 0 {
					_, isPtr := results.At(0).Type().(*types.Pointer)
					return isPtr
				}
			}
		}
	}
	return false
}

// ReturnsErrorOnly reports whether pkgPath.funcName returns exactly one value and it's error.
func (r *GoTypeResolver) ReturnsErrorOnly(pkgPath, funcName string) bool {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return false
	}
	results := sig.Results()
	return results.Len() == 1 && isErrorType(results.At(0).Type())
}

// implicitPointerParams lists Go functions where a parameter must be
// passed by pointer even though the signature uses interface{}.
var implicitPointerParams = map[string]map[int]bool{
	"encoding/json.Unmarshal": {1: true},
	"encoding/xml.Unmarshal":  {1: true},
	"fmt.Scan":                {0: true},
	"fmt.Scanln":              {0: true},
	"fmt.Scanf":               {1: true},
	"fmt.Sscan":               {1: true},
	"fmt.Sscanln":             {1: true},
	"fmt.Sscanf":              {2: true},
	"fmt.Fscan":               {1: true},
	"fmt.Fscanln":             {1: true},
	"fmt.Fscanf":              {2: true},
}

// NeedsPointerArg reports whether the i-th argument needs & inserted.
func (r *GoTypeResolver) NeedsPointerArg(pkgPath, funcName string, paramIndex int) bool {
	if r.ParamIsPointer(pkgPath, funcName, paramIndex) {
		return true
	}
	key := pkgPath + "." + funcName
	if params, ok := implicitPointerParams[key]; ok {
		return params[paramIndex]
	}
	return false
}

func isErrorType(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() == nil && obj.Name() == "error"
}
