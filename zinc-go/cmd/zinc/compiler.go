package main

// Compilation pipeline: parsing, multi-file merging, subpackage compilation.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	codegen "zinc-go/internal/codegen_go"
	"zinc-go/internal/lexer"
	"zinc-go/internal/parser"
	"zinc-go/internal/typechecker"
)

// runTypecheck collects signatures across all programs, runs the bind
// phase to resolve every Ident to a Symbol, then runs CheckV2 on each.
// Returns the bound programs (keyed by parser.Program pointer identity)
// + aggregated V2Errors from both passes. Caller decides whether to fail.
//
// `crossPkgExports` provides the bind phase's ZincSubpkgExports map —
// alias → exported name → kind. The caller (compileSubpackages) builds
// this from `allExports` so cross-package enum variants and types
// resolve correctly. Pass nil for single-package compileMultiFile usage.
//
// `goModDir` is the dir holding go.mod (or about to). When non-empty, a
// GoTypeResolver is constructed and supplied to CheckV2 so Go-imported
// package calls get GoType-tagged returns in the NodeTypes side-map.
// `crossPkgFnDecls` provides cross-package top-level function decls so
// the typechecker can resolve `pkg.func(...)` qualified calls. Bare-name
// lookup matches (one fnSigs map shared across packages) so callers
// pass a flat map; collisions are rare in practice and resolved by
// last-write-wins (mirrors the codegen-side unqualifiedNames behavior).
func runTypecheck(progs []*parser.Program, importMap map[string]string,
	crossPkgExports map[string]map[string]string, goModDir string,
	crossPkgClassDecls map[string]map[string]*parser.ClassDecl,
	crossPkgFnDecls map[string]*parser.FnDecl) (
	map[*parser.Program]*typechecker.BoundProgram, []typechecker.V2Error) {
	if len(progs) == 0 {
		return nil, nil
	}

	// Phase 3.5.2 — Go-FFI resolver shared across this typecheck pass.
	// Lazily created when both importMap and goModDir are available.
	var ffi typechecker.GoFFIResolver
	if importMap != nil && goModDir != "" {
		r := codegen.NewGoTypeResolver()
		r.SetDir(goModDir)
		ffi = r
	}
	merged := mergePrograms(progs)
	externalSigs := typechecker.CollectSignatures(merged)
	// 3.7.2: feed cross-package class names into externalSigs so CallExpr
	// ctor inference fires for `OtherPkg.Class(...)` (or unqualified
	// `Class(...)` when the import didn't collide). Without this,
	// cross-pkg class instantiation infers to typeAny and breaks
	// downstream side-map lookups (e.g. resolveReceiverClassName).
	if crossPkgExports != nil {
		if externalSigs.ClassNames == nil {
			externalSigs.ClassNames = make(map[string]bool)
		}
		if externalSigs.InterfaceNames == nil {
			externalSigs.InterfaceNames = make(map[string]bool)
		}
		for _, exports := range crossPkgExports {
			for name, kind := range exports {
				switch kind {
				case "class", "data", "interface", "enum", "sealed_variant":
					externalSigs.ClassNames[name] = true
				}
				if kind == "interface" {
					externalSigs.InterfaceNames[name] = true
				}
			}
		}
		// Note: deliberately NOT merging cross-pkg data-class names into
		// DataClassNames — that set drives the implicit-ctor branch
		// (Type(args) → NewType(args), unqualified). Cross-pkg names
		// must always emit through the qualified path (pkg.NewType).
	}
	// 3.7.2: feed cross-package parent relationships (class inheritance,
	// sealed-variant ownership) so subtype compatibility (`Shape s = Circle(...)`)
	// resolves across packages.
	if crossPkgClassDecls != nil {
		if externalSigs.ParentTypes == nil {
			externalSigs.ParentTypes = make(map[string][]string)
		}
		if externalSigs.MethodSigs == nil {
			externalSigs.MethodSigs = make(map[string]map[string]typechecker.V2FnSig)
		}
		for _, classes := range crossPkgClassDecls {
			for _, cls := range classes {
				if len(cls.Parents) > 0 {
					names := make([]string, len(cls.Parents))
					for i, p := range cls.Parents {
						names[i] = p.Name
					}
					externalSigs.ParentTypes[cls.Name] = append(externalSigs.ParentTypes[cls.Name], names...)
				}
				if cls.IsSealed {
					for _, v := range cls.Variants {
						externalSigs.ParentTypes[v.Name] = append(externalSigs.ParentTypes[v.Name], cls.Name)
						externalSigs.ClassNames[v.Name] = true
					}
				}
				if len(cls.Methods) > 0 {
					methods := externalSigs.MethodSigs[cls.Name]
					if methods == nil {
						methods = make(map[string]typechecker.V2FnSig, len(cls.Methods))
					}
					for _, m := range cls.Methods {
						methods[m.Name] = typechecker.MakeFnSigForMethod(m)
					}
					externalSigs.MethodSigs[cls.Name] = methods
				}
			}
		}
	}
	// Cross-pkg top-level FnDecls — register their signatures so a call
	// like `expressions.compile(src)` from another package resolves to
	// the proper return type (instead of falling through to typeAny and
	// breaking the bound side-map / codegen pointer detection).
	if len(crossPkgFnDecls) > 0 {
		if externalSigs.FnSigs == nil {
			externalSigs.FnSigs = make(map[string]typechecker.V2FnSig)
		}
		for name, fn := range crossPkgFnDecls {
			if _, already := externalSigs.FnSigs[name]; already {
				continue
			}
			externalSigs.FnSigs[name] = typechecker.MakeFnSigForFn(fn)
		}
	}

	// Build the bind context: same-package siblings + cross-package imports.
	bindCtx := typechecker.CollectBindContext(merged)
	if crossPkgExports != nil {
		for alias, exports := range crossPkgExports {
			bindCtx.ZincSubpkgExports[alias] = exports
		}
	}
	for _, prog := range progs {
		// Each file's own ImportAliases set: aliases used in `import alias`
		// declarations. We mark every imported alias as "in use" so the
		// bind resolver considers cross-pkg matches under that alias.
		for _, imp := range prog.Imports {
			alias := imp.Alias
			if alias == "" {
				// Last segment of the import path is the implicit alias.
				parts := strings.Split(imp.Path, "/")
				alias = parts[len(parts)-1]
				parts = strings.Split(alias, ".")
				alias = parts[len(parts)-1]
			}
			bindCtx.ImportAliases[alias] = true
		}
	}
	// Phase 3.3 stub: GoPkgExports left empty. Go-imported package symbols
	// (hambaAvro.Schema, etc.) aren't yet introspected at the bind layer.
	// Phase 3.5 wires this through `goResolver`.
	_ = importMap

	bps := make(map[*parser.Program]*typechecker.BoundProgram)
	var allErrors []typechecker.V2Error
	for _, prog := range progs {
		bp, bindErrs := typechecker.Bind(prog, bindCtx)
		// Attach the package-level CollectedSigs aggregate. Every program
		// in the same package shares the same pointer — externalSigs is
		// already cross-file (and cross-pkg via the additions above), so
		// codegen reads one canonical source instead of rebuilding parallel
		// per-file maps.
		bp.Sigs = &externalSigs
		bps[prog] = bp
		allErrors = append(allErrors, bindErrs...)
	}
	for _, prog := range progs {
		// Phase 3.5: pass importMap (alias→pkgPath) and the FFI resolver
		// so CheckV2 can tag return types of Go-imported calls with GoType.
		// Codegen consumes via the BoundProgram side-map.
		checkErrs, nodeTypes := typechecker.CheckV2WithContextAndNodes(
			prog, &externalSigs, importMap, ffi)
		allErrors = append(allErrors, checkErrs...)
		if bp, ok := bps[prog]; ok {
			bp.NodeTypes = nodeTypes
		}
	}
	return bps, allErrors
}

