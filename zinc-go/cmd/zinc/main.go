package main

import (
	"fmt"
	"os"
	"strings"

	"zinc-go/internal/errs"
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
			os.Exit(1)
		}
		target := os.Args[2]
		info, err := os.Stat(target)
		if err != nil {
			errs.Errorf("cannot stat %s: %s", target, err)
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
			errs.Error("zinc add requires a module (e.g. github.com/foo/bar@v1.0.0)")
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

	case "version":
		fmt.Printf("zinc %s\n", version)

	default:
		// Default: treat first arg as a .zn file to run (shorthand for zinc run)
		input := os.Args[1]
		if strings.HasSuffix(input, ".zn") || isProjectDir(input) {
			// Collect program args after "--"
			var progArgs []string
			for i := 2; i < len(os.Args); i++ {
				if os.Args[i] == "--" {
					progArgs = os.Args[i+1:]
					break
				}
			}
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
		} else {
			printUsage()
			os.Exit(1)
		}
	}
}

func printUsage() {
	fmt.Println("Usage: zinc <command> [args]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Printf("  %-45s %s\n", "zinc run <file.zn|dir> [-- args...]", "Transpile and run")
	fmt.Printf("  %-45s %s\n", "zinc build [dir] [-o outdir] [--cross os/arch]", "Transpile and build")
	fmt.Printf("  %-45s %s\n", "zinc init <name>", "Create a new Zinc project")
	fmt.Printf("  %-45s %s\n", "zinc fmt <file.zn|dir>", "Format Zinc source code")
	fmt.Printf("  %-45s %s\n", "zinc add <module@version>", "Add a Go dependency")
	fmt.Printf("  %-45s %s\n", "zinc deps", "List dependencies")
	fmt.Printf("  %-45s %s\n", "zinc <file.zn> [-- args...]", "Shorthand for zinc run")
	fmt.Printf("  %-45s %s\n", "zinc version", "Show version")
	fmt.Println()
	fmt.Println("Project mode: when a zinc.toml is present, build/run use the project config.")
	fmt.Println()
	fmt.Println("Cross-compilation targets: linux/amd64, linux/arm64, darwin/amd64,")
	fmt.Println("  darwin/arm64, windows/amd64, windows/arm64")
}
