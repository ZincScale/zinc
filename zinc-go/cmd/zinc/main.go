package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	codegen "zinc-go/internal/codegen_go"
	"zinc-go/internal/errs"
	"zinc-go/internal/lexer"
	"zinc-go/internal/parser"
)

// version is set via ldflags: -X main.version=v1.0.0
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		name := ""
		if len(os.Args) >= 3 {
			name = os.Args[2]
		}
		if name == "" {
			errs.Error("zinc init requires a project name")
			fmt.Fprintln(os.Stderr, "Usage: zinc init <name>")
			os.Exit(1)
		}
		if err := initProject(name); err != nil {
			errs.Errorf("%s", err)
			os.Exit(1)
		}

	case "build":
		input := "."
		if len(os.Args) >= 3 && !strings.HasPrefix(os.Args[2], "-") {
			input = os.Args[2]
		}
		outDir := "zinc-out"
		crossTarget := ""
		for i, arg := range os.Args {
			if arg == "-o" && i+1 < len(os.Args) {
				outDir = os.Args[i+1]
			}
			if arg == "--cross" && i+1 < len(os.Args) {
				crossTarget = os.Args[i+1] // e.g. "linux/amd64"
			}
		}
		// Detect project mode: zinc.toml present
		if info, err := os.Stat(input); err == nil && info.IsDir() && isProjectDir(input) {
			if err := buildProject(input, outDir, false); err != nil {
				errs.Errorf("%s", err)
				os.Exit(1)
			}
		} else {
			if err := build(input, outDir, false); err != nil {
				errs.Errorf("%s", err)
				os.Exit(1)
			}
		}
		// Cross-compile if requested
		if crossTarget != "" {
			if err := crossCompile(outDir, crossTarget); err != nil {
				errs.Errorf("%s", err)
				os.Exit(1)
			}
		}

	case "run":
		input := "."
		if len(os.Args) >= 3 && !strings.HasPrefix(os.Args[2], "-") {
			input = os.Args[2]
		}
		// Collect program args after "--"
		var progArgs []string
		for i := 2; i < len(os.Args); i++ {
			if os.Args[i] == "--" {
				progArgs = os.Args[i+1:]
				break
			}
		}
		// Detect project mode: zinc.toml present
		if info, err := os.Stat(input); err == nil && info.IsDir() && isProjectDir(input) {
			if err := runProject(input, progArgs); err != nil {
				errs.Errorf("%s", err)
				os.Exit(1)
			}
		} else {
			if err := run(input, progArgs); err != nil {
				errs.Errorf("%s", err)
				os.Exit(1)
			}
		}

	case "fmt":
		if len(os.Args) < 3 {
			errs.Error("zinc fmt requires a file or directory")
			fmt.Fprintln(os.Stderr, "Usage: zinc fmt <file.zn|dir>")
			os.Exit(1)
		}
		target := os.Args[2]
		info, err := os.Stat(target)
		if err != nil {
			errs.Errorf("%s", err)
			os.Exit(1)
		}
		if info.IsDir() {
			if err := fmtDir(target); err != nil {
				errs.Errorf("%s", err)
				os.Exit(1)
			}
		} else {
			if err := fmtFile(target); err != nil {
				errs.Errorf("%s", err)
				os.Exit(1)
			}
		}

	case "add":
		if len(os.Args) < 3 {
			errs.Error("zinc add requires a Go module path")
			fmt.Fprintln(os.Stderr, "Usage: zinc add <module@version>")
			fmt.Fprintln(os.Stderr, "  e.g.: zinc add github.com/gorilla/mux@v1.8.1")
			os.Exit(1)
		}
		if err := addDep(os.Args[2]); err != nil {
			errs.Errorf("%s", err)
			os.Exit(1)
		}

	case "deps":
		if err := listDeps(); err != nil {
			errs.Errorf("%s", err)
			os.Exit(1)
		}

	case "--version", "version":
		fmt.Printf("zinc %s (Go backend)\n", version)

	case "--help", "help":
		printUsage()

	default:
		// If it ends with .zn, treat as shorthand for zinc run
		if strings.HasSuffix(os.Args[1], ".zn") {
			// Collect any args after the filename
			var progArgs []string
			for i := 2; i < len(os.Args); i++ {
				if os.Args[i] == "--" {
					progArgs = os.Args[i+1:]
					break
				}
			}
			if err := run(os.Args[1], progArgs); err != nil {
				errs.Errorf("%s", err)
				os.Exit(1)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
			printUsage()
			os.Exit(1)
		}
	}
}