// parseFile reads and parses a .zn file, returning the AST.
func parseFile(path string) (*parser.Program, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	l := lexer.New(string(src))
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		return nil, fmt.Errorf("lex errors in %s:\n%s", path, strings.Join(l.Errors, "\n"))
	}

	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		return nil, fmt.Errorf("parse errors in %s:\n%s", path, strings.Join(p.Errors, "\n"))
	}

	absPath, _ := filepath.Abs(path)
	prog.SourceFile = absPath
	return prog, nil
}

// compileFile reads a .zn file, parses it, and generates Go source.
func compileFile(path, goModDir string, importAliases ...map[string]string) ([]codegen.OutputFile, error) {
	prog, err := parseFile(path)
	if err != nil {
		return nil, err
	}

	className := strings.TrimSuffix(filepath.Base(path), ".zn")
	if len(className) > 0 {
		className = strings.ToUpper(className[:1]) + className[1:]
	}

	gen := codegen.New()
	gen.SetSourceFile(prog.SourceFile)
	// goModDir is the directory containing (or about to contain) go.mod for
	// the generated output. The codegen resolver uses it to locate third-
	// party deps (via go.mod replace directives or the module cache) when
	// deciding whether to pointerize qualified type references like
	// `pkg.Type` → `*pkg.Type`. Without this, all third-party qualified
	// types silently emit as value types, breaking any List<pkg.T> where
	// pkg.T's constructor returns *T.
	if goModDir != "" {
		gen.SetGoModDir(goModDir)
	}
	var im map[string]string
	if len(importAliases) > 0 && importAliases[0] != nil {
		im = importAliases[0]
		gen.SetImportAliases(im)
	}
	// 3.7.2: build importMap from this file's `import` directives so the
	// typechecker's FFI resolver can resolve `pkg.Func()` for single-file
	// scripts (compileFile path — no zinc.toml deps). The parser stores
	// paths with dots (`encoding.json`); convert to slashes for the
	// Go-import shape the resolver expects.
	if im == nil {
		im = make(map[string]string)
	}
	for _, imp := range prog.Imports {
		goPath := strings.ReplaceAll(imp.Path, ".", "/")
		alias := imp.Alias
		if alias == "" {
			parts := strings.Split(goPath, "/")
			alias = parts[len(parts)-1]
		}
		if _, exists := im[alias]; !exists {
			im[alias] = goPath
		}
	}
	bps, tcErrors := runTypecheck([]*parser.Program{prog}, im, nil, goModDir, nil, nil)
	if len(tcErrors) > 0 {
		for _, e := range tcErrors {
			fmt.Fprintf(os.Stderr, "typecheck: %s\n", e.String())
		}
		return nil, fmt.Errorf("%d typecheck error(s)", len(tcErrors))
	}
	if bp, ok := bps[prog]; ok {
		gen.SetBoundProgram(bp)
	}
	files := gen.GenerateFiles(prog, className)
	for _, w := range gen.CompileWarnings() {
		fmt.Fprintln(os.Stderr, w)
	}
	if errs := gen.CompileErrors(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, e)
		}
		return nil, fmt.Errorf("%d compile error(s)", len(errs))
	}
	return files, nil
}

