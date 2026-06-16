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

Now the **multi-process** version — a read stage, your transform, and a write stage running
in parallel, paced by a bounded channel so memory stays flat:

```java
public class Main implements Application {
  void main() {
    String in = "input.txt", out = "output.txt";

    Channel<String> ch = new Channel<>(64);    // bounded -> backpressure
    new FileReader(in, ch);                     // stage 1: file -> channel (own process)
    FileWriter fw = new FileWriter(ch, out);    // stage 3: channel -> file (own process)
    fw.join();                                  // block until the pipeline drains
  }
}
```

To transform between the ends, drop an actor in the middle that drains one channel and feeds
another (`while (in.hasNext()) out.put(transform(in.take()));`). Want 20 workers sharing the
load? Have 20 actors each drain the *same* channel — items go to whoever's free
(work-stealing), and `put` blocking gives you backpressure automatically.

The point: at no instant is more than ~one chunk-per-stage resident. The reader can't outrun
the writer (the channel is bounded), and `FileReader`/`FileWriter` close their handles when
done.

---

See **[the guide](guide.md)** for the full surface, or
**[coming from Java](coming-from-java.md)** for the gotchas.
