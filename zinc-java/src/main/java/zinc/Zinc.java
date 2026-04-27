package zinc;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/// Thin CLI wrapping sbt 2.0 via its bootstrap launcher
/// (`sbt-launch.jar`, bundled alongside zinc-java). The goal is Java
/// developers never learn sbt's Scala configuration surface for
/// day-to-day work — `zinc init`, `zinc build`, `zinc run`, `zinc test`,
/// `zinc clean` cover the common loop.
///
/// The install script drops `sbt-launch.jar` next to `zinc-java.jar`
/// and passes its path via -Dzinc.sbtLaunch. No external sbt install
/// required; the launcher itself downloads the sbt distribution on
/// first `zinc build` and caches it under ~/.sbt.
public final class Zinc {

    private static final String VERSION = "0.1.0";

    public static void main(String[] args) throws Exception {
        if (args.length == 0) {
            usage();
            System.exit(1);
        }
        String cmd = args[0];
        String[] rest = new String[args.length - 1];
        System.arraycopy(args, 1, rest, 0, rest.length);

        int code = switch (cmd) {
            case "init" -> Init.run(rest);
            case "build" -> sbt("assembly");
            case "run" -> sbt(joinSbt("run", rest));
            case "test" -> rest.length == 0 ? sbt("test") : sbt("testOnly " + String.join(" ", rest));
            case "clean" -> sbt("clean");
            case "shell" -> sbt(); // interactive sbt for power users
            case "doctor" -> { printDoctor(); yield 0; }
            case "version", "--version", "-v" -> { printVersion(); yield 0; }
            case "help", "--help", "-h" -> { usage(); yield 0; }
            default -> {
                System.err.println("zinc: unknown command '" + cmd + "'");
                usage();
                yield 1;
            }
        };
        System.exit(code);
    }

    /// Run sbt via the bundled launcher. Each string becomes one
    /// positional argument to sbt — e.g. `sbt("clean", "compile")` runs
    /// both in a single sbt invocation, which matters for cold-start
    /// cost (one sbt startup, not two).
    private static int sbt(String... sbtCommands) throws IOException, InterruptedException {
        Path launchJar = resolveSbtLaunchJar();
        if (launchJar == null) {
            System.err.println("""
                    zinc: sbt-launch.jar not found.
                    Expected next to zinc-java.jar, but the install looks incomplete.
                    Re-run install.sh from the zinc-java source directory.
                    """);
            return 2;
        }

        List<String> cmd = new ArrayList<>();
        cmd.add("java");
        // sbt's launcher expects standard JVM flags; leave heap at default.
        cmd.add("-jar");
        cmd.add(launchJar.toString());
        for (String s : sbtCommands) cmd.add(s);

        ProcessBuilder pb = new ProcessBuilder(cmd).inheritIO();
        return pb.start().waitFor();
    }

    /// Resolve sbt-launch.jar. Priority:
    ///   1. -Dzinc.sbtLaunch=/path (set by the install-generated wrapper)
    ///   2. $ZINC_SBT_LAUNCH env var
    ///   3. <dir-of-zinc-java.jar>/sbt-launch.jar (convention)
    ///   4. $HOME/.local/lib/sbt-launch.jar (install default)
    private static Path resolveSbtLaunchJar() {
        String prop = System.getProperty("zinc.sbtLaunch");
        if (prop != null && Files.isRegularFile(Path.of(prop))) return Path.of(prop);

        String env = System.getenv("ZINC_SBT_LAUNCH");
        if (env != null && Files.isRegularFile(Path.of(env))) return Path.of(env);

        try {
            Path self = Path.of(Zinc.class.getProtectionDomain().getCodeSource().getLocation().toURI());
            Path sibling = self.getParent().resolve("sbt-launch.jar");
            if (Files.isRegularFile(sibling)) return sibling;
        } catch (Exception ignored) { /* fall through */ }

        String home = System.getProperty("user.home");
        if (home != null) {
            Path fallback = Path.of(home, ".local/lib/sbt-launch.jar");
            if (Files.isRegularFile(fallback)) return fallback;
        }
        return null;
    }

    private static String joinSbt(String first, String[] rest) {
        // For `run` + args, sbt expects a single command line: `run arg1 arg2`.
        if (rest.length == 0) return first;
        StringBuilder sb = new StringBuilder(first);
        for (String r : rest) sb.append(' ').append(r);
        return sb.toString();
    }

