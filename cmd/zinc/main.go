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

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"zinc/internal/codegen_java"
	"zinc/internal/errs"
	"zinc/internal/lexer"
	"zinc/internal/parser"
	"zinc/internal/typechecker"
)

var version = "3.0.0"

const usage = `Zinc — convention-over-configuration JVM language.

Usage:
  zinc init [name]               Scaffold a new project
  zinc build <file.zn|dir>       Transpile + compile (Mill if project, javac if script)
  zinc build --native <dir>      Build native binary (GraalVM native-image via Mill)
  zinc build --docker <dir>      Build Docker image (Mill) or generate Dockerfile
  zinc build --k8s <dir>         Docker image + K8s manifest
  zinc run <file.zn|dir>         Transpile, compile, and run
  zinc add <dep>                 Add a Maven dependency to build.mill.yaml
  zinc remove <artifact>         Remove a dependency
  zinc deps                      List project dependencies
  zinc fmt <file.zn>             Format Zinc source code
  zinc repl                      Interactive Zinc REPL
  zinc update                    Update Zinc toolchain (GraalVM, Mill, Quarkus)

Flags:
  -o <dir>               Output directory for non-Mill builds (default: zinc-out/)
  --verbose              Print tokens and AST summary
  --version              Print version and exit
`

func main() {
	args := os.Args[1:]

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--version" || a == "-V":
			fmt.Printf("zinc version %s\n", version)
			return
		case a == "repl":
			runREPLV2()
			return
		case a == "init":
			name := "myapp"
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				name = args[i+1]
			}
			if err := runInit(name); err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			return
		case a == "fmt":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "usage: zinc fmt <file.zn>")
				os.Exit(1)
			}
			runFmt(args[i+1])
			return
		case a == "build":
			target := ""
			outDir := "zinc-out"
			verbose := false
			native := false
			docker := false
			k8s := false
			for j := i + 1; j < len(args); j++ {
				if args[j] == "-o" && j+1 < len(args) {
					outDir = args[j+1]
					j++
				} else if args[j] == "--verbose" || args[j] == "-v" {
					verbose = true
				} else if args[j] == "--native" {
					native = true
				} else if args[j] == "--docker" {
					docker = true
				} else if args[j] == "--k8s" {
					k8s = true
				} else if !strings.HasPrefix(args[j], "-") && target == "" {
					target = args[j]
				}
			}
			if target == "" {
				fmt.Fprintln(os.Stderr, "usage: zinc build <file.zn|dir> [-o outdir] [--native|--docker|--k8s]")
				os.Exit(1)
			}

			appName := filepath.Base(strings.TrimSuffix(target, ".zn"))

			// Check for Mill project
			if projectDir, hasMill := hasMillConfig(target); hasMill {
				// For Mill projects, derive app name from project directory, not target
				absProject, _ := filepath.Abs(projectDir)
				appName = filepath.Base(absProject)
				// Mill project: transpile .zn → .java into Mill's source dir, delegate to Mill
				srcDir := filepath.Join(projectDir, "src")
				cleanGeneratedJava(srcDir)
				cleanMillCache(projectDir)
				if _, err := transpileToJava(target, srcDir, verbose); err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}

				if native {
					fmt.Println("building native binary via Mill...")
					if err := runMill(projectDir, "nativeImage"); err != nil {
						exitOnMillErr(err)
					}
				} else if docker {
					fmt.Println("building Docker image via Mill...")
					if err := runMill(projectDir, "nativeImage"); err != nil {
						fmt.Println("native-image not available, building fat JAR...")
						if err := runMill(projectDir, "assembly"); err != nil {
							exitOnMillErr(err)
						}
					}
					if err := generateDockerfile(projectDir, appName); err != nil {
						errs.Error(err.Error())
						os.Exit(1)
					}
				} else if k8s {
					fmt.Println("building Docker image + K8s manifest via Mill...")
					if err := runMill(projectDir, "nativeImage"); err != nil {
						fmt.Println("native-image not available, building fat JAR...")
						if err := runMill(projectDir, "assembly"); err != nil {
							exitOnMillErr(err)
						}
					}
					if err := generateDockerfile(projectDir, appName); err != nil {
						errs.Error(err.Error())
						os.Exit(1)
					}
					if err := generateK8sManifest(projectDir, appName); err != nil {
						errs.Error(err.Error())
						os.Exit(1)
					}
				} else {
					if err := runMill(projectDir, "compile"); err != nil {
						exitOnMillErr(err)
					}
				}
				fmt.Printf("build complete: %s (Mill project)\n", projectDir)
			} else {
				// No Mill: single-file script — direct javac
				javaFiles, err := transpileToJava(target, outDir, verbose)
				if err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}
				if err := compileJava(javaFiles, outDir); err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}

				if native {
					if err := buildNativeDirect(outDir, appName); err != nil {
						errs.Error(err.Error())
						os.Exit(1)
					}
				} else if docker || k8s {
					if err := generateDockerfile(".", appName); err != nil {
						errs.Error(err.Error())
						os.Exit(1)
					}
					if k8s {
						if err := generateK8sManifest(".", appName); err != nil {
							errs.Error(err.Error())
							os.Exit(1)
						}
					}
				}
				fmt.Printf("build complete: %s → %s/\n", target, outDir)
			}
			return
		case a == "run":
			target := ""
			verbose := false
			for j := i + 1; j < len(args); j++ {
				if args[j] == "--verbose" || args[j] == "-v" {
					verbose = true
				} else if args[j] == "--" {
					break
				} else if !strings.HasPrefix(args[j], "-") && target == "" {
					target = args[j]
				}
			}
			if target == "" {
				fmt.Fprintln(os.Stderr, "usage: zinc run <file.zn|dir> [-- args...]")
				os.Exit(1)
			}

			// Collect script args (after --)
			var scriptArgs []string
			for j := i + 1; j < len(args); j++ {
				if args[j] == "--" {
					scriptArgs = args[j+1:]
					break
				}
			}

			// Check for Mill project
			if projectDir, hasMill := hasMillConfig(target); hasMill {
				// Mill project: transpile .zn → .java into Mill's source dir, delegate to Mill
				srcDir := filepath.Join(projectDir, "src")
				cleanGeneratedJava(srcDir)
				buildTarget := target
				if !isDir(target) {
					buildTarget = filepath.Dir(target)
				}
				if _, err := transpileToJava(buildTarget, srcDir, verbose); err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}

				millArgs := []string{"run"}
				if len(scriptArgs) > 0 {
					millArgs = append(millArgs, "--")
					millArgs = append(millArgs, scriptArgs...)
				}
				if err := runMill(projectDir, millArgs...); err != nil {
					if exitErr, ok := err.(*exec.ExitError); ok {
						os.Exit(exitErr.ExitCode())
					}
					errs.Error(err.Error())
					os.Exit(1)
				}
			} else {
				// No Mill: single-file script — direct javac + java
				buildTarget := target
				if !isDir(target) && strings.HasSuffix(target, ".zn") {
					if hasPackageDecl(target) {
						buildTarget = filepath.Dir(target)
					}
				}

				outDir := filepath.Join(os.TempDir(), "zinc-run-"+filepath.Base(strings.TrimSuffix(target, ".zn")))
				javaFiles, err := transpileToJava(buildTarget, outDir, verbose)
				if err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}
				if err := compileJava(javaFiles, outDir); err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}

				className := classNameFromFile(target)
				if src, err := os.ReadFile(target); err == nil {
					for _, line := range strings.Split(string(src), "\n") {
						line = strings.TrimSpace(line)
						if strings.HasPrefix(line, "package ") {
							pkg := strings.TrimSuffix(strings.TrimPrefix(line, "package "), ";")
							pkg = strings.TrimSpace(pkg)
							className = pkg + "." + className
							break
						}
						if line != "" && !strings.HasPrefix(line, "//") {
							break
						}
					}
				}

				if err := runJava(outDir, className, scriptArgs); err != nil {
					if exitErr, ok := err.(*exec.ExitError); ok {
						os.Exit(exitErr.ExitCode())
					}
					errs.Error(err.Error())
					os.Exit(1)
				}
			}
			return
		case a == "add":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "usage: zinc add <group:artifact:version>")
				fmt.Fprintln(os.Stderr, "  e.g. zinc add com.google.code.gson:gson:2.11.0")
				os.Exit(1)
			}
			if err := runAddDep(args[i+1]); err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			return
		case a == "remove":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "usage: zinc remove <artifact>")
				fmt.Fprintln(os.Stderr, "  e.g. zinc remove gson")
				os.Exit(1)
			}
			if err := runRemoveDep(args[i+1]); err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			return
		case a == "deps":
			if err := runListDeps(); err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			return
		case a == "update":
			if err := runUpdate(); err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			return
		case !strings.HasPrefix(a, "-"):
			// Default: zinc build <file.zn>
			javaFiles, err := transpileToJava(a, "zinc-out", false)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			if err := compileJava(javaFiles, "zinc-out"); err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			fmt.Printf("build complete: %s → zinc-out/\n", a)
			return
		}
	}

	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
}

