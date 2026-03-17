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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"zinc/internal/config"
	"zinc/internal/lexer"
	"zinc/internal/parser"
)

// findDotnet locates the dotnet CLI binary.
func findDotnet() string {
	// Check PATH first
	if p, err := exec.LookPath("dotnet"); err == nil {
		return p
	}
	// Check common install locations
	home, _ := os.UserHomeDir()
	for _, candidate := range []string{
		home + "/.dotnet/dotnet",
		"/usr/local/bin/dotnet",
		"/usr/bin/dotnet",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// e2eRun transpiles Zinc source to C#, creates a .NET console project,
// compiles and runs it, and returns trimmed stdout.
func e2eRun(t *testing.T, src string) string {
	t.Helper()

	dotnetPath := findDotnet()
	if dotnetPath == "" {
		t.Skip("dotnet SDK not found, skipping E2E test")
	}

	csCode := transpile(src)
	if strings.HasPrefix(csCode, "PARSE ERROR") {
		t.Fatalf("transpile error: %s", csCode)
	}

	dir := t.TempDir()

	// Create a minimal .NET console project
	cmd := exec.Command(dotnetPath, "new", "console", "--force", "--no-restore")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dotnet new console failed: %v\n%s", err, out)
	}

	// Overwrite Program.cs with our generated C#
	if err := os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(csCode), 0644); err != nil {
		t.Fatalf("write Program.cs: %v", err)
	}

	// Restore dependencies
	restoreCmd := exec.Command(dotnetPath, "restore")
	restoreCmd.Dir = dir
	restoreCmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	if out, err := restoreCmd.CombinedOutput(); err != nil {
		t.Fatalf("dotnet restore failed: %v\n%s", err, out)
	}

	// Run the project
	runCmd := exec.Command(dotnetPath, "run")
	runCmd.Dir = dir
	runCmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	var stdout, stderr strings.Builder
	runCmd.Stdout = &stdout
	runCmd.Stderr = &stderr
	if err := runCmd.Run(); err != nil {
		t.Fatalf("dotnet run failed.\ngenerated C#:\n%s\nstderr:\n%s\nstdout:\n%s", csCode, stderr.String(), stdout.String())
	}
	return strings.TrimSpace(stdout.String())
}

// e2eRunResolved is like e2eRun but uses a CSharpTypeResolver to discover
// .NET types, enabling constructor calls like Stopwatch() → new Stopwatch().
func e2eRunResolved(t *testing.T, src string) string {
	t.Helper()

	dotnetPath := findDotnet()
	if dotnetPath == "" {
		t.Skip("dotnet SDK not found, skipping E2E test")
	}

	// Transpile with type resolver
	tokens := lexer.New(src).Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		t.Fatalf("parse error: %s", strings.Join(p.Errors, "; "))
	}

	resolver := NewCSharpTypeResolver()
	cfg := &config.Config{Name: "test", Target: "csharp"}
	if err := resolver.Probe(cfg); err != nil {
		t.Fatalf("type probe failed: %v", err)
	}

	gen := New()
	gen.SetTypeResolver(resolver)
	csCode := gen.Generate(prog)

	dir := t.TempDir()

	cmd := exec.Command(dotnetPath, "new", "console", "--force", "--no-restore")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dotnet new console failed: %v\n%s", err, out)
	}

	if err := os.WriteFile(filepath.Join(dir, "Program.cs"), []byte(csCode), 0644); err != nil {
		t.Fatalf("write Program.cs: %v", err)
	}

	restoreCmd := exec.Command(dotnetPath, "restore")
	restoreCmd.Dir = dir
	restoreCmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	if out, err := restoreCmd.CombinedOutput(); err != nil {
		t.Fatalf("dotnet restore failed: %v\n%s", err, out)
	}

	runCmd := exec.Command(dotnetPath, "run")
	runCmd.Dir = dir
	runCmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	var stdout, stderr strings.Builder
	runCmd.Stdout = &stdout
	runCmd.Stderr = &stderr
	if err := runCmd.Run(); err != nil {
		t.Fatalf("dotnet run failed.\ngenerated C#:\n%s\nstderr:\n%s\nstdout:\n%s", csCode, stderr.String(), stdout.String())
	}
	return strings.TrimSpace(stdout.String())
}

func assertOutput(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("expected output:\n%s\ngot:\n%s", want, got)
	}
}

// --- Basic -------------------------------------------------------------------

func TestE2EHelloWorld(t *testing.T) {
	out := e2eRun(t, `main() { print("Hello, World!") }`)
	assertOutput(t, out, "Hello, World!")
}

