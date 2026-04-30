// SKETCH — zinc-crystal project emission. Mirrors zinc-go's
// cmd/zinc/project.go in shape, generates the five Crystal-side
// project files described in zinc-crystal/PLAN.md §9 and the build
// spec hashed out against zinc-flow-crystal:
//
//   shard.yml            — from [package] + [deps]
//   Makefile             — from [build.crystal] + [build.docker.brew_libs]
//   Dockerfile           — from [build.docker]
//   scripts/build-static.sh — invariant template
//   .dockerignore        — invariant template
//
// Status: skeleton. The TOML loader, template renderers, and main
// orchestrator are wired up; many details are TODO-marked. This
// compiles standalone (no parser dependency yet) so the project-level
// file shape can be iterated on before zinc-crystal's transpiler
// proper exists.

package main

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"zinc-crystal/internal/codegen_cr"
	"zinc-go/lexer"
	"zinc-go/parser"
)

//go:embed all:templates
var templatesFS embed.FS

// ----------------------------------------------------------------------
// Config
// ----------------------------------------------------------------------

// crystalConfig is the parsed zinc.toml view, Crystal-target-specific.
//
// Source of truth is the zinc.toml file at the project root. Sections
// recognized:
//
//   [package]                — name, version, description, authors[]
//   [build.crystal]          — preview_mt (default true), workers (default 4),
//                              version (default "1.20.0"), license (default "MIT")
//   [build.docker]           — expose[], config_file, apk_static[], brew_libs[]
//   [deps]                   — alias = { path = "..." } | { github = "user/repo", version = "~> 1.0" }
//
// Only [package].name is required. Everything else has a sensible default.
type crystalConfig struct {
	// [package]
	Name        string
	Version     string
	Description string
	Authors     []string

	// [build.crystal]
	CrystalVersion string // pinned Alpine image tag, e.g. "1.20.0"
	Workers        int    // CRYSTAL_WORKERS default
	PreviewMT      bool   // -Dpreview_mt — TRUE by default per §1.4 thesis
	License        string

	// [build.docker]
	Expose     []int
	ConfigFile string   // optional config copied into runtime stage
	ApkStatic  []string // extra apk packages beyond the defaults
	BrewLibs   []string // brew lib subdirs, e.g. ["zstd","pcre"]

	// [deps]
	Deps []depEntry

	// Derived (not from TOML)
	ProjectDir    string   // basename of the project dir
	SiblingShards []string // basenames of any path: dep dirs (drives Dockerfile + build-static.sh)
	HasSpec       bool     // true iff <project>/spec/ exists, gates the Dockerfile spec stage
	HasShardLock  bool     // true iff <project>/shard.lock exists, gates a COPY in the Dockerfile

	// CtxPrefix is what gets prepended to COPY source paths in the
	// emitted Dockerfile. It mirrors what build-static.sh sets as
	// BUILD_CTX:
	//   - no sibling shards → BUILD_CTX = project dir → CtxPrefix = ""
	//     (sources are relative to project dir: `COPY shard.yml ...`)
	//   - has sibling shards → BUILD_CTX = repo root → CtxPrefix = "<project_dir>/"
	//     (sources need the project subdir: `COPY <project>/shard.yml ...`)
	// Set in loadZincToml after derived fields are resolved.
	CtxPrefix string
}

type depEntry struct {
	Alias   string // local name (used as `require "<alias>"` in zinc / Crystal)
	Path    string // path: dep — relative to project dir
	Github  string // github: dep — "user/repo"
	Version string // optional, for github deps
}

// HostLibraryPath builds the colon-separated CRYSTAL_LIBRARY_PATH from
// [build.docker.brew_libs]. Empty if no brew libs declared — Makefile
// template skips the env-var entirely in that case.
func (c *crystalConfig) HostLibraryPath() string {
	if len(c.BrewLibs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(c.BrewLibs)+1)
	for _, lib := range c.BrewLibs {
		// Per zinc-flow-crystal: $(BREW_OPT)/<lib>/lib comes first, generic lib second.
		// TODO(phase-1): Mac homebrew uses /opt/homebrew/lib not /home/linuxbrew/.linuxbrew/lib.
		// Either probe at zinc-build time or emit a $(BREW_PREFIX) Makefile var with platform fallback.
		parts = append(parts, fmt.Sprintf("/home/linuxbrew/.linuxbrew/opt/%s/lib", lib))
	}
	parts = append(parts, "/home/linuxbrew/.linuxbrew/lib")
	return strings.Join(parts, ":")
}

