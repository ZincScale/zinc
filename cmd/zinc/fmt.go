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
	"strings"

	"zinc/internal/lexer"
)

// runFmt formats a .zn file with consistent indentation and style.
// Uses a token-level formatter (not AST-based) for simplicity and
// to preserve comments and structure.
func runFmt(filename string) {
	src, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	formatted := formatZinc(string(src))

	if err := os.WriteFile(filename, []byte(formatted), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", filename, err)
		os.Exit(1)
	}
	fmt.Printf("formatted %s\n", filename)
}

// formatZinc reformats Zinc source code with consistent indentation.
func formatZinc(src string) string {
	lines := strings.Split(src, "\n")
	var result []string
	indent := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines — preserve them
		if trimmed == "" {
			result = append(result, "")
			continue
		}

		// Decrease indent before } or lines starting with } else / } catch
		if strings.HasPrefix(trimmed, "}") {
			indent--
			if indent < 0 {
				indent = 0
			}
		}

		// Write line with current indent
		result = append(result, strings.Repeat("    ", indent)+trimmed)

		// Increase indent after lines ending with {
		if strings.HasSuffix(trimmed, "{") {
			indent++
		}
	}

	// Ensure trailing newline
	out := strings.Join(result, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

// Keep lexer import used
var _ = lexer.New
