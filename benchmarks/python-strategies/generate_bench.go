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

//go:build ignore

// generate_bench.go produces benchmark Python files from Zinc source
// using all three collection strategies, for performance comparison.
package main

import (
	"fmt"
	"os"

	"zinc/internal/codegen_python"
	"zinc/internal/lexer"
	"zinc/internal/parser"
)

func transpile(src string, strategy codegen_python.CollectionStrategy) string {
	tokens := lexer.New(src).Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "Parse errors: %v\n", p.Errors)
		os.Exit(1)
	}
	g := codegen_python.New()
	g.Strategy = strategy
	return g.Generate(prog)
}

func main() {
	// The Zinc source that exercises collection chains
	src := `
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    result := nums.Where(x => x > 5).Select(x => x * 2).ToList()
    print(result)
}
`
	fmt.Println("=== Comprehension ===")
	fmt.Println(transpile(src, codegen_python.StrategyComprehension))
	fmt.Println("=== NumPy ===")
	fmt.Println(transpile(src, codegen_python.StrategyNumPy))
	fmt.Println("=== Numba ===")
	fmt.Println(transpile(src, codegen_python.StrategyNumba))
}
