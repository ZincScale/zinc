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
import java.util.Map;

/**
 * Zinc compiler CLI.
 * Usage:
 *   zinc build <file.zn|dir> [-o outdir]
 *   zinc run <file.zn|dir> [args...]
 */
public class Main {

    public static void main(String[] args) {
        if (args.length == 0) {
            printUsage();
            System.exit(1);
        }

        String command = args[0];
        var rest = List.of(args).subList(1, args.length);

        switch (command) {
            case "build" -> cmdBuild(rest);
            case "run" -> cmdRun(rest);
            case "init" -> cmdInit(rest);
            default -> {
                // No subcommand — treat as file to compile (backwards compat)
                if (command.endsWith(".zn")) {
                    cmdBuild(List.of(args));
                } else {
                    System.err.println("unknown command: " + command);
                    printUsage();
                    System.exit(1);
                }
            }
        }
    }

    private static void printUsage() {
        System.err.println("usage: zinc <command> [args]");
        System.err.println("commands:");
        System.err.println("  build <file.zn|dir> [-o outdir]   compile to Java");
        System.err.println("  run <file.zn|dir> [args...]       compile and run");
        System.err.println("  init <name>                       create a new project");
    }

    // --- build ---------------------------------------------------------------

    private static void cmdBuild(List<String> args) {
        String input = null;
        Path outDir = null;
        boolean nativeImage = false;
        boolean fatJar = false;
        boolean packageApp = true; // default: jpackage + jlink (works with any library)
        boolean docker = false;

        for (int i = 0; i < args.size(); i++) {
            switch (args.get(i)) {
                case "-o" -> { if (i + 1 < args.size()) outDir = Path.of(args.get(++i)); }
                case "--native" -> { nativeImage = true; packageApp = false; }
                case "--fat-jar" -> { fatJar = true; packageApp = false; }
                case "--package" -> { packageApp = true; nativeImage = false; }
                case "--docker" -> { docker = true; packageApp = false; }
                case "--no-package" -> packageApp = false;
                default -> { if (!args.get(i).startsWith("-")) input = args.get(i); }
            }
        }

        if (input == null) {
            System.err.println("error: no input file or directory");
            System.exit(1);
        }

        var inputPath = Path.of(input);
        if (outDir == null) {
            outDir = Path.of("/tmp/zinc-build-" + inputPath.getFileName().toString().replace(".zn", ""));
        }

        // Check for Mill project
        Path projectDir = null;
        if (Files.isDirectory(inputPath)) {
            projectDir = findProjectDir(inputPath);
        }

        var result = compileProject(inputPath, outDir);
        switch (result) {
            case Result.Ok<List<Path>> ok -> {
                for (var f : ok.value()) System.out.println("compiled: " + f);
            }
            case Result.Err<List<Path>> err -> {
                for (var e : err.errors()) System.err.println("error: " + e);
                System.exit(1);
            }
        }

        // If Mill project, run mill compile after transpilation
        if (projectDir != null) {
            System.out.println("mill compile");
            int exitCode = runMill(projectDir, "compile");
            if (exitCode != 0) System.exit(exitCode);

            if (nativeImage) {
                // Build native binary via native-image
                System.out.println("building native image...");
                exitCode = runNativeImage(projectDir, outDir);
                if (exitCode != 0) System.exit(exitCode);
            }

            // Fat jar
            if (fatJar) {
                System.out.println("building fat jar...");
                runMill(projectDir, "assembly");
            }

            // Packaged app (jpackage + jlink = bundled JVM)
            if (packageApp) {
                System.out.println("building packaged app...");
                int pkgExit = buildPackagedApp(projectDir);
                if (pkgExit != 0) System.exit(pkgExit);
            }

            // Docker
            if (docker) {
                System.out.println("building Docker image...");
                int dkrExit = buildDocker(projectDir);
                if (dkrExit != 0) System.exit(dkrExit);
            }

            System.out.println("build complete: " + projectDir + " (Mill project)");
        } else if (nativeImage) {
            // Single-file native build: javac first, then native-image
            var javacResult = runJavac(result.unwrap(), outDir);
            if (javacResult.isErr()) {
                for (var e : ((Result.Err<?>) javacResult).errors()) System.err.println(e);
                System.exit(1);
            }
            String mainClass = findMainClass(result.unwrap(), outDir);
            if (mainClass != null) {
                System.out.println("building native image...");
                int exitCode = runNativeImage(mainClass, outDir);
                if (exitCode != 0) System.exit(exitCode);
            }
        }
    }

