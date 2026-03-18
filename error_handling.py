class _Ok:
    __slots__ = ('value',)
    def __init__(self, value):
        self.value = value
    def is_ok(self): return True
    def is_err(self): return False
    def unwrap(self): return self.value
    def __repr__(self): return f"Ok({self.value!r})"

class _Err:
    __slots__ = ('error',)
    def __init__(self, error):
        self.error = error
    def is_ok(self): return False
    def is_err(self): return True
    def unwrap(self): raise ValueError(f"called unwrap on Err: {self.error}")
    def __repr__(self): return f"Err({self.error!r})"

def Ok(value): return _Ok(value)
def Err(error): return _Err(error)

def parse_int(s: str) -> Result[int]:
    if not s.isdigit():
        return Err(f"not a number: {s}")
    n = int(s)
    if (n < 0):
        return Err(f"negative: {n}")
    return Ok(n)

def connect(host: str):
    if (host == "bad-host"):
        raise ConnectionError(f"cannot reach {host}")
    print(f"Connected to {host}")

_result = parse_int("8080")
port = _result.value if _result.is_ok() else 80
print(f"Port: {port}")
_result = parse_int("abc")
if _result.is_err():
    err = _result.error
    print(f"Bad input: {err}")
    age = 0
else:
    age = _result.value
print(f"Age: {age}")
inputs = ["10", "abc", "20", "xyz", "30"]
values: list = []
for s in inputs:
    _result = parse_int(s)
    if _result.is_err():
        err = _result.error
        print(f"Skipping bad input: {s} ({err})")
        continue
    else:
        n = _result.value
    values.append(n)
print(f"Valid values: {values}")
try:
    connect("localhost")
    connect("bad-host")
except ConnectionError as err:
    print(f"Connection failed: {err}")
