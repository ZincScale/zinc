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
String label = if count == 1: "item" else: "items"
int abs_val = if x >= 0: x else: -x
String status = if active: "on" else: "off"
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

int count = 0
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
    case String -> print("string: {value}")
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

### Record Destructuring

Destructure `data` class (record) fields directly in match cases:

```zinc
match shape {
    case Circle(r) -> Math.PI * r ** 2
    case Rect(w, h) -> w * h
    case Triangle(b, h) -> 0.5 * b * h
}
```

Nested destructuring:

```zinc
match event {
    case Click(Point(x, y)) -> print("clicked at {x}, {y}")
    case KeyPress(key) -> print("pressed {key}")
}
```

### Guard Clauses

Add conditions to match cases with `if`:

```zinc
match value {
    case int n if n > 0 -> print("positive: {n}")
    case int n if n < 0 -> print("negative: {n}")
    case int n -> print("zero")
    case _ -> print("not an int")
}
```

## Match Expressions

`match` can return a value — use it on the right side of an assignment:

```zinc
double area = match shape {
    case Circle(r) -> Math.PI * r ** 2
    case Rect(w, h) -> w * h
    case Triangle(b, h) -> 0.5 * b * h
}

String label = match status {
    case 200 -> "OK"
    case 404 -> "Not Found"
    case 500 -> "Server Error"
    case _ -> "Unknown"
}
```

Match expressions must be exhaustive — all cases must be covered, or a `case _` default is required.

## with (Resource Management)

The `with` statement manages resources that need cleanup. It automatically closes resources when the block exits — similar to Python's `with` or Java's try-with-resources, but cleaner.

### Single Resource

```zinc
with f = FileReader("data.txt") {
    var line = f.readLine()
    print(line)
}
// f.close() is called automatically when the block exits
```

Transpiles to:
```java
try (var f = new FileReader("data.txt")) {
    var line = f.readLine();
    System.out.println(line);
}
```

### Multiple Resources

Comma-separated resources are all cleaned up when the block exits:

```zinc
with input = FileReader("in.txt"), output = FileWriter("out.txt") {
    output.write(input.read())
}
// both input and output are closed automatically
```

### Why `with` instead of `defer`

Some languages (Go, Swift) use `defer` for cleanup. Zinc uses `with` because:
- Cleanup is **scoped** — tied to the block, not the whole function
- Cleanup is **automatic** — any `AutoCloseable` resource gets closed
- No risk of forgetting the `defer` call
- Reads as a single unit: open, use, done
