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

package codegen_java

import (
	"strings"
	"testing"

	"zinc/internal/lexer"
	"zinc/internal/parser"
)

func transpile(src string) string {
	lex := lexer.New(src)
	tokens := lex.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		return "PARSE_ERRORS: " + strings.Join(p.Errors, "; ")
	}
	gen := New()
	return gen.Generate(prog, "Test")
}

func assertContains(t *testing.T, src string, expected ...string) {
	t.Helper()
	result := transpile(src)
	for _, exp := range expected {
		if !strings.Contains(result, exp) {
			t.Errorf("expected output to contain %q\ngot:\n%s", exp, result)
		}
	}
}

func assertNotContains(t *testing.T, src string, unexpected ...string) {
	t.Helper()
	result := transpile(src)
	for _, unexp := range unexpected {
		if strings.Contains(result, unexp) {
			t.Errorf("expected output NOT to contain %q\ngot:\n%s", unexp, result)
		}
	}
}

// =============================================================================
// Script mode + variables
// =============================================================================

func TestScriptModeHelloWorld(t *testing.T) {
	assertContains(t,
		`print("Hello, world!")`,
		`public class Test {`,
		`public static void main(String[] args) throws Exception {`,
		`System.out.println("Hello, world!");`,
	)
}

func TestVarInferred(t *testing.T) {
	assertContains(t,
		`var name = "Alice"`,
		`var name = "Alice";`,
	)
}

func TestVarExplicitType(t *testing.T) {
	assertContains(t,
		`var int age = 30`,
		`int age = 30;`,
	)
}

func TestVarStringType(t *testing.T) {
	assertContains(t,
		`var String greeting = "hi"`,
		`String greeting = "hi";`,
	)
}

func TestVarBoolType(t *testing.T) {
	assertContains(t,
		`var boolean active = true`,
		`boolean active = true;`,
	)
}

func TestVarFloatType(t *testing.T) {
	assertContains(t,
		`var double pi = 3.14`,
		`double pi = 3.14;`,
	)
}

// --- var elision tests (Type name = expr without var keyword) ---

func TestTypedVarNoKeyword(t *testing.T) {
	assertContains(t, `int age = 30`, `int age = 30;`)
}

func TestTypedVarNoKeywordString(t *testing.T) {
	assertContains(t, `String name = "Alice"`, `String name = "Alice";`)
}

func TestTypedVarNoKeywordGeneric(t *testing.T) {
	assertContains(t, `List<int> scores = []`, `List<Integer> scores = new java.util.ArrayList<>();`)
}

func TestTypedVarNoKeywordMap(t *testing.T) {
	assertContains(t, `Map<String, int> ages = {}`, `Map<String, Integer> ages = new java.util.HashMap<>();`)
}

func TestConstVar(t *testing.T) {
	assertContains(t,
		`const PI = 3.14`,
		`final var PI = 3.14;`,
	)
}

func TestVarGenericList(t *testing.T) {
	assertContains(t,
		`var List<int> scores = []`,
		`List<Integer> scores = new java.util.ArrayList<>();`,
	)
}

func TestVarGenericMap(t *testing.T) {
	assertContains(t,
		`var Map<String, int> lookup = {}`,
		`Map<String, Integer> lookup = new java.util.HashMap<>();`,
	)
}

// =============================================================================
// String interpolation
// =============================================================================

func TestStringInterpolation(t *testing.T) {
	assertContains(t, `
var name = "Alice"
print("Hello, {name}!")
`,
		`"Hello, " + name + "!"`,
	)
}

// =============================================================================
// Functions
// =============================================================================

func TestFnBasic(t *testing.T) {
	assertContains(t, `
fn greet(String name): String {
    return "Hello, {name}!"
}
`,
		`public static String greet(String name) {`,
		`return "Hello, " + name + "!";`,
	)
}

func TestFnVoid(t *testing.T) {
	assertContains(t, `
fn sayHello() {
    print("Hello!")
}
`,
		`public static void sayHello() {`,
		`System.out.println("Hello!");`,
	)
}

func TestFnMultipleParams(t *testing.T) {
	assertContains(t, `
fn add(int a, int b): int {
    return a + b
}
`,
		`public static int add(int a, int b) {`,
		`return a + b;`,
	)
}

// =============================================================================
// Control flow
// =============================================================================

func TestIfElse(t *testing.T) {
	assertContains(t, `
if x > 10 {
    print("big")
} else {
    print("small")
}
`,
		`if (x > 10) {`,
		`System.out.println("big");`,
		`} else {`,
		`System.out.println("small");`,
	)
}

