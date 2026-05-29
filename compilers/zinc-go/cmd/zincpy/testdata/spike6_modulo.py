print(7 % 3)
print(-7 % 3)
print(7 % -3)
print(-7 % -3)
print(6 % 3)
print(-6 % 3)
print(10 % 4)
print(-10 % 4)
print(10 % -4)


def wrap(i: int, n: int) -> int:
    return i % n


print(wrap(-1, 5))
print(wrap(7, 5))
print(wrap(-13, 5))

for k in range(-3, 4):
    print(k % 3)
