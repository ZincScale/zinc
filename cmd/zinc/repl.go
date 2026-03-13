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

	"zinc/internal/codegen"
	"zinc/internal/errs"
	"zinc/internal/lexer"
	"zinc/internal/parser"
	"zinc/internal/typechecker"
)

func runREPL() {
	fmt.Println("Zinc REPL — type Zinc code, press Enter to run.")
	fmt.Println("Commands: help, clear, exit")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	var topDecls []string // accumulated top-level decls (functions, classes, enum, const, import)
	var bodyDecls []string // accumulated variable declarations inside main

	for {
		fmt.Print("zinc> ")
		var lines []string
		depth := 0

		// Read one logical input (possibly multi-line)
		for scanner.Scan() {
			line := scanner.Text()
			lines = append(lines, line)
			depth += countBraceDepth(line)
			if depth <= 0 {
				break
			}
			fmt.Print("  ... ")
		}

		input := strings.TrimSpace(strings.Join(lines, "\n"))
		if input == "" {
			continue
		}

		switch input {
		case "exit", "quit":
			fmt.Println("Goodbye!")
			return
		case "clear":
			topDecls = nil
			bodyDecls = nil
			fmt.Println("Session cleared.")
			continue
		case "help":
			printREPLHelp()
			continue
		}

		replEval(input, topDecls, bodyDecls)

		if isTopLevelDecl(input) {
			topDecls = append(topDecls, input)
		} else if isVarDecl(input) {
			bodyDecls = append(bodyDecls, input)
		}
	}
}

func printREPLHelp() {
	fmt.Println("Commands:")
	fmt.Println("  help    — show this help")
	fmt.Println("  clear   — clear session history (functions, classes, variables)")
	fmt.Println("  exit    — quit the REPL")
	fmt.Println()
	fmt.Println("Tips:")
	fmt.Println("  - Expressions are auto-printed: type `1 + 2` and see the result")
	fmt.Println("  - Multi-line: end a line with `{` to continue, close with `}`")
	fmt.Println("  - Functions, classes, enums, consts, and vars persist across inputs")
}

// countBraceDepth counts net brace depth in a line, ignoring braces inside
// string literals (double-quoted), raw strings (backtick), and comments.
func countBraceDepth(line string) int {
	depth := 0
	inString := false
	inRaw := false
	i := 0
	for i < len(line) {
		ch := line[i]
		if inRaw {
			if ch == '`' {
				inRaw = false
			}
			i++
			continue
		}
		if inString {
			if ch == '\\' {
				i += 2 // skip escaped char
				continue
			}
			if ch == '"' {
				inString = false
			}
			i++
			continue
		}
		// Check for line comment
		if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			break // rest of line is comment
		}
		switch ch {
		case '"':
			inString = true
		case '`':
			inRaw = true
		case '{':
			depth++
		case '}':
			depth--
		}
		i++
	}
	return depth
}

// isTopLevelDecl returns true if input looks like a top-level declaration.
// In type-before-name syntax:
//   - Classes: CapitalName { ... } or CapitalName : Parent { ... }
//   - Functions: name(params) { ... } or ReturnType name(params) { ... }
//   - pub ReturnType name(params) ... (public functions)
//   - interface, enum, const, import keep their keywords
func isTopLevelDecl(input string) bool {
	trimmed := strings.TrimSpace(input)
	if strings.HasPrefix(trimmed, "interface ") ||
		strings.HasPrefix(trimmed, "enum ") ||
		strings.HasPrefix(trimmed, "const ") ||
		strings.HasPrefix(trimmed, "import ") {
		return true
	}
	// Strip "pub " prefix for visibility modifier
	check := trimmed
	if strings.HasPrefix(check, "pub ") {
		check = strings.TrimSpace(check[4:])
	}
	if len(check) == 0 {
		return false
	}
	// Class or function-with-return-type: starts with uppercase letter
	if check[0] >= 'A' && check[0] <= 'Z' {
		// Find end of first identifier
		i := 0
		for i < len(check) && (check[i] >= 'A' && check[i] <= 'Z' || check[i] >= 'a' && check[i] <= 'z' || check[i] >= '0' && check[i] <= '9' || check[i] == '_') {
			i++
		}
		rest := strings.TrimSpace(check[i:])
		// Class decl: Name { or Name : Parent { or Name<T> {
		if strings.HasPrefix(rest, "{") || strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "<") {
			return true
		}
		// Function with return type: ReturnType name(params) { — next token is lowercase
		if len(rest) > 0 && rest[0] >= 'a' && rest[0] <= 'z' || len(rest) > 0 && rest[0] == '_' {
			return isFuncShape(rest)
		}
		return false
	}
	// Function without return type: starts with lowercase, e.g. main() { or add(Int a) {
	if check[0] >= 'a' && check[0] <= 'z' || check[0] == '_' {
		return isFuncShape(check)
	}
	return false
}

// isFuncShape returns true if s looks like name(...)...{ (function definition shape).
func isFuncShape(s string) bool {
	if strings.Contains(s, "(") && strings.Contains(s, "{") {
		parenIdx := strings.Index(s, "(")
		braceIdx := strings.Index(s, "{")
		return parenIdx < braceIdx
	}
	return false
}

