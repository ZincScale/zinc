# Tutorials

These tutorials use the current `.zn` surface. They assume `zc` is on your `PATH`; see
[install](install.md) or [getting started](getting-started.md).

## Tutorial 1: A Self-Healing JSON Service

Create a project:

```sh
zc new --py userstore
cd userstore
zc add cowboy@2.12.0
```

Replace `src/main.zn`:

```python
record User(id: String, name: String)

class Store(Actor) {
    users: Map<String, User> = {}

    def put(id: String, u: User) { users[id] = u }
    def get(id: String) -> User { return users.get(id) }
    def reset() { users = {} }
}

class Main(Application) {
    store = Store()
    server = HttpServer(8080, Router.create()
        .get("/health", req -> Response.ok("ok"))
        .post("/users/{id}", req -> {
            u: User = Json.decode(User, req.body())
            store.put(req.pathParam("id"), u)
            return Response.status(201)
        })
        .get("/users/{id}", req ->
            Response.ok(Json.encode(store.get(req.pathParam("id"))) )
                .header("content-type", "application/json"))
        .get("/boom", req -> {
            x = 1 / 0
            return Response.ok("never")
        }))

    def main() {
        print("listening on :8080")
    }
}
```

Run it:

```sh
zc run
```

In another terminal:

```sh
curl -s localhost:8080/health
curl -s -XPOST localhost:8080/users/7 -d '{"id":"7","name":"vin"}' -o /dev/null -w '%{http_code}\n'
curl -s localhost:8080/users/7
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/boom
curl -s localhost:8080/users/7
```

What this demonstrates:

- `Store` is an actor process with private state.
- `Main` supervises `store` and `server` through fields.
- route lambdas close over the actor handle and send typed messages.
- JSON codecs are derived from the `User` record.
- a crashing request returns an error without taking down the store or server.

Stop it with `Ctrl-C`. Build a release with:

```sh
zc release
```

## Tutorial 2: Read The Flowdemo Dogfood App

`dogfood/flowdemo` is the canonical acceptance app for solidifying typical application
behavior. Run it from the repository:

```sh
cd beam-transpiler
./dogfood/flowdemo/test.sh
```

The app is intentionally small, but it exercises production-shaped pieces together:

- typed config: `Config.decode(FlowConfig, configPath)`
- recursive file discovery: `Files.walk(inputDir)`
- filtering: `Files.extension(path).equals("jsonl")`
- metadata: `Files.modifiedTime(path)`
- bounded streaming: nested `with Files.openReader/openAppender`
- record processing through an actor worker
- sealed success/failure routing
- HTTP `/health` and `/status`
- worker crash and restart evidence

Open the source:

```sh
sed -n '1,220p' dogfood/flowdemo/src/main.zn
```

The most important pattern is the processing loop:

```python
for path in Files.walk(cfg.inputDir) {
    if Files.extension(path).equals("jsonl") {
        with Files.openReader(path) as r {
            with Files.openAppender(cfg.successPath) as okWriter {
                with Files.openAppender(cfg.failurePath) as badWriter {
                    while r.hasNextLine() {
                        result = worker.process(path, r.nextLine())
                        match result {
                            case Success(ff) { okWriter.writeLine(ff.name + "," + ff.score) }
                            case Failed(reason, raw) { badWriter.writeLine(reason + ":" + raw) }
                        }
                    }
                }
            }
        }
    }
}
```

That is the application model Zinc is trying to make boring: discover files, stream records,
process through supervised actors, route outcomes, and expose health/status without writing
OTP boilerplate.

## More Examples

The full tested reading path is [examples/README.md](../examples/README.md). The highest-value
files after these tutorials are:

- `examples/py/resources.zn` - config, paths, recursive discovery, and streaming.
- `examples/py/selfheal.zn` - actor restart with stable handles.
- `examples/py/pipeline.zn` - bounded multi-process file pipeline.
- `examples/py/http_client.zn` and `http_facade.zn` - HTTP client behavior.
- `examples/py/sql.zn` - Postgres API and transactions.
