package lexer

import (
	"fmt"
	"strings"
)

// Lexer tokenizes Zinc source code.
type Lexer struct {
	src     []rune
	pos     int // current position
	line    int
	col     int
	Errors  []string
}

// New creates a Lexer from source text.
func New(src string) *Lexer {
	return &Lexer{src: []rune(src), line: 1, col: 1}
}

func (l *Lexer) peek() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *Lexer) peekAt(offset int) rune {
	idx := l.pos + offset
	if idx >= len(l.src) {
		return 0
	}
	return l.src[idx]
}

func (l *Lexer) advance() rune {
	if l.pos >= len(l.src) {
		return 0
	}
	ch := l.src[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *Lexer) skipWhitespaceAndComments() {
	for {
		ch := l.peek()
		switch {
		case ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n':
			l.advance()
		case ch == '/' && l.peekAt(1) == '/':
			// line comment
			for l.peek() != '\n' && l.peek() != 0 {
				l.advance()
			}
		case ch == '/' && l.peekAt(1) == '*':
			// block comment
			l.advance()
			l.advance()
			for !(l.peek() == '*' && l.peekAt(1) == '/') && l.peek() != 0 {
				l.advance()
			}
			if l.peek() != 0 {
				l.advance() // *
				l.advance() // /
			}
		default:
			return
		}
	}
}

func (l *Lexer) makeToken(t TokenType, lit string, line, col int) Token {
	return Token{Type: t, Literal: lit, Line: line, Col: col}
}

// NextToken returns the next token from the source.
func (l *Lexer) NextToken() Token {
	l.skipWhitespaceAndComments()

	line, col := l.line, l.col
	ch := l.peek()

	if ch == 0 {
		return l.makeToken(TOKEN_EOF, "", line, col)
	}

	// String literal
	if ch == '"' {
		return l.readString(line, col)
	}

	// Raw string literal
	if ch == '`' {
		return l.readRawString(line, col)
	}

	// Number literal
	if isDigit(ch) {
		return l.readNumber(line, col)
	}

	// Identifier or keyword
	if isLetter(ch) {
		return l.readIdent(line, col)
	}

	l.advance()

	switch ch {
	case '(':
		return l.makeToken(TOKEN_LPAREN, "(", line, col)
	case ')':
		return l.makeToken(TOKEN_RPAREN, ")", line, col)
	case '{':
		return l.makeToken(TOKEN_LBRACE, "{", line, col)
	case '}':
		return l.makeToken(TOKEN_RBRACE, "}", line, col)
	case '[':
		return l.makeToken(TOKEN_LBRACKET, "[", line, col)
	case ']':
		return l.makeToken(TOKEN_RBRACKET, "]", line, col)
	case ',':
		return l.makeToken(TOKEN_COMMA, ",", line, col)
	case '.':
		return l.makeToken(TOKEN_DOT, ".", line, col)
	case ':':
		return l.makeToken(TOKEN_COLON, ":", line, col)
	case ';':
		return l.makeToken(TOKEN_SEMICOLON, ";", line, col)
	case '?':
		if l.peek() == '.' {
			l.advance()
			return l.makeToken(TOKEN_QUESTION_DOT, "?.", line, col)
		}
		if l.peek() == '?' {
			l.advance()
			return l.makeToken(TOKEN_QUESTION_QUESTION, "??", line, col)
		}
		return l.makeToken(TOKEN_QUESTION, "?", line, col)
	case '@':
		return l.makeToken(TOKEN_AT, "@", line, col)
	case '%':
		return l.makeToken(TOKEN_PERCENT, "%", line, col)
	case '+':
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(TOKEN_PLUS_EQ, "+=", line, col)
		}
		return l.makeToken(TOKEN_PLUS, "+", line, col)
	case '-':
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(TOKEN_MINUS_EQ, "-=", line, col)
		}
		return l.makeToken(TOKEN_MINUS, "-", line, col)
	case '*':
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(TOKEN_STAR_EQ, "*=", line, col)
		}
		return l.makeToken(TOKEN_STAR, "*", line, col)
	case '/':
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(TOKEN_SLASH_EQ, "/=", line, col)
		}
		return l.makeToken(TOKEN_SLASH, "/", line, col)
	case '!':
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(TOKEN_NEQ, "!=", line, col)
		}
		return l.makeToken(TOKEN_BANG, "!", line, col)
	case '=':
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(TOKEN_EQ, "==", line, col)
		}
		if l.peek() == '>' {
			l.advance()
			return l.makeToken(TOKEN_FAT_ARROW, "=>", line, col)
		}
		return l.makeToken(TOKEN_ASSIGN, "=", line, col)
	case '<':
		if l.peek() == '-' {
			l.advance()
			return l.makeToken(TOKEN_ARROW, "<-", line, col)
		}
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(TOKEN_LTE, "<=", line, col)
		}
		return l.makeToken(TOKEN_LT, "<", line, col)
	case '>':
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(TOKEN_GTE, ">=", line, col)
		}
		return l.makeToken(TOKEN_GT, ">", line, col)
	case '&':
		if l.peek() == '&' {
			l.advance()
			return l.makeToken(TOKEN_AMP_AMP, "&&", line, col)
		}
		l.Errors = append(l.Errors, fmt.Sprintf("%d:%d: unexpected '&'", line, col))
		return l.makeToken(TOKEN_ILLEGAL, "&", line, col)
	case '|':
		if l.peek() == '|' {
			l.advance()
			return l.makeToken(TOKEN_PIPE_PIPE, "||", line, col)
		}
		l.Errors = append(l.Errors, fmt.Sprintf("%d:%d: unexpected '|'", line, col))
		return l.makeToken(TOKEN_ILLEGAL, "|", line, col)
	}

	l.Errors = append(l.Errors, fmt.Sprintf("%d:%d: unexpected character %q", line, col, ch))
	return l.makeToken(TOKEN_ILLEGAL, string(ch), line, col)
}