    // --- run -----------------------------------------------------------------

    private static void cmdRun(List<String> args) {
        if (args.isEmpty()) {
            System.err.println("error: no input file");
            System.exit(1);
        }

        String input = args.getFirst();
        var runArgs = args.size() > 1 ? args.subList(1, args.size()) : List.<String>of();
        var inputPath = Path.of(input);

        // Check for Mill project (directory with build.mill.yaml)
        if (Files.isDirectory(inputPath)) {
            var projectDir = findProjectDir(inputPath);
            if (projectDir != null) {
                // Mill project: transpile → mill run
                var outDir = inputPath; // transpile in-place (Mill expects src/)
                var compileResult = compileProject(inputPath, outDir);
                if (compileResult.isErr()) {
                    for (var e : ((Result.Err<?>) compileResult).errors()) System.err.println("error: " + e);
                    System.exit(1);
                }
                System.out.println("mill run");
                System.exit(runMill(projectDir, "run"));
                return;
            }
        }

        // Script/single-file mode: compile → javac → java
        var outDir = Path.of("/tmp/zinc-run-" + inputPath.getFileName().toString().replace(".zn", ""));

        var compileResult = compileProject(inputPath, outDir);
        if (compileResult.isErr()) {
            for (var e : ((Result.Err<?>) compileResult).errors()) System.err.println("error: " + e);
            System.exit(1);
        }
        var javaFiles = compileResult.unwrap();

        var javacResult = runJavac(javaFiles, outDir);
        if (javacResult.isErr()) {
            for (var e : ((Result.Err<?>) javacResult).errors()) System.err.println(e);
            System.exit(1);
        }

        String mainClass = findMainClass(javaFiles, outDir);
        if (mainClass == null) {
            System.err.println("error: no main class found");
            System.exit(1);
        }

        System.exit(runJava(mainClass, outDir, runArgs));
    }

    // --- Compilation pipeline ------------------------------------------------

    /**
     * Compile a .zn file or directory of .zn files to .java files.
     * Multi-file: two-pass (collect signatures, then transform with cross-file knowledge).
     */
    public static Result<List<Path>> compileProject(Path input, Path outDir) {
        List<Path> znFiles = new ArrayList<>();

        if (Files.isDirectory(input)) {
            try (var stream = Files.walk(input)) {
                stream.filter(p -> p.toString().endsWith(".zn"))
                    .forEach(znFiles::add);
            } catch (IOException e) {
                return Result.err("cannot scan directory " + input + ": " + e.getMessage());
            }
        } else {
            znFiles.add(input);
        }

        if (znFiles.isEmpty()) {
            return Result.err("no .zn files found in " + input);
        }

        // Single file — simple path
        if (znFiles.size() == 1) {
            return compileSingleFile(znFiles.getFirst(), outDir);
        }

        // Multi-file: parse all, collect cross-file info, then transform
        record ParsedFile(Path path, String className, Ast.Program program) {}
        var parsed = new ArrayList<ParsedFile>();
        var allInterfaces = new java.util.HashSet<String>();
        var allClasses = new java.util.HashSet<String>();

        // Pass 1: parse all files, collect interface/class names
        for (var znFile : znFiles) {
            String source;
            try { source = Files.readString(znFile); }
            catch (IOException e) { return Result.err("cannot read " + znFile + ": " + e.getMessage()); }

            String className = capitalize(znFile.getFileName().toString().replace(".zn", ""));

            // Infer package from directory relative to source root
            String pkg = null;
            var sourceRoot = Files.isDirectory(input) ? input : input.getParent();
            var relative = sourceRoot.toAbsolutePath().relativize(znFile.toAbsolutePath().getParent());
            if (relative.getNameCount() > 0 && !relative.toString().isEmpty()) {
                pkg = relative.toString().replace("/", ".").replace("\\", ".");
            }

            var lexResult = new Lexer(source).tokenize();
            if (lexResult.isErr()) return Result.err(((Result.Err<?>) lexResult).errors());

            var parser = new Parser(lexResult.unwrap());
            var parseResult = parser.parseResult();
            if (parseResult.isErr()) return Result.err(((Result.Err<?>) parseResult).errors());
            var program = parseResult.unwrap();

            // Set inferred package if not declared in source
            if (program.pkg() == null && pkg != null) {
                program = new Ast.Program(program.sourceFile(), new Ast.PackageDecl(pkg),
                    program.imports(), program.decls(), program.stmts());
            }

            parsed.add(new ParsedFile(znFile, className, program));

            for (var decl : program.decls()) {
                switch (decl) {
                    case Ast.InterfaceDecl iface -> allInterfaces.add(iface.name());
                    case Ast.ClassDecl cls -> allClasses.add(cls.name());
                    case Ast.DataClassDecl data -> allClasses.add(data.name());
                    case Ast.SealedClassDecl sealed -> {
                        allClasses.add(sealed.name());
                        for (var v : sealed.variants()) allClasses.add(v.name());
                    }
                    default -> {}
                }
            }
        }

        // Pass 2: transform each file with cross-file interface knowledge
        var allJavaFiles = new ArrayList<Path>();
        var emitter = new Emitter();

        for (var pf : parsed) {
            var typeChecker = new TypeChecker();
            var typeResult = typeChecker.check(pf.program());
            var resolvedTypes = typeResult.isOk() ? typeResult.unwrap() : Map.<String, TypeInfo>of();

            var transformer = new Transformer(pf.className(), resolvedTypes);
            // Register cross-file interfaces
            for (var ifaceName : allInterfaces) transformer.registerInterface(ifaceName);

            var transformResult = transformer.transformAll(pf.program());
            if (transformResult.isErr()) return Result.err(((Result.Err<?>) transformResult).errors());

            for (var cu : transformResult.unwrap()) {
                var emitResult = emitter.emit(cu, outDir);
                if (emitResult.isErr()) return Result.err(((Result.Err<?>) emitResult).errors());
                allJavaFiles.add(emitResult.unwrap());
            }
        }

        return Result.ok(allJavaFiles);
    }

