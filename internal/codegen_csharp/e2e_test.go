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
	"fmt"
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
	if p, err := exec.LookPath("dotnet"); err == nil {
		return p
	}
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

// --- Test case definitions ---------------------------------------------------

type assertMode int

const (
	modeExact    assertMode = iota // exact match
	modeContains                   // all substrings must be present
)

type e2eCase struct {
	name     string
	src      string
	mode     assertMode
	expected []string // exact: [0] is full expected; contains: each is a substring
	resolved bool     // needs CSharpTypeResolver during transpilation
}

// e2eCases defines all E2E test cases. One Zinc source → one expected output.
var e2eCases = []e2eCase{
	// --- Basic ---
	{name: "HelloWorld", src: `main() { print("Hello, World!") }`,
		mode: modeExact, expected: []string{"Hello, World!"}},
	{name: "Arithmetic", src: `main() { var x = 3 + 4 * 2; print(x) }`,
		mode: modeExact, expected: []string{"11"}},
	{name: "StringInterpolation", src: `main() { var name = "Zinc"; print("Hello, {name}!") }`,
		mode: modeExact, expected: []string{"Hello, Zinc!"}},
	{name: "IfElse", src: `
main() {
    var x = 10
    if x > 5 { print("big") } else { print("small") }
}`, mode: modeExact, expected: []string{"big"}},
	{name: "ForLoop", src: `
main() {
    for var i = 0; i < 3; i += 1 {
        print(i)
    }
}`, mode: modeExact, expected: []string{"0\n1\n2"}},
	{name: "WhileLoop", src: `
main() {
    var x = 0
    while x < 3 {
        print(x)
        x += 1
    }
}`, mode: modeExact, expected: []string{"0\n1\n2"}},
	{name: "Class", src: `
Dog {
    pub String name
    new(String name) { this.name = name }
    pub String bark() { return "Woof!" }
}
main() {
    var d = Dog("Rex")
    print(d.bark())
}`, mode: modeExact, expected: []string{"Woof!"}},
	{name: "ClassInheritance", src: `
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
}`, mode: modeExact, expected: []string{"Woof!"}},

	// --- Enum & Match ---
	{name: "Enum", src: `
enum Color { Red, Green, Blue }
main() {
    var c = Color.Green
    print(c)
}`, mode: modeExact, expected: []string{"Green"}},
	{name: "MatchStmt", src: `
main() {
    var x = 2
    match x {
        case 1 -> { print("one") }
        case 2 -> { print("two") }
        case _ -> { print("other") }
    }
}`, mode: modeExact, expected: []string{"two"}},

	// --- LINQ ---
	{name: "LinqWhereSelect", src: `
main() {
    var nums = [1, 2, 3, 4, 5]
    var big = nums.Where((Int x) -> x > 3)
    var doubled = big.Select((Int x) -> x * 2)
    for item in doubled { print(item) }
}`, mode: modeExact, expected: []string{"8\n10"}},
	{name: "LinqFirstAny", src: `
main() {
    var nums = [10, 20, 30]
    var f = nums.First()
    var hasLarge = nums.Any((Int x) -> x > 25)
    print(f)
    print(hasLarge)
}`, mode: modeExact, expected: []string{"10\nTrue"}},
	{name: "LinqSumMinMax", src: `
main() {
    var nums = [3, 1, 4, 1, 5]
    print(nums.Sum())
    print(nums.Min())
    print(nums.Max())
}`, mode: modeExact, expected: []string{"14\n1\n5"}},
	{name: "LinqOrderByTake", src: `
main() {
    var nums = [5, 3, 1, 4, 2]
    var sorted = nums.OrderBy((Int x) -> x)
    var first3 = sorted.Take(3)
    for item in first3 { print(item) }
}`, mode: modeExact, expected: []string{"1\n2\n3"}},
	{name: "LinqDistinct", src: `
main() {
    var nums = [1, 2, 2, 3, 3, 3]
    var unique = nums.Distinct()
    for item in unique { print(item) }
}`, mode: modeExact, expected: []string{"1\n2\n3"}},
	{name: "LinqAggregate", src: `
main() {
    var nums = [1, 2, 3, 4]
    var sum = nums.Aggregate(0, (Int acc, Int x) -> acc + x)
    print(sum)
}`, mode: modeExact, expected: []string{"10"}},
	{name: "LinqChain", src: `
main() {
    var nums = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    var result = nums.Where((Int x) -> x > 3).Select((Int x) -> x * x).Take(3)
    for item in result { print(item) }
}`, mode: modeExact, expected: []string{"16\n25\n36"}},

	// --- Builtins ---
	{name: "BuiltinToString", src: `main() { var s = toString(42); print(s) }`,
		mode: modeExact, expected: []string{"42"}},
	{name: "BuiltinToInt", src: `main() { var n = toInt("99"); print(n + 1) }`,
		mode: modeExact, expected: []string{"100"}},
	{name: "BuiltinAbs", src: `
main() {
    var x = 0 - 7
    print(abs(x))
}`, mode: modeExact, expected: []string{"7"}},
	{name: "BuiltinSqrt", src: `main() { print(sqrt(16.0)) }`,
		mode: modeExact, expected: []string{"4"}},
	{name: "BuiltinMaxMin", src: `
main() {
    print(max(3, 7))
    print(min(3, 7))
}`, mode: modeExact, expected: []string{"7\n3"}},
	{name: "BuiltinTypeOf", src: `main() { print(typeOf(42)) }`,
		mode: modeExact, expected: []string{"Int32"}},
	{name: "BuiltinGetEnv", src: `
main() {
    setEnv("ZINC_TEST_VAR", "hello_zinc")
    var v = getEnv("ZINC_TEST_VAR")
    print(v)
}`, mode: modeExact, expected: []string{"hello_zinc"}},
	{name: "BuiltinReadWriteFile", src: `
main() {
    writeFile("_test_out.txt", "zinc_data") or { print(err) }
    var content = readFile("_test_out.txt") or { print(err) }
    print(content)
}`, mode: modeExact, expected: []string{"zinc_data"}},
	{name: "BuiltinJsonEncode", src: `main() { var j = jsonEncode(42); print(j) }`,
		mode: modeExact, expected: []string{"42"}},
	{name: "BuiltinSprintf", src: "main() {\n    var pattern = `{0} is {1}`\n    var s = sprintf(pattern, \"age\", 30)\n    print(s)\n}",
		mode: modeExact, expected: []string{"age is 30"}},

	// --- Import ---
	{name: "ImportSystemTextJson", src: `
import "System.Text.Json"
main() {
    var s = JsonSerializer.Serialize(42)
    print(s)
}`, mode: modeExact, expected: []string{"42"}},
	{name: "ImportJsonShortcut", src: `
import "json"
main() {
    var s = JsonSerializer.Serialize("hello")
    print(s)
}`, mode: modeExact, expected: []string{`"hello"`}},
	{name: "ImportSystemDiagnostics", src: `
import "System.Diagnostics"
main() {
    var sw = Stopwatch.StartNew()
    sw.Stop()
    print("ok")
}`, mode: modeExact, expected: []string{"ok"}},
	{name: "ImportMultiple", src: `
import "System.Text.Json"
import "System.IO"
main() {
    var j = JsonSerializer.Serialize(99)
    print(j)
}`, mode: modeExact, expected: []string{"99"}},

	// --- Resolver (need type introspection) ---
	{name: "ResolverStopwatch", resolved: true, src: `
import "System.Diagnostics"
main() {
    var sw = Stopwatch()
    sw.Start()
    sw.Stop()
    print("ok")
}`, mode: modeExact, expected: []string{"ok"}},
	{name: "ResolverHttpClient", resolved: true, src: `
import "http"
main() {
    var client = HttpClient()
    print("created")
}`, mode: modeExact, expected: []string{"created"}},
	{name: "ResolverStringBuilder", resolved: true, src: `
import "System.Text"
main() {
    var sb = StringBuilder()
    sb.Append("hello")
    sb.Append(" world")
    print(sb.ToString())
}`, mode: modeExact, expected: []string{"hello world"}},
	{name: "ResolverRandom", resolved: true, src: `
main() {
    var r = Random()
    var n = r.Next(1, 100)
    if n > 0 { print("ok") } else { print("fail") }
}`, mode: modeExact, expected: []string{"ok"}},

	// --- Annotations ---
	{name: "AnnotationOnClass", src: `
@Serializable
User {
    pub String name
    new(String name) { this.name = name }
    pub String greet() { return "Hi, {this.name}" }
}
main() {
    var u = User("Alice")
    print(u.greet())
}`, mode: modeExact, expected: []string{"Hi, Alice"}},
	{name: "AnnotationJsonPropertyName", resolved: true, src: `
import "System.Text.Json"
import "System.Text.Json.Serialization"
User {
    @JsonInclude
    @JsonPropertyName("user_name")
    pub String name
    new(String name) { this.name = name }
}
main() {
    var opts = JsonSerializerOptions()
    opts.IncludeFields = true
    var u = User("Bob")
    var json = JsonSerializer.Serialize(u, opts)
    print(json)
}`, mode: modeContains, expected: []string{"user_name", "Bob"}},

	// --- Trailing Lambdas ---
	{name: "TrailingLambdaWhere", src: `
main() {
    var nums = [5, 3, 8, 1, 9, 2]
    var big = nums.Where { it > 4 }.ToList()
    for n in big { print(n) }
}`, mode: modeContains, expected: []string{"5", "8", "9"}},
	{name: "TrailingLambdaChain", src: `
main() {
    var nums = [5, 3, 8, 1, 9, 2]
    var result = nums.Where { it > 3 }.Select { it * 2 }.OrderBy { it }.ToList()
    for n in result { print(n) }
}`, mode: modeContains, expected: []string{"10\n16\n18"}},
	{name: "TrailingLambdaAggregate", src: `
main() {
    var nums = [1, 2, 3, 4, 5]
    var sum = nums.Aggregate(0) { acc, x -> acc + x }
    print(sum)
}`, mode: modeContains, expected: []string{"15"}},
	{name: "TrailingLambdaOrderByTake", src: `
main() {
    var nums = [5, 3, 8, 1, 9, 2, 7]
    var top3 = nums.OrderBy { it }.Take(3).ToList()
    for n in top3 { print(n) }
}`, mode: modeContains, expected: []string{"1\n2\n3"}},

	// --- Data Classes ---
	{name: "DataClass", src: `
data User(pub String name, pub Int age)
main() {
    var u = User("Alice", 30)
    print(u)
}`, mode: modeContains, expected: []string{"User", "Alice", "30"}},
	{name: "DataClassWithMethod", src: `
data User(pub String name, pub Int age) {
    pub String greet() {
        return "Hello, {name}!"
    }
}
main() {
    var u = User("Alice", 30)
    print(u.greet())
}`, mode: modeContains, expected: []string{"Hello, Alice!"}},

	// --- P1: Implicit return (method) ---
	{name: "ImplicitReturnMethod", src: `
Calculator {
    pub Int square(Int x) { x * x }
    pub String describe(Int x) { "result: {x}" }
}
main() {
    var c = Calculator()
    print(c.square(7))
    print(c.describe(42))
}`, mode: modeExact, expected: []string{"49\nresult: 42"}},

	// --- P1: Expression if ---
	{name: "ExpressionIf", src: `
main() {
    var x = 10
    var label = if x > 0 { "positive" } else { "negative" }
    print(label)
    var y = -5
    var label2 = if y > 0 { "positive" } else { "negative" }
    print(label2)
}`, mode: modeExact, expected: []string{"positive\nnegative"}},

	{name: "ExpressionIfNested", src: `
main() {
    var x = 0
    var label = if x > 0 { "positive" } else if x == 0 { "zero" } else { "negative" }
    print(label)
}`, mode: modeExact, expected: []string{"zero"}},

	// --- P1: Expression match ---
	{name: "ExpressionMatch", src: `
main() {
    var status = 1
    var msg = match status {
        case 1 -> "running"
        case 2 -> "stopped"
        case _ -> "unknown"
    }
    print(msg)
}`, mode: modeExact, expected: []string{"running"}},

	// --- P2: Ranges ---
	{name: "RangeExclusive", src: `
main() {
    for i in 0..5 {
        print(i)
    }
}`, mode: modeExact, expected: []string{"0\n1\n2\n3\n4"}},

	{name: "RangeInclusive", src: `
main() {
    for i in 1..=3 {
        print(i)
    }
}`, mode: modeExact, expected: []string{"1\n2\n3"}},

	// --- Concurrency ---
	{name: "SpawnFuture", src: `
main() {
    var f = spawn { 42 }
    print(f.value)
}`, mode: modeExact, expected: []string{"42"}},

	{name: "SpawnTwoFutures", src: `
main() {
    var a = spawn { 10 }
    var b = spawn { 20 }
    print(a.value)
    print(b.value)
}`, mode: modeExact, expected: []string{"10\n20"}},

	{name: "ParallelBasic", src: `
main() {
    var nums = [1, 2, 3]
    var results = parallel(nums) { it * 10 }
    for r in results {
        print(r)
    }
}`, mode: modeContains, expected: []string{"10", "20", "30"}},

	{name: "LockUpdate", src: `
main() {
    var counter = Lock(0)
    counter.update { it + 1 }
    counter.update { it + 1 }
    counter.update { it + 1 }
    print(counter.value)
}`, mode: modeExact, expected: []string{"3"}},

}

