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
	assertV2Contains(t,
		`var doubled = items.map(x -> x * 2)`,
		`[(lambda x: (x * 2))(x) for x in items]`,
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
