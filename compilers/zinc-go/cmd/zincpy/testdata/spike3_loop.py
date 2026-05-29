def total(n: int) -> int:
    s = 0
    for i in range(n):
        s = s + i
    return s


def count_evens(n: int) -> int:
    c = 0
    for i in range(n):
        if i % 2 == 0:
            c = c + 1
    return c


print(total(5))
print(total(100))
print(count_evens(10))
