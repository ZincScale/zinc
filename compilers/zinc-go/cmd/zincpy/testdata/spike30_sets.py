# set literal + membership + len
s = {1, 2, 3, 2, 1}
print(len(s))
print(2 in s)
print(5 in s)
print(5 not in s)
print(sorted(s))

# set() from a list (dedup)
xs = [3, 1, 2, 3, 1, 4]
u = set(xs)
print(len(u))
print(sorted(u))

# add / discard / remove
s.add(4)
s.add(2)
print(len(s))
print(4 in s)
s.discard(1)
print(1 in s)
s.discard(99)
s.remove(2)
print(sorted(s))

# string set + membership
chars = set("hello")
print(len(chars))
print("l" in chars)
print("z" in chars)
print(sorted(chars))

# empty set
e = set()
print(len(e))
if not e:
    print("empty set is falsy")

print("done")
