package codegen_go

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

// GoTypeResolver introspects Go package signatures at transpile time.
// Uses go/importer for stdlib, go/packages for modules, AST parsing as fallback.
type GoTypeResolver struct {
	imp      types.Importer
	cache    map[string]*types.Package
	astCache map[string]*ast.Package // fallback: parsed AST without type checking
	negative map[string]bool
	mu       sync.Mutex
	dir      string // working directory with go.mod
}

func NewGoTypeResolver() *GoTypeResolver {
	return &GoTypeResolver{
		imp:      importer.Default(),
		cache:    make(map[string]*types.Package),
		astCache: make(map[string]*ast.Package),
		negative: make(map[string]bool),
	}
}

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
	// Try stdlib importer first (fast)
	pkg, err := r.imp.Import(pkgPath)
	if err == nil {
		r.cache[pkgPath] = pkg
		return pkg
	}
	// Try go/packages for module dependencies
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
	pkg := pkgs[0]
	// Errors here mean packages.Load couldn't fully resolve — usually
	// "no required module provides package X" when the dep is in
	// go.mod but the in-process module state is stale. Reject so the
	// caller doesn't read garbage from an empty scope.
	if len(pkg.Errors) > 0 || pkg.Types == nil || pkg.Types.Scope().Len() == 0 {
		return nil
	}
	return pkg.Types
}

// loadAST parses Go source files in a directory to find type/function declarations.
// Used as fallback when full type resolution fails (e.g., transitive deps missing).
func (r *GoTypeResolver) loadAST(pkgPath string) *ast.Package {
	if pkg, ok := r.astCache[pkgPath]; ok {
		return pkg
	}
	if r.dir == "" {
		return nil
	}
	// Resolve package directory via go list
	// For replace directives, go list can find the actual directory
	dir := r.resolvePackageDir(pkgPath)
	if dir == "" {
		return nil
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !isTestFile(fi.Name())
	}, 0)
	if err != nil || len(pkgs) == 0 {
		return nil
	}
	for _, pkg := range pkgs {
		r.astCache[pkgPath] = pkg
		return pkg
	}
	return nil
}

func (r *GoTypeResolver) resolvePackageDir(pkgPath string) string {
	// Check common patterns:
	// 1. Local subpackage: pkgPath relative to dir
	local := filepath.Join(r.dir, pkgPath)
	if isDir(local) {
		return local
	}
	// 2. Module with replace: parse go.mod for replace directives
	gomod := filepath.Join(r.dir, "go.mod")
	data, _ := os.ReadFile(gomod)
	// Simple line-based go.mod parser for replace directives
	lines := splitLines(string(data))
	for _, line := range lines {
		line = trimSpace(line)
		// Strip "replace" keyword prefix
		if hasPrefix(line, "replace ") {
			line = trimSpace(line[8:])
		}
		// Look for: modulePath => localPath
		if idx := indexOf(line, "=>"); idx > 0 {
			modPath := trimSpace(line[:idx])
			localPath := trimSpace(line[idx+2:])
			// Strip version suffix from modPath (e.g., "module v0.0.0" → "module")
			if spaceIdx := indexOf(modPath, " "); spaceIdx > 0 {
				modPath = modPath[:spaceIdx]
			}
			// Check if pkgPath starts with the replaced module
			if hasPrefix(pkgPath, modPath) {
				subPkg := pkgPath[len(modPath):]
				resolved := filepath.Join(localPath, subPkg)
				if isDir(resolved) {
					return resolved
				}
			}
		}
	}
	// 3. Module cache: $GOMODCACHE/<pkgPath>@<version>/. The transpiler
	// runs codegen BEFORE `go mod tidy` populates zinc-out's go.mod, so
	// loadPkgViaGoPackages can't see the dep. But the dep was likely
	// downloaded by a previous build (or by `zinc-go add`), so its source
	// sits in the user's module cache. Look there as a last resort.
	if cached := r.resolveFromModCache(pkgPath); cached != "" {
		return cached
	}
	return ""
}

