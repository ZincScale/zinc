import json
import math

# index into a parsed JSON list
nums = json.loads('[10, 20, 30, 40]')
print(nums[0])
print(nums[2])
print(nums[-1])          # negative index
print(len(nums))

# iterate a parsed list
total = 0
for n in nums:
    total = total + int(n)
print(total)

# index into a parsed JSON dict
person = json.loads('{"name": "Ada", "age": 36}')
print(person["name"])
print(person["age"])

# nested access: list of dicts
records = json.loads('[{"id": 1}, {"id": 2}, {"id": 3}]')
print(records[1]["id"])
print(len(records))
for r in records:
    print(r["id"])

# cast a dynamic scalar, then do native arithmetic on it
root = math.sqrt(2)
print(float(root) + 1.0)

# str() of a dynamic value
print("name is " + str(person["name"]))

print("done")
