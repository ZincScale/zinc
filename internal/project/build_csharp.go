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

package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	codegen_csharp "zinc/internal/codegen_csharp"
	"zinc/internal/config"
	"zinc/internal/errs"
	"zinc/internal/lexer"
	"zinc/internal/parser"
	"zinc/internal/typechecker"
)

// BuildCSharp transpiles all .zn files to C# and runs dotnet publish AOT.
func BuildCSharp(rootDir string, cfg *config.Config) error {
	buildDir := filepath.Join(rootDir, ".zinc-build")

	csFiles, err := TranspileCSharp(rootDir, buildDir)
	if err != nil {
		return err
	}
	fmt.Printf("transpiled %d file(s) to C#\n", len(csFiles))

	// Generate .csproj
	csproj := config.GenerateCsproj(cfg)
	csprojPath := filepath.Join(buildDir, cfg.Name+".csproj")
	if err := os.WriteFile(csprojPath, []byte(csproj), 0644); err != nil {
		return fmt.Errorf("writing .csproj: %w", err)
	}

	// dotnet restore
	restore := exec.Command("dotnet", "restore")
	restore.Dir = buildDir
	restore.Stdout = os.Stdout
	restore.Stderr = os.Stderr
	if err := restore.Run(); err != nil {
		return fmt.Errorf("dotnet restore failed: %w", err)
	}

	if cfg.Optimize {
		// AOT publish
		rid := config.RuntimeID()
		publish := exec.Command("dotnet", "publish", "-c", "Release", "-r", rid, "/p:PublishAot=true")
		publish.Dir = buildDir
		publish.Stdout = os.Stdout
		publish.Stderr = os.Stderr
		if err := publish.Run(); err != nil {
			return fmt.Errorf("dotnet publish failed: %w", err)
		}

		// Copy binary to project root
		publishDir := filepath.Join(buildDir, "bin", "Release", "net10.0", rid, "publish")
		binaryName := cfg.Name
		srcBin := filepath.Join(publishDir, binaryName)
		dstBin := filepath.Join(rootDir, binaryName)
		if err := copyFile(srcBin, dstBin); err != nil {
			return fmt.Errorf("copying binary: %w", err)
		}
		os.Chmod(dstBin, 0755)
		fmt.Printf("built %s (AOT native binary)\n", binaryName)
	} else {
		// Regular build
		build := exec.Command("dotnet", "build", "-c", "Release")
		build.Dir = buildDir
		build.Stdout = os.Stdout
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			return fmt.Errorf("dotnet build failed: %w", err)
		}
		fmt.Println("built (managed, no AOT)")
	}

	return nil
}

// RunCSharp transpiles all .zn files to C# and runs the project.
func RunCSharp(rootDir string, cfg *config.Config) error {
	buildDir := filepath.Join(rootDir, ".zinc-build")

	if _, err := TranspileCSharp(rootDir, buildDir); err != nil {
		return err
	}

	// Generate .csproj if needed
	csprojPath := filepath.Join(buildDir, cfg.Name+".csproj")
	if _, err := os.Stat(csprojPath); os.IsNotExist(err) {
		csproj := config.GenerateCsproj(cfg)
		if err := os.WriteFile(csprojPath, []byte(csproj), 0644); err != nil {
			return fmt.Errorf("writing .csproj: %w", err)
		}
	}

	// dotnet run
	run := exec.Command("dotnet", "run", "--project", buildDir)
	run.Stdout = os.Stdout
	run.Stderr = os.Stderr
	return run.Run()
}

// TranspileCSharp transpiles all .zn files to .cs files in the build directory.
func TranspileCSharp(rootDir, buildDir string) ([]string, error) {
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	rootDir = abs

	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return nil, err
	}

	// Collect .zn files
	var srcPaths []string
	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// Skip build directory
		if info.IsDir() && filepath.Base(path) == ".zinc-build" {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(path, ".zn") {
			srcPaths = append(srcPaths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(srcPaths) == 0 {
		return nil, fmt.Errorf("no .zn files found in %s", rootDir)
	}

	// Parse all files
	type parsedFile struct {
		srcPath string
		prog    *parser.Program
	}
	var parsed []parsedFile

	for _, src := range srcPaths {
		rel, _ := filepath.Rel(rootDir, src)
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

	// Type checking
	progs := make([]*parser.Program, len(parsed))
	for i, pf := range parsed {
		progs[i] = pf.prog
	}
	if tcErrs := typechecker.CheckAll(progs); len(tcErrs) > 0 {
		for _, e := range tcErrs {
			file := e.File
			if file == "" {
				file = rootDir
			}
			msg := e.Msg
			if e.Line > 0 {
				msg = fmt.Sprintf("line %d: %s", e.Line, e.Msg)
			}
			errs.TypeErrors(file, []string{msg})
		}
		return nil, fmt.Errorf("%d type error(s) found", len(tcErrs))
	}

	// Generate C# files
	var csFiles []string
	for _, pf := range parsed {
		rel, _ := filepath.Rel(rootDir, pf.srcPath)

		gen := codegen_csharp.New()
		csSrc := gen.Generate(pf.prog)

		// Output to build directory
		outName := strings.TrimSuffix(filepath.Base(rel), ".zn") + ".cs"
		outPath := filepath.Join(buildDir, outName)

		if err := os.WriteFile(outPath, []byte(csSrc), 0644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", outPath, err)
		}

		fmt.Printf("  %s → %s\n", rel, outName)
		csFiles = append(csFiles, outPath)
	}

	return csFiles, nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
