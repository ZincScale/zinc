xs = [10, 20, 30, 40, 50]
print(xs[0])
print(xs[2])
i = -1
print(xs[i])
print(xs[-2])
n = len(xs)
print(xs[n - 1])
# negative computed index in a loop (e.g. reverse access)
for k in range(1, 4):
    print(xs[-k])
# xs[i-1] where i can be 0 → -1 wraps
for i in range(3):
    print(xs[i - 1])
