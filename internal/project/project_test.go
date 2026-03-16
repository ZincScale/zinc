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
	"fmt"
	"os"
	"os/exec"
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
	src := `main() {
    x := 42
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

pub Int add(Int a, Int b) {
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

	os.WriteFile(filepath.Join(dir, "main.zn"), []byte(`main() { print("hello") }`), 0644)

	utilsDir := filepath.Join(dir, "utils")
	os.Mkdir(utilsDir, 0755)
	os.WriteFile(filepath.Join(utilsDir, "math.zn"), []byte(`package "myapp/utils"
pub Int add(Int a, Int b) { return a }`), 0644)

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
Animal {
    String name
    new(String name) { this.name = name }
}
`
	dog := `package "myapp/models"
Dog : Animal {
    new(String name) { super(name) }
    pub String bark() { return "Woof!" }
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

func TestCrossFileSuperArgs(t *testing.T) {
	// Animal defined in one file, Dog inheriting from Animal in another.
	// Dog's constructor calls super(name, "Woof") — the registry must share
	// ClassCtors across files for the super args to codegen correctly.
	dir := t.TempDir()

	animal := `Animal {
    String name
    String sound
    new(String name, String sound) {
        this.name = name
        this.sound = sound
    }
}
`
	dog := `Dog : Animal {
    new(String name) {
        super(name, "Woof")
    }
}

main() {
    d := Dog("Rex")
    print(d.name)
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

	// Find dog.go and verify super args flow through
	for _, u := range units {
		if strings.HasSuffix(u.OutPath, "dog.go") {
			data, err := os.ReadFile(u.OutPath)
			if err != nil {
				t.Fatalf("reading dog.go: %v", err)
			}
			out := string(data)
			if !strings.Contains(out, `*NewAnimal(name, "Woof")`) {
				t.Errorf("expected super args in Dog constructor, got:\n%s", out)
			}
			return
		}
	}
	t.Error("dog.go not found in transpile output")
}

func TestCrossFileFailableDetection(t *testing.T) {
	// File A defines a failable function, file B calls it.
	// The registry must share CanThrowFns so B's caller auto-propagates.
	dir := t.TempDir()

	fileA := `Int risky() {
    return Error("something went wrong")
}
`
	fileB := `main() {
    x := risky()
    print(x)
}
`
	os.WriteFile(filepath.Join(dir, "a.zn"), []byte(fileA), 0644)
	os.WriteFile(filepath.Join(dir, "b.zn"), []byte(fileB), 0644)

	units, err := Transpile(dir)
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units))
	}

	// Find b.go and verify error auto-propagation
	for _, u := range units {
		if strings.HasSuffix(u.OutPath, "b.go") {
			data, err := os.ReadFile(u.OutPath)
			if err != nil {
				t.Fatalf("reading b.go: %v", err)
			}
			out := string(data)
			// Should have error handling: _err variable and panic check
			if !strings.Contains(out, "_err") {
				t.Errorf("expected error propagation in b.go, got:\n%s", out)
			}
			return
		}
	}
	t.Error("b.go not found in transpile output")
}

func TestCrossFileNamedArgs(t *testing.T) {
	// File A defines a function with defaults, file B calls it with named args.
	dir := t.TempDir()

	fileA := `greet(String name, String greeting = "Hello") {
    print("{greeting}, {name}!")
}
`
	fileB := `main() {
    greet("Alice")
    greet("Bob", greeting: "Hi")
}
`
	os.WriteFile(filepath.Join(dir, "a.zn"), []byte(fileA), 0644)
	os.WriteFile(filepath.Join(dir, "b.zn"), []byte(fileB), 0644)

	units, err := Transpile(dir)
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units))
	}

	// Find b.go and verify default arg was inlined
	for _, u := range units {
		if strings.HasSuffix(u.OutPath, "b.go") {
			data, err := os.ReadFile(u.OutPath)
			if err != nil {
				t.Fatalf("reading b.go: %v", err)
			}
			out := string(data)
			if !strings.Contains(out, `greet("Alice", "Hello")`) {
				t.Errorf("expected default arg inlined in b.go, got:\n%s", out)
			}
			return
		}
	}
	t.Error("b.go not found in transpile output")
}

// --- Cross-file enum usage ---------------------------------------------------

