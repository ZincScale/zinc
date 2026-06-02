# Nested functions (closures) defined and called locally, plus nonlocal for
# mutating an enclosing local. Go closures capture by reference.


def scale_all(items: list[int], factor: int) -> list[int]:
    def scale(x: int) -> int:
        return x * factor  # captures `factor` from the enclosing scope
    out: list[int] = []
    for i in items:
        out.append(scale(i))
    return out


print(scale_all([1, 2, 3], 10))


def running_total(nums: list[int]) -> int:
    total: int = 0

    def add(x: int) -> None:
        nonlocal total
        total = total + x

    for n in nums:
        add(n)
    return total


print(running_total([5, 10, 15]))


def build_greeting(name: str) -> str:
    def punctuate(s: str) -> str:
        def emphasize(t: str) -> str:
            return t + "!"
        return emphasize(s) + "!"
    return punctuate("hi " + name)


print(build_greeting("sam"))


# A nested helper used inside a comprehension.
def squares_of_evens(limit: int) -> list[int]:
    def is_even(x: int) -> bool:
        return x % 2 == 0

    def square(x: int) -> int:
        return x * x

    return [square(n) for n in range(limit) if is_even(n)]


print(squares_of_evens(8))
