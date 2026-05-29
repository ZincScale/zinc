# multi-target list comprehension
pairs = [(1, 2), (3, 4), (5, 6)]
print([a + b for a, b in pairs])
print([a * b for a, b in pairs])

# over dict items
prices = {"a": 1, "b": 2, "c": 3}
print(sorted([k for k, v in prices.items()]))
print(sorted([v * 10 for k, v in prices.items()]))

# over enumerate
print([i for i, c in enumerate("xyz")])

# single-target still works
print([x * x for x in range(5)])
print([x for x in range(10) if x % 2 == 0])
