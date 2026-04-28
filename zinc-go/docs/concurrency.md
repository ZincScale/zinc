# Concurrency

Zinc wraps Go's concurrency primitives in clean syntax. Everything compiles to standard goroutines, channels, and `sync` primitives.

## spawn — goroutines

Launch a goroutine:

```zinc
spawn {
    print("running in background")
}
```

Compiles to `go func() { ... }()`.

### Errors inside spawn

Goroutines have no return channel to their launcher. If `doRiskyWork()` can fail, handle it inside the goroutine:

```zinc
spawn {
    var ok = doRiskyWork() or {
        logging.error("worker failed", "err", err)
        return
    }
    use(ok)
}
```

For fan-in, pass errors out over a channel:

```zinc
var errCh = Channel<errors.Err>(len(items))

parallel for (item in items) {
    process(item) or { errCh.send(err); return }
}
```

## Channels

Typed, buffered channels for goroutine communication:

```zinc
// Create — buffer size in parens
var ch = Channel<String>(1)

// Send and receive
spawn {
    ch.send("hello")
}
var msg = ch.recv()
print(msg)    // hello

// Close when done
ch.close()
```

### Producer / consumer

```zinc
var work = Channel<String>(10)
work.send("task1")
work.send("task2")
work.send("task3")

for (i in 0..3) {
    var task = work.recv()
    print("processing: ${task}")
}
```

### Worker pool

Fan-out with multiple consumers:

```zinc
var jobs = Channel<String>(10)
var results = Channel<String>(10)

for (w in 0..3) {
    var id = w
    spawn {
        while (true) {
            var job = jobs.recv()
            if (job == "STOP") { return }
            results.send("worker ${id}: ${job}")
        }
    }
}

List<String> tasks = ["fetch", "parse", "transform"]
for (task in tasks) { jobs.send(task) }
for (i in 0..3)     { jobs.send("STOP") }
for (i in 0..3)     { print(results.recv()) }
```

## parallel for

Concurrent iteration with automatic `sync.WaitGroup`:

```zinc
List<String> urls = ["url1", "url2", "url3"]
parallel for (url in urls) {
    fetch(url)
}
// All iterations complete before continuing
```

Compiles to a `sync.WaitGroup` with one goroutine per iteration.

## select

`select { case ... }` multiplexes channel operations. It maps 1:1 to Go's `select`:

```zinc
import time

// Receive with binding
var ch = Channel<int>(1)
spawn { ch.send(42) }
select {
    case x = ch.recv():
        print("got: ${x}")
}

// Receive without binding
select {
    case ch.recv():
        print("ready")
}

// Send arm
var out = Channel<String>(1)
select {
    case out.send("hi"):
        print("sent")
}

// Default arm — non-blocking
select {
    case n = ch.recv():
        print("got: ${n}")
    case _:
        print("nothing ready")
}

// Timer
select {
    case time.After(20 * time.Millisecond).recv():
        print("timeout")
    case msg = ch.recv():
        print("got: ${msg}")
}
```

## Comparison with Go

| Zinc | Go |
|------|-----|
| `spawn { ... }` | `go func() { ... }()` |
| `var ch = Channel<T>(n)` | `ch := make(chan T, n)` |
| `ch.send(val)` | `ch <- val` |
| `ch.recv()` | `<-ch` |
| `ch.close()` | `close(ch)` |
| `parallel for (x in xs) { ... }` | `var wg sync.WaitGroup` + goroutine loop |
| `select { case x = ch.recv(): ... }` | `select { case x := <-ch: ... }` |
| `select { case _: ... }` | `select { default: ... }` |
