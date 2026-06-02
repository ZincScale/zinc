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

package pyfront

import "fmt"

// Lexer turns Python source into a token stream with INDENT/DEDENT layout
// tokens, following CPython's significant-indentation rules: an indent
// stack, blank/comment lines that don't affect layout, and physical
// newlines suppressed while inside brackets (implicit line continuation).
type Lexer struct {
	src    string
	i      int   // byte offset
	line   int   // 1-indexed
	col    int   // 1-indexed column of current position
	indent []int // indentation stack, starts at {0}
	paren  int   // bracket nesting depth ( [ {

	atLineStart bool
	toks        []Token
	Errors      []string
}

// multi-char operators, longest first so greedy matching works.
var pyOps = []string{
	"**=", "//=", ">>=", "<<=",
	"==", "!=", "<=", ">=", "->", "+=", "-=", "*=", "/=", "%=", "**", "//", ":=",
	"+", "-", "*", "/", "%", "(", ")", "[", "]", "{", "}", ":", ",", "=", "<", ">", ".", "@", ";",
}

// New creates a lexer over src.
func New(src string) *Lexer {
	return &Lexer{
		src:         src,
		line:        1,
		col:         1,
		indent:      []int{0},
		atLineStart: true,
	}
}

func (l *Lexer) errf(format string, a ...any) {
	l.Errors = append(l.Errors, fmt.Sprintf("line %d: %s", l.line, fmt.Sprintf(format, a...)))
}

func (l *Lexer) emit(k TokKind, v string, col int) {
	l.toks = append(l.toks, Token{Kind: k, Value: v, Line: l.line, Col: col})
}

func (l *Lexer) peek() byte {
	if l.i < len(l.src) {
		return l.src[l.i]
	}
	return 0
}

func (l *Lexer) at(off int) byte {
	if l.i+off < len(l.src) {
		return l.src[l.i+off]
	}
	return 0
}

// Tokenize runs the lexer to completion and returns the token slice. On
// error, Errors is non-empty and the returned tokens are partial.
func (l *Lexer) Tokenize() []Token {
	for l.i < len(l.src) {
		if l.atLineStart && l.paren == 0 {
			if l.handleLineStart() {
				continue // blank/comment line consumed
			}
		}

		c := l.peek()
		switch {
		case c == '\n':
			l.i++
			if l.paren == 0 && !l.atLineStart {
				l.emit(TNewline, "", l.col)
				l.atLineStart = true
			}
			l.line++
			l.col = 1
		case c == '\r':
			l.i++
		case c == ' ' || c == '\t':
			l.i++
			l.col++
		case c == '#':
			for l.i < len(l.src) && l.src[l.i] != '\n' {
				l.i++
			}
		case c == '\\' && l.at(1) == '\n':
			// explicit line continuation
			l.i += 2
			l.line++
			l.col = 1
		case isDigit(c):
			l.lexNumber()
		case (c == 'f' || c == 'F') && (l.at(1) == '"' || l.at(1) == '\''):
			l.lexFString()
		case isIdentStart(c):
			l.lexName()
		case c == '"' || c == '\'':
			l.lexString()
		default:
			if !l.lexOp() {
				l.errf("unexpected character %q", string(c))
				l.i++
			}
		}
	}

	// End of input: close the final logical line, then unwind indentation.
	if !l.atLineStart {
		l.emit(TNewline, "", l.col)
	}
	for len(l.indent) > 1 {
		l.indent = l.indent[:len(l.indent)-1]
		l.emit(TDedent, "", l.col)
	}
	l.emit(TEOF, "", l.col)
	return l.toks
}

// handleLineStart measures leading indentation of a logical line and emits
// INDENT/DEDENT tokens. Returns true if the line was blank or a comment
// (and was consumed), in which case layout is untouched.
func (l *Lexer) handleLineStart() bool {
	indent := 0
	start := l.i
	for l.i < len(l.src) {
		switch l.src[l.i] {
		case ' ':
			indent++
		case '\t':
			indent += 8 - (indent % 8)
		default:
			goto measured
		}
		l.i++
	}
measured:
	l.col = indent + 1

	// Blank line or comment-only line: skip without affecting layout.
	if l.i >= len(l.src) || l.src[l.i] == '\n' || l.src[l.i] == '#' || l.src[l.i] == '\r' {
		for l.i < len(l.src) && l.src[l.i] != '\n' {
			l.i++
		}
		if l.i < len(l.src) {
			l.i++ // consume '\n'
			l.line++
			l.col = 1
		}
		return true
	}

	top := l.indent[len(l.indent)-1]
	switch {
	case indent > top:
		l.indent = append(l.indent, indent)
		l.emit(TIndent, "", start+1)
	case indent < top:
		for len(l.indent) > 1 && indent < l.indent[len(l.indent)-1] {
			l.indent = l.indent[:len(l.indent)-1]
			l.emit(TDedent, "", l.col)
		}
		if l.indent[len(l.indent)-1] != indent {
			l.errf("unindent does not match any outer indentation level")
		}
	}
	l.atLineStart = false
	return false
}