func TestCrossFileEnumUsage(t *testing.T) {
	// File A defines an enum, file B uses it in a match statement.
	// Uses Color.Red syntax (Zinc-style), not ColorRed (Go-style).
	dir := t.TempDir()

	fileA := `enum Color { Red, Green, Blue }
`
	fileB := `String describe(Color c) {
    match c {
        case Color.Red -> { return "red" }
        case Color.Green -> { return "green" }
        case _ -> { return "other" }
    }
}

main() {
    print(describe(Color.Red))
    print(describe(Color.Blue))
}
`
	os.WriteFile(filepath.Join(dir, "color.zn"), []byte(fileA), 0644)
	os.WriteFile(filepath.Join(dir, "main.zn"), []byte(fileB), 0644)

	units, err := Transpile(dir)
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units))
	}

	// Find main.go and verify enum cases are referenced correctly
	for _, u := range units {
		if strings.HasSuffix(u.OutPath, "main.go") {
			data, err := os.ReadFile(u.OutPath)
			if err != nil {
				t.Fatalf("reading main.go: %v", err)
			}
			out := string(data)
			if !strings.Contains(out, "case ColorRed:") {
				t.Errorf("expected 'case ColorRed:' in main.go, got:\n%s", out)
			}
			return
		}
	}
	t.Error("main.go not found in transpile output")
}

// --- Cross-file interface compliance -----------------------------------------

func TestCrossFileInterfaceCompliance(t *testing.T) {
	// File A defines an interface, file B defines a class implementing it.
	dir := t.TempDir()

	fileA := `interface Speaker {
    pub String speak()
}
`
	fileB := `Dog : Speaker {
    String name
    new(String n) { this.name = n }
    pub String speak() { return "{this.name} says woof" }
}

main() {
    d := Dog("Rex")
    print(d.speak())
}
`
	os.WriteFile(filepath.Join(dir, "speaker.zn"), []byte(fileA), 0644)
	os.WriteFile(filepath.Join(dir, "dog.zn"), []byte(fileB), 0644)

	units, err := Transpile(dir)
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units))
	}

	// Find dog.go and verify interface compliance check
	for _, u := range units {
		if strings.HasSuffix(u.OutPath, "dog.go") {
			data, err := os.ReadFile(u.OutPath)
			if err != nil {
				t.Fatalf("reading dog.go: %v", err)
			}
			out := string(data)
			if !strings.Contains(out, "var _ Speaker = (*DogImpl)(nil)") {
				t.Errorf("expected interface compliance check in dog.go, got:\n%s", out)
			}
			return
		}
	}
	t.Error("dog.go not found in transpile output")
}

// --- Cross-file method params with defaults ----------------------------------

func TestCrossFileMethodNamedArgs(t *testing.T) {
	// File A defines a class with a method that has default params.
	// File B calls that method with named args.
	dir := t.TempDir()

	fileA := `Greeter {
    String prefix
    new(String prefix) { this.prefix = prefix }
    pub String greet(String name, Bool excited = false) {
        if excited {
            return "{this.prefix} {name}!!!"
        }
        return "{this.prefix} {name}"
    }
}
`
	fileB := `main() {
    g := Greeter("Hello")
    print(g.greet("Alice"))
    print(g.greet("Bob", excited: true))
}
`
	os.WriteFile(filepath.Join(dir, "greeter.zn"), []byte(fileA), 0644)
	os.WriteFile(filepath.Join(dir, "main.zn"), []byte(fileB), 0644)

	units, err := Transpile(dir)
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(units))
	}

	// Find main.go and verify default arg was inlined
	for _, u := range units {
		if strings.HasSuffix(u.OutPath, "main.go") {
			data, err := os.ReadFile(u.OutPath)
			if err != nil {
				t.Fatalf("reading main.go: %v", err)
			}
			out := string(data)
			// Method is pub so emits as Greet; default excited=false should be inlined
			if !strings.Contains(out, `g.Greet("Alice", false)`) {
				t.Errorf("expected default arg inlined in main.go, got:\n%s", out)
			}
			if !strings.Contains(out, `g.Greet("Bob", true)`) {
				t.Errorf("expected named arg reordered in main.go, got:\n%s", out)
			}
			return
		}
	}
	t.Error("main.go not found in transpile output")
}

// --- Cross-file end-to-end (transpile + compile + run) -----------------------

