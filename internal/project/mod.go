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