func TestForRange(t *testing.T) {
	assertContains(t, `
for item in items {
    print(item)
}
`,
		`for (var item : items) {`,
		`System.out.println(item);`,
	)
}

func TestWhileLoop(t *testing.T) {
	assertContains(t, `
while running {
    process()
}
`,
		`while (running) {`,
		`process();`,
	)
}

func TestMatchStmt(t *testing.T) {
	assertContains(t, `
match cmd {
    case "start" {
        run()
    }
    case _ {
        stop()
    }
}
`,
		`switch (cmd) {`,
		`case "start" -> {`,
		`default -> {`,
	)
}

func TestMatchRecordPattern(t *testing.T) {
	assertContains(t, `
match result {
    case Single(f) {
        print(f)
    }
    case Drop() {
        print("dropped")
    }
    case _ {
        print("other")
    }
}
`,
		`case Single(var f) -> {`,
		`case Drop _ -> {`,
		`default -> {`,
	)
}

func TestBreakContinue(t *testing.T) {
	assertContains(t, `
for x in items {
    if x == 0 {
        continue
    }
    if x < 0 {
        break
    }
}
`,
		`continue;`,
		`break;`,
	)
}

// =============================================================================
// Error handling
// =============================================================================

func TestReturnError(t *testing.T) {
	assertContains(t, `
fn risky(): int {
    return Error("something went wrong")
}
`,
		`throw new RuntimeException("something went wrong");`,
	)
}

func TestReturnErrorCustomType(t *testing.T) {
	assertContains(t, `
fn fetch(): String {
    return Error(NotFound("user not found"))
}
`,
		`throw new NotFound("user not found");`,
	)
}

func TestReturnErrorRethrow(t *testing.T) {
	assertContains(t, `
fn risky(): int {
    var x = doStuff() or {
        return Error(err)
    }
    return x
}
`,
		`throw err;`,
	)
}

func TestOrBlock(t *testing.T) {
	assertContains(t, `
var x = risky() or {
    print("failed")
}
`,
		`try { x = risky(); } catch (Exception err) {`,
		`System.out.println("failed");`,
	)
}

func TestOrOnExprStmt(t *testing.T) {
	assertContains(t, `
doSomething() or {
    print("failed")
}
`,
		`try { doSomething(); } catch (Exception err) {`,
		`System.out.println("failed");`,
	)
}

func TestOrMatch(t *testing.T) {
	assertContains(t, `
var user = fetchUser(id) or match err {
    case NotFound -> defaultUser
    case Timeout -> retry(id)
    case _ -> fallback
}
`,
		`try { user = fetchUser(id); }`,
		`catch (NotFound err) {`,
		`user = defaultUser;`,
		`catch (Timeout err) {`,
		`user = retry(id);`,
		`catch (Exception err) {`,
		`user = fallback;`,
	)
}

// =============================================================================
// Lambdas
// =============================================================================

func TestLambdaSingleParam(t *testing.T) {
	assertContains(t, `
items.filter(x -> x > 0)
`,
		`x -> x > 0`,
	)
}

func TestLambdaMultiParam(t *testing.T) {
	assertContains(t, `
items.reduce(0, (acc, x) -> acc + x)
`,
		`(acc, x) -> acc + x`,
	)
}

// =============================================================================
// Operators
// =============================================================================

func TestBooleanOperators(t *testing.T) {
	assertContains(t, `
if a && b {
    print("both")
}
`,
		`if (a && b) {`,
	)
}

func TestOrOperator(t *testing.T) {
	assertContains(t, `
if a || b {
    print("either")
}
`,
		`if (a || b) {`,
	)
}

func TestPowerOperator(t *testing.T) {
	assertContains(t, `
var result = x ** 2
`,
		`Math.pow(x, 2)`,
	)
}

func TestInOperator(t *testing.T) {
	assertContains(t, `
if item in items {
    print("found")
}
`,
		`items.contains(item)`,
	)
}

func TestStructuralEquality(t *testing.T) {
	assertContains(t, `
if a == b {
    print("equal")
}
`,
		`java.util.Objects.equals(a, b)`,
	)
}

func TestStructuralInequality(t *testing.T) {
	assertContains(t, `
if a != b {
    print("not equal")
}
`,
		`!java.util.Objects.equals(a, b)`,
	)
}

// =============================================================================
// Visibility
// =============================================================================

func TestPubFieldGeneratesGetterSetter(t *testing.T) {
	assertContains(t, `
class User {
    pub String name
}
`,
		`private String name;`,
		`public String getName()`,
		`public void setName(String name)`,
	)
}

