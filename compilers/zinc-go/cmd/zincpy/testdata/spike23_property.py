class Rectangle:
    def __init__(self, width: float, height: float):
        self.width = width
        self.height = height

    @property
    def area(self) -> float:
        return self.width * self.height

    @property
    def perimeter(self) -> float:
        return 2.0 * (self.width + self.height)

    def scaled(self, factor: float) -> float:
        # a property read inside a method
        return self.area * factor


r = Rectangle(3.0, 4.0)

# property access: no parentheses
print(r.area)
print(r.perimeter)

# property result in arithmetic
print(r.area + 1.0)
print(r.area * 2)

# regular method still works alongside properties
print(r.scaled(10.0))


class Temperature:
    def __init__(self, celsius: float):
        self.celsius = celsius

    @property
    def fahrenheit(self) -> float:
        return self.celsius * 9.0 / 5.0 + 32.0


t = Temperature(100.0)
print(t.fahrenheit)
print(t.celsius)

print("done")
