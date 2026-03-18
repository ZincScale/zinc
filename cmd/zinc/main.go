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

	"zinc/internal/codegen"
	"zinc/internal/codegen_python"
	"zinc/internal/config"
	"zinc/internal/errs"
	"zinc/internal/lexer"
	"zinc/internal/nuget"
	"zinc/internal/parser"
	"zinc/internal/project"
	"zinc/internal/typechecker"

	// v2 type checker is used in transpileV2File
)

// version is set by goreleaser via ldflags at build time.
var version = "0.13.0"

const usage = `Zinc — typed Python with explicit blocks.

Usage:
  zinc run <file.zn>           Transpile to Python and run
  zinc transpile <file.zn>     Output .py file
  zinc <file.zn>               Transpile a single file (outputs .py)

Legacy (v1 — C#/Go backends):
  zinc build [dir]         Transpile + compile (native AOT binary)
  zinc test [dir]          Discover and run test_* functions

Flags:
  -o <file>              Output file (default: <input>.py)
  --optimize <backend>   Collection dispatch backend: polars, numpy (default: pure Python)
  --verbose              Print tokens and AST summary after transpiling
  --version              Print version and exit
`

func main() {
	// Manual arg parsing (flag pkg stops at first non-flag)
	var inFile, outFile string
	verbose := false
	runAfter := false
	args := os.Args[1:]

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--version" || a == "-V":
			fmt.Printf("zinc version %s\n", version)
			return
		case a == "init":
			name := ""
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				name = args[i+1]
				i++
			}
			if name == "" {
				dir, err := os.Getwd()
				if err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}
				name = filepath.Base(dir)
			}
			if _, err := os.Stat("zinc.toml"); err == nil {
				errs.Error("zinc.toml already exists")
				os.Exit(1)
			}
			cfg := config.DefaultConfig(name)
			if err := os.WriteFile("zinc.toml", []byte(config.Generate(cfg)), 0644); err != nil {
				errs.Errorf("writing zinc.toml: %v", err)
				os.Exit(1)
			}
			mainZn := "main() {\n    print(\"Hello from Zinc!\")\n}\n"
			if err := os.WriteFile("main.zn", []byte(mainZn), 0644); err != nil {
				errs.Errorf("writing main.zn: %v", err)
				os.Exit(1)
			}
			fmt.Printf("initialized project %q\n", name)
			fmt.Println("  created zinc.toml")
			fmt.Println("  created main.zn")
			return
		case a == "repl":
			runREPL()
			return
		case a == "build":
			dir := "."
			release := false
			for j := i + 1; j < len(args); j++ {
				if args[j] == "--release" {
					release = true
				} else if !strings.HasPrefix(args[j], "-") && dir == "." {
					dir = args[j]
				}
			}
			cfg, err := config.Load(dir)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			if cfg != nil && cfg.Target == "csharp" {
				cfg.Release = release
				if err := project.BuildCSharp(dir, cfg); err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}
			} else {
				if err := project.Build(dir); err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}
			}
			return
		case a == "run":
			target := "."
			optimize := ""
			for j := i + 1; j < len(args); j++ {
				if args[j] == "--optimize" && j+1 < len(args) {
					optimize = args[j+1]
					j++
				} else if !strings.HasPrefix(args[j], "-") && target == "." {
					target = args[j]
				}
			}
			if target != "." {
				i++ // skip target in main loop
			}
			// If target is a .zn file, use v2 pipeline (transpile → run with python)
			if strings.HasSuffix(target, ".zn") {
				// Transpile to temp file, run, clean up
				tmpFile := filepath.Join(os.TempDir(), "zinc_run_"+filepath.Base(strings.TrimSuffix(target, ".zn"))+".py")
				pyFile, sourceMap, err := transpileV2File(target, tmpFile, false, optimize)
				if err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}
				defer os.Remove(pyFile)
				// Collect remaining args to pass to the script
				var scriptArgs []string
				for j := i + 1; j < len(args); j++ {
					if args[j] == "--" {
						scriptArgs = args[j+1:]
						break
					}
				}
				runArgs := append([]string{pyFile}, scriptArgs...)
				cmd := exec.Command("python3", runArgs...)
				cmd.Stdout = os.Stdout
				cmd.Stdin = os.Stdin
				// Capture stderr to rewrite tracebacks
				var stderrBuf strings.Builder
				cmd.Stderr = &stderrBuf
				if err := cmd.Run(); err != nil {
					// Rewrite traceback: replace .py file/line with .zn file/line
					stderr := stderrBuf.String()
					stderr = rewriteTraceback(stderr, pyFile, target, sourceMap)
					fmt.Fprint(os.Stderr, stderr)
					if exitErr, ok := err.(*exec.ExitError); ok {
						os.Exit(exitErr.ExitCode())
					}
					os.Exit(1)
				}
				return
			}
			// Legacy: directory-based project mode
			dir := target
			cfg, err := config.Load(dir)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			if cfg != nil && cfg.Target == "csharp" {
				if err := project.RunCSharp(dir, cfg); err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}
			} else {
				if err := project.Run(dir); err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}
			}
			return
		case a == "transpile":
			target := ""
			localOut := ""
			localVerbose := false
			localOptimize := ""
			for j := i + 1; j < len(args); j++ {
				if args[j] == "-o" && j+1 < len(args) {
					localOut = args[j+1]
					j++
				} else if args[j] == "--optimize" && j+1 < len(args) {
					localOptimize = args[j+1]
					j++
				} else if args[j] == "--verbose" || args[j] == "-v" {
					localVerbose = true
				} else if !strings.HasPrefix(args[j], "-") && target == "" {
					target = args[j]
				}
			}
			if target == "" {
				errs.Error("usage: zinc transpile <file.zn>")
				os.Exit(1)
			}
			pyFile, _, err := transpileV2File(target, localOut, localVerbose, localOptimize)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			fmt.Printf("transpiled %s → %s\n", target, pyFile)
			return
		case a == "test":
			dir := "."
			verboseTest := false
			filterFn := ""
			for j := i + 1; j < len(args); j++ {
				if args[j] == "-v" || args[j] == "--verbose" {
					verboseTest = true
				} else if (args[j] == "-f" || args[j] == "--filter") && j+1 < len(args) {
					filterFn = args[j+1]
					j++
				} else if !strings.HasPrefix(args[j], "-") && dir == "." {
					dir = args[j]
				}
			}
			cfg, err := config.Load(dir)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			if cfg == nil {
				cfg = config.DefaultConfig("zinc-test")
			}
			if err := project.TestCSharp(dir, cfg, verboseTest, filterFn); err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			return
		case a == "add":
			// zinc add Serilog [AWSSDK.SQS ...] [--version X.Y.Z] [--source name]
			var packages []string
			specVersion := ""
			sourceName := ""
			for j := i + 1; j < len(args); j++ {
				if args[j] == "--version" && j+1 < len(args) {
					specVersion = args[j+1]
					j++
				} else if args[j] == "--source" && j+1 < len(args) {
					sourceName = args[j+1]
					j++
				} else if !strings.HasPrefix(args[j], "-") {
					packages = append(packages, args[j])
				}
			}
			if len(packages) == 0 {
				errs.Error("usage: zinc add <package> [package...] [--version X.Y.Z] [--source name]")
				os.Exit(1)
			}
			dir := "."
			cfg, err := config.Load(dir)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			if cfg == nil {
				errs.Error("no zinc.toml found — run 'zinc init' first")
				os.Exit(1)
			}
			for _, pkg := range packages {
				ver := specVersion
				if ver == "" {
					var resolved string
					var resolveErr error
					sourceURL, authToken, authType := cfg.GetNuGetSource(sourceName)
					if sourceURL != "" {
						resolved, resolveErr = nuget.ResolveLatestFrom(sourceURL, pkg, authToken, authType)
					} else {
						resolved, resolveErr = nuget.ResolveLatest(pkg)
					}
					if resolveErr != nil {
						errs.Errorf("%s: %v", pkg, resolveErr)
						os.Exit(1)
					}
					ver = resolved
				}
				cfg.AddDependency(pkg, ver)
				fmt.Printf("  added %s %s\n", pkg, ver)
			}
			if err := cfg.SaveToFile(dir); err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			return
		case a == "remove":
			if i+1 >= len(args) {
				errs.Error("usage: zinc remove <package>")
				os.Exit(1)
			}
			pkg := args[i+1]
			dir := "."
			cfg, err := config.Load(dir)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			if cfg == nil {
				errs.Error("no zinc.toml found")
				os.Exit(1)
			}
			if cfg.RemoveDependency(pkg) {
				if err := cfg.SaveToFile(dir); err != nil {
					errs.Error(err.Error())
					os.Exit(1)
				}
				fmt.Printf("  removed %s\n", pkg)
			} else {
				fmt.Printf("  %s not found in dependencies\n", pkg)
			}
			return
		case a == "deps":
			dir := "."
			cfg, err := config.Load(dir)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			if cfg == nil || len(cfg.Dependencies) == 0 {
				fmt.Println("no dependencies")
				return
			}
			fmt.Println("Dependencies:")
			for _, dep := range cfg.Dependencies {
				fmt.Printf("  %-30s %s\n", dep.Name, dep.Version)
			}
			return
		case a == "-o" || a == "--o":
			if i+1 < len(args) {
				outFile = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-o="):
			outFile = strings.TrimPrefix(a, "-o=")
		case a == "--verbose" || a == "-v":
			verbose = true
		case a == "--run" || a == "-r":
			runAfter = true
		case !strings.HasPrefix(a, "-"):
			if inFile == "" {
				inFile = a
			}
		}
	}

	if inFile == "" {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	// Default: v2 transpile single file to Python
	pyFile, _, err := transpileV2File(inFile, outFile, verbose)
	if err != nil {
		errs.Error(err.Error())
		os.Exit(1)
	}
	fmt.Printf("transpiled %s → %s\n", inFile, pyFile)

	if runAfter {
		cmd := exec.Command("python3", pyFile)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}
	}
}

