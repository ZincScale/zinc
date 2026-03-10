package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"zinc/internal/codegen"
	"zinc/internal/errs"
	"zinc/internal/lexer"
	"zinc/internal/parser"
	"zinc/internal/typechecker"
)

// FileUnit represents a single .zn → .go transpilation result.
type FileUnit struct {
	SrcPath     string // absolute path to .zn source file
	OutPath     string // absolute path to .go output file
	PackageName string // Go package name written to the output file
}

// Build transpiles all .zn files under rootDir and runs `go build ./...`.
func Build(rootDir string) error {
	units, err := Transpile(rootDir)
	if err != nil {
		return err
	}
	fmt.Printf("transpiled %d file(s)\n", len(units))

	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = rootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Run transpiles all .zn files under rootDir and runs `go run .`.
func Run(rootDir string) error {
	if _, err := Transpile(rootDir); err != nil {
		return err
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = rootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Transpile walks rootDir, groups .zn files by directory (= Go package),
// builds a shared TypeRegistry per directory, and emits .go files.
func Transpile(rootDir string) ([]FileUnit, error) {
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	rootDir = abs

	// Collect .zn files grouped by directory
	dirFiles := make(map[string][]string) // dir → []srcPath
	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() && strings.HasSuffix(path, ".zn") {
			dir := filepath.Dir(path)
			dirFiles[dir] = append(dirFiles[dir], path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var units []FileUnit
	for dir, srcPaths := range dirFiles {
		dirUnits, err := transpileDir(rootDir, dir, srcPaths)
		if err != nil {
			return nil, err
		}
		units = append(units, dirUnits...)
	}
	return units, nil
}

// transpileDir handles all .zn files in one directory (one Go package).
func transpileDir(rootDir, dir string, srcPaths []string) ([]FileUnit, error) {
	// Phase 1: parse all files
	type parsedFile struct {
		srcPath string
		prog    *parser.Program
	}
	var parsed []parsedFile

	for _, src := range srcPaths {
		rel, _ := filepath.Rel(rootDir, src)
		if rel == "" {
			rel = src
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		l := lexer.New(string(data))
		tokens := l.Tokenize()
		if len(l.Errors) > 0 {
			errs.FileErrors(rel, l.Errors)
			return nil, fmt.Errorf("%s: lexer errors found", rel)
		}
		p := parser.New(tokens)
		prog := p.Parse()
		if len(p.Errors) > 0 {
			errs.FileErrors(rel, p.Errors)
			return nil, fmt.Errorf("%s: parse errors found", rel)
		}
		prog.SourceFile = rel
		parsed = append(parsed, parsedFile{srcPath: src, prog: prog})
	}

	// Determine Go package name for this directory
	pkgName := ""
	for _, pf := range parsed {
		if pf.prog.Package != nil && pkgName == "" {
			pkgName = pkgLastSegment(pf.prog.Package.Path)
		}
	}
	if pkgName == "" {
		if dir == rootDir {
			pkgName = "main"
		} else {
			pkgName = filepath.Base(dir)
		}
	}

	// Build shared TypeRegistry from all files in this directory
	progs := make([]*parser.Program, len(parsed))
	for i, pf := range parsed {
		progs[i] = pf.prog
	}
	reg := codegen.BuildRegistry(progs)

	// Type checking across all files in this directory
	if tcErrs := typechecker.CheckAll(progs); len(tcErrs) > 0 {
		for _, e := range tcErrs {
			file := e.File
			if file == "" {
				file = dir
			}
			msg := e.Msg
			if e.Line > 0 {
				msg = fmt.Sprintf("line %d: %s", e.Line, e.Msg)
			}
			errs.TypeErrors(file, []string{msg})
		}
		return nil, fmt.Errorf("%d type error(s) found", len(tcErrs))
	}

	// Phase 2: generate .go files
	var units []FileUnit
	for _, pf := range parsed {
		// Compute relative path for //line directives
		rel, err := filepath.Rel(rootDir, pf.srcPath)
		if err != nil {
			return nil, err
		}

		gen := codegen.NewWithRegistry(reg, pkgName)
		gen.SetSourceFile(rel)
		goSrc := gen.Generate(pf.prog)

		// Mirror directory structure: change ext
		outRel := strings.TrimSuffix(rel, ".zn") + ".go"
		outPath := filepath.Join(rootDir, outRel)

		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(outPath, []byte(goSrc), 0644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", outPath, err)
		}

		// Best-effort gofmt
		exec.Command("gofmt", "-w", outPath).Run() //nolint:errcheck

		fmt.Printf("  %s → %s\n", rel, outRel)
		units = append(units, FileUnit{
			SrcPath:     pf.srcPath,
			OutPath:     outPath,
			PackageName: pkgName,
		})
	}
	return units, nil
}

// pkgLastSegment returns the last segment of a package path, e.g.
// "myapp/utils" → "utils", "myapp" → "myapp".
func pkgLastSegment(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