// defaults applied to a freshly-parsed config before validation.
func (c *crystalConfig) applyDefaults() {
	if c.Version == "" {
		c.Version = "0.1.0"
	}
	if c.CrystalVersion == "" {
		c.CrystalVersion = "1.20.0"
	}
	if c.Workers == 0 {
		c.Workers = 4
	}
	if c.License == "" {
		c.License = "MIT"
	}
	// PreviewMT defaults to true; the parser sets it explicitly only when
	// the user opts out via `preview_mt = false`. See loadZincToml.
}

// ----------------------------------------------------------------------
// TOML loader (line-based, minimal — same shape as zinc-go's)
// ----------------------------------------------------------------------

// loadZincToml parses a Crystal-target zinc.toml. SKETCH — handles the
// straightforward key=value form, line-based, no inline tables yet.
// Phase 1 will likely swap to a real TOML library
// (github.com/BurntSushi/toml) since [deps] needs inline tables for
// path/github discrimination. Hand-rolled here so the sketch has no
// external deps.
//
// TODO(phase-1): switch to BurntSushi/toml. Inline tables are awkward
// in line-based parsing — for now [deps] uses a simplified form:
//
//   [deps]
//   avro = "../crystal-avro"            # path: dep (starts with . or /)
//   yaml = "github.com/foo/yaml@~> 1.0" # github: dep
func loadZincToml(path string) (*crystalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &crystalConfig{}
	cfg.PreviewMT = true // default-on; user must opt out

	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]")
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), "\"")

		switch section {
		case "package":
			switch key {
			case "name":
				cfg.Name = val
			case "version":
				cfg.Version = val
			case "description":
				cfg.Description = val
			case "authors":
				// TODO(phase-1): authors is a TOML array; line-based parser
				// receives the raw `["a","b"]` literal. Strip brackets +
				// split for now.
				val = strings.Trim(val, "[]")
				for _, a := range strings.Split(val, ",") {
					a = strings.Trim(strings.TrimSpace(a), "\"")
					if a != "" {
						cfg.Authors = append(cfg.Authors, a)
					}
				}
			}
		case "build.crystal":
			switch key {
			case "version":
				cfg.CrystalVersion = val
			case "workers":
				// TODO(phase-1): parse int safely; fall back to 4 on error.
				fmt.Sscanf(val, "%d", &cfg.Workers)
			case "preview_mt":
				cfg.PreviewMT = (val == "true")
			case "license":
				cfg.License = val
			}
		case "build.docker":
			switch key {
			case "config_file":
				cfg.ConfigFile = val
			case "expose":
				// TODO(phase-1): `expose = [9092, 9093]` parsing.
				val = strings.Trim(val, "[]")
				for _, p := range strings.Split(val, ",") {
					var port int
					fmt.Sscanf(strings.TrimSpace(p), "%d", &port)
					if port > 0 {
						cfg.Expose = append(cfg.Expose, port)
					}
				}
			case "apk_static":
				cfg.ApkStatic = parseStringArray(val)
			case "brew_libs":
				cfg.BrewLibs = parseStringArray(val)
			}
		case "deps":
			d := depEntry{Alias: key}
			if strings.HasPrefix(val, "./") || strings.HasPrefix(val, "../") || strings.HasPrefix(val, "/") {
				d.Path = val
			} else if strings.Contains(val, "@") {
				at := strings.LastIndex(val, "@")
				d.Github = val[:at]
				d.Version = strings.TrimSpace(val[at+1:])
			} else {
				d.Github = val
			}
			cfg.Deps = append(cfg.Deps, d)
		}
	}

	cfg.applyDefaults()
	if cfg.Name == "" {
		return nil, fmt.Errorf("zinc.toml: [package].name is required")
	}

	// Compute derived fields.
	manifestDir, _ := filepath.Abs(filepath.Dir(path))
	cfg.ProjectDir = filepath.Base(manifestDir)
	for _, d := range cfg.Deps {
		if d.Path == "" {
			continue
		}
		// Sibling shard basenames are computed against the project's
		// parent (the repo root for the docker-build context).
		abs := filepath.Join(manifestDir, d.Path)
		cfg.SiblingShards = append(cfg.SiblingShards, filepath.Base(abs))
	}
	if info, err := os.Stat(filepath.Join(manifestDir, "spec")); err == nil && info.IsDir() {
		cfg.HasSpec = true
	}
	if _, err := os.Stat(filepath.Join(manifestDir, "shard.lock")); err == nil {
		cfg.HasShardLock = true
	}

	if len(cfg.SiblingShards) > 0 {
		cfg.CtxPrefix = cfg.ProjectDir + "/"
	} else {
		cfg.CtxPrefix = ""
	}

	return cfg, nil
}

