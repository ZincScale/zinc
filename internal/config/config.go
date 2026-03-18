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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// NuGetSource represents a named NuGet package source with optional auth.
type NuGetSource struct {
	Name string `toml:"name"`           // display name (e.g. "github", "artifactory")
	URL  string `toml:"url"`            // flat container URL
	Auth string `toml:"auth,omitempty"` // auth token reference (e.g. "env:GITHUB_TOKEN")
	Type string `toml:"type,omitempty"` // auth type: "bearer" (default) or "basic"
}

// NuGetConfig holds NuGet package source configuration.
type NuGetConfig struct {
	Source  string        `toml:"source,omitempty"`  // default source URL (simple case)
	Sources []NuGetSource `toml:"sources,omitempty"` // named sources with auth
}

// tomlFile is the raw TOML structure that maps directly to zinc.toml.
type tomlFile struct {
	Project      tomlProject       `toml:"project"`
	Build        tomlBuild         `toml:"build"`
	NuGet        NuGetConfig       `toml:"nuget"`
	Dependencies map[string]string `toml:"dependencies,omitempty"`
}

type tomlProject struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
}

type tomlBuild struct {
	Target   string `toml:"target"`
	Optimize *bool  `toml:"optimize,omitempty"` // pointer so we can detect missing vs false
}

// Dependency represents a package dependency.
type Dependency struct {
	Name    string
	Version string
}

// Config represents a zinc.toml project configuration.
type Config struct {
	Name         string
	Version      string
	Target       string       // "csharp" (default) or "go"
	Optimize     bool         // AOT with full optimizations (default: true)
	Release      bool         // strip symbols for production (set by --release flag, not in toml)
	NuGet        NuGetConfig  // NuGet source configuration
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
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var raw tomlFile
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("zinc.toml: %w", err)
	}

	cfg := &Config{
		Name:    raw.Project.Name,
		Version: raw.Project.Version,
		Target:  raw.Build.Target,
		NuGet:   raw.NuGet,
	}

	// Default target
	if cfg.Target == "" {
		cfg.Target = "csharp"
	}

	// Default optimize to true unless explicitly set to false
	if raw.Build.Optimize != nil {
		cfg.Optimize = *raw.Build.Optimize
	} else {
		cfg.Optimize = true
	}

	// Convert dependencies map to slice
	for name, version := range raw.Dependencies {
		cfg.Dependencies = append(cfg.Dependencies, Dependency{Name: name, Version: version})
	}
	// Sort for deterministic ordering
	sort.Slice(cfg.Dependencies, func(i, j int) bool {
		return cfg.Dependencies[i].Name < cfg.Dependencies[j].Name
	})

	return cfg, nil
}

// Generate creates a zinc.toml file content from a Config.
func Generate(cfg *Config) string {
	raw := tomlFile{
		Project: tomlProject{
			Name:    cfg.Name,
			Version: cfg.Version,
		},
		Build: tomlBuild{
			Target:   cfg.Target,
			Optimize: &cfg.Optimize,
		},
		NuGet: cfg.NuGet,
	}

	if len(cfg.Dependencies) > 0 {
		raw.Dependencies = make(map[string]string)
		for _, dep := range cfg.Dependencies {
			raw.Dependencies[dep.Name] = dep.Version
		}
	}

	data, err := toml.Marshal(raw)
	if err != nil {
		// Fallback — shouldn't happen
		return fmt.Sprintf("# error marshaling config: %v\n", err)
	}
	return string(data)
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

// GetNuGetSource returns the URL and auth for a named source.
// If sourceName is empty, returns the default source.
func (c *Config) GetNuGetSource(sourceName string) (url, authToken, authType string) {
	if sourceName == "" {
		// Use default source
		if c.NuGet.Source != "" {
			return c.NuGet.Source, "", ""
		}
		// Check if there's a first named source
		if len(c.NuGet.Sources) > 0 {
			src := c.NuGet.Sources[0]
			return src.URL, resolveAuth(src.Auth), src.Type
		}
		return "", "", ""
	}

	// Find named source
	for _, src := range c.NuGet.Sources {
		if strings.EqualFold(src.Name, sourceName) {
			return src.URL, resolveAuth(src.Auth), src.Type
		}
	}
	return "", "", ""
}

// resolveAuth resolves an auth reference to its value.
// Supports "env:VARNAME" to read from environment variables.
func resolveAuth(auth string) string {
	if auth == "" {
		return ""
	}
	if strings.HasPrefix(auth, "env:") {
		return os.Getenv(strings.TrimPrefix(auth, "env:"))
	}
	return auth
}

// RuntimeID returns the .NET runtime identifier for the current platform.
func RuntimeID() string {
	goos := runtime.GOOS
	arch := runtime.GOARCH
	switch goos {
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
	return goos + "-" + arch
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
