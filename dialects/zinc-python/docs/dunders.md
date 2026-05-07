# Method name mappings

`zinc-python` rewrites a small set of friendly method names to their Python `__dunder__` equivalents. Anything not in this table passes through unchanged — write `__add__`, `__len__`, etc. directly when you need them.

| Zinc | Python | Purpose |
|------|--------|---------|
| `init` | `__init__` | Constructor |
| `toString` | `__repr__` | Developer-facing string |
| `toStr` | `__str__` | User-facing string |
| `equals` | `__eq__` | `==` operator |
| `notEquals` | `__ne__` | `!=` operator |
| `hashCode` | `__hash__` | Hashing for set/dict keys |
| `getItem` | `__getitem__` | `obj[key]` |
| `setItem` | `__setitem__` | `obj[key] = value` |
| `delItem` | `__delitem__` | `del obj[key]` |
| `enter` | `__enter__` | `with` block entry |
| `exit` | `__exit__` | `with` block exit |

## Example

```zinc
class Point {
    def init(x, y) {
        self.x = x
        self.y = y
    }

    def toString() {
        return "Point({self.x}, {self.y})"
    }

    def equals(other) {
        return self.x == other.x and self.y == other.y
    }

    def hashCode() {
        return hash((self.x, self.y))
    }

    def getItem(index) {
        if index == 0 { return self.x }
        if index == 1 { return self.y }
        raise IndexError("index out of range")
    }
}

def main() {
    p1 = Point(1, 2)
    p2 = Point(1, 2)
    print(p1)            # Point(1, 2)        — toString
    print(p1 == p2)      # True               — equals
    print(p1[0])         # 1                  — getItem
    print({p1: "origin"})    # hashable      — hashCode
}
```

## Anything else

Names not in the table pass through unchanged. If you need a less common dunder, just write it directly:

```zinc
class Vec {
    def init(x, y) {
        self.x = x
        self.y = y
    }

    // Not in the table — written as the Python name directly.
    def __add__(other) {
        return Vec(self.x + other.x, self.y + other.y)
    }

    def __len__() {
        return 2
    }
}
```
