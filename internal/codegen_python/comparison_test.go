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

package codegen_python

import (
	"fmt"
	"strings"
	"testing"

	gocodegen "zinc/internal/codegen"
	"zinc/internal/lexer"
	"zinc/internal/parser"
)

// transpileGo generates Go output from Zinc source.
func transpileGo(src string) string {
	tokens := lexer.New(src).Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		return "PARSE ERROR: " + strings.Join(p.Errors, "; ")
	}
	g := gocodegen.New()
	return g.Generate(prog)
}

// transpilePython generates Python output from Zinc source.
func transpilePython(src string) string {
	return transpile(src) // uses the local transpile helper
}

func TestComparisonClass(t *testing.T) {
	src := `
Dog {
    String name
    Int age = 0

    new(String name, Int age = 0) {
        this.name = name
        this.age = age
    }

    pub String bark() {
        return "Woof!"
    }
}

main() {
    d := Dog(name: "Rex", age: 5)
    print(d.bark())
}
`
	goOut := transpileGo(src)
	pyOut := transpilePython(src)

	fmt.Println("=== Class Comparison ===")
	fmt.Println()
	fmt.Println("--- Go ---")
	fmt.Println(goOut)
	fmt.Println("--- Python ---")
	fmt.Println(pyOut)
}

// TestCodegenLineCount compares total codegen source complexity.
func TestCodegenLineCount(t *testing.T) {
	fmt.Println("\n=== Codegen Source Complexity ===")
	fmt.Println()
	fmt.Println("Go backend:")
	fmt.Println("  codegen.go:      ~2980 lines")
	fmt.Println("  collections.go:  ~740 lines")
	fmt.Println("  registry.go:     ~100 lines")
	fmt.Println("  gotypes.go:      ~200 lines")
	fmt.Println("  Total:           ~4020 lines")
	fmt.Println()
	fmt.Println("Python backend (prototype):")
	fmt.Println("  codegen.go:      ~370 lines")
	fmt.Println("  Total:           ~370 lines")
	fmt.Println()
	fmt.Println("Key simplifications:")
	fmt.Println("  - No auto-generated interfaces (Python is duck-typed)")
	fmt.Println("  - No pointer/value receiver distinction")
	fmt.Println("  - No interfaceVars/classVars tracking")
	fmt.Println("  - No failable detection (exceptions propagate naturally)")
	fmt.Println("  - No getter/setter generation")
	fmt.Println("  - No type assertion codegen")
	fmt.Println("  - Error handling: try/except maps directly to or {}")
	fmt.Println("  - Classes: direct 1:1 mapping")
}
