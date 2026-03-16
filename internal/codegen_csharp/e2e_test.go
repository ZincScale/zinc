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
)

const dotnetPath = "/home/vrjoshi/.dotnet/dotnet"

// e2eRun transpiles Zinc source to C#, creates a .NET console project,
// compiles and runs it, and returns trimmed stdout.
func e2eRun(t *testing.T, src string) string {
	t.Helper()

	// Check dotnet is available
	if _, err := os.Stat(dotnetPath); err != nil {
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
	cmd.Env = append(os.Environ(), "DOTNET_ROOT=/home/vrjoshi/.dotnet", "DOTNET_NOLOGO=1")
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
	restoreCmd.Env = append(os.Environ(), "DOTNET_ROOT=/home/vrjoshi/.dotnet", "DOTNET_NOLOGO=1")
	if out, err := restoreCmd.CombinedOutput(); err != nil {
		t.Fatalf("dotnet restore failed: %v\n%s", err, out)
	}

	// Run the project
	runCmd := exec.Command(dotnetPath, "run")
	runCmd.Dir = dir
	runCmd.Env = append(os.Environ(), "DOTNET_ROOT=/home/vrjoshi/.dotnet", "DOTNET_NOLOGO=1")
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
