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
	"strings"
	"testing"

	"zinc/internal/lexer"
	"zinc/internal/parser"
)

// transpile is the test helper — tokenize → parse → C# codegen.
func transpile(src string) string {
	tokens := lexer.New(src).Tokenize()
	p := parser.New(tokens)
	prog := p.Parse()
	if len(p.Errors) > 0 {
		return "PARSE ERROR: " + strings.Join(p.Errors, "; ")
	}
	g := New()
	return g.Generate(prog)
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("output does not contain %q\n--- got ---\n%s", want, got)
	}
}

func assertNotContains(t *testing.T, got, notWant string) {
	t.Helper()
	if strings.Contains(got, notWant) {
		t.Errorf("output should not contain %q\n--- got ---\n%s", notWant, got)
	}
}

// === Core Language ===

func TestMain_HelloWorld(t *testing.T) {
	out := transpile(`
main() {
    print("hello world")
}
`)
	assertContains(t, out, "public class Program")
	assertContains(t, out, "public static void Main(string[] args)")
	assertContains(t, out, `Console.WriteLine("hello world")`)
}

func TestFunctionDecl(t *testing.T) {
	out := transpile(`
Int add(Int a, Int b) {
    return a + b
}
main() {
    var x = add(1, 2)
    print(x)
}
`)
	assertContains(t, out, "public static class Functions")
	assertContains(t, out, "int Add(int a, int b)")
	assertContains(t, out, "return (a + b);")
}

func TestMultipleFunctionsSingleClass(t *testing.T) {
	out := transpile(`
Int add(Int a, Int b) { return a + b }
Int multiply(Int a, Int b) { return a * b }
main() { print(add(1, 2)) }
`)
	// All functions should be in ONE Functions class
	assertContains(t, out, "public static class Functions")
	assertContains(t, out, "int Add(int a, int b)")
	assertContains(t, out, "int Multiply(int a, int b)")
	// Should NOT have multiple Functions class declarations
	count := strings.Count(out, "public static class Functions")
	if count != 1 {
		t.Errorf("expected exactly 1 Functions class, got %d\n--- output ---\n%s", count, out)
	}
}

func TestVariables(t *testing.T) {
	out := transpile(`
main() {
    var x = 42
    var name = "hello"
    print(x)
}
`)
	assertContains(t, out, "var x = 42;")
	assertContains(t, out, `var name = "hello";`)
}

func TestClassDecl(t *testing.T) {
	out := transpile(`
Dog {
    pub String name
    Int age = 0

    new(String name, Int age = 0) {
        this.name = name
        this.age = age
    }

    pub String bark() {
        return "Woof!"
    }
}
main() {
    var d = Dog(name: "Rex", age: 5)
    print(d.bark())
}
`)
	assertContains(t, out, "public class Dog")
	assertContains(t, out, "public string Name;")
	assertContains(t, out, "private int _age = 0;")
	assertContains(t, out, "public Dog(string name, int age = 0)")
	assertContains(t, out, "public string Bark()")
	assertContains(t, out, `return "Woof!";`)
	assertContains(t, out, "new Dog(name: ")
}

func TestClassInheritance(t *testing.T) {
	out := transpile(`
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
    var d = Dog(name: "Rex")
    print(d.speak())
}
`)
	assertContains(t, out, "public class Animal")
	assertContains(t, out, "public class Dog : Animal")
	assertContains(t, out, ": base(name)")
}

func TestInterface(t *testing.T) {
	out := transpile(`
interface Speaker {
    pub String speak()
}
Cat : Speaker {
    new() {}
    pub String speak() { return "Meow!" }
}
main() {
    var c = Cat()
    print(c.speak())
}
`)
	assertContains(t, out, "public interface ISpeaker")
	assertContains(t, out, "string Speak();")
	assertContains(t, out, "public class Cat : ISpeaker")
}

func TestIfElse(t *testing.T) {
	out := transpile(`
main() {
    var x = 10
    if x > 5 { print("big") } else { print("small") }
}
`)
	assertContains(t, out, "if ((x > 5))")
	assertContains(t, out, "else")
}

