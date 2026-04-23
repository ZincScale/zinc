"""Go stdlib/third-party type introspection via `go doc`.

Zinc source doesn't expose `*` or `&`, but transpiled Go needs to know
which imported types are structs (take `*T` in idiomatic Go) vs
interfaces (bare `T`). zinc-go's original transpiler used Go's native
`go/types` library. Python has no equivalent, so we shell out to
`go doc <pkg>` and parse the output.

Cache per (pkg, type) to avoid repeated process spawns. First lookup
in a package is ~100ms; subsequent ones are free.
"""
from __future__ import annotations

import re
import subprocess
from functools import lru_cache


# `type <Name> struct {` — matches when the named type is declared as a struct.
_STRUCT_RE = re.compile(r"^type\s+(\w+)\s+struct\b", re.MULTILINE)
# `type <Name> interface {`
_INTERFACE_RE = re.compile(r"^type\s+(\w+)\s+interface\b", re.MULTILINE)
# `func (r *Name) Method(...)` — pointer receiver method on the type.
_POINTER_RECV_RE = re.compile(r"^func\s+\([^)]*\*(\w+)\)", re.MULTILINE)


@lru_cache(maxsize=256)
def _go_doc_all(pkg: str) -> str:
    """Return `go doc -all <pkg>` output. Empty string if the package can't
    be resolved (unknown dep, no Go toolchain, etc.)."""
    try:
        result = subprocess.run(
            ["go", "doc", "-all", pkg],
            capture_output=True, text=True, timeout=10,
        )
        return result.stdout if result.returncode == 0 else ""
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return ""


def is_struct(pkg: str, name: str) -> bool:
    doc = _go_doc_all(pkg)
    return any(m.group(1) == name for m in _STRUCT_RE.finditer(doc))


def is_interface(pkg: str, name: str) -> bool:
    doc = _go_doc_all(pkg)
    return any(m.group(1) == name for m in _INTERFACE_RE.finditer(doc))


def has_pointer_receivers(pkg: str, name: str) -> bool:
    doc = _go_doc_all(pkg)
    return any(m.group(1) == name for m in _POINTER_RECV_RE.finditer(doc))


def needs_pointer(pkg: str, name: str) -> bool:
    """True when Go-idiomatic usage of pkg.Name takes a pointer. A struct
    with pointer-receiver methods almost always wants `*pkg.Name` in function
    parameters and return types (e.g. `*http.Request`, `*sql.DB`).
    Interfaces never take pointers. Structs without pointer-receiver methods
    (value types like `time.Duration`) stay bare."""
    if is_interface(pkg, name):
        return False
    return is_struct(pkg, name) and has_pointer_receivers(pkg, name)
