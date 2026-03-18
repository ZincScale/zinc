# Zinc v2 — Language Reference

## Blocks

Blocks use `{ }` braces. Indentation is for readability only.

```zinc
fn example() {
    if true {
        print("yes")
    }
}
```

## Variables

```zinc
var x = 42                  // inferred type
var name: str = "Alice"     // explicit type
var items: list[int] = []   // generic type
var a, b = divmod(10, 3)    // tuple unpacking
```

## Constants

```zinc
const PI = 3.14159
const MAX_RETRIES = 3
const APP_NAME = "zinc-app"
```

## Functions

```zinc
fn greet(name: str): str {
    return "Hello, {name}!"
}

fn double(x: int): int = x * 2       // single-expression
fn log(*args, **kwargs) { }           // variadic
fn connect(host: str, port: int = 80) { }  // default args

// Named arguments at call site
connect("db.example.com", port=3306, ssl=false)
```

## Strings

```zinc
"Hello, {name}!"        // double quotes: interpolation with {}
"{data["key"]}"         // nested quotes in interpolation — works
'no {interpolation}'    // single quotes: literal strings
"""multi-line
string"""               // triple quotes: multi-line
```

## Tuples

```zinc
var point = (3, 5)
var rgb = (255, 128, 0)

fn swap(a: int, b: int) {
    return b, a              // return tuple without parens
}
var x, y = swap(1, 2)       // tuple unpacking
```

## Classes

```zinc
class Stack {
    var items: list[int] = []

    fn push(item: int) {
        items.append(item)       // auto-injects self.items
    }

    fn len(): int {              // → __len__(self)
        return len(items)
    }

    fn str(): str {              // → __str__(self)
        return "Stack({items})"
    }

    @property
    fn size(): int {
        return len(items)
    }
}
```

### Inheritance

```zinc
class Animal {
    var name: str
    var sound: str

    fn speak(): str {
        return "{name} says {sound}"
    }
}

class Dog(Animal) {
    var breed: str

    fn fetch(): str {
        return "{name} fetches!"   // inherited fields auto-inject self.
    }
}

var d = Dog(breed="Lab", name="Rex", sound="Woof")
print(d.speak())     // Rex says Woof
print(d.fetch())     // Rex fetches!
```

### Dunder Mapping

| Zinc | Python |
|---|---|
| `fn init(...)` | `__init__` |
| `fn str()` | `__str__` |
| `fn repr()` | `__repr__` |
| `fn eq(other)` | `__eq__` |
| `fn len()` | `__len__` |
| `fn iter()` | `__iter__` |
| `fn contains(item)` | `__contains__` |
| `fn get(key)` | `__getitem__` |
| `fn set(key, val)` | `__setitem__` |
| `fn add(other)` | `__add__` |
| `fn lt(other)` | `__lt__` |
| `fn call(...)` | `__call__` |

### Decorators

```zinc
@cache
fn expensive(n: int): int {
    return compute(n)
}

class MyClass {
    @staticmethod
    fn create(): MyClass {
        return MyClass()
    }

    @classmethod
    fn from_dict(d: dict): MyClass {
        return MyClass()
    }

    @property
    fn label(): str {
        return "MyClass"
    }
}
```

## Data Classes

```zinc
data User {
    name: str
    email: str
    age: int = 0
}

var u = User("Alice", "alice@example.com", 30)
// Auto-generates __init__, __repr__, __eq__
```

Transpiles to `@dataclass class User`.

## Enums

```zinc
enum Color {
    Red
    Green
    Blue
}

enum Direction {
    North
    South
    East
    West
}
```

## Control Flow

```zinc
// if / else if / else
if x > 0 {
    print("positive")
} else if x == 0 {
    print("zero")
} else {
    print("negative")
}

// expression if (ternary) — condition first
var label = if count == 1: "item" else: "items"

// for loop
for item in items {
    print(item)
}

for i, item in items {       // with index (auto enumerate)
    print("{i}: {item}")
}

// while
while running {
    process()
}

// match
match command {
    case "start" -> start()
    case "stop" -> stop()
    case _ -> print("unknown")
}
```

## Type Checking with `is`

`is` does both identity checks and type checks — the transpiler decides based on context:

```zinc
// Type check — rhs is a type name → generates isinstance()
if x is str {
    print(x.upper())        // x narrowed to str in this block
}
if x is int {
    print(x + 1)             // x narrowed to int
}
if x is not list {
    print("not a list")
}

// Identity check — rhs is a value → generates Python is
if value is none {
    print("no value")
}
if value is not none {
    print("has value: {value}")
}
```

## Error Handling

### Track 1 — Result[T] for expected failures

Use for validation, parsing, missing data — anything you'd put in a loop over 10,000 records:

```zinc
fn parse_port(s: str): Result[int] {
    if not s.isdigit() {
        return Err("not a number: {s}")
    }
    var port = int(s)
    if port < 1 or port > 65535 {
        return Err("out of range: {port}")
    }
    return port              // auto-wrapped in Ok()
}

// Default value
var port = parse_port("8080") Err 80

// Handler block
var port = parse_port(input) Err {
    print("bad port: {err}")
    return
}

// Batch processing — skip bad records
for record in records {
    var age = parse_age(record["age"]) Err {
        print("skipping: {err}")
        continue
    }
    process(age)
}
```

