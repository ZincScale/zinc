x = 5
print("pos" if x > 0 else "neg")
y = -3
print("pos" if y > 0 else "neg")
a = 10
b = 20
print(a if a > b else b)
n = 7
label = "even" if n % 2 == 0 else "odd"
print(label)
# ternary with truthiness on a container
xs = []
print("empty" if not xs else "full")
# nested ternary
score = 85
grade = "A" if score >= 90 else "B" if score >= 80 else "C"
print(grade)
# ternary as comprehension output
nums = [1, 2, 3, 4, 5]
parity = [("even" if v % 2 == 0 else "odd") for v in nums]
print(parity)
# ternary result in arithmetic
m = (a if a > b else b) + 5
print(m)
