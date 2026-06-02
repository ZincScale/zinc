# First-class function values: a function (named, nested, or returned closure)
# stored in a variable or passed as an argument is called via reflection when
# its static type is only `interface{}` (an unannotated param, an `any` return).


def greet(name: str) -> str:
    return "hi " + name


def apply(fn, x: str) -> str:
    return fn(x)


def make_adder(n: int):
    def add(x: int) -> int:
        return x + n
    return add


def call_op(op, x: int) -> int:
    return op(x)


def double(x: int) -> int:
    return x * 2


# Named function bound to a variable, then called.
g = greet
print(g("bob"))

# Higher-order: a function passed as an unannotated argument and called.
print(apply(greet, "sue"))

# Returned closure capturing `n`: stored then called, and called chained.
add10 = make_adder(10)
print(add10(5))
print(make_adder(100)(7))

# A named function passed through a higher-order helper.
print(call_op(double, 21))


# map / filter / sorted(key=) accept a named function (not just a lambda),
# since the higher-order helpers invoke the callable via reflection.
def square(x: int) -> int:
    return x * x


def is_odd(x: int) -> bool:
    return x % 2 == 1


nums = [1, 2, 3, 4, 5]
print(list(map(square, nums)))
print(list(filter(is_odd, nums)))
print(sorted(["ccc", "a", "bb"], key=len))


def first_char(s: str) -> str:
    return s[0]


print(sorted(["banana", "apple", "cherry"], key=first_char))