// parseAndCheck runs lexer → parser → typechecker, returns the AST.
func parseOnly(inFile string, verbose bool) (*parser.Program, error) {
	src, err := os.ReadFile(inFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", inFile, err)
	}

	l := lexer.New(string(src))
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		return nil, fmt.Errorf("lexer errors in %s:\n%s", inFile, strings.Join(l.Errors, "\n"))
	}

	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		return nil, fmt.Errorf("parse errors in %s:\n%s", inFile, strings.Join(p.Errors, "\n"))
	}

	return prog, nil
}

func parseAndCheck(inFile string, verbose bool) (*parser.Program, error) {
	src, err := os.ReadFile(inFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", inFile, err)
	}

	l := lexer.New(string(src))
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		return nil, fmt.Errorf("lexer errors in %s:\n%s", inFile, strings.Join(l.Errors, "\n"))
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] %d tokens\n", len(tokens))
	}

	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		return nil, fmt.Errorf("parse errors in %s:\n%s", inFile, strings.Join(p.Errors, "\n"))
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] %d declarations, %d top-level statements\n",
			len(prog.Decls), len(prog.Stmts))
	}

	if tcErrors := typechecker.CheckV2(prog); len(tcErrors) > 0 {
		var msgs []string
		for _, e := range tcErrors {
			msgs = append(msgs, e.String())
		}
		return nil, fmt.Errorf("type errors in %s:\n%s", inFile, strings.Join(msgs, "\n"))
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] type check passed\n")
	}

	return prog, nil
}

