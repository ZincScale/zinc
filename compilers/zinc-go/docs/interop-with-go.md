# Interop with Go

Zinc compiles to Go source, so calling Go from Zinc isn't an FFI in
the usual sense — there's no JNI, no cgo boundary, no marshalling
layer. A Go package import in Zinc emits a regular Go import in the
generated source, and a call site emits a regular Go call. This
chapter is about the few places where the surface differs from
hand-written Go.

## Importing Go packages

Three flavors of import, distinguished by path shape:

```zinc
// 1. Go stdlib — bare or slash-separated module path
import time
import strings
import net/http
import encoding/json

// 2. Zinc stdlib & project subpackages — paths starting with stdlib/
//    or referencing a sibling directory
import stdlib/errors
import stdlib/asserts
import store               // sibling subpackage in this project

// 3. Third-party Go modules — declared in zinc.toml under [deps]
import mux                 // alias for github.com/gorilla/mux
import viper               // alias for github.com/spf13/viper
```

For third-party deps, the `import` name is the *alias* you set in
`zinc.toml`:

```toml
[deps]
mux = "github.com/gorilla/mux@v1.8.1"
viper = "github.com/spf13/viper@v1.20.1"
```

Then in Zinc:

```zinc
import mux

var r = mux.NewRouter()
```

Add deps from the CLI:

```bash
zinc-go add github.com/gorilla/mux@v1.8.1
```

`[replace]` keys match `[deps]` keys for local-path overrides.

## Calling Go functions

A regular call. No special syntax.

```zinc
import strings
import strconv

var upper = strings.ToUpper("hello")
var n = strconv.Atoi("42") catch { 0 }     // Atoi returns (int, error), so it's a thrower
```

Zinc recognizes Go signatures returning `(T, error)` (or `(T1, ..., Tn,
error)`, or bare `error`) as throwers. The call site uses the same
`catch { ... }` syntax as Zinc's own throwers.

## Passing pointers — auto-pointerization

When a Go function's signature is `*T`, Zinc inserts `&` for you at the
call site. You don't write it.

```zinc
import bytes

var buf = bytes.Buffer{}
fmt.Fprintln(&buf, "x")           // OK to write & explicitly...
fmt.Fprintln(buf, "x")            // ...also OK; Zinc inserts & because Fprintln takes io.Writer (and bytes.Buffer's Write methods are pointer-receiver)
```

The rule is type-driven: when the Go signature requires a pointer and
the Zinc value is a struct, the `&` is inserted at codegen.

## Class fields typed as Go stdlib structs

When you declare a class field whose type is a Go struct with
**pointer-receiver methods** (`sync.Mutex`, `sync.RWMutex`,
`sync.WaitGroup`, `bytes.Buffer`, `http.Client`, `strings.Builder`,
...), Zinc:

1. Auto-pointerizes the field to `*T` in the generated Go struct.
2. Initializes it in the constructor — `Mu: &sync.Mutex{}`.
3. Lets you call methods on it without explicit dereferences.

```zinc
import sync

class Counter {
    sync.Mutex mu             // → *sync.Mutex
    int n

    init() { this.n = 0 }     // mu is auto-init'd to &sync.Mutex{}

    pub void inc() {
        lock (mu) { n = n + 1 }
    }

    pub int get() {
        lock (mu) { return n }
    }
}
```

Pre-fix, you'd have to remember to `this.mu = sync.Mutex{}` in every
constructor and the field would still be a value type that didn't
mutate correctly through method calls. Zinc handles the entire
boilerplate.

For `bytes.Buffer`-style fields you can re-assign with a struct
literal:

```zinc
import bytes

class Holder {
    bytes.Buffer buf

    init() { this.buf = bytes.Buffer{} }

    pub void writeIn(String s) {
        this.buf = bytes.Buffer{}        // Zinc auto-prepends & at codegen
        this.buf.WriteString(s)
    }

    pub String contents() {
        return this.buf.String()
    }
}
```

The `&` insertion happens inside both init-body folding and regular
method bodies.

## Passing values across the FFI

The general rule: **explicit pointer types in Go signatures are
auto-handled. `any` parameters that need a pointer are not** — because
the type system has no contract to read.

The canonical case: `json.Unmarshal(data []byte, v any)` returns
nothing useful unless `v` is a pointer to your destination, but the
signature is `any`. Zinc can't know that from the signature alone, so
you write `&` yourself:

```zinc
import encoding/json

class Person {
    pub String name
    pub int age
    init(String name, int age) {
        this.name = name
        this.age = age
    }
}

var p = Person("", 0)
json.Unmarshal(data, &p) catch { return }
print("${p.name} is ${p.age}")
```

Same for `avro.Unmarshal(schema, data, v)` and any other
`any`-typed-but-pointer-required Go API.

Outside an `any` argument to a Go function, `&` is **not a legal Zinc
operator**. Var inits, returns, assignments, args of Zinc-side
functions, nested sub-expressions all reject it. This keeps `&` as an
FFI escape hatch — never general Zinc surface area.

## Calling methods on Go values

Methods on Go-typed values work the same way as on Zinc values:

```zinc
import time
import net/http

var t = time.Now()
print(t.Format(time.RFC3339))

var c = http.Client{}
var resp = c.Get("https://example.com") catch { return }
defer_close(resp.Body)        // wrapped in `using` is more Zinc-idiomatic
```

When the receiver method has a pointer-receiver, the `&` is inserted
automatically. You don't write it.

## Errors from Go functions

Go's `(T, error)` is Zinc's `(T, error)`. The shapes are 1:1; there's
no wrapper type, no inference, no marshalling.

```zinc
import os

var f = os.Open("config.toml") catch {
    print("can't open: ${err}")
    return
}
```

Custom Zinc errors (extending `BaseError`) satisfy Go's `error`
interface via `BaseError.Error() string`, so they compose with
`errors.Is`, `errors.As`, and `fmt.Errorf("%w", ...)` wrapping just
like any native Go error.

```zinc
import fmt
import stdlib/errors

class NotFoundError : errors.BaseError {
    init(String msg) { super(msg) }
}

pub error doLookup() {
    return fmt.Errorf("wrapped: %w", NotFoundError("user 42"))
}
```

## Goroutines, channels, mutexes

These are all surfaced as first-class Zinc primitives — see the
[concurrency chapter](concurrency.md). Under the hood:

- `spawn { ... }` → `go func() { ... }()`
- `Channel<T>(n)` → `make(chan T, n)`
- `lock (mu) { ... }` → `mu.Lock(); defer mu.Unlock(); ...`
- `parallel for (x in xs) { ... }` → `sync.WaitGroup` + per-iteration goroutine

## What's *not* there

- **No cgo.** This is Go-target only; calling C requires you to wrap
  the C surface in a Go package and import that.
- **No reflection magic.** Zinc emits straight Go; runtime reflection
  works exactly as it does in hand-written Go.
- **No JNI / native FFI shim.** The Go binary *is* the native binary.

## Reading the generated Go

Zinc emits clean, formatted Go into `zinc-out/` (or your `-o` target).
You can read it, debug it, or hand-edit it if you ever need to. The
output is what you'd write yourself, with the boilerplate Zinc
abstracted away — `if err != nil` ladders become `catch` blocks,
`sync.Mutex` field setup becomes auto-init, struct embedding gets
generated for `class A : B` declarations.

This is by design: same property that made TypeScript trustworthy.
The output is auditable, and you can always drop down to it.
