import sys

class Counter:
    def __init__(self, count: int = 0):
        self.count = count

    def increment(self):
        self.count = (self.count + 1)

    def __str__(self) -> str:
        return f"Counter({self.count})"

x = None
if (x is not None):
    print("has value")
items = ["a", "b", "c"]
if ("d" not in items):
    print("d not found")
a, b = divmod(10, 3)
print(f"a={a}, b={b}")
words = ["hello", "world", "zinc"]
lengths = {w: len(w) for w in words}
print(f"lengths: {lengths}")
c = Counter()
c.increment()
c.increment()
c.increment()
print(c)
name = "world"
print(f"Hello, {name}!")
numbers = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
total = sum(n for n in numbers if ((n % 2) == 0))
print(f"Sum of evens: {total}")
