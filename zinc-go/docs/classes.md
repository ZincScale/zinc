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

    fn address(): String {
        return "{host}:{port}"
    }

    fn toString(): String {
        return "Server({address()})"
    }
}

var s = Server("localhost", 3000)
print(s.address())    // localhost:3000
print(s)              // Server(localhost:3000)
```

Fields are accessed with `this.` in the constructor. In methods, bare field names work directly.

## Data classes

Auto-generated record types with `toString()`:

```zinc
data User(String name, String email, int age = 0)

var u = User("Alice", "alice@example.com", 30)
print(u)    // User(name=Alice, email=alice@example.com, age=30)
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

    fn speak(): String {
        return "${name} says ${sound}"
    }
}

class Dog : Animal {
    String breed

    init(String name, String breed) {
        super(name, "Woof")
        this.breed = breed
    }

    fn toString(): String {
        return "Dog(${name}, ${breed})"
    }
}

var dog = Dog("Rex", "Lab")
print(dog.speak())    // Rex says Woof (inherited method)
print(dog)            // Dog(Rex, Lab) (overridden toString)
```

Child classes:
- Inherit all parent fields and methods
- Can override methods by redefining them
- Access parent fields directly by name
- Call parent constructors via `super()`

### Multi-level inheritance

```zinc
class Vehicle {
    String make
    int year

    init(String make, int year) {
        this.make = make
        this.year = year
    }

    fn category(): String {
        return "vehicle"
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

Child types can be assigned to parent type variables:

```zinc
Vehicle v = Car("Toyota", 2024, 4)
print(v.category())    // "vehicle"
```

## Interfaces

```zinc
interface Greeter {
    fn greet(String name): String
}

class FormalGreeter : Greeter {
    fn greet(String name): String {
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

## Visibility

Use `pub` to mark fields as public:

```zinc
class Config {
    pub String host = "localhost"
    pub int port = 8080
}
```

## Constants

```zinc
const String VERSION = "1.0"
```

## How it works

Under the hood, Zinc compiles classes to Go structs:
- Inheritance uses Go struct embedding (field/method promotion)
- Interfaces map directly to Go interfaces
- `super()` calls the parent's constructor
- Method overrides shadow the embedded struct's methods
- Polymorphism works through Go's structural typing
