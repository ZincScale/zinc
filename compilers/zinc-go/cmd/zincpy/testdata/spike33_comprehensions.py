# dict comprehension
squares = {x: x * x for x in range(1, 6)}
print(squares[3])
print(len(squares))
print(sorted(squares.keys()))
print(sorted(squares.values()))

# dict comprehension with filter
evens = {x: x * 2 for x in range(10) if x % 2 == 0}
print(len(evens))
print(evens[4])

# set comprehension
s = {x % 3 for x in range(10)}
print(len(s))
print(sorted(s))

# set comprehension with filter
big = {x for x in [1, 5, 2, 8, 3, 9] if x > 4}
print(sorted(big))

# dict comp over items
prices = {"apple": 3, "banana": 1, "cherry": 5}
doubled = {k: v * 2 for k, v in prices.items()}
print(sorted(doubled.values()))

print("done")
