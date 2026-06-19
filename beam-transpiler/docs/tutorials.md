# Tutorial: a self-healing JSON service

We'll build a small HTTP service that stores and serves users as JSON — and watch it shrug
off a crash without a line of recovery code. About 15 minutes. By the end you'll have used
the three things that make zinc what it is: **supervised actors**, the **failure ladder**,
and **derived JSON**.

You need `zc` ([install](install.md)). Each step below is a real command with its real
output.

---

## Step 0 — scaffold and run

```sh
zc new --py userstore && cd userstore
zc run
```
```
Hello from userstore!
```

`zc new --py` made a `zinc.toml` and `src/main.zn` (a hello-world). A top-level `def main()`
is the entry point. Let's make it do something.

## Step 1 — state that lives in a process

Open `src/main.zn` and replace it with a store. We'll drive it from `main` first, no HTTP
yet, just to see the shape:

```python
record User(id: String, name: String)

class Store(Actor) {
    users: Map<String, User> = {}
    def put(id: String, u: User) { users[id] = u }    # no return -> async (cast)
    def get(id: String) -> User  { return users.get(id) }  # typed -> sync (call)
}

class Main(Application) {
    store = Store()                     # a supervised child of the app
    def main() {
        store.put("7", User("7", "vin"))
        print(store.get("7").name)
    }
}
```
```sh
zc run
```
```
vin
```

What just happened, and why it matters:
- `class Store(Actor)` makes it a **process**, not an object. Its map is private state that
  **only the Store's own process can touch** — no locks, no data races, because no one else
  has a reference to it.
- `store` is a *field* of the `Application`, so it's a **supervised child** — the runtime
  starts it when the app boots and restarts it if it crashes.
- The method shapes are the messaging contract: `put` has no return type, so calling it is a
  fire-and-forget **cast**; `get` returns a value, so it's a synchronous **call**. You write
  ordinary code; the return type decides the protocol.

## Step 2 — put it behind HTTP

Now add an HTTP server as a *second* supervised child, with routes that close over the store.
We need cowboy:

```sh
zc add cowboy@2.12.0
```

Replace `Main` (keep `User` and `Store`):

```python
class Main(Application) {
    store = Store()
    server = HttpServer(8080, Router.create()
        .post("/users/{id}", req -> {
            u: User = Json.decode(User, req.body())        # JSON body -> User record
            store.put(req.pathParam("id"), u)
            return Response.status(201)
        })
        .get("/users/{id}", req ->
            Response.ok(Json.encode(store.get(req.pathParam("id"))))   # User -> JSON
                    .header("content-type", "application/json")))
    def main() {
        print("listening on :8080")
    }
}
```
```sh
zc run
```
```
listening on :8080
```

`main` returns, but the program **keeps serving** — an `Application` with a live child (the
`HttpServer`) runs until stopped. In another terminal:

```sh
curl -s -XPOST localhost:8080/users/7 -d '{"id":"7","name":"vin"}' -o /dev/null -w '%{http_code}\n'
# 201
curl -s localhost:8080/users/7
# {"id":"7","name":"vin"}
```

The new pieces:
- **`Router`** is a programmatic table (no annotations); `{id}` is a path parameter read via
  `req.pathParam("id")`.
- Handlers are **lambdas** that close over `store`'s handle — they send it messages.
- **`Json.decode(User, …)` / `Json.encode(…)`** use codecs the transpiler *derives from the
  `User` record at compile time* — no reflection, no annotations, no `to_json` boilerplate.
  The bare record name `User` is the Pythonic spelling (`User.class` also works).

## Step 3 — crash it on purpose

Here's the payoff. Add one more route that contains a bug, and a `GET` that proves the store
survives. Add this route to the `Router` chain:

```python
        .get("/boom", req -> { z = 0
                               x = 1 / z          # a genuine crash
                               return Response.ok("never") })
```

Restart (`Ctrl-C`, `zc run`) and hit the bug:

```sh
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/boom
# 500
curl -s localhost:8080/users/7
# {"id":"7","name":"vin"}      <- still there, service never went down
```

That `1 / z` is a genuine crash. But each request runs in **its own process**, so the bug
takes down *only that request* (500 + a log line) — the server process and the store are
untouched. That's the **failure ladder**: a bug crashes a process, the blast radius is one
request, and supervision keeps everything above it alive. You wrote zero error-handling to
get that.

(If the *server itself* crashed, its supervisor would restart it — and because `store` is a
sibling, not a child, of the server, the data would survive that too.)

## What you built

A stateful, JSON, crash-isolating HTTP service — in ~30 lines, no functional code, no
supervisor wiring, no locks, no Dockerfile. The supervision tree *is* your field
declarations; the messaging contract *is* your method signatures; the JSON *is* your record
shape.

## Next

- `zc release` bundles this into a self-contained OTP release (ERTS + beam + boot script) you
  can drop on a `$10` VM.
- For bounded-memory data pipelines (the `Channel` + `FileReader`/`FileWriter` story), see the
  streaming section of the **[guide](guide.md#7-io-and-streaming--bounded-memory-8-kb--5-gb)**
  and the tested [`examples/py/pipeline.zn`](../examples/py/pipeline.zn).
- The full surface: **[the guide](guide.md)**.
```
