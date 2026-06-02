# from X import name: each imported name binds to (module, attr) and routes
# through the libpython FFI — a call lowers like X.name(...), a bare value reads
# the attribute. Aliases (as) supported; deterministic math keeps it identical.
from math import sqrt, floor, ceil, gcd, factorial, pi

print(sqrt(16.0))
print(floor(3.7))
print(ceil(4.1))
print(gcd(48, 36))
print(factorial(5))

# bare value use of an imported constant
print(round(pi, 5))
r: float = 2.0
print(round(pi * r * r, 5))

# alias form
from math import factorial as fact
print(fact(6))

# imported name narrowed at an annotated boundary, then used as its static type
n: int = factorial(7)
print(n)
print(n // 7)

x: float = sqrt(2.0)
print(round(x, 6))

# imported call inside an expression / comprehension
roots: list[float] = [round(sqrt(float(k)), 4) for k in range(1, 5)]
print(roots)

# regular `import math` still coexists with the from-imported names
import math
print(math.trunc(9.99))
