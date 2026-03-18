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

package codegen_python

import (
	"strings"
	"testing"

	"zinc/internal/lexer"
	"zinc/internal/parser"
)

func transpileV2(src string) string {
	lex := lexer.New(src)
	tokens := lex.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseV2()
	if len(p.Errors) > 0 {
		return "PARSE_ERRORS: " + strings.Join(p.Errors, "; ")
	}
	gen := New()
	return gen.GenerateV2(prog)
}

func assertV2Contains(t *testing.T, src string, expected ...string) {
	t.Helper()
	result := transpileV2(src)
	for _, exp := range expected {
		if !strings.Contains(result, exp) {
			t.Errorf("expected output to contain %q\ngot:\n%s", exp, result)
		}
	}
}

func TestV2HelloWorld(t *testing.T) {
	assertV2Contains(t,
		`print("Hello, world!")`,
		`print("Hello, world!")`,
	)
}

func TestV2VarAndPrint(t *testing.T) {
	assertV2Contains(t, `
var name = "Alice"
var age: int = 30
print("Hello, {name}!")
`,
		`name = "Alice"`,
		`age: int = 30`,
		`print(f"Hello, {name}!")`,
	)
}

func TestV2FnWithReturnType(t *testing.T) {
	assertV2Contains(t, `
fn greet(name: str): str
    return "Hello, {name}!"
end
`,
		`def greet(name: str) -> str:`,
		`return f"Hello, {name}!"`,
	)
}

func TestV2FnSingleExpr(t *testing.T) {
	assertV2Contains(t, `fn double(x: int): int = x * 2`,
		`def double(x: int) -> int:`,
		`return (x * 2)`,
	)
}

func TestV2IfElseIfElse(t *testing.T) {
	assertV2Contains(t, `
if x > 0
    print("positive")
else if x == 0
    print("zero")
else
    print("negative")
end
`,
		"if (x > 0):",
		"elif (x == 0):",
		"else:",
	)
}

func TestV2ForLoop(t *testing.T) {
	assertV2Contains(t, `
for item in items
    print(item)
end
`,
		"for item in items:",
		"print(item)",
	)
}

func TestV2ForWithIndex(t *testing.T) {
	assertV2Contains(t, `
for i, item in items
    print(i)
end
`,
		"for i, item in enumerate(items):",
	)
}

func TestV2WhileLoop(t *testing.T) {
	assertV2Contains(t, `
while running
    process_next()
end
`,
		"while running:",
		"process_next()",
	)
}

func TestV2DataClass(t *testing.T) {
	assertV2Contains(t, `
data User
    name: str
    email: str
    age: int = 0
end
`,
		"@dataclasses.dataclass",
		"class User:",
		"name: str",
		"email: str",
		"age: int = 0",
	)
}

func TestV2Enum(t *testing.T) {
	assertV2Contains(t, `
enum Color
    Red
    Green
    Blue
end
`,
		"class Color(enum.Enum):",
		"Red = 1",
		"Green = 2",
		"Blue = 3",
	)
}

func TestV2ClassWithMethods(t *testing.T) {
	assertV2Contains(t, `
class Stack
    var items: list[int] = []

    fn push(item: int)
        items.append(item)
    end

    fn len(): int
        return len(items)
    end

    fn str(): str
        return "Stack"
    end
end
`,
		"class Stack:",
		"def __init__(self, items: list[int] = []):",
		"self.items = items",
		"def push(self, item: int):",
		"def __len__(self) -> int:",
		"def __str__(self) -> str:",
	)
}

func TestV2Imports(t *testing.T) {
	assertV2Contains(t, `
import json
import os.path
from pathlib import Path
from requests import get as http_get
`,
		"import json",
		"import os.path",
		"from pathlib import Path",
		"from requests import get as http_get",
	)
}

func TestV2Lambda(t *testing.T) {
	// Lambda body is inlined directly into comprehension (no awkward lambda call)
	assertV2Contains(t,
		`var doubled = items.map(x -> x * 2)`,
		`[(x * 2) for x in items]`,
	)
}

func TestV2ExpressionIf(t *testing.T) {
	assertV2Contains(t,
		`var label = if count == 1: "item" else: "items"`,
		`"item" if (count == 1) else "items"`,
	)
}

func TestV2TryCatch(t *testing.T) {
	assertV2Contains(t, `
try
    var conn = db.connect(url)
catch err: ConnectionError
    print("failed")
end
`,
		"try:",
		"conn = db.connect(url)",
		"except ConnectionError as err:",
		`print("failed")`,
	)
}

