# Tutorials

These tutorials use the current `.zn` surface. They assume `zc` is on your `PATH`; see
[install](install.md) or [getting started](getting-started.md).

## Tutorial 1: A Self-Healing JSON Service

Create a project:

```sh
zc new userstore
cd userstore
zc add cowboy@2.12.0
```

Replace `src/main.zn`:

```zinc
record User(String id, String name)

class Store : Actor {
    Map<String, User> users = {}

    void put(String id, User u) { users[id] = u }
    User get(String id) { return users.get(id) }
    void reset() { users = {} }
}

class Main : Application {
    Store store = Store()
    HttpServer server = HttpServer(8080, Router.create()
        .get("/health", req -> Response.ok("ok"))
        .post("/users/{id}", req -> {
            User u = Json.decode(User, req.body())
            store.put(req.pathParam("id"), u)
            return Response.status(201)
        })
        .get("/users/{id}", req ->
            Response.ok(Json.encode(store.get(req.pathParam("id"))) )
                .header("content-type", "application/json"))
        .get("/boom", req -> {
            int x = 1 / 0
            return Response.ok("never")
        }))

    void main() {
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

## Tutorial 2: Read A Project Application

`examples/zinc/project_app` is a minimal Application project. Run it from the repository:

```sh
cd beam-transpiler
zc run --once examples/zinc/project_app
```

The app is intentionally small, but it exercises the project-mode pieces together:

- `zinc.toml` project metadata
- `class Main : Application`
- a supervised `Counter : Actor` field
- `zc run --once` for deterministic command output

Open the source:

```sh
sed -n '1,120p' examples/zinc/project_app/src/main.zn
```

The important pattern is that the Application owns the Actor as a field:

```zinc
class Main : Application {
    Counter counter = Counter()

    void main() {
        counter.add(8)
        print(counter.get())
    }
}
```

That is the core service model: declare the supervision tree in source fields, then write
ordinary imperative code against process handles.

## More Examples

The full tested reading path is [examples/README.md](../examples/README.md). The highest-value
files after these tutorials are:

- `examples/zinc/resources.zn` - config, paths, recursive discovery, and streaming.
- `examples/zinc/supervised.zn` - actor restart with stable handles.
- `examples/zinc/pipeline.zn` - bounded multi-process file pipeline.
- `examples/zinc/http_client.zn` and `http_facade.zn` - HTTP client behavior.
