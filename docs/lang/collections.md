# Zinc — Collections

## Collection Methods

Zinc provides fluent collection methods using Java's native collection API. No `.stream()` / `.collect()` ceremony — the transpiler handles that.

```zinc
items.filter(x -> x > 0)
items.map(x -> x * 2)
items.sum()
items.sortBy(x -> x.age)
items.limit(10)
items.skip(5)
items.findFirst(x -> x > 10)
items.anyMatch(x -> x > 0)
items.allMatch(x -> x > 0)
items.distinct()
items.groupBy(x -> x.category)
items.forEach(x -> print(x))
```

### The `it` Keyword

For single-parameter lambdas, use `it` instead of naming the parameter (Kotlin-style):

```zinc
items.filter(it > 0)                 // same as x -> x > 0
items.map(it * 2)                    // same as x -> x * 2
users.sortBy(it.age)                 // same as u -> u.age
users.findFirst(it.isActive)         // same as u -> u.isActive
users.anyMatch(it.role == "admin")   // same as u -> u.role == "admin"
names.forEach(print(it))             // same as n -> print(n)
```

`it` is the implicit parameter — the transpiler expands it to a named lambda. Use explicit `x ->` when you need clarity or have nested lambdas.

### Method Reference

| Zinc | Description | Java equivalent |
|---|---|---|
| `.filter(pred)` | Keep matching elements | `stream().filter(pred).toList()` |
| `.map(fn)` | Transform each element | `stream().map(fn).toList()` |
| `.flatMap(fn)` | Map + flatten | `stream().flatMap(fn).toList()` |
| `.sortBy(key)` | Sort by key function | `stream().sorted(comparing(key)).toList()` |
| `.limit(n)` | First n elements | `stream().limit(n).toList()` |
| `.skip(n)` | Skip first n elements | `stream().skip(n).toList()` |
| `.findFirst()` | First element (or none) | `stream().findFirst().orElse(null)` |
| `.findFirst(pred)` | First matching element | `stream().filter(pred).findFirst().orElse(null)` |
| `.anyMatch(pred)` | True if any match | `stream().anyMatch(pred)` |
| `.allMatch(pred)` | True if all match | `stream().allMatch(pred)` |
| `.noneMatch(pred)` | True if none match | `stream().noneMatch(pred)` |
| `.distinct()` | Remove duplicates | `stream().distinct().toList()` |
| `.groupBy(key)` | Group into Map | `stream().collect(groupingBy(key))` |
| `.count()` | Number of elements | `size()` |
| `.sum()` | Sum all elements | `stream().mapToInt(...).sum()` |
| `.min()` / `.max()` | Min/max element | `stream().min/max(...)` |
| `.reduce(init, fn)` | Fold to single value | `stream().reduce(init, fn)` |
| `.forEach(fn)` | Execute for each | `forEach(fn)` |
| `.toList()` | Convert to List | `stream().toList()` |
| `.toMap(k, v)` | Convert to Map | `stream().collect(toMap(k, v))` |
| `.toSet()` | Convert to Set | `new HashSet<>(list)` |

The transpiler generates the stream chain. You write the fluent call.

### Chained Operations

```zinc
int total = orders
    .filter(o -> o.status == "active")
    .map(o -> o.amount)
    .sum()

List<String> topNames = users
    .filter(u -> u.isActive)
    .sortBy(u -> u.lastName)
    .limit(10)
    .map(u -> u.name)
```

## Working with Lists

```zinc
List<String> names = ["Alice", "Bob", "Charlie"]
names.add("Dave")
names.addAll(["Eve", "Frank"])
int count = names.size()
String first = names.get(0)
names.set(0, "Alicia")
names.remove("Bob")

// Check membership
if "Alice" in names {
    print("found")
}

// Slicing
List<String> firstTwo = names.limit(2)
List<String> rest = names.skip(1)
```

## Working with Maps

```zinc
Map<String, int> ages = {"Alice": 30, "Bob": 25}
ages.put("Charlie", 35)
int age = ages.get("Alice")
boolean has = ages.containsKey("Alice")

// Iterate
for entry in ages.entrySet() {
    print("{entry.getKey()} is {entry.getValue()}")
}

// Remove a key
ages.remove("Bob")

// Get with default
int val = ages.getOrDefault("Unknown", 0)
```

## Working with Sets

```zinc
Set<String> tags = Set.of("java", "zinc", "flow")
tags.add("quarkus")
boolean has = tags.contains("java")

// Set operations
Set<String> union = Set.copyOf(a)
union.addAll(b)
```

## Tuples

Zinc has built-in tuple types for lightweight groupings. Each tuple arity generates a `value record` (Valhalla value type when available, regular record as fallback).

```zinc
var point = (3, 5)                   // Tuple2<int, int>
var rgb = (255, 128, 0)              // Tuple3<int, int, int>
var entry = ("Alice", 30, true)      // Tuple3<String, int, boolean>
```

### Tuple Access

```zinc
var x = point.0                      // first element
var y = point.1                      // second element
```

### Tuple Destructuring

```zinc
var (x, y) = point                   // destructure into variables
var (name, age, active) = entry

fn swap(int a, int b): (int, int) {
    return (b, a)
}
var (x, y) = swap(1, 2)             // x=2, y=1
```

### Tuples as Return Types

Functions returning tuples generate a record type:

```zinc
fn minMax(List<int> items): (int, int) {
    return (items.min(), items.max())
}

var (lo, hi) = minMax(numbers)
```

Transpiles to:
```java
record MinMaxResult(int _0, int _1) {}

static MinMaxResult minMax(List<Integer> items) {
    return new MinMaxResult(
        items.stream().mapToInt(Integer::intValue).min().orElseThrow(),
        items.stream().mapToInt(Integer::intValue).max().orElseThrow()
    );
}

var result = minMax(numbers);
var lo = result._0();
var hi = result._1();
```

### Typed Tuples

```zinc
var (String name, int age) = getUser()  // typed destructuring
```

### Tuples in Collections

```zinc
List<(String, int)> pairs = [("Alice", 30), ("Bob", 25)]

for (name, age) in pairs {
    print("{name} is {age}")
}
```