func TestForRange(t *testing.T) {
	out := transpile(`
main() {
    var items = [1, 2, 3]
    for item in items { print(item) }
}
`)
	assertContains(t, out, "foreach (var item in items)")
}

func TestForRangeWithIndex(t *testing.T) {
	out := transpile(`
main() {
    var items = ["a", "b", "c"]
    for i, item in items { print(i) }
}
`)
	assertContains(t, out, "for (int i = 0; i < items.Count; i++)")
	assertContains(t, out, "var item = items[i];")
}

func TestWhileLoop(t *testing.T) {
	out := transpile(`
main() {
    var x = 0
    while x < 10 { x += 1 }
}
`)
	assertContains(t, out, "while ((x < 10))")
}

func TestMapLiteral(t *testing.T) {
	out := transpile(`
main() {
    var scores = {"Alice": 90, "Bob": 85}
    print(scores)
}
`)
	assertContains(t, out, "new Dictionary<string, int>")
	assertContains(t, out, `{ "Alice", 90 }`)
}

func TestStringInterpolation(t *testing.T) {
	out := transpile(`
main() {
    var name = "world"
    print("hello {name}!")
}
`)
	assertContains(t, out, `$"hello {name}!"`)
}

func TestEnum(t *testing.T) {
	out := transpile(`
enum Color { Red, Green, Blue }
main() { var c = Color.Red }
`)
	assertContains(t, out, "public enum Color")
	assertContains(t, out, "Red,")
	assertContains(t, out, "Green,")
	assertContains(t, out, "Blue")
}

func TestErrorHandling(t *testing.T) {
	out := transpile(`
main() {
    var x = riskyCall() or { print("failed") }
}
Int riskyCall() { return 42 }
`)
	assertContains(t, out, "try")
	assertContains(t, out, "catch (Exception _err")
	assertContains(t, out, ".Message;")
	assertContains(t, out, "throw;")
}

func TestBuiltinListMethods(t *testing.T) {
	out := transpile(`
main() {
    var items = [1, 2, 3]
    items.Add(4)
    items.Remove(2)
    items.Clear()
    print(items)
}
`)
	assertContains(t, out, "items.Add(4)")
	assertContains(t, out, "items.Remove(2)")
	assertContains(t, out, "items.Clear()")
}

func TestBuiltinStringMethods(t *testing.T) {
	out := transpile(`
main() {
    var s = "Hello World"
    var u = s.ToUpper()
    var l = s.ToLower()
    var tr = s.Trim()
    var idx = s.IndexOf("World")
    print(u)
}
`)
	assertContains(t, out, "s.ToUpper()")
	assertContains(t, out, "s.ToLower()")
	assertContains(t, out, "s.Trim()")
	assertContains(t, out, `s.IndexOf("World")`)
}

func TestBuiltinMapMethods(t *testing.T) {
	out := transpile(`
main() {
    var m = {"a": 1}
    var k = m.Keys()
    var v = m.Values()
    print(k)
}
`)
	assertContains(t, out, "m.Keys.ToList()")
	assertContains(t, out, "m.Values.ToList()")
	assertContains(t, out, "using System.Linq;")
}

func TestLinqWhere(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3, 4, 5]
    var evens = nums.Where((Int x) -> x > 2)
    print(evens)
}
`)
	assertContains(t, out, ".Where(x => (x > 2)).ToList()")
	assertContains(t, out, "using System.Linq;")
}

func TestLinqSelect(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3]
    var doubled = nums.Select((Int x) -> x * 2)
    print(doubled)
}
`)
	assertContains(t, out, ".Select(x => (x * 2)).ToList()")
}

func TestLinqFirst(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3]
    var f = nums.First()
    var g = nums.First((Int x) -> x > 1)
    print(f)
}
`)
	assertContains(t, out, ".First()")
	assertContains(t, out, ".First(x => (x > 1))")
}

func TestLinqAnyAll(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3]
    var hasAny = nums.Any((Int x) -> x > 2)
    var allPos = nums.All((Int x) -> x > 0)
    print(hasAny)
}
`)
	assertContains(t, out, ".Any(x => (x > 2))")
	assertContains(t, out, ".All(x => (x > 0))")
}