### Track 2 — Exceptions for unexpected failures

Use for program-stopping failures — network down, disk full, out of memory:

```zinc
try {
    var conn = db.connect(url)
} catch err: ConnectionError {
    print("database down: {err}")
    exit(1)
}

// Exception chaining
raise ValueError("bad config") from original_error
```

## Imports

```zinc
import json
import os.path
from pathlib import Path
from os.path import join, exists, basename
```

## Operators

```zinc
// Arithmetic
+ - * / % **

// Comparison
== != < <= > >=

// Boolean
and  or  not

// Membership
in   not in

// Type check / Identity
is   is not                  // type check or identity based on rhs

// None
none
```

## Collection Methods

Single operations generate inline comprehensions (zero overhead):

```zinc
items.filter(x -> x > 0)        // → [x for x in items if (x > 0)]
items.map(x -> x * 2)           // → [(x * 2) for x in items]
items.sum()                      // → sum(items)
items.sort_by(x -> x.age)       // → sorted(items, key=...)
items.take(10)                   // → items[:10]
items.skip(5)                    // → items[5:]
items.first(x -> x > 10)        // → next(x for x if ...)
items.any(x -> x > 0)           // → any(...)
items.all(x -> x > 0)           // → all(...)
items.distinct()                 // → list(set(...))
items.group_by(x -> x.category) // → itertools.groupby(...)
```

Chained operations use smart dispatch runtime:

```zinc
// Chain of 2+ → _zinc_collect() with method chaining
var total = orders
    .filter(o -> o["status"] == "active")
    .map(o -> o["amount"])
    .sum()
```


```zinc
// Same code, different backend:
// → pl.DataFrame(orders).lazy().filter(...).select(...).sum().collect().item()
```

## Comprehensions

```zinc
// List comprehension
var squares = [x ** 2 for x in range(10)]
var evens = [x for x in numbers if x % 2 == 0]

// Dict comprehension
var lengths = {w: len(w) for w in words}

// Auto generator promotion — transpiler strips brackets inside sum/any/all
var total = sum([x for x in items])   // → sum(x for x in items)
```

## Generators

```zinc
fn fibonacci(limit: int) {
    var a = 0
    var b = 1
    while a < limit {
        yield a
        var temp = a
        a = b
        b = temp + b
    }
}

for n in fibonacci(100) {
    print(n)
}
```

## Context Managers

```zinc
with f = open("data.txt") {
    var content = f.read()
}
// f is automatically closed
```

## Assert

```zinc
assert x > 0, "x must be positive"
assert len(items) > 0
```

## Delete

```zinc
var config = {"host": "localhost", "secret": "abc123"}
del config["secret"]
```

## Type Safety

Types are enforced at transpile time — errors block `.py` output:

```
var x: int = "hello"                    // type mismatch: expected int, got str
fn add(): int { return "bad" }          // return type mismatch
greet(42)                               // argument 1: expected str, got int
greet("a", "b")                         // expects 1 args, got 2
break                                   // 'break' outside of loop
y = 10                                  // undefined variable "y"
fn f(): int { if x > 0 { return 1 } }  // not all code paths return
```

Type narrowing works after `is` checks:

```zinc
fn process(x: any) {
    if x is str {
        var s: str = x       // OK — x narrowed to str
    }
}
```

## Threading

Zinc runs on free-threaded Python (GIL disabled). Threads are real parallelism.

```zinc
// Spawn — run in background, returns a Future
var future = spawn {
    expensive_computation()
}
print("main continues...")
var result = future.result()  // wait for result

// Parallel for — process items across thread pool
parallel for item in items {
    process(item)
}

// Thread-safe critical section
import threading
var lock = threading.Lock()
var counter = 0
parallel for item in items {
    var result = compute(item)
    with lock {
        counter = counter + result
    }
}
```

On free-threaded Python 3.14+, `parallel for` achieves real speedup (8-10x on 10 items).

## Shebang

```zinc
#!/usr/bin/env zinc run
print("directly executable!")
```

```bash
chmod +x script.zn
./script.zn
```

## CLI

```bash
zinc run script.zn                    # transpile + run (free-threaded Python)
zinc run script.zn -- arg1 arg2       # pass args to script
zinc transpile script.zn              # output .py file
zinc transpile script.zn -o out.py    # specify output path
zinc fmt script.zn                    # format source code
zinc pack script.zn                   # package with PyInstaller
zinc pack script.zn --format nuitka   # compile to native binary (30-50% faster)
zinc pack script.zn --format docker   # generate Dockerfile
zinc pack script.zn --format k8s      # Dockerfile + K8s manifest
zinc pack myproject/                  # package entire project directory
zinc repl                             # interactive REPL
```

All `zinc run` and `zinc pack` use free-threaded Python (GIL disabled) by default. `PYTHON_GIL=0` is set in generated Dockerfiles and K8s manifests.