// mergePrograms combines multiple parsed Programs into one.
// Imports are deduplicated, all Decls and Stmts are concatenated.
func mergePrograms(progs []*parser.Program) *parser.Program {
	merged := &parser.Program{}
	seen := make(map[string]bool)
	for _, p := range progs {
		if merged.SourceFile == "" {
			merged.SourceFile = p.SourceFile
		}
		if merged.Package == nil && p.Package != nil {
			merged.Package = p.Package
		}
		for _, imp := range p.Imports {
			if !seen[imp.Path] {
				seen[imp.Path] = true
				merged.Imports = append(merged.Imports, imp)
			}
		}
		merged.Decls = append(merged.Decls, p.Decls...)
		merged.Stmts = append(merged.Stmts, p.Stmts...)
	}
	return merged
}

// collectZnFiles walks a directory and returns all .zn file paths (sorted).
// collectZnFiles returns every .zn under dir. *_test.zn files are included —
// they're transpiled to *_test.go which `go build` ignores and `go test`
// picks up. One pipeline, no mode flag.
func collectZnFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".zn") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// compileMultiFile parses all .zn files and generates one .go file per .zn file.
// A first pass collects exports from all files so each file's codegen knows about
// sibling types. Go handles cross-file visibility natively within a package.
func compileMultiFile(znFiles []string, outDir string, quiet bool, importAliases ...map[string]string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	if len(znFiles) == 0 {
		return nil
	}

	// If there's only one file, use the single-file path (simpler output naming)
	if len(znFiles) == 1 {
		files, err := compileFile(znFiles[0], outDir, importAliases...)
		if err != nil {
			return err
		}
		for _, f := range files {
			outPath := filepath.Join(outDir, f.Name)
			if wErr := os.WriteFile(outPath, []byte(f.Content), 0o644); wErr != nil {
				return fmt.Errorf("write %s: %w", outPath, wErr)
			}
			if !quiet {
				fmt.Printf("  %s → %s\n", znFiles[0], outPath)
			}
		}
		return nil
	}

	// Pass 1: Parse all files and collect exports
	progs := make([]*parser.Program, 0, len(znFiles))
	for _, path := range znFiles {
		prog, err := parseFile(path)
		if err != nil {
			return err
		}
		progs = append(progs, prog)
	}

	merged := mergePrograms(progs)
	allExports := codegen.CollectExports(merged)

	// Bind + typecheck pass. Bind() resolves every Ident to a Symbol via
	// the 5-level order in the spec; CheckV2 then runs with shared
	// signature context. Any error aborts before codegen. The BoundProgram
	// per file feeds each Generator so codegen consumes the side-map.
	var boundPrograms map[*parser.Program]*typechecker.BoundProgram
	{
		var im map[string]string
		if len(importAliases) > 0 && importAliases[0] != nil {
			im = importAliases[0]
		}
		bps, tcErrors := runTypecheck(progs, im, nil, outDir, nil, nil)
		if len(tcErrors) > 0 {
			for _, e := range tcErrors {
				fmt.Fprintf(os.Stderr, "typecheck: %s\n", e.String())
			}
			return fmt.Errorf("%d typecheck error(s)", len(tcErrors))
		}
		boundPrograms = bps
	}

	// Pass 2: Generate each file individually with sibling context
	var allCompileErrors []string
	for i, prog := range progs {
		baseName := strings.TrimSuffix(filepath.Base(znFiles[i]), ".zn")
		className := strings.ToUpper(baseName[:1]) + baseName[1:]

		gen := codegen.New()
		gen.SetSourceFile(prog.SourceFile)
		gen.SetGoModDir(outDir)
		gen.SetSiblingExports(allExports)
		if len(importAliases) > 0 && importAliases[0] != nil {
			gen.SetImportAliases(importAliases[0])
		}
		if bp, ok := boundPrograms[prog]; ok {
			gen.SetBoundProgram(bp)
		}
		files := gen.GenerateFiles(prog, className)
		allCompileErrors = append(allCompileErrors, gen.CompileErrors()...)
		for _, w := range gen.CompileWarnings() {
			fmt.Fprintln(os.Stderr, w)
		}

		for _, f := range files {
			outPath := filepath.Join(outDir, f.Name)
			if wErr := os.WriteFile(outPath, []byte(f.Content), 0o644); wErr != nil {
				return fmt.Errorf("write %s: %w", outPath, wErr)
			}
			if !quiet {
				fmt.Printf("  %s → %s\n", filepath.Base(znFiles[i]), outPath)
			}
		}
	}
	if len(allCompileErrors) > 0 {
		for _, e := range allCompileErrors {
			fmt.Fprintln(os.Stderr, e)
		}
		return fmt.Errorf("%d compile error(s)", len(allCompileErrors))
	}
	return nil
}