// parseStringArray strips `["a","b"]` → []string{"a","b"}.
// SKETCH — fragile; replace with TOML library in Phase 1.
func parseStringArray(val string) []string {
	val = strings.Trim(val, "[]")
	out := []string{}
	for _, s := range strings.Split(val, ",") {
		s = strings.Trim(strings.TrimSpace(s), "\"")
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ----------------------------------------------------------------------
// File emitters — one per output file, all template-driven
// ----------------------------------------------------------------------

// emitProjectFiles writes shard.yml, Makefile, Dockerfile,
// scripts/build-static.sh, and .dockerignore into outDir. Each file
// is rendered from its embedded template.
//
// outDir is typically the project root (the dir containing zinc.toml).
// In `zinc build`, this is the same place the .cr files are emitted.
func emitProjectFiles(cfg *crystalConfig, outDir string) error {
	steps := []struct {
		tmpl string      // basename of template file under templates/
		dst  string      // output path relative to outDir
		mode os.FileMode // file permissions
		ctx  any         // template data
	}{
		{"shard.yml.tmpl", "shard.yml", 0o644, cfg},
		{"Makefile.tmpl", "Makefile", 0o644, makefileCtx(cfg)},
		{"Dockerfile.tmpl", "Dockerfile", 0o644, cfg},
		{"build-static.sh.tmpl", "scripts/build-static.sh", 0o755, cfg},
		{"dockerignore.tmpl", ".dockerignore", 0o644, cfg},
	}
	for _, s := range steps {
		dst := filepath.Join(outDir, s.dst)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
		}
		if err := renderTemplate(s.tmpl, s.ctx, dst, s.mode); err != nil {
			return fmt.Errorf("emit %s: %w", s.tmpl, err)
		}
	}
	return nil
}

// makefileCtx wraps the config with derived fields the Makefile
// template needs (HostLibraryPath is method-call-aware in templates,
// but we precompute as a field for clarity).
type makefileContext struct {
	*crystalConfig
	HostLibraryPath string
}

func makefileCtx(cfg *crystalConfig) makefileContext {
	return makefileContext{
		crystalConfig:   cfg,
		HostLibraryPath: cfg.HostLibraryPath(),
	}
}

func renderTemplate(name string, ctx any, dstPath string, mode os.FileMode) error {
	tmplBytes, err := templatesFS.ReadFile("templates/" + name)
	if err != nil {
		return fmt.Errorf("read template %s: %w", name, err)
	}
	tmpl, err := template.New(name).Parse(string(tmplBytes))
	if err != nil {
		return fmt.Errorf("parse template %s: %w", name, err)
	}
	f, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := tmpl.Execute(f, ctx); err != nil {
		return fmt.Errorf("exec template %s: %w", name, err)
	}
	return nil
}

// ----------------------------------------------------------------------
// Entry points — stubs for `zinc build` and `zinc init`
// ----------------------------------------------------------------------

// buildOptions controls what `zinc build` does after parsing the config.
type buildOptions struct {
	// EmitOnly skips invoking `make build-static`. Useful when iterating
	// on the project files themselves, or when the user wants to inspect
	// the emitted Dockerfile/Makefile before kicking off the (slow)
	// docker build.
	EmitOnly bool
}

// buildProject is the analog of zinc-go's buildProject. Three phases:
//
//   1. Parse zinc.toml.
//   2. Emit shard.yml, Makefile, Dockerfile, scripts/build-static.sh,
//      .dockerignore — all alongside zinc.toml.
//   3. Invoke `make build-static` to produce bin/<name> via Docker
//      (unless EmitOnly is set). This is the user-confirmed default —
//      direct host `make build` doesn't work on every workstation
//      (glibc/libgc mismatch with linuxbrew Crystal), so docker IS
//      the build path.
//
// SKETCH: phase 2.5 — .zn → .cr transpilation — is not wired yet.
// internal/codegen_cr/ is empty. Once that lands, it slots between
// emitProjectFiles and invokeBuildStatic.
func buildProject(projectDir string, opts buildOptions) error {
	tomlPath := findZincToml(projectDir)
	if tomlPath == "" {
		return fmt.Errorf("no zinc.toml found in %s or parents", projectDir)
	}
	cfg, err := loadZincToml(tomlPath)
	if err != nil {
		return err
	}
	manifestDir := filepath.Dir(tomlPath)
	if err := emitProjectFiles(cfg, manifestDir); err != nil {
		return err
	}
	if err := transpileSources(cfg, manifestDir); err != nil {
		return err
	}

	if opts.EmitOnly {
		fmt.Printf("→ emitted project files under %s\n", manifestDir)
		fmt.Printf("→ run `make build-static` to produce bin/%s\n", cfg.Name)
		return nil
	}
	return invokeBuildStatic(manifestDir, cfg.Name)
}

// invokeBuildStatic runs `make build-static` in projectDir, streaming
// stdout/stderr through to the user's terminal so the docker build
// progress is visible. Reports a clear error if make or docker is
// missing.
func invokeBuildStatic(projectDir, projectName string) error {
	if _, err := exec.LookPath("make"); err != nil {
		return fmt.Errorf("`make` not found in PATH; install GNU make to use `zinc build`")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("`docker` not found in PATH; install Docker — `zinc build` uses it for the static-binary path")
	}
	fmt.Printf("→ make build-static (in %s)\n", projectDir)
	cmd := exec.Command("make", "build-static")
	cmd.Dir = projectDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("make build-static failed: %w", err)
	}
	fmt.Printf("→ binary at %s/bin/%s\n", projectDir, projectName)
	return nil
}

