package errs

import (
	"fmt"
	"os"
)

// ANSI color codes
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	red    = "\033[31m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
	gray   = "\033[90m"
)

// colorEnabled is true when stderr is a terminal.
var colorEnabled = func() bool {
	// Simple heuristic: check TERM env var
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}()

func color(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + reset
}

// Red wraps text in red color.
func Red(s string) string {
	return color(bold+red, s)
}

// Error prints a formatted error message to stderr.
func Error(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", color(bold+red, "error:"), msg)
}

// Errorf prints a formatted error message to stderr.
func Errorf(format string, args ...any) {
	Error(fmt.Sprintf(format, args...))
}

// FileError prints a single error with a file location.
func FileError(file, detail string) {
	fmt.Fprintf(os.Stderr, "%s%s%s%s %s\n",
		color(bold+red, "error"),
		color(gray, "["),
		color(cyan, file),
		color(gray, "]:"),
		detail,
	)
}

// FileErrors prints multiple errors with a file location prefix.
func FileErrors(file string, errors []string) {
	for _, e := range errors {
		FileError(file, e)
	}
}

// TypeErrors prints type errors with a file location prefix.
func TypeErrors(file string, errors []string) {
	for _, e := range errors {
		fmt.Fprintf(os.Stderr, "%s%s%s%s %s\n",
			color(bold+red, "type error"),
			color(gray, "["),
			color(cyan, file),
			color(gray, "]:"),
			e,
		)
	}
}