func (l *Lexer) readString(line, col int) Token {
	l.advance() // consume opening "
	var sb strings.Builder
	hasInterp := false
	for {
		ch := l.peek()
		if ch == 0 || ch == '\n' {
			l.Errors = append(l.Errors, fmt.Sprintf("%d:%d: unterminated string", line, col))
			break
		}
		if ch == '"' {
			l.advance()
			break
		}
		if ch == '{' {
			hasInterp = true
			sb.WriteRune('{')
			l.advance()
			continue
		}
		if ch == '}' {
			sb.WriteRune('}')
			l.advance()
			continue
		}
		if ch == '\\' {
			l.advance()
			esc := l.advance()
			switch esc {
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case '"':
				sb.WriteRune('"')
			case '\\':
				sb.WriteRune('\\')
			case 'r':
				sb.WriteRune('\r')
			default:
				sb.WriteRune('\\')
				sb.WriteRune(esc)
			}
			continue
		}
		sb.WriteRune(l.advance())
	}
	if hasInterp {
		return l.makeToken(TOKEN_INTERP_STRING, sb.String(), line, col)
	}
	return l.makeToken(TOKEN_STRING_LIT, sb.String(), line, col)
}

func (l *Lexer) readRawString(line, col int) Token {
	l.advance() // consume opening `
	var sb strings.Builder
	for {
		ch := l.peek()
		if ch == 0 {
			l.Errors = append(l.Errors, fmt.Sprintf("%d:%d: unterminated raw string", line, col))
			break
		}
		if ch == '`' {
			l.advance()
			break
		}
		sb.WriteRune(l.advance())
	}
	return l.makeToken(TOKEN_RAW_STRING, sb.String(), line, col)
}

func (l *Lexer) readNumber(line, col int) Token {
	start := l.pos
	isFloat := false
	for isDigit(l.peek()) {
		l.advance()
	}
	if l.peek() == '.' && isDigit(l.peekAt(1)) {
		isFloat = true
		l.advance() // consume .
		for isDigit(l.peek()) {
			l.advance()
		}
	}
	lit := string(l.src[start:l.pos])
	if isFloat {
		return l.makeToken(TOKEN_FLOAT_LIT, lit, line, col)
	}
	return l.makeToken(TOKEN_INT_LIT, lit, line, col)
}

func (l *Lexer) readIdent(line, col int) Token {
	start := l.pos
	for isLetter(l.peek()) || isDigit(l.peek()) {
		l.advance()
	}
	lit := string(l.src[start:l.pos])
	tt := LookupIdent(lit)
	// Normalize bool literals to TOKEN_BOOL_LIT
	if tt == TOKEN_TRUE || tt == TOKEN_FALSE {
		return l.makeToken(TOKEN_BOOL_LIT, lit, line, col)
	}
	return l.makeToken(tt, lit, line, col)
}

// Tokenize returns all tokens (including EOF).
func (l *Lexer) Tokenize() []Token {
	var tokens []Token
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == TOKEN_EOF {
			break
		}
	}
	return tokens
}

func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}

func isLetter(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}
