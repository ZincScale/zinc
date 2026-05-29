import math
import json
import statistics

# module function calls returning float / int
print(math.sqrt(2))
print(math.factorial(20))
print(math.gcd(48, 36))

# module attribute read
print(math.pi)

# nested FFI calls: result of one call feeds another
print(math.sqrt(math.factorial(5)))

# pass a native list across the FFI boundary; get a string back
print(json.dumps([1, "two", 3.5, True]))

# pass a dict literal across the boundary
print(json.dumps({"name": "Ada", "age": 36}))

# round-trip: parse JSON back into a native value and print it
print(json.loads('[10, 20, 30]'))

# a pure-Python stdlib module
print(statistics.median([5, 3, 8, 1, 9]))

# a Python exception raised across the FFI boundary is catchable
try:
    math.sqrt(-1)
except ValueError as e:
    print("caught:", e)

print("done")
