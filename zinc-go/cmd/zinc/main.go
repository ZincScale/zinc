package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	codegen "zinc-go/internal/codegen_go"
	"zinc-go/internal/errs"
	"zinc-go/internal/lexer"
	"zinc-go/internal/parser"
)

const version = "3.0.0-go"

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
		if len(os.Args) < 3 {
			errs.Error("zinc build requires a file or directory")
			fmt.Fprintln(os.Stderr, "Usage: zinc build <file.zn|dir> [-o outdir] [--native]")
			os.Exit(1)
		}
		input := os.Args[2]
		outDir := "zinc-out"
		for i, arg := range os.Args {
			if arg == "-o" && i+1 < len(os.Args) {
				outDir = os.Args[i+1]
			}
		}
		if err := build(input, outDir, false); err != nil {
			errs.Errorf("%s", err)
			os.Exit(1)
		}

	case "run":
		if len(os.Args) < 3 {
			errs.Error("zinc run requires a file or directory")
			fmt.Fprintln(os.Stderr, "Usage: zinc run <file.zn|dir> [-- args...]")
			os.Exit(1)
		}
		input := os.Args[2]
		// Collect program args after "--"
		var progArgs []string
		for i := 3; i < len(os.Args); i++ {
			if os.Args[i] == "--" {
				progArgs = os.Args[i+1:]
				break
			}
		}
		if err := run(input, progArgs); err != nil {
			errs.Errorf("%s", err)
			os.Exit(1)
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
	fmt.Println("  zinc build <file.zn|dir> [-o outdir]     Transpile and compile")
	fmt.Println("  zinc run <file.zn|dir> [-- args...]      Transpile and run")
	fmt.Println("  zinc fmt <file.zn|dir>                   Format Zinc source code")
	fmt.Println("  zinc <file.zn> [-- args...]              Shorthand for zinc run")
	fmt.Println("  zinc version                             Show version")
	fmt.Println("  zinc help                                Show this help")
}

// ---------------------------------------------------------------------------
// Compilation
// ---------------------------------------------------------------------------

// compileFile reads a .zn file, parses it, and generates Go source.
func compileFile(path string) ([]codegen.OutputFile, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	// Lex
	l := lexer.New(string(src))
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		return nil, fmt.Errorf("lex errors in %s:\n%s", path, strings.Join(l.Errors, "\n"))
	}

	// Parse
	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		return nil, fmt.Errorf("parse errors in %s:\n%s", path, strings.Join(p.Errors, "\n"))
	}

	// Set source file for //line directives
	absPath, _ := filepath.Abs(path)
	prog.SourceFile = absPath
	className := strings.TrimSuffix(filepath.Base(path), ".zn")
	if len(className) > 0 {
		className = strings.ToUpper(className[:1]) + className[1:]
	}

	gen := codegen.New()
	gen.SetSourceFile(absPath)
	files := gen.GenerateFiles(prog, className)
	return files, nil
}

// compileDir compiles all .zn files in a directory (recursively) and writes
// the generated .go files into outDir. If quiet is true, the "file -> out"
// progress lines are suppressed.
func compileDir(dir, outDir string, quiet bool) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".zn") {
			return nil
		}
		files, cErr := compileFile(path)
		if cErr != nil {
			return cErr
		}
		for _, f := range files {
			outPath := filepath.Join(outDir, f.Name)
			if wErr := os.WriteFile(outPath, []byte(f.Content), 0o644); wErr != nil {
				return fmt.Errorf("write %s: %w", outPath, wErr)
			}
			if !quiet {
				fmt.Printf("  %s → %s\n", path, outPath)
			}
		}
		return nil
	})
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

	// src/main.zn
	mainZn := `fn main() {
    print("Hello from Zinc!")
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "main.zn"), []byte(mainZn), 0o644); err != nil {
		return fmt.Errorf("write main.zn: %w", err)
	}

	// go.mod
	goMod := fmt.Sprintf("module %s\n\ngo 1.26\n", name)
	if err := os.WriteFile(filepath.Join(name, "go.mod"), []byte(goMod), 0o644); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	// .gitignore
	gitignore := "zinc-out/\n*.exe\n"
	if err := os.WriteFile(filepath.Join(name, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}

	fmt.Printf("Created project %s/\n", name)
	fmt.Printf("  %s/src/main.zn\n", name)
	fmt.Printf("  %s/go.mod\n", name)
	fmt.Printf("  %s/.gitignore\n", name)
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
