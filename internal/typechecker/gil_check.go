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

package typechecker

import (
	"fmt"
	"strings"

	"zinc/internal/parser"
)

// GIL-dependent libraries — known to have issues with free-threaded Python.
// Status as of Python 3.13+.
var gilDependentLibs = map[string]string{
	"numba":       "Numba JIT relies on GIL internals — not yet free-thread safe",
	"pandas":      "pandas has partial free-threading support — some operations not thread-safe",
	"tkinter":     "tkinter is not thread-safe — use from main thread only",
	"sqlite3":     "sqlite3 connections are not thread-safe — use one per thread",
	"multiprocessing": "multiprocessing uses fork — prefer threading with free-threaded Python",
}

// Libraries with verified free-threading support.
var freeThreadSafeLibs = map[string]bool{
	"numpy":      true, // Thread-safe since 2.1+
	"polars":     true, // Rust-based, never depended on GIL
	"orjson":     true, // Rust-based
	"requests":   true, // I/O-bound, benefits from free-threading
	"httpx":      true, // I/O-bound
	"duckdb":     true, // C++ engine, own threading
	"sqlalchemy": true, // Thread-safe connection pooling (2.0+)
	"json":       true, // stdlib, safe
	"csv":        true, // stdlib, safe
	"pathlib":    true, // stdlib, safe
	"os":         true, // stdlib, safe
	"sys":        true, // stdlib, safe
}

// CheckGILDependencies scans imports and warns about GIL-dependent libraries.
// Returns warnings (not errors — code still transpiles, just warns).
func CheckGILDependencies(prog *parser.Program) []string {
	var warnings []string
	for _, imp := range prog.Imports {
		// Extract the base module name
		modName := imp.Path
		if strings.HasPrefix(modName, "from:") {
			parts := strings.SplitN(modName, ":", 3)
			if len(parts) >= 2 {
				modName = parts[1]
			}
		}
		// Get root module (e.g., "pandas" from "pandas.core")
		root := strings.SplitN(modName, ".", 2)[0]

		if warning, found := gilDependentLibs[root]; found {
			warnings = append(warnings,
				fmt.Sprintf("warning: import %q — %s", root, warning))
		}
	}
	return warnings
}
