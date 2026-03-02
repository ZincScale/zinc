package project

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// GrowlerMod represents a parsed growler.mod project manifest.
type GrowlerMod struct {
	Module  string // module name, e.g. "myapp"
	Version string // growler version, e.g. "0.1"
}

// FindMod walks up from startDir looking for a growler.mod file.
// Returns the path to growler.mod and the project root directory.
func FindMod(startDir string) (modPath string, rootDir string, err error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", err
	}
	for {
		candidate := filepath.Join(dir, "growler.mod")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", "", errors.New("growler.mod not found (searched from " + startDir + ")")
}

// ParseMod reads and parses a growler.mod file.
// Format (one directive per line):
//
//	module <name>
//	growler <version>
func ParseMod(path string) (*GrowlerMod, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mod := &GrowlerMod{}
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
			case "growler":
				mod.Version = parts[1]
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if mod.Module == "" {
		return nil, errors.New("growler.mod: missing 'module' directive")
	}
	return mod, nil
}