func TestLinqSumMinMax(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3]
    var total = nums.Sum()
    var lo = nums.Min()
    var hi = nums.Max()
    print(total)
}
`)
	assertContains(t, out, ".Sum()")
	assertContains(t, out, ".Min()")
	assertContains(t, out, ".Max()")
}

func TestLinqOrderBy(t *testing.T) {
	out := transpile(`
main() {
    var nums = [3, 1, 2]
    var sorted = nums.OrderBy((Int x) -> x)
    var desc = nums.OrderByDescending((Int x) -> x)
    print(sorted)
}
`)
	assertContains(t, out, ".OrderBy(x => x).ToList()")
	assertContains(t, out, ".OrderByDescending(x => x).ToList()")
}

func TestLinqTakeSkip(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3, 4, 5]
    var first3 = nums.Take(3)
    var rest = nums.Skip(2)
    print(first3)
}
`)
	assertContains(t, out, ".Take(3).ToList()")
	assertContains(t, out, ".Skip(2).ToList()")
}

func TestLinqDistinct(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 2, 3, 3]
    var unique = nums.Distinct()
    print(unique)
}
`)
	assertContains(t, out, ".Distinct().ToList()")
}

func TestLinqAggregate(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3]
    var sum = nums.Aggregate(0, (Int acc, Int x) -> acc + x)
    print(sum)
}
`)
	assertContains(t, out, ".Aggregate(0, (acc, x) => (acc + x))")
}

func TestLinqToDictionary(t *testing.T) {
	out := transpile(`
main() {
    var names = ["alice", "bob"]
    var dict = names.ToDictionary((String s) -> s, (String s) -> s.Length())
    print(dict)
}
`)
	assertContains(t, out, ".ToDictionary(s => s, s => s.Length)")
}

func TestLinqForEach(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3]
    nums.ForEach((Int x) -> x * 2)
    print("done")
}
`)
	assertContains(t, out, ".ForEach(x => (x * 2))")
}

func TestLambda(t *testing.T) {
	out := transpile(`
main() {
    var f = (Int x) -> x * 2
    print(f)
}
`)
	assertContains(t, out, "x => (x * 2)")
}

func TestMatchStmt(t *testing.T) {
	out := transpile(`
main() {
    var x = 1
    match x {
        case 1 -> { print("one") }
        case 2 -> { print("two") }
        case _ -> { print("other") }
    }
}
`)
	assertContains(t, out, "switch (x)")
	assertContains(t, out, "case 1:")
	assertContains(t, out, "case 2:")
	assertContains(t, out, "default:")
	assertContains(t, out, "break;")
}

func TestSafeNavigation(t *testing.T) {
	out := transpile(`
main() {
    var x = null
    var y = x?.name
}
`)
	assertContains(t, out, "x?.Name")
}

func TestGenericClass(t *testing.T) {
	out := transpile(`
Box<T> {
    pub T value
    new(T value) { this.value = value }
    pub T get() { return this.value }
}
main() {
    var b = Box<Int>(42)
    print(b.get())
}
`)
	assertContains(t, out, "public class Box<T>")
	assertContains(t, out, "public T Value;")
	assertContains(t, out, "public T Get()")
}

func TestWithStatement(t *testing.T) {
	out := transpile(`
main() {
    with (f = openFile("test.txt")) {
        print(f)
    }
}
`)
	assertContains(t, out, "using (var f = ")
}

func TestGoStatement(t *testing.T) {
	out := transpile(`
main() {
    go { print("async") }
}
`)
	assertContains(t, out, "Task.Run(() =>")
	assertContains(t, out, "using System.Threading.Tasks;")
}

func TestConstDecl(t *testing.T) {
	out := transpile(`
pub const Int MAX = 100
main() { print(MAX) }
`)
	assertContains(t, out, "public const int MAX = 100;")
}

func TestTypeMapping(t *testing.T) {
	out := transpile(`
main() {
    List<Int> nums = [1, 2, 3]
    Map<String, Int> scores = {"a": 1}
    print(nums)
}
`)
	assertContains(t, out, "List<int>")
	assertContains(t, out, "Dictionary<string, int>")
	assertContains(t, out, "using System.Collections.Generic;")
}

func TestConstructorCallUsesNew(t *testing.T) {
	out := transpile(`
