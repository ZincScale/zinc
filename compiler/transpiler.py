# Copyright 2026 ZincScale
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
Zinc transpiler — converts .zn (braces Python) to standard .py

Four transforms:
1. Braces → indentation (tracks brace depth, emits proper Python indentation)
2. Method renames — sane names → dunders (init → __init__, toString → __repr__, etc.)
3. Implicit self — injects `self` as first param in class/instance methods
4. All strings → f-strings (prefix all string literals with f)
"""

import re

# Sane name → Python dunder mapping
# Only unambiguous renames — names that are clearly dunder-intent.
# Ambiguous names (add, len, iter, next, contains, etc.) are left as-is;
# use __add__ explicitly if you want the operator dunder.
METHOD_MAP = {
    "init": "__init__",
    "toString": "__repr__",
    "toStr": "__str__",
    "equals": "__eq__",
    "notEquals": "__ne__",
    "hashCode": "__hash__",
    "getItem": "__getitem__",
    "setItem": "__setitem__",
    "delItem": "__delitem__",
    "enter": "__enter__",
    "exit": "__exit__",
}

# Regex to match `def methodname(` in a line
DEF_PATTERN = re.compile(r"^(\s*)def\s+(\w+)\s*\(")
# Regex to match string literals (handles single, double, triple quotes)
STRING_PATTERN = re.compile(
    r'''(?<!f)("""[\s\S]*?"""|\'\'\'[\s\S]*?\'\'\'|"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*')'''
)


def transpile(source: str, filename: str = "<stdin>", entry_point: bool = False) -> str:
    """Transpile a .zn source string to Python source.

    If entry_point=True and the file defines main(), appends
    if __name__ == "__main__": main() automatically.
    """
    lines = source.split("\n")
    result = _braces_to_indent(lines)
    result = _strip_name_guard(result)
    result = _rename_methods(result)
    result = _inject_self(result)
    result = _fstrings(result)

    if entry_point and _has_main(result):
        while result and result[-1].strip() == "":
            result.pop()
        result.append("")
        result.append('if __name__ == "__main__":')
        result.append("    main()")

    return "\n".join(result)


def _strip_name_guard(lines: list[str]) -> list[str]:
    """Remove if __name__ == '__main__' blocks — zinc handles entry points."""
    out = []
    skip_depth = 0
    skipping = False

    for line in lines:
        stripped = line.lstrip()
        indent = len(line) - len(stripped)

        if not skipping:
            if stripped.startswith('if __name__') and '__main__' in stripped:
                skipping = True
                skip_indent = indent
                continue
            out.append(line)
        else:
            # Inside the guard block — skip indented lines
            if stripped == "" or indent > skip_indent:
                continue
            # Back to same/lower indent — stop skipping
            skipping = False
            out.append(line)

    return out


def _has_main(lines: list[str]) -> bool:
    """Check if the file defines a top-level main() function."""
    for line in lines:
        if line.startswith("def main("):
            return True
    return False


_BLOCK_KEYWORDS = {"if", "else", "elif", "for", "while", "def", "class",
                    "try", "except", "finally", "with", "elif"}


def _is_block_open(stripped: str) -> bool:
    """Check if a line ending with { is a block opening (not a dict/set literal).
    Block openings follow keywords: if x {, def foo() {, class Bar {, etc."""
    content = stripped[:-1].rstrip()
    if not content:
        return True  # bare { on a line — treat as block (rare)
    # First word of the line (after } prefix removal) must be a block keyword
    first_word = content.split()[0].lstrip("}")
    if first_word in _BLOCK_KEYWORDS:
        return True
    # Also handle: } else {, } except {, etc.
    if content.startswith("}"):
        rest = content[1:].strip()
        if rest:
            first_word = rest.split()[0]
            return first_word in _BLOCK_KEYWORDS
    return False


def _braces_to_indent(lines: list[str]) -> list[str]:
    """Convert brace-delimited blocks to Python indentation.

    Tracks a stack to distinguish block braces (if/def/class/etc.) from
    literal braces (dict/set). Block braces become indentation; literal
    braces pass through unchanged.
    """
    out = []
    indent = 0
    # Stack: True = block brace (converted to indent), False = literal brace (pass through)
    brace_stack = []

    for raw_line in lines:
        stripped = raw_line.strip()

        # Skip empty lines
        if not stripped:
            out.append("")
            continue

        # `else if` → `elif`
        if stripped.startswith("} else if ") or stripped.startswith("else if "):
            stripped = stripped.replace("else if ", "elif ", 1)

        # Line is just a closing brace
        if stripped == "}":
            if brace_stack and brace_stack[-1]:
                # Block brace — decrease indent, consume the line
                brace_stack.pop()
                indent = max(0, indent - 1)
                continue
            else:
                # Literal brace — pass through
                if brace_stack:
                    brace_stack.pop()
                out.append("    " * indent + stripped)
                continue

        # Closing brace followed by continuation (} else {, } except {, etc.)
        if stripped.startswith("}"):
            if brace_stack and brace_stack[-1]:
                brace_stack.pop()
                indent = max(0, indent - 1)
            rest = stripped[1:].strip()
            if rest:
                stripped = rest
            else:
                continue

        # Line ends with opening brace
        if stripped.endswith("{"):
            if _is_block_open(stripped):
                # Block brace — convert to colon + indent
                brace_stack.append(True)
                content = stripped[:-1].rstrip()
                if content:
                    out.append("    " * indent + content + ":")
                indent += 1
                continue
            else:
                # Literal brace (dict/set) — pass through
                brace_stack.append(False)
                out.append("    " * indent + stripped)
                continue

        # Regular line — emit with current indent
        out.append("    " * indent + stripped)

    return out


def _rename_methods(lines: list[str]) -> list[str]:
    """Rename sane method names to Python dunders."""
    out = []
    for line in lines:
        m = DEF_PATTERN.match(line)
        if m:
            name = m.group(2)
            if name in METHOD_MAP:
                line = line.replace(f"def {name}(", f"def {METHOD_MAP[name]}(", 1)
        out.append(line)
    return out


def _inject_self(lines: list[str]) -> list[str]:
    """Inject `self` as first param in class instance methods."""
    out = []
    in_class = False
    class_indent = 0

    for line in lines:
        stripped = line.lstrip()
        current_indent = len(line) - len(stripped)

        # Track class scope
        if stripped.startswith("class ") and stripped.endswith(":"):
            in_class = True
            class_indent = current_indent
            out.append(line)
            continue

        # Exited class (line at same or less indent as class keyword)
        if in_class and stripped and current_indent <= class_indent and not stripped.startswith("class "):
            in_class = False

        # Inside a class, transform def statements
        if in_class and stripped.startswith("def ") and current_indent > class_indent:
            # Skip @staticmethod and @classmethod decorated methods
            if out and out[-1].strip().startswith("@staticmethod"):
                out.append(line)
                continue
            if out and out[-1].strip().startswith("@classmethod"):
                # classmethod gets cls, not self
                out.append(line)
                continue

            m = DEF_PATTERN.match(line)
            if m:
                ws = m.group(1)
                name = m.group(2)
                # Extract params after `def name(`
                after_paren = line[m.end():]
                # Inject self
                if after_paren.startswith(")"):
                    line = f"{ws}def {name}(self):{after_paren[2:]}" if after_paren.startswith("):") else f"{ws}def {name}(self){after_paren}"
                else:
                    line = f"{ws}def {name}(self, {after_paren}"

        out.append(line)
    return out


# Pattern to detect actual interpolation: {word_chars...} not {\n or {} or {special
_INTERP_PATTERN = re.compile(r"\{[a-zA-Z_]")


def _fstrings(lines: list[str]) -> list[str]:
    """Prefix all string literals with f to make them f-strings."""
    out = []
    for line in lines:
        # Don't touch lines that are comments or already have f-strings
        stripped = line.lstrip()
        if stripped.startswith("#"):
            out.append(line)
            continue

        # Only add f prefix to strings that contain {identifier...} interpolation
        # but NOT strings followed by .format( — those use explicit formatting
        def _maybe_fstring(m):
            if not _INTERP_PATTERN.search(m.group(0)):
                return m.group(0)
            # Check if followed by .format(
            after = line[m.end():]
            if after.lstrip().startswith(".format("):
                return m.group(0)
            return "f" + m.group(0)

        line = STRING_PATTERN.sub(_maybe_fstring, line)
        out.append(line)
    return out
