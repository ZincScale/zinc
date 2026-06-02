# One-line compound suites: the body of a compound statement on the same line
# as its header (`if c: foo()`), plus `;`-separated simple statements.

# --- inline def ---
def square(n: int) -> int: return n * n
def greet(name: str) -> str: return "hi " + name

print(square(7))
print(greet("ada"))

# --- inline if / elif / else ---
def classify(n: int) -> str:
    if n < 0: return "neg"
    elif n == 0: return "zero"
    else: return "pos"

print(classify(-3))
print(classify(0))
print(classify(42))

# --- inline if as a guard clause ---
def first_even(xs: list[int]) -> int:
    for x in xs:
        if x % 2 == 0: return x
    return -1

print(first_even([1, 3, 5, 8, 9]))

# --- inline for / while ---
total: int = 0
for i in range(5): total = total + i
print(total)

n: int = 3
while n > 0: n = n - 1
print(n)

# --- inline try / except / finally ---
def safe_div(a: int, b: int) -> int:
    try: return a // b
    except ZeroDivisionError: return 0

print(safe_div(10, 2))
print(safe_div(10, 0))

# --- `;`-separated simple statements on one line (top level + inline body) ---
a: int = 1; b: int = 2; c: int = 3
print(a + b + c)

def sum3(x: int, y: int, z: int) -> int:
    s: int = 0; s = s + x; s = s + y; s = s + z
    return s

print(sum3(4, 5, 6))

# trailing semicolon is allowed
d: int = 9;
print(d)

# --- inline class bodies ---
class Empty: pass

class Box:
    def value(self) -> int: return 99

Empty()
print(Box().value())

# inline `if c: a; b` with two statements in the body
flag: bool = True
out: int = 0
if flag: out = out + 10; out = out + 20
print(out)

# nested inline: inline if inside a normal for inside a def
def count_pos(xs: list[int]) -> int:
    c: int = 0
    for x in xs:
        if x > 0: c = c + 1
    return c

print(count_pos([1, 2, 3, 4, 5]))
