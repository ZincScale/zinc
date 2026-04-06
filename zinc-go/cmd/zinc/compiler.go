package main

// Compilation pipeline: parsing, multi-file merging, subpackage compilation.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	codegen "zinc-go/internal/codegen_go"
	"zinc-go/internal/lexer"
	"zinc-go/internal/parser"
)

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
func compileFile(path string) ([]codegen.OutputFile, error) {
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
	files := gen.GenerateFiles(prog, className)
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
func compileMultiFile(znFiles []string, outDir string, quiet bool) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	if len(znFiles) == 0 {
		return nil
	}

	// If there's only one file, use the single-file path (simpler output naming)
	if len(znFiles) == 1 {
		files, err := compileFile(znFiles[0])
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

	// Collect exports from all files for sibling awareness
	merged := mergePrograms(progs)
	allExports := codegen.CollectExports(merged)

	// Pass 2: Generate each file individually with sibling context
	for i, prog := range progs {
		baseName := strings.TrimSuffix(filepath.Base(znFiles[i]), ".zn")
		className := strings.ToUpper(baseName[:1]) + baseName[1:]

		gen := codegen.New()
		gen.SetSourceFile(prog.SourceFile)
		gen.SetSiblingExports(allExports)
		files := gen.GenerateFiles(prog, className)

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
	return nil
}

// compileDir compiles all .zn files in a directory using multi-file merging
// for cross-file type resolution. Writes generated .go files into outDir.
// If quiet is true, the progress lines are suppressed.
func compileDir(dir, outDir string, quiet bool) error {
	znFiles, err := collectZnFiles(dir)
	if err != nil {
		return err
	}
	return compileMultiFile(znFiles, outDir, quiet)
}

// collectZnFilesFlat collects .zn files in a directory (non-recursive, single level only).
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
	goModDir := outDir // go.mod lives in outDir for module dep resolution
	// 1. Discover subpackages (subdirectories of src/)
	subdirs, err := collectSubdirs(srcDir)
	if err != nil {
		return err
	}

	subpackages := make(map[string]bool)
	for _, d := range subdirs {
		subpackages[d] = true
	}

	// 2. Filter to only leaf packages (those with .zn files) and sort for dependency order
	// Parent dirs without .zn files are just namespace containers.
	var leafPkgs []string
	for _, pkg := range subdirs {
		pkgDir := filepath.Join(srcDir, pkg)
		znFiles, _ := collectZnFilesFlat(pkgDir)
		if len(znFiles) > 0 {
			leafPkgs = append(leafPkgs, pkg)
		}
	}

	// 3. Parse all subpackages and collect exports (two-pass: parse first, generate second)
	allExports := make(map[string]map[string]string)            // pkg → name → kind
	allDataFields := make(map[string]map[string][]*parser.FieldDecl)  // pkg → data class name → fields
	allClassDecls := make(map[string]map[string]*parser.ClassDecl)   // pkg → class name → full decl
	allMerged := make(map[string]*parser.Program)               // pkg → merged AST
	allZnFiles := make(map[string][]string)                     // pkg → source file paths

	for _, pkg := range leafPkgs {
		pkgDir := filepath.Join(srcDir, pkg)
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
		allExports[pkg] = codegen.CollectExports(merged)
		allDataFields[pkg] = codegen.CollectDataClassFields(merged)
		allClassDecls[pkg] = codegen.CollectClassDecls(merged)
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

		// Parse each file individually and generate separately
		for _, znPath := range znFiles {
			prog, err := parseFile(znPath)
			if err != nil {
				return err
			}

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
				}
			}

			// Inject package declaration from merged (individual files may not have it)
			if prog.Package == nil && merged.Package != nil {
				prog.Package = merged.Package
			}

			files := gen.GenerateFiles(prog, className)
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
		for pkg, exports := range allExports {
			alias := filepath.Base(pkg)
			gen.SetSubpackageExports(alias, exports)
			if fields, ok := allDataFields[pkg]; ok {
				gen.SetSubpackageDataFields(alias, fields)
			}
			if classes, ok := allClassDecls[pkg]; ok {
				gen.SetSubpackageStructs(alias, classes)
			}
		}

		files := gen.GenerateFiles(prog, className)
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

	return nil
}
