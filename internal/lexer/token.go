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

package lexer

// TokenType identifies the type of a lexical token.
type TokenType int

const (
	// Literals
	TOKEN_INT_LIT      TokenType = iota
	TOKEN_FLOAT_LIT              // 1.5
	TOKEN_STRING_LIT             // "hello"
	TOKEN_INTERP_STRING          // "Hello, {name}!"
	TOKEN_BOOL_LIT               // true / false
	TOKEN_NULL                   // null
	TOKEN_IDENT                  // identifiers

	// Keywords
	TOKEN_CLASS
	TOKEN_INTERFACE
	TOKEN_CONSTRUCT
	TOKEN_NEW
	TOKEN_FN
	TOKEN_RETURN
	TOKEN_IF
	TOKEN_ELSE
	TOKEN_FOR
	TOKEN_WHILE
	TOKEN_BREAK
	TOKEN_CONTINUE
	TOKEN_GO
	TOKEN_OR
	TOKEN_PRINT
	TOKEN_VAR
	TOKEN_PUB
	TOKEN_STATIC
	TOKEN_SUPER
	TOKEN_THIS
	TOKEN_IMPORT
	TOKEN_AS
	TOKEN_IN
	TOKEN_TRUE
	TOKEN_FALSE
	TOKEN_ENUM
	TOKEN_MATCH
	TOKEN_CASE
	TOKEN_PACKAGE
	TOKEN_CONST
	TOKEN_DEFER
	TOKEN_IS
	TOKEN_WITH
	TOKEN_DATA
	TOKEN_SPAWN
	TOKEN_USE
	TOKEN_READONLY // read (read-only field)
	TOKEN_OVERRIDE // override (method override)
	TOKEN_END      // end (block closer)
	TOKEN_TRY      // try
	TOKEN_CATCH    // catch
	TOKEN_RAISE    // raise (throw)
	TOKEN_NOT   // not (boolean negation)
	TOKEN_AND   // and (boolean and)
	TOKEN_FROM  // from (import support)
	TOKEN_NONE     // none (Python-style null)
	TOKEN_PARALLEL // parallel (parallel for)
	TOKEN_INIT     // init (constructor-set immutable field)

	// Symbols
	TOKEN_LPAREN    // (
	TOKEN_RPAREN    // )
	TOKEN_LBRACE    // {
	TOKEN_RBRACE    // }
	TOKEN_LBRACKET  // [
	TOKEN_RBRACKET  // ]
	TOKEN_COMMA     // ,
	TOKEN_DOT       // .
	TOKEN_COLON     // :
	TOKEN_SEMICOLON // ;
	TOKEN_ASSIGN    // =
	TOKEN_PLUS      // +
	TOKEN_MINUS     // -
	TOKEN_STAR      // *
	TOKEN_SLASH     // /
	TOKEN_PERCENT   // %
	TOKEN_BANG      // !
	TOKEN_AMP_AMP   // &&
	TOKEN_PIPE_PIPE // ||
	TOKEN_EQ        // ==
	TOKEN_NEQ       // !=
	TOKEN_REF_EQ    // ===
	TOKEN_REF_NEQ   // !==
	TOKEN_LT        // <
	TOKEN_LTE       // <=
	TOKEN_GT        // >
	TOKEN_GTE       // >=
	TOKEN_PLUS_EQ    // +=
	TOKEN_MINUS_EQ   // -=
	TOKEN_STAR_EQ    // *=
	TOKEN_SLASH_EQ   // /=
	TOKEN_ARROW              // ->
	TOKEN_QUESTION           // ?
	TOKEN_QUESTION_DOT       // ?.
	TOKEN_QUESTION_QUESTION  // ??
	TOKEN_RAW_STRING         // `raw string`
	TOKEN_AT                 // @
	TOKEN_DOTDOT             // ..
	TOKEN_DOTDOTEQ           // ..=
	TOKEN_DOTDOTDOT          // ...
	TOKEN_COLONASSIGN        // :=
	TOKEN_STAR_STAR          // **

	TOKEN_EOF
	TOKEN_ILLEGAL
)

