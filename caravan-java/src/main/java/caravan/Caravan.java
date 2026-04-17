package caravan;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/// Thin CLI wrapping sbt 2.0 via its bootstrap launcher
/// (`sbt-launch.jar`, bundled alongside caravan-java). The goal is Java
/// developers never learn sbt's Scala configuration surface for
/// day-to-day work — `caravan init`, `caravan build`, `caravan run`, `caravan test`,
/// `caravan clean` cover the common loop.
///
/// The install script drops `sbt-launch.jar` next to `caravan-java.jar`
/// and passes its path via -Dcaravan.sbtLaunch. No external sbt install
/// required; the launcher itself downloads the sbt distribution on
/// first `caravan build` and caches it under ~/.sbt.
public final class Caravan {

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
            case "version", "--version", "-v" -> { printVersion(); yield 0; }
            case "help", "--help", "-h" -> { usage(); yield 0; }
            default -> {
                System.err.println("caravan: unknown command '" + cmd + "'");
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
                    caravan: sbt-launch.jar not found.
                    Expected next to caravan-java.jar, but the install looks incomplete.
                    Re-run install.sh from the caravan-java source directory.
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
    ///   1. -Dcaravan.sbtLaunch=/path (set by the install-generated wrapper)
    ///   2. $CARAVAN_SBT_LAUNCH env var
    ///   3. <dir-of-caravan-java.jar>/sbt-launch.jar (convention)
    ///   4. $HOME/.local/lib/sbt-launch.jar (install default)
    private static Path resolveSbtLaunchJar() {
        String prop = System.getProperty("caravan.sbtLaunch");
        if (prop != null && Files.isRegularFile(Path.of(prop))) return Path.of(prop);

        String env = System.getenv("CARAVAN_SBT_LAUNCH");
        if (env != null && Files.isRegularFile(Path.of(env))) return Path.of(env);

        try {
            Path self = Path.of(Caravan.class.getProtectionDomain().getCodeSource().getLocation().toURI());
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

    private static void printVersion() {
        System.out.println("caravan-java " + VERSION);
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
                caravan — build tool for Java projects (wraps sbt 2.0, launcher bundled).

                Usage: caravan <command> [args]

                Commands:
                  init [name]    Scaffold a new Java project in ./<name> (or current dir if omitted)
                  build          Compile + assemble a fat jar (sbt assembly)
                  run [args]     Run the main class; extra args are passed through
                  test [pattern] Run tests (optional JUnit filter pattern)
                  clean          Remove build outputs
                  shell          Drop into interactive sbt (for power users)
                  version        Print caravan + sbt launcher paths
                  help           Show this message

                sbt itself is downloaded by the bundled launcher on first `caravan build`,
                using the version pinned in project/build.properties. No external
                install required.

                The generated build.sbt is plain sbt Scala — edit it directly to add
                library dependencies. caravan never rewrites it after init.
                """);
    }
}
