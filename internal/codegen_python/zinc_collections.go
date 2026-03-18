// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package codegen_python

// ZincCollectionsRuntime is the smart dispatch runtime for collection operations.
// Auto-detects available libraries (polars, numpy) and dispatches to the fastest
// available backend. Falls back to pure Python comprehensions when no deps installed.
const ZincCollectionsRuntime = `# Zinc smart collection dispatch — auto-detects best backend
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

# Auto-install and use best available backend
import subprocess as _sp

def _auto_install(pkg):
    """Install a package if not available."""
    try:
        _sp.check_call([sys.executable, '-m', 'pip', 'install', '-q', pkg],
                       stdout=_sp.DEVNULL, stderr=_sp.DEVNULL)
        return True
    except Exception:
        return False

# Try Polars for structured data — auto-install if needed
try:
    import polars as pl
    _POLARS = True
except ImportError:
    if _auto_install('polars'):
        import polars as pl
        _POLARS = True
    else:
        _POLARS = False

# Try NumPy for numeric data — auto-install if needed
try:
    import numpy as np
    _NUMPY = True
except ImportError:
    if _auto_install('numpy'):
        import numpy as np
        _NUMPY = True
    else:
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
`
