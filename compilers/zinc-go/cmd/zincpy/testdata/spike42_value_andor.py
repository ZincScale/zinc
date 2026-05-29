# value-returning or (common default idiom)
name = ""
display = name or "anonymous"
print(display)
name2 = "Ada"
print(name2 or "anonymous")

# value-returning and
a = 5
b = 10
print(a and b)        # → 10 (both truthy → last)
z = 0
print(z and b)        # → 0 (first falsy → first)

# or with numbers
x = 0
print(x or 42)
y = 7
print(y or 42)

# chained or
print("" or "" or "third")

# still works in boolean context
if a > 0 and b > 0:
    print("both positive")
if name or name2:
    print("at least one name")

# or for a fallback in a function
def greet(who: str) -> str:
    return (who or "world")

print(greet(""))
print(greet("Bob"))