// transpileSources walks <projectDir>/src/, parses each .zn file
// through zinc-go's shared parser, runs the Crystal codegen, and
// writes the result alongside the .zn as .cr.
//
// SKETCH: single-file projects only. Multi-file (subpackages) lands
// later — same flat-walk shape, but with per-package output dirs.
func transpileSources(cfg *crystalConfig, projectDir string) error {
	srcDir := filepath.Join(projectDir, "src")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read src/: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zn") {
			continue
		}
		znPath := filepath.Join(srcDir, e.Name())
		crName := strings.TrimSuffix(e.Name(), ".zn") + ".cr"
		crPath := filepath.Join(srcDir, crName)
		if err := transpileOne(znPath, crPath, cfg.Name); err != nil {
			return fmt.Errorf("transpile %s: %w", e.Name(), err)
		}
	}
	return nil
}

func transpileOne(znPath, crPath, projectName string) error {
	src, err := os.ReadFile(znPath)
	if err != nil {
		return err
	}
	l := lexer.New(string(src))
	var tokens []lexer.Token
	for {
		t := l.NextToken()
		tokens = append(tokens, t)
		if t.Type == lexer.TOKEN_EOF {
			break
		}
	}
	if len(l.Errors) > 0 {
		return fmt.Errorf("lex errors: %s", strings.Join(l.Errors, "; "))
	}
	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		return fmt.Errorf("parse errors: %s", strings.Join(p.Errors, "; "))
	}
	g := codegen_cr.New()
	g.SetClassName(projectName)
	files := g.GenerateFiles(prog)
	if errs := g.CompileErrors(); len(errs) > 0 {
		return fmt.Errorf("codegen errors: %s", strings.Join(errs, "; "))
	}
	// SKETCH: GenerateFiles returns one file today (single-target name).
	// Write its content to crPath. Multi-file lands when codegen_cr
	// grows class-per-file emit.
	if len(files) == 0 {
		return fmt.Errorf("codegen produced no output")
	}
	return os.WriteFile(crPath, []byte(files[0].Content), 0o644)
}

