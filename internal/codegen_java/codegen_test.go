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
	"zinc/internal/typechecker"
)

func transpile(src string) string {
	lex := lexer.New(src)
	tokens := lex.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		return "PARSE_ERRORS: " + strings.Join(p.Errors, "; ")
	}
	// Run typechecker to fill in resolved types (e.g., var + or handler inference)
	typechecker.CheckV2(prog)
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
		`public static String greet(String name) throws Exception {`,
		`return "Hello, " + name + "!";`,
	)
}

func TestFnVoid(t *testing.T) {
	assertContains(t, `
fn sayHello() {
    print("Hello!")
}
`,
		`public static void sayHello() throws Exception {`,
		`System.out.println("Hello!");`,
	)
}

func TestFnDefaultParams(t *testing.T) {
	assertContains(t, `
fn connect(String host, int port = 8080): String {
    return "{host}:{port}"
}
`,
		// Full version
		`static String connect(String host, int port) throws Exception {`,
		// Overload with default
		`static String connect(String host) throws Exception {`,
		`return connect(host, 8080);`,
	)
}

func TestFnMultipleDefaults(t *testing.T) {
	assertContains(t, `
fn setup(String host, int port = 80, boolean ssl = false): String {
    return host
}
`,
		// Full version
		`static String setup(String host, int port, boolean ssl) throws Exception {`,
		// Overload: host + port (ssl defaults)
		`static String setup(String host, int port) throws Exception {`,
		`return setup(host, port, false);`,
		// Overload: host only (port + ssl default)
		`static String setup(String host) throws Exception {`,
		`return setup(host, 80, false);`,
	)
}

func TestConstructorDefaultParams(t *testing.T) {
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
		// Full constructor
		`public Server(String host, int port) throws Exception {`,
		// Overload with default
		`public Server(String host) throws Exception {`,
		`this(host, 8080);`,
	)
}

func TestMethodDefaultParams(t *testing.T) {
	assertContains(t, `
class Logger {
    pub fn log(String msg, String level = "INFO") {
        print(msg)
    }
}
`,
		// Full method
		`public void log(String msg, String level) throws Exception {`,
		// Overload with default
		`public void log(String msg) {`,
		`log(msg, "INFO");`,
	)
}

func TestFnSingleExpression(t *testing.T) {
	assertContains(t, `
fn double(int x): int = x * 2
`,
		`static int double(int x) throws Exception {`,
		`return x * 2;`,
	)
}

func TestFnSingleExpressionVoid(t *testing.T) {
	assertContains(t, `
fn greet(String name) = print("Hello, {name}!")
`,
		`static void greet(String name) throws Exception {`,
	)
}

func TestFnNamedArgs(t *testing.T) {
	assertContains(t, `
fn connect(String host, int port): String {
    return host
}
var x = connect(port = 3000, host = "localhost")
`,
		`connect(`,
	)
}

func TestFnVariadicParams(t *testing.T) {
	assertContains(t, `
fn log(String... messages) {
    for msg in messages {
        print(msg)
    }
}
`,
		`static void log(String... messages) throws Exception {`,
	)
}

func TestBlockLambda(t *testing.T) {
	assertContains(t, `
var result = items.map(x -> {
    var doubled = x * 2
    return doubled + 1
})
`,
		`x -> {`,
	)
}

