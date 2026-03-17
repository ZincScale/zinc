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

package codegen_csharp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"zinc/internal/config"
)

// CSharpTypeResolver resolves .NET type information at transpile time.
//
// It creates a temporary .NET console project, runs it, and uses .NET's own
// System.Reflection APIs to enumerate types from the BCL and any NuGet
// packages the user has declared. This is the C# equivalent of GoTypeResolver
// (which uses go/types).
type CSharpTypeResolver struct {
	mu    sync.Mutex
	cache map[string][]TypeInfo // namespace → type infos
}

// TypeInfo describes a .NET type discovered via reflection.
type TypeInfo struct {
	Name     string `json:"Name"`
	IsClass  bool   `json:"IsClass"`  // instantiable class/struct (needs `new`)
	IsStatic bool   `json:"IsStatic"` // static class (no constructor)
	IsEnum   bool   `json:"IsEnum"`
}

// NewCSharpTypeResolver creates an empty resolver.
func NewCSharpTypeResolver() *CSharpTypeResolver {
	return &CSharpTypeResolver{
		cache: make(map[string][]TypeInfo),
	}
}

// IsType reports whether name is a known type in the given namespace.
func (r *CSharpTypeResolver) IsType(namespace, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ti := range r.cache[namespace] {
		if ti.Name == name {
			return true
		}
	}
	return false
}

// IsClass reports whether name is a constructable class (needs `new`).
func (r *CSharpTypeResolver) IsClass(namespace, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ti := range r.cache[namespace] {
		if ti.Name == name {
			return ti.IsClass && !ti.IsStatic
		}
	}
	return false
}

// IsClassAnywhere reports whether name is a constructable class in any loaded namespace.
func (r *CSharpTypeResolver) IsClassAnywhere(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, types := range r.cache {
		for _, ti := range types {
			if ti.Name == name && ti.IsClass && !ti.IsStatic {
				return true
			}
		}
	}
	return false
}

// IsTypeAnywhere reports whether name exists as any type in any loaded namespace.
func (r *CSharpTypeResolver) IsTypeAnywhere(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, types := range r.cache {
		for _, ti := range types {
			if ti.Name == name {
				return true
			}
		}
	}
	return false
}

// ClassesInNamespace returns constructable class names in a namespace.
func (r *CSharpTypeResolver) ClassesInNamespace(namespace string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var names []string
	for _, ti := range r.cache[namespace] {
		if ti.IsClass && !ti.IsStatic {
			names = append(names, ti.Name)
		}
	}
	return names
}

// Loaded reports whether any types have been loaded.
func (r *CSharpTypeResolver) Loaded() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cache) > 0
}

// probeProgram is the C# source that uses .NET reflection to enumerate ALL
// public types from every assembly reachable at runtime. Rather than hardcoding
// assembly names/versions, it references a type from each assembly to force
// the runtime to load it, then walks AppDomain.CurrentDomain.GetAssemblies().
const probeProgram = `using System;
using System.Collections.Generic;
using System.Reflection;
using System.Text.Json;

// Touch types from common assemblies to force-load them.
// The runtime only loads assemblies on demand; without this, assemblies like
// System.Net.Http or System.Diagnostics.Process would not be enumerable.
static void Touch(params Type[] _) { }
Touch(
    typeof(System.Net.Http.HttpClient),
    typeof(System.Text.Json.JsonSerializer),
    typeof(System.Text.RegularExpressions.Regex),
    typeof(System.Diagnostics.Stopwatch),
    typeof(System.Diagnostics.Process),
    typeof(System.Threading.Thread),
    typeof(System.Threading.Semaphore),
    typeof(System.Collections.Generic.HashSet<int>),
    typeof(System.Linq.Enumerable),
    typeof(System.IO.StreamReader),
    typeof(System.IO.MemoryStream),
    typeof(System.Net.IPAddress),
    typeof(System.Uri),
    typeof(System.Text.StringBuilder),
    typeof(System.Security.Cryptography.SHA256)
);

// Also try loading any NuGet package assemblies that may be present
foreach (var file in System.IO.Directory.GetFiles(
    System.Runtime.InteropServices.RuntimeEnvironment.GetRuntimeDirectory(), "*.dll"))
{
    try { Assembly.LoadFrom(file); } catch { }
}

var result = new List<Dictionary<string, object>>();
foreach (var asm in AppDomain.CurrentDomain.GetAssemblies())
{
    try
    {
        foreach (var type in asm.GetExportedTypes())
        {
            if (type.Namespace == null) continue;
            if (type.Name.Contains('` + "`" + `')) continue;

            result.Add(new Dictionary<string, object>
            {
                ["Ns"] = type.Namespace,
                ["Name"] = type.Name,
                ["IsClass"] = type.IsClass && !type.IsInterface,
                ["IsStatic"] = type.IsClass && type.IsAbstract && type.IsSealed,
                ["IsEnum"] = type.IsEnum,
            });
        }
    }
    catch { }
}

Console.Write(JsonSerializer.Serialize(result));
`