// findStdlibDir locates the stdlib directory relative to the zinc binary.
// Checks: <binary-dir>/stdlib, <binary-dir>/../stdlib, and the ZINC_STDLIB env var.
func findStdlibDir() string {
	// Check ZINC_STDLIB env var first
	if dir := os.Getenv("ZINC_STDLIB"); dir != "" {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}

	// Find the binary's directory
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		binDir := filepath.Dir(exe)

		// Check <binary-dir>/stdlib
		candidate := filepath.Join(binDir, "stdlib")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		// Check <binary-dir>/../stdlib (for go run / development)
		candidate = filepath.Join(binDir, "..", "stdlib")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Fallback: check current working directory's parent (development mode)
	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, "stdlib")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

// transpileToJava transpiles .zn file(s) to .java files in outDir.
// Accepts a single file or a directory (scans for all .zn files).
// Each data class, enum, and class gets its own .java file.
// For multi-file projects, auto-resolves cross-file type imports.
func transpileToJava(target, outDir string, verbose bool) ([]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("cannot access %s: %w", target, err)
	}

	var znFiles []string
	if info.IsDir() {
		// Recursively scan directory for .zn files
		filepath.Walk(target, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			if strings.HasSuffix(fi.Name(), ".zn") {
				znFiles = append(znFiles, path)
			}
			return nil
		})
		if len(znFiles) == 0 {
			return nil, fmt.Errorf("no .zn files found in %s", target)
		}
	} else {
		znFiles = []string{target}
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	// Check if single file needs stdlib imports — if so, promote to multi-file
	if len(znFiles) == 1 {
		stdlibDir := findStdlibDir()
		if stdlibDir != "" {
			prog, err := parseOnly(znFiles[0], false)
			if err == nil {
				for _, imp := range prog.Imports {
					if strings.HasPrefix(imp.Path, "zinc.") {
						parts := strings.SplitN(imp.Path, ".", 2)
						if len(parts) == 2 {
							znFile := filepath.Join(stdlibDir, "zinc", parts[1]+".zn")
							if _, err := os.Stat(znFile); err == nil {
								znFiles = append(znFiles, znFile)
							}
						}
					}
				}
			}
		}
	}

	// For multi-file projects: parse all files first, build a type registry,
	// then inject cross-file imports before codegen.
	if len(znFiles) > 1 {
		return transpileMultiFile(znFiles, outDir, verbose)
	}

	// Single file — no cross-file resolution needed
	javaFiles, err := transpileSingleFile(znFiles[0], outDir, verbose)
	if err != nil {
		return nil, err
	}
	return javaFiles, nil
}

// inferPackageFromDir derives a Java package path from a file's directory
// relative to a source root. e.g., src/models/user.zn → "models"
// Files directly in src/ get no package (root package).
func inferPackageFromDir(filePath, sourceRoot string) string {
	absFile, _ := filepath.Abs(filePath)
	absRoot, _ := filepath.Abs(sourceRoot)
	dir := filepath.Dir(absFile)

	rel, err := filepath.Rel(absRoot, dir)
	if err != nil || rel == "." {
		return "" // root package
	}
	// Convert path separators to dots: models/orders → models.orders
	return strings.ReplaceAll(rel, string(filepath.Separator), ".")
}

// findSourceRoot walks up from znFiles to find the common source root (where src/ is).
func findSourceRoot(znFiles []string) string {
	if len(znFiles) == 0 {
		return "."
	}
	// Find the common directory prefix, then check if it's a "src" dir
	first, _ := filepath.Abs(znFiles[0])
	dir := filepath.Dir(first)

	// Walk up looking for a directory named "src" in the path
	for d := dir; d != filepath.Dir(d); d = filepath.Dir(d) {
		if filepath.Base(d) == "src" {
			return d
		}
	}
	// Fallback: use the directory of the first file's parent
	return dir
}

