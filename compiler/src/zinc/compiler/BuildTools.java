// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;

/**
 * External build tool integration: Mill, native-image, jpackage, Docker.
 */
public class BuildTools {

    // --- Mill -----------------------------------------------------------------

    /** Find the project root containing build.mill.yaml. */
    static Path findProjectDir(Path dir) {
        var current = dir.toAbsolutePath();
        while (current != null) {
            if (Files.exists(current.resolve("build.mill.yaml"))) return current;
            current = current.getParent();
        }
        return null;
    }

    /** Run a Mill command in the project directory. Uses bundled Mill if available. */
    static int runMill(Path projectDir, String command) {
        try {
            var millPath = findBundledMill();
            List<String> cmd;
            if (millPath != null) {
                cmd = List.of(millPath.toString(), command);
            } else {
                cmd = List.of("mill", command);
            }
            var process = new ProcessBuilder(cmd)
                .directory(projectDir.toFile())
                .inheritIO()
                .start();
            return process.waitFor();
        } catch (Exception e) {
            System.err.println("failed to run mill: " + e.getMessage());
            System.err.println("install mill: curl -L https://raw.githubusercontent.com/com-lihaoyi/mill/main/mill > mill && chmod +x mill");
            return 1;
        }
    }

    /** Find bundled Mill launcher relative to zinc binary location. */
    private static Path findBundledMill() {
        try {
            var zincDir = Path.of(Main.class.getProtectionDomain().getCodeSource().getLocation().toURI()).getParent();
            for (var candidate : List.of(
                zincDir.resolve("mill"),
                zincDir.resolve("lib/mill"),
                zincDir.resolve("../lib/mill"))) {
                if (Files.exists(candidate)) return candidate;
            }
        } catch (Exception e) { /* ignore */ }

        var jarDir = Path.of(System.getProperty("user.dir"));
        var candidate = jarDir.resolve("lib/mill");
        if (Files.exists(candidate)) return candidate;

        return null;
    }

    // --- native-image ---------------------------------------------------------

    /** Build native binary from a classpath directory. */
    static int runNativeImage(String mainClass, Path classDir) {
        var outputName = mainClass.toLowerCase();
        try {
            var cmd = List.of(
                "native-image", "--enable-preview",
                "-cp", classDir.toString(),
                "-o", classDir.resolve(outputName).toString(),
                "--no-fallback", "-O2", "-march=native",
                mainClass);
            var process = new ProcessBuilder(cmd).inheritIO().start();
            int exitCode = process.waitFor();
            if (exitCode == 0) {
                System.out.println("native binary: " + classDir.resolve(outputName));
            }
            return exitCode;
        } catch (Exception e) {
            System.err.println("native-image not found. Install GraalVM JDK 25.");
            return 1;
        }
    }

    /** Build native binary from a Mill project using mill classpath + native-image. */
    static int runNativeImage(Path projectDir, Path outDir) {
        try {
            var cpProcess = new ProcessBuilder("mill", "show", "runClasspath")
                .directory(projectDir.toFile())
                .redirectErrorStream(true)
                .start();
            var cpOutput = new String(cpProcess.getInputStream().readAllBytes());
            cpProcess.waitFor();

            var cpEntries = new ArrayList<String>();
            for (var line : cpOutput.split("[,\\[\\]\"]")) {
                line = line.trim();
                if (line.startsWith("ref:") || line.startsWith("qref:")) {
                    var path = line.substring(line.lastIndexOf(':') + 1);
                    if (Files.exists(Path.of(path))) cpEntries.add(path);
                }
            }
            var classesDir = projectDir.resolve("out/compile.dest/classes");
            if (Files.exists(classesDir)) cpEntries.addFirst(classesDir.toString());

            if (cpEntries.isEmpty()) {
                System.err.println("error: could not determine classpath from Mill");
                return 1;
            }

            var classpath = String.join(":", cpEntries);

            String mainClass = "Main";
            var buildYaml = projectDir.resolve("build.mill.yaml");
            if (Files.exists(buildYaml)) {
                for (var line : Files.readAllLines(buildYaml)) {
                    if (line.trim().startsWith("mainClass:")) {
                        mainClass = line.trim().substring("mainClass:".length()).trim();
                        break;
                    }
                }
            }

            var binaryName = projectDir.getFileName().toString().toLowerCase().replace("-", "");

            var metadataArgs = NativeImageConfig.buildNativeImageArgs(cpEntries);

            var cmd = new ArrayList<>(List.of(
                "native-image", "--enable-preview",
                "-cp", classpath,
                "-o", projectDir.resolve(binaryName).toString(),
                "--no-fallback", "-O2", "-march=native"));
            cmd.addAll(metadataArgs);

            if (metadataArgs.isEmpty() && cpEntries.size() > 1) {
                System.out.println("no bundled metadata — running tracing agent...");
                var tracingDir = NativeImageConfig.runTracingAgent(mainClass, classpath, projectDir);
                if (tracingDir != null) {
                    cmd.add("-H:ConfigurationFileDirectories=" + tracingDir);
                }
            }

            cmd.add(mainClass);

            System.out.println("native-image: " + mainClass + " → " + binaryName);
            var process = new ProcessBuilder(cmd).inheritIO().start();
            int exitCode = process.waitFor();
            if (exitCode == 0) {
                var binary = projectDir.resolve(binaryName);
                System.out.println("native binary: " + binary + " (" +
                    Files.size(binary) / 1024 / 1024 + "MB)");
            }
            return exitCode;
        } catch (Exception e) {
            System.err.println("native-image failed: " + e.getMessage());
            return 1;
        }
    }

