from dataclasses import dataclass

@dataclass
class Point:
    x: int
    y: int

p = Point(1, 2)
print(p.x)
print(p.y)
print(p)
print(Point(3, 4))

# equality
print(Point(1, 2) == Point(1, 2))
print(Point(1, 2) == Point(1, 3))

# keyword args + defaults
@dataclass
class Config:
    name: str
    level: int = 1
    debug: bool = False

c = Config("prod")
print(c)
print(Config("dev", 5, True))
print(Config("test", debug=True))
print(c.level)

# field used in a method-like access + arithmetic
@dataclass
class Vec:
    x: float
    y: float

v = Vec(3.0, 4.0)
print(v.x + v.y)
