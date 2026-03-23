# Design: Arrays in Zinc

> **Status**: DESIGN
> **Context**: Zinc needs arrays as a fundamental type. Java's `String[]`, `int[]`, `byte[]` appear throughout the ecosystem (main args, I/O, serialization, interop).

## Syntax

```zinc
// Declaration — Type[] syntax
String[] names = ["Alice", "Bob"]
int[] scores = [1, 2, 3]
byte[] data = [0, 1, 2]

// Inferred type
var names = ["Alice", "Bob"]       // List<String> (current behavior, unchanged)

// Context-dependent inference (Option B)
int[] nums = [1, 2, 3]            // → new int[] {1, 2, 3}
List<int> nums = [1, 2, 3]        // → new ArrayList<>(List.of(1, 2, 3))
var nums = [1, 2, 3]              // → ArrayList (default, backwards compatible)

// Access
var first = names[0]
names[1] = "Charlie"
var len = names.length             // .length for arrays (not .size())

// In function signatures
fn main(String[] args) {
    print("arg: {args[0]}")
}

fn sum(int[] numbers): int {
    int total = 0
    for n in numbers { total = total + n }
    return total
}
```

## Entry Point Convention

```zinc
// Script mode — top-level code, args is implicit List<String>
print("arg count: {args.size()}")

// Project mode — fn main() is entry point
fn main() {
    // args is implicitly available as List<String>
    print("arg count: {args.size()}")
}

// Explicit args if you want them
fn main(String[] args) {
    print("arg count: {args.length}")
}
```

**Codegen for `fn main()`:**
```java
public static void main(String[] args) throws Exception {
    // args converted to List<String> as local var if used
    ...
}
```

**Codegen for `fn main(String[] args)`:**
```java
public static void main(String[] args) throws Exception {
    ...
}
```

## Type Mapping

| Zinc | Java | Notes |
|---|---|---|
| `int[]` | `int[]` | Primitive array |
| `double[]` | `double[]` | Primitive array |
| `byte[]` | `byte[]` | For I/O, serialization |
| `boolean[]` | `boolean[]` | Primitive array |
| `String[]` | `String[]` | Object array |
| `Type[]` | `Type[]` | Any object array |
| `[1, 2, 3]` (no type context) | `ArrayList<Integer>` | Default: List (backwards compatible) |
| `int[] x = [1, 2, 3]` | `int[] x = new int[] {1, 2, 3}` | Array from context |
| `List<int> x = [1, 2, 3]` | `var x = new ArrayList<>(List.of(1, 2, 3))` | List from context |

## Implementation Plan

### Lexer
- No changes needed — `[`, `]` already tokenized

### Parser
1. **Type parsing** — recognize `Type[]` as array type in `v2IsTypeAnnotation()` and type parsing
   - `ident[]` → array type
   - `ident[]?` → nullable array type
2. **Function params** — allow `Type[] name` in parameter lists
3. **AST** — add array type representation (e.g., `TypeName + IsArray bool` or `ArrayType` node)

### Typechecker
1. Recognize `Type[]` as valid type
2. Array index access `arr[i]` → check index is int
3. Array assignment `arr[i] = val` → check value type matches element type
4. `.length` property on array types
5. `for x in arr` → iterate over array

### Codegen
1. **Declaration**: `int[] x = [1, 2, 3]` → `int[] x = new int[] {1, 2, 3};`
2. **Access**: `x[0]` → `x[0]` (same)
3. **Assignment**: `x[0] = 5` → `x[0] = 5;` (same)
4. **Length**: `x.length` → `x.length` (same, not .size())
5. **For-each**: `for n in arr` → `for (var n : arr)` (same as List)
6. **Context inference**: when left-hand type is `Type[]`, generate array literal instead of ArrayList
7. **fn main()**: detect and generate `main(String[] args)`

### Scope
- Phase 1: `Type[]` in declarations, params, return types + `fn main()` entry point
- Phase 2: Array ↔ List conversions, array stream operations