func printUsage() {
	fmt.Println("zinc - Zinc to Go transpiler")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  zinc init <name>                         Create a new Zinc project")
	fmt.Println("  zinc build [dir] [-o outdir] [--cross os/arch]")
	fmt.Println("                                           Transpile and compile to native binary")
	fmt.Println("  zinc run [file.zn|dir] [-- args...]      Transpile and run")
	fmt.Println("  zinc fmt <file.zn|dir>                   Format Zinc source code")
	fmt.Println("  zinc add <module@version>                Add a Go dependency")
	fmt.Println("  zinc deps                                List dependencies")
	fmt.Println("  zinc <file.zn> [-- args...]              Shorthand for zinc run")
	fmt.Println("  zinc version                             Show version")
	fmt.Println()
	fmt.Println("Project mode: when a zinc.toml is present, build/run use the project config.")
	fmt.Println()
	fmt.Println("Cross-compilation targets: linux/amd64, linux/arm64, darwin/amd64,")
	fmt.Println("  darwin/arm64, windows/amd64, windows/arm64")
}

// ---------------------------------------------------------------------------
// Compilation
// ---------------------------------------------------------------------------

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

// compileMultiFile parses all .zn files, merges their ASTs, and runs codegen
// once on the combined program. This gives the generator cross-file knowledge
// of types, constructors, and error-returning functions.
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

	// Parse all files
	progs := make([]*parser.Program, 0, len(znFiles))
	for _, path := range znFiles {
		prog, err := parseFile(path)
		if err != nil {
			return err
		}
		progs = append(progs, prog)
	}

	// Merge into one combined program
	merged := mergePrograms(progs)

	// Determine a className from the directory name
	dirName := filepath.Base(filepath.Dir(znFiles[0]))
	className := strings.ToUpper(dirName[:1]) + dirName[1:]

	// Generate with full cross-file context
	gen := codegen.New()
	gen.SetSourceFile(merged.SourceFile)
	files := gen.GenerateFiles(merged, className)

	for _, f := range files {
		outPath := filepath.Join(outDir, f.Name)
		if wErr := os.WriteFile(outPath, []byte(f.Content), 0o644); wErr != nil {
			return fmt.Errorf("write %s: %w", outPath, wErr)
		}
		if !quiet {
			fmt.Printf("  [%d files] → %s\n", len(znFiles), outPath)
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

	// 3. Compile each subpackage first, collect their exports
	allExports := make(map[string]map[string]string) // pkg → name → kind
	for _, pkg := range leafPkgs {
		pkgDir := filepath.Join(srcDir, pkg)
		pkgOutDir := filepath.Join(outDir, pkg)

		znFiles, err := collectZnFilesFlat(pkgDir)
		if err != nil {
			return fmt.Errorf("collect files in %s: %w", pkgDir, err)
		}
		if len(znFiles) == 0 {
			continue
		}

		// Parse and merge files in this subpackage
		progs := make([]*parser.Program, 0, len(znFiles))
		for _, path := range znFiles {
			prog, err := parseFile(path)
			if err != nil {
				return err
			}
			progs = append(progs, prog)
		}
		merged := mergePrograms(progs)

		// Collect exports before generating
		exports := codegen.CollectExports(merged)
		allExports[pkg] = exports

		// Generate Go code for this subpackage
		if err := os.MkdirAll(pkgOutDir, 0o755); err != nil {
			return err
		}

		// Go package name is the last segment of the path (e.g. "fabric/router" → "router")
		goPkgName := filepath.Base(pkg)

		gen := codegen.New()
		gen.SetSourceFile(merged.SourceFile)
		gen.SetPackageName(goPkgName)
		gen.SetModuleName(moduleName)
		gen.SetZincSubpackages(subpackages)
		if len(importAliases) > 0 && importAliases[0] != nil {
			gen.SetImportAliases(importAliases[0])
		}
		// Subpackages can import other subpackages
		for otherPkg, otherExports := range allExports {
			if otherPkg != pkg {
				// Register exports under the Go alias (last segment)
				otherAlias := filepath.Base(otherPkg)
				gen.SetSubpackageExports(otherAlias, otherExports)
			}
		}

		className := strings.ToUpper(goPkgName[:1]) + goPkgName[1:]
		files := gen.GenerateFiles(merged, className)

		for _, f := range files {
			outPath := filepath.Join(pkgOutDir, f.Name)
			if wErr := os.WriteFile(outPath, []byte(f.Content), 0o644); wErr != nil {
				return fmt.Errorf("write %s: %w", outPath, wErr)
			}
			if !quiet {
				fmt.Printf("  [%s] %d files → %s\n", pkg, len(znFiles), outPath)
			}
		}
	}

	// 3. Compile root files (package main) with knowledge of all subpackages
	rootFiles, err := collectZnFilesFlat(srcDir)
	if err != nil {
		return err
	}
	if len(rootFiles) == 0 {
		return nil
	}

	progs := make([]*parser.Program, 0, len(rootFiles))
	for _, path := range rootFiles {
		prog, err := parseFile(path)
		if err != nil {
			return err
		}
		progs = append(progs, prog)
	}
	merged := mergePrograms(progs)

	gen := codegen.New()
	gen.SetSourceFile(merged.SourceFile)
	gen.SetModuleName(moduleName)
	gen.SetZincSubpackages(subpackages)
	if len(importAliases) > 0 && importAliases[0] != nil {
		gen.SetImportAliases(importAliases[0])
	}
	for pkg, exports := range allExports {
		alias := filepath.Base(pkg)
		gen.SetSubpackageExports(alias, exports)
	}

	dirName := filepath.Base(srcDir)
	className := strings.ToUpper(dirName[:1]) + dirName[1:]
	files := gen.GenerateFiles(merged, className)

	for _, f := range files {
		outPath := filepath.Join(outDir, f.Name)
		if wErr := os.WriteFile(outPath, []byte(f.Content), 0o644); wErr != nil {
			return fmt.Errorf("write %s: %w", outPath, wErr)
		}
		if !quiet {
			fmt.Printf("  [main] %d files → %s\n", len(rootFiles), outPath)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Build
// ---------------------------------------------------------------------------

// build transpiles .zn file(s) to .go, writes them to outDir, and then
// invokes `go build` to produce a native binary.
func build(input, outDir string, quiet bool) error {
	info, err := os.Stat(input)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	if info.IsDir() {
		if err := compileDir(input, outDir, quiet); err != nil {
			return err
		}
	} else {
		files, cErr := compileFile(input)
		if cErr != nil {
			return cErr
		}
		for _, f := range files {
			outPath := filepath.Join(outDir, f.Name)
			if wErr := os.WriteFile(outPath, []byte(f.Content), 0o644); wErr != nil {
				return fmt.Errorf("write %s: %w", outPath, wErr)
			}
			if !quiet {
				fmt.Printf("  %s → %s\n", input, outPath)
			}
		}
	}

	// Write a go.mod so `go build` works in the output directory
	goModPath := filepath.Join(outDir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		goMod := "module zinc_build\n\ngo 1.26\n"
		if wErr := os.WriteFile(goModPath, []byte(goMod), 0o644); wErr != nil {
			return fmt.Errorf("write go.mod: %w", wErr)
		}
	}

	// Run go build
	cmd := exec.Command("go", "build", "-o", "zinc-app", ".")
	cmd.Dir = outDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if bErr := cmd.Run(); bErr != nil {
		return fmt.Errorf("go build failed: %w", bErr)
	}

	absOut, _ := filepath.Abs(filepath.Join(outDir, "zinc-app"))
	if !quiet {
		fmt.Printf("  Built: %s\n", absOut)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------

// run transpiles .zn file(s) to a temp directory and executes the result,
// passing progArgs to the compiled program.
func run(input string, progArgs []string) error {
	tmpDir, err := os.MkdirTemp("", "zinc-run-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// Write go.mod for the temporary module
	goMod := "module zinc_run\n\ngo 1.26\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return err
	}

	// Compile source files into the temp directory (quiet mode — no progress)
	info, sErr := os.Stat(input)
	if sErr != nil {
		return sErr
	}

	if info.IsDir() {
		if err := compileDir(input, tmpDir, true); err != nil {
			return err
		}
	} else {
		files, cErr := compileFile(input)
		if cErr != nil {
			return cErr
		}
		for _, f := range files {
			outPath := filepath.Join(tmpDir, f.Name)
			if wErr := os.WriteFile(outPath, []byte(f.Content), 0o644); wErr != nil {
				return fmt.Errorf("write %s: %w", outPath, wErr)
			}
		}
	}

	// Build and run
	binPath := filepath.Join(tmpDir, "zinc-app")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = tmpDir
	buildCmd.Stderr = os.Stderr
	if bErr := buildCmd.Run(); bErr != nil {
		return fmt.Errorf("go build failed: %w", bErr)
	}

	runArgs := append([]string{}, progArgs...)
	runCmd := exec.Command(binPath, runArgs...)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	runCmd.Stdin = os.Stdin
	if rErr := runCmd.Run(); rErr != nil {
		// If the program exited with a non-zero code, propagate the exit code
		if exitErr, ok := rErr.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return rErr
	}
	return nil
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

// initProject scaffolds a new Zinc project directory.
func initProject(name string) error {
	// Ensure the directory doesn't already exist
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %q already exists", name)
	}

	srcDir := filepath.Join(name, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return fmt.Errorf("create src/: %w", err)
	}

	// zinc.toml — project config
	baseName := filepath.Base(name)
	zincToml := fmt.Sprintf(`[project]
name = "%s"
version = "0.1.0"
main = "main.zn"

[go]
version = "1.26"
deps = []
`, baseName)
	if err := os.WriteFile(filepath.Join(name, "zinc.toml"), []byte(zincToml), 0o644); err != nil {
		return fmt.Errorf("write zinc.toml: %w", err)
	}

	// src/main.zn
	mainZn := fmt.Sprintf(`fn main() {
    print("Hello from %s!")
}
`, baseName)
	if err := os.WriteFile(filepath.Join(srcDir, "main.zn"), []byte(mainZn), 0o644); err != nil {
		return fmt.Errorf("write main.zn: %w", err)
	}

	// .gitignore
	gitignore := "zinc-out/\n*.exe\ngo.mod\ngo.sum\n*.go\n"
	if err := os.WriteFile(filepath.Join(name, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}

	fmt.Printf("Created project %s/\n", name)
	fmt.Printf("  %s/zinc.toml\n", name)
	fmt.Printf("  %s/src/main.zn\n", name)
	fmt.Printf("  %s/.gitignore\n", name)
	fmt.Println()
	fmt.Printf("Run: cd %s && zinc run\n", name)
	return nil
}

// ---------------------------------------------------------------------------
// Format
// ---------------------------------------------------------------------------

// fmtFile formats a single .zn file in place using a token-level formatter.
func fmtFile(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	formatted := formatZinc(string(src))

	if err := os.WriteFile(path, []byte(formatted), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("  formatted %s\n", path)
	return nil
}

// fmtDir formats all .zn files in a directory recursively.
func fmtDir(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".zn") {
			return fmtFile(path)
		}
		return nil
	})
}

// formatZinc reformats Zinc source code with consistent indentation.
// It operates line-by-line: decrease indent before a line starting with '}',
// increase indent after a line ending with '{'.
func formatZinc(src string) string {
	lines := strings.Split(src, "\n")
	var out strings.Builder
	indent := 0
	const indentStr = "    " // 4 spaces

	for i, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)

		// Preserve empty lines
		if trimmed == "" {
			if i < len(lines)-1 {
				out.WriteByte('\n')
			}
			continue
		}

		// Decrease indent before lines that start with '}'
		if strings.HasPrefix(trimmed, "}") {
			indent--
			if indent < 0 {
				indent = 0
			}
		}

		// Write indented line
		for j := 0; j < indent; j++ {
			out.WriteString(indentStr)
		}
		out.WriteString(trimmed)
		if i < len(lines)-1 {
			out.WriteByte('\n')
		}

		// Increase indent after lines that end with '{'
		if strings.HasSuffix(trimmed, "{") {
			indent++
		}
	}

	// Ensure file ends with a newline
	result := out.String()
	if len(result) > 0 && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

// ---------------------------------------------------------------------------
// zinc.toml project config
// ---------------------------------------------------------------------------

// zincConfig holds parsed zinc.toml fields.
type zincConfig struct {
	Name     string
	Version  string
	Main     string
	GoVer    string
	Deps     []string
	Imports  map[string]string // import alias → module path (e.g. "stdlib" → "github.com/ZincScale/zinc-stdlib")
	Replaces map[string]string // module → local path (for local development)
}

// findZincToml walks up from dir looking for zinc.toml.
func findZincToml(dir string) string {
	abs, _ := filepath.Abs(dir)
	for {
		candidate := filepath.Join(abs, "zinc.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
		}
		abs = parent
	}
}

// loadZincToml parses a zinc.toml file (simple line-based TOML subset).
func loadZincToml(path string) (*zincConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &zincConfig{
		Version:  "0.1.0",
		Main:     "main.zn",
		GoVer:    "1.26",
		Imports:  make(map[string]string),
		Replaces: make(map[string]string),
	}
	section := "" // current TOML section
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Track section headers
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]")
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"")

		// [imports] section: alias = "module/path"
		if section == "imports" {
			cfg.Imports[key] = val
			continue
		}

		// [go.replace] section: module = "local/path"
		if section == "go.replace" {
			cfg.Replaces[key] = val
			continue
		}

		switch key {
		case "name":
			cfg.Name = val
		case "version":
			if strings.Contains(val, ".") && !strings.Contains(val, ">") {
				cfg.Version = val
			} else {
				cfg.GoVer = strings.Trim(val, "\">=")
			}
		case "main":
			cfg.Main = val
		case "deps":
			// Parse simple TOML array: ["dep1", "dep2"]
			val = strings.Trim(val, "[]")
			if val != "" {
				for _, d := range strings.Split(val, ",") {
					d = strings.TrimSpace(d)
					d = strings.Trim(d, "\"")
					if d != "" {
						cfg.Deps = append(cfg.Deps, d)
					}
				}
			}
		}
	}
	return cfg, nil
}

// generateGoMod creates a go.mod from zinc.toml config.
func generateGoMod(cfg *zincConfig, dir string) error {
	var buf strings.Builder
	modName := cfg.Name
	if modName == "" {
		modName = "zinc_project"
	}
	buf.WriteString(fmt.Sprintf("module %s\n\ngo %s\n", modName, cfg.GoVer))
	if len(cfg.Deps) > 0 {
		buf.WriteString("\nrequire (\n")
		for _, dep := range cfg.Deps {
			// dep format: "github.com/foo/bar v1.2.3"
			buf.WriteString(fmt.Sprintf("\t%s\n", dep))
		}
		buf.WriteString(")\n")
	}
	if len(cfg.Replaces) > 0 {
		buf.WriteString("\nreplace (\n")
		for mod, localPath := range cfg.Replaces {
			buf.WriteString(fmt.Sprintf("\t%s => %s\n", mod, localPath))
		}
		buf.WriteString(")\n")
	}
	return os.WriteFile(filepath.Join(dir, "go.mod"), []byte(buf.String()), 0o644)
}

// isProjectDir returns true if dir contains a zinc.toml.
func isProjectDir(dir string) bool {
	return findZincToml(dir) != ""
}

// buildProject transpiles a zinc.toml project: src/*.zn → zinc-out/ → go build.
func buildProject(projectDir, outDir string, quiet bool) error {
	tomlPath := findZincToml(projectDir)
	if tomlPath == "" {
		return fmt.Errorf("no zinc.toml found in %s or parents", projectDir)
	}
	cfg, err := loadZincToml(tomlPath)
	if err != nil {
		return fmt.Errorf("read zinc.toml: %w", err)
	}

	root := filepath.Dir(tomlPath)
	srcDir := filepath.Join(root, "src")
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return fmt.Errorf("no src/ directory in project %s", root)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	// Transpile src/ → outDir/
	// Check if there are subdirectories (subpackages)
	subdirs, _ := collectSubdirs(srcDir)
	moduleName := cfg.Name
	if moduleName == "" {
		moduleName = "zinc_project"
	}

	if len(subdirs) > 0 {
		if err := compileDirWithSubpackages(srcDir, outDir, moduleName, quiet, cfg.Imports); err != nil {
			return err
		}
	} else {
		if err := compileDir(srcDir, outDir, quiet); err != nil {
			return err
		}
	}

	// Generate go.mod from zinc.toml
	if err := generateGoMod(cfg, outDir); err != nil {
		return fmt.Errorf("generate go.mod: %w", err)
	}

	// If there are deps, run go mod tidy
	if len(cfg.Deps) > 0 {
		tidy := exec.Command("go", "mod", "tidy")
		tidy.Dir = outDir
		tidy.Stdout = os.Stdout
		tidy.Stderr = os.Stderr
		if err := tidy.Run(); err != nil {
			return fmt.Errorf("go mod tidy: %w", err)
		}
	}

	// Build
	binName := cfg.Name
	if binName == "" {
		binName = "zinc-app"
	}
	cmd := exec.Command("go", "build", "-o", binName, ".")
	cmd.Dir = outDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}

	if !quiet {
		absOut, _ := filepath.Abs(filepath.Join(outDir, binName))
		fmt.Printf("  Built: %s\n", absOut)
	}
	return nil
}