var tokenNames = map[TokenType]string{
	TOKEN_INT_LIT:       "INT",
	TOKEN_FLOAT_LIT:     "FLOAT",
	TOKEN_STRING_LIT:    "STRING",
	TOKEN_INTERP_STRING: "INTERP_STRING",
	TOKEN_BOOL_LIT:      "BOOL",
	TOKEN_NULL:       "null",
	TOKEN_IDENT:      "IDENT",

	TOKEN_CLASS:     "class",
	TOKEN_INTERFACE: "interface",
	TOKEN_CONSTRUCT: "construct",
	TOKEN_NEW:       "new",
	TOKEN_FN:        "fn",
	TOKEN_RETURN:    "return",
	TOKEN_IF:        "if",
	TOKEN_ELSE:      "else",
	TOKEN_FOR:       "for",
	TOKEN_WHILE:     "while",
	TOKEN_BREAK:     "break",
	TOKEN_CONTINUE:  "continue",
	TOKEN_GO:        "go",
	TOKEN_OR:        "or",
	TOKEN_PRINT:     "print",
	TOKEN_VAR:       "var",
	TOKEN_PUB:       "pub",
	TOKEN_STATIC:    "static",
	TOKEN_SUPER:     "super",
	TOKEN_THIS:      "this",
	TOKEN_IMPORT:    "import",
	TOKEN_AS:        "as",
	TOKEN_IN:        "in",
	TOKEN_TRUE:      "true",
	TOKEN_FALSE:     "false",
	TOKEN_ENUM:      "enum",
	TOKEN_MATCH:     "match",
	TOKEN_CASE:      "case",
	TOKEN_PACKAGE:   "package",
	TOKEN_CONST:     "const",
	TOKEN_DEFER:     "defer",
	TOKEN_IS:        "is",
	TOKEN_WITH:      "with",
	TOKEN_DATA:      "data",
	TOKEN_SPAWN:     "spawn",
	TOKEN_USE:       "use",
	TOKEN_READONLY:  "read",
	TOKEN_OVERRIDE:  "override",
	TOKEN_END:       "end",
	TOKEN_TRY:       "try",
	TOKEN_CATCH:     "catch",
	TOKEN_RAISE:     "raise",
	TOKEN_NOT:       "not",
	TOKEN_AND:       "and",
	TOKEN_FROM:      "from",
	TOKEN_NONE:      "none",
	TOKEN_PARALLEL:  "parallel",
	TOKEN_INIT:      "init",

	TOKEN_LPAREN:    "(",
	TOKEN_RPAREN:    ")",
	TOKEN_LBRACE:    "{",
	TOKEN_RBRACE:    "}",
	TOKEN_LBRACKET:  "[",
	TOKEN_RBRACKET:  "]",
	TOKEN_COMMA:     ",",
	TOKEN_DOT:       ".",
	TOKEN_COLON:     ":",
	TOKEN_SEMICOLON: ";",
	TOKEN_ASSIGN:    "=",
	TOKEN_PLUS:      "+",
	TOKEN_MINUS:     "-",
	TOKEN_STAR:      "*",
	TOKEN_SLASH:     "/",
	TOKEN_PERCENT:   "%",
	TOKEN_BANG:      "!",
	TOKEN_AMP_AMP:   "&&",
	TOKEN_PIPE_PIPE: "||",
	TOKEN_EQ:        "==",
	TOKEN_NEQ:       "!=",
	TOKEN_REF_EQ:    "===",
	TOKEN_REF_NEQ:   "!==",
	TOKEN_LT:        "<",
	TOKEN_LTE:       "<=",
	TOKEN_GT:        ">",
	TOKEN_GTE:       ">=",
	TOKEN_PLUS_EQ:   "+=",
	TOKEN_MINUS_EQ:  "-=",
	TOKEN_STAR_EQ:   "*=",
	TOKEN_SLASH_EQ:  "/=",
	TOKEN_ARROW:             "->",
	TOKEN_QUESTION:          "?",
	TOKEN_QUESTION_DOT:      "?.",
	TOKEN_QUESTION_QUESTION: "??",
	TOKEN_RAW_STRING:        "RAW_STRING",
	TOKEN_AT:                "@",
	TOKEN_DOTDOT:            "..",
	TOKEN_DOTDOTEQ:          "..=",
	TOKEN_DOTDOTDOT:         "...",
	TOKEN_COLONASSIGN:       ":=",
	TOKEN_STAR_STAR:         "**",

	TOKEN_EOF:     "EOF",
	TOKEN_ILLEGAL: "ILLEGAL",
}

func (t TokenType) String() string {
	if s, ok := tokenNames[t]; ok {
		return s
	}
	return "UNKNOWN"
}

// Token is a single lexical unit.
type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Col     int
}

// keywords maps reserved words to their token types.
var keywords = map[string]TokenType{
	"class":     TOKEN_CLASS,
	"interface": TOKEN_INTERFACE,
	"construct": TOKEN_CONSTRUCT,
	"new":       TOKEN_NEW,
	"fn":        TOKEN_FN,
	"return":    TOKEN_RETURN,
	"if":        TOKEN_IF,
	"else":      TOKEN_ELSE,
	"for":       TOKEN_FOR,
	"while":     TOKEN_WHILE,
	"break":     TOKEN_BREAK,
	"continue":  TOKEN_CONTINUE,
	"go":        TOKEN_GO,
	"or":        TOKEN_OR,
	"print":     TOKEN_PRINT,
	"var":       TOKEN_VAR,
	"pub":       TOKEN_PUB,
	"static":    TOKEN_STATIC,
	"super":     TOKEN_SUPER,
	"this":      TOKEN_THIS,
	"import":    TOKEN_IMPORT,
	"as":        TOKEN_AS,
	"in":        TOKEN_IN,
	"true":      TOKEN_TRUE,
	"false":     TOKEN_FALSE,
	"null":      TOKEN_NULL,
	"enum":      TOKEN_ENUM,
	"match":     TOKEN_MATCH,
	"case":      TOKEN_CASE,
	"package":   TOKEN_PACKAGE,
	"const":     TOKEN_CONST,
	"defer":     TOKEN_DEFER,
	"is":        TOKEN_IS,
	"with":      TOKEN_WITH,
	"data":      TOKEN_DATA,
	"spawn":     TOKEN_SPAWN,
	"use":       TOKEN_USE,
	"override":  TOKEN_OVERRIDE,
	"end":       TOKEN_END,
	"try":       TOKEN_TRY,
	"catch":     TOKEN_CATCH,
	"raise":     TOKEN_RAISE,
	"not":       TOKEN_NOT,
	"and":       TOKEN_AND,
	"from":      TOKEN_FROM,
	"none":      TOKEN_NONE,
	"parallel":  TOKEN_PARALLEL,
	"init":      TOKEN_INIT,
}

// LookupIdent returns the token type for a string — keyword or IDENT.
func LookupIdent(ident string) TokenType {
	if t, ok := keywords[ident]; ok {
		return t
	}
	return TOKEN_IDENT
}