func TestV2Match(t *testing.T) {
	assertV2Contains(t, `
match command
    case "start" -> start_server()
    case "stop" -> stop_server()
    case _ -> print("unknown")
end
`,
		"match command:",
		`case "start":`,
		`case "stop":`,
		"case _:",
	)
}

func TestV2CollectionChain(t *testing.T) {
	assertV2Contains(t, `
var result = orders.filter(o -> o.status == "active").map(o -> o.amount).sum()
`,
		"sum(",
	)
}

func TestV2AndOrNot(t *testing.T) {
	assertV2Contains(t,
		`var x = a and b or not c`,
		"and",
		"or",
		"not",
	)
}

func TestV2Comprehension(t *testing.T) {
	assertV2Contains(t,
		`var squares = [x * x for x in range(10)]`,
		`squares = [(x * x) for x in range(10)]`,
	)
}

func TestV2ComprehensionWithFilter(t *testing.T) {
	assertV2Contains(t,
		`var evens = [x for x in numbers if x % 2 == 0]`,
		`evens = [x for x in numbers if ((x % 2) == 0)]`,
	)
}

func TestV2ComprehensionAutoGenerator(t *testing.T) {
	// User writes [x for x in items] — transpiler strips brackets inside sum()
	assertV2Contains(t,
		`var total = sum([x for x in items])`,
		`total = sum(x for x in items)`,
	)
}

func TestV2ComprehensionStaysList(t *testing.T) {
	// Inside non-generator-friendly functions, brackets stay
	assertV2Contains(t,
		`var result = process([x for x in items])`,
		`result = process([x for x in items])`,
	)
}

func TestV2SingleQuoteString(t *testing.T) {
	assertV2Contains(t,
		`var x = 'hello'`,
		`x = "hello"`,
	)
}

func TestV2NotIn(t *testing.T) {
	assertV2Contains(t,
		`var x = "a" not in items`,
		`("a" not in items)`,
	)
}

func TestV2IsNot(t *testing.T) {
	assertV2Contains(t,
		`var found = value is not none`,
		`(value is not None)`,
	)
}

func TestV2None(t *testing.T) {
	assertV2Contains(t,
		`var x = none`,
		`x = None`,
	)
}

func TestV2DictComprehension(t *testing.T) {
	assertV2Contains(t,
		`var lengths = {word: len(word) for word in words}`,
		`{word: len(word) for word in words}`,
	)
}

func TestV2TupleUnpacking(t *testing.T) {
	assertV2Contains(t,
		`var a, b = get_pair()`,
		`a, b = get_pair()`,
	)
}

func TestV2AutoSelfInjection(t *testing.T) {
	assertV2Contains(t, `
class Counter
    var count: int = 0

    fn increment()
        count = count + 1
    end

    fn get_count(): int
        return count
    end
end
`,
		"self.count = (self.count + 1)",
		"return self.count",
	)
}

func TestV2PowerOperator(t *testing.T) {
	assertV2Contains(t,
		`var x = 2 ** 10`,
		`x = (2 ** 10)`,
	)
}

func TestV2WithStatement(t *testing.T) {
	assertV2Contains(t, `
with f = open("test.txt")
    var content = f.read()
end
`,
		"with open(\"test.txt\") as f:",
		"content = f.read()",
	)
}

func TestV2PrivateFields(t *testing.T) {
	assertV2Contains(t, `
class Cache
    var _data: dict = {}

    fn get(key: str): str
        return _data[key]
    end
end
`,
		"self._data",
	)
}

func TestV2ClassInheritance(t *testing.T) {
	assertV2Contains(t, `
class Dog(Animal)
    var breed: str

    fn speak(): str
        return "Woof"
    end
end
`,
		"class Dog(Animal):",
		"def speak(self) -> str:",
	)
}

func TestV2ArgsKwargs(t *testing.T) {
	assertV2Contains(t, `
fn flexible(*args, **kwargs)
    print(args)
end
`,
		"def flexible(*args, **kwargs):",
	)
}

func TestV2DefaultArgs(t *testing.T) {
	assertV2Contains(t, `
fn greet(name: str, greeting: str = "Hello"): str
    return "{greeting}, {name}!"
end
`,
		`def greet(name: str, greeting: str = "Hello") -> str:`,
	)
}

func TestV2MultipleFromImports(t *testing.T) {
	// Multiple names from same module consolidated on one line
	assertV2Contains(t,
		`from os.path import join, exists`,
		"from os.path import join, exists",
	)
}

func TestV2Decorator(t *testing.T) {
	assertV2Contains(t, `
@cache
fn expensive(n: int): int
    return n * n
end
`,
		"@cache",
		"def expensive(n: int) -> int:",
	)
}

