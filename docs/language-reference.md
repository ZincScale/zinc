# Zinc v2 — Language Reference

## Blocks

All blocks close with `end`. No braces, no significant whitespace.

```zinc
fn example()
    if true
        print("yes")
    end
end
```

## Variables

```zinc
var x = 42                  // inferred type
var name: str = "Alice"     // explicit type
var items: list[int] = []   // generic type
var a, b = divmod(10, 3)    // tuple unpacking
```

## Functions

```zinc
fn greet(name: str): str
    return "Hello, {name}!"
end

fn double(x: int): int = x * 2       // single-expression
fn log(*args, **kwargs)               // variadic
fn connect(host: str, port: int = 80) // default args
```

## Strings

```zinc
"Hello, {name}!"        // double quotes: interpolation with {}
'no {interpolation}'    // single quotes: literal strings
"""multi-line
string"""               // triple quotes: multi-line
```

## Classes

```zinc
class Stack
    var items: list[int] = []

    fn push(item: int)
        items.append(item)       // auto-injects self.items
    end

    fn len(): int                // → __len__(self)
        return len(items)
    end

    fn str(): str                // → __str__(self)
        return "Stack({items})"
    end
end

class Dog(Animal)                // inheritance
    var breed: str

    @staticmethod
    fn species(): str
        return "Canis lupus"
    end
end
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

## Data Classes

```zinc
data User
    name: str
    email: str
    age: int = 0
end
```

Transpiles to `@dataclass class User`.

## Enums

```zinc
enum Color
    Red
    Green
    Blue
end
```

## Control Flow

```zinc
// if / else if / else
if x > 0
    print("positive")
else if x == 0
    print("zero")
else
    print("negative")
end

// expression if (ternary)
var label = if count == 1: "item" else: "items"

// for loop
for item in items
    print(item)
end

for i, item in items        // with index (enumerate)
    print("{i}: {item}")
end

// while
while running
    process()
end

// match
match command
    case "start" -> start()
    case "stop" -> stop()
    case _ -> print("unknown")
end
```

## Error Handling

### Track 1 — Result[T] for expected failures

```zinc
fn parse_age(input: str): Result[int]
    if not input.isdigit()
        return Err("not a number")
    end
    return int(input)            // auto-wrapped in Ok()
end

// Default value (single expression, no end needed)
var age = parse_age(input) Err 0

// Handler block
var age = parse_age(input) Err
    print("bad: {err}")
    return
end
```

### Track 2 — Exceptions for unexpected failures

```zinc
try
    var conn = db.connect(url)
catch err: ConnectionError
    print("down: {err}")
end

raise ValueError("bad") from original
```

## Imports

```zinc
import json
import os.path
from pathlib import Path
from os.path import join, exists
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

// Identity
is   is not

// None
none
```

## Collection Methods

```zinc
items.filter(x -> x > 0)        // → [x for x in items if (x > 0)]
items.map(x -> x * 2)           // → [(x * 2) for x in items]
items.sum()                      // → sum(items)
items.sort_by(x -> x.age)       // → sorted(items, key=...)
items.take(10)                   // → items[:10]
items.first(x -> x > 10)        // → next(x for x in items if ...)
items.any(x -> x > 0)           // → any(...)
items.group_by(x -> x.category) // → itertools.groupby(...)

// Comprehensions (transpiler auto-picks list vs generator)
var squares = [x * x for x in range(10)]
var total = sum([x for x in items])   // auto-stripped to generator
var lengths = {w: len(w) for w in words}

// With --optimize polars, chains become Polars lazy frames:
// orders.filter(o -> o["status"] == "active").map(o -> o["amount"]).sum()
// → pl.DataFrame(orders).lazy().filter(pl.col("status") == "active").select("amount").sum().collect().item()
```

## Generators

```zinc
fn fibonacci(limit: int)
    var a = 0
    var b = 1
    while a < limit
        yield a
        var temp = a
        a = b
        b = temp + b
    end
end
```

## Context Managers

```zinc
with f = open("data.txt")
    var content = f.read()
end
```

## Decorators

```zinc
@cache
fn expensive(n: int): int
    return compute(n)
end

class MyClass
    @staticmethod
    fn create(): MyClass
        return MyClass()
    end

    @classmethod
    fn from_dict(d: dict): MyClass
        return MyClass()
    end
end
```