    /**
     * Compile a single .zn file to one or more .java files.
     */
    public static Result<List<Path>> compileSingleFile(Path inputFile, Path outDir) {
        String source;
        try {
            source = Files.readString(inputFile);
        } catch (IOException e) {
            return Result.err("cannot read " + inputFile + ": " + e.getMessage());
        }

        String fileName = inputFile.getFileName().toString();
        String className = capitalize(fileName.replace(".zn", ""));

        // Lex
        var lexResult = new Lexer(source).tokenize();
        if (lexResult.isErr()) return Result.err(((Result.Err<?>) lexResult).errors());
        var tokens = lexResult.unwrap();

        // Parse
        var parser = new Parser(tokens);
        var parseResult = parser.parseResult();
        if (parseResult.isErr()) return Result.err(((Result.Err<?>) parseResult).errors());
        var program = parseResult.unwrap();

        // Typecheck
        var typeChecker = new TypeChecker();
        var typeResult = typeChecker.check(program);
        var resolvedTypes = typeResult.isOk() ? typeResult.unwrap() : Map.<String, TypeInfo>of();

        // Transform
        var transformer = new Transformer(className, resolvedTypes);
        var transformResult = transformer.transformAll(program);
        if (transformResult.isErr()) return Result.err(((Result.Err<?>) transformResult).errors());
        var units = transformResult.unwrap();

        // Emit
        var emitter = new Emitter();
        var javaFiles = new ArrayList<Path>();
        for (var cu : units) {
            var emitResult = emitter.emit(cu, outDir);
            if (emitResult.isErr()) return Result.err(((Result.Err<?>) emitResult).errors());
            javaFiles.add(emitResult.unwrap());
        }

        return Result.ok(javaFiles);
    }

    // --- init ----------------------------------------------------------------

    private static void cmdInit(List<String> args) {
        if (args.isEmpty()) {
            System.err.println("usage: zinc init <project-name>");
            System.exit(1);
        }

        String name = args.getFirst();
        var dir = Path.of(name);

        try {
            Files.createDirectories(dir.resolve("src"));
            Files.createDirectories(dir.resolve("test"));

            // build.mill.yaml
            Files.writeString(dir.resolve("build.mill.yaml"), """
                # %s — Zinc project
                extends: JavaModule
                jvmVersion: 25

                javacOptions:
                  - --enable-preview
                  - --release
                  - "25"

                forkArgs:
                  - --enable-preview

                mainClass: Main

                mvnDeps: []
                """.formatted(name).stripIndent());

            // src/main.zn
            Files.writeString(dir.resolve("src/main.zn"), """
                fn main() {
                    print("Hello from %s!")
                }
                """.formatted(name).stripIndent());

            // .gitignore
            Files.writeString(dir.resolve(".gitignore"), """
                out/
                *.class
                .mill-*
                """.stripIndent());

            System.out.println("created project: " + name);
            System.out.println("  " + dir + "/src/main.zn");
            System.out.println("  " + dir + "/build.mill.yaml");
            System.out.println("\nrun: zinc run " + name + "/src");

        } catch (IOException e) {
            System.err.println("error: " + e.getMessage());
            System.exit(1);
        }
    }