// isVarDecl returns true if input is a variable declaration.
// In type-before-name syntax, variable declarations use:
//   - name := expr (inferred type)
//   - Type name = expr (explicit typed, e.g. String? name = null)
func isVarDecl(input string) bool {
	trimmed := strings.TrimSpace(input)
	// Inferred: name := expr
	if strings.Contains(trimmed, ":=") {
		return true
	}
	// Typed: Type name = expr — starts with uppercase, has ' = ' but not ':='
	if len(trimmed) > 0 && trimmed[0] >= 'A' && trimmed[0] <= 'Z' &&
		strings.Contains(trimmed, " = ") && !isTopLevelDecl(trimmed) {
		return true
	}
	return false
}

// isBareExpression returns true if the input looks like a standalone expression
// (not a statement keyword like var, if, for, while, etc.).
func isBareExpression(input string) bool {
	trimmed := strings.TrimSpace(input)
	stmtPrefixes := []string{
		"if ", "for ", "while ",
		"return ", "return\n",
		"match ", "with ", "go ", "go{",
		"print(", "printf(",
	}
	for _, p := range stmtPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return false
		}
	}
	if isTopLevelDecl(trimmed) {
		return false
	}
	// Declaration with := or typed var decl
	if strings.Contains(trimmed, ":=") {
		return false
	}
	if isVarDecl(trimmed) {
		return false
	}
	// Assignment operators
	if strings.Contains(trimmed, " = ") ||
		strings.Contains(trimmed, " += ") ||
		strings.Contains(trimmed, " -= ") ||
		strings.Contains(trimmed, " *= ") ||
		strings.Contains(trimmed, " /= ") {
		return false
	}
	return true
}

func replEval(input string, topDecls []string, bodyDecls []string) {
	var src strings.Builder

	// Top-level declarations (functions, classes, enum, const, import)
	for _, h := range topDecls {
		src.WriteString(h)
		src.WriteString("\n")
	}

	if isTopLevelDecl(input) {
		src.WriteString(input)
		src.WriteString("\n")
		src.WriteString("main() {\n")
		// Include body vars so they're "used" (avoids Go compile errors on re-eval)
		for _, b := range bodyDecls {
			src.WriteString(b)
			src.WriteString("\n")
		}
		// Use _ to suppress "declared and not used" for accumulated vars
		for _, b := range bodyDecls {
			name := extractVarName(b)
			if name != "" {
				src.WriteString("_ = " + name + "\n")
			}
		}
		src.WriteString("}\n")
	} else {
		src.WriteString("main() {\n")
		// Replay accumulated body vars
		for _, b := range bodyDecls {
			src.WriteString(b)
			src.WriteString("\n")
		}
		if isBareExpression(input) {
			src.WriteString("print(")
			src.WriteString(input)
			src.WriteString(")")
		} else {
			src.WriteString(input)
		}
		src.WriteString("\n")
		// Suppress "declared and not used" for all accumulated vars
		// plus the current input if it's a var decl
		allVars := bodyDecls
		if isVarDecl(input) {
			allVars = append(allVars, input)
		}
		for _, b := range allVars {
			name := extractVarName(b)
			if name != "" {
				src.WriteString("_ = " + name + "\n")
			}
		}
		src.WriteString("}\n")
	}

	// Transpile
	l := lexer.New(src.String())
	tokens := l.Tokenize()
	if len(l.Errors) > 0 {
		for _, e := range l.Errors {
			errs.ReplError("lex", e)
		}
		return
	}

	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		for _, e := range p.Errors {
			errs.ReplError("parse", e)
		}
		return
	}

	if tcErrs := typechecker.Check(prog); len(tcErrs) > 0 {
		for _, e := range tcErrs {
			errs.ReplError("type error", e.String())
		}
		return
	}

	gen := codegen.New()
	goSrc := gen.Generate(prog)

	// Write to temp file and run
	tmp, err := os.CreateTemp("", "zinc_repl_*.go")
	if err != nil {
		errs.Error(err.Error())
		return
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(goSrc); err != nil {
		errs.Error(err.Error())
		return
	}
	tmp.Close()

	// gofmt silently
	exec.Command("gofmt", "-w", tmp.Name()).Run() //nolint

	cmd := exec.Command("go", "run", tmp.Name())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run() //nolint
}

// extractVarName pulls the variable name from a declaration.
// Handles: name := expr, Type name = expr
func extractVarName(decl string) string {
	trimmed := strings.TrimSpace(decl)
	// name := expr
	if idx := strings.Index(trimmed, ":="); idx > 0 {
		name := strings.TrimSpace(trimmed[:idx])
		// Skip tuple destructuring like (a, b) := ...
		if strings.HasPrefix(name, "(") {
			return ""
		}
		return name
	}
	// Type name = expr — skip first word (type), extract second word (name)
	// e.g. "String name = ..." or "String? name = ..."
	parts := strings.Fields(trimmed)
	if len(parts) >= 3 && parts[2] == "=" {
		return parts[1]
	}
	return ""
}
