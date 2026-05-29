def divmod2(a: int, b: int) -> tuple[int, int]:
    return a // b, a % b


q, r = divmod2(17, 5)
print(q)
print(r)


def min_max(xs: list[int]) -> tuple[int, int]:
    lo = xs[0]
    hi = xs[0]
    for x in xs:
        if x < lo:
            lo = x
        if x > hi:
            hi = x
    return lo, hi


a, b = min_max([3, 1, 4, 1, 5, 9, 2, 6])
print(a)
print(b)
print(a + b)


def stats(xs: list[int]) -> tuple[int, int, int]:
    total = 0
    count = 0
    for x in xs:
        total = total + x
        count = count + 1
    return total, count, total // count


s, c, avg = stats([10, 20, 30, 40])
print(s)
print(c)
print(avg)