    /// First-run diagnostic. Walks the toolchain resolution chain and
    /// surfaces what zinc actually sees: JDK path + version, sbt
    /// launcher location, whether we're inside a zinc project right
    /// now, and what sbt version the project pins (if any).
    ///
    /// Mirrors zinc-csharp's {@code doctor} command — intended as
    /// the first thing a user runs when something looks wrong.
    private static void printDoctor() {
        System.out.println("zinc-java diagnostics");
        System.out.println();

        // JDK
        String javaHome = System.getProperty("java.home");
        String javaVer = System.getProperty("java.version");
        String vmName = System.getProperty("java.vm.name");
        System.out.println("  java: " + javaVer + " (" + vmName + ")");
        System.out.println("        " + javaHome);

        // zinc-java.jar location
        try {
            Path self = Path.of(Zinc.class.getProtectionDomain().getCodeSource().getLocation().toURI());
            System.out.println("  zinc-java.jar: " + self);
        } catch (Exception ex) {
            System.out.println("  zinc-java.jar: (unresolved — " + ex.getClass().getSimpleName() + ")");
        }

        // sbt launcher
        Path launchJar = resolveSbtLaunchJar();
        if (launchJar == null) {
            System.out.println("  sbt-launch.jar: NOT FOUND — install.sh needed");
        } else {
            System.out.println("  sbt-launch.jar: " + launchJar);
        }

        // Project context (are we inside a zinc-scaffolded repo?)
        Path buildSbt = Path.of(System.getProperty("user.dir"), "build.sbt");
        Path buildProps = Path.of(System.getProperty("user.dir"), "project", "build.properties");
        if (Files.isRegularFile(buildSbt)) {
            System.out.println("  project: " + buildSbt.getParent());
            System.out.println("  build.sbt: " + buildSbt);
            if (Files.isRegularFile(buildProps)) {
                try {
                    String pinned = Files.readString(buildProps).lines()
                            .map(String::trim)
                            .filter(l -> l.startsWith("sbt.version="))
                            .findFirst().orElse("sbt.version=?");
                    System.out.println("  " + pinned + " (from project/build.properties)");
                } catch (IOException ex) {
                    System.out.println("  project/build.properties: read failed — " + ex.getMessage());
                }
            } else {
                System.out.println("  project/build.properties: missing");
            }
        } else {
            System.out.println("  project: (not in a zinc project — run 'zinc init <name>' to scaffold)");
        }

        // sbt + ivy caches — useful when builds act weird
        String home = System.getProperty("user.home");
        if (home != null) {
            Path sbtCache = Path.of(home, ".sbt");
            Path ivyCache = Path.of(home, ".ivy2");
            System.out.println("  ~/.sbt:  " + (Files.isDirectory(sbtCache) ? "present" : "(not yet populated)"));
            System.out.println("  ~/.ivy2: " + (Files.isDirectory(ivyCache) ? "present" : "(not yet populated)"));
        }
    }

    private static void printVersion() {
        System.out.println("zinc-java " + VERSION);
        Path launchJar = resolveSbtLaunchJar();
        if (launchJar == null) {
            System.out.println("sbt      (launcher missing — reinstall)");
            return;
        }
        System.out.println("sbt      launcher at " + launchJar);
        System.out.println("         sbt version is project-specified via project/build.properties");
    }

    private static void usage() {
        System.out.println("""
                zinc — build tool for Java projects (wraps sbt 2.0, launcher bundled).

                Usage: zinc <command> [args]

                Commands:
                  init [name]    Scaffold a new Java project in ./<name> (or current dir if omitted)
                  build          Compile + assemble a fat jar (sbt assembly)
                  run [args]     Run the main class; extra args are passed through
                  test [pattern] Run tests (optional JUnit filter pattern)
                  clean          Remove build outputs
                  shell          Drop into interactive sbt (for power users)
                  doctor         Check toolchain and project status
                  version        Print zinc + sbt launcher paths
                  help           Show this message

                sbt itself is downloaded by the bundled launcher on first `zinc build`,
                using the version pinned in project/build.properties. No external
                install required.

                The generated build.sbt is plain sbt Scala — edit it directly to add
                library dependencies. zinc never rewrites it after init.
                """);
    }
}
