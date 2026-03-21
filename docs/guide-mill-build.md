# Zinc Build Guide — Mill

Zinc uses [Mill](https://mill-build.org/mill/index.html) as its build system. Mill is declarative (YAML config), 3-7x faster than Maven, and has built-in support for GraalVM native-image, Docker, and JLink — no plugins needed.

For single-file scripts, no build tool is needed — `zinc run script.zn` handles everything. Mill is for projects with dependencies, multiple files, and production deployment.

---

## Quick Start

```bash
# Create a new project
zinc init myapp
cd myapp

# Run it
zinc run src/main.zn

# Build it
zinc build src/main.zn

# Build native binary
zinc build --native src/main.zn

# Build Docker image
zinc build --docker src/main.zn
```

---

## Project Structure

```
myapp/
  build.mill.yaml          # Mill build config
  src/
    main.zn                # entry point
    models/
      user.zn
      order.zn
    services/
      user_service.zn
  test/
    user_test.zn
```

`zinc init` generates this structure with a starter `build.mill.yaml` and `src/main.zn`.

---

## build.mill.yaml

### Minimal

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
  - https://repo.example.com/maven2
```

### Dependency Types

| Key | Visible at compile | Bundled at runtime | Use case |
|---|---|---|---|
| `mvnDeps` | Yes | Yes | Most dependencies |
| `compileMvnDeps` | Yes | No | Annotation processors, compile-time tools |
| `runMvnDeps` | No | Yes | JDBC drivers, SPI implementations |

---

## Mill Commands

Once you have a `build.mill.yaml`, you can use Mill directly:

### Build & Run

```bash
mill app.compile              # compile Java sources
mill app.run                  # compile + run
mill app.run -- arg1 arg2     # pass args to the app
mill app.test                 # run tests
```

### Package

```bash
mill app.assembly             # build fat JAR (all deps bundled)
mill app.jar                  # build thin JAR (no deps)
```

### Native Binary (GraalVM)

```bash
mill app.nativeImage          # compile to native binary via GraalVM
```

Requires GraalVM with `native-image` installed. Produces a standalone binary (~20-50MB) with ~10ms startup.

If native-image fails (reflection-heavy libraries), fall back to JLink:

```bash
mill app.jlink                # self-contained JRE + app (~40-80MB)
```

### Docker

```bash
mill app.docker               # build Docker image
```

### Publish

```bash
mill app.publishM2            # publish to local Maven repo (~/.m2)
mill app.publishMaven         # publish to remote Maven repo
```

---

## Zinc CLI Integration

The `zinc` CLI detects `build.mill.yaml` and delegates to Mill automatically:

| zinc command | What happens |
|---|---|
| `zinc run src/main.zn` | Transpile .zn → .java, then `mill app.run` |
| `zinc build src/` | Transpile .zn → .java, then `mill app.compile` |
| `zinc build --native src/` | Transpile, then `mill app.nativeImage` (jlink fallback) |
| `zinc build --docker src/` | Transpile, then `mill app.docker` |
| `zinc build --k8s src/` | Transpile, then `mill app.docker` + generate K8s manifest |

**No `build.mill.yaml`?** Zinc falls back to direct `javac` — no Mill needed for single-file scripts.

---

## Deployment Targets

### 1. JVM JAR (default)

```bash
zinc build src/
# or
mill app.assembly
```

Run anywhere with Java 25+:
```bash
java --enable-preview -jar out/app/assembly.dest/out.jar
```

### 2. Native Binary (GraalVM)

```bash
zinc build --native src/
# or
mill app.nativeImage
```

- ~10ms startup, ~20-50MB binary
- No JVM needed on target machine
- Best for: CLI tools, serverless/Lambda, microservices

### 3. JLink (self-contained JRE)

```bash
mill app.jlink
```

- ~200ms startup, ~40-80MB
- Bundles a minimal JRE with your app
- Best for: when native-image fails, desktop apps
- Works with all libraries (no reflection restrictions)

### 4. Docker

```bash
zinc build --docker src/
# or
mill app.docker
```

Push and deploy:
```bash
docker tag myapp:latest myregistry/myapp:latest
docker push myregistry/myapp:latest
```

### 5. Kubernetes

```bash
zinc build --k8s src/
```

Generates a K8s deployment manifest + Docker image:
```bash
docker build -t myregistry/myapp:latest .
docker push myregistry/myapp:latest
kubectl apply -f myapp-deployment.yaml
```

---

## Quarkus Projects

For web services, REST APIs, and production applications, add Quarkus dependencies:

```yaml
extends: JavaModule
jvmVersion: 25

mvnDeps:
  - io.quarkus:quarkus-rest:3.17.0
  - io.quarkus:quarkus-jdbc-postgresql:3.17.0
  - io.quarkus:quarkus-smallrye-health:3.17.0
```

Quarkus provides:
- REST endpoints with `@Path`, `@GET`, `@POST`
- Virtual thread support with `@RunOnVirtualThread`
- Health checks, metrics, OpenAPI
- GraalVM native-image support (build-time class scanning, no runtime reflection)
- Dev mode with hot-reload

```bash
mill app.nativeImage          # Quarkus handles GraalVM config automatically
```

---

## Common Dependency Examples

### HTTP & REST

```yaml
mvnDeps:
  - io.quarkus:quarkus-rest:3.17.0                    # REST server
  - org.apache.httpcomponents.client5:httpclient5:5.4  # HTTP client
```

### JSON

```yaml
mvnDeps:
  - com.google.code.gson:gson:2.11.0                  # Google Gson
  - com.fasterxml.jackson.core:jackson-databind:2.18.0 # Jackson
```

### Database

```yaml
mvnDeps:
  - io.quarkus:quarkus-jdbc-postgresql:3.17.0          # PostgreSQL
  - io.quarkus:quarkus-jdbc-mysql:3.17.0               # MySQL
  - io.quarkus:quarkus-hibernate-orm:3.17.0            # ORM
```

### Messaging

```yaml
mvnDeps:
  - io.quarkus:quarkus-kafka-client:3.17.0             # Kafka
  - io.nats:jnats:2.20.5                               # NATS
```

### Testing

```yaml
# Mill uses a separate test module config
# test/build.mill.yaml or test section in main build.mill.yaml
mvnDeps:
  - org.junit.jupiter:junit-jupiter:5.11.0
  - io.quarkus:quarkus-junit5:3.17.0
```

---

## Installing Mill

```bash
# macOS
brew install mill

# Linux
curl -L https://github.com/com-lihaoyi/mill/releases/download/0.12.5/mill > ~/bin/mill
chmod +x ~/bin/mill

# Or use the Mill wrapper (auto-downloads correct version)
curl -L https://raw.githubusercontent.com/lefou/millw/main/millw > mill
chmod +x mill
./mill app.compile
```

See: [Mill Installation Guide](https://mill-build.org/mill/cli/installation-ide.html)

---

## Reference

- [Mill Documentation](https://mill-build.org/mill/index.html)
- [Java Module Configuration](https://mill-build.org/mill/javalib/module-config.html)
- [Library Dependencies](https://mill-build.org/mill/fundamentals/library-deps.html)
- [Java Build Examples](https://mill-build.org/mill/javalib/build-examples.html)
- [Publishing](https://mill-build.org/mill/javalib/publishing.html)
