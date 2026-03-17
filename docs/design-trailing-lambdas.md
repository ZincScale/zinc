# Design: Trailing Lambdas + `it` Keyword

## Problem

Every LINQ call in Zinc requires explicit parameter types:

```zinc
users.Where((User u) -> u.age > 28).Select((User u) -> u.name).OrderBy((String s) -> s)
```

This is noisy. The types are inferrable from context. Kotlin solves this with trailing lambdas and an implicit `it` parameter.

## Solution

Two features that work together:

### 1. `it` — implicit single parameter

When a lambda has exactly one parameter, it's automatically named `it`. No declaration needed.

```zinc
// Before:
nums.Where((Int x) -> x > 3)

// After:
nums.Where { it > 3 }
```

### 2. Trailing lambda — last argument as block

When the last argument to a function is a lambda, it can be written as a `{ }` block after the call:

```zinc
// Before:
nums.Select((Int x) -> x * 2)

// After:
nums.Select { it * 2 }
```

If the function has other arguments before the lambda, they stay in parens:

```zinc
// Before:
nums.Aggregate(0, (Int acc, Int x) -> acc + x)

// After:
nums.Aggregate(0) { acc, x -> acc + x }
```

## Full Examples

### Lists

```zinc
var nums = [5, 3, 8, 1, 9, 2, 7, 4, 6]

// Filter
var big = nums.Where { it > 5 }

// Transform
var doubled = nums.Select { it * 2 }

// Sort
var sorted = nums.OrderBy { it }
var desc = nums.OrderByDescending { it }

// Aggregation
var total = nums.Sum()                    // no lambda needed
var sum = nums.Aggregate(0) { acc, x -> acc + x }

// Terminal queries
var first = nums.First { it > 7 }
var hasNeg = nums.Any { it < 0 }
var allPos = nums.All { it > 0 }
var count = nums.Count { it > 5 }

// Subsetting
var top3 = nums.OrderBy { it }.Take(3)
var unique = nums.Distinct()

// Chaining
var result = nums.Where { it > 3 }
                 .Select { it * it }
                 .OrderBy { it }
                 .Take(5)
```

### Objects

```zinc
data User(pub String name, pub Int age, pub Bool active)

var users = [User("Alice", 30, true), User("Bob", 25, false), User("Carol", 35, true)]

// Filter + map
var names = users.Where { it.active }.Select { it.name }

// Sort by field
var byAge = users.OrderByDescending { it.age }

// Group
var byStatus = users.GroupBy { it.active }

// Any/All
var hasSenior = users.Any { it.age > 30 }

// Complex chain
var result = users.Where { it.active }
                  .Select { it.name.ToUpper() }
                  .OrderBy { it }
                  .Take(10)
```

### Maps

For map operations, `it` is the entry (KeyValuePair). Access `.Key` and `.Value`:

```zinc
var scores = {"Alice": 95, "Bob": 72, "Carol": 88}

// Filter entries
var passing = scores.Where { it.Value > 80 }

// Extract keys
var names = scores.Select { it.Key }

// Query
var anyFailing = scores.Any { it.Value < 60 }

// Transform to new map
var curved = scores.ToDictionary { it.Key } { it.Value + 5 }
```

### Multi-param lambdas

When more than one parameter is needed, name them explicitly with `->`:

```zinc
// Aggregate with accumulator
var sum = nums.Aggregate(0) { acc, x -> acc + x }

// Zip
var pairs = names.Zip(ages) { name, age -> "{name}: {age}" }

// ToDictionary with key + value selectors (two lambdas)
var lookup = users.ToDictionary({ it.name }) { it.age }
```

## Parsing Rules

1. If the token after `)` or after a method name is `{`, parse it as a trailing lambda
2. Inside `{ ... }` without `->`, the single parameter is `it`
3. Inside `{ a, b -> expr }`, the named parameters are `a` and `b`
4. Trailing lambda is always the **last** argument
5. If a method takes only a lambda, the `()` can be omitted: `nums.Where { it > 3 }`
6. If a method takes args + lambda, args stay in parens: `nums.Aggregate(0) { acc, x -> acc + x }`

## Type Inference

The type of `it` (or named params) is inferred from context:

- `List<Int>.Where { ... }` → `it` is `Int`
- `List<User>.Select { ... }` → `it` is `User`
- `Map<String, Int>.Where { ... }` → `it` is `KeyValuePair<String, Int>` (has `.Key` and `.Value`)
- `List<T>.Aggregate(seed) { acc, x -> ... }` → `acc` is seed type, `x` is `T`

The typechecker already knows collection element types from generic type parameters.

## C# Code Generation

Trailing lambdas compile to the same C# LINQ as today:

```zinc
// Zinc:
users.Where { it.age > 28 }.Select { it.name }

// Generated C#:
users.Where(it => it.Age > 28).Select(it => it.Name)
```

The parameter name `it` is just a valid C# identifier. Multi-param:

```zinc
// Zinc:
nums.Aggregate(0) { acc, x -> acc + x }

// Generated C#:
nums.Aggregate(0, (acc, x) => acc + x)
```

## Backward Compatibility

The existing explicit lambda syntax `(Type param) -> expr` continues to work. Trailing lambdas are purely additive — no breaking changes.

```zinc
// All three are equivalent:
nums.Where((Int x) -> x > 3)          // explicit (today)
nums.Where(x -> x > 3)                // inferred param type (P2)
nums.Where { it > 3 }                 // trailing lambda (this feature)
```

## What This Replaces

This is the **only** collection expressiveness feature needed. No comprehensions, no query syntax, no new keywords. One mechanism that covers all 22 LINQ methods + maps + any future collection type.
