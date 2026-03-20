# Zinc — Collections

## Collection Methods

Zinc provides fluent collection methods that transpile to efficient Python. Single operations generate inline comprehensions with zero overhead.

```zinc
items.filter(x -> x > 0)            // [x for x in items if (x > 0)]
items.map(x -> x * 2)               // [(x * 2) for x in items]
items.sum()                          // sum(items)
items.sort_by(x -> x.age)           // sorted(items, key=...)
items.take(10)                       // items[:10]
items.skip(5)                        // items[5:]
items.first(x -> x > 10)            // next(x for x if ...)
items.any(x -> x > 0)               // any(...)
items.all(x -> x > 0)               // all(...)
items.distinct()                     // list(set(...))
items.group_by(x -> x.category)     // itertools.groupby(...)
```

### Method Reference

| Zinc | Python | Description |
|---|---|---|
| `.filter(pred)` | list comprehension with `if` | Keep matching elements |
| `.map(fn)` | list comprehension | Transform each element |
| `.sum()` | `sum(...)` | Sum all elements |
| `.sort_by(key)` | `sorted(..., key=)` | Sort by key function |
| `.take(n)` | `[:n]` | First n elements |
| `.skip(n)` | `[n:]` | Skip first n elements |
| `.first(pred)` | `next(... for ... if)` | First match |
| `.any(pred)` | `any(...)` | True if any match |
| `.all(pred)` | `all(...)` | True if all match |
| `.distinct()` | `list(set(...))` | Remove duplicates |
| `.group_by(key)` | `itertools.groupby(...)` | Group by key |

## Chained Operations and Smart Dispatch

When you chain two or more collection methods, Zinc uses smart dispatch at runtime to pick the best backend:

```zinc
var int total = orders
    .filter(o -> o["status"] == "active")
    .map(o -> o["amount"])
    .sum()
```

For plain lists, this generates `_zinc_collect()` with method chaining. For structured data (DataFrames), the same Zinc code can dispatch to Polars:

```zinc
// Same code, Polars backend:
// pl.DataFrame(orders).lazy().filter(...).select(...).sum().collect().item()
```

## Comprehensions

Zinc supports Python-style list and dict comprehensions:

### List Comprehensions

```zinc
var list<int> squares = [x ** 2 for x in range(10)]
var list<int> evens = [x for x in numbers if x % 2 == 0]
```

### Dict Comprehensions

```zinc
var dict<str, int> lengths = {w: len(w) for w in words}
```

### Auto Generator Promotion

The transpiler automatically strips brackets inside `sum`, `any`, and `all` to produce generator expressions (more memory-efficient):

```zinc
var int total = sum([x for x in items])
// transpiles to: sum(x for x in items)
```

## Working with Lists

```zinc
var list<str> names = ["Alice", "Bob", "Charlie"]
names.append("Dave")
var int count = len(names)

// Slicing
var list<str> first_two = names.take(2)
var list<str> rest = names.skip(1)

// Check membership
if "Alice" in names {
    print("found")
}
```

## Working with Dicts

```zinc
var dict<str, int> ages = {"Alice": 30, "Bob": 25}
ages["Charlie"] = 35

// Iterate
for key, value in ages.items() {
    print("{key} is {value}")
}

// Delete a key
del ages["Bob"]
```

## Tuples

Tuples are immutable sequences:

```zinc
var point = (3, 5)
var rgb = (255, 128, 0)

fn swap(int a, int b) {
    return b, a                  // return tuple without parens
}
var x, y = swap(1, 2)           // tuple unpacking
```
