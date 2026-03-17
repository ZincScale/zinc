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
	"zinc/internal/config"
	"zinc/internal/errs"
	"zinc/internal/lexer"
	"zinc/internal/parser"
	"zinc/internal/project"
	"zinc/internal/typechecker"
)

// version is set by goreleaser via ldflags at build time.
var version = "0.10.0"

const usage = `Zinc — convention over configuration for native apps.

Usage:
  zinc <file.zn> [flags]   Transpile a single file
  zinc build [dir]         Transpile + compile (native AOT binary)
  zinc run [dir]           Transpile + run
  zinc init [name]         Initialize a new Zinc project (creates zinc.toml + main.zn)
  zinc repl                Launch interactive REPL

Flags:
  -o <file>       Output file (default: <input>.cs)
  --release       Strip debug symbols for production (smaller binary, no line numbers)
  --verbose       Print tokens and AST summary after transpiling
  --run           Transpile and immediately run
  --watch         Watch file for changes and re-transpile automatically
  --version       Print version and exit
`

func main() {
	// Manual arg parsing (flag pkg stops at first non-flag)
	var inFile, outFile string
	verbose := false
	runAfter := false
	watchMode := false
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
			dir := "."
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				dir = args[i+1]
				i++
			}
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
		case a == "--watch" || a == "-w":
			watchMode = true
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

	if watchMode {
		runWatch(inFile, outFile)
		return
	}

	src, err := os.ReadFile(inFile)
	if err != nil {
		errs.FileError(inFile, err.Error())
		os.Exit(1)
	}

	// Lexer
	l := lexer.New(string(src))
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		errs.FileErrors(inFile, l.Errors)
		os.Exit(1)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] %d tokens\n", len(tokens))
	}

	// Parser
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		errs.FileErrors(inFile, p.Errors)
		os.Exit(1)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] %d top-level declarations\n", len(prog.Decls))
	}

	// Type checking
	if tcErrs := typechecker.Check(prog); len(tcErrs) > 0 {
		strs := make([]string, len(tcErrs))
		for i, e := range tcErrs {
			strs[i] = e.String()
		}
		errs.TypeErrors(inFile, strs)
		os.Exit(1)
	}

	// Code generation
	gen := codegen.New()
	gen.SetSourceFile(inFile)
	goSrc := gen.Generate(prog)

	// Determine output path
	if outFile == "" {
		base := filepath.Base(inFile)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		outFile = base + ".go"
	}

	// Write output
	if err := os.WriteFile(outFile, []byte(goSrc), 0644); err != nil {
		errs.Errorf("writing %s: %v", outFile, err)
		os.Exit(1)
	}

	// Run gofmt
	cmd := exec.Command("gofmt", "-w", outFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		errs.Warningf("gofmt: %v\n%s", err, string(out))
	}

	fmt.Printf("transpiled %s → %s\n", inFile, outFile)

	if runAfter {
		run := exec.Command("go", "run", outFile)
		run.Stdout = os.Stdout
		run.Stderr = os.Stderr
		if err := run.Run(); err != nil {
			os.Exit(1)
		}
	}
}
