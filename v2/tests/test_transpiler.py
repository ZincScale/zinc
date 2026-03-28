"""Unit tests for the zinc transpiler."""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))
from transpiler import transpile


def test(name, source, expected):
    result = transpile(source)
    # Normalize trailing whitespace
    result_lines = [l.rstrip() for l in result.strip().split("\n")]
    expected_lines = [l.rstrip() for l in expected.strip().split("\n")]
    if result_lines == expected_lines:
        print(f"PASS: {name}")
        return True
    else:
        print(f"FAIL: {name}")
        print(f"  expected:")
        for l in expected_lines:
            print(f"    |{l}|")
        print(f"  got:")
        for l in result_lines:
            print(f"    |{l}|")
        return False


passed = 0
failed = 0


def run(name, source, expected):
    global passed, failed
    if test(name, source, expected):
        passed += 1
    else:
        failed += 1


# --- Braces to indentation ---

run("simple block",
    'if True {\n    print("yes")\n}',
    'if True:\n    print("yes")')

run("nested blocks",
    'if True {\n    if False {\n        print("deep")\n    }\n}',
    'if True:\n    if False:\n        print("deep")')

run("else if → elif",
    'if x > 0 {\n    print("pos")\n} else if x == 0 {\n    print("zero")\n} else {\n    print("neg")\n}',
    'if x > 0:\n    print("pos")\nelif x == 0:\n    print("zero")\nelse:\n    print("neg")')

run("for loop",
    'for i in range(10) {\n    print(i)\n}',
    'for i in range(10):\n    print(i)')

run("while loop",
    'while True {\n    break\n}',
    'while True:\n    break')

run("try except finally",
    'try {\n    risky()\n} except ValueError as e {\n    handle(e)\n} finally {\n    cleanup()\n}',
    'try:\n    risky()\nexcept ValueError as e:\n    handle(e)\nfinally:\n    cleanup()')

run("function def",
    'def greet(name) {\n    print(name)\n}',
    'def greet(name):\n    print(name)')

run("class def",
    'class Foo {\n    pass\n}',
    'class Foo:\n    pass')

# --- Method renames ---

run("init → __init__",
    'class A {\n    def init(x) {\n        self.x = x\n    }\n}',
    'class A:\n    def __init__(self, x):\n        self.x = x')

run("toString → __repr__",
    'class A {\n    def toString() {\n        return "A"\n    }\n}',
    'class A:\n    def __repr__(self):\n        return "A"')

run("equals → __eq__",
    'class A {\n    def equals(other) {\n        return self.x == other.x\n    }\n}',
    'class A:\n    def __eq__(self, other):\n        return self.x == other.x')

run("getItem → __getitem__",
    'class A {\n    def getItem(key) {\n        return self.data[key]\n    }\n}',
    'class A:\n    def __getitem__(self, key):\n        return self.data[key]')

run("ambiguous names left alone",
    'class A {\n    def add(x, y) {\n        return x + y\n    }\n}',
    'class A:\n    def add(self, x, y):\n        return x + y')

# --- Implicit self ---

run("self injected in class method",
    'class A {\n    def greet() {\n        return "hi"\n    }\n}',
    'class A:\n    def greet(self):\n        return "hi"')

run("self injected with params",
    'class A {\n    def process(x, y) {\n        return x + y\n    }\n}',
    'class A:\n    def process(self, x, y):\n        return x + y')

run("staticmethod skips self",
    'class A {\n    @staticmethod\n    def create() {\n        return A()\n    }\n}',
    'class A:\n    @staticmethod\n    def create():\n        return A()')

run("no self on top-level function",
    'def greet(name) {\n    print(name)\n}',
    'def greet(name):\n    print(name)')

# --- F-strings ---

run("string with interpolation gets f prefix",
    'x = "value is {n}"',
    'x = f"value is {n}"')

run("plain string stays plain",
    'x = "hello world"',
    'x = "hello world"')

run("single quotes with interpolation",
    "x = 'value is {n}'",
    "x = f'value is {n}'")

run("comments not touched",
    '# this has {braces}',
    '# this has {braces}')

# --- Combined ---

run("full class",
    '''class Greeter {
    def init(name) {
        self.name = name
    }

    def toString() {
        return "Hello, {self.name}"
    }

    @staticmethod
    def create(name) {
        return Greeter(name)
    }
}''',
    '''class Greeter:
    def __init__(self, name):
        self.name = name

    def __repr__(self):
        return f"Hello, {self.name}"

    @staticmethod
    def create(name):
        return Greeter(name)''')

run("main with __name__ guard",
    '''def main() {
    print("hello")
}

if __name__ == "__main__" {
    main()
}''',
    '''def main():
    print("hello")

if __name__ == "__main__":
    main()''')


# --- Results ---

print(f"\nResults: {passed} passed, {failed} failed")
sys.exit(1 if failed else 0)
