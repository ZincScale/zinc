# String methods called on a dynamic (unannotated / duck-typed) receiver route
# to a runtime dispatcher that asserts the value is really a string.

def shout(s) -> str:
    return s.upper() + "!"

def quiet(s) -> str:
    return s.lower()

def words(s):
    return s.split()

def clean(s):
    return s.strip().lower()

def starts(s, p) -> bool:
    return s.startswith(p)

def ends(s, p) -> bool:
    return s.endswith(p)

def where(s, sub) -> int:
    return s.find(sub)

def howmany(s, sub) -> int:
    return s.count(sub)

def swap(s):
    return s.replace("o", "0")

def joiner(sep, parts):
    return sep.join(parts)

print(shout("hi"))
print(quiet("HeLLo"))
print(words("a b c"))
print(clean("  HeLLo  "))
print(starts("hello", "he"))
print(ends("hello", "lo"))
print(where("hello", "ll"))
print(howmany("banana", "a"))
print(swap("foobar"))
print(joiner("-", ["a", "b", "c"]))

# Chained: split() returns a dynamic list, iterate it.
for w in words("one two three"):
    print(w.upper())

# AttributeError when the dynamic value is not a string (catchable).
def show(x):
    try:
        print(x.upper())
    except AttributeError:
        print("no-upper")

show("works")
show(42)