// Probe runs a .NET reflection probe to discover types available in the runtime
// and any NuGet packages declared in the config. Creates a temporary project,
// runs it, and populates the cache with discovered types.
func (r *CSharpTypeResolver) Probe(cfg *config.Config) error {
	dotnetPath, err := findDotnetBinary()
	if err != nil {
		return fmt.Errorf("dotnet not found: %w", err)
	}

	// Create temp probe project
	probeDir, err := os.MkdirTemp("", "zinc-probe-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(probeDir)

	// Generate .csproj with the same dependencies as the user's project
	probeCsproj := generateProbeCsproj(cfg)
	if err := os.WriteFile(filepath.Join(probeDir, "probe.csproj"), []byte(probeCsproj), 0644); err != nil {
		return err
	}

	// Write the probe program
	if err := os.WriteFile(filepath.Join(probeDir, "Program.cs"), []byte(probeProgram), 0644); err != nil {
		return err
	}

	// Restore dependencies
	restore := exec.Command(dotnetPath, "restore", "--verbosity", "quiet")
	restore.Dir = probeDir
	restore.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	if out, err := restore.CombinedOutput(); err != nil {
		return fmt.Errorf("probe restore failed: %w\n%s", err, out)
	}

	// Run the probe
	run := exec.Command(dotnetPath, "run", "--no-restore", "--verbosity", "quiet")
	run.Dir = probeDir
	run.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	out, err := run.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("probe run failed: %w\nstderr: %s", err, exitErr.Stderr)
		}
		return fmt.Errorf("probe run failed: %w", err)
	}

	// Parse the JSON output
	var types []struct {
		Ns       string `json:"Ns"`
		Name     string `json:"Name"`
		IsClass  bool   `json:"IsClass"`
		IsStatic bool   `json:"IsStatic"`
		IsEnum   bool   `json:"IsEnum"`
	}
	if err := json.Unmarshal(out, &types); err != nil {
		return fmt.Errorf("probe parse failed: %w\nraw output: %s", err, string(out[:min(len(out), 200)]))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range types {
		r.cache[t.Ns] = append(r.cache[t.Ns], TypeInfo{
			Name:     t.Name,
			IsClass:  t.IsClass,
			IsStatic: t.IsStatic,
			IsEnum:   t.IsEnum,
		})
	}
	return nil
}

// generateProbeCsproj creates a minimal .csproj for the type probe,
// including the same NuGet dependencies as the user's project.
func generateProbeCsproj(cfg *config.Config) string {
	var b strings.Builder
	b.WriteString("<Project Sdk=\"Microsoft.NET.Sdk\">\n\n")
	b.WriteString("  <PropertyGroup>\n")
	b.WriteString("    <OutputType>Exe</OutputType>\n")
	b.WriteString("    <TargetFramework>net10.0</TargetFramework>\n")
	b.WriteString("  </PropertyGroup>\n")

	if cfg != nil && len(cfg.Dependencies) > 0 {
		b.WriteString("\n  <ItemGroup>\n")
		for _, dep := range cfg.Dependencies {
			b.WriteString(fmt.Sprintf("    <PackageReference Include=\"%s\" Version=\"%s\" />\n", dep.Name, dep.Version))
		}
		b.WriteString("  </ItemGroup>\n")
	}

	b.WriteString("\n</Project>\n")
	return b.String()
}

func findDotnetBinary() (string, error) {
	// Check PATH first
	if p, err := exec.LookPath("dotnet"); err == nil {
		return p, nil
	}
	// Check common install locations
	home, _ := os.UserHomeDir()
	for _, candidate := range []string{
		home + "/.dotnet/dotnet",
		"/usr/local/bin/dotnet",
		"/usr/bin/dotnet",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("dotnet not found in PATH or common locations")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
