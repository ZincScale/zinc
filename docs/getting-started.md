# Getting Started with Zinc

Zinc is a convention-over-configuration JVM language. Write `.zn` files, run them with `zinc run`. The compiler generates clean Java 25, compiles it, and optionally produces native binaries.

## Install

```bash
curl -sSL https://raw.githubusercontent.com/ZincScale/zinc/master/install.sh | sh
```

Requires: GraalVM JDK 25+.

## Hello World

```zinc
// hello.zn
print("Hello, world!")
```

```bash
zinc run hello.zn
# Hello, world!
```

No `public static void main`, no project setup. Top-level code just runs.

## Create a Project

```bash
zinc init my-app
```

Creates:
```
my-app/
  src/main.zn
  build.mill.yaml
  .gitignore
```

```bash
zinc run my-app/src/main.zn
# Hello from my-app!
```

## Build a Native Binary

```bash
zinc build hello.zn
./hello    # 13MB binary, 22ms startup
```

For projects with dependencies (Mill):

```bash
zinc build my-app/src
# Transpiles .zn → .java, runs mill compile, builds native binary
```

## What Zinc Generates

```zinc
// hello.zn
data Point(int x, int y)

fn main() {
    var p = new Point(3, 4)
    print("Point: {p}")
}
```

Generates:
```java
public record Point(int x, int y) {}

public class Hello {
    public static void main(String[] args) throws Exception {
        var p = new Point(3, 4);
        System.out.println("Point: " + p);
    }
}
```

Standard Java 25 — records, sealed interfaces, pattern matching, virtual threads.

## Next Steps

- [Language Reference](language-reference.md) — full syntax guide
- [Concurrency](lang/concurrency.md) — spawn, concurrent, parallel for
- [Error Handling](lang/error-handling.md) — errors as values, or handlers
- [Examples](../examples/v3/) — working code with expected output