// resolveFromModCache looks in $GOMODCACHE for a checked-out version of
// pkgPath. Picks the highest version when multiple exist. Returns "" if
// no match found.
func (r *GoTypeResolver) resolveFromModCache(pkgPath string) string {
	modCache := goModCacheDir()
	if modCache == "" {
		return ""
	}
	// Walk up the path looking for a `<segment>@<version>` directory
	// match. We try from the longest prefix to the shortest because Go
	// modules can sub-package (e.g. github.com/hamba/avro/v2 → the
	// `v2` is part of the import path, not the version).
	parts := strings.Split(pkgPath, "/")
	for cut := len(parts); cut > 0; cut-- {
		modPath := strings.Join(parts[:cut], "/")
		subPath := strings.Join(parts[cut:], "/")
		// Module dirs in the cache use ! to escape uppercase letters in
		// their original path; we only call into ASCII-lowercase paths
		// (github.com/foo/bar) so this is mostly cosmetic, but keep
		// the simple form to avoid pulling in module/cache.
		parent := filepath.Join(modCache, filepath.FromSlash(modPath))
		parentDir := filepath.Dir(parent)
		base := filepath.Base(parent)
		entries, err := os.ReadDir(parentDir)
		if err != nil {
			continue
		}
		var matches []string
		prefix := base + "@"
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
				matches = append(matches, e.Name())
			}
		}
		if len(matches) == 0 {
			continue
		}
		sort.Strings(matches) // lexicographic — close to semver for vN.N.N tags
		picked := matches[len(matches)-1]
		resolved := filepath.Join(parentDir, picked, filepath.FromSlash(subPath))
		if isDir(resolved) {
			return resolved
		}
	}
	return ""
}

// goModCacheDir returns $GOMODCACHE, falling back to $GOPATH/pkg/mod
// then ~/go/pkg/mod. Empty string if neither resolves.
func goModCacheDir() string {
	if v := os.Getenv("GOMODCACHE"); v != "" {
		return v
	}
	// `go env GOMODCACHE` is the canonical answer; fall through to env
	// vars if the binary isn't on PATH for some reason.
	if out, err := exec.Command("go", "env", "GOMODCACHE").Output(); err == nil {
		v := strings.TrimSpace(string(out))
		if v != "" {
			return v
		}
	}
	if v := os.Getenv("GOPATH"); v != "" {
		return filepath.Join(v, "pkg", "mod")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "go", "pkg", "mod")
	}
	return ""
}

// hasStructDecl checks if the AST has a struct type declaration with the given name.
func (r *GoTypeResolver) hasStructDecl(pkgPath, name string) bool {
	astPkg := r.loadAST(pkgPath)
	if astPkg == nil {
		return false
	}
	for _, file := range astPkg.Files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if typeSpec.Name.Name == name {
					_, isStruct := typeSpec.Type.(*ast.StructType)
					return isStruct
				}
			}
		}
	}
	return false
}

// hasFuncDecl checks if the AST has a function declaration with the given name.
func (r *GoTypeResolver) hasFuncDecl(pkgPath, name string) bool {
	astPkg := r.loadAST(pkgPath)
	if astPkg == nil {
		return false
	}
	for _, file := range astPkg.Files {
		for _, decl := range file.Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if funcDecl.Name.Name == name {
				return true
			}
		}
	}
	return false
}

// --- Public API ---

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

func (r *GoTypeResolver) FuncReturnType(pkgPath, funcName string) types.Type {
	return r.FuncReturnTypeAt(pkgPath, funcName, 0)
}

// FuncReturnTypeAt returns the i-th return slot's type (0-based) or nil if
// the function doesn't have that many results. Used by the multi-value
// var-decl path to track Go types per slot.
func (r *GoTypeResolver) FuncReturnTypeAt(pkgPath, funcName string, idx int) types.Type {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return nil
	}
	results := sig.Results()
	if idx < 0 || idx >= results.Len() {
		return nil
	}
	return results.At(idx).Type()
}

