# Zinc — Classes

## Class Basics

Classes use `class` with brace-delimited bodies. Fields are declared with `var` or `const`, using type-first syntax.

```zinc
class Stack {
    var List<int> items = []

    fn push(int item) {
        items.append(item)       // auto-injects self.items
    }

    fn pop() int {
        return items.pop()
    }

    fn len() int {               // maps to __len__(self)
        return len(items)
    }

    fn str() str {               // maps to __str__(self)
        return "Stack({items})"
    }
}

var s = Stack()
s.push(1)
s.push(2)
print(s)                         // Stack([1, 2])
```

### Auto-Self

Inside a class, field references are automatically prefixed with `self.` in the generated Python. You never write `self` in Zinc:

```zinc
class Counter {
    var int count = 0

    fn increment() {
        count = count + 1        // transpiles to self.count = self.count + 1
    }

    fn value() int {
        return count             // transpiles to return self.count
    }
}
```

## Fields

Fields use `var` (mutable) or `const` (immutable) with type-first syntax:

```zinc
class Config {
    var str host = "localhost"    // mutable, has default
    var int port = 8080          // mutable, has default
    const str version = "1.0"    // immutable
}
```

## Inheritance

Use parentheses after the class name to specify a parent class:

```zinc
class Animal {
    var str name
    var str sound

    fn speak() str {
        return "{name} says {sound}"
    }
}

class Dog(Animal) {
    var str breed

    fn fetch() str {
        return "{name} fetches!"     // inherited fields auto-inject self.
    }
}

var d = Dog(breed="Lab", name="Rex", sound="Woof")
print(d.speak())                     // Rex says Woof
print(d.fetch())                     // Rex fetches!
```

## Dunder Mapping

Zinc maps short method names to Python's dunder methods automatically:

| Zinc | Python |
|---|---|
| `fn init(...)` | `__init__` |
| `fn str()` | `__str__` |
| `fn repr()` | `__repr__` |
| `fn eq(other)` | `__eq__` |
| `fn len()` | `__len__` |
| `fn iter()` | `__iter__` |
| `fn contains(item)` | `__contains__` |
| `fn get(key)` | `__getitem__` |
| `fn set(key, val)` | `__setitem__` |
| `fn add(other)` | `__add__` |
| `fn lt(other)` | `__lt__` |
| `fn call(...)` | `__call__` |

Example:

```zinc
class Vector {
    var float x = 0.0
    var float y = 0.0

    fn add(Vector other) Vector {
        return Vector(x + other.x, y + other.y)
    }

    fn str() str {
        return "({x}, {y})"
    }

    fn eq(Vector other) bool {
        return x == other.x and y == other.y
    }
}
```

## Decorators

### @staticmethod

Static methods do not receive the instance:

```zinc
class MyClass {
    @staticmethod
    fn create() MyClass {
        return MyClass()
    }
}

var obj = MyClass.create()
```

### @classmethod

Class methods receive the class as the first argument:

```zinc
class MyClass {
    @classmethod
    fn from_dict(dict d) MyClass {
        return MyClass()
    }
}
```

### @property

Properties look like method calls but act like field access:

```zinc
class Circle {
    var float radius = 1.0

    @property
    fn area() float {
        return 3.14159 * radius * radius
    }

    @property
    fn diameter() float {
        return radius * 2
    }
}

var c = Circle(radius=5.0)
print(c.area)                    // 78.53975 — no parentheses
print(c.diameter)                // 10.0
```

### Custom Decorators

Any function can be used as a decorator:

```zinc
@cache
fn expensive(int n) int {
    return compute(n)
}
```

## Data Classes

Use `data` for immutable value types. Data classes auto-generate `__init__`, `__repr__`, and `__eq__`:

```zinc
data User {
    var str name
    var str email
    var int age = 0
}

var u = User("Alice", "alice@example.com", 30)
print(u)                         // User(name='Alice', email='alice@example.com', age=30)
print(u == User("Alice", "alice@example.com", 30))  // true
```

Transpiles to Python's `@dataclass class User`.

## Enums

Enums define a fixed set of named values:

```zinc
enum Color {
    Red
    Green
    Blue
}

enum Direction {
    North
    South
    East
    West
}

var c = Color.Red
match c {
    case Color.Red -> print("red")
    case Color.Green -> print("green")
    case Color.Blue -> print("blue")
}
```