// transpileMultiFile handles multi-file projects with cross-file type resolution.
// Convention: directory structure = package. src/models/user.zn → package models.
func transpileMultiFile(znFiles []string, outDir string, verbose bool) ([]string, error) {
	sourceRoot := findSourceRoot(znFiles)

	// Pass 1: Parse all files, infer/enforce package from directory, collect types
	type parsedFile struct {
		path string
		prog *parser.Program
	}
	var parsed []parsedFile
	// typeRegistry maps type name → package path (e.g., "User" → "models")
	typeRegistry := make(map[string]string)
	// pkgTypes maps package → list of type names (for wildcard resolution)
	pkgTypes := make(map[string][]string)

	// Pass 1a: Parse all files (no typechecking yet)
	for _, znFile := range znFiles {
		prog, err := parseOnly(znFile, verbose)
		if err != nil {
			return nil, err
		}

		// Convention: directory = package
		inferredPkg := inferPackageFromDir(znFile, sourceRoot)

		// Check if file is under source root (not stdlib)
		absFile, _ := filepath.Abs(znFile)
		absRoot, _ := filepath.Abs(sourceRoot)
		isUnderSourceRoot := strings.HasPrefix(absFile, absRoot)

		if prog.Package != nil {
			// Validate declared package matches directory — only for project files
			if isUnderSourceRoot && inferredPkg != "" && prog.Package.Path != inferredPkg {
				fmt.Fprintf(os.Stderr, "warning: %s declares package '%s' but directory suggests '%s'\n",
					znFile, prog.Package.Path, inferredPkg)
			}
		} else if inferredPkg != "" && isUnderSourceRoot {
			// Auto-set package from directory
			prog.Package = &parser.PackageDecl{Path: inferredPkg}
			if verbose {
				fmt.Fprintf(os.Stderr, "[verbose] %s → auto-package '%s'\n", znFile, inferredPkg)
			}
		}

		parsed = append(parsed, parsedFile{path: znFile, prog: prog})

		pkg := ""
		if prog.Package != nil {
			pkg = prog.Package.Path
		}
		for _, d := range prog.Decls {
			switch decl := d.(type) {
			case *parser.DataClassDecl:
				typeRegistry[decl.Name] = pkg
				pkgTypes[pkg] = append(pkgTypes[pkg], decl.Name)
			case *parser.ClassDecl:
				typeRegistry[decl.Name] = pkg
				pkgTypes[pkg] = append(pkgTypes[pkg], decl.Name)
				if decl.IsSealed {
					for _, v := range decl.Variants {
						typeRegistry[v.Name] = pkg
						pkgTypes[pkg] = append(pkgTypes[pkg], v.Name)
					}
				}
			case *parser.EnumDecl:
				typeRegistry[decl.Name] = pkg
				pkgTypes[pkg] = append(pkgTypes[pkg], decl.Name)
			case *parser.InterfaceDecl:
				typeRegistry[decl.Name] = pkg
				pkgTypes[pkg] = append(pkgTypes[pkg], decl.Name)
			}
		}
	}

	// Resolve stdlib imports: if any file imports zinc.*, include ALL stdlib .zn files
	stdlibDir := findStdlibDir()
	if stdlibDir != "" {
		hasZincImport := false
		for _, pf := range parsed {
			for _, imp := range pf.prog.Imports {
				if strings.HasPrefix(imp.Path, "zinc.") {
					hasZincImport = true
					break
				}
			}
			if hasZincImport {
				break
			}
		}
		var stdlibNeeded []string
		if hasZincImport {
			// Include all stdlib files when any zinc.* import is present
			stdlibZincDir := filepath.Join(stdlibDir, "zinc")
			filepath.Walk(stdlibZincDir, func(path string, fi os.FileInfo, err error) error {
				if err != nil || fi.IsDir() {
					return nil
				}
				if strings.HasSuffix(fi.Name(), ".zn") {
					stdlibNeeded = append(stdlibNeeded, path)
				}
				return nil
			})
		}
		// Parse and include stdlib files
		seen := make(map[string]bool)
		for _, znFile := range stdlibNeeded {
			if seen[znFile] {
				continue
			}
			seen[znFile] = true
			prog, err := parseOnly(znFile, verbose)
			if err != nil {
				return nil, fmt.Errorf("parsing stdlib %s: %w", znFile, err)
			}
			// Stdlib files declare their own package
			parsed = append(parsed, parsedFile{path: znFile, prog: prog})
			pkg := ""
			if prog.Package != nil {
				pkg = prog.Package.Path
			}
			for _, d := range prog.Decls {
				switch decl := d.(type) {
				case *parser.ClassDecl:
					typeRegistry[decl.Name] = pkg
					pkgTypes[pkg] = append(pkgTypes[pkg], decl.Name)
				case *parser.InterfaceDecl:
					typeRegistry[decl.Name] = pkg
					pkgTypes[pkg] = append(pkgTypes[pkg], decl.Name)
				}
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "[verbose] included stdlib: %s\n", znFile)
			}
		}
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] type registry: %d types across %d files\n",
			len(typeRegistry), len(parsed))
	}

	// Pass 1b: Collect all function/method signatures across files
	allSigs := &typechecker.CollectedSigs{
		FnSigs:      make(map[string]typechecker.V2FnSig),
		MethodSigs:  make(map[string]map[string]typechecker.V2FnSig),
		ParentTypes: make(map[string][]string),
	}
	for _, pf := range parsed {
		fileSigs := typechecker.CollectSignatures(pf.prog)
		for k, v := range fileSigs.FnSigs {
			allSigs.FnSigs[k] = v
		}
		for k, v := range fileSigs.MethodSigs {
			allSigs.MethodSigs[k] = v
		}
		for k, v := range fileSigs.ParentTypes {
			allSigs.ParentTypes[k] = v
		}
	}

	// Pass 1c: Typecheck each file with cross-file context
	for _, pf := range parsed {
		if tcErrors := typechecker.CheckV2WithContext(pf.prog, allSigs); len(tcErrors) > 0 {
			var msgs []string
			for _, e := range tcErrors {
				msgs = append(msgs, e.String())
			}
			return nil, fmt.Errorf("type errors in %s:\n%s", pf.path, strings.Join(msgs, "\n"))
		}
	}

	// Pass 2: Resolve imports and generate Java
	var allJavaFiles []string
	for _, pf := range parsed {
		myPkg := ""
		if pf.prog.Package != nil {
			myPkg = pf.prog.Package.Path
		}

		// Resolve internal wildcard imports (import models.* → specific types)
		var resolvedImports []*parser.ImportDecl
		existingImports := make(map[string]bool)
		for _, imp := range pf.prog.Imports {
			if strings.HasSuffix(imp.Path, ".*") {
				// Check if this is an internal package
				wildcardPkg := strings.TrimSuffix(imp.Path, ".*")
				if types, ok := pkgTypes[wildcardPkg]; ok {
					// Internal package — resolve to specific type imports
					for _, typeName := range types {
						specific := wildcardPkg + "." + typeName
						if !existingImports[specific] {
							resolvedImports = append(resolvedImports, &parser.ImportDecl{Path: specific})
							existingImports[specific] = true
						}
					}
					continue // don't keep the wildcard
				}
			}
			// External import — keep as-is
			if !existingImports[imp.Path] {
				resolvedImports = append(resolvedImports, imp)
				existingImports[imp.Path] = true
			}
		}

		// Auto-inject imports for cross-package types
		for typeName, typePkg := range typeRegistry {
			if typePkg != myPkg && typePkg != "" {
				importPath := typePkg + "." + typeName
				if !existingImports[importPath] {
					resolvedImports = append(resolvedImports, &parser.ImportDecl{Path: importPath})
					existingImports[importPath] = true
				}
			}
		}
		pf.prog.Imports = resolvedImports

		// Generate Java files
		className := classNameFromFile(pf.path)
		gen := codegen_java.New()
		// Register cross-file type names for codegen
		for _, other := range parsed {
			for _, d := range other.prog.Decls {
				if iface, ok := d.(*parser.InterfaceDecl); ok {
					gen.RegisterInterface(iface.Name)
				}
				// Register actor classes (classes extending Actor or another actor)
				if cls, ok := d.(*parser.ClassDecl); ok {
					for _, parent := range cls.Parents {
						if parent == "Actor" {
							gen.RegisterActor(cls.Name)
						}
					}
				}
			}
		}
		// Second pass: transitive actor detection (class extends an actor class)
		for _, other := range parsed {
			for _, d := range other.prog.Decls {
				if cls, ok := d.(*parser.ClassDecl); ok {
					for _, parent := range cls.Parents {
						if gen.IsActor(parent) {
							gen.RegisterActor(cls.Name)
						}
					}
				}
			}
		}
		outputFiles := gen.GenerateFiles(pf.prog, className)

		pkgDir := outDir
		if pf.prog.Package != nil {
			pkgDir = filepath.Join(outDir, strings.ReplaceAll(pf.prog.Package.Path, ".", string(filepath.Separator)))
			if err := os.MkdirAll(pkgDir, 0755); err != nil {
				return nil, fmt.Errorf("creating package dir: %w", err)
			}
		}

		for _, of := range outputFiles {
			path := filepath.Join(pkgDir, of.Name)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return nil, fmt.Errorf("creating dir for %s: %w", path, err)
			}
			if err := os.WriteFile(path, []byte(of.Content), 0644); err != nil {
				return nil, fmt.Errorf("writing %s: %w", path, err)
			}
			allJavaFiles = append(allJavaFiles, path)
			if verbose {
				fmt.Fprintf(os.Stderr, "[verbose] wrote %s\n", path)
			}
		}
	}

	return allJavaFiles, nil
}

