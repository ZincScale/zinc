# Design: Class Literals

## Problem

Java's `.class` syntax (`Foo.class`, `String.class`) is used for reflection, DI frameworks, serialization, and type tokens. In Zinc, `class` is a keyword, so `Foo.class` fails to parse — the parser sees `class` as the start of a class declaration.

## Examples

```zinc
// DI framework — get a bean by type
var source = ctx.getBean(HttpSource.class)

// Micronaut bootstrap
var ctx = Micronaut.run(Main.class)

// Serialization
var user = objectMapper.readValue(json, User.class)

// Reflection
var fields = User.class.getDeclaredFields()

// Logging
var log = LoggerFactory.getLogger(Pipeline.class)
```

## Design

### Syntax

Allow `.class` as a field access on any identifier or dotted expression:

```zinc
Foo.class
java.util.List.class
```

This reuses the existing selector expression syntax (`expr.field`). The only change is that `class` must be allowed as a valid field name in selector context (it's currently blocked because it's a keyword).

### Parser Changes

The selector parsing in `v2ParsePostfix()` already handles `expr.field` via `v2ExpectIdentOrKeyword()`. This function accepts certain keywords as field names (e.g., `concurrent`, `data`, `match`). Adding `class` to this list is the minimal fix:

```go
func isIdentLike(t lexer.TokenType) bool {
    return t == lexer.TOKEN_IDENT || t == lexer.TOKEN_CONCURRENT ||
        t == lexer.TOKEN_DATA || t == lexer.TOKEN_MATCH ||
        t == lexer.TOKEN_PRINT || t == lexer.TOKEN_SPAWN ||
        t == lexer.TOKEN_INTERFACE || t == lexer.TOKEN_CLASS  // add this
}
```

However, `isIdentLike` is also used for dotted type names and type annotation detection. Adding `TOKEN_CLASS` there could cause ambiguity — `Foo.class` in a type position would be misinterpreted.

### Better Approach

Instead of adding `class` to `isIdentLike`, handle `.class` specifically in the postfix/selector parser. When parsing `expr.field`, if the next token after `.` is `TOKEN_CLASS`, consume it and emit a `SelectorExpr` with field `"class"`:

In `v2ParsePostfixFrom()`:
```go
case p.check(lexer.TOKEN_DOT):
    p.advance()
    // Allow .class as a special selector (Java class literal)
    if p.check(lexer.TOKEN_CLASS) {
        p.advance()
        expr = &SelectorExpr{Object: expr, Field: "class"}
    } else {
        field := p.v2ExpectIdentOrKeyword()
        expr = &SelectorExpr{Object: expr, Field: field}
    }
```

### Codegen

No changes needed — `SelectorExpr{Object: Ident("Foo"), Field: "class"}` already emits as `Foo.class` via `formatExpr`.

### Type Checking

`Foo.class` returns `Class<Foo>` in Java. The typechecker can treat this as `any` for now since we don't track `Class<T>` generics.

### Files to Modify

- `internal/parser/parser_v2.go` — handle `TOKEN_CLASS` in selector parsing
- `internal/codegen_java/codegen_test.go` — add test cases

### Test Cases

1. Simple: `Foo.class` → `Foo.class`
2. In method call: `ctx.getBean(Foo.class)` → `ctx.getBean(Foo.class)`
3. Dotted: `java.util.List.class` → `java.util.List.class`
4. Chained: `Foo.class.getName()` → `Foo.class.getName()`
5. In static call: `Micronaut.run(Main.class)` → `Micronaut.run(Main.class)`