Point {
    pub Int x
    pub Int y
    new(Int x, Int y) { this.x = x; this.y = y }
}
main() {
    var p = Point(1, 2)
    print(p)
}
`)
	assertContains(t, out, "new Point(1, 2)")
}

func TestPrivateFieldNaming(t *testing.T) {
	out := transpile(`
Secret {
    String hidden
    new(String hidden) { this.hidden = hidden }
}
main() { var s = Secret("shh") }
`)
	assertContains(t, out, "private string _hidden;")
}

func TestUsingsIncluded(t *testing.T) {
	out := transpile(`
main() {
    print("hello")
}
`)
	assertContains(t, out, "using System;")
}

// === Builtin Functions ===

func TestBuiltinToString(t *testing.T) {
	out := transpile(`main() { var s = toString(42); print(s) }`)
	assertContains(t, out, "(42).ToString()")
}

func TestBuiltinToInt(t *testing.T) {
	out := transpile(`main() { var n = toInt("42"); print(n) }`)
	assertContains(t, out, `int.Parse("42")`)
}

func TestBuiltinParseInt(t *testing.T) {
	out := transpile(`main() { var n = parseInt("42"); print(n) }`)
	assertContains(t, out, `int.Parse("42")`)
}

func TestBuiltinToFloat(t *testing.T) {
	out := transpile(`main() { var f = toFloat("3.14"); print(f) }`)
	assertContains(t, out, `double.Parse("3.14")`)
}

func TestBuiltinParseFloat(t *testing.T) {
	out := transpile(`main() { var f = parseFloat("3.14"); print(f) }`)
	assertContains(t, out, `double.Parse("3.14")`)
}

func TestBuiltinToBool(t *testing.T) {
	out := transpile(`main() { var b = toBool("true"); print(b) }`)
	assertContains(t, out, `bool.Parse("true")`)
}

func TestBuiltinTypeOf(t *testing.T) {
	out := transpile(`main() { var t = typeOf(42); print(t) }`)
	assertContains(t, out, "(42).GetType().Name")
}

func TestBuiltinAbs(t *testing.T) {
	out := transpile(`main() { var a = abs(-5); print(a) }`)
	assertContains(t, out, "Math.Abs(")
}

func TestBuiltinSqrt(t *testing.T) {
	out := transpile(`main() { var s = sqrt(16.0); print(s) }`)
	assertContains(t, out, "Math.Sqrt(16.0)")
}

func TestBuiltinPow(t *testing.T) {
	out := transpile(`main() { var p = pow(2.0, 3.0); print(p) }`)
	assertContains(t, out, "Math.Pow(2.0, 3.0)")
}

func TestBuiltinFloor(t *testing.T) {
	out := transpile(`main() { var f = floor(3.7); print(f) }`)
	assertContains(t, out, "Math.Floor(3.7)")
}

func TestBuiltinCeil(t *testing.T) {
	out := transpile(`main() { var c = ceil(3.2); print(c) }`)
	assertContains(t, out, "Math.Ceiling(3.2)")
}

func TestBuiltinRound(t *testing.T) {
	out := transpile(`main() { var r = round(3.5); print(r) }`)
	assertContains(t, out, "Math.Round(3.5)")
}

func TestBuiltinMaxMin(t *testing.T) {
	out := transpile(`main() { var a = max(3, 5); var b = min(3, 5); print(a) }`)
	assertContains(t, out, "Math.Max(3, 5)")
	assertContains(t, out, "Math.Min(3, 5)")
}

func TestBuiltinPanic(t *testing.T) {
	out := transpile(`main() { panic("boom") }`)
	assertContains(t, out, `throw new Exception("boom")`)
}

func TestBuiltinExit(t *testing.T) {
	out := transpile(`main() { exit(1) }`)
	assertContains(t, out, "Environment.Exit(1)")
}

func TestBuiltinGetEnv(t *testing.T) {
	out := transpile(`main() { var h = getEnv("HOME"); print(h) }`)
	assertContains(t, out, `Environment.GetEnvironmentVariable("HOME")`)
}

func TestBuiltinSetEnv(t *testing.T) {
	out := transpile(`main() { setEnv("FOO", "bar") }`)
	assertContains(t, out, `Environment.SetEnvironmentVariable("FOO", "bar")`)
}

func TestBuiltinNow(t *testing.T) {
	out := transpile(`main() { var t = now(); print(t) }`)
	assertContains(t, out, "DateTime.Now.ToString()")
}

func TestBuiltinSleep(t *testing.T) {
	out := transpile(`main() { sleep(100) }`)
	assertContains(t, out, "Thread.Sleep(100)")
	assertContains(t, out, "using System.Threading;")
}

func TestBuiltinSprintf(t *testing.T) {
	src := "main() { var s = sprintf(`{0} is {1}`, \"age\", 30); print(s) }"
	out := transpile(src)
	assertContains(t, out, "string.Format(")
}

func TestBuiltinJsonEncode(t *testing.T) {
	out := transpile(`main() { var j = jsonEncode(42); print(j) }`)
	assertContains(t, out, "JsonSerializer.Serialize(42)")
	assertContains(t, out, "using System.Text.Json;")
}

func TestBuiltinReadFile(t *testing.T) {
	out := transpile(`main() { var c = readFile("test.txt") or { print(err) }; print(c) }`)
	assertContains(t, out, `File.ReadAllText("test.txt")`)
	assertContains(t, out, "using System.IO;")
}

func TestBuiltinWriteFile(t *testing.T) {
	out := transpile(`main() { writeFile("test.txt", "hello") or { print(err) } }`)
	assertContains(t, out, `File.WriteAllText("test.txt", "hello")`)
}

func TestBuiltinHttpGet(t *testing.T) {
	out := transpile(`main() { var r = httpGet("http://example.com") or { print(err) }; print(r) }`)
	assertContains(t, out, `new HttpClient().GetStringAsync("http://example.com").Result`)
	assertContains(t, out, "using System.Net.Http;")
}

func TestBuiltinReadLine(t *testing.T) {
	out := transpile(`main() { var s = readLine(); print(s) }`)
	assertContains(t, out, "Console.ReadLine()")
}

func TestFailableWithOrHandler(t *testing.T) {
	out := transpile(`
