# Collections

## Collection Literals

List and map literals are automatically typed. When all elements share the same type, the output uses that concrete type:

```zinc
main() {
    var nums = [1, 2, 3]                      // List<Int>
    var names = ["Alice", "Bob"]              // List<String>
    var scores = {"math": 95, "sci": 88}     // Map<String, Int>

    // Empty literals use the declared type
    Map<String, Int> m = {}
    List<Int> l = []

    // Nested collections
    var grid = [[1, 2], [3, 4]]
}
```

## Slicing

Extract sub-sequences from lists and strings:

```zinc
var nums = [1, 2, 3, 4, 5]

// Bracket syntax — [low:high], either bound optional
print(nums[1:3])    // [2 3]
print(nums[2:])     // [3 4 5]
print(nums[:3])     // [1 2 3]

// OO method
print(nums.slice(1, 3))   // [2 3]

// Works on strings too
var s = "Hello, Zinc!"
print(s[0:5])          // Hello
```

## Collection Methods (C# Backend)

When targeting the C# AOT backend, Zinc supports LINQ-style collection methods:

```zinc
main() {
    var nums = [5, 3, 8, 1, 9, 2, 7, 4, 6]

    // Filtering
    var evens = nums.Where((Int x) -> x % 2 == 0)

    // Transformation
    var doubled = nums.Select((Int x) -> x * 2)

    // Sorting
    var sorted = nums.OrderBy((Int x) -> x)

    // Querying
    var first = nums.First((Int x) -> x > 7)
    var hasNeg = nums.Any((Int x) -> x < 0)
    var allPos = nums.All((Int x) -> x > 0)

    // Aggregation
    var total = nums.Sum()
    var lo = nums.Min()
    var hi = nums.Max()
    var product = nums.Aggregate(1, (Int a, Int x) -> a * x)

    // Subsetting
    var top3 = nums.Take(3)
    var rest = nums.Skip(3)
    var unique = [1, 2, 2, 3].Distinct()

    // Chaining
    var result = nums.Where((Int x) -> x % 2 == 0)
                     .Select((Int x) -> x * x)
                     .OrderBy((Int x) -> x)
                     .Take(3)
}
```

### Method Reference

| Method | Description | Example |
|--------|-------------|---------|
| `Where(predicate)` | Filter elements | `nums.Where((x) -> x > 5)` |
| `Select(transform)` | Transform elements | `nums.Select((x) -> x * 2)` |
| `First()` / `First(predicate)` | First element (or first matching) | `nums.First((x) -> x > 5)` |
| `FirstOrDefault(predicate)` | First matching or default | `nums.FirstOrDefault((x) -> x > 100)` |
| `Last()` / `Last(predicate)` | Last element | `nums.Last()` |
| `Any()` / `Any(predicate)` | True if any match | `nums.Any((x) -> x < 0)` |
| `All(predicate)` | True if all match | `nums.All((x) -> x > 0)` |
| `Count()` / `Count(predicate)` | Count (optionally with filter) | `nums.Count((x) -> x > 5)` |
| `Sum()` / `Sum(selector)` | Sum values | `nums.Sum()` |
| `Min()` / `Max()` | Minimum / maximum | `nums.Min()` |
| `Average()` | Average value | `nums.Average()` |
| `Aggregate(seed, func)` | Fold / reduce | `nums.Aggregate(0, (a, x) -> a + x)` |
| `OrderBy(key)` | Sort ascending | `nums.OrderBy((x) -> x)` |
| `OrderByDescending(key)` | Sort descending | `nums.OrderByDescending((x) -> x)` |
| `Take(n)` | First n elements | `nums.Take(3)` |
| `Skip(n)` | Skip first n | `nums.Skip(3)` |
| `Distinct()` | Remove duplicates | `nums.Distinct()` |
| `SelectMany(func)` | Flatten nested collections | `lists.SelectMany((x) -> x)` |
| `GroupBy(key)` | Group by key | `items.GroupBy((x) -> x.category)` |
| `Zip(other, func)` | Combine two lists | `a.Zip(b, (x, y) -> x + y)` |
| `ToDictionary(key, value)` | Convert to map | `items.ToDictionary((x) -> x.id, (x) -> x)` |
| `ToList()` | Materialize to list | `query.ToList()` |
| `ForEach(action)` | Execute action per element | `nums.ForEach((x) -> x * 2)` |

> **Note:** Collection methods use C# LINQ under the hood and are fully supported in Zinc.