func TestV2StaticMethod(t *testing.T) {
	assertV2Contains(t, `
class Math
    @staticmethod
    fn add(a: int, b: int): int
        return a + b
    end
end
`,
		"@staticmethod",
		"def add(a: int, b: int) -> int:",
	)
}

func TestV2ClassMethod(t *testing.T) {
	assertV2Contains(t, `
class MyClass
    @classmethod
    fn create(name: str): MyClass
        return MyClass(name)
    end
end
`,
		"@classmethod",
		"def create(cls, name: str) -> MyClass:",
	)
}

func TestV2Assert(t *testing.T) {
	assertV2Contains(t,
		`assert x > 0, "must be positive"`,
		`assert (x > 0), "must be positive"`,
	)
}

func TestV2PrintMultiArg(t *testing.T) {
	assertV2Contains(t,
		`print("hello", "world", sep=", ")`,
		`print("hello", "world", sep=", ")`,
	)
}

func TestV2ResultFnOkWrap(t *testing.T) {
	// Bare return in Result function → wrapped in Ok()
	assertV2Contains(t, `
fn parse_age(input: str): Result[int]
    return 42
end
`,
		"return Ok(42)",
	)
}

func TestV2ResultFnErrPassthrough(t *testing.T) {
	// Err() return stays as-is (not double-wrapped)
	assertV2Contains(t, `
fn parse_age(input: str): Result[int]
    return Err("bad input")
end
`,
		`return Err("bad input")`,
	)
}

func TestV2ErrHandlerBlock(t *testing.T) {
	assertV2Contains(t, `
var age = parse_age(input) Err
    print("bad age")
    return
end
`,
		"_result = parse_age(input)",
		"if _result.is_err():",
		"err = _result.error",
		"age = _result.value",
	)
}

func TestV2ErrHandlerDefault(t *testing.T) {
	assertV2Contains(t, `var age = parse_age(input) Err 0`,
		"_result = parse_age(input)",
		"_result.value if _result.is_ok() else 0",
	)
}

func TestV2ResultRuntime(t *testing.T) {
	// When Result types are used, runtime is inlined
	result := transpileV2(`
fn validate(x: int): Result[int]
    return x
end
`)
	if !strings.Contains(result, "class _Ok:") {
		t.Error("expected Result runtime to be inlined")
	}
	if !strings.Contains(result, "def Ok(value)") {
		t.Error("expected Ok() function in runtime")
	}
}

func TestV2RaiseFrom(t *testing.T) {
	assertV2Contains(t,
		`raise ValueError("bad") from original`,
		`raise ValueError("bad") from original`,
	)
}

func TestV2TwoTrackErrorStory(t *testing.T) {
	// Full two-track test: Result for expected, try/catch for exceptional
	result := transpileV2(`
fn parse_port(s: str): Result[int]
    if not s.isdigit()
        return Err("not a number: {s}")
    end
    var port = int(s)
    if port < 1 or port > 65535
        return Err("out of range: {port}")
    end
    return port
end

var port = parse_port("8080") Err 80
print("Using port: {port}")

try
    var conn = connect(host, port)
catch err: ConnectionError
    print("Connection failed: {err}")
    exit(1)
end
`)
	expected := []string{
		"def parse_port(s: str) -> Result[int]:",
		`return Err(f"not a number: {s}")`,
		`return Err(f"out of range: {port}")`,
		"return Ok(port)",
		"_result = parse_port(\"8080\")",
		"_result.value if _result.is_ok() else 80",
		"try:",
		"except ConnectionError as err:",
	}
	for _, exp := range expected {
		if !strings.Contains(result, exp) {
			t.Errorf("expected output to contain %q\ngot:\n%s", exp, result)
		}
	}
}

func TestV2DelStatement(t *testing.T) {
	assertV2Contains(t,
		`del items["key"]`,
		`del items["key"]`,
	)
}

func TestV2DataAsVariable(t *testing.T) {
	assertV2Contains(t, `
var data = json.loads(text)
print(data)
`,
		"data = json.loads(text)",
		"print(data)",
	)
}

func TestV2TripleQuoteString(t *testing.T) {
	src := "var sql = \"\"\"SELECT * FROM users\"\"\""
	result := transpileV2(src)
	if !strings.Contains(result, "SELECT") {
		t.Errorf("expected SQL in output, got: %s", result)
	}
}

func TestV2Yield(t *testing.T) {
	assertV2Contains(t, `
fn count_up(n: int)
    for i in range(n)
        yield i
    end
end
`,
		"yield i",
	)
}

