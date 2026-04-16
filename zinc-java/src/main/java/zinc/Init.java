package zinc;

import java.io.IOException;
import java.nio.file.*;

/// `zinc init [name]` — scaffolds a minimal Java project that builds and
/// runs with `zinc build` / `zinc run` out of the box. The generated
/// project targets JDK 25, uses sbt 2.0, and has JUnit 5 wired up. Other
/// libraries (Javalin, Jackson, etc.) are additive — users paste
/// libraryDependencies lines into build.sbt as they need them.
final class Init {

    // sbt 2.0 is still in milestones as of 2026-04. M5 is the latest
    // sbt-core release, but sbt-assembly (the fat-jar plugin) hasn't
    // been published for M5 yet — the newest milestone with the full
    // plugin ecosystem in place is M4. Pinning to M4 gets us the
    // brand-spanking-latest sbt 2 that actually produces a working fat
    // jar. Bump these three versions together as the 2.0 ecosystem
    // catches up; all drop in to a 2.0.0 GA upgrade when that lands.
    // sbt 1.12.6 — latest stable sbt 1.x. Same Lightbend/Scala Center
    // backing as sbt 2.0. Chosen because sbt-jupiter-interface plugin
    // resolves cleanly here (on sbt 2.0 milestones the plugin's
    // cross-version isn't published, blocking JUnit 6 test discovery).
    // Bump to "2.0.0" when sbt 2.0 GAs — one-line upgrade, the official
    // starter example works verbatim on both.
    private static final String SBT_VERSION = "1.12.6";
    private static final String ASSEMBLY_PLUGIN_VERSION = "2.3.1";
    private static final String JUPITER_VERSION = "6.0.3";
    private static final String JUPITER_INTERFACE_VERSION = "0.18.0";

    static int run(String[] args) throws IOException {
        Path dir;
        String pkg = "app";
        if (args.length == 0 || args[0].equals(".")) {
            dir = Path.of(".").toAbsolutePath().normalize();
        } else {
            dir = Path.of(args[0]).toAbsolutePath().normalize();
            Files.createDirectories(dir);
        }
        String projectName = dir.getFileName().toString();

        write(dir.resolve("build.sbt"), buildSbt(projectName));
        write(dir.resolve(".gitignore"), gitignore());
        write(dir.resolve("README.md"), readme(projectName));

        Path projectDir = dir.resolve("project");
        Files.createDirectories(projectDir);
        write(projectDir.resolve("build.properties"), "sbt.version=" + SBT_VERSION + "\n");
        write(projectDir.resolve("plugins.sbt"), pluginsSbt());

        Path mainDir = dir.resolve("src/main/java/" + pkg);
        Files.createDirectories(mainDir);
        write(mainDir.resolve("Main.java"), mainJava(pkg));

        Path testDir = dir.resolve("src/test/java/" + pkg);
        Files.createDirectories(testDir);
        write(testDir.resolve("MainTest.java"), mainTestJava(pkg));

        System.out.println("Created project '" + projectName + "' in " + dir);
        System.out.println();
        System.out.println("Next:");
        if (!dir.equals(Path.of(".").toAbsolutePath().normalize())) {
            System.out.println("  cd " + args[0]);
        }
        System.out.println("  zinc build   # compile + assemble");
        System.out.println("  zinc run     # execute");
        System.out.println("  zinc test    # run tests");
        return 0;
    }

    private static void write(Path path, String content) throws IOException {
        Files.writeString(path, content, StandardOpenOption.CREATE_NEW);
    }

    private static String buildSbt(String name) {
        // sbt's DSL is percent-heavy (`%`, `%%`, `%%%`). Java's
        // String.formatted would need every `%` escaped as `%%`, which
        // is noisy and fragile. Token replacement is cleaner.
        //
        // Test block mirrors the official sbt-jupiter-interface
        // starter verbatim — `JupiterKeys.jupiterVersion.value` and
        // `jupiterTestFramework` are provided by the plugin loaded in
        // project/plugins.sbt.
        return """
                ThisBuild / version := "0.1.0-SNAPSHOT"
                ThisBuild / javacOptions ++= Seq("--release", "25")

                lazy val root = (project in file("."))
                  .settings(
                    name := "__NAME__",
                    // Java-only project — drop Scala path suffixes and don't pull
                    // the Scala library into the app.
                    crossPaths := false,
                    autoScalaLibrary := false,
                    Compile / mainClass := Some("app.Main"),
                    assembly / mainClass := Some("app.Main"),
                    assembly / assemblyJarName := "__NAME__.jar",
                    libraryDependencies ++= Seq(
                      // Add runtime dependencies here. Examples:
                      //   "io.javalin" % "javalin" % "6.3.0",
                      //   "com.fasterxml.jackson.core" % "jackson-databind" % "2.18.0",
                      //   "org.slf4j" % "slf4j-api" % "2.0.16",
                      //   "ch.qos.logback" % "logback-classic" % "1.5.12",
                      // JUnit 6 — official sbt-jupiter-interface starter shape.
                      "com.github.sbt.junit" % "jupiter-interface" % JupiterKeys.jupiterVersion.value % Test,
                      "org.junit.jupiter" % "junit-jupiter" % "__JUPITER__" % Test,
                      "org.junit.platform" % "junit-platform-launcher" % "__JUPITER__" % Test
                    ),
                    testOptions += Tests.Argument(jupiterTestFramework, "--display-mode=tree")
                  )
                """
                .replace("__NAME__", name)
                .replace("__JUPITER__", JUPITER_VERSION);
    }

    private static String pluginsSbt() {
        return ("""
                // sbt-assembly produces a fat jar on `zinc build` (sbt assembly).
                addSbtPlugin("com.eed3si9n" % "sbt-assembly" % "__ASM__")

                // sbt-jupiter-interface provides `JupiterKeys.jupiterVersion.value`
                // and the `jupiterTestFramework` value used in build.sbt — the
                // official JUnit 5/6 starter shape.
                addSbtPlugin("com.github.sbt.junit" % "sbt-jupiter-interface" % "__JIF__")
                """).replace("__ASM__", ASSEMBLY_PLUGIN_VERSION)
                   .replace("__JIF__", JUPITER_INTERFACE_VERSION);
    }

    private static String gitignore() {
        return """
                target/
                project/target/
                project/project/
                .idea/
                .vscode/
                *.iml
                .bsp/
                .metals/
                """;
    }

    private static String readme(String name) {
        return "# " + name + "\n\n"
                + "Built with [zinc-java](https://github.com/ZincScale/zinc/tree/master/zinc-java).\n\n"
                + "```bash\n"
                + "zinc build   # compile + assemble\n"
                + "zinc run     # execute\n"
                + "zinc test    # run tests\n"
                + "```\n";
    }

    private static String mainJava(String pkg) {
        return "package " + pkg + ";\n\n"
                + "public final class Main {\n"
                + "    public static void main(String[] args) {\n"
                + "        System.out.println(\"Hello from zinc-java on JDK \" + Runtime.version() + \"!\");\n"
                + "    }\n"
                + "}\n";
    }

    private static String mainTestJava(String pkg) {
        return "package " + pkg + ";\n\n"
                + "import org.junit.jupiter.api.Test;\n"
                + "import static org.junit.jupiter.api.Assertions.assertEquals;\n\n"
                + "final class MainTest {\n"
                + "    @Test\n"
                + "    void sanity() {\n"
                + "        assertEquals(2, 1 + 1);\n"
                + "    }\n"
                + "}\n";
    }
}
