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

// Command zincpy is an isolated spike: it compiles a (valid CPython) Python
// file through the zinc-go Python front-end and the existing typechecker +
// Go codegen, then either emits the generated Go (`--emit`) or builds and
// runs it. It does not touch the main `zinc` CLI.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	codegen "zinc-go/internal/codegen_go"
	"zinc-go/internal/parser"
	"zinc-go/internal/pyfront"
	"zinc-go/internal/typechecker"
)

func main() {
	args := os.Args[1:]
	emitOnly := false
	var path string
	for _, a := range args {
		switch a {
		case "--emit", "-e":
			emitOnly = true
		default:
			path = a
		}
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "usage: zincpy [--emit] <file.py>")
		os.Exit(2)
	}

	files, meta, err := compile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if emitOnly {
		for _, f := range files {
			fmt.Printf("// === %s ===\n%s\n", f.Name, f.Content)
		}
		fmt.Printf("// === %s ===\n%s\n", pyfront.RuntimeFileName, pyfront.RuntimeGo)
		if meta.GlobalDecls != "" {
			fmt.Printf("// === %s ===\n%s\n", pyfront.GlobalDeclsFileName, meta.GlobalDecls)
		}
		if len(meta.FFIModules) > 0 {
			cf, ld, _ := pythonCGOFlags()
			fmt.Printf("// === %s ===\n%s\n", pyfront.FFIRuntimeFileName, pyfront.FFIRuntime(cf, ld))
		}
		return
	}
	if err := runGo(files, meta); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// compile runs the full Python → Go pipeline for one file.
func compile(path string) ([]codegen.OutputFile, *pyfront.Meta, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}

	prog, meta, perrs := pyfront.Parse(string(src))
	if len(perrs) > 0 {
		return nil, nil, fmt.Errorf("parse %s:\n  %s", path, strings.Join(perrs, "\n  "))
	}

	bp, tcErrs := typecheckSingle(prog)
	if len(tcErrs) > 0 {
		var b strings.Builder
		for _, e := range tcErrs {
			fmt.Fprintf(&b, "  %s\n", e.String())
		}
		return nil, nil, fmt.Errorf("typecheck %s:\n%s", path, b.String())
	}

	className := classNameFor(path)
	gen := codegen.New()
	gen.SetBoundProgram(bp)
	gen.SetPythonMode(true) // user methods (.upper/.keys/...) must not hit Zinc's builtin dispatch
	out := gen.GenerateFiles(prog, className)
	for _, w := range gen.CompileWarnings() {
		fmt.Fprintln(os.Stderr, w)
	}
	if errs := gen.CompileErrors(); len(errs) > 0 {
		return nil, nil, fmt.Errorf("codegen %s:\n  %s", path, strings.Join(errs, "\n  "))
	}
	return out, meta, nil
}

// typecheckSingle mirrors the single-file slice of the main CLI's
// runTypecheck: collect signatures, build a bind context, bind, then
// CheckV2. No imports, no cross-package wiring (spike scope).
func typecheckSingle(prog *parser.Program) (*typechecker.BoundProgram, []typechecker.V2Error) {
	sigs := typechecker.CollectSignatures(prog)
	ctx := typechecker.CollectBindContext(prog)
	ctx.Sigs = &sigs
	if ctx.ImportAliases == nil {
		ctx.ImportAliases = map[string]bool{}
	}

	bp, errs := typechecker.Bind(prog, ctx)
	bp.Sigs = &sigs

	checkErrs, nodeTypes := typechecker.CheckV2WithContextAndNodes(prog, &sigs, nil, nil)
	bp.NodeTypes = nodeTypes
	errs = append(errs, checkErrs...)
	return bp, errs
}

func classNameFor(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if base == "" {
		return "Main"
	}
	return strings.ToUpper(base[:1]) + base[1:]
}

// runGo builds and runs the generated files, streaming output through.
func runGo(files []codegen.OutputFile, meta *pyfront.Meta) error {
	out, err := runOutput(files, meta)
	fmt.Print(out)
	return err
}

