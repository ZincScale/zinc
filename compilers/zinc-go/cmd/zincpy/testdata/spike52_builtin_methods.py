# print(sep=/end=)
print("a", "b", "c", sep="-")
print("no newline", end="")
print()
print(1, 2, 3, sep=", ", end="!\n")

# dict get/setdefault/pop/update + sorted(items()) (tuple comparison)
d = {"a": 1, "b": 2}
print(d.get("a"))
print(d.get("z", 99))
d.setdefault("c", 3)
print(d.pop("a"))
print(d.pop("zz", -1))
d.update({"x": 10})
print(sorted(d.items()))

# enumerate with start (keyword and positional)
for i, c in enumerate("xy", start=1):
    print(i, c)
for i, c in enumerate("xy", 10):
    print(i, c)

# str methods: title/capitalize/just/center/lstrip/rstrip/split(maxsplit)
s = "hello, world"
print(s.title())
print(s.capitalize())
print("5".rjust(3, "0"))
print("x".ljust(4, "-"))
print("hi".center(7, "*"))
print("  pad  ".rstrip() + "|")
print("|" + "  pad  ".lstrip())
print("a,b,c,d".split(",", 2))


# str methods on a dynamic receiver, chained
def shout(w):
    return w.upper().center(9, "=")


print(shout("hi"))