    // --- jpackage + jlink ----------------------------------------------------

    static int buildPackagedApp(Path projectDir) {
        try {
            runMill(projectDir, "assembly");
            var fatJar = projectDir.resolve("out/assembly.dest/out.jar");
            if (!Files.exists(fatJar)) {
                System.err.println("error: fat jar not found at " + fatJar);
                return 1;
            }

            var jdepsProcess = new ProcessBuilder(
                "jdeps", "--print-module-deps", "--ignore-missing-deps",
                "--multi-release", "25", fatJar.toString())
                .redirectErrorStream(true).start();
            var modules = new String(jdepsProcess.getInputStream().readAllBytes()).trim();
            jdepsProcess.waitFor();

            if (modules.isEmpty() || modules.contains("Error")) {
                modules = "java.base,java.desktop,java.instrument,java.management,java.naming,java.security.jgss,java.sql";
            }
            System.out.println("modules: " + modules);

            String mainClass = "Main";
            var buildYaml = projectDir.resolve("build.mill.yaml");
            if (Files.exists(buildYaml)) {
                for (var line : Files.readAllLines(buildYaml)) {
                    if (line.trim().startsWith("mainClass:"))
                        mainClass = line.trim().substring("mainClass:".length()).trim();
                }
            }

            var appName = projectDir.getFileName().toString().toLowerCase().replace("-", "");
            var cmd = List.of(
                "jpackage",
                "--type", "app-image",
                "--name", appName,
                "--input", fatJar.getParent().toString(),
                "--main-jar", fatJar.getFileName().toString(),
                "--main-class", mainClass,
                "--dest", projectDir.resolve("dist").toString(),
                "--add-modules", modules,
                "--java-options", "--enable-preview",
                "--jlink-options", "--strip-debug --no-man-pages --no-header-files --compress=zip-6");

            System.out.println("jpackage: " + appName);
            var process = new ProcessBuilder(cmd).inheritIO().start();
            int exitCode = process.waitFor();
            if (exitCode == 0) {
                var dist = projectDir.resolve("dist/" + appName);
                System.out.println("packaged app: " + dist);
                try (var walk = Files.walk(dist)) {
                    long totalSize = walk.filter(Files::isRegularFile).mapToLong(p -> {
                        try { return Files.size(p); } catch (IOException e) { return 0; }
                    }).sum();
                    System.out.println("total size: " + totalSize / 1024 / 1024 + "MB");
                }
            }
            return exitCode;
        } catch (Exception e) {
            System.err.println("jpackage failed: " + e.getMessage());
            return 1;
        }
    }

    // --- Docker ---------------------------------------------------------------

    static int buildDocker(Path projectDir) {
        try {
            runMill(projectDir, "assembly");
            var fatJar = projectDir.resolve("out/assembly.dest/out.jar");

            var appName = projectDir.getFileName().toString().toLowerCase();

            String mainClass = "Main";
            var buildYaml = projectDir.resolve("build.mill.yaml");
            if (Files.exists(buildYaml)) {
                for (var line : Files.readAllLines(buildYaml)) {
                    if (line.trim().startsWith("mainClass:"))
                        mainClass = line.trim().substring("mainClass:".length()).trim();
                }
            }

            var jdepsProcess = new ProcessBuilder(
                "jdeps", "--print-module-deps", "--ignore-missing-deps",
                "--multi-release", "25", fatJar.toString())
                .redirectErrorStream(true).start();
            var modules = new String(jdepsProcess.getInputStream().readAllBytes()).trim();
            jdepsProcess.waitFor();
            if (modules.isEmpty() || modules.contains("Error")) {
                modules = "java.base,java.desktop,java.instrument,java.management,java.naming,java.security.jgss,java.sql";
            }

            var dockerfile = projectDir.resolve("Dockerfile");
            Files.writeString(dockerfile, """
                # Stage 1: Build minimal JRE with jlink (JDK only needed here)
                FROM eclipse-temurin:25-jdk-alpine AS jre-build
                RUN jlink --add-modules %s \\
                    --strip-debug --no-man-pages --no-header-files --compress=zip-6 \\
                    --output /custom-jre

                # Stage 2: Distroless base — no shell, no package manager, minimal attack surface
                FROM gcr.io/distroless/base-nossl-debian12:nonroot
                COPY --from=jre-build /custom-jre /jre
                WORKDIR /app
                COPY %s app.jar
                EXPOSE 8080
                ENTRYPOINT ["/jre/bin/java", "--enable-preview", "-jar", "app.jar"]
                """.formatted(modules, fatJar.getFileName()));

            var cmd = List.of("docker", "build", "-t", appName, "-f", dockerfile.toString(), fatJar.getParent().toString());
            System.out.println("docker build: " + appName);
            var process = new ProcessBuilder(cmd).inheritIO().start();
            return process.waitFor();
        } catch (Exception e) {
            System.err.println("docker build failed: " + e.getMessage());
            return 1;
        }
    }
}
