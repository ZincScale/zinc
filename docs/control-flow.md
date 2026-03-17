# Control Flow

## If / Else

```zinc
if x > 0 {
    print("positive")
} else if x < 0 {
    print("negative")
} else {
    print("zero")
}
```

### Expression If

`if` can be used in expression position — returns a value:

```zinc
var label = if x > 0 { "positive" } else { "negative" }
var tier = if score > 90 { "A" } else if score > 80 { "B" } else { "C" }
```

## While Loop

```zinc
while x > 0 {
    x -= 1
}
```

## For Loops

```zinc
// C-style for
for (var i = 0; i < 10; i += 1) {
    print(i)
}

// Range loops
for i in 0..10 { print(i) }       // 0 to 9 (exclusive end)
for i in 1..=10 { print(i) }      // 1 to 10 (inclusive)

// for-in (collection)
for item in items {
    print(item)
}

// for-in with index (lists)
for (i, item) in items {
    print("{i}: {item}")
}

// for-in with key-value (maps)
var scores = {"Alice": 95, "Bob": 87}
for (name, score) in scores {
    print("{name} got {score}")
}
```

## Match / Switch

```zinc
enum Direction { North, South, East, West }

String describe(Direction d) {
    match d {
        case Direction.North -> { return "Going North" }
        case Direction.South -> { return "Going South" }
        case Direction.East  -> { return "Going East"  }
        case Direction.West  -> { return "Going West"  }
        case _ -> { return "Unknown" }
    }
}
```

### Expression Match

`match` can be used in expression position — returns a value:

```zinc
var msg = match status {
    case 1 -> "running"
    case 2 -> "stopped"
    case _ -> "unknown"
}
```

## Safe Navigation (`?.`)

Access fields and call methods on nullable references without manual null checks. If the receiver is `nil`, the expression evaluates to `nil`:

```zinc
User? user = User("Alice", Address("NYC"))

var name = user?.name           // "Alice"
var city = user?.address?.city   // "NYC"
user?.doSomething()             // skipped if nil

User? nobody = null
var x = nobody?.name             // nil
nobody?.doSomething()            // no-op
```

## Concurrency

```zinc
main() {
    Chan<Int> ch = Chan(1)

    go {
        ch.send(42)
    }

    var val = ch.receive()
    print(val)
}
```

## Tuple Unpacking

```zinc
import "strconv"

main() {
    var (n, err) = strconv.Atoi("42")
}
```

> **Note:** Both names in `var (a, b) = ...` must be used. If you only need one value, assign the other to `_`.
