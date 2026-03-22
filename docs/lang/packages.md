# Zinc — Packages and Imports

## Directory-as-Package Convention

Zinc uses directory structure as the package — no `package` declaration needed.

```
src/
  main.zn              # root package (no package)
  models/
    user.zn            # package models (automatic)
    order.zn           # package models (automatic)
  services/
    report.zn          # package services (automatic)
```

The package is inferred from the file's directory relative to `src/`. If you declare a `package` explicitly, Zinc validates it matches the directory.

## Auto-Imports

Types defined in your project are automatically imported across packages:

```zinc
// src/models/user.zn — automatically gets package models
data User(String name, int age)
```

```zinc
// src/main.zn — User is auto-imported from models
fn main() {
    var u = User("Alice", 30)
    print(u)
}
```

No `import` statement needed for project types.

## Wildcard Imports

Internal wildcard imports resolve to specific types at transpile time:

```zinc
import models.*    // → import models.User; import models.Order;
```

This is tree-shaken — only types that exist in the package are imported. External wildcards pass through as-is:

```zinc
import java.nio.file.*    // passes through to Java
```

## External Imports

Java imports pass through directly:

```zinc
import java.time.Instant
import java.nio.file.Path
import java.util.concurrent.locks.ReentrantLock
```

## Auto-Imported Packages

These are always available without an import:

- `java.util.*`
- `java.util.stream.*`

## Transpilation

| Zinc | Java |
|---|---|
| `src/models/user.zn` | `package models;` (auto-inferred) |
| `import java.time.Instant` | `import java.time.Instant;` |
| `import models.*` | `import models.User; import models.Order;` (resolved) |
| Use `User` from another package | `import models.User;` (auto-injected) |
