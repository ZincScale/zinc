# list methods: sort / sort(reverse=True) / reverse / insert / index / count.
# Mutators return None and modify in place (lowered to xs = helper(xs, ...)).
xs: list[int] = [3, 1, 4, 1, 5, 9, 2, 6]
xs.sort()
print(xs)
xs.sort(reverse=True)
print(xs)
xs.reverse()
print(xs)

xs.insert(0, 100)
xs.insert(2, 200)
xs.insert(-1, 300)
print(xs)

print(xs.index(200))
print(xs.count(1))

names: list[str] = ["bob", "amy", "cara", "amy"]
names.sort()
print(names)
print(names.index("bob"))
print(names.count("amy"))

fs: list[float] = [2.5, 1.5, 3.5, 0.5]
fs.sort()
print(fs)
fs.sort(reverse=True)
print(fs)

# index() raises ValueError (with CPython's fixed message) when absent
miss: list[int] = [1, 2, 3]
try:
    print(miss.index(99))
except ValueError as e:
    print("err:", str(e))
