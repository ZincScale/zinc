# Imports and Dependencies

## .NET Namespace Imports (C# Backend)

```zinc
import "System.Text.Json"           // → using System.Text.Json;
import "Newtonsoft.Json"             // → using Newtonsoft.Json;
import "Serilog"                     // → using Serilog;
```

### Short Aliases

Common .NET namespaces have short aliases:

| Zinc Import | C# Namespace |
|-------------|-------------|
| `import "http"` | `System.Net.Http` |
| `import "json"` | `System.Text.Json` |
| `import "io"` | `System.IO` |
| `import "regex"` | `System.Text.RegularExpressions` |
| `import "threading"` | `System.Threading` |
| `import "tasks"` | `System.Threading.Tasks` |
| `import "diagnostics"` | `System.Diagnostics` |
| `import "net"` | `System.Net` |
| `import "crypto"` | `System.Security.Cryptography` |
| `import "text"` | `System.Text` |
| `import "xml"` | `System.Xml` |
| `import "data"` | `System.Data` |
| `import "reflection"` | `System.Reflection` |
| `import "linq"` | `System.Linq` |
| `import "collections"` | `System.Collections.Generic` |

### Automatic Type Detection

The compiler runs a .NET type probe at transpile time that discovers 3,700+ BCL types. Imported constructable classes automatically emit `new`:

```zinc
import "System.Diagnostics"
import "http"
import "System.Text"

main() {
    var sw = Stopwatch()           // → new Stopwatch()
    var client = HttpClient()      // → new HttpClient()
    var sb = StringBuilder()       // → new StringBuilder()

    sw.Start()
    sw.Stop()
}
```

Static classes (`Console`, `Math`, `File`, etc.) are detected automatically and don't receive `new`.

### NuGet Dependencies

Declare NuGet packages in `zinc.toml`:

```toml
[dependencies]
"Newtonsoft.Json" = "13.0.3"
"Serilog" = "4.0.0"
```

These become `<PackageReference>` entries in the generated `.csproj`. Then import and use:

```zinc
import "Newtonsoft.Json"

main() {
    var json = JsonConvert.SerializeObject(42)
    print(json)
}
```

## Go Package Imports (Go Backend)

```zinc
import "os"
import "math/rand" as rand

main() {
    Any args = os.Args
}
```

### Pointer Inference

Many Go APIs expect pointer-to-struct parameters. Zinc automatically infers when `&` is needed:

```zinc
import "net/http"
import "crypto/tls"

main() {
    // http.Server.TLSConfig is *tls.Config — auto-emits &tls.Config{...}
    var s = http.Server(TLSConfig: tls.Config(MinVersion: 3))
}
```

## Same-Project Types (Auto-Discovery)

All types (classes, interfaces, enums) defined anywhere in your project are **automatically visible** to all other files. No import needed:

```
myapp/
  zinc.toml
  main.zn          ← can use Dog, User, Color without importing
  models/
    dog.zn         ← defines Dog class
    user.zn        ← defines User class
  types/
    color.zn       ← defines enum Color
```

```zinc
// main.zn — no imports needed for project types
main() {
    var d = Dog("Rex")           // Dog from models/dog.zn
    var u = User("Alice", 30)    // User from models/user.zn
    var c = Color.Red            // Color from types/color.zn
    print(d.bark())
}
```

This matches how C#, Kotlin, and Swift work — all types in the same project/module are visible without imports.

> **Note:** Top-level functions across files are currently scoped to their file. For shared logic, use classes with static methods.

## Local Package Imports (Go Backend)

On the Go backend, cross-directory imports require explicit `import` statements because Go enforces package boundaries:

```zinc
import "myapp/utils"                 // required on Go backend
```

On the C# backend, these are silently ignored — types are already auto-discovered.
