# empty vs non-empty containers
e = []
if e:
    print("ne")
else:
    print("empty list is falsy")

xs = [1, 2]
if xs:
    print("non-empty list is truthy")

# strings
s = ""
if not s:
    print("empty string is falsy")
if "hi":
    print("non-empty string is truthy")

# numbers
n = 0
if n:
    print("nonzero")
else:
    print("zero is falsy")
if 5:
    print("five is truthy")

# and / or in boolean context with non-bool operands
if "hi" and [1]:
    print("both truthy")
if [] or "fallback":
    print("or short-circuits to truthy")

# filtering with truthiness
vals = [0, 1, 2, 0, 3]
kept = []
for v in vals:
    if v:
        kept.append(v)
print(kept)

# while with a count (truthy until 0)
count = 3
while count:
    print(count)
    count = count - 1

# not on an empty container
if not []:
    print("not [] is true")

print("done")
