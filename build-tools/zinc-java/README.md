# zinc-java

End-to-end build CLI for Java projects. One install script gets a
developer a working `zinc init / build / run / test` loop — no separate
JDK, sbt, or Maven installs required. Targets **Oracle OpenJDK 25**
(the GPL build from jdk.java.net).

## Install

```bash
./install.sh       # installs to ~/.local (override with $ZINC_PREFIX)
```

`install.sh` bootstraps everything:

1. Downloads **Oracle OpenJDK 25** (the GPL build from `download.java.net`)
   into `~/.local/share/zinc/jdk/` — always, regardless of what's on
   PATH. zinc runs under this JDK so Graal / distro JDK quirks don't
   affect builds.
2. Downloads **sbt's bootstrap launcher** (`sbt-launch.jar`, ~2MB).
3. Compiles and installs `zinc-java.jar` using the managed JDK.
4. Writes a `zinc` launcher script to `~/.local/bin/` that bakes in
   `JAVA_HOME` pointing to the managed OpenJDK.

Add `~/.local/bin` to PATH if it isn't already:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Usage

```bash
zinc init myapp        # scaffold ./myapp
cd myapp
zinc build             # sbt assembly → target/myapp.jar (fat)
zinc run               # sbt run
zinc test              # sbt test (JUnit 6)
zinc clean             # sbt clean
zinc shell             # drop into interactive sbt (power users)
zinc version           # print zinc + sbt launcher paths
zinc help              # full usage
```

sbt itself is downloaded on first `zinc build` by the bundled launcher
and cached under `~/.sbt`. Subsequent builds reuse the cache.

## What it generates

`zinc init` creates:

```
myapp/
├── build.sbt                   # sbt config (JDK 25, JUnit 6, sbt-assembly)
├── project/
│   ├── build.properties        # sbt.version=1.12.6
│   └── plugins.sbt             # sbt-assembly + sbt-jupiter-interface
├── src/main/java/app/Main.java # Hello World
├── src/test/java/app/MainTest.java  # JUnit 6 sanity test
├── .gitignore
└── README.md
```

The generated `build.sbt` is plain sbt — edit it directly to add
dependencies. zinc never rewrites it after init.

```scala
libraryDependencies ++= Seq(
  "io.javalin" % "javalin" % "6.3.0",
  "com.fasterxml.jackson.core" % "jackson-databind" % "2.18.0",
  "org.slf4j" % "slf4j-api" % "2.0.16",
  "ch.qos.logback" % "logback-classic" % "1.5.12"
)
```

## Versions pinned in the scaffold

| Piece | Version | Why |
|---|---|---|
| JDK | Oracle OpenJDK 25.0.2 | Official GPL build from jdk.java.net, HotSpot |
| sbt | 1.12.6 | Latest stable 1.x — `sbt-jupiter-interface` plugin resolves here (not on 2.0 milestones) |
| sbt-assembly | 2.3.1 | Latest — produces fat jars |
| sbt-jupiter-interface | 0.18.0 | Official JUnit 5/6 bridge |
| JUnit Jupiter | 6.0.3 | Latest stable JUnit 6 |

When sbt 2.0 GAs, bump `sbt.version` in `project/build.properties` —
the official starter shape (the same one zinc generates) works verbatim
on both sbt 1.12.6 and sbt 2.0.0.

## Why this exists

zinc-flow-java (and other projects targeting the Java ecosystem) need a
consistent project-start and build command. sbt is the build system —
stable long-term support via Lightbend/Scala Center, strong incremental
compilation, mature plugin ecosystem — but its config is Scala and it's
not something every Java dev wants to learn. `zinc-java` gives Java
developers the friendly CLI on top so they don't trip on sbt's syntax
until they want to.
