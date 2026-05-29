def add(x: int, y: int) -> int:
    return x + y


def square(n: int) -> int:
    return n * n


print(add(3, 4))
print(square(5))
print(add(square(2), square(3)))
