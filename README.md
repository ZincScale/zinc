# Growl

**Growl** is an object-oriented language that transpiles to Go. Write clean, expressive OO code — get fast, idiomatic Go output.

```
fn main() {
    var name: String = "World"
    print("Hello, {name}!")
}
```

Transpiles to:

```go
func main() {
    name := "World"
    fmt.Println(fmt.Sprintf("Hello, %v!", name))
}
```

---

## Installation

```bash
git clone https://github.com/victorybhg/go-transpiler
cd go-transpiler
go build -o growl ./cmd/growl/
```

Requires Go 1.21+.

---

## CLI

```bash
growl <file.gw>               # transpile to <file>.go
growl <file.gw> -o out.go     # specify output file
growl <file.gw> --run         # transpile and run immediately
growl <file.gw> --watch       # watch for changes, re-transpile automatically
growl <file.gw> --verbose     # show token/AST debug info
growl repl                    # launch interactive REPL
```

---

## Language Reference

### Variables

```growl
var x: Int = 42
var name: String = "Growl"
var flag: Bool = true
var ratio: Float = 3.14
var maybeNull: String? = null    // optional (nullable) type
```

### Functions

```growl
fn add(a: Int, b: Int): Int {
    return a + b
}

pub fn greet(name: String): String {
    return "Hello, {name}!"
}
```

### Generic Functions

```growl
fn identity<T>(val: T): T {
    return val
}

fn pair<K, V>(key: K, value: V): K {
    return key
}
```

### Classes

```growl
class Dog {
    var name: String
    var age: Int

    construct new(name: String, age: Int) {
        this.name = name
        this.age = age
    }

    pub fn bark(): String {
        return "{this.name} says: Woof!"
    }

    pub static fn create(name: String): Dog {
        return Dog.new(name, 0)
    }
}
```

### Generic Classes

```growl
class Box<T> {
    var value: T

    construct new(v: T) {
        this.value = v
    }

    pub fn get(): T {
        return this.value
    }
}
```

### Interfaces

```growl
interface Speaker {
    pub fn speak(): String
}

class Cat : Speaker {
    pub fn speak(): String {
        return "Meow!"
    }
}
```

### Inheritance

```growl
class Animal {
    var name: String
    construct new(name: String) { this.name = name }
    pub fn describe(): String { return "Animal: {this.name}" }
}

class Dog : Animal, Speaker {
    construct new(name: String) {
        super(name)
    }
    pub fn speak(): String { return "Woof!" }
}
```

### Enums

```growl
enum Direction { North, South, East, West }
enum Status { Pending, Active, Closed }
```

Emits Go `iota` constants:

```go
type Direction int
const (
    North Direction = iota
    South
    East
    West
)
```

### Match / Switch

```growl
match direction {
    case 0 => { print("North") }
    case 1 => { print("South") }
    case _ => { print("Other") }
}
```

### String Interpolation

```growl
var name: String = "Growl"
var version: Int = 1
print("Welcome to {name} v{version}!")
// → fmt.Println(fmt.Sprintf("Welcome to %v v%v!", name, version))
```

### Control Flow

```growl
// if / else if / else
if (x > 0) {
    print("positive")
} else if (x < 0) {
    print("negative")
} else {
    print("zero")
}

// while loop
while (x > 0) {
    x -= 1
}

// C-style for
for (var i: Int = 0; i < 10; i += 1) {
    print(i)
}

// for-in (range)
for item in items {
    print(item)
}
```

### Error Handling

```growl
fn divide(a: Int, b: Int): Int {
    if (b == 0) {
        throw Error("division by zero")
    }
    return a / b
}

fn main() {
    try {
        var result: Int = divide(10, 0)
    } catch (err) {
        print("caught error")
    }
}
```

### Concurrency

```growl
fn main() {
    var ch: Chan<Int> = Chan.new(1)

    go {
        ch.send(42)
    }

    var val: Int = ch.receive()
    print(val)
}
```

### Tuple Unpacking

