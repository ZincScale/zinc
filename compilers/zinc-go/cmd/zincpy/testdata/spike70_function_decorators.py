# Function decorators: `@deco def f` keeps the body under a hidden name and
# rebinds `f` to deco(impl) as a package-level var, so `f(...)` dispatches
# through the dynamic-call path. Works across functions (package-scope var).


def shout(fn):
    def wrapper(name: str) -> str:
        return fn(name).upper()
    return wrapper


@shout
def greet(name: str) -> str:
    return "hello " + name


print(greet("world"))


# Stacked decorators apply bottom-up: render = bold(italic(impl)).
def bold(fn):
    def wrap(s: str) -> str:
        return "<b>" + fn(s) + "</b>"
    return wrap


def italic(fn):
    def wrap(s: str) -> str:
        return "<i>" + fn(s) + "</i>"
    return wrap


@bold
@italic
def render(s: str) -> str:
    return s


print(render("hi"))


# A decorated function called from another function (cross-function visibility).
def render_all(items: list[str]) -> list[str]:
    out: list[str] = []
    for it in items:
        out.append(str(render(it)))
    return out


print(render_all(["a", "b"]))


# Identity decorator + a two-argument decorated function.
def register(fn):
    return fn


@register
def add(a: int, b: int) -> int:
    return a + b


print(add(2, 3))


# Stateful decorator: a closure over a mutable cell counts the calls.
def counted(fn):
    calls = [0]

    def wrap(x: int) -> int:
        calls[0] = calls[0] + 1
        return fn(x) + calls[0]

    return wrap


@counted
def base(x: int) -> int:
    return x * 10


print(base(1))
print(base(1))
print(base(1))
