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

// Package config handles zinc.toml project configuration.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Dependency represents a package dependency.
type Dependency struct {
	Name    string // package name (NuGet package or Go module)
	Version string // version constraint
}

// Config represents a zinc.toml project configuration.
type Config struct {
	Name         string       // project name
	Version      string       // project version
	Target       string       // "csharp" (default) or "go"
	Optimize     bool         // AOT with full optimizations (default: true)
	Release      bool         // strip symbols for production (default: false, set by --release)
	NuGetSource  string       // custom NuGet source URL (default: nuget.org)
	Dependencies []Dependency // package dependencies
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig(name string) *Config {
	return &Config{
		Name:     name,
		Version:  "0.1.0",
		Target:   "csharp",
		Optimize: true,
	}
}

// Load reads zinc.toml from the given directory.
// Returns nil if no zinc.toml exists.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, "zinc.toml")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	cfg := DefaultConfig("")
	section := ""
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Section header
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}

		// Key = value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("zinc.toml:%d: invalid line: %s", lineNum, line)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"`)

		fullKey := key
		if section != "" {
			fullKey = section + "." + key
		}

		switch {
		case fullKey == "project.name":
			cfg.Name = val
		case fullKey == "project.version":
			cfg.Version = val
		case fullKey == "build.target":
			cfg.Target = val
		case fullKey == "build.optimize":
			cfg.Optimize = val == "true"
		case fullKey == "nuget.source":
			cfg.NuGetSource = val
		case section == "dependencies":
			cfg.Dependencies = append(cfg.Dependencies, Dependency{
				Name:    strings.Trim(key, `"`),
				Version: val,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading zinc.toml: %w", err)
	}

	return cfg, nil
}

// Generate creates a zinc.toml file content from a Config.
func Generate(cfg *Config) string {
	var b strings.Builder
	b.WriteString("[project]\n")
	b.WriteString(fmt.Sprintf("name = \"%s\"\n", cfg.Name))
	b.WriteString(fmt.Sprintf("version = \"%s\"\n", cfg.Version))
	b.WriteString("\n")
	b.WriteString("[build]\n")
	b.WriteString(fmt.Sprintf("target = \"%s\"\n", cfg.Target))
	b.WriteString(fmt.Sprintf("optimize = %t\n", cfg.Optimize))
	if len(cfg.Dependencies) > 0 {
		b.WriteString("\n")
		b.WriteString("[dependencies]\n")
		for _, dep := range cfg.Dependencies {
			b.WriteString(fmt.Sprintf("\"%s\" = \"%s\"\n", dep.Name, dep.Version))
		}
	}
	return b.String()
}

// AddDependency adds or updates a dependency in the config.
func (c *Config) AddDependency(name, version string) {
	for i, dep := range c.Dependencies {
		if strings.EqualFold(dep.Name, name) {
			c.Dependencies[i].Version = version
			return
		}
	}
	c.Dependencies = append(c.Dependencies, Dependency{Name: name, Version: version})
}

// RemoveDependency removes a dependency by name. Returns true if found.
func (c *Config) RemoveDependency(name string) bool {
	for i, dep := range c.Dependencies {
		if strings.EqualFold(dep.Name, name) {
			c.Dependencies = append(c.Dependencies[:i], c.Dependencies[i+1:]...)
			return true
		}
	}
	return false
}

// SaveToFile writes the config back to zinc.toml in the given directory.
func (c *Config) SaveToFile(dir string) error {
	path := filepath.Join(dir, "zinc.toml")
	return os.WriteFile(path, []byte(Generate(c)), 0644)
}

// RuntimeID returns the .NET runtime identifier for the current platform.
func RuntimeID() string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	switch os {
	case "linux":
		switch arch {
		case "amd64":
			return "linux-x64"
		case "arm64":
			return "linux-arm64"
		}
	case "darwin":
		switch arch {
		case "amd64":
			return "osx-x64"
		case "arm64":
			return "osx-arm64"
		}
	case "windows":
		switch arch {
		case "amd64":
			return "win-x64"
		case "arm64":
			return "win-arm64"
		}
	}
	return os + "-" + arch
}

// GenerateCsproj creates a .csproj file for C# AOT compilation.
func GenerateCsproj(cfg *Config) string {
	var b strings.Builder
	b.WriteString("<Project Sdk=\"Microsoft.NET.Sdk\">\n\n")
	b.WriteString("  <PropertyGroup>\n")
	b.WriteString("    <OutputType>Exe</OutputType>\n")
	b.WriteString("    <TargetFramework>net10.0</TargetFramework>\n")
	b.WriteString(fmt.Sprintf("    <AssemblyName>%s</AssemblyName>\n", cfg.Name))
	if cfg.Optimize {
		b.WriteString("    <PublishAot>true</PublishAot>\n")
		b.WriteString("    <SelfContained>true</SelfContained>\n")
		b.WriteString("    <OptimizationPreference>Speed</OptimizationPreference>\n")
		b.WriteString("    <IlcOptimizationPreference>Speed</IlcOptimizationPreference>\n")
		if cfg.Release {
			b.WriteString("    <StripSymbols>true</StripSymbols>\n")
		} else {
			b.WriteString("    <DebugType>embedded</DebugType>\n")
		}
		b.WriteString("    <TrimMode>full</TrimMode>\n")
		b.WriteString("    <InvariantGlobalization>true</InvariantGlobalization>\n")
		b.WriteString("    <JsonSerializerIsReflectionEnabledByDefault>true</JsonSerializerIsReflectionEnabledByDefault>\n")
	}
	b.WriteString("  </PropertyGroup>\n")

	if len(cfg.Dependencies) > 0 {
		b.WriteString("\n  <ItemGroup>\n")
		// Sort for deterministic output
		deps := make([]Dependency, len(cfg.Dependencies))
		copy(deps, cfg.Dependencies)
		sort.Slice(deps, func(i, j int) bool { return deps[i].Name < deps[j].Name })
		for _, dep := range deps {
			b.WriteString(fmt.Sprintf("    <PackageReference Include=\"%s\" Version=\"%s\" />\n", dep.Name, dep.Version))
		}
		b.WriteString("  </ItemGroup>\n")
	}

	b.WriteString("\n</Project>\n")
	return b.String()
}

// GenerateGoMod creates a go.mod file for the Go backend.
func GenerateGoMod(cfg *Config) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("module %s\n\ngo 1.26\n", cfg.Name))
	if len(cfg.Dependencies) > 0 {
		b.WriteString("\nrequire (\n")
		for _, dep := range cfg.Dependencies {
			b.WriteString(fmt.Sprintf("\t%s %s\n", dep.Name, dep.Version))
		}
		b.WriteString(")\n")
	}
	return b.String()
}
