# Nested-list element typing: an unannotated list-of-lists literal now types as
# [][]int (not []interface{}), so the inner index works and elements are usable
# as their real type.
m = [[1, 2], [3, 4], [5, 6]]
print(m[2][0])
print(m[0][1] + m[1][1])

# arithmetic over a nested grid
total: int = 0
for row in m:
    for v in row:
        total = total + v
print(total)

# strings nested
grid = [["a", "b"], ["c", "d"]]
print(grid[1][0])
print(grid[0][1] + grid[1][1])

# three levels deep
cube = [[[1, 2]], [[3, 4]]]
print(cube[1][0][1])

# annotated form keeps working too
am: list[list[int]] = [[7, 8], [9, 10]]
print(am[1][0])