// transpileSingleFile transpiles one .zn file to .java files in outDir.
func transpileSingleFile(inFile, outDir string, verbose bool) ([]string, error) {
	prog, err := parseAndCheck(inFile, verbose)
	if err != nil {
		return nil, err
	}

	className := classNameFromFile(inFile)
	gen := codegen_java.New()
	outputFiles := gen.GenerateFiles(prog, className)

	// If package is declared, create subdirectory structure
	pkgDir := outDir
	if prog.Package != nil {
		pkgDir = filepath.Join(outDir, strings.ReplaceAll(prog.Package.Path, ".", string(filepath.Separator)))
		if err := os.MkdirAll(pkgDir, 0755); err != nil {
			return nil, fmt.Errorf("creating package dir: %w", err)
		}
	}

	var javaFiles []string
	for _, of := range outputFiles {
		path := filepath.Join(pkgDir, of.Name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("creating dir for %s: %w", path, err)
		}
		if err := os.WriteFile(path, []byte(of.Content), 0644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", path, err)
		}
		javaFiles = append(javaFiles, path)
		if verbose {
			fmt.Fprintf(os.Stderr, "[verbose] wrote %s\n", path)
		}
	}

	return javaFiles, nil
}

// compileJava runs javac on the generated .java files.
func compileJava(javaFiles []string, outDir string) error {
	javac, err := exec.LookPath("javac")
	if err != nil {
		return fmt.Errorf("javac not found — install JDK 25+")
	}

	args := []string{"--enable-preview", "--release", "25", "-d", outDir}
	args = append(args, javaFiles...)

	cmd := exec.Command(javac, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("javac failed: %w", err)
	}
	return nil
}

// runJava runs a compiled Java class.
func runJava(classDir, className string, scriptArgs []string) error {
	java, err := exec.LookPath("java")
	if err != nil {
		return fmt.Errorf("java not found — install JDK 25+")
	}

	args := []string{"--enable-preview", "-cp", classDir, className}
	args = append(args, scriptArgs...)

	cmd := exec.Command(java, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func hasPackageDecl(path string) bool {
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return true
		}
		if line != "" && !strings.HasPrefix(line, "//") {
			return false
		}
	}
	return false
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// classNameFromFile derives a Java class name from a .zn filename.
// e.g., "hello_world.zn" → "HelloWorld", "script.zn" → "Script"
func classNameFromFile(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))

	// Convert snake_case to PascalCase
	var result strings.Builder
	upper := true
	for _, ch := range base {
		if ch == '_' || ch == '-' {
			upper = true
			continue
		}
		if upper {
			result.WriteRune(rune(strings.ToUpper(string(ch))[0]))
			upper = false
		} else {
			result.WriteRune(ch)
		}
	}
	return result.String()
}