func TestReadFieldGeneratesGetterOnly(t *testing.T) {
	result := transpile(`
class User {
    readonly String email
}
`)
	if !strings.Contains(result, "private String email;") {
		t.Errorf("expected private field\ngot:\n%s", result)
	}
	if !strings.Contains(result, "public String getEmail()") {
		t.Errorf("expected getter\ngot:\n%s", result)
	}
	if strings.Contains(result, "setEmail") {
		t.Errorf("read field should NOT have setter\ngot:\n%s", result)
	}
}

func TestInitFieldGeneratesGetterOnly(t *testing.T) {
	result := transpile(`
class User {
    init String id
}
`)
	if !strings.Contains(result, "private final String id;") {
		t.Errorf("expected private final field\ngot:\n%s", result)
	}
	if !strings.Contains(result, "public String getId()") {
		t.Errorf("expected getter\ngot:\n%s", result)
	}
	if strings.Contains(result, "setId") {
		t.Errorf("init field should NOT have setter\ngot:\n%s", result)
	}
}

func TestPrivateFieldNoAccessors(t *testing.T) {
	result := transpile(`
class Counter {
    var int count = 0
}
`)
	if !strings.Contains(result, "private int count = 0;") {
		t.Errorf("expected private field\ngot:\n%s", result)
	}
	if strings.Contains(result, "getCount") || strings.Contains(result, "setCount") {
		t.Errorf("private field should NOT have accessors\ngot:\n%s", result)
	}
}

func TestPubMethod(t *testing.T) {
	assertContains(t, `
class Service {
    pub fn process() {
        print("working")
    }
}
`,
		`public void process()`,
	)
}

func TestPrivateMethodDefault(t *testing.T) {
	assertContains(t, `
class Service {
    fn helper() {
        print("internal")
    }
}
`,
		`private void helper()`,
	)
}

func TestOverrideMethod(t *testing.T) {
	assertContains(t, `
class Dog : Animal {
    override fn speak(): String {
        return "Woof!"
    }
}
`,
		`@Override`,
		`public String speak()`,
	)
}

// =============================================================================
// it keyword
// =============================================================================

func TestItFilter(t *testing.T) {
	assertContains(t,
		`items.filter(it > 0)`,
		`_it -> _it > 0`,
	)
}

func TestItMap(t *testing.T) {
	assertContains(t,
		`items.map(it * 2)`,
		`_it -> _it * 2`,
	)
}

func TestItSelectorAccess(t *testing.T) {
	assertContains(t,
		`users.sortBy(it.age)`,
		`_it -> _it.age`,
	)
}

func TestItMethodCall(t *testing.T) {
	assertContains(t,
		`names.filter(it.startsWith("A"))`,
		`_it -> _it.startsWith("A")`,
	)
}

// =============================================================================
// Multi-file output
// =============================================================================

func TestGenerateFilesDataClass(t *testing.T) {
	src := `
data User(String name, int age)

print("hello")
`
	lex := lexer.New(src)
	tokens := lex.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		t.Fatalf("parse errors: %v", p.Errors)
	}

	gen := New()
	files := gen.GenerateFiles(prog, "Main")

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	// Find the files by name
	var userFile, mainFile *OutputFile
	for i := range files {
		switch files[i].Name {
		case "User.java":
			userFile = &files[i]
		case "Main.java":
			mainFile = &files[i]
		}
	}

	if userFile == nil {
		t.Fatal("expected User.java file")
	}
	if !strings.Contains(userFile.Content, "public record User(String name, int age)") {
		t.Errorf("User.java should contain record\ngot:\n%s", userFile.Content)
	}

	if mainFile == nil {
		t.Fatal("expected Main.java file")
	}
	if !strings.Contains(mainFile.Content, "public static void main(") {
		t.Errorf("Main.java should contain main\ngot:\n%s", mainFile.Content)
	}
}

func TestGenerateFilesEnum(t *testing.T) {
	src := `
enum Color {
    Red
    Green
    Blue
}

var c = Color.Red
`
	lex := lexer.New(src)
	tokens := lex.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		t.Fatalf("parse errors: %v", p.Errors)
	}

	gen := New()
	files := gen.GenerateFiles(prog, "Main")

	var colorFile *OutputFile
	for i := range files {
		if files[i].Name == "Color.java" {
			colorFile = &files[i]
		}
	}

	if colorFile == nil {
		t.Fatal("expected Color.java file")
	}
	if !strings.Contains(colorFile.Content, "public enum Color") {
		t.Errorf("Color.java should contain enum\ngot:\n%s", colorFile.Content)
	}
}

// =============================================================================
// Stream API codegen
// =============================================================================

