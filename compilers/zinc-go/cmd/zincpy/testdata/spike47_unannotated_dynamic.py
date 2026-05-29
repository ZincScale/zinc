# Unannotated parameters are dynamic (duck-typed): their uses route through the
# runtime helpers, so the same function works across types.

def add(a, b):
    return a + b

def first(xs):
    return xs[0]

def size(xs):
    return len(xs)

def pick(flag, a, b):
    if flag:
        return a
    return b

# Annotated return type: a dynamic value is coerced to it on return.
def describe(x) -> str:
    if isinstance(x, int):
        return "int=" + str(x)
    if isinstance(x, str):
        return "str=" + x
    if isinstance(x, float):
        return "float=" + str(x)
    return "other"

print(add(2, 3))
print(add("a", "b"))
print(add(1.5, 2.5))
print(add([1, 2], [3, 4]))

print(first([10, 20, 30]))
print(first("hello"))
print(first((7, 8, 9)))

print(size([1, 2, 3, 4]))
print(size("hello"))
print(size({"a": 1, "b": 2}))

print(pick(True, "yes", "no"))
print(pick(False, 1, 2))

print(describe(7))
print(describe("hi"))
print(describe(3.5))
print(describe([1]))

# Dynamic dispatch over a heterogeneous list (loop var named `it`, which
# collides with a Zinc-codegen reserved identifier and is renamed).
items = [1, "two", 3.0, [4]]
for it in items:
    print(describe(it))