// MethodReturnTypeAt returns the i-th return slot's Go type for a method
// on a Go-resolved receiver type. Falls back to *T method-set when the
// value-receiver method-set doesn't have it.
func (r *GoTypeResolver) MethodReturnTypeAt(typ types.Type, methodName string, idx int) types.Type {
	if typ == nil {
		return nil
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
		return nil
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		return nil
	}
	sig := fn.Type().(*types.Signature)
	results := sig.Results()
	if idx < 0 || idx >= results.Len() {
		return nil
	}
	return results.At(idx).Type()
}

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

// IsType reports whether name is a type in pkgPath.
func (r *GoTypeResolver) IsType(pkgPath, name string) bool {
	pkg := r.loadPkg(pkgPath)
	if pkg != nil {
		obj := pkg.Scope().Lookup(name)
		if obj != nil {
			_, ok := obj.(*types.TypeName)
			return ok
		}
	}
	return false
}

// IsStruct reports whether name is a struct type in pkgPath.
// Falls back to AST parsing when type resolution fails.
func (r *GoTypeResolver) IsStruct(pkgPath, name string) bool {
	pkg := r.loadPkg(pkgPath)
	if pkg != nil {
		obj := pkg.Scope().Lookup(name)
		if obj != nil {
			tn, ok := obj.(*types.TypeName)
			if ok {
				_, isStruct := tn.Type().Underlying().(*types.Struct)
				return isStruct
			}
		}
	}
	// Fallback: parse AST directly (no type checking needed)
	return r.hasStructDecl(pkgPath, name)
}

// IsInterface reports whether name is an interface type in pkgPath.
// Used by zero-value selection — interface types are zero-valued as
// `nil`, not `Type{}` which the Go compiler rejects.
func (r *GoTypeResolver) IsInterface(pkgPath, name string) bool {
	pkg := r.loadPkg(pkgPath)
	if pkg != nil {
		obj := pkg.Scope().Lookup(name)
		if obj != nil {
			tn, ok := obj.(*types.TypeName)
			if ok {
				_, isIface := tn.Type().Underlying().(*types.Interface)
				return isIface
			}
		}
	}
	// Fallback: parse AST. Mirrors hasStructDecl. Without this,
	// zeroValueFor on a third-party-package interface type falls
	// through to the `goType + "{}"` branch and emits an invalid
	// composite literal (`pkg.Iface{}`) — Go rejects composite
	// literals on interfaces. The most visible breakage is on
	// fallthrough returns after exhaustive `match`, where codegen
	// inserts a zero-value return at the end of the function body.
	return r.hasInterfaceDeclAST(pkgPath, name)
}

// hasInterfaceDeclAST is the AST-level companion to IsInterface.
func (r *GoTypeResolver) hasInterfaceDeclAST(pkgPath, name string) bool {
	astPkg := r.loadAST(pkgPath)
	if astPkg == nil {
		return false
	}
	for _, file := range astPkg.Files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if typeSpec.Name.Name == name {
					_, isIface := typeSpec.Type.(*ast.InterfaceType)
					return isIface
				}
			}
		}
	}
	return false
}

// HasFunc reports whether pkgPath has a function named funcName.
// Falls back to AST parsing when type resolution fails.
func (r *GoTypeResolver) HasFunc(pkgPath, funcName string) bool {
	if r.lookupFunc(pkgPath, funcName) != nil {
		return true
	}
	// Fallback: parse AST
	return r.hasFuncDecl(pkgPath, funcName)
}

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

func (r *GoTypeResolver) FuncReturnsPointer(pkgPath, funcName string) bool {
	retType := r.FuncReturnType(pkgPath, funcName)
	if retType == nil {
		return false
	}
	_, isPtr := retType.(*types.Pointer)
	return isPtr
}

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

func (r *GoTypeResolver) ReturnsErrorOnly(pkgPath, funcName string) bool {
	sig := r.lookupFunc(pkgPath, funcName)
	if sig == nil {
		return false
	}
	results := sig.Results()
	return results.Len() == 1 && isErrorType(results.At(0).Type())
}

