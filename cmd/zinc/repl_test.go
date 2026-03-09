package main

import "testing"

func TestCountBraceDepth(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{`fn main() {`, 1},
		{`}`, -1},
		{`if (x > 0) { return 1 }`, 0},
		// Braces inside strings should be ignored
		{`var s = "hello {world}"`, 0},
		{`var s = "a{b}c"`, 0},
		{`print("{name}")`, 0},
		// Braces inside raw strings
		{"var s = `raw {brace}`", 0},
		// Braces after line comment
		{`// this { doesn't count`, 0},
		{`var x = 1 // trailing { comment`, 0},
		// Escaped quote in string
		{`var s = "escaped \" { quote"`, 0},
		// Normal nesting
		{`class Dog {`, 1},
		{`fn foo() { if (true) {`, 2},
		{`} }`, -2},
	}

	for _, tt := range tests {
		got := countBraceDepth(tt.line)
		if got != tt.want {
			t.Errorf("countBraceDepth(%q) = %d, want %d", tt.line, got, tt.want)
		}
	}
}

func TestIsTopLevelDecl(t *testing.T) {
	trueCases := []string{
		"fn main() { }",
		"pub fn greet() { }",
		"class Dog { }",
		"interface Speaker { }",
		"enum Color { Red, Blue }",
		"const PI = 3.14",
		`import "fmt"`,
	}
	for _, s := range trueCases {
		if !isTopLevelDecl(s) {
			t.Errorf("isTopLevelDecl(%q) = false, want true", s)
		}
	}

	falseCases := []string{
		"var x = 1",
		"print(42)",
		"1 + 2",
		"x = 5",
	}
	for _, s := range falseCases {
		if isTopLevelDecl(s) {
			t.Errorf("isTopLevelDecl(%q) = true, want false", s)
		}
	}
}

func TestIsVarDecl(t *testing.T) {
	if !isVarDecl("var x = 1") {
		t.Error("expected var x = 1 to be a var decl")
	}
	if isVarDecl("print(x)") {
		t.Error("expected print(x) to not be a var decl")
	}
}

func TestIsBareExpression(t *testing.T) {
	trueCases := []string{
		"1 + 2",
		`"hello"`,
		"42",
		"x",
		"foo()",
		"dog.name",
		"[1, 2, 3]",
		`{"a": 1}`,
		"true",
	}
	for _, s := range trueCases {
		if !isBareExpression(s) {
			t.Errorf("isBareExpression(%q) = false, want true", s)
		}
	}

	falseCases := []string{
		"var x = 1",
		"if (true) { }",
		"for (var i = 0; i < 5; i += 1) { }",
		"while (true) { }",
		"return 42",
		"match x { }",
		"print(42)",
		"x = 5",
		"x += 1",
		"fn foo() { }",
		"class Dog { }",
		"const PI = 3.14",
	}
	for _, s := range falseCases {
		if isBareExpression(s) {
			t.Errorf("isBareExpression(%q) = true, want false", s)
		}
	}
}

func TestExtractVarName(t *testing.T) {
	tests := []struct {
		decl string
		want string
	}{
		{"var x = 1", "x"},
		{"var name: String = \"hello\"", "name"},
		{"var count: Int", "count"},
		{"  var spaced = 42  ", "spaced"},
		{"var (a, b) = divide(10, 2)", ""},
		{"print(x)", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractVarName(tt.decl)
		if got != tt.want {
			t.Errorf("extractVarName(%q) = %q, want %q", tt.decl, got, tt.want)
		}
	}
}
