# lambda assigned to a var and called
inc = lambda x: x + 1
print(inc(5))
add = lambda a, b: a + b
print(add(3, 4))
always = lambda: 42
print(always())

# sorted with a key lambda
nums = [3, 1, 4, 1, 5, 9, 2, 6]
print(sorted(nums, key=lambda x: -x))
words = ["ccc", "a", "bb", "dddd"]
print(sorted(words, key=lambda w: len(w)))
pairs = [(1, "z"), (2, "a"), (3, "m")]
print(sorted(pairs, key=lambda p: p[1]))

# map / filter
print(list(map(lambda x: x * x, [1, 2, 3, 4])))
print(list(filter(lambda x: x % 2 == 0, [1, 2, 3, 4, 5, 6])))

# map/filter results iterated
for v in map(lambda x: x * 10, [1, 2, 3]):
    print(v)
