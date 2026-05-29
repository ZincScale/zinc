# tuple as a stored, first-class value
t = (1, "two", 3.5, True)
print(t)
print(t[0])
print(t[1])
print(t[-1])         # negative index
print(len(t))

# iterate a tuple
for item in t:
    print(item)

# single-element and empty-ish tuples print the Python way
single = (42,)
print(single)

# nested: list of tuples
pairs = [(1, 2), (3, 4), (5, 6)]
print(pairs)
print(pairs[1])
for p in pairs:
    print(p[0] + p[1])

# tuple of tuples
matrix = ((1, 0), (0, 1))
print(matrix)
print(matrix[0][1])

# multi-return (bare tuple) still coexists with value tuples
def first_last(xs: list[int]) -> tuple[int, int]:
    return xs[0], xs[len(xs) - 1]

lo, hi = first_last([4, 2, 7, 1])
print(lo)
print(hi)

print("done")
