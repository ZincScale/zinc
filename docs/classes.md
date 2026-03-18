# Classes and OOP

## Visibility

Everything in Zinc is **private by default**. Use `pub` to make it public. There is no `protected` or `internal` — just private and public.

| Declaration | Default | With `pub` |
|------------|---------|-----------|
| Fields | private — only accessible inside the class | public — accessible from anywhere |
| Methods | private | public |
| Functions | private — only accessible within the file | public |
| Constants | private | public |
| Classes | always public | — |
| Interfaces | always public | — |
| Enums | always public | — |

```zinc
Dog {
    pub String name        // public — accessible from outside
    String secret          // private — only inside Dog

    new(String name) {
        this.name = name
        this.secret = "shhh"
    }

    pub String bark() {    // public method
        return helper()    // can call private method
    }

    String helper() {      // private method
        return "{this.name}: {this.secret}"
    }
}

main() {
    var d = Dog("Rex")
    print(d.name)          // OK — name is pub
    print(d.bark())        // OK — bark is pub
    // print(d.secret)     // ERROR — secret is private
    // print(d.helper())   // ERROR — helper is private
}
```

**Generated C#:**

```csharp
public class Dog
{
    public string Name { get; set; }   // pub → C# property
    private string _secret;            // no pub → private field

    public string Bark() { ... }      // pub → public
    private string Helper() { ... }   // no pub → private
}
```

### Readonly Fields

Use `readonly` for fields that are set in the constructor and never change:

```zinc
User {
    pub readonly String name       // set once in constructor, then immutable
    pub readonly String email
    pub Int loginCount             // mutable — can be updated after construction

    new(String name, String email) {
        this.name = name
        this.email = email
        this.loginCount = 0
    }
}
```

**Generated C#:**

| Zinc | C# |
|------|-----|
| `pub String name` | `public string Name { get; set; }` |
| `pub readonly String name` | `public string Name { get; init; }` |
| `String name` | `private string _name;` |
| `readonly String name` | `private readonly string _name;` |

`pub readonly` maps to C# `{ get; init; }` — settable in the constructor, readonly after that. This is the proper OO pattern: most fields should be readonly, with mutation only where explicitly needed.

**Constants and functions follow the same rule:**

```zinc
const Float INTERNAL = 0.05       // private
pub const Float PI = 3.14159      // public

String helper() { return "hi" }   // private
pub String greet() { return "hello" }  // public
```

## Classes

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

## Annotations

Use `@Name` or `@Name("args")` to attach C# attributes to classes, fields, and methods. Any annotation passes through — no hardcoded list.

```zinc
@Serializable
@Table("users")
User {
    @Column("user_name")
    @Required
    pub String name

    @JsonIgnore
    String secret

    new(String name) { this.name = name }

    @HttpGet
    @Route("/api/greet")
    pub String greet() { return "Hi, {this.name}" }
}
```

Generated C#:

```csharp
[Serializable]
[Table("users")]
public class User
{
    [Column("user_name")]
    [Required]
    public string Name;

    [JsonIgnore]
    private string _secret;

    // ...

    [HttpGet]
    [Route("/api/greet")]
    public string Greet() { return $"Hi, {Name}"; }
}
```

Multiple arguments: `@Authorize("admin", "editor")` → `[Authorize("admin", "editor")]`

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
