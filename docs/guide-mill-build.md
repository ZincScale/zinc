# Zinc Build Guide

Zinc transpiles `.zn` files to Java 25, compiles with `javac`, and runs on the JVM. The `zinc` CLI handles everything — from single-file scripts to multi-file projects with dependencies.

```
.zn source → zinc build → .java → javac → .class → run / package
```

---

## Installation

One command installs Zinc and everything it needs:

```bash
curl -sSL https://raw.githubusercontent.com/victorybhg/zinc/master/install.sh | sh
```

This installs:
1. **Zinc** — the compiler CLI
2. **GraalVM JDK 25** — Java runtime + `native-image` AOT compiler
3. **Mill** — build tool for projects with dependencies
4. **Quarkus CLI** — web services, REST APIs, dev mode

On macOS the installer uses Homebrew. On Linux it uses [SDKMAN](https://sdkman.io/) for JDK/Quarkus and direct binary install for Mill.

### What Each Tool Does

| Tool | Required for | Install check |
|---|---|---|
| GraalVM JDK 25 | Everything — compiles and runs `.java` | `java --version` |
| `native-image` | `zinc build --native` | `native-image --version` |
| Mill | Projects with dependencies, fat JARs | `mill --version` |
| Quarkus CLI | Web services, REST APIs, dev mode | `quarkus --version` |
| Docker | `zinc build --docker`, `--k8s` | `docker --version` |

**Minimum**: GraalVM JDK 25 is all you need for single-file scripts. Add Mill for projects with dependencies, Quarkus for web services, Docker for containers.

### Verify Installation

```bash
java --version           # should show GraalVM CE 25.x
native-image --version   # should show GraalVM CE 25.x
mill version             # should show 1.x
quarkus --version        # should show 3.x
docker --version         # should show 27.x+
```

---

## Single-File Scripts

No project setup needed. Just write and run:

```bash
zinc run hello.zn
zinc run hello.zn -- arg1 arg2
```

Build without running:

```bash
zinc build hello.zn
# output: zinc-out/
```

### Shebang Scripts

Make `.zn` files directly executable on Unix:

```zinc
#!/usr/bin/env zinc run
print("Hello from Zinc!")
```

```bash
chmod +x script.zn
./script.zn
```

---

## Multi-File Projects

### Create a Project

```bash
zinc init myapp
cd myapp
```

This generates:

```
myapp/
  build.mill.yaml          # build config (dependencies, JVM version)
  src/
    main.zn                # entry point
  test/
    (test files go here)
  .gitignore
```

### Organize Source Files

```
myapp/
  build.mill.yaml
  src/
    main.zn
    models/
      user.zn
      order.zn
    services/
      user_service.zn
  test/
    user_test.zn
```

Use `package` declarations to match directory structure:

```zinc
// src/models/user.zn
package models

data User(var name: String, var email: String)
```

```zinc
// src/main.zn
var user = User("Alice", "alice@example.com")
print(user)
```

### Run and Build

```bash
zinc run src/main.zn          # transpile + compile + run
zinc build src/                # transpile + compile all .zn in src/
zinc build src/main.zn         # transpile + compile single file
```

When a file has a `package` declaration, `zinc run` automatically builds the whole directory so cross-file references resolve.

---

## build.mill.yaml

### Minimal (no dependencies)

```yaml
extends: JavaModule
jvmVersion: 25
mvnDeps: []
```

### With Dependencies

```yaml
extends: JavaModule
jvmVersion: 25

mvnDeps:
  - com.google.code.gson:gson:2.11.0
  - org.apache.httpcomponents.client5:httpclient5:5.4
  - org.slf4j:slf4j-simple:2.0.16
```

Dependencies use Maven Central coordinates: `group:artifact:version`.

### Full Example

```yaml
extends: [JavaModule, PublishModule]
jvmVersion: 25
mainClass: com.example.Main

# Runtime + compile dependencies
mvnDeps:
  - io.quarkus:quarkus-rest:3.17.0
  - io.quarkus:quarkus-jdbc-postgresql:3.17.0
  - com.google.code.gson:gson:2.11.0

# Compile-time only (not bundled at runtime)
compileMvnDeps:
  - org.projectlombok:lombok:1.18.30

# Runtime only (not visible at compile-time)
runMvnDeps:
  - org.postgresql:postgresql:42.7.4

# Custom repositories (optional — Maven Central is always included)
repositories:
  - https://oss.sonatype.org/content/repositories/releases
```

### Dependency Types

| Key | Visible at compile | Bundled at runtime | Use case |
|---|---|---|---|
| `mvnDeps` | Yes | Yes | Most dependencies |
| `compileMvnDeps` | Yes | No | Annotation processors, compile-time tools |
| `runMvnDeps` | No | Yes | JDBC drivers, SPI implementations |

---

## Deployment Targets

### JVM JAR (default)

```bash
zinc build src/
```

Run the compiled output anywhere with Java 25+:

```bash
java --enable-preview -cp zinc-out/ Main
```

For a fat JAR (all deps bundled) with Mill:

```bash
mill app.assembly
java --enable-preview -jar out/app/assembly.dest/out.jar
```

### Native Binary (GraalVM)

```bash
zinc build --native src/
```

- ~10ms startup, ~20-50MB binary
- No JVM needed on target machine
- Best for: CLI tools, serverless/Lambda, microservices

Requires GraalVM with `native-image` installed. Falls back to JLink automatically if native-image fails.

### Docker

```bash
zinc build --docker src/
```

Generates a `Dockerfile` for your app. Then build and deploy:

```bash
docker build -t myapp .
docker run myapp
```

### Kubernetes

```bash
zinc build --k8s src/
```

Generates both a `Dockerfile` and a K8s deployment manifest:

```bash
docker build -t myregistry/myapp:latest .
docker push myregistry/myapp:latest
kubectl apply -f myapp-deployment.yaml
```

### Comparison

| | JAR | Native Image | JLink | Docker |
|---|---|---|---|---|
| Startup | ~500ms | ~10ms | ~200ms | varies |
| Binary size | Small + JVM | ~20-50 MB | ~40-80 MB | image size |
| Compatibility | All libraries | Some restrictions | All libraries | All |
| JVM required | Yes | No | No (bundled) | No (in image) |

---

## Zinc CLI Reference

| Command | What it does |
|---|---|
| `zinc init [name]` | Scaffold a new project with `build.mill.yaml` |
| `zinc run <file.zn>` | Transpile, compile, and run |
| `zinc run <file.zn> -- args` | Run with arguments |
| `zinc build <file.zn\|dir>` | Transpile and compile |
| `zinc build --native <dir>` | Native binary (GraalVM, JLink fallback) |
| `zinc build --docker <dir>` | Generate Dockerfile + compile |
| `zinc build --k8s <dir>` | Dockerfile + K8s manifest + compile |
| `zinc build -o <dir>` | Custom output directory (default: `zinc-out/`) |
| `zinc fmt <file.zn>` | Format source code |
| `zinc repl` | Interactive REPL |
| `zinc --version` | Print version |

When `build.mill.yaml` exists, `zinc build` delegates to Mill automatically for native/docker/k8s targets. Without it, Zinc uses direct `javac` compilation.

---

## Mill Direct Commands

For projects with `build.mill.yaml`, you can also use Mill directly:

```bash
mill app.compile              # compile
mill app.run                  # compile + run
mill app.run -- arg1 arg2     # pass args
mill app.test                 # run tests
mill app.assembly             # fat JAR
mill app.jar                  # thin JAR
mill app.nativeImage          # GraalVM native binary
mill app.jlink                # self-contained JRE + app
mill app.docker               # Docker image
mill app.publishM2            # publish to local Maven repo (~/.m2)
```

---

## Quarkus Projects

For web services and REST APIs, add Quarkus dependencies:

```yaml
extends: JavaModule
jvmVersion: 25

mvnDeps:
  - io.quarkus:quarkus-rest:3.17.0
  - io.quarkus:quarkus-jdbc-postgresql:3.17.0
  - io.quarkus:quarkus-smallrye-health:3.17.0
```

Quarkus provides REST endpoints, health checks, metrics, OpenAPI, dev mode with hot-reload, and automatic GraalVM native-image configuration.

---

## Common Dependencies

### HTTP & REST

```yaml
mvnDeps:
  - io.quarkus:quarkus-rest:3.17.0
  - org.apache.httpcomponents.client5:httpclient5:5.4
```

### JSON

```yaml
mvnDeps:
  - com.google.code.gson:gson:2.11.0
  - com.fasterxml.jackson.core:jackson-databind:2.18.0
```

### Database

```yaml
mvnDeps:
  - io.quarkus:quarkus-jdbc-postgresql:3.17.0
  - io.quarkus:quarkus-hibernate-orm:3.17.0
```

### Messaging

```yaml
mvnDeps:
  - io.quarkus:quarkus-kafka-client:3.17.0
  - io.nats:jnats:2.20.5
```

### Testing

```yaml
mvnDeps:
  - org.junit.jupiter:junit-jupiter:5.11.0
  - io.quarkus:quarkus-junit5:3.17.0
```

---

## CI/CD Example

GitHub Actions workflow:

```yaml
name: Build and Deploy
on: [push]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4

    - name: Set up GraalVM JDK 25
      uses: graalvm/setup-graalvm@v1
      with:
        java-version: 25
        distribution: graalvm-community

    - name: Install Mill
      run: |
        curl -fsSL https://raw.githubusercontent.com/com-lihaoyi/mill/main/mill > ~/bin/mill
        chmod +x ~/bin/mill

    - name: Install Zinc
      run: go install github.com/example/zinc/cmd/zinc@latest

    - name: Build
      run: zinc build src/

    - name: Build Docker image
      run: |
        zinc build --docker src/
        docker build -t myapp:${{ github.sha }} .

    - name: Push to registry
      run: docker push myregistry/myapp:${{ github.sha }}
```

---

## Reference

- [Mill Documentation](https://mill-build.org/mill/index.html)
- [Java Module Configuration](https://mill-build.org/mill/javalib/module-config.html)
- [Library Dependencies](https://mill-build.org/mill/fundamentals/library-deps.html)
- [Java Build Examples](https://mill-build.org/mill/javalib/build-examples.html)
