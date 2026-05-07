package main

// Project management: build, run, init, format, zinc.toml, deps, cross-compile.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	codegen "zinc-go/internal/codegen_go"
	"zinc-go/internal/parser"
)

// ---------------------------------------------------------------------------
// Build
// ---------------------------------------------------------------------------

// cleanOutDir removes stale generated files from a previous build.
// Preserves go.mod and go.sum so dependencies don't need re-downloading.
func cleanOutDir(outDir string) {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return // doesn't exist yet — nothing to clean
	}
	for _, e := range entries {
		name := e.Name()
		// Keep dependency files
		if name == "go.mod" || name == "go.sum" {
			continue
		}
		os.RemoveAll(filepath.Join(outDir, name))
	}
}

// build transpiles .zn file(s) to .go, writes them to outDir, and then
// invokes `go build` to produce a native binary.
func build(input, outDir string, quiet bool) error {
	info, err := os.Stat(input)
	if err != nil {
		return err
	}

	cleanOutDir(outDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	if info.IsDir() {
		if err := compileDir(input, outDir, quiet); err != nil {
			return err
		}
	} else {
		files, cErr := compileFile(input, outDir)
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
		files, cErr := compileFile(input, tmpDir)
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
//
//     [project]
//     name    = "myapp"
//     version = "0.1.0"
//     main    = "main.zn"
//
//     [go]
//     version = "1.26"
//
//     [deps]
//     viper    = "github.com/spf13/viper@v1.20.1"      # module + version → go.mod require
//     viperRem = "github.com/spf13/viper/remote"       # subpackage alias only — no require
//
//     [replace]
//     viper = "/home/local/fork-of-viper"
//
// [deps] is the single dependency table — keys are the local aliases used
// in Zinc (`import viper`), values are the fully-qualified Go import paths.
// An optional `@version` suffix promotes the entry to a go.mod require
// line. Entries without `@version` are alias-only (used for subpackages
// of a module already required, or for [replace]-overridden local deps,
// where v0.0.0 is emitted as the placeholder version).
//
// [replace] keys off the same alias so deps + replaces can never drift
// apart. Relative [replace] paths are resolved against the directory
// containing zinc.toml, letting devs commit a portable override that
// assumes a sibling repo layout (e.g. "../zinc-stdlib/zinc-out") instead
// of an absolute path.
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
	// replaceByAlias holds [replace] entries that key off [deps] aliases —
	// resolved after the whole file is parsed so [replace] can precede or
	// follow [deps] without order dependence.
	replaceByAlias := make(map[string]string)

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
		key = strings.Trim(key, "\"")
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"")

		// [deps] — unified table: alias = "module/path[@version]".
		// With @version: alias-resolved import + go.mod require.
		// Without @version: alias-resolved import only (subpackage of an
		// already-required module, or [replace]-overridden local dep —
		// the post-parse [replace] pass promotes those to v0.0.0 requires).
		if section == "deps" {
			modulePath, version := splitModuleVersion(val)
			cfg.Imports[key] = modulePath
			if version != "" {
				cfg.Deps = append(cfg.Deps, modulePath+" "+version)
			}
			continue
		}

		// [replace] — keyed by alias. Resolved to module path after
		// parsing, since [deps] may come after [replace] in the file.
		if section == "replace" {
			replaceByAlias[key] = val
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
		}
	}

	// Resolve [replace] aliases to module paths now that [deps] has been parsed.
	// Relative local paths are resolved against the manifest's directory and
	// canonicalized to absolute, so the path is anchored regardless of CWD when
	// it's later stat'd or copied verbatim into the generated go.mod.
	manifestDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	// Track which module paths already have a require line so we don't
	// emit duplicates when promoting a [replace]-only entry.
	requiredModules := make(map[string]bool)
	for _, dep := range cfg.Deps {
		if i := strings.Index(dep, " "); i > 0 {
			requiredModules[dep[:i]] = true
		}
	}
	for key, localPath := range replaceByAlias {
		// Resolution order: alias match first (the canonical zinc.toml
		// shape — `stdlib = ".."`), then full module-path match
		// (`"github.com/ZincScale/zinc-stdlib" = ".."`) so users coming
		// from go.mod conventions don't trip on the alias-only rule.
		modulePath, ok := cfg.Imports[key]
		if !ok {
			// Strip @version suffix on the [deps] value before comparing
			// since [replace] keys are bare module paths.
			for alias, dep := range cfg.Imports {
				bareDep := dep
				if at := strings.Index(dep, "@"); at >= 0 {
					bareDep = dep[:at]
				}
				if bareDep == key || dep == key {
					modulePath = cfg.Imports[alias]
					ok = true
					break
				}
			}
		}
		if !ok {
			return nil, fmt.Errorf("zinc.toml: [replace] %q has no matching [deps] entry", key)
		}
		if !filepath.IsAbs(localPath) {
			localPath = filepath.Join(manifestDir, localPath)
		}
		cfg.Replaces[modulePath] = localPath
		// go.mod requires every replace target to also appear in require.
		// If the [deps] entry didn't carry an @version, synthesize a
		// v0.0.0 placeholder so the replace lands.
		if !requiredModules[modulePath] {
			cfg.Deps = append(cfg.Deps, modulePath+" v0.0.0")
			requiredModules[modulePath] = true
		}
	}
	return cfg, nil
}