// crossFileE2ERun transpiles multi-file zinc code, writes a go.mod, and runs
// the resulting Go code. Returns trimmed stdout.
func crossFileE2ERun(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	for name, src := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0644)
	}

	units, err := Transpile(dir)
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	if len(units) == 0 {
		t.Fatal("no transpile units produced")
	}

	// Write go.mod
	goMod := "module e2e\n\ngo 1.26\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644)

	// Collect .go files for go run
	var goFiles []string
	for _, u := range units {
		goFiles = append(goFiles, u.OutPath)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	raw, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Print all generated Go files for debugging
			var allGo string
			for _, u := range units {
				data, _ := os.ReadFile(u.OutPath)
				allGo += fmt.Sprintf("--- %s ---\n%s\n", u.OutPath, string(data))
			}
			t.Fatalf("go run failed.\ngenerated Go:\n%s\nstderr:\n%s", allGo, exitErr.Stderr)
		}
		t.Fatalf("go run: %v", err)
	}
	return strings.TrimSpace(string(raw))
}

func TestE2ECrossFileSuperArgs(t *testing.T) {
	out := crossFileE2ERun(t, map[string]string{
		"animal.zn": `Animal {
    String name
    String sound
    new(String name, String sound) {
        this.name = name
        this.sound = sound
    }
}
`,
		"dog.zn": `Dog : Animal {
    new(String name) {
        super(name, "Woof")
    }
}

main() {
    d := Dog("Rex")
    print(d.name)
    print(d.sound)
}
`,
	})
	if out != "Rex\nWoof" {
		t.Errorf("expected 'Rex\\nWoof', got:\n%s", out)
	}
}

func TestE2ECrossFileFailableDetection(t *testing.T) {
	out := crossFileE2ERun(t, map[string]string{
		"a.zn": `Int risky() {
    return Error("something went wrong")
}
`,
		"b.zn": `main() {
    x := risky() or {
        print("caught error")
        exit(0)
    }
    print(x)
}
`,
	})
	if out != "caught error" {
		t.Errorf("expected 'caught error', got:\n%s", out)
	}
}

func TestE2ECrossFileNamedArgs(t *testing.T) {
	out := crossFileE2ERun(t, map[string]string{
		"a.zn": `String greet(String name, String greeting = "Hello") {
    return "{greeting}, {name}!"
}
`,
		"b.zn": `main() {
    print(greet("Alice"))
    print(greet("Bob", greeting: "Hi"))
}
`,
	})
	if out != "Hello, Alice!\nHi, Bob!" {
		t.Errorf("expected 'Hello, Alice!\\nHi, Bob!', got:\n%s", out)
	}
}

func TestE2ECrossFileEnumMatch(t *testing.T) {
	out := crossFileE2ERun(t, map[string]string{
		"color.zn": `enum Color { Red, Green, Blue }
`,
		"main.zn": `String describe(Color c) {
    match c {
        case Color.Red -> { return "red" }
        case Color.Green -> { return "green" }
        case _ -> { return "other" }
    }
}

main() {
    print(describe(Color.Red))
    print(describe(Color.Green))
    print(describe(Color.Blue))
}
`,
	})
	if out != "red\ngreen\nother" {
		t.Errorf("expected 'red\\ngreen\\nother', got:\n%s", out)
	}
}

func TestE2ECrossFileInterfaceCompliance(t *testing.T) {
	out := crossFileE2ERun(t, map[string]string{
		"speaker.zn": `interface Speaker {
    pub String speak()
}
`,
		"dog.zn": `Dog : Speaker {
    String name
    new(String n) { this.name = n }
    pub String speak() { return "{this.name} says woof" }
}

main() {
    d := Dog("Rex")
    print(d.speak())
}
`,
	})
	if out != "Rex says woof" {
		t.Errorf("expected 'Rex says woof', got:\n%s", out)
	}
}

func TestE2ECrossFileMethodNamedArgs(t *testing.T) {
	out := crossFileE2ERun(t, map[string]string{
		"greeter.zn": `Greeter {
    String prefix
    new(String prefix) { this.prefix = prefix }
    pub String greet(String name, Bool excited = false) {
        if excited {
            return "{this.prefix} {name}!!!"
        }
        return "{this.prefix} {name}"
    }
}
`,
		"main.zn": `main() {
    g := Greeter("Hello")
    print(g.greet("Alice"))
    print(g.greet("Bob", excited: true))
}
`,
	})
	if out != "Hello Alice\nHello Bob!!!" {
		t.Errorf("expected 'Hello Alice\\nHello Bob!!!', got:\n%s", out)
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
