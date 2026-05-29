type = "admin"
print(type)
func = 10
var = 20
print(func + var)

def select(type: str, default: int) -> int:
    if type == "admin":
        return 100
    return default

print(select("admin", 0))
print(select("user", 5))

# range/map builtins must still work (they're excluded from renaming)
for i in range(3):
    print(i)
print(list(map(lambda x: x + 1, [1, 2, 3])))

# keyword name as a loop var
for type in ["a", "b"]:
    print(type)