// runProject transpiles a zinc.toml project to a temp dir and runs it.
func runProject(projectDir string, progArgs []string) error {
	tomlPath := findZincToml(projectDir)
	if tomlPath == "" {
		return fmt.Errorf("no zinc.toml found in %s or parents", projectDir)
	}
	cfg, err := loadZincToml(tomlPath)
	if err != nil {
		return fmt.Errorf("read zinc.toml: %w", err)
	}

	root := filepath.Dir(tomlPath)
	srcDir := filepath.Join(root, "src")

	tmpDir, err := os.MkdirTemp("", "zinc-run-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// Transpile
	subdirs, _ := collectSubdirs(srcDir)
	moduleName := cfg.Name
	if moduleName == "" {
		moduleName = "zinc_project"
	}

	if len(subdirs) > 0 {
		if err := compileDirWithSubpackages(srcDir, tmpDir, moduleName, true, cfg.Imports); err != nil {
			return err
		}
	} else {
		if err := compileDir(srcDir, tmpDir, true); err != nil {
			return err
		}
	}

	// Generate go.mod
	if err := generateGoMod(cfg, tmpDir); err != nil {
		return err
	}

	if len(cfg.Deps) > 0 {
		tidy := exec.Command("go", "mod", "tidy")
		tidy.Dir = tmpDir
		tidy.Stderr = os.Stderr
		if err := tidy.Run(); err != nil {
			return fmt.Errorf("go mod tidy: %w", err)
		}
	}

	// Build and run
	binPath := filepath.Join(tmpDir, "zinc-app")
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = tmpDir
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}

	runCmd := exec.Command(binPath, progArgs...)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	runCmd.Stdin = os.Stdin
	return runCmd.Run()
}

