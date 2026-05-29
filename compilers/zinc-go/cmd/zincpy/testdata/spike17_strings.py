def describe(name: str, age: int, score: float) -> str:
    return f"{name} is {age} years old, score {score}"


name = "Ada"
age = 36
score = 9.5
ready = True

# f-strings: str / int / float / bool interpolation, and expressions
print(f"{name} is {age}")
print(f"score = {score}, ready = {ready}")
print(f"{age} + 1 = {age + 1}")
print(f"half of {age} is {age / 2}")
print(f"literal braces {{ and }} stay")
print(describe(name, age, score))

# !r conversion
print(f"name repr is {name!r}")

# str methods
greeting = "  Hello, World  "
print(greeting.strip())
print(greeting.strip().upper())
print(greeting.strip().lower())
print("hello".startswith("he"))
print("hello".endswith("lo"))
print("hello world".find("world"))
print("banana".count("a"))
print("a,b,c".replace(",", "-"))

# split / join
parts = "a,b,c".split(",")
print(len(parts))
for piece in parts:
    print(piece)
print("-".join(parts))
print("one two   three".split())

# multi-arg print: space-separated, mixed types
print(name, age, score, ready)
print(1, 2, 3)
print()
print("done")
