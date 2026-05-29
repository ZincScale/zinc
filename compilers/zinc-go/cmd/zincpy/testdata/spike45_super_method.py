class Shape:
    def __init__(self, name: str):
        self.name = name

    def describe(self) -> str:
        return "shape:" + self.name

    def area(self) -> float:
        return 0.0

    def __str__(self) -> str:
        return "Shape(" + self.name + ")"


class Circle(Shape):
    def __init__(self, r: float):
        super().__init__("circle")
        self.r = r

    # Override-and-extend: call the parent method, then add to its result.
    def describe(self) -> str:
        return super().describe() + " r=" + str(self.r)

    def area(self) -> float:
        return 3.14 * self.r * self.r

    # Dunder via super: __str__ → String().
    def __str__(self) -> str:
        return super().__str__() + "/circle"


class Cylinder(Circle):
    def __init__(self, r: float, h: float):
        super().__init__(r)
        self.h = h

    # Multilevel: super() reaches Circle.area(), used in arithmetic.
    def area(self) -> float:
        return super().area() * 2.0 + 6.28 * self.r * self.h

    def describe(self) -> str:
        return super().describe() + " h=" + str(self.h)


c = Circle(2.0)
print(c.describe())
print(c.area())
print(str(c))

cy = Cylinder(2.0, 5.0)
print(cy.describe())
print(cy.area())
print(str(cy))

# str() formats floats the Python way.
print(str(2.0))
print(str(7))
print(str(True))