main() {
    var content = readFile("data.txt") or { print(err) }
    print(content)
}
`)
	assertContains(t, out, "try")
	assertContains(t, out, "catch (Exception _err")
	assertContains(t, out, ".Message;")
	assertContains(t, out, "throw;")
}

func TestFailableExprStmtWithOrHandler(t *testing.T) {
	out := transpile(`
main() {
    writeFile("out.txt", "data") or { print(err) }
}
`)
	assertContains(t, out, "try")
	assertContains(t, out, `File.WriteAllText("out.txt", "data")`)
	assertContains(t, out, "catch (Exception _err")
	assertContains(t, out, ".Message;")
}

func TestHandlerHaltSuppressesThrow(t *testing.T) {
	out := transpile(`
main() {
    var content = readFile("data.txt") or { exit(1) }
    print(content)
}
`)
	assertContains(t, out, "Environment.Exit(1)")
	assertNotContains(t, out, "throw;")
}

// === Imports / NuGet ===

func TestImportDirectNamespace(t *testing.T) {
	out := transpile(`
import "Newtonsoft.Json"
main() {
    print("hello")
}
`)
	assertContains(t, out, "using Newtonsoft.Json;")
}

func TestImportShortAlias(t *testing.T) {
	out := transpile(`
import "http"
main() {
    print("hello")
}
`)
	assertContains(t, out, "using System.Net.Http;")
}

func TestImportJsonShortcut(t *testing.T) {
	out := transpile(`
import "json"
main() {
    print("hello")
}
`)
	assertContains(t, out, "using System.Text.Json;")
}

func TestImportWithAlias(t *testing.T) {
	out := transpile(`
import "Newtonsoft.Json" as nj
main() {
    print("hello")
}
`)
	assertContains(t, out, "using Newtonsoft.Json;")
}

func TestImportMultipleNamespaces(t *testing.T) {
	out := transpile(`