func TestStreamFilter(t *testing.T) {
	assertContains(t,
		`items.filter(x -> x > 0)`,
		`.stream().filter(x -> x > 0).toList()`,
	)
}

func TestStreamMap(t *testing.T) {
	assertContains(t,
		`items.map(x -> x * 2)`,
		`.stream().map(x -> x * 2).toList()`,
	)
}

func TestStreamFilterMap(t *testing.T) {
	assertContains(t,
		`items.filter(x -> x > 0).map(x -> x * 2)`,
		`.stream().filter(x -> x > 0).map(x -> x * 2).toList()`,
	)
}

func TestStreamSortBy(t *testing.T) {
	assertContains(t,
		`users.sortBy(u -> u.age)`,
		`.stream().sorted(java.util.Comparator.comparing(u -> u.age)).toList()`,
	)
}

func TestStreamLimit(t *testing.T) {
	assertContains(t,
		`items.limit(10)`,
		`.stream().limit(10).toList()`,
	)
}

func TestStreamSum(t *testing.T) {
	assertContains(t,
		`numbers.sum()`,
		`.stream().mapToInt(Integer::intValue).sum()`,
	)
}

func TestStreamAnyMatch(t *testing.T) {
	assertContains(t,
		`items.anyMatch(x -> x > 0)`,
		`.stream().anyMatch(x -> x > 0)`,
	)
}

func TestStreamChainFilterMapSum(t *testing.T) {
	assertContains(t,
		`orders.filter(o -> o.active).map(o -> o.amount).sum()`,
		`.stream().filter(o -> o.active).map(o -> o.amount).mapToInt(Integer::intValue).sum()`,
	)
}

func TestStreamDistinct(t *testing.T) {
	assertContains(t,
		`items.distinct()`,
		`.stream().distinct().toList()`,
	)
}

func TestStreamForEach(t *testing.T) {
	assertContains(t,
		`items.forEach(x -> print(x))`,
		`.stream().forEach(x -> System.out.println(x))`,
	)
}

func TestStreamFilterWithIt(t *testing.T) {
	assertContains(t,
		`items.filter(it > 0)`,
		`.stream().filter(_it -> _it > 0).toList()`,
	)
}

func TestStreamGroupBy(t *testing.T) {
	assertContains(t,
		`users.groupBy(u -> u.role)`,
		`.stream().collect(java.util.stream.Collectors.groupingBy(u -> u.role))`,
	)
}

func TestStreamMapWithIt(t *testing.T) {
	assertContains(t,
		`items.map(it * 2)`,
		`.stream().map(_it -> _it * 2).toList()`,
	)
}

func TestStreamSortByWithIt(t *testing.T) {
	assertContains(t,
		`users.sortBy(it.age)`,
		`.stream().sorted(java.util.Comparator.comparing(_it -> _it.age)).toList()`,
	)
}

func TestStreamChainWithIt(t *testing.T) {
	assertContains(t,
		`orders.filter(it.active).map(it.amount).sum()`,
		`.stream().filter(_it -> _it.active).map(_it -> _it.amount).mapToInt(Integer::intValue).sum()`,
	)
}

func TestStreamFindFirst(t *testing.T) {
	assertContains(t,
		`items.findFirst(x -> x > 10)`,
		`.stream().filter(x -> x > 10).findFirst().orElse(null)`,
	)
}

// =============================================================================
// Tuples
// =============================================================================

func TestTupleLiteral(t *testing.T) {
	assertContains(t,
		`var point = (3, 5)`,
		`new Tuple2(3, 5)`,
	)
}

func TestTupleDestructuring(t *testing.T) {
	assertContains(t, `
var x, y = swap(1, 2)
print(x)
`,
		`var _tuple = swap(1, 2);`,
		`var x = _tuple._0();`,
		`var y = _tuple._1();`,
	)
}

func TestTupleRecordGenerated(t *testing.T) {
	result := transpile(`var point = (3, 5)`)
	if !strings.Contains(result, "record Tuple2<T0, T1>(T0 _0, T1 _1) {}") {
		t.Errorf("expected Tuple2 record\ngot:\n%s", result)
	}
}

func TestTuple3(t *testing.T) {
	assertContains(t,
		`var rgb = (255, 128, 0)`,
		`new Tuple3(255, 128, 0)`,
	)
}

// =============================================================================
// or {} error handling
// =============================================================================

func TestOrDefault(t *testing.T) {
	assertContains(t, `
var port = parsePort("8080") or 80
`,
		`try { port = parsePort("8080"); } catch (Exception err) {`,
		`port = 80;`,
	)
}

