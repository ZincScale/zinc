def describe(x: any):
    if isinstance(x, str):
        print(f"string: {x}")
    elif isinstance(x, int):
        print(f"integer: {x}")
    elif isinstance(x, list):
        print(f"list with {len(x)} items")
    else:
        print(f"something else: {x}")

name: str = "Alice"
age: int = 30
scores: list[int] = [95, 87, 92]
config: dict = {"debug": True}
describe("hello")
describe(42)
describe([1, 2, 3])
describe(3.14)
value = None
if (value is None):
    print("value is none")
value = "found"
if (value is not None):
    print(f"value: {value}")
allowed = ["admin", "editor", "viewer"]
role = "guest"
if (role not in allowed):
    print(f"{role} is not allowed")
