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

import (
	"testing"
)

func tokenize(src string) []Token {
	l := New(src)
	return l.Tokenize()
}

func types(tokens []Token) []TokenType {
	var out []TokenType
	for _, t := range tokens {
		out = append(out, t.Type)
	}
	return out
}

func TestKeywords(t *testing.T) {
	src := "fn class interface var return if else for while pub static"
	toks := tokenize(src)
	expected := []TokenType{
		TOKEN_FN, TOKEN_CLASS, TOKEN_INTERFACE, TOKEN_VAR,
		TOKEN_RETURN, TOKEN_IF, TOKEN_ELSE, TOKEN_FOR,
		TOKEN_WHILE, TOKEN_PUB, TOKEN_STATIC, TOKEN_EOF,
	}
	if len(toks) != len(expected) {
		t.Fatalf("expected %d tokens, got %d", len(expected), len(toks))
	}
	for i, tt := range expected {
		if toks[i].Type != tt {
			t.Errorf("token[%d]: expected %s, got %s", i, tt, toks[i].Type)
		}
	}
}

func TestIntLiteral(t *testing.T) {
	toks := tokenize("42")
	if toks[0].Type != TOKEN_INT_LIT || toks[0].Literal != "42" {
		t.Errorf("expected INT_LIT 42, got %s %q", toks[0].Type, toks[0].Literal)
	}
}

func TestFloatLiteral(t *testing.T) {
	toks := tokenize("3.14")
	if toks[0].Type != TOKEN_FLOAT_LIT || toks[0].Literal != "3.14" {
		t.Errorf("expected FLOAT_LIT 3.14, got %s %q", toks[0].Type, toks[0].Literal)
	}
}

func TestStringLiteral(t *testing.T) {
	toks := tokenize(`"hello world"`)
	if toks[0].Type != TOKEN_STRING_LIT || toks[0].Literal != "hello world" {
		t.Errorf("expected STRING_LIT, got %s %q", toks[0].Type, toks[0].Literal)
	}
}

func TestStringEscapes(t *testing.T) {
	toks := tokenize(`"a\nb\tc"`)
	if toks[0].Literal != "a\nb\tc" {
		t.Errorf("expected escaped string, got %q", toks[0].Literal)
	}
}

func TestBoolLiterals(t *testing.T) {
	toks := tokenize("true false")
	if toks[0].Type != TOKEN_BOOL_LIT || toks[0].Literal != "true" {
		t.Errorf("expected BOOL_LIT true")
	}
	if toks[1].Type != TOKEN_BOOL_LIT || toks[1].Literal != "false" {
		t.Errorf("expected BOOL_LIT false")
	}
}

func TestOperators(t *testing.T) {
	cases := []struct {
		src string
		tt  TokenType
		lit string
	}{
		{"+", TOKEN_PLUS, "+"},
		{"-", TOKEN_MINUS, "-"},
		{"*", TOKEN_STAR, "*"},
		{"/", TOKEN_SLASH, "/"},
		{"%", TOKEN_PERCENT, "%"},
		{"==", TOKEN_EQ, "=="},
		{"!=", TOKEN_NEQ, "!="},
		{"<", TOKEN_LT, "<"},
		{"<=", TOKEN_LTE, "<="},
		{">", TOKEN_GT, ">"},
		{">=", TOKEN_GTE, ">="},
		{"&&", TOKEN_AMP_AMP, "&&"},
		{"||", TOKEN_PIPE_PIPE, "||"},
		{"+=", TOKEN_PLUS_EQ, "+="},
		{"-=", TOKEN_MINUS_EQ, "-="},
		{"*=", TOKEN_STAR_EQ, "*="},
		{"/=", TOKEN_SLASH_EQ, "/="},
	}
	for _, c := range cases {
		toks := tokenize(c.src)
		if toks[0].Type != c.tt {
			t.Errorf("src=%q: expected %s, got %s", c.src, c.tt, toks[0].Type)
		}
		if toks[0].Literal != c.lit {
			t.Errorf("src=%q: expected literal %q, got %q", c.src, c.lit, toks[0].Literal)
		}
	}
}

func TestLineComment(t *testing.T) {
	toks := tokenize("42 // this is a comment\n99")
	if toks[0].Literal != "42" || toks[1].Literal != "99" {
		t.Errorf("comments not skipped correctly")
	}
}

func TestBlockComment(t *testing.T) {
	toks := tokenize("42 /* block */ 99")
	if toks[0].Literal != "42" || toks[1].Literal != "99" {
		t.Errorf("block comments not skipped correctly")
	}
}

func TestLineNumbers(t *testing.T) {
	toks := tokenize("a\nb\nc")
	if toks[0].Line != 1 {
		t.Errorf("expected line 1, got %d", toks[0].Line)
	}
	if toks[1].Line != 2 {
		t.Errorf("expected line 2, got %d", toks[1].Line)
	}
	if toks[2].Line != 3 {
		t.Errorf("expected line 3, got %d", toks[2].Line)
	}
}

func TestIdentifier(t *testing.T) {
	toks := tokenize("myVar _foo bar123")
	for i, lit := range []string{"myVar", "_foo", "bar123"} {
		if toks[i].Type != TOKEN_IDENT || toks[i].Literal != lit {
			t.Errorf("expected IDENT %q, got %s %q", lit, toks[i].Type, toks[i].Literal)
		}
	}
}

func TestPackageKeyword(t *testing.T) {
	toks := tokenize(`package "myapp/utils"`)
	if toks[0].Type != TOKEN_PACKAGE {
		t.Errorf("expected TOKEN_PACKAGE, got %s", toks[0].Type)
	}
	if toks[1].Type != TOKEN_STRING_LIT || toks[1].Literal != "myapp/utils" {
		t.Errorf("expected STRING_LIT 'myapp/utils', got %s %q", toks[1].Type, toks[1].Literal)
	}
}

func TestPackageKeywordEnum(t *testing.T) {
	toks := tokenize("package enum match case")
	expected := []TokenType{TOKEN_PACKAGE, TOKEN_ENUM, TOKEN_MATCH, TOKEN_CASE, TOKEN_EOF}
	for i, tt := range expected {
		if toks[i].Type != tt {
			t.Errorf("token[%d]: expected %s, got %s", i, tt, toks[i].Type)
		}
	}
}
