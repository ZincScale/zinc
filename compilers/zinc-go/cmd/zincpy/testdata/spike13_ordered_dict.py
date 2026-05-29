scores = {"alice": 30, "bob": 25, "carol": 40}

for name in scores:
    print(name)

total = 0
for name in scores:
    total = total + scores[name]
print(total)

scores["dave"] = 10
for name in scores:
    print(name)
    print(scores[name])

print(scores)
print(len(scores))

for k in scores.keys():
    print(k)


def build(n: int) -> dict[int, int]:
    d = {}
    d[1] = 10
    d[2] = 20
    d[3] = 30
    return d


squares = build(3)
for k in squares:
    print(k)
    print(squares[k])
print(squares)
