import sys

def greet(name: str) -> str:
    return f"Hello, {name}!"

name = sys.argv[1] if (len(sys.argv) > 1) else "world"
print(greet(name))
numbers = [1, 2, 3, 4, 5]
for i, n in enumerate(numbers):
    if (n > 3):
        print(f"{i}: {n} is big")
