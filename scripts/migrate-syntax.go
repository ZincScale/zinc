//go:build ignore

package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// --- Regexes (compiled once) ------------------------------------------------

var (
	// param/field colon: name: Type → name Type (only before capital letter or ...)
	reParamColon = regexp.MustCompile(`([a-z_][a-zA-Z0-9_]*): (\.\.\.)?([A-Z])`)
	// return type colon: ): Type → ) Type
	reReturnColon = regexp.MustCompile(`\):\s+([A-Z\[])`)
	// constructor call: ClassName.new( → ClassName(
	reCtorNew = regexp.MustCompile(`\b([A-Z][a-zA-Z0-9_]*)\.new\(`)
	// for (a, b) in → for a, b in
	reForTuple = regexp.MustCompile(`for \(([a-z_][a-zA-Z0-9_]*),\s*([a-z_][a-zA-Z0-9_]*)\)\s+in\b`)
	// declaration heuristic: line starts with identifier + paren/angle (function)
	reFuncDecl = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*\s*[<(]`)
	// field heuristic: starts with lowercase identifier + space + CapitalType
	reFieldDecl = regexp.MustCompile(`^[a-z_][a-zA-Z0-9_]*\s+[A-Z]`)
	// mid-line var with explicit type: var name: Type = ... → name Type = ...
	reVarTyped = regexp.MustCompile(`\bvar ([a-z_][a-zA-Z0-9_]*):\s*([A-Z][a-zA-Z0-9_<>\[\]?]*)(\s*=)`)
	// mid-line var with explicit type (no init): var name: Type → name Type
	reVarTypedNoInit = regexp.MustCompile(`\bvar ([a-z_][a-zA-Z0-9_]*):\s*([A-Z][a-zA-Z0-9_<>\[\]?]*)`)
	// mid-line var inferred: var name = ... → name := ...
	reVarInferred = regexp.MustCompile(`\bvar ([a-z_][a-zA-Z0-9_]*) = `)
)

// --- Main migration entry points -------------------------------------------

func migrateZinc(src string) string {
	lines := strings.Split(src, "\n")
	var result []string
	for _, line := range lines {
		result = append(result, migrateLine(line))
	}
	return strings.Join(result, "\n")
}

func migrateLine(line string) string {
	trimmed := strings.TrimSpace(line)
	indent := line[:len(line)-len(trimmed)]

	// Skip empty lines and comments
	if trimmed == "" || strings.HasPrefix(trimmed, "//") {
		return line
	}

	// 1. Drop 'class ' keyword (class Foo → Foo, but not "class" inside strings)
	if strings.HasPrefix(trimmed, "class ") && len(trimmed) > 6 && trimmed[6] >= 'A' && trimmed[6] <= 'Z' {
		trimmed = trimmed[6:]
	}

	// 2. Drop 'fn ' keyword
	// Order matters: check compound prefixes first
	if strings.HasPrefix(trimmed, "pub fn ") {
		trimmed = "pub " + trimmed[7:]
	} else if strings.HasPrefix(trimmed, "static fn ") {
		trimmed = "static " + trimmed[10:]
	} else if strings.HasPrefix(trimmed, "fn ") {
		trimmed = trimmed[3:]
	}
	// Also handle "pub fn" or "static fn" mid-line (e.g. interface body)
	// These are already handled above since interface methods start with "pub fn"

	// 3. Drop 'construct ' from constructor declarations
	if strings.HasPrefix(trimmed, "construct new(") {
		trimmed = trimmed[10:] // "construct " = 10 chars → "new("
	}

	// 4. Handle 'var' declarations (line-start)
	trimmed = migrateVar(trimmed)

	// 4b. Handle 'var' mid-line (e.g. "main() { var x: Int = 42 }")
	trimmed = migrateVarMidLine(trimmed)

	// 5. Remove colons from parameter type annotations (name: Type → name Type)
	trimmed = migrateTypeColons(trimmed)

	// 6. Remove colon from return type: ): Type → ) Type
	trimmed = reReturnColon.ReplaceAllString(trimmed, ") ${1}")

	// 7. Drop parens from if/while/for
	trimmed = migrateControlFlowParens(trimmed)

	// 8. ClassName.new( → ClassName(
	trimmed = migrateConstructorNew(trimmed)

	return indent + trimmed
}

// migrateVar handles var declarations:
//   - var name: Type [= expr]  → name Type [= expr]     (field or typed local)
//   - var name = expr           → name := expr            (inferred local)
//   - var (a, b) = ...          → unchanged (tuple)
func migrateVar(trimmed string) string {
	if !strings.HasPrefix(trimmed, "var ") {
		return trimmed
	}
	rest := trimmed[4:]

	// Tuple destructuring: var (a, b) = ... → keep as-is
	if strings.HasPrefix(rest, "(") {
		return trimmed
	}

	// Find the name part (everything before first : or space)
	nameEnd := strings.IndexAny(rest, ": ")
	if nameEnd < 0 {
		return trimmed // just "var x" — unusual, keep
	}

	name := rest[:nameEnd]
	after := rest[nameEnd:]

	// Has type annotation: "var name: Type ..." or "var name:Type ..."
	if after[0] == ':' {
		// Strip ": " or ":" before type
		after = strings.TrimLeft(after, ":")
		after = strings.TrimLeft(after, " ")
		// Result: name Type [= expr]
		return name + " " + after
	}

	// "var name = expr" (space after name, next is = or something else)
	after = strings.TrimLeft(after, " ")
	if strings.HasPrefix(after, "= ") {
		// Inferred: var name = expr → name := expr
		return name + " := " + after[2:]
	}

	// "var name Type ..." (no colon — already new-style, or explicit type without colon)
	// Check if next word starts with capital (type name)
	if len(after) > 0 && after[0] >= 'A' && after[0] <= 'Z' {
		return name + " " + after
	}

	// Unknown pattern, keep as-is
	return trimmed
}

// migrateVarMidLine handles var declarations that appear mid-line
// (e.g. inside "main() { var x: Int = 42 }").
func migrateVarMidLine(line string) string {
	// var name: Type = expr → name Type = expr
	line = reVarTyped.ReplaceAllString(line, "${1} ${2}${3}")
	// var name: Type (no init) → name Type
	line = reVarTypedNoInit.ReplaceAllString(line, "${1} ${2}")
	// var name = expr → name := expr
	line = reVarInferred.ReplaceAllString(line, "${1} := ")
	return line
}

// migrateTypeColons removes colons in param declarations: name: Type → name Type
// Only operates outside of string literals and only in declaration contexts.
func migrateTypeColons(line string) string {
	// Identify string spans to avoid modifying string contents
	type span struct{ start, end int }
	var strSpans []span

	runes := []rune(line)
	i := 0
	for i < len(runes) {
		if runes[i] == '"' {
			start := i
			i++
			for i < len(runes) && runes[i] != '"' {
				if runes[i] == '\\' && i+1 < len(runes) {
					i++
				}
				i++
			}
			if i < len(runes) {
				i++
			}
			strSpans = append(strSpans, span{start, i})
		} else {
			i++
		}
	}

	inString := func(pos int) bool {
		for _, s := range strSpans {
			if pos >= s.start && pos < s.end {
				return true
			}
		}
		return false
	}

	// Only replace in declaration contexts (not named args in function calls)
	stripped := strings.TrimSpace(line)
	isDeclaration := false
	for _, prefix := range []string{"pub ", "static ", "new(", "interface "} {
		if strings.Contains(stripped, prefix) {
			isDeclaration = true
			break
		}
	}
	if !isDeclaration {
		isDeclaration = reFuncDecl.MatchString(stripped)
	}
	if !isDeclaration && (strings.HasPrefix(stripped, "var ") || reFieldDecl.MatchString(stripped)) {
		isDeclaration = true
	}

	if !isDeclaration {
		return line
	}

	// Apply regex, but skip matches inside strings
	result := reParamColon.ReplaceAllStringFunc(line, func(match string) string {
		idx := strings.Index(line, match)
		if inString(idx) {
			return match
		}
		// Remove the colon: "name: Type" → "name Type"
		return reParamColon.ReplaceAllString(match, "${1} ${2}${3}")
	})

	return result
}

func migrateControlFlowParens(line string) string {
	// if (...) { → if ... {
	if strings.HasPrefix(line, "if (") || strings.HasPrefix(line, "} else if (") {
		return removeControlParens(line, "if")
	}
	// while (...) { → while ... {
	if strings.HasPrefix(line, "while (") {
		return removeControlParens(line, "while")
	}
	// for (i, item) in → for i, item in
	if strings.HasPrefix(line, "for (") {
		if reForTuple.MatchString(line) {
			return reForTuple.ReplaceAllString(line, "for ${1}, ${2} in")
		}
	}
	return line
}

func removeControlParens(line, keyword string) string {
	// Handle "} else if (" prefix
	prefix := ""
	working := line
	if strings.HasPrefix(working, "} else ") {
		prefix = "} else "
		working = working[len(prefix):]
	}

	kwPrefix := keyword + " ("
	if !strings.HasPrefix(working, kwPrefix) {
		return line
	}

	rest := working[len(keyword)+1:] // starts with "("

	// Find matching close paren
	depth := 0
	closeIdx := -1
	for j := 0; j < len(rest); j++ {
		switch rest[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				closeIdx = j
			}
		}
		if closeIdx >= 0 {
			break
		}
	}

	if closeIdx < 0 {
		return line
	}

	inner := rest[1:closeIdx]
	after := rest[closeIdx+1:]

	return prefix + keyword + " " + inner + after
}

func migrateConstructorNew(line string) string {
	return reCtorNew.ReplaceAllStringFunc(line, func(match string) string {
		idx := strings.Index(line, match)
		if idx > 0 && line[idx-1] == '.' {
			return match // pkg.Type.new( — keep it
		}
		name := match[:len(match)-5] // remove ".new("
		return name + "("
	})
}

// --- Go test file handling --------------------------------------------------

// migrateGoTestFile transforms Zinc source inside backtick strings in Go test files.
// Regular Go strings (double-quoted) are left untouched.
func migrateGoTestFile(src string) string {
	var result strings.Builder
	i := 0

	for i < len(src) {
		if src[i] == '`' {
			// Backtick string — likely contains Zinc source
			result.WriteByte(src[i])
			i++
			var zincBuf strings.Builder
			for i < len(src) && src[i] != '`' {
				zincBuf.WriteByte(src[i])
				i++
			}
			// Transform the Zinc content
			transformed := migrateZinc(zincBuf.String())
			result.WriteString(transformed)
			if i < len(src) {
				result.WriteByte(src[i]) // closing backtick
				i++
			}
		} else if src[i] == '"' {
			// Double-quoted Go string — don't transform
			result.WriteByte(src[i])
			i++
			for i < len(src) && src[i] != '"' {
				if src[i] == '\\' && i+1 < len(src) {
					result.WriteByte(src[i])
					i++
				}
				result.WriteByte(src[i])
				i++
			}
			if i < len(src) {
				result.WriteByte(src[i])
				i++
			}
		} else {
			result.WriteByte(src[i])
			i++
		}
	}

	return result.String()
}

// --- Main -------------------------------------------------------------------

func main() {
	dryRun := false
	var files []string
	for _, arg := range os.Args[1:] {
		if arg == "--dry-run" || arg == "-n" {
			dryRun = true
		} else {
			files = append(files, arg)
		}
	}

	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: go run migrate-syntax.go [--dry-run] <file> ...\n")
		os.Exit(1)
	}

	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", path, err)
			continue
		}

		src := string(data)
		var migrated string

		if strings.HasSuffix(path, ".go") {
			migrated = migrateGoTestFile(src)
		} else {
			migrated = migrateZinc(src)
		}

		if src == migrated {
			fmt.Printf("No changes: %s\n", path)
			continue
		}

		if dryRun {
			fmt.Printf("Would change: %s\n", path)
			continue
		}

		err = os.WriteFile(path, []byte(migrated), 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", path, err)
			continue
		}
		fmt.Printf("Migrated: %s\n", path)
	}
}