func TestOverrideGeneratesAnnotation(t *testing.T) {
	assertContains(t, `
class Dog : Animal {
    override fn toString(): String {
        return "Dog"
    }
}
`,
		`@Override`,
		`public String toString()`,
	)
}

// =============================================================================
// Package declarations
// =============================================================================

func TestPackageDeclaration(t *testing.T) {
	src := `
package com.example.myapp

print("hello")
`
	lex := lexer.New(src)
	tokens := lex.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		t.Fatalf("parse errors: %v", p.Errors)
	}
	if prog.Package == nil {
		t.Fatal("expected package declaration")
	}
	if prog.Package.Path != "com.example.myapp" {
		t.Errorf("expected package 'com.example.myapp', got %q", prog.Package.Path)
	}

	gen := New()
	result := gen.Generate(prog, "Main")
	if !strings.Contains(result, "package com.example.myapp;") {
		t.Errorf("expected package statement\ngot:\n%s", result)
	}
}

func TestImportDottedPath(t *testing.T) {
	assertContains(t,
		`import java.time.Instant`,
		`import java.time.Instant;`,
	)
}

func TestImportWildcard(t *testing.T) {
	assertContains(t,
		`import java.nio.file.*`,
		`import java.nio.file.*;`,
	)
}

// =============================================================================
// Safe navigation
// =============================================================================

func TestSafeNavField(t *testing.T) {
	assertContains(t,
		`var x = obj?.name`,
		`obj != null ? obj.name : null`,
	)
}

func TestSafeNavMethod(t *testing.T) {
	assertContains(t,
		`var x = obj?.toString()`,
		`obj != null ? obj.toString() : null`,
	)
}

// =============================================================================
// Sealed classes
// =============================================================================

// =============================================================================
// Concurrency
// =============================================================================

func TestSpawnExpr(t *testing.T) {
	assertContains(t, `
spawn {
    print("background")
}
`,
		`Thread.startVirtualThread(() -> {`,
	)
}

func TestSpawnAsExpr(t *testing.T) {
	assertContains(t, `
var future = spawn {
    compute()
}
`,
		`Thread.startVirtualThread(() -> {`,
	)
}

func TestParallelFor(t *testing.T) {
	assertContains(t, `
parallel for item in items {
    process(item)
}
`,
		`java.util.concurrent.StructuredTaskScope.open()`,
		`for (var item : items)`,
		`_scope.fork(() -> {`,
		`_scope.join()`,
	)
}

func TestParallelForOr(t *testing.T) {
	assertContains(t, `
parallel for item in items {
    process(item)
} or {
    print("failed")
}
`,
		`_scope.join();`,
		`} catch (Exception err) {`,
		`System.out.println("failed");`,
	)
}

func TestConcurrentOr(t *testing.T) {
	assertContains(t, `
concurrent {
    fetchUser(id)
    fetchOrders(id)
} or {
    print("task failed")
}
`,
		`_scope.join();`,
		`} catch (Exception err) {`,
		`System.out.println("task failed");`,
	)
}

func TestConcurrentWithResultsOr(t *testing.T) {
	assertContains(t, `
var (user, orders) = concurrent {
    fetchUser(id)
    fetchOrders(id)
} or {
    return Error("concurrent failed")
}
`,
		`_scope.join();`,
		`user = _task0.get();`,
		`} catch (Exception err) {`,
		`throw new RuntimeException("concurrent failed");`,
	)
}

func TestChannelType(t *testing.T) {
	assertContains(t,
		`var Channel<String> ch = Channel(100)`,
		`java.util.concurrent.ArrayBlockingQueue<String> ch = new java.util.concurrent.ArrayBlockingQueue(100)`,
	)
}

func TestLockStmt(t *testing.T) {
	assertContains(t, `
lock mu {
    counter = counter + 1
}
`,
		`mu.lock();`,
		`try {`,
		`} finally {`,
		`mu.unlock();`,
	)
}

func TestWithStmt(t *testing.T) {
	assertContains(t, `
with f = FileReader("data.txt") {
    print("reading")
}
`,
		`try (var f = new FileReader("data.txt"))`,
	)
}

func TestConcurrentStandalone(t *testing.T) {
	assertContains(t, `
concurrent {
    fetchUser(id)
    fetchOrders(id)
}
`,
		`java.util.concurrent.StructuredTaskScope.open()`,
		`_scope.fork(() -> { fetchUser(id); return null; });`,
		`_scope.fork(() -> { fetchOrders(id); return null; });`,
		`_scope.join();`,
	)
}