// transpileV2File transpiles a .zn file to .py using the v2 pipeline.
func transpileV2File(inFile, outFile string, verbose bool, opts ...string) (string, map[int]int, error) {
	// Parse optimize option from opts
	optimize := ""
	for _, o := range opts {
		optimize = o
	}
	src, err := os.ReadFile(inFile)
	if err != nil {
		return "", nil, fmt.Errorf("reading %s: %w", inFile, err)
	}

	// Lexer
	l := lexer.New(string(src))
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		return "", nil, fmt.Errorf("lexer errors in %s:\n%s", inFile, strings.Join(l.Errors, "\n"))
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] %d tokens\n", len(tokens))
	}

	// Parser (v2)
	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		return "", nil, fmt.Errorf("parse errors in %s:\n%s", inFile, strings.Join(p.Errors, "\n"))
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] %d declarations, %d top-level statements\n",
			len(prog.Decls), len(prog.Stmts))
	}

	// Type checking (v2)
	if tcErrors := typechecker.CheckV2(prog); len(tcErrors) > 0 {
		var msgs []string
		for _, e := range tcErrors {
			msgs = append(msgs, e.String())
		}
		return "", nil, fmt.Errorf("type errors in %s:\n%s", inFile, strings.Join(msgs, "\n"))
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] type check passed\n")
	}

	// Code generation (Python)
	gen := codegen_python.New()
	gen.OptimizeBackend = optimize
	gen.SourceFile = inFile
	pySrc := gen.GenerateV2(prog)

	// Determine output path
	if outFile == "" {
		base := filepath.Base(inFile)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		outFile = base + ".py"
	}

	// Write output
	if err := os.WriteFile(outFile, []byte(pySrc), 0644); err != nil {
		return "", nil, fmt.Errorf("writing %s: %w", outFile, err)
	}

	return outFile, gen.GetSourceMap(), nil
}

