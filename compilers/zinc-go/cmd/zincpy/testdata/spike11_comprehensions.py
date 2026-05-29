squares = [x * x for x in range(6)]
print(squares[0])
print(squares[5])
print(len(squares))

evens = [n for n in range(10) if n % 2 == 0]
print(len(evens))
print(evens[0])
print(evens[4])

nums = [1, 2, 3, 4, 5]
doubled = [v * 2 for v in nums]
print(doubled[0])
print(doubled[4])

total = 0
for s in [x * x for x in range(5)]:
    total = total + s
print(total)


def sum_of(xs: list[int]) -> int:
    s = 0
    for x in xs:
        s = s + x
    return s


print(sum_of([n * n for n in range(1, 6)]))

big = [x for x in range(20) if x > 15]
print(len(big))
print(big[0])
print(big[3])