// --- Batched E2E runner ------------------------------------------------------

// transpileCase transpiles a single test case to C#.
func transpileCase(tc e2eCase, resolver *CSharpTypeResolver) string {
	tokens := lexer.New(tc.src).Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		return "PARSE ERROR: " + strings.Join(p.Errors, "; ")
	}
	gen := New()
	if resolver != nil {
		gen.SetTypeResolver(resolver)
	}
	return gen.Generate(prog)
}

// wrapInNamespace wraps generated C# code in a unique namespace.
func wrapInNamespace(name, csCode string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("namespace ZincTest_%s\n{\n", name))
	for _, line := range strings.Split(csCode, "\n") {
		sb.WriteString("    " + line + "\n")
	}
	sb.WriteString("}\n")
	return sb.String()
}

// generateTestRunner creates the C# entry point that runs all tests and reports results.
func generateTestRunner(cases []e2eCase) string {
	var sb strings.Builder
	sb.WriteString(`using System;
using System.IO;

public class TestRunner
{
    static int passed = 0;
    static int failed = 0;

    static string Capture(Action<string[]> action)
    {
        var sw = new StringWriter();
        var original = Console.Out;
        Console.SetOut(sw);
        try
        {
            action(Array.Empty<string>());
        }
        catch (Exception ex)
        {
            Console.SetOut(original);
            return "EXCEPTION: " + ex.GetType().Name + ": " + ex.Message;
        }
        Console.SetOut(original);
        return sw.ToString().TrimEnd('\r', '\n');
    }

    static void AssertExact(string name, string expected, string actual)
    {
        if (expected == actual)
        {
            passed++;
            Console.Error.WriteLine(">>>PASS:" + name);
        }
        else
        {
            failed++;
            Console.Error.WriteLine(">>>FAIL:" + name);
            Console.Error.WriteLine("  expected: " + expected.Replace("\n", "\\n"));
            Console.Error.WriteLine("  actual:   " + actual.Replace("\n", "\\n"));
        }
    }

    static void AssertContains(string name, string actual, string substring)
    {
        if (actual.Contains(substring))
        {
            passed++;
            Console.Error.WriteLine(">>>PASS:" + name + "/" + substring.Replace("\n", "\\n"));
        }
        else
        {
            failed++;
            Console.Error.WriteLine(">>>FAIL:" + name + "/" + substring.Replace("\n", "\\n"));
            Console.Error.WriteLine("  expected to contain: " + substring.Replace("\n", "\\n"));
            Console.Error.WriteLine("  actual: " + actual.Replace("\n", "\\n"));
        }
    }

    public static void Main(string[] args)
    {
`)

	for _, tc := range cases {
		sb.WriteString(fmt.Sprintf("        // --- %s ---\n", tc.name))
		sb.WriteString(fmt.Sprintf("        {\n"))
		sb.WriteString(fmt.Sprintf("            var output = Capture(ZincTest_%s.Program.Main);\n", tc.name))
		switch tc.mode {
		case modeExact:
			escaped := strings.ReplaceAll(tc.expected[0], `\`, `\\`)
			escaped = strings.ReplaceAll(escaped, `"`, `\"`)
			escaped = strings.ReplaceAll(escaped, "\n", `\n`)
			sb.WriteString(fmt.Sprintf("            AssertExact(%q, \"%s\", output);\n", tc.name, escaped))
		case modeContains:
			for _, sub := range tc.expected {
				escaped := strings.ReplaceAll(sub, `\`, `\\`)
				escaped = strings.ReplaceAll(escaped, `"`, `\"`)
				escaped = strings.ReplaceAll(escaped, "\n", `\n`)
				sb.WriteString(fmt.Sprintf("            AssertContains(%q, output, \"%s\");\n", tc.name, escaped))
			}
		}
		sb.WriteString(fmt.Sprintf("        }\n\n"))
	}

	sb.WriteString(`        Console.Error.WriteLine(">>>DONE:" + passed + ":" + failed);
        if (failed > 0) Environment.Exit(1);
    }
}
`)
	return sb.String()
}

const csproj = `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>disable</Nullable>
    <StartupObject>TestRunner</StartupObject>
  </PropertyGroup>
</Project>
`

func TestE2E(t *testing.T) {
	dotnetPath := findDotnet()
	if dotnetPath == "" {
		t.Skip("dotnet SDK not found, skipping E2E tests")
	}

	// Set up type resolver once for resolved tests
	var resolver *CSharpTypeResolver
	needsResolver := false
	for _, tc := range e2eCases {
		if tc.resolved {
			needsResolver = true
			break
		}
	}
	if needsResolver {
		resolver = NewCSharpTypeResolver()
		cfg := config.DefaultConfig("test")
		if err := resolver.Probe(cfg); err != nil {
			t.Fatalf("type probe failed: %v", err)
		}
	}

	dir := t.TempDir()

	// Write .csproj
	if err := os.WriteFile(filepath.Join(dir, "ZincE2E.csproj"), []byte(csproj), 0644); err != nil {
		t.Fatalf("write csproj: %v", err)
	}

	// Transpile and write each test case
	csCodes := make(map[string]string)
	for _, tc := range e2eCases {
		var csCode string
		if tc.resolved && resolver != nil {
			csCode = transpileCase(tc, resolver)
		} else {
			csCode = transpileCase(tc, nil)
		}
		if strings.HasPrefix(csCode, "PARSE ERROR") {
			t.Fatalf("test %s: %s", tc.name, csCode)
		}
		csCodes[tc.name] = csCode

		wrapped := wrapInNamespace(tc.name, csCode)
		path := filepath.Join(dir, tc.name+".cs")
		if err := os.WriteFile(path, []byte(wrapped), 0644); err != nil {
			t.Fatalf("write %s.cs: %v", tc.name, err)
		}
	}

	// Write test runner
	runner := generateTestRunner(e2eCases)
	if err := os.WriteFile(filepath.Join(dir, "TestRunner.cs"), []byte(runner), 0644); err != nil {
		t.Fatalf("write TestRunner.cs: %v", err)
	}

	// Restore dependencies
	restoreCmd := exec.Command(dotnetPath, "restore")
	restoreCmd.Dir = dir
	restoreCmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	if out, err := restoreCmd.CombinedOutput(); err != nil {
		t.Fatalf("dotnet restore failed: %v\n%s", err, out)
	}

	// Build once
	buildCmd := exec.Command(dotnetPath, "build", "--no-restore", "-c", "Release")
	buildCmd.Dir = dir
	buildCmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("dotnet build failed: %v\n%s", err, out)
	}

	// Run all tests
	runCmd := exec.Command(dotnetPath, "run", "--no-build", "-c", "Release")
	runCmd.Dir = dir
	runCmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	var stdout, stderr strings.Builder
	runCmd.Stdout = &stdout
	runCmd.Stderr = &stderr

	runErr := runCmd.Run()
	stderrStr := stderr.String()

	// Parse results from stderr
	passCount := 0
	failCount := 0
	failedTests := make(map[string]string) // test name → error detail

	for _, line := range strings.Split(stderrStr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, ">>>PASS:") {
			passCount++
		} else if strings.HasPrefix(line, ">>>FAIL:") {
			failCount++
			testName := strings.TrimPrefix(line, ">>>FAIL:")
			failedTests[testName] = ""
		} else if strings.HasPrefix(line, "  ") && failCount > 0 {
			// Accumulate error detail for the last failed test
			for name := range failedTests {
				if failedTests[name] == "" || strings.HasSuffix(failedTests[name], "\n") {
					failedTests[name] += line
					break
				}
			}
		}
	}

	// Report results as Go subtests
	for _, tc := range e2eCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Check if any assertion for this test failed
			hasFail := false
			var failDetail strings.Builder
			for name, detail := range failedTests {
				if name == tc.name || strings.HasPrefix(name, tc.name+"/") {
					hasFail = true
					failDetail.WriteString(fmt.Sprintf("  %s: %s\n", name, detail))
				}
			}
			if hasFail {
				t.Errorf("test failed:\n%s\ngenerated C#:\n%s", failDetail.String(), csCodes[tc.name])
			}
		})
	}

	if runErr != nil && failCount == 0 {
		// Process failed but no test failures detected — unexpected
		t.Fatalf("dotnet run failed unexpectedly.\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderrStr)
	}

	t.Logf("E2E results: %d passed, %d failed (single dotnet run)", passCount, failCount)
}
