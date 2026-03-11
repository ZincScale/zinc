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

package project

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ZincMod represents a parsed zinc.mod project manifest.
type ZincMod struct {
	Module  string // module name, e.g. "myapp"
	Version string // zinc version, e.g. "0.1"
}

// FindMod walks up from startDir looking for a zinc.mod file.
// Returns the path to zinc.mod and the project root directory.
func FindMod(startDir string) (modPath string, rootDir string, err error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", err
	}
	for {
		candidate := filepath.Join(dir, "zinc.mod")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", "", errors.New("zinc.mod not found (searched from " + startDir + ")")
}

// ParseMod reads and parses a zinc.mod file.
// Format (one directive per line):
//
//	module <name>
//	zinc <version>
func ParseMod(path string) (*ZincMod, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mod := &ZincMod{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			switch parts[0] {
			case "module":
				mod.Module = parts[1]
			case "zinc":
				mod.Version = parts[1]
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if mod.Module == "" {
		return nil, errors.New("zinc.mod: missing 'module' directive")
	}
	return mod, nil
}