func TestConcurrentWithResults(t *testing.T) {
	assertContains(t, `
var (user, orders) = concurrent {
    fetchUser(id)
    fetchOrders(id)
}
`,
		`Object user;`,
		`Object orders;`,
		`java.util.concurrent.StructuredTaskScope.open()`,
		`var _task0 = _scope.fork(() -> fetchUser(id));`,
		`var _task1 = _scope.fork(() -> fetchOrders(id));`,
		`_scope.join();`,
		`user = _task0.get();`,
		`orders = _task1.get();`,
	)
}

func TestTimeoutBasic(t *testing.T) {
	assertContains(t, `
timeout(5000) {
    slowApi(request)
}
`,
		`java.util.concurrent.StructuredTaskScope.open()`,
		`_scope.fork(() -> {`,
		`_scope.joinUntil(java.time.Instant.now().plus(5000));`,
	)
}

func TestTimeoutWithOr(t *testing.T) {
	assertContains(t, `
timeout(5000) {
    slowApi(request)
} or {
    print("timed out")
}
`,
		`java.util.concurrent.StructuredTaskScope.open()`,
		`} catch (java.util.concurrent.TimeoutException err) {`,
		`System.out.println("timed out");`,
	)
}

func TestSealedClass(t *testing.T) {
	src := `
sealed class Shape {
    data Circle(double radius)
    data Rect(double width, double height)
}
`
	lex := lexer.New(src)
	tokens := lex.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		t.Fatalf("parse errors: %v", p.Errors)
	}

	gen := New()
	files := gen.GenerateFiles(prog, "Main")

	// Should generate: Shape.java (sealed interface), Circle.java, Rect.java
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	var shapeFile, circleFile, rectFile *OutputFile
	for i := range files {
		switch files[i].Name {
		case "Shape.java":
			shapeFile = &files[i]
		case "Circle.java":
			circleFile = &files[i]
		case "Rect.java":
			rectFile = &files[i]
		}
	}

	if shapeFile == nil {
		t.Fatal("expected Shape.java")
	}
	if !strings.Contains(shapeFile.Content, "sealed interface Shape permits Circle, Rect") {
		t.Errorf("Shape.java should be sealed interface\ngot:\n%s", shapeFile.Content)
	}

	if circleFile == nil {
		t.Fatal("expected Circle.java")
	}
	if !strings.Contains(circleFile.Content, "record Circle(double radius) implements Shape") {
		t.Errorf("Circle.java should implement Shape\ngot:\n%s", circleFile.Content)
	}

	if rectFile == nil {
		t.Fatal("expected Rect.java")
	}
	if !strings.Contains(rectFile.Content, "record Rect(double width, double height) implements Shape") {
		t.Errorf("Rect.java should implement Shape\ngot:\n%s", rectFile.Content)
	}
}

func TestReferenceIdentity(t *testing.T) {
	assertContains(t, `
if a === b {
    print("same object")
}
`,
		`a == b`,
	)
}

func TestReferenceNonIdentity(t *testing.T) {
	assertContains(t, `
if a !== b {
    print("different objects")
}
`,
		`a != b`,
	)
}
// Need lexer/parser support for === and !== tokens first.
// Codegen already handles them in formatBinaryExpr.

func TestIsOperator(t *testing.T) {
	assertContains(t, `
if x is String {
    print("string")
}
`,
		`x instanceof String`,
	)
}

// =============================================================================
// Data classes → Records
// =============================================================================

func TestDataClass(t *testing.T) {
	assertContains(t,
		`data User(String name, int age)`,
		`public record User(String name, int age) {`,
	)
}

func TestDataClassWithDefault(t *testing.T) {
	assertContains(t,
		`data Point(double x, double y)`,
		`public record Point(double x, double y) {`,
	)
}

func TestDataClassOneLiner(t *testing.T) {
	assertContains(t,
		`data Config(String host, int port = 8080)`,
		`public record Config(String host, int port) {`,
	)
}

func TestDataClassWithMethods(t *testing.T) {
	assertContains(t, `
data Point(int x, int y) {
    pub fn sum(): int {
        return x + y
    }

    pub fn scale(int factor): Point {
        return Point(x * factor, y * factor)
    }
}
`,
		`public record Point(int x, int y) {`,
		`public int sum() {`,
		`return x + y;`,
		`public Point scale(int factor) {`,
	)
}

// =============================================================================
// Generics — classes, functions, data classes
// =============================================================================

func TestGenericClass(t *testing.T) {
	assertContains(t, `
class Box<T> {
    pub T value

    pub fn get(): T {
        return value
    }
}
`,
		`public static class Box<T>`,
		`private T value;`,
		`public T getValue()`,
		`public T get() {`,
		`return value;`,
	)
}

