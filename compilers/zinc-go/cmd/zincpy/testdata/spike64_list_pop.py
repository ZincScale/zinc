# list.pop([i]): removes and returns an element. Lowered via a statement-hoist
# (value-read + slice write-back) so it works in every position.

# bare statement (value discarded)
xs: list[int] = [1, 2, 3, 4, 5]
xs.pop()
print(xs)

# assignment form
last: int = xs.pop()
print(last, xs)

# pop with an index
mid: int = xs.pop(1)
print(mid, xs)

# negative index
end: int = xs.pop(-1)
print(end, xs)

# pop inside a larger expression (two pops, left-to-right order)
ys: list[int] = [10, 20, 30]
print(ys.pop() + ys.pop())
print(ys)

# the idiomatic drain loop (pop in the body, not the condition)
stack: list[str] = ["a", "b", "c"]
out: list[str] = []
while stack:
    out.append(stack.pop())
print(out)

# accumulate while draining
nums: list[int] = [1, 2, 3, 4]
total: int = 0
while nums:
    total = total + nums.pop()
print(total)

# pop in nested while/if blocks (write-back must target the outer variable)
def drain_evens(zs: list[int]) -> list[int]:
    res: list[int] = []
    while zs:
        v: int = zs.pop()
        if v % 2 == 0:
            res.append(v)
    return res

print(drain_evens([1, 2, 3, 4, 5, 6]))

# error cases match CPython's messages
empty: list[int] = []
try:
    empty.pop()
except IndexError as e:
    print("e1:", str(e))

small: list[int] = [1, 2, 3]
try:
    small.pop(10)
except IndexError as e:
    print("e2:", str(e))