// compileDir compiles all .zn files in a directory using multi-file merging
// for cross-file type resolution. Writes generated .go files into outDir.
// If quiet is true, the progress lines are suppressed.
// importAliases are optional [deps] entries from zinc.toml for package resolution.
func compileDir(dir, outDir string, quiet bool, importAliases ...map[string]string) error {
	znFiles, err := collectZnFiles(dir)
	if err != nil {
		return err
	}
	return compileMultiFile(znFiles, outDir, quiet, importAliases...)
}

// collectZnFilesFlat collects .zn files in a directory (non-recursive, single level only).
// collectZnFilesFlat returns .zn files directly in dir (non-recursive).
// Includes *_test.zn; see collectZnFiles for rationale.
func collectZnFilesFlat(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".zn") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

// collectSubdirs returns relative paths of all subdirectories (recursive) in dir.
// e.g. for src/ containing core/, fabric/router/ → ["core", "fabric", "fabric/router"]
func collectSubdirs(dir string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != dir {
			rel, _ := filepath.Rel(dir, path)
			dirs = append(dirs, rel)
		}
		return nil
	})
	return dirs, err
}

// compileDirWithSubpackages compiles a project with subpackage support.
// Root .zn files → package main; each subdirectory → its own Go package.
func compileDirWithSubpackages(srcDir, outDir, moduleName string, quiet bool, importAliases ...map[string]string) error {
	return compileDirWithSubpackagesAndExtras(srcDir, outDir, moduleName, quiet, nil, importAliases...)
}

