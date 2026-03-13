# Design: Type-Before-Name Syntax Migration

## Context
Zinc uses Pascal-style type annotations (`name Type`) inherited from Go. Enterprise developers from Java/C#/Dart find this foreign — they expect `Type name`. Since Zinc's goal is familiarity for OO developers, this migration switches all type annotations to C-style (type before name). Additionally, the `Fn<(Params), Return>` function type syntax changes to Dart-style `Return Fn(Params)`, and lambda return types become always-inferred (matching C#/Java/Dart behavior).

## Syntax Changes Summary

| Construct | Current | New |
|---|---|---|
| Variable | `name Int = 5` | `Int name = 5` |
| Inferred | `x := 5` | `x := 5` |
| Nullable | `name String? = null` | `String? name = null` |
| Function | `apply(x Int) Int { }` | `Int apply(Int x) { }` |
| Void function | `greet(name String) { }` | `greet(String name) { }` |
| Param default | `greet(name String, greeting String = "Hi")` | `greet(String name, String greeting = "Hi")` |
| Variadic | `log(msgs ...String)` | `log(String... msgs)` |
| Lambda | `(x Int) Int => x * 2` | `(Int x) => x * 2` |
| Lambda untyped | `x => x * 2` | `x => x * 2` |
| Fn type | `Fn<(Int), Int>` | `Int Fn(Int)` |
| Fn type void | `Fn<(), Void>` | `Fn()` |
| Field | `name String` | `String name` |
| Const typed | `const MAX Int = 5` | `const Int MAX = 5` |
| Const inferred | `const PI = 3.14` | `const PI = 3.14` |
| Generic fn | `identity<T>(val T) T` | `T identity<T>(T val)` |
| Method | `pub speak() String` | `pub String speak()` |
| Static method | `pub static square(n Int) Int` | `pub static Int square(Int n)` |
| Constructor | `new(name String)` | `new(String name)` |

## Key Design Decisions

1. **Variadic syntax**: `String... names` (Java/C# style)
2. **Class declarations**: Stay implicit (`Dog { }`, no `class` keyword). Lookahead disambiguates from typed function returns since function names are always lowercase.
3. **Lambda return types**: Always inferred — no explicit return type syntax on lambdas. Codegen already handles `ReturnType == nil` by defaulting to `interface{}` for single-expression lambdas.
4. **Fn type syntax**: `ReturnType Fn(ParamTypes)` for non-void, `Fn(ParamTypes)` for void. Parsed via suffix check after base type.

## Parsing Disambiguation

### Top-level: class vs function with return type
Classes never have parentheses; functions always do. Scan forward from the uppercase IDENT:
- If `(` is reached before `{` → it's a function (with return type)
- If `{` or `:` is reached first → it's a class

### Class body: field vs method with return type
Same principle — methods have parentheses, fields don't. Scan forward:
- If `(` is reached before `;`/`=`/`}`/newline → method
- Otherwise → field

### Statement level: typed var decl vs expression
- `Int x = 5` → typed var (uppercase + lowercase + `=`/`;`/`}`)
- `SomeClass.new()` → expression (uppercase + `.`)

### Const declarations
- `const Int MAX = 5` → typed (after `const`, uppercase IDENT followed by another IDENT)
- `const MAX = 5` → untyped (after `const`, IDENT followed by `=`)

### Lambda detection
- `(Int x) =>` → typed param (uppercase + lowercase inside parens)
- Other forms unchanged

### Fn type parsing in `parseType()`
1. If current token is `"Fn"` + `(` → void function type `Fn(params)`
2. Else parse base type normally
3. After base type, if next is `"Fn"` + `(` → base type is return type, parse `Fn(params)`
4. Apply optional `?` suffix

## What Does NOT Change

- **AST node structures** — Same fields, only parser changes how it populates them.
- **Codegen logic** — Reads the same AST. Zero codegen changes needed.
- **Cross-file registry** — Stores metadata from AST, syntax-agnostic.
- **REPL** — Uses the same parser pipeline, picks up changes automatically.
- **`:=` inference** — Completely unchanged.