// runInit scaffolds a new Zinc project.
func runInit(name string) error {
	dirs := []string{
		filepath.Join(name, "src"),
		filepath.Join(name, "test"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}
	}

	// Detect native-image path for build.mill.yaml
	nativeImagePath := ""
	if niPath, err := exec.LookPath("native-image"); err == nil {
		nativeImagePath, _ = filepath.Abs(niPath)
	}

	// build.mill.yaml — Mill build config
	nativeImageLine := ""
	extends := "JavaModule"
	if nativeImagePath != "" {
		extends = "[JavaModule, NativeImageModule]"
		nativeImageLine = fmt.Sprintf("\nnativeImageTool: %s\n", nativeImagePath)
	}
	millConfig := fmt.Sprintf(`# %s — Zinc project (Mill build)
# Docs: https://mill-build.org/mill/index.html
#
# Commands (via zinc CLI or Mill directly):
#   zinc build src/            compile (transpile + mill compile)
#   zinc run src/main.zn       run (transpile + mill run)
#   zinc build --native src/   GraalVM native binary (mill nativeImage)
#   mill assembly              fat JAR
#   mill test                  run tests

extends: %s
jvmVersion: 25

javacOptions:
  - --enable-preview
  - --release
  - "25"

forkArgs:
  - --enable-preview
%s
# Maven Central dependencies
mvnDeps: []
  # - com.google.code.gson:gson:2.11.0
  # - io.quarkus:quarkus-rest:3.17.0

# Custom Maven repositories (optional)
# repositories:
#   - https://repo.example.com/maven2
`, name, extends, nativeImageLine)
	if err := os.WriteFile(filepath.Join(name, "build.mill.yaml"), []byte(millConfig), 0644); err != nil {
		return err
	}

	// src/main.zn
	mainZn := `// main.zn
print("Hello from Zinc!")
`
	if err := os.WriteFile(filepath.Join(name, "src", "main.zn"), []byte(mainZn), 0644); err != nil {
		return err
	}

	// .gitignore
	gitignore := `# Generated by zinc transpiler
src/**/*.java

# Build output
zinc-out/
out/
*.class
dist/
.mill-version
`
	if err := os.WriteFile(filepath.Join(name, ".gitignore"), []byte(gitignore), 0644); err != nil {
		return err
	}

	fmt.Printf("created project: %s/\n", name)
	fmt.Printf("  %s/build.mill.yaml\n", name)
	fmt.Printf("  %s/src/main.zn\n", name)
	fmt.Printf("  %s/.gitignore\n", name)
	fmt.Println("\nGet started:")
	fmt.Printf("  cd %s && zinc run src/main.zn\n", name)
	fmt.Println("\nAdd dependencies in build.mill.yaml under mvnDeps:")
	fmt.Println("  mvnDeps:")
	fmt.Println("    - com.google.code.gson:gson:2.11.0")
	return nil
}

// cleanGeneratedJava removes .java files from the source directory that were generated by zinc.
func cleanGeneratedJava(srcDir string) {
	filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".java") {
			os.Remove(path)
		}
		return nil
	})
}

// cleanMillCache removes Mill's build cache if build.mill.yaml changed since last build.
// This prevents stale cache errors when the build config is modified.
func cleanMillCache(projectDir string) {
	yamlPath := filepath.Join(projectDir, "build.mill.yaml")
	cachePath := filepath.Join(projectDir, "out", "mill-build")

	yamlInfo, err := os.Stat(yamlPath)
	if err != nil {
		return
	}
	cacheInfo, err := os.Stat(cachePath)
	if err != nil {
		return // no cache yet
	}

	if yamlInfo.ModTime().After(cacheInfo.ModTime()) {
		os.RemoveAll(cachePath)
	}
}