// externalClassDecls is per-compile-session state: class decls loaded
// from external deps (via [replace] zinc.toml entries). Keyed by
// subpackage alias (e.g. "errors"). Populated by buildProject /
// runProject / testProject before calling the compile function, read
// inside and passed through to each generator's SetSubpackageStructs.
var externalClassDecls map[string]map[string]*parser.ClassDecl

// compileDirWithSubpackagesAndExtras is compileDirWithSubpackages plus an
// optional `extraPkgs` map (subpackage name → absolute fs path) that the
// caller wants compiled alongside the discovered src/ subpackages. Used
// to bring in a sibling `tests/` directory during `zinc test` so test
// files live separately from production source but still resolve project
// imports (`import core`, etc.). Pass nil to behave like the wrapper.
func compileDirWithSubpackagesAndExtras(srcDir, outDir, moduleName string, quiet bool, extraPkgs map[string]string, importAliases ...map[string]string) error {
	goModDir := outDir // go.mod lives in outDir for module dep resolution
	var subpkgCompileErrors []string
	// 1. Discover subpackages (subdirectories of src/)
	subdirs, err := collectSubdirs(srcDir)
	if err != nil {
		return err
	}

	// pkgFsDir maps subpackage name → fs directory. For src/ subpackages
	// it's srcDir/<name>. extraPkgs entries override (or add) so callers
	// can mount a sibling directory like `tests/` as a virtual subpackage.
	pkgFsDir := make(map[string]string, len(subdirs)+len(extraPkgs))
	subpackages := make(map[string]bool)
	for _, d := range subdirs {
		subpackages[d] = true
		pkgFsDir[d] = filepath.Join(srcDir, d)
	}
	for name, dir := range extraPkgs {
		subpackages[name] = true
		pkgFsDir[name] = dir
	}

	// 2. Filter to only leaf packages (those with .zn files) and sort for dependency order
	// Parent dirs without .zn files are just namespace containers.
	var leafPkgs []string
	for pkg := range subpackages {
		znFiles, _ := collectZnFilesFlat(pkgFsDir[pkg])
		if len(znFiles) > 0 {
			leafPkgs = append(leafPkgs, pkg)
		}
	}
	sort.Strings(leafPkgs)

	// 3. Parse all subpackages and collect exports (two-pass: parse first, generate second)
	allExports := make(map[string]map[string]string)            // pkg → name → kind
	allDataFields := make(map[string]map[string][]*parser.FieldDecl)  // pkg → data class name → fields
	allClassDecls := make(map[string]map[string]*parser.ClassDecl)   // pkg → class name → full decl
	allTypeAliases := make(map[string]map[string]parser.TypeExpr)    // pkg → alias name → underlying TypeExpr
	allMerged := make(map[string]*parser.Program)               // pkg → merged AST
	allZnFiles := make(map[string][]string)                     // pkg → source file paths
	allProgs := make(map[string][]*parser.Program)              // pkg → per-file ASTs (one per file in znFiles)

	for _, pkg := range leafPkgs {
		pkgDir := pkgFsDir[pkg]
		znFiles, err := collectZnFilesFlat(pkgDir)
		if err != nil {
			return fmt.Errorf("collect files in %s: %w", pkgDir, err)
		}
		if len(znFiles) == 0 {
			continue
		}
		progs := make([]*parser.Program, 0, len(znFiles))
		for _, path := range znFiles {
			prog, err := parseFile(path)
			if err != nil {
				return err
			}
			progs = append(progs, prog)
		}
		merged := mergePrograms(progs)
		allMerged[pkg] = merged
		allZnFiles[pkg] = znFiles
		allProgs[pkg] = progs
		allExports[pkg] = codegen.CollectExports(merged)
		allDataFields[pkg] = codegen.CollectDataClassFields(merged)
		allClassDecls[pkg] = codegen.CollectClassDecls(merged)
		allTypeAliases[pkg] = codegen.CollectTypeAliases(merged)
	}

	// Phase 3.2 + 3.3 — bind + typecheck for the multi-package case. We
	// share the BoundProgram across the per-file emit loop so the codegen's
	// side-map lookups (keyed by *parser.Ident pointer identity) hit. The
	// previous approach re-parsed each file at emit time, producing different
	// AST nodes that the side-map would never match.
	allBound := make(map[*parser.Program]*typechecker.BoundProgram)
	{
		var im map[string]string
		if len(importAliases) > 0 && importAliases[0] != nil {
			im = importAliases[0]
		}
		var allTcErrors []typechecker.V2Error
		for _, pkg := range leafPkgs {
			pkgProgs := allProgs[pkg]
			if len(pkgProgs) == 0 {
				continue
			}
			// 3.7.2: build a per-leaf importMap that includes the project's
			// dep aliases AND the leaf's `import` directives (paths stored
			// with dots — convert to slashes for Go-import shape). The
			// typechecker's FFI resolver consults this for `pkg.Func()`
			// inference.
			leafIm := make(map[string]string, len(im))
			for k, v := range im {
				leafIm[k] = v
			}
			for _, prog := range pkgProgs {
				for _, imp := range prog.Imports {
					goPath := strings.ReplaceAll(imp.Path, ".", "/")
					alias := imp.Alias
					if alias == "" {
						parts := strings.Split(goPath, "/")
						alias = parts[len(parts)-1]
					}
					if _, exists := leafIm[alias]; !exists {
						leafIm[alias] = goPath
					}
				}
			}
			// Build cross-package exports for this package's bind context:
			// every other package's exports, keyed by the alias the
			// importer would use (the package's last path segment).
			crossPkg := make(map[string]map[string]string)
			crossDecls := make(map[string]map[string]*parser.ClassDecl)
			crossFns := make(map[string]*parser.FnDecl)
			for otherPkg, otherExports := range allExports {
				if otherPkg == pkg {
					continue
				}
				alias := filepath.Base(otherPkg)
				crossPkg[alias] = otherExports
				// Walk the merged AST directly so sealed classes are
				// included (CollectClassDecls filters them out). Also
				// gather top-level FnDecls so qualified calls like
				// `expressions.compile(...)` resolve to a real signature.
				if merged, ok := allMerged[otherPkg]; ok {
					cls := make(map[string]*parser.ClassDecl)
					for _, d := range merged.Decls {
						if cd, ok := d.(*parser.ClassDecl); ok {
							cls[cd.Name] = cd
						}
						if fd, ok := d.(*parser.FnDecl); ok {
							crossFns[fd.Name] = fd
						}
					}
					if len(cls) > 0 {
						crossDecls[alias] = cls
					}
				}
			}
			bps, errs := runTypecheck(pkgProgs, leafIm, crossPkg, goModDir, crossDecls, crossFns)
			for prog, bp := range bps {
				allBound[prog] = bp
			}
			allTcErrors = append(allTcErrors, errs...)
		}
		if len(allTcErrors) > 0 {
			for _, e := range allTcErrors {
				fmt.Fprintf(os.Stderr, "typecheck: %s\n", e.String())
			}
			return fmt.Errorf("%d typecheck error(s)", len(allTcErrors))
		}
	}

	// 4. Generate Go code for each subpackage — one .go file per .zn file
	for _, pkg := range leafPkgs {
		merged, ok := allMerged[pkg]
		if !ok {
			continue
		}
		pkgOutDir := filepath.Join(outDir, pkg)
		if err := os.MkdirAll(pkgOutDir, 0o755); err != nil {
			return err
		}

		goPkgName := filepath.Base(pkg)
		znFiles := allZnFiles[pkg]
		siblingExports := allExports[pkg] // exports from all files in this package
		pkgProgs := allProgs[pkg]         // per-file ASTs (parsed in step 3)

		// Use the per-file ASTs from step 3 instead of re-parsing — the
		// bind side-map (when typecheck is on) is keyed by AST node pointer
		// identity, so re-parsing would produce nodes the side-map can't
		// recognize.
		for i, znPath := range znFiles {
			prog := pkgProgs[i]

			baseName := strings.TrimSuffix(filepath.Base(znPath), ".zn")
			className := strings.ToUpper(baseName[:1]) + baseName[1:]

			gen := codegen.New()
			gen.SetSourceFile(prog.SourceFile)
			gen.SetPackageName(goPkgName)
			gen.SetModuleName(moduleName)
			gen.SetGoModDir(goModDir)
			gen.SetZincSubpackages(subpackages)
			gen.SetSiblingExports(siblingExports)
			if len(importAliases) > 0 && importAliases[0] != nil {
				gen.SetImportAliases(importAliases[0])
			}
			if bp, ok := allBound[prog]; ok {
				gen.SetBoundProgram(bp)
			}
			for otherPkg, otherExports := range allExports {
				if otherPkg != pkg {
					otherAlias := filepath.Base(otherPkg)
					gen.SetSubpackageExports(otherAlias, otherExports)
					if fields, ok := allDataFields[otherPkg]; ok {
						gen.SetSubpackageDataFields(otherAlias, fields)
					}
					if classes, ok := allClassDecls[otherPkg]; ok {
						gen.SetSubpackageStructs(otherAlias, classes)
					}
					if aliases, ok := allTypeAliases[otherPkg]; ok {
						gen.SetSubpackageTypeAliases(otherAlias, aliases)
					}
				}
			}
			// External deps (stdlib etc.) — register their class decls
			// so cross-package error-type checks work.
			for alias, classes := range externalClassDecls {
				gen.SetSubpackageStructs(alias, classes)
			}

			// Inject package declaration from merged (individual files may not have it)
			if prog.Package == nil && merged.Package != nil {
				prog.Package = merged.Package
			}

			files := gen.GenerateFiles(prog, className)
			subpkgCompileErrors = append(subpkgCompileErrors, gen.CompileErrors()...)
			for _, w := range gen.CompileWarnings() {
				fmt.Fprintln(os.Stderr, w)
			}
			for _, f := range files {
				outPath := filepath.Join(pkgOutDir, f.Name)
				if wErr := os.WriteFile(outPath, []byte(f.Content), 0o644); wErr != nil {
					return fmt.Errorf("write %s: %w", outPath, wErr)
				}
				if !quiet {
					fmt.Printf("  [%s] %s → %s\n", pkg, filepath.Base(znPath), f.Name)
				}
			}
		}
	}

	// 5. Compile root files (package main) — one .go file per .zn file
	rootFiles, err := collectZnFilesFlat(srcDir)
	if err != nil {
		return err
	}
	if len(rootFiles) == 0 {
		return nil
	}

	// Collect root-level exports for sibling awareness
	rootProgs := make([]*parser.Program, 0, len(rootFiles))
	for _, path := range rootFiles {
		prog, err := parseFile(path)
		if err != nil {
			return err
		}
		rootProgs = append(rootProgs, prog)
	}
	rootMerged := mergePrograms(rootProgs)
	rootExports := codegen.CollectExports(rootMerged)

	// Bind + typecheck root files. Cross-package exports surface every
	// leaf subpackage so root code can reference `core.Foo` etc. through
	// the side-map, matching what the leaf-package emit loop does.
	rootBound := make(map[*parser.Program]*typechecker.BoundProgram)
	{
		var im map[string]string
		if len(importAliases) > 0 && importAliases[0] != nil {
			im = importAliases[0]
		}
		// 3.7.2: extend importMap with root files' `import` directives.
		rootIm := make(map[string]string, len(im))
		for k, v := range im {
			rootIm[k] = v
		}
		for _, prog := range rootProgs {
			for _, imp := range prog.Imports {
				goPath := strings.ReplaceAll(imp.Path, ".", "/")
				alias := imp.Alias
				if alias == "" {
					parts := strings.Split(goPath, "/")
					alias = parts[len(parts)-1]
				}
				if _, exists := rootIm[alias]; !exists {
					rootIm[alias] = goPath
				}
			}
		}
		crossPkg := make(map[string]map[string]string)
		crossDecls := make(map[string]map[string]*parser.ClassDecl)
		crossFns := make(map[string]*parser.FnDecl)
		for otherPkg, otherExports := range allExports {
			alias := filepath.Base(otherPkg)
			crossPkg[alias] = otherExports
			if merged, ok := allMerged[otherPkg]; ok {
				cls := make(map[string]*parser.ClassDecl)
				for _, d := range merged.Decls {
					if cd, ok := d.(*parser.ClassDecl); ok {
						cls[cd.Name] = cd
					}
					if fd, ok := d.(*parser.FnDecl); ok {
						crossFns[fd.Name] = fd
					}
				}
				if len(cls) > 0 {
					crossDecls[alias] = cls
				}
			}
		}
		bps, errs := runTypecheck(rootProgs, rootIm, crossPkg, goModDir, crossDecls, crossFns)
		for prog, bp := range bps {
			rootBound[prog] = bp
		}
		if len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "typecheck: %s\n", e.String())
			}
			return fmt.Errorf("%d typecheck error(s)", len(errs))
		}
	}

	// Generate each root file individually
	for i, prog := range rootProgs {
		baseName := strings.TrimSuffix(filepath.Base(rootFiles[i]), ".zn")
		className := strings.ToUpper(baseName[:1]) + baseName[1:]

		gen := codegen.New()
		gen.SetSourceFile(prog.SourceFile)
		gen.SetModuleName(moduleName)
		gen.SetGoModDir(goModDir)
		gen.SetZincSubpackages(subpackages)
		gen.SetSiblingExports(rootExports)
		if len(importAliases) > 0 && importAliases[0] != nil {
			gen.SetImportAliases(importAliases[0])
		}
		if bp, ok := rootBound[prog]; ok {
			gen.SetBoundProgram(bp)
		}
		for pkg, exports := range allExports {
			alias := filepath.Base(pkg)
			gen.SetSubpackageExports(alias, exports)
			if fields, ok := allDataFields[pkg]; ok {
				gen.SetSubpackageDataFields(alias, fields)
			}
			if classes, ok := allClassDecls[pkg]; ok {
				gen.SetSubpackageStructs(alias, classes)
			}
			if aliases, ok := allTypeAliases[pkg]; ok {
				gen.SetSubpackageTypeAliases(alias, aliases)
			}
		}
		for alias, classes := range externalClassDecls {
			gen.SetSubpackageStructs(alias, classes)
		}

		files := gen.GenerateFiles(prog, className)
		subpkgCompileErrors = append(subpkgCompileErrors, gen.CompileErrors()...)
		for _, w := range gen.CompileWarnings() {
			fmt.Fprintln(os.Stderr, w)
		}
		for _, f := range files {
			outPath := filepath.Join(outDir, f.Name)
			if wErr := os.WriteFile(outPath, []byte(f.Content), 0o644); wErr != nil {
				return fmt.Errorf("write %s: %w", outPath, wErr)
			}
			if !quiet {
				fmt.Printf("  [main] %s → %s\n", filepath.Base(rootFiles[i]), f.Name)
			}
		}
	}

	if len(subpkgCompileErrors) > 0 {
		for _, e := range subpkgCompileErrors {
			fmt.Fprintln(os.Stderr, e)
		}
		return fmt.Errorf("%d compile error(s)", len(subpkgCompileErrors))
	}
	return nil
}
