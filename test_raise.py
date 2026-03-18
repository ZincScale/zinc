try:
    raise ValueError("test") from RuntimeError("cause")
except ValueError as err:
    print("caught")
