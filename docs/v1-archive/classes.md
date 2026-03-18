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

### Fields, Properties, and Readonly

Zinc has four field modifiers that control visibility and mutability:

```zinc
User {
    pub String nickname              // public, mutable — anyone can read and write
    pub readonly String name         // public, immutable — set in constructor, read-only after
    readonly String hashedPassword   // private, immutable — internal, set once, never changes
    String sessionToken              // private, mutable — internal, can be updated

    new(String name, String nickname, String password) {
        this.name = name
        this.nickname = nickname
        this.hashedPassword = hash(password)
        this.sessionToken = ""
    }

    pub updateNickname(String newName) {
        this.nickname = newName          // OK — pub, mutable
        // this.name = "other"           // ERROR — readonly, can't change after construction
    }
}

main() {
    var u = User("Alice", "Ali", "secret123")
    print(u.name)                        // OK — pub, can read
    u.nickname = "Ally"                  // OK — pub, mutable
    // u.name = "Bob"                    // ERROR — readonly
    // print(u.hashedPassword)           // ERROR — private
}
```

**How each modifier maps to C#:**

| Zinc | C# Output | Visible | Mutable |
|------|-----------|---------|---------|
| `pub String name` | `public string Name { get; set; }` | Yes | Yes |
| `pub readonly String name` | `public string Name { get; init; }` | Yes | Only in constructor |
| `readonly String name` | `private readonly string _name;` | No | Only in constructor |
| `String name` | `private string _name;` | No | Yes (internally) |

**Guidelines:**
- Use `pub readonly` for identity fields (name, id, email) — set once, never changes
- Use `pub` for fields that external code needs to modify (settings, counters)
- Use `readonly` for internal state that must not change after construction (hashed values, computed fields)
- Use bare type for internal mutable state (caches, session data)

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
