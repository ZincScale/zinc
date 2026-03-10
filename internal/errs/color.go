package errs

import (
	"fmt"
	"os"

	"golang.org/x/term"
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

var colorEnabled = term.IsTerminal(int(os.Stderr.Fd()))

func color(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + reset
}

// Error prints a formatted error message to stderr.
func Error(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", color(bold+red, "error:"), msg)
}

// Errorf prints a formatted error message to stderr.
func Errorf(format string, args ...any) {
	Error(fmt.Sprintf(format, args...))
}

// Warning prints a formatted warning message to stderr.
func Warning(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", color(bold+yellow, "warning:"), msg)
}

// Warningf prints a formatted warning message to stderr.
func Warningf(format string, args ...any) {
	Warning(fmt.Sprintf(format, args...))
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

// WatchError prints a timestamped error for watch mode.
func WatchError(ts, msg string) {
	fmt.Fprintf(os.Stderr, "%s %s %s\n",
		color(gray, "["+ts+"]"),
		color(bold+red, "error:"),
		msg,
	)
}

// WatchErrors prints timestamped errors for watch mode.
func WatchErrors(ts, file, category string, errors []string) {
	fmt.Fprintf(os.Stderr, "%s %s\n",
		color(gray, "["+ts+"]"),
		color(bold+red, category+" errors:"),
	)
	for _, e := range errors {
		fmt.Fprintf(os.Stderr, "  %s%s%s%s %s\n",
			color(bold+red, "error"),
			color(gray, "["),
			color(cyan, file),
			color(gray, "]:"),
			e,
		)
	}
}

// ReplError prints an error for REPL mode.
func ReplError(category, msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n",
		color(bold+red, category+":"),
		msg,
	)
}
