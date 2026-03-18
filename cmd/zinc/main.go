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

	"zinc/internal/codegen_python"
	"zinc/internal/errs"
	"zinc/internal/lexer"
	"zinc/internal/parser"
	"zinc/internal/typechecker"
)

var version = "0.2.0"

const usage = `Zinc — typed Python with explicit blocks.

Usage:
  zinc run <file.zn>           Transpile to Python and run (free-threaded)
  zinc transpile <file.zn>     Output .py file
  zinc fmt <file.zn>           Format Zinc source code
  zinc pack <file.zn|dir>      Package for deployment (pyinstaller, nuitka, docker, k8s)
  zinc repl                    Interactive Zinc REPL
  zinc <file.zn>               Transpile a single file (outputs .py)

Flags:
  -o <file>              Output file (default: <input>.py)
  --verbose              Print tokens and AST summary after transpiling
  --version              Print version and exit
`

func main() {
	var inFile, outFile string
	verbose := false
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
		case a == "pack":
			target := ""
			format := ""
			for j := i + 1; j < len(args); j++ {
				if args[j] == "--format" && j+1 < len(args) {
					format = args[j+1]
					j++
				} else if !strings.HasPrefix(args[j], "-") && target == "" {
					target = args[j]
				}
			}
			if target == "" {
				fmt.Fprintln(os.Stderr, "usage: zinc pack <file.zn|dir> [--format pyinstaller|nuitka|docker|k8s]")
				os.Exit(1)
			}
			runPack(target, format)
			return
		case a == "run":
			target := ""
			for j := i + 1; j < len(args); j++ {
				if !strings.HasPrefix(args[j], "-") && target == "" {
					target = args[j]
				}
			}
			if target == "" || !strings.HasSuffix(target, ".zn") {
				fmt.Fprintln(os.Stderr, "usage: zinc run <file.zn> [-- args...]")
				os.Exit(1)
			}
			// Transpile to temp file, run with free-threaded Python, clean up
			tmpFile := filepath.Join(os.TempDir(), "zinc_run_"+filepath.Base(strings.TrimSuffix(target, ".zn"))+".py")
			pyFile, sourceMap, err := transpileV2File(target, tmpFile, false)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			defer os.Remove(pyFile)

			// Collect script args (after --)
			var scriptArgs []string
			for j := i + 1; j < len(args); j++ {
				if args[j] == "--" {
					scriptArgs = args[j+1:]
					break
				}
			}
			runArgs := append([]string{pyFile}, scriptArgs...)

			// Try free-threaded Python first
			pythonBin := findPython()
			cmd := exec.Command(pythonBin, runArgs...)
			if !strings.HasSuffix(pythonBin, "t") {
				cmd.Env = append(os.Environ(), "PYTHON_GIL=0")
			}
			cmd.Stdout = os.Stdout
			cmd.Stdin = os.Stdin
			var stderrBuf strings.Builder
			cmd.Stderr = &stderrBuf
			runErr := cmd.Run()

			// If GIL=0 not supported, retry without
			if runErr != nil && strings.Contains(stderrBuf.String(), "Disabling the GIL is not supported") {
				stderrBuf.Reset()
				cmd = exec.Command(pythonBin, runArgs...)
				cmd.Stdout = os.Stdout
				cmd.Stdin = os.Stdin
				cmd.Stderr = &stderrBuf
				runErr = cmd.Run()
			}

			if runErr != nil {
				stderr := stderrBuf.String()
				stderr = rewriteTraceback(stderr, pyFile, target, sourceMap)
				fmt.Fprint(os.Stderr, stderr)
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					os.Exit(exitErr.ExitCode())
				}
				os.Exit(1)
			}
			return
		case a == "transpile":
			target := ""
			localOut := ""
			localVerbose := false
			for j := i + 1; j < len(args); j++ {
				if args[j] == "-o" && j+1 < len(args) {
					localOut = args[j+1]
					j++
				} else if args[j] == "--verbose" || args[j] == "-v" {
					localVerbose = true
				} else if !strings.HasPrefix(args[j], "-") && target == "" {
					target = args[j]
				}
			}
			if target == "" {
				fmt.Fprintln(os.Stderr, "usage: zinc transpile <file.zn> [-o output.py]")
				os.Exit(1)
			}
			pyFile, _, err := transpileV2File(target, localOut, localVerbose)
			if err != nil {
				errs.Error(err.Error())
				os.Exit(1)
			}
			fmt.Printf("transpiled %s → %s\n", target, pyFile)
			return
		case a == "-o" || a == "--o":
			if i+1 < len(args) {
				outFile = args[i+1]
				i++
			}
		case a == "--verbose" || a == "-v":
			verbose = true
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

	// Default: transpile single file to Python
	pyFile, _, err := transpileV2File(inFile, outFile, verbose)
	if err != nil {
		errs.Error(err.Error())
		os.Exit(1)
	}
	fmt.Printf("transpiled %s → %s\n", inFile, pyFile)
}

// findPython finds the best Python interpreter, preferring free-threaded builds.
func findPython() string {
	for _, bin := range []string{"python3.14t", "python3.13t", "python3t"} {
		if path, err := exec.LookPath(bin); err == nil {
			return path
		}
	}
	for _, path := range []string{
		os.Getenv("HOME") + "/python3.14t/bin/python3.14t",
		os.Getenv("HOME") + "/python3.14t/bin/python3",
		"/usr/local/bin/python3t",
	} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "python3"
}

// transpileV2File transpiles a .zn file to .py using the v2 pipeline.
func transpileV2File(inFile, outFile string, verbose bool) (string, map[int]int, error) {
	src, err := os.ReadFile(inFile)
	if err != nil {
		return "", nil, fmt.Errorf("reading %s: %w", inFile, err)
	}

	l := lexer.New(string(src))
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		return "", nil, fmt.Errorf("lexer errors in %s:\n%s", inFile, strings.Join(l.Errors, "\n"))
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] %d tokens\n", len(tokens))
	}

	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		return "", nil, fmt.Errorf("parse errors in %s:\n%s", inFile, strings.Join(p.Errors, "\n"))
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
		return "", nil, fmt.Errorf("type errors in %s:\n%s", inFile, strings.Join(msgs, "\n"))
	}

	gilWarnings := typechecker.CheckGILDependencies(prog)
	for _, w := range gilWarnings {
		fmt.Fprintf(os.Stderr, "%s\n", w)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] type check passed\n")
	}

	gen := codegen_python.New()
	gen.SourceFile = inFile
	pySrc := gen.GenerateV2(prog)

	if outFile == "" {
		base := filepath.Base(inFile)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		outFile = base + ".py"
	}

	if err := os.WriteFile(outFile, []byte(pySrc), 0644); err != nil {
		return "", nil, fmt.Errorf("writing %s: %w", outFile, err)
	}

	return outFile, gen.GetSourceMap(), nil
}

// rewriteTraceback replaces .py file references with .zn file references in Python tracebacks.
func rewriteTraceback(stderr, pyFile, znFile string, sourceMap map[int]int) string {
	var result strings.Builder
	for _, line := range strings.Split(stderr, "\n") {
		if strings.Contains(line, pyFile) && strings.Contains(line, ", line ") {
			idx := strings.Index(line, ", line ")
			if idx >= 0 {
				after := line[idx+7:]
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

func findClosestZnLine(pyLine int, sourceMap map[int]int) int {
	if zn, ok := sourceMap[pyLine]; ok {
		return zn
	}
	for offset := 1; offset < 20; offset++ {
		if zn, ok := sourceMap[pyLine-offset]; ok {
			return zn
		}
	}
	return 0
}
