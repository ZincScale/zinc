# sorted(xs, key=<builtin>): a bare builtin (len/str/abs/int/float) used as a
# sort key is turned into the lambda it stands for.
words = ["ccc", "a", "bb", "dddd", "ee"]
print(sorted(words, key=len))

nums = ["10", "2", "33", "4", "100"]
print(sorted(nums, key=int))

vals = [-5, 3, -1, 4, -2, 0]
print(sorted(vals, key=abs))

floats = ["1.5", "0.25", "3.0"]
print(sorted(floats, key=float))

# str key sorts by string representation
mixed = [22, 3, 111, 4]
print(sorted(mixed, key=str))

# an explicit lambda key still works (and matches the bare-builtin form)
print(sorted(words, key=lambda w: len(w)))

# plain sorted (no key) still works
print(sorted(vals))
print(sorted(words))
