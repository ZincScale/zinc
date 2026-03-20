# Zinc — Concurrency

Zinc runs on free-threaded Python (GIL disabled). Threads provide real parallelism, not just concurrency.

## spawn

Run a block of code in a background thread. Returns a `Future`:

```zinc
var future = spawn {
    expensive_computation()
}
print("main continues...")
var result = future.result()     // wait for result
```

Spawn multiple tasks:

```zinc
var f1 = spawn { download("file1.zip") }
var f2 = spawn { download("file2.zip") }
var f3 = spawn { download("file3.zip") }

// All three download in parallel
var r1 = f1.result()
var r2 = f2.result()
var r3 = f3.result()
```

## parallel for

Process items across a thread pool. Each iteration runs in its own thread:

```zinc
parallel for item in items {
    process(item)
}
```

On free-threaded Python 3.14+, `parallel for` achieves real speedup (8-10x on 10 items).

## Thread Safety with lock

Use `with lock` for thread-safe critical sections:

```zinc
import threading
var lock = threading.Lock()
var int counter = 0

parallel for item in items {
    var int result = compute(item)
    with lock {
        counter = counter + result
    }
}
```

## Practical Example

A parallel web scraper:

```zinc
import threading

fn scrape_urls(list<str> urls) list<str> {
    var list<str> results = []
    var lock = threading.Lock()

    parallel for url in urls {
        var str content = fetch(url)
        var str title = parse_title(content)
        with lock {
            results.append(title)
        }
    }

    return results
}

var list<str> urls = [
    "https://example.com/page1",
    "https://example.com/page2",
    "https://example.com/page3",
]
var list<str> titles = scrape_urls(urls)
for title in titles {
    print(title)
}
```

## Free-Threaded Python

Zinc targets Python 3.13+ with the free-threaded build (no GIL). This means:

- `spawn` creates real OS threads with true parallelism
- `parallel for` distributes work across CPU cores
- No GIL contention -- threads run simultaneously
- `PYTHON_GIL=0` is set automatically in generated Dockerfiles and K8s manifests

All `zinc run` and `zinc pack` commands use free-threaded Python by default.
