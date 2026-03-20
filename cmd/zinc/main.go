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
  zinc build <file.zn>           Transpile to Java and compile with javac
  zinc run <file.zn>             Transpile, compile, and run
  zinc fmt <file.zn>             Format Zinc source code
  zinc repl                      Interactive Zinc REPL

Flags:
  -o <dir>               Output directory (default: zinc-out/)
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
			for j := i + 1; j < len(args); j++ {
				if args[j] == "-o" && j+1 < len(args) {
					outDir = args[j+1]
					j++
				} else if args[j] == "--verbose" || args[j] == "-v" {
					verbose = true
				} else if !strings.HasPrefix(args[j], "-") && target == "" {
					target = args[j]
				}
			}
			if target == "" {
				fmt.Fprintln(os.Stderr, "usage: zinc build <file.zn> [-o outdir]")
				os.Exit(1)
			}
			javaFiles, err := transpileToJava(target, outDir, verbose)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			if err := compileJava(javaFiles, outDir); err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			fmt.Printf("build complete: %s → %s/\n", target, outDir)
			return
		case a == "run":
			target := ""
			verbose := false
			for j := i + 1; j < len(args); j++ {
				if args[j] == "--verbose" || args[j] == "-v" {
					verbose = true
				} else if !strings.HasPrefix(args[j], "-") && target == "" {
					target = args[j]
				}
			}
			if target == "" || !strings.HasSuffix(target, ".zn") {
				fmt.Fprintln(os.Stderr, "usage: zinc run <file.zn> [-- args...]")
				os.Exit(1)
			}

			outDir := filepath.Join(os.TempDir(), "zinc-run-"+filepath.Base(strings.TrimSuffix(target, ".zn")))
			javaFiles, err := transpileToJava(target, outDir, verbose)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			if err := compileJava(javaFiles, outDir); err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}

			// Derive class name from filename
			className := classNameFromFile(target)

			// Collect script args (after --)
			var scriptArgs []string
			for j := i + 1; j < len(args); j++ {
				if args[j] == "--" {
					scriptArgs = args[j+1:]
					break
				}
			}

			if err := runJava(outDir, className, scriptArgs); err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					os.Exit(exitErr.ExitCode())
				}
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

// transpileToJava transpiles a .zn file to .java files in outDir.
func transpileToJava(inFile, outDir string, verbose bool) ([]string, error) {
	prog, err := parseAndCheck(inFile, verbose)
	if err != nil {
		return nil, err
	}

	className := classNameFromFile(inFile)
	gen := codegen_java.New()
	javaSrc := gen.Generate(prog, className)

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	mainFile := filepath.Join(outDir, className+".java")
	if err := os.WriteFile(mainFile, []byte(javaSrc), 0644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", mainFile, err)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] wrote %s\n", mainFile)
	}

	return []string{mainFile}, nil
}

// compileJava runs javac on the generated .java files.
func compileJava(javaFiles []string, outDir string) error {
	javac, err := exec.LookPath("javac")
	if err != nil {
		return fmt.Errorf("javac not found — install JDK 25+")
	}

	args := []string{"-d", outDir}
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

	args := []string{"-cp", classDir, className}
	args = append(args, scriptArgs...)

	cmd := exec.Command(java, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
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
