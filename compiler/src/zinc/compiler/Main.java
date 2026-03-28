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
        System.err.println("  build <file.zn|dir> [-o outdir]        compile to Java");
        System.err.println("  build --python <file.zn|dir> [-o dir]  transpile to Python");
        System.err.println("  run <file.zn|dir> [args...]            compile and run (Java)");
        System.err.println("  run --python <file.zn> [args...]        transpile and run (Python)");
        System.err.println("  run --python --main <file.zn>          transpile src/, run entry point");
        System.err.println("  init <name>                            create a new project");
    }

    // --- build ---------------------------------------------------------------

    private static void cmdBuild(List<String> args) {
        String input = null;
        Path outDir = null;
        boolean nativeImage = false;
        boolean fatJar = false;
        boolean packageApp = true; // default: jpackage + jlink (works with any library)
        boolean docker = false;
        boolean targetPython = false;

        for (int i = 0; i < args.size(); i++) {
            switch (args.get(i)) {
                case "-o" -> { if (i + 1 < args.size()) outDir = Path.of(args.get(++i)); }
                case "--native" -> { nativeImage = true; packageApp = false; }
                case "--fat-jar" -> { fatJar = true; packageApp = false; }
                case "--package" -> { packageApp = true; nativeImage = false; }
                case "--docker" -> { docker = true; packageApp = false; }
                case "--no-package" -> packageApp = false;
                case "--python", "--target-python" -> { targetPython = true; packageApp = false; }
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

        var result = targetPython
            ? compileToPython(inputPath, outDir)
            : compileProject(inputPath, outDir);
        switch (result) {
            case Result.Ok<List<Path>> ok -> {
                for (var f : ok.value()) System.out.println("compiled: " + f);
            }
            case Result.Err<List<Path>> err -> {
                for (var e : err.errors()) System.err.println("error: " + e);
                System.exit(1);
            }
        }

        // Python target — copy stdlib + done, no javac/Mill/jpackage
        if (targetPython) {
            try {
                ZincStdlib.copyPythonStdlib(outDir.resolve("app"));
            } catch (IOException e) {
                System.err.println("warning: could not copy stdlib: " + e.getMessage());
            }
            System.out.println("python transpilation complete: " + outDir);
            return;
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

        // Parse flags
        boolean targetPython = false;
        String input = null;
        String mainFile = null;
        var runArgs = new ArrayList<String>();
        boolean pastFlags = false;

        for (int i = 0; i < args.size(); i++) {
            var arg = args.get(i);
            if (arg.equals("--python") || arg.equals("--target-python")) {
                targetPython = true;
            } else if (arg.equals("--main") && i + 1 < args.size()) {
                mainFile = args.get(++i);
            } else if (!arg.startsWith("-") && input == null) {
                input = arg;
            } else if (input != null) {
                runArgs.add(arg);
            }
        }

        // Default input to src/ for Python projects, or current .zn file
        if (input == null && targetPython) {
            if (Files.isDirectory(Path.of("src"))) {
                input = "src";
            } else {
                System.err.println("error: no input file (no src/ directory found)");
                System.exit(1);
            }
        } else if (input == null) {
            System.err.println("error: no input file");
            System.exit(1);
        }

        var inputPath = Path.of(input);

        // Python target: transpile → copy stdlib → install deps → python
        if (targetPython) {
            // Check for zinc.toml project config
            ZincConfig config = null;
            var configFile = ZincConfig.findConfigFile(inputPath);
            if (configFile != null) {
                try {
                    config = ZincConfig.parse(configFile);
                    // Use project root's src/ as input if input is a directory
                    if (input.equals("src") || Files.isDirectory(inputPath)) {
                        var projectRoot = configFile.getParent();
                        inputPath = projectRoot.resolve("src");
                    }
                    // Use main from config if not specified on CLI
                    if (mainFile == null) mainFile = config.main;
                } catch (IOException e) {
                    System.err.println("warning: could not read zinc.toml: " + e.getMessage());
                }
            }

            var outDir = Path.of("/tmp/zinc-run-py-" + inputPath.getFileName().toString().replace(".zn", ""));
            var compileResult = compileToPython(inputPath, outDir);
            if (compileResult.isErr()) {
                for (var e : ((Result.Err<?>) compileResult).errors()) System.err.println("error: " + e);
                System.exit(1);
            }
            try { ZincStdlib.copyPythonStdlib(outDir.resolve("app")); }
            catch (IOException e) { System.err.println("warning: could not copy stdlib: " + e.getMessage()); }

            // Install Python deps from zinc.toml if present
            if (config != null && !config.pythonDeps.isEmpty()) {
                installPythonDeps(config.pythonDeps);
            }

            var pyFiles = compileResult.unwrap();
            Path mainPy = findMainPy(pyFiles, inputPath, mainFile);
            if (mainPy == null) {
                System.err.println("error: could not determine entry point");
                if (Files.isDirectory(inputPath)) {
                    System.err.println("hint: use --main <file.zn> to specify the entry point, or add main to zinc.toml");
                }
                System.exit(1);
            }
            // Run as module from parent dir to avoid stdlib shadowing
            System.exit(runPythonModule(outDir, mainPy, runArgs));
            return;
        }

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

    // --- Python compilation --------------------------------------------------

    /**
     * Compile .zn files to Python (.py) instead of Java.
     * Bypasses Transformer entirely — emits directly from Zinc AST.
     */
    public static Result<List<Path>> compileToPython(Path input, Path outDir) {
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

        // Output into app/ subdirectory — avoids Python stdlib name conflicts
        // when run with `python -m app.<module>`
        var appDir = outDir.resolve("app");
        var pyFiles = new ArrayList<Path>();

        for (var znFile : znFiles) {
            String source;
            try { source = Files.readString(znFile); }
            catch (IOException e) { return Result.err("cannot read " + znFile + ": " + e.getMessage()); }

            String fileName = znFile.getFileName().toString();
            String className = capitalize(fileName.replace(".zn", ""));

            // Lex
            var lexResult = new Lexer(source).tokenize();
            if (lexResult.isErr()) return Result.err(((Result.Err<?>) lexResult).errors());

            // Parse
            var parser = new Parser(lexResult.unwrap());
            var parseResult = parser.parseResult();
            if (parseResult.isErr()) return Result.err(((Result.Err<?>) parseResult).errors());
            var program = parseResult.unwrap();

            // Emit Python (skip TypeChecker and Transformer)
            var emitter = new PythonEmitter(className);
            var emitResult = emitter.emit(program, appDir);
            if (emitResult.isErr()) return Result.err(((Result.Err<?>) emitResult).errors());
            pyFiles.add(emitResult.unwrap());
        }

        // Create __init__.py for the app package
        try {
            Files.writeString(appDir.resolve("__init__.py"), "");
        } catch (IOException e) {
            return Result.err("failed to write __init__.py: " + e.getMessage());
        }

        return Result.ok(pyFiles);
    }

    // --- init ----------------------------------------------------------------

    private static void cmdInit(List<String> args) {
        if (args.isEmpty()) {
            System.err.println("usage: zinc init <project-name> [--python]");
            System.exit(1);
        }

        boolean python = args.contains("--python");
        String name = args.stream().filter(a -> !a.startsWith("-")).findFirst().orElse("app");
        var dir = Path.of(name);

        try {
            Files.createDirectories(dir.resolve("src"));

            // src/main.zn
            Files.writeString(dir.resolve("src/main.zn"), """
                fn main() {
                    print("Hello from %s!")
                }
                """.formatted(name).stripIndent());

            if (python) {
                // zinc.toml for Python projects
                Files.writeString(dir.resolve("zinc.toml"), """
                    [project]
                    name = "%s"
                    version = "0.1.0"
                    main = "main.zn"

                    [python]
                    version = ">=3.10"
                    deps = []
                    """.formatted(name).stripIndent());

                // .gitignore
                Files.writeString(dir.resolve(".gitignore"), """
                    out/
                    __pycache__/
                    *.pyc
                    .venv/
                    """.stripIndent());

                System.out.println("created project: " + name);
                System.out.println("  " + dir + "/src/main.zn");
                System.out.println("  " + dir + "/zinc.toml");
                System.out.println("\nrun: cd " + name + " && zinc run --python");
            } else {
                Files.createDirectories(dir.resolve("test"));

                // build.mill.yaml for Java projects
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
            }

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

    /** Run a Mill command in the project directory. Uses bundled Mill if available. */
    private static int runMill(Path projectDir, String command) {
        try {
            // Find Mill: bundled jar > system mill
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
        var compiler = javax.tools.ToolProvider.getSystemJavaCompiler();
        if (compiler == null) {
            return Result.err("javac not available — jdk.compiler module missing");
        }

        try {
            Files.createDirectories(outDir);
        } catch (IOException e) {
            return Result.err("cannot create output dir: " + e.getMessage());
        }

        var fileManager = compiler.getStandardFileManager(null, null, null);
        var sourceFiles = fileManager.getJavaFileObjectsFromPaths(javaFiles);
        var options = List.of(
            "--enable-preview", "--source", "25",
            "-d", outDir.toString());

        var diagnostics = new javax.tools.DiagnosticCollector<javax.tools.JavaFileObject>();
        var task = compiler.getTask(null, fileManager, diagnostics, options, null, sourceFiles);
        boolean success = task.call();

        try { fileManager.close(); } catch (IOException e) { /* ignore */ }

        if (!success) {
            var errors = new StringBuilder("javac failed:\n");
            for (var d : diagnostics.getDiagnostics()) {
                if (d.getKind() == javax.tools.Diagnostic.Kind.ERROR) {
                    errors.append(d.toString()).append("\n");
                }
            }
            return Result.err(errors.toString());
        }

        return Result.ok(null);
    }

    // --- java ----------------------------------------------------------------

    /**
     * Run a compiled class in-process via class loading.
     * No external java process needed — zinc is self-contained.
     */
    private static int runJava(String mainClass, Path classDir, List<String> args) {
        try {
            var classLoader = new java.net.URLClassLoader(
                new java.net.URL[]{classDir.toUri().toURL()},
                ClassLoader.getSystemClassLoader());
            var clazz = classLoader.loadClass(mainClass);
            var main = clazz.getMethod("main", String[].class);
            main.invoke(null, (Object) args.toArray(new String[0]));
            return 0;
        } catch (java.lang.reflect.InvocationTargetException e) {
            if (e.getCause() != null) {
                e.getCause().printStackTrace();
            }
            return 1;
        } catch (Exception e) {
            System.err.println("failed to run " + mainClass + ": " + e.getMessage());
            return 1;
        }
    }

    // --- python --------------------------------------------------------------

    /**
     * Find the main .py file to run.
     *
     * Resolution order:
     * 1. --main flag (explicit entry point)
     * 2. Single file input (the only file)
     * 3. File matching input name (concurrency.zn → zn_concurrency.py)
     * 4. main.zn convention (→ zn_main.py)
     * 5. null (error — user must specify --main)
     */
    private static Path findMainPy(List<Path> pyFiles, Path inputPath, String mainFile) {
        if (pyFiles.isEmpty()) return null;

        // 1. Explicit --main flag
        if (mainFile != null) {
            String target = mainFile.replace(".zn", ".py").toLowerCase();
            for (var f : pyFiles) {
                if (f.getFileName().toString().equals(target)) return f;
            }
            System.err.println("error: --main " + mainFile + " not found in transpiled output");
            return null;
        }

        // 2. Single file — obvious
        if (pyFiles.size() == 1) return pyFiles.getFirst();

        // 3. Match by input filename (single .zn file, not directory)
        if (!Files.isDirectory(inputPath)) {
            String target = inputPath.getFileName().toString().replace(".zn", ".py").toLowerCase();
            for (var f : pyFiles) {
                if (f.getFileName().toString().equals(target)) return f;
            }
        }

        // 4. Convention: main.zn → main.py
        for (var f : pyFiles) {
            if (f.getFileName().toString().equals("main.py")) return f;
        }

        // 5. Directory with multiple files and no main — error
        return null;
    }

    /**
     * Run a Python module via `python -m app.<module>` from the output root.
     * Using -m avoids the script directory being added to sys.path,
     * which prevents generated files from shadowing Python stdlib modules.
     */
    private static int runPythonModule(Path outDir, Path pyFile, List<String> args) {
        String python = findPython();
        if (python == null) {
            System.err.println("error: python3 not found. Install Python 3.10+ to use --python");
            return 1;
        }

        // Convert app/hello.py → app.hello
        String moduleName = "app." + pyFile.getFileName().toString().replace(".py", "");

        try {
            var cmd = new ArrayList<String>();
            cmd.add(python);
            cmd.add("-m");
            cmd.add(moduleName);
            cmd.addAll(args);

            var pb = new ProcessBuilder(cmd)
                .inheritIO()
                .directory(outDir.toFile());
            var proc = pb.start();
            return proc.waitFor();
        } catch (Exception e) {
            System.err.println("failed to run python: " + e.getMessage());
            return 1;
        }
    }

    /**
     * Run a Python file via subprocess (direct execution).
     */
    private static int runPython(Path pyFile, List<String> args) {
        String python = findPython();
        if (python == null) {
            System.err.println("error: python3 not found. Install Python 3.10+ to use --python");
            return 1;
        }

        try {
            var cmd = new ArrayList<String>();
            cmd.add(python);
            cmd.add(pyFile.toString());
            cmd.addAll(args);

            var pb = new ProcessBuilder(cmd)
                .inheritIO()
                .directory(pyFile.getParent().toFile());
            var proc = pb.start();
            return proc.waitFor();
        } catch (Exception e) {
            System.err.println("failed to run python: " + e.getMessage());
            return 1;
        }
    }

    /**
     * Install Python dependencies via pip.
     */
    private static void installPythonDeps(List<String> deps) {
        String python = findPython();
        if (python == null) return;

        try {
            var cmd = new ArrayList<>(List.of(python, "-m", "pip", "install", "-q"));
            cmd.addAll(deps);
            var proc = new ProcessBuilder(cmd)
                .inheritIO()
                .start();
            int exit = proc.waitFor();
            if (exit != 0) {
                System.err.println("warning: pip install failed (exit " + exit + ")");
            }
        } catch (Exception e) {
            System.err.println("warning: could not install deps: " + e.getMessage());
        }
    }

    private static String findPython() {
        for (var candidate : List.of("python3.14t", "python3.14", "python3.13", "python3.12", "python3.11", "python3.10", "python3", "python")) {
            try {
                var proc = new ProcessBuilder(candidate, "--version")
                    .redirectErrorStream(true)
                    .start();
                int exit = proc.waitFor();
                if (exit == 0) return candidate;
            } catch (Exception e) { /* not found, try next */ }
        }
        return null;
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
