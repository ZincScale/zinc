def total(*nums) -> int:
    s = 0
    for n in nums:
        s = s + int(n)
    return s

print(total(1, 2, 3))
print(total(10, 20, 30, 40))
print(total())

def first(label: str, *rest) -> str:
    out = label + ":"
    for r in rest:
        out = out + " " + str(r)
    return out

print(first("nums", 1, 2, 3))
print(first("empty"))

def count_args(*args) -> int:
    return len(args)

print(count_args(1, 2, 3, 4, 5))

def describe(*items) -> str:
    if len(items) == 0:
        return "nothing"
    return "first is " + str(items[0])

print(describe())
print(describe("a", "b"))
