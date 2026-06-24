# Zinc Guide

Zinc on BEAM compiles `.zn` code to Erlang/OTP. The canonical surface is type-first Zinc
syntax with BEAM-native actors and applications. The compiler emits BEAM modules and the
`zc` CLI builds and runs them with rebar3.

## Programs And Modules

A script can be one file with a top-level `void main()`:

```zinc
void main() {
    print("hello")
}
```

A service uses one `class Main : Application`:

```zinc
class Main : Application {
    void main() {
        print("started")
    }
}
```

Files are modules. Lowercase file names are the module names. Same-directory modules can be
referenced directly; nested modules use imports such as `from util import mathutil`.

## Values And Types

Primitive types are `int`, `double`, `boolean`, `String`, and `byte[]`. Locals infer from
assignment. Parameters must be typed. Return types are checked.

```zinc
int add(int a, int b) {
    return a + b
}

void main() {
    name = "zinc"
    print("hello " + name)
}
```

Records are immutable values:

```zinc
record User(id: String, name: String)

u = User("7", "vin")
print(u.name)
```

Enums, sealed unions, nominal interfaces, instance classes, and lambdas are supported. See
`examples/zinc/records.zn`, `match.zn`, `sealed.zn`, and `protocols.zn`.

## Collections

List and dict literals are typed when homogeneous:

```zinc
scores = {"a": 1, "b": 2}
scores["a"] = scores["a"] + 10
print(scores["a"] + len(scores))

xs = [10, 20, 30]
print(xs[0] + len(xs))
```

`List<T>` is the receive/iterate type. `ArrayList<T>` is the build/index type. `Map<K,V>` and
`HashMap<K,V>` lower to Erlang maps. Mixed dynamic values are allowed at foreign boundaries,
but typed crossings are checked.

## Control Flow And Errors

Supported flow: `if` / `else if` / `else`, `while`, `for x in xs`, `for i in range(a, b)`,
`break`, `continue`, ternary expressions, and exhaustive `match`.

```zinc
match color {
    case RED { print("warm") }
    case GREEN, BLUE { print("cool") }
}
```

Errors use unchecked exceptions:

```zinc
class BadInput(Exception) {}

try {
    throw BadInput("nope")
} catch BadInput e {
    print(e.message)
} catch Exception e {
    print("fallback")
}
```

Inside a caught `try`, outer-variable mutation is transactional: partial bindings from the
failed path are not leaked.

## Actors And Applications

`Actor` is the main BEAM abstraction.

- Actor fields are process state.
- Public methods are the message protocol.
- A method with no return type is an async cast.
- A method with `-> T` is a sync call.
- Actor handles remain valid across restarts.
- Actor fields on an `Application` are supervised children.

```zinc
class Store : Actor {
    Map<String, String> users = {}

    void put(String id, String name) { users[id] = name }
    String get(String id) { return users.get(id) }
}

class Main : Application {
    Store store = Store()

    void main() {
        store.put("7", "vin")
        print(store.get("7"))
    }
}
```

Actor protocol methods are public in the current BEAM frontend. Prefer simple public actor
methods or top-level helper functions until private helpers are promoted in the canonical
`.zn` surface.

## Files, Config, And Streaming

Small whole-file APIs:

```zinc
text = Files.readString("in.txt")
Files.writeString("out.txt", text)
```

Path and discovery APIs:

```zinc
root = "/tmp/app"
input = Files.join(root, "in")
for path in Files.walk(input) {
    if Files.extension(path).equals("jsonl") {
        print(Files.dirName(path) + ":" + Files.baseName(path))
        print(Files.modifiedTime(path))
    }
}
```

`Files.list(dir)` returns sorted direct child names. `Files.walk(dir)` returns sorted
recursive full paths. `Files.size(path)` returns file size.

Typed config decode reads JSON into a record through the existing JSON codec:

```zinc
record AppConfig(inputDir: String, output: String)
cfg: AppConfig = Config.decode(AppConfig, "config.json")
```

Scoped streaming keeps memory bounded:

```zinc
with Files.openReader(src) as r {
    with Files.openWriter(dst) as w {
        while r.hasNextLine() {
            w.writeLine(r.nextLine().toUpperCase())
        }
    }
}
```

For cross-process pipelines, use `Channel<T>`, `FileReader.pump`, and `FileWriter.drain`.
See `examples/zinc/pipeline.zn`.

## JSON, HTTP, SQL, And Other Stdlib Pieces

JSON:

```zinc
record User(id: String, name: String)
u: User = Json.decode(User, "{\"id\":\"7\",\"name\":\"vin\"}")
print(Json.encode(u))
```

Dynamic JSON access is available through `Json.parse(s).get("key").asText/asInt/...`.

HTTP client:

```zinc
client = HttpClient.newBuilder().connectTimeout(2000).build()
resp = client.send(HttpRequest.newBuilder("http://127.0.0.1:8080/health").GET().build())
print(resp.body())
```

HTTP server:

```zinc
class Main : Application {
    HttpServer server = HttpServer(8080, Router.create()
        .get("/health", req -> Response.ok("ok")))

    void main() {
        print("listening")
    }
}
```

SQL is exposed through `Db`, `query`, `exec`, and transaction lambdas.

Other covered APIs include `Log`, `Base64`, `Hex`, `Gzip`, `Crypto`, `Random`, and `Uuid`.
See `examples/zinc/encoding.zn` and `webauth.zn`.

## FFI And Dependencies

Use raw Erlang modules through the FFI escape hatch:

```zinc
from erlang import lists

sorted = lists.sort(xs)
```

Add Hex packages with:

```sh
zc add cowboy@2.12.0
```

`zc check --xref` validates undefined FFI calls. `zc check` also runs dialyzer.

## CLI Reference

```sh
zc new <name>               create a canonical .zn project
zc run [file|dir]           build and run
zc run --once [dir]         run Application main and stop, useful for tests
zc build [dir]              transpile and compile
zc test [dir]               run test/**/*.zn
zc fmt <file|dir>           reindent by brace depth
zc add <name@version>       add a Hex dependency
zc check [--xref] [dir]     xref, optionally dialyzer
zc release [dir]            self-contained OTP release tarball
zc toolchain install [ver]  install managed OTP
zc doctor                   inspect install/toolchain resolution
```

## What To Read Next

- [Getting started](getting-started.md) for the first project path.
- [Tutorials](tutorials.md) for an HTTP service and project examples.
- [Examples](../examples/README.md) for tested `.zn` snippets by topic.
- [Solidification plan](solidification-plan.md) for current engineering priorities.
