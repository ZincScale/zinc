# Tuple-unpack from a stored tuple VALUE: `a, b, c = t`. Lowers to per-element
# zincpyGetItem reads, while a function call stays on the Go multi-return path.
t = (1, "two", 3.5)
a, b, c = t
print(a)
print(b)
print(c)

# homogeneous tuple, then use the unpacked numbers
nums = (10, 20, 30)
x, y, z = nums
print(x + y + z)

# unpack an element pulled out of a list of pairs (RHS is an index expression)
pairs = [(1, 2), (3, 4), (5, 6)]
p, q = pairs[1]
print(p)
print(q)

# swap still works
p, q = q, p
print(p)
print(q)

# multi-return function unpack still works (Go multi-value path, not GetItem)
def first_last(xs: list[int]) -> tuple[int, int]:
    return xs[0], xs[len(xs) - 1]

lo, hi = first_last([4, 2, 7, 1])
print(lo)
print(hi)