// runProject does `zinc build` if the binary is stale (or missing),
// then execs the binary with progArgs forwarded as argv. Stale = the
// binary's mtime is older than any .zn source under src/ or the
// zinc.toml itself. Caller's stdin/stdout/stderr are inherited so the
// binary's I/O lands in the user's terminal.
func runProject(projectDir string, progArgs []string) error {
	tomlPath := findZincToml(projectDir)
	if tomlPath == "" {
		return fmt.Errorf("no zinc.toml found in %s or parents", projectDir)
	}
	cfg, err := loadZincToml(tomlPath)
	if err != nil {
		return err
	}
	manifestDir := filepath.Dir(tomlPath)
	binPath := filepath.Join(manifestDir, "bin", cfg.Name)

	if needsRebuild(binPath, manifestDir) {
		if err := buildProject(projectDir, buildOptions{}); err != nil {
			return err
		}
	}

	cmd := exec.Command(binPath, progArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Propagate exit status — `zinc run` should mirror the binary's
		// own exit code so scripts wrapping it behave correctly.
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

// needsRebuild reports whether bin/<name> is missing or older than
// any input that affects the build:
//   - zinc.toml (project config)
//   - any .zn under src/ (source)
//   - the zinc-crystal compiler binary itself (codegen changes)
//
// The compiler-binary check is what catches `zinc-crystal updated,
// no .zn changes, but the .cr we'd emit is now different`. Without
// it, codegen changes silently no-op until the user touches a .zn.
func needsRebuild(binPath, projectDir string) bool {
	binInfo, err := os.Stat(binPath)
	if err != nil {
		return true // missing
	}
	binMtime := binInfo.ModTime()

	if info, err := os.Stat(filepath.Join(projectDir, "zinc.toml")); err == nil {
		if info.ModTime().After(binMtime) {
			return true
		}
	}
	if zincBin, err := os.Executable(); err == nil {
		if info, err := os.Stat(zincBin); err == nil && info.ModTime().After(binMtime) {
			return true
		}
	}
	srcDir := filepath.Join(projectDir, "src")
	stale := false
	_ = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".zn") && info.ModTime().After(binMtime) {
			stale = true
		}
		return nil
	})
	return stale
}

// initProject scaffolds a new zinc-crystal project at <name>/. Writes
// a minimal zinc.toml + src/<name>.zn hello-world stub. The caller can
// run `zinc build` immediately after to produce a working binary.
func initProject(name string) error {
	if name == "" {
		return fmt.Errorf("project name required: `zinc init <name>`")
	}
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %q already exists", name)
	}
	if err := os.MkdirAll(filepath.Join(name, "src"), 0o755); err != nil {
		return err
	}
	tomlPath := filepath.Join(name, "zinc.toml")
	tomlContent := fmt.Sprintf(`[package]
name = %q
version = "0.1.0"
description = "A zinc-crystal project"

[build.crystal]
version = "1.20.0"
workers = 4
license = "MIT"
`, name)
	if err := os.WriteFile(tomlPath, []byte(tomlContent), 0o644); err != nil {
		return err
	}
	zincPath := filepath.Join(name, "src", name+".zn")
	zincContent := `void main() {
    print("Hello from zinc-crystal!")
}
`
	if err := os.WriteFile(zincPath, []byte(zincContent), 0o644); err != nil {
		return err
	}
	gitignorePath := filepath.Join(name, ".gitignore")
	gitignoreContent := "bin/\nlib/\n.crystal/\n.shards/\nshard.lock\n"
	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0o644); err != nil {
		return err
	}
	fmt.Printf("→ created %s/\n", name)
	fmt.Printf("→ next: cd %s && zinc build\n", name)
	return nil
}

func findZincToml(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
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
