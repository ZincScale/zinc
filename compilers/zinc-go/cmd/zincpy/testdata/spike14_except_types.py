def classify(n: int) -> str:
    if n < 0:
        raise ValueError("negative")
    if n == 0:
        raise ZeroDivisionError("zero")
    return "positive"


def handle(n: int) -> str:
    try:
        return classify(n)
    except ValueError as e:
        return "value error: " + str(e)
    except ZeroDivisionError as e:
        return "zero error: " + str(e)


print(handle(5))
print(handle(-1))
print(handle(0))


def multi(n: int) -> str:
    try:
        if n == 1:
            raise KeyError("k")
        if n == 2:
            raise IndexError("i")
        raise ValueError("v")
    except (KeyError, IndexError):
        return "lookup"
    except Exception:
        return "other"


print(multi(1))
print(multi(2))
print(multi(3))


def with_reraise(n: int) -> str:
    try:
        try:
            raise ValueError("inner")
        except KeyError:
            return "caught key"
    except ValueError as e:
        return "outer caught: " + str(e)


print(with_reraise(1))
