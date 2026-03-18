# Zinc smart collection dispatch — auto-detects best backend
class _ZincCollection:
    """Wraps a list and dispatches .filter/.map/.sum/etc to the best available backend."""
    __slots__ = ('_data',)

    def __init__(self, data):
        self._data = list(data) if not isinstance(data, list) else data

    def filter(self, pred):
        return _ZincCollection([x for x in self._data if pred(x)])

    def map(self, fn):
        # Auto-parallelize on free-threaded Python for large collections
        if _FREE_THREADED and len(self._data) > 1000:
            from concurrent.futures import ThreadPoolExecutor
            with ThreadPoolExecutor() as pool:
                return _ZincCollection(list(pool.map(fn, self._data)))
        return _ZincCollection([fn(x) for x in self._data])

    def sum(self):
        return sum(self._data)

    def min(self):
        return min(self._data)

    def max(self):
        return max(self._data)

    def sort(self):
        return _ZincCollection(sorted(self._data))

    def sort_by(self, key, reverse=False):
        return _ZincCollection(sorted(self._data, key=key, reverse=reverse))

    def take(self, n):
        return _ZincCollection(self._data[:n])

    def skip(self, n):
        return _ZincCollection(self._data[n:])

    def first(self, pred=None):
        if pred is None:
            return self._data[0]
        return next(x for x in self._data if pred(x))

    def any(self, pred):
        return any(pred(x) for x in self._data)

    def all(self, pred):
        return all(pred(x) for x in self._data)

    def distinct(self):
        seen = set()
        result = []
        for x in self._data:
            key = id(x) if isinstance(x, dict) else x
            if key not in seen:
                seen.add(key)
                result.append(x)
        return _ZincCollection(result)

    def flat_map(self, fn):
        return _ZincCollection([item for x in self._data for item in fn(x)])

    def group_by(self, key_fn):
        groups = {}
        for item in self._data:
            k = key_fn(item)
            groups.setdefault(k, []).append(item)
        return groups

    def reduce(self, initial, fn):
        import functools
        return functools.reduce(fn, self._data, initial)

    def to_list(self):
        return self._data

    def to_dict(self):
        return dict(self._data)

    def __len__(self):
        return len(self._data)

    def __iter__(self):
        return iter(self._data)

    def __getitem__(self, key):
        result = self._data[key]
        if isinstance(result, list):
            return _ZincCollection(result)
        return result

    def __repr__(self):
        return repr(self._data)

# Detect free-threaded Python (3.13t+)
import sys
_FREE_THREADED = False
try:
    _FREE_THREADED = not sys._is_gil_enabled()
except AttributeError:
    pass  # Python < 3.13

# Try to enhance with Polars for structured data
try:
    import polars as pl
    _POLARS = True
except ImportError:
    _POLARS = False

# Try to enhance with NumPy for numeric data
try:
    import numpy as np
    _NUMPY = True
except ImportError:
    _NUMPY = False

def _zinc_collect(data):
    """Wrap data for smart collection dispatch.
    Auto-selects backend based on data shape:
    - list[dict] + Polars available → Polars dispatch
    - list[int/float] + NumPy available → NumPy dispatch
    - otherwise → pure Python comprehensions
    """
    if _POLARS and len(data) > 0 and isinstance(data[0], dict):
        return _ZincPolarsCollection(data)
    if _NUMPY and len(data) > 0 and isinstance(data[0], (int, float)):
        return _ZincNumpyCollection(data)
    return _ZincCollection(data)

class _ZincPolarsCollection(_ZincCollection):
    """Polars-backed collection for list[dict] data."""
    def filter(self, pred):
        # Fall back to Python filter — Polars needs column expressions
        return _ZincPolarsCollection([x for x in self._data if pred(x)])

    def map(self, fn):
        return _ZincPolarsCollection([fn(x) for x in self._data])

    def sum(self):
        if _POLARS and len(self._data) > 0 and isinstance(self._data[0], (int, float)):
            return pl.Series(self._data).sum()
        return sum(self._data)

    def sort_by(self, key, reverse=False):
        return _ZincPolarsCollection(sorted(self._data, key=key, reverse=reverse))

class _ZincNumpyCollection(_ZincCollection):
    """NumPy-backed collection for numeric data."""
    def __init__(self, data):
        super().__init__(data)
        self._arr = np.array(data)

    def filter(self, pred):
        mask = np.array([pred(x) for x in self._data])
        return _ZincNumpyCollection(self._arr[mask].tolist())

    def map(self, fn):
        return _ZincNumpyCollection([fn(x) for x in self._data])

    def sum(self):
        return self._arr.sum()

    def min(self):
        return self._arr.min()

    def max(self):
        return self._arr.max()

def apply(f, x: int) -> int:
    return f(x)

def make_adder(n: int):
    return lambda x: (x + n)

double = lambda x: (x * 2)
add = lambda a, b: (a + b)
print(f"double(5) = {double(5)}")
print(f"3 + 4 = {add(3, 4)}")
print(f"apply(double, 10) = {apply(double, 10)}")
add5 = make_adder(5)
add10 = make_adder(10)
print(f"add5(3) = {add5(3)}")
print(f"add10(3) = {add10(3)}")
numbers = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
evens = [x for x in numbers if ((x % 2) == 0)]
doubled = [(x * 2) for x in numbers]
total = _zinc_collect(numbers).filter(lambda x: (x > 5)).map(lambda x: (x * 10)).sum()
print(f"evens: {evens}")
print(f"doubled: {doubled}")
print(f"sum of >5 * 10: {total}")
names = ["Charlie", "Alice", "Bob"]
sorted_names = sorted(names, key=lambda x: len(x))
print(f"sorted by length: {sorted_names}")
