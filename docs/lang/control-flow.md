# Zinc — Control Flow

## if / else if / else

Standard conditional branching with brace-delimited blocks:

```zinc
if x > 0 {
    print("positive")
} else if x == 0 {
    print("zero")
} else {
    print("negative")
}
```

No parentheses around the condition. Braces are always required.

## Expression if (Ternary)

Use `if`/`else` as an expression. The condition comes first:

```zinc
var str label = if count == 1: "item" else: "items"
var int abs_val = if x >= 0: x else: -x
var str status = if active: "on" else: "off"
```

## for Loops

Iterate over any iterable:

```zinc
for item in items {
    print(item)
}
```

### With Index

Use two variables to get the index automatically (auto-enumerate):

```zinc
for i, item in items {
    print("{i}: {item}")
}
```

### Range-Based

```zinc
for i in range(10) {
    print(i)                     // 0 through 9
}

for i in range(1, 11) {
    print(i)                     // 1 through 10
}

for i in range(0, 20, 2) {
    print(i)                     // 0, 2, 4, ..., 18
}
```

## while Loops

```zinc
while running {
    process()
}

var int count = 0
while count < 10 {
    count = count + 1
}
```

## break and continue

`break` exits the innermost loop. `continue` skips to the next iteration:

```zinc
for item in items {
    if item == "skip" {
        continue
    }
    if item == "stop" {
        break
    }
    process(item)
}
```

## match

Pattern matching with `->` for each case:

```zinc
match command {
    case "start" -> start()
    case "stop" -> stop()
    case _ -> print("unknown")
}
```

Match on types:

```zinc
match value {
    case int -> print("integer: {value}")
    case str -> print("string: {value}")
    case _ -> print("other")
}
```

Match with multiple statements per case using braces:

```zinc
match status {
    case "error" -> {
        log_error(status)
        notify_admin()
        exit(1)
    }
    case "warning" -> log_warning(status)
    case _ -> log_info(status)
}
```