func TestE2EArithmetic(t *testing.T) {
	out := e2eRun(t, `main() { var x = 3 + 4 * 2; print(x) }`)
	assertOutput(t, out, "11")
}

func TestE2EStringInterpolation(t *testing.T) {
	out := e2eRun(t, `main() { var name = "Zinc"; print("Hello, {name}!") }`)
	assertOutput(t, out, "Hello, Zinc!")
}

func TestE2EIfElse(t *testing.T) {
	out := e2eRun(t, `
main() {
    var x = 10
    if x > 5 { print("big") } else { print("small") }
}
`)
	assertOutput(t, out, "big")
}

func TestE2EForLoop(t *testing.T) {
	out := e2eRun(t, `
main() {
    for var i = 0; i < 3; i += 1 {
        print(i)
    }
}
`)
	assertOutput(t, out, "0\n1\n2")
}

func TestE2EWhileLoop(t *testing.T) {
	out := e2eRun(t, `
main() {
    var x = 0
    while x < 3 {
        print(x)
        x += 1
    }
}
`)
	assertOutput(t, out, "0\n1\n2")
}

func TestE2EClass(t *testing.T) {
	out := e2eRun(t, `
Dog {
    pub String name

    new(String name) {
        this.name = name
    }

    pub String bark() {
        return "Woof!"
    }
}
main() {
    var d = Dog("Rex")
    print(d.bark())
}
`)
	assertOutput(t, out, "Woof!")
}

func TestE2EClassInheritance(t *testing.T) {
	out := e2eRun(t, `
Animal {
    pub String name
    new(String name) { this.name = name }
    pub String speak() { return "..." }
}
Dog : Animal {
    new(String name) { super(name) }
    pub String speak() { return "Woof!" }
}
main() {
    var d = Dog("Rex")
    print(d.speak())
}
`)
	assertOutput(t, out, "Woof!")
}

func TestE2EEnum(t *testing.T) {
	out := e2eRun(t, `
enum Color { Red, Green, Blue }
main() {
    var c = Color.Green
    print(c)
}
`)
	assertOutput(t, out, "Green")
}

func TestE2EMatchStmt(t *testing.T) {
	out := e2eRun(t, `
main() {
    var x = 2
    match x {
        case 1 -> { print("one") }
        case 2 -> { print("two") }
        case _ -> { print("other") }
    }
}
`)
	assertOutput(t, out, "two")
}

// --- LINQ Collection Methods -------------------------------------------------

func TestE2ELinqWhereSelect(t *testing.T) {
	out := e2eRun(t, `
main() {
    var nums = [1, 2, 3, 4, 5]
    var big = nums.Where((Int x) -> x > 3)
    var doubled = big.Select((Int x) -> x * 2)
    for item in doubled { print(item) }
}
`)
	assertOutput(t, out, "8\n10")
}

func TestE2ELinqFirstAny(t *testing.T) {
	out := e2eRun(t, `
main() {
    var nums = [10, 20, 30]
    var f = nums.First()
    var hasLarge = nums.Any((Int x) -> x > 25)
    print(f)
    print(hasLarge)
}
`)
	assertOutput(t, out, "10\nTrue")
}

func TestE2ELinqSumMinMax(t *testing.T) {
	out := e2eRun(t, `
main() {
    var nums = [3, 1, 4, 1, 5]
    print(nums.Sum())
    print(nums.Min())
    print(nums.Max())
}
`)
	assertOutput(t, out, "14\n1\n5")
}

func TestE2ELinqOrderByTake(t *testing.T) {
	out := e2eRun(t, `
main() {
    var nums = [5, 3, 1, 4, 2]
    var sorted = nums.OrderBy((Int x) -> x)
    var first3 = sorted.Take(3)
    for item in first3 { print(item) }
}
`)
	assertOutput(t, out, "1\n2\n3")
}

func TestE2ELinqDistinct(t *testing.T) {
	out := e2eRun(t, `
main() {
    var nums = [1, 2, 2, 3, 3, 3]
    var unique = nums.Distinct()
    for item in unique { print(item) }
}
`)
	assertOutput(t, out, "1\n2\n3")
}

func TestE2ELinqAggregate(t *testing.T) {
	out := e2eRun(t, `
main() {
    var nums = [1, 2, 3, 4]
    var sum = nums.Aggregate(0, (Int acc, Int x) -> acc + x)
    print(sum)
}
`)
	assertOutput(t, out, "10")
}

func TestE2ELinqChain(t *testing.T) {
	out := e2eRun(t, `
main() {
    var nums = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    var result = nums.Where((Int x) -> x > 3).Select((Int x) -> x * x).Take(3)
    for item in result { print(item) }
}
`)
	assertOutput(t, out, "16\n25\n36")
}

