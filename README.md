<img alt="Growler logo" src="./logo.jpg" />


# Growler

**Growler** is an object-oriented language that transpiles to Go. Write clean, expressive OO code — get fast, idiomatic Go output.

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
go build -o growler ./cmd/growler/
```

Requires Go 1.21+.

---

## CLI

```bash
growler <file.gw>               # transpile to <file>.go
growler <file.gw> -o out.go     # specify output file
growler <file.gw> --run         # transpile and run immediately
growler <file.gw> --watch       # watch for changes, re-transpile automatically
growler <file.gw> --verbose     # show token/AST debug info
growler repl                    # launch interactive REPL
```

---

## Language Reference

### Variables

```growler
var x: Int = 42
var name: String = "Growler"
var flag: Bool = true
var ratio: Float = 3.14
var maybeNull: String? = null    // optional (nullable) type
```

### Functions

```growler
fn add(a: Int, b: Int): Int {
    return a + b
}

pub fn greet(name: String): String {
    return "Hello, {name}!"
}
```

### Generic Functions

```growler
fn identity<T>(val: T): T {
    return val
}

fn pair<K, V>(key: K, value: V): K {
    return key
}
```

### Classes

```growler
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

```growler
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

```growler
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

```growler
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

```growler
enum Direction { North, South, East, West }
enum Status { Pending, Active, Closed }
```

Emits Go `iota` constants:

```go
type Direction int
const (
    DirectionNorth Direction = iota
    DirectionSouth
    DirectionEast
    DirectionWest
)
```

### Match / Switch

```growler
enum Direction { North, South, East, West }

fn describe(d: Direction): String {
    match d {
        case Direction.North => { return "Going North" }
        case Direction.South => { return "Going South" }
        case Direction.East  => { return "Going East"  }
        case Direction.West  => { return "Going West"  }
        case _ => { return "Unknown" }
    }
}
```

### String Interpolation

```growler
var name: String = "Growler"
var version: Int = 1
print("Welcome to {name} v{version}!")
// → fmt.Println(fmt.Sprintf("Welcome to %v v%v!", name, version))
```

### Control Flow

```growler
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

### Closures / Lambdas

Lambdas use the `(params): ReturnType => body` syntax. The body is either a
single expression or a block `{ ... }`.

```growler
// Single-expression lambda (inferred as a func literal)
var double = (x: Int): Int => x * 2
var greet  = (): String => "Hello!"

// Block-body lambda
var describe = (x: Int): String => {
    if (x > 0) {
        return "positive"
    }
    return "non-positive"
}

// Closure capture — lambda body may reference outer variables
var base   = 100
var addBase = (x: Int): Int => x + base

// String interpolation works inside lambda bodies
var makeMsg = (name: String): String => "Hello, {name}!"
```

Transpiles to idiomatic Go `func` literals:

```go
double  := func(x int) int { return (x * 2) }
greet   := func() string { return "Hello!" }
describe := func(x int) string { ... }
base    := 100
addBase := func(x int) int { return (x + base) }
makeMsg := func(name string) string { return fmt.Sprintf("Hello, %v!", name) }
```

#### Throwing lambdas

A lambda that contains `throw` automatically gets an `error` return appended to
its signature. Calls to that lambda inside a `try` block are automatically
unwrapped — you don't write any error-handling boilerplate:

```growler
var safeDivide = (a: Int, b: Int): Int => {
    if (b == 0) {
        throw Error("division by zero")
    }
    return a / b
}

try {
    var result = safeDivide(10, 2)   // unwrapped automatically
    print(result)
} catch(err) {
    print("Error: {err}")
}
```

Transpiles to:

```go
safeDivide := func(a int, b int) (int, error) {
    if b == 0 {
        return 0, fmt.Errorf("division by zero")
    }
    return (a / b), nil
}
{
    err := func() error {
        result, _err := safeDivide(10, 2)
        if _err != nil { return _err }
        fmt.Println(result)
        return nil
    }()
    if err != nil {
        fmt.Println(fmt.Sprintf("Error: %v", err))
    }
}
```

### Error Handling

```growler
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

```growler
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

Growler maps directly to Go's multi-return. The most common use is unpacking a value + error from a `CanThrow` function (which automatically returns `(T, error)` in Go):

```growler
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

```growler
import "strconv"

fn main() {
    // strconv.Atoi returns (int, error)
    var (n, err) = strconv.Atoi("42")
}
```

> **Note:** Both names in `var (a, b) = ...` must be used. If you only need one value, assign the other to `_` using a regular `var` and ignore it, or restructure as a `try/catch` instead.

### Imports

```growler
import "os"
import "math/rand" as rand

fn main() {
    var args: Any = os.Args
}
```

### Built-in Functions

| Growler            | Go equivalent              |
|-------------------|----------------------------|
| `print(x)`        | `fmt.Println(x)`           |
| `printf(fmt, ...)` | `fmt.Printf(fmt, ...)`   |
| `len(x)`          | `len(x)`                   |
| `append(s, x)`    | `append(s, x)`             |
| `toString(x)`     | `fmt.Sprintf("%v", x)`     |
| `parseInt(s)`     | `strconv.Atoi(s)`          |
| `parseFloat(s)`   | `strconv.ParseFloat(s,64)` |
| `sqrt(x)`         | `math.Sqrt(x)`             |
| `pow(x, y)`       | `math.Pow(x, y)`           |
| `abs(x)`          | `math.Abs(x)`              |
| `floor(x)`        | `math.Floor(x)`            |
| `ceil(x)`         | `math.Ceil(x)`             |
| `round(x)`        | `math.Round(x)`            |
| `max(a, b)`       | `math.Max(a, b)`           |
| `min(a, b)`       | `math.Min(a, b)`           |
| `strUpper(s)`     | `strings.ToUpper(s)`       |
| `strLower(s)`     | `strings.ToLower(s)`       |
| `strContains(s,x)`| `strings.Contains(s, x)`  |
| `strTrim(s)`      | `strings.TrimSpace(s)`     |
| `strSplit(s, sep)`| `strings.Split(s, sep)`    |
| `strJoin(a, sep)` | `strings.Join(a, sep)`     |
| `strReplace(s,a,b)`| `strings.ReplaceAll(s,a,b)` |
| `sortInts(s)`     | `sort.Ints(s)`             |
| `sortStrings(s)`  | `sort.Strings(s)`          |
| `sortFloats(s)`   | `sort.Float64s(s)`         |
| `panic(msg)`      | `panic(msg)`               |
| `exit(code)`      | `os.Exit(code)`            |

### Type System

| Growler     | Go          |
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
- [`closures.gw`](examples/closures.gw) — Lambdas, closures, throwing lambdas

Run any example:

```bash
./growler examples/hello.gw --run
```

---

## License

MIT
