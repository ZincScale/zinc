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

package main

import "testing"

func TestCountBraceDepth(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{`main() {`, 1},
		{`}`, -1},
		{`if x > 0 { return 1 }`, 0},
		// Braces inside strings should be ignored
		{`s := "hello {world}"`, 0},
		{`s := "a{b}c"`, 0},
		{`print("{name}")`, 0},
		// Braces inside raw strings
		{"s := `raw {brace}`", 0},
		// Braces after line comment
		{`// this { doesn't count`, 0},
		{`x := 1 // trailing { comment`, 0},
		// Escaped quote in string
		{`s := "escaped \" { quote"`, 0},
		// Normal nesting
		{`Dog {`, 1},
		{`foo() { if true {`, 2},
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
		"main() { }",
		"pub greet() { }",
		"Int add(Int a, Int b) { return a + b }",
		"String greet(String name) { return name }",
		"Dog { }",
		"Dog : Animal { }",
		"Puppy { }",
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
		"x := 1",
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
	trueCases := []string{
		"x := 1",
		"name := \"hello\"",
		`String name = "hello"`,
		"Int x = 42",
		"String? name = null",
	}
	for _, s := range trueCases {
		if !isVarDecl(s) {
			t.Errorf("isVarDecl(%q) = false, want true", s)
		}
	}

	falseCases := []string{
		"print(x)",
		"1 + 2",
		"x = 5",
	}
	for _, s := range falseCases {
		if isVarDecl(s) {
			t.Errorf("isVarDecl(%q) = true, want false", s)
		}
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
		"x := 1",
		"if true { }",
		"for i in items { }",
		"while true { }",
		"return 42",
		"match x { }",
		"print(42)",
		"x = 5",
		"x += 1",
		"main() { }",
		"Dog { }",
		"const PI = 3.14",
		`String name = "hello"`,
		"Int x = 42",
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
		{"x := 1", "x"},
		{"name := \"hello\"", "name"},
		{"  spaced := 42  ", "spaced"},
		{"(a, b) := divide(10, 2)", ""},
		{`String name = "hello"`, "name"},
		{"Int x = 42", "x"},
		{"String? name = null", "name"},
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