Growl maps directly to Go's multi-return. The most common use is unpacking a value + error from a `CanThrow` function (which automatically returns `(T, error)` in Go):

```growl
fn divide(a: Int, b: Int): Int {
    if (b == 0) {
        throw Error("division by zero")
    }
    return a / b
}

fn main() {
    // divide() compiles to func divide(a, b int) (int, error)
    // so we unpack both the result and the error:
    var (result, err) = divide(10, 2)
    if (err != null) {
        print("error occurred")
    } else {
        print(result)   // prints 5
    }
}
```

You can also unpack any Go function that returns multiple values via `import`:

```growl
import "strconv"

fn main() {
    // strconv.Atoi returns (int, error)
    var (n, err) = strconv.Atoi("42")
}
```

> **Note:** Both names in `var (a, b) = ...` must be used. If you only need one value, assign the other to `_` using a regular `var` and ignore it, or restructure as a `try/catch` instead.

### Imports

```growl
import "os"
import "math/rand" as rand

fn main() {
    var args: Any = os.Args
}
```

### Built-in Functions

| Growl             | Go equivalent              |
|-------------------|---------------------------|
| `print(x)`        | `fmt.Println(x)`          |
| `printf(fmt, ...)` | `fmt.Printf(fmt, ...)`  |
| `len(x)`          | `len(x)`                  |
| `append(s, x)`    | `append(s, x)`            |
| `toString(x)`     | `fmt.Sprintf("%v", x)`    |
| `parseInt(s)`     | `strconv.Atoi(s)`         |
| `parseFloat(s)`   | `strconv.ParseFloat(s,64)`|
| `sqrt(x)`         | `math.Sqrt(x)`            |
| `pow(x, y)`       | `math.Pow(x, y)`          |
| `abs(x)`          | `math.Abs(x)`             |
| `floor(x)`        | `math.Floor(x)`           |
| `ceil(x)`         | `math.Ceil(x)`            |
| `round(x)`        | `math.Round(x)`           |
| `max(a, b)`       | `math.Max(a, b)`          |
| `min(a, b)`       | `math.Min(a, b)`          |
| `strUpper(s)`     | `strings.ToUpper(s)`      |
| `strLower(s)`     | `strings.ToLower(s)`      |
| `strContains(s,x)`| `strings.Contains(s, x)` |
| `strTrim(s)`      | `strings.TrimSpace(s)`    |
| `strSplit(s, sep)`| `strings.Split(s, sep)`   |
| `strJoin(a, sep)` | `strings.Join(a, sep)`    |
| `strReplace(s,a,b)`| `strings.ReplaceAll(s,a,b)` |
| `sortInts(s)`     | `sort.Ints(s)`            |
| `sortStrings(s)`  | `sort.Strings(s)`         |
| `sortFloats(s)`   | `sort.Float64s(s)`        |
| `panic(msg)`      | `panic(msg)`              |
| `exit(code)`      | `os.Exit(code)`           |

### Type System

| Growl       | Go          |
|-------------|-------------|
| `Int`       | `int`       |
| `Float`     | `float64`   |
| `String`    | `string`    |
| `Bool`      | `bool`      |
| `Any`       | `interface{}`|
| `String?`   | `*string`   |
| `List<T>`   | `[]T`       |
| `Map<K,V>`  | `map[K]V`   |
| `Chan<T>`   | `chan T`    |

---

## Examples

See the [`examples/`](examples/) directory:

- [`hello.gw`](examples/hello.gw) — Hello World + variables
- [`classes.gw`](examples/classes.gw) — Classes, interfaces, inheritance
- [`concurrency.gw`](examples/concurrency.gw) — Channels + goroutines
- [`errors.gw`](examples/errors.gw) — try/catch/throw
- [`enums.gw`](examples/enums.gw) — Enums + match
- [`generics.gw`](examples/generics.gw) — Generic functions and classes
- [`fibonacci.gw`](examples/fibonacci.gw) — Recursion

Run any example:

```bash
./growl examples/hello.gw --run
```

---

## License

MIT
