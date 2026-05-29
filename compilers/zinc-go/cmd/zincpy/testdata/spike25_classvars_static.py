class Circle:
    PI = 3.14159
    SIDES = 0

    def __init__(self, radius: float):
        self.radius = radius

    def area(self) -> float:
        # class constant via self
        return self.PI * self.radius * self.radius

    @staticmethod
    def unit_area() -> float:
        # class constant via the class name
        return Circle.PI

    @classmethod
    def unit(cls) -> "Circle":
        # alternate constructor using cls(...)
        return cls(1.0)


# class constants read off the class
print(Circle.PI)
print(Circle.SIDES)

c = Circle(2.0)
print(c.area())

# staticmethod via the class
print(Circle.unit_area())

# classmethod alternate constructor
u = Circle.unit()
print(u.area())


class Config:
    MAX = 100
    NAME = "prod"
    DEBUG = False

    @staticmethod
    def doubled_max() -> int:
        return Config.MAX * 2


print(Config.MAX)
print(Config.NAME)
print(Config.DEBUG)
print(Config.doubled_max())

print("done")
