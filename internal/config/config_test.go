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

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("myapp")
	if cfg.Name != "myapp" {
		t.Errorf("expected name myapp, got %s", cfg.Name)
	}
	if cfg.Target != "csharp" {
		t.Errorf("expected target csharp, got %s", cfg.Target)
	}
	if !cfg.Optimize {
		t.Error("expected optimize true")
	}
}

func TestGenerate(t *testing.T) {
	cfg := DefaultConfig("myapp")
	out := Generate(cfg)
	if !strings.Contains(out, "name = ") || !strings.Contains(out, "myapp") {
		t.Errorf("missing name in output:\n%s", out)
	}
	if !strings.Contains(out, "target = ") || !strings.Contains(out, "csharp") {
		t.Errorf("missing target in output:\n%s", out)
	}
	if !strings.Contains(out, "optimize = true") {
		t.Errorf("missing optimize in output:\n%s", out)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	content := `[project]
name = "testapp"
version = "1.0.0"

[build]
target = "go"
optimize = false
`
	if err := os.WriteFile(filepath.Join(dir, "zinc.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "testapp" {
		t.Errorf("expected name testapp, got %s", cfg.Name)
	}
	if cfg.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", cfg.Version)
	}
	if cfg.Target != "go" {
		t.Errorf("expected target go, got %s", cfg.Target)
	}
	if cfg.Optimize {
		t.Error("expected optimize false")
	}
}

func TestLoadConfigMissing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Error("expected nil config for missing file")
	}
}

func TestGenerateCsproj(t *testing.T) {
	cfg := DefaultConfig("myapp")
	out := GenerateCsproj(cfg)
	for _, want := range []string{
		"PublishAot",
		"SelfContained",
		"OptimizationPreference",
		"TrimMode",
		"DebugType",
		"InvariantGlobalization",
		"JsonSerializerIsReflectionEnabledByDefault",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in csproj:\n%s", want, out)
		}
	}
}

func TestGenerateCsprojDebugMode(t *testing.T) {
	cfg := DefaultConfig("myapp")
	out := GenerateCsproj(cfg)
	if strings.Contains(out, "StripSymbols") {
		t.Errorf("default build should not strip symbols:\n%s", out)
	}
	if !strings.Contains(out, "DebugType") {
		t.Errorf("default build should have embedded debug info:\n%s", out)
	}
}

func TestGenerateCsprojRelease(t *testing.T) {
	cfg := DefaultConfig("myapp")
	cfg.Release = true
	out := GenerateCsproj(cfg)
	if !strings.Contains(out, "StripSymbols") {
		t.Errorf("release build should strip symbols:\n%s", out)
	}
	if strings.Contains(out, "DebugType") {
		t.Errorf("release build should not have embedded debug info:\n%s", out)
	}
}

func TestGenerateCsprojNoOptimize(t *testing.T) {
	cfg := DefaultConfig("myapp")
	cfg.Optimize = false
	out := GenerateCsproj(cfg)
	if strings.Contains(out, "PublishAot") {
		t.Errorf("should not have PublishAot when optimize=false:\n%s", out)
	}
}

func TestGenerateGoMod(t *testing.T) {
	cfg := DefaultConfig("myapp")
	out := GenerateGoMod(cfg)
	if !strings.Contains(out, "module myapp") {
		t.Errorf("missing module in go.mod:\n%s", out)
	}
}

func TestRoundTrip(t *testing.T) {
	cfg := DefaultConfig("roundtrip")
	content := Generate(cfg)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zinc.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != cfg.Name {
		t.Errorf("name mismatch: %s vs %s", loaded.Name, cfg.Name)
	}
	if loaded.Target != cfg.Target {
		t.Errorf("target mismatch: %s vs %s", loaded.Target, cfg.Target)
	}
	if loaded.Optimize != cfg.Optimize {
		t.Errorf("optimize mismatch: %v vs %v", loaded.Optimize, cfg.Optimize)
	}
}