import "Newtonsoft.Json"
import "Serilog"
main() {
    print("hello")
}
`)
	assertContains(t, out, "using Newtonsoft.Json;")
	assertContains(t, out, "using Serilog;")
}

func TestImportLocalPackageSkipped(t *testing.T) {
	out := transpile(`
import "myapp/utils"
main() {
    print("hello")
}
`)
	assertNotContains(t, out, "using myapp")
}

func TestImportQualifiedCall(t *testing.T) {
	out := transpile(`
import "Newtonsoft.Json"
main() {
    var s = JsonConvert.SerializeObject(42)
    print(s)
}
`)
	assertContains(t, out, "using Newtonsoft.Json;")
	assertContains(t, out, "JsonConvert.SerializeObject(42)")
}

func TestImportSystemTextRegex(t *testing.T) {
	out := transpile(`
import "regex"
main() {
    print("hello")
}
`)
	assertContains(t, out, "using System.Text.RegularExpressions;")
}

func TestImportDotNetNamespaceDirect(t *testing.T) {
	out := transpile(`
import "System.Diagnostics"
main() {
    print("hello")
}
`)
	assertContains(t, out, "using System.Diagnostics;")
}

// === Annotations ===

func TestAnnotationOnClass(t *testing.T) {
	out := transpile(`
@Serializable
User {
    pub String name
    new(String name) { this.name = name }
}
main() { print("ok") }
`)
	assertContains(t, out, "[Serializable]")
	assertContains(t, out, "public class User")
}

func TestAnnotationWithArgs(t *testing.T) {
	out := transpile(`
@Table("users")
User {
    pub String name
    new(String name) { this.name = name }
}
main() { print("ok") }
`)
	assertContains(t, out, `[Table("users")]`)
}

func TestAnnotationOnField(t *testing.T) {
	out := transpile(`
User {
    @JsonPropertyName("user_name")
    pub String name
    new(String name) { this.name = name }
}
main() { print("ok") }
`)
	assertContains(t, out, `[JsonPropertyName("user_name")]`)
	assertContains(t, out, "public string Name;")
}

func TestAnnotationOnMethod(t *testing.T) {
	out := transpile(`
Controller {
    new() {}

    @HttpGet
    @Route("/api/hello")
    pub String hello() { return "hi" }
}
main() { print("ok") }
`)
	assertContains(t, out, "[HttpGet]")
	assertContains(t, out, `[Route("/api/hello")]`)
	assertContains(t, out, "public string Hello()")
}

func TestMultipleAnnotationsOnClass(t *testing.T) {
	out := transpile(`
@Serializable
@Table("products")
Product {
    @Column("product_name")
    pub String name
    new(String name) { this.name = name }
}
main() { print("ok") }
`)
	assertContains(t, out, "[Serializable]")
	assertContains(t, out, `[Table("products")]`)
	assertContains(t, out, `[Column("product_name")]`)
}

func TestAnnotationMultipleArgs(t *testing.T) {
	out := transpile(`
@Authorize("admin", "editor")
Controller {
    new() {}
}
main() { print("ok") }
`)
	assertContains(t, out, `[Authorize("admin", "editor")]`)
}

func TestAnnotationNoArgsOnField(t *testing.T) {
	out := transpile(`
User {
    @Required
    pub String name
    new(String name) { this.name = name }
}
main() { print("ok") }
`)
	assertContains(t, out, "[Required]")
}

func TestAnnotationOnFunction(t *testing.T) {
	out := transpile(`
@Obsolete("use newMethod instead")
String oldMethod() { return "old" }
main() { print("ok") }
`)
	assertContains(t, out, `[Obsolete("use newMethod instead")]`)
	assertContains(t, out, "string OldMethod()")
}

// === Trailing Lambdas + it ===

func TestTrailingLambda_ImplicitIt(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3, 4, 5]
    var big = nums.Where { it > 3 }
    print(big)
}
`)
	assertContains(t, out, "nums.Where(it => (it > 3))")
}

func TestTrailingLambda_Select(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3]
    var doubled = nums.Select { it * 2 }
    print(doubled)
}
`)
	assertContains(t, out, "nums.Select(it => (it * 2))")
}

func TestTrailingLambda_Chain(t *testing.T) {
	out := transpile(`