// --- Builtin Functions -------------------------------------------------------

func TestE2EBuiltinToString(t *testing.T) {
	out := e2eRun(t, `main() { var s = toString(42); print(s) }`)
	assertOutput(t, out, "42")
}

func TestE2EBuiltinToInt(t *testing.T) {
	out := e2eRun(t, `main() { var n = toInt("99"); print(n + 1) }`)
	assertOutput(t, out, "100")
}

func TestE2EBuiltinAbs(t *testing.T) {
	out := e2eRun(t, `
main() {
    var x = 0 - 7
    print(abs(x))
}
`)
	assertOutput(t, out, "7")
}

func TestE2EBuiltinSqrt(t *testing.T) {
	out := e2eRun(t, `main() { print(sqrt(16.0)) }`)
	assertOutput(t, out, "4")
}

func TestE2EBuiltinMaxMin(t *testing.T) {
	out := e2eRun(t, `
main() {
    print(max(3, 7))
    print(min(3, 7))
}
`)
	assertOutput(t, out, "7\n3")
}

func TestE2EBuiltinTypeOf(t *testing.T) {
	out := e2eRun(t, `main() { print(typeOf(42)) }`)
	assertOutput(t, out, "Int32")
}

func TestE2EBuiltinGetEnv(t *testing.T) {
	out := e2eRun(t, `
main() {
    setEnv("ZINC_TEST_VAR", "hello_zinc")
    var v = getEnv("ZINC_TEST_VAR")
    print(v)
}
`)
	assertOutput(t, out, "hello_zinc")
}

func TestE2EBuiltinReadWriteFile(t *testing.T) {
	out := e2eRun(t, `
main() {
    writeFile("_test_out.txt", "zinc_data") or { print(err) }
    var content = readFile("_test_out.txt") or { print(err) }
    print(content)
}
`)
	assertOutput(t, out, "zinc_data")
}

func TestE2EBuiltinJsonEncode(t *testing.T) {
	out := e2eRun(t, `main() { var j = jsonEncode(42); print(j) }`)
	assertOutput(t, out, "42")
}

func TestE2EBuiltinSprintf(t *testing.T) {
	src := "main() {\n    var pattern = `{0} is {1}`\n    var s = sprintf(pattern, \"age\", 30)\n    print(s)\n}"
	out := e2eRun(t, src)
	assertOutput(t, out, "age is 30")
}

// --- Import / NuGet ----------------------------------------------------------

func TestE2EImportSystemTextJson(t *testing.T) {
	out := e2eRun(t, `
import "System.Text.Json"
main() {
    var s = JsonSerializer.Serialize(42)
    print(s)
}
`)
	assertOutput(t, out, "42")
}

func TestE2EImportJsonShortcut(t *testing.T) {
	out := e2eRun(t, `
import "json"
main() {
    var s = JsonSerializer.Serialize("hello")
    print(s)
}
`)
	assertOutput(t, out, `"hello"`)
}

func TestE2EImportSystemDiagnostics(t *testing.T) {
	out := e2eRun(t, `
import "System.Diagnostics"
main() {
    var sw = Stopwatch.StartNew()
    sw.Stop()
    print("ok")
}
`)
	assertOutput(t, out, "ok")
}

func TestE2EImportMultiple(t *testing.T) {
	out := e2eRun(t, `
import "System.Text.Json"
import "System.IO"
main() {
    var j = JsonSerializer.Serialize(99)
    print(j)
}
`)
	assertOutput(t, out, "99")
}

// --- Type Resolver: constructor calls via .NET reflection --------------------

func TestE2EResolverStopwatchConstructor(t *testing.T) {
	out := e2eRunResolved(t, `
import "System.Diagnostics"
main() {
    var sw = Stopwatch()
    sw.Start()
    sw.Stop()
    print("ok")
}
`)
	assertOutput(t, out, "ok")
}

func TestE2EResolverHttpClient(t *testing.T) {
	out := e2eRunResolved(t, `
import "http"
main() {
    var client = HttpClient()
    print("created")
}
`)
	assertOutput(t, out, "created")
}

func TestE2EResolverStringBuilder(t *testing.T) {
	out := e2eRunResolved(t, `
import "System.Text"
main() {
    var sb = StringBuilder()
    sb.Append("hello")
    sb.Append(" world")
    print(sb.ToString())
}
`)
	assertOutput(t, out, "hello world")
}

func TestE2EResolverRandom(t *testing.T) {
	out := e2eRunResolved(t, `
main() {
    var r = Random()
    var n = r.Next(1, 100)
    if n > 0 { print("ok") } else { print("fail") }
}
`)
	assertOutput(t, out, "ok")
}
