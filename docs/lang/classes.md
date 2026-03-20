# Zinc — Classes

## Class Basics

Classes use `class` with brace-delimited bodies. Fields are declared with `var` or `const`, using type-first syntax.

```zinc
class Stack {
    var List<int> items = []

    fn push(int item) {
        items.add(item)              // auto-injects this.items
    }

    fn pop() int {
        return items.removeLast()
    }

    fn size() int {
        return items.size()
    }

    fn str() str {                   // maps to toString()
        return "Stack({items})"
    }
}

var s = Stack()
s.push(1)
s.push(2)
print(s)                             // Stack([1, 2])
```

### Auto-This

Inside a class, field references are automatically prefixed with `this.` in the generated Java. You never write `this` in Zinc unless disambiguating from a parameter:

```zinc
class Counter {
    var int count = 0

    fn increment() {
        count = count + 1            // transpiles to this.count = this.count + 1
    }

    fn value() int {
        return count                 // transpiles to return this.count
    }
}
```

## Fields

Fields use `var` (mutable), `const` (immutable), or `init` (set once in constructor) with type-first syntax:

```zinc
class Config {
    var str host = "localhost"        // mutable, has default
    var int port = 8080              // mutable, has default
    const str VERSION = "1.0"        // immutable (static final)
}
```

### Init Fields (final)

Use `init` for fields that must be set in the constructor and cannot be changed after:

```zinc
class User {
    init str name                    // must be set in constructor
    init str email                   // frozen after construction
    var int loginCount = 0           // mutable, has default
}
```

Transpiles to `final` fields in Java:
```java
public class User {
    private final String name;
    private final String email;
    private int loginCount = 0;
}
```

### Nullable Fields

Use `Type?` for fields that can be null:

```zinc
class Order {
    init str id
    var str? shippingAddress = null   // nullable, starts as null
    var str? trackingNumber = null    // nullable, starts as null
    var str status = "pending"        // non-nullable, has default
}
```

The compiler enforces null safety — you must check before using a nullable field:

```zinc
if order.trackingNumber != null {
    print("Tracking: {order.trackingNumber}")
}

// Safe navigation
var str? tracking = order.trackingNumber?.toUpperCase()
```

## Constructor

Use `fn init(...)` to define a constructor:

```zinc
class Dog {
    var str name
    var str breed

    fn init(str name, str breed) {
        this.name = name
        this.breed = breed
    }
}

var d = Dog("Rex", "Lab")
```

## Inheritance

Use a colon after the class name to specify a parent class or interfaces:

```zinc
class Animal {
    var str name
    var str sound

    fn speak() str {
        return "{name} says {sound}"
    }
}

class Dog : Animal {
    var str breed

    fn fetch() str {
        return "{name} fetches!"     // inherited fields auto-inject this.
    }
}

var d = Dog(breed="Lab", name="Rex", sound="Woof")
print(d.speak())                     // Rex says Woof
print(d.fetch())                     // Rex fetches!
```

Multiple interfaces:

```zinc
class Dog : Animal, Serializable, Comparable {
    // First parent is extends, rest are implements
}
```

## Methods

Methods use Java names directly — no mapping or translation. Use `fn init(...)` for constructors, and standard Java method names like `toString()`, `equals()`, `hashCode()`, `compareTo()` etc.

```zinc
class Vector {
    var float x = 0.0
    var float y = 0.0

    fn add(Vector other) Vector {
        return Vector(x + other.x, y + other.y)
    }

    fn toString() str {
        return "({x}, {y})"
    }

    fn equals(Vector other) bool {
        return x == other.x and y == other.y
    }

    fn hashCode() int {
        return Objects.hash(x, y)
    }
}
```

For `data` classes (records), `equals()`, `hashCode()`, and `toString()` are generated automatically — you don't need to write them.

## Annotations

Java annotations work directly in Zinc:

```zinc
@Deprecated
fn oldMethod() str {
    return "use newMethod"
}

@Override
fn toString() str {
    return "MyClass"
}

// Quarkus REST endpoint
@Path("/users")
class UserResource {
    @GET
    fn list() List<User> {
        return userService.findAll()
    }
}
```

## Data Classes (Records)

Use `data` for immutable value types. Each `data` declaration generates a separate Java record file.

```zinc
data User {
    var str name
    var str email
    var int age = 0
}

var u = User("Alice", "alice@example.com", 30)
print(u)                         // User[name=Alice, email=alice@example.com, age=30]
print(u == User("Alice", "alice@example.com", 30))  // true
```

Transpiles to a separate `User.java`:
```java
public record User(String name, String email, int age) {
    public User(String name, String email) {
        this(name, email, 0);
    }
}
```

A single `.zn` file can contain multiple `data` declarations — each produces its own `.java` record file. Records auto-generate `equals()`, `hashCode()`, and `toString()`.

### Data Classes with Methods

```zinc
data Point {
    var float x
    var float y

    fn distance(Point other) float {
        return Math.sqrt((x - other.x) ** 2 + (y - other.y) ** 2)
    }
}
```

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

Each `enum` also generates a separate `.java` file.
