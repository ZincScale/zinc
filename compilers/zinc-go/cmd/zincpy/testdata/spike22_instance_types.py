class Circle:
    def __init__(self, radius: float):
        self.radius = radius
        self.scale = 2

    def area(self) -> float:
        return 3.14159 * self.radius * self.radius

    def diameter(self) -> float:
        # arithmetic mixing an int field and a float field — needs field types
        return self.radius * self.scale


c = Circle(2.0)

# method result used in float arithmetic (area() returns float)
print(c.area() + 1.0)
print(c.diameter())

# instance field read in arithmetic (float field promoted against int)
print(c.radius * 3)
print(c.radius + 1)


def describe(shape: Circle) -> float:
    # instance passed as a typed param: shape.area() resolves to float
    return shape.area() * 2.0


print(describe(c))


class Account:
    def __init__(self, balance: float):
        self.balance = balance

    def deposit(self, amount: float):
        self.balance = self.balance + amount

    def report(self) -> float:
        return self.balance


a = Account(100.0)
a.deposit(50)        # int arg into float field arithmetic
a.deposit(25.5)
print(a.report())

print("done")
