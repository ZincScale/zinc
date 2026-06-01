# `-> None` return annotation (void) — biting every annotated __init__.
class Account:
    def __init__(self, balance: int) -> None:
        self.balance = balance

    def deposit(self, amount: int) -> None:
        self.balance += amount

    def value(self) -> int:
        return self.balance


a = Account(100)
a.deposit(50)
print(a.value())


# `except ... as e` where e is unused (Python allows it).
try:
    1 / 0
except ZeroDivisionError as e:
    print("caught division by zero")


# Power operator ** : right-associative, binds tighter than unary minus,
# int**non-negative-int stays int, float/negative exponent → float.
print(2 ** 10, 2 ** -1, 2.0 ** 3, -2 ** 2, 2 ** 3 ** 2)


# round() with banker's rounding; round(x, n) → float.
print(round(2.5), round(3.5), round(0.5), round(3.14159, 2))


# int(str) / float(str) with whitespace tolerance.
print(int("42"), float("1.5"), int("  7  "))


# A function returning None explicitly, and one with a power expression.
def area(r: float) -> float:
    return 3.14159 * r ** 2


print(round(area(2.0), 5))
