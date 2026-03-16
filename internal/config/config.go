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
	"strings"
)

// Config represents a zinc.toml project configuration.
type Config struct {
	Name     string // project name
	Version  string // project version
	Target   string // "csharp" (default) or "go"
	Optimize bool   // AOT with full optimizations (default: true)
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

		switch fullKey {
		case "project.name":
			cfg.Name = val
		case "project.version":
			cfg.Version = val
		case "build.target":
			cfg.Target = val
		case "build.optimize":
			cfg.Optimize = val == "true"
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
	return b.String()
}

// GenerateCsproj creates a .csproj file for C# AOT compilation.
func GenerateCsproj(cfg *Config) string {
	var b strings.Builder
	b.WriteString("<Project Sdk=\"Microsoft.NET.Sdk\">\n\n")
	b.WriteString("  <PropertyGroup>\n")
	b.WriteString("    <OutputType>Exe</OutputType>\n")
	b.WriteString("    <TargetFramework>net10.0</TargetFramework>\n")
	if cfg.Optimize {
		b.WriteString("    <PublishAot>true</PublishAot>\n")
		b.WriteString("    <OptimizationPreference>Speed</OptimizationPreference>\n")
		b.WriteString("    <IlcOptimizationPreference>Speed</IlcOptimizationPreference>\n")
		b.WriteString("    <StripSymbols>true</StripSymbols>\n")
		b.WriteString("    <TrimMode>full</TrimMode>\n")
		b.WriteString("    <InvariantGlobalization>true</InvariantGlobalization>\n")
	}
	b.WriteString("  </PropertyGroup>\n\n")
	b.WriteString("</Project>\n")
	return b.String()
}

// GenerateGoMod creates a go.mod file for the Go backend.
func GenerateGoMod(cfg *Config) string {
	return fmt.Sprintf("module %s\n\ngo 1.26\n", cfg.Name)
}