// splitModuleVersion splits "github.com/foo/bar@v1.2.3" into ("github.com/foo/bar", "v1.2.3").
// Missing @version yields ("github.com/foo/bar", "").
func splitModuleVersion(s string) (string, string) {
	at := strings.LastIndex(s, "@")
	if at < 0 {
		return s, ""
	}
	return s[:at], s[at+1:]
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

// loadDepClassDecls loads class declarations from external deps that
// have a [replace] pointing at a local directory. The replace path
// points at the dep's built `zinc-out/`; the Zinc sources live at the
// sibling `src/` directory. Each subdirectory of `src/` is a
// subpackage; its class decls are registered under the subpackage's
// directory name so the codegen can type-check cross-package returns
// (e.g. `return errors.ConfigError(...)` knows ConfigError extends Err).
func loadDepClassDecls(cfg *zincConfig) map[string]map[string]*parser.ClassDecl {
	out := make(map[string]map[string]*parser.ClassDecl)
	for _, modulePath := range cfg.Imports {
		localPath, ok := cfg.Replaces[modulePath]
		if !ok {
			continue
		}
		// <replace-path>/../src
		srcDir := filepath.Join(filepath.Dir(localPath), "src")
		info, err := os.Stat(srcDir)
		if err != nil || !info.IsDir() {
			continue
		}
		subdirs, err := collectSubdirs(srcDir)
		if err != nil {
			continue
		}
		for _, sub := range subdirs {
			subPath := filepath.Join(srcDir, sub)
			znFiles, _ := collectZnFilesFlat(subPath)
			if len(znFiles) == 0 {
				continue
			}
			progs := make([]*parser.Program, 0, len(znFiles))
			for _, path := range znFiles {
				prog, err := parseFile(path)
				if err != nil {
					continue
				}
				progs = append(progs, prog)
			}
			if len(progs) == 0 {
				continue
			}
			merged := mergePrograms(progs)
			out[sub] = codegen.CollectClassDecls(merged)
		}
	}
	return out
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

	cleanOutDir(outDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	// Generate go.mod FIRST so the type resolver can find module dependencies
	if err := generateGoMod(cfg, outDir); err != nil {
		return fmt.Errorf("generate go.mod: %w", err)
	}

	// Pre-transpile, run `go mod download` (NOT tidy) so every dep
	// listed in the just-written go.mod ends up in GOMODCACHE. The
	// resolver's packages.Load then works during transpile.
	//
	// Why download not tidy: tidy PRUNES unused deps. At this point
	// no .go files exist in outDir yet, so every declared dep looks
	// unused — tidy would strip the whole require list. download
	// just pulls listed deps without pruning. Tidy still runs AFTER
	// transpile to clean up indirect deps once .go files reveal what's
	// actually used. Mirrors the testProject path's earlier fix.
	if len(cfg.Deps) > 0 {
		dl := exec.Command("go", "mod", "download", "all")
		dl.Dir = outDir
		if !quiet {
			dl.Stderr = os.Stderr
		}
		if err := dl.Run(); err != nil && !quiet {
			fmt.Fprintf(os.Stderr, "warning: go mod download: %v\n", err)
		}
	}

	// Transpile src/ → outDir/
	subdirs, _ := collectSubdirs(srcDir)
	moduleName := cfg.Name
	if moduleName == "" {
		moduleName = "zinc_project"
	}

	// Load dep class decls (stdlib etc.) so cross-package type checks work.
	externalClassDecls = loadDepClassDecls(cfg)
	defer func() { externalClassDecls = nil }()

	if len(subdirs) > 0 {
		if err := compileDirWithSubpackages(srcDir, outDir, moduleName, quiet, cfg.Imports); err != nil {
			return err
		}
	} else {
		if err := compileDir(srcDir, outDir, quiet, cfg.Imports); err != nil {
			return err
		}
	}

	// If there are deps, run go mod tidy again after transpilation
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

	// Generate go.mod first for module dep resolution
	if err := generateGoMod(cfg, tmpDir); err != nil {
		return err
	}
	// Pre-transpile, run `go mod download` (NOT tidy) so deps land in
	// GOMODCACHE without pruning. tidy strips unused deps, but at this
	// point no Go source exists yet so EVERY dep looks unused. download
	// pulls listed deps without pruning. Tidy still runs after transpile
	// to clean up indirect deps. Mirrors testProject's earlier fix.
	if len(cfg.Deps) > 0 {
		dl := exec.Command("go", "mod", "download", "all")
		dl.Dir = tmpDir
		dl.Stderr = os.Stderr
		if err := dl.Run(); err != nil {
			return fmt.Errorf("go mod download (pre-transpile): %w", err)
		}
	}

	// Transpile
	subdirs, _ := collectSubdirs(srcDir)
	moduleName := cfg.Name
	if moduleName == "" {
		moduleName = "zinc_project"
	}

	externalClassDecls = loadDepClassDecls(cfg)
	defer func() { externalClassDecls = nil }()

	if len(subdirs) > 0 {
		if err := compileDirWithSubpackages(srcDir, tmpDir, moduleName, true, cfg.Imports); err != nil {
			return err
		}
	} else {
		if err := compileDir(srcDir, tmpDir, true, cfg.Imports); err != nil {
			return err
		}
	}

	// Re-generate go.mod (may have new imports from transpiled code)
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
// Test
// ---------------------------------------------------------------------------

// testProject transpiles prod + test .zn files into a temp output and runs
// `go test ./...`. Because the codegen emits *_test.go naturally (and `go
// build` ignores those files), the same pipeline that powers buildProject
// works here — we just hand the output to `go test` instead of `go build`.
//
// goTestArgs are forwarded unchanged so callers can pass -run, -race, -v,
// -count, etc.
func testProject(projectDir string, goTestArgs []string) error {
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

	// Use zinc-out/ (same as build) so incremental test runs reuse cached
	// module state. If that's wrong we can switch to a temp dir later.
	outDir := filepath.Join(root, "zinc-out")
	cleanOutDir(outDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := generateGoMod(cfg, outDir); err != nil {
		return fmt.Errorf("generate go.mod: %w", err)
	}
	// Pre-transpile, run `go mod download` (NOT tidy) so every dep
	// listed in the just-written go.mod ends up in GOMODCACHE. The
	// resolver's packages.Load then works during transpile.
	//
	// Why download not tidy: tidy PRUNES unused deps. At this point
	// no .go files exist in outDir yet, so every declared dep looks
	// unused — tidy would strip the whole require list. download
	// just pulls listed deps without pruning. Tidy still runs AFTER
	// transpile (line ~793) to clean up indirect deps once .go files
	// reveal what's actually used.
	if len(cfg.Deps) > 0 {
		dl := exec.Command("go", "mod", "download", "all")
		dl.Dir = outDir
		dl.Stderr = os.Stderr
		if err := dl.Run(); err != nil {
			return fmt.Errorf("go mod download (pre-transpile): %w", err)
		}
	}

	subdirs, _ := collectSubdirs(srcDir)
	moduleName := cfg.Name
	if moduleName == "" {
		moduleName = "zinc_project"
	}

	// Optional sibling `tests/` directory — convention for projects that
	// keep test files separate from production source. The compiler
	// treats it as if it were `src/tests/` so test files can import any
	// of the project's subpackages by name. Production `zinc build` does
	// not pass the extra dir, so the binary stays test-free.
	var extraPkgs map[string]string
	testsDir := filepath.Join(root, "tests")
	if info, err := os.Stat(testsDir); err == nil && info.IsDir() {
		extraPkgs = map[string]string{"tests": testsDir}
	}

	externalClassDecls = loadDepClassDecls(cfg)
	defer func() { externalClassDecls = nil }()

	if len(subdirs) > 0 || len(extraPkgs) > 0 {
		if err := compileDirWithSubpackagesAndExtras(srcDir, outDir, moduleName, false, extraPkgs, cfg.Imports); err != nil {
			return err
		}
	} else {
		if err := compileDir(srcDir, outDir, false, cfg.Imports); err != nil {
			return err
		}
	}

	if len(cfg.Deps) > 0 {
		tidy := exec.Command("go", "mod", "tidy")
		tidy.Dir = outDir
		tidy.Stdout = os.Stdout
		tidy.Stderr = os.Stderr
		if err := tidy.Run(); err != nil {
			return fmt.Errorf("go mod tidy: %w", err)
		}
	}

	args := append([]string{"test", "./..."}, goTestArgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = outDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go test failed: %w", err)
	}
	return nil
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

	// dep is "module/path@version". Derive a short alias from the
	// last path segment so the user can `import <alias>` in Zinc.
	modulePath, _ := splitModuleVersion(dep)
	alias := modulePath
	if i := strings.LastIndex(modulePath, "/"); i >= 0 {
		alias = modulePath[i+1:]
	}

	if strings.Contains(content, modulePath) {
		return fmt.Errorf("dependency %s already exists", modulePath)
	}

	entry := fmt.Sprintf("%s = \"%s\"\n", alias, dep)
	if idx := strings.Index(content, "[deps]"); idx >= 0 {
		// Insert on the line after the [deps] header.
		newline := strings.Index(content[idx:], "\n")
		if newline < 0 {
			content += "\n" + entry
		} else {
			insertAt := idx + newline + 1
			content = content[:insertAt] + entry + content[insertAt:]
		}
	} else {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n[deps]\n" + entry
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
