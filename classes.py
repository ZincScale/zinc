import dataclasses
import enum

class Animal:
    def __init__(self, name: str, sound: str):
        self.name = name
        self.sound = sound

    def speak(self) -> str:
        return f"{self.name} says {self.sound}"

    def __str__(self) -> str:
        return f"Animal({self.name})"

class Dog(Animal):
    def __init__(self, breed: str, **kwargs):
        super().__init__(**kwargs)
        self.breed = breed

    def fetch(self) -> str:
        return f"{self.name} fetches the ball!"

@dataclasses.dataclass
class Point:
    x: float
    y: float

def distance(a: Point, b: Point) -> float:
    return ((((b.x - a.x) ** 2) + ((b.y - a.y) ** 2)) ** 0.5)

class Direction(enum.Enum):
    North = 1
    South = 2
    East = 3
    West = 4

dog = Dog(breed="Lab", name="Rex", sound="Woof")
print(dog.speak())
print(dog.fetch())
p1 = Point(0.0, 0.0)
p2 = Point(3.0, 4.0)
print(f"Distance: {distance(p1, p2)}")
