ages = {"alice": 30, "bob": 25}
print(ages["alice"])
print(ages["bob"])
print(len(ages))

ages["carol"] = 40
print(ages["carol"])
print(len(ages))

ages["alice"] = 31
print(ages["alice"])

counts = {"x": 1, "y": 5}
counts["x"] = counts["x"] + 1
print(counts["x"])
print(counts["y"])
print(len(counts))


def lookup(d: dict[str, int], k: str) -> int:
    return d[k]


print(lookup(ages, "bob"))
print(lookup(ages, "carol"))
