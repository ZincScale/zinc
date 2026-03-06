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
	fmt.Println("Growler REPL — type Growler code, press Enter to run. Ctrl+C to exit.")
	fmt.Println("Tip: Multi-line input — end a line with '{' to continue. Close with '}'.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	var history []string // accumulated top-level decls across session

	for {
		fmt.Print("growler> ")
		var lines []string
		depth := 0

		// Read one logical input (possibly multi-line)
		for scanner.Scan() {
			line := scanner.Text()
			lines = append(lines, line)
			for _, ch := range line {
				if ch == '{' {
					depth++
				} else if ch == '}' {
					depth--
				}
			}
			if depth <= 0 {
				break
			}
			fmt.Print("  ... ")
		}

		input := strings.TrimSpace(strings.Join(lines, "\n"))
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			return
		}
		if input == "clear" {
			history = nil
			fmt.Println("Session cleared.")
			continue
		}

		replEval(input, history)

		if isTopLevelDecl(input) {
			history = append(history, input)
		}
	}
}

// isTopLevelDecl returns true if input looks like a top-level declaration.
func isTopLevelDecl(input string) bool {
	trimmed := strings.TrimSpace(input)
	return strings.HasPrefix(trimmed, "fn ") ||
		strings.HasPrefix(trimmed, "pub fn ") ||
		strings.HasPrefix(trimmed, "class ") ||
		strings.HasPrefix(trimmed, "interface ") ||
		strings.HasPrefix(trimmed, "enum ") ||
		strings.HasPrefix(trimmed, "import ")
}

func replEval(input string, history []string) {
	var src strings.Builder
	for _, h := range history {
		src.WriteString(h)
		src.WriteString("\n")
	}

	if isTopLevelDecl(input) {
		src.WriteString(input)
		src.WriteString("\n")
		src.WriteString("fn main() { }\n")
	} else {
		src.WriteString("fn main() {\n")
		src.WriteString(input)
		src.WriteString("\n}\n")
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
