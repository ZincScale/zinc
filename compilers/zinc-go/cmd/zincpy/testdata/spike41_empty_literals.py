def total(xs: list[int]) -> int:
    s = 0
    for x in xs:
        s = s + x
    return s

print(total([]))
print(total([1, 2, 3]))

def names(extra: list[str]) -> int:
    return len(extra)

print(names([]))
print(names(["a", "b"]))

def make_empty() -> list[int]:
    return []

e = make_empty()
print(len(e))

def first_or(xs: list[int], default: int) -> int:
    if len(xs) == 0:
        return default
    return xs[0]

print(first_or([], 99))
print(first_or([5, 6], 99))
