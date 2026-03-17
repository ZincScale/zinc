# Classes and OOP

## Classes

Fields are **private by default**. Prefix with `pub` to make a field public.

```zinc
Dog {
    pub String name       // public
    pub Int age           // public
    String secret         // private

    new(String name, Int age = 0) {
        this.name = name
        this.age = age
        this.secret = "shhh"
    }

    pub String bark() {
        return "{this.name} says: Woof!"
    }

    pub static Dog create(String name) {
        return Dog(name)
    }
}
```

### Named Constructors

Every class has one primary constructor declared with `new(...)`. Additional named constructors are `pub static` factory methods:

```zinc
Point {
    pub Float x
    pub Float y

    new(Float x, Float y) {
        this.x = x
        this.y = y
    }

    pub static Point origin() {
        return Point(0.0, 0.0)
    }

    pub static Point diagonal(Float v) {
        return Point(v, v)
    }
}

main() {
    var a = Point(3.0, 4.0)        // primary constructor
    var b = Point.origin()          // named constructor
    var c = Point.diagonal(5.0)     // named constructor
}
```

### Generic Classes

```zinc
Box<T> {
    pub T value
    new(T v) { this.value = v }
    pub T get() { return this.value }
}

main() {
    var intBox = Box(42)
    var strBox = Box("hello")
    print(intBox.get())     // 42
}
```

Multi-type-parameter:

```zinc
Pair<K, V> {
    pub K key
    pub V val
    new(K key, V val) { this.key = key; this.val = val }
}
```

## Interfaces

```zinc
interface Speaker {
    pub String speak()
}

Cat : Speaker {
    pub String speak() {
        return "Meow!"
    }
}
```

## Inheritance

```zinc
Animal {
    pub String name
    new(String name) { this.name = name }
    pub String describe() { return "Animal: {this.name}" }
}

Dog : Animal, Speaker {
    new(String name) { super(name) }
    pub String speak() { return "Woof!" }
}
```

## Polymorphism

A function that accepts a class or interface type can receive any subclass:

```zinc
printSpeak(Speaker s) {
    print(s.speak())
}

main() {
    var d = Dog("Rex")
    printSpeak(d)         // Rex says Woof!
}
```

Field access through interface-typed parameters uses auto-generated getters:

```zinc
greet(Person p) {
    print("Hello, {p.name}")  // uses p.GetName() under the hood
}
```
