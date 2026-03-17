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
	"testing"

	"zinc/internal/config"
)

func skipIfNoDotnet(t *testing.T) {
	t.Helper()
	if findDotnet() == "" {
		t.Skip("dotnet SDK not found, skipping type resolver test")
	}
}

func probeResolver(t *testing.T) *CSharpTypeResolver {
	t.Helper()
	skipIfNoDotnet(t)
	r := NewCSharpTypeResolver()
	cfg := config.DefaultConfig("test")
	if err := r.Probe(cfg); err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	if !r.Loaded() {
		t.Fatal("expected resolver to have loaded types")
	}
	return r
}

func TestProbeLoadsTypes(t *testing.T) {
	r := probeResolver(t)

	// The probe should discover thousands of types across many namespaces
	total := 0
	r.mu.Lock()
	for _, types := range r.cache {
		total += len(types)
	}
	nsCount := len(r.cache)
	r.mu.Unlock()

	if total < 100 {
		t.Errorf("expected hundreds of types, got %d", total)
	}
	if nsCount < 10 {
		t.Errorf("expected many namespaces, got %d", nsCount)
	}
	t.Logf("probe discovered %d types across %d namespaces", total, nsCount)
}

func TestProbeConstructableClasses(t *testing.T) {
	r := probeResolver(t)

	// These should all be constructable (need `new`)
	classes := []struct{ ns, name string }{
		{"System", "Random"},
		{"System", "Uri"},
		{"System.IO", "StreamReader"},
		{"System.IO", "MemoryStream"},
		{"System.Net.Http", "HttpClient"},
		{"System.Text", "StringBuilder"},
		{"System.Text.RegularExpressions", "Regex"},
		{"System.Diagnostics", "Stopwatch"},
	}
	for _, tt := range classes {
		if !r.IsClass(tt.ns, tt.name) {
			t.Errorf("expected %s.%s to be a constructable class", tt.ns, tt.name)
		}
	}
}

func TestProbeStaticClasses(t *testing.T) {
	r := probeResolver(t)

	// Static classes should NOT be constructable
	statics := []struct{ ns, name string }{
		{"System", "Console"},
		{"System", "Math"},
		{"System", "Environment"},
		{"System.IO", "File"},
		{"System.IO", "Directory"},
	}
	for _, tt := range statics {
		if !r.IsType(tt.ns, tt.name) {
			t.Errorf("expected %s.%s to be a known type", tt.ns, tt.name)
		}
		if r.IsClass(tt.ns, tt.name) {
			t.Errorf("expected %s.%s to NOT be constructable (it's static)", tt.ns, tt.name)
		}
	}
}

func TestProbeIsClassAnywhere(t *testing.T) {
	r := probeResolver(t)

	if !r.IsClassAnywhere("HttpClient") {
		t.Error("expected HttpClient to be found as constructable class")
	}
	if !r.IsClassAnywhere("Stopwatch") {
		t.Error("expected Stopwatch to be found as constructable class")
	}
	if !r.IsClassAnywhere("StringBuilder") {
		t.Error("expected StringBuilder to be found as constructable class")
	}
	// Static classes should not match
	if r.IsClassAnywhere("Console") {
		t.Error("expected Console to NOT be a constructable class")
	}
	if r.IsClassAnywhere("Math") {
		t.Error("expected Math to NOT be a constructable class")
	}
}

func TestProbeClassesInNamespace(t *testing.T) {
	r := probeResolver(t)

	classes := r.ClassesInNamespace("System.Net.Http")
	found := false
	for _, c := range classes {
		if c == "HttpClient" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected HttpClient in System.Net.Http classes, got %v", classes)
	}
}
