d = {"a": 1, "b": 2, "c": 3}
for k, v in d.items():
    print(k, v)

# enumerate
for i, c in enumerate("abc"):
    print(i, c)

xs = [10, 20, 30]
for i, v in enumerate(xs):
    print(i, v)

# zip
names = ["alice", "bob", "carol"]
ages = [30, 25, 35]
for name, age in zip(names, ages):
    print(name, age)

# sum over dict values
print(sum(d.values()))

# list of tuples unpack
pairs = [(1, "one"), (2, "two")]
for num, word in pairs:
    print(num, word)
