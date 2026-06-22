# Zinc Guide

Zinc on BEAM compiles `.zn` braces-Python to Erlang/OTP. You write Python-shaped code with
explicit types where they matter; the compiler emits BEAM modules and the `zc` CLI builds and
runs them with rebar3.

## Programs And Modules

A script can be one file with a top-level `def main()`:

```python
def main() {
    print("hello")
}
```

A service uses one `class Main(Application)`:

```python
class Main(Application) {
    def main() {
        print("started")
    }
}
```

Files are modules. Lowercase file names are the module names. Same-directory modules can be
referenced directly; nested modules use imports such as `from util import mathutil`.

## Values And Types

Primitive types are `int`, `double`, `boolean`, `String`, and `byte[]`. Locals infer from
assignment. Parameters must be typed. Return types are checked.

```python
def add(a: int, b: int) -> int {
    return a + b
}

def main() {
    name = "zinc"
    print(f"hello {name}")
}
```

Records are immutable values:

```python
record User(id: String, name: String)

u = User("7", "vin")
print(u.name)
```

Enums, sealed unions, nominal interfaces, instance classes, and lambdas are supported. See
`examples/py/records.zn`, `match.zn`, `sealed.zn`, and `protocols.zn`.

## Collections

List and dict literals are typed when homogeneous:

```python
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

```python
match color {
    case RED { print("warm") }
    case GREEN, BLUE { print("cool") }
}
```

Errors use unchecked exceptions:

```python
class BadInput(Exception) {}

try {
    raise BadInput("nope")
} except BadInput as e {
    print(e.message)
} except Exception as e {
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

```python
class Store(Actor) {
    users: Map<String, String> = {}

    def put(id: String, name: String) { users[id] = name }
    def get(id: String) -> String { return users.get(id) }
}

class Main(Application) {
    store = Store()

    def main() {
        store.put("7", "vin")
        print(store.get("7"))
    }
}
```

An actor can have `private` helpers in the legal-Java surface. In current `.zn` examples,
prefer simple public actor protocol methods or top-level helper functions until private
Python-shaped helpers are promoted in the frontend.

## Files, Config, And Streaming

Small whole-file APIs:

```python
text = Files.readString("in.txt")
Files.writeString("out.txt", text)
```

Path and discovery APIs:

```python
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

```python
record AppConfig(inputDir: String, output: String)
cfg: AppConfig = Config.decode(AppConfig, "config.json")
```

Scoped streaming keeps memory bounded:

```python
with Files.openReader(src) as r {
    with Files.openWriter(dst) as w {
        while r.hasNextLine() {
            w.writeLine(r.nextLine().toUpperCase())
        }
    }
}
```

For cross-process pipelines, use `Channel<T>`, `FileReader.pump`, and `FileWriter.drain`.
See `examples/py/pipeline.zn`.

## JSON, HTTP, SQL, And Other Stdlib Pieces

JSON:

```python
record User(id: String, name: String)
u: User = Json.decode(User, "{\"id\":\"7\",\"name\":\"vin\"}")
print(Json.encode(u))
```

Dynamic JSON access is available through `Json.parse(s).get("key").asText/asInt/...`.

HTTP client:

```python
client = HttpClient.newBuilder().connectTimeout(2000).build()
resp = client.send(HttpRequest.newBuilder("http://127.0.0.1:8080/health").GET().build())
print(resp.body())
```

HTTP server:

```python
class Main(Application) {
    server = HttpServer(8080, Router.create()
        .get("/health", req -> Response.ok("ok")))

    def main() {
        print("listening")
    }
}
```

SQL is exposed through `Db`, `query`, `exec`, and transaction lambdas. See
`examples/py/sql.zn`.

Other covered APIs include `Log`, `Base64`, `Hex`, `Gzip`, `Crypto`, `Random`, and `Uuid`.
See `examples/py/encoding.zn` and `webauth.zn`.

## FFI And Dependencies

Use raw Erlang modules through the FFI escape hatch:

```python
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
zc new --py <name>          create a .zn project
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
- [Tutorials](tutorials.md) for an HTTP service and flowdemo tour.
- [Examples](../examples/README.md) for tested `.zn` snippets by topic.
- [Solidification plan](solidification-plan.md) for current engineering priorities.
