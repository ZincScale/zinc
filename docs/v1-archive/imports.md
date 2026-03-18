# Use — Imports and Dependencies

## .NET Namespace Imports (C# Backend)

```zinc
use System.Text.Json           // → using System.Text.Json;
use Newtonsoft.Json             // → using Newtonsoft.Json;
use Serilog                     // → using Serilog;
```

### Short Aliases

Common .NET namespaces have short aliases:

| Zinc | C# Namespace |
|-------------|-------------|
| `use http` | `System.Net.Http` |
| `use json` | `System.Text.Json` |
| `use io` | `System.IO` |
| `use regex` | `System.Text.RegularExpressions` |
| `use threading` | `System.Threading` |
| `use tasks` | `System.Threading.Tasks` |
| `use diagnostics` | `System.Diagnostics` |
| `use net` | `System.Net` |
| `use crypto` | `System.Security.Cryptography` |
| `use text` | `System.Text` |
| `use xml` | `System.Xml` |
| `use data` | `System.Data` |
| `use reflection` | `System.Reflection` |
| `use linq` | `System.Linq` |
| `use collections` | `System.Collections.Generic` |

### Automatic Type Detection

The compiler runs a .NET type probe at transpile time that discovers 3,700+ BCL types. Imported constructable classes automatically emit `new`:

```zinc
use System.Diagnostics
use http
use System.Text

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

These become `<PackageReference>` entries in the generated `.csproj`. Then use them in your code:

```zinc
use Newtonsoft.Json

main() {
    var json = JsonConvert.SerializeObject(42)
    print(json)
}
```

## Same-Project Types (Auto-Discovery)

All types (classes, interfaces, enums) defined anywhere in your project are **automatically visible** to all other files. No `use` needed:

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
// main.zn — no use statements needed for project types
main() {
    var d = Dog("Rex")           // Dog from models/dog.zn
    var u = User("Alice", 30)    // User from models/user.zn
    var c = Color.Red            // Color from types/color.zn
    print(d.bark())
}
```

This matches how C#, Kotlin, and Swift work — all types in the same project/module are visible without `use` statements.

> **Note:** Top-level functions across files are currently scoped to their file. For shared logic, use classes with static methods.

