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

// FuncReturnType returns the full Go type string of the first return value of pkgPath.funcName.
// Used to track variable types from Go stdlib calls (e.g. exec.Command → *exec.Cmd).
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

// MethodReturnsErrorOnly reports whether typObj.methodName returns exactly one value and it's error.
// typObj is a types.Type obtained from FuncReturnType or similar.
func (r *GoTypeResolver) MethodReturnsErrorOnly(typ types.Type, methodName string) bool {
	if typ == nil {
		return false
	}
	// Check method set of the type (and pointer-to-type)
	mset := types.NewMethodSet(typ)
	sel := mset.Lookup(nil, methodName)
	if sel == nil {
		// Try pointer-to-type if not a pointer already
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

// ParamIsBytes reports whether the i-th parameter of pkgPath.funcName is []byte.
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

// ReturnsErrorOnly reports whether pkgPath.funcName returns exactly one value and it's error.
// e.g. json.Unmarshal returns just error, not (value, error).
func (r *GoTypeResolver) ReturnsErrorOnly(pkgPath, funcName string) bool {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return false
	}
	results := sig.Results()
	return results.Len() == 1 && isErrorType(results.At(0).Type())
}

// implicitPointerParams lists Go stdlib functions where a parameter must be
// passed by pointer even though the signature uses interface{}.
// Key: "pkg/path.FuncName", Value: set of 0-based parameter indices that need &.
// If this table exceeds ~50 entries, revisit with a more general solution.
var implicitPointerParams = map[string]map[int]bool{
	// encoding
	"encoding/json.Unmarshal": {1: true},
	"encoding/xml.Unmarshal":  {1: true},

	// fmt scanning
	"fmt.Scan":    {0: true},
	"fmt.Scanln":  {0: true},
	"fmt.Scanf":   {1: true},
	"fmt.Sscan":   {1: true},
	"fmt.Sscanln": {1: true},
	"fmt.Sscanf":  {2: true},
	"fmt.Fscan":   {1: true},
	"fmt.Fscanln": {1: true},
	"fmt.Fscanf":  {2: true},
}

// NeedsPointerArg reports whether the i-th argument of pkg.func needs & inserted.
// Checks both the Go type signature (explicit *T params) and the implicit pointer table.
func (r *GoTypeResolver) NeedsPointerArg(pkgPath, funcName string, paramIndex int) bool {
	// Check explicit pointer params via Go type introspection
	if r.ParamIsPointer(pkgPath, funcName, paramIndex) {
		return true
	}
	// Check implicit pointer table
	key := pkgPath + "." + funcName
	if params, ok := implicitPointerParams[key]; ok {
		return params[paramIndex]
	}
	return false
}

// FuncReturnsPointer reports whether the first return value of pkgPath.funcName is a pointer.
// Used to avoid double-pointer: if slog.New() returns *Logger, don't add & when passing to SetDefault(*Logger).
func (r *GoTypeResolver) FuncReturnsPointer(pkgPath, funcName string) bool {
	retType := r.FuncReturnType(pkgPath, funcName)
	if retType == nil {
		return false
	}
	_, isPtr := retType.(*types.Pointer)
	return isPtr
}

// ExprReturnsPointer checks if a call expression's return type is already a pointer.
// Handles both package functions (slog.New) and methods (obj.Method).
func (r *GoTypeResolver) ExprReturnsPointer(pkgPath, funcName string, receiverType types.Type) bool {
	if pkgPath != "" {
		// Package function
		return r.FuncReturnsPointer(pkgPath, funcName)
	}
	if receiverType != nil {
		// Method call — look up method return type
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

func isErrorType(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() == nil && obj.Name() == "error"
}
