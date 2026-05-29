# parallel literal assignment
a, b = 1, 2
print(a)
print(b)

# swap
a, b = b, a
print(a)
print(b)

# parallel assign with expressions and mixed types
x, y, z = 10 + 5, "hi", 3.5
print(x)
print(y)
print(z)

# swap inside a function, used in a classic gcd
def gcd(p: int, q: int) -> int:
    while q != 0:
        p, q = q, p % q
    return p

print(gcd(48, 36))
print(gcd(1071, 462))

# fibonacci via parallel update
def fib(n: int) -> int:
    a, b = 0, 1
    for _ in range(n):
        a, b = b, a + b
    return a

print(fib(10))
print(fib(20))
