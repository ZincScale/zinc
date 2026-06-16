# Tutorials

Two real programs end to end. Both are distilled from tested examples — the HTTP service
mirrors [`dogfood/webdemo`](../dogfood/webdemo), the pipeline mirrors
[`examples/pipeline.zinc`](../examples/pipeline.zinc) and friends.

---

## 1. An HTTP + JSON service

A REST store: `POST /users/{id}` saves a user, `GET /users/{id}` returns it as JSON. The
state lives in a supervised `Store` actor; the HTTP server is a child of the same app, so
if the server crashes the supervisor restarts it and the store is untouched.

```java
record User(String id, String name) {}

class Store implements Actor {
  HashMap<String, User> users = new HashMap<String, User>();
  void put(String id, User u) { users.put(id, u); }   // cast (void)
  User get(String id)         { return users.get(id); } // call (typed)
}

public class Main implements Application {
  Store store = new Store();                            // supervised child #1
  HttpServer server = new HttpServer(8080, Router.create()  // supervised child #2
      .post("/users/{id}", req -> {
        User u = Json.decode(User.class, req.body());   // JSON -> record
        store.put(req.pathParam("id"), u);
        return Response.status(201);
      })
      .get("/users/{id}", req ->
          Response.ok(Json.encode(store.get(req.pathParam("id"))))
                  .header("content-type", "application/json")));

  void main() {
    // (a real service would just serve; here main doubles as a client to show it works)
    var client = HttpClient.newBuilder().build();
    var u = new User("7", "vin");
    client.send(HttpRequest.newBuilder("http://127.0.0.1:8080/users/7")
        .POST(Json.encode(u)).build());
    var got = client.send(HttpRequest.newBuilder("http://127.0.0.1:8080/users/7")
        .GET().build());
    System.out.println(Json.decode(User.class, got.body()).name());   // vin
  }
}
```

What you got for free: `Store` and `HttpServer` are declared as fields, so they're the
app's supervision tree — born in order, restarted on crash, drained in reverse on SIGTERM.
Handlers are plain lambdas closing over the store's handle. The JSON codecs are derived from
the `User` record at compile time (no reflection, no annotations). `Router` lowers to
cowboy's dispatch; `{id}` is a path param.

`zc add cowboy@2.12.0` to pull the dependency; `zc run` to start it.

---

## 2. A bounded-memory file pipeline

Count word lengths across a file that might be 8 KB or 50 GB — same code, constant memory.
First the **single-process** version (scoped streaming, accumulate into locals):

```java
public class Main {
  public static void main(String[] args) {
    int total = 0;
    int lines = 0;
    try (Reader r = Files.openReader("input.txt")) {
      while (r.hasNextLine()) {
        total += r.nextLine().length();   // one line resident at a time
        lines++;
      }
    }
    Files.writeString("summary.txt", "lines=" + lines + " chars=" + total);
  }
}
```

Now the **multi-process** version — a read stage, a real transform stage, and a write stage,
each in its own process, running in parallel and paced by bounded channels so memory stays
flat. The transform is just an actor that drains one channel and feeds the next:

```java
class Upper implements Actor {
  void run(Channel<String> in, Channel<String> out) {
    while (in.hasNext()) {
      out.put(in.take().toUpperCase());   // do the actual work here
    }
    out.close();                          // tell the next stage we're done
  }
}

public class Main implements Application {
  void main() {
    String in = "input.txt", out = "output.txt";

    Channel<String> a = new Channel<>(64);   // reader -> transform   (bounded)
    Channel<String> b = new Channel<>(64);   // transform -> writer   (bounded)
    new FileReader(in, a);                    // stage 1: file -> a
    Upper up = new Upper();
    up.run(a, b);                             // stage 2: a -> uppercase -> b (cast: own process)
    FileWriter fw = new FileWriter(b, out);   // stage 3: b -> file
    fw.join();                                // block until the pipeline drains
  }
}
```

`up.run(a, b)` is a `void` method, so the call is a cast — it runs the drain/transform loop
in `up`'s *own* process, concurrently with the reader and writer. The whole thing
([`examples/pipeline.zinc`](../examples/pipeline.zinc)) uppercases a file line-by-line; swap
the body of the loop for parsing, filtering, JSON-mapping, whatever.

Want 20 workers sharing the load instead of one? Have 20 actors each drain the *same* input
channel — items go to whoever's free (work-stealing), and `put` blocking gives you
backpressure automatically. (Chain more stages the same way: each is an actor between two
channels.)

The point: at no instant is more than ~one item-per-stage resident — the reader can't
outrun the slowest stage (the channels are bounded), and `FileReader`/`FileWriter` close
their handles when done.

---

See **[the guide](guide.md)** for the full surface, or
**[coming from Java](coming-from-java.md)** for the gotchas.