func TestGenericFunction(t *testing.T) {
	assertContains(t, `
fn identity<T>(T val): T {
    return val
}
`,
		`<T> T identity(T val) {`,
		`return val;`,
	)
}

func TestGenericDataClass(t *testing.T) {
	assertContains(t, `
data Pair<A, B>(A first, B second)
`,
		`public record Pair<A, B>(A first, B second) {`,
	)
}

func TestClassImplementsInterface(t *testing.T) {
	assertContains(t, `
interface Greeter {
    fn greet(): String
}

class HelloGreeter : Greeter {
    pub fn greet(): String {
        return "hello"
    }
}
`,
		`public interface Greeter`,
		`String greet()`,
		`public static class HelloGreeter implements Greeter`,
		`public String greet()`,
	)
}

func TestClassExtendsClassImplementsInterface(t *testing.T) {
	assertContains(t, `
interface Serializable {
    fn serialize(): String
}

class Base {
    pub String name = "base"
}

class Child : Base, Serializable {
    pub fn serialize(): String {
        return name
    }
}
`,
		`public static class Child extends Base implements Serializable`,
	)
}

func TestSealedClassWithEmptyVariant(t *testing.T) {
	src := `
sealed class ProcessorResult {
    data Single(String value)
    data Drop()
}
`
	lex := lexer.New(src)
	tokens := lex.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		t.Fatalf("parse errors: %v", p.Errors)
	}

	gen := New()
	files := gen.GenerateFiles(prog, "Main")

	// Should generate: ProcessorResult.java (sealed interface), Single.java, Drop.java
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	found := false
	for _, f := range files {
		if f.Name == "Drop.java" {
			found = true
			if !strings.Contains(f.Content, "record Drop()") {
				t.Errorf("Drop.java should contain 'record Drop()' but got:\n%s", f.Content)
			}
			if !strings.Contains(f.Content, "implements ProcessorResult") {
				t.Errorf("Drop.java should implement ProcessorResult but got:\n%s", f.Content)
			}
		}
	}
	if !found {
		t.Errorf("expected Drop.java file")
	}
}

// =============================================================================
// Enums
// =============================================================================

func TestEnum(t *testing.T) {
	assertContains(t, `
enum Color {
    Red
    Green
    Blue
}
`,
		`public enum Color {`,
		`Red,`,
		`Green,`,
		`Blue;`,
	)
}

// =============================================================================
// Classes
// =============================================================================

func TestClassBasic(t *testing.T) {
	assertContains(t, `
class Dog {
    var String name
    var String breed

    fn speak(): String {
        return "Woof!"
    }
}
`,
		`public static class Dog {`,
		`private String name;`,
		`private String breed;`,
		`String speak() {`,
		`return "Woof!";`,
	)
}

func TestClassWithDefault(t *testing.T) {
	assertContains(t, `
class Config {
    var String host = "localhost"
    var int port = 8080
}
`,
		`private String host = "localhost";`,
		`private int port = 8080;`,
	)
}

func TestClassInheritance(t *testing.T) {
	assertContains(t, `
class Puppy : Dog {
    var String name

    fn speak(): String {
        return "yap!"
    }
}
`,
		`public static class Puppy extends Dog {`,
		`private String name;`,
		`String speak() {`,
	)
}

func TestClassInitFields(t *testing.T) {
	assertContains(t, `
class User {
    init String name
    init String email
}
`,
		`private final String name;`,
		`private final String email;`,
	)
}

func TestClassConstructor(t *testing.T) {
	assertContains(t, `
class User {
    init String name
    init int age

    init(String name, int age) {
        this.name = name
        this.age = age
    }
}
`,
		`private final String name;`,
		`private final int age;`,
		`public User(String name, int age) {`,
		`this.name = name;`,
		`this.age = age;`,
	)
}

func TestClassConstructorWithDefaults(t *testing.T) {
	assertContains(t, `
class Server {
    init String host
    init int port

    init(String host, int port = 8080) {
        this.host = host
        this.port = port
    }
}
`,
		`public Server(String host, int port) {`,
		`this.host = host;`,
		`this.port = port;`,
	)
}

func TestClassConstructorWithSuper(t *testing.T) {
	assertContains(t, `
class Dog : Animal {
    init String breed

    init(String name, String breed) {
        super(name)
        this.breed = breed
    }
}
`,
		`public Dog(String name, String breed) {`,
		`super(name);`,
		`this.breed = breed;`,
	)
}

func TestClassConstructorAndMethods(t *testing.T) {
	assertContains(t, `
class Counter {
    init int start
    var int count = 0

    init(int start) {
        this.start = start
        count = start
    }

    pub fn increment() {
        count = count + 1
    }

    pub fn value(): int {
        return count
    }
}
`,
		`public Counter(int start) {`,
		`this.start = start;`,
		`count = start;`,
		`public void increment()`,
		`public int value()`,
	)
}

