# range(...) used as a VALUE (not just a for-loop iterable) materializes to a
# list of ints, so it works as an argument to list/map/filter/sum/sorted/etc.
print(list(range(5)))
print(list(range(2, 8)))
print(list(range(0, 20, 3)))
print(list(range(10, 0, -2)))
print(list(range(5, 5)))

print(list(filter(lambda x: x % 2 == 0, range(10))))
print(list(map(lambda x: x * x, range(6))))
print(sum(range(11)))
print(max(range(3, 9)))
print(min(range(3, 9)))

# 3-arg range in a for-loop iterates the materialized slice
acc: int = 0
for i in range(0, 10, 2):
    acc = acc + i
print(acc)

# negative step in a for-loop
seq: list[int] = []
for i in range(10, 0, -3):
    seq.append(i)
print(seq)

# 1-arg / 2-arg for-loops keep the optimized numeric range (same output)
s: int = 0
for i in range(5):
    s = s + i
print(s)

# range bound to a variable, then iterated
r = range(4)
out: list[int] = []
for i in r:
    out.append(i)
print(out)
