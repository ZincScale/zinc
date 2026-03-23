# Zinc — Strings

## String Types

Zinc has three string literal forms, each with different behavior.

### Double-Quoted Strings (Interpolation)

Double-quoted strings support interpolation with `{}`:

```zinc
String name = "Alice"
int age = 30
print("Hello, {name}!")              // Hello, Alice!
print("{name} is {age} years old")   // Alice is 30 years old
```

Expressions inside `{}` are evaluated:

```zinc
print("2 + 3 = {2 + 3}")            // 2 + 3 = 5
print("upper: {name.toUpperCase()}")  // upper: ALICE
```

### Single-Quoted Strings (Literal)

Single-quoted strings are literal — no interpolation:

```zinc
String pattern = 'no {interpolation} here'
print(pattern)                       // no {interpolation} here
```

Use single quotes for regex patterns, format strings, or any text where braces are literal:

```zinc
String regex = '[a-zA-Z0-9_]+'
String template = '{user}: {message}'
```

### Triple-Quoted Strings (Multi-Line)

Triple quotes for multi-line strings:

```zinc
String query = """
SELECT name, age
FROM users
WHERE active = true
ORDER BY name
"""

String message = """Dear {name},

Thank you for your order #{orderId}.
Your items will ship on {shipDate}.

Best regards,
The Team"""
```

Triple-quoted strings support interpolation (like double-quoted strings).

## Common String Operations

```zinc
String s = "Hello, World!"

// Methods (Java String methods)
s.toUpperCase()                      // "HELLO, WORLD!"
s.toLowerCase()                      // "hello, world!"
s.trim()                             // trim whitespace
s.split(",")                         // ["Hello", " World!"]
s.replace("World", "Zinc")           // "Hello, Zinc!"
s.startsWith("Hello")                // true
s.endsWith("!")                      // true
s.length()                           // 13
s.contains("World")                  // true
s.isEmpty()                          // false
s.substring(0, 5)                    // "Hello"
s.charAt(0)                          // 'H'
s.indexOf("World")                   // 7

// Membership
if "World" in s {
    print("found")
}
```

## String Conversion

Convert values to strings with `String.valueOf()` or interpolation:

```zinc
String numStr = String.valueOf(42)       // "42"
String piStr = String.valueOf(3.14)      // "3.14"

// Or just use interpolation — cleaner:
int count = 5
String label = "{count} items"       // "5 items"
```
