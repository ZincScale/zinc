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

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSpikesMatchCPython is the contract test for the Python front-end:
// every testdata program must produce identical output whether run through
// CPython or through the full Python → Go pipeline. Skips when go/python3
// aren't on PATH.
func TestSpikesMatchCPython(t *testing.T) {
	requireTool(t, "go")
	requireTool(t, "python3")

	files, err := filepath.Glob("testdata/*.py")
	if err != nil || len(files) == 0 {
		t.Fatalf("no testdata/*.py files found")
	}

	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			want, err := exec.Command("python3", f).CombinedOutput()
			if err != nil {
				t.Fatalf("python3 %s failed: %v\n%s", f, err, want)
			}

			outFiles, meta, cerr := compile(f)
			if cerr != nil {
				t.Fatalf("compile %s: %v", f, cerr)
			}
			got, rerr := runOutput(outFiles, meta)
			if rerr != nil {
				t.Fatalf("go run for %s failed: %v\n%s", f, rerr, got)
			}

			if got != string(want) {
				t.Errorf("output mismatch for %s\n--- cpython ---\n%s\n--- zincpy ---\n%s", f, want, got)
			}
		})
	}
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not on PATH", name)
	}
}
