# Concurrency

Zinc wraps Go's concurrency primitives in clean syntax. All constructs compile to standard goroutines, channels, and sync primitives.

## spawn — goroutines

Launch a goroutine:

```zinc
spawn {
    print("running in background")
}
```

Compiles to `go func() { ... }()`.

## Channels

Typed, buffered channels for goroutine communication:

```zinc
// Create a buffered channel
var ch = Channel(1)

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
var work = Channel(10)
work.send("task1")
work.send("task2")
work.send("task3")

for (i in 0..3) {
    var task = work.recv()
    print("processing: {task}")
}
```

### Worker pool

Fan-out pattern with multiple consumers:

```zinc
var jobs = Channel(10)
var results = Channel(10)

// Spawn 3 workers
for (w in 0..3) {
    var id = w
    spawn {
        while (true) {
            var job = jobs.recv()
            if (job == "STOP") { return }
            results.send("worker {id}: {job}")
        }
    }
}

// Send work
List<String> tasks = ["fetch", "parse", "transform"]
for (task in tasks) {
    jobs.send(task)
}

// Send stop signals
for (i in 0..3) {
    jobs.send("STOP")
}

// Collect results
for (i in 0..3) {
    print(results.recv())
}
```

## parallel for

Concurrent iteration with automatic WaitGroup management:

```zinc
List<String> urls = ["url1", "url2", "url3"]
parallel for (url in urls) {
    print("fetching: {url}")
}
// All iterations complete before continuing
```

Compiles to a `sync.WaitGroup` with one goroutine per iteration.

## Comparison with Go

| Zinc | Go |
|------|-----|
| `spawn { ... }` | `go func() { ... }()` |
| `var ch = Channel(n)` | `ch := make(chan interface{}, n)` |
| `ch.send(val)` | `ch <- val` |
| `ch.recv()` | `<-ch` |
| `ch.close()` | `close(ch)` |
| `parallel for x in xs { ... }` | `var wg sync.WaitGroup` + goroutine loop |