    // --- Mill integration ----------------------------------------------------

    /** Find the project root containing build.mill.yaml. */
    private static Path findProjectDir(Path dir) {
        var current = dir.toAbsolutePath();
        while (current != null) {
            if (Files.exists(current.resolve("build.mill.yaml"))) return current;
            current = current.getParent();
        }
        return null;
    }

    /** Run a Mill command in the project directory. */
    private static int runMill(Path projectDir, String command) {
        try {
            var process = new ProcessBuilder("mill", command)
                .directory(projectDir.toFile())
                .inheritIO()
                .start();
            return process.waitFor();
        } catch (Exception e) {
            System.err.println("failed to run mill: " + e.getMessage());
            return 1;
        }
    }

    // --- native-image --------------------------------------------------------

    /** Build native binary from a classpath directory. */
    private static int runNativeImage(String mainClass, Path classDir) {
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
    private static int runNativeImage(Path projectDir, Path outDir) {
        // Get classpath from Mill
        try {
            var cpProcess = new ProcessBuilder("mill", "show", "runClasspath")
                .directory(projectDir.toFile())
                .redirectErrorStream(true)
                .start();
            var cpOutput = new String(cpProcess.getInputStream().readAllBytes());
            cpProcess.waitFor();

            // Parse classpath entries from Mill output
            var cpEntries = new ArrayList<String>();
            for (var line : cpOutput.split("[,\\[\\]\"]")) {
                line = line.trim();
                if (line.startsWith("ref:") || line.startsWith("qref:")) {
                    // Extract path after the last colon
                    var path = line.substring(line.lastIndexOf(':') + 1);
                    if (Files.exists(Path.of(path))) cpEntries.add(path);
                }
            }
            // Add compiled classes
            var classesDir = projectDir.resolve("out/compile.dest/classes");
            if (Files.exists(classesDir)) cpEntries.addFirst(classesDir.toString());

            if (cpEntries.isEmpty()) {
                System.err.println("error: could not determine classpath from Mill");
                return 1;
            }

            var classpath = String.join(":", cpEntries);

            // Find main class from build.mill.yaml
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

            // Derive binary name from project directory
            var binaryName = projectDir.getFileName().toString().toLowerCase().replace("-", "");

            // Get reachability metadata for dependencies
            var metadataArgs = NativeImageConfig.buildNativeImageArgs(cpEntries);

            var cmd = new ArrayList<>(List.of(
                "native-image", "--enable-preview",
                "-cp", classpath,
                "-o", projectDir.resolve(binaryName).toString(),
                "--no-fallback", "-O2", "-march=native"));
            cmd.addAll(metadataArgs);

            // Run tracing agent if no metadata found for some deps
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

    /**
     * Build a packaged app with bundled minimal JVM via jpackage.
     * jdeps detects modules → jpackage bundles only what's needed.
     */
    private static int buildPackagedApp(Path projectDir) {
        try {
            // Step 1: Build fat jar via Mill
            runMill(projectDir, "assembly");
            var fatJar = projectDir.resolve("out/assembly.dest/out.jar");
            if (!Files.exists(fatJar)) {
                System.err.println("error: fat jar not found at " + fatJar);
                return 1;
            }

            // Step 2: Detect required modules via jdeps
            var jdepsProcess = new ProcessBuilder(
                "jdeps", "--print-module-deps", "--ignore-missing-deps",
                "--multi-release", "25", fatJar.toString())
                .redirectErrorStream(true).start();
            var modules = new String(jdepsProcess.getInputStream().readAllBytes()).trim();
            jdepsProcess.waitFor();

            if (modules.isEmpty() || modules.contains("Error")) {
                // Fallback: common modules for server apps
                modules = "java.base,java.desktop,java.instrument,java.management,java.naming,java.security.jgss,java.sql";
            }
            System.out.println("modules: " + modules);

            // Step 3: Find main class
            String mainClass = "Main";
            var buildYaml = projectDir.resolve("build.mill.yaml");
            if (Files.exists(buildYaml)) {
                for (var line : Files.readAllLines(buildYaml)) {
                    if (line.trim().startsWith("mainClass:"))
                        mainClass = line.trim().substring("mainClass:".length()).trim();
                }
            }

            // Step 4: jpackage
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
                // Show size
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

    private static int buildDocker(Path projectDir) {
        try {
            // Build fat jar first
            runMill(projectDir, "assembly");
            var fatJar = projectDir.resolve("out/assembly.dest/out.jar");

            var appName = projectDir.getFileName().toString().toLowerCase();

            // Find main class
            String mainClass = "Main";
            var buildYaml = projectDir.resolve("build.mill.yaml");
            if (Files.exists(buildYaml)) {
                for (var line : Files.readAllLines(buildYaml)) {
                    if (line.trim().startsWith("mainClass:"))
                        mainClass = line.trim().substring("mainClass:".length()).trim();
                }
            }

            // Detect modules for jlink
            var jdepsProcess = new ProcessBuilder(
                "jdeps", "--print-module-deps", "--ignore-missing-deps",
                "--multi-release", "25", fatJar.toString())
                .redirectErrorStream(true).start();
            var modules = new String(jdepsProcess.getInputStream().readAllBytes()).trim();
            jdepsProcess.waitFor();
            if (modules.isEmpty() || modules.contains("Error")) {
                modules = "java.base,java.desktop,java.instrument,java.management,java.naming,java.security.jgss,java.sql";
            }

            // Generate multi-stage Dockerfile: jlink JRE on distroless
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

            // Build
            var cmd = List.of("docker", "build", "-t", appName, "-f", dockerfile.toString(), fatJar.getParent().toString());
            System.out.println("docker build: " + appName);
            var process = new ProcessBuilder(cmd).inheritIO().start();
            return process.waitFor();
        } catch (Exception e) {
            System.err.println("docker build failed: " + e.getMessage());
            return 1;
        }
    }

    // --- javac ----------------------------------------------------------------

    private static Result<Void> runJavac(List<Path> javaFiles, Path outDir) {
        var cmd = new ArrayList<String>();
        cmd.add("javac");
        cmd.add("--enable-preview");
        cmd.add("--source");
        cmd.add("25");
        cmd.add("-d");
        cmd.add(outDir.toString());
        for (var f : javaFiles) cmd.add(f.toString());

        try {
            var process = new ProcessBuilder(cmd)
                .redirectErrorStream(true)
                .start();
            var output = new String(process.getInputStream().readAllBytes());
            int exitCode = process.waitFor();
            if (exitCode != 0) {
                return Result.err("javac failed:\n" + output);
            }
            return Result.ok(null);
        } catch (Exception e) {
            return Result.err("failed to run javac: " + e.getMessage());
        }
    }

    // --- java ----------------------------------------------------------------

    private static int runJava(String mainClass, Path classDir, List<String> args) {
        var cmd = new ArrayList<String>();
        cmd.add("java");
        cmd.add("--enable-preview");
        cmd.add("-cp");
        cmd.add(classDir.toString());
        cmd.add(mainClass);
        cmd.addAll(args);

        try {
            var process = new ProcessBuilder(cmd)
                .inheritIO()
                .start();
            return process.waitFor();
        } catch (Exception e) {
            System.err.println("failed to run java: " + e.getMessage());
            return 1;
        }
    }

    // --- Helpers --------------------------------------------------------------

    private static String findMainClass(List<Path> javaFiles, Path outDir) {
        for (var f : javaFiles) {
            try {
                var content = Files.readString(f);
                if (content.contains("public static void main(")) {
                    // Extract class name from file
                    var name = f.getFileName().toString().replace(".java", "");
                    // Include package if present
                    var relative = outDir.relativize(f);
                    if (relative.getNameCount() > 1) {
                        var pkg = relative.getParent().toString().replace("/", ".").replace("\\", ".");
                        return pkg + "." + name;
                    }
                    return name;
                }
            } catch (IOException e) {
                // skip
            }
        }
        return null;
    }

    static String capitalize(String s) {
        if (s.isEmpty()) return s;
        var sb = new StringBuilder();
        boolean upper = true;
        for (char c : s.toCharArray()) {
            if (c == '_' || c == '-') { upper = true; continue; }
            sb.append(upper ? Character.toUpperCase(c) : c);
            upper = false;
        }
        return sb.toString();
    }
}