main() {
    var nums = [5, 3, 8, 1, 9]
    var result = nums.Where { it > 3 }.Select { it * 2 }.OrderBy { it }
    print(result)
}
`)
	assertContains(t, out, ".Where(it => (it > 3))")
	assertContains(t, out, ".Select(it => (it * 2))")
	assertContains(t, out, ".OrderBy(it => it)")
}

func TestTrailingLambda_WithArgs(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3, 4, 5]
    var sum = nums.Aggregate(0) { acc, x -> acc + x }
    print(sum)
}
`)
	assertContains(t, out, "nums.Aggregate(0, (acc, x) => (acc + x))")
}

func TestTrailingLambda_ExplicitParams(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3]
    var sum = nums.Aggregate(0) { a, b -> a + b }
    print(sum)
}
`)
	assertContains(t, out, "(a, b) => (a + b)")
}

func TestTrailingLambda_FieldAccess(t *testing.T) {
	out := transpile(`
User {
    pub String name
    pub Int age
    new(String name, Int age) { this.name = name; this.age = age }
}
main() {
    var users = [User("Alice", 30), User("Bob", 25)]
    var names = users.Where { it.age > 28 }.Select { it.name }
    print(names)
}
`)
	assertContains(t, out, ".Where(it => (it.Age > 28))")
	assertContains(t, out, ".Select(it => it.Name)")
}

// === Data Classes ===

func TestDataClass_Simple(t *testing.T) {
	out := transpile(`
data User(pub String name, pub Int age)
main() {
    var u = User("Alice", 30)
    print(u)
}
`)
	assertContains(t, out, "public record User(string Name, int Age);")
	assertContains(t, out, "new User")
}

func TestDataClass_WithMethods(t *testing.T) {
	out := transpile(`
data User(pub String name, pub Int age) {
    pub String greet() {
        return "Hello, I am {name}"
    }
}
main() {
    var u = User("Alice", 30)
    print(u.greet())
}
`)
	assertContains(t, out, "public record User(string Name, int Age)")
	assertContains(t, out, "public string Greet()")
}

func TestDataClass_PrivateFields(t *testing.T) {
	out := transpile(`
data Point(Int x, Int y)
main() {
    var p = Point(1, 2)
    print(p)
}
`)
	assertContains(t, out, "public record Point(int X, int Y);")
}

// === Concurrency ===

func TestSpawn_EmitsZincFuture(t *testing.T) {
	out := transpile(`
main() {
    var f = spawn {
        42
    }
    print(f.Value)
}
`)
	assertContains(t, out, "new ZincFuture<dynamic>(Task.Run(")
	assertContains(t, out, "public class ZincFuture<T>")
	assertContains(t, out, "using System.Threading.Tasks;")
}

func TestParallel_EmitsTaskWhenAll(t *testing.T) {
	out := transpile(`
main() {
    var nums = [1, 2, 3]
    var results = parallel(nums) { it * 2 }
    print(results)
}
`)
	assertContains(t, out, "Task.WhenAll(")
	assertContains(t, out, "Task.Run(")
	assertContains(t, out, "using System.Threading.Tasks;")
}

func TestLock_EmitsZincLock(t *testing.T) {
	out := transpile(`
main() {
    var counter = Lock(0)
    print(counter.Value)
}
`)
	assertContains(t, out, "new ZincLock<dynamic>(0)")
	assertContains(t, out, "public class ZincLock<T>")
}

func TestConcurrencyHelpers_NotEmittedWhenUnused(t *testing.T) {
	out := transpile(`
main() {
    print("no concurrency")
}
`)
	assertNotContains(t, out, "ZincFuture")
	assertNotContains(t, out, "ZincLock")
}

func TestDataClass_WithParent(t *testing.T) {
	out := transpile(`
interface Printable {
    String display()
}
data User(pub String name) : Printable {
    pub String display() {
        return name
    }
}
main() {
    var u = User("Alice")
    print(u.display())
}
`)
	assertContains(t, out, "public record User(string Name) : IPrintable")
}