func TestV2SmartDispatchChain(t *testing.T) {
	// Chain of 2+ collection methods → uses _zinc_collect() dispatch
	result := transpileV2(`
var result = orders.filter(o -> o.status == "active").map(o -> o.amount).sum()
`)
	if !strings.Contains(result, "_zinc_collect(orders)") {
		t.Errorf("expected _zinc_collect dispatch for chain, got:\n%s", result)
	}
	if !strings.Contains(result, ".filter(") {
		t.Errorf("expected .filter() in chain, got:\n%s", result)
	}
	if !strings.Contains(result, ".sum()") {
		t.Errorf("expected .sum() in chain, got:\n%s", result)
	}
	if !strings.Contains(result, "_ZincCollection") {
		t.Errorf("expected collections runtime to be inlined, got:\n%s", result)
	}
}

func TestV2SingleMethodNoDispatch(t *testing.T) {
	// Single collection method → inline comprehension (no dispatch overhead)
	result := transpileV2(`var evens = items.filter(x -> x > 0)`)
	if strings.Contains(result, "_zinc_collect") {
		t.Errorf("single method should NOT use dispatch, got:\n%s", result)
	}
	if !strings.Contains(result, "for x in items if") {
		t.Errorf("expected inline comprehension, got:\n%s", result)
	}
}

func TestV2PolarsDispatch(t *testing.T) {
	// --optimize polars → generates Polars lazy frame pipeline
	lex := lexer.New(`var revenue = orders.filter(o -> o["status"] == "active").map(o -> o["amount"]).sum()`)
	tokens := lex.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseV2()
	gen := New()
	gen.OptimizeBackend = "polars"
	result := gen.GenerateV2(prog)
	if !strings.Contains(result, "pl.DataFrame(orders)") {
		t.Errorf("expected pl.DataFrame, got:\n%s", result)
	}
	if !strings.Contains(result, "pl.col(") {
		t.Errorf("expected pl.col(), got:\n%s", result)
	}
	if !strings.Contains(result, ".lazy()") {
		t.Errorf("expected .lazy(), got:\n%s", result)
	}
	if !strings.Contains(result, ".collect()") {
		t.Errorf("expected .collect(), got:\n%s", result)
	}
}

func TestV2PolarsFilterSort(t *testing.T) {
	lex := lexer.New(`var top = orders.filter(o -> o["amount"] > 100).sort_by(o -> o["amount"], reverse=true).take(5)`)
	tokens := lex.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseV2()
	gen := New()
	gen.OptimizeBackend = "polars"
	result := gen.GenerateV2(prog)
	if !strings.Contains(result, ".filter(") {
		t.Errorf("expected .filter, got:\n%s", result)
	}
	if !strings.Contains(result, ".sort(") {
		t.Errorf("expected .sort, got:\n%s", result)
	}
	if !strings.Contains(result, ".head(5)") {
		t.Errorf("expected .head(5), got:\n%s", result)
	}
	if !strings.Contains(result, "to_dicts()") {
		t.Errorf("expected to_dicts(), got:\n%s", result)
	}
}

func TestV2DefaultNoPolars(t *testing.T) {
	// Without --optimize, chains use _zinc_collect
	result := transpileV2(`var x = items.filter(o -> o > 0).map(o -> o * 2).sum()`)
	if strings.Contains(result, "pl.DataFrame") {
		t.Errorf("default should NOT use Polars, got:\n%s", result)
	}
	if !strings.Contains(result, "_zinc_collect") {
		t.Errorf("default should use _zinc_collect, got:\n%s", result)
	}
}

func TestV2FullScript(t *testing.T) {
	result := transpileV2(`
import json
import sys

data Config
    host: str
    port: int = 8080
end

fn load_config(path: str): Config
    var text = open(path).read()
    var raw = json.loads(text)
    return Config(raw["host"], raw["port"])
end

var config = load_config("config.json")
print("Server at {config.host}:{config.port}")

if len(sys.argv) > 1
    print("Args provided")
end
`)
	// Verify it produces valid-looking Python
	expected := []string{
		"import json",
		"import sys",
		"@dataclasses.dataclass",
		"class Config:",
		"host: str",
		"port: int = 8080",
		"def load_config(path: str) -> Config:",
		`config = load_config("config.json")`,
		"if (len(sys.argv) > 1):",
	}
	for _, exp := range expected {
		if !strings.Contains(result, exp) {
			t.Errorf("expected output to contain %q\ngot:\n%s", exp, result)
		}
	}
}
