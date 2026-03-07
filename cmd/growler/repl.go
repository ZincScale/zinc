package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"growler/internal/codegen"
	"growler/internal/lexer"
	"growler/internal/parser"
	"growler/internal/typechecker"
)

func runREPL() {
	fmt.Println("Growler REPL — type Growler code, press Enter to run.")
	fmt.Println("Commands: help, clear, exit")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	var topDecls []string // accumulated top-level decls (fn, class, enum, const, import)
	var bodyDecls []string // accumulated var/statement history inside main

	for {
		fmt.Print("growler> ")
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
func isTopLevelDecl(input string) bool {
	trimmed := strings.TrimSpace(input)
	return strings.HasPrefix(trimmed, "fn ") ||
		strings.HasPrefix(trimmed, "pub fn ") ||
		strings.HasPrefix(trimmed, "class ") ||
		strings.HasPrefix(trimmed, "interface ") ||
		strings.HasPrefix(trimmed, "enum ") ||
		strings.HasPrefix(trimmed, "const ") ||
		strings.HasPrefix(trimmed, "import ")
}

// isVarDecl returns true if input is a variable declaration.
func isVarDecl(input string) bool {
	trimmed := strings.TrimSpace(input)
	return strings.HasPrefix(trimmed, "var ")
}

// isBareExpression returns true if the input looks like a standalone expression
// (not a statement keyword like var, if, for, while, etc.).
func isBareExpression(input string) bool {
	trimmed := strings.TrimSpace(input)
	stmtPrefixes := []string{
		"var ", "if ", "if(", "for ", "for(", "while ", "while(",
		"return ", "return\n", "throw ", "try ", "try{",
		"match ", "with ", "with(", "go ", "go{",
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

	// Top-level declarations (fn, class, enum, const, import)
	for _, h := range topDecls {
		src.WriteString(h)
		src.WriteString("\n")
	}

	if isTopLevelDecl(input) {
		src.WriteString(input)
		src.WriteString("\n")
		src.WriteString("fn main() {\n")
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
		src.WriteString("fn main() {\n")
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
			fmt.Fprintln(os.Stderr, "lex:", e)
		}
		return
	}

	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		for _, e := range p.Errors {
			fmt.Fprintln(os.Stderr, "parse:", e)
		}
		return
	}

	if errs := typechecker.Check(prog); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "type error:", e)
		}
		return
	}

	gen := codegen.New()
	goSrc := gen.Generate(prog)

	// Write to temp file and run
	tmp, err := os.CreateTemp("", "growler_repl_*.go")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(goSrc); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
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

// extractVarName pulls the variable name from a "var NAME ..." declaration.
func extractVarName(decl string) string {
	trimmed := strings.TrimSpace(decl)
	if !strings.HasPrefix(trimmed, "var ") {
		return ""
	}
	rest := strings.TrimSpace(trimmed[4:])
	// Handle tuple: var (a, b) = ...
	if strings.HasPrefix(rest, "(") {
		return ""
	}
	// var name[: Type][ = expr]
	for i, ch := range rest {
		if ch == ' ' || ch == ':' || ch == '=' {
			return rest[:i]
		}
	}
	return rest
}
