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
	"strings"

	"zinc/internal/codegen_python"
	"zinc/internal/lexer"
	"zinc/internal/parser"
)

// runREPLV2 starts an interactive Zinc v2 REPL.
// Each input is transpiled to Python and executed in a persistent Python process.
func runREPLV2() {
	fmt.Println("Zinc v2 REPL — type Zinc code, see Python output")
	fmt.Println("Type 'exit' or Ctrl-D to quit, ':py' to see generated Python")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	var history []string // accumulate declarations for context
	showPy := false

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
		if trimmed == ":py" {
			showPy = !showPy
			if showPy {
				fmt.Println("  [showing generated Python]")
			} else {
				fmt.Println("  [hiding generated Python]")
			}
			continue
		}

		// For multi-line input (blocks), collect until braces balance
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

		// Build full source: history (declarations) + current input
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

		// Generate Python
		gen := codegen_python.New()
		pySrc := gen.GenerateV2(prog)

		if showPy {
			fmt.Println("  --- Python ---")
			for _, pyLine := range strings.Split(pySrc, "\n") {
				if strings.TrimSpace(pyLine) != "" {
					fmt.Printf("  %s\n", pyLine)
				}
			}
			fmt.Println("  ---")
		}

		// Execute with Python
		cmd := exec.Command("python3", "-c", pySrc)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()

		// If input was a declaration (fn, class, data, enum, const, import),
		// add to history so future inputs have context
		if strings.HasPrefix(trimmed, "fn ") || strings.HasPrefix(trimmed, "class ") ||
			strings.HasPrefix(trimmed, "data ") || strings.HasPrefix(trimmed, "enum ") ||
			strings.HasPrefix(trimmed, "const ") || strings.HasPrefix(trimmed, "import ") ||
			strings.HasPrefix(trimmed, "from ") || strings.HasPrefix(trimmed, "var ") {
			history = append(history, input)
		}
	}
}
