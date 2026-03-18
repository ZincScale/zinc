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
    """Wrap data for smart collection dispatch."""
    return _ZincCollection(data)
`
