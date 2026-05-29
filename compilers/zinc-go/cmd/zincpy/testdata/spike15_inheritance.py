class Animal:
    def __init__(self, name: str, sound: str):
        self.name = name
        self.sound = sound

    def speak(self) -> str:
        return self.name + " says " + self.sound

    def __str__(self) -> str:
        return "Animal(" + self.name + ")"


class Dog(Animal):
    def __init__(self, name: str):
        super().__init__(name, "Woof")
        self.tricks = 0

    def learn(self) -> int:
        self.tricks = self.tricks + 1
        return self.tricks

    def __str__(self) -> str:
        return "Dog(" + self.name + ", " + self.sound + ")"


a = Animal("Cat", "Meow")
print(a.speak())
print(a)

d = Dog("Rex")
print(d.speak())
print(d.learn())
print(d.learn())
print(d)
print(d.name)
print(d.sound)
print(d.tricks)
