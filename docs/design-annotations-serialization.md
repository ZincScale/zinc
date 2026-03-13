# Annotations & Serialization — Design Document

## Overview

Format-agnostic annotations for field metadata, plus a unified `serialize`/`deserialize` verb pair that works with any serialization format. JSON ships built-in (Go stdlib); other formats work through Go packages with no new Zinc features needed.

Follows the **Rust serde / Kotlin kotlinx.serialization** model: annotations describe the data model, not the wire format. The format is chosen at the call site. This is the proven approach — languages that went per-format (Go struct tags, Java's `@JsonProperty` vs `@XmlElement`) are the ones whose communities complain most about annotation duplication.

## Design Principles

1. **Annotations describe the data model, not the format** — `@name("x")` means "this field is called x in serialized form," regardless of JSON/YAML/TOML
2. **One verb pair for all formats** — `serialize`/`deserialize`, format as parameter
3. **Both verbs are failable** — integrates with Zinc's `or {}` error handling and auto-propagation
4. **Annotations are minimal** — 5 core annotations covering 95% of real use cases
5. **Skipped fields must have defaults** — enforced at compile time (Rust/Kotlin pattern)

## Annotations

### Core Annotations

```zinc
@rename_all("snake_case")           // class-level: rename all fields at once
UserProfile {
    String firstName            // → "first_name" in serialized output

    @name("uid")                     // field-level override
    Int userId                  // → "uid" (overrides rename_all)

    @skip                            // excluded from all serialization
    String cache = ""           // must have default (won't be deserialized)

    @required                        // deserialize fails if field missing
    String email

    @omitempty                       // skip zero/null values on serialization
    String? nickname
}
```

Five annotations: `@rename_all`, `@name`, `@skip`, `@required`, `@omitempty`.

### Annotation Reference

| Annotation | Level | Args | Description |
|-----------|-------|------|-------------|
| `@rename_all("style")` | Class | `"snake_case"`, `"camelCase"`, `"PascalCase"`, `"UPPER_SNAKE_CASE"` | Bulk rename all fields to target convention |
| `@name("wire_name")` | Field | String | Override the serialized field name |
| `@skip` | Field | None | Exclude from serialization and deserialization |
| `@required` | Field | None | Deserialization fails if field is absent |
| `@omitempty` | Field | None | Skip field on serialization if zero value or null |

### How `@rename_all` Works

Most APIs use `snake_case` or `camelCase`. Instead of annotating every field:

```zinc
// Without @rename_all — tedious
User {
    @name("first_name")
    String firstName
    @name("last_name")
    String lastName
    @name("created_at")
    String createdAt
}

// With @rename_all — one annotation covers all fields
@rename_all("snake_case")
User {
    String firstName       // → "first_name"
    String lastName        // → "last_name"
    String createdAt       // → "created_at"
}
```

`@name` on a specific field overrides `@rename_all` for that field. This is serde's most-loved feature — it eliminates 80% of field-level rename annotations.

### What They Emit (Go Struct Tags)

```zinc
@rename_all("snake_case")
User {
    String firstName

    @name("uid")
    Int userId

    @skip
    String cache = ""

    @omitempty
    String? nickname
}
```

**Emitted Go:**
```go
type UserImpl struct {
    FirstName string  `json:"first_name" yaml:"first_name"`
    UserId    int     `json:"uid" yaml:"uid"`
    Cache     string  `json:"-" yaml:"-"`
    Nickname  *string `json:"nickname,omitempty" yaml:"nickname,omitempty"`
}
```

The codegen emits tags for all configured formats. JSON is always included by default.

### Why These Five and Not More?

| Considered | Decision | Reason |
|-----------|----------|--------|
| `@readonly` | Cut | Handle in business logic, not serialization metadata |
| `@default("value")` | Cut | Zinc already has default parameter values in constructors |
| `@validate(...)` | Cut | Validation is business logic, not serialization. Keep concerns separate. |
| `@nullable` | Cut | Zinc already has null safety (`Type?`) — use that |
| `@format("date")` | Defer | Niche (date formatting). Can add later if needed |
| `@alias("old_name")` | Defer | Accept alternate names on deserialization. Useful for API evolution but not v1 |
| `@flatten` | Defer | Serde's flatten for embedded structs. Zinc's class inheritance may cover this |

### Compile-Time Validation

The typechecker enforces:

- `@name` must have exactly one string argument
- `@rename_all` must have one argument from the allowed set (`"snake_case"`, `"camelCase"`, `"PascalCase"`, `"UPPER_SNAKE_CASE"`)
- `@skip`, `@required`, `@omitempty` must have no arguments
- Annotations only valid on class fields (`@rename_all` only on class declarations)
- `@skip` fields must have a default value (compile error otherwise)
- `@name` + `@skip` on the same field is an error
- `@required` + `@omitempty` on the same field is an error (contradictory)
- `@required` + `@skip` on the same field is an error
- Duplicate annotations on the same field are an error

## Serialization

### The Verb Pair: `serialize` / `deserialize`

Both verbs are **failable**. Zinc's error handling auto-propagates errors — no explicit `or { return }` needed unless you want to add context.

```zinc
// Auto-propagation (most common) — errors propagate to caller automatically
json := serialize(user)
user := deserialize<User>(jsonStr)

// With context — log then propagate
json := serialize(user) or { print("serialize failed: {err}") }
user := deserialize<User>(input) or { print("bad input: {err}") }

// With recovery — handle error and continue
user := deserialize<User>(input) or {
    print("bad input, using defaults: {err}")
    exit(0)
}
```

**Key decisions:**
- JSON is the default format (no second argument needed)
- `serialize` returns `String`, is failable
- `deserialize<T>` takes a type parameter, returns `T`, is failable
- Format is an optional second argument

### Format Parameter

```zinc
// JSON (default — no format needed)
json := serialize(user)
user := deserialize<User>(jsonStr)

// YAML
yaml := serialize(user, Format.Yaml)
user := deserialize<User>(yamlStr, Format.Yaml)
```

```zinc
// Built-in (core)
Format.Json     // encoding/json — Go stdlib, always available

// Available via Go imports (ecosystem)
Format.Yaml     // requires gopkg.in/yaml.v3
Format.Toml     // requires github.com/BurntSushi/toml
Format.Xml      // encoding/xml — Go stdlib
```

For v1, only `Format.Json` ships. Other formats are a codegen extension — when the codegen sees `Format.Yaml`, it emits `yaml.Marshal`/`yaml.Unmarshal` and adds the import.

### Codegen: serialize

**Zinc:**
```zinc
json := serialize(user)
```

**Emitted Go (auto-propagation):**
```go
_bytes, err := json.Marshal(user)
if err != nil {
    return fmt.Errorf("serialize: %w", err)
}
json := string(_bytes)
```

**Zinc with context:**
```zinc
json := serialize(user) or { print("failed: {err}") }
```

**Emitted Go:**
```go
_bytes, err := json.Marshal(user)
if err != nil {
    fmt.Println("failed: " + err.Error())
    return fmt.Errorf("serialize: %w", err)
}
json := string(_bytes)
```

### Codegen: deserialize

**Zinc:**
```zinc
user := deserialize<User>(input)
```

**Emitted Go (auto-propagation):**
```go
var user *UserImpl
if err := json.Unmarshal([]byte(input), &user); err != nil {
    return fmt.Errorf("deserialize: %w", err)
}
```

### Codegen: deserialize with `@required` Validation

**Zinc:**
```zinc
Config {
    @required
    String host

    @required
    Int port

    Bool debug              // optional, defaults to zero value (false)
}

cfg := deserialize<Config>(input)
```

**Emitted Go:**
```go
var cfg *ConfigImpl
if err := json.Unmarshal([]byte(input), &cfg); err != nil {
    return fmt.Errorf("deserialize: %w", err)
}
if cfg.Host == "" {
    return fmt.Errorf("deserialize: required field 'host' is missing or empty")
}
if cfg.Port == 0 {
    return fmt.Errorf("deserialize: required field 'port' is missing or zero")
}
```

The `@required` checks are part of the failable operation — if validation fails, the error auto-propagates just like an unmarshal error. The caller's `or {}` handler (if any) catches both unmarshal and validation errors uniformly.

### Serializing Classes and Generics

```zinc
Animal {
    String name
    String sound

    new(String name, String sound) {
        this.name = name
        this.sound = sound
    }
}

dog := Animal("Rex", "Woof")
json := serialize(dog)           // auto-propagates on error
// {"name":"Rex","sound":"Woof"}

dog2 := deserialize<Animal>(json)
print(dog2.name)                    // Rex
```

Generic classes work the same way:

```zinc
Box<T> {
    T value

    new(T value) {
        this.value = value
    }
}

box := Box(42)
json := serialize(box)           // {"value":42}
box2 := deserialize<Box<Int>>(json)
print(box2.value)                   // 42
```

The codegen knows that class names route to `*Impl` types for marshal/unmarshal. The type parameter in `deserialize<Box<Int>>` provides the concrete Go type.

## Migration from Current Builtins

The existing `jsonEncode`/`jsonDecode` builtins **swallow errors** — this is a bug:
- `jsonEncode` uses `b, _ := json.Marshal(...)` (ignores marshal error)
- `jsonDecode` ignores the `Unmarshal` error

| Current (deprecated) | New | Difference |
|----------------------|-----|------------|
| `jsonEncode(x)` — swallows errors | `serialize(x)` | Failable, errors auto-propagate |
| `jsonDecode<T>(s)` — swallows errors | `deserialize<T>(s)` | Failable, errors auto-propagate |

Keep `jsonEncode`/`jsonDecode` working for backward compatibility but mark them as deprecated in docs. Remove in the next major version.

## Implementation Order

1. **Annotation parsing** — `@name`, `@skip`, `@required`, `@omitempty`, `@rename_all` in parser
2. **Typechecker validation** — enforce rules (skip needs default, no contradictions)
3. **Struct tag codegen** — emit tags on class `Impl` structs
4. **`serialize`/`deserialize`** — failable builtins with JSON default
5. **`@required` validation** — post-unmarshal field checks in codegen
6. **`@rename_all` codegen** — camelCase→snake_case field name transformation
7. **Deprecate `jsonEncode`/`jsonDecode`** — add warnings, update docs/examples
8. **Additional formats** — `Format.Yaml`, `Format.Xml` (codegen mappings)

Steps 1-4 deliver a working v1. Steps 5-6 add safety and ergonomics. Steps 7-8 are cleanup and extension.

## Example: Real-World API Handler

```zinc
import "net/http"

@rename_all("snake_case")
ApiRequest {
    @required
    String action

    @required
    Int userId                 // → "user_id"

    @skip
    String receivedAt = ""
}

@rename_all("snake_case")
ApiResponse {
    Bool success
    String errorMessage        // → "error_message"

    @omitempty
    String? data
}

String handleRequest(String body) {
    req := deserialize<ApiRequest>(body) or {
        resp := ApiResponse()
        resp.success = false
        resp.errorMessage = "invalid request: {err}"
        return serialize(resp)
    }

    // Process request...
    resp := ApiResponse()
    resp.success = true
    return serialize(resp)
}
```

Annotations are minimal. Serialization is failable. Errors auto-propagate. The `or {}` handler on `deserialize` catches both malformed JSON and missing `@required` fields.

## Cross-Language Design Rationale

This design follows serde (Rust) and kotlinx.serialization (Kotlin) — the two frameworks most praised by their communities:

| Decision | Precedent |
|----------|-----------|
| Unified annotations (not per-format) | Rust serde, Kotlin kotlinx.serialization |
| `@rename_all` for bulk renaming | Rust `#[serde(rename_all)]` — eliminates 80% of field annotations |
| `@skip` fields need defaults | Rust `#[serde(skip)]`, Kotlin `@Transient` |
| Format chosen at call site | Kotlin `Json.encodeToString()` vs `Yaml.encodeToString()` |
| `@required` for missing field errors | Kotlin `@Required` |
| `@omitempty` for zero-value omission | Go `omitempty`, Rust `skip_serializing_if` |
| No XML-specific design | Every unified system struggles with XML. JSON + YAML + TOML covers 95% of modern use. |

Languages that went per-format (Go's `json:"x" yaml:"x"`, Java's `@JsonProperty` vs `@XmlElement`) force devs to duplicate annotations for each format. The community consensus: this was a mistake.
