class Animal:
    def __init__(self, name: str):
        self.name = name

    def kind(self) -> str:
        return "animal"


class Dog(Animal):
    def __init__(self, name: str):
        super().__init__(name)

    def kind(self) -> str:
        return "dog"


class Puppy(Dog):
    def __init__(self, name: str):
        super().__init__(name)


class Cat(Animal):
    def __init__(self, name: str):
        super().__init__(name)


d = Dog("Rex")
p = Puppy("Bits")
c = Cat("Tom")

# User classes: exact, parent, grandparent, and negative checks.
print(isinstance(d, Dog))
print(isinstance(d, Animal))
print(isinstance(d, Cat))
print(isinstance(p, Puppy))
print(isinstance(p, Dog))
print(isinstance(p, Animal))
print(isinstance(c, Animal))
print(isinstance(c, Dog))
print(isinstance(d, object))

# Builtin types.
print(isinstance(42, int))
print(isinstance(42, float))
print(isinstance(3.5, float))
print(isinstance("hi", str))
print(isinstance([1, 2, 3], list))
print(isinstance((1, 2), tuple))
print(isinstance({"a": 1}, dict))

# Python: bool is a subclass of int, but an int is not a bool.
print(isinstance(True, bool))
print(isinstance(True, int))
print(isinstance(1, bool))

# Tuple-of-types form (matches any).
print(isinstance(42, (int, float)))
print(isinstance(3.5, (int, float)))
print(isinstance("x", (int, float)))
print(isinstance(d, (Cat, Dog)))
print(isinstance(c, (Cat, Dog)))

# In guards / boolean operators (result is a real bool).
def label(n: int) -> str:
    if not isinstance(n, int):
        return "not-int"
    return "int=" + str(n)

print(label(7))
print(isinstance(5, int) and isinstance("a", str))
print(isinstance(5, str) or isinstance(5, int))
