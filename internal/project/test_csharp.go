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
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"zinc/internal/codegen"
	codegen_csharp "zinc/internal/codegen_csharp"
	"zinc/internal/config"
	"zinc/internal/errs"
	"zinc/internal/lexer"
	"zinc/internal/parser"
	"zinc/internal/typechecker"
)

// TestCSharp discovers test_* functions, generates a test harness, and runs tests.
func TestCSharp(rootDir string, cfg *config.Config, verbose bool, filterFn string) error {
	buildDir := filepath.Join(rootDir, ".zinc-build")

	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return err
	}

	// Clean stale .cs files
	existing, _ := filepath.Glob(filepath.Join(buildDir, "*.cs"))
	for _, f := range existing {
		os.Remove(f)
	}

	// Collect all .zn files
	var srcPaths []string
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() && filepath.Base(path) == ".zinc-build" {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(path, ".zn") {
			srcPaths = append(srcPaths, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(srcPaths) == 0 {
		return fmt.Errorf("no .zn files found in %s", rootDir)
	}

	// Parse all files, discover test_* functions
	type parsedFile struct {
		srcPath string
		prog    *parser.Program
	}
	var parsed []parsedFile

	var tests []testFunc

	for _, src := range srcPaths {
		rel, _ := filepath.Rel(rootDir, src)
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		l := lexer.New(string(data))
		tokens := l.Tokenize()
		if len(l.Errors) > 0 {
			errs.FileErrors(rel, l.Errors)
			return fmt.Errorf("%s: lexer errors found", rel)
		}
		p := parser.New(tokens)
		prog := p.Parse()
		if len(p.Errors) > 0 {
			errs.FileErrors(rel, p.Errors)
			return fmt.Errorf("%s: parse errors found", rel)
		}
		prog.SourceFile = rel
		parsed = append(parsed, parsedFile{srcPath: src, prog: prog})

		// Discover test functions
		for _, decl := range prog.Decls {
			fn, ok := decl.(*parser.FnDecl)
			if !ok {
				continue
			}
			if !strings.HasPrefix(fn.Name, "test_") {
				continue
			}
			if filterFn != "" && fn.Name != filterFn {
				continue
			}
			// Convert test_addition → TestAddition for C# method name
			csName := testNameToCSharp(fn.Name)
			tests = append(tests, testFunc{name: fn.Name, csName: csName, srcFile: rel})
		}
	}

	if len(tests) == 0 {
		fmt.Println("no test functions found (functions must start with test_)")
		return nil
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
		return fmt.Errorf("%d type error(s) found", len(tcErrs))
	}

	// Build cross-file type registry
	reg := codegen.BuildRegistry(progs)

	// Generate C# files — but skip main() functions, we'll generate our own entry point
	for _, pf := range parsed {
		rel, _ := filepath.Rel(rootDir, pf.srcPath)
		gen := codegen_csharp.NewWithRegistry(reg)
		gen.SetSourceFile(rel)
		gen.SetTestMode(true) // Skip main() emission
		csSrc := gen.Generate(pf.prog)

		outName := strings.TrimSuffix(filepath.Base(rel), ".zn") + ".cs"
		outPath := filepath.Join(buildDir, outName)
		if err := os.WriteFile(outPath, []byte(csSrc), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", outPath, err)
		}
	}

	// Generate test harness
	harness := generateTestHarness(tests, verbose)
	harnessPath := filepath.Join(buildDir, "__zinc_test_harness.cs")
	if err := os.WriteFile(harnessPath, []byte(harness), 0644); err != nil {
		return fmt.Errorf("writing test harness: %w", err)
	}

	// Generate .csproj
	csproj := config.GenerateCsproj(cfg)
	csprojPath := filepath.Join(buildDir, cfg.Name+".csproj")
	if err := os.WriteFile(csprojPath, []byte(csproj), 0644); err != nil {
		return fmt.Errorf("writing .csproj: %w", err)
	}

	// Run tests via dotnet run (not AOT — speed matters for tests)
	fmt.Printf("Running %d test(s)...\n\n", len(tests))
	dotnet := findDotnet()
	if dotnet == "" {
		return fmt.Errorf("dotnet SDK not found — install .NET SDK to run tests")
	}
	run := exec.Command(dotnet, "run", "--project", buildDir)
	run.Stderr = os.Stderr

	// Capture stdout to parse results
	stdout, err := run.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating pipe: %w", err)
	}

	if err := run.Start(); err != nil {
		return fmt.Errorf("dotnet run failed: %w", err)
	}

	// Stream output
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}

	if err := run.Wait(); err != nil {
		// Non-zero exit means test failures — already printed
		return fmt.Errorf("tests failed")
	}

	return nil
}

// generateTestHarness creates a C# Main that runs each test function.
func generateTestHarness(tests []testFunc, verbose bool) string {
	var sb strings.Builder
	sb.WriteString("using System;\nusing System.Diagnostics;\nusing static Functions;\n\n")
	sb.WriteString("public class Program\n{\n")
	sb.WriteString("    public static void Main(string[] args)\n    {\n")
	sb.WriteString("        int passed = 0, failed = 0;\n")

	for _, t := range tests {
		sb.WriteString(fmt.Sprintf("        try\n"))
		sb.WriteString(fmt.Sprintf("        {\n"))
		sb.WriteString(fmt.Sprintf("            var _sw = Stopwatch.StartNew();\n"))
		sb.WriteString(fmt.Sprintf("            %s();\n", t.csName))
		sb.WriteString(fmt.Sprintf("            _sw.Stop();\n"))
		sb.WriteString(fmt.Sprintf("            Console.WriteLine($\"  PASS  %s ({_sw.ElapsedMilliseconds}ms)\");\n", t.name))
		sb.WriteString(fmt.Sprintf("            passed++;\n"))
		sb.WriteString(fmt.Sprintf("        }\n"))
		sb.WriteString(fmt.Sprintf("        catch (Exception _ex)\n"))
		sb.WriteString(fmt.Sprintf("        {\n"))
		sb.WriteString(fmt.Sprintf("            Console.WriteLine($\"  FAIL  %s\");\n", t.name))
		sb.WriteString(fmt.Sprintf("            Console.WriteLine($\"        %s — {_ex.Message}\");\n", t.srcFile))
		sb.WriteString(fmt.Sprintf("            failed++;\n"))
		sb.WriteString(fmt.Sprintf("        }\n"))
	}

	sb.WriteString("        Console.WriteLine();\n")
	sb.WriteString("        Console.WriteLine($\"{passed} passed, {failed} failed\");\n")
	sb.WriteString("        if (failed > 0) Environment.Exit(1);\n")
	sb.WriteString("    }\n}\n")
	return sb.String()
}

// testNameToCSharp converts test_addition → Test_addition (matches codegen capitalize)
func testNameToCSharp(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

type testFunc struct {
	name    string
	csName  string
	srcFile string
}

// findDotnet locates the dotnet CLI binary.
func findDotnet() string {
	if p, err := exec.LookPath("dotnet"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, candidate := range []string{
		home + "/.dotnet/dotnet",
		"/usr/local/bin/dotnet",
		"/usr/bin/dotnet",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