// HasPointerReceiverMethods reports whether typeName in pkgPath has any methods
// with pointer receivers. If so, the type is designed to be used as *T.
func (r *GoTypeResolver) HasPointerReceiverMethods(pkgPath, typeName string) bool {
	pkg := r.loadPkg(pkgPath)
	if pkg != nil {
		obj := pkg.Scope().Lookup(typeName)
		if obj != nil {
			tn, ok := obj.(*types.TypeName)
			if ok {
				// Check method set of *T — if it has more methods than T,
				// the extra ones have pointer receivers.
				valMethods := types.NewMethodSet(tn.Type())
				ptrMethods := types.NewMethodSet(types.NewPointer(tn.Type()))
				return ptrMethods.Len() > valMethods.Len()
			}
		}
	}
	// Fallback: parse AST directly. loadPkg can fail for legitimate reasons
	// — go.mod hasn't been written by the transpiler yet, the dep is fresh,
	// the package isn't in the build cache. Without this fallback, every
	// formatType call site that gates pointerization on
	// HasPointerReceiverMethods silently emits non-pointer types, which
	// produces broken Go code (e.g. `[]hambaAvro.Field{}` when hamba's
	// NewField returns *Field). Mirror hasStructDecl's AST walk and look
	// for any FuncDecl whose receiver is *typeName.
	return r.hasPointerReceiverMethodAST(pkgPath, typeName)
}

// hasPointerReceiverMethodAST is the AST-level companion to
// HasPointerReceiverMethods. Returns true if the package has at least one
// method declaration with a pointer receiver on the named type.
func (r *GoTypeResolver) hasPointerReceiverMethodAST(pkgPath, typeName string) bool {
	astPkg := r.loadAST(pkgPath)
	if astPkg == nil {
		return false
	}
	for _, file := range astPkg.Files {
		for _, decl := range file.Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if !ok || funcDecl.Recv == nil || len(funcDecl.Recv.List) == 0 {
				continue
			}
			recvType := funcDecl.Recv.List[0].Type
			star, isPtr := recvType.(*ast.StarExpr)
			if !isPtr {
				continue
			}
			ident, ok := star.X.(*ast.Ident)
			if !ok {
				continue
			}
			if ident.Name == typeName {
				return true
			}
		}
	}
	return false
}

// NeedsPointerArg reports whether the i-th parameter of pkgPath.funcName
// has an explicit pointer (*T) in its Go signature. Used by codegen to
// auto-insert `&` at the call site for typed-pointer params.
//
// We deliberately do NOT carry a hand-curated table of `any`-typed funcs
// whose runtime contract requires a pointer (e.g. encoding/json.Unmarshal,
// fmt.Scan, hamba/avro.Unmarshal). Those used to live in a map here — they
// were a maintenance debt and an admission that the answer couldn't be
// derived from tooling. The replacement is the explicit prefix `&x`
// operator at the call site: the user signals the pointer requirement
// directly when the type system has no information.
func (r *GoTypeResolver) NeedsPointerArg(pkgPath, funcName string, paramIndex int) bool {
	return r.ParamIsPointer(pkgPath, funcName, paramIndex)
}

// ListExports returns all exported names from a Go package with their kind.
// Kind is "func", "type", "var", or "const".
func (r *GoTypeResolver) ListExports(pkgPath string) map[string]string {
	pkg := r.loadPkg(pkgPath)
	if pkg == nil {
		return nil
	}
	exports := make(map[string]string)
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		if len(name) == 0 || name[0] < 'A' || name[0] > 'Z' {
			continue // unexported
		}
		obj := scope.Lookup(name)
		switch obj.(type) {
		case *types.Func:
			exports[name] = "func"
		case *types.TypeName:
			exports[name] = "type"
		case *types.Var:
			exports[name] = "var"
		case *types.Const:
			exports[name] = "const"
		}
	}
	return exports
}

func isErrorType(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() == nil && obj.Name() == "error"
}

// --- Helpers ---

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func isTestFile(name string) bool {
	return len(name) > 8 && name[len(name)-8:] == "_test.go"
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