func (l *Lexer) lexNumber() {
	start := l.i
	col := l.col
	for l.i < len(l.src) && (isDigit(l.src[l.i]) || l.src[l.i] == '_') {
		l.i++
		l.col++
	}
	// fractional part
	if l.peek() == '.' && isDigit(l.at(1)) {
		l.i++
		l.col++
		for l.i < len(l.src) && (isDigit(l.src[l.i]) || l.src[l.i] == '_') {
			l.i++
			l.col++
		}
	}
	l.emit(TNumber, l.src[start:l.i], col)
}

func (l *Lexer) lexName() {
	start := l.i
	col := l.col
	for l.i < len(l.src) && isIdentPart(l.src[l.i]) {
		l.i++
		l.col++
	}
	l.emit(TName, l.src[start:l.i], col)
}

// lexString scans a single- or double-quoted string and stores the decoded
// inner text (escapes processed). Triple-quoted strings are not yet
// supported.
func (l *Lexer) lexString() {
	quote := l.peek()
	col := l.col
	l.i++
	l.col++
	var b []byte
	for l.i < len(l.src) {
		c := l.src[l.i]
		if c == '\\' && l.i+1 < len(l.src) {
			next := l.src[l.i+1]
			switch next {
			case 'n':
				b = append(b, '\n')
			case 't':
				b = append(b, '\t')
			case 'r':
				b = append(b, '\r')
			case '\\':
				b = append(b, '\\')
			case '\'':
				b = append(b, '\'')
			case '"':
				b = append(b, '"')
			default:
				b = append(b, '\\', next)
			}
			l.i += 2
			l.col += 2
			continue
		}
		if c == quote {
			l.i++
			l.col++
			l.emit(TString, string(b), col)
			return
		}
		if c == '\n' {
			l.errf("unterminated string literal")
			return
		}
		b = append(b, c)
		l.i++
		l.col++
	}
	l.errf("unterminated string literal")
}

// lexFString scans an f-string (`f"..."` / `f'...'`). Backslash escapes are
// processed like a normal string, but `{` / `}` (including the `{{` / `}}`
// literal-brace doublings) pass through verbatim so the parser can split out
// the interpolated `{expr}` fields. The decoded inner text is stored under
// TFString.
func (l *Lexer) lexFString() {
	col := l.col
	l.i++ // consume the f / F prefix
	l.col++
	quote := l.peek()
	l.i++
	l.col++
	var b []byte
	for l.i < len(l.src) {
		c := l.src[l.i]
		if c == '\\' && l.i+1 < len(l.src) {
			next := l.src[l.i+1]
			switch next {
			case 'n':
				b = append(b, '\n')
			case 't':
				b = append(b, '\t')
			case 'r':
				b = append(b, '\r')
			case '\\':
				b = append(b, '\\')
			case '\'':
				b = append(b, '\'')
			case '"':
				b = append(b, '"')
			default:
				b = append(b, '\\', next)
			}
			l.i += 2
			l.col += 2
			continue
		}
		if c == quote {
			l.i++
			l.col++
			l.emit(TFString, string(b), col)
			return
		}
		if c == '\n' {
			l.errf("unterminated string literal")
			return
		}
		b = append(b, c)
		l.i++
		l.col++
	}
	l.errf("unterminated string literal")
}

func (l *Lexer) lexOp() bool {
	for _, op := range pyOps {
		if l.i+len(op) <= len(l.src) && l.src[l.i:l.i+len(op)] == op {
			switch op {
			case "(", "[", "{":
				l.paren++
			case ")", "]", "}":
				if l.paren > 0 {
					l.paren--
				}
			}
			l.emit(TOp, op, l.col)
			l.i += len(op)
			l.col += len(op)
			return true
		}
	}
	return false
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentPart(c byte) bool { return isIdentStart(c) || isDigit(c) }
