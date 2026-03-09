package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- ParseMod tests ----------------------------------------------------------

func TestParseMod(t *testing.T) {
	content := "module myapp\nzinc 0.1\n"
	path := writeTempFile(t, "zinc.mod", content)

	mod, err := ParseMod(path)
	if err != nil {
		t.Fatalf("ParseMod: %v", err)
	}
	if mod.Module != "myapp" {
		t.Errorf("expected Module 'myapp', got %q", mod.Module)
	}
	if mod.Version != "0.1" {
		t.Errorf("expected Version '0.1', got %q", mod.Version)
	}
}

func TestParseModWithComments(t *testing.T) {
	content := "# This is a comment\nmodule myproject\n# another comment\nzinc 0.2\n"
	path := writeTempFile(t, "zinc.mod", content)

	mod, err := ParseMod(path)
	if err != nil {
		t.Fatalf("ParseMod: %v", err)
	}
	if mod.Module != "myproject" {
		t.Errorf("expected Module 'myproject', got %q", mod.Module)
	}
	if mod.Version != "0.2" {
		t.Errorf("expected Version '0.2', got %q", mod.Version)
	}
}

func TestParseModMissingModule(t *testing.T) {
	content := "zinc 0.1\n"
	path := writeTempFile(t, "zinc.mod", content)

	_, err := ParseMod(path)
	if err == nil {
		t.Error("expected error for missing 'module' directive")
	}
}

func TestParseModNotFound(t *testing.T) {
	_, err := ParseMod("/nonexistent/path/zinc.mod")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestParseModEmptyLines(t *testing.T) {
	content := "\n\nmodule   spacious\n\nzinc 0.3\n\n"
	path := writeTempFile(t, "zinc.mod", content)

	mod, err := ParseMod(path)
	if err != nil {
		t.Fatalf("ParseMod: %v", err)
	}
	if mod.Module != "spacious" {
		t.Errorf("expected Module 'spacious', got %q", mod.Module)
	}
}

// --- FindMod tests -----------------------------------------------------------

func TestFindModInSameDir(t *testing.T) {
	dir := t.TempDir()
	modPath := filepath.Join(dir, "zinc.mod")
	os.WriteFile(modPath, []byte("module test\nzinc 0.1\n"), 0644)

	found, root, err := FindMod(dir)
	if err != nil {
		t.Fatalf("FindMod: %v", err)
	}
	if found != modPath {
		t.Errorf("expected modPath %q, got %q", modPath, found)
	}
	if root != dir {
		t.Errorf("expected root %q, got %q", dir, root)
	}
}

func TestFindModInParentDir(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "models")
	os.Mkdir(subDir, 0755)

	modPath := filepath.Join(dir, "zinc.mod")
	os.WriteFile(modPath, []byte("module test\nzinc 0.1\n"), 0644)

	found, root, err := FindMod(subDir)
	if err != nil {
		t.Fatalf("FindMod: %v", err)
	}
	if found != modPath {
		t.Errorf("expected modPath %q, got %q", modPath, found)
	}
	if root != dir {
		t.Errorf("expected root %q, got %q", dir, root)
	}
}

func TestFindModNotFound(t *testing.T) {
	dir := t.TempDir()
	_, _, err := FindMod(dir)
	if err == nil {
		t.Error("expected error when zinc.mod not found")
	}
	if !strings.Contains(err.Error(), "zinc.mod not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// --- Transpile tests ---------------------------------------------------------

func TestTranspileSingleFile(t *testing.T) {
	dir := t.TempDir()
	src := `fn main() {
    var x: Int = 42
    print(x)
}`
	os.WriteFile(filepath.Join(dir, "main.zn"), []byte(src), 0644)

	units, err := Transpile(dir)
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}

	outPath := filepath.Join(dir, "main.go")
	if units[0].OutPath != outPath {
		t.Errorf("expected OutPath %q, got %q", outPath, units[0].OutPath)
	}
	if units[0].PackageName != "main" {
		t.Errorf("expected PackageName 'main', got %q", units[0].PackageName)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "package main") {
		t.Errorf("expected 'package main' in output:\n%s", out)
	}
	if !strings.Contains(out, "x := 42") {
		t.Errorf("expected 'x := 42' in output:\n%s", out)
	}
}

func TestTranspileWithPackageDecl(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "utils")
	os.Mkdir(subDir, 0755)

	src := `package "myapp/utils"

pub fn add(a: Int, b: Int): Int {
    return a + b
}
`
	os.WriteFile(filepath.Join(subDir, "math.zn"), []byte(src), 0644)

	units, err := Transpile(dir)
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}
	if units[0].PackageName != "utils" {
		t.Errorf("expected PackageName 'utils', got %q", units[0].PackageName)
	}

	data, err := os.ReadFile(units[0].OutPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "package utils") {
		t.Errorf("expected 'package utils' in output:\n%s", out)
	}
}

func TestTranspileMultipleFiles(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "main.zn"), []byte(`fn main() { print("hello") }`), 0644)

	utilsDir := filepath.Join(dir, "utils")
	os.Mkdir(utilsDir, 0755)
	os.WriteFile(filepath.Join(utilsDir, "math.zn"), []byte(`package "myapp/utils"
pub fn add(a: Int, b: Int): Int { return a }`), 0644)

	units, err := Transpile(dir)
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units))
	}
}

func TestTranspileSharedRegistry(t *testing.T) {
	// Two files in the same directory: dog.zn defines Dog, animal.zn defines Animal.
	// With shared registry, Dog can reference Animal (cross-file type resolution).
	dir := t.TempDir()

	animal := `package "myapp/models"
class Animal {
    var name: String
    construct new(name: String) { this.name = name }
}
`
	dog := `package "myapp/models"
class Dog : Animal {
    construct new(name: String) { super(name) }
    pub fn bark(): String { return "Woof!" }
}
`
	os.WriteFile(filepath.Join(dir, "animal.zn"), []byte(animal), 0644)
	os.WriteFile(filepath.Join(dir, "dog.zn"), []byte(dog), 0644)

	units, err := Transpile(dir)
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units))
	}
	for _, u := range units {
		if u.PackageName != "models" {
			t.Errorf("expected PackageName 'models', got %q", u.PackageName)
		}
	}
}

func TestPkgLastSegment(t *testing.T) {
	cases := []struct{ path, want string }{
		{"myapp/utils", "utils"},
		{"myapp/models/sub", "sub"},
		{"myapp", "myapp"},
		{"", ""},
	}
	for _, c := range cases {
		got := pkgLastSegment(c.path)
		if got != c.want {
			t.Errorf("pkgLastSegment(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// --- Helpers -----------------------------------------------------------------

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
	return path
}
