# Generator expressions as the sole call argument (materialized).
row = [1, 2, 3]
print(", ".join(str(x) for x in row))
print(sum(x * x for x in range(5)))
print(max(len(w) for w in ["a", "bbb", "cc"]))
print(sorted(x % 3 for x in range(6)))

# any() / all() including the empty-iterable conventions.
print(any(x > 2 for x in row))
print(all(x > 0 for x in row))
print(any([]))
print(all([]))

# Sequence repetition: str * int and list * int (either order).
print("=" * 20)
print("ab" * 3)
print(3 * "xy")
n = 4
print("-" * n)
zeros = [0] * 5
print(zeros)
print([1, 2] * 3)
print(", ".join(str(x) for x in zeros))
