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

## Local Package Imports

```zinc
import "myapp/utils"                 // cross-file import (handled by TypeRegistry)
```

Local imports (paths containing `/`) are resolved by the build system — all `.zn` files in a directory share a namespace.
