# Zinc — Strings

## String Types

Zinc has three string literal forms, each with different behavior.

### Double-Quoted Strings (Interpolation)

Double-quoted strings support interpolation with `{}`:

```zinc
var str name = "Alice"
var int age = 30
print("Hello, {name}!")              // Hello, Alice!
print("{name} is {age} years old")   // Alice is 30 years old
```

Expressions inside `{}` are evaluated:

```zinc
print("2 + 3 = {2 + 3}")            // 2 + 3 = 5
print("upper: {name.upper()}")      // upper: ALICE
```

Nested quotes work inside interpolation:

```zinc
var dict<str, str> data = {"key": "value"}
print("{data["key"]}")               // value
```

### Single-Quoted Strings (Literal)

Single-quoted strings are literal -- no interpolation:

```zinc
var str pattern = 'no {interpolation} here'
print(pattern)                       // no {interpolation} here
```

Use single quotes for regex patterns, format strings, or any text where braces are literal:

```zinc
var str regex = '[a-zA-Z0-9_]+'
var str template = '{user}: {message}'
```

### Triple-Quoted Strings (Multi-Line)

Triple quotes for multi-line strings:

```zinc
var str query = """
SELECT name, age
FROM users
WHERE active = true
ORDER BY name
"""

var str message = """Dear {name},

Thank you for your order #{order_id}.
Your items will ship on {ship_date}.

Best regards,
The Team"""
```

Triple-quoted strings support interpolation (like double-quoted strings).

## Common String Operations

```zinc
var str s = "Hello, World!"

// Methods
s.upper()                            // "HELLO, WORLD!"
s.lower()                            // "hello, world!"
s.strip()                            // trim whitespace
s.split(",")                         // ["Hello", " World!"]
s.replace("World", "Zinc")           // "Hello, Zinc!"
s.startswith("Hello")                // true
s.endswith("!")                       // true
len(s)                               // 13

// Membership
if "World" in s {
    print("found")
}

// Slicing
var str first_five = s[:5]           // "Hello"
```

## String Conversion

Convert values to strings with `str()`:

```zinc
var str num_str = str(42)            // "42"
var str pi_str = str(3.14)           // "3.14"
var str bool_str = str(true)         // "True"
```

Or use interpolation:

```zinc
var int count = 5
var str label = "{count} items"      // "5 items"
```
