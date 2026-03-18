def fibonacci(limit: int):
    a = 0
    b = 1
    while (a < limit):
        yield a
        temp = a
        a = b
        b = (temp + b)

def chunk(items: list, size: int):
    i = 0
    while (i < len(items)):
        yield items[i:(i + size)]
        i = (i + size)

print("Fibonacci < 100:")
for n in fibonacci(100):
    print(f"  {n}")
print("Chunks of 3:")
data = [1, 2, 3, 4, 5, 6, 7, 8]
for c in chunk(data, 3):
    print(f"  {c}")
