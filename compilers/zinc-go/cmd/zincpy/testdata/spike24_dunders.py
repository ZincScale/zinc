class Vec:
    def __init__(self, x: int, y: int):
        self.x = x
        self.y = y

    def __add__(self, other: "Vec") -> "Vec":
        return Vec(self.x + other.x, self.y + other.y)

    def __sub__(self, other: "Vec") -> "Vec":
        return Vec(self.x - other.x, self.y - other.y)

    def __eq__(self, other: "Vec") -> bool:
        return self.x == other.x and self.y == other.y

    def __str__(self) -> str:
        return "Vec(" + str(self.x) + ", " + str(self.y) + ")"


a = Vec(1, 2)
b = Vec(3, 4)

print(a + b)
print(b - a)
print(a == b)
print(a == Vec(1, 2))

# chained operator results (a + b + a)
print(a + b + a)


class Counter:
    def __init__(self, n: int):
        self.n = n

    def __len__(self) -> int:
        return self.n


c = Counter(5)
print(len(c))

print("done")
