# Zinc — Classes

## Class Basics

Classes are public by default. Fields are private by default — `pub` generates getters/setters, `readonly` generates getter only. Methods are private by default — `pub` makes them public.

```zinc
class Stack {
    List<int> items = []

    pub fn push(int item) {
        items.add(item)
    }

    pub fn pop(): int {
        return items.removeLast()
    }

    pub fn size(): int {
        return items.size()
    }

    override fn toString(): String {
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
    int count = 0

    pub fn increment() {
        count = count + 1            // transpiles to this.count = this.count + 1
    }

    pub fn value(): int {
        return count                 // transpiles to return this.count
    }
}
```

## Visibility

### Classes

Classes are **public** by default:

```zinc
class UserService { }                // public class UserService
```

### Fields

Fields are **private** by default. Use `pub` or `readonly` to control access:

| Declaration | Generated Java | Accessors |
|---|---|---|
| `String name` | `private String name;` | None — internal only |
| `pub String name` | `private String name;` + `getName()` + `setName()` | Getter + setter |
| `readonly String name` | `private String name;` + `getName()` | Getter only |
| `init String name` | `private final String name;` + `getName()` | Getter only (final) |
| `const String NAME = "x"` | `public static final String NAME = "x";` | Direct access (constant) |

```zinc
class User {
    init String id                      // private final + getter
    pub String name                     // private + getter + setter
    readonly String email                   // private + getter only
    int loginCount = 0                  // private, no accessors
    const String TABLE = "users"        // public static final
}
```

Transpiles to:
```java
public class User {
    private final String id;
    private String name;
    private String email;
    private int loginCount = 0;
    public static final String TABLE = "users";

    public String getId() { return id; }
    public String getName() { return name; }
    public void setName(String name) { this.name = name; }
    public String getEmail() { return email; }
}
```

This means `pub` fields are always encapsulated — frameworks that expect JavaBean conventions (Quarkus, Jackson, JPA) work automatically.

### Methods

Methods are **private** by default. Use `pub` to make them public:

```zinc
class OrderService {
    fn validate(Order order): boolean {   // private — internal helper
        return order.total > 0
    }

    pub fn placeOrder(Order order): Receipt {  // public — API
        if not validate(order) {
            return Error("invalid order")
        }
        return processPayment(order)
    }

    fn processPayment(Order order): Receipt {  // private
        // ...
    }
}
```

### Override

Use the `override` keyword before `fn` when overriding a parent method. The transpiler generates `@Override`:

```zinc
class Dog : Animal {
    override fn speak(): String {
        return "Woof!"
    }

    override fn toString(): String {
        return "Dog({name})"
    }

    override fn equals(Object other): boolean {
        if other is Dog {
            return name == other.name
        }
        return false
    }

    override fn hashCode(): int {
        return Objects.hash(name)
    }
}
```

Transpiles to:
```java
public class Dog extends Animal {
    @Override
    public String speak() {
        return "Woof!";
    }

    @Override
    public String toString() {
        return "Dog(" + name + ")";
    }

    @Override
    public boolean equals(Object other) {
        if (other instanceof Dog o) {
            return java.util.Objects.equals(name, o.name);
        }
        return false;
    }

    @Override
    public int hashCode() {
        return java.util.Objects.hash(name);
    }
}
```

The compiler enforces that `override` is only used when a parent method exists — prevents silent typo bugs.

## Fields

Fields use the type directly (mutable), `const` (immutable), or `init` (set once in constructor):

```zinc
class Config {
    String host = "localhost"           // private, mutable, has default
    int port = 8080                     // private, mutable, has default
    const String VERSION = "1.0"        // public static final
}
```

### Init Fields (final)

Use `init` for fields that must be set in the constructor and cannot be changed after. Generates a getter automatically:

```zinc
class User {
    init String name                    // private final + getter
    init String email                   // private final + getter
    int loginCount = 0                  // private, mutable
}
```

### Nullable Fields

Use `Type?` for fields that can be null:

```zinc
class Order {
    init String id
    pub String? shippingAddress = null
    pub String? trackingNumber = null
    pub String status = "pending"
}
```

The compiler enforces null safety — you must check before using a nullable field:

```zinc
if order.getTrackingNumber() != null {
    print("Tracking: {order.getTrackingNumber()}")
}
```

## Constructor

Use `fn init(...)` to define a constructor:

```zinc
class Dog {
    String name
    String breed

    fn init(String name, String breed) {
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
    pub String name
    pub String sound

    pub fn speak(): String {
        return "{name} says {sound}"
    }
}

class Dog : Animal {
    pub String breed

    pub fn fetch(): String {
        return "{name} fetches!"
    }
}
```

Multiple interfaces:

```zinc
class Dog : Animal, Serializable, Comparable {
    // First parent is extends, rest are implements
}
```

## Annotations

Java annotations work directly in Zinc:

```zinc
@Deprecated
pub fn oldMethod(): String {
    return "use newMethod"
}

// Quarkus REST endpoint
@Path("/users")
class UserResource {
    @GET
    pub fn list(): List<User> {
        return userService.findAll()
    }
}
```

## Data Classes (Records)

Use `data` for immutable value types. Each `data` declaration generates a separate Java record file. All fields are public (accessor methods generated by the record).

```zinc
data User(String name, String email, int age = 0)

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
data Point(double x, double y) {
    pub fn distance(Point other): double {
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
