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
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"zinc/internal/codegen_java"
	"zinc/internal/lexer"
	"zinc/internal/parser"
)

// runREPLV2 starts an interactive Zinc REPL.
// Each input is transpiled to Java, compiled, and executed.
func runREPLV2() {
	fmt.Println("Zinc v3 REPL — type Zinc code, see Java output")
	fmt.Println("Type 'exit' or Ctrl-D to quit, ':java' to see generated Java")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	var history []string
	showJava := false
	tmpDir := filepath.Join(os.TempDir(), "zinc-repl")
	os.MkdirAll(tmpDir, 0755)

	for {
		fmt.Print("zinc> ")
		if !scanner.Scan() {
			fmt.Println()
			break
		}
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}
		if trimmed == "exit" || trimmed == "quit" {
			break
		}
		if trimmed == ":java" {
			showJava = !showJava
			if showJava {
				fmt.Println("  [showing generated Java]")
			} else {
				fmt.Println("  [hiding generated Java]")
			}
			continue
		}

		// Multi-line input (blocks)
		input := line
		braceDepth := strings.Count(input, "{") - strings.Count(input, "}")
		for braceDepth > 0 {
			fmt.Print("  ... ")
			if !scanner.Scan() {
				break
			}
			next := scanner.Text()
			input += "\n" + next
			braceDepth += strings.Count(next, "{") - strings.Count(next, "}")
		}

		// Build full source
		fullSrc := strings.Join(history, "\n") + "\n" + input

		// Lex + Parse
		lex := lexer.New(fullSrc)
		tokens := lex.Tokenize()
		if len(lex.Errors) > 0 {
			fmt.Fprintf(os.Stderr, "  error: %s\n", strings.Join(lex.Errors, "; "))
			continue
		}

		p := parser.New(tokens)
		prog := p.ParseV2()
		if len(p.Errors) > 0 {
			fmt.Fprintf(os.Stderr, "  error: %s\n", strings.Join(p.Errors, "; "))
			continue
		}

		// Generate Java
		gen := codegen_java.New()
		javaSrc := gen.Generate(prog, "ZincRepl")

		if showJava {
			fmt.Println("  --- Java ---")
			for _, jLine := range strings.Split(javaSrc, "\n") {
				if strings.TrimSpace(jLine) != "" {
					fmt.Printf("  %s\n", jLine)
				}
			}
			fmt.Println("  ---")
		}

		// Write, compile, run
		javaFile := filepath.Join(tmpDir, "ZincRepl.java")
		os.WriteFile(javaFile, []byte(javaSrc), 0644)

		compileCmd := exec.Command("javac", "-d", tmpDir, javaFile)
		compileCmd.Stderr = os.Stderr
		if err := compileCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  compile error\n")
			continue
		}

		runCmd := exec.Command("java", "-cp", tmpDir, "ZincRepl")
		runCmd.Stdout = os.Stdout
		runCmd.Stderr = os.Stderr
		runCmd.Run()

		// Track declarations for context
		if strings.HasPrefix(trimmed, "fn ") || strings.HasPrefix(trimmed, "class ") ||
			strings.HasPrefix(trimmed, "data ") || strings.HasPrefix(trimmed, "enum ") ||
			strings.HasPrefix(trimmed, "const ") || strings.HasPrefix(trimmed, "import ") ||
			strings.HasPrefix(trimmed, "var ") {
			history = append(history, input)
		}
	}
}