func TestFnMultipleParams(t *testing.T) {
	assertContains(t, `
fn add(int a, int b): int {
    return a + b
}
`,
		`public static int add(int a, int b) throws Exception {`,
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

func TestExpressionIf(t *testing.T) {
	assertContains(t, `
var x = if true: "yes" else: "no"
`,
		`(true ? "yes" : "no")`,
	)
}

func TestForWithIndexMap(t *testing.T) {
	// for key, value in map → entrySet iteration
	assertContains(t, `
for key, value in ages {
    print("{key}: {value}")
}
`,
		`for (var _entry : ages.entrySet())`,
		`var key = _entry.getKey();`,
		`var value = _entry.getValue();`,
	)
}

func TestMatchEnum(t *testing.T) {
	assertContains(t, `
match color {
    case "Red" {
        print("red")
    }
    case "Green" {
        print("green")
    }
    case _ {
        print("other")
    }
}
`,
		`switch (color)`,
		`case "Red" -> {`,
		`case "Green" -> {`,
		`default -> {`,
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

func TestMatchExpression(t *testing.T) {
	assertContains(t, `
var label = match status {
    case "ok" { "success" }
    case "err" { "failure" }
    case _ { "unknown" }
}
`,
		`switch (status) {`,
		`case "ok" -> "success"`,
		`case "err" -> "failure"`,
		`default -> "unknown"`,
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
		`throw new RuntimeException(err);`,
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

func TestOrHandlerTypeInference(t *testing.T) {
	assertContains(t, `
fn divide(int a, int b): int {
    if b == 0 { return Error("zero") }
    return a / b
}
var x = divide(10, 2) or -1
`,
		`int x;`,
	)
	assertNotContains(t, `
fn divide(int a, int b): int {
    if b == 0 { return Error("zero") }
    return a / b
}
var x = divide(10, 2) or -1
`,
		`Object x;`,
	)
}

func TestOrBlockReturn(t *testing.T) {
	assertContains(t, `
fn loadConfig(): String {
    var data = readFile("config.json") or {
        return Error("config missing")
    }
    return data
}
`,
		`try { data = readFile("config.json"); } catch (Exception err) {`,
		`throw new RuntimeException("config missing");`,
	)
}

func TestOrBlockContinue(t *testing.T) {
	assertContains(t, `
for item in items {
    var result = process(item) or {
        continue
    }
    print(result)
}
`,
		`try { result = process(item); } catch (Exception err) {`,
		`continue;`,
	)
}

func TestCustomErrorType(t *testing.T) {
	assertContains(t, `
fn validate(int x): int {
    if x < 0 {
        return Error(ValidationError("must be positive"))
    }
    return x
}
`,
		`throw new ValidationError("must be positive");`,
	)
}

func TestPowerOperatorIntResult(t *testing.T) {
	assertContains(t, `
var x = 2 ** 3
`,
		`(long)Math.pow(2, 3)`,
	)
}

func TestPowerOperatorDoubleResult(t *testing.T) {
	assertContains(t, `
var x = 2.0 ** 3
`,
		`Math.pow(2.0, 3)`,
	)
	assertNotContains(t, `
var x = 2.0 ** 3
`,
		`(long)Math.pow`,
	)
}

func TestNestedOrBlocksUniqueErrVars(t *testing.T) {
	assertContains(t, `
fn risky(): int {
    return Error("fail")
}
fn outer() {
    var x = risky() or {
        var y = risky() or {
            print("inner failed")
        }
    }
}
`,
		`catch (Exception err)`,
		`catch (Exception _err2)`,
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

func TestAndKeyword(t *testing.T) {
	assertContains(t, `
if a and b {
    print("both")
}
`,
		`if (a && b) {`,
	)
}

// NOTE: 'or' is the error handler keyword, not boolean OR.
// Boolean OR uses || only. See TestOrOperator.

func TestInterpolationWithOperators(t *testing.T) {
	assertContains(t, `
print("eq: {a == b}")
`,
		`(java.util.Objects.equals(a, b))`,
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

func TestStreamFlatMap(t *testing.T) {
	assertContains(t,
		`orders.flatMap(o -> o.items)`,
		`.stream().flatMap(o -> o.items).toList()`,
	)
}

func TestStreamSkip(t *testing.T) {
	assertContains(t,
		`items.skip(5)`,
		`.stream().skip(5).toList()`,
	)
}

func TestStreamAllMatch(t *testing.T) {
	assertContains(t,
		`items.allMatch(x -> x > 0)`,
		`.stream().allMatch(x -> x > 0)`,
	)
}

func TestStreamNoneMatch(t *testing.T) {
	assertContains(t,
		`items.noneMatch(x -> x < 0)`,
		`.stream().noneMatch(x -> x < 0)`,
	)
}

func TestStreamMin(t *testing.T) {
	assertContains(t,
		`items.min()`,
		`.stream().min(`,
	)
}

func TestStreamMax(t *testing.T) {
	assertContains(t,
		`items.max()`,
		`.stream().max(`,
	)
}

func TestStreamReduce(t *testing.T) {
	assertContains(t,
		`items.reduce(0, (a, b) -> a + b)`,
		`.stream().reduce(0, (a, b) -> a + b)`,
	)
}

func TestStreamToSet(t *testing.T) {
	assertContains(t,
		`items.toSet()`,
		`.stream().collect(java.util.stream.Collectors.toSet())`,
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

func TestParallelForBounded(t *testing.T) {
	assertContains(t, `
parallel(max: 5) for item in items {
    process(item)
}
`,
		`new java.util.concurrent.Semaphore(5)`,
		`_semaphore.acquire()`,
		`_scope.fork(() -> {`,
		`finally { _semaphore.release(); }`,
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

func TestConcurrentFirstOnly(t *testing.T) {
	assertContains(t, `
var a, b = concurrent(first: true) {
    slowApi()
    fastApi()
}
`,
		`ShutdownOnSuccess`,
	)
}

func TestChannelType(t *testing.T) {
	assertContains(t,
		`var Channel<String> ch = new Channel(100)`,
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
with f = new FileReader("data.txt") {
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
		`public int sum() throws Exception {`,
		`return x + y;`,
		`public Point scale(int factor) throws Exception {`,
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
		`public T get() throws Exception {`,
		`return value;`,
	)
}

func TestGenericFunction(t *testing.T) {
	assertContains(t, `
fn identity<T>(T val): T {
    return val
}
`,
		`<T> T identity(T val) throws Exception {`,
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
		`String greet() throws Exception`,
		`public static class HelloGreeter implements Greeter`,
		`public String greet() throws Exception`,
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
		`String speak() throws Exception {`,
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
		`String speak() throws Exception {`,
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
		`public User(String name, int age) throws Exception {`,
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
		`public Server(String host, int port) throws Exception {`,
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
		`public Dog(String name, String breed) throws Exception {`,
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
		`public Counter(int start) throws Exception {`,
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
		`try {`,
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
	assertContains(t, `var x = null`,
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

// =============================================================================
// Types — nullable, byte[], Set, annotations
// =============================================================================

func TestNullableType(t *testing.T) {
	assertContains(t, `
fn find(String key): String? {
    return null
}
`,
		`static String find(String key) throws Exception {`,
		`return null;`,
	)
}

func TestNullableField(t *testing.T) {
	assertContains(t, `
class Order {
    pub String? shippingAddress = null
}
`,
		`private String shippingAddress = null;`,
	)
}

func TestByteArray(t *testing.T) {
	assertContains(t, `
fn process(byte[] data): int {
    return data.length
}
`,
		`static int process(byte[] data) throws Exception {`,
		`return data.length;`,
	)
}

func TestSetType(t *testing.T) {
	assertContains(t, `
Set<String> names = Set.of("Alice", "Bob")
`,
		`Set<String> names = Set.of("Alice", "Bob");`,
	)
}

func TestAnnotationWithArgs(t *testing.T) {
	assertContains(t, `
@Deprecated
fn oldMethod() {
    print("old")
}
`,
		`@Deprecated`,
		`static void oldMethod()`,
	)
}

func TestAnnotationWithStringArg(t *testing.T) {
	assertContains(t, `
@Path("/api")
fn handleApi() {
    print("api")
}
`,
		`@Path("/api")`,
	)
}

func TestSafeNavigationField(t *testing.T) {
	assertContains(t, `var x = obj?.name`,
		`(obj != null ? obj.name : null)`,
	)
}

func TestSafeNavigationMethod(t *testing.T) {
	assertContains(t, `var x = obj?.toString()`,
		`(obj != null ? obj.toString() : null)`,
	)
}

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

func TestStringTrim(t *testing.T) {
	assertContains(t, `var x = "  hello  ".trim()`,
		`"  hello  ".strip()`,
	)
}

func TestStringTrimStartEnd(t *testing.T) {
	assertContains(t, `var x = "  hello  ".trimStart()`,
		`"  hello  ".stripLeading()`,
	)
}

func TestStringSplit(t *testing.T) {
	assertContains(t, `var parts = "a,b,c".split(",")`,
		`"a,b,c".split(",")`,
	)
}

func TestStringContains(t *testing.T) {
	assertContains(t, `var b = "hello world".contains("world")`,
		`"hello world".contains("world")`,
	)
}

func TestStringStartsWith(t *testing.T) {
	assertContains(t, `var b = "hello".startsWith("he")`,
		`"hello".startsWith("he")`,
	)
}

func TestStringReplace(t *testing.T) {
	assertContains(t, `var x = "hello".replace("l", "r")`,
		`"hello".replace("l", "r")`,
	)
}

func TestStringRepeat(t *testing.T) {
	assertContains(t, `var x = "ha".repeat(3)`,
		`"ha".repeat(3)`,
	)
}

func TestStringIsEmpty(t *testing.T) {
	assertContains(t, `var b = "".isEmpty()`,
		`"".isEmpty()`,
	)
}

func TestStringSubstring(t *testing.T) {
	assertContains(t, `var x = "hello".substring(1, 3)`,
		`"hello".substring(1, 3)`,
	)
}

func TestStringCharAt(t *testing.T) {
	assertContains(t, `var c = "hello".charAt(0)`,
		`"hello".charAt(0)`,
	)
}

func TestStringIndexOf(t *testing.T) {
	assertContains(t, `var i = "hello".indexOf("ll")`,
		`"hello".indexOf("ll")`,
	)
}

func TestSingleQuoteString(t *testing.T) {
	assertContains(t, `var x = 'hello world'`,
		`"hello world"`,
	)
}

func TestTripleQuoteString(t *testing.T) {
	assertContains(t, "var x = \"\"\"hello\nworld\"\"\"",
		`"""`,
		`hello`,
		`world"""`,
	)
}

func TestFullyQualifiedTypes(t *testing.T) {
	// Variable declaration with FQDN type
	assertContains(t, "java.util.concurrent.atomic.AtomicInteger counter = new java.util.concurrent.atomic.AtomicInteger(0)",
		"java.util.concurrent.atomic.AtomicInteger counter = new java.util.concurrent.atomic.AtomicInteger(0)",
	)

	// FQDN type in function parameter
	assertContains(t, "fn process(java.util.List<String> items): int { return 0 }",
		"java.util.List<String> items",
	)

	// FQDN type as return type
	assertContains(t, "fn getMap(): java.util.Map<String, int> { return null }",
		"java.util.Map<String, Integer>",
	)

	// FQDN array type
	assertContains(t, "java.math.BigDecimal[] values = null",
		"java.math.BigDecimal[] values",
	)
}

// --- Actor Tests -------------------------------------------------------------

func TestActorBasic(t *testing.T) {
	assertContains(t, `
actor Counter {
	var int count = 0

	receive fn increment() {
		count += 1
	}
}`,
		"LinkedBlockingQueue<Runnable> _mailbox",
		"volatile boolean _running = true",
		"Thread.startVirtualThread",
		"_mailbox.take()",
		"public void increment()",
		"_mailbox.add(() ->",
	)
}

func TestActorRequestReply(t *testing.T) {
	assertContains(t, `
actor Counter {
	var int count = 0

	receive fn getCount(): int {
		return count
	}
}`,
		"CompletableFuture<Integer>",
		"_future.complete(count)",
		"return _future.get()",
		"public int getCount()",
	)
}

func TestActorConstructor(t *testing.T) {
	assertContains(t, `
actor Counter {
	var int count = 0

	init(int start) {
		count = start
	}

	receive fn getCount(): int {
		return count
	}
}`,
		"public Counter(int start) throws Exception",
		"count = start",
		"Thread.startVirtualThread",
	)
}

func TestActorPrivateMethods(t *testing.T) {
	assertContains(t, `
actor Processor {
	fn validate(int n): boolean {
		return n > 0
	}
}`,
		"private boolean validate(int n)",
	)
}

func TestActorLifecycle(t *testing.T) {
	assertContains(t, `
actor Worker {
	receive fn doWork() {
	}
}`,
		"public void shutdown() throws Exception",
		"public void shutdown(long timeoutMs) throws Exception",
		"public void kill()",
		"_actorThread.interrupt()",
		"_mailbox.clear()",
		"ActorRuntime.pendingKill",
	)
}

func TestActorMultipleReceiveFns(t *testing.T) {
	assertContains(t, `
actor Counter {
	var int count = 0
	receive fn increment() { count += 1 }
	receive fn add(int n) { count += n }
	receive fn getCount(): int { return count }
	receive fn reset() { count = 0 }
}`,
		"public void increment()",
		"public void add(int n)",
		"public int getCount()",
		"public void reset()",
		// All fire-and-forget should use _mailbox.add
		"_mailbox.add(() ->",
		// Request-reply should use CompletableFuture
		"CompletableFuture<Integer>",
	)
}

func TestActorReceiveTryCatch(t *testing.T) {
	// Verify fire-and-forget wraps in try-catch preserving exception type
	assertContains(t, `
actor Worker {
	receive fn doWork() {
		print("working")
	}
}`,
		"try {",
		"(e instanceof RuntimeException re) ? re : new RuntimeException(e)",
	)
}

func TestActorRequestReplyTryCatch(t *testing.T) {
	// Verify request-reply wraps in try-catch with completeExceptionally
	assertContains(t, `
actor Worker {
	receive fn compute(): int {
		return 42
	}
}`,
		"try {",
		"_future.completeExceptionally(e)",
	)
}

func TestActorNoFields(t *testing.T) {
	// Actor with no state — just a message handler
	assertContains(t, `
actor Echo {
	receive fn echo(String msg): String {
		return msg
	}
}`,
		"public static class Echo",
		"LinkedBlockingQueue<Runnable> _mailbox",
		"public String echo(String msg)",
	)
}

func TestActorFieldsPrivate(t *testing.T) {
	// Actor fields should be private with no getters
	result := transpile(`
actor Secret {
	var String data = "hidden"
	receive fn getData(): String { return data }
}`)
	if !strings.Contains(result, "private String data") {
		t.Errorf("expected private field, got:\n%s", result)
	}
	// Should NOT have a getter method for 'data'
	if strings.Contains(result, "public String getData") && strings.Contains(result, "return this.data") {
		// getData is a receive fn, not a getter — it should use CompletableFuture
		if !strings.Contains(result, "CompletableFuture") {
			t.Errorf("expected receive fn with CompletableFuture, got plain getter:\n%s", result)
		}
	}
}

func TestActorWithParent(t *testing.T) {
	assertContains(t, `
interface Pingable {
	fn ping(): String
}
actor PingActor : Pingable {
	receive fn ping(): String {
		return "pong"
	}
}`,
		"implements Pingable",
	)
}

func TestSupervisorBasic(t *testing.T) {
	assertContains(t, `
supervisor Pipeline {
	init String strategy = "one_for_one"

	child worker1 = new Object()
}`,
		"public static class Pipeline",
		"private Object worker1",
		"public void start()",
		"public void shutdown()",
	)
}