func TestClassMethodDirect(t *testing.T) {
	assertContains(t, `
class Foo {
    fn toString(): String {
        return "Foo"
    }
}
`,
		`String toString() {`,
		`return "Foo";`,
	)
	assertNotContains(t, `
class Foo {
    fn toString(): String {
        return "Foo"
    }
}
`,
		`@Override`,
	)
}

// =============================================================================
// Literals
// =============================================================================

func TestEmptyList(t *testing.T) {
	assertContains(t, `var items = []`,
		`new java.util.ArrayList<>()`,
	)
}

func TestEmptyMap(t *testing.T) {
	assertContains(t, `var config = {}`,
		`new java.util.HashMap<>()`,
	)
}

func TestNullLiteral(t *testing.T) {
	assertContains(t, `var x = none`,
		`var x = null;`,
	)
}

func TestBoolLiterals(t *testing.T) {
	assertContains(t, `
var a = true
var b = false
`,
		`var a = true;`,
		`var b = false;`,
	)
}

// =============================================================================
// Imports
// =============================================================================

func TestImports(t *testing.T) {
	assertContains(t, `
import java.util.List
import java.nio.file.Path
`,
		`import java.util.List;`,
		`import java.nio.file.Path;`,
	)
}

// =============================================================================
// Assert
// =============================================================================

func TestAssertBasic(t *testing.T) {
	assertContains(t, `assert x > 0`,
		`assert x > 0;`,
	)
}

func TestAssertWithMessage(t *testing.T) {
	assertContains(t, `assert x > 0, "x must be positive"`,
		`assert x > 0 : "x must be positive";`,
	)
}

// =============================================================================
// Arrays
// =============================================================================

func TestArrayDeclaration(t *testing.T) {
	assertContains(t, `var int[] nums = [1, 2, 3]`,
		`int[] nums = new int[] {1, 2, 3};`,
	)
}

func TestArrayStringDeclaration(t *testing.T) {
	assertContains(t, `var String[] names = ["Alice", "Bob"]`,
		`String[] names = new String[] {"Alice", "Bob"};`,
	)
}

func TestArrayEmptyDeclaration(t *testing.T) {
	assertContains(t, `var int[] nums = []`,
		`int[] nums = new int[0];`,
	)
}

func TestArrayAccess(t *testing.T) {
	assertContains(t, `
var int[] nums = [10, 20, 30]
var x = nums[0]
`,
		`int[] nums = new int[] {10, 20, 30};`,
		`var x = nums[0];`,
	)
}

func TestArrayInFunction(t *testing.T) {
	assertContains(t, `
fn sum(int[] numbers): int {
    return 0
}
`,
		`static int sum(int[] numbers)`,
	)
}

// =============================================================================
// fn main() entry point
// =============================================================================

func TestMainNoArgs(t *testing.T) {
	assertContains(t, `
fn main() {
    print("hello")
}
`,
		`public static void main(String[] args) throws Exception {`,
		`System.out.println("hello")`,
	)
}

func TestMainWithArgs(t *testing.T) {
	assertContains(t, `
fn main(String[] args) {
    print("hello")
}
`,
		`public static void main(String[] args) throws Exception {`,
	)
}

// =============================================================================
// Range syntax
// =============================================================================

func TestRangeExclusive(t *testing.T) {
	assertContains(t, `
for i in 1..5 {
    print(i)
}
`,
		`for (int i = 1; i < 5; i++) {`,
	)
}

func TestRangeInclusive(t *testing.T) {
	assertContains(t, `
for i in 1..=5 {
    print(i)
}
`,
		`for (int i = 1; i <= 5; i++) {`,
	)
}

// =============================================================================
// Map destructuring
// =============================================================================

func TestMapDestructuring(t *testing.T) {
	assertContains(t, `
var Map<String, int> ages = {"Alice": 30}
for key, value in ages {
    print("{key}: {value}")
}
`,
		`for (var _entry : ages.entrySet()) {`,
		`var key = _entry.getKey();`,
		`var value = _entry.getValue();`,
	)
}

// =============================================================================
// String method aliases
// =============================================================================

func TestStringUpper(t *testing.T) {
	assertContains(t, `var x = "hello".upper()`,
		`"hello".toUpperCase()`,
	)
}

func TestStringLower(t *testing.T) {
	assertContains(t, `var x = "HELLO".lower()`,
		`"HELLO".toLowerCase()`,
	)
}
