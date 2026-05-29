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

import (
	"strings"
	"testing"
)

// TestTypingContract checks the type-checker-clean contract: a variable that
// changes type on reassignment is rejected, while type-consistent code parses.
func TestTypingContract(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr string // substring expected in the error; "" means must succeed
	}{
		{"int to float", "x = 5\nx = 3.5\n", "type-consistent"},
		{"int to str", "x = 5\nx = \"hi\"\n", "type-consistent"},
		{"int stays int", "x = 5\nx = 6\nx = x + 1\n", ""},
		{"float stays float", "x = 1.0\nx = x / 2\n", ""},
		{"unknown rhs allowed", "x = 5\nx = foo()\n", ""},
		{"shadow across funcs ok", "def f(n: int) -> int:\n    return n\nx = 1.5\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, errs := Parse(tc.src)
			if tc.wantErr == "" {
				if len(errs) > 0 {
					t.Fatalf("expected success, got errors: %v", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("expected error containing %q, got none", tc.wantErr)
			}
			if !strings.Contains(strings.Join(errs, "\n"), tc.wantErr) {
				t.Fatalf("error %v does not contain %q", errs, tc.wantErr)
			}
		})
	}
}