// runMill runs a Mill command in the given directory.
func runMill(dir string, millArgs ...string) error {
	mill, err := exec.LookPath("mill")
	if err != nil {
		return fmt.Errorf("mill not found — install Mill: https://mill-build.org/mill/Installation.html")
	}
	cmd := exec.Command(mill, millArgs...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// exitOnMillErr handles a Mill error — forwards exit code if available, else exits 1.
func exitOnMillErr(err error) {
	if exitErr, ok := err.(*exec.ExitError); ok {
		os.Exit(exitErr.ExitCode())
	}
	errs.Error(err.Error())
	os.Exit(1)
}

// hasMillConfig checks if a build.mill.yaml exists in or above the target path.
func hasMillConfig(target string) (string, bool) {
	dir := target
	if !isDir(dir) {
		dir = filepath.Dir(dir)
	}
	// Walk up to find build.mill.yaml
	for {
		if _, err := os.Stat(filepath.Join(dir, "build.mill.yaml")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// buildNativeDirect builds a native binary without Mill — direct native-image/jlink.
func buildNativeDirect(outDir, appName string) error {
	nativeImage, err := exec.LookPath("native-image")
	if err == nil {
		fmt.Println("building native binary with GraalVM native-image...")
		os.MkdirAll("dist", 0755)
		cmd := exec.Command(nativeImage,
			"--enable-preview", "-cp", outDir,
			"-o", filepath.Join("dist", appName),
			classNameFromFile(appName+".zn"),
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			fmt.Printf("native binary: dist/%s\n", appName)
			return nil
		}
		fmt.Fprintln(os.Stderr, "native-image failed, falling back to jlink...")
	}

	jlink, err := exec.LookPath("jlink")
	if err != nil {
		return fmt.Errorf("neither native-image nor jlink found — install GraalVM or JDK 25+")
	}
	os.MkdirAll("dist", 0755)
	distDir := filepath.Join("dist", appName+"-jlink")
	cmd := exec.Command(jlink,
		"--module-path", outDir, "--add-modules", "ALL-MODULE-PATH",
		"--output", distDir, "--strip-debug", "--compress", "zip-6",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("jlink failed: %w", err)
	}
	fmt.Printf("jlink image: %s/\n", distDir)
	return nil
}

// generateDockerfile generates a Dockerfile in the given directory.
// If a native binary exists (from mill nativeImage), generates a minimal native Dockerfile.
// Otherwise, generates a JVM Dockerfile using the fat JAR from mill assembly.
func generateDockerfile(dir, appName string) error {
	nativeBinary := filepath.Join(dir, "out", "nativeImage.dest", "native-executable")
	assemblyJar := filepath.Join(dir, "out", "assembly.dest", "out.jar")

	var dockerfile string
	if _, err := os.Stat(nativeBinary); err == nil {
		// Native binary exists — minimal distroless image
		dockerfile = fmt.Sprintf(`# Zinc native-image Docker build
# Binary built via: zinc build --native src/
FROM gcr.io/distroless/base-nossl-debian12

COPY out/nativeImage.dest/native-executable /app/%s
WORKDIR /app

EXPOSE 8080
ENTRYPOINT ["/app/%s"]
`, appName, appName)
	} else if _, err := os.Stat(assemblyJar); err == nil {
		// Fat JAR exists — JVM image
		dockerfile = `# Zinc JVM Docker build
# JAR built via: mill assembly
FROM eclipse-temurin:25-jre-alpine

WORKDIR /app
COPY out/assembly.dest/out.jar app.jar

EXPOSE 8080
CMD ["java", "--enable-preview", "-jar", "app.jar"]
`
	} else {
		// Fallback — generate a multi-stage native build Dockerfile
		dockerfile = fmt.Sprintf(`# Zinc multi-stage native Docker build
FROM ghcr.io/graalvm/native-image-community:25 AS build

WORKDIR /build
COPY src/ src/
RUN javac --enable-preview --release 25 -d classes src/*.java && \
    native-image --enable-preview -cp classes -o %s Main

FROM gcr.io/distroless/base-nossl-debian12
COPY --from=build /build/%s /app/%s
WORKDIR /app

EXPOSE 8080
ENTRYPOINT ["/app/%s"]
`, appName, appName, appName, appName)
	}

	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(dockerfile), 0644); err != nil {
		return err
	}
	fmt.Printf("generated: %s\n", path)
	return nil
}

// generateK8sManifest creates a K8s deployment manifest in the given directory.
func generateK8sManifest(dir, appName string) error {
	manifest := fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  labels:
    app: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
      - name: %s
        image: %s:latest
        resources:
          requests:
            memory: "64Mi"
            cpu: "100m"
          limits:
            memory: "256Mi"
            cpu: "500m"
`, appName, appName, appName, appName, appName, appName)

	manifestFile := filepath.Join(dir, appName+"-deployment.yaml")
	if err := os.WriteFile(manifestFile, []byte(manifest), 0644); err != nil {
		return err
	}
	fmt.Printf("generated: %s\n", manifestFile)
	return nil
}

// runUpdate updates the Zinc toolchain (GraalVM, Mill, Quarkus).
func runUpdate() error {
	fmt.Println("Updating Zinc toolchain...")

	// Detect OS
	uname := "linux"
	if out, err := exec.Command("uname", "-s").Output(); err == nil {
		if strings.Contains(strings.ToLower(string(out)), "darwin") {
			uname = "darwin"
		}
	}

	if uname == "darwin" {
		// macOS: use Homebrew
		fmt.Println("\n==> Updating GraalVM JDK...")
		runCmd("brew", "upgrade", "--cask", "graalvm-jdk")
		fmt.Println("\n==> Updating Mill...")
		runCmd("brew", "upgrade", "mill")
		fmt.Println("\n==> Updating Quarkus CLI...")
		runCmd("brew", "upgrade", "quarkus")
	} else {
		// Linux: use SDKMAN for JDK/Quarkus, reinstall Mill launcher
		sdkmanInit := filepath.Join(os.Getenv("HOME"), ".sdkman", "bin", "sdkman-init.sh")
		if _, err := os.Stat(sdkmanInit); err != nil {
			return fmt.Errorf("SDKMAN not found — run the Zinc installer first")
		}

		// Install latest GraalVM CE (stays on GraalVM, doesn't switch to Temurin)
		fmt.Println("\n==> Updating GraalVM JDK via SDKMAN...")
		runCmd("bash", "-c", fmt.Sprintf(
			`source %s && LATEST=$(sdk list java 2>/dev/null | grep -oP '\d+\.\d+\.\d+-graalce' | head -1) && `+
				`if [ -n "$LATEST" ]; then sdk install java "$LATEST" && sdk default java "$LATEST"; `+
				`else echo "GraalVM CE already up to date"; fi`, sdkmanInit))

		fmt.Println("\n==> Updating Quarkus CLI via SDKMAN...")
		runCmd("bash", "-c", fmt.Sprintf("source %s && sdk upgrade quarkus", sdkmanInit))

		fmt.Println("\n==> Updating Mill launcher...")
		millURL := "https://raw.githubusercontent.com/com-lihaoyi/mill/main/mill"
		runCmd("bash", "-c", fmt.Sprintf("curl -fsSL %s -o /tmp/mill-update && chmod +x /tmp/mill-update && sudo mv /tmp/mill-update /usr/local/bin/mill", millURL))
	}

	fmt.Println("\n==> Toolchain versions:")
	runCmd("java", "--version")
	runCmd("native-image", "--version")
	runCmd("mill", "version")
	runCmd("quarkus", "--version")
	return nil
}

// runCmd runs a command, printing output. Non-fatal on error.
func runCmd(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "  %s: %v\n", name, err)
	}
}

// ---------------------------------------------------------------------------
// Dependency management: zinc add / remove / deps
// ---------------------------------------------------------------------------

// findBuildConfig finds build.mill.yaml in the current or parent directories.
func findBuildConfig() (string, error) {
	dir, _ := os.Getwd()
	for {
		path := filepath.Join(dir, "build.mill.yaml")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no build.mill.yaml found — run 'zinc init' to create a project")
}

// readBuildDeps reads the mvnDeps list from build.mill.yaml.
func readBuildDeps(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var deps []string
	inMvnDeps := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "mvnDeps:" || strings.HasPrefix(trimmed, "mvnDeps: [") {
			if trimmed == "mvnDeps: []" {
				return nil, nil
			}
			inMvnDeps = true
			continue
		}
		if inMvnDeps {
			if strings.HasPrefix(trimmed, "- ") {
				dep := strings.TrimPrefix(trimmed, "- ")
				dep = strings.TrimSpace(dep)
				if !strings.HasPrefix(dep, "#") {
					deps = append(deps, dep)
				}
			} else if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				break // next YAML key
			}
		}
	}
	return deps, nil
}

// runAddDep adds a Maven dependency to build.mill.yaml.
func runAddDep(dep string) error {
	// Validate format: group:artifact:version
	parts := strings.Split(dep, ":")
	if len(parts) != 3 {
		return fmt.Errorf("invalid dependency format: %s\n  expected: group:artifact:version (e.g., com.google.code.gson:gson:2.11.0)", dep)
	}

	path, err := findBuildConfig()
	if err != nil {
		return err
	}

	// Check for duplicates
	existing, err := readBuildDeps(path)
	if err != nil {
		return err
	}
	artifact := parts[1]
	for _, d := range existing {
		if strings.Contains(d, ":"+artifact+":") || d == dep {
			return fmt.Errorf("%s is already in dependencies", artifact)
		}
	}

	// Read file and insert the dep
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	var result []string
	inserted := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle empty mvnDeps: []
		if trimmed == "mvnDeps: []" {
			result = append(result, "mvnDeps:")
			result = append(result, "  - "+dep)
			inserted = true
			// Skip any commented examples after mvnDeps: []
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if strings.HasPrefix(next, "# ") || strings.HasPrefix(next, "#-") {
					i = j
					continue
				}
				break
			}
			continue
		}

		result = append(result, line)

		// Find last dep in mvnDeps section and insert after
		if !inserted && (trimmed == "mvnDeps:" || strings.HasPrefix(trimmed, "mvnDeps:")) && trimmed != "mvnDeps: []" {
			// Find the last "  - " line in this section
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if strings.HasPrefix(next, "- ") || strings.HasPrefix(next, "#") || next == "" {
					result = append(result, lines[j])
				} else {
					// Insert before this non-dep line
					result = append(result, "  - "+dep)
					inserted = true
					// Re-process remaining lines
					for k := j; k < len(lines); k++ {
						result = append(result, lines[k])
					}
					goto done
				}
			}
			// Reached end of file while in deps section
			result = append(result, "  - "+dep)
			inserted = true
			goto done
		}
	}

done:
	if !inserted {
		return fmt.Errorf("could not find mvnDeps section in %s", path)
	}

	if err := os.WriteFile(path, []byte(strings.Join(result, "\n")), 0644); err != nil {
		return err
	}
	fmt.Printf("added: %s\n", dep)
	return nil
}

// runRemoveDep removes a dependency from build.mill.yaml by artifact name.
func runRemoveDep(artifact string) error {
	path, err := findBuildConfig()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	var result []string
	removed := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			dep := strings.TrimPrefix(trimmed, "- ")
			if strings.Contains(dep, ":"+artifact+":") || strings.Contains(dep, artifact) {
				removed = dep
				continue // skip this line
			}
		}
		result = append(result, line)
	}

	if removed == "" {
		return fmt.Errorf("dependency '%s' not found in build.mill.yaml", artifact)
	}

	if err := os.WriteFile(path, []byte(strings.Join(result, "\n")), 0644); err != nil {
		return err
	}
	fmt.Printf("removed: %s\n", removed)
	return nil
}

// runListDeps lists dependencies from build.mill.yaml.
func runListDeps() error {
	path, err := findBuildConfig()
	if err != nil {
		return err
	}

	deps, err := readBuildDeps(path)
	if err != nil {
		return err
	}

	if len(deps) == 0 {
		fmt.Println("no dependencies")
		return nil
	}

	fmt.Printf("dependencies (%s):\n", filepath.Base(filepath.Dir(path)))
	for _, dep := range deps {
		parts := strings.Split(dep, ":")
		if len(parts) == 3 {
			fmt.Printf("  %s (%s:%s)\n", parts[1], parts[0], parts[2])
		} else {
			fmt.Printf("  %s\n", dep)
		}
	}
	return nil
}
