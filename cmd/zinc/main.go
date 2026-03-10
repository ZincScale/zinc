package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"zinc/internal/codegen"
	"zinc/internal/lexer"
	"zinc/internal/parser"
	"zinc/internal/project"
	"zinc/internal/typechecker"
)

const usage = `Zinc transpiler — compiles .zn files to Go source.

Usage:
  zinc <file.zn> [flags]   Transpile a single file
  zinc build [dir]         Transpile all .zn files in project and run go build
  zinc run [dir]           Transpile all .zn files and run the project
  zinc init [name]         Initialize a new Zinc project
  zinc repl                Launch interactive REPL

Flags:
  -o <file>    Output Go file (default: <input>.go)
  --verbose    Print tokens and AST summary after transpiling
  --run        Transpile and immediately run with 'go run'
  --watch      Watch file for changes and re-transpile automatically
  --version    Print version and exit
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
			fmt.Println("zinc version 0.1.0")
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
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
				name = filepath.Base(dir)
			}
			if _, err := os.Stat("go.mod"); err == nil {
				fmt.Fprintln(os.Stderr, "error: go.mod already exists")
				os.Exit(1)
			}
			gomod := fmt.Sprintf("module %s\n\ngo 1.26\n", name)
			if err := os.WriteFile("go.mod", []byte(gomod), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "error writing go.mod: %v\n", err)
				os.Exit(1)
			}
			mainZn := "fn main() {\n    print(\"Hello from Zinc!\")\n}\n"
			if err := os.WriteFile("main.zn", []byte(mainZn), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "error writing main.zn: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("initialized project %q\n", name)
			fmt.Println("  created go.mod")
			fmt.Println("  created main.zn")
			return
		case a == "repl":
			runREPL()
			return
		case a == "build":
			dir := "."
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				dir = args[i+1]
				i++
			}
			if err := project.Build(dir); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case a == "run":
			dir := "."
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				dir = args[i+1]
				i++
			}
			if err := project.Run(dir); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "%s: %v\n", inFile, err)
		os.Exit(1)
	}

	// Lexer
	l := lexer.New(string(src))
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		for _, e := range l.Errors {
			fmt.Fprintf(os.Stderr, "%s:%s\n", inFile, e)
		}
		os.Exit(1)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] %d tokens\n", len(tokens))
	}

	// Parser
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		for _, e := range p.Errors {
			fmt.Fprintf(os.Stderr, "%s:%s\n", inFile, e)
		}
		os.Exit(1)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[verbose] %d top-level declarations\n", len(prog.Decls))
	}

	// Type checking
	if errs := typechecker.Check(prog); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "%s: type error: %s\n", inFile, e)
		}
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
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", outFile, err)
		os.Exit(1)
	}

	// Run gofmt
	cmd := exec.Command("gofmt", "-w", outFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "gofmt warning: %v\n%s\n", err, string(out))
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
