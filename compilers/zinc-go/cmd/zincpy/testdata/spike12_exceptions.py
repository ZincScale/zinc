def checked(n: int) -> int:
    if n < 0:
        raise ValueError("negative input")
    return n * 2


ok = 0
try:
    ok = checked(5)
except:
    ok = -1
print(ok)

bad = 0
try:
    bad = checked(-2)
except:
    bad = -1
print(bad)

try:
    checked(-7)
except Exception as e:
    print(e)
finally:
    print("done")

steps = 0
try:
    steps = 1
    checked(-1)
    steps = 99
except:
    steps = steps + 10
finally:
    steps = steps + 100
print(steps)


def safe_div(a: int, b: int) -> int:
    try:
        return a // b
    except:
        return -1


print(safe_div(20, 4))
print(safe_div(7, 0))


def describe(n: int) -> str:
    try:
        checked(n)
        return "ok"
    except:
        return "failed"
    finally:
        print("checked " + str(n))


print(describe(3))
print(describe(-5))
