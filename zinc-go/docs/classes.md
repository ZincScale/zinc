# Classes & Inheritance

Zinc provides familiar OO syntax that compiles to Go structs with embedding.

## Classes

```zinc
class Server {
    String host
    int port

    init(String host, int port = 8080) {
        this.host = host
        this.port = port
    }

    String address() {
        return "${host}:${port}"
    }

    String toString() {
        return "Server(${address()})"
    }
}

var s = Server("localhost", 3000)
print(s.address())    // localhost:3000
print(s)              // Server(localhost:3000)
```

Fields are accessed with `this.` in the constructor. In methods, bare field names work directly. `toString()` is honoured by `print()` and string interpolation.

## Visibility

Use `pub` to expose fields and methods:

```zinc
class Config {
    pub String host = "localhost"     // exported Go field
    pub int port = 8080
    String secret                     // package-private

    pub String describe() {
        return "${host}:${port}"
    }
}
```

`pub` fields become exported Go struct fields; `pub` methods become exported Go methods. Default visibility is package-private.

## Constructors that fail

Under the explicit-`error` design, an `init` block doesn't return an error directly — `init` is a constructor and has no return type to declare. To express a fallible construction, expose a free factory function (or a static-style helper) whose declared signature carries the trailing `error`:

```zinc
import stdlib/errors

class Server {
    pub String host
    pub int port

    init(String host, int port) {
        this.host = host
        this.port = port
    }
}

(Server, error) NewValidatedServer(String host, int port) {
    if (port < 1 || port > 65535) {
        return errors.IllegalArgumentError("invalid port: ${port}")
    }
    return Server(host, port), null
}

var s = NewValidatedServer("localhost", 99999) or {
    print("bad config: ${err}")
    return
}
```

This keeps the rule simple — thrower-ness is purely a property of the declared return type — and matches Go's idiomatic `New…(...) (*T, error)` constructor pattern.

## Data classes

Auto-generated record types with `toString()`:

```zinc
data User(String name, String email, int age = 0)

var u = User("Alice", "alice@example.com", 30)
print(u)    // User(name=Alice, email=alice@example.com, age=30)
```

Data classes work with generics:

```zinc
data Pair<A, B>(A first, B second)
data Result<T>(T value, bool ok)
```

## Inheritance

Single inheritance with `super()` constructor chaining:

```zinc
class Animal {
    String name
    String sound

    init(String name, String sound) {
        this.name = name
        this.sound = sound
    }

    String speak() {
        return "${name} says ${sound}"
    }
}

class Dog : Animal {
    String breed

    init(String name, String breed) {
        super(name, "Woof")
        this.breed = breed
    }

    String toString() {
        return "Dog(${name}, ${breed})"
    }
}

var dog = Dog("Rex", "Lab")
print(dog.speak())    // Rex says Woof (inherited)
print(dog)            // Dog(Rex, Lab)  (overridden toString)
```

Child classes:
- Inherit all parent fields and methods.
- Can override methods by redefining them with the same signature.
- Access parent fields directly by name.
- Call parent constructors via `super(...)`.

### Multi-level inheritance

```zinc
class Vehicle {
    String make
    int year
    init(String make, int year) {
        this.make = make
        this.year = year
    }
}

class Car : Vehicle {
    int doors
    init(String make, int year, int doors) {
        super(make, year)
        this.doors = doors
    }
}

class ElectricCar : Car {
    int maxRange
    init(String make, int year, int doors, int maxRange) {
        super(make, year, doors)
        this.maxRange = maxRange
    }
}
```

### Polymorphism

Child types assign to parent type variables:

```zinc
Vehicle v = Car("Toyota", 2024, 4)
print(v.make)    // "Toyota"
```

## Interfaces

```zinc
interface Greeter {
    String greet(String name)
}

class FormalGreeter : Greeter {
    String greet(String name) {
        return "Good day, ${name}."
    }
}

Greeter g = FormalGreeter()
print(g.greet("World"))    // Good day, World.
```

A class can extend one class and implement multiple interfaces:

```zinc
class Truck : Vehicle, Printable {
    // inherits Vehicle fields, must implement Printable methods
}
```

## Sealed classes

Closed hierarchies for exhaustive pattern matching:

```zinc
sealed class Shape {
    data Circle(double radius)
    data Rect(double width, double height)
    data Triangle(double base, double height)
}

double area(Shape s) {
    match (s) {
        case Circle(r)      { return 3.14159 * r * r }
        case Rect(w, h)     { return w * h }
        case Triangle(b, h) { return 0.5 * b * h }
    }
    return 0.0
}
```

The compiler enforces exhaustiveness — match expressions over a sealed type must cover every variant or include `case _`.

## Constants

```zinc
const String VERSION = "1.0"
const PI = 3.14159
```

## How it works

Under the hood, Zinc compiles classes to Go structs:

- Inheritance uses Go struct embedding (field/method promotion).
- Interfaces map to Go interfaces.
- `super(...)` calls the parent constructor.
- Method overrides shadow the embedded struct's methods.
- Polymorphism works through Go's structural typing.