// ---------------------------------------------------------------------------
// Dependency management
// ---------------------------------------------------------------------------

// addDep adds a Go module dependency to zinc.toml.
func addDep(dep string) error {
	tomlPath := findZincToml(".")
	if tomlPath == "" {
		return fmt.Errorf("no zinc.toml found — run zinc init first")
	}

	data, err := os.ReadFile(tomlPath)
	if err != nil {
		return err
	}

	content := string(data)

	// Parse module@version → "module version"
	depEntry := strings.Replace(dep, "@", " ", 1)

	// Check for duplicate
	if strings.Contains(content, depEntry) || strings.Contains(content, dep) {
		return fmt.Errorf("dependency %s already exists", dep)
	}

	// Find deps = [...] and add to it
	if strings.Contains(content, "deps = []") {
		content = strings.Replace(content, "deps = []",
			fmt.Sprintf("deps = [\"%s\"]", depEntry), 1)
	} else if strings.Contains(content, "deps = [") {
		// Append to existing array — find the closing ]
		idx := strings.Index(content, "deps = [")
		closeBracket := strings.Index(content[idx:], "]")
		if closeBracket > 0 {
			insertAt := idx + closeBracket
			content = content[:insertAt] + fmt.Sprintf(", \"%s\"", depEntry) + content[insertAt:]
		}
	} else {
		// No deps line — add under [go] section
		content += fmt.Sprintf("deps = [\"%s\"]\n", depEntry)
	}

	if err := os.WriteFile(tomlPath, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Printf("  added: %s\n", dep)
	return nil
}

// listDeps lists dependencies from zinc.toml.
func listDeps() error {
	tomlPath := findZincToml(".")
	if tomlPath == "" {
		return fmt.Errorf("no zinc.toml found")
	}
	cfg, err := loadZincToml(tomlPath)
	if err != nil {
		return err
	}

	if len(cfg.Deps) == 0 {
		fmt.Println("No dependencies.")
		return nil
	}

	fmt.Printf("Dependencies (%s):\n", cfg.Name)
	for _, d := range cfg.Deps {
		fmt.Printf("  %s\n", d)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cross-compilation
// ---------------------------------------------------------------------------

// crossCompile builds for a target platform. Target format: "os/arch" e.g. "linux/amd64"
func crossCompile(outDir, target string) error {
	parts := strings.SplitN(target, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid cross target %q — use os/arch (e.g. linux/amd64)", target)
	}
	goos, goarch := parts[0], parts[1]

	// Determine binary name
	binName := "zinc-app"
	tomlPath := findZincToml(".")
	if tomlPath != "" {
		if cfg, err := loadZincToml(tomlPath); err == nil && cfg.Name != "" {
			binName = cfg.Name
		}
	}
	if goos == "windows" {
		binName += ".exe"
	}
	outBin := binName + "-" + goos + "-" + goarch
	if goos == "windows" {
		outBin = binName[:len(binName)-4] + "-" + goos + "-" + goarch + ".exe"
	}

	cmd := exec.Command("go", "build", "-o", outBin, ".")
	cmd.Dir = outDir
	cmd.Env = append(os.Environ(),
		"GOOS="+goos,
		"GOARCH="+goarch,
		"CGO_ENABLED=0",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cross-compile %s failed: %w", target, err)
	}

	absOut, _ := filepath.Abs(filepath.Join(outDir, outBin))
	fmt.Printf("  Cross-compiled: %s (%s/%s)\n", absOut, goos, goarch)
	return nil
}
