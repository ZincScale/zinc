nums = [1, 2, 3, 4, 5]
print(len(nums))
print(nums[0])
print(nums[4])

total = 0
for n in nums:
    total = total + n
print(total)

nums.append(6)
print(len(nums))
print(nums[5])

squares = [0, 0, 0, 0, 0]
for i in range(5):
    squares[i] = i * i
print(squares[0])
print(squares[4])
print(len(squares))


def total_of(xs: list[int]) -> int:
    s = 0
    for x in xs:
        s = s + x
    return s


print(total_of(nums))
print(total_of([10, 20, 30]))

xs = [1, 2, 3]
xs[1] = 99
print(xs[1])
print(xs[0] + xs[2])
