# Walrus operator := (assignment expression). The binding is hoisted to a
# statement before the one that uses it, and the name leaks to the enclosing
# scope (as in Python).

# in an if-condition; the name is usable in the body AND after the if
data: list[int] = [1, 2, 3, 4, 5]
if (n := len(data)) > 3:
    print("long:", n)
print("len was", n)

# reused within the same expression
print((x := 10) + x)

# narrow an FFI/dynamic value with a walrus, then use the static name
import math
if (root := float(math.sqrt(16.0))) > 3.0:
    print("root", round(root, 2))

# walrus feeding a computation
nums: list[int] = [2, 4, 6, 8]
if (s := sum(nums)) > 10:
    print("sum", s, "avg", s // len(nums))

# walrus in a print argument
print("doubled:", (d := 21 * 2))
print(d)

# chained use: bind once, branch on it
text: str = "hello world"
if (length := len(text)) >= 5:
    print(length, "chars")
