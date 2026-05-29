def divide(a: int, b: int) -> str:
    try:
        a // b
        return "ok"
    except ZeroDivisionError:
        return "zero div"


print(divide(10, 2))
print(divide(10, 0))


def truediv(a: int, b: int) -> str:
    try:
        a / b
        return "ok"
    except ZeroDivisionError:
        return "zero div"


print(truediv(6, 3))
print(truediv(6, 0))


def reraise_demo(n: int) -> str:
    try:
        try:
            raise ValueError("boom")
        except ValueError:
            print("logging")
            raise
    except ValueError as e:
        return "reraised: " + str(e)


print(reraise_demo(1))


def with_else(n: int) -> str:
    try:
        100 // n
    except ZeroDivisionError:
        return "div by zero"
    else:
        return "no error"


print(with_else(5))
print(with_else(0))
