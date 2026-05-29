# Python methods whose names collide with the codegen's built-in collection /
# string method dispatch (add, size, upper, keys, values, replace, sort, ...)
# must call the user's method, not the built-in lowering, when the receiver is
# a class instance.
class Store:
    def __init__(self):
        self.n = 0

    def add(self, x: int) -> int:
        self.n = self.n + x
        return self.n

    def size(self) -> int:
        return self.n

    def keys(self) -> int:
        return 42

    def values(self) -> str:
        return "vals"

    def upper(self) -> str:
        return "UP"

    def replace(self, x: int) -> int:
        return x * 2

    def sort(self) -> str:
        return "sorted"

    def join(self, x: int) -> int:
        return x + 1

    def length(self) -> int:
        return self.n


s = Store()
print(s.add(3))
print(s.add(4))
print(s.size())
print(s.keys())
print(s.values())
print(s.upper())
print(s.replace(8))
print(s.sort())
print(s.join(10))
print(s.length())

# A plain variable named `it` (collides with no Python construct, but did with
# Zinc's implicit closure parameter) is an ordinary identifier.
nums = [5, 6, 7]
for it in nums:
    print(it)
