def greet(name: str, greeting: str = "Hello", punct: str = "!") -> str:
    return greeting + ", " + name + punct

print(greet("Ada"))
print(greet("Bob", "Hi"))
print(greet("Carol", "Hey", "?"))
print(greet("Dave", punct="."))
print(greet("Eve", greeting="Yo", punct="~"))
print(greet(name="Frank", greeting="Hola"))

def power(base: int, exp: int = 2) -> int:
    result = 1
    for _ in range(exp):
        result = result * base
    return result

print(power(5))
print(power(2, 10))
print(power(exp=3, base=2))

class Box:
    def __init__(self, width: int = 1, height: int = 1):
        self.width = width
        self.height = height
    def area(self) -> int:
        return self.width * self.height

print(Box().area())
print(Box(3).area())
print(Box(3, 4).area())
print(Box(height=5).area())
print(Box(width=2, height=3).area())
