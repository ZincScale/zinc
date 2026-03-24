# Design: Parameter Annotations

## Problem

Zinc supports annotations on classes (`@Controller class Foo {}`) and methods (`@Get pub fn index()`) but not on function/method parameters. This blocks integration with Java frameworks (Micronaut, Spring, Jakarta EE) that use parameter annotations for dependency injection, HTTP binding, and validation.

## Examples

```zinc
// HTTP controller — bind request parts to parameters
@Post("/users")
pub fn createUser(@Body String json, @Header("Authorization") String auth): HttpResponse<String> {
    // ...
}

// Path variables
@Get("/users/{id}")
pub fn getUser(@PathVariable String id): User {
    // ...
}

// DI constructor injection
@Inject
init(@Named("primary") DataSource ds, Config config) {
    this.ds = ds
    this.config = config
}
```

## Design

### Syntax

Annotations on parameters follow the same `@Name` / `@Name(args)` syntax as class/method annotations, placed before the parameter type:

```
fn method(@Annotation Type name, @Annotation("value") Type name2)
```

Multiple annotations per parameter are allowed:

```
fn method(@NotNull @Size(min: 1) String name)
```

### AST Changes

`ParamDecl` in `ast.go` gains an `Annotations` field:

```go
type ParamDecl struct {
    Name        string
    Type        TypeExpr
    Default     Expr        // default value (nil if none)
    IsVariadic  bool        // true for Type... params
    Annotations []*Annotation // parameter annotations
}
```

### Parser Changes

In `v2ParseParamList()` / parameter parsing, before parsing the type, check for `TOKEN_AT` and collect annotations:

```
// Before each parameter:
// 1. Parse annotations (0 or more @Name / @Name(args))
// 2. Parse type
// 3. Parse name
// 4. Parse optional default value
```

### Codegen Changes

In `formatParams()`, emit annotations before each parameter:

```java
// Zinc: fn ingest(@Body String body, @Header("Auth") String auth)
// Java: public String ingest(@Body String body, @Header("Auth") String auth) throws Exception {
```

### Scope

- Function parameters (top-level `fn`)
- Method parameters (class methods)
- Constructor parameters (`init(...)`)
- Lambda parameters — NOT supported (Java doesn't allow annotations on lambda params in most cases)

### Files to Modify

- `internal/parser/ast.go` — add `Annotations` to `ParamDecl`
- `internal/parser/parser_v2.go` — parse `@Name` before param type in `v2ParseParamList()`
- `internal/codegen_java/codegen.go` — emit param annotations in `formatParams()`
- `internal/codegen_java/codegen_test.go` — add test cases

### Test Cases

1. Single annotation: `fn process(@Body String data)` → `@Body String data`
2. Annotation with value: `fn get(@PathVariable("id") String id)` → `@PathVariable("id") String id`
3. Multiple annotations: `fn save(@NotNull @Valid User user)` → `@NotNull @Valid User user`
4. Mixed annotated and plain: `fn calc(@Named("x") int a, int b)` → `@Named("x") int a, int b`
5. Constructor annotations: `init(@Inject DataSource ds)` → `@Inject DataSource ds`