// rewriteTraceback replaces .py file references with .zn file references in Python tracebacks.
func rewriteTraceback(stderr, pyFile, znFile string, sourceMap map[int]int) string {
	var result strings.Builder
	for _, line := range strings.Split(stderr, "\n") {
		// Python traceback lines look like:
		//   File "/tmp/zinc_run_foo.py", line 15, in <module>
		if strings.Contains(line, pyFile) && strings.Contains(line, ", line ") {
			// Extract the Python line number after ", line "
			idx := strings.Index(line, ", line ")
			if idx >= 0 {
				after := line[idx+7:] // skip ", line "
				numStr := ""
				for _, ch := range after {
					if ch >= '0' && ch <= '9' {
						numStr += string(ch)
					} else {
						break
					}
				}
				if numStr != "" {
					var pyLineNum int
					fmt.Sscanf(numStr, "%d", &pyLineNum)
					znLine := findClosestZnLine(pyLineNum, sourceMap)
					if znLine > 0 {
						line = strings.Replace(line, pyFile, znFile, 1)
						line = strings.Replace(line, ", line "+numStr, fmt.Sprintf(", line %d", znLine), 1)
					} else {
						// No source map match — still replace filename
						line = strings.Replace(line, pyFile, znFile, 1)
					}
				}
			}
		}
		result.WriteString(line)
		result.WriteString("\n")
	}
	return strings.TrimRight(result.String(), "\n") + "\n"
}

// findClosestZnLine finds the .zn line for a given .py line, searching backwards.
func findClosestZnLine(pyLine int, sourceMap map[int]int) int {
	// Exact match
	if zn, ok := sourceMap[pyLine]; ok {
		return zn
	}
	// Search backwards for the closest mapped line
	for offset := 1; offset < 20; offset++ {
		if zn, ok := sourceMap[pyLine-offset]; ok {
			return zn
		}
	}
	return 0
}

// Keep v1 imports referenced so they compile (used by legacy commands).
var (
	_ = codegen.New
	_ = typechecker.Check
)
