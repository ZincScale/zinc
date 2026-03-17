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

// for-in (range)
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

## Labeled Loops

Like Java, Zinc supports labeled `break` and `continue` for nested loop control:

```zinc
@outer for (var i = 0; i < 10; i += 1) {
    for (var j = 0; j < 10; j += 1) {
        if j == 5 {
            break @outer       // exits both loops
        }
        if i == j {
            continue @outer    // skips to next i iteration
        }
    }
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
