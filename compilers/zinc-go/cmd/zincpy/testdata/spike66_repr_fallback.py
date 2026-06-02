# __str__ falls back to __repr__ when absent (Python semantics), and containers
# format elements with __repr__.

# only __repr__: str()/print() fall back to it
class Point:
    def __init__(self, x: int, y: int) -> None:
        self.x = x
        self.y = y
    def __repr__(self) -> str:
        return "Point(" + str(self.x) + ", " + str(self.y) + ")"

p = Point(3, 4)
print(p)            # falls back to __repr__
print(str(p))       # falls back to __repr__
print([p, p])       # list shows repr of each element
print((p, p))       # tuple too

# both defined: print/str use __str__, containers use __repr__
class Money:
    def __init__(self, cents: int) -> None:
        self.cents = cents
    def __str__(self) -> str:
        return "$" + str(self.cents // 100) + "." + str(self.cents % 100)
    def __repr__(self) -> str:
        return "Money(" + str(self.cents) + ")"

m = Money(1299)
print(m)            # __str__
print(str(m))       # __str__
print([m])          # __repr__ for the element
print([Money(500), Money(99)])