// runOutput writes the generated files into a throwaway module, `go run`s
// it, and returns its combined stdout/stderr. When the program FFIs into
// CPython, the cgo runtime (which links libpython) is added too.
func runOutput(files []codegen.OutputFile, meta *pyfront.Meta) (string, error) {
	dir, err := os.MkdirTemp("", "zincpy-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module zincpyout\n\ngo 1.21\n"), 0o644); err != nil {
		return "", err
	}
	for _, f := range files {
		name := f.Name
		if !strings.HasSuffix(name, ".go") {
			name += ".go"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(f.Content), 0o644); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, pyfront.RuntimeFileName), []byte(pyfront.RuntimeGo), 0o644); err != nil {
		return "", err
	}
	if meta != nil && meta.GlobalDecls != "" {
		if err := os.WriteFile(filepath.Join(dir, pyfront.GlobalDeclsFileName), []byte(meta.GlobalDecls), 0o644); err != nil {
			return "", err
		}
	}
	if meta != nil && len(meta.FFIModules) > 0 {
		cf, ld, ferr := pythonCGOFlags()
		if ferr != nil {
			return "", ferr
		}
		if err := os.WriteFile(filepath.Join(dir, pyfront.FFIRuntimeFileName), []byte(pyfront.FFIRuntime(cf, ld)), 0o644); err != nil {
			return "", err
		}
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// pythonConfigTool returns the python*-config script that belongs to the SAME
// interpreter as the `python3` on PATH. The embedded libpython must match the
// `python3` we differential-test against, byte for byte — linking a different
// version (e.g. system 3.9 while `python3` is a 3.14 venv) makes runtime
// messages diverge (math.sqrt(-1) phrasing changed across versions). A bare
// `python3-config` resolves to the system Python, NOT the venv's, so we instead
// ask `python3` for its base prefix + version and locate
// `{base_prefix}/bin/python{X.Y}-config`. Falls back to `python3-config` when
// that sibling is absent (hosts where only the system Python ships a config).
func pythonConfigTool() string {
	out, err := exec.Command("python3", "-c",
		"import sys; print(sys.base_prefix); print('%d.%d' % sys.version_info[:2])").Output()
	if err != nil {
		return "python3-config"
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return "python3-config"
	}
	cfg := filepath.Join(strings.TrimSpace(lines[0]), "bin", "python"+strings.TrimSpace(lines[1])+"-config")
	if _, err := os.Stat(cfg); err != nil {
		return "python3-config"
	}
	return cfg
}

// pythonCGOFlags derives the cgo compile/link flags for embedding the host
// CPython from python*-config: --includes for the <Python.h> search path, and
// --embed --ldflags for libpython (the --embed variant adds -lpython, which
// the plain extension-module form omits on 3.8+). The config tool is resolved
// to match the `python3` on PATH (see pythonConfigTool), so the same compiler
// targets whatever interpreter we run against — 3.9, a 3.14 venv, etc. Requires
// that interpreter's dev files (python*-config + Python.h).
func pythonCGOFlags() (cflags, ldflags string, err error) {
	cfg := pythonConfigTool()
	inc, e1 := exec.Command(cfg, "--includes").Output()
	if e1 != nil {
		return "", "", fmt.Errorf("%s --includes failed (install python3-devel?): %w", cfg, e1)
	}
	ld, e2 := exec.Command(cfg, "--embed", "--ldflags").Output()
	if e2 != nil {
		return "", "", fmt.Errorf("%s --embed --ldflags failed: %w", cfg, e2)
	}
	cflags = strings.TrimSpace(string(inc))
	ldflags = strings.TrimSpace(string(ld))
	if ldflags == "" {
		return "", "", fmt.Errorf("%s --embed --ldflags was empty; need a python3 built with --enable-shared", cfg)
	}
	// Bake the libpython directory into the binary as an rpath so it loads at
	// runtime without LD_LIBRARY_PATH. --embed --ldflags emits the dir as a
	// `-L<dir>` link-search flag; the same dir is where libpython lives, and a
	// non-standard install (a uv/pyenv venv's lib dir) is otherwise invisible to
	// the dynamic loader. cgo's LDFLAGS allowlist permits -Wl,-rpath.
	if libDir := firstLibDir(ldflags); libDir != "" {
		ldflags += " -Wl,-rpath," + libDir
	}
	return cflags, ldflags, nil
}

// firstLibDir returns the path from the first `-L<dir>` token in a linker flag
// string, or "" if there is none.
func firstLibDir(ldflags string) string {
	for _, tok := range strings.Fields(ldflags) {
		if strings.HasPrefix(tok, "-L") && len(tok) > 2 {
			return tok[2:]
		}
	}
	return ""
}
