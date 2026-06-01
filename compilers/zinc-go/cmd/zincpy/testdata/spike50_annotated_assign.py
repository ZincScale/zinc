import json

# PEP 526 annotated locals: the annotation pins the static type.
x: int = 5
y: float = 2.0
print(x + 1)
print(y * 2)


# The annotation drives boundary narrowing: a dynamic value (json.loads → Any)
# assigned to an int-annotated local is coerced, so it stays a static int.
def total(s: str) -> int:
    n: int = json.loads(s)
    return n + 10


print(total("5"))


# `: Any` is the explicit escape hatch for a genuinely-dynamic local — it boxes
# so a later dynamic reassignment is allowed (what a bare `t = 0` would reject).
def accumulate(s: str):
    d = json.loads(s)
    t: Any = 0
    t = t + d
    return t


print(accumulate("7"))


# An annotated empty list takes its element type, so native append/sum work.
xs: list[int] = []
xs.append(3)
xs.append(7)
print(sum(xs))


# Annotated str narrowing from a dynamic value.
def label(v) -> str:
    s: str = v
    return s.upper()


print(label("hello"))
